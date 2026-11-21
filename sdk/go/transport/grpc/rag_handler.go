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

	"google.golang.org/grpc"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/rag"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// ragHandler calls the node's quicbaas.rag.v1.RAGService.
type ragHandler struct {
	conn *grpc.ClientConn
}

// newRAGHandler creates a new RAG handler.
func newRAGHandler(conn *grpc.ClientConn) *ragHandler {
	return &ragHandler{conn: conn}
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
		return nil, errors.NewInvalidInput("grpc.rag.handleRequest", "unknown method: "+method)
	}
}

// handleQuery calls RAGService.Query.
func (h *ragHandler) handleQuery(ctx context.Context, req interface{}) (interface{}, error) {
	const op = "grpc.rag.handleQuery"
	queryReq, ok := req.(*rag.QueryRequest)
	if !ok || queryReq == nil {
		return nil, errors.NewInvalidInput(op, "invalid request type")
	}
	pbReq, err := ragQueryRequestToProto(op, queryReq)
	if err != nil {
		return nil, err
	}
	resp, err := pb.NewRAGServiceClient(h.conn).Query(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	return &rag.QueryResponse{
		Context:     resp.GetContext(),
		Tokens:      int(resp.GetTokens()),
		Truncated:   resp.GetTruncated(),
		ResultCount: int(resp.GetResultCount()),
		Citations:   ragCitationsFromProto(resp.GetCitations()),
	}, nil
}

// handleBuildContext calls RAGService.BuildContext.
func (h *ragHandler) handleBuildContext(ctx context.Context, req interface{}) (interface{}, error) {
	const op = "grpc.rag.handleBuildContext"
	buildReq, ok := req.(*rag.BuildContextRequest)
	if !ok || buildReq == nil {
		return nil, errors.NewInvalidInput(op, "invalid request type")
	}
	pbReq, err := ragBuildContextRequestToProto(op, buildReq)
	if err != nil {
		return nil, err
	}
	resp, err := pb.NewRAGServiceClient(h.conn).BuildContext(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	return &rag.BuildContextResponse{
		Context:     resp.GetContext(),
		Tokens:      int(resp.GetTokens()),
		Truncated:   resp.GetTruncated(),
		ResultCount: int(resp.GetResultCount()),
		Citations:   ragCitationsFromProto(resp.GetCitations()),
	}, nil
}

// ragQueryRequestToProto maps the SDK query to the wire message. min_score
// is sent only when the caller set it, so an omitted threshold takes the
// server's default and an explicit 0 disables the filter.
func ragQueryRequestToProto(op string, r *rag.QueryRequest) (*pb.RAGQueryRequest, error) {
	topK, err := int32ToProto(op, "top_k", r.TopK)
	if err != nil {
		return nil, err
	}
	maxTokens, err := int32ToProto(op, "max_tokens", r.MaxTokens)
	if err != nil {
		return nil, err
	}
	template, err := ragTemplateToProto(op, r.Template)
	if err != nil {
		return nil, err
	}
	out := &pb.RAGQueryRequest{
		Query:            r.Query,
		Namespace:        r.Namespace,
		TopK:             topK,
		MaxTokens:        maxTokens,
		Template:         template,
		IncludeCitations: r.IncludeCitations,
		Model:            r.Model,
	}
	if r.MinScoreSet || r.MinScore != 0 {
		minScore := r.MinScore
		out.MinScore = &minScore
	}
	return out, nil
}

// ragBuildContextRequestToProto maps the SDK build-context request to the
// wire message.
func ragBuildContextRequestToProto(op string, r *rag.BuildContextRequest) (*pb.RAGBuildContextRequest, error) {
	maxTokens, err := int32ToProto(op, "max_tokens", r.MaxTokens)
	if err != nil {
		return nil, err
	}
	template, err := ragTemplateToProto(op, r.Template)
	if err != nil {
		return nil, err
	}
	truncation, err := ragTruncationToProto(op, r.TruncationStrategy)
	if err != nil {
		return nil, err
	}
	results := make([]*pb.RAGResultInput, len(r.Results))
	for i, in := range r.Results {
		results[i] = &pb.RAGResultInput{Content: in.Content, Score: in.Score, Metadata: in.Metadata}
	}
	return &pb.RAGBuildContextRequest{
		Results:            results,
		MaxTokens:          maxTokens,
		Template:           template,
		IncludeCitations:   r.IncludeCitations,
		TruncationStrategy: truncation,
	}, nil
}

// ragTemplateToProto maps a template name to the wire enum; "" is the
// server's default.
func ragTemplateToProto(op, name string) (pb.RAGTemplate, error) {
	switch name {
	case "":
		return pb.RAGTemplate_RAG_TEMPLATE_UNSPECIFIED, nil
	case "markdown":
		return pb.RAGTemplate_RAG_TEMPLATE_MARKDOWN, nil
	case "json":
		return pb.RAGTemplate_RAG_TEMPLATE_JSON, nil
	case "xml":
		return pb.RAGTemplate_RAG_TEMPLATE_XML, nil
	case "plain":
		return pb.RAGTemplate_RAG_TEMPLATE_PLAIN, nil
	default:
		return 0, errors.NewInvalidInput(op, "unknown template: "+name)
	}
}

// ragTruncationToProto maps a truncation strategy name to the wire enum; ""
// is the server's default.
func ragTruncationToProto(op, name string) (pb.RAGTruncationStrategy, error) {
	switch name {
	case "":
		return pb.RAGTruncationStrategy_RAG_TRUNCATION_UNSPECIFIED, nil
	case "smart":
		return pb.RAGTruncationStrategy_RAG_TRUNCATION_SMART, nil
	case "hard":
		return pb.RAGTruncationStrategy_RAG_TRUNCATION_HARD, nil
	case "none":
		return pb.RAGTruncationStrategy_RAG_TRUNCATION_NONE, nil
	default:
		return 0, errors.NewInvalidInput(op, "unknown truncation strategy: "+name)
	}
}

// ragCitationsFromProto maps wire citations to the SDK's.
func ragCitationsFromProto(in []*pb.RAGCitation) []rag.Citation {
	if len(in) == 0 {
		return nil
	}
	out := make([]rag.Citation, len(in))
	for i, c := range in {
		out[i] = rag.Citation{
			Index:     int(c.GetIndex()),
			FilePath:  c.GetFilePath(),
			StartLine: int(c.GetStartLine()),
			EndLine:   int(c.GetEndLine()),
			Score:     c.GetScore(),
		}
	}
	return out
}
