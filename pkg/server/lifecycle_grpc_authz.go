//go:build iam

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

package server

import (
	"context"
	"errors"

	ggrpc "google.golang.org/grpc"

	grpcapi "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	"github.com/jeremyhahn/go-quicbaas/pkg/auth/qrdbauthz"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
)

// errGRPCOIDCProviderUnavailable indicates the IAM OIDC provider that signs and
// validates access tokens is not yet initialized. The gRPC authz interceptor
// treats this as a token-validation failure (Unauthenticated) rather than a
// server error.
var errGRPCOIDCProviderUnavailable = errors.New("oidc provider unavailable")

// grpcOIDCProviderGetter is the narrow accessor the gRPC authz wiring uses to
// reach the IAM OIDC provider that signs and validates every access token. It
// mirrors the REST authz getter so the same in-process provider validates
// tokens across REST, gRPC, QUIC, and MCP.
type grpcOIDCProviderGetter interface {
	GetOIDCProvider(ctx context.Context) (*iam.OIDCProvider, error)
}

// GRPCAuthzError wraps a failure that occurs while validating a gRPC bearer
// token. It carries the failing operation and the underlying cause.
type GRPCAuthzError struct {
	Op    string
	Cause error
}

func (e *GRPCAuthzError) Error() string {
	if e.Cause != nil {
		return "grpc authz: " + e.Op + ": " + e.Cause.Error()
	}
	return "grpc authz: " + e.Op
}

func (e *GRPCAuthzError) Unwrap() error { return e.Cause }

// grpcOIDCTokenValidator adapts the IAM OIDCProvider to the gRPC authorization
// interceptor's GRPCTokenValidator contract. It verifies the inbound bearer
// against the provider's signing key and projects the OIDC claims onto a
// *grpcapi.GRPCIdentity. It is the single gRPC token-validation seam and accepts
// the SAME OIDC tokens the daemon issues (client_credentials service accounts,
// hardware-attested PIV / WebAuthn logins).
type grpcOIDCTokenValidator struct {
	getter grpcOIDCProviderGetter
}

// Compile-time check that grpcOIDCTokenValidator satisfies the interceptor seam.
var _ grpcapi.GRPCTokenValidator = (*grpcOIDCTokenValidator)(nil)

// ValidateGRPCToken verifies the access token and returns the authenticated
// identity. Any verification failure surfaces as a typed error so the
// interceptor can respond Unauthenticated.
func (v *grpcOIDCTokenValidator) ValidateGRPCToken(ctx context.Context, token string) (*grpcapi.GRPCIdentity, error) {
	provider, err := v.getter.GetOIDCProvider(ctx)
	if err != nil {
		return nil, &GRPCAuthzError{Op: "get oidc provider", Cause: err}
	}
	if provider == nil {
		return nil, &GRPCAuthzError{Op: "get oidc provider", Cause: errGRPCOIDCProviderUnavailable}
	}

	claims, err := provider.ValidateAccessToken(ctx, token)
	if err != nil {
		return nil, &GRPCAuthzError{Op: "validate access token", Cause: err}
	}

	return &grpcapi.GRPCIdentity{
		ID:       claims.Subject,
		Username: claims.Name,
		TenantID: claims.TenantID,
		Roles:    claims.Roles,
	}, nil
}

// grpcQRDBAuthorizer adapts the qrdbauthz.Adapter to the gRPC interceptor's
// GRPCAuthorizer seam. It maps the interceptor's GRPCIdentity onto the adapter's
// Subject and delegates the decision to qrdb's policy decision point. When the
// identity carries no tenant claim it falls back to the cert-derived tenant on
// the context so the adapter's well-formed-identity invariant holds. This is the
// single gRPC PEP -> PDP bridge; it adds no policy logic of its own.
type grpcQRDBAuthorizer struct {
	adapter *qrdbauthz.Adapter
}

// Compile-time check that grpcQRDBAuthorizer satisfies the interceptor seam.
var _ grpcapi.GRPCAuthorizer = (*grpcQRDBAuthorizer)(nil)

// AuthorizeGRPC maps the gRPC identity onto the qrdbauthz subject and delegates
// to the qrdb-backed adapter.
func (a *grpcQRDBAuthorizer) AuthorizeGRPC(ctx context.Context, id *grpcapi.GRPCIdentity, resource, action string) error {
	tenantID := id.TenantID
	if tenantID == "" {
		tenantID = iam.TenantFromContext(ctx)
	}
	return a.adapter.Authorize(ctx, qrdbauthz.Subject{
		ID:       id.ID,
		TenantID: tenantID,
		Roles:    id.Roles,
	}, resource, action)
}

// grpcAuthorizationInterceptors builds the gRPC unary + stream authorization
// interceptors when RequireAuthz=true and an OIDC provider is available to
// validate tokens. It returns (nil, nil) otherwise, in which case the gRPC chain
// adds no authorization gate — preserving the pre-authz behavior exactly. The
// same chain backs the Unix-domain-socket gRPC listener, so this builder covers
// both the gRPC and Unix transports in one place.
func (s *Server) grpcAuthorizationInterceptors() (ggrpc.UnaryServerInterceptor, ggrpc.StreamServerInterceptor) {
	if s.config == nil || !s.config.Security.RequireAuthz {
		return nil, nil
	}

	getter, ok := s.quicBaaSService.(grpcOIDCProviderGetter)
	if !ok {
		s.logger.Debug("gRPC authz disabled: service does not expose an OIDC provider")
		return nil, nil
	}
	if provider, err := getter.GetOIDCProvider(context.Background()); err != nil || provider == nil {
		s.logger.Debug("gRPC authz disabled: OIDC provider not available")
		return nil, nil
	}

	// Build the single qrdb-backed authorizer adapter. It late-binds the qrdb
	// RBAC client (the node client is constructed during lifecycle, so it is
	// resolved per request via GetQRDBNodeClient().RBAC()). The ABAC overlay is
	// optional and left nil here.
	rbacResolver := func() qrdbtransport.RBACClient {
		node := s.GetQRDBNodeClient()
		if node == nil {
			return nil
		}
		return node.RBAC()
	}
	adapter, err := qrdbauthz.New(rbacResolver, nil)
	if err != nil {
		s.logger.WithError(err).Error("gRPC authz disabled: failed to build qrdb authorizer adapter")
		return nil, nil
	}

	validator := &grpcOIDCTokenValidator{getter: getter}
	authorizer := &grpcQRDBAuthorizer{adapter: adapter}

	return grpcapi.UnaryAuthorizationInterceptor(validator, authorizer),
		grpcapi.StreamAuthorizationInterceptor(validator, authorizer)
}
