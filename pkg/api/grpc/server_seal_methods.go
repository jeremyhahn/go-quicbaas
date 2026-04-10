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
	"sync"
	"time"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/seal"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// globalSealManager is the package-level seal manager instance.
// Kept for backward compatibility; prefer using Server.sealManager via SetSealManager.
var globalSealManager interfaces.SealManager

// SetGlobalSealManager sets the global seal manager instance.
// Deprecated: Use Server.SetSealManager for proper dependency injection.
func SetGlobalSealManager(mgr interfaces.SealManager) {
	globalSealManager = mgr
}

// GetGlobalSealManager returns the global seal manager instance.
// Deprecated: Use Server.sealManager via SetSealManager instead.
func GetGlobalSealManager() interfaces.SealManager {
	return globalSealManager
}

// InitializeDefaultSealManager initializes a default in-memory seal manager
// for testing. The seal manager uses an embedded stateful mock that tracks
// seal/unseal state without requiring a real StorageBarrier or BarrierClient.
func InitializeDefaultSealManager(log *logger.Logger) error {
	if globalSealManager != nil {
		return nil // Already initialized
	}

	globalSealManager = newDefaultSealManager()
	log.Info("Default in-memory seal manager initialized for testing")
	return nil
}

// NewDefaultSealManager creates an in-memory seal manager initialized with the
// given configuration. Used by the server package when no BarrierClient is
// available.
func NewDefaultSealManager(cfg *seal.Config) interfaces.SealManager {
	threshold := cfg.Threshold
	if threshold <= 0 {
		threshold = 2
	}
	totalShares := cfg.TotalShares
	if totalShares <= 0 {
		totalShares = 3
	}
	return &defaultSealManager{
		sealed:      true,
		initialized: false,
		threshold:   threshold,
		totalShares: totalShares,
	}
}

// defaultSealManager is a stateful in-memory SealManager used as a default
// when no BarrierClient is available. It provides the same interface contract
// for testing and development scenarios.
type defaultSealManager struct {
	mu          sync.Mutex
	sealed      bool
	initialized bool
	shares      [][]byte
	threshold   int
	totalShares int
	unsealCount int
	version     uint64
}

func newDefaultSealManager() *defaultSealManager {
	return &defaultSealManager{
		sealed:      true,
		initialized: false,
		threshold:   2,
		totalShares: 3,
	}
}

func (m *defaultSealManager) Status(_ context.Context) (interface{}, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return &service.SealStatusResponse{
		Sealed:         m.sealed,
		Initialized:    m.initialized,
		Mode:           "manual",
		Threshold:      m.threshold,
		TotalShares:    m.totalShares,
		SharesProvided: m.unsealCount,
		Version:        m.version,
		Timestamp:      time.Now(),
	}, nil
}

func (m *defaultSealManager) IsSealed() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sealed
}

func (m *defaultSealManager) IsInitialized() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.initialized
}

// AlreadyInitializedError is returned when initialization is attempted on
// an already-initialized seal manager.
type AlreadyInitializedError struct{}

// Error implements the error interface.
func (e *AlreadyInitializedError) Error() string {
	return "seal manager already initialized"
}

// InvalidThresholdError is returned when threshold exceeds total shares.
type InvalidThresholdError struct {
	Threshold int
	Total     int
}

// Error implements the error interface.
func (e *InvalidThresholdError) Error() string {
	return "invalid threshold: threshold must be <= total shares"
}

func (m *defaultSealManager) Initialize(_ context.Context, shares, threshold int) (interface{}, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.initialized {
		return nil, &AlreadyInitializedError{}
	}
	if shares <= 0 {
		shares = m.totalShares
	}
	if threshold <= 0 {
		threshold = m.threshold
	}
	if threshold > shares {
		return nil, &InvalidThresholdError{Threshold: threshold, Total: shares}
	}
	shareBytes := make([][]byte, shares)
	for i := range shareBytes {
		// Generate 32-byte shares (matching real Shamir share sizes)
		share := make([]byte, 32)
		for j := range share {
			share[j] = byte((i*32 + j + 1) % 256)
		}
		shareBytes[i] = share
	}
	m.shares = shareBytes
	m.initialized = true
	m.sealed = true
	m.threshold = threshold
	m.totalShares = shares
	m.unsealCount = 0
	m.version = 1
	return &service.SealInitResponse{
		Shares:    shareBytes,
		RootToken: "default-root-token",
		Version:   1,
		Timestamp: time.Now(),
	}, nil
}

func (m *defaultSealManager) Unseal(_ context.Context, _ []byte) (interface{}, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.initialized {
		return nil, ErrSealNotInitialized
	}
	m.unsealCount++
	complete := m.unsealCount >= m.threshold
	if complete {
		m.sealed = false
	}
	return &service.UnsealResponse{
		Sealed:         !complete,
		SharesProvided: m.unsealCount,
		Threshold:      m.threshold,
		Timestamp:      time.Now(),
	}, nil
}

func (m *defaultSealManager) Seal(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sealed = true
	m.unsealCount = 0
	return nil
}

func (m *defaultSealManager) Rekey(_ context.Context, shares, threshold int) (interface{}, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sealed {
		return nil, ErrBarrierSealed
	}
	if threshold > shares {
		return nil, &InvalidThresholdError{Threshold: threshold, Total: shares}
	}
	shareBytes := make([][]byte, shares)
	for i := range shareBytes {
		// Generate 32-byte shares (matching real Shamir share sizes)
		share := make([]byte, 32)
		for j := range share {
			share[j] = byte((i*32 + j + 10) % 256)
		}
		shareBytes[i] = share
	}
	m.shares = shareBytes
	m.threshold = threshold
	m.totalShares = shares
	m.version++
	return &service.RekeyResponse{
		Shares:    shareBytes,
		Version:   m.version,
		Timestamp: time.Now(),
	}, nil
}

func (m *defaultSealManager) GenerateRecoveryKeys(_ context.Context, _, totalKeys int) ([][]byte, error) {
	keys := make([][]byte, totalKeys)
	for i := range keys {
		key := make([]byte, 32)
		for j := range key {
			key[j] = byte((i*32 + j + 100) % 256)
		}
		keys[i] = key
	}
	return keys, nil
}

func (m *defaultSealManager) DeleteRecoveryKeys(_ context.Context) error { return nil }

func (m *defaultSealManager) GenerateRootToken(_ context.Context, _ string, _ [][]byte) (string, error) {
	return "generated-root-token", nil
}

func (m *defaultSealManager) Close() error { return nil }

// getSealManager returns the seal manager to use. Prefers the instance-level
// seal manager (set via SetSealManager), falls back to the global for backward compat.
func (s *Server) getSealManager() interfaces.SealManager {
	if s.sealManager != nil {
		return s.sealManager
	}
	return globalSealManager
}

// SealStatus returns the current seal status of the database.
func (s *Server) SealStatus(ctx context.Context, req *pb.SealStatusRequest) (*pb.SealStatusResponse, error) {
	s.logger.Debug("Processing SealStatus request")

	mgr := s.getSealManager()
	if mgr == nil {
		// Return uninitialized state
		return &pb.SealStatusResponse{
			Sealed:      true,
			Initialized: false,
			Mode:        pb.SealMode_SEAL_MODE_MANUAL,
			Threshold:   0,
			TotalShares: 0,
		}, nil
	}

	result, err := mgr.Status(ctx)
	if err != nil {
		return &pb.SealStatusResponse{
			Error: err.Error(),
		}, nil
	}

	status, ok := result.(*service.SealStatusResponse)
	if !ok {
		return &pb.SealStatusResponse{
			Error: "unexpected status response type from seal manager",
		}, nil
	}

	return &pb.SealStatusResponse{
		Sealed:         status.Sealed,
		Initialized:    status.Initialized,
		Mode:           convertSealMode(status.Mode),
		Threshold:      int32(status.Threshold),      //nolint:gosec // G115: seal parameters are small values (typically < 10)
		TotalShares:    int32(status.TotalShares),    //nolint:gosec // G115: seal parameters are small values (typically < 10)
		SharesProvided: int32(status.SharesProvided), //nolint:gosec // G115: seal parameters are small values (typically < 10)
		TpmAvailable:   status.TPMAvailable,
		ClusterNodes:   int32(status.ClusterNodes), //nolint:gosec // G115: cluster nodes small value
		Version:        status.Version,
	}, nil
}

// SealInit initializes the seal manager and generates key shares (bootstrap).
func (s *Server) SealInit(ctx context.Context, req *pb.SealInitRequest) (*pb.SealInitResponse, error) {
	s.logger.Info("Processing SealInit request")

	mgr := s.getSealManager()
	if mgr == nil {
		// Initialize default seal manager if not already done
		if err := InitializeDefaultSealManager(s.logger); err != nil {
			return &pb.SealInitResponse{
				Error: "Failed to initialize seal manager: " + err.Error(),
			}, nil
		}
		mgr = globalSealManager
	}

	result, err := mgr.Initialize(ctx, int(req.TotalShares), int(req.Threshold))
	if err != nil {
		return &pb.SealInitResponse{
			Error: err.Error(),
		}, nil
	}

	initResp, ok := result.(*service.SealInitResponse)
	if !ok {
		return &pb.SealInitResponse{
			Error: "unexpected initialization response type from seal manager",
		}, nil
	}

	return &pb.SealInitResponse{
		Shares:       initResp.Shares,
		RecoveryKeys: initResp.RecoveryKeys,
		RootToken:    initResp.RootToken,
		Version:      initResp.Version,
	}, nil
}

// Unseal provides a key share to attempt unsealing the database.
func (s *Server) Unseal(ctx context.Context, req *pb.UnsealRequest) (*pb.UnsealResponse, error) {
	s.logger.Info("Processing Unseal request")

	mgr := s.getSealManager()
	if mgr == nil {
		return &pb.UnsealResponse{
			Sealed: true,
			Error:  "Seal manager not initialized",
		}, nil
	}

	result, err := mgr.Unseal(ctx, req.Share)
	if err != nil {
		return &pb.UnsealResponse{
			Sealed: true,
			Error:  err.Error(),
		}, nil
	}

	unsealResp, ok := result.(*service.UnsealResponse)
	if !ok {
		return &pb.UnsealResponse{
			Sealed: true,
			Error:  "unexpected unseal response type from seal manager",
		}, nil
	}

	return &pb.UnsealResponse{
		Sealed:         unsealResp.Sealed,
		SharesProvided: int32(unsealResp.SharesProvided), //nolint:gosec // G115: seal parameters small
		Threshold:      int32(unsealResp.Threshold),      //nolint:gosec // G115: seal parameters small
		Error:          unsealResp.Error,
	}, nil
}

// Seal re-seals the database (requires unsealed state).
func (s *Server) Seal(ctx context.Context, req *pb.SealRequest) (*pb.SealResponse, error) {
	s.logger.Info("Processing Seal request")

	mgr := s.getSealManager()
	if mgr == nil {
		return &pb.SealResponse{
			Sealed: true,
			Error:  "Seal manager not initialized",
		}, nil
	}

	err := mgr.Seal(ctx)
	if err != nil {
		return &pb.SealResponse{
			Sealed: false,
			Error:  err.Error(),
		}, nil
	}

	return &pb.SealResponse{
		Sealed: true,
	}, nil
}

// SealRekey generates new key shares with a new threshold configuration.
func (s *Server) SealRekey(ctx context.Context, req *pb.SealRekeyRequest) (*pb.SealRekeyResponse, error) {
	s.logger.Infof("Processing SealRekey request: threshold=%d, total=%d", req.NewThreshold, req.NewTotal)

	mgr := s.getSealManager()
	if mgr == nil {
		return &pb.SealRekeyResponse{
			Error: "Seal manager not initialized",
		}, nil
	}

	result, err := mgr.Rekey(ctx, int(req.NewTotal), int(req.NewThreshold))
	if err != nil {
		return &pb.SealRekeyResponse{
			Error: err.Error(),
		}, nil
	}

	rekeyResp, ok := result.(*service.RekeyResponse)
	if !ok {
		return &pb.SealRekeyResponse{
			Error: "unexpected rekey response type from seal manager",
		}, nil
	}

	return &pb.SealRekeyResponse{
		Shares:  rekeyResp.Shares,
		Version: rekeyResp.Version,
	}, nil
}
