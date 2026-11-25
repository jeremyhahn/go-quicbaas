//go:build rest && mcp

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

package server

import (
	"strings"
	"testing"

	mcpgo "github.com/mark3labs/mcp-go/mcp"

	mcpapi "github.com/jeremyhahn/go-quicbaas/pkg/api/mcp"
	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	mcptransport "github.com/jeremyhahn/go-quicbaas/sdk/go/transport/mcp"
)

// TestAuditGetNotFound_MCPAnswersNotFoundWithEventID is the #1700 contract
// for quicbaas_iam_audit_get: a missing audit event, wrapped the way the tool
// wraps it, answers MCP not found and keeps the text naming the event, as
// REST answers 404 with the event ID.
func TestAuditGetNotFound_MCPAnswersNotFoundWithEventID(t *testing.T) {
	const eventID = "evt-1700"
	err := &mcpapi.AuditEventGetError{
		EventID: eventID,
		Cause:   &AuditEventNotFoundError{EventID: eventID},
	}

	result := mcpapi.ToolErrorResult(nil, "quicbaas_iam_audit_get", err)

	want := mcptransport.ToolError{Code: mcptransport.MCPResourceNotFound}
	if result.StructuredContent != want {
		t.Fatalf("structured content = %#v, want %#v", result.StructuredContent, want)
	}
	if len(result.Content) == 0 {
		t.Fatal("tool error result has no content")
	}
	text, ok := result.Content[0].(mcpgo.TextContent)
	if !ok {
		t.Fatalf("content[0] = %T, want mcp.TextContent", result.Content[0])
	}
	if text.Text == qberrors.ServerFailureMessage {
		t.Fatalf("text was sanitized to the server-failure message: %q", text.Text)
	}
	if !strings.Contains(text.Text, eventID) {
		t.Fatalf("text %q does not name the event %q", text.Text, eventID)
	}
}
