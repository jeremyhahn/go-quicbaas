//go:build rest

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

package rest

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTransactionRequest_Validate_EmptyOperations(t *testing.T) {
	req := &TransactionRequest{
		Compare: []CompareCondition{},
		Success: []TransactionOp{},
		Failure: []TransactionOp{},
	}

	err := req.Validate()
	assert.Error(t, err)

	valErr, ok := err.(*ValidationError)
	assert.True(t, ok)
	assert.Contains(t, valErr.Message, "at least one success or failure operation required")
}

func TestTransactionRequest_Validate_InvalidCompareEntityType(t *testing.T) {
	req := &TransactionRequest{
		Compare: []CompareCondition{
			{
				EntityType: "", // Empty entity type
				ID:         1,
				Target:     CompareTargetVersion,
				Operator:   CompareOpEqual,
			},
		},
		Success: []TransactionOp{
			{Type: TxnOpGet, EntityType: "user", ID: 1},
		},
	}

	err := req.Validate()
	assert.Error(t, err)

	valErr, ok := err.(*ValidationError)
	assert.True(t, ok)
	assert.Contains(t, valErr.Field, "compare")
	assert.Contains(t, valErr.Field, "entity_type")
}

func TestTransactionRequest_Validate_InvalidCompareID(t *testing.T) {
	req := &TransactionRequest{
		Compare: []CompareCondition{
			{
				EntityType: "user",
				ID:         0, // Zero ID
				Target:     CompareTargetVersion,
				Operator:   CompareOpEqual,
			},
		},
		Success: []TransactionOp{
			{Type: TxnOpGet, EntityType: "user", ID: 1},
		},
	}

	err := req.Validate()
	assert.Error(t, err)

	valErr, ok := err.(*ValidationError)
	assert.True(t, ok)
	assert.Contains(t, valErr.Field, "compare")
	assert.Contains(t, valErr.Field, "id")
}

func TestTransactionRequest_Validate_InvalidCompareTarget(t *testing.T) {
	req := &TransactionRequest{
		Compare: []CompareCondition{
			{
				EntityType: "user",
				ID:         1,
				Target:     CompareTarget("invalid"), // Invalid target
				Operator:   CompareOpEqual,
			},
		},
		Success: []TransactionOp{
			{Type: TxnOpGet, EntityType: "user", ID: 1},
		},
	}

	err := req.Validate()
	assert.Error(t, err)

	valErr, ok := err.(*ValidationError)
	assert.True(t, ok)
	assert.Contains(t, valErr.Field, "compare")
	assert.Contains(t, valErr.Field, "target")
}

func TestTransactionRequest_Validate_InvalidCompareOperator(t *testing.T) {
	req := &TransactionRequest{
		Compare: []CompareCondition{
			{
				EntityType: "user",
				ID:         1,
				Target:     CompareTargetVersion,
				Operator:   CompareOperator("invalid"), // Invalid operator
			},
		},
		Success: []TransactionOp{
			{Type: TxnOpGet, EntityType: "user", ID: 1},
		},
	}

	err := req.Validate()
	assert.Error(t, err)

	valErr, ok := err.(*ValidationError)
	assert.True(t, ok)
	assert.Contains(t, valErr.Field, "compare")
	assert.Contains(t, valErr.Field, "operator")
}

func TestTransactionRequest_Validate_SuccessOpInvalidEntityType(t *testing.T) {
	req := &TransactionRequest{
		Success: []TransactionOp{
			{
				Type:       TxnOpGet,
				EntityType: "", // Empty entity type
				ID:         1,
			},
		},
	}

	err := req.Validate()
	assert.Error(t, err)

	valErr, ok := err.(*ValidationError)
	assert.True(t, ok)
	assert.Contains(t, valErr.Field, "success")
	assert.Contains(t, valErr.Field, "entity_type")
}

func TestTransactionRequest_Validate_SuccessOpInvalidID(t *testing.T) {
	req := &TransactionRequest{
		Success: []TransactionOp{
			{
				Type:       TxnOpGet,
				EntityType: "user",
				ID:         0, // Zero ID
			},
		},
	}

	err := req.Validate()
	assert.Error(t, err)

	valErr, ok := err.(*ValidationError)
	assert.True(t, ok)
	assert.Contains(t, valErr.Field, "success")
	assert.Contains(t, valErr.Field, "id")
}

func TestTransactionRequest_Validate_SuccessOpInvalidType(t *testing.T) {
	req := &TransactionRequest{
		Success: []TransactionOp{
			{
				Type:       TransactionOpType("invalid"), // Invalid type
				EntityType: "user",
				ID:         1,
			},
		},
	}

	err := req.Validate()
	assert.Error(t, err)

	valErr, ok := err.(*ValidationError)
	assert.True(t, ok)
	assert.Contains(t, valErr.Field, "success")
	assert.Contains(t, valErr.Field, "type")
}

func TestTransactionRequest_Validate_PutOpMissingData(t *testing.T) {
	req := &TransactionRequest{
		Success: []TransactionOp{
			{
				Type:       TxnOpPut,
				EntityType: "user",
				ID:         1,
				Data:       nil, // Missing data for put
			},
		},
	}

	err := req.Validate()
	assert.Error(t, err)

	valErr, ok := err.(*ValidationError)
	assert.True(t, ok)
	assert.Contains(t, valErr.Field, "success")
	assert.Contains(t, valErr.Field, "data")
}

func TestTransactionRequest_Validate_FailureOpInvalidEntityType(t *testing.T) {
	req := &TransactionRequest{
		Failure: []TransactionOp{
			{
				Type:       TxnOpGet,
				EntityType: "", // Empty entity type
				ID:         1,
			},
		},
	}

	err := req.Validate()
	assert.Error(t, err)

	valErr, ok := err.(*ValidationError)
	assert.True(t, ok)
	assert.Contains(t, valErr.Field, "failure")
	assert.Contains(t, valErr.Field, "entity_type")
}

func TestTransactionRequest_Validate_FailureOpInvalidID(t *testing.T) {
	req := &TransactionRequest{
		Failure: []TransactionOp{
			{
				Type:       TxnOpGet,
				EntityType: "user",
				ID:         0, // Zero ID
			},
		},
	}

	err := req.Validate()
	assert.Error(t, err)

	valErr, ok := err.(*ValidationError)
	assert.True(t, ok)
	assert.Contains(t, valErr.Field, "failure")
	assert.Contains(t, valErr.Field, "id")
}

func TestTransactionRequest_Validate_ValidRequest(t *testing.T) {
	req := &TransactionRequest{
		Compare: []CompareCondition{
			{
				EntityType: "user",
				ID:         1,
				Target:     CompareTargetVersion,
				Operator:   CompareOpEqual,
				Version:    5,
			},
		},
		Success: []TransactionOp{
			{
				Type:       TxnOpPut,
				EntityType: "user",
				ID:         1,
				Data:       json.RawMessage(`{"name": "test"}`),
			},
		},
		Failure: []TransactionOp{
			{
				Type:       TxnOpGet,
				EntityType: "user",
				ID:         1,
			},
		},
	}

	err := req.Validate()
	assert.NoError(t, err)
}

func TestIsValidCompareTarget(t *testing.T) {
	assert.True(t, isValidCompareTarget(CompareTargetVersion))
	assert.True(t, isValidCompareTarget(CompareTargetValue))
	assert.True(t, isValidCompareTarget(CompareTargetExists))
	assert.False(t, isValidCompareTarget(CompareTarget("invalid")))
}

func TestIsValidCompareOperator(t *testing.T) {
	assert.True(t, isValidCompareOperator(CompareOpEqual))
	assert.True(t, isValidCompareOperator(CompareOpNotEqual))
	assert.True(t, isValidCompareOperator(CompareOpGreater))
	assert.True(t, isValidCompareOperator(CompareOpLess))
	assert.True(t, isValidCompareOperator(CompareOpGreaterEq))
	assert.True(t, isValidCompareOperator(CompareOpLessEq))
	assert.False(t, isValidCompareOperator(CompareOperator("invalid")))
}

func TestIsValidTransactionOpType(t *testing.T) {
	assert.True(t, isValidTransactionOpType(TxnOpPut))
	assert.True(t, isValidTransactionOpType(TxnOpDelete))
	assert.True(t, isValidTransactionOpType(TxnOpGet))
	assert.False(t, isValidTransactionOpType(TransactionOpType("invalid")))
}
