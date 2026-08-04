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
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/server"
)

// EmbeddedStartOptions controls how the in-process quicbaasd is launched
// from the desktop app. All fields are optional — when empty, safe
// loopback defaults are used so the UI can "just start" in a clean env.
type EmbeddedStartOptions struct {
	// DataDir overrides the temp directory used for storage. When empty a
	// new os.TempDir entry is allocated and cleaned up on Stop.
	DataDir string `json:"data_dir,omitempty"`
	// Host is the loopback host the REST endpoint binds to. Defaults to 127.0.0.1.
	Host string `json:"host,omitempty"`
	// RESTPort pins the REST listener port. 0 = pick a free port at runtime.
	RESTPort int `json:"rest_port,omitempty"`
	// LogLevel overrides the embedded server log level. Defaults to "error"
	// so the desktop console stays quiet.
	LogLevel string `json:"log_level,omitempty"`
}

// EmbeddedServerBinding manages an in-process quicbaasd when the desktop
// app is launched in standalone mode (no --server flag). This lets an
// operator run the full stack locally for offline work, demos, or
// single-user deployments without any external server process.
//
// The Svelte frontend can point its REST/gRPC client at the URL
// returned by ConnectionURL() once Start has returned.
type EmbeddedServerBinding struct {
	logger *logger.Logger
	ctx    context.Context

	mu       sync.Mutex
	started  atomic.Bool
	server   *server.Server
	tempDir  string
	bindAddr string
}

// NewEmbeddedServerBinding constructs a binding that owns at most one
// embedded server at a time. The constructor does not start anything —
// Start must be called from the frontend when standalone mode is chosen.
func NewEmbeddedServerBinding(log *logger.Logger) *EmbeddedServerBinding {
	return &EmbeddedServerBinding{
		logger: log,
	}
}

// startup stores the Wails application context so event emissions (if
// added later) can reference it.
func (b *EmbeddedServerBinding) startup(ctx context.Context) {
	b.ctx = ctx
}

// shutdown tears down the embedded server if one is running. Any error
// is logged — shutdown is best-effort during application close.
func (b *EmbeddedServerBinding) shutdown(ctx context.Context) {
	if err := b.Stop(); err != nil {
		b.logger.WithError(err).Error("failed to stop embedded server on shutdown")
	}
}

// Start boots an in-process quicbaasd with an in-memory KV store bound
// to the configured loopback address. Returns an error if an embedded
// server is already running.
func (b *EmbeddedServerBinding) Start(opts EmbeddedStartOptions) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.started.Load() {
		return NewGUIEmbeddedAlreadyRunningError("EmbeddedServerBinding.Start")
	}

	cfg, tempDir, err := b.buildConfig(opts)
	if err != nil {
		return NewGUIEmbeddedStartError("EmbeddedServerBinding.Start", err)
	}

	srv, err := server.NewServer(cfg)
	if err != nil {
		if tempDir != "" {
			os.RemoveAll(tempDir)
		}
		return NewGUIEmbeddedStartError("EmbeddedServerBinding.Start", err)
	}

	if err := srv.Start(); err != nil {
		if tempDir != "" {
			os.RemoveAll(tempDir)
		}
		return NewGUIEmbeddedStartError("EmbeddedServerBinding.Start", err)
	}

	if err := b.waitReady(srv); err != nil {
		srv.Shutdown(context.Background())
		if tempDir != "" {
			os.RemoveAll(tempDir)
		}
		return NewGUIEmbeddedStartError("EmbeddedServerBinding.Start", err)
	}

	b.server = srv
	b.tempDir = tempDir
	b.bindAddr = restConnectionURL(cfg)
	b.started.Store(true)

	// Publish the server's CA into the process-wide seam so
	// `quicbaas agent deploy` (from the same desktop process) can
	// mint mTLS client identities for the daemons it provisions.
	if ca := srv.CA(); ca != nil {
		installEmbeddedAgentCA(ca)
	}

	// Publish the reachable MCP endpoint so the same desktop process's
	// `quicbaas agent deploy` can start deployed daemons with --mcp-endpoint.
	installEmbeddedMCPEndpoint(embeddedMCPEndpoint(cfg))

	b.logger.Infof("embedded quicbaasd started at %s", b.bindAddr)
	return nil
}

// Stop gracefully shuts the embedded server down and removes any
// allocated temp dir. Calling Stop when nothing is running is a no-op.
func (b *EmbeddedServerBinding) Stop() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.started.Load() {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var firstErr error
	if b.server != nil {
		if err := b.server.Shutdown(ctx); err != nil {
			firstErr = NewGUIEmbeddedStopError("EmbeddedServerBinding.Stop", err)
		}
	}
	if b.tempDir != "" {
		if err := os.RemoveAll(b.tempDir); err != nil && firstErr == nil {
			firstErr = NewGUIEmbeddedStopError("EmbeddedServerBinding.Stop", err)
		}
	}

	uninstallEmbeddedAgentCA()
	uninstallEmbeddedMCPEndpoint()
	b.server = nil
	b.tempDir = ""
	b.bindAddr = ""
	b.started.Store(false)
	return firstErr
}

// IsRunning reports whether an embedded server is currently active.
func (b *EmbeddedServerBinding) IsRunning() bool {
	return b.started.Load()
}

// ConnectionURL returns the REST endpoint the frontend SDK can connect
// to, or empty string when no embedded server is running.
func (b *EmbeddedServerBinding) ConnectionURL() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.bindAddr
}

// buildConfig assembles a single-node config suitable for running in a
// desktop context — in-memory storage, REST-only on loopback, all
// clustering features disabled.
func (b *EmbeddedServerBinding) buildConfig(opts EmbeddedStartOptions) (*config.Config, string, error) {
	host := opts.Host
	if host == "" {
		host = "127.0.0.1"
	}

	var tempDir string
	dataDir := opts.DataDir
	if dataDir == "" {
		dir, err := os.MkdirTemp("", "quicbaas-desktop-embedded-*")
		if err != nil {
			return nil, "", err
		}
		tempDir = dir
		dataDir = dir
	}

	raftPort, err := reserveLoopbackPort()
	if err != nil {
		if tempDir != "" {
			os.RemoveAll(tempDir)
		}
		return nil, "", err
	}

	restPort := opts.RESTPort
	if restPort == 0 {
		port, err := reserveLoopbackPort()
		if err != nil {
			if tempDir != "" {
				os.RemoveAll(tempDir)
			}
			return nil, "", err
		}
		restPort = port
	}

	logLevel := opts.LogLevel
	if logLevel == "" {
		logLevel = "error"
	}

	cfg := config.DefaultConfig()
	cfg.Server.Host = host
	cfg.Server.Embedded = false // we want real listeners for the frontend
	cfg.Server.GRPC.Enabled = false
	cfg.Server.REST.Enabled = true
	cfg.Server.REST.Port = restPort
	cfg.Server.QUIC.Enabled = false
	cfg.Server.MCP.Enabled = false
	cfg.Server.Unix.Enabled = false

	// In-memory KV store keeps the desktop app self-contained and
	// avoids leaving artefacts on disk when DataDir is auto-allocated.
	cfg.Storage.Backend = config.StorageBackendMemory
	cfg.Storage.DataDir = filepath.Join(dataDir, "data")
	cfg.Storage.WALDir = filepath.Join(dataDir, "wal")

	cfg.Raft.NodeID = 1
	cfg.Raft.RaftAddress = host + ":" + strconv.Itoa(raftPort)
	cfg.Raft.ListenAddress = host + ":" + strconv.Itoa(raftPort)

	cfg.Logging.Level = logLevel

	cfg.Security.TLSEnabled = false
	cfg.Discovery.Enabled = false
	cfg.Backup.Enabled = false
	cfg.KMS.Enabled = false
	cfg.Secrets.Enabled = false
	cfg.Vector.Enabled = false
	cfg.TimeSeries.Enabled = false
	cfg.Ollama.Enabled = false

	return cfg, tempDir, nil
}

// waitReady blocks until the embedded server reports IsStarted or the
// internal timeout elapses. The existing CLI embedded server uses the
// same polling pattern for leader election.
func (b *EmbeddedServerBinding) waitReady(srv *server.Server) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		if srv.IsStarted() {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// restConnectionURL constructs the URL the frontend SDK uses to reach
// the embedded server. TLS is never enabled in the desktop embedded
// config so this is always http://.
func restConnectionURL(cfg *config.Config) string {
	if !cfg.Server.REST.Enabled {
		return ""
	}
	return "http://" + cfg.Server.Host + ":" + strconv.Itoa(cfg.Server.REST.Port)
}

// reserveLoopbackPort asks the kernel for an ephemeral TCP port on
// 127.0.0.1, then releases it so the server can bind to it. This is
// subject to a theoretical TOCTOU race — in practice it's what every
// other in-process server in this codebase does (see embedded.go).
func reserveLoopbackPort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		return 0, &EmbeddedPortError{}
	}
	return addr.Port, nil
}

// embeddedMCPEndpoint derives the reachable MCP endpoint
// (https://host:port/mcp) from the embedded server config, or returns empty
// when MCP is disabled or no concrete host can be derived. A wildcard or empty
// bind host cannot be advertised to a remote agent, so it yields no endpoint
// rather than an unreachable loopback URL.
func embeddedMCPEndpoint(cfg *config.Config) string {
	if cfg == nil || !cfg.Server.MCP.Enabled {
		return ""
	}
	host := strings.TrimSpace(cfg.Server.MCP.BindAddress)
	if host == "" {
		host = strings.TrimSpace(cfg.Server.Host)
	}
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		return ""
	}
	return "https://" + net.JoinHostPort(host, strconv.Itoa(cfg.Server.MCP.Port)) + "/mcp"
}
