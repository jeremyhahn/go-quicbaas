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
	"github.com/jeremyhahn/go-quicbaas/pkg/api/sealedwindow"
)

// AuthzInterceptorOption configures the gRPC authorization interceptors
// (UnaryAuthorizationInterceptor / StreamAuthorizationInterceptor). The same
// options apply to both, and to both listeners that share the chain (the TCP
// gRPC listener and the Unix-domain-socket listener).
type AuthzInterceptorOption func(*authzInterceptorConfig)

// authzInterceptorConfig carries the optional interceptor dependencies
// resolved from AuthzInterceptorOption values.
type authzInterceptorConfig struct {
	sealedBypass *sealedwindow.AuthBypass
}

// WithSealedWindowAuthBypass installs the shared sealed-window
// authentication-bypass predicate on the authorization interceptors.
//
// Composition contract (this interceptor runs AFTER the sealed-window
// interceptor in the lifecycle chain): while the barrier is sealed the
// sealed-window interceptor already denies every method off the canonical
// allowlist, so the only sealed traffic reaching this gate is allowlisted —
// and WHILE SEALED the allowlist IS the entire control surface (possession
// of unseal material is the authorization; a sealed IAM plane cannot mint or
// validate tokens). The predicate therefore admits sealed+allowlisted
// requests without authentication, audited with reason
// "sealed-window allowlist". Once the barrier is unsealed the predicate
// returns false for every method and full enforcement applies unchanged.
//
// A nil bypass leaves the gate fully fail-closed (identical to not passing
// the option), so RequireAuthz behavior without a sealed-window probe is
// byte-for-byte the prior posture.
func WithSealedWindowAuthBypass(bypass *sealedwindow.AuthBypass) AuthzInterceptorOption {
	return func(cfg *authzInterceptorConfig) {
		cfg.sealedBypass = bypass
	}
}

// resolveAuthzInterceptorOptions folds opts into the interceptor config.
func resolveAuthzInterceptorOptions(opts []AuthzInterceptorOption) authzInterceptorConfig {
	var cfg authzInterceptorConfig
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}
