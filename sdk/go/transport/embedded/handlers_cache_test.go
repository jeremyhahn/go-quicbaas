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

package embedded

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cache "github.com/jeremyhahn/go-quicbaas/sdk/go/client/cache"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

// errCacheDown is the error the cache double returns when told to fail.
type errCacheDown struct{}

func (errCacheDown) Error() string { return "cache down" }

// fakeCacheQuicBaaS answers the four cache verbs deterministically and records
// the service requests it received. The embedded interface is never called.
type fakeCacheQuicBaaS struct {
	service.QuicBaaS
	fail bool

	statsReq  *service.GetCacheStatsRequest
	clearReq  *service.ClearCacheRequest
	updateReq *service.UpdateCacheConfigRequest
}

func (f *fakeCacheQuicBaaS) GetCacheStats(_ context.Context, req *service.GetCacheStatsRequest) (*service.GetCacheStatsResponse, error) {
	f.statsReq = req
	if f.fail {
		return nil, errCacheDown{}
	}
	return &service.GetCacheStatsResponse{HitRate: 0.75, Size: 3, MaxSize: 10, Evictions: 1, Hits: 6, Misses: 2}, nil
}

func (f *fakeCacheQuicBaaS) ClearCache(_ context.Context, req *service.ClearCacheRequest) (*service.ClearCacheResponse, error) {
	f.clearReq = req
	if f.fail {
		return nil, errCacheDown{}
	}
	return &service.ClearCacheResponse{EntriesCleared: 3}, nil
}

func (f *fakeCacheQuicBaaS) GetCacheConfig(context.Context, *service.GetCacheConfigRequest) (*service.GetCacheConfigResponse, error) {
	if f.fail {
		return nil, errCacheDown{}
	}
	return &service.GetCacheConfigResponse{L1Size: 10, L1TTL: time.Minute, CleanupInterval: time.Second, Enabled: true}, nil
}

func (f *fakeCacheQuicBaaS) UpdateCacheConfig(_ context.Context, req *service.UpdateCacheConfigRequest) (*service.UpdateCacheConfigResponse, error) {
	f.updateReq = req
	if f.fail {
		return nil, errCacheDown{}
	}
	return &service.UpdateCacheConfigResponse{
		Updated:   true,
		NewConfig: &service.GetCacheConfigResponse{L1Size: *req.L1Size, L1TTL: time.Minute, Enabled: true},
	}, nil
}

func newCacheClient(t *testing.T, svc *fakeCacheQuicBaaS) cache.Client {
	t.Helper()
	tr, err := New(svc)
	require.NoError(t, err)
	return cache.NewClient(tr)
}

func TestCacheClient_Embedded_Success(t *testing.T) {
	ctx := context.Background()
	svc := &fakeCacheQuicBaaS{}
	c := newCacheClient(t, svc)

	stats, err := c.GetCacheStats(ctx, &cache.GetCacheStatsRequest{EntityType: "user"})
	require.NoError(t, err)
	assert.Equal(t, &cache.GetCacheStatsResponse{HitRate: 0.75, Size: 3, MaxSize: 10, Evictions: 1, Hits: 6, Misses: 2}, stats)
	assert.Equal(t, "user", svc.statsReq.EntityType)

	cleared, err := c.ClearCache(ctx, &cache.ClearCacheRequest{EntityType: "user", All: true})
	require.NoError(t, err)
	assert.Equal(t, uint64(3), cleared.EntriesCleared)
	assert.Equal(t, &service.ClearCacheRequest{EntityType: "user", All: true}, svc.clearReq)

	cfg, err := c.GetCacheConfig(ctx, &cache.GetCacheConfigRequest{})
	require.NoError(t, err)
	assert.Equal(t, &cache.GetCacheConfigResponse{L1Size: 10, L1TTL: time.Minute, CleanupInterval: time.Second, Enabled: true}, cfg)

	size := 20
	ttl := 2 * time.Minute
	updated, err := c.UpdateCacheConfig(ctx, &cache.UpdateCacheConfigRequest{L1Size: &size, L1TTL: &ttl})
	require.NoError(t, err)
	assert.True(t, updated.Updated)
	require.NotNil(t, updated.NewConfig)
	assert.Equal(t, 20, updated.NewConfig.L1Size)
	assert.Equal(t, &size, svc.updateReq.L1Size)
	assert.Equal(t, &ttl, svc.updateReq.L1TTL)
	assert.Nil(t, svc.updateReq.CleanupInterval)
}

func TestCacheClient_Embedded_ServiceError(t *testing.T) {
	ctx := context.Background()
	c := newCacheClient(t, &fakeCacheQuicBaaS{fail: true})

	_, err := c.GetCacheStats(ctx, &cache.GetCacheStatsRequest{})
	assert.ErrorIs(t, err, errCacheDown{})
	_, err = c.ClearCache(ctx, &cache.ClearCacheRequest{All: true})
	assert.ErrorIs(t, err, errCacheDown{})
	_, err = c.GetCacheConfig(ctx, &cache.GetCacheConfigRequest{})
	assert.ErrorIs(t, err, errCacheDown{})
	size := 5
	_, err = c.UpdateCacheConfig(ctx, &cache.UpdateCacheConfigRequest{L1Size: &size})
	assert.ErrorIs(t, err, errCacheDown{})
}

func TestCacheHandlers_WrongRequestType(t *testing.T) {
	tr, err := New(&fakeCacheQuicBaaS{})
	require.NoError(t, err)
	for _, method := range []string{"GetCacheStats", "ClearCache", "GetCacheConfig", "UpdateCacheConfig"} {
		t.Run(method, func(t *testing.T) {
			_, err := tr.Request(context.Background(), "quicbaas.Cache", method, "invalid")
			var typeErr *InvalidRequestTypeError
			assert.ErrorAs(t, err, &typeErr)
		})
	}
}

func TestCacheHandlers_OldNamesUnrouted(t *testing.T) {
	tr, err := New(&fakeCacheQuicBaaS{})
	require.NoError(t, err)
	for _, method := range []string{"GetStats", "Clear", "GetConfig", "UpdateConfig"} {
		t.Run(method, func(t *testing.T) {
			_, err := tr.Request(context.Background(), "quicbaas.Cache", method, &cache.GetCacheConfigRequest{})
			var unknown *UnknownMethodError
			assert.ErrorAs(t, err, &unknown)
		})
	}
}
