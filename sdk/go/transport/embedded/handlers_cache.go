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

	cache "github.com/jeremyhahn/go-quicbaas/sdk/go/client/cache"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

// =============================================================================
// Cache Handlers
// =============================================================================
//
// Each handler accepts either the service request type or the SDK cache
// client's request type. A service request gets the service response; a
// client request gets the client response, which is what the client decodes.

func handleGetCacheStats(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	if r, ok := req.(*service.GetCacheStatsRequest); ok {
		return svc.GetCacheStats(ctx, r)
	}
	sdkReq, ok := req.(*cache.GetCacheStatsRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Cache", "GetCacheStats", "*service.GetCacheStatsRequest or *cache.GetCacheStatsRequest", typeNameOf(req))
	}
	resp, err := svc.GetCacheStats(ctx, &service.GetCacheStatsRequest{EntityType: sdkReq.EntityType})
	if err != nil {
		return nil, err
	}
	return &cache.GetCacheStatsResponse{
		HitRate:   resp.HitRate,
		Size:      resp.Size,
		MaxSize:   resp.MaxSize,
		Evictions: resp.Evictions,
		Hits:      resp.Hits,
		Misses:    resp.Misses,
	}, nil
}

func handleClearCache(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	if r, ok := req.(*service.ClearCacheRequest); ok {
		return svc.ClearCache(ctx, r)
	}
	sdkReq, ok := req.(*cache.ClearCacheRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Cache", "ClearCache", "*service.ClearCacheRequest or *cache.ClearCacheRequest", typeNameOf(req))
	}
	resp, err := svc.ClearCache(ctx, &service.ClearCacheRequest{
		EntityType: sdkReq.EntityType,
		All:        sdkReq.All,
	})
	if err != nil {
		return nil, err
	}
	return &cache.ClearCacheResponse{EntriesCleared: resp.EntriesCleared}, nil
}

func handleGetCacheConfig(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	if r, ok := req.(*service.GetCacheConfigRequest); ok {
		return svc.GetCacheConfig(ctx, r)
	}
	if _, ok := req.(*cache.GetCacheConfigRequest); !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Cache", "GetCacheConfig", "*service.GetCacheConfigRequest or *cache.GetCacheConfigRequest", typeNameOf(req))
	}
	resp, err := svc.GetCacheConfig(ctx, &service.GetCacheConfigRequest{})
	if err != nil {
		return nil, err
	}
	return toSDKCacheConfig(resp), nil
}

func handleUpdateCacheConfig(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	if r, ok := req.(*service.UpdateCacheConfigRequest); ok {
		return svc.UpdateCacheConfig(ctx, r)
	}
	sdkReq, ok := req.(*cache.UpdateCacheConfigRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Cache", "UpdateCacheConfig", "*service.UpdateCacheConfigRequest or *cache.UpdateCacheConfigRequest", typeNameOf(req))
	}
	resp, err := svc.UpdateCacheConfig(ctx, &service.UpdateCacheConfigRequest{
		L1Size:          sdkReq.L1Size,
		L1TTL:           sdkReq.L1TTL,
		CleanupInterval: sdkReq.CleanupInterval,
	})
	if err != nil {
		return nil, err
	}
	return &cache.UpdateCacheConfigResponse{
		Updated:   resp.Updated,
		NewConfig: toSDKCacheConfig(resp.NewConfig),
	}, nil
}

// toSDKCacheConfig maps a service cache configuration onto the SDK client's
// response type. A nil configuration maps to nil.
func toSDKCacheConfig(cfg *service.GetCacheConfigResponse) *cache.GetCacheConfigResponse {
	if cfg == nil {
		return nil
	}
	return &cache.GetCacheConfigResponse{
		L1Size:          cfg.L1Size,
		L1TTL:           cfg.L1TTL,
		CleanupInterval: cfg.CleanupInterval,
		Enabled:         cfg.Enabled,
	}
}
