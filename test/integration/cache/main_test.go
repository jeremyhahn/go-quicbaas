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

//go:build integration && quic

package cache

import (
	"fmt"
	"os"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// TestMain releases the tenant the multiprotocol suite's tenant principal is
// provisioned in. The tenant outlives every test, so no t.Cleanup can delete
// it. It carries the build constraint of multiprotocol_test.go, the only file
// of this package that enrols a principal.
func TestMain(m *testing.M) {
	code := m.Run()
	if err := helpers.ReleaseDataPlaneTenants(); err != nil {
		fmt.Fprintf(os.Stderr, "release data-plane tenants: %v\n", err)
	}
	os.Exit(code)
}
