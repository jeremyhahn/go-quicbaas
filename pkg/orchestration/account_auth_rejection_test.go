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
)

// codexDeadLoginStderr is what codex-cli 0.155.1 printed on worker-host-2 on
// 2026-09-28 when its refresh token had already been rotated away.
const codexDeadLoginStderr = `2026-09-28T14:56:39.373586Z ERROR codex_login::auth::manager: Failed to refresh token: 401 Unauthorized: {
2026-09-28T14:56:39.374434Z ERROR codex_login::auth::manager: Failed to refresh token: Your access token could not be refreshed. Please log out and sign in again.`

func TestAuthRejected_NamesADeadLoginAndNothingElse(t *testing.T) {
	cases := []struct {
		name string
		text string
		want bool
	}{
		{"codex refresh already used", codexDeadLoginStderr, true},
		{"codex expired token", "unexpected status 401 Unauthorized: Provided authentication token is expired.", true},
		{"refresh token reused", `{"error":{"code":"refresh_token_reused"}}`, true},
		{"claude login prompt", "Invalid API key · Please run /login", true},
		{"bare 401 with context", "HTTP 401", true},
		{"a limit is not a login", "rate limit exceeded; retry-after: 120", false},
		{"401 inside a number", "processed 14012 tokens with status ok", false},
		{"401 with no context", "line 401 of main.go", false},
		{"empty", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := authRejected(tc.text); got != tc.want {
				t.Fatalf("authRejected(%q) = %v, want %v", tc.text, got, tc.want)
			}
		})
	}
}

func TestProviderAuthEvidence_CrossesTheDaemonWireRedacted(t *testing.T) {
	in := TurnFailureEvidence{ProviderAuth: &ProviderAuthEvidence{Signal: "401 Unauthorized sk-ant-oat01-" + strings.Repeat("a", 40)}}
	wire := turnFailureEvidenceToProto(in)
	if wire == nil || !wire.GetProviderAuthRejected() {
		t.Fatalf("wire = %+v, want the rejection carried", wire)
	}
	if strings.Contains(wire.GetProviderAuthSignal(), "sk-ant-oat01-aaaa") {
		t.Fatalf("signal = %q, a credential-shaped token crossed the wire", wire.GetProviderAuthSignal())
	}
	out, ok := turnFailureEvidenceFromProto(wire)
	if !ok || out.ProviderAuth == nil || !strings.Contains(out.ProviderAuth.Signal, "401") {
		t.Fatalf("round trip = (%+v, %v), want the rejection rebuilt", out, ok)
	}
	if empty := turnFailureEvidenceToProto(TurnFailureEvidence{}); empty != nil {
		t.Fatalf("empty evidence = %+v, want nil", empty)
	}
}

// sessionAuthRejectedBackend fails every turn the way a remote daemon forwards
// a dead codex login.
type sessionAuthRejectedBackend struct {
	poolResumeBackend
}

func (b *sessionAuthRejectedBackend) Prompt(_ context.Context, req *PromptRequest) (*PromptResponse, error) {
	return nil, &PromptError{SessionID: req.SessionID, Cause: &RemoteTurnFailureError{
		Op: "Prompt", Err: errors.New("rpc error: code = Internal desc = acp: one-shot execution failed: codex"),
		TurnFailureEvidence: TurnFailureEvidence{ProviderAuth: providerAuthEvidence(codexDeadLoginStderr)},
	}}
}

// TestPromptSession_ARejectedLoginErrorsTheAccount: the account leaves
// selection with a last_error an operator reads as "log it in again", and the
// turn's error says the account was refused so the runner can move on.
func TestPromptSession_ARejectedLoginErrorsTheAccount(t *testing.T) {
	backend := &sessionAuthRejectedBackend{poolResumeBackend: poolResumeBackend{autoPick: "acct-a"}}
	h, ctx, sessionID := newLimitHarness(t, backend)

	_, err := h.service.PromptSession(ctx, &PromptSessionRequest{SessionID: sessionID, Prompt: "review"})
	rejected, ok := AccountAuthRejectionOf(err)
	if !ok || rejected.AccountID != "acct-a" || !errors.Is(err, ErrAccountAuthRejected) {
		t.Fatalf("err = %v, want the account's login rejection", err)
	}
	if _, carried := EvidenceOf(err); !carried {
		t.Fatal("the turn's own evidence was lost")
	}
	account, getErr := h.service.accountStore.Get(ctx, "tenant-a", "acct-a")
	if getErr != nil {
		t.Fatalf("Get: %v", getErr)
	}
	if account.Status != AccountStatusError || !strings.HasPrefix(account.LastError, AccountReloginNeededPrefix) {
		t.Fatalf("account = %s %q, want errored with a re-login last_error", account.Status, account.LastError)
	}
	if account.Selectable(account.UpdatedAt) {
		t.Fatal("an account whose login was rejected must not be selectable")
	}
}

// TestPromptSession_AnUnrelatedFailureErrorsNoAccount is the other side: an
// ordinary failure, and a limit, leave the login alone.
func TestPromptSession_AnUnrelatedFailureErrorsNoAccount(t *testing.T) {
	backend := &failingPoolBackend{poolResumeBackend: poolResumeBackend{autoPick: "acct-a"}}
	h, ctx, sessionID := newLimitHarness(t, backend)
	_, err := h.service.PromptSession(ctx, &PromptSessionRequest{SessionID: sessionID, Prompt: "work"})
	if _, rejected := AccountAuthRejectionOf(err); rejected {
		t.Fatalf("err = %v was reported as a rejected login", err)
	}
	account, _ := h.service.accountStore.Get(ctx, "tenant-a", "acct-a")
	if account.Status != AccountStatusActive {
		t.Fatalf("status = %s, want active", account.Status)
	}
}

func TestClassifyEnvironmentFault_ARejectedLoginRequeuesUncharged(t *testing.T) {
	err := &TurnAccountAuthRejectedError{
		SessionID: "s1",
		Rejection: &AccountAuthRejectedError{AccountID: "codex1", Provider: AccountProviderCodex},
		Cause:     errors.New("turn failed"),
	}
	fault, ok := classifyEnvironmentFault(err)
	if !ok || fault.Class != FaultAccountAuthRejected || !strings.Contains(fault.Reason, "codex1") {
		t.Fatalf("classifyEnvironmentFault = (%+v, %v), want account_auth_rejected naming codex1", fault, ok)
	}
	outcome := faultOutcomeFor(&Task{}, fault)
	if !outcome.refund || !outcome.requeue {
		t.Fatalf("outcome = %+v, want refunded and requeued", outcome)
	}
	if !KnownFaultClass(FaultAccountAuthRejected) {
		t.Fatal("the SDLC module must accept account_auth_rejected")
	}
	if reason, failover := AgentFailoverReason(err); !failover || reason != AgentFailoverAccountExpired {
		t.Fatalf("AgentFailoverReason = (%q, %v), want account_expired so the next Agent is tried", reason, failover)
	}
}
