package operation_setting

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestMatchSkipRetryRule_ClientGoneAfterStreamStarted(t *testing.T) {
	orig := AutomaticSkipRetryRules
	t.Cleanup(func() { AutomaticSkipRetryRules = orig })

	require.NoError(t, AutomaticSkipRetryRulesFromString(defaultAutomaticSkipRetryRulesJSON))

	err := types.NewOpenAIError(
		fmt.Errorf("request context done: %w", context.Canceled),
		types.ErrorCodeBadResponse,
		http.StatusInternalServerError,
	)

	rule, matched := MatchSkipRetryRule(err, RetrySkipRuleContext{
		StreamStarted:   true,
		StreamEndReason: "client_gone",
	})

	require.True(t, matched)
	require.Equal(t, "client_gone_after_stream_started", rule.Name)
	require.True(t, rule.SkipChannelErrorLog)
}

func TestMatchSkipRetryRule_DoesNotMatchBeforeStreamStarted(t *testing.T) {
	orig := AutomaticSkipRetryRules
	t.Cleanup(func() { AutomaticSkipRetryRules = orig })

	require.NoError(t, AutomaticSkipRetryRulesFromString(defaultAutomaticSkipRetryRulesJSON))

	err := types.NewOpenAIError(
		fmt.Errorf("request context done: %w", context.Canceled),
		types.ErrorCodeBadResponse,
		http.StatusInternalServerError,
	)

	_, matched := MatchSkipRetryRule(err, RetrySkipRuleContext{
		StreamStarted:   false,
		StreamEndReason: "client_gone",
	})

	require.False(t, matched)
}

func TestAutomaticSkipRetryRulesFromString_ValidatesRegex(t *testing.T) {
	orig := AutomaticSkipRetryRules
	t.Cleanup(func() { AutomaticSkipRetryRules = orig })

	err := AutomaticSkipRetryRulesFromString(`[{"name":"bad-regex","message_regex":["["]}]`)

	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid message_regex")
}
