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
	"os"
	"path/filepath"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/sdlc"
)

// =============================================================================
// The smoke — one real session in the candidate image
// =============================================================================
//
// THE PROBES PROVE THE IMAGE; THE SMOKE PROVES THE PROMOTION. A CLI can print
// its version and still refuse the model a binding names — which is the exact
// failure this refresh exists to end (Claude Code 2.1.278 rejects
// claude-opus-5-5). So every agent the promotion would put into the new image
// is asked one question in it, the way a worker is: through the orchestrator's
// own spawn path, sandboxed in the candidate, with the worker's own argv and
// the tenant's POOLED credential (ApplyWorkerSpawnEnvironment). Nothing about
// the session is special-cased for the gate, so what passes here is what a job
// will run.

// SmokePrompt is the one question a smoke session is asked.
const SmokePrompt = "This is an automated health check of a new worker image. " +
	"Reply with exactly the word " + SmokeAnswer + " and nothing else. Do not run any tools."

// SmokeAnswer is the word a healthy session replies with.
const SmokeAnswer = "READY"

// SmokeRequest is one smoke: an agent, in an image, on a project's behalf.
type SmokeRequest struct {
	// TenantID and ProjectID name the binding the agent will serve; the
	// session is spawned as that tenant, on that project's pooled account and
	// under that project's workspace.
	TenantID  string
	ProjectID string

	// Backend and Model are the agent, exactly as the binding names them.
	Backend string
	Model   string

	// Image is the candidate's reference.
	Image string

	// Host pins the session to the host whose engine holds the candidate.
	// Empty lets the orchestrator place it.
	Host string
}

// Smoker runs one smoke and returns the agent's reply.
type Smoker interface {
	Smoke(ctx context.Context, req SmokeRequest) (string, error)
}

// SessionAPI is the part of the orchestrator a smoke drives;
// orchestration.AgentService satisfies it.
type SessionAPI interface {
	SpawnSession(ctx context.Context, req *orchestration.SpawnSessionRequest) (*orchestration.SpawnSessionResponse, error)
	PromptSession(ctx context.Context, req *orchestration.PromptSessionRequest) (*orchestration.PromptSessionResponse, error)
	CloseSession(ctx context.Context, req *orchestration.CloseSessionRequest) (*orchestration.CloseSessionResponse, error)
}

// SessionSmoker smokes an image through the orchestrator.
type SessionSmoker struct {
	sessions SessionAPI
	projects sdlc.ProjectReader
}

// NewSessionSmoker wires a smoker; both collaborators are required.
func NewSessionSmoker(sessions SessionAPI, projects sdlc.ProjectReader) (*SessionSmoker, error) {
	if sessions == nil {
		return nil, &InvalidConfigError{Field: "sessions", Message: "a smoke is a session the orchestrator spawns"}
	}
	if projects == nil {
		return nil, &InvalidConfigError{Field: "projects", Message: "a smoke runs under its project's workspace"}
	}
	return &SessionSmoker{sessions: sessions, projects: projects}, nil
}

// smokeDir is the directory, under a project's workspace root, a smoke session
// is given as its working directory. It is inside the root the mount policy
// already admits for the tenant, and it holds nothing.
const smokeDir = ".worker-image-smoke"

// Smoke spawns the agent in req.Image, asks SmokePrompt, and closes the
// session whatever happened.
func (s *SessionSmoker) Smoke(ctx context.Context, req SmokeRequest) (reply string, err error) {
	workDir, err := s.workDir(ctx, req)
	if err != nil {
		return "", err
	}
	ctx = tenantContext(ctx, req.TenantID)
	spawn := &orchestration.SpawnSessionRequest{
		Backend:            req.Backend,
		Model:              req.Model,
		ProjectID:          req.ProjectID,
		WorkDir:            workDir,
		ContainerImage:     req.Image,
		SandboxMode:        orchestration.SandboxModeRequired,
		HostAffinity:       req.Host,
		HostAffinityStrict: req.Host != "",
	}
	orchestration.ApplyWorkerSpawnEnvironment(spawn, "")
	if req.Host != "" {
		ctx = orchestration.WithHostAffinityStrict(ctx)
	}
	resp, err := s.sessions.SpawnSession(ctx, spawn)
	if err != nil {
		return "", &SmokeError{Backend: req.Backend, Model: req.Model, Stage: "spawn", Cause: err}
	}
	defer func() {
		if _, closeErr := s.sessions.CloseSession(context.WithoutCancel(ctx),
			&orchestration.CloseSessionRequest{SessionID: resp.SessionID}); closeErr != nil && err == nil {
			err = &SmokeError{Backend: req.Backend, Model: req.Model, Stage: "close", Cause: closeErr}
		}
	}()
	if strings.TrimSpace(resp.ContainerID) == "" {
		// A session that did not run in the candidate proves nothing about it.
		return "", &SmokeError{Backend: req.Backend, Model: req.Model, Stage: "spawn", Cause: errSmokeNotContainerized}
	}
	answer, err := s.sessions.PromptSession(ctx, &orchestration.PromptSessionRequest{SessionID: resp.SessionID, Prompt: SmokePrompt})
	if err != nil {
		return "", &SmokeError{Backend: req.Backend, Model: req.Model, Stage: "prompt", Cause: err}
	}
	reply = sdlc.ReadAgentTurn(answer).Reply
	if !strings.Contains(strings.ToUpper(reply), SmokeAnswer) {
		return reply, &SmokeError{Backend: req.Backend, Model: req.Model, Stage: "answer", Cause: errSmokeWrongAnswer}
	}
	return reply, nil
}

// workDir prepares the smoke's working directory under the project's
// workspace root.
func (s *SessionSmoker) workDir(ctx context.Context, req SmokeRequest) (string, error) {
	project, err := s.projects.Get(tenantContext(ctx, req.TenantID), req.TenantID, req.ProjectID)
	if err != nil {
		return "", &SmokeError{Backend: req.Backend, Model: req.Model, Stage: "project", Cause: err}
	}
	root := strings.TrimSpace(project.Workspace.Path)
	if root == "" {
		return "", &SmokeError{Backend: req.Backend, Model: req.Model, Stage: "project",
			Cause: &sdlc.WorkspaceUnavailableError{ProjectID: req.ProjectID}}
	}
	dir := filepath.Join(root, smokeDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", &SmokeError{Backend: req.Backend, Model: req.Model, Stage: "project", Cause: err}
	}
	return dir, nil
}
