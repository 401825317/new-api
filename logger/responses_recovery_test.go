package logger

import (
	"context"
	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"io"
	"sync"
	"testing"
)

func TestResponsesRecoveryConcurrentLogging(t *testing.T) {
	common.LogWriterMu.Lock()
	previous := gin.DefaultWriter
	gin.DefaultWriter = io.Discard
	common.LogWriterMu.Unlock()
	previousCount := logCount.Swap(0)
	defer func() {
		logCount.Store(previousCount)
		common.LogWriterMu.Lock()
		gin.DefaultWriter = previous
		common.LogWriterMu.Unlock()
	}()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				LogInfo(context.Background(), "recovery concurrency test")
			}
		}()
	}
	wg.Wait()
	require.Equal(t, int64(1000), logCount.Load())
}
