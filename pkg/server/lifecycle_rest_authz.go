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
	"strings"

	restapi "github.com/jeremyhahn/go-quicbaas/pkg/api/rest"
	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
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

// Compile-time check that oidcTokenValidator is DPoP-aware so the REST authz
// middleware enforces RFC 9449 sender-constraint on inbound cnf.jkt-bound tokens
// and builds the scp -> RBAC / tenant_id -> RLS AuthorizedContext.
var _ restapi.DPoPTokenValidator = (*oidcTokenValidator)(nil)

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

// ValidateTokenWithDPoP verifies the access token and, when it carries a cnf.jkt
// binding, enforces the presented DPoP proof against it (RFC 9449) before
// returning the platform claims the REST authz middleware projects onto an
// AuthorizedContext. It delegates to OIDCProvider.ValidateAccessTokenWithDPoP so
// inbound enforcement reuses the SAME signing-key resolution and DPoP replay
// cache the issuance path uses — one code path, no duplicated crypto.
//
// A cnf.jkt-bound token presented without a valid proof (a bearer replay) returns
// a typed error and the middleware responds 401. A plain bearer token (no
// cnf.jkt) is accepted, exactly as before.
func (v *oidcTokenValidator) ValidateTokenWithDPoP(ctx context.Context, token, dpopProof, htm, htu string) (*restapi.PlatformTokenResult, error) {
	provider, err := v.getter.GetOIDCProvider(ctx)
	if err != nil {
		return nil, &RESTAuthzError{Op: "get oidc provider", Cause: err}
	}
	if provider == nil {
		return nil, &RESTAuthzError{Op: "get oidc provider", Cause: ErrOIDCProviderUnavailable}
	}

	claims, err := provider.ValidateAccessTokenWithDPoP(ctx, token, dpopProof, htm, htu)
	if err != nil {
		return nil, &RESTAuthzError{Op: "validate access token (dpop)", Cause: err}
	}

	jkt := ""
	if claims.Confirmation != nil {
		jkt = claims.Confirmation.Jkt
	}

	return &restapi.PlatformTokenResult{
		Subject:  claims.Subject,
		Username: claims.Name,
		TenantID: claims.TenantID,
		Roles:    claims.Roles,
		Scopes:   tokenScopes(claims),
		JKT:      jkt,
	}, nil
}

// tokenScopes returns the authoritative scope set the platform-token contract
// defines: the array-form scp claim when present, otherwise the space-separated
// scope string split into a slice. This mirrors platformtoken.Claims.scopes so
// the REST surface and downstream services derive the same RBAC scope set.
func tokenScopes(claims *iam.Claims) []string {
	if len(claims.SCP) > 0 {
		return claims.SCP
	}
	if strings.TrimSpace(claims.Scope) == "" {
		return nil
	}
	return strings.Fields(claims.Scope)
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

	// The token validator wraps the IAM OIDC provider and is DPoP-aware: it
	// enforces RFC 9449 sender-constraint on cnf.jkt-bound tokens and surfaces
	// the platform claims (scp / tenant_id / roles) the middleware projects onto
	// an AuthorizedContext.
	validator := &oidcTokenValidator{getter: getter}

	// Install the proof-of-possession-only enforcement gate. It validates every
	// inbound bearer and, for DPoP sender-constrained tokens, enforces the
	// presented proof fail-closed (a bearer replay of a bound token is rejected
	// 401), then injects the verified principal + AuthorizedContext so handlers
	// can enforce scp -> RBAC / tenant_id -> RLS.
	//
	// The full RBAC authorization DECISION (Authorize gate) remains intentionally
	// deferred: integration validation showed the cluster's authenticated
	// principals (admin via PIV bearer, OAuth2 client_credentials) are not yet
	// recognized by the standalone RBAC role manager, so enforcing the decision
	// here 403s legitimate callers cluster-wide. Per-resource RBAC is enforced at
	// the handler layer via the injected AuthorizedContext until that
	// token-claims -> roles -> role-grant chain is validated end-to-end. This
	// keeps the data plane reachable for unbound-bearer callers while closing the
	// DPoP replay gap on sender-constrained tokens (additive enforcement).
	//
	// When the operator opts in (SecurityConfig.RequireAuthz), install the
	// default-deny scope -> RBAC gate instead: non-public, non-cluster-mTLS
	// requests must present a valid platform token carrying the required scope,
	// and token-less requests are denied. This is the locked-down posture and is
	// off by default so current behavior (anonymous data-plane access) is
	// unchanged until the fleet has migrated to authenticated clients.
	if s.config.Security.RequireAuthz {
		// Build the single central platform-token Validator and install the
		// reference (REST) enforcement gate. One Validator call per request does
		// offline signature / iss / aud / exp verification, DPoP sender-constraint
		// (RFC 9449) for cnf.jkt-bound tokens, the offline scp -> permission
		// decision, and the per-tenant liveness/revocation check. This REPLACES the
		// prior online qrdb role-PDP path on REST.
		platformValidator, err := s.buildPlatformValidator(context.Background())
		if err != nil {
			s.logger.WithError(err).Error("REST authz disabled: failed to build platform-token validator")
			return
		}
		restServer.SetValidatorAuthzEnforcement(platformValidator)
		s.logger.Info("REST platform-token validator authorization installed (RequireAuthz=true)")
		return
	}

	restServer.SetTokenBindingEnforcement(validator)
	s.logger.Info("REST token-binding (DPoP/mTLS) enforcement installed; RBAC decision deferred (role propagation pending cluster validation)")
}
