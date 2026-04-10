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

//go:build !timeseries && !integration

package grpc

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// =============================================================================
// Server Timeseries - Nil Request / Validation Tests (without timeseries tag)
// Without the timeseries build tag, all TS* methods are handled by the
// protobuf UnimplementedQuicBaaSServiceServer stub, which always returns
// codes.Unimplemented regardless of input.
// =============================================================================

func TestGRPCRemaining91_ServerTimeseries_NilRequests(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	srv := &Server{logger: log, service: nil}
	ctx := context.Background()

	t.Run("TSWritePoint_Nil", func(t *testing.T) {
		_, err := srv.TSWritePoint(ctx, nil)
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("TSWriteBatch_Nil", func(t *testing.T) {
		_, err := srv.TSWriteBatch(ctx, nil)
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("TSQuery_Nil", func(t *testing.T) {
		_, err := srv.TSQuery(ctx, nil)
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("TSDeleteSeries_Nil", func(t *testing.T) {
		_, err := srv.TSDeleteSeries(ctx, nil)
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("TSGetRetentionPolicy_Nil", func(t *testing.T) {
		_, err := srv.TSGetRetentionPolicy(ctx, nil)
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("TSSetRetentionPolicy_Nil", func(t *testing.T) {
		_, err := srv.TSSetRetentionPolicy(ctx, nil)
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("TSListMetrics_Nil", func(t *testing.T) {
		_, err := srv.TSListMetrics(ctx, nil)
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})
}

func TestGRPCRemaining91_ServerTimeseries_ValidationErrors(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	srv := &Server{logger: log, service: nil}
	ctx := context.Background()

	t.Run("TSWritePoint_EmptyMetric", func(t *testing.T) {
		_, err := srv.TSWritePoint(ctx, &pb.TSWritePointRequest{Metric: ""})
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("TSWriteBatch_EmptyPoints", func(t *testing.T) {
		_, err := srv.TSWriteBatch(ctx, &pb.TSWriteBatchRequest{Points: nil})
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("TSQuery_EmptyMetric", func(t *testing.T) {
		_, err := srv.TSQuery(ctx, &pb.TSQueryRequest{Metric: ""})
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("TSQuery_NoStartTime", func(t *testing.T) {
		_, err := srv.TSQuery(ctx, &pb.TSQueryRequest{Metric: "m", StartTime: 0})
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("TSQuery_NoEndTime", func(t *testing.T) {
		_, err := srv.TSQuery(ctx, &pb.TSQueryRequest{Metric: "m", StartTime: 1, EndTime: 0})
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("TSDeleteSeries_EmptyMetric", func(t *testing.T) {
		_, err := srv.TSDeleteSeries(ctx, &pb.TSDeleteSeriesRequest{Metric: ""})
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("TSGetRetentionPolicy_EmptyMetric", func(t *testing.T) {
		_, err := srv.TSGetRetentionPolicy(ctx, &pb.TSGetRetentionPolicyRequest{Metric: ""})
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("TSSetRetentionPolicy_EmptyMetric", func(t *testing.T) {
		_, err := srv.TSSetRetentionPolicy(ctx, &pb.TSSetRetentionPolicyRequest{Metric: ""})
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("TSSetRetentionPolicy_NilPolicy", func(t *testing.T) {
		_, err := srv.TSSetRetentionPolicy(ctx, &pb.TSSetRetentionPolicyRequest{Metric: "m", Policy: nil})
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})
}
