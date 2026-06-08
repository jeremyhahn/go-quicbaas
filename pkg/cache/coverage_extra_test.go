// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

package cache

import (
	"context"
	"testing"
	"time"

	qconfig "github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// failingL2Cache is an L2Cache whose every method returns a non-NotFound error.
// It exercises the graceful-degradation error branches in CacheManager and
// CacheRegistry that the in-memory MockL2Cache cannot reach (it only fails on
// close / context-cancel).
type failingL2Cache struct {
	getErr   error
	setErr   error
	delErr   error
	closeErr error
}

func (f *failingL2Cache) Get(ctx context.Context, key string) ([]byte, error) {
	return nil, f.getErr
}

func (f *failingL2Cache) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	return f.setErr
}

func (f *failingL2Cache) Delete(ctx context.Context, key string) error {
	return f.delErr
}

func (f *failingL2Cache) Ping(ctx context.Context) error { return nil }

func (f *failingL2Cache) Close() error { return f.closeErr }

// newFailingL2 returns a failing L2 whose operations report internal errors.
func newFailingL2() *failingL2Cache {
	return &failingL2Cache{
		getErr:   errors.Internal("failingL2.Get", "backend down"),
		setErr:   errors.Internal("failingL2.Set", "backend down"),
		delErr:   errors.Internal("failingL2.Delete", "backend down"),
		closeErr: errors.Internal("failingL2.Close", "backend down"),
	}
}

func managerWithL2(t *testing.T, l2 L2Cache, policy WritePolicy) *CacheManager {
	t.Helper()
	cfg := DefaultConfig()
	cfg.L2Cache = l2
	cfg.WritePolicy = policy
	cm, err := NewManager(cfg, testLogger(t))
	require.NoError(t, err)
	return cm
}

// TestManagerGet_L2NonNotFoundError covers the branch in Get where the L2
// backend returns an error other than NotFound (tracks l2_errors, not
// l2_misses).
func TestManagerGet_L2NonNotFoundError(t *testing.T) {
	cm := managerWithL2(t, newFailingL2(), WriteThroughSync)
	defer cm.Close()

	_, err := cm.Get(context.Background(), "missing")
	require.Error(t, err)
	assert.True(t, errors.IsNotFound(err), "Get should still return NotFound to caller")

	stats := cm.Stats()
	assert.Equal(t, int64(1), stats.L2Errors, "non-NotFound L2 error should be counted")
	assert.Equal(t, int64(0), stats.L2Misses, "non-NotFound error must not count as a miss")
}

// TestManagerGet_L2NotFoundError covers the NotFound branch (counted as miss).
func TestManagerGet_L2NotFoundError(t *testing.T) {
	l2 := &failingL2Cache{getErr: errors.NotFound("failingL2.Get", "k")}
	cm := managerWithL2(t, l2, WriteThroughSync)
	defer cm.Close()

	_, err := cm.Get(context.Background(), "missing")
	require.Error(t, err)
	stats := cm.Stats()
	assert.Equal(t, int64(1), stats.L2Misses)
	assert.Equal(t, int64(0), stats.L2Errors)
}

// TestManagerSet_SyncL2Error covers the synchronous write-through error path
// where the L2 write fails but the operation still succeeds (graceful
// degradation) and an l2_errors metric is recorded.
func TestManagerSet_SyncL2Error(t *testing.T) {
	cm := managerWithL2(t, newFailingL2(), WriteThroughSync)
	defer cm.Close()

	err := cm.Set(context.Background(), "key", []byte("value"))
	require.NoError(t, err, "Set must not fail when only L2 fails")

	stats := cm.Stats()
	assert.Equal(t, int64(1), stats.L2Errors)
	assert.Equal(t, int64(0), stats.L2Sets)

	// L1 must still hold the value.
	v, err := cm.Get(context.Background(), "key")
	require.NoError(t, err)
	assert.Equal(t, []byte("value"), v)
}

// TestManagerDelete_L2Error covers the L2 delete error branch.
func TestManagerDelete_L2Error(t *testing.T) {
	cm := managerWithL2(t, newFailingL2(), WriteThroughSync)
	defer cm.Close()

	err := cm.Delete(context.Background(), "key")
	require.NoError(t, err, "Delete must not fail when only L2 fails")

	stats := cm.Stats()
	assert.Equal(t, int64(1), stats.L2Errors)
}

// TestManagerClose_L2Error covers the branch where the L2 Close returns an
// error and the manager propagates it.
func TestManagerClose_L2Error(t *testing.T) {
	cm := managerWithL2(t, newFailingL2(), WriteThroughSync)

	err := cm.Close()
	require.Error(t, err)
	assert.Equal(t, errors.ErrInternal, errors.Code(err))
}

// TestRegistryClose_GlobalCacheError covers the registry Close branch where the
// shared L2 used by the global cache fails to close.
func TestRegistryClose_GlobalCacheError(t *testing.T) {
	cfg := &qconfig.EnhancedCacheConfig{
		Enabled:         true,
		MaxSize:         16,
		DefaultTTL:      time.Minute,
		CleanupInterval: time.Minute,
	}
	reg, err := NewCacheRegistry(cfg, testLogger(t), newFailingL2())
	require.NoError(t, err)
	require.NotNil(t, reg.globalCache)

	err = reg.Close()
	require.Error(t, err, "registry Close should surface the global cache close error")
	assert.Equal(t, errors.ErrInternal, errors.Code(err))
}

// TestPolicyString covers the WriteBack and default branches of String.
func TestPolicyString(t *testing.T) {
	assert.Equal(t, "WriteBack", WriteBack.String())
	assert.Equal(t, "WriteThroughSync", WriteThroughSync.String())
	assert.Equal(t, "WriteThroughAsync", WriteThroughAsync.String())
	assert.Equal(t, "WritePolicy(99)", WritePolicy(99).String())
}

// TestLRUPeek_Missing covers the not-found branch of Peek.
func TestLRUPeek_Missing(t *testing.T) {
	c := NewLRUCache(4)
	entry, ok := c.Peek("nope")
	assert.False(t, ok)
	assert.Nil(t, entry)
}

// TestLRUGetOldest_Empty covers the empty-list branch of GetOldest.
func TestLRUGetOldest_Empty(t *testing.T) {
	c := NewLRUCache(4)
	key, entry, ok := c.GetOldest()
	assert.False(t, ok)
	assert.Empty(t, key)
	assert.Nil(t, entry)
}

// TestWarmPrefixes_SkipsEmptyPrefix covers the inner "prefix == \"\" -> continue"
// branch in WarmPrefixes (distinct from the whole-list-empty guard).
func TestWarmPrefixes_SkipsEmptyPrefix(t *testing.T) {
	cm := managerWithL2(t, NewMockL2Cache(), WriteThroughSync)
	defer cm.Close()

	storage := newMockStorageReader()
	storage.AddKey("user:1", "alice")

	warmer, err := NewCacheWarmer(cm, storage, WarmingConfig{BatchSize: 10}, testLogger(t))
	require.NoError(t, err)

	// Mixed list: an empty prefix must be skipped without aborting the others.
	status, err := warmer.WarmPrefixes(context.Background(), []string{"", "user:"})
	require.NoError(t, err)
	assert.Equal(t, "completed", status.State)
	assert.GreaterOrEqual(t, status.KeysWarmed, int64(1))
}

// TestRedisStubReturnsNotImplemented verifies the build-tag fallback used when
// the package is compiled without -tags l2_redis: every Redis L2 operation must
// report ErrNotImplemented rather than silently succeeding.
func TestRedisStubReturnsNotImplemented(t *testing.T) {
	ctx := context.Background()

	cfg := DefaultRedisL2Config()
	assert.Equal(t, "localhost:6379", cfg.Addr)
	assert.Equal(t, errors.ErrNotImplemented, errors.Code(cfg.Validate()))

	c, err := NewRedisL2Cache(ctx, cfg, testLogger(t))
	require.Error(t, err)
	assert.Nil(t, c)
	assert.Equal(t, errors.ErrNotImplemented, errors.Code(err))

	// Methods on a zero-value stub receiver must all report not-implemented
	// (except Close, a no-op, and PoolStats, which returns nil).
	stub := &RedisL2Cache{}
	_, err = stub.Get(ctx, "k")
	assert.Equal(t, errors.ErrNotImplemented, errors.Code(err))
	assert.Equal(t, errors.ErrNotImplemented, errors.Code(stub.Set(ctx, "k", []byte("v"), 0)))
	assert.Equal(t, errors.ErrNotImplemented, errors.Code(stub.Delete(ctx, "k")))
	assert.Equal(t, errors.ErrNotImplemented, errors.Code(stub.Ping(ctx)))
	assert.NoError(t, stub.Close())
	assert.Nil(t, stub.PoolStats())
	assert.Equal(t, errors.ErrNotImplemented, errors.Code(stub.FlushDB(ctx)))
	_, err = stub.DBSize(ctx)
	assert.Equal(t, errors.ErrNotImplemented, errors.Code(err))
}
