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

package grpc

import (
	"context"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/rag/ragservice"
)

// RAGServer serves quicbaas.rag.v1.RAGService over the one RAG service REST,
// MCP and the embedded transport call (#1611). It parses the protobuf
// request, calls ragservice and renders its response; it holds no pipeline of
// its own.
type RAGServer struct {
	pb.UnimplementedRAGServiceServer
	backends ragservice.Backends
	logger   *logger.Logger
}

// NewRAGServer creates the RAG gRPC server over backends (service.QuicBaaS).
func NewRAGServer(backends ragservice.Backends, log *logger.Logger) *RAGServer {
	return &RAGServer{
		backends: backends,
		logger:   log.WithField("component", "rag-grpc-server"),
	}
}

// Query embeds the query, searches the namespace and assembles context.
func (s *RAGServer) Query(ctx context.Context, req *pb.RAGQueryRequest) (*pb.RAGQueryResponse, error) {
	out, err := ragservice.Query(ctx, s.backends, ragservice.QueryRequest{
		Query:            req.GetQuery(),
		Namespace:        req.GetNamespace(),
		TopK:             int(req.GetTopK()),
		MaxTokens:        int(req.GetMaxTokens()),
		MinScore:         req.GetMinScore(),
		Template:         ragTemplateName(req.GetTemplate()),
		IncludeCitations: req.GetIncludeCitations(),
		Model:            req.GetModel(),
	})
	if err != nil {
		return nil, ToGRPCError(err)
	}
	tokens, resultCount, citations, err := ragCounts(out)
	if err != nil {
		return nil, err
	}
	return &pb.RAGQueryResponse{
		Context:     out.Context,
		Tokens:      tokens,
		Truncated:   out.Truncated,
		ResultCount: resultCount,
		Citations:   citations,
	}, nil
}

// BuildContext assembles context from the caller's results.
func (s *RAGServer) BuildContext(ctx context.Context, req *pb.RAGBuildContextRequest) (*pb.RAGBuildContextResponse, error) {
	in := make([]ragservice.ResultInput, len(req.GetResults()))
	for i, r := range req.GetResults() {
		in[i] = ragservice.ResultInput{Content: r.GetContent(), Score: r.GetScore(), Metadata: r.GetMetadata()}
	}
	out, err := ragservice.BuildContext(ctx, ragservice.BuildContextRequest{
		Results:            in,
		MaxTokens:          int(req.GetMaxTokens()),
		Template:           ragTemplateName(req.GetTemplate()),
		IncludeCitations:   req.GetIncludeCitations(),
		TruncationStrategy: ragTruncationName(req.GetTruncationStrategy()),
	})
	if err != nil {
		return nil, ToGRPCError(err)
	}
	tokens, resultCount, citations, err := ragCounts(out)
	if err != nil {
		return nil, err
	}
	return &pb.RAGBuildContextResponse{
		Context:     out.Context,
		Tokens:      tokens,
		Truncated:   out.Truncated,
		ResultCount: resultCount,
		Citations:   citations,
	}, nil
}

// ragTemplateName maps the wire enum to ragservice's template name;
// unspecified is the service default (markdown).
func ragTemplateName(t pb.RAGTemplate) string {
	switch t {
	case pb.RAGTemplate_RAG_TEMPLATE_JSON:
		return "json"
	case pb.RAGTemplate_RAG_TEMPLATE_XML:
		return "xml"
	case pb.RAGTemplate_RAG_TEMPLATE_PLAIN:
		return "plain"
	case pb.RAGTemplate_RAG_TEMPLATE_MARKDOWN:
		return "markdown"
	default:
		return ""
	}
}

// ragTruncationName maps the wire enum to ragservice's strategy name;
// unspecified is the service default (smart).
func ragTruncationName(t pb.RAGTruncationStrategy) string {
	switch t {
	case pb.RAGTruncationStrategy_RAG_TRUNCATION_HARD:
		return "hard"
	case pb.RAGTruncationStrategy_RAG_TRUNCATION_NONE:
		return "none"
	case pb.RAGTruncationStrategy_RAG_TRUNCATION_SMART:
		return "smart"
	default:
		return ""
	}
}

// ragCounts narrows the response's counts to the wire's int32, refusing a
// value that would not fit rather than wrapping it.
func ragCounts(out *ragservice.Response) (tokens, resultCount int32, citations []*pb.RAGCitation, err error) {
	if tokens, err = responseInt32("tokens", out.Tokens); err != nil {
		return 0, 0, nil, err
	}
	if resultCount, err = responseInt32("result_count", out.ResultCount); err != nil {
		return 0, 0, nil, err
	}
	for _, c := range out.Citations {
		index, err := responseInt32("citation index", c.Index)
		if err != nil {
			return 0, 0, nil, err
		}
		start, err := responseInt32("citation start_line", c.StartLine)
		if err != nil {
			return 0, 0, nil, err
		}
		end, err := responseInt32("citation end_line", c.EndLine)
		if err != nil {
			return 0, 0, nil, err
		}
		citations = append(citations, &pb.RAGCitation{
			Index:     index,
			FilePath:  c.FilePath,
			StartLine: start,
			EndLine:   end,
			Score:     c.Score,
		})
	}
	return tokens, resultCount, citations, nil
}
