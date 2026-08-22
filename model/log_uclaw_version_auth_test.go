package model

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func uclawDiagnosticTestContext() *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "https://zz-cn.lingzhiwuxian.com/v1/responses", nil)
	c.Request.Header.Set("X-UClaw-Client", "desktop")
	c.Request.Header.Set("X-UClaw-Version", "2.0.3")
	return c
}

func TestBuildClientDiagnosticsRejectsForgedHeadersFromOrdinaryToken(t *testing.T) {
	previousDB := DB
	dsn := fmt.Sprintf("file:uclaw-log-auth-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&ClawXDevice{}))
	DB = db
	t.Cleanup(func() { DB = previousDB })

	c := uclawDiagnosticTestContext()
	c.Set("id", 11)
	c.Set("token_id", 101)
	require.Nil(t, buildClientDiagnostics(c))

	require.NoError(t, DB.Create(&ClawXDevice{
		UserId:   11,
		DeviceId: "managed-device",
		TokenId:  101,
		Status:   ClawXDeviceStatusActive,
	}).Error)
	require.Equal(t, "2.0.3", buildClientDiagnostics(c)["uclaw_version"])

	require.NoError(t, DB.Model(&ClawXDevice{}).Where("user_id = ?", 11).Update("status", ClawXDeviceStatusRevoked).Error)
	require.Nil(t, buildClientDiagnostics(c))
}
