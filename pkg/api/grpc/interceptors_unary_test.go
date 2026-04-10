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
	"testing"

	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

func TestUnaryRequestIDInterceptor_WithIncomingID(t *testing.T) {
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("x-request-id", "abc"))
	handler := func(ctx context.Context, req interface{}) (interface{}, error) { return nil, nil }
	_, err := UnaryRequestIDInterceptor()(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/test"}, handler)
	assert.NoError(t, err)
}

func TestUnaryMetricsInterceptor_Basic(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	// compose chain to include metrics
	interceptor := UnaryMetricsInterceptor()
	handler := func(ctx context.Context, req interface{}) (interface{}, error) { return nil, nil }
	_, err := interceptor(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: "/svc/Method"}, handler)
	assert.NoError(t, err)
	_ = log // ensure import used in package
}

func TestUnaryMetricsInterceptor_WithStatusError(t *testing.T) {
	interceptor := UnaryMetricsInterceptor()
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return nil, status.Error(codes.NotFound, "missing")
	}
	_, err := interceptor(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: "/svc/Method2"}, handler)
	assert.Error(t, err)
}
