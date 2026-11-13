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
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// accountLoginRejected is a prompt failure the way the orchestration layer
// returns one whose account login the provider rejected, after it has taken
// that account out of selection.
func accountLoginRejected(accountID string) error {
	return &orchestration.TurnAccountAuthRejectedError{
		SessionID: "s",
		Rejection: &orchestration.AccountAuthRejectedError{
			AccountID: accountID, Provider: orchestration.AccountProviderCodex, Signal: "401 Unauthorized",
		},
		Cause: errors.New("acp: one-shot execution failed: codex"),
	}
}

// TestRunner_ARejectedLoginRollsTheSessionOntoAnotherAccount: a dead login is
// the same answer as a limit for the work — another account, same attempt,
// uncharged — and the audit names why.
func TestRunner_ARejectedLoginRollsTheSessionOntoAnotherAccount(t *testing.T) {
	rig := newRunnerRig(t)
	rig.sessionRotation = orchestration.SessionRotationFresh
	rig.rebindVerification(t, "true")
	rig.workerWrote()
	rig.verdictOnPublish("APPROVED", "lgtm")
	rig.agents.promptErrFn = func(turn int) error {
		if turn == 1 {
			return accountLoginRejected("codex1")
		}
		return nil
	}
	rig.agents.onPrompt = func(*orchestration.PromptSessionRequest) { rig.workerWrote() }

	if err := rig.runner.Run(context.Background(), rig.job(t)); err != nil {
		t.Fatalf("Run: %v — a rejected login must not end the attempt", err)
	}
	if len(rig.agents.spawns) != 2 {
		t.Fatalf("spawns = %d, want the session replaced once", len(rig.agents.spawns))
	}
	rolled := rig.auditDetails(auditSessionRolled)
	if len(rolled) != 1 || !strings.Contains(rolled[0], "class=account_auth_rejected") ||
		!strings.Contains(rolled[0], "account=codex1") || strings.Contains(rolled[0], "available_at") {
		t.Fatalf("session_rolled audits = %v, want one naming account_auth_rejected and codex1, with no return time", rolled)
	}
}

// TestRunner_TheSameRejectedLoginTwiceIsNotRolledForever: when the replacement
// lands on the same dead login, the error is returned instead of looping.
func TestRunner_TheSameRejectedLoginTwiceIsNotRolledForever(t *testing.T) {
	rig := newRunnerRig(t)
	rig.rebindVerification(t, "true")
	rig.workerWrote()
	rig.agents.promptErrFn = func(int) error { return accountLoginRejected("codex1") }

	err := rig.runner.Run(context.Background(), rig.job(t))
	if !errors.Is(err, orchestration.ErrAccountAuthRejected) {
		t.Fatalf("run error = %v, want the rejected login carried through", err)
	}
	if len(rig.agents.spawns) != 2 {
		t.Errorf("spawns = %d, want exactly one roll off the account", len(rig.agents.spawns))
	}
}
