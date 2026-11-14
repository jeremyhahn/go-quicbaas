package iam

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"testing"
	"time"
)

// TestSessionRefresh_SurvivesADaemonRestart is the regression guard for
// #619: a quicbaasd restart must not end a tenant UI session.
//
// The burn-in node lost every tenant session on each deploy: the SPA landed on
// #/login, and a fresh passkey login needed a person at the display. A session
// outlives the process only if both halves of it do: the access token's
// signing key (persisted in xkms, see TestRotate_SurvivesARestart) and the
// refresh grant (persisted in the IAM TokenStorage, the per-tenant KV store).
//
// The restart is modelled as a second OIDCProvider and a second BackendStorage
// built over the same KV store and the same signing key, with nothing else
// carried across: any grant held only in the first provider's process memory
// is gone, exactly as it is after a daemon restart.
func TestSessionRefresh_SurvivesADaemonRestart(t *testing.T) {
	kv := newRoutingTestKV()
	kv.barrierTenant(burninTenant)

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate signing key: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal signing key: %v", err)
	}
	users := NewMemoryStorage()
	seedCeremonyUser(t, users, burninTenant, burninOperator, []string{operatorRoleID})

	boot := func() *OIDCProvider {
		t.Helper()
		// Each boot parses the key afresh, as a restarted daemon loads it
		// from the keystore rather than sharing the old process's value.
		restored, err := x509.ParsePKCS8PrivateKey(der)
		if err != nil {
			t.Fatalf("parse signing key: %v", err)
		}
		provider, err := NewOIDCProvider(&OIDCConfig{
			Issuer:            "https://ceremony.example.com",
			SigningAlgorithm:  "RS256",
			SigningKeyManager: testSigningKeyManager(t, restored.(*rsa.PrivateKey), "RS256"),
			AccessTokenTTL:    15 * time.Minute,
			RefreshTokenTTL:   7 * 24 * time.Hour,
			IDTokenTTL:        time.Hour,
			AuthCodeTTL:       10 * time.Minute,
		}, OIDCDependencies{
			IdentityStore: NewIdentityStore(IdentityStoreOptions{
				UserStorage: users, RoleStorage: users,
			}),
			AppStorage:   newMockAppStorage(),
			TokenStorage: NewBackendStorage(kv),
		})
		if err != nil {
			t.Fatalf("create OIDC provider: %v", err)
		}
		return provider
	}

	// Before the restart: the ceremony login and the refresh credential the
	// SPA stores beside the bearer (quicbaas_refresh_token).
	ctx := context.Background()
	before := boot()
	bearer, _, err := before.IssueSessionToken(ctx, SessionIdentity{
		Subject:  burninOperator,
		TenantID: burninTenant,
	})
	if err != nil {
		t.Fatalf("IssueSessionToken: %v", err)
	}
	refresh, _, err := before.IssueRefreshForSessionBearer(ctx, bearer)
	if err != nil {
		t.Fatalf("IssueRefreshForSessionBearer: %v", err)
	}

	// After the restart.
	after := boot()
	if _, err := after.ValidateAccessToken(ctx, bearer); err != nil {
		t.Fatalf("the pre-restart bearer must still validate after a restart: %v", err)
	}
	renewed, err := after.RefreshSession(ctx, SessionRefreshRequest{RefreshToken: refresh})
	if err != nil {
		t.Fatalf("the pre-restart refresh credential must renew the session after a restart: %v", err)
	}
	if renewed.AccessToken == "" {
		t.Fatal("renewal after a restart returned no access token")
	}
	if renewed.RefreshToken == "" || renewed.RefreshToken == refresh {
		t.Fatal("renewal after a restart did not rotate the refresh credential")
	}
	claims, err := after.ValidateAccessToken(ctx, renewed.AccessToken)
	if err != nil {
		t.Fatalf("the renewed bearer must validate: %v", err)
	}
	if claims.TenantID != burninTenant || claims.Subject != burninOperator {
		t.Fatalf("renewed bearer names %q in %q, want %q in %q",
			claims.Subject, claims.TenantID, burninOperator, burninTenant)
	}
}
