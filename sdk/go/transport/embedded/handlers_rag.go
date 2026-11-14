//go:build rag

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

package embedded

import (
	"context"

	"github.com/jeremyhahn/go-quicbaas/pkg/rag/ragservice"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/rag"
)

// ragServiceName is the service name the RAG client sends.
const ragServiceName = "quicbaas.RAG"

// The RAG verbs call the one RAG service REST, MCP and gRPC serve (#1611);
// the embedded transport only converts the SDK types.
func init() {
	unaryHandlers[ragServiceName+"/Query"] = handleRAGQuery
	unaryHandlers[ragServiceName+"/BuildContext"] = handleRAGBuildContext
}

func handleRAGQuery(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*rag.QueryRequest)
	if !ok || r == nil {
		return nil, NewInvalidRequestTypeError(ragServiceName, "Query", "*rag.QueryRequest", typeNameOf(req))
	}
	out, err := ragservice.Query(ctx, svc, ragservice.QueryRequest{
		Query:            r.Query,
		Namespace:        r.Namespace,
		TopK:             r.TopK,
		MaxTokens:        r.MaxTokens,
		MinScore:         r.MinScore,
		Template:         r.Template,
		IncludeCitations: r.IncludeCitations,
		Model:            r.Model,
	})
	if err != nil {
		return nil, err
	}
	return &rag.QueryResponse{
		Context:     out.Context,
		Tokens:      out.Tokens,
		Truncated:   out.Truncated,
		ResultCount: out.ResultCount,
		Citations:   ragCitationsToSDK(out.Citations),
	}, nil
}

func handleRAGBuildContext(ctx context.Context, _ service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*rag.BuildContextRequest)
	if !ok || r == nil {
		return nil, NewInvalidRequestTypeError(ragServiceName, "BuildContext", "*rag.BuildContextRequest", typeNameOf(req))
	}
	in := make([]ragservice.ResultInput, len(r.Results))
	for i, res := range r.Results {
		in[i] = ragservice.ResultInput{Content: res.Content, Score: res.Score, Metadata: res.Metadata}
	}
	out, err := ragservice.BuildContext(ctx, ragservice.BuildContextRequest{
		Results:            in,
		MaxTokens:          r.MaxTokens,
		Template:           r.Template,
		IncludeCitations:   r.IncludeCitations,
		TruncationStrategy: r.TruncationStrategy,
	})
	if err != nil {
		return nil, err
	}
	return &rag.BuildContextResponse{
		Context:     out.Context,
		Tokens:      out.Tokens,
		Truncated:   out.Truncated,
		ResultCount: out.ResultCount,
		Citations:   ragCitationsToSDK(out.Citations),
	}, nil
}

func ragCitationsToSDK(in []ragservice.Citation) []rag.Citation {
	if len(in) == 0 {
		return nil
	}
	out := make([]rag.Citation, len(in))
	for i, c := range in {
		out[i] = rag.Citation{Index: c.Index, FilePath: c.FilePath, StartLine: c.StartLine, EndLine: c.EndLine, Score: c.Score}
	}
	return out
}
