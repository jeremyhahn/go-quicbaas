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

		// Send SIGTERM to ourselves. setupSignalHandlers registers its
		// signal.Notify channel synchronously, so the signal cannot be
		// missed and no delay is needed before sending it.
		proc, err := os.FindProcess(os.Getpid())
		require.NoError(t, err)
		require.NoError(t, proc.Signal(syscall.SIGTERM))

		awaitSignal(t, shutdownChan, "the SIGTERM handler closing the shutdown channel")
	})

	t.Run("SIGINT", func(t *testing.T) {
		cfg := createTestConfig(t)
		defer cleanupTestData(t, cfg)

		srv, err := NewServer(cfg)
		require.NoError(t, err)

		shutdownChan := make(chan struct{})

		// Setup signal handlers
		setupSignalHandlers(shutdownChan, srv.logger)

		// Send SIGINT to ourselves. setupSignalHandlers registers its
		// signal.Notify channel synchronously, so the signal cannot be
		// missed and no delay is needed before sending it.
		proc, err := os.FindProcess(os.Getpid())
		require.NoError(t, err)
		require.NoError(t, proc.Signal(syscall.SIGINT))

		awaitSignal(t, shutdownChan, "the SIGINT handler closing the shutdown channel")
	})

	t.Run("MultipleSignals", func(t *testing.T) {
		cfg := createTestConfig(t)
		defer cleanupTestData(t, cfg)

		srv, err := NewServer(cfg)
		require.NoError(t, err)

		shutdownChan := make(chan struct{})

		// Setup signal handlers
		setupSignalHandlers(shutdownChan, srv.logger)

		// Send multiple signals (only the first should close the channel).
		proc, err := os.FindProcess(os.Getpid())
		require.NoError(t, err)
		require.NoError(t, proc.Signal(syscall.SIGTERM))

		// Wait for the first shutdown signal, then re-deliver: the
		// once-guarded close must not panic on the second delivery.
		awaitSignal(t, shutdownChan, "the first SIGTERM closing the shutdown channel")
		require.NoError(t, proc.Signal(syscall.SIGTERM))

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

		awaitSignal(t, srv.shutdownChan, "TriggerShutdown closing the shutdown channel")

		assert.False(t, srv.IsClosed()) // Shutdown method not called yet
	})
}
