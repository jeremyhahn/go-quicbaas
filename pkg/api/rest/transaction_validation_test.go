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

// TestTransactionRequestValidation tests transaction request validation.
func TestTransactionRequestValidation(t *testing.T) {
	tests := []struct {
		name      string
		req       TransactionRequest
		wantError bool
		errorMsg  string
	}{
		{
			name: "valid request with success ops",
			req: TransactionRequest{
				Success: []TransactionOp{
					{
						Type:       TxnOpPut,
						EntityType: "user",
						ID:         123,
						Data:       json.RawMessage(`{"name":"test"}`),
					},
				},
			},
			wantError: false,
		},
		{
			name: "valid request with compare and success",
			req: TransactionRequest{
				Compare: []CompareCondition{
					{
						EntityType: "user",
						ID:         123,
						Target:     CompareTargetVersion,
						Operator:   CompareOpEqual,
						Version:    1,
					},
				},
				Success: []TransactionOp{
					{
						Type:       TxnOpPut,
						EntityType: "user",
						ID:         123,
						Data:       json.RawMessage(`{"name":"updated"}`),
					},
				},
			},
			wantError: false,
		},
		{
			name: "empty request",
			req: TransactionRequest{
				Compare: []CompareCondition{},
				Success: []TransactionOp{},
				Failure: []TransactionOp{},
			},
			wantError: true,
			errorMsg:  "at least one success or failure operation required",
		},
		{
			name: "compare with missing entity_type",
			req: TransactionRequest{
				Compare: []CompareCondition{
					{
						ID:       123,
						Target:   CompareTargetVersion,
						Operator: CompareOpEqual,
						Version:  1,
					},
				},
				Success: []TransactionOp{
					{
						Type:       TxnOpPut,
						EntityType: "user",
						ID:         123,
						Data:       json.RawMessage(`{"name":"test"}`),
					},
				},
			},
			wantError: true,
			errorMsg:  "entity_type is required",
		},
		{
			name: "operation with missing entity_type",
			req: TransactionRequest{
				Success: []TransactionOp{
					{
						Type: TxnOpPut,
						ID:   123,
						Data: json.RawMessage(`{"name":"test"}`),
					},
				},
			},
			wantError: true,
			errorMsg:  "entity_type is required",
		},
		{
			name: "put operation with missing data",
			req: TransactionRequest{
				Success: []TransactionOp{
					{
						Type:       TxnOpPut,
						EntityType: "user",
						ID:         123,
					},
				},
			},
			wantError: true,
			errorMsg:  "data is required for put operations",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if tt.wantError {
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

// TestTransactionValidationHelpers tests validation helper functions.
func TestTransactionValidationHelpers(t *testing.T) {
	// Test isValidCompareTarget
	assert.True(t, isValidCompareTarget(CompareTargetVersion))
	assert.True(t, isValidCompareTarget(CompareTargetValue))
	assert.True(t, isValidCompareTarget(CompareTargetExists))
	assert.False(t, isValidCompareTarget(CompareTarget("invalid")))

	// Test isValidCompareOperator
	assert.True(t, isValidCompareOperator(CompareOpEqual))
	assert.True(t, isValidCompareOperator(CompareOpNotEqual))
	assert.True(t, isValidCompareOperator(CompareOpGreater))
	assert.True(t, isValidCompareOperator(CompareOpLess))
	assert.True(t, isValidCompareOperator(CompareOpGreaterEq))
	assert.True(t, isValidCompareOperator(CompareOpLessEq))
	assert.False(t, isValidCompareOperator(CompareOperator("invalid")))

	// Test isValidTransactionOpType
	assert.True(t, isValidTransactionOpType(TxnOpPut))
	assert.True(t, isValidTransactionOpType(TxnOpDelete))
	assert.True(t, isValidTransactionOpType(TxnOpGet))
	assert.False(t, isValidTransactionOpType(TransactionOpType("invalid")))
}
