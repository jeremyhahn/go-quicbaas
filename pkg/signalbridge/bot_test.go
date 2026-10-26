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
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// fakeSignalIO is an in-memory SignalIO backed by a channel of
// inbound messages and a fakeSender.
type fakeSignalIO struct {
	fakeSender
	in chan InboundMessage
}

func newFakeSignalIO() *fakeSignalIO {
	return &fakeSignalIO{in: make(chan InboundMessage, 64)}
}

func (f *fakeSignalIO) Incoming() <-chan InboundMessage { return f.in }

// mockOrchestrator records calls and returns stubbed responses.
type mockOrchestrator struct {
	mu        sync.Mutex
	spawns    []*orchestratorSDK.SpawnSessionRequest
	prompts   []*orchestratorSDK.PromptSessionRequest
	closes    []*orchestratorSDK.CloseSessionRequest
	sessions  []*orchestratorSDK.SessionStatusResponse
	agents    []*orchestratorSDK.FleetNode
	jobs      int
	eventChan chan *orchestratorSDK.SessionEventEnvelope
	promptErr error
}

func newMockOrchestrator() *mockOrchestrator {
	return &mockOrchestrator{
		sessions:  []*orchestratorSDK.SessionStatusResponse{{SessionID: "sess-1", Status: "running", Backend: "claude"}},
		eventChan: make(chan *orchestratorSDK.SessionEventEnvelope, 16),
	}
}

func (m *mockOrchestrator) SpawnSession(ctx context.Context, req *orchestratorSDK.SpawnSessionRequest) (*orchestratorSDK.SpawnSessionResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.spawns = append(m.spawns, req)
	return &orchestratorSDK.SpawnSessionResponse{SessionID: "sess-new", Backend: req.Backend}, nil
}
func (m *mockOrchestrator) PromptSession(ctx context.Context, req *orchestratorSDK.PromptSessionRequest) (*orchestratorSDK.PromptSessionResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prompts = append(m.prompts, req)
	if m.promptErr != nil {
		return nil, m.promptErr
	}
	return &orchestratorSDK.PromptSessionResponse{SessionID: req.SessionID}, nil
}
func (m *mockOrchestrator) CloseSession(ctx context.Context, req *orchestratorSDK.CloseSessionRequest) (*orchestratorSDK.CloseSessionResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closes = append(m.closes, req)
	return &orchestratorSDK.CloseSessionResponse{SessionID: req.SessionID, Closed: true}, nil
}
func (m *mockOrchestrator) ListSessions(ctx context.Context, req *orchestratorSDK.ListSessionsRequest) (*orchestratorSDK.ListSessionsResponse, error) {
	return &orchestratorSDK.ListSessionsResponse{Sessions: m.sessions, Count: len(m.sessions)}, nil
}
func (m *mockOrchestrator) ListFleetNodes(ctx context.Context, req *orchestratorSDK.ListFleetNodesRequest) (*orchestratorSDK.ListFleetNodesResponse, error) {
	return &orchestratorSDK.ListFleetNodesResponse{Nodes: m.agents, Count: len(m.agents)}, nil
}
func (m *mockOrchestrator) SessionStatus(ctx context.Context, req *orchestratorSDK.SessionStatusRequest) (*orchestratorSDK.SessionStatusResponse, error) {
	return &orchestratorSDK.SessionStatusResponse{SessionID: req.SessionID, Backend: "claude", Status: "running"}, nil
}
func (m *mockOrchestrator) StartJob(ctx context.Context, req *orchestratorSDK.StartJobRequest) (*orchestratorSDK.StartJobResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.jobs++
	return &orchestratorSDK.StartJobResponse{JobID: "job-1"}, nil
}
func (m *mockOrchestrator) SubscribeSessionEvents(ctx context.Context, req *orchestratorSDK.SubscribeSessionEventsRequest) (<-chan *orchestratorSDK.SessionEventEnvelope, error) {
	return m.eventChan, nil
}

// noopLogger is a minimal interfaces.Logger that discards everything.
// Keeping tests free of log output keeps `go test -v` readable.
type noopLogger struct{}

func (noopLogger) Debug(string, ...any)                                  {}
func (noopLogger) Debugf(string, ...interface{})                         {}
func (noopLogger) Info(string, ...any)                                   {}
func (noopLogger) Infof(string, ...interface{})                          {}
func (noopLogger) Warn(string, ...any)                                   {}
func (noopLogger) Warnf(string, ...interface{})                          {}
func (noopLogger) Error(string, ...any)                                  {}
func (noopLogger) Errorf(string, ...interface{})                         {}
func (noopLogger) Fatal(string)                                          {}
func (noopLogger) Fatalf(string, ...interface{})                         {}
func (l noopLogger) WithField(string, interface{}) interfaces.Logger     { return l }
func (l noopLogger) WithFields(map[string]interface{}) interfaces.Logger { return l }
func (l noopLogger) WithError(error) interfaces.Logger                   { return l }

// newTestBot assembles a bot backed entirely by in-memory fakes.
func newTestBot(t *testing.T) (*OrchestratorBot, *fakeSignalIO, *mockOrchestrator, *SessionManager) {
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
	orch := newMockOrchestrator()
	auth, err := NewAuthBridge(AuthBridgeConfig{
		Mode:           AuthModeImplicit,
		TenantID:       "t",
		ServiceAccount: "bot",
	})
	if err != nil {
		t.Fatalf("auth: %v", err)
	}
	bot, err := NewOrchestratorBot(BotConfigRuntime{
		Config:       &BotConfig{BotIdentity: "+15551234567", GoSignalURL: "https://x", QuicBaaSAddr: "x", TenantID: "t", AuthMode: AuthModeImplicit, ServiceAccount: "bot", VerbosityDefaults: DefaultVerbosityPrefs()},
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

// runBotBriefly starts the bot, sends it msgs, waits until the bot has
// processed them, then stops the bot. Uses a done-channel handshake
// rather than time.Sleep.
func runBotBriefly(t *testing.T, bot *OrchestratorBot, signal *fakeSignalIO, inbound []InboundMessage) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runDone := make(chan error, 1)
	go func() { runDone <- bot.Run(ctx) }()

	for _, m := range inbound {
		signal.in <- m
	}
	// Wait for the fakeSignalIO to have produced replies to all
	// inbound messages. We poll with a select because the bot returns
	// responses asynchronously; use a completion signal keyed off the
	// fakeSender's message count.
	waitFor(t, func() bool {
		signal.mu.Lock()
		defer signal.mu.Unlock()
		return len(signal.sends) >= len(inbound)
	})

	_ = bot.Stop()
	select {
	case err := <-runDone:
		if err != nil {
			t.Fatalf("bot run: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("bot did not stop in time")
	}
}

// waitFor spins until cond returns true or a ~2s deadline passes.
// Intended for test synchronization without time.Sleep; uses a
// ticker-less polling loop with a done channel so the test runner can
// interrupt.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		if cond() {
			return
		}
		select {
		case <-tick.C:
			if time.Now().After(deadline) {
				t.Fatalf("waitFor: deadline exceeded")
			}
		}
	}
}

// TestBot_SpawnAndPrompt exercises the happy path: a spawn attaches
// the new session, implicit prompt sends a PromptSession call.
// TestBot_SpawnGoalPromptFailureSurfaced ensures a failed goal prompt is
// reported to the sender instead of being dropped.
func TestBot_SpawnGoalPromptFailureSurfaced(t *testing.T) {
	bot, signal, orch, _ := newTestBot(t)
	orch.promptErr = errors.New("prompt boom")
	msg := InboundMessage{ConversationID: "conv-1", Kind: ConversationDM, SenderACI: "aci-1"}
	intent := Intent{Kind: IntentSpawn, Backend: "claude", Goal: "do the thing"}
	if err := bot.spawnOrDispatch(context.Background(), msg, OperatorIdentity{}, intent); err != nil {
		t.Fatalf("spawnOrDispatch: %v", err)
	}
	signal.mu.Lock()
	defer signal.mu.Unlock()
	if len(signal.sends) != 1 || !strings.Contains(signal.sends[0].Body, "goal prompt failed: prompt boom") {
		t.Fatalf("sends: %+v", signal.sends)
	}
}

func TestBot_SpawnAndPrompt(t *testing.T) {
	bot, signal, orch, mgr := newTestBot(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runDone := make(chan error, 1)
	go func() { runDone <- bot.Run(ctx) }()
	signal.in <- InboundMessage{ConversationID: "conv-1", Kind: ConversationDM, SenderACI: "aci-1", Body: "spawn claude"}
	signal.in <- InboundMessage{ConversationID: "conv-1", Kind: ConversationDM, SenderACI: "aci-1", Body: "hello there"}
	waitFor(t, func() bool {
		orch.mu.Lock()
		defer orch.mu.Unlock()
		return len(orch.spawns) >= 1 && len(orch.prompts) >= 1
	})
	_ = bot.Stop()
	<-runDone
	if len(orch.spawns) != 1 || orch.spawns[0].Backend != "claude" {
		t.Fatalf("spawn calls: %+v", orch.spawns)
	}
	if len(orch.prompts) != 1 || orch.prompts[0].Prompt != "hello there" {
		t.Fatalf("prompt calls: %+v", orch.prompts)
	}
	state := mgr.Get("conv-1", ConversationDM)
	if state.AttachedSessionID != "sess-new" {
		t.Fatalf("auto-attach failed: %q", state.AttachedSessionID)
	}
}

// TestBot_HelpCommand returns the help text.
func TestBot_HelpCommand(t *testing.T) {
	bot, signal, _, _ := newTestBot(t)
	runBotBriefly(t, bot, signal, []InboundMessage{
		{ConversationID: "c", Kind: ConversationDM, SenderACI: "a", Body: "help"},
	})
	if len(signal.sends) != 1 {
		t.Fatalf("expected 1 reply got %d", len(signal.sends))
	}
	if !contains(signal.sends[0].Body, "help") && !contains(signal.sends[0].Body, "Commands") {
		t.Fatalf("unexpected help body: %q", signal.sends[0].Body)
	}
}

// TestBot_UnknownCommand yields the ❓ reply when not attached.
func TestBot_UnknownCommand(t *testing.T) {
	bot, signal, _, _ := newTestBot(t)
	runBotBriefly(t, bot, signal, []InboundMessage{
		{ConversationID: "c", Kind: ConversationDM, SenderACI: "a", Body: "potato"},
	})
	if len(signal.sends) == 0 || !contains(signal.sends[0].Body, "unknown") {
		t.Fatalf("unexpected reply: %+v", signal.sends)
	}
}

// TestBot_AttachAndForwardEvents verifies that a manual attach opens
// a subscription and forwards assistant chunks into the conversation.
func TestBot_AttachAndForwardEvents(t *testing.T) {
	bot, signal, orch, mgr := newTestBot(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runDone := make(chan error, 1)
	go func() { runDone <- bot.Run(ctx) }()

	signal.in <- InboundMessage{ConversationID: "c", Kind: ConversationDM, SenderACI: "a", Body: "attach sess-X"}
	waitFor(t, func() bool {
		return mgr.Get("c", ConversationDM).AttachedSessionID == "sess-X"
	})

	evt := orchestration.SessionEvent{
		Kind:           orchestration.KindAssistantChunk,
		AssistantChunk: &orchestration.AssistantChunkPayload{Text: "Hi!", StopReason: "end_turn"},
	}
	raw, _ := json.Marshal(&evt)
	orch.eventChan <- &orchestratorSDK.SessionEventEnvelope{
		SessionID:   "sess-X",
		Sequence:    1,
		PayloadJSON: raw,
	}

	// Wait on the WATERMARK, not on the send. forwardLoop translates first —
	// which is what performs the send — and advances the watermark after, so
	// a wait that stops at the send returns inside a window where the
	// watermark is still 0. That window is invisible on an idle machine and
	// lost this test under -race on a loaded CI runner
	// ("watermark not advanced: 0"). The watermark strictly follows the send,
	// so waiting on it and then asserting the send closes the window without
	// weakening either assertion.
	waitFor(t, func() bool {
		return mgr.Get("c", ConversationDM).LastDeliveredSeq == 1
	})

	signal.mu.Lock()
	delivered := false
	for _, s := range signal.sends {
		if s.Body == "Hi!" {
			delivered = true
			break
		}
	}
	signal.mu.Unlock()
	if !delivered {
		t.Fatal("assistant chunk was not forwarded to the conversation")
	}

	_ = bot.Stop()
	<-runDone
}

// TestBot_ExactlyOnceWatermark verifies events at or below watermark
// are not redelivered on resubscribe.
func TestBot_ExactlyOnceWatermark(t *testing.T) {
	bot, signal, orch, mgr := newTestBot(t)
	_ = mgr.Attach("c", "sess-X", ConversationDM, OperatorIdentity{TenantID: "t", SubjectID: "bot"})
	_ = mgr.AdvanceWatermark("c", 10)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runDone := make(chan error, 1)
	go func() { runDone <- bot.Run(ctx) }()

	// Event with sequence <= watermark should be dropped.
	evt := orchestration.SessionEvent{
		Kind:           orchestration.KindAssistantChunk,
		AssistantChunk: &orchestration.AssistantChunkPayload{Text: "stale"},
	}
	raw, _ := json.Marshal(&evt)
	orch.eventChan <- &orchestratorSDK.SessionEventEnvelope{
		SessionID:   "sess-X",
		Sequence:    5,
		PayloadJSON: raw,
	}
	// Fresh event must be delivered.
	raw2, _ := json.Marshal(&evt)
	orch.eventChan <- &orchestratorSDK.SessionEventEnvelope{
		SessionID:   "sess-X",
		Sequence:    11,
		PayloadJSON: raw2,
	}
	waitFor(t, func() bool {
		signal.mu.Lock()
		defer signal.mu.Unlock()
		return len(signal.sends) > 0
	})
	_ = bot.Stop()
	<-runDone

	signal.mu.Lock()
	defer signal.mu.Unlock()
	if len(signal.sends) != 1 {
		t.Fatalf("expected exactly one forwarded message; got %d", len(signal.sends))
	}
}

// TestNewOrchestratorBot_Errors rejects missing required fields.
func TestNewOrchestratorBot_Errors(t *testing.T) {
	if _, err := NewOrchestratorBot(BotConfigRuntime{}); err == nil {
		t.Fatalf("expected error on empty runtime")
	}
}

// TestBot_DoubleRun yields ErrKindConflict.
func TestBot_DoubleRun(t *testing.T) {
	bot, signal, _, _ := newTestBot(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var ran atomic.Bool
	go func() {
		ran.Store(true)
		_ = bot.Run(ctx)
	}()
	waitFor(t, func() bool { return ran.Load() })
	err := bot.Run(ctx)
	if !IsKind(err, ErrKindConflict) {
		t.Fatalf("want conflict got %v", err)
	}
	_ = bot.Stop()
	close(signal.in)
}

// TestBot_StopAsSoonAsRunning lands a Stop at the instant Run marks the
// bot running, with a parent context that is never cancelled: Run must
// still return.
func TestBot_StopAsSoonAsRunning(t *testing.T) {
	bot, signal, _, _ := newTestBot(t)
	defer close(signal.in)
	stopErr := make(chan error, 1)
	bot.onRunning = func() { stopErr <- bot.Stop() }

	runErr := make(chan error, 1)
	go func() { runErr <- bot.Run(context.Background()) }()

	if err := <-stopErr; err != nil {
		t.Fatalf("stop: %v", err)
	}
	select {
	case err := <-runErr:
		if err != nil {
			t.Fatalf("run: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after Stop landed as the bot started running")
	}
}

// contains is a tiny substring helper.
func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// TestFakeSenderBehavior verifies the test double itself. Included so
// failures in the real code don't get misdiagnosed as fake issues.
func TestFakeSenderBehavior(t *testing.T) {
	s := &fakeSender{}
	if err := s.Send(context.Background(), OutboundMessage{Body: "hi"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if len(s.sends) != 1 {
		t.Fatalf("want 1 send, got %d", len(s.sends))
	}
	s.failSends = true
	if err := s.Send(context.Background(), OutboundMessage{}); err == nil {
		t.Fatalf("expected forced failure")
	}
	if err := s.Edit(context.Background(), "c", 1, "x"); err != nil {
		t.Fatalf("edit: %v", err)
	}
	if err := errors.Unwrap(s.Send(context.Background(), OutboundMessage{})); err != nil {
		_ = err
	}
}
