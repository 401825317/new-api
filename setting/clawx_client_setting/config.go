package clawx_client_setting

import "github.com/QuantumNous/new-api/setting/config"

type ClientSetting struct {
	Announcements        string `json:"announcements"`
	AnnouncementsEnabled bool   `json:"announcements_enabled"`
	Support              string `json:"support"`
	SupportEnabled       bool   `json:"support_enabled"`
	ModelOptions         string `json:"model_options"`
	Observability        string `json:"observability"`
	Features             string `json:"features"`
}

type Announcement struct {
	Id          string `json:"id"`
	Title       string `json:"title"`
	Content     string `json:"content"`
	Level       string `json:"level"`
	PublishedAt string `json:"publishedAt"`
	ExpiresAt   string `json:"expiresAt,omitempty"`
	Link        string `json:"link,omitempty"`
	Enabled     bool   `json:"enabled"`
}

type Support struct {
	Title       string           `json:"title"`
	Description string           `json:"description"`
	Contacts    []SupportContact `json:"contacts"`
	QrCodeUrl   string           `json:"qrCodeUrl"`
	WorkHours   string           `json:"workHours"`
	WechatId    string           `json:"wechatId"`
	ExtraNote   string           `json:"extraNote"`
}

type SupportContact struct {
	Id          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description"`
	QrCodeUrl   string `json:"qrCodeUrl"`
	WorkHours   string `json:"workHours"`
	WechatId    string `json:"wechatId"`
	ExtraNote   string `json:"extraNote"`
	Enabled     bool   `json:"enabled"`
}

type ModelOptions struct {
	Text  TextModelOptions  `json:"text"`
	Image ImageModelOptions `json:"image"`
	Video VideoModelOptions `json:"video"`
}

type TextModelOptions struct {
	DefaultModel         string            `json:"defaultModel"`
	FallbackModels       []string          `json:"fallbackModels"`
	DefaultThinkingLevel string            `json:"defaultThinkingLevel"`
	Models               []ClientModelItem `json:"models"`
}

type ImageModelOptions struct {
	DefaultModel   string                 `json:"defaultModel"`
	DefaultSize    string                 `json:"defaultSize"`
	DefaultQuality string                 `json:"defaultQuality"`
	Models         []ClientImageModelItem `json:"models"`
}

type VideoModelOptions struct {
	DefaultModel           string                 `json:"defaultModel"`
	DefaultSize            string                 `json:"defaultSize"`
	DefaultDurationSeconds int                    `json:"defaultDurationSeconds"`
	Models                 []ClientVideoModelItem `json:"models"`
}

type ClientModelItem struct {
	Id          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	Enabled     *bool  `json:"enabled,omitempty"`
	Visible     *bool  `json:"visible,omitempty"`
}

type Observability struct {
	Enabled                bool    `json:"enabled"`
	RolloutPercentage      float64 `json:"rolloutPercentage"`
	SentryDsn              string  `json:"sentryDsn,omitempty"`
	TunnelPath             string  `json:"tunnelPath"`
	CrashSampleRate        float64 `json:"crashSampleRate"`
	HandledErrorSampleRate float64 `json:"handledErrorSampleRate"`
	TracesSampleRate       float64 `json:"tracesSampleRate"`
	ArtifactSampleRate     float64 `json:"artifactSampleRate"`
	MaxEventsPerHour       int     `json:"maxEventsPerHour"`
}

const (
	observabilityTunnelPath       = "/api/clawx/observability/envelope"
	observabilityMaxEventsPerHour = 30
)

type FeatureGate struct {
	Enabled           bool    `json:"enabled"`
	RolloutPercentage float64 `json:"rolloutPercentage"`
	// InternalUserIds is server-only. It lets explicitly approved accounts
	// bypass a percentage rollout while the public client contract stays opaque.
	InternalUserIds []int `json:"internalUserIds,omitempty"`
}

type PublicFeatureGate struct {
	Enabled           bool    `json:"enabled"`
	RolloutPercentage float64 `json:"rolloutPercentage"`
	Eligible          bool    `json:"eligible"`
}

type ArtifactFeature struct {
	FeatureGate
	ModelAlias    string `json:"modelAlias"`
	UpstreamModel string `json:"upstreamModel"`
	PolicyVersion string `json:"policyVersion"`
}

type EcommerceMainImageFeature struct {
	FeatureGate
	SkillVersion string `json:"skillVersion"`
}

type Features struct {
	Artifacts          ArtifactFeature           `json:"artifacts"`
	EcommerceMainImage EcommerceMainImageFeature `json:"ecommerceMainImage"`
	HtmlPreview        FeatureGate               `json:"htmlPreview"`
	LongTermRules      FeatureGate               `json:"longTermRules"`
}

type PublicArtifactFeature struct {
	PublicFeatureGate
	ModelAlias    string `json:"modelAlias"`
	PolicyVersion string `json:"policyVersion"`
}

type PublicEcommerceMainImageFeature struct {
	PublicFeatureGate
	SkillVersion string `json:"skillVersion"`
}

type PublicFeatures struct {
	Artifacts          PublicArtifactFeature           `json:"artifacts"`
	EcommerceMainImage PublicEcommerceMainImageFeature `json:"ecommerceMainImage"`
	HtmlPreview        PublicFeatureGate               `json:"htmlPreview"`
	LongTermRules      PublicFeatureGate               `json:"longTermRules"`
}

const (
	managedArtifactV1Alias         = "uclaw-artifact-v1"
	managedArtifactV1UpstreamModel = "smart-latest"
)

type ClientImageModelItem struct {
	Id              string   `json:"id"`
	Label           string   `json:"label"`
	Description     string   `json:"description,omitempty"`
	Sizes           []string `json:"sizes"`
	Qualities       []string `json:"qualities"`
	DefaultSize     string   `json:"defaultSize,omitempty"`
	DefaultQuality  string   `json:"defaultQuality,omitempty"`
	SupportsEditing bool     `json:"supportsEditing"`
	Enabled         *bool    `json:"enabled,omitempty"`
}

type ClientVideoModelItem struct {
	Id                     string   `json:"id"`
	Label                  string   `json:"label"`
	Description            string   `json:"description,omitempty"`
	Modes                  []string `json:"modes"`
	Sizes                  []string `json:"sizes"`
	Durations              []int    `json:"durations"`
	DefaultSize            string   `json:"defaultSize,omitempty"`
	DefaultDurationSeconds int      `json:"defaultDurationSeconds,omitempty"`
	RequiresImage          bool     `json:"requiresImage"`
	Enabled                *bool    `json:"enabled,omitempty"`
}

const defaultModelOptionsJSON = `{"text":{"defaultModel":"smart-latest","fallbackModels":[],"defaultThinkingLevel":"medium","models":[{"id":"smart-latest","label":"智能路由","description":"自动选择合适的文本模型。","enabled":true},{"id":"uclaw-artifact-v1","label":"UClaw Artifact v1","description":"Versioned artifact runtime route.","enabled":true,"visible":false},{"id":"qwen-latest","label":"通义千问","enabled":true},{"id":"deepseek-latest","label":"DeepSeek","enabled":true},{"id":"doubao-latest","label":"豆包","enabled":true},{"id":"kimi-latest","label":"Kimi","enabled":true},{"id":"glm-latest","label":"智谱 GLM","enabled":true}]},"image":{"defaultModel":"gpt-image-2","defaultSize":"1024x1024","defaultQuality":"medium","models":[{"id":"gpt-image-2","label":"Image 2","description":"Image generation and editing.","sizes":["1024x1024","2048x2048","3840x2160"],"qualities":["low","medium","high"],"defaultSize":"1024x1024","defaultQuality":"medium","supportsEditing":true,"enabled":true}]},"video":{"defaultModel":"grok-image-video","defaultSize":"1280x720","defaultDurationSeconds":6,"models":[{"id":"grok-image-video","label":"Grok Video","description":"Supports text-to-video and image-to-video.","modes":["text-to-video","image-to-video"],"sizes":["1280x720","720x1280","1024x1024"],"durations":[6,10],"defaultSize":"1280x720","defaultDurationSeconds":6,"requiresImage":false,"enabled":true},{"id":"grok-video-1.5","label":"Grok Video 1.5","description":"Image-to-video model that requires one reference image.","modes":["image-to-video"],"sizes":["1280x720","720x1280","1024x1024"],"durations":[6,10],"defaultSize":"1280x720","defaultDurationSeconds":6,"requiresImage":true,"enabled":true}]}}`

const defaultObservabilityJSON = `{"enabled":false,"rolloutPercentage":0,"tunnelPath":"/api/clawx/observability/envelope","crashSampleRate":1,"handledErrorSampleRate":0.2,"tracesSampleRate":0.05,"artifactSampleRate":0.2,"maxEventsPerHour":30}`
const defaultFeaturesJSON = `{"artifacts":{"enabled":false,"rolloutPercentage":0,"modelAlias":"uclaw-artifact-v1","upstreamModel":"","policyVersion":"v1"},"ecommerceMainImage":{"enabled":false,"rolloutPercentage":0,"skillVersion":"v1"},"htmlPreview":{"enabled":false,"rolloutPercentage":0},"longTermRules":{"enabled":false,"rolloutPercentage":0}}`

var defaultClientSetting = ClientSetting{
	Announcements:        "[]",
	AnnouncementsEnabled: false,
	Support:              "{}",
	SupportEnabled:       false,
	ModelOptions:         defaultModelOptionsJSON,
	Observability:        defaultObservabilityJSON,
	Features:             defaultFeaturesJSON,
}

var clientSetting = defaultClientSetting

func init() {
	config.GlobalConfig.Register("clawx_client_setting", &clientSetting)
}

func GetClientSetting() *ClientSetting {
	return &clientSetting
}
