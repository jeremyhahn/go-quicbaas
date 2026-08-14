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
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// =============================================================================
// Mock Logger
// =============================================================================

type agentTestLogger struct{}

func (l *agentTestLogger) Debug(msg string, args ...any)                              {}
func (l *agentTestLogger) Debugf(format string, args ...interface{})                  {}
func (l *agentTestLogger) Info(msg string, args ...any)                               {}
func (l *agentTestLogger) Infof(format string, args ...interface{})                   {}
func (l *agentTestLogger) Warn(msg string, args ...any)                               {}
func (l *agentTestLogger) Warnf(format string, args ...interface{})                   {}
func (l *agentTestLogger) Error(msg string, args ...any)                              {}
func (l *agentTestLogger) Errorf(format string, args ...interface{})                  {}
func (l *agentTestLogger) Fatal(msg string)                                           {}
func (l *agentTestLogger) Fatalf(format string, args ...interface{})                  {}
func (l *agentTestLogger) WithField(key string, value interface{}) interfaces.Logger  { return l }
func (l *agentTestLogger) WithFields(fields map[string]interface{}) interfaces.Logger { return l }
func (l *agentTestLogger) WithError(err error) interfaces.Logger                      { return l }

// Compile-time interface satisfaction check.
var _ interfaces.Logger = (*agentTestLogger)(nil)

// =============================================================================
// Mock Agent Backend
// =============================================================================

type mockAgentBackend struct {
	spawnFn     func(ctx context.Context, req *SpawnRequest) (*Session, error)
	promptFn    func(ctx context.Context, req *PromptRequest) (*PromptResponse, error)
	statusFn    func(ctx context.Context, sessionID string) (*SessionStatus, error)
	closeFn     func(ctx context.Context, sessionID string) error
	caps        BackendCapabilities
	backendType string

	// resolvedHost, when non-empty, makes this mock satisfy the
	// resolvedHostBackend interface so SpawnSession captures it as the
	// session's pinned host. Used to exercise the Prompt/Status/Close
	// re-resolution-pinning regression path.
	resolvedHost string
}

// ResolvedHost implements resolvedHostBackend when resolvedHost is set.
func (m *mockAgentBackend) ResolvedHost() string {
	return m.resolvedHost
}

func (m *mockAgentBackend) Spawn(ctx context.Context, req *SpawnRequest) (*Session, error) {
	if m.spawnFn != nil {
		return m.spawnFn(ctx, req)
	}
	return &Session{
		ID:        "session-123",
		ProjectID: req.ProjectID,
		Backend:   "mock",
		Model:     req.Model,
		Status:    SessionStateActive,
		WorkDir:   req.WorkDir,
		CreatedAt: 1000,
	}, nil
}

func (m *mockAgentBackend) Prompt(ctx context.Context, req *PromptRequest) (*PromptResponse, error) {
	if m.promptFn != nil {
		return m.promptFn(ctx, req)
	}
	return &PromptResponse{
		SessionID:  req.SessionID,
		Content:    []ContentBlock{{Type: "text", Text: "response"}},
		Model:      req.Model,
		StopReason: "end_turn",
	}, nil
}

func (m *mockAgentBackend) Status(ctx context.Context, sessionID string) (*SessionStatus, error) {
	if m.statusFn != nil {
		return m.statusFn(ctx, sessionID)
	}
	return &SessionStatus{
		SessionID:    sessionID,
		State:        SessionStateActive,
		LastActivity: 2000,
		Uptime:       1000,
	}, nil
}

func (m *mockAgentBackend) Close(ctx context.Context, sessionID string) error {
	if m.closeFn != nil {
		return m.closeFn(ctx, sessionID)
	}
	return nil
}

func (m *mockAgentBackend) Capabilities() BackendCapabilities {
	return m.caps
}

func (m *mockAgentBackend) Type() string {
	if m.backendType != "" {
		return m.backendType
	}
	return "mock"
}

func (m *mockAgentBackend) ToolGuidance() string {
	return ""
}

// =============================================================================
// Constructor Tests
// =============================================================================

func TestNewAgentService(t *testing.T) {
	t.Parallel()

	backends := map[string]AgentBackend{
		"mock": &mockAgentBackend{},
	}

	svc, err := NewAgentService(NewInMemoryAgentStore(), backends, &agentTestLogger{})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if svc == nil {
		t.Fatal("expected non-nil service")
	}
}

func TestNewAgentService_NilLogger(t *testing.T) {
	t.Parallel()

	_, err := NewAgentService(NewInMemoryAgentStore(), nil, nil)
	if err == nil {
		t.Fatal("expected error for nil logger")
	}
	if !errors.Is(err, ErrAgentServiceUnavailable) {
		t.Fatalf("expected ErrAgentServiceUnavailable, got %v", err)
	}
}

func TestNewAgentService_NilStore(t *testing.T) {
	t.Parallel()

	_, err := NewAgentService(nil, nil, &agentTestLogger{})
	if err == nil {
		t.Fatal("expected error for nil store")
	}
	if !errors.Is(err, ErrAgentStoreRequired) {
		t.Fatalf("expected ErrAgentStoreRequired, got %v", err)
	}
}

// =============================================================================
// DiscoverProjects Tests
// =============================================================================

func TestAgentService_DiscoverProjects(t *testing.T) {
	t.Parallel()

	// Create temp directory structure
	root := t.TempDir()

	// Create project with CLAUDE.md
	projectDir := filepath.Join(root, "project-alpha")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("failed to create project dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "CLAUDE.md"), []byte("# Alpha"), 0o644); err != nil {
		t.Fatalf("failed to write CLAUDE.md: %v", err)
	}

	// Create project with .claude/agents/
	agentProject := filepath.Join(root, "project-beta")
	agentsDir := filepath.Join(agentProject, ".claude", "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatalf("failed to create agents dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentsDir, "backend.md"), []byte("# Backend Agent"), 0o644); err != nil {
		t.Fatalf("failed to write agent file: %v", err)
	}

	svc, err := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	resp, err := svc.DiscoverProjects(context.Background(), &DiscoverProjectsRequest{
		RootPath: root,
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(resp.Projects) < 2 {
		t.Fatalf("expected at least 2 projects, got %d", len(resp.Projects))
	}

	// Verify we found both types
	foundClaudeMD := false
	foundAgents := false
	for _, p := range resp.Projects {
		if p.HasClaudeMD {
			foundClaudeMD = true
		}
		if p.HasAgents {
			foundAgents = true
		}
	}
	if !foundClaudeMD {
		t.Error("expected to find project with CLAUDE.md")
	}
	if !foundAgents {
		t.Error("expected to find project with .claude/agents/")
	}
}

func TestAgentService_DiscoverProjects_EmptyPath(t *testing.T) {
	t.Parallel()

	svc, _ := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})

	_, err := svc.DiscoverProjects(context.Background(), &DiscoverProjectsRequest{
		RootPath: "",
	})
	if err == nil {
		t.Fatal("expected error for empty root path")
	}
	if !errors.Is(err, ErrRootPathRequired) {
		t.Fatalf("expected ErrRootPathRequired, got %v", err)
	}
}

func TestAgentService_DiscoverProjects_NonexistentPath(t *testing.T) {
	t.Parallel()

	svc, _ := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})

	_, err := svc.DiscoverProjects(context.Background(), &DiscoverProjectsRequest{
		RootPath: "/nonexistent/path/that/does/not/exist",
	})
	if err == nil {
		t.Fatal("expected error for nonexistent path")
	}
	if !errors.Is(err, ErrRootPathRequired) {
		t.Fatalf("expected ErrRootPathRequired, got %v", err)
	}
}

// =============================================================================
// RegisterProject Tests
// =============================================================================

func TestAgentService_RegisterProject(t *testing.T) {
	t.Parallel()

	svc, _ := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})

	resp, err := svc.RegisterProject(context.Background(), &RegisterProjectRequest{
		Name:         "test-project",
		Path:         "/tmp/test-project",
		Dependencies: []string{"dep-1"},
		Labels:       map[string]string{"env": "test"},
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp.ProjectID == "" {
		t.Fatal("expected non-empty project ID")
	}
	if resp.Timestamp == 0 {
		t.Fatal("expected non-zero timestamp")
	}
}

func TestAgentService_RegisterProject_EmptyName(t *testing.T) {
	t.Parallel()

	svc, _ := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})

	_, err := svc.RegisterProject(context.Background(), &RegisterProjectRequest{
		Name: "",
		Path: "/tmp/test",
	})
	if err == nil {
		t.Fatal("expected error for empty name")
	}
	if !errors.Is(err, ErrProjectNameRequired) {
		t.Fatalf("expected ErrProjectNameRequired, got %v", err)
	}
}

func TestAgentService_RegisterProject_EmptyPath(t *testing.T) {
	t.Parallel()

	svc, _ := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})

	_, err := svc.RegisterProject(context.Background(), &RegisterProjectRequest{
		Name: "test",
		Path: "",
	})
	if err == nil {
		t.Fatal("expected error for empty path")
	}
	if !errors.Is(err, ErrProjectPathRequired) {
		t.Fatalf("expected ErrProjectPathRequired, got %v", err)
	}
}

// =============================================================================
// ListProjects Tests
// =============================================================================

func TestAgentService_ListProjects(t *testing.T) {
	t.Parallel()

	svc, _ := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})

	// Register two projects
	_, err := svc.RegisterProject(context.Background(), &RegisterProjectRequest{
		Name: "project-b",
		Path: "/tmp/b",
	})
	if err != nil {
		t.Fatalf("failed to register project b: %v", err)
	}

	_, err = svc.RegisterProject(context.Background(), &RegisterProjectRequest{
		Name: "project-a",
		Path: "/tmp/a",
	})
	if err != nil {
		t.Fatalf("failed to register project a: %v", err)
	}

	resp, err := svc.ListProjects(context.Background(), &ListProjectsRequest{})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(resp.Projects) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(resp.Projects))
	}

	// Verify sorted by name
	if resp.Projects[0].Name != "project-a" {
		t.Errorf("expected first project to be 'project-a', got %q", resp.Projects[0].Name)
	}
}

func TestAgentService_ListProjects_Empty(t *testing.T) {
	t.Parallel()

	svc, _ := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})

	resp, err := svc.ListProjects(context.Background(), &ListProjectsRequest{})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(resp.Projects) != 0 {
		t.Fatalf("expected 0 projects, got %d", len(resp.Projects))
	}
}

// =============================================================================
// SpawnSession Tests
// =============================================================================

func TestAgentService_SpawnSession(t *testing.T) {
	t.Parallel()

	backends := map[string]AgentBackend{
		"mock": &mockAgentBackend{},
	}
	svc, _ := NewAgentService(NewInMemoryAgentStore(), backends, &agentTestLogger{})

	// Register a project first
	regResp, err := svc.RegisterProject(context.Background(), &RegisterProjectRequest{
		Name:         "test-project",
		Path:         "/tmp/test",
		DefaultModel: "llama3",
	})
	if err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	resp, err := svc.SpawnSession(context.Background(), &SpawnSessionRequest{
		Backend:   "mock",
		ProjectID: regResp.ProjectID,
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp.SessionID == "" {
		t.Fatal("expected non-empty session ID")
	}
	if resp.Backend != "mock" {
		t.Errorf("expected backend 'mock', got %q", resp.Backend)
	}
}

func TestAgentService_SpawnSession_BackendNotFound(t *testing.T) {
	t.Parallel()

	svc, _ := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})

	_, err := svc.SpawnSession(context.Background(), &SpawnSessionRequest{
		Backend:   "nonexistent",
		ProjectID: "proj-123",
	})
	if err == nil {
		t.Fatal("expected error for nonexistent backend")
	}
	if !errors.Is(err, ErrBackendNotFound) {
		t.Fatalf("expected ErrBackendNotFound, got %v", err)
	}
}

func TestAgentService_SpawnSession_EmptyBackend(t *testing.T) {
	t.Parallel()

	svc, _ := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})

	_, err := svc.SpawnSession(context.Background(), &SpawnSessionRequest{
		Backend: "",
	})
	if err == nil {
		t.Fatal("expected error for empty backend")
	}
	if !errors.Is(err, ErrBackendRequired) {
		t.Fatalf("expected ErrBackendRequired, got %v", err)
	}
}

// =============================================================================
// PromptSession Tests
// =============================================================================

func TestAgentService_PromptSession(t *testing.T) {
	t.Parallel()

	backends := map[string]AgentBackend{
		"mock": &mockAgentBackend{
			promptFn: func(ctx context.Context, req *PromptRequest) (*PromptResponse, error) {
				return &PromptResponse{
					SessionID: req.SessionID,
					Content: []ContentBlock{
						{Type: "text", Text: "Hello, world!"},
					},
					Model:      "llama3",
					StopReason: "end_turn",
				}, nil
			},
		},
	}
	svc, _ := NewAgentService(NewInMemoryAgentStore(), backends, &agentTestLogger{})

	resp, err := svc.PromptSession(context.Background(), &PromptSessionRequest{
		Backend: "mock",
		Prompt:  "Hello",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(resp.Content) == 0 {
		t.Fatal("expected non-empty content")
	}
	if resp.Content[0].Text != "Hello, world!" {
		t.Errorf("expected 'Hello, world!', got %q", resp.Content[0].Text)
	}
}

func TestAgentService_PromptSession_EmptyPrompt(t *testing.T) {
	t.Parallel()

	svc, _ := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})

	_, err := svc.PromptSession(context.Background(), &PromptSessionRequest{
		Backend: "mock",
		Prompt:  "",
	})
	if err == nil {
		t.Fatal("expected error for empty prompt")
	}
	if !errors.Is(err, ErrPromptRequired) {
		t.Fatalf("expected ErrPromptRequired, got %v", err)
	}
}

func TestAgentService_PromptSession_NoBackendOrSession(t *testing.T) {
	t.Parallel()

	svc, _ := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})

	_, err := svc.PromptSession(context.Background(), &PromptSessionRequest{
		Prompt: "Hello",
	})
	if err == nil {
		t.Fatal("expected error when neither backend nor session_id provided")
	}
	if !errors.Is(err, ErrBackendRequired) {
		t.Fatalf("expected ErrBackendRequired, got %v", err)
	}
}

func TestAgentService_PromptSession_ViaSesionID(t *testing.T) {
	t.Parallel()

	backends := map[string]AgentBackend{
		"mock": &mockAgentBackend{},
	}
	svc, _ := NewAgentService(NewInMemoryAgentStore(), backends, &agentTestLogger{})

	// Spawn a session first
	spawnResp, err := svc.SpawnSession(context.Background(), &SpawnSessionRequest{
		Backend:   "mock",
		ProjectID: "proj-1",
	})
	if err != nil {
		t.Fatalf("failed to spawn session: %v", err)
	}

	// Prompt using session ID only
	resp, err := svc.PromptSession(context.Background(), &PromptSessionRequest{
		SessionID: spawnResp.SessionID,
		Prompt:    "test prompt",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp.Content == nil {
		t.Fatal("expected content in response")
	}
}

// TestAgentService_PromptSession_RecordsConversationEvents is the regression
// guard for the event-sourced session UI: a prompt against a spawned session
// MUST append a KindPromptSubmitted event (the operator's prompt) AND a
// KindAssistantChunk event (the assistant reply) to the session event log, in
// that order, so the SSE/history-driven UI has something to render. Before this
// was wired, the synchronous reply returned over HTTP but the event log stayed
// empty and the UI showed "0 events loaded".
func TestAgentService_PromptSession_RecordsConversationEvents(t *testing.T) {
	t.Parallel()

	backends := map[string]AgentBackend{
		"mock": &mockAgentBackend{
			promptFn: func(ctx context.Context, req *PromptRequest) (*PromptResponse, error) {
				return &PromptResponse{
					SessionID:  req.SessionID,
					Content:    []ContentBlock{{Type: "text", Text: "the reply"}},
					Model:      "llama3",
					StopReason: "end_turn",
				}, nil
			},
		},
	}
	svc, _ := NewAgentService(NewInMemoryAgentStore(), backends, &agentTestLogger{})

	log := NewMemorySessionEventLog()
	svc.(*agentServiceImpl).SetSessionEventLog(log)

	// Events are tenant-scoped; thread a tenant the same way the REST handler
	// does so the append side and the read side agree on the namespace.
	const tenant = "tenant-x"
	ctx := ContextWithTenant(context.Background(), tenant)

	spawnResp, err := svc.SpawnSession(ctx, &SpawnSessionRequest{
		Backend:    "mock",
		Persistent: true,
	})
	if err != nil {
		t.Fatalf("failed to spawn session: %v", err)
	}
	sessionID := spawnResp.SessionID

	resp, err := svc.PromptSession(ctx, &PromptSessionRequest{
		SessionID: sessionID,
		Prompt:    "the question",
	})
	if err != nil {
		t.Fatalf("PromptSession: %v", err)
	}

	events, _, err := log.Range(ctx, tenant, sessionID, 0, 0)
	if err != nil {
		t.Fatalf("Range: %v", err)
	}
	// SpawnSession records a system "session spawned" event (seq 1); the prompt
	// turn then adds the operator prompt (seq 2) and the assistant reply (seq 3).
	if len(events) != 3 {
		t.Fatalf("expected 3 events (spawn + prompt + assistant), got %d: %+v", len(events), events)
	}
	if events[0].Kind != KindSystemMessage {
		t.Fatalf("event[0] kind = %q, want %q (spawn marker)", events[0].Kind, KindSystemMessage)
	}

	prompt := events[1]
	if prompt.Kind != KindPromptSubmitted {
		t.Fatalf("event[1] kind = %q, want %q", prompt.Kind, KindPromptSubmitted)
	}
	if prompt.Prompt == nil || prompt.Prompt.Prompt != "the question" {
		t.Fatalf("event[1] prompt payload = %+v, want prompt %q", prompt.Prompt, "the question")
	}

	reply := events[2]
	if reply.Kind != KindAssistantChunk {
		t.Fatalf("event[2] kind = %q, want %q", reply.Kind, KindAssistantChunk)
	}
	if reply.AssistantChunk == nil || reply.AssistantChunk.Text != "the reply" {
		t.Fatalf("event[2] assistant payload = %+v, want text %q", reply.AssistantChunk, "the reply")
	}
	// The recorded reply must match the synchronous HTTP response so API
	// clients and the event-sourced UI never diverge.
	if reply.AssistantChunk.Text != resp.Content[0].Text {
		t.Fatalf("recorded reply %q != synchronous resp %q", reply.AssistantChunk.Text, resp.Content[0].Text)
	}
	if reply.AssistantChunk.StopReason != "end_turn" {
		t.Fatalf("reply stop_reason = %q, want end_turn", reply.AssistantChunk.StopReason)
	}
}

// TestAgentService_PromptSession_NoConversationEventsWithoutPrompt is the
// negative guard: spawning a session but never prompting it produces only the
// system "session spawned" marker — no operator-prompt or assistant-reply
// events. This proves the conversation events come strictly from PromptSession
// and aren't fabricated on spawn.
func TestAgentService_PromptSession_NoConversationEventsWithoutPrompt(t *testing.T) {
	t.Parallel()

	backends := map[string]AgentBackend{"mock": &mockAgentBackend{}}
	svc, _ := NewAgentService(NewInMemoryAgentStore(), backends, &agentTestLogger{})

	log := NewMemorySessionEventLog()
	svc.(*agentServiceImpl).SetSessionEventLog(log)

	const tenant = "tenant-empty"
	ctx := ContextWithTenant(context.Background(), tenant)

	spawnResp, err := svc.SpawnSession(ctx, &SpawnSessionRequest{Backend: "mock", Persistent: true})
	if err != nil {
		t.Fatalf("failed to spawn session: %v", err)
	}

	events, _, err := log.Range(ctx, tenant, spawnResp.SessionID, 0, 0)
	if err != nil {
		t.Fatalf("Range: %v", err)
	}
	// Only the spawn marker; no prompt/assistant conversation events.
	if len(events) != 1 {
		t.Fatalf("expected exactly 1 spawn-marker event, got %d: %+v", len(events), events)
	}
	if events[0].Kind != KindSystemMessage {
		t.Fatalf("event[0] kind = %q, want %q", events[0].Kind, KindSystemMessage)
	}
	for _, ev := range events {
		if ev.Kind == KindPromptSubmitted || ev.Kind == KindAssistantChunk {
			t.Fatalf("un-prompted session must have no conversation events, found %q", ev.Kind)
		}
	}
}

// =============================================================================
// SessionStatus Tests
// =============================================================================

func TestAgentService_SessionStatus(t *testing.T) {
	t.Parallel()

	backends := map[string]AgentBackend{
		"mock": &mockAgentBackend{},
	}
	svc, _ := NewAgentService(NewInMemoryAgentStore(), backends, &agentTestLogger{})

	// Spawn a session
	spawnResp, err := svc.SpawnSession(context.Background(), &SpawnSessionRequest{
		Backend:   "mock",
		ProjectID: "proj-1",
	})
	if err != nil {
		t.Fatalf("failed to spawn: %v", err)
	}

	resp, err := svc.SessionStatus(context.Background(), &SessionStatusRequest{
		SessionID: spawnResp.SessionID,
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp.State != SessionStateActive {
		t.Errorf("expected active state, got %q", resp.State)
	}
}

func TestAgentService_SessionStatus_EmptyID(t *testing.T) {
	t.Parallel()

	svc, _ := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})

	_, err := svc.SessionStatus(context.Background(), &SessionStatusRequest{
		SessionID: "",
	})
	if err == nil {
		t.Fatal("expected error for empty session ID")
	}
	if !errors.Is(err, ErrSessionIDRequired) {
		t.Fatalf("expected ErrSessionIDRequired, got %v", err)
	}
}

func TestAgentService_SessionStatus_NotFound(t *testing.T) {
	t.Parallel()

	svc, _ := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})

	_, err := svc.SessionStatus(context.Background(), &SessionStatusRequest{
		SessionID: "nonexistent",
	})
	if err == nil {
		t.Fatal("expected error for nonexistent session")
	}
	if !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("expected ErrSessionNotFound, got %v", err)
	}
}

// =============================================================================
// CloseSession Tests
// =============================================================================

func TestAgentService_CloseSession(t *testing.T) {
	t.Parallel()

	backends := map[string]AgentBackend{
		"mock": &mockAgentBackend{},
	}
	svc, _ := NewAgentService(NewInMemoryAgentStore(), backends, &agentTestLogger{})

	// Spawn a session
	spawnResp, err := svc.SpawnSession(context.Background(), &SpawnSessionRequest{
		Backend:   "mock",
		ProjectID: "proj-1",
	})
	if err != nil {
		t.Fatalf("failed to spawn: %v", err)
	}

	resp, err := svc.CloseSession(context.Background(), &CloseSessionRequest{
		SessionID: spawnResp.SessionID,
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp.SessionID != spawnResp.SessionID {
		t.Errorf("expected session ID %q, got %q", spawnResp.SessionID, resp.SessionID)
	}

	// Verify session is removed from in-memory map but still in store as closed
	statusResp, err := svc.SessionStatus(context.Background(), &SessionStatusRequest{
		SessionID: spawnResp.SessionID,
	})
	if err != nil {
		t.Fatalf("expected no error (session should be in store), got %v", err)
	}
	if statusResp.State != SessionStateClosed {
		t.Errorf("expected closed state, got %q", statusResp.State)
	}
}

func TestAgentService_CloseSession_EmptyID(t *testing.T) {
	t.Parallel()

	svc, _ := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})

	_, err := svc.CloseSession(context.Background(), &CloseSessionRequest{
		SessionID: "",
	})
	if err == nil {
		t.Fatal("expected error for empty session ID")
	}
	if !errors.Is(err, ErrSessionIDRequired) {
		t.Fatalf("expected ErrSessionIDRequired, got %v", err)
	}
}

// =============================================================================
// ListSessions Tests
// =============================================================================

func TestAgentService_ListSessions(t *testing.T) {
	t.Parallel()

	callCount := 0
	backends := map[string]AgentBackend{
		"mock": &mockAgentBackend{
			spawnFn: func(ctx context.Context, req *SpawnRequest) (*Session, error) {
				callCount++
				return &Session{
					ID:        "session-" + req.ProjectID,
					ProjectID: req.ProjectID,
					Backend:   "mock",
					Status:    SessionStateActive,
					CreatedAt: int64(callCount * 1000),
				}, nil
			},
		},
	}
	svc, _ := NewAgentService(NewInMemoryAgentStore(), backends, &agentTestLogger{})

	// Spawn two sessions
	_, err := svc.SpawnSession(context.Background(), &SpawnSessionRequest{
		Backend:   "mock",
		ProjectID: "proj-1",
	})
	if err != nil {
		t.Fatalf("failed to spawn session 1: %v", err)
	}

	_, err = svc.SpawnSession(context.Background(), &SpawnSessionRequest{
		Backend:   "mock",
		ProjectID: "proj-2",
	})
	if err != nil {
		t.Fatalf("failed to spawn session 2: %v", err)
	}

	resp, err := svc.ListSessions(context.Background(), &ListSessionsRequest{})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(resp.Sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(resp.Sessions))
	}
}

func TestAgentService_ListSessions_FilterByBackend(t *testing.T) {
	t.Parallel()

	callCount := 0
	sessionIDGen := func(ctx context.Context, req *SpawnRequest) (*Session, error) {
		callCount++
		id := "session-" + req.ProjectID
		return &Session{
			ID:        id,
			ProjectID: req.ProjectID,
			Backend:   "mock",
			Status:    SessionStateActive,
			CreatedAt: int64(callCount * 1000),
		}, nil
	}

	backends := map[string]AgentBackend{
		"alpha": &mockAgentBackend{backendType: "alpha", spawnFn: sessionIDGen},
		"beta":  &mockAgentBackend{backendType: "beta", spawnFn: sessionIDGen},
	}
	svc, _ := NewAgentService(NewInMemoryAgentStore(), backends, &agentTestLogger{})

	_, err := svc.SpawnSession(context.Background(), &SpawnSessionRequest{
		Backend:   "alpha",
		ProjectID: "proj-1",
	})
	if err != nil {
		t.Fatalf("failed to spawn on alpha: %v", err)
	}

	_, err = svc.SpawnSession(context.Background(), &SpawnSessionRequest{
		Backend:   "beta",
		ProjectID: "proj-2",
	})
	if err != nil {
		t.Fatalf("failed to spawn on beta: %v", err)
	}

	resp, err := svc.ListSessions(context.Background(), &ListSessionsRequest{
		Backend: "alpha",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(resp.Sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(resp.Sessions))
	}
	if resp.Sessions[0].Backend != "alpha" {
		t.Errorf("expected backend 'alpha', got %q", resp.Sessions[0].Backend)
	}
}

// =============================================================================
// DecomposeTask Tests
// =============================================================================

func TestAgentService_DecomposeTask(t *testing.T) {
	t.Parallel()

	backends := map[string]AgentBackend{
		"mock": &mockAgentBackend{
			promptFn: func(ctx context.Context, req *PromptRequest) (*PromptResponse, error) {
				jsonResp := `[
					{
						"id": "task-1",
						"project_id": "proj-1",
						"title": "Setup database",
						"description": "Initialize the database schema",
						"priority": 1,
						"dependencies": []
					},
					{
						"id": "task-2",
						"project_id": "proj-1",
						"title": "Build API",
						"description": "Implement REST endpoints",
						"priority": 2,
						"dependencies": ["task-1"]
					}
				]`
				return &PromptResponse{
					Content: []ContentBlock{
						{Type: "text", Text: jsonResp},
					},
				}, nil
			},
		},
	}
	svc, _ := NewAgentService(NewInMemoryAgentStore(), backends, &agentTestLogger{})

	resp, err := svc.DecomposeTask(context.Background(), &DecomposeTaskRequest{
		Backend: "mock",
		Task:    "Build a REST API with database",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(resp.WorkItems) != 2 {
		t.Fatalf("expected 2 work items, got %d", len(resp.WorkItems))
	}
	if resp.WorkItems[0].Title != "Setup database" {
		t.Errorf("expected 'Setup database', got %q", resp.WorkItems[0].Title)
	}
}

func TestAgentService_DecomposeTask_EmptyTask(t *testing.T) {
	t.Parallel()

	svc, _ := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})

	_, err := svc.DecomposeTask(context.Background(), &DecomposeTaskRequest{
		Backend: "mock",
		Task:    "",
	})
	if err == nil {
		t.Fatal("expected error for empty task")
	}
	if !errors.Is(err, ErrTaskRequired) {
		t.Fatalf("expected ErrTaskRequired, got %v", err)
	}
}

func TestAgentService_DecomposeTask_EmptyBackend(t *testing.T) {
	t.Parallel()

	svc, _ := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})

	_, err := svc.DecomposeTask(context.Background(), &DecomposeTaskRequest{
		Backend: "",
		Task:    "some task",
	})
	if err == nil {
		t.Fatal("expected error for empty backend")
	}
	if !errors.Is(err, ErrBackendRequired) {
		t.Fatalf("expected ErrBackendRequired, got %v", err)
	}
}

func TestAgentService_DecomposeTask_NonJSONResponse(t *testing.T) {
	t.Parallel()

	backends := map[string]AgentBackend{
		"mock": &mockAgentBackend{
			promptFn: func(ctx context.Context, req *PromptRequest) (*PromptResponse, error) {
				return &PromptResponse{
					Content: []ContentBlock{
						{Type: "text", Text: "This is not JSON, just plain text response"},
					},
				}, nil
			},
		},
	}
	svc, _ := NewAgentService(NewInMemoryAgentStore(), backends, &agentTestLogger{})

	resp, err := svc.DecomposeTask(context.Background(), &DecomposeTaskRequest{
		Backend: "mock",
		Task:    "Build something",
	})
	if err != nil {
		t.Fatalf("expected no error (should gracefully degrade), got %v", err)
	}
	// Should return raw response as single work item
	if len(resp.WorkItems) != 1 {
		t.Fatalf("expected 1 fallback work item, got %d", len(resp.WorkItems))
	}
	if resp.WorkItems[0].ID != "raw-1" {
		t.Errorf("expected raw-1 ID, got %q", resp.WorkItems[0].ID)
	}
}

// =============================================================================
// ListBackends Tests
// =============================================================================

func TestAgentService_ListBackends(t *testing.T) {
	t.Parallel()

	backends := map[string]AgentBackend{
		"ollama": &mockAgentBackend{
			backendType: "ollama",
			caps:        BackendCapabilities{Streaming: true},
		},
		"claude": &mockAgentBackend{
			backendType: "acp",
			caps:        BackendCapabilities{PersistentSessions: true, ToolUse: true, ACP: true},
		},
	}
	svc, _ := NewAgentService(NewInMemoryAgentStore(), backends, &agentTestLogger{})

	result, err := svc.ListBackends(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 backends, got %d", len(result))
	}

	// Verify sorted by name
	if result[0].Name != "claude" {
		t.Errorf("expected first backend to be 'claude', got %q", result[0].Name)
	}
}

func TestAgentService_ListBackends_Empty(t *testing.T) {
	t.Parallel()

	svc, _ := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})

	result, err := svc.ListBackends(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(result) != 0 {
		t.Fatalf("expected 0 backends, got %d", len(result))
	}
}

// =============================================================================
// RegisterBackend Tests
// =============================================================================

func TestAgentService_RegisterBackend(t *testing.T) {
	t.Parallel()

	svc, _ := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})

	err := svc.RegisterBackend(context.Background(), &RegisterBackendRequest{
		Name:    "new-backend",
		Backend: &mockAgentBackend{backendType: "test"},
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Verify it's listed
	backends, err := svc.ListBackends(context.Background())
	if err != nil {
		t.Fatalf("failed to list backends: %v", err)
	}
	if len(backends) != 1 {
		t.Fatalf("expected 1 backend, got %d", len(backends))
	}
	if backends[0].Name != "new-backend" {
		t.Errorf("expected 'new-backend', got %q", backends[0].Name)
	}
}

func TestAgentService_RegisterBackend_NilBackend(t *testing.T) {
	t.Parallel()

	svc, _ := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})

	err := svc.RegisterBackend(context.Background(), &RegisterBackendRequest{
		Name:    "test",
		Backend: nil,
	})
	if err == nil {
		t.Fatal("expected error for nil backend")
	}
	if !errors.Is(err, ErrBackendNilRequired) {
		t.Fatalf("expected ErrBackendNilRequired, got %v", err)
	}
}

func TestAgentService_RegisterBackend_EmptyName(t *testing.T) {
	t.Parallel()

	svc, _ := NewAgentService(NewInMemoryAgentStore(), nil, &agentTestLogger{})

	err := svc.RegisterBackend(context.Background(), &RegisterBackendRequest{
		Name:    "",
		Backend: &mockAgentBackend{},
	})
	if err == nil {
		t.Fatal("expected error for empty name")
	}
	if !errors.Is(err, ErrBackendNameRequired) {
		t.Fatalf("expected ErrBackendNameRequired, got %v", err)
	}
}

// =============================================================================
// Session Persistence Tests
// =============================================================================

func TestAgentService_SpawnSession_PersistsToStore(t *testing.T) {
	t.Parallel()

	store := NewInMemoryAgentStore()
	backends := map[string]AgentBackend{
		"mock": &mockAgentBackend{},
	}
	svc, err := NewAgentService(store, backends, &agentTestLogger{})
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	resp, err := svc.SpawnSession(context.Background(), &SpawnSessionRequest{
		Backend:   "mock",
		ProjectID: "proj-persist",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Verify the session was persisted to the store
	record, err := store.GetSession(context.Background(), "", resp.SessionID)
	if err != nil {
		t.Fatalf("expected session in store, got error: %v", err)
	}
	if record.Backend != "mock" {
		t.Errorf("expected backend 'mock', got %q", record.Backend)
	}
	if record.ProjectID != "proj-persist" {
		t.Errorf("expected project ID 'proj-persist', got %q", record.ProjectID)
	}
	if record.Status != SessionStateActive {
		t.Errorf("expected active status, got %q", record.Status)
	}
	if record.LastActivity == 0 {
		t.Error("expected non-zero last activity timestamp")
	}
}

func TestAgentService_CloseSession_UpdatesStore(t *testing.T) {
	t.Parallel()

	store := NewInMemoryAgentStore()
	backends := map[string]AgentBackend{
		"mock": &mockAgentBackend{},
	}
	svc, err := NewAgentService(store, backends, &agentTestLogger{})
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	spawnResp, err := svc.SpawnSession(context.Background(), &SpawnSessionRequest{
		Backend:   "mock",
		ProjectID: "proj-close",
	})
	if err != nil {
		t.Fatalf("failed to spawn: %v", err)
	}

	_, err = svc.CloseSession(context.Background(), &CloseSessionRequest{
		SessionID: spawnResp.SessionID,
	})
	if err != nil {
		t.Fatalf("failed to close: %v", err)
	}

	// Verify the store record was updated to closed
	record, err := store.GetSession(context.Background(), "", spawnResp.SessionID)
	if err != nil {
		t.Fatalf("expected session in store, got error: %v", err)
	}
	if record.Status != SessionStateClosed {
		t.Errorf("expected closed status in store, got %q", record.Status)
	}
}

func TestAgentService_ListSessions_MergesStoreData(t *testing.T) {
	t.Parallel()

	store := NewInMemoryAgentStore()

	// Pre-populate store with a historical closed session
	historicalRecord := &SessionRecord{
		SessionID:    "historical-session-1",
		ProjectID:    "proj-old",
		Backend:      "mock",
		Model:        "llama3",
		Status:       SessionStateClosed,
		CreatedAt:    500,
		LastActivity: 900,
	}
	if err := store.PutSession(context.Background(), historicalRecord); err != nil {
		t.Fatalf("failed to store historical session: %v", err)
	}

	backends := map[string]AgentBackend{
		"mock": &mockAgentBackend{},
	}
	svc, err := NewAgentService(store, backends, &agentTestLogger{})
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	// Spawn a live session
	_, err = svc.SpawnSession(context.Background(), &SpawnSessionRequest{
		Backend:   "mock",
		ProjectID: "proj-live",
	})
	if err != nil {
		t.Fatalf("failed to spawn: %v", err)
	}

	// ListSessions should return both live and historical sessions
	resp, err := svc.ListSessions(context.Background(), &ListSessionsRequest{})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(resp.Sessions) != 2 {
		t.Fatalf("expected 2 sessions (1 live + 1 historical), got %d", len(resp.Sessions))
	}

	// Verify sorted by creation time (historical first)
	if resp.Sessions[0].SessionID != "historical-session-1" {
		t.Errorf("expected historical session first, got %q", resp.Sessions[0].SessionID)
	}
	if resp.Sessions[0].State != SessionStateClosed {
		t.Errorf("expected closed state for historical session, got %q", resp.Sessions[0].State)
	}
}

func TestAgentService_SessionStatus_FallsBackToStore(t *testing.T) {
	t.Parallel()

	store := NewInMemoryAgentStore()

	// Store a session record directly (simulating a persisted session)
	record := &SessionRecord{
		SessionID:    "persisted-session-1",
		ProjectID:    "proj-persisted",
		Backend:      "mock",
		Status:       SessionStateClosed,
		CreatedAt:    1000,
		LastActivity: 2000,
	}
	if err := store.PutSession(context.Background(), record); err != nil {
		t.Fatalf("failed to store session: %v", err)
	}

	svc, err := NewAgentService(store, nil, &agentTestLogger{})
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	// Session is not in memory, but exists in store
	resp, err := svc.SessionStatus(context.Background(), &SessionStatusRequest{
		SessionID: "persisted-session-1",
	})
	if err != nil {
		t.Fatalf("expected no error (should fall back to store), got %v", err)
	}
	if resp.State != SessionStateClosed {
		t.Errorf("expected closed state, got %q", resp.State)
	}
	if resp.LastActivity != 2000 {
		t.Errorf("expected last activity 2000, got %d", resp.LastActivity)
	}
}

// TestAgentService_LoadPersistedSessions pins which durable records become
// addressable again at boot. Only a persistent session with a registered
// backend is restored: a closed session is over, a one-shot session held no
// process to miss, and a session whose backend is gone cannot be reached.
func TestAgentService_LoadPersistedSessions(t *testing.T) {
	t.Parallel()

	store := NewInMemoryAgentStore()

	persistentRecord := &SessionRecord{
		SessionID:    "active-session-1",
		TenantID:     "tenant-a",
		ProjectID:    "proj-1",
		Backend:      "mock",
		Model:        "llama3",
		WorkDir:      "/w",
		Persistent:   true,
		BackendLocal: true,
		Status:       SessionStateActive,
		CreatedAt:    1000,
	}
	closedRecord := &SessionRecord{
		SessionID:  "closed-session-1",
		TenantID:   "tenant-a",
		ProjectID:  "proj-2",
		Backend:    "mock",
		Persistent: true,
		Status:     SessionStateClosed,
		CreatedAt:  500,
	}
	noBackendRecord := &SessionRecord{
		SessionID:  "orphan-session-1",
		TenantID:   "tenant-a",
		ProjectID:  "proj-3",
		Backend:    "nonexistent",
		Persistent: true,
		Status:     SessionStateActive,
		CreatedAt:  800,
	}
	oneShotRecord := &SessionRecord{
		SessionID: "oneshot-session-1",
		TenantID:  "tenant-a",
		ProjectID: "proj-4",
		Backend:   "mock",
		Status:    SessionStateActive,
		CreatedAt: 900,
	}

	for _, r := range []*SessionRecord{persistentRecord, closedRecord, noBackendRecord, oneShotRecord} {
		if err := store.PutSession(context.Background(), r); err != nil {
			t.Fatalf("failed to store record: %v", err)
		}
	}

	backends := map[string]AgentBackend{
		"mock": &mockAgentBackend{},
	}
	svc, err := NewAgentService(store, backends, &agentTestLogger{})
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	impl := svc.(*agentServiceImpl)
	if err := impl.LoadPersistedSessions(context.Background()); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	rec, found := impl.liveSessionRecord("active-session-1")
	if !found {
		t.Fatal("expected active-session-1 to be restored in memory")
	}
	if !rec.resumable.Load() {
		t.Error("a restored persistent session must report as resumable, not live")
	}

	if _, found := impl.sessions.Load("closed-session-1"); found {
		t.Error("expected closed-session-1 to NOT be restored in memory")
	}
	if _, found := impl.sessions.Load("orphan-session-1"); found {
		t.Error("expected orphan-session-1 to NOT be restored (backend not available)")
	}
	if _, found := impl.sessions.Load("oneshot-session-1"); found {
		t.Error("expected a non-persistent session to NOT be restored: it held no process to miss")
	}

	oneShot, err := store.GetSession(context.Background(), "tenant-a", "oneshot-session-1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if oneShot.Status != SessionStateClosed {
		t.Errorf("non-persistent session status = %q after restart, want closed", oneShot.Status)
	}
}

func TestAgentService_LoadPersistedSessions_EmptyStore(t *testing.T) {
	t.Parallel()

	store := NewInMemoryAgentStore()
	svc, err := NewAgentService(store, nil, &agentTestLogger{})
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	impl := svc.(*agentServiceImpl)
	if err := impl.LoadPersistedSessions(context.Background()); err != nil {
		t.Fatalf("expected no error for empty store, got %v", err)
	}
}

// =============================================================================
// Helper Function Tests
// =============================================================================

func TestExtractJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "plain JSON array",
			input:    `[{"id":"1"}]`,
			expected: `[{"id":"1"}]`,
		},
		{
			name:     "JSON embedded in text",
			input:    `Here are the tasks:\n[{"id":"1"}]\nDone.`,
			expected: `[{"id":"1"}]`,
		},
		{
			name:     "no JSON",
			input:    "just plain text",
			expected: "",
		},
		{
			name:     "nested brackets",
			input:    `[{"deps":["a","b"]}]`,
			expected: `[{"deps":["a","b"]}]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractJSON(tt.input)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestBuildDecompositionPrompt(t *testing.T) {
	t.Parallel()

	t.Run("with projects", func(t *testing.T) {
		prompt := buildDecompositionPrompt("build API", []string{"proj-1", "proj-2"})
		if prompt == "" {
			t.Fatal("expected non-empty prompt")
		}
		if !containsStr(prompt, "build API") {
			t.Error("expected prompt to contain task description")
		}
		if !containsStr(prompt, "proj-1") {
			t.Error("expected prompt to contain project IDs")
		}
	})

	t.Run("without projects", func(t *testing.T) {
		prompt := buildDecompositionPrompt("build API", nil)
		if prompt == "" {
			t.Fatal("expected non-empty prompt")
		}
		if containsStr(prompt, "Target projects") {
			t.Error("expected prompt to not contain target projects section")
		}
	})
}

// =============================================================================
// StartJob Context Decoupling Tests
// =============================================================================

// TestStartJobSurvivesContextCancellation verifies that a started job continues
// running after the caller's request context is cancelled. The job goroutine
// uses a detached context so it is not tied to the HTTP/gRPC request lifecycle.
func TestStartJobSurvivesContextCancellation(t *testing.T) {
	t.Parallel()

	// blockCh gates the mock backend Prompt call so we can control
	// exactly when the decomposition step completes.
	blockCh := make(chan struct{})

	// promptCalled signals that the backend received the decompose prompt,
	// confirming the job goroutine is still alive after parent ctx cancel.
	promptCalled := make(chan struct{}, 1)

	backend := &mockAgentBackend{
		promptFn: func(ctx context.Context, req *PromptRequest) (*PromptResponse, error) {
			select {
			case promptCalled <- struct{}{}:
			default:
			}
			// Block until test unblocks or context cancels.
			select {
			case <-blockCh:
				return &PromptResponse{
					Content:    []ContentBlock{{Type: "text", Text: `{"work_items":[{"description":"task-1","backend":"mock"}]}`}},
					StopReason: "end_turn",
				}, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
	}

	backends := map[string]AgentBackend{"mock": backend}
	svc, err := NewAgentService(NewInMemoryAgentStore(), backends, &agentTestLogger{})
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	// Use a short-lived context that simulates a CLI timeout.
	parentCtx, parentCancel := context.WithCancel(context.Background())
	tenantCtx := ContextWithTenant(parentCtx, "tenant-test")

	resp, err := svc.StartJob(tenantCtx, &StartJobRequest{
		TenantID:      "tenant-test",
		Description:   "test job survives context cancellation",
		LeadBackend:   "mock",
		MaxIterations: 1,
	})
	if err != nil {
		t.Fatalf("StartJob failed: %v", err)
	}
	if resp.JobID == "" {
		t.Fatal("expected non-empty job ID")
	}

	// Cancel the parent request context, simulating a CLI 30s timeout.
	parentCancel()

	// Wait for the prompt to be called, proving the goroutine is alive
	// even though the parent context is cancelled.
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case <-promptCalled:
		// The job goroutine reached the decompose step after parent cancel.
	case <-timer.C:
		t.Fatal("timed out waiting for job goroutine to call Prompt")
	}

	// Verify the job is NOT failed: it should be in decomposing state
	// because the backend is blocked.
	status, err := svc.GetJobStatus(context.Background(), "tenant-test", resp.JobID)
	if err != nil {
		t.Fatalf("GetJobStatus failed: %v", err)
	}
	if status.State == JobStateFailed {
		t.Fatalf("expected job to NOT be failed, but got state=%s error=%s",
			status.State, status.Error)
	}

	// Unblock the backend to allow cleanup.
	close(blockCh)
}

// TestStartJobCancelJobExplicitly verifies that CancelJob terminates a running
// job. The job context is cancelled, causing the control loop to fail with a
// context error.
func TestStartJobCancelJobExplicitly(t *testing.T) {
	t.Parallel()

	// blockCh keeps the mock backend blocked in decomposition until the
	// test explicitly cancels the job.
	blockCh := make(chan struct{})

	// reachedPrompt signals the backend Prompt was called so the test
	// knows the job is in decomposing state before cancelling.
	reachedPrompt := make(chan struct{}, 1)

	backend := &mockAgentBackend{
		promptFn: func(ctx context.Context, req *PromptRequest) (*PromptResponse, error) {
			select {
			case reachedPrompt <- struct{}{}:
			default:
			}
			// Block until cancelled or unblocked.
			select {
			case <-blockCh:
				return &PromptResponse{
					Content:    []ContentBlock{{Type: "text", Text: `{"work_items":[{"description":"task-1","backend":"mock"}]}`}},
					StopReason: "end_turn",
				}, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
	}

	backends := map[string]AgentBackend{"mock": backend}
	svc, err := NewAgentService(NewInMemoryAgentStore(), backends, &agentTestLogger{})
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	tenantCtx := ContextWithTenant(context.Background(), "tenant-cancel")

	resp, err := svc.StartJob(tenantCtx, &StartJobRequest{
		TenantID:      "tenant-cancel",
		Description:   "test explicit job cancellation",
		LeadBackend:   "mock",
		MaxIterations: 1,
	})
	if err != nil {
		t.Fatalf("StartJob failed: %v", err)
	}
	if resp.JobID == "" {
		t.Fatal("expected non-empty job ID")
	}

	// Wait until the backend Prompt is called so we know the job is running.
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case <-reachedPrompt:
		// Job is now blocked in decompose state.
	case <-timer.C:
		t.Fatal("timed out waiting for job to reach decompose state")
	}

	// Cancel the job explicitly.
	err = svc.CancelJob(tenantCtx, "tenant-cancel", resp.JobID)
	if err != nil {
		t.Fatalf("CancelJob failed: %v", err)
	}

	// The job should be removed from the jobs map. GetJobStatus should
	// return a not-found error since there is no KV store configured.
	_, err = svc.GetJobStatus(context.Background(), "tenant-cancel", resp.JobID)
	if err == nil {
		t.Fatal("expected error after cancellation, got nil")
	}
	var notFound *JobNotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("expected JobNotFoundError, got %T: %v", err, err)
	}

	// Unblock the backend to allow the goroutine to exit cleanly.
	close(blockCh)
}

// containsStr is a helper to check if a string contains a substring.
func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && searchStr(s, substr)
}

// searchStr checks if s contains substr.
func searchStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// =============================================================================
// SubscribeJobEvents Tests
// =============================================================================

// TestSubscribeJobEvents_ReceivesEvents verifies that a subscriber receives
// events published via publishEvent with a matching job_id.
func TestSubscribeJobEvents_ReceivesEvents(t *testing.T) {
	t.Parallel()

	backends := map[string]AgentBackend{"mock": &mockAgentBackend{}}
	svc, err := NewAgentService(NewInMemoryAgentStore(), backends, &agentTestLogger{})
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	impl := svc.(*agentServiceImpl)

	tenantID := "tenant-sub-test"
	jobID := "job-sub-123"
	jobKey := tenantID + "/" + jobID

	// Store a fake running job so SubscribeJobEvents can find it.
	impl.jobs.Store(jobKey, &runningJob{
		loop:   NewControlLoop(impl, &agentTestLogger{}),
		cancel: func() {},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, subErr := svc.SubscribeJobEvents(ctx, tenantID, jobID)
	if subErr != nil {
		t.Fatalf("SubscribeJobEvents failed: %v", subErr)
	}
	if ch == nil {
		t.Fatal("expected non-nil channel")
	}

	// Publish an event with matching job_id.
	impl.publishEvent(ctx, tenantID, "test_event", map[string]string{
		"details": "hello subscriber",
		"job_id":  jobID,
		"state":   string(JobStateExecuting),
	})

	// Read the event from the channel.
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	select {
	case evt := <-ch:
		if evt.JobID != jobID {
			t.Fatalf("expected job ID %q, got %q", jobID, evt.JobID)
		}
		if evt.Type != "test_event" {
			t.Fatalf("expected type %q, got %q", "test_event", evt.Type)
		}
		if evt.Message != "hello subscriber" {
			t.Fatalf("expected message %q, got %q", "hello subscriber", evt.Message)
		}
		if evt.State != JobStateExecuting {
			t.Fatalf("expected state %q, got %q", JobStateExecuting, evt.State)
		}
		if evt.Timestamp == 0 {
			t.Fatal("expected non-zero timestamp")
		}
	case <-timer.C:
		t.Fatal("timed out waiting for event on subscriber channel")
	}

	cancel()
}

// TestSubscribeJobEvents_InvalidJob verifies that subscribing to a non-existent
// job returns a JobNotFoundError.
func TestSubscribeJobEvents_InvalidJob(t *testing.T) {
	t.Parallel()

	backends := map[string]AgentBackend{"mock": &mockAgentBackend{}}
	svc, err := NewAgentService(NewInMemoryAgentStore(), backends, &agentTestLogger{})
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	ctx := context.Background()
	_, subErr := svc.SubscribeJobEvents(ctx, "tenant-x", "nonexistent-job")
	if subErr == nil {
		t.Fatal("expected error for non-existent job, got nil")
	}

	var notFound *JobNotFoundError
	if !errors.As(subErr, &notFound) {
		t.Fatalf("expected JobNotFoundError, got %T: %v", subErr, subErr)
	}
}

// TestSubscribeJobEvents_ContextCancellation verifies that cancelling the
// subscriber context closes the event channel.
func TestSubscribeJobEvents_ContextCancellation(t *testing.T) {
	t.Parallel()

	backends := map[string]AgentBackend{"mock": &mockAgentBackend{}}
	svc, err := NewAgentService(NewInMemoryAgentStore(), backends, &agentTestLogger{})
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	impl := svc.(*agentServiceImpl)

	tenantID := "tenant-ctx-cancel"
	jobID := "job-ctx-cancel"
	jobKey := tenantID + "/" + jobID

	impl.jobs.Store(jobKey, &runningJob{
		loop:   NewControlLoop(impl, &agentTestLogger{}),
		cancel: func() {},
	})

	ctx, cancel := context.WithCancel(context.Background())
	ch, subErr := svc.SubscribeJobEvents(ctx, tenantID, jobID)
	if subErr != nil {
		t.Fatalf("SubscribeJobEvents failed: %v", subErr)
	}

	// Cancel context and verify channel is closed.
	cancel()

	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("expected channel to be closed, got a value")
		}
		// Channel closed as expected.
	case <-timer.C:
		t.Fatal("timed out waiting for channel to close after context cancellation")
	}
}

// TestSubscribeJobEvents_SlowSubscriber verifies that a slow subscriber does
// not block event publishing. Events are dropped for slow subscribers.
func TestSubscribeJobEvents_SlowSubscriber(t *testing.T) {
	t.Parallel()

	backends := map[string]AgentBackend{"mock": &mockAgentBackend{}}
	svc, err := NewAgentService(NewInMemoryAgentStore(), backends, &agentTestLogger{})
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	impl := svc.(*agentServiceImpl)

	tenantID := "tenant-slow"
	jobID := "job-slow"
	jobKey := tenantID + "/" + jobID

	impl.jobs.Store(jobKey, &runningJob{
		loop:   NewControlLoop(impl, &agentTestLogger{}),
		cancel: func() {},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, subErr := svc.SubscribeJobEvents(ctx, tenantID, jobID)
	if subErr != nil {
		t.Fatalf("SubscribeJobEvents failed: %v", subErr)
	}

	// Fill the subscriber buffer (channel capacity is 64) and then publish
	// more events. The extra events should be silently dropped.
	for i := 0; i < 100; i++ {
		impl.publishEvent(ctx, tenantID, "flood_event", map[string]string{
			"job_id": jobID,
		})
	}

	// Drain the channel: we should get at most 64 events (the buffer size).
	received := 0
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				t.Fatal("channel closed unexpectedly")
			}
			received++
		default:
			goto done
		}
	}
done:
	if received == 0 {
		t.Fatal("expected at least one event, got none")
	}
	if received > 64 {
		t.Fatalf("expected at most 64 events (buffer size), got %d", received)
	}
}

// TestSubscribeJobEvents_EmptyTenantID verifies that an empty tenant ID
// returns the appropriate error.
func TestSubscribeJobEvents_EmptyTenantID(t *testing.T) {
	t.Parallel()

	backends := map[string]AgentBackend{"mock": &mockAgentBackend{}}
	svc, err := NewAgentService(NewInMemoryAgentStore(), backends, &agentTestLogger{})
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	ctx := context.Background()
	_, subErr := svc.SubscribeJobEvents(ctx, "", "some-job")
	if subErr == nil {
		t.Fatal("expected error for empty tenant ID, got nil")
	}
	if !errors.Is(subErr, ErrTenantIDRequired) {
		t.Fatalf("expected ErrTenantIDRequired, got %v", subErr)
	}
}

// TestSubscribeJobEvents_EmptyJobID verifies that an empty job ID
// returns the appropriate error.
func TestSubscribeJobEvents_EmptyJobID(t *testing.T) {
	t.Parallel()

	backends := map[string]AgentBackend{"mock": &mockAgentBackend{}}
	svc, err := NewAgentService(NewInMemoryAgentStore(), backends, &agentTestLogger{})
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	ctx := context.Background()
	_, subErr := svc.SubscribeJobEvents(ctx, "tenant-x", "")
	if subErr == nil {
		t.Fatal("expected error for empty job ID, got nil")
	}
	if !errors.Is(subErr, ErrJobNotFound) {
		t.Fatalf("expected ErrJobNotFound, got %v", subErr)
	}
}

// TestSubscribeJobEvents_CleanupOnJobComplete verifies that subscriber channels
// are closed when cleanupJobSubscribers is called (simulating job completion).
func TestSubscribeJobEvents_CleanupOnJobComplete(t *testing.T) {
	t.Parallel()

	backends := map[string]AgentBackend{"mock": &mockAgentBackend{}}
	svc, err := NewAgentService(NewInMemoryAgentStore(), backends, &agentTestLogger{})
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	impl := svc.(*agentServiceImpl)

	tenantID := "tenant-cleanup"
	jobID := "job-cleanup"
	jobKey := tenantID + "/" + jobID

	impl.jobs.Store(jobKey, &runningJob{
		loop:   NewControlLoop(impl, &agentTestLogger{}),
		cancel: func() {},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, subErr := svc.SubscribeJobEvents(ctx, tenantID, jobID)
	if subErr != nil {
		t.Fatalf("SubscribeJobEvents failed: %v", subErr)
	}

	// Simulate job completion cleanup.
	impl.cleanupJobSubscribers(tenantID, jobID)

	// Verify the channel is closed.
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("expected channel to be closed after cleanup, got a value")
		}
		// Channel closed as expected.
	case <-timer.C:
		t.Fatal("timed out waiting for channel to close after cleanup")
	}
}

// TestSubscribeJobEvents_MultipleSubscribers verifies that multiple subscribers
// to the same job each receive published events independently.
func TestSubscribeJobEvents_MultipleSubscribers(t *testing.T) {
	t.Parallel()

	backends := map[string]AgentBackend{"mock": &mockAgentBackend{}}
	svc, err := NewAgentService(NewInMemoryAgentStore(), backends, &agentTestLogger{})
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	impl := svc.(*agentServiceImpl)

	tenantID := "tenant-multi"
	jobID := "job-multi"
	jobKey := tenantID + "/" + jobID

	impl.jobs.Store(jobKey, &runningJob{
		loop:   NewControlLoop(impl, &agentTestLogger{}),
		cancel: func() {},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch1, err1 := svc.SubscribeJobEvents(ctx, tenantID, jobID)
	if err1 != nil {
		t.Fatalf("SubscribeJobEvents (sub1) failed: %v", err1)
	}
	ch2, err2 := svc.SubscribeJobEvents(ctx, tenantID, jobID)
	if err2 != nil {
		t.Fatalf("SubscribeJobEvents (sub2) failed: %v", err2)
	}

	// Publish one event.
	impl.publishEvent(ctx, tenantID, "multi_event", map[string]string{
		"job_id":  jobID,
		"details": "broadcast",
	})

	// Both subscribers should receive the event.
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()

	for i, ch := range []<-chan *JobEvent{ch1, ch2} {
		select {
		case evt := <-ch:
			if evt.Type != "multi_event" {
				t.Fatalf("subscriber %d: expected type %q, got %q", i, "multi_event", evt.Type)
			}
		case <-timer.C:
			t.Fatalf("subscriber %d: timed out waiting for event", i)
		}
	}
}

// =============================================================================
// BackendToolGuidance Tests
// =============================================================================

func TestBackendToolGuidance_KnownBackend(t *testing.T) {
	t.Parallel()

	// Use a real ACPBackend configured for opencode, which returns non-empty guidance.
	backends := map[string]AgentBackend{
		"opencode": NewACPBackend(ACPBackendConfig{
			Command:      "opencode",
			DefaultModel: "some-model",
		}),
	}
	svc, err := NewAgentService(NewInMemoryAgentStore(), backends, &agentTestLogger{})
	if err != nil {
		t.Fatalf("failed to create agent service: %v", err)
	}

	guidance := svc.BackendToolGuidance("opencode")
	if guidance == "" {
		t.Fatal("expected non-empty guidance for opencode backend")
	}
	if !strings.Contains(guidance, "description") {
		t.Error("opencode guidance should mention 'description' field requirement")
	}
}

func TestBackendToolGuidance_UnknownBackend(t *testing.T) {
	t.Parallel()

	backends := map[string]AgentBackend{
		"mock": &mockAgentBackend{},
	}
	svc, err := NewAgentService(NewInMemoryAgentStore(), backends, &agentTestLogger{})
	if err != nil {
		t.Fatalf("failed to create agent service: %v", err)
	}

	guidance := svc.BackendToolGuidance("nonexistent-backend")
	if guidance != "" {
		t.Errorf("expected empty guidance for non-existent backend, got %q", guidance)
	}
}

func TestBackendToolGuidance_OllamaBackend(t *testing.T) {
	t.Parallel()

	backends := map[string]AgentBackend{
		"ollama": NewOllamaBackend(OllamaBackendConfig{
			DefaultModel: "tinyllama",
		}),
	}
	svc, err := NewAgentService(NewInMemoryAgentStore(), backends, &agentTestLogger{})
	if err != nil {
		t.Fatalf("failed to create agent service: %v", err)
	}

	guidance := svc.BackendToolGuidance("ollama")
	if guidance != "" {
		t.Errorf("expected empty guidance for ollama backend, got %q", guidance)
	}
}

// =============================================================================
// BackendDispatcher remote-routing tests (Phase 4)
// =============================================================================

func TestBackendDispatcher_RoutesToRemoteBackendWhenResolverMatches(t *testing.T) {
	t.Parallel()

	local := &mockAgentBackend{backendType: "local"}
	remote := &mockAgentBackend{backendType: "remote"}

	svc, err := NewAgentService(NewInMemoryAgentStore(), map[string]AgentBackend{
		"acp": local,
	}, &agentTestLogger{})
	if err != nil {
		t.Fatalf("NewAgentService: %v", err)
	}
	svcImpl, ok := svc.(*agentServiceImpl)
	if !ok {
		t.Fatalf("expected *agentServiceImpl, got %T", svc)
	}

	var resolverCalls int32
	svcImpl.SetRemoteBackendResolver(func(ctx context.Context, name, projectID string) (AgentBackend, bool) {
		resolverCalls++
		if name == "acp" {
			return remote, true
		}
		return nil, false
	})

	got, err := svcImpl.lookupBackendCtx(context.Background(), "acp", "proj-1")
	if err != nil {
		t.Fatalf("lookupBackendCtx: %v", err)
	}
	if got != remote {
		t.Fatalf("expected remote backend, got %T (type=%q)", got, got.Type())
	}
	if resolverCalls != 1 {
		t.Fatalf("expected resolver called once, got %d", resolverCalls)
	}
}

func TestBackendDispatcher_FallsBackToLocalWhenResolverDeclines(t *testing.T) {
	t.Parallel()

	local := &mockAgentBackend{backendType: "local"}

	svc, err := NewAgentService(NewInMemoryAgentStore(), map[string]AgentBackend{
		"acp": local,
	}, &agentTestLogger{})
	if err != nil {
		t.Fatalf("NewAgentService: %v", err)
	}
	svcImpl := svc.(*agentServiceImpl)

	svcImpl.SetRemoteBackendResolver(func(ctx context.Context, name, projectID string) (AgentBackend, bool) {
		return nil, false
	})

	got, err := svcImpl.lookupBackendCtx(context.Background(), "acp", "")
	if err != nil {
		t.Fatalf("lookupBackendCtx: %v", err)
	}
	if got != local {
		t.Fatalf("expected local backend, got %T", got)
	}
}

func TestBackendDispatcher_NilResolverUsesLocalLookup(t *testing.T) {
	t.Parallel()

	local := &mockAgentBackend{backendType: "local"}
	svc, err := NewAgentService(NewInMemoryAgentStore(), map[string]AgentBackend{
		"acp": local,
	}, &agentTestLogger{})
	if err != nil {
		t.Fatalf("NewAgentService: %v", err)
	}
	svcImpl := svc.(*agentServiceImpl)

	got, err := svcImpl.lookupBackendCtx(context.Background(), "acp", "")
	if err != nil {
		t.Fatalf("lookupBackendCtx: %v", err)
	}
	if got != local {
		t.Fatalf("expected local, got %T", got)
	}

	// Unknown backend with nil resolver should still surface BackendNotFoundError.
	if _, err := svcImpl.lookupBackendCtx(context.Background(), "missing", ""); err == nil {
		t.Fatal("expected not-found error for missing backend")
	} else {
		var nfe *BackendNotFoundError
		if !errors.As(err, &nfe) {
			t.Fatalf("expected *BackendNotFoundError, got %T", err)
		}
	}
}

func TestBackendDispatcher_SpawnSessionRoutesThroughResolver(t *testing.T) {
	t.Parallel()

	local := &mockAgentBackend{backendType: "local"}

	var remoteCalls int32
	remote := &mockAgentBackend{
		backendType: "remote",
		spawnFn: func(ctx context.Context, req *SpawnRequest) (*Session, error) {
			remoteCalls++
			return &Session{
				ID:        "remote-sess",
				ProjectID: req.ProjectID,
				Backend:   "acp",
				Status:    SessionStateActive,
				CreatedAt: time.Now().UnixNano(),
			}, nil
		},
	}

	svc, err := NewAgentService(NewInMemoryAgentStore(), map[string]AgentBackend{
		"acp": local,
	}, &agentTestLogger{})
	if err != nil {
		t.Fatalf("NewAgentService: %v", err)
	}
	svcImpl := svc.(*agentServiceImpl)

	// Register a daemon-kind registration and point the resolver at the
	// remote backend when the names match. This mirrors the production
	// wiring: the orchestrator inspects the AgentRegistry and routes
	// daemon-kind entries through RemoteACPBackend.
	reg := &AgentRegistration{
		ID:       "agent-1",
		TenantID: "tenant-a",
		Backend:  "acp",
		Kind:     AgentKindDaemon,
		Host:     "daemon:9091",
	}
	svcImpl.SetRemoteBackendResolver(func(ctx context.Context, name, projectID string) (AgentBackend, bool) {
		if reg.Backend == name && reg.EffectiveKind() == AgentKindDaemon {
			return remote, true
		}
		return nil, false
	})

	ctx := ContextWithTenant(context.Background(), "tenant-a")
	resp, err := svcImpl.SpawnSession(ctx, &SpawnSessionRequest{
		Backend:   "acp",
		ProjectID: "proj-1",
	})
	if err != nil {
		t.Fatalf("SpawnSession: %v", err)
	}
	if resp.SessionID != "remote-sess" {
		t.Fatalf("session_id: got %q want remote-sess", resp.SessionID)
	}
	if remoteCalls != 1 {
		t.Fatalf("expected remote backend Spawn called once, got %d", remoteCalls)
	}
}

// =============================================================================
// ListBackendModels — remote resolver dispatch and local fallback
// =============================================================================

// modelListerBackend is a test AgentBackend that also implements ModelLister.
type modelListerBackend struct {
	mockAgentBackend
	models    []string
	modelsErr error
}

func (b *modelListerBackend) ListModels(_ context.Context) ([]string, error) {
	if b.modelsErr != nil {
		return nil, b.modelsErr
	}
	return b.models, nil
}

// TestListBackendModels_ProxiesToAgentViaRemoteResolver verifies that when a
// remote backend resolver returns a ModelLister-capable backend,
// ListBackendModels calls ListModels on it and returns the result.
func TestListBackendModels_ProxiesToAgentViaRemoteResolver(t *testing.T) {
	t.Parallel()

	remoteBackend := &modelListerBackend{
		mockAgentBackend: mockAgentBackend{backendType: "remote-ollama"},
		models:           []string{"qwen3:1.7b", "llama3.2:1b"},
	}

	svc, err := NewAgentService(NewInMemoryAgentStore(), map[string]AgentBackend{
		"local-dummy": &mockAgentBackend{},
	}, &agentTestLogger{})
	if err != nil {
		t.Fatalf("NewAgentService: %v", err)
	}
	svcImpl := svc.(*agentServiceImpl)
	svcImpl.SetRemoteBackendResolver(func(ctx context.Context, name, projectID string) (AgentBackend, bool) {
		if name == "ollama" {
			return remoteBackend, true
		}
		return nil, false
	})

	ctx := WithHostAffinity(ContextWithTenant(context.Background(), "tenant-a"), "agent-default-agent-198.51.100.91")
	models, err := svc.ListBackendModels(ctx, "ollama")
	if err != nil {
		t.Fatalf("ListBackendModels error: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("model count: got %d, want 2", len(models))
	}
	if models[0] != "qwen3:1.7b" {
		t.Errorf("models[0]: got %q, want %q", models[0], "qwen3:1.7b")
	}
}

// TestListBackendModels_LocalFallbackForNonListerBackend verifies that when the
// resolved backend does not implement ModelLister, an empty slice is returned
// without error (non-lister backends are valid; they just don't have a catalog).
func TestListBackendModels_LocalFallbackForNonListerBackend(t *testing.T) {
	t.Parallel()

	// mockAgentBackend does NOT implement ModelLister.
	local := &mockAgentBackend{backendType: "claude"}
	svc, err := NewAgentService(NewInMemoryAgentStore(), map[string]AgentBackend{
		"claude": local,
	}, &agentTestLogger{})
	if err != nil {
		t.Fatalf("NewAgentService: %v", err)
	}

	// No remote resolver — falls back to local lookup.
	models, err := svc.ListBackendModels(context.Background(), "claude")
	if err != nil {
		t.Fatalf("ListBackendModels error: %v", err)
	}
	if len(models) != 0 {
		t.Errorf("expected empty slice for non-lister backend, got %v", models)
	}
}
