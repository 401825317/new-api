package controller

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"github.com/wechatpay-apiv3/wechatpay-go/core"
	"github.com/wechatpay-apiv3/wechatpay-go/core/auth/verifiers"
	"github.com/wechatpay-apiv3/wechatpay-go/core/notify"
	"github.com/wechatpay-apiv3/wechatpay-go/core/option"
	"github.com/wechatpay-apiv3/wechatpay-go/services/payments"
	"github.com/wechatpay-apiv3/wechatpay-go/services/payments/native"
	"github.com/wechatpay-apiv3/wechatpay-go/utils"
)

const (
	wxPayAPIv3KeyLength         = 32
	wxPayCurrency               = "CNY"
	wxPayEventTransactionOK     = "TRANSACTION.SUCCESS"
	wxPayTradeStateSuccess      = "SUCCESS"
	wxPayTradeStateClosed       = "CLOSED"
	wxPayTradeStateRevoked      = "REVOKED"
	wxPayPaymentMethod          = "wxpay"
	wxPayNotifySuccessCode      = "SUCCESS"
	wxPayNotifySuccessMessage   = "成功"
	wxPayNotifyFailCode         = "FAIL"
	wxPayNotifyFailMessage      = "失败"
	wxPayDefaultPaymentMethodCN = "微信支付"
	wxPayOutTradeNoMaxLength    = 32
	wxPayNativeOrderTTL         = 2 * time.Hour
)

type wxPayNotificationResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type wxPayVerifiedNotification struct {
	OutTradeNo    string
	TransactionID string
	TradeState    string
	PaidAmount    float64
	RawBody       string
}

type wxPayNativeOrderResult struct {
	TradeNo      string
	CodeURL      string
	PayMoney     float64
	CreditAmount int64
	ExpiresAt    int64
}

type wxPaySubscriptionOrderResult struct {
	TradeNo   string
	CodeURL   string
	ExpiresAt int64
}

type wxPayQueriedOrder struct {
	OutTradeNo     string
	TransactionID  string
	TradeState     string
	TradeStateDesc string
	PaidAmount     float64
	RawPayload     string
}

type wxPayOrderQueryRequest struct {
	TradeNo string `json:"trade_no"`
}

var queryWxPayNativeOrder = queryWxPayNativeOrderFromAPI
var startWxPayOrderSyncOnce sync.Once

func isWxPayCancelledTradeState(tradeState string) bool {
	switch strings.TrimSpace(strings.ToUpper(tradeState)) {
	case wxPayTradeStateClosed, wxPayTradeStateRevoked:
		return true
	default:
		return false
	}
}

func isWxPayMethod(paymentMethod string) bool {
	return strings.TrimSpace(paymentMethod) == wxPayPaymentMethod
}

func isStandardPaymentMethodAvailable(paymentMethod string) bool {
	paymentMethod = strings.TrimSpace(paymentMethod)
	if paymentMethod == "" {
		return false
	}
	if isWxPayMethod(paymentMethod) && isWxPayTopUpEnabled() {
		return true
	}
	return isEpayTopUpEnabled() && operation_setting.ContainsPayMethod(paymentMethod)
}

func clonePayMethod(method map[string]string) map[string]string {
	next := make(map[string]string, len(method))
	for key, value := range method {
		next[key] = value
	}
	return next
}

func findConfiguredPayMethod(methodType string) (map[string]string, bool) {
	for _, method := range operation_setting.PayMethods {
		if strings.TrimSpace(method["type"]) == methodType {
			return clonePayMethod(method), true
		}
	}
	return nil, false
}

func containsPayMethod(methods []map[string]string, methodType string) bool {
	for _, method := range methods {
		if strings.TrimSpace(method["type"]) == methodType {
			return true
		}
	}
	return false
}

func buildAvailableStandardPayMethods() []map[string]string {
	if !operation_setting.IsPaymentComplianceConfirmed() {
		return []map[string]string{}
	}

	methods := make([]map[string]string, 0, len(operation_setting.PayMethods)+1)
	if isEpayTopUpEnabled() {
		for _, method := range operation_setting.PayMethods {
			if strings.TrimSpace(method["type"]) == "" {
				continue
			}
			methods = append(methods, clonePayMethod(method))
		}
	}

	if isWxPayTopUpEnabled() && !containsPayMethod(methods, wxPayPaymentMethod) {
		if configured, ok := findConfiguredPayMethod(wxPayPaymentMethod); ok {
			methods = append(methods, configured)
		} else {
			methods = append(methods, map[string]string{
				"name":  wxPayDefaultPaymentMethodCN,
				"type":  wxPayPaymentMethod,
				"color": "rgba(var(--semi-green-5), 1)",
			})
		}
	}

	return methods
}

func formatWxPayPEM(key, keyType string) string {
	key = strings.TrimSpace(key)
	if strings.HasPrefix(key, "-----BEGIN") {
		return key
	}
	return fmt.Sprintf("-----BEGIN %s-----\n%s\n-----END %s-----", keyType, key, keyType)
}

func newWxPayClientAndNotifyHandler() (*core.Client, *notify.Handler, error) {
	apiV3Key := strings.TrimSpace(setting.WxPayAPIv3Key)
	mchID := strings.TrimSpace(setting.WxPayMchID)
	certSerial := strings.TrimSpace(setting.WxPayCertSerial)
	publicKeyID := strings.TrimSpace(setting.WxPayPublicKeyID)

	if len(apiV3Key) != wxPayAPIv3KeyLength {
		return nil, nil, fmt.Errorf("微信支付 APIv3 密钥长度必须为 %d 位", wxPayAPIv3KeyLength)
	}

	privateKey, err := utils.LoadPrivateKey(formatWxPayPEM(setting.WxPayPrivateKey, "PRIVATE KEY"))
	if err != nil {
		return nil, nil, fmt.Errorf("微信支付商户私钥无效: %w", err)
	}
	publicKey, err := utils.LoadPublicKey(formatWxPayPEM(setting.WxPayPublicKey, "PUBLIC KEY"))
	if err != nil {
		return nil, nil, fmt.Errorf("微信支付公钥无效: %w", err)
	}

	verifier := verifiers.NewSHA256WithRSAPubkeyVerifier(publicKeyID, *publicKey)
	client, err := core.NewClient(context.Background(),
		option.WithMerchantCredential(mchID, certSerial, privateKey),
		option.WithVerifier(verifier),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("初始化微信支付客户端失败: %w", err)
	}

	notifyHandler, err := notify.NewRSANotifyHandler(apiV3Key, verifier)
	if err != nil {
		return nil, nil, fmt.Errorf("初始化微信支付回调处理器失败: %w", err)
	}

	return client, notifyHandler, nil
}

func getWxPayNotifyURL() string {
	if notifyURL := strings.TrimSpace(setting.WxPayNotifyURL); notifyURL != "" {
		return notifyURL
	}
	return strings.TrimRight(service.GetCallbackAddress(), "/") + "/api/user/wxpay/notify"
}

func newWxPayTradeNo(prefix string, userID int) string {
	prefix = strings.ToUpper(strings.TrimSpace(prefix))
	if prefix == "" {
		prefix = "WXUSR"
	}

	suffix := strings.ToUpper(common.GetRandomString(6)) + strconv.FormatInt(time.Now().Unix(), 10)
	userPart := strconv.Itoa(userID)
	userBudget := wxPayOutTradeNoMaxLength - len(prefix) - len("NO") - len(suffix)
	if userBudget < 1 {
		userBudget = 1
	}
	if len(userPart) > userBudget {
		userPart = strings.ToUpper(strconv.FormatInt(int64(userID), 36))
	}
	if len(userPart) > userBudget {
		userPart = userPart[len(userPart)-userBudget:]
	}

	tradeNo := prefix + userPart + "NO" + suffix
	if len(tradeNo) > wxPayOutTradeNoMaxLength {
		return tradeNo[:wxPayOutTradeNoMaxLength]
	}
	return tradeNo
}

func yuanToFen(value float64) int64 {
	return decimal.NewFromFloat(value).Mul(decimal.NewFromInt(100)).Round(0).IntPart()
}

func createWxPayNativePayment(ctx context.Context, tradeNo string, subject string, payMoney float64) (string, int64, error) {
	client, _, err := newWxPayClientAndNotifyHandler()
	if err != nil {
		return "", 0, err
	}

	totalFen := yuanToFen(payMoney)
	if totalFen <= 0 {
		return "", 0, fmt.Errorf("微信支付金额过低")
	}

	currency := wxPayCurrency
	expireAt := time.Now().Add(wxPayNativeOrderTTL)
	svc := native.NativeApiService{Client: client}
	resp, _, err := svc.Prepay(ctx, native.PrepayRequest{
		Appid:       core.String(strings.TrimSpace(setting.WxPayAppID)),
		Mchid:       core.String(strings.TrimSpace(setting.WxPayMchID)),
		Description: core.String(subject),
		OutTradeNo:  core.String(tradeNo),
		TimeExpire:  &expireAt,
		NotifyUrl:   core.String(getWxPayNotifyURL()),
		Amount: &native.Amount{
			Total:    core.Int64(totalFen),
			Currency: &currency,
		},
	})
	if err != nil {
		return "", 0, fmt.Errorf("微信支付预下单失败: %w", err)
	}
	if resp == nil || resp.CodeUrl == nil || strings.TrimSpace(*resp.CodeUrl) == "" {
		return "", 0, fmt.Errorf("微信支付未返回二维码")
	}
	return strings.TrimSpace(*resp.CodeUrl), expireAt.Unix(), nil
}

func normalizeTopUpAmountForStorage(amount int64) int64 {
	if operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens {
		return decimal.NewFromInt(amount).Div(decimal.NewFromFloat(common.QuotaPerUnit)).IntPart()
	}
	return amount
}

func createWxPayBalanceOrder(c *gin.Context, userID int, amount int64, subject string, tradeNo string) (*wxPayNativeOrderResult, error) {
	group, err := model.GetUserGroup(userID, true)
	if err != nil {
		return nil, fmt.Errorf("获取用户分组失败")
	}
	payMoney := getPayMoney(amount, group)
	if payMoney < 0.01 {
		return nil, fmt.Errorf("充值金额过低")
	}

	creditAmount := normalizeTopUpAmountForStorage(amount)
	topUp := &model.TopUp{
		UserId:          userID,
		Amount:          creditAmount,
		Money:           payMoney,
		TradeNo:         tradeNo,
		PaymentMethod:   wxPayPaymentMethod,
		PaymentProvider: model.PaymentProviderWxPay,
		CreateTime:      time.Now().Unix(),
		Status:          common.TopUpStatusPending,
	}
	if err := topUp.Insert(); err != nil {
		return nil, fmt.Errorf("创建订单失败")
	}

	codeURL, expiresAt, err := createWxPayNativePayment(c.Request.Context(), tradeNo, subject, payMoney)
	if err != nil {
		_ = model.UpdatePendingTopUpStatus(tradeNo, model.PaymentProviderWxPay, common.TopUpStatusFailed)
		return nil, err
	}

	return &wxPayNativeOrderResult{
		TradeNo:      tradeNo,
		CodeURL:      codeURL,
		PayMoney:     payMoney,
		CreditAmount: creditAmount,
		ExpiresAt:    expiresAt,
	}, nil
}

func requestWxPayBalanceOrder(c *gin.Context, req EpayRequest) {
	if req.Amount < getMinTopup() {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": fmt.Sprintf("充值数量不能小于 %d", getMinTopup())})
		return
	}
	if !isWxPayTopUpEnabled() {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "当前管理员未配置微信支付信息"})
		return
	}

	userID := c.GetInt("id")
	tradeNo := newWxPayTradeNo("WXUSR", userID)
	result, err := createWxPayBalanceOrder(c, userID, req.Amount, fmt.Sprintf("TUC%d", req.Amount), tradeNo)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("微信支付 创建充值订单失败 user_id=%d trade_no=%s amount=%d error=%q", userID, tradeNo, req.Amount, err.Error()))
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": err.Error()})
		return
	}

	logger.LogInfo(c.Request.Context(), fmt.Sprintf("微信支付 充值订单创建成功 user_id=%d trade_no=%s amount=%d money=%.2f", userID, result.TradeNo, req.Amount, result.PayMoney))
	c.JSON(http.StatusOK, gin.H{
		"message": "success",
		"data": gin.H{
			"trade_no":     result.TradeNo,
			"out_trade_no": result.TradeNo,
			"qr_code":      result.CodeURL,
			"code_url":     result.CodeURL,
			"payment_type": wxPayPaymentMethod,
			"money":        fmt.Sprintf("%.2f", result.PayMoney),
			"expires_at":   result.ExpiresAt,
		},
	})
}

func createClawXWxPayBalanceOrder(c *gin.Context, req clawXBillingOrderRequest) {
	amount := req.Amount
	if amount <= 0 && req.Money > 0 {
		amount = int64(req.Money)
	}
	if amount < getMinTopup() {
		common.ApiErrorMsg(c, fmt.Sprintf("充值数量不能小于 %d", getMinTopup()))
		return
	}
	if !isWxPayTopUpEnabled() {
		common.ApiErrorMsg(c, "当前管理员未配置微信支付信息")
		return
	}

	userID := c.GetInt("id")
	tradeNo := newWxPayTradeNo("WXCUSR", userID)
	result, err := createWxPayBalanceOrder(c, userID, amount, fmt.Sprintf("ClawX 余额充值 %d", amount), tradeNo)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("ClawX 微信支付创建余额订单失败 user_id=%d trade_no=%s error=%q", userID, tradeNo, err.Error()))
		common.ApiErrorMsg(c, err.Error())
		return
	}

	common.ApiSuccess(c, gin.H{
		"trade_no":      result.TradeNo,
		"out_trade_no":  result.TradeNo,
		"status":        "pending",
		"order_type":    "balance",
		"payment_type":  wxPayPaymentMethod,
		"money":         fmt.Sprintf("%.2f", result.PayMoney),
		"amount":        result.CreditAmount,
		"credit_quota":  result.CreditAmount,
		"qr_code":       result.CodeURL,
		"code_url":      result.CodeURL,
		"expires_at":    result.ExpiresAt,
		"provider_data": gin.H{"code_url": result.CodeURL},
	})
}

func createWxPaySubscriptionOrder(c *gin.Context, plan *model.SubscriptionPlan, userID int) {
	if !isWxPayTopUpEnabled() {
		common.ApiErrorMsg(c, "当前管理员未配置微信支付信息")
		return
	}

	tradeNo := newWxPayTradeNo("WXSUBUSR", userID)
	result, err := createWxPaySubscriptionNativeOrder(c, plan, userID, tradeNo, fmt.Sprintf("SUB:%s", plan.Title))
	if err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}

	common.ApiSuccess(c, gin.H{
		"trade_no":     result.TradeNo,
		"out_trade_no": result.TradeNo,
		"qr_code":      result.CodeURL,
		"code_url":     result.CodeURL,
		"payment_type": wxPayPaymentMethod,
		"expires_at":   result.ExpiresAt,
	})
}

func createWxPaySubscriptionNativeOrder(c *gin.Context, plan *model.SubscriptionPlan, userID int, tradeNo string, subject string) (*wxPaySubscriptionOrderResult, error) {
	order := &model.SubscriptionOrder{
		UserId:          userID,
		PlanId:          plan.Id,
		Money:           plan.PriceAmount,
		TradeNo:         tradeNo,
		PaymentMethod:   wxPayPaymentMethod,
		PaymentProvider: model.PaymentProviderWxPay,
		CreateTime:      time.Now().Unix(),
		Status:          common.TopUpStatusPending,
	}
	if err := order.Insert(); err != nil {
		return nil, fmt.Errorf("创建订单失败")
	}

	codeURL, expiresAt, err := createWxPayNativePayment(c.Request.Context(), tradeNo, subject, plan.PriceAmount)
	if err != nil {
		_ = model.ExpireSubscriptionOrder(tradeNo, model.PaymentProviderWxPay)
		return nil, err
	}

	return &wxPaySubscriptionOrderResult{
		TradeNo:   tradeNo,
		CodeURL:   codeURL,
		ExpiresAt: expiresAt,
	}, nil
}

func createClawXWxPaySubscriptionOrder(c *gin.Context, req clawXBillingOrderRequest) {
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
	if !isWxPayTopUpEnabled() {
		common.ApiErrorMsg(c, "当前管理员未配置微信支付信息")
		return
	}

	userID := c.GetInt("id")
	if plan.MaxPurchasePerUser > 0 {
		count, err := model.CountUserSubscriptionsByPlan(userID, plan.Id)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		if count >= int64(plan.MaxPurchasePerUser) {
			common.ApiErrorMsg(c, "已达到该套餐购买上限")
			return
		}
	}

	tradeNo := newWxPayTradeNo("WXCSUBUSR", userID)
	result, err := createWxPaySubscriptionNativeOrder(c, plan, userID, tradeNo, fmt.Sprintf("ClawX 订阅 %s", plan.Title))
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("ClawX 微信支付创建订阅订单失败 user_id=%d trade_no=%s error=%q", userID, tradeNo, err.Error()))
		common.ApiErrorMsg(c, err.Error())
		return
	}

	common.ApiSuccess(c, gin.H{
		"trade_no":      result.TradeNo,
		"out_trade_no":  result.TradeNo,
		"status":        "pending",
		"order_type":    "subscription",
		"payment_type":  wxPayPaymentMethod,
		"plan_id":       plan.Id,
		"plan_name":     plan.Title,
		"money":         fmt.Sprintf("%.2f", plan.PriceAmount),
		"credit_quota":  plan.TotalAmount,
		"qr_code":       result.CodeURL,
		"code_url":      result.CodeURL,
		"expires_at":    result.ExpiresAt,
		"provider_data": gin.H{"code_url": result.CodeURL},
	})
}

func parseWxPayNotification(c *gin.Context) (*wxPayVerifiedNotification, error) {
	_, notifyHandler, err := newWxPayClientAndNotifyHandler()
	if err != nil {
		return nil, err
	}

	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return nil, fmt.Errorf("读取微信支付回调失败: %w", err)
	}
	c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, "/", io.NopCloser(bytes.NewBuffer(bodyBytes)))
	if err != nil {
		return nil, fmt.Errorf("构造微信支付回调请求失败: %w", err)
	}
	req.Header = c.Request.Header.Clone()

	var tx payments.Transaction
	notifyReq, err := notifyHandler.ParseNotifyRequest(c.Request.Context(), req, &tx)
	if err != nil {
		return nil, fmt.Errorf("微信支付回调验签失败: %w", err)
	}
	if notifyReq == nil || notifyReq.EventType != wxPayEventTransactionOK {
		return nil, nil
	}

	return &wxPayVerifiedNotification{
		OutTradeNo:    strings.TrimSpace(stringValue(tx.OutTradeNo)),
		TransactionID: strings.TrimSpace(stringValue(tx.TransactionId)),
		TradeState:    strings.TrimSpace(stringValue(tx.TradeState)),
		PaidAmount:    paidAmountFromWxPayTransaction(&tx),
		RawBody:       string(bodyBytes),
	}, nil
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func wxPayNotifyOK(c *gin.Context) {
	c.JSON(http.StatusOK, wxPayNotificationResponse{Code: wxPayNotifySuccessCode, Message: wxPayNotifySuccessMessage})
}

func wxPayNotifyFail(c *gin.Context, message string) {
	if strings.TrimSpace(message) == "" {
		message = wxPayNotifyFailMessage
	}
	c.JSON(http.StatusInternalServerError, wxPayNotificationResponse{Code: wxPayNotifyFailCode, Message: message})
}

func amountCoversExpected(paidAmount float64, expectedAmount float64) bool {
	return decimal.NewFromFloat(paidAmount).Add(decimal.NewFromFloat(0.000001)).GreaterThanOrEqual(decimal.NewFromFloat(expectedAmount))
}

func paidAmountFromWxPayTransaction(tx *payments.Transaction) float64 {
	if tx == nil || tx.Amount == nil {
		return 0
	}
	if tx.Amount.Total != nil {
		return decimal.NewFromInt(*tx.Amount.Total).Div(decimal.NewFromInt(100)).InexactFloat64()
	}
	if tx.Amount.PayerTotal != nil {
		return decimal.NewFromInt(*tx.Amount.PayerTotal).Div(decimal.NewFromInt(100)).InexactFloat64()
	}
	return 0
}

func queryWxPayNativeOrderFromAPI(ctx context.Context, tradeNo string) (*wxPayQueriedOrder, error) {
	tradeNo = strings.TrimSpace(tradeNo)
	if tradeNo == "" {
		return nil, fmt.Errorf("缺少订单号")
	}
	client, _, err := newWxPayClientAndNotifyHandler()
	if err != nil {
		return nil, err
	}

	svc := native.NativeApiService{Client: client}
	tx, _, err := svc.QueryOrderByOutTradeNo(ctx, native.QueryOrderByOutTradeNoRequest{
		OutTradeNo: core.String(tradeNo),
		Mchid:      core.String(strings.TrimSpace(setting.WxPayMchID)),
	})
	if err != nil {
		return nil, fmt.Errorf("微信支付查单失败: %w", err)
	}
	if tx == nil {
		return nil, fmt.Errorf("微信支付查单未返回订单")
	}

	return &wxPayQueriedOrder{
		OutTradeNo:     strings.TrimSpace(stringValue(tx.OutTradeNo)),
		TransactionID:  strings.TrimSpace(stringValue(tx.TransactionId)),
		TradeState:     strings.TrimSpace(stringValue(tx.TradeState)),
		TradeStateDesc: strings.TrimSpace(stringValue(tx.TradeStateDesc)),
		PaidAmount:     paidAmountFromWxPayTransaction(tx),
		RawPayload:     common.GetJsonString(tx),
	}, nil
}

func wxPayQueryResponse(orderType string, tradeNo string, localStatus string, queried *wxPayQueriedOrder) gin.H {
	data := gin.H{
		"order_type":   orderType,
		"trade_no":     tradeNo,
		"out_trade_no": tradeNo,
		"local_status": localStatus,
		"paid":         localStatus == common.TopUpStatusSuccess,
	}
	if queried != nil {
		data["trade_state"] = queried.TradeState
		data["trade_state_desc"] = queried.TradeStateDesc
		data["transaction_id"] = queried.TransactionID
		data["paid_amount"] = fmt.Sprintf("%.2f", queried.PaidAmount)
		data["paid"] = queried.TradeState == wxPayTradeStateSuccess || localStatus == common.TopUpStatusSuccess
	}
	return data
}

func settleWxPayTopUpFromQuery(ctx context.Context, topUp *model.TopUp, callerIp string) (*wxPayQueriedOrder, string, error) {
	if topUp == nil {
		return nil, "", fmt.Errorf("充值订单不存在")
	}
	if topUp.PaymentProvider != model.PaymentProviderWxPay {
		return nil, topUp.Status, fmt.Errorf("非微信支付订单")
	}
	if topUp.Status == common.TopUpStatusSuccess {
		return nil, common.TopUpStatusSuccess, nil
	}
	if topUp.Status != common.TopUpStatusPending {
		return nil, topUp.Status, nil
	}

	queried, err := queryWxPayNativeOrder(ctx, topUp.TradeNo)
	if err != nil {
		return nil, topUp.Status, err
	}
	if queried.OutTradeNo != "" && queried.OutTradeNo != topUp.TradeNo {
		return queried, topUp.Status, fmt.Errorf("微信支付返回订单号不匹配")
	}
	if queried.TradeState != wxPayTradeStateSuccess {
		if isWxPayCancelledTradeState(queried.TradeState) {
			if err := model.UpdatePendingTopUpStatus(topUp.TradeNo, model.PaymentProviderWxPay, common.TopUpStatusCancelled); err != nil {
				return queried, topUp.Status, err
			}
			return queried, common.TopUpStatusCancelled, nil
		}
		return queried, topUp.Status, nil
	}
	if !amountCoversExpected(queried.PaidAmount, topUp.Money) {
		return queried, topUp.Status, fmt.Errorf("微信支付金额不足")
	}

	LockOrder(topUp.TradeNo)
	defer UnlockOrder(topUp.TradeNo)
	if err := model.RechargeWxPay(topUp.TradeNo, queried.PaidAmount, callerIp); err != nil {
		return queried, topUp.Status, err
	}
	return queried, common.TopUpStatusSuccess, nil
}

func settleWxPaySubscriptionFromQuery(ctx context.Context, order *model.SubscriptionOrder, callerIp string) (*wxPayQueriedOrder, string, error) {
	if order == nil {
		return nil, "", fmt.Errorf("订阅订单不存在")
	}
	if order.PaymentProvider != model.PaymentProviderWxPay {
		return nil, order.Status, fmt.Errorf("非微信支付订单")
	}
	if order.Status == common.TopUpStatusSuccess {
		return nil, common.TopUpStatusSuccess, nil
	}
	if order.Status != common.TopUpStatusPending {
		return nil, order.Status, nil
	}

	queried, err := queryWxPayNativeOrder(ctx, order.TradeNo)
	if err != nil {
		return nil, order.Status, err
	}
	if queried.OutTradeNo != "" && queried.OutTradeNo != order.TradeNo {
		return queried, order.Status, fmt.Errorf("微信支付返回订单号不匹配")
	}
	if queried.TradeState != wxPayTradeStateSuccess {
		if isWxPayCancelledTradeState(queried.TradeState) {
			if err := model.UpdatePendingSubscriptionOrderStatus(order.TradeNo, model.PaymentProviderWxPay, common.TopUpStatusCancelled); err != nil {
				return queried, order.Status, err
			}
			return queried, common.TopUpStatusCancelled, nil
		}
		return queried, order.Status, nil
	}
	if !amountCoversExpected(queried.PaidAmount, order.Money) {
		return queried, order.Status, fmt.Errorf("微信支付金额不足")
	}

	LockOrder(order.TradeNo)
	defer UnlockOrder(order.TradeNo)
	if err := model.CompleteSubscriptionOrder(order.TradeNo, queried.RawPayload, model.PaymentProviderWxPay, wxPayPaymentMethod); err != nil {
		return queried, order.Status, err
	}
	logger.LogInfo(ctx, fmt.Sprintf("微信支付订阅查单补单成功 trade_no=%s transaction_id=%s paid=%.2f client_ip=%s", order.TradeNo, queried.TransactionID, queried.PaidAmount, callerIp))
	return queried, common.TopUpStatusSuccess, nil
}

func QueryWxPayOrder(c *gin.Context) {
	var req wxPayOrderQueryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	tradeNo := strings.TrimSpace(req.TradeNo)
	if tradeNo == "" {
		common.ApiErrorMsg(c, "缺少订单号")
		return
	}

	userID := c.GetInt("id")
	if topUp := model.GetTopUpByTradeNo(tradeNo); topUp != nil {
		if topUp.UserId != userID {
			common.ApiErrorMsg(c, "订单不存在")
			return
		}
		queried, localStatus, err := settleWxPayTopUpFromQuery(c.Request.Context(), topUp, c.ClientIP())
		if err != nil {
			common.ApiErrorMsg(c, err.Error())
			return
		}
		common.ApiSuccess(c, wxPayQueryResponse("topup", tradeNo, localStatus, queried))
		return
	}

	if order := model.GetSubscriptionOrderByTradeNo(tradeNo); order != nil {
		if order.UserId != userID {
			common.ApiErrorMsg(c, "订单不存在")
			return
		}
		queried, localStatus, err := settleWxPaySubscriptionFromQuery(c.Request.Context(), order, c.ClientIP())
		if err != nil {
			common.ApiErrorMsg(c, err.Error())
			return
		}
		common.ApiSuccess(c, wxPayQueryResponse("subscription", tradeNo, localStatus, queried))
		return
	}

	common.ApiErrorMsg(c, "订单不存在")
}

func syncPendingWxPayOrders(ctx context.Context) {
	if !isWxPayTopUpEnabled() {
		return
	}
	limit := common.GetEnvOrDefault("WXPAY_ORDER_SYNC_BATCH", 20)
	if limit <= 0 {
		limit = 20
	}
	lookbackMinutes := common.GetEnvOrDefault("WXPAY_ORDER_SYNC_LOOKBACK_MINUTES", 130)
	if lookbackMinutes <= 0 {
		lookbackMinutes = 130
	}
	cutoff := time.Now().Add(-time.Duration(lookbackMinutes) * time.Minute).Unix()

	var topUps []model.TopUp
	if err := model.DB.Where("payment_provider = ? AND status = ? AND create_time >= ?", model.PaymentProviderWxPay, common.TopUpStatusPending, cutoff).
		Order("create_time asc").Limit(limit).Find(&topUps).Error; err != nil {
		logger.LogError(ctx, fmt.Sprintf("微信支付充值查单任务读取失败 error=%q", err.Error()))
	} else {
		for i := range topUps {
			queried, localStatus, err := settleWxPayTopUpFromQuery(ctx, &topUps[i], "wxpay-sync-task")
			if err != nil {
				logger.LogWarn(ctx, fmt.Sprintf("微信支付充值查单失败 trade_no=%s error=%q", topUps[i].TradeNo, err.Error()))
				continue
			}
			if localStatus == common.TopUpStatusSuccess && queried != nil {
				logger.LogInfo(ctx, fmt.Sprintf("微信支付充值查单补单成功 trade_no=%s transaction_id=%s paid=%.2f", topUps[i].TradeNo, queried.TransactionID, queried.PaidAmount))
			}
		}
	}

	var orders []model.SubscriptionOrder
	if err := model.DB.Where("payment_provider = ? AND status = ? AND create_time >= ?", model.PaymentProviderWxPay, common.TopUpStatusPending, cutoff).
		Order("create_time asc").Limit(limit).Find(&orders).Error; err != nil {
		logger.LogError(ctx, fmt.Sprintf("微信支付订阅查单任务读取失败 error=%q", err.Error()))
		return
	}
	for i := range orders {
		_, _, err := settleWxPaySubscriptionFromQuery(ctx, &orders[i], "wxpay-sync-task")
		if err != nil {
			logger.LogWarn(ctx, fmt.Sprintf("微信支付订阅查单失败 trade_no=%s error=%q", orders[i].TradeNo, err.Error()))
		}
	}
}

func StartWxPayOrderSyncTask() {
	startWxPayOrderSyncOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}
		intervalSeconds := common.GetEnvOrDefault("WXPAY_ORDER_SYNC_INTERVAL", 30)
		if intervalSeconds <= 0 {
			return
		}
		interval := time.Duration(intervalSeconds) * time.Second
		go func() {
			ctx := context.Background()
			logger.LogInfo(ctx, fmt.Sprintf("wxpay order sync task started: tick=%s", interval))
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for range ticker.C {
				syncPendingWxPayOrders(ctx)
			}
		}()
	})
}

func WxPayNotify(c *gin.Context) {
	if !isWxPayWebhookEnabled() {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("微信支付 webhook 被拒绝 reason=webhook_disabled path=%q client_ip=%s", c.Request.RequestURI, c.ClientIP()))
		wxPayNotifyFail(c, "webhook disabled")
		return
	}

	notification, err := parseWxPayNotification(c)
	if err != nil {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("微信支付 webhook 验签失败 path=%q client_ip=%s error=%q", c.Request.RequestURI, c.ClientIP(), err.Error()))
		wxPayNotifyFail(c, "verify failed")
		return
	}
	if notification == nil {
		wxPayNotifyOK(c)
		return
	}
	if notification.OutTradeNo == "" {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("微信支付 webhook 缺少订单号 transaction_id=%s client_ip=%s", notification.TransactionID, c.ClientIP()))
		wxPayNotifyFail(c, "missing out_trade_no")
		return
	}
	if notification.TradeState != wxPayTradeStateSuccess {
		logger.LogInfo(c.Request.Context(), fmt.Sprintf("微信支付 webhook 忽略非成功状态 trade_no=%s trade_state=%s transaction_id=%s client_ip=%s", notification.OutTradeNo, notification.TradeState, notification.TransactionID, c.ClientIP()))
		wxPayNotifyOK(c)
		return
	}

	LockOrder(notification.OutTradeNo)
	defer UnlockOrder(notification.OutTradeNo)

	if topUp := model.GetTopUpByTradeNo(notification.OutTradeNo); topUp != nil {
		if !amountCoversExpected(notification.PaidAmount, topUp.Money) {
			logger.LogWarn(c.Request.Context(), fmt.Sprintf("微信支付 webhook 金额不足 trade_no=%s paid=%.2f expected=%.2f client_ip=%s", notification.OutTradeNo, notification.PaidAmount, topUp.Money, c.ClientIP()))
			wxPayNotifyFail(c, "amount mismatch")
			return
		}
		if err := model.RechargeWxPay(notification.OutTradeNo, notification.PaidAmount, c.ClientIP()); err != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("微信支付 充值处理失败 trade_no=%s transaction_id=%s client_ip=%s error=%q", notification.OutTradeNo, notification.TransactionID, c.ClientIP(), err.Error()))
			wxPayNotifyFail(c, "recharge failed")
			return
		}
		logger.LogInfo(c.Request.Context(), fmt.Sprintf("微信支付 充值成功 trade_no=%s transaction_id=%s paid=%.2f client_ip=%s", notification.OutTradeNo, notification.TransactionID, notification.PaidAmount, c.ClientIP()))
		wxPayNotifyOK(c)
		return
	}

	if order := model.GetSubscriptionOrderByTradeNo(notification.OutTradeNo); order != nil {
		if !amountCoversExpected(notification.PaidAmount, order.Money) {
			logger.LogWarn(c.Request.Context(), fmt.Sprintf("微信支付订阅 webhook 金额不足 trade_no=%s paid=%.2f expected=%.2f client_ip=%s", notification.OutTradeNo, notification.PaidAmount, order.Money, c.ClientIP()))
			wxPayNotifyFail(c, "amount mismatch")
			return
		}
		if err := model.CompleteSubscriptionOrder(notification.OutTradeNo, notification.RawBody, model.PaymentProviderWxPay, wxPayPaymentMethod); err != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("微信支付订阅处理失败 trade_no=%s transaction_id=%s client_ip=%s error=%q", notification.OutTradeNo, notification.TransactionID, c.ClientIP(), err.Error()))
			wxPayNotifyFail(c, "subscription failed")
			return
		}
		logger.LogInfo(c.Request.Context(), fmt.Sprintf("微信支付订阅成功 trade_no=%s transaction_id=%s paid=%.2f client_ip=%s", notification.OutTradeNo, notification.TransactionID, notification.PaidAmount, c.ClientIP()))
		wxPayNotifyOK(c)
		return
	}

	logger.LogWarn(c.Request.Context(), fmt.Sprintf("微信支付 webhook 订单不存在 trade_no=%s transaction_id=%s client_ip=%s", notification.OutTradeNo, notification.TransactionID, c.ClientIP()))
	wxPayNotifyOK(c)
}
