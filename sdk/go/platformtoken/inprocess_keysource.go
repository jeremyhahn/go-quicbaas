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
	"sync"

	jose "github.com/go-jose/go-jose/v4"
)

// inProcessKeySource serves verification keys from a live, in-memory issuer
// JWKS supplied by a function rather than fetched over HTTP. It is used when the
// token issuer validates its own multi-tenant tokens in-process: keySetFunc
// returns the aggregated key set (global key plus every active dedicated tenant
// key), so a cache miss refreshes from the live set and picks up rotated or
// newly added tenant keys without an HTTP self-fetch and without going stale.
type inProcessKeySource struct {
	keySetFunc func() *jose.JSONWebKeySet

	mu   sync.RWMutex
	keys map[string]crypto.PublicKey
}

// newInProcessKeySource constructs an inProcessKeySource backed by fn. The key
// map starts empty; refresh-on-miss populates it from the live set on demand.
func newInProcessKeySource(fn func() *jose.JSONWebKeySet) *inProcessKeySource {
	return &inProcessKeySource{
		keySetFunc: fn,
		keys:       make(map[string]crypto.PublicKey),
	}
}

// publicKey returns the verification key for kid. On a cache miss it refreshes
// the cached map from the live key set and retries, so a rotated or newly added
// kid is resolved without a restart. The context is accepted to satisfy the
// keySource interface but is unused (no I/O). A still-missing kid yields an
// UnknownKeyIDError.
func (s *inProcessKeySource) publicKey(_ context.Context, kid string) (crypto.PublicKey, error) {
	if key := s.cached(kid); key != nil {
		return key, nil
	}
	s.refresh()
	if key := s.cached(kid); key != nil {
		return key, nil
	}
	return nil, &UnknownKeyIDError{KID: kid}
}

// cached returns the cached key for kid, or nil if absent.
func (s *inProcessKeySource) cached(kid string) crypto.PublicKey {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.keys[kid]
}

// refresh rebuilds the cached kid->key map from the live key set. A nil key set
// is treated as empty.
func (s *inProcessKeySource) refresh() {
	jwks := s.keySetFunc()
	keys := map[string]crypto.PublicKey{}
	if jwks != nil {
		keys = make(map[string]crypto.PublicKey, len(jwks.Keys))
		for i := range jwks.Keys {
			k := jwks.Keys[i]
			if pub := publicOf(k); pub != nil {
				keys[k.KeyID] = pub
			}
		}
	}

	s.mu.Lock()
	s.keys = keys
	s.mu.Unlock()
}
