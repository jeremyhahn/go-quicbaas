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
	"context"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/server"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// embeddedServer manages the lifecycle of an embedded QuicBaaS server for CLI use.
type embeddedServer struct {
	server  *server.Server
	service service.QuicBaaS
	tempDir string
	started atomic.Bool
	mu      sync.Mutex
}

// embeddedServerHolder holds the singleton embedded server for the CLI.
var embeddedServerHolder atomic.Pointer[embeddedServer]

// embeddedCLITrustDomain is the SPIFFE trust domain the CLI's own embedded node
// issues its platform-admin certificate under.
//
// A trust domain is REQUIRED of any node that has a CA — first-boot fails closed
// without one and the boot is refused (go-quicbaas#57) — and EmbeddedConfig keeps
// TLS on precisely so that certificate is issued. Declaring one here does not
// synthesize a deployment identifier, which is forbidden because configuring a
// real trust domain later would silently invalidate every certificate issued
// under the guessed one. Nothing can be invalidated here: this node lives in a
// throwaway os.MkdirTemp directory that is removed on shutdown, binds no
// listener, and its admin credential is never presented to anything.
//
// `.invalid` is reserved by RFC 2606, so the value cannot collide with a real
// deployment's trust domain or resolve anywhere.
const embeddedCLITrustDomain = "cli.embedded.quicbaas.invalid"

// EmbeddedServerFailedError is returned when the embedded server fails to start.
type EmbeddedServerFailedError struct {
	Reason string
}

// Error implements the error interface.
func (e *EmbeddedServerFailedError) Error() string {
	return "embedded server failed: " + e.Reason
}

// EmbeddedPortError is returned when the listener address cannot be resolved to a TCP address.
type EmbeddedPortError struct{}

// Error implements the error interface.
func (e *EmbeddedPortError) Error() string {
	return "listener address is not a TCP address"
}

// embeddedServerConfig returns the configuration the CLI's in-process server
// boots with, its data and WAL under tempDir.
//
// It reserves no port. The server runs in standalone mode, where the qrdb node
// is built without a Raft host and binds no listener at all, so a Raft address
// here would never be bound. The CLI used to pick one anyway -- by listening on
// ":0" and closing the listener -- which left a release-then-rebind window for
// a port nothing then used.
func embeddedServerConfig(tempDir string) *config.Config {
	cfg := config.EmbeddedConfig()
	cfg.Storage.DataDir = filepath.Join(tempDir, "data")
	cfg.Storage.WALDir = filepath.Join(tempDir, "wal")
	cfg.Raft.NodeID = 1
	cfg.Logging.Level = "error" // Reduce noise
	cfg.Server.Embedded = true
	// Without this the node has a CA and no trust domain, so first-boot cannot
	// issue a usable admin certificate and refuses the boot — see
	// embeddedCLITrustDomain.
	cfg.IAM.SPIFFETrustDomain = embeddedCLITrustDomain
	return cfg
}

// ErrEmbeddedServerNotStarted is returned when operations are attempted before the server starts.
var ErrEmbeddedServerNotStarted = errors.New(errors.ErrInternal, "getEmbeddedService",
	"embedded server not started")

// getOrCreateEmbeddedServer returns the singleton embedded server, creating it if needed.
func getOrCreateEmbeddedServer() (*embeddedServer, error) {
	// Fast path: check if server already exists
	if srv := embeddedServerHolder.Load(); srv != nil {
		return srv, nil
	}

	// Slow path: create new server
	srv, err := createEmbeddedServer()
	if err != nil {
		return nil, err
	}

	// Store atomically (handle race condition)
	if !embeddedServerHolder.CompareAndSwap(nil, srv) {
		// Another goroutine created the server first, clean up ours
		_ = srv.shutdown()
		if existing := embeddedServerHolder.Load(); existing != nil {
			return existing, nil
		}
		return nil, ErrEmbeddedServerNotStarted
	}

	return srv, nil
}

// createEmbeddedServer creates and starts a new embedded QuicBaaS server.
func createEmbeddedServer() (*embeddedServer, error) {
	// Create temporary directory for embedded data
	tempDir, err := os.MkdirTemp("", "quicbaas-cli-embedded-*")
	if err != nil {
		return nil, &EmbeddedServerFailedError{Reason: "failed to create temp dir: " + err.Error()}
	}

	cfg := embeddedServerConfig(tempDir)

	// Create server
	srv, err := server.NewServer(cfg)
	if err != nil {
		os.RemoveAll(tempDir)
		return nil, &EmbeddedServerFailedError{Reason: "failed to create server: " + err.Error()}
	}

	// Start server. A failed Start does not unwind itself (see
	// server.Server.Start), so the partial start is released here exactly as
	// the ready-timeout and nil-service paths below do — and before the temp
	// dir is removed, so no store is still open over the files being deleted.
	if err := srv.Start(); err != nil {
		srv.Shutdown(context.Background())
		os.RemoveAll(tempDir)
		return nil, &EmbeddedServerFailedError{Reason: "failed to start server: " + err.Error()}
	}

	// Wait for server to be ready with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ready := make(chan struct{})
	go func() {
		for {
			if srv.IsStarted() {
				close(ready)
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(100 * time.Millisecond):
			}
		}
	}()

	select {
	case <-ready:
		// Server is ready
	case <-ctx.Done():
		srv.Shutdown(context.Background())
		os.RemoveAll(tempDir)
		return nil, &EmbeddedServerFailedError{Reason: "server startup timed out"}
	}

	// Wait for Raft leader election (single-node becomes leader quickly)
	select {
	case <-time.After(2 * time.Second):
	case <-ctx.Done():
		srv.Shutdown(context.Background())
		os.RemoveAll(tempDir)
		return nil, &EmbeddedServerFailedError{Reason: "server startup timed out waiting for leader election"}
	}

	// Get the service
	svc := srv.Service()
	if svc == nil {
		srv.Shutdown(context.Background())
		os.RemoveAll(tempDir)
		return nil, &EmbeddedServerFailedError{Reason: "service is nil after server start"}
	}

	es := &embeddedServer{
		server:  srv,
		service: svc,
		tempDir: tempDir,
	}
	es.started.Store(true)

	return es, nil
}

// getEmbeddedService returns the service from the embedded server.
func getEmbeddedService() (service.QuicBaaS, error) {
	srv, err := getOrCreateEmbeddedServer()
	if err != nil {
		return nil, err
	}
	return srv.service, nil
}

// shutdown shuts down the embedded server and cleans up resources.
func (es *embeddedServer) shutdown() error {
	es.mu.Lock()
	defer es.mu.Unlock()

	if !es.started.Load() {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var firstErr error

	if es.server != nil {
		if err := es.server.Shutdown(ctx); err != nil {
			firstErr = errors.Wrapf(err, errors.ErrInternal, "shutdown", "failed to shutdown embedded server")
		}
	}

	if es.tempDir != "" {
		if err := os.RemoveAll(es.tempDir); err != nil && firstErr == nil {
			firstErr = errors.Wrapf(err, errors.ErrInternal, "shutdown", "failed to remove temp dir")
		}
	}

	es.started.Store(false)
	return firstErr
}

// shutdownEmbeddedServer shuts down the global embedded server if running.
func shutdownEmbeddedServer() error {
	srv := embeddedServerHolder.Swap(nil)
	if srv == nil {
		return nil
	}
	return srv.shutdown()
}
