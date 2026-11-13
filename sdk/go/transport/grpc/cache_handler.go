// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

package grpc

import (
	"context"
	"math"
	"strings"
	"time"

	"google.golang.org/grpc"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/cache"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// isCacheService reports whether the service name is the Cache service.
func isCacheService(service string) bool {
	svc := service
	if idx := strings.LastIndex(service, "."); idx >= 0 {
		svc = service[idx+1:]
	}
	return svc == "Cache"
}

type cacheHandlerFunc func(ctx context.Context, client pb.QuicBaaSClient, req interface{}) (interface{}, error)

// cacheMethodMap dispatches Cache methods onto the quicbaas.QuicBaaS cache
// RPCs of the same names.
var cacheMethodMap = map[string]cacheHandlerFunc{
	"GetCacheStats":     handleCacheGetStats,
	"ClearCache":        handleCacheClear,
	"GetCacheConfig":    handleCacheGetConfig,
	"UpdateCacheConfig": handleCacheUpdateConfig,
}

// handleCacheRequest routes one Cache method over conn.
func handleCacheRequest(ctx context.Context, conn *grpc.ClientConn, method string, req interface{}) (interface{}, error) {
	handler, ok := cacheMethodMap[method]
	if !ok {
		return nil, errors.NewUnimplemented("grpc.cache."+method, "method not exposed by the QuicBaaS cache RPCs")
	}
	return handler(ctx, pb.NewQuicBaaSClient(conn), req)
}

func handleCacheGetStats(ctx context.Context, client pb.QuicBaaSClient, req interface{}) (interface{}, error) {
	const op = "grpc.cache.GetCacheStats"
	r, err := sdkRequest[cache.GetCacheStatsRequest](op, req)
	if err != nil {
		return nil, err
	}
	if r.EntityType != "" {
		return nil, errors.NewUnimplemented(op, "GetCacheStatsRequest has no entity type filter")
	}
	resp, err := client.GetCacheStats(ctx, &pb.GetCacheStatsRequest{})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	return &cache.GetCacheStatsResponse{
		HitRate:   resp.GetL1HitRate(),
		Size:      cacheCount(int64(resp.GetL1Size())),
		MaxSize:   cacheCount(int64(resp.GetL1Capacity())),
		Evictions: cacheCount(resp.GetL1Evictions()),
		Hits:      cacheCount(resp.GetL1Hits()),
		Misses:    cacheCount(resp.GetL1Misses()),
	}, nil
}

// handleCacheClear serves only a clear of every entry: ClearCacheRequest has
// no entity type, and the server clears the whole cache.
func handleCacheClear(ctx context.Context, client pb.QuicBaaSClient, req interface{}) (interface{}, error) {
	const op = "grpc.cache.ClearCache"
	r, err := sdkRequest[cache.ClearCacheRequest](op, req)
	if err != nil {
		return nil, err
	}
	if r.EntityType != "" {
		return nil, errors.NewUnimplemented(op, "ClearCacheRequest has no entity type filter")
	}
	resp, err := client.ClearCache(ctx, &pb.ClearCacheRequest{})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	return &cache.ClearCacheResponse{EntriesCleared: cacheCount(int64(resp.GetEntriesCleared()))}, nil
}

func handleCacheGetConfig(ctx context.Context, client pb.QuicBaaSClient, req interface{}) (interface{}, error) {
	const op = "grpc.cache.GetCacheConfig"
	if _, err := sdkRequest[cache.GetCacheConfigRequest](op, req); err != nil {
		return nil, err
	}
	resp, err := client.GetCacheConfig(ctx, &pb.GetCacheConfigRequest{})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	return &cache.GetCacheConfigResponse{
		L1Size:          int(resp.GetL1Size()),
		L1TTL:           time.Duration(resp.GetL1TtlSeconds()) * time.Second,
		CleanupInterval: time.Duration(resp.GetCleanupIntervalSeconds()) * time.Second,
		Enabled:         resp.GetEnabled(),
	}, nil
}

// handleCacheUpdateConfig carries the L1 size and TTL. The wire has no
// cleanup interval, counts the TTL in whole seconds and reads a zero size or
// TTL as "no change", so a request it cannot carry faithfully is refused.
func handleCacheUpdateConfig(ctx context.Context, client pb.QuicBaaSClient, req interface{}) (interface{}, error) {
	const op = "grpc.cache.UpdateCacheConfig"
	r, err := sdkRequest[cache.UpdateCacheConfigRequest](op, req)
	if err != nil {
		return nil, err
	}
	if r.CleanupInterval != nil {
		return nil, errors.NewUnimplemented(op, "UpdateCacheConfigRequest has no cleanup interval field")
	}
	wire := &pb.UpdateCacheConfigRequest{}
	if r.L1Size != nil {
		if *r.L1Size <= 0 || *r.L1Size > math.MaxInt32 {
			return nil, errors.NewInvalidInput(op, "L1 size must be between 1 and 2147483647 entries")
		}
		wire.L1Size = int32(*r.L1Size)
	}
	if r.L1TTL != nil {
		if *r.L1TTL < time.Second || *r.L1TTL%time.Second != 0 {
			return nil, errors.NewInvalidInput(op, "L1 TTL must be a whole number of seconds above zero")
		}
		wire.L1TtlSeconds = int64(*r.L1TTL / time.Second)
	}
	resp, err := client.UpdateCacheConfig(ctx, wire)
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	return &cache.UpdateCacheConfigResponse{Updated: resp.GetSuccess()}, nil
}

// cacheCount converts a wire counter; a negative value, which the server
// never sends, reads as zero.
func cacheCount(v int64) uint64 {
	if v < 0 {
		return 0
	}
	return uint64(v)
}
