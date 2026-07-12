package sora

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
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

func TestBuildJSONTaskBodyNormalizesObjectInputReferenceBeforeParamOverride(t *testing.T) {
	body, ok, err := buildJSONTaskBody([]byte(`{
		"model": "grok-image-video",
		"prompt": "animate",
		"input_reference": {
			"image_url": "data:image/png;base64,xyz"
		}
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
				},
			},
		},
	})

	require.NoError(t, err)
	require.True(t, ok)

	var got map[string]any
	require.NoError(t, json.Unmarshal(body, &got))
	require.Equal(t, "grok-imagine-video-1.5", got["model"])
	require.NotContains(t, got, "input_reference")

	image, ok := got["image"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "data:image/png;base64,xyz", image["url"])
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
