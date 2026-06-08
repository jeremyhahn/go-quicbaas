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

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// GRPCIdentity is the authenticated principal the authorization interceptor
// resolves from the inbound bearer token. It carries exactly the fields the
// qrdb policy decision point and the per-key service layer consume.
type GRPCIdentity struct {
	// ID is the qrdb identity subject (the token's user id).
	ID string
	// Username is the human-readable subject name.
	Username string
	// TenantID is the principal's home tenant (the token's tenant claim).
	TenantID string
	// Roles are the qrdb-canonical role names from the token.
	Roles []string
}

// GRPCTokenValidator validates an inbound bearer token and returns the
// authenticated identity. It is satisfied by the server-layer OIDC validator
// (the same provider that validates REST / MCP / QUIC tokens). A validation
// failure returns a non-nil error and the interceptor responds Unauthenticated.
type GRPCTokenValidator interface {
	ValidateGRPCToken(ctx context.Context, token string) (*GRPCIdentity, error)
}

// GRPCAuthorizer is the narrow authorization contract the gRPC PEP depends on.
// It is satisfied by the server-layer bridge over pkg/auth/qrdbauthz.Adapter,
// which delegates the decision to qrdb's policy decision point. resource and
// action are qrdb-canonical; the join "resource:action" is a registered qrdb
// permission. Authorize returns nil to allow and a typed error to deny.
type GRPCAuthorizer interface {
	AuthorizeGRPC(ctx context.Context, id *GRPCIdentity, resource, action string) error
}

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
// point. On allow it returns the context enriched with the authenticated
// Subject; on deny it returns a gRPC status error.
//
// The decision is default-deny under RequireAuthz: a public method bypasses; a
// missing token, an unmapped method, an invalid token, or a qrdb denial all
// reject the call.
func authorizeGRPCRequest(ctx context.Context, fullMethod string, validator GRPCTokenValidator, authorizer GRPCAuthorizer) (context.Context, error) {
	if isPublicGRPCMethod(fullMethod) {
		return ctx, nil
	}

	resource, action, ok := grpcResourceAndAction(fullMethod)
	if !ok {
		return ctx, status.Error(codes.PermissionDenied, (&UnmappedMethodError{Method: fullMethod}).Error())
	}

	token := bearerFromContext(ctx)
	if token == "" {
		return ctx, status.Error(codes.Unauthenticated, (&MissingTokenError{Method: fullMethod}).Error())
	}

	id, err := validator.ValidateGRPCToken(ctx, token)
	if err != nil {
		return ctx, status.Error(codes.Unauthenticated, "invalid token")
	}

	if err := authorizer.AuthorizeGRPC(ctx, id, resource, action); err != nil {
		return ctx, status.Error(codes.PermissionDenied, "permission denied: "+err.Error())
	}

	// Inject the verified principal for service-layer per-key authorization.
	ctx = auth.SubjectIntoContext(ctx, interfaces.Subject{
		ID:            id.ID,
		Username:      id.Username,
		TenantID:      id.TenantID,
		Roles:         id.Roles,
		Authenticated: true,
	})
	return ctx, nil
}

// UnaryAuthorizationInterceptor enforces qrdb-backed authorization on every
// unary RPC. It is installed only when RequireAuthz=true; when authz is off this
// interceptor is never added to the chain, so behavior is unchanged. Both the
// TCP gRPC listener and the Unix-domain-socket gRPC listener share this chain,
// so a single installation covers both the gRPC and Unix transports.
func UnaryAuthorizationInterceptor(validator GRPCTokenValidator, authorizer GRPCAuthorizer) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		authedCtx, err := authorizeGRPCRequest(ctx, info.FullMethod, validator, authorizer)
		if err != nil {
			return nil, err
		}
		return handler(authedCtx, req)
	}
}

// StreamAuthorizationInterceptor is the streaming counterpart of
// UnaryAuthorizationInterceptor. It authorizes the stream open and wraps the
// stream so downstream handlers observe the authenticated context.
func StreamAuthorizationInterceptor(validator GRPCTokenValidator, authorizer GRPCAuthorizer) grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		authedCtx, err := authorizeGRPCRequest(ss.Context(), info.FullMethod, validator, authorizer)
		if err != nil {
			return err
		}
		if authedCtx != ss.Context() {
			ss = wrapServerStream(ss, authedCtx)
		}
		return handler(srv, ss)
	}
}
