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

package orchestration

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

const startJobTestWorkflowYAML = `name: file-workflow
description: "loaded from a file"
mode: autonomous
agents:
  - role: lead
    backend: mock
    model: mock-model
settings:
  max_iterations: 1
  poll_interval: 5s
`

// cancelAllJobs immediately cancels every running job so the spawned control
// loop goroutine exits without doing real work.
func cancelAllJobs(svc *agentServiceImpl) {
	svc.jobs.Range(func(_, v interface{}) bool {
		if job, ok := v.(*runningJob); ok && job.cancel != nil {
			job.cancel()
		}
		return true
	})
}

func newMockBackendService(t *testing.T) *agentServiceImpl {
	t.Helper()
	backends := map[string]AgentBackend{"mock": &mockAgentBackend{backendType: "mock"}}
	svc, err := NewAgentService(NewInMemoryAgentStore(), backends, &agentTestLogger{})
	if err != nil {
		t.Fatalf("NewAgentService: %v", err)
	}
	return svc.(*agentServiceImpl)
}

func TestStartJob_LoadsWorkflowFromFile(t *testing.T) {
	svc := newMockBackendService(t)
	svc.SetWorkflowLoader(NewWorkflowLoader())

	dir := t.TempDir()
	path := filepath.Join(dir, "wf.yaml")
	if err := os.WriteFile(path, []byte(startJobTestWorkflowYAML), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	resp, err := svc.StartJob(context.Background(), &StartJobRequest{
		TenantID:     "tenant-1",
		Description:  "build via file workflow",
		WorkflowFile: path,
	})
	if err != nil {
		t.Fatalf("StartJob: %v", err)
	}
	defer cancelAllJobs(svc)

	if resp.JobID == "" {
		t.Fatal("expected non-empty job ID")
	}
	// The workflow file's lead role derives the lead backend/model.
	got, err := svc.GetWorkflow(ContextWithTenant(context.Background(), "tenant-1"), "file-workflow")
	if err != nil {
		t.Fatalf("workflow should be registered from file: %v", err)
	}
	if got.Agents[0].Backend != "mock" {
		t.Fatalf("expected lead backend mock, got %q", got.Agents[0].Backend)
	}
}

func TestStartJob_WorkflowNameNotFound(t *testing.T) {
	svc := newMockBackendService(t)
	svc.SetWorkflowLoader(NewWorkflowLoader())

	_, err := svc.StartJob(context.Background(), &StartJobRequest{
		TenantID:     "tenant-1",
		Description:  "x",
		WorkflowName: "does-not-exist",
	})
	defer cancelAllJobs(svc)
	if err == nil {
		t.Fatal("expected error for unknown workflow name")
	}
}

func TestStartJob_AutoMatchesWorkflowByLeadBackend(t *testing.T) {
	svc := newMockBackendService(t)
	loader := NewWorkflowLoader()
	svc.SetWorkflowLoader(loader)

	// Register a workflow whose lead backend matches the request's LeadBackend.
	def := &WorkflowDefinition{
		Name: "matched",
		Mode: WorkflowModeAutonomous,
		Agents: []AgentRoleSpec{
			{Role: "lead", Backend: "mock", Model: "mock-model"},
			{Role: "worker", Backend: "mock", Model: "mock-model"},
		},
	}
	if err := loader.Register("tenant-1", def); err != nil {
		t.Fatalf("Register: %v", err)
	}

	// No WorkflowName, but LeadBackend matches → auto-match branch runs.
	resp, err := svc.StartJob(context.Background(), &StartJobRequest{
		TenantID:      "tenant-1",
		Description:   "auto match",
		routedBackend: "mock",
	})
	if err != nil {
		t.Fatalf("StartJob: %v", err)
	}
	defer cancelAllJobs(svc)
	if resp.JobID == "" {
		t.Fatal("expected non-empty job ID")
	}
}

// =============================================================================
// CloseSession — session-log terminal-event path
// =============================================================================

func TestCloseSession_AppendsSessionLogTerminalEvent(t *testing.T) {
	svc := newMockBackendService(t)
	log := NewMemorySessionEventLog()
	svc.SetSessionEventLog(log)

	ctx := ContextWithTenant(context.Background(), "tenant-1")
	spawn, err := svc.SpawnSession(ctx, &SpawnSessionRequest{Backend: "mock", ProjectID: "p1"})
	if err != nil {
		t.Fatalf("SpawnSession: %v", err)
	}

	if _, err := svc.CloseSession(ctx, &CloseSessionRequest{SessionID: spawn.SessionID}); err != nil {
		t.Fatalf("CloseSession: %v", err)
	}

	// The terminal SessionClosed event must have been appended.
	events, _, err := log.Range(ctx, "tenant-1", spawn.SessionID, 0, 0)
	if err != nil {
		t.Fatalf("Range: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("expected a terminal session-closed event to be appended")
	}
	last := events[len(events)-1]
	if last.Kind != KindSessionClosed {
		t.Fatalf("expected last event KindSessionClosed, got %q", last.Kind)
	}
}

// =============================================================================
// SpawnChildJob — empty-tenant branch
// =============================================================================

func TestSpawnChildJob_EmptyTenant(t *testing.T) {
	svc := newMockBackendService(t)
	// No tenant in context → ErrTenantIDRequired.
	_, err := svc.SpawnChildJob(context.Background(), &SpawnChildJobRequest{
		ParentJobID:   "parent-1",
		CallerAgentID: "agent-1",
	})
	if err == nil {
		t.Fatal("expected error for missing tenant")
	}
}

// =============================================================================
// CodexExecBackend.Prompt — model falls back to the session's model
// =============================================================================

func TestCodexExecBackend_PromptUsesSessionModel(t *testing.T) {
	ctx := context.Background()
	b := NewCodexExecBackend(CodexExecBackendConfig{Command: fakeCodex(t, codexTurnJSONL).path})

	// Spawn with an explicit model; Prompt omits Model so the session model wins.
	sess, err := b.Spawn(ctx, &SpawnRequest{WorkDir: t.TempDir(), Model: "session-model"})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	resp, err := b.Prompt(ctx, &PromptRequest{SessionID: sess.ID, Prompt: "go"})
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	if resp.Model != "session-model" {
		t.Fatalf("expected session-model, got %q", resp.Model)
	}
}
