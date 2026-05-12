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
	stderrors "errors"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// Inference handles the unary Inference RPC. It calls agentSvc.Infer, drains
// the chunk channel, and returns a single aggregated InferenceResponse.
func (s *OrchestratorServer) Inference(ctx context.Context, req *pb.InferenceRequest) (*pb.InferenceResponse, error) {
	s.logger.WithField("backend", req.Backend).Debug("Processing Inference request")

	if req.Backend == "" {
		return nil, status.Error(codes.InvalidArgument, "backend is required")
	}
	if req.Prompt == "" {
		return nil, status.Error(codes.InvalidArgument, "prompt is required")
	}

	tenantID := req.GetTenantId()
	if tenantID == "" {
		tenantID = "default"
	}
	tenantCtx := orchestration.ContextWithTenant(ctx, tenantID)
	if req.GetHostAffinity() != "" {
		tenantCtx = orchestration.WithHostAffinity(tenantCtx, req.GetHostAffinity())
	}
	if req.GetGpuRequired() {
		tenantCtx = orchestration.WithGpuRequired(tenantCtx)
	}
	if req.GetHostAffinityStrict() {
		tenantCtx = orchestration.WithHostAffinityStrict(tenantCtx)
	}

	inferReq := &orchestration.InferRequest{
		Backend:            req.Backend,
		Model:              req.Model,
		Prompt:             req.Prompt,
		SystemPrompt:       req.SystemPrompt,
		TenantID:           tenantID,
		HostAffinity:       req.HostAffinity,
		GpuRequired:        req.GpuRequired,
		HostAffinityStrict: req.HostAffinityStrict,
		Metadata:           req.Metadata,
	}

	chunks, err := s.agentSvc.Infer(tenantCtx, inferReq)
	if err != nil {
		return nil, convertInferError(err)
	}

	resp, err := orchestration.AccumulateChunks(chunks)
	if err != nil {
		return nil, convertInferError(err)
	}

	return &pb.InferenceResponse{
		Content:    serviceContentBlocksToProto(resp.Content),
		Model:      resp.Model,
		StopReason: resp.StopReason,
	}, nil
}

// InferenceStream handles the server-streaming InferenceStream RPC. It calls
// agentSvc.Infer and forwards each InferChunk as an InferenceChunk proto
// message until the channel is closed.
func (s *OrchestratorServer) InferenceStream(req *pb.InferenceRequest, stream grpc.ServerStreamingServer[pb.InferenceChunk]) error {
	s.logger.WithField("backend", req.Backend).Debug("Processing InferenceStream request")

	if req.Backend == "" {
		return status.Error(codes.InvalidArgument, "backend is required")
	}
	if req.Prompt == "" {
		return status.Error(codes.InvalidArgument, "prompt is required")
	}

	tenantID := req.GetTenantId()
	if tenantID == "" {
		tenantID = "default"
	}
	ctx := stream.Context()
	tenantCtx := orchestration.ContextWithTenant(ctx, tenantID)
	if req.GetHostAffinity() != "" {
		tenantCtx = orchestration.WithHostAffinity(tenantCtx, req.GetHostAffinity())
	}
	if req.GetGpuRequired() {
		tenantCtx = orchestration.WithGpuRequired(tenantCtx)
	}
	if req.GetHostAffinityStrict() {
		tenantCtx = orchestration.WithHostAffinityStrict(tenantCtx)
	}

	inferReq := &orchestration.InferRequest{
		Backend:            req.Backend,
		Model:              req.Model,
		Prompt:             req.Prompt,
		SystemPrompt:       req.SystemPrompt,
		TenantID:           tenantID,
		HostAffinity:       req.HostAffinity,
		GpuRequired:        req.GpuRequired,
		HostAffinityStrict: req.HostAffinityStrict,
		Metadata:           req.Metadata,
	}

	chunks, err := s.agentSvc.Infer(tenantCtx, inferReq)
	if err != nil {
		return convertInferError(err)
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case chunk, ok := <-chunks:
			if !ok {
				return nil
			}
			if chunk == nil {
				continue
			}
			protoChunk := &pb.InferenceChunk{
				Delta:      serviceContentBlocksToProto(chunk.Delta),
				Model:      chunk.Model,
				StopReason: chunk.StopReason,
				Final:      chunk.Final,
			}
			if sendErr := stream.Send(protoChunk); sendErr != nil {
				return sendErr
			}
		}
	}
}

// convertInferError converts inference-specific errors to gRPC status errors.
// It handles InferUnsupportedError and InferBackendError before falling back
// to the general orchestrator error converter.
func convertInferError(err error) error {
	if err == nil {
		return nil
	}

	var unsup *orchestration.InferUnsupportedError
	if stderrors.As(err, &unsup) {
		return status.Error(codes.Unimplemented, unsup.Error())
	}

	var backendErr *orchestration.InferBackendError
	if stderrors.As(err, &backendErr) {
		return status.Error(codes.Internal, backendErr.Error())
	}

	return convertOrchestratorError(err)
}
