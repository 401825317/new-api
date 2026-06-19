package controller

import (
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestClawXRuntimePayloadDefaultsToSmartRouting(t *testing.T) {
	t.Setenv("CLAWX_DEFAULT_MODEL", "")
	t.Setenv("CLAWX_MODEL_FAMILIES", "")
	t.Setenv("CLAWX_FALLBACK_MODELS", "")

	payload := clawXRuntimePayload()

	assert.Equal(t, "smart-latest", payload["defaultModel"])

	families, ok := payload["modelFamilies"].([]gin.H)
	if !ok {
		t.Fatalf("unexpected modelFamilies type: %T", payload["modelFamilies"])
	}
	if assert.NotEmpty(t, families) {
		assert.Equal(t, "smart-latest", families[0]["id"])
		assert.Equal(t, "智能路由", families[0]["name"])
	}
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
