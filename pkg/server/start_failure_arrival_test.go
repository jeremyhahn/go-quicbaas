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

// start_failure_arrival_test.go is the ARRIVAL half of the Start()-failure
// cleanup contract (go-quicbaas#62); start_partial_failure_cleanup_test.go
// owns the RELEASE half. Here a real initialize fails at orchestration, which
// it reaches after startService, so the orphans really
// exist when Shutdown runs. The background goroutine is observed through the
// cache manager's Done channel, which closes only when that goroutine exits,
// so nothing waits on a timer. The KMS key-cache goroutine is unexported state
// this file sees only as a dropped field; joining it, and releasing recovered
// persistent tenant databases, are proven over a real Start in
// test/integration/lifecycle.
package server

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// withArrivalServer returns an embedded test server with the KMS key cache
// on, so a start builds both the cache and the KMS managers.
func withArrivalServer(t *testing.T) *Server {
	t.Helper()
	srv := withTempServer(t)
	srv.config.Server.Embedded = true
	srv.config.KMS.Enabled = true
	srv.config.KMS.Cache.Enabled = true
	return srv
}

// shutdownWithin runs Shutdown under a deadline so a teardown that never
// joins fails the test instead of hanging it.
func shutdownWithin(t *testing.T, srv *Server) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return srv.Shutdown(ctx)
}

func TestServerStartFailure_ShutdownReleasesPartialStart(t *testing.T) {
	srv := withArrivalServer(t)
	srv.config.Orchestration.Enabled = true
	srv.config.Orchestration.IdleThresholds = map[string]time.Duration{
		"not-a-watched-job-state": time.Hour,
	}

	startErr := srv.initialize()
	require.Error(t, startErr, "the configured idle threshold must abort the start")

	// Preconditions: the start failed at orchestration, after the
	// cache and KMS managers were built. Without them the assertions below
	// would pass just as well on a start that failed before any orphan existed.
	var thresholdErr *orchestration.JobIdleThresholdError
	require.ErrorAs(t, startErr, &thresholdErr,
		"precondition: the start must fail at orchestration")
	cacheMgr := srv.cacheManager
	require.NotNil(t, cacheMgr, "precondition: the failed start must leave the cache manager behind")
	require.False(t, isDone(cacheMgr.Done()),
		"precondition: the orphaned cache-cleanup goroutine must still be running")
	require.NotNil(t, srv.kmsManager, "precondition: the failed start must leave the KMS manager behind")

	require.NoError(t, shutdownWithin(t, srv), "Shutdown after a failed start must complete without error")

	assert.True(t, isDone(cacheMgr.Done()),
		"Shutdown must join the cache-cleanup goroutine the failed start orphaned")
	assert.Nil(t, srv.kmsManager, "Shutdown must release the orphaned KMS manager")

	require.NoError(t, shutdownWithin(t, srv), "a second Shutdown after a failed start must stay a no-op")
}

func TestServerShutdown_BeforeStartHoldsNothing(t *testing.T) {
	srv := withArrivalServer(t)

	require.NoError(t, shutdownWithin(t, srv), "Shutdown before any start must complete without error")
	assert.Nil(t, srv.cacheManager, "a server that never started holds no cache manager")
	assert.Nil(t, srv.kmsManager, "a server that never started holds no KMS manager")
}

func TestServerStartSuccess_ShutdownJoinsBackgroundGoroutines(t *testing.T) {
	srv := withArrivalServer(t)

	require.NoError(t, srv.initialize(), "the clean embedded start must succeed")
	cacheMgr := srv.cacheManager
	require.NotNil(t, cacheMgr, "precondition: a clean start builds the cache manager")
	require.False(t, isDone(cacheMgr.Done()),
		"precondition: the cache-cleanup goroutine must be running")
	require.NotNil(t, srv.kmsManager, "precondition: a clean start builds the KMS manager")

	require.NoError(t, shutdownWithin(t, srv), "the clean shutdown must succeed")

	assert.True(t, isDone(cacheMgr.Done()), "a clean Shutdown must join the cache-cleanup goroutine")
	assert.Nil(t, srv.kmsManager, "a clean Shutdown must release the KMS manager")
}
