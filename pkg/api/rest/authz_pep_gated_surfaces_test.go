//go:build rest

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

package rest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/platformtoken"
)

// This file exercises the surfaces that were mounted but absent from pepMapping
// and therefore answered 403 "Authorization denied" to EVERY request under the
// production posture (SecurityConfig.RequireAuthz defaults to true, so
// AuthorizeViaValidator is installed and default-denies unmapped paths).
//
// Each surface gets the same three assertions end-to-end through the real gate:
//
//	no token       -> 401 (the gate demands authentication first);
//	wrong scope    -> 403 AND the gate demanded the expected qrdb permission;
//	correct scope  -> the request passes the gate and reaches the handler.
//
// The middle case is the one that regresses silently: a coarse or wrong mapping
// still returns 403, so the test pins the exact permission string the gate
// forwards to the validator rather than only the status code.

// scopedValidatorAuthorizer is a ValidatorAuthorizer double that performs the
// real scope decision: it admits the request when the required permission is in
// granted and otherwise fails with the same *platformtoken.ScopeDeniedError the
// live Validator returns (which the gate maps to 403).
type scopedValidatorAuthorizer struct {
	granted  map[string]bool
	lastPerm string
}

func (a *scopedValidatorAuthorizer) Authorize(_ context.Context, _, requiredPerm, _, _, _ string) (*platformtoken.AuthorizedContext, error) {
	a.lastPerm = requiredPerm
	if !a.granted[requiredPerm] {
		return nil, &platformtoken.ScopeDeniedError{Required: requiredPerm}
	}
	return &platformtoken.AuthorizedContext{
		Subject:  "alice",
		Username: "alice",
		TenantID: "system",
		Scopes:   []string{requiredPerm},
		Roles:    []string{"admin"},
	}, nil
}

// gatedSurfaceCase is one (method, path) endpoint plus the qrdb permission the
// PEP must demand for it.
type gatedSurfaceCase struct {
	name   string
	method string
	path   string
	want   string
}

// assertGatedSurface runs the 401 / 403 / allow triple for one endpoint.
func assertGatedSurface(t *testing.T, tc gatedSurfaceCase) {
	t.Helper()

	t.Run(tc.name+" unauthenticated is 401", func(t *testing.T) {
		authorizer := &scopedValidatorAuthorizer{granted: map[string]bool{}}
		mw := newValidatorMiddleware(authorizer)
		handler := &testHandler{}

		req := httptest.NewRequest(tc.method, tc.path, nil)
		rr := httptest.NewRecorder()
		mw.AuthorizeViaValidator(handler).ServeHTTP(rr, req)

		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rr.Code)
		}
		if handler.called {
			t.Error("handler must not run without a token")
		}
	})

	t.Run(tc.name+" wrong scope is 403 naming the mapped permission", func(t *testing.T) {
		authorizer := &scopedValidatorAuthorizer{granted: map[string]bool{"kv:get": true}}
		mw := newValidatorMiddleware(authorizer)
		handler := &testHandler{}

		req := httptest.NewRequest(tc.method, tc.path, nil)
		req.Header.Set("Authorization", "Bearer tok")
		rr := httptest.NewRecorder()
		mw.AuthorizeViaValidator(handler).ServeHTTP(rr, req)

		if rr.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", rr.Code)
		}
		if handler.called {
			t.Error("handler must not run without the required scope")
		}
		// The distinguishing assertion: the gate REACHED the validator with the
		// mapped permission. An unmapped path also yields 403, but never
		// consults the validator at all.
		if authorizer.lastPerm != tc.want {
			t.Fatalf("required permission = %q, want %q (an empty value means the "+
				"path is unmapped and hard-403, not scope-checked)",
				authorizer.lastPerm, tc.want)
		}
	})

	t.Run(tc.name+" correct scope passes the gate", func(t *testing.T) {
		authorizer := &scopedValidatorAuthorizer{granted: map[string]bool{tc.want: true}}
		mw := newValidatorMiddleware(authorizer)
		handler := &testHandler{}

		req := httptest.NewRequest(tc.method, tc.path, nil)
		req.Header.Set("Authorization", "Bearer tok")
		rr := httptest.NewRecorder()
		mw.AuthorizeViaValidator(handler).ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rr.Code)
		}
		if !handler.called {
			t.Fatal("handler must run once the required scope is granted")
		}
		if handler.userID != "alice" {
			t.Errorf("injected principal = %q, want alice", handler.userID)
		}
	})
}

// TestGate_TrustSurface covers the trust composition routes (handlers_trust.go).
func TestGate_TrustSurface(t *testing.T) {
	for _, tc := range []gatedSurfaceCase{
		{"cross-signed roots", http.MethodGet, "/api/v1/trust/cross-signed-roots", "trust:read"},
		{"auto-import AIA", http.MethodPost, "/api/v1/trust/auto-import-aia", "trust:write"},
		{"publish DANE", http.MethodPost, "/api/v1/trust/publish-dane", "trust:write"},
	} {
		assertGatedSurface(t, tc)
	}
}

// TestGate_ACMEControlPlane covers the QuicBaaS ACME control verbs
// (handlers_acme.go registerACMERoutes). The raw RFC 8555 mount at the router
// root is a different surface and carries no platform-token gate.
func TestGate_ACMEControlPlane(t *testing.T) {
	for _, tc := range []gatedSurfaceCase{
		{"register challenge", http.MethodPost, "/api/v1/acme/register-challenge", "acme:write"},
		{"issue", http.MethodPost, "/api/v1/acme/issue", "acme:write"},
		{"new order", http.MethodPost, "/api/v1/acme/new-order", "acme:write"},
		{"finalize", http.MethodPost, "/api/v1/acme/finalize", "acme:write"},
		{"renew", http.MethodPost, "/api/v1/acme/renew", "acme:write"},
	} {
		assertGatedSurface(t, tc)
	}
}

// TestGate_XKMSCompatSurface covers the go-xkms compatibility routes
// (routes_xkms.go) across every permission family the surface touches.
func TestGate_XKMSCompatSurface(t *testing.T) {
	for _, tc := range []gatedSurfaceCase{
		{"list backends", http.MethodGet, "/api/v1/xkms/backends", "backends:list"},
		{"get backend", http.MethodGet, "/api/v1/xkms/backends/pkcs11", "backends:read"},
		{"generate key", http.MethodPost, "/api/v1/xkms/keys", "keys:create"},
		{"list keys", http.MethodGet, "/api/v1/xkms/keys", "keys:list"},
		{"get key", http.MethodGet, "/api/v1/xkms/keys/k1", "keys:get"},
		{"delete key", http.MethodDelete, "/api/v1/xkms/keys/k1", "keys:delete"},
		{"rotate key", http.MethodPost, "/api/v1/xkms/keys/k1/rotate", "keys:rotate"},
		{"sign", http.MethodPost, "/api/v1/xkms/keys/k1/sign", "keys:sign"},
		{"verify", http.MethodPost, "/api/v1/xkms/keys/k1/verify", "keys:verify"},
		{"encrypt", http.MethodPost, "/api/v1/xkms/keys/k1/encrypt", "keys:encrypt"},
		{"decrypt", http.MethodPost, "/api/v1/xkms/keys/k1/decrypt", "keys:decrypt"},
		{"list key versions", http.MethodGet, "/api/v1/xkms/keys/k1/versions", "keys:list"},
		{"enable key version", http.MethodPost, "/api/v1/xkms/keys/k1/versions/2/enable", "keys:rotate"},
		{"disable all key versions", http.MethodPost, "/api/v1/xkms/keys/k1/versions/disable-all", "keys:rotate"},
		{"save cert", http.MethodPost, "/api/v1/xkms/certs", "certs:generate"},
		{"list certs", http.MethodGet, "/api/v1/xkms/certs", "certs:list"},
		{"get cert", http.MethodGet, "/api/v1/xkms/certs/c1", "certs:read"},
		{"delete cert", http.MethodDelete, "/api/v1/xkms/certs/c1", "certs:revoke"},
		{"envelope seal", http.MethodPost, "/api/v1/xkms/seal", "keys:encrypt"},
		{"envelope unseal", http.MethodPost, "/api/v1/xkms/unseal", "keys:decrypt"},
		{"seal capability", http.MethodGet, "/api/v1/xkms/seal/capability", "keys:list"},
		{"list PIV slots", http.MethodGet, "/api/v1/xkms/piv/slots", "piv:read"},
		{"read PIV cert", http.MethodGet, "/api/v1/xkms/piv/slots/9a/cert", "piv:read"},
		{"store PIV cert", http.MethodPut, "/api/v1/xkms/piv/slots/9a/cert", "piv:write"},
		{"delete PIV cert", http.MethodDelete, "/api/v1/xkms/piv/slots/9a/cert", "piv:delete"},
		{"generate PIV key", http.MethodPost, "/api/v1/xkms/piv/slots/9a/key", "piv:write"},
		{"generate PIV CSR", http.MethodPost, "/api/v1/xkms/piv/slots/9a/csr", "piv:use"},
		{"CA bundle", http.MethodGet, "/api/v1/xkms/ca/bundle", "ca:read"},
		{"CA cert", http.MethodGet, "/api/v1/xkms/ca/cert", "ca:read"},
		{"CA sign CSR", http.MethodPost, "/api/v1/xkms/ca/sign", "certs:generate"},
		{"CA issue", http.MethodPost, "/api/v1/xkms/ca/issue", "certs:generate"},
		{"CA revoke", http.MethodPost, "/api/v1/xkms/ca/revoke", "certs:revoke"},
		{"CA CRL", http.MethodPost, "/api/v1/xkms/ca/crl", "certs:revoke"},
		{"CA revocation check", http.MethodGet, "/api/v1/xkms/ca/revoked/0a0b", "certs:read"},
		{"set SO PIN", http.MethodPost, "/api/v1/xkms/pin/so/set", "pin:write"},
		{"change user PIN", http.MethodPost, "/api/v1/xkms/pin/user/change", "pin:write"},
		{"verify SO PIN", http.MethodPost, "/api/v1/xkms/pin/so/verify", "pin:read"},
		{"verify user PIN", http.MethodPost, "/api/v1/xkms/pin/user/verify", "pin:read"},
		{"lockout status", http.MethodGet, "/api/v1/xkms/pin/lockout", "pin:read"},
		{"lockout reset", http.MethodPost, "/api/v1/xkms/pin/lockout/reset", "pin:write"},
		{"health", http.MethodGet, "/api/v1/xkms/health", "admin:health"},
	} {
		assertGatedSurface(t, tc)
	}
}

// TestGate_XKMSSSHAliasUnchanged pins that adding the /api/v1/xkms rules did not
// disturb the longer /api/v1/xkms/v1/ssh alias, which must keep resolving to the
// SSH surface's own mappings.
func TestGate_XKMSSSHAliasUnchanged(t *testing.T) {
	cases := []gatedSurfaceCase{
		{"ssh generate key", http.MethodPost, "/api/v1/xkms/v1/ssh/keys", "keys:create"},
		{"ssh import key", http.MethodPost, "/api/v1/xkms/v1/ssh/keys/import", "keys:import"},
		{"ssh list keys", http.MethodGet, "/api/v1/xkms/v1/ssh/keys", "keys:list"},
		{"ssh get key", http.MethodGet, "/api/v1/xkms/v1/ssh/keys/user/alice/id", "keys:get"},
		{"ssh sign", http.MethodPost, "/api/v1/xkms/v1/ssh/keys/user/alice/id/sign", "keys:sign"},
		{"ssh add known host", http.MethodPost, "/api/v1/xkms/v1/ssh/known-hosts", "keys:create"},
		{"ssh emit authorized", http.MethodPost, "/api/v1/xkms/v1/ssh/authorized", "keys:get"},
	}
	for _, tc := range cases {
		resource, action, ok := mapRequestToPermission(tc.path, tc.method)
		if !ok {
			t.Fatalf("mapRequestToPermission(%q, %q) ok = false", tc.path, tc.method)
		}
		if got := resource + ":" + action; got != tc.want {
			t.Errorf("%s %s = %q, want %q", tc.method, tc.path, got, tc.want)
		}
	}
}

// TestGate_SecretSideSurfaces covers the three secret side-mounts. The versions
// endpoint is the GUI-visible instance of this bug: SecretsView "view versions"
// issues GET /api/v1/secrets-versions/{path}, which was hard-403 because
// "/api/v1/secrets" does not prefix-match "/api/v1/secrets-versions".
func TestGate_SecretSideSurfaces(t *testing.T) {
	for _, tc := range []gatedSurfaceCase{
		{"list secret versions", http.MethodGet, "/api/v1/secrets-versions/prod/db", "credentials:list_versions"},
		{"get secrets by prefix", http.MethodGet, "/api/v1/secrets-prefix/prod", "credentials:retrieve"},
		{"watch secrets", http.MethodGet, "/api/v1/secrets-watch/prod", "credentials:retrieve"},
	} {
		assertGatedSurface(t, tc)
	}
}

// TestGate_SecretsPrefixDoesNotSwallowSiblings pins the prefix-matching hazard
// that caused the bug: the sibling mounts must NOT resolve through the
// "/api/v1/secrets" entry, and "/api/v1/secrets" must keep its own mapping.
func TestGate_SecretsPrefixDoesNotSwallowSiblings(t *testing.T) {
	if _, action, _ := mapRequestToPermission("/api/v1/secrets/prod/db", http.MethodGet); action != "retrieve" {
		t.Errorf("/api/v1/secrets item GET action = %q, want retrieve", action)
	}
	if _, action, _ := mapRequestToPermission("/api/v1/secrets-versions/prod/db", http.MethodGet); action != "list_versions" {
		t.Errorf("/api/v1/secrets-versions GET action = %q, want list_versions", action)
	}
}

// TestGate_LegacySealSurface covers the deprecated /api/v1/seal routes. They are
// still the REST projection of the Security().Seal() SDK service in the Go,
// TypeScript and JavaScript SDKs, so they must authorize with the same granular
// scopes as /api/v1/barrier rather than be default-denied (which also made
// their RFC 8594 Deprecation/Sunset headers unreachable).
func TestGate_LegacySealSurface(t *testing.T) {
	for _, tc := range []gatedSurfaceCase{
		{"status", http.MethodGet, "/api/v1/seal/status", "tenant_barrier:status"},
		{"init", http.MethodPost, "/api/v1/seal/init", "tenant_barrier:init"},
		{"unseal", http.MethodPost, "/api/v1/seal/unseal", "tenant_barrier:unseal"},
		{"seal", http.MethodPost, "/api/v1/seal/seal", "tenant_barrier:seal"},
		{"rekey", http.MethodPost, "/api/v1/seal/rekey", "seal:rekey"},
		{"generate recovery key", http.MethodPost, "/api/v1/seal/recovery/generate", "seal:generate_recovery"},
		{"delete recovery key", http.MethodDelete, "/api/v1/seal/recovery", "seal:generate_recovery"},
		{"generate root token", http.MethodPost, "/api/v1/seal/root/generate", "seal:bootstrap"},
		{"rotate root key", http.MethodPost, "/api/v1/seal/rotate-root", "seal:rekey"},
	} {
		assertGatedSurface(t, tc)
	}
}

// TestGate_BatchAndQuerySurfaces covers the atomic batch plane and the
// secondary-index query plane, both of which mirror the gRPC canon.
func TestGate_BatchAndQuerySurfaces(t *testing.T) {
	for _, tc := range []gatedSurfaceCase{
		{"batch put", http.MethodPost, "/api/v1/batch/put", "batch:execute"},
		{"batch get", http.MethodPost, "/api/v1/batch/get", "batch:execute"},
		{"batch delete", http.MethodPost, "/api/v1/batch/delete", "batch:execute"},
		{"index query", http.MethodGet, "/api/v1/query/users/email", "kv:range"},
		{"index range scan", http.MethodPost, "/api/v1/query/range", "index:scan"},
	} {
		assertGatedSurface(t, tc)
	}
}

// TestGate_HookSurface covers the Claude Code hook callbacks, which the route
// guard discovered were also unmapped.
func TestGate_HookSurface(t *testing.T) {
	for _, tc := range []gatedSurfaceCase{
		{"task created", http.MethodPost, "/api/v1/hooks/task-created", "orchestrator:hook"},
		{"task completed", http.MethodPost, "/api/v1/hooks/task-completed", "orchestrator:hook"},
		{"teammate idle", http.MethodPost, "/api/v1/hooks/teammate-idle", "orchestrator:hook"},
	} {
		assertGatedSurface(t, tc)
	}
}

// TestGate_UnknownMethodOnMappedSurfaceStaysDenied asserts the PEP remains a
// closed set: an HTTP method with no canonical qrdb action is unmapped even on a
// mapped prefix, so the gate denies without consulting the validator.
func TestGate_UnknownMethodOnMappedSurfaceStaysDenied(t *testing.T) {
	authorizer := &scopedValidatorAuthorizer{granted: map[string]bool{"trust:read": true, "trust:write": true}}
	mw := newValidatorMiddleware(authorizer)
	handler := &testHandler{}

	req := httptest.NewRequest(http.MethodTrace, "/api/v1/trust/cross-signed-roots", nil)
	req.Header.Set("Authorization", "Bearer tok")
	rr := httptest.NewRecorder()
	mw.AuthorizeViaValidator(handler).ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rr.Code)
	}
	if authorizer.lastPerm != "" {
		t.Errorf("validator consulted with %q; an unmapped method must never reach it", authorizer.lastPerm)
	}
}
