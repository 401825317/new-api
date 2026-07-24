package sora

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"

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
	Model     string   `json:"model"`
	Prompt    string   `json:"prompt"`
	Size      string   `json:"size,omitempty"`
	Duration  int      `json:"duration,omitempty"`
	Quality   string   `json:"quality,omitempty"`
	ImageURLs []string `json:"image_urls,omitempty"`
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
	if !isApimartRelay(info, info.OriginModelName) {
		return nil
	}
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}
	if _, err := buildApimartPayload(req, upstreamModelName(info)); err != nil {
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
		if strings.ToLower(strings.TrimSpace(payload.Quality)) == "720p" {
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
		Model:     strings.TrimSpace(modelName),
		Prompt:    req.Prompt,
		Size:      apimartSize(req.Size),
		Duration:  duration,
		Quality:   taskcommon.DefaultString(strings.TrimSpace(req.Quality), "480p"),
		ImageURLs: apimartImageURLs(req),
	}
	if err := taskcommon.UnmarshalMetadata(req.Metadata, payload); err != nil {
		return nil, err
	}
	payload.Model = strings.TrimSpace(modelName)
	if payload.Model == "" {
		payload.Model = "grok-imagine-1.5-video-apimart"
	}
	if payload.Size == "" {
		payload.Size = "16:9"
	}
	if payload.Quality == "" {
		payload.Quality = "480p"
	}
	if payload.Duration < 6 || payload.Duration > 30 {
		return nil, fmt.Errorf("duration must be between 6 and 30 seconds")
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
	case "1280x720", "1792x1024":
		return "16:9"
	case "720x1280", "1024x1792":
		return "9:16"
	case "1024x1024":
		return "1:1"
	default:
		if strings.TrimSpace(size) == "" {
			return "16:9"
		}
		return strings.TrimSpace(size)
	}
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
