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

package transaction

import (
	"context"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

const (
	serviceName = "quicbaas.Transaction"
)

// Client provides compare-and-swap transaction operations.
type Client interface {
	// Execute executes a compare-and-swap transaction.
	Execute(ctx context.Context, req *TxnRequest) (*TxnResponse, error)
}

// TxnRequest is a compare-and-swap transaction request.
type TxnRequest struct {
	Compare []Compare
	Success []TxnOp
	Failure []TxnOp
}

// Compare is a condition in a transaction.
type Compare struct {
	Key      string
	Target   CompareTarget
	Result   CompareResult
	Value    []byte // For value comparison
	Revision uint64 // For revision comparison
}

// CompareTarget specifies what to compare.
type CompareTarget int

const (
	// CompareVersion compares key version (0 means key doesn't exist).
	CompareVersion CompareTarget = iota
	// CompareValue compares key value.
	CompareValue
	// CompareModRevision compares modification revision.
	CompareModRevision
	// CompareCreateRevision compares creation revision.
	CompareCreateRevision
)

// String returns the string representation of the compare target.
func (t CompareTarget) String() string {
	switch t {
	case CompareVersion:
		return "VERSION"
	case CompareValue:
		return "VALUE"
	case CompareModRevision:
		return "MOD_REVISION"
	case CompareCreateRevision:
		return "CREATE_REVISION"
	default:
		return "UNKNOWN"
	}
}

// CompareResult specifies the comparison operator.
type CompareResult int

const (
	// CompareEqual compares for equality.
	CompareEqual CompareResult = iota
	// CompareNotEqual compares for inequality.
	CompareNotEqual
	// CompareLess compares for less than.
	CompareLess
	// CompareGreater compares for greater than.
	CompareGreater
)

// String returns the string representation of the compare result.
func (r CompareResult) String() string {
	switch r {
	case CompareEqual:
		return "EQUAL"
	case CompareNotEqual:
		return "NOT_EQUAL"
	case CompareLess:
		return "LESS"
	case CompareGreater:
		return "GREATER"
	default:
		return "UNKNOWN"
	}
}

// TxnOp is an operation in a transaction.
type TxnOp struct {
	Type       TxnOpType
	EntityType string
	Key        string
	Value      []byte
}

// TxnOpType is the type of transaction operation.
type TxnOpType int

const (
	// TxnOpPut creates or updates a key.
	TxnOpPut TxnOpType = iota
	// TxnOpGet retrieves a key value.
	TxnOpGet
	// TxnOpDelete deletes a key.
	TxnOpDelete
)

// String returns the string representation of the operation type.
func (t TxnOpType) String() string {
	switch t {
	case TxnOpPut:
		return "PUT"
	case TxnOpGet:
		return "GET"
	case TxnOpDelete:
		return "DELETE"
	default:
		return "UNKNOWN"
	}
}

// TxnResponse is the response from a transaction.
type TxnResponse struct {
	Succeeded bool
	Revision  uint64
	Responses []TxnOpResponse
}

// TxnOpResponse is the response from a single transaction operation.
type TxnOpResponse struct {
	Type  TxnOpType
	Value []byte // For Get operations
}

// client implements the Client interface.
type client struct {
	transport transport.Transport
}

// NewClient creates a new transaction client.
func NewClient(t transport.Transport) Client {
	if t == nil {
		return nil
	}
	return &client{
		transport: t,
	}
}

// Execute executes a compare-and-swap transaction.
func (c *client) Execute(ctx context.Context, req *TxnRequest) (*TxnResponse, error) {
	if err := validateTxnRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("transaction.Execute", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "Execute", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("transaction.Execute", "nil response from server")
	}

	txnResp, ok := resp.(*TxnResponse)
	if !ok {
		return nil, errors.NewInternal("transaction.Execute", "invalid response type")
	}

	return txnResp, nil
}

// Validation functions

func validateTxnRequest(req *TxnRequest) error {
	if req == nil {
		return errors.NewInvalidInput("transaction.Execute", "request is required")
	}

	// Must have at least one compare or one success/failure operation
	if len(req.Compare) == 0 && len(req.Success) == 0 && len(req.Failure) == 0 {
		return errors.NewInvalidInput("transaction.Execute", "transaction must have at least one compare or operation")
	}

	// Validate compares
	for i, cmp := range req.Compare {
		if err := validateCompare(&cmp, i); err != nil {
			return err
		}
	}

	// Validate success operations
	for i, op := range req.Success {
		if err := validateTxnOp(&op, "success", i); err != nil {
			return err
		}
	}

	// Validate failure operations
	for i, op := range req.Failure {
		if err := validateTxnOp(&op, "failure", i); err != nil {
			return err
		}
	}

	return nil
}

func validateCompare(cmp *Compare, index int) error {
	if cmp.Key == "" {
		return errors.NewInvalidInput("transaction.Execute", "compare key is required").
			WithContext("compare_index", index)
	}

	switch cmp.Target {
	case CompareVersion, CompareValue, CompareModRevision, CompareCreateRevision:
		// Valid targets
	default:
		return errors.NewInvalidInput("transaction.Execute", "invalid compare target").
			WithContext("compare_index", index).
			WithContext("target", int(cmp.Target))
	}

	switch cmp.Result {
	case CompareEqual, CompareNotEqual, CompareLess, CompareGreater:
		// Valid results
	default:
		return errors.NewInvalidInput("transaction.Execute", "invalid compare result").
			WithContext("compare_index", index).
			WithContext("result", int(cmp.Result))
	}

	return nil
}

func validateTxnOp(op *TxnOp, branch string, index int) error {
	if op.EntityType == "" {
		return errors.NewInvalidInput("transaction.Execute", "operation entity type is required").
			WithContext("branch", branch).
			WithContext("operation_index", index)
	}
	if op.Key == "" {
		return errors.NewInvalidInput("transaction.Execute", "operation key is required").
			WithContext("branch", branch).
			WithContext("operation_index", index)
	}

	switch op.Type {
	case TxnOpPut, TxnOpGet, TxnOpDelete:
		// Valid types
	default:
		return errors.NewInvalidInput("transaction.Execute", "invalid operation type").
			WithContext("branch", branch).
			WithContext("operation_index", index).
			WithContext("operation_type", int(op.Type))
	}

	return nil
}
