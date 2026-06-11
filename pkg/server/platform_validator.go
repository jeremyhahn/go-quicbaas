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

	"github.com/jeremyhahn/go-quicbaas/pkg/auth/liveness"
	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam/dpop"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/platformtoken"
)

// defaultLivenessCacheTTL bounds how long a positive hybrid-mode liveness result
// is cached before the next request live-checks again. It is the fallback used
// when the deployment config leaves security.liveness_cache_ttl unset (zero).
const defaultLivenessCacheTTL = config.DefaultLivenessCacheTTL

// buildPlatformValidator constructs the single, long-lived platform-token
// Validator the REST and gRPC (and Unix-over-gRPC) authorization gates call per
// request. It wires the IN-PROCESS IAM OIDC provider as both the live key source
// (GetJWKS, covering key rotation and newly added tenant keys without an HTTP
// fetch) and the liveness backend (TokenLiveness -> revocation + subject
// disablement).
//
// The builder is transport-agnostic (no rest/grpc tag): the same Validator
// backs every authenticated transport, so each transport's enforcement gate
// constructs it from this one place rather than re-deriving the wiring.
//
// Audience convention (AS==RS): quicbaas validates the tokens it itself issues,
// so the required audience equals the issuer. iss and aud are therefore both the
// provider's issuer; aud is NOT relaxed or skipped.
//
// The mode resolver is configurable. For each tenant it consults the tenant's
// TenantSettings.AuthzMode override (via the provider) and falls back to the
// deployment-level security.authz_mode default. The fallback is fail-SAFE: when
// the per-tenant lookup errors (unknown tenant, unavailable tenant manager) the
// resolver returns the deployment default rather than panicking. The liveness
// cache TTL is sourced from security.liveness_cache_ttl. A single
// MemoryReplayCache backs DPoP proof-replay rejection for the validator's
// lifetime.
func (s *Server) buildPlatformValidator(ctx context.Context) (*platformtoken.Validator, error) {
	getter, ok := s.quicBaaSService.(grpcOIDCProviderGetter)
	if !ok {
		return nil, &GRPCAuthzError{Op: "build platform validator", Cause: errGRPCOIDCProviderUnavailable}
	}
	provider, err := getter.GetOIDCProvider(ctx)
	if err != nil {
		return nil, &GRPCAuthzError{Op: "get oidc provider", Cause: err}
	}
	if provider == nil {
		return nil, &GRPCAuthzError{Op: "get oidc provider", Cause: errGRPCOIDCProviderUnavailable}
	}

	issuer := provider.Issuer()

	// Resolve the deployment-level default once. The config layer validated the
	// authz_mode string at load time, so an error here only happens if the value
	// was tampered with after load; fail SAFE to hybrid in that case.
	deploymentDefault := platformtoken.AuthzModeHybrid
	if s.config != nil {
		if mode, perr := s.config.Security.EffectiveAuthzMode(); perr == nil {
			deploymentDefault = mode
		}
	}

	cacheTTL := defaultLivenessCacheTTL
	if s.config != nil {
		cacheTTL = s.config.Security.EffectiveLivenessCacheTTL()
	}

	validator, err := platformtoken.NewValidator(platformtoken.Options{
		Issuer:           issuer,
		Audience:         issuer,
		KeySetFunc:       provider.GetJWKS,
		LivenessChecker:  liveness.NewChecker(provider),
		ModeResolver:     newTenantModeResolver(provider, deploymentDefault),
		LivenessCacheTTL: cacheTTL,
		// A nil dpop clock defaults to the system wall clock, matching how the
		// OIDC provider constructs its own replay cache.
		ReplayCache: dpop.NewMemoryReplayCache(nil),
	})
	if err != nil {
		return nil, &GRPCAuthzError{Op: "construct platform validator", Cause: err}
	}
	return validator, nil
}

// tenantAuthzModeResolver is the narrow accessor the per-tenant mode resolver
// needs from the OIDC provider. It returns the tenant's AuthzMode override
// (empty when unset) or a lookup error.
type tenantAuthzModeResolver interface {
	TenantAuthzMode(ctx context.Context, tenantID string) (string, error)
}

// newTenantModeResolver returns a platformtoken.ModeResolver that resolves a
// tenant's authorization mode from its TenantSettings.AuthzMode override and
// falls back to deploymentDefault when the override is empty/unset, names an
// unknown mode, or the lookup fails. The fallback is fail-SAFE: a lookup error
// never panics — it yields the deployment default so authorization continues to
// enforce the operator's baseline posture.
func newTenantModeResolver(provider tenantAuthzModeResolver, deploymentDefault platformtoken.AuthzMode) platformtoken.ModeResolver {
	return func(tenantID string) platformtoken.AuthzMode {
		override, err := provider.TenantAuthzMode(context.Background(), tenantID)
		if err != nil || override == "" {
			return deploymentDefault
		}
		mode, perr := config.ParseAuthzMode(override)
		if perr != nil {
			return deploymentDefault
		}
		return mode
	}
}

// _ keeps the iam import referenced even if the resolver wiring is ever pruned;
// the OIDCProvider concrete type satisfies tenantAuthzModeResolver.
var _ tenantAuthzModeResolver = (*iam.OIDCProvider)(nil)
