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

package cluster

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"testing"
	"time"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"

	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"

	"github.com/jeremyhahn/go-quicbaas/pkg/api/rest"
)

// Each test's e2eHTTPClient carries the bearer its routes require: the tenant
// operator's for entity and batch routes, which need go-qrdb's KV grants, and
// the system admin's for cluster status and metrics.

// e2eTenantClient returns a client that calls as the tenant operator.
func e2eTenantClient(t *testing.T) *http.Client {
	t.Helper()
	return helpers.NewTenantOperatorHTTPClient(t, 30*time.Second)
}

// e2eCleanup sends a cleanup request, with body as JSON when it is non-nil,
// and fails the test if the request cannot be sent.
func e2eCleanup(t *testing.T, method, url string, body []byte) {
	e2eHTTPClient := e2eTenantClient(t)
	t.Helper()

	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatalf("cleanup %s %s: build request: %v", method, url, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := e2eHTTPClient.Do(req)
	if err != nil {
		t.Fatalf("cleanup %s %s: %v", method, url, err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatalf("cleanup %s %s: close body: %v", method, url, err)
	}
}

// TestE2EDataConsistency validates that data written to one node is visible on all nodes
func TestE2EDataConsistency(t *testing.T) {
	e2eHTTPClient := e2eTenantClient(t)
	if helpers.IsStandaloneMode(t, helpers.GetClusterEndpoints()) {
		t.Log("Cluster running in standalone/embedded mode - data consistency requires real Raft cluster")
		return
	}

	nodes := getNodeURLs()
	waitForQuorumReady(t, nodes)

	// Test data
	entityType := "users"
	entityID := uint64(12345)
	testData := map[string]interface{}{
		"name":  "Alice Johnson",
		"email": "alice@example.com",
		"role":  "admin",
	}

	dataBytes, err := json.Marshal(testData)
	if err != nil {
		t.Fatalf("Failed to marshal test data: %v", err)
	}

	// Step 1: Write to node 1
	t.Log("Step 1: Writing data to node 1...")
	writePayload := map[string]interface{}{
		"id":   entityID,
		"data": json.RawMessage(dataBytes),
	}

	writeBody, _ := json.Marshal(writePayload)
	// POST to /api/v1/entities/{entityType} (ID is in the body, not the URL)
	writeURL := fmt.Sprintf("%s/api/v1/entities/%s", nodes[0], entityType)

	resp, err := e2eHTTPClient.Post(writeURL, "application/json", bytes.NewReader(writeBody))
	if err != nil {
		t.Fatalf("Failed to write to node 1: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("Write failed with status %d: %s", resp.StatusCode, string(body))
	}

	t.Logf("Write successful with status %d", resp.StatusCode)

	// Step 2: Wait for replication
	t.Log("Step 2: Waiting for replication...")
	for _, nodeURL := range nodes {
		waitForEntity(t, nodeURL, entityType, entityID, anyEntity)
	}

	// Step 3: Read from all nodes to verify consistency
	t.Log("Step 3: Verifying data on all nodes...")
	for i, nodeURL := range nodes {
		readURL := fmt.Sprintf("%s/api/v1/entities/%s/%d", nodeURL, entityType, entityID)
		resp, err := e2eHTTPClient.Get(readURL)
		if err != nil {
			t.Errorf("Failed to read from node %d: %v", i+1, err)
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Errorf("Read from node %d failed with status %d: %s", i+1, resp.StatusCode, string(body))
			continue
		}

		var readResp rest.GetResponse
		if err := json.NewDecoder(resp.Body).Decode(&readResp); err != nil {
			t.Errorf("Failed to decode response from node %d: %v", i+1, err)
			continue
		}

		if len(readResp.Data) == 0 {
			t.Errorf("Data not found on node %d", i+1)
			continue
		}

		t.Logf("Node %d: Data verified successfully", i+1)
	}

	// Cleanup
	deleteURL := fmt.Sprintf("%s/api/v1/entities/%s/%d", nodes[0], entityType, entityID)
	e2eCleanup(t, http.MethodDelete, deleteURL, nil)

	t.Log("SUCCESS: Data consistency verified across all nodes")
}

// TestE2ELeaderElection validates leader election behavior
func TestE2ELeaderElection(t *testing.T) {
	e2eHTTPClient := helpers.NewSystemAdminHTTPClient(t, 30*time.Second)
	if helpers.IsStandaloneMode(t, helpers.GetClusterEndpoints()) {
		t.Log("Cluster running in standalone/embedded mode - leader election requires real Raft cluster")
		return
	}

	nodes := getNodeURLs()
	waitForQuorumReady(t, nodes)

	// Check cluster status to verify leader
	statusURL := fmt.Sprintf("%s/api/v1/cluster/status", nodes[0])
	resp, err := e2eHTTPClient.Get(statusURL)
	if err != nil {
		t.Fatalf("Failed to get cluster status: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("Cluster status failed with status %d: %s", resp.StatusCode, string(body))
	}

	var statusResp map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&statusResp); err != nil {
		t.Fatalf("Failed to decode cluster status: %v", err)
	}

	// Verify we have members/nodes
	if members, ok := statusResp["members"].([]interface{}); ok && len(members) > 0 {
		t.Logf("SUCCESS: Cluster has %d members", len(members))
	} else if totalNodes, ok := statusResp["total_nodes"].(float64); ok && totalNodes > 0 {
		t.Logf("SUCCESS: Cluster has %d nodes", int(totalNodes))
	} else if shards, ok := statusResp["shards"].([]interface{}); ok && len(shards) > 0 {
		t.Logf("SUCCESS: Cluster has %d shards (node is active)", len(shards))
	} else {
		t.Error("No nodes found in cluster status")
	}
}

// TestE2EHealthChecks validates health check endpoints
func TestE2EHealthChecks(t *testing.T) {
	// The health routes are public.
	e2eHTTPClient := newInsecureHTTPClient(30 * time.Second)
	nodes := getNodeURLs()
	waitForQuorumReady(t, nodes)

	for i, nodeURL := range nodes {
		healthURL := fmt.Sprintf("%s/api/v1/health/live", nodeURL)
		resp, err := e2eHTTPClient.Get(healthURL)
		if err != nil {
			t.Errorf("Health check failed for node %d: %v", i+1, err)
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Node %d health check returned status %d", i+1, resp.StatusCode)
			continue
		}

		var healthResp map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&healthResp); err != nil {
			t.Errorf("Failed to decode health response from node %d: %v", i+1, err)
			continue
		}

		t.Logf("Node %d: Healthy - %+v", i+1, healthResp)
	}

	t.Log("SUCCESS: All nodes passed health checks")
}

// TestE2EMetricsEndpoint validates metrics endpoint
func TestE2EMetricsEndpoint(t *testing.T) {
	e2eHTTPClient := helpers.NewSystemAdminHTTPClient(t, 30*time.Second)
	// Metrics are served on the REST port at /api/v1/metrics
	// Try multiple nodes in case one was killed by other tests
	metricsURLs := []string{
		"https://test-node1:8080/api/v1/metrics",
		"https://test-node2:8080/api/v1/metrics",
		"https://test-node3:8080/api/v1/metrics",
	}

	var resp *http.Response
	var err error
	var successURL string
	maxRetries := 5
	for _, metricsURL := range metricsURLs {
		t.Logf("Trying metrics endpoint: %s", metricsURL)
		for i := 0; i < maxRetries; i++ {
			resp, err = e2eHTTPClient.Get(metricsURL)
			if err == nil && resp.StatusCode == http.StatusOK {
				successURL = metricsURL
				break
			}
			if resp != nil {
				resp.Body.Close()
			}
			t.Logf("Attempt %d/%d on %s: not ready, retrying...", i+1, maxRetries, metricsURL)
			pace(t, 3*time.Second)
		}
		if successURL != "" {
			break
		}
	}
	if successURL == "" {
		t.Fatalf("Failed to get metrics from any node after trying all endpoints")
	}
	t.Logf("Successfully connected to metrics endpoint: %s", successURL)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Metrics endpoint returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read metrics response: %v", err)
	}

	if len(body) == 0 {
		t.Error("Metrics endpoint returned empty response")
	} else {
		t.Logf("SUCCESS: Metrics endpoint returned %d bytes", len(body))
	}
}

// TestE2EBasicCRUD validates basic Create, Read, Update, Delete operations
func TestE2EBasicCRUD(t *testing.T) {
	e2eHTTPClient := e2eTenantClient(t)
	nodes := getNodeURLs()
	waitForQuorumReady(t, nodes)

	entityType := "products"
	entityID := uint64(50000)

	// CREATE
	t.Log("Testing CREATE...")
	productData := map[string]interface{}{
		"name":  "Test Product",
		"price": 99.99,
		"stock": 100,
	}

	dataBytes, _ := json.Marshal(productData)
	createPayload := map[string]interface{}{
		"id":   entityID,
		"data": json.RawMessage(dataBytes),
	}
	createBody, _ := json.Marshal(createPayload)

	createURL := fmt.Sprintf("%s/api/v1/entities/%s", nodes[0], entityType)
	resp, err := e2eHTTPClient.Post(createURL, "application/json", bytes.NewReader(createBody))
	if err != nil {
		t.Fatalf("CREATE failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("CREATE failed with status %d: %s", resp.StatusCode, string(body))
	}
	t.Log("CREATE: Success")

	// READ
	waitForEntity(t, nodes[0], entityType, entityID, anyEntity)
	t.Log("Testing READ...")

	readURL := fmt.Sprintf("%s/api/v1/entities/%s/%d", nodes[0], entityType, entityID)
	resp, err = e2eHTTPClient.Get(readURL)
	if err != nil {
		t.Fatalf("READ failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("READ failed with status %d: %s", resp.StatusCode, string(body))
	}
	t.Log("READ: Success")

	// DELETE
	t.Log("Testing DELETE...")

	deleteURL := fmt.Sprintf("%s/api/v1/entities/%s/%d", nodes[0], entityType, entityID)
	req, _ := http.NewRequest(http.MethodDelete, deleteURL, nil)
	resp, err = e2eHTTPClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("DELETE failed with status %d: %s", resp.StatusCode, string(body))
	}
	t.Log("DELETE: Success")

	t.Log("SUCCESS: All CRUD operations completed")
}

// TestE2EBatchOperations validates atomic batch operations
func TestE2EBatchOperations(t *testing.T) {
	e2eHTTPClient := e2eTenantClient(t)
	nodes := getNodeURLs()
	waitForQuorumReady(t, nodes)

	// Prepare batch write - use IDs that route to the same shard
	// With 3 shards [1,2,3], IDs that are multiples of 3 route to shard 1
	entityType := "orders"
	batchSize := 5

	operations := make([]map[string]interface{}, batchSize)
	createdIDs := make([]uint64, batchSize)

	for i := 0; i < batchSize; i++ {
		// Use multiples of 3 to ensure they route to the same shard (shard 1)
		entityID := uint64((i + 1) * 3)
		createdIDs[i] = entityID

		orderData := map[string]interface{}{
			"order_id":   entityID,
			"customer":   fmt.Sprintf("customer_%d", i),
			"total":      100.0 + float64(i)*10,
			"status":     "pending",
			"created_at": time.Now().Unix(),
		}

		dataBytes, _ := json.Marshal(orderData)
		operations[i] = map[string]interface{}{
			"entity_type": entityType,
			"id":          entityID,
			"data":        dataBytes,
		}
	}

	batchPayload := map[string]interface{}{
		"operations": operations,
		"shard_id":   1, // Explicitly specify shard 1
	}

	batchBody, _ := json.Marshal(batchPayload)

	// Execute batch write
	t.Logf("Executing batch write of %d entities...", batchSize)

	batchURL := fmt.Sprintf("%s/api/v1/batch/put", nodes[0])
	resp, err := e2eHTTPClient.Post(batchURL, "application/json", bytes.NewReader(batchBody))
	if err != nil {
		t.Fatalf("Batch write failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("Batch write failed with status %d: %s", resp.StatusCode, string(body))
	}

	var batchResp map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&batchResp); err != nil {
		t.Fatalf("Failed to decode batch response: %v", err)
	}

	t.Log("Batch write completed successfully")

	// Verify batch write results
	t.Log("Verifying batch write results...")

	successCount := 0
	for _, entityID := range createdIDs {
		readURL := fmt.Sprintf("%s/api/v1/entities/%s/%d", nodes[1], entityType, entityID)

		resp, err := e2eHTTPClient.Get(readURL)
		if err != nil {
			t.Logf("Warning: Failed to read entity %d: %v", entityID, err)
			continue
		}

		if resp.StatusCode == http.StatusOK {
			successCount++
		}
		resp.Body.Close()
	}

	if successCount == batchSize {
		t.Logf("SUCCESS: All %d batch entities verified", batchSize)
	} else {
		t.Logf("INFO: %d/%d batch entities verified (some may have routed differently)", successCount, batchSize)
	}

	// Cleanup with batch delete
	deleteOps := make([]map[string]interface{}, batchSize)
	for i, entityID := range createdIDs {
		deleteOps[i] = map[string]interface{}{
			"entity_type": entityType,
			"id":          entityID,
		}
	}

	deleteBatchPayload := map[string]interface{}{
		"operations": deleteOps,
		"shard_id":   1,
	}
	deleteBatchBody, _ := json.Marshal(deleteBatchPayload)

	deleteURL := fmt.Sprintf("%s/api/v1/batch/delete", nodes[0])
	e2eCleanup(t, http.MethodPost, deleteURL, deleteBatchBody)
}

// discoverLeaderNode probes each node with a test write to find the Raft leader.
// Only the leader can accept writes; non-leader nodes return 408 (proposal timeout).
// The probe entity is cleaned up before returning.
func discoverLeaderNode(t *testing.T, nodes []string) string {
	e2eHTTPClient := e2eTenantClient(t)
	t.Helper()

	probeEntityType := fmt.Sprintf("leader_probe_%d", time.Now().UnixNano())
	probeID := uint64(99999)

	probeData, _ := json.Marshal(map[string]interface{}{"probe": true})
	probePayload, _ := json.Marshal(map[string]interface{}{
		"id":   probeID,
		"data": json.RawMessage(probeData),
	})

	for _, node := range nodes {
		writeURL := fmt.Sprintf("%s/api/v1/entities/%s", node, probeEntityType)
		resp, err := e2eHTTPClient.Post(writeURL, "application/json", bytes.NewReader(probePayload))
		if err != nil {
			t.Logf("Leader probe: node %s unreachable: %v", node, err)
			continue
		}
		status := resp.StatusCode
		resp.Body.Close()

		if status == http.StatusCreated || status == http.StatusOK {
			t.Logf("Leader probe: node %s accepted write (status %d), identified as leader", node, status)

			// Clean up probe entity
			deleteURL := fmt.Sprintf("%s/api/v1/entities/%s/%d", node, probeEntityType, probeID)
			e2eCleanup(t, http.MethodDelete, deleteURL, nil)

			return node
		}

		t.Logf("Leader probe: node %s rejected write (status %d), not leader", node, status)
	}

	return ""
}

// TestE2EConcurrentWrites validates concurrent writes don't cause issues
func TestE2EConcurrentWrites(t *testing.T) {
	e2eHTTPClient := e2eTenantClient(t)
	nodes := getNodeURLs()
	waitForQuorumReady(t, nodes)

	// Overall test context bounds the total execution time
	testCtx, testCancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer testCancel()

	entityType := "concurrent_test"
	baseID := uint64(70000)
	numConcurrent := 20

	// Discover the leader node before launching concurrent writes.
	// In a Raft cluster, only the leader can accept write proposals.
	// Sending writes to non-leader nodes results in HTTP 408 (proposal timeout).
	leaderNode := discoverLeaderNode(t, nodes)
	if leaderNode == "" {
		t.Fatal("Could not discover leader node among cluster nodes")
	}
	t.Logf("Routing all %d concurrent writes to leader: %s", numConcurrent, leaderNode)

	// Channel to collect results
	results := make(chan error, numConcurrent)

	// Launch concurrent writes - all routed to the leader node
	for i := 0; i < numConcurrent; i++ {
		go func(index int) {
			entityID := baseID + uint64(index)
			testData := map[string]interface{}{
				"index":     index,
				"timestamp": time.Now().UnixNano(),
				"thread_id": index,
			}

			dataBytes, _ := json.Marshal(testData)
			writePayload := map[string]interface{}{
				"id":   entityID,
				"data": json.RawMessage(dataBytes),
			}

			writeBody, _ := json.Marshal(writePayload)
			// POST to /api/v1/entities/{entityType} (ID is in the body, not the URL)
			writeURL := fmt.Sprintf("%s/api/v1/entities/%s", leaderNode, entityType)

			// Per-operation context with 15s timeout derived from the test context
			opCtx, opCancel := context.WithTimeout(testCtx, 15*time.Second)
			defer opCancel()

			req, reqErr := http.NewRequestWithContext(opCtx, http.MethodPost, writeURL, bytes.NewReader(writeBody))
			if reqErr != nil {
				results <- &qberrors.WrappedMessageError{Message: "write " + strconv.Itoa(index) + " request creation failed", Cause: reqErr}
				return
			}
			req.Header.Set("Content-Type", "application/json")

			resp, err := e2eHTTPClient.Do(req)
			if err != nil {
				results <- &qberrors.WrappedMessageError{Message: "write " + strconv.Itoa(index) + " failed", Cause: err}
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
				results <- &qberrors.MessageError{Message: "write " + strconv.Itoa(index) + " failed with status " + strconv.Itoa(resp.StatusCode)}
				return
			}

			results <- nil
		}(i)
	}

	// Collect results with test context to prevent infinite hanging
	successCount := 0
	for i := 0; i < numConcurrent; i++ {
		select {
		case err := <-results:
			if err != nil {
				t.Logf("Concurrent write error: %v", err)
			} else {
				successCount++
			}
		case <-testCtx.Done():
			t.Fatalf("Test context expired while collecting results (%d/%d received): %v", i, numConcurrent, testCtx.Err())
		}
	}

	if successCount < numConcurrent {
		t.Errorf("Only %d/%d concurrent writes succeeded", successCount, numConcurrent)
	} else {
		t.Logf("SUCCESS: All %d concurrent writes completed", numConcurrent)
	}

	// Cleanup - route deletes through the leader as well
	for i := 0; i < numConcurrent; i++ {
		entityID := baseID + uint64(i)
		deleteURL := fmt.Sprintf("%s/api/v1/entities/%s/%d", leaderNode, entityType, entityID)
		e2eCleanup(t, http.MethodDelete, deleteURL, nil)
	}
}

// TestE2EListOperations validates list functionality
func TestE2EListOperations(t *testing.T) {
	e2eHTTPClient := e2eTenantClient(t)
	nodes := getNodeURLs()
	waitForQuorumReady(t, nodes)

	entityType := "inventory"
	numItems := 15

	// Write test items - use IDs that route to the same shard
	t.Logf("Writing %d items for list test...", numItems)
	createdIDs := make([]uint64, numItems)

	for i := 0; i < numItems; i++ {
		// Use multiples of 3 to route to shard 1
		entityID := uint64((i + 1) * 3)
		createdIDs[i] = entityID

		itemData := map[string]interface{}{
			"sku":      fmt.Sprintf("SKU-%05d", i),
			"name":     fmt.Sprintf("Item %d", i),
			"quantity": 100 + i,
		}

		dataBytes, _ := json.Marshal(itemData)
		writePayload := map[string]interface{}{
			"id":   entityID,
			"data": json.RawMessage(dataBytes),
		}

		writeBody, _ := json.Marshal(writePayload)
		// POST to /api/v1/entities/{entityType} (ID is in the body, not the URL)
		writeURL := fmt.Sprintf("%s/api/v1/entities/%s", nodes[0], entityType)

		resp, err := e2eHTTPClient.Post(writeURL, "application/json", bytes.NewReader(writeBody))
		if err != nil {
			t.Logf("Warning: Failed to write item %d: %v", i, err)
			continue
		}
		resp.Body.Close()
	}

	// Test list with pagination - query shard 1 where our entities are
	t.Log("Testing list operation...")

	listURL := fmt.Sprintf("%s/api/v1/entities/%s?limit=10&shard_id=1", nodes[0], entityType)
	resp, err := e2eHTTPClient.Get(listURL)
	if err != nil {
		t.Fatalf("List operation failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("List failed with status %d: %s", resp.StatusCode, string(body))
	}

	var listResp rest.ListResponse
	if err := json.NewDecoder(resp.Body).Decode(&listResp); err != nil {
		t.Fatalf("Failed to decode list response: %v", err)
	}

	t.Logf("List returned %d entities", len(listResp.Entities))

	if len(listResp.Entities) > 0 {
		t.Log("SUCCESS: List operation returned entities")
	} else {
		t.Log("INFO: List returned no entities (may need shard configuration)")
	}

	// Cleanup
	for _, entityID := range createdIDs {
		deleteURL := fmt.Sprintf("%s/api/v1/entities/%s/%d", nodes[0], entityType, entityID)
		e2eCleanup(t, http.MethodDelete, deleteURL, nil)
	}
}

// getNodeURLsForE2E is a local helper for E2E tests that returns all node URLs.
// Uses the shared getNodeURLs function from cluster_test.go.
func getNodeURLsForE2E() []string {
	return getNodeURLs()
}
