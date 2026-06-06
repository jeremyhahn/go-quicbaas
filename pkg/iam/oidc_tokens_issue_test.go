//go:build iam

// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// IssueHardwareTokens mints an access/id/refresh set and validates required input.
func TestTokenService_IssueHardwareTokens(t *testing.T) {
	service, privateKey := createTestTokenService(t)
	ctx := context.Background()

	// Missing user is rejected.
	if _, err := service.IssueHardwareTokens(ctx, HardwareTokenOptions{}); err == nil {
		t.Fatal("IssueHardwareTokens(no user) want error")
	}

	set, err := service.IssueHardwareTokens(ctx, HardwareTokenOptions{
		User: &User{
			ID:       "user-1",
			TenantID: "tenant-1",
			Email:    "u@example.com",
			Name:     "U",
			Roles:    []string{"admin"},
			Groups:   []string{"devs"},
		},
		AMR:          []string{"webauthn", "hwk"},
		ACR:          "phr",
		AttestAAGUID: "aaguid-1",
		AttestLevel:  "fips-140-3",
		ClientID:     "client-1",
	})
	if err != nil {
		t.Fatalf("IssueHardwareTokens: %v", err)
	}
	if set.AccessToken == "" || set.IDToken == "" || set.RefreshToken == "" {
		t.Fatalf("token set incomplete: %+v", set)
	}
	if set.TokenType != "Bearer" {
		t.Fatalf("token type = %q, want Bearer", set.TokenType)
	}

	// The ID token verifies and carries the attestation claims.
	claims, err := xkms.JWTVerifyWithKeyAndClaims(set.IDToken, &privateKey.PublicKey, "RS256")
	if err != nil {
		t.Fatalf("verify id token: %v", err)
	}
	if claims["attest_aaguid"] != "aaguid-1" {
		t.Fatalf("attest_aaguid = %v, want aaguid-1", claims["attest_aaguid"])
	}
}

// IssueServiceAccountTokens mints an mTLS-bound access token with a cnf claim and
// validates required input.
func TestTokenService_IssueServiceAccountTokens(t *testing.T) {
	service, privateKey := createTestTokenService(t)
	ctx := context.Background()

	// Missing client_id and thumbprint each error.
	if _, err := service.IssueServiceAccountTokens(ctx, ServiceAccountTokenOptions{CertThumbprintS256: "t"}); err == nil {
		t.Fatal("IssueServiceAccountTokens(no client) want error")
	}
	if _, err := service.IssueServiceAccountTokens(ctx, ServiceAccountTokenOptions{ClientID: "c"}); err == nil {
		t.Fatal("IssueServiceAccountTokens(no thumbprint) want error")
	}

	set, err := service.IssueServiceAccountTokens(ctx, ServiceAccountTokenOptions{
		ClientID:           "svc-1",
		TenantID:           "tenant-1",
		CertThumbprintS256: "thumb-abc",
		AttestMethod:       "tpm",
		AMR:                []string{"mtls", "hwk"},
	})
	if err != nil {
		t.Fatalf("IssueServiceAccountTokens: %v", err)
	}
	if set.AccessToken == "" || set.TokenType != "Bearer" {
		t.Fatalf("service-account token set wrong: %+v", set)
	}

	claims, err := xkms.JWTVerifyWithKeyAndClaims(set.AccessToken, &privateKey.PublicKey, "RS256")
	if err != nil {
		t.Fatalf("verify access token: %v", err)
	}
	cnf, ok := claims["cnf"].(map[string]interface{})
	if !ok || cnf["x5t#S256"] != "thumb-abc" {
		t.Fatalf("cnf claim wrong: %v", claims["cnf"])
	}
	if claims["attest_method"] != "tpm" {
		t.Fatalf("attest_method = %v, want tpm", claims["attest_method"])
	}
}
