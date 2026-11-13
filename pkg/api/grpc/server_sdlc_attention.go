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

package grpc

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/sdlc"
)

// =============================================================================
// The attention inbox on gRPC (and so on Unix)
// =============================================================================
//
// The same two verbs REST serves at /api/v1/orchestrator/attention, served by
// the same sdlc.AttentionService, under the same permissions. Before these
// RPCs the inbox existed on REST, QUIC, MCP and the embedded transport only,
// so a Unix-socket operator could neither see nor clear it.

// ListAttention implements pb.SDLCServiceServer.
func (s *SDLCServer) ListAttention(ctx context.Context, req *pb.ListAttentionRequest) (*pb.ListAttentionResponse, error) {
	attention, ctx, tenantID, err := s.attentionRequest(ctx, req.GetTenantId())
	if err != nil {
		return nil, err
	}
	page, err := pageFromRPC(req.GetLimit(), req.GetOffset(), req.GetSort(), req.GetQ())
	if err != nil {
		return nil, err
	}
	result, err := attention.List(ctx, tenantID, sdlc.AttentionFilter{
		ProjectID: req.GetProjectId(), Kind: sdlc.AttentionKind(req.GetKind()),
	}, page)
	if err != nil {
		return nil, convertListError(err, convertSDLCError)
	}
	items := make([]*pb.AttentionItem, 0, len(result.Items))
	for _, item := range result.Items {
		items = append(items, attentionItemToProto(item))
	}
	response := &pb.ListAttentionResponse{
		Items: items, Total: pageInt32(result.Total), Limit: pageInt32(result.Limit),
		Offset: pageInt32(result.Offset), Facets: facetsToProto(result.Facets),
	}
	if result.HasNext {
		response.NextOffset = pageInt32(result.NextOffset)
	}
	return response, nil
}

// ResolveAttention implements pb.SDLCServiceServer. The actor recorded on the
// ruling is the verified principal, never a request field.
func (s *SDLCServer) ResolveAttention(ctx context.Context, req *pb.ResolveAttentionRequest) (*pb.ResolveAttentionResponse, error) {
	if req.GetTaskId() == "" {
		return nil, status.Error(codes.InvalidArgument, "task_id is required")
	}
	attention, ctx, tenantID, err := s.attentionRequest(ctx, req.GetTenantId())
	if err != nil {
		return nil, err
	}
	item, err := attention.Resolve(ctx, tenantID, req.GetTaskId(), sdlc.AttentionResolution{
		Action: sdlc.AttentionAction(req.GetAction()), Answer: req.GetAnswer(), ActorID: auth.PrincipalFromContext(ctx),
	})
	if err != nil {
		return nil, convertSDLCError(err)
	}
	return &pb.ResolveAttentionResponse{Item: attentionItemToProto(item)}, nil
}

// attentionRequest resolves the inbox, the tenant, and a context stamped with
// that tenant for the engine's task reads.
func (s *SDLCServer) attentionRequest(ctx context.Context, requestedTenant string) (*sdlc.AttentionService, context.Context, string, error) {
	svc, err := s.sdlcService()
	if err != nil {
		return nil, ctx, "", err
	}
	attention := svc.Attention()
	if attention == nil {
		return nil, ctx, "", status.Error(codes.Unavailable, "the attention inbox is not available on this node")
	}
	tenantID, err := sdlcTenant(ctx, requestedTenant)
	if err != nil {
		return nil, ctx, "", err
	}
	return attention, orchestration.ContextWithTenant(ctx, tenantID), tenantID, nil
}

// attentionItemToProto renders one inbox item.
func attentionItemToProto(item *sdlc.AttentionItem) *pb.AttentionItem {
	if item == nil {
		return nil
	}
	actions := make([]string, 0, len(item.Actions))
	for _, action := range item.Actions {
		actions = append(actions, string(action))
	}
	return &pb.AttentionItem{
		Id: item.ID, Kind: string(item.Kind), ProjectId: item.ProjectID, ProjectName: item.ProjectName,
		Subject: item.Subject, IssueRef: item.IssueRef, IssueUrl: item.IssueURL,
		BlockKind: string(item.BlockKind), Condition: string(item.Condition),
		Question: item.Question, Recommendation: item.Recommendation, Truncated: item.Truncated,
		RulingPosted: item.RulingPosted, Message: item.Message, Severity: item.Severity,
		CreatedAt: item.CreatedAt, StatusChangedAt: item.StatusChangedAt, Attempts: pageInt32(item.Attempts),
		Actions: actions,
	}
}

// facetsToProto renders a page's facet counts; nil stays nil, as a response
// the server did not page carries none.
func facetsToProto(facets orchestration.Facets) map[string]*pb.FacetCounts {
	if facets == nil {
		return nil
	}
	out := make(map[string]*pb.FacetCounts, len(facets))
	for field, values := range facets {
		counts := make(map[string]int32, len(values))
		for value, n := range values {
			counts[value] = pageInt32(n)
		}
		out[field] = &pb.FacetCounts{Counts: counts}
	}
	return out
}
