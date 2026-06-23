package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupClawXControllerTest(t *testing.T) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.User{},
		&model.ClawXDevice{},
		&model.ClawXSession{},
		&model.ClawXActivationTicket{},
		&model.ClawXRelease{},
		&model.Redemption{},
		&model.Token{},
		&model.Log{},
	))
	model.DB = db
	model.LOG_DB = db
	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	common.PasswordLoginEnabled = true
	common.PasswordRegisterEnabled = true
	common.RegisterEnabled = true
	common.EmailVerificationEnabled = false
	common.QuotaForNewUser = 0
	common.QuotaPerUnit = 500000
	t.Setenv("CLAWX_ACTIVATION_REQUIRED", "true")
}

func createClawXReleaseForTest(t *testing.T, release model.ClawXRelease) {
	t.Helper()
	require.NoError(t, model.CreateClawXRelease(&release))
}

func performClawXRequest(handler gin.HandlerFunc, body string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/clawx/test", bytes.NewBufferString(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	handler(ctx)
	return recorder
}

func createClawXTestUser(t *testing.T, username string) model.User {
	t.Helper()
	user := model.User{
		Username:    username,
		Email:       username + "@example.com",
		Password:    "password123",
		DisplayName: username,
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
	}
	require.NoError(t, user.InsertWithTx(model.DB, 0))
	return user
}

func createClawXActivationCode(t *testing.T, key string) {
	t.Helper()
	require.NoError(t, model.DB.Create(&model.Redemption{
		Key:         key,
		Status:      common.RedemptionCodeStatusEnabled,
		Name:        "test activation",
		Quota:       100,
		CreatedTime: common.GetTimestamp(),
		ExpiredTime: time.Now().Add(time.Hour).Unix(),
	}).Error)
}

func TestClawXLoginRequiresActivationForNewDevice(t *testing.T) {
	setupClawXControllerTest(t)
	createClawXTestUser(t, "alice")

	recorder := performClawXRequest(ClawXLogin, `{
		"account":"alice@example.com",
		"password":"password123",
		"device":{"id":"device-new","name":"Mac"}
	}`)

	require.Equal(t, http.StatusForbidden, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"code":"device_authorization_required"`)
	require.Contains(t, recorder.Body.String(), "当前设备需要激活码授权")
}

func TestClawXLoginAuthorizesNewDeviceWithActivationCode(t *testing.T) {
	setupClawXControllerTest(t)
	user := createClawXTestUser(t, "alice")
	createClawXActivationCode(t, "ACTIVATE-1")

	recorder := performClawXRequest(ClawXLogin, `{
		"account":"alice@example.com",
		"password":"password123",
		"activationCode":"ACTIVATE-1",
		"device":{"id":"device-new","name":"Mac"}
	}`)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"success":true`)
	device, err := model.GetClawXDevice(user.Id, "device-new")
	require.NoError(t, err)
	require.Equal(t, model.ClawXDeviceStatusActive, device.Status)
	var redemption model.Redemption
	require.NoError(t, model.DB.Where("`key` = ?", "ACTIVATE-1").First(&redemption).Error)
	require.Equal(t, common.RedemptionCodeStatusUsed, redemption.Status)
	require.Equal(t, user.Id, redemption.UsedUserId)
}

func TestClawXLoginExistingActiveDeviceDoesNotNeedActivationCode(t *testing.T) {
	setupClawXControllerTest(t)
	user := createClawXTestUser(t, "alice")
	_, err := model.UpsertClawXDevice(user.Id, model.ClawXDevice{DeviceId: "device-known", Name: "Mac"})
	require.NoError(t, err)

	recorder := performClawXRequest(ClawXLogin, `{
		"account":"alice@example.com",
		"password":"password123",
		"device":{"id":"device-known","name":"Mac"}
	}`)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"success":true`)
}

func TestClawXRegisterReturnsActivationErrorCode(t *testing.T) {
	setupClawXControllerTest(t)

	recorder := performClawXRequest(ClawXRegister, `{
		"account":"new@example.com",
		"password":"password123",
		"activationCode":"BAD-CODE",
		"device":{"id":"device-new","name":"Mac"}
	}`)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"code":"activation_invalid"`)
	require.Contains(t, recorder.Body.String(), "激活码无效")
}

func TestClawXRegisterRejectsTakenUsernameWithoutConsumingActivationCode(t *testing.T) {
	setupClawXControllerTest(t)
	createClawXTestUser(t, "test2")
	createClawXActivationCode(t, "ACTIVATE-TAKEN")

	recorder := performClawXRequest(ClawXRegister, `{
		"account":"test2",
		"password":"password123",
		"activationCode":"ACTIVATE-TAKEN",
		"device":{"id":"device-new","name":"Mac"}
	}`)

	require.Equal(t, http.StatusConflict, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"code":"user_exists"`)

	var count int64
	require.NoError(t, model.DB.Model(&model.User{}).Where("username IN ?", []string{"test21", "test22"}).Count(&count).Error)
	require.Equal(t, int64(0), count)

	var redemption model.Redemption
	require.NoError(t, model.DB.Where("`key` = ?", "ACTIVATE-TAKEN").First(&redemption).Error)
	require.Equal(t, common.RedemptionCodeStatusEnabled, redemption.Status)

	var ticketCount int64
	require.NoError(t, model.DB.Model(&model.ClawXActivationTicket{}).Count(&ticketCount).Error)
	require.Equal(t, int64(0), ticketCount)
}

func TestClawXLatestReleasePayloadSeparatesInstallerAndPortableZip(t *testing.T) {
	setupClawXControllerTest(t)
	now := common.GetTimestamp()
	createClawXReleaseForTest(t, model.ClawXRelease{
		Channel:     "latest",
		Platform:    "win",
		Arch:        "x64",
		PackageType: model.ClawXReleasePackageTypeInstaller,
		Version:     "0.4.9",
		FileName:    "UClaw-0.4.9-win-x64.exe",
		FileURL:     "https://example.com/UClaw-0.4.9-win-x64.exe",
		Sha512:      "installer-sha",
		Size:        101,
		Enabled:     true,
		CreatedAt:   now,
	})
	createClawXReleaseForTest(t, model.ClawXRelease{
		Channel:     "latest",
		Platform:    "win",
		Arch:        "x64",
		PackageType: model.ClawXReleasePackageTypePortableZip,
		Version:     "0.5.0",
		FileName:    "UClaw-0.5.0-win-x64-usb.zip",
		FileURL:     "https://example.com/UClaw-0.5.0-win-x64-usb.zip",
		Sha512:      "portable-sha",
		Size:        202,
		Enabled:     true,
		CreatedAt:   now + 1,
	})

	installerPayload, ok, err := clawXLatestReleasePayload("latest", "win", model.ClawXReleasePackageTypeInstaller, "x64")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "0.4.9", installerPayload["version"])
	require.Equal(t, model.ClawXReleasePackageTypeInstaller, installerPayload["package_type"])
	require.Equal(t, "installer-sha", installerPayload["sha512"])

	portablePayload, ok, err := clawXLatestReleasePayload("latest", "win", model.ClawXReleasePackageTypePortableZip, "x64")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "0.5.0", portablePayload["version"])
	require.Equal(t, model.ClawXReleasePackageTypePortableZip, portablePayload["package_type"])
	require.Equal(t, "UClaw-0.5.0-win-x64-usb.zip", portablePayload["fileName"])
	require.Equal(t, int64(202), portablePayload["size"])
	require.Equal(t, "portable-sha", portablePayload["sha512"])
}

func TestClawXInstallerFeedIgnoresPortableZipReleases(t *testing.T) {
	setupClawXControllerTest(t)
	now := common.GetTimestamp()
	createClawXReleaseForTest(t, model.ClawXRelease{
		Channel:     "latest",
		Platform:    "win",
		Arch:        "x64",
		PackageType: model.ClawXReleasePackageTypeInstaller,
		Version:     "0.4.9",
		FileName:    "UClaw-0.4.9-win-x64.exe",
		FileURL:     "https://example.com/UClaw-0.4.9-win-x64.exe",
		Sha512:      "installer-sha",
		Size:        101,
		Enabled:     true,
		CreatedAt:   now,
	})
	createClawXReleaseForTest(t, model.ClawXRelease{
		Channel:     "latest",
		Platform:    "win",
		Arch:        "x64",
		PackageType: model.ClawXReleasePackageTypePortableZip,
		Version:     "0.5.0",
		FileName:    "UClaw-0.5.0-win-x64-usb.zip",
		FileURL:     "https://example.com/UClaw-0.5.0-win-x64-usb.zip",
		Sha512:      "portable-sha",
		Size:        202,
		Enabled:     true,
		CreatedAt:   now + 1,
	})

	feed, ok, err := clawXBuildUpdateFeedYAML("latest", "win")
	require.NoError(t, err)
	require.True(t, ok)
	require.Contains(t, feed, "0.4.9")
	require.Contains(t, feed, "installer-sha")
	require.NotContains(t, feed, "0.5.0")
	require.NotContains(t, feed, "portable-sha")
}

func TestClawXPortableLatestPrefersRequestedArchOverUniversal(t *testing.T) {
	setupClawXControllerTest(t)
	now := common.GetTimestamp()
	createClawXReleaseForTest(t, model.ClawXRelease{
		Channel:     "latest",
		Platform:    "win",
		Arch:        "universal",
		PackageType: model.ClawXReleasePackageTypePortableZip,
		Version:     "0.5.0",
		FileName:    "UClaw-0.5.0-win-universal-usb.zip",
		FileURL:     "https://example.com/UClaw-0.5.0-win-universal-usb.zip",
		Sha512:      "universal-sha",
		Size:        201,
		Enabled:     true,
		CreatedAt:   now + 1,
	})
	createClawXReleaseForTest(t, model.ClawXRelease{
		Channel:     "latest",
		Platform:    "win",
		Arch:        "x64",
		PackageType: model.ClawXReleasePackageTypePortableZip,
		Version:     "0.5.0",
		FileName:    "UClaw-0.5.0-win-x64-usb.zip",
		FileURL:     "https://example.com/UClaw-0.5.0-win-x64-usb.zip",
		Sha512:      "x64-sha",
		Size:        202,
		Enabled:     true,
		CreatedAt:   now,
	})

	payload, ok, err := clawXLatestReleasePayload("latest", "win", model.ClawXReleasePackageTypePortableZip, "x64")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "x64", payload["arch"])
	require.Equal(t, "x64-sha", payload["sha512"])
}
