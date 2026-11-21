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

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/sdlc"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// TestSDLCAdoptTool_RegisteredGatedAndRefusedWithoutTheModule: the tool is
// registered, dispatched, gated as task creation is on REST and gRPC, and
// answers an error without the module.
func TestSDLCAdoptTool_RegisteredGatedAndRefusedWithoutTheModule(t *testing.T) {
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	if err != nil {
		t.Fatal(err)
	}
	registry := NewToolRegistry(testutil.NewMockQuicBaaS(), log)
	registered := false
	for _, tool := range registry.ListTools() {
		registered = registered || tool.Name == "sdlc_adopt_issue"
	}
	if !registered || !isSDLCTool("sdlc_adopt_issue") {
		t.Fatal("sdlc_adopt_issue is not registered and dispatched")
	}
	if perm := qrdbToolExactMapping["sdlc_adopt_issue"]; perm != (qrdbToolResource{"orchestrator", "task_create"}) {
		t.Fatalf("sdlc_adopt_issue is gated %v, want orchestrator:task_create", perm)
	}
	if _, err := registry.executeSDLCTool(context.Background(), "sdlc_adopt_issue", map[string]interface{}{"project_id": "p", "issue": 1.0}); err == nil {
		t.Fatal("sdlc_adopt_issue answered without the module")
	}
}

// TestSDLCAdoptTool_ReachesTheAdopter: the arguments reach the adopter, which
// refuses an unbound project and a request naming no issue.
func TestSDLCAdoptTool_ReachesTheAdopter(t *testing.T) {
	registry, _ := newMCPSDLCRegistry(t)
	ctx := context.Background()
	if _, err := registry.ExecuteTool(ctx, "sdlc_adopt_issue", map[string]interface{}{
		"tenant_id": "tenant-a", "project_id": "p1", "issue": float64(141),
	}); !errors.Is(err, sdlc.ErrNotFound) {
		t.Fatalf("an unbound project: %v, want ErrNotFound", err)
	}
	if _, err := registry.ExecuteTool(ctx, "sdlc_adopt_issue", map[string]interface{}{
		"tenant_id": "tenant-a", "project_id": "p1",
	}); !errors.Is(err, sdlc.ErrInvalidInput) {
		t.Fatalf("no issue: %v, want ErrInvalidInput", err)
	}
}
