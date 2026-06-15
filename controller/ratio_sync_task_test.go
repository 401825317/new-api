package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestParseRatioConfigResponse(t *testing.T) {
	raw := []byte(`{"model_ratio":{"gpt-test":1.5}}`)
	parsed, err := parseRatioConfigResponse(raw)
	require.NoError(t, err)
	require.Equal(t, 1.5, parsed["model_ratio"].(map[string]any)["gpt-test"])

	wrapped := []byte(`{"success":true,"data":{"model_price":{"image-test":0.04}}}`)
	parsed, err = parseRatioConfigResponse(wrapped)
	require.NoError(t, err)
	require.Equal(t, 0.04, parsed["model_price"].(map[string]any)["image-test"])
}

func TestBuildMissingPricingOptionUpdatesDoesNotOverrideExisting(t *testing.T) {
	localData := map[string]any{
		"model_ratio": map[string]any{
			"existing-model": 1.0,
		},
		"completion_ratio": map[string]any{
			"existing-model": 2.0,
		},
	}
	sources := []modelPricingSyncSourceResult{
		{
			Source: modelPricingSyncSource{Name: "test"},
			Data: map[string]any{
				"model_ratio": map[string]any{
					"existing-model": 9.0,
					"new-model":      3.0,
				},
				"completion_ratio": map[string]any{
					"new-model": 4.0,
				},
			},
		},
	}

	updates, added, err := buildMissingPricingOptionUpdates(localData, sources)
	require.NoError(t, err)
	require.Equal(t, 2, added)

	var modelRatio map[string]float64
	require.NoError(t, common.Unmarshal([]byte(updates["ModelRatio"]), &modelRatio))
	require.Equal(t, 1.0, modelRatio["existing-model"])
	require.Equal(t, 3.0, modelRatio["new-model"])

	var completionRatio map[string]float64
	require.NoError(t, common.Unmarshal([]byte(updates["CompletionRatio"]), &completionRatio))
	require.Equal(t, 2.0, completionRatio["existing-model"])
	require.Equal(t, 4.0, completionRatio["new-model"])
}
