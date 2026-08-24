package clawx_client_setting

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestGetModelOptionsKeepsConfiguredGrokVideoDurations(t *testing.T) {
	original := clientSetting
	t.Cleanup(func() {
		clientSetting = original
	})
	clientSetting.ModelOptions = `{"video":{"defaultModel":"grok-image-video","defaultSize":"1280x720","defaultDurationSeconds":10,"models":[{"id":"grok-image-video","sizes":["854x480","1280x720","720x1280"],"durations":[6,10],"defaultSize":"1280x720","defaultDurationSeconds":10},{"id":"grok-video-1.5","sizes":["1280x720"],"durations":[15],"defaultSize":"1280x720","defaultDurationSeconds":15},{"id":"other-video","sizes":["640x360"],"durations":[5,15],"defaultDurationSeconds":15}]}}`

	options := GetModelOptions()
	require.Len(t, options.Video.Models, 3)
	assert.Equal(t, 10, options.Video.DefaultDurationSeconds)
	assert.Equal(t, []int{6, 10}, options.Video.Models[0].Durations)
	assert.Equal(t, []string{"854x480", "1280x720", "720x1280", "1920x1080"}, options.Video.Models[0].Sizes)
	assert.Equal(t, 10, options.Video.Models[0].DefaultDurationSeconds)
	assert.Equal(t, []int{15}, options.Video.Models[1].Durations)
	assert.Equal(t, []string{"854x480", "1280x720", "720x1280", "1920x1080"}, options.Video.Models[1].Sizes)
	assert.Equal(t, 15, options.Video.Models[1].DefaultDurationSeconds)
	assert.Equal(t, []int{5, 15}, options.Video.Models[2].Durations)
	assert.Equal(t, []string{"640x360"}, options.Video.Models[2].Sizes)
	assert.Equal(t, 15, options.Video.Models[2].DefaultDurationSeconds)
}

func TestDefaultModelOptionsKeepConfiguredGrokVideoDurations(t *testing.T) {
	var options ModelOptions
	require.NoError(t, common.UnmarshalJsonStr(defaultModelOptionsJSON, &options))
	options = normalizeModelOptions(options)
	require.Len(t, options.Video.Models, 2)
	assert.Equal(t, 6, options.Video.DefaultDurationSeconds)
	for _, model := range options.Video.Models {
		assert.Equal(t, []int{6, 10}, model.Durations)
		assert.Equal(t, 6, model.DefaultDurationSeconds)
	}
	assert.Equal(t, []string{"854x480", "1280x720", "720x1280", "1920x1080"}, options.Video.Models[0].Sizes)
	assert.Equal(t, []string{"854x480", "1280x720", "720x1280", "1920x1080"}, options.Video.Models[1].Sizes)
}

func TestValidateModelOptionsAcceptsDocumentedGrokVideoDurations(t *testing.T) {
	validEightSeconds := `{"video":{"defaultModel":"grok-image-video","defaultDurationSeconds":8,"models":[{"id":"grok-image-video","durations":[6,8,10,15],"defaultDurationSeconds":8}]}}`
	require.NoError(t, ValidateClientSettings(validEightSeconds, "ModelOptions"))

	invalid := `{"video":{"defaultModel":"grok-image-video","defaultDurationSeconds":16,"models":[{"id":"grok-image-video","durations":[6,10,16],"defaultDurationSeconds":16}]}}`
	require.Error(t, ValidateClientSettings(invalid, "ModelOptions"))

	valid := `{"video":{"defaultModel":"grok-image-video","defaultDurationSeconds":15,"models":[{"id":"grok-image-video","durations":[6,10,15],"defaultDurationSeconds":15}]}}`
	require.NoError(t, ValidateClientSettings(valid, "ModelOptions"))
}

func TestResolveVideoDurationUsesConfiguredMaximumForFallback(t *testing.T) {
	original := clientSetting
	t.Cleanup(func() { clientSetting = original })
	clientSetting.ModelOptions = `{"video":{"defaultModel":"grok-image-video","models":[{"id":"grok-image-video","durations":[6,8,15],"defaultDurationSeconds":8}]}}`

	duration, ok := ResolveVideoDuration("grok-image-video", 8)
	require.True(t, ok)
	require.Equal(t, 8, duration)

	duration, ok = ResolveVideoDuration("grok-image-video", 10)
	require.True(t, ok)
	require.Equal(t, 15, duration)

	duration, ok = ResolveVideoDuration("grok-imagine-1.5-video-ext", 0)
	require.True(t, ok)
	require.Equal(t, 15, duration)
}
