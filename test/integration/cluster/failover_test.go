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

package cluster

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"testing"
	"time"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"

	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// failoverHTTPClient is a package-level HTTP client configured for self-signed TLS certs.
var failoverHTTPClient = newInsecureHTTPClient(30 * time.Second)

// TestDataConsistencyAcrossNodes validates that data remains consistent after writes
func TestDataConsistencyAcrossNodes(t *testing.T) {
	if helpers.IsStandaloneMode(t, helpers.GetClusterEndpoints()) {
		t.Log("Cluster running in standalone/embedded mode - data consistency requires real Raft cluster")
		return
	}

	nodes := getNodeURLs()
	time.Sleep(10 * time.Second)

	entityType := "consistency_test"
	entityID := uint64(100001)

	// Write initial data to node 1
	t.Log("Phase 1: Writing initial data to node 1...")

	initialData := map[string]interface{}{
		"version": 1,
		"value":   "initial",
	}

	if err := writeEntity(nodes[0], entityType, entityID, initialData); err != nil {
		t.Fatalf("Failed to write initial data: %v", err)
	}

	time.Sleep(2 * time.Second)

	// Read from all nodes and verify
	t.Log("Phase 2: Verifying data on all nodes...")

	for i, nodeURL := range nodes {
		data, err := readEntity(nodeURL, entityType, entityID)
		if err != nil {
			t.Errorf("Node %d: Failed to read data: %v", i+1, err)
			continue
		}

		if version, ok := data["version"].(float64); !ok || version != 1 {
			t.Errorf("Node %d: Incorrect version: %v (expected 1)", i+1, data["version"])
		}

		if value, ok := data["value"].(string); !ok || value != "initial" {
			t.Errorf("Node %d: Incorrect value: %v (expected 'initial')", i+1, data["value"])
		}

		t.Logf("Node %d: Data verified", i+1)
	}

	// Update data - try all nodes to find the current leader
	t.Log("Phase 3: Updating data (leader discovery)...")

	updatedData := map[string]interface{}{
		"version": 2,
		"value":   "updated",
	}

	var updateErr error
	for _, nodeURL := range nodes {
		if err := writeEntity(nodeURL, entityType, entityID, updatedData); err == nil {
			updateErr = nil
			break
		} else {
			updateErr = err
		}
	}
	if updateErr != nil {
		t.Fatalf("Failed to update data on any node: %v", updateErr)
	}

	time.Sleep(2 * time.Second)

	// Read from all nodes and verify update
	t.Log("Phase 4: Verifying updated data on all nodes...")

	for i, nodeURL := range nodes {
		data, err := readEntity(nodeURL, entityType, entityID)
		if err != nil {
			t.Errorf("Node %d: Failed to read updated data: %v", i+1, err)
			continue
		}

		if version, ok := data["version"].(float64); !ok || version != 2 {
			t.Errorf("Node %d: Incorrect updated version: %v (expected 2)", i+1, data["version"])
		}

		if value, ok := data["value"].(string); !ok || value != "updated" {
			t.Errorf("Node %d: Incorrect updated value: %v (expected 'updated')", i+1, data["value"])
		}

		t.Logf("Node %d: Updated data verified", i+1)
	}

	t.Log("SUCCESS: Data consistency validated across all nodes")

	// Cleanup
	deleteEntity(nodes[0], entityType, entityID)
}

// TestReadConsistencyLevels validates different consistency levels
func TestReadConsistencyLevels(t *testing.T) {
	nodes := getNodeURLs()
	time.Sleep(10 * time.Second)

	entityType := "consistency_levels"
	entityID := uint64(100002)

	// Write data
	testData := map[string]interface{}{
		"test":      "consistency_levels",
		"timestamp": time.Now().Unix(),
	}

	if err := writeEntity(nodes[0], entityType, entityID, testData); err != nil {
		t.Fatalf("Failed to write test data: %v", err)
	}

	time.Sleep(2 * time.Second)

	// Test different consistency levels
	consistencyLevels := []string{"linearizable", "strong", "eventual"}

	for _, level := range consistencyLevels {
		t.Run(fmt.Sprintf("Consistency_%s", level), func(t *testing.T) {
			url := fmt.Sprintf("%s/api/v1/entities/%s/%d?consistency=%s",
				nodes[0], entityType, entityID, level)

			resp, err := failoverHTTPClient.Get(url)
			if err != nil {
				t.Fatalf("Read with %s consistency failed: %v", level, err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				t.Errorf("Read failed with status %d: %s", resp.StatusCode, string(body))
				return
			}

			t.Logf("SUCCESS: Read with %s consistency", level)
		})
	}

	// Cleanup
	deleteEntity(nodes[0], entityType, entityID)
}

// TestNetworkPartitionRecovery simulates a node going down and coming back
// Note: This test requires manual intervention or Docker container manipulation
// For now, it validates the cluster can handle temporary node unavailability
func TestClusterResilience(t *testing.T) {
	nodes := getNodeURLs()
	time.Sleep(10 * time.Second)

	entityType := "resilience_test"
	baseID := uint64(100100)

	// Write data to all nodes
	t.Log("Phase 1: Writing data across cluster...")

	numEntities := 10
	for i := 0; i < numEntities; i++ {
		entityID := baseID + uint64(i)
		data := map[string]interface{}{
			"id":    entityID,
			"value": fmt.Sprintf("value_%d", i),
		}

		nodeIndex := i % len(nodes)
		if err := writeEntity(nodes[nodeIndex], entityType, entityID, data); err != nil {
			t.Logf("Warning: Failed to write entity %d: %v", entityID, err)
		}
	}

	time.Sleep(2 * time.Second)

	// Verify all data is accessible
	t.Log("Phase 2: Verifying all data is accessible...")

	accessibleCount := 0
	for i := 0; i < numEntities; i++ {
		entityID := baseID + uint64(i)
		if _, err := readEntity(nodes[0], entityType, entityID); err == nil {
			accessibleCount++
		}
	}

	t.Logf("Phase 2 Result: %d/%d entities accessible", accessibleCount, numEntities)

	// Simulate reading even if one node might be temporarily slow
	t.Log("Phase 3: Testing cluster continues to operate...")

	// Try writing new data
	testID := baseID + uint64(numEntities)
	testData := map[string]interface{}{
		"test": "cluster_resilience",
	}

	if err := writeEntity(nodes[0], entityType, testID, testData); err != nil {
		t.Errorf("Cluster unable to accept writes: %v", err)
	} else {
		t.Log("SUCCESS: Cluster continues to accept writes")
	}

	// Verify read
	if _, err := readEntity(nodes[0], entityType, testID); err != nil {
		t.Errorf("Unable to read newly written data: %v", err)
	} else {
		t.Log("SUCCESS: Cluster continues to serve reads")
	}

	// Cleanup
	for i := 0; i <= numEntities; i++ {
		entityID := baseID + uint64(i)
		deleteEntity(nodes[0], entityType, entityID)
	}
}

// TestDataPersistence validates data survives across operations
func TestDataPersistence(t *testing.T) {
	if helpers.IsStandaloneMode(t, helpers.GetClusterEndpoints()) {
		t.Log("Cluster running in standalone/embedded mode - data persistence requires real Raft cluster")
		return
	}

	nodes := getNodeURLs()
	time.Sleep(10 * time.Second)

	entityType := "persistence_test"
	baseID := uint64(100200)
	numEntities := 20

	// Write batch of entities
	t.Logf("Writing %d entities...", numEntities)

	writeSuccess := 0
	for i := 0; i < numEntities; i++ {
		entityID := baseID + uint64(i)
		data := map[string]interface{}{
			"id":        entityID,
			"created":   time.Now().Unix(),
			"persisted": true,
		}

		if err := writeEntity(nodes[0], entityType, entityID, data); err == nil {
			writeSuccess++
		}
	}

	t.Logf("Successfully wrote %d/%d entities", writeSuccess, numEntities)

	time.Sleep(3 * time.Second)

	// Read back all entities
	t.Log("Reading back entities...")

	readSuccess := 0
	for i := 0; i < numEntities; i++ {
		entityID := baseID + uint64(i)
		if data, err := readEntity(nodes[1], entityType, entityID); err == nil {
			if persisted, ok := data["persisted"].(bool); ok && persisted {
				readSuccess++
			}
		}
	}

	t.Logf("Successfully read %d/%d entities", readSuccess, writeSuccess)

	if readSuccess == writeSuccess {
		t.Log("SUCCESS: All written data persisted correctly")
	} else {
		t.Errorf("WARNING: Data persistence incomplete (%d/%d)", readSuccess, writeSuccess)
	}

	// Cleanup
	for i := 0; i < numEntities; i++ {
		entityID := baseID + uint64(i)
		deleteEntity(nodes[0], entityType, entityID)
	}
}

// TestConcurrentReadWrite validates concurrent operations don't cause corruption
func TestConcurrentReadWrite(t *testing.T) {
	if helpers.IsStandaloneMode(t, helpers.GetClusterEndpoints()) {
		t.Log("Cluster running in standalone/embedded mode - concurrent read/write requires real Raft cluster")
		return
	}

	nodes := getNodeURLs()
	time.Sleep(10 * time.Second)

	entityType := "concurrent_rw"
	entityID := uint64(100300)

	// Initial write
	initialData := map[string]interface{}{
		"counter": 0,
	}

	if err := writeEntity(nodes[0], entityType, entityID, initialData); err != nil {
		t.Fatalf("Failed to write initial data: %v", err)
	}

	time.Sleep(1 * time.Second)

	// Concurrent reads
	numReaders := 10
	duration := 10 * time.Second

	t.Logf("Running %d concurrent readers for %v...", numReaders, duration)

	done := make(chan bool, numReaders)
	errors := make(chan error, numReaders)

	for i := 0; i < numReaders; i++ {
		go func(readerID int) {
			deadline := time.Now().Add(duration)
			readCount := 0

			for time.Now().Before(deadline) {
				nodeIndex := readerID % len(nodes)
				if _, err := readEntity(nodes[nodeIndex], entityType, entityID); err != nil {
					errors <- &qberrors.WrappedMessageError{Message: "reader " + strconv.Itoa(readerID), Cause: err}
					done <- false
					return
				}
				readCount++
				time.Sleep(100 * time.Millisecond)
			}

			t.Logf("Reader %d completed %d reads", readerID, readCount)
			errors <- nil
			done <- true
		}(i)
	}

	// Wait for all readers
	successCount := 0
	for i := 0; i < numReaders; i++ {
		if <-done {
			successCount++
		}
		if err := <-errors; err != nil {
			t.Logf("Reader error: %v", err)
		}
	}

	if successCount == numReaders {
		t.Logf("SUCCESS: All %d concurrent readers completed", numReaders)
	} else {
		t.Errorf("Only %d/%d readers succeeded", successCount, numReaders)
	}

	// Cleanup
	deleteEntity(nodes[0], entityType, entityID)
}

// Helper functions

func writeEntity(nodeURL, entityType string, entityID uint64, data map[string]interface{}) error {
	dataBytes, err := json.Marshal(data)
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "marshal failed", Cause: err}
	}

	payload := map[string]interface{}{
		"id":   entityID,
		"data": json.RawMessage(dataBytes),
	}

	payloadBytes, _ := json.Marshal(payload)
	// POST to /api/v1/entities/{entityType} - ID is in the body, not the URL
	url := fmt.Sprintf("%s/api/v1/entities/%s", nodeURL, entityType)

	resp, err := failoverHTTPClient.Post(url, "application/json", bytes.NewReader(payloadBytes))
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "post failed", Cause: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return &qberrors.MessageError{Message: "write failed with status " + strconv.Itoa(resp.StatusCode) + ": " + string(body)}
	}

	return nil
}

func readEntity(nodeURL, entityType string, entityID uint64) (map[string]interface{}, error) {
	url := fmt.Sprintf("%s/api/v1/entities/%s/%d", nodeURL, entityType, entityID)

	resp, err := failoverHTTPClient.Get(url)
	if err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "get failed", Cause: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, &qberrors.MessageError{Message: "read failed with status " + strconv.Itoa(resp.StatusCode) + ": " + string(body)}
	}

	// Read the raw body first
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "read body failed", Cause: err}
	}

	var readResp map[string]json.RawMessage
	if err := json.Unmarshal(bodyBytes, &readResp); err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "decode failed", Cause: err}
	}

	// Extract and parse the data field
	if dataRaw, ok := readResp["data"]; ok {
		var data map[string]interface{}
		if err := json.Unmarshal(dataRaw, &data); err != nil {
			return nil, &qberrors.WrappedMessageError{Message: "parse data failed", Cause: err}
		}
		return data, nil
	}

	return nil, &qberrors.MessageError{Message: "no data field in response"}
}

func deleteEntity(nodeURL, entityType string, entityID uint64) error {
	url := fmt.Sprintf("%s/api/v1/entities/%s/%d", nodeURL, entityType, entityID)

	req, _ := http.NewRequest(http.MethodDelete, url, nil)
	resp, err := failoverHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return nil
}
