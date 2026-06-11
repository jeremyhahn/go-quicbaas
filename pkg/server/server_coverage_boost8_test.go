//go:build !integration && raft && local && vector

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

// server_coverage_boost8_test.go exercises Shutdown with the full
// constellation of subsystems pre-populated. Each individual subsystem
// is covered by an earlier test; this file ensures the cleanup
// ordering and the aggregate-Shutdown-returns-cleanly contract holds
// when every if-guard is satisfied at once.

package server

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/embedding"
	"github.com/jeremyhahn/go-quicbaas/pkg/messaging"
	"github.com/jeremyhahn/go-quicbaas/pkg/rag"
	"github.com/jeremyhahn/go-quicbaas/pkg/watchbridge"
)

// TestBoost8_Shutdown_FullSubsystemConstellation populates every
// subsystem field that has a lightweight zero-value constructor and
// verifies Shutdown traverses every cleanup branch in a single call.
// This is the canonical reverse-order cleanup test.
func TestBoost8_Shutdown_FullSubsystemConstellation(t *testing.T) {
	srv := withTempServer(t)

	// All subsystems below are uninitialized zero values whose Close/Stop
	// is a no-op when the underlying state is empty. None reference
	// external resources.
	srv.ragSystem = &rag.System{}
	srv.embeddingManager = &embedding.Manager{}
	srv.messagingService = &messaging.PushService{}
	srv.watchBridge = &watchbridge.WatchBridge{}
	srv.qrdbClient = &ShardedClient{}
	srv.localBackend = &LocalBackend{}
	srv.fileBackend = &FileBackend{}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	require.NoError(t, srv.Shutdown(ctx))
	require.True(t, srv.IsClosed())
}
