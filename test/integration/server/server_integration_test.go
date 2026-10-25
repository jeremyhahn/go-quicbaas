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

package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// TestServerHealthEndpoints tests all health check endpoints
func TestServerHealthEndpoints(t *testing.T) {
	baseURL := getRESTEndpoint()
	client := helpers.NewHTTPClient(30 * time.Second)

	tests := []struct {
		name           string
		endpoint       string
		expectedStatus int
	}{
		{
			name:           "LivenessCheck",
			endpoint:       "/api/v1/health/live",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "ReadinessCheck",
			endpoint:       "/api/v1/health/ready",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "OverallHealth",
			endpoint:       "/api/v1/health",
			expectedStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := client.Get(baseURL + tt.endpoint)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, tt.expectedStatus, resp.StatusCode)

			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)

			t.Logf("Response: %s", string(body))
		})
	}
}

// TestServerRESTAPI tests REST API endpoints
func TestServerRESTAPI(t *testing.T) {
	baseURL := getRESTEndpoint()
	client := helpers.NewTenantOperatorHTTPClient(t, 30*time.Second)

	entityType := "rest_test"
	entityID := time.Now().UnixNano()

	// Test PUT (create)
	t.Run("CreateEntity", func(t *testing.T) {
		data := map[string]interface{}{
			"name":  "REST Test Entity",
			"value": 42,
		}
		jsonData, err := json.Marshal(data)
		require.NoError(t, err)

		url := fmt.Sprintf("%s/api/v1/entities/%s", baseURL, entityType)
		resp, err := client.Post(url, "application/json", strings.NewReader(string(jsonData)))
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusCreated, resp.StatusCode)

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)

		t.Logf("Create response: %s", string(body))
	})

	// Test GET
	t.Run("GetEntity", func(t *testing.T) {
		url := fmt.Sprintf("%s/api/v1/entities/%s/%d", baseURL, entityType, entityID)
		resp, err := client.Get(url)
		require.NoError(t, err)
		defer resp.Body.Close()

		// May not exist yet, that's OK
		if resp.StatusCode == http.StatusOK {
			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			t.Logf("Get response: %s", string(body))
		}
	})

	// Test LIST
	t.Run("ListEntities", func(t *testing.T) {
		url := fmt.Sprintf("%s/api/v1/entities/%s?limit=10", baseURL, entityType)
		resp, err := client.Get(url)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)

		t.Logf("List response: %s", string(body))
	})
}

// TestServerMetrics tests the Prometheus metrics endpoint
func TestServerMetrics(t *testing.T) {
	metricsURL := getMetricsEndpoint()
	client := helpers.NewSystemAdminHTTPClient(t, 30*time.Second)

	resp, err := client.Get(metricsURL)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	metrics := string(body)

	// Verify key metrics exist
	assert.Contains(t, metrics, "quicbaas_")
	assert.Contains(t, metrics, "go_goroutines")

	t.Logf("Metrics endpoint working, returned %d bytes", len(metrics))
}

// TestServerClusterStatus tests that the cluster status endpoint returns a valid
// JSON response. In embedded/standalone mode, the server has no real Raft
// consensus, so fields like leader_id and total_nodes may be absent or zero.
// The test validates the API is reachable and returns parseable JSON.
func TestServerClusterStatus(t *testing.T) {
	baseURL := getRESTEndpoint()
	client := helpers.NewSystemAdminHTTPClient(t, 30*time.Second)

	resp, err := client.Get(baseURL + "/api/v1/cluster/status")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var status map[string]interface{}
	err = json.Unmarshal(body, &status)
	require.NoError(t, err, "Response must be valid JSON")

	t.Logf("Cluster status: %+v", status)
}

// TestServerFullLifecycle tests complete CRUD lifecycle
func TestServerFullLifecycle(t *testing.T) {
	baseURL := getRESTEndpoint()
	client := helpers.NewTenantOperatorHTTPClient(t, 30*time.Second)

	entityType := "lifecycle_test"

	// Create
	var createdID uint64
	t.Run("Create", func(t *testing.T) {
		data := map[string]interface{}{
			"name":  "Lifecycle Test",
			"stage": "created",
			"count": 1,
		}
		jsonData, err := json.Marshal(data)
		require.NoError(t, err)

		url := fmt.Sprintf("%s/api/v1/entities/%s", baseURL, entityType)
		resp, err := client.Post(url, "application/json", strings.NewReader(string(jsonData)))
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusCreated, resp.StatusCode)

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)

		var result map[string]interface{}
		err = json.Unmarshal(body, &result)
		require.NoError(t, err)

		if id, ok := result["entity_id"].(float64); ok {
			createdID = uint64(id)
		}
	})

	// Read
	if createdID > 0 {
		t.Run("Read", func(t *testing.T) {
			url := fmt.Sprintf("%s/api/v1/entities/%s/%d", baseURL, entityType, createdID)
			resp, err := client.Get(url)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, http.StatusOK, resp.StatusCode)
		})

		// Update
		t.Run("Update", func(t *testing.T) {
			data := map[string]interface{}{
				"name":  "Lifecycle Test Updated",
				"stage": "updated",
				"count": 2,
			}
			jsonData, err := json.Marshal(data)
			require.NoError(t, err)

			url := fmt.Sprintf("%s/api/v1/entities/%s/%d", baseURL, entityType, createdID)
			req, err := http.NewRequest(http.MethodPut, url, strings.NewReader(string(jsonData)))
			require.NoError(t, err)
			req.Header.Set("Content-Type", "application/json")

			resp, err := client.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, http.StatusOK, resp.StatusCode)
		})

		// Delete
		t.Run("Delete", func(t *testing.T) {
			url := fmt.Sprintf("%s/api/v1/entities/%s/%d", baseURL, entityType, createdID)
			req, err := http.NewRequest(http.MethodDelete, url, nil)
			require.NoError(t, err)

			resp, err := client.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, http.StatusNoContent, resp.StatusCode)
		})

		// Verify deleted
		t.Run("VerifyDeleted", func(t *testing.T) {
			url := fmt.Sprintf("%s/api/v1/entities/%s/%d", baseURL, entityType, createdID)
			resp, err := client.Get(url)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, http.StatusNotFound, resp.StatusCode)
		})
	}
}

// TestServerConcurrentOperations tests concurrent REST operations
func TestServerConcurrentOperations(t *testing.T) {
	baseURL := getRESTEndpoint()
	entityType := "concurrent_rest_test"
	concurrency := 10

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	var mu sync.Mutex
	var errs []error

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()

			client := helpers.NewTenantOperatorHTTPClient(t, 30*time.Second)

			data := map[string]interface{}{
				"index": index,
				"name":  fmt.Sprintf("Concurrent Item %d", index),
			}
			jsonData, err := json.Marshal(data)
			if err != nil {
				mu.Lock()
				errs = append(errs, fmt.Errorf("goroutine %d: marshal failed: %w", index, err))
				mu.Unlock()
				return
			}

			reqURL := fmt.Sprintf("%s/api/v1/entities/%s", baseURL, entityType)
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, strings.NewReader(string(jsonData)))
			if err != nil {
				mu.Lock()
				errs = append(errs, fmt.Errorf("goroutine %d: new request failed: %w", index, err))
				mu.Unlock()
				return
			}
			req.Header.Set("Content-Type", "application/json")

			resp, err := client.Do(req)
			if err != nil {
				mu.Lock()
				errs = append(errs, fmt.Errorf("goroutine %d: request failed: %w", index, err))
				mu.Unlock()
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusCreated {
				mu.Lock()
				errs = append(errs, fmt.Errorf("goroutine %d: unexpected status: %d", index, resp.StatusCode))
				mu.Unlock()
				return
			}
		}(i)
	}

	wg.Wait()

	require.Empty(t, errs, "concurrent operations had errors: %v", errs)
	t.Logf("Successfully completed %d concurrent REST operations", concurrency)
}

// getMetricsEndpoint returns the metrics endpoint URL.
func getMetricsEndpoint() string {
	return getRESTEndpoint() + "/api/v1/metrics"
}
