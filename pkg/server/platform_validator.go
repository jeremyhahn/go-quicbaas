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
	"strings"
	"sync"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
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

var _ qrdbsdk.UnobservableMethodAuthenticator = (*platformBearerAuthenticator)(nil)

type platformBearerAuthenticator struct {
	server *Server

	mu        sync.Mutex
	validator *platformtoken.Validator
}

type PlatformBearerAuthUnavailableError struct {
	Cause error
}

func (e *PlatformBearerAuthUnavailableError) Error() string {
	if e.Cause != nil {
		return "platform bearer authenticator is unavailable: " + e.Cause.Error()
	}
	return "platform bearer authenticator is unavailable"
}

func (e *PlatformBearerAuthUnavailableError) Unwrap() error {
	return e.Cause
}

type PlatformBearerMissingError struct{}

func (e *PlatformBearerMissingError) Error() string {
	return "platform bearer authorization is missing"
}

type PlatformBearerInvalidHeaderError struct{}

func (e *PlatformBearerInvalidHeaderError) Error() string {
	return "platform bearer authorization must be 'Bearer <token>'"
}

type PlatformBearerTenantRequiredError struct{}

func (e *PlatformBearerTenantRequiredError) Error() string {
	return "platform bearer token tenant_id is required"
}

type PlatformBearerCertificateRequiredError struct{}

func (e *PlatformBearerCertificateRequiredError) Error() string {
	return "platform bearer token requires a client certificate"
}

func platformBearerHasPeerCertificate(ctx context.Context) bool {
	state := qrdbsdk.TLSStateFromContext(ctx)
	return state != nil && len(state.PeerCertificates) > 0 && state.PeerCertificates[0] != nil
}

func enforcePlatformBearerCertificateBinding(ctx context.Context, confirmation *platformtoken.Confirmation) error {
	if confirmation == nil || strings.TrimSpace(confirmation.X5TS256) == "" {
		return nil
	}
	if platformBearerHasPeerCertificate(ctx) {
		return nil
	}
	return &PlatformBearerCertificateRequiredError{}
}

func newPlatformBearerAuthenticator(s *Server) qrdbsdk.UnobservableMethodAuthenticator {
	return &platformBearerAuthenticator{server: s}
}

func (a *platformBearerAuthenticator) Name() string {
	return "platform-bearer"
}

func (a *platformBearerAuthenticator) MethodUnobservable() bool {
	return true
}

func (a *platformBearerAuthenticator) Authenticate(ctx context.Context) (*qrdbsdk.Identity, error) {
	return a.authenticateAuthorization(ctx, qrdbsdk.HTTPAuthorizationFromContext(ctx))
}

func (a *platformBearerAuthenticator) authenticateAuthorization(ctx context.Context, authorization string) (*qrdbsdk.Identity, error) {
	// Known limit (go-qrdb#645): qrdb's node authenticator chain runs
	// built-in mTLS before embedder authenticators and stops on the first
	// success. If a peer certificate is present, decline so qrdb mTLS alone
	// decides the request. Until #645 lands, cnf.x5t#S256 platform tokens
	// cannot authenticate on qrdb-native routes.
	if platformBearerHasPeerCertificate(ctx) {
		return nil, nil
	}
	raw := strings.TrimSpace(authorization)
	if raw == "" {
		return nil, &PlatformBearerMissingError{}
	}
	parts := strings.Fields(raw)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || strings.TrimSpace(parts[1]) == "" {
		return nil, &PlatformBearerInvalidHeaderError{}
	}
	validator, err := a.resolveValidator(ctx)
	if err != nil {
		return nil, err
	}
	// Known limit (go-qrdb#646): qrdb's request context does not yet expose
	// DPoP proof, HTTP method, or canonical URI values to embedder
	// authenticators. Empty values make cnf.jkt tokens fail on qrdb-native
	// routes; module-owned REST routes use Server.AuthenticateHTTP and are
	// unaffected.
	ac, err := validator.Validate(ctx, parts[1], "", "", "")
	if err != nil {
		return nil, err
	}
	if err := enforcePlatformBearerCertificateBinding(ctx, ac.Confirmation); err != nil {
		return nil, err
	}
	if strings.TrimSpace(ac.TenantID) == "" {
		return nil, &PlatformBearerTenantRequiredError{}
	}
	// sub is the login name; the account is the verified uid claim.
	return auth.MarkTokenSubject(&qrdbsdk.Identity{
		Subject:          ac.Subject,
		TenantID:         ac.TenantID,
		ScopePermissions: ac.Scopes,
	}, ac.UserID), nil
}

func (a *platformBearerAuthenticator) resolveValidator(ctx context.Context) (*platformtoken.Validator, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.validator != nil {
		return a.validator, nil
	}
	if a.server == nil {
		return nil, &PlatformBearerAuthUnavailableError{}
	}
	validator, err := a.server.buildPlatformValidator(ctx)
	if err != nil {
		return nil, &PlatformBearerAuthUnavailableError{Cause: err}
	}
	a.validator = validator
	return validator, nil
}

func (s *Server) platformOIDCProviderGetter(_ context.Context) (grpcOIDCProviderGetter, error) {
	if s == nil {
		return nil, &GRPCAuthzError{Op: "build platform validator", Cause: errGRPCOIDCProviderUnavailable}
	}
	getter, ok := s.quicBaaSServiceRef().(grpcOIDCProviderGetter)
	if !ok {
		return nil, &GRPCAuthzError{Op: "build platform validator", Cause: errGRPCOIDCProviderUnavailable}
	}
	return getter, nil
}

// buildPlatformValidator constructs a platform-token Validator for the qrdb
// authenticator and the REST module fallback compatibility path. It wires the
// IN-PROCESS IAM OIDC provider as both the live key source
// (GetJWKS, covering key rotation and newly added tenant keys without an HTTP
// fetch) and the liveness backend (TokenLiveness -> revocation + subject
// disablement).
//
// The builder is transport-agnostic (no rest/grpc tag): qrdb's central
// authenticator calls the resulting validator for every transport that projects
// Authorization into the request context.
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
	getter, err := s.platformOIDCProviderGetter(ctx)
	if err != nil {
		return nil, err
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
