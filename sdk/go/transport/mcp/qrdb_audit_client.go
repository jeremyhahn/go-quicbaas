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
	"strconv"
	"time"

	qrdbaudit "github.com/jeremyhahn/go-qrdb/pkg/audit"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport/internal/qrdbauditconv"
)

// QRDBAuditClient calls go-qrdb's audit tool over this transport's MCP
// session, so every call carries the platform credentials the transport was
// built with.
type QRDBAuditClient struct{ t *Transport }

var _ qrdbtransport.AuditClient = (*QRDBAuditClient)(nil)

// QRDBAudit returns a go-qrdb audit client bound to this transport.
func (t *Transport) QRDBAudit() *QRDBAuditClient { return &QRDBAuditClient{t: t} }

// EmitAudit is not served over MCP, as in go-qrdb's own MCP transport.
func (c *QRDBAuditClient) EmitAudit(context.Context, qrdbtransport.AuditEvent) error {
	return &qrdbtransport.NotImplementedError{Method: "EmitAudit"}
}

// QueryAudit queries audit events with go-qrdb's qrdb_audit_query tool.
func (c *QRDBAuditClient) QueryAudit(ctx context.Context, f qrdbtransport.AuditFilter) (*qrdbtransport.AuditPage, error) {
	args := map[string]interface{}{}
	if !f.From.IsZero() {
		args["from"] = f.From.UTC().Format(time.RFC3339)
	}
	if !f.To.IsZero() {
		args["to"] = f.To.UTC().Format(time.RFC3339)
	}
	for k, v := range map[string]string{
		"actor":     f.Actor,
		"resource":  f.Resource,
		"action":    f.Action,
		"outcome":   f.Outcome,
		"tenant_id": f.TenantID,
		"cursor":    f.Cursor,
	} {
		if v != "" {
			args[k] = v
		}
	}
	if f.Limit > 0 {
		args["limit"] = strconv.Itoa(f.Limit)
	}
	var page qrdbaudit.Page
	if err := callQRDBTool(ctx, c.t, "mcp.QRDBAuditClient.QueryAudit", "qrdb_audit_query", args, &page); err != nil {
		return nil, err
	}
	return qrdbauditconv.PageToTransport(&page), nil
}
