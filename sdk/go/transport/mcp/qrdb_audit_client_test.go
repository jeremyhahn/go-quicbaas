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
	"testing"
	"time"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

func TestQRDBAuditClient_QueryAudit_SendsGoQRDBArgsAndDecodesPage(t *testing.T) {
	var got map[string]any
	srv := mcpserver.NewMCPServer("TestServer", "1.0.0")
	srv.AddTool(mcpgo.NewTool("qrdb_audit_query"), func(_ context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		got = req.GetArguments()
		return mcpgo.NewToolResultText(`{"events":[{"actor":"alice","tenant_id":"t1","client_ip":"10.0.0.1","request_id":"r1","prev_hash":"h0","attributes":{"k":"v"}}],"next":"c2","total":1}`), nil
	})
	httpSrv := mcpserver.NewTestStreamableHTTPServer(srv)
	t.Cleanup(httpSrv.Close)

	tr, err := New(&transport.Config{Address: httpSrv.URL, Timeout: 5 * time.Second})
	require.NoError(t, err)
	t.Cleanup(func() { _ = tr.Close() })

	page, err := tr.QRDBAudit().QueryAudit(context.Background(), qrdbtransport.AuditFilter{Actor: "alice", TenantID: "t1", Limit: 5})
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"actor": "alice", "tenant_id": "t1", "limit": "5"}, got)
	assert.Equal(t, &qrdbtransport.AuditPage{
		Events: []qrdbtransport.AuditEvent{{
			Actor:      "alice",
			TenantID:   "t1",
			ClientIP:   "10.0.0.1",
			RequestID:  "r1",
			PrevHash:   "h0",
			Attributes: map[string]any{"k": "v"},
		}},
		Next:  "c2",
		Total: 1,
	}, page)
}

func TestQRDBAuditClient_EmitAudit_NotImplemented(t *testing.T) {
	tr, err := New(&transport.Config{Address: "http://127.0.0.1:1", Timeout: time.Second})
	require.NoError(t, err)
	t.Cleanup(func() { _ = tr.Close() })

	err = tr.QRDBAudit().EmitAudit(context.Background(), qrdbtransport.AuditEvent{Actor: "a"})
	var ni *qrdbtransport.NotImplementedError
	assert.ErrorAs(t, err, &ni)
}
