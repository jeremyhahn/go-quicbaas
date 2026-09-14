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

package sdlc

import (
	"strconv"
	"strings"
)

// =============================================================================
// Discovery — how a project learns that something happened on its forge
// =============================================================================
//
// THERE ARE EXACTLY TWO PATHS AND THE PROJECT CHOOSES WHICH IT RUNS. A
// delivery arrives because the forge pushed it; a sweep finds the same fact
// because this node went and looked. They answer the same question at
// different latencies and with different failure modes, which is why the
// default runs BOTH: webhook delivery is at-least-once and droppable, so a
// deployment that deletes the sweep trades a token cost for a liveness bug —
// one lost delivery strands a change until a person notices. Measured on the
// burn-in node, 2026-09-13: a repository with no webhook provisioned at all
// left two finished pull requests permanently unmergeable, because the review
// status branch protection requires was never written and nothing ever looked
// again (go-quicbaas#933).
//
// THE MODES ARE INDEPENDENT, NOT A FLAG IN FRONT OF ONE CODE PATH. `poll`
// must work on a node with no ingress configured at all, and `webhook` must
// not silently degrade to polling — so each trigger asks this type whether
// the path it IS may act, and a project that admits neither is refused at
// bind time rather than discovered to be inert.

// DiscoveryMode selects which discovery paths a project runs.
type DiscoveryMode string

const (
	// DiscoveryBoth is the default: a delivery acts immediately and the
	// sweep reconciles whatever the delivery path missed.
	DiscoveryBoth DiscoveryMode = "both"

	// DiscoveryPoll runs the sweep only. For a forge that cannot reach this
	// node — air-gapped, NAT, no inbound route — and for a forge with no
	// webhook support. A delivery that arrives anyway is ignored.
	DiscoveryPoll DiscoveryMode = "poll"

	// DiscoveryWebhook runs the delivery path only, for a trusted and
	// observed delivery path where latency matters more than the floor.
	DiscoveryWebhook DiscoveryMode = "webhook"
)

// DiscoveryPath is which of the two paths is asking to act.
type DiscoveryPath string

const (
	// DiscoveryPathDelivery is a verified webhook delivery.
	DiscoveryPathDelivery DiscoveryPath = "delivery"

	// DiscoveryPathSweep is a periodic pass over the project's forge.
	DiscoveryPathSweep DiscoveryPath = "sweep"
)

// discoveryPaths says which paths each mode admits. Map-based so a mode
// answers in constant time and so the matrix is data a test can walk rather
// than a condition a test has to re-derive.
var discoveryPaths = map[DiscoveryMode]map[DiscoveryPath]bool{
	DiscoveryBoth:    {DiscoveryPathDelivery: true, DiscoveryPathSweep: true},
	DiscoveryPoll:    {DiscoveryPathSweep: true},
	DiscoveryWebhook: {DiscoveryPathDelivery: true},
}

// Effective is the mode that applies, which is DiscoveryBoth for a binding
// that names none.
//
// THE ZERO VALUE IS THE DEFAULT AND THE DEFAULT IS BOTH, so a binding written
// before this field existed keeps the delivery path it had and gains the
// floor. Trimmed and lowercased because the value arrives from an operator's
// JSON or YAML; Validate refuses anything that is still not a mode after
// that, so no unknown value can normalise into a working one.
func (m DiscoveryMode) Effective() DiscoveryMode {
	normalised := DiscoveryMode(strings.ToLower(strings.TrimSpace(string(m))))
	if normalised == "" {
		return DiscoveryBoth
	}
	return normalised
}

// Admits reports whether this mode lets path act.
//
// EVERY TRIGGER ASKS BEFORE IT ACTS, and there are two of them: DispatchReview
// for a delivery and ReviewSweeper for a sweep. A third would ask here too.
func (m DiscoveryMode) Admits(path DiscoveryPath) bool {
	return discoveryPaths[m.Effective()][path]
}

// Validate refuses a mode this module does not implement.
//
// REFUSED AT BIND TIME, because the alternative is a project that looks
// configured and discovers nothing: an unknown mode admits no path, so its
// pull requests would never be reviewed and its issues never swept, with
// nothing anywhere saying why.
func (m DiscoveryMode) Validate() error {
	if _, ok := discoveryPaths[m.Effective()]; ok {
		return nil
	}
	return &InvalidInputError{
		Field: "discovery",
		Message: "is " + strconv.Quote(string(m)) + "; must be " +
			string(DiscoveryPoll) + ", " + string(DiscoveryWebhook) + " or " +
			string(DiscoveryBoth) + " (empty takes " + string(DiscoveryBoth) + ")",
	}
}

// String implements fmt.Stringer with the effective mode, so a log line about
// a binding that names none says which mode is actually live.
func (m DiscoveryMode) String() string { return string(m.Effective()) }
