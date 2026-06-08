//go:build iam

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
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"testing"
	"time"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// fakeRBACClient is a deterministic qrdb RBAC client for issuance tests: it
// returns a fixed permission set keyed by the identity's roles so a token's scp
// can be asserted against the subject's qrdb-resolved authority.
type fakeRBACClient struct {
	// permsByRole maps a role name to the colon "resource:action" permissions
	// that role grants.
	permsByRole map[string][]string
	// err, when set, is returned by ListPermissions to exercise error handling.
	err error
	// lastIdentity captures the identity passed to ListPermissions.
	lastIdentity *qrdbtransport.Identity
}

func (f *fakeRBACClient) Authorize(ctx context.Context, identity *qrdbtransport.Identity, resource, action string) error {
	return nil
}

func (f *fakeRBACClient) ListPermissions(ctx context.Context, identity *qrdbtransport.Identity) ([]qrdbtransport.PermissionInfo, error) {
	f.lastIdentity = identity
	if f.err != nil {
		return nil, f.err
	}
	var out []qrdbtransport.PermissionInfo
	for _, role := range identity.Roles {
		for _, name := range f.permsByRole[role] {
			out = append(out, qrdbtransport.PermissionInfo{Name: name})
		}
	}
	return out, nil
}

// fakePermissionResolver hands a fixed RBAC client (or nil) to the token
// service, mirroring the late-bound serverPermissionResolver seam.
type fakePermissionResolver struct {
	client qrdbtransport.RBACClient
}

func (r *fakePermissionResolver) RBACClient() qrdbtransport.RBACClient { return r.client }

func newSCPTestService(t *testing.T, resolver PermissionResolver) (*TokenService, *rsa.PrivateKey) {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}
	config := &OIDCConfig{
		Issuer:           "https://test.example.com",
		SigningAlgorithm: "RS256",
		SigningKey:       privateKey,
		AccessTokenTTL:   time.Hour,
		RefreshTokenTTL:  24 * time.Hour,
		IDTokenTTL:       time.Hour,
		AuthCodeTTL:      10 * time.Minute,
	}
	svc := NewTokenService(newMockTokenStorage(), privateKey, config)
	svc.permResolver = resolver
	return svc, privateKey
}

func baseSCPClaims(roles []string) *Claims {
	now := time.Now()
	return &Claims{
		Issuer:    "https://test.example.com",
		Subject:   "user-123",
		Audience:  []string{"https://test.example.com"},
		ExpiresAt: NewUnixTime(now.Add(time.Hour)),
		IssuedAt:  NewUnixTime(now),
		NotBefore: NewUnixTime(now),
		JTI:       generateJTI(),
		TenantID:  "tenant-1",
		Roles:     roles,
		Scope:     "openid profile email offline_access",
		ClientID:  "client-123",
	}
}

// scpFromToken parses the signed token and returns its scp claim as a []string.
func scpFromToken(t *testing.T, token string, key *rsa.PrivateKey) []string {
	t.Helper()
	claims, err := xkms.JWTVerifyWithKeyAndClaims(token, &key.PublicKey, "RS256")
	if err != nil {
		t.Fatalf("parse token: %v", err)
	}
	raw, ok := claims["scp"]
	if !ok {
		return nil
	}
	arr, ok := raw.([]interface{})
	if !ok {
		t.Fatalf("scp claim is %T, want array", raw)
	}
	out := make([]string, 0, len(arr))
	for _, v := range arr {
		out = append(out, v.(string))
	}
	return out
}

func hasScope(scopes []string, want string) bool {
	for _, s := range scopes {
		if s == want {
			return true
		}
	}
	return false
}

// TestGenerateAccessToken_SCPIsResolvedColonPerms is the Stage-2/3 contract: an
// admin token's scp carries the admin role's qrdb-resolved permissions in colon
// form, a limited user carries only its own perms, and the OIDC scope claim is
// untouched.
func TestGenerateAccessToken_SCPIsResolvedColonPerms(t *testing.T) {
	rbac := &fakeRBACClient{permsByRole: map[string][]string{
		"admin":  {"keys:get", "keys:create", "tenant_barrier:init", "users:read"},
		"reader": {"kv:get"},
	}}
	svc, key := newSCPTestService(t, &fakePermissionResolver{client: rbac})
	ctx := context.Background()

	t.Run("admin carries admin perms in colon grammar", func(t *testing.T) {
		token, err := svc.GenerateAccessToken(ctx, baseSCPClaims([]string{"admin"}))
		if err != nil {
			t.Fatalf("GenerateAccessToken: %v", err)
		}
		scp := scpFromToken(t, token, key)
		for _, want := range []string{"keys:get", "keys:create", "tenant_barrier:init", "users:read"} {
			if !hasScope(scp, want) {
				t.Errorf("admin scp missing %q; got %v", want, scp)
			}
		}
		// The OIDC scope set must NOT leak into scp.
		if hasScope(scp, "openid") || hasScope(scp, "profile") {
			t.Errorf("scp must not carry OIDC scopes; got %v", scp)
		}
		// The identity passed to qrdb carries subject/tenant/roles.
		if rbac.lastIdentity == nil || rbac.lastIdentity.Subject != "user-123" || rbac.lastIdentity.TenantID != "tenant-1" {
			t.Errorf("identity not threaded to RBAC: %+v", rbac.lastIdentity)
		}
	})

	t.Run("limited user carries only its perms", func(t *testing.T) {
		token, err := svc.GenerateAccessToken(ctx, baseSCPClaims([]string{"reader"}))
		if err != nil {
			t.Fatalf("GenerateAccessToken: %v", err)
		}
		scp := scpFromToken(t, token, key)
		if !hasScope(scp, "kv:get") {
			t.Errorf("reader scp missing kv:get; got %v", scp)
		}
		if hasScope(scp, "keys:create") || hasScope(scp, "tenant_barrier:init") {
			t.Errorf("reader must not carry admin perms; got %v", scp)
		}
	})

	t.Run("scope claim keeps OIDC scopes", func(t *testing.T) {
		token, err := svc.GenerateAccessToken(ctx, baseSCPClaims([]string{"admin"}))
		if err != nil {
			t.Fatalf("GenerateAccessToken: %v", err)
		}
		claims, err := xkms.JWTVerifyWithKeyAndClaims(token, &key.PublicKey, "RS256")
		if err != nil {
			t.Fatalf("parse token: %v", err)
		}
		if claims["scope"] != "openid profile email offline_access" {
			t.Errorf("scope claim = %v, want OIDC scopes unchanged", claims["scope"])
		}
	})
}

// TestGenerateAccessToken_SCPFallsBackToScope verifies behavior is unchanged
// when no resolver is wired or qrdb is unavailable: scp falls back to the OIDC
// scope (today's behavior), so non-server callers/tests keep working.
func TestGenerateAccessToken_SCPFallsBackToScope(t *testing.T) {
	ctx := context.Background()

	t.Run("nil resolver falls back to scope", func(t *testing.T) {
		svc, key := newSCPTestService(t, nil)
		token, err := svc.GenerateAccessToken(ctx, baseSCPClaims([]string{"admin"}))
		if err != nil {
			t.Fatalf("GenerateAccessToken: %v", err)
		}
		scp := scpFromToken(t, token, key)
		if !hasScope(scp, "openid") {
			t.Errorf("fallback scp should carry OIDC scopes; got %v", scp)
		}
	})

	t.Run("nil RBAC client falls back to scope", func(t *testing.T) {
		svc, key := newSCPTestService(t, &fakePermissionResolver{client: nil})
		token, err := svc.GenerateAccessToken(ctx, baseSCPClaims([]string{"admin"}))
		if err != nil {
			t.Fatalf("GenerateAccessToken: %v", err)
		}
		scp := scpFromToken(t, token, key)
		if !hasScope(scp, "profile") {
			t.Errorf("fallback scp should carry OIDC scopes; got %v", scp)
		}
	})

	t.Run("pre-populated SCP wins over resolver", func(t *testing.T) {
		rbac := &fakeRBACClient{permsByRole: map[string][]string{"admin": {"keys:get"}}}
		svc, key := newSCPTestService(t, &fakePermissionResolver{client: rbac})
		claims := baseSCPClaims([]string{"admin"})
		claims.SCP = []string{"explicit:perm"}
		token, err := svc.GenerateAccessToken(ctx, claims)
		if err != nil {
			t.Fatalf("GenerateAccessToken: %v", err)
		}
		scp := scpFromToken(t, token, key)
		if len(scp) != 1 || scp[0] != "explicit:perm" {
			t.Errorf("pre-populated SCP must win; got %v", scp)
		}
	})
}

// TestGenerateAccessToken_SCPDedupAndSkipEmpty verifies the resolver de-dupes
// repeated permission Names and skips empty Names so the scp set is a clean
// union.
func TestGenerateAccessToken_SCPDedupAndSkipEmpty(t *testing.T) {
	rbac := &fakeRBACClient{permsByRole: map[string][]string{
		// Two roles both grant kv:get (duplicate) and one empty Name to skip.
		"a": {"kv:get", ""},
		"b": {"kv:get", "kv:put"},
	}}
	svc, key := newSCPTestService(t, &fakePermissionResolver{client: rbac})
	token, err := svc.GenerateAccessToken(context.Background(), baseSCPClaims([]string{"a", "b"}))
	if err != nil {
		t.Fatalf("GenerateAccessToken: %v", err)
	}
	scp := scpFromToken(t, token, key)
	got := 0
	for _, s := range scp {
		if s == "kv:get" {
			got++
		}
		if s == "" {
			t.Error("empty permission name must be skipped")
		}
	}
	if got != 1 {
		t.Errorf("kv:get must appear once after dedup; got %d in %v", got, scp)
	}
	if !hasScope(scp, "kv:put") {
		t.Errorf("scp missing kv:put; got %v", scp)
	}
}

// TestGenerateAccessToken_SCPResolverError verifies a real RBAC error fails
// issuance (the token must not ship with an unresolved/unauthorized perm set).
func TestGenerateAccessToken_SCPResolverError(t *testing.T) {
	rbac := &fakeRBACClient{err: errors.New("qrdb unavailable")}
	svc, _ := newSCPTestService(t, &fakePermissionResolver{client: rbac})
	_, err := svc.GenerateAccessToken(context.Background(), baseSCPClaims([]string{"admin"}))
	if err == nil {
		t.Fatal("expected issuance to fail when RBAC resolution errors")
	}
}
