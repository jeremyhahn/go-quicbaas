// Copyright (c) 2025 Jeremy Hahn
// SPDX-License-Identifier: AGPL-3.0-or-later

package quicbaas

import (
	"context"

	"google.golang.org/grpc/metadata"
)

// qrdbBearerAuditClient sends the platform bearer as gRPC metadata on every
// audit call. go-qrdb's gRPC and Unix transports carry no token of their own.
type qrdbBearerAuditClient struct {
	inner  QRDBAuditClient
	bearer string
}

var _ QRDBAuditClient = (*qrdbBearerAuditClient)(nil)

// EmitAudit forwards to go-qrdb's EmitAudit with the bearer attached.
func (b *qrdbBearerAuditClient) EmitAudit(ctx context.Context, ev QRDBAuditEvent) error {
	return b.inner.EmitAudit(metadata.AppendToOutgoingContext(ctx, "authorization", b.bearer), ev)
}

// QueryAudit forwards to go-qrdb's QueryAudit with the bearer attached.
func (b *qrdbBearerAuditClient) QueryAudit(ctx context.Context, f QRDBAuditFilter) (*QRDBAuditPage, error) {
	return b.inner.QueryAudit(metadata.AppendToOutgoingContext(ctx, "authorization", b.bearer), f)
}
