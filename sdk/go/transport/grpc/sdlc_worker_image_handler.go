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
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/sdlc"
)

// handleSDLCWorkerImageStatus invokes GetWorkerImageStatus. No tenant: the
// refresh is a host verb.
func handleSDLCWorkerImageStatus(ctx context.Context, conn *grpc.ClientConn, _ interface{}) (interface{}, error) {
	const op = "grpc.sdlc.WorkerImageStatus"
	resp, err := pb.NewSDLCServiceClient(conn).GetWorkerImageStatus(ctx, &pb.GetWorkerImageStatusRequest{})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	return sdlcWorkerImageStatusFromProto(resp.GetStatus()), nil
}

// handleSDLCRefreshWorkerImage invokes RefreshWorkerImage.
func handleSDLCRefreshWorkerImage(ctx context.Context, conn *grpc.ClientConn, _ interface{}) (interface{}, error) {
	const op = "grpc.sdlc.RefreshWorkerImage"
	resp, err := pb.NewSDLCServiceClient(conn).RefreshWorkerImage(ctx, &pb.RefreshWorkerImageRequest{})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	return sdlcWorkerImageStatusFromProto(resp.GetStatus()), nil
}

func sdlcWorkerImageStatusFromProto(p *pb.WorkerImageStatus) *sdlc.WorkerImageStatus {
	if p == nil {
		return &sdlc.WorkerImageStatus{}
	}
	out := &sdlc.WorkerImageStatus{
		Serving:      p.GetServing(),
		Running:      p.GetRunning(),
		RunningSince: timeFromUnix(p.GetRunningSinceUnix()),
		Queued:       p.GetQueued(),
	}
	if last := p.GetLast(); last != nil {
		out.Last = sdlcWorkerImageReportFromProto(last)
	}
	return out
}

func sdlcWorkerImageReportFromProto(p *pb.WorkerImageReport) *sdlc.WorkerImageReport {
	return &sdlc.WorkerImageReport{
		Trigger:    p.GetTrigger(),
		Outcome:    p.GetOutcome(),
		StartedAt:  timeFromUnix(p.GetStartedAtUnix()),
		FinishedAt: timeFromUnix(p.GetFinishedAtUnix()),
		Latest:     sdlc.WorkerImageAgents{ClaudeCode: p.GetLatest().GetClaudeCode(), Codex: p.GetLatest().GetCodex()},
		Stale:      append([]string(nil), p.GetStale()...),
		Skipped:    append([]string(nil), p.GetSkipped()...),
		Candidate:  p.GetCandidate(),
		ImageID:    p.GetImageId(),
		Reference:  p.GetReference(),
		Digest:     p.GetDigest(),
		Probes:     sdlcWorkerImageChecksFromProto(p.GetProbes()),
		Smokes:     sdlcWorkerImageChecksFromProto(p.GetSmokes()),
		Promotions: sdlcWorkerImagePromotionsFromProto(p.GetPromotions()),
		Error:      p.GetError(),
		Bound:      sdlcWorkerImageBoundFromProto(p.GetBound()),
	}
}

func sdlcWorkerImageBoundFromProto(bound []*pb.WorkerImageBound) []sdlc.WorkerImageBound {
	out := make([]sdlc.WorkerImageBound, 0, len(bound))
	for _, b := range bound {
		out = append(out, sdlc.WorkerImageBound{
			Image:  b.GetImage(),
			Agents: sdlc.WorkerImageAgents{ClaudeCode: b.GetAgents().GetClaudeCode(), Codex: b.GetAgents().GetCodex()},
			Uses:   int(b.GetUses()),
			Stale:  b.GetStale(),
		})
	}
	return out
}

func sdlcWorkerImageChecksFromProto(checks []*pb.WorkerImageCheck) []sdlc.WorkerImageCheck {
	out := make([]sdlc.WorkerImageCheck, 0, len(checks))
	for _, c := range checks {
		out = append(out, sdlc.WorkerImageCheck{Name: c.GetName(), Passed: c.GetPassed(), Detail: c.GetDetail()})
	}
	return out
}

func sdlcWorkerImagePromotionsFromProto(promotions []*pb.WorkerImagePromotion) []sdlc.WorkerImagePromotion {
	out := make([]sdlc.WorkerImagePromotion, 0, len(promotions))
	for _, p := range promotions {
		out = append(out, sdlc.WorkerImagePromotion{
			TenantID: p.GetTenantId(), AgentID: p.GetAgentId(), From: append([]string(nil), p.GetFrom()...),
			To: p.GetTo(), Error: p.GetError(),
		})
	}
	return out
}
