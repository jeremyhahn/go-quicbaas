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
	"fmt"
	"strings"
	"sync"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// attachmentPromotionThreshold is the byte size above which a tool
// result is uploaded as a Signal attachment instead of inlined. 2 MB
// matches the requirements document.
const attachmentPromotionThreshold = 2 * 1024 * 1024

// maxInlineToolArgs bounds the length of rendered tool arguments in a
// "🔧 <tool>(<args>)" line. Longer blobs are truncated with an ellipsis.
const maxInlineToolArgs = 256

// maxInlineToolResult bounds the inline rendering of successful tool
// results. Applies even when the raw result is smaller than
// attachmentPromotionThreshold — keeps conversations readable.
const maxInlineToolResult = 1024

// Sender is the narrow subset of SignalClient the translator uses.
// Defined as an interface so tests can swap in a FakeSender that
// records Send/Edit calls.
type Sender interface {
	Send(ctx context.Context, msg OutboundMessage) error
	Edit(ctx context.Context, conversationID string, originalTimestamp int64, newBody string) error
}

// EventTranslator converts orchestrator SessionEvent envelopes into
// Signal messages. Per-conversation state (the timestamp of the
// current rolling assistant message, invocation thread anchors) is
// delegated to the SessionManager so it survives restart.
type EventTranslator struct {
	sender  Sender
	manager *SessionManager

	mu sync.Mutex
	// rollingBodies keeps the full assistant text accumulated per
	// rolling message. Persisting this across restart is intentionally
	// out of scope — after a crash the translator finalizes any open
	// rolling message as-is and starts fresh.
	rollingBodies map[string]*strings.Builder
}

// NewEventTranslator wires a translator to a sender and state
// manager.
func NewEventTranslator(sender Sender, manager *SessionManager) (*EventTranslator, error) {
	const op = "signalbridge.NewEventTranslator"
	if sender == nil {
		return nil, newError(op, ErrKindInvalidInput, "sender required")
	}
	if manager == nil {
		return nil, newError(op, ErrKindInvalidInput, "manager required")
	}
	return &EventTranslator{
		sender:        sender,
		manager:       manager,
		rollingBodies: make(map[string]*strings.Builder),
	}, nil
}

// Translate is the single entry point. Each envelope produces zero or
// more Sender calls based on the event kind and conversation prefs.
func (t *EventTranslator) Translate(ctx context.Context, conversationID string, kind ConversationKind, env *orchestratorSDK.SessionEventEnvelope) error {
	const op = "signalbridge.EventTranslator.Translate"
	if env == nil {
		return newError(op, ErrKindInvalidInput, "envelope required")
	}
	var evt orchestration.SessionEvent
	if err := json.Unmarshal(env.PayloadJSON, &evt); err != nil {
		return wrapError(op, ErrKindInvalidInput, err, "decode envelope payload")
	}

	state := t.manager.Get(conversationID, kind)
	prefs := state.Verbosity

	handler, ok := eventHandlers[evt.Kind]
	if !ok {
		return nil
	}
	return handler(ctx, t, conversationID, kind, prefs, &evt)
}

// translatorHandler is the shape of a per-event-kind handler. Map-based
// dispatch matches the CLAUDE.md guidance (O(1) lookup) and keeps the
// Translate function a one-liner.
type translatorHandler func(ctx context.Context, t *EventTranslator, conversationID string, kind ConversationKind, prefs VerbosityPrefs, evt *orchestration.SessionEvent) error

// eventHandlers is the dispatch table. Unknown kinds fall through to
// a no-op because new kinds added upstream should not break older
// bots.
var eventHandlers = map[orchestration.SessionEventKind]translatorHandler{
	orchestration.KindAssistantChunk:  handleAssistantChunk,
	orchestration.KindToolInvocation:  handleToolInvocation,
	orchestration.KindToolResult:      handleToolResult,
	orchestration.KindFileOp:          handleFileOp,
	orchestration.KindSystemMessage:   handleSystemMessage,
	orchestration.KindOperatorJoined:  handleOperatorJoined,
	orchestration.KindOperatorLeft:    handleOperatorLeft,
	orchestration.KindSessionClosed:   handleSessionClosed,
	orchestration.KindPromptSubmitted: handlePromptSubmitted,
	orchestration.KindSessionResumed:  handleSessionResumed,
}

// handleAssistantChunk coalesces streamed chunks into a single rolling
// Signal message via Edit, and finalizes on stop_reason.
func handleAssistantChunk(ctx context.Context, t *EventTranslator, conversationID string, kind ConversationKind, prefs VerbosityPrefs, evt *orchestration.SessionEvent) error {
	if evt.AssistantChunk == nil {
		return nil
	}
	chunk := evt.AssistantChunk
	if !prefs.StreamTokens {
		if chunk.StopReason == "" {
			return nil
		}
		// Buffered mode: emit the final accumulated body as one message.
		body := t.flushRollingBody(conversationID)
		body += chunk.Text
		return t.sender.Send(ctx, OutboundMessage{
			ConversationID: conversationID,
			Kind:           kind,
			Body:           body,
		})
	}

	state := t.manager.Get(conversationID, kind)
	rolling := state.RollingMsgTimestamp
	if rolling == 0 {
		// Start a new rolling message. Send inserts with Timestamp==0
		// which SignalClient fills in. We need to know the assigned
		// timestamp so Edit can reference it — we pre-assign one here
		// by letting Send allocate. To recover the allocated
		// timestamp, we pre-compute it client-side.
		ts := nextRollingTimestamp(t)
		msg := OutboundMessage{
			ConversationID: conversationID,
			Kind:           kind,
			Body:           chunk.Text,
			Timestamp:      ts,
		}
		if err := t.sender.Send(ctx, msg); err != nil {
			return err
		}
		t.appendRollingBody(conversationID, chunk.Text)
		if err := t.manager.UpdateRollingMessage(conversationID, kind, ts); err != nil {
			return err
		}
		if chunk.StopReason != "" {
			t.clearRollingBody(conversationID)
			return t.manager.UpdateRollingMessage(conversationID, kind, 0)
		}
		return nil
	}

	// Append to existing rolling message.
	t.appendRollingBody(conversationID, chunk.Text)
	body := t.snapshotRollingBody(conversationID)
	if err := t.sender.Edit(ctx, conversationID, rolling, body); err != nil {
		return err
	}
	if chunk.StopReason != "" {
		t.clearRollingBody(conversationID)
		return t.manager.UpdateRollingMessage(conversationID, kind, 0)
	}
	return nil
}

// nextRollingTimestamp produces a placeholder timestamp for rolling
// messages. The translator tracks these so Edit targets are stable;
// they do not need to collide-avoid against SignalClient's own stream
// because the translator hands fully-specified messages to Send.
func nextRollingTimestamp(t *EventTranslator) int64 {
	return clockNowMilli()
}

// handleToolInvocation sends the 🔧 line and records its timestamp so
// the matching tool_result can reply-thread.
func handleToolInvocation(ctx context.Context, t *EventTranslator, conversationID string, kind ConversationKind, prefs VerbosityPrefs, evt *orchestration.SessionEvent) error {
	if !prefs.ToolDetails || evt.ToolInvocation == nil {
		return nil
	}
	inv := evt.ToolInvocation
	args := string(inv.Args)
	if len(args) > maxInlineToolArgs {
		args = args[:maxInlineToolArgs] + "…"
	}
	body := "🔧 " + inv.Name + "(" + args + ")"
	ts := clockNowMilli()
	if err := t.sender.Send(ctx, OutboundMessage{
		ConversationID: conversationID,
		Kind:           kind,
		Body:           body,
		Timestamp:      ts,
	}); err != nil {
		return err
	}
	return t.manager.RecordInvocation(conversationID, inv.InvocationID, ts)
}

// handleToolResult replies to the invocation marker, promotes > 2MB
// results to attachments, and prefixes errors with ❌.
func handleToolResult(ctx context.Context, t *EventTranslator, conversationID string, kind ConversationKind, prefs VerbosityPrefs, evt *orchestration.SessionEvent) error {
	if !prefs.ToolDetails || evt.ToolResult == nil {
		return nil
	}
	res := evt.ToolResult
	quoteTS := t.manager.InvocationTimestamp(conversationID, res.InvocationID)

	var msg OutboundMessage
	msg.ConversationID = conversationID
	msg.Kind = kind
	msg.QuoteTimestamp = quoteTS
	msg.Timestamp = clockNowMilli()

	content := res.Content
	prefix := "→ "
	if res.IsError {
		prefix = "❌ ERROR "
	}

	if len(content) > attachmentPromotionThreshold {
		msg.Body = prefix + fmt.Sprintf("(%d bytes, attached)", len(content))
		msg.Attachments = []Attachment{{
			ContentType: "text/plain",
			Filename:    "tool_result.txt",
			Bytes:       []byte(content),
			Size:        int64(len(content)),
		}}
	} else {
		inline := content
		if len(inline) > maxInlineToolResult {
			inline = inline[:maxInlineToolResult] + "…"
		}
		msg.Body = prefix + inline
	}
	return t.sender.Send(ctx, msg)
}

// handleFileOp emits a 📝 line for file mutations.
func handleFileOp(ctx context.Context, t *EventTranslator, conversationID string, kind ConversationKind, prefs VerbosityPrefs, evt *orchestration.SessionEvent) error {
	if !prefs.FileOps || evt.FileOp == nil {
		return nil
	}
	fop := evt.FileOp
	body := "📝 " + fop.Op + " " + fop.Path
	if fop.Size > 0 {
		body += fmt.Sprintf(" (%d bytes)", fop.Size)
	}
	return t.sender.Send(ctx, OutboundMessage{
		ConversationID: conversationID,
		Kind:           kind,
		Body:           body,
		Timestamp:      clockNowMilli(),
	})
}

// handleSystemMessage renders orchestrator-internal messages with a
// level-appropriate emoji.
func handleSystemMessage(ctx context.Context, t *EventTranslator, conversationID string, kind ConversationKind, prefs VerbosityPrefs, evt *orchestration.SessionEvent) error {
	if evt.System == nil {
		return nil
	}
	sys := evt.System
	emoji := levelEmojis[sys.Level]
	if emoji == "" {
		emoji = "ℹ️"
	}
	return t.sender.Send(ctx, OutboundMessage{
		ConversationID: conversationID,
		Kind:           kind,
		Body:           emoji + " " + sys.Message,
		Timestamp:      clockNowMilli(),
	})
}

// levelEmojis maps SystemMessagePayload.Level to a leading emoji.
var levelEmojis = map[string]string{
	"info":  "ℹ️",
	"warn":  "⚠️",
	"error": "🛑",
}

func handleOperatorJoined(ctx context.Context, t *EventTranslator, conversationID string, kind ConversationKind, prefs VerbosityPrefs, evt *orchestration.SessionEvent) error {
	if evt.OperatorJoined == nil {
		return nil
	}
	name := evt.OperatorJoined.DisplayName
	if name == "" {
		name = evt.OperatorJoined.OperatorID
	}
	return t.sender.Send(ctx, OutboundMessage{
		ConversationID: conversationID,
		Kind:           kind,
		Body:           "👋 " + name + " joined (" + evt.OperatorJoined.Interface + ")",
		Timestamp:      clockNowMilli(),
	})
}

func handleOperatorLeft(ctx context.Context, t *EventTranslator, conversationID string, kind ConversationKind, prefs VerbosityPrefs, evt *orchestration.SessionEvent) error {
	if evt.OperatorLeft == nil {
		return nil
	}
	name := evt.OperatorLeft.DisplayName
	if name == "" {
		name = evt.OperatorLeft.OperatorID
	}
	return t.sender.Send(ctx, OutboundMessage{
		ConversationID: conversationID,
		Kind:           kind,
		Body:           "🚪 " + name + " left",
		Timestamp:      clockNowMilli(),
	})
}

func handleSessionClosed(ctx context.Context, t *EventTranslator, conversationID string, kind ConversationKind, prefs VerbosityPrefs, evt *orchestration.SessionEvent) error {
	if evt.Closed == nil {
		return nil
	}
	body := "✅ Session closed (" + evt.Closed.Reason + ")"
	if evt.Closed.Detail != "" {
		body += ": " + evt.Closed.Detail
	}
	// Finalize any rolling message before announcing closure.
	t.clearRollingBody(conversationID)
	_ = t.manager.UpdateRollingMessage(conversationID, kind, 0)
	return t.sender.Send(ctx, OutboundMessage{
		ConversationID: conversationID,
		Kind:           kind,
		Body:           body,
		Timestamp:      clockNowMilli(),
	})
}

func handlePromptSubmitted(ctx context.Context, t *EventTranslator, conversationID string, kind ConversationKind, prefs VerbosityPrefs, evt *orchestration.SessionEvent) error {
	// We do not echo operator prompts back — Signal already shows them
	// in the originating conversation. But if verbose mode is enabled
	// we could, in a future iteration, mirror lead-agent prompts.
	return nil
}

func handleSessionResumed(ctx context.Context, t *EventTranslator, conversationID string, kind ConversationKind, prefs VerbosityPrefs, evt *orchestration.SessionEvent) error {
	if evt.Resumed == nil {
		return nil
	}
	return t.sender.Send(ctx, OutboundMessage{
		ConversationID: conversationID,
		Kind:           kind,
		Body:           "🔁 Session resumed (" + evt.Resumed.ResumedBy + ")",
		Timestamp:      clockNowMilli(),
	})
}

// appendRollingBody grows the accumulated assistant text for a
// conversation.
func (t *EventTranslator) appendRollingBody(conversationID, chunk string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	sb, ok := t.rollingBodies[conversationID]
	if !ok {
		sb = &strings.Builder{}
		t.rollingBodies[conversationID] = sb
	}
	sb.WriteString(chunk)
}

// snapshotRollingBody returns the accumulated body.
func (t *EventTranslator) snapshotRollingBody(conversationID string) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	sb, ok := t.rollingBodies[conversationID]
	if !ok {
		return ""
	}
	return sb.String()
}

// flushRollingBody returns and clears the accumulated body.
func (t *EventTranslator) flushRollingBody(conversationID string) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	sb, ok := t.rollingBodies[conversationID]
	if !ok {
		return ""
	}
	out := sb.String()
	delete(t.rollingBodies, conversationID)
	return out
}

// clearRollingBody discards the accumulated body without emitting.
func (t *EventTranslator) clearRollingBody(conversationID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.rollingBodies, conversationID)
}
