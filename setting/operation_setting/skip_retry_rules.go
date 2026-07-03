package operation_setting

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"
)

const defaultAutomaticSkipRetryRulesJSON = `[
{"name":"client_gone_after_stream_started","enabled":true,"after_stream_started":true,"stream_end_reasons":["client_gone"],"message_contains":["request context done","context canceled","client_gone","client disconnected","broken pipe","connection reset by peer"],"skip_channel_error_log":true}
]`

type RetrySkipRule struct {
	Name                string   `json:"name,omitempty"`
	Enabled             *bool    `json:"enabled,omitempty"`
	AfterStreamStarted  bool     `json:"after_stream_started,omitempty"`
	StatusCodes         string   `json:"status_codes,omitempty"`
	ErrorCodes          []string `json:"error_codes,omitempty"`
	ErrorTypes          []string `json:"error_types,omitempty"`
	MessageContains     []string `json:"message_contains,omitempty"`
	MessageRegex        []string `json:"message_regex,omitempty"`
	StreamEndReasons    []string `json:"stream_end_reasons,omitempty"`
	SkipChannelErrorLog bool     `json:"skip_channel_error_log,omitempty"`
}

type RetrySkipRuleContext struct {
	StreamStarted   bool
	StreamEndReason string
}

var AutomaticSkipRetryRules = defaultAutomaticSkipRetryRules()

func defaultAutomaticSkipRetryRules() []RetrySkipRule {
	var rules []RetrySkipRule
	if err := common.UnmarshalJsonStr(defaultAutomaticSkipRetryRulesJSON, &rules); err != nil {
		common.SysError("failed to parse default automatic skip retry rules: " + err.Error())
		return nil
	}
	return rules
}

func AutomaticSkipRetryRulesToString() string {
	if len(AutomaticSkipRetryRules) == 0 {
		return ""
	}
	bytes, err := common.Marshal(AutomaticSkipRetryRules)
	if err != nil {
		common.SysError("failed to marshal automatic skip retry rules: " + err.Error())
		return defaultAutomaticSkipRetryRulesJSON
	}
	return string(bytes)
}

func AutomaticSkipRetryRulesFromString(s string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		AutomaticSkipRetryRules = nil
		return nil
	}

	var rules []RetrySkipRule
	if err := common.UnmarshalJsonStr(s, &rules); err != nil {
		return fmt.Errorf("invalid automatic skip retry rules JSON: %w", err)
	}
	if err := ValidateRetrySkipRules(rules); err != nil {
		return err
	}
	AutomaticSkipRetryRules = normalizeRetrySkipRules(rules)
	return nil
}

func ValidateRetrySkipRules(rules []RetrySkipRule) error {
	for i, rule := range rules {
		if strings.TrimSpace(rule.StatusCodes) != "" {
			if _, err := ParseHTTPStatusCodeRanges(rule.StatusCodes); err != nil {
				return fmt.Errorf("invalid status_codes in skip retry rule #%d: %w", i+1, err)
			}
		}
		for _, pattern := range rule.MessageRegex {
			pattern = strings.TrimSpace(pattern)
			if pattern == "" {
				continue
			}
			if _, err := regexp.Compile(pattern); err != nil {
				return fmt.Errorf("invalid message_regex in skip retry rule #%d: %w", i+1, err)
			}
		}
	}
	return nil
}

func MatchSkipRetryRule(err *types.NewAPIError, ctx RetrySkipRuleContext) (RetrySkipRule, bool) {
	if err == nil {
		return RetrySkipRule{}, false
	}

	for _, rule := range AutomaticSkipRetryRules {
		rule = normalizeRetrySkipRule(rule)
		if !retrySkipRuleEnabled(rule) {
			continue
		}
		if rule.AfterStreamStarted && !ctx.StreamStarted {
			continue
		}
		if retrySkipRuleMatches(rule, err, ctx) {
			return rule, true
		}
	}
	return RetrySkipRule{}, false
}

func ShouldSkipRetryByRules(err *types.NewAPIError, ctx RetrySkipRuleContext) bool {
	_, ok := MatchSkipRetryRule(err, ctx)
	return ok
}

func retrySkipRuleEnabled(rule RetrySkipRule) bool {
	return rule.Enabled == nil || *rule.Enabled
}

func retrySkipRuleMatches(rule RetrySkipRule, err *types.NewAPIError, ctx RetrySkipRuleContext) bool {
	matcherCount := 0
	if strings.TrimSpace(rule.StatusCodes) != "" {
		matcherCount++
		if retrySkipStatusCodeMatches(rule.StatusCodes, err.StatusCode) {
			return true
		}
	}
	if len(rule.ErrorCodes) > 0 {
		matcherCount++
		if retrySkipStringListMatches(rule.ErrorCodes, string(err.GetErrorCode())) {
			return true
		}
	}
	if len(rule.ErrorTypes) > 0 {
		matcherCount++
		if retrySkipStringListMatches(rule.ErrorTypes, string(err.GetErrorType())) {
			return true
		}
	}
	if len(rule.StreamEndReasons) > 0 {
		matcherCount++
		if retrySkipStringListMatches(rule.StreamEndReasons, ctx.StreamEndReason) {
			return true
		}
	}

	errMsg := strings.ToLower(err.ErrorWithStatusCode())
	if len(rule.MessageContains) > 0 {
		matcherCount++
		for _, keyword := range rule.MessageContains {
			keyword = strings.ToLower(strings.TrimSpace(keyword))
			if keyword != "" && strings.Contains(errMsg, keyword) {
				return true
			}
		}
	}
	if len(rule.MessageRegex) > 0 {
		matcherCount++
		for _, pattern := range rule.MessageRegex {
			pattern = strings.TrimSpace(pattern)
			if pattern == "" {
				continue
			}
			matched, compileErr := regexp.MatchString(pattern, err.ErrorWithStatusCode())
			if compileErr == nil && matched {
				return true
			}
		}
	}

	return matcherCount == 0
}

func retrySkipStatusCodeMatches(rangesText string, statusCode int) bool {
	ranges, err := ParseHTTPStatusCodeRanges(rangesText)
	if err != nil {
		return false
	}
	return shouldMatchStatusCodeRanges(ranges, statusCode)
}

func retrySkipStringListMatches(values []string, target string) bool {
	target = strings.ToLower(strings.TrimSpace(target))
	if target == "" {
		return false
	}
	for _, value := range values {
		if strings.ToLower(strings.TrimSpace(value)) == target {
			return true
		}
	}
	return false
}

func normalizeRetrySkipRules(rules []RetrySkipRule) []RetrySkipRule {
	normalized := make([]RetrySkipRule, 0, len(rules))
	for _, rule := range rules {
		normalized = append(normalized, normalizeRetrySkipRule(rule))
	}
	return normalized
}

func normalizeRetrySkipRule(rule RetrySkipRule) RetrySkipRule {
	rule.Name = strings.TrimSpace(rule.Name)
	rule.StatusCodes = strings.TrimSpace(rule.StatusCodes)
	rule.ErrorCodes = normalizeRetrySkipStrings(rule.ErrorCodes)
	rule.ErrorTypes = normalizeRetrySkipStrings(rule.ErrorTypes)
	rule.MessageContains = normalizeRetrySkipStrings(rule.MessageContains)
	rule.MessageRegex = normalizeRetrySkipStrings(rule.MessageRegex)
	rule.StreamEndReasons = normalizeRetrySkipStrings(rule.StreamEndReasons)
	return rule
}

func normalizeRetrySkipStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			normalized = append(normalized, value)
		}
	}
	return normalized
}
