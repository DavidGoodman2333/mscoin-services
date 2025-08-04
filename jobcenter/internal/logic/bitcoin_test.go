package logic

import (
	"context"
	"errors"
	"mscoin-common/tools"
	"testing"

	"github.com/stretchr/testify/require"

	"grpc-common/ucenter/types/asset"
)

// minimal stubs used in logic

type fakeCache struct {
	data map[string]string
	set  map[string]interface{}
}

func (f *fakeCache) Get(key string, dest *string) {
	if f.data == nil {
		*dest = ""
		return
	}
	*dest = f.data[key]
}

func (f *fakeCache) Set(key string, val interface{}) {
	if f.set == nil {
		f.set = make(map[string]interface{})
	}
	f.set[key] = val
}

func (f *fakeCache) GetCalledVal(key string) string {
	if f.data == nil {
		return ""
	}
	return f.data[key]
}

// fakeAssetRpc implements ucclient.Asset minimal part
type fakeAssetRpc struct {
	addressList *asset.AddressList
	err         error
}

func (f *fakeAssetRpc) GetAddress(ctx context.Context, in *asset.AssetReq) (*asset.AddressList, error) {
	return f.addressList, f.err
}

// fakeBitCoinDomain records recharge calls
type fakeBitCoinDomain struct {
	recharged []rechargeCall
	err       error
}

type rechargeCall struct {
	txId      string
	value     float64
	address   string
	time      int64
	blockhash string
}

func (f *fakeBitCoinDomain) Recharge(txId string, value float64, address string, time int64, blockhash string) error {
	if f.err != nil {
		return f.err
	}
	f.recharged = append(f.recharged, rechargeCall{txId, value, address, time, blockhash})
	return nil
}

// fakeQueueDomain records SendRecharge
type fakeQueueDomain struct {
	sent []map[string]any
}

func (f *fakeQueueDomain) SendRecharge(value float64, address string, time int64) {
	f.sent = append(f.sent, map[string]any{
		"value":   value,
		"address": address,
		"time":    time,
	})
}

// testable version of BitCoin with overridable RPC helpers
type testableBitCoin struct {
	wgDoneCalled   bool
	ch             *fakeCache
	assetRpc       *fakeAssetRpc
	bitCoinDomain  *fakeBitCoinDomain
	queueDomain    *fakeQueueDomain
	getMiningInfoF func(addr string) (int64, error)
	getBlockHashF  func(height int64, addr string) (string, error)
	getBlockF      func(blockHash, addr string) ([]string, error)
	getRawTxF      func(txId, addr string) (*RawTransaction, error)
}

func (b *testableBitCoin) Do(address string) {
	b.ScanTx(address)
}

func (b *testableBitCoin) ScanTx(btcAddress string) {
	var dealBlocksStr string
	val := b.ch.data["BTC::TX"]
	dealBlocksStr = val
	var dealBlocks int64
	if dealBlocksStr == "" {
		dealBlocks = 2428713
	} else {
		dealBlocks = tools.ToInt64(dealBlocksStr)
	}
	currentBlocks, err := b.getMiningInfoF(btcAddress)
	if err != nil {
		b.wgDoneCalled = true
		return
	}
	diff := currentBlocks - dealBlocks
	if diff <= 0 {
		b.wgDoneCalled = true
		return
	}
	// get address list
	addressListResp, err := b.assetRpc.GetAddress(context.Background(), &asset.AssetReq{CoinName: "BTC"})
	if err != nil {
		b.wgDoneCalled = true
		return
	}
	addressList := addressListResp.List
	for i := currentBlocks; i > dealBlocks; i-- {
		blockHash, err := b.getBlockHashF(i, btcAddress)
		if err != nil {
			continue
		}
		txIdList, err := b.getBlockF(blockHash, btcAddress)
		if err != nil {
			continue
		}
		for _, txId := range txIdList {
			txResult, err := b.getRawTxF(txId, btcAddress)
			if err != nil {
				continue
			}
			inputAddressList := make([]string, len(txResult.Vin))
			for idx, vin := range txResult.Vin {
				if vin.Txid == "" {
					continue
				}
				inputTx, err := b.getRawTxF(vin.Txid, btcAddress)
				if err != nil {
					continue
				}
				vout := inputTx.Vout[vin.Vout]
				inputAddressList[idx] = vout.ScriptPubKey.Address
			}
			for _, vout := range txResult.Vout {
				voutAddress := vout.ScriptPubKey.Address
				flag := false
				for _, inputAddress := range inputAddressList {
					if inputAddress != "" && voutAddress != "" && inputAddress == voutAddress {
						flag = true
					}
				}
				if flag {
					continue
				}
				for _, address := range addressList {
					if address != "" && address == voutAddress {
						err := b.bitCoinDomain.Recharge(txResult.TxId, vout.Value, voutAddress, txResult.Time, txResult.Blockhash)
						if err != nil {
							continue
						}
						b.queueDomain.SendRecharge(vout.Value, voutAddress, txResult.Time)
					}
				}
			}
		}
	}
	b.ch.Set("BTC::TX", currentBlocks)
	b.wgDoneCalled = true
}

// ---- helpers to build RawTransaction results ----

func makeRawTx(txid string, vinTxid string, vinVout int, voutAddress string, voutValue float64) *RawTransaction {
	return &RawTransaction{
		TxId: txid,
		Vin: []Vin{
			{
				Txid: vinTxid,
				Vout: vinVout,
			},
		},
		Vout: []Vout{
			{
				Value: voutValue,
				ScriptPubKey: ScriptPubKey{
					Address: voutAddress,
				},
			},
		},
		Time:      123456,
		Blockhash: "block-hash",
	}
}

// ---- tests ----

func TestScanTx_noNewBlocks(t *testing.T) {
	b := &testableBitCoin{
		ch:            &fakeCache{data: map[string]string{"BTC::TX": "3000"}},
		assetRpc:      &fakeAssetRpc{addressList: &asset.AddressList{List: []string{"addr1"}}},
		bitCoinDomain: &fakeBitCoinDomain{},
		queueDomain:   &fakeQueueDomain{},
		getMiningInfoF: func(addr string) (int64, error) {
			return 3000, nil
		},
		getBlockHashF: func(height int64, addr string) (string, error) { return "", nil },
		getBlockF:     func(blockHash, addr string) ([]string, error) { return nil, nil },
		getRawTxF:     func(txId, addr string) (*RawTransaction, error) { return nil, nil },
	}
	b.ScanTx("dummy")
	require.True(t, b.wgDoneCalled)
	// no recharge should happen
	require.Empty(t, b.bitCoinDomain.recharged)
	require.Empty(t, b.queueDomain.sent)
}

func TestScanTx_miningInfoError(t *testing.T) {
	b := &testableBitCoin{
		ch:            &fakeCache{data: map[string]string{}},
		assetRpc:      &fakeAssetRpc{},
		bitCoinDomain: &fakeBitCoinDomain{},
		queueDomain:   &fakeQueueDomain{},
		getMiningInfoF: func(addr string) (int64, error) {
			return 0, errors.New("rpc fail")
		},
		getBlockHashF: func(height int64, addr string) (string, error) { return "", nil },
		getBlockF:     func(blockHash, addr string) ([]string, error) { return nil, nil },
		getRawTxF:     func(txId, addr string) (*RawTransaction, error) { return nil, nil },
	}
	b.ScanTx("dummy")
	require.True(t, b.wgDoneCalled)
}

func TestScanTx_assetRpcError(t *testing.T) {
	b := &testableBitCoin{
		ch:            &fakeCache{data: map[string]string{"BTC::TX": "1000"}},
		assetRpc:      &fakeAssetRpc{err: errors.New("fail")},
		bitCoinDomain: &fakeBitCoinDomain{},
		queueDomain:   &fakeQueueDomain{},
		getMiningInfoF: func(addr string) (int64, error) {
			return 2000, nil
		},
		getBlockHashF: func(height int64, addr string) (string, error) { return "hash", nil },
		getBlockF:     func(blockHash, addr string) ([]string, error) { return []string{"tx1"}, nil },
		getRawTxF:     func(txId, addr string) (*RawTransaction, error) { return makeRawTx("tx1", "", 0, "addr-match", 1.2), nil },
	}
	b.ScanTx("dummy")
	require.True(t, b.wgDoneCalled)
	// no recharge because asset RPC failure
	require.Empty(t, b.bitCoinDomain.recharged)
}

func TestScanTx_successfulRecharge(t *testing.T) {
	addrToMatch := "match-address"
	rawTx := &RawTransaction{
		TxId: "tx_main",
		Vin: []Vin{
			{Txid: "", Vout: 0}, // no input address
		},
		Vout: []Vout{
			{
				Value: 2.5,
				ScriptPubKey: ScriptPubKey{
					Address: addrToMatch,
				},
			},
		},
		Time:      999,
		Blockhash: "bh",
	}
	b := &testableBitCoin{
		ch:            &fakeCache{data: map[string]string{"BTC::TX": "1000"}},
		assetRpc:      &fakeAssetRpc{addressList: &asset.AddressList{List: []string{addrToMatch}}},
		bitCoinDomain: &fakeBitCoinDomain{},
		queueDomain:   &fakeQueueDomain{},
		getMiningInfoF: func(addr string) (int64, error) {
			return 1002, nil // diff=2 >0
		},
		getBlockHashF: func(height int64, addr string) (string, error) {
			return "blockhash", nil
		},
		getBlockF: func(blockHash, addr string) ([]string, error) {
			return []string{"tx_main"}, nil
		},
		getRawTxF: func(txId, addr string) (*RawTransaction, error) {
			return rawTx, nil
		},
	}
	b.ScanTx("dummy")
	require.True(t, b.wgDoneCalled)
	// expect one recharge and one queue send
	require.Len(t, b.bitCoinDomain.recharged, 1)
	require.Len(t, b.queueDomain.sent, 1)
	call := b.bitCoinDomain.recharged[0]
	require.Equal(t, "tx_main", call.txId)
	require.Equal(t, addrToMatch, call.address)
	require.Equal(t, float64(2.5), call.value)
}

func TestScanTx_inputAddressFilter(t *testing.T) {
	// if input address equals output address, skip recharge
	addr := "same"
	rawTxMain := &RawTransaction{
		TxId: "tx_main",
		Vin: []Vin{
			{Txid: "prev", Vout: 0},
		},
		Vout: []Vout{
			{
				Value: 1.1,
				ScriptPubKey: ScriptPubKey{
					Address: addr,
				},
			},
		},
		Time:      888,
		Blockhash: "bh2",
	}
	prevTx := &RawTransaction{
		TxId: "prev",
		Vout: []Vout{
			{
				ScriptPubKey: ScriptPubKey{
					Address: addr,
				},
			},
		},
	}
	b := &testableBitCoin{
		ch:            &fakeCache{data: map[string]string{"BTC::TX": "1000"}},
		assetRpc:      &fakeAssetRpc{addressList: &asset.AddressList{List: []string{addr}}},
		bitCoinDomain: &fakeBitCoinDomain{},
		queueDomain:   &fakeQueueDomain{},
		getMiningInfoF: func(addr string) (int64, error) {
			return 1001, nil
		},
		getBlockHashF: func(height int64, addr string) (string, error) { return "blockhash", nil },
		getBlockF:     func(blockHash, addr string) ([]string, error) { return []string{"tx_main"}, nil },
		getRawTxF: func(txId, addr string) (*RawTransaction, error) {
			if txId == "prev" {
				return prevTx, nil
			}
			return rawTxMain, nil
		},
	}
	b.ScanTx("dummy")
	require.True(t, b.wgDoneCalled)
	require.Len(t, b.bitCoinDomain.recharged, 0)
	require.Len(t, b.queueDomain.sent, 0)
}
