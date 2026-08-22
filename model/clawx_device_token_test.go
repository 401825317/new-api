package model

import (
	"fmt"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestIsActiveClawXDeviceTokenRequiresMatchingActiveBinding(t *testing.T) {
	previousDB := DB
	dsn := fmt.Sprintf("file:clawx-device-token-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&ClawXDevice{}))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	DB = db
	t.Cleanup(func() {
		DB = previousDB
		require.NoError(t, sqlDB.Close())
	})

	require.NoError(t, db.Create(&[]ClawXDevice{
		{UserId: 11, DeviceId: "active", TokenId: 101, InstallationId: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Status: ClawXDeviceStatusActive},
		{UserId: 11, DeviceId: "revoked", TokenId: 102, Status: ClawXDeviceStatusRevoked},
		{UserId: 12, DeviceId: "other-user", TokenId: 103, Status: ClawXDeviceStatusActive},
	}).Error)

	active, err := IsActiveClawXDeviceToken(11, 101)
	require.NoError(t, err)
	assert.True(t, active)

	bound, err := IsActiveClawXDeviceTokenBoundToInstallation(11, 101, "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
	require.NoError(t, err)
	assert.True(t, bound)
	bound, err = IsActiveClawXDeviceTokenBoundToInstallation(11, 101, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	require.NoError(t, err)
	assert.False(t, bound)

	for _, input := range []struct {
		name    string
		userID  int
		tokenID int
	}{
		{name: "revoked device", userID: 11, tokenID: 102},
		{name: "token belongs to another user", userID: 11, tokenID: 103},
		{name: "unknown token", userID: 11, tokenID: 999},
		{name: "invalid user", userID: 0, tokenID: 101},
		{name: "invalid token", userID: 11, tokenID: 0},
	} {
		t.Run(input.name, func(t *testing.T) {
			matched, lookupErr := IsActiveClawXDeviceToken(input.userID, input.tokenID)
			require.NoError(t, lookupErr)
			assert.False(t, matched)
		})
	}
}
