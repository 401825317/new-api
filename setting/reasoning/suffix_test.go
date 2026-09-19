package reasoning

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDeepSeekV4ModelBoundaries(t *testing.T) {
	for _, modelName := range []string{"deepseek-v4-pro", "deepseek-v4-flash", "deepseek-v4.1-flash"} {
		require.True(t, IsDeepSeekV4Model(modelName), modelName)
		for _, suffix := range []string{"none", "max"} {
			base, thinking, effort, ok := ParseDeepSeekV4ThinkingSuffix(modelName + "-" + suffix)
			require.True(t, ok)
			require.Equal(t, modelName, base)
			if suffix == "none" {
				require.Equal(t, "disabled", thinking)
				require.Empty(t, effort)
			} else {
				require.Equal(t, "enabled", thinking)
				require.Equal(t, "max", effort)
			}
		}
	}
	for _, modelName := range []string{"deepseek-v40-flash", "deepseek-v4.10-flash", "deepseek-v4.2-flash", "deepseek-v3-flash", "alias-deepseek-v4.1-flash", "deepseek-v4-", "deepseek-v4.1-", "glm-5"} {
		require.False(t, IsDeepSeekV4Model(modelName), modelName)
		base, _, _, ok := ParseDeepSeekV4ThinkingSuffix(modelName + "-none")
		require.False(t, ok, modelName)
		require.Equal(t, modelName+"-none", base)
	}
}
