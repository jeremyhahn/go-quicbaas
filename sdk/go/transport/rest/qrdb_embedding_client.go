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

// Package rest implements the SDK's REST transport.
package rest

import (
	"context"
	"net/http"
	"net/url"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
)

// QRDBEmbeddingClient calls go-qrdb's embedding routes through this
// transport, so the platform bearer and headers ride every call. go-qrdb's
// own REST sub-client sets no Authorization header.
type QRDBEmbeddingClient struct {
	t *Transport
}

var _ qrdbtransport.EmbeddingClient = (*QRDBEmbeddingClient)(nil)

// QRDBEmbedding returns the go-qrdb embedding client bound to this transport.
func (t *Transport) QRDBEmbedding() *QRDBEmbeddingClient {
	return &QRDBEmbeddingClient{t: t}
}

// Embed generates embedding vectors for one or more texts.
func (c *QRDBEmbeddingClient) Embed(ctx context.Context, req *qrdbtransport.EmbedRequest) (*qrdbtransport.EmbedResponse, error) {
	var resp qrdbtransport.EmbedResponse
	if err := doJSONResult(ctx, c.t, "rest.QRDBEmbeddingClient", http.MethodPost, "/v1/embeddings", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// BatchEmbed embeds a large set of texts in provider sub-batches.
func (c *QRDBEmbeddingClient) BatchEmbed(ctx context.Context, req *qrdbtransport.BatchEmbedRequest) (*qrdbtransport.BatchEmbedResponse, error) {
	var resp qrdbtransport.BatchEmbedResponse
	if err := doJSONResult(ctx, c.t, "rest.QRDBEmbeddingClient", http.MethodPost, "/v1/embeddings/batch", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ListEmbeddingModels returns the embedding models available from provider.
// An empty provider queries the service default.
func (c *QRDBEmbeddingClient) ListEmbeddingModels(ctx context.Context, provider string) (*qrdbtransport.ListEmbeddingModelsResponse, error) {
	path := "/v1/embedding/models"
	if provider != "" {
		path += "?provider=" + url.QueryEscape(provider)
	}
	var resp qrdbtransport.ListEmbeddingModelsResponse
	if err := doJSONResult(ctx, c.t, "rest.QRDBEmbeddingClient", http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
