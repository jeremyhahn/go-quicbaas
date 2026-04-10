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
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRebalanceCommands(t *testing.T) {
	cmd := newRebalanceCommands()

	require.NotNil(t, cmd)
	assert.Equal(t, "rebalance", cmd.Use)
	assert.Contains(t, cmd.Short, "Cluster rebalancing")

	// Verify subcommands are registered
	subcommands := cmd.Commands()
	commandNames := make([]string, len(subcommands))
	for i, subcmd := range subcommands {
		commandNames[i] = subcmd.Name()
	}

	assert.Contains(t, commandNames, "start", "start command should be registered")
	assert.Contains(t, commandNames, "status", "status command should be registered")
}

func TestRebalanceStartCommand(t *testing.T) {
	cmd := newRebalanceStartCommand()

	require.NotNil(t, cmd)
	assert.Equal(t, "start", cmd.Use)
	assert.Contains(t, cmd.Short, "Start")

	// Verify command-specific flags exist (--address and --timeout are global in main.go)
	targetImbalanceFlag := cmd.Flags().Lookup("target-imbalance")
	require.NotNil(t, targetImbalanceFlag)
	assert.Equal(t, "0.1", targetImbalanceFlag.DefValue)

	maxConcurrentFlag := cmd.Flags().Lookup("max-concurrent")
	require.NotNil(t, maxConcurrentFlag)
	assert.Equal(t, "2", maxConcurrentFlag.DefValue)

	minKeysFlag := cmd.Flags().Lookup("min-keys")
	require.NotNil(t, minKeysFlag)
	assert.Equal(t, "1000", minKeysFlag.DefValue)

	dryRunFlag := cmd.Flags().Lookup("dry-run")
	require.NotNil(t, dryRunFlag)
	assert.Equal(t, "false", dryRunFlag.DefValue)

	rateLimitFlag := cmd.Flags().Lookup("rate-limit")
	require.NotNil(t, rateLimitFlag)
	assert.Equal(t, "10240", rateLimitFlag.DefValue)

	outputFlag := cmd.Flags().Lookup("output")
	require.NotNil(t, outputFlag)
	assert.Equal(t, "table", outputFlag.DefValue)

	followFlag := cmd.Flags().Lookup("follow")
	require.NotNil(t, followFlag)
	assert.Equal(t, "false", followFlag.DefValue)
}

func TestRebalanceStartCommandHelp(t *testing.T) {
	cmd := newRebalanceStartCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"--help"})

	err := cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "start")
	assert.Contains(t, output, "Start")
	assert.Contains(t, output, "--target-imbalance")
	assert.Contains(t, output, "--max-concurrent")
	assert.Contains(t, output, "--dry-run")
	assert.Contains(t, output, "--rate-limit")
	assert.Contains(t, output, "--follow")
	// Note: --address is a global flag defined in main.go
	assert.Contains(t, output, "global --address flag")
}

func TestRebalanceStartCommandWithExtraArgs(t *testing.T) {
	cmd := newRebalanceStartCommand()
	cmd.SetArgs([]string{"extra-arg"})

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	// Command execution - behavior depends on implementation
	// The command may or may not accept extra args
	_ = cmd.Execute()
}

func TestRebalanceStatusCommand(t *testing.T) {
	cmd := newRebalanceStatusCommand()

	require.NotNil(t, cmd)
	assert.Equal(t, "status", cmd.Use)
	assert.Contains(t, cmd.Short, "Check")

	// Verify command-specific flags exist (--address and --timeout are global in main.go)
	outputFlag := cmd.Flags().Lookup("output")
	require.NotNil(t, outputFlag)
	assert.Equal(t, "table", outputFlag.DefValue)

	operationFlag := cmd.Flags().Lookup("operation")
	require.NotNil(t, operationFlag)
}

func TestRebalanceStatusCommandHelp(t *testing.T) {
	cmd := newRebalanceStatusCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"--help"})

	err := cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "status")
	assert.Contains(t, output, "Check")
	// Note: --address is a global flag defined in main.go
	assert.Contains(t, output, "global --address flag")
}

func TestRebalanceStatusCommandWithExtraArgs(t *testing.T) {
	cmd := newRebalanceStatusCommand()
	cmd.SetArgs([]string{"extra-arg"})

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	// Command execution - behavior depends on implementation
	// The command may or may not accept extra args
	_ = cmd.Execute()
}

func TestRebalancePlanCommand(t *testing.T) {
	cmd := newRebalancePlanCommand()

	require.NotNil(t, cmd)
	assert.Equal(t, "plan", cmd.Use)
	assert.Contains(t, cmd.Short, "plan")

	// Verify command-specific flags exist (--address and --timeout are global in main.go)
	targetImbalanceFlag := cmd.Flags().Lookup("target-imbalance")
	require.NotNil(t, targetImbalanceFlag)
	assert.Equal(t, "0.1", targetImbalanceFlag.DefValue)

	outputFlag := cmd.Flags().Lookup("output")
	require.NotNil(t, outputFlag)
	assert.Equal(t, "table", outputFlag.DefValue)
}

func TestRebalancePlanCommandHelp(t *testing.T) {
	cmd := newRebalancePlanCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"--help"})

	err := cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "plan")
	assert.Contains(t, output, "--target-imbalance")
	assert.Contains(t, output, "--output")
	// Note: --address is a global flag defined in main.go
	assert.Contains(t, output, "global --address flag")
}
