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

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/rag"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// ragHandler handles RAG REST requests.
type ragHandler struct {
	transport *Transport
}

// newRAGHandler creates a new RAG handler.
func newRAGHandler(t *Transport) *ragHandler {
	return &ragHandler{transport: t}
}

// isRAGService returns true if the service is a RAG service.
func isRAGService(service string) bool {
	return service == "quicbaas.RAG" || service == "RAG"
}

// handleRequest handles a RAG request.
func (h *ragHandler) handleRequest(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
	switch method {
	case "Query":
		return h.handleQuery(ctx, req)
	case "BuildContext":
		return h.handleBuildContext(ctx, req)
	default:
		return nil, errors.NewInvalidInput("rest.rag.handleRequest", "unknown method: "+method)
	}
}

// handleQuery handles a RAG Query request.
func (h *ragHandler) handleQuery(ctx context.Context, req interface{}) (interface{}, error) {
	queryReq, ok := req.(*rag.QueryRequest)
	if !ok {
		return nil, errors.NewInvalidInput("rest.rag.handleQuery", "invalid request type")
	}

	// Convert to REST request format
	restReq := map[string]interface{}{
		"query":             queryReq.Query,
		"namespace":         queryReq.Namespace,
		"top_k":             queryReq.TopK,
		"max_tokens":        queryReq.MaxTokens,
		"min_score":         queryReq.MinScore,
		"template":          queryReq.Template,
		"include_citations": queryReq.IncludeCitations,
		"model":             queryReq.Model,
	}

	// Make REST request
	var respMap map[string]interface{}
	if err := h.transport.doJSONRequest(ctx, http.MethodPost, "/api/v1/rag/query", restReq, &respMap); err != nil {
		return nil, err
	}

	return convertRAGQueryResponse(respMap)
}

// handleBuildContext handles a RAG BuildContext request.
func (h *ragHandler) handleBuildContext(ctx context.Context, req interface{}) (interface{}, error) {
	buildReq, ok := req.(*rag.BuildContextRequest)
	if !ok {
		return nil, errors.NewInvalidInput("rest.rag.handleBuildContext", "invalid request type")
	}

	// Convert results to map format
	results := make([]map[string]interface{}, len(buildReq.Results))
	for i, r := range buildReq.Results {
		results[i] = map[string]interface{}{
			"content":  r.Content,
			"score":    r.Score,
			"metadata": r.Metadata,
		}
	}

	// Convert to REST request format
	restReq := map[string]interface{}{
		"results":             results,
		"max_tokens":          buildReq.MaxTokens,
		"template":            buildReq.Template,
		"include_citations":   buildReq.IncludeCitations,
		"truncation_strategy": buildReq.TruncationStrategy,
	}

	// Make REST request
	var respMap map[string]interface{}
	if err := h.transport.doJSONRequest(ctx, http.MethodPost, "/api/v1/rag/context", restReq, &respMap); err != nil {
		return nil, err
	}

	return convertRAGBuildContextResponse(respMap)
}

// convertRAGQueryResponse converts a raw map to a QueryResponse.
func convertRAGQueryResponse(m map[string]interface{}) (*rag.QueryResponse, error) {
	resp := &rag.QueryResponse{}

	if v, ok := m["context"].(string); ok {
		resp.Context = v
	}
	if v, ok := m["tokens"].(float64); ok {
		resp.Tokens = int(v)
	}
	if v, ok := m["truncated"].(bool); ok {
		resp.Truncated = v
	}
	if v, ok := m["result_count"].(float64); ok {
		resp.ResultCount = int(v)
	}

	if citations, ok := m["citations"].([]interface{}); ok {
		resp.Citations = make([]rag.Citation, len(citations))
		for i, c := range citations {
			if cm, ok := c.(map[string]interface{}); ok {
				resp.Citations[i] = convertCitation(cm)
			}
		}
	}

	return resp, nil
}

// convertRAGBuildContextResponse converts a raw map to a BuildContextResponse.
func convertRAGBuildContextResponse(m map[string]interface{}) (*rag.BuildContextResponse, error) {
	resp := &rag.BuildContextResponse{}

	if v, ok := m["context"].(string); ok {
		resp.Context = v
	}
	if v, ok := m["tokens"].(float64); ok {
		resp.Tokens = int(v)
	}
	if v, ok := m["truncated"].(bool); ok {
		resp.Truncated = v
	}
	if v, ok := m["result_count"].(float64); ok {
		resp.ResultCount = int(v)
	}

	if citations, ok := m["citations"].([]interface{}); ok {
		resp.Citations = make([]rag.Citation, len(citations))
		for i, c := range citations {
			if cm, ok := c.(map[string]interface{}); ok {
				resp.Citations[i] = convertCitation(cm)
			}
		}
	}

	return resp, nil
}

// convertCitation converts a map to a Citation.
func convertCitation(m map[string]interface{}) rag.Citation {
	c := rag.Citation{}

	if v, ok := m["index"].(float64); ok {
		c.Index = int(v)
	}
	if v, ok := m["file_path"].(string); ok {
		c.FilePath = v
	}
	if v, ok := m["start_line"].(float64); ok {
		c.StartLine = int(v)
	}
	if v, ok := m["end_line"].(float64); ok {
		c.EndLine = int(v)
	}
	if v, ok := m["score"].(float64); ok {
		c.Score = float32(v)
	}

	return c
}
