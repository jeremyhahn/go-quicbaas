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
	"testing"
	"time"
)

// TestIssueSessionToken_RoundTrip proves a ceremony-minted session token
// validates back to the same principal claims (Subject/TenantID/Roles/Groups/
// AuthMethod) and carries a future expiry.
func TestIssueSessionToken_RoundTrip(t *testing.T) {
	provider, _, _, _, _ := createTestOIDCProvider(t)

	identity := SessionIdentity{
		Subject:     "user-42",
		TenantID:    "tenant-a",
		Roles:       []string{"admin", "operator"},
		Groups:      []string{"platform"},
		AuthMethod:  "piv",
		MFAVerified: true,
	}

	token, expiresAt, err := provider.IssueSessionToken(t.Context(), identity)
	if err != nil {
		t.Fatalf("IssueSessionToken: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}
	if !expiresAt.After(time.Now()) {
		t.Errorf("expected future expiry, got %v", expiresAt)
	}

	claims, err := provider.ValidateAccessToken(t.Context(), token)
	if err != nil {
		t.Fatalf("ValidateAccessToken: %v", err)
	}
	if claims.Subject != identity.Subject {
		t.Errorf("Subject = %q, want %q", claims.Subject, identity.Subject)
	}
	if claims.TenantID != identity.TenantID {
		t.Errorf("TenantID = %q, want %q", claims.TenantID, identity.TenantID)
	}
	if len(claims.Roles) != 2 || claims.Roles[0] != "admin" {
		t.Errorf("Roles = %v, want [admin operator]", claims.Roles)
	}
	if len(claims.Groups) != 1 || claims.Groups[0] != "platform" {
		t.Errorf("Groups = %v, want [platform]", claims.Groups)
	}
	if claims.AuthMethod != "piv" {
		t.Errorf("AuthMethod = %q, want piv", claims.AuthMethod)
	}
	if !claims.MFAVerified {
		t.Error("expected MFAVerified = true")
	}
}

// TestRevokeSessionToken invalidates a minted bearer so subsequent validation
// fails — the logout path for ceremony-issued session tokens.
func TestRevokeSessionToken(t *testing.T) {
	provider, _, _, _, _ := createTestOIDCProvider(t)

	token, _, err := provider.IssueSessionToken(t.Context(), SessionIdentity{
		Subject:  "user-1",
		TenantID: "tenant-a",
		Roles:    []string{"viewer"},
	})
	if err != nil {
		t.Fatalf("IssueSessionToken: %v", err)
	}

	// Valid before revocation.
	if _, err := provider.ValidateAccessToken(t.Context(), token); err != nil {
		t.Fatalf("token should validate before revoke: %v", err)
	}

	if err := provider.RevokeSessionToken(t.Context(), token); err != nil {
		t.Fatalf("RevokeSessionToken: %v", err)
	}

	// Invalid after revocation.
	if _, err := provider.ValidateAccessToken(t.Context(), token); err == nil {
		t.Fatal("expected validation to fail after revoke")
	}
}
