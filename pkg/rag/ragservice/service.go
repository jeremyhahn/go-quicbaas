//go:build rag

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

// Package ragservice is QuicBaaS's own RAG service: the one Query and the one
// BuildContext that every transport serves (#1611). REST, MCP, gRPC and the
// embedded SDK transport parse their wire form into a QueryRequest or a
// BuildContextRequest, call this package, and render its Response; none of
// them embeds, searches or assembles context itself.
//
// This is the parallel implementation SPEC row rag (#1672) keeps beside
// go-qrdb's RAG service until the owner retires it.
package ragservice

import (
	"context"
	"fmt"
	"strconv"

	"github.com/jeremyhahn/go-quicbaas/pkg/api"
	ragctx "github.com/jeremyhahn/go-quicbaas/pkg/rag/context"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// Defaults applied when a request leaves a field at its zero value.
const (
	DefaultTopK      = 10
	DefaultMaxTokens = 4000
	DefaultMinScore  = float32(0.7)
)

// Backends are the services a RAG query needs. service.QuicBaaS satisfies it.
type Backends interface {
	Ollama() service.OllamaService
	Vector() service.VectorService
}

// QueryRequest is a RAG query: embed Query, search Namespace, assemble context.
type QueryRequest struct {
	Query            string
	Namespace        string
	TopK             int     // <= 0 means DefaultTopK
	MaxTokens        int     // <= 0 means DefaultMaxTokens
	MinScore         float32 // <= 0 means DefaultMinScore
	Template         string  // markdown (default), json, xml or plain
	IncludeCitations bool
	Model            string // embedding model; empty means the default model
}

// ResultInput is one caller-supplied search result for BuildContext.
type ResultInput struct {
	Content  string
	Score    float32
	Metadata map[string]string // file_path, file_name, language, start_line, end_line
}

// BuildContextRequest assembles context from caller-supplied results.
type BuildContextRequest struct {
	Results            []ResultInput
	MaxTokens          int    // <= 0 means DefaultMaxTokens
	Template           string // markdown (default), json, xml or plain
	IncludeCitations   bool
	TruncationStrategy string // smart (default), hard or none
}

// Citation is one source the assembled context cites.
type Citation struct {
	Index     int
	FilePath  string
	StartLine int
	EndLine   int
	Score     float32
}

// Response is the assembled context, for Query and BuildContext alike.
type Response struct {
	Context     string
	Tokens      int
	Truncated   bool
	ResultCount int
	Citations   []Citation // only when the request asked for citations
}

// Query embeds the query, searches the namespace for similar vectors and
// assembles the hits into context.
//
// Errors are typed so each transport maps them the same way: a missing query
// or namespace is invalid input, a missing Ollama or vector service is
// unavailable, and an embedding, search or assembly failure keeps the code of
// its cause (internal when it has none).
func Query(ctx context.Context, b Backends, req QueryRequest) (*Response, error) {
	if req.Query == "" {
		return nil, api.NewRequiredFieldError("query")
	}
	if req.Namespace == "" {
		return nil, api.NewRequiredFieldError("namespace")
	}
	topK := req.TopK
	if topK <= 0 {
		topK = DefaultTopK
	}
	minScore := req.MinScore
	if minScore <= 0 {
		minScore = DefaultMinScore
	}

	ollama := b.Ollama()
	if ollama == nil {
		return nil, api.NewServiceNotConfiguredError("ollama")
	}
	embedding, err := embed(ctx, ollama, req.Query, req.Model)
	if err != nil {
		return nil, api.NewOperationFailedError("failed to generate query embedding", err)
	}

	vector := b.Vector()
	if vector == nil {
		return nil, api.NewServiceNotConfiguredError("vector")
	}
	found, err := vector.SearchSimilar(ctx, &service.SearchSimilarRequest{
		Namespace:       req.Namespace,
		QueryVector:     embedding,
		K:               topK,
		MinScore:        minScore,
		IncludeMetadata: true,
		IncludeVectors:  false,
	})
	if err != nil {
		return nil, api.NewOperationFailedError("vector search", err)
	}

	results := make([]ragctx.Result, len(found.Results))
	for i, hit := range found.Results {
		results[i] = ragctx.Result{
			ID:          fmt.Sprintf("%d", hit.ID),
			Content:     anyString(hit.Metadata, "content"),
			VectorScore: hit.Score,
			FusionScore: hit.Score,
			Metadata: ragctx.Metadata{
				FilePath:  anyString(hit.Metadata, "file_path"),
				FileName:  anyString(hit.Metadata, "file_name"),
				Language:  anyString(hit.Metadata, "language"),
				StartLine: anyInt(hit.Metadata, "start_line"),
				EndLine:   anyInt(hit.Metadata, "end_line"),
			},
		}
	}
	return assemble(results, req.MaxTokens, req.Template, req.IncludeCitations, ragctx.TruncationSmart)
}

// BuildContext assembles context from caller-supplied results. An empty
// result list is invalid input.
func BuildContext(_ context.Context, req BuildContextRequest) (*Response, error) {
	if len(req.Results) == 0 {
		return nil, api.NewEmptyCollectionError("results array")
	}
	results := make([]ragctx.Result, len(req.Results))
	for i, in := range req.Results {
		results[i] = ragctx.Result{
			ID:          fmt.Sprintf("result_%d", i),
			Content:     in.Content,
			FusionScore: in.Score,
			Metadata: ragctx.Metadata{
				FilePath:  in.Metadata["file_path"],
				FileName:  in.Metadata["file_name"],
				Language:  in.Metadata["language"],
				StartLine: stringInt(in.Metadata, "start_line"),
				EndLine:   stringInt(in.Metadata, "end_line"),
			},
		}
	}
	return assemble(results, req.MaxTokens, req.Template, req.IncludeCitations, ParseTruncationStrategy(req.TruncationStrategy))
}

// ParseTemplate maps a template name to its template; anything else is
// markdown.
func ParseTemplate(s string) ragctx.Template {
	switch s {
	case "json":
		return ragctx.TemplateJSON
	case "xml":
		return ragctx.TemplateXML
	case "plain":
		return ragctx.TemplatePlain
	default:
		return ragctx.TemplateMarkdown
	}
}

// ParseTruncationStrategy maps a strategy name to its strategy; anything else
// is smart.
func ParseTruncationStrategy(s string) ragctx.TruncationStrategy {
	switch s {
	case "hard":
		return ragctx.TruncationHard
	case "none":
		return ragctx.TruncationNone
	default:
		return ragctx.TruncationSmart
	}
}

func embed(ctx context.Context, ollama service.OllamaService, text, model string) ([]float32, error) {
	if model != "" {
		resp, err := ollama.EmbedWithModel(ctx, &service.OllamaEmbedWithModelRequest{Text: text, Model: model})
		if err != nil {
			return nil, err
		}
		return resp.Embedding, nil
	}
	resp, err := ollama.Embed(ctx, &service.OllamaEmbedRequest{Text: text})
	if err != nil {
		return nil, err
	}
	return resp.Embedding, nil
}

func assemble(results []ragctx.Result, maxTokens int, template string, citations bool, truncation ragctx.TruncationStrategy) (*Response, error) {
	if maxTokens <= 0 {
		maxTokens = DefaultMaxTokens
	}
	tmpl := ParseTemplate(template)
	builder := ragctx.NewBuilderWithConfig(ragctx.Config{
		DefaultMaxTokens:   maxTokens,
		DefaultTemplate:    tmpl,
		IncludeCitations:   citations,
		TruncationStrategy: truncation,
		MaxResultsPerChunk: 20,
		TokenMultiplier:    1.3,
	})
	built, err := builder.BuildWithTemplate(results, tmpl, maxTokens)
	if err != nil {
		return nil, api.NewOperationFailedError("context building", err)
	}
	resp := &Response{
		Context:     built.Content,
		Tokens:      built.Tokens,
		Truncated:   built.Truncated,
		ResultCount: len(results),
	}
	if citations && len(built.Citations) > 0 {
		resp.Citations = make([]Citation, len(built.Citations))
		for i, c := range built.Citations {
			resp.Citations[i] = Citation{
				Index:     c.Index,
				FilePath:  c.FilePath,
				StartLine: c.StartLine,
				EndLine:   c.EndLine,
				Score:     c.Score,
			}
		}
	}
	return resp, nil
}

func anyString(m map[string]interface{}, key string) string {
	if s, ok := m[key].(string); ok {
		return s
	}
	return ""
}

func anyInt(m map[string]interface{}, key string) int {
	switch v := m[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case string:
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return 0
}

func stringInt(m map[string]string, key string) int {
	if i, err := strconv.Atoi(m[key]); err == nil {
		return i
	}
	return 0
}
