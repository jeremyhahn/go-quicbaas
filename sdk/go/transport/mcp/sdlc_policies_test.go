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

// TestSDLCMCP_TheBindingCarriesTheTrainAndClustering proves the MCP decode
// keeps both throughput policies, so a rebind through MCP writes back what it
// read.
func TestSDLCMCP_TheBindingCarriesTheTrainAndClustering(t *testing.T) {
	raw := map[string]any{
		"project_id":  "p1",
		"merge_train": map[string]any{"disabled": true, "max_size": 7},
		"clustering":  map[string]any{"max_size": 3},
	}
	decoded, err := transformSDLCResponse("Binding", raw)
	if err != nil {
		t.Fatal(err)
	}
	binding := decoded.(*sdlc.ProjectBinding)
	if binding.MergeTrain != (sdlc.MergeTrainPolicy{Disabled: true, MaxSize: 7}) ||
		binding.Clustering != (sdlc.ClusteringPolicy{MaxSize: 3}) {
		t.Fatalf("binding = %+v %+v", binding.MergeTrain, binding.Clustering)
	}
}
