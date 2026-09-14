// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

func TestOrchestratorTaskDeleteCommandShape(t *testing.T) {
	cmd := newOrchTaskDeleteCommand()
	assert.Equal(t, "delete <taskID>", cmd.Use)
	assert.Error(t, cmd.Args(cmd, []string{}))
	assert.NoError(t, cmd.Args(cmd, []string{"task_1"}))
	assert.Error(t, cmd.Args(cmd, []string{"task_1", "task_2"}))
}

// TestOrchestratorTaskDeleteIsRegistered proves the subcommand is reachable
// from `quicbaas orchestrator task`; a command nobody adds is unusable.
func TestOrchestratorTaskDeleteIsRegistered(t *testing.T) {
	for _, sub := range newOrchTaskCommand().Commands() {
		if sub.Name() == "delete" {
			return
		}
	}
	t.Fatal("`orchestrator task delete` is not registered")
}

func TestOrchestratorRunOrchTaskDelete(t *testing.T) {
	saveAndRestoreOrchestratorState(t)

	t.Run("Success", func(t *testing.T) {
		mock := &mockOrchestratorClient{
			deleteTaskFn: func(_ context.Context, req *orchestratorSDK.DeleteTaskRequest) (*orchestratorSDK.DeleteTaskResponse, error) {
				assert.Equal(t, "task_123", req.TaskID)
				return &orchestratorSDK.DeleteTaskResponse{TaskID: "task_123", Deleted: true}, nil
			},
		}
		setMockOrchestratorFactory(mock)
		orchOutputFmt = "table"

		cmd := newOrchTaskDeleteCommand()
		require.NoError(t, cmd.RunE(cmd, []string{"task_123"}))
	})

	t.Run("ClientError", func(t *testing.T) {
		mock := &mockOrchestratorClient{
			deleteTaskFn: func(context.Context, *orchestratorSDK.DeleteTaskRequest) (*orchestratorSDK.DeleteTaskResponse, error) {
				return nil, errors.New("task is leased by a running job")
			},
		}
		setMockOrchestratorFactory(mock)
		cmd := newOrchTaskDeleteCommand()
		require.Error(t, cmd.RunE(cmd, []string{"task_123"}))
	})

	t.Run("FactoryError", func(t *testing.T) {
		orchestratorClientFactory = func() (OrchestratorClient, error) { return nil, errors.New("no client") }
		cmd := newOrchTaskDeleteCommand()
		require.Error(t, cmd.RunE(cmd, []string{"task_123"}))
	})
}

func TestFormatOrchDeleteResponse(t *testing.T) {
	resp := &orchestratorSDK.DeleteTaskResponse{TaskID: "task_1", Deleted: true}
	for _, format := range []string{"json", "yaml", "table", "raw"} {
		t.Run(format, func(t *testing.T) {
			var buf bytes.Buffer
			require.NoError(t, formatOrchDeleteResponse(resp, format, &buf))
			assert.Contains(t, buf.String(), "task_1")
		})
	}
	t.Run("not deleted raw", func(t *testing.T) {
		var buf bytes.Buffer
		require.NoError(t, formatOrchDeleteResponse(&orchestratorSDK.DeleteTaskResponse{TaskID: "task_1"}, "raw", &buf))
		assert.Contains(t, buf.String(), "Failed to delete")
	})
	t.Run("invalid format", func(t *testing.T) {
		var buf bytes.Buffer
		require.Error(t, formatOrchDeleteResponse(resp, "bogus", &buf))
	})
}
