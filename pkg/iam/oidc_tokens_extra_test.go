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
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// GenerateIDToken stamps the optional attestation, AMR, and ACR claims when set.
func TestTokenService_GenerateIDToken_OptionalClaims(t *testing.T) {
	service, privateKey := createTestTokenService(t)
	ctx := context.Background()

	now := time.Now()
	claims := &IDTokenClaims{
		Issuer:       "https://test.example.com",
		Subject:      "user-123",
		Audience:     []string{"client-123"},
		ExpiresAt:    NewUnixTime(now.Add(time.Hour)),
		IssuedAt:     NewUnixTime(now),
		AuthTime:     NewUnixTime(now),
		TenantID:     "tenant-1",
		AMR:          []string{"webauthn", "hwk"},
		ACR:          "phr",
		AttestAAGUID: "aaguid-xyz",
		AttestLevel:  "fips-140-3",
	}

	tokenString, err := service.GenerateIDToken(ctx, claims)
	if err != nil {
		t.Fatalf("GenerateIDToken: %v", err)
	}

	mapClaims, err := xkms.JWTVerifyWithKeyAndClaims(tokenString, &privateKey.PublicKey, "RS256")
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if mapClaims["acr"] != "phr" {
		t.Fatalf("acr = %v, want phr", mapClaims["acr"])
	}
	if mapClaims["attest_aaguid"] != "aaguid-xyz" {
		t.Fatalf("attest_aaguid = %v, want aaguid-xyz", mapClaims["attest_aaguid"])
	}
	if mapClaims["attest_level"] != "fips-140-3" {
		t.Fatalf("attest_level = %v, want fips-140-3", mapClaims["attest_level"])
	}
	amr, ok := mapClaims["amr"].([]interface{})
	if !ok || len(amr) != 2 {
		t.Fatalf("amr = %v, want 2 entries", mapClaims["amr"])
	}
}

// ValidateAuthCode is one-time-use: a second validation of the same code fails.
func TestTokenService_ValidateAuthCode_OneTimeUse(t *testing.T) {
	service, _ := createTestTokenService(t)
	ctx := context.Background()

	req := &AuthorizeRequest{
		ClientID:    "client-123",
		TenantID:    "tenant-1",
		RedirectURI: "https://example.com/cb",
		Scope:       []string{"openid"},
	}
	code, err := service.GenerateAuthCode(ctx, req, "user-123")
	if err != nil {
		t.Fatalf("GenerateAuthCode: %v", err)
	}

	if _, err := service.ValidateAuthCode(ctx, code); err != nil {
		t.Fatalf("first ValidateAuthCode: %v", err)
	}
	// Second use must fail (code already consumed).
	if _, err := service.ValidateAuthCode(ctx, code); err == nil {
		t.Fatal("second ValidateAuthCode should fail (one-time use)")
	}

	// Validating an entirely unknown code is not-found.
	if _, err := service.ValidateAuthCode(ctx, "no-such-code"); err == nil {
		t.Fatal("ValidateAuthCode(unknown) should fail")
	}
}
