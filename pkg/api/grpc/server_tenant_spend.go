// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
//  1. GNU Affero General Public License v3.0 (AGPL-3.0)
//     See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
//  2. Commercial License
//     Contact licensing@automatethethings.com for commercial licensing options.
package grpc

import (
	"context"
	stderrors "errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// OrchestratorService.GetTenantSpendPolicy / SetTenantSpendPolicy
// (go-quicbaas#1058, ruling F5)
// =============================================================================

// GetTenantSpendPolicy returns a tenant's own limits and its usage against
// each window.
func (s *OrchestratorServer) GetTenantSpendPolicy(ctx context.Context, req *pb.GetTenantSpendPolicyRequest) (*pb.TenantSpendPolicyResponse, error) {
	if req == nil || req.GetTenantId() == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	provider, ok := s.agentSvc.(service.TenantSpendPolicySource)
	if !ok {
		return nil, status.Error(codes.Unavailable, "tenant spend policy not available")
	}
	report, err := service.GetTenantSpendPolicy(ctx, provider, req.GetTenantId())
	if err != nil {
		return nil, tenantSpendStatusError(err)
	}
	return tenantSpendReportToProto(report), nil
}

// SetTenantSpendPolicy replaces a tenant's own limits.
func (s *OrchestratorServer) SetTenantSpendPolicy(ctx context.Context, req *pb.SetTenantSpendPolicyRequest) (*pb.TenantSpendPolicyResponse, error) {
	if req == nil || req.GetTenantId() == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	provider, ok := s.agentSvc.(service.TenantSpendPolicySource)
	if !ok {
		return nil, status.Error(codes.Unavailable, "tenant spend policy not available")
	}
	report, err := service.SetTenantSpendPolicy(ctx, provider, req.GetTenantId(), spendLimitsFromProto(req.GetLimits()))
	if err != nil {
		return nil, tenantSpendStatusError(err)
	}
	return tenantSpendReportToProto(report), nil
}

// tenantSpendStatusError maps a tenant spend-policy failure to a status.
func tenantSpendStatusError(err error) error {
	switch {
	case stderrors.Is(err, orchestration.ErrQuotaInvalid), stderrors.Is(err, orchestration.ErrTenantSpendPolicyInvalid):
		return status.Error(codes.InvalidArgument, err.Error())
	case stderrors.Is(err, orchestration.ErrAccountTenantScope):
		return status.Error(codes.PermissionDenied, err.Error())
	case stderrors.Is(err, orchestration.ErrTenantSpendPolicyStore):
		return status.Error(codes.Unavailable, err.Error())
	default:
		return status.Errorf(codes.Internal, "tenant spend policy: %v", err)
	}
}

// tenantSpendReportToProto renders a tenant's policy and window usage.
func tenantSpendReportToProto(report *orchestration.TenantSpendPolicyReport) *pb.TenantSpendPolicyResponse {
	out := &pb.TenantSpendPolicyResponse{
		TenantId: report.TenantID,
		Limits:   spendLimitsToProto(report.Limits),
	}
	if !report.UpdatedAt.IsZero() {
		out.UpdatedAt = timestamppb.New(report.UpdatedAt)
	}
	for _, window := range report.Windows {
		out.Windows = append(out.Windows, spendWindowStatusToProto(window))
	}
	return out
}
