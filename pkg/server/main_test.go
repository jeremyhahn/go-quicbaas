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
	"os"
	"testing"
)

// TestMain runs pkg/server's test binary with an unbounded embedded-node
// slot. This one test binary builds hundreds of Servers, some of them in
// parallel and many without stopping the node they start, so the
// one-node-per-process guard would refuse them. The guard's own tests
// (qrdb_node_slot_test.go) swap a real slot in.
func TestMain(m *testing.M) {
	activeNodeSlot.Store(&nodeSlot{unbounded: true})
	os.Exit(m.Run())
}
