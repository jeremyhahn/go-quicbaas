// Copyright (c) 2025 Jeremy Hahn
// SPDX-License-Identifier: AGPL-3.0-or-later

package quicbaas

import (
	"context"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"google.golang.org/grpc/metadata"
)

// qrdbBearerBackupClient forwards to go-qrdb's backup client and adds the
// platform bearer as outgoing gRPC authorization metadata on every call, the
// way the SDK's own gRPC transport does.
type qrdbBearerBackupClient struct {
	inner  QRDBBackupClient
	bearer string
}

var _ QRDBBackupClient = (*qrdbBearerBackupClient)(nil)

func (b *qrdbBearerBackupClient) auth(ctx context.Context) context.Context {
	return metadata.AppendToOutgoingContext(ctx, "authorization", b.bearer)
}

// CreateBackup forwards with the bearer attached.
func (b *qrdbBearerBackupClient) CreateBackup(ctx context.Context, req *QRDBBackupRequest) (*QRDBBackupInfo, error) {
	return b.inner.CreateBackup(b.auth(ctx), req)
}

// ListBackups forwards with the bearer attached.
func (b *qrdbBearerBackupClient) ListBackups(ctx context.Context) ([]QRDBBackupInfo, error) {
	return b.inner.ListBackups(b.auth(ctx))
}

// GetBackup forwards with the bearer attached.
func (b *qrdbBearerBackupClient) GetBackup(ctx context.Context, id string) (*QRDBBackupInfo, error) {
	return b.inner.GetBackup(b.auth(ctx), id)
}

// DeleteBackup forwards with the bearer attached.
func (b *qrdbBearerBackupClient) DeleteBackup(ctx context.Context, id string) error {
	return b.inner.DeleteBackup(b.auth(ctx), id)
}

// RestoreBackup forwards with the bearer attached.
func (b *qrdbBearerBackupClient) RestoreBackup(ctx context.Context, req *qrdbtransport.RestoreRequest) (*qrdbtransport.RestoreResult, error) {
	return b.inner.RestoreBackup(b.auth(ctx), req)
}

// VerifyBackup forwards with the bearer attached.
func (b *qrdbBearerBackupClient) VerifyBackup(ctx context.Context, id string) (*qrdbtransport.VerifyResult, error) {
	return b.inner.VerifyBackup(b.auth(ctx), id)
}
