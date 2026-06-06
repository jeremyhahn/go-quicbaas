// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
//  1. GNU Affero General Public License v3.0 (AGPL-3.0)
//     See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
//  2. Commercial License
//     Contact licensing@automatethethings.com for commercial licensing options.

package signalbridge

import "testing"

// TestConversationKind_String covers all named kinds plus the unknown
// fallback so a future enum value renders predictably.
func TestConversationKind_String(t *testing.T) {
	cases := []struct {
		kind ConversationKind
		want string
	}{
		{ConversationDM, "dm"},
		{ConversationGroup, "group"},
		{ConversationKind(0), "unknown"},
		{ConversationKind(99), "unknown"},
	}
	for _, tc := range cases {
		if got := tc.kind.String(); got != tc.want {
			t.Fatalf("ConversationKind(%d).String() = %q, want %q", tc.kind, got, tc.want)
		}
	}
}
