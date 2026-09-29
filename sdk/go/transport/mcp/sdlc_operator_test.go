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

package mcp

import (
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/sdlc"
)

func TestSDLCMCP_OperatorToolsAndTransforms(t *testing.T) {
	for method, want := range map[string]string{"FileIssue": "sdlc_issue_create", "Collaborators": "sdlc_collaborators"} {
		if got, ok := sdlcToolName(method); !ok || got != want {
			t.Fatalf("%s → %q %v", method, got, ok)
		}
	}
	issue, err := transformSDLCResponse("FileIssue", map[string]any{"number": 88, "title": "Follow-up", "url": "https://f/88", "created": "2026-09-25T00:00:00Z"})
	if err != nil || issue.(*sdlc.ForgeIssue).Number != 88 || issue.(*sdlc.ForgeIssue).URL != "https://f/88" {
		t.Fatalf("FileIssue transform: %+v %v", issue, err)
	}
	users, err := transformSDLCResponse("Collaborators", map[string]any{"users": []any{map[string]any{"login": "acme"}}, "count": 1})
	if err != nil || users.(*sdlc.CollaboratorsResponse).Users[0].Login != "acme" {
		t.Fatalf("Collaborators transform: %+v %v", users, err)
	}
	if _, err := transformSDLCResponse("FileIssue", map[string]any{"number": "eighty-eight"}); err == nil {
		t.Fatal("a mistyped issue decoded")
	}
	if _, err := transformSDLCResponse("Collaborators", map[string]any{"users": "nobody"}); err == nil {
		t.Fatal("a mistyped listing decoded")
	}
}
