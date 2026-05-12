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
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// fakeSender records every Send and Edit call and can be asked about
// its history.
type fakeSender struct {
	mu    sync.Mutex
	sends []OutboundMessage
	edits []editCall
	failSends bool
}

type editCall struct {
	ConversationID string
	Original       int64
	Body           string
}

func (f *fakeSender) Send(ctx context.Context, msg OutboundMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failSends {
		return errors.New("forced send failure")
	}
	f.sends = append(f.sends, msg)
	return nil
}

func (f *fakeSender) Edit(ctx context.Context, conv string, original int64, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.edits = append(f.edits, editCall{ConversationID: conv, Original: original, Body: body})
	return nil
}

// envelopeFor marshals evt into a SessionEventEnvelope for testing.
func envelopeFor(t *testing.T, seq uint64, evt orchestration.SessionEvent) *orchestratorSDK.SessionEventEnvelope {
	t.Helper()
	raw, err := json.Marshal(&evt)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	return &orchestratorSDK.SessionEventEnvelope{
		SessionID:   evt.SessionID,
		TenantID:    evt.TenantID,
		Sequence:    seq,
		PayloadJSON: raw,
		Timestamp:   time.Now().UTC(),
	}
}

// newTestTranslator sets up a translator backed by an in-memory state
// store so each test runs isolated.
func newTestTranslator(t *testing.T) (*EventTranslator, *fakeSender, *SessionManager) {
	t.Helper()
	store := NewMemoryStateStore()
	mgr, err := NewSessionManager(store, DefaultVerbosityPrefs())
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	sender := &fakeSender{}
	tr, err := NewEventTranslator(sender, mgr)
	if err != nil {
		t.Fatalf("new translator: %v", err)
	}
	return tr, sender, mgr
}

// TestEventTranslator_AssistantChunkCoalescing is the critical test: a
// series of streamed chunks produces exactly one Send followed by
// Edits accumulating the growing body, finalizing on stop_reason.
func TestEventTranslator_AssistantChunkCoalescing(t *testing.T) {
	tr, sender, mgr := newTestTranslator(t)
	ctx := context.Background()
	conv := "conv-1"

	chunks := []orchestration.AssistantChunkPayload{
		{Text: "Hello"},
		{Text: ", "},
		{Text: "world"},
		{Text: "!", StopReason: "end_turn"},
	}
	for i, c := range chunks {
		evt := orchestration.SessionEvent{
			Kind:           orchestration.KindAssistantChunk,
			AssistantChunk: &c,
		}
		env := envelopeFor(t, uint64(i+1), evt)
		if err := tr.Translate(ctx, conv, ConversationDM, env); err != nil {
			t.Fatalf("translate #%d: %v", i, err)
		}
	}

	if got := len(sender.sends); got != 1 {
		t.Fatalf("sends: want 1, got %d: %+v", got, sender.sends)
	}
	if got := len(sender.edits); got != 3 {
		t.Fatalf("edits: want 3, got %d: %+v", got, sender.edits)
	}
	finalBody := sender.edits[len(sender.edits)-1].Body
	if finalBody != "Hello, world!" {
		t.Fatalf("final body: want %q, got %q", "Hello, world!", finalBody)
	}

	// Rolling marker cleared after finalize.
	state := mgr.Get(conv, ConversationDM)
	if state.RollingMsgTimestamp != 0 {
		t.Fatalf("rolling message timestamp not cleared: %d", state.RollingMsgTimestamp)
	}
}

// TestEventTranslator_ToolInvocationAndResult verifies the tool call
// produces a 🔧 line and the result replies to it via QuoteTimestamp.
func TestEventTranslator_ToolInvocationAndResult(t *testing.T) {
	tr, sender, _ := newTestTranslator(t)
	ctx := context.Background()
	conv := "conv-2"

	inv := orchestration.SessionEvent{
		Kind: orchestration.KindToolInvocation,
		ToolInvocation: &orchestration.ToolInvocationPayload{
			InvocationID: "inv-1",
			Name:         "Bash",
			Args:         json.RawMessage(`{"cmd":"echo hi"}`),
		},
	}
	if err := tr.Translate(ctx, conv, ConversationDM, envelopeFor(t, 1, inv)); err != nil {
		t.Fatalf("translate invocation: %v", err)
	}
	res := orchestration.SessionEvent{
		Kind: orchestration.KindToolResult,
		ToolResult: &orchestration.ToolResultPayload{
			InvocationID: "inv-1",
			Content:      "hi\n",
		},
	}
	if err := tr.Translate(ctx, conv, ConversationDM, envelopeFor(t, 2, res)); err != nil {
		t.Fatalf("translate result: %v", err)
	}
	if len(sender.sends) != 2 {
		t.Fatalf("want 2 sends got %d", len(sender.sends))
	}
	invMsg := sender.sends[0]
	if !strings.Contains(invMsg.Body, "🔧 Bash") {
		t.Fatalf("invocation body: %q", invMsg.Body)
	}
	resMsg := sender.sends[1]
	if resMsg.QuoteTimestamp != invMsg.Timestamp {
		t.Fatalf("quote thread: got %d want %d", resMsg.QuoteTimestamp, invMsg.Timestamp)
	}
	if !strings.HasPrefix(resMsg.Body, "→ ") {
		t.Fatalf("result body: %q", resMsg.Body)
	}
}

// TestEventTranslator_ToolResultError prefixes failures with ❌.
func TestEventTranslator_ToolResultError(t *testing.T) {
	tr, sender, _ := newTestTranslator(t)
	ctx := context.Background()
	res := orchestration.SessionEvent{
		Kind: orchestration.KindToolResult,
		ToolResult: &orchestration.ToolResultPayload{
			InvocationID: "unknown",
			Content:      "boom",
			IsError:      true,
		},
	}
	if err := tr.Translate(ctx, "c", ConversationDM, envelopeFor(t, 1, res)); err != nil {
		t.Fatalf("translate: %v", err)
	}
	if len(sender.sends) != 1 || !strings.HasPrefix(sender.sends[0].Body, "❌ ERROR") {
		t.Fatalf("expected error prefix, got %+v", sender.sends)
	}
}

// TestEventTranslator_AttachmentPromotion verifies that tool results
// over 2MB are attached instead of inlined.
func TestEventTranslator_AttachmentPromotion(t *testing.T) {
	tr, sender, _ := newTestTranslator(t)
	ctx := context.Background()
	big := strings.Repeat("a", attachmentPromotionThreshold+1)
	res := orchestration.SessionEvent{
		Kind: orchestration.KindToolResult,
		ToolResult: &orchestration.ToolResultPayload{
			InvocationID: "inv",
			Content:      big,
		},
	}
	if err := tr.Translate(ctx, "c", ConversationDM, envelopeFor(t, 1, res)); err != nil {
		t.Fatalf("translate: %v", err)
	}
	if len(sender.sends) != 1 {
		t.Fatalf("want 1 send got %d", len(sender.sends))
	}
	m := sender.sends[0]
	if len(m.Attachments) != 1 {
		t.Fatalf("expected attachment got %d", len(m.Attachments))
	}
	if m.Attachments[0].Size != int64(len(big)) {
		t.Fatalf("size mismatch: got %d", m.Attachments[0].Size)
	}
	if strings.Contains(m.Body, "aaaaaaaaaa") {
		t.Fatalf("body should not inline large content: %q", m.Body)
	}
}

// TestEventTranslator_SessionClosedFinalizesRolling checks that a
// session_closed event clears any rolling assistant state before
// emitting the closure announcement.
func TestEventTranslator_SessionClosedFinalizesRolling(t *testing.T) {
	tr, sender, mgr := newTestTranslator(t)
	ctx := context.Background()
	conv := "c"

	// Open a rolling message.
	evt := orchestration.SessionEvent{
		Kind:           orchestration.KindAssistantChunk,
		AssistantChunk: &orchestration.AssistantChunkPayload{Text: "partial"},
	}
	if err := tr.Translate(ctx, conv, ConversationDM, envelopeFor(t, 1, evt)); err != nil {
		t.Fatalf("translate: %v", err)
	}
	closed := orchestration.SessionEvent{
		Kind:   orchestration.KindSessionClosed,
		Closed: &orchestration.SessionClosedPayload{Reason: "user"},
	}
	if err := tr.Translate(ctx, conv, ConversationDM, envelopeFor(t, 2, closed)); err != nil {
		t.Fatalf("translate: %v", err)
	}
	if mgr.Get(conv, ConversationDM).RollingMsgTimestamp != 0 {
		t.Fatalf("rolling message not cleared")
	}
	if !strings.Contains(sender.sends[len(sender.sends)-1].Body, "✅") {
		t.Fatalf("closure message missing: %+v", sender.sends)
	}
}

// TestEventTranslator_InvalidEnvelope rejects malformed JSON.
func TestEventTranslator_InvalidEnvelope(t *testing.T) {
	tr, _, _ := newTestTranslator(t)
	ctx := context.Background()
	env := &orchestratorSDK.SessionEventEnvelope{PayloadJSON: []byte("not-json")}
	if err := tr.Translate(ctx, "c", ConversationDM, env); err == nil {
		t.Fatalf("expected error on malformed envelope")
	}
}

// TestEventTranslator_NilEnvelope rejects nil.
func TestEventTranslator_NilEnvelope(t *testing.T) {
	tr, _, _ := newTestTranslator(t)
	if err := tr.Translate(context.Background(), "c", ConversationDM, nil); err == nil {
		t.Fatalf("expected error on nil envelope")
	}
}

// TestEventTranslator_NoStreamTokens honors the verbosity pref: the
// translator buffers and emits one final message at stop_reason.
func TestEventTranslator_NoStreamTokens(t *testing.T) {
	store := NewMemoryStateStore()
	prefs := DefaultVerbosityPrefs()
	prefs.StreamTokens = false
	mgr, err := NewSessionManager(store, prefs)
	if err != nil {
		t.Fatalf("manager: %v", err)
	}
	sender := &fakeSender{}
	tr, err := NewEventTranslator(sender, mgr)
	if err != nil {
		t.Fatalf("translator: %v", err)
	}
	ctx := context.Background()
	conv := "c"
	chunks := []orchestration.AssistantChunkPayload{
		{Text: "A"}, {Text: "B"}, {Text: "C", StopReason: "end_turn"},
	}
	for i, c := range chunks {
		evt := orchestration.SessionEvent{Kind: orchestration.KindAssistantChunk, AssistantChunk: &c}
		if err := tr.Translate(ctx, conv, ConversationDM, envelopeFor(t, uint64(i+1), evt)); err != nil {
			t.Fatalf("translate: %v", err)
		}
	}
	if len(sender.sends) != 1 {
		t.Fatalf("want 1 send, got %d", len(sender.sends))
	}
	if len(sender.edits) != 0 {
		t.Fatalf("want 0 edits, got %d", len(sender.edits))
	}
}

// TestNewEventTranslator_Errors covers the constructor failure paths.
func TestNewEventTranslator_Errors(t *testing.T) {
	if _, err := NewEventTranslator(nil, nil); err == nil {
		t.Fatalf("expected error on nil sender")
	}
	sender := &fakeSender{}
	if _, err := NewEventTranslator(sender, nil); err == nil {
		t.Fatalf("expected error on nil manager")
	}
}
