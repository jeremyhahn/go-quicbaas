//go:build rest

// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// IAMHandler handles all IAM-related REST API endpoints including
// OIDC, tenant management, user/group/role management, policies, and WebAuthn.
type IAMHandler struct {
	server *Server

	// selfSignedAuth authenticates mTLS clients via pinned SHA-256 cert thumbprint
	// (self_signed_tls_client_auth per RFC 8705 §2.2). Wired at startup.
	selfSignedAuth iam.MTLSClientAuthenticator

	// pkiAuth authenticates mTLS clients by verifying the cert chain against a
	// trusted root pool and matching Subject DN or SAN dNSName (tls_client_auth
	// per RFC 8705 §2.1). Wired at startup when an operator CA pool is available.
	pkiAuth iam.MTLSClientAuthenticator

	// tpmResolver classifies the peer certificate origin for amr/attest_method
	// claim enrichment. May be nil; defaults to AttestMethodManual when absent.
	tpmResolver *iam.TPMAttestationResolver
}

// IAMHandlerDependencies contains dependencies for the IAM handler.
type IAMHandlerDependencies struct {
	Server *Server
}

// NewIAMHandler creates a new IAM handler with the specified dependencies.
//
// The invite store is NOT constructed here. The one-time activation invite
// first-boot prints is written to the canonical PERSISTENT InviteStore owned by
// the IAM components (pkg/server/iam_init.go), and the invite endpoints resolve
// that same store lazily via getInviteStore — a previous fork that built a
// disjoint in-memory store here made the banner token permanently unredeemable.
func NewIAMHandler(deps IAMHandlerDependencies) *IAMHandler {
	return &IAMHandler{
		server: deps.Server,
	}
}

// RegisterRoutes registers all IAM routes on the provided router.
// Note: This expects the root router. OIDC endpoints are registered at root level,
// while IAM API endpoints are registered under /api/v1/iam and /api/v1/tenants.
func (h *IAMHandler) RegisterRoutes(router chi.Router) {
	// OIDC/OAuth discovery endpoints (public) - must be at root level per specs
	router.Get("/.well-known/openid-configuration", h.GetOIDCConfig)
	router.Get("/.well-known/oauth-authorization-server", h.GetOAuthASMetadata) // RFC 8414
	router.Get("/.well-known/jwks.json", h.GetJWKS)

	// OAuth2/OIDC endpoints (public)
	router.Get("/oauth2/authorize", h.Authorize)
	router.Post("/oauth2/token", h.Token)
	router.Post("/oauth2/token-mtls", h.TokenMTLS) // RFC 8705 §5 mTLS alias
	router.Get("/oauth2/userinfo", h.UserInfo)
	router.Post("/oauth2/revoke", h.Revoke)
	router.Post("/oauth2/introspect", h.Introspect) // RFC 7662 token introspection

	// Dynamic Client Registration (RFC 7591)
	router.Post("/oauth2/register", h.RegisterDynamicClient)
	router.Get("/oauth2/register/{client_id}", h.GetDynamicClient)
	router.Put("/oauth2/register/{client_id}", h.UpdateDynamicClient)
	router.Delete("/oauth2/register/{client_id}", h.DeleteDynamicClient)

	// Token management endpoints (public).
	router.Route("/api/v1/auth", func(r chi.Router) {
		r.Post("/refresh", h.RefreshToken)   // Refresh access token
		r.Post("/logout", h.Logout)          // Revoke access token
		r.Post("/validate", h.ValidateToken) // Validate access token
		r.Post("/login/piv", h.PIVLogin)     // PIV smartcard mTLS login

		// Unified login ceremony (mtls / fido2 / piv) — delegates to the
		// composed qrdb node via the LoginCeremony() facade. Public: the
		// ceremony itself authenticates (mtls handshake / signature).
		r.Post("/login/begin", h.CeremonyBeginLogin)
		r.Post("/login/finish", h.CeremonyFinishLogin)

		// Self-service registration (public — no auth required).
		// Gated by iam.self_service_registration.enabled in config.
		r.Post("/register/begin", h.RegisterSelfBegin)
		r.Post("/register/finish", h.RegisterSelfFinish)
	})

	// Federated authentication endpoints (public)
	RegisterFederationAuthRoutes(router, h)

	// IAM Management API (protected)
	// Note: We register directly under /api/v1/tenants and /api/v1/iam
	// to avoid conflicting with the main /api/v1 route block in server.go

	// Tenants management
	router.Route("/api/v1/tenants", func(r chi.Router) {
		r.Get("/", h.ListTenants)
		r.Post("/", h.CreateTenant)
		r.Get("/{tenant_id}", h.GetTenant)
		r.Put("/{tenant_id}", h.UpdateTenant)
		r.Delete("/{tenant_id}", h.DeleteTenant)

		// Per-tenant barrier lifecycle (delegates to the embedded qrdb node via
		// the broker's TenantBarrierServicer facade). Authorized as the
		// tenant_barrier resource (see pepSpecialRules), tenant-scoped.
		// The IAM route group is registered outside the standard BodyLimitMiddleware
		// group (pkg/api/rest/server.go), so body limiting is applied explicitly here
		// using the same MaxBodySize knob as the standard API routes.
		r.Route("/{tenant_id}/barrier", func(r chi.Router) {
			r.Use(BodyLimitMiddleware(h.server.config.MaxBodySize))
			r.Post("/init", h.server.HandleTenantBarrierInit)
			r.Post("/unseal", h.server.HandleTenantBarrierUnseal)
			r.Post("/unseal-share", h.server.HandleTenantBarrierUnsealShare)
			r.Post("/seal", h.server.HandleTenantBarrierSeal)
			r.Get("/status", h.server.HandleTenantBarrierStatus)
		})

		// Tenant-scoped resources
		r.Route("/{tenant_id}/users", func(r chi.Router) {
			r.Get("/", h.ListUsers)
			r.Post("/", h.CreateUser)
			r.Get("/{user_id}", h.GetUser)
			r.Put("/{user_id}", h.UpdateUser)
			r.Delete("/{user_id}", h.DeleteUser)
			r.Post("/{user_id}/roles", h.AssignRole)
			r.Delete("/{user_id}/roles/{role}", h.RevokeRole)

			// User sessions (tenant-scoped)
			r.Route("/{user_id}/sessions", func(r chi.Router) {
				r.Get("/", h.ListUserSessions)
				r.Post("/", h.CreateSession)
			})

			// GDPR routes (nested under user)
			r.Route("/{user_id}/gdpr", func(userRouter chi.Router) {
				RegisterGDPRRoutes(userRouter, h)
			})

			// MFA endpoints (nested under user)
			r.Route("/{user_id}/mfa", func(r chi.Router) {
				r.Get("/", h.GetMFAStatus)
				r.Delete("/", h.DisableMFA)
				r.Post("/enroll", h.BeginMFAEnrollment)
				r.Post("/verify", h.CompleteMFAEnrollment)
				r.Delete("/enroll/{enrollment_id}", h.CancelMFAEnrollment)
				r.Post("/challenge", h.BeginMFAChallenge)
				r.Post("/verify-challenge", h.VerifyMFAChallenge)
				r.Get("/backup-codes", h.GetBackupCodeStatus)
				r.Post("/backup-codes", h.GenerateBackupCodes)
			})

			// Federation identity link routes (nested under user)
			r.Route("/{user_id}/links", func(r chi.Router) {
				r.Post("/", h.LinkAccount)
				r.Get("/", h.GetUserLinks)
				r.Delete("/{provider_id}", h.UnlinkAccount)
			})
		})

		r.Route("/{tenant_id}/groups", func(r chi.Router) {
			r.Get("/", h.ListGroups)
			r.Post("/", h.CreateGroup)
			r.Get("/{group_id}", h.GetGroup)
			r.Put("/{group_id}", h.UpdateGroup)
			r.Delete("/{group_id}", h.DeleteGroup)
			r.Get("/{group_id}/members", h.ListMembers)
			r.Post("/{group_id}/members", h.AddMember)
			r.Delete("/{group_id}/members/{user_id}", h.RemoveMember)
		})

		r.Route("/{tenant_id}/roles", func(r chi.Router) {
			r.Get("/", h.ListRoles)
			r.Post("/", h.CreateRole)
			r.Get("/{role_id}", h.GetRole)
			r.Put("/{role_id}", h.UpdateRole)
			r.Delete("/{role_id}", h.DeleteRole)
		})

		r.Route("/{tenant_id}/apps", func(r chi.Router) {
			r.Get("/", h.ListApps)
			r.Post("/", h.RegisterApp)
			r.Get("/{client_id}", h.GetApp)
			r.Put("/{client_id}", h.UpdateApp)
			r.Delete("/{client_id}", h.DeleteApp)
			r.Post("/{client_id}/rotate-secret", h.RotateSecret)
		})

		r.Route("/{tenant_id}/policies", func(r chi.Router) {
			r.Get("/", h.ListPolicies)
			r.Post("/", h.CreatePolicy)
			r.Get("/{policy_name}", h.GetPolicy)
			r.Put("/{policy_name}", h.UpdatePolicy)
			r.Delete("/{policy_name}", h.DeletePolicy)
			r.Post("/validate", h.ValidatePolicy)
			r.Post("/test", h.TestPolicy)
			r.Post("/simulate", h.SimulatePolicy)
			r.Post("/effective-permissions", h.GetEffectivePermissions)
		})

		// WebAuthn endpoints
		r.Route("/{tenant_id}/webauthn", func(r chi.Router) {
			r.Post("/register/begin", h.BeginRegistration)
			r.Post("/register/finish", h.FinishRegistration)
			r.Post("/login/begin", h.BeginLogin)
			r.Post("/login/finish", h.FinishLogin)
		})

		// Federation provider management (tenant-scoped)
		r.Route("/{tenant_id}/providers", func(r chi.Router) {
			r.Post("/", h.RegisterProvider)
			r.Get("/", h.ListProviders)
			r.Get("/{provider_id}", h.GetProvider)
			r.Put("/{provider_id}", h.UpdateProvider)
			r.Delete("/{provider_id}", h.DeleteProvider)
		})

	})

	// Invite activation endpoints (public — no auth required).
	// These are intentionally outside the /api/v1/iam auth block so that
	// new users can activate before they have a session token.
	router.Route("/api/v1/iam/invites", func(r chi.Router) {
		r.Get("/{token}", h.GetInvite)
		r.Post("/{token}/activate/fido2/begin", h.ActivateFIDO2Begin)
		r.Post("/{token}/activate/fido2/finish", h.ActivateFIDO2Finish)
		r.Post("/{token}/activate/piv", h.ActivatePIV)
	})

	// IAM utility endpoints (not tenant-scoped)
	router.Route("/api/v1/iam", func(r chi.Router) {
		// Public tenant listing for the login UI tenant selector — no auth required.
		r.Get("/tenants/public", h.ListPublicTenants)

		// Admin approval/rejection for pending self-registered users.
		// Requires Admin@system scope; tenant_id passed as query parameter.
		r.Post("/users/{user_id}/approve", h.ApproveUser)
		r.Post("/users/{user_id}/reject", h.RejectUser)

		r.Post("/validate-client", h.ValidateClient)
		// PEP (Policy Enforcement Point) endpoints
		r.Post("/check", h.CheckAccess)
		r.Post("/check/batch", h.BatchCheckAccess) // Also support /check/batch path
		r.Post("/batch-check", h.BatchCheckAccess)

		// Audit endpoints
		r.Get("/audit", h.ListAuditEvents)
		r.Get("/audit/{id}", h.GetAuditEvent)
		r.Delete("/audit", h.DeleteOldAuditEvents)

		// Session management endpoints
		r.Get("/sessions", h.ListSessions)
		r.Delete("/sessions", h.RevokeAllSessions)
		r.Get("/sessions/{id}", h.GetSession)
		r.Delete("/sessions/{id}", h.RevokeSession)
		r.Post("/sessions/revoke-others", h.RevokeOtherSessions)
	})

	// Register delegated admin routes
	RegisterAdminRoutes(router, h)

	// Mount xkms's native WebAuthn routes under /api/v1/iam/{tenant_id}/webauthn.
	// This is the canonical surface for tenant-scoped FIDO2 ceremonies; the
	// legacy /api/v1/tenants/{tenant_id}/webauthn routes above remain wired
	// for clients that have not yet migrated. Use a bounded initialization
	// context instead of context.Background() so the manager lookup respects
	// a startup timeout.
	initCtx, initCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer initCancel()
	h.MountXKMSWebAuthnRoutes(initCtx, router)
}

// Type aliases to iam package interfaces.
// These are used by the REST handlers to interact with IAM functionality.

// TenantManager interface defines tenant management operations.
type TenantManager = iam.TenantManager

// IdentityStore is the identity store for user/group/role management.
type IdentityStore = iam.IdentityStore

// PolicyEngine is the policy engine for ABAC policy management.
type PolicyEngine = iam.PolicyEngine

// AppManager manages OAuth2 applications.
type AppManager = iam.AppManager

// OIDCProvider handles OIDC operations.
type OIDCProvider = iam.OIDCProvider

// WebAuthnManager handles WebAuthn operations.
type WebAuthnManager = service.WebAuthnManager

// logAuditEvent logs an audit event for IAM operations.
// It fails silently if the audit logger is not available.
func (h *IAMHandler) logAuditEvent(ctx context.Context, tenantID string, eventType iam.AuditEventType, category iam.AuditCategory, action string, outcome iam.AuditOutcome, resource *iam.AuditResource, details map[string]interface{}) {
	// Get audit logger from service
	auditLogger, ok := h.server.service.(interface {
		GetAuditLogger(ctx context.Context) (iam.AuditLogger, error)
	})
	if !ok {
		return
	}

	logger, err := auditLogger.GetAuditLogger(ctx)
	if err != nil || logger == nil {
		return
	}

	event := &iam.AuditEvent{
		ID:        uuid.New().String(),
		TenantID:  tenantID,
		Timestamp: time.Now().UTC(),
		EventType: eventType,
		Category:  category,
		Action:    action,
		Outcome:   outcome,
		Resource:  resource,
		Details:   details,
	}

	// Log asynchronously to avoid blocking the request
	logger.LogAsync(event)
}
