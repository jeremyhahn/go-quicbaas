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
)

// TestExecuteOrchestratorAttention_ModuleNotRunning proves both new tools
// answer "unavailable" rather than panicking or reaching for a nil pointer
// when the SDLC module is absent from this node.
func TestExecuteOrchestratorAttention_ModuleNotRunning(t *testing.T) {
	t.Parallel()
	reg := newSDLCToolRegistry(t, false)

	if _, err := reg.ExecuteTool(context.Background(), "quicbaas_orchestrator_attention_list",
		map[string]interface{}{"tenant_id": "tenant-a"}); err == nil {
		t.Fatal("attention_list with no SDLC module: want an error, got none")
	}
	if _, err := reg.ExecuteTool(context.Background(), "quicbaas_orchestrator_attention_resolve",
		map[string]interface{}{"tenant_id": "tenant-a", "task_id": "t1", "action": "readmit"}); err == nil {
		t.Fatal("attention_resolve with no SDLC module: want an error, got none")
	}
}

// TestExecuteOrchestratorAttention_InboxNotWired proves both tools answer
// "unavailable" — rather than a nil-pointer panic — for a node whose SDLC
// module is running but whose attention inbox never got constructed (a
// zero-value *service.SDLCService, the same double
// TestSDLCForgesToolIsDispatchable uses for "module present").
func TestExecuteOrchestratorAttention_InboxNotWired(t *testing.T) {
	t.Parallel()
	reg := newSDLCToolRegistry(t, true)

	if _, err := reg.ExecuteTool(context.Background(), "quicbaas_orchestrator_attention_list",
		map[string]interface{}{"tenant_id": "tenant-a"}); err == nil {
		t.Fatal("attention_list with no inbox wired: want an error, got none")
	}
	if _, err := reg.ExecuteTool(context.Background(), "quicbaas_orchestrator_attention_resolve",
		map[string]interface{}{"tenant_id": "tenant-a", "task_id": "t1", "action": "readmit"}); err == nil {
		t.Fatal("attention_resolve with no inbox wired: want an error, got none")
	}
}
