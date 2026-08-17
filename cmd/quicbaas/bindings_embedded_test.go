// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

//go:build desktop

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// Embedded server unit tests exercise the lightweight surface of the
// binding: construction, config assembly, URL shaping, and lifecycle
// semantics on an unstarted binding. Starting a real server would bring
// up PebbleDB + Raft + REST listeners, which is too heavy for unit
// tests (see project CLAUDE.md: "NEVER run heavy logic in unit tests").
// End-to-end validation of Start lives in integration tests.

func newTestEmbeddedBinding(t *testing.T) *EmbeddedServerBinding {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "console", Output: "stderr"})
	require.NoError(t, err)

	b := NewEmbeddedServerBinding(log)
	b.ctx = context.Background()
	return b
}

func TestEmbeddedServerBinding_IsRunning_DefaultFalse(t *testing.T) {
	b := newTestEmbeddedBinding(t)
	assert.False(t, b.IsRunning())
	assert.Empty(t, b.ConnectionURL())
}

func TestEmbeddedServerBinding_Stop_NoopWhenNotStarted(t *testing.T) {
	b := newTestEmbeddedBinding(t)
	require.NoError(t, b.Stop())
}

func TestEmbeddedServerBinding_BuildConfig_DefaultsToLoopback(t *testing.T) {
	b := newTestEmbeddedBinding(t)

	// Provide an explicit DataDir so buildConfig does not allocate a
	// temp directory that survives the test.
	dataDir := t.TempDir()
	cfg, tempDir, err := b.buildConfig(EmbeddedStartOptions{DataDir: dataDir})
	require.NoError(t, err)
	assert.Empty(t, tempDir, "caller-supplied DataDir should not allocate a new tempdir")

	assert.Equal(t, "127.0.0.1", cfg.Server.Host)
	assert.True(t, cfg.Server.REST.Enabled)
	assert.Greater(t, cfg.Server.REST.Port, 0)
	assert.False(t, cfg.Server.GRPC.Enabled)
	assert.False(t, cfg.Server.MCP.Enabled)
	assert.False(t, cfg.Server.Unix.Enabled)
	assert.Equal(t, config.StorageBackendMemory, cfg.Storage.Backend)
	assert.Equal(t, "error", cfg.Logging.Level)

	// DataDir must be reflected in the storage paths.
	assert.Equal(t, filepath.Join(dataDir, "data"), cfg.Storage.DataDir)
	assert.Equal(t, filepath.Join(dataDir, "wal"), cfg.Storage.WALDir)
}

func TestEmbeddedServerBinding_BuildConfig_AllocatesTempDir(t *testing.T) {
	b := newTestEmbeddedBinding(t)

	cfg, tempDir, err := b.buildConfig(EmbeddedStartOptions{})
	require.NoError(t, err)
	require.NotEmpty(t, tempDir)
	t.Cleanup(func() { os.RemoveAll(tempDir) })

	assert.DirExists(t, tempDir)
	assert.True(t, strings.HasPrefix(filepath.Base(tempDir), "quicbaas-desktop-embedded-"))

	// Both data and wal should live beneath the allocated temp dir.
	assert.Equal(t, filepath.Join(tempDir, "data"), cfg.Storage.DataDir)
}

func TestEmbeddedServerBinding_BuildConfig_RespectsOverrides(t *testing.T) {
	b := newTestEmbeddedBinding(t)

	dataDir := t.TempDir()
	cfg, _, err := b.buildConfig(EmbeddedStartOptions{
		DataDir:  dataDir,
		Host:     "127.0.0.1",
		RESTPort: 15999,
		LogLevel: "debug",
	})
	require.NoError(t, err)

	assert.Equal(t, "127.0.0.1", cfg.Server.Host)
	assert.Equal(t, 15999, cfg.Server.REST.Port)
	assert.Equal(t, "debug", cfg.Logging.Level)
}

func TestRestConnectionURL_Disabled(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.REST.Enabled = false
	assert.Empty(t, restConnectionURL(cfg))
}

func TestRestConnectionURL_EnabledShape(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.REST.Enabled = true
	cfg.Server.REST.Port = 18443
	cfg.Server.Host = "127.0.0.1"
	assert.Equal(t, "http://127.0.0.1:18443", restConnectionURL(cfg))
}

func TestReserveLoopbackPort_ReturnsFreePort(t *testing.T) {
	port, err := reserveLoopbackPort()
	require.NoError(t, err)
	assert.Greater(t, port, 0)
	assert.Less(t, port, 65536)
}

func TestEmbeddedServerBinding_Startup_StoresContext(t *testing.T) {
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "console", Output: "stderr"})
	require.NoError(t, err)
	b := NewEmbeddedServerBinding(log)

	ctx := context.Background()
	b.startup(ctx)
	assert.Equal(t, ctx, b.ctx)
}

func TestEmbeddedServerBinding_AlreadyRunningError(t *testing.T) {
	// Simulate an already-running embedded server by flipping the
	// internal started flag. This exercises the guard in Start without
	// requiring a real server boot.
	b := newTestEmbeddedBinding(t)
	b.started.Store(true)
	defer b.started.Store(false)

	err := b.Start(EmbeddedStartOptions{})
	require.Error(t, err)

	var typed *GUIEmbeddedAlreadyRunningError
	assert.ErrorAs(t, err, &typed)
}

func TestEmbeddedServerBinding_Shutdown_Noop(t *testing.T) {
	b := newTestEmbeddedBinding(t)
	// Should not panic when nothing is running.
	b.shutdown(context.Background())
}
