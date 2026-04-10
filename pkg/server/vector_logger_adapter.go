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

//go:build vector

package server

import (
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// qrdbLoggerAdapter wraps a quicbaas *logger.Logger to satisfy the
// qrdb interfaces.Logger interface. The qrdb Logger methods accept
// variadic args on Debug/Info/Warn/Error while quicbaas Logger does not.
type qrdbLoggerAdapter struct {
	log *logger.Logger
}

// newQRDBLoggerAdapter creates a new adapter for the qrdb logger interface.
func newQRDBLoggerAdapter(log *logger.Logger) xkmssdk.QRDBLogger {
	return &qrdbLoggerAdapter{log: log}
}

// Debug logs a debug-level message. Extra args are ignored since the
// underlying quicbaas logger does not support structured args on Debug.
func (a *qrdbLoggerAdapter) Debug(msg string, _ ...any) {
	a.log.Debug(msg)
}

// Debugf logs a formatted debug-level message.
func (a *qrdbLoggerAdapter) Debugf(format string, args ...interface{}) {
	a.log.Debugf(format, args...)
}

// Info logs an info-level message.
func (a *qrdbLoggerAdapter) Info(msg string, _ ...any) {
	a.log.Info(msg)
}

// Infof logs a formatted info-level message.
func (a *qrdbLoggerAdapter) Infof(format string, args ...interface{}) {
	a.log.Infof(format, args...)
}

// Warn logs a warning-level message.
func (a *qrdbLoggerAdapter) Warn(msg string, _ ...any) {
	a.log.Warn(msg)
}

// Warnf logs a formatted warning-level message.
func (a *qrdbLoggerAdapter) Warnf(format string, args ...interface{}) {
	a.log.Warnf(format, args...)
}

// Error logs an error-level message.
func (a *qrdbLoggerAdapter) Error(msg string, _ ...any) {
	a.log.Error(msg)
}

// Errorf logs a formatted error-level message.
func (a *qrdbLoggerAdapter) Errorf(format string, args ...interface{}) {
	a.log.Errorf(format, args...)
}

// Fatal logs a fatal-level message and exits.
func (a *qrdbLoggerAdapter) Fatal(msg string) {
	a.log.Fatal(msg)
}

// Fatalf logs a formatted fatal-level message and exits.
func (a *qrdbLoggerAdapter) Fatalf(format string, args ...interface{}) {
	a.log.Fatalf(format, args...)
}

// WithField creates a new logger with an additional field.
func (a *qrdbLoggerAdapter) WithField(key string, value interface{}) xkmssdk.QRDBLogger {
	return &qrdbLoggerAdapter{log: a.log.WithField(key, value)}
}

// WithFields creates a new logger with multiple additional fields.
func (a *qrdbLoggerAdapter) WithFields(fields map[string]interface{}) xkmssdk.QRDBLogger {
	return &qrdbLoggerAdapter{log: a.log.WithFields(fields)}
}

// WithError creates a new logger with an error field.
func (a *qrdbLoggerAdapter) WithError(err error) xkmssdk.QRDBLogger {
	return &qrdbLoggerAdapter{log: a.log.WithError(err)}
}
