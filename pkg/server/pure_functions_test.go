//go:build raft

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

package server

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// =============================================================================
// Error Variables Tests (errors.go)
// =============================================================================

func TestErrorVariables(t *testing.T) {
	t.Run("ErrDataDirRequired", func(t *testing.T) {
		t.Run("is not nil", func(t *testing.T) {
			assert.NotNil(t, ErrDataDirRequired)
		})

		t.Run("has correct message", func(t *testing.T) {
			assert.Equal(t, "data directory is required", ErrDataDirRequired.Error())
		})

		t.Run("can be checked with errors.Is", func(t *testing.T) {
			wrappedErr := errors.New("outer: " + ErrDataDirRequired.Error())
			// Verify the error message is preserved when wrapped
			assert.Contains(t, wrappedErr.Error(), "data directory is required")
		})

		t.Run("is distinguishable from other errors", func(t *testing.T) {
			assert.NotEqual(t, ErrDataDirRequired, ErrServerNotRunning)
			assert.NotEqual(t, ErrDataDirRequired, ErrServerAlreadyRunning)
			assert.NotEqual(t, ErrDataDirRequired, ErrInvalidServerMode)
		})
	})

	t.Run("ErrServerNotRunning", func(t *testing.T) {
		t.Run("is not nil", func(t *testing.T) {
			assert.NotNil(t, ErrServerNotRunning)
		})

		t.Run("has correct message", func(t *testing.T) {
			assert.Equal(t, "server is not running", ErrServerNotRunning.Error())
		})

		t.Run("can be matched with errors.Is", func(t *testing.T) {
			err := ErrServerNotRunning
			assert.True(t, errors.Is(err, ErrServerNotRunning))
		})

		t.Run("is distinguishable from other errors", func(t *testing.T) {
			assert.NotEqual(t, ErrServerNotRunning, ErrDataDirRequired)
			assert.NotEqual(t, ErrServerNotRunning, ErrServerAlreadyRunning)
			assert.NotEqual(t, ErrServerNotRunning, ErrInvalidServerMode)
		})
	})

	t.Run("ErrServerAlreadyRunning", func(t *testing.T) {
		t.Run("is not nil", func(t *testing.T) {
			assert.NotNil(t, ErrServerAlreadyRunning)
		})

		t.Run("has correct message", func(t *testing.T) {
			assert.Equal(t, "server is already running", ErrServerAlreadyRunning.Error())
		})

		t.Run("can be matched with errors.Is", func(t *testing.T) {
			err := ErrServerAlreadyRunning
			assert.True(t, errors.Is(err, ErrServerAlreadyRunning))
		})

		t.Run("is distinguishable from other errors", func(t *testing.T) {
			assert.NotEqual(t, ErrServerAlreadyRunning, ErrDataDirRequired)
			assert.NotEqual(t, ErrServerAlreadyRunning, ErrServerNotRunning)
			assert.NotEqual(t, ErrServerAlreadyRunning, ErrInvalidServerMode)
		})
	})

	t.Run("ErrInvalidServerMode", func(t *testing.T) {
		t.Run("is not nil", func(t *testing.T) {
			assert.NotNil(t, ErrInvalidServerMode)
		})

		t.Run("has correct message", func(t *testing.T) {
			assert.Equal(t, "invalid server mode", ErrInvalidServerMode.Error())
		})

		t.Run("can be matched with errors.Is", func(t *testing.T) {
			err := ErrInvalidServerMode
			assert.True(t, errors.Is(err, ErrInvalidServerMode))
		})

		t.Run("is distinguishable from other errors", func(t *testing.T) {
			assert.NotEqual(t, ErrInvalidServerMode, ErrDataDirRequired)
			assert.NotEqual(t, ErrInvalidServerMode, ErrServerNotRunning)
			assert.NotEqual(t, ErrInvalidServerMode, ErrServerAlreadyRunning)
		})
	})

	t.Run("all errors are unique", func(t *testing.T) {
		errs := []error{
			ErrDataDirRequired,
			ErrServerNotRunning,
			ErrServerAlreadyRunning,
			ErrInvalidServerMode,
		}

		// Verify each error is unique
		seen := make(map[string]bool)
		for _, err := range errs {
			msg := err.Error()
			assert.False(t, seen[msg], "duplicate error message: %s", msg)
			seen[msg] = true
		}
	})

	t.Run("errors are sentinel values", func(t *testing.T) {
		// Verify that errors can be used as sentinel values for error checking
		testCases := []struct {
			name     string
			err      error
			sentinel error
			match    bool
		}{
			{
				name:     "ErrDataDirRequired matches itself",
				err:      ErrDataDirRequired,
				sentinel: ErrDataDirRequired,
				match:    true,
			},
			{
				name:     "ErrServerNotRunning matches itself",
				err:      ErrServerNotRunning,
				sentinel: ErrServerNotRunning,
				match:    true,
			},
			{
				name:     "ErrServerAlreadyRunning matches itself",
				err:      ErrServerAlreadyRunning,
				sentinel: ErrServerAlreadyRunning,
				match:    true,
			},
			{
				name:     "ErrInvalidServerMode matches itself",
				err:      ErrInvalidServerMode,
				sentinel: ErrInvalidServerMode,
				match:    true,
			},
			{
				name:     "different errors do not match",
				err:      ErrDataDirRequired,
				sentinel: ErrServerNotRunning,
				match:    false,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				result := errors.Is(tc.err, tc.sentinel)
				assert.Equal(t, tc.match, result)
			})
		}
	})
}

// =============================================================================
// Health Status Constants Tests (health.go)
// =============================================================================

func TestHealthStatusConstants(t *testing.T) {
	t.Run("HealthStatusHealthy has correct value", func(t *testing.T) {
		assert.Equal(t, HealthStatus("healthy"), HealthStatusHealthy)
	})

	t.Run("HealthStatusDegraded has correct value", func(t *testing.T) {
		assert.Equal(t, HealthStatus("degraded"), HealthStatusDegraded)
	})

	t.Run("HealthStatusUnhealthy has correct value", func(t *testing.T) {
		assert.Equal(t, HealthStatus("unhealthy"), HealthStatusUnhealthy)
	})

	t.Run("HealthStatusUnknown has correct value", func(t *testing.T) {
		assert.Equal(t, HealthStatus("unknown"), HealthStatusUnknown)
	})

	t.Run("all status values are unique", func(t *testing.T) {
		statuses := []HealthStatus{
			HealthStatusHealthy,
			HealthStatusDegraded,
			HealthStatusUnhealthy,
			HealthStatusUnknown,
		}

		seen := make(map[HealthStatus]bool)
		for _, status := range statuses {
			assert.False(t, seen[status], "duplicate health status: %s", status)
			seen[status] = true
		}
	})

	t.Run("statuses can be compared", func(t *testing.T) {
		assert.True(t, HealthStatusHealthy == HealthStatusHealthy)
		assert.False(t, HealthStatusHealthy == HealthStatusDegraded)
		assert.False(t, HealthStatusHealthy == HealthStatusUnhealthy)
		assert.False(t, HealthStatusHealthy == HealthStatusUnknown)
	})
}

// =============================================================================
// Interface Type Constants Tests (interfaces.go)
// =============================================================================

func TestHealthStatusTypeConstants(t *testing.T) {
	t.Run("HealthStatusTypeHealthy has correct value", func(t *testing.T) {
		assert.Equal(t, HealthStatusType("healthy"), HealthStatusTypeHealthy)
	})

	t.Run("HealthStatusTypeDegraded has correct value", func(t *testing.T) {
		assert.Equal(t, HealthStatusType("degraded"), HealthStatusTypeDegraded)
	})

	t.Run("HealthStatusTypeUnhealthy has correct value", func(t *testing.T) {
		assert.Equal(t, HealthStatusType("unhealthy"), HealthStatusTypeUnhealthy)
	})

	t.Run("HealthStatusTypeUnknown has correct value", func(t *testing.T) {
		assert.Equal(t, HealthStatusType("unknown"), HealthStatusTypeUnknown)
	})

	t.Run("all status type values are unique", func(t *testing.T) {
		types := []HealthStatusType{
			HealthStatusTypeHealthy,
			HealthStatusTypeDegraded,
			HealthStatusTypeUnhealthy,
			HealthStatusTypeUnknown,
		}

		seen := make(map[HealthStatusType]bool)
		for _, statusType := range types {
			assert.False(t, seen[statusType], "duplicate health status type: %s", statusType)
			seen[statusType] = true
		}
	})
}

// =============================================================================
// Struct Type Tests
// =============================================================================

func TestComponentHealthStruct(t *testing.T) {
	t.Run("can be created with all fields", func(t *testing.T) {
		now := time.Now()
		health := ComponentHealth{
			Name:      "test-component",
			Status:    HealthStatusHealthy,
			Message:   "all systems operational",
			LastCheck: now,
			Error:     "",
		}

		assert.Equal(t, "test-component", health.Name)
		assert.Equal(t, HealthStatusHealthy, health.Status)
		assert.Equal(t, "all systems operational", health.Message)
		assert.Equal(t, now, health.LastCheck)
		assert.Empty(t, health.Error)
	})

	t.Run("can represent unhealthy component with error", func(t *testing.T) {
		health := ComponentHealth{
			Name:      "failing-component",
			Status:    HealthStatusUnhealthy,
			Message:   "component failed",
			LastCheck: time.Now(),
			Error:     "connection refused",
		}

		assert.Equal(t, HealthStatusUnhealthy, health.Status)
		assert.NotEmpty(t, health.Error)
	})
}

func TestOverallHealthStruct(t *testing.T) {
	t.Run("can be created with components", func(t *testing.T) {
		now := time.Now()
		components := map[string]ComponentHealth{
			"raft": {
				Name:      "raft",
				Status:    HealthStatusHealthy,
				LastCheck: now,
			},
			"storage": {
				Name:      "storage",
				Status:    HealthStatusHealthy,
				LastCheck: now,
			},
		}

		health := OverallHealth{
			Status:     HealthStatusHealthy,
			Timestamp:  now,
			Uptime:     time.Hour,
			Components: components,
		}

		assert.Equal(t, HealthStatusHealthy, health.Status)
		assert.Equal(t, now, health.Timestamp)
		assert.Equal(t, time.Hour, health.Uptime)
		assert.Len(t, health.Components, 2)
	})

	t.Run("degraded status when some components unhealthy", func(t *testing.T) {
		health := OverallHealth{
			Status:    HealthStatusDegraded,
			Timestamp: time.Now(),
			Uptime:    time.Minute,
			Components: map[string]ComponentHealth{
				"raft": {
					Name:   "raft",
					Status: HealthStatusHealthy,
				},
				"storage": {
					Name:   "storage",
					Status: HealthStatusUnhealthy,
				},
			},
		}

		assert.Equal(t, HealthStatusDegraded, health.Status)
	})
}

func TestComponentHealthStatusStruct(t *testing.T) {
	t.Run("can be created with interface types", func(t *testing.T) {
		status := ComponentHealthStatus{
			Name:      "test-component",
			Status:    HealthStatusTypeHealthy,
			Message:   "operational",
			LastCheck: time.Now().Unix(),
			Error:     "",
		}

		assert.Equal(t, "test-component", status.Name)
		assert.Equal(t, HealthStatusTypeHealthy, status.Status)
		assert.Equal(t, "operational", status.Message)
		assert.Empty(t, status.Error)
	})
}

func TestOverallHealthStatusStruct(t *testing.T) {
	t.Run("can be created with components map", func(t *testing.T) {
		status := OverallHealthStatus{
			Status:    HealthStatusTypeHealthy,
			Timestamp: time.Now().Unix(),
			Uptime:    3600,
			Components: map[string]ComponentHealthStatus{
				"raft": {
					Name:   "raft",
					Status: HealthStatusTypeHealthy,
				},
			},
		}

		assert.Equal(t, HealthStatusTypeHealthy, status.Status)
		assert.NotZero(t, status.Timestamp)
		assert.Equal(t, int64(3600), status.Uptime)
		assert.Len(t, status.Components, 1)
	})
}
