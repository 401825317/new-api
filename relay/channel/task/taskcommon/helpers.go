package taskcommon

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// UnmarshalMetadata converts a map[string]any metadata to a typed struct via JSON round-trip.
// This replaces the repeated pattern: json.Marshal(metadata) → json.Unmarshal(bytes, &target).
func UnmarshalMetadata(metadata map[string]any, target any) error {
	if metadata == nil {
		return nil
	}
	// Prevent metadata from overriding model fields to avoid billing bypass.
	delete(metadata, "model")
	metaBytes, err := common.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("marshal metadata failed: %w", err)
	}
	if err := common.Unmarshal(metaBytes, target); err != nil {
		return fmt.Errorf("unmarshal metadata failed: %w", err)
	}
	return nil
}

// DefaultString returns val if non-empty, otherwise fallback.
func DefaultString(val, fallback string) string {
	if val == "" {
		return fallback
	}
	return val
}

// DefaultInt returns val if non-zero, otherwise fallback.
func DefaultInt(val, fallback int) int {
	if val == 0 {
		return fallback
	}
	return val
}

// EncodeLocalTaskID encodes an upstream operation name to a URL-safe base64 string.
// Used by Gemini/Vertex to store upstream names as task IDs.
func EncodeLocalTaskID(name string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(name))
}

// DecodeLocalTaskID decodes a base64-encoded upstream operation name.
func DecodeLocalTaskID(id string) (string, error) {
	b, err := base64.RawURLEncoding.DecodeString(id)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// BuildProxyURL constructs the video proxy URL using the public task ID.
// e.g., "https://your-server.com/v1/videos/task_xxxx/content"
func BuildProxyURL(taskID string) string {
	baseURL := strings.TrimRight(strings.TrimSpace(system_setting.VideoProxyAddress), "/")
	if baseURL == "" {
		baseURL = strings.TrimRight(strings.TrimSpace(system_setting.ServerAddress), "/")
	}
	return fmt.Sprintf("%s/v1/videos/%s/content", baseURL, taskID)
}

// BuildGrokVideoProxyURL builds a short-lived browser-readable URL for x.ai/Grok video media.
func BuildGrokVideoProxyURL(task *model.Task) string {
	if !IsGrokVideoProxyCandidate(task) {
		return ""
	}
	secret := videoProxySignSecret()
	if secret == "" {
		return ""
	}
	baseURL := strings.TrimRight(strings.TrimSpace(system_setting.VideoProxyAddress), "/")
	if baseURL == "" {
		baseURL = strings.TrimRight(strings.TrimSpace(system_setting.ServerAddress), "/")
	}
	if baseURL == "" {
		return ""
	}
	exp := time.Now().Add(24 * time.Hour).Unix()
	sig := signGrokVideoProxy(task.TaskID, exp, secret)
	return fmt.Sprintf("%s/video/grok/%s?exp=%d&sig=%s", baseURL, url.PathEscape(task.TaskID), exp, sig)
}

func VerifyGrokVideoProxySignature(taskID, expValue, sig string) bool {
	secret := videoProxySignSecret()
	if secret == "" || strings.TrimSpace(taskID) == "" || strings.TrimSpace(sig) == "" {
		return false
	}
	exp, err := strconv.ParseInt(strings.TrimSpace(expValue), 10, 64)
	if err != nil || exp < time.Now().Unix() {
		return false
	}
	want := signGrokVideoProxy(taskID, exp, secret)
	return subtle.ConstantTimeCompare([]byte(want), []byte(strings.TrimSpace(sig))) == 1
}

func IsGrokVideoProxyCandidate(task *model.Task) bool {
	if task == nil || task.Status != model.TaskStatusSuccess {
		return false
	}
	return IsAllowedGrokVideoURL(task.GetResultURL())
}

func IsAllowedGrokVideoURL(rawURL string) bool {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Host == "" {
		return false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" {
		host = strings.ToLower(parsed.Host)
		if splitHost, _, err := net.SplitHostPort(host); err == nil {
			host = splitHost
		}
	}
	return host == "x.ai" || strings.HasSuffix(host, ".x.ai")
}

func signGrokVideoProxy(taskID string, exp int64, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(taskID))
	_, _ = mac.Write([]byte("\n"))
	_, _ = mac.Write([]byte(strconv.FormatInt(exp, 10)))
	return hex.EncodeToString(mac.Sum(nil))
}

func videoProxySignSecret() string {
	if secret := strings.TrimSpace(system_setting.VideoProxySignSecret); secret != "" {
		return secret
	}
	return strings.TrimSpace(os.Getenv("VIDEO_PROXY_SIGN_SECRET"))
}

// PublicResultURL returns the URL exposed to API clients for a finished video task.
// mode=proxy keeps the current behavior; mode=downstream exposes the channel URL.
func PublicResultURL(task *model.Task) string {
	if task == nil {
		return ""
	}
	resultURL := strings.TrimSpace(task.GetResultURL())
	if resultURL == "" || task.Status != model.TaskStatusSuccess || strings.HasPrefix(resultURL, "data:") {
		return resultURL
	}
	if grokProxyURL := BuildGrokVideoProxyURL(task); grokProxyURL != "" {
		return grokProxyURL
	}
	if videoResultURLMode() == "downstream" {
		return resultURL
	}
	if strings.Contains(resultURL, "/v1/videos/"+task.TaskID+"/content") {
		return resultURL
	}
	return BuildProxyURL(task.TaskID)
}

func videoResultURLMode() string {
	switch strings.ToLower(strings.TrimSpace(system_setting.VideoResultURLMode)) {
	case "downstream", "upstream", "origin", "direct":
		return "downstream"
	default:
		return "proxy"
	}
}

// RewriteOpenAIVideoResultURL normalizes URL fields in OpenAI-compatible video responses.
func RewriteOpenAIVideoResultURL(data []byte, task *model.Task) ([]byte, error) {
	publicURL := PublicResultURL(task)
	if publicURL == "" || task == nil || task.Status != model.TaskStatusSuccess || strings.HasPrefix(publicURL, "data:") {
		return data, nil
	}

	var err error
	if data, err = sjson.SetBytes(data, "metadata.url", publicURL); err != nil {
		return nil, fmt.Errorf("set metadata url failed: %w", err)
	}

	for _, path := range []string{"url", "result_url", "video.url"} {
		if !gjson.GetBytes(data, path).Exists() {
			continue
		}
		if data, err = sjson.SetBytes(data, path, publicURL); err != nil {
			return nil, fmt.Errorf("set %s failed: %w", path, err)
		}
	}
	return data, nil
}

// Status-to-progress mapping constants for polling updates.
const (
	ProgressSubmitted  = "10%"
	ProgressQueued     = "20%"
	ProgressInProgress = "30%"
	ProgressComplete   = "100%"
)

// ---------------------------------------------------------------------------
// BaseBilling — embeddable no-op implementations for TaskAdaptor billing methods.
// Adaptors that do not need custom billing can embed this struct directly.
// ---------------------------------------------------------------------------

type BaseBilling struct{}

// EstimateBilling returns nil (no extra ratios; use base model price).
func (BaseBilling) EstimateBilling(_ *gin.Context, _ *relaycommon.RelayInfo) map[string]float64 {
	return nil
}

// AdjustBillingOnSubmit returns nil (no submit-time adjustment).
func (BaseBilling) AdjustBillingOnSubmit(_ *relaycommon.RelayInfo, _ []byte) map[string]float64 {
	return nil
}

// AdjustBillingOnComplete returns 0 (keep pre-charged amount).
func (BaseBilling) AdjustBillingOnComplete(_ *model.Task, _ *relaycommon.TaskInfo) int {
	return 0
}
