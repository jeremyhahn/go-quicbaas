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

//go:build integration

// Package agent_admin_test contains integration tests for the agent admin
// disable/enable lifecycle and its side effects (OAuth2 client revocation,
// mTLS pin removal, audit event emission).
//
// These tests intentionally drive service.AgentAdminService directly rather
// than through the embedded SDK client. The QuicBaaS facade currently
// constructs its AgentAdminService with a private in-memory registry that
// callers cannot share with the orchestration AgentService — so registering
// an agent through client.Orchestrator().RegisterAgent() does not make it
// visible to client.Orchestrator().DisableAgent(). This is documented as a
// production wiring gap in the deliverable status report.
//
// Direct service-layer testing keeps the test honest: it exercises the
// real DisableAgent/EnableAgent code paths and verifies all observable
// side-effects via injected test doubles.
package agent_admin_test

import (
	"sync/atomic"
	"testing"
)

var idCounter uint64

func uniqueSuffix(prefix string) string {
	return prefix + "-" + itoa(atomic.AddUint64(&idCounter, 1))
}

func itoa(v uint64) string {
	// Fast unsigned-int formatter without strconv import to keep helpers tight.
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}

// TestMain is a placeholder that lets `go test` discover this package.
// No process-level setup is needed; each test wires its own registry.
func TestMain(m *testing.M) {
	m.Run()
}
