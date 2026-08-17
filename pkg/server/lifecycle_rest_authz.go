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

package server

import (
	"context"
	"errors"

	restapi "github.com/jeremyhahn/go-quicbaas/pkg/api/rest"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// ErrOIDCProviderUnavailable indicates the IAM OIDC provider that signs and
// validates access tokens is not yet initialized.
var ErrOIDCProviderUnavailable = errors.New("oidc provider unavailable")

// oidcProviderGetter is the narrow accessor the REST authz wiring uses to reach
// the IAM OIDC provider that signs and validates every access token. It mirrors
// the type assertion the REST IAM handlers already perform on the service facade
// (see pkg/api/rest/iam_middleware.go), so the same in-process provider validates
// tokens at the module's local platform-token fallback and at the per-handler IAM
// middleware.
type oidcProviderGetter interface {
	GetOIDCProvider(ctx context.Context) (*iam.OIDCProvider, error)
}

// configureRESTAuthz wires the platform-token Validator the module's qrdb
// ModuleAuthFallback (pkg/apimodule module_http.go AuthenticateHTTP) depends
// on: a caller that reaches a unified-API-server module route WITHOUT a
// qrdb-authenticated identity — a quicbaas platform-token (OIDC JWT/DPoP)
// caller, which qrdb's own mTLS chain does not understand — is authenticated
// by AuthenticateHTTP and then authorized by qrdb's ONE RBAC authorizer
// (store-resolved roles for a caller with a qrdb user record, additively
// unioned with the token's ScopePermissions otherwise — go-qrdb
// v0.5.6-alpha+) using the SAME central platform-token Validator every
// other transport shares (buildPlatformValidator, platform_validator.go).
//
// The fallback is gated by SecurityConfig.RequireAuthz, mirroring the
// identical selector on the gRPC/QUIC/MCP transports
// (grpcAuthorizationInterceptors, lifecycle_grpc_authz.go): RequireAuthz=false
// installs no fallback at all, so a request without a qrdb identity passes
// through anonymously — the historical "anonymous data-plane access" default
// — and RequireAuthz=true installs the fallback fail-closed: an unavailable
// OIDC provider or a Validator construction failure is a typed
// *AuthzGateConstructionError that ABORTS daemon startup (refuse-to-start
// posture), so REST is never served with a silently-missing enforcement
// gate.
func (s *Server) configureRESTAuthz(restServer *restapi.Server) error {
	if s.config == nil || !s.config.Security.RequireAuthz {
		return nil
	}

	getter, ok := s.quicBaaSService.(oidcProviderGetter)
	if !ok {
		return &AuthzGateConstructionError{
			Transport: "rest",
			Op:        "service does not expose an OIDC provider",
		}
	}
	if provider, err := getter.GetOIDCProvider(context.Background()); err != nil || provider == nil {
		if err == nil {
			err = ErrOIDCProviderUnavailable
		}
		return &AuthzGateConstructionError{
			Transport: "rest",
			Op:        "resolve OIDC provider",
			Cause:     err,
		}
	}

	// Build the single central platform-token Validator and install it as the
	// module's local fallback authorizer. One Validator call per request does
	// offline signature / iss / aud / exp verification, DPoP sender-constraint
	// (RFC 9449) for cnf.jkt-bound tokens, the offline scp -> permission
	// decision, and the per-tenant liveness/revocation check.
	platformValidator, err := s.buildPlatformValidator(context.Background())
	if err != nil {
		return &AuthzGateConstructionError{
			Transport: "rest",
			Op:        "build platform-token validator",
			Cause:     err,
		}
	}
	restServer.SetModuleTokenAuthorizer(platformValidator)
	s.logger.Info("REST module local platform-token fallback installed (RequireAuthz=true): a caller reaching " +
		"the unified-server module surface without a qrdb identity is authenticated and authorized against " +
		"each route's frozen permission")
	return nil
}
