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
	"strings"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
)

// =============================================================================
// Harness
// =============================================================================

// resumeHarness is one daemon lifetime: a service bound to a shared KV store,
// so a "restart" is simply a second harness built over the same store.
type resumeHarness struct {
	service *agentServiceImpl
	backend *recordingResumeBackend
	store   AgentStore
	log     SessionEventLog
	kv      interfaces.KVStore
}

// newResumeHarness boots a service over kv. Passing the kv from an earlier
// harness simulates a daemon restart: the process-local registry starts empty
// while every durable record survives.
func newResumeHarness(t *testing.T, kv interfaces.KVStore) *resumeHarness {
	t.Helper()
	if kv == nil {
		kv = kvstore.NewMemory()
	}

	backend := &recordingResumeBackend{}
	store := NewAgentStore(kv)
	svc, err := NewAgentService(store, map[string]AgentBackend{"mock": backend}, &agentTestLogger{})
	if err != nil {
		t.Fatalf("NewAgentService: %v", err)
	}
	impl, ok := svc.(*agentServiceImpl)
	if !ok {
		t.Fatalf("NewAgentService returned %T, want *agentServiceImpl", svc)
	}

	log, err := NewKVSessionEventLog(KVSessionEventLogConfig{KVStore: kv})
	if err != nil {
		t.Fatalf("NewKVSessionEventLog: %v", err)
	}
	impl.SetSessionEventLog(log)

	return &resumeHarness{service: impl, backend: backend, store: store, log: log, kv: kv}
}

// restart returns a fresh harness over the same durable store and runs the
// boot-time restoration, exactly as the daemon does at startup.
func (h *resumeHarness) restart(t *testing.T) *resumeHarness {
	t.Helper()
	next := newResumeHarness(t, h.kv)
	if err := next.service.LoadPersistedSessions(context.Background()); err != nil {
		t.Fatalf("LoadPersistedSessions: %v", err)
	}
	return next
}

// recordingResumeBackend is an AgentBackend that also implements SessionResumer
// and records what it was asked to reconstitute, so a test can assert that the
// prior conversation actually reached the replacement agent.
type recordingResumeBackend struct {
	resumeCalls  []*ResumeRequest
	resumeErr    error
	resumeMode   ResumeMode
	promptCalls  []string
	liveSessions map[string]bool
}

func (b *recordingResumeBackend) Spawn(_ context.Context, req *SpawnRequest) (*Session, error) {
	if b.liveSessions == nil {
		b.liveSessions = map[string]bool{}
	}
	// Session IDs are key segments, so the separator is not legal in one.
	id := "sess" + strings.ReplaceAll(req.WorkDir, "/", "-")
	b.liveSessions[id] = true
	return &Session{
		ID:        id,
		ProjectID: req.ProjectID,
		Backend:   "mock",
		Model:     req.Model,
		Status:    SessionStateActive,
		WorkDir:   req.WorkDir,
		CreatedAt: time.Now().UnixNano(),
	}, nil
}

func (b *recordingResumeBackend) Prompt(_ context.Context, req *PromptRequest) (*PromptResponse, error) {
	if req.SessionID != "" && !b.liveSessions[req.SessionID] {
		return nil, &SessionNotFoundError{SessionID: req.SessionID}
	}
	b.promptCalls = append(b.promptCalls, req.Prompt)
	return &PromptResponse{
		SessionID:  req.SessionID,
		Content:    []ContentBlock{{Type: "text", Text: "answer to " + req.Prompt}},
		StopReason: StopReasonEndTurn,
	}, nil
}

func (b *recordingResumeBackend) Status(_ context.Context, sessionID string) (*SessionStatus, error) {
	if !b.liveSessions[sessionID] {
		return nil, &SessionNotFoundError{SessionID: sessionID}
	}
	return &SessionStatus{SessionID: sessionID, State: SessionStateActive}, nil
}

func (b *recordingResumeBackend) Close(_ context.Context, sessionID string) error {
	delete(b.liveSessions, sessionID)
	return nil
}

func (b *recordingResumeBackend) Capabilities() BackendCapabilities {
	return BackendCapabilities{Kind: BackendKindAgent, PersistentSessions: true}
}

func (b *recordingResumeBackend) Type() string         { return "mock" }
func (b *recordingResumeBackend) ToolGuidance() string { return "" }

func (b *recordingResumeBackend) Resume(_ context.Context, req *ResumeRequest) (*ResumeResult, error) {
	b.resumeCalls = append(b.resumeCalls, req)
	if b.resumeErr != nil {
		return nil, b.resumeErr
	}
	if b.liveSessions == nil {
		b.liveSessions = map[string]bool{}
	}
	b.liveSessions[req.SessionID] = true

	mode := b.resumeMode
	if mode == ResumeModeNone {
		mode = ResumeModeReplay
	}
	result := &ResumeResult{
		Session: &Session{ID: req.SessionID, Backend: "mock", Status: SessionStateActive},
		Mode:    mode,
	}
	if req.Transcript != nil {
		result.ReplayedTurns = len(req.Transcript.Turns)
		result.OmittedTurns = req.Transcript.Omitted
	}
	return result, nil
}

var _ AgentBackend = (*recordingResumeBackend)(nil)
var _ SessionResumer = (*recordingResumeBackend)(nil)

// spawnAndConverse spawns a session for tenant and drives one prompt/response
// exchange through it, so the durable transcript has content to replay.
func (h *resumeHarness) spawnAndConverse(t *testing.T, tenant, workDir string, persistent bool) string {
	t.Helper()
	ctx := ContextWithTenant(context.Background(), tenant)

	spawn, err := h.service.SpawnSession(ctx, &SpawnSessionRequest{
		Backend:      "mock",
		WorkDir:      workDir,
		SystemPrompt: "you are a careful engineer",
		Persistent:   persistent,
	})
	if err != nil {
		t.Fatalf("SpawnSession: %v", err)
	}
	if _, err := h.service.PromptSession(ctx, &PromptSessionRequest{
		SessionID: spawn.SessionID,
		Prompt:    "what is the capital of France?",
	}); err != nil {
		t.Fatalf("PromptSession: %v", err)
	}
	return spawn.SessionID
}

// =============================================================================
// Durable record: round trip, restart survival, tenant isolation
// =============================================================================

// TestSessionRecord_RoundTrip asserts that every field needed to reconstitute a
// session survives a write/read cycle. A field lost here is a session that
// resumes under different parameters than it was created with.
func TestSessionRecord_RoundTrip(t *testing.T) {
	t.Parallel()

	store := NewInMemoryAgentStore()
	want := &SessionRecord{
		SessionID:      "sess-round-trip",
		TenantID:       "tenant-a",
		ProjectID:      "proj-1",
		Backend:        "claude",
		Model:          "claude-opus-5",
		SystemPrompt:   "be concise",
		WorkDir:        "/srv/work",
		Env:            map[string]string{"FOO": "bar"},
		ExtraArgs:      []string{"--dangerously-skip-permissions"},
		MCPServers:     []MCPServerConfig{{Name: QuicBaaSMCPPresetName}},
		SandboxMode:    SandboxModeNone,
		ContainerImage: "img:1",
		HostAffinity:   "host-7",
		BackendLocal:   true,
		Persistent:     true,
		Status:         SessionStateActive,
		CreatedAt:      1000,
		LastActivity:   2000,
	}
	if err := store.PutSession(context.Background(), want); err != nil {
		t.Fatalf("PutSession: %v", err)
	}

	got, err := store.GetSession(context.Background(), "tenant-a", "sess-round-trip")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.WorkDir != want.WorkDir || got.SystemPrompt != want.SystemPrompt {
		t.Errorf("spawn configuration lost: got work_dir=%q system_prompt=%q", got.WorkDir, got.SystemPrompt)
	}
	if !got.Persistent || !got.BackendLocal {
		t.Errorf("lifecycle flags lost: persistent=%v backend_local=%v", got.Persistent, got.BackendLocal)
	}
	if got.Env["FOO"] != "bar" || len(got.ExtraArgs) != 1 || len(got.MCPServers) != 1 {
		t.Errorf("agent invocation state lost: env=%v extra_args=%v mcp=%v", got.Env, got.ExtraArgs, got.MCPServers)
	}
	if got.HostAffinity != "host-7" || got.Model != "claude-opus-5" {
		t.Errorf("placement/model lost: host=%q model=%q", got.HostAffinity, got.Model)
	}
	if !got.Resumable() {
		t.Error("Resumable() = false for a persistent, local, open session")
	}
}

// TestSessionRecord_PutSessionRejectsInvalidInput asserts the store refuses
// records it cannot key, rather than writing them under a malformed key where
// they would be unreachable or collide across tenants.
func TestSessionRecord_PutSessionRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	store := NewInMemoryAgentStore()

	if err := store.PutSession(context.Background(), nil); !errors.Is(err, ErrAgentStoreInvalidInput) {
		t.Errorf("PutSession(nil) error = %v, want ErrAgentStoreInvalidInput", err)
	}

	err := store.PutSession(context.Background(), &SessionRecord{TenantID: "t", SessionID: ""})
	if !errors.Is(err, ErrAgentStoreInvalidInput) {
		t.Errorf("PutSession(empty session id) error = %v, want ErrAgentStoreInvalidInput", err)
	}

	// A separator inside an identifier would let a crafted ID escape its
	// tenant subtree; it must be rejected, not encoded.
	err = store.PutSession(context.Background(), &SessionRecord{TenantID: "t", SessionID: "a/b"})
	if !errors.Is(err, ErrAgentStoreInvalidInput) || !errors.Is(err, ErrInvalidKeySegment) {
		t.Errorf("PutSession(separator in session id) error = %v, want invalid key segment", err)
	}
}

// TestSessionRecord_TenantIsolation asserts one tenant can neither read, list,
// mutate nor delete another tenant's session records.
func TestSessionRecord_TenantIsolation(t *testing.T) {
	t.Parallel()

	store := NewInMemoryAgentStore()
	ctx := context.Background()
	for _, tenant := range []string{"tenant-a", "tenant-b"} {
		if err := store.PutSession(ctx, &SessionRecord{
			SessionID:  "sess-" + tenant,
			TenantID:   tenant,
			Backend:    "claude",
			WorkDir:    "/w",
			Persistent: true,
			Status:     SessionStateActive,
		}); err != nil {
			t.Fatalf("PutSession(%s): %v", tenant, err)
		}
	}

	if _, err := store.GetSession(ctx, "tenant-b", "sess-tenant-a"); !errors.Is(err, ErrAgentStoreNotFound) {
		t.Errorf("cross-tenant GetSession error = %v, want not found", err)
	}
	if err := store.DeleteSession(ctx, "tenant-b", "sess-tenant-a"); !errors.Is(err, ErrAgentStoreNotFound) {
		t.Errorf("cross-tenant DeleteSession error = %v, want not found", err)
	}
	if err := store.UpdateSessionStatus(ctx, "tenant-b", "sess-tenant-a", SessionStateClosed); !errors.Is(err, ErrAgentStoreNotFound) {
		t.Errorf("cross-tenant UpdateSessionStatus error = %v, want not found", err)
	}

	listed, err := store.ListSessions(ctx, "tenant-b")
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(listed) != 1 || listed[0].SessionID != "sess-tenant-b" {
		t.Fatalf("ListSessions(tenant-b) = %v, want only tenant-b's own session", listed)
	}

	// The owner still sees its own record untouched by the denied writes.
	own, err := store.GetSession(ctx, "tenant-a", "sess-tenant-a")
	if err != nil {
		t.Fatalf("GetSession(owner): %v", err)
	}
	if own.Status != SessionStateActive {
		t.Errorf("owner record status = %q, want it unchanged by the cross-tenant write", own.Status)
	}

	// Boot-time recovery is the one scan that must see every tenant.
	all, err := store.ListAllSessions(ctx)
	if err != nil {
		t.Fatalf("ListAllSessions: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("ListAllSessions returned %d records, want 2", len(all))
	}
}

// TestSessionRecord_SurvivesRestart asserts a spawned session's durable record
// is readable by a service built fresh over the same store — the property the
// whole feature rests on.
func TestSessionRecord_SurvivesRestart(t *testing.T) {
	t.Parallel()

	first := newResumeHarness(t, nil)
	sessionID := first.spawnAndConverse(t, "tenant-a", "/work/repo", true)

	second := first.restart(t)
	record, err := second.store.GetSession(context.Background(), "tenant-a", sessionID)
	if err != nil {
		t.Fatalf("session record did not survive restart: %v", err)
	}
	if record.WorkDir != "/work/repo" || record.SystemPrompt != "you are a careful engineer" {
		t.Fatalf("spawn configuration not reconstitutable: %+v", record)
	}
	if !record.Persistent {
		t.Fatal("persistent flag lost across restart")
	}
}

// =============================================================================
// Status reporting
// =============================================================================

// TestSessionStatus_ReportsResumableAfterRestart asserts an operator can tell a
// restored-but-processless session from a live one, and from a closed one.
func TestSessionStatus_ReportsResumableAfterRestart(t *testing.T) {
	t.Parallel()

	first := newResumeHarness(t, nil)
	ctx := ContextWithTenant(context.Background(), "tenant-a")
	sessionID := first.spawnAndConverse(t, "tenant-a", "/work/repo", true)

	live, err := first.service.SessionStatus(ctx, &SessionStatusRequest{SessionID: sessionID})
	if err != nil {
		t.Fatalf("SessionStatus before restart: %v", err)
	}
	if live.State != SessionStateActive {
		t.Fatalf("state before restart = %q, want active", live.State)
	}

	second := first.restart(t)
	after, err := second.service.SessionStatus(ctx, &SessionStatusRequest{SessionID: sessionID})
	if err != nil {
		t.Fatalf("SessionStatus after restart: %v", err)
	}
	if after.State != SessionStateResumable {
		t.Fatalf("state after restart = %q, want resumable", after.State)
	}

	listed, err := second.service.ListSessions(ctx, &ListSessionsRequest{})
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(listed.Sessions) != 1 || listed.Sessions[0].State != SessionStateResumable {
		t.Fatalf("ListSessions state = %+v, want a single resumable session", listed.Sessions)
	}

	if _, err := second.service.CloseSession(ctx, &CloseSessionRequest{SessionID: sessionID}); err != nil {
		t.Fatalf("CloseSession on a resumable session: %v", err)
	}
	closed, err := second.service.SessionStatus(ctx, &SessionStatusRequest{SessionID: sessionID})
	if err != nil {
		t.Fatalf("SessionStatus after close: %v", err)
	}
	if closed.State != SessionStateClosed {
		t.Fatalf("state after close = %q, want closed", closed.State)
	}
}

// =============================================================================
// Resume on prompt
// =============================================================================

// TestPromptSession_ResumesAfterRestart is the headline test: a prompt sent to
// a session whose daemon restarted must transparently reconstitute the session,
// hand the prior turns to the replacement agent, and run the turn.
func TestPromptSession_ResumesAfterRestart(t *testing.T) {
	t.Parallel()

	first := newResumeHarness(t, nil)
	ctx := ContextWithTenant(context.Background(), "tenant-a")
	sessionID := first.spawnAndConverse(t, "tenant-a", "/work/repo", true)

	second := first.restart(t)
	resp, err := second.service.PromptSession(ctx, &PromptSessionRequest{
		SessionID: sessionID,
		Prompt:    "and its population?",
	})
	if err != nil {
		t.Fatalf("PromptSession after restart: %v", err)
	}
	if len(resp.Content) == 0 {
		t.Fatal("resumed prompt returned no content")
	}

	if len(second.backend.resumeCalls) != 1 {
		t.Fatalf("Resume called %d times, want exactly 1", len(second.backend.resumeCalls))
	}
	call := second.backend.resumeCalls[0]
	if call.SessionID != sessionID {
		t.Errorf("resumed under %q, want the original ID %q", call.SessionID, sessionID)
	}
	if call.Spawn.WorkDir != "/work/repo" || call.Spawn.SystemPrompt != "you are a careful engineer" {
		t.Errorf("resume spawn config = %+v, want the original spawn parameters", call.Spawn)
	}
	if !call.Spawn.Persistent {
		t.Error("resume spawn request lost the persistent flag")
	}

	// The replayed transcript must carry the conversation the operator had
	// before the restart, in order.
	if call.Transcript == nil || len(call.Transcript.Turns) < 2 {
		t.Fatalf("transcript = %+v, want at least the prior prompt and answer", call.Transcript)
	}
	if call.Transcript.Turns[0].Role != ReplayRoleUser ||
		!strings.Contains(call.Transcript.Turns[0].Text, "capital of France") {
		t.Errorf("first replayed turn = %+v, want the operator's prior prompt", call.Transcript.Turns[0])
	}
	if call.Transcript.Turns[1].Role != ReplayRoleAssistant ||
		!strings.Contains(call.Transcript.Turns[1].Text, "answer to") {
		t.Errorf("second replayed turn = %+v, want the agent's prior answer", call.Transcript.Turns[1])
	}
	if call.Transcript.SystemPrompt != "you are a careful engineer" {
		t.Errorf("transcript system prompt = %q, want the session's own", call.Transcript.SystemPrompt)
	}

	// The session is live again, and a second prompt does not resume twice.
	status, err := second.service.SessionStatus(ctx, &SessionStatusRequest{SessionID: sessionID})
	if err != nil {
		t.Fatalf("SessionStatus after resume: %v", err)
	}
	if status.State != SessionStateActive {
		t.Fatalf("state after resume = %q, want active", status.State)
	}
	if _, err := second.service.PromptSession(ctx, &PromptSessionRequest{
		SessionID: sessionID,
		Prompt:    "anything else?",
	}); err != nil {
		t.Fatalf("second PromptSession: %v", err)
	}
	if len(second.backend.resumeCalls) != 1 {
		t.Fatalf("Resume called %d times, want 1 — a live session must not be resumed again",
			len(second.backend.resumeCalls))
	}

	// The restoration is on the durable record and the transcript.
	record, err := second.store.GetSession(ctx, "tenant-a", sessionID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if record.ResumeMode != ResumeModeReplay || record.Status != SessionStateActive {
		t.Errorf("record after resume: mode=%q status=%q, want replay/active", record.ResumeMode, record.Status)
	}
	events, _, err := second.log.Range(ctx, "tenant-a", sessionID, 1, 0)
	if err != nil {
		t.Fatalf("Range: %v", err)
	}
	var sawResumed bool
	for _, event := range events {
		if event.Kind == KindSessionResumed {
			sawResumed = true
		}
	}
	if !sawResumed {
		t.Error("no session_resumed event on the transcript")
	}
}

// TestPromptSession_NonPersistentSessionDoesNotResume asserts a session that
// never held a long-running process is reported as closed after a restart
// rather than being reconstituted as though it had context to restore.
func TestPromptSession_NonPersistentSessionDoesNotResume(t *testing.T) {
	t.Parallel()

	first := newResumeHarness(t, nil)
	ctx := ContextWithTenant(context.Background(), "tenant-a")
	sessionID := first.spawnAndConverse(t, "tenant-a", "/work/oneshot", false)

	second := first.restart(t)

	if len(second.backend.resumeCalls) != 0 {
		t.Fatalf("Resume called %d times for a non-persistent session, want 0",
			len(second.backend.resumeCalls))
	}

	record, err := second.store.GetSession(ctx, "tenant-a", sessionID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if record.Status != SessionStateClosed {
		t.Fatalf("non-persistent session status after restart = %q, want closed", record.Status)
	}

	status, err := second.service.SessionStatus(ctx, &SessionStatusRequest{SessionID: sessionID})
	if err != nil {
		t.Fatalf("SessionStatus: %v", err)
	}
	if status.State != SessionStateClosed {
		t.Fatalf("SessionStatus state = %q, want closed", status.State)
	}

	// Prompting it must fail rather than quietly starting a fresh agent.
	if _, err := second.service.PromptSession(ctx, &PromptSessionRequest{
		SessionID: sessionID,
		Prompt:    "still there?",
	}); err == nil {
		t.Fatal("PromptSession on a non-resumed session succeeded, want an error")
	}
}

// TestPromptSession_ResumeFailureIsReported asserts a failed resume surfaces to
// the caller and is recorded on the session, instead of silently answering the
// prompt with a context-free agent.
func TestPromptSession_ResumeFailureIsReported(t *testing.T) {
	t.Parallel()

	first := newResumeHarness(t, nil)
	ctx := ContextWithTenant(context.Background(), "tenant-a")
	sessionID := first.spawnAndConverse(t, "tenant-a", "/work/repo", true)

	second := first.restart(t)
	second.backend.resumeErr = errors.New("agent binary is missing")

	_, err := second.service.PromptSession(ctx, &PromptSessionRequest{
		SessionID: sessionID,
		Prompt:    "carry on",
	})
	if err == nil {
		t.Fatal("PromptSession succeeded despite a failed resume")
	}
	if !strings.Contains(err.Error(), "agent binary is missing") {
		t.Errorf("error = %v, want it to carry the resume failure", err)
	}
	if len(second.backend.promptCalls) != 0 {
		t.Errorf("backend received %d prompts after a failed resume, want 0", len(second.backend.promptCalls))
	}

	record, err := second.store.GetSession(ctx, "tenant-a", sessionID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if record.Status != SessionStateError || record.ResumeError == "" {
		t.Fatalf("record after failed resume: status=%q resume_error=%q, want error state with a reason",
			record.Status, record.ResumeError)
	}
}

// TestPromptSession_ResumeUnsupportedBackendIsReported asserts a backend with
// no resume mechanism produces a typed error rather than a fresh agent.
func TestPromptSession_ResumeUnsupportedBackendIsReported(t *testing.T) {
	t.Parallel()

	first := newResumeHarness(t, nil)
	ctx := ContextWithTenant(context.Background(), "tenant-a")
	sessionID := first.spawnAndConverse(t, "tenant-a", "/work/repo", true)

	// Rebuild the service with a backend that cannot resume.
	second := newResumeHarness(t, first.kv)
	second.service.backends.Store("mock", &mockAgentBackend{})
	if err := second.service.LoadPersistedSessions(context.Background()); err != nil {
		t.Fatalf("LoadPersistedSessions: %v", err)
	}

	_, err := second.service.PromptSession(ctx, &PromptSessionRequest{
		SessionID: sessionID,
		Prompt:    "carry on",
	})
	if !errors.Is(err, ErrSessionResumeUnsupported) {
		t.Fatalf("error = %v, want ErrSessionResumeUnsupported", err)
	}
}

// TestPromptSession_ResumeRefusesForeignTenant asserts a caller from another
// tenant cannot trigger a resume, which would replay the owning tenant's
// transcript into an agent answering the foreign caller's prompt.
func TestPromptSession_ResumeRefusesForeignTenant(t *testing.T) {
	t.Parallel()

	first := newResumeHarness(t, nil)
	sessionID := first.spawnAndConverse(t, "tenant-a", "/work/repo", true)

	second := first.restart(t)
	intruder := ContextWithTenant(context.Background(), "tenant-b")

	_, err := second.service.PromptSession(intruder, &PromptSessionRequest{
		SessionID: sessionID,
		Prompt:    "show me the conversation",
	})
	if !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("cross-tenant prompt error = %v, want ErrSessionNotFound", err)
	}
	if len(second.backend.resumeCalls) != 0 {
		t.Fatalf("Resume called %d times for a foreign tenant, want 0", len(second.backend.resumeCalls))
	}

	// The owner can still resume its own session.
	owner := ContextWithTenant(context.Background(), "tenant-a")
	if _, err := second.service.PromptSession(owner, &PromptSessionRequest{
		SessionID: sessionID,
		Prompt:    "carry on",
	}); err != nil {
		t.Fatalf("owner PromptSession: %v", err)
	}
	if len(second.backend.resumeCalls) != 1 {
		t.Fatalf("owner resume calls = %d, want 1", len(second.backend.resumeCalls))
	}
}

// TestPromptSessionAsync_ResumeFailureLandsOnTranscript asserts the
// asynchronous turn contract holds for a resume: the acknowledgement returns
// immediately and the failure is published as a terminal turn_failed event
// rather than being lost.
func TestPromptSessionAsync_ResumeFailureLandsOnTranscript(t *testing.T) {
	t.Parallel()

	first := newResumeHarness(t, nil)
	ctx := ContextWithTenant(context.Background(), "tenant-a")
	sessionID := first.spawnAndConverse(t, "tenant-a", "/work/repo", true)

	second := first.restart(t)
	second.backend.resumeErr = errors.New("workspace is gone")

	ack, err := second.service.PromptSessionAsync(ctx, &PromptSessionRequest{
		SessionID: sessionID,
		Prompt:    "carry on",
	})
	if err != nil {
		t.Fatalf("PromptSessionAsync: %v", err)
	}
	if ack.TurnID == "" {
		t.Fatal("acknowledgement carried no turn ID")
	}
	second.service.WaitForTurns()

	events, _, err := second.log.Range(ctx, "tenant-a", sessionID, 1, 0)
	if err != nil {
		t.Fatalf("Range: %v", err)
	}
	var failure *TurnFailedPayload
	for _, event := range events {
		if event.Kind == KindTurnFailed {
			failure = event.TurnFailed
		}
	}
	if failure == nil {
		t.Fatal("no turn_failed event for a turn whose resume failed")
	}
	if !strings.Contains(failure.Error, "workspace is gone") {
		t.Errorf("turn_failed error = %q, want the resume failure", failure.Error)
	}
}

// TestLoadPersistedSessions_SkipsUnknownBackend asserts a session whose backend
// is no longer registered stays out of the registry — it cannot be addressed,
// so presenting it as resumable would be a lie — and that its record is
// CONCLUDED rather than left saying active, which is the stranded state
// go-quicbaas#792 was filed for.
func TestLoadPersistedSessions_SkipsUnknownBackend(t *testing.T) {
	t.Parallel()

	kv := kvstore.NewMemory()
	store := NewAgentStore(kv)
	if err := store.PutSession(context.Background(), &SessionRecord{
		SessionID:    "sess-orphan",
		TenantID:     "tenant-a",
		Backend:      "retired-backend",
		WorkDir:      "/w",
		Persistent:   true,
		BackendLocal: true,
		Status:       SessionStateActive,
	}); err != nil {
		t.Fatalf("PutSession: %v", err)
	}

	h := newResumeHarness(t, kv)
	if err := h.service.LoadPersistedSessions(context.Background()); err != nil {
		t.Fatalf("LoadPersistedSessions: %v", err)
	}
	if _, ok := h.service.liveSessionRecord("sess-orphan"); ok {
		t.Fatal("a session with an unregistered backend was restored into the registry")
	}
	record, err := store.GetSession(context.Background(), "tenant-a", "sess-orphan")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if record.Status != SessionStateClosed || record.ConclusionReason != ConcludedByNodeRestart {
		t.Fatalf("status=%q reason=%q, want closed/%s; a record nothing can address must not keep "+
			"reporting a process that is gone", record.Status, record.ConclusionReason, ConcludedByNodeRestart)
	}
}

// TestLoadPersistedSessions_RemoteSessionStaysLive asserts a session dispatched
// to another daemon is restored as live rather than resumable: this daemon's
// restart did not touch the process that serves it.
func TestLoadPersistedSessions_RemoteSessionStaysLive(t *testing.T) {
	t.Parallel()

	kv := kvstore.NewMemory()
	store := NewAgentStore(kv)
	if err := store.PutSession(context.Background(), &SessionRecord{
		SessionID:    "sess-remote",
		TenantID:     "tenant-a",
		Backend:      "mock",
		WorkDir:      "/w",
		Persistent:   true,
		BackendLocal: false,
		HostAffinity: "worker-3",
		Status:       SessionStateActive,
	}); err != nil {
		t.Fatalf("PutSession: %v", err)
	}

	h := newResumeHarness(t, kv)
	if err := h.service.LoadPersistedSessions(context.Background()); err != nil {
		t.Fatalf("LoadPersistedSessions: %v", err)
	}
	rec, ok := h.service.liveSessionRecord("sess-remote")
	if !ok {
		t.Fatal("remote session was not restored into the registry")
	}
	if rec.resumable.Load() {
		t.Error("remote session marked resumable; its process lives on another host")
	}
	if rec.HostAffinity != "worker-3" {
		t.Errorf("host affinity = %q, want it preserved so follow-ups reach the same daemon", rec.HostAffinity)
	}
}

// TestLoadPersistedSessions_StoreFailureIsTyped asserts a store that cannot be
// read produces a typed service error rather than a silent empty registry.
func TestLoadPersistedSessions_StoreFailureIsTyped(t *testing.T) {
	t.Parallel()

	h := newResumeHarness(t, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := h.service.LoadPersistedSessions(ctx)
	if !errors.Is(err, ErrAgentServiceUnavailable) {
		t.Fatalf("LoadPersistedSessions on a failing store = %v, want ErrAgentServiceUnavailable", err)
	}
}

// =============================================================================
// Transcript construction and bounding
// =============================================================================

// TestBuildReplayTranscript_ProjectsConversation asserts the transcript carries
// the conversation in order, merging streamed assistant chunks into the single
// message the operator saw.
func TestBuildReplayTranscript_ProjectsConversation(t *testing.T) {
	t.Parallel()

	log := NewMemorySessionEventLog()
	ctx := context.Background()
	events := []*SessionEvent{
		{Kind: KindPromptSubmitted, Prompt: &PromptSubmittedPayload{Prompt: "first question"}},
		{Kind: KindAssistantChunk, AssistantChunk: &AssistantChunkPayload{Text: "part one "}},
		{Kind: KindAssistantChunk, AssistantChunk: &AssistantChunkPayload{Text: "part two"}},
		{Kind: KindToolInvocation},
		{Kind: KindPromptSubmitted, Prompt: &PromptSubmittedPayload{Prompt: "second question"}},
	}
	for _, event := range events {
		event.SessionID = "sess-1"
		event.TenantID = "tenant-a"
		if err := AppendNextSessionEvent(ctx, log, event); err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	transcript, err := buildReplayTranscript(ctx, log, "tenant-a", "sess-1", "sys", 0)
	if err != nil {
		t.Fatalf("buildReplayTranscript: %v", err)
	}
	if len(transcript.Turns) != 3 {
		t.Fatalf("turns = %+v, want 3 (prompt, merged answer, prompt)", transcript.Turns)
	}
	if transcript.Turns[1].Text != "part one part two" {
		t.Errorf("streamed chunks not merged: %q", transcript.Turns[1].Text)
	}
	if transcript.Turns[2].Text != "second question" {
		t.Errorf("last turn = %q, want the second prompt", transcript.Turns[2].Text)
	}
	if transcript.SystemPrompt != "sys" {
		t.Errorf("system prompt = %q, want it carried", transcript.SystemPrompt)
	}
}

// TestBuildReplayTranscript_NoLogIsAnError asserts a missing event log is
// reported rather than yielding an empty transcript, which would resume the
// session with a context-free agent.
func TestBuildReplayTranscript_NoLogIsAnError(t *testing.T) {
	t.Parallel()

	_, err := buildReplayTranscript(context.Background(), nil, "tenant-a", "sess-1", "", 0)
	if !errors.Is(err, ErrSessionTranscriptUnavailable) {
		t.Fatalf("error = %v, want ErrSessionTranscriptUnavailable", err)
	}
}

// TestBoundReplayTurns_DropsOldestFirst asserts the budget is enforced from the
// oldest end, so the turns nearest the one about to be answered are kept.
func TestBoundReplayTurns_DropsOldestFirst(t *testing.T) {
	t.Parallel()

	turns := []ReplayTurn{
		{Role: ReplayRoleUser, Text: strings.Repeat("a", 10)},
		{Role: ReplayRoleAssistant, Text: strings.Repeat("b", 10)},
		{Role: ReplayRoleUser, Text: strings.Repeat("c", 10)},
	}

	retained, omitted := boundReplayTurns(turns, 25)
	if omitted != 1 {
		t.Fatalf("omitted = %d, want 1", omitted)
	}
	if len(retained) != 2 || retained[0].Text[0] != 'b' || retained[1].Text[0] != 'c' {
		t.Fatalf("retained = %+v, want the two newest turns", retained)
	}

	// A single turn larger than the whole budget is kept whole: a turn cut
	// mid-sentence reads as a different statement from the one that was made.
	retained, omitted = boundReplayTurns(turns, 1)
	if len(retained) != 1 || omitted != 2 || retained[0].Text[0] != 'c' {
		t.Fatalf("retained = %+v omitted = %d, want the newest turn kept whole", retained, omitted)
	}

	if retained, omitted := boundReplayTurns(nil, 100); retained != nil || omitted != 0 {
		t.Fatalf("boundReplayTurns(nil) = %v, %d, want nil, 0", retained, omitted)
	}
}

// TestReplayTranscript_PrimingMessageDeclaresOmissions asserts the agent is
// told, in the message itself, that its view of the conversation is partial.
func TestReplayTranscript_PrimingMessageDeclaresOmissions(t *testing.T) {
	t.Parallel()

	transcript := &ReplayTranscript{
		SessionID:    "sess-1",
		SystemPrompt: "be terse",
		Turns: []ReplayTurn{
			{Role: ReplayRoleUser, Text: "what changed?"},
			{Role: ReplayRoleAssistant, Text: "the config"},
		},
		Omitted: 4,
	}

	msg := transcript.PrimingMessage()
	for _, want := range []string{"resuming", "be terse", "sess-1", "what changed?", "the config", "4 earlier turn(s) omitted"} {
		if !strings.Contains(msg, want) {
			t.Errorf("priming message missing %q:\n%s", want, msg)
		}
	}

	full := &ReplayTranscript{SessionID: "sess-2", Turns: []ReplayTurn{{Role: ReplayRoleUser, Text: "hi"}}}
	if strings.Contains(full.PrimingMessage(), "omitted") {
		t.Error("a complete transcript must not claim turns were omitted")
	}
	if (*ReplayTranscript)(nil).PrimingMessage() != "" {
		t.Error("nil transcript must render an empty priming message")
	}
}

// TestReplayBudget_DefaultsWhenUnset asserts the service falls back to the
// package default rather than an unbounded (or zero) replay.
func TestReplayBudget_DefaultsWhenUnset(t *testing.T) {
	t.Parallel()

	h := newResumeHarness(t, nil)
	if got := h.service.replayBudget(); got != DefaultReplayBudget {
		t.Fatalf("replayBudget() = %d, want %d", got, DefaultReplayBudget)
	}
	h.service.SetTranscriptReplayBudget(1234)
	if got := h.service.replayBudget(); got != 1234 {
		t.Fatalf("replayBudget() after override = %d, want 1234", got)
	}
	h.service.SetTranscriptReplayBudget(-1)
	if got := h.service.replayBudget(); got != DefaultReplayBudget {
		t.Fatalf("replayBudget() after invalid override = %d, want the default", got)
	}
}

// =============================================================================
// Typed errors
// =============================================================================

// TestResumeErrors_ReportCauseAndSentinel asserts every resume error names the
// session it concerns, states why, and matches both its sentinel and its
// underlying cause — the two contracts a caller inspects it through.
func TestResumeErrors_ReportCauseAndSentinel(t *testing.T) {
	t.Parallel()

	cause := errors.New("underlying failure")

	notResumable := &SessionNotResumableError{SessionID: "s1", Reason: "not persistent"}
	if !errors.Is(notResumable, ErrSessionNotResumable) ||
		!strings.Contains(notResumable.Error(), "s1") ||
		!strings.Contains(notResumable.Error(), "not persistent") {
		t.Errorf("SessionNotResumableError = %q", notResumable.Error())
	}

	unsupported := &SessionResumeUnsupportedError{Backend: "ollama", SessionID: "s2", Reason: "stateless"}
	if !errors.Is(unsupported, ErrSessionResumeUnsupported) ||
		!strings.Contains(unsupported.Error(), "ollama") ||
		!strings.Contains(unsupported.Error(), "stateless") {
		t.Errorf("SessionResumeUnsupportedError = %q", unsupported.Error())
	}
	bare := &SessionResumeUnsupportedError{Backend: "ollama", SessionID: "s2"}
	if strings.HasSuffix(bare.Error(), ": ") {
		t.Errorf("SessionResumeUnsupportedError without a reason = %q", bare.Error())
	}

	transcript := &SessionTranscriptUnavailableError{SessionID: "s3", Reason: "log offline", Cause: cause}
	if !errors.Is(transcript, ErrSessionTranscriptUnavailable) || !errors.Is(transcript, cause) {
		t.Errorf("SessionTranscriptUnavailableError did not expose both contracts: %v", transcript)
	}
	if !strings.Contains(transcript.Error(), "log offline") {
		t.Errorf("SessionTranscriptUnavailableError = %q", transcript.Error())
	}
	if len((&SessionTranscriptUnavailableError{SessionID: "s3"}).Unwrap()) != 1 {
		t.Error("a causeless transcript error must unwrap to its sentinel alone")
	}

	resumeErr := &SessionResumeError{SessionID: "s4", Mode: ResumeModeReplay, Cause: cause}
	if !errors.Is(resumeErr, ErrSessionResumeFailed) || !errors.Is(resumeErr, cause) {
		t.Errorf("SessionResumeError did not expose both contracts: %v", resumeErr)
	}
	if !strings.Contains(resumeErr.Error(), "replay") || !strings.Contains(resumeErr.Error(), "underlying failure") {
		t.Errorf("SessionResumeError = %q", resumeErr.Error())
	}
	bareResume := &SessionResumeError{SessionID: "s4"}
	if len(bareResume.Unwrap()) != 1 || strings.Contains(bareResume.Error(), "(") {
		t.Errorf("causeless SessionResumeError = %q", bareResume.Error())
	}

	live := &SessionAlreadyLiveError{SessionID: "s5"}
	if !strings.Contains(live.Error(), "s5") {
		t.Errorf("SessionAlreadyLiveError = %q", live.Error())
	}
}

// TestListAllSessions_AttributesRecordsByKey asserts the boot-time scan can
// attribute a record whose body lost its tenant, since the key is the
// authoritative owner and the restore path needs it to address the record.
func TestListAllSessions_AttributesRecordsByKey(t *testing.T) {
	t.Parallel()

	kv := kvstore.NewMemory()
	if err := kv.Put(context.Background(), "orch/session/tenant-a/sess-1",
		[]byte(`{"session_id":"sess-1","backend":"claude","work_dir":"/w","status":"active"}`)); err != nil {
		t.Fatalf("seed: %v", err)
	}

	all, err := NewAgentStore(kv).ListAllSessions(context.Background())
	if err != nil {
		t.Fatalf("ListAllSessions: %v", err)
	}
	if len(all) != 1 || all[0].TenantID != "tenant-a" {
		t.Fatalf("records = %+v, want tenant-a attributed from the key", all)
	}
}
