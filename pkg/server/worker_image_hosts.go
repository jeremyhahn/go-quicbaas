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

package server

import (
	"context"
	"net"
	"os"
	"sort"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc/workerimage"
)

// localFleetNodeHosts resolves, from the fleet registry, the agent registered from
// THIS node: the one whose container engine is the engine the worker image
// refresh built its candidate on, and so the only one a smoke may run on.
//
// WHY NOT os.Hostname() DIRECTLY. A smoke is placed by host affinity, which
// matches an agent's registered NAME or HOST — and those are what the agent
// reported, not what this process calls itself. The embedded agent registers
// as "embedded-<hostname>", an `fleet serve` daemon as its hostname, possibly
// with a port. Resolving the registration and answering with its name is what
// makes the affinity match the agent that actually exists.
type localFleetNodeHosts struct {
	registry orchestration.FleetRegistry
	hostname func() (string, error)
}

var (
	_ workerimage.HostResolver = (*localFleetNodeHosts)(nil)
	_ workerimage.FleetNodes   = (*localFleetNodeHosts)(nil)
)

// newLocalFleetNodeHosts resolves against registry; nil yields a resolver that
// always refuses, so a node without a fleet registry fails its smokes loudly.
func newLocalFleetNodeHosts(registry orchestration.FleetRegistry) *localFleetNodeHosts {
	return &localFleetNodeHosts{registry: registry, hostname: os.Hostname}
}

// LocalFleetNodeHost implements workerimage.HostResolver. It prefers this node's
// embedded agent — in-process, so necessarily local — and otherwise a daemon
// whose registered host is this node's hostname. The answer is the agent's
// NAME, which the affinity matches exactly.
func (h *localFleetNodeHosts) LocalFleetNodeHost(ctx context.Context, tenantID, backend string) (string, error) {
	if h.registry == nil {
		return "", &LocalAgentNotFoundError{Backend: backend}
	}
	hostname, ownEmbedded, err := h.identity()
	if err != nil {
		return "", &LocalAgentNotFoundError{Backend: backend, Cause: err}
	}
	agents, err := h.registry.ListFleetNodes(ctx, tenantID, &orchestration.FleetNodeFilter{Backend: backend})
	if err != nil {
		return "", &LocalAgentNotFoundError{Backend: backend, Hostname: hostname, Cause: err}
	}
	var daemon string
	var seen []string
	for _, a := range agents {
		if a == nil || a.DisabledAt != 0 {
			continue
		}
		seen = append(seen, a.Name+"@"+a.Host)
		if !isLocalFleetNode(a, hostname, ownEmbedded) {
			continue
		}
		if a.EffectiveKind() == orchestration.FleetNodeKindEmbedded {
			return a.Name, nil
		}
		if daemon == "" {
			daemon = a.Name
		}
	}
	if daemon != "" {
		return daemon, nil
	}
	sort.Strings(seen)
	return "", &LocalAgentNotFoundError{Backend: backend, Hostname: hostname, Seen: seen}
}

// RemoteFleetNodes implements workerimage.FleetNodes: every node registered in
// the tenant's fleet that is not this one, as "name@host". Disabled nodes
// count — re-enabling one must not find it bound to an image it cannot pull.
// A node with no fleet registry has no fleet beyond itself.
func (h *localFleetNodeHosts) RemoteFleetNodes(ctx context.Context, tenantID string) ([]string, error) {
	if h.registry == nil {
		return nil, nil
	}
	hostname, ownEmbedded, err := h.identity()
	if err != nil {
		return nil, &FleetTopologyError{TenantID: tenantID, Cause: err}
	}
	nodes, err := h.registry.ListFleetNodes(ctx, tenantID, nil)
	if err != nil {
		return nil, &FleetTopologyError{TenantID: tenantID, Cause: err}
	}
	var remote []string
	for _, node := range nodes {
		if node != nil && !isLocalFleetNode(node, hostname, ownEmbedded) {
			remote = append(remote, node.Name+"@"+node.Host)
		}
	}
	sort.Strings(remote)
	return remote, nil
}

// identity is this node's hostname, as the fleet registry records it, and the
// id this node's embedded agent registers under.
func (h *localFleetNodeHosts) identity() (string, string, error) {
	hostname, err := h.hostname()
	if err != nil {
		return "", "", err
	}
	if hostname == "" {
		// The embedded agent registers a host it cannot name as localhost.
		hostname = "localhost"
	}
	return hostname, embeddedFleetNodeID(hostname), nil
}

// isLocalFleetNode reports whether a registration is this node's own.
//
// The fleet registry is shared by every node, so an embedded registration is
// not local merely for being embedded: node B lists embedded-A too. Only the
// registration carrying the id this node's embedded agent registers under is
// ours, whatever host it claims. A daemon is ours when its registered host is
// this node's hostname.
func isLocalFleetNode(node *orchestration.FleetNode, hostname, ownEmbedded string) bool {
	if node.EffectiveKind() == orchestration.FleetNodeKindEmbedded {
		return node.ID == ownEmbedded || node.Name == ownEmbedded
	}
	return hostOnly(node.Host) == hostname
}

// hostOnly strips a port from a registered host.
func hostOnly(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}
	return host
}

// LocalAgentNotFoundError is a node with no registered agent of its own for
// the backend a smoke needs.
type LocalAgentNotFoundError struct {
	Backend  string
	Hostname string
	Seen     []string
	Cause    error
}

// Error implements the error interface.
func (e *LocalAgentNotFoundError) Error() string {
	msg := "no agent registered from this node (" + e.Hostname + ") serves " + e.Backend
	if len(e.Seen) > 0 {
		msg += "; registered: " + strings.Join(e.Seen, ", ")
	}
	msg += "; set sdlc.worker_image.smoke_host to the local agent's name"
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap returns the cause.
func (e *LocalAgentNotFoundError) Unwrap() error { return e.Cause }

// FleetTopologyError is a tenant fleet whose nodes could not be told apart
// from this one, so a local-mode promotion cannot be proven safe.
type FleetTopologyError struct {
	TenantID string
	Cause    error
}

// Error implements the error interface.
func (e *FleetTopologyError) Error() string {
	return "cannot tell tenant " + e.TenantID + "'s fleet nodes from this one: " + e.Cause.Error()
}

// Unwrap returns the cause.
func (e *FleetTopologyError) Unwrap() error { return e.Cause }
