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
	"os/signal"
	"syscall"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// setupSignalHandlers configures signal handling for graceful shutdown.
//
// The handler routes through the trigger callback (Server.TriggerShutdown)
// rather than closing the shutdown channel itself: the channel has exactly
// ONE close site, guarded by one sync.Once, so a signal racing a direct
// Shutdown call — the daemon's main handles SIGTERM by calling Shutdown,
// which triggers the same signal — cannot double-close the channel (#139).
func setupSignalHandlers(trigger func(), log *logger.Logger) {
	sigChan := make(chan os.Signal, 1)

	// Register for SIGTERM and SIGINT (Ctrl+C)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)

	// Handle signals in background
	go func() {
		for sig := range sigChan {
			log.Infof("Received signal: %v", sig)
			log.Info("Initiating graceful shutdown...")
			trigger()
		}
	}()

	log.Debug("Signal handlers configured (SIGTERM, SIGINT)")
}
