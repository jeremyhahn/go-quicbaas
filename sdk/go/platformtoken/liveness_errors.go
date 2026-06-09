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

import "fmt"

// TokenInactiveError indicates an online liveness check reported the token as
// inactive: it has been revoked, or its subject (user/service account) has been
// disabled. The token's signature, scopes, and expiry may all still be valid;
// liveness is the authoritative revocation/disablement signal that overrides a
// structurally valid token. Callers map this to 401.
type TokenInactiveError struct {
	// Subject is the token sub whose liveness check failed.
	Subject string
	// JTI is the token identifier (jti) whose liveness check failed, if present.
	JTI string
}

func (e *TokenInactiveError) Error() string {
	return fmt.Sprintf("platformtoken: token is inactive (revoked or subject disabled): sub=%q jti=%q", e.Subject, e.JTI)
}

// LivenessCheckError indicates the online liveness backend could not be reached
// or returned an unusable response. The validator treats this as a deny
// (fail-closed): a request must not be authorized when liveness cannot be
// confirmed in a mode that requires it.
type LivenessCheckError struct {
	// Cause is the wrapped underlying transport or decode error.
	Cause error
}

func (e *LivenessCheckError) Error() string {
	return fmt.Sprintf("platformtoken: liveness check failed: %v", e.Cause)
}

func (e *LivenessCheckError) Unwrap() error { return e.Cause }
