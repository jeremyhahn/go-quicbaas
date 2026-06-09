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

package platformtoken

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// AuthzMode selects how a tenant's tokens are authorized. The scope decision is
// always offline (fast, supports service tokens); the mode governs whether a
// short-TTL online liveness/revocation check is also enforced.
type AuthzMode string

const (
	// AuthzModeOffline trusts the signed token's scopes and expiry only. No
	// liveness call is made; revocation is bounded by token lifetime.
	AuthzModeOffline AuthzMode = "offline"
	// AuthzModeOnline enforces an online liveness check on every request with
	// no caching: revocation and subject-disablement take effect immediately.
	AuthzModeOnline AuthzMode = "online"
	// AuthzModeHybrid enforces an online liveness check whose positive results
	// are cached for a short TTL. It is the default mode.
	AuthzModeHybrid AuthzMode = "hybrid"
)

// ModeResolver resolves the authorization mode for a tenant. A nil resolver
// (or one returning the empty string) is treated as AuthzModeHybrid.
type ModeResolver func(tenantID string) AuthzMode

// LivenessChecker reports whether a validated token is still live: not revoked
// and belonging to an enabled subject. Implementations are injected; the
// validator never builds the in-process backend itself. The raw token is passed
// alongside the parsed claims so both an RFC 7662 introspection client (which
// needs the wire token) and an in-process checker (which needs claims.ID /
// claims.Subject) satisfy one interface.
type LivenessChecker interface {
	CheckLiveness(ctx context.Context, token string, claims *Claims) (active bool, err error)
}

// IntrospectionLivenessChecker is an RFC 7662 OAuth 2.0 token introspection
// client. It POSTs the raw token to a configured introspection endpoint and
// reports the response's active flag. It is the SDK-provided LivenessChecker for
// services that confirm liveness over HTTP rather than in-process.
type IntrospectionLivenessChecker struct {
	endpoint     string
	httpClient   *http.Client
	clientID     string
	clientSecret string
	bearer       string
}

// NewIntrospectionLivenessChecker constructs an introspection-backed liveness
// checker for the given RFC 7662 endpoint. A nil httpClient uses
// http.DefaultClient.
func NewIntrospectionLivenessChecker(endpoint string, httpClient *http.Client) *IntrospectionLivenessChecker {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &IntrospectionLivenessChecker{endpoint: endpoint, httpClient: httpClient}
}

// WithBasicAuth sets RFC 7662 client_id/client_secret HTTP Basic credentials on
// the introspection request and returns the checker for chaining.
func (c *IntrospectionLivenessChecker) WithBasicAuth(clientID, clientSecret string) *IntrospectionLivenessChecker {
	c.clientID = clientID
	c.clientSecret = clientSecret
	return c
}

// WithBearer sets a Bearer authorization token on the introspection request and
// returns the checker for chaining.
func (c *IntrospectionLivenessChecker) WithBearer(token string) *IntrospectionLivenessChecker {
	c.bearer = token
	return c
}

// introspectionResponse is the subset of the RFC 7662 response the liveness
// decision needs.
type introspectionResponse struct {
	Active bool `json:"active"`
}

// CheckLiveness POSTs the raw token to the introspection endpoint and returns
// the active flag. Any transport, status, or decode failure is returned as a
// *LivenessCheckError so the caller can fail closed.
func (c *IntrospectionLivenessChecker) CheckLiveness(ctx context.Context, token string, _ *Claims) (bool, error) {
	form := url.Values{}
	form.Set("token", token)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return false, &LivenessCheckError{Cause: err}
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	if c.bearer != "" {
		req.Header.Set("Authorization", "Bearer "+c.bearer)
	} else if c.clientID != "" || c.clientSecret != "" {
		req.SetBasicAuth(c.clientID, c.clientSecret)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false, &LivenessCheckError{Cause: err}
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return false, &LivenessCheckError{Cause: &introspectionStatusError{status: resp.StatusCode}}
	}

	var ir introspectionResponse
	if err := json.NewDecoder(resp.Body).Decode(&ir); err != nil {
		return false, &LivenessCheckError{Cause: err}
	}
	return ir.Active, nil
}

// introspectionStatusError reports a non-200 introspection HTTP status.
type introspectionStatusError struct {
	status int
}

func (e *introspectionStatusError) Error() string {
	return fmt.Sprintf("introspection endpoint returned HTTP %d", e.status)
}

// CachedLivenessChecker decorates a base LivenessChecker with a short-TTL cache
// of POSITIVE results only. A negative (inactive) result is never cached, so a
// revocation or subject-disablement is reflected on the very next request. The
// cache is keyed by token jti (claims.ID), falling back to the subject when jti
// is empty. It is safe for concurrent use.
type CachedLivenessChecker struct {
	base LivenessChecker
	ttl  time.Duration
	now  func() time.Time

	mu      sync.Mutex
	entries map[string]time.Time // key -> expiry instant of a cached positive
}

// NewCachedLivenessChecker wraps base with a TTL cache of positive liveness
// results. A ttl of zero disables caching (every call delegates to base). A nil
// now uses time.Now.
func NewCachedLivenessChecker(base LivenessChecker, ttl time.Duration, now func() time.Time) *CachedLivenessChecker {
	if now == nil {
		now = time.Now
	}
	return &CachedLivenessChecker{
		base:    base,
		ttl:     ttl,
		now:     now,
		entries: make(map[string]time.Time),
	}
}

// cacheKey derives the cache key for a token: jti when present, else subject.
func cacheKey(claims *Claims) string {
	if claims.ID != "" {
		return "jti:" + claims.ID
	}
	return "sub:" + claims.Subject
}

// CheckLiveness returns a cached positive result when one is unexpired,
// otherwise delegates to the base checker and caches a fresh positive result.
func (c *CachedLivenessChecker) CheckLiveness(ctx context.Context, token string, claims *Claims) (bool, error) {
	if c.ttl <= 0 {
		return c.base.CheckLiveness(ctx, token, claims)
	}

	key := cacheKey(claims)
	now := c.now()

	c.mu.Lock()
	if expiry, ok := c.entries[key]; ok {
		if now.Before(expiry) {
			c.mu.Unlock()
			return true, nil
		}
		delete(c.entries, key)
	}
	c.mu.Unlock()

	active, err := c.base.CheckLiveness(ctx, token, claims)
	if err != nil {
		return false, err
	}
	if active {
		c.mu.Lock()
		c.entries[key] = now.Add(c.ttl)
		c.mu.Unlock()
	}
	return active, nil
}
