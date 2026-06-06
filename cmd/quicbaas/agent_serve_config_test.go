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

// Package main: coverage for the buildServeConfig + TLS helper paths in
// agent_serve.go.
package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/agent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withServeGlobals installs a clean set of serve* globals and restores
// the originals on cleanup.
func withServeGlobals(t *testing.T) {
	t.Helper()
	origs := struct {
		ServerAddr    string
		TenantID      string
		BindAddr      string
		Backends      string
		SandboxPolicy string
		ContainerRT   string
		ImageMap      string
		ImageRegistry string
		AutoPull      bool
		WorkDir       string
		AgentCertFile string
		AgentKeyFile  string
		ServerCAFile  string
		Heartbeat     time.Duration
		Shutdown      time.Duration
	}{
		ServerAddr:    serveServerAddr,
		TenantID:      serveTenantID,
		BindAddr:      serveBindAddr,
		Backends:      serveBackendsCSV,
		SandboxPolicy: serveSandboxPolicy,
		ContainerRT:   serveContainerRT,
		ImageMap:      serveImageMapCSV,
		ImageRegistry: serveImageRegistry,
		AutoPull:      serveAutoPull,
		WorkDir:       serveWorkDir,
		AgentCertFile: serveAgentCertFile,
		AgentKeyFile:  serveAgentKeyFile,
		ServerCAFile:  serveServerCAFile,
		Heartbeat:     serveHeartbeatEvery,
		Shutdown:      serveShutdownGrace,
	}
	t.Cleanup(func() {
		serveServerAddr = origs.ServerAddr
		serveTenantID = origs.TenantID
		serveBindAddr = origs.BindAddr
		serveBackendsCSV = origs.Backends
		serveSandboxPolicy = origs.SandboxPolicy
		serveContainerRT = origs.ContainerRT
		serveImageMapCSV = origs.ImageMap
		serveImageRegistry = origs.ImageRegistry
		serveAutoPull = origs.AutoPull
		serveWorkDir = origs.WorkDir
		serveAgentCertFile = origs.AgentCertFile
		serveAgentKeyFile = origs.AgentKeyFile
		serveServerCAFile = origs.ServerCAFile
		serveHeartbeatEvery = origs.Heartbeat
		serveShutdownGrace = origs.Shutdown
	})
	// Reset to safe defaults so tests don't inherit residue from prior runs.
	serveServerAddr = ""
	serveTenantID = ""
	serveBindAddr = ""
	serveBackendsCSV = ""
	serveSandboxPolicy = "opt-in"
	serveContainerRT = "podman"
	serveImageMapCSV = ""
	serveImageRegistry = ""
	serveAutoPull = false
	serveWorkDir = ""
	serveAgentCertFile = ""
	serveAgentKeyFile = ""
	serveServerCAFile = ""
	serveHeartbeatEvery = time.Second
	serveShutdownGrace = time.Second
}

// =============================================================================
// buildServeConfig
// =============================================================================

func TestBuildServeConfig_BadSandboxPolicy(t *testing.T) {
	withServeGlobals(t)
	serveSandboxPolicy = "bogus-policy"
	_, err := buildServeConfig()
	require.Error(t, err)
}

func TestBuildServeConfig_BadImageMap(t *testing.T) {
	withServeGlobals(t)
	serveImageMapCSV = "no-equals-sign"
	_, err := buildServeConfig()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "image-map entry")
}

func TestBuildServeConfig_TrailingEquals(t *testing.T) {
	withServeGlobals(t)
	serveImageMapCSV = "claude="
	_, err := buildServeConfig()
	require.Error(t, err)
}

func TestBuildServeConfig_Happy(t *testing.T) {
	withServeGlobals(t)
	serveServerAddr = "https://x:8443"
	serveTenantID = "t1"
	serveBackendsCSV = "claude,ollama"
	serveImageMapCSV = "claude=image-a, ollama=image-b"

	cfg, err := buildServeConfig()
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, "https://x:8443", cfg.ServerAddr)
	assert.Equal(t, "t1", cfg.TenantID)
	assert.Equal(t, []agent.BackendKind{"claude", "ollama"}, cfg.Backends)
	assert.Equal(t, "image-a", cfg.ImageMap["claude"])
	assert.Equal(t, "image-b", cfg.ImageMap["ollama"])
}

// =============================================================================
// buildAgentTLSConfig
// =============================================================================

func TestBuildAgentTLSConfig_NoCert(t *testing.T) {
	cfg := &agent.Config{}
	tc := buildAgentTLSConfig(cfg)
	require.NotNil(t, tc)
}

func TestBuildAgentTLSConfig_LoadsCA(t *testing.T) {
	dir := t.TempDir()
	caPath := filepath.Join(dir, "ca.pem")
	writeSelfSignedCertPEM(t, caPath)
	tc := buildAgentTLSConfig(&agent.Config{ServerCAFile: caPath})
	require.NotNil(t, tc)
	assert.NotNil(t, tc.RootCAs)
}

func TestBuildAgentTLSConfig_LoadsClientCert(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "client.crt")
	keyPath := filepath.Join(dir, "client.key")
	writeSelfSignedKeyPair(t, certPath, keyPath)
	tc := buildAgentTLSConfig(&agent.Config{AgentCertFile: certPath, AgentKeyFile: keyPath})
	require.NotNil(t, tc)
	assert.Len(t, tc.Certificates, 1)
}

func TestBuildAgentTLSConfig_BadCAGracefullyIgnored(t *testing.T) {
	// Build the config with a path that doesn't exist: the helper
	// swallows the read error and returns a config without RootCAs.
	tc := buildAgentTLSConfig(&agent.Config{ServerCAFile: "/no/such/ca.pem"})
	require.NotNil(t, tc)
}

// =============================================================================
// buildAgentHTTPClient
// =============================================================================

func TestBuildAgentHTTPClient_Happy(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "client.crt")
	keyPath := filepath.Join(dir, "client.key")
	caPath := filepath.Join(dir, "ca.pem")
	writeSelfSignedKeyPair(t, certPath, keyPath)
	writeSelfSignedCertPEM(t, caPath)

	c, err := buildAgentHTTPClient(&agent.Config{
		AgentCertFile: certPath,
		AgentKeyFile:  keyPath,
		ServerCAFile:  caPath,
	})
	require.NoError(t, err)
	require.NotNil(t, c)
}

func TestBuildAgentHTTPClient_BadCert(t *testing.T) {
	_, err := buildAgentHTTPClient(&agent.Config{
		AgentCertFile: "/no/cert.crt",
		AgentKeyFile:  "/no/key.key",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "load agent key pair")
}

func TestBuildAgentHTTPClient_BadCA(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "client.crt")
	keyPath := filepath.Join(dir, "client.key")
	writeSelfSignedKeyPair(t, certPath, keyPath)

	_, err := buildAgentHTTPClient(&agent.Config{
		AgentCertFile: certPath,
		AgentKeyFile:  keyPath,
		ServerCAFile:  "/no/such/ca.pem",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read server CA")
}

func TestBuildAgentHTTPClient_BadCABundle(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "client.crt")
	keyPath := filepath.Join(dir, "client.key")
	caPath := filepath.Join(dir, "ca.pem")
	writeSelfSignedKeyPair(t, certPath, keyPath)
	require.NoError(t, os.WriteFile(caPath, []byte("not a PEM"), 0o600))

	_, err := buildAgentHTTPClient(&agent.Config{
		AgentCertFile: certPath,
		AgentKeyFile:  keyPath,
		ServerCAFile:  caPath,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no PEM certificates parsed")
}
