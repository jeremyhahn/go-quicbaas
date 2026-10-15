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

	"github.com/jeremyhahn/go-quicbaas/pkg/auth/liveness"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/platformtoken"
)

// TestUIRefreshFlow_RenewedTokenAuthorizesTheSameEndpointTheLoginTokenDid is
// the decisive in-process reproduction for the burn-in owner's report: the UI
// logs in with the xKey, an ordinary request 401s, POST /api/v1/iam/refresh
// SUCCEEDS, and the replay with the ROTATED bearer 401s again.
//
// It exercises the exact chain the live daemon runs on that second request:
//
//	OIDCProvider.IssueSessionToken (unified login ceremony bearer)
//	  -> OIDCProvider.IssueRefreshForSessionBearer (freezes the session)
//	  -> OIDCProvider.RefreshSession (POST /api/v1/iam/refresh)
//	  -> platformtoken.Validator.Validate / Authorize, backed by the SAME
//	     liveness.Checker(OIDCProvider) AuthenticateHTTP uses.
//
// If the renewed bearer is accepted wherever the login bearer was, the defect
// is NOT reproducible from token issuance/validation alone and the owner must
// capture the live 401's response body / WWW-Authenticate detail and the
// exact endpoint the UI called (browser devtools Network tab) instead.
func TestUIRefreshFlow_RenewedTokenAuthorizesTheSameEndpointTheLoginTokenDid(t *testing.T) {
	const (
		issuer       = "https://agent-04:18443"
		requiredPerm = "orchestrator:status"
		tenantID     = "acme"
		roleID       = "r-admin" // qrdb role ID, as user.Roles carries them
		roleName     = "admin"   // resolved role NAME, as the groups claim carries them
	)

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("issuer key: %v", err)
	}

	storage := NewMemoryStorage()
	identityStore := NewIdentityStore(IdentityStoreOptions{
		UserStorage: storage,
		RoleStorage: storage,
	})
	if err := storage.CreateRole(context.Background(), &Role{ID: roleID, TenantID: tenantID, Name: roleName}); err != nil {
		t.Fatalf("seed role: %v", err)
	}
	if err := storage.CreateUser(context.Background(), &User{
		ID:        71,
		TenantID:  tenantID,
		Username:  "owner",
		Name:      "Burn-in Owner",
		Email:     "owner@example.com",
		Roles:     []string{roleID},
		Lifecycle: LifecycleActive,
	}); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	rbac := &fakeRBACClient{permsByRole: map[string][]string{roleID: {"orchestrator:*"}}}

	config := &OIDCConfig{
		Issuer:            issuer,
		SigningAlgorithm:  "ES256",
		SigningKeyManager: testSigningKeyManager(t, priv, "ES256"),
		AccessTokenTTL:    15 * time.Minute,
		RefreshTokenTTL:   24 * time.Hour,
		IDTokenTTL:        time.Hour,
		AuthCodeTTL:       10 * time.Minute,
	}
	provider, err := NewOIDCProvider(config, OIDCDependencies{
		IdentityStore:      identityStore,
		AppStorage:         newMockAppStorage(),
		TokenStorage:       storage,
		PermissionResolver: &fakePermissionResolver{client: rbac},
	})
	if err != nil {
		t.Fatalf("NewOIDCProvider: %v", err)
	}
	// The manager stamps this kid on every token it signs; the platform
	// validator's key set below publishes the same kid.
	kid, _, _ := provider.config.SigningKeyManager.Current()

	ctx := context.Background()

	// --- Login: the unified ceremony bearer (oidcSessionIssuer.Issue). ---
	loginBearer, _, err := provider.IssueSessionToken(ctx, SessionIdentity{
		Subject:     "owner@example.com",
		TenantID:    tenantID,
		MFAVerified: true,
		AuthMethod:  "fido2",
	})
	if err != nil {
		t.Fatalf("IssueSessionToken: %v", err)
	}
	refreshToken, _, err := provider.IssueRefreshForSessionBearer(ctx, loginBearer)
	if err != nil {
		t.Fatalf("IssueRefreshForSessionBearer: %v", err)
	}

	// --- Refresh: POST /api/v1/iam/refresh (iam_login.go RefreshToken). ---
	renewed, err := provider.RefreshSession(ctx, SessionRefreshRequest{RefreshToken: refreshToken})
	if err != nil {
		t.Fatalf("RefreshSession: %v (the refresh call itself must succeed — the owner saw it succeed)", err)
	}

	// --- Validate exactly like AuthenticateHTTP does. ---
	jwks := &jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
		Key:       priv.Public(),
		KeyID:     kid,
		Algorithm: "ES256",
		Use:       "sig",
	}}}
	validator, err := platformtoken.NewValidator(platformtoken.Options{
		Issuer:          issuer,
		Audience:        issuer,
		JWKS:            jwks,
		LivenessChecker: liveness.NewChecker(provider),
	})
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}

	loginClaims, loginErr := validator.ValidatePlatformToken(ctx, loginBearer)
	renewedClaims, renewedErr := validator.ValidatePlatformToken(ctx, renewed.AccessToken)
	if loginErr != nil {
		t.Fatalf("ValidatePlatformToken(login bearer) = %v, want nil", loginErr)
	}
	if renewedErr != nil {
		t.Fatalf("ValidatePlatformToken(renewed bearer) = %v, want nil (signature/iss/aud/exp)", renewedErr)
	}

	_, loginValidateErr := validator.Validate(ctx, loginBearer, "", "", "")
	_, renewedValidateErr := validator.Validate(ctx, renewed.AccessToken, "", "", "")
	if loginValidateErr != nil {
		t.Fatalf("Validate(login bearer) = %v, want nil", loginValidateErr)
	}
	if renewedValidateErr != nil {
		t.Errorf("Validate(renewed bearer) = %v, want nil (this is the second 401 the UI hits)", renewedValidateErr)
	}

	loginAC, loginAuthzErr := validator.Authorize(ctx, loginBearer, requiredPerm, "", "", "")
	renewedAC, renewedAuthzErr := validator.Authorize(ctx, renewed.AccessToken, requiredPerm, "", "", "")
	if loginAuthzErr != nil {
		t.Fatalf("Authorize(login bearer, %q) = %v, want nil", requiredPerm, loginAuthzErr)
	}
	if renewedAuthzErr != nil {
		t.Errorf("Authorize(renewed bearer, %q) = %v, want nil (this is the second 401 the UI hits)", requiredPerm, renewedAuthzErr)
	}

	t.Logf("login claims:   uid=%d roles=%v scp=%v aud=%v cnf=%v",
		loginClaims.UserID, loginClaims.Roles, loginClaims.SCP, loginClaims.Audience, loginClaims.Confirmation)
	t.Logf("renewed claims: uid=%d roles=%v scp=%v aud=%v cnf=%v",
		renewedClaims.UserID, renewedClaims.Roles, renewedClaims.SCP, renewedClaims.Audience, renewedClaims.Confirmation)
	if loginAC != nil {
		t.Logf("login AuthorizedContext:   scopes=%v tenant=%q", loginAC.Scopes, loginAC.TenantID)
	}
	if renewedAC != nil {
		t.Logf("renewed AuthorizedContext: scopes=%v tenant=%q", renewedAC.Scopes, renewedAC.TenantID)
	}
}
