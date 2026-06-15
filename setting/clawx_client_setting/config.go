package clawx_client_setting

import "github.com/QuantumNous/new-api/setting/config"

type ClientSetting struct {
	Announcements        string `json:"announcements"`
	AnnouncementsEnabled bool   `json:"announcements_enabled"`
	Support              string `json:"support"`
	SupportEnabled       bool   `json:"support_enabled"`
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

var defaultClientSetting = ClientSetting{
	Announcements:        "[]",
	AnnouncementsEnabled: false,
	Support:              "{}",
	SupportEnabled:       false,
}

var clientSetting = defaultClientSetting

func init() {
	config.GlobalConfig.Register("clawx_client_setting", &clientSetting)
}

func GetClientSetting() *ClientSetting {
	return &clientSetting
}
