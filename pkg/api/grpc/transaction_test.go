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
	"testing"

	"github.com/stretchr/testify/assert"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// Test validateTransactionRequest.
func TestValidateTransactionRequest(t *testing.T) {
	tests := []struct {
		name        string
		req         *pb.TransactionRequest
		expectError bool
		errorMsg    string
	}{
		{
			name:        "nil request",
			req:         nil,
			expectError: true,
			errorMsg:    "request cannot be nil",
		},
		{
			name: "empty transaction - no operations at all",
			req: &pb.TransactionRequest{
				Compare: []*pb.CompareCondition{},
				Success: []*pb.TransactionOp{},
				Failure: []*pb.TransactionOp{},
			},
			expectError: true,
			errorMsg:    "at least one compare, success, or failure operation",
		},
		{
			name: "no success or failure operations",
			req: &pb.TransactionRequest{
				Compare: []*pb.CompareCondition{
					{
						EntityType: "user",
						Id:         123,
						Target:     pb.CompareTarget_COMPARE_TARGET_EXISTS,
						Operator:   pb.CompareOperator_COMPARE_OPERATOR_EQUAL,
					},
				},
				Success: []*pb.TransactionOp{},
				Failure: []*pb.TransactionOp{},
			},
			expectError: true,
			errorMsg:    "at least one success or failure operation",
		},
		{
			name: "valid transaction with success operations",
			req: &pb.TransactionRequest{
				Compare: []*pb.CompareCondition{
					{
						EntityType: "user",
						Id:         123,
						Target:     pb.CompareTarget_COMPARE_TARGET_EXISTS,
						Operator:   pb.CompareOperator_COMPARE_OPERATOR_EQUAL,
					},
				},
				Success: []*pb.TransactionOp{
					{
						EntityType: "user",
						Id:         123,
						OpType:     pb.TransactionOpType_TRANSACTION_OP_TYPE_PUT,
						Data:       []byte(`{"name":"test"}`),
					},
				},
			},
			expectError: false,
		},
		{
			name: "valid transaction with failure operations",
			req: &pb.TransactionRequest{
				Compare: []*pb.CompareCondition{
					{
						EntityType: "user",
						Id:         123,
						Target:     pb.CompareTarget_COMPARE_TARGET_EXISTS,
						Operator:   pb.CompareOperator_COMPARE_OPERATOR_NOT_EQUAL,
					},
				},
				Failure: []*pb.TransactionOp{
					{
						EntityType: "user",
						Id:         123,
						OpType:     pb.TransactionOpType_TRANSACTION_OP_TYPE_PUT,
						Data:       []byte(`{"name":"new"}`),
					},
				},
			},
			expectError: false,
		},
		{
			name: "valid transaction with both success and failure",
			req: &pb.TransactionRequest{
				Compare: []*pb.CompareCondition{
					{
						EntityType: "user",
						Id:         123,
						Target:     pb.CompareTarget_COMPARE_TARGET_VERSION,
						Operator:   pb.CompareOperator_COMPARE_OPERATOR_EQUAL,
						Version:    1,
					},
				},
				Success: []*pb.TransactionOp{
					{
						EntityType: "user",
						Id:         123,
						OpType:     pb.TransactionOpType_TRANSACTION_OP_TYPE_PUT,
						Data:       []byte(`{"name":"updated"}`),
					},
				},
				Failure: []*pb.TransactionOp{
					{
						EntityType: "user",
						Id:         123,
						OpType:     pb.TransactionOpType_TRANSACTION_OP_TYPE_GET,
					},
				},
			},
			expectError: false,
		},
		{
			name: "transaction with no compare conditions",
			req: &pb.TransactionRequest{
				Success: []*pb.TransactionOp{
					{
						EntityType: "user",
						Id:         123,
						OpType:     pb.TransactionOpType_TRANSACTION_OP_TYPE_PUT,
						Data:       []byte(`{"name":"test"}`),
					},
				},
			},
			expectError: false,
		},
		{
			name: "invalid consistency level",
			req: &pb.TransactionRequest{
				Success: []*pb.TransactionOp{
					{
						EntityType: "user",
						Id:         123,
						OpType:     pb.TransactionOpType_TRANSACTION_OP_TYPE_PUT,
						Data:       []byte(`{"name":"test"}`),
					},
				},
				Consistency: pb.ConsistencyLevel(999), // Invalid
			},
			expectError: true,
			errorMsg:    "invalid consistency level",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateTransactionRequest(tt.req)
			if tt.expectError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
				// Check it's a QBError with correct code
				var qbErr *errors.QBError
				assert.ErrorAs(t, err, &qbErr)
				assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// Test validateCompareCondition.
func TestValidateCompareCondition(t *testing.T) {
	tests := []struct {
		name        string
		cmp         *pb.CompareCondition
		index       int
		expectError bool
		errorMsg    string
	}{
		{
			name:        "nil condition",
			cmp:         nil,
			index:       0,
			expectError: true,
			errorMsg:    "item 0 compare condition cannot be nil",
		},
		{
			name: "empty entity type",
			cmp: &pb.CompareCondition{
				EntityType: "",
				Id:         123,
				Target:     pb.CompareTarget_COMPARE_TARGET_EXISTS,
				Operator:   pb.CompareOperator_COMPARE_OPERATOR_EQUAL,
			},
			index:       0,
			expectError: true,
			errorMsg:    "entity_type is required",
		},
		{
			name: "zero id",
			cmp: &pb.CompareCondition{
				EntityType: "user",
				Id:         0,
				Target:     pb.CompareTarget_COMPARE_TARGET_EXISTS,
				Operator:   pb.CompareOperator_COMPARE_OPERATOR_EQUAL,
			},
			index:       0,
			expectError: true,
			errorMsg:    "id is required",
		},
		{
			name: "unspecified target",
			cmp: &pb.CompareCondition{
				EntityType: "user",
				Id:         123,
				Target:     pb.CompareTarget_COMPARE_TARGET_UNSPECIFIED,
				Operator:   pb.CompareOperator_COMPARE_OPERATOR_EQUAL,
			},
			index:       0,
			expectError: true,
			errorMsg:    "target is required",
		},
		{
			name: "unspecified operator",
			cmp: &pb.CompareCondition{
				EntityType: "user",
				Id:         123,
				Target:     pb.CompareTarget_COMPARE_TARGET_EXISTS,
				Operator:   pb.CompareOperator_COMPARE_OPERATOR_UNSPECIFIED,
			},
			index:       0,
			expectError: true,
			errorMsg:    "operator is required",
		},
		{
			name: "EXISTS with invalid operator GREATER",
			cmp: &pb.CompareCondition{
				EntityType: "user",
				Id:         123,
				Target:     pb.CompareTarget_COMPARE_TARGET_EXISTS,
				Operator:   pb.CompareOperator_COMPARE_OPERATOR_GREATER,
			},
			index:       0,
			expectError: true,
			errorMsg:    "EXISTS target only supports EQUAL and NOT_EQUAL",
		},
		{
			name: "VERSION without version value",
			cmp: &pb.CompareCondition{
				EntityType: "user",
				Id:         123,
				Target:     pb.CompareTarget_COMPARE_TARGET_VERSION,
				Operator:   pb.CompareOperator_COMPARE_OPERATOR_EQUAL,
				Version:    0,
			},
			index:       1,
			expectError: true,
			errorMsg:    "item 1 version is required for VERSION target",
		},
		{
			name: "VALUE without value data",
			cmp: &pb.CompareCondition{
				EntityType: "user",
				Id:         123,
				Target:     pb.CompareTarget_COMPARE_TARGET_VALUE,
				Operator:   pb.CompareOperator_COMPARE_OPERATOR_EQUAL,
				Value:      []byte{},
			},
			index:       2,
			expectError: true,
			errorMsg:    "item 2 value is required for VALUE target",
		},
		{
			name: "valid EXISTS comparison",
			cmp: &pb.CompareCondition{
				EntityType: "user",
				Id:         123,
				Target:     pb.CompareTarget_COMPARE_TARGET_EXISTS,
				Operator:   pb.CompareOperator_COMPARE_OPERATOR_EQUAL,
			},
			index:       0,
			expectError: false,
		},
		{
			name: "valid VERSION comparison",
			cmp: &pb.CompareCondition{
				EntityType: "user",
				Id:         123,
				Target:     pb.CompareTarget_COMPARE_TARGET_VERSION,
				Operator:   pb.CompareOperator_COMPARE_OPERATOR_GREATER_EQUAL,
				Version:    5,
			},
			index:       0,
			expectError: false,
		},
		{
			name: "valid VALUE comparison",
			cmp: &pb.CompareCondition{
				EntityType: "user",
				Id:         123,
				Target:     pb.CompareTarget_COMPARE_TARGET_VALUE,
				Operator:   pb.CompareOperator_COMPARE_OPERATOR_NOT_EQUAL,
				Value:      []byte(`{"status":"active"}`),
			},
			index:       0,
			expectError: false,
		},
		{
			name: "valid CREATE comparison",
			cmp: &pb.CompareCondition{
				EntityType: "user",
				Id:         123,
				Target:     pb.CompareTarget_COMPARE_TARGET_CREATE,
				Operator:   pb.CompareOperator_COMPARE_OPERATOR_LESS,
				Version:    1000,
			},
			index:       0,
			expectError: false,
		},
		{
			name: "valid MOD comparison",
			cmp: &pb.CompareCondition{
				EntityType: "user",
				Id:         123,
				Target:     pb.CompareTarget_COMPARE_TARGET_MOD,
				Operator:   pb.CompareOperator_COMPARE_OPERATOR_GREATER,
				Version:    2000,
			},
			index:       0,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateCompareCondition(tt.cmp, tt.index)
			if tt.expectError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// Test validateTransactionOp.
func TestValidateTransactionOp(t *testing.T) {
	tests := []struct {
		name        string
		op          *pb.TransactionOp
		index       int
		branch      string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "nil operation",
			op:          nil,
			index:       0,
			branch:      "success",
			expectError: true,
			errorMsg:    "item 0 success operation cannot be nil",
		},
		{
			name: "empty entity type",
			op: &pb.TransactionOp{
				EntityType: "",
				Id:         123,
				OpType:     pb.TransactionOpType_TRANSACTION_OP_TYPE_GET,
			},
			index:       0,
			branch:      "success",
			expectError: true,
			errorMsg:    "entity_type is required",
		},
		{
			name: "zero id",
			op: &pb.TransactionOp{
				EntityType: "user",
				Id:         0,
				OpType:     pb.TransactionOpType_TRANSACTION_OP_TYPE_GET,
			},
			index:       1,
			branch:      "failure",
			expectError: true,
			errorMsg:    "item 1 id is required in failure",
		},
		{
			name: "unspecified operation type",
			op: &pb.TransactionOp{
				EntityType: "user",
				Id:         123,
				OpType:     pb.TransactionOpType_TRANSACTION_OP_TYPE_UNSPECIFIED,
			},
			index:       0,
			branch:      "success",
			expectError: true,
			errorMsg:    "operation type is required",
		},
		{
			name: "PUT without data",
			op: &pb.TransactionOp{
				EntityType: "user",
				Id:         123,
				OpType:     pb.TransactionOpType_TRANSACTION_OP_TYPE_PUT,
				Data:       []byte{},
			},
			index:       2,
			branch:      "success",
			expectError: true,
			errorMsg:    "item 2 data is required for PUT operation in success",
		},
		{
			name: "GET with data (should not have)",
			op: &pb.TransactionOp{
				EntityType: "user",
				Id:         123,
				OpType:     pb.TransactionOpType_TRANSACTION_OP_TYPE_GET,
				Data:       []byte(`{"extra":"data"}`),
			},
			index:       0,
			branch:      "failure",
			expectError: true,
			errorMsg:    "GET operation should not have data",
		},
		{
			name: "DELETE with data (should not have)",
			op: &pb.TransactionOp{
				EntityType: "user",
				Id:         123,
				OpType:     pb.TransactionOpType_TRANSACTION_OP_TYPE_DELETE,
				Data:       []byte(`{"extra":"data"}`),
			},
			index:       1,
			branch:      "success",
			expectError: true,
			errorMsg:    "DELETE operation should not have data",
		},
		{
			name: "valid PUT operation",
			op: &pb.TransactionOp{
				EntityType: "user",
				Id:         123,
				OpType:     pb.TransactionOpType_TRANSACTION_OP_TYPE_PUT,
				Data:       []byte(`{"name":"John","email":"john@example.com"}`),
			},
			index:       0,
			branch:      "success",
			expectError: false,
		},
		{
			name: "valid GET operation",
			op: &pb.TransactionOp{
				EntityType: "user",
				Id:         456,
				OpType:     pb.TransactionOpType_TRANSACTION_OP_TYPE_GET,
			},
			index:       1,
			branch:      "failure",
			expectError: false,
		},
		{
			name: "valid DELETE operation",
			op: &pb.TransactionOp{
				EntityType: "user",
				Id:         789,
				OpType:     pb.TransactionOpType_TRANSACTION_OP_TYPE_DELETE,
			},
			index:       2,
			branch:      "success",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateTransactionOp(tt.op, tt.index, tt.branch)
			if tt.expectError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// Test complex transaction request validation.
func TestValidateTransactionRequest_ComplexScenarios(t *testing.T) {
	tests := []struct {
		name        string
		req         *pb.TransactionRequest
		expectError bool
		description string
	}{
		{
			name: "multi-compare multi-operation transaction",
			req: &pb.TransactionRequest{
				Compare: []*pb.CompareCondition{
					{
						EntityType: "user",
						Id:         123,
						Target:     pb.CompareTarget_COMPARE_TARGET_EXISTS,
						Operator:   pb.CompareOperator_COMPARE_OPERATOR_EQUAL,
					},
					{
						EntityType: "user",
						Id:         123,
						Target:     pb.CompareTarget_COMPARE_TARGET_VERSION,
						Operator:   pb.CompareOperator_COMPARE_OPERATOR_GREATER_EQUAL,
						Version:    5,
					},
				},
				Success: []*pb.TransactionOp{
					{
						EntityType: "user",
						Id:         123,
						OpType:     pb.TransactionOpType_TRANSACTION_OP_TYPE_PUT,
						Data:       []byte(`{"name":"updated"}`),
					},
					{
						EntityType: "profile",
						Id:         123,
						OpType:     pb.TransactionOpType_TRANSACTION_OP_TYPE_PUT,
						Data:       []byte(`{"bio":"new bio"}`),
					},
				},
				Failure: []*pb.TransactionOp{
					{
						EntityType: "user",
						Id:         123,
						OpType:     pb.TransactionOpType_TRANSACTION_OP_TYPE_GET,
					},
				},
				Consistency: pb.ConsistencyLevel_CONSISTENCY_LEVEL_LINEARIZABLE,
			},
			expectError: false,
			description: "Valid multi-entity transaction with multiple comparisons",
		},
		{
			name: "read-modify-write pattern",
			req: &pb.TransactionRequest{
				Success: []*pb.TransactionOp{
					{
						EntityType: "user",
						Id:         123,
						OpType:     pb.TransactionOpType_TRANSACTION_OP_TYPE_GET,
					},
					{
						EntityType: "user",
						Id:         123,
						OpType:     pb.TransactionOpType_TRANSACTION_OP_TYPE_PUT,
						Data:       []byte(`{"name":"modified"}`),
					},
				},
				Consistency: pb.ConsistencyLevel_CONSISTENCY_LEVEL_STRONG,
			},
			expectError: false,
			description: "Valid read-modify-write without comparisons",
		},
		{
			name: "optimistic locking pattern",
			req: &pb.TransactionRequest{
				Compare: []*pb.CompareCondition{
					{
						EntityType: "user",
						Id:         123,
						Target:     pb.CompareTarget_COMPARE_TARGET_VERSION,
						Operator:   pb.CompareOperator_COMPARE_OPERATOR_EQUAL,
						Version:    10,
					},
				},
				Success: []*pb.TransactionOp{
					{
						EntityType: "user",
						Id:         123,
						OpType:     pb.TransactionOpType_TRANSACTION_OP_TYPE_PUT,
						Data:       []byte(`{"name":"updated","version":11}`),
					},
				},
				Failure: []*pb.TransactionOp{
					{
						EntityType: "user",
						Id:         123,
						OpType:     pb.TransactionOpType_TRANSACTION_OP_TYPE_GET,
					},
				},
			},
			expectError: false,
			description: "Valid optimistic locking with version check",
		},
		{
			name: "create-if-not-exists pattern",
			req: &pb.TransactionRequest{
				Compare: []*pb.CompareCondition{
					{
						EntityType: "user",
						Id:         123,
						Target:     pb.CompareTarget_COMPARE_TARGET_EXISTS,
						Operator:   pb.CompareOperator_COMPARE_OPERATOR_NOT_EQUAL,
					},
				},
				Failure: []*pb.TransactionOp{
					{
						EntityType: "user",
						Id:         123,
						OpType:     pb.TransactionOpType_TRANSACTION_OP_TYPE_PUT,
						Data:       []byte(`{"name":"new user"}`),
					},
				},
			},
			expectError: false,
			description: "Valid create-if-not-exists using failure branch",
		},
		{
			name: "invalid - mixed valid and invalid operations",
			req: &pb.TransactionRequest{
				Success: []*pb.TransactionOp{
					{
						EntityType: "user",
						Id:         123,
						OpType:     pb.TransactionOpType_TRANSACTION_OP_TYPE_PUT,
						Data:       []byte(`{"name":"test"}`),
					},
					{
						EntityType: "user",
						Id:         456,
						OpType:     pb.TransactionOpType_TRANSACTION_OP_TYPE_PUT,
						Data:       []byte{}, // Invalid: empty data
					},
				},
			},
			expectError: true,
			description: "Should fail on second operation with empty data",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateTransactionRequest(tt.req)
			if tt.expectError {
				assert.Error(t, err, tt.description)
			} else {
				assert.NoError(t, err, tt.description)
			}
		})
	}
}

// Benchmark validation functions.
func BenchmarkValidateTransactionRequest(b *testing.B) {
	req := &pb.TransactionRequest{
		Compare: []*pb.CompareCondition{
			{
				EntityType: "user",
				Id:         123,
				Target:     pb.CompareTarget_COMPARE_TARGET_VERSION,
				Operator:   pb.CompareOperator_COMPARE_OPERATOR_EQUAL,
				Version:    1,
			},
		},
		Success: []*pb.TransactionOp{
			{
				EntityType: "user",
				Id:         123,
				OpType:     pb.TransactionOpType_TRANSACTION_OP_TYPE_PUT,
				Data:       []byte(`{"name":"test"}`),
			},
		},
		Failure: []*pb.TransactionOp{
			{
				EntityType: "user",
				Id:         123,
				OpType:     pb.TransactionOpType_TRANSACTION_OP_TYPE_GET,
			},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = validateTransactionRequest(req)
	}
}

func BenchmarkValidateCompareCondition(b *testing.B) {
	cmp := &pb.CompareCondition{
		EntityType: "user",
		Id:         123,
		Target:     pb.CompareTarget_COMPARE_TARGET_VERSION,
		Operator:   pb.CompareOperator_COMPARE_OPERATOR_EQUAL,
		Version:    1,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = validateCompareCondition(cmp, 0)
	}
}

func BenchmarkValidateTransactionOp(b *testing.B) {
	op := &pb.TransactionOp{
		EntityType: "user",
		Id:         123,
		OpType:     pb.TransactionOpType_TRANSACTION_OP_TYPE_PUT,
		Data:       []byte(`{"name":"test"}`),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = validateTransactionOp(op, 0, "success")
	}
}

func BenchmarkValidateComplexTransaction(b *testing.B) {
	req := &pb.TransactionRequest{
		Compare: []*pb.CompareCondition{
			{
				EntityType: "user",
				Id:         123,
				Target:     pb.CompareTarget_COMPARE_TARGET_EXISTS,
				Operator:   pb.CompareOperator_COMPARE_OPERATOR_EQUAL,
			},
			{
				EntityType: "user",
				Id:         123,
				Target:     pb.CompareTarget_COMPARE_TARGET_VERSION,
				Operator:   pb.CompareOperator_COMPARE_OPERATOR_GREATER_EQUAL,
				Version:    5,
			},
		},
		Success: []*pb.TransactionOp{
			{
				EntityType: "user",
				Id:         123,
				OpType:     pb.TransactionOpType_TRANSACTION_OP_TYPE_PUT,
				Data:       []byte(`{"name":"updated"}`),
			},
			{
				EntityType: "profile",
				Id:         123,
				OpType:     pb.TransactionOpType_TRANSACTION_OP_TYPE_PUT,
				Data:       []byte(`{"bio":"new bio"}`),
			},
		},
		Failure: []*pb.TransactionOp{
			{
				EntityType: "user",
				Id:         123,
				OpType:     pb.TransactionOpType_TRANSACTION_OP_TYPE_GET,
			},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = validateTransactionRequest(req)
	}
}
