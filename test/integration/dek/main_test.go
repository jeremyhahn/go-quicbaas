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

//go:build integration && quic

package dek

import (
	"fmt"
	"os"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// TestMain runs before all tests in this package.
func TestMain(m *testing.M) {
	// Boot embedded server for multi-protocol tests
	_, srv, err := helpers.SetupEmbeddedServer()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Failed to start embedded server: %v\n", err)
	}

	code := m.Run()
	if err := helpers.ReleaseDataPlaneTenants(); err != nil {
		fmt.Fprintf(os.Stderr, "release data-plane tenants: %v\n", err)
	}

	// Cleanup
	if srv != nil {
		helpers.TeardownEmbeddedServer(srv)
	}
	os.Exit(code)
}
