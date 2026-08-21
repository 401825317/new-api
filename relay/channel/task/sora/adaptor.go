package sora

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	taskcommon "github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	"github.com/tidwall/sjson"
)

// ============================
// Request / Response structures
// ============================

type ContentItem struct {
	Type     string    `json:"type"`                // "text" or "image_url"
	Text     string    `json:"text,omitempty"`      // for text type
	ImageURL *ImageURL `json:"image_url,omitempty"` // for image_url type
}

type ImageURL struct {
	URL string `json:"url"`
}

type responseTask struct {
	ID                 string   `json:"id"`
	TaskID             string   `json:"task_id,omitempty"` //兼容旧接口
	Object             string   `json:"object"`
	Model              string   `json:"model"`
	Status             string   `json:"status"`
	Progress           int      `json:"progress"`
	CreatedAt          int64    `json:"created_at"`
	CompletedAt        int64    `json:"completed_at,omitempty"`
	ExpiresAt          int64    `json:"expires_at,omitempty"`
	Seconds            string   `json:"seconds,omitempty"`
	Size               string   `json:"size,omitempty"`
	RemixedFromVideoID string   `json:"remixed_from_video_id,omitempty"`
	ResultURL          string   `json:"result_url,omitempty"`
	URL                string   `json:"url,omitempty"`
	VideoURL           string   `json:"video_url,omitempty"`
	Output             []string `json:"output,omitempty"`
	Video              *struct {
		URL string `json:"url,omitempty"`
	} `json:"video,omitempty"`
	Error *taskError `json:"error,omitempty"`
}

type apimartSubmitResponse struct {
	Code  int              `json:"code"`
	Data  []apimartTask    `json:"data"`
	Error *apimartAPIError `json:"error,omitempty"`
}

type apimartTask struct {
	TaskID string `json:"task_id"`
	Status string `json:"status"`
}

type apimartAPIError struct {
	Code    any    `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
	Type    string `json:"type,omitempty"`
}

type apimartRequestPayload struct {
	Model      string   `json:"model"`
	Prompt     string   `json:"prompt"`
	Size       string   `json:"size,omitempty"`
	Duration   int      `json:"duration,omitempty"`
	Resolution string   `json:"resolution,omitempty"`
	ImageURLs  []string `json:"image_urls,omitempty"`
}

const (
	apimartBase64ImageChannelIDsEnv = "APIMART_BASE64_IMAGE_CHANNEL_IDS"
	apimartInputMediaUploadURLEnv   = "APIMART_INPUT_MEDIA_UPLOAD_URL"

	apimartInputMediaMaxBytes      = 20 * 1024 * 1024
	apimartInputMediaUploadTimeout = 15 * time.Second
	apimartRequestBodyContextKey   = "apimart_prepared_request_body"
)

type apimartInputMediaUploadConfig struct {
	url string
}

type apimartInlineImage struct {
	data     []byte
	mimeType string
}

type apimartPreparedRequestBody struct {
	channelID int
	body      []byte
}

type taskError struct {
	Message string `json:"message"`
	Code    string `json:"code"`
}

func (e *taskError) UnmarshalJSON(data []byte) error {
	var msg string
	if err := json.Unmarshal(data, &msg); err == nil {
		e.Message = msg
		return nil
	}

	type alias taskError
	return json.Unmarshal(data, (*alias)(e))
}

// ============================
// Adaptor implementation
// ============================

type TaskAdaptor struct {
	taskcommon.BaseBilling
	ChannelType int
	apiKey      string
	baseURL     string
}

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	a.ChannelType = info.ChannelType
	a.baseURL = info.ChannelBaseUrl
	a.apiKey = info.ApiKey
}

func validateRemixRequest(c *gin.Context) *dto.TaskError {
	var req relaycommon.TaskSubmitReq
	if err := common.UnmarshalBodyReusable(c, &req); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}
	if strings.TrimSpace(req.Prompt) == "" {
		return service.TaskErrorWrapperLocal(fmt.Errorf("field prompt is required"), "invalid_request", http.StatusBadRequest)
	}
	// 存储原始请求到 context，与 ValidateMultipartDirect 路径保持一致
	c.Set("task_request", req)
	return nil
}

func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) (taskErr *dto.TaskError) {
	if info.Action == constant.TaskActionRemix {
		return validateRemixRequest(c)
	}
	if taskErr := relaycommon.ValidateMultipartDirect(c, info); taskErr != nil {
		return taskErr
	}
	if !isApimartRelay(info, info.OriginModelName) && !isApimartRelay(info, upstreamModelName(info)) {
		return nil
	}
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}
	payload, err := buildApimartPayload(req, upstreamModelName(info))
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}
	if err := validateApimartBase64ImageInputs(payload.ImageURLs, info.ChannelId); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}
	return nil
}

// EstimateBilling 根据用户请求的 seconds 和 size 计算 OtherRatios。
func (a *TaskAdaptor) EstimateBilling(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64 {
	// remix 路径的 OtherRatios 已在 ResolveOriginTask 中设置
	if info.Action == constant.TaskActionRemix {
		return nil
	}

	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil
	}
	if isApimartRelay(info, upstreamModelName(info)) {
		payload, err := buildApimartPayload(req, upstreamModelName(info))
		if err != nil {
			return nil
		}
		ratios := map[string]float64{
			"seconds": float64(payload.Duration),
			"quality": 1,
		}
		if strings.ToLower(strings.TrimSpace(payload.Resolution)) == "720p" {
			ratios["quality"] = 1.5
		}
		return ratios
	}

	seconds, _ := strconv.Atoi(req.Seconds)
	if seconds == 0 {
		seconds = req.Duration
	}
	if seconds <= 0 {
		seconds = 4
	}

	size := req.Size
	if size == "" {
		size = "720x1280"
	}

	ratios := map[string]float64{
		"seconds": float64(seconds),
		"size":    1,
	}
	if size == "1792x1024" || size == "1024x1792" {
		ratios["size"] = 1.666667
	}
	return ratios
}

func (a *TaskAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	if isApimartRelay(info, upstreamModelName(info)) {
		if info.Action == constant.TaskActionRemix {
			return "", fmt.Errorf("apimart video does not support remix")
		}
		return fmt.Sprintf("%s/v1/videos/generations", strings.TrimRight(a.baseURL, "/")), nil
	}
	if info.Action == constant.TaskActionRemix {
		return fmt.Sprintf("%s/v1/videos/%s/remix", a.baseURL, info.OriginTaskID), nil
	}
	return fmt.Sprintf("%s/v1/videos", a.baseURL), nil
}

// BuildRequestHeader sets required headers.
func (a *TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	if isApimartRelay(info, upstreamModelName(info)) {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		return nil
	}
	req.Header.Set("Content-Type", c.Request.Header.Get("Content-Type"))
	return nil
}

func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	if isApimartRelay(info, upstreamModelName(info)) {
		if cachedBody, ok := getCachedApimartRequestBody(c, info.ChannelId); ok {
			return bytes.NewReader(cachedBody), nil
		}

		req, err := relaycommon.GetTaskRequest(c)
		if err != nil {
			return nil, err
		}
		payload, err := buildApimartPayload(req, upstreamModelName(info))
		if err != nil {
			return nil, err
		}
		data, err := common.Marshal(payload)
		if err != nil {
			return nil, err
		}
		if len(info.ParamOverride) > 0 {
			data, err = relaycommon.ApplyParamOverrideWithRelayInfo(data, info)
			if err != nil {
				return nil, errors.Wrap(err, "apply_param_override_failed")
			}
		}
		imageURLs, err := apimartPayloadImageURLs(data)
		if err != nil {
			return nil, err
		}
		if err := validateApimartBase64ImageInputs(imageURLs, info.ChannelId); err != nil {
			return nil, err
		}
		imageURLs, uploaded, err := replaceApimartInlineImages(c.Request.Context(), imageURLs, info.ChannelId)
		if err != nil {
			return nil, err
		}
		if uploaded {
			data, err = sjson.SetBytes(data, "image_urls", imageURLs)
			if err != nil {
				return nil, errors.Wrap(err, "set_apimart_image_urls_failed")
			}
			cacheApimartRequestBody(c, info.ChannelId, data)
		}
		return bytes.NewReader(data), nil
	}

	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil, errors.Wrap(err, "get_request_body_failed")
	}
	cachedBody, err := storage.Bytes()
	if err != nil {
		return nil, errors.Wrap(err, "read_body_bytes_failed")
	}
	contentType := c.GetHeader("Content-Type")

	if strings.HasPrefix(contentType, "application/json") {
		newBody, ok, err := buildJSONTaskBody(cachedBody, info)
		if err != nil {
			return nil, err
		}
		if ok {
			return bytes.NewReader(newBody), nil
		}
		return bytes.NewReader(cachedBody), nil
	}

	if strings.Contains(contentType, "multipart/form-data") {
		formData, err := common.ParseMultipartFormReusable(c)
		if err != nil {
			return bytes.NewReader(cachedBody), nil
		}
		var buf bytes.Buffer
		writer := multipart.NewWriter(&buf)
		writer.WriteField("model", info.UpstreamModelName)
		for key, values := range formData.Value {
			if key == "model" {
				continue
			}
			for _, v := range values {
				writer.WriteField(key, v)
			}
		}
		for fieldName, fileHeaders := range formData.File {
			for _, fh := range fileHeaders {
				f, err := fh.Open()
				if err != nil {
					continue
				}
				ct := fh.Header.Get("Content-Type")
				if ct == "" || ct == "application/octet-stream" {
					buf512 := make([]byte, 512)
					n, _ := io.ReadFull(f, buf512)
					ct = http.DetectContentType(buf512[:n])
					// Re-open after sniffing so the full content is copied below
					f.Close()
					f, err = fh.Open()
					if err != nil {
						continue
					}
				}
				h := make(textproto.MIMEHeader)
				h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`, fieldName, fh.Filename))
				h.Set("Content-Type", ct)
				part, err := writer.CreatePart(h)
				if err != nil {
					f.Close()
					continue
				}
				io.Copy(part, f)
				f.Close()
			}
		}
		writer.Close()
		c.Request.Header.Set("Content-Type", writer.FormDataContentType())
		return &buf, nil
	}

	return common.ReaderOnly(storage), nil
}

func buildJSONTaskBody(cachedBody []byte, info *relaycommon.RelayInfo) ([]byte, bool, error) {
	var bodyMap map[string]interface{}
	if err := common.Unmarshal(cachedBody, &bodyMap); err != nil {
		return nil, false, nil
	}
	if info != nil && info.ChannelMeta != nil && strings.TrimSpace(info.UpstreamModelName) != "" {
		bodyMap["model"] = info.UpstreamModelName
	}
	newBody, err := common.Marshal(bodyMap)
	if err != nil {
		return nil, true, err
	}
	if info != nil && info.ChannelMeta != nil && len(info.ParamOverride) > 0 {
		newBody, err = relaycommon.ApplyParamOverrideWithRelayInfo(newBody, info)
		if err != nil {
			return nil, true, errors.Wrap(err, "apply_param_override_failed")
		}
	}
	return newBody, true, nil
}

// DoRequest delegates to common helper.
func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	return channel.DoTaskApiRequest(a, c, info, requestBody)
}

// DoResponse handles upstream response, returns taskID etc.
func (a *TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (taskID string, taskData []byte, taskErr *dto.TaskError) {
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		taskErr = service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusInternalServerError)
		return
	}
	_ = resp.Body.Close()

	if isApimartRelay(info, upstreamModelName(info)) {
		return a.doApimartResponse(c, responseBody, info)
	}

	// Parse Sora response
	var dResp responseTask
	if err := common.Unmarshal(responseBody, &dResp); err != nil {
		taskErr = service.TaskErrorWrapper(errors.Wrapf(err, "body: %s", responseBody), "unmarshal_response_body_failed", http.StatusInternalServerError)
		return
	}

	upstreamID := dResp.ID
	if upstreamID == "" {
		upstreamID = dResp.TaskID
	}
	if upstreamID == "" {
		taskErr = service.TaskErrorWrapper(fmt.Errorf("task_id is empty"), "invalid_response", http.StatusInternalServerError)
		return
	}

	// 使用公开 task_xxxx ID 返回给客户端
	dResp.ID = info.PublicTaskID
	dResp.TaskID = info.PublicTaskID
	taskcommon.SetTaskSubmitResponse(c, http.StatusOK, dResp)
	return upstreamID, responseBody, nil
}

// FetchTask fetch task status
func (a *TaskAdaptor) FetchTask(baseUrl, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid task_id")
	}

	uri := fmt.Sprintf("%s/v1/videos/%s", baseUrl, taskID)
	if isApimartBaseURL(baseUrl) || isApimartModel(stringFromTaskBody(body, "upstream_model_name")) || isApimartModel(stringFromTaskBody(body, "model")) {
		uri = fmt.Sprintf("%s/v1/tasks/%s?language=zh", strings.TrimRight(baseUrl, "/"), url.PathEscape(taskID))
	}

	req, err := http.NewRequest(http.MethodGet, uri, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+key)

	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return nil, fmt.Errorf("new proxy http client failed: %w", err)
	}
	if client == nil {
		client = http.DefaultClient
	}
	return client.Do(req)
}

func (a *TaskAdaptor) GetModelList() []string {
	return ModelList
}

func (a *TaskAdaptor) GetChannelName() string {
	return ChannelName
}

func (a *TaskAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	if taskInfo, ok, err := parseApimartTaskResult(respBody); ok || err != nil {
		return taskInfo, err
	}

	resTask := responseTask{}
	if err := common.Unmarshal(respBody, &resTask); err != nil {
		return nil, errors.Wrap(err, "unmarshal task result failed")
	}
	taskID := strings.TrimSpace(resTask.TaskID)
	if taskID == "" {
		taskID = strings.TrimSpace(resTask.ID)
	}

	taskResult := relaycommon.TaskInfo{
		Code: 0,
	}

	switch strings.ToLower(strings.TrimSpace(resTask.Status)) {
	case "queued", "pending":
		taskResult.Status = model.TaskStatusQueued
	case "processing", "in_progress":
		taskResult.Status = model.TaskStatusInProgress
	case "completed", "succeeded", "success", "done":
		taskResult.Status = model.TaskStatusSuccess
		taskResult.Url = resTask.resultURL()
		if taskResult.Url == "" || taskcommon.IsTaskProxyContentURL(taskResult.Url, taskID) {
			if extracted := taskcommon.ExtractVideoResultURL(respBody, taskID); extracted != "" {
				taskResult.Url = extracted
			}
		}
	case "failed", "cancelled":
		taskResult.Status = model.TaskStatusFailure
		if resTask.Error != nil {
			taskResult.Reason = resTask.Error.Message
		} else {
			taskResult.Reason = "task failed"
		}
	default:
	}
	if resTask.Progress > 0 && resTask.Progress < 100 {
		taskResult.Progress = fmt.Sprintf("%d%%", resTask.Progress)
	}

	return &taskResult, nil
}

func (r responseTask) resultURL() string {
	candidates := []string{r.ResultURL, r.URL}
	if r.Video != nil {
		candidates = append(candidates, r.Video.URL)
	}
	candidates = append(candidates, r.VideoURL)
	candidates = append(candidates, r.Output...)

	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate != "" && !taskcommon.IsAllowedGrokVideoURL(candidate) {
			return candidate
		}
	}
	for _, candidate := range candidates {
		if candidate = strings.TrimSpace(candidate); candidate != "" {
			return candidate
		}
	}
	return ""
}

func (a *TaskAdaptor) ConvertToOpenAIVideo(task *model.Task) ([]byte, error) {
	if isApimartModel(task.Properties.UpstreamModelName) || looksLikeApimartTaskData(task.Data) {
		openAIVideo := task.ToOpenAIVideo()
		if task.Status == model.TaskStatusFailure {
			openAIVideo.Error = &dto.OpenAIVideoError{
				Message: taskcommon.DefaultString(task.FailReason, "task failed"),
			}
		}
		return common.Marshal(openAIVideo)
	}

	data := task.Data
	var err error
	if data, err = sjson.SetBytes(data, "id", task.TaskID); err != nil {
		return nil, errors.Wrap(err, "set id failed")
	}
	if data, err = sjson.SetBytes(data, "task_id", task.TaskID); err != nil {
		return nil, errors.Wrap(err, "set task_id failed")
	}
	return data, nil
}

func (a *TaskAdaptor) doApimartResponse(c *gin.Context, responseBody []byte, info *relaycommon.RelayInfo) (taskID string, taskData []byte, taskErr *dto.TaskError) {
	var apiResp apimartSubmitResponse
	if err := common.Unmarshal(responseBody, &apiResp); err != nil {
		taskErr = service.TaskErrorWrapper(errors.Wrapf(err, "body: %s", responseBody), "unmarshal_response_body_failed", http.StatusInternalServerError)
		return
	}
	if apiResp.Error != nil && strings.TrimSpace(apiResp.Error.Message) != "" {
		taskErr = service.TaskErrorWrapperLocal(fmt.Errorf("%s", apiResp.Error.Message), "task_failed", http.StatusBadRequest)
		return
	}
	if apiResp.Code != 0 && apiResp.Code != http.StatusOK {
		taskErr = service.TaskErrorWrapperLocal(fmt.Errorf("apimart api error code: %d", apiResp.Code), "task_failed", http.StatusBadRequest)
		return
	}
	if len(apiResp.Data) == 0 || strings.TrimSpace(apiResp.Data[0].TaskID) == "" {
		taskErr = service.TaskErrorWrapper(fmt.Errorf("task_id is empty"), "invalid_response", http.StatusInternalServerError)
		return
	}

	ov := dto.NewOpenAIVideo()
	ov.ID = info.PublicTaskID
	ov.TaskID = info.PublicTaskID
	ov.CreatedAt = 0
	ov.Model = info.OriginModelName
	taskcommon.SetTaskSubmitResponse(c, http.StatusOK, ov)
	return strings.TrimSpace(apiResp.Data[0].TaskID), responseBody, nil
}

func parseApimartTaskResult(respBody []byte) (*relaycommon.TaskInfo, bool, error) {
	var root map[string]any
	if err := common.Unmarshal(respBody, &root); err != nil {
		return nil, false, nil
	}
	if errObj := objectValue(root["error"]); errObj != nil {
		return &relaycommon.TaskInfo{
			Code:   intValue(root["code"]),
			Status: model.TaskStatusFailure,
			Reason: apimartErrorMessage(errObj, "task failed"),
		}, true, nil
	}
	data := objectValue(root["data"])
	if data == nil {
		return nil, false, nil
	}
	status := strings.ToLower(strings.TrimSpace(stringValue(data["status"])))
	if status == "" {
		return nil, false, nil
	}

	taskInfo := &relaycommon.TaskInfo{
		Code:   intValue(root["code"]),
		TaskID: stringValue(data["id"]),
	}
	switch status {
	case "pending":
		taskInfo.Status = model.TaskStatusQueued
		taskInfo.Progress = apimartProgress(data["progress"], taskcommon.ProgressQueued)
	case "submitted":
		taskInfo.Status = model.TaskStatusSubmitted
		taskInfo.Progress = apimartProgress(data["progress"], taskcommon.ProgressSubmitted)
	case "processing", "running":
		taskInfo.Status = model.TaskStatusInProgress
		taskInfo.Progress = apimartProgress(data["progress"], taskcommon.ProgressInProgress)
	case "completed", "succeeded", "success", "done":
		taskInfo.Status = model.TaskStatusSuccess
		taskInfo.Progress = taskcommon.ProgressComplete
		taskInfo.Url = taskcommon.ExtractVideoResultURL(respBody, taskInfo.TaskID)
	case "failed", "cancelled", "canceled":
		taskInfo.Status = model.TaskStatusFailure
		taskInfo.Progress = taskcommon.ProgressComplete
		if errObj := objectValue(data["error"]); errObj != nil {
			taskInfo.Reason = apimartErrorMessage(errObj, "task failed")
		} else if message := stringValue(data["message"]); message != "" {
			taskInfo.Reason = message
		} else {
			taskInfo.Reason = "task failed"
		}
	default:
		return nil, true, fmt.Errorf("unknown apimart task status: %s", status)
	}
	return taskInfo, true, nil
}

func buildApimartPayload(req relaycommon.TaskSubmitReq, modelName string) (*apimartRequestPayload, error) {
	duration, err := apimartDuration(req)
	if err != nil {
		return nil, err
	}
	payload := &apimartRequestPayload{
		Model:      normalizeApimartModelName(modelName),
		Prompt:     req.Prompt,
		Size:       apimartSize(req.Size),
		Duration:   duration,
		Resolution: taskcommon.DefaultString(strings.TrimSpace(req.Quality), "480p"),
		ImageURLs:  apimartImageURLs(req),
	}
	metadata := apimartMetadata(req.Metadata)
	if err := taskcommon.UnmarshalMetadata(metadata, payload); err != nil {
		return nil, err
	}
	payload.Model = normalizeApimartModelName(modelName)
	if payload.Model == "" {
		payload.Model = "grok-imagine-1.5-video-ext"
	}
	if payload.Size == "" {
		payload.Size = "16:9"
	}
	if payload.Resolution == "" {
		payload.Resolution = "480p"
	}
	if payload.Duration < 6 || payload.Duration > 15 {
		return nil, fmt.Errorf("duration must be between 6 and 15 seconds")
	}
	if len(payload.ImageURLs) > 7 {
		return nil, fmt.Errorf("image_urls supports at most 7 images")
	}
	return payload, nil
}

func apimartDuration(req relaycommon.TaskSubmitReq) (int, error) {
	if req.Duration != 0 {
		return req.Duration, nil
	}
	if strings.TrimSpace(req.Seconds) == "" {
		return 6, nil
	}
	duration, err := strconv.Atoi(strings.TrimSpace(req.Seconds))
	if err != nil {
		return 0, err
	}
	return duration, nil
}

func apimartSize(size string) string {
	switch strings.TrimSpace(size) {
	case "854x480", "1280x720", "1920x1080", "1792x1024":
		return "16:9"
	case "720x1280", "1080x1920", "1024x1792":
		return "9:16"
	case "1024x1024":
		return "1:1"
	case "16:9", "9:16", "1:1", "3:2", "2:3":
		return strings.TrimSpace(size)
	default:
		if strings.TrimSpace(size) == "" {
			return "16:9"
		}
		return strings.TrimSpace(size)
	}
}

func apimartMetadata(metadata map[string]any) map[string]any {
	if len(metadata) == 0 {
		return metadata
	}
	result := make(map[string]any, len(metadata)+1)
	for key, value := range metadata {
		result[key] = value
	}
	if _, hasResolution := result["resolution"]; !hasResolution {
		if quality, hasQuality := result["quality"]; hasQuality {
			result["resolution"] = quality
		}
	}
	return result
}

func apimartImageURLs(req relaycommon.TaskSubmitReq) []string {
	candidates := append([]string{}, req.ImageURLs...)
	candidates = append(candidates, req.Images...)
	candidates = append(candidates, req.Image, req.InputReference)
	seen := map[string]bool{}
	out := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" || seen[candidate] {
			continue
		}
		seen[candidate] = true
		out = append(out, candidate)
	}
	return out
}

func apimartPayloadImageURLs(data []byte) ([]string, error) {
	var payload struct {
		ImageURLs []string `json:"image_urls,omitempty"`
	}
	if err := common.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("invalid APIMart image_urls")
	}
	return payload.ImageURLs, nil
}

// validateApimartBase64ImageInputs intentionally only validates local input and
// configuration. Uploading is deferred to BuildRequestBody, after pre-consume.
func validateApimartBase64ImageInputs(imageURLs []string, channelID int) error {
	if len(imageURLs) > 7 {
		return fmt.Errorf("image_urls supports at most 7 images")
	}
	hasInlineImage := false
	for _, imageURL := range imageURLs {
		inlineImage, err := parseApimartInlineImage(imageURL)
		if err != nil {
			return err
		}
		if inlineImage != nil {
			hasInlineImage = true
		}
	}
	if !hasInlineImage {
		return nil
	}
	_, err := apimartInputMediaUploadConfigForChannel(channelID)
	return err
}

func replaceApimartInlineImages(ctx context.Context, imageURLs []string, channelID int) ([]string, bool, error) {
	converted := append([]string(nil), imageURLs...)
	var config apimartInputMediaUploadConfig
	configLoaded := false
	uploaded := false

	for index, imageURL := range converted {
		inlineImage, err := parseApimartInlineImage(imageURL)
		if err != nil {
			return nil, false, err
		}
		if inlineImage == nil {
			continue
		}
		if !configLoaded {
			config, err = apimartInputMediaUploadConfigForChannel(channelID)
			if err != nil {
				return nil, false, err
			}
			configLoaded = true
		}
		publicURL, err := uploadApimartInputMedia(ctx, config, inlineImage)
		if err != nil {
			return nil, false, err
		}
		converted[index] = publicURL
		uploaded = true
	}

	return converted, uploaded, nil
}

func parseApimartInlineImage(value string) (*apimartInlineImage, error) {
	value = strings.TrimSpace(value)
	if value == "" || isApimartHTTPURL(value) {
		return nil, nil
	}
	if strings.HasPrefix(strings.ToLower(value), "data:") {
		return parseApimartDataImage(value)
	}
	return parseApimartRawBase64Image(value)
}

func parseApimartDataImage(value string) (*apimartInlineImage, error) {
	commaIndex := strings.Index(value, ",")
	if commaIndex < 0 {
		return nil, fmt.Errorf("APIMart input image must be a valid base64 data URL")
	}
	metadata := value[len("data:"):commaIndex]
	parts := strings.Split(metadata, ";")
	if len(parts) == 0 {
		return nil, fmt.Errorf("APIMart input image must be a valid base64 data URL")
	}
	mimeType := strings.ToLower(strings.TrimSpace(parts[0]))
	if !isApimartSupportedImageMIME(mimeType) {
		return nil, fmt.Errorf("APIMart supports only PNG, JPEG, or WebP input images")
	}
	hasBase64Encoding := false
	for _, part := range parts[1:] {
		if strings.EqualFold(strings.TrimSpace(part), "base64") {
			hasBase64Encoding = true
			break
		}
	}
	if !hasBase64Encoding {
		return nil, fmt.Errorf("APIMart input image must be a base64 data URL")
	}
	return decodeApimartBase64Image(value[commaIndex+1:], true)
}

func parseApimartRawBase64Image(value string) (*apimartInlineImage, error) {
	return decodeApimartBase64Image(value, false)
}

func decodeApimartBase64Image(encoded string, strict bool) (*apimartInlineImage, error) {
	encoded = strings.TrimSpace(encoded)
	if encoded == "" {
		if strict {
			return nil, fmt.Errorf("APIMart input image must be a valid base64-encoded image")
		}
		return nil, nil
	}
	if len(encoded) > base64.StdEncoding.EncodedLen(apimartInputMediaMaxBytes) {
		return nil, fmt.Errorf("APIMart input image exceeds the 20 MiB limit")
	}

	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		data, err = base64.RawStdEncoding.DecodeString(encoded)
	}
	if err != nil {
		data, err = base64.URLEncoding.DecodeString(encoded)
	}
	if err != nil {
		data, err = base64.RawURLEncoding.DecodeString(encoded)
	}
	if err != nil {
		if strict {
			return nil, fmt.Errorf("APIMart input image must be a valid base64-encoded image")
		}
		return nil, nil
	}
	if len(data) > apimartInputMediaMaxBytes {
		return nil, fmt.Errorf("APIMart input image exceeds the 20 MiB limit")
	}

	mimeType := strings.ToLower(strings.TrimSpace(strings.SplitN(http.DetectContentType(data), ";", 2)[0]))
	if isApimartSupportedImageMIME(mimeType) {
		return &apimartInlineImage{data: data, mimeType: mimeType}, nil
	}
	if strict || strings.HasPrefix(mimeType, "image/") {
		return nil, fmt.Errorf("APIMart supports only PNG, JPEG, or WebP input images")
	}
	return nil, nil
}

func isApimartSupportedImageMIME(mimeType string) bool {
	switch strings.ToLower(strings.TrimSpace(mimeType)) {
	case "image/png", "image/jpeg", "image/webp":
		return true
	default:
		return false
	}
}

func apimartInputMediaUploadConfigForChannel(channelID int) (apimartInputMediaUploadConfig, error) {
	allowed, err := isApimartBase64ImageChannelAllowed(channelID)
	if err != nil {
		return apimartInputMediaUploadConfig{}, err
	}
	if !allowed {
		return apimartInputMediaUploadConfig{}, fmt.Errorf("APIMart base64 image input is not enabled for this channel")
	}

	uploadURL := strings.TrimSpace(os.Getenv(apimartInputMediaUploadURLEnv))
	if uploadURL == "" || !isApimartHTTPURL(uploadURL) {
		return apimartInputMediaUploadConfig{}, fmt.Errorf("APIMart base64 image upload is not configured")
	}
	return apimartInputMediaUploadConfig{url: uploadURL}, nil
}

func isApimartBase64ImageChannelAllowed(channelID int) (bool, error) {
	configuredIDs := strings.TrimSpace(os.Getenv(apimartBase64ImageChannelIDsEnv))
	if configuredIDs == "" {
		return false, nil
	}
	for _, configuredID := range strings.Split(configuredIDs, ",") {
		configuredID = strings.TrimSpace(configuredID)
		id, err := strconv.Atoi(configuredID)
		if err != nil || id <= 0 {
			return false, fmt.Errorf("APIMart base64 image channel allowlist is invalid")
		}
		if id == channelID {
			return true, nil
		}
	}
	return false, nil
}

func uploadApimartInputMedia(ctx context.Context, config apimartInputMediaUploadConfig, image *apimartInlineImage) (string, error) {
	uploadCtx, cancel := context.WithTimeout(ctx, apimartInputMediaUploadTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(uploadCtx, http.MethodPost, config.url, bytes.NewReader(image.data))
	if err != nil {
		return "", fmt.Errorf("APIMart image upload request could not be created")
	}
	req.Header.Set("Content-Type", image.mimeType)

	resp, err := (&http.Client{Timeout: apimartInputMediaUploadTimeout}).Do(req)
	if err != nil {
		return "", fmt.Errorf("APIMart image upload failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("APIMart image upload failed with status %d", resp.StatusCode)
	}

	var response struct {
		URL string `json:"url"`
	}
	if err := common.DecodeJson(resp.Body, &response); err != nil {
		return "", fmt.Errorf("APIMart image upload returned an invalid response")
	}
	publicURL := strings.TrimSpace(response.URL)
	if !isApimartHTTPURL(publicURL) {
		return "", fmt.Errorf("APIMart image upload returned an invalid URL")
	}
	return publicURL, nil
}

func isApimartHTTPURL(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return false
	}
	return parsed.Host != "" && (strings.EqualFold(parsed.Scheme, "http") || strings.EqualFold(parsed.Scheme, "https"))
}

func getCachedApimartRequestBody(c *gin.Context, channelID int) ([]byte, bool) {
	if c == nil {
		return nil, false
	}
	value, exists := c.Get(apimartRequestBodyContextKey)
	if !exists {
		return nil, false
	}
	cached, ok := value.(apimartPreparedRequestBody)
	if !ok || cached.channelID != channelID || len(cached.body) == 0 {
		return nil, false
	}
	return cached.body, true
}

func cacheApimartRequestBody(c *gin.Context, channelID int, body []byte) {
	if c == nil {
		return
	}
	c.Set(apimartRequestBodyContextKey, apimartPreparedRequestBody{
		channelID: channelID,
		body:      append([]byte(nil), body...),
	})
}

func isApimartRelay(info *relaycommon.RelayInfo, modelName string) bool {
	if info != nil && info.ChannelMeta != nil && isApimartBaseURL(info.ChannelBaseUrl) {
		return true
	}
	return isApimartModel(modelName)
}

func isApimartBaseURL(baseURL string) bool {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	return host == "api.apimart.ai"
}

func isApimartModel(modelName string) bool {
	switch strings.TrimSpace(modelName) {
	case "grok-imagine-1.5-video-apimart", "grok-imagine-1.5-video-ext":
		return true
	default:
		return false
	}
}

func normalizeApimartModelName(modelName string) string {
	switch strings.TrimSpace(modelName) {
	case "", "grok-imagine-1.5-video-apimart":
		return "grok-imagine-1.5-video-ext"
	default:
		return strings.TrimSpace(modelName)
	}
}

func upstreamModelName(info *relaycommon.RelayInfo) string {
	if info == nil {
		return ""
	}
	if info.ChannelMeta != nil && strings.TrimSpace(info.UpstreamModelName) != "" {
		return info.UpstreamModelName
	}
	return info.OriginModelName
}

func stringFromTaskBody(body map[string]any, key string) string {
	if body == nil {
		return ""
	}
	if value, ok := body[key].(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

func looksLikeApimartTaskData(data []byte) bool {
	var root map[string]any
	if err := common.Unmarshal(data, &root); err != nil {
		return false
	}
	if _, ok := root["code"]; !ok {
		return false
	}
	if dataObj := objectValue(root["data"]); dataObj != nil {
		_, hasStatus := dataObj["status"]
		_, hasResult := dataObj["result"]
		return hasStatus || hasResult
	}
	if dataItems, ok := root["data"].([]any); ok && len(dataItems) > 0 {
		if first := objectValue(dataItems[0]); first != nil {
			_, hasTaskID := first["task_id"]
			return hasTaskID
		}
	}
	return false
}

func objectValue(value any) map[string]any {
	if obj, ok := value.(map[string]any); ok {
		return obj
	}
	return nil
}

func stringValue(value any) string {
	if s, ok := value.(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

func intValue(value any) int {
	switch v := value.(type) {
	case float64:
		return int(v)
	case int:
		return v
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(v))
		return n
	default:
		return 0
	}
}

func apimartErrorMessage(errObj map[string]any, fallback string) string {
	if message := stringValue(errObj["message"]); message != "" {
		return message
	}
	if typ := stringValue(errObj["type"]); typ != "" {
		return typ
	}
	return fallback
}

func apimartProgress(value any, fallback string) string {
	if n := intValue(value); n > 0 {
		return fmt.Sprintf("%d%%", n)
	}
	return fallback
}
