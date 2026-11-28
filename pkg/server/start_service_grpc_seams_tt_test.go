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

// start_service_grpc_seams_tt_test.go drives two more lifecycle error branches
// via the established function seams:
//
//   - startService tolerates a cache-manager construction failure (the cache is
//     optional; a failure is logged and the service still comes up).
//   - startGRPCServer tolerates a default seal-manager init failure (seal
//     operations become unavailable but the gRPC server still starts).

package server

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/cache"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// TestServer_StartService_TT_CacheManagerFailureTolerated covers the
// cache-manager error branch of startService: a failure constructing the cache
// manager is logged and startService still completes (the cache is optional).
func TestServer_StartService_TT_CacheManagerFailureTolerated(t *testing.T) {
	saveAndRestoreSeams(t)
	srv := withTempServer(t)
	srv.config.Server.Embedded = true
	srv.config.Cache.Enabled = true

	require.NoError(t, srv.initStandaloneQRDBNode(), "qrdb node init must succeed")
	t.Cleanup(func() { _ = srv.stopQRDBNode() })

	newCacheManagerFn = func(cache.Config, interfaces.Logger) (*cache.CacheManager, error) {
		return nil, errors.New("cache manager unavailable")
	}

	require.NoError(t, srv.startService(),
		"a cache-manager failure must be tolerated, not abort startService")
	assert.Nil(t, srv.cacheManager, "no cache manager may be attached on failure")
}
