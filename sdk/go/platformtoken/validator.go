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

// Package platformtoken is the reusable validator downstream services consume
// to verify QuicBaaS platform access tokens (the single-issuer, ES256,
// JWKS-published, DPoP sender-constrained contract). It mirrors the identity
// token contract: verify the token signature against the issuer JWKS by kid,
// check iss/aud/exp, optionally verify a presented DPoP proof against the
// token's cnf.jkt binding (RFC 9449), and map scp -> RBAC / tenant_id -> RLS.
//
// Downstream services import this package directly; the DPoP verification
// reuses the canonical pkg/iam/dpop implementation so issuer and validator
// share one code path.
package platformtoken

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/golang-jwt/jwt/v5"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam/dpop"
)

// Confirmation mirrors the RFC 7800 cnf claim. For DPoP it carries jkt.
type Confirmation struct {
	// Jkt is the RFC 9449 DPoP key thumbprint (base64url SHA-256 of the JWK).
	Jkt string `json:"jkt,omitempty"`
	// X5TS256 is the RFC 8705 mTLS client-certificate SHA-256 thumbprint.
	X5TS256 string `json:"x5t#S256,omitempty"`
}

// Claims are the platform access-token claims a downstream service consumes.
// Unknown claims are tolerated so the issuer can add optional claims without a
// validator change (per the contract's versioning rule).
type Claims struct {
	jwt.RegisteredClaims
	// TenantID sets the downstream row-level-security tenant.
	TenantID string `json:"tenant_id,omitempty"`
	// SCP is the array-form scope list mapped to RBAC.
	SCP []string `json:"scp,omitempty"`
	// Scope is the space-separated scope string (OIDC compat); SCP takes
	// precedence when both are present.
	Scope string `json:"scope,omitempty"`
	// Roles is optional supplemental RBAC input.
	Roles []string `json:"roles,omitempty"`
	// Confirmation is the cnf sender-constraint binding (DPoP jkt / mTLS x5t).
	Confirmation *Confirmation `json:"cnf,omitempty"`
}

// scopes returns the authoritative scope set: SCP if present, else the
// space-separated Scope split into a slice.
func (c *Claims) scopes() []string {
	if len(c.SCP) > 0 {
		return c.SCP
	}
	if strings.TrimSpace(c.Scope) == "" {
		return nil
	}
	return strings.Fields(c.Scope)
}

// Options configures a platform-token validation.
type Options struct {
	// Issuer is the single trusted token issuer (iss must equal this).
	Issuer string
	// Audience is this service's resource identifier (e.g. "api://app"); the
	// token aud must contain it.
	Audience string
	// JWKS, when set, is an in-memory key set used directly (no HTTP fetch).
	JWKS *jose.JSONWebKeySet
	// JWKSURL, when set (and JWKS is nil), is the issuer JWKS endpoint the
	// validator fetches and caches by kid.
	JWKSURL string
	// HTTPClient overrides the client used for JWKS fetches. Inject a test
	// server client to override TLS trust without touching production code.
	HTTPClient *http.Client
	// CacheTTL bounds how long fetched JWKS keys are cached. Zero => 1 hour.
	CacheTTL time.Duration
	// Clock overrides the time source used for exp checks and JWKS cache TTL.
	// Nil uses the system clock. Injected for deterministic tests.
	Clock func() time.Time
}

// Validator verifies platform access tokens for one trusted issuer and one
// service audience. It is safe for concurrent use.
type Validator struct {
	issuer   string
	audience string
	keys     keySource
	nowFunc  func() time.Time
	parser   *jwt.Parser
}

// NewValidator constructs a Validator. It requires an issuer, an audience, and
// exactly one key source (JWKS or JWKSURL).
func NewValidator(opts Options) (*Validator, error) {
	if strings.TrimSpace(opts.Issuer) == "" {
		return nil, &ConfigError{Detail: "issuer is required"}
	}
	if strings.TrimSpace(opts.Audience) == "" {
		return nil, &ConfigError{Detail: "audience is required"}
	}
	if opts.JWKS == nil && strings.TrimSpace(opts.JWKSURL) == "" {
		return nil, &ConfigError{Detail: "one of JWKS or JWKSURL is required"}
	}

	nowFunc := opts.Clock
	if nowFunc == nil {
		nowFunc = time.Now
	}

	var src keySource
	if opts.JWKS != nil {
		s, err := newStaticKeySource(opts.JWKS)
		if err != nil {
			return nil, err
		}
		src = s
	} else {
		src = newRemoteKeySource(opts.JWKSURL, opts.HTTPClient, opts.CacheTTL, nowFunc)
	}

	// Restrict to asymmetric algorithms; HS* and none are never accepted.
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{"ES256", "ES384", "ES512", "RS256", "RS384", "RS512", "PS256", "PS384", "PS512", "EdDSA"}),
		jwt.WithTimeFunc(nowFunc),
	)

	return &Validator{
		issuer:   opts.Issuer,
		audience: opts.Audience,
		keys:     src,
		nowFunc:  nowFunc,
		parser:   parser,
	}, nil
}

// ValidatePlatformToken verifies a platform access token: signature against the
// issuer JWKS (selected by kid), iss equals the trusted issuer, aud contains
// this service's audience, and exp has not passed. It returns the parsed claims
// on success and a typed error otherwise (callers map these to 401).
//
// This does NOT verify DPoP. Use ValidateWithDPoP on sender-constrained paths.
func (v *Validator) ValidatePlatformToken(ctx context.Context, tokenString string) (*Claims, error) {
	if strings.TrimSpace(tokenString) == "" {
		return nil, &InvalidTokenError{Detail: "empty token"}
	}

	claims := &Claims{}
	token, err := v.parser.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (interface{}, error) {
		kid, _ := t.Header["kid"].(string)
		if kid == "" {
			return nil, &InvalidTokenError{Detail: "missing kid header"}
		}
		key, kerr := v.keys.publicKey(ctx, kid)
		if kerr != nil {
			return nil, kerr
		}
		return key, nil
	})
	if err != nil {
		switch {
		case errors.Is(err, jwt.ErrTokenExpired):
			return nil, &ExpiredTokenError{}
		default:
			return nil, classifyParseError(err)
		}
	}
	if !token.Valid {
		return nil, &InvalidTokenError{Detail: "token failed validation"}
	}

	if claims.Issuer != v.issuer {
		return nil, &InvalidIssuerError{Expected: v.issuer, Actual: claims.Issuer}
	}
	if !audienceContains(claims.Audience, v.audience) {
		return nil, &InvalidAudienceError{Expected: v.audience}
	}

	return claims, nil
}

// ValidateWithDPoP verifies the access token (as ValidatePlatformToken) and
// then verifies the presented DPoP proof against the token's cnf.jkt binding
// (RFC 9449): the proof signature, htm/htu, iat skew, jti replay, and that the
// SHA-256 thumbprint of the proof key equals cnf.jkt. A token without cnf.jkt
// is rejected on this path (a bearer cannot be replayed with an attacker's DPoP
// key and accepted). The replayCache must be supplied by the caller and is
// typically a long-lived per-service instance.
func (v *Validator) ValidateWithDPoP(ctx context.Context, tokenString, dpopProof, htm, htu string, replayCache dpop.ReplayCache) (*Claims, error) {
	claims, err := v.ValidatePlatformToken(ctx, tokenString)
	if err != nil {
		return nil, err
	}
	if claims.Confirmation == nil || claims.Confirmation.Jkt == "" {
		return nil, &NotSenderConstrainedError{}
	}
	if strings.TrimSpace(dpopProof) == "" {
		return nil, &dpop.MissingProofError{}
	}

	if _, err := dpop.VerifyProof(dpopProof, dpop.VerifyOptions{
		HTM:         htm,
		HTU:         htu,
		ExpectedJkt: claims.Confirmation.Jkt,
		ReplayCache: replayCache,
		Clock:       clockAdapter{now: v.nowFunc},
	}); err != nil {
		return nil, err
	}
	return claims, nil
}

// clockAdapter adapts a now func to the dpop.Clock interface.
type clockAdapter struct{ now func() time.Time }

func (c clockAdapter) Now() time.Time { return c.now() }

// audienceContains reports whether aud includes want.
func audienceContains(aud jwt.ClaimStrings, want string) bool {
	for _, a := range aud {
		if a == want {
			return true
		}
	}
	return false
}

// classifyParseError maps a jwt parse error to a typed validator error,
// surfacing JWKS key-resolution errors verbatim so callers see the cause.
func classifyParseError(err error) error {
	var unknown *UnknownKeyIDError
	if errors.As(err, &unknown) {
		return unknown
	}
	var fetch *JWKSFetchError
	if errors.As(err, &fetch) {
		return fetch
	}
	var invalid *InvalidTokenError
	if errors.As(err, &invalid) {
		return invalid
	}
	return &InvalidTokenError{Detail: "signature verification failed", Cause: err}
}
