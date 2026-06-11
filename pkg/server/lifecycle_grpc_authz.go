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
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// errGRPCOIDCProviderUnavailable indicates the IAM OIDC provider that signs and
// validates access tokens is not yet initialized. The gRPC authz path treats
// this as a token-validation failure rather than a server error.
var errGRPCOIDCProviderUnavailable = errors.New("oidc provider unavailable")

// grpcOIDCProviderGetter is the narrow accessor the gRPC authz wiring uses to
// reach the IAM OIDC provider that signs and validates every access token. It
// mirrors the REST authz getter so the same in-process provider validates
// tokens across REST, gRPC, QUIC, and MCP. buildPlatformValidator uses it to
// source the live JWKS and liveness backend for the platform-token Validator.
type grpcOIDCProviderGetter interface {
	GetOIDCProvider(ctx context.Context) (*iam.OIDCProvider, error)
}

// GRPCAuthzError wraps a failure that occurs while building or wiring the gRPC
// authorization gate (e.g. the OIDC provider that backs the platform-token
// Validator is unavailable). It carries the failing operation and the underlying
// cause.
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

// grpcAuthorizationInterceptors builds the gRPC unary + stream authorization
// interceptors when RequireAuthz=true and an OIDC provider is available to back
// the platform-token Validator. It returns (nil, nil) otherwise, in which case
// the gRPC chain adds no authorization gate — preserving the pre-authz behavior
// exactly. The same chain backs the Unix-domain-socket gRPC listener, so this
// builder covers both the gRPC and Unix transports in one place.
//
// The interceptors enforce authorization through the SAME central platform-token
// Validator the REST gate uses (buildPlatformValidator): one Authorize call per
// request does offline signature / iss / aud / exp verification, the offline
// scp -> permission decision, and the per-tenant liveness/revocation check. This
// REPLACES the prior online qrdb role-PDP two-step on the gRPC transport.
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

	// Build the single central platform-token Validator and install the
	// validator-backed enforcement gate. One Validator call per request does
	// offline signature / iss / aud / exp verification, the offline scp ->
	// permission decision, and the per-tenant liveness/revocation check.
	validator, err := s.buildPlatformValidator(context.Background())
	if err != nil {
		s.logger.WithError(err).Error("gRPC authz disabled: failed to build platform-token validator")
		return nil, nil
	}

	return grpcapi.UnaryAuthorizationInterceptor(validator),
		grpcapi.StreamAuthorizationInterceptor(validator)
}
