package service

import (
	"errors"
	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type drainRefundSource struct {
	release chan struct{}
	calls   atomic.Int32
}

func (f *drainRefundSource) Source() string       { return BillingSourceWallet }
func (f *drainRefundSource) PreConsume(int) error { return nil }
func (f *drainRefundSource) Settle(int) error     { return errors.New("injected settlement error") }
func (f *drainRefundSource) Refund() error {
	f.calls.Add(1)
	<-f.release
	return errors.New("injected refund error")
}

func TestReleaseDrainWaitsForRefundAndLatchesFailure(t *testing.T) {
	oldEnabled, oldTracker := common.ReleaseDrainEnabled, common.ReleaseDrain
	common.ReleaseDrainEnabled = true
	common.ReleaseDrain = &common.DrainTracker{}
	defer func() { common.ReleaseDrainEnabled, common.ReleaseDrain = oldEnabled, oldTracker }()
	source := &drainRefundSource{release: make(chan struct{})}
	session := &BillingSession{funding: source, tokenConsumed: 1, relayInfo: &relaycommon.RelayInfo{IsPlayground: true}}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	done, _ := common.ReleaseDrain.Start()
	session.Refund(c)
	session.Refund(c)
	common.ReleaseDrain.Begin()
	done()
	pending := common.ReleaseDrain.Snapshot().Active
	close(source.release)
	deadline := time.Now().Add(3 * time.Second)
	for common.ReleaseDrain.Snapshot().Active != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if pending != 1 || source.calls.Load() != 1 {
		t.Fatal("refund not tracked exactly once")
	}
	state := common.ReleaseDrain.Snapshot()
	if state.Active != 0 || !state.Failed {
		t.Fatalf("refund failure lost: %+v", state)
	}
	if GetReleaseDrainStatus(false).SafeToStop {
		t.Fatal("failed refund allowed stop")
	}
}

func TestReleaseDrainAuthenticationAndPendingWork(t *testing.T) {
	oldEnabled, oldTracker := common.ReleaseDrainEnabled, common.ReleaseDrain
	common.ReleaseDrainEnabled = true
	common.ReleaseDrain = &common.DrainTracker{}
	defer func() { common.ReleaseDrainEnabled, common.ReleaseDrain = oldEnabled, oldTracker }()
	token := strings.Repeat("x", 32)
	handler := ReleaseDrainHandler(token)
	request := httptest.NewRequest(http.MethodPost, "/drain", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, request)
	if w.Code != 401 || common.ReleaseDrain.Snapshot().Draining {
		t.Fatal("unauthorized drain")
	}
	done, _ := common.ReleaseDrain.Start()
	child := common.ReleaseDrain.Child()
	request.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, request)
	if w.Code != 202 {
		t.Fatal("drain not accepted")
	}
	done()
	if GetReleaseDrainStatus(false).SafeToStop {
		t.Fatal("ignored queued billing")
	}
	child()
	if !GetReleaseDrainStatus(false).SafeToStop {
		t.Fatal("empty process not safe")
	}
	common.ReleaseDrain.Fail()
	if GetReleaseDrainStatus(false).SafeToStop {
		t.Fatal("failure allowed stop")
	}
}
