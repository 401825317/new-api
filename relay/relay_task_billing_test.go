package relay

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fixedPreConsumedBilling struct {
	quota int
}

type taskBillingEventLog struct {
	mu     sync.Mutex
	events []string
}

func (l *taskBillingEventLog) add(event string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, event)
}

func (l *taskBillingEventLog) snapshot() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.events...)
}

type taskRetryPriceFixture struct {
	context *gin.Context
	info    *relaycommon.RelayInfo
	events  *taskBillingEventLog
}

func (b *fixedPreConsumedBilling) Settle(int) error         { return nil }
func (b *fixedPreConsumedBilling) Refund(*gin.Context)      {}
func (b *fixedPreConsumedBilling) NeedsRefund() bool        { return false }
func (b *fixedPreConsumedBilling) GetPreConsumedQuota() int { return b.quota }
func (b *fixedPreConsumedBilling) Reserve(int) error {
	panic("prepared task retry gate must not reserve quota")
}

func TestRecalcQuotaFromRatiosRejectsInvalidAdjustments(t *testing.T) {
	priceData := types.PriceData{Quota: 200}
	priceData.AddOtherRatio("seconds", 2)
	info := &relaycommon.RelayInfo{PriceData: priceData}

	quota, ok := recalcQuotaFromRatios(info, map[string]float64{
		"negative": -1,
		"nan":      math.NaN(),
		"inf":      math.Inf(1),
	})

	assert.False(t, ok)
	assert.Zero(t, quota)
	assert.Equal(t, map[string]float64{"seconds": 2}, info.PriceData.OtherRatios())
	assert.Nil(t, info.QuotaClamp)
}

func TestRecalcQuotaFromRatiosFiltersInvalidValues(t *testing.T) {
	priceData := types.PriceData{Quota: 200}
	priceData.AddOtherRatio("seconds", 2)
	info := &relaycommon.RelayInfo{PriceData: priceData}

	quota, ok := recalcQuotaFromRatios(info, map[string]float64{
		"duration": 3,
		"negative": -1,
		"nan":      math.NaN(),
		"inf":      math.Inf(1),
	})

	require.True(t, ok)
	assert.Equal(t, 300, quota)
	assert.Nil(t, info.QuotaClamp)
}

func TestRejectUncoveredPreparedTaskRetry(t *testing.T) {
	for _, preConsumed := range []int{0, 6} {
		t.Run(fmt.Sprintf("pre-consumed_%d", preConsumed), func(t *testing.T) {
			info := &relaycommon.RelayInfo{
				RetryIndex: 1,
				Billing:    &fixedPreConsumedBilling{quota: preConsumed},
				PriceData:  types.PriceData{Quota: 10},
			}

			taskErr := rejectUncoveredPreparedTaskRetry(info, true)

			require.NotNil(t, taskErr)
			assert.True(t, taskErr.LocalError)
			assert.Equal(t, "pre_consume_failed", taskErr.Code)
			assert.Equal(t, http.StatusForbidden, taskErr.StatusCode)
			assert.ErrorContains(t, taskErr.Error, fmt.Sprintf("quota 10 exceeds pre-consumed quota %d", preConsumed))
		})
	}
}

func TestRejectUncoveredPreparedTaskRetryAllowsCoveredOrUnpreparedBilling(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		prepared   bool
		retryIndex int
		preConsume int
		quota      int
	}{
		{name: "initial attempt", prepared: true, retryIndex: 0, preConsume: 0, quota: 10},
		{name: "not prepared by adaptor", prepared: false, retryIndex: 1, preConsume: 6, quota: 10},
		{name: "same quota", prepared: true, retryIndex: 1, preConsume: 6, quota: 6},
		{name: "lower quota", prepared: true, retryIndex: 1, preConsume: 10, quota: 6},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			info := &relaycommon.RelayInfo{
				RetryIndex: testCase.retryIndex,
				Billing:    &fixedPreConsumedBilling{quota: testCase.preConsume},
				PriceData:  types.PriceData{Quota: testCase.quota},
			}
			assert.Nil(t, rejectUncoveredPreparedTaskRetry(info, testCase.prepared))
		})
	}
}

func TestRelayTaskSubmitStopsBeforeSideEffectsOnHigherPricedRetry(t *testing.T) {
	configureTaskRetryPrice(t)
	fixture := newTaskRetryPriceFixture(t, 6, 10)

	result, taskErr := RelayTaskSubmit(fixture.context, fixture.info)

	require.Nil(t, result)
	require.NotNil(t, taskErr)
	assert.Equal(t, "pre_consume_failed", taskErr.Code)
	assert.Equal(t, http.StatusForbidden, taskErr.StatusCode)
	assert.True(t, taskErr.LocalError)
	assert.ErrorContains(t, taskErr.Error, "quota 10 exceeds pre-consumed quota 6")
	assert.Empty(t, fixture.events.snapshot(), "billing gate must run before image upload and upstream request")
}

func TestRelayTaskSubmitAllowsSameOrLowerPricedRetry(t *testing.T) {
	configureTaskRetryPrice(t)

	for _, testCase := range []struct {
		name        string
		preConsumed int
		duration    int
	}{
		{name: "same price", preConsumed: 10, duration: 10},
		{name: "lower price", preConsumed: 10, duration: 6},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newTaskRetryPriceFixture(t, testCase.preConsumed, testCase.duration)

			result, taskErr := RelayTaskSubmit(fixture.context, fixture.info)

			require.Nil(t, taskErr)
			require.NotNil(t, result)
			assert.Equal(t, testCase.duration, result.Quota)
			assert.Equal(t, []string{"upload", "request"}, fixture.events.snapshot())
			assert.Equal(t, testCase.preConsumed, fixture.info.Billing.GetPreConsumedQuota())
		})
	}
}

func TestRelayTaskSubmitUsesConfiguredDurationFallbackBeforeBillingGate(t *testing.T) {
	configureTaskRetryPrice(t)
	fixture := newTaskRetryPriceFixture(t, 6, 15)

	result, taskErr := RelayTaskSubmit(fixture.context, fixture.info)

	require.Nil(t, result)
	require.NotNil(t, taskErr)
	assert.Equal(t, http.StatusForbidden, taskErr.StatusCode)
	assert.Contains(t, taskErr.Message, "quota 10 exceeds pre-consumed quota 6")
	assert.Empty(t, fixture.events.snapshot())
}

func configureTaskRetryPrice(t *testing.T) {
	t.Helper()
	if service.GetHttpClient() == nil {
		service.InitHttpClient()
	}
	savedModelPrices := ratio_setting.ModelPrice2JSONString()
	savedTaskPricePatches := append([]string(nil), constant.TaskPricePatches...)
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(savedModelPrices))
		constant.TaskPricePatches = savedTaskPricePatches
	})

	modelPrices, err := common.Marshal(map[string]float64{
		"grok-image-video": 1 / float64(common.QuotaPerUnit),
	})
	require.NoError(t, err)
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(string(modelPrices)))
	constant.TaskPricePatches = nil
}

func newTaskRetryPriceFixture(t *testing.T, preConsumedQuota, duration int) *taskRetryPriceFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)

	events := &taskBillingEventLog{}
	uploadServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		events.add("upload")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"url":"https://media.example.com/reference.png"}`)
	}))
	t.Cleanup(uploadServer.Close)

	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		events.add("request")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":200,"data":[{"task_id":"task_upstream","status":"submitted"}]}`)
	}))
	t.Cleanup(upstreamServer.Close)

	t.Setenv("APIMART_BASE64_IMAGE_CHANNEL_IDS", "18")
	t.Setenv("APIMART_INPUT_MEDIA_UPLOAD_URL", uploadServer.URL)
	inlineImage := "data:image/png;base64," + base64.StdEncoding.EncodeToString(
		[]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'},
	)
	body, err := common.Marshal(relaycommon.TaskSubmitReq{
		Model:     "grok-image-video",
		Prompt:    "animate the reference image",
		Duration:  duration,
		ImageURLs: []string{inlineImage},
	})
	require.NoError(t, err)

	writer := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(writer)
	context.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewReader(body))
	context.Request.Header.Set("Content-Type", "application/json")
	common.SetContextKey(context, constant.ContextKeyOriginalModel, "grok-image-video")
	common.SetContextKey(context, constant.ContextKeyChannelId, 18)
	common.SetContextKey(context, constant.ContextKeyChannelType, constant.ChannelTypeSora)
	common.SetContextKey(context, constant.ContextKeyChannelBaseUrl, upstreamServer.URL)
	common.SetContextKey(context, constant.ContextKeyChannelKey, "sk-test")
	common.SetContextKey(context, constant.ContextKeyChannelModelMapping,
		`{"grok-image-video":"grok-imagine-1.5-video-ext"}`)
	common.SetContextKey(context, constant.ContextKeyChannelParamOverride, map[string]interface{}{})
	common.SetContextKey(context, constant.ContextKeyChannelHeaderOverride, map[string]interface{}{})

	info := &relaycommon.RelayInfo{
		OriginModelName: "grok-image-video",
		UserGroup:       "default",
		UsingGroup:      "default",
		RetryIndex:      1,
		Billing: &fixedPreConsumedBilling{
			quota: preConsumedQuota,
		},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{},
	}

	return &taskRetryPriceFixture{
		context: context,
		info:    info,
		events:  events,
	}
}
