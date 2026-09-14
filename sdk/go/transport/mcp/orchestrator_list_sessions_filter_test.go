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

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// TestMCPListSessions_ForwardsStatusFilter pins the status filter into the
// quicbaas_orchestrator_list_sessions tool arguments.
//
// ListSessions has no bespoke transformRequest: its arguments come from the
// generic JSON round-trip over the SDK request struct, so the filter reaches
// the server only because ListSessionsRequest.Status carries a `status` JSON
// tag. That is an easy thing to lose by accident -- the sibling gRPC
// transport dropped the same filter outright (go-quicbaas#792) -- so the
// argument is asserted rather than assumed.
func TestMCPListSessions_ForwardsStatusFilter(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_orchestrator_list_sessions",
		assertArgs: func(t *testing.T, args map[string]interface{}) {
			if args["status"] != "active" {
				t.Errorf("status argument = %v, want active", args["status"])
			}
			if args["backend"] != "claude" {
				t.Errorf("backend argument = %v, want claude", args["backend"])
			}
		},
		result: map[string]interface{}{"sessions": []interface{}{}},
	})
	defer cleanup()

	if _, err := tr.Request(context.Background(), "quicbaas.Orchestrator", "ListSessions",
		&orchestrator.ListSessionsRequest{Backend: "claude", Status: "active"}); err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
}

// TestMCPListSessions_EmptyStatusIsOmitted proves an unset filter sends no
// status argument. The tool schema types status as an enum of the lifecycle
// states, so an empty string is not a legal value -- omitting the key is the
// only correct way to ask for every state.
func TestMCPListSessions_EmptyStatusIsOmitted(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_orchestrator_list_sessions",
		assertArgs: func(t *testing.T, args map[string]interface{}) {
			if _, present := args["status"]; present {
				t.Errorf("status argument present for an unfiltered list: %v", args)
			}
		},
		result: map[string]interface{}{"sessions": []interface{}{}},
	})
	defer cleanup()

	if _, err := tr.Request(context.Background(), "quicbaas.Orchestrator", "ListSessions",
		&orchestrator.ListSessionsRequest{}); err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
}

// TestMCPListSessions_CarriesTheTenantFromContext closes the gap the two tests
// above could not see.
//
// They assert what the SDK puts in the arguments, against a fake that accepts
// anything. The REAL server's session tools take the tenant as an ARGUMENT and
// refuse a call without one (sessionTenantContext -> RequiredFieldError),
// while every SDK caller carries it on the CONTEXT, because that is how the
// wire transports move it — gRPC onto the proto, REST onto the query.
// ListSessionsRequest has no tenant field at all, so the generic conversion
// sent no tenant and a perfectly ordinary filtered listing came back as a
// required-field error over MCP alone.
func TestMCPListSessions_CarriesTheTenantFromContext(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_orchestrator_list_sessions",
		assertArgs: func(t *testing.T, args map[string]interface{}) {
			if args["tenant_id"] != "tenant-a" {
				t.Errorf("tenant_id argument = %v, want tenant-a; the server's session "+
					"tools refuse a call that names no tenant", args["tenant_id"])
			}
			if args["status"] != "active" {
				t.Errorf("status argument = %v, want active", args["status"])
			}
		},
		result: map[string]interface{}{"sessions": []interface{}{}},
	})
	defer cleanup()

	ctx := transport.ContextWithTenant(context.Background(), "tenant-a")
	if _, err := tr.Request(ctx, "quicbaas.Orchestrator", "ListSessions",
		&orchestrator.ListSessionsRequest{Status: "active"}); err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
}

// TestMCPListSessions_AnExplicitTenantIsNotOverridden holds the injection to
// filling a gap rather than imposing a value. A caller that put a tenant in
// the arguments meant it.
func TestMCPListSessions_AnExplicitTenantIsNotOverridden(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_orchestrator_list_sessions",
		assertArgs: func(t *testing.T, args map[string]interface{}) {
			if args["tenant_id"] != "tenant-explicit" {
				t.Errorf("tenant_id argument = %v, want tenant-explicit", args["tenant_id"])
			}
		},
		result: map[string]interface{}{"sessions": []interface{}{}},
	})
	defer cleanup()

	ctx := transport.ContextWithTenant(context.Background(), "tenant-a")
	if _, err := tr.Request(ctx, "quicbaas.Orchestrator", "ListSessions",
		map[string]interface{}{"tenant_id": "tenant-explicit"}); err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
}
