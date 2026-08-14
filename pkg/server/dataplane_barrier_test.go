//go:build !integration

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

// dataplane_barrier_test.go exercises buildDataPlaneBarrierService and
// buildSealStrategies. Two groups of tests are provided:
//
//  1. Unit tests for buildSealStrategies — verify that each strategy name
//     returns the correct concrete strategy type without touching the
//     filesystem.
//
//  2. Integration-style tests for buildDataPlaneBarrierService — open a real
//     PebbleDB store under a temp directory, assert that the returned service
//     is non-nil and the closer releases the file lock.

package server

import (
	"os"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// ---------------------------------------------------------------------------
// buildSealStrategies unit tests
// ---------------------------------------------------------------------------

// TestBuildSealStrategies_None verifies that StrategyNone returns exactly
// one NoneStrategy and no error.
func TestBuildSealStrategies_None(t *testing.T) {
	strats, err := buildSealStrategies(qrdbsdk.StrategyNone, 0, 0)
	require.NoError(t, err)
	require.Len(t, strats, 1)
	assert.Equal(t, qrdbsdk.StrategyNone, strats[0].ID())
}

// TestBuildSealStrategies_Software verifies that StrategyPassphrase returns
// exactly one deferred-software strategy and no error.
func TestBuildSealStrategies_Software(t *testing.T) {
	strats, err := buildSealStrategies(qrdbsdk.StrategyPassphrase, 0, 0)
	require.NoError(t, err)
	require.Len(t, strats, 1)
	assert.Equal(t, qrdbsdk.StrategyPassphrase, strats[0].ID())
}

// TestBuildSealStrategies_Shamir verifies that StrategyShamir returns a
// ShamirStrategy with valid threshold/total configuration and no error.
func TestBuildSealStrategies_Shamir(t *testing.T) {
	strats, err := buildSealStrategies(qrdbsdk.StrategyShamir, 2, 3)
	require.NoError(t, err)
	require.Len(t, strats, 1)
	assert.Equal(t, qrdbsdk.StrategyShamir, strats[0].ID())
}

// TestBuildSealStrategies_UnknownFallsToSoftware verifies that an unrecognised
// strategy name falls through to the deferred-software strategy (safe default).
func TestBuildSealStrategies_UnknownFallsToSoftware(t *testing.T) {
	strats, err := buildSealStrategies(qrdbsdk.StrategyID("tpm2"), 0, 0)
	require.NoError(t, err)
	require.Len(t, strats, 1)
	// hardware strategies fall back to software in this builder; the test
	// simply asserts the strategy ID is software (the safe default).
	assert.Equal(t, qrdbsdk.StrategyPassphrase, strats[0].ID())
}

// ---------------------------------------------------------------------------
// buildDataPlaneBarrierService unit tests
// ---------------------------------------------------------------------------

// TestBuildDataPlaneBarrierService_NilCfg returns (nil, nil, nil) for a nil
// config so callers do not need to guard against nil before calling.
func TestBuildDataPlaneBarrierService_NilCfg(t *testing.T) {
	svc, barrier, _, closer, err := buildDataPlaneBarrierService(nil, nil)
	assert.Nil(t, svc)
	assert.Nil(t, barrier)
	assert.Nil(t, closer)
	assert.NoError(t, err)
}

// TestBuildDataPlaneBarrierService_SealDisabledStillBuilds verifies the real
// qrdb StorageBarrier is constructed even when cfg.Seal.Enabled is false, as
// long as a persistent data dir exists. cfg.Seal.Enabled only governs
// auto-init/encryption of the data plane at startup; the barrier SERVICE that
// backs the `barrier` CLI verbs must always be the real qrdb barrier (never
// the in-memory bootstrap fallback) so operators can drive its lifecycle.
func TestBuildDataPlaneBarrierService_SealDisabledStillBuilds(t *testing.T) {
	cfg := config.Default()
	cfg.Seal.Enabled = false
	cfg.Storage.DataDir = t.TempDir()
	svc, barrier, _, closer, err := buildDataPlaneBarrierService(cfg, nil)
	assert.NoError(t, err)
	assert.NotNil(t, svc, "data-plane barrier must be built regardless of seal.enabled")
	assert.NotNil(t, barrier, "raw StorageBarrier must be returned alongside the service")
	require.NotNil(t, closer)
	assert.NoError(t, closer.Close())
}

// TestBuildDataPlaneBarrierService_EmptyDataDir returns (nil, nil, nil) when
// cfg.Storage.DataDir is empty — we cannot open a PebbleDB without a path.
func TestBuildDataPlaneBarrierService_EmptyDataDir(t *testing.T) {
	cfg := config.Default()
	cfg.Seal.Enabled = true
	cfg.Storage.DataDir = ""
	svc, barrier, _, closer, err := buildDataPlaneBarrierService(cfg, nil)
	assert.Nil(t, svc)
	assert.Nil(t, barrier)
	assert.Nil(t, closer)
	assert.NoError(t, err)
}

// TestBuildDataPlaneBarrierService_PassphraseStrategy verifies that a real
// PebbleDB-backed StorageBarrier is constructed with the passphrase strategy
// when cfg.Seal.Strategy == "passphrase". The returned service must be non-nil,
// the closer must release the PebbleDB lock when called.
func TestBuildDataPlaneBarrierService_PassphraseStrategy(t *testing.T) {
	dir := t.TempDir()

	cfg := config.Default()
	cfg.Seal.Enabled = true
	cfg.Seal.Strategy = "passphrase"
	cfg.Storage.DataDir = dir

	svc, barrier, _, closer, err := buildDataPlaneBarrierService(cfg, nil)
	require.NoError(t, err)
	require.NotNil(t, svc)
	require.NotNil(t, barrier, "raw StorageBarrier must be returned")
	require.NotNil(t, closer)

	// Service must respond to BarrierStatus without panicking.
	// The barrier starts sealed (not initialized); Status must return a valid
	// status struct indicating sealed=true.
	ctx := t.Context()
	info, statusErr := svc.BarrierStatus(ctx)
	require.NoError(t, statusErr)
	assert.True(t, info.Sealed)

	// Release the PebbleDB file lock.
	require.NoError(t, closer.Close())
}

// TestBuildDataPlaneBarrierService_SealedStatusReportsConfiguredStrategy
// verifies the strategyAwareBarrierService decorator: a freshly-constructed,
// still-sealed barrier reports the CONFIGURED strategy (not an empty string),
// even though qrdb's underlying StorageBarrier only activates a strategy at
// init/unseal time. This is the contract the multi-transport `barrier status`
// CLI relies on (mirrors Vault showing "Seal Type" on a sealed vault).
func TestBuildDataPlaneBarrierService_SealedStatusReportsConfiguredStrategy(t *testing.T) {
	cases := []struct {
		name     string
		strategy string
		want     string
	}{
		{"passphrase", "passphrase", string(qrdbsdk.StrategyPassphrase)},
		{"none", "none", string(qrdbsdk.StrategyNone)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.Default()
			cfg.Seal.Enabled = true
			cfg.Seal.Strategy = tc.strategy
			cfg.Storage.DataDir = t.TempDir()

			svc, _, _, closer, err := buildDataPlaneBarrierService(cfg, nil)
			require.NoError(t, err)
			require.NotNil(t, svc)
			require.NotNil(t, closer)
			defer func() { assert.NoError(t, closer.Close()) }()

			info, statusErr := svc.BarrierStatus(t.Context())
			require.NoError(t, statusErr)
			// Barrier is sealed (not initialised) yet must still name its strategy.
			assert.True(t, info.Sealed, "barrier must start sealed")
			assert.Equal(t, tc.want, info.Strategy,
				"sealed barrier must report its configured strategy")
		})
	}
}

// TestBuildDataPlaneBarrierService_NoneStrategy verifies that StrategyNone
// is accepted and returns a valid, non-nil service.
func TestBuildDataPlaneBarrierService_NoneStrategy(t *testing.T) {
	dir := t.TempDir()

	cfg := config.Default()
	cfg.Seal.Enabled = true
	cfg.Seal.Strategy = "none"
	cfg.Storage.DataDir = dir

	svc, barrier, _, closer, err := buildDataPlaneBarrierService(cfg, nil)
	require.NoError(t, err)
	require.NotNil(t, svc)
	require.NotNil(t, barrier)
	require.NotNil(t, closer)

	// NoneStrategy barrier starts sealed until initialized.
	ctx := t.Context()
	info, statusErr := svc.BarrierStatus(ctx)
	require.NoError(t, statusErr)
	assert.True(t, info.Sealed)

	require.NoError(t, closer.Close())
}

// TestBuildDataPlaneBarrierService_PersistenceAcrossOpen verifies that state
// written to the seal sub-store in one call survives across a Close + reopen.
// After Initialize (with none strategy), a second call to
// buildDataPlaneBarrierService should see the barrier as already initialized.
func TestBuildDataPlaneBarrierService_PersistenceAcrossOpen(t *testing.T) {
	dir := t.TempDir()

	cfg := config.Default()
	cfg.Seal.Enabled = true
	cfg.Seal.Strategy = "none"
	cfg.Storage.DataDir = dir

	ctx := t.Context()

	// --- First open: initialize the barrier. ---
	svc1, _, _, closer1, err := buildDataPlaneBarrierService(cfg, nil)
	require.NoError(t, err)
	require.NotNil(t, svc1)

	// Initialize with empty secret (None strategy ignores credentials).
	initErr := svc1.BarrierInitialize(ctx, "")
	require.NoError(t, initErr)

	// Barrier is now unsealed after initialization.
	info1, statusErr1 := svc1.BarrierStatus(ctx)
	require.NoError(t, statusErr1)
	assert.False(t, info1.Sealed)

	// Close the first PebbleDB instance.
	require.NoError(t, closer1.Close())

	// --- Second open: verify persistence. ---
	svc2, _, _, closer2, err2 := buildDataPlaneBarrierService(cfg, nil)
	require.NoError(t, err2)
	require.NotNil(t, svc2)

	// After reopening, the barrier is sealed (root key must be loaded via Unseal).
	// But BarrierInitialize must now return ErrAlreadyInitialized, proving the
	// root-key blob survived the PebbleDB round-trip.
	initErr2 := svc2.BarrierInitialize(ctx, "")
	assert.Error(t, initErr2, "expected ErrAlreadyInitialized on second init")

	require.NoError(t, closer2.Close())
}

// TestBuildDataPlaneBarrierService_InvalidDataDir returns a
// DataPlaneBarrierError when the seal directory cannot be created (e.g. the
// parent path points to an existing regular file).
func TestBuildDataPlaneBarrierService_InvalidDataDir(t *testing.T) {
	// Create a regular file where the seal dir would be created.
	f, err := os.CreateTemp("", "quicbaas-barrier-test-*")
	require.NoError(t, err)
	_ = f.Close()
	defer os.Remove(f.Name())

	cfg := config.Default()
	cfg.Seal.Enabled = true
	cfg.Seal.Strategy = "passphrase"
	// DataDir points to the regular file; seal/ cannot be created under it.
	cfg.Storage.DataDir = f.Name()

	svc, barrier, _, closer, err := buildDataPlaneBarrierService(cfg, nil)
	assert.Nil(t, svc)
	assert.Nil(t, barrier)
	assert.Nil(t, closer)
	// The error may be either a DataPlaneBarrierError (mkdir failed) or a
	// pebble open error; either way it must be non-nil.
	assert.Error(t, err)
}

// initDataPlaneEncryptor builds the data-plane barrier + encryptor from the
// config. With a real data dir the barrier is constructed and the encryptor is
// non-nil; with no data dir it falls back to a no-op encryptor.
func TestInitDataPlaneEncryptor_WithDataDir(t *testing.T) {
	cfg := config.Default()
	cfg.Storage.DataDir = t.TempDir()
	s := &Server{config: cfg, logger: buildTestLogger(t)}

	if err := s.initDataPlaneEncryptor(); err != nil {
		t.Fatalf("initDataPlaneEncryptor: %v", err)
	}
	if s.dataPlaneEncryptor == nil {
		t.Fatal("encryptor must be set")
	}
	if s.dataPlaneBarrier == nil {
		t.Fatal("data-plane barrier must be constructed with a data dir")
	}
	if s.dataPlaneBarrierCloser != nil {
		_ = s.dataPlaneBarrierCloser.Close()
	}
}

func TestInitDataPlaneEncryptor_NoDataDir(t *testing.T) {
	cfg := config.Default()
	cfg.Storage.DataDir = ""
	s := &Server{config: cfg, logger: buildTestLogger(t)}

	if err := s.initDataPlaneEncryptor(); err != nil {
		t.Fatalf("initDataPlaneEncryptor: %v", err)
	}
	// No data dir: no barrier, but a no-op encryptor is still installed.
	if s.dataPlaneEncryptor == nil {
		t.Fatal("a no-op encryptor must still be installed")
	}
	if s.dataPlaneBarrier != nil {
		t.Fatal("no barrier expected without a data dir")
	}
}
