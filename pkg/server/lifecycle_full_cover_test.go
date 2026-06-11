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

//go:build !integration && raft && local && quic && rest && mcp && vector && timeseries && rag

package server

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// TestServer_FullLifecycle_Standalone_Cover drives a complete embedded
// standalone server through Start (which runs the full initialize pipeline:
// CA, storage, qrdb node, indexes, KMS, secrets, vector, timeseries, RAG,
// embedding, catalog, backup, service, messaging, transports) and then through
// a graceful Shutdown. Embedded mode keeps Start non-blocking and the whole run
// in-memory/in-tempdir. This exercises the long Shutdown branch-per-subsystem
// path and the initialize ordering that flat helper tests cannot reach.
func TestServer_FullLifecycle_Standalone_Cover(t *testing.T) {
	saveAndRestoreSeams(t)

	srv := withTempServer(t)
	srv.config.Server.Embedded = true
	srv.config.Server.Mode = config.ServerModeStandalone
	srv.config.KMS.Enabled = true
	// Enable hermetic in-process subsystems so their start + Shutdown branches
	// are exercised (vector via the embedded node, the in-process messaging
	// pub/sub service, and its watch bridge).
	srv.config.Vector.Enabled = true
	srv.config.Messaging.Enabled = true
	srv.config.Messaging.WatchBridge.Enabled = true

	require.NoError(t, srv.Start(), "embedded standalone Start must succeed")
	assert.True(t, srv.IsStarted())

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	require.NoError(t, srv.Shutdown(ctx), "graceful shutdown must succeed")

	// Shutdown is idempotent: a second call must be a no-op (shutdownOnce).
	require.NoError(t, srv.Shutdown(ctx), "second shutdown must be a no-op")
}

// TestServer_FullLifecycle_Cluster_Cover drives the cluster-mode initialize
// branch (single-node local raft shard transport) plus shutdown, in embedded
// mode so the run stays hermetic. This covers the cluster arm of the
// initialize() node-construction switch that the standalone path skips.
func TestServer_FullLifecycle_Cluster_Cover(t *testing.T) {
	saveAndRestoreSeams(t)

	srv := withTempServer(t)
	srv.config.Server.Embedded = true
	srv.config.Server.Mode = config.ServerModeCluster

	require.NoError(t, srv.initialize(), "cluster-mode initialize must succeed")
	assert.Equal(t, config.ServerModeCluster, srv.GetEffectiveMode())

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	require.NoError(t, srv.Shutdown(ctx))
}

// TestServer_Initialize_AlreadyStarted_Cover verifies the guard that rejects a
// second initialize on an already-started server.
func TestServer_Initialize_AlreadyStarted_Cover(t *testing.T) {
	saveAndRestoreSeams(t)

	srv := withTempServer(t)
	srv.config.Server.Embedded = true
	srv.config.Server.Mode = config.ServerModeStandalone

	require.NoError(t, srv.initialize())
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})

	err := srv.initialize()
	require.Error(t, err, "second initialize must be rejected")
}

// TestServer_Initialize_AfterShutdown_Cover verifies that initialize on a
// closed server is rejected.
func TestServer_Initialize_AfterShutdown_Cover(t *testing.T) {
	saveAndRestoreSeams(t)

	srv := withTempServer(t)
	srv.config.Server.Embedded = true
	srv.config.Server.Mode = config.ServerModeStandalone

	require.NoError(t, srv.initialize())
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	require.NoError(t, srv.Shutdown(ctx))

	// A fresh server that is closed before initialize must reject it.
	srv2 := withTempServer(t)
	srv2.config.Server.Embedded = true
	require.NoError(t, srv2.Shutdown(ctx))
	err := srv2.initialize()
	require.Error(t, err, "initialize on a closed server must be rejected")
}
