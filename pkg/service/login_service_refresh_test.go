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

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
)

// fakeRefreshIssuer is a test double for SessionRefreshIssuer.
type fakeRefreshIssuer struct {
	value      string
	expiresAt  time.Time
	err        error
	gotBearer  string
	invocation int
}

func (f *fakeRefreshIssuer) IssueRefreshForSessionBearer(_ context.Context, bearer string) (string, time.Time, error) {
	f.invocation++
	f.gotBearer = bearer
	if f.err != nil {
		return "", time.Time{}, f.err
	}
	return f.value, f.expiresAt, nil
}

// refreshIssuerDownError is the failure a wired-but-broken issuer reports.
type refreshIssuerDownError struct{}

func (e *refreshIssuerDownError) Error() string { return "token service unavailable" }

// =============================================================================
// FinishLogin -> refresh credential
// =============================================================================

func TestLoginService_FinishLoginAttachesRefreshCredential(t *testing.T) {
	deadline := time.Now().Add(7 * 24 * time.Hour).UTC().Truncate(time.Second)
	client := &fakeLoginClient{
		finishResp: &qrdbtransport.FinishLoginResponse{
			Bearer:    "session-bearer",
			ExpiresAt: time.Now().Add(15 * time.Minute),
			Identity:  &qrdbtransport.Identity{Subject: "alice", TenantID: "acme"},
			FactorID:  "factor-1",
		},
	}
	issuer := &fakeRefreshIssuer{value: "refresh-1", expiresAt: deadline}

	svc := NewLoginService(client)
	svc.SetSessionRefreshIssuer(issuer)

	got, err := svc.FinishLogin(context.Background(), &qrdbtransport.FinishLoginRequest{
		SessionID: "s1",
		Type:      qrdbtransport.FactorTypeFIDO2,
	})
	if err != nil {
		t.Fatalf("FinishLogin: %v", err)
	}

	// The qrdb half is promoted unchanged.
	if got.Bearer != "session-bearer" || got.FactorID != "factor-1" {
		t.Fatalf("ceremony fields not promoted: %+v", got)
	}
	if got.Identity == nil || got.Identity.Subject != "alice" {
		t.Fatalf("identity not promoted: %+v", got.Identity)
	}

	// The QuicBaaS half is layered on.
	if got.RefreshToken != "refresh-1" {
		t.Fatalf("RefreshToken = %q, want refresh-1", got.RefreshToken)
	}
	if !got.RefreshExpiresAt.Equal(deadline) {
		t.Fatalf("RefreshExpiresAt = %v, want %v", got.RefreshExpiresAt, deadline)
	}
	if issuer.gotBearer != "session-bearer" {
		t.Fatalf("issuer received bearer %q, want session-bearer", issuer.gotBearer)
	}
}

func TestLoginService_FinishLoginWithoutIssuerStillCompletes(t *testing.T) {
	// Before the OIDC provider is bound (early boot) the ceremony still works;
	// the session simply cannot be renewed.
	client := &fakeLoginClient{
		finishResp: &qrdbtransport.FinishLoginResponse{Bearer: "bearer-only"},
	}
	svc := NewLoginService(client)

	got, err := svc.FinishLogin(context.Background(), &qrdbtransport.FinishLoginRequest{
		SessionID: "s1",
		Type:      qrdbtransport.FactorTypePIV,
	})
	if err != nil {
		t.Fatalf("FinishLogin: %v", err)
	}
	if got.Bearer != "bearer-only" {
		t.Fatalf("Bearer = %q", got.Bearer)
	}
	if got.RefreshToken != "" {
		t.Fatalf("RefreshToken = %q, want empty with no issuer wired", got.RefreshToken)
	}
}

func TestLoginService_FinishLoginFailsWhenRefreshIssuanceFails(t *testing.T) {
	// A bearer with no way to renew it is exactly the failure mode this work
	// removes: the client would look signed in and then die at the 15-minute
	// boundary. Reject the login instead.
	client := &fakeLoginClient{
		finishResp: &qrdbtransport.FinishLoginResponse{Bearer: "session-bearer"},
	}
	cause := &refreshIssuerDownError{}
	svc := NewLoginService(client)
	svc.SetSessionRefreshIssuer(&fakeRefreshIssuer{err: cause})

	_, err := svc.FinishLogin(context.Background(), &qrdbtransport.FinishLoginRequest{
		SessionID: "s1",
		Type:      qrdbtransport.FactorTypeFIDO2,
	})

	var issuance *LoginRefreshIssuanceError
	if !errors.As(err, &issuance) {
		t.Fatalf("error = %v, want LoginRefreshIssuanceError", err)
	}
	if !errors.Is(err, error(cause)) {
		t.Fatalf("error %v does not wrap the issuer failure", err)
	}
}

func TestLoginService_FinishLoginSkipsRefreshForEmptyBearer(t *testing.T) {
	// A ceremony that returned no bearer has nothing to renew; asking the
	// issuer would fail on a nonsensical input.
	client := &fakeLoginClient{
		finishResp: &qrdbtransport.FinishLoginResponse{},
	}
	issuer := &fakeRefreshIssuer{value: "refresh-1"}
	svc := NewLoginService(client)
	svc.SetSessionRefreshIssuer(issuer)

	got, err := svc.FinishLogin(context.Background(), &qrdbtransport.FinishLoginRequest{
		SessionID: "s1",
		Type:      qrdbtransport.FactorTypePIV,
	})
	if err != nil {
		t.Fatalf("FinishLogin: %v", err)
	}
	if issuer.invocation != 0 {
		t.Fatalf("issuer invoked %d times for an empty bearer", issuer.invocation)
	}
	if got.RefreshToken != "" {
		t.Fatalf("RefreshToken = %q, want empty", got.RefreshToken)
	}
}
