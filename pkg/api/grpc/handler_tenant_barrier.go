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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Per-tenant barrier gRPC handlers.
//
// TenantBarrierGRPCServer is the gRPC wrapper around the TenantBarrierServicer
// facade. Every RPC delegates to the facade which, in turn, forwards to the
// embedded go-qrdb node's TenantClient — quicbaas owns no barrier logic.
//
// The handlers mirror pkg/api/rest/handlers_tenant_barrier.go one-for-one in
// input validation and response shape, so the two transports are
// indistinguishable from the caller's perspective.
//
// Authorization (caller must hold tenant_barrier:{init,unseal,seal,status} for
// the target tenant; the system tenant is denied) is enforced by the gRPC authz
// interceptor before any RPC body is executed. qrdb re-applies ScopeTenantOnly
// beneath it (defense in depth). Share material is proxied straight through and
// never logged.
// =============================================================================

// TenantBarrierGRPCServer wraps the TenantBarrierServicer facade for the
// gRPC TenantBarrier service.
type TenantBarrierGRPCServer struct {
	pb.UnimplementedTenantBarrierServer
	tenantBarrierSvc service.TenantBarrierServicer
	logger           *logger.Logger
}

// NewTenantBarrierGRPCServer constructs a TenantBarrierGRPCServer over the
// supplied facade. A nil facade is permitted — every RPC short-circuits with
// codes.Unavailable so that handlers report "not yet wired" cleanly during
// early-boot windows.
func NewTenantBarrierGRPCServer(svc service.TenantBarrierServicer, log *logger.Logger) *TenantBarrierGRPCServer {
	srv := &TenantBarrierGRPCServer{tenantBarrierSvc: svc}
	if log != nil {
		srv.logger = log.WithField("component", "tenant-barrier-grpc-server")
	}
	return srv
}

// RegisterTenantBarrierGRPCServer registers the TenantBarrier service
// implementation on the supplied grpc.Server.
func RegisterTenantBarrierGRPCServer(server *grpc.Server, svc service.TenantBarrierServicer, log *logger.Logger) {
	pb.RegisterTenantBarrierServer(server, NewTenantBarrierGRPCServer(svc, log))
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// tenantBarrierServiceUnavailableError is the canonical "service not wired"
// status returned by every TenantBarrier RPC when the facade is nil.
func tenantBarrierServiceUnavailableError() error {
	return status.Error(codes.Unavailable, "tenant barrier service not available")
}

// requireTenantBarrierService returns the canonical unavailable status when the
// facade has not yet been wired. Returns nil when the service is present.
func (s *TenantBarrierGRPCServer) requireTenantBarrierService() error {
	if s.tenantBarrierSvc == nil {
		return tenantBarrierServiceUnavailableError()
	}
	return nil
}

// mapTenantBarrierError translates service-layer errors into gRPC status errors.
func mapTenantBarrierError(err error) error {
	if err == nil {
		return nil
	}
	return ToGRPCError(err)
}

// requireTenantID returns an InvalidArgument error when tenantID is empty.
func requireTenantID(tenantID string) error {
	if tenantID == "" {
		return status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	return nil
}

// ---------------------------------------------------------------------------
// RPC handlers
// ---------------------------------------------------------------------------

// Init initializes the per-tenant barrier with the selected strategy.
func (s *TenantBarrierGRPCServer) Init(ctx context.Context, req *pb.TenantBarrierInitRequest) (*pb.TenantBarrierInitResponse, error) {
	if err := s.requireTenantBarrierService(); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if err := requireTenantID(req.GetTenantId()); err != nil {
		return nil, err
	}

	result, err := s.tenantBarrierSvc.TenantBarrierInit(
		ctx,
		req.GetTenantId(),
		req.GetPassphrase(),
		int(req.GetThreshold()),
		int(req.GetShares()),
		req.GetStrategy(),
	)
	if err != nil {
		return nil, mapTenantBarrierError(err)
	}
	if result == nil {
		return &pb.TenantBarrierInitResponse{TenantId: req.GetTenantId()}, nil
	}
	return &pb.TenantBarrierInitResponse{
		TenantId:    result.TenantID,
		Strategy:    result.Strategy,
		Shares:      result.Shares,
		Threshold:   int32(result.Threshold),   //nolint:gosec // small count
		TotalShares: int32(result.TotalShares), //nolint:gosec // small count
	}, nil
}

// Unseal unseals the per-tenant barrier with a single key (passphrase strategy).
func (s *TenantBarrierGRPCServer) Unseal(ctx context.Context, req *pb.TenantBarrierUnsealRequest) (*emptypb.Empty, error) {
	if err := s.requireTenantBarrierService(); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if err := requireTenantID(req.GetTenantId()); err != nil {
		return nil, err
	}

	if err := s.tenantBarrierSvc.TenantBarrierUnseal(ctx, req.GetTenantId(), req.GetKey()); err != nil {
		return nil, mapTenantBarrierError(err)
	}
	return &emptypb.Empty{}, nil
}

// UnsealShare submits a single Shamir share toward the per-tenant quorum.
func (s *TenantBarrierGRPCServer) UnsealShare(ctx context.Context, req *pb.TenantBarrierUnsealShareRequest) (*pb.TenantBarrierUnsealProgress, error) {
	if err := s.requireTenantBarrierService(); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if err := requireTenantID(req.GetTenantId()); err != nil {
		return nil, err
	}
	if req.GetShare() == "" {
		return nil, status.Error(codes.InvalidArgument, "share is required")
	}

	progress, err := s.tenantBarrierSvc.TenantBarrierUnsealShare(ctx, req.GetTenantId(), req.GetShare())
	if err != nil {
		return nil, mapTenantBarrierError(err)
	}
	if progress == nil {
		return &pb.TenantBarrierUnsealProgress{TenantId: req.GetTenantId()}, nil
	}
	return &pb.TenantBarrierUnsealProgress{
		TenantId:  progress.TenantID,
		Required:  int32(progress.Required),  //nolint:gosec // small count
		Submitted: int32(progress.Submitted), //nolint:gosec // small count
		Complete:  progress.Complete,
		Sealed:    progress.Sealed,
	}, nil
}

// Seal seals the per-tenant barrier.
func (s *TenantBarrierGRPCServer) Seal(ctx context.Context, req *pb.TenantBarrierSealRequest) (*emptypb.Empty, error) {
	if err := s.requireTenantBarrierService(); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if err := requireTenantID(req.GetTenantId()); err != nil {
		return nil, err
	}

	if err := s.tenantBarrierSvc.TenantBarrierSeal(ctx, req.GetTenantId()); err != nil {
		return nil, mapTenantBarrierError(err)
	}
	return &emptypb.Empty{}, nil
}

// Status reports the per-tenant barrier state.
func (s *TenantBarrierGRPCServer) Status(ctx context.Context, req *pb.TenantBarrierStatusRequest) (*pb.TenantBarrierStatusResponse, error) {
	if err := s.requireTenantBarrierService(); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if err := requireTenantID(req.GetTenantId()); err != nil {
		return nil, err
	}

	info, err := s.tenantBarrierSvc.TenantBarrierStatus(ctx, req.GetTenantId())
	if err != nil {
		return nil, mapTenantBarrierError(err)
	}
	if info == nil {
		return &pb.TenantBarrierStatusResponse{TenantId: req.GetTenantId()}, nil
	}
	return &pb.TenantBarrierStatusResponse{
		TenantId:        info.TenantID,
		Sealed:          info.Sealed,
		Initialized:     info.Initialized,
		Strategy:        info.Strategy,
		HardwareBacked:  info.HardwareBacked,
		ShamirThreshold: int32(info.ShamirThreshold), //nolint:gosec // small count
		ShamirTotal:     int32(info.ShamirTotal),     //nolint:gosec // small count
	}, nil
}
