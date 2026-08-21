package sora

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
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
			"duration": 10,
		},
	}, "grok-imagine-1.5-video-ext")

	require.NoError(t, err)
	require.Equal(t, "grok-imagine-1.5-video-ext", payload.Model)
	require.Equal(t, "720p", payload.Quality)
	require.Equal(t, 10, payload.Duration)
}

func TestBuildApimartPayloadOnlyAllowsSixOrTenSecondDuration(t *testing.T) {
	testCases := []struct {
		name    string
		req     relaycommon.TaskSubmitReq
		want    int
		wantErr string
	}{
		{
			name: "defaults to six seconds",
			req:  relaycommon.TaskSubmitReq{Prompt: "animate it"},
			want: 6,
		},
		{
			name: "allows explicit six seconds",
			req:  relaycommon.TaskSubmitReq{Prompt: "animate it", Duration: 6},
			want: 6,
		},
		{
			name: "allows explicit ten seconds",
			req:  relaycommon.TaskSubmitReq{Prompt: "animate it", Seconds: "10"},
			want: 10,
		},
		{
			name:    "rejects unsupported duration",
			req:     relaycommon.TaskSubmitReq{Prompt: "animate it", Duration: 8},
			wantErr: "duration must be 6 or 10 seconds",
		},
		{
			name: "rejects metadata duration override",
			req: relaycommon.TaskSubmitReq{
				Prompt: "animate it",
				Metadata: map[string]any{
					"duration": 12,
				},
			},
			wantErr: "duration must be 6 or 10 seconds",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			payload, err := buildApimartPayload(testCase.req, "grok-imagine-1.5-video-apimart")
			if testCase.wantErr != "" {
				require.EqualError(t, err, testCase.wantErr)
				return
			}

			require.NoError(t, err)
			require.Equal(t, testCase.want, payload.Duration)
		})
	}
}

func TestApimartBase64ImageUploadOnlyOccursInBuildRequestBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	imageData := apimartTestPNG()
	dataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(imageData)
	uploadCalls := 0
	uploadServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uploadCalls++
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "image/png", r.Header.Get("Content-Type"))
		require.Empty(t, r.Header.Get("x-input-media-key"))
		got, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.Equal(t, imageData, got)
		_, _ = w.Write([]byte(`{"url":"https://media.example.com/input.png"}`))
	}))
	defer uploadServer.Close()
	configureApimartInputMedia(t, "18", uploadServer.URL)

	context := apimartTestContext(t, relaycommon.TaskSubmitReq{
		Model:     "grok-image-video",
		Prompt:    "animate the reference image",
		Seconds:   "6",
		ImageURLs: []string{dataURL},
	})
	info := apimartTestRelayInfo(18)
	adaptor := &TaskAdaptor{}

	taskErr := adaptor.ValidateRequestAndSetAction(context, info)
	require.Nil(t, taskErr)
	require.Equal(t, 0, uploadCalls)
	require.Equal(t, map[string]float64{"seconds": 6, "quality": 1}, adaptor.EstimateBilling(context, info))
	require.Equal(t, 0, uploadCalls)

	firstBody, err := adaptor.BuildRequestBody(context, info)
	require.NoError(t, err)
	firstPayload := decodeApimartRequestPayload(t, firstBody)
	require.Equal(t, []string{"https://media.example.com/input.png"}, firstPayload.ImageURLs)
	require.Equal(t, 1, uploadCalls)

	secondBody, err := adaptor.BuildRequestBody(context, info)
	require.NoError(t, err)
	secondPayload := decodeApimartRequestPayload(t, secondBody)
	require.Equal(t, firstPayload, secondPayload)
	require.Equal(t, 1, uploadCalls)
}

func TestApimartBase64ImageRejectsNonAllowlistChannelWithoutUpload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	uploadCalls := 0
	uploadServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uploadCalls++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer uploadServer.Close()
	configureApimartInputMedia(t, "18", uploadServer.URL)

	context := apimartTestContext(t, relaycommon.TaskSubmitReq{
		Model:     "grok-image-video",
		Prompt:    "animate the reference image",
		Seconds:   "6",
		ImageURLs: []string{"data:image/png;base64," + base64.StdEncoding.EncodeToString(apimartTestPNG())},
	})
	taskErr := (&TaskAdaptor{}).ValidateRequestAndSetAction(context, apimartTestRelayInfo(19))

	require.NotNil(t, taskErr)
	require.Contains(t, taskErr.Message, "not enabled for this channel")
	require.Equal(t, 0, uploadCalls)
}

func TestBuildApimartRequestBodyLeavesURLsAndTextOnlyRequestsUntouched(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv(apimartBase64ImageChannelIDsEnv, "")
	t.Setenv(apimartInputMediaUploadURLEnv, "")
	adaptor := &TaskAdaptor{}

	t.Run("existing HTTP URL", func(t *testing.T) {
		context := apimartTestContext(t, relaycommon.TaskSubmitReq{
			Prompt:    "animate the reference image",
			ImageURLs: []string{"https://client.example.com/reference.png"},
		})
		context.Set("task_request", relaycommon.TaskSubmitReq{
			Prompt:    "animate the reference image",
			ImageURLs: []string{"https://client.example.com/reference.png"},
		})

		body, err := adaptor.BuildRequestBody(context, apimartTestRelayInfo(19))
		require.NoError(t, err)
		payload := decodeApimartRequestPayload(t, body)
		require.Equal(t, []string{"https://client.example.com/reference.png"}, payload.ImageURLs)
	})

	t.Run("text to video", func(t *testing.T) {
		context := apimartTestContext(t, relaycommon.TaskSubmitReq{Prompt: "make a sunset video"})
		context.Set("task_request", relaycommon.TaskSubmitReq{Prompt: "make a sunset video"})

		body, err := adaptor.BuildRequestBody(context, apimartTestRelayInfo(19))
		require.NoError(t, err)
		payload := decodeApimartRequestPayload(t, body)
		require.Empty(t, payload.ImageURLs)
	})
}

func TestApimartBase64ImageValidationAndUploadFailuresAreSafe(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("invalid data URL", func(t *testing.T) {
		err := validateApimartBase64ImageInputs([]string{"data:image/png;base64,not-base64!"}, 18)
		require.Error(t, err)
		require.Contains(t, err.Error(), "valid base64")
	})

	t.Run("upload failure", func(t *testing.T) {
		imageData := apimartTestPNG()
		dataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(imageData)
		uploadServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte("upstream body must not be exposed"))
		}))
		defer uploadServer.Close()
		configureApimartInputMedia(t, "18", uploadServer.URL)

		context := apimartTestContext(t, relaycommon.TaskSubmitReq{Prompt: "animate", ImageURLs: []string{dataURL}})
		context.Set("task_request", relaycommon.TaskSubmitReq{Prompt: "animate", ImageURLs: []string{dataURL}})
		_, err := (&TaskAdaptor{}).BuildRequestBody(context, apimartTestRelayInfo(18))

		require.EqualError(t, err, "APIMart image upload failed with status 502")
		require.NotContains(t, err.Error(), dataURL)
	})
}

func TestBuildApimartRequestBodyUploadsEveryInlineImageAndPreservesURLs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	imageData := apimartTestPNG()
	urlSafeImageData := append(append([]byte(nil), imageData...), 0xff, 0xff, 0xff)
	paddedURLSafe := base64.URLEncoding.EncodeToString(urlSafeImageData)
	rawURLSafe := base64.RawURLEncoding.EncodeToString(urlSafeImageData)
	require.True(t, strings.ContainsAny(paddedURLSafe, "-_"))
	require.True(t, strings.ContainsAny(rawURLSafe, "-_"))
	uploadCalls := 0
	uploadServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uploadCalls++
		_, _ = io.ReadAll(r.Body)
		_, _ = fmt.Fprintf(w, `{"url":"https://media.example.com/input-%d.png"}`, uploadCalls)
	}))
	defer uploadServer.Close()
	configureApimartInputMedia(t, "18", uploadServer.URL)

	context := apimartTestContext(t, relaycommon.TaskSubmitReq{
		Prompt: "animate several references",
		ImageURLs: []string{
			base64.StdEncoding.EncodeToString(imageData),
			"https://client.example.com/already-public.png",
			paddedURLSafe,
			rawURLSafe,
			"data:image/png;base64," + base64.StdEncoding.EncodeToString(imageData),
		},
	})
	context.Set("task_request", relaycommon.TaskSubmitReq{
		Prompt: "animate several references",
		ImageURLs: []string{
			base64.StdEncoding.EncodeToString(imageData),
			"https://client.example.com/already-public.png",
			paddedURLSafe,
			rawURLSafe,
			"data:image/png;base64," + base64.StdEncoding.EncodeToString(imageData),
		},
	})

	body, err := (&TaskAdaptor{}).BuildRequestBody(context, apimartTestRelayInfo(18))
	require.NoError(t, err)
	payload := decodeApimartRequestPayload(t, body)
	require.Equal(t, []string{
		"https://media.example.com/input-1.png",
		"https://client.example.com/already-public.png",
		"https://media.example.com/input-2.png",
		"https://media.example.com/input-3.png",
		"https://media.example.com/input-4.png",
	}, payload.ImageURLs)
	require.Equal(t, 4, uploadCalls)
}

func apimartTestContext(t *testing.T, req relaycommon.TaskSubmitReq) *gin.Context {
	t.Helper()
	body, err := common.Marshal(req)
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodPost, "http://zz-cn.test/v1/videos", bytes.NewReader(body))
	context.Request.Header.Set("Content-Type", "application/json")
	return context
}

func apimartTestRelayInfo(channelID int) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		OriginModelName: "grok-image-video",
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:         channelID,
			ChannelBaseUrl:    "https://api.apimart.ai",
			UpstreamModelName: "grok-imagine-1.5-video-apimart",
		},
	}
}

func configureApimartInputMedia(t *testing.T, channelIDs string, uploadURL string) {
	t.Helper()
	t.Setenv(apimartBase64ImageChannelIDsEnv, channelIDs)
	t.Setenv(apimartInputMediaUploadURLEnv, uploadURL)
}

func apimartTestPNG() []byte {
	return []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
}

func decodeApimartRequestPayload(t *testing.T, body io.Reader) apimartRequestPayload {
	t.Helper()
	data, err := io.ReadAll(body)
	require.NoError(t, err)
	var payload apimartRequestPayload
	require.NoError(t, common.Unmarshal(data, &payload))
	return payload
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
