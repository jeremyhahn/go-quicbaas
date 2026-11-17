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
	qbsealedwindow "github.com/jeremyhahn/go-quicbaas/pkg/api/sealedwindow"
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

// AuthzGateConstructionError is returned when RequireAuthz=true and the
// authorization gate for a transport cannot be constructed (no OIDC
// provider, or the platform-token Validator failed to build). Under the
// refuse-to-start posture this error ABORTS daemon startup: a transport is
// never served with a silently-missing authorization gate. Gate
// construction happens in the post-unseal boot phase, where the OIDC /
// validator dependencies are guaranteed by ordering; while sealed, the
// sealed-window allowlist gates are the control and no authz gate exists.
type AuthzGateConstructionError struct {
	Transport string
	Op        string
	Cause     error
}

func (e *AuthzGateConstructionError) Error() string {
	msg := e.Transport + " authz gate construction: " + e.Op +
		" (RequireAuthz=true refuses to start without an enforcement gate)"
	if e.Cause != nil {
		return msg + ": " + e.Cause.Error()
	}
	return msg
}

func (e *AuthzGateConstructionError) Unwrap() error { return e.Cause }

// grpcAuthorizationInterceptors builds the gRPC unary + stream authorization
// interceptors when RequireAuthz=true. Gate construction is FAIL-CLOSED:
// when RequireAuthz=true and the OIDC provider or the platform-token
// Validator cannot be built, a typed *AuthzGateConstructionError is
// returned and daemon startup ABORTS (refuse-to-start posture). It never
// returns (nil, nil, nil) under RequireAuthz. With RequireAuthz=false it
// returns (nil, nil, nil) and the gRPC chain adds no authorization gate —
// preserving the pre-authz behavior exactly. The same chain backs the
// Unix-domain-socket gRPC listener, so this builder covers both the gRPC
// and Unix transports in one place.
//
// Construction runs in the post-unseal boot phase (startGRPCServer runs
// after startService completes IAM init), so the OIDC / validator
// dependencies are guaranteed by ordering; while sealed, the sealed-window
// allowlist interceptors are the control and no authz gate exists.
//
// The interceptors enforce authorization through the SAME central platform-token
// Validator the REST gate uses (buildPlatformValidator): one Authorize call per
// request does offline signature / iss / aud / exp verification, the offline
// scp -> permission decision, and the per-tenant liveness/revocation check. This
// REPLACES the prior online qrdb role-PDP two-step on the gRPC transport.
func (s *Server) grpcAuthorizationInterceptors() (ggrpc.UnaryServerInterceptor, ggrpc.StreamServerInterceptor, error) {
	if s.config == nil || !s.config.Security.RequireAuthz {
		return nil, nil, nil
	}

	getter, ok := s.quicBaaSService.(grpcOIDCProviderGetter)
	if !ok {
		return nil, nil, &AuthzGateConstructionError{
			Transport: "grpc",
			Op:        "service does not expose an OIDC provider",
		}
	}
	if provider, err := getter.GetOIDCProvider(context.Background()); err != nil || provider == nil {
		if err == nil {
			err = errGRPCOIDCProviderUnavailable
		}
		return nil, nil, &AuthzGateConstructionError{
			Transport: "grpc",
			Op:        "resolve OIDC provider",
			Cause:     err,
		}
	}

	// Build the single central platform-token Validator and install the
	// validator-backed enforcement gate. One Validator call per request does
	// offline signature / iss / aud / exp verification, the offline scp ->
	// permission decision, and the per-tenant liveness/revocation check.
	validator, err := s.buildPlatformValidator(context.Background())
	if err != nil {
		return nil, nil, &AuthzGateConstructionError{
			Transport: "grpc",
			Op:        "build platform-token validator",
			Cause:     err,
		}
	}

	// Sealed-window authentication bypass (owner-approved contract): while
	// the barrier is sealed — a post-boot RE-SEAL, since this builder runs in
	// the post-unseal boot phase — the sealed-window allowlist IS the entire
	// control surface. The sealed-window interceptors ahead of this gate
	// already deny every off-allowlist method, and the gate itself admits the
	// allowlisted unseal-flow verbs WITHOUT authentication (a sealed IAM
	// plane cannot mint tokens), audited with reason "sealed-window
	// allowlist". Unsealed, the predicate is inert and enforcement is
	// unchanged. Same single probe instance as every other transport gate.
	sealedBypass := qbsealedwindow.NewAuthBypass(
		s.SealedWindowProbe(), s.OperationalProbe(),
		qbsealedwindow.NewLoggerAuditSink(s.logger))

	return grpcapi.UnaryTenantAdmission(grpcapi.UnaryAuthorizationInterceptor(validator, grpcapi.WithSealedWindowAuthBypass(sealedBypass)), s.tenantUnsealHooks.Admit),
		grpcapi.StreamTenantAdmission(grpcapi.StreamAuthorizationInterceptor(validator, grpcapi.WithSealedWindowAuthBypass(sealedBypass)), s.tenantUnsealHooks.Admit), nil
}
