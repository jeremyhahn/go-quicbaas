// Copyright (c) 2025 Jeremy Hahn
// SPDX-License-Identifier: AGPL-3.0-or-later

package quicbaas

import (
	"context"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	qbtransport "github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// qrdbEmbeddedTenantAuditClient binds the tenant a caller named with the
// SDK's transport.ContextWithTenant onto go-qrdb's tenant key before each
// audit call on the embedded transport. go-qrdb confines audit queries to
// the tenant bound under its own key, so without this an embedded caller's
// tenant is invisible to it and the query runs as the system tenant.
type qrdbEmbeddedTenantAuditClient struct {
	inner QRDBAuditClient
}

var _ QRDBAuditClient = (*qrdbEmbeddedTenantAuditClient)(nil)

// EmitAudit forwards to go-qrdb's EmitAudit with the caller's tenant bound.
func (c *qrdbEmbeddedTenantAuditClient) EmitAudit(ctx context.Context, ev QRDBAuditEvent) error {
	return c.inner.EmitAudit(qrdbAuditTenantContext(ctx), ev)
}

// QueryAudit forwards to go-qrdb's QueryAudit with the caller's tenant bound.
func (c *qrdbEmbeddedTenantAuditClient) QueryAudit(ctx context.Context, f QRDBAuditFilter) (*QRDBAuditPage, error) {
	return c.inner.QueryAudit(qrdbAuditTenantContext(ctx), f)
}

// qrdbAuditTenantContext keeps a tenant already bound under go-qrdb's key;
// otherwise it binds the tenant named with transport.ContextWithTenant.
// With neither, ctx is returned unchanged and go-qrdb applies its own
// default.
func qrdbAuditTenantContext(ctx context.Context) context.Context {
	if _, ok := qrdbsdk.TenantFromContext(ctx); ok {
		return ctx
	}
	if id := qbtransport.TenantFromContext(ctx); id != "" {
		return qrdbsdk.ContextWithTenant(ctx, id)
	}
	return ctx
}
