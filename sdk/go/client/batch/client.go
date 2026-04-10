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

package batch

import (
	"context"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

const (
	serviceName = "quicbaas.Batch"
)

// Client provides atomic batch operations.
type Client interface {
	// Execute executes a batch of operations atomically.
	Execute(ctx context.Context, req *BatchRequest) (*BatchResponse, error)

	// Put performs multiple Put operations atomically.
	Put(ctx context.Context, requests []*service.PutRequest) (*BatchPutResponse, error)

	// Get retrieves multiple values in a single request.
	Get(ctx context.Context, requests []*service.GetRequest) (*BatchGetResponse, error)

	// Delete removes multiple keys atomically.
	Delete(ctx context.Context, requests []*service.DeleteRequest) (*BatchDeleteResponse, error)
}

// BatchRequest contains multiple operations to execute atomically.
type BatchRequest struct {
	Operations []BatchOperation
}

// BatchOperation is a single operation in a batch.
type BatchOperation struct {
	Type       OpType
	EntityType string
	Key        string
	Value      []byte
}

// OpType is the type of batch operation.
type OpType int

const (
	// OpPut creates or updates a key-value pair.
	OpPut OpType = iota
	// OpDelete removes a key.
	OpDelete
)

// String returns the string representation of the operation type.
func (t OpType) String() string {
	switch t {
	case OpPut:
		return "PUT"
	case OpDelete:
		return "DELETE"
	default:
		return "UNKNOWN"
	}
}

// BatchResponse is the response from a batch operation.
type BatchResponse struct {
	Revision uint64
	Results  []BatchResult
}

// BatchResult is the result of a single batch operation.
type BatchResult struct {
	Success bool
	Error   string
}

// BatchPutResponse is the response from a batch put operation.
type BatchPutResponse struct {
	Responses []*PutResultItem
	Timestamp int64
}

// PutResultItem contains the result of a single put in a batch.
type PutResultItem struct {
	EntityType string
	ID         uint64
	Revision   int64
	Version    uint64
	Success    bool
	Error      string
}

// BatchGetResponse is the response from a batch get operation.
type BatchGetResponse struct {
	Responses []*GetResultItem
}

// GetResultItem contains the result of a single get in a batch.
type GetResultItem struct {
	EntityType string
	ID         uint64
	Data       []byte
	Revision   int64
	Version    uint64
	Found      bool
	Error      string
}

// BatchDeleteResponse is the response from a batch delete operation.
type BatchDeleteResponse struct {
	Responses []*DeleteResultItem
	Timestamp int64
}

// DeleteResultItem contains the result of a single delete in a batch.
type DeleteResultItem struct {
	EntityType string
	ID         uint64
	Deleted    bool
	Error      string
}

// client implements the Client interface.
type client struct {
	transport transport.Transport
}

// NewClient creates a new batch client.
func NewClient(t transport.Transport) Client {
	if t == nil {
		return nil
	}
	return &client{
		transport: t,
	}
}

// Execute executes a batch of operations atomically.
func (c *client) Execute(ctx context.Context, req *BatchRequest) (*BatchResponse, error) {
	if err := validateBatchRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("batch.Execute", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "Execute", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("batch.Execute", "nil response from server")
	}

	batchResp, ok := resp.(*BatchResponse)
	if !ok {
		return nil, errors.NewInternal("batch.Execute", "invalid response type")
	}

	return batchResp, nil
}

// Put performs multiple Put operations atomically.
func (c *client) Put(ctx context.Context, requests []*service.PutRequest) (*BatchPutResponse, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("batch.Put", "context is required")
	}

	if len(requests) == 0 {
		return &BatchPutResponse{Responses: []*PutResultItem{}}, nil
	}

	req := &service.BatchPutRequest{
		Requests: requests,
	}

	resp, err := c.transport.Request(ctx, serviceName, "Put", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("batch.Put", "nil response from server")
	}

	svcResp, ok := resp.(*service.BatchPutResponse)
	if !ok {
		return nil, errors.NewInternal("batch.Put", "invalid response type")
	}

	// Convert service response to batch client response
	result := &BatchPutResponse{
		Responses: make([]*PutResultItem, len(svcResp.Responses)),
		Timestamp: svcResp.Timestamp,
	}

	for i, r := range svcResp.Responses {
		var entityType string
		if i < len(requests) {
			entityType = requests[i].EntityType
		}
		result.Responses[i] = &PutResultItem{
			EntityType: entityType,
			ID:         r.ID,
			Version:    r.Version,
			Success:    true,
		}
	}

	return result, nil
}

// Get retrieves multiple values in a single request.
func (c *client) Get(ctx context.Context, requests []*service.GetRequest) (*BatchGetResponse, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("batch.Get", "context is required")
	}

	if len(requests) == 0 {
		return &BatchGetResponse{Responses: []*GetResultItem{}}, nil
	}

	req := &service.BatchGetRequest{
		Requests: requests,
	}

	resp, err := c.transport.Request(ctx, serviceName, "Get", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("batch.Get", "nil response from server")
	}

	svcResp, ok := resp.(*service.BatchGetResponse)
	if !ok {
		return nil, errors.NewInternal("batch.Get", "invalid response type")
	}

	// Convert service response to batch client response
	result := &BatchGetResponse{
		Responses: make([]*GetResultItem, len(svcResp.Responses)),
	}

	for i, r := range svcResp.Responses {
		var entityType string
		if i < len(requests) {
			entityType = requests[i].EntityType
		}
		result.Responses[i] = &GetResultItem{
			EntityType: entityType,
			ID:         r.ID,
			Data:       r.Data,
			Version:    r.Version,
			Found:      r.Found,
		}
	}

	return result, nil
}

// Delete removes multiple keys atomically.
func (c *client) Delete(ctx context.Context, requests []*service.DeleteRequest) (*BatchDeleteResponse, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("batch.Delete", "context is required")
	}

	if len(requests) == 0 {
		return &BatchDeleteResponse{Responses: []*DeleteResultItem{}}, nil
	}

	req := &service.BatchDeleteRequest{
		Requests: requests,
	}

	resp, err := c.transport.Request(ctx, serviceName, "Delete", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("batch.Delete", "nil response from server")
	}

	svcResp, ok := resp.(*service.BatchDeleteResponse)
	if !ok {
		return nil, errors.NewInternal("batch.Delete", "invalid response type")
	}

	// Convert service response to batch client response
	result := &BatchDeleteResponse{
		Responses: make([]*DeleteResultItem, len(svcResp.Responses)),
		Timestamp: svcResp.Timestamp,
	}

	for i, r := range svcResp.Responses {
		var entityType string
		if i < len(requests) {
			entityType = requests[i].EntityType
		}
		result.Responses[i] = &DeleteResultItem{
			EntityType: entityType,
			ID:         requests[i].ID,
			Deleted:    r.Deleted,
		}
	}

	return result, nil
}

// Validation functions

func validateBatchRequest(req *BatchRequest) error {
	if req == nil {
		return errors.NewInvalidInput("batch.Execute", "request is required")
	}
	if len(req.Operations) == 0 {
		return errors.NewInvalidInput("batch.Execute", "at least one operation is required")
	}

	for i, op := range req.Operations {
		if err := validateBatchOperation(&op, i); err != nil {
			return err
		}
	}

	return nil
}

func validateBatchOperation(op *BatchOperation, index int) error {
	if op.EntityType == "" {
		return errors.NewInvalidInput("batch.Execute", "entity type is required").
			WithContext("operation_index", index)
	}
	if op.Key == "" {
		return errors.NewInvalidInput("batch.Execute", "key is required").
			WithContext("operation_index", index)
	}

	switch op.Type {
	case OpPut:
		// Value can be empty for put operations
	case OpDelete:
		// Delete doesn't need a value
	default:
		return errors.NewInvalidInput("batch.Execute", "invalid operation type").
			WithContext("operation_index", index).
			WithContext("operation_type", int(op.Type))
	}

	return nil
}
