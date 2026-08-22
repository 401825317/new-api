package clawx_client_setting

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
)

var validAnnouncementLevels = map[string]bool{
	"normal":    true,
	"important": true,
	"urgent":    true,
}

const defaultGrokVideoDurationSeconds = 6

var versionedArtifactAliasPattern = regexp.MustCompile(`^uclaw-artifact-v[1-9][0-9]*$`)
var versionIdentifierPattern = regexp.MustCompile(`^v[1-9][0-9]*$`)
var uclawInstallationIDPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

var supportedGrokVideoDurations = []int{6, 10, 15}

var supportedGrokImageVideoSizes = []string{"854x480", "1280x720", "720x1280", "1920x1080"}

var supportedGrokVideo15Sizes = []string{"854x480", "1280x720", "720x1280", "1920x1080"}

func ValidateClientSettings(settingsStr string, settingType string) error {
	if strings.TrimSpace(settingsStr) == "" {
		return nil
	}

	switch settingType {
	case "Announcements":
		return validateAnnouncements(settingsStr)
	case "Support":
		return validateSupport(settingsStr)
	case "ModelOptions":
		return validateModelOptions(settingsStr)
	case "Observability":
		return validateObservability(settingsStr)
	case "Features":
		return validateFeatures(settingsStr)
	default:
		return fmt.Errorf("未知的 ClawX 客户端设置类型：%s", settingType)
	}
}

func validateSampleRate(value float64, field string) error {
	if value < 0 || value > 1 {
		return fmt.Errorf("%s must be between 0 and 1", field)
	}
	return nil
}

func validateRolloutPercentage(value float64, field string) error {
	if value < 0 || value > 100 {
		return fmt.Errorf("%s rolloutPercentage must be between 0 and 100", field)
	}
	return nil
}

func defaultObservability() Observability {
	return Observability{
		Enabled:                false,
		RolloutPercentage:      0,
		TunnelPath:             observabilityTunnelPath,
		CrashSampleRate:        1,
		HandledErrorSampleRate: 0.2,
		TracesSampleRate:       0.05,
		ArtifactSampleRate:     0.2,
		MaxEventsPerHour:       observabilityMaxEventsPerHour,
	}
}

func parseObservability(settingsStr string) (Observability, error) {
	settings := defaultObservability()
	if err := common.UnmarshalJsonStr(settingsStr, &settings); err != nil {
		return Observability{}, fmt.Errorf("ClawX observability config format error: %s", err.Error())
	}
	settings.SentryDsn = strings.TrimSpace(settings.SentryDsn)
	settings.TunnelPath = fallbackString(settings.TunnelPath, observabilityTunnelPath)
	return settings, nil
}

func validateSentryDSN(rawDSN string) error {
	if strings.TrimSpace(rawDSN) == "" {
		return nil
	}
	if err := validateHTTPURL(rawDSN, "Sentry DSN"); err != nil {
		return err
	}
	parsed, err := url.Parse(strings.TrimSpace(rawDSN))
	if err != nil {
		return fmt.Errorf("Sentry DSN URL format is invalid")
	}
	if parsed.User == nil {
		return fmt.Errorf("Sentry DSN must include a public key")
	}
	if _, hasPassword := parsed.User.Password(); hasPassword {
		return fmt.Errorf("Sentry DSN must not include a password or secret")
	}
	if strings.TrimSpace(parsed.User.Username()) == "" {
		return fmt.Errorf("Sentry DSN must include a public key")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("Sentry DSN must not include a query or fragment")
	}
	pathSegments := strings.Split(strings.Trim(parsed.EscapedPath(), "/"), "/")
	projectID := pathSegments[len(pathSegments)-1]
	decodedProjectID, err := url.PathUnescape(projectID)
	if err != nil || strings.TrimSpace(decodedProjectID) == "" || strings.Contains(decodedProjectID, "/") || decodedProjectID == "." || decodedProjectID == ".." {
		return fmt.Errorf("Sentry DSN must include a project ID")
	}
	return nil
}

func validateObservabilityValue(settings Observability) error {
	if settings.Enabled && strings.TrimSpace(settings.SentryDsn) == "" {
		return fmt.Errorf("Sentry DSN is required when observability is enabled")
	}
	if err := validateSentryDSN(settings.SentryDsn); err != nil {
		return err
	}
	if err := validateRolloutPercentage(settings.RolloutPercentage, "observability"); err != nil {
		return err
	}
	if strings.TrimSpace(settings.TunnelPath) != observabilityTunnelPath {
		return fmt.Errorf("observability tunnelPath must be %s", observabilityTunnelPath)
	}
	for _, sample := range []struct {
		value float64
		field string
	}{
		{settings.CrashSampleRate, "crashSampleRate"},
		{settings.HandledErrorSampleRate, "handledErrorSampleRate"},
		{settings.TracesSampleRate, "tracesSampleRate"},
		{settings.ArtifactSampleRate, "artifactSampleRate"},
	} {
		if err := validateSampleRate(sample.value, sample.field); err != nil {
			return err
		}
	}
	if settings.MaxEventsPerHour < 1 || settings.MaxEventsPerHour > observabilityMaxEventsPerHour {
		return fmt.Errorf("maxEventsPerHour must be between 1 and %d", observabilityMaxEventsPerHour)
	}
	return nil
}

func validateObservability(settingsStr string) error {
	settings, err := parseObservability(settingsStr)
	if err != nil {
		return err
	}
	return validateObservabilityValue(settings)
}

func validateFeatureGate(gate FeatureGate, field string) error {
	if err := validateRolloutPercentage(gate.RolloutPercentage, field); err != nil {
		return err
	}
	seenUserIds := make(map[int]struct{}, len(gate.InternalUserIds))
	for _, userId := range gate.InternalUserIds {
		if userId <= 0 {
			return fmt.Errorf("%s internalUserIds must contain positive user IDs", field)
		}
		if _, exists := seenUserIds[userId]; exists {
			return fmt.Errorf("%s internalUserIds must not contain duplicates", field)
		}
		seenUserIds[userId] = struct{}{}
	}
	return nil
}

func validateFeatures(settingsStr string) error {
	settings := defaultFeatures()
	if err := common.UnmarshalJsonStr(settingsStr, &settings); err != nil {
		return fmt.Errorf("ClawX feature config format error: %s", err.Error())
	}
	return validateFeaturesValue(settings)
}

func validateFeaturesValue(settings Features) error {
	if err := validateFeatureGate(settings.Artifacts.FeatureGate, "artifacts"); err != nil {
		return err
	}
	if err := validateFeatureGate(settings.EcommerceMainImage.FeatureGate, "ecommerceMainImage"); err != nil {
		return err
	}
	if err := validateFeatureGate(settings.HtmlPreview, "htmlPreview"); err != nil {
		return err
	}
	if err := validateFeatureGate(settings.LongTermRules, "longTermRules"); err != nil {
		return err
	}
	alias := strings.TrimSpace(settings.Artifacts.ModelAlias)
	if !versionedArtifactAliasPattern.MatchString(alias) {
		return fmt.Errorf("artifacts modelAlias must be a versioned uclaw-artifact-vN alias")
	}
	lockedUpstreamModel, locked := managedArtifactUpstreamModel(alias)
	if !locked {
		return fmt.Errorf("artifacts modelAlias must reference a supported locked artifact route")
	}
	upstreamModel := strings.TrimSpace(settings.Artifacts.UpstreamModel)
	if upstreamModel != "" {
		upstreamName := upstreamModel
		if separator := strings.LastIndex(upstreamName, "/"); separator >= 0 {
			upstreamName = upstreamName[separator+1:]
		}
		if strings.HasPrefix(upstreamName, "uclaw-artifact-v") {
			return fmt.Errorf("artifacts upstreamModel cannot reference a private artifact alias")
		}
		if upstreamModel != lockedUpstreamModel {
			return fmt.Errorf("artifacts upstreamModel cannot override the locked %s route", alias)
		}
	}
	if !versionIdentifierPattern.MatchString(strings.TrimSpace(settings.Artifacts.PolicyVersion)) {
		return fmt.Errorf("artifacts policyVersion must use vN format")
	}
	if !versionIdentifierPattern.MatchString(strings.TrimSpace(settings.EcommerceMainImage.SkillVersion)) {
		return fmt.Errorf("ecommerceMainImage skillVersion must use vN format")
	}
	if settings.EcommerceMainImage.Enabled {
		if !settings.Artifacts.Enabled {
			return fmt.Errorf("ecommerceMainImage requires artifacts to be enabled")
		}
	}
	return nil
}

func validateHTTPURL(raw string, field string) error {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("%s URL 格式不正确", field)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("%s URL 只支持 http 或 https", field)
	}
	return nil
}

func validateTimestamp(value string, field string, required bool) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		if required {
			return fmt.Errorf("%s不能为空", field)
		}
		return nil
	}
	if _, err := time.Parse(time.RFC3339, trimmed); err != nil {
		return fmt.Errorf("%s格式错误，应使用 RFC3339 时间", field)
	}
	return nil
}

func validateAnnouncements(settingsStr string) error {
	var list []Announcement
	if err := common.UnmarshalJsonStr(settingsStr, &list); err != nil {
		return fmt.Errorf("ClawX 客户端公告格式错误：%s", err.Error())
	}
	if len(list) > 30 {
		return fmt.Errorf("ClawX 客户端公告数量不能超过30个")
	}

	seen := make(map[string]bool, len(list))
	for i, item := range list {
		index := i + 1
		if strings.TrimSpace(item.Id) == "" {
			return fmt.Errorf("第%d个客户端公告缺少ID", index)
		}
		if seen[item.Id] {
			return fmt.Errorf("第%d个客户端公告ID重复", index)
		}
		seen[item.Id] = true
		if strings.TrimSpace(item.Title) == "" {
			return fmt.Errorf("第%d个客户端公告缺少标题", index)
		}
		if len(item.Title) > 80 {
			return fmt.Errorf("第%d个客户端公告标题不能超过80字符", index)
		}
		if strings.TrimSpace(item.Content) == "" {
			return fmt.Errorf("第%d个客户端公告缺少内容", index)
		}
		if len(item.Content) > 1000 {
			return fmt.Errorf("第%d个客户端公告内容不能超过1000字符", index)
		}
		level := strings.TrimSpace(item.Level)
		if level == "" {
			level = "normal"
		}
		if !validAnnouncementLevels[level] {
			return fmt.Errorf("第%d个客户端公告级别不合法", index)
		}
		if err := validateTimestamp(item.PublishedAt, fmt.Sprintf("第%d个客户端公告发布时间", index), true); err != nil {
			return err
		}
		if err := validateTimestamp(item.ExpiresAt, fmt.Sprintf("第%d个客户端公告过期时间", index), false); err != nil {
			return err
		}
		if strings.TrimSpace(item.ExpiresAt) != "" {
			publishedAt, _ := time.Parse(time.RFC3339, item.PublishedAt)
			expiresAt, _ := time.Parse(time.RFC3339, item.ExpiresAt)
			if !expiresAt.After(publishedAt) {
				return fmt.Errorf("第%d个客户端公告过期时间必须晚于发布时间", index)
			}
		}
		if err := validateHTTPURL(item.Link, fmt.Sprintf("第%d个客户端公告链接", index)); err != nil {
			return err
		}
	}
	return nil
}

func validateSupport(settingsStr string) error {
	var support Support
	if err := common.UnmarshalJsonStr(settingsStr, &support); err != nil {
		return fmt.Errorf("ClawX 客服配置格式错误：%s", err.Error())
	}
	if len(support.Title) > 60 {
		return fmt.Errorf("客服标题不能超过60字符")
	}
	if len(support.Description) > 300 {
		return fmt.Errorf("客服说明不能超过300字符")
	}
	if len(support.WorkHours) > 100 {
		return fmt.Errorf("客服服务时间不能超过100字符")
	}
	if len(support.WechatId) > 100 {
		return fmt.Errorf("客服微信号不能超过100字符")
	}
	if len(support.ExtraNote) > 200 {
		return fmt.Errorf("客服备注不能超过200字符")
	}
	if err := validateHTTPURL(support.QrCodeUrl, "客服二维码"); err != nil {
		return err
	}
	if len(support.Contacts) > 6 {
		return fmt.Errorf("客服联系人不能超过6个")
	}
	seen := make(map[string]bool, len(support.Contacts))
	for i, contact := range support.Contacts {
		index := i + 1
		id := strings.TrimSpace(contact.Id)
		if id == "" {
			return fmt.Errorf("第%d个客服联系人缺少ID", index)
		}
		if seen[id] {
			return fmt.Errorf("第%d个客服联系人ID重复", index)
		}
		seen[id] = true
		if len(contact.Label) > 60 {
			return fmt.Errorf("第%d个客服联系人名称不能超过60字符", index)
		}
		if len(contact.Description) > 200 {
			return fmt.Errorf("第%d个客服联系人说明不能超过200字符", index)
		}
		if len(contact.WorkHours) > 100 {
			return fmt.Errorf("第%d个客服联系人服务时间不能超过100字符", index)
		}
		if len(contact.WechatId) > 100 {
			return fmt.Errorf("第%d个客服联系人微信号不能超过100字符", index)
		}
		if len(contact.ExtraNote) > 200 {
			return fmt.Errorf("第%d个客服联系人备注不能超过200字符", index)
		}
		if err := validateHTTPURL(contact.QrCodeUrl, fmt.Sprintf("第%d个客服联系人二维码", index)); err != nil {
			return err
		}
	}
	return nil
}

func validateModelOptions(settingsStr string) error {
	var options ModelOptions
	if err := common.UnmarshalJsonStr(settingsStr, &options); err != nil {
		return fmt.Errorf("ClawX model options format error: %s", err.Error())
	}
	if len(options.Text.Models) > 100 {
		return fmt.Errorf("text model count cannot exceed 100")
	}
	if len(options.Image.Models) > 30 {
		return fmt.Errorf("image model count cannot exceed 30")
	}
	if len(options.Video.Models) > 30 {
		return fmt.Errorf("video model count cannot exceed 30")
	}
	if level := strings.ToLower(strings.TrimSpace(options.Text.DefaultThinkingLevel)); level != "" && !isSupportedThinkingLevel(level) {
		return fmt.Errorf("default thinking level must be one of off, minimal, low, medium, high, or xhigh")
	}
	for i, item := range options.Text.Models {
		index := i + 1
		if strings.TrimSpace(item.Id) == "" {
			return fmt.Errorf("text model %d is missing id", index)
		}
		if len(item.Label) > 80 {
			return fmt.Errorf("text model %d label cannot exceed 80 characters", index)
		}
	}
	if len(options.Text.FallbackModels) > 100 {
		return fmt.Errorf("text fallback model count cannot exceed 100")
	}
	defaults := ModelOptions{}
	_ = common.UnmarshalJsonStr(defaultModelOptionsJSON, &defaults)
	availableModels := normalizeTextModels(options.Text.Models)
	if len(availableModels) == 0 {
		availableModels = normalizeTextModels(defaults.Text.Models)
	}
	availableTextModels := make(map[string]struct{}, len(availableModels))
	for _, item := range availableModels {
		availableTextModels[item.Id] = struct{}{}
	}
	primaryModel := fallbackString(options.Text.DefaultModel, defaults.Text.DefaultModel)
	if _, exists := availableTextModels[primaryModel]; !exists && len(availableModels) > 0 {
		primaryModel = availableModels[0].Id
	}
	seenFallbacks := make(map[string]struct{}, len(options.Text.FallbackModels))
	for i, fallbackModel := range options.Text.FallbackModels {
		fallbackModel = strings.TrimSpace(fallbackModel)
		if fallbackModel == "" {
			return fmt.Errorf("text fallback model %d is empty", i+1)
		}
		if strings.HasPrefix(strings.ToLower(fallbackModel), "uclaw-artifact-v") {
			return fmt.Errorf("text fallback model %d cannot reference a private artifact alias", i+1)
		}
		if fallbackModel == primaryModel {
			return fmt.Errorf("text fallback model %d cannot equal the primary model", i+1)
		}
		if _, duplicate := seenFallbacks[fallbackModel]; duplicate {
			return fmt.Errorf("text fallback model %d is duplicated", i+1)
		}
		if _, available := availableTextModels[fallbackModel]; !available {
			return fmt.Errorf("text fallback model %d must reference an enabled text model", i+1)
		}
		seenFallbacks[fallbackModel] = struct{}{}
	}
	for i, item := range options.Image.Models {
		index := i + 1
		if strings.TrimSpace(item.Id) == "" {
			return fmt.Errorf("image model %d is missing id", index)
		}
		if len(item.Label) > 80 {
			return fmt.Errorf("image model %d label cannot exceed 80 characters", index)
		}
		if len(item.Sizes) > 20 {
			return fmt.Errorf("image model %d size count cannot exceed 20", index)
		}
		if len(item.Qualities) > 20 {
			return fmt.Errorf("image model %d quality count cannot exceed 20", index)
		}
	}
	for i, item := range options.Video.Models {
		index := i + 1
		if strings.TrimSpace(item.Id) == "" {
			return fmt.Errorf("video model %d is missing id", index)
		}
		if len(item.Label) > 80 {
			return fmt.Errorf("video model %d label cannot exceed 80 characters", index)
		}
		if len(item.Modes) > 10 {
			return fmt.Errorf("video model %d mode count cannot exceed 10", index)
		}
		if len(item.Sizes) > 20 {
			return fmt.Errorf("video model %d size count cannot exceed 20", index)
		}
		if len(item.Durations) > 20 {
			return fmt.Errorf("video model %d duration count cannot exceed 20", index)
		}
		for _, duration := range item.Durations {
			if duration <= 0 || duration > 600 {
				return fmt.Errorf("video model %d has invalid duration: %d", index, duration)
			}
			if isGrokVideoModel(item.Id) && !isSupportedGrokVideoDuration(duration) {
				return fmt.Errorf("Grok video model %d only supports 6, 10, or 15 second durations", index)
			}
		}
		if isGrokVideoModel(item.Id) && item.DefaultDurationSeconds != 0 && !isSupportedGrokVideoDuration(item.DefaultDurationSeconds) {
			return fmt.Errorf("Grok video model %d default duration must be 6, 10, or 15 seconds", index)
		}
	}
	defaultVideoModel := fallbackString(options.Video.DefaultModel, "grok-image-video")
	if isGrokVideoModel(defaultVideoModel) && options.Video.DefaultDurationSeconds != 0 && !isSupportedGrokVideoDuration(options.Video.DefaultDurationSeconds) {
		return fmt.Errorf("default Grok video duration must be 6, 10, or 15 seconds")
	}
	return nil
}

func GetAnnouncements() []Announcement {
	if !GetClientSetting().AnnouncementsEnabled {
		return []Announcement{}
	}
	var list []Announcement
	if err := common.UnmarshalJsonStr(GetClientSetting().Announcements, &list); err != nil {
		return []Announcement{}
	}
	now := time.Now()
	active := make([]Announcement, 0, len(list))
	for _, item := range list {
		if !item.Enabled {
			continue
		}
		if strings.TrimSpace(item.Level) == "" {
			item.Level = "normal"
		}
		publishedAt, err := time.Parse(time.RFC3339, item.PublishedAt)
		if err != nil || publishedAt.After(now) {
			continue
		}
		if strings.TrimSpace(item.ExpiresAt) != "" {
			expiresAt, err := time.Parse(time.RFC3339, item.ExpiresAt)
			if err != nil || expiresAt.Before(now) {
				continue
			}
		}
		active = append(active, item)
	}
	sort.SliceStable(active, func(i, j int) bool {
		left, _ := time.Parse(time.RFC3339, active[i].PublishedAt)
		right, _ := time.Parse(time.RFC3339, active[j].PublishedAt)
		return left.After(right)
	})
	return active
}

func GetSupport() Support {
	if !GetClientSetting().SupportEnabled {
		return Support{}
	}
	var support Support
	if err := common.UnmarshalJsonStr(GetClientSetting().Support, &support); err != nil {
		return Support{}
	}
	support.Contacts = normalizeSupportContacts(support)
	return support
}

func GetModelOptions() ModelOptions {
	var options ModelOptions
	if err := common.UnmarshalJsonStr(GetClientSetting().ModelOptions, &options); err != nil {
		_ = common.UnmarshalJsonStr(defaultModelOptionsJSON, &options)
	}
	options = normalizeModelOptions(options)
	features := GetFeatures()
	if features.Artifacts.Enabled && features.Artifacts.ModelAlias == managedArtifactV1Alias {
		found := false
		for index := range options.Text.Models {
			if strings.TrimSpace(options.Text.Models[index].Id) != managedArtifactV1Alias {
				continue
			}
			enabled := true
			visible := false
			options.Text.Models[index].Enabled = &enabled
			options.Text.Models[index].Visible = &visible
			found = true
			break
		}
		if !found {
			enabled := true
			visible := false
			options.Text.Models = append(options.Text.Models, ClientModelItem{
				Id:      managedArtifactV1Alias,
				Label:   "UClaw Artifact v1",
				Enabled: &enabled,
				Visible: &visible,
			})
		}
	}
	return options
}

func GetObservability() Observability {
	settings, err := parseObservability(GetClientSetting().Observability)
	if err != nil || validateObservabilityValue(settings) != nil {
		return defaultObservability()
	}
	return settings
}

func defaultFeatures() Features {
	return Features{
		Artifacts: ArtifactFeature{
			FeatureGate: FeatureGate{
				Enabled:           false,
				RolloutPercentage: 0,
			},
			ModelAlias:    managedArtifactV1Alias,
			UpstreamModel: managedArtifactV1UpstreamModel,
			PolicyVersion: "v1",
		},
		EcommerceMainImage: EcommerceMainImageFeature{
			FeatureGate: FeatureGate{
				Enabled:           false,
				RolloutPercentage: 0,
			},
			SkillVersion: "v1",
		},
		HtmlPreview:   FeatureGate{Enabled: false, RolloutPercentage: 0},
		LongTermRules: FeatureGate{Enabled: false, RolloutPercentage: 0},
	}
}

// unavailableFeatures is used only when persisted feature settings cannot be
// trusted. Unlike a valid disabled default, it removes the private alias and
// route entirely so a malformed setting cannot leave an artifact runtime
// accidentally addressable.
func unavailableFeatures() Features {
	features := defaultFeatures()
	features.Artifacts.Enabled = false
	features.Artifacts.RolloutPercentage = 0
	features.Artifacts.InternalUserIds = nil
	features.Artifacts.ModelAlias = ""
	features.Artifacts.UpstreamModel = ""
	features.Artifacts.PolicyVersion = ""
	features.EcommerceMainImage.Enabled = false
	features.EcommerceMainImage.RolloutPercentage = 0
	features.EcommerceMainImage.InternalUserIds = nil
	features.EcommerceMainImage.SkillVersion = ""
	features.HtmlPreview.Enabled = false
	features.HtmlPreview.RolloutPercentage = 0
	features.HtmlPreview.InternalUserIds = nil
	features.LongTermRules.Enabled = false
	features.LongTermRules.RolloutPercentage = 0
	features.LongTermRules.InternalUserIds = nil
	return features
}

func GetFeatures() Features {
	if strings.TrimSpace(GetClientSetting().Features) == "" {
		return defaultFeatures()
	}
	settings := defaultFeatures()
	if err := common.UnmarshalJsonStr(GetClientSetting().Features, &settings); err != nil {
		return unavailableFeatures()
	}
	settings.Artifacts.ModelAlias = fallbackString(settings.Artifacts.ModelAlias, managedArtifactV1Alias)
	settings.Artifacts.UpstreamModel = strings.TrimSpace(settings.Artifacts.UpstreamModel)
	settings.Artifacts.PolicyVersion = fallbackString(settings.Artifacts.PolicyVersion, "v1")
	settings.EcommerceMainImage.SkillVersion = fallbackString(settings.EcommerceMainImage.SkillVersion, "v1")
	if validateFeaturesValue(settings) != nil {
		return unavailableFeatures()
	}
	// The public model catalog is intentionally not the source of truth for
	// artifact routing. A versioned alias always resolves through this registry.
	settings.Artifacts.UpstreamModel, _ = managedArtifactUpstreamModel(settings.Artifacts.ModelAlias)
	return settings
}

func GetPublicFeatures() PublicFeatures {
	return GetPublicFeaturesForClient(0, "")
}

func GetPublicFeaturesForClient(userId int, installationId string) PublicFeatures {
	settings := GetFeatures()
	return PublicFeatures{
		Artifacts: PublicArtifactFeature{
			PublicFeatureGate: publicFeatureGate(settings.Artifacts.FeatureGate, userId, installationId, "artifacts"),
			ModelAlias:        settings.Artifacts.ModelAlias,
			PolicyVersion:     settings.Artifacts.PolicyVersion,
		},
		EcommerceMainImage: PublicEcommerceMainImageFeature{
			PublicFeatureGate: publicFeatureGate(settings.EcommerceMainImage.FeatureGate, userId, installationId, "ecommerce-main-image"),
			SkillVersion:      settings.EcommerceMainImage.SkillVersion,
		},
		HtmlPreview:   publicFeatureGate(settings.HtmlPreview, userId, installationId, "html-preview"),
		LongTermRules: publicFeatureGate(settings.LongTermRules, userId, installationId, "long-term-rules"),
	}
}

func publicFeatureGate(gate FeatureGate, userId int, installationId string, salt string) PublicFeatureGate {
	return PublicFeatureGate{
		Enabled:           gate.Enabled,
		RolloutPercentage: gate.RolloutPercentage,
		Eligible:          managedFeatureEligible(gate, userId, installationId, salt),
	}
}

func managedFeatureEligible(gate FeatureGate, userId int, installationId string, salt string) bool {
	if !gate.Enabled {
		return false
	}
	for _, internalUserId := range gate.InternalUserIds {
		if internalUserId == userId {
			return true
		}
	}
	installationId = strings.ToLower(strings.TrimSpace(installationId))
	if !uclawInstallationIDPattern.MatchString(installationId) || gate.RolloutPercentage <= 0 {
		return false
	}
	sum := sha256.Sum256([]byte(installationId + ":" + salt))
	bucket := binary.BigEndian.Uint32(sum[:4]) % 10_000
	return float64(bucket) < gate.RolloutPercentage*100
}

// ManagedArtifactFeatureEligible is the relay-side enforcement point. The
// client may hide an out-of-bucket feature early, but the server makes the
// authoritative decision for the active device token's request.
func ManagedArtifactFeatureEligible(features Features, userId int, installationId string) bool {
	return managedFeatureEligible(features.Artifacts.FeatureGate, userId, installationId, "artifacts")
}

// ManagedArtifactUpstreamModel is deliberately a code-owned registry rather
// than a normal model-options mapping. Adding v2 requires an explicit server
// release, so an administrator cannot silently retarget v1 by editing a model.
func ManagedArtifactUpstreamModel(alias string) (string, bool) {
	return managedArtifactUpstreamModel(alias)
}

func managedArtifactUpstreamModel(alias string) (string, bool) {
	switch strings.TrimSpace(alias) {
	case managedArtifactV1Alias:
		return managedArtifactV1UpstreamModel, true
	default:
		return "", false
	}
}

func normalizeSupportContacts(support Support) []SupportContact {
	contacts := make([]SupportContact, 0, len(support.Contacts)+1)
	for _, contact := range support.Contacts {
		if !contact.Enabled {
			continue
		}
		if strings.TrimSpace(contact.QrCodeUrl) == "" {
			continue
		}
		contact.Id = strings.TrimSpace(contact.Id)
		contact.Label = strings.TrimSpace(contact.Label)
		contact.Description = strings.TrimSpace(contact.Description)
		contact.QrCodeUrl = strings.TrimSpace(contact.QrCodeUrl)
		contact.WorkHours = strings.TrimSpace(contact.WorkHours)
		contact.WechatId = strings.TrimSpace(contact.WechatId)
		contact.ExtraNote = strings.TrimSpace(contact.ExtraNote)
		if contact.Label == "" {
			contact.Label = "官方客服"
		}
		contacts = append(contacts, contact)
	}
	if len(contacts) > 0 {
		return contacts
	}
	if strings.TrimSpace(support.QrCodeUrl) == "" {
		return []SupportContact{}
	}
	return []SupportContact{
		{
			Id:          "default",
			Label:       fallbackString(support.Title, "官方客服"),
			Description: strings.TrimSpace(support.Description),
			QrCodeUrl:   strings.TrimSpace(support.QrCodeUrl),
			WorkHours:   strings.TrimSpace(support.WorkHours),
			WechatId:    strings.TrimSpace(support.WechatId),
			ExtraNote:   strings.TrimSpace(support.ExtraNote),
			Enabled:     true,
		},
	}
}

func normalizeModelOptions(options ModelOptions) ModelOptions {
	defaults := ModelOptions{}
	_ = common.UnmarshalJsonStr(defaultModelOptionsJSON, &defaults)

	options.Text.Models = normalizeTextModels(options.Text.Models)
	if len(options.Text.Models) == 0 {
		options.Text.Models = normalizeTextModels(defaults.Text.Models)
	}
	options.Text.DefaultModel = fallbackString(options.Text.DefaultModel, defaults.Text.DefaultModel)
	options.Text.DefaultThinkingLevel = normalizeThinkingLevel(
		options.Text.DefaultThinkingLevel,
		defaults.Text.DefaultThinkingLevel,
	)
	if !textModelExists(options.Text.Models, options.Text.DefaultModel) && len(options.Text.Models) > 0 {
		options.Text.DefaultModel = options.Text.Models[0].Id
	}
	options.Text.FallbackModels = normalizeTextFallbackModels(
		options.Text.FallbackModels,
		options.Text.Models,
		options.Text.DefaultModel,
	)

	options.Image.Models = normalizeImageModels(options.Image.Models)
	if len(options.Image.Models) == 0 {
		options.Image.Models = normalizeImageModels(defaults.Image.Models)
	}
	options.Image.DefaultModel = fallbackString(options.Image.DefaultModel, defaults.Image.DefaultModel)
	options.Image.DefaultSize = fallbackString(options.Image.DefaultSize, defaults.Image.DefaultSize)
	options.Image.DefaultQuality = fallbackString(options.Image.DefaultQuality, defaults.Image.DefaultQuality)
	if !imageModelExists(options.Image.Models, options.Image.DefaultModel) && len(options.Image.Models) > 0 {
		options.Image.DefaultModel = options.Image.Models[0].Id
	}

	options.Video.Models = normalizeVideoModels(options.Video.Models)
	if len(options.Video.Models) == 0 {
		options.Video.Models = normalizeVideoModels(defaults.Video.Models)
	}
	options.Video.DefaultModel = fallbackString(options.Video.DefaultModel, defaults.Video.DefaultModel)
	options.Video.DefaultSize = fallbackString(options.Video.DefaultSize, defaults.Video.DefaultSize)
	if !videoModelExists(options.Video.Models, options.Video.DefaultModel) && len(options.Video.Models) > 0 {
		options.Video.DefaultModel = options.Video.Models[0].Id
	}
	selectedVideoModel := findVideoModel(options.Video.Models, options.Video.DefaultModel)
	if selectedVideoModel != nil {
		if options.Video.DefaultDurationSeconds <= 0 || !containsInt(selectedVideoModel.Durations, options.Video.DefaultDurationSeconds) {
			options.Video.DefaultDurationSeconds = firstInt(selectedVideoModel.Durations, defaults.Video.DefaultDurationSeconds)
		}
	} else if options.Video.DefaultDurationSeconds <= 0 {
		options.Video.DefaultDurationSeconds = defaults.Video.DefaultDurationSeconds
	}

	return options
}

func isSupportedThinkingLevel(value string) bool {
	switch value {
	case "off", "minimal", "low", "medium", "high", "xhigh":
		return true
	default:
		return false
	}
}

func normalizeThinkingLevel(value string, fallback string) string {
	level := strings.ToLower(strings.TrimSpace(value))
	if isSupportedThinkingLevel(level) {
		return level
	}
	return strings.ToLower(strings.TrimSpace(fallback))
}

func normalizeTextModels(models []ClientModelItem) []ClientModelItem {
	seen := make(map[string]bool, len(models))
	result := make([]ClientModelItem, 0, len(models))
	for _, item := range models {
		if item.Enabled != nil && !*item.Enabled {
			continue
		}
		item.Id = strings.TrimSpace(item.Id)
		if item.Id == "" || seen[item.Id] {
			continue
		}
		item.Label = strings.TrimSpace(item.Label)
		item.Description = strings.TrimSpace(item.Description)
		if item.Label == "" {
			item.Label = item.Id
		}
		seen[item.Id] = true
		result = append(result, item)
	}
	return result
}

func normalizeTextFallbackModels(
	values []string,
	models []ClientModelItem,
	primaryModel string,
) []string {
	available := make(map[string]struct{}, len(models))
	for _, model := range models {
		available[model.Id] = struct{}{}
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || value == primaryModel {
			continue
		}
		if strings.HasPrefix(strings.ToLower(value), "uclaw-artifact-v") {
			continue
		}
		if _, exists := available[value]; !exists {
			continue
		}
		if _, duplicate := seen[value]; duplicate {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func normalizeImageModels(models []ClientImageModelItem) []ClientImageModelItem {
	seen := make(map[string]bool, len(models))
	result := make([]ClientImageModelItem, 0, len(models))
	for _, item := range models {
		if item.Enabled != nil && !*item.Enabled {
			continue
		}
		item.Id = strings.TrimSpace(item.Id)
		if item.Id == "" || seen[item.Id] {
			continue
		}
		item.Label = strings.TrimSpace(item.Label)
		item.Description = strings.TrimSpace(item.Description)
		item.Sizes = normalizeStringList(item.Sizes)
		item.Qualities = normalizeStringList(item.Qualities)
		if len(item.Sizes) == 0 {
			item.Sizes = []string{"1024x1024"}
		}
		if len(item.Qualities) == 0 {
			item.Qualities = []string{"medium"}
		}
		item.DefaultSize = fallbackString(item.DefaultSize, firstString(item.Sizes, "1024x1024"))
		item.DefaultQuality = fallbackString(item.DefaultQuality, firstString(item.Qualities, "medium"))
		if item.Label == "" {
			item.Label = item.Id
		}
		seen[item.Id] = true
		result = append(result, item)
	}
	return result
}

func normalizeVideoModels(models []ClientVideoModelItem) []ClientVideoModelItem {
	seen := make(map[string]bool, len(models))
	result := make([]ClientVideoModelItem, 0, len(models))
	for _, item := range models {
		if item.Enabled != nil && !*item.Enabled {
			continue
		}
		item.Id = strings.TrimSpace(item.Id)
		if item.Id == "" || seen[item.Id] {
			continue
		}
		item.Label = strings.TrimSpace(item.Label)
		item.Description = strings.TrimSpace(item.Description)
		item.Modes = normalizeStringList(item.Modes)
		item.Sizes = normalizeStringList(item.Sizes)
		if isGrokVideoModel(item.Id) {
			item.Sizes = supportedGrokVideoSizes(item.Id)
			item.Durations = normalizeGrokVideoDurations(item.Durations)
		} else {
			item.Durations = normalizeDurationList(item.Durations)
		}
		if len(item.Modes) == 0 {
			item.Modes = []string{"text-to-video"}
		}
		if len(item.Sizes) == 0 {
			item.Sizes = []string{"1280x720"}
		}
		if len(item.Durations) == 0 {
			item.Durations = []int{4}
		}
		item.DefaultSize = fallbackString(item.DefaultSize, firstString(item.Sizes, "1280x720"))
		if item.DefaultDurationSeconds <= 0 {
			item.DefaultDurationSeconds = firstInt(item.Durations, 4)
		}
		if item.Label == "" {
			item.Label = item.Id
		}
		seen[item.Id] = true
		result = append(result, item)
	}
	return result
}

// normalizeGrokVideoDurations keeps the administrator-selected catalog while
// filtering values that the upstream provider cannot accept. An empty list
// falls back to the complete provider capability list for legacy records.
func normalizeGrokVideoDurations(values []int) []int {
	values = normalizeDurationList(values)
	result := make([]int, 0, len(values))
	for _, value := range values {
		if isSupportedGrokVideoDuration(value) {
			result = append(result, value)
		}
	}
	if len(result) == 0 {
		return cloneSupportedGrokVideoDurations()
	}
	return result
}

func normalizeStringList(values []string) []string {
	seen := make(map[string]bool, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func normalizeDurationList(values []int) []int {
	seen := make(map[int]bool, len(values))
	result := make([]int, 0, len(values))
	for _, value := range values {
		if value <= 0 || value > 600 || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func isGrokVideoModel(modelID string) bool {
	switch strings.ToLower(strings.TrimSpace(modelID)) {
	case "grok-image-video", "grok-video-1.5", "grok-imagine-video", "grok-imagine-video-1.5", "grok-imagine-1.5-video-apimart", "grok-imagine-1.5-video-ext":
		return true
	default:
		return false
	}
}

func isSupportedGrokVideoDuration(duration int) bool {
	for _, allowed := range supportedGrokVideoDurations {
		if duration == allowed {
			return true
		}
	}
	return false
}

func cloneSupportedGrokVideoDurations() []int {
	return append([]int(nil), supportedGrokVideoDurations...)
}

func supportedGrokVideoSizes(modelID string) []string {
	var sizes []string
	switch strings.ToLower(strings.TrimSpace(modelID)) {
	case "grok-image-video", "grok-imagine-video":
		sizes = supportedGrokImageVideoSizes
	case "grok-video-1.5", "grok-imagine-video-1.5", "grok-imagine-1.5-video-apimart", "grok-imagine-1.5-video-ext":
		sizes = supportedGrokVideo15Sizes
	}
	return append([]string(nil), sizes...)
}

func textModelExists(models []ClientModelItem, id string) bool {
	for _, item := range models {
		if item.Id == id {
			return true
		}
	}
	return false
}

func imageModelExists(models []ClientImageModelItem, id string) bool {
	for _, item := range models {
		if item.Id == id {
			return true
		}
	}
	return false
}

func videoModelExists(models []ClientVideoModelItem, id string) bool {
	for _, item := range models {
		if item.Id == id {
			return true
		}
	}
	return false
}

func findVideoModel(models []ClientVideoModelItem, id string) *ClientVideoModelItem {
	for i := range models {
		if models[i].Id == id {
			return &models[i]
		}
	}
	return nil
}

func containsInt(values []int, target int) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func firstString(values []string, fallback string) string {
	if len(values) == 0 {
		return fallback
	}
	return values[0]
}

func firstInt(values []int, fallback int) int {
	if len(values) == 0 {
		return fallback
	}
	return values[0]
}

func fallbackString(value string, fallback string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fallback
	}
	return trimmed
}
