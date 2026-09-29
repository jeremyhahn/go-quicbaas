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

// TestWorkerImageToolsExist pins the SDK's tool names to the ones the server
// registers (pkg/api/mcp, sdlc_tools.go) and requires a decoder for each.
func TestWorkerImageToolsExist(t *testing.T) {
	for method, tool := range map[string]string{
		"WorkerImageStatus":  "quicbaas_sdlc_worker_image",
		"RefreshWorkerImage": "quicbaas_sdlc_worker_image_refresh",
	} {
		got, ok := sdlcToolName(method)
		if !ok || got != tool {
			t.Errorf("%s maps to %q (%v), but the server registers %s", method, got, ok, tool)
		}
		if _, ok := sdlcMCPTransforms[method]; !ok {
			t.Errorf("%s has no result decoder", method)
		}
	}
}

func TestDecodeMCPSDLCWorkerImageStatus(t *testing.T) {
	raw := map[string]interface{}{
		"serving": true, "queued": false,
		"last": map[string]interface{}{"outcome": "current", "latest": map[string]interface{}{"claude_code": "2.1.280", "codex": "0.156.0"}},
	}
	decoded, err := transformSDLCResponse("WorkerImageStatus", raw)
	if err != nil {
		t.Fatalf("transformSDLCResponse: %v", err)
	}
	status, ok := decoded.(*sdlc.WorkerImageStatus)
	if !ok || !status.Serving || status.Last == nil || status.Last.Outcome != "current" || status.Last.Latest.ClaudeCode != "2.1.280" {
		t.Fatalf("decoded %#v", decoded)
	}
	if _, err := decodeMCPSDLCWorkerImageStatus([]byte(`["nope"]`)); err == nil {
		t.Fatal("a JSON array decoded as a worker image status")
	}
}
