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

// Package seal provides the BarrierSealManager, a pure SDK adapter that
// delegates all seal lifecycle operations to a go-qrdb BarrierClient via
// the transport layer. No barrier logic, share accumulation, or crypto
// material is managed locally.
package seal

import (
	"context"
	"sync/atomic"
	"time"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// SealMode defines the unseal coordination mode.
type SealMode string

const (
	// SealModeManual requires manual operator intervention with Shamir shares.
	SealModeManual SealMode = "manual"

	// SealModeAuto uses Raft-coordinated automatic unsealing.
	SealModeAuto SealMode = "auto"
)

const (
	// StrategyRecoveryKeys is the strategy name for using recovery keys.
	StrategyRecoveryKeys = "recovery_keys"

	// StrategyShamirShares is the strategy name for using shamir shares.
	StrategyShamirShares = "shamir_shares"
)

// Config holds seal manager configuration.
type Config struct {
	// Mode specifies manual (Shamir) or auto (Raft) unseal.
	Mode SealMode `json:"mode" yaml:"mode"`

	// Threshold is the minimum shares needed to reconstruct the master key (t).
	Threshold int `json:"threshold" yaml:"threshold"`

	// TotalShares is the total number of shares to generate (n).
	TotalShares int `json:"total_shares" yaml:"total_shares"`

	// RecoveryKeysEnabled enables generation of recovery keys for disaster recovery.
	RecoveryKeysEnabled bool `json:"recovery_keys_enabled" yaml:"recovery_keys_enabled"`

	// RecoveryThreshold is the minimum recovery keys needed.
	RecoveryThreshold int `json:"recovery_threshold" yaml:"recovery_threshold"`

	// RecoveryTotalKeys is total recovery keys to generate.
	RecoveryTotalKeys int `json:"recovery_total_keys" yaml:"recovery_total_keys"`

	// DeploymentID is bound into HKDF info during DEK derivation for
	// cryptographic isolation between clusters.
	DeploymentID uint64 `json:"deployment_id" yaml:"deployment_id"`
}

// DefaultConfig returns a default seal configuration.
func DefaultConfig() *Config {
	return &Config{
		Mode:                SealModeAuto,
		Threshold:           2,
		TotalShares:         3,
		RecoveryKeysEnabled: true,
		RecoveryThreshold:   3,
		RecoveryTotalKeys:   5,
	}
}

// Compile-time interface check.
var _ interfaces.SealManager = (*BarrierSealManager)(nil)

// BarrierNilError is returned when a nil barrier client is provided to the constructor.
type BarrierNilError struct{}

// Error implements the error interface.
func (e *BarrierNilError) Error() string {
	return "barrier_seal_manager: barrier must not be nil"
}

// ConfigNilError is returned when a nil config is provided and defaults cannot
// be applied. This error is informational only; the constructor applies defaults
// rather than returning this error.
type ConfigNilError struct{}

// Error implements the error interface.
func (e *ConfigNilError) Error() string {
	return "barrier_seal_manager: config must not be nil"
}

// InvalidStrategyError is returned when an unrecognized root token generation
// strategy is provided.
type InvalidStrategyError struct {
	Strategy string
}

// Error implements the error interface.
func (e *InvalidStrategyError) Error() string {
	return "barrier_seal_manager: invalid strategy: " + e.Strategy
}

// NotInitializedError is returned when an unseal operation is attempted on a
// barrier that has not been initialized.
type NotInitializedError struct{}

// Error implements the error interface.
func (e *NotInitializedError) Error() string {
	return "barrier_seal_manager: barrier has not been initialized"
}

// BarrierSealedError is returned when an operation requiring an unsealed
// barrier is attempted while the barrier is sealed.
type BarrierSealedError struct {
	Op string
}

// Error implements the error interface.
func (e *BarrierSealedError) Error() string {
	return "barrier_seal_manager: barrier is sealed: " + e.Op
}

// RekeyStrategyError is returned when the ShamirStrategy is not available
// on the barrier during a rekey operation.
type RekeyStrategyError struct{}

// Error implements the error interface.
func (e *RekeyStrategyError) Error() string {
	return "barrier_seal_manager: shamir strategy not available for rekey"
}

// DuplicateShareError is returned when a share that has already been submitted
// is submitted again during the same unseal session.
type DuplicateShareError struct{}

// Error implements the error interface.
func (e *DuplicateShareError) Error() string {
	return "barrier_seal_manager: duplicate share submitted"
}

// AlreadyUnsealedError is returned when an unseal operation is attempted
// on a barrier that is already unsealed.
type AlreadyUnsealedError struct{}

// Error implements the error interface.
func (e *AlreadyUnsealedError) Error() string {
	return "barrier_seal_manager: barrier is already unsealed"
}

// BarrierSealManager is a pure SDK adapter that delegates all seal lifecycle
// operations to a go-qrdb BarrierClient. It performs no barrier logic, share
// accumulation, or cryptographic operations locally.
type BarrierSealManager struct {
	client  qrdbtransport.BarrierClient
	logger  interfaces.Logger
	config  *Config
	version atomic.Uint64
}

// NewBarrierSealManager creates a new BarrierSealManager wrapping the given
// BarrierClient. If config is nil, DefaultConfig() is applied.
func NewBarrierSealManager(
	client qrdbtransport.BarrierClient,
	config *Config,
	logger interfaces.Logger,
) (*BarrierSealManager, error) {
	if client == nil {
		return nil, &BarrierNilError{}
	}
	if config == nil {
		config = DefaultConfig()
	}
	return &BarrierSealManager{
		client: client,
		logger: logger,
		config: config,
	}, nil
}

// Status returns the current seal status as a *service.SealStatusResponse.
func (m *BarrierSealManager) Status(ctx context.Context) (interface{}, error) {
	resp, err := m.client.BarrierStatus(ctx)
	if err != nil {
		return nil, err
	}
	return &service.SealStatusResponse{
		Sealed:      resp.Sealed,
		Initialized: resp.InitializedAt != "",
		Mode:        string(m.config.Mode),
		Threshold:   m.config.Threshold,
		TotalShares: m.config.TotalShares,
		Version:     m.version.Load(),
		Timestamp:   time.Now(),
	}, nil
}

// IsSealed returns true if the barrier is currently sealed.
func (m *BarrierSealManager) IsSealed() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := m.client.BarrierStatus(ctx)
	if err != nil {
		return true // assume sealed on error
	}
	return resp.Sealed
}

// IsInitialized returns true if the barrier has been initialized.
func (m *BarrierSealManager) IsInitialized() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := m.client.BarrierStatus(ctx)
	if err != nil {
		return false
	}
	return resp.InitializedAt != ""
}

// Initialize sets up the barrier using Shamir secret sharing and returns a
// *service.SealInitResponse containing the generated shares. If shares or
// threshold are zero, the values from the manager's Config are used.
//
// After initialization, the barrier is sealed so that callers must explicitly
// unseal using the returned shares.
func (m *BarrierSealManager) Initialize(ctx context.Context, shares, threshold int) (interface{}, error) {
	if threshold <= 0 {
		threshold = m.config.Threshold
	}
	if shares <= 0 {
		shares = m.config.TotalShares
	}

	resp, err := m.client.BarrierInitializeShamir(ctx, &qrdbtransport.BarrierInitializeShamirRequest{
		Threshold:   threshold,
		TotalShares: shares,
	})
	if err != nil {
		return nil, err
	}

	shareBytes := make([][]byte, len(resp.Shares))
	for i, s := range resp.Shares {
		shareBytes[i] = []byte(s)
	}

	m.config.Threshold = threshold
	m.config.TotalShares = shares
	m.version.Store(1)

	// Seal after initialization so callers must explicitly unseal.
	if err := m.client.BarrierSeal(ctx); err != nil {
		return nil, err
	}

	m.logger.Infof("Barrier initialized with threshold=%d, total=%d", threshold, shares)

	return &service.SealInitResponse{
		Shares:    shareBytes,
		RootToken: "", // Root tokens not exposed through SDK transport
		Version:   1,
		Timestamp: time.Now(),
	}, nil
}

// Unseal submits a single Shamir share toward the quorum threshold. Returns a
// *service.UnsealResponse with progress information.
func (m *BarrierSealManager) Unseal(ctx context.Context, share []byte) (interface{}, error) {
	resp, err := m.client.BarrierUnsealWithShare(ctx, &qrdbtransport.BarrierUnsealShareRequest{
		Share: string(share),
	})
	if err != nil {
		return nil, err
	}

	return &service.UnsealResponse{
		Sealed:         !resp.Complete,
		SharesProvided: resp.Submitted,
		Threshold:      resp.Required,
		Timestamp:      time.Now(),
	}, nil
}

// Seal re-seals the barrier, clearing cryptographic material from memory.
func (m *BarrierSealManager) Seal(ctx context.Context) error {
	m.logger.Info("Sealing barrier")
	return m.client.BarrierSeal(ctx)
}

// Rekey generates new Shamir shares with potentially different parameters.
// Returns a *service.RekeyResponse. If shares or threshold are zero, the
// values from the manager's Config are used.
func (m *BarrierSealManager) Rekey(ctx context.Context, shares, threshold int) (interface{}, error) {
	if threshold <= 0 {
		threshold = m.config.Threshold
	}
	if shares <= 0 {
		shares = m.config.TotalShares
	}

	resp, err := m.client.BarrierRekey(ctx, &qrdbtransport.BarrierRekeyRequest{
		Threshold: threshold,
		Total:     shares,
	})
	if err != nil {
		return nil, err
	}

	shareBytes := make([][]byte, len(resp.Shares))
	for i, s := range resp.Shares {
		shareBytes[i] = []byte(s)
	}

	m.config.Threshold = threshold
	m.config.TotalShares = shares
	newVersion := m.version.Add(1)

	m.logger.Infof("Rekeyed with threshold=%d, total=%d", threshold, shares)

	return &service.RekeyResponse{
		Shares:    shareBytes,
		Version:   newVersion,
		Timestamp: time.Now(),
	}, nil
}

// GenerateRecoveryKeys generates Shamir recovery shares via the SDK.
// Returns the recovery key shares as [][]byte.
func (m *BarrierSealManager) GenerateRecoveryKeys(ctx context.Context, threshold, totalKeys int) ([][]byte, error) {
	resp, err := m.client.BarrierGenerateRecoveryKeys(ctx, &qrdbtransport.BarrierGenerateRecoveryKeysRequest{
		Threshold: threshold,
		Total:     totalKeys,
	})
	if err != nil {
		return nil, err
	}

	keys := make([][]byte, len(resp.Keys))
	for i, k := range resp.Keys {
		keys[i] = []byte(k)
	}

	m.logger.Infof("Generated %d recovery keys with threshold %d", totalKeys, threshold)
	return keys, nil
}

// DeleteRecoveryKeys removes recovery key metadata via the SDK.
func (m *BarrierSealManager) DeleteRecoveryKeys(ctx context.Context) error {
	m.logger.Warn("Deleting recovery keys")
	return m.client.BarrierDeleteRecoveryKeys(ctx)
}

// GenerateRootToken generates a root token by proving knowledge of the master
// key through shares. The strategy parameter selects between "recovery_keys"
// and "shamir_shares".
func (m *BarrierSealManager) GenerateRootToken(ctx context.Context, strategy string, shares [][]byte) (string, error) {
	if strategy != StrategyRecoveryKeys && strategy != StrategyShamirShares {
		return "", &InvalidStrategyError{Strategy: strategy}
	}

	shareStrings := make([]string, len(shares))
	for i, s := range shares {
		shareStrings[i] = string(s)
	}

	resp, err := m.client.BarrierGenerateRootToken(ctx, &qrdbtransport.BarrierGenerateRootTokenRequest{
		Shares: shareStrings,
	})
	if err != nil {
		return "", err
	}
	return resp.Token, nil
}

// Close releases resources. The SDK client lifecycle is managed separately.
func (m *BarrierSealManager) Close() error {
	m.logger.Info("Closing barrier seal manager")
	return nil
}
