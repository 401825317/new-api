package model

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

const (
	ClawXDeviceStatusActive  = "active"
	ClawXDeviceStatusRevoked = "revoked"

	ClawXSessionStatusActive  = "active"
	ClawXSessionStatusRevoked = "revoked"

	ClawXActivationTicketStatusActive = "active"
	ClawXActivationTicketStatusUsed   = "used"
)

type ClawXDevice struct {
	Id         int    `json:"id"`
	UserId     int    `json:"user_id" gorm:"index;uniqueIndex:idx_clawx_device_user_device,priority:1"`
	DeviceId   string `json:"device_id" gorm:"type:varchar(128);index;uniqueIndex:idx_clawx_device_user_device,priority:2"`
	Name       string `json:"name" gorm:"type:varchar(255)"`
	Platform   string `json:"platform" gorm:"type:varchar(64)"`
	Arch       string `json:"arch" gorm:"type:varchar(64)"`
	AppVersion string `json:"app_version" gorm:"type:varchar(64)"`
	Status     string `json:"status" gorm:"type:varchar(32);default:'active';index"`
	TokenId    int    `json:"token_id" gorm:"index"`
	CreatedAt  int64  `json:"created_at" gorm:"bigint"`
	UpdatedAt  int64  `json:"updated_at" gorm:"bigint"`
	LastSeenAt int64  `json:"last_seen_at" gorm:"bigint"`
}

type ClawXSession struct {
	Id               int    `json:"id"`
	UserId           int    `json:"user_id" gorm:"index"`
	DeviceId         string `json:"device_id" gorm:"type:varchar(128);index"`
	AccessTokenHash  string `json:"-" gorm:"type:char(64);uniqueIndex"`
	RefreshTokenHash string `json:"-" gorm:"type:char(64);uniqueIndex"`
	AccessExpiresAt  int64  `json:"access_expires_at" gorm:"bigint;index"`
	RefreshExpiresAt int64  `json:"refresh_expires_at" gorm:"bigint;index"`
	Status           string `json:"status" gorm:"type:varchar(32);default:'active';index"`
	CreatedAt        int64  `json:"created_at" gorm:"bigint"`
	UpdatedAt        int64  `json:"updated_at" gorm:"bigint"`
	LastSeenAt       int64  `json:"last_seen_at" gorm:"bigint"`
}

type ClawXActivationTicket struct {
	Id             int    `json:"id"`
	TicketHash     string `json:"-" gorm:"type:char(64);uniqueIndex"`
	RedemptionId   int    `json:"redemption_id" gorm:"index"`
	DeviceId       string `json:"device_id" gorm:"type:varchar(128);index"`
	ExpiresAt      int64  `json:"expires_at" gorm:"bigint;index"`
	Status         string `json:"status" gorm:"type:varchar(32);default:'active';index"`
	CreatedAt      int64  `json:"created_at" gorm:"bigint"`
	UsedAt         int64  `json:"used_at" gorm:"bigint"`
	UsedUserId     int    `json:"used_user_id" gorm:"index"`
	ActivationCode string `json:"-" gorm:"type:varchar(128)"`
}

func HashClawXSecret(secret string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(secret)))
	return hex.EncodeToString(sum[:])
}

func UpsertClawXDevice(userId int, device ClawXDevice) (*ClawXDevice, error) {
	if userId <= 0 {
		return nil, errors.New("invalid user id")
	}
	device.DeviceId = strings.TrimSpace(device.DeviceId)
	if device.DeviceId == "" {
		return nil, errors.New("device id is required")
	}
	now := common.GetTimestamp()
	var stored ClawXDevice
	err := DB.Where("user_id = ? AND device_id = ?", userId, device.DeviceId).First(&stored).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		device.UserId = userId
		device.Status = ClawXDeviceStatusActive
		device.CreatedAt = now
		device.UpdatedAt = now
		device.LastSeenAt = now
		if err := DB.Create(&device).Error; err != nil {
			return nil, err
		}
		return &device, nil
	}
	if err != nil {
		return nil, err
	}
	updates := map[string]interface{}{
		"name":         device.Name,
		"platform":     device.Platform,
		"arch":         device.Arch,
		"app_version":  device.AppVersion,
		"status":       ClawXDeviceStatusActive,
		"updated_at":   now,
		"last_seen_at": now,
	}
	if err := DB.Model(&stored).Updates(updates).Error; err != nil {
		return nil, err
	}
	return GetClawXDevice(userId, device.DeviceId)
}

func GetClawXDevice(userId int, deviceId string) (*ClawXDevice, error) {
	var device ClawXDevice
	if err := DB.Where("user_id = ? AND device_id = ?", userId, strings.TrimSpace(deviceId)).First(&device).Error; err != nil {
		return nil, err
	}
	return &device, nil
}

func SetClawXDeviceToken(userId int, deviceId string, tokenId int) error {
	if userId <= 0 || strings.TrimSpace(deviceId) == "" || tokenId <= 0 {
		return errors.New("invalid device token binding")
	}
	return DB.Model(&ClawXDevice{}).
		Where("user_id = ? AND device_id = ?", userId, strings.TrimSpace(deviceId)).
		Updates(map[string]interface{}{
			"token_id":   tokenId,
			"updated_at": common.GetTimestamp(),
		}).Error
}

func RevokeClawXDevice(userId int, deviceId string) error {
	deviceId = strings.TrimSpace(deviceId)
	if userId <= 0 || deviceId == "" {
		return errors.New("invalid device")
	}
	now := common.GetTimestamp()
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&ClawXDevice{}).
			Where("user_id = ? AND device_id = ?", userId, deviceId).
			Updates(map[string]interface{}{"status": ClawXDeviceStatusRevoked, "updated_at": now}).Error; err != nil {
			return err
		}
		return tx.Model(&ClawXSession{}).
			Where("user_id = ? AND device_id = ? AND status = ?", userId, deviceId, ClawXSessionStatusActive).
			Updates(map[string]interface{}{"status": ClawXSessionStatusRevoked, "updated_at": now}).Error
	})
}

func CreateClawXSession(userId int, deviceId string, accessToken string, refreshToken string, accessExpiresAt int64, refreshExpiresAt int64) (*ClawXSession, error) {
	if userId <= 0 || strings.TrimSpace(deviceId) == "" || strings.TrimSpace(accessToken) == "" || strings.TrimSpace(refreshToken) == "" {
		return nil, errors.New("invalid session")
	}
	now := common.GetTimestamp()
	session := &ClawXSession{
		UserId:           userId,
		DeviceId:         strings.TrimSpace(deviceId),
		AccessTokenHash:  HashClawXSecret(accessToken),
		RefreshTokenHash: HashClawXSecret(refreshToken),
		AccessExpiresAt:  accessExpiresAt,
		RefreshExpiresAt: refreshExpiresAt,
		Status:           ClawXSessionStatusActive,
		CreatedAt:        now,
		UpdatedAt:        now,
		LastSeenAt:       now,
	}
	if err := DB.Create(session).Error; err != nil {
		return nil, err
	}
	return session, nil
}

func ValidateClawXAccessToken(token string) (*User, *ClawXSession, error) {
	token = strings.TrimSpace(strings.TrimPrefix(token, "Bearer "))
	if token == "" {
		return nil, nil, ErrTokenNotProvided
	}
	now := common.GetTimestamp()
	var session ClawXSession
	if err := DB.Where("access_token_hash = ?", HashClawXSecret(token)).First(&session).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, ErrTokenInvalid
		}
		return nil, nil, err
	}
	if session.Status != ClawXSessionStatusActive || session.AccessExpiresAt <= now {
		return nil, &session, ErrTokenInvalid
	}
	user, err := GetUserById(session.UserId, true)
	if err != nil {
		return nil, &session, err
	}
	if user.Status != common.UserStatusEnabled {
		return nil, &session, ErrTokenInvalid
	}
	var device ClawXDevice
	if err := DB.Where("user_id = ? AND device_id = ?", session.UserId, session.DeviceId).First(&device).Error; err != nil {
		return nil, &session, err
	}
	if device.Status == ClawXDeviceStatusRevoked {
		return nil, &session, ErrTokenInvalid
	}
	DB.Model(&session).Updates(map[string]interface{}{
		"last_seen_at": now,
		"updated_at":   now,
	})
	return user, &session, nil
}

func ValidateClawXRefreshToken(token string) (*User, *ClawXSession, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, nil, ErrTokenNotProvided
	}
	now := common.GetTimestamp()
	var session ClawXSession
	if err := DB.Where("refresh_token_hash = ?", HashClawXSecret(token)).First(&session).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, ErrTokenInvalid
		}
		return nil, nil, err
	}
	if session.Status != ClawXSessionStatusActive || session.RefreshExpiresAt <= now {
		return nil, &session, ErrTokenInvalid
	}
	user, err := GetUserById(session.UserId, true)
	if err != nil {
		return nil, &session, err
	}
	if user.Status != common.UserStatusEnabled {
		return nil, &session, ErrTokenInvalid
	}
	return user, &session, nil
}

func RotateClawXSession(sessionId int, accessToken string, refreshToken string, accessExpiresAt int64, refreshExpiresAt int64) error {
	if sessionId <= 0 || strings.TrimSpace(accessToken) == "" || strings.TrimSpace(refreshToken) == "" {
		return errors.New("invalid session rotation")
	}
	now := common.GetTimestamp()
	return DB.Model(&ClawXSession{}).Where("id = ?", sessionId).Updates(map[string]interface{}{
		"access_token_hash":  HashClawXSecret(accessToken),
		"refresh_token_hash": HashClawXSecret(refreshToken),
		"access_expires_at":  accessExpiresAt,
		"refresh_expires_at": refreshExpiresAt,
		"updated_at":         now,
		"last_seen_at":       now,
	}).Error
}

func RevokeClawXSessionByRefreshToken(refreshToken string) error {
	refreshToken = strings.TrimSpace(refreshToken)
	if refreshToken == "" {
		return nil
	}
	return DB.Model(&ClawXSession{}).
		Where("refresh_token_hash = ?", HashClawXSecret(refreshToken)).
		Updates(map[string]interface{}{
			"status":     ClawXSessionStatusRevoked,
			"updated_at": common.GetTimestamp(),
		}).Error
}

func GetClawXActivationTicket(ticket string) (*ClawXActivationTicket, error) {
	var activationTicket ClawXActivationTicket
	if err := DB.Where("ticket_hash = ?", HashClawXSecret(ticket)).First(&activationTicket).Error; err != nil {
		return nil, err
	}
	return &activationTicket, nil
}

func CreateClawXActivationTicket(ticket string, redemptionId int, activationCode string, deviceId string, expiresAt int64) (*ClawXActivationTicket, error) {
	now := common.GetTimestamp()
	activationTicket := &ClawXActivationTicket{
		TicketHash:     HashClawXSecret(ticket),
		RedemptionId:   redemptionId,
		DeviceId:       strings.TrimSpace(deviceId),
		ExpiresAt:      expiresAt,
		Status:         ClawXActivationTicketStatusActive,
		CreatedAt:      now,
		ActivationCode: strings.TrimSpace(activationCode),
	}
	if err := DB.Create(activationTicket).Error; err != nil {
		return nil, err
	}
	return activationTicket, nil
}

func MarkClawXActivationTicketUsed(tx *gorm.DB, id int, userId int) error {
	if tx == nil {
		tx = DB
	}
	return tx.Model(&ClawXActivationTicket{}).Where("id = ?", id).Updates(map[string]interface{}{
		"status":       ClawXActivationTicketStatusUsed,
		"used_at":      common.GetTimestamp(),
		"used_user_id": userId,
	}).Error
}
