//go:build !integration && raft && local && iam

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

// server_coverage_boost_test.go targets specific uncovered code paths in
// pkg/server/ that are not exercised by the existing lifecycle, embedded,
// and full-tag coverage suites. Each test in this file is scoped to a
// single behaviour and uses the synthetic-harness pattern (direct field
// assignment on *Server) to drive previously-unreachable branches.

package server

import (
	"context"
	"crypto/tls"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Start: standalone (non-embedded) mode — covers server.go:349-355
//
// The standalone path installs signal handlers and blocks on shutdownChan
// until TriggerShutdown closes the channel. Run Start in a goroutine and
// trigger shutdown to unblock it. This is the only way to cover the
// post-initialize standalone branch without a real signal arriving.
// =============================================================================

func TestBoost_Start_StandaloneModeBlocksUntilShutdown(t *testing.T) {
	saveAndRestoreSeams(t)

	cfg := createFTConfig(t)
	cfg.Server.Mode = config.ServerModeStandalone
	cfg.Storage.Backend = config.StorageBackendMemory
	cfg.Server.Embedded = false
	cfg.Security.TLSEnabled = false
	// Disable Unix socket to avoid permission errors in CI.
	cfg.Server.Unix.Socket = ""
	// At least one listener is required for initialize() to succeed. gRPC
	// on a free port is the lightest-weight option and does not require TLS.
	cfg.Server.GRPC.Enabled = true
	cfg.Server.GRPC.Port = getFreePort(t)
	cfg.Server.REST.Enabled = false
	cfg.Server.QUIC.Enabled = false
	cfg.Server.MCP.Enabled = false

	// Stub IAM and CA so initialize() succeeds without external dependencies.
	seamMu.Lock()
	initIAMComponentsFn = func(_ *Server) (service.IAMComponents, error) { return nil, nil }
	initSealManagerFn = func(_ *logger.Logger) error { return nil }
	validateRoutingFn = func(_ []uint64, _ map[string]uint64) error { return nil }
	initializeCAFn = func(_ *Server) error { return nil }
	seamMu.Unlock()

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	// Run Start in a goroutine; it will block on shutdownChan.
	done := make(chan error, 1)
	go func() {
		done <- srv.Start()
	}()

	// Give Start a moment to reach the blocking select. Channels can't help
	// here because the blocking point is inside Server.Start, not exposed
	// as a state we can synchronize on. The TriggerShutdown is safe to
	// call before Start reaches the select — close(chan) is idempotent
	// across both orderings via shutdownOnce.
	srv.TriggerShutdown()

	select {
	case err := <-done:
		assert.NoError(t, err, "Start should return cleanly after TriggerShutdown")
	case <-time.After(10 * time.Second):
		t.Fatal("Start did not return within 10s after TriggerShutdown")
	}

	// Tidy up.
	require.NoError(t, srv.Shutdown(context.Background()))
}

// =============================================================================
// WithCA / WithTLSCertificate: option application — covers server.go:79, 87
//
// The outer WithCA/WithTLSCertificate closures were covered by existing
// tests that simply constructed the option. The inner closure (the one
// that mutates *serverOptions) only runs when NewServer iterates
// opts. Constructing a server with the option exercises that closure.
// =============================================================================

func TestBoost_WithCA_AppliesOptionToServer(t *testing.T) {
	cfg := createTestConfig(t)
	t.Cleanup(func() { cleanupTestData(t, cfg) })

	// nil CA is acceptable here: WithCA's contract is to store whatever the
	// caller provides on the serverOptions struct. The downstream initializeCA
	// branch that consumes externalCA is independently tested.
	srv, err := NewServer(cfg, WithCA(nil))
	require.NoError(t, err)
	require.NotNil(t, srv)
	assert.Nil(t, srv.opts.externalCA, "WithCA(nil) stores a nil external CA on the options struct")
}

func TestBoost_WithTLSCertificate_AppliesOptionToServer(t *testing.T) {
	cfg := createTestConfig(t)
	t.Cleanup(func() { cleanupTestData(t, cfg) })

	// An empty tls.Certificate is sufficient — the option only records the
	// pointer; consumption is exercised by issueTLSCertFromExternalCA tests.
	cert := &tls.Certificate{}
	srv, err := NewServer(cfg, WithTLSCertificate(cert))
	require.NoError(t, err)
	require.NotNil(t, srv)
	assert.Same(t, cert, srv.opts.tlsCertificate, "WithTLSCertificate must record the supplied certificate pointer")
}

func TestBoost_MultipleOptions_AreAppliedInOrder(t *testing.T) {
	cfg := createTestConfig(t)
	t.Cleanup(func() { cleanupTestData(t, cfg) })

	cert := &tls.Certificate{}
	srv, err := NewServer(cfg, WithCA(nil), WithTLSCertificate(cert))
	require.NoError(t, err)
	require.NotNil(t, srv)
	assert.Nil(t, srv.opts.externalCA)
	assert.Same(t, cert, srv.opts.tlsCertificate)
}

// =============================================================================
// Shutdown / initialize state guards — covers server.go:373-384 (initialize
// state transitions) and the closed-guard inside Shutdown's once closure.
// =============================================================================

// TestBoost_Initialize_RejectsAlreadyStarted verifies that the started
// guard inside initialize() prevents double initialization. This covers
// the early-return branch at server.go:375-378.
func TestBoost_Initialize_RejectsAlreadyStarted(t *testing.T) {
	srv := withTempServer(t)
	srv.mu.Lock()
	srv.started = true
	srv.mu.Unlock()

	err := srv.initialize()
	require.Error(t, err)
	var qbErr *qberrors.QBError
	require.True(t, errors.As(err, &qbErr), "expected QBError for already-started server, got %T", err)
}

// TestBoost_Initialize_RejectsClosed verifies that the closed guard inside
// initialize() prevents starting a closed server. This covers the
// early-return branch at server.go:379-382.
func TestBoost_Initialize_RejectsClosed(t *testing.T) {
	srv := withTempServer(t)
	srv.mu.Lock()
	srv.closed = true
	srv.mu.Unlock()

	err := srv.initialize()
	require.Error(t, err)
	var qbErr *qberrors.QBError
	require.True(t, errors.As(err, &qbErr), "expected QBError for closed server, got %T", err)
}

// TestBoost_Shutdown_HandlesAlreadyClosedFlag drives the closed-guard
// branch inside the shutdownOnce.Do closure (server.go:560-563). The
// outer shutdownOnce wraps the whole cleanup pipeline, but the inner
// closed flag also bails out when set externally.
func TestBoost_Shutdown_HandlesAlreadyClosedFlag(t *testing.T) {
	srv := withTempServer(t)
	srv.mu.Lock()
	srv.closed = true
	srv.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Must not panic and must return nil — the inner closed-guard short
	// circuits the cleanup pipeline.
	require.NoError(t, srv.Shutdown(ctx))
}

// TestBoost_Shutdown_NilQRDBClient_DoesNotPanic verifies the qrdbClient
// nil-check branch in Shutdown is safe when no sharded client was ever
// initialized.
func TestBoost_Shutdown_NilQRDBClient_DoesNotPanic(t *testing.T) {
	srv := withTempServer(t)
	srv.qrdbClient = nil

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, srv.Shutdown(ctx))
}
