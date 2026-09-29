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

	"google.golang.org/grpc"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// =============================================================================
// The attention inbox over gRPC (and so over Unix)
// =============================================================================
//
// The SDK reaches the inbox through the Orchestrator client, as REST serves it
// under /api/v1/orchestrator/attention; on the wire the two verbs are
// SDLCService RPCs, because the SDLC module's AttentionService serves them.

// handleOrchestratorAttentionList invokes SDLCService/ListAttention.
func handleOrchestratorAttentionList(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	const op = "grpc.orchestrator.AttentionList"
	sdkReq, ok := req.(*orchestrator.AttentionListRequest)
	if !ok {
		return nil, errors.NewInvalidInput(op, "invalid request type")
	}
	paging := pageFieldsFromParams(sdkReq.Params)
	resp, err := pb.NewSDLCServiceClient(conn).ListAttention(ctx, &pb.ListAttentionRequest{
		TenantId: sdkReq.TenantID, ProjectId: sdkReq.ProjectID, Kind: sdkReq.Kind,
		Limit: paging.limit, Offset: paging.offset, Sort: paging.sort, Q: paging.q,
	})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	items := make([]*orchestrator.AttentionItem, 0, len(resp.GetItems()))
	for _, item := range resp.GetItems() {
		items = append(items, attentionItemFromProto(item))
	}
	envelope := pageEnvelopeFromProto(resp.GetTotal(), resp.GetLimit(), resp.GetOffset(), resp.GetNextOffset())
	envelope.Facets = facetsFromProto(resp.GetFacets())
	return &orchestrator.AttentionListResponse{Items: items, Count: len(items), Envelope: envelope}, nil
}

// handleOrchestratorAttentionResolve invokes SDLCService/ResolveAttention.
func handleOrchestratorAttentionResolve(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	const op = "grpc.orchestrator.AttentionResolve"
	sdkReq, ok := req.(*orchestrator.AttentionResolveRequest)
	if !ok {
		return nil, errors.NewInvalidInput(op, "invalid request type")
	}
	resp, err := pb.NewSDLCServiceClient(conn).ResolveAttention(ctx, &pb.ResolveAttentionRequest{
		TenantId: sdkReq.TenantID, TaskId: sdkReq.TaskID, Action: sdkReq.Action, Answer: sdkReq.Answer,
	})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	item := attentionItemFromProto(resp.GetItem())
	if item == nil {
		return nil, errors.NewInternal(op, "the server answered no item")
	}
	return item, nil
}

// attentionItemFromProto reads one inbox item.
func attentionItemFromProto(p *pb.AttentionItem) *orchestrator.AttentionItem {
	if p == nil {
		return nil
	}
	return &orchestrator.AttentionItem{
		ID: p.GetId(), Kind: p.GetKind(), ProjectID: p.GetProjectId(), ProjectName: p.GetProjectName(),
		Subject: p.GetSubject(), IssueRef: p.GetIssueRef(), IssueURL: p.GetIssueUrl(),
		BlockKind: p.GetBlockKind(), Condition: p.GetCondition(),
		Question: p.GetQuestion(), Recommendation: p.GetRecommendation(), Truncated: p.GetTruncated(),
		RulingPosted: p.GetRulingPosted(), Message: p.GetMessage(), Severity: p.GetSeverity(),
		CreatedAt: p.GetCreatedAt(), StatusChangedAt: p.GetStatusChangedAt(), Attempts: int(p.GetAttempts()),
		Actions: append([]string(nil), p.GetActions()...),
	}
}

// facetsFromProto reads a page's facet counts; none stays nil.
func facetsFromProto(facets map[string]*pb.FacetCounts) map[string]map[string]int {
	if len(facets) == 0 {
		return nil
	}
	out := make(map[string]map[string]int, len(facets))
	for field, counts := range facets {
		values := make(map[string]int, len(counts.GetCounts()))
		for value, n := range counts.GetCounts() {
			values[value] = int(n)
		}
		out[field] = values
	}
	return out
}
