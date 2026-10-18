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

package batch

import (
	"fmt"
	"os"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// TestMain deletes the data-plane tenant this package's tests acted in once
// they have all run, keeping m.Run's exit code.
func TestMain(m *testing.M) {
	code := m.Run()
	if err := helpers.ReleaseDataPlaneTenants(); err != nil {
		fmt.Fprintf(os.Stderr, "release data-plane tenants: %v\n", err)
	}
	os.Exit(code)
}
