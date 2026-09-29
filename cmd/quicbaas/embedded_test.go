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

package main

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

func TestEmbeddedServer_ShutdownNotStarted_Coverage(t *testing.T) {
	es := &embeddedServer{}

	// shutdown when started is false should return nil
	err := es.shutdown()
	assert.NoError(t, err)
}

// TestShutdownEmbeddedServer_Coverage tests global shutdown function.
func TestShutdownEmbeddedServer_Coverage(t *testing.T) {
	// Ensure no server exists
	embeddedServerHolder.Store(nil)

	// Calling shutdown when no server exists should be fine
	err := shutdownEmbeddedServer()
	assert.NoError(t, err)
}

// TestEmbeddedServerConfig_StandaloneBindsNoRaftPort verifies the CLI's
// in-process server boots standalone -- the mode whose qrdb node has no Raft
// host -- with every network listener off, so there is no port for it to
// reserve. If this ever boots clustered it needs a Raft listener, and that
// listener must be bound by the node itself rather than picked and released
// here.
func TestEmbeddedServerConfig_StandaloneBindsNoRaftPort(t *testing.T) {
	dir := t.TempDir()

	cfg := embeddedServerConfig(dir)

	require.NotNil(t, cfg)
	assert.False(t, config.IsClusterLikeMode(cfg.Server.Mode), "embedded server must boot standalone, got mode %q", cfg.Server.Mode)
	assert.True(t, cfg.Server.Embedded)
	assert.False(t, cfg.Server.REST.Enabled)
	assert.False(t, cfg.Server.GRPC.Enabled)
	assert.False(t, cfg.Server.MCP.Enabled)
	assert.False(t, cfg.Server.Unix.Enabled)
	assert.Equal(t, config.EmbeddedConfig().Raft.RaftAddress, cfg.Raft.RaftAddress, "no per-boot Raft address may be picked")
	assert.Equal(t, config.EmbeddedConfig().Raft.ListenAddress, cfg.Raft.ListenAddress, "no per-boot Raft listen address may be picked")
}

// TestEmbeddedServerConfig_ScopesStateToItsDir verifies each boot keeps its
// data and WAL under its own directory and carries the trust domain first-boot
// needs to issue the admin certificate; without it the boot is refused.
func TestEmbeddedServerConfig_ScopesStateToItsDir(t *testing.T) {
	first := embeddedServerConfig(t.TempDir())
	secondDir := t.TempDir()
	second := embeddedServerConfig(secondDir)

	assert.Equal(t, filepath.Join(secondDir, "data"), second.Storage.DataDir)
	assert.Equal(t, filepath.Join(secondDir, "wal"), second.Storage.WALDir)
	assert.NotEqual(t, first.Storage.DataDir, second.Storage.DataDir)
	assert.Equal(t, embeddedCLITrustDomain, second.IAM.SPIFFETrustDomain)
	assert.Equal(t, uint64(1), uint64(second.Raft.NodeID))
}
