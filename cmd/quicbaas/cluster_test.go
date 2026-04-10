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

package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// TestNewClusterCommands tests the cluster commands creation.
func TestNewClusterCommands(t *testing.T) {
	// Initialize test logger
	testLog := createTestLoggerForCluster(t)
	log = testLog

	t.Run("ClusterCommandsExist", func(t *testing.T) {
		cmd := newClusterCommands()
		assert.NotNil(t, cmd)
		assert.Equal(t, "cluster", cmd.Use)
		assert.NotEmpty(t, cmd.Short)
		assert.NotEmpty(t, cmd.Long)

		// Check subcommands
		commands := cmd.Commands()
		assert.GreaterOrEqual(t, len(commands), 1)

		var foundStatus bool
		for _, c := range commands {
			if c.Use == "status" {
				foundStatus = true
			}
		}

		assert.True(t, foundStatus, "cluster status command should exist")
	})
}

// TestNewClusterStatusCommand tests the cluster status command creation.
func TestNewClusterStatusCommand(t *testing.T) {
	testLog := createTestLoggerForCluster(t)
	log = testLog

	t.Run("StatusCommandCreated", func(t *testing.T) {
		cmd := newClusterStatusCommand()
		assert.NotNil(t, cmd)
		assert.Equal(t, "status", cmd.Use)
		assert.NotEmpty(t, cmd.Short)
		assert.NotEmpty(t, cmd.Long)

		// Check local flags (output is the only local flag; address and timeout are global)
		assert.NotNil(t, cmd.Flags().Lookup("output"))
		// Note: --address and --timeout are global flags defined in main.go
		// They are not local to this command, so we don't check for them here
	})

	t.Run("InvalidOutputFormat", func(t *testing.T) {
		// Set global address for test
		address = "localhost:50051"
		globalTimeout = 10 * time.Second

		cmd := newClusterStatusCommand()
		cmd.SetArgs([]string{"--output", "invalid"})

		// This will fail trying to connect, but that's expected in unit tests
		err := cmd.Execute()
		// We expect an error because we can't connect to a real server
		// or because of invalid format
		assert.Error(t, err)
	})
}

// TestPrintClusterStatusJSON tests JSON output formatting.
func TestPrintClusterStatusJSON(t *testing.T) {
	testLog := createTestLoggerForCluster(t)
	log = testLog

	t.Run("ValidCLIResponse", func(t *testing.T) {
		status := &cli.ClusterStatusResponse{
			Healthy:    true,
			LeaderID:   1,
			NodeCount:  3,
			ReadyNodes: 3,
			Nodes: []cli.NodeInfo{
				{
					NodeID:    1,
					Address:   "localhost:50051",
					IsLeader:  true,
					IsHealthy: true,
					LastSeen:  time.Now(),
				},
			},
			RaftGroupInfo: []cli.RaftGroupInfo{
				{
					GroupID:     1,
					LeaderID:    1,
					MemberCount: 3,
				},
			},
		}

		err := printClusterStatusJSON(status)
		assert.NoError(t, err)
	})

	t.Run("EmptyStatus", func(t *testing.T) {
		status := &cli.ClusterStatusResponse{}
		err := printClusterStatusJSON(status)
		assert.NoError(t, err)
	})

	t.Run("GenericInterface", func(t *testing.T) {
		// Test with a generic map
		status := map[string]interface{}{
			"healthy":     true,
			"leader_id":   1,
			"node_count":  3,
			"ready_nodes": 3,
		}

		err := printClusterStatusJSON(status)
		assert.NoError(t, err)
	})

	t.Run("InvalidJSON", func(t *testing.T) {
		// Channel types cannot be marshaled to JSON
		status := make(chan int)
		err := printClusterStatusJSON(status)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to marshal result")
	})
}

// TestPrintClusterStatusTableCLI tests table output formatting.
func TestPrintClusterStatusTableCLI(t *testing.T) {
	testLog := createTestLoggerForCluster(t)
	log = testLog

	t.Run("HealthyCluster", func(t *testing.T) {
		now := time.Now()
		status := &cli.ClusterStatusResponse{
			Healthy:    true,
			LeaderID:   1,
			NodeCount:  3,
			ReadyNodes: 3,
			Nodes: []cli.NodeInfo{
				{
					NodeID:    1,
					Address:   "localhost:50051",
					IsLeader:  true,
					IsHealthy: true,
					LastSeen:  now,
				},
				{
					NodeID:    2,
					Address:   "localhost:50052",
					IsLeader:  false,
					IsHealthy: true,
					LastSeen:  now,
				},
			},
			RaftGroupInfo: []cli.RaftGroupInfo{
				{
					GroupID:     1,
					LeaderID:    1,
					MemberCount: 3,
				},
			},
		}

		err := printClusterStatusTableCLI(status)
		assert.NoError(t, err)
	})

	t.Run("UnhealthyCluster", func(t *testing.T) {
		now := time.Now()
		status := &cli.ClusterStatusResponse{
			Healthy:    false,
			LeaderID:   0,
			NodeCount:  3,
			ReadyNodes: 1,
			Nodes: []cli.NodeInfo{
				{
					NodeID:    1,
					Address:   "localhost:50051",
					IsLeader:  false,
					IsHealthy: false,
					LastSeen:  now.Add(-5 * time.Minute),
				},
			},
		}

		err := printClusterStatusTableCLI(status)
		assert.NoError(t, err)
	})

	t.Run("EmptyNodes", func(t *testing.T) {
		status := &cli.ClusterStatusResponse{
			Healthy:    true,
			LeaderID:   1,
			NodeCount:  0,
			ReadyNodes: 0,
			Nodes:      []cli.NodeInfo{},
		}

		err := printClusterStatusTableCLI(status)
		assert.NoError(t, err)
	})

	t.Run("EmptyRaftGroups", func(t *testing.T) {
		status := &cli.ClusterStatusResponse{
			Healthy:       true,
			LeaderID:      1,
			NodeCount:     1,
			ReadyNodes:    1,
			RaftGroupInfo: []cli.RaftGroupInfo{},
		}

		err := printClusterStatusTableCLI(status)
		assert.NoError(t, err)
	})

	t.Run("ZeroTimestampLastSeen", func(t *testing.T) {
		status := &cli.ClusterStatusResponse{
			Healthy:    true,
			LeaderID:   1,
			NodeCount:  1,
			ReadyNodes: 1,
			Nodes: []cli.NodeInfo{
				{
					NodeID:    1,
					Address:   "localhost:50051",
					IsLeader:  true,
					IsHealthy: true,
					LastSeen:  time.Time{}, // Zero value
				},
			},
		}

		err := printClusterStatusTableCLI(status)
		assert.NoError(t, err)
	})

	t.Run("FallbackToJSON", func(t *testing.T) {
		// Pass a non-CLI response type to trigger fallback
		status := map[string]interface{}{
			"healthy":    true,
			"leader_id":  1,
			"node_count": 3,
		}

		err := printClusterStatusTableCLI(status)
		assert.NoError(t, err)
	})

	t.Run("MultipleRaftGroups", func(t *testing.T) {
		status := &cli.ClusterStatusResponse{
			Healthy:    true,
			LeaderID:   1,
			NodeCount:  6,
			ReadyNodes: 6,
			RaftGroupInfo: []cli.RaftGroupInfo{
				{
					GroupID:     1,
					LeaderID:    1,
					MemberCount: 3,
				},
				2: {
					GroupID:     2,
					LeaderID:    4,
					MemberCount: 3,
				},
			},
		}

		err := printClusterStatusTableCLI(status)
		assert.NoError(t, err)
	})
}

// createTestLoggerForCluster creates a logger for cluster tests.
func createTestLoggerForCluster(t *testing.T) *logger.Logger {
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
