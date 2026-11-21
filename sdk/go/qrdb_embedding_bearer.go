// Copyright (c) 2025 Jeremy Hahn
// SPDX-License-Identifier: AGPL-3.0-or-later

package quicbaas

import (
	"context"

	"google.golang.org/grpc/metadata"
)

// qrdbBearerEmbeddingClient forwards to go-qrdb's embedding client and adds
// the platform bearer as outgoing gRPC authorization metadata on every call,
// the way the SDK's own gRPC transport does.
type qrdbBearerEmbeddingClient struct {
	inner  QRDBEmbeddingClient
	bearer string
}

var _ QRDBEmbeddingClient = (*qrdbBearerEmbeddingClient)(nil)

// Embed forwards to go-qrdb's Embed with the bearer attached.
func (b *qrdbBearerEmbeddingClient) Embed(ctx context.Context, req *QRDBEmbedRequest) (*QRDBEmbedResponse, error) {
	return b.inner.Embed(metadata.AppendToOutgoingContext(ctx, "authorization", b.bearer), req)
}

// BatchEmbed forwards to go-qrdb's BatchEmbed with the bearer attached.
func (b *qrdbBearerEmbeddingClient) BatchEmbed(ctx context.Context, req *QRDBBatchEmbedRequest) (*QRDBBatchEmbedResponse, error) {
	return b.inner.BatchEmbed(metadata.AppendToOutgoingContext(ctx, "authorization", b.bearer), req)
}

// ListEmbeddingModels forwards to go-qrdb's ListEmbeddingModels with the
// bearer attached.
func (b *qrdbBearerEmbeddingClient) ListEmbeddingModels(ctx context.Context, provider string) (*QRDBListEmbeddingModelsResponse, error) {
	return b.inner.ListEmbeddingModels(metadata.AppendToOutgoingContext(ctx, "authorization", b.bearer), provider)
}

// qrdbSubClientSpeaksGRPC reports whether the qrdb sub-client for tt is built
// on go-qrdb's gRPC or Unix transport (both gRPC), where a bearer travels as
// outgoing metadata. An unmapped transport type builds on gRPC.
func qrdbSubClientSpeaksGRPC(tt TransportType) bool {
	switch tt {
	case TransportREST, TransportQUIC, TransportMCP, TransportEmbedded:
		return false
	default:
		return true
	}
}
