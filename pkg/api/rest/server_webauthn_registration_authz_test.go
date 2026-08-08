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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/platformtoken"
)

// =============================================================================
// Router-level authorization contract for the CANONICAL xkms-native WebAuthn
// mount (/api/v1/iam/{tenant_id}/webauthn, iam_xkms_webauthn_mount.go).
//
// The mount is a ROOT chi.Route, so it does not inherit the /api/v1 group's
// authorization chain. It previously registered without installing that chain
// itself, which left the whole group — INCLUDING registration/begin|finish,
// which bind a new authenticator to a request-supplied user id — reachable
// unauthenticated under validator (RequireAuthz=true) enforcement: an
// account-takeover primitive. These tests drive the REAL router; a predicate
// unit test cannot catch a routing/middleware-layering hole.
//
// Contract:
//
//	registration/{begin,finish,status}, no token -> 401 (both postures)
//	registration/begin, token w/o scope          -> 403
//	registration/*, token                        -> user_authfactors:{add,get}
//	login/{begin,finish}, no token               -> admitted (pre-auth pin)
// =============================================================================

// xkmsRegistrationRoutes are the enrollment routes on the canonical mount that
// must require a bearer token, with the qrdb permission each authorizes as.
var xkmsRegistrationRoutes = []struct {
	method string
	path   string
	perm   string
}{
	{http.MethodPost, "/api/v1/iam/system/webauthn/registration/begin", "user_authfactors:add"},
	{http.MethodPost, "/api/v1/iam/system/webauthn/registration/finish", "user_authfactors:add"},
	{http.MethodGet, "/api/v1/iam/system/webauthn/registration/status", "user_authfactors:get"},
}

// xkmsLoginRoutes are the login-ceremony routes on the same mount, which must
// stay reachable without a token (WebAuthn IS the authentication mechanism).
var xkmsLoginRoutes = []string{
	"/api/v1/iam/system/webauthn/login/begin",
	"/api/v1/iam/system/webauthn/login/finish",
}

// unavailableWebAuthnSettings makes the per-tenant xkms service construction
// fail deterministically, so an ADMITTED request lands on the mount's own 503
// ("tenant webauthn service unavailable") instead of touching a live qrdb
// client. That is exactly what these tests need: a routed, mounted endpoint
// whose response can never be confused with a 401/403/404 from the gate.
type unavailableWebAuthnSettings struct{}

func (unavailableWebAuthnSettings) WebAuthnSettingsFor(_ context.Context, tenantID string) (*iam.TenantWebAuthnConfig, error) {
	return nil, &iam.WebAuthnSettingsReadError{TenantID: tenantID}
}

// xkmsWebAuthnService is a service double whose GetWebAuthnManager returns the
// multi-tenant xkms manager, which is what makes MountXKMSWebAuthnRoutes
// actually register its routes (it is a silent no-op otherwise).
type xkmsWebAuthnService struct {
	*testutil.MockQuicBaaS
	mgr WebAuthnManager
}

func (s *xkmsWebAuthnService) GetWebAuthnManager(_ context.Context) (WebAuthnManager, error) {
	return s.mgr, nil
}

// newXKMSWebAuthnServer builds a real REST server whose service resolves the
// xkms-backed WebAuthn manager, so the canonical mount is registered by every
// setupRouter call the enforcement setters trigger.
func newXKMSWebAuthnServer(t *testing.T) *Server {
	t.Helper()

	factory, err := iam.NewWebAuthnServiceFactory(iam.WebAuthnFactoryParams{
		Config: iam.WebAuthnFactoryConfig{
			RPID:          "example.com",
			RPDisplayName: "Example",
			RPOrigins:     []string{"https://example.com"},
		},
		QRDB:     dummyQRDBClient(),
		Settings: unavailableWebAuthnSettings{},
	})
	require.NoError(t, err)

	mgr, err := iam.NewXKMSWebAuthnManager(factory)
	require.NoError(t, err)

	s, mockSvc := newTestServer(t)
	s.service = &xkmsWebAuthnService{MockQuicBaaS: mockSvc, mgr: mgr}
	return s
}

// requireXKMSMountRegistered fails the test unless the canonical mount is
// actually serving, so a 401 can never come from an unmounted route.
func requireXKMSMountRegistered(t *testing.T, s *Server) {
	t.Helper()
	rr := serveWebAuthn(s, http.MethodPost, xkmsLoginRoutes[0], "")
	require.Equal(t, http.StatusServiceUnavailable, rr.Code,
		"canonical xkms webauthn mount must be registered for this test to mean anything")
}

// --- Validator enforcement (RequireAuthz=true, the daemon posture) -----------

// TestRouter_XKMSWebAuthnRegistration_RequiresTokenUnderValidatorEnforcement is
// the account-takeover regression guard: enrollment on the canonical mount must
// be rejected before any handler runs when no bearer is presented.
func TestRouter_XKMSWebAuthnRegistration_RequiresTokenUnderValidatorEnforcement(t *testing.T) {
	for _, route := range xkmsRegistrationRoutes {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			authorizer := &recordingValidatorAuthorizer{
				ret: &platformtoken.AuthorizedContext{Subject: "unused"},
			}
			s := newXKMSWebAuthnServer(t)
			s.SetValidatorAuthzEnforcement(authorizer)
			requireXKMSMountRegistered(t, s)

			rr := serveWebAuthn(s, route.method, route.path, "")

			assert.Equal(t, http.StatusUnauthorized, rr.Code,
				"%s must require a bearer token", route.path)
		})
	}
}

// TestRouter_XKMSWebAuthnRegistration_AuthorizesAsFactorEnrollment proves the
// gate is not merely 401-on-missing-header: with a token the request flows
// through the validator carrying the factor-enrollment permission, the same
// user_authfactors:* vocabulary the /v1/users/factors wire mount uses.
func TestRouter_XKMSWebAuthnRegistration_AuthorizesAsFactorEnrollment(t *testing.T) {
	for _, route := range xkmsRegistrationRoutes {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			authorizer := &recordingValidatorAuthorizer{
				ret: &platformtoken.AuthorizedContext{Subject: "admin", TenantID: "system"},
			}
			s := newXKMSWebAuthnServer(t)
			s.SetValidatorAuthzEnforcement(authorizer)

			rr := serveWebAuthn(s, route.method, route.path, "tok")

			require.True(t, authorizer.called, "validator must gate %s", route.path)
			assert.Equal(t, route.perm, authorizer.gotPerm)
			// Admitted: the request reached the mount's dispatcher, whose
			// per-tenant service construction fails closed with 503.
			assert.Equal(t, http.StatusServiceUnavailable, rr.Code)
		})
	}
}

// TestRouter_XKMSWebAuthnRegistration_ScopeDeniedForbidden pins the deny half:
// a token without the enrollment scope is 403, never silently admitted.
func TestRouter_XKMSWebAuthnRegistration_ScopeDeniedForbidden(t *testing.T) {
	authorizer := &recordingValidatorAuthorizer{
		retErr: &platformtoken.ScopeDeniedError{Required: "user_authfactors:add"},
	}
	s := newXKMSWebAuthnServer(t)
	s.SetValidatorAuthzEnforcement(authorizer)

	rr := serveWebAuthn(s, http.MethodPost,
		"/api/v1/iam/system/webauthn/registration/begin", "tok")

	assert.Equal(t, http.StatusForbidden, rr.Code)
}

// TestRouter_XKMSWebAuthnLogin_StaysPreAuthOnCanonicalMount is the login
// regression pin: gating the mount must NOT gate the login ceremony, which a
// browser calls before it holds any token. It now runs against the REGISTERED
// mount (the earlier login test could only observe the 404 fall-through).
func TestRouter_XKMSWebAuthnLogin_StaysPreAuthOnCanonicalMount(t *testing.T) {
	for _, path := range xkmsLoginRoutes {
		t.Run(path, func(t *testing.T) {
			authorizer := &recordingValidatorAuthorizer{
				ret: &platformtoken.AuthorizedContext{Subject: "unused"},
			}
			s := newXKMSWebAuthnServer(t)
			s.SetValidatorAuthzEnforcement(authorizer)

			rr := serveWebAuthn(s, http.MethodPost, path, "")

			assertReachedHandler(t, rr, path)
			assert.False(t, authorizer.called,
				"a pre-auth login route must never reach the validator")
		})
	}
}

// --- Default-deny enforcement -------------------------------------------------

// TestRouter_XKMSWebAuthnRegistration_RequiresTokenUnderDefaultDeny pins the
// same contract under the top-router default-deny gate.
func TestRouter_XKMSWebAuthnRegistration_RequiresTokenUnderDefaultDeny(t *testing.T) {
	for _, route := range xkmsRegistrationRoutes {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			s := newXKMSWebAuthnServer(t)
			s.SetDefaultDenyEnforcement(&mockDPoPValidator{err: &InvalidTokenResultError{}})
			requireXKMSMountRegistered(t, s)

			rr := serveWebAuthn(s, route.method, route.path, "")

			assert.Equal(t, http.StatusUnauthorized, rr.Code,
				"%s must require a bearer token", route.path)
		})
	}
}

// TestRouter_XKMSWebAuthnLogin_PreAuthUnderDefaultDenyOnCanonicalMount pins the
// login carve-out under the default-deny gate against the registered mount.
func TestRouter_XKMSWebAuthnLogin_PreAuthUnderDefaultDenyOnCanonicalMount(t *testing.T) {
	for _, path := range xkmsLoginRoutes {
		t.Run(path, func(t *testing.T) {
			s := newXKMSWebAuthnServer(t)
			s.SetDefaultDenyEnforcement(&mockDPoPValidator{err: &InvalidTokenResultError{}})

			rr := serveWebAuthn(s, http.MethodPost, path, "")

			assertReachedHandler(t, rr, path)
		})
	}
}

// --- Delegated admin surface (/api/v1/admin) ----------------------------------

// adminSurfaceRoutes are representative endpoints of the root-mounted
// delegated-admin group, which had the SAME routing hole: creating an admin
// assignment and starting an impersonation session were reachable
// unauthenticated under validator enforcement.
var adminSurfaceRoutes = []struct {
	method string
	path   string
	perm   string
}{
	{http.MethodPost, "/api/v1/admin/assignments", "roles:assign"},
	{http.MethodGet, "/api/v1/admin/assignments", "roles:list"},
	{http.MethodGet, "/api/v1/admin/assignments/a1", "roles:read"},
	{http.MethodPut, "/api/v1/admin/assignments/a1", "roles:assign"},
	{http.MethodDelete, "/api/v1/admin/assignments/a1", "roles:unassign"},
	{http.MethodPost, "/api/v1/admin/check-permission", "permissions:resolve"},
	{http.MethodPost, "/api/v1/admin/impersonation/start", "users:update"},
	{http.MethodPost, "/api/v1/admin/impersonation/s1/end", "users:update"},
	{http.MethodGet, "/api/v1/admin/impersonation", "users:read"},
	{http.MethodGet, "/api/v1/admin/users/u1/assignments", "roles:list"},
	{http.MethodGet, "/api/v1/admin/users/u1/impersonations", "users:read"},
}

// TestRouter_AdminSurface_RequiresTokenUnderValidatorEnforcement pins that the
// delegated-admin group is no longer reachable without a bearer token.
func TestRouter_AdminSurface_RequiresTokenUnderValidatorEnforcement(t *testing.T) {
	for _, route := range adminSurfaceRoutes {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			authorizer := &recordingValidatorAuthorizer{
				ret: &platformtoken.AuthorizedContext{Subject: "unused"},
			}
			s, _ := newTestServer(t)
			s.SetValidatorAuthzEnforcement(authorizer)

			rr := serveWebAuthn(s, route.method, route.path, "")

			assert.Equal(t, http.StatusUnauthorized, rr.Code,
				"%s must require a bearer token", route.path)
			assert.False(t, authorizer.called)
		})
	}
}

// TestRouter_AdminSurface_AuthorizesWithRegisteredPermission proves every
// delegated-admin endpoint resolves to a registered qrdb permission rather than
// being default-denied as unmapped once the gate is installed.
func TestRouter_AdminSurface_AuthorizesWithRegisteredPermission(t *testing.T) {
	for _, route := range adminSurfaceRoutes {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			authorizer := &recordingValidatorAuthorizer{
				retErr: &platformtoken.ScopeDeniedError{Required: route.perm},
			}
			s, _ := newTestServer(t)
			s.SetValidatorAuthzEnforcement(authorizer)

			rr := serveWebAuthn(s, route.method, route.path, "tok")

			require.True(t, authorizer.called, "validator must gate %s", route.path)
			assert.Equal(t, route.perm, authorizer.gotPerm)
			assert.Equal(t, http.StatusForbidden, rr.Code,
				"a scope denial must be a 403, not an unmapped default-deny")
		})
	}
}

// --- Health probes ------------------------------------------------------------

// healthProbePaths are the REAL mounted probe routes (server.go). The legacy
// publicEndpoints list named /health/liveness + /health/readiness, which never
// matched anything.
var healthProbePaths = []string{
	"/api/v1/health",
	"/api/v1/health/live",
	"/api/v1/health/ready",
}

// expectHealthProbes wires the service doubles the probe handlers call so a
// reachable probe answers 200 rather than a mock panic.
func expectHealthProbes(mockSvc *testutil.MockQuicBaaS) {
	mockSvc.On("Health", mock.Anything, mock.Anything).
		Return(&service.HealthResponse{Status: service.HealthStatusHealthy}, nil)
	mockSvc.On("Liveness", mock.Anything, mock.Anything).
		Return(&service.LivenessResponse{Alive: true}, nil)
	mockSvc.On("Readiness", mock.Anything, mock.Anything).
		Return(&service.ReadinessResponse{Ready: true}, nil)
}

// TestRouter_HealthProbes_PublicAtRealPaths proves the probes are reachable
// without a token at the paths that are actually mounted, under BOTH postures.
func TestRouter_HealthProbes_PublicAtRealPaths(t *testing.T) {
	for _, path := range healthProbePaths {
		t.Run("validator "+path, func(t *testing.T) {
			authorizer := &recordingValidatorAuthorizer{
				ret: &platformtoken.AuthorizedContext{Subject: "unused"},
			}
			s, mockSvc := newTestServer(t)
			expectHealthProbes(mockSvc)
			s.SetValidatorAuthzEnforcement(authorizer)

			rr := serveWebAuthn(s, http.MethodGet, path, "")

			assert.Equal(t, http.StatusOK, rr.Code, "%s must be public", path)
			assert.False(t, authorizer.called, "%s must not reach the validator", path)
		})

		t.Run("default-deny "+path, func(t *testing.T) {
			s, mockSvc := newTestServer(t)
			expectHealthProbes(mockSvc)
			s.SetDefaultDenyEnforcement(&mockDPoPValidator{err: &InvalidTokenResultError{}})

			rr := serveWebAuthn(s, http.MethodGet, path, "")

			assert.Equal(t, http.StatusOK, rr.Code, "%s must be public", path)
		})
	}
}

// TestRouter_HealthProbes_DeadNamesAreNotRoutes documents why the corrected
// allowlist entries matter: the old /health/liveness + /health/readiness names
// are not mounted at all, so listing them exempted nothing.
func TestRouter_HealthProbes_DeadNamesAreNotRoutes(t *testing.T) {
	for _, path := range []string{"/api/v1/health/liveness", "/api/v1/health/readiness"} {
		t.Run(path, func(t *testing.T) {
			s, mockSvc := newTestServer(t)
			expectHealthProbes(mockSvc)
			s.SetValidatorAuthzEnforcement(&recordingValidatorAuthorizer{
				ret: &platformtoken.AuthorizedContext{Subject: "unused"},
			})

			rr := serveWebAuthn(s, http.MethodGet, path, "")

			assert.Equal(t, http.StatusNotFound, rr.Code,
				"%s is not a mounted route", path)
		})
	}
}

// TestPublicEndpointLists_HealthEntriesAgree pins that the two allowlists name
// the same real health routes, so a reader of either list sees the truth.
func TestPublicEndpointLists_HealthEntriesAgree(t *testing.T) {
	for _, path := range healthProbePaths {
		assert.Contains(t, publicEndpoints, path,
			"publicEndpoints must name the real probe %s", path)
		assert.Contains(t, defaultDenyPublicPrefixes, path,
			"defaultDenyPublicPrefixes must name the real probe %s", path)
	}
	for _, dead := range []string{"/api/v1/health/liveness", "/api/v1/health/readiness"} {
		assert.NotContains(t, publicEndpoints, dead,
			"dead entry %s must be gone", dead)
		assert.NotContains(t, defaultDenyPublicPrefixes, dead,
			"dead entry %s must be gone", dead)
	}
}
