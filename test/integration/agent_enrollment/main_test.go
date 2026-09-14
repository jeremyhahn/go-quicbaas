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

//go:build integration && rest

// Package agent_enrollment_test is the end-to-end scenario an agent daemon
// actually performs against the control plane, and the one the fleet was never
// tested on: enroll over mTLS, then call the control plane presenting BOTH its
// client certificate and the bearer its enrollment minted.
//
// It is wired from the production types throughout — the real register
// handler, the real IAM identity enrollment, the real fleet registry over a
// tenant-routing store, and go-qrdb's real mTLS authenticator, RBAC authorizer
// and REST middleware — because both defects it guards (go-quicbaas#663) are
// invisible to any test that substitutes one of those:
//
//   - the daemon presents a certificate on every call, so qrdb authenticates
//     it as the CERTIFICATE's subject and the grant on its OAuth2 application
//     never applies. Only the real authenticator chain shows that.
//   - the registry write and the registry read resolved to different tenant
//     namespaces. Only a store that routes by the context tenant shows that.
package agent_enrollment_test

import (
	"testing"
)

// TestMain lets `go test` discover the package; every test wires its own
// server, registry and identity store.
func TestMain(m *testing.M) {
	m.Run()
}
