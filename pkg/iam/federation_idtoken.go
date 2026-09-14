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

package iam

import (
	"context"
	"crypto/ecdsa"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/golang-jwt/jwt/v5"
)

// idpIDTokenClaims holds the validated, security-critical claims extracted from
// an upstream IdP ID token. The full claim set is also returned as a generic
// map (see ValidatedClaims) for downstream claim->role mapping. It is distinct
// from IDTokenClaims, which models quicbaas-ISSUED ID tokens.
type idpIDTokenClaims struct {
	jwt.RegisteredClaims

	// Nonce is the OIDC nonce echoed back by the IdP. It MUST match the nonce
	// stashed in the login state for replay protection (OIDC core 3.1.2.7).
	Nonce string `json:"nonce,omitempty"`
}

// ValidatedClaims is the result of a successful ID-token validation: the parsed
// security claims plus the raw claim map (for claim->role/scope mapping).
type ValidatedClaims struct {
	// Nonce is the validated OIDC nonce from the token.
	Nonce string

	// Raw is the complete decoded claim set. It is only returned AFTER the
	// signature, issuer, audience, and freshness checks have all passed, so it
	// is safe for downstream mapping.
	Raw map[string]interface{}
}

// IDTokenValidatorConfig configures a per-provider ID-token validator.
type IDTokenValidatorConfig struct {
	// Issuer is the exact expected `iss` claim. For an OIDC provider this is the
	// ExternalProvider.IssuerURL.
	Issuer string

	// Audience is the expected entry in the `aud` claim — the provider ClientID.
	Audience string

	// JWKSURL is the explicit JWKS endpoint. When empty it is discovered from
	// the issuer's .well-known/openid-configuration document.
	JWKSURL string

	// CacheTTL is how long fetched signing keys are cached. Defaults to 1h.
	CacheTTL time.Duration

	// HTTPClient performs JWKS / discovery fetches. Required (the federation
	// manager injects its shared client; tests inject httptest clients).
	HTTPClient *http.Client

	// Now overrides the clock for freshness checks. Defaults to time.Now.
	Now func() time.Time

	// Leeway is the allowed clock skew for exp/iat/nbf checks. Defaults to 60s.
	Leeway time.Duration
}

// IDTokenValidator validates an upstream IdP's OIDC ID token: RS256/ES256
// signature against the provider JWKS (cached by kid), exact `iss`, `aud`
// containing the provider ClientID, and `exp`/`iat` freshness. The nonce is
// validated by the caller against the value stashed in the login state.
//
// This validates the UPSTREAM IdP token. It is intentionally separate from the
// quicbaas platformtoken validator, which validates quicbaas-issued tokens.
type IDTokenValidator struct {
	issuer     string
	audience   string
	jwksURL    string
	httpClient *http.Client
	now        func() time.Time
	leeway     time.Duration

	keyCache *idTokenKeyCache

	// discoveryOnce guards lazy JWKS-URL discovery from the issuer.
	discoveryMu sync.Mutex
}

// NewIDTokenValidator builds a validator from config. The JWKSURL may be empty,
// in which case it is discovered lazily from the issuer on first validation.
func NewIDTokenValidator(cfg IDTokenValidatorConfig) (*IDTokenValidator, error) {
	if cfg.Issuer == "" {
		return nil, &IDTokenValidatorConfigError{Reason: "issuer is required"}
	}
	if cfg.Audience == "" {
		return nil, &IDTokenValidatorConfigError{Reason: "audience (client ID) is required"}
	}
	if cfg.HTTPClient == nil {
		return nil, &IDTokenValidatorConfigError{Reason: "http client is required"}
	}

	ttl := cfg.CacheTTL
	if ttl <= 0 {
		ttl = time.Hour
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	leeway := cfg.Leeway
	if leeway <= 0 {
		leeway = 60 * time.Second
	}

	return &IDTokenValidator{
		issuer:     cfg.Issuer,
		audience:   cfg.Audience,
		jwksURL:    cfg.JWKSURL,
		httpClient: cfg.HTTPClient,
		now:        now,
		leeway:     leeway,
		keyCache:   newIDTokenKeyCache(ttl, now),
	}, nil
}

// Validate verifies the ID token's signature and registered claims and returns
// the validated claim set. It fails closed: any verification failure returns a
// typed error and an empty result. The caller MUST additionally check the nonce
// against the login-state nonce.
func (v *IDTokenValidator) Validate(ctx context.Context, idToken string) (*ValidatedClaims, error) {
	if strings.TrimSpace(idToken) == "" {
		return nil, &IDTokenInvalidError{Reason: "empty token"}
	}

	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{"RS256", "RS384", "RS512", "ES256", "ES384", "ES512"}),
		jwt.WithIssuer(v.issuer),
		jwt.WithAudience(v.audience),
		jwt.WithLeeway(v.leeway),
		jwt.WithTimeFunc(v.now),
		jwt.WithExpirationRequired(),
	)

	claims := &idpIDTokenClaims{}
	token, err := parser.ParseWithClaims(idToken, claims, func(token *jwt.Token) (interface{}, error) {
		kid, _ := token.Header["kid"].(string)
		if kid == "" {
			return nil, &IDTokenInvalidError{Reason: "missing kid in token header"}
		}
		return v.publicKeyForKID(ctx, kid)
	})
	if err != nil {
		return nil, classifyIDTokenError(err)
	}
	if !token.Valid {
		return nil, &IDTokenInvalidError{Reason: "token failed validation"}
	}

	// `iat` freshness: jwt v5 validates exp/nbf but not iat presence. OIDC
	// requires iat; reject tokens issued in the future beyond leeway.
	if claims.IssuedAt == nil {
		return nil, &IDTokenInvalidError{Reason: "missing iat claim"}
	}
	if claims.IssuedAt.After(v.now().Add(v.leeway)) {
		return nil, &IDTokenInvalidError{Reason: "token issued in the future"}
	}

	// Re-decode the raw payload for downstream claim->role mapping. This is only
	// reached after the signature/iss/aud/exp checks above have all passed.
	raw, err := decodeJWTPayload(idToken)
	if err != nil {
		return nil, err
	}

	return &ValidatedClaims{
		Nonce: claims.Nonce,
		Raw:   raw,
	}, nil
}

// publicKeyForKID returns the cached signing key for kid, refreshing the JWKS on
// a cache miss.
func (v *IDTokenValidator) publicKeyForKID(ctx context.Context, kid string) (interface{}, error) {
	if key := v.keyCache.get(kid); key != nil {
		return key, nil
	}
	if err := v.refreshJWKS(ctx); err != nil {
		return nil, err
	}
	if key := v.keyCache.get(kid); key != nil {
		return key, nil
	}
	return nil, &IDTokenInvalidError{Reason: "signing key not found for kid"}
}

// refreshJWKS fetches the provider JWKS and repopulates the key cache.
func (v *IDTokenValidator) refreshJWKS(ctx context.Context) error {
	jwksURL, err := v.resolveJWKSURL(ctx)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, jwksURL, nil)
	if err != nil {
		return &IDTokenJWKSError{Reason: "failed to build JWKS request", Cause: err}
	}
	resp, err := v.httpClient.Do(req)
	if err != nil {
		return &IDTokenJWKSError{Reason: "failed to fetch JWKS", Cause: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return &IDTokenJWKSError{Reason: "JWKS fetch returned non-200", StatusCode: resp.StatusCode}
	}

	var set idTokenJWKS
	if err := json.NewDecoder(resp.Body).Decode(&set); err != nil {
		return &IDTokenJWKSError{Reason: "failed to decode JWKS", Cause: err}
	}

	for _, raw := range set.Keys {
		key, kid, err := parseIDTokenJWK(raw)
		if err != nil {
			// RFC 7517 section 5: ignore a key we cannot parse or use rather
			// than discarding the rest of the set. An ignored key never enters
			// the cache, so it can never verify a token.
			continue
		}
		v.keyCache.set(kid, key)
	}
	return nil
}

// resolveJWKSURL returns the configured JWKS URL or discovers it from the
// issuer's OIDC discovery document. The discovered value is memoized.
func (v *IDTokenValidator) resolveJWKSURL(ctx context.Context) (string, error) {
	v.discoveryMu.Lock()
	defer v.discoveryMu.Unlock()

	if v.jwksURL != "" {
		return v.jwksURL, nil
	}

	discoveryURL := strings.TrimSuffix(v.issuer, "/") + "/.well-known/openid-configuration"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, discoveryURL, nil)
	if err != nil {
		return "", &IDTokenJWKSError{Reason: "failed to build discovery request", Cause: err}
	}
	resp, err := v.httpClient.Do(req)
	if err != nil {
		return "", &IDTokenJWKSError{Reason: "failed to fetch discovery document", Cause: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", &IDTokenJWKSError{Reason: "discovery fetch returned non-200", StatusCode: resp.StatusCode}
	}

	var doc struct {
		JwksURI string `json:"jwks_uri"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return "", &IDTokenJWKSError{Reason: "failed to decode discovery document", Cause: err}
	}
	if doc.JwksURI == "" {
		return "", &IDTokenJWKSError{Reason: "discovery document has no jwks_uri"}
	}

	v.jwksURL = doc.JwksURI
	return v.jwksURL, nil
}

// decodeJWTPayload decodes the (already-verified) JWT payload to a claim map.
func decodeJWTPayload(idToken string) (map[string]interface{}, error) {
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return nil, &IDTokenFormatError{}
	}
	payload, err := jwt.NewParser().DecodeSegment(parts[1])
	if err != nil {
		return nil, &IDTokenDecodeError{Cause: err}
	}
	var claims map[string]interface{}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, &IDTokenClaimsError{Cause: err}
	}
	return claims, nil
}

// classifyIDTokenError maps jwt validation errors to typed federation errors so
// callers and tests can distinguish failure modes (RFC-correct RP validation).
func classifyIDTokenError(err error) error {
	switch {
	case errors.Is(err, jwt.ErrTokenExpired):
		return &IDTokenExpiredError{}
	case errors.Is(err, jwt.ErrTokenNotValidYet):
		return &IDTokenInvalidError{Reason: "token not valid yet"}
	case errors.Is(err, jwt.ErrTokenInvalidIssuer):
		return &IDTokenIssuerError{}
	case errors.Is(err, jwt.ErrTokenInvalidAudience):
		return &IDTokenAudienceError{}
	case errors.Is(err, jwt.ErrTokenSignatureInvalid):
		return &IDTokenSignatureError{Cause: err}
	default:
		return &IDTokenInvalidError{Reason: err.Error()}
	}
}

// idTokenJWKS is a JSON Web Key Set as published at a provider's jwks_uri.
// Entries are held raw so a single unusable key does not discard the whole set
// (RFC 7517 section 5).
type idTokenJWKS struct {
	Keys []json.RawMessage `json:"keys"`
}

// parseIDTokenJWK parses one JWKS entry into a verification key. Parsing is
// delegated to go-jose, which validates the key material rather than trusting
// it: for EC that includes checking the coordinates are the curve's field width
// and that the point actually lies on the declared curve. Decoding coordinates
// by hand into an ecdsa.PublicKey cannot detect an off-curve point, which would
// let whoever answers the JWKS fetch install an invalid verification key.
//
// Only the public RSA and ECDSA key types the validator's permitted RS*/ES*
// algorithms can actually use are accepted; every other key type fails closed.
func parseIDTokenJWK(raw json.RawMessage) (interface{}, string, error) {
	var jwk jose.JSONWebKey
	if err := jwk.UnmarshalJSON(raw); err != nil {
		return nil, "", &IDTokenJWKSError{Reason: "failed to parse JWK", Cause: err}
	}
	if !jwk.Valid() {
		return nil, "", &IDTokenJWKSError{Reason: "JWK is missing required key parameters"}
	}
	switch jwk.Key.(type) {
	case *rsa.PublicKey, *ecdsa.PublicKey:
		return jwk.Key, jwk.KeyID, nil
	default:
		return nil, "", &IDTokenJWKSError{
			Reason: fmt.Sprintf("unsupported JWK key type %T for RS*/ES* verification", jwk.Key),
		}
	}
}

// idTokenKeyCache is a thread-safe cache of signing keys by kid with TTL.
type idTokenKeyCache struct {
	mu   sync.RWMutex
	keys map[string]idTokenCachedKey
	ttl  time.Duration
	now  func() time.Time
}

type idTokenCachedKey struct {
	key       interface{}
	expiresAt time.Time
}

func newIDTokenKeyCache(ttl time.Duration, now func() time.Time) *idTokenKeyCache {
	return &idTokenKeyCache{
		keys: make(map[string]idTokenCachedKey),
		ttl:  ttl,
		now:  now,
	}
}

func (c *idTokenKeyCache) get(kid string) interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()
	cached, ok := c.keys[kid]
	if !ok {
		return nil
	}
	if c.now().After(cached.expiresAt) {
		return nil
	}
	return cached.key
}

func (c *idTokenKeyCache) set(kid string, key interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.keys[kid] = idTokenCachedKey{key: key, expiresAt: c.now().Add(c.ttl)}
}
