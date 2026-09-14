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
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

func TestOrchestratorTaskBlockCommandFlags(t *testing.T) {
	cmd := newOrchTaskBlockCommand()
	assert.Equal(t, "block <taskID>", cmd.Use)
	require.NotNil(t, cmd.Flags().Lookup("kind"))
	require.NotNil(t, cmd.Flags().Lookup("reason"))
	assert.Equal(t, "true", cmd.Flags().Lookup("kind").Annotations["cobra_annotation_bash_completion_one_required_flag"][0])
	assert.Equal(t, "true", cmd.Flags().Lookup("reason").Annotations["cobra_annotation_bash_completion_one_required_flag"][0])
}

func TestOrchestratorTaskReadmitCommandFlags(t *testing.T) {
	cmd := newOrchTaskReadmitCommand()
	assert.Equal(t, "readmit <taskID>", cmd.Use)
	assert.Error(t, cmd.Args(cmd, []string{}))
	assert.NoError(t, cmd.Args(cmd, []string{"task_1"}))
}

func TestOrchestratorTaskListCommand_ProjectAndStatusesFlags(t *testing.T) {
	saveAndRestoreOrchestratorState(t)

	mock := &mockOrchestratorClient{
		listTasksFn: func(_ context.Context, req *orchestratorSDK.ListTasksRequest) (*orchestratorSDK.ListTasksResponse, error) {
			assert.Equal(t, "proj-1", req.ProjectID)
			assert.Equal(t, []string{"blocked", "pending"}, req.Statuses)
			return &orchestratorSDK.ListTasksResponse{Tasks: []*orchestratorSDK.Task{}}, nil
		},
	}
	setMockOrchestratorFactory(mock)
	orchOutputFmt = "json"

	cmd := newOrchTaskListCommand()
	require.NotNil(t, cmd.Flags().Lookup("project"))
	require.NotNil(t, cmd.Flags().Lookup("statuses"))
	require.NoError(t, cmd.Flags().Set("project", "proj-1"))
	require.NoError(t, cmd.Flags().Set("statuses", "blocked,pending"))
	require.NoError(t, cmd.RunE(cmd, []string{}))
}

func TestOrchestratorRunOrchTaskBlock(t *testing.T) {
	saveAndRestoreOrchestratorState(t)

	t.Run("Success", func(t *testing.T) {
		mock := &mockOrchestratorClient{
			blockTaskFn: func(_ context.Context, req *orchestratorSDK.BlockTaskRequest) (*orchestratorSDK.BlockTaskResponse, error) {
				assert.Equal(t, "task_123", req.TaskID)
				assert.Equal(t, "needs_info", req.Kind)
				assert.Equal(t, "which branch?", req.Reason)
				return &orchestratorSDK.BlockTaskResponse{TaskID: "task_123", Kind: "needs_info", Blocked: true}, nil
			},
		}
		setMockOrchestratorFactory(mock)
		orchOutputFmt = "json"

		// Flags are bound after the command is built, since binding resets
		// the backing variable to the flag's default.
		cmd := newOrchTaskBlockCommand()
		orchTaskBlockKind = "needs_info"
		orchTaskBlockReason = "which branch?"
		require.NoError(t, cmd.RunE(cmd, []string{"task_123"}))
	})

	t.Run("ClientError", func(t *testing.T) {
		mock := &mockOrchestratorClient{
			blockTaskFn: func(context.Context, *orchestratorSDK.BlockTaskRequest) (*orchestratorSDK.BlockTaskResponse, error) {
				return nil, errors.New("task is completed")
			},
		}
		setMockOrchestratorFactory(mock)
		orchOutputFmt = "json"

		cmd := newOrchTaskBlockCommand()
		orchTaskBlockKind = "blocked"
		orchTaskBlockReason = "r"
		require.Error(t, cmd.RunE(cmd, []string{"task_123"}))
	})

	t.Run("FactoryError", func(t *testing.T) {
		orchestratorClientFactory = func() (OrchestratorClient, error) { return nil, errors.New("no client") }
		cmd := newOrchTaskBlockCommand()
		require.Error(t, cmd.RunE(cmd, []string{"task_123"}))
	})
}

func TestOrchestratorRunOrchTaskReadmit(t *testing.T) {
	saveAndRestoreOrchestratorState(t)

	t.Run("Success", func(t *testing.T) {
		mock := &mockOrchestratorClient{
			readmitTaskFn: func(_ context.Context, req *orchestratorSDK.ReadmitTaskRequest) (*orchestratorSDK.ReadmitTaskResponse, error) {
				assert.Equal(t, "task_123", req.TaskID)
				return &orchestratorSDK.ReadmitTaskResponse{TaskID: "task_123", Readmitted: true}, nil
			},
		}
		setMockOrchestratorFactory(mock)
		orchOutputFmt = "table"

		cmd := newOrchTaskReadmitCommand()
		require.NoError(t, cmd.RunE(cmd, []string{"task_123"}))
	})

	t.Run("ClientError", func(t *testing.T) {
		mock := &mockOrchestratorClient{
			readmitTaskFn: func(context.Context, *orchestratorSDK.ReadmitTaskRequest) (*orchestratorSDK.ReadmitTaskResponse, error) {
				return nil, errors.New("not blocked")
			},
		}
		setMockOrchestratorFactory(mock)
		cmd := newOrchTaskReadmitCommand()
		require.Error(t, cmd.RunE(cmd, []string{"task_123"}))
	})

	t.Run("FactoryError", func(t *testing.T) {
		orchestratorClientFactory = func() (OrchestratorClient, error) { return nil, errors.New("no client") }
		cmd := newOrchTaskReadmitCommand()
		require.Error(t, cmd.RunE(cmd, []string{"task_123"}))
	})
}

func TestFormatOrchBlockResponse(t *testing.T) {
	resp := &orchestratorSDK.BlockTaskResponse{TaskID: "task_1", Kind: "escalated", Blocked: true}
	for _, format := range []string{"json", "yaml", "table", "raw"} {
		t.Run(format, func(t *testing.T) {
			var buf bytes.Buffer
			require.NoError(t, formatOrchBlockResponse(resp, format, &buf))
			assert.Contains(t, buf.String(), "task_1")
		})
	}
	t.Run("not blocked raw", func(t *testing.T) {
		var buf bytes.Buffer
		require.NoError(t, formatOrchBlockResponse(&orchestratorSDK.BlockTaskResponse{TaskID: "task_1"}, "raw", &buf))
		assert.Contains(t, buf.String(), "Failed to block")
	})
	t.Run("invalid format", func(t *testing.T) {
		var buf bytes.Buffer
		require.Error(t, formatOrchBlockResponse(resp, "bogus", &buf))
	})
}

func TestFormatOrchReadmitResponse(t *testing.T) {
	resp := &orchestratorSDK.ReadmitTaskResponse{TaskID: "task_1", Readmitted: true}
	for _, format := range []string{"json", "yaml", "table", "raw"} {
		t.Run(format, func(t *testing.T) {
			var buf bytes.Buffer
			require.NoError(t, formatOrchReadmitResponse(resp, format, &buf))
			assert.Contains(t, buf.String(), "task_1")
		})
	}
	t.Run("not readmitted raw", func(t *testing.T) {
		var buf bytes.Buffer
		require.NoError(t, formatOrchReadmitResponse(&orchestratorSDK.ReadmitTaskResponse{TaskID: "task_1"}, "raw", &buf))
		assert.Contains(t, buf.String(), "Failed to readmit")
	})
	t.Run("invalid format", func(t *testing.T) {
		var buf bytes.Buffer
		require.Error(t, formatOrchReadmitResponse(resp, "bogus", &buf))
	})
}
