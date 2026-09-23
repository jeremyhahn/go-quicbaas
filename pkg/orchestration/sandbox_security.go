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

import (
	"strconv"
	"strings"
)

// This file holds the PRIVILEGE dimension of the sandbox axis
// (sandbox_policy.go decides whether the agent is in a container at all,
// sandbox_network.go which network namespace it gets).
//
// A worker has passwordless sudo inside its container (#927): it installs its
// own build dependencies, so root inside the container is reachable by design,
// and the container is the boundary. What root holds there is therefore the
// width of that boundary. Left to the runtime it is Docker's whole default
// capability set — NET_RAW, MKNOD, SETFCAP, SYS_CHROOT, KILL, AUDIT_WRITE and
// the rest — and no bound on processes, so one fork loop exhausts the host's
// pid space. A live worker used exactly that root to install and start a
// Docker daemon inside itself (#1040).
//
// The policy is: drop EVERY capability, add back only what sudo and apk are
// demonstrated to need, and bound the process count. Two things are
// deliberately NOT done:
//
//   - no --security-opt of any kind: the runtime's default seccomp and
//     AppArmor profiles stay in force, and nothing here may loosen them;
//   - no no-new-privileges: sudo is a setuid binary, and that flag is
//     precisely what stops a setuid binary from raising privilege, so it would
//     take away the grant #927 exists to give.

// DefaultSandboxCapAdd is the capability set a sandboxed container is given
// back after every capability is dropped. It is what the worker's sudo and
// apk need and nothing more:
//
//   - SETUID, SETGID: sudo switching to root and back (setresuid, setgroups);
//   - CHOWN, FOWNER: apk setting the ownership and mode of installed files;
//   - DAC_OVERRIDE: root writing to paths whose mode bits do not name it.
//
// Anything else is added only with a demonstrated failing command, through
// SandboxSecurity.CapAdd.
var DefaultSandboxCapAdd = []string{"CHOWN", "DAC_OVERRIDE", "FOWNER", "SETGID", "SETUID"}

// DefaultSandboxPidsLimit is the process-and-thread bound a sandboxed
// container gets when none is configured.
//
// It is sized from a measurement, not a guess: a full `make test` of this
// repository — the heaviest workload a worker runs — peaked at 609 tasks on a
// 24-core host (the cgroup's pids.peak, so an upper bound). Go threads
// count against the limit and scale with GOMAXPROCS, which is the host's core
// count, and the agent CLI, gopls and golangci-lint run alongside the tests,
// so the default carries roughly 6.7x headroom over that peak rather than
// sitting just above it.
const DefaultSandboxPidsLimit int64 = 4096

// sandboxCapDropAll is the --cap-drop value that removes every capability.
const sandboxCapDropAll = "ALL"

// SandboxSecurity is the operator's privilege policy for sandboxed agent
// containers. Its zero value is the secure default: DefaultSandboxCapAdd and
// DefaultSandboxPidsLimit. It is operator configuration only; a spawn request
// can never widen it (see Apply).
type SandboxSecurity struct {
	// CapAdd REPLACES DefaultSandboxCapAdd when non-empty: the capabilities
	// given back after --cap-drop ALL. Either spelling ("CAP_NET_RAW" or
	// "NET_RAW") and either case is accepted; "ALL" is refused, because it
	// would undo the drop.
	CapAdd []string `json:"cap_add,omitempty"`

	// PidsLimit bounds processes plus threads in the container. Zero takes
	// DefaultSandboxPidsLimit; a negative value is refused — the policy has no
	// "unlimited".
	PidsLimit int64 `json:"pids_limit,omitempty"`
}

// Validate reports a policy that cannot be applied: a negative pids limit, an
// empty or malformed capability name, or "ALL" among the add-backs.
func (p SandboxSecurity) Validate() error {
	if p.PidsLimit < 0 {
		return &InputValidationError{
			Field:   "sandbox_pids_limit",
			Message: "cannot be negative, got " + strconv.FormatInt(p.PidsLimit, 10),
		}
	}
	for _, raw := range p.CapAdd {
		if _, err := normalizeCapability(raw); err != nil {
			return err
		}
	}
	return nil
}

// Apply sets the policy on spec: every capability dropped, the policy's
// add-back set granted, the pids limit bounded, and privileged mode off.
//
// It OVERRIDES whatever the spec carried, exactly as the resolved network
// does: a caller-supplied ContainerConfig must not be able to name a wider
// capability set, lift the pids bound, or ask for --privileged. It is
// called while the spec is being built, before ContainerConfigDigest
// measures it, so the measured configuration is the launched one.
//
// It returns the policy's validation error, if any, and leaves spec untouched
// in that case.
func (p SandboxSecurity) Apply(spec *ContainerSpec) error {
	if spec == nil {
		return &InputValidationError{Field: "spec", Message: "cannot be nil"}
	}
	if err := p.Validate(); err != nil {
		return err
	}
	capAdd, err := p.EffectiveCapAdd()
	if err != nil {
		return err
	}
	spec.Privileged = false
	spec.CapDrop = []string{sandboxCapDropAll}
	spec.CapAdd = capAdd
	spec.PidsLimit = p.EffectivePidsLimit()
	return nil
}

// EffectiveCapAdd returns the normalized, bare-name capability set the policy
// grants: CapAdd when configured, DefaultSandboxCapAdd otherwise. The result
// is a fresh slice the caller may keep.
func (p SandboxSecurity) EffectiveCapAdd() ([]string, error) {
	source := p.CapAdd
	if len(source) == 0 {
		source = DefaultSandboxCapAdd
	}
	seen := make(map[string]struct{}, len(source))
	out := make([]string, 0, len(source))
	for _, raw := range source {
		name, err := normalizeCapability(raw)
		if err != nil {
			return nil, err
		}
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return sortedCopy(out), nil
}

// EffectivePidsLimit returns the configured pids limit, or
// DefaultSandboxPidsLimit when none is configured.
func (p SandboxSecurity) EffectivePidsLimit() int64 {
	if p.PidsLimit > 0 {
		return p.PidsLimit
	}
	return DefaultSandboxPidsLimit
}

// sandboxSecurityHolder is implemented by every backend that carries an
// operator-configured SandboxSecurity: the ACP backends and the OpenVINO tool
// backend.
type sandboxSecurityHolder interface {
	sandboxSecurity() SandboxSecurity
}

// sandboxSecurity returns the operator's privilege policy for this backend.
func (b *ACPBackend) sandboxSecurity() SandboxSecurity { return b.config.SandboxSecurity }

// sandboxSecurity returns the operator's privilege policy for this backend.
func (b *OpenVINOBackend) sandboxSecurity() SandboxSecurity { return b.config.SandboxSecurity }

// sandboxSecurityFor returns the privilege policy for a container the
// orchestrator launches itself on behalf of backend: the backend's own
// configured policy when it carries one (ACP, OpenVINO), and the secure
// zero-value default for every other backend.
//
// A LOCAL OPENVINO BACKEND TAKES THIS PATH: SpawnSession treats only an ACP
// backend holding a runtime as launching its own container, so recognizing
// ACP alone here would drop an operator's configured limit to the default.
func sandboxSecurityFor(backend AgentBackend) SandboxSecurity {
	if holder, ok := backend.(sandboxSecurityHolder); ok {
		return holder.sandboxSecurity()
	}
	return SandboxSecurity{}
}

// normalizeCapability returns a capability in the bare upper-case form
// ("NET_RAW") the runtime accepts, or an error for a name that is empty,
// malformed, or "ALL".
func normalizeCapability(raw string) (string, error) {
	name := strings.ToUpper(strings.TrimSpace(raw))
	name = strings.TrimPrefix(name, "CAP_")
	if name == "" {
		return "", &InputValidationError{Field: "sandbox_cap_add", Message: "capability name cannot be empty"}
	}
	if name == sandboxCapDropAll {
		return "", &InputValidationError{
			Field:   "sandbox_cap_add",
			Message: "ALL cannot be added back: it would undo --cap-drop ALL",
		}
	}
	for _, r := range name {
		if (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '_' {
			return "", &InputValidationError{
				Field:   "sandbox_cap_add",
				Message: "malformed capability name " + strconv.Quote(raw),
			}
		}
	}
	return name, nil
}
