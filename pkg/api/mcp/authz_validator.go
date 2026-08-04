//go:build mcp

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

package mcp

import (
	"context"
	"errors"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/platformtoken"
)

// ValidatorAuthorizer is the narrow seam the MCP authorization checker depends on
// for the unified platform-token authorization decision. It is satisfied by
// *platformtoken.Validator: a SINGLE call validates the token offline
// (signature / iss / aud / exp), enforces DPoP sender-constraint when the token
// carries cnf.jkt, runs the offline scp decision, and applies the per-tenant
// liveness mode. requiredPerm is the qrdb-canonical colon "resource:action".
//
// The checker stays decoupled from how the validator is constructed (the
// in-process IAM OIDC key source lives in pkg/server) and tests inject a double.
//
// MCP carries NO DPoP proof: a tool call is a JSON-RPC request over the bearer
// header with no proof channel, so the checker always calls Authorize with empty
// dpopProof/htm/htu. The validator still enforces DPoP when the token itself is
// cnf.jkt-bound (a sender-constrained token presented without a proof is denied),
// so this is not a relaxation of the sender-constraint check.
type ValidatorAuthorizer interface {
	Authorize(ctx context.Context, token, requiredPerm, dpopProof, htm, htu string) (*platformtoken.AuthorizedContext, error)
}

// Compile-time check that the SDK validator satisfies the MCP seam.
var _ ValidatorAuthorizer = (*platformtoken.Validator)(nil)

// authorizeViaValidator is the request-time authorization gate backed by the
// central platform-token Validator. It REPLACES the bespoke OIDC token-validate +
// online qrdb role-PDP path: the Validator performs signature + iss + aud + exp,
// DPoP sender-constraint (RFC 9449) for cnf.jkt-bound tokens, the offline scp ->
// permission decision, and the per-tenant liveness/revocation check in one call.
//
// It is default-deny: a non-public tool with no qrdb-canonical permission mapping
// is denied (there is no registered permission to admit it), and any
// validation/authorization failure fails closed with the AuthorizationError type
// mapped by validatorErrorToMCP (missing-token, invalid/inactive-token,
// permission-denied, or misconfigured -> InternalError). On allow it returns the
// verified AuthorizedContext so the caller can reconstruct and inject the
// principal for downstream service-layer per-key authorization.
func (ac *AuthorizationChecker) authorizeViaValidator(ctx context.Context, toolName, token string) (*platformtoken.AuthorizedContext, *AuthorizationError) {
	// Public tools require no token and no authorization decision.
	if IsPublicTool(toolName) {
		return nil, nil
	}

	// A non-public tool requires a bearer token under the validator posture —
	// unless the caller is an AGENT authenticating with its verified mTLS agent
	// certificate (OU=agents), in which case it is admitted with the bounded
	// least-privilege AgentMCPScopes for its own tenant. A presented token
	// always wins over the certificate (this branch is token-less only); see
	// authz_agent_cert.go for the trust model.
	if token == "" {
		agentCtx, certErr := agentCertAuthorizedContext(ctx)
		if certErr != nil {
			return nil, certErr
		}
		if agentCtx != nil {
			return ac.authorizeAgentCertContext(agentCtx, toolName)
		}
		return nil, &AuthorizationError{
			Type:    AuthErrorMissingToken,
			Message: "authentication required",
		}
	}

	// Map the tool to the qrdb-canonical permission it demands. Under this posture
	// an unmapped protected tool is default-denied: there is no registered
	// permission to admit it.
	resource, action, ok := getToolQRDBPermission(toolName)
	if !ok {
		return nil, &AuthorizationError{
			Type:    AuthErrorPermissionDenied,
			Message: "permission denied: no qrdb permission mapping for tool " + toolName,
		}
	}
	requiredPerm := resource + ":" + action

	// MCP carries no DPoP proof channel: dpopProof/htm/htu are empty. The
	// validator still rejects a cnf.jkt-bound token presented without a proof.
	authzCtx, err := ac.validatorAuthorizer.Authorize(ctx, token, requiredPerm, "", "", "")
	if err != nil {
		return nil, validatorErrorToMCP(err)
	}
	return authzCtx, nil
}

// validatorErrorToMCP maps a platform-token authorization error to the MCP
// AuthorizationError convention, fail-closed:
//
//   - *platformtoken.ScopeDeniedError   -> permission-denied (authenticated but
//     not permitted);
//   - *platformtoken.TokenInactiveError -> invalid-token (token revoked / subject
//     disabled);
//   - *platformtoken.ConfigError        -> misconfigured (server-side wiring
//     error: a missing liveness checker or replay cache is an operator error, not
//     the caller's; the request is still DENIED);
//   - every other error (invalid signature / issuer / audience / expiry / DPoP /
//     missing-proof / liveness-backend failure) -> invalid-token.
//
// There is no allow path here: an unrecognised error still denies.
func validatorErrorToMCP(err error) *AuthorizationError {
	var scopeDenied *platformtoken.ScopeDeniedError
	if errors.As(err, &scopeDenied) {
		return &AuthorizationError{
			Type:    AuthErrorPermissionDenied,
			Message: "permission denied: " + err.Error(),
		}
	}
	var inactive *platformtoken.TokenInactiveError
	if errors.As(err, &inactive) {
		return &AuthorizationError{
			Type:    AuthErrorInvalidToken,
			Message: "invalid token: " + err.Error(),
		}
	}
	var cfg *platformtoken.ConfigError
	if errors.As(err, &cfg) {
		return &AuthorizationError{
			Type:    AuthErrorMisconfigured,
			Message: "authorization is misconfigured",
		}
	}
	return &AuthorizationError{
		Type:    AuthErrorInvalidToken,
		Message: "invalid token",
	}
}

// subjectFromAuthorizedContext reconstructs the principal Subject from a verified
// AuthorizedContext. The AuthorizedContext is the canonical post-validation
// identity; Username carries the token name claim so principal matching by
// username (in addition to ID) is preserved for service-layer per-key
// authorization.
func subjectFromAuthorizedContext(authzCtx *platformtoken.AuthorizedContext) interfaces.Subject {
	return interfaces.Subject{
		ID:            authzCtx.Subject,
		Username:      authzCtx.Username,
		TenantID:      authzCtx.TenantID,
		Roles:         authzCtx.Roles,
		Authenticated: true,
		Attributes:    make(map[string]interface{}),
	}
}

// injectPrincipalFromAuthorizedContext rebuilds the Subject from the verified
// AuthorizedContext and injects both the authenticated principal and the tenant
// into the context so downstream service-layer per-key authorization
// (auth.SubjectFromContext) and tenant isolation see the validated identity.
func injectPrincipalFromAuthorizedContext(ctx context.Context, authzCtx *platformtoken.AuthorizedContext) context.Context {
	subject := subjectFromAuthorizedContext(authzCtx)
	ctx = auth.SubjectIntoContext(ctx, subject)
	if authzCtx.TenantID != "" {
		ctx = iam.ContextWithTenant(ctx, authzCtx.TenantID)
	}
	return ctx
}
