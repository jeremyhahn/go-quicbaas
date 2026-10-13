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
	"strconv"
	"time"
)

// TransactionRequest represents a transaction with compare-and-swap semantics.
type TransactionRequest struct {
	// Compare is a list of predicates to check before executing
	Compare []CompareCondition `json:"compare,omitempty"`

	// Success operations to execute if all comparisons succeed
	Success []TransactionOp `json:"success,omitempty"`

	// Failure operations to execute if any comparison fails
	Failure []TransactionOp `json:"failure,omitempty"`
}

// CompareCondition represents a single comparison in a transaction.
type CompareCondition struct {
	// EntityType is the type of entity to check
	EntityType string `json:"entity_type"`

	// ID is the entity ID to check
	ID uint64 `json:"id"`

	// Target is what field to compare (version, value, exists)
	Target CompareTarget `json:"target"`

	// Operator is the comparison operator (equal, not_equal, greater, less)
	Operator CompareOperator `json:"operator"`

	// Value is the expected value (for value comparisons)
	Value json.RawMessage `json:"value,omitempty"`

	// Version is the expected version (for version comparisons)
	Version int64 `json:"version,omitempty"`
}

// CompareTarget specifies what to compare.
type CompareTarget string

const (
	CompareTargetVersion CompareTarget = "version" // Compare entity version
	CompareTargetValue   CompareTarget = "value"   // Compare entity value
	CompareTargetExists  CompareTarget = "exists"  // Check if entity exists
)

// CompareOperator specifies the comparison operator.
type CompareOperator string

const (
	CompareOpEqual     CompareOperator = "equal"
	CompareOpNotEqual  CompareOperator = "not_equal"
	CompareOpGreater   CompareOperator = "greater"
	CompareOpLess      CompareOperator = "less"
	CompareOpGreaterEq CompareOperator = "greater_equal"
	CompareOpLessEq    CompareOperator = "less_equal"
)

// TransactionOp represents a single operation in a transaction.
type TransactionOp struct {
	// Type is the operation type (put, delete, get)
	Type TransactionOpType `json:"type"`

	// EntityType is the entity type
	EntityType string `json:"entity_type"`

	// ID is the entity ID
	ID uint64 `json:"id"`

	// Data is the entity data (for put operations)
	Data json.RawMessage `json:"data,omitempty"`
}

// TransactionOpType specifies the operation type.
type TransactionOpType string

const (
	TxnOpPut    TransactionOpType = "put"
	TxnOpDelete TransactionOpType = "delete"
	TxnOpGet    TransactionOpType = "get"
)

// TransactionResponse represents the result of a transaction.
type TransactionResponse struct {
	// Succeeded indicates if the transaction succeeded
	Succeeded bool `json:"succeeded"`

	// Responses contains the results of executed operations
	Responses []TransactionOpResponse `json:"responses,omitempty"`

	// Timestamp is when the transaction completed
	Timestamp time.Time `json:"timestamp"`

	// Message provides additional information
	Message string `json:"message,omitempty"`
}

// TransactionOpResponse represents the result of a single operation.
type TransactionOpResponse struct {
	// Type is the operation type that was executed
	Type TransactionOpType `json:"type"`

	// EntityType is the entity type
	EntityType string `json:"entity_type"`

	// ID is the entity ID
	ID uint64 `json:"id"`

	// Data is the entity data (for get operations)
	Data json.RawMessage `json:"data,omitempty"`

	// Success indicates if the operation succeeded
	Success bool `json:"success"`

	// Error contains error message if operation failed
	Error string `json:"error,omitempty"`
}

// Validate validates the transaction request.
func (req *TransactionRequest) Validate() error {
	// At least one operation (success or failure) required
	if len(req.Success) == 0 && len(req.Failure) == 0 {
		return &ValidationError{
			Message: "at least one success or failure operation required",
		}
	}

	// Validate compare conditions
	for i, cmp := range req.Compare {
		if cmp.EntityType == "" {
			return &ValidationError{
				Field:   "compare[" + strconv.Itoa(i) + "].entity_type",
				Message: "entity_type is required",
			}
		}
		if cmp.ID == 0 {
			return &ValidationError{
				Field:   "compare[" + strconv.Itoa(i) + "].id",
				Message: "id is required",
			}
		}
		if !isValidCompareTarget(cmp.Target) {
			return &ValidationError{
				Field:   "compare[" + strconv.Itoa(i) + "].target",
				Message: "invalid compare target",
			}
		}
		if !isValidCompareOperator(cmp.Operator) {
			return &ValidationError{
				Field:   "compare[" + strconv.Itoa(i) + "].operator",
				Message: "invalid compare operator",
			}
		}
	}

	// Validate success operations
	for i, op := range req.Success {
		if err := validateTransactionOp(op, i, "success"); err != nil {
			return err
		}
	}

	// Validate failure operations
	for i, op := range req.Failure {
		if err := validateTransactionOp(op, i, "failure"); err != nil {
			return err
		}
	}

	return nil
}

func validateTransactionOp(op TransactionOp, index int, opList string) error {
	if op.EntityType == "" {
		return &ValidationError{
			Field:   opList + "[" + strconv.Itoa(index) + "].entity_type",
			Message: "entity_type is required",
		}
	}
	if op.ID == 0 {
		return &ValidationError{
			Field:   opList + "[" + strconv.Itoa(index) + "].id",
			Message: "id is required",
		}
	}
	if !isValidTransactionOpType(op.Type) {
		return &ValidationError{
			Field:   opList + "[" + strconv.Itoa(index) + "].type",
			Message: "invalid operation type",
		}
	}
	if op.Type == TxnOpPut && len(op.Data) == 0 {
		return &ValidationError{
			Field:   opList + "[" + strconv.Itoa(index) + "].data",
			Message: "data is required for put operations",
		}
	}
	return nil
}

func isValidCompareTarget(target CompareTarget) bool {
	switch target {
	case CompareTargetVersion, CompareTargetValue, CompareTargetExists:
		return true
	default:
		return false
	}
}

func isValidCompareOperator(op CompareOperator) bool {
	switch op {
	case CompareOpEqual, CompareOpNotEqual, CompareOpGreater, CompareOpLess, CompareOpGreaterEq, CompareOpLessEq:
		return true
	default:
		return false
	}
}

func isValidTransactionOpType(opType TransactionOpType) bool {
	switch opType {
	case TxnOpPut, TxnOpDelete, TxnOpGet:
		return true
	default:
		return false
	}
}
