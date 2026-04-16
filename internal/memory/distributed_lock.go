package memory

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// DistributedLock implements a Redis distributed lock using SETNX + Lua release.
// Mirrors Java CompressibleChatMemory's lock mechanism.
type DistributedLock struct {
	client         *redis.Client
	expireSeconds  time.Duration
	retryTimes     int
	retryInterval  time.Duration
}

// NewDistributedLock creates a distributed lock with the given parameters.
func NewDistributedLock(client *redis.Client, expireSeconds int, retryTimes int, retryIntervalMs int) *DistributedLock {
	return &DistributedLock{
		client:        client,
		expireSeconds: time.Duration(expireSeconds) * time.Second,
		retryTimes:    retryTimes,
		retryInterval: time.Duration(retryIntervalMs) * time.Millisecond,
	}
}

// Acquire tries to acquire the lock with retries. Returns the lock value on success.
// Mirrors Java acquireLock — SETNX with TTL.
func (l *DistributedLock) Acquire(ctx context.Context, lockKey string) (string, bool) {
	lockValue := fmt.Sprintf("%d", time.Now().UnixNano())

	for i := 0; i < l.retryTimes; i++ {
		ok, err := l.client.SetNX(ctx, lockKey, lockValue, l.expireSeconds).Result()
		if err != nil {
			continue
		}
		if ok {
			return lockValue, true
		}
		if i < l.retryTimes-1 {
			time.Sleep(l.retryInterval)
		}
	}

	return "", false
}

// Release releases the lock using a Lua script to ensure atomicity.
// Mirrors Java releaseLock — only the lock owner can delete.
func (l *DistributedLock) Release(ctx context.Context, lockKey, lockValue string) {
	luaScript := `
if redis.call('get', KEYS[1]) == ARGV[1] then
	return redis.call('del', KEYS[1])
else
	return 0
end
`
	l.client.Eval(ctx, luaScript, []string{lockKey}, lockValue)
}
