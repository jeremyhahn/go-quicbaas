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
	"errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam/dpop"
)

const (
	validateHTM = "GET"
	validateHTU = "https://api.example.com/api/v1/keys"
)

// issueBoundToken mints a cnf.jkt-bound access token via the client_credentials
// grant and returns the access token plus the DPoP key it is bound to.
func issueBoundToken(t *testing.T, provider *OIDCProvider, appStorage *mockAppStorage, scopes ...string) (string, *ecdsa.PrivateKey) {
	t.Helper()
	clientID, secret := registerClientCredentialsApp(t, appStorage)
	dpopKey := newDPoPKey(t)
	issueProof, err := dpop.NewProof(dpopKey, dpopTokenHTM, dpopTokenHTU)
	if err != nil {
		t.Fatalf("NewProof (issue): %v", err)
	}
	resp, err := provider.Token(context.Background(), &TokenRequest{
		GrantType:    GrantTypeClientCredentials,
		ClientID:     clientID,
		ClientSecret: secret,
		Scope:        scopes,
		DPoPProof:    issueProof,
		HTTPMethod:   dpopTokenHTM,
		HTTPURI:      dpopTokenHTU,
	})
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	return resp.AccessToken, dpopKey
}

// issueBearerToken mints an unbound (plain bearer) access token.
func issueBearerToken(t *testing.T, provider *OIDCProvider, appStorage *mockAppStorage, scopes ...string) string {
	t.Helper()
	clientID, secret := registerClientCredentialsApp(t, appStorage)
	resp, err := provider.Token(context.Background(), &TokenRequest{
		GrantType:    GrantTypeClientCredentials,
		ClientID:     clientID,
		ClientSecret: secret,
		Scope:        scopes,
	})
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	return resp.AccessToken
}

// (a) cnf-bound token + valid DPoP proof over the request => allowed.
func TestValidateAccessTokenWithDPoP_BoundValidProof(t *testing.T) {
	provider, _, appStorage, _, _ := createTestOIDCProvider(t)
	token, dpopKey := issueBoundToken(t, provider, appStorage, "keys.read")

	proof, err := dpop.NewProof(dpopKey, validateHTM, validateHTU)
	if err != nil {
		t.Fatalf("NewProof: %v", err)
	}

	claims, err := provider.ValidateAccessTokenWithDPoP(context.Background(), token, proof, validateHTM, validateHTU)
	if err != nil {
		t.Fatalf("ValidateAccessTokenWithDPoP: %v", err)
	}
	if claims.Confirmation == nil || claims.Confirmation.Jkt == "" {
		t.Fatal("expected cnf.jkt on validated claims")
	}
	if len(claims.SCP) != 1 || claims.SCP[0] != "keys.read" {
		t.Fatalf("expected scp [keys.read], got %v", claims.SCP)
	}
}

// (b) cnf-bound token presented as a bearer (no proof) => rejected (fail closed).
func TestValidateAccessTokenWithDPoP_BoundMissingProof(t *testing.T) {
	provider, _, appStorage, _, _ := createTestOIDCProvider(t)
	token, _ := issueBoundToken(t, provider, appStorage, "keys.read")

	_, err := provider.ValidateAccessTokenWithDPoP(context.Background(), token, "", validateHTM, validateHTU)
	if err == nil {
		t.Fatal("expected missing-proof rejection, got nil")
	}
	var missing *dpop.MissingProofError
	if !errors.As(err, &missing) {
		t.Fatalf("expected *dpop.MissingProofError, got %T: %v", err, err)
	}
}

// (c) cnf-bound token + proof with the wrong htu => rejected.
func TestValidateAccessTokenWithDPoP_WrongHTU(t *testing.T) {
	provider, _, appStorage, _, _ := createTestOIDCProvider(t)
	token, dpopKey := issueBoundToken(t, provider, appStorage)

	proof, err := dpop.NewProof(dpopKey, validateHTM, "https://api.example.com/api/v1/secrets")
	if err != nil {
		t.Fatalf("NewProof: %v", err)
	}
	_, err = provider.ValidateAccessTokenWithDPoP(context.Background(), token, proof, validateHTM, validateHTU)
	if err == nil {
		t.Fatal("expected htu mismatch rejection, got nil")
	}
	var binding *dpop.HTTPBindingError
	if !errors.As(err, &binding) {
		t.Fatalf("expected *dpop.HTTPBindingError, got %T: %v", err, err)
	}
}

// (c) cnf-bound token + proof with the wrong htm => rejected.
func TestValidateAccessTokenWithDPoP_WrongHTM(t *testing.T) {
	provider, _, appStorage, _, _ := createTestOIDCProvider(t)
	token, dpopKey := issueBoundToken(t, provider, appStorage)

	proof, err := dpop.NewProof(dpopKey, "DELETE", validateHTU)
	if err != nil {
		t.Fatalf("NewProof: %v", err)
	}
	_, err = provider.ValidateAccessTokenWithDPoP(context.Background(), token, proof, validateHTM, validateHTU)
	if err == nil {
		t.Fatal("expected htm mismatch rejection, got nil")
	}
	var binding *dpop.HTTPBindingError
	if !errors.As(err, &binding) {
		t.Fatalf("expected *dpop.HTTPBindingError, got %T: %v", err, err)
	}
}

// (c) cnf-bound token + proof from a DIFFERENT key (wrong jkt) => rejected.
func TestValidateAccessTokenWithDPoP_WrongKey(t *testing.T) {
	provider, _, appStorage, _, _ := createTestOIDCProvider(t)
	token, _ := issueBoundToken(t, provider, appStorage)

	attackerKey := newDPoPKey(t)
	proof, err := dpop.NewProof(attackerKey, validateHTM, validateHTU)
	if err != nil {
		t.Fatalf("NewProof: %v", err)
	}
	_, err = provider.ValidateAccessTokenWithDPoP(context.Background(), token, proof, validateHTM, validateHTU)
	if err == nil {
		t.Fatal("expected jkt mismatch rejection, got nil")
	}
	var mismatch *dpop.ThumbprintMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("expected *dpop.ThumbprintMismatchError, got %T: %v", err, err)
	}
}

// (d) a replayed proof jti (same proof validated twice) => rejected.
func TestValidateAccessTokenWithDPoP_ReplayRejected(t *testing.T) {
	provider, _, appStorage, _, _ := createTestOIDCProvider(t)
	token, dpopKey := issueBoundToken(t, provider, appStorage)

	proof, err := dpop.NewProof(dpopKey, validateHTM, validateHTU)
	if err != nil {
		t.Fatalf("NewProof: %v", err)
	}
	if _, err := provider.ValidateAccessTokenWithDPoP(context.Background(), token, proof, validateHTM, validateHTU); err != nil {
		t.Fatalf("first validation should succeed: %v", err)
	}
	_, err = provider.ValidateAccessTokenWithDPoP(context.Background(), token, proof, validateHTM, validateHTU)
	if err == nil {
		t.Fatal("expected replay rejection on second use, got nil")
	}
	var replay *dpop.ReplayError
	if !errors.As(err, &replay) {
		t.Fatalf("expected *dpop.ReplayError, got %T: %v", err, err)
	}
}

// (e) plain bearer token (no cnf) => allowed without a proof, exactly as
// ValidateAccessToken.
func TestValidateAccessTokenWithDPoP_PlainBearer(t *testing.T) {
	provider, _, appStorage, _, _ := createTestOIDCProvider(t)
	token := issueBearerToken(t, provider, appStorage, "keys.read")

	claims, err := provider.ValidateAccessTokenWithDPoP(context.Background(), token, "", validateHTM, validateHTU)
	if err != nil {
		t.Fatalf("plain bearer must validate without a proof: %v", err)
	}
	if claims.Confirmation != nil && claims.Confirmation.Jkt != "" {
		t.Fatalf("plain bearer must carry no cnf.jkt, got %v", claims.Confirmation)
	}
}

// A malformed / invalidly-signed token is rejected before the DPoP step.
func TestValidateAccessTokenWithDPoP_BadToken(t *testing.T) {
	provider, _, _, _, _ := createTestOIDCProvider(t)
	_, err := provider.ValidateAccessTokenWithDPoP(context.Background(), "not-a-jwt", "", validateHTM, validateHTU)
	if err == nil {
		t.Fatal("expected rejection of malformed token, got nil")
	}
}
