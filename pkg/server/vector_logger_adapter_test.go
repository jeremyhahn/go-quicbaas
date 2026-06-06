//go:build vector

package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// ---------------------------------------------------------------------------
// qrdbLoggerAdapter — full coverage of all logging methods
// ---------------------------------------------------------------------------

func createTestLoggerAdapter(t *testing.T) qrdbsdk.Logger {
	t.Helper()
	log := mustCreateLogger()
	return newQRDBLoggerAdapter(log)
}

func TestQRDBLoggerAdapter_DebugMethods(t *testing.T) {
	adapter := createTestLoggerAdapter(t)

	t.Run("Debug does not panic", func(t *testing.T) {
		require.NotPanics(t, func() {
			adapter.Debug("debug message")
		})
	})

	t.Run("Debug with extra args does not panic", func(t *testing.T) {
		require.NotPanics(t, func() {
			adapter.Debug("debug message", "extra", "args")
		})
	})

	t.Run("Debugf does not panic", func(t *testing.T) {
		require.NotPanics(t, func() {
			adapter.Debugf("debug %s %d", "formatted", 42)
		})
	})
}

func TestQRDBLoggerAdapter_InfoMethods(t *testing.T) {
	adapter := createTestLoggerAdapter(t)

	t.Run("Info does not panic", func(t *testing.T) {
		require.NotPanics(t, func() {
			adapter.Info("info message")
		})
	})

	t.Run("Info with extra args does not panic", func(t *testing.T) {
		require.NotPanics(t, func() {
			adapter.Info("info message", "key", "value")
		})
	})

	t.Run("Infof does not panic", func(t *testing.T) {
		require.NotPanics(t, func() {
			adapter.Infof("info %s", "formatted")
		})
	})
}

func TestQRDBLoggerAdapter_WarnMethods(t *testing.T) {
	adapter := createTestLoggerAdapter(t)

	t.Run("Warn does not panic", func(t *testing.T) {
		require.NotPanics(t, func() {
			adapter.Warn("warn message")
		})
	})

	t.Run("Warn with extra args does not panic", func(t *testing.T) {
		require.NotPanics(t, func() {
			adapter.Warn("warn message", "extra")
		})
	})

	t.Run("Warnf does not panic", func(t *testing.T) {
		require.NotPanics(t, func() {
			adapter.Warnf("warn %s", "formatted")
		})
	})
}

func TestQRDBLoggerAdapter_ErrorMethods(t *testing.T) {
	adapter := createTestLoggerAdapter(t)

	t.Run("Error does not panic", func(t *testing.T) {
		require.NotPanics(t, func() {
			adapter.Error("error message")
		})
	})

	t.Run("Error with extra args does not panic", func(t *testing.T) {
		require.NotPanics(t, func() {
			adapter.Error("error message", "detail")
		})
	})

	t.Run("Errorf does not panic", func(t *testing.T) {
		require.NotPanics(t, func() {
			adapter.Errorf("error %s", "formatted")
		})
	})
}

func TestQRDBLoggerAdapter_WithField(t *testing.T) {
	adapter := createTestLoggerAdapter(t)

	t.Run("returns new logger with field", func(t *testing.T) {
		newAdapter := adapter.WithField("key", "value")
		require.NotNil(t, newAdapter)
		// Verify the returned adapter is still usable
		require.NotPanics(t, func() {
			newAdapter.Info("with field message")
		})
	})
}

func TestQRDBLoggerAdapter_WithFields(t *testing.T) {
	adapter := createTestLoggerAdapter(t)

	t.Run("returns new logger with multiple fields", func(t *testing.T) {
		fields := map[string]interface{}{
			"key1": "value1",
			"key2": 42,
		}
		newAdapter := adapter.WithFields(fields)
		require.NotNil(t, newAdapter)
		require.NotPanics(t, func() {
			newAdapter.Info("with fields message")
		})
	})
}

func TestQRDBLoggerAdapter_WithError(t *testing.T) {
	adapter := createTestLoggerAdapter(t)

	t.Run("returns new logger with error", func(t *testing.T) {
		testErr := &ServerMessageError{Message: "test error"}
		newAdapter := adapter.WithError(testErr)
		require.NotNil(t, newAdapter)
		require.NotPanics(t, func() {
			newAdapter.Error("with error message")
		})
	})
}

func TestQRDBLoggerAdapter_InterfaceCompliance(t *testing.T) {
	t.Run("satisfies qrdb Logger interface", func(t *testing.T) {
		var _ qrdbsdk.Logger = createTestLoggerAdapter(t)
	})
}

func TestNewQRDBLoggerAdapter_NilSafe(t *testing.T) {
	t.Run("construction with valid logger succeeds", func(t *testing.T) {
		log := mustCreateLogger()
		adapter := newQRDBLoggerAdapter(log)
		assert.NotNil(t, adapter)
	})
}
