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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	restapi "github.com/jeremyhahn/go-quicbaas/pkg/api/rest"
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
	// A DRAWN free port, not 0. Port 0 is ephemeral to the sealed-window
	// listener, which binds it directly, but NOT to the post-unseal one.
	// That one is the embedded qrdb node's, and qrdb_node.go copies this
	// value into qcfg.Server.AdminPort, which reaches
	//
	//     go-qrdb/sdk/go@v0.16.0-alpha node/node.go:2417
	//     if serverCfg.Server.AdminPort == 0 {
	//         serverCfg.Server.AdminPort = 63001
	//     }
	//
	// a hardcoded literal in the SDK's node builder. Note it is NOT
	// qrdbconfig.DefaultConfig(), where AdminPort is 63002 and 63001 is the
	// RAFT default -- so this fallback lands the REST listener on the raft
	// port by construction, and checking the config defaults will not show
	// you that. Measured, not inferred: with REST.Port 0 the post-unseal
	// listener binds 63001; with a drawn port it binds that port.
	//
	// So "port 0" arrives at the bind as a fixed, well-known port every
	// concurrent process in the tree competes for. `go test -p 2` alone is
	// enough to lose that race. go#497: this test spent 1601s of a CI runner
	// on "listen tcp 127.0.0.1:63001: bind: address already in use".
	cfg.Server.REST.Port = getFreePort(t)
	cfg.Server.Host = "127.0.0.1"
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
	cfg.Server.EmbeddedFleetNode.Enabled = false
	cfg.Scheduler.Enabled = false

	cfg.Logging.Level = "error"
	cfg.Logging.Format = "json"
	cfg.Logging.Output = "stderr"
	return cfg
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

	awaitCond(t, "the sealed-window REST listener to come up", func() bool {
		return srv.RESTAddr() != nil
	})
	require.True(t, srv.qrdbNode.IsSealed(), "node must boot sealed without auto-unseal material")
	require.Nil(t, srv.GetQRDBNodeClient(), "sealed node has no SDK client yet")

	// Trigger shutdown while sealed: the wait loop must observe it and
	// abort the boot with the typed error. runSealedWindow's error path
	// tears the sealed listeners + client down before returning.
	srv.TriggerShutdown()

	bootErr := awaitResult(t, startErr, "Start after shutdown during the sealed window")
	var sdErr *SealedWindowShutdownError
	require.ErrorAs(t, bootErr, &sdErr, "boot must abort with the typed sealed-window shutdown error, got %v", bootErr)

	assert.Nil(t, srv.sealedWindowClient, "sealed-window client torn down on aborted boot")
	assert.Nil(t, srv.restServer, "sealed REST listener torn down on aborted boot")

	// The signal is not the teardown: TriggerShutdown has its own Once
	// (#139), so the full Shutdown still runs — and releases the node's
	// pebble locks for the tempdir cleanup.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	require.NoError(t, srv.Shutdown(ctx),
		"Shutdown after TriggerShutdown must still run the teardown, not be consumed by a shared Once")
}

// TestSealedBoot_ShutdownWhileSealedDoesNotDeadlock is the daemon's
// signal path (#139): main answers SIGTERM/SIGINT by calling Shutdown — not
// TriggerShutdown — and Shutdown waits for the in-flight initialize() to
// return before touching any component. While sealed, initialize() is itself
// blocked in awaitUnseal() waiting on the shutdown signal, so a Shutdown
// that does not SEND that signal first is a deadlock: the measured daemon
// ignored SIGTERM and SIGINT while sealed and could only be SIGKILLed.
func TestSealedBoot_ShutdownWhileSealedDoesNotDeadlock(t *testing.T) {
	cfg := NewSealedBootTestConfig(t)

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	startErr := make(chan error, 1)
	go func() { startErr <- srv.Start() }()

	awaitCond(t, "the sealed-window REST listener to come up", func() bool {
		return srv.RESTAddr() != nil
	})
	require.True(t, srv.qrdbNode.IsSealed(), "node must boot sealed without auto-unseal material")

	// Shutdown exactly as the daemon's signal handler does. It must return,
	// not hang on initWG waiting for a boot that is waiting on it.
	shutdownDone := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		shutdownDone <- srv.Shutdown(ctx)
	}()

	select {
	case err := <-shutdownDone:
		require.NoError(t, err, "Shutdown during the sealed window")
	case <-time.After(60 * time.Second):
		t.Fatal("Shutdown deadlocked during the sealed window: nothing signalled awaitUnseal, and Shutdown waits on the initialize it is blocking")
	}

	bootErr := awaitResult(t, startErr, "Start after Shutdown during the sealed window")
	var sdErr *SealedWindowShutdownError
	require.ErrorAs(t, bootErr, &sdErr, "boot must abort with the typed sealed-window shutdown error, got %v", bootErr)
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

// driveBarrierInitialize drives the sealed daemon's barrier unsealed through
// its sealed-window REST listener, as an operator's first-boot initialize
// does: BarrierInitialize seeds the root key and unseals in one step.
func driveBarrierInitialize(t *testing.T, srv *Server) {
	t.Helper()
	addr := srv.RESTAddr()
	require.NotNil(t, addr, "sealed-window REST listener must be up")
	// The body is marshalled from go-qrdb's own request type, not hand-written
	// JSON, because that type IS the wire format (docs/barrier/api.md, "a
	// credential is bytes, and the shapes are aliases"). A literal here would
	// have gone on passing while the handler and the SDK disagreed about
	// whether `secret` is base64 — which is exactly how go-quicbaas#964 reached
	// a burn-in node.
	body, err := json.Marshal(&qrdbtransport.BarrierInitializeRequest{
		Secret: []byte("sealed-window-e2e-passphrase"),
	})
	require.NoError(t, err, "marshal the barrier initialize request")
	resp, err := http.Post(
		fmt.Sprintf("http://%s/api/v1/barrier/initialize", addr.String()),
		"application/json",
		bytes.NewReader(body),
	)
	require.NoError(t, err, "barrier initialize against the sealed-window REST listener")
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusNoContent, resp.StatusCode, "barrier initialize must succeed")
}

// TestSealedBoot_PinnedPortUnsealCompletesFullBoot is the deterministic #139
// collision: with the REST port PINNED (as every deployed config pins it),
// the sealed-window listener holds 127.0.0.1:P while the post-unseal full
// boot binds the node's unified listener on the same port. Under the old
// ordering (Unseal before stopSealedTransports) this failed every time with
// "address already in use" and the daemon exited 1; the reorder must carry
// the boot through on the same pinned port.
//
// The last clause of this comment used to read "the ephemeral-port config of
// the other tests in this file cannot exercise this -- port 0 never
// collides." That was false, and it is the belief that cost go#497 a
// 1601-second CI runner slot. Port 0 is ephemeral only to the sealed-window
// listener. The post-unseal listener is the node's: qrdb_node.go copies
// Server.REST.Port into qcfg.Server.AdminPort, where the node builder
// substitutes a default for a zero port -- so "port 0" arrived at the bind as
// the FIXED, well-known 63001, the single most contended port in this tree
// rather than a private one. The other tests were not exempt from collisions;
// they were pointed at a shared port with no way to see it.
//
// So this test is not the pinned-port special case it was written as. It is
// the only one in the file that ever bound a port of its own, and drawing a
// real port (which NewSealedBootTestConfig now does for everyone) is the
// general rule, not the exception.
func TestSealedBoot_PinnedPortUnsealCompletesFullBoot(t *testing.T) {
	cfg := NewSealedBootTestConfig(t)
	cfg.Server.REST.Port = getFreePort(t)

	srv, err := NewServer(cfg)
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})

	startErr := make(chan error, 1)
	go func() { startErr <- srv.Start() }()

	awaitCond(t, "the sealed-window REST listener to come up on the pinned port", func() bool {
		addr := srv.RESTAddr()
		return addr != nil && addr.(*net.TCPAddr).Port == cfg.Server.REST.Port
	})
	require.True(t, srv.qrdbNode.IsSealed(), "node must boot sealed without auto-unseal material")

	driveBarrierInitialize(t, srv)

	// A Start that returns while we wait is the collision this test exists
	// to catch.
	awaitCond(t, "the node boot to complete on the pinned port", func() bool {
		select {
		case bootErr := <-startErr:
			t.Fatalf("Start aborted instead of completing the pinned-port boot (#139 collision): %v", bootErr)
		default:
		}
		return srv.GetQRDBNodeClient() != nil
	})

	// The node client is NOT the full-boot readiness signal — initialization
	// steps after it exists (IAM writes, transport rewire) can still fail,
	// and Start would return their error after the poll above succeeded
	// (hosted review finding). Drive the observable completion instead: a
	// clean Shutdown unblocks Start, whose return value is initialize()'s
	// verdict — nil ONLY if every boot step on the pinned port completed.
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	require.NoError(t, srv.Shutdown(ctx), "clean shutdown after the pinned-port boot")
	require.NoError(t, awaitResult(t, startErr, "Start after the pinned-port boot"),
		"Start must return nil: any error is a boot step that failed after the node client appeared")
}

// TestSealedBoot_TeardownFailureAbortsBoot pins the fail-closed side of the
// #139 reorder: when the sealed-phase teardown cannot prove its listeners
// released (stopSealedTransports reports an error), the boot must abort with
// the typed error INSTEAD of proceeding into node.Unseal — a sealed listener
// that may still hold its port would turn the deterministic collision the
// reorder fixed into a racy one.
func TestSealedBoot_TeardownFailureAbortsBoot(t *testing.T) {
	cfg := NewSealedBootTestConfig(t)

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	// Inject the failure through the restShutdownFn seam. The override
	// still runs the real shutdown — the listener genuinely releases, the
	// test does not leak it — but REPORTS failure, which is exactly the
	// "cannot prove released" condition the boot must refuse to build on.
	realShutdown := restShutdownFn
	injected := errors.New("injected sealed REST teardown failure")
	restShutdownFn = func(rs *restapi.Server, ctx context.Context) error {
		_ = realShutdown(rs, ctx)
		return injected
	}
	t.Cleanup(func() { restShutdownFn = realShutdown })

	startErr := make(chan error, 1)
	go func() { startErr <- srv.Start() }()

	awaitCond(t, "the sealed-window REST listener to come up", func() bool {
		return srv.RESTAddr() != nil
	})
	require.True(t, srv.qrdbNode.IsSealed(), "node must boot sealed without auto-unseal material")

	driveBarrierInitialize(t, srv)

	// Fail fast on the fail-open outcome: a node SDK client appearing means
	// the full boot ran on top of the unproven teardown.
	var bootErr error
	awaitCond(t, "Start to abort after a failing sealed teardown", func() bool {
		if srv.GetQRDBNodeClient() != nil {
			t.Fatal("the full node boot completed after a failing sealed teardown instead of aborting")
		}
		select {
		case bootErr = <-startErr:
			return true
		default:
			return false
		}
	})
	var swErr *SealedWindowBootError
	require.ErrorAs(t, bootErr, &swErr, "boot must abort with the typed sealed-window error, got %v", bootErr)
	assert.Equal(t, "sealed transport teardown", swErr.Stage)
	assert.ErrorContains(t, bootErr, "injected sealed REST teardown failure")
	assert.Nil(t, srv.GetQRDBNodeClient(), "the full node boot must NOT have run after a failing teardown")

	// Cleanup must still work: the seam is restored and the components were
	// released by the teardown attempt.
	restShutdownFn = realShutdown
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	require.NoError(t, srv.Shutdown(ctx))
}

// TestSealedBoot_ShutdownRacesTriggerShutdown races the daemon's two
// shutdown entry points against each other while sealed, under -race: main's
// signal handler calls TriggerShutdown while an operator-driven Shutdown
// runs concurrently. The channel close has exactly one site behind its own
// Once (#139), so neither interleaving may double-close, deadlock, or panic.
// The sibling tests cover each entry point sequentially and alone; this one
// exists because the double-close path only opens when they overlap.
func TestSealedBoot_ShutdownRacesTriggerShutdown(t *testing.T) {
	cfg := NewSealedBootTestConfig(t)

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	startErr := make(chan error, 1)
	go func() { startErr <- srv.Start() }()

	awaitCond(t, "the sealed-window REST listener to come up", func() bool {
		return srv.RESTAddr() != nil
	})

	var wg sync.WaitGroup
	shutdownErr := make(chan error, 1)
	wg.Add(2)
	go func() {
		defer wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		shutdownErr <- srv.Shutdown(ctx)
	}()
	go func() {
		defer wg.Done()
		srv.TriggerShutdown()
		srv.TriggerShutdown() // a repeated signal must be as safe as one
	}()
	wg.Wait()

	require.NoError(t, awaitResult(t, shutdownErr, "Shutdown racing TriggerShutdown"))
	bootErr := awaitResult(t, startErr, "Start after racing shutdown paths")
	var sdErr *SealedWindowShutdownError
	require.ErrorAs(t, bootErr, &sdErr, "boot must abort with the typed sealed-window shutdown error, got %v", bootErr)
}
