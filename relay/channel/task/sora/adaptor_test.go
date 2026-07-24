package sora

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestParseTaskResultDoneWithVideoURL(t *testing.T) {
	adaptor := &TaskAdaptor{}
	task, err := adaptor.ParseTaskResult([]byte(`{
		"id": "task_upstream",
		"model": "grok-image-video",
		"status": "done",
		"progress": 100,
		"video": {
			"url": "https://example.com/video.mp4",
			"duration": 4
		}
	}`))

	require.NoError(t, err)
	require.Equal(t, string(model.TaskStatusSuccess), task.Status)
	require.Equal(t, "https://example.com/video.mp4", task.Url)
}

func TestParseTaskResultPrefersDownstreamProxyURLOverRawVideoURL(t *testing.T) {
	adaptor := &TaskAdaptor{}
	task, err := adaptor.ParseTaskResult([]byte(`{
		"id": "task_upstream",
		"status": "done",
		"video_url": "https://vidgen.x.ai/raw.mp4",
		"result_url": "https://video.example.com/video/grok/task?exp=1&sig=old",
		"video": {
			"url": "https://video.example.com/video/grok/task?exp=1&sig=old"
		}
	}`))

	require.NoError(t, err)
	require.Equal(t, string(model.TaskStatusSuccess), task.Status)
	require.Equal(t, "https://video.example.com/video/grok/task?exp=1&sig=old", task.Url)
}

func TestParseTaskResultPrefersDownstreamProxyURLOverRawOutput(t *testing.T) {
	adaptor := &TaskAdaptor{}
	task, err := adaptor.ParseTaskResult([]byte(`{
		"id": "task_upstream",
		"status": "done",
		"output": ["https://vidgen.x.ai/output.mp4"],
		"result_url": "https://video.example.com/video/grok/task?exp=1&sig=old"
	}`))

	require.NoError(t, err)
	require.Equal(t, string(model.TaskStatusSuccess), task.Status)
	require.Equal(t, "https://video.example.com/video/grok/task?exp=1&sig=old", task.Url)
}

func TestParseTaskResultCompletedWithResultURL(t *testing.T) {
	adaptor := &TaskAdaptor{}
	task, err := adaptor.ParseTaskResult([]byte(`{
		"id": "task_upstream",
		"status": "completed",
		"result_url": "https://example.com/result.mp4"
	}`))

	require.NoError(t, err)
	require.Equal(t, string(model.TaskStatusSuccess), task.Status)
	require.Equal(t, "https://example.com/result.mp4", task.Url)
}

func TestParseTaskResultCompletedWithNestedResultURL(t *testing.T) {
	adaptor := &TaskAdaptor{}
	task, err := adaptor.ParseTaskResult([]byte(`{
		"id": "task_public",
		"task_id": "task_public",
		"status": "completed",
		"result_url": "https://zz-cn.lingzhiwuxian.com/v1/videos/task_public/content",
		"data": {
			"video": {
				"url": "https://example.com/result.mp4"
			}
		}
	}`))

	require.NoError(t, err)
	require.Equal(t, string(model.TaskStatusSuccess), task.Status)
	require.Equal(t, "https://example.com/result.mp4", task.Url)
}

func TestParseTaskResultCompletedWithNestedDataVideoURL(t *testing.T) {
	adaptor := &TaskAdaptor{}
	task, err := adaptor.ParseTaskResult([]byte(`{
		"id": "task_public",
		"task_id": "task_public",
		"status": "completed",
		"result_url": "https://zz-cn.lingzhiwuxian.com/v1/videos/task_public/content",
		"data": {
			"video_url": "https://example.com/result.mp4"
		}
	}`))

	require.NoError(t, err)
	require.Equal(t, string(model.TaskStatusSuccess), task.Status)
	require.Equal(t, "https://example.com/result.mp4", task.Url)
}

func TestBuildRequestURLUsesApimartGenerationEndpoint(t *testing.T) {
	adaptor := &TaskAdaptor{baseURL: "https://api.apimart.ai"}
	got, err := adaptor.BuildRequestURL(&relaycommon.RelayInfo{
		TaskRelayInfo: &relaycommon.TaskRelayInfo{},
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl: "https://api.apimart.ai",
		},
		OriginModelName: "grok-image-video",
	})

	require.NoError(t, err)
	require.Equal(t, "https://api.apimart.ai/v1/videos/generations", got)
}

func TestBuildApimartPayloadNormalizesOpenAIVideoFields(t *testing.T) {
	payload, err := buildApimartPayload(relaycommon.TaskSubmitReq{
		Prompt:         "animate it",
		Size:           "1280x720",
		Seconds:        "10",
		Quality:        "720p",
		ImageURLs:      []string{"https://example.com/a.png"},
		Image:          "https://example.com/a.png",
		InputReference: "https://example.com/b.png",
	}, "grok-imagine-1.5-video-apimart")

	require.NoError(t, err)
	require.Equal(t, "grok-imagine-1.5-video-apimart", payload.Model)
	require.Equal(t, "animate it", payload.Prompt)
	require.Equal(t, "16:9", payload.Size)
	require.Equal(t, 10, payload.Duration)
	require.Equal(t, "720p", payload.Quality)
	require.Equal(t, []string{"https://example.com/a.png", "https://example.com/b.png"}, payload.ImageURLs)
}

func TestBuildApimartPayloadMetadataCannotOverrideModel(t *testing.T) {
	payload, err := buildApimartPayload(relaycommon.TaskSubmitReq{
		Prompt: "animate it",
		Metadata: map[string]any{
			"model":    "wrong-model",
			"quality":  "720p",
			"duration": 12,
		},
	}, "grok-imagine-1.5-video-ext")

	require.NoError(t, err)
	require.Equal(t, "grok-imagine-1.5-video-ext", payload.Model)
	require.Equal(t, "720p", payload.Quality)
	require.Equal(t, 12, payload.Duration)
}

func TestDoResponseParsesApimartArrayTaskIDAndReturnsPublicTaskID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	resp := &http.Response{
		Body: io.NopCloser(strings.NewReader(`{"code":200,"data":[{"task_id":"task_upstream","status":"submitted"}]}`)),
	}

	taskID, taskData, taskErr := (&TaskAdaptor{}).DoResponse(context, resp, &relaycommon.RelayInfo{
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{PublicTaskID: "task_public"},
		OriginModelName: "grok-image-video",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl:    "https://api.apimart.ai",
			UpstreamModelName: "grok-imagine-1.5-video-apimart",
		},
	})

	require.Nil(t, taskErr)
	require.Equal(t, "task_upstream", taskID)
	require.JSONEq(t, `{"code":200,"data":[{"task_id":"task_upstream","status":"submitted"}]}`, string(taskData))
	status, body, ok := taskcommon.GetTaskSubmitResponse(context)
	require.True(t, ok)
	require.Equal(t, http.StatusOK, status)
	data, err := json.Marshal(body)
	require.NoError(t, err)
	require.Contains(t, string(data), `"id":"task_public"`)
	require.Contains(t, string(data), `"task_id":"task_public"`)
	require.Contains(t, string(data), `"model":"grok-image-video"`)
}

func TestFetchTaskUsesApimartStatusEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/tasks/task_upstream", r.URL.Path)
		require.Equal(t, "zh", r.URL.Query().Get("language"))
		require.Equal(t, "Bearer sk-test", r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{"code":200,"data":{"id":"task_upstream","status":"pending"}}`))
	}))
	defer server.Close()

	resp, err := (&TaskAdaptor{}).FetchTask(server.URL, "sk-test", map[string]any{
		"task_id":             "task_upstream",
		"upstream_model_name": "grok-imagine-1.5-video-apimart",
	}, "")

	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NoError(t, resp.Body.Close())
}

func TestParseTaskResultApimartCompletedWithVideosURLArray(t *testing.T) {
	task, err := (&TaskAdaptor{}).ParseTaskResult([]byte(`{
		"code": 200,
		"data": {
			"id": "task_upstream",
			"status": "completed",
			"progress": 100,
			"result": {
				"videos": [
					{"url": ["https://upload.apimart.ai/f/video/result.mp4"]}
				]
			}
		}
	}`))

	require.NoError(t, err)
	require.Equal(t, string(model.TaskStatusSuccess), task.Status)
	require.Equal(t, "100%", task.Progress)
	require.Equal(t, "https://upload.apimart.ai/f/video/result.mp4", task.Url)
}

func TestParseTaskResultApimartFailureReason(t *testing.T) {
	task, err := (&TaskAdaptor{}).ParseTaskResult([]byte(`{
		"code": 200,
		"data": {
			"id": "task_upstream",
			"status": "failed",
			"error": {"message": "content rejected"}
		}
	}`))

	require.NoError(t, err)
	require.Equal(t, string(model.TaskStatusFailure), task.Status)
	require.Equal(t, "content rejected", task.Reason)
}

func TestParseTaskResultFailureReason(t *testing.T) {
	adaptor := &TaskAdaptor{}
	task, err := adaptor.ParseTaskResult([]byte(`{
		"id": "task_upstream",
		"status": "failed",
		"error": {
			"message": "content rejected"
		}
	}`))

	require.NoError(t, err)
	require.Equal(t, string(model.TaskStatusFailure), task.Status)
	require.Equal(t, "content rejected", task.Reason)
}

func TestParseTaskResultFailureReasonStringError(t *testing.T) {
	adaptor := &TaskAdaptor{}
	task, err := adaptor.ParseTaskResult([]byte(`{
		"id": "task_upstream",
		"status": "failed",
		"error": "upstream rejected the request"
	}`))

	require.NoError(t, err)
	require.Equal(t, string(model.TaskStatusFailure), task.Status)
	require.Equal(t, "upstream rejected the request", task.Reason)
}

func TestBuildJSONTaskBodyAppliesParamOverrideAfterModelMapping(t *testing.T) {
	body, ok, err := buildJSONTaskBody([]byte(`{
		"model": "grok-video-1.5",
		"prompt": "animate",
		"input_reference": "data:image/jpeg;base64,abc",
		"seconds": 10
	}`), &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "grok-imagine-video-1.5",
			ParamOverride: map[string]any{
				"operations": []any{
					map[string]any{
						"mode": "move",
						"from": "input_reference",
						"to":   "image.url",
					},
					map[string]any{
						"mode": "move",
						"from": "seconds",
						"to":   "duration",
					},
				},
			},
		},
	})

	require.NoError(t, err)
	require.True(t, ok)

	var got map[string]any
	require.NoError(t, json.Unmarshal(body, &got))
	require.Equal(t, "grok-imagine-video-1.5", got["model"])
	require.Equal(t, float64(10), got["duration"])
	require.NotContains(t, got, "input_reference")
	require.NotContains(t, got, "seconds")

	image, ok := got["image"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "data:image/jpeg;base64,abc", image["url"])
}

func TestBuildJSONTaskBodySkipsParamOverrideWhenJSONInvalid(t *testing.T) {
	body, ok, err := buildJSONTaskBody([]byte(`not-json`), &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "grok-imagine-video-1.5",
			ParamOverride: map[string]any{
				"model": "should-not-apply",
			},
		},
	})

	require.NoError(t, err)
	require.False(t, ok)
	require.Nil(t, body)
}
