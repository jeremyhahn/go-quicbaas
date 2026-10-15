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

//go:build integration && rest

package transactions

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"net"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/api/rest"
	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// hashKeyToID generates a deterministic uint64 ID from a string key using simple FNV-1.
// This maps string keys to numeric IDs for the gRPC API which uses ID-based storage.
func hashKeyToID(key string) uint64 {
	var h uint64 = 14695981039346656037 // FNV offset basis
	for i := 0; i < len(key); i++ {
		h ^= uint64(key[i])
		h *= 1099511628211 // FNV prime
	}
	// Ensure non-zero ID
	if h == 0 {
		h = 1
	}
	return h
}

// hashKeyToIDFNV1a generates a deterministic uint64 ID using FNV-1a hash.
// This matches the hashing used by the REST KV API server.
func hashKeyToIDFNV1a(entityType, key string) uint64 {
	h := fnv.New64a()
	// hash.Hash.Write never returns an error.
	h.Write([]byte(entityType))
	h.Write([]byte(":"))
	h.Write([]byte(key))
	return h.Sum64()
}

// =============================================================================
// Result Structs
// =============================================================================

// CRUDResult contains the result of a CRUD operation.
type CRUDResult struct {
	Key     string
	Value   []byte
	Found   bool
	Success bool
	Message string
}

// TransactionResult contains the result of a transaction operation.
type TransactionResult struct {
	Succeeded bool
	Responses []TransactionOpResult
	Message   string
}

// TransactionOpResult contains the result of a single transaction operation.
type TransactionOpResult struct {
	Type       string
	EntityType string
	Key        string
	Data       []byte
	Success    bool
	Error      string
}

// =============================================================================
// Protocol Interface
// =============================================================================

// transactionProtocol abstracts the different protocols for testing.
type transactionProtocol interface {
	Setup(t *testing.T, ctx context.Context) error
	Teardown(t *testing.T) error
	Put(ctx context.Context, entityType, key string, value []byte) (*CRUDResult, error)
	Get(ctx context.Context, entityType, key string) (*CRUDResult, error)
	Delete(ctx context.Context, entityType, key string) (*CRUDResult, error)
	Transaction(ctx context.Context, req *rest.TransactionRequest) (*TransactionResult, error)
	// HashKeyToID computes the numeric ID for a given entity type and key.
	// Each protocol may use a different hashing scheme.
	HashKeyToID(entityType, key string) uint64
	Name() string
}

// =============================================================================
// Main Test Function
// =============================================================================

// TestTransactionMultiProtocol verifies that transaction operations work identically
// across all supported protocols: Embedded, REST, gRPC, Unix Socket, and MCP.
func TestTransactionMultiProtocol(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if helpers.IsEmbeddedAvailable() {
		t.Run("Embedded", func(t *testing.T) {
			testTransactionProtocol(t, ctx, &embeddedTransactionClient{})
		})
	}

	t.Run("REST", func(t *testing.T) {
		testTransactionProtocol(t, ctx, &restTransactionClient{})
	})

	t.Run("gRPC", func(t *testing.T) {
		testTransactionProtocol(t, ctx, &grpcTransactionClient{})
	})

	t.Run("UnixSocket", func(t *testing.T) {
		testTransactionProtocol(t, ctx, &unixSocketTransactionClient{})
	})

	// NOTE: no MCP leg here. quicbaas retired its bespoke JSON-RPC MCP
	// listener in favor of go-qrdb's unified multi-protocol server; the
	// unified server's MCP surface has no finalized docker endpoint
	// convention yet, so this suite does not exercise it (deferred).
}

// testTransactionProtocol runs the transaction test suite against a protocol.
func testTransactionProtocol(t *testing.T, ctx context.Context, protocol transactionProtocol) {
	t.Helper()

	err := protocol.Setup(t, ctx)
	require.NoError(t, err, "Protocol %s must be available", protocol.Name())
	defer func() {
		if teardownErr := protocol.Teardown(t); teardownErr != nil {
			t.Logf("Warning: teardown error for %s: %v", protocol.Name(), teardownErr)
		}
	}()

	t.Run("CRUD_PutGetDelete", func(t *testing.T) {
		testCRUDPutGetDelete(t, ctx, protocol)
	})

	t.Run("CRUD_Overwrite", func(t *testing.T) {
		testCRUDOverwrite(t, ctx, protocol)
	})

	t.Run("CRUD_NotFound", func(t *testing.T) {
		testCRUDNotFound(t, ctx, protocol)
	})

	t.Run("Transaction_CompareAndSwap", func(t *testing.T) {
		testTransactionCompareAndSwap(t, ctx, protocol)
	})

	t.Run("Transaction_ConditionalCreate", func(t *testing.T) {
		testTransactionConditionalCreate(t, ctx, protocol)
	})
}

// =============================================================================
// Test Functions
// =============================================================================

func testCRUDPutGetDelete(t *testing.T, ctx context.Context, protocol transactionProtocol) {
	key := fmt.Sprintf("test-crud-%s-%d", protocol.Name(), time.Now().UnixNano())
	value := []byte(`{"name":"test","protocol":"` + protocol.Name() + `"}`)

	// Put
	putResult, err := protocol.Put(ctx, "default", key, value)
	require.NoError(t, err, "%s: Put failed", protocol.Name())
	require.NotNil(t, putResult, "%s: Put returned nil result", protocol.Name())
	assert.True(t, putResult.Success, "%s: Put should succeed", protocol.Name())

	// Get
	getResult, err := protocol.Get(ctx, "default", key)
	require.NoError(t, err, "%s: Get failed", protocol.Name())
	require.NotNil(t, getResult, "%s: Get returned nil result", protocol.Name())
	assert.True(t, getResult.Found, "%s: Get should find key", protocol.Name())
	assert.Equal(t, value, getResult.Value, "%s: Get value should match", protocol.Name())

	// Delete
	deleteResult, err := protocol.Delete(ctx, "default", key)
	require.NoError(t, err, "%s: Delete failed", protocol.Name())
	require.NotNil(t, deleteResult, "%s: Delete returned nil result", protocol.Name())
	assert.True(t, deleteResult.Success, "%s: Delete should succeed", protocol.Name())

	// Verify deleted
	getResult2, err := protocol.Get(ctx, "default", key)
	// Some protocols may return error, others may return Found=false
	if err == nil && getResult2 != nil {
		assert.False(t, getResult2.Found, "%s: Key should not be found after delete", protocol.Name())
	}

	t.Logf("%s: CRUD Put/Get/Delete verified", protocol.Name())
}

func testCRUDOverwrite(t *testing.T, ctx context.Context, protocol transactionProtocol) {
	key := fmt.Sprintf("test-overwrite-%s-%d", protocol.Name(), time.Now().UnixNano())
	value1 := []byte(`{"version":1}`)
	value2 := []byte(`{"version":2}`)

	// Put original value
	_, err := protocol.Put(ctx, "default", key, value1)
	require.NoError(t, err, "%s: First Put failed", protocol.Name())

	// Verify original value
	getResult1, err := protocol.Get(ctx, "default", key)
	require.NoError(t, err, "%s: First Get failed", protocol.Name())
	assert.Equal(t, value1, getResult1.Value, "%s: First Get should return original value", protocol.Name())

	// Overwrite
	_, err = protocol.Put(ctx, "default", key, value2)
	require.NoError(t, err, "%s: Second Put failed", protocol.Name())

	// Verify new value
	getResult2, err := protocol.Get(ctx, "default", key)
	require.NoError(t, err, "%s: Second Get failed", protocol.Name())
	assert.Equal(t, value2, getResult2.Value, "%s: Second Get should return new value", protocol.Name())

	// Cleanup
	_, _ = protocol.Delete(ctx, "default", key)

	t.Logf("%s: CRUD Overwrite verified", protocol.Name())
}

func testCRUDNotFound(t *testing.T, ctx context.Context, protocol transactionProtocol) {
	key := fmt.Sprintf("test-notfound-%s-%d", protocol.Name(), time.Now().UnixNano())

	// Get non-existent key
	getResult, err := protocol.Get(ctx, "default", key)
	// Some protocols may return error, others may return Found=false
	if err == nil && getResult != nil {
		assert.False(t, getResult.Found, "%s: Get should not find non-existent key", protocol.Name())
	}

	t.Logf("%s: CRUD NotFound verified", protocol.Name())
}

func testTransactionCompareAndSwap(t *testing.T, ctx context.Context, protocol transactionProtocol) {
	key := fmt.Sprintf("test-txn-cas-%s-%d", protocol.Name(), time.Now().UnixNano())
	initialValue := []byte(`{"counter":1}`)
	updatedValue := []byte(`{"counter":2}`)

	// Create initial value
	_, err := protocol.Put(ctx, "default", key, initialValue)
	require.NoError(t, err, "%s: Initial Put failed", protocol.Name())

	// Compute the ID from the key for transaction operations
	keyID := protocol.HashKeyToID("default", key)

	// Transaction: compare exists, then update
	txnReq := &rest.TransactionRequest{
		Compare: []rest.CompareCondition{
			{
				EntityType: "default",
				ID:         keyID,
				Target:     rest.CompareTargetExists,
				Operator:   rest.CompareOpEqual,
			},
		},
		Success: []rest.TransactionOp{
			{
				Type:       rest.TxnOpPut,
				EntityType: "default",
				ID:         keyID,
				Data:       updatedValue,
			},
		},
	}

	txnResult, err := protocol.Transaction(ctx, txnReq)
	require.NoError(t, err, "%s: Transaction must be implemented and available", protocol.Name())

	require.NotNil(t, txnResult, "%s: Transaction returned nil result", protocol.Name())
	assert.True(t, txnResult.Succeeded, "%s: Transaction should succeed", protocol.Name())

	// Verify updated value
	getResult, err := protocol.Get(ctx, "default", key)
	require.NoError(t, err, "%s: Get after transaction failed", protocol.Name())
	assert.Equal(t, updatedValue, getResult.Value, "%s: Value should be updated by transaction", protocol.Name())

	// Cleanup
	_, _ = protocol.Delete(ctx, "default", key)

	t.Logf("%s: Transaction Compare-and-Swap verified", protocol.Name())
}

func testTransactionConditionalCreate(t *testing.T, ctx context.Context, protocol transactionProtocol) {
	key := fmt.Sprintf("test-txn-create-%s-%d", protocol.Name(), time.Now().UnixNano())
	newValue := []byte(`{"created":true}`)

	// Compute the ID from the key for transaction operations
	keyID := protocol.HashKeyToID("default", key)

	// Transaction: create only if not exists
	txnReq := &rest.TransactionRequest{
		Compare: []rest.CompareCondition{
			{
				EntityType: "default",
				ID:         keyID,
				Target:     rest.CompareTargetExists,
				Operator:   rest.CompareOpNotEqual,
			},
		},
		Success: []rest.TransactionOp{
			{
				Type:       rest.TxnOpPut,
				EntityType: "default",
				ID:         keyID,
				Data:       newValue,
			},
		},
	}

	txnResult, err := protocol.Transaction(ctx, txnReq)
	require.NoError(t, err, "%s: Transaction must be implemented and available", protocol.Name())

	require.NotNil(t, txnResult, "%s: Transaction returned nil result", protocol.Name())
	assert.True(t, txnResult.Succeeded, "%s: Transaction should succeed (key didn't exist)", protocol.Name())

	// Verify created value
	getResult, err := protocol.Get(ctx, "default", key)
	require.NoError(t, err, "%s: Get after transaction failed", protocol.Name())
	assert.Equal(t, newValue, getResult.Value, "%s: Value should be created by transaction", protocol.Name())

	// Try to create again - should fail comparison
	txnResult2, err := protocol.Transaction(ctx, txnReq)
	if err == nil && txnResult2 != nil {
		assert.False(t, txnResult2.Succeeded, "%s: Second transaction should fail (key exists)", protocol.Name())
	}

	// Cleanup
	_, _ = protocol.Delete(ctx, "default", key)

	t.Logf("%s: Transaction Conditional Create verified", protocol.Name())
}

// =============================================================================
// Embedded Protocol Implementation
// =============================================================================

// ErrEmbeddedClientNotInitialized is returned when the embedded client is not initialized.
type ErrEmbeddedClientNotInitialized struct{}

func (e ErrEmbeddedClientNotInitialized) Error() string {
	return "embedded client not initialized"
}

type embeddedTransactionClient struct {
	client  quicbaas.Client
	keyToID map[string]uint64 // Track key to ID mapping
}

func (c *embeddedTransactionClient) Name() string { return "Embedded" }

func (c *embeddedTransactionClient) HashKeyToID(entityType, key string) uint64 {
	return hashKeyToID(entityType + ":" + key)
}

func (c *embeddedTransactionClient) Setup(t *testing.T, ctx context.Context) error {
	svc, err := helpers.GetEmbeddedService(t)
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "failed to get embedded service", Cause: err}
	}

	client, err := quicbaas.NewClient("", quicbaas.WithEmbedded(svc))
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "failed to create embedded client", Cause: err}
	}

	c.client = client
	c.keyToID = make(map[string]uint64)
	return nil
}

func (c *embeddedTransactionClient) Teardown(t *testing.T) error {
	if c.client != nil {
		return c.client.Close()
	}
	return nil
}

func (c *embeddedTransactionClient) Put(ctx context.Context, entityType, key string, value []byte) (*CRUDResult, error) {
	if c.client == nil {
		return nil, ErrEmbeddedClientNotInitialized{}
	}

	// Generate a deterministic ID from the key using hash
	mapKey := entityType + ":" + key
	id := hashKeyToID(mapKey)

	resp, err := c.client.KV().Put(ctx, &quicbaas.PutRequest{
		EntityType: entityType,
		ID:         id, // Provide the ID since server echoes it back
		Data:       value,
	})
	if err != nil {
		return nil, err
	}

	// Store key -> ID mapping for subsequent Get/Delete
	c.keyToID[mapKey] = resp.ID

	return &CRUDResult{
		Key:     key,
		Success: resp.ID > 0 || resp.Version > 0,
	}, nil
}

func (c *embeddedTransactionClient) Get(ctx context.Context, entityType, key string) (*CRUDResult, error) {
	if c.client == nil {
		return nil, ErrEmbeddedClientNotInitialized{}
	}

	// Look up ID from the key mapping
	mapKey := entityType + ":" + key
	id, ok := c.keyToID[mapKey]
	if !ok {
		return &CRUDResult{Key: key, Found: false}, nil
	}

	resp, err := c.client.KV().Get(ctx, &quicbaas.GetRequest{
		EntityType: entityType,
		ID:         id,
	})
	if err != nil {
		return nil, err
	}

	return &CRUDResult{
		Key:   key,
		Value: resp.Data,
		Found: resp.Found,
	}, nil
}

func (c *embeddedTransactionClient) Delete(ctx context.Context, entityType, key string) (*CRUDResult, error) {
	if c.client == nil {
		return nil, ErrEmbeddedClientNotInitialized{}
	}

	// Look up ID from the key mapping
	mapKey := entityType + ":" + key
	id, ok := c.keyToID[mapKey]
	if !ok {
		return &CRUDResult{Key: key, Success: false}, nil
	}

	resp, err := c.client.KV().Delete(ctx, &quicbaas.DeleteRequest{
		EntityType: entityType,
		ID:         id,
	})
	if err != nil {
		return nil, err
	}

	// Remove from mapping after delete
	delete(c.keyToID, mapKey)

	return &CRUDResult{
		Key:     key,
		Success: resp.Deleted,
	}, nil
}

func (c *embeddedTransactionClient) Transaction(ctx context.Context, req *rest.TransactionRequest) (*TransactionResult, error) {
	if c.client == nil {
		return nil, ErrEmbeddedClientNotInitialized{}
	}

	// Embedded transaction operations use the SDK's Transaction client
	// For now, return not implemented as the SDK's Transaction interface may differ
	return nil, &qberrors.MessageError{Message: "embedded transaction operations not yet implemented in test client"}
}

// =============================================================================
// REST Protocol Implementation
// =============================================================================

type restTransactionClient struct {
	baseURL    string
	httpClient *http.Client
}

func (c *restTransactionClient) Name() string { return "REST" }

func (c *restTransactionClient) HashKeyToID(entityType, key string) uint64 {
	// REST KV API uses FNV-1a to hash string keys to numeric IDs
	return hashKeyToIDFNV1a(entityType, key)
}

func (c *restTransactionClient) Setup(t *testing.T, ctx context.Context) error {
	endpoints := helpers.GetRESTEndpoints()
	if len(endpoints) == 0 {
		return &qberrors.MessageError{Message: "no REST endpoints available"}
	}
	c.baseURL = endpoints[0]
	c.httpClient = helpers.NewHTTPClient(30 * time.Second)
	return nil
}

func (c *restTransactionClient) Teardown(t *testing.T) error {
	return nil
}

func (c *restTransactionClient) Put(ctx context.Context, entityType, key string, value []byte) (*CRUDResult, error) {
	url := fmt.Sprintf("%s/api/v1/kv/%s/%s", c.baseURL, entityType, key)
	req, err := http.NewRequestWithContext(ctx, "PUT", url, bytes.NewReader(value))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	return &CRUDResult{
		Key:     key,
		Success: resp.StatusCode >= 200 && resp.StatusCode < 300,
	}, nil
}

func (c *restTransactionClient) Get(ctx context.Context, entityType, key string) (*CRUDResult, error) {
	url := fmt.Sprintf("%s/api/v1/kv/%s/%s", c.baseURL, entityType, key)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return &CRUDResult{Key: key, Found: false}, nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// Parse JSON response to extract the value field
	var getResp struct {
		EntityType string          `json:"entity_type"`
		Key        string          `json:"key"`
		Value      json.RawMessage `json:"value"`
		Found      bool            `json:"found"`
		Timestamp  string          `json:"timestamp"`
	}
	if err := json.Unmarshal(body, &getResp); err != nil {
		// If parsing fails, return raw body (for backwards compatibility)
		return &CRUDResult{
			Key:   key,
			Value: body,
			Found: resp.StatusCode == http.StatusOK,
		}, nil
	}

	return &CRUDResult{
		Key:   key,
		Value: getResp.Value,
		Found: getResp.Found,
	}, nil
}

func (c *restTransactionClient) Delete(ctx context.Context, entityType, key string) (*CRUDResult, error) {
	url := fmt.Sprintf("%s/api/v1/kv/%s/%s", c.baseURL, entityType, key)
	req, err := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	return &CRUDResult{
		Key:     key,
		Success: resp.StatusCode >= 200 && resp.StatusCode < 300,
	}, nil
}

func (c *restTransactionClient) Transaction(ctx context.Context, req *rest.TransactionRequest) (*TransactionResult, error) {
	url := fmt.Sprintf("%s/api/v1/transaction", c.baseURL)
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var txnResp rest.TransactionResponse
	if err := json.Unmarshal(respBody, &txnResp); err != nil {
		return nil, err
	}

	result := &TransactionResult{
		Succeeded: txnResp.Succeeded,
		Message:   txnResp.Message,
	}

	for _, opResp := range txnResp.Responses {
		result.Responses = append(result.Responses, TransactionOpResult{
			Type:       string(opResp.Type),
			EntityType: opResp.EntityType,
			Data:       opResp.Data,
			Success:    opResp.Success,
			Error:      opResp.Error,
		})
	}

	return result, nil
}

// =============================================================================
// gRPC Protocol Implementation
// =============================================================================

type grpcTransactionClient struct {
	conn    *grpc.ClientConn
	client  pb.QuicBaaSClient
	keyToID map[string]uint64 // Track key to ID mapping
}

func (c *grpcTransactionClient) Name() string { return "gRPC" }

func (c *grpcTransactionClient) HashKeyToID(entityType, key string) uint64 {
	return hashKeyToID(entityType + ":" + key)
}

func (c *grpcTransactionClient) Setup(t *testing.T, ctx context.Context) error {
	endpoints := helpers.GetClusterEndpoints()
	if len(endpoints) == 0 {
		return &qberrors.MessageError{Message: "no gRPC endpoints available"}
	}

	endpoint := endpoints[0]

	dialCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	tlsCfg, err := helpers.IntegrationTLSConfig()
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "load integration TLS config", Cause: err}
	}
	conn, err := grpc.DialContext(dialCtx, endpoint,
		grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)),
		grpc.WithBlock(),
	)
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "failed to connect to gRPC endpoint " + endpoint, Cause: err}
	}

	c.conn = conn
	c.client = pb.NewQuicBaaSClient(conn)
	c.keyToID = make(map[string]uint64)
	return nil
}

func (c *grpcTransactionClient) Teardown(t *testing.T) error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

func (c *grpcTransactionClient) Put(ctx context.Context, entityType, key string, value []byte) (*CRUDResult, error) {
	// Generate a deterministic ID from the key using hash
	// This maps string keys to numeric IDs for the gRPC API
	mapKey := entityType + ":" + key
	id := hashKeyToID(mapKey)

	resp, err := c.client.Put(ctx, &pb.PutRequest{
		EntityType:  entityType,
		Id:          id, // Provide the ID since server echoes it back
		Data:        value,
		Consistency: pb.ConsistencyLevel_CONSISTENCY_LEVEL_LINEARIZABLE,
	})
	if err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "gRPC Put failed", Cause: err}
	}

	// Store key -> ID mapping for subsequent Get/Delete
	c.keyToID[mapKey] = resp.Id

	return &CRUDResult{
		Key:     key,
		Success: resp.Id > 0 || resp.Version > 0,
	}, nil
}

func (c *grpcTransactionClient) Get(ctx context.Context, entityType, key string) (*CRUDResult, error) {
	// Look up ID from the key mapping, or compute from key if not found
	mapKey := entityType + ":" + key
	id, ok := c.keyToID[mapKey]
	if !ok || id == 0 {
		// ID not in map (e.g., created via transaction), compute from key
		id = hashKeyToID(mapKey)
	}

	resp, err := c.client.Get(ctx, &pb.GetRequest{
		EntityType:  entityType,
		Id:          id,
		Consistency: pb.ConsistencyLevel_CONSISTENCY_LEVEL_LINEARIZABLE,
	})
	if err != nil {
		return nil, err
	}

	return &CRUDResult{
		Key:   key,
		Value: resp.Data,
		Found: resp.Found,
	}, nil
}

func (c *grpcTransactionClient) Delete(ctx context.Context, entityType, key string) (*CRUDResult, error) {
	// Look up ID from the key mapping
	mapKey := entityType + ":" + key
	id, ok := c.keyToID[mapKey]
	if !ok {
		return &CRUDResult{Key: key, Success: false}, nil
	}

	resp, err := c.client.Delete(ctx, &pb.DeleteRequest{
		EntityType:  entityType,
		Id:          id,
		Consistency: pb.ConsistencyLevel_CONSISTENCY_LEVEL_LINEARIZABLE,
	})
	if err != nil {
		return nil, err
	}

	// Remove from mapping after delete
	delete(c.keyToID, mapKey)

	return &CRUDResult{
		Key:     key,
		Success: resp.Deleted,
	}, nil
}

func (c *grpcTransactionClient) Transaction(ctx context.Context, req *rest.TransactionRequest) (*TransactionResult, error) {
	// Convert REST transaction request to gRPC
	pbReq := &pb.TransactionRequest{}

	for _, cmp := range req.Compare {
		pbCmp := &pb.CompareCondition{
			EntityType: cmp.EntityType,
			Id:         cmp.ID,
		}

		switch cmp.Target {
		case rest.CompareTargetExists:
			pbCmp.Target = pb.CompareTarget_COMPARE_TARGET_EXISTS
		case rest.CompareTargetVersion:
			pbCmp.Target = pb.CompareTarget_COMPARE_TARGET_VERSION
			pbCmp.Version = uint64(cmp.Version)
		case rest.CompareTargetValue:
			pbCmp.Target = pb.CompareTarget_COMPARE_TARGET_VALUE
			pbCmp.Value = cmp.Value
		}

		switch cmp.Operator {
		case rest.CompareOpEqual:
			pbCmp.Operator = pb.CompareOperator_COMPARE_OPERATOR_EQUAL
		case rest.CompareOpNotEqual:
			pbCmp.Operator = pb.CompareOperator_COMPARE_OPERATOR_NOT_EQUAL
		case rest.CompareOpGreater:
			pbCmp.Operator = pb.CompareOperator_COMPARE_OPERATOR_GREATER
		case rest.CompareOpLess:
			pbCmp.Operator = pb.CompareOperator_COMPARE_OPERATOR_LESS
		}

		pbReq.Compare = append(pbReq.Compare, pbCmp)
	}

	for _, op := range req.Success {
		pbOp := &pb.TransactionOp{
			EntityType: op.EntityType,
			Id:         op.ID,
			Data:       op.Data,
		}

		switch op.Type {
		case rest.TxnOpGet:
			pbOp.OpType = pb.TransactionOpType_TRANSACTION_OP_TYPE_GET
		case rest.TxnOpPut:
			pbOp.OpType = pb.TransactionOpType_TRANSACTION_OP_TYPE_PUT
		case rest.TxnOpDelete:
			pbOp.OpType = pb.TransactionOpType_TRANSACTION_OP_TYPE_DELETE
		}

		pbReq.Success = append(pbReq.Success, pbOp)
	}

	for _, op := range req.Failure {
		pbOp := &pb.TransactionOp{
			EntityType: op.EntityType,
			Id:         op.ID,
			Data:       op.Data,
		}

		switch op.Type {
		case rest.TxnOpGet:
			pbOp.OpType = pb.TransactionOpType_TRANSACTION_OP_TYPE_GET
		case rest.TxnOpPut:
			pbOp.OpType = pb.TransactionOpType_TRANSACTION_OP_TYPE_PUT
		case rest.TxnOpDelete:
			pbOp.OpType = pb.TransactionOpType_TRANSACTION_OP_TYPE_DELETE
		}

		pbReq.Failure = append(pbReq.Failure, pbOp)
	}

	resp, err := c.client.Transaction(ctx, pbReq)
	if err != nil {
		return nil, err
	}

	result := &TransactionResult{
		Succeeded: resp.Succeeded,
	}

	for _, opResp := range resp.Responses {
		opType := "unknown"
		switch opResp.OpType {
		case pb.TransactionOpType_TRANSACTION_OP_TYPE_GET:
			opType = "get"
		case pb.TransactionOpType_TRANSACTION_OP_TYPE_PUT:
			opType = "put"
		case pb.TransactionOpType_TRANSACTION_OP_TYPE_DELETE:
			opType = "delete"
		}

		result.Responses = append(result.Responses, TransactionOpResult{
			Type:       opType,
			EntityType: opResp.EntityType,
			Key:        strconv.FormatUint(opResp.Id, 10), // Convert ID to string for Key field
			Data:       opResp.Data,
			Success:    opResp.Success,
			Error:      opResp.Error,
		})
	}

	return result, nil
}

// =============================================================================
// Unix Socket Protocol Implementation
// =============================================================================

type unixSocketTransactionClient struct {
	conn    *grpc.ClientConn
	client  pb.QuicBaaSClient
	keyToID map[string]uint64 // Track key to ID mapping
}

func (c *unixSocketTransactionClient) Name() string { return "UnixSocket" }

func (c *unixSocketTransactionClient) HashKeyToID(entityType, key string) uint64 {
	return hashKeyToID(entityType + ":" + key)
}

func (c *unixSocketTransactionClient) Setup(t *testing.T, ctx context.Context) error {
	socketPath := helpers.GetUnixSocketPath()
	if socketPath == "" {
		return &qberrors.MessageError{Message: "no Unix socket path available"}
	}

	dialCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	tlsCfg, err := helpers.IntegrationTLSConfig()
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "load integration TLS config", Cause: err}
	}
	conn, err := grpc.DialContext(dialCtx,
		"unix://"+socketPath,
		grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)),
		grpc.WithBlock(),
		grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
			return net.DialTimeout("unix", socketPath, 10*time.Second)
		}),
	)
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "failed to connect to Unix socket " + socketPath, Cause: err}
	}

	c.conn = conn
	c.client = pb.NewQuicBaaSClient(conn)
	c.keyToID = make(map[string]uint64)
	return nil
}

func (c *unixSocketTransactionClient) Teardown(t *testing.T) error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

func (c *unixSocketTransactionClient) Put(ctx context.Context, entityType, key string, value []byte) (*CRUDResult, error) {
	// Generate a deterministic ID from the key using hash
	mapKey := entityType + ":" + key
	id := hashKeyToID(mapKey)

	resp, err := c.client.Put(ctx, &pb.PutRequest{
		EntityType:  entityType,
		Id:          id, // Provide the ID since server echoes it back
		Data:        value,
		Consistency: pb.ConsistencyLevel_CONSISTENCY_LEVEL_LINEARIZABLE,
	})
	if err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "UnixSocket Put failed", Cause: err}
	}

	// Store key -> ID mapping for subsequent Get/Delete
	c.keyToID[mapKey] = resp.Id

	return &CRUDResult{
		Key:     key,
		Success: resp.Id > 0 || resp.Version > 0,
	}, nil
}

func (c *unixSocketTransactionClient) Get(ctx context.Context, entityType, key string) (*CRUDResult, error) {
	// Look up ID from the key mapping, or compute from key if not found
	mapKey := entityType + ":" + key
	id, ok := c.keyToID[mapKey]
	if !ok || id == 0 {
		// ID not in map (e.g., created via transaction), compute from key
		id = hashKeyToID(mapKey)
	}

	resp, err := c.client.Get(ctx, &pb.GetRequest{
		EntityType:  entityType,
		Id:          id,
		Consistency: pb.ConsistencyLevel_CONSISTENCY_LEVEL_LINEARIZABLE,
	})
	if err != nil {
		return nil, err
	}

	return &CRUDResult{
		Key:   key,
		Value: resp.Data,
		Found: resp.Found,
	}, nil
}

func (c *unixSocketTransactionClient) Delete(ctx context.Context, entityType, key string) (*CRUDResult, error) {
	// Look up ID from the key mapping
	mapKey := entityType + ":" + key
	id, ok := c.keyToID[mapKey]
	if !ok {
		return &CRUDResult{Key: key, Success: false}, nil
	}

	resp, err := c.client.Delete(ctx, &pb.DeleteRequest{
		EntityType:  entityType,
		Id:          id,
		Consistency: pb.ConsistencyLevel_CONSISTENCY_LEVEL_LINEARIZABLE,
	})
	if err != nil {
		return nil, err
	}

	// Remove from mapping after delete
	delete(c.keyToID, mapKey)

	return &CRUDResult{
		Key:     key,
		Success: resp.Deleted,
	}, nil
}

func (c *unixSocketTransactionClient) Transaction(ctx context.Context, req *rest.TransactionRequest) (*TransactionResult, error) {
	// Reuse gRPC transaction implementation
	grpcClient := &grpcTransactionClient{conn: c.conn, client: c.client, keyToID: c.keyToID}
	return grpcClient.Transaction(ctx, req)
}
