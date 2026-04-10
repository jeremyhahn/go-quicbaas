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

package helpers

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/server"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// ErrEmbeddedServerStartupTimeout is returned when the embedded server does not
// become ready within the configured timeout period.
type ErrEmbeddedServerStartupTimeout struct{}

func (e ErrEmbeddedServerStartupTimeout) Error() string {
	return "embedded server startup timed out"
}

// ErrEmbeddedServerNilService is returned when the server starts but its
// service instance is nil.
type ErrEmbeddedServerNilService struct{}

func (e ErrEmbeddedServerNilService) Error() string {
	return "service is nil after embedded server start"
}

// ErrEmbeddedNotEnabled is returned when the QUICBAAS_EMBEDDED_ENABLED
// environment variable is not set to "true".
type ErrEmbeddedNotEnabled struct{}

func (e ErrEmbeddedNotEnabled) Error() string {
	return "embedded server not enabled (QUICBAAS_EMBEDDED_ENABLED != true)"
}

// SetupEmbeddedServer boots an in-process embedded QuicBaaS server for use
// by multi-protocol integration tests. The server runs Raft in single-node
// mode with KMS, Secrets, Vector, Scheduler, and Messaging enabled.
//
// On success the returned service.QuicBaaS is registered via
// RegisterEmbeddedService so that helpers.GetEmbeddedService will return it.
//
// Callers must call TeardownEmbeddedServer when the test suite completes.
func SetupEmbeddedServer() (service.QuicBaaS, *server.Server, error) {
	if os.Getenv("QUICBAAS_EMBEDDED_ENABLED") != "true" {
		return nil, nil, ErrEmbeddedNotEnabled{}
	}

	tempDir, err := os.MkdirTemp("", "quicbaas-embedded-integration-*")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create temp dir: %w", err)
	}

	raftPort, err := getFreePort()
	if err != nil {
		os.RemoveAll(tempDir)
		return nil, nil, fmt.Errorf("failed to get free port: %w", err)
	}

	cfg := config.EmbeddedConfig()
	cfg.Storage.DataDir = filepath.Join(tempDir, "data")
	cfg.Storage.WALDir = filepath.Join(tempDir, "wal")
	cfg.Raft.NodeID = 1
	cfg.Raft.RaftAddress = "127.0.0.1:" + strconv.Itoa(raftPort)
	cfg.Raft.ListenAddress = "127.0.0.1:" + strconv.Itoa(raftPort)
	cfg.Logging.Level = "error"
	cfg.Server.Embedded = true

	// Enable services needed by integration tests
	cfg.KMS.Enabled = true
	cfg.Secrets.Enabled = true
	cfg.Secrets.Versioning = config.SecretsVersioningConfig{Enabled: true}
	cfg.Vector.Enabled = true
	cfg.Scheduler.Enabled = true
	cfg.Messaging.Enabled = true

	srv, err := server.NewServer(cfg)
	if err != nil {
		os.RemoveAll(tempDir)
		return nil, nil, fmt.Errorf("failed to create server: %w", err)
	}

	if err := srv.Start(); err != nil {
		os.RemoveAll(tempDir)
		return nil, nil, fmt.Errorf("failed to start server: %w", err)
	}

	// Wait for the server to report started using a channel-based approach.
	startupCtx, startupCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer startupCancel()

	ready := make(chan struct{})
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			if srv.IsStarted() {
				close(ready)
				return
			}
			select {
			case <-startupCtx.Done():
				return
			case <-ticker.C:
			}
		}
	}()

	select {
	case <-ready:
	case <-startupCtx.Done():
		srv.Shutdown(context.Background())
		os.RemoveAll(tempDir)
		return nil, nil, ErrEmbeddedServerStartupTimeout{}
	}

	// Allow Raft leader election to settle. Use a timer channel to avoid
	// time.Sleep per project conventions.
	leaderElectionTimer := time.NewTimer(2 * time.Second)
	select {
	case <-leaderElectionTimer.C:
	}

	svc := srv.Service()
	if svc == nil {
		srv.Shutdown(context.Background())
		os.RemoveAll(tempDir)
		return nil, nil, ErrEmbeddedServerNilService{}
	}

	// Register the service so GetEmbeddedService can find it.
	RegisterEmbeddedService(svc)

	return svc, srv, nil
}

// TeardownEmbeddedServer stops a previously started embedded server and
// cleans up its temporary data directory.
func TeardownEmbeddedServer(srv *server.Server) {
	if srv == nil {
		return
	}

	// Shutdown server
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}

// getFreePort returns an available TCP port.
func getFreePort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}
