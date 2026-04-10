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

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// ServerWithSeal wraps a Server and adds seal functionality.
type ServerWithSeal struct {
	*Server
	sealSvc *SealService
}

// NewServerWithSeal creates a new gRPC server with seal service support.
func NewServerWithSeal(srv *Server, sealManager interfaces.SealManager) *ServerWithSeal {
	sealSvc := NewSealService(sealManager, srv.logger)
	return &ServerWithSeal{
		Server:  srv,
		sealSvc: sealSvc,
	}
}

// SealStatus returns the current seal status of the database.
func (s *ServerWithSeal) SealStatus(ctx context.Context, req *pb.SealStatusRequest) (*pb.SealStatusResponse, error) {
	s.logger.Debug("Processing SealStatus request")

	status, err := s.sealSvc.Status(ctx)
	if err != nil {
		return &pb.SealStatusResponse{
			Error: err.Error(),
		}, nil
	}

	//nolint:gosec // G115: seal parameters are small values (threshold/shares typically < 10)
	return &pb.SealStatusResponse{
		Sealed:         status.Sealed,
		Initialized:    status.Initialized,
		Mode:           convertSealMode(status.Mode),
		Threshold:      int32(status.Threshold),
		TotalShares:    int32(status.TotalShares),
		SharesProvided: int32(status.SharesProvided),
		TpmAvailable:   status.TPMAvailable,
		ClusterNodes:   int32(status.ClusterNodes),
		Version:        status.Version,
	}, nil
}

// SealInit initializes the seal manager and generates key shares (bootstrap).
func (s *ServerWithSeal) SealInit(ctx context.Context, req *pb.SealInitRequest) (*pb.SealInitResponse, error) {
	s.logger.Info("Processing SealInit request")

	bootstrapReq := &BootstrapRequest{
		Threshold:           int(req.Threshold),
		TotalShares:         int(req.TotalShares),
		RecoveryKeysEnabled: req.RecoveryKeysEnabled,
		RecoveryThreshold:   int(req.RecoveryThreshold),
		RecoveryTotalKeys:   int(req.RecoveryTotalKeys),
	}

	resp, err := s.sealSvc.Bootstrap(ctx, bootstrapReq)
	if err != nil {
		return &pb.SealInitResponse{
			Error: err.Error(),
		}, nil
	}

	if resp.Error != "" {
		return &pb.SealInitResponse{
			Error: resp.Error,
		}, nil
	}

	return &pb.SealInitResponse{
		Shares:       resp.Shares,
		RecoveryKeys: resp.RecoveryKeys,
		RootToken:    resp.RootToken,
		Version:      resp.Version,
	}, nil
}

// Unseal provides a key share to attempt unsealing the database.
func (s *ServerWithSeal) Unseal(ctx context.Context, req *pb.UnsealRequest) (*pb.UnsealResponse, error) {
	s.logger.Info("Processing Unseal request")

	unsealReq := &UnsealRequestDTO{
		Share:     req.Share,
		NodeID:    req.NodeId,
		Signature: req.Signature,
	}

	resp, err := s.sealSvc.Unseal(ctx, unsealReq)
	if err != nil {
		return &pb.UnsealResponse{
			Sealed: true,
			Error:  err.Error(),
		}, nil
	}

	//nolint:gosec // G115: seal parameters are small values (threshold/shares typically < 10)
	return &pb.UnsealResponse{
		Sealed:         resp.Sealed,
		SharesProvided: int32(resp.SharesProvided),
		Threshold:      int32(resp.Threshold),
		Error:          resp.Error,
	}, nil
}

// Seal re-seals the database (requires unsealed state).
func (s *ServerWithSeal) Seal(ctx context.Context, req *pb.SealRequest) (*pb.SealResponse, error) {
	s.logger.Info("Processing Seal request")

	sealReq := &SealRequestDTO{
		Force: req.Force,
	}

	resp, err := s.sealSvc.Seal(ctx, sealReq)
	if err != nil {
		return &pb.SealResponse{
			Sealed: false,
			Error:  err.Error(),
		}, nil
	}

	return &pb.SealResponse{
		Sealed: resp.Sealed,
		Error:  resp.Error,
	}, nil
}

// SealRekey generates new key shares with a new threshold configuration.
func (s *ServerWithSeal) SealRekey(ctx context.Context, req *pb.SealRekeyRequest) (*pb.SealRekeyResponse, error) {
	s.logger.Infof("Processing SealRekey request: threshold=%d, total=%d", req.NewThreshold, req.NewTotal)

	rekeyReq := &RekeyRequestDTO{
		NewThreshold: int(req.NewThreshold),
		NewTotal:     int(req.NewTotal),
	}

	resp, err := s.sealSvc.Rekey(ctx, rekeyReq)
	if err != nil {
		return &pb.SealRekeyResponse{
			Error: err.Error(),
		}, nil
	}

	return &pb.SealRekeyResponse{
		Shares:  resp.Shares,
		Version: resp.Version,
		Error:   resp.Error,
	}, nil
}

// convertSealMode converts a seal mode string to the proto enum.
func convertSealMode(mode string) pb.SealMode {
	switch mode {
	case "manual":
		return pb.SealMode_SEAL_MODE_MANUAL
	case "auto":
		return pb.SealMode_SEAL_MODE_AUTO
	case "crypto":
		return pb.SealMode_SEAL_MODE_CRYPTO
	default:
		return pb.SealMode_SEAL_MODE_UNSPECIFIED
	}
}
