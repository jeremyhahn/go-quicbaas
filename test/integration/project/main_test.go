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

// Package project_test contains integration tests for project lifecycle and
// per-project agent role enforcement. The tests bootstrap an in-process
// embedded QuicBaaS server and exercise the SDK Project() and Orchestrator()
// clients end-to-end through the embedded transport.
package project_test

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

// ErrEmbeddedBootTimeout indicates the in-process server failed to become
// ready before the configured deadline.
type ErrEmbeddedBootTimeout struct{}

func (ErrEmbeddedBootTimeout) Error() string { return "embedded server boot timeout" }

// ErrEmbeddedNilService indicates the in-process server returned a nil
// service handle after start.
type ErrEmbeddedNilService struct{}

func (ErrEmbeddedNilService) Error() string { return "embedded server returned nil service" }

var (
	testServer  *server.Server
	testService service.QuicBaaS
	testTempDir string

	idCounter uint64
)

// TestMain boots the embedded server once for the package and tears it down
// when the suite finishes. The server has Project, AgentAdmin, and (lazily)
// AgentService wired so the tests can exercise the orchestration surface.
func TestMain(m *testing.M) {
	if err := bootEmbeddedServer(); err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: failed to start embedded server: %v\n", err)
		os.Exit(1)
	}
	code := m.Run()
	teardownEmbeddedServer()
	os.Exit(code)
}

// bootEmbeddedServer starts a single-node in-process QuicBaaS server with
// the orchestration surface enabled. Returns a typed error on failure.
func bootEmbeddedServer() error {
	tempDir, err := os.MkdirTemp("", "quicbaas-project-integration-*")
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

	// Allow Raft leader election to settle deterministically by waiting on a
	// timer channel rather than time.Sleep.
	settleCtx, settleCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer settleCancel()
	settleTimer := time.NewTimer(2 * time.Second)
	defer settleTimer.Stop()
	select {
	case <-settleTimer.C:
	case <-settleCtx.Done():
	}

	svc := srv.Service()
	if svc == nil {
		_ = srv.Shutdown(context.Background())
		os.RemoveAll(tempDir)
		return ErrEmbeddedNilService{}
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

// waitForServerReady polls the server's IsStarted predicate via a ticker and
// returns when ready or with ErrEmbeddedBootTimeout when the timeout elapses.
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

// uniqueSuffix returns a process-unique suffix for naming projects and tenants
// within a test run.
func uniqueSuffix() string {
	return strconv.FormatUint(atomic.AddUint64(&idCounter, 1), 10) +
		"-" + strconv.FormatInt(time.Now().UnixNano(), 10)
}
