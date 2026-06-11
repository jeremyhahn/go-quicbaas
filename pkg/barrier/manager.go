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

// Package barrier provides the QuicBaaS BarrierManager, a thin facade over a
// go-qrdb StorageBarrier. Strategy chains (software, shamir, tpm2, pkcs11,
// awskms, gcpkms, azurekv, vault) are constructed by the lifecycle layer and
// passed to seal.NewStorageBarrier; this manager exposes the resulting
// barrier through the interfaces.BarrierManager surface so that REST/gRPC
// service code can drive seal lifecycle operations without taking a direct
// dependency on the underlying qrdb seal package types.
package barrier

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

const (
	// StrategyRecoveryKeys is the strategy name for using recovery keys when
	// generating a root token via the StorageBarrier.
	StrategyRecoveryKeys = "recovery_keys"

	// StrategyShamirShares is the strategy name for using shamir shares when
	// generating a root token via the StorageBarrier.
	StrategyShamirShares = "shamir_shares"
)

// Config holds barrier manager configuration. The real cryptographic
// strategy chain lives on the underlying StorageBarrier; this struct only
// records the operator-visible Shamir parameters so that Status responses
// can echo them and Initialize/Rekey calls can default to them when the
// caller does not supply explicit values.
type Config struct {
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
}

// DefaultConfig returns a default barrier configuration.
func DefaultConfig() *Config {
	return &Config{
		Threshold:           2,
		TotalShares:         3,
		RecoveryKeysEnabled: true,
		RecoveryThreshold:   3,
		RecoveryTotalKeys:   5,
	}
}

// Compile-time interface check.
var _ interfaces.BarrierManager = (*Manager)(nil)

// BarrierNilError is returned when a nil StorageBarrier is provided to the constructor.
type BarrierNilError struct{}

// Error implements the error interface.
func (e *BarrierNilError) Error() string {
	return "barrier.Manager: storage barrier must not be nil"
}

// LoggerNilError is returned when a nil logger is provided to the constructor.
type LoggerNilError struct{}

// Error implements the error interface.
func (e *LoggerNilError) Error() string {
	return "barrier.Manager: logger must not be nil"
}

// InvalidStrategyError is returned when an unrecognized root token generation
// strategy is provided.
type InvalidStrategyError struct {
	Strategy string
}

// Error implements the error interface.
func (e *InvalidStrategyError) Error() string {
	return "barrier.Manager: invalid strategy: " + e.Strategy
}

// ErrRotateRootKeyUnsupported is returned when the underlying barrier does
// not expose a root-key rotation primitive. The qrdb StorageBarrier rotates
// root keys as part of rekey/init flows rather than as a standalone op, so
// QuicBaaS surfaces this as unsupported until the upstream API lands.
var ErrRotateRootKeyUnsupported = errors.New("rotate root key: not supported by this barrier backend")

// Manager wraps a go-qrdb StorageBarrier and exposes it through the
// interfaces.BarrierManager surface. All cryptographic state lives on the
// embedded barrier; Manager performs no key handling of its own.
type Manager struct {
	barrier *qrdbsdk.StorageBarrier
	logger  interfaces.Logger
	config  *Config
	version atomic.Uint64
}

// NewManager creates a new Manager wrapping the provided StorageBarrier.
// If config is nil, DefaultConfig() is applied. The logger is required
// because seal/unseal operations are security-sensitive and must always
// emit operator-visible audit messages.
func NewManager(
	barrier *qrdbsdk.StorageBarrier,
	config *Config,
	logger interfaces.Logger,
) (*Manager, error) {
	if barrier == nil {
		return nil, &BarrierNilError{}
	}
	if logger == nil {
		return nil, &LoggerNilError{}
	}
	if config == nil {
		config = DefaultConfig()
	}
	return &Manager{
		barrier: barrier,
		logger:  logger,
		config:  config,
	}, nil
}

// SystemBarrier returns the underlying StorageBarrier so that the lifecycle
// layer can register tenant barriers against the same root. Callers must
// not hold the returned pointer past the manager's lifetime.
func (m *Manager) SystemBarrier() *qrdbsdk.StorageBarrier {
	return m.barrier
}

// Status returns the current barrier status as a *service.SealStatusResponse.
// The Mode field is populated with the active sealing strategy ID emitted by
// the underlying barrier (e.g. "software", "shamir", "tpm2"). Returning the
// strategy through the Mode field preserves the existing service-layer DTO
// contract while removing the manual/auto Seal abstraction.
func (m *Manager) Status(_ context.Context) (interface{}, error) {
	status := m.barrier.Status()
	return &service.SealStatusResponse{
		Sealed:      status.Sealed,
		Initialized: !status.InitializedAt.IsZero(),
		Mode:        string(status.Strategy),
		Threshold:   m.config.Threshold,
		TotalShares: m.config.TotalShares,
		Version:     m.version.Load(),
		Timestamp:   time.Now(),
	}, nil
}

// IsSealed returns true if the barrier is currently sealed.
func (m *Manager) IsSealed() bool {
	return m.barrier.IsSealed()
}

// IsInitialized returns true if the barrier has been initialized.
func (m *Manager) IsInitialized() bool {
	return !m.barrier.Status().InitializedAt.IsZero()
}

// Initialize sets up the barrier using Shamir secret sharing and returns a
// *service.SealInitResponse. Share material is never returned (qrdb
// share-free contract): shares are sealed to enrolled Security Officers via
// the CeremonyDistributor and claimed individually through the share-claim
// flow. If shares or threshold are zero, the values from the manager's
// Config are used.
func (m *Manager) Initialize(ctx context.Context, shares, threshold int) (interface{}, error) {
	if threshold <= 0 {
		threshold = m.config.Threshold
	}
	if shares <= 0 {
		shares = m.config.TotalShares
	}

	m.barrier.SetShamirConfig(&qrdbsdk.ShamirConfig{
		Threshold:   threshold,
		TotalShares: shares,
	})

	if _, err := m.barrier.InitializeShamir(ctx, qrdbsdk.Credentials{}); err != nil {
		return nil, err
	}

	m.config.Threshold = threshold
	m.config.TotalShares = shares
	m.version.Store(1)

	if err := m.barrier.Seal(); err != nil {
		return nil, err
	}

	m.logger.Infof("Barrier initialized with threshold=%d, total=%d", threshold, shares)

	return &service.SealInitResponse{
		RootToken: "",
		Version:   1,
		Timestamp: time.Now(),
	}, nil
}

// Unseal submits a single Shamir share toward the quorum threshold and
// returns a *service.UnsealResponse with progress information.
func (m *Manager) Unseal(ctx context.Context, share []byte) (interface{}, error) {
	progress, err := m.barrier.UnsealWithShare(ctx, string(share))
	if err != nil {
		return nil, err
	}

	return &service.UnsealResponse{
		Sealed:         m.barrier.IsSealed(),
		SharesProvided: progress.Submitted,
		Threshold:      progress.Required,
		Timestamp:      time.Now(),
	}, nil
}

// Seal re-seals the barrier, clearing cryptographic material from memory.
func (m *Manager) Seal(_ context.Context) error {
	m.logger.Info("Sealing barrier")
	return m.barrier.Seal()
}

// Rekey generates new Shamir shares with potentially different parameters
// and returns a *service.RekeyResponse. New share material is never
// returned (qrdb share-free contract): shares are sealed to enrolled
// Security Officers and claimed via the share-claim flow. If shares or
// threshold are zero, the values from the manager's Config are used.
func (m *Manager) Rekey(ctx context.Context, shares, threshold int) (interface{}, error) {
	if threshold <= 0 {
		threshold = m.config.Threshold
	}
	if shares <= 0 {
		shares = m.config.TotalShares
	}

	if _, err := m.barrier.Rekey(ctx, threshold, shares); err != nil {
		return nil, err
	}

	m.config.Threshold = threshold
	m.config.TotalShares = shares
	newVersion := m.version.Add(1)

	m.logger.Infof("Rekeyed barrier with threshold=%d, total=%d", threshold, shares)

	return &service.RekeyResponse{
		Version:   newVersion,
		Timestamp: time.Now(),
	}, nil
}

// GenerateRecoveryKeys generates Shamir recovery shares via the underlying
// barrier. Recovery share material is never returned (qrdb share-free
// contract): shares are sealed to enrolled recovery officers and claimed
// via the recovery share-claim flow, so the returned slice is always empty.
func (m *Manager) GenerateRecoveryKeys(ctx context.Context, threshold, totalKeys int) ([][]byte, error) {
	if _, err := m.barrier.GenerateRecoveryKeys(ctx, threshold, totalKeys); err != nil {
		return nil, err
	}

	m.logger.Infof("Generated %d recovery keys with threshold %d", totalKeys, threshold)
	return [][]byte{}, nil
}

// DeleteRecoveryKeys removes recovery key metadata from the underlying
// barrier. When the barrier is sealed (and therefore cannot have any
// recovery metadata that requires deletion), this is treated as a
// successful no-op so callers can clear recovery state idempotently
// without unsealing first.
func (m *Manager) DeleteRecoveryKeys(ctx context.Context) error {
	m.logger.Warn("Deleting recovery keys")
	if err := m.barrier.DeleteRecoveryKeys(ctx); err != nil {
		if errors.Is(err, qrdbsdk.ErrSealed) {
			return nil
		}
		return err
	}
	return nil
}

// GenerateRootToken generates a root token by proving knowledge of the
// master key through shares. The strategy parameter selects between
// "recovery_keys" and "shamir_shares".
func (m *Manager) GenerateRootToken(ctx context.Context, strategy string, shares [][]byte) (string, error) {
	if strategy != StrategyRecoveryKeys && strategy != StrategyShamirShares {
		return "", &InvalidStrategyError{Strategy: strategy}
	}

	shareStrings := make([]string, len(shares))
	for i, s := range shares {
		shareStrings[i] = string(s)
	}

	tok, err := m.barrier.GenerateRootToken(ctx, shareStrings)
	if err != nil {
		return "", err
	}
	return tok.Token, nil
}

// RotateRootKey rotates the root encryption key. The current qrdb barrier
// rotates the root key as part of rekey/init rather than exposing a
// standalone primitive, so this method returns ErrRotateRootKeyUnsupported.
func (m *Manager) RotateRootKey(_ context.Context) error {
	return ErrRotateRootKeyUnsupported
}

// Close releases resources. The underlying barrier's lifecycle is owned by
// the lifecycle layer that constructed it, so this is a no-op beyond
// emitting an audit log entry.
func (m *Manager) Close() error {
	m.logger.Info("Closing barrier manager")
	return nil
}
