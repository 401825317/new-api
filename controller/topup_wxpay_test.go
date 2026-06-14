package controller

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewWxPayTradeNoFitsWeChatOutTradeNoLimit(t *testing.T) {
	allowed := regexp.MustCompile(`^[A-Z0-9]+$`)

	for _, prefix := range []string{"WXUSR", "WXCUSR", "WXSUBUSR", "WXCSUBUSR"} {
		tradeNo := newWxPayTradeNo(prefix, 123456789)

		require.LessOrEqual(t, len(tradeNo), wxPayOutTradeNoMaxLength)
		require.Regexp(t, allowed, tradeNo)
	}
}

func TestNewWxPayTradeNoUsesExistingNoStyle(t *testing.T) {
	tradeNo := newWxPayTradeNo("WXUSR", 42)

	require.Regexp(t, regexp.MustCompile(`^WXUSR42NO[A-Z0-9]{6}\d{10}$`), tradeNo)
	require.LessOrEqual(t, len(tradeNo), wxPayOutTradeNoMaxLength)
}
