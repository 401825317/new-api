package clawx_client_setting

import "github.com/QuantumNous/new-api/setting/config"

type ClientSetting struct {
	Announcements        string `json:"announcements"`
	AnnouncementsEnabled bool   `json:"announcements_enabled"`
	Support              string `json:"support"`
	SupportEnabled       bool   `json:"support_enabled"`
	ModelOptions         string `json:"model_options"`
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
	DefaultModel string            `json:"defaultModel"`
	Models       []ClientModelItem `json:"models"`
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
}

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

const defaultModelOptionsJSON = `{"text":{"defaultModel":"smart-latest","models":[{"id":"smart-latest","label":"智能路由","description":"自动选择合适的文本模型。","enabled":true},{"id":"qwen-latest","label":"通义千问","enabled":true},{"id":"deepseek-latest","label":"DeepSeek","enabled":true},{"id":"doubao-latest","label":"豆包","enabled":true},{"id":"kimi-latest","label":"Kimi","enabled":true},{"id":"glm-latest","label":"智谱 GLM","enabled":true}]},"image":{"defaultModel":"gpt-image-2","defaultSize":"1024x1024","defaultQuality":"medium","models":[{"id":"gpt-image-2","label":"Image 2","description":"Image generation and editing.","sizes":["1024x1024","2048x2048","3840x2160"],"qualities":["low","medium","high"],"defaultSize":"1024x1024","defaultQuality":"medium","supportsEditing":true,"enabled":true}]},"video":{"defaultModel":"grok-image-video","defaultSize":"1280x720","defaultDurationSeconds":4,"models":[{"id":"grok-image-video","label":"Grok Video","description":"Supports text-to-video and image-to-video.","modes":["text-to-video","image-to-video"],"sizes":["1280x720","720x1280","1024x1024"],"durations":[4,6,8,10,12,15],"defaultSize":"1280x720","defaultDurationSeconds":4,"requiresImage":false,"enabled":true},{"id":"grok-video-1.5","label":"Grok Video 1.5","description":"Image-to-video model that requires one reference image.","modes":["image-to-video"],"sizes":["1280x720","720x1280","1024x1024"],"durations":[4,6,8,10,12,15],"defaultSize":"1280x720","defaultDurationSeconds":4,"requiresImage":true,"enabled":true}]}}`

var defaultClientSetting = ClientSetting{
	Announcements:        "[]",
	AnnouncementsEnabled: false,
	Support:              "{}",
	SupportEnabled:       false,
	ModelOptions:         defaultModelOptionsJSON,
}

var clientSetting = defaultClientSetting

func init() {
	config.GlobalConfig.Register("clawx_client_setting", &clientSetting)
}

func GetClientSetting() *ClientSetting {
	return &clientSetting
}
