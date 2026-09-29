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

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// The worker is released for the wait in reviewing (go-quicbaas#938)
// =============================================================================

// TestRunner_AWorkerWhoseWorkDidNotReachTheForgeIsNotReleased is the error
// path. The release is only safe once the branch is pushed: the container is
// the only copy of unpushed work. A publish that fails therefore leaves the
// worker in place for the run's own teardown, which closes it exactly once.
func TestRunner_AWorkerWhoseWorkDidNotReachTheForgeIsNotReleased(t *testing.T) {
	rig := newRunnerRig(t)
	rig.rebindVerification(t, "true")
	rig.workerWrote()
	rig.container.pushErr = errors.New("the remote refused")

	if err := rig.runner.Run(context.Background(), rig.job(t)); err == nil {
		t.Fatal("a publish the remote refused completed the run")
	}
	if released := rig.auditDetails(auditWorkerReleased); len(released) != 0 {
		t.Fatalf("the worker was released with its work unpublished: %v", released)
	}
	rig.agents.mu.Lock()
	closed := slices.Clone(rig.agents.closed)
	rig.agents.mu.Unlock()
	if len(closed) != 1 || closed[0] != "session-1" {
		t.Fatalf("closed = %v, want the one worker closed once by the teardown", closed)
	}
}

// TestRunner_AnApprovedReviewSpawnsNoSecondWorker: the release costs nothing
// on the path most attempts take. An approval merges from the daemon, so the
// released worker is never replaced and the run's teardown has nothing left
// to close.
func TestRunner_AnApprovedReviewSpawnsNoSecondWorker(t *testing.T) {
	rig := deliveredRun(t)
	if got := rig.agents.sessionCount(); got != WorkerSessionCount {
		t.Fatalf("spawns = %d, want the one worker", got)
	}
	rig.agents.mu.Lock()
	closed := slices.Clone(rig.agents.closed)
	rig.agents.mu.Unlock()
	if len(closed) != 1 {
		t.Fatalf("closed = %v, want the worker closed once, on entering reviewing", closed)
	}
	if len(rig.forge.merges) != 1 {
		t.Fatalf("merges = %d, want the approved change landed", len(rig.forge.merges))
	}
}

// TestRunner_EveryRespawnedRoundIsToldWhyItsSessionIsNew: each round runs in a
// fresh container, and its first prompt says so exactly once. The first turn,
// which replaced nothing, is told nothing. On a project whose session_rotation
// is fresh no conversation is read or carried.
func TestRunner_EveryRespawnedRoundIsToldWhyItsSessionIsNew(t *testing.T) {
	rig := newRunnerRig(t)
	rig.sessionRotation = orchestration.SessionRotationFresh
	rig.agents.transcriptFn = func(sessionID string) (*orchestration.ReplayTranscript, error) {
		return workerTranscript(sessionID), nil
	}
	rig.rebindVerification(t, "true")
	rig.workerWrote()
	rig.verdictOnPublish("REQUEST_CHANGES", "keep the fix inside pkg/thing")
	round := 0
	rig.agents.onPrompt = func(*orchestration.PromptSessionRequest) {
		round++
		if round == 2 {
			rig.requestChangesOnANewHead("head-round-2", "still not it")
		}
		if round == 3 {
			rig.approveOnANewHead("head-round-3")
		}
	}
	if err := rig.runner.Run(context.Background(), rig.job(t)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	prompts := rig.agents.promptTexts()
	if len(prompts) != 3 {
		t.Fatalf("prompts = %d, want the first turn and two rounds", len(prompts))
	}
	const note = "the previous one was closed while your pull request was reviewed"
	for i, prompt := range prompts {
		want := i > 0
		if got := strings.Count(prompt, note); (got == 1) != want || got > 1 {
			t.Errorf("prompt %d carries the released note %d time(s), want %v:\n%s", i+1, got, want, prompt)
		}
	}
	if got := rig.agents.sessionCount(); got != 3 {
		t.Fatalf("spawns = %d, want one worker per round, each after the last was released", got)
	}
	if len(rig.agents.transcriptReads) != 0 {
		t.Errorf("transcript reads = %v, want none on a fresh project", rig.agents.transcriptReads)
	}
	for _, released := range rig.auditDetails(auditWorkerReleased) {
		if strings.Contains(released, "replayed_turns=") {
			t.Errorf("worker_released audit %q records a replay on a fresh project", released)
		}
	}
}

// TestRunner_ARespawnedRoundResumesTheConversationByDefault: on a project that
// never chose a session_rotation policy, the session respawned for a review
// round continues the conversation of the one released while the review ran.
// The conversation is read BEFORE the release closes the session, because the
// log answers only for a live session.
func TestRunner_ARespawnedRoundResumesTheConversationByDefault(t *testing.T) {
	rig := newRunnerRig(t)
	rig.rebindVerification(t, "true")
	rig.workerWrote()
	rig.verdictOnPublish("REQUEST_CHANGES", "keep the fix inside pkg/thing")
	rig.agents.transcriptFn = func(sessionID string) (*orchestration.ReplayTranscript, error) {
		return workerTranscript(sessionID), nil
	}
	turn := 0
	rig.agents.onPrompt = func(*orchestration.PromptSessionRequest) {
		turn++
		if turn == 2 {
			rig.approveOnANewHead("head-round-2")
		}
	}
	if err := rig.runner.Run(context.Background(), rig.job(t)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	prompts := rig.agents.promptTexts()
	if len(prompts) != 2 {
		t.Fatalf("prompts = %d, want the first turn and one round", len(prompts))
	}
	for _, want := range []string{
		"continues in a new container after the review",
		"This is the same conversation, continued.",
		"the handler table lives in pkg/router/table.go",
	} {
		if !strings.Contains(prompts[1], want) {
			t.Errorf("the respawned session's first prompt does not carry %q:\n%s", want, prompts[1])
		}
	}
	if strings.Contains(prompts[1], "the previous one was closed while your pull request was reviewed") {
		t.Errorf("a resumed session was told it is fresh:\n%s", prompts[1])
	}
	rig.agents.mu.Lock()
	reads, closedAtRead := slices.Clone(rig.agents.transcriptReads), slices.Clone(rig.agents.closedAtRead)
	rig.agents.mu.Unlock()
	if len(reads) == 0 || reads[0] != "session-1" || closedAtRead[0] != 0 {
		t.Fatalf("transcript reads = %v (closed at read %v), want session-1 read before it was closed", reads, closedAtRead)
	}
	released := rig.auditDetails(auditWorkerReleased)
	if len(released) == 0 || !strings.Contains(released[0], "replayed_turns=2") ||
		!strings.Contains(released[0], "omitted_turns=1") {
		t.Fatalf("worker_released audits = %v, want the first recording the replayed conversation", released)
	}
}

// TestRunner_AReleaseWhoseConversationCannotBeReadRespawnsFresh: a project that
// resumes, on a service whose session log will not answer, still releases its
// worker; the respawned session starts fresh and the audit says why.
func TestRunner_AReleaseWhoseConversationCannotBeReadRespawnsFresh(t *testing.T) {
	rig := newRunnerRig(t)
	rig.rebindVerification(t, "true")
	rig.workerWrote()
	rig.verdictOnPublish("REQUEST_CHANGES", "keep the fix inside pkg/thing")
	rig.agents.transcriptFn = func(sessionID string) (*orchestration.ReplayTranscript, error) {
		return nil, &orchestration.SessionTranscriptUnavailableError{SessionID: sessionID, Reason: "no session event log is configured"}
	}
	turn := 0
	rig.agents.onPrompt = func(*orchestration.PromptSessionRequest) {
		turn++
		if turn == 2 {
			rig.approveOnANewHead("head-round-2")
		}
	}
	if err := rig.runner.Run(context.Background(), rig.job(t)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	prompts := rig.agents.promptTexts()
	if len(prompts) != 2 || !strings.Contains(prompts[1], "the previous one was closed while your pull request was reviewed") {
		t.Fatalf("prompts = %q, want the respawned session told it is fresh", prompts)
	}
	released := rig.auditDetails(auditWorkerReleased)
	if len(released) == 0 || !strings.Contains(released[0], `resume_unavailable="no session event log is configured"`) {
		t.Fatalf("worker_released audits = %v, want the first naming why the resume was unavailable", released)
	}
}

// TestRunner_AReleaseWhoseConversationReadFailsOtherwiseFailsTheRun: only an
// UNAVAILABLE conversation is replaced fresh. A session the service does not
// know is not what the runner believes it holds, so the failure is returned
// and the worker is left for the run's own teardown rather than released.
func TestRunner_AReleaseWhoseConversationReadFailsOtherwiseFailsTheRun(t *testing.T) {
	rig := newRunnerRig(t)
	rig.rebindVerification(t, "true")
	rig.workerWrote()
	rig.verdictOnPublish("APPROVED", "lgtm")
	rig.agents.transcriptFn = func(sessionID string) (*orchestration.ReplayTranscript, error) {
		return nil, &orchestration.SessionNotFoundError{SessionID: sessionID}
	}

	err := rig.runner.Run(context.Background(), rig.job(t))
	var notFound *orchestration.SessionNotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("run error = %v, want the failed conversation read returned", err)
	}
	if released := rig.auditDetails(auditWorkerReleased); len(released) != 0 {
		t.Errorf("worker_released audits = %v, want none", released)
	}
	if len(rig.forge.merges) != 0 {
		t.Errorf("merges = %d, want none", len(rig.forge.merges))
	}
}
