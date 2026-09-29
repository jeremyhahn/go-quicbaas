// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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
)

// TestSessionRotationPolicy_UnsetResumes: the owner's ruling (2026-09-24) is
// that a project which never chose a policy resumes. Every known policy
// validates; an explicit one is kept as it is.
func TestSessionRotationPolicy_UnsetResumes(t *testing.T) {
	cases := []struct {
		policy SessionRotationPolicy
		want   SessionRotationPolicy
	}{
		{SessionRotationUnset, SessionRotationResume},
		{SessionRotationResume, SessionRotationResume},
		{SessionRotationFresh, SessionRotationFresh},
	}
	for _, tc := range cases {
		t.Run(string(tc.policy), func(t *testing.T) {
			if err := tc.policy.Validate(); err != nil {
				t.Fatalf("Validate(%q) = %v, want nil", tc.policy, err)
			}
			if got := tc.policy.Effective(); got != tc.want {
				t.Errorf("Effective(%q) = %q, want %q", tc.policy, got, tc.want)
			}
		})
	}
	if DefaultSessionRotation != SessionRotationResume {
		t.Errorf("DefaultSessionRotation = %q, want resume", DefaultSessionRotation)
	}
}

// TestSessionRotationPolicy_AnUnknownPolicyIsRefused: a typo is refused rather
// than read as the default, so "frseh" cannot silently leave a project
// resuming.
func TestSessionRotationPolicy_AnUnknownPolicyIsRefused(t *testing.T) {
	for _, policy := range []SessionRotationPolicy{"frseh", "RESUME", " resume", "restart"} {
		err := policy.Validate()
		var invalid *SessionRotationPolicyInvalidError
		if !errors.As(err, &invalid) {
			t.Fatalf("Validate(%q) = %v, want SessionRotationPolicyInvalidError", policy, err)
		}
		if !errors.Is(err, ErrSessionRotationPolicyInvalid) {
			t.Errorf("Validate(%q) does not unwrap to ErrSessionRotationPolicyInvalid", policy)
		}
		if invalid.Policy != string(policy) || !strings.Contains(err.Error(), `"resume" or "fresh"`) {
			t.Errorf("Validate(%q) error = %q, want the refused value and the accepted ones named", policy, err)
		}
	}
}

// TestReplayTranscript_RenderIsTheConversationWithoutFraming: Render is the
// part every reproduction of a session shares — the turns and the omission
// count — and PrimingMessage is that plus the restore framing.
func TestReplayTranscript_RenderIsTheConversationWithoutFraming(t *testing.T) {
	transcript := &ReplayTranscript{
		SessionID:    "session-1",
		SystemPrompt: "be terse",
		Turns: []ReplayTurn{
			{Role: ReplayRoleUser, Text: "implement the router"},
			{Role: ReplayRoleAssistant, Text: "done"},
		},
		Omitted: 2,
	}
	rendered := transcript.Render()
	for _, want := range []string{
		"--- transcript of session session-1 ---", "2 earlier turn(s) omitted",
		"[user]\nimplement the router", "[assistant]\ndone", "--- end of transcript ---",
	} {
		if !strings.Contains(rendered, want) {
			t.Errorf("Render() does not carry %q:\n%s", want, rendered)
		}
	}
	if strings.Contains(rendered, "be terse") || strings.Contains(rendered, replayPrimingHeader()) {
		t.Errorf("Render() carries framing it must leave to its caller:\n%s", rendered)
	}
	priming := transcript.PrimingMessage()
	if !strings.HasPrefix(priming, replayPrimingHeader()) || !strings.HasSuffix(priming, rendered) {
		t.Errorf("PrimingMessage() is not the restore framing around Render():\n%s", priming)
	}
	if got := (*ReplayTranscript)(nil).Render(); got != "" {
		t.Errorf("Render() on a nil transcript = %q, want empty", got)
	}
}

// TestSessionTranscript_ReadsTheConversationTheLogHolds: the conversation a
// resumed worker continues is the one the durable session log holds — the
// prompts and the agent's merged answer — and carries no system prompt,
// because a replacement is spawned with its own.
func TestSessionTranscript_ReadsTheConversationTheLogHolds(t *testing.T) {
	h, ctx, sessionID := newFreshnessHarness(t)
	now := time.Now().UTC()
	for _, event := range []*SessionEvent{
		{Kind: KindPromptSubmitted, Prompt: &PromptSubmittedPayload{Prompt: "implement the router", SystemPrompt: "charter"}},
		{Kind: KindAssistantChunk, AssistantChunk: &AssistantChunkPayload{Text: "the handler table "}},
		{Kind: KindAssistantChunk, AssistantChunk: &AssistantChunkPayload{Text: "is in table.go"}},
	} {
		event.SessionID, event.TenantID, event.Timestamp = sessionID, "tenant-a", now
		h.service.appendSessionEvent(ctx, event)
	}

	transcript, err := h.service.SessionTranscript(ctx, sessionID)
	if err != nil {
		t.Fatalf("SessionTranscript: %v", err)
	}
	if transcript.SessionID != sessionID || transcript.SystemPrompt != "" {
		t.Errorf("transcript = %+v, want session %s and no system prompt", transcript, sessionID)
	}
	if len(transcript.Turns) != 2 ||
		transcript.Turns[0].Text != "implement the router" ||
		transcript.Turns[1].Text != "the handler table is in table.go" {
		t.Fatalf("turns = %+v, want the prompt and the merged answer", transcript.Turns)
	}

	run := &JobRun{Agents: h.service}
	viaJob, err := run.SessionTranscript(ctx, sessionID)
	if err != nil || len(viaJob.Turns) != 2 {
		t.Errorf("JobRun.SessionTranscript = %+v, %v; want the same conversation", viaJob, err)
	}
}

// TestSessionTranscript_RefusesWhatItCannotAnswerFor: an empty or unknown
// session is a typed refusal, another tenant's session does not exist for this
// caller, and a service with no transcripts at all is reported as the one
// typed error a runner handles for "no conversation to carry".
func TestSessionTranscript_RefusesWhatItCannotAnswerFor(t *testing.T) {
	h, ctx, sessionID := newFreshnessHarness(t)

	if _, err := h.service.SessionTranscript(ctx, ""); !errors.Is(err, ErrSessionIDRequired) {
		t.Errorf("SessionTranscript(\"\") error = %v, want ErrSessionIDRequired", err)
	}
	var notFound *SessionNotFoundError
	if _, err := h.service.SessionTranscript(ctx, "session-nobody-spawned"); !errors.As(err, &notFound) {
		t.Errorf("an unknown session's error = %v, want SessionNotFoundError", err)
	}
	other := ContextWithTenant(context.Background(), "tenant-b")
	if _, err := h.service.SessionTranscript(other, sessionID); !errors.As(err, &notFound) {
		t.Errorf("another tenant's session error = %v, want SessionNotFoundError", err)
	}

	var unavailable *SessionTranscriptUnavailableError
	h.service.SetSessionEventLog(nil)
	if _, err := h.service.SessionTranscript(ctx, sessionID); !errors.As(err, &unavailable) {
		t.Errorf("a service with no event log answered %v, want SessionTranscriptUnavailableError", err)
	}
	for _, run := range []*JobRun{{Agents: &plainAgentsStub{}}, {Agents: nil}} {
		if _, err := run.SessionTranscript(ctx, sessionID); !errors.As(err, &unavailable) {
			t.Errorf("JobRun over a service that keeps no transcripts answered %v, want SessionTranscriptUnavailableError", err)
		}
	}
}
