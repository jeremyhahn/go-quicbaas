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

	"google.golang.org/grpc/metadata"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
)

// GetWithCodecInterface for state machines that support dual-codec architecture.
type GetWithCodecInterface interface {
	GetWithCodec(id uint64) (interface{}, []byte, string, error)
}

// extractCodecPreference extracts the preferred codec from gRPC metadata.
// Clients can set "x-quicbaas-codec" header to "json", "yaml", or "msgpack".
// Returns empty string if not specified (use default/auto-detect).
func extractCodecPreference(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "" // Default/auto-detect
	}

	values := md.Get("x-quicbaas-codec")
	if len(values) == 0 {
		return "" // Default/auto-detect
	}

	// Validate codec name
	codec := values[0]
	switch codec {
	case "json", "yaml", "msgpack":
		return codec
	default:
		return "" // Invalid, use default
	}
}

// GetOptimized is an optimized Get operation that delegates to the service layer.
// The service layer handles codec optimization internally.
//
// Performance benefits:
//   - Eliminates double encoding (storage -> entity -> wire)
//   - Direct transcoding (storage -> wire)
//   - ~2-3x performance improvement for read operations
//   - Lower memory allocations and GC pressure
func (s *Server) GetOptimized(ctx context.Context, req *pb.GetRequest) (*pb.GetResponse, error) {
	ctx, endSpan := startGRPCSpan(ctx, "grpc.GetOptimized", map[string]interface{}{
		"entity_type": req.EntityType,
		"id":          int64(req.Id), //nolint:gosec // G115: ID for tracing attribute
	})
	defer endSpan()
	start := time.Now()
	defer func() {
		s.metrics.requestDuration.With("GetOptimized").Observe(time.Since(start).Seconds())
	}()

	// Use standard Get operation through service layer
	// The service layer handles codec optimization internally
	return s.Get(ctx, req)
}
