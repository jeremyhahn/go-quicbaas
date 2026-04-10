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

//go:build integration

package helpers

import (
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// CreateTestLoggerConcrete creates a concrete logger for integration tests
func CreateTestLoggerConcrete(t *testing.T) *logger.Logger {
	t.Helper()
	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	if err != nil {
		t.Fatalf("Failed to create test logger: %v", err)
	}
	return log
}

// LoggerAdapter adapts logger.Logger to interfaces.Logger
type LoggerAdapter struct {
	impl *logger.Logger
}

// NewLoggerAdapter creates a new logger adapter
func NewLoggerAdapter(impl *logger.Logger) interfaces.Logger {
	return &LoggerAdapter{impl: impl}
}

func (l *LoggerAdapter) Debug(msg string) {
	l.impl.Debug(msg)
}

func (l *LoggerAdapter) Debugf(format string, args ...interface{}) {
	l.impl.Debugf(format, args...)
}

func (l *LoggerAdapter) Info(msg string) {
	l.impl.Info(msg)
}

func (l *LoggerAdapter) Infof(format string, args ...interface{}) {
	l.impl.Infof(format, args...)
}

func (l *LoggerAdapter) Warn(msg string) {
	l.impl.Warn(msg)
}

func (l *LoggerAdapter) Warnf(format string, args ...interface{}) {
	l.impl.Warnf(format, args...)
}

func (l *LoggerAdapter) Error(msg string) {
	l.impl.Error(msg)
}

func (l *LoggerAdapter) Errorf(format string, args ...interface{}) {
	l.impl.Errorf(format, args...)
}

func (l *LoggerAdapter) Fatal(msg string) {
	l.impl.Fatal(msg)
}

func (l *LoggerAdapter) Fatalf(format string, args ...interface{}) {
	l.impl.Fatalf(format, args...)
}

func (l *LoggerAdapter) WithField(key string, value interface{}) interfaces.Logger {
	return NewLoggerAdapter(l.impl.WithField(key, value))
}

func (l *LoggerAdapter) WithFields(fields map[string]interface{}) interfaces.Logger {
	return NewLoggerAdapter(l.impl.WithFields(fields))
}

func (l *LoggerAdapter) WithError(err error) interfaces.Logger {
	return NewLoggerAdapter(l.impl.WithError(err))
}
