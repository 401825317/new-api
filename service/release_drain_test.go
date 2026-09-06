package service

import (
	"github.com/QuantumNous/new-api/common"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

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
