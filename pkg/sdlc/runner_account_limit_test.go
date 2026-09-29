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

package sdlc

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// accountLimited is a prompt failure the way the orchestration layer returns
// one whose account the provider refused, after it has held that account.
func accountLimited(accountID string) error {
	return &orchestration.TurnAccountLimitedError{
		SessionID: "s",
		Limit: &orchestration.AccountLimitedError{
			AccountID:   accountID,
			AvailableAt: time.Date(2026, 9, 23, 1, 0, 0, 0, time.UTC),
			Signal:      "You've hit your session limit",
		},
		Cause: &orchestration.RemoteTurnFailureError{
			Op:                  "Prompt",
			TurnFailureEvidence: orchestration.TurnFailureEvidence{TerminalReason: orchestration.TerminalReasonAPIError},
			Err:                 errors.New("acp: stream-json error"),
		},
	}
}

// TestRunner_ARefusedAccountRollsTheSessionOntoAnother is the owner's rule:
// when a session's account hits its limit, the work moves to another account
// and carries on, uncharged, in the same attempt and round. On a project whose
// session_rotation is fresh the new session starts a new conversation, told
// only why it is new, and no conversation is read.
func TestRunner_ARefusedAccountRollsTheSessionOntoAnother(t *testing.T) {
	rig := newRunnerRig(t)
	rig.sessionRotation = orchestration.SessionRotationFresh
	rig.agents.transcriptFn = func(sessionID string) (*orchestration.ReplayTranscript, error) {
		return workerTranscript(sessionID), nil
	}
	rig.rebindVerification(t, "true")
	rig.workerWrote()
	rig.verdictOnPublish("APPROVED", "lgtm")
	rig.agents.promptErrFn = func(turn int) error {
		if turn == 1 {
			return accountLimited("claude3")
		}
		return nil
	}
	rig.agents.onPrompt = func(*orchestration.PromptSessionRequest) { rig.workerWrote() }

	if err := rig.runner.Run(context.Background(), rig.job(t)); err != nil {
		t.Fatalf("Run: %v — a refused account must not end the attempt", err)
	}
	if len(rig.agents.spawns) != 2 {
		t.Fatalf("spawns = %d, want the refused session replaced by one fresh one", len(rig.agents.spawns))
	}
	first := rig.agents.prompts[0].SessionID
	if !slices.Contains(rig.agents.closed, first) {
		t.Errorf("the refused session %s was not closed", first)
	}
	second := rig.agents.prompts[1]
	if second.SessionID == first || !strings.Contains(second.Prompt, "moved to another account") {
		t.Errorf("the fresh session was not told why it is fresh: session=%s prompt=%q", second.SessionID, second.Prompt)
	}
	rolled := rig.auditDetails(auditSessionRolled)
	if len(rolled) != 1 {
		t.Fatalf("session_rolled audits = %v, want one", rolled)
	}
	for _, want := range []string{"class=account_limited", "account=claude3", "available_at=2026-09-23T01:00:00Z"} {
		if !strings.Contains(rolled[0], want) {
			t.Errorf("session_rolled audit %q is missing %q", rolled[0], want)
		}
	}
	if len(rig.agents.transcriptReads) != 0 {
		t.Errorf("transcript reads = %v, want none on a fresh project", rig.agents.transcriptReads)
	}
	if strings.Contains(second.Prompt, "pkg/router/table.go") {
		t.Errorf("a fresh session was handed the old conversation:\n%s", second.Prompt)
	}
	if resumed := rig.auditDetails(auditSessionResumed); len(resumed) != 0 {
		t.Errorf("session_resumed audits = %v, want none on a fresh project", resumed)
	}
}

// TestRunner_ARefusedAccountResumesTheConversationByDefault: on a project that
// never chose a session_rotation policy, the session moved off a refused
// account continues its conversation on the new one (go-quicbaas#1046 under
// the #997 policy). The conversation is read BEFORE the refused session is
// closed, because the log answers only for a live session.
func TestRunner_ARefusedAccountResumesTheConversationByDefault(t *testing.T) {
	rig := newRunnerRig(t)
	rig.rebindVerification(t, "true")
	rig.workerWrote()
	rig.verdictOnPublish("APPROVED", "lgtm")
	rig.agents.promptErrFn = func(turn int) error {
		if turn == 1 {
			return accountLimited("claude3")
		}
		return nil
	}
	rig.agents.transcriptFn = func(sessionID string) (*orchestration.ReplayTranscript, error) {
		return workerTranscript(sessionID), nil
	}
	rig.agents.onPrompt = func(*orchestration.PromptSessionRequest) { rig.workerWrote() }

	job := rig.job(t)
	if err := rig.runner.Run(context.Background(), job); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(rig.agents.spawns) != 2 {
		t.Fatalf("spawns = %d, want the refused session replaced by one", len(rig.agents.spawns))
	}
	first := rig.agents.prompts[0].SessionID
	if len(rig.agents.transcriptReads) == 0 || rig.agents.transcriptReads[0] != first {
		t.Fatalf("transcript reads = %v, want the refused session %s read", rig.agents.transcriptReads, first)
	}
	if rig.agents.closedAtRead[0] != 0 {
		t.Errorf("the conversation was read after %d session(s) were closed, want before the refused one",
			rig.agents.closedAtRead[0])
	}
	second := rig.agents.prompts[1]
	for _, want := range []string{
		"continues on another account",
		"This is the same conversation, continued.",
		"the handler table lives in pkg/router/table.go",
	} {
		if !strings.Contains(second.Prompt, want) {
			t.Errorf("the resumed session's first prompt does not carry %q:\n%s", want, second.Prompt)
		}
	}
	resumed := rig.auditDetails(auditSessionResumed)
	if len(resumed) != 1 {
		t.Fatalf("session_resumed audits = %v, want one", resumed)
	}
	for _, want := range []string{"class=account_limited", "account=claude3", "replayed_turns=2", "omitted_turns=1"} {
		if !strings.Contains(resumed[0], want) {
			t.Errorf("session_resumed audit %q is missing %q", resumed[0], want)
		}
	}
	if rolled := rig.auditDetails(auditSessionRolled); len(rolled) != 0 {
		t.Errorf("session_rolled audits = %v, want none on a resume", rolled)
	}
	if job.Iteration() != 0 {
		t.Errorf("iteration = %d, want the roll to spend no round", job.Iteration())
	}
}

// TestRunner_ARefusedAccountWhoseConversationCannotBeReadRollsFresh: a project
// that resumes, on a service whose session log will not answer, still gets off
// the refused account — fresh, with the reason on the audit record.
func TestRunner_ARefusedAccountWhoseConversationCannotBeReadRollsFresh(t *testing.T) {
	rig := newRunnerRig(t)
	rig.rebindVerification(t, "true")
	rig.workerWrote()
	rig.verdictOnPublish("APPROVED", "lgtm")
	rig.agents.promptErrFn = func(turn int) error {
		if turn == 1 {
			return accountLimited("claude3")
		}
		return nil
	}
	rig.agents.transcriptFn = func(sessionID string) (*orchestration.ReplayTranscript, error) {
		return nil, &orchestration.SessionTranscriptUnavailableError{SessionID: sessionID, Reason: "no session event log is configured"}
	}
	rig.agents.onPrompt = func(*orchestration.PromptSessionRequest) { rig.workerWrote() }

	if err := rig.runner.Run(context.Background(), rig.job(t)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	rolled := rig.auditDetails(auditSessionRolled)
	if len(rolled) != 1 || !strings.Contains(rolled[0], `resume_unavailable="no session event log is configured"`) {
		t.Fatalf("session_rolled audits = %v, want one naming why the resume was unavailable", rolled)
	}
	if prompt := rig.agents.prompts[1].Prompt; !strings.Contains(prompt, "moved to another account") {
		t.Errorf("the fresh session was not told why it is fresh:\n%s", prompt)
	}
}

// TestRunner_ARefusedAccountWhoseConversationReadFailsOtherwiseFailsTheRun:
// only an UNAVAILABLE conversation is replaced fresh. A session the service
// does not know is not a condition a fresh session answers, so the failure is
// returned and nothing is spawned.
func TestRunner_ARefusedAccountWhoseConversationReadFailsOtherwiseFailsTheRun(t *testing.T) {
	rig := newRunnerRig(t)
	rig.rebindVerification(t, "true")
	rig.workerWrote()
	rig.agents.promptErrFn = func(int) error { return accountLimited("claude3") }
	rig.agents.transcriptFn = func(sessionID string) (*orchestration.ReplayTranscript, error) {
		return nil, &orchestration.SessionNotFoundError{SessionID: sessionID}
	}

	err := rig.runner.Run(context.Background(), rig.job(t))
	var notFound *orchestration.SessionNotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("run error = %v, want the failed conversation read returned", err)
	}
	if len(rig.agents.spawns) != 1 {
		t.Errorf("spawns = %d, want no replacement", len(rig.agents.spawns))
	}
}

// TestRunner_TheSameRefusedAccountTwiceIsNotRolledForever: if the fresh
// session lands on the account that was just refused, the hold did not take,
// and rolling again would loop. The failure is returned so the attempt is
// requeued, uncharged, by the orchestration layer.
func TestRunner_TheSameRefusedAccountTwiceIsNotRolledForever(t *testing.T) {
	rig := newRunnerRig(t)
	rig.rebindVerification(t, "true")
	rig.workerWrote()
	rig.agents.promptErrFn = func(int) error { return accountLimited("claude3") }

	err := rig.runner.Run(context.Background(), rig.job(t))
	if _, limited := orchestration.AccountLimitOf(err); !limited {
		t.Fatalf("run error = %v, want the account limit carried through", err)
	}
	if len(rig.agents.spawns) != 2 {
		t.Errorf("spawns = %d, want exactly one roll off the account", len(rig.agents.spawns))
	}
}

// TestRunner_EachRefusedAccountIsRolledOffOnce: a pool whose accounts run out
// one after another moves the session each time, and only stops when an
// account repeats.
func TestRunner_EachRefusedAccountIsRolledOffOnce(t *testing.T) {
	rig := newRunnerRig(t)
	rig.rebindVerification(t, "true")
	rig.workerWrote()
	rig.verdictOnPublish("APPROVED", "lgtm")
	refused := []string{"claude3", "claude1"}
	rig.agents.promptErrFn = func(turn int) error {
		if turn <= len(refused) {
			return accountLimited(refused[turn-1])
		}
		return nil
	}
	rig.agents.onPrompt = func(*orchestration.PromptSessionRequest) { rig.workerWrote() }

	if err := rig.runner.Run(context.Background(), rig.job(t)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(rig.agents.spawns) != 3 {
		t.Fatalf("spawns = %d, want one fresh session per refused account", len(rig.agents.spawns))
	}
	// The default project resumes, so each move is recorded as a resume.
	if resumed := rig.auditDetails(auditSessionResumed); len(resumed) != 2 {
		t.Fatalf("session_resumed audits = %v, want two", resumed)
	}
}
