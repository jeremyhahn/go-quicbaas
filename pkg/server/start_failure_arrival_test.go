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

// start_failure_arrival_test.go holds the one ARRIVAL check of the Start()
// cleanup contract (go-quicbaas#62) that opens nothing on disk: a Shutdown
// before any start. start_partial_failure_cleanup_test.go owns the RELEASE
// half. A real start opens Pebble stores that have no in-memory option yet
// (see pebbleStoresWithoutMemoryOption), so the arrival checks over a failed
// and a clean start live in test/integration/lifecycle (#1770), where the
// goroutine stack frames prove the cache-cleanup and KMS key-cache goroutines
// are joined.
package server

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestServerShutdown_BeforeStartHoldsNothing(t *testing.T) {
	srv := withArrivalServer(t)

	require.NoError(t, shutdownWithin(t, srv), "Shutdown before any start must complete without error")
	assert.Nil(t, srv.cacheManager, "a server that never started holds no cache manager")
	assert.Nil(t, srv.kmsManager, "a server that never started holds no KMS manager")
}
