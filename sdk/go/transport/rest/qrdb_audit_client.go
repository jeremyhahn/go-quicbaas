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

package rest

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	qrdbaudit "github.com/jeremyhahn/go-qrdb/pkg/audit"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport/internal/qrdbauditconv"
)

// QRDBAuditClient is go-qrdb's audit service over this transport. Requests
// go through doJSONResult, so they carry the transport's bearer.
type QRDBAuditClient struct{ t *Transport }

var _ qrdbtransport.AuditClient = (*QRDBAuditClient)(nil)

// QRDBAudit returns go-qrdb's audit service client over this transport.
func (t *Transport) QRDBAudit() *QRDBAuditClient { return &QRDBAuditClient{t: t} }

// EmitAudit is not served over REST, as in go-qrdb's own REST transport.
func (c *QRDBAuditClient) EmitAudit(context.Context, qrdbtransport.AuditEvent) error {
	return &qrdbtransport.NotImplementedError{Method: "EmitAudit"}
}

// QueryAudit is GET /v1/audit/events with go-qrdb's query parameters.
func (c *QRDBAuditClient) QueryAudit(ctx context.Context, f qrdbtransport.AuditFilter) (*qrdbtransport.AuditPage, error) {
	const layout = "2006-01-02T15:04:05Z"
	q := url.Values{}
	if !f.From.IsZero() {
		q.Set("from", f.From.UTC().Format(layout))
	}
	if !f.To.IsZero() {
		q.Set("to", f.To.UTC().Format(layout))
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
			q.Set(k, v)
		}
	}
	if f.Limit > 0 {
		q.Set("limit", strconv.Itoa(f.Limit))
	}
	path := "/v1/audit/events"
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	var page qrdbaudit.Page
	if err := doJSONResult(ctx, c.t, "rest.QRDBAuditClient", http.MethodGet, path, nil, &page); err != nil {
		return nil, err
	}
	return qrdbauditconv.PageToTransport(&page), nil
}
