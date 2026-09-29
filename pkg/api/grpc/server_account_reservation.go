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
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// callerTenant binds a pooled-account verb to the AUTHENTICATED principal's
// tenant, the rule the credential release established: the tenant is the
// caller's, never the request's. A request naming another tenant is refused
// rather than silently re-pointed, so a principal of tenant A can neither reach
// tenant B's accounts nor mistake the refusal for an empty answer.
func callerTenant(ctx context.Context, requested string) (interfaces.Subject, error) {
	subject, ok := auth.SubjectFromContext(ctx)
	if !ok || !subject.Authenticated || subject.ID == "" {
		return interfaces.Subject{}, status.Error(codes.Unauthenticated, "caller is not authenticated")
	}
	if subject.TenantID == "" {
		return interfaces.Subject{}, status.Error(codes.InvalidArgument, "tenant_id is required: the caller carries no tenant binding")
	}
	if requested != "" && requested != subject.TenantID {
		return interfaces.Subject{}, status.Error(codes.PermissionDenied, "tenant_id must be the caller's own tenant")
	}
	return subject, nil
}

// ReserveAccountSession handles the ReserveAccountSession RPC: one session
// slot on an account, claimed in the control plane's reservation table for
// the caller's own tenant.
func (s *AccountServer) ReserveAccountSession(ctx context.Context, req *pb.ReserveAccountSessionRequest) (*pb.ReserveAccountSessionResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if req.AccountId == "" {
		return nil, status.Error(codes.InvalidArgument, "account_id is required")
	}
	subject, err := callerTenant(ctx, req.TenantId)
	if err != nil {
		return nil, err
	}
	svcReq := &service.ReserveAccountSessionRequest{TenantID: subject.TenantID, AccountID: req.AccountId}
	if req.ExpectedLive != nil {
		expected := int(req.GetExpectedLive())
		svcReq.ExpectedLive = &expected
	}
	resp, err := s.accountSvc.ReserveAccountSession(ctx, svcReq)
	if err != nil {
		return nil, convertAccountError(err)
	}
	return &pb.ReserveAccountSessionResponse{
		ReservationId: resp.ReservationID,
		Timestamp:     resp.Timestamp.Unix(),
	}, nil
}

// ReleaseAccountSession handles the ReleaseAccountSession RPC, for the
// caller's own tenant.
func (s *AccountServer) ReleaseAccountSession(ctx context.Context, req *pb.ReleaseAccountSessionRequest) (*pb.ReleaseAccountSessionResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if req.AccountId == "" || req.ReservationId == "" {
		return nil, status.Error(codes.InvalidArgument, "account_id and reservation_id are required")
	}
	subject, err := callerTenant(ctx, req.TenantId)
	if err != nil {
		return nil, err
	}
	resp, err := s.accountSvc.ReleaseAccountSession(ctx, &service.ReleaseAccountSessionRequest{
		TenantID: subject.TenantID, AccountID: req.AccountId, ReservationID: req.ReservationId,
	})
	if err != nil {
		return nil, convertAccountError(err)
	}
	return &pb.ReleaseAccountSessionResponse{
		ReservationId: resp.ReservationID,
		Timestamp:     resp.Timestamp.Unix(),
	}, nil
}
