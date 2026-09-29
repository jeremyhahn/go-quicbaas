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

func TestSDLCMCP_AdoptIssueToolAndTransform(t *testing.T) {
	if got, ok := sdlcToolName("AdoptIssue"); !ok || got != "sdlc_adopt_issue" {
		t.Fatalf("AdoptIssue → %q %v", got, ok)
	}
	decoded, err := transformSDLCResponse("AdoptIssue", map[string]any{"task_id": "task-1", "issue": 141, "branch": "issue/141-x"})
	if err != nil || decoded.(*sdlc.Adoption).TaskID != "task-1" || decoded.(*sdlc.Adoption).Issue != 141 {
		t.Fatalf("transform: %+v %v", decoded, err)
	}
	if _, err := transformSDLCResponse("AdoptIssue", map[string]any{"issue": "one"}); err == nil {
		t.Fatal("a mistyped adoption decoded")
	}
}
