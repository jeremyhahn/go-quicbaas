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

package grpc

import (
	"context"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/common/safeconv"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// GetCacheStats returns cache statistics.
func (s *Server) GetCacheStats(ctx context.Context, req *pb.GetCacheStatsRequest) (*pb.GetCacheStatsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	// Call service layer
	serviceReq := &service.GetCacheStatsRequest{}

	serviceResp, err := s.service.GetCacheStats(ctx, serviceReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	return &pb.GetCacheStatsResponse{
		Enabled:     true, // Cache is enabled if we got stats
		L1Size:      safeconv.Uint64ToInt32(serviceResp.Size),
		L1Capacity:  safeconv.Uint64ToInt32(serviceResp.MaxSize),
		L1Hits:      safeconv.Uint64ToInt64(serviceResp.Hits),
		L1Misses:    safeconv.Uint64ToInt64(serviceResp.Misses),
		L1HitRate:   serviceResp.HitRate,
		L1Evictions: safeconv.Uint64ToInt64(serviceResp.Evictions),
	}, nil
}

// ClearCache clears the cache.
func (s *Server) ClearCache(ctx context.Context, req *pb.ClearCacheRequest) (*pb.ClearCacheResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	// Call service layer
	serviceReq := &service.ClearCacheRequest{
		All: true, // Default to clearing all
	}

	serviceResp, err := s.service.ClearCache(ctx, serviceReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	return &pb.ClearCacheResponse{
		Success:        true,
		EntriesCleared: safeconv.Uint64ToInt32(serviceResp.EntriesCleared),
	}, nil
}

// GetCacheConfig returns the cache configuration.
func (s *Server) GetCacheConfig(ctx context.Context, req *pb.GetCacheConfigRequest) (*pb.GetCacheConfigResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	// Call service layer
	serviceReq := &service.GetCacheConfigRequest{}

	serviceResp, err := s.service.GetCacheConfig(ctx, serviceReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	return &pb.GetCacheConfigResponse{
		Enabled:                serviceResp.Enabled,
		L1Size:                 safeconv.IntToInt32(serviceResp.L1Size),
		L1TtlSeconds:           safeconv.Float64ToInt64(serviceResp.L1TTL.Seconds()),
		CleanupIntervalSeconds: safeconv.Float64ToInt64(serviceResp.CleanupInterval.Seconds()),
	}, nil
}

// UpdateCacheConfig updates the cache configuration.
func (s *Server) UpdateCacheConfig(ctx context.Context, req *pb.UpdateCacheConfigRequest) (*pb.UpdateCacheConfigResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	// Call service layer
	serviceReq := &service.UpdateCacheConfigRequest{}

	// Only set values if they are non-zero in the request
	if req.L1Size > 0 {
		l1Size := int(req.L1Size)
		serviceReq.L1Size = &l1Size
	}

	if req.L1TtlSeconds > 0 {
		ttl := time.Duration(req.L1TtlSeconds) * time.Second
		serviceReq.L1TTL = &ttl
	}

	serviceResp, err := s.service.UpdateCacheConfig(ctx, serviceReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	msg := "Cache configuration unchanged"
	if serviceResp.Updated {
		msg = "Cache configuration updated successfully"
	}

	return &pb.UpdateCacheConfigResponse{
		Success: serviceResp.Updated,
		Message: msg,
	}, nil
}
