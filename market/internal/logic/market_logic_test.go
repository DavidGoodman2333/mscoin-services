package logic

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
	"unsafe"

	"github.com/stretchr/testify/require"

	"grpc-common/market/types/market"
	"market/internal/domain"
	"market/internal/model"
)

// --- fake repos ------------------------------------------------

type fakeCoinRepo struct {
	unitMap       map[string]*model.Coin
	idMap         map[int64]*model.Coin
	findAllReturn []*model.Coin
	findAllErr    error
}

func (f *fakeCoinRepo) FindByUnit(ctx context.Context, unit string) (*model.Coin, error) {
	if c, ok := f.unitMap[unit]; ok {
		return c, nil
	}
	return nil, nil
}

func (f *fakeCoinRepo) FindById(ctx context.Context, id int64) (*model.Coin, error) {
	if c, ok := f.idMap[id]; ok {
		return c, nil
	}
	return nil, nil
}

func (f *fakeCoinRepo) FindAll(ctx context.Context) ([]*model.Coin, error) {
	return f.findAllReturn, f.findAllErr
}

type fakeExchangeCoinRepo struct {
	visible          []*model.ExchangeCoin
	bySymbol         map[string]*model.ExchangeCoin
	findVisibleErr   error
	findBySymbolErr  error
}

func (f *fakeExchangeCoinRepo) FindVisible(ctx context.Context) ([]*model.ExchangeCoin, error) {
	return f.visible, f.findVisibleErr
}

func (f *fakeExchangeCoinRepo) FindBySymbol(ctx context.Context, symbol string) (*model.ExchangeCoin, error) {
	if c, ok := f.bySymbol[symbol]; ok {
		return c, f.findBySymbolErr
	}
	return nil, nil
}

type fakeKlineRepo struct {
	klines []*model.Kline
	err    error
}

func (f *fakeKlineRepo) FindBySymbol(ctx context.Context, symbol, period string, count int64) ([]*model.Kline, error) {
	return nil, errors.New("unused")
}

func (f *fakeKlineRepo) FindBySymbolTime(ctx context.Context, symbol, period string, from, end int64, s string) ([]*model.Kline, error) {
	return f.klines, f.err
}

// --- helpers to inject unexported repos via reflection+unsafe ------------

func injectUnexportedField(target any, fieldName string, value any) {
	v := reflect.ValueOf(target).Elem()
	f := v.FieldByName(fieldName)
	if !f.IsValid() {
		panic("field not found: " + fieldName)
	}
	ptrToField := unsafe.Pointer(f.UnsafeAddr())
	real := reflect.NewAt(f.Type(), ptrToField).Elem()
	real.Set(reflect.ValueOf(value))
}

func makeCoinDomainWithFake(repo *fakeCoinRepo) *domain.CoinDomain {
	cd := &domain.CoinDomain{}
	injectUnexportedField(cd, "coinRepo", repo)
	return cd
}

func makeExchangeCoinDomainWithFake(repo *fakeExchangeCoinRepo) *domain.ExchangeCoinDomain {
	ed := &domain.ExchangeCoinDomain{}
	injectUnexportedField(ed, "exchangeCoinRepo", repo)
	return ed
}

func makeMarketDomainWithFake(repo *fakeKlineRepo) *domain.MarketDomain {
	md := &domain.MarketDomain{}
	injectUnexportedField(md, "klineRepo", repo)
	return md
}

// --- tests ------------------------------------------------

func TestFindCoinInfo_success_and_error(t *testing.T) {
	fakeRepo := &fakeCoinRepo{
		unitMap: map[string]*model.Coin{
			"USD": {Unit: "USD", Name: "Dollar"},
		},
	}
	coinDomain := makeCoinDomainWithFake(fakeRepo)
	logic := &MarketLogic{
		ctx:        context.Background(),
		coinDomain: coinDomain,
	}

	res, err := logic.FindCoinInfo(&market.MarketReq{Unit: "USD"})
	require.NoError(t, err)
	require.Equal(t, "USD", res.Unit)
	require.Equal(t, "Dollar", res.Name)

	_, err = logic.FindCoinInfo(&market.MarketReq{Unit: "EUR"})
	require.Error(t, err)

	fakeRepoErr := &fakeCoinRepo{
		findAllReturn: nil,
		findAllErr:    errors.New("db failure"),
	}
	cdErr := makeCoinDomainWithFake(fakeRepoErr)
	logicErr := &MarketLogic{ctx: context.Background(), coinDomain: cdErr}
	_, err = logicErr.FindCoinInfo(&market.MarketReq{Unit: "ANY"})
	require.Error(t, err)
}

func TestFindById_success_and_error(t *testing.T) {
	fakeRepo := &fakeCoinRepo{
		idMap: map[int64]*model.Coin{
			42: {Id: 42, Name: "TestCoin"},
		},
	}
	coinDomain := makeCoinDomainWithFake(fakeRepo)
	logic := &MarketLogic{
		ctx:        context.Background(),
		coinDomain: coinDomain,
	}

	res, err := logic.FindById(&market.MarketReq{Id: 42})
	require.NoError(t, err)
	require.Equal(t, int32(42), res.Id)
	require.Equal(t, "TestCoin", res.Name)

	_, err = logic.FindById(&market.MarketReq{Id: 100})
	require.Error(t, err)
}

func TestFindAllCoin_success_and_empty(t *testing.T) {
	all := []*model.Coin{{Unit: "u1"}, {Unit: "u2"}}
	fakeRepo := &fakeCoinRepo{
		findAllReturn: all,
	}
	coinDomain := makeCoinDomainWithFake(fakeRepo)
	logic := &MarketLogic{
		ctx:        context.Background(),
		coinDomain: coinDomain,
	}

	res, err := logic.FindAllCoin(&market.MarketReq{})
	require.NoError(t, err)
	require.Len(t, res.List, 2)

	fakeRepoEmpty := &fakeCoinRepo{
		findAllReturn: []*model.Coin{},
	}
	logicEmpty := &MarketLogic{
		ctx:        context.Background(),
		coinDomain: makeCoinDomainWithFake(fakeRepoEmpty),
	}
	res2, err := logicEmpty.FindAllCoin(&market.MarketReq{})
	require.NoError(t, err)
	require.Len(t, res2.List, 0)
}

func TestFindSymbolInfo_found_and_notfound(t *testing.T) {
	expected := &model.ExchangeCoin{Symbol: "X"}
	fakeExRepo := &fakeExchangeCoinRepo{
		visible:  []*model.ExchangeCoin{expected},
		bySymbol: map[string]*model.ExchangeCoin{"X": expected},
	}
	exDomain := makeExchangeCoinDomainWithFake(fakeExRepo)
	logic := &MarketLogic{
		ctx:                context.Background(),
		exchangeCoinDomain: exDomain,
	}

	req := &market.MarketReq{Symbol: "X"}
	res, err := logic.FindSymbolInfo(req)
	require.NoError(t, err)
	require.Equal(t, "X", res.Symbol)

	logicMissing := &MarketLogic{
		ctx:                context.Background(),
		exchangeCoinDomain: makeExchangeCoinDomainWithFake(&fakeExchangeCoinRepo{
			visible:  []*model.ExchangeCoin{},
			bySymbol: map[string]*model.ExchangeCoin{},
		}),
	}
	_, err = logicMissing.FindSymbolInfo(&market.MarketReq{Symbol: "Y"})
	require.Error(t, err)
}

func TestFindExchangeCoinVisible(t *testing.T) {
	coins := []*model.ExchangeCoin{{Symbol: "A"}, {Symbol: "B"}}
	fakeExRepo := &fakeExchangeCoinRepo{
		visible: coins,
	}
	logic := &MarketLogic{
		ctx:                context.Background(),
		exchangeCoinDomain: makeExchangeCoinDomainWithFake(fakeExRepo),
	}

	res, err := logic.FindExchangeCoinVisible(&market.MarketReq{})
	require.NoError(t, err)
	require.Len(t, res.List, 2)
}

func TestFindSymbolThumbTrend_empty_and_withData(t *testing.T) {
	marketDomain := makeMarketDomainWithFake(&fakeKlineRepo{
		klines: []*model.Kline{},
		err:    nil,
	})
	exDomainEmpty := makeExchangeCoinDomainWithFake(&fakeExchangeCoinRepo{
		visible: []*model.ExchangeCoin{},
	})
	logicEmpty := &MarketLogic{
		ctx:                context.Background(),
		exchangeCoinDomain: exDomainEmpty,
		marketDomain:       marketDomain,
	}

	resEmpty, err := logicEmpty.FindSymbolThumbTrend(&market.MarketReq{})
	require.NoError(t, err)
	require.Len(t, resEmpty.List, 0)

	kline := &model.Kline{
		ClosePrice:    10,
		OpenPrice:     8,
		HighestPrice:  12,
		LowestPrice:   7,
		Time:          time.Now().UnixMilli(),
		Volume:        100,
		Turnover:      200,
	}
	fakeKRepo := &fakeKlineRepo{
		klines: []*model.Kline{kline},
		err:    nil,
	}
	marketDomain2 := makeMarketDomainWithFake(fakeKRepo)
	exDomain := makeExchangeCoinDomainWithFake(&fakeExchangeCoinRepo{
		visible: []*model.ExchangeCoin{{Symbol: "SYM"}},
		bySymbol: map[string]*model.ExchangeCoin{
			"SYM": {Symbol: "SYM"},
		},
	})
	logic := &MarketLogic{
		ctx:                context.Background(),
		exchangeCoinDomain: exDomain,
		marketDomain:       marketDomain2,
	}

	res, err := logic.FindSymbolThumbTrend(&market.MarketReq{})
	require.NoError(t, err)
	require.Len(t, res.List, 1)
	thumb := res.List[0]
	require.Equal(t, "SYM", thumb.Symbol)
	require.Equal(t, float64(10), thumb.Close)
	require.NotEmpty(t, thumb.Trend)
}

func TestHistoryKline_resolution_and_error(t *testing.T) {
	testKline := &model.Kline{
		Time:          1000,
		OpenPrice:     1,
		HighestPrice:  2,
		LowestPrice:   0.5,
		ClosePrice:    1.5,
		Volume:        10,
		Turnover:      20,
	}
	fakeMk := &fakeKlineRepo{
		klines: []*model.Kline{testKline},
		err:    nil,
	}
	logic := &MarketLogic{
		ctx:          context.Background(),
		marketDomain: makeMarketDomainWithFake(fakeMk),
	}

	res, err := logic.HistoryKline(&market.MarketReq{
		Resolution: "15",
		Symbol:     "ABC",
		From:       1000,
		To:         2000,
	})
	require.NoError(t, err)
	require.Len(t, res.List, 1)
	h := res.List[0]
	require.Equal(t, int64(1000), h.Time)
	require.Equal(t, float64(1), h.Open)
	require.Equal(t, float64(1.5), h.Close)

	fakeMkErr := &fakeKlineRepo{
		klines: nil,
		err:    errors.New("fail"),
	}
	logicErr := &MarketLogic{
		ctx:          context.Background(),
		marketDomain: makeMarketDomainWithFake(fakeMkErr),
	}
	_, err = logicErr.HistoryKline(&market.MarketReq{Resolution: "5", Symbol: "X", From: 0, To: 1})
	require.Error(t, err)
}
