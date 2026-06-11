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
	"encoding/hex"
	"strings"
)

// AAGUIDPolicy controls how unknown FIDO2 AAGUIDs are handled during WebAuthn
// registration and login.
type AAGUIDPolicy int

const (
	// AAGUIDPolicyPermissive logs a warning but allows unknown authenticators.
	// Recommended for development environments.
	AAGUIDPolicyPermissive AAGUIDPolicy = iota

	// AAGUIDPolicyStrict rejects credentials from authenticators whose AAGUID is
	// not present in the allowlist. Recommended for production environments.
	AAGUIDPolicyStrict
)

// AAGUIDAllowlist holds the set of permitted FIDO2 authenticator AAGUIDs and
// enforces the configured policy during WebAuthn operations.
type AAGUIDAllowlist struct {
	allowed map[string]struct{}
	policy  AAGUIDPolicy
}

// NewAAGUIDAllowlist creates a new allowlist from a slice of hex-encoded AAGUID
// strings. Pass AAGUIDPolicyStrict for production deployments to reject any
// authenticator not present in the list.
func NewAAGUIDAllowlist(hexAAGUIDs []string, policy AAGUIDPolicy) *AAGUIDAllowlist {
	allowed := make(map[string]struct{}, len(hexAAGUIDs))
	for _, v := range hexAAGUIDs {
		// Normalise: strip hyphens (UUID format) and lowercase.
		normalized := strings.ToLower(strings.ReplaceAll(v, "-", ""))
		allowed[normalized] = struct{}{}
	}
	return &AAGUIDAllowlist{
		allowed: allowed,
		policy:  policy,
	}
}

// Check returns an error when the supplied AAGUID bytes are not in the allowlist
// and the policy is AAGUIDPolicyStrict. In permissive mode it always returns nil.
func (a *AAGUIDAllowlist) Check(aaguid []byte) error {
	// Empty allowlist in permissive mode: allow all.
	if len(a.allowed) == 0 && a.policy == AAGUIDPolicyPermissive {
		return nil
	}

	normalized := strings.ToLower(hex.EncodeToString(aaguid))
	if _, ok := a.allowed[normalized]; ok {
		return nil
	}

	if a.policy == AAGUIDPolicyStrict {
		return NewErrAAGUIDNotAllowed(normalized, "")
	}

	// Permissive: warn via the error type but do not block.
	return nil
}

// Contains reports whether the hex-encoded AAGUID is in the allowlist.
func (a *AAGUIDAllowlist) Contains(aaguid []byte) bool {
	if len(aaguid) == 0 {
		return false
	}
	normalized := strings.ToLower(hex.EncodeToString(aaguid))
	_, ok := a.allowed[normalized]
	return ok
}

// Len returns the number of allowed AAGUIDs.
func (a *AAGUIDAllowlist) Len() int {
	return len(a.allowed)
}

// Policy returns the configured enforcement policy.
func (a *AAGUIDAllowlist) Policy() AAGUIDPolicy {
	return a.policy
}
