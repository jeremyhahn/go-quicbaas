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
	// AAGUIDPolicyStrict rejects credentials from authenticators whose AAGUID
	// is not present in the allowlist.
	//
	// It is the ZERO VALUE deliberately. Permissive used to occupy this slot,
	// which meant an AAGUIDPolicy nobody set accepted every authenticator on
	// the planet — the safe configuration was the one you had to remember to
	// ask for. The ordering is inverted so that doing nothing denies.
	AAGUIDPolicyStrict AAGUIDPolicy = iota

	// AAGUIDPolicyPermissive logs a warning but allows unknown authenticators.
	// It is an explicit opt-in for development environments and for tenant
	// (non-administrative) enrollment where the operator has decided any
	// authenticator model is acceptable. It must never be reached by default.
	AAGUIDPolicyPermissive
)

// String implements fmt.Stringer so log lines and errors name the policy rather
// than printing an integer.
func (p AAGUIDPolicy) String() string {
	switch p {
	case AAGUIDPolicyStrict:
		return "strict"
	case AAGUIDPolicyPermissive:
		return "permissive"
	default:
		return "unknown"
	}
}

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
//
// A NIL ALLOWLIST DENIES. The gate used to be an optional decoration —
// `if allowlist != nil { allowlist.Check(...) }` — which made an unconfigured
// deployment strictly worse than a permissive one: it had no gate at all, and
// the absence was invisible. Checking on the nil receiver here moves the
// decision inside the type, so every caller inherits the refusal and no future
// caller can reintroduce the nil-skip by writing the guard the obvious way.
//
// An empty AAGUID under a strict policy is refused with the same reason a
// non-allowlisted one is: an authenticator that conveyed no attestation reports
// no AAGUID, so an empty value is the ABSENCE of an identifier rather than an
// identifier that happens to be zero. Admitting it would make the allowlist
// vacuous exactly when it matters.
func (a *AAGUIDAllowlist) Check(aaguid []byte) error {
	if a == nil {
		return NewErrAAGUIDGateUnconfigured()
	}

	// Empty allowlist in permissive mode: allow all.
	if len(a.allowed) == 0 && a.policy == AAGUIDPolicyPermissive {
		return nil
	}

	normalized := strings.ToLower(hex.EncodeToString(aaguid))
	if len(aaguid) > 0 {
		if _, ok := a.allowed[normalized]; ok {
			return nil
		}
	}

	if a.policy == AAGUIDPolicyStrict {
		return NewErrAAGUIDNotAllowed(normalized, "")
	}

	// Permissive: warn via the error type but do not block.
	return nil
}

// Contains reports whether the hex-encoded AAGUID is in the allowlist. A nil
// allowlist contains nothing.
func (a *AAGUIDAllowlist) Contains(aaguid []byte) bool {
	if a == nil || len(aaguid) == 0 {
		return false
	}
	normalized := strings.ToLower(hex.EncodeToString(aaguid))
	_, ok := a.allowed[normalized]
	return ok
}

// Len returns the number of allowed AAGUIDs. A nil allowlist has length zero.
func (a *AAGUIDAllowlist) Len() int {
	if a == nil {
		return 0
	}
	return len(a.allowed)
}

// Policy returns the configured enforcement policy. A nil allowlist reports
// AAGUIDPolicyStrict: an absent gate denies, and Policy must describe the same
// behaviour Check implements rather than the permissive one an integer zero
// value would otherwise suggest.
func (a *AAGUIDAllowlist) Policy() AAGUIDPolicy {
	if a == nil {
		return AAGUIDPolicyStrict
	}
	return a.policy
}
