package middleware

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/clawx_client_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func enabledArtifactFeatures() clawx_client_setting.Features {
	return clawx_client_setting.Features{
		Artifacts: clawx_client_setting.ArtifactFeature{
			FeatureGate: clawx_client_setting.FeatureGate{
				Enabled:           true,
				RolloutPercentage: 100,
			},
			ModelAlias:    "uclaw-artifact-v1",
			UpstreamModel: "smart-latest",
			PolicyVersion: "v1",
		},
	}
}

func TestResolveManagedArtifactModelEnforcesPrivateAliasContract(t *testing.T) {
	features := enabledArtifactFeatures()

	modelName, err := resolveManagedArtifactModel("uclaw-artifact-v1", features, true, true)
	require.Nil(t, err)
	assert.Equal(t, "smart-latest", modelName)

	_, err = resolveManagedArtifactModel("uclaw-artifact-v1", features, false, true)
	require.NotNil(t, err)
	assert.Equal(t, http.StatusForbidden, err.status)

	_, err = resolveManagedArtifactModel("uclaw-artifact-v2", features, true, true)
	require.NotNil(t, err)
	assert.Equal(t, http.StatusForbidden, err.status)

	features.Artifacts.Enabled = false
	_, err = resolveManagedArtifactModel("uclaw-artifact-v1", features, true, true)
	require.NotNil(t, err)
	assert.Equal(t, http.StatusForbidden, err.status)

	features = enabledArtifactFeatures()
	features.Artifacts.RolloutPercentage = 0
	_, err = resolveManagedArtifactModel("uclaw-artifact-v1", features, true, false)
	require.NotNil(t, err)
	assert.Equal(t, http.StatusForbidden, err.status)

	modelName, err = resolveManagedArtifactModel("personal-model", features, false, false)
	require.Nil(t, err)
	assert.Equal(t, "personal-model", modelName)
}

func TestResolveManagedArtifactModelIgnoresLegacyUpstreamOverride(t *testing.T) {
	features := enabledArtifactFeatures()
	features.Artifacts.UpstreamModel = "uclaw-artifact-v2"

	modelName, err := resolveManagedArtifactModel("uclaw-artifact-v1", features, true, true)
	require.Nil(t, err)
	assert.Equal(t, "smart-latest", modelName)
}

func TestResolveManagedArtifactModelUsesLockedV1Route(t *testing.T) {
	features := enabledArtifactFeatures()
	features.Artifacts.UpstreamModel = "another-model"

	modelName, err := resolveManagedArtifactModel("uclaw-artifact-v1", features, true, true)
	require.Nil(t, err)
	assert.Equal(t, "smart-latest", modelName)
}

func TestReplaceJSONRequestModelPreservesRequestPayload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(
		http.MethodPost,
		"/v1/responses",
		strings.NewReader(`{"model":"uclaw-artifact-v1","input":"hello","stream":false,"seed":9007199254740993}`),
	)
	ctx.Request.Header.Set("Content-Type", "application/json; charset=utf-8")
	t.Cleanup(func() { common.CleanupBodyStorage(ctx) })

	require.NoError(t, replaceJSONRequestModel(ctx, "smart-latest"))
	body, err := io.ReadAll(ctx.Request.Body)
	require.NoError(t, err)
	var payload map[string]interface{}
	require.NoError(t, common.Unmarshal(body, &payload))
	assert.Equal(t, "smart-latest", payload["model"])
	assert.Equal(t, "hello", payload["input"])
	assert.Equal(t, false, payload["stream"])
	assert.Contains(t, string(body), `"seed":9007199254740993`)
	assert.Equal(t, int64(len(body)), ctx.Request.ContentLength)
}

func TestManagedArtifactAliasRequiresServerBoundInstallationAndKeepsAliasVisible(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousDB := model.DB
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:artifact-alias-%d?mode=memory&cache=shared", time.Now().UnixNano())), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.ClawXDevice{}))
	model.DB = db
	t.Cleanup(func() { model.DB = previousDB })

	previousFeatures := clawx_client_setting.GetClientSetting().Features
	encoded, err := common.Marshal(enabledArtifactFeatures())
	require.NoError(t, err)
	clawx_client_setting.GetClientSetting().Features = string(encoded)
	t.Cleanup(func() { clawx_client_setting.GetClientSetting().Features = previousFeatures })

	installationID := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	require.NoError(t, db.Create(&model.ClawXDevice{UserId: 7, DeviceId: "device", TokenId: 11, InstallationId: installationID, Status: model.ClawXDeviceStatusActive}).Error)

	apply := func(header string) (*gin.Context, *managedArtifactModelError) {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"uclaw-artifact-v1","input":"hello"}`))
		ctx.Request.Header.Set("Content-Type", "application/json")
		ctx.Request.Header.Set("X-UClaw-Install-Id", header)
		ctx.Set("id", 7)
		ctx.Set("token_id", 11)
		return ctx, applyManagedArtifactModelAlias(ctx, &ModelRequest{Model: "uclaw-artifact-v1"})
	}

	forgedContext, forgedErr := apply("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	require.NotNil(t, forgedErr)
	assert.Equal(t, http.StatusForbidden, forgedErr.status)
	assert.Empty(t, common.GetContextKeyString(forgedContext, constant.ContextKeyUClawArtifactAlias))

	context, applyErr := apply(installationID)
	require.Nil(t, applyErr)
	assert.Equal(t, "uclaw-artifact-v1", common.GetContextKeyString(context, constant.ContextKeyUClawArtifactAlias))
	assert.Equal(t, "smart-latest", common.GetContextKeyString(context, constant.ContextKeyUClawArtifactUpstreamModel))
	body, readErr := io.ReadAll(context.Request.Body)
	require.NoError(t, readErr)
	assert.Contains(t, string(body), `"model":"smart-latest"`)

	SetupContextForSelectedChannel(context, &model.Channel{}, "smart-latest")
	assert.Equal(t, "smart-latest", context.GetString("original_model"))
}
