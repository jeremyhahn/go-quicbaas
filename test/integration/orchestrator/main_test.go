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

//go:build integration

// Package orchestrator_test contains integration tests for orchestration
// features that require an in-process embedded QuicBaaS server with the
// orchestration AgentService wired (project access enforcement on ClaimTask,
// auto-spawn task path, sub-job spawning, artifacts).
package orchestrator_test

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/server"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// ErrEmbeddedBootTimeout is returned when the embedded server fails to start
// within the configured deadline.
type ErrEmbeddedBootTimeout struct{}

func (ErrEmbeddedBootTimeout) Error() string { return "embedded server boot timeout" }

// ErrEmbeddedNilService is returned when the embedded server reports started
// but Service() returns nil.
type ErrEmbeddedNilService struct{}

func (ErrEmbeddedNilService) Error() string { return "embedded server returned nil service" }

// ErrAgentServiceNotWired is returned when orchestration AgentService failed
// to attach during server startup.
type ErrAgentServiceNotWired struct{}

func (ErrAgentServiceNotWired) Error() string { return "embedded server did not wire AgentService" }

var (
	testServer  *server.Server
	testService service.QuicBaaS
	testTempDir string

	idCounter uint64
)

// TestMain boots a single embedded server with orchestration enabled and
// shares it across the whole package. Each test gets its own SDK client and
// uses unique tenant/project/agent IDs.
func TestMain(m *testing.M) {
	if err := bootEmbeddedServer(); err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: failed to start embedded server: %v\n", err)
		os.Exit(1)
	}
	code := m.Run()
	teardownEmbeddedServer()
	os.Exit(code)
}

func bootEmbeddedServer() error {
	tempDir, err := os.MkdirTemp("", "quicbaas-orchestrator-integration-*")
	if err != nil {
		return err
	}
	testTempDir = tempDir

	raftPort, err := findFreePort()
	if err != nil {
		os.RemoveAll(tempDir)
		return err
	}

	cfg := config.EmbeddedConfig()
	cfg.Storage.DataDir = filepath.Join(tempDir, "data")
	cfg.Storage.WALDir = filepath.Join(tempDir, "wal")
	cfg.Raft.NodeID = 1
	cfg.Raft.RaftAddress = "127.0.0.1:" + strconv.Itoa(raftPort)
	cfg.Raft.ListenAddress = "127.0.0.1:" + strconv.Itoa(raftPort)
	cfg.Logging.Level = "error"
	cfg.Server.Embedded = true

	// Orchestration is enabled by default in EmbeddedConfig() (via DefaultConfig()).
	// We do not wire Ollama for these tests; backends like "claude" / "codex" /
	// "opencode" are registered but never invoked.

	srv, err := server.NewServer(cfg)
	if err != nil {
		os.RemoveAll(tempDir)
		return err
	}
	if err := srv.Start(); err != nil {
		os.RemoveAll(tempDir)
		return err
	}

	if err := waitForServerReady(srv, 30*time.Second); err != nil {
		_ = srv.Shutdown(context.Background())
		os.RemoveAll(tempDir)
		return err
	}

	// Allow Raft leader election to settle.
	settleTimer := time.NewTimer(2 * time.Second)
	defer settleTimer.Stop()
	<-settleTimer.C

	svc := srv.Service()
	if svc == nil {
		_ = srv.Shutdown(context.Background())
		os.RemoveAll(tempDir)
		return ErrEmbeddedNilService{}
	}
	if svc.Agent() == nil {
		_ = srv.Shutdown(context.Background())
		os.RemoveAll(tempDir)
		return ErrAgentServiceNotWired{}
	}

	testServer = srv
	testService = svc
	return nil
}

func teardownEmbeddedServer() {
	if testServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = testServer.Shutdown(ctx)
	}
	if testTempDir != "" {
		os.RemoveAll(testTempDir)
	}
}

func waitForServerReady(srv *server.Server, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ready := make(chan struct{})
	go func() {
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()
		for {
			if srv.IsStarted() {
				close(ready)
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()

	select {
	case <-ready:
		return nil
	case <-ctx.Done():
		return ErrEmbeddedBootTimeout{}
	}
}

func findFreePort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

func uniqueSuffix() string {
	return strconv.FormatUint(atomic.AddUint64(&idCounter, 1), 10) +
		"-" + strconv.FormatInt(time.Now().UnixNano(), 10)
}
