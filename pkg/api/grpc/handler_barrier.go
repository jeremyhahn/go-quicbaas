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
	if err := s.requireBarrierService(); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	secret := req.GetSecret()
	defer secure.Wipe(secret)
	if err := s.barrierSvc.BarrierInitialize(ctx, secret); err != nil {
		return nil, mapBarrierError(err)
	}
	return &emptypb.Empty{}, nil
}

// BarrierUnseal unseals the barrier with a passphrase.
func (s *BarrierServer) BarrierUnseal(ctx context.Context, req *pb.BarrierUnsealRequest) (*emptypb.Empty, error) {
	if err := s.requireBarrierService(); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	secret := req.GetSecret()
	defer secure.Wipe(secret)
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
		Sealed:          info.Sealed,
		Strategy:        info.Strategy,
		HardwareBacked:  info.HardwareBacked,
		InitializedAt:   info.InitializedAt,
		ShamirThreshold: int32(info.ShamirThreshold),
		ShamirTotal:     int32(info.ShamirTotal),
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
	secret := req.GetSecret()
	defer secure.Wipe(secret)
	info, err := s.barrierSvc.BarrierInitializeShamir(ctx, secret, int(req.GetThreshold()), int(req.GetTotalShares()))
	if err != nil {
		return nil, mapBarrierError(err)
	}
	// Share material is never returned (qrdb share-free contract);
	// Security Officers claim their shares via the share-claim flow.
	return &pb.BarrierShamirInitializeResponse{
		Threshold:   int32(info.Threshold),
		TotalShares: int32(info.TotalShares),
	}, nil
}

// BarrierShamirUnsealShare submits a single Shamir share toward the
// unseal quorum.
func (s *BarrierServer) BarrierShamirUnsealShare(ctx context.Context, req *pb.BarrierShamirUnsealShareRequest) (*pb.BarrierShamirUnsealShareResponse, error) {
	if err := s.requireBarrierService(); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if req.GetShare() == "" {
		return nil, status.Error(codes.InvalidArgument, "share is required")
	}
	progress, err := s.barrierSvc.BarrierUnsealWithShare(ctx, req.GetShare())
	if err != nil {
		return nil, mapBarrierError(err)
	}
	return &pb.BarrierShamirUnsealShareResponse{
		Required:  int32(progress.Required),
		Submitted: int32(progress.Submitted),
		Complete:  progress.Complete,
	}, nil
}

// BarrierShamirUnsealShares unseals the barrier with a complete set of
// shares supplied in one call.
func (s *BarrierServer) BarrierShamirUnsealShares(ctx context.Context, req *pb.BarrierShamirUnsealSharesRequest) (*emptypb.Empty, error) {
	if err := s.requireBarrierService(); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if len(req.GetShares()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "shares is required")
	}
	if err := s.barrierSvc.BarrierUnsealWithShares(ctx, req.GetShares()); err != nil {
		return nil, mapBarrierError(err)
	}
	return &emptypb.Empty{}, nil
}

// BarrierRekey regenerates Shamir shares for the existing root key.
func (s *BarrierServer) BarrierRekey(ctx context.Context, req *pb.BarrierRekeyRequest) (*pb.BarrierShamirInitializeResponse, error) {
	if err := s.requireBarrierService(); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if req.GetThreshold() <= 0 {
		return nil, status.Error(codes.InvalidArgument, "threshold must be greater than 0")
	}
	if req.GetTotal() <= 0 {
		return nil, status.Error(codes.InvalidArgument, "total must be greater than 0")
	}
	if req.GetThreshold() > req.GetTotal() {
		return nil, status.Error(codes.InvalidArgument, "threshold cannot exceed total")
	}
	info, err := s.barrierSvc.BarrierRekey(ctx, int(req.GetThreshold()), int(req.GetTotal()))
	if err != nil {
		return nil, mapBarrierError(err)
	}
	// New shares are sealed to enrolled Security Officers and never
	// returned; only the threshold/total metadata is reported.
	return &pb.BarrierShamirInitializeResponse{
		Threshold:   int32(info.Threshold),
		TotalShares: int32(info.TotalShares),
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
	if req.GetTotal() <= 0 {
		return nil, status.Error(codes.InvalidArgument, "total must be greater than 0")
	}
	if req.GetThreshold() > req.GetTotal() {
		return nil, status.Error(codes.InvalidArgument, "threshold cannot exceed total")
	}
	info, err := s.barrierSvc.BarrierGenerateRecoveryKeys(ctx, int(req.GetThreshold()), int(req.GetTotal()))
	if err != nil {
		return nil, mapBarrierError(err)
	}
	// Recovery shares are sealed to enrolled recovery officers and never
	// returned; only the threshold/total metadata is reported.
	return &pb.BarrierRecoveryKeysResponse{
		Threshold: int32(info.Threshold),
		Total:     int32(info.Total),
	}, nil
}

// BarrierRecoveryRecover unseals the barrier using recovery shares.
func (s *BarrierServer) BarrierRecoveryRecover(ctx context.Context, req *pb.BarrierRecoveryRecoverRequest) (*emptypb.Empty, error) {
	if err := s.requireBarrierService(); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if len(req.GetKeys()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "keys is required")
	}
	if err := s.barrierSvc.BarrierRecoverWithKeys(ctx, req.GetKeys()); err != nil {
		return nil, mapBarrierError(err)
	}
	return &emptypb.Empty{}, nil
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

// BarrierRootToken generates a fresh root token by proving knowledge of
// the master key through Shamir shares.
func (s *BarrierServer) BarrierRootToken(ctx context.Context, req *pb.BarrierRootTokenRequest) (*pb.BarrierRootTokenResponse, error) {
	if err := s.requireBarrierService(); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if len(req.GetShares()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "shares is required")
	}
	info, err := s.barrierSvc.BarrierGenerateRootToken(ctx, req.GetShares())
	if err != nil {
		return nil, mapBarrierError(err)
	}
	return &pb.BarrierRootTokenResponse{
		Token:     info.Token,
		CreatedAt: info.CreatedAt,
	}, nil
}

// BarrierEnableEncryption migrates the system barrier from the none (unencrypted)
// strategy to a keyed strategy in place. This operation is only available when
// the embedded qrdb node is present (the migrator has been injected via
// SetMigrator); otherwise codes.Unavailable is returned.
func (s *BarrierServer) BarrierEnableEncryption(ctx context.Context, req *pb.BarrierEnableEncryptionRequest) (*emptypb.Empty, error) {
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
	if passphrase := req.GetPassphrase(); len(passphrase) > 0 {
		defer secure.Wipe(passphrase)
		creds, credErr := qrdbsdk.CredentialsFor(passphrase)
		if credErr != nil {
			return nil, status.Error(codes.Internal, credErr.Error())
		}
		defer creds.Secret.Destroy()
		opts.Credentials = creds
	}

	if err := s.barrierSvc.EnableEncryption(ctx, "", opts); err != nil {
		return nil, mapBarrierError(err)
	}
	return &emptypb.Empty{}, nil
}
