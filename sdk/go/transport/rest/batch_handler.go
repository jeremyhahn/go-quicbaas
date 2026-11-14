// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/batch"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// batchRESTHandler serves the Batch service over /api/v1/batch. It sends
// the server's snake_case request bodies and decodes each reply into the
// typed response the batch client asserts.
type batchRESTHandler struct {
	transport *Transport
}

func newBatchRESTHandler(t *Transport) *batchRESTHandler {
	return &batchRESTHandler{transport: t}
}

// isBatchService reports whether the service name is the Batch service.
func isBatchService(service string) bool {
	svc := service
	if idx := strings.LastIndex(service, "."); idx >= 0 {
		svc = service[idx+1:]
	}
	return svc == "Batch"
}

type batchMethodHandler func(h *batchRESTHandler, ctx context.Context, req interface{}) (interface{}, error)

// batchMethods dispatches by method name.
var batchMethods = map[string]batchMethodHandler{
	"Execute": (*batchRESTHandler).handleExecute,
	"Put":     (*batchRESTHandler).handlePut,
	"Get":     (*batchRESTHandler).handleGet,
	"Delete":  (*batchRESTHandler).handleDelete,
}

func (h *batchRESTHandler) handleRequest(ctx context.Context, method string, req interface{}) (interface{}, error) {
	handler, ok := batchMethods[method]
	if !ok {
		return nil, errors.NewInvalidInput("batch."+method, "unknown method")
	}
	return handler(h, ctx, req)
}

// batchRequest asserts the SDK request type of a verb.
func batchRequest[T any](method string, req interface{}) (*T, error) {
	r, ok := req.(*T)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("batch."+method, "invalid request type")
	}
	return r, nil
}

// Wire shapes of the /api/v1/batch routes (pkg/api/rest/types.go).
type (
	batchExecuteWireOp struct {
		Type       string `json:"type"`
		EntityType string `json:"entity_type"`
		Key        string `json:"key"`
		Value      []byte `json:"value,omitempty"`
	}
	batchExecuteWireResponse struct {
		Results []struct {
			Success bool `json:"success"`
		} `json:"results"`
	}
	batchEntityWireOp struct {
		EntityType string          `json:"entity_type"`
		ID         uint64          `json:"id"`
		Data       json.RawMessage `json:"data,omitempty"`
	}
	batchPutWireResponse struct {
		Responses []struct {
			ID uint64 `json:"id"`
		} `json:"responses"`
		Timestamp int64 `json:"timestamp"`
	}
	batchGetWireResponse struct {
		Responses []struct {
			ID    uint64          `json:"id"`
			Data  json.RawMessage `json:"data"`
			Found bool            `json:"found"`
		} `json:"responses"`
	}
	batchDeleteWireResponse struct {
		Responses []struct {
			Deleted bool `json:"deleted"`
		} `json:"responses"`
		Timestamp int64 `json:"timestamp"`
	}
)

// send POSTs body to a batch route and decodes the answer into out.
func (h *batchRESTHandler) send(ctx context.Context, path string, body interface{}, out interface{}) error {
	return h.transport.doJSONRequest(ctx, http.MethodPost, path, body, out)
}

func (h *batchRESTHandler) handleExecute(ctx context.Context, req interface{}) (interface{}, error) {
	r, err := batchRequest[batch.BatchRequest]("Execute", req)
	if err != nil {
		return nil, err
	}
	ops := make([]batchExecuteWireOp, len(r.Operations))
	for i, op := range r.Operations {
		ops[i] = batchExecuteWireOp{
			Type:       op.Type.String(),
			EntityType: op.EntityType,
			Key:        op.Key,
			Value:      op.Value,
		}
	}
	var wire batchExecuteWireResponse
	body := map[string]interface{}{"operations": ops}
	if err := h.send(ctx, "/api/v1/batch/execute", body, &wire); err != nil {
		return nil, err
	}
	resp := &batch.BatchResponse{Results: make([]batch.BatchResult, len(wire.Results))}
	for i, result := range wire.Results {
		resp.Results[i] = batch.BatchResult{Success: result.Success}
	}
	return resp, nil
}

func (h *batchRESTHandler) handlePut(ctx context.Context, req interface{}) (interface{}, error) {
	r, err := batchRequest[service.BatchPutRequest]("Put", req)
	if err != nil {
		return nil, err
	}
	ops := make([]batchEntityWireOp, len(r.Requests))
	for i, put := range r.Requests {
		ops[i] = batchEntityWireOp{EntityType: put.EntityType, ID: put.ID, Data: put.Data}
	}
	var wire batchPutWireResponse
	if err := h.send(ctx, "/api/v1/batch/put", map[string]interface{}{"operations": ops}, &wire); err != nil {
		return nil, err
	}
	resp := &service.BatchPutResponse{
		Responses: make([]*service.PutResponse, len(wire.Responses)),
		Timestamp: wire.Timestamp,
	}
	for i, item := range wire.Responses {
		resp.Responses[i] = &service.PutResponse{ID: item.ID, Timestamp: wire.Timestamp}
	}
	return resp, nil
}

func (h *batchRESTHandler) handleGet(ctx context.Context, req interface{}) (interface{}, error) {
	r, err := batchRequest[service.BatchGetRequest]("Get", req)
	if err != nil {
		return nil, err
	}
	ops := make([]batchEntityWireOp, len(r.Requests))
	for i, get := range r.Requests {
		ops[i] = batchEntityWireOp{EntityType: get.EntityType, ID: get.ID}
	}
	var wire batchGetWireResponse
	if err := h.send(ctx, "/api/v1/batch/get", map[string]interface{}{"operations": ops}, &wire); err != nil {
		return nil, err
	}
	resp := &service.BatchGetResponse{Responses: make([]*service.GetResponse, len(wire.Responses))}
	for i, item := range wire.Responses {
		resp.Responses[i] = &service.GetResponse{ID: item.ID, Data: item.Data, Found: item.Found}
	}
	return resp, nil
}

func (h *batchRESTHandler) handleDelete(ctx context.Context, req interface{}) (interface{}, error) {
	r, err := batchRequest[service.BatchDeleteRequest]("Delete", req)
	if err != nil {
		return nil, err
	}
	ops := make([]batchEntityWireOp, len(r.Requests))
	for i, del := range r.Requests {
		ops[i] = batchEntityWireOp{EntityType: del.EntityType, ID: del.ID}
	}
	var wire batchDeleteWireResponse
	if err := h.send(ctx, "/api/v1/batch/delete", map[string]interface{}{"operations": ops}, &wire); err != nil {
		return nil, err
	}
	resp := &service.BatchDeleteResponse{
		Responses: make([]*service.DeleteResponse, len(wire.Responses)),
		Timestamp: wire.Timestamp,
	}
	for i, item := range wire.Responses {
		resp.Responses[i] = &service.DeleteResponse{Deleted: item.Deleted, Timestamp: wire.Timestamp}
	}
	return resp, nil
}
