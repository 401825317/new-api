package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/setting/clawx_client_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestClawXRuntimePayloadDefaultsToSmartRouting(t *testing.T) {
	t.Setenv("CLAWX_DEFAULT_MODEL", "")
	t.Setenv("CLAWX_MODEL_FAMILIES", "")
	t.Setenv("CLAWX_FALLBACK_MODELS", "")

	payload := clawXRuntimePayload()

	assert.Equal(t, "smart-latest", payload["defaultModel"])
	assert.Contains(t, payload["fallbackModels"], "qwen-latest")

	families, ok := payload["modelFamilies"].([]gin.H)
	if !ok {
		t.Fatalf("unexpected modelFamilies type: %T", payload["modelFamilies"])
	}
	if assert.NotEmpty(t, families) {
		assert.Equal(t, "smart-latest", families[0]["id"])
		assert.Equal(t, "智能路由", families[0]["name"])
	}
}

func TestClawXRuntimePayloadDoesNotInventFallbackModelsWhenConfiguredFamiliesHaveNoSafeMatch(t *testing.T) {
	t.Setenv("CLAWX_DEFAULT_MODEL", "custom-primary")
	t.Setenv("CLAWX_MODEL_FAMILIES", "custom-primary:Custom")
	t.Setenv("CLAWX_FALLBACK_MODELS", "")

	payload := clawXRuntimePayload()
	assert.Empty(t, payload["fallbackModels"])
}

func TestClawXModelFamiliesCanBeOverridden(t *testing.T) {
	t.Setenv("CLAWX_MODEL_FAMILIES", "qwen-latest:通义千问最新版")

	families := clawXModelFamilies()

	if assert.Len(t, families, 1) {
		assert.Equal(t, "qwen-latest", families[0]["id"])
		assert.Equal(t, "通义千问最新版", families[0]["name"])
	}
}

func TestClawXRuntimePayloadKeepsExplicitDefaultModel(t *testing.T) {
	t.Setenv("CLAWX_DEFAULT_MODEL", "qwen-latest")
	t.Setenv("CLAWX_MODEL_FAMILIES", "")
	t.Setenv("CLAWX_FALLBACK_MODELS", "")

	payload := clawXRuntimePayload()

	assert.Equal(t, "qwen-latest", payload["defaultModel"])
}

func TestClawXRuntimePayloadUsesConfiguredClientFallbackModels(t *testing.T) {
	original := clawx_client_setting.GetClientSetting().ModelOptions
	clawx_client_setting.GetClientSetting().ModelOptions = `{"text":{"defaultModel":"smart-latest","fallbackModels":["qwen-latest"],"models":[{"id":"smart-latest","enabled":true},{"id":"qwen-latest","enabled":true}]}}`
	t.Cleanup(func() { clawx_client_setting.GetClientSetting().ModelOptions = original })
	t.Setenv("CLAWX_DEFAULT_MODEL", "")
	t.Setenv("CLAWX_MODEL_FAMILIES", "")
	t.Setenv("CLAWX_FALLBACK_MODELS", "")

	payload := clawXRuntimePayload()
	assert.Equal(t, []string{"qwen-latest"}, payload["fallbackModels"])
}

func TestClawXRuntimePayloadFiltersPrivateAndDuplicateEnvFallbacks(t *testing.T) {
	t.Setenv("CLAWX_DEFAULT_MODEL", "smart-latest")
	t.Setenv("CLAWX_FALLBACK_MODELS", "smart-latest,qwen-latest,qwen-latest,uclaw-artifact-v1,deepseek-latest")

	payload := clawXRuntimePayload()
	assert.Equal(t, []string{"qwen-latest", "deepseek-latest"}, payload["fallbackModels"])
}
