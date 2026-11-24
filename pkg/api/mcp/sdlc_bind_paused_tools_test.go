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
	"errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/sdlc"
)

func (p *mcpSDLCProjects) autonomous(id string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.projects[id].Autonomous
}

// TestSDLCBindTool_PausedLeavesAutonomyOff: the paused argument reaches the
// service, and a later bind without it still switches autonomy on.
func TestSDLCBindTool_PausedLeavesAutonomyOff(t *testing.T) {
	registry, forge, projects := newMCPSDLCRig(t)
	ctx := context.Background()

	if _, err := registry.ExecuteTool(ctx, "sdlc_bind", map[string]interface{}{
		"tenant_id": "tenant-a", "project_id": "p1", "binding": profiledToolBinding(t, forge), "paused": true,
	}); err != nil {
		t.Fatalf("paused sdlc_bind: %v", err)
	}
	if projects.autonomous("p1") {
		t.Fatal("a paused bind turned autonomy on")
	}
	bindMCPProject(t, registry, profiledToolBinding(t, forge))
	if !projects.autonomous("p1") {
		t.Fatal("a plain bind left autonomy off")
	}
}

// TestSDLCBindTool_NonBooleanPausedIsInvalid: paused must be a JSON boolean;
// anything else is refused before the bind.
func TestSDLCBindTool_NonBooleanPausedIsInvalid(t *testing.T) {
	registry, forge, _ := newMCPSDLCRig(t)
	_, err := registry.ExecuteTool(context.Background(), "sdlc_bind", map[string]interface{}{
		"tenant_id": "tenant-a", "project_id": "p1", "binding": profiledToolBinding(t, forge), "paused": "yes",
	})
	if !errors.Is(err, sdlc.ErrInvalidInput) {
		t.Fatalf("a string paused was accepted: %v", err)
	}
}
