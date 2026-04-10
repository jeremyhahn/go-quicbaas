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

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// SealService provides gRPC endpoints for seal/unseal operations.
type SealService struct {
	manager interfaces.SealManager
	logger  *logger.Logger
}

// NewSealService creates a new seal service.
func NewSealService(manager interfaces.SealManager, log *logger.Logger) *SealService {
	return &SealService{
		manager: manager,
		logger:  log,
	}
}

// BootstrapRequest represents a bootstrap request.
type BootstrapRequest struct {
	Threshold           int  `json:"threshold"`
	TotalShares         int  `json:"total_shares"`
	RecoveryKeysEnabled bool `json:"recovery_keys_enabled"`
	RecoveryThreshold   int  `json:"recovery_threshold"`
	RecoveryTotalKeys   int  `json:"recovery_total_keys"`
}

// BootstrapResponse contains the generated key shares.
type BootstrapResponse struct {
	Shares       [][]byte `json:"shares"`
	RecoveryKeys [][]byte `json:"recovery_keys"`
	RootToken    string   `json:"root_token"`
	Version      uint64   `json:"version"`
	Error        string   `json:"error,omitempty"`
}

// Bootstrap initializes the seal manager and generates key shares.
func (s *SealService) Bootstrap(ctx context.Context, req *BootstrapRequest) (*BootstrapResponse, error) {
	s.logger.Info("Processing seal bootstrap request")

	result, err := s.manager.Initialize(ctx, req.TotalShares, req.Threshold)
	if err != nil {
		s.logger.Errorf("Bootstrap failed: %v", err)
		return &BootstrapResponse{
			Error: err.Error(),
		}, nil
	}

	initResp, ok := result.(*service.SealInitResponse)
	if !ok {
		return &BootstrapResponse{
			Error: "unexpected response type from seal manager",
		}, nil
	}

	return &BootstrapResponse{
		Shares:       initResp.Shares,
		RecoveryKeys: initResp.RecoveryKeys,
		RootToken:    initResp.RootToken,
		Version:      initResp.Version,
	}, nil
}

// UnsealRequestDTO represents an unseal request.
type UnsealRequestDTO struct {
	Share     []byte `json:"share"`
	NodeID    uint64 `json:"node_id"`
	Signature []byte `json:"signature"`
}

// UnsealResponseDTO represents an unseal response.
type UnsealResponseDTO struct {
	Sealed         bool   `json:"sealed"`
	SharesProvided int    `json:"shares_provided"`
	Threshold      int    `json:"threshold"`
	Error          string `json:"error,omitempty"`
}

// Unseal provides a key share to attempt unsealing.
func (s *SealService) Unseal(ctx context.Context, req *UnsealRequestDTO) (*UnsealResponseDTO, error) {
	s.logger.Info("Processing unseal request")

	result, err := s.manager.Unseal(ctx, req.Share)
	if err != nil {
		s.logger.Errorf("Unseal failed: %v", err)
		return &UnsealResponseDTO{
			Sealed: true,
			Error:  err.Error(),
		}, nil
	}

	unsealResp, ok := result.(*service.UnsealResponse)
	if !ok {
		return &UnsealResponseDTO{
			Sealed: true,
			Error:  "unexpected response type from seal manager",
		}, nil
	}

	return &UnsealResponseDTO{
		Sealed:         unsealResp.Sealed,
		SharesProvided: unsealResp.SharesProvided,
		Threshold:      unsealResp.Threshold,
		Error:          unsealResp.Error,
	}, nil
}

// SealRequestDTO represents a seal request.
type SealRequestDTO struct {
	Force bool `json:"force"`
}

// SealResponseDTO represents a seal response.
type SealResponseDTO struct {
	Sealed bool   `json:"sealed"`
	Error  string `json:"error,omitempty"`
}

// Seal re-seals the database.
func (s *SealService) Seal(ctx context.Context, req *SealRequestDTO) (*SealResponseDTO, error) {
	s.logger.Info("Processing seal request")

	err := s.manager.Seal(ctx)
	if err != nil {
		s.logger.Errorf("Seal failed: %v", err)
		return &SealResponseDTO{
			Sealed: false,
			Error:  err.Error(),
		}, nil
	}

	return &SealResponseDTO{
		Sealed: true,
	}, nil
}

// StatusResponseDTO represents seal status.
type StatusResponseDTO struct {
	Sealed         bool   `json:"sealed"`
	Initialized    bool   `json:"initialized"`
	Mode           string `json:"mode"`
	Threshold      int    `json:"threshold"`
	TotalShares    int    `json:"total_shares"`
	SharesProvided int    `json:"shares_provided"`
	TPMAvailable   bool   `json:"tpm_available"`
	ClusterNodes   int    `json:"cluster_nodes"`
	SharesReceived int    `json:"shares_received"`
	Version        uint64 `json:"version"`
	Error          string `json:"error,omitempty"`
}

// Status returns the current seal status.
func (s *SealService) Status(ctx context.Context) (*StatusResponseDTO, error) {
	s.logger.Debug("Processing seal status request")

	status, err := s.manager.Status(ctx)
	if err != nil {
		s.logger.Errorf("Status failed: %v", err)
		return &StatusResponseDTO{
			Error: err.Error(),
		}, nil
	}

	statusResp, ok := status.(*service.SealStatusResponse)
	if !ok {
		return &StatusResponseDTO{
			Error: "unexpected response type from seal manager",
		}, nil
	}

	return &StatusResponseDTO{
		Sealed:         statusResp.Sealed,
		Initialized:    statusResp.Initialized,
		Mode:           statusResp.Mode,
		Threshold:      statusResp.Threshold,
		TotalShares:    statusResp.TotalShares,
		SharesProvided: statusResp.SharesProvided,
		TPMAvailable:   statusResp.TPMAvailable,
		ClusterNodes:   statusResp.ClusterNodes,
		SharesReceived: statusResp.SharesReceived,
		Version:        statusResp.Version,
	}, nil
}

// RekeyRequestDTO represents a rekey request.
type RekeyRequestDTO struct {
	NewThreshold int `json:"new_threshold"`
	NewTotal     int `json:"new_total"`
}

// RekeyResponseDTO represents a rekey response.
type RekeyResponseDTO struct {
	Shares  [][]byte `json:"shares"`
	Version uint64   `json:"version"`
	Error   string   `json:"error,omitempty"`
}

// Rekey generates new key shares.
func (s *SealService) Rekey(ctx context.Context, req *RekeyRequestDTO) (*RekeyResponseDTO, error) {
	s.logger.Infof("Processing rekey request: threshold=%d, total=%d", req.NewThreshold, req.NewTotal)

	result, err := s.manager.Rekey(ctx, req.NewTotal, req.NewThreshold)
	if err != nil {
		s.logger.Errorf("Rekey failed: %v", err)
		return &RekeyResponseDTO{
			Error: err.Error(),
		}, nil
	}

	rekeyResp, ok := result.(*service.RekeyResponse)
	if !ok {
		return &RekeyResponseDTO{
			Error: "unexpected response type from seal manager",
		}, nil
	}

	return &RekeyResponseDTO{
		Shares:  rekeyResp.Shares,
		Version: rekeyResp.Version,
	}, nil
}

// RecoverRequestDTO represents a recovery request.
type RecoverRequestDTO struct {
	RecoveryKeys [][]byte `json:"recovery_keys"`
}

// RecoverResponseDTO represents a recovery response.
type RecoverResponseDTO struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// Recover attempts recovery using recovery keys.
func (s *SealService) Recover(ctx context.Context, req *RecoverRequestDTO) (*RecoverResponseDTO, error) {
	s.logger.Info("Processing recovery request")

	_, err := s.manager.GenerateRootToken(ctx, "recovery_keys", req.RecoveryKeys)
	if err != nil {
		s.logger.Errorf("Recovery failed: %v", err)
		return &RecoverResponseDTO{
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	return &RecoverResponseDTO{
		Success: true,
	}, nil
}

// IsSealed returns whether the database is sealed.
func (s *SealService) IsSealed() bool {
	return s.manager.IsSealed()
}

// IsInitialized returns whether the seal manager is initialized.
func (s *SealService) IsInitialized() bool {
	return s.manager.IsInitialized()
}

// Close closes the seal service.
func (s *SealService) Close() error {
	return s.manager.Close()
}
