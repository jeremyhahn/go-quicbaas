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
	stderrors "errors"
	"fmt"
	"strings"
	"testing"
	"time"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// TestCeremonyStatus_ToolNotServed_IsUnsupportedCapability pins the error a
// ceremony verb returns on a node that serves no qrdb_ceremony_* tool (a
// node without a ceremony service): the unsupported capability gRPC reports,
// not an "invalid params" input error.
func TestCeremonyStatus_ToolNotServed_IsUnsupportedCapability(t *testing.T) {
	tr, _, cleanup := newMultiToolMCPServer(t, []string{"qrdb_barrier_status"}, nil)
	defer cleanup()

	_, err := tr.CeremonyStatus(context.Background())
	if err == nil {
		t.Fatal("CeremonyStatus: expected an error for an unserved tool")
	}
	var unsupported *qrdbtransport.CapabilityUnsupportedError
	if !stderrors.As(err, &unsupported) {
		t.Fatalf("CeremonyStatus error is not a CapabilityUnsupportedError: %v", err)
	}
	if got := errors.Code(err); got != errors.CodeUnimplemented {
		t.Errorf("error code = %v, want %v", got, errors.CodeUnimplemented)
	}
	if !strings.Contains(err.Error(), "not configured") {
		t.Errorf("error %q does not say the ceremony service is not configured", err)
	}
	if strings.Contains(err.Error(), "invalid params") {
		t.Errorf("error %q still carries the invalid-params text", err)
	}
}

// TestCeremonyVerbs_ToolNotServed_AllMapped checks that every ceremony verb
// that needs no TLS maps an unserved tool to the unsupported capability.
func TestCeremonyVerbs_ToolNotServed_AllMapped(t *testing.T) {
	tr, _, cleanup := newMultiToolMCPServer(t, []string{"qrdb_barrier_status"}, nil)
	defer cleanup()
	ctx := context.Background()

	cases := []struct {
		name string
		call func() error
	}{
		{"CeremonyStatus", func() error {
			_, err := tr.CeremonyStatus(ctx)
			return err
		}},
		{"CeremonyClaimCertBegin", func() error {
			_, err := tr.CeremonyClaimCertBegin(ctx, &qrdbtransport.ClaimCertBeginRequest{Username: "alice"})
			return err
		}},
		{"CeremonyClaimCertComplete", func() error {
			_, err := tr.CeremonyClaimCertComplete(ctx, &qrdbtransport.ClaimCertCompleteRequest{Username: "alice"})
			return err
		}},
		{"CeremonyBeginClaimShare", func() error {
			_, err := tr.CeremonyBeginClaimShare(ctx, &qrdbtransport.BeginClaimShareRequest{Username: "alice"})
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call()
			var unsupported *qrdbtransport.CapabilityUnsupportedError
			if !stderrors.As(err, &unsupported) {
				t.Fatalf("%s error is not a CapabilityUnsupportedError: %v", tc.name, err)
			}
		})
	}
}

// TestCeremony_ToolError_PassesThrough checks that a tool-level error from a
// served ceremony tool is not mistaken for a missing ceremony service.
func TestCeremony_ToolError_PassesThrough(t *testing.T) {
	srv := mcpserver.NewMCPServer("TestServer", "1.0.0")
	srv.AddTool(mcpgo.NewTool("qrdb_ceremony_status"), func(_ context.Context, _ mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		return mcpgo.NewToolResultError("boom"), nil
	})
	httpSrv := mcpserver.NewTestStreamableHTTPServer(srv)
	defer httpSrv.Close()
	tr, err := New(&transport.Config{Address: httpSrv.URL, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = tr.Close() }()

	_, err = tr.CeremonyStatus(context.Background())
	if err == nil {
		t.Fatal("CeremonyStatus: expected the tool error")
	}
	var unsupported *qrdbtransport.CapabilityUnsupportedError
	if stderrors.As(err, &unsupported) {
		t.Fatalf("a tool-level error was mapped to CapabilityUnsupportedError: %v", err)
	}
}

// TestCeremonyStatus_OtherUnknownTool_NotMapped checks that the mapping
// fires for the called tool only: an unknown-tool refusal naming another
// tool is not taken as this tool's absence.
func TestCeremonyStatus_OtherUnknownTool_NotMapped(t *testing.T) {
	// A server with no tools at all does not advertise the tools capability
	// and refuses tools/call outright, so one unrelated tool is registered.
	tr, _, cleanup := newMultiToolMCPServer(t, []string{"qrdb_barrier_status"}, nil)
	defer cleanup()

	_, err := tr.callCeremony(context.Background(), "qrdb_ceremony_status", nil)
	var unsupported *qrdbtransport.CapabilityUnsupportedError
	if !stderrors.As(err, &unsupported) {
		t.Fatalf("callCeremony error is not a CapabilityUnsupportedError: %v", err)
	}

	other := fmt.Errorf("%w: tool 'qrdb_ceremony_other' not found", mcpgo.ErrInvalidParams)
	if isUnknownToolError(other, "qrdb_ceremony_status") {
		t.Error("isUnknownToolError matched a refusal naming a different tool")
	}
	same := fmt.Errorf("%w: tool 'qrdb_ceremony_status' not found", mcpgo.ErrInvalidParams)
	if !isUnknownToolError(same, "qrdb_ceremony_status") {
		t.Error("isUnknownToolError did not match the refusal naming the tool")
	}
}
