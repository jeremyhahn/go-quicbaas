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

package grpc

import (
	"context"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/jeremyhahn/go-quicbaas/pkg/api/sealedwindow"
	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/platformtoken"
)

// UnmappedMethodError is returned by the gRPC PEP when a protected method has no
// qrdb-canonical permission mapping. Under RequireAuthz the gate is default-deny,
// so an unmapped protected surface is denied rather than admitted.
type UnmappedMethodError struct {
	Method string
}

func (e *UnmappedMethodError) Error() string {
	return "no qrdb permission mapping for gRPC method: " + e.Method
}

// MissingTokenError is returned when a protected gRPC method is invoked without
// an inbound bearer token.
type MissingTokenError struct {
	Method string
}

func (e *MissingTokenError) Error() string {
	return "authentication required for gRPC method: " + e.Method
}

// bearerFromContext extracts the bearer token from the inbound "authorization"
// metadata ("Bearer <token>"). It returns the empty string when no token is
// present.
func bearerFromContext(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	values := md.Get("authorization")
	if len(values) == 0 {
		return ""
	}
	raw := values[0]
	if len(raw) >= 7 && strings.EqualFold(raw[:7], "Bearer ") {
		return strings.TrimSpace(raw[7:])
	}
	return strings.TrimSpace(raw)
}

// authorizeGRPCRequest is the shared decision path for the unary and stream
// interceptors. It is the single gRPC (and Unix-over-gRPC) policy enforcement
// point, backed by the central platform-token Validator: ONE Authorize call does
// offline signature / iss / aud / exp verification, DPoP sender-constraint for
// cnf.jkt-bound tokens, the offline scp -> permission decision, and the
// per-tenant liveness/revocation check. This REPLACES the prior online qrdb
// role-PDP two-step (validate token + authorize).
//
// On allow it returns the context enriched with the authenticated Subject
// reconstructed from the verified AuthorizedContext; on deny it returns a gRPC
// status error mapped by validatorGRPCStatus.
//
// The decision is default-deny under RequireAuthz: a public method bypasses; a
// missing token, an unmapped method, an invalid/inactive token, or a missing
// scope all reject the call. The gRPC metadata path carries no DPoP proof, so
// the empty dpopProof / htm / htu args are passed deliberately (see
// GRPCValidatorAuthorizer).
func authorizeGRPCRequest(ctx context.Context, fullMethod string, authorizer GRPCValidatorAuthorizer, sealedBypass *sealedwindow.AuthBypass) (context.Context, error) {
	if isPublicGRPCMethod(fullMethod) {
		return ctx, nil
	}

	// Sealed-window authentication bypass. WHILE the barrier is SEALED the
	// sealed-window allowlist IS the entire control surface: possession of
	// unseal material is the authorization, because a sealed IAM plane
	// cannot mint or validate tokens. The sealed-window interceptor ahead
	// of this gate already rejected every non-allowlisted method, so the
	// predicate only ever admits the allowlisted unseal-flow verbs — and it
	// emits an audit event (reason "sealed-window allowlist") on every
	// admit. Unsealed, the predicate is always false and enforcement below
	// is unchanged; the handlers keep their own credential checks (officer
	// cert + nonce verification) on the share-claim verbs.
	if sealedBypass.BypassesGRPC(fullMethod) {
		return ctx, nil
	}

	token := bearerFromContext(ctx)
	if token == "" {
		return ctx, status.Error(codes.Unauthenticated, (&MissingTokenError{Method: fullMethod}).Error())
	}

	// IdentityGated verbs (the qrdb barrier share-claim methods): any
	// AUTHENTICATED principal is admitted — the validator's scope-free
	// Validate path performs the full offline token validation and liveness
	// check without demanding a registered scp permission, and the HANDLER
	// enforces the real credential (registered-officer lookup, mTLS SPKI
	// thumbprint match, nonce signature verification). Exact parity with
	// qrdb's permission.IdentityGated sentinel. Fail closed: an authorizer
	// without the authenticated seam denies these verbs outright, and every
	// validation failure denies via validatorGRPCStatus like any other
	// decision.
	if qrdbIdentityGatedMethods[fullMethod] {
		authenticator, ok := authorizer.(GRPCValidatorAuthenticator)
		if !ok {
			return ctx, status.Error(codes.PermissionDenied, "permission denied")
		}
		authzCtx, err := authenticator.Validate(ctx, token, "", "", "")
		if err != nil {
			return ctx, validatorGRPCStatus(err)
		}
		return contextWithAuthorizedSubject(ctx, authzCtx), nil
	}

	resource, action, ok := grpcResourceAndAction(fullMethod)
	if !ok {
		return ctx, status.Error(codes.PermissionDenied, (&UnmappedMethodError{Method: fullMethod}).Error())
	}

	authzCtx, err := authorizer.Authorize(ctx, token, resource+":"+action, "", "", "")
	if err != nil {
		return ctx, validatorGRPCStatus(err)
	}

	return contextWithAuthorizedSubject(ctx, authzCtx), nil
}

// contextWithAuthorizedSubject reconstructs the principal from the verified
// AuthorizedContext so the service-layer per-key authorization
// (auth.SubjectFromContext) sees an authenticated subject, and injects the
// tenant context key so the service / storage layer resolves row-level
// isolation from iam.TenantFromContext (the canonical RLS source), matching
// the REST and MCP validator-allow paths. Shared by the scoped Authorize
// path and the IdentityGated Validate path so both admit decisions produce
// identical downstream identity context.
func contextWithAuthorizedSubject(ctx context.Context, authzCtx *platformtoken.AuthorizedContext) context.Context {
	// The AuthorizedContext is the canonical post-validation identity;
	// Username carries the token name claim so principal matching by
	// username (in addition to ID) is preserved.
	ctx = auth.SubjectIntoContext(ctx, interfaces.Subject{
		ID:            authzCtx.Subject,
		Username:      authzCtx.Username,
		TenantID:      authzCtx.TenantID,
		Roles:         authzCtx.Roles,
		Authenticated: true,
		Attributes:    make(map[string]interface{}),
	})

	// Only set the tenant key when the token carries a tenant; an empty
	// TenantID leaves single-tenant behavior unchanged.
	if authzCtx.TenantID != "" {
		ctx = tenantctx.WithTenant(ctx, authzCtx.TenantID)
	}
	return ctx
}

// UnaryAuthorizationInterceptor enforces platform-token authorization on every
// unary RPC. It is installed only when RequireAuthz=true; when authz is off this
// interceptor is never added to the chain, so behavior is unchanged. Both the
// TCP gRPC listener and the Unix-domain-socket gRPC listener share this chain,
// so a single installation covers both the gRPC and Unix transports.
func UnaryAuthorizationInterceptor(authorizer GRPCValidatorAuthorizer, opts ...AuthzInterceptorOption) grpc.UnaryServerInterceptor {
	cfg := resolveAuthzInterceptorOptions(opts)
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		authedCtx, err := authorizeGRPCRequest(ctx, info.FullMethod, authorizer, cfg.sealedBypass)
		if err != nil {
			return nil, err
		}
		return handler(authedCtx, req)
	}
}

// StreamAuthorizationInterceptor is the streaming counterpart of
// UnaryAuthorizationInterceptor. It authorizes the stream open and wraps the
// stream so downstream handlers observe the authenticated context.
func StreamAuthorizationInterceptor(authorizer GRPCValidatorAuthorizer, opts ...AuthzInterceptorOption) grpc.StreamServerInterceptor {
	cfg := resolveAuthzInterceptorOptions(opts)
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		authedCtx, err := authorizeGRPCRequest(ss.Context(), info.FullMethod, authorizer, cfg.sealedBypass)
		if err != nil {
			return err
		}
		if authedCtx != ss.Context() {
			ss = wrapServerStream(ss, authedCtx)
		}
		return handler(srv, ss)
	}
}
