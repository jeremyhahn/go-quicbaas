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

// Package service provides the Seal service implementation for QuicBaaS.
// This file implements the SealService interface for database seal/unseal operations.
package service

import (
	"context"
	"time"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/util/metrics"
)

// sealService implements the SealService interface.
// It provides secure database seal/unseal operations following HashiCorp Vault patterns.
type sealService struct {
	sealManager interfaces.SealManager
	logger      interfaces.Logger
	metrics     *sealMetrics
}

// sealMetrics holds Prometheus metrics for seal operations.
type sealMetrics struct {
	sealOperationsTotal   *metrics.Counter
	unsealOperationsTotal *metrics.Counter
	rekeyOperationsTotal  *metrics.Counter
	recoveryKeyOpsTotal   *metrics.Counter
	rootTokenOpsTotal     *metrics.Counter
	operationDuration     *metrics.HistogramVec
	sealStatus            *metrics.Gauge
}

// NewSealService creates a new SealService from the given seal manager and logger.
// This is the public constructor used by server lifecycle code to wire the seal
// subsystem into the QuicBaaS service facade.
func NewSealService(sealManager interfaces.SealManager, logger interfaces.Logger) SealService {
	return newSealServiceImpl(sealManager, logger)
}

// newSealServiceImpl creates a new seal service implementation.
func newSealServiceImpl(sealManager interfaces.SealManager, logger interfaces.Logger) *sealService {
	svc := &sealService{
		sealManager: sealManager,
		logger:      logger,
	}

	// Initialize metrics
	svc.initMetrics()

	return svc
}

// initMetrics initializes Prometheus metrics for seal operations.
func (s *sealService) initMetrics() {
	s.metrics = &sealMetrics{
		sealOperationsTotal: metrics.NewCounter(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "seal",
			Name:      "seal_operations_total",
			Help:      "Total number of seal operations",
		}),

		unsealOperationsTotal: metrics.NewCounter(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "seal",
			Name:      "unseal_operations_total",
			Help:      "Total number of unseal operations",
		}),

		rekeyOperationsTotal: metrics.NewCounter(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "seal",
			Name:      "rekey_operations_total",
			Help:      "Total number of rekey operations",
		}),

		recoveryKeyOpsTotal: metrics.NewCounter(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "seal",
			Name:      "recovery_key_operations_total",
			Help:      "Total number of recovery key operations",
		}),

		rootTokenOpsTotal: metrics.NewCounter(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "seal",
			Name:      "root_token_operations_total",
			Help:      "Total number of root token generation operations",
		}),

		operationDuration: metrics.NewHistogramVec(metrics.HistogramOpts{
			Namespace: "quicbaas",
			Subsystem: "seal",
			Name:      "operation_duration_seconds",
			Help:      "Duration of seal operations in seconds",
			Buckets:   []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5},
		}, []string{"operation"}),

		sealStatus: metrics.NewGauge(metrics.GaugeOpts{
			Namespace: "quicbaas",
			Subsystem: "seal",
			Name:      "status",
			Help:      "Current seal status (1=sealed, 0=unsealed)",
		}),
	}
}

// Status returns the current seal status of the database.
func (s *sealService) Status(ctx context.Context) (*SealStatusResponse, error) {
	start := time.Now()
	defer func() {
		s.metrics.operationDuration.With("status").Observe(time.Since(start).Seconds())
	}()

	if s.logger != nil {
		s.logger.Debug("retrieving seal status")
	}

	// Get status from seal manager
	status, err := s.sealManager.Status(ctx)
	if err != nil {
		if s.logger != nil {
			s.logger.WithField("error", err).Error("failed to get seal status")
		}
		return nil, WrapError(err, dberrors.ErrInternal, "seal.Status", "failed to retrieve seal status")
	}

	// Type assert the status response
	statusResp, ok := status.(*SealStatusResponse)
	if !ok {
		err := NewErrorf(dberrors.ErrInternal, "seal.Status",
			"unexpected status type: %T", status)
		if s.logger != nil {
			s.logger.WithField("error", err).Error("invalid status response type")
		}
		return nil, err
	}

	// Update seal status metric
	if statusResp.Sealed {
		s.metrics.sealStatus.Set(1)
	} else {
		s.metrics.sealStatus.Set(0)
	}

	if s.logger != nil {
		s.logger.WithFields(map[string]interface{}{
			"sealed":      statusResp.Sealed,
			"initialized": statusResp.Initialized,
		}).Debug("seal status retrieved")
	}

	return statusResp, nil
}

// Initialize performs first-time initialization of the database seal.
func (s *sealService) Initialize(ctx context.Context, req *SealInitRequest) (*SealInitResponse, error) {
	start := time.Now()
	defer func() {
		s.metrics.operationDuration.With("initialize").Observe(time.Since(start).Seconds())
	}()

	// Validate input
	if req == nil {
		return nil, NewError(dberrors.ErrInvalidInput, "seal.Initialize", "request cannot be nil")
	}

	if req.Threshold <= 0 {
		return nil, NewError(dberrors.ErrInvalidInput, "seal.Initialize", "threshold must be positive")
	}

	if req.TotalShares <= 0 {
		return nil, NewError(dberrors.ErrInvalidInput, "seal.Initialize", "total shares must be positive")
	}

	if req.Threshold > req.TotalShares {
		return nil, NewErrorf(dberrors.ErrInvalidInput, "seal.Initialize",
			"threshold (%d) cannot exceed total shares (%d)", req.Threshold, req.TotalShares)
	}

	if req.RecoveryKeysEnabled {
		if req.RecoveryThreshold <= 0 {
			return nil, NewError(dberrors.ErrInvalidInput, "seal.Initialize",
				"recovery threshold must be positive when recovery keys enabled")
		}
		if req.RecoveryTotalKeys <= 0 {
			return nil, NewError(dberrors.ErrInvalidInput, "seal.Initialize",
				"recovery total keys must be positive when recovery keys enabled")
		}
		if req.RecoveryThreshold > req.RecoveryTotalKeys {
			return nil, NewErrorf(dberrors.ErrInvalidInput, "seal.Initialize",
				"recovery threshold (%d) cannot exceed recovery total keys (%d)",
				req.RecoveryThreshold, req.RecoveryTotalKeys)
		}
	}

	if s.logger != nil {
		s.logger.WithFields(map[string]interface{}{
			"threshold":        req.Threshold,
			"total_shares":     req.TotalShares,
			"recovery_enabled": req.RecoveryKeysEnabled,
		}).Info("initializing seal")
	}

	// Check if already initialized
	if s.sealManager.IsInitialized() {
		err := NewError(dberrors.ErrAlreadyExists, "seal.Initialize", "seal already initialized")
		if s.logger != nil {
			s.logger.Warn("seal initialization attempted when already initialized")
		}
		return nil, err
	}

	// Initialize via seal manager
	result, err := s.sealManager.Initialize(ctx, req.TotalShares, req.Threshold)
	if err != nil {
		if s.logger != nil {
			s.logger.WithField("error", err).Error("seal initialization failed")
		}
		return nil, WrapError(err, dberrors.ErrInternal, "seal.Initialize", "failed to initialize seal")
	}

	// Type assert the result
	initResp, ok := result.(*SealInitResponse)
	if !ok {
		err := NewErrorf(dberrors.ErrInternal, "seal.Initialize",
			"unexpected response type: %T", result)
		if s.logger != nil {
			s.logger.WithField("error", err).Error("invalid initialization response type")
		}
		return nil, err
	}

	if s.logger != nil {
		s.logger.WithField("version", initResp.Version).Info("seal initialized successfully")
	}

	return initResp, nil
}

// Unseal provides one key share to unseal the database.
func (s *sealService) Unseal(ctx context.Context, req *UnsealRequest) (*UnsealResponse, error) {
	start := time.Now()
	defer func() {
		s.metrics.operationDuration.With("unseal").Observe(time.Since(start).Seconds())
		s.metrics.unsealOperationsTotal.Inc()
	}()

	// Validate input
	if req == nil {
		return nil, NewError(dberrors.ErrInvalidInput, "seal.Unseal", "request cannot be nil")
	}

	if len(req.Share) == 0 {
		return nil, NewError(dberrors.ErrInvalidInput, "seal.Unseal", "share cannot be empty")
	}

	if s.logger != nil {
		s.logger.Debug("processing unseal request")
	}

	// Check if initialized
	if !s.sealManager.IsInitialized() {
		err := NewError(dberrors.ErrNotFound, "seal.Unseal", "seal not initialized")
		if s.logger != nil {
			s.logger.Warn("unseal attempted on uninitialized seal")
		}
		return nil, err
	}

	// Submit unseal share
	result, err := s.sealManager.Unseal(ctx, req.Share)
	if err != nil {
		if s.logger != nil {
			s.logger.WithField("error", err).Error("unseal operation failed")
		}
		return nil, WrapError(err, dberrors.ErrInternal, "seal.Unseal", "failed to process unseal share")
	}

	// Type assert the result
	unsealResp, ok := result.(*UnsealResponse)
	if !ok {
		err := NewErrorf(dberrors.ErrInternal, "seal.Unseal",
			"unexpected response type: %T", result)
		if s.logger != nil {
			s.logger.WithField("error", err).Error("invalid unseal response type")
		}
		return nil, err
	}

	// Update seal status metric
	if unsealResp.Sealed {
		s.metrics.sealStatus.Set(1)
	} else {
		s.metrics.sealStatus.Set(0)
	}

	if s.logger != nil {
		s.logger.WithFields(map[string]interface{}{
			"sealed":          unsealResp.Sealed,
			"shares_provided": unsealResp.SharesProvided,
			"threshold":       unsealResp.Threshold,
		}).Info("unseal progress")
	}

	return unsealResp, nil
}

// Seal immediately seals the database, making it inaccessible.
func (s *sealService) Seal(ctx context.Context) error {
	start := time.Now()
	defer func() {
		s.metrics.operationDuration.With("seal").Observe(time.Since(start).Seconds())
		s.metrics.sealOperationsTotal.Inc()
	}()

	if s.logger != nil {
		s.logger.Warn("sealing database")
	}

	// Perform seal operation
	if err := s.sealManager.Seal(ctx); err != nil {
		if s.logger != nil {
			s.logger.WithField("error", err).Error("seal operation failed")
		}
		return WrapError(err, dberrors.ErrInternal, "seal.Seal", "failed to seal database")
	}

	// Update seal status metric
	s.metrics.sealStatus.Set(1)

	if s.logger != nil {
		s.logger.Info("database sealed successfully")
	}

	return nil
}

// Rekey rotates the master key by generating new key shares.
func (s *sealService) Rekey(ctx context.Context, req *RekeyRequest) (*RekeyResponse, error) {
	start := time.Now()
	defer func() {
		s.metrics.operationDuration.With("rekey").Observe(time.Since(start).Seconds())
		s.metrics.rekeyOperationsTotal.Inc()
	}()

	// Validate input
	if req == nil {
		return nil, NewError(dberrors.ErrInvalidInput, "seal.Rekey", "request cannot be nil")
	}

	if req.NewThreshold <= 0 {
		return nil, NewError(dberrors.ErrInvalidInput, "seal.Rekey", "new threshold must be positive")
	}

	if req.NewTotalShares <= 0 {
		return nil, NewError(dberrors.ErrInvalidInput, "seal.Rekey", "new total shares must be positive")
	}

	if req.NewThreshold > req.NewTotalShares {
		return nil, NewErrorf(dberrors.ErrInvalidInput, "seal.Rekey",
			"new threshold (%d) cannot exceed new total shares (%d)",
			req.NewThreshold, req.NewTotalShares)
	}

	if s.logger != nil {
		s.logger.WithFields(map[string]interface{}{
			"new_threshold":    req.NewThreshold,
			"new_total_shares": req.NewTotalShares,
		}).Info("initiating rekey operation")
	}

	// Check if initialized
	if !s.sealManager.IsInitialized() {
		err := NewError(dberrors.ErrNotFound, "seal.Rekey", "seal not initialized")
		if s.logger != nil {
			s.logger.Warn("rekey attempted on uninitialized seal")
		}
		return nil, err
	}

	// Perform rekey operation
	result, err := s.sealManager.Rekey(ctx, req.NewTotalShares, req.NewThreshold)
	if err != nil {
		if s.logger != nil {
			s.logger.WithField("error", err).Error("rekey operation failed")
		}
		return nil, WrapError(err, dberrors.ErrInternal, "seal.Rekey", "failed to rekey seal")
	}

	// Type assert the result
	rekeyResp, ok := result.(*RekeyResponse)
	if !ok {
		err := NewErrorf(dberrors.ErrInternal, "seal.Rekey",
			"unexpected response type: %T", result)
		if s.logger != nil {
			s.logger.WithField("error", err).Error("invalid rekey response type")
		}
		return nil, err
	}

	if s.logger != nil {
		s.logger.WithField("version", rekeyResp.Version).Info("rekey completed successfully")
	}

	return rekeyResp, nil
}

// GenerateRecoveryKey creates new recovery keys for emergency access.
func (s *sealService) GenerateRecoveryKey(ctx context.Context, req *GenerateRecoveryKeyRequest) (*GenerateRecoveryKeyResponse, error) {
	start := time.Now()
	defer func() {
		s.metrics.operationDuration.With("generate_recovery_key").Observe(time.Since(start).Seconds())
		s.metrics.recoveryKeyOpsTotal.Inc()
	}()

	// Validate input
	if req == nil {
		return nil, NewError(dberrors.ErrInvalidInput, "seal.GenerateRecoveryKey", "request cannot be nil")
	}

	if req.RecoveryThreshold <= 0 {
		return nil, NewError(dberrors.ErrInvalidInput, "seal.GenerateRecoveryKey",
			"recovery threshold must be positive")
	}

	if req.RecoveryTotalKeys <= 0 {
		return nil, NewError(dberrors.ErrInvalidInput, "seal.GenerateRecoveryKey",
			"recovery total keys must be positive")
	}

	if req.RecoveryThreshold > req.RecoveryTotalKeys {
		return nil, NewErrorf(dberrors.ErrInvalidInput, "seal.GenerateRecoveryKey",
			"recovery threshold (%d) cannot exceed recovery total keys (%d)",
			req.RecoveryThreshold, req.RecoveryTotalKeys)
	}

	if s.logger != nil {
		s.logger.WithFields(map[string]interface{}{
			"threshold":  req.RecoveryThreshold,
			"total_keys": req.RecoveryTotalKeys,
		}).Info("generating recovery keys")
	}

	// Check if initialized
	if !s.sealManager.IsInitialized() {
		err := NewError(dberrors.ErrNotFound, "seal.GenerateRecoveryKey", "seal not initialized")
		if s.logger != nil {
			s.logger.Warn("generate recovery key attempted on uninitialized seal")
		}
		return nil, err
	}

	// Generate recovery keys via seal manager. Key material is never
	// returned (qrdb share-free contract).
	if _, err := s.sealManager.GenerateRecoveryKeys(ctx, req.RecoveryThreshold, req.RecoveryTotalKeys); err != nil {
		if s.logger != nil {
			s.logger.WithField("error", err).Error("generate recovery keys failed")
		}
		return nil, WrapError(err, dberrors.ErrInternal, "seal.GenerateRecoveryKey", "failed to generate recovery keys")
	}

	resp := &GenerateRecoveryKeyResponse{
		Timestamp: time.Now(),
	}

	if s.logger != nil {
		s.logger.Info("recovery keys generated successfully")
	}

	return resp, nil
}

// DeleteRecoveryKey removes all recovery keys from the system.
func (s *sealService) DeleteRecoveryKey(ctx context.Context, req *DeleteRecoveryKeyRequest) (*DeleteRecoveryKeyResponse, error) {
	start := time.Now()
	defer func() {
		s.metrics.operationDuration.With("delete_recovery_key").Observe(time.Since(start).Seconds())
		s.metrics.recoveryKeyOpsTotal.Inc()
	}()

	// Validate input
	if req == nil {
		return nil, NewError(dberrors.ErrInvalidInput, "seal.DeleteRecoveryKey", "request cannot be nil")
	}

	if !req.Confirm {
		return nil, NewError(dberrors.ErrInvalidInput, "seal.DeleteRecoveryKey",
			"must confirm deletion by setting confirm=true")
	}

	if s.logger != nil {
		s.logger.Warn("deleting recovery keys")
	}

	// Check if initialized
	if !s.sealManager.IsInitialized() {
		err := NewError(dberrors.ErrNotFound, "seal.DeleteRecoveryKey", "seal not initialized")
		if s.logger != nil {
			s.logger.Warn("delete recovery key attempted on uninitialized seal")
		}
		return nil, err
	}

	// Delete recovery keys via seal manager
	if err := s.sealManager.DeleteRecoveryKeys(ctx); err != nil {
		if s.logger != nil {
			s.logger.WithField("error", err).Error("delete recovery keys failed")
		}
		return nil, WrapError(err, dberrors.ErrInternal, "seal.DeleteRecoveryKey", "failed to delete recovery keys")
	}

	resp := &DeleteRecoveryKeyResponse{
		Deleted:   true,
		Timestamp: time.Now(),
	}

	if s.logger != nil {
		s.logger.Info("recovery keys deleted successfully")
	}

	return resp, nil
}

// RotateRootKey rotates the root encryption key protecting master key shares.
func (s *sealService) RotateRootKey(ctx context.Context, req *RotateRootKeyRequest) (*RotateRootKeyResponse, error) {
	if req == nil {
		return nil, NewError(dberrors.ErrInvalidInput, "seal.RotateRootKey", "request cannot be nil")
	}

	if s.sealManager.IsSealed() {
		return nil, NewError(dberrors.ErrUnavailable, "seal.RotateRootKey", "database is sealed")
	}

	if err := s.sealManager.RotateRootKey(ctx); err != nil {
		if s.logger != nil {
			s.logger.WithField("error", err).Error("rotate root key failed")
		}
		return nil, WrapError(err, dberrors.ErrInternal, "seal.RotateRootKey", "failed to rotate root key")
	}

	if s.logger != nil {
		s.logger.Info("root key rotated successfully")
	}

	return &RotateRootKeyResponse{
		Success:   true,
		Message:   "root key rotated successfully",
		Timestamp: time.Now(),
	}, nil
}

// GenerateRootToken creates a new root token for admin access.
func (s *sealService) GenerateRootToken(ctx context.Context, req *GenerateRootTokenRequest) (*GenerateRootTokenResponse, error) {
	start := time.Now()
	defer func() {
		s.metrics.operationDuration.With("generate_root_token").Observe(time.Since(start).Seconds())
		s.metrics.rootTokenOpsTotal.Inc()
	}()

	// Validate input
	if req == nil {
		return nil, NewError(dberrors.ErrInvalidInput, "seal.GenerateRootToken", "request cannot be nil")
	}

	if req.Strategy == "" {
		return nil, NewError(dberrors.ErrInvalidInput, "seal.GenerateRootToken", "strategy cannot be empty")
	}

	validStrategies := map[string]bool{
		"recovery_keys": true,
		"shamir_shares": true,
	}

	if !validStrategies[req.Strategy] {
		return nil, NewErrorf(dberrors.ErrInvalidInput, "seal.GenerateRootToken",
			"invalid strategy: %s (must be 'recovery_keys' or 'shamir_shares')", req.Strategy)
	}

	if len(req.Shares) == 0 {
		return nil, NewError(dberrors.ErrInvalidInput, "seal.GenerateRootToken",
			"shares cannot be empty")
	}

	if s.logger != nil {
		s.logger.WithFields(map[string]interface{}{
			"strategy":        req.Strategy,
			"shares_provided": len(req.Shares),
		}).Info("generating root token")
	}

	// Check if initialized
	if !s.sealManager.IsInitialized() {
		err := NewError(dberrors.ErrNotFound, "seal.GenerateRootToken", "seal not initialized")
		if s.logger != nil {
			s.logger.Warn("generate root token attempted on uninitialized seal")
		}
		return nil, err
	}

	// Generate root token via seal manager
	rootToken, err := s.sealManager.GenerateRootToken(ctx, req.Strategy, req.Shares)
	if err != nil {
		if s.logger != nil {
			s.logger.WithField("error", err).Error("generate root token failed")
		}
		return nil, WrapError(err, dberrors.ErrInternal, "seal.GenerateRootToken", "failed to generate root token")
	}

	resp := &GenerateRootTokenResponse{
		RootToken: rootToken,
		Timestamp: time.Now(),
	}

	if s.logger != nil {
		s.logger.Info("root token generated successfully")
	}

	return resp, nil
}
