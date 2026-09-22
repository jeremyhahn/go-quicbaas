// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package server

import (
	"context"
	"testing"
	"time"

	qrdbauth "github.com/jeremyhahn/go-qrdb/pkg/auth"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// setupOIDCTestXKMS initializes the process-global xkms package with an
// in-memory software backend so NewSigningKeyManagerFromXKMS produces a genuine
// ES256 signing key. The backend is reset when the test finishes.
func setupOIDCTestXKMS(t *testing.T) {
	t.Helper()
	xkms.Reset()
	t.Cleanup(xkms.Reset)

	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	if err != nil {
		t.Fatalf("logger.New: %v", err)
	}
	keyStorage, err := xkmssdk.NewMemoryBackend()
	if err != nil {
		t.Fatalf("NewMemoryBackend: %v", err)
	}
	certStorage := xkmssdk.NewStorageMemory()
	softwareBackend, err := xkmssdk.NewSoftwareBackend(&xkmssdk.SoftwareBackendConfig{KeyStorage: keyStorage})
	if err != nil {
		t.Fatalf("NewSoftwareBackend: %v", err)
	}
	ks, err := xkmssdk.NewServiceBackend(&xkmssdk.ServiceBackendConfig{
		Backend:     softwareBackend,
		CertStorage: certStorage,
	})
	if err != nil {
		t.Fatalf("NewServiceBackend: %v", err)
	}
	const backendName = "software"
	if err := xkms.Initialize(&xkms.Config{
		DefaultBackend: backendName,
		Backends:       map[string]xkmssdk.ServiceBackend{backendName: ks},
		Logger:         logger.NewAdapter(log),
	}); err != nil {
		t.Fatalf("xkms.Initialize: %v", err)
	}
}

// newTestOIDCProvider builds a minimal, in-memory OIDC provider suitable for
// issuing and validating session tokens.
func newTestOIDCProvider(t *testing.T) *iam.OIDCProvider {
	t.Helper()
	provider, _ := newTestOIDCProviderWithUsers(t)
	return provider
}

// newTestOIDCProviderWithUsers is newTestOIDCProvider with the user store
// exposed, so a test can seed the IAM record a session bearer is minted from.
// Session issuance JOINS that record for the principal's roles and refuses a
// subject it cannot resolve (go-quicbaas #274), so any test that issues a
// bearer must seed one.
func newTestOIDCProviderWithUsers(t *testing.T) (*iam.OIDCProvider, *iam.MemoryStorage) {
	t.Helper()
	return newTestOIDCProviderWithResolver(t, nil)
}

// newTestOIDCProviderWithResolver is newTestOIDCProviderWithUsers with the
// access-token permission resolver bound. A nil resolver is the seam's
// documented "qrdb not wired" state, in which the scp claim falls back to the
// OIDC scope — so passing nil reproduces the plain fixture exactly, and a test
// about RBAC-resolved authority passes its own resolver instead of standing up
// a second copy of this wiring.
func newTestOIDCProviderWithResolver(t *testing.T, resolver iam.PermissionResolver) (*iam.OIDCProvider, *iam.MemoryStorage) {
	t.Helper()
	setupOIDCTestXKMS(t)

	mgr, err := iam.NewSigningKeyManagerFromXKMS("oidc-session-issuer-test", "")
	if err != nil {
		t.Fatalf("NewSigningKeyManagerFromXKMS: %v", err)
	}

	kv := kvstore.NewMemory()
	backend := iam.NewBackendStorage(kv)
	users := iam.NewMemoryStorage()
	// User persistence moved to qrdb (C1-F); BackendStorage no longer satisfies
	// UserStorage. This test's subject is OIDC token issue/resolve, not user
	// persistence, so the user store is an in-memory double. Apps/tokens stay on
	// the BackendStorage KVStore.
	identityStore := iam.NewIdentityStore(iam.IdentityStoreOptions{
		UserStorage:  users,
		GroupStorage: backend,
		RoleStorage:  backend,
	})

	provider, err := iam.NewOIDCProvider(
		&iam.OIDCConfig{
			Issuer:            "https://issuer.test",
			SigningAlgorithm:  iam.SigningAlgorithmES256,
			SigningKeyManager: mgr,
			AccessTokenTTL:    15 * time.Minute,
			RefreshTokenTTL:   24 * time.Hour,
			IDTokenTTL:        time.Hour,
			AuthCodeTTL:       10 * time.Minute,
			PKCERequired:      true,
			AllowedMethods:    []string{"S256"},
		},
		iam.OIDCDependencies{
			IdentityStore:      identityStore,
			AppStorage:         backend,
			TokenStorage:       backend,
			PermissionResolver: resolver,
		},
	)
	if err != nil {
		t.Fatalf("NewOIDCProvider: %v", err)
	}
	return provider, users
}

// seedSessionUser writes the IAM user record a ceremony subject resolves to.
// The ceremony's subject is the tenant-scoped qrdb username, which the broker
// mirrors as User.Name.
func seedSessionUser(t *testing.T, users *iam.MemoryStorage, tenantID, username string, roles []string) {
	t.Helper()
	// No ID: the identity is the Snowflake the store assigns. A hand-written
	// one would have to be a canonical qrdb Snowflake, and synthesizing it from
	// the tenant and username produced a string that is not one.
	err := users.CreateUser(context.Background(), &iam.User{
		TenantID: tenantID,
		// The session subject is the tenant-scoped qrdb USERNAME, so the handle
		// is the field the resolve runs through; Name is presentation only.
		Username:  username,
		Name:      username,
		Email:     username + "@example.test",
		Roles:     roles,
		Lifecycle: iam.LifecycleActive,
	})
	if err != nil {
		t.Fatalf("seed user %q: %v", username, err)
	}
}

// Issue mints a real bearer for a bound provider, and Resolve round-trips it
// back into an identity carrying the subject, tenant, roles, and factor type.
func TestOIDCSessionIssuer_IssueResolveRoundTrip(t *testing.T) {
	provider, users := newTestOIDCProviderWithUsers(t)
	seedSessionUser(t, users, "acme", "user-42", []string{"admin", "reader"})
	issuer := newOIDCSessionIssuer()
	issuer.SetProvider(provider)
	ctx := context.Background()

	// The roles reaching the bearer come from the seeded record, not from this
	// identity: qrdb's factor-resolved principal carries none.
	identity := &qrdbauth.Identity{
		Subject:  "user-42",
		TenantID: "acme",
		Metadata: map[string]string{authMethodMetadataKey: "piv"},
	}

	token, expiry, err := issuer.Issue(ctx, identity)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if token == "" {
		t.Fatal("Issue returned empty token")
	}
	if expiry.Before(time.Now()) {
		t.Fatalf("expiry %v is in the past", expiry)
	}

	resolved, err := issuer.Resolve(ctx, token)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resolved.Subject != "user-42" || resolved.TenantID != "acme" {
		t.Fatalf("resolved identity wrong: %+v", resolved)
	}
	if len(resolved.Roles) != 2 {
		t.Fatalf("roles not round-tripped: %+v", resolved.Roles)
	}
	if resolved.Metadata == nil || resolved.Metadata[authMethodMetadataKey] != "piv" {
		t.Fatalf("auth method not round-tripped: %+v", resolved.Metadata)
	}
}

// Issue with no auth-method metadata leaves the session AuthMethod empty and
// still mints a valid bearer (the nil-metadata branch).
func TestOIDCSessionIssuer_IssueWithoutMetadata(t *testing.T) {
	provider, users := newTestOIDCProviderWithUsers(t)
	seedSessionUser(t, users, "system", "u", []string{"admin"})
	issuer := newOIDCSessionIssuer()
	issuer.SetProvider(provider)

	token, _, err := issuer.Issue(context.Background(), &qrdbauth.Identity{Subject: "u", TenantID: "system"})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if token == "" {
		t.Fatal("empty token")
	}
}

// A DPoP proof threaded on the login context is bound into the session bearer.
// An invalid proof fails closed.
func TestOIDCSessionIssuer_IssueWithDPoPFromContext(t *testing.T) {
	provider, users := newTestOIDCProviderWithUsers(t)
	// Seeded so the failure below is the DPoP proof and not an unresolvable
	// principal — the join runs first.
	seedSessionUser(t, users, "t", "u", []string{"admin"})
	issuer := newOIDCSessionIssuer()
	issuer.SetProvider(provider)

	// An invalid DPoP proof must fail closed (the bindSessionDPoP error path).
	ctx := iam.ContextWithLoginDPoP(context.Background(), &iam.LoginDPoP{
		Proof:  "not-a-valid-dpop-jwt",
		Method: "POST",
		URI:    "https://issuer.test/login",
	})
	if _, _, err := issuer.Issue(ctx, &qrdbauth.Identity{Subject: "u", TenantID: "t"}); err == nil {
		t.Fatal("Issue with invalid DPoP proof should fail closed")
	}
}

// Revoke invalidates a previously issued bearer.
func TestOIDCSessionIssuer_Revoke(t *testing.T) {
	provider, users := newTestOIDCProviderWithUsers(t)
	seedSessionUser(t, users, "system", "u", []string{"admin"})
	issuer := newOIDCSessionIssuer()
	issuer.SetProvider(provider)
	ctx := context.Background()

	token, _, err := issuer.Issue(ctx, &qrdbauth.Identity{Subject: "u", TenantID: "system"})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if err := issuer.Revoke(ctx, token); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
}

// Resolve surfaces the provider's validation error for a malformed bearer.
func TestOIDCSessionIssuer_ResolveInvalidToken(t *testing.T) {
	provider := newTestOIDCProvider(t)
	issuer := newOIDCSessionIssuer()
	issuer.SetProvider(provider)

	if _, err := issuer.Resolve(context.Background(), "garbage.token.value"); err == nil {
		t.Fatal("Resolve of malformed token should error")
	}
}

// TestBindLoginCeremonyProvider_NonIAMComponentsNoOp covers the type-assert
// guard of bindLoginCeremonyProvider: a nil / non-*iamComponents value must be
// a no-op (no provider binding) rather than a panic.
func TestBindLoginCeremonyProvider_NonIAMComponentsNoOp(t *testing.T) {
	s := &Server{logger: buildTestLogger(t)}

	// A nil service.IAMComponents fails the *iamComponents type assertion and
	// must be handled gracefully.
	s.bindLoginCeremonyProvider(nil)
}
