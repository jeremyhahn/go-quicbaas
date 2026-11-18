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

package sdlc

import "testing"

// TestEntityIDFor_PinnedValue pins the FNV-1a 64 entity id of a known
// reference. The id addresses stored bindings and deliveries, so the
// algorithm must never change.
func TestEntityIDFor_PinnedValue(t *testing.T) {
	const want uint64 = 0xedddb03a469be32f
	if got := entityIDFor("project-42"); got != want {
		t.Fatalf("entityIDFor = %#x, want %#x", got, want)
	}
}
