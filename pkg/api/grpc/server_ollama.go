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

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// OllamaServer wraps the Ollama service for gRPC endpoints.
type OllamaServer struct {
	pb.UnimplementedOllamaServiceServer
	ollamaSvc service.OllamaService
	logger    *logger.Logger
}

// NewOllamaServer creates a new gRPC server for Ollama operations.
func NewOllamaServer(ollamaSvc service.OllamaService, log *logger.Logger) *OllamaServer {
	return &OllamaServer{
		ollamaSvc: ollamaSvc,
		logger:    log.WithField("component", "ollama-grpc-server"),
	}
}

// Health returns the health status of the Ollama connection.
func (s *OllamaServer) Health(ctx context.Context, req *pb.OllamaHealthRequest) (*pb.OllamaHealthResponse, error) {
	s.logger.Debug("Processing Ollama Health request")

	svcReq := &service.OllamaHealthRequest{}

	resp, err := s.ollamaSvc.Health(ctx, svcReq)
	if err != nil {
		return nil, convertOllamaError(err)
	}

	return &pb.OllamaHealthResponse{
		Available:   resp.Available,
		Version:     resp.Version,
		BaseUrl:     resp.BaseURL,
		LastChecked: timestamppb.New(resp.LastChecked),
		Error:       resp.Error,
	}, nil
}

// ListModels returns the list of available Ollama models.
func (s *OllamaServer) ListModels(ctx context.Context, req *pb.OllamaListModelsRequest) (*pb.OllamaListModelsResponse, error) {
	s.logger.Debug("Processing Ollama ListModels request")

	svcReq := &service.OllamaListModelsRequest{}

	resp, err := s.ollamaSvc.ListModels(ctx, svcReq)
	if err != nil {
		return nil, convertOllamaError(err)
	}

	models := make([]*pb.OllamaModelInfo, len(resp.Models))
	for i, m := range resp.Models {
		models[i] = &pb.OllamaModelInfo{
			Name:       m.Name,
			Size:       m.Size,
			Digest:     m.Digest,
			ModifiedAt: timestamppb.New(m.ModifiedAt),
		}
	}

	count, err := responseInt32("count", resp.Count)
	if err != nil {
		return nil, err
	}

	return &pb.OllamaListModelsResponse{
		Models: models,
		Count:  count,
	}, nil
}

// GetModelInfo returns detailed information about a specific model.
func (s *OllamaServer) GetModelInfo(ctx context.Context, req *pb.OllamaGetModelInfoRequest) (*pb.OllamaGetModelInfoResponse, error) {
	s.logger.WithField("model", req.GetModel()).Debug("Processing Ollama GetModelInfo request")

	if req.GetModel() == "" {
		return nil, status.Error(codes.InvalidArgument, "model name is required")
	}

	svcReq := &service.OllamaGetModelInfoRequest{
		Model: req.GetModel(),
	}

	resp, err := s.ollamaSvc.GetModelInfo(ctx, svcReq)
	if err != nil {
		return nil, convertOllamaError(err)
	}

	return &pb.OllamaGetModelInfoResponse{
		Model: resp.Model,
		Details: &pb.OllamaModelDetails{
			License:           resp.Details.License,
			Format:            resp.Details.Format,
			Family:            resp.Details.Family,
			ParameterSize:     resp.Details.ParameterSize,
			QuantizationLevel: resp.Details.QuantizationLevel,
		},
	}, nil
}

// Embed generates an embedding for a single text using the default model.
func (s *OllamaServer) Embed(ctx context.Context, req *pb.OllamaEmbedRequest) (*pb.OllamaEmbedResponse, error) {
	s.logger.Debug("Processing Ollama Embed request")

	if req.GetText() == "" {
		return nil, status.Error(codes.InvalidArgument, "text is required")
	}

	svcReq := &service.OllamaEmbedRequest{
		Text: req.GetText(),
	}

	resp, err := s.ollamaSvc.Embed(ctx, svcReq)
	if err != nil {
		return nil, convertOllamaError(err)
	}

	dimensions, err := responseInt32("dimensions", resp.Dimensions)
	if err != nil {
		return nil, err
	}

	return &pb.OllamaEmbedResponse{
		Embedding:      resp.Embedding,
		Model:          resp.Model,
		Dimensions:     dimensions,
		ProcessingTime: durationpb.New(resp.ProcessingTime),
	}, nil
}

// EmbedWithModel generates an embedding for a single text using a specific model.
func (s *OllamaServer) EmbedWithModel(ctx context.Context, req *pb.OllamaEmbedWithModelRequest) (*pb.OllamaEmbedResponse, error) {
	s.logger.WithField("model", req.GetModel()).Debug("Processing Ollama EmbedWithModel request")

	if req.GetText() == "" {
		return nil, status.Error(codes.InvalidArgument, "text is required")
	}

	if req.GetModel() == "" {
		return nil, status.Error(codes.InvalidArgument, "model is required")
	}

	svcReq := &service.OllamaEmbedWithModelRequest{
		Text:  req.GetText(),
		Model: req.GetModel(),
	}

	resp, err := s.ollamaSvc.EmbedWithModel(ctx, svcReq)
	if err != nil {
		return nil, convertOllamaError(err)
	}

	dimensions, err := responseInt32("dimensions", resp.Dimensions)
	if err != nil {
		return nil, err
	}

	return &pb.OllamaEmbedResponse{
		Embedding:      resp.Embedding,
		Model:          resp.Model,
		Dimensions:     dimensions,
		ProcessingTime: durationpb.New(resp.ProcessingTime),
	}, nil
}

// EmbedBatch generates embeddings for multiple texts.
func (s *OllamaServer) EmbedBatch(ctx context.Context, req *pb.OllamaEmbedBatchRequest) (*pb.OllamaEmbedBatchResponse, error) {
	s.logger.WithField("count", len(req.GetTexts())).Debug("Processing Ollama EmbedBatch request")

	if len(req.GetTexts()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "texts are required")
	}

	svcReq := &service.OllamaEmbedBatchRequest{
		Texts: req.GetTexts(),
		Model: req.GetModel(),
	}

	resp, err := s.ollamaSvc.EmbedBatch(ctx, svcReq)
	if err != nil {
		return nil, convertOllamaError(err)
	}

	embeddings := make([]*pb.OllamaEmbeddingVector, len(resp.Embeddings))
	for i, emb := range resp.Embeddings {
		embeddings[i] = &pb.OllamaEmbeddingVector{
			Values: emb,
		}
	}

	n := &responseNarrower{}
	batch := &pb.OllamaEmbedBatchResponse{
		Embeddings:     embeddings,
		Model:          resp.Model,
		Dimensions:     n.int32("dimensions", resp.Dimensions),
		Count:          n.int32("count", resp.Count),
		ProcessingTime: durationpb.New(resp.ProcessingTime),
	}
	if err := n.err(); err != nil {
		return nil, err
	}
	return batch, nil
}

// convertOllamaError converts Ollama service errors to gRPC status errors.
func convertOllamaError(err error) error {
	if err == nil {
		return nil
	}

	// Use the standard ToGRPCError for QBError types
	return ToGRPCError(err)
}

// Ensure OllamaServer implements the interface.
var _ pb.OllamaServiceServer = (*OllamaServer)(nil)
