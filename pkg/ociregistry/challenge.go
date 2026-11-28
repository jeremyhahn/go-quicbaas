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

package ociregistry

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"sync"
	"time"
)

// challengeNonceSize is the size in bytes of a server-issued attestation
// challenge. A TPM quote carries it as its qualifying data.
const challengeNonceSize = 32

// challenge is one issued, not yet consumed attestation nonce.
type challenge struct {
	clientID string
	expires  time.Time
}

// challengeStore issues single-use attestation nonces and consumes them.
// A quote is fresh only when it carries a nonce this store issued to the
// same client, before the nonce expired, and that nobody has presented
// before: a captured quote cannot be replayed, because its nonce is gone
// after its first use. The zero value is ready to use.
type challengeStore struct {
	mu      sync.Mutex
	pending map[string]challenge
}

// issue returns a fresh nonce for clientID that is valid for ttl.
// crypto/rand.Read never returns an error (since Go 1.24 it crashes the
// program instead).
func (s *challengeStore) issue(clientID string, now time.Time, ttl time.Duration) []byte {
	nonce := make([]byte, challengeNonceSize)
	rand.Read(nonce)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pending == nil {
		s.pending = make(map[string]challenge)
	}
	for k, c := range s.pending {
		if now.After(c.expires) {
			delete(s.pending, k)
		}
	}
	s.pending[hex.EncodeToString(nonce)] = challenge{clientID: clientID, expires: now.Add(ttl)}
	return nonce
}

// consume removes nonce and reports why it is not a valid challenge for
// clientID, or "" when it is. The nonce is removed whatever the outcome,
// so each one is presented at most once.
func (s *challengeStore) consume(clientID string, nonce []byte, now time.Time) string {
	if len(nonce) == 0 {
		return "missing challenge nonce"
	}
	key := hex.EncodeToString(nonce)
	s.mu.Lock()
	c, ok := s.pending[key]
	delete(s.pending, key)
	s.mu.Unlock()
	switch {
	case !ok:
		return "challenge nonce was not issued or was already used"
	case now.After(c.expires):
		return "challenge nonce has expired"
	case subtle.ConstantTimeCompare([]byte(c.clientID), []byte(clientID)) != 1:
		return "challenge nonce was issued to another client"
	}
	return ""
}
