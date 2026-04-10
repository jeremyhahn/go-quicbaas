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

// Package main provides tests for QuicBaaS CLI operations commands.
package main

import (
	"bytes"
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	storagePkg "github.com/jeremyhahn/go-quicbaas/pkg/storage"
)

// createTestLogger creates a test logger.
func createTestLogger(t *testing.T) *logger.Logger {
	logConfig := logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stderr",
		Component: "test",
	}
	testLog, err := logger.New(logConfig)
	if err != nil {
		t.Fatalf("Failed to create test logger: %v", err)
	}
	return testLog
}

// TestMigrateCommands tests the migrate command group.
func TestMigrateCommands(t *testing.T) {
	// Initialize test logger
	testLog := createTestLogger(t)
	log = testLog

	t.Run("MigrateCommandsExist", func(t *testing.T) {
		cmd := newMigrateCommands()
		assert.NotNil(t, cmd)
		assert.Equal(t, "migrate", cmd.Use)

		// Check subcommands
		commands := cmd.Commands()
		assert.GreaterOrEqual(t, len(commands), 3)

		var foundStart, foundStatus, foundCancel bool
		for _, c := range commands {
			switch c.Use {
			case "start --source <url> --dest <url>":
				foundStart = true
			case "status":
				foundStatus = true
			case "cancel":
				foundCancel = true
			}
		}

		assert.True(t, foundStart, "migrate start command should exist")
		assert.True(t, foundStatus, "migrate status command should exist")
		assert.True(t, foundCancel, "migrate cancel command should exist")
	})

	t.Run("MigrateStartValidation", func(t *testing.T) {
		cmd := newMigrateStartCommand()

		// Test without required flags
		err := cmd.RunE(cmd, []string{})
		assert.Error(t, err)
	})

	t.Run("MigrateStatusWithoutActive", func(t *testing.T) {
		currentMigration = nil
		cmd := newMigrateStatusCommand()

		err := cmd.RunE(cmd, []string{})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no active migration")
	})

	t.Run("MigrateCancelWithoutActive", func(t *testing.T) {
		currentMigration = nil
		cmd := newMigrateCancelCommand()

		err := cmd.RunE(cmd, []string{})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no active migration")
	})

	t.Run("MigrateStartWithMemory", func(t *testing.T) {
		cmd := newMigrateStartCommand()

		migrateSourceURL = "memory://"
		migrateDestURL = "memory://"
		migrateBatchSize = 10
		migrateConcurrency = 1
		migrateVerify = false
		migrateDeleteSource = false
		migrateRateLimit = 0
		migrateCheckpoint = 1 * time.Second
		migrateOutputFormat = "table"
		migrateFollowProgress = false

		// Capture output
		oldStdout := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w

		err := cmd.RunE(cmd, []string{})

		w.Close()
		os.Stdout = oldStdout
		var buf bytes.Buffer
		buf.ReadFrom(r)

		assert.NoError(t, err)
	})

	t.Run("MigrateStatusJSONOutput", func(t *testing.T) {
		// Create a migration manager using the local types
		source, _ := createStorageFromURL("memory://")
		dest, _ := createStorageFromURL("memory://")
		config := &MigrationConfig{
			BatchSize:          10,
			Concurrency:        1,
			VerifyData:         false,
			DeleteSource:       false,
			RateLimitKBps:      0,
			CheckpointInterval: 1 * time.Second,
		}
		mgr, _ := NewMigrationManager(source, dest, config, asInterfaceLogger(log))
		currentMigration = mgr

		cmd := newMigrateStatusCommand()
		migrateOutputFormat = "json"

		// Capture output
		oldStdout := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w

		err := cmd.RunE(cmd, []string{})

		w.Close()
		os.Stdout = oldStdout
		var buf bytes.Buffer
		buf.ReadFrom(r)

		assert.NoError(t, err)
		source.Close()
		dest.Close()
	})
}

// TestRebalanceCommands tests the rebalance command group.
func TestRebalanceCommands(t *testing.T) {
	testLog := createTestLogger(t)
	log = testLog

	t.Run("RebalanceCommandsExist", func(t *testing.T) {
		cmd := newRebalanceCommands()
		assert.NotNil(t, cmd)
		assert.Equal(t, "rebalance", cmd.Use)

		// Check subcommands
		commands := cmd.Commands()
		assert.GreaterOrEqual(t, len(commands), 3)

		var foundStart, foundStatus, foundPlan bool
		for _, c := range commands {
			switch c.Use {
			case "start":
				foundStart = true
			case "status":
				foundStatus = true
			case "plan":
				foundPlan = true
			}
		}

		assert.True(t, foundStart, "rebalance start command should exist")
		assert.True(t, foundStatus, "rebalance status command should exist")
		assert.True(t, foundPlan, "rebalance plan command should exist")
	})

	t.Run("RebalanceStartFlagsExist", func(t *testing.T) {
		cmd := newRebalanceStartCommand()

		// Verify command-specific flags exist (--address and --timeout are global flags from main.go)
		assert.NotNil(t, cmd.Flags().Lookup("dry-run"))
		assert.NotNil(t, cmd.Flags().Lookup("target-imbalance"))
		assert.NotNil(t, cmd.Flags().Lookup("max-concurrent"))
		assert.NotNil(t, cmd.Flags().Lookup("min-keys"))
		assert.NotNil(t, cmd.Flags().Lookup("rate-limit"))
		assert.NotNil(t, cmd.Flags().Lookup("output"))
		assert.NotNil(t, cmd.Flags().Lookup("follow"))
	})

	t.Run("RebalanceStatusFlagsExist", func(t *testing.T) {
		cmd := newRebalanceStatusCommand()

		// Verify command-specific flags exist (--address and --timeout are global flags from main.go)
		assert.NotNil(t, cmd.Flags().Lookup("operation"))
		assert.NotNil(t, cmd.Flags().Lookup("output"))
	})

	t.Run("RebalancePlanFlagsExist", func(t *testing.T) {
		cmd := newRebalancePlanCommand()

		// Verify command-specific flags exist (--address and --timeout are global flags from main.go)
		assert.NotNil(t, cmd.Flags().Lookup("target-imbalance"))
		assert.NotNil(t, cmd.Flags().Lookup("output"))
	})
}

// TestNodeCommands tests the node command group.
func TestNodeCommands(t *testing.T) {
	testLog := createTestLogger(t)
	log = testLog

	t.Run("NodeCommandsExist", func(t *testing.T) {
		cmd := newNodeCommands()
		assert.NotNil(t, cmd)
		assert.Equal(t, "node", cmd.Use)

		// Check subcommands
		commands := cmd.Commands()
		assert.GreaterOrEqual(t, len(commands), 4)

		var foundAdd, foundRemove, foundReplace, foundList bool
		for _, c := range commands {
			switch c.Use {
			case "add <node-id> <address>":
				foundAdd = true
			case "remove <node-id>":
				foundRemove = true
			case "replace <old-id> <new-id> <address>":
				foundReplace = true
			case "list":
				foundList = true
			}
		}

		assert.True(t, foundAdd, "node add command should exist")
		assert.True(t, foundRemove, "node remove command should exist")
		assert.True(t, foundReplace, "node replace command should exist")
		assert.True(t, foundList, "node list command should exist")
	})

	t.Run("NodeAddAndList", func(t *testing.T) {
		// Reset node manager and initialize flags with valid defaults
		currentNodeManager = nil
		nodeOutputFormat = "table"
		nodeMaxTransfers = 3
		nodeHealthCheckInterval = 30 * time.Second
		nodeHealthCheckTimeout = 5 * time.Second
		nodeEvacuationTimeout = 30 * time.Minute
		nodeVerifyData = true
		nodeAutoReplace = false
		nodeGracePeriod = 2 * time.Minute

		// Add a node
		addCmd := newNodeAddCommand()
		err := addCmd.RunE(addCmd, []string{"test-node-1", "localhost:50051"})
		assert.NoError(t, err)

		// List nodes
		listCmd := newNodeListCommand()

		// Capture output
		var buf bytes.Buffer
		originalStdout := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w

		err = listCmd.RunE(listCmd, []string{})

		w.Close()
		os.Stdout = originalStdout
		buf.ReadFrom(r)
		output := buf.String()

		assert.NoError(t, err)
		assert.Contains(t, output, "test-node-1")
		assert.Contains(t, output, "localhost:50051")
	})

	t.Run("NodeAddDuplicateError", func(t *testing.T) {
		// Reset node manager and initialize flags with valid defaults
		currentNodeManager = nil
		nodeOutputFormat = "table"
		nodeMaxTransfers = 3
		nodeHealthCheckInterval = 30 * time.Second
		nodeHealthCheckTimeout = 5 * time.Second
		nodeEvacuationTimeout = 30 * time.Minute
		nodeVerifyData = true
		nodeAutoReplace = false
		nodeGracePeriod = 2 * time.Minute

		addCmd := newNodeAddCommand()

		// First add should succeed
		err := addCmd.RunE(addCmd, []string{"test-node-2", "localhost:50052"})
		assert.NoError(t, err)

		// Second add with same ID should fail
		err = addCmd.RunE(addCmd, []string{"test-node-2", "localhost:50052"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "already exists")
	})

	t.Run("NodeListJSONOutput", func(t *testing.T) {
		currentNodeManager = nil
		nodeOutputFormat = "json"
		nodeMaxTransfers = 3
		nodeHealthCheckInterval = 30 * time.Second
		nodeHealthCheckTimeout = 5 * time.Second
		nodeEvacuationTimeout = 30 * time.Minute
		nodeVerifyData = true
		nodeAutoReplace = false
		nodeGracePeriod = 2 * time.Minute

		addCmd := newNodeAddCommand()
		err := addCmd.RunE(addCmd, []string{"test-node-json", "localhost:50053"})
		assert.NoError(t, err)

		listCmd := newNodeListCommand()

		// Capture output
		oldStdout := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w

		err = listCmd.RunE(listCmd, []string{})

		w.Close()
		os.Stdout = oldStdout
		var buf bytes.Buffer
		buf.ReadFrom(r)
		output := buf.String()

		assert.NoError(t, err)
		assert.Contains(t, output, "test-node-json")
	})

	t.Run("NodeRemove", func(t *testing.T) {
		currentNodeManager = nil
		nodeOutputFormat = "table"
		nodeMaxTransfers = 3
		nodeHealthCheckInterval = 30 * time.Second
		nodeHealthCheckTimeout = 5 * time.Second
		nodeEvacuationTimeout = 30 * time.Minute
		nodeVerifyData = false
		nodeAutoReplace = false
		nodeGracePeriod = 2 * time.Minute

		// Add a node first
		addCmd := newNodeAddCommand()
		err := addCmd.RunE(addCmd, []string{"test-node-remove", "localhost:50054"})
		assert.NoError(t, err)

		// Remove the node
		removeCmd := newNodeRemoveCommand()

		// Capture output
		oldStdout := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w

		err = removeCmd.RunE(removeCmd, []string{"test-node-remove"})

		w.Close()
		os.Stdout = oldStdout
		var buf bytes.Buffer
		buf.ReadFrom(r)

		assert.NoError(t, err)
	})

	t.Run("NodeReplace", func(t *testing.T) {
		currentNodeManager = nil
		nodeOutputFormat = "table"
		nodeMaxTransfers = 3
		nodeHealthCheckInterval = 30 * time.Second
		nodeHealthCheckTimeout = 5 * time.Second
		nodeEvacuationTimeout = 30 * time.Minute
		nodeVerifyData = false
		nodeAutoReplace = false
		nodeGracePeriod = 2 * time.Minute

		// Add a node first
		addCmd := newNodeAddCommand()
		err := addCmd.RunE(addCmd, []string{"test-node-old", "localhost:50055"})
		assert.NoError(t, err)

		// Replace the node
		replaceCmd := newNodeReplaceCommand()

		// Capture output
		oldStdout := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w

		err = replaceCmd.RunE(replaceCmd, []string{"test-node-old", "test-node-new", "localhost:50056"})

		w.Close()
		os.Stdout = oldStdout
		var buf bytes.Buffer
		buf.ReadFrom(r)

		assert.NoError(t, err)
	})
}

// TestUpgradeCommands tests the upgrade command group.
func TestUpgradeCommands(t *testing.T) {
	testLog := createTestLogger(t)
	log = testLog

	t.Run("UpgradeCommandsExist", func(t *testing.T) {
		cmd := newUpgradeCommands()
		assert.NotNil(t, cmd)
		assert.Equal(t, "upgrade", cmd.Use)

		// Check subcommands
		commands := cmd.Commands()
		assert.GreaterOrEqual(t, len(commands), 2)

		var foundStart, foundStatus bool
		for _, c := range commands {
			switch c.Use {
			case "start <version>":
				foundStart = true
			case "status":
				foundStatus = true
			}
		}

		assert.True(t, foundStart, "upgrade start command should exist")
		assert.True(t, foundStatus, "upgrade status command should exist")
	})

	t.Run("UpgradeStatusCommandStructure", func(t *testing.T) {
		cmd := newUpgradeStatusCommand()
		assert.NotNil(t, cmd)
		assert.Equal(t, "status", cmd.Use)

		// Check flags exist
		assert.NotNil(t, cmd.Flags().Lookup("server"))
		assert.NotNil(t, cmd.Flags().Lookup("timeout"))
		assert.NotNil(t, cmd.Flags().Lookup("operation"))
		assert.NotNil(t, cmd.Flags().Lookup("output"))
	})

	t.Run("UpgradeStartCommandStructure", func(t *testing.T) {
		cmd := newUpgradeStartCommand()
		assert.NotNil(t, cmd)
		assert.Equal(t, "start <version>", cmd.Use)

		// Check flags exist
		assert.NotNil(t, cmd.Flags().Lookup("server"))
		assert.NotNil(t, cmd.Flags().Lookup("timeout"))
		assert.NotNil(t, cmd.Flags().Lookup("node-id"))
		assert.NotNil(t, cmd.Flags().Lookup("max-unavailable"))
		assert.NotNil(t, cmd.Flags().Lookup("follow"))
		assert.NotNil(t, cmd.Flags().Lookup("output"))
	})
}

// TestStorageURLParsing tests the createStorageFromURL helper.
func TestStorageURLParsing(t *testing.T) {
	testLog := createTestLogger(t)
	log = testLog

	tests := []struct {
		name      string
		url       string
		expectErr bool
		errorMsg  string
	}{
		{
			name:      "MemoryStorage",
			url:       "memory://",
			expectErr: false,
		},
		{
			name:      "PebbleStorage",
			url:       "pebble:///tmp/test-storage",
			expectErr: true,
			errorMsg:  "unsupported storage scheme",
		},
		{
			name:      "FileStorage",
			url:       "file:///tmp/file-storage",
			expectErr: true,
			errorMsg:  "unsupported storage scheme",
		},
		{
			name:      "InvalidScheme",
			url:       "invalid://path",
			expectErr: true,
			errorMsg:  "unsupported storage scheme",
		},
		{
			name:      "NoScheme",
			url:       "/just/a/path",
			expectErr: true,
			errorMsg:  "invalid storage URL format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storage, err := createStorageFromURL(tt.url)

			if tt.expectErr {
				assert.Error(t, err)
				assert.Nil(t, storage)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, storage)
				if storage != nil {
					storage.Close()
				}
			}
		})
	}
}

// TestMigrateFormatBytes tests the formatBytes helper function.
func TestMigrateFormatBytes(t *testing.T) {
	tests := []struct {
		bytes    uint64
		expected string
	}{
		{0, "0 B"},
		{1023, "1023 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1048576, "1.0 MB"},
		{1572864, "1.5 MB"},
		{1073741824, "1.0 GB"},
		{1099511627776, "1.0 TB"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := formatBytes(tt.bytes)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestMemoryStorageAdapter tests the memory storage adapter.
func TestMemoryStorageAdapter(t *testing.T) {
	testLog := createTestLogger(t)
	log = testLog

	storage, err := createStorageFromURL("memory://")
	require.NoError(t, err)
	require.NotNil(t, storage)
	defer storage.Close()

	t.Run("SetAndGet", func(t *testing.T) {
		err := storage.Set([]byte("key1"), []byte("value1"))
		assert.NoError(t, err)

		val, err := storage.Get([]byte("key1"))
		assert.NoError(t, err)
		assert.Equal(t, []byte("value1"), val)
	})

	t.Run("SetWithContext", func(t *testing.T) {
		ctx := context.Background()
		err := storage.SetWithContext(ctx, []byte("key2"), []byte("value2"))
		assert.NoError(t, err)

		val, err := storage.GetWithContext(ctx, []byte("key2"))
		assert.NoError(t, err)
		assert.Equal(t, []byte("value2"), val)
	})

	t.Run("Delete", func(t *testing.T) {
		err := storage.Set([]byte("key3"), []byte("value3"))
		assert.NoError(t, err)

		err = storage.Delete([]byte("key3"))
		assert.NoError(t, err)

		_, err = storage.Get([]byte("key3"))
		assert.Error(t, err)
	})

	t.Run("Batch", func(t *testing.T) {
		batch := storage.Batch()
		assert.NotNil(t, batch)

		err := batch.Set([]byte("batch1"), []byte("bvalue1"))
		assert.NoError(t, err)

		err = batch.Set([]byte("batch2"), []byte("bvalue2"))
		assert.NoError(t, err)

		err = batch.Commit()
		assert.NoError(t, err)

		val, err := storage.Get([]byte("batch1"))
		assert.NoError(t, err)
		assert.Equal(t, []byte("bvalue1"), val)
	})

	t.Run("BatchRollback", func(t *testing.T) {
		batch := storage.Batch()
		err := batch.Set([]byte("rollback1"), []byte("value"))
		assert.NoError(t, err)

		err = batch.Rollback()
		assert.NoError(t, err)

		_, err = storage.Get([]byte("rollback1"))
		assert.Error(t, err)
	})

	t.Run("Iterator", func(t *testing.T) {
		// Setup test data
		storage.Set([]byte("iter1"), []byte("val1"))
		storage.Set([]byte("iter2"), []byte("val2"))
		storage.Set([]byte("iter3"), []byte("val3"))

		opts := &storagePkg.IteratorOptions{
			LowerBound: []byte("iter1"),
			UpperBound: []byte("iter9"),
		}
		iter := storage.NewIterator(opts)
		assert.NotNil(t, iter)
		defer iter.Close()

		found := iter.SeekToFirst()
		assert.True(t, found)
		assert.True(t, iter.Valid())

		key := iter.Key()
		assert.NotNil(t, key)

		value := iter.Value()
		assert.NotNil(t, value)

		hasNext := iter.Next()
		assert.True(t, hasNext)

		err := iter.Error()
		assert.NoError(t, err)
	})

	t.Run("Snapshot", func(t *testing.T) {
		storage.Set([]byte("snap1"), []byte("snapval1"))

		snap, err := storage.Snapshot()
		assert.NoError(t, err)
		assert.NotNil(t, snap)
		defer snap.Close()

		val, err := snap.Get([]byte("snap1"))
		assert.NoError(t, err)
		assert.Equal(t, []byte("snapval1"), val)
	})

	t.Run("CompactAndFlush", func(t *testing.T) {
		err := storage.Compact([]byte("a"), []byte("z"))
		assert.NoError(t, err)

		err = storage.Flush()
		assert.NoError(t, err)
	})
}

// TestHelpers tests helper functions.
func TestHelpers(t *testing.T) {
	testLog := createTestLogger(t)

	t.Run("AsInterfaceLogger", func(t *testing.T) {
		ilog := asInterfaceLogger(testLog)
		assert.NotNil(t, ilog)

		// Test logger methods
		ilog2 := ilog.WithField("test", "value")
		assert.NotNil(t, ilog2)

		ilog3 := ilog.WithFields(map[string]interface{}{"key1": "val1"})
		assert.NotNil(t, ilog3)

		ilog4 := ilog.WithError(assert.AnError)
		assert.NotNil(t, ilog4)
	})
}

// TestDisplayFunctions tests display helper functions.
func TestDisplayFunctions(t *testing.T) {
	testLog := createTestLogger(t)
	log = testLog

	t.Run("DisplayNodeState", func(t *testing.T) {
		nodeState := &NodeState{
			NodeID:           "test-node",
			Address:          "localhost:50051",
			Status:           NodeStatusHealthy,
			JoinedAt:         time.Now(),
			LastSeenAt:       time.Now(),
			HealthCheckCount: 10,
			FailureCount:     0,
			KeyCount:         100,
			ByteCount:        1024,
		}

		// Test table format
		nodeOutputFormat = "table"
		oldStdout := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w

		err := displayNodeState(nodeState)

		w.Close()
		os.Stdout = oldStdout
		var buf bytes.Buffer
		buf.ReadFrom(r)
		output := buf.String()

		assert.NoError(t, err)
		assert.Contains(t, output, "test-node")

		// Test JSON format
		nodeOutputFormat = "json"
		r, w, _ = os.Pipe()
		os.Stdout = w

		err = displayNodeState(nodeState)

		w.Close()
		os.Stdout = oldStdout
		buf.Reset()
		buf.ReadFrom(r)

		assert.NoError(t, err)

		// Test invalid format
		nodeOutputFormat = "invalid"
		err = displayNodeState(nodeState)
		assert.Error(t, err)
	})

	t.Run("DisplayNodeReplacementReport", func(t *testing.T) {
		report := &NodeReplacementReport{
			OldNodeID:        "old-node",
			NewNodeID:        "new-node",
			StartTime:        time.Now().Add(-5 * time.Minute),
			EndTime:          time.Now(),
			Duration:         5 * time.Minute,
			KeysEvacuated:    1000,
			BytesEvacuated:   1048576,
			EvacuationTime:   4 * time.Minute,
			VerificationTime: 1 * time.Minute,
			Success:          true,
			Errors:           []string{},
		}

		// Test table format
		nodeOutputFormat = "table"
		oldStdout := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w

		err := displayNodeReplacementReport(report)

		w.Close()
		os.Stdout = oldStdout
		var buf bytes.Buffer
		buf.ReadFrom(r)
		output := buf.String()

		assert.NoError(t, err)
		assert.Contains(t, output, "old-node")
		assert.Contains(t, output, "new-node")

		// Test JSON format
		nodeOutputFormat = "json"
		r, w, _ = os.Pipe()
		os.Stdout = w

		err = displayNodeReplacementReport(report)

		w.Close()
		os.Stdout = oldStdout
		buf.Reset()
		buf.ReadFrom(r)

		assert.NoError(t, err)
	})

	t.Run("DisplayRebalanceReport", func(t *testing.T) {
		report := &RebalanceReport{
			StartTime:       time.Now().Add(-10 * time.Minute),
			EndTime:         time.Now(),
			Duration:        10 * time.Minute,
			MovesCompleted:  5,
			MovesFailed:     0,
			KeysRebalanced:  5000,
			BytesRebalanced: 5242880,
			ImbalanceBefore: 0.25,
			ImbalanceAfter:  0.05,
			Errors:          []string{},
		}

		// Test table format
		oldStdout := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w

		err := displayRebalanceReport(report, "table")

		w.Close()
		os.Stdout = oldStdout
		var buf bytes.Buffer
		buf.ReadFrom(r)

		assert.NoError(t, err)

		// Test JSON format
		r, w, _ = os.Pipe()
		os.Stdout = w

		err = displayRebalanceReport(report, "json")

		w.Close()
		os.Stdout = oldStdout
		buf.Reset()
		buf.ReadFrom(r)

		assert.NoError(t, err)
	})

	t.Run("UpgradeCommandFlagsExist", func(t *testing.T) {
		// Test upgrade start command flags
		startCmd := newUpgradeStartCommand()
		assert.NotNil(t, startCmd.Flags().Lookup("server"))
		assert.NotNil(t, startCmd.Flags().Lookup("timeout"))
		assert.NotNil(t, startCmd.Flags().Lookup("max-unavailable"))
		assert.NotNil(t, startCmd.Flags().Lookup("follow"))

		// Test upgrade status command flags
		statusCmd := newUpgradeStatusCommand()
		assert.NotNil(t, statusCmd.Flags().Lookup("server"))
		assert.NotNil(t, statusCmd.Flags().Lookup("timeout"))
		assert.NotNil(t, statusCmd.Flags().Lookup("operation"))
		assert.NotNil(t, statusCmd.Flags().Lookup("output"))
	})
}

// TestOutputFormatErrors tests invalid output format handling.
func TestOutputFormatErrors(t *testing.T) {
	testLog := createTestLogger(t)
	log = testLog

	t.Run("InvalidRebalanceOutputFormat", func(t *testing.T) {
		report := &RebalanceReport{}
		err := displayRebalanceReport(report, "invalid")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported output format")
	})

	t.Run("InvalidUpgradeOutputFormat", func(t *testing.T) {
		resp := &pb.GetUpgradeStatusResponse{
			OperationId:     "test-op",
			NodesUpgraded:   1,
			TotalNodes:      3,
			PercentComplete: 33.3,
			CurrentVersion:  "v1.0.0",
			TargetVersion:   "v2.0.0",
			Status:          "in_progress",
		}
		err := displayUpgradeStatusResponse(resp, "invalid")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported output format")
	})

	t.Run("InvalidMigrateOutputFormat", func(t *testing.T) {
		migrateOutputFormat = "invalid"
		result := &MigrationResult{}
		err := displayMigrationResult(result)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported output format")
	})

	t.Run("InvalidNodeStateFormat", func(t *testing.T) {
		nodeState := &NodeState{
			NodeID:  "test",
			Address: "localhost:50051",
			Status:  NodeStatusHealthy,
		}
		nodeOutputFormat = "invalid"
		err := displayNodeState(nodeState)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported output format")
	})
}
