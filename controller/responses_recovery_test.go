package controller

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestResponsesRecoveryFullRelay(t *testing.T) {
	for _, cached := range []bool{false, true} {
		t.Run(fmt.Sprintf("memory_cache_%t", cached), func(t *testing.T) { runResponsesRecoveryFullRelay(t, cached) })
	}
}

func runResponsesRecoveryFullRelay(t *testing.T, cached bool) {
	t.Setenv("RESPONSES_STREAM_RECOVERY_ENABLED", "true")
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Token{}, &model.Log{}))
	oldMemory, oldRetries, oldCount, oldErrorLogs := common.MemoryCacheEnabled, common.RetryTimes, constant.CountToken, constant.ErrorLogEnabled
	common.MemoryCacheEnabled = cached
	common.RetryTimes = 5
	constant.CountToken = false
	constant.ErrorLogEnabled = true
	t.Cleanup(func() {
		common.MemoryCacheEnabled = oldMemory
		common.RetryTimes = oldRetries
		constant.CountToken = oldCount
		constant.ErrorLogEnabled = oldErrorLogs
	})
	service.InitHttpClient()
	oldStreamTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldStreamTimeout })
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	affinity := operation_setting.GetChannelAffinitySetting()
	oldAffinity := *affinity
	*affinity = operation_setting.ChannelAffinitySetting{Enabled: true, DefaultTTLSeconds: 60, Rules: []operation_setting.ChannelAffinityRule{{Name: "recovery-test", ModelRegex: []string{".*"}, KeySources: []operation_setting.ChannelAffinityKeySource{{Type: "gjson", Path: "prompt_cache_key"}}, IncludeUsingGroup: true, IncludeModelName: true}}}
	t.Cleanup(func() { *affinity = oldAffinity })
	oldRatios := ratio_setting.ModelRatio2JSONString()
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"gpt-4o-mini":1}`))
	t.Cleanup(func() { _ = ratio_setting.UpdateModelRatioByJSONString(oldRatios) })
	var a, b atomic.Int32
	var partial, allFail atomic.Bool
	var barrier chan struct{}
	var arriving atomic.Int32
	pre := "data: {\"type\":\"response.created\",\"response\":{\"status\":\"in_progress\",\"output\":[]}}\n\n"
	delta := "data: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\n\n"
	failure := "data: {\"type\":\"error\",\"error\":{\"type\":\"service_unavailable_error\",\"code\":\"server_error\",\"message\":\"overloaded\"}}\n\n"
	complete := "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"usage\":{\"input_tokens\":10,\"output_tokens\":2,\"total_tokens\":12}}}\n\n"
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.Add(1)
		if barrier != nil {
			if arriving.Add(1) == 50 {
				close(barrier)
			}
			select {
			case <-barrier:
			case <-r.Context().Done():
				return
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, pre)
		if partial.Load() {
			io.WriteString(w, delta)
		}
		io.WriteString(w, failure)
	}))
	defer first.Close()
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		if allFail.Load() {
			io.WriteString(w, failure)
		} else {
			io.WriteString(w, pre+delta+complete)
		}
	}))
	defer second.Close()
	const balance = 1000000
	user := model.User{Id: 781, Username: "recovery-lab", Quota: balance, Group: "default"}
	token := model.Token{Id: 781, UserId: 781, Key: "local-test-only", RemainQuota: balance}
	require.NoError(t, db.Create(&user).Error)
	require.NoError(t, db.Create(&token).Error)
	runID := fmt.Sprint(time.Now().UnixNano())
	group := "recovery-fallback-" + runID
	addChannels := func(g string) {
		for i, url := range []string{first.URL, second.URL} {
			ch := model.Channel{Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Name: fmt.Sprintf("lab-%s-%d", g, i), Key: "local-mock", BaseURL: &url, Models: "gpt-4o-mini", Group: g, Priority: common.GetPointer(int64(20 - i)), Weight: common.GetPointer(uint(100)), AutoBan: common.GetPointer(0)}
			require.NoError(t, ch.Insert())
		}
		model.InitChannelCache()
	}
	addChannels(group)
	engine := gin.New()
	engine.POST("/v1/responses", func(c *gin.Context) {
		c.Set("id", 781)
		c.Set("user_group", "default")
		c.Set("group", group)
		c.Set("token_group", group)
		c.Set("token_id", 781)
		c.Set("token_key", "local-test-only")
		c.Set("user_quota", balance)
		common.SetContextKey(c, constant.ContextKeyUserSetting, dto.UserSetting{BillingPreference: "wallet_only"})
	}, middleware.Distribute(), func(c *gin.Context) { Relay(c, types.RelayFormatOpenAIResponses) })
	request := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(`{"model":"gpt-4o-mini","input":"hi","stream":true,"prompt_cache_key":"recovery-session"}`))
		r.Header.Set("Content-Type", "application/json")
		engine.ServeHTTP(w, r)
		return w
	}
	w := request()
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, int32(1), a.Load())
	require.Equal(t, int32(1), b.Load())
	require.Contains(t, w.Body.String(), "response.completed")
	require.NotContains(t, w.Body.String(), "overloaded")
	require.Equal(t, 1, strings.Count(w.Body.String(), "\"delta\":\"hello\""))
	var consume, errorCount int64
	require.NoError(t, db.Model(&model.Log{}).Where("type = ?", model.LogTypeConsume).Count(&consume).Error)
	require.NoError(t, db.Model(&model.Log{}).Where("type = ?", model.LogTypeError).Count(&errorCount).Error)
	require.Equal(t, int64(1), consume)
	require.Equal(t, int64(1), errorCount)
	var billed model.User
	require.NoError(t, db.First(&billed, 781).Error)
	require.Greater(t, balance-billed.Quota, 0)
	var logged model.Log
	require.NoError(t, db.Where("type = ?", model.LogTypeConsume).First(&logged).Error)
	require.Equal(t, logged.Quota, balance-billed.Quota)
	probe, _ := gin.CreateTestContext(httptest.NewRecorder())
	probe.Request = httptest.NewRequest("POST", "/v1/responses", strings.NewReader(`{"prompt_cache_key":"recovery-session"}`))
	probe.Request.Header.Set("Content-Type", "application/json")
	probe.Set("responses_recovery_stream_request", true)
	id, found := service.GetPreferredChannelByAffinity(probe, "gpt-4o-mini", group)
	require.True(t, found)
	require.Equal(t, 2, id, "affinity must follow successful fallback, not failed initial channel")
	w = request()
	require.Equal(t, 200, w.Code)
	require.Equal(t, int32(1), a.Load())
	require.Equal(t, int32(2), b.Load())
	group = "recovery-partial-" + runID
	addChannels(group)
	partial.Store(true)
	w = request()
	require.Equal(t, 200, w.Code)
	require.Equal(t, int32(2), a.Load())
	require.Equal(t, int32(2), b.Load())
	require.Contains(t, w.Body.String(), "overloaded")
	require.NotContains(t, w.Body.String(), "response.completed")
	partial.Store(false)
	allFail.Store(true)
	group = "recovery-exhausted-" + runID
	addChannels(group)
	before := billed.Quota
	require.NoError(t, db.First(&billed, 781).Error)
	before = billed.Quota
	w = request()
	require.Equal(t, 503, w.Code, w.Body.String())
	require.NotContains(t, w.Header().Get("Content-Type"), "text/event-stream")
	require.Equal(t, int32(3), a.Load())
	require.Equal(t, int32(3), b.Load())
	require.Eventually(t, func() bool { var u model.User; return db.First(&u, 781).Error == nil && u.Quota == before }, time.Second*3, time.Millisecond*20, "failed request must refund reservation")
	allFail.Store(false)
	group = "recovery-concurrent-" + runID
	addChannels(group)
	barrier = make(chan struct{})
	var wg sync.WaitGroup
	results := make(chan *httptest.ResponseRecorder, 50)
	started := time.Now()
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); results <- request() }()
	}
	wg.Wait()
	close(results)
	for result := range results {
		require.Equal(t, 200, result.Code, result.Body.String())
		require.Contains(t, result.Body.String(), "response.completed")
		require.NotContains(t, result.Body.String(), "overloaded")
		require.Equal(t, 1, strings.Count(result.Body.String(), "\"delta\":\"hello\""))
	}
	require.Equal(t, int32(53), a.Load())
	require.Equal(t, int32(53), b.Load())
	var concurrentLogs []model.Log
	require.NoError(t, db.Where("type = ? AND channel_id = ?", model.LogTypeConsume, 8).Find(&concurrentLogs).Error)
	require.Len(t, concurrentLogs, 50)
	for _, entry := range concurrentLogs {
		require.Equal(t, 18, entry.Quota)
	}
	t.Logf("50 concurrent full-relay requests: 50 failed attempts, 50 successful fallbacks, 50 single settlements; elapsed=%s", time.Since(started))
	barrier = nil
	t.Run("OfficialBaseline", func(t *testing.T) {
		t.Setenv("RESPONSES_STREAM_RECOVERY_ENABLED", "false")
		group = "recovery-off-" + runID
		addChannels(group)
		w = request()
		require.Equal(t, 200, w.Code)
		require.Contains(t, w.Body.String(), "overloaded")
		require.NotContains(t, w.Body.String(), "response.completed")
		require.Equal(t, int32(54), a.Load())
		require.Equal(t, int32(53), b.Load(), "official behavior must not switch on this SSE error")
	})
}
