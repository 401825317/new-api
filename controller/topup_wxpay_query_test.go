package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type wxPayQueryHTTPResponse struct {
	Success bool                   `json:"success"`
	Message string                 `json:"message"`
	Data    map[string]interface{} `json:"data"`
}

func setupWxPayQueryControllerTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	gin.SetMode(gin.TestMode)
	originalQuotaPerUnit := common.QuotaPerUnit
	originalUsingSQLite := common.UsingSQLite
	originalUsingMySQL := common.UsingMySQL
	originalUsingPostgreSQL := common.UsingPostgreSQL
	originalRedisEnabled := common.RedisEnabled
	t.Cleanup(func() {
		common.QuotaPerUnit = originalQuotaPerUnit
		common.UsingSQLite = originalUsingSQLite
		common.UsingMySQL = originalUsingMySQL
		common.UsingPostgreSQL = originalUsingPostgreSQL
		common.RedisEnabled = originalRedisEnabled
	})

	common.QuotaPerUnit = 500000
	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	common.RedisEnabled = false

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	model.LOG_DB = db

	require.NoError(t, db.AutoMigrate(
		&model.User{},
		&model.Log{},
		&model.TopUp{},
		&model.SubscriptionPlan{},
		&model.SubscriptionOrder{},
		&model.UserSubscription{},
	))

	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})

	return db
}

func stubWxPayQuery(t *testing.T, fn func(context.Context, string) (*wxPayQueriedOrder, error)) {
	t.Helper()
	original := queryWxPayNativeOrder
	queryWxPayNativeOrder = fn
	t.Cleanup(func() {
		queryWxPayNativeOrder = original
	})
}

func performWxPayQueryRequest(t *testing.T, userID int, tradeNo string) wxPayQueryHTTPResponse {
	t.Helper()

	body, err := json.Marshal(wxPayOrderQueryRequest{TradeNo: tradeNo})
	require.NoError(t, err)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/api/user/wxpay/query", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("id", userID)

	QueryWxPayOrder(c)

	var response wxPayQueryHTTPResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	return response
}

func insertWxPayQueryUser(t *testing.T, userID int) {
	t.Helper()
	require.NoError(t, model.DB.Create(&model.User{
		Id:       userID,
		Username: fmt.Sprintf("wxpay_query_user_%d", userID),
		Status:   common.UserStatusEnabled,
		Group:    "default",
		AffCode:  fmt.Sprintf("wxpay-query-aff-%d", userID),
	}).Error)
}

func TestQueryWxPayOrderCompletesPendingTopUp(t *testing.T) {
	setupWxPayQueryControllerTestDB(t)
	insertWxPayQueryUser(t, 1)

	const tradeNo = "WXUSR1NOQUERY123"
	require.NoError(t, (&model.TopUp{
		UserId:          1,
		Amount:          2,
		Money:           0.02,
		TradeNo:         tradeNo,
		PaymentMethod:   wxPayPaymentMethod,
		PaymentProvider: model.PaymentProviderWxPay,
		CreateTime:      time.Now().Unix(),
		Status:          common.TopUpStatusPending,
	}).Insert())

	stubWxPayQuery(t, func(_ context.Context, gotTradeNo string) (*wxPayQueriedOrder, error) {
		require.Equal(t, tradeNo, gotTradeNo)
		return &wxPayQueriedOrder{
			OutTradeNo:    tradeNo,
			TransactionID: "4200000000000000001",
			TradeState:    wxPayTradeStateSuccess,
			PaidAmount:    0.02,
			RawPayload:    `{"trade_state":"SUCCESS"}`,
		}, nil
	})

	response := performWxPayQueryRequest(t, 1, tradeNo)
	require.True(t, response.Success, response.Message)
	assert.Equal(t, "topup", response.Data["order_type"])
	assert.Equal(t, common.TopUpStatusSuccess, response.Data["local_status"])
	assert.Equal(t, true, response.Data["paid"])

	topUp := model.GetTopUpByTradeNo(tradeNo)
	require.NotNil(t, topUp)
	assert.Equal(t, common.TopUpStatusSuccess, topUp.Status)

	var user model.User
	require.NoError(t, model.DB.First(&user, 1).Error)
	assert.Equal(t, 1000000, user.Quota)
}

func TestQueryWxPayOrderCancelsClosedPendingTopUp(t *testing.T) {
	setupWxPayQueryControllerTestDB(t)
	insertWxPayQueryUser(t, 1)

	const tradeNo = "WXUSR1NOCLOSED123"
	require.NoError(t, (&model.TopUp{
		UserId:          1,
		Amount:          2,
		Money:           0.02,
		TradeNo:         tradeNo,
		PaymentMethod:   wxPayPaymentMethod,
		PaymentProvider: model.PaymentProviderWxPay,
		CreateTime:      time.Now().Unix(),
		Status:          common.TopUpStatusPending,
	}).Insert())

	stubWxPayQuery(t, func(_ context.Context, gotTradeNo string) (*wxPayQueriedOrder, error) {
		require.Equal(t, tradeNo, gotTradeNo)
		return &wxPayQueriedOrder{
			OutTradeNo:     tradeNo,
			TradeState:     wxPayTradeStateClosed,
			TradeStateDesc: "订单已关闭",
			RawPayload:     `{"trade_state":"CLOSED"}`,
		}, nil
	})

	response := performWxPayQueryRequest(t, 1, tradeNo)
	require.True(t, response.Success, response.Message)
	assert.Equal(t, "topup", response.Data["order_type"])
	assert.Equal(t, common.TopUpStatusCancelled, response.Data["local_status"])
	assert.Equal(t, false, response.Data["paid"])

	topUp := model.GetTopUpByTradeNo(tradeNo)
	require.NotNil(t, topUp)
	assert.Equal(t, common.TopUpStatusCancelled, topUp.Status)
	assert.Zero(t, topUp.CompleteTime)

	var user model.User
	require.NoError(t, model.DB.First(&user, 1).Error)
	assert.Equal(t, 0, user.Quota)
}

func TestQueryWxPayOrderRejectsOtherUsersOrderWithoutRemoteQuery(t *testing.T) {
	setupWxPayQueryControllerTestDB(t)
	insertWxPayQueryUser(t, 1)
	insertWxPayQueryUser(t, 2)

	const tradeNo = "WXUSR2NOQUERY123"
	require.NoError(t, (&model.TopUp{
		UserId:          2,
		Amount:          1,
		Money:           0.01,
		TradeNo:         tradeNo,
		PaymentMethod:   wxPayPaymentMethod,
		PaymentProvider: model.PaymentProviderWxPay,
		CreateTime:      time.Now().Unix(),
		Status:          common.TopUpStatusPending,
	}).Insert())

	called := false
	stubWxPayQuery(t, func(_ context.Context, _ string) (*wxPayQueriedOrder, error) {
		called = true
		return nil, nil
	})

	response := performWxPayQueryRequest(t, 1, tradeNo)
	require.False(t, response.Success)
	assert.Equal(t, "订单不存在", response.Message)
	assert.False(t, called)
	assert.Equal(t, common.TopUpStatusPending, model.GetTopUpByTradeNo(tradeNo).Status)
}

func TestQueryWxPayOrderCompletesPendingSubscription(t *testing.T) {
	setupWxPayQueryControllerTestDB(t)
	insertWxPayQueryUser(t, 1)

	plan := &model.SubscriptionPlan{
		Id:                    11,
		Title:                 "WxPay Plan",
		PriceAmount:           9.99,
		Currency:              "CNY",
		DurationUnit:          model.SubscriptionDurationMonth,
		DurationValue:         1,
		QuotaResetPeriod:      model.SubscriptionResetNever,
		Enabled:               true,
		TotalAmount:           12345,
		MaxPurchasePerUser:    0,
		AllowBalancePay:       common.GetPointer(true),
		StripePriceId:         "",
		CreemProductId:        "",
		WaffoPancakeProductId: "",
	}
	require.NoError(t, model.DB.Create(plan).Error)

	const tradeNo = "WXSUBUSR1NOQUERY123"
	require.NoError(t, (&model.SubscriptionOrder{
		UserId:          1,
		PlanId:          plan.Id,
		Money:           plan.PriceAmount,
		TradeNo:         tradeNo,
		PaymentMethod:   wxPayPaymentMethod,
		PaymentProvider: model.PaymentProviderWxPay,
		CreateTime:      time.Now().Unix(),
		Status:          common.TopUpStatusPending,
	}).Insert())

	stubWxPayQuery(t, func(_ context.Context, gotTradeNo string) (*wxPayQueriedOrder, error) {
		require.Equal(t, tradeNo, gotTradeNo)
		return &wxPayQueriedOrder{
			OutTradeNo:    tradeNo,
			TransactionID: "4200000000000000002",
			TradeState:    wxPayTradeStateSuccess,
			PaidAmount:    9.99,
			RawPayload:    `{"trade_state":"SUCCESS"}`,
		}, nil
	})

	response := performWxPayQueryRequest(t, 1, tradeNo)
	require.True(t, response.Success, response.Message)
	assert.Equal(t, "subscription", response.Data["order_type"])
	assert.Equal(t, common.TopUpStatusSuccess, response.Data["local_status"])
	assert.Equal(t, true, response.Data["paid"])

	order := model.GetSubscriptionOrderByTradeNo(tradeNo)
	require.NotNil(t, order)
	assert.Equal(t, common.TopUpStatusSuccess, order.Status)

	var count int64
	require.NoError(t, model.DB.Model(&model.UserSubscription{}).Where("user_id = ?", 1).Count(&count).Error)
	assert.Equal(t, int64(1), count)
	assert.NotNil(t, model.GetTopUpByTradeNo(tradeNo))
}

func TestQueryWxPayOrderCancelsRevokedPendingSubscription(t *testing.T) {
	setupWxPayQueryControllerTestDB(t)
	insertWxPayQueryUser(t, 1)

	plan := &model.SubscriptionPlan{
		Id:                    12,
		Title:                 "WxPay Revoked Plan",
		PriceAmount:           9.99,
		Currency:              "CNY",
		DurationUnit:          model.SubscriptionDurationMonth,
		DurationValue:         1,
		QuotaResetPeriod:      model.SubscriptionResetNever,
		Enabled:               true,
		TotalAmount:           12345,
		MaxPurchasePerUser:    0,
		AllowBalancePay:       common.GetPointer(true),
		StripePriceId:         "",
		CreemProductId:        "",
		WaffoPancakeProductId: "",
	}
	require.NoError(t, model.DB.Create(plan).Error)

	const tradeNo = "WXSUBUSR1NOREVOKED123"
	require.NoError(t, (&model.SubscriptionOrder{
		UserId:          1,
		PlanId:          plan.Id,
		Money:           plan.PriceAmount,
		TradeNo:         tradeNo,
		PaymentMethod:   wxPayPaymentMethod,
		PaymentProvider: model.PaymentProviderWxPay,
		CreateTime:      time.Now().Unix(),
		Status:          common.TopUpStatusPending,
	}).Insert())

	stubWxPayQuery(t, func(_ context.Context, gotTradeNo string) (*wxPayQueriedOrder, error) {
		require.Equal(t, tradeNo, gotTradeNo)
		return &wxPayQueriedOrder{
			OutTradeNo:     tradeNo,
			TradeState:     wxPayTradeStateRevoked,
			TradeStateDesc: "订单已撤销",
			RawPayload:     `{"trade_state":"REVOKED"}`,
		}, nil
	})

	response := performWxPayQueryRequest(t, 1, tradeNo)
	require.True(t, response.Success, response.Message)
	assert.Equal(t, "subscription", response.Data["order_type"])
	assert.Equal(t, common.TopUpStatusCancelled, response.Data["local_status"])
	assert.Equal(t, false, response.Data["paid"])

	order := model.GetSubscriptionOrderByTradeNo(tradeNo)
	require.NotNil(t, order)
	assert.Equal(t, common.TopUpStatusCancelled, order.Status)
	assert.Zero(t, order.CompleteTime)

	var count int64
	require.NoError(t, model.DB.Model(&model.UserSubscription{}).Where("user_id = ?", 1).Count(&count).Error)
	assert.Equal(t, int64(0), count)
	assert.Nil(t, model.GetTopUpByTradeNo(tradeNo))
}
