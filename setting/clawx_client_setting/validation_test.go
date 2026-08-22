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

func TestGetModelOptionsPublishesSupportedGrokVideoCapabilities(t *testing.T) {
	original := clientSetting
	t.Cleanup(func() {
		clientSetting = original
	})
	clientSetting.ModelOptions = `{"video":{"defaultModel":"grok-image-video","defaultSize":"1280x720","defaultDurationSeconds":15,"models":[{"id":"grok-image-video","sizes":["854x480","1280x720","720x1280"],"durations":[6,10,15],"defaultSize":"1280x720","defaultDurationSeconds":15},{"id":"grok-video-1.5","sizes":["1280x720"],"durations":[15],"defaultSize":"1280x720","defaultDurationSeconds":15},{"id":"other-video","sizes":["640x360"],"durations":[5,15],"defaultDurationSeconds":15}]}}`

	options := GetModelOptions()
	require.Len(t, options.Video.Models, 3)
	assert.Equal(t, 6, options.Video.DefaultDurationSeconds)
	assert.Equal(t, []int{6, 10}, options.Video.Models[0].Durations)
	assert.Equal(t, []string{"854x480", "1280x720", "720x1280", "1920x1080"}, options.Video.Models[0].Sizes)
	assert.Equal(t, 6, options.Video.Models[0].DefaultDurationSeconds)
	assert.Equal(t, []int{6, 10}, options.Video.Models[1].Durations)
	assert.Equal(t, []string{"854x480", "1280x720", "720x1280", "1920x1080"}, options.Video.Models[1].Sizes)
	assert.Equal(t, 6, options.Video.Models[1].DefaultDurationSeconds)
	assert.Equal(t, []int{5, 15}, options.Video.Models[2].Durations)
	assert.Equal(t, []string{"640x360"}, options.Video.Models[2].Sizes)
	assert.Equal(t, 15, options.Video.Models[2].DefaultDurationSeconds)
}

func TestDefaultModelOptionsNormalizeToSupportedGrokVideoCapabilities(t *testing.T) {
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

func TestValidateModelOptionsRejectsUnsupportedGrokVideoDurations(t *testing.T) {
	invalid := `{"video":{"defaultModel":"grok-image-video","defaultDurationSeconds":8,"models":[{"id":"grok-image-video","durations":[6,8,10,15],"defaultDurationSeconds":8}]}}`
	require.Error(t, ValidateClientSettings(invalid, "ModelOptions"))

	invalidFifteenSeconds := `{"video":{"defaultModel":"grok-image-video","defaultDurationSeconds":15,"models":[{"id":"grok-image-video","durations":[6,10,15],"defaultDurationSeconds":15}]}}`
	require.Error(t, ValidateClientSettings(invalidFifteenSeconds, "ModelOptions"))

	valid := `{"video":{"defaultModel":"grok-image-video","defaultDurationSeconds":10,"models":[{"id":"grok-image-video","durations":[6,10],"defaultDurationSeconds":10}]}}`
	require.NoError(t, ValidateClientSettings(valid, "ModelOptions"))
}

func TestModelOptionsDefaultThinkingLevelContract(t *testing.T) {
	original := clientSetting
	t.Cleanup(func() {
		clientSetting = original
	})

	clientSetting.ModelOptions = `{"text":{"defaultModel":"smart-latest","defaultThinkingLevel":" HIGH ","models":[{"id":"smart-latest","enabled":true}]}}`
	options := GetModelOptions()
	assert.Equal(t, "high", options.Text.DefaultThinkingLevel)

	clientSetting.ModelOptions = `{"text":{"defaultModel":"smart-latest","models":[{"id":"smart-latest","enabled":true}]}}`
	options = GetModelOptions()
	assert.Equal(t, "medium", options.Text.DefaultThinkingLevel)

	require.NoError(t, ValidateClientSettings(`{"text":{"defaultThinkingLevel":"off"}}`, "ModelOptions"))
	require.Error(t, ValidateClientSettings(`{"text":{"defaultThinkingLevel":"extreme"}}`, "ModelOptions"))
}

func TestTextFallbackModelContract(t *testing.T) {
	valid := `{"text":{"defaultModel":"smart-latest","fallbackModels":["qwen-latest","hidden-runtime"],"models":[{"id":"smart-latest","enabled":true},{"id":"qwen-latest","enabled":true},{"id":"hidden-runtime","enabled":true,"visible":false}]}}`
	require.NoError(t, ValidateClientSettings(valid, "ModelOptions"))

	tests := []struct {
		name    string
		payload string
		match   string
	}{
		{"primary", `{"text":{"defaultModel":"smart-latest","fallbackModels":["smart-latest"],"models":[{"id":"smart-latest","enabled":true}]}}`, "primary"},
		{"duplicate", `{"text":{"defaultModel":"smart-latest","fallbackModels":["qwen-latest","qwen-latest"],"models":[{"id":"smart-latest","enabled":true},{"id":"qwen-latest","enabled":true}]}}`, "duplicated"},
		{"unknown", `{"text":{"defaultModel":"smart-latest","fallbackModels":["missing-model"],"models":[{"id":"smart-latest","enabled":true}]}}`, "enabled text model"},
		{"disabled", `{"text":{"defaultModel":"smart-latest","fallbackModels":["disabled-model"],"models":[{"id":"smart-latest","enabled":true},{"id":"disabled-model","enabled":false}]}}`, "enabled text model"},
		{"private artifact", `{"text":{"defaultModel":"smart-latest","fallbackModels":["uclaw-artifact-v1"],"models":[{"id":"smart-latest","enabled":true},{"id":"uclaw-artifact-v1","enabled":true,"visible":false}]}}`, "private artifact alias"},
		{"empty", `{"text":{"defaultModel":"smart-latest","fallbackModels":["  "],"models":[{"id":"smart-latest","enabled":true}]}}`, "empty"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.ErrorContains(t, ValidateClientSettings(test.payload, "ModelOptions"), test.match)
		})
	}
}

func TestGetModelOptionsNormalizesFallbackModelsFailClosed(t *testing.T) {
	original := clientSetting
	t.Cleanup(func() { clientSetting = original })
	clientSetting.ModelOptions = `{"text":{"defaultModel":"smart-latest","fallbackModels":[" qwen-latest ","missing","qwen-latest","smart-latest","hidden-runtime","uclaw-artifact-v1"],"models":[{"id":"smart-latest","enabled":true},{"id":"qwen-latest","enabled":true},{"id":"disabled-model","enabled":false},{"id":"hidden-runtime","enabled":true,"visible":false},{"id":"uclaw-artifact-v1","enabled":true,"visible":false}]}}`

	options := GetModelOptions()
	assert.Equal(t, []string{"qwen-latest", "hidden-runtime"}, options.Text.FallbackModels)

	clientSetting.ModelOptions = `{"text":{"defaultModel":"smart-latest","models":[{"id":"smart-latest","enabled":true}]}}`
	options = GetModelOptions()
	assert.Empty(t, options.Text.FallbackModels)
	require.NotNil(t, options.Text.FallbackModels)
	encoded, err := common.Marshal(options)
	require.NoError(t, err)
	assert.Contains(t, string(encoded), `"fallbackModels":[]`)
}

func TestValidateFeaturesRequiresLockedArtifactRoute(t *testing.T) {
	valid := `{"artifacts":{"enabled":true,"rolloutPercentage":10,"modelAlias":"uclaw-artifact-v1","upstreamModel":"smart-latest","policyVersion":"v1"},"ecommerceMainImage":{"enabled":false,"rolloutPercentage":0,"skillVersion":"v1"}}`
	require.NoError(t, ValidateClientSettings(valid, "Features"))

	missingLegacyUpstream := `{"artifacts":{"enabled":true,"rolloutPercentage":10,"modelAlias":"uclaw-artifact-v1","policyVersion":"v1"}}`
	require.NoError(t, ValidateClientSettings(missingLegacyUpstream, "Features"))

	recursiveUpstream := `{"artifacts":{"enabled":true,"rolloutPercentage":10,"modelAlias":"uclaw-artifact-v1","upstreamModel":"uclaw-artifact-v2","policyVersion":"v1"}}`
	require.ErrorContains(t, ValidateClientSettings(recursiveUpstream, "Features"), "cannot reference")

	prefixedRecursiveUpstream := `{"artifacts":{"enabled":true,"rolloutPercentage":10,"modelAlias":"uclaw-artifact-v1","upstreamModel":"openai/uclaw-artifact-v2","policyVersion":"v1"}}`
	require.ErrorContains(t, ValidateClientSettings(prefixedRecursiveUpstream, "Features"), "cannot reference")

	invalidAlias := `{"artifacts":{"enabled":true,"rolloutPercentage":10,"modelAlias":"uclaw-artifact-vnext","upstreamModel":"smart-latest","policyVersion":"v1"}}`
	require.ErrorContains(t, ValidateClientSettings(invalidAlias, "Features"), "modelAlias")

	unknownVersion := `{"artifacts":{"enabled":true,"rolloutPercentage":10,"modelAlias":"uclaw-artifact-v2","upstreamModel":"smart-latest","policyVersion":"v2"}}`
	require.ErrorContains(t, ValidateClientSettings(unknownVersion, "Features"), "locked artifact route")

	retargetedV1 := `{"artifacts":{"enabled":true,"rolloutPercentage":10,"modelAlias":"uclaw-artifact-v1","upstreamModel":"another-model","policyVersion":"v1"}}`
	require.ErrorContains(t, ValidateClientSettings(retargetedV1, "Features"), "cannot override")

	invalidVersion := `{"artifacts":{"enabled":true,"rolloutPercentage":10,"modelAlias":"uclaw-artifact-v1","upstreamModel":"smart-latest","policyVersion":"latest"}}`
	require.ErrorContains(t, ValidateClientSettings(invalidVersion, "Features"), "vN format")

	ecommerceWithoutArtifacts := `{"artifacts":{"enabled":false,"rolloutPercentage":0},"ecommerceMainImage":{"enabled":true,"rolloutPercentage":10,"skillVersion":"v1"}}`
	require.ErrorContains(t, ValidateClientSettings(ecommerceWithoutArtifacts, "Features"), "requires artifacts")
}

func TestManagedArtifactFeatureEligibleEnforcesServerRollout(t *testing.T) {
	features := defaultFeatures()
	features.Artifacts.Enabled = true
	features.Artifacts.RolloutPercentage = 100
	installationId := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	assert.True(t, ManagedArtifactFeatureEligible(features, 42, installationId))
	assert.False(t, ManagedArtifactFeatureEligible(features, 42, "not-a-hash"))

	features.Artifacts.RolloutPercentage = 0
	assert.False(t, ManagedArtifactFeatureEligible(features, 42, installationId))

	features.Artifacts.InternalUserIds = []int{42}
	assert.True(t, ManagedArtifactFeatureEligible(features, 42, ""))
	assert.False(t, ManagedArtifactFeatureEligible(features, 43, installationId))

	features.Artifacts.Enabled = false
	assert.False(t, ManagedArtifactFeatureEligible(features, 42, installationId))
}

func TestManagedArtifactRolloutUsesStableCrossClientVectors(t *testing.T) {
	features := defaultFeatures()
	features.Artifacts.Enabled = true
	features.Artifacts.RolloutPercentage = 35
	assert.False(t, ManagedArtifactFeatureEligible(features, 0, "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"), "bucket 9961")
	assert.True(t, ManagedArtifactFeatureEligible(features, 0, "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"), "bucket 3084")
	assert.True(t, ManagedArtifactFeatureEligible(features, 0, "0000000000000000000000000000000000000000000000000000000000000000"), "bucket 3437")
}

func TestPublicFeaturesDoNotExposeInternalRolloutAccounts(t *testing.T) {
	original := clientSetting
	t.Cleanup(func() { clientSetting = original })
	clientSetting.Features = `{"artifacts":{"enabled":true,"rolloutPercentage":10,"internalUserIds":[7],"modelAlias":"uclaw-artifact-v1","upstreamModel":"smart-latest","policyVersion":"v1"},"ecommerceMainImage":{"enabled":false,"rolloutPercentage":0,"internalUserIds":[8],"skillVersion":"v1"}}`

	public := GetPublicFeatures()
	encoded, err := common.Marshal(public)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "internalUserIds")
	assert.NotContains(t, string(encoded), "[7]")
	assert.NotContains(t, string(encoded), "[8]")
}

func TestPublicFeaturesExposeOnlyServerDecidedInternalEligibility(t *testing.T) {
	original := clientSetting
	t.Cleanup(func() { clientSetting = original })
	clientSetting.Features = `{"artifacts":{"enabled":true,"rolloutPercentage":0,"internalUserIds":[7],"modelAlias":"uclaw-artifact-v1","policyVersion":"v1"},"ecommerceMainImage":{"enabled":false,"rolloutPercentage":0,"skillVersion":"v1"},"htmlPreview":{"enabled":false,"rolloutPercentage":0},"longTermRules":{"enabled":false,"rolloutPercentage":0}}`

	eligible := GetPublicFeaturesForClient(7, "")
	assert.True(t, eligible.Artifacts.Eligible)
	assert.False(t, GetPublicFeaturesForClient(8, "").Artifacts.Eligible)
	encoded, err := common.Marshal(eligible)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "internalUserIds")
	assert.NotContains(t, string(encoded), "[7]")
}

func TestValidateObservabilityRequiresRoutableSentryTunnel(t *testing.T) {
	valid := `{"enabled":true,"sentryDsn":"https://public@sentry.example.com/42","tunnelPath":"/api/clawx/observability/envelope","crashSampleRate":1,"handledErrorSampleRate":0.2,"tracesSampleRate":0.05,"artifactSampleRate":0.2,"maxEventsPerHour":30}`
	require.NoError(t, ValidateClientSettings(valid, "Observability"))

	missingPublicKey := `{"enabled":true,"sentryDsn":"https://sentry.example.com/42","tunnelPath":"/api/clawx/observability/envelope","maxEventsPerHour":30}`
	require.ErrorContains(t, ValidateClientSettings(missingPublicKey, "Observability"), "public key")

	unknownTunnel := `{"enabled":true,"sentryDsn":"https://public@sentry.example.com/42","tunnelPath":"/api/clawx/other","maxEventsPerHour":30}`
	require.ErrorContains(t, ValidateClientSettings(unknownTunnel, "Observability"), "observability/envelope")

	missingProjectID := `{"enabled":true,"sentryDsn":"https://public@sentry.example.com","tunnelPath":"/api/clawx/observability/envelope","maxEventsPerHour":30}`
	require.ErrorContains(t, ValidateClientSettings(missingProjectID, "Observability"), "project ID")

	secretInUserInfo := `{"enabled":true,"sentryDsn":"https://public:private-secret@sentry.example.com/42","tunnelPath":"/api/clawx/observability/envelope","maxEventsPerHour":30}`
	require.ErrorContains(t, ValidateClientSettings(secretInUserInfo, "Observability"), "password or secret")
	disabledWithSecret := `{"enabled":false,"sentryDsn":"https://public:private-secret@sentry.example.com/42","tunnelPath":"/api/clawx/observability/envelope","maxEventsPerHour":30}`
	require.ErrorContains(t, ValidateClientSettings(disabledWithSecret, "Observability"), "password or secret")

	queryString := `{"enabled":true,"sentryDsn":"https://public@sentry.example.com/42?token=private-secret","tunnelPath":"/api/clawx/observability/envelope","maxEventsPerHour":30}`
	require.ErrorContains(t, ValidateClientSettings(queryString, "Observability"), "query or fragment")

	overServerLimit := `{"enabled":true,"sentryDsn":"https://public@sentry.example.com/42","tunnelPath":"/api/clawx/observability/envelope","maxEventsPerHour":31}`
	require.ErrorContains(t, ValidateClientSettings(overServerLimit, "Observability"), "between 1 and 30")
}

func TestGetObservabilityAppliesDefaultsAndFailsClosed(t *testing.T) {
	original := clientSetting
	t.Cleanup(func() { clientSetting = original })

	clientSetting.Observability = `{"enabled":true,"sentryDsn":"https://public@sentry.example.com/42"}`
	settings := GetObservability()
	assert.True(t, settings.Enabled)
	assert.Equal(t, observabilityTunnelPath, settings.TunnelPath)
	assert.Equal(t, 1.0, settings.CrashSampleRate)
	assert.Equal(t, 0.2, settings.HandledErrorSampleRate)
	assert.Equal(t, 0.05, settings.TracesSampleRate)
	assert.Equal(t, 0.2, settings.ArtifactSampleRate)
	assert.Equal(t, observabilityMaxEventsPerHour, settings.MaxEventsPerHour)

	clientSetting.Observability = `{"enabled":false,"sentryDsn":"https://public@sentry.example.com/42","maxEventsPerHour":30}`
	settings = GetObservability()
	assert.False(t, settings.Enabled)

	clientSetting.Observability = `{"enabled":true,"sentryDsn":"https://public:private-secret@sentry.example.com/42","maxEventsPerHour":30}`
	settings = GetObservability()
	assert.False(t, settings.Enabled)
	assert.Empty(t, settings.SentryDsn)
	assert.Equal(t, observabilityTunnelPath, settings.TunnelPath)

	clientSetting.Observability = `{"enabled":true,"sentryDsn":"https://public@sentry.example.com/42","crashSampleRate":1.1,"maxEventsPerHour":30}`
	settings = GetObservability()
	assert.False(t, settings.Enabled)
	assert.Empty(t, settings.SentryDsn)
}

func TestGetFeaturesFailsClosedForSemanticallyInvalidStoredConfig(t *testing.T) {
	original := clientSetting
	t.Cleanup(func() { clientSetting = original })

	clientSetting.Features = `{"artifacts":{"enabled":true,"rolloutPercentage":101,"modelAlias":"uclaw-artifact-v1","upstreamModel":"smart-latest","policyVersion":"v1"}}`
	settings := GetFeatures()
	assert.False(t, settings.Artifacts.Enabled)
	assert.Zero(t, settings.Artifacts.RolloutPercentage)
	assert.Empty(t, settings.Artifacts.ModelAlias)
	assert.Empty(t, settings.Artifacts.UpstreamModel)
	assert.Empty(t, settings.Artifacts.PolicyVersion)
	assert.False(t, settings.EcommerceMainImage.Enabled)
}

func TestGetFeaturesUsesDisabledDefaultForUnsetLegacyConfig(t *testing.T) {
	original := clientSetting
	t.Cleanup(func() { clientSetting = original })

	clientSetting.Features = ""
	settings := GetFeatures()
	assert.False(t, settings.Artifacts.Enabled)
	assert.Equal(t, managedArtifactV1Alias, settings.Artifacts.ModelAlias)
	assert.Equal(t, managedArtifactV1UpstreamModel, settings.Artifacts.UpstreamModel)
	assert.Equal(t, "v1", settings.Artifacts.PolicyVersion)
}

func TestFeatureRemoteStopOverridesRollout(t *testing.T) {
	original := clientSetting
	t.Cleanup(func() { clientSetting = original })

	clientSetting.Features = `{"artifacts":{"enabled":false,"rolloutPercentage":100,"modelAlias":"uclaw-artifact-v1","upstreamModel":"smart-latest","policyVersion":"v1"},"ecommerceMainImage":{"enabled":false,"rolloutPercentage":100,"skillVersion":"v1"},"htmlPreview":{"enabled":false,"rolloutPercentage":100},"longTermRules":{"enabled":false,"rolloutPercentage":100}}`
	public := GetPublicFeatures()
	assert.False(t, public.Artifacts.Enabled)
	assert.Equal(t, 100.0, public.Artifacts.RolloutPercentage)
	assert.False(t, public.EcommerceMainImage.Enabled)
	assert.Equal(t, 100.0, public.EcommerceMainImage.RolloutPercentage)
	assert.False(t, public.HtmlPreview.Enabled)
	assert.Equal(t, 100.0, public.HtmlPreview.RolloutPercentage)
	assert.False(t, public.LongTermRules.Enabled)
	assert.Equal(t, 100.0, public.LongTermRules.RolloutPercentage)
}

func TestRuntimeFeatureGatesValidateAndProjectIndependently(t *testing.T) {
	valid := `{"artifacts":{"enabled":true,"rolloutPercentage":10,"modelAlias":"uclaw-artifact-v1","policyVersion":"v1"},"ecommerceMainImage":{"enabled":true,"rolloutPercentage":20,"skillVersion":"v1"},"htmlPreview":{"enabled":true,"rolloutPercentage":30},"longTermRules":{"enabled":true,"rolloutPercentage":40}}`
	require.NoError(t, ValidateClientSettings(valid, "Features"))
	require.ErrorContains(t, ValidateClientSettings(`{"htmlPreview":{"enabled":true,"rolloutPercentage":101}}`, "Features"), "htmlPreview rolloutPercentage")
	require.ErrorContains(t, ValidateClientSettings(`{"longTermRules":{"enabled":true,"rolloutPercentage":-1}}`, "Features"), "longTermRules rolloutPercentage")

	original := clientSetting
	t.Cleanup(func() { clientSetting = original })
	clientSetting.Features = valid
	public := GetPublicFeatures()
	assert.Equal(t, PublicFeatureGate{Enabled: true, RolloutPercentage: 30}, public.HtmlPreview)
	assert.Equal(t, PublicFeatureGate{Enabled: true, RolloutPercentage: 40}, public.LongTermRules)
}

func TestObservabilityHasAnIndependentKillSwitchAndRollout(t *testing.T) {
	original := clientSetting
	t.Cleanup(func() { clientSetting = original })
	clientSetting.Observability = `{"enabled":false,"rolloutPercentage":100,"tunnelPath":"/api/clawx/observability/envelope","crashSampleRate":1,"handledErrorSampleRate":0.2,"tracesSampleRate":0.05,"artifactSampleRate":0.2,"maxEventsPerHour":30}`
	settings := GetObservability()
	assert.False(t, settings.Enabled)
	assert.Equal(t, 100.0, settings.RolloutPercentage)
	require.ErrorContains(t, ValidateClientSettings(`{"enabled":false,"rolloutPercentage":101}`, "Observability"), "observability rolloutPercentage")
}

func TestGetPublicFeaturesDoesNotExposeUpstreamModel(t *testing.T) {
	original := clientSetting
	t.Cleanup(func() { clientSetting = original })
	clientSetting.Features = `{"artifacts":{"enabled":true,"rolloutPercentage":10,"modelAlias":"uclaw-artifact-v1","upstreamModel":"smart-latest","policyVersion":"v1"}}`

	public := GetPublicFeatures()
	assert.True(t, public.Artifacts.Enabled)
	assert.Equal(t, "uclaw-artifact-v1", public.Artifacts.ModelAlias)
	assert.Equal(t, "v1", public.Artifacts.PolicyVersion)
	encoded, err := common.Marshal(public)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "smart-latest")
	assert.NotContains(t, string(encoded), "upstreamModel")
}

func TestEnabledArtifactAliasIsInjectedAsHiddenEnabledTextModel(t *testing.T) {
	original := clientSetting
	t.Cleanup(func() { clientSetting = original })
	clientSetting.Features = `{"artifacts":{"enabled":true,"rolloutPercentage":100,"modelAlias":"uclaw-artifact-v1","policyVersion":"v1"}}`
	clientSetting.ModelOptions = `{"text":{"defaultModel":"smart-latest","models":[{"id":"smart-latest","enabled":true}]}}`

	options := GetModelOptions()
	var artifact *ClientModelItem
	for index := range options.Text.Models {
		if options.Text.Models[index].Id == managedArtifactV1Alias {
			artifact = &options.Text.Models[index]
			break
		}
	}
	require.NotNil(t, artifact)
	require.NotNil(t, artifact.Enabled)
	assert.True(t, *artifact.Enabled)
	require.NotNil(t, artifact.Visible)
	assert.False(t, *artifact.Visible)
}
