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

package iam

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestRevokeSubjectCredentials_EndsBearerAndRefresh proves that after the
// subject's credentials are revoked, its access token no longer validates and
// its refresh credential no longer redeems.
func TestRevokeSubjectCredentials_EndsBearerAndRefresh(t *testing.T) {
	ctx := context.Background()
	provider, _ := newSessionRefreshProvider(t, 15*time.Minute, 24*time.Hour)

	access, _, err := provider.IssueSessionToken(ctx, SessionIdentity{
		Subject:    "user-42",
		TenantID:   "acme",
		Roles:      []string{"admin"},
		UserID:     refreshUserID,
		AuthMethod: "fido2",
	})
	if err != nil {
		t.Fatalf("IssueSessionToken: %v", err)
	}
	refresh, _, err := provider.IssueSessionRefresh(ctx, loginSession())
	if err != nil {
		t.Fatalf("IssueSessionRefresh: %v", err)
	}
	if _, err := provider.ValidateAccessToken(ctx, access); err != nil {
		t.Fatalf("access token does not validate before logout: %v", err)
	}

	n, err := provider.RevokeSubjectCredentials(ctx, "acme", "user-42")
	if err != nil {
		t.Fatalf("RevokeSubjectCredentials: %v", err)
	}
	if n < 2 {
		t.Fatalf("revoked %d tokens, want at least the access and refresh token", n)
	}

	if _, err := provider.ValidateAccessToken(ctx, access); err == nil {
		t.Fatal("access token still validates after the subject's credentials were revoked")
	}
	if _, err := provider.RefreshSession(ctx, SessionRefreshRequest{RefreshToken: refresh}); err == nil {
		t.Fatal("refresh credential still redeems after the subject's credentials were revoked")
	}
}

// TestRevokeSubjectCredentials_LeavesOtherTenantAndSubject proves revocation
// ends only the named subject's tokens in the named tenant.
func TestRevokeSubjectCredentials_LeavesOtherTenantAndSubject(t *testing.T) {
	ctx := context.Background()
	provider, storage := newSessionRefreshProvider(t, 15*time.Minute, 24*time.Hour)

	if _, _, err := provider.IssueSessionRefresh(ctx, loginSession()); err != nil {
		t.Fatalf("IssueSessionRefresh: %v", err)
	}

	now := time.Now()
	otherTenant := &Token{
		JTI:       "other-tenant-token",
		Subject:   "user-42",
		TenantID:  "other",
		Scopes:    []string{"openid"},
		Issued:    now,
		Expires:   now.Add(time.Hour),
		TokenType: TokenTypeAccess,
	}
	otherSubject := &Token{
		JTI:       "other-subject-token",
		Subject:   "user-43",
		TenantID:  "acme",
		Scopes:    []string{"openid"},
		Issued:    now,
		Expires:   now.Add(time.Hour),
		TokenType: TokenTypeAccess,
	}
	for _, tok := range []*Token{otherTenant, otherSubject} {
		if err := storage.StoreToken(withTokenTenant(ctx, tok.TenantID), tok); err != nil {
			t.Fatalf("StoreToken %s: %v", tok.JTI, err)
		}
	}

	if _, err := provider.RevokeSubjectCredentials(ctx, "acme", "user-42"); err != nil {
		t.Fatalf("RevokeSubjectCredentials: %v", err)
	}

	for _, tok := range []*Token{otherTenant, otherSubject} {
		revoked, err := storage.IsTokenRevoked(withTokenTenant(ctx, tok.TenantID), tok.JTI)
		if err != nil {
			t.Fatalf("IsTokenRevoked %s: %v", tok.JTI, err)
		}
		if revoked {
			t.Fatalf("token %s (subject %s, tenant %s) was revoked", tok.JTI, tok.Subject, tok.TenantID)
		}
	}
}

// failingRevokeStorage is a token storage whose RevokeToken always fails.
type failingRevokeStorage struct {
	*MemoryStorage
}

var errRevokeInjected = errors.New("injected revoke failure")

func (f *failingRevokeStorage) RevokeToken(context.Context, string) error {
	return errRevokeInjected
}

// TestRevokeSubjectCredentials_FailsClosed proves a revocation that fails is
// reported, not skipped.
func TestRevokeSubjectCredentials_FailsClosed(t *testing.T) {
	ctx := context.Background()
	provider, storage := newSessionRefreshProvider(t, 15*time.Minute, 24*time.Hour)
	if _, _, err := provider.IssueSessionRefresh(ctx, loginSession()); err != nil {
		t.Fatalf("IssueSessionRefresh: %v", err)
	}
	provider.tokenService.storage = &failingRevokeStorage{MemoryStorage: storage}

	_, err := provider.RevokeSubjectCredentials(ctx, "acme", "user-42")
	if err == nil {
		t.Fatal("RevokeSubjectCredentials reported success though a revocation failed")
	}
	var revErr *CredentialRevocationError
	if !errors.As(err, &revErr) {
		t.Fatalf("error %v is not a *CredentialRevocationError", err)
	}
	if revErr.Op != "revoke" {
		t.Fatalf("Op = %q, want revoke", revErr.Op)
	}
	if !errors.Is(err, errRevokeInjected) {
		t.Fatalf("error %v does not wrap the storage failure", err)
	}
}

// TestRevokeSubjectCredentials_RefusesEmptyInput proves an unnamed tenant or
// subject is refused rather than matched loosely.
func TestRevokeSubjectCredentials_RefusesEmptyInput(t *testing.T) {
	provider, _ := newSessionRefreshProvider(t, 15*time.Minute, 24*time.Hour)
	for _, tc := range []struct {
		tenant, subject, field string
	}{
		{"", "user-42", "tenant_id"},
		{"acme", "", "subject"},
	} {
		_, err := provider.RevokeSubjectCredentials(context.Background(), tc.tenant, tc.subject)
		var inErr *CredentialRevocationInputError
		if !errors.As(err, &inErr) {
			t.Fatalf("(%q, %q): error %v is not a *CredentialRevocationInputError", tc.tenant, tc.subject, err)
		}
		if inErr.Field != tc.field {
			t.Fatalf("(%q, %q): Field = %q, want %q", tc.tenant, tc.subject, inErr.Field, tc.field)
		}
	}
}
