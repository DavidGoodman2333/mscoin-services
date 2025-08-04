package logic

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jinzhu/copier"
	"github.com/stretchr/testify/require"

	"exchange/internal/model"
	"grpc-common/exchange/types/order"
	"grpc-common/market/types/market"
	"grpc-common/ucenter/types/asset"
	"grpc-common/ucenter/types/member"
)

// minimal interfaces

type orderDomain interface {
	FindOrderHistory(ctx context.Context, symbol string, page, size, memberId int64) ([]*order.ExchangeOrder, int64, error)
	FindOrderCurrent(ctx context.Context, symbol string, page, size, memberId int64) ([]*order.ExchangeOrder, int64, error)
	FindCurrentTradingCount(ctx context.Context, userId int64, symbol, direction string) (int64, error)
	AddOrder(ctx context.Context, order *model.ExchangeOrder, exchangeCoin *market.ExchangeCoin, baseWallet *asset.MemberWallet, coinWallet *asset.MemberWallet) (float64, error)
	FindOrderByOrderId(ctx context.Context, orderId string) (*model.ExchangeOrder, error)
	UpdateStatusCancel(ctx context.Context, orderId string) error
}

type memberRPC interface {
	FindMemberById(ctx context.Context, req *member.MemberReq) (*member.MemberInfo, error)
}

type marketRPC interface {
	FindSymbolInfo(ctx context.Context, req *market.MarketReq) (*market.ExchangeCoin, error)
	FindCoinInfo(ctx context.Context, req *market.MarketReq) (*market.Coin, error)
}

type assetRPC interface {
	FindWalletBySymbol(ctx context.Context, req *asset.AssetReq) (*asset.MemberWallet, error)
}

type kafkaDomain interface {
	SendOrderAdd(topic string, userId int64, orderId string, money float64, symbol string, direction int, baseSymbol string, coinSymbol string) error
}

// fakes

type fakeOrderDomain struct {
	history           []*order.ExchangeOrder
	historyTotal      int64
	historyErr        error
	current           []*order.ExchangeOrder
	currentTotal      int64
	currentErr        error
	tradingCount      int64
	tradingCountErr   error
	addOrderMoney     float64
	addOrderErr       error
	findByOrderIdRet  *model.ExchangeOrder
	findByOrderIdErr  error
	cancelCalled      bool
}

func (f *fakeOrderDomain) FindOrderHistory(ctx context.Context, symbol string, page, size, memberId int64) ([]*order.ExchangeOrder, int64, error) {
	return f.history, f.historyTotal, f.historyErr
}
func (f *fakeOrderDomain) FindOrderCurrent(ctx context.Context, symbol string, page, size, memberId int64) ([]*order.ExchangeOrder, int64, error) {
	return f.current, f.currentTotal, f.currentErr
}
func (f *fakeOrderDomain) FindCurrentTradingCount(ctx context.Context, userId int64, symbol, direction string) (int64, error) {
	return f.tradingCount, f.tradingCountErr
}
func (f *fakeOrderDomain) AddOrder(ctx context.Context, order *model.ExchangeOrder, exchangeCoin *market.ExchangeCoin, baseWallet *asset.MemberWallet, coinWallet *asset.MemberWallet) (float64, error) {
	return f.addOrderMoney, f.addOrderErr
}
func (f *fakeOrderDomain) FindOrderByOrderId(ctx context.Context, orderId string) (*model.ExchangeOrder, error) {
	return f.findByOrderIdRet, f.findByOrderIdErr
}
func (f *fakeOrderDomain) UpdateStatusCancel(ctx context.Context, orderId string) error {
	f.cancelCalled = true
	return nil
}

type fakeMemberRPC struct {
	memberRes *member.MemberInfo
	err       error
}

func (f *fakeMemberRPC) FindMemberById(ctx context.Context, req *member.MemberReq) (*member.MemberInfo, error) {
	return f.memberRes, f.err
}

type fakeMarketRPC struct {
	symbolInfo *market.ExchangeCoin
	coinInfo   *market.Coin
	errSymbol  error
	errCoin    error
}

func (f *fakeMarketRPC) FindSymbolInfo(ctx context.Context, req *market.MarketReq) (*market.ExchangeCoin, error) {
	return f.symbolInfo, f.errSymbol
}
func (f *fakeMarketRPC) FindCoinInfo(ctx context.Context, req *market.MarketReq) (*market.Coin, error) {
	return f.coinInfo, f.errCoin
}

type fakeAssetRPC struct {
	baseWallet *asset.MemberWallet
	coinWallet *asset.MemberWallet
	errBase    error
	errCoin    error
}

func (f *fakeAssetRPC) FindWalletBySymbol(ctx context.Context, req *asset.AssetReq) (*asset.MemberWallet, error) {
	if req.CoinName == "BASE" {
		return f.baseWallet, f.errBase
	}
	return f.coinWallet, f.errCoin
}

type fakeKafkaDomain struct {
	sent bool
	err  error
}

func (f *fakeKafkaDomain) SendOrderAdd(topic string, userId int64, orderId string, money float64, symbol string, direction int, baseSymbol string, coinSymbol string) error {
	f.sent = true
	return f.err
}

// testable logic using minimal interfaces

type testableExchangeOrderLogic struct {
	ctx        context.Context
	orderDom   orderDomain
	memberRpc  memberRPC
	marketRpc  marketRPC
	assetRpc   assetRPC
	kafka      kafkaDomain
}

func (l *testableExchangeOrderLogic) FindOrderHistory(req *order.OrderReq) (*order.OrderRes, error) {
	exchangeOrders, total, err := l.orderDom.FindOrderHistory(l.ctx, req.Symbol, req.Page, req.PageSize, req.UserId)
	if err != nil {
		return nil, err
	}
	return &order.OrderRes{List: exchangeOrders, Total: total}, nil
}

func (l *testableExchangeOrderLogic) FindOrderCurrent(req *order.OrderReq) (*order.OrderRes, error) {
	exchangeOrders, total, err := l.orderDom.FindOrderCurrent(l.ctx, req.Symbol, req.Page, req.PageSize, req.UserId)
	if err != nil {
		return nil, err
	}
	return &order.OrderRes{List: exchangeOrders, Total: total}, nil
}

func (l *testableExchangeOrderLogic) CancelOrder(req *order.OrderReq) (*order.CancelOrderRes, error) {
	_ = l.orderDom.UpdateStatusCancel(l.ctx, req.OrderId)
	return &order.CancelOrderRes{}, nil
}

func (l *testableExchangeOrderLogic) FindByOrderId(req *order.OrderReq) (*order.ExchangeOrderOrigin, error) {
	exchangeOrder, err := l.orderDom.FindOrderByOrderId(l.ctx, req.OrderId)
	if err != nil {
		return nil, err
	}
	oo := &order.ExchangeOrderOrigin{}
	copier.Copy(oo, exchangeOrder)
	return oo, nil
}

func (l *testableExchangeOrderLogic) Add(req *order.OrderReq) (*order.AddOrderRes, error) {
	memberRes, err := l.memberRpc.FindMemberById(l.ctx, &member.MemberReq{MemberId: req.UserId})
	if err != nil {
		return nil, err
	}
	if memberRes.TransactionStatus == 0 {
		return nil, errors.New("此用户已经被禁止交易")
	}
	if req.Type == model.TypeMap[model.LimitPrice] && req.Price <= 0 {
		return nil, errors.New("限价模式下价格不能小于等于0")
	}
	if req.Amount <= 0 {
		return nil, errors.New("数量不能小于等于0")
	}
	exchangeCoin, err := l.marketRpc.FindSymbolInfo(l.ctx, &market.MarketReq{Symbol: req.Symbol})
	if err != nil {
		return nil, errors.New("nonsupport coin")
	}
	if exchangeCoin.Exchangeable != 1 && exchangeCoin.Enable != 1 {
		return nil, errors.New("coin forbidden")
	}
	baseSymbol := exchangeCoin.GetBaseSymbol()
	coinSymbol := exchangeCoin.GetCoinSymbol()
	cc := baseSymbol
	if req.Direction == model.DirectionMap.Value(model.SELL) {
		cc = coinSymbol
	}
	coin, err := l.marketRpc.FindCoinInfo(l.ctx, &market.MarketReq{Unit: cc})
	if err != nil || coin == nil {
		return nil, errors.New("nonsupport coin")
	}
	if req.Type == model.TypeMap[model.MarketPrice] && req.Direction == model.DirectionMap.Value(model.BUY) {
		if exchangeCoin.GetMinTurnover() > 0 && req.Amount < float64(exchangeCoin.GetMinTurnover()) {
			return nil, errors.New("成交额至少是" + fmt.Sprintf("%d", exchangeCoin.GetMinTurnover()))
		}
	} else {
		if exchangeCoin.GetMaxVolume() > 0 && exchangeCoin.GetMaxVolume() < req.Amount {
			return nil, errors.New("数量超出"+fmt.Sprintf("%f", exchangeCoin.GetMaxVolume()))
		}
		if exchangeCoin.GetMinVolume() > 0 && exchangeCoin.GetMinVolume() > req.Amount {
			return nil, errors.New("数量不能低于"+fmt.Sprintf("%f", exchangeCoin.GetMinVolume()))
		}
	}
	baseWallet, err := l.assetRpc.FindWalletBySymbol(l.ctx, &asset.AssetReq{UserId: req.UserId, CoinName: baseSymbol})
	if err != nil {
		return nil, errors.New("no wallet")
	}
	exCoinWallet, err := l.assetRpc.FindWalletBySymbol(l.ctx, &asset.AssetReq{UserId: req.UserId, CoinName: coinSymbol})
	if err != nil {
		return nil, errors.New("no wallet")
	}
	if baseWallet.IsLock == 1 || exCoinWallet.IsLock == 1 {
		return nil, errors.New("wallet locked")
	}
	if req.Direction == model.DirectionMap.Value(model.SELL) && exchangeCoin.GetMinSellPrice() > 0 {
		if req.Price < exchangeCoin.GetMinSellPrice() || req.Type == model.TypeMap[model.MarketPrice] {
			return nil, errors.New("不能低于最低限价:" + fmt.Sprintf("%f", exchangeCoin.GetMinSellPrice()))
		}
	}
	if req.Direction == model.DirectionMap.Value(model.BUY) && exchangeCoin.GetMaxBuyPrice() > 0 {
		if req.Price > exchangeCoin.GetMaxBuyPrice() || req.Type == model.TypeMap[model.MarketPrice] {
			return nil, errors.New("不能低于最高限价:" + fmt.Sprintf("%f", exchangeCoin.GetMaxBuyPrice()))
		}
	}
	if req.Type == model.TypeMap[model.MarketPrice] {
		if req.Direction == model.DirectionMap.Value(model.BUY) && exchangeCoin.EnableMarketBuy == 0 {
			return nil, errors.New("不支持市价购买")
		} else if req.Direction == model.DirectionMap.Value(model.SELL) && exchangeCoin.EnableMarketSell == 0 {
			return nil, errors.New("不支持市价出售")
		}
	}
	count, err := l.orderDom.FindCurrentTradingCount(l.ctx, req.UserId, req.Symbol, req.Direction)
	if err != nil {
		return nil, err
	}
	if exchangeCoin.GetMaxTradingOrder() > 0 && count >= exchangeCoin.GetMaxTradingOrder() {
		return nil, errors.New("超过最大挂单数量 " + fmt.Sprintf("%d", exchangeCoin.GetMaxTradingOrder()))
	}
	// assemble order
	exchangeOrder := model.NewOrder()
	exchangeOrder.MemberId = req.UserId
	exchangeOrder.Symbol = req.Symbol
	exchangeOrder.BaseSymbol = baseSymbol
	exchangeOrder.CoinSymbol = coinSymbol
	typeCode := model.TypeMap.Code(req.Type)
	exchangeOrder.Type = typeCode
	directionCode := model.DirectionMap.Code(req.Direction)
	exchangeOrder.Direction = directionCode
	if exchangeOrder.Type == model.MarketPrice {
		exchangeOrder.Price = 0
	} else {
		exchangeOrder.Price = req.Price
	}
	exchangeOrder.UseDiscount = "0"
	exchangeOrder.Amount = req.Amount

	money, err := l.orderDom.AddOrder(l.ctx, exchangeOrder, exchangeCoin, baseWallet, exCoinWallet)
	if err != nil {
		return nil, errors.New("订单提交失败")
	}
	err = l.kafka.SendOrderAdd("add-exchange-order", req.UserId, exchangeOrder.OrderId, money, req.Symbol, exchangeOrder.Direction, baseSymbol, coinSymbol)
	if err != nil {
		return nil, errors.New("发消息失败")
	}
	return &order.AddOrderRes{OrderId: exchangeOrder.OrderId}, nil
}

// --- tests ---

func TestFindOrderHistory_success_and_error(t *testing.T) {
	fakeDomain := &fakeOrderDomain{
		history:      []*order.ExchangeOrder{{OrderId: "o1"}},
		historyTotal: 1,
	}
	logic := &testableExchangeOrderLogic{
		ctx:      context.Background(),
		orderDom: fakeDomain,
	}
	res, err := logic.FindOrderHistory(&order.OrderReq{Symbol: "S", Page: 1, PageSize: 10, UserId: 2})
	require.NoError(t, err)
	require.Equal(t, int64(1), res.Total)
	require.Len(t, res.List, 1)

	fakeDomainErr := &fakeOrderDomain{historyErr: errors.New("bad")}
	logicErr := &testableExchangeOrderLogic{
		ctx:      context.Background(),
		orderDom: fakeDomainErr,
	}
	_, err = logicErr.FindOrderHistory(&order.OrderReq{})
	require.Error(t, err)
}

func TestFindOrderCurrent_success_and_error(t *testing.T) {
	fakeDomain := &fakeOrderDomain{
		current:      []*order.ExchangeOrder{{OrderId: "c1"}},
		currentTotal: 2,
	}
	logic := &testableExchangeOrderLogic{
		ctx:      context.Background(),
		orderDom: fakeDomain,
	}
	res, err := logic.FindOrderCurrent(&order.OrderReq{Symbol: "S", Page: 1, PageSize: 10, UserId: 2})
	require.NoError(t, err)
	require.Equal(t, int64(2), res.Total)
	require.Len(t, res.List, 1)

	fakeDomainErr := &fakeOrderDomain{currentErr: errors.New("bad")}
	logicErr := &testableExchangeOrderLogic{
		ctx:      context.Background(),
		orderDom: fakeDomainErr,
	}
	_, err = logicErr.FindOrderCurrent(&order.OrderReq{})
	require.Error(t, err)
}

func TestCancelOrder_callsCancel(t *testing.T) {
	fakeDomain := &fakeOrderDomain{}
	logic := &testableExchangeOrderLogic{
		ctx:      context.Background(),
		orderDom: fakeDomain,
	}
	_, err := logic.CancelOrder(&order.OrderReq{OrderId: "OID"})
	require.NoError(t, err)
	require.True(t, fakeDomain.cancelCalled)
}

func TestAdd_validation_errors_and_success(t *testing.T) {
	// user forbidden
	memberForbidden := &fakeMemberRPC{memberRes: &member.MemberInfo{TransactionStatus: 0}}
	logicBadUser := &testableExchangeOrderLogic{
		ctx:       context.Background(),
		orderDom:  &fakeOrderDomain{},
		memberRpc: memberForbidden,
	}
	_, err := logicBadUser.Add(&order.OrderReq{UserId: 1})
	require.Error(t, err)
	require.Contains(t, err.Error(), "此用户已经被禁止交易")

	// invalid price
	memberOK := &fakeMemberRPC{memberRes: &member.MemberInfo{TransactionStatus: 1}}
	logicPrice := &testableExchangeOrderLogic{
		ctx:       context.Background(),
		orderDom:  &fakeOrderDomain{},
		memberRpc: memberOK,
	}
	_, err = logicPrice.Add(&order.OrderReq{
		UserId:    1,
		Type:      model.TypeMap[model.LimitPrice],
		Price:     0,
		Amount:    10,
		Symbol:    "SYMBOL",
		Direction: model.DirectionMap.Value(model.BUY),
	})
	require.Error(t, err)
	require.Equal(t, "限价模式下价格不能小于等于0", err.Error())

	// invalid amount
	_, err = logicPrice.Add(&order.OrderReq{
		UserId:    1,
		Type:      model.TypeMap[model.LimitPrice],
		Price:     1,
		Amount:    0,
		Symbol:    "SYMBOL",
		Direction: model.DirectionMap.Value(model.BUY),
	})
	require.Error(t, err)
	require.Equal(t, "数量不能小于等于0", err.Error())

	// unsupported symbol
	logicNoCoin := &testableExchangeOrderLogic{
		ctx:       context.Background(),
		orderDom:  &fakeOrderDomain{},
		memberRpc: memberOK,
		marketRpc: &fakeMarketRPC{symbolInfo: nil, errSymbol: errors.New("no")},
	}
	_, err = logicNoCoin.Add(&order.OrderReq{
		UserId:    1,
		Type:      model.TypeMap[model.LimitPrice],
		Price:     1,
		Amount:    1,
		Symbol:    "SYMBOL",
		Direction: model.DirectionMap.Value(model.BUY),
	})
	require.Error(t, err)
	require.Equal(t, "nonsupport coin", err.Error())

	// coin forbidden
	badExchangeCoin := &market.ExchangeCoin{Enable: 0, Exchangeable: 0}
	logicCoinForbidden := &testableExchangeOrderLogic{
		ctx:      context.Background(),
		orderDom: &fakeOrderDomain{},
		memberRpc: memberOK,
		marketRpc: &fakeMarketRPC{symbolInfo: badExchangeCoin, errSymbol: nil},
	}
	_, err = logicCoinForbidden.Add(&order.OrderReq{
		UserId:    1,
		Type:      model.TypeMap[model.LimitPrice],
		Price:     1,
		Amount:    1,
		Symbol:    "SYMBOL",
		Direction: model.DirectionMap.Value(model.BUY),
	})
	require.Error(t, err)
	require.Equal(t, "coin forbidden", err.Error())

	// wallet missing
	okExchangeCoin := &market.ExchangeCoin{Enable: 1, Exchangeable: 1, BaseSymbol: "BASE", CoinSymbol: "COIN"}
	logicWalletMissing := &testableExchangeOrderLogic{
		ctx:       context.Background(),
		orderDom:  &fakeOrderDomain{},
		memberRpc: memberOK,
		marketRpc: &fakeMarketRPC{symbolInfo: okExchangeCoin, errSymbol: nil, coinInfo: &market.Coin{}},
		assetRpc:  &fakeAssetRPC{baseWallet: nil, coinWallet: nil, errBase: errors.New("no"), errCoin: errors.New("no")},
	}
	_, err = logicWalletMissing.Add(&order.OrderReq{
		UserId:    1,
		Type:      model.TypeMap[model.LimitPrice],
		Price:     1,
		Amount:    1,
		Symbol:    "SYMBOL",
		Direction: model.DirectionMap.Value(model.BUY),
	})
	require.Error(t, err)
	require.Equal(t, "no wallet", err.Error())

	// wallet locked
	assetLocked := &fakeAssetRPC{
		baseWallet: &asset.MemberWallet{IsLock: 1},
		coinWallet: &asset.MemberWallet{IsLock: 0},
	}
	logicWalletLocked := &testableExchangeOrderLogic{
		ctx:       context.Background(),
		orderDom:  &fakeOrderDomain{},
		memberRpc: memberOK,
		marketRpc: &fakeMarketRPC{symbolInfo: okExchangeCoin, errSymbol: nil, coinInfo: &market.Coin{}},
		assetRpc:  assetLocked,
	}
	_, err = logicWalletLocked.Add(&order.OrderReq{
		UserId:    1,
		Type:      model.TypeMap[model.LimitPrice],
		Price:     1,
		Amount:    1,
		Symbol:    "SYMBOL",
		Direction: model.DirectionMap.Value(model.BUY),
	})
	require.Error(t, err)
	require.Equal(t, "wallet locked", err.Error())

	// success path
	fakeDomainSuccess := &fakeOrderDomain{
		addOrderMoney:    50,
		addOrderErr:      nil,
	}
	assetOK := &fakeAssetRPC{
		baseWallet: &asset.MemberWallet{IsLock: 0, Balance: 1000},
		coinWallet: &asset.MemberWallet{IsLock: 0, Balance: 1000},
	}
	kafka := &fakeKafkaDomain{}
	logicSuccess := &testableExchangeOrderLogic{
		ctx:       context.Background(),
		orderDom:  fakeDomainSuccess,
		memberRpc: memberOK,
		marketRpc: &fakeMarketRPC{symbolInfo: okExchangeCoin, errSymbol: nil, coinInfo: &market.Coin{}},
		assetRpc:  assetOK,
		kafka:     kafka,
	}
	res, err := logicSuccess.Add(&order.OrderReq{
		UserId:    1,
		Type:      model.TypeMap[model.LimitPrice],
		Price:     1,
		Amount:    1,
		Symbol:    "SYMBOL",
		Direction: model.DirectionMap.Value(model.BUY),
	})
	require.NoError(t, err)
	require.NotNil(t, res)
	require.True(t, kafka.sent)
}

func TestFindByOrderId_and_Cancel(t *testing.T) {
	expected := &model.ExchangeOrder{OrderId: "OID123"}
	fakeDomain := &fakeOrderDomain{
		findByOrderIdRet: expected,
	}
	logic := &testableExchangeOrderLogic{
		ctx:      context.Background(),
		orderDom: fakeDomain,
	}
	res, err := logic.FindByOrderId(&order.OrderReq{OrderId: "OID123"})
	require.NoError(t, err)
	require.Equal(t, "OID123", res.OrderId)

	_, err = logic.CancelOrder(&order.OrderReq{OrderId: "OID123"})
	require.NoError(t, err)
	require.True(t, fakeDomain.cancelCalled)
}
