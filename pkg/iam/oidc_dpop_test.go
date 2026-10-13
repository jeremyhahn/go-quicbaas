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

	"crypto/rsa"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam/dpop"
	"github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// assertAccessTokenClaims verifies an access token against the test RSA signing
// key and returns its claims.
func assertAccessTokenClaims(t *testing.T, signingKey *rsa.PrivateKey, token string) (xkms.JWTClaims, error) {
	t.Helper()
	if token == "" {
		t.Fatal("empty access token")
	}
	return xkms.JWTVerifyWithKeyAndClaims(token, &signingKey.PublicKey, "RS256")
}

const (
	dpopTokenHTM = "POST"
	dpopTokenHTU = "https://test.example.com/oauth/token"
)

// registerClientCredentialsApp registers a confidential client allowed to use
// the client_credentials grant and returns its id + plaintext secret.
func registerClientCredentialsApp(t *testing.T, appStorage *mockAppStorage) (string, string) {
	t.Helper()
	const clientID = "svc-client"
	const secret = "super-secret"
	hash := currentSecretHash(t, secret)
	app := &Application{
		ClientID:     clientID,
		ClientSecret: hash,
		Name:         "Service Client",
		TenantID:     "tenant-1",
		GrantTypes:   []string{GrantTypeClientCredentials},
	}
	if err := appStorage.CreateApp(context.Background(), app); err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	return clientID, secret
}

func newDPoPKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("dpop key: %v", err)
	}
	return k
}

// TestToken_DPoPBindsCnfJkt verifies the token endpoint binds cnf.jkt from a
// presented DPoP proof on the client_credentials grant.
func TestToken_DPoPBindsCnfJkt(t *testing.T) {
	t.Parallel()

	provider, signingKey, appStorage, _, _ := createTestOIDCProvider(t)
	clientID, secret := registerClientCredentialsApp(t, appStorage)

	dpopKey := newDPoPKey(t)
	jkt, err := dpop.Thumbprint(dpopKey.Public())
	if err != nil {
		t.Fatalf("thumbprint: %v", err)
	}
	proof, err := dpop.NewProof(dpopKey, dpopTokenHTM, dpopTokenHTU)
	if err != nil {
		t.Fatalf("NewProof: %v", err)
	}

	resp, err := provider.Token(context.Background(), &TokenRequest{
		GrantType:    GrantTypeClientCredentials,
		ClientID:     clientID,
		ClientSecret: secret,
		Scope:        []string{"app.read"},
		DPoPProof:    proof,
		HTTPMethod:   dpopTokenHTM,
		HTTPURI:      dpopTokenHTU,
	})
	if err != nil {
		t.Fatalf("Token: %v", err)
	}

	claims, err := assertAccessTokenClaims(t, signingKey, resp.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	cnf, ok := claims["cnf"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected cnf claim, got %v", claims["cnf"])
	}
	if cnf["jkt"] != jkt {
		t.Fatalf("cnf.jkt = %v, want %q", cnf["jkt"], jkt)
	}
	// scp must be emitted as an array for the platform-token contract.
	scp, ok := claims["scp"].([]interface{})
	if !ok || len(scp) != 1 || scp[0] != "app.read" {
		t.Fatalf("expected scp [app.read], got %v", claims["scp"])
	}
}

// TestToken_NoDPoPNoCnf verifies a bearer token (no proof) carries no cnf.
func TestToken_NoDPoPNoCnf(t *testing.T) {
	t.Parallel()

	provider, signingKey, appStorage, _, _ := createTestOIDCProvider(t)
	clientID, secret := registerClientCredentialsApp(t, appStorage)

	resp, err := provider.Token(context.Background(), &TokenRequest{
		GrantType:    GrantTypeClientCredentials,
		ClientID:     clientID,
		ClientSecret: secret,
		Scope:        []string{"app.read"},
	})
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	claims, err := assertAccessTokenClaims(t, signingKey, resp.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	if _, present := claims["cnf"]; present {
		t.Fatalf("bearer token must not carry cnf, got %v", claims["cnf"])
	}
}

// TestToken_InvalidDPoPFailsClosed verifies a presented-but-invalid proof is a
// hard failure (no silent downgrade to bearer).
func TestToken_InvalidDPoPFailsClosed(t *testing.T) {
	t.Parallel()

	provider, _, appStorage, _, _ := createTestOIDCProvider(t)
	clientID, secret := registerClientCredentialsApp(t, appStorage)

	dpopKey := newDPoPKey(t)
	// Proof bound to a DIFFERENT method than the request.
	proof, err := dpop.NewProof(dpopKey, "GET", dpopTokenHTU)
	if err != nil {
		t.Fatalf("NewProof: %v", err)
	}

	_, err = provider.Token(context.Background(), &TokenRequest{
		GrantType:    GrantTypeClientCredentials,
		ClientID:     clientID,
		ClientSecret: secret,
		DPoPProof:    proof,
		HTTPMethod:   dpopTokenHTM,
		HTTPURI:      dpopTokenHTU,
	})
	if err == nil {
		t.Fatal("expected invalid_dpop_proof error, got nil")
	}
	oauthErr, ok := err.(*OAuthError)
	if !ok || oauthErr.ErrorCode != ErrOAuthInvalidDPoPProof {
		t.Fatalf("expected ErrOAuthInvalidDPoPProof, got %v", err)
	}
}

// TestToken_DPoPReplayRejected verifies a captured proof cannot be replayed at
// the token endpoint to mint a second bound token.
func TestToken_DPoPReplayRejected(t *testing.T) {
	t.Parallel()

	provider, _, appStorage, _, _ := createTestOIDCProvider(t)
	clientID, secret := registerClientCredentialsApp(t, appStorage)

	dpopKey := newDPoPKey(t)
	proof, err := dpop.NewProof(dpopKey, dpopTokenHTM, dpopTokenHTU)
	if err != nil {
		t.Fatalf("NewProof: %v", err)
	}

	req := &TokenRequest{
		GrantType:    GrantTypeClientCredentials,
		ClientID:     clientID,
		ClientSecret: secret,
		DPoPProof:    proof,
		HTTPMethod:   dpopTokenHTM,
		HTTPURI:      dpopTokenHTU,
	}
	if _, err := provider.Token(context.Background(), req); err != nil {
		t.Fatalf("first token issuance: %v", err)
	}
	_, err = provider.Token(context.Background(), req)
	if err == nil {
		t.Fatal("expected replay rejection, got nil")
	}
	oauthErr, ok := err.(*OAuthError)
	if !ok || oauthErr.ErrorCode != ErrOAuthInvalidDPoPProof {
		t.Fatalf("expected ErrOAuthInvalidDPoPProof on replay, got %v", err)
	}
}

// TestIssueSessionToken_DPoPBindsCnfJkt verifies the login (session) path binds
// cnf.jkt when a DPoP proof accompanies the login.
func TestIssueSessionToken_DPoPBindsCnfJkt(t *testing.T) {
	provider, signingKey, _, _, memStorage := createTestOIDCProvider(t)
	seedCeremonyUser(t, memStorage, "tenant-1", "user-1", []string{"admin"})

	dpopKey := newDPoPKey(t)
	jkt, err := dpop.Thumbprint(dpopKey.Public())
	if err != nil {
		t.Fatalf("thumbprint: %v", err)
	}
	const htm, htu = "POST", "https://test.example.com/login/finish"
	proof, err := dpop.NewProof(dpopKey, htm, htu)
	if err != nil {
		t.Fatalf("NewProof: %v", err)
	}

	token, _, err := provider.IssueSessionToken(context.Background(), SessionIdentity{
		Subject:    "user-1",
		TenantID:   "tenant-1",
		Roles:      []string{"admin"},
		AuthMethod: "piv",
		DPoPProof:  proof,
		DPoPMethod: htm,
		DPoPURI:    htu,
	})
	if err != nil {
		t.Fatalf("IssueSessionToken: %v", err)
	}

	claims, err := assertAccessTokenClaims(t, signingKey, token)
	if err != nil {
		t.Fatal(err)
	}
	cnf, ok := claims["cnf"].(map[string]interface{})
	if !ok || cnf["jkt"] != jkt {
		t.Fatalf("expected session cnf.jkt %q, got %v", jkt, claims["cnf"])
	}
}

// TestIssueSessionToken_NoDPoPNoCnf verifies a login without a proof yields a
// bearer session token.
func TestIssueSessionToken_NoDPoPNoCnf(t *testing.T) {
	provider, signingKey, _, _, memStorage := createTestOIDCProvider(t)
	seedCeremonyUser(t, memStorage, "tenant-1", "user-1", []string{"admin"})

	token, _, err := provider.IssueSessionToken(context.Background(), SessionIdentity{
		Subject:    "user-1",
		TenantID:   "tenant-1",
		AuthMethod: "piv",
	})
	if err != nil {
		t.Fatalf("IssueSessionToken: %v", err)
	}
	claims, err := assertAccessTokenClaims(t, signingKey, token)
	if err != nil {
		t.Fatal(err)
	}
	if _, present := claims["cnf"]; present {
		t.Fatalf("bearer session token must not carry cnf, got %v", claims["cnf"])
	}
}
