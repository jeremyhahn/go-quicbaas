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
	"sync"
	"syscall"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// setupSignalHandlers configures signal handling for graceful shutdown.
func setupSignalHandlers(shutdown chan<- struct{}, log *logger.Logger) {
	sigChan := make(chan os.Signal, 1)

	// Register for SIGTERM and SIGINT (Ctrl+C)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)

	// Use sync.Once to ensure channel is closed only once
	var once sync.Once

	// Handle signals in background
	go func() {
		for sig := range sigChan {
			log.Infof("Received signal: %v", sig)
			log.Info("Initiating graceful shutdown...")

			// Trigger shutdown only once using select to avoid panic on closed channel
			once.Do(func() {
				defer func() {
					if r := recover(); r != nil {
						log.Debugf("Shutdown channel already closed: %v", r)
					}
				}()
				close(shutdown)
			})
		}
	}()

	log.Debug("Signal handlers configured (SIGTERM, SIGINT)")
}
