//go:build rest

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

// sealed_window_e2e_test.go covers the sealed-window boot phase against the
// real daemon, hermetically in-process (white-box side). The quicbaas-SDK
// TP-compat flow lives in sealed_window_sdk_compat_test.go (external test
// package — the SDK imports pkg/server, so it cannot be imported from the
// internal test package).
package server

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// NewSealedBootTestConfig builds a minimal seal-enabled daemon config for
// sealed-window tests: pebble storage, passphrase strategy with NO
// auto-unseal material, REST on an ephemeral port, gRPC over a Unix socket
// (the sealed gRPC bring-up requires at least one listener), every optional
// subsystem off, TLS off (hermetic HTTP).
//
// TLS is deliberately kept OFF here rather than flipped on for the post-flip
// unified-server (TLS-only) REST path: enabling Security.TLSEnabled on this
// config was empirically found to also engage first-boot's automatic system
// barrier initialize-and-unseal (observed via "barrier initialized" /
// "barrier initialized strategy=passphrase" appearing immediately at boot,
// before any sealed-window listener ever starts) — a pre-existing coupling
// between TLS and first-boot bootstrap this test does not intend to exercise
// and that defeats its entire premise (driving BarrierInitialize against a
// genuinely SEALED daemon). Consequently, post-unseal REST reachability
// (which now requires TLS — see (*config.Config).NetworkServing /
// validateNetworkServingTLS) is NOT verified by
// TestSealedBoot_SDKDrivesBarrierAgainstSealedDaemon's Phase 4; only the
// node's own Unseal completion (GetQRDBNodeClient) is. See that test's
// comments for the full account. Exported for the external SDK-compat test
// package.
func NewSealedBootTestConfig(t *testing.T) *config.Config {
	t.Helper()
	dataDir := t.TempDir()

	cfg := config.Default()
	cfg.Server.Embedded = false
	cfg.Server.Mode = config.ServerModeStandalone
	cfg.Server.Host = "127.0.0.1"
	cfg.Storage.Backend = config.StorageBackendPebble
	cfg.Storage.DataDir = dataDir

	cfg.Seal.Enabled = true
	cfg.Seal.Strategy = "passphrase"

	cfg.Security.TLSEnabled = false
	cfg.Security.RequireAuthz = false

	cfg.Server.REST.Enabled = true
	cfg.Server.REST.Port = 0
	cfg.Server.REST.BindAddress = "127.0.0.1"
	cfg.Server.GRPC.Enabled = false
	cfg.Server.MCP.Enabled = false
	cfg.Server.Unix.Socket = filepath.Join(dataDir, "qb.sock")

	cfg.Auth.AllowPasswordAuth = false
	cfg.Auth.InsecureDevMode = false

	cfg.KMS.Enabled = false
	cfg.Secrets.Enabled = false
	cfg.Vector.Enabled = false
	cfg.Ollama.Enabled = false
	cfg.Backup.Enabled = false
	cfg.Discovery.Enabled = false
	cfg.Messaging.Enabled = false
	cfg.Orchestration.Enabled = false
	cfg.TimeSeries.Enabled = false
	cfg.Raft.EnableBootstrap = false
	cfg.Server.EmbeddedAgent.Enabled = false
	cfg.Scheduler.Enabled = false

	cfg.Logging.Level = "error"
	cfg.Logging.Format = "json"
	cfg.Logging.Output = "stderr"
	return cfg
}

// waitFor polls fn every 100ms until it returns true or the deadline
// elapses (channel-based, no sleeps in the assertion path).
func waitFor(t *testing.T, timeout time.Duration, what string, fn func() bool) {
	t.Helper()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.After(timeout)
	for {
		select {
		case <-ticker.C:
			if fn() {
				return
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}

// TestSealedBoot_ShutdownWhileSealedIsClean asserts a daemon shut down while
// still waiting for unseal aborts the boot with the typed
// SealedWindowShutdownError, tears the sealed listeners down, and neither
// hangs nor panics.
func TestSealedBoot_ShutdownWhileSealedIsClean(t *testing.T) {
	cfg := NewSealedBootTestConfig(t)

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	startErr := make(chan error, 1)
	go func() { startErr <- srv.Start() }()

	waitFor(t, 60*time.Second, "sealed-window REST listener", func() bool {
		return srv.RESTAddr() != nil
	})
	require.True(t, srv.qrdbNode.IsSealed(), "node must boot sealed without auto-unseal material")
	require.Nil(t, srv.GetQRDBNodeClient(), "sealed node has no SDK client yet")

	// Trigger shutdown while sealed: the wait loop must observe it and
	// abort the boot with the typed error. runSealedWindow's error path
	// tears the sealed listeners + client down before returning.
	srv.TriggerShutdown()

	select {
	case bootErr := <-startErr:
		var sdErr *SealedWindowShutdownError
		require.ErrorAs(t, bootErr, &sdErr, "boot must abort with the typed sealed-window shutdown error, got %v", bootErr)
	case <-time.After(60 * time.Second):
		t.Fatal("Start did not return after shutdown was triggered during the sealed window")
	}

	assert.Nil(t, srv.sealedWindowClient, "sealed-window client torn down on aborted boot")
	assert.Nil(t, srv.restServer, "sealed REST listener torn down on aborted boot")

	// TriggerShutdown consumed the shared shutdown Once, so Shutdown is a
	// no-op; close the sealed node directly (white-box) to release the
	// pebble locks for the tempdir cleanup.
	require.NoError(t, srv.stopQRDBNode())
}

// TestSealedBoot_EmbeddedRefusalHasNoSideEffects asserts an embedded-mode
// server over a sealed node fails initialize with the typed error and does
// not leave sealed-window state behind.
func TestSealedBoot_EmbeddedRefusalHasNoSideEffects(t *testing.T) {
	cfg := NewSealedBootTestConfig(t)
	cfg.Server.Embedded = true
	cfg.Server.Unix.Socket = ""

	srv, err := NewServer(cfg)
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})

	err = srv.Start()
	var embErr *EmbeddedSealedBootError
	require.ErrorAs(t, err, &embErr, "embedded sealed boot must be refused with the typed error, got %v", err)
	assert.Nil(t, srv.sealedWindowClient)
	assert.Nil(t, srv.sealedWindowFacade)
}
