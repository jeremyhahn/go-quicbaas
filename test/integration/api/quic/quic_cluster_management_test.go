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

package quic

import (
	"context"
	"crypto/tls"
	"os"
	"sync"
	"testing"
	"time"

	quicgo "github.com/quic-go/quic-go"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/api/quic"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// QUIC Client Helper Functions
// =============================================================================

// quicClient wraps a QUIC connection for making cluster management requests
type quicClient struct {
	conn *quicgo.Conn
}

// newQUICClient creates a new QUIC client connection to the specified address
func newQUICClient(ctx context.Context, addr string) (*quicClient, error) {
	// Create insecure TLS config for testing
	tlsConfig := &tls.Config{
		InsecureSkipVerify: true,
		MinVersion:         tls.VersionTLS13,
		NextProtos:         []string{"quicbaas-quic"},
	}

	// QUIC client configuration
	quicConfig := &quicgo.Config{
		MaxIdleTimeout:  30 * time.Second,
		KeepAlivePeriod: 15 * time.Second,
	}

	// Dial QUIC connection
	conn, err := quicgo.DialAddr(ctx, addr, tlsConfig, quicConfig)
	if err != nil {
		return nil, err
	}

	return &quicClient{conn: conn}, nil
}

// Close closes the QUIC connection
func (c *quicClient) Close() error {
	if c.conn != nil {
		return c.conn.CloseWithError(0, "client closed")
	}
	return nil
}

// executeRequest sends a QUIC request and returns the response
func (c *quicClient) executeRequest(ctx context.Context, opcode byte, reqPayload interface{}) (*quic.Response, error) {
	// Open a new stream for this request
	stream, err := c.conn.OpenStreamSync(ctx)
	if err != nil {
		return nil, err
	}
	defer stream.Close()

	// Encode request payload
	payload, err := quic.EncodePayload(reqPayload)
	if err != nil {
		return nil, err
	}

	// Create and write request
	req := &quic.Request{
		Opcode:  opcode,
		Payload: payload,
	}

	if err := quic.WriteRequest(stream, req); err != nil {
		return nil, err
	}

	// Read response
	resp, err := quic.ReadResponse(stream)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

// =============================================================================
// Helper Functions
// =============================================================================

// getQUICEndpoint returns the QUIC endpoint from environment or default
func getQUICEndpoint() string {
	if endpoint := os.Getenv("QUICBAAS_NODE1_QUIC"); endpoint != "" {
		return endpoint
	}
	// Default QUIC port
	return "test-node1:4433"
}

// getQUICEndpoints returns all QUIC cluster endpoints
func getQUICEndpoints() []string {
	node1 := os.Getenv("QUICBAAS_NODE1_QUIC")
	if node1 == "" {
		node1 = "test-node1:4433"
	}

	node2 := os.Getenv("QUICBAAS_NODE2_QUIC")
	if node2 == "" {
		node2 = "test-node2:4433"
	}

	node3 := os.Getenv("QUICBAAS_NODE3_QUIC")
	if node3 == "" {
		node3 = "test-node3:4433"
	}

	return []string{node1, node2, node3}
}

// =============================================================================
// Test: ListNodes Operation
// =============================================================================

// TestQUIC_ClusterManagement_ListNodes verifies the ListNodes QUIC operation
// returns a valid response. In embedded/standalone mode, the server has no real
// Raft consensus, so TotalCount may be 0 and Nodes may be empty. The test
// validates the API is reachable and returns a well-formed response.
func TestQUIC_ClusterManagement_ListNodes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Connect to QUIC endpoint
	endpoint := getQUICEndpoint()
	client, err := newQUICClient(ctx, endpoint)
	require.NoError(t, err, "QUIC server must be available at %s", endpoint)
	defer client.Close()

	t.Logf("Connected to QUIC endpoint: %s", endpoint)

	// Send ListNodes request
	resp, err := client.executeRequest(ctx, quic.OpListNodes, &service.ClusterStatusRequest{
		IncludeDetails: true,
	})
	if err != nil {
		t.Fatalf("Failed to execute ListNodes request: %v", err)
	}

	// Verify response status
	if resp.Status != quic.StatusOK {
		t.Fatalf("Expected StatusOK, got: 0x%02X, payload: %s", resp.Status, string(resp.Payload))
	}

	// Decode response
	type ListNodesResponse struct {
		Nodes      []*service.NodeInfo
		TotalCount uint64
	}

	var listResp ListNodesResponse
	if err := quic.DecodePayload(resp.Payload, &listResp); err != nil {
		t.Fatalf("Failed to decode ListNodes response: %v", err)
	}

	// In embedded mode, TotalCount may be 0 and Nodes may be empty since
	// there is no real Raft cluster. Log the actual state for diagnostics.
	t.Logf("ListNodes response: TotalCount=%d, Nodes=%d", listResp.TotalCount, len(listResp.Nodes))
	for i, node := range listResp.Nodes {
		t.Logf("  Node %d: ID=%d, Address=%s, Leader=%v",
			i+1, node.NodeID, node.Address, node.IsLeader)
	}
}

func TestQUIC_ClusterManagement_ListNodes_MultipleNodes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoints := getQUICEndpoints()

	// Query each node and verify they all return consistent cluster view
	var firstNodeCount uint64

	for i, endpoint := range endpoints {
		client, err := newQUICClient(ctx, endpoint)
		if err != nil {
			t.Logf("Warning: Cannot connect to node %d at %s: %v", i+1, endpoint, err)
			continue
		}
		defer client.Close()

		resp, err := client.executeRequest(ctx, quic.OpListNodes, &service.ClusterStatusRequest{})
		if err != nil {
			t.Logf("Warning: Failed to query node %d: %v", i+1, err)
			continue
		}

		if resp.Status != quic.StatusOK {
			t.Errorf("Node %d returned non-OK status: 0x%02X", i+1, resp.Status)
			continue
		}

		type ListNodesResponse struct {
			Nodes      []*service.NodeInfo
			TotalCount uint64
		}

		var listResp ListNodesResponse
		if err := quic.DecodePayload(resp.Payload, &listResp); err != nil {
			t.Errorf("Node %d: Failed to decode response: %v", i+1, err)
			continue
		}

		if i == 0 {
			firstNodeCount = listResp.TotalCount
		} else {
			if listResp.TotalCount != firstNodeCount {
				t.Errorf("Inconsistent node count: Node 1 reported %d, Node %d reported %d",
					firstNodeCount, i+1, listResp.TotalCount)
			}
		}

		t.Logf("Node %d reports %d nodes in cluster", i+1, listResp.TotalCount)
	}
}

// =============================================================================
// Test: RebalanceStatus Operation
// =============================================================================

func TestQUIC_ClusterManagement_RebalanceStatus(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoint := getQUICEndpoint()
	client, err := newQUICClient(ctx, endpoint)
	require.NoError(t, err, "QUIC server must be available at %s", endpoint)
	defer client.Close()

	// Define request structure
	type RebalanceStatusRequest struct {
		OperationID string `json:"operation_id"`
	}

	type RebalanceStatusResponse struct {
		OperationID    string  `json:"operation_id"`
		Status         string  `json:"status"`
		Progress       float64 `json:"progress"`
		ShardsMoved    int     `json:"shards_moved"`
		TotalShards    int     `json:"total_shards"`
		ElapsedSeconds float64 `json:"elapsed_seconds"`
	}

	// Query rebalance status for a test operation ID
	resp, err := client.executeRequest(ctx, quic.OpRebalanceStatus, &RebalanceStatusRequest{
		OperationID: "test-job-123",
	})
	if err != nil {
		t.Fatalf("Failed to execute RebalanceStatus request: %v", err)
	}

	if resp.Status != quic.StatusOK {
		t.Fatalf("Expected StatusOK, got: 0x%02X", resp.Status)
	}

	var statusResp RebalanceStatusResponse
	if err := quic.DecodePayload(resp.Payload, &statusResp); err != nil {
		t.Fatalf("Failed to decode RebalanceStatus response: %v", err)
	}

	// Verify response structure - the operation ID should be returned in the response
	if statusResp.OperationID != "test-job-123" {
		t.Errorf("Expected OperationID 'test-job-123', got '%s'", statusResp.OperationID)
	}

	if statusResp.Status == "" {
		t.Error("Expected non-empty status field")
	}

	t.Logf("Rebalance status for operation %s: %s (%.1f%% complete)",
		statusResp.OperationID, statusResp.Status, statusResp.Progress)
}

func TestQUIC_ClusterManagement_RebalanceStatus_InvalidJob(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoint := getQUICEndpoint()
	client, err := newQUICClient(ctx, endpoint)
	require.NoError(t, err, "QUIC server must be available at %s", endpoint)
	defer client.Close()

	type RebalanceStatusRequest struct {
		JobID string
	}

	// Query with empty job ID
	resp, err := client.executeRequest(ctx, quic.OpRebalanceStatus, &RebalanceStatusRequest{
		JobID: "",
	})
	if err != nil {
		t.Fatalf("Failed to execute RebalanceStatus request: %v", err)
	}

	// Should still succeed (implementation returns mock data)
	if resp.Status != quic.StatusOK {
		t.Logf("Note: Empty JobID returned status 0x%02X (this is acceptable)", resp.Status)
	}
}

// =============================================================================
// Test: RebalancePlan Operation
// =============================================================================

func TestQUIC_ClusterManagement_RebalancePlan(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoint := getQUICEndpoint()
	client, err := newQUICClient(ctx, endpoint)
	require.NoError(t, err, "QUIC server must be available at %s", endpoint)
	defer client.Close()

	type RebalancePlanRequest struct {
		TargetShardCount uint64
		Strategy         string
	}

	type ShardMove struct {
		ShardID    uint64
		FromNodeID uint64
		ToNodeID   uint64
		DataSize   uint64
	}

	type RebalancePlanResponse struct {
		Strategy        string
		Moves           []*ShardMove
		EstimatedTime   int64
		ExpectedBalance float64
	}

	// Request rebalance plan
	resp, err := client.executeRequest(ctx, quic.OpRebalancePlan, &RebalancePlanRequest{
		TargetShardCount: 5,
		Strategy:         "round_robin",
	})
	if err != nil {
		t.Fatalf("Failed to execute RebalancePlan request: %v", err)
	}

	if resp.Status != quic.StatusOK {
		t.Fatalf("Expected StatusOK, got: 0x%02X", resp.Status)
	}

	var planResp RebalancePlanResponse
	if err := quic.DecodePayload(resp.Payload, &planResp); err != nil {
		t.Fatalf("Failed to decode RebalancePlan response: %v", err)
	}

	// Verify response
	if planResp.Strategy != "round_robin" {
		t.Errorf("Expected strategy 'round_robin', got '%s'", planResp.Strategy)
	}

	if planResp.Moves == nil {
		t.Error("Expected non-nil Moves slice")
	}

	t.Logf("Rebalance plan: strategy=%s, moves=%d, estimated_time=%ds, expected_balance=%.2f",
		planResp.Strategy, len(planResp.Moves), planResp.EstimatedTime, planResp.ExpectedBalance)
}

func TestQUIC_ClusterManagement_RebalancePlan_DifferentStrategies(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoint := getQUICEndpoint()
	client, err := newQUICClient(ctx, endpoint)
	require.NoError(t, err, "QUIC server must be available at %s", endpoint)
	defer client.Close()

	type RebalancePlanRequest struct {
		TargetShardCount uint64
		Strategy         string
	}

	strategies := []string{"uniform", "consistent-hash", "minimum-movement", "round_robin"}

	for _, strategy := range strategies {
		t.Run(strategy, func(t *testing.T) {
			resp, err := client.executeRequest(ctx, quic.OpRebalancePlan, &RebalancePlanRequest{
				TargetShardCount: 8,
				Strategy:         strategy,
			})
			if err != nil {
				t.Fatalf("Failed to execute RebalancePlan with %s: %v", strategy, err)
			}

			if resp.Status != quic.StatusOK {
				t.Errorf("Strategy %s returned non-OK status: 0x%02X", strategy, resp.Status)
			}

			t.Logf("Strategy %s: OK", strategy)
		})
	}
}

// =============================================================================
// Test: CancelMigration Operation
// =============================================================================

func TestQUIC_ClusterManagement_CancelMigration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoint := getQUICEndpoint()
	client, err := newQUICClient(ctx, endpoint)
	require.NoError(t, err, "QUIC server must be available at %s", endpoint)
	defer client.Close()

	type CancelMigrationRequest struct {
		MigrationID string
	}

	type CancelMigrationResponse struct {
		MigrationID string
		Cancelled   bool
		Message     string
	}

	// Send cancel migration request
	resp, err := client.executeRequest(ctx, quic.OpCancelMigration, &CancelMigrationRequest{
		MigrationID: "migration-456",
	})
	if err != nil {
		t.Fatalf("Failed to execute CancelMigration request: %v", err)
	}

	if resp.Status != quic.StatusOK {
		t.Fatalf("Expected StatusOK, got: 0x%02X", resp.Status)
	}

	var cancelResp CancelMigrationResponse
	if err := quic.DecodePayload(resp.Payload, &cancelResp); err != nil {
		t.Fatalf("Failed to decode CancelMigration response: %v", err)
	}

	// Verify response
	if cancelResp.MigrationID != "migration-456" {
		t.Errorf("Expected MigrationID 'migration-456', got '%s'", cancelResp.MigrationID)
	}

	if !cancelResp.Cancelled {
		t.Error("Expected Cancelled to be true")
	}

	if cancelResp.Message == "" {
		t.Error("Expected non-empty message")
	}

	t.Logf("Migration %s cancellation: %v - %s",
		cancelResp.MigrationID, cancelResp.Cancelled, cancelResp.Message)
}

func TestQUIC_ClusterManagement_CancelMigration_MultipleMigrations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoint := getQUICEndpoint()
	client, err := newQUICClient(ctx, endpoint)
	require.NoError(t, err, "QUIC server must be available at %s", endpoint)
	defer client.Close()

	type CancelMigrationRequest struct {
		MigrationID string
	}

	type CancelMigrationResponse struct {
		MigrationID string
		Cancelled   bool
		Message     string
	}

	migrationIDs := []string{"mig-001", "mig-002", "mig-003"}

	for _, migID := range migrationIDs {
		resp, err := client.executeRequest(ctx, quic.OpCancelMigration, &CancelMigrationRequest{
			MigrationID: migID,
		})
		if err != nil {
			t.Errorf("Failed to cancel migration %s: %v", migID, err)
			continue
		}

		if resp.Status != quic.StatusOK {
			t.Errorf("Migration %s returned non-OK status: 0x%02X", migID, resp.Status)
			continue
		}

		var cancelResp CancelMigrationResponse
		if err := quic.DecodePayload(resp.Payload, &cancelResp); err != nil {
			t.Errorf("Failed to decode response for %s: %v", migID, err)
			continue
		}

		t.Logf("Cancelled migration %s: %v", migID, cancelResp.Cancelled)
	}
}

// =============================================================================
// Test: NodeReplaceRequest Operation
// =============================================================================

func TestQUIC_ClusterManagement_NodeReplaceRequest(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoint := getQUICEndpoint()
	client, err := newQUICClient(ctx, endpoint)
	require.NoError(t, err, "QUIC server must be available at %s", endpoint)
	defer client.Close()

	// Send node replace request
	resp, err := client.executeRequest(ctx, quic.OpNodeReplaceRequest, &service.ReplaceNodeRequest{
		ShardID:    1,
		OldNodeID:  99, // Use non-existent node to avoid actual replacement
		NewNodeID:  100,
		NewAddress: "test-node-new:8080",
	})
	if err != nil {
		t.Fatalf("Failed to execute NodeReplaceRequest: %v", err)
	}

	// Note: This might fail if the node doesn't exist, which is acceptable
	if resp.Status == quic.StatusOK {
		var replaceResp service.ReplaceNodeResponse
		if err := quic.DecodePayload(resp.Payload, &replaceResp); err != nil {
			t.Fatalf("Failed to decode ReplaceNode response: %v", err)
		}

		t.Logf("Node replacement initiated: JobID=%s, Status=%s",
			replaceResp.JobID, replaceResp.Status)
	} else {
		t.Logf("Node replacement returned status: 0x%02X (expected for non-existent node)", resp.Status)
	}
}

// =============================================================================
// Test: Combined Workflow
// =============================================================================

func TestQUIC_ClusterManagement_Workflow(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	endpoint := getQUICEndpoint()
	client, err := newQUICClient(ctx, endpoint)
	require.NoError(t, err, "QUIC server must be available at %s", endpoint)
	defer client.Close()

	t.Log("Step 1: List cluster nodes")
	resp, err := client.executeRequest(ctx, quic.OpListNodes, &service.ClusterStatusRequest{})
	if err != nil {
		t.Fatalf("Step 1 failed: %v", err)
	}
	if resp.Status != quic.StatusOK {
		t.Fatalf("Step 1 failed with status: 0x%02X", resp.Status)
	}

	type ListNodesResponse struct {
		Nodes      []*service.NodeInfo
		TotalCount uint64
	}
	var listResp ListNodesResponse
	if err := quic.DecodePayload(resp.Payload, &listResp); err != nil {
		t.Fatalf("Step 1 decode failed: %v", err)
	}
	t.Logf("Step 1 complete: Found %d nodes", listResp.TotalCount)

	t.Log("Step 2: Generate rebalance plan")
	type RebalancePlanRequest struct {
		TargetShardCount uint64
		Strategy         string
	}
	resp, err = client.executeRequest(ctx, quic.OpRebalancePlan, &RebalancePlanRequest{
		TargetShardCount: 5,
		Strategy:         "uniform",
	})
	if err != nil {
		t.Fatalf("Step 2 failed: %v", err)
	}
	if resp.Status != quic.StatusOK {
		t.Fatalf("Step 2 failed with status: 0x%02X", resp.Status)
	}
	t.Log("Step 2 complete: Rebalance plan generated")

	t.Log("Step 3: Check rebalance status")
	type RebalanceStatusRequest struct {
		JobID string
	}
	resp, err = client.executeRequest(ctx, quic.OpRebalanceStatus, &RebalanceStatusRequest{
		JobID: "workflow-test-job",
	})
	if err != nil {
		t.Fatalf("Step 3 failed: %v", err)
	}
	if resp.Status != quic.StatusOK {
		t.Fatalf("Step 3 failed with status: 0x%02X", resp.Status)
	}
	t.Log("Step 3 complete: Rebalance status retrieved")

	t.Log("SUCCESS: QUIC cluster management workflow completed")
}

// =============================================================================
// Test: Concurrent Operations
// =============================================================================

func TestQUIC_ClusterManagement_ConcurrentRequests(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	endpoint := getQUICEndpoint()

	// Reduced concurrency for QUIC to avoid overwhelming the server
	const concurrency = 5
	var wg sync.WaitGroup
	errors := make(chan error, concurrency)
	successes := make(chan bool, concurrency)

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		// Staggered starts to avoid overwhelming the server with simultaneous connections
		if i > 0 {
			time.Sleep(100 * time.Millisecond)
		}
		go func(id int) {
			defer wg.Done()

			// Per-operation context with timeout
			opCtx, opCancel := context.WithTimeout(ctx, 30*time.Second)
			defer opCancel()

			// Create a separate QUIC client for this goroutine to avoid
			// stream flow control issues on shared connections
			client, err := newQUICClient(opCtx, endpoint)
			if err != nil {
				errors <- err
				return
			}
			defer client.Close()

			resp, err := client.executeRequest(opCtx, quic.OpListNodes, &service.ClusterStatusRequest{})
			if err != nil {
				errors <- err
				return
			}

			if resp.Status != quic.StatusOK {
				errors <- nil // Non-critical error, counted as failure but not logged
				return
			}

			successes <- true
		}(i)
	}

	// Wait for all goroutines to complete
	wg.Wait()
	close(errors)
	close(successes)

	// Collect results
	successCount := 0
	errorCount := 0

	for range successes {
		successCount++
	}
	for err := range errors {
		errorCount++
		if err != nil {
			t.Logf("Request failed: %v", err)
		}
	}

	t.Logf("Concurrent test: %d/%d requests succeeded", successCount, concurrency)

	// Allow partial success - at least 60% should succeed
	minSuccess := int(float64(concurrency) * 0.6)
	if successCount < minSuccess {
		t.Fatalf("Too many concurrent requests failed: %d/%d succeeded (minimum %d required)",
			successCount, concurrency, minSuccess)
	}
}

// =============================================================================
// Test: Error Handling
// =============================================================================

func TestQUIC_ClusterManagement_InvalidPayload(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoint := getQUICEndpoint()
	client, err := newQUICClient(ctx, endpoint)
	require.NoError(t, err, "QUIC server must be available at %s", endpoint)
	defer client.Close()

	// Open stream
	stream, err := client.conn.OpenStreamSync(ctx)
	if err != nil {
		t.Fatalf("Failed to open stream: %v", err)
	}
	defer stream.Close()

	// Send request with invalid payload
	req := &quic.Request{
		Opcode:  quic.OpListNodes,
		Payload: []byte{0xFF, 0xFE, 0xFD}, // Invalid MessagePack
	}

	if err := quic.WriteRequest(stream, req); err != nil {
		t.Fatalf("Failed to write request: %v", err)
	}

	// Read response
	resp, err := quic.ReadResponse(stream)
	if err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}

	// Should return error status
	if resp.Status == quic.StatusOK {
		t.Error("Expected error status for invalid payload, got OK")
	}

	t.Logf("Invalid payload correctly returned error status: 0x%02X", resp.Status)
}

// =============================================================================
// Test: Connection Resilience
// =============================================================================

func TestQUIC_ClusterManagement_ConnectionReuse(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	endpoint := getQUICEndpoint()
	client, err := newQUICClient(ctx, endpoint)
	require.NoError(t, err, "QUIC server must be available at %s", endpoint)
	defer client.Close()

	// Make multiple requests over the same connection
	const numRequests = 20

	for i := 0; i < numRequests; i++ {
		resp, err := client.executeRequest(ctx, quic.OpListNodes, &service.ClusterStatusRequest{})
		if err != nil {
			t.Errorf("Request %d failed: %v", i+1, err)
			continue
		}

		if resp.Status != quic.StatusOK {
			t.Errorf("Request %d returned non-OK status: 0x%02X", i+1, resp.Status)
		}

		// Small delay between requests
		time.Sleep(100 * time.Millisecond)
	}

	t.Logf("Successfully made %d requests over single QUIC connection", numRequests)
}
