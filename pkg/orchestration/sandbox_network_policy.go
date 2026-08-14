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

import "strings"

// This file is the ORCHESTRATOR-LAYER enforcement point for the sandbox network
// dimension defined in sandbox_network.go. That file establishes the policy —
// an isolated namespace by default, host networking only as an explicit
// operator decision — and ACPBackend honors it for the containers it launches
// itself.
//
// One path bypasses that: SpawnSession's own sandbox path launches a
// caller-supplied ContainerSpec (SpawnSessionRequest.ContainerConfig), whose
// Network field the runtime passes straight to --network. Without the clamp
// below, a caller who can reach the spawn verb can ask for --network host and
// join the daemon's own network namespace, reaching every loopback-bound
// service on the host — the admin listeners, local metrics, a 127.0.0.1
// datastore. The network a sandboxed session runs in is therefore resolved the
// same way its trust level is: from operator configuration, never from the
// request.

// SandboxNetworkNotAllowedError reports a spawn request that asked for a
// container network the caller may not select — in practice, host networking,
// which removes network isolation from a session that asked to be sandboxed.
//
// It is refused rather than silently rewritten so the caller learns their
// request was not honored instead of believing they got the host namespace.
type SandboxNetworkNotAllowedError struct {
	// Requested is the network named on the request.
	Requested string

	// Effective is the network the spawn would have run in, resolved from
	// operator configuration.
	Effective SandboxNetwork
}

// Error implements the error interface.
func (e *SandboxNetworkNotAllowedError) Error() string {
	return "sandbox network " + strings.TrimSpace(e.Requested) +
		" not permitted: host networking is an operator decision, not a request field; " +
		"this session runs in " + e.Effective.String()
}

// requestsHostNetwork reports whether a caller-supplied network name selects
// the host's network namespace. The comparison is case-insensitive and
// whitespace-tolerant: only the runtime decides how it canonicalizes a network
// name, and a guard that a different spelling walks past is not a guard.
func requestsHostNetwork(name string) bool {
	return strings.EqualFold(strings.TrimSpace(name), string(SandboxNetworkHost))
}

// resolveSandboxNetwork returns the network a sandboxed spawn must run in, and
// refuses a request that asked for host networking.
//
// The effective network comes from the backend that will run the session — an
// ACP backend carries the operator's ACPBackendConfig.SandboxNetwork — and
// falls back to SandboxNetworkDefault for the backends whose containers the
// orchestrator launches itself. A caller-supplied value never contributes: the
// only thing the request can do with the network is fail the spawn.
func (s *agentServiceImpl) resolveSandboxNetwork(
	backend AgentBackend,
	requested string,
) (SandboxNetwork, error) {
	effective := SandboxNetworkDefault
	if acp, ok := backend.(*ACPBackend); ok {
		effective = acp.sandboxNetwork()
	}
	if requestsHostNetwork(requested) {
		return effective, &SandboxNetworkNotAllowedError{Requested: requested, Effective: effective}
	}
	return effective, nil
}

// containerConfigNetwork returns the network named on a request's full
// container spec, or "" when the request carries no spec.
func containerConfigNetwork(req *SpawnSessionRequest) string {
	if req == nil || req.ContainerConfig == nil {
		return ""
	}
	return req.ContainerConfig.Network
}
