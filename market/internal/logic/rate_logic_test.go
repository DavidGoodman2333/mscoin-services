package logic

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"grpc-common/market/types/rate"
	"market/internal/domain"
)

// helper that builds logic with real domain (no external dependencies)
func makeExchangeRateLogic() *ExchangeRateLogic {
	return &ExchangeRateLogic{
		ctx:                context.Background(),
		exchangeRateDomain: domain.NewExchangeRateDomain(),
	}
}

func TestUsdRate_knownUnits(t *testing.T) {
	logic := makeExchangeRateLogic()

	resCNY, err := logic.UsdRate(&rate.RateReq{Unit: "CNY"})
	require.NoError(t, err)
	require.Equal(t, float64(6.95), resCNY.Rate)

	resJPY, err := logic.UsdRate(&rate.RateReq{Unit: "JPY"})
	require.NoError(t, err)
	require.Equal(t, float64(136.23), resJPY.Rate)
}

func TestUsdRate_caseInsensitive(t *testing.T) {
	logic := makeExchangeRateLogic()

	resLower, err := logic.UsdRate(&rate.RateReq{Unit: "cny"})
	require.NoError(t, err)
	require.Equal(t, float64(6.95), resLower.Rate)

	resMixed, err := logic.UsdRate(&rate.RateReq{Unit: "jPy"})
	require.NoError(t, err)
	require.Equal(t, float64(136.23), resMixed.Rate)
}

func TestUsdRate_unknownUnit(t *testing.T) {
	logic := makeExchangeRateLogic()

	res, err := logic.UsdRate(&rate.RateReq{Unit: "EUR"})
	require.NoError(t, err)
	require.Equal(t, float64(0), res.Rate)
}
