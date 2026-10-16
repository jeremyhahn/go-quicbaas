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

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/prompts"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// PromptServiceProvider resolves the prompt service per call, so a node
// without a prompt store answers Unavailable rather than leaving the service
// unregistered.
type PromptServiceProvider interface {
	Prompts() *service.PromptService
}

// PromptServer implements pb.PromptServiceServer over the prompt service.
type PromptServer struct {
	pb.UnimplementedPromptServiceServer
	provider PromptServiceProvider
	logger   *logger.Logger
}

// NewPromptServer creates the gRPC server for prompt sets.
func NewPromptServer(provider PromptServiceProvider, log *logger.Logger) *PromptServer {
	return &PromptServer{provider: provider, logger: log.WithField("component", "prompt-grpc-server")}
}

var _ pb.PromptServiceServer = (*PromptServer)(nil)

// promptFaultCode is the gRPC code of each prompt refusal.
var promptFaultCode = map[service.PromptFault]codes.Code{
	service.PromptFaultInvalid:     codes.InvalidArgument,
	service.PromptFaultNotFound:    codes.NotFound,
	service.PromptFaultConflict:    codes.Aborted,
	service.PromptFaultUnavailable: codes.Unavailable,
}

// convertPromptError maps a prompt service error onto its gRPC code; an
// error the service does not classify is Internal.
func convertPromptError(err error) error {
	code := codes.Internal
	if fault, ok := service.ClassifyPromptError(err); ok {
		code = promptFaultCode[fault]
	}
	return status.Error(code, err.Error())
}

// promptService resolves the mounted prompt service or answers Unavailable.
func (s *PromptServer) promptService() (*service.PromptService, error) {
	if s.provider == nil {
		return nil, status.Error(codes.Unavailable, service.ErrPromptsUnavailable.Error())
	}
	svc := s.provider.Prompts()
	if svc == nil {
		return nil, status.Error(codes.Unavailable, service.ErrPromptsUnavailable.Error())
	}
	return svc, nil
}

// promptTarget resolves the service and the address a request names, in
// the caller's own tenant; naming another tenant is refused.
func (s *PromptServer) promptTarget(ctx context.Context, addr *pb.PromptAddress) (*service.PromptService, service.PromptAddress, error) {
	svc, err := s.promptService()
	if err != nil {
		return nil, service.PromptAddress{}, err
	}
	tenantID, err := tenantctx.Target(ctx, "grpc.Prompts", addr.GetTenantId())
	if err != nil {
		return nil, service.PromptAddress{}, toProtoError(err)
	}
	return svc, service.PromptAddress{
		TenantID:  tenantID,
		Scope:     addr.GetScope(),
		ProjectID: addr.GetProjectId(),
		Set:       addr.GetSet(),
		Name:      addr.GetName(),
	}, nil
}

// ListPrompts implements pb.PromptServiceServer.
func (s *PromptServer) ListPrompts(ctx context.Context, req *pb.ListPromptsRequest) (*pb.ListPromptsResponse, error) {
	svc, addr, err := s.promptTarget(ctx, &pb.PromptAddress{
		TenantId: req.GetTenantId(), Scope: req.GetScope(), ProjectId: req.GetProjectId(), Set: req.GetSet(),
	})
	if err != nil {
		return nil, err
	}
	got, err := svc.List(ctx, &service.ListPromptsRequest{
		TenantID: addr.TenantID, Scope: addr.Scope, ProjectID: addr.ProjectID, Set: addr.Set,
	})
	if err != nil {
		return nil, convertPromptError(err)
	}
	return promptListToProto(got), nil
}

// GetPrompt implements pb.PromptServiceServer.
func (s *PromptServer) GetPrompt(ctx context.Context, req *pb.GetPromptRequest) (*pb.PromptRecord, error) {
	svc, addr, err := s.promptTarget(ctx, req.GetAddress())
	if err != nil {
		return nil, err
	}
	got, err := svc.Get(ctx, &service.GetPromptRequest{PromptAddress: addr, Version: req.GetVersion()})
	if err != nil {
		return nil, convertPromptError(err)
	}
	return promptRecordToProto(got), nil
}

// GetEffectivePrompt implements pb.PromptServiceServer.
func (s *PromptServer) GetEffectivePrompt(ctx context.Context, req *pb.PromptAddress) (*pb.EffectivePrompt, error) {
	svc, addr, err := s.promptTarget(ctx, req)
	if err != nil {
		return nil, err
	}
	got, err := svc.Effective(ctx, &addr)
	if err != nil {
		return nil, convertPromptError(err)
	}
	return effectivePromptToProto(got), nil
}

// DiffPrompt implements pb.PromptServiceServer.
func (s *PromptServer) DiffPrompt(ctx context.Context, req *pb.PromptAddress) (*pb.PromptDiff, error) {
	svc, addr, err := s.promptTarget(ctx, req)
	if err != nil {
		return nil, err
	}
	got, err := svc.Diff(ctx, &addr)
	if err != nil {
		return nil, convertPromptError(err)
	}
	return promptDiffToProto(got), nil
}

// GetPromptHistory implements pb.PromptServiceServer.
func (s *PromptServer) GetPromptHistory(ctx context.Context, req *pb.PromptAddress) (*pb.PromptHistory, error) {
	svc, addr, err := s.promptTarget(ctx, req)
	if err != nil {
		return nil, err
	}
	got, err := svc.History(ctx, &addr)
	if err != nil {
		return nil, convertPromptError(err)
	}
	return promptHistoryToProto(got), nil
}

// PutPrompt implements pb.PromptServiceServer.
func (s *PromptServer) PutPrompt(ctx context.Context, req *pb.PutPromptRequest) (*pb.PromptRecord, error) {
	svc, addr, err := s.promptTarget(ctx, req.GetAddress())
	if err != nil {
		return nil, err
	}
	got, err := svc.Put(ctx, &service.PutPromptRequest{
		PromptAddress: addr, Content: req.GetContent(), Note: req.GetNote(), ExpectedVersion: req.GetExpectedVersion(),
	})
	if err != nil {
		return nil, convertPromptError(err)
	}
	return promptRecordToProto(got), nil
}

// RollbackPrompt implements pb.PromptServiceServer.
func (s *PromptServer) RollbackPrompt(ctx context.Context, req *pb.RollbackPromptRequest) (*pb.PromptRecord, error) {
	svc, addr, err := s.promptTarget(ctx, req.GetAddress())
	if err != nil {
		return nil, err
	}
	got, err := svc.Rollback(ctx, &service.RollbackPromptRequest{
		PromptAddress: addr, ToVersion: req.GetToVersion(), Note: req.GetNote(), ExpectedVersion: req.GetExpectedVersion(),
	})
	if err != nil {
		return nil, convertPromptError(err)
	}
	return promptRecordToProto(got), nil
}

// ResetPrompt implements pb.PromptServiceServer.
func (s *PromptServer) ResetPrompt(ctx context.Context, req *pb.ResetPromptRequest) (*pb.PromptRecord, error) {
	svc, addr, err := s.promptTarget(ctx, req.GetAddress())
	if err != nil {
		return nil, err
	}
	got, err := svc.Reset(ctx, &service.ResetPromptRequest{
		PromptAddress: addr, Note: req.GetNote(), ExpectedVersion: req.GetExpectedVersion(),
	})
	if err != nil {
		return nil, convertPromptError(err)
	}
	return promptRecordToProto(got), nil
}

func promptRecordToProto(v *service.PromptView) *pb.PromptRecord {
	return &pb.PromptRecord{
		TenantId:      v.TenantID,
		ProjectId:     v.ProjectID,
		Scope:         string(v.Scope),
		Set:           string(v.Set),
		Name:          v.Name,
		Version:       v.Version,
		ContentHash:   v.ContentHash,
		Content:       v.Content,
		FollowParent:  v.FollowParent,
		UpdatedAtUnix: v.UpdatedAt.Unix(),
	}
}

func promptVersionRefToProto(ref prompts.VersionRef) *pb.PromptVersionRef {
	return &pb.PromptVersionRef{
		Set:     string(ref.Set),
		Name:    ref.Name,
		Scope:   string(ref.Scope),
		ScopeId: ref.ScopeID,
		Version: ref.Version,
		Hash:    ref.Hash,
	}
}

func promptListToProto(list *service.ListPromptsResponse) *pb.ListPromptsResponse {
	out := &pb.ListPromptsResponse{
		Set:     string(list.Set),
		Scope:   string(list.Scope),
		Prompts: make([]*pb.PromptListEntry, len(list.Prompts)),
	}
	for i, entry := range list.Prompts {
		out.Prompts[i] = &pb.PromptListEntry{
			Name:         entry.Name,
			Description:  entry.Description,
			Overridden:   entry.Overridden,
			Version:      entry.Version,
			FollowParent: entry.FollowParent,
			Effective:    promptVersionRefToProto(entry.Effective),
		}
	}
	return out
}

func effectivePromptToProto(e *service.EffectivePrompt) *pb.EffectivePrompt {
	out := &pb.EffectivePrompt{
		Set:     string(e.Set),
		Name:    e.Name,
		Scope:   string(e.Scope),
		Content: e.Content,
		Origin:  promptVersionRefToProto(e.Origin),
		Chain:   make([]*pb.PromptLayer, len(e.Chain)),
	}
	for i, layer := range e.Chain {
		out.Chain[i] = &pb.PromptLayer{
			Scope:        string(layer.Scope),
			ScopeId:      layer.ScopeID,
			Present:      layer.Present,
			Version:      layer.Version,
			Hash:         layer.Hash,
			FollowParent: layer.FollowParent,
			Answered:     layer.Answered,
		}
	}
	return out
}

func promptDiffToProto(d *service.PromptDiff) *pb.PromptDiff {
	out := &pb.PromptDiff{
		Set:          string(d.Set),
		Name:         d.Name,
		Scope:        string(d.Scope),
		Origin:       promptVersionRefToProto(d.Origin),
		ParentOrigin: promptVersionRefToProto(d.ParentOrigin),
		Same:         d.Same,
		Lines:        make([]*pb.PromptDiffLine, len(d.Lines)),
	}
	for i, line := range d.Lines {
		out.Lines[i] = &pb.PromptDiffLine{Op: string(line.Op), Text: line.Text}
	}
	return out
}

func promptHistoryToProto(h *service.PromptHistory) *pb.PromptHistory {
	out := &pb.PromptHistory{
		Set:       string(h.Set),
		Name:      h.Name,
		Scope:     string(h.Scope),
		Revisions: make([]*pb.PromptRevisionSummary, len(h.Revisions)),
	}
	for i, rev := range h.Revisions {
		out.Revisions[i] = &pb.PromptRevisionSummary{
			Version:       rev.Version,
			ContentHash:   rev.ContentHash,
			Author:        rev.Author,
			Note:          rev.Note,
			FollowParent:  rev.FollowParent,
			CreatedAtUnix: rev.CreatedAt.Unix(),
		}
	}
	return out
}
