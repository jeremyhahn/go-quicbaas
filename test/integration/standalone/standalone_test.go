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

//go:build integration

// Package standalone drives a real standalone QuicBaaS server (single-node
// qrdb engine on PebbleDB, no Raft) whose store lives in the test's temp
// directory: KV, secrets and scheduler round trips, a restart over the same
// data directory, a passphrase-sealed boot, and what the server leaves on disk.
package standalone

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/server"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

const sealPassphrase = "integration-test-barrier-passphrase"

// standaloneConfig is a standalone server with KMS and secrets on, storing its
// PebbleDB data and WAL under dataDir.
func standaloneConfig(dataDir string) *config.Config {
	cfg := config.EmbeddedConfig()
	cfg.IAM.SPIFFETrustDomain = helpers.SPIFFETrustDomain
	cfg.Storage.Backend = config.StorageBackendPebble
	cfg.Storage.DataDir = filepath.Join(dataDir, "data")
	cfg.Storage.WALDir = filepath.Join(dataDir, "wal")
	cfg.Raft.NodeID = 1
	cfg.Logging.Level = "error"
	cfg.KMS.Enabled = true
	cfg.Secrets.Enabled = true
	return cfg
}

// sealedStandaloneConfig is standaloneConfig behind a passphrase barrier.
func sealedStandaloneConfig(dataDir string) *config.Config {
	cfg := standaloneConfig(dataDir)
	cfg.Seal.Enabled = true
	cfg.Seal.Strategy = "passphrase"
	return cfg
}

// runStandalone starts a server on cfg (auto-unsealed with sealPassphrase when
// cfg is sealed), hands it to use, then shuts it down cleanly.
func runStandalone(t *testing.T, cfg *config.Config, use func(srv *server.Server)) {
	t.Helper()
	var opts []server.ServerOption
	if cfg.Seal.Enabled {
		opts = append(opts, server.WithSealPassphrase([]byte(sealPassphrase)))
	}
	srv, err := server.NewServer(cfg, opts...)
	require.NoError(t, err)
	require.NoError(t, srv.Start())
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		assert.NoError(t, srv.Shutdown(ctx), "clean shutdown must succeed")
	}()
	use(srv)
}

// filesContaining walks root and returns every regular file holding needle,
// plus how many PebbleDB table files (.sst) the walk saw.
func filesContaining(t *testing.T, root string, needle []byte) (hits []string, tables int) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return err
		}
		if filepath.Ext(path) == ".sst" {
			tables++
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.Contains(raw, needle) {
			hits = append(hits, path)
		}
		return nil
	})
	require.NoError(t, err, "walking the data directory must not fail")
	return hits, tables
}

// A passphrase-sealed standalone server must keep no plaintext on disk. A
// small write sits in the memtable and the WAL, so the server is restarted on
// the same directory before the scan: PebbleDB flushes the replayed WAL into
// a table on open, and the read-back proves the scan covers the written data.
// Every file is scanned, tables, WAL and manifests alike.
func TestStandalone_AtRest_SentinelAbsentFromDisk(t *testing.T) {
	dataDir := t.TempDir()
	cfg := sealedStandaloneConfig(dataDir)
	sentinel := []byte("ATREST-SENTINEL-c0ffee-1596")
	const key = "atrest/sentinel"

	runStandalone(t, cfg, func(srv *server.Server) {
		require.NoError(t, srv.GetKVStore(1).Put(context.Background(), key, sentinel))
	})
	runStandalone(t, cfg, func(srv *server.Server) {
		got, err := srv.GetKVStore(1).Get(context.Background(), key)
		require.NoError(t, err, "the sentinel must survive the restart that flushes it")
		require.Equal(t, sentinel, got)
	})

	hits, tables := filesContaining(t, dataDir, sentinel)
	require.Positive(t, tables, "the restart must have flushed data into at least one .sst table")
	require.Empty(t, hits, "plaintext sentinel %q found on disk (at-rest encryption broken)", sentinel)
}

func TestStandalone_KVRoundTripAndScan(t *testing.T) {
	runStandalone(t, standaloneConfig(t.TempDir()), func(srv *server.Server) {
		kv := srv.GetKVStore(1)
		ctx := context.Background()

		want := map[string]string{"scan/a": "val0", "scan/b": "val1", "scan/c": "val2"}
		for k, v := range want {
			require.NoError(t, kv.Put(ctx, k, []byte(v)))
		}

		got := make(map[string]string)
		require.NoError(t, kv.Scan(ctx, "scan/", func(key string, value []byte) error {
			got[key] = string(value)
			return nil
		}))
		assert.Equal(t, want, got)

		require.NoError(t, kv.Delete(ctx, "scan/a"))
		_, err := kv.Get(ctx, "scan/a")
		require.Error(t, err, "a deleted key must not read back")
	})
}

// TestStandalone_KVSurvivesRestart boots a second server over the first
// server's data directory and reads back what the first one wrote.
func TestStandalone_KVSurvivesRestart(t *testing.T) {
	cfg := standaloneConfig(t.TempDir())

	runStandalone(t, cfg, func(srv *server.Server) {
		require.NoError(t, srv.GetKVStore(1).Put(context.Background(), "persist/key", []byte("sentinel")))
	})
	runStandalone(t, cfg, func(srv *server.Server) {
		got, err := srv.GetKVStore(1).Get(context.Background(), "persist/key")
		require.NoError(t, err, "the key must survive the restart")
		assert.Equal(t, []byte("sentinel"), got)
	})
}

func TestStandalone_SecretsRoundTrip(t *testing.T) {
	runStandalone(t, standaloneConfig(t.TempDir()), func(srv *server.Server) {
		secrets := srv.Service().Secrets()
		require.NotNil(t, secrets)
		// Secrets are tenant-scoped; first boot creates the system tenant.
		ctx := tenantctx.WithSystem(context.Background())

		stored, err := secrets.StoreSecret(ctx, &service.StoreSecretRequest{Name: "standalone-secret", Data: []byte("my-secret-value")})
		require.NoError(t, err)
		require.NotEmpty(t, stored.SecretID)

		got, err := secrets.GetSecret(ctx, &service.GetSecretRequest{SecretID: stored.SecretID})
		require.NoError(t, err)
		assert.Equal(t, []byte("my-secret-value"), got.Data)

		_, err = secrets.DeleteSecret(ctx, &service.DeleteSecretRequest{SecretID: stored.SecretID})
		require.NoError(t, err)
		_, err = secrets.GetSecret(ctx, &service.GetSecretRequest{SecretID: stored.SecretID})
		require.Error(t, err, "a deleted secret must not read back")
	})
}

func TestStandalone_SchedulerCreateAndGet(t *testing.T) {
	runStandalone(t, standaloneConfig(t.TempDir()), func(srv *server.Server) {
		scheduler := srv.Service().Scheduler()
		require.NotNil(t, scheduler)
		ctx := tenantctx.WithSystem(context.Background())

		created, err := scheduler.CreateJob(ctx, &service.CreateSchedulerJobRequest{
			Name:      "standalone-job",
			Type:      service.SchedulerJobTypeOneShot,
			ExecuteAt: time.Now().Add(time.Hour),
			Handler:   "noop",
			Payload:   []byte(`{"action":"integration-test"}`),
		})
		require.NoError(t, err)
		require.NotNil(t, created.Job)
		require.NotZero(t, created.Job.ID)

		got, err := scheduler.GetJob(ctx, &service.GetSchedulerJobRequest{JobID: created.Job.ID})
		require.NoError(t, err)
		assert.Equal(t, "standalone-job", got.Job.Name)
	})
}

// TestStandalone_PassphraseSealedStartServesKV proves the passphrase barrier
// auto-unseals at Start and the KV store then serves reads and writes.
func TestStandalone_PassphraseSealedStartServesKV(t *testing.T) {
	runStandalone(t, sealedStandaloneConfig(t.TempDir()), func(srv *server.Server) {
		require.NotNil(t, srv.GetQRDBNodeClient(), "the node client must be online after the auto-unseal")

		kv := srv.GetKVStore(1)
		require.NoError(t, kv.Put(context.Background(), "sealed/key", []byte("value")))
		got, err := kv.Get(context.Background(), "sealed/key")
		require.NoError(t, err)
		assert.Equal(t, []byte("value"), got)
	})
}
