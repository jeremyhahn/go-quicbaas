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
	"strings"
	"testing"
)

// newHistoryTestLog builds a durable, KV-backed transcript over an in-memory
// store. The KV log — not the memory one — is what production runs and the
// only one that enforces the tenant keyspace and retention, so the history
// tests exercise it directly.
func newHistoryTestLog(t *testing.T, retention SessionRetention) *KVSessionEventLog {
	t.Helper()
	log, err := NewKVSessionEventLog(KVSessionEventLogConfig{
		KVStore:   newMemKVStore(),
		Retention: retention,
	})
	if err != nil {
		t.Fatalf("NewKVSessionEventLog: %v", err)
	}
	return log
}

// spawnHistoryService wires an agent service to a KV-backed transcript and
// spawns one persistent session on the supplied backend.
func spawnHistoryService(t *testing.T, backend *mockAgentBackend, ctx context.Context) (AgentService, *KVSessionEventLog, string) {
	t.Helper()
	svc, err := NewAgentService(NewInMemoryAgentStore(), map[string]AgentBackend{"mock": backend}, &agentTestLogger{})
	if err != nil {
		t.Fatalf("NewAgentService: %v", err)
	}
	log := newHistoryTestLog(t, SessionRetention{})
	svc.(*agentServiceImpl).SetSessionEventLog(log)
	resp, err := svc.SpawnSession(ctx, &SpawnSessionRequest{Backend: "mock", Persistent: true})
	if err != nil {
		t.Fatalf("SpawnSession: %v", err)
	}
	return svc, log, resp.SessionID
}

// echoBackend answers every prompt with one text block and the given usage.
func echoBackend(answer string, usage *TokenUsage) *mockAgentBackend {
	return &mockAgentBackend{
		promptFn: func(ctx context.Context, req *PromptRequest) (*PromptResponse, error) {
			return &PromptResponse{
				SessionID:  req.SessionID,
				Content:    []ContentBlock{{Type: "text", Text: answer}},
				Model:      "m1",
				StopReason: "end_turn",
				Usage:      usage,
			}, nil
		},
	}
}

// TestReadSessionHistory_SpawnPromptReply is the issue's own acceptance check:
// a spawn, a prompt and a reply through a fake backend must read back as a
// conversation, with the reply's token usage on it. go-quicbaas#793.
func TestReadSessionHistory_SpawnPromptReply(t *testing.T) {
	t.Parallel()

	const tenant = "tenant-history"
	ctx := ContextWithTenant(context.Background(), tenant)
	usage := &TokenUsage{InputTokens: 11, OutputTokens: 22}
	svc, log, sessionID := spawnHistoryService(t, echoBackend("hello back", usage), ctx)

	if _, err := svc.PromptSession(ctx, &PromptSessionRequest{SessionID: sessionID, Prompt: "hi there"}); err != nil {
		t.Fatalf("PromptSession: %v", err)
	}

	page, err := ReadSessionHistory(ctx, log, &SessionHistoryRequest{TenantID: tenant, SessionID: sessionID})
	if err != nil {
		t.Fatalf("ReadSessionHistory: %v", err)
	}
	// system "session spawned", the operator's prompt, the agent's answer.
	wantRoles := []SessionTurnRole{SessionTurnRoleSystem, SessionTurnRoleUser, SessionTurnRoleAssistant}
	if len(page.Turns) != len(wantRoles) {
		t.Fatalf("got %d turns, want %d: %+v", len(page.Turns), len(wantRoles), page.Turns)
	}
	for i, want := range wantRoles {
		if page.Turns[i].Role != want {
			t.Fatalf("turn[%d] role = %q, want %q", i, page.Turns[i].Role, want)
		}
	}
	if page.Turns[1].Text != "hi there" {
		t.Fatalf("prompt turn text = %q", page.Turns[1].Text)
	}
	reply := page.Turns[2]
	if reply.Text != "hello back" {
		t.Fatalf("reply text = %q", reply.Text)
	}
	if reply.Usage == nil || reply.Usage.OutputTokens != 22 || reply.Usage.InputTokens != 11 {
		t.Fatalf("reply usage = %+v, want input=11 output=22", reply.Usage)
	}
	if reply.Model != "m1" || reply.StopReason != "end_turn" {
		t.Fatalf("reply model/stop = %q/%q", reply.Model, reply.StopReason)
	}
	if page.NextCursor != 0 {
		t.Fatalf("next cursor = %d, want 0 on a fully-read transcript", page.NextCursor)
	}
}

// TestReadSessionHistory_UntenantedProducerIsReadable is the regression guard
// for the defect the issue reported: an in-process caller carrying no tenant
// wrote its transcript under the empty tenant, which the KV log refuses, while
// every reader resolved the absent tenant to "system" and found an empty
// conversation. go-quicbaas#793.
func TestReadSessionHistory_UntenantedProducerIsReadable(t *testing.T) {
	t.Parallel()

	producerCtx := context.Background() // exactly an in-process job runner
	svc, log, sessionID := spawnHistoryService(t, echoBackend("worker reply", nil), producerCtx)
	if _, err := svc.PromptSession(producerCtx, &PromptSessionRequest{SessionID: sessionID, Prompt: "do the work"}); err != nil {
		t.Fatalf("PromptSession: %v", err)
	}

	// The reader is a transport, which always resolves a missing tenant to the
	// system tenant before it reaches the orchestrator.
	page, err := ReadSessionHistory(context.Background(), log, &SessionHistoryRequest{
		TenantID:  SystemTenantID,
		SessionID: sessionID,
	})
	if err != nil {
		t.Fatalf("ReadSessionHistory: %v", err)
	}
	var sawPrompt, sawReply bool
	for _, turn := range page.Turns {
		switch {
		case turn.Role == SessionTurnRoleUser && turn.Text == "do the work":
			sawPrompt = true
		case turn.Role == SessionTurnRoleAssistant && turn.Text == "worker reply":
			sawReply = true
		}
	}
	if !sawPrompt || !sawReply {
		t.Fatalf("untenanted transcript unreadable under %q: prompt=%v reply=%v turns=%+v",
			SystemTenantID, sawPrompt, sawReply, page.Turns)
	}
}

// TestReadSessionHistory_TenantIsolation asserts a second tenant reading the
// same session id sees nothing. The transcript keyspace is tenant-prefixed,
// which is what makes that structural rather than a filter someone can forget.
func TestReadSessionHistory_TenantIsolation(t *testing.T) {
	t.Parallel()

	const owner = "tenant-owner"
	ctx := ContextWithTenant(context.Background(), owner)
	svc, log, sessionID := spawnHistoryService(t, echoBackend("secret answer", nil), ctx)
	if _, err := svc.PromptSession(ctx, &PromptSessionRequest{SessionID: sessionID, Prompt: "secret question"}); err != nil {
		t.Fatalf("PromptSession: %v", err)
	}

	page, err := ReadSessionHistory(context.Background(), log, &SessionHistoryRequest{
		TenantID:  "tenant-intruder",
		SessionID: sessionID,
	})
	if err != nil {
		t.Fatalf("ReadSessionHistory: %v", err)
	}
	if len(page.Turns) != 0 {
		t.Fatalf("a second tenant read %d turns of another tenant's conversation: %+v", len(page.Turns), page.Turns)
	}
}

// TestReadSessionHistory_CapTruncatesAndMarks asserts an over-long answer is
// clipped to the documented cap, marked as clipped, and reported as clipped on
// the turn record.
func TestReadSessionHistory_CapTruncatesAndMarks(t *testing.T) {
	t.Parallel()

	const tenant = "tenant-cap"
	const overBy = 5000
	ctx := ContextWithTenant(context.Background(), tenant)
	huge := strings.Repeat("a", MaxSessionEventTextBytes+overBy)
	svc, log, sessionID := spawnHistoryService(t, echoBackend(huge, nil), ctx)

	if _, err := svc.PromptSession(ctx, &PromptSessionRequest{SessionID: sessionID, Prompt: "generate"}); err != nil {
		t.Fatalf("PromptSession: %v", err)
	}
	page, err := ReadSessionHistory(ctx, log, &SessionHistoryRequest{TenantID: tenant, SessionID: sessionID})
	if err != nil {
		t.Fatalf("ReadSessionHistory: %v", err)
	}
	reply := page.Turns[len(page.Turns)-1]
	if reply.Role != SessionTurnRoleAssistant {
		t.Fatalf("last turn role = %q", reply.Role)
	}
	if reply.TruncatedBytes != overBy {
		t.Fatalf("truncated bytes = %d, want %d", reply.TruncatedBytes, overBy)
	}
	if !strings.Contains(reply.Text, truncationMarkerPrefix) {
		t.Fatalf("clipped text carries no truncation marker: ...%q", reply.Text[len(reply.Text)-40:])
	}
	if len(reply.Text) > MaxSessionEventTextBytes+64 {
		t.Fatalf("clipped text is %d bytes, cap is %d", len(reply.Text), MaxSessionEventTextBytes)
	}
}

// TestReadSessionHistory_ToolUseSummary asserts a streaming turn's tool calls
// are summarised on the one assistant turn they happened inside, paired with
// their results.
func TestReadSessionHistory_ToolUseSummary(t *testing.T) {
	t.Parallel()

	const tenant = "tenant-tools"
	ctx := ContextWithTenant(context.Background(), tenant)
	backend := &mockAgentBackend{
		promptFn: func(ctx context.Context, req *PromptRequest) (*PromptResponse, error) {
			req.EventCallback(&SessionEvent{
				SessionID:      req.SessionID,
				Kind:           KindToolInvocation,
				ToolInvocation: &ToolInvocationPayload{InvocationID: "i1", Name: "Read"},
			})
			req.EventCallback(&SessionEvent{
				SessionID:  req.SessionID,
				Kind:       KindToolResult,
				ToolResult: &ToolResultPayload{InvocationID: "i1", Content: "12345"},
			})
			req.EventCallback(&SessionEvent{
				SessionID:      req.SessionID,
				Kind:           KindToolInvocation,
				ToolInvocation: &ToolInvocationPayload{InvocationID: "i2", Name: "Bash"},
			})
			req.EventCallback(&SessionEvent{
				SessionID:  req.SessionID,
				Kind:       KindToolResult,
				ToolResult: &ToolResultPayload{InvocationID: "i2", Content: "boom", IsError: true},
			})
			req.EventCallback(&SessionEvent{
				SessionID:      req.SessionID,
				Kind:           KindAssistantChunk,
				AssistantChunk: &AssistantChunkPayload{Text: "done", StopReason: "end_turn"},
			})
			return &PromptResponse{SessionID: req.SessionID, Model: "m1", StopReason: "end_turn"}, nil
		},
	}
	svc, log, sessionID := spawnHistoryService(t, backend, ctx)
	if _, err := svc.PromptSession(ctx, &PromptSessionRequest{SessionID: sessionID, Prompt: "work"}); err != nil {
		t.Fatalf("PromptSession: %v", err)
	}

	page, err := ReadSessionHistory(ctx, log, &SessionHistoryRequest{TenantID: tenant, SessionID: sessionID})
	if err != nil {
		t.Fatalf("ReadSessionHistory: %v", err)
	}
	reply := page.Turns[len(page.Turns)-1]
	if reply.Role != SessionTurnRoleAssistant || reply.Text != "done" {
		t.Fatalf("last turn = %q/%q", reply.Role, reply.Text)
	}
	if len(reply.Tools) != 2 {
		t.Fatalf("got %d tool summaries, want 2: %+v", len(reply.Tools), reply.Tools)
	}
	if reply.Tools[0].Name != "Read" || reply.Tools[0].ResultBytes != 5 || reply.Tools[0].IsError {
		t.Fatalf("tool[0] = %+v", reply.Tools[0])
	}
	if reply.Tools[1].Name != "Bash" || !reply.Tools[1].IsError {
		t.Fatalf("tool[1] = %+v", reply.Tools[1])
	}
}

// TestReadSessionHistory_Paging asserts a page boundary falls between turns
// and that the cursor walks the whole conversation exactly once.
func TestReadSessionHistory_Paging(t *testing.T) {
	t.Parallel()

	const tenant = "tenant-paging"
	const rounds = 5
	ctx := ContextWithTenant(context.Background(), tenant)
	svc, log, sessionID := spawnHistoryService(t, echoBackend("ok", nil), ctx)
	for i := 0; i < rounds; i++ {
		if _, err := svc.PromptSession(ctx, &PromptSessionRequest{SessionID: sessionID, Prompt: "q"}); err != nil {
			t.Fatalf("PromptSession %d: %v", i, err)
		}
	}

	var all []*SessionTurn
	cursor := uint64(0)
	for pages := 0; ; pages++ {
		if pages > rounds*3 {
			t.Fatalf("cursor did not terminate after %d pages", pages)
		}
		page, err := ReadSessionHistory(ctx, log, &SessionHistoryRequest{
			TenantID: tenant, SessionID: sessionID, Cursor: cursor, Limit: 2,
		})
		if err != nil {
			t.Fatalf("ReadSessionHistory: %v", err)
		}
		if len(page.Turns) > 2 {
			t.Fatalf("page returned %d turns for limit 2", len(page.Turns))
		}
		all = append(all, page.Turns...)
		if page.NextCursor == 0 {
			break
		}
		cursor = page.NextCursor
	}
	// spawn marker + (prompt, reply) per round.
	if want := 1 + rounds*2; len(all) != want {
		t.Fatalf("paged %d turns, want %d", len(all), want)
	}
	for i := 1; i < len(all); i++ {
		if all[i].Sequence <= all[i-1].Sequence {
			t.Fatalf("turn %d sequence %d is not after %d — a page repeated or reordered",
				i, all[i].Sequence, all[i-1].Sequence)
		}
	}
}

// TestReadSessionHistory_RejectsBadInput asserts every unusable request is a
// typed refusal rather than an empty page that looks like a quiet session.
func TestReadSessionHistory_RejectsBadInput(t *testing.T) {
	t.Parallel()

	log := newHistoryTestLog(t, SessionRetention{})
	tests := []struct {
		name string
		log  SessionEventLog
		req  *SessionHistoryRequest
	}{
		{"no log", nil, &SessionHistoryRequest{SessionID: "s1"}},
		{"nil request", log, nil},
		{"no session id", log, &SessionHistoryRequest{TenantID: "t1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			page, err := ReadSessionHistory(context.Background(), tt.log, tt.req)
			if err == nil {
				t.Fatalf("expected a typed error, got page %+v", page)
			}
			var histErr *SessionHistoryError
			if !asSessionHistoryError(err, &histErr) {
				t.Fatalf("error %v is not a *SessionHistoryError", err)
			}
		})
	}
}

// asSessionHistoryError is errors.As specialised to the history error, kept
// local so the table above reads as a single assertion.
func asSessionHistoryError(err error, target **SessionHistoryError) bool {
	e, ok := err.(*SessionHistoryError)
	if ok {
		*target = e
	}
	return ok
}

// TestReadSessionHistory_MissingSessionIsEmptyNotError asserts opening a
// session that has no transcript reads as an empty conversation. An operator
// opening a session whose history retention removed, or that never produced
// one, must not be shown a failure.
func TestReadSessionHistory_MissingSessionIsEmptyNotError(t *testing.T) {
	t.Parallel()

	log := newHistoryTestLog(t, SessionRetention{})
	page, err := ReadSessionHistory(context.Background(), log, &SessionHistoryRequest{
		TenantID: "t1", SessionID: "never-existed",
	})
	if err != nil {
		t.Fatalf("ReadSessionHistory: %v", err)
	}
	if len(page.Turns) != 0 || page.NextCursor != 0 {
		t.Fatalf("expected an empty page, got %+v", page)
	}
}

// TestReadSessionHistory_LimitIsClamped asserts a caller cannot ask for an
// unbounded page.
func TestReadSessionHistory_LimitIsClamped(t *testing.T) {
	t.Parallel()

	const tenant = "tenant-clamp"
	ctx := ContextWithTenant(context.Background(), tenant)
	svc, log, sessionID := spawnHistoryService(t, echoBackend("ok", nil), ctx)
	if _, err := svc.PromptSession(ctx, &PromptSessionRequest{SessionID: sessionID, Prompt: "q"}); err != nil {
		t.Fatalf("PromptSession: %v", err)
	}
	page, err := ReadSessionHistory(ctx, log, &SessionHistoryRequest{
		TenantID: tenant, SessionID: sessionID, Limit: MaxSessionHistoryLimit * 10,
	})
	if err != nil {
		t.Fatalf("ReadSessionHistory: %v", err)
	}
	if len(page.Turns) > MaxSessionHistoryLimit {
		t.Fatalf("page returned %d turns, above the %d cap", len(page.Turns), MaxSessionHistoryLimit)
	}
}
