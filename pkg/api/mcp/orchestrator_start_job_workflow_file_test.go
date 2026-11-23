//go:build mcp

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

package mcp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// A start-job whose workflow_file does not load is refused before any job
// runs, so a job never starts without the definition the caller asked for.

const startJobWorkflowFileYAML = `name: file-workflow
description: "loaded from a file"
mode: autonomous
agents:
  - role: lead
    backend: test
    model: test-model
settings:
  max_iterations: 1
  poll_interval: 5s
`

// workflowFileOrchestrator returns a real orchestrator, with a workflow
// loader installed when withLoader is set.
func workflowFileOrchestrator(t *testing.T, withLoader bool) orchestration.AgentService {
	t.Helper()
	svc := newAsyncTurnOrchestrator(t, false)
	if withLoader {
		inst, ok := svc.(interface {
			SetWorkflowLoader(*orchestration.WorkflowLoader)
		})
		require.True(t, ok, "agent service must accept a workflow loader")
		inst.SetWorkflowLoader(orchestration.NewWorkflowLoader())
	}
	return svc
}

// requireStartJobWorkflowFileRefused calls the MCP start-job tool with path
// as its workflow_file and requires an invalid-input refusal with no job.
func requireStartJobWorkflowFileRefused(t *testing.T, svc orchestration.AgentService, path string) {
	t.Helper()
	_, err := newFleetRegistry(t, svc).executeAgentTool(tenantctx.WithTenant(context.Background(), "t1"),
		"quicbaas_orchestrator_start_job", map[string]interface{}{"description": "d", "workflow_file": path})
	require.Error(t, err)
	assert.True(t, errors.Is(err, orchestration.ErrInvalidInput), "want ErrInvalidInput, got %T: %v", err, err)
	jobs, listErr := svc.ListJobs(orchestration.ContextWithTenant(context.Background(), "t1"), "t1", nil)
	require.NoError(t, listErr)
	assert.Empty(t, jobs, "no job may start")
}

func TestOrchestratorStartJob_WorkflowFileMalformedRefused(t *testing.T) {
	svc := workflowFileOrchestrator(t, true)
	path := filepath.Join(t.TempDir(), "wf.yaml")
	require.NoError(t, os.WriteFile(path, []byte("name: [unterminated"), 0o600))
	requireStartJobWorkflowFileRefused(t, svc, path)
}

func TestOrchestratorStartJob_WorkflowFileMissingRefused(t *testing.T) {
	svc := workflowFileOrchestrator(t, true)
	requireStartJobWorkflowFileRefused(t, svc, filepath.Join(t.TempDir(), "absent.yaml"))
}

func TestOrchestratorStartJob_WorkflowFileWithoutLoaderRefused(t *testing.T) {
	svc := workflowFileOrchestrator(t, false)
	path := filepath.Join(t.TempDir(), "wf.yaml")
	require.NoError(t, os.WriteFile(path, []byte(startJobWorkflowFileYAML), 0o600))
	requireStartJobWorkflowFileRefused(t, svc, path)
}
