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

package orchestration

// This file holds the SANDBOX axis: is the agent isolated from the host?
//
// The orthogonal TRUST axis — is the agent the code we approved, and can the
// host prove it? — lives in trust_policy.go as TrustLevel / TrustPolicy /
// ResolveTrustLevel, which mirror the SandboxMode / SandboxPolicy /
// ResolveSandboxMode shapes below. Neither axis subsumes the other and both
// deny by default when a required level cannot be satisfied.

// SandboxMode selects the execution sandboxing strategy for an agent session
// on a per-request basis.
type SandboxMode int32

const (
	// SandboxModeDefault lets the server resolve the effective mode using its
	// configured SandboxPolicy and any tenant-level override.
	SandboxModeDefault SandboxMode = 0

	// SandboxModeRequired forces container-sandboxed execution. The request is
	// rejected if no container runtime is available on the target host.
	SandboxModeRequired SandboxMode = 1

	// SandboxModeNone requests raw process execution with no sandbox. The
	// request is rejected if the server's SandboxPolicy forbids raw exec.
	SandboxModeNone SandboxMode = 2
)

// String returns the canonical wire name for a SandboxMode.
func (m SandboxMode) String() string {
	switch m {
	case SandboxModeDefault:
		return "default"
	case SandboxModeRequired:
		return "required"
	case SandboxModeNone:
		return "none"
	default:
		return "unknown"
	}
}

// ParseSandboxMode decodes a string (as produced by String) back into a
// SandboxMode. Empty string maps to SandboxModeDefault.
func ParseSandboxMode(s string) (SandboxMode, error) {
	switch s {
	case "", "default":
		return SandboxModeDefault, nil
	case "required":
		return SandboxModeRequired, nil
	case "none":
		return SandboxModeNone, nil
	default:
		return SandboxModeDefault, &InputValidationError{
			Field:   "sandbox_mode",
			Message: "must be one of: default, required, none",
		}
	}
}

// SandboxPolicy is the server-side policy that governs how SandboxModeDefault
// is resolved and which explicit modes are permitted.
//
// The zero value is SandboxPolicyOptIn: the central orchestrator preserves
// legacy "raw exec unless explicitly sandboxed" semantics unless the operator
// explicitly tightens the policy. Remote agent daemons flip this default to
// SandboxPolicyRequired at startup (see Phase 2).
type SandboxPolicy int32

const (
	// SandboxPolicyOptIn resolves DEFAULT to raw execution. Container
	// sandboxing only happens when the client explicitly requests REQUIRED.
	// Suitable for trusted development environments and backward compatibility.
	// Zero value.
	SandboxPolicyOptIn SandboxPolicy = 0

	// SandboxPolicyPreferred resolves DEFAULT to container-sandboxed, but
	// allows NONE when the client explicitly requests it.
	SandboxPolicyPreferred SandboxPolicy = 1

	// SandboxPolicyRequired is the most restrictive policy. DEFAULT resolves
	// to container-sandboxed execution, NONE is forbidden. This is the
	// production default for compliance-sensitive deployments and the
	// default for agent daemons.
	SandboxPolicyRequired SandboxPolicy = 2
)

// String returns the canonical wire name for a SandboxPolicy.
func (p SandboxPolicy) String() string {
	switch p {
	case SandboxPolicyRequired:
		return "required"
	case SandboxPolicyPreferred:
		return "preferred"
	case SandboxPolicyOptIn:
		return "opt-in"
	default:
		return "unknown"
	}
}

// ParseSandboxPolicy decodes a string (as produced by String) back into a
// SandboxPolicy. Empty string maps to SandboxPolicyOptIn, matching the
// Go zero value. CLI flag consumers that want "required" as the default
// should set that default at the flag declaration site rather than relying
// on empty-string parsing.
func ParseSandboxPolicy(s string) (SandboxPolicy, error) {
	switch s {
	case "", "opt-in", "optin", "opt_in":
		return SandboxPolicyOptIn, nil
	case "preferred":
		return SandboxPolicyPreferred, nil
	case "required":
		return SandboxPolicyRequired, nil
	default:
		return SandboxPolicyOptIn, &InputValidationError{
			Field:   "sandbox_policy",
			Message: "must be one of: required, preferred, opt-in",
		}
	}
}

// SandboxModeViolationError signals that a request's SandboxMode cannot be
// satisfied under the active SandboxPolicy (possibly after tenant override)
// or because the target host lacks a container runtime.
type SandboxModeViolationError struct {
	Requested SandboxMode
	Policy    SandboxPolicy
	Reason    string
}

func (e *SandboxModeViolationError) Error() string {
	return "sandbox mode " + e.Requested.String() + " not permitted under policy " +
		e.Policy.String() + ": " + e.Reason
}

// Reasons a backend refuses a spawn that demands a sandbox. They are constants
// rather than literals at the raise sites because they are ASSERTED ON: the
// tests that pin the fail-closed guard (#741) check which refusal was given,
// and a refusal whose wording drifts is a test that stops testing anything.
const (
	// sandboxReasonNoRuntime: the backend was never given a container runtime,
	// so it has nothing to build a sandbox with.
	sandboxReasonNoRuntime = "backend has no container runtime configured"

	// sandboxReasonCLIMode: the backend drives its agent over a CLI process's
	// stdio and the only process it can start is a host process. Refusing
	// keeps `required` fail-closed instead of degrading to an unsandboxed exec
	// on the daemon with the tenant's provider credential in its environment.
	sandboxReasonCLIMode = "CLI-mode backend cannot run its agent in a container"
)

// ResolveSandboxMode resolves the effective sandbox mode for a request given
// the server's policy, an optional tenant-level policy override, and whether
// a container runtime is available on the target host.
//
// A tenant override may only TIGHTEN the server policy, never loosen it. It
// exists so a tenant with a stricter compliance obligation (FedRAMP/HIPAA
// forcing SandboxPolicyRequired) can raise the floor above the deployment
// default; it must never become the way one tenant's record buys its way out of
// the operator's baseline. The constants are ordered by strictness
// (opt-in < preferred < required), so the stricter of the two wins.
//
// Returns SandboxModeViolationError if the request cannot be honored.
func ResolveSandboxMode(
	requested SandboxMode,
	serverPolicy SandboxPolicy,
	tenantOverride *SandboxPolicy,
	hasContainerRuntime bool,
) (SandboxMode, error) {
	effective := serverPolicy
	if tenantOverride != nil && *tenantOverride > effective {
		effective = *tenantOverride
	}

	switch requested {
	case SandboxModeRequired:
		if !hasContainerRuntime {
			return SandboxModeRequired, &SandboxModeViolationError{
				Requested: requested,
				Policy:    effective,
				Reason:    "no container runtime available on target host",
			}
		}
		return SandboxModeRequired, nil

	case SandboxModeNone:
		if effective == SandboxPolicyRequired {
			return SandboxModeNone, &SandboxModeViolationError{
				Requested: requested,
				Policy:    effective,
				Reason:    "raw exec forbidden under this policy",
			}
		}
		return SandboxModeNone, nil

	case SandboxModeDefault:
		switch effective {
		case SandboxPolicyRequired, SandboxPolicyPreferred:
			if !hasContainerRuntime {
				if effective == SandboxPolicyRequired {
					return SandboxModeRequired, &SandboxModeViolationError{
						Requested: requested,
						Policy:    effective,
						Reason:    "policy requires container sandbox but runtime is unavailable",
					}
				}
				return SandboxModeNone, nil
			}
			return SandboxModeRequired, nil
		case SandboxPolicyOptIn:
			return SandboxModeNone, nil
		default:
			return SandboxModeRequired, &SandboxModeViolationError{
				Requested: requested,
				Policy:    effective,
				Reason:    "unknown sandbox policy",
			}
		}

	default:
		return SandboxModeDefault, &SandboxModeViolationError{
			Requested: requested,
			Policy:    effective,
			Reason:    "unknown sandbox mode",
		}
	}
}
