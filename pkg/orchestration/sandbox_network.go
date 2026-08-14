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

// This file holds the NETWORK dimension of the sandbox axis (sandbox_policy.go
// holds the other: is the agent in a container at all?).
//
// Sandboxing an agent has two independent isolation dimensions, and running a
// container with --network host disables one of them completely: the container
// joins the host's own network namespace, so every loopback-bound service
// reaches it — the daemon's admin listeners, local metrics endpoints, a
// 127.0.0.1 datastore — precisely the services whose host firewall rules assume
// no container can reach them. A container-sandboxed agent that can still
// dial 127.0.0.1 is only half sandboxed.
//
// The default is therefore an isolated network namespace. Host networking
// remains available, but only as an explicit operator decision recorded in
// configuration (ACPBackendConfig.SandboxNetwork); it can never be selected by
// the caller of a spawn.

// SandboxNetwork is the container network a sandboxed agent session runs in.
// Besides the three named modes below, any operator-defined network name
// (a docker/podman user-defined bridge, for example) is accepted.
type SandboxNetwork string

const (
	// SandboxNetworkBridge gives the container its own network namespace with
	// outbound connectivity through the runtime's NAT bridge. It is the
	// default: an ACP agent needs egress to reach its provider API
	// (api.anthropic.com, a remote Ollama, an OIDC issuer) but has no
	// legitimate need to reach the host's loopback services.
	SandboxNetworkBridge SandboxNetwork = "bridge"

	// SandboxNetworkNone gives the container a network namespace with only a
	// loopback device — no egress at all. Appropriate for agents that talk
	// exclusively over their stdio ACP transport.
	SandboxNetworkNone SandboxNetwork = "none"

	// SandboxNetworkHost shares the HOST's network namespace with the
	// container, which removes network isolation entirely. Only reachable by
	// explicit operator configuration; see ParseSandboxNetwork.
	SandboxNetworkHost SandboxNetwork = "host"
)

// SandboxNetworkDefault is the network a sandboxed session runs in when the
// operator configured none.
const SandboxNetworkDefault = SandboxNetworkBridge

// String returns the network name passed to the container runtime.
func (n SandboxNetwork) String() string {
	return string(n)
}

// IsolatesHost reports whether the container gets its own network namespace,
// i.e. whether the host's loopback services are out of reach.
func (n SandboxNetwork) IsolatesHost() bool {
	return n != SandboxNetworkHost
}

// ParseSandboxNetwork validates an operator-configured network name and
// resolves the empty value to SandboxNetworkDefault.
//
// Any syntactically valid network name is accepted, including
// SandboxNetworkHost: reaching this function at all means the value came from
// server configuration, which IS the explicit operator policy that host
// networking requires. What the type prevents is a network arriving from
// anywhere else — a SpawnRequest carries no network field, so a tenant cannot
// ask for the host namespace.
func ParseSandboxNetwork(s string) (SandboxNetwork, error) {
	name := strings.TrimSpace(s)
	if name == "" {
		return SandboxNetworkDefault, nil
	}
	if !isValidNetworkName(name) {
		return SandboxNetworkDefault, &InputValidationError{
			Field:   "sandbox_network",
			Message: "must be a container network name (letters, digits, '_', '.', '-'), or one of: bridge, none, host",
		}
	}
	return SandboxNetwork(name), nil
}

// isValidNetworkName reports whether name is a legal docker/podman network
// name: an alphanumeric first character followed by alphanumerics, '_', '.'
// or '-'. Rejecting anything else keeps a configuration typo from becoming a
// stray argument on the runtime command line.
func isValidNetworkName(name string) bool {
	if name == "" || !isAlphanumeric(name[0]) {
		return false
	}
	for i := 1; i < len(name); i++ {
		c := name[i]
		if !isAlphanumeric(c) && c != '_' && c != '.' && c != '-' {
			return false
		}
	}
	return true
}

// isAlphanumeric reports whether c is an ASCII letter or digit.
func isAlphanumeric(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// sandboxNetwork returns the container network this backend launches sandboxed
// sessions in, resolving the unset configuration value to
// SandboxNetworkDefault.
//
// A configured value that does not parse is NOT silently replaced by the
// operator's intent-of-the-day: it falls back to the isolated default, which is
// the safe direction, and says so once. An operator who configured host
// networking is also warned once, because that configuration turns off half of
// what sandboxing provides and should be visible in the log of every daemon
// that runs with it.
func (b *ACPBackend) sandboxNetwork() SandboxNetwork {
	network, err := ParseSandboxNetwork(b.config.SandboxNetwork.String())
	if err != nil {
		b.sandboxNetworkWarning.Do(func() {
			if b.logger != nil {
				b.logger.Warnf("acp_backend: invalid sandbox_network %q (%v); using %s",
					b.config.SandboxNetwork, err, SandboxNetworkDefault)
			}
		})
		return SandboxNetworkDefault
	}
	if !network.IsolatesHost() {
		b.sandboxNetworkWarning.Do(func() {
			if b.logger != nil {
				b.logger.Warnf("acp_backend: sandbox_network=%s — sandboxed sessions share the host network namespace "+
					"and can reach every loopback-bound service on this host; this is operator-configured, not the default",
					network)
			}
		})
	}
	return network
}
