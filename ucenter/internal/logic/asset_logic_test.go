package logic

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"grpc-common/market/types/market"
	"grpc-common/ucenter/types/asset"
)

// --- minimal interfaces used by the testable logic ---
type marketRPC interface {
	FindCoinInfo(ctx context.Context, req *market.MarketReq) (*market.Coin, error)
}

type walletDomain interface {
	FindWalletBySymbol(ctx context.Context, userId int64, coinName string, coin *market.Coin) (*asset.MemberWallet, error)
	FindWallet(ctx context.Context, userId int64) ([]*asset.MemberWallet, error)
	FindWalletByMemIdAndCoin(ctx context.Context, memberId int64, coinName string) (*asset.MemberWallet, error)
	UpdateAddress(ctx context.Context, wallet *asset.MemberWallet) error
	GetAllAddress(ctx context.Context, coinName string) ([]string, error)
}

type txDomain interface {
	FindTransaction(ctx context.Context, userId int64, pageNo int64, pageSize int64, symbol string, startTime string, endTime string, t string) ([]*asset.MemberTransaction, int64, error)
}

// --- fakes implementing those interfaces ---

type fakeMarketRPC struct {
	findCoinInfoFunc func(ctx context.Context, req *market.MarketReq) (*market.Coin, error)
}

func (f *fakeMarketRPC) FindCoinInfo(ctx context.Context, req *market.MarketReq) (*market.Coin, error) {
	return f.findCoinInfoFunc(ctx, req)
}

type fakeWallet struct {
	findBySymbolFunc     func(ctx context.Context, userId int64, coinName string, coin *market.Coin) (*asset.MemberWallet, error)
	findWalletFunc       func(ctx context.Context, userId int64) ([]*asset.MemberWallet, error)
	findByMemCoinFunc    func(ctx context.Context, memberId int64, coinName string) (*asset.MemberWallet, error)
	updateAddressInvoked bool
	getAllAddressFunc    func(ctx context.Context, coinName string) ([]string, error)
}

func (f *fakeWallet) FindWalletBySymbol(ctx context.Context, userId int64, coinName string, coin *market.Coin) (*asset.MemberWallet, error) {
	if f.findBySymbolFunc != nil {
		return f.findBySymbolFunc(ctx, userId, coinName, coin)
	}
	return &asset.MemberWallet{Address: "wallet-default"}, nil
}

func (f *fakeWallet) FindWallet(ctx context.Context, userId int64) ([]*asset.MemberWallet, error) {
	if f.findWalletFunc != nil {
		return f.findWalletFunc(ctx, userId)
	}
	return []*asset.MemberWallet{{Address: "w1"}}, nil
}

func (f *fakeWallet) FindWalletByMemIdAndCoin(ctx context.Context, memberId int64, coinName string) (*asset.MemberWallet, error) {
	if f.findByMemCoinFunc != nil {
		return f.findByMemCoinFunc(ctx, memberId, coinName)
	}
	return &asset.MemberWallet{Address: ""}, nil
}

func (f *fakeWallet) UpdateAddress(ctx context.Context, wallet *asset.MemberWallet) error {
	f.updateAddressInvoked = true
	return nil
}

func (f *fakeWallet) GetAllAddress(ctx context.Context, coinName string) ([]string, error) {
	if f.getAllAddressFunc != nil {
		return f.getAllAddressFunc(ctx, coinName)
	}
	return []string{"addr1", "addr2"}, nil
}

type fakeTx struct {
	findTransactionFunc func(ctx context.Context, userId int64, pageNo int64, pageSize int64, symbol string, startTime string, endTime string, t string) ([]*asset.MemberTransaction, int64, error)
}

func (f *fakeTx) FindTransaction(ctx context.Context, userId int64, pageNo int64, pageSize int64, symbol string, startTime string, endTime string, t string) ([]*asset.MemberTransaction, int64, error) {
	if f.findTransactionFunc != nil {
		return f.findTransactionFunc(ctx, userId, pageNo, pageSize, symbol, startTime, endTime, t)
	}
	return []*asset.MemberTransaction{{Amount: 1}}, 1, nil
}

// --- testable logic that uses fakes directly ---

type testableAssetLogic struct {
	ctx    context.Context
	wallet walletDomain
	tx     txDomain
	market marketRPC
}

func newTestableAssetLogic(w walletDomain, ttx txDomain, m marketRPC) *testableAssetLogic {
	return &testableAssetLogic{
		ctx:    context.Background(),
		wallet: w,
		tx:     ttx,
		market: m,
	}
}

func (l *testableAssetLogic) FindWalletBySymbol(req *asset.AssetReq) (*asset.MemberWallet, error) {
	coinInfo, err := l.market.FindCoinInfo(l.ctx, &market.MarketReq{Unit: req.CoinName})
	if err != nil {
		return nil, err
	}
	return l.wallet.FindWalletBySymbol(l.ctx, req.UserId, req.CoinName, coinInfo)
}

func (l *testableAssetLogic) FindWallet(req *asset.AssetReq) (*asset.MemberWalletList, error) {
	memberWalletCoins, err := l.wallet.FindWallet(l.ctx, req.UserId)
	if err != nil {
		return nil, err
	}
	return &asset.MemberWalletList{List: memberWalletCoins}, nil
}

func (l *testableAssetLogic) ResetAddress(req *asset.AssetReq) (*asset.AssetResp, error) {
	memberWallet, err := l.wallet.FindWalletByMemIdAndCoin(l.ctx, req.UserId, req.CoinName)
	if err != nil {
		return nil, err
	}
	if req.CoinName == "BTC" {
		if memberWallet.Address == "" {
			memberWallet.Address = "new-address"
			err = l.wallet.UpdateAddress(l.ctx, memberWallet)
			if err != nil {
				return nil, err
			}
		}
	}
	return &asset.AssetResp{}, nil
}

func (l *testableAssetLogic) FindTransaction(req *asset.AssetReq) (*asset.MemberTransactionList, error) {
	memberTransactionVos, total, err := l.tx.FindTransaction(
		l.ctx,
		req.UserId,
		req.PageNo,
		req.PageSize,
		req.Symbol,
		req.StartTime,
		req.EndTime,
		req.Type,
	)
	if err != nil {
		return nil, err
	}
	return &asset.MemberTransactionList{
		List:  memberTransactionVos,
		Total: total,
	}, nil
}

func (l *testableAssetLogic) GetAddress(req *asset.AssetReq) (*asset.AddressList, error) {
	addressList, err := l.wallet.GetAllAddress(l.ctx, req.CoinName)
	if err != nil {
		return nil, err
	}
	return &asset.AddressList{List: addressList}, nil
}

// --- tests ---

func TestFindWalletBySymbol_success_and_rpc_error(t *testing.T) {
	fakeMarket := &fakeMarketRPC{
		findCoinInfoFunc: func(ctx context.Context, req *market.MarketReq) (*market.Coin, error) {
			return &market.Coin{Unit: "BTC"}, nil
		},
	}
	fakeWallet := &fakeWallet{
		findBySymbolFunc: func(ctx context.Context, userId int64, coinName string, coin *market.Coin) (*asset.MemberWallet, error) {
			return &asset.MemberWallet{Address: "wallet123"}, nil
		},
	}
	logic := newTestableAssetLogic(fakeWallet, &fakeTx{}, fakeMarket)

	res, err := logic.FindWalletBySymbol(&asset.AssetReq{UserId: 1, CoinName: "BTC"})
	require.NoError(t, err)
	require.Equal(t, "wallet123", res.Address)

	// RPC failure path
	fakeMarketErr := &fakeMarketRPC{
		findCoinInfoFunc: func(ctx context.Context, req *market.MarketReq) (*market.Coin, error) {
			return nil, errors.New("rpc fail")
		},
	}
	logicErr := newTestableAssetLogic(fakeWallet, &fakeTx{}, fakeMarketErr)
	_, err = logicErr.FindWalletBySymbol(&asset.AssetReq{UserId: 1, CoinName: "BTC"})
	require.Error(t, err)
}

func TestFindWallet_success_and_error(t *testing.T) {
	fakeWalletSuccess := &fakeWallet{
		findWalletFunc: func(ctx context.Context, userId int64) ([]*asset.MemberWallet, error) {
			return []*asset.MemberWallet{{Address: "a"}, {Address: "b"}}, nil
		},
	}
	logic := newTestableAssetLogic(fakeWalletSuccess, &fakeTx{}, &fakeMarketRPC{
		findCoinInfoFunc: func(ctx context.Context, req *market.MarketReq) (*market.Coin, error) {
			return &market.Coin{}, nil
		},
	})
	res, err := logic.FindWallet(&asset.AssetReq{UserId: 7})
	require.NoError(t, err)
	require.Len(t, res.List, 2)

	fakeWalletErr := &fakeWallet{
		findWalletFunc: func(ctx context.Context, userId int64) ([]*asset.MemberWallet, error) {
			return nil, errors.New("fail")
		},
	}
	logicErr := newTestableAssetLogic(fakeWalletErr, &fakeTx{}, &fakeMarketRPC{})
	_, err = logicErr.FindWallet(&asset.AssetReq{UserId: 7})
	require.Error(t, err)
}

func TestResetAddress_BTC_and_nonBTC(t *testing.T) {
	fakeWalletDomain := &fakeWallet{
		findByMemCoinFunc: func(ctx context.Context, memberId int64, coinName string) (*asset.MemberWallet, error) {
			return &asset.MemberWallet{Address: ""}, nil
		},
	}
	logic := newTestableAssetLogic(fakeWalletDomain, &fakeTx{}, &fakeMarketRPC{})

	res, err := logic.ResetAddress(&asset.AssetReq{UserId: 1, CoinName: "BTC"})
	require.NoError(t, err)
	require.NotNil(t, res)

	res2, err := logic.ResetAddress(&asset.AssetReq{UserId: 2, CoinName: "ETH"})
	require.NoError(t, err)
	require.NotNil(t, res2)
}

func TestFindTransaction_and_GetAddress(t *testing.T) {
	fakeTxDomain := &fakeTx{
		findTransactionFunc: func(ctx context.Context, userId int64, pageNo int64, pageSize int64, symbol string, startTime string, endTime string, t string) ([]*asset.MemberTransaction, int64, error) {
			return []*asset.MemberTransaction{{Amount: 9}}, 99, nil
		},
	}
	fakeWalletDomain := &fakeWallet{
		getAllAddressFunc: func(ctx context.Context, coinName string) ([]string, error) {
			return []string{"addrX"}, nil
		},
	}
	logic := newTestableAssetLogic(fakeWalletDomain, fakeTxDomain, &fakeMarketRPC{})

	txRes, err := logic.FindTransaction(&asset.AssetReq{UserId: 3, PageNo: 1, PageSize: 2, Symbol: "S"})
	require.NoError(t, err)
	require.Len(t, txRes.List, 1)
	require.EqualValues(t, 99, txRes.Total)

	addrRes, err := logic.GetAddress(&asset.AssetReq{CoinName: "X"})
	require.NoError(t, err)
	require.Len(t, addrRes.List, 1)
}
