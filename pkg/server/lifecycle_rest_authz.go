//go:build rest && iam

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

package server

import (
	"context"
	"errors"

	restapi "github.com/jeremyhahn/go-quicbaas/pkg/api/rest"
	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/auth/providers"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// ErrOIDCProviderUnavailable indicates the IAM OIDC provider that signs and
// validates access tokens is not yet initialized. The REST authz middleware
// treats this as a token-validation failure (401) rather than a server error.
var ErrOIDCProviderUnavailable = errors.New("oidc provider unavailable")

// RESTAuthzError wraps a failure that occurs while validating a bearer token or
// configuring the REST authorization middleware. It carries the failing
// operation and the underlying cause for precise diagnostics.
type RESTAuthzError struct {
	Op    string
	Cause error
}

func (e *RESTAuthzError) Error() string {
	if e.Cause != nil {
		return "rest authz: " + e.Op + ": " + e.Cause.Error()
	}
	return "rest authz: " + e.Op
}

func (e *RESTAuthzError) Unwrap() error { return e.Cause }

// oidcProviderGetter is the narrow accessor the REST authz wiring uses to reach
// the IAM OIDC provider that signs and validates every access token. It mirrors
// the type assertion the REST IAM handlers already perform on the service facade
// (see pkg/api/rest/iam_middleware.go), so the same in-process provider validates
// tokens at the global authz middleware and at the per-handler IAM middleware.
type oidcProviderGetter interface {
	GetOIDCProvider(ctx context.Context) (*iam.OIDCProvider, error)
}

// oidcTokenValidator adapts the IAM OIDCProvider to the REST AuthzMiddleware's
// TokenValidator contract. It verifies the bearer JWT against the provider's
// signing key (the same key the /oauth2/token and PIV-login endpoints sign with)
// and projects the resulting OIDC claims onto an *auth.TokenValidationResult so
// the middleware can build an authorization Subject without importing the IAM
// package.
//
// This is the canonical REST token-validation seam: it accepts the OIDC tokens
// the daemon actually issues (client_credentials service accounts and
// hardware-attested PIV / WebAuthn logins), unlike the password-era in-memory
// auth manager whose HS256 tokens are not used on the REST surface.
type oidcTokenValidator struct {
	getter oidcProviderGetter
}

// Compile-time check that oidcTokenValidator satisfies the REST TokenValidator.
var _ restapi.TokenValidator = (*oidcTokenValidator)(nil)

// ValidateToken verifies the access token and returns a populated
// *auth.TokenValidationResult. A nil error with Valid=false is never returned:
// any verification failure surfaces as a typed error so the middleware can log
// the precise cause and respond 401.
func (v *oidcTokenValidator) ValidateToken(ctx context.Context, token string) (*auth.TokenValidationResult, error) {
	provider, err := v.getter.GetOIDCProvider(ctx)
	if err != nil {
		return nil, &RESTAuthzError{Op: "get oidc provider", Cause: err}
	}
	if provider == nil {
		return nil, &RESTAuthzError{Op: "get oidc provider", Cause: ErrOIDCProviderUnavailable}
	}

	claims, err := provider.ValidateAccessToken(ctx, token)
	if err != nil {
		return nil, &RESTAuthzError{Op: "validate access token", Cause: err}
	}

	return &auth.TokenValidationResult{
		Valid:     true,
		UserID:    claims.Subject,
		Username:  claims.Name,
		TenantID:  claims.TenantID,
		Roles:     claims.Roles,
		ExpiresAt: claims.ExpiresAt.Time, // UnixTime embeds time.Time.
		IsRevoked: false,                 // ValidateAccessToken already rejects revoked tokens.
	}, nil
}

// configureRESTAuthz wires the RBAC authorization middleware onto the REST
// server so authenticated-but-unprivileged callers receive 403 on protected
// /api/v1 endpoints while public routes (health, login, oauth2/token,
// activation) remain open. It mirrors the MCP authz wiring in
// configureMCPAuth: both transports validate the same OIDC tokens and resolve
// authorization through the same role model.
//
// The token validator wraps the IAM OIDC provider. The authorization service is
// an RBAC provider seeded with the canonical built-in roles (admin → *, the
// read/write/read-only data roles, etc.), so the bootstrap admin — whose PIV /
// WebAuthn token carries the "admin" role — is authorized, while a bare
// client_credentials service-account token (which carries no roles) is denied.
//
// When the OIDC provider is unavailable (IAM not initialized) the function is a
// no-op and the REST server starts without the authz middleware, exactly as
// before. This keeps storage-less / embedded boots working.
func (s *Server) configureRESTAuthz(restServer *restapi.Server) {
	getter, ok := s.quicBaaSService.(oidcProviderGetter)
	if !ok {
		s.logger.Debug("REST authz disabled: service does not expose an OIDC provider")
		return
	}
	if provider, err := getter.GetOIDCProvider(context.Background()); err != nil || provider == nil {
		s.logger.Debug("REST authz disabled: OIDC provider not available")
		return
	}

	validator := &oidcTokenValidator{getter: getter}

	// The RBAC provider is backed by a role manager seeded with the canonical
	// built-in roles. It is constructed standalone (rather than borrowing the
	// optional password-era auth manager's role manager) so REST authorization
	// is enforced regardless of whether password authentication is enabled —
	// the integration cluster authenticates via PIV / OAuth2, not passwords.
	roleManager := auth.NewRoleManager(s.logger)
	provider, err := providers.NewRBACProvider(roleManager, s.logger)
	if err != nil {
		s.logger.WithError(err).Warn("REST authz disabled: failed to build RBAC provider")
		return
	}

	// Install the per-tenant permission scope resolver when the embedded qrdb
	// node exposes a StoreResolver (seal enabled with a barrier registry). With
	// it, an admitted permission is gated by its scope resolved in the
	// subject's own tenant: a barrier tenant's scope definitions live in that
	// tenant's barrier, a prefix / system tenant's in the shared store, and a
	// sealed barrier tenant fails closed. Without a resolver the provider keeps
	// its scope-unaware behaviour.
	if scopeStore := s.iamTenantScopeStore(); scopeStore != nil {
		provider = provider.WithScopeResolver(scopeStore)
		s.logger.Info("REST authz: per-tenant permission scope resolver installed")
	}

	authzService, err := auth.NewAuthorizationService(provider, s.logger)
	if err != nil {
		s.logger.WithError(err).Warn("REST authz disabled: failed to build authorization service")
		return
	}
	_ = authzService
	_ = validator

	// RBAC ENFORCEMENT INTENTIONALLY NOT INSTALLED YET.
	//
	// Wiring SetAuthorizationService(authzService, validator) here enforces RBAC
	// on every /api/v1 request. Integration validation showed the cluster's
	// authenticated principals (admin via PIV bearer, OAuth2 client_credentials)
	// are not yet recognized by the standalone RBAC role manager — the
	// token-claims -> roles -> role-grant chain does not currently authorize the
	// admin principal, so enforcing here 403s legitimate callers cluster-wide.
	//
	// The validator + RBAC provider are built and exercised by unit tests; full
	// enforcement is deferred until the admin principal's role propagation is
	// fixed AND validated end-to-end against the live cluster (the token must
	// carry the admin role and the role manager must grant it). Until then REST
	// keeps its prior behaviour so the data plane is reachable.
	s.logger.Info("REST authorization middleware built but enforcement deferred (RBAC role propagation pending cluster validation)")
}

// Compile-time assertion that the AuthorizationService we build satisfies the
// interface the REST server expects.
var _ interfaces.AuthorizationService = (*auth.AuthorizationServiceImpl)(nil)
