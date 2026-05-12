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

package agent

import (
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// SandboxResolver resolves a client-supplied SandboxMode into an effective
// mode, honoring the daemon's SandboxPolicy and any tenant-level override
// surfaced by the control plane during enrollment.
//
// The daemon enforces the resolved mode before dispatching to ACPBackend or
// OllamaBackend. A SandboxModeViolationError returned here is sent back to
// the control plane as a PERMISSION_DENIED gRPC status code.
type SandboxResolver struct {
	policy           orchestration.SandboxPolicy
	tenantOverride   *orchestration.SandboxPolicy
	hasContainerRT   bool
}

// NewSandboxResolver builds a resolver from the daemon's effective policy.
// hasContainerRT reflects whether the daemon could actually start a
// containerized session right now.
func NewSandboxResolver(policy orchestration.SandboxPolicy, hasContainerRT bool) *SandboxResolver {
	return &SandboxResolver{policy: policy, hasContainerRT: hasContainerRT}
}

// WithTenantOverride returns a resolver copy with the given tenant-level
// override applied. Pass nil to clear a previously-set override.
func (r *SandboxResolver) WithTenantOverride(override *orchestration.SandboxPolicy) *SandboxResolver {
	clone := *r
	clone.tenantOverride = override
	return &clone
}

// Resolve turns a requested mode into an effective mode. Returns a
// *orchestration.SandboxModeViolationError when the request cannot be
// honored under the current policy.
func (r *SandboxResolver) Resolve(requested orchestration.SandboxMode) (orchestration.SandboxMode, error) {
	return orchestration.ResolveSandboxMode(requested, r.policy, r.tenantOverride, r.hasContainerRT)
}

// Policy returns the daemon's configured policy (no tenant override applied).
func (r *SandboxResolver) Policy() orchestration.SandboxPolicy {
	return r.policy
}

// HasContainerRuntime reports whether the daemon believes it can satisfy a
// SandboxModeRequired request right now.
func (r *SandboxResolver) HasContainerRuntime() bool {
	return r.hasContainerRT
}
