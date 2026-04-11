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

// Package logger provides structured logging for QuicBaaS.
// It re-exports the canonical logger implementation from go-qrdb,
// preserving this package's import path for all existing callers.
package logger

import (
	"log/slog"

	sdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// Logger is the structured logger type. It re-exports the canonical
// implementation from go-qrdb so callers of this package are unaffected.
type Logger = sdk.ConcreteLogger

// Custom log levels for trace, fatal, and panic.
const (
	LevelTrace = slog.Level(-8)
	LevelFatal = slog.Level(12)
	LevelPanic = slog.Level(16)
)

// New creates a new Logger with the given configuration.
var New = sdk.NewLogger

// FromContext retrieves the logger stored in a context.
// If no logger is found, a default logger is returned.
var FromContext = sdk.LoggerFromContext
