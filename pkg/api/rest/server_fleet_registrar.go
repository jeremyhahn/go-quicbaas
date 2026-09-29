//go:build rest

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

package rest

// SetFleetRegistrar wires the fleet-registry registrar used by the
// mTLS agent-register handler. The caller should pass an
// iam.FleetRegistrar implementation; the server stores it as an opaque
// interface{} so pkg/api/rest does not pull the iam package into the
// untagged build.
//
// Wired from pkg/server lifecycle after the IAM components are
// initialized. Passing nil disables the fleet-registry publish step;
// the daemon's first heartbeat then serves as its announcement.
func (s *Server) SetFleetRegistrar(registrar interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fleetRegistrar = registrar
}
