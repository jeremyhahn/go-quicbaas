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

package batch

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// =============================================================================
// Result Structs
// =============================================================================

// BatchPutResult contains the result of a batch put operation.
type BatchPutResult struct {
	Results   []*PutResultItem `json:"results"`
	Timestamp int64            `json:"timestamp"`
}

// PutResultItem contains the result of a single put in a batch.
type PutResultItem struct {
	EntityType string `json:"entity_type"`
	ID         uint64 `json:"id"`
	Revision   int64  `json:"revision"`
	Version    uint64 `json:"version"`
	Success    bool   `json:"success"`
	Error      string `json:"error,omitempty"`
}

// BatchGetResult contains the result of a batch get operation.
type BatchGetResult struct {
	Results   []*GetResultItem `json:"results"`
	Timestamp int64            `json:"timestamp"`
}

// GetResultItem contains the result of a single get in a batch.
type GetResultItem struct {
	EntityType string `json:"entity_type"`
	ID         uint64 `json:"id"`
	Data       []byte `json:"data"`
	Revision   int64  `json:"revision"`
	Version    uint64 `json:"version"`
	Found      bool   `json:"found"`
	Error      string `json:"error,omitempty"`
}

// BatchDeleteResult contains the result of a batch delete operation.
type BatchDeleteResult struct {
	Results   []*DeleteResultItem `json:"results"`
	Timestamp int64               `json:"timestamp"`
}

// DeleteResultItem contains the result of a single delete in a batch.
type DeleteResultItem struct {
	EntityType string `json:"entity_type"`
	ID         uint64 `json:"id"`
	Deleted    bool   `json:"deleted"`
	Error      string `json:"error,omitempty"`
}

// =============================================================================
// Protocol Interface
// =============================================================================

// batchProtocol abstracts the different protocols for testing.
type batchProtocol interface {
	Setup(t *testing.T, ctx context.Context) error
	Teardown(t *testing.T) error
	BatchPut(ctx context.Context, requests []*service.PutRequest) (*BatchPutResult, error)
	BatchGet(ctx context.Context, requests []*service.GetRequest) (*BatchGetResult, error)
	BatchDelete(ctx context.Context, requests []*service.DeleteRequest) (*BatchDeleteResult, error)
	Name() string
}

// =============================================================================
// Main Test Function
// =============================================================================

// TestBatchMultiProtocol verifies that batch operations work identically
// across all supported protocols: Embedded, REST, gRPC, QUIC, Unix Socket, and MCP.
func TestBatchMultiProtocol(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if helpers.IsEmbeddedAvailable() {
		t.Run("Embedded", func(t *testing.T) {
			testBatchProtocol(t, ctx, &embeddedBatchClient{})
		})
	}

	t.Run("REST", func(t *testing.T) {
		testBatchProtocol(t, ctx, &restBatchClient{})
	})

	t.Run("gRPC", func(t *testing.T) {
		testBatchProtocol(t, ctx, &grpcBatchClient{})
	})

	t.Run("UnixSocket", func(t *testing.T) {
		testBatchProtocol(t, ctx, &unixSocketBatchClient{})
	})

	// NOTE: no MCP leg here. quicbaas retired its bespoke JSON-RPC MCP
	// listener in favor of go-qrdb's unified multi-protocol server; the
	// unified server's MCP surface has no finalized docker endpoint
	// convention yet, so this suite does not exercise it (deferred).
}

// testBatchProtocol runs the complete batch test suite against a protocol.
func testBatchProtocol(t *testing.T, ctx context.Context, protocol batchProtocol) {
	t.Helper()

	err := protocol.Setup(t, ctx)
	require.NoError(t, err, "%s protocol setup must succeed", protocol.Name())
	defer func() {
		if teardownErr := protocol.Teardown(t); teardownErr != nil {
			t.Logf("Warning: teardown error for %s: %v", protocol.Name(), teardownErr)
		}
	}()

	basePrefix := "batch_mp_" + protocol.Name() + "_" + strconv.FormatInt(time.Now().UnixNano(), 10)

	t.Run("BatchPut", func(t *testing.T) {
		testBatchPut(t, ctx, protocol, basePrefix+"_put")
	})

	t.Run("BatchGet", func(t *testing.T) {
		testBatchGet(t, ctx, protocol, basePrefix+"_get")
	})

	t.Run("BatchDelete", func(t *testing.T) {
		testBatchDelete(t, ctx, protocol, basePrefix+"_delete")
	})

	t.Run("BatchPutGetDelete_Lifecycle", func(t *testing.T) {
		testBatchLifecycle(t, ctx, protocol, basePrefix+"_lifecycle")
	})

	t.Run("BatchPut_LargeBatch", func(t *testing.T) {
		testBatchPutLarge(t, ctx, protocol, basePrefix+"_large")
	})

	t.Run("BatchGet_MixedResults", func(t *testing.T) {
		testBatchGetMixed(t, ctx, protocol, basePrefix+"_mixed")
	})

	t.Run("BatchPut_EmptyBatch", func(t *testing.T) {
		testBatchPutEmpty(t, ctx, protocol)
	})
}

// =============================================================================
// Test Functions
// =============================================================================

func testBatchPut(t *testing.T, ctx context.Context, protocol batchProtocol, entityType string) {
	requests := []*service.PutRequest{
		{EntityType: entityType, ID: 1, Data: []byte(`{"name":"item1","value":100}`)},
		{EntityType: entityType, ID: 2, Data: []byte(`{"name":"item2","value":200}`)},
		{EntityType: entityType, ID: 3, Data: []byte(`{"name":"item3","value":300}`)},
	}

	result, err := protocol.BatchPut(ctx, requests)
	require.NoError(t, err, "%s: BatchPut must be implemented", protocol.Name())
	require.NotNil(t, result, "%s: BatchPut returned nil result", protocol.Name())
	assert.Len(t, result.Results, 3, "%s: Expected 3 results", protocol.Name())

	for i, r := range result.Results {
		assert.True(t, r.Success, "%s: Item %d should succeed", protocol.Name(), i)
		assert.Equal(t, entityType, r.EntityType, "%s: EntityType mismatch for item %d", protocol.Name(), i)
		assert.Equal(t, uint64(i+1), r.ID, "%s: ID mismatch for item %d", protocol.Name(), i)
	}

	t.Logf("%s: BatchPut succeeded with %d items", protocol.Name(), len(result.Results))
}

func testBatchGet(t *testing.T, ctx context.Context, protocol batchProtocol, entityType string) {
	// First put some data
	putRequests := []*service.PutRequest{
		{EntityType: entityType, ID: 10, Data: []byte(`{"name":"get1"}`)},
		{EntityType: entityType, ID: 20, Data: []byte(`{"name":"get2"}`)},
	}

	_, err := protocol.BatchPut(ctx, putRequests)
	require.NoError(t, err, "%s: BatchPut must be implemented for setup", protocol.Name())

	// Now get the data
	getRequests := []*service.GetRequest{
		{EntityType: entityType, ID: 10},
		{EntityType: entityType, ID: 20},
	}

	result, err := protocol.BatchGet(ctx, getRequests)
	require.NoError(t, err, "%s: BatchGet must be implemented", protocol.Name())
	require.NotNil(t, result, "%s: BatchGet returned nil result", protocol.Name())
	assert.Len(t, result.Results, 2, "%s: Expected 2 results", protocol.Name())

	for _, r := range result.Results {
		assert.True(t, r.Found, "%s: Item %d should be found", protocol.Name(), r.ID)
		assert.NotEmpty(t, r.Data, "%s: Item %d should have data", protocol.Name(), r.ID)
	}

	t.Logf("%s: BatchGet succeeded with %d items", protocol.Name(), len(result.Results))
}

func testBatchDelete(t *testing.T, ctx context.Context, protocol batchProtocol, entityType string) {
	// First put some data
	putRequests := []*service.PutRequest{
		{EntityType: entityType, ID: 100, Data: []byte(`{"name":"delete1"}`)},
		{EntityType: entityType, ID: 200, Data: []byte(`{"name":"delete2"}`)},
	}

	_, err := protocol.BatchPut(ctx, putRequests)
	require.NoError(t, err, "%s: BatchPut must be implemented for setup", protocol.Name())

	// Now delete the data
	deleteRequests := []*service.DeleteRequest{
		{EntityType: entityType, ID: 100},
		{EntityType: entityType, ID: 200},
	}

	result, err := protocol.BatchDelete(ctx, deleteRequests)
	require.NoError(t, err, "%s: BatchDelete must be implemented", protocol.Name())
	require.NotNil(t, result, "%s: BatchDelete returned nil result", protocol.Name())
	assert.Len(t, result.Results, 2, "%s: Expected 2 results", protocol.Name())

	for _, r := range result.Results {
		assert.True(t, r.Deleted, "%s: Item %d should be deleted", protocol.Name(), r.ID)
	}

	// Verify deletion by trying to get the data
	getRequests := []*service.GetRequest{
		{EntityType: entityType, ID: 100},
		{EntityType: entityType, ID: 200},
	}

	getResult, err := protocol.BatchGet(ctx, getRequests)
	require.NoError(t, err, "%s: BatchGet must be implemented for verification", protocol.Name())
	for _, r := range getResult.Results {
		assert.False(t, r.Found, "%s: Item %d should not be found after delete", protocol.Name(), r.ID)
	}

	t.Logf("%s: BatchDelete succeeded with %d items", protocol.Name(), len(result.Results))
}

func testBatchLifecycle(t *testing.T, ctx context.Context, protocol batchProtocol, entityType string) {
	// Create
	putRequests := []*service.PutRequest{
		{EntityType: entityType, ID: 1000, Data: []byte(`{"stage":"created"}`)},
		{EntityType: entityType, ID: 1001, Data: []byte(`{"stage":"created"}`)},
	}

	putResult, err := protocol.BatchPut(ctx, putRequests)
	require.NoError(t, err, "%s: BatchPut must be implemented for create", protocol.Name())
	assert.Len(t, putResult.Results, 2)

	// Read
	getRequests := []*service.GetRequest{
		{EntityType: entityType, ID: 1000},
		{EntityType: entityType, ID: 1001},
	}

	getResult, err := protocol.BatchGet(ctx, getRequests)
	require.NoError(t, err, "%s: BatchGet must be implemented for read", protocol.Name())
	for _, r := range getResult.Results {
		assert.True(t, r.Found)
	}

	// Update
	updateRequests := []*service.PutRequest{
		{EntityType: entityType, ID: 1000, Data: []byte(`{"stage":"updated"}`)},
		{EntityType: entityType, ID: 1001, Data: []byte(`{"stage":"updated"}`)},
	}

	_, err = protocol.BatchPut(ctx, updateRequests)
	require.NoError(t, err, "%s: Update failed", protocol.Name())

	// Delete
	deleteRequests := []*service.DeleteRequest{
		{EntityType: entityType, ID: 1000},
		{EntityType: entityType, ID: 1001},
	}

	deleteResult, err := protocol.BatchDelete(ctx, deleteRequests)
	require.NoError(t, err, "%s: BatchDelete must be implemented for delete", protocol.Name())
	for _, r := range deleteResult.Results {
		assert.True(t, r.Deleted)
	}

	t.Logf("%s: Full lifecycle test passed", protocol.Name())
}

func testBatchPutLarge(t *testing.T, ctx context.Context, protocol batchProtocol, entityType string) {
	numItems := 50
	requests := make([]*service.PutRequest, numItems)

	for i := 0; i < numItems; i++ {
		iStr := strconv.Itoa(i)
		requests[i] = &service.PutRequest{
			EntityType: entityType,
			ID:         uint64(i + 1),
			Data:       []byte(`{"index":` + iStr + `,"data":"item_` + iStr + `"}`),
		}
	}

	result, err := protocol.BatchPut(ctx, requests)
	require.NoError(t, err, "%s: Large BatchPut must be implemented", protocol.Name())
	assert.Len(t, result.Results, numItems, "%s: Expected %d results", protocol.Name(), numItems)

	successCount := 0
	for _, r := range result.Results {
		if r.Success {
			successCount++
		}
	}
	assert.Equal(t, numItems, successCount, "%s: All items should succeed", protocol.Name())

	t.Logf("%s: Large batch put succeeded with %d items", protocol.Name(), successCount)
}

func testBatchGetMixed(t *testing.T, ctx context.Context, protocol batchProtocol, entityType string) {
	// Put only some items
	putRequests := []*service.PutRequest{
		{EntityType: entityType, ID: 5000, Data: []byte(`{"exists":true}`)},
		{EntityType: entityType, ID: 5002, Data: []byte(`{"exists":true}`)},
	}

	_, err := protocol.BatchPut(ctx, putRequests)
	require.NoError(t, err, "%s: BatchPut must be implemented for setup", protocol.Name())

	// Get mix of existing and non-existing
	getRequests := []*service.GetRequest{
		{EntityType: entityType, ID: 5000}, // exists
		{EntityType: entityType, ID: 5001}, // does not exist
		{EntityType: entityType, ID: 5002}, // exists
		{EntityType: entityType, ID: 5003}, // does not exist
	}

	result, err := protocol.BatchGet(ctx, getRequests)
	require.NoError(t, err, "%s: BatchGet must be implemented for mixed results", protocol.Name())
	assert.Len(t, result.Results, 4, "%s: Expected 4 results", protocol.Name())

	foundCount := 0
	notFoundCount := 0
	for _, r := range result.Results {
		if r.Found {
			foundCount++
		} else {
			notFoundCount++
		}
	}

	assert.Equal(t, 2, foundCount, "%s: Expected 2 found items", protocol.Name())
	assert.Equal(t, 2, notFoundCount, "%s: Expected 2 not found items", protocol.Name())

	t.Logf("%s: Mixed batch get: %d found, %d not found", protocol.Name(), foundCount, notFoundCount)
}

func testBatchPutEmpty(t *testing.T, ctx context.Context, protocol batchProtocol) {
	requests := []*service.PutRequest{}

	result, err := protocol.BatchPut(ctx, requests)
	// Empty batch may return error or empty result depending on implementation
	if err == nil {
		assert.Empty(t, result.Results, "%s: Empty batch should return empty results", protocol.Name())
	}

	t.Logf("%s: Empty batch test completed", protocol.Name())
}

// =============================================================================
// Embedded Protocol Implementation
// =============================================================================

// ErrEmbeddedClientNotInitialized is returned when the embedded client is not initialized.
type ErrEmbeddedClientNotInitialized struct{}

func (e ErrEmbeddedClientNotInitialized) Error() string {
	return "embedded client not initialized"
}

type embeddedBatchClient struct {
	client quicbaas.Client
}

func (c *embeddedBatchClient) Name() string { return "Embedded" }

func (c *embeddedBatchClient) Setup(t *testing.T, ctx context.Context) error {
	svc, err := helpers.GetEmbeddedService(t)
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "failed to get embedded service", Cause: err}
	}

	client, err := quicbaas.NewClient("", quicbaas.WithEmbedded(svc), helpers.EmbeddedSystemIdentity())
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "failed to create embedded client", Cause: err}
	}

	c.client = client
	return nil
}

func (c *embeddedBatchClient) Teardown(t *testing.T) error {
	if c.client != nil {
		return c.client.Close()
	}
	return nil
}

func (c *embeddedBatchClient) BatchPut(ctx context.Context, requests []*service.PutRequest) (*BatchPutResult, error) {
	if c.client == nil {
		return nil, ErrEmbeddedClientNotInitialized{}
	}

	resp, err := c.client.Batch().Put(ctx, requests)
	if err != nil {
		return nil, err
	}

	result := &BatchPutResult{
		Results:   make([]*PutResultItem, len(resp.Responses)),
		Timestamp: time.Now().UnixNano(),
	}

	for i, r := range resp.Responses {
		result.Results[i] = &PutResultItem{
			EntityType: r.EntityType,
			ID:         r.ID,
			Revision:   r.Revision,
			Version:    r.Version,
			Success:    r.Success,
			Error:      r.Error,
		}
	}

	return result, nil
}

func (c *embeddedBatchClient) BatchGet(ctx context.Context, requests []*service.GetRequest) (*BatchGetResult, error) {
	if c.client == nil {
		return nil, ErrEmbeddedClientNotInitialized{}
	}

	resp, err := c.client.Batch().Get(ctx, requests)
	if err != nil {
		return nil, err
	}

	result := &BatchGetResult{
		Results:   make([]*GetResultItem, len(resp.Responses)),
		Timestamp: time.Now().UnixNano(),
	}

	for i, r := range resp.Responses {
		result.Results[i] = &GetResultItem{
			EntityType: r.EntityType,
			ID:         r.ID,
			Data:       r.Data,
			Revision:   r.Revision,
			Version:    r.Version,
			Found:      r.Found,
			Error:      r.Error,
		}
	}

	return result, nil
}

func (c *embeddedBatchClient) BatchDelete(ctx context.Context, requests []*service.DeleteRequest) (*BatchDeleteResult, error) {
	if c.client == nil {
		return nil, ErrEmbeddedClientNotInitialized{}
	}

	resp, err := c.client.Batch().Delete(ctx, requests)
	if err != nil {
		return nil, err
	}

	result := &BatchDeleteResult{
		Results:   make([]*DeleteResultItem, len(resp.Responses)),
		Timestamp: time.Now().UnixNano(),
	}

	for i, r := range resp.Responses {
		result.Results[i] = &DeleteResultItem{
			EntityType: r.EntityType,
			ID:         r.ID,
			Deleted:    r.Deleted,
			Error:      r.Error,
		}
	}

	return result, nil
}

// =============================================================================
// REST Protocol Implementation
// =============================================================================

type restBatchClient struct {
	baseURL    string
	httpClient *http.Client
}

func (c *restBatchClient) Name() string { return "REST" }

func (c *restBatchClient) Setup(t *testing.T, ctx context.Context) error {
	endpoints := helpers.GetRESTEndpoints()
	if len(endpoints) == 0 {
		return &qberrors.MessageError{Message: "no REST endpoints available"}
	}
	c.baseURL = endpoints[0]
	c.httpClient = helpers.NewDataPlaneHTTPClient(t, 30*time.Second)

	// Probe to verify batch operations are available
	// REST API supports /batch/put and /batch/delete but not /batch/get
	// We only need one working operation for the protocol to be "available"
	probeReq := []*service.PutRequest{
		{EntityType: "probe_test", ID: 999999, Data: []byte(`{"probe":true}`)},
	}
	_, err := c.BatchPut(ctx, probeReq)
	if err != nil {
		// Check if it's a connectivity error vs not implemented
		if strings.Contains(err.Error(), "connection refused") ||
			strings.Contains(err.Error(), "no such host") {
			return &qberrors.WrappedMessageError{Message: "REST endpoint not available", Cause: err}
		}
		// Other errors may be OK - batch might work but probe failed for other reasons
	}

	return nil
}

func (c *restBatchClient) Teardown(t *testing.T) error {
	return nil
}

func (c *restBatchClient) BatchPut(ctx context.Context, requests []*service.PutRequest) (*BatchPutResult, error) {
	type restPutOperation struct {
		EntityType string `json:"entity_type"`
		ID         uint64 `json:"id"`
		Data       []byte `json:"data"`
	}
	type restBatchRequest struct {
		Operations []restPutOperation `json:"operations"`
	}

	// REST API response types
	type restPutResponse struct {
		ID         uint64    `json:"id"`
		EntityType string    `json:"entity_type"`
		Created    bool      `json:"created"`
		Timestamp  time.Time `json:"timestamp"`
	}
	type restBatchPutResponse struct {
		Success   bool              `json:"success"`
		Responses []restPutResponse `json:"responses"`
		Timestamp int64             `json:"timestamp"`
	}

	restOps := make([]restPutOperation, len(requests))
	for i, r := range requests {
		restOps[i] = restPutOperation{
			EntityType: r.EntityType,
			ID:         r.ID,
			Data:       r.Data,
		}
	}

	body, err := json.Marshal(restBatchRequest{Operations: restOps})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/api/v1/batch/put", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, &qberrors.MessageError{Message: "REST BatchPut failed with status " + strconv.Itoa(resp.StatusCode) + ": " + string(respBody)}
	}

	var restResp restBatchPutResponse
	if err := json.Unmarshal(respBody, &restResp); err != nil {
		return nil, err
	}

	// Convert REST response to common BatchPutResult format
	result := &BatchPutResult{
		Results:   make([]*PutResultItem, len(restResp.Responses)),
		Timestamp: restResp.Timestamp,
	}

	for i, r := range restResp.Responses {
		result.Results[i] = &PutResultItem{
			EntityType: r.EntityType,
			ID:         r.ID,
			Success:    r.Created,
		}
	}

	return result, nil
}

func (c *restBatchClient) BatchGet(ctx context.Context, requests []*service.GetRequest) (*BatchGetResult, error) {
	type restGetOperation struct {
		EntityType string `json:"entity_type"`
		ID         uint64 `json:"id"`
	}
	type restBatchRequest struct {
		Operations []restGetOperation `json:"operations"`
	}

	type restGetItemResponse struct {
		ID         uint64          `json:"id"`
		EntityType string          `json:"entity_type"`
		Data       json.RawMessage `json:"data,omitempty"`
		Found      bool            `json:"found"`
		Timestamp  time.Time       `json:"timestamp,omitempty"`
	}
	type restBatchGetResponse struct {
		Success   bool                  `json:"success"`
		Responses []restGetItemResponse `json:"responses"`
		Timestamp int64                 `json:"timestamp"`
	}

	restOps := make([]restGetOperation, len(requests))
	for i, r := range requests {
		restOps[i] = restGetOperation{
			EntityType: r.EntityType,
			ID:         r.ID,
		}
	}

	body, err := json.Marshal(restBatchRequest{Operations: restOps})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/api/v1/batch/get", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, &qberrors.MessageError{Message: "REST BatchGet failed with status " + strconv.Itoa(resp.StatusCode) + ": " + string(respBody)}
	}

	var restResp restBatchGetResponse
	if err := json.Unmarshal(respBody, &restResp); err != nil {
		return nil, err
	}

	result := &BatchGetResult{
		Results:   make([]*GetResultItem, len(restResp.Responses)),
		Timestamp: restResp.Timestamp,
	}

	for i, r := range restResp.Responses {
		result.Results[i] = &GetResultItem{
			EntityType: r.EntityType,
			ID:         r.ID,
			Data:       r.Data,
			Found:      r.Found,
		}
	}

	return result, nil
}

func (c *restBatchClient) BatchDelete(ctx context.Context, requests []*service.DeleteRequest) (*BatchDeleteResult, error) {
	type restDeleteOperation struct {
		EntityType string `json:"entity_type"`
		ID         uint64 `json:"id"`
	}
	type restBatchRequest struct {
		Operations []restDeleteOperation `json:"operations"`
	}

	// REST API response types
	type restDeleteResponse struct {
		ID         uint64    `json:"id"`
		EntityType string    `json:"entity_type"`
		Deleted    bool      `json:"deleted"`
		Timestamp  time.Time `json:"timestamp"`
	}
	type restBatchDeleteResponse struct {
		Success   bool                 `json:"success"`
		Responses []restDeleteResponse `json:"responses"`
		Timestamp int64                `json:"timestamp"`
	}

	restOps := make([]restDeleteOperation, len(requests))
	for i, r := range requests {
		restOps[i] = restDeleteOperation{
			EntityType: r.EntityType,
			ID:         r.ID,
		}
	}

	body, err := json.Marshal(restBatchRequest{Operations: restOps})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/api/v1/batch/delete", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, &qberrors.MessageError{Message: "REST BatchDelete failed with status " + strconv.Itoa(resp.StatusCode) + ": " + string(respBody)}
	}

	var restResp restBatchDeleteResponse
	if err := json.Unmarshal(respBody, &restResp); err != nil {
		return nil, err
	}

	// Convert REST response to common BatchDeleteResult format
	result := &BatchDeleteResult{
		Results:   make([]*DeleteResultItem, len(restResp.Responses)),
		Timestamp: restResp.Timestamp,
	}

	for i, r := range restResp.Responses {
		result.Results[i] = &DeleteResultItem{
			EntityType: r.EntityType,
			ID:         r.ID,
			Deleted:    r.Deleted,
		}
	}

	return result, nil
}

// =============================================================================
// gRPC Protocol Implementation
// =============================================================================

type grpcBatchClient struct {
	conn   *grpc.ClientConn
	client pb.QuicBaaSClient
}

func (c *grpcBatchClient) Name() string { return "gRPC" }

func (c *grpcBatchClient) Setup(t *testing.T, ctx context.Context) error {
	endpoints := helpers.GetClusterEndpoints()
	if len(endpoints) == 0 {
		return &qberrors.MessageError{Message: "no gRPC endpoints available"}
	}

	// Create a context with timeout to prevent indefinite blocking on dial
	dialCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	tlsCfg, err := helpers.IntegrationTLSConfig()
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "load integration TLS config", Cause: err}
	}
	conn, err := grpc.DialContext(dialCtx, endpoints[0],
		grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)),
		grpc.WithPerRPCCredentials(helpers.DataPlaneGRPCCredentials(t)),
		grpc.WithBlock(),
	)
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "gRPC dial to " + endpoints[0] + " failed", Cause: err}
	}

	c.conn = conn
	c.client = pb.NewQuicBaaSClient(conn)
	return nil
}

func (c *grpcBatchClient) Teardown(t *testing.T) error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

func (c *grpcBatchClient) BatchPut(ctx context.Context, requests []*service.PutRequest) (*BatchPutResult, error) {
	pbRequests := make([]*pb.PutRequest, len(requests))
	for i, r := range requests {
		pbRequests[i] = &pb.PutRequest{
			EntityType: r.EntityType,
			Id:         r.ID,
			Data:       r.Data,
		}
	}

	resp, err := c.client.BatchPut(ctx, &pb.BatchPutRequest{Requests: pbRequests})
	if err != nil {
		return nil, err
	}

	result := &BatchPutResult{
		Results:   make([]*PutResultItem, len(resp.Responses)),
		Timestamp: time.Now().UnixNano(),
	}

	for i, r := range resp.Responses {
		var entityType string
		if i < len(requests) {
			entityType = requests[i].EntityType
		}
		result.Results[i] = &PutResultItem{
			EntityType: entityType,
			ID:         r.Id,
			Version:    r.Version,
			Success:    true, // Proto PutResponse doesn't have Success field, assume success if no error
		}
	}

	return result, nil
}

func (c *grpcBatchClient) BatchGet(ctx context.Context, requests []*service.GetRequest) (*BatchGetResult, error) {
	pbRequests := make([]*pb.GetRequest, len(requests))
	for i, r := range requests {
		pbRequests[i] = &pb.GetRequest{
			EntityType: r.EntityType,
			Id:         r.ID,
		}
	}

	resp, err := c.client.BatchGet(ctx, &pb.BatchGetRequest{Requests: pbRequests})
	if err != nil {
		return nil, err
	}

	result := &BatchGetResult{
		Results:   make([]*GetResultItem, len(resp.Responses)),
		Timestamp: time.Now().UnixNano(),
	}

	for i, r := range resp.Responses {
		var entityType string
		if i < len(requests) {
			entityType = requests[i].EntityType
		}
		result.Results[i] = &GetResultItem{
			EntityType: entityType,
			ID:         r.Id,
			Data:       r.Data,
			Version:    r.Version,
			Found:      r.Found,
		}
	}

	return result, nil
}

func (c *grpcBatchClient) BatchDelete(ctx context.Context, requests []*service.DeleteRequest) (*BatchDeleteResult, error) {
	pbRequests := make([]*pb.DeleteRequest, len(requests))
	for i, r := range requests {
		pbRequests[i] = &pb.DeleteRequest{
			EntityType: r.EntityType,
			Id:         r.ID,
		}
	}

	resp, err := c.client.BatchDelete(ctx, &pb.BatchDeleteRequest{Requests: pbRequests})
	if err != nil {
		return nil, err
	}

	result := &BatchDeleteResult{
		Results:   make([]*DeleteResultItem, len(resp.Responses)),
		Timestamp: time.Now().UnixNano(),
	}

	for i, r := range resp.Responses {
		var entityType string
		if i < len(requests) {
			entityType = requests[i].EntityType
		}
		result.Results[i] = &DeleteResultItem{
			EntityType: entityType,
			ID:         requests[i].ID,
			Deleted:    r.Deleted,
		}
	}

	return result, nil
}

// =============================================================================
// Unix Socket Protocol Implementation
// =============================================================================

type unixSocketBatchClient struct {
	conn   *grpc.ClientConn
	client pb.QuicBaaSClient
}

func (c *unixSocketBatchClient) Name() string { return "UnixSocket" }

func (c *unixSocketBatchClient) Setup(t *testing.T, ctx context.Context) error {
	socketPath := helpers.GetUnixSocketPath()
	if socketPath == "" {
		return &qberrors.MessageError{Message: "no Unix socket path available"}
	}

	// Create a context with timeout to prevent indefinite blocking on dial
	dialCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	tlsCfg, err := helpers.IntegrationTLSConfig()
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "load integration TLS config", Cause: err}
	}
	conn, err := grpc.DialContext(dialCtx,
		"unix://"+socketPath,
		grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)),
		grpc.WithPerRPCCredentials(helpers.DataPlaneGRPCCredentials(t)),
		grpc.WithBlock(),
		grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
			return net.DialTimeout("unix", socketPath, 10*time.Second)
		}),
	)
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "Unix socket dial to " + socketPath + " failed", Cause: err}
	}

	c.conn = conn
	c.client = pb.NewQuicBaaSClient(conn)
	return nil
}

func (c *unixSocketBatchClient) Teardown(t *testing.T) error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

func (c *unixSocketBatchClient) BatchPut(ctx context.Context, requests []*service.PutRequest) (*BatchPutResult, error) {
	pbRequests := make([]*pb.PutRequest, len(requests))
	for i, r := range requests {
		pbRequests[i] = &pb.PutRequest{
			EntityType: r.EntityType,
			Id:         r.ID,
			Data:       r.Data,
		}
	}

	resp, err := c.client.BatchPut(ctx, &pb.BatchPutRequest{Requests: pbRequests})
	if err != nil {
		return nil, err
	}

	result := &BatchPutResult{
		Results:   make([]*PutResultItem, len(resp.Responses)),
		Timestamp: time.Now().UnixNano(),
	}

	for i, r := range resp.Responses {
		var entityType string
		if i < len(requests) {
			entityType = requests[i].EntityType
		}
		result.Results[i] = &PutResultItem{
			EntityType: entityType,
			ID:         r.Id,
			Version:    r.Version,
			Success:    true, // Proto PutResponse doesn't have Success field, assume success if no error
		}
	}

	return result, nil
}

func (c *unixSocketBatchClient) BatchGet(ctx context.Context, requests []*service.GetRequest) (*BatchGetResult, error) {
	pbRequests := make([]*pb.GetRequest, len(requests))
	for i, r := range requests {
		pbRequests[i] = &pb.GetRequest{
			EntityType: r.EntityType,
			Id:         r.ID,
		}
	}

	resp, err := c.client.BatchGet(ctx, &pb.BatchGetRequest{Requests: pbRequests})
	if err != nil {
		return nil, err
	}

	result := &BatchGetResult{
		Results:   make([]*GetResultItem, len(resp.Responses)),
		Timestamp: time.Now().UnixNano(),
	}

	for i, r := range resp.Responses {
		var entityType string
		if i < len(requests) {
			entityType = requests[i].EntityType
		}
		result.Results[i] = &GetResultItem{
			EntityType: entityType,
			ID:         r.Id,
			Data:       r.Data,
			Version:    r.Version,
			Found:      r.Found,
		}
	}

	return result, nil
}

func (c *unixSocketBatchClient) BatchDelete(ctx context.Context, requests []*service.DeleteRequest) (*BatchDeleteResult, error) {
	pbRequests := make([]*pb.DeleteRequest, len(requests))
	for i, r := range requests {
		pbRequests[i] = &pb.DeleteRequest{
			EntityType: r.EntityType,
			Id:         r.ID,
		}
	}

	resp, err := c.client.BatchDelete(ctx, &pb.BatchDeleteRequest{Requests: pbRequests})
	if err != nil {
		return nil, err
	}

	result := &BatchDeleteResult{
		Results:   make([]*DeleteResultItem, len(resp.Responses)),
		Timestamp: time.Now().UnixNano(),
	}

	for i, r := range resp.Responses {
		var entityType string
		if i < len(requests) {
			entityType = requests[i].EntityType
		}
		result.Results[i] = &DeleteResultItem{
			EntityType: entityType,
			ID:         requests[i].ID,
			Deleted:    r.Deleted,
		}
	}

	return result, nil
}
