package common

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	rootcommon "github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// TestTaskDurationBounds guards the billing invariant that user-supplied
// video duration (a quota multiplier via OtherRatio "seconds") is bounded, so
// it can never overflow quota calculation into a negative charge.
func TestTaskDurationBounds(t *testing.T) {
	gin.SetMode(gin.TestMode)

	newContext := func(t *testing.T, body string) (*gin.Context, *RelayInfo) {
		request := httptest.NewRequest(http.MethodPost, "/v1/video/generations", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		context, _ := gin.CreateTestContext(httptest.NewRecorder())
		context.Request = request
		return context, &RelayInfo{TaskRelayInfo: &TaskRelayInfo{}}
	}

	tests := []struct {
		name    string
		body    string
		wantErr bool
	}{
		{
			name:    "huge duration is rejected",
			body:    `{"model":"sora-2","prompt":"a cat","duration":9999999999}`,
			wantErr: true,
		},
		{
			name:    "huge seconds string is rejected",
			body:    `{"model":"sora-2","prompt":"a cat","seconds":"9999999999"}`,
			wantErr: true,
		},
		{
			name:    "seconds string outside int range is rejected",
			body:    `{"model":"sora-2","prompt":"a cat","seconds":"184467440737095516160"}`,
			wantErr: true,
		},
		{
			name:    "negative duration is rejected",
			body:    `{"model":"sora-2","prompt":"a cat","duration":-8}`,
			wantErr: true,
		},
		{
			name:    "duration above max is rejected",
			body:    `{"model":"sora-2","prompt":"a cat","duration":3601}`,
			wantErr: true,
		},
		{
			name: "duration at max is accepted",
			body: `{"model":"sora-2","prompt":"a cat","duration":3600}`,
		},
		{
			name: "normal duration is accepted",
			body: `{"model":"sora-2","prompt":"a cat","seconds":"8"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name+" (multipart direct)", func(t *testing.T) {
			context, info := newContext(t, tt.body)
			taskErr := ValidateMultipartDirect(context, info)
			if tt.wantErr {
				require.NotNil(t, taskErr)
				require.Equal(t, "invalid_seconds", taskErr.Code)
			} else {
				require.Nil(t, taskErr)
			}
		})
		t.Run(tt.name+" (basic task request)", func(t *testing.T) {
			context, info := newContext(t, tt.body)
			taskErr := ValidateBasicTaskRequest(context, info, constant.TaskActionGenerate)
			if tt.wantErr {
				require.NotNil(t, taskErr)
				require.Equal(t, "invalid_seconds", taskErr.Code)
			} else {
				require.Nil(t, taskErr)
			}
		})
	}
}

func TestMultipartTaskDurationBounds(t *testing.T) {
	gin.SetMode(gin.TestMode)

	newContext := func(t *testing.T, seconds string) (*gin.Context, *RelayInfo) {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		require.NoError(t, writer.WriteField("model", "sora-2"))
		require.NoError(t, writer.WriteField("prompt", "a cat"))
		require.NoError(t, writer.WriteField("seconds", seconds))
		require.NoError(t, writer.Close())

		request := httptest.NewRequest(http.MethodPost, "/v1/video/generations", &body)
		request.Header.Set("Content-Type", writer.FormDataContentType())
		context, _ := gin.CreateTestContext(httptest.NewRecorder())
		context.Request = request
		storage, err := rootcommon.GetBodyStorage(context)
		require.NoError(t, err)
		context.Request.Body = io.NopCloser(storage)
		return context, &RelayInfo{TaskRelayInfo: &TaskRelayInfo{}}
	}

	t.Run("duration at max is accepted", func(t *testing.T) {
		context, info := newContext(t, "3600")
		require.Nil(t, ValidateBasicTaskRequest(context, info, constant.TaskActionGenerate))
	})

	t.Run("duration above max is rejected", func(t *testing.T) {
		context, info := newContext(t, "3601")
		taskErr := ValidateBasicTaskRequest(context, info, constant.TaskActionGenerate)
		require.NotNil(t, taskErr)
		require.Equal(t, "invalid_seconds", taskErr.Code)
	})

	t.Run("duration outside int range is rejected", func(t *testing.T) {
		context, info := newContext(t, "184467440737095516160")
		taskErr := ValidateBasicTaskRequest(context, info, constant.TaskActionGenerate)
		require.NotNil(t, taskErr)
		require.Equal(t, "invalid_seconds", taskErr.Code)
	})
}
