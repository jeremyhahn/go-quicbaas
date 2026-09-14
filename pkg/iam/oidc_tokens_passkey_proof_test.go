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
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/platformtoken"
)

// TestPasskeyLogin_AdminTokenAuthorizesOrchestrator is the decisive in-process
// functional proof for the passkey-login token-issuance fix. It exercises the
// full chain the live daemon runs after a successful passkey login:
//
//	IssueHardwareTokens(admin) -> signed access token -> platformtoken.Validator
//	verification -> AuthorizedContext.RequireScope("orchestrator:status")
//
// Before the fix the admin token carried no roles, an unusable raw-bytes
// subject, and an OIDC-scope-only scp, so RequireScope returned a
// ScopeDeniedError (HTTP 403). After the fix the token carries the canonical
// string subject, the admin role, and the qrdb-resolved "orchestrator:*"
// wildcard, so RequireScope("orchestrator:status") succeeds (the chain the
// GET /api/v1/orchestrator/health endpoint runs => HTTP 200).
func TestPasskeyLogin_AdminTokenAuthorizesOrchestrator(t *testing.T) {
	const (
		issuer       = "https://broker.example.com"
		kid          = "test-kid-1"
		adminSubject = "1234567890123456789" // canonical decimal-string user id
	)

	// Issuer signing key with a matching kid + JWKS, as the live broker emits.
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("issuer key: %v", err)
	}
	jwks := &jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
		Key:       priv.Public(),
		KeyID:     kid,
		Algorithm: "ES256",
		Use:       "sig",
	}}}

	// qrdb RBAC resolver granting the admin role the orchestrator wildcard.
	rbac := &fakeRBACClient{permsByRole: map[string][]string{
		"admin": {"orchestrator:*", "keys:*", "scheduler:*"},
	}}

	config := &OIDCConfig{
		Issuer:           issuer,
		SigningAlgorithm: "ES256",
		SigningKey:       priv,
		AccessTokenTTL:   time.Hour,
		RefreshTokenTTL:  24 * time.Hour,
		IDTokenTTL:       time.Hour,
		AuthCodeTTL:      10 * time.Minute,
	}
	svc := NewTokenService(newMockTokenStorage(), priv, config)
	svc.signingAlg = "ES256"
	svc.currentKID = kid // stamp the kid so the validator's JWKS lookup resolves
	svc.permResolver = &fakePermissionResolver{client: rbac}

	// Mint the hardware (passkey) token set for the admin, exactly as the REST
	// webauthn login-finish path does.
	set, err := svc.IssueHardwareTokens(context.Background(), HardwareTokenOptions{
		User: &User{
			ID:       adminSubject,
			TenantID: "system",
			Email:    "admin@example.com",
			Name:     "Admin",
			Roles:    []string{"admin"},
		},
		AMR: []string{"webauthn", "hwk"},
		ACR: ACRHardwareAttested,
		// The hardware ACR now requires proof: IssueHardwareTokens refuses to
		// mint it from a caller's assertion alone.
		Attestation: testHardwareAttestation("cb69481e"),
	})
	if err != nil {
		t.Fatalf("IssueHardwareTokens: %v", err)
	}

	// Validate with the real platform-token validator (the downstream resource
	// server's verification), then run the orchestrator scope decision.
	validator, err := platformtoken.NewValidator(platformtoken.Options{
		Issuer:   issuer,
		Audience: issuer, // hardware tokens set aud == issuer
		JWKS:     jwks,
	})
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}

	claims, err := validator.ValidatePlatformToken(context.Background(), set.AccessToken)
	if err != nil {
		t.Fatalf("ValidatePlatformToken: %v", err)
	}

	// Decoded-claims assertions (the live-token proof).
	if claims.Subject != adminSubject {
		t.Errorf("sub = %q, want canonical string %q (raw-bytes regression)", claims.Subject, adminSubject)
	}
	if len(claims.Roles) != 1 || claims.Roles[0] != "admin" {
		t.Errorf("roles = %v, want [admin]", claims.Roles)
	}

	ac := platformtoken.Authorize(claims)
	if !ac.HasScope("orchestrator:*") {
		t.Errorf("scp missing orchestrator:* wildcard; got %v", ac.Scopes)
	}

	// GET /api/v1/orchestrator/health gate: RequireScope must succeed (200), the
	// exact decision that returned 403 before the fix.
	if err := ac.RequireScope("orchestrator:status"); err != nil {
		t.Fatalf("RequireScope(orchestrator:status) = %v, want nil (HTTP 200)", err)
	}

	// Negative control: a no-role user must still be DENIED the orchestrator
	// scope (no over-grant) when issued through the same path.
	noRole, err := svc.IssueHardwareTokens(context.Background(), HardwareTokenOptions{
		User: &User{ID: "55", TenantID: "system", Email: "n@x.com", Name: "N"},
	})
	if err != nil {
		t.Fatalf("IssueHardwareTokens(no role): %v", err)
	}
	nc, err := validator.ValidatePlatformToken(context.Background(), noRole.AccessToken)
	if err != nil {
		t.Fatalf("ValidatePlatformToken(no role): %v", err)
	}
	if err := platformtoken.Authorize(nc).RequireScope("orchestrator:status"); err == nil {
		t.Fatal("no-role token must be DENIED orchestrator:status (403), got nil")
	}
}
