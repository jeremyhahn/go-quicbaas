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

// SetJobLifecyclePolicy installs the operator's lifecycle policy — the
// command allowlist the command validator enforces and the bound on each
// lifecycle moment. Every control loop built over this service takes the
// policy from it (JobLifecyclePolicyProvider) when its own is not set.
func (s *agentServiceImpl) SetJobLifecyclePolicy(policy JobLifecyclePolicy) {
	s.lifecyclePolicy = policy
}

// JobLifecyclePolicy implements JobLifecyclePolicyProvider.
func (s *agentServiceImpl) JobLifecyclePolicy() JobLifecyclePolicy {
	return s.lifecyclePolicy
}
