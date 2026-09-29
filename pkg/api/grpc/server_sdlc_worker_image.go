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
	stderrors "errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc/workerimage"
)

// GetWorkerImageStatus implements pb.SDLCServiceServer. A host verb: no
// tenant is derived, and the cross-tenant worker_images:read gates it.
func (s *SDLCServer) GetWorkerImageStatus(ctx context.Context, _ *pb.GetWorkerImageStatusRequest) (*pb.GetWorkerImageStatusResponse, error) {
	svc, err := s.sdlcService()
	if err != nil {
		return nil, err
	}
	st, err := svc.WorkerImageStatus(ctx)
	if err != nil {
		return nil, convertWorkerImageError(err)
	}
	return &pb.GetWorkerImageStatusResponse{Status: workerImageStatusToProto(st)}, nil
}

// RefreshWorkerImage implements pb.SDLCServiceServer.
func (s *SDLCServer) RefreshWorkerImage(ctx context.Context, _ *pb.RefreshWorkerImageRequest) (*pb.RefreshWorkerImageResponse, error) {
	svc, err := s.sdlcService()
	if err != nil {
		return nil, err
	}
	st, err := svc.RefreshWorkerImage(ctx)
	if err != nil {
		return nil, convertWorkerImageError(err)
	}
	return &pb.RefreshWorkerImageResponse{Status: workerImageStatusToProto(st)}, nil
}

// convertWorkerImageError maps the refresh's refusals onto codes: no loop on
// this node is Unavailable, a pass already running or queued is Aborted (try
// again later), anything else is Internal.
func convertWorkerImageError(err error) error {
	switch {
	case stderrors.Is(err, workerimage.ErrNotRunning):
		return status.Error(codes.Unavailable, err.Error())
	case stderrors.Is(err, workerimage.ErrBusy):
		return status.Error(codes.Aborted, err.Error())
	default:
		return status.Error(codes.Internal, err.Error())
	}
}

func workerImageStatusToProto(st workerimage.Status) *pb.WorkerImageStatus {
	out := &pb.WorkerImageStatus{
		Serving:          st.Serving,
		Running:          st.Running,
		RunningSinceUnix: unixOrZero(st.RunningSince),
		Queued:           st.Queued,
	}
	if st.Last != nil {
		out.Last = workerImageReportToProto(st.Last)
	}
	return out
}

func workerImageReportToProto(r *workerimage.Report) *pb.WorkerImageReport {
	return &pb.WorkerImageReport{
		Trigger:        string(r.Trigger),
		Outcome:        string(r.Outcome),
		StartedAtUnix:  unixOrZero(r.StartedAt),
		FinishedAtUnix: unixOrZero(r.FinishedAt),
		Latest:         &pb.WorkerImageAgents{ClaudeCode: r.Latest.ClaudeCode, Codex: r.Latest.Codex},
		Stale:          append([]string(nil), r.Stale...),
		Skipped:        append([]string(nil), r.Skipped...),
		Candidate:      r.Candidate,
		ImageId:        r.ImageID,
		Reference:      r.Reference,
		Digest:         r.Digest,
		Probes:         workerImageChecksToProto(r.Probes),
		Smokes:         workerImageChecksToProto(r.Smokes),
		Promotions:     workerImagePromotionsToProto(r.Promotions),
		Error:          r.Error,
		Bound:          workerImageBoundToProto(r.Bound),
	}
}

func workerImageBoundToProto(bound []workerimage.BoundImage) []*pb.WorkerImageBound {
	out := make([]*pb.WorkerImageBound, 0, len(bound))
	for _, b := range bound {
		out = append(out, &pb.WorkerImageBound{
			Image:  b.Image,
			Agents: &pb.WorkerImageAgents{ClaudeCode: b.Agents.ClaudeCode, Codex: b.Agents.Codex},
			Uses:   int32(b.Uses), // #nosec G115 -- a count of binding roles, bounded far below int32.
			Stale:  b.Stale,
		})
	}
	return out
}

func workerImageChecksToProto(checks []workerimage.CheckResult) []*pb.WorkerImageCheck {
	out := make([]*pb.WorkerImageCheck, 0, len(checks))
	for _, c := range checks {
		out = append(out, &pb.WorkerImageCheck{Name: c.Name, Passed: c.Passed, Detail: c.Detail})
	}
	return out
}

func workerImagePromotionsToProto(promotions []workerimage.Promotion) []*pb.WorkerImagePromotion {
	out := make([]*pb.WorkerImagePromotion, 0, len(promotions))
	for _, p := range promotions {
		out = append(out, &pb.WorkerImagePromotion{
			TenantId: p.TenantID, AgentId: p.AgentID, From: append([]string(nil), p.From...), To: p.To, Error: p.Error,
		})
	}
	return out
}
