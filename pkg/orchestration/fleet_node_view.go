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

// ServedBackends is every name this fleet node will be matched on: the
// primary Backend first, then each capability, each name once.
//
// AN OPERATOR READING THE REGISTRY MUST SEE WHAT DISPATCH SEES. A daemon that
// serves claude, codex and opencode is one record whose primary Backend names
// only the first, and whose other two live in Capabilities beside traits like
// "gpu". Reading "backend": "claude" off that record, an operator concludes a
// task routed on "codex" cannot land there — and is wrong, because the
// registry's backend filter and the remote backend resolver both match on the
// whole list (FleetNodeServesBackend). This is that list, so the answer a
// listing gives and the answer dispatch gives are the same one
// (go-quicbaas#681).
//
// It is the ONE derivation every transport renders (go-quicbaas#700): REST,
// QUIC and MCP through FleetNodeView, gRPC, Unix and the embedded transport
// field by field. A nil node or a record naming nothing serves an empty list,
// never nil, so "serves nothing" is never confused with "field absent".
func (a *FleetNode) ServedBackends() []string {
	if a == nil {
		return []string{}
	}
	backends := make([]string, 0, len(a.Capabilities)+1)
	seen := make(map[string]struct{}, len(a.Capabilities)+1)
	for _, name := range append([]string{a.Backend}, a.Capabilities...) {
		if name == "" {
			continue
		}
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		backends = append(backends, name)
	}
	return backends
}

// FleetNodeView is a FleetNode as a JSON transport renders it: every field
// of the record, plus the "backends" the record does not itself carry.
//
// Additive by construction: "backend" and "capabilities" are rendered
// unchanged, because the TypeScript SDK's FleetNode and the Agent Fleet UI
// read them.
type FleetNodeView struct {
	*FleetNode

	// Backends is ServedBackends. Never null.
	Backends []string `json:"backends"`
}

// NewFleetNodeView renders one registration.
func NewFleetNodeView(node *FleetNode) FleetNodeView {
	return FleetNodeView{FleetNode: node, Backends: node.ServedBackends()}
}

// FleetNodeViews renders a fleet listing; an empty fleet renders an empty
// list, never null.
func FleetNodeViews(nodes []*FleetNode) []FleetNodeView {
	out := make([]FleetNodeView, 0, len(nodes))
	for _, node := range nodes {
		out = append(out, NewFleetNodeView(node))
	}
	return out
}
