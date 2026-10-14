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
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// Credential-expiry mirror.
//
// Account.CredentialExpiresAt is the non-secret projection of the credential
// envelope's expiry, and Account.CredentialHealth is derived from it. For a
// static token the operator's declaration at import time is the only expiry
// QuicBaaS will ever have — nothing calls the provider to discover one — so if
// CreateAccount drops it, every static account reports "unknown" forever and
// the expiry warning that exists to catch a lapse BEFORE spawns start failing
// never fires. These tests hold that mirror in place.
// =============================================================================

func TestAccountService_CreateAccount_MirrorsDeclaredExpiry(t *testing.T) {
	svc := newTestAccountService(t)
	expiry := time.Now().UTC().Add(72 * time.Hour).Truncate(time.Second)

	resp, err := svc.CreateAccount(callerContext("tenant-a"), &CreateAccountRequest{
		TenantID:      "tenant-a",
		Provider:      orchestration.AccountProviderClaude,
		RefreshPolicy: orchestration.AccountRefreshNone,
		Credential: &orchestration.AccountCredential{
			AccessTokenEnvKey: "CLAUDE_CODE_OAUTH_TOKEN",
			AccessToken:       "sk-static",
			ExpiresAt:         expiry,
		},
	})
	if err != nil {
		t.Fatalf("CreateAccount() error = %v", err)
	}
	if !resp.Account.CredentialExpiresAt.Equal(expiry) {
		t.Fatalf("CredentialExpiresAt = %v, want %v", resp.Account.CredentialExpiresAt, expiry)
	}
	if got := resp.Account.CredentialHealth(); got != orchestration.AccountCredentialHealthValid {
		t.Fatalf("CredentialHealth() = %q, want valid", got)
	}

	stored, err := svc.GetAccount(callerContext("tenant-a"), &GetAccountRequest{TenantID: "tenant-a", AccountID: resp.Account.ID})
	if err != nil {
		t.Fatalf("GetAccount() error = %v", err)
	}
	if !stored.Account.CredentialExpiresAt.Equal(expiry) {
		t.Fatalf("stored CredentialExpiresAt = %v, want %v", stored.Account.CredentialExpiresAt, expiry)
	}
}

// TestAccountService_CreateAccount_UndeclaredExpiryStaysUnknown is the honest
// other half: no expiry declared means none invented.
func TestAccountService_CreateAccount_UndeclaredExpiryStaysUnknown(t *testing.T) {
	svc := newTestAccountService(t)

	resp, err := svc.CreateAccount(callerContext("tenant-a"), &CreateAccountRequest{
		TenantID:      "tenant-a",
		Provider:      orchestration.AccountProviderClaude,
		RefreshPolicy: orchestration.AccountRefreshNone,
		Credential:    &orchestration.AccountCredential{AccessToken: "sk-static"},
	})
	if err != nil {
		t.Fatalf("CreateAccount() error = %v", err)
	}
	if !resp.Account.CredentialExpiresAt.IsZero() {
		t.Fatalf("CredentialExpiresAt = %v, want zero", resp.Account.CredentialExpiresAt)
	}
	if got := resp.Account.CredentialHealth(); got != orchestration.AccountCredentialHealthUnknown {
		t.Fatalf("CredentialHealth() = %q, want unknown", got)
	}
}

// TestAccountService_RotateAccountCredential_ReplacesExpiry proves a renewed
// static token resets the countdown, and that clearing the declaration clears
// the mirror rather than leaving the retired credential's expiry behind.
func TestAccountService_RotateAccountCredential_ReplacesExpiry(t *testing.T) {
	svc := newTestAccountService(t)
	ctx := callerContext("tenant-a")

	nearlyGone := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	created, err := svc.CreateAccount(ctx, &CreateAccountRequest{
		TenantID:      "tenant-a",
		Provider:      orchestration.AccountProviderClaude,
		RefreshPolicy: orchestration.AccountRefreshNone,
		Credential:    &orchestration.AccountCredential{AccessToken: "sk-old", ExpiresAt: nearlyGone},
	})
	if err != nil {
		t.Fatalf("CreateAccount() error = %v", err)
	}
	if got := created.Account.CredentialHealth(); got != orchestration.AccountCredentialHealthExpiring {
		t.Fatalf("CredentialHealth() = %q, want expiring", got)
	}

	renewed := time.Now().UTC().Add(90 * 24 * time.Hour).Truncate(time.Second)
	if _, err := svc.RotateAccountCredential(ctx, &RotateAccountCredentialRequest{
		TenantID:   "tenant-a",
		AccountID:  created.Account.ID,
		Credential: &orchestration.AccountCredential{AccessToken: "sk-new", ExpiresAt: renewed},
	}); err != nil {
		t.Fatalf("RotateAccountCredential() error = %v", err)
	}

	after, err := svc.GetAccount(ctx, &GetAccountRequest{TenantID: "tenant-a", AccountID: created.Account.ID})
	if err != nil {
		t.Fatalf("GetAccount() error = %v", err)
	}
	if !after.Account.CredentialExpiresAt.Equal(renewed) {
		t.Fatalf("CredentialExpiresAt = %v, want %v", after.Account.CredentialExpiresAt, renewed)
	}
	if got := after.Account.CredentialHealth(); got != orchestration.AccountCredentialHealthValid {
		t.Fatalf("CredentialHealth() = %q, want valid after rotation", got)
	}

	// Rotating to a credential with no declared expiry must retire the old
	// timestamp, not keep describing a credential that is gone.
	if _, err := svc.RotateAccountCredential(ctx, &RotateAccountCredentialRequest{
		TenantID:   "tenant-a",
		AccountID:  created.Account.ID,
		Credential: &orchestration.AccountCredential{AccessToken: "sk-newest"},
	}); err != nil {
		t.Fatalf("RotateAccountCredential(no expiry) error = %v", err)
	}
	cleared, err := svc.GetAccount(ctx, &GetAccountRequest{TenantID: "tenant-a", AccountID: created.Account.ID})
	if err != nil {
		t.Fatalf("GetAccount() error = %v", err)
	}
	if !cleared.Account.CredentialExpiresAt.IsZero() {
		t.Fatalf("CredentialExpiresAt = %v, want zero after rotating to an undeclared expiry", cleared.Account.CredentialExpiresAt)
	}
}
