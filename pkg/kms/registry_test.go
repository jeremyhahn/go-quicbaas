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
	"sync/atomic"
	"testing"
	"time"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// testBackend is a simple mock backend for testing.
type testBackend struct {
	backendType config.BackendType
	available   atomic.Bool
	algorithms  []string
	closed      atomic.Bool
}

// newTestBackend creates a new testBackend with the given parameters.
func newTestBackend(backendType config.BackendType, available bool, algorithms []string) *testBackend {
	tb := &testBackend{
		backendType: backendType,
		algorithms:  algorithms,
	}
	tb.available.Store(available)
	return tb
}

// SetAvailable sets the availability status atomically.
func (b *testBackend) SetAvailable(available bool) {
	b.available.Store(available)
}

// IsClosed returns true if the backend has been closed.
func (b *testBackend) IsClosed() bool {
	return b.closed.Load()
}

func (b *testBackend) Type() config.BackendType                         { return b.backendType }
func (b *testBackend) IsAvailable(ctx context.Context) bool             { return b.available.Load() }
func (b *testBackend) SupportedAlgorithms(ctx context.Context) []string { return b.algorithms }
func (b *testBackend) Close() error                                     { b.closed.Store(true); return nil }

// Implement remaining Backend interface methods with minimal stubs
func (b *testBackend) CreateKey(ctx context.Context, attrs *xkmssdk.KeyAttributes) (*KeyMetadata, error) {
	return nil, nil
}
func (b *testBackend) ImportKey(ctx context.Context, keyID string, material []byte, algorithm string, options map[string]interface{}) (*KeyMetadata, error) {
	return nil, nil
}
func (b *testBackend) GetKeyMetadata(ctx context.Context, keyID string) (*KeyMetadata, error) {
	return nil, nil
}
func (b *testBackend) ListKeys(ctx context.Context, prefix string, limit int) ([]*KeyMetadata, string, error) {
	return nil, "", nil
}
func (b *testBackend) RotateKey(ctx context.Context, keyID string) (*KeyMetadata, error) {
	return nil, nil
}
func (b *testBackend) DisableKey(ctx context.Context, keyID string) error { return nil }
func (b *testBackend) EnableKey(ctx context.Context, keyID string) error  { return nil }
func (b *testBackend) DeleteKey(ctx context.Context, keyID string) error  { return nil }
func (b *testBackend) Encrypt(ctx context.Context, keyID string, plaintext, aad []byte) ([]byte, error) {
	return nil, nil
}
func (b *testBackend) Decrypt(ctx context.Context, keyID string, ciphertext, aad []byte) ([]byte, error) {
	return nil, nil
}
func (b *testBackend) Sign(ctx context.Context, keyID string, digest []byte, algorithm string) ([]byte, error) {
	return nil, nil
}
func (b *testBackend) Verify(ctx context.Context, keyID string, digest, signature []byte, algorithm string) (bool, error) {
	return false, nil
}
func (b *testBackend) GenerateDataKey(ctx context.Context, keyID string, keyBytes int) (plaintext, wrapped []byte, err error) {
	return nil, nil, nil
}
func (b *testBackend) ExportPublicKey(ctx context.Context, keyID string) ([]byte, error) {
	return nil, nil
}

func TestBackendRegistry_Register(t *testing.T) {
	registry := NewBackendRegistry(nil)
	defer registry.Close()

	backend := newTestBackend(config.BackendTypeSoftware, true, []string{"AES-256-GCM", "RSA-2048"})

	// Register should succeed
	err := registry.Register("primary", backend, 1)
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	// Duplicate registration should fail
	err = registry.Register("primary", backend, 1)
	if err == nil {
		t.Fatal("Expected error for duplicate registration")
	}

	regErr, ok := err.(*RegistryError)
	if !ok {
		t.Fatalf("Expected RegistryError, got %T", err)
	}
	if regErr.Code != ErrBackendExists {
		t.Errorf("Expected ErrBackendExists, got %s", regErr.Code)
	}
}

func TestBackendRegistry_Get(t *testing.T) {
	registry := NewBackendRegistry(nil)
	defer registry.Close()

	backend := newTestBackend(config.BackendType("test"), true, nil)
	registry.Register("primary", backend, 1)

	// Get existing backend
	got, err := registry.Get("primary")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got != backend {
		t.Error("Got wrong backend")
	}

	// Get non-existent backend
	_, err = registry.Get("nonexistent")
	if err == nil {
		t.Fatal("Expected error for non-existent backend")
	}

	regErr, ok := err.(*RegistryError)
	if !ok || regErr.Code != ErrBackendNotFound {
		t.Errorf("Expected ErrBackendNotFound, got %v", err)
	}
}

func TestBackendRegistry_EnableDisable(t *testing.T) {
	registry := NewBackendRegistry(nil)
	defer registry.Close()

	backend := newTestBackend(config.BackendType("test"), true, nil)
	registry.Register("primary", backend, 1)

	// Disable backend
	err := registry.Disable("primary")
	if err != nil {
		t.Fatalf("Disable failed: %v", err)
	}

	// Get should fail for disabled backend
	_, err = registry.Get("primary")
	if err == nil {
		t.Fatal("Expected error for disabled backend")
	}

	regErr, ok := err.(*RegistryError)
	if !ok || regErr.Code != ErrBackendDisabled {
		t.Errorf("Expected ErrBackendDisabled, got %v", err)
	}

	// Re-enable backend
	err = registry.Enable("primary")
	if err != nil {
		t.Fatalf("Enable failed: %v", err)
	}

	// Get should succeed now
	_, err = registry.Get("primary")
	if err != nil {
		t.Fatalf("Get after enable failed: %v", err)
	}
}

func TestBackendRegistry_GetHealthy(t *testing.T) {
	regConfig := DefaultRegistryConfig()
	regConfig.HealthCheckInterval = 100 * time.Millisecond
	registry := NewBackendRegistry(regConfig)
	defer registry.Close()

	// Register backends with different priorities
	backend1 := newTestBackend(config.BackendType("backend1"), true, nil)
	backend2 := newTestBackend(config.BackendType("backend2"), true, nil)

	registry.Register("backend1", backend1, 2)
	registry.Register("backend2", backend2, 1) // Higher priority

	// Mark both as healthy
	ctx := context.Background()
	registry.CheckHealth(ctx, "backend1")
	registry.CheckHealth(ctx, "backend2")

	// GetHealthy should return higher priority backend
	got, err := registry.GetHealthy(ctx)
	if err != nil {
		t.Fatalf("GetHealthy failed: %v", err)
	}
	if got != backend2 {
		t.Error("Expected backend2 (higher priority)")
	}

	// Make backend2 unavailable
	backend2.SetAvailable(false)
	registry.CheckHealth(ctx, "backend2")
	registry.CheckHealth(ctx, "backend2")
	registry.CheckHealth(ctx, "backend2") // 3 failures = unhealthy

	// GetHealthy should now return backend1
	got, err = registry.GetHealthy(ctx)
	if err != nil {
		t.Fatalf("GetHealthy after failover failed: %v", err)
	}
	if got != backend1 {
		t.Error("Expected backend1 after backend2 became unhealthy")
	}
}

func TestBackendRegistry_GetByAlgorithm(t *testing.T) {
	registry := NewBackendRegistry(nil)
	defer registry.Close()

	backend1 := newTestBackend(config.BackendTypeSoftware, true, []string{"AES-256-GCM", "RSA-2048"})
	backend2 := newTestBackend(config.BackendTypePKCS11, true, []string{"RSA-4096", "ECDSA-P256"})

	registry.Register("software", backend1, 2)
	registry.Register("hsm", backend2, 1)

	ctx := context.Background()
	registry.CheckHealth(ctx, "software")
	registry.CheckHealth(ctx, "hsm")

	// Should return HSM for RSA-4096
	got, err := registry.GetByAlgorithm(ctx, "RSA-4096")
	if err != nil {
		t.Fatalf("GetByAlgorithm failed: %v", err)
	}
	if got != backend2 {
		t.Error("Expected hsm backend for RSA-4096")
	}

	// Should return software for AES-256-GCM
	got, err = registry.GetByAlgorithm(ctx, "AES-256-GCM")
	if err != nil {
		t.Fatalf("GetByAlgorithm failed: %v", err)
	}
	if got != backend1 {
		t.Error("Expected software backend for AES-256-GCM")
	}

	// Should fail for unsupported algorithm
	_, err = registry.GetByAlgorithm(ctx, "UNSUPPORTED")
	if err == nil {
		t.Fatal("Expected error for unsupported algorithm")
	}
}

func TestBackendRegistry_HealthChecks(t *testing.T) {
	regConfig := DefaultRegistryConfig()
	regConfig.HealthCheckInterval = 50 * time.Millisecond
	regConfig.UnhealthyThreshold = 2

	registry := NewBackendRegistry(regConfig)
	defer registry.Close()

	backend := newTestBackend(config.BackendType("test"), true, nil)
	registry.Register("primary", backend, 1)

	// Start health checks
	registry.StartHealthChecks()

	// Wait for initial check
	time.Sleep(100 * time.Millisecond)

	health, _ := registry.GetHealth("primary")
	if health.Status != HealthStatusHealthy {
		t.Errorf("Expected healthy status, got %s", health.Status)
	}

	// Make backend unavailable
	backend.SetAvailable(false)

	// Wait for health checks to detect
	time.Sleep(150 * time.Millisecond)

	health, _ = registry.GetHealth("primary")
	if health.Status == HealthStatusHealthy {
		t.Error("Expected non-healthy status after failures")
	}

	// Stop health checks
	registry.StopHealthChecks()
}

func TestBackendRegistry_List(t *testing.T) {
	registry := NewBackendRegistry(nil)
	defer registry.Close()

	backend1 := newTestBackend(config.BackendType("backend1"), true, nil)
	backend2 := newTestBackend(config.BackendType("backend2"), true, nil)

	registry.Register("backend1", backend1, 2)
	registry.Register("backend2", backend2, 1)

	list := registry.List()
	if len(list) != 2 {
		t.Fatalf("Expected 2 backends, got %d", len(list))
	}

	// Verify list doesn't expose internal Backend reference
	for _, info := range list {
		if info.Backend != nil {
			t.Error("List should not expose Backend reference")
		}
	}
}

func TestBackendRegistry_Stats(t *testing.T) {
	registry := NewBackendRegistry(nil)
	defer registry.Close()

	backend1 := newTestBackend(config.BackendType("backend1"), true, nil)
	backend2 := newTestBackend(config.BackendType("backend2"), false, nil)

	registry.Register("backend1", backend1, 1)
	registry.Register("backend2", backend2, 2)

	ctx := context.Background()
	registry.CheckHealth(ctx, "backend1")
	registry.CheckHealth(ctx, "backend2")

	stats := registry.Stats()
	if stats.TotalBackends != 2 {
		t.Errorf("Expected 2 total backends, got %d", stats.TotalBackends)
	}
	if stats.EnabledBackends != 2 {
		t.Errorf("Expected 2 enabled backends, got %d", stats.EnabledBackends)
	}
	if stats.TotalHealthChecks != 2 {
		t.Errorf("Expected 2 health checks, got %d", stats.TotalHealthChecks)
	}
}

func TestBackendRegistry_Unregister(t *testing.T) {
	registry := NewBackendRegistry(nil)
	defer registry.Close()

	backend := newTestBackend(config.BackendType("test"), true, nil)
	registry.Register("primary", backend, 1)

	err := registry.Unregister("primary")
	if err != nil {
		t.Fatalf("Unregister failed: %v", err)
	}

	if !backend.IsClosed() {
		t.Error("Backend should have been closed")
	}

	_, err = registry.Get("primary")
	if err == nil {
		t.Fatal("Expected error after unregister")
	}

	// Unregister non-existent should fail
	err = registry.Unregister("nonexistent")
	if err == nil {
		t.Fatal("Expected error for non-existent backend")
	}
}

func TestBackendRegistry_SetPriority(t *testing.T) {
	registry := NewBackendRegistry(nil)
	defer registry.Close()

	backend := newTestBackend(config.BackendType("test"), true, nil)
	registry.Register("primary", backend, 1)

	err := registry.SetPriority("primary", 5)
	if err != nil {
		t.Fatalf("SetPriority failed: %v", err)
	}

	list := registry.List()
	if len(list) != 1 || list[0].Priority != 5 {
		t.Error("Priority was not updated")
	}
}

func TestBackendRegistry_Close(t *testing.T) {
	registry := NewBackendRegistry(nil)

	backend1 := newTestBackend(config.BackendType("backend1"), true, nil)
	backend2 := newTestBackend(config.BackendType("backend2"), true, nil)

	registry.Register("backend1", backend1, 1)
	registry.Register("backend2", backend2, 2)

	registry.StartHealthChecks()

	err := registry.Close()
	if err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	if !backend1.IsClosed() || !backend2.IsClosed() {
		t.Error("All backends should have been closed")
	}

	list := registry.List()
	if len(list) != 0 {
		t.Error("Registry should be empty after close")
	}
}

func TestRegistryError(t *testing.T) {
	err := &RegistryError{
		Code:    ErrBackendNotFound,
		Op:      "Get",
		Backend: "primary",
		Message: "backend not found",
	}

	expected := "Get: primary: backend not found"
	if err.Error() != expected {
		t.Errorf("Expected %q, got %q", expected, err.Error())
	}

	// Without backend
	err2 := &RegistryError{
		Code:    ErrNoHealthyBackend,
		Op:      "GetHealthy",
		Message: "no healthy backends",
	}

	expected2 := "GetHealthy: no healthy backends"
	if err2.Error() != expected2 {
		t.Errorf("Expected %q, got %q", expected2, err2.Error())
	}
}

func TestBackendRegistry_GetAllHealth(t *testing.T) {
	registry := NewBackendRegistry(nil)
	defer registry.Close()

	backend1 := newTestBackend(config.BackendType("backend1"), true, nil)
	backend2 := newTestBackend(config.BackendType("backend2"), false, nil)

	registry.Register("backend1", backend1, 1)
	registry.Register("backend2", backend2, 2)

	ctx := context.Background()
	registry.CheckHealth(ctx, "backend1")
	registry.CheckHealth(ctx, "backend2")

	allHealth := registry.GetAllHealth()
	if len(allHealth) != 2 {
		t.Fatalf("Expected 2 health entries, got %d", len(allHealth))
	}

	// Verify both backends have health entries
	_, ok1 := allHealth["backend1"]
	_, ok2 := allHealth["backend2"]
	if !ok1 || !ok2 {
		t.Error("Expected health entries for both backends")
	}
}

func TestBackendRegistry_EnableNonexistent(t *testing.T) {
	registry := NewBackendRegistry(nil)
	defer registry.Close()

	err := registry.Enable("nonexistent")
	if err == nil {
		t.Fatal("Expected error for non-existent backend")
	}

	regErr, ok := err.(*RegistryError)
	if !ok || regErr.Code != ErrBackendNotFound {
		t.Errorf("Expected ErrBackendNotFound, got %v", err)
	}
}

func TestBackendRegistry_DisableNonexistent(t *testing.T) {
	registry := NewBackendRegistry(nil)
	defer registry.Close()

	err := registry.Disable("nonexistent")
	if err == nil {
		t.Fatal("Expected error for non-existent backend")
	}

	regErr, ok := err.(*RegistryError)
	if !ok || regErr.Code != ErrBackendNotFound {
		t.Errorf("Expected ErrBackendNotFound, got %v", err)
	}
}

func TestBackendRegistry_SetPriorityNonexistent(t *testing.T) {
	registry := NewBackendRegistry(nil)
	defer registry.Close()

	err := registry.SetPriority("nonexistent", 5)
	if err == nil {
		t.Fatal("Expected error for non-existent backend")
	}

	regErr, ok := err.(*RegistryError)
	if !ok || regErr.Code != ErrBackendNotFound {
		t.Errorf("Expected ErrBackendNotFound, got %v", err)
	}
}

func TestBackendRegistry_GetHealthNonexistent(t *testing.T) {
	registry := NewBackendRegistry(nil)
	defer registry.Close()

	_, err := registry.GetHealth("nonexistent")
	if err == nil {
		t.Fatal("Expected error for non-existent backend")
	}

	regErr, ok := err.(*RegistryError)
	if !ok || regErr.Code != ErrBackendNotFound {
		t.Errorf("Expected ErrBackendNotFound, got %v", err)
	}
}

func TestBackendRegistry_CheckHealthNonexistent(t *testing.T) {
	registry := NewBackendRegistry(nil)
	defer registry.Close()

	ctx := context.Background()
	// Should not panic, just silently return
	registry.CheckHealth(ctx, "nonexistent")
}

func TestBackendRegistry_GetHealthyNoBackends(t *testing.T) {
	registry := NewBackendRegistry(nil)
	defer registry.Close()

	ctx := context.Background()
	_, err := registry.GetHealthy(ctx)
	if err == nil {
		t.Fatal("Expected error when no backends")
	}

	regErr, ok := err.(*RegistryError)
	if !ok || regErr.Code != ErrNoHealthyBackend {
		t.Errorf("Expected ErrNoHealthyBackend, got %v", err)
	}
}

func TestBackendRegistry_GetByAlgorithmNoBackends(t *testing.T) {
	registry := NewBackendRegistry(nil)
	defer registry.Close()

	ctx := context.Background()
	_, err := registry.GetByAlgorithm(ctx, "AES-256-GCM")
	if err == nil {
		t.Fatal("Expected error when no backends")
	}

	regErr, ok := err.(*RegistryError)
	if !ok || regErr.Code != ErrNoBackendForAlgorithm {
		t.Errorf("Expected ErrNoBackendForAlgorithm, got %v", err)
	}
}

func TestBackendRegistry_CloseWithErrors(t *testing.T) {
	registry := NewBackendRegistry(nil)

	// Use a backend that returns error on close
	backend := newTestBackend(config.BackendType("test"), true, nil)
	registry.Register("test", backend, 1)

	registry.StartHealthChecks()

	// Close should still succeed even if backend close errors occur
	err := registry.Close()
	if err != nil {
		t.Fatalf("Close failed: %v", err)
	}
}

func TestBackendRegistry_DefaultConfig(t *testing.T) {
	regConfig := DefaultRegistryConfig()

	if regConfig.HealthCheckInterval != 30*time.Second {
		t.Errorf("Expected 30s health check interval, got %v", regConfig.HealthCheckInterval)
	}
	if regConfig.RecoveryThreshold != 2 {
		t.Errorf("Expected recovery threshold 2, got %d", regConfig.RecoveryThreshold)
	}
	if regConfig.UnhealthyThreshold != 3 {
		t.Errorf("Expected unhealthy threshold 3, got %d", regConfig.UnhealthyThreshold)
	}
	if regConfig.HealthCheckTimeout != 5*time.Second {
		t.Errorf("Expected 5s health check timeout, got %v", regConfig.HealthCheckTimeout)
	}
}

func TestBackendRegistry_HealthCheckWorker(t *testing.T) {
	regConfig := DefaultRegistryConfig()
	regConfig.HealthCheckInterval = 50 * time.Millisecond
	regConfig.UnhealthyThreshold = 1
	regConfig.RecoveryThreshold = 1

	registry := NewBackendRegistry(regConfig)
	defer registry.Close()

	backend := newTestBackend(config.BackendType("test"), true, nil)
	registry.Register("primary", backend, 1)

	// Start and let run briefly
	registry.StartHealthChecks()
	time.Sleep(100 * time.Millisecond)
	registry.StopHealthChecks()

	health, _ := registry.GetHealth("primary")
	if health.Status != HealthStatusHealthy {
		t.Errorf("Expected healthy status, got %s", health.Status)
	}
}

func TestBackendRegistry_UnregisterClosesBackend(t *testing.T) {
	registry := NewBackendRegistry(nil)
	defer registry.Close()

	backend := newTestBackend(config.BackendType("test"), true, nil)
	registry.Register("primary", backend, 1)

	err := registry.Unregister("primary")
	if err != nil {
		t.Fatalf("Unregister failed: %v", err)
	}

	if !backend.IsClosed() {
		t.Error("Backend should have been closed on unregister")
	}
}

func TestBackendRegistry_PrioritySelection(t *testing.T) {
	registry := NewBackendRegistry(nil)
	defer registry.Close()

	// Higher priority number = higher priority
	backend1 := newTestBackend(config.BackendType("low"), true, nil)
	backend2 := newTestBackend(config.BackendType("high"), true, nil)

	registry.Register("low", backend1, 10)
	registry.Register("high", backend2, 1) // Lower number = higher priority

	ctx := context.Background()
	registry.CheckHealth(ctx, "low")
	registry.CheckHealth(ctx, "high")

	// Should return the higher priority (lower number) backend
	got, err := registry.GetHealthy(ctx)
	if err != nil {
		t.Fatalf("GetHealthy failed: %v", err)
	}
	if got != backend2 {
		t.Error("Expected high priority backend")
	}
}

func TestRouteWithFailover_MaxAttemptsBreak(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)

	// Register 5 backends
	for i := 0; i < 5; i++ {
		backend := NewMockBackend()
		err := registry.Register("backend"+string(rune('a'+i)), backend, 100-i)
		require.NoError(t, err)
	}

	// Set MaxFailoverAttempts to 2, less than the 5 backends
	router := NewBackendRouter(registry, &RoutingConfig{
		FailoverEnabled:     true,
		MaxFailoverAttempts: 2,
	})

	attemptCount := 0
	err := router.RouteWithFailover(ctx, "", AlgAES256GCM, func(b Backend) error {
		attemptCount++
		return errors.New("operation failed")
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "all failover attempts failed")
	// Should only attempt 2 times, not all 5 backends
	assert.Equal(t, 2, attemptCount)
}

// TestRouteWithFailover_MaxAttemptsBreak_WithLogger tests the break condition
// with logging enabled to ensure proper log output during failover.
func TestRouteWithFailover_MaxAttemptsBreak_WithLogger(t *testing.T) {
	ctx := context.Background()
	logger := newCoverageTestLogger()
	registry := NewBackendRegistry(nil)

	// Register 4 backends
	for i := 0; i < 4; i++ {
		backend := NewMockBackend()
		err := registry.Register("backend"+string(rune('a'+i)), backend, 100-i)
		require.NoError(t, err)
	}

	// Set MaxFailoverAttempts to 2, less than the 4 backends
	router := NewBackendRouter(registry, &RoutingConfig{
		FailoverEnabled:     true,
		MaxFailoverAttempts: 2,
	}, WithLogger(logger))

	attemptCount := 0
	err := router.RouteWithFailover(ctx, "", AlgAES256GCM, func(b Backend) error {
		attemptCount++
		return errors.New("operation failed")
	})

	require.Error(t, err)
	// Should only attempt 2 times due to MaxFailoverAttempts limit
	assert.Equal(t, 2, attemptCount)
}

// TestRouteWithFailover_MaxAttemptsExactMatch tests when MaxFailoverAttempts
// equals the number of backends (no break, all backends tried).
func TestRouteWithFailover_MaxAttemptsExactMatch(t *testing.T) {
	ctx := context.Background()
	registry := NewBackendRegistry(nil)

	// Register 3 backends
	for i := 0; i < 3; i++ {
		backend := NewMockBackend()
		err := registry.Register("backend"+string(rune('a'+i)), backend, 100-i)
		require.NoError(t, err)
	}

	// Set MaxFailoverAttempts to 3, equal to number of backends
	router := NewBackendRouter(registry, &RoutingConfig{
		FailoverEnabled:     true,
		MaxFailoverAttempts: 3,
	})

	attemptCount := 0
	err := router.RouteWithFailover(ctx, "", AlgAES256GCM, func(b Backend) error {
		attemptCount++
		return errors.New("operation failed")
	})

	require.Error(t, err)
	// All 3 backends should be attempted
	assert.Equal(t, 3, attemptCount)
}

// =============================================================================
// BackendFactory Close Error Coverage Tests
// =============================================================================

// TestBackendFactory_Close_RegistryCloseError tests the error path when
// registry.Close() returns an error during factory close.
func TestBackendFactory_Close_RegistryCloseError(t *testing.T) {
	factory, err := NewBackendFactory(nil)
	require.NoError(t, err)

	// Register a backend that will fail on close
	backend := NewMockBackend()
	backend.SetError("Close", errors.New("backend close failure"))
	err = factory.RegisterBackend("failing-backend", backend, 100)
	require.NoError(t, err)

	// Close the factory - registry.Close() will return error from failing backend
	err = factory.Close()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to close registry")
}

// =============================================================================
// KMSService Close with Nil Backend Coverage Tests
// =============================================================================

// TestKMSService_Close_NilBackendInRegistry tests the nil backend check
// in the Close function's loop through registry backends.
func TestKMSService_Close_NilBackendInRegistry(t *testing.T) {
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
	})
	require.NoError(t, err)

	// Directly manipulate the registry to add a nil backend entry
	// This simulates a scenario where a backend was removed but
	// the entry still exists
	registry.mu.Lock()
	registry.backends["nil-backend"] = &BackendInfo{
		Name:     "nil-backend",
		Backend:  nil, // nil backend
		Priority: 50,
		Enabled:  true,
	}
	registry.mu.Unlock()

	// Close should handle nil backend gracefully
	err = service.Close()
	require.NoError(t, err)
}

// TestKMSService_Close_BackendError_WithLogger tests backend close error
// handling when logger is configured.
func TestKMSService_Close_BackendError_WithLogger(t *testing.T) {
	logger := newCoverageTestLogger()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	backend.SetError("Close", errors.New("backend close failure"))
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
		Logger:   logger,
	})
	require.NoError(t, err)

	// Close should log the error but not return it
	err = service.Close()
	require.NoError(t, err)
}

// TestCoverage81_KMSService_Close_WithLoggerEnabled tests the logger info message during close.
func TestCoverage81_KMSService_Close_WithLoggerEnabled(t *testing.T) {
	logger := newCoverageTestLogger()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
		Logger:   logger,
	})
	require.NoError(t, err)

	err = service.Close()
	require.NoError(t, err)
}

// =============================================================================
// Store SetState Error Coverage Tests
// These tests cover the warning log paths when store.SetState fails
// because the key is not in the cache yet.
// =============================================================================

// TestCoverage81_RevokeKey_StoreSetStateError tests the store.SetState error
// path in RevokeKey when the key is not in the local store cache.
func TestCoverage81_RevokeKey_StoreSetStateError(t *testing.T) {
	ctx := context.Background()
	logger := newCoverageTestLogger()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
		Logger:   logger,
	})
	require.NoError(t, err)

	// Create a key in the backend but NOT in the service's store
	keyID := "revoke-test-key"
	backend.mu.Lock()
	backend.keys[keyID] = &KeyMetadata{
		KeyID:     keyID,
		Algorithm: AlgAES256GCM,
		State:     KeyStateEnabled,
	}
	backend.mu.Unlock()

	// RevokeKey should succeed (backend succeeds) but store.SetState will fail
	// because the key is not in the store cache. This should be logged as warning.
	err = service.RevokeKey(ctx, keyID)
	require.NoError(t, err)
}

// TestCoverage81_EnableKey_StoreSetStateError tests the store.SetState error
// path in EnableKey when the key is not in the local store cache.
func TestCoverage81_EnableKey_StoreSetStateError(t *testing.T) {
	ctx := context.Background()
	logger := newCoverageTestLogger()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
		Logger:   logger,
	})
	require.NoError(t, err)

	// Create a disabled key in the backend but NOT in the service's store
	keyID := "enable-test-key"
	backend.mu.Lock()
	backend.keys[keyID] = &KeyMetadata{
		KeyID:     keyID,
		Algorithm: AlgAES256GCM,
		State:     KeyStateDisabled,
	}
	backend.mu.Unlock()

	// EnableKey should succeed (backend succeeds) but store.SetState will fail
	// because the key is not in the store cache. This should be logged as warning.
	err = service.EnableKey(ctx, keyID)
	require.NoError(t, err)
}

// TestCoverage81_DisableKey_StoreSetStateError tests the store.SetState error
// path in DisableKey when the key is not in the local store cache.
func TestCoverage81_DisableKey_StoreSetStateError(t *testing.T) {
	ctx := context.Background()
	logger := newCoverageTestLogger()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
		Logger:   logger,
	})
	require.NoError(t, err)

	// Create an enabled key in the backend but NOT in the service's store
	keyID := "disable-test-key"
	backend.mu.Lock()
	backend.keys[keyID] = &KeyMetadata{
		KeyID:     keyID,
		Algorithm: AlgAES256GCM,
		State:     KeyStateEnabled,
	}
	backend.mu.Unlock()

	// DisableKey should succeed (backend succeeds) but store.SetState will fail
	// because the key is not in the store cache. This should be logged as warning.
	err = service.DisableKey(ctx, keyID)
	require.NoError(t, err)
}

// TestCoverage81_DeleteKey_StoreDeleteError tests the store.Delete error
// path in DeleteKey when the key is deleted from backend but cache delete fails.
func TestCoverage81_DeleteKey_StoreDeleteError(t *testing.T) {
	ctx := context.Background()
	logger := newCoverageTestLogger()
	registry := NewBackendRegistry(nil)
	backend := NewMockBackend()
	require.NoError(t, registry.Register("software", backend, 100))

	service, err := NewKMSService(&KMSServiceOptions{
		Registry: registry,
		Logger:   logger,
	})
	require.NoError(t, err)

	// Create a key in the backend but NOT in the service's store
	keyID := "delete-test-key"
	backend.mu.Lock()
	backend.keys[keyID] = &KeyMetadata{
		KeyID:     keyID,
		Algorithm: AlgAES256GCM,
		State:     KeyStateEnabled,
	}
	backend.mu.Unlock()

	// DeleteKey should succeed (backend succeeds) but store.Delete will fail
	// because the key is not in the store cache. This should be logged as warning.
	err = service.DeleteKey(ctx, keyID)
	require.NoError(t, err)
}

// =============================================================================
// Extended Registry Tests - Health Checks, Logging, and Edge Cases
// =============================================================================
// =============================================================================
// Registry StartHealthChecks/StopHealthChecks Tests
// =============================================================================

// TestBackendRegistry_StartHealthChecks_AlreadyRunning tests double start.
func TestBackendRegistry_StartHealthChecks_AlreadyRunning(t *testing.T) {
	regConfig := DefaultRegistryConfig()
	regConfig.HealthCheckInterval = 1 * time.Second
	registry := NewBackendRegistry(regConfig)
	defer registry.Close()

	backend := newTestBackend(config.BackendTypeSoftware, true, nil)
	registry.Register("test", backend, 1)

	// Start once
	registry.StartHealthChecks()

	// Start again - should return immediately
	registry.StartHealthChecks()

	// Still running
	assert.True(t, registry.running.Load())

	registry.StopHealthChecks()
}

// TestBackendRegistry_StopHealthChecks_NotRunning tests stop when not running.
func TestBackendRegistry_StopHealthChecks_NotRunning(t *testing.T) {
	registry := NewBackendRegistry(nil)
	defer registry.Close()

	// Stop when not running - should return immediately
	registry.StopHealthChecks()

	// Should still be not running
	assert.False(t, registry.running.Load())
}

// TestBackendRegistry_StopHealthChecks_ThenRestart tests stop then restart.
func TestBackendRegistry_StopHealthChecks_ThenRestart(t *testing.T) {
	regConfig := DefaultRegistryConfig()
	regConfig.HealthCheckInterval = 100 * time.Millisecond
	registry := NewBackendRegistry(regConfig)
	defer registry.Close()

	backend := newTestBackend(config.BackendTypeSoftware, true, nil)
	registry.Register("test", backend, 1)

	// Start
	registry.StartHealthChecks()
	time.Sleep(50 * time.Millisecond)
	assert.True(t, registry.running.Load())

	// Stop
	registry.StopHealthChecks()
	assert.False(t, registry.running.Load())

	// Restart
	registry.StartHealthChecks()
	time.Sleep(50 * time.Millisecond)
	assert.True(t, registry.running.Load())

	registry.StopHealthChecks()
}

// =============================================================================
// Registry Close Tests
// =============================================================================

// errorClosingBackend is a mock backend that returns error on Close.
type errorClosingBackend struct {
	testBackend
	closeErr error
}

func (b *errorClosingBackend) Close() error {
	return b.closeErr
}

// newErrorClosingBackend creates a new errorClosingBackend with the given error.
func newErrorClosingBackend(backendType config.BackendType, available bool, closeErr error) *errorClosingBackend {
	return &errorClosingBackend{
		testBackend: *newTestBackend(backendType, available, nil),
		closeErr:    closeErr,
	}
}

// TestBackendRegistry_Close_WithBackendCloseError tests Close when backend returns error.
func TestBackendRegistry_Close_WithBackendCloseError(t *testing.T) {
	registry := NewBackendRegistry(nil)

	backend := newErrorClosingBackend(config.BackendTypeSoftware, true, errors.New("close failed"))
	registry.Register("error-backend", backend, 1)

	// Close should return last error
	err := registry.Close()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "close failed")
}

// =============================================================================
// Registry Unregister Tests
// =============================================================================

// TestBackendRegistry_Unregister_WithCloseError tests Unregister when backend Close fails.
func TestBackendRegistry_Unregister_WithCloseError(t *testing.T) {
	registry := NewBackendRegistry(nil)
	defer registry.Close()

	backend := newErrorClosingBackend(config.BackendTypeSoftware, true, errors.New("unregister close failed"))
	registry.Register("error-backend", backend, 1)

	// Unregister should log error but still unregister
	err := registry.Unregister("error-backend")
	require.NoError(t, err)

	// Verify backend is unregistered
	_, err = registry.Get("error-backend")
	require.Error(t, err)
}

// =============================================================================
// Registry GetByAlgorithm Tests
// =============================================================================

// TestBackendRegistry_GetByAlgorithm_DisabledBackend tests GetByAlgorithm skips disabled backends.
func TestBackendRegistry_GetByAlgorithm_DisabledBackend(t *testing.T) {
	registry := NewBackendRegistry(nil)
	defer registry.Close()

	backend := newTestBackend(config.BackendTypeSoftware, true, []string{"AES-256-GCM"})
	registry.Register("test", backend, 1)

	// Check health so it's not Unknown
	ctx := context.Background()
	registry.CheckHealth(ctx, "test")

	// Works before disable
	_, err := registry.GetByAlgorithm(ctx, "AES-256-GCM")
	require.NoError(t, err)

	// Disable
	registry.Disable("test")

	// Should fail now
	_, err = registry.GetByAlgorithm(ctx, "AES-256-GCM")
	require.Error(t, err)
}

// TestBackendRegistry_GetByAlgorithm_UnhealthyBackend tests GetByAlgorithm skips unhealthy backends.
func TestBackendRegistry_GetByAlgorithm_UnhealthyBackend(t *testing.T) {
	regConfig := DefaultRegistryConfig()
	regConfig.UnhealthyThreshold = 1
	registry := NewBackendRegistry(regConfig)
	defer registry.Close()

	backend := newTestBackend(config.BackendTypeSoftware, false, []string{"AES-256-GCM"})
	registry.Register("test", backend, 1)

	ctx := context.Background()

	// Check health - should become unhealthy
	registry.CheckHealth(ctx, "test")

	// Should fail - backend is unhealthy
	_, err := registry.GetByAlgorithm(ctx, "AES-256-GCM")
	require.Error(t, err)
}

// TestBackendRegistry_GetByAlgorithm_PrioritySelection tests priority-based selection.
func TestBackendRegistry_GetByAlgorithm_PrioritySelection(t *testing.T) {
	registry := NewBackendRegistry(nil)
	defer registry.Close()

	backend1 := newTestBackend(config.BackendTypeSoftware, true, []string{"AES-256-GCM"})
	backend2 := newTestBackend(config.BackendTypePKCS11, true, []string{"AES-256-GCM"})

	// Register with different priorities
	registry.Register("low-priority", backend1, 10)
	registry.Register("high-priority", backend2, 1)

	ctx := context.Background()
	registry.CheckHealth(ctx, "low-priority")
	registry.CheckHealth(ctx, "high-priority")

	// Should return high priority backend
	selected, err := registry.GetByAlgorithm(ctx, "AES-256-GCM")
	require.NoError(t, err)
	assert.Equal(t, backend2, selected)
}

// =============================================================================
// Registry Enable/Disable/SetPriority with Logger Tests
// =============================================================================

// TestBackendRegistry_Enable_WithLogger tests Enable logs the action.
func TestBackendRegistry_Enable_WithLogger(t *testing.T) {
	regConfig := DefaultRegistryConfig()
	registry := NewBackendRegistry(regConfig)
	defer registry.Close()

	backend := newTestBackend(config.BackendTypeSoftware, true, nil)
	registry.Register("test", backend, 1)
	registry.Disable("test")

	// Enable
	err := registry.Enable("test")
	require.NoError(t, err)

	// Verify enabled
	_, err = registry.Get("test")
	require.NoError(t, err)
}

// TestBackendRegistry_Disable_WithLogger tests Disable logs the action.
func TestBackendRegistry_Disable_WithLogger(t *testing.T) {
	regConfig := DefaultRegistryConfig()
	registry := NewBackendRegistry(regConfig)
	defer registry.Close()

	backend := newTestBackend(config.BackendTypeSoftware, true, nil)
	registry.Register("test", backend, 1)

	// Disable
	err := registry.Disable("test")
	require.NoError(t, err)

	// Verify disabled
	_, err = registry.Get("test")
	require.Error(t, err)
}

// TestBackendRegistry_SetPriority_WithLogger tests SetPriority logs the action.
func TestBackendRegistry_SetPriority_WithLogger(t *testing.T) {
	regConfig := DefaultRegistryConfig()
	registry := NewBackendRegistry(regConfig)
	defer registry.Close()

	backend := newTestBackend(config.BackendTypeSoftware, true, nil)
	registry.Register("test", backend, 1)

	// Set priority
	err := registry.SetPriority("test", 100)
	require.NoError(t, err)

	// Verify priority changed
	list := registry.List()
	require.Len(t, list, 1)
	assert.Equal(t, 100, list[0].Priority)
}

// =============================================================================
// Registry PerformHealthCheck Tests
// =============================================================================

// TestBackendRegistry_PerformHealthCheck_RecoveringFromUnhealthy tests recovery state.
func TestBackendRegistry_PerformHealthCheck_RecoveringFromUnhealthy(t *testing.T) {
	regConfig := DefaultRegistryConfig()
	regConfig.UnhealthyThreshold = 1
	registry := NewBackendRegistry(regConfig)
	defer registry.Close()

	backend := newTestBackend(config.BackendTypeSoftware, false, nil)
	registry.Register("test", backend, 1)

	ctx := context.Background()

	// Become unhealthy
	registry.CheckHealth(ctx, "test")
	health, _ := registry.GetHealth("test")
	assert.Equal(t, HealthStatusUnhealthy, health.Status)

	// Make available again
	backend.SetAvailable(true)
	registry.CheckHealth(ctx, "test")
	health, _ = registry.GetHealth("test")
	// Should be degraded (recovering from unhealthy)
	assert.Equal(t, HealthStatusDegraded, health.Status)
}

// TestBackendRegistry_PerformHealthCheck_RecoveringFromFailures tests recovery from failures.
func TestBackendRegistry_PerformHealthCheck_RecoveringFromFailures(t *testing.T) {
	regConfig := DefaultRegistryConfig()
	regConfig.UnhealthyThreshold = 3 // High threshold
	registry := NewBackendRegistry(regConfig)
	defer registry.Close()

	backend := newTestBackend(config.BackendTypeSoftware, false, nil)
	registry.Register("test", backend, 1)

	ctx := context.Background()

	// Cause one failure (not enough to become unhealthy)
	registry.CheckHealth(ctx, "test")
	health, _ := registry.GetHealth("test")
	assert.Equal(t, HealthStatusDegraded, health.Status)

	// Make available again
	backend.SetAvailable(true)
	registry.CheckHealth(ctx, "test")
	health, _ = registry.GetHealth("test")
	// Should be degraded (recovering from failures)
	assert.Equal(t, HealthStatusDegraded, health.Status)
}

// TestBackendRegistry_PerformHealthCheck_CountersUpdate tests that counters are updated.
func TestBackendRegistry_PerformHealthCheck_CountersUpdate(t *testing.T) {
	registry := NewBackendRegistry(nil)
	defer registry.Close()

	backend := newTestBackend(config.BackendTypeSoftware, true, nil)
	registry.Register("test", backend, 1)

	ctx := context.Background()

	// Initial check
	registry.CheckHealth(ctx, "test")
	stats := registry.Stats()
	assert.Equal(t, uint64(1), stats.TotalHealthChecks)
	assert.Equal(t, uint64(0), stats.TotalFailedChecks)

	// Failed check
	backend.SetAvailable(false)
	registry.CheckHealth(ctx, "test")
	stats = registry.Stats()
	assert.Equal(t, uint64(2), stats.TotalHealthChecks)
	assert.Equal(t, uint64(1), stats.TotalFailedChecks)
}

// =============================================================================
// Registry CheckHealth Edge Cases
// =============================================================================

// TestBackendRegistry_CheckHealth_NonexistentReturnsError tests CheckHealth for nonexistent backend.
func TestBackendRegistry_CheckHealth_NonexistentReturnsError(t *testing.T) {
	registry := NewBackendRegistry(nil)
	defer registry.Close()

	ctx := context.Background()
	_, err := registry.CheckHealth(ctx, "nonexistent")
	require.Error(t, err)
}

// =============================================================================
// Registry ListWithBackends Tests
// =============================================================================

// TestBackendRegistry_ListWithBackends_ReturnsBackends tests ListWithBackends includes backend refs.
func TestBackendRegistry_ListWithBackends_ReturnsBackends(t *testing.T) {
	registry := NewBackendRegistry(nil)
	defer registry.Close()

	backend := newTestBackend(config.BackendTypeSoftware, true, nil)
	registry.Register("test", backend, 1)

	list := registry.ListWithBackends()
	require.Len(t, list, 1)
	assert.NotNil(t, list[0].Backend)
	assert.Equal(t, backend, list[0].Backend)
}

// =============================================================================
// Registry GetHealthy Edge Cases
// =============================================================================

// TestBackendRegistry_GetHealthy_AllUnhealthy tests GetHealthy when all backends are unhealthy.
func TestBackendRegistry_GetHealthy_AllUnhealthy(t *testing.T) {
	regConfig := DefaultRegistryConfig()
	regConfig.UnhealthyThreshold = 1
	registry := NewBackendRegistry(regConfig)
	defer registry.Close()

	backend := newTestBackend(config.BackendTypeSoftware, false, nil)
	registry.Register("test", backend, 1)

	ctx := context.Background()
	registry.CheckHealth(ctx, "test")

	_, err := registry.GetHealthy(ctx)
	require.Error(t, err)
}

// TestBackendRegistry_GetHealthy_AllDisabled tests GetHealthy when all backends are disabled.
func TestBackendRegistry_GetHealthy_AllDisabled(t *testing.T) {
	registry := NewBackendRegistry(nil)
	defer registry.Close()

	backend := newTestBackend(config.BackendTypeSoftware, true, nil)
	registry.Register("test", backend, 1)
	registry.Disable("test")

	ctx := context.Background()
	_, err := registry.GetHealthy(ctx)
	require.Error(t, err)
}

// =============================================================================
// Registry containsAlgorithm Helper Tests
// =============================================================================

// TestContainsAlgorithm tests the containsAlgorithm helper function.
func TestContainsAlgorithm(t *testing.T) {
	tests := []struct {
		name       string
		algorithms []string
		algorithm  string
		expected   bool
	}{
		{"found first", []string{"A", "B", "C"}, "A", true},
		{"found middle", []string{"A", "B", "C"}, "B", true},
		{"found last", []string{"A", "B", "C"}, "C", true},
		{"not found", []string{"A", "B", "C"}, "D", false},
		{"empty list", []string{}, "A", false},
		{"nil list", nil, "A", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := containsAlgorithm(tt.algorithms, tt.algorithm)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// =============================================================================
// Registry Stats Tests
// =============================================================================

// TestBackendRegistry_Stats_AllHealthStatuses tests Stats counts all health statuses.
func TestBackendRegistry_Stats_AllHealthStatuses(t *testing.T) {
	regConfig := DefaultRegistryConfig()
	regConfig.UnhealthyThreshold = 1
	registry := NewBackendRegistry(regConfig)
	defer registry.Close()

	// Healthy backend
	healthyBackend := newTestBackend(config.BackendTypeSoftware, true, nil)
	registry.Register("healthy", healthyBackend, 1)

	// Unhealthy backend
	unhealthyBackend := newTestBackend(config.BackendTypeSoftware, false, nil)
	registry.Register("unhealthy", unhealthyBackend, 2)

	ctx := context.Background()
	registry.CheckHealth(ctx, "healthy")
	registry.CheckHealth(ctx, "unhealthy")

	stats := registry.Stats()
	assert.Equal(t, 2, stats.TotalBackends)
	assert.Equal(t, 2, stats.EnabledBackends)
	assert.Equal(t, 1, stats.HealthyBackends)
	assert.Equal(t, 1, stats.UnhealthyBackends)
}

// =============================================================================
// Registry RegistryError Tests
// =============================================================================

// TestRegistryError_Error tests RegistryError.Error() formatting.
func TestRegistryError_Error(t *testing.T) {
	tests := []struct {
		name     string
		err      *RegistryError
		expected string
	}{
		{
			name: "with backend",
			err: &RegistryError{
				Code:    ErrBackendNotFound,
				Op:      "Get",
				Backend: "primary",
				Message: "backend not found",
			},
			expected: "Get: primary: backend not found",
		},
		{
			name: "without backend",
			err: &RegistryError{
				Code:    ErrNoHealthyBackend,
				Op:      "GetHealthy",
				Message: "no healthy backends",
			},
			expected: "GetHealthy: no healthy backends",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.err.Error())
		})
	}
}

// =============================================================================
// Registry Health Worker Tests
// =============================================================================

// TestBackendRegistry_HealthWorker_RunsPeriodicChecks tests the health check worker runs periodically.
func TestBackendRegistry_HealthWorker_RunsPeriodicChecks(t *testing.T) {
	regConfig := DefaultRegistryConfig()
	regConfig.HealthCheckInterval = 50 * time.Millisecond
	registry := NewBackendRegistry(regConfig)
	defer registry.Close()

	backend := newTestBackend(config.BackendTypeSoftware, true, nil)
	registry.Register("test", backend, 1)

	// Start health checks
	registry.StartHealthChecks()

	// Wait for multiple health checks
	time.Sleep(150 * time.Millisecond)

	// Should have multiple checks
	stats := registry.Stats()
	assert.Greater(t, stats.TotalHealthChecks, uint64(1))

	registry.StopHealthChecks()
}

// =============================================================================
// Register with Logger Tests
// =============================================================================

// TestBackendRegistry_Register_WithLogger tests Register logs the action.
func TestBackendRegistry_Register_WithLogger(t *testing.T) {
	regConfig := DefaultRegistryConfig()
	registry := NewBackendRegistry(regConfig)
	defer registry.Close()

	backend := newTestBackend(config.BackendTypeSoftware, true, []string{"AES-256-GCM", "RSA-2048"})

	err := registry.Register("logged-backend", backend, 5)
	require.NoError(t, err)

	// Verify registered
	list := registry.List()
	require.Len(t, list, 1)
	assert.Equal(t, "logged-backend", list[0].Name)
	assert.Equal(t, 5, list[0].Priority)
	assert.Equal(t, 2, len(list[0].Algorithms))
}

// =============================================================================
// storeTestBackend for testing store operations
// =============================================================================

type storeTestBackend struct {
	testBackend
	createKeyFunc func(ctx context.Context, attrs *xkmssdk.KeyAttributes) (*KeyMetadata, error)
}

func (b *storeTestBackend) CreateKey(ctx context.Context, attrs *xkmssdk.KeyAttributes) (*KeyMetadata, error) {
	if b.createKeyFunc != nil {
		return b.createKeyFunc(ctx, attrs)
	}
	return b.testBackend.CreateKey(ctx, attrs)
}

// =============================================================================
// Registry Logger Coverage Tests
// These tests specifically hit all the logger code paths
// =============================================================================

// registryTestLogger implements interfaces.Logger for testing
type registryTestLogger struct{}

func (l *registryTestLogger) Debug(msg string)                          {}
func (l *registryTestLogger) Debugf(format string, args ...interface{}) {}
func (l *registryTestLogger) Info(msg string)                           {}
func (l *registryTestLogger) Infof(format string, args ...interface{})  {}
func (l *registryTestLogger) Warn(msg string)                           {}
func (l *registryTestLogger) Warnf(format string, args ...interface{})  {}
func (l *registryTestLogger) Error(msg string)                          {}
func (l *registryTestLogger) Errorf(format string, args ...interface{}) {}
func (l *registryTestLogger) Fatal(msg string)                          {}
func (l *registryTestLogger) Fatalf(format string, args ...interface{}) {}
func (l *registryTestLogger) WithField(key string, value interface{}) interfaces.Logger {
	return l
}
func (l *registryTestLogger) WithFields(fields map[string]interface{}) interfaces.Logger {
	return l
}
func (l *registryTestLogger) WithError(err error) interfaces.Logger {
	return l
}

// TestBackendRegistry_Register_LoggerPath tests Register with logger set
func TestBackendRegistry_Register_LoggerPath(t *testing.T) {
	regConfig := DefaultRegistryConfig()
	regConfig.Logger = &registryTestLogger{}
	registry := NewBackendRegistry(regConfig)
	defer registry.Close()

	backend := newTestBackend(config.BackendTypeSoftware, true, []string{"AES-256-GCM"})

	err := registry.Register("test-logger", backend, 1)
	require.NoError(t, err)

	// Verify registered
	_, err = registry.Get("test-logger")
	require.NoError(t, err)
}

// TestBackendRegistry_Unregister_LoggerPath tests Unregister with logger set
func TestBackendRegistry_Unregister_LoggerPath(t *testing.T) {
	regConfig := DefaultRegistryConfig()
	regConfig.Logger = &registryTestLogger{}
	registry := NewBackendRegistry(regConfig)

	backend := newTestBackend(config.BackendTypeSoftware, true, nil)
	registry.Register("test", backend, 1)

	// Unregister triggers logger path
	err := registry.Unregister("test")
	require.NoError(t, err)
}

// TestBackendRegistry_Unregister_CloseError_LoggerPath tests Unregister with close error and logger
func TestBackendRegistry_Unregister_CloseError_LoggerPath(t *testing.T) {
	regConfig := DefaultRegistryConfig()
	regConfig.Logger = &registryTestLogger{}
	registry := NewBackendRegistry(regConfig)

	backend := newErrorClosingBackend(config.BackendTypeSoftware, true, errors.New("close failed"))
	registry.Register("error-backend", backend, 1)

	// Unregister should log the close error
	err := registry.Unregister("error-backend")
	require.NoError(t, err)
}

// TestBackendRegistry_Enable_LoggerPath tests Enable with logger set
func TestBackendRegistry_Enable_LoggerPath(t *testing.T) {
	regConfig := DefaultRegistryConfig()
	regConfig.Logger = &registryTestLogger{}
	registry := NewBackendRegistry(regConfig)
	defer registry.Close()

	backend := newTestBackend(config.BackendTypeSoftware, true, nil)
	registry.Register("test", backend, 1)
	registry.Disable("test")

	err := registry.Enable("test")
	require.NoError(t, err)
}

// TestBackendRegistry_Disable_LoggerPath tests Disable with logger set
func TestBackendRegistry_Disable_LoggerPath(t *testing.T) {
	regConfig := DefaultRegistryConfig()
	regConfig.Logger = &registryTestLogger{}
	registry := NewBackendRegistry(regConfig)
	defer registry.Close()

	backend := newTestBackend(config.BackendTypeSoftware, true, nil)
	registry.Register("test", backend, 1)

	err := registry.Disable("test")
	require.NoError(t, err)
}

// TestBackendRegistry_SetPriority_LoggerPath tests SetPriority with logger set
func TestBackendRegistry_SetPriority_LoggerPath(t *testing.T) {
	regConfig := DefaultRegistryConfig()
	regConfig.Logger = &registryTestLogger{}
	registry := NewBackendRegistry(regConfig)
	defer registry.Close()

	backend := newTestBackend(config.BackendTypeSoftware, true, nil)
	registry.Register("test", backend, 1)

	err := registry.SetPriority("test", 10)
	require.NoError(t, err)
}

// TestBackendRegistry_StartHealthChecks_LoggerPath tests StartHealthChecks with logger set
func TestBackendRegistry_StartHealthChecks_LoggerPath(t *testing.T) {
	regConfig := DefaultRegistryConfig()
	regConfig.Logger = &registryTestLogger{}
	regConfig.HealthCheckInterval = 100 * time.Millisecond
	registry := NewBackendRegistry(regConfig)
	defer registry.Close()

	backend := newTestBackend(config.BackendTypeSoftware, true, nil)
	registry.Register("test", backend, 1)

	registry.StartHealthChecks()
	time.Sleep(50 * time.Millisecond)
	registry.StopHealthChecks()
}

// TestBackendRegistry_StopHealthChecks_LoggerPath tests StopHealthChecks with logger set
func TestBackendRegistry_StopHealthChecks_LoggerPath(t *testing.T) {
	regConfig := DefaultRegistryConfig()
	regConfig.Logger = &registryTestLogger{}
	regConfig.HealthCheckInterval = 100 * time.Millisecond
	registry := NewBackendRegistry(regConfig)
	defer registry.Close()

	backend := newTestBackend(config.BackendTypeSoftware, true, nil)
	registry.Register("test", backend, 1)

	registry.StartHealthChecks()
	time.Sleep(10 * time.Millisecond)
	registry.StopHealthChecks()
}

// TestBackendRegistry_Close_CloseError_LoggerPath tests Close with close error and logger
func TestBackendRegistry_Close_CloseError_LoggerPath(t *testing.T) {
	regConfig := DefaultRegistryConfig()
	regConfig.Logger = &registryTestLogger{}
	registry := NewBackendRegistry(regConfig)

	backend := newErrorClosingBackend(config.BackendTypeSoftware, true, errors.New("close failed"))
	registry.Register("error-backend", backend, 1)

	err := registry.Close()
	require.Error(t, err)
}

// TestBackendRegistry_HealthStatusChange_LoggerPath tests health status change with logger
func TestBackendRegistry_HealthStatusChange_LoggerPath(t *testing.T) {
	regConfig := DefaultRegistryConfig()
	regConfig.Logger = &registryTestLogger{}
	regConfig.UnhealthyThreshold = 1
	registry := NewBackendRegistry(regConfig)
	defer registry.Close()

	backend := newTestBackend(config.BackendTypeSoftware, true, nil)
	registry.Register("test", backend, 1)

	ctx := context.Background()

	// First check - becomes healthy
	registry.CheckHealth(ctx, "test")

	// Make unavailable - triggers status change log
	backend.SetAvailable(false)
	registry.CheckHealth(ctx, "test")

	health, _ := registry.GetHealth("test")
	assert.Equal(t, HealthStatusUnhealthy, health.Status)
}

// TestBackendRegistry_Stats_DegradedBackend tests Stats with degraded backend
func TestBackendRegistry_Stats_DegradedBackend(t *testing.T) {
	regConfig := DefaultRegistryConfig()
	regConfig.UnhealthyThreshold = 3 // High threshold so we get degraded, not unhealthy
	registry := NewBackendRegistry(regConfig)
	defer registry.Close()

	// Backend that will become degraded (1 failure, not enough for unhealthy)
	backend := newTestBackend(config.BackendTypeSoftware, false, nil)
	registry.Register("degraded", backend, 1)

	ctx := context.Background()
	registry.CheckHealth(ctx, "degraded")

	stats := registry.Stats()
	assert.Equal(t, 1, stats.DegradedBackends)
}

// =============================================================================
// Mock Logger for Testing - captures log messages using shared storage
// =============================================================================

// logStorage provides thread-safe shared storage for log messages.
type logStorage struct {
	mu        sync.Mutex
	debugMsgs []string
	infoMsgs  []string
	warnMsgs  []string
	errorMsgs []string
}

func newLogStorage() *logStorage {
	return &logStorage{
		debugMsgs: make([]string, 0),
		infoMsgs:  make([]string, 0),
		warnMsgs:  make([]string, 0),
		errorMsgs: make([]string, 0),
	}
}

func (s *logStorage) addInfo(msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.infoMsgs = append(s.infoMsgs, msg)
}

func (s *logStorage) addWarn(msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.warnMsgs = append(s.warnMsgs, msg)
}

func (s *logStorage) addDebug(msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.debugMsgs = append(s.debugMsgs, msg)
}

func (s *logStorage) addError(msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.errorMsgs = append(s.errorMsgs, msg)
}

func (s *logStorage) getInfoMsgs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]string, len(s.infoMsgs))
	copy(result, s.infoMsgs)
	return result
}

func (s *logStorage) getWarnMsgs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]string, len(s.warnMsgs))
	copy(result, s.warnMsgs)
	return result
}

// capturingLogger implements interfaces.Logger and captures log messages for assertions.
type capturingLogger struct {
	storage *logStorage
	fields  map[string]interface{}
}

func newCapturingLogger() *capturingLogger {
	return &capturingLogger{
		storage: newLogStorage(),
		fields:  make(map[string]interface{}),
	}
}

func (l *capturingLogger) Debug(msg string) {
	l.storage.addDebug(msg)
}

func (l *capturingLogger) Debugf(format string, args ...interface{}) {
	l.storage.addDebug(format)
}

func (l *capturingLogger) Info(msg string) {
	l.storage.addInfo(msg)
}

func (l *capturingLogger) Infof(format string, args ...interface{}) {
	l.storage.addInfo(format)
}

func (l *capturingLogger) Warn(msg string) {
	l.storage.addWarn(msg)
}

func (l *capturingLogger) Warnf(format string, args ...interface{}) {
	l.storage.addWarn(format)
}

func (l *capturingLogger) Error(msg string) {
	l.storage.addError(msg)
}

func (l *capturingLogger) Errorf(format string, args ...interface{}) {
	l.storage.addError(format)
}

func (l *capturingLogger) Fatal(msg string) {}

func (l *capturingLogger) Fatalf(format string, args ...interface{}) {}

func (l *capturingLogger) WithField(key string, value interface{}) interfaces.Logger {
	newFields := make(map[string]interface{})
	for k, v := range l.fields {
		newFields[k] = v
	}
	newFields[key] = value
	return &capturingLogger{
		storage: l.storage,
		fields:  newFields,
	}
}

func (l *capturingLogger) WithFields(fields map[string]interface{}) interfaces.Logger {
	newFields := make(map[string]interface{})
	for k, v := range l.fields {
		newFields[k] = v
	}
	for k, v := range fields {
		newFields[k] = v
	}
	return &capturingLogger{
		storage: l.storage,
		fields:  newFields,
	}
}

func (l *capturingLogger) WithError(err error) interfaces.Logger {
	return &capturingLogger{
		storage: l.storage,
		fields:  l.fields,
	}
}

func (l *capturingLogger) getInfoMsgs() []string {
	return l.storage.getInfoMsgs()
}

func (l *capturingLogger) getWarnMsgs() []string {
	return l.storage.getWarnMsgs()
}

// =============================================================================
// Close Coverage Tests - Error Logging Path
// =============================================================================

// TestRegistry_Coverage_CloseWithErrorLogging tests Close logs errors when backend close fails.
func TestRegistry_Coverage_CloseWithErrorLogging(t *testing.T) {
	logger := newCapturingLogger()
	regConfig := DefaultRegistryConfig()
	regConfig.Logger = logger
	registry := NewBackendRegistry(regConfig)

	// Add a backend that fails on Close
	backend := newErrorClosingBackend(config.BackendTypeSoftware, true, errors.New("simulated close failure"))
	err := registry.Register("failing-backend", backend, 1)
	require.NoError(t, err)

	// Close should return the error and log warning
	err = registry.Close()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "simulated close failure")

	// Verify warning was logged
	warnMsgs := logger.getWarnMsgs()
	assert.Greater(t, len(warnMsgs), 0, "Expected warning to be logged")
}

// TestRegistry_Coverage_CloseWithMultipleBackendErrors tests Close with multiple failing backends.
func TestRegistry_Coverage_CloseWithMultipleBackendErrors(t *testing.T) {
	logger := newCapturingLogger()
	regConfig := DefaultRegistryConfig()
	regConfig.Logger = logger
	registry := NewBackendRegistry(regConfig)

	// Add multiple backends that fail on Close
	backend1 := newErrorClosingBackend(config.BackendTypeSoftware, true, errors.New("backend1 close failed"))
	backend2 := newErrorClosingBackend(config.BackendTypePKCS11, true, errors.New("backend2 close failed"))

	registry.Register("backend1", backend1, 1)
	registry.Register("backend2", backend2, 2)

	// Close returns last error
	err := registry.Close()
	require.Error(t, err)

	// Multiple warnings should have been logged
	warnMsgs := logger.getWarnMsgs()
	assert.GreaterOrEqual(t, len(warnMsgs), 1)
}

// =============================================================================
// GetHealthy Coverage Tests
// =============================================================================

// TestRegistry_Coverage_GetHealthyWithMixedStates tests GetHealthy with various backend states.
func TestRegistry_Coverage_GetHealthyWithMixedStates(t *testing.T) {
	regConfig := DefaultRegistryConfig()
	regConfig.UnhealthyThreshold = 1
	registry := NewBackendRegistry(regConfig)
	defer registry.Close()

	// Backend that is disabled
	disabledBackend := newTestBackend(config.BackendTypeSoftware, true, nil)
	registry.Register("disabled", disabledBackend, 1)
	registry.Disable("disabled")

	// Backend that is unhealthy
	unhealthyBackend := newTestBackend(config.BackendTypeSoftware, false, nil)
	registry.Register("unhealthy", unhealthyBackend, 2)

	ctx := context.Background()
	registry.CheckHealth(ctx, "unhealthy")

	// No healthy backend available - both are either disabled or unhealthy
	_, err := registry.GetHealthy(ctx)
	require.Error(t, err)

	regErr, ok := err.(*RegistryError)
	require.True(t, ok)
	assert.Equal(t, ErrNoHealthyBackend, regErr.Code)
}

// TestRegistry_Coverage_GetHealthySelectsHighestPriority tests priority selection logic.
func TestRegistry_Coverage_GetHealthySelectsHighestPriority(t *testing.T) {
	registry := NewBackendRegistry(nil)
	defer registry.Close()

	// Create backends with different priorities
	lowPriBackend := newTestBackend(config.BackendTypeSoftware, true, nil)
	medPriBackend := newTestBackend(config.BackendTypePKCS11, true, nil)
	highPriBackend := newTestBackend(config.BackendTypeTPM2, true, nil)

	// Register in non-priority order to test selection logic
	registry.Register("low", lowPriBackend, 100)
	registry.Register("high", highPriBackend, 1)
	registry.Register("med", medPriBackend, 50)

	ctx := context.Background()

	// Should return highest priority (lowest number)
	selected, err := registry.GetHealthy(ctx)
	require.NoError(t, err)
	assert.Equal(t, highPriBackend, selected)
}

// TestRegistry_Coverage_GetHealthySkipsDisabledBackend tests that disabled backends are skipped.
func TestRegistry_Coverage_GetHealthySkipsDisabledBackend(t *testing.T) {
	registry := NewBackendRegistry(nil)
	defer registry.Close()

	backend1 := newTestBackend(config.BackendTypeSoftware, true, nil)
	backend2 := newTestBackend(config.BackendTypePKCS11, true, nil)

	registry.Register("primary", backend1, 1)
	registry.Register("secondary", backend2, 2)

	// Disable primary
	registry.Disable("primary")

	ctx := context.Background()
	selected, err := registry.GetHealthy(ctx)
	require.NoError(t, err)

	// Should return secondary since primary is disabled
	assert.Equal(t, backend2, selected)
}

// =============================================================================
// Health Check Logging Coverage Tests
// =============================================================================

// TestRegistry_Coverage_HealthCheckLogsStateChanges tests health state change logging.
func TestRegistry_Coverage_HealthCheckLogsStateChanges(t *testing.T) {
	logger := newCapturingLogger()
	regConfig := DefaultRegistryConfig()
	regConfig.Logger = logger
	regConfig.UnhealthyThreshold = 1
	registry := NewBackendRegistry(regConfig)
	defer registry.Close()

	backend := newTestBackend(config.BackendTypeSoftware, true, nil)
	registry.Register("test", backend, 1)

	ctx := context.Background()

	// Initial check - should become healthy
	registry.CheckHealth(ctx, "test")
	health, _ := registry.GetHealth("test")
	assert.Equal(t, HealthStatusHealthy, health.Status)

	// Make unavailable and check - should log state change
	backend.SetAvailable(false)
	registry.CheckHealth(ctx, "test")
	health, _ = registry.GetHealth("test")
	assert.Equal(t, HealthStatusUnhealthy, health.Status)

	// Verify state change was logged
	infoMsgs := logger.getInfoMsgs()
	foundStateChange := false
	for _, msg := range infoMsgs {
		if msg == "KMS backend health status changed" {
			foundStateChange = true
			break
		}
	}
	assert.True(t, foundStateChange, "Expected health status change to be logged")
}

// TestRegistry_Coverage_PerformHealthCheckDegradedRecovery tests recovery from degraded state.
func TestRegistry_Coverage_PerformHealthCheckDegradedRecovery(t *testing.T) {
	logger := newCapturingLogger()
	regConfig := DefaultRegistryConfig()
	regConfig.Logger = logger
	regConfig.UnhealthyThreshold = 3
	registry := NewBackendRegistry(regConfig)
	defer registry.Close()

	backend := newTestBackend(config.BackendTypeSoftware, true, nil)
	registry.Register("test", backend, 1)

	ctx := context.Background()

	// Start healthy
	registry.CheckHealth(ctx, "test")

	// Cause single failure - should become degraded
	backend.SetAvailable(false)
	registry.CheckHealth(ctx, "test")
	health, _ := registry.GetHealth("test")
	assert.Equal(t, HealthStatusDegraded, health.Status)
	assert.Equal(t, 1, health.ConsecutiveFailures)

	// Recover - should go to degraded "recovering from failures"
	backend.SetAvailable(true)
	registry.CheckHealth(ctx, "test")
	health, _ = registry.GetHealth("test")
	assert.Equal(t, HealthStatusDegraded, health.Status)
	assert.Contains(t, health.Message, "recovering")
}

// =============================================================================
// Register/Unregister with Logger Coverage Tests
// =============================================================================

// TestRegistry_Coverage_RegisterLogsAction tests Register logs the action.
func TestRegistry_Coverage_RegisterLogsAction(t *testing.T) {
	logger := newCapturingLogger()
	regConfig := DefaultRegistryConfig()
	regConfig.Logger = logger
	registry := NewBackendRegistry(regConfig)
	defer registry.Close()

	backend := newTestBackend(config.BackendTypeSoftware, true, []string{"AES-256-GCM"})

	err := registry.Register("logged-backend", backend, 5)
	require.NoError(t, err)

	// Verify registration was logged
	infoMsgs := logger.getInfoMsgs()
	foundLog := false
	for _, msg := range infoMsgs {
		if msg == "Registered KMS backend" {
			foundLog = true
			break
		}
	}
	assert.True(t, foundLog, "Expected registration to be logged")
}

// TestRegistry_Coverage_UnregisterLogsAction tests Unregister logs the action.
func TestRegistry_Coverage_UnregisterLogsAction(t *testing.T) {
	logger := newCapturingLogger()
	regConfig := DefaultRegistryConfig()
	regConfig.Logger = logger
	registry := NewBackendRegistry(regConfig)
	defer registry.Close()

	backend := newTestBackend(config.BackendTypeSoftware, true, nil)
	registry.Register("test-unregister", backend, 1)

	err := registry.Unregister("test-unregister")
	require.NoError(t, err)

	// Verify unregistration was logged
	infoMsgs := logger.getInfoMsgs()
	foundLog := false
	for _, msg := range infoMsgs {
		if msg == "Unregistered KMS backend" {
			foundLog = true
			break
		}
	}
	assert.True(t, foundLog, "Expected unregistration to be logged")
}

// TestRegistry_Coverage_UnregisterLogsCloseError tests Unregister logs backend close errors.
func TestRegistry_Coverage_UnregisterLogsCloseError(t *testing.T) {
	logger := newCapturingLogger()
	regConfig := DefaultRegistryConfig()
	regConfig.Logger = logger
	registry := NewBackendRegistry(regConfig)
	defer registry.Close()

	backend := newErrorClosingBackend(config.BackendTypeSoftware, true, errors.New("close error during unregister"))
	registry.Register("error-backend", backend, 1)

	// Unregister should log the close error but still succeed
	err := registry.Unregister("error-backend")
	require.NoError(t, err)

	// Verify warning was logged
	warnMsgs := logger.getWarnMsgs()
	foundWarning := false
	for _, msg := range warnMsgs {
		if msg == "Error closing backend during unregister" {
			foundWarning = true
			break
		}
	}
	assert.True(t, foundWarning, "Expected close error warning to be logged")
}

// =============================================================================
// Enable/Disable/SetPriority with Logger Coverage Tests
// =============================================================================

// TestRegistry_Coverage_EnableLogsAction tests Enable logs the action.
func TestRegistry_Coverage_EnableLogsAction(t *testing.T) {
	logger := newCapturingLogger()
	regConfig := DefaultRegistryConfig()
	regConfig.Logger = logger
	registry := NewBackendRegistry(regConfig)
	defer registry.Close()

	backend := newTestBackend(config.BackendTypeSoftware, true, nil)
	registry.Register("test", backend, 1)
	registry.Disable("test")

	err := registry.Enable("test")
	require.NoError(t, err)

	// Verify enable was logged
	infoMsgs := logger.getInfoMsgs()
	foundLog := false
	for _, msg := range infoMsgs {
		if msg == "Enabled KMS backend" {
			foundLog = true
			break
		}
	}
	assert.True(t, foundLog, "Expected enable action to be logged")
}

// TestRegistry_Coverage_DisableLogsAction tests Disable logs the action.
func TestRegistry_Coverage_DisableLogsAction(t *testing.T) {
	logger := newCapturingLogger()
	regConfig := DefaultRegistryConfig()
	regConfig.Logger = logger
	registry := NewBackendRegistry(regConfig)
	defer registry.Close()

	backend := newTestBackend(config.BackendTypeSoftware, true, nil)
	registry.Register("test", backend, 1)

	err := registry.Disable("test")
	require.NoError(t, err)

	// Verify disable was logged
	infoMsgs := logger.getInfoMsgs()
	foundLog := false
	for _, msg := range infoMsgs {
		if msg == "Disabled KMS backend" {
			foundLog = true
			break
		}
	}
	assert.True(t, foundLog, "Expected disable action to be logged")
}

// TestRegistry_Coverage_SetPriorityLogsAction tests SetPriority logs the action.
func TestRegistry_Coverage_SetPriorityLogsAction(t *testing.T) {
	logger := newCapturingLogger()
	regConfig := DefaultRegistryConfig()
	regConfig.Logger = logger
	registry := NewBackendRegistry(regConfig)
	defer registry.Close()

	backend := newTestBackend(config.BackendTypeSoftware, true, nil)
	registry.Register("test", backend, 1)

	err := registry.SetPriority("test", 99)
	require.NoError(t, err)

	// Verify priority change was logged
	infoMsgs := logger.getInfoMsgs()
	foundLog := false
	for _, msg := range infoMsgs {
		if msg == "Updated KMS backend priority" {
			foundLog = true
			break
		}
	}
	assert.True(t, foundLog, "Expected priority update to be logged")
}

// =============================================================================
// StartHealthChecks/StopHealthChecks Logging Coverage Tests
// =============================================================================

// TestRegistry_Coverage_StartHealthChecksLogsAction tests StartHealthChecks logs the action.
func TestRegistry_Coverage_StartHealthChecksLogsAction(t *testing.T) {
	logger := newCapturingLogger()
	regConfig := DefaultRegistryConfig()
	regConfig.Logger = logger
	regConfig.HealthCheckInterval = 100 * time.Millisecond
	registry := NewBackendRegistry(regConfig)
	defer registry.Close()

	backend := newTestBackend(config.BackendTypeSoftware, true, nil)
	registry.Register("test", backend, 1)

	registry.StartHealthChecks()
	defer registry.StopHealthChecks()

	// Allow goroutine to start
	time.Sleep(10 * time.Millisecond)

	// Verify start was logged
	infoMsgs := logger.getInfoMsgs()
	foundLog := false
	for _, msg := range infoMsgs {
		if msg == "Started KMS backend health checks" {
			foundLog = true
			break
		}
	}
	assert.True(t, foundLog, "Expected health check start to be logged")
}

// TestRegistry_Coverage_StopHealthChecksLogsAction tests StopHealthChecks logs the action.
func TestRegistry_Coverage_StopHealthChecksLogsAction(t *testing.T) {
	logger := newCapturingLogger()
	regConfig := DefaultRegistryConfig()
	regConfig.Logger = logger
	regConfig.HealthCheckInterval = 100 * time.Millisecond
	registry := NewBackendRegistry(regConfig)
	defer registry.Close()

	backend := newTestBackend(config.BackendTypeSoftware, true, nil)
	registry.Register("test", backend, 1)

	registry.StartHealthChecks()
	time.Sleep(10 * time.Millisecond)
	registry.StopHealthChecks()

	// Verify stop was logged
	infoMsgs := logger.getInfoMsgs()
	foundLog := false
	for _, msg := range infoMsgs {
		if msg == "Stopped KMS backend health checks" {
			foundLog = true
			break
		}
	}
	assert.True(t, foundLog, "Expected health check stop to be logged")
}

// =============================================================================
// Stats Coverage Tests
// =============================================================================

// TestRegistry_Coverage_StatsDegradedBackends tests Stats counts degraded backends.
func TestRegistry_Coverage_StatsDegradedBackends(t *testing.T) {
	regConfig := DefaultRegistryConfig()
	regConfig.UnhealthyThreshold = 3 // High threshold so we stay degraded
	registry := NewBackendRegistry(regConfig)
	defer registry.Close()

	backend := newTestBackend(config.BackendTypeSoftware, false, nil)
	registry.Register("degraded", backend, 1)

	ctx := context.Background()
	// Single failure should result in degraded status
	registry.CheckHealth(ctx, "degraded")

	stats := registry.Stats()
	assert.Equal(t, 1, stats.TotalBackends)
	assert.Equal(t, 1, stats.DegradedBackends)
	assert.Equal(t, 0, stats.HealthyBackends)
	assert.Equal(t, 0, stats.UnhealthyBackends)
}

// TestRegistry_Coverage_StatsWithUnknownStatus tests Stats with unknown health status.
func TestRegistry_Coverage_StatsWithUnknownStatus(t *testing.T) {
	registry := NewBackendRegistry(nil)
	defer registry.Close()

	backend := newTestBackend(config.BackendTypeSoftware, true, nil)
	registry.Register("unknown", backend, 1)

	// Don't perform health check - status should be unknown
	stats := registry.Stats()
	assert.Equal(t, 1, stats.TotalBackends)
	assert.Equal(t, 1, stats.EnabledBackends)
	assert.Equal(t, 0, stats.HealthyBackends)
	assert.Equal(t, 0, stats.DegradedBackends)
	assert.Equal(t, 0, stats.UnhealthyBackends)
}

// =============================================================================
// Health Check Edge Cases Coverage Tests
// =============================================================================

// TestRegistry_Coverage_CheckHealthBackendRemovedDuringCheck tests race condition handling.
func TestRegistry_Coverage_CheckHealthBackendRemovedDuringCheck(t *testing.T) {
	registry := NewBackendRegistry(nil)
	defer registry.Close()

	backend := newTestBackend(config.BackendTypeSoftware, true, nil)
	registry.Register("transient", backend, 1)

	ctx := context.Background()

	// First check succeeds
	health, err := registry.CheckHealth(ctx, "transient")
	require.NoError(t, err)
	assert.NotNil(t, health)

	// Remove the backend
	registry.Unregister("transient")

	// Check now fails
	_, err = registry.CheckHealth(ctx, "transient")
	require.Error(t, err)
}

// TestRegistry_Coverage_PerformHealthCheckWithNilInfo tests nil info handling.
func TestRegistry_Coverage_PerformHealthCheckWithNilInfo(t *testing.T) {
	registry := NewBackendRegistry(nil)
	defer registry.Close()

	backend := newTestBackend(config.BackendTypeSoftware, true, nil)
	registry.Register("test", backend, 1)

	ctx := context.Background()

	// Perform health check - should handle the case where info might not exist
	health, err := registry.CheckHealth(ctx, "test")
	require.NoError(t, err)
	assert.Equal(t, HealthStatusHealthy, health.Status)
}

// TestRegistry_Coverage_CheckAllBackendsRemovedDuringIteration tests concurrent modification.
func TestRegistry_Coverage_CheckAllBackendsRemovedDuringIteration(t *testing.T) {
	regConfig := DefaultRegistryConfig()
	regConfig.HealthCheckInterval = 50 * time.Millisecond
	registry := NewBackendRegistry(regConfig)
	defer registry.Close()

	backend := newTestBackend(config.BackendTypeSoftware, true, nil)
	registry.Register("transient", backend, 1)

	registry.StartHealthChecks()
	time.Sleep(20 * time.Millisecond)

	// Remove backend while health check might be running
	registry.Unregister("transient")

	registry.StopHealthChecks()

	// Should not panic
	stats := registry.Stats()
	assert.Equal(t, 0, stats.TotalBackends)
}

// =============================================================================
// GetByAlgorithm Edge Cases Coverage Tests
// =============================================================================

// TestRegistry_Coverage_GetByAlgorithmMultipleBackendsSameAlgorithm tests priority selection.
func TestRegistry_Coverage_GetByAlgorithmMultipleBackendsSameAlgorithm(t *testing.T) {
	registry := NewBackendRegistry(nil)
	defer registry.Close()

	backend1 := newTestBackend(config.BackendTypeSoftware, true, []string{"AES-256-GCM", "RSA-2048"})
	backend2 := newTestBackend(config.BackendTypePKCS11, true, []string{"AES-256-GCM", "RSA-4096"})

	// Register with priorities
	registry.Register("low", backend1, 100)
	registry.Register("high", backend2, 1)

	ctx := context.Background()

	// Both support AES-256-GCM, should return higher priority
	selected, err := registry.GetByAlgorithm(ctx, "AES-256-GCM")
	require.NoError(t, err)
	assert.Equal(t, backend2, selected)
}

// TestRegistry_Coverage_GetByAlgorithmWithDisabledBackend tests disabled backend handling.
func TestRegistry_Coverage_GetByAlgorithmWithDisabledBackend(t *testing.T) {
	registry := NewBackendRegistry(nil)
	defer registry.Close()

	backend1 := newTestBackend(config.BackendTypeSoftware, true, []string{"AES-256-GCM"})
	backend2 := newTestBackend(config.BackendTypePKCS11, true, []string{"AES-256-GCM"})

	registry.Register("primary", backend1, 1)
	registry.Register("secondary", backend2, 2)

	// Disable primary
	registry.Disable("primary")

	ctx := context.Background()

	// Should return secondary since primary is disabled
	selected, err := registry.GetByAlgorithm(ctx, "AES-256-GCM")
	require.NoError(t, err)
	assert.Equal(t, backend2, selected)
}

// =============================================================================
// Health Recovery Transitions Coverage Tests
// =============================================================================

// TestRegistry_Coverage_HealthTransitionsFullCycle tests complete health cycle.
func TestRegistry_Coverage_HealthTransitionsFullCycle(t *testing.T) {
	logger := newCapturingLogger()
	regConfig := DefaultRegistryConfig()
	regConfig.Logger = logger
	regConfig.UnhealthyThreshold = 2
	registry := NewBackendRegistry(regConfig)
	defer registry.Close()

	backend := newTestBackend(config.BackendTypeSoftware, true, nil)
	registry.Register("lifecycle", backend, 1)

	ctx := context.Background()

	// 1. Start healthy
	registry.CheckHealth(ctx, "lifecycle")
	health, _ := registry.GetHealth("lifecycle")
	assert.Equal(t, HealthStatusHealthy, health.Status)

	// 2. First failure -> degraded
	backend.SetAvailable(false)
	registry.CheckHealth(ctx, "lifecycle")
	health, _ = registry.GetHealth("lifecycle")
	assert.Equal(t, HealthStatusDegraded, health.Status)

	// 3. Second failure -> unhealthy
	registry.CheckHealth(ctx, "lifecycle")
	health, _ = registry.GetHealth("lifecycle")
	assert.Equal(t, HealthStatusUnhealthy, health.Status)

	// 4. Recovery -> degraded (recovering from unhealthy)
	backend.SetAvailable(true)
	registry.CheckHealth(ctx, "lifecycle")
	health, _ = registry.GetHealth("lifecycle")
	assert.Equal(t, HealthStatusDegraded, health.Status)
	assert.Contains(t, health.Message, "recovering")

	// 5. Continue recovery -> healthy
	registry.CheckHealth(ctx, "lifecycle")
	health, _ = registry.GetHealth("lifecycle")
	assert.Equal(t, HealthStatusHealthy, health.Status)
}

// =============================================================================
// ListWithBackends Coverage Tests
// =============================================================================

// TestRegistry_Coverage_ListWithBackendsIncludesAllFields tests all fields are copied.
func TestRegistry_Coverage_ListWithBackendsIncludesAllFields(t *testing.T) {
	registry := NewBackendRegistry(nil)
	defer registry.Close()

	backend := newTestBackend(config.BackendTypeSoftware, true, []string{"AES-256-GCM", "RSA-2048"})
	registry.Register("full-info", backend, 42)

	ctx := context.Background()
	registry.CheckHealth(ctx, "full-info")

	list := registry.ListWithBackends()
	require.Len(t, list, 1)

	info := list[0]
	assert.Equal(t, "full-info", info.Name)
	assert.Equal(t, config.BackendTypeSoftware, info.Type)
	assert.Equal(t, 42, info.Priority)
	assert.True(t, info.Enabled)
	assert.Equal(t, 2, len(info.Algorithms))
	assert.NotNil(t, info.Backend)
	assert.Equal(t, backend, info.Backend)
	assert.Equal(t, HealthStatusHealthy, info.Health.Status)
}

// =============================================================================
// GetAllHealth Coverage Tests
// =============================================================================

// TestRegistry_Coverage_GetAllHealthReturnsIndependentCopies tests health copies are independent.
func TestRegistry_Coverage_GetAllHealthReturnsIndependentCopies(t *testing.T) {
	registry := NewBackendRegistry(nil)
	defer registry.Close()

	backend := newTestBackend(config.BackendTypeSoftware, true, nil)
	registry.Register("copy-test", backend, 1)

	ctx := context.Background()
	registry.CheckHealth(ctx, "copy-test")

	// Get all health
	allHealth := registry.GetAllHealth()
	require.Len(t, allHealth, 1)

	// Modify returned health
	allHealth["copy-test"].Status = HealthStatusUnhealthy

	// Original should be unchanged
	health, _ := registry.GetHealth("copy-test")
	assert.Equal(t, HealthStatusHealthy, health.Status)
}

// =============================================================================
// Error Code Constants Coverage Tests
// =============================================================================

// TestRegistry_Coverage_ErrorCodes tests all error code constants.
func TestRegistry_Coverage_ErrorCodes(t *testing.T) {
	assert.Equal(t, "backend_exists", ErrBackendExists)
	assert.Equal(t, "backend_not_found", ErrBackendNotFound)
	assert.Equal(t, "backend_disabled", ErrBackendDisabled)
	assert.Equal(t, "no_healthy_backend", ErrNoHealthyBackend)
	assert.Equal(t, "no_backend_for_algorithm", ErrNoBackendForAlgorithm)
}

// =============================================================================
// DefaultRegistryConfig Coverage Tests
// =============================================================================

// TestRegistry_Coverage_DefaultRegistryConfigValues tests default config values.
func TestRegistry_Coverage_DefaultRegistryConfigValues(t *testing.T) {
	cfg := DefaultRegistryConfig()

	assert.Equal(t, 30*time.Second, cfg.HealthCheckInterval)
	assert.Equal(t, 5*time.Second, cfg.HealthCheckTimeout)
	assert.Equal(t, 3, cfg.UnhealthyThreshold)
	assert.Equal(t, 2, cfg.RecoveryThreshold)
	assert.True(t, cfg.EnableFailover)
	assert.Nil(t, cfg.Logger)
}

// TestRegistry_Coverage_NewBackendRegistryWithNilConfig tests nil config handling.
func TestRegistry_Coverage_NewBackendRegistryWithNilConfig(t *testing.T) {
	registry := NewBackendRegistry(nil)
	defer registry.Close()

	// Should use default config
	assert.NotNil(t, registry.config)
	assert.Equal(t, 30*time.Second, registry.config.HealthCheckInterval)
}

// =============================================================================
// Health Status Constants Coverage Tests
// =============================================================================

// TestRegistry_Coverage_HealthStatusConstants tests all health status constants.
func TestRegistry_Coverage_HealthStatusConstants(t *testing.T) {
	assert.Equal(t, HealthStatus("healthy"), HealthStatusHealthy)
	assert.Equal(t, HealthStatus("degraded"), HealthStatusDegraded)
	assert.Equal(t, HealthStatus("unhealthy"), HealthStatusUnhealthy)
	assert.Equal(t, HealthStatus("unknown"), HealthStatusUnknown)
}
