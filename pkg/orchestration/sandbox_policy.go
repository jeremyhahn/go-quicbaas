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

// ResolveSandboxMode resolves the effective sandbox mode for a request given
// the server's policy, an optional tenant-level policy override, and whether
// a container runtime is available on the target host.
//
// The tenant override, when non-nil, takes precedence over the server policy.
// This supports per-tenant compliance requirements (e.g., FedRAMP/HIPAA
// tenants forcing SandboxPolicyRequired regardless of server default).
//
// Returns SandboxModeViolationError if the request cannot be honored.
func ResolveSandboxMode(
	requested SandboxMode,
	serverPolicy SandboxPolicy,
	tenantOverride *SandboxPolicy,
	hasContainerRuntime bool,
) (SandboxMode, error) {
	effective := serverPolicy
	if tenantOverride != nil {
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
