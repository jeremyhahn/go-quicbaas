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

package server

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// newPersistentStandaloneServer builds a fresh standalone server on a pebble
// store in an empty data dir, through the production node wiring only: no
// pool directory or catalog entry is created by the test.
func newPersistentStandaloneServer(t *testing.T) *Server {
	t.Helper()
	cfg := &config.Config{}
	cfg.Storage.Backend = config.StorageBackendPebble
	cfg.Storage.DataDir = t.TempDir()
	cfg.Seal.Enabled = true
	cfg.Seal.Strategy = "passphrase"

	srv, err := NewServer(cfg, WithSealPassphrase([]byte("super-secret-passphrase")))
	require.NoError(t, err)
	require.NoError(t, srv.initStandaloneQRDBNode())
	t.Cleanup(func() { _ = srv.stopQRDBNode() })
	return srv
}

// A fresh persistent node registers a pooled tenant barrier once startup has
// provisioned the default volume pool: its directory exists before the node
// opens it, and the replicated catalog holds the pool the barrier reserves on.
func TestDefaultVolumePool_FreshNodeRegistersATenantBarrier(t *testing.T) {
	srv := newPersistentStandaloneServer(t)
	require.NoError(t, srv.provisionDefaultVolumePool())

	poolDir := filepath.Join(srv.config.Storage.DataDir, defaultVolumePoolDirName)
	info, err := os.Stat(poolDir)
	require.NoError(t, err, "startup must create the default pool directory")
	require.True(t, info.IsDir())

	_, err = srv.qrdbNode.BarrierRegistry().RegisterTenant(context.Background(), "acme", nil,
		qrdbsdk.BarrierConfig{}, qrdbsdk.NewDeferredSoftwareStrategy())
	require.NoError(t, err, "a pooled tenant barrier must register on a fresh node")
}

// Provisioning runs on every boot, so a catalog entry created by an earlier
// boot (or by another cluster member) is not an error.
func TestDefaultVolumePool_ProvisioningIsIdempotent(t *testing.T) {
	srv := newPersistentStandaloneServer(t)
	require.NoError(t, srv.provisionDefaultVolumePool())
	require.NoError(t, srv.provisionDefaultVolumePool())
}

// A node with no data dir maps no default pool, so there is nothing to
// provision and startup does not try.
func TestDefaultVolumePool_NoDataDirProvisionsNothing(t *testing.T) {
	cfg := &config.Config{}
	cfg.Storage.Backend = config.StorageBackendMemory
	srv, err := NewServer(cfg)
	require.NoError(t, err)
	require.NoError(t, srv.initStandaloneQRDBNode())
	t.Cleanup(func() { _ = srv.stopQRDBNode() })

	require.NoError(t, srv.provisionDefaultVolumePool())
}

// A node over a store with no volume catalog attached maps a pool directory
// but places no tenant on it, so provisioning leaves the catalog alone
// instead of failing startup.
func TestDefaultVolumePool_NodeWithoutCatalogProvisionsNothing(t *testing.T) {
	cfg := &config.Config{}
	cfg.Storage.Backend = config.StorageBackendMemory
	cfg.Storage.DataDir = t.TempDir()
	srv, err := NewServer(cfg)
	require.NoError(t, err)
	require.NoError(t, srv.initStandaloneQRDBNode())
	t.Cleanup(func() { _ = srv.stopQRDBNode() })
	registry := srv.qrdbNode.BarrierRegistry()
	require.True(t, registry == nil || !registry.HasVolumePool(), "the memory node attaches no volume catalog")

	require.NoError(t, srv.provisionDefaultVolumePool())
}
