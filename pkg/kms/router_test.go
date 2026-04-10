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

package kms

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// =============================================================================
// Mock Logger for Testing
// =============================================================================

// mockLogger implements interfaces.Logger for testing purposes.
type routerTestLogger struct {
	mu       sync.Mutex
	messages *[]logEntry // shared slice pointer for all derived loggers
	fields   map[string]interface{}
}

type logEntry struct {
	level   string
	message string
	fields  map[string]interface{}
}

func newRouterTestLogger() *routerTestLogger {
	messages := make([]logEntry, 0)
	return &routerTestLogger{
		messages: &messages,
		fields:   make(map[string]interface{}),
	}
}

func (m *routerTestLogger) Debug(msg string) {
	m.log("debug", msg)
}

func (m *routerTestLogger) Debugf(format string, args ...interface{}) {
	m.log("debug", format)
}

func (m *routerTestLogger) Info(msg string) {
	m.log("info", msg)
}

func (m *routerTestLogger) Infof(format string, args ...interface{}) {
	m.log("info", format)
}

func (m *routerTestLogger) Warn(msg string) {
	m.log("warn", msg)
}

func (m *routerTestLogger) Warnf(format string, args ...interface{}) {
	m.log("warn", format)
}

func (m *routerTestLogger) Error(msg string) {
	m.log("error", msg)
}

func (m *routerTestLogger) Errorf(format string, args ...interface{}) {
	m.log("error", format)
}

func (m *routerTestLogger) Fatal(msg string) {
	m.log("fatal", msg)
}

func (m *routerTestLogger) Fatalf(format string, args ...interface{}) {
	m.log("fatal", format)
}

func (m *routerTestLogger) WithField(key string, value interface{}) interfaces.Logger {
	newLogger := &routerTestLogger{
		messages: m.messages, // share the same slice pointer
		fields:   make(map[string]interface{}),
	}
	for k, v := range m.fields {
		newLogger.fields[k] = v
	}
	newLogger.fields[key] = value
	return newLogger
}

func (m *routerTestLogger) WithFields(fields map[string]interface{}) interfaces.Logger {
	newLogger := &routerTestLogger{
		messages: m.messages, // share the same slice pointer
		fields:   make(map[string]interface{}),
	}
	for k, v := range m.fields {
		newLogger.fields[k] = v
	}
	for k, v := range fields {
		newLogger.fields[k] = v
	}
	return newLogger
}

func (m *routerTestLogger) WithError(err error) interfaces.Logger {
	return m.WithField("error", err.Error())
}

func (m *routerTestLogger) log(level, msg string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry := logEntry{
		level:   level,
		message: msg,
		fields:  make(map[string]interface{}),
	}
	for k, v := range m.fields {
		entry.fields[k] = v
	}
	*m.messages = append(*m.messages, entry)
}

func (m *routerTestLogger) getMessages() []logEntry {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]logEntry, len(*m.messages))
	copy(result, *m.messages)
	return result
}

func (m *routerTestLogger) hasMessageWithLevel(level string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, entry := range *m.messages {
		if entry.level == level {
			return true
		}
	}
	return false
}

// =============================================================================
// ParseKeyID Tests
// =============================================================================

func TestParseKeyID_Structured(t *testing.T) {
	tests := []struct {
		name     string
		keyID    string
		expected ParsedKeyID
	}{
		{
			name:  "full structured key ID",
			keyID: "hsm:signing:ecdsa-p256:my-signing-key",
			expected: ParsedKeyID{
				Backend:      "hsm",
				KeyType:      "signing",
				Algorithm:    "ecdsa-p256",
				KeyName:      "my-signing-key",
				Raw:          "hsm:signing:ecdsa-p256:my-signing-key",
				IsStructured: true,
			},
		},
		{
			name:  "structured with colons in key name",
			keyID: "vault:master:rsa-4096:root:key:name",
			expected: ParsedKeyID{
				Backend:      "vault",
				KeyType:      "master",
				Algorithm:    "rsa-4096",
				KeyName:      "root:key:name",
				Raw:          "vault:master:rsa-4096:root:key:name",
				IsStructured: true,
			},
		},
		{
			name:  "software backend",
			keyID: "software:encryption:aes-256-gcm:data-key",
			expected: ParsedKeyID{
				Backend:      "software",
				KeyType:      "encryption",
				Algorithm:    "aes-256-gcm",
				KeyName:      "data-key",
				Raw:          "software:encryption:aes-256-gcm:data-key",
				IsStructured: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseKeyID(tt.keyID)
			assert.Equal(t, tt.expected.Backend, result.Backend)
			assert.Equal(t, tt.expected.KeyType, result.KeyType)
			assert.Equal(t, tt.expected.Algorithm, result.Algorithm)
			assert.Equal(t, tt.expected.KeyName, result.KeyName)
			assert.Equal(t, tt.expected.Raw, result.Raw)
			assert.Equal(t, tt.expected.IsStructured, result.IsStructured)
		})
	}
}

func TestParseKeyID_Unstructured(t *testing.T) {
	tests := []struct {
		name     string
		keyID    string
		expected ParsedKeyID
	}{
		{
			name:  "simple key name",
			keyID: "my-simple-key",
			expected: ParsedKeyID{
				Backend:      "",
				KeyType:      "",
				Algorithm:    "",
				KeyName:      "my-simple-key",
				Raw:          "my-simple-key",
				IsStructured: false,
			},
		},
		{
			name:  "key with one colon (not enough parts)",
			keyID: "prefix:keyname",
			expected: ParsedKeyID{
				Backend:      "",
				KeyType:      "",
				Algorithm:    "",
				KeyName:      "prefix:keyname",
				Raw:          "prefix:keyname",
				IsStructured: false,
			},
		},
		{
			name:  "key with two colons (not enough parts)",
			keyID: "a:b:c",
			expected: ParsedKeyID{
				Backend:      "",
				KeyType:      "",
				Algorithm:    "",
				KeyName:      "a:b:c",
				Raw:          "a:b:c",
				IsStructured: false,
			},
		},
		{
			name:  "empty key",
			keyID: "",
			expected: ParsedKeyID{
				Backend:      "",
				KeyType:      "",
				Algorithm:    "",
				KeyName:      "",
				Raw:          "",
				IsStructured: false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseKeyID(tt.keyID)
			assert.Equal(t, tt.expected.Backend, result.Backend)
			assert.Equal(t, tt.expected.KeyType, result.KeyType)
			assert.Equal(t, tt.expected.Algorithm, result.Algorithm)
			assert.Equal(t, tt.expected.KeyName, result.KeyName)
			assert.Equal(t, tt.expected.Raw, result.Raw)
			assert.Equal(t, tt.expected.IsStructured, result.IsStructured)
		})
	}
}

// =============================================================================
// RoutingConfig Tests
// =============================================================================

func TestDefaultRoutingConfig(t *testing.T) {
	cfg := DefaultRoutingConfig()

	assert.True(t, cfg.FailoverEnabled)
	assert.Equal(t, 3, cfg.MaxFailoverAttempts)
	assert.NotNil(t, cfg.AlgorithmRouting)
	assert.NotNil(t, cfg.BackendTypeRouting)
	assert.Empty(t, cfg.DefaultBackend)
}

// =============================================================================
// BackendRouter Tests
// =============================================================================

func TestNewBackendRouter_NilConfig(t *testing.T) {
	registry := NewBackendRegistry(nil)
	router := NewBackendRouter(registry, nil)

	assert.NotNil(t, router)
	assert.NotNil(t, router.config)
	assert.True(t, router.config.FailoverEnabled)
}

func TestNewBackendRouter_WithOptions(t *testing.T) {
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()

	router := NewBackendRouter(registry, nil,
		WithDefaultBackend(backend),
	)

	assert.NotNil(t, router)
	assert.NotNil(t, router.defaultBackend)
}

func TestNewBackendRouter_WithLogger(t *testing.T) {
	registry := NewBackendRegistry(nil)
	logger := newRouterTestLogger()

	router := NewBackendRouter(registry, nil, WithLogger(logger))

	assert.NotNil(t, router)
	assert.NotNil(t, router.logger)
	assert.Equal(t, logger, router.logger)
}

func TestNewBackendRouter_WithMultipleOptions(t *testing.T) {
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	logger := newRouterTestLogger()

	router := NewBackendRouter(registry, nil,
		WithDefaultBackend(backend),
		WithLogger(logger),
	)

	assert.NotNil(t, router)
	assert.NotNil(t, router.defaultBackend)
	assert.NotNil(t, router.logger)
	assert.Equal(t, backend, router.defaultBackend)
	assert.Equal(t, logger, router.logger)
}

func TestBackendRouter_GetRegistry(t *testing.T) {
	registry := NewBackendRegistry(nil)
	router := NewBackendRouter(registry, nil)

	assert.Equal(t, registry, router.GetRegistry())
}

func TestBackendRouter_GetConfig(t *testing.T) {
	cfg := &RoutingConfig{
		DefaultBackend:      "test-backend",
		FailoverEnabled:     false,
		MaxFailoverAttempts: 5,
	}
	router := NewBackendRouter(nil, cfg)

	assert.Equal(t, cfg, router.GetConfig())
}

func TestBackendRouter_Route_WithStructuredKeyID(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()

	// Register backend with name matching key ID prefix
	err := registry.Register("software", backend, 100)
	require.NoError(t, err)

	router := NewBackendRouter(registry, nil)

	// Route using structured key ID
	result, err := router.Route(ctx, "software:encryption:aes-256-gcm:my-key", "")
	require.NoError(t, err)
	assert.Equal(t, backend, result)
}

func TestBackendRouter_Route_WithAlgorithm(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()

	err := registry.Register("software", backend, 100)
	require.NoError(t, err)

	router := NewBackendRouter(registry, nil)

	// Route using algorithm
	result, err := router.Route(ctx, "", AlgAES256GCM)
	require.NoError(t, err)
	assert.Equal(t, backend, result)
}

func TestBackendRouter_Route_WithDefaultBackend(t *testing.T) {
	ctx := context.Background()
	defaultBackend := NewMockBackend()

	router := NewBackendRouter(nil, &RoutingConfig{}, WithDefaultBackend(defaultBackend))

	// Route with no registry - should use default backend
	result, err := router.Route(ctx, "any-key", "")
	require.NoError(t, err)
	assert.Equal(t, defaultBackend, result)
}

func TestBackendRouter_Route_StructuredKeyID_NilRegistry_FallsBackToDefaultBackend(t *testing.T) {
	ctx := context.Background()
	defaultBackend := NewMockBackend()
	logger := newRouterTestLogger()

	// Create router with nil registry but with a default backend
	router := NewBackendRouter(nil, &RoutingConfig{}, WithDefaultBackend(defaultBackend), WithLogger(logger))

	// Route using structured key ID when registry is nil
	// This should trigger routeByBackendName with nil registry, which returns error
	// Then fall back to the default backend
	result, err := router.Route(ctx, "hsm:encryption:aes-256-gcm:my-key", "")
	require.NoError(t, err)
	assert.Equal(t, defaultBackend, result)

	// Verify that a debug log was created for the fallback
	assert.True(t, logger.hasMessageWithLevel("debug"))
}

func TestBackendRouter_Route_NoBackendAvailable(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)

	router := NewBackendRouter(registry, nil)

	// Route with empty registry and no default
	result, err := router.Route(ctx, "any-key", "")
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "no available backend")
}

func TestBackendRouter_Route_StructuredKeyIDBackendNotFound_WithLogger(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	logger := newRouterTestLogger()

	// Register a different backend than the one in the key ID
	err := registry.Register("software", backend, 100)
	require.NoError(t, err)

	router := NewBackendRouter(registry, nil, WithLogger(logger))

	// Route using structured key ID that references a non-existent backend
	// Should fall back to algorithm routing or default
	result, err := router.Route(ctx, "hsm:encryption:aes-256-gcm:my-key", AlgAES256GCM)
	require.NoError(t, err)
	assert.Equal(t, backend, result)

	// Verify that a debug log was created for the fallback
	assert.True(t, logger.hasMessageWithLevel("debug"))
}

func TestBackendRouter_Route_StructuredKeyIDBackendNotFound_WithoutLogger(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()

	// Register a different backend than the one in the key ID
	err := registry.Register("software", backend, 100)
	require.NoError(t, err)

	router := NewBackendRouter(registry, nil) // No logger

	// Route using structured key ID that references a non-existent backend
	result, err := router.Route(ctx, "hsm:encryption:aes-256-gcm:my-key", AlgAES256GCM)
	require.NoError(t, err)
	assert.Equal(t, backend, result)
}

func TestBackendRouter_RouteByKeyID(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()

	err := registry.Register("hsm", backend, 100)
	require.NoError(t, err)

	router := NewBackendRouter(registry, nil)

	result, err := router.RouteByKeyID(ctx, "hsm:signing:ecdsa-p256:my-key")
	require.NoError(t, err)
	assert.Equal(t, backend, result)
}

func TestBackendRouter_RouteByAlgorithm(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()

	err := registry.Register("software", backend, 100)
	require.NoError(t, err)

	router := NewBackendRouter(registry, nil)

	result, err := router.RouteByAlgorithm(ctx, AlgChaCha20Poly1305)
	require.NoError(t, err)
	assert.Equal(t, backend, result)
}

func TestBackendRouter_RouteWithFailover_Success(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()

	err := registry.Register("primary", backend, 100)
	require.NoError(t, err)

	router := NewBackendRouter(registry, nil)

	operationCalled := false
	err = router.RouteWithFailover(ctx, "", AlgAES256GCM, func(b Backend) error {
		operationCalled = true
		return nil
	})
	require.NoError(t, err)
	assert.True(t, operationCalled)
}

func TestBackendRouter_RouteWithFailover_Failover(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	primary := NewMockBackend()
	secondary := NewMockBackend()

	err := registry.Register("primary", primary, 100)
	require.NoError(t, err)
	err = registry.Register("secondary", secondary, 50)
	require.NoError(t, err)

	router := NewBackendRouter(registry, nil)

	callCount := 0
	err = router.RouteWithFailover(ctx, "", AlgAES256GCM, func(b Backend) error {
		callCount++
		if callCount == 1 {
			return errors.New("primary failed")
		}
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, 2, callCount)
}

func TestBackendRouter_RouteWithFailover_AllFailed(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()

	err := registry.Register("backend", backend, 100)
	require.NoError(t, err)

	router := NewBackendRouter(registry, &RoutingConfig{
		FailoverEnabled:     true,
		MaxFailoverAttempts: 2,
	})

	err = router.RouteWithFailover(ctx, "", AlgAES256GCM, func(b Backend) error {
		return errors.New("operation failed")
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "all failover attempts failed")
}

func TestBackendRouter_RouteWithFailover_Disabled(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()

	err := registry.Register("backend", backend, 100)
	require.NoError(t, err)

	router := NewBackendRouter(registry, &RoutingConfig{
		FailoverEnabled: false,
	})

	err = router.RouteWithFailover(ctx, "", AlgAES256GCM, func(b Backend) error {
		return errors.New("operation failed")
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "operation failed")
}

func TestBackendRouter_RouteWithFailover_NoBackends(t *testing.T) {
	ctx := context.Background()
	router := NewBackendRouter(nil, nil)

	err := router.RouteWithFailover(ctx, "", "", func(b Backend) error {
		return nil
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no backends available")
}

func TestBackendRouter_RouteWithFailover_WithLogger(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	primary := NewMockBackend()
	secondary := NewMockBackend()
	logger := newRouterTestLogger()

	err := registry.Register("primary", primary, 100)
	require.NoError(t, err)
	err = registry.Register("secondary", secondary, 50)
	require.NoError(t, err)

	router := NewBackendRouter(registry, nil, WithLogger(logger))

	callCount := 0
	err = router.RouteWithFailover(ctx, "", AlgAES256GCM, func(b Backend) error {
		callCount++
		if callCount == 1 {
			return errors.New("primary failed")
		}
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, 2, callCount)

	// Verify that a warning log was created for the failover
	assert.True(t, logger.hasMessageWithLevel("warn"))
}

func TestBackendRouter_RouteWithFailover_AllFailedWithLogger(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	logger := newRouterTestLogger()

	err := registry.Register("backend", backend, 100)
	require.NoError(t, err)

	router := NewBackendRouter(registry, &RoutingConfig{
		FailoverEnabled:     true,
		MaxFailoverAttempts: 2,
	}, WithLogger(logger))

	err = router.RouteWithFailover(ctx, "", AlgAES256GCM, func(b Backend) error {
		return errors.New("operation failed")
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "all failover attempts failed")

	// Verify warning logs were created for each failed attempt
	assert.True(t, logger.hasMessageWithLevel("warn"))
}

func TestBackendRouter_RouteWithFailover_DisabledNoRoute(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)

	router := NewBackendRouter(registry, &RoutingConfig{
		FailoverEnabled: false,
	})

	// Empty registry, should fail to route
	err := router.RouteWithFailover(ctx, "", AlgAES256GCM, func(b Backend) error {
		return nil
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no available backend")
}

func TestBackendRouter_RouteWithFailover_MaxAttemptsZeroUsesBackendCount(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend1 := NewMockBackend()
	backend2 := NewMockBackend()
	backend3 := NewMockBackend()

	err := registry.Register("backend1", backend1, 100)
	require.NoError(t, err)
	err = registry.Register("backend2", backend2, 50)
	require.NoError(t, err)
	err = registry.Register("backend3", backend3, 25)
	require.NoError(t, err)

	router := NewBackendRouter(registry, &RoutingConfig{
		FailoverEnabled:     true,
		MaxFailoverAttempts: 0, // Zero means use number of backends
	})

	callCount := 0
	err = router.RouteWithFailover(ctx, "", AlgAES256GCM, func(b Backend) error {
		callCount++
		return errors.New("always fail")
	})
	require.Error(t, err)
	assert.Equal(t, 3, callCount) // Should try all 3 backends
}

func TestBackendRouter_RouteWithAlgorithmRouting(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	softwareBackend := NewMockBackend()
	hsmBackend := NewMockBackend()

	err := registry.Register("software", softwareBackend, 100)
	require.NoError(t, err)
	err = registry.Register("hsm", hsmBackend, 50)
	require.NoError(t, err)

	// Configure algorithm routing
	cfg := &RoutingConfig{
		FailoverEnabled:     true,
		MaxFailoverAttempts: 3,
		AlgorithmRouting: map[string]string{
			AlgChaCha20Poly1305: "software",
			AlgRSA4096:          "hsm",
		},
	}

	router := NewBackendRouter(registry, cfg)

	// Route ChaCha20 to software backend
	result, err := router.Route(ctx, "", AlgChaCha20Poly1305)
	require.NoError(t, err)
	assert.Equal(t, softwareBackend, result)

	// Route RSA to HSM backend
	result, err = router.Route(ctx, "", AlgRSA4096)
	require.NoError(t, err)
	assert.Equal(t, hsmBackend, result)
}

func TestBackendRouter_RouteWithAlgorithmRouting_BackendNotFound(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	softwareBackend := NewMockBackend()

	err := registry.Register("software", softwareBackend, 100)
	require.NoError(t, err)

	// Configure algorithm routing pointing to non-existent backend
	cfg := &RoutingConfig{
		FailoverEnabled:     true,
		MaxFailoverAttempts: 3,
		AlgorithmRouting: map[string]string{
			AlgRSA4096: "hsm", // hsm not registered
		},
	}

	router := NewBackendRouter(registry, cfg)

	// Should fall back to registry's algorithm routing
	result, err := router.Route(ctx, "", AlgRSA4096)
	require.NoError(t, err)
	assert.Equal(t, softwareBackend, result)
}

func TestBackendRouter_RouteWithDefaultBackendConfig(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()

	err := registry.Register("default-backend", backend, 100)
	require.NoError(t, err)

	cfg := &RoutingConfig{
		DefaultBackend: "default-backend",
	}

	router := NewBackendRouter(registry, cfg)

	// Should route to default backend when no other rules match
	result, err := router.Route(ctx, "unknown-key", "")
	require.NoError(t, err)
	assert.Equal(t, backend, result)
}

func TestBackendRouter_RouteWithDefaultBackendConfig_NotFound(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()

	err := registry.Register("other-backend", backend, 100)
	require.NoError(t, err)

	cfg := &RoutingConfig{
		DefaultBackend: "non-existent-backend",
	}

	router := NewBackendRouter(registry, cfg)

	// Should fall back to highest priority healthy backend
	result, err := router.Route(ctx, "unknown-key", "")
	require.NoError(t, err)
	assert.Equal(t, backend, result)
}

// =============================================================================
// getFailoverCandidates Tests
// =============================================================================

func TestBackendRouter_getFailoverCandidates_NoRegistry(t *testing.T) {
	ctx := context.Background()
	router := NewBackendRouter(nil, nil)

	candidates := router.getFailoverCandidates(ctx, "")
	assert.Nil(t, candidates)
}

func TestBackendRouter_getFailoverCandidates_NoRegistryWithDefaultBackend(t *testing.T) {
	ctx := context.Background()
	defaultBackend := NewMockBackend()
	router := NewBackendRouter(nil, nil, WithDefaultBackend(defaultBackend))

	candidates := router.getFailoverCandidates(ctx, "")
	require.Len(t, candidates, 1)
	assert.Equal(t, defaultBackend, candidates[0])
}

func TestBackendRouter_getFailoverCandidates_EmptyRegistry(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	router := NewBackendRouter(registry, nil)

	candidates := router.getFailoverCandidates(ctx, "")
	assert.Empty(t, candidates)
}

func TestBackendRouter_getFailoverCandidates_AllUnhealthy(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend1 := NewMockBackend()
	backend2 := NewMockBackend()

	err := registry.Register("backend1", backend1, 100)
	require.NoError(t, err)
	err = registry.Register("backend2", backend2, 50)
	require.NoError(t, err)

	// Mark both backends as unhealthy
	registry.mu.Lock()
	for _, info := range registry.backends {
		info.Health.Status = HealthStatusUnhealthy
	}
	registry.mu.Unlock()

	router := NewBackendRouter(registry, nil)

	candidates := router.getFailoverCandidates(ctx, "")
	assert.Empty(t, candidates)
}

func TestBackendRouter_getFailoverCandidates_AllUnhealthyWithDefaultBackend(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend1 := NewMockBackend()
	backend2 := NewMockBackend()
	defaultBackend := NewMockBackend()

	err := registry.Register("backend1", backend1, 100)
	require.NoError(t, err)
	err = registry.Register("backend2", backend2, 50)
	require.NoError(t, err)

	// Mark both backends as unhealthy
	registry.mu.Lock()
	for _, info := range registry.backends {
		info.Health.Status = HealthStatusUnhealthy
	}
	registry.mu.Unlock()

	router := NewBackendRouter(registry, nil, WithDefaultBackend(defaultBackend))

	candidates := router.getFailoverCandidates(ctx, "")
	require.Len(t, candidates, 1)
	assert.Equal(t, defaultBackend, candidates[0])
}

func TestBackendRouter_getFailoverCandidates_DisabledBackends(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend1 := NewMockBackend()
	backend2 := NewMockBackend()

	err := registry.Register("backend1", backend1, 100)
	require.NoError(t, err)
	err = registry.Register("backend2", backend2, 50)
	require.NoError(t, err)

	// Disable one backend
	err = registry.Disable("backend1")
	require.NoError(t, err)

	router := NewBackendRouter(registry, nil)

	candidates := router.getFailoverCandidates(ctx, "")
	require.Len(t, candidates, 1)
	assert.Equal(t, backend2, candidates[0])
}

func TestBackendRouter_getFailoverCandidates_WithAlgorithmFilter(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	aesBackend := NewMockBackend()
	rsaBackend := NewMockBackend()

	err := registry.Register("aes-backend", aesBackend, 100)
	require.NoError(t, err)
	err = registry.Register("rsa-backend", rsaBackend, 50)
	require.NoError(t, err)

	// Manually set algorithms: aes-backend only supports AES, rsa-backend only supports RSA
	registry.mu.Lock()
	registry.backends["aes-backend"].Algorithms = []string{AlgAES256GCM, AlgAES128GCM}
	registry.backends["rsa-backend"].Algorithms = []string{AlgRSA2048, AlgRSA4096}
	registry.mu.Unlock()

	router := NewBackendRouter(registry, nil)

	// Filter by AES algorithm - only aes-backend should match
	candidates := router.getFailoverCandidates(ctx, AlgAES256GCM)
	require.Len(t, candidates, 1)
	assert.Equal(t, aesBackend, candidates[0])

	// Filter by RSA algorithm - only rsaBackend should match
	candidates = router.getFailoverCandidates(ctx, AlgRSA4096)
	require.Len(t, candidates, 1)
	assert.Equal(t, rsaBackend, candidates[0])

	// Filter by unsupported algorithm - no backends should match
	candidates = router.getFailoverCandidates(ctx, AlgEd25519)
	assert.Empty(t, candidates)
}

func TestBackendRouter_getFailoverCandidates_DegradedBackendIncluded(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()

	err := registry.Register("backend", backend, 100)
	require.NoError(t, err)

	// Mark backend as degraded
	registry.mu.Lock()
	registry.backends["backend"].Health.Status = HealthStatusDegraded
	registry.mu.Unlock()

	router := NewBackendRouter(registry, nil)

	candidates := router.getFailoverCandidates(ctx, "")
	require.Len(t, candidates, 1)
	assert.Equal(t, backend, candidates[0])
}

func TestBackendRouter_getFailoverCandidates_UnknownStatusBackendIncluded(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()

	err := registry.Register("backend", backend, 100)
	require.NoError(t, err)

	// Backend should have unknown status by default
	registry.mu.RLock()
	status := registry.backends["backend"].Health.Status
	registry.mu.RUnlock()
	assert.Equal(t, HealthStatusUnknown, status)

	router := NewBackendRouter(registry, nil)

	candidates := router.getFailoverCandidates(ctx, "")
	require.Len(t, candidates, 1)
	assert.Equal(t, backend, candidates[0])
}

func TestBackendRouter_getFailoverCandidates_DefaultBackendNotDuplicated(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	defaultBackend := NewMockBackend()

	// Register the default backend in the registry as well
	err := registry.Register("default", defaultBackend, 100)
	require.NoError(t, err)

	router := NewBackendRouter(registry, nil, WithDefaultBackend(defaultBackend))

	candidates := router.getFailoverCandidates(ctx, "")
	require.Len(t, candidates, 1) // Should not be duplicated
	assert.Equal(t, defaultBackend, candidates[0])
}

func TestBackendRouter_getFailoverCandidates_DefaultBackendAddedAsLastResort(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	registeredBackend := NewMockBackend()
	defaultBackend := NewMockBackend()

	err := registry.Register("registered", registeredBackend, 100)
	require.NoError(t, err)

	router := NewBackendRouter(registry, nil, WithDefaultBackend(defaultBackend))

	candidates := router.getFailoverCandidates(ctx, "")
	require.Len(t, candidates, 2)
	// Default backend should be added as last resort
	assert.Equal(t, registeredBackend, candidates[0])
	assert.Equal(t, defaultBackend, candidates[1])
}

func TestBackendRouter_getFailoverCandidates_NilBackendInRegistry(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()

	err := registry.Register("backend", backend, 100)
	require.NoError(t, err)

	// Manually set one backend to nil
	registry.mu.Lock()
	registry.backends["backend"].Backend = nil
	registry.mu.Unlock()

	router := NewBackendRouter(registry, nil)

	candidates := router.getFailoverCandidates(ctx, "")
	assert.Empty(t, candidates)
}

func TestBackendRouter_getFailoverCandidates_HealthyBackendIncluded(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()

	err := registry.Register("backend", backend, 100)
	require.NoError(t, err)

	// Mark backend as healthy
	registry.mu.Lock()
	registry.backends["backend"].Health.Status = HealthStatusHealthy
	registry.mu.Unlock()

	router := NewBackendRouter(registry, nil)

	candidates := router.getFailoverCandidates(ctx, "")
	require.Len(t, candidates, 1)
	assert.Equal(t, backend, candidates[0])
}

func TestBackendRouter_getFailoverCandidates_MixedHealthStatus(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	healthyBackend := NewMockBackend()
	degradedBackend := NewMockBackend()
	unhealthyBackend := NewMockBackend()

	err := registry.Register("healthy", healthyBackend, 100)
	require.NoError(t, err)
	err = registry.Register("degraded", degradedBackend, 50)
	require.NoError(t, err)
	err = registry.Register("unhealthy", unhealthyBackend, 25)
	require.NoError(t, err)

	registry.mu.Lock()
	registry.backends["healthy"].Health.Status = HealthStatusHealthy
	registry.backends["degraded"].Health.Status = HealthStatusDegraded
	registry.backends["unhealthy"].Health.Status = HealthStatusUnhealthy
	registry.mu.Unlock()

	router := NewBackendRouter(registry, nil)

	candidates := router.getFailoverCandidates(ctx, "")
	require.Len(t, candidates, 2) // healthy and degraded, not unhealthy

	// Verify the backends are the expected ones
	var foundHealthy, foundDegraded bool
	for _, c := range candidates {
		if c == healthyBackend {
			foundHealthy = true
		}
		if c == degradedBackend {
			foundDegraded = true
		}
	}
	assert.True(t, foundHealthy, "healthy backend should be in candidates")
	assert.True(t, foundDegraded, "degraded backend should be in candidates")
}

// =============================================================================
// Multi-Backend Manager Tests
// =============================================================================

func TestManager_WithRegistry(t *testing.T) {
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()

	err := registry.Register("software", backend, 100)
	require.NoError(t, err)

	manager, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	// Registry and router are always present
	assert.NotNil(t, manager.GetRegistry())
	assert.NotNil(t, manager.GetRouter())
}

func TestManager_WithDefaultBackendFallback(t *testing.T) {
	registry := NewBackendRegistry(nil)
	defaultBackend := NewMockBackend()
	hsmBackend := NewMockBackend()

	err := registry.Register("hsm", hsmBackend, 100)
	require.NoError(t, err)

	manager, err := NewKMSService(&KMSServiceOptions{
		Registry:       registry,
		DefaultBackend: defaultBackend, // Used as default fallback
	})
	require.NoError(t, err)

	assert.Equal(t, config.BackendTypeSoftware, manager.GetBackendType())
}

func TestManager_MultiBackend_IsAvailable(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend1 := NewMockBackend()
	backend2 := NewMockBackend()

	err := registry.Register("backend1", backend1, 100)
	require.NoError(t, err)
	err = registry.Register("backend2", backend2, 50)
	require.NoError(t, err)

	manager, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	assert.True(t, manager.IsAvailable(ctx))
}

func TestManager_MultiBackend_GetSupportedAlgorithms(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)
	backend1 := NewMockBackend()
	backend2 := NewMockBackend()

	err := registry.Register("backend1", backend1, 100)
	require.NoError(t, err)
	err = registry.Register("backend2", backend2, 50)
	require.NoError(t, err)

	manager, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	algorithms, err := manager.GetSupportedAlgorithms(ctx)
	require.NoError(t, err)

	// Should include algorithms from all backends
	assert.GreaterOrEqual(t, len(algorithms), 2)
}

func TestManager_MultiBackend_Close(t *testing.T) {
	registry := NewBackendRegistry(nil)
	backend1 := NewMockBackend()
	backend2 := NewMockBackend()

	err := registry.Register("backend1", backend1, 100)
	require.NoError(t, err)
	err = registry.Register("backend2", backend2, 50)
	require.NoError(t, err)

	manager, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	err = manager.Close()
	require.NoError(t, err)
}
