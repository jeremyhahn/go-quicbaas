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
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/jeremyhahn/go-quicbaas/pkg/api/rest/ui"
	"github.com/jeremyhahn/go-quicbaas/pkg/util/metrics"
)

// This file contributes the IAM/OIDC plane, the raw RFC 8555 ACME directory,
// the docs mount, the embedded UI mount, the Prometheus scrape endpoint, and
// the env-gated debug mount into Routes() (routes.go) under the module-only
// RouteGroups declared there, so the unified-API-server ServiceModule
// (pkg/apimodule RegisterHTTP) reaches the same surfaces the legacy
// in-process listener serves via registerIAMRoutes (iam_register.go),
// registerACMEDirectoryRoutes (handlers_acme.go), setupRouter's docs/UI
// registrations, and registerDebug (debug.go). None of the RouteGroups used
// here are iterated by registerRoutes, so the legacy registration paths are
// completely unchanged by this file's existence.

// iamHTTP resolves s's *IAMHandler for module route binding. The Server
// field is stored as interface{} (module wiring predates the module-route
// seam and several packages read it via the untyped IAMHandler() getter), so
// every binder resolves it through this helper rather than repeating the
// type assertion. registerIAMRoutes sets the field unconditionally during
// NewServer, so a nil result only means s was never constructed through it
// (e.g. a hand-built test double) — iamRoute below fails closed for that
// case rather than panicking.
func (s *Server) iamHTTP() *IAMHandler {
	h, _ := s.iamHandler.(*IAMHandler)
	return h
}

// iamRoute adapts an *IAMHandler method reference into a RouteSpec Handler,
// resolving the handler at request time and answering 503 if s was
// constructed without an IAM handler (defensive; NewServer always wires one).
func iamRoute(method func(*IAMHandler) http.HandlerFunc) func(*Server) http.HandlerFunc {
	return func(s *Server) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			h := s.iamHTTP()
			if h == nil {
				http.Error(w, "iam is not configured", http.StatusServiceUnavailable)
				return
			}
			method(h)(w, r)
		}
	}
}

// oauth2ClientAuthRateLimited wraps a RouteSpec handler factory in the shared
// per-source-IP limiter for the OAuth2 client-authenticating endpoints
// (middleware.go RateLimitOAuth2ClientAuth).
//
// It binds at the spec rather than in the consumer so that any consumer of this
// table serves those endpoints metered. Today the only one that binds
// GroupOAuth2Core is apimodule.RegisterHTTP (via ModuleRoutes and
// Server.BindRoute) for the unified server — Server.registerRoutes binds only
// GroupAPI, GroupAPIUpload and the wire groups, and the legacy listener instead
// gets them from IAMHandler.RegisterRoutes, which wraps them there. Wrapping
// here is the placement where adding a second table consumer cannot silently
// reintroduce an unbounded path to the KDF.
func oauth2ClientAuthRateLimited(next func(*Server) http.HandlerFunc) func(*Server) http.HandlerFunc {
	return func(s *Server) http.HandlerFunc {
		return RateLimitOAuth2ClientAuth(next(s))
	}
}

// iamManagedRouteSpecs returns the root-mounted, permission-gated IAM
// management surface: tenant/user/group/role/policy/provider administration,
// per-tenant barrier lifecycle, GDPR, session/audit/PEP utility endpoints,
// invite activation, delegated-admin, and the xkms-native WebAuthn ceremony
// mount. Every chi.Route block these patterns come from
// (iam.go/iam_admin.go/iam_gdpr.go/iam_xkms_webauthn_mount.go) is wrapped in
// useAPIAuthzMiddleware, so classifyRoutes (routes.go) is the single source
// of truth for their Auth/Resource/Action, exactly as it is for every other
// GroupAPI/wire-mount route in this table.
func iamManagedRouteSpecs() []RouteSpec {
	const tenants = "/api/v1/tenants/{tenant_id}"
	const user = tenants + "/users/{user_id}"
	specs := []RouteSpec{
		// ---- Tenants ----
		{Method: "GET", Pattern: "/api/v1/tenants/", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.ListTenants })},
		{Method: "POST", Pattern: "/api/v1/tenants/", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.CreateTenant })},
		{Method: "GET", Pattern: tenants, Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.GetTenant })},
		{Method: "PUT", Pattern: tenants, Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.UpdateTenant })},
		{Method: "DELETE", Pattern: tenants, Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.DeleteTenant })},
		{Method: "PATCH", Pattern: tenants + "/quotas", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.UpdateTenantEngineQuotas })},
		{Method: "PATCH", Pattern: tenants + "/entitlements", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.UpdateTenantEngineEntitlements })},

		// ---- Per-tenant barrier lifecycle (Server methods, not IAMHandler) ----
		{Method: "POST", Pattern: tenants + "/barrier/init", Group: GroupIAM, Handler: func(s *Server) http.HandlerFunc { return s.HandleTenantBarrierInit }},
		{Method: "POST", Pattern: tenants + "/barrier/unseal", Group: GroupIAM, Handler: func(s *Server) http.HandlerFunc { return s.HandleTenantBarrierUnseal }},
		{Method: "POST", Pattern: tenants + "/barrier/unseal-share", Group: GroupIAM, Handler: func(s *Server) http.HandlerFunc { return s.HandleTenantBarrierUnsealShare }},
		{Method: "POST", Pattern: tenants + "/barrier/seal", Group: GroupIAM, Handler: func(s *Server) http.HandlerFunc { return s.HandleTenantBarrierSeal }},
		{Method: "GET", Pattern: tenants + "/barrier/status", Group: GroupIAM, Handler: func(s *Server) http.HandlerFunc { return s.HandleTenantBarrierStatus }},
		{Method: "POST", Pattern: tenants + "/barrier/enable-encryption", Group: GroupIAM, Handler: func(s *Server) http.HandlerFunc { return s.HandleTenantBarrierEnableEncryption }},

		// ---- Users ----
		{Method: "GET", Pattern: tenants + "/users/", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.ListUsers })},
		{Method: "POST", Pattern: tenants + "/users/", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.CreateUser })},
		{Method: "GET", Pattern: user, Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.GetUser })},
		{Method: "PUT", Pattern: user, Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.UpdateUser })},
		{Method: "DELETE", Pattern: user, Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.DeleteUser })},
		{Method: "POST", Pattern: user + "/roles", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.AssignRole })},
		{Method: "DELETE", Pattern: user + "/roles/{role}", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.RevokeRole })},

		// ---- User sessions (admin-scoped, tenant-nested) ----
		{Method: "GET", Pattern: user + "/sessions/", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.ListUserSessions })},
		{Method: "POST", Pattern: user + "/sessions/", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.CreateSession })},
		{Method: "DELETE", Pattern: user + "/sessions/", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.RevokeAllUserSessions })},

		// ---- GDPR (RegisterGDPRRoutes, iam_gdpr.go) ----
		{Method: "POST", Pattern: user + "/gdpr/consent", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.GrantConsent })},
		{Method: "GET", Pattern: user + "/gdpr/consents", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.ListUserConsents })},
		{Method: "GET", Pattern: user + "/gdpr/consent/{purpose}", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.GetConsent })},
		{Method: "PUT", Pattern: user + "/gdpr/consent/{purpose}/withdraw", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.WithdrawConsent })},
		{Method: "DELETE", Pattern: user + "/gdpr/consent/{purpose}", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.WithdrawConsent })},
		{Method: "POST", Pattern: user + "/gdpr/export", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.RequestDataExport })},
		{Method: "GET", Pattern: user + "/gdpr/export/{export_id}", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.GetExportStatus })},
		{Method: "GET", Pattern: user + "/gdpr/export/{export_id}/download", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.DownloadExport })},
		{Method: "POST", Pattern: user + "/gdpr/deletion", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.RequestDeletion })},
		{Method: "GET", Pattern: user + "/gdpr/deletion/{deletion_id}", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.GetDeletionStatus })},
		{Method: "DELETE", Pattern: user + "/gdpr/deletion/{deletion_id}", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.CancelDeletion })},

		// ---- MFA ----
		{Method: "GET", Pattern: user + "/mfa/", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.GetMFAStatus })},
		{Method: "DELETE", Pattern: user + "/mfa/", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.DisableMFA })},
		{Method: "POST", Pattern: user + "/mfa/enroll", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.BeginMFAEnrollment })},
		{Method: "POST", Pattern: user + "/mfa/verify", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.CompleteMFAEnrollment })},
		{Method: "DELETE", Pattern: user + "/mfa/enroll/{enrollment_id}", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.CancelMFAEnrollment })},
		{Method: "POST", Pattern: user + "/mfa/challenge", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.BeginMFAChallenge })},
		{Method: "POST", Pattern: user + "/mfa/verify-challenge", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.VerifyMFAChallenge })},
		{Method: "GET", Pattern: user + "/mfa/backup-codes", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.GetBackupCodeStatus })},
		{Method: "POST", Pattern: user + "/mfa/backup-codes", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.GenerateBackupCodes })},

		// ---- Federation identity links (user-nested) ----
		{Method: "POST", Pattern: user + "/links/", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.LinkAccount })},
		{Method: "GET", Pattern: user + "/links/", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.GetUserLinks })},
		{Method: "DELETE", Pattern: user + "/links/{provider_id}", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.UnlinkAccount })},

		// ---- Groups ----
		{Method: "GET", Pattern: tenants + "/groups/", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.ListGroups })},
		{Method: "POST", Pattern: tenants + "/groups/", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.CreateGroup })},
		{Method: "GET", Pattern: tenants + "/groups/{group_id}", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.GetGroup })},
		{Method: "PUT", Pattern: tenants + "/groups/{group_id}", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.UpdateGroup })},
		{Method: "DELETE", Pattern: tenants + "/groups/{group_id}", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.DeleteGroup })},
		{Method: "GET", Pattern: tenants + "/groups/{group_id}/members", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.ListMembers })},
		{Method: "POST", Pattern: tenants + "/groups/{group_id}/members", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.AddMember })},
		{Method: "DELETE", Pattern: tenants + "/groups/{group_id}/members/{user_id}", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.RemoveMember })},

		// ---- Roles ----
		{Method: "GET", Pattern: tenants + "/roles/", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.ListRoles })},
		{Method: "POST", Pattern: tenants + "/roles/", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.CreateRole })},
		{Method: "GET", Pattern: tenants + "/roles/{role_id}", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.GetRole })},
		{Method: "PUT", Pattern: tenants + "/roles/{role_id}", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.UpdateRole })},
		{Method: "DELETE", Pattern: tenants + "/roles/{role_id}", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.DeleteRole })},

		// ---- OAuth2 applications ----
		{Method: "GET", Pattern: tenants + "/apps/", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.ListApps })},
		{Method: "POST", Pattern: tenants + "/apps/", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.RegisterApp })},
		{Method: "GET", Pattern: tenants + "/apps/{client_id}", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.GetApp })},
		{Method: "PUT", Pattern: tenants + "/apps/{client_id}", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.UpdateApp })},
		{Method: "DELETE", Pattern: tenants + "/apps/{client_id}", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.DeleteApp })},
		{Method: "POST", Pattern: tenants + "/apps/{client_id}/rotate-secret", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.RotateSecret })},

		// ---- ABAC policies ----
		{Method: "GET", Pattern: tenants + "/policies/", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.ListPolicies })},
		{Method: "POST", Pattern: tenants + "/policies/", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.CreatePolicy })},
		{Method: "GET", Pattern: tenants + "/policies/{policy_name}", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.GetPolicy })},
		{Method: "PUT", Pattern: tenants + "/policies/{policy_name}", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.UpdatePolicy })},
		{Method: "DELETE", Pattern: tenants + "/policies/{policy_name}", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.DeletePolicy })},
		{Method: "POST", Pattern: tenants + "/policies/validate", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.ValidatePolicy })},
		{Method: "POST", Pattern: tenants + "/policies/test", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.TestPolicy })},
		{Method: "POST", Pattern: tenants + "/policies/simulate", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.SimulatePolicy })},
		{Method: "POST", Pattern: tenants + "/policies/effective-permissions", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.GetEffectivePermissions })},

		// ---- Federation providers (tenant-scoped) ----
		{Method: "POST", Pattern: tenants + "/providers/", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.RegisterProvider })},
		{Method: "GET", Pattern: tenants + "/providers/", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.ListProviders })},
		{Method: "GET", Pattern: tenants + "/providers/{provider_id}", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.GetProvider })},
		{Method: "PUT", Pattern: tenants + "/providers/{provider_id}", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.UpdateProvider })},
		{Method: "DELETE", Pattern: tenants + "/providers/{provider_id}", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.DeleteProvider })},

		// ---- Invite activation (public via preAuthRouteTemplates) ----
		{Method: "GET", Pattern: "/api/v1/iam/{tenant_id}/invites/{token}", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.GetInvite })},
		{Method: "POST", Pattern: "/api/v1/iam/{tenant_id}/invites/{token}/activate/fido2/begin", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.ActivateFIDO2Begin })},
		{Method: "POST", Pattern: "/api/v1/iam/{tenant_id}/invites/{token}/activate/fido2/finish", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.ActivateFIDO2Finish })},
		{Method: "POST", Pattern: "/api/v1/iam/{tenant_id}/invites/{token}/activate/piv", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.ActivatePIV })},

		// ---- IAM utility (sessions / audit / PEP checks / approvals) ----
		{Method: "POST", Pattern: "/api/v1/iam/users/{user_id}/approve", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.ApproveUser })},
		{Method: "POST", Pattern: "/api/v1/iam/users/{user_id}/reject", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.RejectUser })},
		{Method: "POST", Pattern: "/api/v1/iam/validate-client", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.ValidateClient })},
		{Method: "POST", Pattern: "/api/v1/iam/check", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.CheckAccess })},
		{Method: "POST", Pattern: "/api/v1/iam/check/batch", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.BatchCheckAccess })},
		{Method: "POST", Pattern: "/api/v1/iam/batch-check", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.BatchCheckAccess })},
		{Method: "GET", Pattern: "/api/v1/iam/audit", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.ListAuditEvents })},
		{Method: "GET", Pattern: "/api/v1/iam/audit/{id}", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.GetAuditEvent })},
		{Method: "POST", Pattern: "/api/v1/iam/audit", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.RecordAuditEvent })},
		{Method: "DELETE", Pattern: "/api/v1/iam/audit", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.DeleteOldAuditEvents })},
		{Method: "GET", Pattern: "/api/v1/iam/sessions", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.ListSessions })},
		{Method: "DELETE", Pattern: "/api/v1/iam/sessions", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.RevokeAllSessions })},
		{Method: "GET", Pattern: "/api/v1/iam/sessions/{id}", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.GetSession })},
		{Method: "DELETE", Pattern: "/api/v1/iam/sessions/{id}", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.RevokeSession })},
		{Method: "POST", Pattern: "/api/v1/iam/sessions/revoke-others", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.RevokeOtherSessions })},

		// ---- OIDC-session PIV login (P3.3 de-collision) ----
		// Moved from /api/v1/auth/login/piv: on a composed node xkms's module
		// owns that path as part of its own session-login plane, so
		// quicbaas's distinct OIDC-provider-backed PIV login (mints via
		// authService.sessionAuthority -> iam.OIDCProvider, NOT the qrdb
		// LoginCeremony facade that /api/v1/auth/login/{begin,finish} use)
		// was renamed here to avoid the collision while remaining reachable.
		// Public, same class as the WebAuthn login ceremony below: the mTLS
		// client certificate presented on the connection IS the credential,
		// no bearer token exists yet (frozen RouteAuthPublic,
		// routes_permissions_generated.go).
		{Method: "POST", Pattern: "/api/v1/iam/login/piv", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.PIVLogin })},

		// ---- xkms-native WebAuthn ceremony mount ----
		// login/begin + login/finish derive Public via preAuthRouteTemplates
		// (WebAuthn IS the authentication mechanism); the registration verbs
		// derive user_authfactors:add / :get via pepSpecialRulesBase. The
		// dispatcher resolves the tenant's xkms WebAuthn service PER REQUEST
		// (moduleWebAuthnHandler, iam_xkms_webauthn_mount.go) rather than
		// once at registration time, so it stays reachable even if the
		// factory was not yet available when the module registered.
		{Method: "POST", Pattern: "/api/v1/iam/{tenant_id}/webauthn/registration/begin", Group: GroupIAM, Handler: func(s *Server) http.HandlerFunc { return s.moduleWebAuthnHandler() }},
		{Method: "POST", Pattern: "/api/v1/iam/{tenant_id}/webauthn/registration/finish", Group: GroupIAM, Handler: func(s *Server) http.HandlerFunc { return s.moduleWebAuthnHandler() }},
		{Method: "GET", Pattern: "/api/v1/iam/{tenant_id}/webauthn/registration/status", Group: GroupIAM, Handler: func(s *Server) http.HandlerFunc { return s.moduleWebAuthnHandler() }},
		{Method: "POST", Pattern: "/api/v1/iam/{tenant_id}/webauthn/login/begin", Group: GroupIAM, Handler: func(s *Server) http.HandlerFunc { return s.moduleWebAuthnHandler() }},
		{Method: "POST", Pattern: "/api/v1/iam/{tenant_id}/webauthn/login/finish", Group: GroupIAM, Handler: func(s *Server) http.HandlerFunc { return s.moduleWebAuthnHandler() }},

		// ---- Delegated admin (RegisterAdminRoutes, iam_admin.go) ----
		{Method: "POST", Pattern: "/api/v1/admin/assignments/", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.CreateAdminAssignment })},
		{Method: "GET", Pattern: "/api/v1/admin/assignments/", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.ListAdminAssignments })},
		{Method: "GET", Pattern: "/api/v1/admin/assignments/{assignment_id}", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.GetAdminAssignment })},
		{Method: "PUT", Pattern: "/api/v1/admin/assignments/{assignment_id}", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.UpdateAdminAssignment })},
		{Method: "DELETE", Pattern: "/api/v1/admin/assignments/{assignment_id}", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.DeleteAdminAssignment })},
		{Method: "POST", Pattern: "/api/v1/admin/check-permission", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.CheckAdminPermission })},
		{Method: "GET", Pattern: "/api/v1/admin/impersonation/", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.ListImpersonations })},
		{Method: "POST", Pattern: "/api/v1/admin/impersonation/start", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.StartImpersonation })},
		{Method: "POST", Pattern: "/api/v1/admin/impersonation/{session_id}/end", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.EndImpersonation })},
		{Method: "GET", Pattern: "/api/v1/admin/users/{user_id}/assignments", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.GetUserAssignments })},
		{Method: "GET", Pattern: "/api/v1/admin/users/{user_id}/impersonations", Group: GroupIAM, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.ListActiveImpersonations })},
	}
	return specs
}

// oauth2RegisterRouteSpecs returns the RFC 7591/7592 dynamic client
// registration surface. POST (create) derives Permission (tenants:create)
// via the PEP mapper's "/oauth2/register" entry; the three per-client
// management verbs derive Public via preAuthRouteTemplates (their credential
// is the per-client registration access token, not a platform bearer).
// Tagged GroupOAuth2Register (not GroupIAM) only because its mount is not
// under /api/v1 — TestRoutes_SurfaceGuard validates against
// routeGroupMounts[GroupOAuth2Register].
func oauth2RegisterRouteSpecs() []RouteSpec {
	return []RouteSpec{
		{Method: "POST", Pattern: "/oauth2/register/", Group: GroupOAuth2Register, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.RegisterDynamicClient })},
		{Method: "GET", Pattern: "/oauth2/register/{client_id}", Group: GroupOAuth2Register, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.GetDynamicClient })},
		{Method: "PUT", Pattern: "/oauth2/register/{client_id}", Group: GroupOAuth2Register, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.UpdateDynamicClient })},
		{Method: "DELETE", Pattern: "/oauth2/register/{client_id}", Group: GroupOAuth2Register, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.DeleteDynamicClient })},
	}
}

// oidcDiscoveryRouteSpecs returns the OIDC/OAuth2 discovery documents.
// registerIAMRoutes mounts these at the router root with no authorization
// wrapper — every OIDC issuer's discovery surface is public by protocol — so
// Auth is set explicitly rather than derived (see GroupOIDCDiscovery's doc
// comment in routes.go for why classifyRoutes cannot see this surface).
func oidcDiscoveryRouteSpecs() []RouteSpec {
	return []RouteSpec{
		{Method: "GET", Pattern: "/.well-known/openid-configuration", Group: GroupOIDCDiscovery, Auth: RouteAuthPublic, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.GetOIDCConfig })},
		{Method: "GET", Pattern: "/.well-known/oauth-authorization-server", Group: GroupOIDCDiscovery, Auth: RouteAuthPublic, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.GetOAuthASMetadata })},
		{Method: "GET", Pattern: "/.well-known/oauth-protected-resource", Group: GroupOIDCDiscovery, Auth: RouteAuthPublic, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.GetProtectedResourceMetadata })}, // RFC 9728
		{Method: "GET", Pattern: "/.well-known/jwks.json", Group: GroupOIDCDiscovery, Auth: RouteAuthPublic, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.GetJWKS })},
	}
}

// oauth2CoreRouteSpecs returns the core OAuth2 token endpoints. Each
// authenticates from a credential carried IN the request (client_secret, the
// token itself, the mTLS handshake) rather than a pre-validated platform
// bearer, and registerIAMRoutes mounts them unwrapped exactly like the
// discovery documents, so Auth is set explicitly to Public.
func oauth2CoreRouteSpecs() []RouteSpec {
	return []RouteSpec{
		{Method: "GET", Pattern: "/oauth2/authorize", Group: GroupOAuth2Core, Auth: RouteAuthPublic, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.Authorize })},
		// Token, revoke and introspect are rate limited (#118 F1): each is
		// RouteAuthPublic and each authenticates its caller through
		// ValidateClientSecret, so every attempt spends a PBKDF2 derivation
		// whether or not the client exists and an unbounded path is a
		// CPU-exhaustion amplifier. One shared bucket per source across the
		// three — the capped resource is total KDF CPU per source.
		//
		// token-mtls and userinfo are NOT limited and are not omissions:
		// token-mtls authenticates by client certificate on the TLS handshake
		// (RFC 8705) and userinfo by bearer token, so neither lets an anonymous
		// caller reach the KDF.
		{Method: "POST", Pattern: "/oauth2/token", Group: GroupOAuth2Core, Auth: RouteAuthPublic, Handler: oauth2ClientAuthRateLimited(iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.Token }))},
		{Method: "POST", Pattern: "/oauth2/token-mtls", Group: GroupOAuth2Core, Auth: RouteAuthPublic, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.TokenMTLS })},
		{Method: "GET", Pattern: "/oauth2/userinfo", Group: GroupOAuth2Core, Auth: RouteAuthPublic, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.UserInfo })},
		{Method: "POST", Pattern: "/oauth2/revoke", Group: GroupOAuth2Core, Auth: RouteAuthPublic, Handler: oauth2ClientAuthRateLimited(iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.Revoke }))},
		{Method: "POST", Pattern: "/oauth2/introspect", Group: GroupOAuth2Core, Auth: RouteAuthPublic, Handler: oauth2ClientAuthRateLimited(iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.Introspect }))},
	}
}

// apiAuthPublicRouteSpecs returns the /api/v1/auth login ceremony +
// self-registration verbs, the federated login/callback redirect, and the
// mTLS agent-registration bootstrap. registerIAMRoutes /
// RegisterFederationAuthRoutes / registerAgentRegistrationPublicRoutes mount
// every one of these at the router root with NO authorization wrapper: each
// is a first-contact ceremony (no token exists yet) or authenticates via the
// credential carried in the request itself (the mTLS handshake for agent
// registration), so Auth is set explicitly to Public.
//
// P3.3 de-collision: the token-lifecycle verbs (refresh/logout/validate) and
// the OIDC-session PIV login (login/piv) are DELIBERATELY absent from this
// module-surface table. On a composed node the xkms module owns the
// session-login plane at these exact /api/v1/auth paths (identical
// mtls/fido2/piv login endpoints on every node in the stack, per the DRY
// requirement), so quicbaas's own registrations at those paths would
// route-collide with xkms's module. All three token-lifecycle verbs share
// ONE session authority with PIV login — RefreshToken/Logout/ValidateToken
// all resolve through authService.sessionAuthority (pkg/service/quicbaas.go),
// the exact iam.OIDCProvider that PIVLogin also mints sessions from
// (pkg/api/rest/iam_login_piv.go) — so all four are the same superseded
// family. PIVLogin itself is KEPT, but moved to the IAM namespace
// (POST /api/v1/iam/login/piv, iamManagedRouteSpecs below) to avoid the
// literal path collision on /api/v1/auth/login/piv while remaining
// reachable; refresh/logout/validate remain reachable ONLY on the legacy
// in-process listener's independent hand-mount (iam.go RegisterRoutes),
// which this table does not feed — a standalone (non-composed) deployment
// loses nothing.
func apiAuthPublicRouteSpecs() []RouteSpec {
	return []RouteSpec{
		{Method: "POST", Pattern: "/api/v1/auth/login/begin", Group: GroupAPIAuthPublic, Auth: RouteAuthPublic, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.CeremonyBeginLogin })},
		{Method: "POST", Pattern: "/api/v1/auth/login/finish", Group: GroupAPIAuthPublic, Auth: RouteAuthPublic, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.CeremonyFinishLogin })},
		{Method: "POST", Pattern: "/api/v1/auth/register/begin", Group: GroupAPIAuthPublic, Auth: RouteAuthPublic, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.RegisterSelfBegin })},
		{Method: "POST", Pattern: "/api/v1/auth/register/finish", Group: GroupAPIAuthPublic, Auth: RouteAuthPublic, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.RegisterSelfFinish })},
		{Method: "GET", Pattern: "/api/v1/auth/providers/{tenant_id}/{provider_name}/login", Group: GroupAPIAuthPublic, Auth: RouteAuthPublic, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.BeginFederatedLogin })},
		{Method: "GET", Pattern: "/api/v1/auth/providers/{tenant_id}/{provider_name}/callback", Group: GroupAPIAuthPublic, Auth: RouteAuthPublic, Handler: iamRoute(func(h *IAMHandler) http.HandlerFunc { return h.HandleFederatedCallback })},
		{Method: "POST", Pattern: "/api/v1/agents/register", Group: GroupAPIAuthPublic, Auth: RouteAuthPublic, Handler: func(s *Server) http.HandlerFunc { return s.HandleAgentRegister }},
	}
}

// acmeDirectoryRouteSpecs returns the raw RFC 8555 ACME directory
// (registerACMEDirectoryRoutes, handlers_acme.go), mounted at the router
// root so the advertised directory URL follows the RFC 8555 convention.
// Every endpoint authenticates via JWS + nonce + account key per the ACME
// protocol itself and carries no platform-token gate on the legacy listener,
// so Auth is set explicitly to Public. Handlers reuse s.acmeRFCHandler
// verbatim (handlers_acme.go) — the same request-time resolution against the
// composed xkms RestServicer that answers 503 when ACME is not configured.
func acmeDirectoryRouteSpecs() []RouteSpec {
	acme := func(sel func(acmeRest) http.HandlerFunc) func(*Server) http.HandlerFunc {
		return func(s *Server) http.HandlerFunc { return s.acmeRFCHandler(sel) }
	}
	return []RouteSpec{
		{Method: "GET", Pattern: "/acme/directory", Group: GroupACMEDirectory, Auth: RouteAuthPublic, Handler: acme(func(rs acmeRest) http.HandlerFunc { return rs.DirectoryHandler })},
		{Method: "HEAD", Pattern: "/acme/directory", Group: GroupACMEDirectory, Auth: RouteAuthPublic, Handler: acme(func(rs acmeRest) http.HandlerFunc { return rs.DirectoryHandler })},
		{Method: "GET", Pattern: "/acme/new-nonce", Group: GroupACMEDirectory, Auth: RouteAuthPublic, Handler: acme(func(rs acmeRest) http.HandlerFunc { return rs.NewNonceHandler })},
		{Method: "HEAD", Pattern: "/acme/new-nonce", Group: GroupACMEDirectory, Auth: RouteAuthPublic, Handler: acme(func(rs acmeRest) http.HandlerFunc { return rs.NewNonceHandler })},
		{Method: "POST", Pattern: "/acme/new-account", Group: GroupACMEDirectory, Auth: RouteAuthPublic, Handler: acme(func(rs acmeRest) http.HandlerFunc { return rs.NewAccountHandler })},
		{Method: "POST", Pattern: "/acme/account/{id}", Group: GroupACMEDirectory, Auth: RouteAuthPublic, Handler: acme(func(rs acmeRest) http.HandlerFunc { return rs.AccountHandler })},
		{Method: "POST", Pattern: "/acme/new-order", Group: GroupACMEDirectory, Auth: RouteAuthPublic, Handler: acme(func(rs acmeRest) http.HandlerFunc { return rs.NewOrderHandler })},
		{Method: "POST", Pattern: "/acme/order/{id}", Group: GroupACMEDirectory, Auth: RouteAuthPublic, Handler: acme(func(rs acmeRest) http.HandlerFunc { return rs.OrderHandler })},
		{Method: "POST", Pattern: "/acme/order/{id}/finalize", Group: GroupACMEDirectory, Auth: RouteAuthPublic, Handler: acme(func(rs acmeRest) http.HandlerFunc { return rs.OrderFinalizeHandler })},
		{Method: "POST", Pattern: "/acme/orders/{id}", Group: GroupACMEDirectory, Auth: RouteAuthPublic, Handler: acme(func(rs acmeRest) http.HandlerFunc { return rs.OrdersListHandler })},
		{Method: "POST", Pattern: "/acme/authz/{id}", Group: GroupACMEDirectory, Auth: RouteAuthPublic, Handler: acme(func(rs acmeRest) http.HandlerFunc { return rs.AuthorizationHandler })},
		{Method: "POST", Pattern: "/acme/challenge/{id}", Group: GroupACMEDirectory, Auth: RouteAuthPublic, Handler: acme(func(rs acmeRest) http.HandlerFunc { return rs.ChallengeHandler })},
		{Method: "POST", Pattern: "/acme/cert/{id}", Group: GroupACMEDirectory, Auth: RouteAuthPublic, Handler: acme(func(rs acmeRest) http.HandlerFunc { return rs.CertificateHandler })},
		{Method: "POST", Pattern: "/acme/revoke-cert", Group: GroupACMEDirectory, Auth: RouteAuthPublic, Handler: acme(func(rs acmeRest) http.HandlerFunc { return rs.RevokeCertHandler })},
		{Method: "POST", Pattern: "/acme/key-change", Group: GroupACMEDirectory, Auth: RouteAuthPublic, Handler: acme(func(rs acmeRest) http.HandlerFunc { return rs.KeyChangeHandler })},
		{Method: "GET", Pattern: "/acme/ca-bundle", Group: GroupACMEDirectory, Auth: RouteAuthPublic, Handler: acme(func(rs acmeRest) http.HandlerFunc { return rs.CABundleHandler })},
	}
}

// docsRouteSpecs returns the embedded API documentation mount (Swagger UI +
// OpenAPI spec), registered at the router root with no authorization
// wrapper.
func docsRouteSpecs() []RouteSpec {
	return []RouteSpec{
		{Method: "GET", Pattern: "/docs", Group: GroupDocs, Auth: RouteAuthPublic, Handler: func(s *Server) http.HandlerFunc { return s.HandleDocsRedirect }},
		{Method: "GET", Pattern: "/docs/", Group: GroupDocs, Auth: RouteAuthPublic, Handler: func(s *Server) http.HandlerFunc { return s.HandleSwaggerUI }},
		{Method: "GET", Pattern: "/docs/openapi.yaml", Group: GroupDocs, Auth: RouteAuthPublic, Handler: func(s *Server) http.HandlerFunc { return s.HandleOpenAPISpec }},
	}
}

// uiMountRouteSpecs returns the embedded Svelte web UI SPA mount
// (ui.RegisterUI), registered at the router root with no authorization
// wrapper: the login page must render before the caller holds a token. GET
// only, mirroring go-xkms's WithWebUI module mount (pkg/apimodule/
// module_http.go) — static asset serving needs no other verb.
func uiMountRouteSpecs() []RouteSpec {
	return []RouteSpec{
		{Method: "GET", Pattern: "/ui", Group: GroupUIMount, Auth: RouteAuthPublic, Handler: func(s *Server) http.HandlerFunc {
			return func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, "/ui/", http.StatusFound)
			}
		}},
		{Method: "GET", Pattern: "/ui/*", Group: GroupUIMount, Auth: RouteAuthPublic, Handler: func(s *Server) http.HandlerFunc { return ui.Handler().ServeHTTP }},
	}
}

// metricsScrapeRouteSpecs returns the Prometheus scrape endpoint mounted
// inside the /api/v1 chi group (server.go registerRoutes, the g.Route
// "/metrics" block). No PEP entry maps "/api/v1/metrics" specifically, so
// classifyRoutes derives the same identity-gated (RouteAuthSession) posture
// as its sibling /api/v1/metrics/performance route — carried over faithfully
// rather than hand-set.
func metricsScrapeRouteSpecs() []RouteSpec {
	return []RouteSpec{
		{Method: "GET", Pattern: "/api/v1/metrics/", Group: GroupMetricsScrape, Handler: func(s *Server) http.HandlerFunc {
			h := promhttp.HandlerFor(metrics.Registry(), promhttp.HandlerOpts{EnableOpenMetrics: true})
			return h.ServeHTTP
		}},
	}
}

// debugModuleRouteSpecs returns the env-gated shard-routing introspection
// mount (debug.go), reachable on the module surface only when
// QUICBAAS_ENABLE_DEBUG_ROUTES=1 — the SAME env var and semantics the legacy
// listener's registerDebug checks, evaluated once at module-registration
// time (matching registerDebug's own construction-time check). The PEP
// mapper already assigns "/api/v1/debug" the admin:status permission, so
// classifyRoutes derives it; the surface is never exposed publicly.
func debugModuleRouteSpecs() []RouteSpec {
	return []RouteSpec{
		{
			Method:  "GET",
			Pattern: "/api/v1/debug/route/{type}/{id}",
			Group:   GroupDebugModule,
			Enabled: func(s *Server) bool { return debugRoutesEnabled() },
			Handler: func(s *Server) http.HandlerFunc { return s.handleDebugRoute },
		},
	}
}
