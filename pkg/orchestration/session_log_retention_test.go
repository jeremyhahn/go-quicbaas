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
	"unicode/utf8"
)

// TestSessionRetentionTurnsFor covers the resolution order: a tenant override
// wins, then the deployment setting, then the documented default.
func TestSessionRetentionTurnsFor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		retention SessionRetention
		tenant    string
		want      int
	}{
		{"default when unset", SessionRetention{}, "t1", DefaultSessionRetentionTurns},
		{"deployment setting", SessionRetention{MaxTurns: 7}, "t1", 7},
		{"tenant override", SessionRetention{MaxTurns: 7, PerTenant: map[string]int{"t1": 3}}, "t1", 3},
		{"other tenant unaffected", SessionRetention{MaxTurns: 7, PerTenant: map[string]int{"t1": 3}}, "t2", 7},
		{"legal hold keeps everything", SessionRetention{MaxTurns: 7, PerTenant: map[string]int{"t1": 0}}, "t1", 0},
		{"empty tenant reads the system setting", SessionRetention{PerTenant: map[string]int{SystemTenantID: 5}}, "", 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.retention.TurnsFor(tt.tenant); got != tt.want {
				t.Fatalf("TurnsFor(%q) = %d, want %d", tt.tenant, got, tt.want)
			}
		})
	}
}

// TestKVSessionEventLog_RetentionDropsWholeTurns asserts that a transcript
// past its bound loses its OLDEST TURNS ENTIRE — never half of one. A cut
// through the middle of a turn would show an answer with no question above it.
func TestKVSessionEventLog_RetentionDropsWholeTurns(t *testing.T) {
	t.Parallel()

	const tenant = "tenant-retain"
	const sessionID = "s-retain"
	const keep = 2
	const rounds = 5
	ctx := context.Background()
	log := newHistoryTestLog(t, SessionRetention{MaxTurns: keep})
	if err := log.PutMeta(ctx, &SessionMeta{SessionID: sessionID, TenantID: tenant, Status: SessionStateActive}); err != nil {
		t.Fatalf("PutMeta: %v", err)
	}

	for i := 0; i < rounds; i++ {
		appendTurn(t, ctx, log, tenant, sessionID, "question", "answer")
	}

	page, err := ReadSessionHistory(ctx, log, &SessionHistoryRequest{TenantID: tenant, SessionID: sessionID})
	if err != nil {
		t.Fatalf("ReadSessionHistory: %v", err)
	}
	// keep prompts + keep answers, and nothing older.
	if len(page.Turns) != keep*2 {
		t.Fatalf("retained %d turns, want %d: %+v", len(page.Turns), keep*2, page.Turns)
	}
	if page.Turns[0].Role != SessionTurnRoleUser {
		t.Fatalf("the oldest retained turn is %q — retention cut inside a turn", page.Turns[0].Role)
	}
	if page.OldestSequence == 0 {
		t.Fatalf("a pruned transcript reports OldestSequence 0, so a reader cannot tell it was pruned")
	}
}

// TestKVSessionEventLog_RetentionDisabledKeepsEverything asserts a tenant with
// retention switched off keeps its whole conversation.
func TestKVSessionEventLog_RetentionDisabledKeepsEverything(t *testing.T) {
	t.Parallel()

	const tenant = "tenant-hold"
	const sessionID = "s-hold"
	const rounds = 4
	ctx := context.Background()
	log := newHistoryTestLog(t, SessionRetention{MaxTurns: 1, PerTenant: map[string]int{tenant: 0}})
	if err := log.PutMeta(ctx, &SessionMeta{SessionID: sessionID, TenantID: tenant, Status: SessionStateActive}); err != nil {
		t.Fatalf("PutMeta: %v", err)
	}
	for i := 0; i < rounds; i++ {
		appendTurn(t, ctx, log, tenant, sessionID, "question", "answer")
	}

	page, err := ReadSessionHistory(ctx, log, &SessionHistoryRequest{TenantID: tenant, SessionID: sessionID})
	if err != nil {
		t.Fatalf("ReadSessionHistory: %v", err)
	}
	if len(page.Turns) != rounds*2 {
		t.Fatalf("a tenant under legal hold kept %d turns, want %d", len(page.Turns), rounds*2)
	}
}

// TestKVSessionEventLog_RetentionReportsPruneFailure asserts a store that
// refuses a delete surfaces the condition instead of advancing past keys it
// did not remove.
func TestKVSessionEventLog_RetentionReportsPruneFailure(t *testing.T) {
	t.Parallel()

	const tenant = "tenant-prune-fail"
	const sessionID = "s-prune-fail"
	ctx := context.Background()
	deleteErr := errors.New("store refused the delete")
	kv := &deleteFailingKVStore{memKVStore: newMemKVStore(), err: deleteErr}
	var reported []error
	log, err := NewKVSessionEventLog(KVSessionEventLogConfig{
		KVStore:          kv,
		Retention:        SessionRetention{MaxTurns: 1},
		OnRetentionError: func(e error) { reported = append(reported, e) },
	})
	if err != nil {
		t.Fatalf("NewKVSessionEventLog: %v", err)
	}
	if err := log.PutMeta(ctx, &SessionMeta{SessionID: sessionID, TenantID: tenant, Status: SessionStateActive}); err != nil {
		t.Fatalf("PutMeta: %v", err)
	}
	appendTurn(t, ctx, log, tenant, sessionID, "q1", "a1")
	appendTurn(t, ctx, log, tenant, sessionID, "q2", "a2")

	if len(reported) == 0 {
		t.Fatal("a prune that could not delete reported nothing")
	}
	var retentionErr *SessionRetentionError
	if !errors.As(reported[0], &retentionErr) {
		t.Fatalf("reported %v, want a *SessionRetentionError", reported[0])
	}
	if !errors.Is(reported[0], deleteErr) {
		t.Fatalf("reported error does not unwrap to the store failure: %v", reported[0])
	}
	// The turn that could not be pruned is still readable — a failed prune
	// must not remove a turn from the view while leaving it in the store.
	page, readErr := ReadSessionHistory(ctx, log, &SessionHistoryRequest{TenantID: tenant, SessionID: sessionID})
	if readErr != nil {
		t.Fatalf("ReadSessionHistory: %v", readErr)
	}
	if len(page.Turns) != 4 {
		t.Fatalf("got %d turns after a failed prune, want all 4 still present", len(page.Turns))
	}
}

// TestKVSessionEventLog_DeleteSessionRemovesTranscript asserts deleting a
// session removes every event and its metadata, and that reading it afterwards
// is an empty conversation rather than an error.
func TestKVSessionEventLog_DeleteSessionRemovesTranscript(t *testing.T) {
	t.Parallel()

	const tenant = "tenant-delete"
	const sessionID = "s-delete"
	ctx := context.Background()
	log := newHistoryTestLog(t, SessionRetention{})
	if err := log.PutMeta(ctx, &SessionMeta{SessionID: sessionID, TenantID: tenant, Status: SessionStateActive}); err != nil {
		t.Fatalf("PutMeta: %v", err)
	}
	appendTurn(t, ctx, log, tenant, sessionID, "q", "a")

	if err := log.DeleteSession(ctx, tenant, sessionID); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	page, err := ReadSessionHistory(ctx, log, &SessionHistoryRequest{TenantID: tenant, SessionID: sessionID})
	if err != nil {
		t.Fatalf("ReadSessionHistory after delete: %v", err)
	}
	if len(page.Turns) != 0 {
		t.Fatalf("a deleted session still reads %d turns", len(page.Turns))
	}
	if _, metaErr := log.GetMeta(ctx, tenant, sessionID); !errors.Is(metaErr, ErrSessionLogNotFound) {
		t.Fatalf("GetMeta after delete = %v, want ErrSessionLogNotFound", metaErr)
	}
}

// TestKVSessionEventLog_DeleteSessionRejectsEmptyID asserts an unaddressed
// delete is refused rather than treated as "delete everything".
func TestKVSessionEventLog_DeleteSessionRejectsEmptyID(t *testing.T) {
	t.Parallel()

	log := newHistoryTestLog(t, SessionRetention{})
	if err := log.DeleteSession(context.Background(), "t1", ""); err == nil {
		t.Fatal("DeleteSession with no session id succeeded")
	}
}

// TestMemorySessionEventLog_DeleteSession covers the in-memory log's half of
// the interface: the happy path removes the transcript, and an unaddressed
// delete is refused.
func TestMemorySessionEventLog_DeleteSession(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	log := NewMemorySessionEventLog()
	if err := log.PutMeta(ctx, &SessionMeta{SessionID: "s1", TenantID: "t1"}); err != nil {
		t.Fatalf("PutMeta: %v", err)
	}
	if err := AppendNextSessionEvent(ctx, log, &SessionEvent{
		SessionID: "s1", TenantID: "t1", Kind: KindPromptSubmitted,
		Prompt: &PromptSubmittedPayload{Prompt: "q"},
	}); err != nil {
		t.Fatalf("append: %v", err)
	}

	if err := log.DeleteSession(ctx, "t1", "s1"); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	events, _, err := log.Range(ctx, "t1", "s1", 0, 0)
	if err != nil {
		t.Fatalf("Range: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("a deleted session still holds %d events", len(events))
	}
	if err := log.DeleteSession(ctx, "t1", ""); err == nil {
		t.Fatal("DeleteSession with no session id succeeded")
	}
}

// appendTurn writes one prompt and one answer to a transcript.
func appendTurn(t *testing.T, ctx context.Context, log SessionEventLog, tenant, sessionID, prompt, answer string) {
	t.Helper()
	if err := AppendNextSessionEvent(ctx, log, &SessionEvent{
		SessionID: sessionID, TenantID: tenant, Kind: KindPromptSubmitted,
		Prompt: &PromptSubmittedPayload{Prompt: prompt},
	}); err != nil {
		t.Fatalf("append prompt: %v", err)
	}
	if err := AppendNextSessionEvent(ctx, log, &SessionEvent{
		SessionID: sessionID, TenantID: tenant, Kind: KindAssistantChunk,
		AssistantChunk: &AssistantChunkPayload{Text: answer, StopReason: "end_turn"},
	}); err != nil {
		t.Fatalf("append answer: %v", err)
	}
}

// deleteFailingKVStore fails every Delete and passes everything else through.
type deleteFailingKVStore struct {
	*memKVStore
	err error
}

// Delete always fails, standing in for a store that cannot reclaim a key.
func (s *deleteFailingKVStore) Delete(ctx context.Context, key string) error { return s.err }

// TestTruncateSessionText covers the cap in isolation: text within the bound
// is untouched, text past it is clipped on a rune boundary and marked.
func TestTruncateSessionText(t *testing.T) {
	t.Parallel()

	t.Run("within the cap is unchanged", func(t *testing.T) {
		in := strings.Repeat("x", 128)
		out, dropped := truncateSessionText(in)
		if out != in || dropped != 0 {
			t.Fatalf("truncateSessionText clipped a value inside the cap: dropped=%d", dropped)
		}
	})

	t.Run("past the cap is clipped and marked", func(t *testing.T) {
		in := strings.Repeat("x", MaxSessionEventTextBytes+10)
		out, dropped := truncateSessionText(in)
		if dropped != 10 {
			t.Fatalf("dropped = %d, want 10", dropped)
		}
		if !strings.HasSuffix(out, truncationMarkerSuffix) || !strings.Contains(out, truncationMarkerPrefix) {
			t.Fatalf("clipped text carries no marker: ...%q", out[len(out)-32:])
		}
	})

	t.Run("clips on a rune boundary", func(t *testing.T) {
		// A three-byte rune straddling the cap must be dropped whole, not
		// halved into bytes that are not valid UTF-8.
		in := strings.Repeat("a", MaxSessionEventTextBytes-1) + "€€"
		out, dropped := truncateSessionText(in)
		if dropped == 0 {
			t.Fatal("expected the oversized value to be clipped")
		}
		body := strings.TrimSuffix(out, truncationMarkerSuffix)
		if idx := strings.LastIndex(body, truncationMarkerPrefix); idx >= 0 {
			body = body[:idx]
		}
		if !isValidUTF8(body) {
			t.Fatal("clip split a multi-byte rune")
		}
	})
}

// isValidUTF8 reports whether s is well-formed UTF-8.
func isValidUTF8(s string) bool { return utf8.ValidString(s) }

// TestCapSessionEventText_MarksEveryTextField asserts the cap runs over every
// operator-facing field an event can carry, and that a producer's own
// truncation count is added to rather than overwritten.
func TestCapSessionEventText_MarksEveryTextField(t *testing.T) {
	t.Parallel()

	over := strings.Repeat("y", MaxSessionEventTextBytes+3)
	event := &SessionEvent{
		SessionID: "s1", TenantID: "t1",
		Prompt:         &PromptSubmittedPayload{Prompt: over, SystemPrompt: over},
		AssistantChunk: &AssistantChunkPayload{Text: over},
		Thought:        &AgentThoughtPayload{Text: over},
		ToolInvocation: &ToolInvocationPayload{Args: []byte(strings.Repeat("z", MaxSessionEventArgsBytes+1))},
		ToolResult:     &ToolResultPayload{Content: over, TruncatedBytes: 100},
		System:         &SystemMessagePayload{Message: over},
		TurnFailed:     &TurnFailedPayload{Error: over},
	}
	capSessionEventText(event)

	if event.Prompt.TruncatedBytes != 6 { // 3 from each of Prompt and SystemPrompt
		t.Fatalf("prompt truncated bytes = %d, want 6", event.Prompt.TruncatedBytes)
	}
	if event.AssistantChunk.TruncatedBytes != 3 || event.Thought.TruncatedBytes != 3 {
		t.Fatalf("chunk/thought truncated bytes = %d/%d, want 3/3",
			event.AssistantChunk.TruncatedBytes, event.Thought.TruncatedBytes)
	}
	if event.ToolResult.TruncatedBytes != 103 {
		t.Fatalf("tool result truncated bytes = %d — the producer's own count was overwritten, not added to",
			event.ToolResult.TruncatedBytes)
	}
	if len(event.ToolInvocation.Args) > 64 || event.ToolInvocation.TruncatedBytes == 0 {
		t.Fatalf("oversized args were not replaced with a marker: len=%d truncated=%d",
			len(event.ToolInvocation.Args), event.ToolInvocation.TruncatedBytes)
	}
	if len(event.System.Message) > MaxSessionEventTextBytes+64 {
		t.Fatalf("system message was not clipped: %d bytes", len(event.System.Message))
	}
	if len(event.TurnFailed.Error) > MaxSessionEventTextBytes+64 {
		t.Fatalf("turn failure text was not clipped: %d bytes", len(event.TurnFailed.Error))
	}
}

// TestCapSessionEventText_NilIsSafe asserts the cap tolerates the nil event
// and the event with no payloads — both reachable from the shared write path.
func TestCapSessionEventText_NilIsSafe(t *testing.T) {
	t.Parallel()

	capSessionEventText(nil)
	capSessionEventText(&SessionEvent{SessionID: "s1", TenantID: "t1", Kind: KindOperatorJoined})
}
