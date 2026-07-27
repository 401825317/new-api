package clawx_client_setting

import (
	"encoding/json"
	"testing"
)

func TestValidateSupportAcceptsMultipleContacts(t *testing.T) {
	payload := `{
		"title":"帮助与客服",
		"description":"请选择合适的客服入口",
		"contacts":[
			{
				"id":"official",
				"label":"官方客服",
				"description":"账号和充值问题",
				"qrCodeUrl":"https://example.com/official.png",
				"workHours":"工作日 9:00-18:00",
				"wechatId":"support",
				"extraNote":"请备注账号",
				"enabled":true
			},
			{
				"id":"business",
				"label":"商务合作",
				"qrCodeUrl":"https://example.com/business.png",
				"enabled":true
			}
		]
	}`

	if err := ValidateClientSettings(payload, "Support"); err != nil {
		t.Fatalf("expected support payload to be valid, got %v", err)
	}
}

func TestValidateSupportRejectsDuplicateContactID(t *testing.T) {
	payload := `{
		"contacts":[
			{"id":"official","label":"官方客服","qrCodeUrl":"https://example.com/a.png","enabled":true},
			{"id":"official","label":"商务合作","qrCodeUrl":"https://example.com/b.png","enabled":true}
		]
	}`

	if err := ValidateClientSettings(payload, "Support"); err == nil {
		t.Fatal("expected duplicate support contact id to be rejected")
	}
}

func TestNormalizeSupportContactsKeepsLegacySingleQRCode(t *testing.T) {
	contacts := normalizeSupportContacts(Support{
		Title:       "联系官方客服",
		Description: "扫码咨询",
		QrCodeUrl:   "https://example.com/support.png",
		WorkHours:   "工作日",
		WechatId:    "support",
		ExtraNote:   "请备注账号",
	})

	if len(contacts) != 1 {
		t.Fatalf("expected one legacy contact, got %d", len(contacts))
	}
	if contacts[0].Label != "联系官方客服" {
		t.Fatalf("expected legacy title as contact label, got %q", contacts[0].Label)
	}
	if contacts[0].QrCodeUrl != "https://example.com/support.png" {
		t.Fatalf("unexpected qr code url: %q", contacts[0].QrCodeUrl)
	}
}

func TestNormalizeSupportContactsFiltersDisabledAndEmptyQRCode(t *testing.T) {
	contacts := normalizeSupportContacts(Support{
		Contacts: []SupportContact{
			{Id: "disabled", Label: "禁用客服", QrCodeUrl: "https://example.com/disabled.png", Enabled: false},
			{Id: "empty", Label: "空二维码", Enabled: true},
			{Id: "official", Label: "官方客服", QrCodeUrl: "https://example.com/official.png", Enabled: true},
		},
	})

	if len(contacts) != 1 {
		t.Fatalf("expected one active contact, got %d", len(contacts))
	}
	if contacts[0].Id != "official" {
		t.Fatalf("expected official contact, got %q", contacts[0].Id)
	}
}

func TestGetModelOptionsRestrictsStaleGrokVideoDurations(t *testing.T) {
	original := clientSetting
	t.Cleanup(func() {
		clientSetting = original
	})
	clientSetting.ModelOptions = `{"video":{"defaultModel":"grok-image-video","defaultDurationSeconds":15,"models":[{"id":"grok-image-video","durations":[6,10,15],"defaultDurationSeconds":15},{"id":"grok-video-1.5","durations":[15],"defaultDurationSeconds":15},{"id":"other-video","durations":[5,15],"defaultDurationSeconds":15}]}}`

	options := GetModelOptions()
	if options.Video.DefaultDurationSeconds != 6 {
		t.Fatalf("expected Grok video default duration to be 6, got %d", options.Video.DefaultDurationSeconds)
	}
	if len(options.Video.Models) != 3 {
		t.Fatalf("expected three video models, got %d", len(options.Video.Models))
	}
	for _, model := range options.Video.Models[:2] {
		assertDurations(t, model.Durations, []int{6, 10})
		if model.DefaultDurationSeconds != 6 {
			t.Fatalf("expected %s default duration to be 6, got %d", model.Id, model.DefaultDurationSeconds)
		}
	}
	assertDurations(t, options.Video.Models[2].Durations, []int{5, 15})
	if options.Video.Models[2].DefaultDurationSeconds != 15 {
		t.Fatalf("expected non-Grok video duration to stay 15, got %d", options.Video.Models[2].DefaultDurationSeconds)
	}
}

func TestDefaultModelOptionsOnlyExposeSupportedGrokVideoDurations(t *testing.T) {
	var options ModelOptions
	if err := json.Unmarshal([]byte(defaultModelOptionsJSON), &options); err != nil {
		t.Fatalf("unmarshal default model options: %v", err)
	}
	if options.Video.DefaultDurationSeconds != 6 {
		t.Fatalf("expected default video duration to be 6, got %d", options.Video.DefaultDurationSeconds)
	}
	for _, model := range options.Video.Models {
		assertDurations(t, model.Durations, []int{6, 10})
		if model.DefaultDurationSeconds != 6 {
			t.Fatalf("expected %s default duration to be 6, got %d", model.Id, model.DefaultDurationSeconds)
		}
	}
}

func TestValidateModelOptionsRejectsUnsupportedGrokVideoDurations(t *testing.T) {
	invalid := `{"video":{"defaultModel":"grok-image-video","defaultDurationSeconds":15,"models":[{"id":"grok-image-video","durations":[6,10,15],"defaultDurationSeconds":15}]}}`
	if err := ValidateClientSettings(invalid, "ModelOptions"); err == nil {
		t.Fatal("expected unsupported Grok video duration to be rejected")
	}

	valid := `{"video":{"defaultModel":"grok-image-video","defaultDurationSeconds":6,"models":[{"id":"grok-image-video","durations":[6,10],"defaultDurationSeconds":6}]}}`
	if err := ValidateClientSettings(valid, "ModelOptions"); err != nil {
		t.Fatalf("expected 6/10 Grok video durations to be valid, got %v", err)
	}
}

func assertDurations(t *testing.T, got []int, want []int) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("duration count mismatch: got %v, want %v", got, want)
	}
	for i, duration := range want {
		if got[i] != duration {
			t.Fatalf("duration mismatch: got %v, want %v", got, want)
		}
	}
}
