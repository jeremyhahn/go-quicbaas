//go:build iam

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

// init_runtime_test.go exercises the real init flow against an embedded
// Server. The tests rely on the iam build tag because the persistence
// contract (admin assignment + invite + roles surviving a daemon restart)
// only holds when the IAM subsystem compiles in. All tests use t.TempDir()
// so they leave no host-side state behind.

package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/server"
	qbshards "github.com/jeremyhahn/go-quicbaas/pkg/shards"
)

// TestRunInlineInit_NoneStrategy_WorksWithoutPINs verifies that
// "--barrier-strategy none" does not require --so-pin / --user-pin and
// completes successfully against a fresh data dir.
func TestRunInlineInit_NoneStrategy_WorksWithoutPINs(t *testing.T) {
	dataDir := t.TempDir()
	f := &inlineInitFlags{
		barrierStrategy:    "none",
		credentialStrategy: "barrier",
		dataDir:            dataDir,
	}
	cmd := &cobra.Command{Use: "init"}
	if err := runInlineInit(cmd, f); err != nil {
		t.Fatalf("runInlineInit(none) error: %v", err)
	}
	markerPath := filepath.Join(dataDir, initDoneMarkerName)
	if _, err := os.Stat(markerPath); err != nil {
		t.Fatalf("expected init-done marker at %s: %v", markerPath, err)
	}
}

// TestRunInlineInit_NoneRejectsPINs verifies that supplying --so-pin or
// --user-pin under "--barrier-strategy none" surfaces PINsNotAllowedError.
// Silent ignoring would let operators believe their SO credential is in
// effect when the daemon has no barrier at all.
func TestRunInlineInit_NoneRejectsPINs(t *testing.T) {
	cases := []struct {
		name string
		f    *inlineInitFlags
	}{
		{"so-pin only", &inlineInitFlags{barrierStrategy: "none", credentialStrategy: "barrier", soPin: "x"}},
		{"user-pin only", &inlineInitFlags{barrierStrategy: "none", credentialStrategy: "barrier", userPin: "y"}},
		{"both pins", &inlineInitFlags{barrierStrategy: "none", credentialStrategy: "barrier", soPin: "x", userPin: "y"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.f.dataDir = t.TempDir()
			cmd := &cobra.Command{Use: "init"}
			// runInlineInit invokes validateInlineInitFlags which only
			// runs credential strategy validation. The PIN rejection lives
			// in resolveAndValidateCanonicalInit so we drive that path
			// directly through resolveAndValidateInlineInit.
			err := resolveAndValidateInlineInit(cmd, c.f, "QUICBAAS_TEST_NONE")
			if err == nil {
				t.Fatal("expected PINsNotAllowedError")
			}
			var typed *PINsNotAllowedError
			if !errors.As(err, &typed) {
				t.Fatalf("got %T (%v), want *PINsNotAllowedError", err, err)
			}
			if typed.Strategy != "none" {
				t.Errorf("Strategy: got %q, want %q", typed.Strategy, "none")
			}
		})
	}
}

// TestRunInlineInit_DisabledRejectsPINs mirrors the none-rejects-PINs
// check for the legacy "disabled" synonym so operators upgrading from
// older builds see the same explicit error.
func TestRunInlineInit_DisabledRejectsPINs(t *testing.T) {
	f := &inlineInitFlags{
		barrierStrategy:    "disabled",
		credentialStrategy: "barrier",
		soPin:              "supplied",
		dataDir:            t.TempDir(),
	}
	cmd := &cobra.Command{Use: "init"}
	err := resolveAndValidateInlineInit(cmd, f, "QUICBAAS_TEST_DISABLED")
	if err == nil {
		t.Fatal("expected PINsNotAllowedError")
	}
	var typed *PINsNotAllowedError
	if !errors.As(err, &typed) {
		t.Fatalf("got %T, want *PINsNotAllowedError", err)
	}
}

// TestRunInlineInit_HardwareStrategiesRejected verifies that every
// hardware-backed strategy is recognised on the CLI surface but rejected
// with UnsupportedBarrierStrategyError until its factory wiring lands.
func TestRunInlineInit_HardwareStrategiesRejected(t *testing.T) {
	hardware := []string{"pkcs11", "tpm2", "awskms", "gcpkms", "azurekv", "vault"}
	for _, strategy := range hardware {
		t.Run(strategy, func(t *testing.T) {
			f := &inlineInitFlags{
				barrierStrategy:    strategy,
				credentialStrategy: "barrier",
				soPin:              "so-pin-1234",
				userPin:            "user-pin-1234",
				dataDir:            t.TempDir(),
			}
			cmd := &cobra.Command{Use: "init"}
			err := resolveAndValidateInlineInit(cmd, f, "QUICBAAS_TEST_HW")
			if err == nil {
				t.Fatalf("expected UnsupportedBarrierStrategyError for %s", strategy)
			}
			var typed *UnsupportedBarrierStrategyError
			if !errors.As(err, &typed) {
				t.Fatalf("got %T (%v), want *UnsupportedBarrierStrategyError", err, err)
			}
			if typed.Strategy != strategy {
				t.Errorf("Strategy: got %q, want %q", typed.Strategy, strategy)
			}
		})
	}
}

// TestRunInlineInit_InvalidBarrierStrategyRejected verifies that a value
// outside the validBarrierStrategies map surfaces the canonical
// "invalid --barrier-strategy" error rather than the typed
// UnsupportedBarrierStrategyError. The two errors mean different things:
// invalid = "this is not a recognized name", unsupported = "name is known
// but factory wiring has not landed".
func TestRunInlineInit_InvalidBarrierStrategyRejected(t *testing.T) {
	f := &inlineInitFlags{
		barrierStrategy:    "phantom-strategy",
		credentialStrategy: "barrier",
	}
	cmd := &cobra.Command{Use: "init"}
	err := resolveAndValidateInlineInit(cmd, f, "QUICBAAS_TEST_BAD")
	if err == nil {
		t.Fatal("expected invalid --barrier-strategy error")
	}
	var typed *UnsupportedBarrierStrategyError
	if errors.As(err, &typed) {
		t.Fatalf("expected invalid-strategy error, got UnsupportedBarrierStrategyError")
	}
}

// TestRunInlineInit_InitDoneMarkerIdempotency drives the canonical
// idempotency contract: a second init against an already-initialised data
// dir returns InitAlreadyDoneError. The error references the data dir so
// operators have a single message that names the offending path.
func TestRunInlineInit_InitDoneMarkerIdempotency(t *testing.T) {
	dataDir := t.TempDir()
	f := &inlineInitFlags{
		barrierStrategy:    "none",
		credentialStrategy: "barrier",
		dataDir:            dataDir,
	}
	cmd := &cobra.Command{Use: "init"}
	if err := runInlineInit(cmd, f); err != nil {
		t.Fatalf("first runInlineInit error: %v", err)
	}
	err := runInlineInit(cmd, f)
	if err == nil {
		t.Fatal("expected InitAlreadyDoneError on second invocation")
	}
	var typed *InitAlreadyDoneError
	if !errors.As(err, &typed) {
		t.Fatalf("got %T (%v), want *InitAlreadyDoneError", err, err)
	}
	if typed.DataDir != dataDir {
		t.Errorf("DataDir: got %q, want %q", typed.DataDir, dataDir)
	}
}

// TestRunInlineInit_PersistenceRoundTrip verifies the end-to-end
// init → embedded server start → no-rebootstrap contract. After
// runInlineInit completes against a fresh data dir, opening the same
// data dir with a regular embedded Server start MUST NOT create a
// second bootstrap admin assignment.
//
// The first invocation runs through runInlineInit which constructs its
// own embedded Server, runs the IAM bootstrap, and shuts it down. The
// second invocation opens the same data dir with a fresh embedded Server
// (mirroring "quicbaasd start --data-dir ..."). The admin assignment
// count must remain at exactly one after the second start.
func TestRunInlineInit_PersistenceRoundTrip(t *testing.T) {
	dataDir := t.TempDir()
	f := &inlineInitFlags{
		barrierStrategy:    "passphrase",
		secret:             "test-barrier-secret-12345",
		soPin:              "so-pin-1234",
		userPin:            "user-pin-1234",
		credentialStrategy: "barrier",
		dataDir:            dataDir,
	}
	cmd := &cobra.Command{Use: "init"}
	if err := runInlineInit(cmd, f); err != nil {
		t.Fatalf("runInlineInit error: %v", err)
	}

	// Open the same data dir as a regular "start" would. The embedded
	// Server runs initializeIAMComponents which in turn runs
	// firstBootBootstrap; the admin assignment from the init phase
	// MUST be discovered so the second bootstrap is skipped.
	cfg := buildInitConfig(dataDir, f, true)
	// A real "start" against a passphrase barrier supplies the operator
	// passphrase via WithSealPassphrase so the qrdb node auto-unseals on
	// boot (matching runInlineInit's own server build). Without it the node
	// stays sealed and node.Client() is nil.
	srv, err := server.NewServer(cfg, server.WithSealPassphrase([]byte(f.secret)))
	require.NoError(t, err)
	require.NoError(t, srv.Start())
	defer func() {
		_ = srv.Shutdown(context.Background())
	}()

	// Reach into the persistent admin storage directly. We do NOT trust
	// the in-memory iamComponents because the contract under test is the
	// on-disk one: any future change that breaks persistence (e.g.
	// switching back to MemoryAdminStorage) MUST fail this assertion.
	adminStorage := iam.NewBackendAdminStorage(srv.GetKVStore(qbshards.PlatformShardID))
	assignments, err := adminStorage.ListAssignments(context.Background(), iam.ListAdminAssignmentsOptions{
		ActiveOnly: true,
		Limit:      10,
	})
	require.NoError(t, err)
	assert.Len(t, assignments, 1,
		"exactly one bootstrap admin assignment must exist after init+start; got %d (re-bootstrap fired)",
		len(assignments),
	)

	// The bootstrap admin lives in qrdb's canonical system tenant.
	assert.Equal(t, iam.AdminRoleSuperAdmin, assignments[0].AdminRole)
}

// TestRunInlineInit_ShamirWritesPendingState verifies the shamir branch
// of runInlineInit: pending_ceremony.json is written under the data dir
// with the requested officer + threshold + share configuration.
func TestRunInlineInit_ShamirWritesPendingState(t *testing.T) {
	tmp := t.TempDir()
	dataDir := filepath.Join(tmp, "data")
	csrPath := filepath.Join(tmp, "alice.csr.pem")
	certPath := filepath.Join(tmp, "bob.cert.pem")
	pubPath := filepath.Join(tmp, "carol.pub.pem")
	writeTestCSRFile(t, csrPath, "alice")
	writeTestCertFile(t, certPath, "bob")
	writeTestPubKeyFile(t, pubPath, "carol")

	f := &inlineInitFlags{
		barrierStrategy:    "shamir",
		soPin:              "so-pin-1234",
		userPin:            "user-pin-1234",
		credentialStrategy: "barrier",
		shares:             3,
		threshold:          2,
		officers: []string{
			"alice:csr:" + csrPath,
			"bob:cert:" + certPath,
			"carol:pubkey:" + pubPath,
		},
		dataDir: dataDir,
	}
	cmd := &cobra.Command{Use: "init"}
	if err := runInlineInit(cmd, f); err != nil {
		t.Fatalf("runInlineInit(shamir) error: %v", err)
	}

	statePath := filepath.Join(dataDir, pendingCeremonyFileName)
	raw, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read pending ceremony state: %v", err)
	}
	var state pendingCeremonyState
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatalf("unmarshal pending ceremony state: %v", err)
	}
	assert.Equal(t, "shamir", state.Strategy)
	assert.Equal(t, 3, state.Shares)
	assert.Equal(t, 2, state.Threshold)
	require.Len(t, state.Officers, 3)
	assert.Equal(t, "alice", state.Officers[0].Username)
	assert.Equal(t, "csr", state.Officers[0].Kind)
	assert.Equal(t, csrPath, state.Officers[0].Path)

	// Init-done marker MUST also be present so a second invocation is
	// rejected with InitAlreadyDoneError.
	if _, err := os.Stat(filepath.Join(dataDir, initDoneMarkerName)); err != nil {
		t.Fatalf("expected init-done marker after shamir init: %v", err)
	}
}

// TestRunInlineInit_ShamirRequiresThresholdAndShares verifies that
// supplying --barrier-strategy=shamir without valid --shares / --threshold
// fails at the canonical validation layer.
func TestRunInlineInit_ShamirRequiresThresholdAndShares(t *testing.T) {
	cases := []struct {
		name   string
		shares int
		thresh int
	}{
		{"missing shares", 0, 0},
		{"shares without threshold", 3, 0},
		{"threshold above shares", 3, 5},
		{"shares below 2", 1, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &inlineInitFlags{
				barrierStrategy:    "shamir",
				soPin:              "so-pin-1234",
				userPin:            "user-pin-1234",
				credentialStrategy: "barrier",
				shares:             c.shares,
				threshold:          c.thresh,
			}
			cmd := &cobra.Command{Use: "init"}
			if err := resolveAndValidateInlineInit(cmd, f, "QUICBAAS_TEST_SHAMIR"); err == nil {
				t.Fatal("expected shamir validation error")
			}
		})
	}
}

// TestPINsNotAllowedError_Message ensures the typed error includes the
// offending strategy so operators see precisely which mode rejected
// their PIN.
func TestPINsNotAllowedError_Message(t *testing.T) {
	e := &PINsNotAllowedError{Strategy: "none"}
	msg := e.Error()
	if msg == "" {
		t.Fatal("expected non-empty error message")
	}
	assert.Contains(t, msg, "none")
	assert.Contains(t, msg, "pin")
}

// TestUnsupportedBarrierStrategyError_Message ensures the typed error
// names the strategy and lists the supported alternatives.
func TestUnsupportedBarrierStrategyError_Message(t *testing.T) {
	e := &UnsupportedBarrierStrategyError{Strategy: "pkcs11"}
	msg := e.Error()
	if msg == "" {
		t.Fatal("expected non-empty error message")
	}
	assert.Contains(t, msg, "pkcs11")
	assert.Contains(t, msg, "passphrase")
	assert.Contains(t, msg, "shamir")
}

// TestInitAlreadyDoneError_Message ensures the error names the data dir
// and a recovery command.
func TestInitAlreadyDoneError_Message(t *testing.T) {
	e := &InitAlreadyDoneError{DataDir: "/var/lib/quicbaas"}
	msg := e.Error()
	assert.Contains(t, msg, "/var/lib/quicbaas")
	assert.Contains(t, msg, "quicbaasd start")
}

// TestBuildInitConfig_DisablesAllListeners verifies the config returned
// by buildInitConfig has every transport listener turned off so init
// cannot accidentally bind a public port.
func TestBuildInitConfig_DisablesAllListeners(t *testing.T) {
	f := &inlineInitFlags{barrierStrategy: "none"}
	cfg := buildInitConfig(t.TempDir(), f, false)

	assert.True(t, cfg.Server.Embedded, "init config must run in embedded mode")
	assert.False(t, cfg.Server.GRPC.Enabled, "init must not enable gRPC listener")
	assert.False(t, cfg.Server.REST.Enabled, "init must not enable REST listener")
	assert.False(t, cfg.Server.QUIC.Enabled, "init must not enable QUIC listener")
	assert.False(t, cfg.Server.MCP.Enabled, "init must not enable MCP listener")
	assert.False(t, cfg.Server.Unix.Enabled, "init must not enable Unix listener")
	assert.False(t, cfg.Security.TLSEnabled, "init must not require TLS")
	assert.Equal(t, config.ServerModeStandalone, cfg.Server.Mode,
		"init must run in standalone mode")
}

// TestBuildInitConfig_BarrierFlagTogglesSeal verifies the barrierEnabled
// argument flows through to cfg.Seal.Enabled. The "none" / "disabled"
// strategies must leave seal off; "software" must turn it on.
func TestBuildInitConfig_BarrierFlagTogglesSeal(t *testing.T) {
	f := &inlineInitFlags{barrierStrategy: "passphrase"}
	cfgOn := buildInitConfig(t.TempDir(), f, true)
	cfgOff := buildInitConfig(t.TempDir(), f, false)

	assert.True(t, cfgOn.Seal.Enabled, "barrierEnabled=true must enable seal")
	assert.False(t, cfgOff.Seal.Enabled, "barrierEnabled=false must disable seal")
}

// TestResolveInitDataDir_PrefersInitDataDir verifies the resolution
// precedence: --init-data-dir > package cfg.Storage.DataDir > global
// --data-dir variable.
func TestResolveInitDataDir_PrefersInitDataDir(t *testing.T) {
	f := &inlineInitFlags{dataDir: "/explicit"}
	// Save / restore the package globals so the test does not pollute
	// state for other tests in the same binary.
	origCfg := cfg
	origDataDir := dataDir
	defer func() { cfg = origCfg; dataDir = origDataDir }()

	cfg = config.Default()
	cfg.Storage.DataDir = "/from-cfg"
	dataDir = "/from-global"

	assert.Equal(t, "/explicit", resolveInitDataDir(f))

	f.dataDir = ""
	assert.Equal(t, "/from-cfg", resolveInitDataDir(f))

	cfg.Storage.DataDir = ""
	assert.Equal(t, "/from-global", resolveInitDataDir(f))
}
