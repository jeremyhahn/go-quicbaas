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
	"errors"
	"testing"
)

// spawnBackend is a minimal backend that blocks on Prompt to keep jobs alive
// for the duration of spawn tests.
type spawnBackend struct {
	done chan struct{}
}

func newSpawnBackend() *spawnBackend {
	return &spawnBackend{done: make(chan struct{})}
}

func (b *spawnBackend) Spawn(_ context.Context, req *SpawnRequest) (*Session, error) {
	return &Session{ID: "spawn-session", Backend: "spawn"}, nil
}

func (b *spawnBackend) Prompt(_ context.Context, req *PromptRequest) (*PromptResponse, error) {
	// Block until the test signals done.
	<-b.done
	return &PromptResponse{
		Content:    []ContentBlock{{Type: "text", Text: `{"work_items":[]}`}},
		StopReason: "end_turn",
	}, nil
}

func (b *spawnBackend) Status(_ context.Context, _ string) (*SessionStatus, error) {
	return &SessionStatus{SessionID: "spawn-session"}, nil
}

func (b *spawnBackend) Close(_ context.Context, _ string) error {
	close(b.done)
	return nil
}

func (b *spawnBackend) Capabilities() BackendCapabilities {
	return BackendCapabilities{Kind: BackendKindModelServer}
}

func (b *spawnBackend) ToolGuidance() string { return "" }

func (b *spawnBackend) Type() string { return "spawn" }

// newSpawnTestService creates an AgentService with a blocking backend and starts
// a parent job whose Config.LeadAgentID is set to leadAgentID. It returns the
// service, the parent job ID, and a teardown function.
func newSpawnTestService(t *testing.T, tenantID, leadAgentID string) (AgentService, string, func()) {
	t.Helper()

	backend := newSpawnBackend()
	backends := map[string]AgentBackend{"spawn": backend}
	svc, err := NewAgentService(NewInMemoryAgentStore(), backends, &agentTestLogger{})
	if err != nil {
		t.Fatalf("NewAgentService: %v", err)
	}

	ctx := ContextWithTenant(context.Background(), tenantID)
	resp, err := svc.StartJob(ctx, &StartJobRequest{
		TenantID:      tenantID,
		Description:   "parent job for spawn tests",
		LeadBackend:   "spawn",
		LeadAgentID:   leadAgentID,
		MaxIterations: 1,
	})
	if err != nil {
		t.Fatalf("StartJob: %v", err)
	}

	teardown := func() {
		// Allow the background goroutine to finish.
		select {
		case <-backend.done:
		default:
			close(backend.done)
		}
	}

	return svc, resp.JobID, teardown
}

// =============================================================================
// SpawnChildJob Tests
// =============================================================================

// TestSpawnChildJob_LeadSpawnsChild verifies that the registered lead agent can
// successfully spawn a child job and that the parent's ChildJobIDs is updated.
func TestSpawnChildJob_LeadSpawnsChild(t *testing.T) {
	t.Parallel()

	tenantID := "tenant-spawn-1"
	leadAgentID := "agent-lead-001"

	svc, parentJobID, teardown := newSpawnTestService(t, tenantID, leadAgentID)
	defer teardown()

	ctx := ContextWithTenant(context.Background(), tenantID)

	// Lead agent spawns a child job.
	resp, err := svc.SpawnChildJob(ctx, &SpawnChildJobRequest{
		ParentJobID:   parentJobID,
		CallerAgentID: leadAgentID,
		Tasks: []*TaskSpec{
			{Title: "sub-task", Description: "do the sub-work"},
		},
	})

	if err != nil {
		t.Fatalf("SpawnChildJob: unexpected error: %v", err)
	}
	if resp == nil || resp.ChildJobID == "" {
		t.Fatal("expected non-empty ChildJobID")
	}
	if resp.ChildJobID == parentJobID {
		t.Error("child job ID must differ from parent job ID")
	}

	// appendChildJobID is called synchronously before SpawnChildJob returns,
	// so the parent's ChildJobIDs is already updated.
	parentStatus, err := svc.GetJobStatus(ctx, tenantID, parentJobID)
	if err != nil {
		t.Fatalf("GetJobStatus: %v", err)
	}
	if len(parentStatus.ChildJobIDs) == 0 {
		t.Error("expected parent ChildJobIDs to contain child job ID")
	} else if parentStatus.ChildJobIDs[0] != resp.ChildJobID {
		t.Errorf("parent ChildJobIDs[0] = %q, want %q", parentStatus.ChildJobIDs[0], resp.ChildJobID)
	}
}

// TestSpawnChildJob_WorkerForbidden verifies that a non-lead agent receives a
// NotLeadOfParentError (HTTP 403 / gRPC PermissionDenied) when attempting to
// spawn a child job.
func TestSpawnChildJob_WorkerForbidden(t *testing.T) {
	t.Parallel()

	tenantID := "tenant-spawn-2"
	leadAgentID := "agent-lead-002"
	workerAgentID := "agent-worker-002"

	svc, parentJobID, teardown := newSpawnTestService(t, tenantID, leadAgentID)
	defer teardown()

	ctx := ContextWithTenant(context.Background(), tenantID)

	_, err := svc.SpawnChildJob(ctx, &SpawnChildJobRequest{
		ParentJobID:   parentJobID,
		CallerAgentID: workerAgentID,
		Tasks: []*TaskSpec{
			{Title: "unauthorized spawn", Description: "should be rejected"},
		},
	})

	if err == nil {
		t.Fatal("expected NotLeadOfParentError, got nil")
	}

	var notLead *NotLeadOfParentError
	if !errors.As(err, &notLead) {
		t.Fatalf("expected *NotLeadOfParentError, got %T: %v", err, err)
	}
	if notLead.ParentJobID != parentJobID {
		t.Errorf("NotLeadOfParentError.ParentJobID = %q, want %q", notLead.ParentJobID, parentJobID)
	}
	if notLead.CallerAgentID != workerAgentID {
		t.Errorf("NotLeadOfParentError.CallerAgentID = %q, want %q", notLead.CallerAgentID, workerAgentID)
	}
	if notLead.LeadAgentID != leadAgentID {
		t.Errorf("NotLeadOfParentError.LeadAgentID = %q, want %q", notLead.LeadAgentID, leadAgentID)
	}
}

// TestSpawnChildJob_ParentNotFound verifies that spawning with an unknown
// parent job ID returns a ParentJobNotFoundError.
func TestSpawnChildJob_ParentNotFound(t *testing.T) {
	t.Parallel()

	tenantID := "tenant-spawn-3"
	svc, _, teardown := newSpawnTestService(t, tenantID, "agent-lead-003")
	defer teardown()

	ctx := ContextWithTenant(context.Background(), tenantID)

	_, err := svc.SpawnChildJob(ctx, &SpawnChildJobRequest{
		ParentJobID:   "nonexistent-job-id",
		CallerAgentID: "agent-lead-003",
	})

	if err == nil {
		t.Fatal("expected ParentJobNotFoundError, got nil")
	}
	var notFound *ParentJobNotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("expected *ParentJobNotFoundError, got %T: %v", err, err)
	}
}

// TestSpawnChildJob_NilRequest verifies that a nil request is rejected with a
// descriptive AgentServiceError.
func TestSpawnChildJob_NilRequest(t *testing.T) {
	t.Parallel()

	svc, err := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})
	if err != nil {
		t.Fatalf("NewAgentService: %v", err)
	}

	ctx := ContextWithTenant(context.Background(), "tenant-nil")
	_, err = svc.SpawnChildJob(ctx, nil)
	if err == nil {
		t.Fatal("expected error for nil request")
	}
}

// TestSpawnChildJob_MissingParentJobID verifies that an empty ParentJobID is
// rejected with a descriptive error.
func TestSpawnChildJob_MissingParentJobID(t *testing.T) {
	t.Parallel()

	svc, err := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})
	if err != nil {
		t.Fatalf("NewAgentService: %v", err)
	}

	ctx := ContextWithTenant(context.Background(), "tenant-missing-parent")
	_, err = svc.SpawnChildJob(ctx, &SpawnChildJobRequest{
		ParentJobID:   "",
		CallerAgentID: "agent-001",
	})
	if err == nil {
		t.Fatal("expected error for empty ParentJobID")
	}
}

// TestSpawnChildJob_ChildJobIDUpdatesParent verifies ChildJobIDs atomicity:
// multiple sequential child spawns all appear in the parent's ChildJobIDs list.
func TestSpawnChildJob_ChildJobIDUpdatesParent(t *testing.T) {
	t.Parallel()

	tenantID := "tenant-spawn-atomic"
	leadAgentID := "agent-lead-atomic"

	svc, parentJobID, teardown := newSpawnTestService(t, tenantID, leadAgentID)
	defer teardown()

	ctx := ContextWithTenant(context.Background(), tenantID)

	const numChildren = 3
	childIDs := make([]string, 0, numChildren)

	for i := 0; i < numChildren; i++ {
		resp, err := svc.SpawnChildJob(ctx, &SpawnChildJobRequest{
			ParentJobID:   parentJobID,
			CallerAgentID: leadAgentID,
			Tasks: []*TaskSpec{
				{Title: "child", Description: "child task"},
			},
		})
		if err != nil {
			t.Fatalf("SpawnChildJob[%d]: %v", i, err)
		}
		childIDs = append(childIDs, resp.ChildJobID)
	}

	// After all spawns complete, verify the parent status reflects all children.
	// appendChildJobID is synchronous within SpawnChildJob so the count is
	// guaranteed by the time the call returns.
	parentStatus, err := svc.GetJobStatus(ctx, tenantID, parentJobID)
	if err != nil {
		t.Fatalf("GetJobStatus: %v", err)
	}
	if len(parentStatus.ChildJobIDs) != numChildren {
		t.Errorf("parent ChildJobIDs len = %d, want %d", len(parentStatus.ChildJobIDs), numChildren)
	}
	childSet := make(map[string]bool, numChildren)
	for _, id := range parentStatus.ChildJobIDs {
		childSet[id] = true
	}
	for _, id := range childIDs {
		if !childSet[id] {
			t.Errorf("child ID %q not found in parent ChildJobIDs", id)
		}
	}
}
