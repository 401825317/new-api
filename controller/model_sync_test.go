package controller

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeLocale(t *testing.T) {
	locale, ok := normalizeLocale("zh-CN")
	require.True(t, ok)
	require.Equal(t, "zh", locale)

	locale, ok = normalizeLocale("zh-cn")
	require.True(t, ok)
	require.Equal(t, "zh", locale)

	locale, ok = normalizeLocale("ja")
	require.True(t, ok)
	require.Equal(t, "ja", locale)
}
