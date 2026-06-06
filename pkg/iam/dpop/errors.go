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

// Package dpop implements RFC 9449 DPoP (Demonstrating Proof of Possession)
// proof generation and verification plus a jti replay cache. It is the single
// canonical DPoP implementation in the stack: the OIDC token issuer binds a
// cnf.jkt from a presented proof, and the reusable SDK platform-token validator
// verifies the proof against that binding. The package carries no IAM build tag
// and no IAM-specific dependencies so downstream services can import it
// directly for sender-constrained token enforcement.
package dpop

import "fmt"

// MalformedProofError indicates the DPoP proof JWT could not be parsed or is
// structurally invalid (missing header fields, wrong typ, etc.).
type MalformedProofError struct {
	// Detail describes the structural defect.
	Detail string
	// Cause is the wrapped underlying error, if any.
	Cause error
}

func (e *MalformedProofError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("dpop: malformed proof: %s: %v", e.Detail, e.Cause)
	}
	return fmt.Sprintf("dpop: malformed proof: %s", e.Detail)
}

func (e *MalformedProofError) Unwrap() error { return e.Cause }

// SignatureError indicates the proof signature did not verify against the
// embedded jwk header key.
type SignatureError struct {
	// Cause is the wrapped verification error.
	Cause error
}

func (e *SignatureError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("dpop: proof signature verification failed: %v", e.Cause)
	}
	return "dpop: proof signature verification failed"
}

func (e *SignatureError) Unwrap() error { return e.Cause }

// HTTPBindingError indicates the htm (method) or htu (URI) claim in the proof
// did not match the actual request.
type HTTPBindingError struct {
	// Field is "htm" or "htu".
	Field string
	// Expected is the value derived from the request.
	Expected string
	// Actual is the value found in the proof.
	Actual string
}

func (e *HTTPBindingError) Error() string {
	return fmt.Sprintf("dpop: %s mismatch: expected %q, proof carried %q", e.Field, e.Expected, e.Actual)
}

// StaleProofError indicates the proof iat is outside the allowed clock skew
// window (too old or too far in the future).
type StaleProofError struct {
	// Detail describes whether the proof was expired or future-dated.
	Detail string
}

func (e *StaleProofError) Error() string {
	return fmt.Sprintf("dpop: proof iat out of skew window: %s", e.Detail)
}

// ReplayError indicates the proof jti has already been seen within its
// lifetime, i.e. the proof is being replayed.
type ReplayError struct {
	// JTI is the replayed proof identifier.
	JTI string
}

func (e *ReplayError) Error() string {
	return fmt.Sprintf("dpop: proof jti %q already used (replay)", e.JTI)
}

// ThumbprintMismatchError indicates the SHA-256 JWK thumbprint of the proof key
// did not equal the access token's cnf.jkt binding.
type ThumbprintMismatchError struct {
	// Expected is the access-token cnf.jkt value.
	Expected string
	// Actual is the computed proof-key thumbprint.
	Actual string
}

func (e *ThumbprintMismatchError) Error() string {
	return fmt.Sprintf("dpop: proof key thumbprint %q does not match cnf.jkt %q", e.Actual, e.Expected)
}

// MissingProofError indicates a DPoP proof was required (the token is
// sender-constrained via cnf.jkt) but none was presented.
type MissingProofError struct{}

func (e *MissingProofError) Error() string {
	return "dpop: proof required by cnf.jkt but none presented"
}

// SignerError indicates a proof could not be generated.
type SignerError struct {
	// Detail describes the failure.
	Detail string
	// Cause is the wrapped underlying error.
	Cause error
}

func (e *SignerError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("dpop: %s: %v", e.Detail, e.Cause)
	}
	return fmt.Sprintf("dpop: %s", e.Detail)
}

func (e *SignerError) Unwrap() error { return e.Cause }
