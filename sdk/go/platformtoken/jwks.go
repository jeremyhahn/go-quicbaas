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
	"crypto"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	jose "github.com/go-jose/go-jose/v4"
)

// keySource resolves a verification public key by kid. Implementations cache
// the issuer JWKS and refresh it on a cache miss.
type keySource interface {
	// publicKey returns the verification key for kid, fetching/refreshing the
	// JWKS if the kid is not cached.
	publicKey(ctx context.Context, kid string) (crypto.PublicKey, error)
}

// staticKeySource serves keys from an in-memory JWKS supplied at construction
// time (used when the consumer already holds the issuer JWKS, e.g. in-process
// or test wiring).
type staticKeySource struct {
	keys map[string]crypto.PublicKey
}

func newStaticKeySource(jwks *jose.JSONWebKeySet) (*staticKeySource, error) {
	keys := make(map[string]crypto.PublicKey, len(jwks.Keys))
	for i := range jwks.Keys {
		k := jwks.Keys[i]
		pub := publicOf(k)
		if pub == nil {
			continue
		}
		keys[k.KeyID] = pub
	}
	return &staticKeySource{keys: keys}, nil
}

func (s *staticKeySource) publicKey(_ context.Context, kid string) (crypto.PublicKey, error) {
	if key, ok := s.keys[kid]; ok {
		return key, nil
	}
	return nil, &UnknownKeyIDError{KID: kid}
}

// remoteKeySource fetches and caches the issuer JWKS over HTTP. The cache is
// keyed by kid with a TTL; a cache miss triggers a single refresh so a freshly
// rotated signing key is picked up without a restart. The cache supports
// ECDSA (ES256) in addition to RSA.
type remoteKeySource struct {
	jwksURL    string
	httpClient *http.Client
	ttl        time.Duration
	nowFunc    func() time.Time

	mu        sync.RWMutex
	keys      map[string]crypto.PublicKey
	expiresAt time.Time
}

func newRemoteKeySource(jwksURL string, client *http.Client, ttl time.Duration, nowFunc func() time.Time) *remoteKeySource {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	if ttl <= 0 {
		ttl = time.Hour
	}
	if nowFunc == nil {
		nowFunc = time.Now
	}
	return &remoteKeySource{
		jwksURL:    jwksURL,
		httpClient: client,
		ttl:        ttl,
		nowFunc:    nowFunc,
		keys:       make(map[string]crypto.PublicKey),
	}
}

func (r *remoteKeySource) publicKey(ctx context.Context, kid string) (crypto.PublicKey, error) {
	if key := r.cached(kid); key != nil {
		return key, nil
	}
	if err := r.refresh(ctx); err != nil {
		return nil, err
	}
	if key := r.cached(kid); key != nil {
		return key, nil
	}
	return nil, &UnknownKeyIDError{KID: kid}
}

func (r *remoteKeySource) cached(kid string) crypto.PublicKey {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.nowFunc().After(r.expiresAt) {
		return nil
	}
	return r.keys[kid]
}

func (r *remoteKeySource) refresh(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.jwksURL, nil)
	if err != nil {
		return &JWKSFetchError{Detail: "build request", Cause: err}
	}
	resp, err := r.httpClient.Do(req)
	if err != nil {
		return &JWKSFetchError{Detail: "fetch", Cause: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return &JWKSFetchError{Detail: "unexpected status " + resp.Status}
	}

	var jwks jose.JSONWebKeySet
	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		return &JWKSFetchError{Detail: "decode JWKS", Cause: err}
	}

	keys := make(map[string]crypto.PublicKey, len(jwks.Keys))
	for i := range jwks.Keys {
		k := jwks.Keys[i]
		if pub := publicOf(k); pub != nil {
			keys[k.KeyID] = pub
		}
	}

	r.mu.Lock()
	r.keys = keys
	r.expiresAt = r.nowFunc().Add(r.ttl)
	r.mu.Unlock()
	return nil
}

// publicOf extracts the public key from a JWK entry, handling both the case
// where the JWK already holds a public key and where it wraps a private key.
func publicOf(k jose.JSONWebKey) crypto.PublicKey {
	if k.IsPublic() {
		return k.Key
	}
	pub := k.Public()
	return pub.Key
}
