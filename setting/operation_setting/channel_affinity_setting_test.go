package operation_setting

import "testing"

func hasString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func hasPromptCacheKeySource(sources []ChannelAffinityKeySource) bool {
	for _, source := range sources {
		if source.Type == "gjson" && source.Path == "prompt_cache_key" {
			return true
		}
	}
	return false
}

func TestDefaultChannelAffinityKeepsResponsesPromptCacheRule(t *testing.T) {
	setting := GetChannelAffinitySetting()
	for _, rule := range setting.Rules {
		if rule.Name != "codex cli trace" {
			continue
		}
		if !hasString(rule.PathRegex, "/v1/responses") {
			t.Fatalf("codex cli trace rule must keep /v1/responses path")
		}
		if !hasString(rule.ModelRegex, "^gpt-.*$") {
			t.Fatalf("codex cli trace rule must keep gpt model regex")
		}
		if !hasPromptCacheKeySource(rule.KeySources) {
			t.Fatalf("codex cli trace rule must keep prompt_cache_key source")
		}
		return
	}
	t.Fatalf("default channel affinity setting must include codex cli trace rule")
}

func TestDefaultChannelAffinityIncludesChatPromptCacheKeyRule(t *testing.T) {
	setting := GetChannelAffinitySetting()
	for _, rule := range setting.Rules {
		if rule.Name != "openai chat prompt cache" {
			continue
		}
		if !hasString(rule.PathRegex, "/v1/chat/completions") {
			t.Fatalf("chat prompt cache rule must match /v1/chat/completions")
		}
		if !hasString(rule.ModelRegex, "^gpt-.*$") || !hasString(rule.ModelRegex, "^smart-.*$") {
			t.Fatalf("chat prompt cache rule must cover gpt-* and smart-* models")
		}
		if !hasPromptCacheKeySource(rule.KeySources) {
			t.Fatalf("chat prompt cache rule must read prompt_cache_key from JSON")
		}
		if !rule.SkipRetryOnFailure || !rule.IncludeUsingGroup || !rule.IncludeRuleName {
			t.Fatalf("chat prompt cache rule must preserve affinity failure and cache key flags")
		}
		if rule.ParamOverrideTemplate == nil {
			t.Fatalf("chat prompt cache rule must pass through Codex headers")
		}
		return
	}
	t.Fatalf("default channel affinity setting must include openai chat prompt cache rule")
}

func TestGetChannelAffinitySettingRestoresMissingBuiltInRules(t *testing.T) {
	setting := GetChannelAffinitySetting()
	originalRules := setting.Rules
	defer func() {
		setting.Rules = originalRules
	}()

	setting.Rules = []ChannelAffinityRule{
		{
			Name:       "custom prompt cache",
			ModelRegex: []string{"^custom-.*$"},
			PathRegex:  []string{"/v1/chat/completions"},
			KeySources: []ChannelAffinityKeySource{
				{Type: "gjson", Path: "prompt_cache_key"},
			},
		},
	}

	setting = GetChannelAffinitySetting()

	if !hasRuleNamed(setting.Rules, "custom prompt cache") {
		t.Fatalf("custom channel affinity rule must be preserved")
	}
	if !hasRuleNamed(setting.Rules, "openai chat prompt cache") {
		t.Fatalf("missing built-in chat prompt cache rule must be restored")
	}
	if !hasRuleNamed(setting.Rules, "codex cli trace") || !hasRuleNamed(setting.Rules, "claude cli trace") {
		t.Fatalf("missing built-in cli trace rules must be restored")
	}
}

func hasRuleNamed(rules []ChannelAffinityRule, name string) bool {
	for _, rule := range rules {
		if rule.Name == name {
			return true
		}
	}
	return false
}
