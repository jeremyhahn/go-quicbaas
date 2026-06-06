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
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// translateEvent runs a single event through the translator and returns
// the last reply body sent (or "").
func translateEvent(t *testing.T, evt orchestration.SessionEvent) (*fakeSender, *EventTranslator, *SessionManager) {
	t.Helper()
	tr, sender, mgr := newTestTranslator(t)
	env := envelopeFor(t, 1, evt)
	if err := tr.Translate(context.Background(), "conv-1", ConversationDM, env); err != nil {
		t.Fatalf("translate: %v", err)
	}
	return sender, tr, mgr
}

func lastBody(s *fakeSender) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.sends) == 0 {
		return ""
	}
	return s.sends[len(s.sends)-1].Body
}

// TestTranslate_FileOp emits a 📝 line and suppresses it when prefs
// disable file ops.
func TestTranslate_FileOp(t *testing.T) {
	sender, _, _ := translateEvent(t, orchestration.SessionEvent{
		Kind:   orchestration.KindFileOp,
		FileOp: &orchestration.FileOpPayload{Op: "write", Path: "/tmp/x", Size: 1024},
	})
	if !contains(lastBody(sender), "/tmp/x") || !contains(lastBody(sender), "1024 bytes") {
		t.Fatalf("body = %q", lastBody(sender))
	}
}

func TestTranslate_FileOp_Disabled(t *testing.T) {
	tr, sender, mgr := newTestTranslator(t)
	prefs := DefaultVerbosityPrefs()
	prefs.FileOps = false
	if err := mgr.SetVerbosity("conv-1", ConversationDM, prefs); err != nil {
		t.Fatalf("set verbosity: %v", err)
	}
	env := envelopeFor(t, 1, orchestration.SessionEvent{
		Kind:   orchestration.KindFileOp,
		FileOp: &orchestration.FileOpPayload{Op: "write", Path: "/tmp/x"},
	})
	if err := tr.Translate(context.Background(), "conv-1", ConversationDM, env); err != nil {
		t.Fatalf("translate: %v", err)
	}
	if len(sender.sends) != 0 {
		t.Fatalf("file op should be suppressed, got %+v", sender.sends)
	}
}

// TestTranslate_FileOp_NilPayload is a no-op when the payload is missing.
func TestTranslate_FileOp_NilPayload(t *testing.T) {
	sender, _, _ := translateEvent(t, orchestration.SessionEvent{Kind: orchestration.KindFileOp})
	if len(sender.sends) != 0 {
		t.Fatalf("nil payload should not send")
	}
}

// TestTranslate_SystemMessage renders the level emoji.
func TestTranslate_SystemMessage(t *testing.T) {
	cases := []struct {
		level string
		emoji string
	}{
		{"info", "ℹ️"},
		{"warn", "⚠️"},
		{"error", "🛑"},
		{"unknown-level", "ℹ️"}, // falls back to info emoji
	}
	for _, tc := range cases {
		sender, _, _ := translateEvent(t, orchestration.SessionEvent{
			Kind:   orchestration.KindSystemMessage,
			System: &orchestration.SystemMessagePayload{Level: tc.level, Message: "hi"},
		})
		if !contains(lastBody(sender), tc.emoji) || !contains(lastBody(sender), "hi") {
			t.Fatalf("level %q: body = %q", tc.level, lastBody(sender))
		}
	}
}

func TestTranslate_SystemMessage_NilPayload(t *testing.T) {
	sender, _, _ := translateEvent(t, orchestration.SessionEvent{Kind: orchestration.KindSystemMessage})
	if len(sender.sends) != 0 {
		t.Fatalf("nil system payload should not send")
	}
}

// TestTranslate_OperatorJoined prefers display name, falls back to id.
func TestTranslate_OperatorJoined(t *testing.T) {
	sender, _, _ := translateEvent(t, orchestration.SessionEvent{
		Kind:           orchestration.KindOperatorJoined,
		OperatorJoined: &orchestration.OperatorJoinedPayload{DisplayName: "Alice", Interface: "web"},
	})
	if !contains(lastBody(sender), "Alice") || !contains(lastBody(sender), "joined") || !contains(lastBody(sender), "web") {
		t.Fatalf("body = %q", lastBody(sender))
	}
}

func TestTranslate_OperatorJoined_FallbackID(t *testing.T) {
	sender, _, _ := translateEvent(t, orchestration.SessionEvent{
		Kind:           orchestration.KindOperatorJoined,
		OperatorJoined: &orchestration.OperatorJoinedPayload{OperatorID: "op-7", Interface: "cli"},
	})
	if !contains(lastBody(sender), "op-7") {
		t.Fatalf("body = %q", lastBody(sender))
	}
}

func TestTranslate_OperatorJoined_NilPayload(t *testing.T) {
	sender, _, _ := translateEvent(t, orchestration.SessionEvent{Kind: orchestration.KindOperatorJoined})
	if len(sender.sends) != 0 {
		t.Fatalf("nil payload should not send")
	}
}

// TestTranslate_OperatorLeft renders the leave notice.
func TestTranslate_OperatorLeft(t *testing.T) {
	sender, _, _ := translateEvent(t, orchestration.SessionEvent{
		Kind:         orchestration.KindOperatorLeft,
		OperatorLeft: &orchestration.OperatorLeftPayload{DisplayName: "Bob"},
	})
	if !contains(lastBody(sender), "Bob") || !contains(lastBody(sender), "left") {
		t.Fatalf("body = %q", lastBody(sender))
	}
}

func TestTranslate_OperatorLeft_FallbackID(t *testing.T) {
	sender, _, _ := translateEvent(t, orchestration.SessionEvent{
		Kind:         orchestration.KindOperatorLeft,
		OperatorLeft: &orchestration.OperatorLeftPayload{OperatorID: "op-9"},
	})
	if !contains(lastBody(sender), "op-9") {
		t.Fatalf("body = %q", lastBody(sender))
	}
}

func TestTranslate_OperatorLeft_NilPayload(t *testing.T) {
	sender, _, _ := translateEvent(t, orchestration.SessionEvent{Kind: orchestration.KindOperatorLeft})
	if len(sender.sends) != 0 {
		t.Fatalf("nil payload should not send")
	}
}

// TestTranslate_SessionClosed renders the reason and optional detail and
// finalizes any rolling message.
func TestTranslate_SessionClosed(t *testing.T) {
	sender, _, _ := translateEvent(t, orchestration.SessionEvent{
		Kind:   orchestration.KindSessionClosed,
		Closed: &orchestration.SessionClosedPayload{Reason: "timeout", Detail: "idle"},
	})
	if !contains(lastBody(sender), "timeout") || !contains(lastBody(sender), "idle") {
		t.Fatalf("body = %q", lastBody(sender))
	}
}

func TestTranslate_SessionClosed_NoDetail(t *testing.T) {
	sender, _, _ := translateEvent(t, orchestration.SessionEvent{
		Kind:   orchestration.KindSessionClosed,
		Closed: &orchestration.SessionClosedPayload{Reason: "user"},
	})
	if !contains(lastBody(sender), "user") {
		t.Fatalf("body = %q", lastBody(sender))
	}
}

func TestTranslate_SessionClosed_NilPayload(t *testing.T) {
	sender, _, _ := translateEvent(t, orchestration.SessionEvent{Kind: orchestration.KindSessionClosed})
	if len(sender.sends) != 0 {
		t.Fatalf("nil payload should not send")
	}
}

// TestTranslate_PromptSubmitted is intentionally a no-op (Signal already
// shows operator prompts in the conversation).
func TestTranslate_PromptSubmitted(t *testing.T) {
	sender, _, _ := translateEvent(t, orchestration.SessionEvent{
		Kind:   orchestration.KindPromptSubmitted,
		Prompt: &orchestration.PromptSubmittedPayload{},
	})
	if len(sender.sends) != 0 {
		t.Fatalf("prompt submitted should not echo: %+v", sender.sends)
	}
}

// TestTranslate_SessionResumed renders the resume notice.
func TestTranslate_SessionResumed(t *testing.T) {
	sender, _, _ := translateEvent(t, orchestration.SessionEvent{
		Kind:    orchestration.KindSessionResumed,
		Resumed: &orchestration.SessionResumedPayload{ResumedBy: "daemon_restart"},
	})
	if !contains(lastBody(sender), "resumed") || !contains(lastBody(sender), "daemon_restart") {
		t.Fatalf("body = %q", lastBody(sender))
	}
}

func TestTranslate_SessionResumed_NilPayload(t *testing.T) {
	sender, _, _ := translateEvent(t, orchestration.SessionEvent{Kind: orchestration.KindSessionResumed})
	if len(sender.sends) != 0 {
		t.Fatalf("nil payload should not send")
	}
}

// TestTranslate_UnknownKind falls through to a no-op so new upstream kinds
// don't break older bots.
func TestTranslate_UnknownKind(t *testing.T) {
	tr, sender, _ := newTestTranslator(t)
	env := envelopeFor(t, 1, orchestration.SessionEvent{Kind: orchestration.SessionEventKind("brand_new_kind")})
	if err := tr.Translate(context.Background(), "conv-1", ConversationDM, env); err != nil {
		t.Fatalf("translate: %v", err)
	}
	if len(sender.sends) != 0 {
		t.Fatalf("unknown kind should be a no-op")
	}
}

// TestTranslate_NilEnvelope and bad payload return invalid-input errors.
func TestTranslate_NilEnvelope(t *testing.T) {
	tr, _, _ := newTestTranslator(t)
	if err := tr.Translate(context.Background(), "c", ConversationDM, nil); !IsKind(err, ErrKindInvalidInput) {
		t.Fatalf("want invalid_input, got %v", err)
	}
}

func TestTranslate_BadPayload(t *testing.T) {
	tr, _, _ := newTestTranslator(t)
	env := envelopeFor(t, 1, orchestration.SessionEvent{Kind: orchestration.KindAssistantChunk})
	env.PayloadJSON = []byte("{not json")
	if err := tr.Translate(context.Background(), "c", ConversationDM, env); !IsKind(err, ErrKindInvalidInput) {
		t.Fatalf("want invalid_input, got %v", err)
	}
}

// TestRollingBody_SnapshotAndFlush exercises the rolling-body accumulator
// independent of the streaming path.
func TestRollingBody_SnapshotAndFlush(t *testing.T) {
	tr, _, _ := newTestTranslator(t)
	if tr.snapshotRollingBody("c") != "" {
		t.Fatalf("empty snapshot should be blank")
	}
	if tr.flushRollingBody("c") != "" {
		t.Fatalf("empty flush should be blank")
	}
	tr.appendRollingBody("c", "ab")
	tr.appendRollingBody("c", "cd")
	if got := tr.snapshotRollingBody("c"); got != "abcd" {
		t.Fatalf("snapshot = %q", got)
	}
	if got := tr.flushRollingBody("c"); got != "abcd" {
		t.Fatalf("flush = %q", got)
	}
	if tr.snapshotRollingBody("c") != "" {
		t.Fatalf("snapshot after flush should be empty")
	}
}
