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
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// createTestServerForTransactions creates a server with mock service for transaction tests.
func createTestServerForTransactions(t *testing.T) (*Server, *testutil.MockQuicBaaS) {
	t.Helper()

	logConfig := logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "grpc-txn-test",
	}
	log, err := logger.New(logConfig)
	require.NoError(t, err)

	mockSvc := testutil.NewMockQuicBaaS()

	srv, err := NewServer(mockSvc, log, 1, "test-v1.0.0")
	require.NoError(t, err)

	return srv, mockSvc
}

func TestTransaction_CompareVersion(t *testing.T) {
	srv, mockSvc := createTestServerForTransactions(t)

	mockSvc.On("Transaction", mock.Anything, mock.MatchedBy(func(req *service.TransactionRequest) bool {
		return len(req.Compare) == 1 &&
			req.Compare[0].Target == service.CompareTargetVersion &&
			req.Compare[0].Version == 1
	})).Return(&service.TransactionResponse{
		Succeeded: true,
		Responses: []*service.TransactionOpResponse{
			{
				OpType:     service.TransactionOpTypePut,
				EntityType: "user",
				ID:         42,
				Success:    true,
				Version:    2,
				Timestamp:  time.Now().UnixNano(),
			},
		},
		Timestamp: time.Now().UnixNano(),
		Message:   "Transaction succeeded",
		Revision:  1,
	}, nil).Once()

	resp, err := srv.Transaction(context.Background(), &pb.TransactionRequest{
		Compare: []*pb.CompareCondition{
			{
				EntityType: "user",
				Id:         42,
				Target:     pb.CompareTarget_COMPARE_TARGET_VERSION,
				Operator:   pb.CompareOperator_COMPARE_OPERATOR_EQUAL,
				Version:    1,
			},
		},
		Success: []*pb.TransactionOp{
			{
				OpType:     pb.TransactionOpType_TRANSACTION_OP_TYPE_PUT,
				EntityType: "user",
				Id:         42,
				Data:       []byte(`{"name":"updated"}`),
			},
		},
	})

	require.NoError(t, err)
	assert.True(t, resp.Succeeded)
	assert.Len(t, resp.Responses, 1)
	assert.Equal(t, pb.TransactionOpType_TRANSACTION_OP_TYPE_PUT, resp.Responses[0].OpType)
	mockSvc.AssertExpectations(t)
}

func TestTransaction_CompareValue(t *testing.T) {
	srv, mockSvc := createTestServerForTransactions(t)

	mockSvc.On("Transaction", mock.Anything, mock.MatchedBy(func(req *service.TransactionRequest) bool {
		return len(req.Compare) == 1 &&
			req.Compare[0].Target == service.CompareTargetValue
	})).Return(&service.TransactionResponse{
		Succeeded: true,
		Responses: []*service.TransactionOpResponse{
			{
				OpType:     service.TransactionOpTypePut,
				EntityType: "user",
				ID:         43,
				Success:    true,
				Version:    1,
				Timestamp:  time.Now().UnixNano(),
			},
		},
		Timestamp: time.Now().UnixNano(),
		Message:   "Transaction succeeded",
		Revision:  1,
	}, nil).Once()

	resp, err := srv.Transaction(context.Background(), &pb.TransactionRequest{
		Compare: []*pb.CompareCondition{
			{
				EntityType: "user",
				Id:         43,
				Target:     pb.CompareTarget_COMPARE_TARGET_VALUE,
				Operator:   pb.CompareOperator_COMPARE_OPERATOR_EQUAL,
				Value:      []byte(`{"status":"active"}`),
			},
		},
		Success: []*pb.TransactionOp{
			{
				OpType:     pb.TransactionOpType_TRANSACTION_OP_TYPE_PUT,
				EntityType: "user",
				Id:         43,
				Data:       []byte(`{"status":"inactive"}`),
			},
		},
	})

	require.NoError(t, err)
	assert.True(t, resp.Succeeded)
	mockSvc.AssertExpectations(t)
}

func TestTransaction_CompareExists(t *testing.T) {
	srv, mockSvc := createTestServerForTransactions(t)

	mockSvc.On("Transaction", mock.Anything, mock.MatchedBy(func(req *service.TransactionRequest) bool {
		return len(req.Compare) == 1 &&
			req.Compare[0].Target == service.CompareTargetExists
	})).Return(&service.TransactionResponse{
		Succeeded: true,
		Responses: []*service.TransactionOpResponse{
			{
				OpType:     service.TransactionOpTypeDelete,
				EntityType: "user",
				ID:         44,
				Success:    true,
				Timestamp:  time.Now().UnixNano(),
			},
		},
		Timestamp: time.Now().UnixNano(),
		Message:   "Transaction succeeded",
		Revision:  1,
	}, nil).Once()

	resp, err := srv.Transaction(context.Background(), &pb.TransactionRequest{
		Compare: []*pb.CompareCondition{
			{
				EntityType: "user",
				Id:         44,
				Target:     pb.CompareTarget_COMPARE_TARGET_EXISTS,
				Operator:   pb.CompareOperator_COMPARE_OPERATOR_EQUAL,
			},
		},
		Success: []*pb.TransactionOp{
			{
				OpType:     pb.TransactionOpType_TRANSACTION_OP_TYPE_DELETE,
				EntityType: "user",
				Id:         44,
			},
		},
	})

	require.NoError(t, err)
	assert.True(t, resp.Succeeded)
	mockSvc.AssertExpectations(t)
}

func TestTransaction_Failed(t *testing.T) {
	srv, mockSvc := createTestServerForTransactions(t)

	mockSvc.On("Transaction", mock.Anything, mock.Anything).
		Return(&service.TransactionResponse{
			Succeeded: false,
			Responses: []*service.TransactionOpResponse{
				{
					OpType:     service.TransactionOpTypeGet,
					EntityType: "user",
					ID:         45,
					Success:    true,
					Data:       []byte(`{"name":"alice"}`),
					Timestamp:  time.Now().UnixNano(),
				},
			},
			Timestamp: time.Now().UnixNano(),
			Message:   "Transaction failed: version mismatch",
			Revision:  1,
		}, nil).Once()

	resp, err := srv.Transaction(context.Background(), &pb.TransactionRequest{
		Compare: []*pb.CompareCondition{
			{
				EntityType: "user",
				Id:         45,
				Target:     pb.CompareTarget_COMPARE_TARGET_VERSION,
				Operator:   pb.CompareOperator_COMPARE_OPERATOR_EQUAL,
				Version:    999, // Wrong version
			},
		},
		Success: []*pb.TransactionOp{
			{
				OpType:     pb.TransactionOpType_TRANSACTION_OP_TYPE_PUT,
				EntityType: "user",
				Id:         45,
				Data:       []byte(`{"name":"updated"}`),
			},
		},
		Failure: []*pb.TransactionOp{
			{
				OpType:     pb.TransactionOpType_TRANSACTION_OP_TYPE_GET,
				EntityType: "user",
				Id:         45,
			},
		},
	})

	require.NoError(t, err)
	assert.False(t, resp.Succeeded)
	assert.Len(t, resp.Responses, 1)
	assert.Equal(t, pb.TransactionOpType_TRANSACTION_OP_TYPE_GET, resp.Responses[0].OpType)
	mockSvc.AssertExpectations(t)
}

func TestTransaction_MultipleConditions(t *testing.T) {
	srv, mockSvc := createTestServerForTransactions(t)

	mockSvc.On("Transaction", mock.Anything, mock.MatchedBy(func(req *service.TransactionRequest) bool {
		return len(req.Compare) == 2
	})).Return(&service.TransactionResponse{
		Succeeded: true,
		Responses: []*service.TransactionOpResponse{
			{
				OpType:     service.TransactionOpTypePut,
				EntityType: "user",
				ID:         46,
				Success:    true,
				Version:    2,
				Timestamp:  time.Now().UnixNano(),
			},
			{
				OpType:     service.TransactionOpTypePut,
				EntityType: "user",
				ID:         47,
				Success:    true,
				Version:    2,
				Timestamp:  time.Now().UnixNano(),
			},
		},
		Timestamp: time.Now().UnixNano(),
		Message:   "Transaction succeeded",
		Revision:  1,
	}, nil).Once()

	resp, err := srv.Transaction(context.Background(), &pb.TransactionRequest{
		Compare: []*pb.CompareCondition{
			{
				EntityType: "user",
				Id:         46,
				Target:     pb.CompareTarget_COMPARE_TARGET_VERSION,
				Operator:   pb.CompareOperator_COMPARE_OPERATOR_EQUAL,
				Version:    1,
			},
			{
				EntityType: "user",
				Id:         47,
				Target:     pb.CompareTarget_COMPARE_TARGET_VERSION,
				Operator:   pb.CompareOperator_COMPARE_OPERATOR_EQUAL,
				Version:    1,
			},
		},
		Success: []*pb.TransactionOp{
			{
				OpType:     pb.TransactionOpType_TRANSACTION_OP_TYPE_PUT,
				EntityType: "user",
				Id:         46,
				Data:       []byte(`{"name":"updated1"}`),
			},
			{
				OpType:     pb.TransactionOpType_TRANSACTION_OP_TYPE_PUT,
				EntityType: "user",
				Id:         47,
				Data:       []byte(`{"name":"updated2"}`),
			},
		},
	})

	require.NoError(t, err)
	assert.True(t, resp.Succeeded)
	assert.Len(t, resp.Responses, 2)
	mockSvc.AssertExpectations(t)
}

func TestTransaction_InvalidRequest(t *testing.T) {
	srv, _ := createTestServerForTransactions(t)

	// Empty compare conditions
	_, err := srv.Transaction(context.Background(), &pb.TransactionRequest{
		Compare: []*pb.CompareCondition{},
		Success: []*pb.TransactionOp{},
	})
	assert.Error(t, err)
}

func TestTransaction_MixedOperations(t *testing.T) {
	srv, mockSvc := createTestServerForTransactions(t)

	mockSvc.On("Transaction", mock.Anything, mock.Anything).
		Return(&service.TransactionResponse{
			Succeeded: true,
			Responses: []*service.TransactionOpResponse{
				{
					OpType:     service.TransactionOpTypePut,
					EntityType: "user",
					ID:         48,
					Success:    true,
					Version:    2,
					Timestamp:  time.Now().UnixNano(),
				},
				{
					OpType:     service.TransactionOpTypeDelete,
					EntityType: "user",
					ID:         49,
					Success:    true,
					Timestamp:  time.Now().UnixNano(),
				},
			},
			Timestamp: time.Now().UnixNano(),
			Message:   "Transaction succeeded",
			Revision:  1,
		}, nil).Once()

	resp, err := srv.Transaction(context.Background(), &pb.TransactionRequest{
		Compare: []*pb.CompareCondition{
			{
				EntityType: "user",
				Id:         48,
				Target:     pb.CompareTarget_COMPARE_TARGET_EXISTS,
				Operator:   pb.CompareOperator_COMPARE_OPERATOR_EQUAL,
			},
		},
		Success: []*pb.TransactionOp{
			{
				OpType:     pb.TransactionOpType_TRANSACTION_OP_TYPE_PUT,
				EntityType: "user",
				Id:         48,
				Data:       []byte(`{"name":"updated"}`),
			},
			{
				OpType:     pb.TransactionOpType_TRANSACTION_OP_TYPE_DELETE,
				EntityType: "user",
				Id:         49,
			},
		},
	})

	require.NoError(t, err)
	assert.True(t, resp.Succeeded)
	assert.Len(t, resp.Responses, 2)
	assert.Equal(t, pb.TransactionOpType_TRANSACTION_OP_TYPE_PUT, resp.Responses[0].OpType)
	assert.Equal(t, pb.TransactionOpType_TRANSACTION_OP_TYPE_DELETE, resp.Responses[1].OpType)
	mockSvc.AssertExpectations(t)
}
