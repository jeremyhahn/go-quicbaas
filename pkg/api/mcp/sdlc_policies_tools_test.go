//go:build mcp

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
	"context"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/sdlc"
)

// TestSDLCTools_ARebindKeepsTheTrainAndClustering is the MCP half of the
// rebind regression for the throughput policies.
func TestSDLCTools_ARebindKeepsTheTrainAndClustering(t *testing.T) {
	registry, forge := newMCPSDLCRegistry(t)
	ctx := context.Background()
	sent := profiledToolBinding(t, forge)
	sent["merge_train"] = map[string]interface{}{"disabled": true, "max_size": 7}
	sent["clustering"] = map[string]interface{}{"max_size": 3}

	if _, err := registry.ExecuteTool(ctx, "sdlc_bind", map[string]interface{}{
		"tenant_id": "tenant-a", "project_id": "p1", "binding": sent,
	}); err != nil {
		t.Fatalf("sdlc_bind: %v", err)
	}
	read, err := registry.ExecuteTool(ctx, "sdlc_binding", map[string]interface{}{"tenant_id": "tenant-a", "project_id": "p1"})
	if err != nil {
		t.Fatalf("sdlc_binding: %v", err)
	}
	rebound, err := registry.ExecuteTool(ctx, "sdlc_bind", map[string]interface{}{
		"tenant_id": "tenant-a", "project_id": "p1", "binding": asArgument(t, read),
	})
	if err != nil {
		t.Fatalf("rebind: %v", err)
	}
	got := rebound.(*sdlc.ProjectBinding)
	if got.MergeTrain != (sdlc.MergeTrainPolicy{Disabled: true, MaxSize: 7}) || got.Clustering != (sdlc.ClusteringPolicy{MaxSize: 3}) {
		t.Fatalf("after a rebind through MCP: %+v %+v", got.MergeTrain, got.Clustering)
	}
}

// TestSDLCBindingSchema_DescribesTheTrainAndClustering keeps the tool's schema
// honest: an agent reading it learns both policies exist.
func TestSDLCBindingSchema_DescribesTheTrainAndClustering(t *testing.T) {
	properties := sdlcBindingSchema["properties"].(map[string]interface{})
	for _, key := range []string{"merge_train", "clustering"} {
		block, ok := properties[key].(map[string]interface{})
		if !ok {
			t.Fatalf("the binding schema lacks %q", key)
		}
		fields := block["properties"].(map[string]interface{})
		if _, ok := fields["disabled"]; !ok {
			t.Fatalf("%q lacks disabled", key)
		}
		if _, ok := fields["max_size"]; !ok {
			t.Fatalf("%q lacks max_size", key)
		}
	}
}
