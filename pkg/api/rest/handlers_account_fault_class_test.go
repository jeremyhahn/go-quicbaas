//go:build rest

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

package rest

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/agent"
	"github.com/jeremyhahn/go-quicbaas/pkg/lease"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/mocks"
)

// =============================================================================
// The spawn fault class across the release wire (go-quicbaas#1023).
//
// A release refused because the account's credential secret is missing must
// reach the daemon's spawn path as a MISSING CREDENTIAL, so the control plane
// parks the work uncharged exactly as it does for an in-process spawn. These
// tests drive the real release route through an httptest server and read it
// back with the daemon's real AccountAPIClient: neither side's half of the
// envelope is assumed.
// =============================================================================

// faultClassTokens is a fixed bearer-token source for the daemon's client.
type faultClassTokens struct{}

func (faultClassTokens) Token(context.Context) (string, error) { return "daemon-token", nil }
func (faultClassTokens) Invalidate()                           {}

// materializeThroughTheDaemonClient serves the release route refusing with
// refusal and returns what the daemon's client makes of it, wrapped the way
// the daemon's credential store wraps a non-miss failure
// (orchestration.AccountSecretStore.Get).
func materializeThroughTheDaemonClient(t *testing.T, refusal error) error {
	t.Helper()
	accountSvc := &mocks.MockAccountService{
		MaterializeAccountCredentialFunc: func(
			context.Context, *service.MaterializeAccountCredentialRequest,
		) (*service.MaterializeAccountCredentialResponse, error) {
			return nil, refusal
		},
	}
	srv := newMaterializeTestServer(accountSvc, daemonSubject("tenant-1"))
	ts := httptest.NewServer(srv.router)
	t.Cleanup(ts.Close)

	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	if err != nil {
		t.Fatalf("logger.New() error = %v", err)
	}
	client, err := agent.NewAccountAPIClient(agent.AccountAPIClientConfig{
		ServerAddr: ts.URL,
		TenantID:   "tenant-1",
		HTTPClient: ts.Client(),
		Tokens:     faultClassTokens{},
		Logger:     &cacheTestLogger{log},
	})
	if err != nil {
		t.Fatalf("NewAccountAPIClient() error = %v", err)
	}

	if _, err = client.MaterializeCredential(context.Background(), "acct-1", "project-x", ""); err == nil {
		t.Fatal("a refused release succeeded")
	}
	var apiErr *agent.AccountAPIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusConflict {
		t.Fatalf("error = %v, want the control plane's 409 as an AccountAPIError", err)
	}
	return &orchestration.AccountStoreError{Message: "failed to read account credential: " + err.Error(), Cause: err}
}

// TestAccountFaultClass_MissingCredentialSurvivesTheReleaseWire is the round
// trip Codex found broken: an unresolved credential_secret_ref must arrive as
// ErrAccountCredentialNotFound and classify as the missing-credential fault.
func TestAccountFaultClass_MissingCredentialSurvivesTheReleaseWire(t *testing.T) {
	refusal := &orchestration.AccountSecretRefUnresolvedError{
		TenantID:  "tenant-1",
		AccountID: "acct-1",
		Ref:       "agents/claude/claude1/oauth_token",
		Reason:    "the secret does not exist in this tenant",
	}

	err := materializeThroughTheDaemonClient(t, refusal)

	if !errors.Is(err, orchestration.ErrAccountCredentialNotFound) {
		t.Errorf("error = %v, want one matching ErrAccountCredentialNotFound", err)
	}
	if got, want := orchestration.SpawnFaultClass(err), orchestration.SpawnFaultClass(refusal); got != want || got == "" {
		t.Errorf("SpawnFaultClass() = %q on the daemon, want %q as on the control plane", got, want)
	}
}

// TestAccountFaultClass_SurvivesAnErrorBodyLongerThanTheDetailBound proves
// the class is decoded from the WHOLE envelope. The client used to cut the body
// at 512 bytes before decoding, so a refusal whose message ran long no longer
// parsed and arrived as an unclassified — charged — failure.
func TestAccountFaultClass_SurvivesAnErrorBodyLongerThanTheDetailBound(t *testing.T) {
	refusal := &orchestration.AccountSecretRefUnresolvedError{
		TenantID:  "tenant-1",
		AccountID: "acct-1",
		Ref:       "agents/claude/claude1/" + strings.Repeat("a-very-long-segment/", 60) + "oauth_token",
		Reason:    "the secret does not exist in this tenant",
	}
	if len(refusal.Error()) <= 1024 {
		t.Fatalf("refusal message is %d bytes; the test needs one well past the detail bound", len(refusal.Error()))
	}

	err := materializeThroughTheDaemonClient(t, refusal)

	if !errors.Is(err, orchestration.ErrAccountCredentialNotFound) {
		t.Errorf("error = %v, want one matching ErrAccountCredentialNotFound", err)
	}
	if got := orchestration.SpawnFaultClass(err); got == "" {
		t.Error("a long refusal lost its fault class")
	}
	var apiErr *agent.AccountAPIError
	if errors.As(err, &apiErr) && len(apiErr.Message) > 600 {
		t.Errorf("error detail is %d bytes, want it bounded", len(apiErr.Message))
	}
}

// TestAccountFaultClass_OtherConflictIsNotAMissingCredential proves the class
// is not inferred from the status: a revoked delivery grant is also a 409, and
// it is a condition the daemon recovers from, not a configuration fault.
func TestAccountFaultClass_OtherConflictIsNotAMissingCredential(t *testing.T) {
	tests := []struct {
		name    string
		refusal error
	}{
		{name: "revoked delivery grant", refusal: &lease.LeaseRevokedError{LeaseID: "lease-1", RevokedAt: time.Now().UTC()}},
		{name: "account not releasable", refusal: &service.AccountNotReleasableError{AccountID: "acct-1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := materializeThroughTheDaemonClient(t, tt.refusal)

			if errors.Is(err, orchestration.ErrAccountCredentialNotFound) {
				t.Errorf("error = %v was classified as a missing credential", err)
			}
			if got := orchestration.SpawnFaultClass(err); got != "" {
				t.Errorf("SpawnFaultClass() = %q, want none", got)
			}
		})
	}
}

// TestAccountErrorStatus_ConflictIs409 proves a lost concurrent write —
// including an inline rotation overtaken by a switch to a secret reference —
// is a 409 the caller may retry, not a 500.
func TestAccountErrorStatus_ConflictIs409(t *testing.T) {
	for _, err := range []error{
		&orchestration.AccountVersionConflictError{TenantID: "t", AccountID: "a"},
		&orchestration.AccountCredentialSourceChangedError{TenantID: "t", AccountID: "a", Ref: "r"},
	} {
		if got := accountErrorStatus(err); got != http.StatusConflict {
			t.Errorf("accountErrorStatus(%T) = %d, want 409", err, got)
		}
	}
}

// TestAccountErrorStatus_UnclassifiedIs500 is the negative side.
func TestAccountErrorStatus_UnclassifiedIs500(t *testing.T) {
	if got := accountErrorStatus(&orchestration.AccountStoreError{Message: "disk"}); got != http.StatusInternalServerError {
		t.Errorf("accountErrorStatus(store error) = %d, want 500", got)
	}
}
