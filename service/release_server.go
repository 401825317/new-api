package service

import (
	"context"
	"errors"
	"github.com/QuantumNous/new-api/common"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func RunReleaseHTTPServer(address string, handler http.Handler) error {
	server := &http.Server{Addr: address, Handler: handler}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	result := make(chan error, 1)
	go func() { result <- server.ListenAndServe() }()
	select {
	case err := <-result:
		return err
	case <-signals:
		common.ReleaseDrain.Begin()
	}
	// A platform SIGKILL cannot be intercepted. The release controller must
	// pre-drain and observe safe_to_stop before asking the platform to stop.
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if GetReleaseDrainStatus(true).SafeToStop {
			break
		}
		select {
		case err := <-result:
			return err
		case <-ticker.C:
		}
	}
	if err := server.Shutdown(context.Background()); err != nil {
		return err
	}
	err := <-result
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
