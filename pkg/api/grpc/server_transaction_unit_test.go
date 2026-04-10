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

//go:build grpc

package grpc

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// createTransactionTestServer creates a server with mock service for Transaction tests.
func createTransactionTestServer(t *testing.T) (*Server, *testutil.MockQuicBaaS) {
	t.Helper()

	logConfig := logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "grpc-test",
	}
	log, err := logger.New(logConfig)
	require.NoError(t, err)

	mockSvc := testutil.NewMockQuicBaaS()

	srv, err := NewServer(mockSvc, log, 1, "test-v1.0.0")
	require.NoError(t, err)
	return srv, mockSvc
}

// TestTransaction_ValidationErrors tests Transaction request validation.
func TestTransaction_ValidationErrors(t *testing.T) {
	srv, _ := createTransactionTestServer(t)
	ctx := context.Background()

	t.Run("empty compare with no operations", func(t *testing.T) {
		req := &pb.TransactionRequest{
			Compare: []*pb.CompareCondition{},
			Success: []*pb.TransactionOp{},
			Failure: []*pb.TransactionOp{},
		}

		_, err := srv.Transaction(ctx, req)

		assert.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("invalid compare condition - empty entity type", func(t *testing.T) {
		req := &pb.TransactionRequest{
			Compare: []*pb.CompareCondition{
				{
					EntityType: "",
					Id:         1,
					Target:     pb.CompareTarget_COMPARE_TARGET_VERSION,
					Operator:   pb.CompareOperator_COMPARE_OPERATOR_EQUAL,
					Version:    1,
				},
			},
			Success: []*pb.TransactionOp{
				{
					OpType:     pb.TransactionOpType_TRANSACTION_OP_TYPE_PUT,
					EntityType: "user",
					Id:         1,
					Data:       []byte(`{"name":"test"}`),
				},
			},
		}

		_, err := srv.Transaction(ctx, req)

		assert.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("invalid success operation - empty entity type", func(t *testing.T) {
		req := &pb.TransactionRequest{
			Compare: []*pb.CompareCondition{
				{
					EntityType: "user",
					Id:         1,
					Target:     pb.CompareTarget_COMPARE_TARGET_VERSION,
					Operator:   pb.CompareOperator_COMPARE_OPERATOR_EQUAL,
					Version:    1,
				},
			},
			Success: []*pb.TransactionOp{
				{
					OpType:     pb.TransactionOpType_TRANSACTION_OP_TYPE_PUT,
					EntityType: "",
					Id:         1,
					Data:       []byte(`{"name":"test"}`),
				},
			},
		}

		_, err := srv.Transaction(ctx, req)

		assert.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("invalid failure operation - empty entity type", func(t *testing.T) {
		req := &pb.TransactionRequest{
			Compare: []*pb.CompareCondition{
				{
					EntityType: "user",
					Id:         1,
					Target:     pb.CompareTarget_COMPARE_TARGET_VERSION,
					Operator:   pb.CompareOperator_COMPARE_OPERATOR_EQUAL,
					Version:    1,
				},
			},
			Success: []*pb.TransactionOp{
				{
					OpType:     pb.TransactionOpType_TRANSACTION_OP_TYPE_PUT,
					EntityType: "user",
					Id:         1,
					Data:       []byte(`{"name":"test"}`),
				},
			},
			Failure: []*pb.TransactionOp{
				{
					OpType:     pb.TransactionOpType_TRANSACTION_OP_TYPE_PUT,
					EntityType: "",
					Id:         1,
					Data:       []byte(`{"name":"fallback"}`),
				},
			},
		}

		_, err := srv.Transaction(ctx, req)

		assert.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})
}

// TestTransaction_SuccessfulExecution tests successful transaction operations.
func TestTransaction_SuccessfulExecution(t *testing.T) {
	srv, mockSvc := createTransactionTestServer(t)
	ctx := context.Background()

	t.Run("successful transaction with success branch", func(t *testing.T) {
		req := &pb.TransactionRequest{
			Compare: []*pb.CompareCondition{
				{
					EntityType: "user",
					Id:         1,
					Target:     pb.CompareTarget_COMPARE_TARGET_VERSION,
					Operator:   pb.CompareOperator_COMPARE_OPERATOR_EQUAL,
					Version:    1,
				},
			},
			Success: []*pb.TransactionOp{
				{
					OpType:     pb.TransactionOpType_TRANSACTION_OP_TYPE_PUT,
					EntityType: "user",
					Id:         1,
					Data:       []byte(`{"name":"updated"}`),
				},
			},
		}

		mockSvc.On("Transaction", mock.Anything, mock.MatchedBy(func(req *service.TransactionRequest) bool {
			return len(req.Compare) == 1 && len(req.Success) == 1
		})).Return(&service.TransactionResponse{
			Succeeded: true,
			Responses: []*service.TransactionOpResponse{
				{
					OpType:  service.TransactionOpTypePut,
					ID:      1,
					Version: 2,
					Success: true,
				},
			},
		}, nil).Once()

		resp, err := srv.Transaction(ctx, req)

		assert.NoError(t, err)
		assert.True(t, resp.Succeeded)
		assert.Len(t, resp.Responses, 1)
		assert.Equal(t, uint64(2), resp.Responses[0].Version)
		mockSvc.AssertExpectations(t)
	})

	t.Run("transaction with failure branch executed", func(t *testing.T) {
		req := &pb.TransactionRequest{
			Compare: []*pb.CompareCondition{
				{
					EntityType: "user",
					Id:         1,
					Target:     pb.CompareTarget_COMPARE_TARGET_VERSION,
					Operator:   pb.CompareOperator_COMPARE_OPERATOR_EQUAL,
					Version:    99, // Version mismatch will cause failure branch
				},
			},
			Success: []*pb.TransactionOp{
				{
					OpType:     pb.TransactionOpType_TRANSACTION_OP_TYPE_PUT,
					EntityType: "user",
					Id:         1,
					Data:       []byte(`{"name":"success"}`),
				},
			},
			Failure: []*pb.TransactionOp{
				{
					OpType:     pb.TransactionOpType_TRANSACTION_OP_TYPE_PUT,
					EntityType: "user",
					Id:         1,
					Data:       []byte(`{"name":"failure"}`),
				},
			},
		}

		mockSvc.On("Transaction", mock.Anything, mock.MatchedBy(func(req *service.TransactionRequest) bool {
			return len(req.Compare) == 1 && len(req.Success) == 1 && len(req.Failure) == 1
		})).Return(&service.TransactionResponse{
			Succeeded: false,
			Responses: []*service.TransactionOpResponse{
				{
					OpType:  service.TransactionOpTypePut,
					ID:      1,
					Version: 1,
					Success: true,
				},
			},
		}, nil).Once()

		resp, err := srv.Transaction(ctx, req)

		assert.NoError(t, err)
		assert.False(t, resp.Succeeded)
		assert.Len(t, resp.Responses, 1)
		mockSvc.AssertExpectations(t)
	})

	t.Run("transaction with delete operation", func(t *testing.T) {
		req := &pb.TransactionRequest{
			Compare: []*pb.CompareCondition{
				{
					EntityType: "user",
					Id:         1,
					Target:     pb.CompareTarget_COMPARE_TARGET_EXISTS,
					Operator:   pb.CompareOperator_COMPARE_OPERATOR_EQUAL,
					Version:    1, // exists check uses version field as bool
				},
			},
			Success: []*pb.TransactionOp{
				{
					OpType:     pb.TransactionOpType_TRANSACTION_OP_TYPE_DELETE,
					EntityType: "user",
					Id:         1,
				},
			},
		}

		mockSvc.On("Transaction", mock.Anything, mock.MatchedBy(func(req *service.TransactionRequest) bool {
			return len(req.Success) == 1 && req.Success[0].OpType == service.TransactionOpTypeDelete
		})).Return(&service.TransactionResponse{
			Succeeded: true,
			Responses: []*service.TransactionOpResponse{
				{
					OpType:  service.TransactionOpTypeDelete,
					ID:      1,
					Success: true,
				},
			},
		}, nil).Once()

		resp, err := srv.Transaction(ctx, req)

		assert.NoError(t, err)
		assert.True(t, resp.Succeeded)
		mockSvc.AssertExpectations(t)
	})
}

// TestTransaction_ServiceError tests Transaction handling of service errors.
func TestTransaction_ServiceError_Extended(t *testing.T) {
	srv, mockSvc := createTransactionTestServer(t)
	ctx := context.Background()

	t.Run("service returns error", func(t *testing.T) {
		req := &pb.TransactionRequest{
			Compare: []*pb.CompareCondition{
				{
					EntityType: "user",
					Id:         1,
					Target:     pb.CompareTarget_COMPARE_TARGET_VERSION,
					Operator:   pb.CompareOperator_COMPARE_OPERATOR_EQUAL,
					Version:    1,
				},
			},
			Success: []*pb.TransactionOp{
				{
					OpType:     pb.TransactionOpType_TRANSACTION_OP_TYPE_PUT,
					EntityType: "user",
					Id:         1,
					Data:       []byte(`{"name":"test"}`),
				},
			},
		}

		mockSvc.On("Transaction", mock.Anything, mock.Anything).
			Return(nil, assert.AnError).Once()

		_, err := srv.Transaction(ctx, req)

		assert.Error(t, err)
		mockSvc.AssertExpectations(t)
	})
}

// TestTransaction_CompareTargets tests different compare target types.
func TestTransaction_CompareTargets(t *testing.T) {
	srv, mockSvc := createTransactionTestServer(t)
	ctx := context.Background()

	targets := []struct {
		name    string
		target  pb.CompareTarget
		version uint64
		value   []byte
	}{
		{"version target", pb.CompareTarget_COMPARE_TARGET_VERSION, 1, nil},
		{"value target", pb.CompareTarget_COMPARE_TARGET_VALUE, 0, []byte(`{"field":"expected"}`)},
		{"exists target", pb.CompareTarget_COMPARE_TARGET_EXISTS, 1, nil},
		{"create target", pb.CompareTarget_COMPARE_TARGET_CREATE, 1000, nil},
		{"mod target", pb.CompareTarget_COMPARE_TARGET_MOD, 2000, nil},
	}

	for _, tt := range targets {
		t.Run(tt.name, func(t *testing.T) {
			req := &pb.TransactionRequest{
				Compare: []*pb.CompareCondition{
					{
						EntityType: "user",
						Id:         1,
						Target:     tt.target,
						Operator:   pb.CompareOperator_COMPARE_OPERATOR_EQUAL,
						Version:    tt.version,
						Value:      tt.value,
					},
				},
				Success: []*pb.TransactionOp{
					{
						OpType:     pb.TransactionOpType_TRANSACTION_OP_TYPE_PUT,
						EntityType: "user",
						Id:         1,
						Data:       []byte(`{"test":"data"}`),
					},
				},
			}

			mockSvc.On("Transaction", mock.Anything, mock.Anything).
				Return(&service.TransactionResponse{
					Succeeded: true,
					Responses: []*service.TransactionOpResponse{},
				}, nil).Once()

			resp, err := srv.Transaction(ctx, req)

			assert.NoError(t, err)
			assert.True(t, resp.Succeeded)
			mockSvc.AssertExpectations(t)
		})
	}
}

// TestTransaction_CompareOperators tests different compare operator types.
func TestTransaction_CompareOperators(t *testing.T) {
	srv, mockSvc := createTransactionTestServer(t)
	ctx := context.Background()

	operators := []struct {
		name string
		op   pb.CompareOperator
	}{
		{"equal operator", pb.CompareOperator_COMPARE_OPERATOR_EQUAL},
		{"not equal operator", pb.CompareOperator_COMPARE_OPERATOR_NOT_EQUAL},
		{"greater operator", pb.CompareOperator_COMPARE_OPERATOR_GREATER},
		{"less operator", pb.CompareOperator_COMPARE_OPERATOR_LESS},
	}

	for _, tt := range operators {
		t.Run(tt.name, func(t *testing.T) {
			req := &pb.TransactionRequest{
				Compare: []*pb.CompareCondition{
					{
						EntityType: "user",
						Id:         1,
						Target:     pb.CompareTarget_COMPARE_TARGET_VERSION,
						Operator:   tt.op,
						Version:    1,
					},
				},
				Success: []*pb.TransactionOp{
					{
						OpType:     pb.TransactionOpType_TRANSACTION_OP_TYPE_PUT,
						EntityType: "user",
						Id:         1,
						Data:       []byte(`{"test":"data"}`),
					},
				},
			}

			mockSvc.On("Transaction", mock.Anything, mock.Anything).
				Return(&service.TransactionResponse{
					Succeeded: true,
					Responses: []*service.TransactionOpResponse{},
				}, nil).Once()

			resp, err := srv.Transaction(ctx, req)

			assert.NoError(t, err)
			assert.True(t, resp.Succeeded)
			mockSvc.AssertExpectations(t)
		})
	}
}
