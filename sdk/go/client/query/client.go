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

package query

import (
	"context"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

const (
	serviceName = "quicbaas.Query"
)

// Client provides secondary index query operations.
type Client interface {
	// Execute executes a query using a secondary index.
	Execute(ctx context.Context, req *QueryRequest) (*QueryResponse, error)
}

// QueryRequest is a secondary index query request.
type QueryRequest struct {
	EntityType string
	Index      string
	Value      interface{}
	Operator   Operator
	Limit      int
	Cursor     string
}

// Operator is the comparison operator for queries.
type Operator int

const (
	// OpEqual matches exactly.
	OpEqual Operator = iota
	// OpNotEqual matches if not equal.
	OpNotEqual
	// OpLessThan matches if less than.
	OpLessThan
	// OpLessThanOrEqual matches if less than or equal.
	OpLessThanOrEqual
	// OpGreaterThan matches if greater than.
	OpGreaterThan
	// OpGreaterThanOrEqual matches if greater than or equal.
	OpGreaterThanOrEqual
	// OpPrefix matches if value starts with prefix.
	OpPrefix
	// OpRange matches if value is within a range.
	OpRange
)

// String returns the string representation of the operator.
func (o Operator) String() string {
	switch o {
	case OpEqual:
		return "EQUAL"
	case OpNotEqual:
		return "NOT_EQUAL"
	case OpLessThan:
		return "LESS_THAN"
	case OpLessThanOrEqual:
		return "LESS_THAN_OR_EQUAL"
	case OpGreaterThan:
		return "GREATER_THAN"
	case OpGreaterThanOrEqual:
		return "GREATER_THAN_OR_EQUAL"
	case OpPrefix:
		return "PREFIX"
	case OpRange:
		return "RANGE"
	default:
		return "UNKNOWN"
	}
}

// QueryResponse is the response from a query.
type QueryResponse struct {
	Results    []QueryResult
	NextCursor string
	HasMore    bool
}

// QueryResult is a single result from a query.
type QueryResult struct {
	Key   string
	Value []byte
}

// client implements the Client interface.
type client struct {
	transport transport.Transport
}

// NewClient creates a new query client.
func NewClient(t transport.Transport) Client {
	if t == nil {
		return nil
	}
	return &client{
		transport: t,
	}
}

// Execute executes a query using a secondary index.
func (c *client) Execute(ctx context.Context, req *QueryRequest) (*QueryResponse, error) {
	if err := validateQueryRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("query.Execute", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "Execute", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return &QueryResponse{Results: []QueryResult{}}, nil
	}

	queryResp, ok := resp.(*QueryResponse)
	if !ok {
		return nil, errors.NewInternal("query.Execute", "invalid response type")
	}

	return queryResp, nil
}

// Validation functions

func validateQueryRequest(req *QueryRequest) error {
	if req == nil {
		return errors.NewInvalidInput("query.Execute", "request is required")
	}
	if req.EntityType == "" {
		return errors.NewInvalidInput("query.Execute", "entity type is required")
	}
	if req.Index == "" {
		return errors.NewInvalidInput("query.Execute", "index is required")
	}

	switch req.Operator {
	case OpEqual, OpNotEqual, OpLessThan, OpLessThanOrEqual,
		OpGreaterThan, OpGreaterThanOrEqual, OpPrefix, OpRange:
		// Valid operators
	default:
		return errors.NewInvalidInput("query.Execute", "invalid operator").
			WithContext("operator", int(req.Operator))
	}

	if req.Limit < 0 {
		return errors.NewInvalidInput("query.Execute", "limit cannot be negative")
	}

	return nil
}
