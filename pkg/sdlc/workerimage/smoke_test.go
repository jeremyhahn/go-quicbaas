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

package workerimage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// fakeSessions is the orchestrator a smoke drives.
type fakeSessions struct {
	spawned     *orchestration.SpawnSessionRequest
	spawnTenant string
	boundTenant string
	strict      bool
	containerID string
	reply       string
	spawnErr    error
	promptErr   error
	closed      []string
}

func (f *fakeSessions) SpawnSession(ctx context.Context, req *orchestration.SpawnSessionRequest) (*orchestration.SpawnSessionResponse, error) {
	f.spawned = req
	f.spawnTenant = orchestration.TenantFromContext(ctx)
	f.boundTenant, _ = tenantctx.Lookup(ctx)
	f.strict = orchestration.HostAffinityStrictFromContext(ctx)
	if f.spawnErr != nil {
		return nil, f.spawnErr
	}
	return &orchestration.SpawnSessionResponse{SessionID: "s1", ContainerID: f.containerID}, nil
}

func (f *fakeSessions) PromptSession(_ context.Context, _ *orchestration.PromptSessionRequest) (*orchestration.PromptSessionResponse, error) {
	if f.promptErr != nil {
		return nil, f.promptErr
	}
	return &orchestration.PromptSessionResponse{Content: []orchestration.ContentBlock{{Type: "text", Text: f.reply}}}, nil
}

func (f *fakeSessions) CloseSession(_ context.Context, req *orchestration.CloseSessionRequest) (*orchestration.CloseSessionResponse, error) {
	f.closed = append(f.closed, req.SessionID)
	return &orchestration.CloseSessionResponse{SessionID: req.SessionID}, nil
}

// fakeProjects serves one project.
type fakeProjects struct{ project *orchestration.Project }

func (f fakeProjects) Get(_ context.Context, _, projectID string) (*orchestration.Project, error) {
	if f.project == nil || f.project.ID != projectID {
		return nil, &sdlc.NotFoundError{Entity: "project", ID: projectID}
	}
	return f.project, nil
}

func smokeFixture(t *testing.T) (*fakeSessions, *SessionSmoker, SmokeRequest, string) {
	t.Helper()
	root := t.TempDir()
	sessions := &fakeSessions{containerID: "c1", reply: "READY"}
	project := &orchestration.Project{ID: "p1"}
	project.Workspace.Path = root
	smoker, err := NewSessionSmoker(sessions, fakeProjects{project: project})
	if err != nil {
		t.Fatalf("NewSessionSmoker: %v", err)
	}
	req := SmokeRequest{TenantID: "acme", ProjectID: "p1", Backend: "claude", Model: "claude-opus-5-5",
		Image: candidateTag, Host: "agent-04"}
	return sessions, smoker, req, root
}

func TestSessionSmoker_SpawnsTheWorkerShapeInTheCandidate(t *testing.T) {
	sessions, smoker, req, root := smokeFixture(t)

	reply, err := smoker.Smoke(t.Context(), req)
	if err != nil || reply != "READY" {
		t.Fatalf("Smoke = %q, %v", reply, err)
	}
	spawned := sessions.spawned
	if spawned.ContainerImage != candidateTag || spawned.SandboxMode != orchestration.SandboxModeRequired {
		t.Fatalf("the smoke must run sandboxed in the candidate: %+v", spawned)
	}
	if !spawned.AccountAuto {
		t.Fatal("the smoke must run on the tenant's pooled credential (AccountAuto)")
	}
	if spawned.Model != "claude-opus-5-5" || spawned.ProjectID != "p1" || sessions.spawnTenant != "acme" {
		t.Fatalf("spawn = %+v tenant = %q", spawned, sessions.spawnTenant)
	}
	if sessions.boundTenant != "acme" {
		t.Fatalf("the spawn must bind the tenant the session services read, got %q", sessions.boundTenant)
	}
	if spawned.HostAffinity != "agent-04" || !sessions.strict {
		t.Fatal("the smoke must be pinned, strictly, to the host holding the candidate")
	}
	if spawned.WorkDir != filepath.Join(root, smokeDir) {
		t.Fatalf("workdir = %q", spawned.WorkDir)
	}
	if _, err := os.Stat(spawned.WorkDir); err != nil {
		t.Fatalf("the smoke workdir was not created: %v", err)
	}
	if len(sessions.closed) != 1 {
		t.Fatal("the session must be closed")
	}
}

func TestSessionSmoker_Failures(t *testing.T) {
	cases := map[string]struct {
		mutate func(*fakeSessions, *SmokeRequest)
		stage  string
		closed int
	}{
		"spawn refused":       {mutate: func(f *fakeSessions, _ *SmokeRequest) { f.spawnErr = errFake }, stage: "spawn"},
		"not containerized":   {mutate: func(f *fakeSessions, _ *SmokeRequest) { f.containerID = "" }, stage: "spawn", closed: 1},
		"prompt fails":        {mutate: func(f *fakeSessions, _ *SmokeRequest) { f.promptErr = errFake }, stage: "prompt", closed: 1},
		"model rejected":      {mutate: func(f *fakeSessions, _ *SmokeRequest) { f.reply = "model not found" }, stage: "answer", closed: 1},
		"project not visible": {mutate: func(_ *fakeSessions, r *SmokeRequest) { r.ProjectID = "other" }, stage: "project"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			sessions, smoker, req, _ := smokeFixture(t)
			tc.mutate(sessions, &req)
			_, err := smoker.Smoke(t.Context(), req)
			var smokeErr *SmokeError
			if !errors.As(err, &smokeErr) || smokeErr.Stage != tc.stage || !errors.Is(err, ErrGate) {
				t.Fatalf("err = %v, want a SmokeError at stage %q", err, tc.stage)
			}
			if len(sessions.closed) != tc.closed {
				t.Fatalf("closed %d sessions, want %d", len(sessions.closed), tc.closed)
			}
		})
	}
}

func TestSessionSmoker_AProjectWithoutAWorkspaceIsRefused(t *testing.T) {
	smoker, err := NewSessionSmoker(&fakeSessions{}, fakeProjects{project: &orchestration.Project{ID: "p1"}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = smoker.Smoke(t.Context(), SmokeRequest{TenantID: "acme", ProjectID: "p1", Backend: "claude"})
	if err == nil || !strings.Contains(err.Error(), "project") {
		t.Fatalf("err = %v", err)
	}
}

func TestNewSessionSmoker_RequiresBothCollaborators(t *testing.T) {
	if _, err := NewSessionSmoker(nil, fakeProjects{}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("err = %v", err)
	}
	if _, err := NewSessionSmoker(&fakeSessions{}, nil); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("err = %v", err)
	}
}
