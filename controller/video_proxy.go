package controller

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	taskcommon "github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
)

// videoProxyError returns a standardized OpenAI-style error response.
func videoProxyError(c *gin.Context, status int, errType, message string) {
	c.JSON(status, gin.H{
		"error": gin.H{
			"message": message,
			"type":    errType,
		},
	})
}

func VideoProxy(c *gin.Context) {
	taskID := c.Param("task_id")
	if taskID == "" {
		videoProxyError(c, http.StatusBadRequest, "invalid_request_error", "task_id is required")
		return
	}

	userID := c.GetInt("id")
	task, exists, err := model.GetByTaskId(userID, taskID)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to query task %s: %s", taskID, err.Error()))
		videoProxyError(c, http.StatusInternalServerError, "server_error", "Failed to query task")
		return
	}
	if !exists || task == nil {
		videoProxyError(c, http.StatusNotFound, "invalid_request_error", "Task not found")
		return
	}

	if task.Status != model.TaskStatusSuccess {
		videoProxyError(c, http.StatusBadRequest, "invalid_request_error",
			fmt.Sprintf("Task is not completed yet, current status: %s", task.Status))
		return
	}

	channel, err := model.CacheGetChannel(task.ChannelId)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to get channel for task %s: %s", taskID, err.Error()))
		videoProxyError(c, http.StatusInternalServerError, "server_error", "Failed to retrieve channel information")
		return
	}
	baseURL := channel.GetBaseURL()
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}

	var videoURL string
	proxy := channel.GetSetting().Proxy
	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to create proxy client for task %s: %s", taskID, err.Error()))
		videoProxyError(c, http.StatusInternalServerError, "server_error", "Failed to create proxy client")
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "", nil)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to create request: %s", err.Error()))
		videoProxyError(c, http.StatusInternalServerError, "server_error", "Failed to create proxy request")
		return
	}

	if videoURL == "" {
		switch channel.Type {
		case constant.ChannelTypeGemini:
			apiKey := task.PrivateData.Key
			if apiKey == "" {
				logger.LogError(c.Request.Context(), fmt.Sprintf("Missing stored API key for Gemini task %s", taskID))
				videoProxyError(c, http.StatusInternalServerError, "server_error", "API key not stored for task")
				return
			}
			videoURL, err = getGeminiVideoURL(channel, task, apiKey)
			if err != nil {
				logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to resolve Gemini video URL for task %s: %s", taskID, err.Error()))
				videoProxyError(c, http.StatusBadGateway, "server_error", "Failed to resolve Gemini video URL")
				return
			}
			req.Header.Set("x-goog-api-key", apiKey)
		case constant.ChannelTypeVertexAi:
			videoURL, err = getVertexVideoURL(channel, task)
			if err != nil {
				logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to resolve Vertex video URL for task %s: %s", taskID, err.Error()))
				videoProxyError(c, http.StatusBadGateway, "server_error", "Failed to resolve Vertex video URL")
				return
			}
		case constant.ChannelTypeOpenAI, constant.ChannelTypeSora:
			if resultURL := strings.TrimSpace(task.GetResultURL()); resultURL != "" && !isTaskProxyContentURL(resultURL, task.TaskID) {
				videoURL = resultURL
				if shouldAuthorizeUpstreamVideoURL(videoURL) {
					setVideoProxyBearer(req, getVideoProxyChannelKey(channel, task))
				}
			} else {
				videoURL = fmt.Sprintf("%s/v1/videos/%s/content", baseURL, task.GetUpstreamTaskID())
				setVideoProxyBearer(req, getVideoProxyChannelKey(channel, task))
			}
		default:
			// Video URL is stored in PrivateData.ResultURL (fallback to FailReason for old data)
			videoURL = task.GetResultURL()
		}
	}

	videoURL = strings.TrimSpace(videoURL)
	if videoURL == "" {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Video URL is empty for task %s", taskID))
		videoProxyError(c, http.StatusBadGateway, "server_error", "Failed to fetch video content")
		return
	}

	if strings.HasPrefix(videoURL, "data:") {
		if err := writeVideoDataURL(c, videoURL); err != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to decode video data URL for task %s: %s", taskID, err.Error()))
			videoProxyError(c, http.StatusBadGateway, "server_error", "Failed to fetch video content")
		}
		return
	}

	fetchSetting := system_setting.GetFetchSetting()
	if err := common.ValidateURLWithFetchSetting(videoURL, fetchSetting.EnableSSRFProtection, fetchSetting.AllowPrivateIp, fetchSetting.DomainFilterMode, fetchSetting.IpFilterMode, fetchSetting.DomainList, fetchSetting.IpList, fetchSetting.AllowedPorts, fetchSetting.ApplyIPFilterForDomain); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Video URL blocked for task %s: %v", taskID, err))
		videoProxyError(c, http.StatusForbidden, "server_error", fmt.Sprintf("request blocked: %v", err))
		return
	}

	req.URL, err = url.Parse(videoURL)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to parse URL %s: %s", videoURL, err.Error()))
		videoProxyError(c, http.StatusInternalServerError, "server_error", "Failed to create proxy request")
		return
	}

	if rangeHeader := strings.TrimSpace(c.Request.Header.Get("Range")); rangeHeader != "" {
		req.Header.Set("Range", rangeHeader)
	}
	if ifRangeHeader := strings.TrimSpace(c.Request.Header.Get("If-Range")); ifRangeHeader != "" {
		req.Header.Set("If-Range", ifRangeHeader)
	}

	resp, err := client.Do(req)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to fetch video from %s: %s", videoURL, err.Error()))
		videoProxyError(c, http.StatusBadGateway, "server_error", "Failed to fetch video content")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Upstream returned status %d for %s", resp.StatusCode, videoURL))
		videoProxyError(c, http.StatusBadGateway, "server_error",
			fmt.Sprintf("Upstream service returned status %d", resp.StatusCode))
		return
	}

	for key, values := range resp.Header {
		for _, value := range values {
			c.Writer.Header().Add(key, value)
		}
	}

	c.Writer.Header().Set("Cache-Control", "public, max-age=86400")
	c.Writer.WriteHeader(resp.StatusCode)
	if _, err = io.Copy(c.Writer, resp.Body); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to stream video content: %s", err.Error()))
	}
}

func GrokVideoProxy(c *gin.Context) {
	taskID := strings.TrimSpace(c.Param("task_id"))
	if taskID == "" {
		videoProxyError(c, http.StatusBadRequest, "invalid_request_error", "task_id is required")
		return
	}
	if !taskcommon.VerifyGrokVideoProxySignature(taskID, c.Query("exp"), c.Query("sig")) {
		videoProxyError(c, http.StatusForbidden, "invalid_request_error", "invalid or expired video signature")
		return
	}

	task, exists, err := model.GetByOnlyTaskId(taskID)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to query grok video task %s: %s", taskID, err.Error()))
		videoProxyError(c, http.StatusInternalServerError, "server_error", "Failed to query task")
		return
	}
	if !exists || task == nil {
		videoProxyError(c, http.StatusNotFound, "invalid_request_error", "Task not found")
		return
	}
	if task.Status != model.TaskStatusSuccess {
		videoProxyError(c, http.StatusBadRequest, "invalid_request_error",
			fmt.Sprintf("Task is not completed yet, current status: %s", task.Status))
		return
	}
	if !taskcommon.IsGrokVideoProxyCandidate(task) {
		videoProxyError(c, http.StatusForbidden, "invalid_request_error", "video url is not allowed")
		return
	}

	videoURL := strings.TrimSpace(task.GetResultURL())
	fetchSetting := system_setting.GetFetchSetting()
	if err := common.ValidateURLWithFetchSetting(videoURL, fetchSetting.EnableSSRFProtection, fetchSetting.AllowPrivateIp, fetchSetting.DomainFilterMode, fetchSetting.IpFilterMode, fetchSetting.DomainList, fetchSetting.IpList, fetchSetting.AllowedPorts, fetchSetting.ApplyIPFilterForDomain); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Grok video URL blocked for task %s: %v", taskID, err))
		videoProxyError(c, http.StatusForbidden, "server_error", fmt.Sprintf("request blocked: %v", err))
		return
	}
	if tryGrokVideoAccelRedirect(c, videoURL) {
		return
	}

	proxy := ""
	if channel, err := model.CacheGetChannel(task.ChannelId); err == nil && channel != nil {
		proxy = channel.GetSetting().Proxy
	}
	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to create grok video proxy client for task %s: %s", taskID, err.Error()))
		videoProxyError(c, http.StatusInternalServerError, "server_error", "Failed to create proxy client")
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, videoURL, nil)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to create grok video request for task %s: %s", taskID, err.Error()))
		videoProxyError(c, http.StatusInternalServerError, "server_error", "Failed to create proxy request")
		return
	}
	if rangeHeader := strings.TrimSpace(c.Request.Header.Get("Range")); rangeHeader != "" {
		req.Header.Set("Range", rangeHeader)
	}
	if ifRangeHeader := strings.TrimSpace(c.Request.Header.Get("If-Range")); ifRangeHeader != "" {
		req.Header.Set("If-Range", ifRangeHeader)
	}

	resp, err := client.Do(req)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to fetch grok video from %s: %s", videoURL, err.Error()))
		videoProxyError(c, http.StatusBadGateway, "server_error", "Failed to fetch video content")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Grok video upstream returned status %d for %s", resp.StatusCode, videoURL))
		videoProxyError(c, http.StatusBadGateway, "server_error",
			fmt.Sprintf("Upstream service returned status %d", resp.StatusCode))
		return
	}

	for key, values := range resp.Header {
		for _, value := range values {
			c.Writer.Header().Add(key, value)
		}
	}
	c.Writer.Header().Set("Cache-Control", "public, max-age=3600")
	c.Writer.WriteHeader(resp.StatusCode)
	if _, err = io.Copy(c.Writer, resp.Body); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to stream grok video content: %s", err.Error()))
	}
}

func tryGrokVideoAccelRedirect(c *gin.Context, videoURL string) bool {
	if !grokVideoAccelRedirectEnabled() {
		return false
	}
	if !grokVideoAccelRedirectHeaderValid(c.GetHeader("X-Video-Proxy-Accel")) {
		return false
	}

	parsed, err := url.Parse(strings.TrimSpace(videoURL))
	if err != nil || parsed.Host == "" || parsed.Scheme == "" {
		return false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return false
	}
	host := parsed.Hostname()
	if host == "" {
		return false
	}

	target := parsed.RequestURI()
	if target == "" {
		target = "/"
	}
	c.Header("X-Accel-Redirect", "/__grok_xai/"+host+target)
	c.Header("X-Accel-Buffering", "no")
	c.Header("Cache-Control", "public, max-age=3600")
	c.Status(http.StatusOK)
	return true
}

func grokVideoAccelRedirectEnabled() bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv("GROK_VIDEO_ACCEL_REDIRECT")))
	return value == "1" || value == "true" || value == "yes" || value == "on"
}

func grokVideoAccelRedirectHeaderValid(headerValue string) bool {
	headerValue = strings.TrimSpace(headerValue)
	token := strings.TrimSpace(os.Getenv("GROK_VIDEO_ACCEL_REDIRECT_TOKEN"))
	if token != "" {
		return subtle.ConstantTimeCompare([]byte(headerValue), []byte(token)) == 1
	}
	return headerValue == "1"
}

func shouldAuthorizeUpstreamVideoURL(videoURL string) bool {
	videoURL = strings.TrimSpace(videoURL)
	if videoURL == "" {
		return false
	}
	parsedVideoURL, err := url.Parse(videoURL)
	if err != nil || parsedVideoURL.Host == "" || parsedVideoURL.Scheme == "" {
		return false
	}
	if parsedVideoURL.Scheme != "http" && parsedVideoURL.Scheme != "https" {
		return false
	}
	path := strings.TrimSpace(parsedVideoURL.EscapedPath())
	return strings.HasPrefix(path, "/v1/videos/") && strings.HasSuffix(path, "/content")
}

func getVideoProxyChannelKey(channel *model.Channel, task *model.Task) string {
	if task != nil {
		if key := strings.TrimSpace(task.PrivateData.Key); key != "" {
			return key
		}
	}
	if channel == nil {
		return ""
	}
	for _, key := range channel.GetKeys() {
		if key = strings.TrimSpace(key); key != "" {
			return key
		}
	}
	return strings.TrimSpace(channel.Key)
}

func setVideoProxyBearer(req *http.Request, key string) {
	key = strings.TrimSpace(key)
	if req != nil && key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
}

func writeVideoDataURL(c *gin.Context, dataURL string) error {
	parts := strings.SplitN(dataURL, ",", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid data url")
	}

	header := parts[0]
	payload := parts[1]
	if !strings.HasPrefix(header, "data:") || !strings.Contains(header, ";base64") {
		return fmt.Errorf("unsupported data url")
	}

	mimeType := strings.TrimPrefix(header, "data:")
	mimeType = strings.TrimSuffix(mimeType, ";base64")
	if mimeType == "" {
		mimeType = "video/mp4"
	}

	videoBytes, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		videoBytes, err = base64.RawStdEncoding.DecodeString(payload)
		if err != nil {
			return err
		}
	}

	c.Writer.Header().Set("Content-Type", mimeType)
	c.Writer.Header().Set("Cache-Control", "public, max-age=86400")
	c.Writer.WriteHeader(http.StatusOK)
	_, err = c.Writer.Write(videoBytes)
	return err
}
