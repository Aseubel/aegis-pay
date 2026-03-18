package mq

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type DistributedLock struct {
	client     *redis.Client
	key        string
	value      string
	expiration time.Duration
}

func NewDistributedLock(client *redis.Client, key string, expiration time.Duration) *DistributedLock {
	return &DistributedLock{
		client:     client,
		key:        key,
		value:      fmt.Sprintf("%d", time.Now().UnixNano()),
		expiration: expiration,
	}
}

func (l *DistributedLock) Acquire(ctx context.Context) (bool, error) {
	ok, err := l.client.SetNX(ctx, l.key, l.value, l.expiration).Result()
	if err != nil {
		return false, err
	}
	return ok, nil
}

func (l *DistributedLock) Release(ctx context.Context) error {
	script := `
		if redis.call("get", KEYS[1]) == ARGV[1] then
			return redis.call("del", KEYS[1])
		else
			return 0
		end
	`
	_, err := l.client.Eval(ctx, script, []string{l.key}, l.value).Result()
	return err
}

type LockManager struct {
	client *redis.Client
}

func NewLockManager(client *redis.Client) *LockManager {
	return &LockManager{client: client}
}

func (m *LockManager) NewLock(key string, expiration time.Duration) *DistributedLock {
	return NewDistributedLock(m.client, key, expiration)
}

func (m *LockManager) AcquireLock(ctx context.Context, key string, expiration time.Duration) (func(), error) {
	lock := m.NewLock(key, expiration)
	ok, err := lock.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("failed to acquire lock: %s", key)
	}
	return func() {
		lock.Release(ctx)
	}, nil
}
