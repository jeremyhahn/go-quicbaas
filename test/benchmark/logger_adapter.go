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

package benchmark

import (
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// loggerAdapter adapts logger.Logger to interfaces.Logger.
type loggerAdapter struct {
	impl *logger.Logger
}

// newLoggerAdapter creates a new logger adapter.
func newLoggerAdapter(impl *logger.Logger) interfaces.Logger {
	return &loggerAdapter{impl: impl}
}

func (l *loggerAdapter) Debug(msg string, args ...any) {
	l.impl.Debug(msg, args...)
}

func (l *loggerAdapter) Debugf(format string, args ...interface{}) {
	l.impl.Debugf(format, args...)
}

func (l *loggerAdapter) Info(msg string, args ...any) {
	l.impl.Info(msg, args...)
}

func (l *loggerAdapter) Infof(format string, args ...interface{}) {
	l.impl.Infof(format, args...)
}

func (l *loggerAdapter) Warn(msg string, args ...any) {
	l.impl.Warn(msg, args...)
}

func (l *loggerAdapter) Warnf(format string, args ...interface{}) {
	l.impl.Warnf(format, args...)
}

func (l *loggerAdapter) Error(msg string, args ...any) {
	l.impl.Error(msg, args...)
}

func (l *loggerAdapter) Errorf(format string, args ...interface{}) {
	l.impl.Errorf(format, args...)
}

func (l *loggerAdapter) Fatal(msg string) {
	l.impl.Fatal(msg)
}

func (l *loggerAdapter) Fatalf(format string, args ...interface{}) {
	l.impl.Fatalf(format, args...)
}

func (l *loggerAdapter) WithField(key string, value interface{}) interfaces.Logger {
	return newLoggerAdapter(l.impl.WithField(key, value))
}

func (l *loggerAdapter) WithFields(fields map[string]interface{}) interfaces.Logger {
	return newLoggerAdapter(l.impl.WithFields(fields))
}

func (l *loggerAdapter) WithError(err error) interfaces.Logger {
	return newLoggerAdapter(l.impl.WithError(err))
}
