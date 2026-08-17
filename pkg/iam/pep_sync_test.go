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

package iam

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestPolicyChangeType tests PolicyChangeType string conversion.
func TestPolicyChangeType(t *testing.T) {
	tests := []struct {
		name     string
		pct      PolicyChangeType
		expected string
	}{
		{"Created", PolicyChangeCreated, "created"},
		{"Updated", PolicyChangeUpdated, "updated"},
		{"Deleted", PolicyChangeDeleted, "deleted"},
		{"Snapshot", PolicyChangeSnapshot, "snapshot"},
		{"Unknown", PolicyChangeType(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.pct.String(); got != tt.expected {
				t.Errorf("PolicyChangeType.String() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// TestPolicyChangeEventMarshal tests PolicyChangeEvent binary serialization.
func TestPolicyChangeEventMarshal(t *testing.T) {
	event := PolicyChangeEvent{
		Type:      PolicyChangeCreated,
		PolicyID:  "policy-123",
		TenantID:  "tenant-abc",
		Version:   42,
		Timestamp: time.Now().UTC().Truncate(time.Second),
		Data:      []byte(`{"name":"test-policy"}`),
		NodeID:    "node-1",
	}

	// Marshal
	data, err := event.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary failed: %v", err)
	}

	// Unmarshal
	var decoded PolicyChangeEvent
	if err := decoded.UnmarshalBinary(data); err != nil {
		t.Fatalf("UnmarshalBinary failed: %v", err)
	}

	// Verify fields
	if decoded.Type != event.Type {
		t.Errorf("Type mismatch: got %v, want %v", decoded.Type, event.Type)
	}
	if decoded.PolicyID != event.PolicyID {
		t.Errorf("PolicyID mismatch: got %v, want %v", decoded.PolicyID, event.PolicyID)
	}
	if decoded.TenantID != event.TenantID {
		t.Errorf("TenantID mismatch: got %v, want %v", decoded.TenantID, event.TenantID)
	}
	if decoded.Version != event.Version {
		t.Errorf("Version mismatch: got %v, want %v", decoded.Version, event.Version)
	}
	if decoded.NodeID != event.NodeID {
		t.Errorf("NodeID mismatch: got %v, want %v", decoded.NodeID, event.NodeID)
	}
}

// TestPolicyChangeEventUnmarshalInvalid tests unmarshaling invalid data.
func TestPolicyChangeEventUnmarshalInvalid(t *testing.T) {
	var event PolicyChangeEvent
	err := event.UnmarshalBinary([]byte("not valid json"))
	if err == nil {
		t.Error("UnmarshalBinary should fail with invalid JSON")
	}
}

// TestCacheSyncConfig tests configuration validation.
func TestCacheSyncConfig(t *testing.T) {
	t.Run("DefaultConfig", func(t *testing.T) {
		config := DefaultCacheSyncConfig("node-1", "tenant-1")

		if config.NodeID != "node-1" {
			t.Errorf("NodeID = %v, want node-1", config.NodeID)
		}
		if config.TenantID != "tenant-1" {
			t.Errorf("TenantID = %v, want tenant-1", config.TenantID)
		}
		if config.SyncInterval != 30*time.Second {
			t.Errorf("SyncInterval = %v, want 30s", config.SyncInterval)
		}
		if config.RetryInterval != 5*time.Second {
			t.Errorf("RetryInterval = %v, want 5s", config.RetryInterval)
		}
		if config.MaxRetries != 3 {
			t.Errorf("MaxRetries = %v, want 3", config.MaxRetries)
		}
		if config.BufferSize != 1000 {
			t.Errorf("BufferSize = %v, want 1000", config.BufferSize)
		}
		if !config.EnableMetrics {
			t.Error("EnableMetrics should be true")
		}
	})

	t.Run("ValidConfig", func(t *testing.T) {
		config := DefaultCacheSyncConfig("node-1", "tenant-1")
		if err := config.Validate(); err != nil {
			t.Errorf("Valid config should pass validation: %v", err)
		}
	})

	t.Run("MissingNodeID", func(t *testing.T) {
		config := DefaultCacheSyncConfig("", "tenant-1")
		err := config.Validate()
		if err == nil {
			t.Error("Config with empty NodeID should fail validation")
		}
		var syncErr *InvalidSyncConfigError
		if !errors.As(err, &syncErr) {
			t.Errorf("Expected InvalidSyncConfigError, got %T", err)
		}
	})

	t.Run("MissingTenantID", func(t *testing.T) {
		config := DefaultCacheSyncConfig("node-1", "")
		err := config.Validate()
		if err == nil {
			t.Error("Config with empty TenantID should fail validation")
		}
	})

	t.Run("ZeroSyncInterval", func(t *testing.T) {
		config := DefaultCacheSyncConfig("node-1", "tenant-1")
		config.SyncInterval = 0
		err := config.Validate()
		if err == nil {
			t.Error("Config with zero SyncInterval should fail validation")
		}
	})

	t.Run("NegativeSyncInterval", func(t *testing.T) {
		config := DefaultCacheSyncConfig("node-1", "tenant-1")
		config.SyncInterval = -1 * time.Second
		err := config.Validate()
		if err == nil {
			t.Error("Config with negative SyncInterval should fail validation")
		}
	})

	t.Run("ZeroRetryInterval", func(t *testing.T) {
		config := DefaultCacheSyncConfig("node-1", "tenant-1")
		config.RetryInterval = 0
		err := config.Validate()
		if err == nil {
			t.Error("Config with zero RetryInterval should fail validation")
		}
	})

	t.Run("NegativeMaxRetries", func(t *testing.T) {
		config := DefaultCacheSyncConfig("node-1", "tenant-1")
		config.MaxRetries = -1
		err := config.Validate()
		if err == nil {
			t.Error("Config with negative MaxRetries should fail validation")
		}
	})

	t.Run("ZeroBufferSize", func(t *testing.T) {
		config := DefaultCacheSyncConfig("node-1", "tenant-1")
		config.BufferSize = 0
		err := config.Validate()
		if err == nil {
			t.Error("Config with zero BufferSize should fail validation")
		}
	})
}

// TestInvalidSyncConfigError tests the error type.
func TestInvalidSyncConfigError(t *testing.T) {
	err := &InvalidSyncConfigError{Reason: "test reason"}
	expected := "invalid sync configuration: test reason"
	if err.Error() != expected {
		t.Errorf("Error() = %v, want %v", err.Error(), expected)
	}
}

// TestErrSyncFailed tests the sync failed error type.
func TestErrSyncFailed(t *testing.T) {
	t.Run("WithCause", func(t *testing.T) {
		cause := errors.New("underlying error")
		err := &SyncFailedError{
			Operation: "test operation",
			Cause:     cause,
		}
		expected := "sync failed during test operation: underlying error"
		if err.Error() != expected {
			t.Errorf("Error() = %v, want %v", err.Error(), expected)
		}
		if !errors.Is(err, cause) {
			t.Error("Unwrap should return the cause")
		}
	})

	t.Run("WithoutCause", func(t *testing.T) {
		err := &SyncFailedError{
			Operation: "test operation",
		}
		expected := "sync failed during test operation"
		if err.Error() != expected {
			t.Errorf("Error() = %v, want %v", err.Error(), expected)
		}
	})
}

// TestCacheVersionTracker tests the version tracker.
func TestCacheVersionTracker(t *testing.T) {
	t.Run("InitialVersion", func(t *testing.T) {
		tracker := NewCacheVersionTracker()
		if tracker.GetVersion() != 0 {
			t.Errorf("Initial version should be 0, got %d", tracker.GetVersion())
		}
	})

	t.Run("IncrementVersion", func(t *testing.T) {
		tracker := NewCacheVersionTracker()
		v1 := tracker.IncrementVersion()
		if v1 != 1 {
			t.Errorf("First increment should return 1, got %d", v1)
		}
		v2 := tracker.IncrementVersion()
		if v2 != 2 {
			t.Errorf("Second increment should return 2, got %d", v2)
		}
	})

	t.Run("TenantVersion", func(t *testing.T) {
		tracker := NewCacheVersionTracker()

		// Initial tenant version should be 0
		if tracker.GetTenantVersion("tenant-1") != 0 {
			t.Error("Initial tenant version should be 0")
		}

		// Set tenant version
		tracker.SetTenantVersion("tenant-1", 10)
		if tracker.GetTenantVersion("tenant-1") != 10 {
			t.Errorf("Tenant version should be 10, got %d", tracker.GetTenantVersion("tenant-1"))
		}

		// Global version should be updated
		if tracker.GetVersion() < 10 {
			t.Errorf("Global version should be at least 10, got %d", tracker.GetVersion())
		}

		// Other tenant should still be 0
		if tracker.GetTenantVersion("tenant-2") != 0 {
			t.Error("Other tenant version should be 0")
		}
	})

	t.Run("InvalidationCallback", func(t *testing.T) {
		tracker := NewCacheVersionTracker()

		var called atomic.Bool
		var capturedTenant, capturedPolicy string
		var capturedVersion int64

		tracker.RegisterInvalidationCallback(func(tenantID, policyID string, version int64) {
			called.Store(true)
			capturedTenant = tenantID
			capturedPolicy = policyID
			capturedVersion = version
		})

		tracker.NotifyInvalidation("tenant-1", "policy-1", 42)

		if !called.Load() {
			t.Error("Callback should have been called")
		}
		if capturedTenant != "tenant-1" {
			t.Errorf("Tenant = %v, want tenant-1", capturedTenant)
		}
		if capturedPolicy != "policy-1" {
			t.Errorf("Policy = %v, want policy-1", capturedPolicy)
		}
		if capturedVersion != 42 {
			t.Errorf("Version = %v, want 42", capturedVersion)
		}
	})

	t.Run("MultipleCallbacks", func(t *testing.T) {
		tracker := NewCacheVersionTracker()

		var count atomic.Int32

		tracker.RegisterInvalidationCallback(func(tenantID, policyID string, version int64) {
			count.Add(1)
		})
		tracker.RegisterInvalidationCallback(func(tenantID, policyID string, version int64) {
			count.Add(1)
		})

		tracker.NotifyInvalidation("tenant-1", "policy-1", 1)

		if count.Load() != 2 {
			t.Errorf("Both callbacks should be called, count = %d", count.Load())
		}
	})

	t.Run("ClearCallbacks", func(t *testing.T) {
		tracker := NewCacheVersionTracker()

		var called atomic.Bool
		tracker.RegisterInvalidationCallback(func(tenantID, policyID string, version int64) {
			called.Store(true)
		})

		tracker.ClearCallbacks()
		tracker.NotifyInvalidation("tenant-1", "policy-1", 1)

		if called.Load() {
			t.Error("Callback should not be called after clear")
		}
	})

	t.Run("ConcurrentUpdates", func(t *testing.T) {
		tracker := NewCacheVersionTracker()

		var wg sync.WaitGroup
		const numGoroutines = 100
		const numUpdates = 100

		wg.Add(numGoroutines)
		for i := 0; i < numGoroutines; i++ {
			go func(id int) {
				defer wg.Done()
				for j := 0; j < numUpdates; j++ {
					tracker.IncrementVersion()
					tracker.SetTenantVersion("tenant-1", int64(id*numUpdates+j))
				}
			}(i)
		}

		wg.Wait()

		// Version should be at least numGoroutines * numUpdates
		if tracker.GetVersion() < numGoroutines*numUpdates {
			t.Errorf("Version should be at least %d, got %d", numGoroutines*numUpdates, tracker.GetVersion())
		}
	})
}
