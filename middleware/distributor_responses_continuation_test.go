package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func continuationTestContext(path string, body string) *gin.Context {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = request
	return ctx
}

func TestPreferredResponsesContinuationChannelType(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("DeepSeek reasoning stays on a DeepSeek channel", func(t *testing.T) {
		ctx := continuationTestContext("/v1/responses", `{
			"model":"smart-latest",
			"input":[
				{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]},
				{"type":"reasoning","content":[{"type":"reasoning_text","text":"deepseek state"}]}
			]
		}`)
		require.Equal(t, constant.ChannelTypeDeepSeek, preferredResponsesContinuationChannelType(ctx))
	})

	t.Run("encrypted foreign state does not pin a DeepSeek channel", func(t *testing.T) {
		ctx := continuationTestContext("/v1/responses", `{
			"model":"smart-latest",
			"input":[
				{"type":"reasoning","encrypted_content":"opaque-openai"},
				{"type":"reasoning","content":[{"type":"reasoning_text","text":"stale"}]}
			]
		}`)
		require.Zero(t, preferredResponsesContinuationChannelType(ctx))
	})

	t.Run("nested encrypted foreign state does not pin a DeepSeek channel", func(t *testing.T) {
		ctx := continuationTestContext("/v1/responses", `{
			"model":"smart-latest",
			"input":[
				{"type":"message","role":"assistant","content":[
					{"type":"output_text","text":"visible answer"},
					{"type":"reasoning","encrypted_content":"opaque-openai"}
				]},
				{"type":"reasoning","content":[{"type":"reasoning_text","text":"stale"}]}
			]
		}`)
		require.Zero(t, preferredResponsesContinuationChannelType(ctx))
	})

	t.Run("non-Responses requests retain normal priority", func(t *testing.T) {
		ctx := continuationTestContext("/v1/chat/completions", `{"model":"smart-latest"}`)
		require.Zero(t, preferredResponsesContinuationChannelType(ctx))
	})
}
