package relay

import (
	"math"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecalcQuotaFromRatiosRejectsInvalidAdjustments(t *testing.T) {
	priceData := types.PriceData{Quota: 200}
	priceData.AddOtherRatio("seconds", 2)
	info := &relaycommon.RelayInfo{PriceData: priceData}

	quota, ok := recalcQuotaFromRatios(info, map[string]float64{
		"negative": -1,
		"nan":      math.NaN(),
		"inf":      math.Inf(1),
	})

	assert.False(t, ok)
	assert.Zero(t, quota)
	assert.Equal(t, map[string]float64{"seconds": 2}, info.PriceData.OtherRatios())
	assert.Nil(t, info.QuotaClamp)
}

func TestRecalcQuotaFromRatiosFiltersInvalidValues(t *testing.T) {
	priceData := types.PriceData{Quota: 200}
	priceData.AddOtherRatio("seconds", 2)
	info := &relaycommon.RelayInfo{PriceData: priceData}

	quota, ok := recalcQuotaFromRatios(info, map[string]float64{
		"duration": 3,
		"negative": -1,
		"nan":      math.NaN(),
		"inf":      math.Inf(1),
	})

	require.True(t, ok)
	assert.Equal(t, 300, quota)
	assert.Nil(t, info.QuotaClamp)
}
