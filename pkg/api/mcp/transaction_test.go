//go:build mcp

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

package mcp

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// TestExecuteTransaction_Success tests successful transaction execution.
func TestExecuteTransaction_Success(t *testing.T) {
	mockSvc := &MockQuicBaaS{}
	log, err := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})
	require.NoError(t, err)

	registry := NewToolRegistry(mockSvc, log)
	ctx := context.Background()

	// Setup mock expectation
	mockSvc.On("Transaction", mock.Anything, mock.MatchedBy(func(req *service.TransactionRequest) bool {
		return req.ShardID == uint64(1) &&
			len(req.Compare) == 1 &&
			req.Compare[0].EntityType == "user" &&
			req.Compare[0].ID == uint64(123) &&
			len(req.Success) == 1 &&
			req.Success[0].OpType == service.TransactionOpTypePut
	})).Return(&service.TransactionResponse{
		Succeeded: true,
		Responses: []*service.TransactionOpResponse{
			{
				OpType:  service.TransactionOpTypePut,
				Success: true,
			},
		},
	}, nil)

	args := map[string]interface{}{
		"shardID": float64(1),
		"compare": []interface{}{
			map[string]interface{}{
				"entityType": "user",
				"id":         float64(123),
				"target":     "version",
				"operator":   "eq",
				"version":    float64(1),
			},
		},
		"success": []interface{}{
			map[string]interface{}{
				"opType":     "put",
				"entityType": "user",
				"id":         float64(123),
				"data":       "updated-data",
			},
		},
	}

	result, err := registry.executeTransaction(ctx, args)
	assert.NoError(t, err)
	assert.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, true, resultMap["Succeeded"])

	mockSvc.AssertExpectations(t)
}

// TestExecuteTransaction_EmptyArgs tests transaction with empty args.
func TestExecuteTransaction_EmptyArgs(t *testing.T) {
	mockSvc := &MockQuicBaaS{}
	log, err := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})
	require.NoError(t, err)

	registry := NewToolRegistry(mockSvc, log)
	ctx := context.Background()

	// Setup mock expectation for empty transaction
	mockSvc.On("Transaction", mock.Anything, mock.MatchedBy(func(req *service.TransactionRequest) bool {
		return len(req.Compare) == 0 && len(req.Success) == 0 && len(req.Failure) == 0
	})).Return(&service.TransactionResponse{
		Succeeded: true,
	}, nil)

	result, err := registry.executeTransaction(ctx, map[string]interface{}{})
	assert.NoError(t, err)
	assert.NotNil(t, result)

	mockSvc.AssertExpectations(t)
}

// TestExecuteTransaction_ServiceError tests transaction with service error.
func TestExecuteTransaction_ServiceError(t *testing.T) {
	mockSvc := &MockQuicBaaS{}
	log, err := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})
	require.NoError(t, err)

	registry := NewToolRegistry(mockSvc, log)
	ctx := context.Background()

	// Setup mock expectation to return error
	mockSvc.On("Transaction", mock.Anything, mock.Anything).Return(nil, errors.New("transaction failed"))

	result, err := registry.executeTransaction(ctx, map[string]interface{}{})
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "transaction failed")

	mockSvc.AssertExpectations(t)
}

// TestExecuteTransaction_InvalidCompareArray tests invalid compare field.
func TestExecuteTransaction_InvalidCompareArray(t *testing.T) {
	mockSvc := &MockQuicBaaS{}
	log, err := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})
	require.NoError(t, err)

	registry := NewToolRegistry(mockSvc, log)
	ctx := context.Background()

	// Test with invalid compare type (not an array)
	args := map[string]interface{}{
		"compare": "not-an-array",
	}

	result, err := registry.executeTransaction(ctx, args)
	assert.Error(t, err)
	assert.Nil(t, result)

	parseErr, ok := err.(*TransactionParseError)
	require.True(t, ok)
	assert.Equal(t, "compare", parseErr.Field)
}

// TestExecuteTransaction_InvalidCompareItem tests invalid compare item.
func TestExecuteTransaction_InvalidCompareItem(t *testing.T) {
	mockSvc := &MockQuicBaaS{}
	log, err := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})
	require.NoError(t, err)

	registry := NewToolRegistry(mockSvc, log)
	ctx := context.Background()

	// Test with invalid compare item (not an object)
	args := map[string]interface{}{
		"compare": []interface{}{"not-an-object"},
	}

	result, err := registry.executeTransaction(ctx, args)
	assert.Error(t, err)
	assert.Nil(t, result)

	parseErr, ok := err.(*TransactionParseError)
	require.True(t, ok)
	assert.Equal(t, "compare[0]", parseErr.Field)
}

// TestExecuteTransaction_InvalidCompareTarget tests invalid compare target.
func TestExecuteTransaction_InvalidCompareTarget(t *testing.T) {
	mockSvc := &MockQuicBaaS{}
	log, err := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})
	require.NoError(t, err)

	registry := NewToolRegistry(mockSvc, log)
	ctx := context.Background()

	args := map[string]interface{}{
		"compare": []interface{}{
			map[string]interface{}{
				"entityType": "user",
				"id":         float64(123),
				"target":     "invalid-target",
				"operator":   "eq",
			},
		},
	}

	result, err := registry.executeTransaction(ctx, args)
	assert.Error(t, err)
	assert.Nil(t, result)

	parseErr, ok := err.(*TransactionParseError)
	require.True(t, ok)
	assert.Equal(t, "compare[0].target", parseErr.Field)
}

// TestExecuteTransaction_InvalidCompareOperator tests invalid compare operator.
func TestExecuteTransaction_InvalidCompareOperator(t *testing.T) {
	mockSvc := &MockQuicBaaS{}
	log, err := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})
	require.NoError(t, err)

	registry := NewToolRegistry(mockSvc, log)
	ctx := context.Background()

	args := map[string]interface{}{
		"compare": []interface{}{
			map[string]interface{}{
				"entityType": "user",
				"id":         float64(123),
				"target":     "version",
				"operator":   "invalid-operator",
			},
		},
	}

	result, err := registry.executeTransaction(ctx, args)
	assert.Error(t, err)
	assert.Nil(t, result)

	parseErr, ok := err.(*TransactionParseError)
	require.True(t, ok)
	assert.Equal(t, "compare[0].operator", parseErr.Field)
}

// TestExecuteTransaction_InvalidSuccessArray tests invalid success field.
func TestExecuteTransaction_InvalidSuccessArray(t *testing.T) {
	mockSvc := &MockQuicBaaS{}
	log, err := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})
	require.NoError(t, err)

	registry := NewToolRegistry(mockSvc, log)
	ctx := context.Background()

	args := map[string]interface{}{
		"success": "not-an-array",
	}

	result, err := registry.executeTransaction(ctx, args)
	assert.Error(t, err)
	assert.Nil(t, result)

	parseErr, ok := err.(*TransactionParseError)
	require.True(t, ok)
	assert.Equal(t, "success", parseErr.Field)
}

// TestExecuteTransaction_InvalidOpType tests invalid operation type.
func TestExecuteTransaction_InvalidOpType(t *testing.T) {
	mockSvc := &MockQuicBaaS{}
	log, err := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})
	require.NoError(t, err)

	registry := NewToolRegistry(mockSvc, log)
	ctx := context.Background()

	args := map[string]interface{}{
		"success": []interface{}{
			map[string]interface{}{
				"opType":     "invalid-op",
				"entityType": "user",
				"id":         float64(123),
			},
		},
	}

	result, err := registry.executeTransaction(ctx, args)
	assert.Error(t, err)
	assert.Nil(t, result)

	parseErr, ok := err.(*TransactionParseError)
	require.True(t, ok)
	assert.Equal(t, "success[0].opType", parseErr.Field)
}

// TestExecuteTransaction_AllCompareTargets tests all valid compare targets.
func TestExecuteTransaction_AllCompareTargets(t *testing.T) {
	targets := []string{"version", "value", "exists", "VERSION", "VALUE", "EXISTS", "Version", "Value", "Exists"}
	for _, target := range targets {
		result, err := parseCompareTarget(target)
		assert.NoError(t, err, "target: %s", target)
		assert.NotEqual(t, service.CompareTarget(0), result)
	}
}

// TestExecuteTransaction_AllCompareOperators tests all valid compare operators.
func TestExecuteTransaction_AllCompareOperators(t *testing.T) {
	operators := []string{"eq", "ne", "gt", "lt", "gte", "lte", "=", "!=", ">", "<", ">=", "<=", "equal", "EQ", "NE", "GT", "LT", "GTE", "LTE"}
	for _, op := range operators {
		result, err := parseCompareOperator(op)
		assert.NoError(t, err, "operator: %s", op)
		assert.NotEqual(t, service.CompareOperator(0), result)
	}
}

// TestExecuteTransaction_AllOpTypes tests all valid operation types.
func TestExecuteTransaction_AllOpTypes(t *testing.T) {
	opTypes := []string{"put", "get", "delete", "PUT", "GET", "DELETE", "Put", "Get", "Delete"}
	for _, opType := range opTypes {
		result, err := parseTransactionOpType(opType)
		assert.NoError(t, err, "opType: %s", opType)
		assert.NotEqual(t, service.TransactionOpType(0), result)
	}
}

// TestExecuteTransaction_WithFailureOps tests transaction with failure operations.
func TestExecuteTransaction_WithFailureOps(t *testing.T) {
	mockSvc := &MockQuicBaaS{}
	log, err := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})
	require.NoError(t, err)

	registry := NewToolRegistry(mockSvc, log)
	ctx := context.Background()

	// Setup mock expectation
	mockSvc.On("Transaction", mock.Anything, mock.MatchedBy(func(req *service.TransactionRequest) bool {
		return len(req.Failure) == 1 && req.Failure[0].OpType == service.TransactionOpTypeGet
	})).Return(&service.TransactionResponse{
		Succeeded: false,
		Responses: []*service.TransactionOpResponse{
			{
				OpType:  service.TransactionOpTypeGet,
				Success: true,
			},
		},
	}, nil)

	args := map[string]interface{}{
		"compare": []interface{}{
			map[string]interface{}{
				"entityType": "user",
				"id":         float64(123),
				"target":     "exists",
				"operator":   "eq",
			},
		},
		"failure": []interface{}{
			map[string]interface{}{
				"opType":     "get",
				"entityType": "user",
				"id":         float64(123),
			},
		},
	}

	result, err := registry.executeTransaction(ctx, args)
	assert.NoError(t, err)
	assert.NotNil(t, result)

	mockSvc.AssertExpectations(t)
}

// TestExecuteTransaction_SnakeCaseParams tests transaction with snake_case parameters.
func TestExecuteTransaction_SnakeCaseParams(t *testing.T) {
	mockSvc := &MockQuicBaaS{}
	log, err := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})
	require.NoError(t, err)

	registry := NewToolRegistry(mockSvc, log)
	ctx := context.Background()

	// Setup mock expectation
	mockSvc.On("Transaction", mock.Anything, mock.MatchedBy(func(req *service.TransactionRequest) bool {
		return req.Compare[0].EntityType == "user" && req.Success[0].EntityType == "user"
	})).Return(&service.TransactionResponse{
		Succeeded: true,
	}, nil)

	// Use snake_case parameter names
	args := map[string]interface{}{
		"compare": []interface{}{
			map[string]interface{}{
				"entity_type":      "user",
				"id":               float64(123),
				"compare_target":   "version",
				"compare_operator": "eq",
			},
		},
		"success": []interface{}{
			map[string]interface{}{
				"op_type":     "put",
				"entity_type": "user",
				"id":          float64(123),
				"value":       "test-data",
			},
		},
	}

	result, err := registry.executeTransaction(ctx, args)
	assert.NoError(t, err)
	assert.NotNil(t, result)

	mockSvc.AssertExpectations(t)
}

// TestTransactionParseError_Error tests the TransactionParseError.Error method.
func TestTransactionParseError_Error(t *testing.T) {
	err := &TransactionParseError{
		Field:   "compare[0].target",
		Message: "expected string",
	}
	assert.Equal(t, "transaction parse error: compare[0].target: expected string", err.Error())
}

// TestTransactionInvalidCompareTargetError_Error tests the error message.
func TestTransactionInvalidCompareTargetError_Error(t *testing.T) {
	err := &TransactionInvalidCompareTargetError{
		Target: "invalid",
	}
	assert.Equal(t, "invalid compare target: invalid", err.Error())
}

// TestTransactionInvalidCompareOperatorError_Error tests the error message.
func TestTransactionInvalidCompareOperatorError_Error(t *testing.T) {
	err := &TransactionInvalidCompareOperatorError{
		Operator: "invalid",
	}
	assert.Equal(t, "invalid compare operator: invalid", err.Error())
}

// TestTransactionInvalidOpTypeError_Error tests the error message.
func TestTransactionInvalidOpTypeError_Error(t *testing.T) {
	err := &TransactionInvalidOpTypeError{
		OpType: "invalid",
	}
	assert.Equal(t, "invalid transaction operation type: invalid", err.Error())
}
