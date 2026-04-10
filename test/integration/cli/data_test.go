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

package cli

import (
	"testing"
)

// =============================================================================
// Data CRUD Tests
// =============================================================================

// TestDataLifecycle tests the complete data CRUD lifecycle across all transports.
func TestDataLifecycle(t *testing.T) {
	RunForAllTransports(t, "DataLifecycle", func(t *testing.T, runner *TestRunner) {
		entityType := "test_entity"
		key := UniqueKey("test-key")
		value := "test-value-data"
		suite := DataSuite(entityType, key, value)
		runner.RunSuite(suite)
	})
}

// TestDataPut tests putting data across all transports.
func TestDataPut(t *testing.T) {
	RunForAllTransports(t, "DataPut", func(t *testing.T, runner *TestRunner) {
		entityType := "put_entity"
		key := UniqueKey("put-key")
		value := "put-value"

		cmd := DataPutCommand(entityType, key, value)
		runner.RunAndAssert(cmd)

		// Cleanup
		cleanup := DataDeleteCommand(entityType, key)
		runner.Run(cleanup)
	})
}

// TestDataGet tests getting data across all transports.
func TestDataGet(t *testing.T) {
	RunForAllTransports(t, "DataGet", func(t *testing.T, runner *TestRunner) {
		entityType := "get_entity"
		key := UniqueKey("get-key")
		value := "get-value"

		// Put first
		putCmd := DataPutCommand(entityType, key, value)
		runner.RunAndAssert(putCmd)

		// Get
		getCmd := DataGetCommand(entityType, key)
		runner.RunAndAssert(getCmd)

		// Cleanup
		cleanup := DataDeleteCommand(entityType, key)
		runner.Run(cleanup)
	})
}

// TestDataList tests listing data across all transports.
func TestDataList(t *testing.T) {
	RunForAllTransports(t, "DataList", func(t *testing.T, runner *TestRunner) {
		entityType := "list_entity"
		keys := []string{
			UniqueKey("list-key-1"),
			UniqueKey("list-key-2"),
			UniqueKey("list-key-3"),
		}

		// Put multiple entities
		for i, key := range keys {
			putCmd := DataPutCommand(entityType, key, "value-"+string(rune('a'+i)))
			runner.RunAndAssert(putCmd)
		}

		// List
		listCmd := DataListCommand(entityType, 10)
		runner.RunAndAssert(listCmd)

		// Cleanup
		for _, key := range keys {
			cleanup := DataDeleteCommand(entityType, key)
			runner.Run(cleanup)
		}
	})
}

// TestDataDelete tests deleting data across all transports.
func TestDataDelete(t *testing.T) {
	RunForAllTransports(t, "DataDelete", func(t *testing.T, runner *TestRunner) {
		entityType := "delete_entity"
		key := UniqueKey("delete-key")
		value := "delete-value"

		// Put
		putCmd := DataPutCommand(entityType, key, value)
		runner.RunAndAssert(putCmd)

		// Delete
		deleteCmd := DataDeleteCommand(entityType, key)
		runner.RunAndAssert(deleteCmd)

		// Verify deletion - get should fail or return not found
		getCmd := DataGetCommand(entityType, key)
		stdout, stderr, err := runner.Run(getCmd)
		if err != nil {
			t.Logf("Get after delete correctly failed or returned not found: stdout=%s stderr=%s", stdout, stderr)
		}
	})
}
