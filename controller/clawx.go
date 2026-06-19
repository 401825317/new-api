package controller

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Calcium-Ion/go-epay/epay"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/clawx_client_setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	defaultClawXOrigin       = "https://zz-cn.lingzhiwuxian.com"
	defaultClawXProviderKey  = "lingzhiwuxian"
	defaultClawXProviderName = "灵智无限"
	defaultClawXModel        = "smart-latest"

	defaultClawXAuthAccessTTLSeconds  = 24 * 60 * 60
	defaultClawXAuthRefreshTTLSeconds = 10 * 365 * 24 * 60 * 60
)

type clawXDevicePayload struct {
	Id         string `json:"id"`
	Name       string `json:"name"`
	Platform   string `json:"platform"`
	Arch       string `json:"arch"`
	AppVersion string `json:"appVersion"`
}

type clawXAuthRequest struct {
	Account          string             `json:"account"`
	Email            string             `json:"email"`
	Username         string             `json:"username"`
	Password         string             `json:"password"`
	VerifyCode       string             `json:"verifyCode"`
	VerificationCode string             `json:"verification_code"`
	ActivationTicket string             `json:"activationTicket"`
	ActivationCode   string             `json:"activationCode"`
	AffCode          string             `json:"affCode"`
	Device           clawXDevicePayload `json:"device"`
}

type clawXRefreshRequest struct {
	RefreshToken      string `json:"refresh_token"`
	RefreshTokenCamel string `json:"refreshToken"`
}

type clawXActivationCheckRequest struct {
	Code   string             `json:"code"`
	Device clawXDevicePayload `json:"device"`
}

type clawXVerifyRequest struct {
	Device  clawXDevicePayload     `json:"device"`
	Runtime map[string]interface{} `json:"runtime"`
}

type clawXRelayTokenRequest struct {
	Device clawXDevicePayload `json:"device"`
	Scope  []string           `json:"scope"`
}

type clawXBillingOrderRequest struct {
	Amount        int64   `json:"amount"`
	Money         float64 `json:"money"`
	PaymentType   string  `json:"payment_type"`
	EpayMethod    string  `json:"epayMethod"`
	OrderType     string  `json:"order_type"`
	PlanId        int     `json:"plan_id"`
	IsMobile      bool    `json:"is_mobile"`
	PaymentSource string  `json:"payment_source"`
}

type clawXBillingOrderVerifyRequest struct {
	OutTradeNo string `json:"out_trade_no"`
	TradeNo    string `json:"trade_no"`
}

func clawXEnv(key string, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func clawXBoolEnv(key string, fallback bool) bool {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	switch strings.ToLower(raw) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func clawXOrigin() string {
	return strings.TrimRight(clawXEnv("CLAWX_PUBLIC_ORIGIN", defaultClawXOrigin), "/")
}

func clawXProviderBaseURL() string {
	base := strings.TrimRight(clawXEnv("CLAWX_PROVIDER_BASE_URL", clawXOrigin()+"/v1"), "/")
	if strings.HasSuffix(base, "/v1") {
		return base
	}
	return base + "/v1"
}

func clawXModelFamilies() []gin.H {
	raw := strings.TrimSpace(os.Getenv("CLAWX_MODEL_FAMILIES"))
	if raw == "" {
		return []gin.H{
			{"id": "smart-latest", "name": "智能路由", "family": "smart"},
			{"id": "qwen-latest", "name": "通义千问最新版", "family": "qwen"},
			{"id": "deepseek-latest", "name": "DeepSeek 最新版", "family": "deepseek"},
			{"id": "doubao-latest", "name": "豆包最新版", "family": "doubao"},
			{"id": "kimi-latest", "name": "Kimi 最新版", "family": "kimi"},
			{"id": "glm-latest", "name": "GLM 最新版", "family": "glm"},
		}
	}
	result := make([]gin.H, 0)
	for _, item := range strings.Split(raw, ",") {
		parts := strings.SplitN(strings.TrimSpace(item), ":", 2)
		id := strings.TrimSpace(parts[0])
		if id == "" {
			continue
		}
		name := id
		if len(parts) == 2 && strings.TrimSpace(parts[1]) != "" {
			name = strings.TrimSpace(parts[1])
		}
		result = append(result, gin.H{"id": id, "name": name, "family": strings.TrimSuffix(id, "-latest")})
	}
	return result
}

func clawXRuntimePayload() gin.H {
	defaultModel := clawXEnv("CLAWX_DEFAULT_MODEL", defaultClawXModel)
	fallbackModels := make([]string, 0)
	for _, item := range strings.Split(os.Getenv("CLAWX_FALLBACK_MODELS"), ",") {
		if modelName := strings.TrimSpace(item); modelName != "" {
			fallbackModels = append(fallbackModels, modelName)
		}
	}
	return gin.H{
		"providerKey":    clawXEnv("CLAWX_PROVIDER_KEY", defaultClawXProviderKey),
		"providerName":   clawXEnv("CLAWX_PROVIDER_NAME", defaultClawXProviderName),
		"baseUrl":        clawXProviderBaseURL(),
		"apiProtocol":    clawXEnv("CLAWX_API_PROTOCOL", "openai-completions"),
		"defaultModel":   defaultModel,
		"fallbackModels": fallbackModels,
		"modelFamilies":  clawXModelFamilies(),
	}
}

func clawXOfflinePayload() gin.H {
	return gin.H{
		"graceSeconds":             int64(common.GetEnvOrDefault("CLAWX_OFFLINE_GRACE_SECONDS", 7*24*60*60)),
		"verifyMemoryCacheSeconds": int64(common.GetEnvOrDefault("CLAWX_VERIFY_MEMORY_CACHE_SECONDS", 300)),
	}
}

func clawXBootstrapPayload() gin.H {
	return gin.H{
		"service": gin.H{
			"name":        clawXEnv("CLAWX_SERVICE_NAME", "lingzhiwuxian"),
			"displayName": clawXEnv("CLAWX_SERVICE_DISPLAY_NAME", defaultClawXProviderName),
			"apiOrigin":   clawXOrigin(),
		},
		"auth": gin.H{
			"registrationEnabled": common.RegisterEnabled && common.PasswordRegisterEnabled && clawXBoolEnv("CLAWX_REGISTRATION_ENABLED", true),
			"loginEnabled":        common.PasswordLoginEnabled && clawXBoolEnv("CLAWX_LOGIN_ENABLED", true),
			"activationRequired":  clawXBoolEnv("CLAWX_ACTIVATION_REQUIRED", true),
		},
		"runtime": clawXRuntimePayload(),
		"offline": clawXOfflinePayload(),
		"skills": gin.H{
			"bundledOpenClawEnabled":   true,
			"remoteMarketplaceEnabled": clawXBoolEnv("CLAWX_SKILL_MARKETPLACE_ENABLED", false),
			"remoteMarketplaceBaseUrl": nil,
		},
		"client": clawXClientConfigPayload(),
	}
}

func ClawXBootstrap(c *gin.Context) {
	common.ApiSuccess(c, clawXBootstrapPayload())
}

func clawXClientConfigPayload() gin.H {
	support := clawx_client_setting.GetSupport()
	supportContacts := support.Contacts
	supportEnabled := clawx_client_setting.GetClientSetting().SupportEnabled && len(supportContacts) > 0
	firstSupportContact := clawx_client_setting.SupportContact{}
	if len(supportContacts) > 0 {
		firstSupportContact = supportContacts[0]
	}
	return gin.H{
		"announcements": gin.H{
			"enabled": clawx_client_setting.GetClientSetting().AnnouncementsEnabled,
			"items":   clawx_client_setting.GetAnnouncements(),
		},
		"support": gin.H{
			"enabled":     supportEnabled,
			"title":       support.Title,
			"description": support.Description,
			"contacts":    supportContacts,
			"qrCodeUrl":   firstSupportContact.QrCodeUrl,
			"workHours":   firstSupportContact.WorkHours,
			"wechatId":    firstSupportContact.WechatId,
			"extraNote":   firstSupportContact.ExtraNote,
		},
	}
}

func ClawXClientConfig(c *gin.Context) {
	common.ApiSuccess(c, clawXClientConfigPayload())
}

func clawXRandomSecret(prefix string) (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return prefix + base64.RawURLEncoding.EncodeToString(buf), nil
}

func normalizeClawXDevice(payload clawXDevicePayload) model.ClawXDevice {
	return model.ClawXDevice{
		DeviceId:   strings.TrimSpace(payload.Id),
		Name:       strings.TrimSpace(payload.Name),
		Platform:   strings.TrimSpace(payload.Platform),
		Arch:       strings.TrimSpace(payload.Arch),
		AppVersion: strings.TrimSpace(payload.AppVersion),
	}
}

func normalizeClawXAccount(req clawXAuthRequest) (account string, email string) {
	account = strings.TrimSpace(req.Account)
	if account == "" {
		account = strings.TrimSpace(req.Email)
	}
	if account == "" {
		account = strings.TrimSpace(req.Username)
	}
	if strings.Contains(account, "@") {
		email = account
	} else {
		candidate := strings.TrimSpace(req.Email)
		if strings.Contains(candidate, "@") {
			email = candidate
		}
	}
	return account, email
}

func clawXApiErrorMsg(c *gin.Context, status int, msg string) {
	c.JSON(status, gin.H{
		"success": false,
		"message": msg,
	})
}

func clawXApiError(c *gin.Context, status int, code string, msg string) {
	c.JSON(status, gin.H{
		"success":   false,
		"code":      code,
		"errorCode": code,
		"message":   msg,
	})
}

func clawXActivationErrorMessage(code string) string {
	switch code {
	case "activation_expired":
		return "激活码已过期，请联系客服获取新的激活码"
	case "activation_consumed":
		return "激活码已使用，请联系客服获取新的激活码"
	case "activation_ticket_expired":
		return "激活校验已过期，请重新输入激活码"
	case "activation_device_mismatch":
		return "激活码校验设备不一致，请重新输入激活码"
	default:
		return "激活码无效，请联系客服获取新的激活码"
	}
}

func normalizeClawXUsernameBase(account string) string {
	base := strings.TrimSpace(account)
	if at := strings.Index(base, "@"); at > 0 {
		base = base[:at]
	}
	base = strings.ToLower(base)
	var b strings.Builder
	for _, r := range base {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			b.WriteRune(r)
		}
	}
	username := strings.Trim(b.String(), "-_")
	if len(username) > model.UserNameMaxLength {
		username = username[:model.UserNameMaxLength]
	}
	return username
}

func buildClawXUsername(account string) string {
	username := normalizeClawXUsernameBase(account)
	if username == "" {
		username = "clawx"
	}
	origin := username
	for i := 0; ; i++ {
		var count int64
		model.DB.Unscoped().Model(&model.User{}).Where("username = ?", username).Count(&count)
		if count == 0 {
			return username
		}
		suffix := strconv.Itoa(i + 1)
		maxBase := model.UserNameMaxLength - len(suffix)
		if maxBase < 1 {
			maxBase = 1
		}
		if len(origin) > maxBase {
			username = origin[:maxBase] + suffix
		} else {
			username = origin + suffix
		}
	}
}

func findClawXUserByLoginIdentifier(identifier string) (*model.User, error) {
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return nil, gorm.ErrRecordNotFound
	}
	var user model.User
	if err := model.DB.Where("username = ? OR email = ?", identifier, identifier).First(&user).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

func findClawXLoginUser(account string, email string, password string) (*model.User, error) {
	candidates := []string{
		strings.TrimSpace(account),
		strings.TrimSpace(email),
		normalizeClawXUsernameBase(account),
	}
	seen := map[string]bool{}
	var lastErr error
	for _, candidate := range candidates {
		if candidate == "" || seen[candidate] {
			continue
		}
		seen[candidate] = true
		user, err := findClawXUserByLoginIdentifier(candidate)
		if err != nil {
			lastErr = err
			if errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			return nil, err
		}
		if !common.ValidatePasswordAndHash(password, user.Password) || user.Status != common.UserStatusEnabled {
			return nil, errors.New("invalid_credentials")
		}
		return user, nil
	}
	if lastErr != nil && !errors.Is(lastErr, gorm.ErrRecordNotFound) {
		return nil, lastErr
	}
	return nil, gorm.ErrRecordNotFound
}

func clawXSessionTTLs() (int64, int64) {
	return int64(common.GetEnvOrDefault("CLAWX_AUTH_ACCESS_TTL_SECONDS", defaultClawXAuthAccessTTLSeconds)),
		int64(common.GetEnvOrDefault("CLAWX_AUTH_REFRESH_TTL_SECONDS", defaultClawXAuthRefreshTTLSeconds))
}

func createClawXAuthResponse(user *model.User, device model.ClawXDevice) (gin.H, error) {
	accessToken, err := clawXRandomSecret("cxa_")
	if err != nil {
		return nil, err
	}
	refreshToken, err := clawXRandomSecret("cxr_")
	if err != nil {
		return nil, err
	}
	accessTTL, refreshTTL := clawXSessionTTLs()
	now := common.GetTimestamp()
	if _, err := model.CreateClawXSession(user.Id, device.DeviceId, accessToken, refreshToken, now+accessTTL, now+refreshTTL); err != nil {
		return nil, err
	}
	return gin.H{
		"accessToken":  accessToken,
		"refreshToken": refreshToken,
		"expiresIn":    accessTTL,
		"tokenType":    "Bearer",
		"user":         clawXUserPayload(user),
		"device": gin.H{
			"id":     device.DeviceId,
			"status": device.Status,
		},
		"runtime": clawXRuntimePayload(),
		"offline": clawXOfflinePayload(),
	}, nil
}

func clawXUserPayload(user *model.User) gin.H {
	balance := 0.0
	if common.QuotaPerUnit > 0 {
		balance = float64(user.Quota) / common.QuotaPerUnit
	}
	return gin.H{
		"id":           user.Id,
		"email":        user.Email,
		"username":     user.Username,
		"displayName":  user.DisplayName,
		"group":        user.Group,
		"quota":        user.Quota,
		"used_quota":   user.UsedQuota,
		"balance":      balance,
		"shrimp_quota": balance,
	}
}

func activationCodeVariants(code string) []string {
	clean := strings.TrimSpace(code)
	if clean == "" {
		return nil
	}
	noDash := strings.ReplaceAll(clean, "-", "")
	noSpace := strings.Join(strings.Fields(clean), "")
	variants := []string{clean, noDash, noSpace, strings.ToUpper(clean), strings.ToLower(clean), strings.ToUpper(noDash), strings.ToLower(noDash)}
	seen := map[string]bool{}
	result := make([]string, 0, len(variants))
	for _, item := range variants {
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		result = append(result, item)
	}
	return result
}

func findClawXRedemptionByCode(code string, tx *gorm.DB) (*model.Redemption, error) {
	var redemption model.Redemption
	query := model.DB
	if tx != nil {
		query = tx
	}
	keyCol := "`key`"
	if common.UsingPostgreSQL {
		keyCol = `"key"`
	}
	variants := activationCodeVariants(code)
	if len(variants) == 0 {
		return nil, errors.New("activation code is required")
	}
	if err := query.Where(keyCol+" IN ?", variants).First(&redemption).Error; err != nil {
		return nil, err
	}
	return &redemption, nil
}

func validateClawXRedemption(redemption *model.Redemption) error {
	if redemption == nil {
		return errors.New("activation_invalid")
	}
	if redemption.Status != common.RedemptionCodeStatusEnabled {
		return errors.New("activation_consumed")
	}
	if redemption.ExpiredTime != 0 && redemption.ExpiredTime < common.GetTimestamp() {
		return errors.New("activation_expired")
	}
	return nil
}

func clawXActivationQuota(redemption *model.Redemption) int {
	if redemption != nil {
		return redemption.Quota
	}
	return int(float64(common.GetEnvOrDefault("CLAWX_ACTIVATION_GIFT_UNITS", 5)) * common.QuotaPerUnit)
}

func resolveClawXActivationTicket(ticketOrCode string, fallbackCode string, deviceId string) (*model.ClawXActivationTicket, *model.Redemption, error) {
	token := strings.TrimSpace(ticketOrCode)
	if token != "" {
		if ticket, err := model.GetClawXActivationTicket(token); err == nil {
			if ticket.Status != model.ClawXActivationTicketStatusActive || ticket.ExpiresAt < common.GetTimestamp() {
				return nil, nil, errors.New("activation_ticket_expired")
			}
			if ticket.DeviceId != "" && deviceId != "" && ticket.DeviceId != deviceId {
				return nil, nil, errors.New("activation_device_mismatch")
			}
			redemption, err := findClawXRedemptionByCode(ticket.ActivationCode, nil)
			if err != nil {
				return nil, nil, errors.New("activation_invalid")
			}
			if err := validateClawXRedemption(redemption); err != nil {
				return nil, nil, err
			}
			return ticket, redemption, nil
		}
	}
	code := strings.TrimSpace(fallbackCode)
	if code == "" {
		code = token
	}
	redemption, err := findClawXRedemptionByCode(code, nil)
	if err != nil {
		return nil, nil, errors.New("activation_invalid")
	}
	if err := validateClawXRedemption(redemption); err != nil {
		return nil, nil, err
	}
	rawTicket, err := clawXRandomSecret("cxt_")
	if err != nil {
		return nil, nil, err
	}
	expiresAt := common.GetTimestamp() + int64(common.GetEnvOrDefault("CLAWX_ACTIVATION_TICKET_TTL_SECONDS", 10*60))
	ticket, err := model.CreateClawXActivationTicket(rawTicket, redemption.Id, redemption.Key, deviceId, expiresAt)
	if err != nil {
		return nil, nil, err
	}
	ticket.ActivationCode = rawTicket
	return ticket, redemption, nil
}

func consumeClawXActivationForUser(tx *gorm.DB, ticket *model.ClawXActivationTicket, redemption *model.Redemption, userId int) error {
	if ticket == nil || redemption == nil {
		return nil
	}
	keyCol := "`key`"
	if common.UsingPostgreSQL {
		keyCol = `"key"`
	}
	if err := tx.Model(&model.Redemption{}).
		Where(keyCol+" = ? AND status = ?", redemption.Key, common.RedemptionCodeStatusEnabled).
		Updates(map[string]interface{}{
			"status":        common.RedemptionCodeStatusUsed,
			"redeemed_time": common.GetTimestamp(),
			"used_user_id":  userId,
		}).Error; err != nil {
		return err
	}
	return model.MarkClawXActivationTicketUsed(tx, ticket.Id, userId)
}

func ClawXActivationCheck(c *gin.Context) {
	var req clawXActivationCheckRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	redemption, err := findClawXRedemptionByCode(req.Code, nil)
	if err != nil {
		common.ApiSuccess(c, gin.H{
			"valid":     false,
			"errorCode": "activation_invalid",
			"message":   clawXActivationErrorMessage("activation_invalid"),
		})
		return
	}
	if err := validateClawXRedemption(redemption); err != nil {
		code := err.Error()
		common.ApiSuccess(c, gin.H{
			"valid":     false,
			"errorCode": code,
			"message":   clawXActivationErrorMessage(code),
		})
		return
	}
	rawTicket, err := clawXRandomSecret("cxt_")
	if err != nil {
		common.ApiError(c, err)
		return
	}
	expiresIn := int64(common.GetEnvOrDefault("CLAWX_ACTIVATION_TICKET_TTL_SECONDS", 10*60))
	if _, err := model.CreateClawXActivationTicket(rawTicket, redemption.Id, redemption.Key, strings.TrimSpace(req.Device.Id), common.GetTimestamp()+expiresIn); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{
		"valid":                true,
		"requiresRegistration": true,
		"activationTicket":     rawTicket,
		"expiresIn":            expiresIn,
		"entitlementPreview": gin.H{
			"groupId":   "default",
			"balance":   float64(clawXActivationQuota(redemption)) / common.QuotaPerUnit,
			"expiresAt": nil,
		},
	})
}

func ClawXRegister(c *gin.Context) {
	if !common.RegisterEnabled || !common.PasswordRegisterEnabled {
		clawXApiError(c, http.StatusForbidden, "registration_disabled", "注册已关闭")
		return
	}
	var req clawXAuthRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		clawXApiError(c, http.StatusBadRequest, "invalid_request", "参数错误")
		return
	}
	account, email := normalizeClawXAccount(req)
	if account == "" || req.Password == "" {
		clawXApiError(c, http.StatusBadRequest, "missing_credentials", "账号和密码不能为空")
		return
	}
	if len(req.Password) < 8 || len(req.Password) > 20 {
		clawXApiError(c, http.StatusBadRequest, "password_policy", "密码长度需为 8-20 位")
		return
	}
	if email != "" {
		if err := common.Validate.Var(email, "email"); err != nil {
			clawXApiError(c, http.StatusBadRequest, "invalid_email", "邮箱格式错误")
			return
		}
		if model.IsEmailAlreadyTaken(email) {
			clawXApiError(c, http.StatusConflict, "email_taken", "邮箱已被占用")
			return
		}
	}
	if common.EmailVerificationEnabled {
		code := strings.TrimSpace(req.VerifyCode)
		if code == "" {
			code = strings.TrimSpace(req.VerificationCode)
		}
		if email == "" || code == "" || !common.VerifyCodeWithKey(email, code, common.EmailVerificationPurpose) {
			clawXApiError(c, http.StatusBadRequest, "verification_invalid", "邮箱验证码错误")
			return
		}
	}
	device := normalizeClawXDevice(req.Device)
	if device.DeviceId == "" {
		clawXApiError(c, http.StatusBadRequest, "device_required", "缺少设备 ID")
		return
	}
	username := normalizeClawXUsernameBase(account)
	if username == "" {
		clawXApiError(c, http.StatusBadRequest, "invalid_username", "用户名格式错误")
		return
	}
	exist, err := model.CheckUserExistOrDeleted(username, email)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if exist {
		clawXApiError(c, http.StatusConflict, "user_exists", "用户名已存在")
		return
	}
	var ticket *model.ClawXActivationTicket
	var redemption *model.Redemption
	if clawXBoolEnv("CLAWX_ACTIVATION_REQUIRED", true) {
		var err error
		ticket, redemption, err = resolveClawXActivationTicket(req.ActivationTicket, req.ActivationCode, device.DeviceId)
		if err != nil {
			code := err.Error()
			clawXApiError(c, http.StatusBadRequest, code, clawXActivationErrorMessage(code))
			return
		}
	}
	var user model.User
	err = model.DB.Transaction(func(tx *gorm.DB) error {
		cleanUser := model.User{
			Username:    username,
			Password:    req.Password,
			DisplayName: username,
			Email:       email,
			Role:        common.RoleCommonUser,
			Status:      common.UserStatusEnabled,
		}
		if err := cleanUser.InsertWithTx(tx, 0); err != nil {
			return err
		}
		giftQuota := clawXActivationQuota(redemption)
		if giftQuota > 0 {
			if err := tx.Model(&model.User{}).Where("id = ?", cleanUser.Id).Update("quota", gorm.Expr("quota + ?", giftQuota)).Error; err != nil {
				return err
			}
			cleanUser.Quota += giftQuota
		}
		device.UserId = cleanUser.Id
		device.Status = model.ClawXDeviceStatusActive
		device.CreatedAt = common.GetTimestamp()
		device.UpdatedAt = device.CreatedAt
		device.LastSeenAt = device.CreatedAt
		if err := tx.Create(&device).Error; err != nil {
			return err
		}
		if ticket != nil && redemption != nil {
			if err := consumeClawXActivationForUser(tx, ticket, redemption, cleanUser.Id); err != nil {
				return err
			}
		}
		user = cleanUser
		return nil
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	user.FinalizeOAuthUserCreation(0)
	model.RecordLog(user.Id, model.LogTypeSystem, fmt.Sprintf("ClawX 激活赠送 %s", logger.LogQuota(clawXActivationQuota(redemption))))
	response, err := createClawXAuthResponse(&user, device)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, response)
}

func ClawXLogin(c *gin.Context) {
	if !common.PasswordLoginEnabled {
		clawXApiError(c, http.StatusForbidden, "login_disabled", "登录已关闭")
		return
	}
	var req clawXAuthRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		clawXApiError(c, http.StatusBadRequest, "invalid_request", "参数错误")
		return
	}
	account, email := normalizeClawXAccount(req)
	if account == "" || req.Password == "" {
		clawXApiError(c, http.StatusBadRequest, "missing_credentials", "账号和密码不能为空")
		return
	}
	user, err := findClawXLoginUser(account, email, req.Password)
	if err != nil {
		clawXApiError(c, http.StatusUnauthorized, "invalid_credentials", "账号或密码错误")
		return
	}
	devicePayload := normalizeClawXDevice(req.Device)
	if devicePayload.DeviceId == "" {
		clawXApiError(c, http.StatusBadRequest, "device_required", "缺少设备 ID")
		return
	}
	device, err := model.GetClawXDevice(user.Id, devicePayload.DeviceId)
	deviceExists := err == nil
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		common.ApiError(c, err)
		return
	}
	deviceAuthorized := deviceExists && device.Status == model.ClawXDeviceStatusActive
	if !deviceAuthorized && clawXBoolEnv("CLAWX_ACTIVATION_REQUIRED", true) {
		if strings.TrimSpace(req.ActivationTicket) == "" && strings.TrimSpace(req.ActivationCode) == "" {
			clawXApiError(c, http.StatusForbidden, "device_authorization_required", "当前设备需要激活码授权后才能使用")
			return
		}
		ticket, redemption, activationErr := resolveClawXActivationTicket(req.ActivationTicket, req.ActivationCode, devicePayload.DeviceId)
		if activationErr != nil {
			code := activationErr.Error()
			clawXApiError(c, http.StatusBadRequest, code, clawXActivationErrorMessage(code))
			return
		}
		err = model.DB.Transaction(func(tx *gorm.DB) error {
			devicePayload.UserId = user.Id
			devicePayload.Status = model.ClawXDeviceStatusActive
			now := common.GetTimestamp()
			devicePayload.CreatedAt = now
			devicePayload.UpdatedAt = now
			devicePayload.LastSeenAt = now
			if deviceExists {
				if err := tx.Model(&model.ClawXDevice{}).
					Where("user_id = ? AND device_id = ?", user.Id, devicePayload.DeviceId).
					Updates(map[string]interface{}{
						"name":         devicePayload.Name,
						"platform":     devicePayload.Platform,
						"arch":         devicePayload.Arch,
						"app_version":  devicePayload.AppVersion,
						"status":       model.ClawXDeviceStatusActive,
						"updated_at":   now,
						"last_seen_at": now,
					}).Error; err != nil {
					return err
				}
			} else {
				if err := tx.Create(&devicePayload).Error; err != nil {
					return err
				}
			}
			return consumeClawXActivationForUser(tx, ticket, redemption, user.Id)
		})
		if err != nil {
			common.ApiError(c, err)
			return
		}
		device = &devicePayload
	} else if deviceAuthorized {
		device, err = model.UpsertClawXDevice(user.Id, devicePayload)
		if err != nil {
			common.ApiError(c, err)
			return
		}
	} else {
		device, err = model.UpsertClawXDevice(user.Id, devicePayload)
		if err != nil {
			common.ApiError(c, err)
			return
		}
	}
	model.UpdateUserLastLoginAt(user.Id)
	response, err := createClawXAuthResponse(user, *device)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, response)
}

func ClawXRefresh(c *gin.Context) {
	var req clawXRefreshRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	refreshToken := strings.TrimSpace(req.RefreshToken)
	if refreshToken == "" {
		refreshToken = strings.TrimSpace(req.RefreshTokenCamel)
	}
	user, session, err := model.ValidateClawXRefreshToken(refreshToken)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "message": "refresh token invalid", "code": "refresh_invalid"})
		return
	}
	accessToken, err := clawXRandomSecret("cxa_")
	if err != nil {
		common.ApiError(c, err)
		return
	}
	nextRefreshToken, err := clawXRandomSecret("cxr_")
	if err != nil {
		common.ApiError(c, err)
		return
	}
	accessTTL, refreshTTL := clawXSessionTTLs()
	now := common.GetTimestamp()
	if err := model.RotateClawXSession(session.Id, accessToken, nextRefreshToken, now+accessTTL, now+refreshTTL); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{
		"accessToken":  accessToken,
		"refreshToken": nextRefreshToken,
		"expiresIn":    accessTTL,
		"tokenType":    "Bearer",
		"user":         clawXUserPayload(user),
		"runtime":      clawXRuntimePayload(),
		"offline":      clawXOfflinePayload(),
	})
}

func ClawXLogout(c *gin.Context) {
	var req clawXRefreshRequest
	_ = c.ShouldBindJSON(&req)
	refreshToken := strings.TrimSpace(req.RefreshToken)
	if refreshToken == "" {
		refreshToken = strings.TrimSpace(req.RefreshTokenCamel)
	}
	if err := model.RevokeClawXSessionByRefreshToken(refreshToken); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"revoked": true})
}

func ClawXSendVerificationCode(c *gin.Context) {
	var req map[string]interface{}
	if err := c.ShouldBindJSON(&req); err != nil {
		clawXApiError(c, http.StatusBadRequest, "invalid_request", "参数错误")
		return
	}
	email := strings.TrimSpace(fmt.Sprintf("%v", req["email"]))
	if email == "" {
		email = strings.TrimSpace(fmt.Sprintf("%v", req["account"]))
	}
	if err := common.Validate.Var(email, "required,email"); err != nil {
		clawXApiError(c, http.StatusBadRequest, "invalid_email", "无效的邮箱地址")
		return
	}
	if model.IsEmailAlreadyTaken(email) {
		clawXApiError(c, http.StatusConflict, "email_taken", "邮箱地址已被占用")
		return
	}
	code := common.GenerateVerificationCode(6)
	common.RegisterVerificationCodeWithKey(email, code, common.EmailVerificationPurpose)
	subject := fmt.Sprintf("%s 邮箱验证", common.SystemName)
	content := fmt.Sprintf("<p>您好，你正在进行 %s 邮箱验证。</p><p>验证码为：<strong>%s</strong></p><p>验证码 %d 分钟内有效。</p>", common.SystemName, code, common.VerificationValidMinutes)
	if err := common.SendEmail(subject, email, content); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"message": "sent"})
}

func ClawXAuthVerify(c *gin.Context) {
	userId := c.GetInt("id")
	user, err := model.GetUserById(userId, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	deviceId := c.GetString("clawx_device_id")
	device, err := model.GetClawXDevice(userId, deviceId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var req clawXVerifyRequest
	_ = c.ShouldBindJSON(&req)
	if strings.TrimSpace(req.Device.Id) != "" {
		if updated, err := model.UpsertClawXDevice(userId, normalizeClawXDevice(req.Device)); err == nil {
			device = updated
		}
	}
	common.ApiSuccess(c, gin.H{
		"valid":      true,
		"serverTime": time.Now().UTC().Format(time.RFC3339),
		"user":       clawXUserPayload(user),
		"device": gin.H{
			"id":         device.DeviceId,
			"status":     device.Status,
			"lastSeenAt": time.Unix(device.LastSeenAt, 0).UTC().Format(time.RFC3339),
		},
		"entitlements": gin.H{
			"providerEnabled":     true,
			"modelGatewayEnabled": true,
			"skillsEnabled":       true,
			"groupIds":            []string{user.Group},
		},
		"runtime": clawXRuntimePayload(),
		"offline": clawXOfflinePayload(),
	})
}

func ClawXUnregisterDevice(c *gin.Context) {
	var req struct {
		DeviceId string `json:"deviceId"`
	}
	_ = c.ShouldBindJSON(&req)
	if req.DeviceId == "" {
		req.DeviceId = c.GetString("clawx_device_id")
	}
	if err := model.RevokeClawXDevice(c.GetInt("id"), req.DeviceId); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"removed": true})
}

func ClawXUserSelf(c *gin.Context) {
	user, err := model.GetUserById(c.GetInt("id"), false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	subscriptions, _ := model.GetAllActiveUserSubscriptions(user.Id)
	common.ApiSuccess(c, gin.H{
		"user":          clawXUserPayload(user),
		"subscriptions": subscriptions,
		"runtime":       clawXRuntimePayload(),
	})
}

func ensureClawXDeviceForContext(c *gin.Context, payload clawXDevicePayload) string {
	deviceId := strings.TrimSpace(payload.Id)
	if deviceId == "" {
		deviceId = c.GetString("clawx_device_id")
	}
	if deviceId != "" {
		device := normalizeClawXDevice(payload)
		if device.DeviceId == "" {
			device.DeviceId = deviceId
		}
		_, _ = model.UpsertClawXDevice(c.GetInt("id"), device)
	}
	return deviceId
}

func ClawXRelayToken(c *gin.Context) {
	var req clawXRelayTokenRequest
	_ = c.ShouldBindJSON(&req)
	userId := c.GetInt("id")
	deviceId := ensureClawXDeviceForContext(c, req.Device)
	if deviceId == "" {
		common.ApiErrorMsg(c, "缺少设备 ID")
		return
	}
	tokenName := "ClawX " + deviceId
	var token model.Token
	err := model.DB.Where("user_id = ? AND name = ? AND status = ?", userId, tokenName, common.TokenStatusEnabled).Order("id desc").First(&token).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		key, err := common.GenerateKey()
		if err != nil {
			common.ApiError(c, err)
			return
		}
		token = model.Token{
			UserId:             userId,
			Name:               tokenName,
			Key:                key,
			CreatedTime:        common.GetTimestamp(),
			AccessedTime:       common.GetTimestamp(),
			ExpiredTime:        -1,
			RemainQuota:        0,
			UnlimitedQuota:     true,
			ModelLimitsEnabled: false,
			Group:              "auto",
		}
		if err := token.Insert(); err != nil {
			common.ApiError(c, err)
			return
		}
	} else if err != nil {
		common.ApiError(c, err)
		return
	}
	_ = model.SetClawXDeviceToken(userId, deviceId, token.Id)
	common.ApiSuccess(c, gin.H{
		"token":     token.GetFullKey(),
		"tokenType": "new-api-api-key",
		"expiresIn": nil,
		"runtime":   clawXRuntimePayload(),
	})
}

func clawXTopupOverviewPayload(user *model.User) gin.H {
	complianceConfirmed := operation_setting.IsPaymentComplianceConfirmed()
	payMethods := buildAvailableStandardPayMethods()
	methods := make([]gin.H, 0, len(payMethods))
	for _, method := range payMethods {
		methodType := strings.TrimSpace(method["type"])
		if methodType == "" {
			continue
		}
		methods = append(methods, gin.H{
			"type": methodType,
			"name": strings.TrimSpace(method["name"]),
		})
	}
	quotaUnits := 0.0
	if common.QuotaPerUnit > 0 {
		quotaUnits = float64(user.Quota) / common.QuotaPerUnit
	}
	return gin.H{
		"user": gin.H{
			"id":           user.Id,
			"username":     user.Username,
			"email":        user.Email,
			"quota":        user.Quota,
			"used_quota":   user.UsedQuota,
			"balance":      quotaUnits,
			"shrimp_quota": quotaUnits,
		},
		"quotaPerUnit": common.QuotaPerUnit,
		"topupInfo": gin.H{
			"payg_current_quota":      user.Quota,
			"payg_credit_usd_per_cny": 1,
			"enable_online_topup":     complianceConfirmed && len(methods) > 0,
			"pay_methods":             common.GetJsonString(methods),
			"payg_products": []gin.H{{
				"id":                1,
				"name":              "余额充值",
				"description":       "充值后自动增加账户余额",
				"enabled":           true,
				"sort_order":        0,
				"stock":             nil,
				"allowed_group_ids": []int{1},
			}},
			"subscription_products": func() []gin.H {
				var plans []model.SubscriptionPlan
				if !complianceConfirmed {
					return []gin.H{}
				}
				if err := model.DB.Where("enabled = ?", true).Order("sort_order desc, id desc").Find(&plans).Error; err != nil {
					return []gin.H{}
				}
				result := make([]gin.H, 0, len(plans))
				for _, plan := range plans {
					result = append(result, gin.H{
						"id":            plan.Id,
						"name":          plan.Title,
						"description":   plan.Subtitle,
						"price":         plan.PriceAmount,
						"currency":      plan.Currency,
						"upgrade_group": plan.UpgradeGroup,
						"total_amount":  plan.TotalAmount,
					})
				}
				return result
			}(),
			"global_min":        getMinTopup(),
			"global_max":        0,
			"recharge_fee_rate": 0,
			"methods":           methods,
		},
	}
}

func ClawXBillingCheckoutInfo(c *gin.Context) {
	user, err := model.GetUserById(c.GetInt("id"), false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, clawXTopupOverviewPayload(user))
}

func ClawXBillingOrderHistory(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	topUps, total, err := model.GetUserTopUps(c.GetInt("id"), pageInfo)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	items := make([]gin.H, 0, len(topUps))
	for _, topUp := range topUps {
		if topUp == nil {
			continue
		}
		items = append(items, gin.H{
			"id":               topUp.Id,
			"trade_no":         topUp.TradeNo,
			"out_trade_no":     topUp.TradeNo,
			"order_type":       "balance",
			"amount":           topUp.Amount,
			"credit_quota":     topUp.Amount,
			"money":            strconv.FormatFloat(topUp.Money, 'f', 2, 64),
			"payment_method":   topUp.PaymentMethod,
			"payment_provider": topUp.PaymentProvider,
			"status":           topUp.Status,
			"create_time":      topUp.CreateTime,
			"complete_time":    topUp.CompleteTime,
		})
	}

	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(items)
	common.ApiSuccess(c, pageInfo)
}

func paymentURL(uri string, params map[string]string) string {
	if uri == "" {
		return ""
	}
	values := url.Values{}
	for key, value := range params {
		values.Set(key, value)
	}
	if encoded := values.Encode(); encoded != "" {
		return uri + "?" + encoded
	}
	return uri
}

func ClawXBillingCreateOrder(c *gin.Context) {
	if !requirePaymentCompliance(c) {
		return
	}
	var req clawXBillingOrderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	orderType := strings.TrimSpace(req.OrderType)
	if orderType == "" {
		orderType = "balance"
	}
	paymentType := strings.TrimSpace(req.PaymentType)
	if paymentType == "" {
		paymentType = strings.TrimSpace(req.EpayMethod)
	}
	if paymentType == "" {
		common.ApiErrorMsg(c, "缺少支付方式")
		return
	}
	if !isStandardPaymentMethodAvailable(paymentType) {
		common.ApiErrorMsg(c, "支付方式不存在")
		return
	}
	if isWxPayMethod(paymentType) && isWxPayTopUpEnabled() {
		if orderType == "subscription" {
			createClawXWxPaySubscriptionOrder(c, req)
		} else {
			createClawXWxPayBalanceOrder(c, req)
		}
		return
	}
	switch orderType {
	case "subscription":
		createClawXSubscriptionOrder(c, req, paymentType)
	default:
		createClawXBalanceOrder(c, req, paymentType)
	}
}

func createClawXBalanceOrder(c *gin.Context, req clawXBillingOrderRequest, paymentType string) {
	amount := req.Amount
	if amount <= 0 && req.Money > 0 {
		amount = int64(req.Money)
	}
	if amount < getMinTopup() {
		common.ApiErrorMsg(c, fmt.Sprintf("充值数量不能小于 %d", getMinTopup()))
		return
	}
	userId := c.GetInt("id")
	group, err := model.GetUserGroup(userId, true)
	if err != nil {
		common.ApiErrorMsg(c, "获取用户分组失败")
		return
	}
	payMoney := getPayMoney(amount, group)
	if payMoney < 0.01 {
		common.ApiErrorMsg(c, "充值金额过低")
		return
	}
	client := GetEpayClient()
	if client == nil {
		common.ApiErrorMsg(c, "当前管理员未配置支付信息")
		return
	}
	callBackAddress := service.GetCallbackAddress()
	returnUrl, _ := url.Parse(paymentReturnPath("/console/log"))
	notifyUrl, _ := url.Parse(callBackAddress + "/api/user/epay/notify")
	tradeNo := fmt.Sprintf("CLAWXUSR%dNO%s%d", userId, common.GetRandomString(6), time.Now().UnixNano())
	uri, params, err := client.Purchase(&epay.PurchaseArgs{
		Type:           paymentType,
		ServiceTradeNo: tradeNo,
		Name:           fmt.Sprintf("ClawX 余额充值 %d", amount),
		Money:          strconv.FormatFloat(payMoney, 'f', 2, 64),
		Device:         epay.PC,
		NotifyUrl:      notifyUrl,
		ReturnUrl:      returnUrl,
	})
	if err != nil {
		common.ApiErrorMsg(c, "拉起支付失败")
		return
	}
	topUp := &model.TopUp{
		UserId:          userId,
		Amount:          amount,
		Money:           payMoney,
		TradeNo:         tradeNo,
		PaymentMethod:   paymentType,
		PaymentProvider: model.PaymentProviderEpay,
		CreateTime:      time.Now().Unix(),
		Status:          common.TopUpStatusPending,
	}
	if err := topUp.Insert(); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("ClawX 创建余额订单失败 user_id=%d trade_no=%s error=%q", userId, tradeNo, err.Error()))
		common.ApiErrorMsg(c, "创建订单失败")
		return
	}
	payURL := paymentURL(uri, params)
	common.ApiSuccess(c, gin.H{
		"trade_no":      tradeNo,
		"out_trade_no":  tradeNo,
		"status":        "pending",
		"order_type":    "balance",
		"payment_type":  paymentType,
		"money":         strconv.FormatFloat(payMoney, 'f', 2, 64),
		"amount":        amount,
		"credit_quota":  amount,
		"checkout_url":  payURL,
		"pay_page_url":  payURL,
		"pay_url":       payURL,
		"provider_data": params,
	})
}

func createClawXSubscriptionOrder(c *gin.Context, req clawXBillingOrderRequest, paymentType string) {
	if req.PlanId <= 0 {
		common.ApiErrorMsg(c, "缺少套餐 ID")
		return
	}
	plan, err := model.GetSubscriptionPlanById(req.PlanId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if !plan.Enabled {
		common.ApiErrorMsg(c, "套餐未启用")
		return
	}
	if plan.PriceAmount < 0.01 {
		common.ApiErrorMsg(c, "套餐金额过低")
		return
	}
	userId := c.GetInt("id")
	if plan.MaxPurchasePerUser > 0 {
		count, err := model.CountUserSubscriptionsByPlan(userId, plan.Id)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		if count >= int64(plan.MaxPurchasePerUser) {
			common.ApiErrorMsg(c, "已达到该套餐购买上限")
			return
		}
	}
	client := GetEpayClient()
	if client == nil {
		common.ApiErrorMsg(c, "当前管理员未配置支付信息")
		return
	}
	callBackAddress := service.GetCallbackAddress()
	returnUrl, err := url.Parse(callBackAddress + "/api/subscription/epay/return")
	if err != nil {
		common.ApiErrorMsg(c, "回调地址配置错误")
		return
	}
	notifyUrl, err := url.Parse(callBackAddress + "/api/subscription/epay/notify")
	if err != nil {
		common.ApiErrorMsg(c, "回调地址配置错误")
		return
	}
	tradeNo := fmt.Sprintf("CLAWXSUBUSR%dNO%s%d", userId, common.GetRandomString(6), time.Now().UnixNano())
	order := &model.SubscriptionOrder{
		UserId:          userId,
		PlanId:          plan.Id,
		Money:           plan.PriceAmount,
		TradeNo:         tradeNo,
		PaymentMethod:   paymentType,
		PaymentProvider: model.PaymentProviderEpay,
		CreateTime:      time.Now().Unix(),
		Status:          common.TopUpStatusPending,
	}
	if err := order.Insert(); err != nil {
		common.ApiErrorMsg(c, "创建订单失败")
		return
	}
	uri, params, err := client.Purchase(&epay.PurchaseArgs{
		Type:           paymentType,
		ServiceTradeNo: tradeNo,
		Name:           fmt.Sprintf("ClawX 订阅 %s", plan.Title),
		Money:          strconv.FormatFloat(plan.PriceAmount, 'f', 2, 64),
		Device:         epay.PC,
		NotifyUrl:      notifyUrl,
		ReturnUrl:      returnUrl,
	})
	if err != nil {
		_ = model.ExpireSubscriptionOrder(tradeNo, model.PaymentProviderEpay)
		common.ApiErrorMsg(c, "拉起支付失败")
		return
	}
	payURL := paymentURL(uri, params)
	common.ApiSuccess(c, gin.H{
		"trade_no":      tradeNo,
		"out_trade_no":  tradeNo,
		"status":        "pending",
		"order_type":    "subscription",
		"payment_type":  paymentType,
		"plan_id":       plan.Id,
		"plan_name":     plan.Title,
		"money":         strconv.FormatFloat(plan.PriceAmount, 'f', 2, 64),
		"credit_quota":  plan.TotalAmount,
		"checkout_url":  payURL,
		"pay_page_url":  payURL,
		"pay_url":       payURL,
		"provider_data": params,
	})
}

func clawXPaymentStatus(status string) string {
	switch status {
	case common.TopUpStatusSuccess:
		return "success"
	case common.TopUpStatusPending:
		return "pending"
	case common.TopUpStatusCancelled:
		return "cancelled"
	case common.TopUpStatusExpired:
		return "expired"
	default:
		return "failed"
	}
}

func ClawXBillingVerifyOrder(c *gin.Context) {
	var req clawXBillingOrderVerifyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	tradeNo := strings.TrimSpace(req.OutTradeNo)
	if tradeNo == "" {
		tradeNo = strings.TrimSpace(req.TradeNo)
	}
	if tradeNo == "" {
		common.ApiErrorMsg(c, "缺少订单号")
		return
	}
	if topUp := model.GetTopUpByTradeNo(tradeNo); topUp != nil && topUp.UserId == c.GetInt("id") {
		common.ApiSuccess(c, gin.H{
			"trade_no":     tradeNo,
			"status":       clawXPaymentStatus(topUp.Status),
			"order_type":   "balance",
			"amount":       topUp.Amount,
			"money":        strconv.FormatFloat(topUp.Money, 'f', 2, 64),
			"credit_quota": topUp.Amount,
		})
		return
	}
	if order := model.GetSubscriptionOrderByTradeNo(tradeNo); order != nil && order.UserId == c.GetInt("id") {
		common.ApiSuccess(c, gin.H{
			"trade_no":   tradeNo,
			"status":     clawXPaymentStatus(order.Status),
			"order_type": "subscription",
			"plan_id":    order.PlanId,
			"money":      strconv.FormatFloat(order.Money, 'f', 2, 64),
		})
		return
	}
	common.ApiErrorMsg(c, "订单不存在")
}

func ClawXUpdateLatest(c *gin.Context) {
	channel := strings.TrimSpace(c.Query("channel"))
	if channel == "" {
		channel = "latest"
	}
	platform := strings.TrimSpace(c.Query("platform"))
	if platform == "" {
		platform = "mac"
	}
	if payload, ok, err := clawXLatestReleasePayload(channel, platform); err != nil {
		common.ApiError(c, err)
		return
	} else if ok {
		common.ApiSuccess(c, payload)
		return
	}
	common.ApiSuccess(c, gin.H{
		"channel":      channel,
		"version":      clawXEnv("CLAWX_UPDATE_VERSION", ""),
		"releaseDate":  clawXEnv("CLAWX_UPDATE_RELEASE_DATE", ""),
		"releaseNotes": clawXEnv("CLAWX_UPDATE_RELEASE_NOTES", ""),
		"feedUrl":      fmt.Sprintf("%s/api/clawx/updates/feed/%s", clawXOrigin(), url.PathEscape(channel)),
	})
}

func ClawXUpdateFeed(c *gin.Context) {
	channel := strings.TrimSpace(c.Param("channel"))
	file := strings.TrimPrefix(c.Param("file"), "/")
	if platform := clawXFeedPlatform(file); platform != "" {
		if feed, ok, err := clawXBuildUpdateFeedYAML(channel, platform); err != nil {
			common.ApiError(c, err)
			return
		} else if ok {
			c.Data(http.StatusOK, "text/yaml; charset=utf-8", []byte(feed))
			return
		}
	}
	base := strings.TrimRight(clawXEnv("CLAWX_UPDATE_BASE_URL", "https://oss.intelli-spectrum.com"), "/")
	if channel == "" || file == "" {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "update feed not found"})
		return
	}
	target := fmt.Sprintf("%s/%s/%s", base, url.PathEscape(channel), file)
	c.Redirect(http.StatusTemporaryRedirect, target)
}
