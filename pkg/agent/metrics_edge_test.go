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

package agent

import (
	"testing"
)

// TestSandboxLabel_SandboxedReturnsLabel verifies the sandboxed=true branch.
func TestSandboxLabel_SandboxedReturnsLabel(t *testing.T) {
	t.Parallel()
	if got := sandboxLabel(true); got != "sandboxed" {
		t.Errorf("sandboxLabel(true): got %q, want %q", got, "sandboxed")
	}
}

// TestSandboxLabel_HostReturnsLabel verifies the sandboxed=false branch.
func TestSandboxLabel_HostReturnsLabel(t *testing.T) {
	t.Parallel()
	if got := sandboxLabel(false); got != "host" {
		t.Errorf("sandboxLabel(false): got %q, want %q", got, "host")
	}
}
