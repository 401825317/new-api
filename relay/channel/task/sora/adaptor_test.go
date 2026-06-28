package sora

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
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

func TestParseTaskResultPrefersRawVideoURL(t *testing.T) {
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
	require.Equal(t, "https://vidgen.x.ai/raw.mp4", task.Url)
}

func TestParseTaskResultUsesOutputBeforeSignedURL(t *testing.T) {
	adaptor := &TaskAdaptor{}
	task, err := adaptor.ParseTaskResult([]byte(`{
		"id": "task_upstream",
		"status": "done",
		"output": ["https://vidgen.x.ai/output.mp4"],
		"result_url": "https://video.example.com/video/grok/task?exp=1&sig=old"
	}`))

	require.NoError(t, err)
	require.Equal(t, string(model.TaskStatusSuccess), task.Status)
	require.Equal(t, "https://vidgen.x.ai/output.mp4", task.Url)
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
