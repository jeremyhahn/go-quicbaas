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

// MCP refuses a start-job that names any workflow_file before any job runs
// or any file is read: a workflow file is a path on the daemon's own
// filesystem, and loading it would register a workflow for the tenant.

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

// A valid workflow file, a symlink to one and a system file are each refused,
// and none registers a workflow. The context carries no permission grants, as
// for a caller allowed only to start jobs: the tool refuses regardless.
func TestOrchestratorStartJob_WorkflowFileRefusedOverMCP(t *testing.T) {
	cases := []struct {
		name string
		path func(t *testing.T) string
	}{
		{"valid file", func(t *testing.T) string {
			path := filepath.Join(t.TempDir(), "wf.yaml")
			require.NoError(t, os.WriteFile(path, []byte(startJobWorkflowFileYAML), 0o600))
			return path
		}},
		{"symlink to a valid file", func(t *testing.T) string {
			dir := t.TempDir()
			target := filepath.Join(dir, "wf.yaml")
			require.NoError(t, os.WriteFile(target, []byte(startJobWorkflowFileYAML), 0o600))
			link := filepath.Join(dir, "link.yaml")
			require.NoError(t, os.Symlink(target, link))
			return link
		}},
		{"system file", func(*testing.T) string { return "/etc/hostname" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := newAsyncTurnOrchestrator(t, false)
			inst, ok := svc.(interface {
				SetWorkflowLoader(*orchestration.WorkflowLoader)
			})
			require.True(t, ok, "agent service must accept a workflow loader")
			loader := orchestration.NewWorkflowLoader()
			inst.SetWorkflowLoader(loader)

			requireStartJobWorkflowFileRefused(t, svc, tc.path(t))
			assert.Empty(t, loader.List("t1"), "no workflow may be registered")
		})
	}
}

// A workflow_file naming another tenant's path is refused before the service
// is reached, whether or not tenant_id also names that tenant.
func TestOrchestratorStartJob_WorkflowFileForeignTenantRefused(t *testing.T) {
	svc := &fleetMockAgentService{
		startJobFn: func(context.Context, *orchestration.StartJobRequest) (*orchestration.StartJobResponse, error) {
			t.Fatal("the service must not be reached")
			return nil, nil
		},
	}
	reg := newFleetRegistry(t, svc)
	ctx := tenantctx.WithTenant(context.Background(), "t1")

	t.Run("foreign path", func(t *testing.T) {
		_, err := reg.executeAgentTool(ctx, "quicbaas_orchestrator_start_job", map[string]interface{}{
			"description":   "d",
			"workflow_file": "/data/tenants/t2/wf.yaml",
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, orchestration.ErrInvalidInput), "want ErrInvalidInput, got %T: %v", err, err)
	})
	t.Run("foreign path and tenant", func(t *testing.T) {
		_, err := reg.executeAgentTool(ctx, "quicbaas_orchestrator_start_job", map[string]interface{}{
			"description":   "d",
			"workflow_file": "/data/tenants/t2/wf.yaml",
			"tenant_id":     "t2",
		})
		require.Error(t, err)
	})
}

// The start-job tool's schema offers workflow_name and no workflow_file.
func TestOrchestratorStartJob_SchemaHasNoWorkflowFile(t *testing.T) {
	reg := newFleetRegistry(t, &fleetMockAgentService{})
	found := false
	for _, tool := range reg.tools {
		if tool.Name != "quicbaas_orchestrator_start_job" {
			continue
		}
		found = true
		properties, ok := tool.InputSchema["properties"].(map[string]interface{})
		require.True(t, ok, "properties is %T, want a map", tool.InputSchema["properties"])
		assert.NotContains(t, properties, "workflow_file")
		assert.Contains(t, properties, "workflow_name")
	}
	require.True(t, found, "quicbaas_orchestrator_start_job is not registered")
}
