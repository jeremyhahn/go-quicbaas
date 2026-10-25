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

// init_marker_test.go verifies that the barrier strategy persisted by
// `quicbaasd init` into the data dir's .quicbaasd_init_done marker is honored
// by the daemon at start: resolveBarrierStrategy reads it, and
// buildDataPlaneBarrierService builds the matching qrdb StorageBarrier.

package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// writeTestInitMarker drops a .quicbaasd_init_done marker into dataDir with the
// supplied barrier strategy (and shamir threshold/shares). It mirrors the JSON
// shape written by cmd/quicbaasd.writeInitDoneMarker so the resolution code
// under test reads exactly what init persists.
func writeTestInitMarker(t *testing.T, dataDir, strategy string, threshold, shares int) {
	t.Helper()
	marker := map[string]any{
		"barrier_strategy":    strategy,
		"credential_strategy": "barrier",
		"threshold":           threshold,
		"shares":              shares,
		"completed_at":        "2026-05-30T00:00:00Z",
	}
	raw, err := json.MarshalIndent(marker, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dataDir, initDoneMarkerName), raw, 0o600))
}

// ---------------------------------------------------------------------------
// readInitMarker
// ---------------------------------------------------------------------------

// TestReadInitMarker_Missing returns (nil, nil) for a data dir with no marker
// so a fresh, never-initialised data dir falls back to the config default.
func TestReadInitMarker_Missing(t *testing.T) {
	marker, err := readInitMarker(t.TempDir())
	require.NoError(t, err)
	assert.Nil(t, marker)
}

// TestReadInitMarker_EmptyDataDir returns (nil, nil) when no data dir is set.
func TestReadInitMarker_EmptyDataDir(t *testing.T) {
	marker, err := readInitMarker("")
	require.NoError(t, err)
	assert.Nil(t, marker)
}

// TestReadInitMarker_Valid parses a well-formed marker.
func TestReadInitMarker_Valid(t *testing.T) {
	dir := t.TempDir()
	writeTestInitMarker(t, dir, "shamir", 2, 3)

	marker, err := readInitMarker(dir)
	require.NoError(t, err)
	require.NotNil(t, marker)
	assert.Equal(t, "shamir", marker.BarrierStrategy)
	assert.Equal(t, 2, marker.Threshold)
	assert.Equal(t, 3, marker.Shares)
}

// TestReadInitMarker_Malformed surfaces a DataPlaneBarrierError for invalid
// JSON rather than silently falling through to the wrong strategy.
func TestReadInitMarker_Malformed(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, initDoneMarkerName), []byte("{not json"), 0o600))

	marker, err := readInitMarker(dir)
	require.Error(t, err)
	assert.Nil(t, marker)
	var dpErr *DataPlaneBarrierError
	require.ErrorAs(t, err, &dpErr)
	assert.Equal(t, "parse init marker", dpErr.Stage)
}

// ---------------------------------------------------------------------------
// resolveBarrierStrategy
// ---------------------------------------------------------------------------

// TestResolveBarrierStrategy_NoMarkerUsesConfigDefault verifies a fresh data
// dir with no marker resolves to the config default ("passphrase").
func TestResolveBarrierStrategy_NoMarkerUsesConfigDefault(t *testing.T) {
	cfg := config.Default()
	cfg.Storage.DataDir = t.TempDir()

	strategy, _, _, _, err := resolveBarrierStrategy(cfg, false)
	require.NoError(t, err)
	assert.Equal(t, qrdbsdk.StrategyPassphrase, strategy)
}

// TestResolveBarrierStrategy_MarkerNoneWins verifies an init marker recording
// "none" overrides the "passphrase" config default when no operator override is
// present. This is the core fix: init --barrier-strategy none then start.
func TestResolveBarrierStrategy_MarkerNoneWins(t *testing.T) {
	cfg := config.Default()
	cfg.Storage.DataDir = t.TempDir()
	writeTestInitMarker(t, cfg.Storage.DataDir, "none", 0, 0)

	strategy, _, _, _, err := resolveBarrierStrategy(cfg, false)
	require.NoError(t, err)
	assert.Equal(t, qrdbsdk.StrategyNone, strategy)
}

// TestResolveBarrierStrategy_MarkerDisabledAliasMapsToNone verifies the init
// "disabled" alias resolves to the canonical none strategy.
func TestResolveBarrierStrategy_MarkerDisabledAliasMapsToNone(t *testing.T) {
	cfg := config.Default()
	cfg.Storage.DataDir = t.TempDir()
	writeTestInitMarker(t, cfg.Storage.DataDir, "disabled", 0, 0)

	strategy, _, _, _, err := resolveBarrierStrategy(cfg, false)
	require.NoError(t, err)
	assert.Equal(t, qrdbsdk.StrategyNone, strategy)
}

// TestResolveBarrierStrategy_MarkerPassphraseWins verifies a passphrase init
// marker resolves to the passphrase strategy.
func TestResolveBarrierStrategy_MarkerPassphraseWins(t *testing.T) {
	cfg := config.Default()
	cfg.Storage.DataDir = t.TempDir()
	writeTestInitMarker(t, cfg.Storage.DataDir, "passphrase", 0, 0)

	strategy, _, _, _, err := resolveBarrierStrategy(cfg, false)
	require.NoError(t, err)
	assert.Equal(t, qrdbsdk.StrategyPassphrase, strategy)
}

// TestResolveBarrierStrategy_MarkerShamirCarriesThresholdAndShares verifies the
// shamir init marker resolves to the shamir strategy AND carries the persisted
// M-of-N threshold/total so the data-plane barrier registers the same shamir
// configuration the init ceremony recorded.
func TestResolveBarrierStrategy_MarkerShamirCarriesThresholdAndShares(t *testing.T) {
	cfg := config.Default()
	cfg.Storage.DataDir = t.TempDir()
	// Use values that differ from DefaultSealConfig (2/3) to prove they flow
	// from the marker, not the config default.
	writeTestInitMarker(t, cfg.Storage.DataDir, "shamir", 3, 5)

	strategy, threshold, total, _, err := resolveBarrierStrategy(cfg, false)
	require.NoError(t, err)
	assert.Equal(t, qrdbsdk.StrategyShamir, strategy)
	assert.Equal(t, 3, threshold)
	assert.Equal(t, 5, total)
}

// TestResolveBarrierStrategy_ExplicitOverrideWins verifies that when the
// operator explicitly supplied a seal strategy (operatorOverride=true), the
// config strategy wins over the persisted init marker.
func TestResolveBarrierStrategy_ExplicitOverrideWins(t *testing.T) {
	cfg := config.Default()
	cfg.Storage.DataDir = t.TempDir()
	cfg.Seal.Strategy = "passphrase"
	// Marker says none, but operator override pins passphrase.
	writeTestInitMarker(t, cfg.Storage.DataDir, "none", 0, 0)

	strategy, _, _, _, err := resolveBarrierStrategy(cfg, true)
	require.NoError(t, err)
	assert.Equal(t, qrdbsdk.StrategyPassphrase, strategy,
		"explicit operator override must win over the persisted init marker")
}

// TestBuildQRDBConfig_ShamirMarkerEnablesNodeSeal is the regression test for the
// gap that would otherwise swallow the whole M-of-N ceremony.
//
// `quicbaasd init --barrier-strategy shamir` deliberately initializes no
// barrier — go-qrdb's bootstrap orchestrator reserves that for the ceremony
// ("Shamir barrier initialization is the ceremony's job (multiple SOs), never
// inline"). The node must therefore boot SEALED so its sealed window can serve
// officer registration and the ceremony. Resolving only the STRATEGY from the
// marker was not enough: seal.enabled came from config alone, so a data dir
// initialized for shamir booted UNSEALED, skipped the sealed window, and ran
// first-boot against an unprotected data plane, silently ignoring the
// operator's chosen strategy.
func TestBuildQRDBConfig_ShamirMarkerEnablesNodeSeal(t *testing.T) {
	cfg := config.Default()
	cfg.Storage.DataDir = t.TempDir()
	cfg.Seal.Enabled = false // config says nothing; the marker is the record
	writeTestInitMarker(t, cfg.Storage.DataDir, "shamir", 2, 3)

	qcfg := mustBuildQRDBConfig(t, cfg, false)

	assert.True(t, qcfg.Seal.Enabled,
		"a shamir init marker must bring the node up sealed, or the ceremony never runs")
	assert.Equal(t, "shamir", qcfg.Seal.Strategy)
	assert.Equal(t, 2, qcfg.Seal.ShamirThreshold)
	assert.Equal(t, 3, qcfg.Seal.ShamirTotal)
}

// TestBuildQRDBConfig_NoMarkerLeavesSealDisabled pins the other side of that
// rule. The config default strategy is itself "passphrase", so keying the
// decision off a non-empty resolved strategy would seal every node whose
// config never asked for a barrier. Only a persisted init decision enables it.
func TestBuildQRDBConfig_NoMarkerLeavesSealDisabled(t *testing.T) {
	cfg := config.Default()
	cfg.Storage.DataDir = t.TempDir() // fresh dir: no marker
	cfg.Seal.Enabled = false

	qcfg := mustBuildQRDBConfig(t, cfg, false)

	assert.False(t, qcfg.Seal.Enabled,
		"an uninitialized data dir must not silently boot sealed")
}

// TestBuildQRDBConfig_NoneMarkerLeavesSealDisabled verifies the "none" strategy
// is exempt: it is the explicit choice to run without a barrier.
func TestBuildQRDBConfig_NoneMarkerLeavesSealDisabled(t *testing.T) {
	cfg := config.Default()
	cfg.Storage.DataDir = t.TempDir()
	cfg.Seal.Enabled = false
	writeTestInitMarker(t, cfg.Storage.DataDir, "none", 0, 0)

	qcfg := mustBuildQRDBConfig(t, cfg, false)

	assert.False(t, qcfg.Seal.Enabled,
		"--barrier-strategy none must not bring up a barrier")
}

// TestResolveBarrierStrategy_MalformedMarkerSurfacesError verifies a malformed
// marker aborts resolution with a typed error rather than silently defaulting.
func TestResolveBarrierStrategy_MalformedMarkerSurfacesError(t *testing.T) {
	cfg := config.Default()
	cfg.Storage.DataDir = t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(cfg.Storage.DataDir, initDoneMarkerName), []byte("{bad"), 0o600))

	_, _, _, _, err := resolveBarrierStrategy(cfg, false)
	require.Error(t, err)
	var dpErr *DataPlaneBarrierError
	require.ErrorAs(t, err, &dpErr)
}

// ---------------------------------------------------------------------------
// buildDataPlaneBarrierService honoring the persisted init marker
// ---------------------------------------------------------------------------

// TestBuildDataPlaneBarrierService_MarkerNoneRoundTrip is the end-to-end proof
// of the fix: a data dir whose init marker says "none" (and a config whose
// Seal.Strategy is the "passphrase" default with no explicit override) builds a
// NoneStrategy barrier. The NoneStrategy passthrough accepts an EMPTY secret on
// qrdb init and unseals. The passphrase DeferredSoftwareStrategy would reject the
// empty secret, so a successful empty-secret init proves the none strategy is
// the one actually wired.
func TestBuildDataPlaneBarrierService_MarkerNoneRoundTrip(t *testing.T) {
	cfg := config.Default()
	cfg.Storage.DataDir = t.TempDir()
	// Config default is "passphrase"; no explicit override.
	require.Equal(t, "passphrase", cfg.Seal.Strategy)
	cfg.Seal.StrategyExplicit = false
	cfg.IAM.SPIFFETrustDomain = testTrustDomain
	writeTestInitMarker(t, cfg.Storage.DataDir, "none", 0, 0)

	svc, _, ceremonyCore, closer, err := buildDataPlaneBarrierService(cfg, nil)
	require.NoError(t, err)
	require.NotNil(t, svc)
	require.NotNil(t, closer)
	defer func() { require.NoError(t, closer.Close()) }()

	ctx := t.Context()
	client := dataPlaneCeremonyClient(t, svc, ceremonyCore)

	// Empty-secret qrdb init succeeds ONLY for the none passthrough strategy.
	require.NoError(t, runQRDBInit(t, client, nil),
		"none strategy must accept an empty secret; software would reject it")

	info, statusErr := svc.BarrierStatus(ctx)
	require.NoError(t, statusErr)
	assert.False(t, info.Sealed, "barrier must be unsealed after none-strategy init")
}

// TestBuildDataPlaneBarrierService_ExplicitOverrideBeatsMarker verifies that an
// explicit operator override (cfg.Seal.StrategyExplicit=true, Strategy=passphrase)
// wins over a none marker: the passphrase DeferredSoftwareStrategy REJECTS an
// empty-secret init, proving the passphrase strategy was wired despite the marker.
func TestBuildDataPlaneBarrierService_ExplicitOverrideBeatsMarker(t *testing.T) {
	cfg := config.Default()
	cfg.Storage.DataDir = t.TempDir()
	cfg.Seal.Strategy = "passphrase"
	cfg.Seal.StrategyExplicit = true
	writeTestInitMarker(t, cfg.Storage.DataDir, "none", 0, 0)

	svc, _, ceremonyCore, closer, err := buildDataPlaneBarrierService(cfg, nil)
	require.NoError(t, err)
	require.NotNil(t, svc)
	require.NotNil(t, closer)
	defer func() { require.NoError(t, closer.Close()) }()

	client := dataPlaneCeremonyClient(t, svc, ceremonyCore)

	// Passphrase strategy rejects an empty secret — proves it, not none, is wired.
	require.Error(t, runQRDBInit(t, client, nil),
		"passphrase strategy must reject an empty secret")
}

// TestBuildDataPlaneBarrierService_MarkerShamirBuilds verifies a shamir marker
// produces a shamir-configured barrier (status reachable, sealed until shares
// are submitted). This proves the shamir threshold/total flow from the marker
// into the StorageBarrier's ShamirConfig without reimplementing the ceremony.
func TestBuildDataPlaneBarrierService_MarkerShamirBuilds(t *testing.T) {
	cfg := config.Default()
	cfg.Storage.DataDir = t.TempDir()
	cfg.Seal.StrategyExplicit = false
	writeTestInitMarker(t, cfg.Storage.DataDir, "shamir", 2, 3)

	svc, _, _, closer, err := buildDataPlaneBarrierService(cfg, nil)
	require.NoError(t, err)
	require.NotNil(t, svc)
	require.NotNil(t, closer)
	defer func() { require.NoError(t, closer.Close()) }()

	info, statusErr := svc.BarrierStatus(t.Context())
	require.NoError(t, statusErr)
	assert.True(t, info.Sealed, "shamir barrier starts sealed until shares are submitted")
}

// TestReadInitMarker_ReadErrorNotMissing covers the read-error-that-is-not-
// ErrNotExist branch of readInitMarker: when the marker path is a directory,
// os.ReadFile fails with a non-ENOENT error that must surface as a typed
// DataPlaneBarrierError (rather than being treated as "no marker").
func TestReadInitMarker_ReadErrorNotMissing(t *testing.T) {
	dir := t.TempDir()
	// Make the marker name a directory so ReadFile fails with EISDIR.
	require.NoError(t, os.Mkdir(filepath.Join(dir, initDoneMarkerName), 0o750))

	marker, err := readInitMarker(dir)
	require.Error(t, err)
	assert.Nil(t, marker)
	var dpErr *DataPlaneBarrierError
	require.ErrorAs(t, err, &dpErr)
	assert.Equal(t, "read init marker", dpErr.Stage)
}

// TestResolveBarrierStrategy_EmptyStrategyDefaultsToPassphrase covers the
// empty-strategy default branch: an empty cfg.Seal.Strategy resolves to
// "passphrase" before the override short-circuit returns it.
func TestResolveBarrierStrategy_EmptyStrategyDefaultsToPassphrase(t *testing.T) {
	cfg := config.Default()
	cfg.Storage.DataDir = t.TempDir()
	cfg.Seal.Strategy = "" // force the empty-default branch

	// operatorOverride=true short-circuits the marker lookup so the defaulted
	// strategy is returned directly.
	strategy, _, _, _, err := resolveBarrierStrategy(cfg, true)
	require.NoError(t, err)
	assert.Equal(t, qrdbsdk.StrategyPassphrase, strategy)
}
