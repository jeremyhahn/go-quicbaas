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
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// OrchestratorSDK is the subset of the QuicBaaS orchestrator client the
// bot uses. Declared as an interface so tests can substitute a mock.
type OrchestratorSDK interface {
	SpawnSession(ctx context.Context, req *orchestratorSDK.SpawnSessionRequest) (*orchestratorSDK.SpawnSessionResponse, error)
	PromptSession(ctx context.Context, req *orchestratorSDK.PromptSessionRequest) (*orchestratorSDK.PromptSessionResponse, error)
	CloseSession(ctx context.Context, req *orchestratorSDK.CloseSessionRequest) (*orchestratorSDK.CloseSessionResponse, error)
	ListSessions(ctx context.Context, req *orchestratorSDK.ListSessionsRequest) (*orchestratorSDK.ListSessionsResponse, error)
	ListAgents(ctx context.Context, req *orchestratorSDK.ListAgentsRequest) (*orchestratorSDK.ListAgentsResponse, error)
	SessionStatus(ctx context.Context, req *orchestratorSDK.SessionStatusRequest) (*orchestratorSDK.SessionStatusResponse, error)
	StartJob(ctx context.Context, req *orchestratorSDK.StartJobRequest) (*orchestratorSDK.StartJobResponse, error)
	SubscribeSessionEvents(ctx context.Context, req *orchestratorSDK.SubscribeSessionEventsRequest) (<-chan *orchestratorSDK.SessionEventEnvelope, error)
}

// SignalIO is the subset of SignalClient the bot drives. Declared as
// an interface so tests can inject a FakeSignalIO.
type SignalIO interface {
	Sender
	Incoming() <-chan InboundMessage
}

// BotConfigRuntime carries the assembled runtime dependencies that the
// CLI build layer (`cmd/quicbaas/signalbridge.go`) injects.
type BotConfigRuntime struct {
	Config       *BotConfig
	Signal       SignalIO
	Orchestrator OrchestratorSDK
	Auth         AuthBridge
	Manager      *SessionManager
	Translator   *EventTranslator
	Parser       *IntentParser
	Logger       interfaces.Logger
}

// OrchestratorBot is the top-level runtime. Run blocks until Stop is
// called or the context is cancelled. All goroutines are tracked and
// drained on Stop so there are no leaks at shutdown.
type OrchestratorBot struct {
	cfg          *BotConfig
	signal       SignalIO
	orchestrator OrchestratorSDK
	auth         AuthBridge
	manager      *SessionManager
	translator   *EventTranslator
	parser       *IntentParser
	logger       interfaces.Logger

	cancelMu sync.Mutex
	cancel   context.CancelFunc
	wg       sync.WaitGroup

	// subscriptions tracks the per-session event-forward goroutines so
	// detach + session closure can cancel only that branch without
	// tearing down the whole bot.
	subsMu        sync.Mutex
	subscriptions map[string]context.CancelFunc

	running atomic.Bool
}

// NewOrchestratorBot validates the runtime and returns a bot ready to
// Run.
func NewOrchestratorBot(rt BotConfigRuntime) (*OrchestratorBot, error) {
	const op = "signalbridge.NewOrchestratorBot"
	if rt.Config == nil {
		return nil, newError(op, ErrKindInvalidInput, "Config is required")
	}
	if rt.Signal == nil {
		return nil, newError(op, ErrKindInvalidInput, "Signal is required")
	}
	if rt.Orchestrator == nil {
		return nil, newError(op, ErrKindInvalidInput, "Orchestrator is required")
	}
	if rt.Auth == nil {
		return nil, newError(op, ErrKindInvalidInput, "Auth is required")
	}
	if rt.Manager == nil {
		return nil, newError(op, ErrKindInvalidInput, "Manager is required")
	}
	if rt.Translator == nil {
		return nil, newError(op, ErrKindInvalidInput, "Translator is required")
	}
	if rt.Parser == nil {
		rt.Parser = NewIntentParser()
	}
	if rt.Logger == nil {
		return nil, newError(op, ErrKindInvalidInput, "Logger is required")
	}
	return &OrchestratorBot{
		cfg:           rt.Config,
		signal:        rt.Signal,
		orchestrator:  rt.Orchestrator,
		auth:          rt.Auth,
		manager:       rt.Manager,
		translator:    rt.Translator,
		parser:        rt.Parser,
		logger:        rt.Logger,
		subscriptions: make(map[string]context.CancelFunc),
	}, nil
}

// Run blocks until Stop is called or ctx is cancelled.
func (b *OrchestratorBot) Run(ctx context.Context) error {
	const op = "signalbridge.OrchestratorBot.Run"
	if !b.running.CompareAndSwap(false, true) {
		return newError(op, ErrKindConflict, "bot already running")
	}
	defer b.running.Store(false)

	runCtx, cancel := context.WithCancel(ctx)
	b.cancelMu.Lock()
	b.cancel = cancel
	b.cancelMu.Unlock()
	defer cancel()

	// Resume subscriptions for any session persisted as attached.
	for _, sid := range b.manager.AttachedSessions() {
		b.startEventSubscription(runCtx, sid)
	}

	b.wg.Add(1)
	go b.inboundLoop(runCtx)

	// Block on the run context. The inbound loop returns when ctx is
	// cancelled or the Signal channel closes, at which point we drain
	// the wait group and return.
	<-runCtx.Done()
	b.wg.Wait()

	b.subsMu.Lock()
	for _, c := range b.subscriptions {
		c()
	}
	b.subscriptions = nil
	b.subsMu.Unlock()
	return nil
}

// Stop requests shutdown. Concurrent Run loops observe the context
// cancellation and exit.
func (b *OrchestratorBot) Stop() error {
	b.cancelMu.Lock()
	cancel := b.cancel
	b.cancelMu.Unlock()
	if cancel != nil {
		cancel()
	}
	return nil
}

// inboundLoop reads messages from SignalIO and dispatches them.
func (b *OrchestratorBot) inboundLoop(ctx context.Context) {
	defer b.wg.Done()
	in := b.signal.Incoming()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-in:
			if !ok {
				return
			}
			if err := b.handleInbound(ctx, msg); err != nil {
				b.logger.WithError(err).Warn("signalbridge: handleInbound",
					"conversation_id", msg.ConversationID,
					"sender", msg.SenderACI,
				)
			}
		}
	}
}

// handleInbound processes a single inbound Signal message end-to-end.
func (b *OrchestratorBot) handleInbound(ctx context.Context, msg InboundMessage) error {

	operator, err := b.auth.ResolveOperator(ctx, msg.SenderACI, msg.ConversationID, msg.Kind)
	if err != nil {
		// Unauthorized senders get a one-line explanation.
		reply := "🔒 " + err.Error()
		if IsKind(err, ErrKindUnauthorized) {
			reply = "🔒 not authorized to issue commands: " + err.Error()
		}
		return b.signal.Send(ctx, OutboundMessage{
			ConversationID: msg.ConversationID,
			Kind:           msg.Kind,
			Body:           reply,
			Timestamp:      clockNowMilli(),
		})
	}

	state := b.manager.Get(msg.ConversationID, msg.Kind)
	intent := b.parser.Parse(msg.Body, state.AttachedSessionID != "")

	handler, ok := intentHandlers[intent.Kind]
	if !ok {
		return b.replyUnknown(ctx, msg)
	}
	return handler(b, ctx, msg, *operator, intent)
}

// intentHandler is the shape of per-intent dispatchers.
type intentHandler func(b *OrchestratorBot, ctx context.Context, msg InboundMessage, operator OperatorIdentity, intent Intent) error

// intentHandlers routes IntentKind → handler.
var intentHandlers = map[IntentKind]intentHandler{
	IntentSpawn:    (*OrchestratorBot).spawnOrDispatch,
	IntentAttach:   (*OrchestratorBot).attachOrDispatch,
	IntentDetach:   (*OrchestratorBot).detachOrDispatch,
	IntentPrompt:   (*OrchestratorBot).promptOrDispatch,
	IntentClose:    (*OrchestratorBot).closeOrDispatch,
	IntentStatus:   (*OrchestratorBot).statusOrDispatch,
	IntentSessions: (*OrchestratorBot).sessionsOrDispatch,
	IntentAgents:   (*OrchestratorBot).agentsOrDispatch,
	IntentWorkflow: (*OrchestratorBot).workflowOrDispatch,
	IntentSettings: (*OrchestratorBot).settingsOrDispatch,
	IntentHelp:     (*OrchestratorBot).helpOrDispatch,
	IntentUnknown:  (*OrchestratorBot).unknownOrDispatch,
}

// The method-value-to-function-pointer trampolines below route map
// dispatch entries to their receiver methods.

func (b *OrchestratorBot) spawnOrDispatch(ctx context.Context, msg InboundMessage, operator OperatorIdentity, intent Intent) error {
	req := &orchestratorSDK.SpawnSessionRequest{
		Backend:      intent.Backend,
		Model:        intent.Model,
		HostAffinity: intent.Host,
	}
	resp, err := b.orchestrator.SpawnSession(ctx, req)
	if err != nil {
		return b.replyError(ctx, msg, "spawn failed: "+err.Error())
	}
	// Auto-attach the newly spawned session to this conversation.
	if err := b.manager.Attach(msg.ConversationID, resp.SessionID, msg.Kind, operator); err != nil {
		return b.replyError(ctx, msg, "attached spawn: "+err.Error())
	}
	b.startEventSubscription(ctx, resp.SessionID)
	if intent.Goal != "" {
		_, _ = b.orchestrator.PromptSession(ctx, &orchestratorSDK.PromptSessionRequest{
			SessionID: resp.SessionID,
			Prompt:    intent.Goal,
		})
	}
	return b.reply(ctx, msg, fmt.Sprintf("🚀 spawned %s session %s", resp.Backend, resp.SessionID))
}

func (b *OrchestratorBot) attachOrDispatch(ctx context.Context, msg InboundMessage, operator OperatorIdentity, intent Intent) error {
	if err := b.manager.Attach(msg.ConversationID, intent.SessionID, msg.Kind, operator); err != nil {
		return b.replyError(ctx, msg, err.Error())
	}
	b.startEventSubscription(ctx, intent.SessionID)
	return b.reply(ctx, msg, fmt.Sprintf("🔗 attached to session %s", intent.SessionID))
}

func (b *OrchestratorBot) detachOrDispatch(ctx context.Context, msg InboundMessage, operator OperatorIdentity, intent Intent) error {
	state := b.manager.Get(msg.ConversationID, msg.Kind)
	if state.AttachedSessionID == "" {
		return b.reply(ctx, msg, "(not attached)")
	}
	sid := state.AttachedSessionID
	if err := b.manager.Detach(msg.ConversationID); err != nil {
		return b.replyError(ctx, msg, err.Error())
	}
	b.maybeStopSubscription(sid)
	return b.reply(ctx, msg, fmt.Sprintf("👋 detached from %s", sid))
}

func (b *OrchestratorBot) promptOrDispatch(ctx context.Context, msg InboundMessage, operator OperatorIdentity, intent Intent) error {
	state := b.manager.Get(msg.ConversationID, msg.Kind)
	if state.AttachedSessionID == "" {
		return b.reply(ctx, msg, "(not attached; use `spawn` or `attach <session-id>`)")
	}
	if _, err := b.orchestrator.PromptSession(ctx, &orchestratorSDK.PromptSessionRequest{
		SessionID: state.AttachedSessionID,
		Prompt:    intent.Prompt,
	}); err != nil {
		return b.replyError(ctx, msg, "prompt failed: "+err.Error())
	}
	return nil
}

func (b *OrchestratorBot) closeOrDispatch(ctx context.Context, msg InboundMessage, operator OperatorIdentity, intent Intent) error {
	state := b.manager.Get(msg.ConversationID, msg.Kind)
	if state.AttachedSessionID == "" {
		return b.reply(ctx, msg, "(nothing to close)")
	}
	sid := state.AttachedSessionID
	_, err := b.orchestrator.CloseSession(ctx, &orchestratorSDK.CloseSessionRequest{SessionID: sid})
	if err != nil {
		return b.replyError(ctx, msg, "close failed: "+err.Error())
	}
	_ = b.manager.Detach(msg.ConversationID)
	b.maybeStopSubscription(sid)
	return b.reply(ctx, msg, fmt.Sprintf("🛑 closed %s", sid))
}

func (b *OrchestratorBot) statusOrDispatch(ctx context.Context, msg InboundMessage, operator OperatorIdentity, intent Intent) error {
	state := b.manager.Get(msg.ConversationID, msg.Kind)
	if state.AttachedSessionID == "" {
		return b.reply(ctx, msg, "(not attached)")
	}
	resp, err := b.orchestrator.SessionStatus(ctx, &orchestratorSDK.SessionStatusRequest{SessionID: state.AttachedSessionID})
	if err != nil {
		return b.replyError(ctx, msg, err.Error())
	}
	body := fmt.Sprintf("🧭 session %s — %s (%s)", resp.SessionID, resp.Status, resp.Backend)
	return b.reply(ctx, msg, body)
}

func (b *OrchestratorBot) sessionsOrDispatch(ctx context.Context, msg InboundMessage, operator OperatorIdentity, intent Intent) error {
	resp, err := b.orchestrator.ListSessions(ctx, &orchestratorSDK.ListSessionsRequest{})
	if err != nil {
		return b.replyError(ctx, msg, err.Error())
	}
	if resp.Count == 0 {
		return b.reply(ctx, msg, "(no active sessions)")
	}
	var sb strings.Builder
	sb.WriteString("📋 sessions:\n")
	for _, s := range resp.Sessions {
		sb.WriteString(fmt.Sprintf("  • %s — %s (%s)\n", s.SessionID, s.Status, s.Backend))
	}
	return b.reply(ctx, msg, sb.String())
}

func (b *OrchestratorBot) agentsOrDispatch(ctx context.Context, msg InboundMessage, operator OperatorIdentity, intent Intent) error {
	resp, err := b.orchestrator.ListAgents(ctx, &orchestratorSDK.ListAgentsRequest{TenantID: operator.TenantID})
	if err != nil {
		return b.replyError(ctx, msg, err.Error())
	}
	if resp.Count == 0 {
		return b.reply(ctx, msg, "(no agents registered)")
	}
	var sb strings.Builder
	sb.WriteString("🤖 agents:\n")
	for _, a := range resp.Agents {
		sb.WriteString(fmt.Sprintf("  • %s @ %s — %s (%s)\n", a.Name, a.Host, a.Status, a.Backend))
	}
	return b.reply(ctx, msg, sb.String())
}

func (b *OrchestratorBot) workflowOrDispatch(ctx context.Context, msg InboundMessage, operator OperatorIdentity, intent Intent) error {
	req := &orchestratorSDK.StartJobRequest{
		TenantID:     operator.TenantID,
		Description:  "workflow:" + intent.WorkflowName,
		WorkflowName: intent.WorkflowName,
	}
	resp, err := b.orchestrator.StartJob(ctx, req)
	if err != nil {
		return b.replyError(ctx, msg, err.Error())
	}
	return b.reply(ctx, msg, fmt.Sprintf("⏱️ started job %s", resp.JobID))
}

func (b *OrchestratorBot) settingsOrDispatch(ctx context.Context, msg InboundMessage, operator OperatorIdentity, intent Intent) error {
	state := b.manager.Get(msg.ConversationID, msg.Kind)
	if len(intent.Settings) == 0 {
		return b.reply(ctx, msg, renderVerbosityPrefs(state.Verbosity))
	}
	prefs := state.Verbosity
	for k, v := range intent.Settings {
		switch strings.ToLower(k) {
		case "stream_tokens":
			prefs.StreamTokens = yamlish(v)
		case "tool_details":
			prefs.ToolDetails = yamlish(v)
		case "file_ops":
			prefs.FileOps = yamlish(v)
		case "typing_indicators":
			prefs.TypingIndicators = yamlish(v)
		case "read_receipts":
			prefs.ReadReceipts = yamlish(v)
		}
	}
	if err := b.manager.SetVerbosity(msg.ConversationID, msg.Kind, prefs); err != nil {
		return b.replyError(ctx, msg, err.Error())
	}
	return b.reply(ctx, msg, "⚙️ updated:\n"+renderVerbosityPrefs(prefs))
}

func (b *OrchestratorBot) helpOrDispatch(ctx context.Context, msg InboundMessage, operator OperatorIdentity, intent Intent) error {
	return b.reply(ctx, msg, helpText)
}

func (b *OrchestratorBot) unknownOrDispatch(ctx context.Context, msg InboundMessage, operator OperatorIdentity, intent Intent) error {
	return b.replyUnknown(ctx, msg)
}

// startEventSubscription launches a goroutine that forwards events
// for sessionID to all conversations attached to it.
func (b *OrchestratorBot) startEventSubscription(ctx context.Context, sessionID string) {
	b.subsMu.Lock()
	defer b.subsMu.Unlock()
	if _, exists := b.subscriptions[sessionID]; exists {
		return
	}
	subCtx, cancel := context.WithCancel(ctx)
	b.subscriptions[sessionID] = cancel

	watermark := uint64(0)
	for _, conv := range b.manager.ConversationsForSession(sessionID) {
		if conv.LastDeliveredSeq > watermark {
			watermark = conv.LastDeliveredSeq
		}
	}

	req := &orchestratorSDK.SubscribeSessionEventsRequest{
		TenantID:     b.cfg.TenantID,
		SessionID:    sessionID,
		FromSequence: watermark + 1,
		Follow:       true,
	}
	ch, err := b.orchestrator.SubscribeSessionEvents(subCtx, req)
	if err != nil {
		b.logger.WithError(err).Warn("signalbridge: subscribe failed", "session_id", sessionID)
		cancel()
		delete(b.subscriptions, sessionID)
		return
	}
	b.wg.Add(1)
	go b.forwardLoop(subCtx, sessionID, ch)
}

// forwardLoop drains session events into the event translator for
// every attached conversation, then advances the watermark.
func (b *OrchestratorBot) forwardLoop(ctx context.Context, sessionID string, ch <-chan *orchestratorSDK.SessionEventEnvelope) {
	defer b.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case env, ok := <-ch:
			if !ok {
				return
			}
			for _, conv := range b.manager.ConversationsForSession(sessionID) {
				// Exactly-once: skip envelopes at or below watermark.
				if env.Sequence <= conv.LastDeliveredSeq {
					continue
				}
				if err := b.translator.Translate(ctx, conv.ConversationID, conv.Kind, env); err != nil {
					b.logger.WithError(err).Warn("signalbridge: translate failed",
						"session_id", sessionID,
						"sequence", env.Sequence,
					)
					continue
				}
				if err := b.manager.AdvanceWatermark(conv.ConversationID, env.Sequence); err != nil {
					b.logger.WithError(err).Warn("signalbridge: watermark advance failed",
						"conversation_id", conv.ConversationID,
					)
				}
			}
		}
	}
}

// maybeStopSubscription cancels a per-session subscription once no
// conversation is still attached.
func (b *OrchestratorBot) maybeStopSubscription(sessionID string) {
	if convs := b.manager.ConversationsForSession(sessionID); len(convs) > 0 {
		return
	}
	b.subsMu.Lock()
	defer b.subsMu.Unlock()
	cancel, ok := b.subscriptions[sessionID]
	if ok {
		cancel()
		delete(b.subscriptions, sessionID)
	}
}

// reply sends a single text message back to the originating
// conversation.
func (b *OrchestratorBot) reply(ctx context.Context, msg InboundMessage, body string) error {
	return b.signal.Send(ctx, OutboundMessage{
		ConversationID: msg.ConversationID,
		Kind:           msg.Kind,
		Body:           body,
		Timestamp:      clockNowMilli(),
	})
}

// replyError formats err as an operator-visible message.
func (b *OrchestratorBot) replyError(ctx context.Context, msg InboundMessage, body string) error {
	return b.reply(ctx, msg, "❌ "+body)
}

// replyUnknown renders the "unknown command" help reply.
func (b *OrchestratorBot) replyUnknown(ctx context.Context, msg InboundMessage) error {
	return b.reply(ctx, msg, "❓ unknown command — try `help`")
}

// helpText is the static help reply. Kept human-readable.
const helpText = `📘 QuicBaaS Orchestrator Bot

Commands:
  spawn <backend> [on <host>] [with <model>] [for <goal>]
  attach <session-id>
  detach
  prompt <text>                (implicit when attached + plain text)
  close | stop                 close the attached session
  status                       show attached session status
  sessions                     list active sessions
  agents                       list registered agents
  workflow <name>              start a workflow job
  /settings k=v [k=v ...]      adjust per-conversation prefs
  help | ?                     this message

Settings keys: stream_tokens, tool_details, file_ops, typing_indicators, read_receipts.`

// renderVerbosityPrefs renders prefs as an operator-readable summary.
func renderVerbosityPrefs(p VerbosityPrefs) string {
	return fmt.Sprintf(
		"stream_tokens=%v\ntool_details=%v\nfile_ops=%v\ntyping_indicators=%v\nread_receipts=%v",
		p.StreamTokens, p.ToolDetails, p.FileOps, p.TypingIndicators, p.ReadReceipts,
	)
}

// yamlish parses common boolean-ish strings.
func yamlish(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "on", "y", "t":
		return true
	}
	return false
}
