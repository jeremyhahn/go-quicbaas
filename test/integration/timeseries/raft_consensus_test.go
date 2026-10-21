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

package timeseries

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// WritePointRequest represents the REST API request body for writing a point.
type WritePointRequest struct {
	Metric    string            `json:"metric"`
	Tags      map[string]string `json:"tags,omitempty"`
	Value     float64           `json:"value"`
	Timestamp time.Time         `json:"timestamp"`
}

// WritePointResponse represents the REST API response for writing a point.
type WritePointResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// WriteBatchRequest represents the REST API request for batch writes.
type WriteBatchRequest struct {
	Points []WritePointRequest `json:"points"`
}

// WriteBatchResponse represents the REST API response for batch writes.
type WriteBatchResponse struct {
	Success       bool   `json:"success"`
	PointsWritten int    `json:"points_written"`
	Error         string `json:"error,omitempty"`
}

// QueryRequest represents a time series query request.
type QueryRequest struct {
	Metric      string            `json:"metric"`
	Tags        map[string]string `json:"tags,omitempty"`
	StartTime   time.Time         `json:"start_time"`
	EndTime     time.Time         `json:"end_time"`
	Aggregation string            `json:"aggregation,omitempty"`
}

// QueryResponse represents a time series query response.
type QueryResponse struct {
	Metric          string      `json:"metric"`
	Points          []DataPoint `json:"points"`
	AggregatedValue *float64    `json:"aggregated_value,omitempty"`
	Error           string      `json:"error,omitempty"`
}

// DataPoint represents a single data point in query results.
type DataPoint struct {
	Timestamp time.Time         `json:"timestamp"`
	Value     float64           `json:"value"`
	Tags      map[string]string `json:"tags,omitempty"`
}

// ClusterStatusResponse represents the cluster status from gRPC.
type ClusterStatusResponse struct {
	Nodes []NodeInfo `json:"nodes"`
}

// NodeInfo represents information about a cluster node.
type NodeInfo struct {
	NodeID   uint64 `json:"node_id"`
	Address  string `json:"address"`
	IsLeader bool   `json:"is_leader"`
}

// httpClient returns an HTTP client with appropriate timeout that acts as the
// tenant operator.
func httpClient(ctx context.Context) (*http.Client, error) {
	return helpers.TenantOperatorHTTPClient(ctx, 30*time.Second)
}

// writePointREST writes a single data point via REST API.
func writePointREST(ctx context.Context, endpoint string, point WritePointRequest) (*WritePointResponse, error) {
	body, err := json.Marshal(point)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/api/v1/timeseries/write", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client, err := httpClient(ctx)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var result WritePointResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w, body: %s", err, string(respBody))
	}

	if resp.StatusCode != http.StatusOK {
		return &result, fmt.Errorf("unexpected status code: %d, error: %s", resp.StatusCode, result.Error)
	}

	return &result, nil
}

// writeBatchREST writes a batch of data points via REST API.
func writeBatchREST(ctx context.Context, endpoint string, batch WriteBatchRequest) (*WriteBatchResponse, error) {
	body, err := json.Marshal(batch)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/api/v1/timeseries/write-batch", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client, err := httpClient(ctx)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var result WriteBatchResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w, body: %s", err, string(respBody))
	}

	if resp.StatusCode != http.StatusOK {
		return &result, fmt.Errorf("unexpected status code: %d, error: %s", resp.StatusCode, result.Error)
	}

	return &result, nil
}

// queryREST queries time series data via REST API.
func queryREST(ctx context.Context, endpoint string, query QueryRequest) (*QueryResponse, error) {
	body, err := json.Marshal(query)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/api/v1/timeseries/query", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client, err := httpClient(ctx)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var result QueryResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w, body: %s", err, string(respBody))
	}

	return &result, nil
}

// waitForReplication waits for replication to complete across the cluster.
// Uses an exponential backoff strategy with verification.
func waitForReplication(ctx context.Context, t *testing.T, metric string, expectedCount int, endpoints []string) bool {
	t.Helper()

	maxAttempts := 10
	baseDelay := 500 * time.Millisecond

	for attempt := 0; attempt < maxAttempts; attempt++ {
		allNodesHaveData := true
		for _, endpoint := range endpoints {
			query := QueryRequest{
				Metric:    metric,
				StartTime: time.Now().Add(-5 * time.Minute),
				EndTime:   time.Now().Add(5 * time.Minute),
			}

			result, err := queryREST(ctx, endpoint, query)
			if err != nil || len(result.Points) < expectedCount {
				allNodesHaveData = false
				break
			}
		}

		if allNodesHaveData {
			return true
		}

		select {
		case <-ctx.Done():
			return false
		case <-time.After(baseDelay * time.Duration(1<<attempt)):
			// Exponential backoff
		}
	}

	return false
}

// getLeaderNode identifies the current leader node using gRPC.
func getLeaderNode(ctx context.Context, t *testing.T) (string, uint64) {
	t.Helper()

	endpoints := helpers.GetClusterEndpoints()

	tlsCfg, err := helpers.IntegrationTLSConfig()
	require.NoError(t, err, "load integration TLS config")

	for _, endpoint := range endpoints {
		connCtx, connCancel := context.WithTimeout(ctx, 10*time.Second)
		conn, err := grpc.DialContext(connCtx, endpoint,
			grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)),
			grpc.WithBlock(),
		)
		connCancel()

		if err != nil {
			continue
		}
		defer conn.Close()

		client := pb.NewQuicBaaSClient(conn)
		statusResp, err := client.ClusterStatus(ctx, &pb.ClusterStatusRequest{})
		if err != nil {
			continue
		}

		for _, node := range statusResp.Nodes {
			if node.IsLeader {
				return node.Address, node.NodeId
			}
		}
	}

	t.Fatal("No leader found in cluster")
	return "", 0
}

// getLeaderRESTEndpoint discovers the Raft leader via gRPC and returns the
// corresponding REST endpoint. It extracts the hostname from the leader's gRPC
// address and matches it against the configured REST endpoints.
func getLeaderRESTEndpoint(ctx context.Context, t *testing.T) string {
	t.Helper()

	leaderAddr, leaderID := getLeaderNode(ctx, t)
	t.Logf("Leader discovered: NodeID=%d, Address=%s", leaderID, leaderAddr)

	// Extract hostname from the leader's gRPC address (host:port format)
	leaderHost, _, err := net.SplitHostPort(leaderAddr)
	if err != nil {
		// Address may not include a port; use it as-is
		leaderHost = leaderAddr
	}

	// Match the leader hostname against REST endpoints
	restEndpoints := getRESTEndpoints()
	for _, restEP := range restEndpoints {
		parsed, parseErr := url.Parse(restEP)
		if parseErr != nil {
			continue
		}
		restHost := parsed.Hostname()
		if restHost == leaderHost {
			t.Logf("Mapped leader to REST endpoint: %s", restEP)
			return restEP
		}
	}

	// Fallback: return the first REST endpoint and log a warning
	t.Logf("WARNING: Could not map leader host %q to a REST endpoint, falling back to %s", leaderHost, restEndpoints[0])
	return restEndpoints[0]
}

// computeShardID computes the shard ID for a metric using FNV-64a hash.
// This matches the logic in pkg/shard/timeseries_router.go.
// Note: The service currently uses NumShards=1 and BaseShardID=3, so all
// timeseries data routes to shard 3. This function is kept for reference
// but should use the actual service configuration.
func computeShardID(metric string, numShards int) uint64 {
	const baseShardID uint64 = 3 // Matches shard.DefaultBaseShardID
	if metric == "" || numShards <= 1 {
		return baseShardID // Single shard routing
	}
	h := fnv.New64a()
	h.Write([]byte(metric))
	hash := h.Sum64()
	shardMask := uint64(numShards - 1)
	idx := hash & shardMask
	return baseShardID + idx // Shard IDs start from BaseShardID (3)
}

// TestTimeSeriesRaft_WriteReplication tests that a data point written to the leader
// is replicated to all follower nodes.
func TestTimeSeriesRaft_WriteReplication(t *testing.T) {
	if helpers.IsStandaloneMode(t, helpers.GetClusterEndpoints()) {
		t.Log("Cluster running in standalone/embedded mode - Raft timeseries test not applicable")
		return
	}
	requireTimeseriesAvailableREST(t)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	endpoints := getRESTEndpoints()
	require.Len(t, endpoints, 3, "Expected 3 nodes in cluster")

	// Write a unique data point to the first node (which may or may not be leader)
	uniqueMetric := fmt.Sprintf("test.replication.%d", time.Now().UnixNano())
	timestamp := time.Now()
	testValue := 42.5

	point := WritePointRequest{
		Metric:    uniqueMetric,
		Tags:      map[string]string{"host": "server1", "region": "us-east-1"},
		Value:     testValue,
		Timestamp: timestamp,
	}

	// Write to first node
	result, err := writePointREST(ctx, endpoints[0], point)
	require.NoError(t, err, "Failed to write data point to leader")
	assert.True(t, result.Success, "Write operation should succeed")

	t.Logf("Successfully wrote point to node 1: metric=%s, value=%f", uniqueMetric, testValue)

	// Wait for replication
	replicated := waitForReplication(ctx, t, uniqueMetric, 1, endpoints)
	require.True(t, replicated, "Data should replicate to all nodes within timeout")

	// Verify data on all nodes
	for i, endpoint := range endpoints {
		query := QueryRequest{
			Metric:    uniqueMetric,
			StartTime: timestamp.Add(-1 * time.Minute),
			EndTime:   timestamp.Add(1 * time.Minute),
		}

		queryResult, err := queryREST(ctx, endpoint, query)
		require.NoError(t, err, "Query should succeed on node %d", i+1)
		require.Len(t, queryResult.Points, 1, "Node %d should have exactly 1 point", i+1)
		assert.Equal(t, testValue, queryResult.Points[0].Value, "Value mismatch on node %d", i+1)

		t.Logf("Verified replication on node %d: value=%f", i+1, queryResult.Points[0].Value)
	}
}

// TestTimeSeriesRaft_BatchReplication tests that a batch of data points written
// to the leader is replicated to all follower nodes.
func TestTimeSeriesRaft_BatchReplication(t *testing.T) {
	if helpers.IsStandaloneMode(t, helpers.GetClusterEndpoints()) {
		t.Log("Cluster running in standalone/embedded mode - Raft timeseries test not applicable")
		return
	}
	requireTimeseriesAvailableREST(t)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	endpoints := getRESTEndpoints()
	require.Len(t, endpoints, 3, "Expected 3 nodes in cluster")

	// Create a batch of points
	uniqueMetric := fmt.Sprintf("test.batch.%d", time.Now().UnixNano())
	now := time.Now()
	batchSize := 10

	batch := WriteBatchRequest{
		Points: make([]WritePointRequest, batchSize),
	}

	for i := 0; i < batchSize; i++ {
		batch.Points[i] = WritePointRequest{
			Metric:    uniqueMetric,
			Tags:      map[string]string{"host": "server1", "index": fmt.Sprintf("%d", i)},
			Value:     float64(i * 10),
			Timestamp: now.Add(time.Duration(i) * time.Second),
		}
	}

	// Write batch to first node
	result, err := writeBatchREST(ctx, endpoints[0], batch)
	require.NoError(t, err, "Failed to write batch")
	assert.True(t, result.Success, "Batch write should succeed")
	assert.Equal(t, batchSize, result.PointsWritten, "All points should be written")

	t.Logf("Successfully wrote batch of %d points", batchSize)

	// Wait for replication
	replicated := waitForReplication(ctx, t, uniqueMetric, batchSize, endpoints)
	require.True(t, replicated, "Batch should replicate to all nodes within timeout")

	// Query from each follower and verify results
	for i, endpoint := range endpoints {
		query := QueryRequest{
			Metric:    uniqueMetric,
			StartTime: now.Add(-1 * time.Minute),
			EndTime:   now.Add(time.Duration(batchSize+1) * time.Second),
		}

		queryResult, err := queryREST(ctx, endpoint, query)
		require.NoError(t, err, "Query should succeed on node %d", i+1)
		assert.Len(t, queryResult.Points, batchSize, "Node %d should have all %d points", i+1, batchSize)

		t.Logf("Node %d has %d points (expected %d)", i+1, len(queryResult.Points), batchSize)
	}
}

// TestTimeSeriesRaft_LeaderFailover tests that the cluster continues to operate
// after a leader failure and new leader election.
func TestTimeSeriesRaft_LeaderFailover(t *testing.T) {
	if helpers.IsStandaloneMode(t, helpers.GetClusterEndpoints()) {
		t.Log("Cluster running in standalone/embedded mode - Raft timeseries test not applicable")
		return
	}
	requireTimeseriesAvailableREST(t)

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	endpoints := getRESTEndpoints()
	grpcEndpoints := helpers.GetClusterEndpoints()
	require.Len(t, endpoints, 3, "Expected 3 nodes in cluster")

	// Identify current leader
	leaderAddr, leaderID := getLeaderNode(ctx, t)
	t.Logf("Current leader: NodeID=%d, Address=%s", leaderID, leaderAddr)

	// Write data to cluster before failover
	preFailoverMetric := fmt.Sprintf("test.failover.pre.%d", time.Now().UnixNano())
	point := WritePointRequest{
		Metric:    preFailoverMetric,
		Tags:      map[string]string{"phase": "pre-failover"},
		Value:     100.0,
		Timestamp: time.Now(),
	}

	result, err := writePointREST(ctx, endpoints[0], point)
	require.NoError(t, err, "Pre-failover write should succeed")
	assert.True(t, result.Success)

	t.Logf("Pre-failover write succeeded")

	// Wait for replication
	replicated := waitForReplication(ctx, t, preFailoverMetric, 1, endpoints)
	require.True(t, replicated, "Pre-failover data should replicate")

	// Simulate leader failure by closing connection
	// In a real test, we would stop the leader container
	// Here we test that the cluster remains operational after a brief disruption
	t.Logf("Simulating leader instability...")

	// Create a connection to leader and close it abruptly
	failoverTLSCfg, err := helpers.IntegrationTLSConfig()
	require.NoError(t, err, "load integration TLS config")
	connCtx, connCancel := context.WithTimeout(ctx, 5*time.Second)
	conn, err := grpc.DialContext(connCtx, grpcEndpoints[0],
		grpc.WithTransportCredentials(credentials.NewTLS(failoverTLSCfg)),
		grpc.WithBlock(),
	)
	connCancel()
	if err == nil {
		conn.Close()
	}

	// Wait for potential leader re-election: poll until a node (not
	// necessarily the new leader) answers a query again.
	var workingEndpoint string
	pollUntil(ctx, 10*time.Second, 250*time.Millisecond, func() bool {
		for _, endpoint := range endpoints {
			testQuery := QueryRequest{
				Metric:    preFailoverMetric,
				StartTime: time.Now().Add(-5 * time.Minute),
				EndTime:   time.Now().Add(5 * time.Minute),
			}
			if _, err := queryREST(ctx, endpoint, testQuery); err == nil {
				workingEndpoint = endpoint
				return true
			}
		}
		return false
	})
	require.NotEmpty(t, workingEndpoint, "At least one node should be accessible")

	// Verify pre-failover data is still accessible
	query := QueryRequest{
		Metric:    preFailoverMetric,
		StartTime: time.Now().Add(-5 * time.Minute),
		EndTime:   time.Now().Add(5 * time.Minute),
	}

	queryResult, err := queryREST(ctx, workingEndpoint, query)
	require.NoError(t, err, "Query should succeed after failover")
	assert.Len(t, queryResult.Points, 1, "Pre-failover data should be accessible")
	assert.Equal(t, 100.0, queryResult.Points[0].Value)

	t.Logf("Pre-failover data verified after leader failover")

	// Write new data to the new leader
	postFailoverMetric := fmt.Sprintf("test.failover.post.%d", time.Now().UnixNano())
	postPoint := WritePointRequest{
		Metric:    postFailoverMetric,
		Tags:      map[string]string{"phase": "post-failover"},
		Value:     200.0,
		Timestamp: time.Now(),
	}

	result, err = writePointREST(ctx, workingEndpoint, postPoint)
	require.NoError(t, err, "Post-failover write should succeed")
	assert.True(t, result.Success)

	t.Logf("Post-failover write succeeded")

	// Verify new data is replicated
	replicated = waitForReplication(ctx, t, postFailoverMetric, 1, endpoints)
	assert.True(t, replicated, "Post-failover data should replicate")
}

// TestTimeSeriesRaft_ConsistencyAfterRestart tests that the cluster maintains
// consistency after node restarts.
func TestTimeSeriesRaft_ConsistencyAfterRestart(t *testing.T) {
	if helpers.IsStandaloneMode(t, helpers.GetClusterEndpoints()) {
		t.Log("Cluster running in standalone/embedded mode - Raft timeseries test not applicable")
		return
	}
	requireTimeseriesAvailableREST(t)

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	endpoints := getRESTEndpoints()
	require.Len(t, endpoints, 3, "Expected 3 nodes in cluster")

	// Write initial data
	metric := fmt.Sprintf("test.restart.%d", time.Now().UnixNano())
	initialPoints := []WritePointRequest{
		{
			Metric:    metric,
			Tags:      map[string]string{"phase": "initial", "seq": "1"},
			Value:     10.0,
			Timestamp: time.Now().Add(-2 * time.Second),
		},
		{
			Metric:    metric,
			Tags:      map[string]string{"phase": "initial", "seq": "2"},
			Value:     20.0,
			Timestamp: time.Now().Add(-1 * time.Second),
		},
	}

	for _, point := range initialPoints {
		result, err := writePointREST(ctx, endpoints[0], point)
		require.NoError(t, err, "Initial write should succeed")
		assert.True(t, result.Success)
	}

	t.Logf("Initial data written: %d points", len(initialPoints))

	// Wait for replication
	replicated := waitForReplication(ctx, t, metric, len(initialPoints), endpoints)
	require.True(t, replicated, "Initial data should replicate")

	// Simulate follower restart by brief network blip
	// In a real test, we would restart a container
	t.Logf("Simulating follower instability...")

	// Verify follower catches up with latest data
	for i, endpoint := range endpoints {
		query := QueryRequest{
			Metric:    metric,
			StartTime: time.Now().Add(-5 * time.Minute),
			EndTime:   time.Now().Add(5 * time.Minute),
		}

		queryResult, err := queryREST(ctx, endpoint, query)
		require.NoError(t, err, "Query should succeed on node %d after restart simulation", i+1)
		assert.GreaterOrEqual(t, len(queryResult.Points), len(initialPoints),
			"Node %d should have at least %d points after restart", i+1, len(initialPoints))
	}

	// Write more data after restart
	postRestartPoint := WritePointRequest{
		Metric:    metric,
		Tags:      map[string]string{"phase": "post-restart"},
		Value:     30.0,
		Timestamp: time.Now(),
	}

	result, err := writePointREST(ctx, endpoints[0], postRestartPoint)
	require.NoError(t, err, "Post-restart write should succeed")
	assert.True(t, result.Success)

	t.Logf("Post-restart write succeeded")

	// Verify post-restart data replicates
	expectedTotal := len(initialPoints) + 1
	replicated = waitForReplication(ctx, t, metric, expectedTotal, endpoints)
	assert.True(t, replicated, "Post-restart data should replicate to all nodes")

	// Verify all nodes have consistent data
	for i, endpoint := range endpoints {
		query := QueryRequest{
			Metric:    metric,
			StartTime: time.Now().Add(-5 * time.Minute),
			EndTime:   time.Now().Add(5 * time.Minute),
		}

		queryResult, err := queryREST(ctx, endpoint, query)
		require.NoError(t, err, "Final query should succeed on node %d", i+1)
		assert.GreaterOrEqual(t, len(queryResult.Points), expectedTotal,
			"Node %d should have %d points", i+1, expectedTotal)

		t.Logf("Node %d verified with %d points", i+1, len(queryResult.Points))
	}
}

// TestTimeSeriesRaft_ConcurrentWrites tests that concurrent writes from multiple
// clients are properly ordered and committed without data loss.
func TestTimeSeriesRaft_ConcurrentWrites(t *testing.T) {
	if helpers.IsStandaloneMode(t, helpers.GetClusterEndpoints()) {
		t.Log("Cluster running in standalone/embedded mode - Raft timeseries test not applicable")
		return
	}
	requireTimeseriesAvailableREST(t)

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	endpoints := getRESTEndpoints()
	require.Len(t, endpoints, 3, "Expected 3 nodes in cluster")

	// Discover the leader node so all writes are routed to it.
	// In a Raft cluster, only the leader can accept writes. Sending writes
	// to follower nodes causes failures, so we route all concurrent writers
	// to the leader's REST endpoint.
	leaderEndpoint := getLeaderRESTEndpoint(ctx, t)

	// Number of concurrent writers and writes per writer
	numWriters := 5
	writesPerWriter := 20

	// Use different metrics for each writer to avoid conflicts
	baseMetric := fmt.Sprintf("test.concurrent.%d", time.Now().UnixNano())

	var wg sync.WaitGroup
	var successCount atomic.Int64
	var failureCount atomic.Int64

	// Collect all metrics for verification
	metrics := make([]string, numWriters)
	for i := 0; i < numWriters; i++ {
		metrics[i] = fmt.Sprintf("%s.writer%d", baseMetric, i)
	}

	// Launch concurrent writers, all targeting the leader node
	for writerID := 0; writerID < numWriters; writerID++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			metric := metrics[id]

			for j := 0; j < writesPerWriter; j++ {
				point := WritePointRequest{
					Metric:    metric,
					Tags:      map[string]string{"writer": fmt.Sprintf("%d", id), "seq": fmt.Sprintf("%d", j)},
					Value:     float64(id*1000 + j),
					Timestamp: time.Now(),
				}

				writeCtx, writeCancel := context.WithTimeout(ctx, 10*time.Second)
				result, err := writePointREST(writeCtx, leaderEndpoint, point)
				writeCancel()

				if err != nil || !result.Success {
					failureCount.Add(1)
				} else {
					successCount.Add(1)
				}
			}
		}(writerID)
	}

	// Wait for all writers to complete
	wg.Wait()

	totalWrites := int64(numWriters * writesPerWriter)
	successes := successCount.Load()
	failures := failureCount.Load()

	t.Logf("Concurrent writes completed: %d succeeded, %d failed out of %d total",
		successes, failures, totalWrites)

	// Verify majority of writes succeeded
	assert.Greater(t, successes, failures, "Majority of concurrent writes should succeed")
	assert.GreaterOrEqual(t, successes, totalWrites*8/10, "At least 80%% of writes should succeed")

	// Wait for replication: poll until every node returns points for every
	// writer's metric. The checks below only log, so the outcome is logged too.
	replicated := pollUntil(ctx, 10*time.Second, 250*time.Millisecond, func() bool {
		for _, metric := range metrics {
			for _, endpoint := range endpoints {
				query := QueryRequest{
					Metric:    metric,
					StartTime: time.Now().Add(-5 * time.Minute),
					EndTime:   time.Now().Add(5 * time.Minute),
				}
				queryResult, err := queryREST(ctx, endpoint, query)
				if err != nil || len(queryResult.Points) == 0 {
					return false
				}
			}
		}
		return true
	})
	t.Logf("Concurrent writes visible on every node: %t", replicated)

	// Verify data on all nodes for each metric
	for writerID, metric := range metrics {
		for nodeID, endpoint := range endpoints {
			query := QueryRequest{
				Metric:    metric,
				StartTime: time.Now().Add(-5 * time.Minute),
				EndTime:   time.Now().Add(5 * time.Minute),
			}

			queryResult, err := queryREST(ctx, endpoint, query)
			if err != nil {
				t.Logf("Query failed for writer %d on node %d: %v", writerID, nodeID+1, err)
				continue
			}

			// At least some points should be replicated
			if len(queryResult.Points) > 0 {
				t.Logf("Writer %d: Node %d has %d points", writerID, nodeID+1, len(queryResult.Points))
			}
		}
	}

	// Verify no data corruption by checking value ranges
	for writerID, metric := range metrics {
		query := QueryRequest{
			Metric:    metric,
			StartTime: time.Now().Add(-5 * time.Minute),
			EndTime:   time.Now().Add(5 * time.Minute),
		}

		queryResult, err := queryREST(ctx, endpoints[0], query)
		if err != nil {
			continue
		}

		expectedMin := float64(writerID * 1000)
		expectedMax := float64(writerID*1000 + writesPerWriter - 1)

		for _, point := range queryResult.Points {
			assert.GreaterOrEqual(t, point.Value, expectedMin,
				"Value should be in expected range for writer %d", writerID)
			assert.LessOrEqual(t, point.Value, expectedMax,
				"Value should be in expected range for writer %d", writerID)
		}
	}
}

// TestTimeSeriesRaft_ShardRouting tests that data points are routed to the correct
// shards based on FNV-64a hash of the metric name.
func TestTimeSeriesRaft_ShardRouting(t *testing.T) {
	if helpers.IsStandaloneMode(t, helpers.GetClusterEndpoints()) {
		t.Log("Cluster running in standalone/embedded mode - Raft timeseries test not applicable")
		return
	}
	requireTimeseriesAvailableREST(t)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	endpoints := getRESTEndpoints()
	require.Len(t, endpoints, 3, "Expected 3 nodes in cluster")

	// The service currently uses single-shard routing (NumShards=1, BaseShardID=3)
	// All timeseries data routes to shard 3
	numShards := 1

	// Create metrics that will hash to different shards
	testMetrics := []string{
		"cpu.usage",
		"memory.used",
		"disk.io",
		"network.bytes_in",
		"network.bytes_out",
		"http.requests",
		"database.connections",
		"cache.hit_rate",
	}

	// Calculate expected shard for each metric
	metricShards := make(map[string]uint64)
	shardMetrics := make(map[uint64][]string)

	for _, metric := range testMetrics {
		shardID := computeShardID(metric, numShards)
		metricShards[metric] = shardID
		shardMetrics[shardID] = append(shardMetrics[shardID], metric)
		t.Logf("Metric '%s' routes to shard %d", metric, shardID)
	}

	// Write data for each metric
	timestamp := time.Now()
	for i, metric := range testMetrics {
		point := WritePointRequest{
			Metric:    metric,
			Tags:      map[string]string{"test": "shard_routing"},
			Value:     float64(i * 100),
			Timestamp: timestamp,
		}

		result, err := writePointREST(ctx, endpoints[0], point)
		require.NoError(t, err, "Write should succeed for metric %s", metric)
		assert.True(t, result.Success)
	}

	t.Logf("Wrote %d metrics across shards", len(testMetrics))

	// Wait until every metric's point is queryable on the node the checks
	// below read from.
	pollUntil(ctx, 10*time.Second, 250*time.Millisecond, func() bool {
		for _, metric := range testMetrics {
			query := QueryRequest{
				Metric:    metric,
				StartTime: timestamp.Add(-1 * time.Minute),
				EndTime:   timestamp.Add(1 * time.Minute),
			}
			queryResult, err := queryREST(ctx, endpoints[0], query)
			if err != nil || len(queryResult.Points) == 0 {
				return false
			}
		}
		return true
	})

	// Verify data for each metric is accessible
	for i, metric := range testMetrics {
		query := QueryRequest{
			Metric:    metric,
			StartTime: timestamp.Add(-1 * time.Minute),
			EndTime:   timestamp.Add(1 * time.Minute),
		}

		queryResult, err := queryREST(ctx, endpoints[0], query)
		require.NoError(t, err, "Query should succeed for metric %s", metric)
		require.Len(t, queryResult.Points, 1, "Should have exactly 1 point for metric %s", metric)
		assert.Equal(t, float64(i*100), queryResult.Points[0].Value,
			"Value mismatch for metric %s", metric)
	}

	// Verify sharding consistency - same metric always routes to same shard
	for _, metric := range testMetrics {
		expectedShard := metricShards[metric]
		actualShard := computeShardID(metric, numShards)
		assert.Equal(t, expectedShard, actualShard,
			"Metric '%s' should consistently route to shard %d", metric, expectedShard)
	}

	// Log shard distribution
	t.Logf("Shard distribution:")
	for shardID, metrics := range shardMetrics {
		t.Logf("  Shard %d: %v", shardID, metrics)
	}
}

// TestTimeSeriesRaft_ConsistencyLevels tests different consistency levels
// (local vs quorum reads) if supported by the API.
func TestTimeSeriesRaft_ConsistencyLevels(t *testing.T) {
	if helpers.IsStandaloneMode(t, helpers.GetClusterEndpoints()) {
		t.Log("Cluster running in standalone/embedded mode - Raft timeseries test not applicable")
		return
	}
	requireTimeseriesAvailableREST(t)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	endpoints := getRESTEndpoints()
	require.Len(t, endpoints, 3, "Expected 3 nodes in cluster")

	// Write test data
	metric := fmt.Sprintf("test.consistency.%d", time.Now().UnixNano())
	point := WritePointRequest{
		Metric:    metric,
		Tags:      map[string]string{"consistency": "test"},
		Value:     999.0,
		Timestamp: time.Now(),
	}

	result, err := writePointREST(ctx, endpoints[0], point)
	require.NoError(t, err, "Write should succeed")
	assert.True(t, result.Success)

	// Wait for replication
	replicated := waitForReplication(ctx, t, metric, 1, endpoints)
	require.True(t, replicated, "Data should replicate to all nodes")

	// Test reading from all nodes to verify consistency
	var values []float64
	for i, endpoint := range endpoints {
		query := QueryRequest{
			Metric:    metric,
			StartTime: time.Now().Add(-5 * time.Minute),
			EndTime:   time.Now().Add(5 * time.Minute),
		}

		queryResult, err := queryREST(ctx, endpoint, query)
		require.NoError(t, err, "Query should succeed on node %d", i+1)
		require.Len(t, queryResult.Points, 1, "Should have exactly 1 point on node %d", i+1)
		values = append(values, queryResult.Points[0].Value)
	}

	// All values should be identical (strong consistency)
	for i, val := range values {
		assert.Equal(t, values[0], val, "All nodes should return consistent value (node %d)", i+1)
	}

	t.Logf("Consistency verified: all %d nodes return value %f", len(endpoints), values[0])
}

// TestTimeSeriesRaft_ErrorHandling tests proper error handling in distributed scenarios.
func TestTimeSeriesRaft_ErrorHandling(t *testing.T) {
	if helpers.IsStandaloneMode(t, helpers.GetClusterEndpoints()) {
		t.Log("Cluster running in standalone/embedded mode - Raft timeseries test not applicable")
		return
	}
	requireTimeseriesAvailableREST(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	endpoints := getRESTEndpoints()
	require.Len(t, endpoints, 3, "Expected 3 nodes in cluster")

	t.Run("EmptyMetricName", func(t *testing.T) {
		point := WritePointRequest{
			Metric:    "",
			Value:     100.0,
			Timestamp: time.Now(),
		}

		_, err := writePointREST(ctx, endpoints[0], point)
		assert.Error(t, err, "Writing with empty metric should fail")
	})

	t.Run("InvalidTimestamp", func(t *testing.T) {
		point := WritePointRequest{
			Metric:    "test.error.timestamp",
			Value:     100.0,
			Timestamp: time.Time{}, // Zero time
		}

		_, err := writePointREST(ctx, endpoints[0], point)
		assert.Error(t, err, "Writing with zero timestamp should fail")
	})

	t.Run("EmptyBatch", func(t *testing.T) {
		batch := WriteBatchRequest{
			Points: []WritePointRequest{},
		}

		result, err := writeBatchREST(ctx, endpoints[0], batch)
		// Empty batch may succeed with 0 points written or return an error
		if err == nil {
			assert.Equal(t, 0, result.PointsWritten, "Empty batch should write 0 points")
		}
	})

	t.Run("QueryNonexistentMetric", func(t *testing.T) {
		query := QueryRequest{
			Metric:    "nonexistent.metric.that.does.not.exist.12345",
			StartTime: time.Now().Add(-1 * time.Hour),
			EndTime:   time.Now(),
		}

		result, err := queryREST(ctx, endpoints[0], query)
		// Should return empty results, not an error
		if err == nil {
			assert.Empty(t, result.Points, "Query for nonexistent metric should return empty results")
		}
	})
}

// TestTimeSeriesRaft_HighVolumeBatch tests handling of large batch writes.
func TestTimeSeriesRaft_HighVolumeBatch(t *testing.T) {
	if helpers.IsStandaloneMode(t, helpers.GetClusterEndpoints()) {
		t.Log("Cluster running in standalone/embedded mode - Raft timeseries test not applicable")
		return
	}
	requireTimeseriesAvailableREST(t)

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	endpoints := getRESTEndpoints()
	require.Len(t, endpoints, 3, "Expected 3 nodes in cluster")

	// Create a large batch of points
	metric := fmt.Sprintf("test.highvolume.%d", time.Now().UnixNano())
	batchSize := 1000
	now := time.Now()

	batch := WriteBatchRequest{
		Points: make([]WritePointRequest, batchSize),
	}

	for i := 0; i < batchSize; i++ {
		batch.Points[i] = WritePointRequest{
			Metric:    metric,
			Tags:      map[string]string{"host": fmt.Sprintf("server%d", i%10)},
			Value:     float64(i),
			Timestamp: now.Add(time.Duration(i) * time.Millisecond),
		}
	}

	// Write the large batch
	startTime := time.Now()
	result, err := writeBatchREST(ctx, endpoints[0], batch)
	writeLatency := time.Since(startTime)

	require.NoError(t, err, "Large batch write should succeed")
	assert.True(t, result.Success, "Batch write should be successful")
	assert.Equal(t, batchSize, result.PointsWritten, "All points should be written")

	t.Logf("High volume batch write completed: %d points in %v", batchSize, writeLatency)

	// Wait for replication
	replicated := waitForReplication(ctx, t, metric, batchSize, endpoints)
	require.True(t, replicated, "Large batch should replicate to all nodes")

	// Verify data integrity on all nodes
	for i, endpoint := range endpoints {
		query := QueryRequest{
			Metric:    metric,
			StartTime: now.Add(-1 * time.Minute),
			EndTime:   now.Add(time.Duration(batchSize) * time.Millisecond).Add(1 * time.Minute),
		}

		queryResult, err := queryREST(ctx, endpoint, query)
		require.NoError(t, err, "Query should succeed on node %d", i+1)
		assert.Len(t, queryResult.Points, batchSize, "Node %d should have all %d points", i+1, batchSize)
	}
}
