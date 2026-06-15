package clawx_client_setting

import "testing"

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
