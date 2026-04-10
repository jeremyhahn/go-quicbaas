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

package cache

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

func TestLoggerAdapter_AllMethods(t *testing.T) {
	cfg := logger.LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	}
	impl, err := logger.New(cfg)
	require.NoError(t, err)

	adapter := newLoggerAdapter(impl)

	t.Run("Debugf", func(t *testing.T) {
		// Should not panic
		adapter.Debugf("debug message: %s", "test")
	})

	t.Run("Infof", func(t *testing.T) {
		// Should not panic
		adapter.Infof("info message: %d", 123)
	})

	t.Run("Warnf", func(t *testing.T) {
		// Should not panic
		adapter.Warnf("warning message: %v", map[string]string{"key": "value"})
	})

	t.Run("Error", func(t *testing.T) {
		// Should not panic
		adapter.Error("error message")
	})

	t.Run("Errorf", func(t *testing.T) {
		// Should not panic
		adapter.Errorf("error message: %v", errors.New("test error"))
	})

	t.Run("WithError", func(t *testing.T) {
		// Should not panic and return a new logger
		testErr := errors.New("test error")
		newLogger := adapter.WithError(testErr)
		assert.NotNil(t, newLogger)

		// Should be usable
		newLogger.Error("error with context")
	})

	t.Run("WithField returns functional logger", func(t *testing.T) {
		fieldLogger := adapter.WithField("request_id", "123")
		assert.NotNil(t, fieldLogger)

		// Verify it's functional
		fieldLogger.Debug("test message")
		fieldLogger.Debugf("test %s", "formatted")
	})

	t.Run("WithFields returns functional logger", func(t *testing.T) {
		fieldsLogger := adapter.WithFields(map[string]interface{}{
			"user_id": "456",
			"action":  "login",
		})
		assert.NotNil(t, fieldsLogger)

		// Verify it's functional
		fieldsLogger.Info("test message")
		fieldsLogger.Infof("test %s", "formatted")
	})

	t.Run("chained logger operations", func(t *testing.T) {
		// Test chaining multiple logger operations
		chainedLogger := adapter.
			WithField("component", "cache").
			WithFields(map[string]interface{}{
				"operation": "get",
				"key":       "test-key",
			}).
			WithError(errors.New("cache miss"))

		assert.NotNil(t, chainedLogger)

		// Should be usable
		chainedLogger.Warn("chained logger warning")
		chainedLogger.Warnf("formatted warning: %s", "test")
	})
}
