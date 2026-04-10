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

func TestNewNodeCommands(t *testing.T) {
	cmd := newNodeCommands()

	require.NotNil(t, cmd)
	assert.Equal(t, "node", cmd.Use)
	assert.Contains(t, cmd.Short, "Node management")

	// Verify subcommands are registered
	subcommands := cmd.Commands()
	commandNames := make([]string, len(subcommands))
	for i, subcmd := range subcommands {
		commandNames[i] = subcmd.Name()
	}

	assert.Contains(t, commandNames, "add", "add command should be registered")
	assert.Contains(t, commandNames, "remove", "remove command should be registered")
	assert.Contains(t, commandNames, "replace", "replace command should be registered")
	assert.Contains(t, commandNames, "list", "list command should be registered")
}

func TestNodeAddCommand(t *testing.T) {
	cmd := newNodeAddCommand()

	require.NotNil(t, cmd)
	assert.Equal(t, "add <node-id> <address>", cmd.Use)
	assert.Contains(t, cmd.Short, "Add node")

	// Verify flags exist
	healthCheckIntervalFlag := cmd.Flags().Lookup("health-check-interval")
	require.NotNil(t, healthCheckIntervalFlag)
	assert.Equal(t, "30s", healthCheckIntervalFlag.DefValue)

	healthCheckTimeoutFlag := cmd.Flags().Lookup("health-check-timeout")
	require.NotNil(t, healthCheckTimeoutFlag)
	assert.Equal(t, "5s", healthCheckTimeoutFlag.DefValue)

	outputFlag := cmd.Flags().Lookup("output")
	require.NotNil(t, outputFlag)
	assert.Equal(t, "table", outputFlag.DefValue)
}

func TestNodeAddCommandHelp(t *testing.T) {
	cmd := newNodeAddCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"--help"})

	err := cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "add")
	assert.Contains(t, output, "Add")
	assert.Contains(t, output, "--health-check-interval")
	assert.Contains(t, output, "--health-check-timeout")
}

func TestNodeAddCommandInvalidArgs(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		expectError bool
	}{
		{
			name:        "no arguments",
			args:        []string{},
			expectError: true,
		},
		{
			name:        "one argument",
			args:        []string{"node-1"},
			expectError: true,
		},
		{
			name:        "too many arguments",
			args:        []string{"node-1", "addr1", "extra"},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newNodeAddCommand()
			cmd.SetArgs(tt.args)

			var buf bytes.Buffer
			cmd.SetOut(&buf)
			cmd.SetErr(&buf)

			err := cmd.Execute()
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestNodeRemoveCommand(t *testing.T) {
	cmd := newNodeRemoveCommand()

	require.NotNil(t, cmd)
	assert.Equal(t, "remove <node-id>", cmd.Use)
	assert.Contains(t, cmd.Short, "Remove")

	// Verify flags exist
	evacuationTimeoutFlag := cmd.Flags().Lookup("evacuation-timeout")
	require.NotNil(t, evacuationTimeoutFlag)
	assert.Equal(t, "30m0s", evacuationTimeoutFlag.DefValue)

	verifyDataFlag := cmd.Flags().Lookup("verify-data")
	require.NotNil(t, verifyDataFlag)
	assert.Equal(t, "true", verifyDataFlag.DefValue)

	outputFlag := cmd.Flags().Lookup("output")
	require.NotNil(t, outputFlag)
	assert.Equal(t, "table", outputFlag.DefValue)
}

func TestNodeRemoveCommandHelp(t *testing.T) {
	cmd := newNodeRemoveCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"--help"})

	err := cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "remove")
	assert.Contains(t, output, "Remove")
	assert.Contains(t, output, "--evacuation-timeout")
	assert.Contains(t, output, "--verify-data")
}

func TestNodeReplaceCommand(t *testing.T) {
	cmd := newNodeReplaceCommand()

	require.NotNil(t, cmd)
	assert.Equal(t, "replace <old-id> <new-id> <address>", cmd.Use)
	assert.Contains(t, cmd.Short, "Replace")

	// Verify flags exist
	evacuationTimeoutFlag := cmd.Flags().Lookup("evacuation-timeout")
	require.NotNil(t, evacuationTimeoutFlag)
	assert.Equal(t, "30m0s", evacuationTimeoutFlag.DefValue)

	verifyDataFlag := cmd.Flags().Lookup("verify-data")
	require.NotNil(t, verifyDataFlag)
	assert.Equal(t, "true", verifyDataFlag.DefValue)

	autoReplaceFlag := cmd.Flags().Lookup("auto-replace")
	require.NotNil(t, autoReplaceFlag)
	assert.Equal(t, "false", autoReplaceFlag.DefValue)

	outputFlag := cmd.Flags().Lookup("output")
	require.NotNil(t, outputFlag)
	assert.Equal(t, "table", outputFlag.DefValue)
}

func TestNodeReplaceCommandHelp(t *testing.T) {
	cmd := newNodeReplaceCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"--help"})

	err := cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "replace")
	assert.Contains(t, output, "Replace")
	assert.Contains(t, output, "--evacuation-timeout")
	assert.Contains(t, output, "--auto-replace")
	assert.Contains(t, output, "--verify-data")
}

func TestNodeListCommand(t *testing.T) {
	cmd := newNodeListCommand()

	require.NotNil(t, cmd)
	assert.Equal(t, "list", cmd.Use)
	assert.Contains(t, cmd.Short, "List all")

	// Verify flags exist
	outputFlag := cmd.Flags().Lookup("output")
	require.NotNil(t, outputFlag)
	assert.Equal(t, "table", outputFlag.DefValue)
}

func TestNodeListCommandHelp(t *testing.T) {
	cmd := newNodeListCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"--help"})

	err := cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "list")
	assert.Contains(t, output, "List")
}

func TestNodeListCommandInvalidArgs(t *testing.T) {
	cmd := newNodeListCommand()
	cmd.SetArgs([]string{"extra-arg"})

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Execute()
	// List command should accept 0 args, so extra args should error
	assert.Error(t, err)
}
