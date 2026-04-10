//go:build rest

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

package rest

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

func TestServer_IsStartedIsClosed(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "error",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	mockSvc := testutil.NewMockQuicBaaS()
	config := DefaultConfig()
	config.Address = "127.0.0.1:0"

	server, err := NewServer(mockSvc, config, log)
	require.NoError(t, err)

	// Initially not started and not closed
	assert.False(t, server.IsStarted())
	assert.False(t, server.IsClosed())

	// Start server
	err = server.Start("")
	require.NoError(t, err)

	// Now started but not closed
	assert.True(t, server.IsStarted())
	assert.False(t, server.IsClosed())

	// Wait for server to be ready
	time.Sleep(50 * time.Millisecond)

	// Close server
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = server.Shutdown(ctx)
	require.NoError(t, err)

	// Now closed
	assert.True(t, server.IsClosed())
}

func TestServer_StartErrors(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "error",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	t.Run("start already started server", func(t *testing.T) {
		mockSvc := testutil.NewMockQuicBaaS()
		config := DefaultConfig()
		config.Address = "127.0.0.1:0"

		server, err := NewServer(mockSvc, config, log)
		require.NoError(t, err)

		err = server.Start("")
		require.NoError(t, err)

		// Try to start again
		err = server.Start("")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "already started")

		// Cleanup
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(ctx)
	})

	t.Run("start closed server", func(t *testing.T) {
		mockSvc := testutil.NewMockQuicBaaS()
		config := DefaultConfig()

		server, err := NewServer(mockSvc, config, log)
		require.NoError(t, err)

		// Close server first
		server.Close()

		// Try to start
		err = server.Start("")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "client is closed")
	})

	t.Run("start with invalid address", func(t *testing.T) {
		mockSvc := testutil.NewMockQuicBaaS()
		config := DefaultConfig()
		config.Address = "invalid-address-no-port"

		server, err := NewServer(mockSvc, config, log)
		require.NoError(t, err)

		err = server.Start("")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "listen on")
	})
}

func TestServer_StartWithMaxConnections(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "error",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	t.Run("listener wrapped when MaxConnections is set", func(t *testing.T) {
		mockSvc := testutil.NewMockQuicBaaS()
		config := DefaultConfig()
		config.Address = "127.0.0.1:0"
		config.MaxConnections = 10

		server, err := NewServer(mockSvc, config, log)
		require.NoError(t, err)

		err = server.Start("")
		require.NoError(t, err)

		// Verify listener is a limitListener
		_, isLimited := server.listener.(*limitListener)
		assert.True(t, isLimited, "listener should be a limitListener when MaxConnections > 0")

		// Verify the server is reachable
		addr := server.listener.Addr().String()
		conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
		require.NoError(t, err)
		conn.Close()

		// Cleanup
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(ctx)
	})

	t.Run("listener not wrapped when MaxConnections is zero", func(t *testing.T) {
		mockSvc := testutil.NewMockQuicBaaS()
		config := DefaultConfig()
		config.Address = "127.0.0.1:0"
		config.MaxConnections = 0

		server, err := NewServer(mockSvc, config, log)
		require.NoError(t, err)

		err = server.Start("")
		require.NoError(t, err)

		// Verify listener is NOT a limitListener
		_, isLimited := server.listener.(*limitListener)
		assert.False(t, isLimited, "listener should not be a limitListener when MaxConnections is 0")

		// Cleanup
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(ctx)
	})
}

func TestServer_Uptime(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "error",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	mockSvc := testutil.NewMockQuicBaaS()
	config := DefaultConfig()

	server, err := NewServer(mockSvc, config, log)
	require.NoError(t, err)

	// Check uptime is positive
	uptime := server.Uptime()
	assert.True(t, uptime > 0)

	// Wait a bit
	time.Sleep(10 * time.Millisecond)

	// Uptime should have increased
	uptime2 := server.Uptime()
	assert.True(t, uptime2 > uptime)

	server.Close()
}

func TestHandleHealth(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "error",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	t.Run("healthy response", func(t *testing.T) {
		mockSvc := testutil.NewMockQuicBaaS()
		mockSvc.On("Health", mock.Anything, mock.Anything).Return(&service.HealthResponse{
			Status:        service.HealthStatusHealthy,
			NodeID:        1,
			UptimeSeconds: 100,
			Details: map[string]*service.ComponentHealth{
				"storage": {
					Status:  service.HealthStatusHealthy,
					Message: "storage is healthy",
				},
			},
		}, nil)

		config := DefaultConfig()
		server, err := NewServer(mockSvc, config, log)
		require.NoError(t, err)
		defer server.Close()

		req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
		w := httptest.NewRecorder()

		server.HandleHealth(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		mockSvc.AssertExpectations(t)
	})

	t.Run("unhealthy response", func(t *testing.T) {
		mockSvc := testutil.NewMockQuicBaaS()
		mockSvc.On("Health", mock.Anything, mock.Anything).Return(&service.HealthResponse{
			Status:        service.HealthStatusUnhealthy,
			NodeID:        1,
			UptimeSeconds: 100,
			Details:       map[string]*service.ComponentHealth{},
		}, nil)

		config := DefaultConfig()
		server, err := NewServer(mockSvc, config, log)
		require.NoError(t, err)
		defer server.Close()

		req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
		w := httptest.NewRecorder()

		server.HandleHealth(w, req)

		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
		mockSvc.AssertExpectations(t)
	})

	t.Run("health check error", func(t *testing.T) {
		mockSvc := testutil.NewMockQuicBaaS()
		mockSvc.On("Health", mock.Anything, mock.Anything).Return(nil, assert.AnError)

		config := DefaultConfig()
		server, err := NewServer(mockSvc, config, log)
		require.NoError(t, err)
		defer server.Close()

		req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
		w := httptest.NewRecorder()

		server.HandleHealth(w, req)

		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
		mockSvc.AssertExpectations(t)
	})
}

func TestHandleReadiness(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "error",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	t.Run("ready response", func(t *testing.T) {
		mockSvc := testutil.NewMockQuicBaaS()
		mockSvc.On("Readiness", mock.Anything, mock.Anything).Return(&service.ReadinessResponse{
			Ready:  true,
			Reason: "service is ready",
		}, nil)

		config := DefaultConfig()
		server, err := NewServer(mockSvc, config, log)
		require.NoError(t, err)
		defer server.Close()

		req := httptest.NewRequest(http.MethodGet, "/api/v1/health/ready", nil)
		w := httptest.NewRecorder()

		server.HandleReadiness(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		mockSvc.AssertExpectations(t)
	})

	t.Run("not ready response", func(t *testing.T) {
		mockSvc := testutil.NewMockQuicBaaS()
		mockSvc.On("Readiness", mock.Anything, mock.Anything).Return(&service.ReadinessResponse{
			Ready:  false,
			Reason: "service is not ready",
		}, nil)

		config := DefaultConfig()
		server, err := NewServer(mockSvc, config, log)
		require.NoError(t, err)
		defer server.Close()

		req := httptest.NewRequest(http.MethodGet, "/api/v1/health/ready", nil)
		w := httptest.NewRecorder()

		server.HandleReadiness(w, req)

		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
		mockSvc.AssertExpectations(t)
	})

	t.Run("readiness check error", func(t *testing.T) {
		mockSvc := testutil.NewMockQuicBaaS()
		mockSvc.On("Readiness", mock.Anything, mock.Anything).Return(nil, assert.AnError)

		config := DefaultConfig()
		server, err := NewServer(mockSvc, config, log)
		require.NoError(t, err)
		defer server.Close()

		req := httptest.NewRequest(http.MethodGet, "/api/v1/health/ready", nil)
		w := httptest.NewRecorder()

		server.HandleReadiness(w, req)

		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
		mockSvc.AssertExpectations(t)
	})
}

func TestHandleLiveness_ErrorCases(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "error",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	t.Run("liveness error", func(t *testing.T) {
		mockSvc := testutil.NewMockQuicBaaS()
		mockSvc.On("Liveness", mock.Anything, mock.Anything).Return(nil, assert.AnError)

		config := DefaultConfig()
		server, err := NewServer(mockSvc, config, log)
		require.NoError(t, err)
		defer server.Close()

		req := httptest.NewRequest(http.MethodGet, "/api/v1/health/live", nil)
		w := httptest.NewRecorder()

		server.HandleLiveness(w, req)

		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
		mockSvc.AssertExpectations(t)
	})

	t.Run("not alive", func(t *testing.T) {
		mockSvc := testutil.NewMockQuicBaaS()
		mockSvc.On("Liveness", mock.Anything, mock.Anything).Return(&service.LivenessResponse{
			Alive: false,
		}, nil)

		config := DefaultConfig()
		server, err := NewServer(mockSvc, config, log)
		require.NoError(t, err)
		defer server.Close()

		req := httptest.NewRequest(http.MethodGet, "/api/v1/health/live", nil)
		w := httptest.NewRecorder()

		server.HandleLiveness(w, req)

		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
		mockSvc.AssertExpectations(t)
	})
}

func TestConvertToServiceConsistency(t *testing.T) {
	tests := []struct {
		name     string
		input    ConsistencyLevel
		expected service.ConsistencyLevel
	}{
		{
			name:     "eventual",
			input:    ConsistencyEventual,
			expected: service.ConsistencyLevelEventual,
		},
		{
			name:     "linearizable",
			input:    ConsistencyLinearizable,
			expected: service.ConsistencyLevelLinearizable,
		},
		{
			name:     "strong",
			input:    ConsistencyStrong,
			expected: service.ConsistencyLevelStrong,
		},
		{
			name:     "empty defaults to linearizable",
			input:    "",
			expected: service.ConsistencyLevelLinearizable,
		},
		{
			name:     "unknown defaults to linearizable",
			input:    "unknown",
			expected: service.ConsistencyLevelLinearizable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertToServiceConsistency(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
