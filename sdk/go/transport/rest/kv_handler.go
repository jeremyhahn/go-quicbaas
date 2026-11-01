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
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"strings"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

// kvHandler handles KV-specific request/response conversions for REST transport.
type kvHandler struct {
	transport *Transport
}

// newKVHandler creates a new KV handler.
func newKVHandler(t *Transport) *kvHandler {
	return &kvHandler{transport: t}
}

// handleRequest routes KV requests to appropriate handlers.
func (h *kvHandler) handleRequest(ctx context.Context, method string, req interface{}) (interface{}, error) {
	switch method {
	case "Put":
		return h.handlePut(ctx, req)
	case "Get":
		return h.handleGet(ctx, req)
	case "Delete":
		return h.handleDelete(ctx, req)
	case "List":
		return h.handleList(ctx, req)
	case "CompareAndSwap":
		return h.handleCompareAndSwap(ctx, req)
	case "PutIfAbsent":
		return h.handlePutIfAbsent(ctx, req)
	default:
		return nil, errors.NewUnimplemented("kvHandler.handleRequest",
			"unsupported KV method: "+method)
	}
}

// handlePut handles Put requests.
func (h *kvHandler) handlePut(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*service.PutRequest)
	if !ok {
		return nil, errors.NewInvalidInput("kvHandler.handlePut",
			"invalid request type, expected *service.PutRequest")
	}

	// Build URL: /api/v1/kv/{type}/{key}
	// The key is the string representation of the ID
	url := h.transport.baseURL + h.transport.basePath +
		fmt.Sprintf("/api/v1/kv/%s/%d", sdkReq.EntityType, sdkReq.ID)

	// Build request body
	body := map[string]interface{}{
		"value": sdkReq.Data,
	}
	if sdkReq.Consistency > 0 {
		body["consistency"] = consistencyToString(sdkReq.Consistency)
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInvalidInput, "kvHandler.handlePut",
			"failed to marshal request body")
	}

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "kvHandler.handlePut",
			"failed to create HTTP request")
	}

	httpReq.Header.Set("Content-Type", "application/json")
	for k, v := range h.transport.headers {
		httpReq.Header.Set(k, v)
	}

	// Execute request
	resp, err := h.transport.client.Do(httpReq)
	if err != nil {
		return nil, h.transport.convertHTTPError(err, "kvHandler.handlePut")
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "kvHandler.handlePut",
			"failed to read response body")
	}

	if resp.StatusCode >= 400 {
		return nil, h.transport.parseErrorResponse(resp.StatusCode, respBody)
	}

	// Parse response
	var restResp struct {
		EntityType string    `json:"entity_type"`
		Key        string    `json:"key"`
		Success    bool      `json:"success"`
		Message    string    `json:"message"`
		Timestamp  time.Time `json:"timestamp"`
	}
	if err := json.Unmarshal(respBody, &restResp); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "kvHandler.handlePut",
			"failed to parse response")
	}

	// Convert to service response
	return &service.PutResponse{
		ID:        sdkReq.ID,
		Timestamp: restResp.Timestamp.UnixNano(),
	}, nil
}

// handleCompareAndSwap handles CompareAndSwap requests via
// POST /api/v1/entities/{type}/{id}/compare-and-swap.
func (h *kvHandler) handleCompareAndSwap(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*service.CompareAndSwapRequest)
	if !ok || sdkReq == nil {
		return nil, errors.NewInvalidInput("kvHandler.handleCompareAndSwap",
			"invalid request type, expected *service.CompareAndSwapRequest")
	}
	body := map[string][]byte{
		"expect": sdkReq.Expect,
		"set":    sdkReq.Set,
	}
	return h.conditionalPut(ctx, "kvHandler.handleCompareAndSwap",
		sdkReq.EntityType, sdkReq.ID, "compare-and-swap", body)
}

// handlePutIfAbsent handles PutIfAbsent requests via
// POST /api/v1/entities/{type}/{id}/put-if-absent.
func (h *kvHandler) handlePutIfAbsent(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*service.PutIfAbsentRequest)
	if !ok || sdkReq == nil {
		return nil, errors.NewInvalidInput("kvHandler.handlePutIfAbsent",
			"invalid request type, expected *service.PutIfAbsentRequest")
	}
	body := map[string][]byte{
		"data": sdkReq.Data,
	}
	return h.conditionalPut(ctx, "kvHandler.handlePutIfAbsent",
		sdkReq.EntityType, sdkReq.ID, "put-if-absent", body)
}

// conditionalPut posts a conditional write to an entity's {verb} route and
// returns the server's {"applied": bool} as a ConditionalPutResponse.
func (h *kvHandler) conditionalPut(ctx context.Context, op, entityType string, id uint64, verb string, body map[string][]byte) (interface{}, error) {
	url := h.transport.baseURL + h.transport.basePath +
		fmt.Sprintf("/api/v1/entities/%s/%d/%s", neturl.PathEscape(entityType), id, verb)

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInvalidInput, op, "failed to marshal request body")
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, op, "failed to create HTTP request")
	}
	httpReq.Header.Set("Content-Type", "application/json")
	for k, v := range h.transport.headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := h.transport.client.Do(httpReq)
	if err != nil {
		return nil, h.transport.convertHTTPError(err, op)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, op, "failed to read response body")
	}
	if resp.StatusCode >= 400 {
		return nil, h.transport.parseErrorResponse(resp.StatusCode, respBody)
	}

	var condResp service.ConditionalPutResponse
	if err := json.Unmarshal(respBody, &condResp); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, op, "failed to parse response")
	}
	return &condResp, nil
}

// handleGet handles Get requests.
func (h *kvHandler) handleGet(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*service.GetRequest)
	if !ok {
		return nil, errors.NewInvalidInput("kvHandler.handleGet",
			"invalid request type, expected *service.GetRequest")
	}

	// Build URL: /api/v1/kv/{type}/{key}
	url := h.transport.baseURL + h.transport.basePath +
		fmt.Sprintf("/api/v1/kv/%s/%d", sdkReq.EntityType, sdkReq.ID)

	if sdkReq.Consistency > 0 {
		url += "?consistency=" + consistencyToString(sdkReq.Consistency)
	}

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "kvHandler.handleGet",
			"failed to create HTTP request")
	}

	httpReq.Header.Set("Accept", "application/json")
	for k, v := range h.transport.headers {
		httpReq.Header.Set(k, v)
	}

	// Execute request
	resp, err := h.transport.client.Do(httpReq)
	if err != nil {
		return nil, h.transport.convertHTTPError(err, "kvHandler.handleGet")
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "kvHandler.handleGet",
			"failed to read response body")
	}

	if resp.StatusCode == http.StatusNotFound {
		return &service.GetResponse{Found: false}, nil
	}

	if resp.StatusCode >= 400 {
		return nil, h.transport.parseErrorResponse(resp.StatusCode, respBody)
	}

	// Parse response
	var restResp struct {
		EntityType string          `json:"entity_type"`
		Key        string          `json:"key"`
		Value      json.RawMessage `json:"value"`
		Found      bool            `json:"found"`
		Timestamp  time.Time       `json:"timestamp"`
	}
	if err := json.Unmarshal(respBody, &restResp); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "kvHandler.handleGet",
			"failed to parse response")
	}

	// Extract data from value - it might be base64 encoded or raw JSON.
	// Initialize with empty slice for consistent behavior when value is empty.
	data := []byte{}
	if restResp.Value != nil && len(restResp.Value) > 0 {
		// Try to unmarshal as string first (might be base64)
		var strValue string
		if err := json.Unmarshal(restResp.Value, &strValue); err == nil {
			// Try base64 decode
			decoded, err := base64.StdEncoding.DecodeString(strValue)
			if err == nil {
				data = decoded
			} else {
				data = []byte(strValue)
			}
		} else {
			// Use raw JSON bytes
			data = restResp.Value
		}
	}

	return &service.GetResponse{
		Found:     restResp.Found,
		Data:      data,
		Timestamp: restResp.Timestamp.UnixNano(),
	}, nil
}

// handleDelete handles Delete requests.
func (h *kvHandler) handleDelete(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*service.DeleteRequest)
	if !ok {
		return nil, errors.NewInvalidInput("kvHandler.handleDelete",
			"invalid request type, expected *service.DeleteRequest")
	}

	// Build URL: /api/v1/kv/{type}/{key}
	url := h.transport.baseURL + h.transport.basePath +
		fmt.Sprintf("/api/v1/kv/%s/%d", sdkReq.EntityType, sdkReq.ID)

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "kvHandler.handleDelete",
			"failed to create HTTP request")
	}

	for k, v := range h.transport.headers {
		httpReq.Header.Set(k, v)
	}

	// Execute request
	resp, err := h.transport.client.Do(httpReq)
	if err != nil {
		return nil, h.transport.convertHTTPError(err, "kvHandler.handleDelete")
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "kvHandler.handleDelete",
			"failed to read response body")
	}

	if resp.StatusCode >= 400 {
		return nil, h.transport.parseErrorResponse(resp.StatusCode, respBody)
	}

	// Parse response
	var restResp struct {
		EntityType string    `json:"entity_type"`
		Key        string    `json:"key"`
		Deleted    bool      `json:"deleted"`
		Timestamp  time.Time `json:"timestamp"`
	}
	if err := json.Unmarshal(respBody, &restResp); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "kvHandler.handleDelete",
			"failed to parse response")
	}

	return &service.DeleteResponse{
		Deleted:   restResp.Deleted,
		Timestamp: restResp.Timestamp.UnixNano(),
	}, nil
}

// handleList handles List requests.
func (h *kvHandler) handleList(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*service.ListRequest)
	if !ok {
		return nil, errors.NewInvalidInput("kvHandler.handleList",
			"invalid request type, expected *service.ListRequest")
	}

	// Build URL: /api/v1/kv/{type}
	url := h.transport.baseURL + h.transport.basePath +
		fmt.Sprintf("/api/v1/kv/%s", sdkReq.EntityType)

	// Add query parameters
	params := []string{}
	if sdkReq.PageSize > 0 {
		params = append(params, fmt.Sprintf("limit=%d", sdkReq.PageSize))
	}
	if sdkReq.PageToken != "" {
		params = append(params, fmt.Sprintf("cursor=%s", sdkReq.PageToken))
	}
	if sdkReq.Consistency > 0 {
		params = append(params, fmt.Sprintf("consistency=%s", consistencyToString(sdkReq.Consistency)))
	}
	if len(params) > 0 {
		url += "?" + strings.Join(params, "&")
	}

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "kvHandler.handleList",
			"failed to create HTTP request")
	}

	httpReq.Header.Set("Accept", "application/json")
	for k, v := range h.transport.headers {
		httpReq.Header.Set(k, v)
	}

	// Execute request
	resp, err := h.transport.client.Do(httpReq)
	if err != nil {
		return nil, h.transport.convertHTTPError(err, "kvHandler.handleList")
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "kvHandler.handleList",
			"failed to read response body")
	}

	if resp.StatusCode >= 400 {
		return nil, h.transport.parseErrorResponse(resp.StatusCode, respBody)
	}

	// Parse response
	var restResp struct {
		EntityType string `json:"entity_type"`
		Items      []struct {
			Key       string          `json:"key"`
			Value     json.RawMessage `json:"value"`
			Timestamp time.Time       `json:"timestamp"`
		} `json:"items"`
		Total      int    `json:"total"`
		NextCursor string `json:"next_cursor"`
		HasMore    bool   `json:"has_more"`
	}
	if err := json.Unmarshal(respBody, &restResp); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "kvHandler.handleList",
			"failed to parse response")
	}

	// Convert to service response
	entities := make([]*service.Entity, len(restResp.Items))
	for i, item := range restResp.Items {
		// Initialize with empty slice for consistent behavior when value is empty.
		data := []byte{}
		if item.Value != nil && len(item.Value) > 0 {
			// Try to unmarshal as string first (might be base64)
			var strValue string
			if err := json.Unmarshal(item.Value, &strValue); err == nil {
				// Try base64 decode
				decoded, err := base64.StdEncoding.DecodeString(strValue)
				if err == nil {
					data = decoded
				} else {
					data = []byte(strValue)
				}
			} else {
				data = item.Value
			}
		}

		entities[i] = &service.Entity{
			EntityType: sdkReq.EntityType,
			Data:       data,
			Timestamp:  item.Timestamp.UnixNano(),
		}
	}

	return &service.ListResponse{
		Entities:      entities,
		NextPageToken: restResp.NextCursor,
		TotalCount:    uint64(restResp.Total),
	}, nil
}

// consistencyToString converts a consistency level to its string representation.
func consistencyToString(level service.ConsistencyLevel) string {
	switch level {
	case service.ConsistencyLevelStrong:
		return "strong"
	case service.ConsistencyLevelLinearizable:
		return "linearizable"
	case service.ConsistencyLevelEventual:
		return "eventual"
	default:
		return "linearizable"
	}
}
