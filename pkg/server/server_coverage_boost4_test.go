//go:build !integration && raft && local

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

// server_coverage_boost4_test.go drives previously-unreached subsystem
// shutdown branches in Server.Shutdown by directly populating the
// matching server fields with stub or empty subsystems. Each branch was
// guarded by `if s.X != nil`, so setting X to a non-nil value walks the
// happy-path log line.
//
// Subsystems that require non-trivial setup (worker manager, catalog
// manager, etc.) are covered by the full-tag lifecycle integration
// tests; this file targets the lightweight subsystems whose stubs can
// be constructed in-process.

package server

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/messaging"
	"github.com/jeremyhahn/go-quicbaas/pkg/watchbridge"
)

// =============================================================================
// Shutdown — drive multiple subsystem branches in a single test
//
// All subsystems are populated with the smallest non-nil value that
// permits Shutdown to enter the cleanup branch. The expected end state
// after Shutdown is closed=true and no panic.
// =============================================================================

// TestBoost4_Shutdown_TraversesShardedClientBranch verifies the qrdbClient
// cleanup branch. An empty ShardedClient has no transportStore and no
// cached clients; Close returns nil without contacting any backend.
func TestBoost4_Shutdown_TraversesShardedClientBranch(t *testing.T) {
	srv := withTempServer(t)
	srv.qrdbClient = &ShardedClient{}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, srv.Shutdown(ctx))
}

// TestBoost4_Shutdown_TraversesMessagingServiceBranch covers the
// messagingService cleanup branch. An empty *messaging.PushService has
// running=false; Stop returns nil immediately without engaging the hub.
func TestBoost4_Shutdown_TraversesMessagingServiceBranch(t *testing.T) {
	srv := withTempServer(t)
	srv.messagingService = &messaging.PushService{}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, srv.Shutdown(ctx))
}

// TestBoost4_Shutdown_TraversesWatchBridgeBranch covers the watchBridge
// cleanup branch. An empty *watchbridge.WatchBridge has state=stopped
// (zero value); Stop returns nil immediately.
func TestBoost4_Shutdown_TraversesWatchBridgeBranch(t *testing.T) {
	srv := withTempServer(t)
	srv.watchBridge = &watchbridge.WatchBridge{}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, srv.Shutdown(ctx))
}

// TestBoost4_Shutdown_MultipleSubsystemsInOneCall exercises several
// branches in a single Shutdown invocation so the cleanup ordering
// (Workers → Backend → Catalog → … → messaging → watch bridge) is
// exercised once end-to-end. Cleanup order is part of the Shutdown
// contract.
func TestBoost4_Shutdown_MultipleSubsystemsInOneCall(t *testing.T) {
	srv := withTempServer(t)
	srv.qrdbClient = &ShardedClient{}
	srv.messagingService = &messaging.PushService{}
	srv.watchBridge = &watchbridge.WatchBridge{}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, srv.Shutdown(ctx))
	require.True(t, srv.IsClosed(), "Shutdown must mark the server as closed")
}
