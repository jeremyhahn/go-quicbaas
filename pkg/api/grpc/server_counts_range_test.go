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
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"google.golang.org/grpc/codes"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// TestCountHandlers_ResponseOutOfRange proves that a rebalance shard count,
// job total, deleted-parameter count or ceremony share index too large for
// its int32 wire field becomes codes.Internal instead of a wrapped integer.
func TestCountHandlers_ResponseOutOfRange(t *testing.T) {
	const tooBig = math.MaxInt32 + 1
	ctx := context.Background()

	rebalanceProgress := func(t *testing.T, resp *service.GetRebalanceProgressResponse) error {
		srv, mockSvc := createTestServerForOperations(t)
		mockSvc.On("GetRebalanceProgress", mock.Anything, mock.Anything).Return(resp, nil).Once()
		_, err := srv.GetRebalanceProgress(ctx, &pb.GetRebalanceProgressRequest{OperationId: "op1"})
		return err
	}

	tests := []struct {
		name  string
		field string
		call  func(t *testing.T) error
	}{
		{
			name:  "GetRebalanceProgress shards moved",
			field: "shards_moved",
			call: func(t *testing.T) error {
				return rebalanceProgress(t, &service.GetRebalanceProgressResponse{ShardsMoved: tooBig, TotalShards: 3})
			},
		},
		{
			name:  "GetRebalanceProgress total shards",
			field: "total_shards",
			call: func(t *testing.T) error {
				return rebalanceProgress(t, &service.GetRebalanceProgressResponse{ShardsMoved: 1, TotalShards: tooBig})
			},
		},
		{
			name:  "Scheduler ListJobs total count",
			field: "total_count",
			call: func(*testing.T) error {
				srv := newTestSchedulerServer(&mockSchedulerService{
					listJobsFunc: func(context.Context, *service.ListSchedulerJobsRequest) (*service.ListSchedulerJobsResponse, error) {
						return &service.ListSchedulerJobsResponse{TotalCount: tooBig, Timestamp: time.Now()}, nil
					},
				})
				_, err := srv.ListJobs(ctx, &pb.ListJobsRequest{})
				return err
			},
		},
		{
			name:  "DeleteParameters deleted count",
			field: "deleted_count",
			call: func(t *testing.T) error {
				srv := newTestParameterStoreServer(t, &mockParameterStoreService{
					deleteParametersFunc: func(context.Context, *interfaces.DeleteParametersRequest) (*interfaces.DeleteParametersResponse, error) {
						return &interfaces.DeleteParametersResponse{DeletedCount: tooBig, Timestamp: time.Now()}, nil
					},
				})
				_, err := srv.DeleteParameters(ctx, &pb.DeleteParametersRequest{Paths: []string{"/app/key1"}})
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requireRangeStatus(t, tt.call(t), codes.Internal, tt.field)
		})
	}
}
