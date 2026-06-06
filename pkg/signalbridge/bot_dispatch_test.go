// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
//  1. GNU Affero General Public License v3.0 (AGPL-3.0)
//     See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
//  2. Commercial License
//     Contact licensing@automatethethings.com for commercial licensing options.

package signalbridge

import (
	"context"
	"errors"
	"testing"

	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// errOrchestrator is an OrchestratorSDK whose every verb can be made to
// fail by setting the matching err field. Successful responses are
// deterministic. Used to drive both happy-path and error branches of the
// bot's per-intent dispatchers.
type errOrchestrator struct {
	spawnErr   error
	promptErr  error
	closeErr   error
	statusErr  error
	listSesErr error
	listAgtErr error
	jobErr     error

	sessions []*orchestratorSDK.SessionStatusResponse
	agents   []*orchestratorSDK.AgentRegistration

	spawns  int
	prompts int
	closes  []*orchestratorSDK.CloseSessionRequest
	jobs    int

	eventChan chan *orchestratorSDK.SessionEventEnvelope
}

func newErrOrchestrator() *errOrchestrator {
	return &errOrchestrator{
		sessions:  []*orchestratorSDK.SessionStatusResponse{{SessionID: "sess-1", Status: "running", Backend: "claude"}},
		eventChan: make(chan *orchestratorSDK.SessionEventEnvelope, 4),
	}
}

func (m *errOrchestrator) SpawnSession(ctx context.Context, req *orchestratorSDK.SpawnSessionRequest) (*orchestratorSDK.SpawnSessionResponse, error) {
	if m.spawnErr != nil {
		return nil, m.spawnErr
	}
	m.spawns++
	return &orchestratorSDK.SpawnSessionResponse{SessionID: "sess-new", Backend: req.Backend}, nil
}
func (m *errOrchestrator) PromptSession(ctx context.Context, req *orchestratorSDK.PromptSessionRequest) (*orchestratorSDK.PromptSessionResponse, error) {
	if m.promptErr != nil {
		return nil, m.promptErr
	}
	m.prompts++
	return &orchestratorSDK.PromptSessionResponse{SessionID: req.SessionID}, nil
}
func (m *errOrchestrator) CloseSession(ctx context.Context, req *orchestratorSDK.CloseSessionRequest) (*orchestratorSDK.CloseSessionResponse, error) {
	if m.closeErr != nil {
		return nil, m.closeErr
	}
	m.closes = append(m.closes, req)
	return &orchestratorSDK.CloseSessionResponse{SessionID: req.SessionID, Closed: true}, nil
}
func (m *errOrchestrator) ListSessions(ctx context.Context, req *orchestratorSDK.ListSessionsRequest) (*orchestratorSDK.ListSessionsResponse, error) {
	if m.listSesErr != nil {
		return nil, m.listSesErr
	}
	return &orchestratorSDK.ListSessionsResponse{Sessions: m.sessions, Count: len(m.sessions)}, nil
}
func (m *errOrchestrator) ListAgents(ctx context.Context, req *orchestratorSDK.ListAgentsRequest) (*orchestratorSDK.ListAgentsResponse, error) {
	if m.listAgtErr != nil {
		return nil, m.listAgtErr
	}
	return &orchestratorSDK.ListAgentsResponse{Agents: m.agents, Count: len(m.agents)}, nil
}
func (m *errOrchestrator) SessionStatus(ctx context.Context, req *orchestratorSDK.SessionStatusRequest) (*orchestratorSDK.SessionStatusResponse, error) {
	if m.statusErr != nil {
		return nil, m.statusErr
	}
	return &orchestratorSDK.SessionStatusResponse{SessionID: req.SessionID, Backend: "claude", Status: "running"}, nil
}
func (m *errOrchestrator) StartJob(ctx context.Context, req *orchestratorSDK.StartJobRequest) (*orchestratorSDK.StartJobResponse, error) {
	if m.jobErr != nil {
		return nil, m.jobErr
	}
	m.jobs++
	return &orchestratorSDK.StartJobResponse{JobID: "job-1"}, nil
}
func (m *errOrchestrator) SubscribeSessionEvents(ctx context.Context, req *orchestratorSDK.SubscribeSessionEventsRequest) (<-chan *orchestratorSDK.SessionEventEnvelope, error) {
	return m.eventChan, nil
}

// newDispatchBot assembles a bot wired to an errOrchestrator so dispatch
// handlers can be invoked directly (without the Run goroutine).
func newDispatchBot(t *testing.T) (*OrchestratorBot, *fakeSignalIO, *errOrchestrator, *SessionManager) {
	t.Helper()
	store := NewMemoryStateStore()
	mgr, err := NewSessionManager(store, DefaultVerbosityPrefs())
	if err != nil {
		t.Fatalf("manager: %v", err)
	}
	signal := newFakeSignalIO()
	translator, err := NewEventTranslator(signal, mgr)
	if err != nil {
		t.Fatalf("translator: %v", err)
	}
	orch := newErrOrchestrator()
	auth, err := NewAuthBridge(AuthBridgeConfig{Mode: AuthModeImplicit, TenantID: "t", ServiceAccount: "bot"})
	if err != nil {
		t.Fatalf("auth: %v", err)
	}
	bot, err := NewOrchestratorBot(BotConfigRuntime{
		Config:       &BotConfig{BotIdentity: "+1", GoSignalURL: "x", QuicBaaSAddr: "x", TenantID: "t", AuthMode: AuthModeImplicit, ServiceAccount: "bot", VerbosityDefaults: DefaultVerbosityPrefs()},
		Signal:       signal,
		Orchestrator: orch,
		Auth:         auth,
		Manager:      mgr,
		Translator:   translator,
		Parser:       NewIntentParser(),
		Logger:       noopLogger{},
	})
	if err != nil {
		t.Fatalf("bot: %v", err)
	}
	return bot, signal, orch, mgr
}

// lastSend returns the body of the most recent reply, or "".
func lastSend(s *fakeSignalIO) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.sends) == 0 {
		return ""
	}
	return s.sends[len(s.sends)-1].Body
}

// dmMsg builds a DM inbound message.
func dmMsg(body string) InboundMessage {
	return InboundMessage{ConversationID: "conv-1", Kind: ConversationDM, SenderACI: "aci-1", Body: body}
}

// testOperator is the operator the implicit auth bridge resolves to.
var testOperator = OperatorIdentity{TenantID: "t", SubjectID: "bot", Source: "implicit"}

// TestBot_DetachNotAttached returns the "(not attached)" reply.
func TestBot_DetachNotAttached(t *testing.T) {
	bot, signal, _, _ := newDispatchBot(t)
	if err := bot.detachOrDispatch(context.Background(), dmMsg("detach"), testOperator, Intent{Kind: IntentDetach}); err != nil {
		t.Fatalf("detach: %v", err)
	}
	if lastSend(signal) != "(not attached)" {
		t.Fatalf("body = %q", lastSend(signal))
	}
}

// TestBot_DetachAttached detaches and stops the subscription.
func TestBot_DetachAttached(t *testing.T) {
	bot, signal, _, mgr := newDispatchBot(t)
	if err := mgr.Attach("conv-1", "sess-D", ConversationDM, testOperator); err != nil {
		t.Fatalf("attach: %v", err)
	}
	if err := bot.detachOrDispatch(context.Background(), dmMsg("detach"), testOperator, Intent{Kind: IntentDetach}); err != nil {
		t.Fatalf("detach: %v", err)
	}
	if mgr.Get("conv-1", ConversationDM).AttachedSessionID != "" {
		t.Fatalf("still attached")
	}
	if !contains(lastSend(signal), "detached") {
		t.Fatalf("body = %q", lastSend(signal))
	}
}

// TestBot_CloseNotAttached returns the no-op reply.
func TestBot_CloseNotAttached(t *testing.T) {
	bot, signal, orch, _ := newDispatchBot(t)
	if err := bot.closeOrDispatch(context.Background(), dmMsg("close"), testOperator, Intent{Kind: IntentClose}); err != nil {
		t.Fatalf("close: %v", err)
	}
	if lastSend(signal) != "(nothing to close)" {
		t.Fatalf("body = %q", lastSend(signal))
	}
	if len(orch.closes) != 0 {
		t.Fatalf("should not have called CloseSession")
	}
}

// TestBot_CloseAttached closes the orchestrator session and detaches.
func TestBot_CloseAttached(t *testing.T) {
	bot, signal, orch, mgr := newDispatchBot(t)
	_ = mgr.Attach("conv-1", "sess-C", ConversationDM, testOperator)
	if err := bot.closeOrDispatch(context.Background(), dmMsg("close"), testOperator, Intent{Kind: IntentClose}); err != nil {
		t.Fatalf("close: %v", err)
	}
	if len(orch.closes) != 1 || orch.closes[0].SessionID != "sess-C" {
		t.Fatalf("close calls: %+v", orch.closes)
	}
	if !contains(lastSend(signal), "closed") {
		t.Fatalf("body = %q", lastSend(signal))
	}
}

// TestBot_CloseError surfaces an orchestrator error as a ❌ reply.
func TestBot_CloseError(t *testing.T) {
	bot, signal, orch, mgr := newDispatchBot(t)
	_ = mgr.Attach("conv-1", "sess-C", ConversationDM, testOperator)
	orch.closeErr = errors.New("backend down")
	if err := bot.closeOrDispatch(context.Background(), dmMsg("close"), testOperator, Intent{Kind: IntentClose}); err != nil {
		t.Fatalf("close: %v", err)
	}
	if !contains(lastSend(signal), "close failed") {
		t.Fatalf("body = %q", lastSend(signal))
	}
}

// TestBot_StatusNotAttached returns "(not attached)".
func TestBot_StatusNotAttached(t *testing.T) {
	bot, signal, _, _ := newDispatchBot(t)
	if err := bot.statusOrDispatch(context.Background(), dmMsg("status"), testOperator, Intent{Kind: IntentStatus}); err != nil {
		t.Fatalf("status: %v", err)
	}
	if lastSend(signal) != "(not attached)" {
		t.Fatalf("body = %q", lastSend(signal))
	}
}

// TestBot_StatusAttached renders the session status line.
func TestBot_StatusAttached(t *testing.T) {
	bot, signal, _, mgr := newDispatchBot(t)
	_ = mgr.Attach("conv-1", "sess-S", ConversationDM, testOperator)
	if err := bot.statusOrDispatch(context.Background(), dmMsg("status"), testOperator, Intent{Kind: IntentStatus}); err != nil {
		t.Fatalf("status: %v", err)
	}
	if !contains(lastSend(signal), "sess-S") || !contains(lastSend(signal), "running") {
		t.Fatalf("body = %q", lastSend(signal))
	}
}

// TestBot_StatusError surfaces an orchestrator error.
func TestBot_StatusError(t *testing.T) {
	bot, signal, orch, mgr := newDispatchBot(t)
	_ = mgr.Attach("conv-1", "sess-S", ConversationDM, testOperator)
	orch.statusErr = errors.New("nope")
	if err := bot.statusOrDispatch(context.Background(), dmMsg("status"), testOperator, Intent{Kind: IntentStatus}); err != nil {
		t.Fatalf("status: %v", err)
	}
	if !contains(lastSend(signal), "❌") {
		t.Fatalf("body = %q", lastSend(signal))
	}
}

// TestBot_SessionsList renders the active sessions list.
func TestBot_SessionsList(t *testing.T) {
	bot, signal, _, _ := newDispatchBot(t)
	if err := bot.sessionsOrDispatch(context.Background(), dmMsg("sessions"), testOperator, Intent{Kind: IntentSessions}); err != nil {
		t.Fatalf("sessions: %v", err)
	}
	if !contains(lastSend(signal), "sess-1") {
		t.Fatalf("body = %q", lastSend(signal))
	}
}

// TestBot_SessionsEmpty renders the no-sessions reply.
func TestBot_SessionsEmpty(t *testing.T) {
	bot, signal, orch, _ := newDispatchBot(t)
	orch.sessions = nil
	if err := bot.sessionsOrDispatch(context.Background(), dmMsg("sessions"), testOperator, Intent{Kind: IntentSessions}); err != nil {
		t.Fatalf("sessions: %v", err)
	}
	if !contains(lastSend(signal), "no active sessions") {
		t.Fatalf("body = %q", lastSend(signal))
	}
}

// TestBot_SessionsError surfaces a list error.
func TestBot_SessionsError(t *testing.T) {
	bot, signal, orch, _ := newDispatchBot(t)
	orch.listSesErr = errors.New("down")
	if err := bot.sessionsOrDispatch(context.Background(), dmMsg("sessions"), testOperator, Intent{Kind: IntentSessions}); err != nil {
		t.Fatalf("sessions: %v", err)
	}
	if !contains(lastSend(signal), "❌") {
		t.Fatalf("body = %q", lastSend(signal))
	}
}

// TestBot_AgentsEmpty renders the no-agents reply.
func TestBot_AgentsEmpty(t *testing.T) {
	bot, signal, _, _ := newDispatchBot(t)
	if err := bot.agentsOrDispatch(context.Background(), dmMsg("agents"), testOperator, Intent{Kind: IntentAgents}); err != nil {
		t.Fatalf("agents: %v", err)
	}
	if !contains(lastSend(signal), "no agents") {
		t.Fatalf("body = %q", lastSend(signal))
	}
}

// TestBot_AgentsList renders the agents table.
func TestBot_AgentsList(t *testing.T) {
	bot, signal, orch, _ := newDispatchBot(t)
	orch.agents = []*orchestratorSDK.AgentRegistration{
		{Name: "alpha", Host: "h1", Status: "ready", Backend: "claude"},
	}
	if err := bot.agentsOrDispatch(context.Background(), dmMsg("agents"), testOperator, Intent{Kind: IntentAgents}); err != nil {
		t.Fatalf("agents: %v", err)
	}
	if !contains(lastSend(signal), "alpha") {
		t.Fatalf("body = %q", lastSend(signal))
	}
}

// TestBot_AgentsError surfaces a list error.
func TestBot_AgentsError(t *testing.T) {
	bot, signal, orch, _ := newDispatchBot(t)
	orch.listAgtErr = errors.New("down")
	if err := bot.agentsOrDispatch(context.Background(), dmMsg("agents"), testOperator, Intent{Kind: IntentAgents}); err != nil {
		t.Fatalf("agents: %v", err)
	}
	if !contains(lastSend(signal), "❌") {
		t.Fatalf("body = %q", lastSend(signal))
	}
}

// TestBot_Workflow starts a job and reports the id.
func TestBot_Workflow(t *testing.T) {
	bot, signal, orch, _ := newDispatchBot(t)
	if err := bot.workflowOrDispatch(context.Background(), dmMsg("workflow x"), testOperator, Intent{Kind: IntentWorkflow, WorkflowName: "x"}); err != nil {
		t.Fatalf("workflow: %v", err)
	}
	if orch.jobs != 1 {
		t.Fatalf("jobs = %d", orch.jobs)
	}
	if !contains(lastSend(signal), "job-1") {
		t.Fatalf("body = %q", lastSend(signal))
	}
}

// TestBot_WorkflowError surfaces a StartJob error.
func TestBot_WorkflowError(t *testing.T) {
	bot, signal, orch, _ := newDispatchBot(t)
	orch.jobErr = errors.New("nope")
	if err := bot.workflowOrDispatch(context.Background(), dmMsg("workflow x"), testOperator, Intent{Kind: IntentWorkflow, WorkflowName: "x"}); err != nil {
		t.Fatalf("workflow: %v", err)
	}
	if !contains(lastSend(signal), "❌") {
		t.Fatalf("body = %q", lastSend(signal))
	}
}

// TestBot_SettingsShow renders the current verbosity prefs when no keys
// are supplied.
func TestBot_SettingsShow(t *testing.T) {
	bot, signal, _, _ := newDispatchBot(t)
	if err := bot.settingsOrDispatch(context.Background(), dmMsg("/settings"), testOperator, Intent{Kind: IntentSettings}); err != nil {
		t.Fatalf("settings: %v", err)
	}
	if !contains(lastSend(signal), "stream_tokens=") {
		t.Fatalf("body = %q", lastSend(signal))
	}
}

// TestBot_SettingsUpdate applies all supported keys and persists them.
func TestBot_SettingsUpdate(t *testing.T) {
	bot, signal, _, mgr := newDispatchBot(t)
	intent := Intent{Kind: IntentSettings, Settings: map[string]string{
		"stream_tokens":     "false",
		"tool_details":      "off",
		"file_ops":          "no",
		"typing_indicators": "false",
		"read_receipts":     "false",
	}}
	if err := bot.settingsOrDispatch(context.Background(), dmMsg("/settings ..."), testOperator, intent); err != nil {
		t.Fatalf("settings: %v", err)
	}
	prefs := mgr.Get("conv-1", ConversationDM).Verbosity
	if prefs.StreamTokens || prefs.ToolDetails || prefs.FileOps || prefs.TypingIndicators || prefs.ReadReceipts {
		t.Fatalf("prefs not all disabled: %+v", prefs)
	}
	if !contains(lastSend(signal), "updated") {
		t.Fatalf("body = %q", lastSend(signal))
	}
}

// TestBot_PromptNotAttached prompts an unattached conversation.
func TestBot_PromptNotAttached(t *testing.T) {
	bot, signal, orch, _ := newDispatchBot(t)
	if err := bot.promptOrDispatch(context.Background(), dmMsg("hi"), testOperator, Intent{Kind: IntentPrompt, Prompt: "hi"}); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	if orch.prompts != 0 {
		t.Fatalf("should not prompt when unattached")
	}
	if !contains(lastSend(signal), "not attached") {
		t.Fatalf("body = %q", lastSend(signal))
	}
}

// TestBot_PromptError surfaces a prompt error.
func TestBot_PromptError(t *testing.T) {
	bot, signal, orch, mgr := newDispatchBot(t)
	_ = mgr.Attach("conv-1", "sess-P", ConversationDM, testOperator)
	orch.promptErr = errors.New("boom")
	if err := bot.promptOrDispatch(context.Background(), dmMsg("hi"), testOperator, Intent{Kind: IntentPrompt, Prompt: "hi"}); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	if !contains(lastSend(signal), "prompt failed") {
		t.Fatalf("body = %q", lastSend(signal))
	}
}

// TestBot_AttachError surfaces a manager conflict on attach.
func TestBot_AttachError(t *testing.T) {
	bot, signal, _, mgr := newDispatchBot(t)
	_ = mgr.Attach("conv-1", "sess-A", ConversationDM, testOperator)
	// Attaching a different session is a conflict.
	if err := bot.attachOrDispatch(context.Background(), dmMsg("attach sess-B"), testOperator, Intent{Kind: IntentAttach, SessionID: "sess-B"}); err != nil {
		t.Fatalf("attach: %v", err)
	}
	if !contains(lastSend(signal), "❌") {
		t.Fatalf("body = %q", lastSend(signal))
	}
}

// TestBot_SpawnError surfaces a spawn error.
func TestBot_SpawnError(t *testing.T) {
	bot, signal, orch, _ := newDispatchBot(t)
	orch.spawnErr = errors.New("no capacity")
	if err := bot.spawnOrDispatch(context.Background(), dmMsg("spawn claude"), testOperator, Intent{Kind: IntentSpawn, Backend: "claude"}); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	if !contains(lastSend(signal), "spawn failed") {
		t.Fatalf("body = %q", lastSend(signal))
	}
}

// TestRenderVerbosityPrefs renders every flag.
func TestRenderVerbosityPrefs(t *testing.T) {
	out := renderVerbosityPrefs(VerbosityPrefs{StreamTokens: true})
	for _, key := range []string{"stream_tokens=true", "tool_details=false", "file_ops=false", "typing_indicators=false", "read_receipts=false"} {
		if !contains(out, key) {
			t.Fatalf("missing %q in %q", key, out)
		}
	}
}

// TestYamlish parses boolean-ish strings.
func TestYamlish(t *testing.T) {
	truthy := []string{"1", "true", "yes", "on", "y", "t", "TRUE", " Yes "}
	for _, s := range truthy {
		if !yamlish(s) {
			t.Fatalf("%q should be true", s)
		}
	}
	for _, s := range []string{"0", "false", "no", "", "maybe"} {
		if yamlish(s) {
			t.Fatalf("%q should be false", s)
		}
	}
}
