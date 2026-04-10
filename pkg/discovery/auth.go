//go:build raft

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

// Package discovery provides cluster node discovery mechanisms for QuicBaaS.
package discovery

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
)

// SignMessage generates an HMAC-SHA256 signature for the given payload using the secret.
// Returns the 32-byte signature.
//
// This function is used to authenticate discovery messages in secure clusters.
// The signature should be sent along with the message to allow recipients to verify authenticity.
func SignMessage(secret, payload []byte) []byte {
	h := hmac.New(sha256.New, secret)
	h.Write(payload)
	return h.Sum(nil)
}

// VerifySignature verifies that the given signature matches the HMAC-SHA256 of the payload.
// Uses constant-time comparison to prevent timing attacks.
//
// Returns true if the signature is valid, false otherwise.
// This function should be used to validate all incoming discovery messages in secure clusters.
func VerifySignature(secret, payload, signature []byte) bool {
	expectedSig := SignMessage(secret, payload)

	// Use constant-time comparison to prevent timing attacks
	return subtle.ConstantTimeCompare(signature, expectedSig) == 1
}
