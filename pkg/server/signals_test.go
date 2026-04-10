//go:build raft

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

package server

import (
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSignalHandlers(t *testing.T) {
	t.Run("SIGTERM", func(t *testing.T) {
		cfg := createTestConfig(t)
		defer cleanupTestData(t, cfg)

		srv, err := NewServer(cfg)
		require.NoError(t, err)

		shutdownChan := make(chan struct{})

		// Setup signal handlers
		setupSignalHandlers(shutdownChan, srv.logger)

		// Send SIGTERM to ourselves
		go func() {
			time.Sleep(100 * time.Millisecond)
			proc, _ := os.FindProcess(os.Getpid())
			proc.Signal(syscall.SIGTERM)
		}()

		// Wait for shutdown signal (with timeout)
		select {
		case <-shutdownChan:
			// Expected
		case <-time.After(2 * time.Second):
			t.Fatal("Shutdown signal not received")
		}
	})

	t.Run("SIGINT", func(t *testing.T) {
		cfg := createTestConfig(t)
		defer cleanupTestData(t, cfg)

		srv, err := NewServer(cfg)
		require.NoError(t, err)

		shutdownChan := make(chan struct{})

		// Setup signal handlers
		setupSignalHandlers(shutdownChan, srv.logger)

		// Send SIGINT to ourselves
		go func() {
			time.Sleep(100 * time.Millisecond)
			proc, _ := os.FindProcess(os.Getpid())
			proc.Signal(syscall.SIGINT)
		}()

		// Wait for shutdown signal (with timeout)
		select {
		case <-shutdownChan:
			// Expected
		case <-time.After(2 * time.Second):
			t.Fatal("Shutdown signal not received")
		}
	})

	t.Run("MultipleSignals", func(t *testing.T) {
		cfg := createTestConfig(t)
		defer cleanupTestData(t, cfg)

		srv, err := NewServer(cfg)
		require.NoError(t, err)

		shutdownChan := make(chan struct{})

		// Setup signal handlers
		setupSignalHandlers(shutdownChan, srv.logger)

		// Send multiple signals (only first should close channel)
		go func() {
			time.Sleep(100 * time.Millisecond)
			proc, _ := os.FindProcess(os.Getpid())
			proc.Signal(syscall.SIGTERM)
			time.Sleep(50 * time.Millisecond)
			proc.Signal(syscall.SIGTERM)
		}()

		// Wait for first shutdown signal
		select {
		case <-shutdownChan:
			// Expected
		case <-time.After(2 * time.Second):
			t.Fatal("Shutdown signal not received")
		}

		// Channel should already be closed
		select {
		case <-shutdownChan:
			// Expected - channel is closed
		default:
			t.Fatal("Shutdown channel should be closed")
		}
	})
}

func TestSignalHandlerIntegration(t *testing.T) {
	t.Run("WithServerShutdown", func(t *testing.T) {
		cfg := createTestConfig(t)
		defer cleanupTestData(t, cfg)

		srv, err := NewServer(cfg)
		require.NoError(t, err)

		// Start signal handlers manually (normally done in Start())
		setupSignalHandlers(srv.shutdownChan, srv.logger)

		// Verify shutdown is triggered
		srv.TriggerShutdown()

		select {
		case <-srv.shutdownChan:
			// Expected
		case <-time.After(1 * time.Second):
			t.Fatal("Shutdown not triggered")
		}

		assert.False(t, srv.IsClosed()) // Shutdown method not called yet
	})
}
