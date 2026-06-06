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

//go:build integration

// Package standalone runs Phase F integration tests for QuicBaaS standalone
// mode (no Raft, single-node qrdb engine, in-process embedded server).
package standalone

import (
	"os"
	"testing"
)

// TestMain is the package entry point. No shared server is needed here
// because each test manages its own temporary data directory and server
// lifecycle, ensuring complete isolation between test cases.
func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
