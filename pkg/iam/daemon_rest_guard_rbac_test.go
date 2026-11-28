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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jeremyhahn/go-qrdb/pkg/auth/rbac"
	"github.com/jeremyhahn/go-qrdb/pkg/permission"
	"github.com/jeremyhahn/go-qrdb/pkg/role"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/platformtoken"
)

// daemonGuardRoleServicer backs the real rbac.RBACAuthorizer: role ids
// resolve through perms, and no role is persisted under another id, so
// the SO deny-list recognises the SO by its name.
type daemonGuardRoleServicer struct {
	role.RoleServicer
	perms map[string][]permission.Permission
}

func (r *daemonGuardRoleServicer) ResolvePermissions(_ context.Context, roleIDs []string) ([]permission.Permission, error) {
	var out []permission.Permission
	for _, id := range roleIDs {
		out = append(out, r.perms[id]...)
	}
	return out, nil
}

func (r *daemonGuardRoleServicer) GetRoleByName(_ context.Context, _, name string) (*role.Role, error) {
	return nil, &role.RoleNotFoundError{ID: name}
}

// TestDaemonRESTGuard_RealRBACKeepsTheSODenyList drives a node-issued token
// through the guard with go-qrdb's real RBAC authorizer under an ABAC allow.
// The decorated authorizer alone admits an SO on that allow, because a
// matched ABAC allow never reaches the inner RBAC; the guard also asks the
// undecorated RBAC with the token's roles, so the SO deny-list for
// cryptographic operations still refuses, while a non-SO signer is admitted.
func TestDaemonRESTGuard_RealRBACKeepsTheSODenyList(t *testing.T) {
	const issuer = "https://broker.example.com"
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("issuer key: %v", err)
	}
	rbacClient := &fakeRBACClient{permsByRole: map[string][]string{"so": {"keys:sign"}, "signer": {"keys:sign"}}}
	svc := newKIDTokenService(t, priv, &OIDCConfig{
		Issuer:           issuer,
		SigningAlgorithm: "ES256",
		AccessTokenTTL:   time.Hour,
		RefreshTokenTTL:  24 * time.Hour,
		IDTokenTTL:       time.Hour,
		AuthCodeTTL:      10 * time.Minute,
	}, &fakePermissionResolver{client: rbacClient})

	realRBAC := rbac.New(&daemonGuardRoleServicer{perms: map[string][]permission.Permission{
		"so": {"keys:sign"}, "signer": {"keys:sign"},
	}})
	policy := &qrdbsdk.ABACPolicy{
		APIVersion: "abac.qrdb.io/v1",
		Kind:       "Policy",
		Metadata:   qrdbsdk.ABACPolicyMetadata{Name: "signers", Tenant: "system"},
		Spec: qrdbsdk.ABACPolicySpec{
			Subjects: qrdbsdk.ABACSubjectSelector{Roles: []string{"so", "signer"}},
			Rules: []qrdbsdk.ABACRule{{
				Name: "sign", Effect: qrdbsdk.ABACEffectAllow,
				Actions: []string{"sign"}, Resources: []string{"keys"},
			}},
		},
	}
	decorated, err := qrdbsdk.DecorateAuthorizer(realRBAC, qrdbsdk.ABACWithPolicies([]*qrdbsdk.ABACPolicy{policy}))
	if err != nil {
		t.Fatalf("DecorateAuthorizer: %v", err)
	}

	soIdentity := &qrdbsdk.Identity{
		Subject: "42", TenantID: "system", Roles: []string{"so"}, ScopePermissions: []string{"keys:sign"},
	}
	systemCtx := qrdbsdk.ContextWithTenant(context.Background(), "system")
	if err := decorated.Authorize(systemCtx, soIdentity, "keys", "sign"); err != nil {
		t.Fatalf("control: the decorated authorizer alone must admit the SO on its ABAC allow: %v", err)
	}
	if err := realRBAC.Authorize(systemCtx, soIdentity, "keys", "sign"); err == nil {
		t.Fatal("control: the real RBAC must refuse the SO a cryptographic permission")
	}

	validator, err := platformtoken.NewValidator(platformtoken.Options{
		Issuer:       issuer,
		Audience:     issuer,
		JWKS:         svc.keyManager.JWKS(),
		ModeResolver: func(string) platformtoken.AuthzMode { return platformtoken.AuthzModeOffline },
		Authorizer:   decorated,
		RBAC:         realRBAC,
	})
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}
	echo := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ac, ok := platformtoken.FromContext(r.Context())
		if !ok {
			t.Error("the guard passed a request without binding its principal")
			return
		}
		_, _ = w.Write([]byte(ac.TenantID + "/" + ac.Subject))
	})

	hardware := func(id uint64, roles []string) string {
		t.Helper()
		set, err := svc.IssueHardwareTokens(context.Background(), HardwareTokenOptions{
			User:        &User{ID: id, TenantID: "system", Email: "k@example.com", Name: "K", Roles: roles},
			AMR:         []string{"webauthn", "hwk"},
			Attestation: testHardwareAttestation("cb69481e"),
		})
		if err != nil {
			t.Fatalf("IssueHardwareTokens(%v): %v", roles, err)
		}
		return set.AccessToken
	}

	for _, tc := range []struct {
		name       string
		token      string
		wantStatus int
		wantBody   string
	}{
		{name: "an SO token with a crypto scope and an ABAC allow is refused",
			token: hardware(42, []string{"so"}), wantStatus: http.StatusForbidden},
		{name: "a signer token with the same allow is admitted",
			token: hardware(43, []string{"signer"}), wantStatus: http.StatusOK, wantBody: "system/43"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "https://daemon.example/v1/keys", nil)
			req.Header.Set("Authorization", "Bearer "+tc.token)
			w := httptest.NewRecorder()
			validator.Middleware("keys:sign")(echo).ServeHTTP(w, req)
			if w.Code != tc.wantStatus {
				t.Fatalf("status %d, want %d (body %q)", w.Code, tc.wantStatus, w.Body.String())
			}
			if tc.wantBody != "" && w.Body.String() != tc.wantBody {
				t.Fatalf("principal %q, want %q", w.Body.String(), tc.wantBody)
			}
			if tc.wantStatus != http.StatusOK && !strings.Contains(w.Body.String(), "access_denied") {
				t.Fatalf("refusal body %q, want access_denied (not insufficient_scope)", w.Body.String())
			}
		})
	}
}
