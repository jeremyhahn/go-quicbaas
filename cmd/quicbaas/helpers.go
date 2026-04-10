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

// Package main provides helper functions for the QuicBaaS CLI.
package main

import (
	"fmt"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// loggerAdapter adapts *logger.Logger to implement interfaces.Logger properly.
// This is needed because *logger.Logger methods return *logger.Logger instead of interfaces.Logger.
type loggerAdapter struct {
	*logger.Logger
}

// WithError wraps the logger's WithError method to return interfaces.Logger.
func (l *loggerAdapter) WithError(err error) interfaces.Logger {
	return &loggerAdapter{Logger: l.Logger.WithError(err)}
}

// WithField wraps the logger's WithField method to return interfaces.Logger.
func (l *loggerAdapter) WithField(key string, value interface{}) interfaces.Logger {
	return &loggerAdapter{Logger: l.Logger.WithField(key, value)}
}

// WithFields wraps the logger's WithFields method to return interfaces.Logger.
func (l *loggerAdapter) WithFields(fields map[string]interface{}) interfaces.Logger {
	return &loggerAdapter{Logger: l.Logger.WithFields(fields)}
}

// asInterfaceLogger converts *logger.Logger to interfaces.Logger.
func asInterfaceLogger(l *logger.Logger) interfaces.Logger {
	return &loggerAdapter{Logger: l}
}

// formatBytes formats a byte count as a human-readable string.
func formatBytes(bytes uint64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := uint64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
