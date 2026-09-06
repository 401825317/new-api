package service

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"net"
	"net/http"
	"sync"
	"time"
)

type ReleaseDrainStatus struct {
	common.DrainSnapshot
	PendingBatch     int  `json:"pending_batch"`
	PendingDashboard int  `json:"pending_dashboard"`
	SafeToStop       bool `json:"safe_to_stop"`
}

var drainPollMu sync.Mutex

func GetReleaseDrainStatus(flush bool) ReleaseDrainStatus {
	drainPollMu.Lock()
	defer drainPollMu.Unlock()
	s := common.ReleaseDrain.Snapshot()
	if flush && s.Draining && s.Active == 0 && !s.Failed {
		if err := model.FlushReleaseBatch(); err == nil {
			_ = model.FlushReleaseQuotaData()
		}
	}
	batch, dashboard, failed := model.ReleasePending()
	s = common.ReleaseDrain.Snapshot()
	return ReleaseDrainStatus{s, batch, dashboard, common.ReleaseDrainEnabled && s.Draining && s.Active == 0 && !s.Failed && !failed && batch == 0 && dashboard == 0}
}

// No public route, no proxy headers, no resume/force-stop endpoint.
func ReleaseDrainHandler(token string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+token)) != 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Path == "/drain" && r.Method == http.MethodPost {
			common.ReleaseDrain.Begin()
			w.WriteHeader(http.StatusAccepted)
			return
		}
		if r.URL.Path != "/status" || r.Method != http.MethodGet {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(GetReleaseDrainStatus(false))
	})
}

func StartReleaseDrainServer(token string) error {
	if !common.ReleaseDrainEnabled {
		return nil
	}
	if len(token) < 32 {
		return errors.New("RELEASE_DRAIN_TOKEN must contain at least 32 characters")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:3001")
	if err != nil {
		return err
	}
	server := &http.Server{Handler: ReleaseDrainHandler(token), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second}
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			common.ReleaseDrain.Fail()
			common.SysError("release drain listener failed")
		}
	}()
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for range ticker.C {
			_ = GetReleaseDrainStatus(true)
		}
	}()
	return nil
}
