package middleware

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupRedisRateLimitTest(t *testing.T) *miniredis.Miniredis {
	t.Helper()
	server := miniredis.RunT(t)
	previousRDB := common.RDB
	previousRedisEnabled := common.RedisEnabled
	common.RDB = redis.NewClient(&redis.Options{Addr: server.Addr()})
	common.RedisEnabled = true
	t.Cleanup(func() {
		_ = common.RDB.Close()
		common.RDB = previousRDB
		common.RedisEnabled = previousRedisEnabled
	})
	return server
}

func TestRedisSlidingWindowIsAtomicUnderConcurrency(t *testing.T) {
	server := setupRedisRateLimitTest(t)
	const (
		requestCount = 32
		maxRequests  = 5
	)

	start := make(chan struct{})
	var waitGroup sync.WaitGroup
	results := make(chan struct {
		allowed bool
		err     error
	}, requestCount)
	for i := 0; i < requestCount; i++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			<-start
			ok, err := redisSlidingWindowAllowed(context.Background(), common.RDB, "rateLimit:test:concurrent", maxRequests, 60)
			results <- struct {
				allowed bool
				err     error
			}{allowed: ok, err: err}
		}()
	}
	close(start)
	waitGroup.Wait()
	close(results)

	allowed := 0
	for result := range results {
		require.NoError(t, result.err)
		if result.allowed {
			allowed++
		}
	}
	assert.Equal(t, maxRequests, allowed)
	assert.Equal(t, int64(maxRequests), common.RDB.LLen(context.Background(), "rateLimit:test:concurrent").Val())
	assert.Greater(t, server.TTL("rateLimit:test:concurrent"), time.Duration(0))
}

func TestRedisSlidingWindowEnforcesBoundaryAndExpiresOldestRequest(t *testing.T) {
	setupRedisRateLimitTest(t)
	ctx := context.Background()
	key := "rateLimit:test:boundary"

	ok, err := redisSlidingWindowAllowed(ctx, common.RDB, key, 2, 60)
	require.NoError(t, err)
	assert.True(t, ok)
	ok, err = redisSlidingWindowAllowed(ctx, common.RDB, key, 2, 60)
	require.NoError(t, err)
	assert.True(t, ok)
	ok, err = redisSlidingWindowAllowed(ctx, common.RDB, key, 2, 60)
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Equal(t, int64(2), common.RDB.LLen(ctx, key).Val())

	oldKey := "rateLimit:test:expired"
	oldTimestamp := time.Now().Add(-2 * time.Second).Format(timeFormat)
	require.NoError(t, common.RDB.RPush(ctx, oldKey, oldTimestamp).Err())
	ok, err = redisSlidingWindowAllowed(ctx, common.RDB, oldKey, 1, 1)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, int64(1), common.RDB.LLen(ctx, oldKey).Val())

	zeroKey := "rateLimit:test:zero"
	ok, err = redisSlidingWindowAllowed(ctx, common.RDB, zeroKey, 0, 60)
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Equal(t, int64(0), common.RDB.LLen(ctx, zeroKey).Val())
}

func TestRedisSlidingWindowRejectsMalformedExistingTimestamp(t *testing.T) {
	setupRedisRateLimitTest(t)
	ctx := context.Background()
	key := "rateLimit:test:malformed"
	require.NoError(t, common.RDB.RPush(ctx, key, "not-a-timestamp").Err())

	ok, err := redisSlidingWindowAllowed(ctx, common.RDB, key, 1, 60)

	assert.Error(t, err)
	assert.False(t, ok)
}

func TestRedisSlidingWindowFailsClosedWithoutClient(t *testing.T) {
	ok, err := redisSlidingWindowAllowed(context.Background(), nil, "rateLimit:test:nil", 1, 60)
	assert.Error(t, err)
	assert.False(t, ok)
}
