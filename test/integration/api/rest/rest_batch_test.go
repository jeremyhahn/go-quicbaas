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

//go:build integration
// +build integration

package rest

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// BatchPutRequest represents the REST API batch put request
type BatchPutRequest struct {
	Operations []PutOperation `json:"operations"`
}

type PutOperation struct {
	EntityType string          `json:"entity_type"`
	ID         uint64          `json:"id"`
	Data       json.RawMessage `json:"data"`
}

// BatchDeleteRequest represents the REST API batch delete request
type BatchDeleteRequest struct {
	Operations []DeleteOperation `json:"operations"`
}

type DeleteOperation struct {
	EntityType string `json:"entity_type"`
	ID         uint64 `json:"id"`
}

// BatchPutResponse represents the REST API batch put response
type BatchPutResponse struct {
	Success   bool          `json:"success"`
	Responses []PutResponse `json:"responses"`
	Timestamp int64         `json:"timestamp"`
}

type PutResponse struct {
	Created    bool   `json:"created"`
	EntityType string `json:"entity_type"`
	ID         uint64 `json:"id"`
}

// BatchDeleteResponse represents the REST API batch delete response
type BatchDeleteResponse struct {
	Success   bool             `json:"success"`
	Responses []DeleteResponse `json:"responses"`
	Timestamp int64            `json:"timestamp"`
}

type DeleteResponse struct {
	Deleted    bool   `json:"deleted"`
	EntityType string `json:"entity_type"`
	ID         uint64 `json:"id"`
}

// newTLSHTTPClient creates an HTTP client configured for self-signed TLS certs.
func newTLSHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			TLSClientConfig:     &tls.Config{InsecureSkipVerify: true},
			TLSHandshakeTimeout: 10 * time.Second,
			ForceAttemptHTTP2:   false,
			DialContext: (&net.Dialer{
				Timeout:   10 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
		},
	}
}

// TestRESTBatchPutSuccess tests successful atomic batch put via REST API
func TestRESTBatchPutSuccess(t *testing.T) {
	baseURL := helpers.GetRESTEndpoints()[0]
	client := newTLSHTTPClient(30 * time.Second)

	timestamp := time.Now().UnixNano()

	// Create batch put request
	batchReq := BatchPutRequest{
		Operations: []PutOperation{
			{
				EntityType: "rest_user",
				ID:         uint64(timestamp),
				Data:       json.RawMessage(`{"name":"Alice","email":"alice@example.com"}`),
			},
			{
				EntityType: "rest_profile",
				ID:         uint64(timestamp),
				Data:       json.RawMessage(`{"bio":"Engineer","location":"NYC"}`),
			},
			{
				EntityType: "rest_settings",
				ID:         uint64(timestamp),
				Data:       json.RawMessage(`{"theme":"dark","lang":"en"}`),
			},
		},
	}

	body, err := json.Marshal(batchReq)
	require.NoError(t, err)

	// Execute batch put
	resp, err := client.Post(
		baseURL+"/api/v1/batch/put",
		"application/json",
		bytes.NewBuffer(body),
	)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var batchResp BatchPutResponse
	err = json.NewDecoder(resp.Body).Decode(&batchResp)
	require.NoError(t, err)

	assert.True(t, batchResp.Success)
	assert.Equal(t, 3, len(batchResp.Responses))

	// Verify all operations succeeded
	for i, op := range batchReq.Operations {
		assert.True(t, batchResp.Responses[i].Created)
		assert.Equal(t, op.ID, batchResp.Responses[i].ID)
		assert.Equal(t, op.EntityType, batchResp.Responses[i].EntityType)
	}

	t.Logf("REST batch put succeeded for %d entities", len(batchReq.Operations))

	// Wait for replication
	time.Sleep(500 * time.Millisecond)

	// Verify entities can be retrieved
	for _, op := range batchReq.Operations {
		getResp, err := client.Get(fmt.Sprintf("%s/api/v1/entities/%s/%d",
			baseURL, op.EntityType, op.ID))
		require.NoError(t, err)
		defer getResp.Body.Close()
		assert.Equal(t, http.StatusOK, getResp.StatusCode)
	}
}

// TestRESTBatchDeleteSuccess tests successful atomic batch delete via REST API
func TestRESTBatchDeleteSuccess(t *testing.T) {
	baseURL := helpers.GetRESTEndpoints()[0]
	client := newTLSHTTPClient(30 * time.Second)

	timestamp := time.Now().UnixNano()

	// First create entities
	putReq := BatchPutRequest{
		Operations: []PutOperation{
			{
				EntityType: "rest_del_user",
				ID:         uint64(timestamp),
				Data:       json.RawMessage(`{"name":"Bob"}`),
			},
			{
				EntityType: "rest_del_profile",
				ID:         uint64(timestamp),
				Data:       json.RawMessage(`{"bio":"Test"}`),
			},
		},
	}

	putBody, _ := json.Marshal(putReq)
	putResp, err := client.Post(
		baseURL+"/api/v1/batch/put",
		"application/json",
		bytes.NewBuffer(putBody),
	)
	require.NoError(t, err)
	putResp.Body.Close()

	// Now delete them atomically
	delReq := BatchDeleteRequest{
		Operations: []DeleteOperation{
			{EntityType: "rest_del_user", ID: uint64(timestamp)},
			{EntityType: "rest_del_profile", ID: uint64(timestamp)},
		},
	}

	delBody, err := json.Marshal(delReq)
	require.NoError(t, err)

	delResp, err := client.Post(
		baseURL+"/api/v1/batch/delete",
		"application/json",
		bytes.NewBuffer(delBody),
	)
	require.NoError(t, err)
	defer delResp.Body.Close()

	assert.Equal(t, http.StatusOK, delResp.StatusCode)

	var batchDelResp BatchDeleteResponse
	err = json.NewDecoder(delResp.Body).Decode(&batchDelResp)
	require.NoError(t, err)

	assert.True(t, batchDelResp.Success)
	assert.Equal(t, 2, len(batchDelResp.Responses))

	// Verify all deletions succeeded
	for _, resp := range batchDelResp.Responses {
		assert.True(t, resp.Deleted)
	}

	t.Logf("REST batch delete succeeded for %d entities", len(delReq.Operations))
}

// TestRESTBatchPutLargeSet tests batch put with larger dataset via REST
func TestRESTBatchPutLargeSet(t *testing.T) {
	baseURL := helpers.GetRESTEndpoints()[0]
	client := newTLSHTTPClient(60 * time.Second)

	batchSize := 50
	baseTimestamp := time.Now().UnixNano()

	// Create large batch
	operations := make([]PutOperation, batchSize)
	for i := 0; i < batchSize; i++ {
		operations[i] = PutOperation{
			EntityType: "rest_large_batch",
			ID:         uint64(baseTimestamp + int64(i*1000)),
			Data:       json.RawMessage(fmt.Sprintf(`{"index":%d,"value":"data_%d"}`, i, i)),
		}
	}

	batchReq := BatchPutRequest{Operations: operations}
	body, err := json.Marshal(batchReq)
	require.NoError(t, err)

	// Execute batch
	startTime := time.Now()
	resp, err := client.Post(
		baseURL+"/api/v1/batch/put",
		"application/json",
		bytes.NewBuffer(body),
	)
	duration := time.Since(startTime)

	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var batchResp BatchPutResponse
	err = json.NewDecoder(resp.Body).Decode(&batchResp)
	require.NoError(t, err)

	assert.True(t, batchResp.Success)
	assert.Equal(t, batchSize, len(batchResp.Responses))

	t.Logf("REST batch put of %d entities completed in %v", batchSize, duration)
}

// TestRESTBatchPutUnrelatedEntities tests batch operations on unrelated types
func TestRESTBatchPutUnrelatedEntities(t *testing.T) {
	baseURL := helpers.GetRESTEndpoints()[0]
	client := newTLSHTTPClient(30 * time.Second)

	timestamp := time.Now().UnixNano()

	// Create batch with unrelated entity types
	batchReq := BatchPutRequest{
		Operations: []PutOperation{
			{
				EntityType: "rest_product",
				ID:         uint64(timestamp),
				Data:       json.RawMessage(`{"name":"Keyboard","price":79.99}`),
			},
			{
				EntityType: "rest_customer",
				ID:         uint64(timestamp + 1000),
				Data:       json.RawMessage(`{"name":"Jane Smith"}`),
			},
			{
				EntityType: "rest_invoice",
				ID:         uint64(timestamp + 2000),
				Data:       json.RawMessage(`{"total":299.99,"paid":true}`),
			},
		},
	}

	body, err := json.Marshal(batchReq)
	require.NoError(t, err)

	resp, err := client.Post(
		baseURL+"/api/v1/batch/put",
		"application/json",
		bytes.NewBuffer(body),
	)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var batchResp BatchPutResponse
	err = json.NewDecoder(resp.Body).Decode(&batchResp)
	require.NoError(t, err)

	assert.True(t, batchResp.Success)
	assert.Equal(t, 3, len(batchResp.Responses))

	for _, resp := range batchResp.Responses {
		assert.True(t, resp.Created)
	}

	t.Logf("REST batch put succeeded for %d unrelated entity types", len(batchReq.Operations))
}

// TestRESTBatchEmptyRequest tests error handling for empty batches
func TestRESTBatchEmptyRequest(t *testing.T) {
	baseURL := helpers.GetRESTEndpoints()[0]
	client := newTLSHTTPClient(30 * time.Second)

	// Try empty batch put
	emptyPutReq := BatchPutRequest{Operations: []PutOperation{}}
	body, _ := json.Marshal(emptyPutReq)

	resp, err := client.Post(
		baseURL+"/api/v1/batch/put",
		"application/json",
		bytes.NewBuffer(body),
	)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.NotEqual(t, http.StatusOK, resp.StatusCode, "Empty batch should fail")

	// Try empty batch delete
	emptyDelReq := BatchDeleteRequest{Operations: []DeleteOperation{}}
	body, _ = json.Marshal(emptyDelReq)

	resp, err = client.Post(
		baseURL+"/api/v1/batch/delete",
		"application/json",
		bytes.NewBuffer(body),
	)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.NotEqual(t, http.StatusOK, resp.StatusCode, "Empty batch should fail")

	t.Log("Empty batch requests correctly rejected via REST API")
}

// TestRESTBatchInvalidJSON tests error handling for malformed requests
func TestRESTBatchInvalidJSON(t *testing.T) {
	baseURL := helpers.GetRESTEndpoints()[0]
	client := newTLSHTTPClient(30 * time.Second)

	invalidJSON := []byte(`{"operations": [{"entity_type": "test", "id": "not_a_number"}]}`)

	resp, err := client.Post(
		baseURL+"/api/v1/batch/put",
		"application/json",
		bytes.NewBuffer(invalidJSON),
	)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.NotEqual(t, http.StatusOK, resp.StatusCode, "Invalid JSON should fail")

	t.Log("Invalid JSON correctly rejected via REST API")
}

// TestRESTBatchPutUpdate tests updating existing entities via batch
func TestRESTBatchPutUpdate(t *testing.T) {
	baseURL := helpers.GetRESTEndpoints()[0]
	client := newTLSHTTPClient(30 * time.Second)

	timestamp := time.Now().UnixNano()

	// Create initial entities
	initialReq := BatchPutRequest{
		Operations: []PutOperation{
			{
				EntityType: "rest_update",
				ID:         uint64(timestamp),
				Data:       json.RawMessage(`{"version":1,"status":"draft"}`),
			},
			{
				EntityType: "rest_update",
				ID:         uint64(timestamp + 1000),
				Data:       json.RawMessage(`{"version":1,"status":"draft"}`),
			},
		},
	}

	body, _ := json.Marshal(initialReq)
	resp, err := client.Post(
		baseURL+"/api/v1/batch/put",
		"application/json",
		bytes.NewBuffer(body),
	)
	require.NoError(t, err)
	resp.Body.Close()

	// Update atomically
	updateReq := BatchPutRequest{
		Operations: []PutOperation{
			{
				EntityType: "rest_update",
				ID:         uint64(timestamp),
				Data:       json.RawMessage(`{"version":2,"status":"published"}`),
			},
			{
				EntityType: "rest_update",
				ID:         uint64(timestamp + 1000),
				Data:       json.RawMessage(`{"version":2,"status":"published"}`),
			},
		},
	}

	body, _ = json.Marshal(updateReq)
	updateResp, err := client.Post(
		baseURL+"/api/v1/batch/put",
		"application/json",
		bytes.NewBuffer(body),
	)
	require.NoError(t, err)
	defer updateResp.Body.Close()

	assert.Equal(t, http.StatusOK, updateResp.StatusCode)

	var batchResp BatchPutResponse
	err = json.NewDecoder(updateResp.Body).Decode(&batchResp)
	require.NoError(t, err)

	assert.True(t, batchResp.Success)
	assert.Equal(t, 2, len(batchResp.Responses))

	t.Logf("REST batch update succeeded for %d entities", len(updateReq.Operations))
}

// TestRESTBatchSequentialOperations tests sequential batch operations via REST
func TestRESTBatchSequentialOperations(t *testing.T) {
	baseURL := helpers.GetRESTEndpoints()[0]
	client := newTLSHTTPClient(30 * time.Second)

	timestamp := time.Now().UnixNano()

	// Step 1: Create entities
	createReq := BatchPutRequest{
		Operations: []PutOperation{
			{
				EntityType: "rest_seq",
				ID:         uint64(timestamp),
				Data:       json.RawMessage(`{"step":1}`),
			},
			{
				EntityType: "rest_seq",
				ID:         uint64(timestamp + 1000),
				Data:       json.RawMessage(`{"step":1}`),
			},
		},
	}

	body, _ := json.Marshal(createReq)
	resp, err := client.Post(baseURL+"/api/v1/batch/put", "application/json", bytes.NewBuffer(body))
	require.NoError(t, err)
	resp.Body.Close()

	// Step 2: Update first entity
	updateReq := BatchPutRequest{
		Operations: []PutOperation{
			{
				EntityType: "rest_seq",
				ID:         uint64(timestamp),
				Data:       json.RawMessage(`{"step":2,"updated":true}`),
			},
		},
	}

	body, _ = json.Marshal(updateReq)
	resp, err = client.Post(baseURL+"/api/v1/batch/put", "application/json", bytes.NewBuffer(body))
	require.NoError(t, err)
	resp.Body.Close()

	// Step 3: Delete second entity
	delReq := BatchDeleteRequest{
		Operations: []DeleteOperation{
			{EntityType: "rest_seq", ID: uint64(timestamp + 1000)},
		},
	}

	body, _ = json.Marshal(delReq)
	resp, err = client.Post(baseURL+"/api/v1/batch/delete", "application/json", bytes.NewBuffer(body))
	require.NoError(t, err)
	resp.Body.Close()

	// Verify final state
	getResp, err := client.Get(fmt.Sprintf("%s/api/v1/entities/rest_seq/%d", baseURL, uint64(timestamp)))
	require.NoError(t, err)
	defer getResp.Body.Close()
	assert.Equal(t, http.StatusOK, getResp.StatusCode)

	t.Log("REST sequential batch operations succeeded")
}

// Helper to get REST endpoint - removed duplicate, use server_integration_test.go version
