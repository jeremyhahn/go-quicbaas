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
	"encoding/json"
	"fmt"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// QRDBEmbeddingClient calls go-qrdb's embedding tools over this transport's
// MCP session, so every call carries the platform credentials the transport
// was built with.
type QRDBEmbeddingClient struct {
	t *Transport
}

// QRDBEmbedding returns a go-qrdb embedding client bound to this transport.
func (t *Transport) QRDBEmbedding() *QRDBEmbeddingClient {
	return &QRDBEmbeddingClient{t: t}
}

// qrdbUsageWire is go-qrdb's token usage as its MCP tools emit it.
type qrdbUsageWire struct {
	PromptTokens int
	TotalTokens  int
}

func (u *qrdbUsageWire) usage() *qrdbtransport.UsageInfo {
	if u == nil {
		return nil
	}
	return &qrdbtransport.UsageInfo{PromptTokens: u.PromptTokens, TotalTokens: u.TotalTokens}
}

// call runs one go-qrdb embedding tool and decodes its JSON result into out.
func (c *QRDBEmbeddingClient) call(ctx context.Context, op, tool string, args map[string]interface{}, out interface{}) error {
	return callQRDBTool(ctx, c.t, op, tool, args, out)
}

// callQRDBTool runs one go-qrdb tool on t's authenticated MCP session and
// decodes its JSON result into out.
func callQRDBTool(ctx context.Context, t *Transport, op, tool string, args map[string]interface{}, out interface{}) error {
	if t.closed.Load() {
		return errors.NewConnection(op, "transport is closed")
	}
	if err := t.ensureInitialized(ctx); err != nil {
		return err
	}
	if err := t.ensureAuthenticated(ctx); err != nil {
		return err
	}
	result, err := t.callTool(ctx, tool, args)
	if err != nil {
		return err
	}
	var raw []byte
	if s, ok := result.(string); ok {
		raw = []byte(s)
	} else if raw, err = json.Marshal(result); err != nil {
		return fmt.Errorf("%s: encode result: %w", op, err)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("%s: decode result: %w", op, err)
	}
	return nil
}

// Embed embeds texts with go-qrdb's qrdb_embed_text tool.
func (c *QRDBEmbeddingClient) Embed(ctx context.Context, req *qrdbtransport.EmbedRequest) (*qrdbtransport.EmbedResponse, error) {
	if req == nil {
		req = &qrdbtransport.EmbedRequest{}
	}
	args := map[string]interface{}{"texts": req.Texts}
	if req.Model != "" {
		args["model"] = req.Model
	}
	if req.Provider != "" {
		args["provider"] = req.Provider
	}
	var wire struct {
		Embeddings [][]float32
		Model      string
		Cached     bool
		Usage      *qrdbUsageWire
	}
	if err := c.call(ctx, "mcp.QRDBEmbedding.Embed", "qrdb_embed_text", args, &wire); err != nil {
		return nil, err
	}
	return &qrdbtransport.EmbedResponse{
		Embeddings: wire.Embeddings,
		Model:      wire.Model,
		Cached:     wire.Cached,
		Usage:      wire.Usage.usage(),
	}, nil
}

// BatchEmbed embeds texts in batches with go-qrdb's qrdb_batch_embed tool.
func (c *QRDBEmbeddingClient) BatchEmbed(ctx context.Context, req *qrdbtransport.BatchEmbedRequest) (*qrdbtransport.BatchEmbedResponse, error) {
	if req == nil {
		req = &qrdbtransport.BatchEmbedRequest{}
	}
	args := map[string]interface{}{"texts": req.Texts}
	if req.Model != "" {
		args["model"] = req.Model
	}
	if req.Provider != "" {
		args["provider"] = req.Provider
	}
	if req.BatchSize > 0 {
		args["batch_size"] = req.BatchSize
	}
	var wire struct {
		Embeddings [][]float32
		Model      string
		TotalUsage *qrdbUsageWire
	}
	if err := c.call(ctx, "mcp.QRDBEmbedding.BatchEmbed", "qrdb_batch_embed", args, &wire); err != nil {
		return nil, err
	}
	return &qrdbtransport.BatchEmbedResponse{
		Embeddings: wire.Embeddings,
		Model:      wire.Model,
		Usage:      wire.TotalUsage.usage(),
	}, nil
}

// ListEmbeddingModels lists a provider's models with go-qrdb's
// qrdb_list_embedding_models tool. An empty provider names the default one.
func (c *QRDBEmbeddingClient) ListEmbeddingModels(ctx context.Context, provider string) (*qrdbtransport.ListEmbeddingModelsResponse, error) {
	args := map[string]interface{}{}
	if provider != "" {
		args["provider"] = provider
	}
	var wire struct {
		Models []struct {
			Name       string
			Dimensions int
			MaxTokens  int
		}
	}
	if err := c.call(ctx, "mcp.QRDBEmbedding.ListEmbeddingModels", "qrdb_list_embedding_models", args, &wire); err != nil {
		return nil, err
	}
	out := &qrdbtransport.ListEmbeddingModelsResponse{Models: make([]qrdbtransport.EmbeddingModelInfo, 0, len(wire.Models))}
	for _, m := range wire.Models {
		out.Models = append(out.Models, qrdbtransport.EmbeddingModelInfo{Name: m.Name, Dimensions: m.Dimensions, MaxTokens: m.MaxTokens})
	}
	return out, nil
}
