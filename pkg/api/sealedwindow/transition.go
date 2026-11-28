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

package sealedwindow

import (
	qrdbsealedwindow "github.com/jeremyhahn/go-qrdb/pkg/api/sealedwindow"
)

// Transition reports whether v can move the barrier out of the sealed state
// (initialize or unseal it). The flag is read from qrdb's canonical table,
// whose verb identifiers this table shares, so the two cannot disagree. On the
// boot-time sealed-window listener a Transition request holds that listener's
// Admission exclusively (see qrdbsealedwindow.Admission).
func (v Verb) Transition() bool {
	return qrdbsealedwindow.Table[qrdbsealedwindow.Verb(v)].Transition
}

// RESTTransition lists the allowlisted REST entries of the Transition verbs.
func RESTTransition() []string {
	var out []string
	seen := make(map[string]struct{})
	for _, v := range Verbs {
		if Excluded(v) || !v.Transition() {
			continue
		}
		for _, p := range Table[v].REST {
			if _, dup := seen[p]; dup {
				continue
			}
			seen[p] = struct{}{}
			out = append(out, p)
		}
	}
	return out
}

// IsRESTTransition reports whether a REST request for method and path is a
// Transition verb.
func IsRESTTransition(method, path string) bool {
	for _, entry := range RESTTransition() {
		if qrdbsealedwindow.RESTEntryMatches(entry, method, path) {
			return true
		}
	}
	return false
}

// GRPCFullMethodTransition is the set of allowlisted fully-qualified gRPC
// methods (see GRPCFullMethodAllowlist) whose verb is a Transition verb.
func GRPCFullMethodTransition() map[string]struct{} {
	tails := make(map[string]struct{})
	for _, v := range Verbs {
		if Excluded(v) || !v.Transition() {
			continue
		}
		for _, t := range Table[v].GRPC {
			tails[t] = struct{}{}
		}
	}
	out := make(map[string]struct{})
	for _, m := range GRPCFullMethodAllowlist() {
		if _, ok := tails[methodTail(m)]; ok {
			out[m] = struct{}{}
		}
	}
	return out
}
