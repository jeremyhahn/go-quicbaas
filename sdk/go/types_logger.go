// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

package quicbaas

import (
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// Shared-logger re-exports. QuicBaaS is the single SDK frontage for the
// stack: downstream consumers (e.g. go-trusted-platform, whose boundary
// forbids importing go-qrdb directly) reach the shared qrdb structured
// logger through this SDK. Pure aliases — no transport state, no behavior.

// LogConfig holds configuration for the shared structured logger.
type LogConfig = qrdbsdk.LogConfig

// ConcreteLogger is the concrete shared structured logger implementation.
type ConcreteLogger = qrdbsdk.ConcreteLogger

// InvalidLogLevelError is returned when a LogConfig carries an unknown level.
type InvalidLogLevelError = qrdbsdk.InvalidLogLevelError

// NilLoggerHandlerError is returned by NewLoggerWithHandler for a nil handler.
type NilLoggerHandlerError = qrdbsdk.NilLoggerHandlerError

// NewLogger creates a new shared Logger with the given configuration.
var NewLogger = qrdbsdk.NewLogger

// NewLoggerWithHandler creates a Logger that emits through a caller-supplied
// slog.Handler — the injection seam for custom sinks (test writers,
// multi-handler fan-outs, JSON-line stores). Output/Format/File in the
// config are ignored; the handler owns rendering.
var NewLoggerWithHandler = qrdbsdk.NewLoggerWithHandler

// NewLoggerAdapter wraps a ConcreteLogger as the qrdb interfaces.Logger.
var NewLoggerAdapter = qrdbsdk.NewLoggerAdapter

// DefaultLogConfig returns a LogConfig with sensible defaults applied.
var DefaultLogConfig = qrdbsdk.DefaultLogConfig

// ContextWithLogger returns a context carrying the given logger; retrieve
// it with LoggerFromContext.
var ContextWithLogger = qrdbsdk.ContextWithLogger

// LoggerFromContext extracts the logger bound by ContextWithLogger.
var LoggerFromContext = qrdbsdk.LoggerFromContext

// Logger level extensions beyond the slog built-ins.
const (
	// LogLevelTrace is the shared logger's trace level (below debug).
	LogLevelTrace = qrdbsdk.LevelTrace
	// LogLevelFatal is the shared logger's fatal level.
	LogLevelFatal = qrdbsdk.LevelFatal
	// LogLevelPanic is the shared logger's panic level. NOTE: renders any
	// slog.Level value 16 as "PANIC" — consumers defining custom levels
	// must avoid 16.
	LogLevelPanic = qrdbsdk.LevelPanic
)
