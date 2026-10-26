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
	"errors"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	secure "github.com/jeremyhahn/go-xkms/sdk/go/secure"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Barrier gRPC handlers.
//
// BarrierServer is the gRPC wrapper around the QuicBaaS BarrierServicer
// facade. The facade itself delegates to the embedded go-qrdb SDK
// BarrierClient — QuicBaaS does not own the barrier, it merely re-publishes
// the surface so that gRPC clients dialling the QuicBaaS endpoint can drive
// barrier operations against the composed stack.
//
// The handlers mirror pkg/api/rest/handlers_barrier.go one-for-one, both
// in input validation and response shape, so the two transports are
// indistinguishable from the caller's perspective.
// =============================================================================

// BarrierServer wraps the BarrierServicer facade for the gRPC Barrier
// service.
type BarrierServer struct {
	pb.UnimplementedBarrierServer
	barrierSvc service.BarrierServicer
	logger     *logger.Logger
}

// NewBarrierServer constructs a BarrierServer over the supplied facade.
// A nil facade is permitted — every RPC short-circuits with
// codes.Unavailable so that handlers report "not yet wired" cleanly during
// early-boot windows.
func NewBarrierServer(barrierSvc service.BarrierServicer, log *logger.Logger) *BarrierServer {
	srv := &BarrierServer{barrierSvc: barrierSvc}
	if log != nil {
		srv.logger = log.WithField("component", "barrier-grpc-server")
	}
	return srv
}

// RegisterBarrierServer registers the Barrier service implementation on
// the supplied grpc.Server.
func RegisterBarrierServer(server *grpc.Server, svc service.BarrierServicer, log *logger.Logger) {
	pb.RegisterBarrierServer(server, NewBarrierServer(svc, log))
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// barrierServiceUnavailableError is the canonical "service not wired"
// status returned by every Barrier RPC when the facade is nil.
func barrierServiceUnavailableError() error {
	return status.Error(codes.Unavailable, "barrier service not available")
}

// requireBarrierService returns the canonical unavailable status when the
// facade has not yet been wired. Returns nil when the service is present.
func (s *BarrierServer) requireBarrierService() error {
	if s.barrierSvc == nil {
		return barrierServiceUnavailableError()
	}
	return nil
}

// mapBarrierError translates service-layer errors into gRPC status errors.
// The BarrierServiceMissingError is surfaced as codes.Unavailable so it is
// distinguishable from genuine failures; everything else flows through the
// shared ToGRPCError mapper which preserves QBError codes.
func mapBarrierError(err error) error {
	if err == nil {
		return nil
	}
	var missing *service.BarrierServiceMissingError
	if errors.As(err, &missing) {
		return barrierServiceUnavailableError()
	}
	return ToGRPCError(err)
}

// barrierTimeFormat is the ONE rendering every barrier timestamp uses on
// every transport. It is RFC3339Nano, not RFC3339, because go-qrdb's own
// qrdb.v1 gRPC barrier surface — which quicbaas's unix and gRPC transports
// reach through the platform-shard servicer adapter — already renders
// InitializedAt at nanosecond precision. Rendering the same instant at
// second precision here made `barrier status` answer
// "2026-08-25T04:40:05Z" over REST/MCP and "2026-08-25T04:40:05.727835519Z"
// over unix/gRPC for one barrier: not a contradiction, but not
// byte-comparable either, and go-quicbaas#405 requires an operator
// diffing two transports to get the same answer.
const barrierTimeFormat = time.RFC3339Nano

// formatTime renders a time as RFC3339Nano (UTC) or returns the empty string
// when the value is zero. The REST handler uses the same encoding.
func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(barrierTimeFormat)
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// BarrierInitialize initialises the storage barrier with a single
// passphrase. Returns FailedPrecondition when the barrier is already
// initialised, propagated from the facade.
func (s *BarrierServer) BarrierInitialize(ctx context.Context, req *pb.BarrierInitializeRequest) (*emptypb.Empty, error) {
	secret := req.GetSecret()
	defer secure.Wipe(secret)
	if err := s.requireBarrierService(); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if err := s.barrierSvc.BarrierInitialize(ctx, secret); err != nil {
		return nil, mapBarrierError(err)
	}
	return &emptypb.Empty{}, nil
}

// BarrierUnseal unseals the barrier with a passphrase.
func (s *BarrierServer) BarrierUnseal(ctx context.Context, req *pb.BarrierUnsealRequest) (*emptypb.Empty, error) {
	secret := req.GetSecret()
	defer secure.Wipe(secret)
	if err := s.requireBarrierService(); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if err := s.barrierSvc.BarrierUnseal(ctx, secret); err != nil {
		return nil, mapBarrierError(err)
	}
	return &emptypb.Empty{}, nil
}

// BarrierSeal transitions the barrier to the sealed state.
func (s *BarrierServer) BarrierSeal(ctx context.Context, req *pb.BarrierSealRequest) (*emptypb.Empty, error) {
	if err := s.requireBarrierService(); err != nil {
		return nil, err
	}
	soPIN := []byte(req.GetSoPin())
	defer secure.Wipe(soPIN)
	if err := s.barrierSvc.BarrierSeal(ctx, &service.BarrierSealRequest{SOPIN: soPIN}); err != nil {
		return nil, mapBarrierError(err)
	}
	return &emptypb.Empty{}, nil
}

// BarrierStatus reports the current barrier state.
func (s *BarrierServer) BarrierStatus(ctx context.Context, _ *emptypb.Empty) (*pb.BarrierStatusResponse, error) {
	if err := s.requireBarrierService(); err != nil {
		return nil, err
	}
	info, err := s.barrierSvc.BarrierStatus(ctx)
	if err != nil {
		return nil, mapBarrierError(err)
	}
	return &pb.BarrierStatusResponse{
		Sealed:         info.Sealed,
		Strategy:       info.Strategy,
		HardwareBacked: info.HardwareBacked,
		InitializedAt:  formatTime(info.InitializedAt),
	}, nil
}

// BarrierShamirInitialize initialises the barrier with Shamir secret
// sharing. Threshold and total_shares must be positive and threshold must
// not exceed total_shares, mirroring the REST validation.
func (s *BarrierServer) BarrierShamirInitialize(ctx context.Context, req *pb.BarrierShamirInitializeRequest) (*pb.BarrierShamirInitializeResponse, error) {
	if err := s.requireBarrierService(); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if req.GetThreshold() <= 0 {
		return nil, status.Error(codes.InvalidArgument, "threshold must be greater than 0")
	}
	if req.GetTotalShares() <= 0 {
		return nil, status.Error(codes.InvalidArgument, "total_shares must be greater than 0")
	}
	if req.GetThreshold() > req.GetTotalShares() {
		return nil, status.Error(codes.InvalidArgument, "threshold cannot exceed total_shares")
	}
	info, err := s.barrierSvc.BarrierInitializeShamir(ctx, &qrdbtransport.BarrierInitializeShamirRequest{
		Threshold:   int(req.GetThreshold()),
		TotalShares: int(req.GetTotalShares()),
		Policy:      req.GetPolicy(),
		SOPin:       req.GetSoPin(),
		UserPin:     req.GetUserPin(),
	})
	if err != nil {
		return nil, mapBarrierError(err)
	}
	// Share material is never returned (qrdb share-free contract);
	// Security Officers claim their shares via the share-claim flow.
	return &pb.BarrierShamirInitializeResponse{
		Threshold:   int32(info.Threshold),
		TotalShares: int32(info.TotalShares),
		ShareCount:  int32(info.ShareCount),
	}, nil
}

// BarrierRecoveryGenerate generates a fresh set of recovery shares.
func (s *BarrierServer) BarrierRecoveryGenerate(ctx context.Context, req *pb.BarrierRecoveryGenerateRequest) (*pb.BarrierRecoveryKeysResponse, error) {
	if err := s.requireBarrierService(); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if req.GetThreshold() <= 0 {
		return nil, status.Error(codes.InvalidArgument, "threshold must be greater than 0")
	}
	info, err := s.barrierSvc.BarrierGenerateRecoveryKeys(ctx, &qrdbtransport.BarrierGenerateRecoveryKeysRequest{
		Threshold: int(req.GetThreshold()),
		Policy:    req.GetPolicy(),
		SOPin:     req.GetSoPin(),
	})
	if err != nil {
		return nil, mapBarrierError(err)
	}
	// Recovery shares are sealed to enrolled recovery officers and never
	// returned; only the threshold/total metadata is reported.
	return &pb.BarrierRecoveryKeysResponse{
		Threshold:  int32(info.Threshold),
		Total:      int32(info.Total),
		ShareCount: int32(info.ShareCount),
	}, nil
}

// BarrierRecoveryDelete removes all persisted recovery key metadata.
func (s *BarrierServer) BarrierRecoveryDelete(ctx context.Context, _ *emptypb.Empty) (*emptypb.Empty, error) {
	if err := s.requireBarrierService(); err != nil {
		return nil, err
	}
	if err := s.barrierSvc.BarrierDeleteRecoveryKeys(ctx); err != nil {
		return nil, mapBarrierError(err)
	}
	return &emptypb.Empty{}, nil
}

// BarrierRecoveryStatus reports whether recovery key metadata exists.
func (s *BarrierServer) BarrierRecoveryStatus(ctx context.Context, _ *emptypb.Empty) (*pb.BarrierRecoveryStatusResponse, error) {
	if err := s.requireBarrierService(); err != nil {
		return nil, err
	}
	has, err := s.barrierSvc.BarrierHasRecoveryKeys(ctx)
	if err != nil {
		return nil, mapBarrierError(err)
	}
	return &pb.BarrierRecoveryStatusResponse{HasRecoveryKeys: has}, nil
}

// BarrierEnableEncryption migrates the system barrier from the none (unencrypted)
// strategy to a keyed strategy in place. This operation is only available when
// the embedded qrdb node is present (the migrator has been injected via
// SetMigrator); otherwise codes.Unavailable is returned.
func (s *BarrierServer) BarrierEnableEncryption(ctx context.Context, req *pb.BarrierEnableEncryptionRequest) (*emptypb.Empty, error) {
	passphrase := req.GetPassphrase()
	defer secure.Wipe(passphrase)
	if err := s.requireBarrierService(); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if req.GetTargetStrategy() == "" {
		return nil, status.Error(codes.InvalidArgument, "target_strategy is required")
	}

	mech := req.GetMechanism()
	if mech == "" {
		mech = string(qrdbsdk.MechanismSwap)
	}
	avail := req.GetAvailability()
	if avail == "" {
		avail = string(qrdbsdk.AvailabilityOnline)
	}

	opts := qrdbsdk.MigrationOptions{
		TargetStrategy:  qrdbsdk.StrategyID(req.GetTargetStrategy()),
		Mechanism:       qrdbsdk.MigrationMechanism(mech),
		Availability:    qrdbsdk.MigrationAvailability(avail),
		ShamirThreshold: int(req.GetShamirThreshold()),
		ShamirTotal:     int(req.GetShamirTotal()),
	}
	if len(passphrase) > 0 {
		creds, credErr := qrdbsdk.CredentialsFor(ctx, passphrase)
		if credErr != nil {
			return nil, status.Error(codes.Internal, credErr.Error())
		}
		opts.Credentials = creds
	}

	enableErr := s.barrierSvc.EnableEncryption(ctx, "", opts)
	if opts.Credentials.Secret != nil {
		if err := opts.Credentials.Secret.Destroy(); err != nil && enableErr == nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
	}
	if enableErr != nil {
		return nil, mapBarrierError(enableErr)
	}
	return &emptypb.Empty{}, nil
}
