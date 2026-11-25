// Copyright (c) 2025 Jeremy Hahn
// SPDX-License-Identifier: AGPL-3.0-or-later

package quicbaas

import (
	"context"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"google.golang.org/grpc/metadata"
)

// qrdbBearerCredentialClient forwards to go-qrdb's credential client and adds
// the platform bearer as outgoing gRPC authorization metadata on every call,
// the way qrdbBearerCeremonyClient does for the ceremony verbs. Every verb is
// written out so a verb added to the interface fails to compile here instead
// of skipping the bearer.
type qrdbBearerCredentialClient struct {
	inner  QRDBCredentialClient
	bearer string
}

var _ QRDBCredentialClient = (*qrdbBearerCredentialClient)(nil)

func (b *qrdbBearerCredentialClient) ctx(ctx context.Context) context.Context {
	return metadata.AppendToOutgoingContext(ctx, "authorization", b.bearer)
}

// CredentialSubmit forwards with the bearer attached.
func (b *qrdbBearerCredentialClient) CredentialSubmit(ctx context.Context, req *qrdbtransport.CredentialSubmitRequest) error {
	return b.inner.CredentialSubmit(b.ctx(ctx), req)
}

// CredentialStrategy forwards with the bearer attached.
func (b *qrdbBearerCredentialClient) CredentialStrategy(ctx context.Context) (*qrdbtransport.CredentialStrategyResponse, error) {
	return b.inner.CredentialStrategy(b.ctx(ctx))
}

// CredentialDelete forwards with the bearer attached.
func (b *qrdbBearerCredentialClient) CredentialDelete(ctx context.Context, name string) error {
	return b.inner.CredentialDelete(b.ctx(ctx), name)
}

// CredentialList forwards with the bearer attached.
func (b *qrdbBearerCredentialClient) CredentialList(ctx context.Context, req *qrdbtransport.CredentialListRequest) (*qrdbtransport.CredentialListResponse, error) {
	return b.inner.CredentialList(b.ctx(ctx), req)
}

// CredentialRotate forwards with the bearer attached.
func (b *qrdbBearerCredentialClient) CredentialRotate(ctx context.Context, req *qrdbtransport.CredentialRotateRequest) (*qrdbtransport.CredentialRotateResponse, error) {
	return b.inner.CredentialRotate(b.ctx(ctx), req)
}

// CredentialRetrieveVersion forwards with the bearer attached.
func (b *qrdbBearerCredentialClient) CredentialRetrieveVersion(ctx context.Context, req *qrdbtransport.CredentialRetrieveVersionRequest) (*qrdbtransport.CredentialRetrieveVersionResponse, error) {
	return b.inner.CredentialRetrieveVersion(b.ctx(ctx), req)
}

// CredentialListVersions forwards with the bearer attached.
func (b *qrdbBearerCredentialClient) CredentialListVersions(ctx context.Context, req *qrdbtransport.CredentialListVersionsRequest) (*qrdbtransport.CredentialListVersionsResponse, error) {
	return b.inner.CredentialListVersions(b.ctx(ctx), req)
}

// CredentialGCExpired forwards with the bearer attached.
func (b *qrdbBearerCredentialClient) CredentialGCExpired(ctx context.Context, req *qrdbtransport.CredentialGCRequest) (*qrdbtransport.CredentialGCResponse, error) {
	return b.inner.CredentialGCExpired(b.ctx(ctx), req)
}

// CredentialStore forwards with the bearer attached.
func (b *qrdbBearerCredentialClient) CredentialStore(ctx context.Context, name string, value []byte) error {
	return b.inner.CredentialStore(b.ctx(ctx), name, value)
}

// CredentialRetrieve forwards with the bearer attached.
func (b *qrdbBearerCredentialClient) CredentialRetrieve(ctx context.Context, name string) ([]byte, error) {
	return b.inner.CredentialRetrieve(b.ctx(ctx), name)
}
