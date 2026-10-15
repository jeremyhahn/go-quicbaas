//go:build mcp

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

package mcp

import (
	"context"
	"errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

func TestSDLCTools_RegisteredAndDispatched(t *testing.T) {
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	if err != nil {
		t.Fatal(err)
	}
	mock := testutil.NewMockQuicBaaS()
	registry := NewToolRegistry(mock, log)
	names := map[string]bool{}
	for _, tool := range registry.ListTools() {
		names[tool.Name] = true
	}
	for _, want := range []string{
		"sdlc_bind", "sdlc_unbind", "sdlc_binding", "sdlc_delivery", "sdlc_forges",
		"sdlc_onboard", "sdlc_project_onboard", "sdlc_conformance", "sdlc_open_work",
	} {
		if !names[want] {
			t.Fatalf("%s not registered", want)
		}
		if !isSDLCTool(want) {
			t.Fatalf("%s not dispatched as an sdlc tool", want)
		}
	}
	if isSDLCTool("project_get") {
		t.Fatal("a project tool is not an sdlc tool")
	}
	// Without the module the tools refuse typed rather than panic.
	ctx := context.Background()
	for name, args := range map[string]map[string]interface{}{
		"sdlc_bind":            {"project_id": "p", "binding": map[string]interface{}{"project_id": "p"}},
		"sdlc_unbind":          {"project_id": "p"},
		"sdlc_binding":         {"project_id": "p"},
		"sdlc_delivery":        {"task_id": "t"},
		"sdlc_forges":          nil,
		"sdlc_onboard":         {"project_id": "p"},
		"sdlc_project_onboard": {"repo": "web", "like": "p"},
		"sdlc_conformance":     {"project_id": "p"},
		"sdlc_open_work":       {"project_id": "p"},
	} {
		if _, err := registry.executeSDLCTool(ctx, name, args); err == nil {
			t.Fatalf("%s answered without the module", name)
		}
	}
	if _, err := registry.executeSDLCTool(ctx, "sdlc_nope", nil); err == nil {
		t.Fatal("unknown tool accepted")
	}
	if _, err := registry.ExecuteTool(ctx, "sdlc_binding", map[string]interface{}{"project_id": "p"}); err == nil {
		t.Fatal("the registry did not route the sdlc tool")
	}
	_ = errors.New
}
