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

//go:build integration && quic

package vector

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"strconv"
	"sync"
	"testing"
	"time"

	quicgo "github.com/quic-go/quic-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	grpcclient "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	"github.com/jeremyhahn/go-quicbaas/pkg/api/quic"
	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// =============================================================================
// Vector Protocol Interface
// =============================================================================

// VectorResult represents the result of a vector operation.
type VectorResult struct {
	ID        string
	Namespace string
	Vector    []float32
	Metadata  map[string]string
	Score     float32
	Found     bool
}

// vectorProtocol abstracts the different protocols for testing vector operations.
type vectorProtocol interface {
	Setup(t *testing.T, ctx context.Context) error
	Teardown(t *testing.T) error
	CreateNamespace(ctx context.Context, name string, dimensions int, metric string) error
	DeleteNamespace(ctx context.Context, name string) error
	UpsertVector(ctx context.Context, namespace string, id string, vector []float32, metadata map[string]string) error
	GetVector(ctx context.Context, namespace string, id string) (*VectorResult, error)
	DeleteVector(ctx context.Context, namespace string, id string) error
	SearchVectors(ctx context.Context, namespace string, vector []float32, k int) ([]*VectorResult, error)
	Name() string
}

// =============================================================================
// Main Multi-Protocol Tests
// =============================================================================

// TestVectorMultiProtocol verifies that vector operations work identically
// across all supported protocols: REST, gRPC, QUIC, Unix Socket, MCP, and Embedded.
func TestVectorMultiProtocol(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// Test each protocol independently
	t.Run("REST", func(t *testing.T) {
		testVectorProtocol(t, ctx, &restVectorClient{})
	})

	t.Run("gRPC", func(t *testing.T) {
		testVectorProtocol(t, ctx, &grpcVectorClient{})
	})

	t.Run("QUIC", func(t *testing.T) {
		testVectorProtocol(t, ctx, &quicVectorClient{})
	})

	t.Run("UnixSocket", func(t *testing.T) {
		testVectorProtocol(t, ctx, &unixSocketVectorClient{})
	})

	t.Run("MCP", func(t *testing.T) {
		testVectorProtocol(t, ctx, &mcpVectorClient{})
	})

	if helpers.IsEmbeddedAvailable() {
		t.Run("Embedded", func(t *testing.T) {
			testVectorProtocol(t, ctx, &embeddedVectorClient{})
		})
	}
}

// testVectorProtocol runs the complete vector test suite against a protocol.
func testVectorProtocol(t *testing.T, ctx context.Context, protocol vectorProtocol) {
	t.Helper()

	// Setup the protocol client
	err := protocol.Setup(t, ctx)
	require.NoError(t, err, "%s protocol setup must succeed: service must be available", protocol.Name())
	defer func() {
		if teardownErr := protocol.Teardown(t); teardownErr != nil {
			t.Logf("Warning: teardown error for %s: %v", protocol.Name(), teardownErr)
		}
	}()

	baseName := fmt.Sprintf("mp-%s-%d", protocol.Name(), time.Now().UnixNano()%1000000)

	// Test 1: CreateNamespace
	t.Run("CreateNamespace", func(t *testing.T) {
		testMultiprotocol_CreateNamespace(t, ctx, protocol, baseName+"-ns1")
	})

	// Test 2: UpsertAndGetVector
	t.Run("UpsertAndGetVector", func(t *testing.T) {
		testMultiprotocol_UpsertAndGetVector(t, ctx, protocol, baseName+"-ns2")
	})

	// Test 3: DeleteVector
	t.Run("DeleteVector", func(t *testing.T) {
		testMultiprotocol_DeleteVector(t, ctx, protocol, baseName+"-ns3")
	})

	// Test 4: SearchVectors
	t.Run("SearchVectors", func(t *testing.T) {
		testMultiprotocol_SearchVectors(t, ctx, protocol, baseName+"-ns4")
	})

	// Test 5: CRUD_Cycle
	t.Run("CRUD_Cycle", func(t *testing.T) {
		testMultiprotocol_CRUD_Cycle(t, ctx, protocol, baseName+"-ns5")
	})
}

// =============================================================================
// Individual Test Functions
// =============================================================================

func testMultiprotocol_CreateNamespace(t *testing.T, ctx context.Context, protocol vectorProtocol, namespace string) {
	t.Helper()

	// Create a new namespace
	err := protocol.CreateNamespace(ctx, namespace, 128, "L2")
	require.NoError(t, err, "[%s] CreateNamespace failed: vector service must be available", protocol.Name())

	t.Logf("[%s] CreateNamespace: created namespace %s with 128 dimensions", protocol.Name(), namespace)

	// Clean up
	err = protocol.DeleteNamespace(ctx, namespace)
	if err != nil {
		t.Logf("[%s] Warning: failed to delete namespace during cleanup: %v", protocol.Name(), err)
	}
}

func testMultiprotocol_UpsertAndGetVector(t *testing.T, ctx context.Context, protocol vectorProtocol, namespace string) {
	t.Helper()

	// Create namespace first
	err := protocol.CreateNamespace(ctx, namespace, 128, "L2")
	require.NoError(t, err, "[%s] CreateNamespace failed: vector service must be available", protocol.Name())
	defer func() {
		_ = protocol.DeleteNamespace(ctx, namespace)
	}()

	// Generate test vector
	testID := "test-vec-1"
	testVector := generateRandomVector(128)
	testMetadata := map[string]string{
		"label":    "test",
		"category": "integration",
	}

	// Upsert vector
	err = protocol.UpsertVector(ctx, namespace, testID, testVector, testMetadata)
	require.NoError(t, err, "[%s] UpsertVector must succeed: vector service must be available", protocol.Name())

	t.Logf("[%s] UpsertVector: upserted vector %s", protocol.Name(), testID)

	// Get vector
	result, err := protocol.GetVector(ctx, namespace, testID)
	require.NoError(t, err, "[%s] GetVector failed", protocol.Name())
	require.NotNil(t, result)
	assert.True(t, result.Found, "[%s] Vector should be found", protocol.Name())
	assert.Equal(t, testID, result.ID)

	// Verify vector values (if returned)
	if len(result.Vector) > 0 {
		assert.Equal(t, len(testVector), len(result.Vector), "[%s] Vector dimension mismatch", protocol.Name())
	}

	// Verify metadata (if returned)
	if result.Metadata != nil {
		assert.Equal(t, testMetadata["label"], result.Metadata["label"])
	}

	t.Logf("[%s] GetVector: retrieved vector %s successfully", protocol.Name(), testID)
}

func testMultiprotocol_DeleteVector(t *testing.T, ctx context.Context, protocol vectorProtocol, namespace string) {
	t.Helper()

	// Create namespace first
	err := protocol.CreateNamespace(ctx, namespace, 128, "L2")
	require.NoError(t, err, "[%s] CreateNamespace failed: vector service must be available", protocol.Name())
	defer func() {
		_ = protocol.DeleteNamespace(ctx, namespace)
	}()

	// Upsert a vector
	testID := "delete-test-vec"
	testVector := generateRandomVector(128)

	err = protocol.UpsertVector(ctx, namespace, testID, testVector, nil)
	require.NoError(t, err, "[%s] UpsertVector must succeed: vector service must be available", protocol.Name())

	// Verify it exists
	result, err := protocol.GetVector(ctx, namespace, testID)
	require.NoError(t, err, "[%s] GetVector failed before delete", protocol.Name())
	assert.True(t, result.Found, "[%s] Vector should exist before delete", protocol.Name())

	// Delete the vector
	err = protocol.DeleteVector(ctx, namespace, testID)
	require.NoError(t, err, "[%s] DeleteVector failed", protocol.Name())

	t.Logf("[%s] DeleteVector: deleted vector %s", protocol.Name(), testID)

	// Verify it no longer exists
	result, err = protocol.GetVector(ctx, namespace, testID)
	if err == nil {
		assert.False(t, result.Found, "[%s] Vector should not be found after delete", protocol.Name())
	}
	// Note: error is also acceptable for deleted vector

	t.Logf("[%s] DeleteVector: verified vector %s no longer accessible", protocol.Name(), testID)
}

func testMultiprotocol_SearchVectors(t *testing.T, ctx context.Context, protocol vectorProtocol, namespace string) {
	t.Helper()

	// Create namespace
	err := protocol.CreateNamespace(ctx, namespace, 128, "L2")
	require.NoError(t, err, "[%s] CreateNamespace failed: vector service must be available", protocol.Name())
	defer func() {
		_ = protocol.DeleteNamespace(ctx, namespace)
	}()

	// Insert multiple vectors
	numVectors := 10
	vectors := make([][]float32, numVectors)
	for i := 0; i < numVectors; i++ {
		vectors[i] = generateRandomVector(128)
		id := "search-vec-" + strconv.Itoa(i)
		metadata := map[string]string{
			"index": strconv.Itoa(i),
		}

		err := protocol.UpsertVector(ctx, namespace, id, vectors[i], metadata)
		require.NoError(t, err, "[%s] UpsertVector[%d] must succeed: vector service must be available", protocol.Name(), i)
	}

	t.Logf("[%s] SearchVectors: inserted %d vectors", protocol.Name(), numVectors)

	// Wait for indexing
	time.Sleep(500 * time.Millisecond)

	// Search using first vector as query
	queryVector := vectors[0]
	k := 5

	results, err := protocol.SearchVectors(ctx, namespace, queryVector, k)
	require.NoError(t, err, "[%s] SearchVectors must succeed: vector service must be available", protocol.Name())
	require.NotEmpty(t, results, "[%s] SearchVectors should return results", protocol.Name())

	// Verify we got up to k results
	assert.LessOrEqual(t, len(results), k, "[%s] Should return at most k results", protocol.Name())

	// The query vector itself should be among the top results (exact match has lowest distance)
	topResultFound := false
	for _, r := range results {
		if r.ID == "search-vec-0" {
			topResultFound = true
			break
		}
	}
	assert.True(t, topResultFound, "[%s] Query vector should be in search results", protocol.Name())

	t.Logf("[%s] SearchVectors: found %d results, query vector in results: %v",
		protocol.Name(), len(results), topResultFound)
}

func testMultiprotocol_CRUD_Cycle(t *testing.T, ctx context.Context, protocol vectorProtocol, namespace string) {
	t.Helper()

	// Create namespace
	err := protocol.CreateNamespace(ctx, namespace, 64, "COSINE")
	require.NoError(t, err, "[%s] CreateNamespace failed: vector service must be available", protocol.Name())

	t.Logf("[%s] CRUD: namespace created", protocol.Name())

	// Create vectors
	vectorIDs := []string{"crud-1", "crud-2", "crud-3"}
	for _, id := range vectorIDs {
		vec := generateRandomVector(64)
		metadata := map[string]string{"type": "crud-test"}
		err := protocol.UpsertVector(ctx, namespace, id, vec, metadata)
		require.NoError(t, err, "[%s] UpsertVector %s must succeed: vector service must be available", protocol.Name(), id)
	}

	t.Logf("[%s] CRUD: %d vectors created", protocol.Name(), len(vectorIDs))

	// Read vectors
	for _, id := range vectorIDs {
		result, err := protocol.GetVector(ctx, namespace, id)
		require.NoError(t, err, "[%s] Read vector %s failed", protocol.Name(), id)
		assert.True(t, result.Found, "[%s] Vector %s should exist", protocol.Name(), id)
	}

	t.Logf("[%s] CRUD: %d vectors read successfully", protocol.Name(), len(vectorIDs))

	// Update vector (upsert with same ID)
	updateID := vectorIDs[0]
	newVec := generateRandomVector(64)
	newMetadata := map[string]string{"type": "crud-test", "updated": "true"}
	err = protocol.UpsertVector(ctx, namespace, updateID, newVec, newMetadata)
	require.NoError(t, err, "[%s] Update vector failed", protocol.Name())

	// Verify update
	result, err := protocol.GetVector(ctx, namespace, updateID)
	require.NoError(t, err, "[%s] Read updated vector failed", protocol.Name())
	assert.True(t, result.Found)
	if result.Metadata != nil {
		assert.Equal(t, "true", result.Metadata["updated"])
	}

	t.Logf("[%s] CRUD: vector updated successfully", protocol.Name())

	// Delete vectors
	for _, id := range vectorIDs {
		err := protocol.DeleteVector(ctx, namespace, id)
		require.NoError(t, err, "[%s] Delete vector %s failed", protocol.Name(), id)
	}

	t.Logf("[%s] CRUD: %d vectors deleted", protocol.Name(), len(vectorIDs))

	// Delete namespace
	err = protocol.DeleteNamespace(ctx, namespace)
	require.NoError(t, err, "[%s] DeleteNamespace failed", protocol.Name())

	t.Logf("[%s] CRUD: namespace deleted - full cycle complete", protocol.Name())
}

// =============================================================================
// REST Protocol Client
// =============================================================================

type restVectorClient struct {
	baseURL string
	client  *http.Client
}

func (c *restVectorClient) Name() string { return "REST" }

func (c *restVectorClient) Setup(t *testing.T, ctx context.Context) error {
	endpoints := helpers.GetRESTEndpoints()
	require.NotEmpty(t, endpoints, "REST endpoints must be available for integration tests")

	c.baseURL = endpoints[0]
	c.client = &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig:     &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
			TLSHandshakeTimeout: 10 * time.Second,
			DialContext: (&net.Dialer{
				Timeout:   10 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
		},
	}

	// Check if vector service is available by calling the health endpoint
	healthURL := c.baseURL + "/api/v1/vector/health"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "failed to create health check request", Cause: err}
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "vector service health check failed", Cause: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusServiceUnavailable {
		respBody, _ := io.ReadAll(resp.Body)
		return &qberrors.MessageError{Message: "vector service health check returned 503: " + string(respBody)}
	}

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return &qberrors.MessageError{Message: "vector service health check returned unexpected status: " + strconv.Itoa(resp.StatusCode) + ": " + string(respBody)}
	}

	return nil
}

func (c *restVectorClient) Teardown(t *testing.T) error {
	return nil
}

func (c *restVectorClient) CreateNamespace(ctx context.Context, name string, dimensions int, metric string) error {
	payload := map[string]interface{}{
		"namespace":       name,
		"dimensions":      dimensions,
		"distance_metric": metric,
		"index_type":      "HNSW",
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "failed to marshal request", Cause: err}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/api/v1/vector/namespaces", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return &qberrors.MessageError{Message: "status " + strconv.Itoa(resp.StatusCode) + ": " + string(respBody)}
	}

	return nil
}

func (c *restVectorClient) DeleteNamespace(ctx context.Context, name string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete,
		c.baseURL+"/api/v1/vector/namespaces/"+name, nil)
	if err != nil {
		return err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		respBody, _ := io.ReadAll(resp.Body)
		return &qberrors.MessageError{Message: "status " + strconv.Itoa(resp.StatusCode) + ": " + string(respBody)}
	}

	return nil
}

func (c *restVectorClient) UpsertVector(ctx context.Context, namespace string, id string, vector []float32, metadata map[string]string) error {
	payload := map[string]interface{}{
		"namespace": namespace,
		"id":        id,
		"vector":    vector,
		"metadata":  metadata,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "failed to marshal request", Cause: err}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/api/v1/vector/namespaces/"+namespace+"/vectors", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return &qberrors.MessageError{Message: "status " + strconv.Itoa(resp.StatusCode) + ": " + string(respBody)}
	}

	return nil
}

func (c *restVectorClient) GetVector(ctx context.Context, namespace string, id string) (*VectorResult, error) {
	url := c.baseURL + "/api/v1/vector/namespaces/" + namespace + "/vectors/" + id
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return &VectorResult{ID: id, Found: false}, nil
	}

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, &qberrors.MessageError{Message: "status " + strconv.Itoa(resp.StatusCode) + ": " + string(respBody)}
	}

	var data map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}

	result := &VectorResult{
		ID:        id,
		Namespace: namespace,
		Found:     true,
	}

	if vecData, ok := data["vector"].([]interface{}); ok {
		result.Vector = make([]float32, len(vecData))
		for i, v := range vecData {
			if f, ok := v.(float64); ok {
				result.Vector[i] = float32(f)
			}
		}
	}

	if metaData, ok := data["metadata"].(map[string]interface{}); ok {
		result.Metadata = make(map[string]string)
		for k, v := range metaData {
			if s, ok := v.(string); ok {
				result.Metadata[k] = s
			}
		}
	}

	return result, nil
}

func (c *restVectorClient) DeleteVector(ctx context.Context, namespace string, id string) error {
	url := c.baseURL + "/api/v1/vector/namespaces/" + namespace + "/vectors/" + id
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		respBody, _ := io.ReadAll(resp.Body)
		return &qberrors.MessageError{Message: "status " + strconv.Itoa(resp.StatusCode) + ": " + string(respBody)}
	}

	return nil
}

func (c *restVectorClient) SearchVectors(ctx context.Context, namespace string, vector []float32, k int) ([]*VectorResult, error) {
	payload := map[string]interface{}{
		"namespace":        namespace,
		"query_vector":     vector,
		"k":                k,
		"include_metadata": true,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "failed to marshal request", Cause: err}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/api/v1/vector/namespaces/"+namespace+"/search/similar", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, &qberrors.MessageError{Message: "status " + strconv.Itoa(resp.StatusCode) + ": " + string(respBody)}
	}

	var data map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}

	var results []*VectorResult
	if resultsData, ok := data["results"].([]interface{}); ok {
		for _, r := range resultsData {
			if rMap, ok := r.(map[string]interface{}); ok {
				result := &VectorResult{Found: true}
				if id, ok := rMap["id"].(string); ok {
					result.ID = id
				}
				if score, ok := rMap["score"].(float64); ok {
					result.Score = float32(score)
				}
				results = append(results, result)
			}
		}
	}

	return results, nil
}

// =============================================================================
// gRPC Protocol Client
// =============================================================================

type grpcVectorClient struct {
	grpcClient *grpcclient.Client
	conn       *grpc.ClientConn
}

func (c *grpcVectorClient) Name() string { return "gRPC" }

func (c *grpcVectorClient) Setup(t *testing.T, ctx context.Context) error {
	endpoints := helpers.GetClusterEndpoints()
	require.NotEmpty(t, endpoints, "gRPC endpoints must be available for integration tests")

	// Vector operations use a dedicated Raft shard whose leader may be on any
	// node. Try each endpoint until we find one that can serve vector writes.
	var lastErr error
	for _, endpoint := range endpoints {
		conn, err := helpers.SetupGRPCConnectionToEndpoint(t, ctx, endpoint)
		if err != nil {
			lastErr = err
			t.Logf("gRPC vector client: endpoint %s connection failed: %v", endpoint, err)
			continue
		}

		client, err := grpcclient.NewClient(endpoint, grpcclient.WithTLSConfig(&tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS13}))
		if err != nil {
			conn.Close()
			lastErr = err
			t.Logf("gRPC vector client: endpoint %s client creation failed: %v", endpoint, err)
			continue
		}

		// Probe: try creating and deleting a test namespace to verify this
		// node is the leader for the vector shard.
		probeNS := fmt.Sprintf("__probe_%d", time.Now().UnixNano())
		probeCtx, probeCancel := context.WithTimeout(ctx, 5*time.Second)
		_, probeErr := client.VectorCreateNamespace(probeCtx, &pb.CreateNamespaceRequest{
			Config: &pb.NamespaceConfig{
				Namespace:  probeNS,
				Dimensions: 2,
			},
		})
		probeCancel()
		if probeErr != nil {
			client.Close()
			conn.Close()
			lastErr = probeErr
			t.Logf("gRPC vector client: endpoint %s not vector leader: %v", endpoint, probeErr)
			continue
		}
		// Clean up probe namespace (best-effort)
		delCtx, delCancel := context.WithTimeout(ctx, 5*time.Second)
		_, _ = client.VectorDeleteNamespace(delCtx, &pb.DeleteNamespaceRequest{Namespace: probeNS})
		delCancel()

		c.conn = conn
		c.grpcClient = client
		t.Logf("gRPC vector client: using endpoint %s (vector shard leader)", endpoint)
		return nil
	}

	return &qberrors.WrappedMessageError{Message: "no endpoint is leader for vector shard", Cause: lastErr}
}

func (c *grpcVectorClient) Teardown(t *testing.T) error {
	if c.grpcClient != nil {
		c.grpcClient.Close()
	}
	if c.conn != nil {
		c.conn.Close()
	}
	return nil
}

func (c *grpcVectorClient) CreateNamespace(ctx context.Context, name string, dimensions int, metric string) error {
	var distMetric pb.DistanceMetric
	switch metric {
	case "COSINE", "cosine":
		distMetric = pb.DistanceMetric_DISTANCE_METRIC_COSINE
	case "DOT_PRODUCT", "dot_product":
		distMetric = pb.DistanceMetric_DISTANCE_METRIC_DOT_PRODUCT
	default:
		distMetric = pb.DistanceMetric_DISTANCE_METRIC_L2
	}

	req := &pb.CreateNamespaceRequest{
		Config: &pb.NamespaceConfig{
			Namespace:      name,
			Dimensions:     int32(dimensions),
			DistanceMetric: distMetric,
			IndexType:      pb.IndexType_INDEX_TYPE_HNSW,
		},
	}

	_, err := c.grpcClient.VectorCreateNamespace(ctx, req)
	return err
}

func (c *grpcVectorClient) DeleteNamespace(ctx context.Context, name string) error {
	req := &pb.DeleteNamespaceRequest{
		Namespace: name,
		Force:     true,
	}

	_, err := c.grpcClient.VectorDeleteNamespace(ctx, req)
	return err
}

func (c *grpcVectorClient) UpsertVector(ctx context.Context, namespace string, id string, vector []float32, metadata map[string]string) error {
	req := &pb.UpsertVectorRequest{
		Vector: &pb.VectorEntity{
			Id:        id,
			Namespace: namespace,
			Vector:    vector,
			Metadata:  metadata,
		},
		Consistency: pb.VectorConsistencyLevel_VECTOR_CONSISTENCY_LEVEL_QUORUM,
	}

	_, err := c.grpcClient.VectorUpsert(ctx, req)
	return err
}

func (c *grpcVectorClient) GetVector(ctx context.Context, namespace string, id string) (*VectorResult, error) {
	req := &pb.GetVectorRequest{
		Namespace:       namespace,
		Id:              id,
		Consistency:     pb.VectorConsistencyLevel_VECTOR_CONSISTENCY_LEVEL_QUORUM,
		IncludeVector:   true,
		IncludeMetadata: true,
	}

	resp, err := c.grpcClient.VectorGet(ctx, req)
	if err != nil {
		return nil, err
	}

	result := &VectorResult{
		ID:        id,
		Namespace: namespace,
		Found:     resp.Found,
	}

	if resp.Vector != nil {
		result.Vector = resp.Vector.Vector
		result.Metadata = resp.Vector.Metadata
	}

	return result, nil
}

func (c *grpcVectorClient) DeleteVector(ctx context.Context, namespace string, id string) error {
	req := &pb.DeleteVectorRequest{
		Namespace:   namespace,
		Id:          id,
		Consistency: pb.VectorConsistencyLevel_VECTOR_CONSISTENCY_LEVEL_QUORUM,
	}

	_, err := c.grpcClient.VectorDelete(ctx, req)
	return err
}

func (c *grpcVectorClient) SearchVectors(ctx context.Context, namespace string, vector []float32, k int) ([]*VectorResult, error) {
	req := &pb.SearchSimilarRequest{
		Namespace:       namespace,
		QueryVector:     vector,
		K:               int32(k),
		Consistency:     pb.VectorConsistencyLevel_VECTOR_CONSISTENCY_LEVEL_QUORUM,
		IncludeVectors:  false,
		IncludeMetadata: true,
	}

	// VectorSearchSimilar returns []*pb.SearchSimilarResponse (already collects streaming responses)
	responses, err := c.grpcClient.VectorSearchSimilar(ctx, req)
	if err != nil {
		return nil, err
	}

	var results []*VectorResult
	for _, resp := range responses {
		for _, r := range resp.Results {
			result := &VectorResult{
				Score: r.Score,
				Found: true,
			}
			if r.Vector != nil {
				result.ID = r.Vector.Id
				if r.Vector.Metadata != nil {
					result.Metadata = r.Vector.Metadata
				}
			}
			results = append(results, result)
		}
	}

	return results, nil
}

// =============================================================================
// QUIC Protocol Client
// =============================================================================

type quicVectorClient struct {
	addr       string
	conn       *quicgo.Conn
	tlsConfig  *tls.Config
	ctx        context.Context
	cancelFunc context.CancelFunc
	// idMap tracks the mapping from uint64 hash to original string ID.
	// This is needed because the QUIC protocol uses uint64 IDs internally,
	// but the test interface uses string IDs for consistency across protocols.
	idMap   map[uint64]string
	idMapMu sync.RWMutex
}

func (c *quicVectorClient) Name() string { return "QUIC" }

func (c *quicVectorClient) Setup(t *testing.T, ctx context.Context) error {
	endpoints := helpers.GetQUICEndpoints()
	require.NotEmpty(t, endpoints, "QUIC endpoints must be available for integration tests")

	addr := endpoints[0]

	tlsConf := &tls.Config{
		InsecureSkipVerify: true, // Test only
		MinVersion:         tls.VersionTLS13,
		NextProtos:         []string{"quicbaas-quic"},
	}

	dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	conn, err := quicgo.DialAddr(dialCtx, addr, tlsConf, nil)
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "failed to connect to QUIC endpoint " + addr, Cause: err}
	}

	clientCtx, clientCancel := context.WithCancel(context.Background())

	c.addr = addr
	c.conn = conn
	c.tlsConfig = tlsConf
	c.ctx = clientCtx
	c.cancelFunc = clientCancel
	c.idMap = make(map[uint64]string)

	return nil
}

func (c *quicVectorClient) Teardown(t *testing.T) error {
	if c.cancelFunc != nil {
		c.cancelFunc()
	}
	if c.conn != nil {
		return c.conn.CloseWithError(0, "client closing")
	}
	return nil
}

func (c *quicVectorClient) sendRequest(ctx context.Context, opcode byte, reqPayload []byte) (*quic.Response, error) {
	stream, err := c.conn.OpenStreamSync(ctx)
	if err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "failed to open stream", Cause: err}
	}
	defer stream.Close()

	req := &quic.Request{
		Opcode:  opcode,
		Payload: reqPayload,
	}

	if err := quic.WriteRequest(stream, req); err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "failed to write request", Cause: err}
	}

	resp, err := quic.ReadResponse(stream)
	if err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "failed to read response", Cause: err}
	}

	if resp.Status != quic.StatusOK {
		errMsg := string(resp.Payload)
		if errMsg == "" {
			errMsg = "unknown error"
		}
		return nil, quic.StatusToError(resp.Status, errMsg)
	}

	return resp, nil
}

// storeIDMapping stores the mapping from uint64 hash to original string ID.
func (c *quicVectorClient) storeIDMapping(stringID string) uint64 {
	hashID := hashStringID(stringID)
	c.idMapMu.Lock()
	c.idMap[hashID] = stringID
	c.idMapMu.Unlock()
	return hashID
}

// lookupStringID retrieves the original string ID from a uint64 hash.
// Returns the stringified hash if no mapping is found.
func (c *quicVectorClient) lookupStringID(hashID uint64) string {
	c.idMapMu.RLock()
	stringID, ok := c.idMap[hashID]
	c.idMapMu.RUnlock()
	if ok {
		return stringID
	}
	// Fallback to stringified hash if no mapping found
	return strconv.FormatUint(hashID, 10)
}

func (c *quicVectorClient) CreateNamespace(ctx context.Context, name string, dimensions int, metric string) error {
	req := &service.CreateNamespaceRequest{
		Namespace:      name,
		Dimension:      uint32(dimensions),
		DistanceMetric: metric,
		IndexType:      "HNSW",
	}

	payload, err := quic.EncodePayload(req)
	if err != nil {
		return err
	}

	_, err = c.sendRequest(ctx, quic.OpVectorCreateNamespace, payload)
	return err
}

func (c *quicVectorClient) DeleteNamespace(ctx context.Context, name string) error {
	req := &service.DeleteNamespaceRequest{
		Namespace: name,
	}

	payload, err := quic.EncodePayload(req)
	if err != nil {
		return err
	}

	_, err = c.sendRequest(ctx, quic.OpVectorDeleteNamespace, payload)
	return err
}

func (c *quicVectorClient) UpsertVector(ctx context.Context, namespace string, id string, vector []float32, metadata map[string]string) error {
	// Convert string ID to uint64 for service request and store the mapping
	idNum := c.storeIDMapping(id)

	// Convert metadata to interface{} map
	var metaInterface map[string]interface{}
	if metadata != nil {
		metaInterface = make(map[string]interface{})
		for k, v := range metadata {
			metaInterface[k] = v
		}
	}

	req := &service.UpsertVectorRequest{
		Namespace: namespace,
		ID:        idNum,
		Vector:    vector,
		Metadata:  metaInterface,
	}

	payload, err := quic.EncodePayload(req)
	if err != nil {
		return err
	}

	_, err = c.sendRequest(ctx, quic.OpVectorUpsert, payload)
	return err
}

func (c *quicVectorClient) GetVector(ctx context.Context, namespace string, id string) (*VectorResult, error) {
	idNum := hashStringID(id)

	req := &service.GetVectorRequest{
		Namespace:       namespace,
		ID:              idNum,
		IncludeVector:   true,
		IncludeMetadata: true,
	}

	payload, err := quic.EncodePayload(req)
	if err != nil {
		return nil, err
	}

	resp, err := c.sendRequest(ctx, quic.OpVectorGet, payload)
	if err != nil {
		return nil, err
	}

	var result service.GetVectorResponse
	if err := quic.DecodePayload(resp.Payload, &result); err != nil {
		return nil, err
	}

	vectorResult := &VectorResult{
		ID:        id,
		Namespace: namespace,
		Found:     result.Found,
		Vector:    result.Vector,
	}

	if result.Metadata != nil {
		vectorResult.Metadata = make(map[string]string)
		for k, v := range result.Metadata {
			if s, ok := v.(string); ok {
				vectorResult.Metadata[k] = s
			}
		}
	}

	return vectorResult, nil
}

func (c *quicVectorClient) DeleteVector(ctx context.Context, namespace string, id string) error {
	idNum := hashStringID(id)

	req := &service.DeleteVectorRequest{
		Namespace: namespace,
		ID:        idNum,
	}

	payload, err := quic.EncodePayload(req)
	if err != nil {
		return err
	}

	_, err = c.sendRequest(ctx, quic.OpVectorDelete, payload)
	return err
}

func (c *quicVectorClient) SearchVectors(ctx context.Context, namespace string, vector []float32, k int) ([]*VectorResult, error) {
	req := &service.SearchSimilarRequest{
		Namespace:       namespace,
		QueryVector:     vector,
		K:               k,
		IncludeVectors:  false,
		IncludeMetadata: true,
	}

	payload, err := quic.EncodePayload(req)
	if err != nil {
		return nil, err
	}

	resp, err := c.sendRequest(ctx, quic.OpVectorSearchSimilar, payload)
	if err != nil {
		return nil, err
	}

	var searchResp service.SearchSimilarResponse
	if err := quic.DecodePayload(resp.Payload, &searchResp); err != nil {
		return nil, err
	}

	var results []*VectorResult
	for _, r := range searchResp.Results {
		result := &VectorResult{
			ID:    c.lookupStringID(r.ID),
			Score: r.Score,
			Found: true,
		}
		if r.Metadata != nil {
			result.Metadata = make(map[string]string)
			for k, v := range r.Metadata {
				if s, ok := v.(string); ok {
					result.Metadata[k] = s
				}
			}
		}
		results = append(results, result)
	}

	return results, nil
}

// =============================================================================
// Unix Socket Protocol Client
// =============================================================================

type unixSocketVectorClient struct {
	grpcClient *grpcclient.Client
	conn       *grpc.ClientConn
}

func (c *unixSocketVectorClient) Name() string { return "UnixSocket" }

func (c *unixSocketVectorClient) Setup(t *testing.T, ctx context.Context) error {
	endpoints := helpers.GetUnixSocketEndpoints()
	require.NotEmpty(t, endpoints, "Unix socket endpoints must be available for integration tests")

	socketPath := endpoints[0]

	// Check if socket exists
	if _, err := net.Dial("unix", socketPath); err != nil {
		return &qberrors.WrappedMessageError{Message: "unix socket not available at " + socketPath, Cause: err}
	}

	// Create gRPC connection over unix socket
	conn, err := grpc.DialContext(ctx,
		"unix://"+socketPath,
		grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS13})),
		grpc.WithDefaultCallOptions(grpc.WaitForReady(true)),
	)
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "failed to connect to unix socket", Cause: err}
	}
	c.conn = conn

	client, err := grpcclient.NewClient("unix://"+socketPath, grpcclient.WithTLSConfig(&tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS13}))
	if err != nil {
		conn.Close()
		return &qberrors.WrappedMessageError{Message: "failed to create gRPC client over unix socket", Cause: err}
	}
	c.grpcClient = client

	return nil
}

func (c *unixSocketVectorClient) Teardown(t *testing.T) error {
	if c.grpcClient != nil {
		c.grpcClient.Close()
	}
	if c.conn != nil {
		c.conn.Close()
	}
	return nil
}

func (c *unixSocketVectorClient) CreateNamespace(ctx context.Context, name string, dimensions int, metric string) error {
	var distMetric pb.DistanceMetric
	switch metric {
	case "COSINE", "cosine":
		distMetric = pb.DistanceMetric_DISTANCE_METRIC_COSINE
	case "DOT_PRODUCT", "dot_product":
		distMetric = pb.DistanceMetric_DISTANCE_METRIC_DOT_PRODUCT
	default:
		distMetric = pb.DistanceMetric_DISTANCE_METRIC_L2
	}

	req := &pb.CreateNamespaceRequest{
		Config: &pb.NamespaceConfig{
			Namespace:      name,
			Dimensions:     int32(dimensions),
			DistanceMetric: distMetric,
			IndexType:      pb.IndexType_INDEX_TYPE_HNSW,
		},
	}

	_, err := c.grpcClient.VectorCreateNamespace(ctx, req)
	return err
}

func (c *unixSocketVectorClient) DeleteNamespace(ctx context.Context, name string) error {
	req := &pb.DeleteNamespaceRequest{
		Namespace: name,
		Force:     true,
	}

	_, err := c.grpcClient.VectorDeleteNamespace(ctx, req)
	return err
}

func (c *unixSocketVectorClient) UpsertVector(ctx context.Context, namespace string, id string, vector []float32, metadata map[string]string) error {
	req := &pb.UpsertVectorRequest{
		Vector: &pb.VectorEntity{
			Id:        id,
			Namespace: namespace,
			Vector:    vector,
			Metadata:  metadata,
		},
		Consistency: pb.VectorConsistencyLevel_VECTOR_CONSISTENCY_LEVEL_QUORUM,
	}

	_, err := c.grpcClient.VectorUpsert(ctx, req)
	return err
}

func (c *unixSocketVectorClient) GetVector(ctx context.Context, namespace string, id string) (*VectorResult, error) {
	req := &pb.GetVectorRequest{
		Namespace:       namespace,
		Id:              id,
		Consistency:     pb.VectorConsistencyLevel_VECTOR_CONSISTENCY_LEVEL_QUORUM,
		IncludeVector:   true,
		IncludeMetadata: true,
	}

	resp, err := c.grpcClient.VectorGet(ctx, req)
	if err != nil {
		return nil, err
	}

	result := &VectorResult{
		ID:        id,
		Namespace: namespace,
		Found:     resp.Found,
	}

	if resp.Vector != nil {
		result.Vector = resp.Vector.Vector
		result.Metadata = resp.Vector.Metadata
	}

	return result, nil
}

func (c *unixSocketVectorClient) DeleteVector(ctx context.Context, namespace string, id string) error {
	req := &pb.DeleteVectorRequest{
		Namespace:   namespace,
		Id:          id,
		Consistency: pb.VectorConsistencyLevel_VECTOR_CONSISTENCY_LEVEL_QUORUM,
	}

	_, err := c.grpcClient.VectorDelete(ctx, req)
	return err
}

func (c *unixSocketVectorClient) SearchVectors(ctx context.Context, namespace string, vector []float32, k int) ([]*VectorResult, error) {
	req := &pb.SearchSimilarRequest{
		Namespace:       namespace,
		QueryVector:     vector,
		K:               int32(k),
		Consistency:     pb.VectorConsistencyLevel_VECTOR_CONSISTENCY_LEVEL_QUORUM,
		IncludeVectors:  false,
		IncludeMetadata: true,
	}

	// VectorSearchSimilar returns []*pb.SearchSimilarResponse (already collects streaming responses)
	responses, err := c.grpcClient.VectorSearchSimilar(ctx, req)
	if err != nil {
		return nil, err
	}

	var results []*VectorResult
	for _, resp := range responses {
		for _, r := range resp.Results {
			result := &VectorResult{
				Score: r.Score,
				Found: true,
			}
			if r.Vector != nil {
				result.ID = r.Vector.Id
				if r.Vector.Metadata != nil {
					result.Metadata = r.Vector.Metadata
				}
			}
			results = append(results, result)
		}
	}

	return results, nil
}

// =============================================================================
// MCP Protocol Client
// =============================================================================

type mcpVectorClient struct {
	endpoint      string
	httpClient    *http.Client
	nextID        int
	accessToken   string
	authenticated bool
}

func (c *mcpVectorClient) Name() string { return "MCP" }

func (c *mcpVectorClient) Setup(t *testing.T, ctx context.Context) error {
	endpoints := helpers.GetMCPEndpoints()
	if len(endpoints) == 0 {
		return &qberrors.MessageError{Message: "no MCP endpoints available"}
	}
	c.endpoint = endpoints[0]
	c.httpClient = &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig:     &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
			TLSHandshakeTimeout: 10 * time.Second,
			DialContext: (&net.Dialer{
				Timeout:   10 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
		},
	}
	c.nextID = 1

	// Verify MCP endpoint via initialize
	initReq := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "initialize",
		"params": map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]interface{}{},
			"clientInfo": map[string]interface{}{
				"name":    "quicbaas-vector-test",
				"version": "1.0.0",
			},
		},
		"id": c.nextID,
	}
	c.nextID++

	body, err := json.Marshal(initReq)
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "failed to marshal init request", Cause: err}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "failed to create request", Cause: err}
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "MCP endpoint not reachable at " + c.endpoint, Cause: err}
	}
	defer resp.Body.Close()

	// Authenticate using bootstrap credentials
	if err := c.authenticate(ctx); err != nil {
		return &qberrors.WrappedMessageError{Message: "MCP authentication failed", Cause: err}
	}

	return nil
}

func (c *mcpVectorClient) authenticate(ctx context.Context) error {
	// Acquire a bearer through the cluster's PIV mTLS login endpoint.
	// Password-based login has been removed across all transports; tests
	// mint a bearer via REST PIV and present it on the protocol under test.
	restEndpoints := helpers.GetRESTEndpoints()
	if len(restEndpoints) == 0 {
		return &qberrors.MessageError{Message: "no REST endpoints available for PIV login"}
	}
	bearer, err := helpers.AuthPIVContext(ctx, restEndpoints[0], "", "", "")
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "PIV login failed", Cause: err}
	}
	c.accessToken = bearer

	return nil
}


func (c *mcpVectorClient) Teardown(t *testing.T) error {
	c.httpClient = nil
	return nil
}

func (c *mcpVectorClient) callMCPTool(ctx context.Context, toolName string, args map[string]interface{}) (json.RawMessage, error) {
	mcpReq := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name":      toolName,
			"arguments": args,
		},
		"id": c.nextID,
	}
	c.nextID++

	body, err := json.Marshal(mcpReq)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.authenticated && c.accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.accessToken)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var mcpResp struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal(respBody, &mcpResp); err != nil {
		return nil, err
	}

	if mcpResp.Error != nil {
		return nil, &qberrors.MessageError{Message: "MCP error: " + mcpResp.Error.Message}
	}

	if mcpResp.Result.IsError && len(mcpResp.Result.Content) > 0 {
		return nil, &qberrors.MessageError{Message: "MCP tool error: " + mcpResp.Result.Content[0].Text}
	}

	if len(mcpResp.Result.Content) > 0 {
		return json.RawMessage(mcpResp.Result.Content[0].Text), nil
	}

	return nil, nil
}

func (c *mcpVectorClient) CreateNamespace(ctx context.Context, name string, dimensions int, metric string) error {
	args := map[string]interface{}{
		"name":       name,
		"dimensions": dimensions,
	}
	if metric != "" {
		args["metric"] = metric
	}
	_, err := c.callMCPTool(ctx, "quicbaas_vector_create_namespace", args)
	return err
}

func (c *mcpVectorClient) DeleteNamespace(ctx context.Context, name string) error {
	args := map[string]interface{}{
		"name": name,
	}
	_, err := c.callMCPTool(ctx, "quicbaas_vector_delete_namespace", args)
	return err
}

func (c *mcpVectorClient) UpsertVector(ctx context.Context, namespace string, id string, vector []float32, metadata map[string]string) error {
	args := map[string]interface{}{
		"namespace": namespace,
		"id":        id,
		"vector":    vector,
	}
	if len(metadata) > 0 {
		args["metadata"] = metadata
	}
	_, err := c.callMCPTool(ctx, "quicbaas_vector_upsert", args)
	return err
}

func (c *mcpVectorClient) GetVector(ctx context.Context, namespace string, id string) (*VectorResult, error) {
	args := map[string]interface{}{
		"namespace": namespace,
		"id":        id,
	}

	result, err := c.callMCPTool(ctx, "quicbaas_vector_get", args)
	if err != nil {
		return nil, err
	}

	var resp struct {
		ID        string            `json:"id"`
		Namespace string            `json:"namespace"`
		Vector    []float32         `json:"vector"`
		Metadata  map[string]string `json:"metadata"`
		Found     bool              `json:"found"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, err
	}

	return &VectorResult{
		ID:        resp.ID,
		Namespace: resp.Namespace,
		Vector:    resp.Vector,
		Metadata:  resp.Metadata,
		Found:     resp.Found,
	}, nil
}

func (c *mcpVectorClient) DeleteVector(ctx context.Context, namespace string, id string) error {
	args := map[string]interface{}{
		"namespace": namespace,
		"id":        id,
	}
	_, err := c.callMCPTool(ctx, "quicbaas_vector_delete", args)
	return err
}

func (c *mcpVectorClient) SearchVectors(ctx context.Context, namespace string, vector []float32, k int) ([]*VectorResult, error) {
	args := map[string]interface{}{
		"namespace": namespace,
		"vector":    vector,
		"k":         k,
	}

	result, err := c.callMCPTool(ctx, "quicbaas_vector_search", args)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Results []struct {
			ID        string            `json:"id"`
			Namespace string            `json:"namespace"`
			Vector    []float32         `json:"vector"`
			Metadata  map[string]string `json:"metadata"`
			Score     float32           `json:"score"`
		} `json:"results"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, err
	}

	results := make([]*VectorResult, len(resp.Results))
	for i, r := range resp.Results {
		results[i] = &VectorResult{
			ID:        r.ID,
			Namespace: r.Namespace,
			Vector:    r.Vector,
			Metadata:  r.Metadata,
			Score:     r.Score,
			Found:     true,
		}
	}

	return results, nil
}

// =============================================================================
// Embedded Protocol Client
// =============================================================================

// ErrEmbeddedNotAvailable is returned when the embedded protocol cannot be used
// in the integration test context because it requires an in-process service instance.
type ErrEmbeddedNotAvailable struct{}

func (e ErrEmbeddedNotAvailable) Error() string {
	return "embedded protocol not available in integration test context (requires in-process service instance)"
}

// ErrEmbeddedClientNotInitialized is returned when attempting to use an
// uninitialized embedded client.
type ErrEmbeddedClientNotInitialized struct{}

func (e ErrEmbeddedClientNotInitialized) Error() string {
	return "embedded client not initialized"
}

type embeddedVectorClient struct {
	client quicbaas.Client
}

func (c *embeddedVectorClient) Name() string { return "Embedded" }

func (c *embeddedVectorClient) Setup(t *testing.T, ctx context.Context) error {
	svc, err := helpers.GetEmbeddedService(t)
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "failed to get embedded service", Cause: err}
	}
	client, err := quicbaas.NewClient("embedded://local", quicbaas.WithEmbedded(svc))
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "failed to create embedded client", Cause: err}
	}
	c.client = client
	return nil
}

func (c *embeddedVectorClient) Teardown(t *testing.T) error {
	if c.client != nil {
		return c.client.Close()
	}
	return nil
}

func (c *embeddedVectorClient) CreateNamespace(ctx context.Context, name string, dimensions int, metric string) error {
	if c.client == nil {
		return ErrEmbeddedClientNotInitialized{}
	}
	return ErrEmbeddedClientNotInitialized{}
}

func (c *embeddedVectorClient) DeleteNamespace(ctx context.Context, name string) error {
	if c.client == nil {
		return ErrEmbeddedClientNotInitialized{}
	}
	return ErrEmbeddedClientNotInitialized{}
}

func (c *embeddedVectorClient) UpsertVector(ctx context.Context, namespace string, id string, vector []float32, metadata map[string]string) error {
	if c.client == nil {
		return ErrEmbeddedClientNotInitialized{}
	}
	return ErrEmbeddedClientNotInitialized{}
}

func (c *embeddedVectorClient) GetVector(ctx context.Context, namespace string, id string) (*VectorResult, error) {
	if c.client == nil {
		return nil, ErrEmbeddedClientNotInitialized{}
	}
	return nil, ErrEmbeddedClientNotInitialized{}
}

func (c *embeddedVectorClient) DeleteVector(ctx context.Context, namespace string, id string) error {
	if c.client == nil {
		return ErrEmbeddedClientNotInitialized{}
	}
	return ErrEmbeddedClientNotInitialized{}
}

func (c *embeddedVectorClient) SearchVectors(ctx context.Context, namespace string, vector []float32, k int) ([]*VectorResult, error) {
	if c.client == nil {
		return nil, ErrEmbeddedClientNotInitialized{}
	}
	return nil, ErrEmbeddedClientNotInitialized{}
}

// =============================================================================
// Helper Functions
// =============================================================================

// generateRandomVector generates a random float32 vector of the given dimension.
func generateRandomVector(dimension int) []float32 {
	vec := make([]float32, dimension)
	for i := range vec {
		vec[i] = rand.Float32()
	}
	return vec
}

// hashStringID converts a string ID to a uint64 hash for QUIC protocol.
// This is needed because the service layer uses uint64 IDs.
func hashStringID(id string) uint64 {
	var hash uint64 = 5381
	for _, c := range id {
		hash = ((hash << 5) + hash) + uint64(c)
	}
	return hash
}
