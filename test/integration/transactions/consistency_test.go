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
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/api/rest"
	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// TestConsistencyLevels_Put tests different consistency levels for Put operations
func TestConsistencyLevels_Put(t *testing.T) {
	baseURL := getEnv("QUICBAAS_NODE1_URL", "https://test-node1:8080")
	client := helpers.NewHTTPClient(10 * time.Second)

	entityType := "consistency_test"
	entityID := uint64(time.Now().UnixNano())

	tests := []struct {
		name        string
		consistency string
	}{
		{"Strong", "strong"},
		{"Linearizable", "linearizable"},
		{"Eventual", "eventual"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testData := map[string]interface{}{
				"test":        tt.name,
				"timestamp":   time.Now().Unix(),
				"consistency": tt.consistency,
			}

			putReq := rest.PutRequest{
				EntityType: entityType,
				ID:         entityID + uint64(len(tt.name)), // Unique ID per test
				Data:       mustMarshal(t, testData),
			}
			body := mustMarshal(t, putReq)

			// Add consistency query parameter
			url := fmt.Sprintf("%s/api/v1/entities/%s?consistency=%s", baseURL, entityType, tt.consistency)

			resp, err := client.Post(url, "application/json", strings.NewReader(string(body)))
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, http.StatusCreated, resp.StatusCode,
				"PUT with %s consistency should succeed", tt.consistency)

			// Parse response
			var putResp rest.PutResponse
			err = json.NewDecoder(resp.Body).Decode(&putResp)
			require.NoError(t, err)
			assert.Greater(t, putResp.ID, uint64(0))
			assert.False(t, putResp.Timestamp.IsZero(), "Timestamp should not be zero")
		})
	}
}

// TestConsistencyLevels_Get tests different consistency levels for Get operations
func TestConsistencyLevels_Get(t *testing.T) {
	baseURL := getEnv("QUICBAAS_NODE1_URL", "https://test-node1:8080")
	client := helpers.NewHTTPClient(10 * time.Second)

	entityType := "consistency_get_test"
	entityID := uint64(time.Now().UnixNano())

	// First, create an entity
	testData := map[string]interface{}{
		"value": "test-consistency-get",
	}
	putReq := rest.PutRequest{
		EntityType: entityType,
		ID:         entityID,
		Data:       mustMarshal(t, testData),
	}
	body := mustMarshal(t, putReq)

	resp, err := client.Post(
		fmt.Sprintf("%s/api/v1/entities/%s", baseURL, entityType),
		"application/json",
		strings.NewReader(string(body)),
	)
	require.NoError(t, err)
	resp.Body.Close()

	// Wait for replication
	time.Sleep(500 * time.Millisecond)

	// Test different consistency levels
	tests := []struct {
		name        string
		consistency string
	}{
		{"Strong", "strong"},
		{"Linearizable", "linearizable"},
		{"Eventual", "eventual"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start := time.Now()
			url := fmt.Sprintf("%s/api/v1/entities/%s/%d?consistency=%s",
				baseURL, entityType, entityID, tt.consistency)

			resp, err := client.Get(url)
			duration := time.Since(start)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, http.StatusOK, resp.StatusCode,
				"GET with %s consistency should succeed", tt.consistency)

			var getResp rest.GetResponse
			err = json.NewDecoder(resp.Body).Decode(&getResp)
			require.NoError(t, err)
			assert.NotEmpty(t, getResp.Data, "Expected data to be present")

			t.Logf("%s consistency GET took: %v", tt.name, duration)

			// Linearizable should generally take longer than eventual
			// (though this is not guaranteed in all cases)
			if tt.consistency == "linearizable" {
				t.Logf("Linearizable read completed in %v", duration)
			}
		})
	}
}

// TestConsistencyLevels_Delete tests different consistency levels for Delete operations
func TestConsistencyLevels_Delete(t *testing.T) {
	baseURL := getEnv("QUICBAAS_NODE1_URL", "https://test-node1:8080")
	client := helpers.NewHTTPClient(10 * time.Second)

	entityType := "consistency_delete_test"

	tests := []struct {
		name        string
		consistency string
	}{
		{"Strong", "strong"},
		{"Linearizable", "linearizable"},
		{"Eventual", "eventual"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create entity to delete
			entityID := uint64(time.Now().UnixNano())
			testData := map[string]interface{}{
				"test": "delete-test",
			}
			putReq := rest.PutRequest{
				EntityType: entityType,
				ID:         entityID,
				Data:       mustMarshal(t, testData),
			}
			body := mustMarshal(t, putReq)

			resp, err := client.Post(
				fmt.Sprintf("%s/api/v1/entities/%s", baseURL, entityType),
				"application/json",
				strings.NewReader(string(body)),
			)
			require.NoError(t, err)
			resp.Body.Close()

			// Wait for replication
			time.Sleep(200 * time.Millisecond)

			// Delete with consistency level
			url := fmt.Sprintf("%s/api/v1/entities/%s/%d?consistency=%s",
				baseURL, entityType, entityID, tt.consistency)

			req, err := http.NewRequest(http.MethodDelete, url, nil)
			require.NoError(t, err)

			resp, err = client.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, http.StatusOK, resp.StatusCode,
				"DELETE with %s consistency should succeed", tt.consistency)
		})
	}
}

// TestConsistencyLevels_List tests consistency levels for list operations
func TestConsistencyLevels_List(t *testing.T) {
	baseURL := getEnv("QUICBAAS_NODE1_URL", "https://test-node1:8080")
	client := helpers.NewHTTPClient(10 * time.Second)

	entityType := "consistency_list_test"

	// Create a few entities
	// Note: Use IDs that all route to the same shard (shard 1 at index 0).
	// With 3 shards [1,2,3], routing uses id % 3 for the shard index.
	// IDs like 3, 6, 9, 12, 15 all give id % 3 = 0, mapping to shards[0] = 1.
	// The list operation queries shard 1 by default (when id=0).
	t.Run("Setup_Entities", func(t *testing.T) {
		// Use IDs that are multiples of 3 (id % 3 == 0) to route to shard 1
		for i := 0; i < 5; i++ {
			entityID := uint64((i + 1) * 3) // IDs: 3, 6, 9, 12, 15 - all route to index 0 (shard 1)
			putReq := rest.PutRequest{
				EntityType: entityType,
				ID:         entityID,
				Data:       mustMarshal(t, map[string]interface{}{"index": i}),
			}
			body := mustMarshal(t, putReq)

			t.Logf("Creating entity with ID %d (routes to shard index %d, shard ID %d)", entityID, entityID%3, 1)

			resp, err := client.Post(
				fmt.Sprintf("%s/api/v1/entities/%s", baseURL, entityType),
				"application/json",
				strings.NewReader(string(body)),
			)
			require.NoError(t, err)

			// Check for successful creation
			if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
				bodyData, _ := io.ReadAll(resp.Body)
				t.Logf("Entity creation returned status %d: %s", resp.StatusCode, string(bodyData))
			}
			resp.Body.Close()

			// Small delay to ensure entities have different timestamps
			time.Sleep(10 * time.Millisecond)
		}

		// Wait for replication before proceeding to list tests
		time.Sleep(2 * time.Second)
	})

	// Test list with different consistency levels
	t.Run("List_StrongConsistency", func(t *testing.T) {
		url := fmt.Sprintf("%s/api/v1/entities/%s?consistency=strong&limit=10",
			baseURL, entityType)

		resp, err := client.Get(url)
		require.NoError(t, err)
		defer resp.Body.Close()

		// Read raw body for debugging
		bodyData, err := io.ReadAll(resp.Body)
		require.NoError(t, err)

		if resp.StatusCode != http.StatusOK {
			t.Logf("List returned status %d: %s", resp.StatusCode, string(bodyData))
		}
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var listResp rest.ListResponse
		err = json.Unmarshal(bodyData, &listResp)
		require.NoError(t, err)
		t.Logf("List returned %d entities", len(listResp.Entities))
		assert.GreaterOrEqual(t, len(listResp.Entities), 5)
	})

	t.Run("List_LinearizableConsistency", func(t *testing.T) {
		start := time.Now()
		url := fmt.Sprintf("%s/api/v1/entities/%s?consistency=linearizable&limit=10",
			baseURL, entityType)

		resp, err := client.Get(url)
		duration := time.Since(start)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
		t.Logf("Linearizable list took: %v", duration)
	})

	t.Run("List_EventualConsistency", func(t *testing.T) {
		start := time.Now()
		url := fmt.Sprintf("%s/api/v1/entities/%s?consistency=eventual&limit=10",
			baseURL, entityType)

		resp, err := client.Get(url)
		duration := time.Since(start)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
		t.Logf("Eventual list took: %v", duration)
	})
}

// Helper functions

func getEnv(key, defaultValue string) string {
	if value := getEnvValue(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvValue(key string) string {
	return os.Getenv(key)
}

func mustMarshal(t *testing.T, v interface{}) json.RawMessage {
	data, err := json.Marshal(v)
	require.NoError(t, err)
	return data
}

func mustReadBody(t *testing.T, r io.Reader) []byte {
	data, err := io.ReadAll(r)
	require.NoError(t, err)
	return data
}
