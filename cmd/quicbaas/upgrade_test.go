// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// TestNewUpgradeCommands tests the upgrade commands creation.
func TestNewUpgradeCommands(t *testing.T) {
	testLog := createTestLoggerForUpgrade(t)
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
}

// TestNewUpgradeStartCommand tests the upgrade start command.
func TestNewUpgradeStartCommand(t *testing.T) {
	testLog := createTestLoggerForUpgrade(t)
	log = testLog

	t.Run("StartCommandCreated", func(t *testing.T) {
		cmd := newUpgradeStartCommand()
		assert.NotNil(t, cmd)
		assert.Equal(t, "start <version>", cmd.Use)

		// Check flags
		assert.NotNil(t, cmd.Flags().Lookup("server"))
		assert.NotNil(t, cmd.Flags().Lookup("timeout"))
		assert.NotNil(t, cmd.Flags().Lookup("node-id"))
		assert.NotNil(t, cmd.Flags().Lookup("max-unavailable"))
		assert.NotNil(t, cmd.Flags().Lookup("wait-time"))
		assert.NotNil(t, cmd.Flags().Lookup("health-timeout"))
		assert.NotNil(t, cmd.Flags().Lookup("drain-timeout"))
		assert.NotNil(t, cmd.Flags().Lookup("rollback-on-failure"))
		assert.NotNil(t, cmd.Flags().Lookup("verify-compatibility"))
		assert.NotNil(t, cmd.Flags().Lookup("skip-backup"))
		assert.NotNil(t, cmd.Flags().Lookup("output"))
		assert.NotNil(t, cmd.Flags().Lookup("follow"))
	})

	t.Run("RequiresVersionArg", func(t *testing.T) {
		cmd := newUpgradeStartCommand()
		err := cmd.Args(cmd, []string{})
		assert.Error(t, err)
	})

	t.Run("AcceptsVersionArg", func(t *testing.T) {
		cmd := newUpgradeStartCommand()
		err := cmd.Args(cmd, []string{"v2.0.0"})
		assert.NoError(t, err)
	})
}

// TestNewUpgradeStatusCommand tests the upgrade status command.
func TestNewUpgradeStatusCommand(t *testing.T) {
	testLog := createTestLoggerForUpgrade(t)
	log = testLog

	t.Run("StatusCommandCreated", func(t *testing.T) {
		cmd := newUpgradeStatusCommand()
		assert.NotNil(t, cmd)
		assert.Equal(t, "status", cmd.Use)

		assert.NotNil(t, cmd.Flags().Lookup("server"))
		assert.NotNil(t, cmd.Flags().Lookup("timeout"))
		assert.NotNil(t, cmd.Flags().Lookup("operation"))
		assert.NotNil(t, cmd.Flags().Lookup("output"))
	})
}

// TestDisplayUpgradeStartResponse tests the display function for upgrade start.
func TestDisplayUpgradeStartResponse(t *testing.T) {
	testLog := createTestLoggerForUpgrade(t)
	log = testLog

	origFormat := upgradeOutputFormat
	defer func() { upgradeOutputFormat = origFormat }()

	resp := &pb.UpgradeNodeResponse{
		OperationId: "op-test-123",
		Started:     time.Now().UnixNano(),
	}

	t.Run("TableOutput", func(t *testing.T) {
		upgradeOutputFormat = "table"
		err := displayUpgradeStartResponse(resp)
		assert.NoError(t, err)
	})

	t.Run("JSONOutput", func(t *testing.T) {
		upgradeOutputFormat = "json"
		err := displayUpgradeStartResponse(resp)
		assert.NoError(t, err)
	})

	t.Run("InvalidOutputFormat", func(t *testing.T) {
		upgradeOutputFormat = "invalid"
		err := displayUpgradeStartResponse(resp)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid")
	})
}

// TestDisplayUpgradeProgress tests the progress display function.
func TestDisplayUpgradeProgress(t *testing.T) {
	resp := &pb.GetUpgradeStatusResponse{
		OperationId:     "op-progress-1",
		NodesUpgraded:   2,
		TotalNodes:      5,
		PercentComplete: 40.0,
		Status:          "in_progress",
	}

	// This function only prints to stdout, no error return
	displayUpgradeProgress(resp)
}

// TestDisplayUpgradeStatusResponse tests the display function for upgrade status.
func TestDisplayUpgradeStatusResponse(t *testing.T) {
	testLog := createTestLoggerForUpgrade(t)
	log = testLog

	resp := &pb.GetUpgradeStatusResponse{
		OperationId:     "op-status-1",
		NodesUpgraded:   3,
		TotalNodes:      5,
		PercentComplete: 60.0,
		CurrentVersion:  "v1.0.0",
		TargetVersion:   "v2.0.0",
		Status:          "in_progress",
	}

	t.Run("TableFormat_InProgress", func(t *testing.T) {
		err := displayUpgradeStatusResponse(resp, "table")
		assert.NoError(t, err)
	})

	t.Run("JSONFormat", func(t *testing.T) {
		err := displayUpgradeStatusResponse(resp, "json")
		assert.NoError(t, err)
	})

	t.Run("TableFormat_Completed", func(t *testing.T) {
		r := &pb.GetUpgradeStatusResponse{Status: "completed"}
		err := displayUpgradeStatusResponse(r, "table")
		assert.NoError(t, err)
	})

	t.Run("TableFormat_Failed", func(t *testing.T) {
		r := &pb.GetUpgradeStatusResponse{Status: "failed"}
		err := displayUpgradeStatusResponse(r, "table")
		assert.NoError(t, err)
	})

	t.Run("TableFormat_RolledBack", func(t *testing.T) {
		r := &pb.GetUpgradeStatusResponse{Status: "rolled_back"}
		err := displayUpgradeStatusResponse(r, "table")
		assert.NoError(t, err)
	})

	t.Run("TableFormat_Initiated", func(t *testing.T) {
		r := &pb.GetUpgradeStatusResponse{Status: "initiated"}
		err := displayUpgradeStatusResponse(r, "table")
		assert.NoError(t, err)
	})

	t.Run("TableFormat_UnknownStatus", func(t *testing.T) {
		r := &pb.GetUpgradeStatusResponse{Status: "unknown_status"}
		err := displayUpgradeStatusResponse(r, "table")
		assert.NoError(t, err)
	})

	t.Run("InvalidFormat", func(t *testing.T) {
		err := displayUpgradeStatusResponse(resp, "xml")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "xml")
	})
}

// createTestLoggerForUpgrade creates a logger for upgrade tests.
func createTestLoggerForUpgrade(t *testing.T) *logger.Logger {
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
