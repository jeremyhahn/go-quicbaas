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

package orchestration

// This file adds the trust axis that sits alongside the sandbox axis in
// sandbox_policy.go.
//
// The two axes answer different questions and neither subsumes the other:
//
//	SandboxMode / SandboxPolicy — is the agent ISOLATED from the host?
//	TrustLevel  / TrustPolicy   — is the agent the code we APPROVED, and can
//	                              the host prove it?
//
// A container can be perfectly isolated while running an image someone
// swapped under a mutable tag, and a digest-pinned image can run with no
// sandbox at all. Both axes resolve the same way — a per-request mode, a
// server policy, an optional tenant override, and host capabilities — and
// both deny by default when a required level cannot be satisfied.

// TrustLevel selects the integrity tier required of an agent execution on a
// per-request basis. It is the trust-axis counterpart of SandboxMode.
type TrustLevel int32

const (
	// TrustLevelDefault lets the server resolve the effective level using
	// its configured TrustPolicy and any tenant-level override. Zero value,
	// so specs written before the trust axis existed keep resolving to
	// whatever the server policy says — which itself defaults to opt-in.
	TrustLevelDefault TrustLevel = 0

	// TrustLevelNone requires no integrity guarantee. The image may be
	// referenced by a mutable tag and no attestation is performed. This is
	// what every execution did before the trust axis existed.
	TrustLevelNone TrustLevel = 1

	// TrustLevelPinned is Tier 0: the container must be launched from an
	// immutable content digest (repository@sha256:<hex>) and the runtime
	// must verify after launch that the container resolves to that digest.
	// Requires no hardware.
	TrustLevelPinned TrustLevel = 2

	// TrustLevelAttested is Tier 1: Tier 0 plus a host TPM quote verified
	// against the expected PCR set before the agent is trusted. Requires a
	// TPM (or software TPM) reachable by the runtime.
	TrustLevelAttested TrustLevel = 3

	// TrustLevelMeasured is Tier 2: Tier 1 plus measured launch — the
	// container's image and configuration digest are extended into a PCR
	// immediately before launch, and that PCR is included in the quote. The
	// resulting quote proves not only that the host booted correctly but
	// that this exact configuration was the thing launched.
	TrustLevelMeasured TrustLevel = 4
)

// String returns the canonical wire name for a TrustLevel.
func (l TrustLevel) String() string {
	switch l {
	case TrustLevelDefault:
		return "default"
	case TrustLevelNone:
		return "none"
	case TrustLevelPinned:
		return "pinned"
	case TrustLevelAttested:
		return "attested"
	case TrustLevelMeasured:
		return "measured"
	default:
		return "unknown"
	}
}

// IsValid reports whether l is one of the declared trust levels.
func (l TrustLevel) IsValid() bool {
	return l >= TrustLevelDefault && l <= TrustLevelMeasured
}

// RequiresDigestPin reports whether the level rests on an immutable image
// content digest. True for Tier 0 and above.
func (l TrustLevel) RequiresDigestPin() bool {
	return l >= TrustLevelPinned
}

// RequiresHostTPM reports whether the level requires a host TPM quote.
// True for Tier 1 and above.
func (l TrustLevel) RequiresHostTPM() bool {
	return l >= TrustLevelAttested
}

// RequiresMeasuredLaunch reports whether the level requires the launch
// measurement to be extended into a PCR. True for Tier 2.
func (l TrustLevel) RequiresMeasuredLaunch() bool {
	return l >= TrustLevelMeasured
}

// ParseTrustLevel decodes a string (as produced by String) back into a
// TrustLevel. Empty string maps to TrustLevelDefault.
func ParseTrustLevel(s string) (TrustLevel, error) {
	switch s {
	case "", "default":
		return TrustLevelDefault, nil
	case "none":
		return TrustLevelNone, nil
	case "pinned":
		return TrustLevelPinned, nil
	case "attested":
		return TrustLevelAttested, nil
	case "measured":
		return TrustLevelMeasured, nil
	default:
		return TrustLevelDefault, &InputValidationError{
			Field:   "trust_level",
			Message: "must be one of: default, none, pinned, attested, measured",
		}
	}
}

// TrustPolicy is the server-side policy that governs how TrustLevelDefault
// resolves and which explicit levels are permitted. It is the trust-axis
// counterpart of SandboxPolicy.
//
// The zero value is TrustPolicyOptIn, so an orchestrator that has never been
// configured for trusted computing behaves exactly as it did before the
// trust axis existed: DEFAULT resolves to NONE and nothing is rejected.
type TrustPolicy int32

const (
	// TrustPolicyOptIn resolves DEFAULT to TrustLevelNone. Higher levels
	// happen only when a request explicitly asks for them. Zero value.
	TrustPolicyOptIn TrustPolicy = 0

	// TrustPolicyPinned resolves DEFAULT to TrustLevelPinned and forbids
	// TrustLevelNone. Every agent image must be digest-pinned.
	TrustPolicyPinned TrustPolicy = 1

	// TrustPolicyAttested resolves DEFAULT to TrustLevelAttested and
	// forbids anything below it. Every agent must run on a host that
	// produced a verified TPM quote.
	TrustPolicyAttested TrustPolicy = 2

	// TrustPolicyMeasured resolves DEFAULT to TrustLevelMeasured and
	// forbids anything below it. The strictest standalone posture.
	TrustPolicyMeasured TrustPolicy = 3
)

// String returns the canonical wire name for a TrustPolicy.
func (p TrustPolicy) String() string {
	switch p {
	case TrustPolicyOptIn:
		return "opt-in"
	case TrustPolicyPinned:
		return "pinned"
	case TrustPolicyAttested:
		return "attested"
	case TrustPolicyMeasured:
		return "measured"
	default:
		return "unknown"
	}
}

// ParseTrustPolicy decodes a string (as produced by String) back into a
// TrustPolicy. Empty string maps to TrustPolicyOptIn, matching the Go zero
// value.
func ParseTrustPolicy(s string) (TrustPolicy, error) {
	switch s {
	case "", "opt-in", "optin", "opt_in":
		return TrustPolicyOptIn, nil
	case "pinned":
		return TrustPolicyPinned, nil
	case "attested":
		return TrustPolicyAttested, nil
	case "measured":
		return TrustPolicyMeasured, nil
	default:
		return TrustPolicyOptIn, &InputValidationError{
			Field:   "trust_policy",
			Message: "must be one of: opt-in, pinned, attested, measured",
		}
	}
}

// floor returns the lowest TrustLevel the policy permits, which is also the
// level TrustLevelDefault resolves to under that policy.
func (p TrustPolicy) floor() TrustLevel {
	switch p {
	case TrustPolicyPinned:
		return TrustLevelPinned
	case TrustPolicyAttested:
		return TrustLevelAttested
	case TrustPolicyMeasured:
		return TrustLevelMeasured
	default:
		return TrustLevelNone
	}
}

// TrustCapabilities describes what the target host can actually prove. It is
// the trust-axis counterpart of the hasContainerRuntime argument on
// ResolveSandboxMode, widened to a struct because there are three
// independent capabilities rather than one.
//
// The zero value advertises no capability at all, which is the correct
// posture for a host that has not been probed: a required tier then fails
// closed rather than silently downgrading.
type TrustCapabilities struct {
	// DigestPinning reports whether the host's container runtime can
	// launch and verify an image by content digest (Tier 0).
	DigestPinning bool

	// HostTPM reports whether the host exposes a TPM the runtime can quote
	// (Tier 1).
	HostTPM bool

	// MeasuredLaunch reports whether the host permits extending the
	// measurement PCR before launch (Tier 2).
	MeasuredLaunch bool
}

// supports reports whether the capabilities satisfy the requested level.
func (c TrustCapabilities) supports(level TrustLevel) bool {
	if level.RequiresDigestPin() && !c.DigestPinning {
		return false
	}
	if level.RequiresHostTPM() && !c.HostTPM {
		return false
	}
	if level.RequiresMeasuredLaunch() && !c.MeasuredLaunch {
		return false
	}
	return true
}

// missingCapability names the first capability the host lacks for level. It
// is only meaningful when supports returns false.
func (c TrustCapabilities) missingCapability(level TrustLevel) string {
	switch {
	case level.RequiresDigestPin() && !c.DigestPinning:
		return "image digest pinning"
	case level.RequiresHostTPM() && !c.HostTPM:
		return "host TPM attestation"
	case level.RequiresMeasuredLaunch() && !c.MeasuredLaunch:
		return "measured launch"
	default:
		return "none"
	}
}

// TrustLevelViolationError signals that a request's TrustLevel cannot be
// satisfied under the active TrustPolicy (possibly after tenant override) or
// because the target host lacks the required capability.
type TrustLevelViolationError struct {
	Requested TrustLevel
	Policy    TrustPolicy
	Reason    string
}

// Error implements the error interface.
func (e *TrustLevelViolationError) Error() string {
	return "trust level " + e.Requested.String() + " not permitted under policy " +
		e.Policy.String() + ": " + e.Reason
}

// ResolveTrustLevel resolves the effective trust level for a request given
// the server's policy, an optional tenant-level policy override, and the
// capabilities of the target host.
//
// The tenant override may only TIGHTEN the server policy, never loosen it —
// the same rule ResolveSandboxMode applies, so a tenant with a stricter
// compliance obligation can raise the floor without the operator tightening
// the whole deployment, and a tenant record can never lower it.
//
// Resolution is deny-by-default in both directions:
//
//   - An explicit level below the effective policy floor is rejected; a
//     caller cannot opt out of a tier the operator requires.
//   - A level the host cannot prove is rejected rather than downgraded. The
//     one exception is a DEFAULT request under TrustPolicyOptIn, which
//     resolves to NONE because opt-in means "no trust requirement at all".
func ResolveTrustLevel(
	requested TrustLevel,
	serverPolicy TrustPolicy,
	tenantOverride *TrustPolicy,
	caps TrustCapabilities,
) (TrustLevel, error) {
	effective := serverPolicy
	if tenantOverride != nil && *tenantOverride > effective {
		effective = *tenantOverride
	}

	if !requested.IsValid() {
		return TrustLevelDefault, &TrustLevelViolationError{
			Requested: requested,
			Policy:    effective,
			Reason:    "unknown trust level",
		}
	}

	floor := effective.floor()

	resolved := requested
	if requested == TrustLevelDefault {
		resolved = floor
	}

	if resolved < floor {
		return resolved, &TrustLevelViolationError{
			Requested: requested,
			Policy:    effective,
			Reason:    "policy requires at least trust level " + floor.String(),
		}
	}

	// TrustLevelNone needs nothing from the host, so it is always
	// satisfiable once the policy floor permits it.
	if resolved == TrustLevelNone {
		return resolved, nil
	}

	if !caps.supports(resolved) {
		return resolved, &TrustLevelViolationError{
			Requested: requested,
			Policy:    effective,
			Reason:    "target host does not support " + caps.missingCapability(resolved),
		}
	}

	return resolved, nil
}
