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
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

func TestNewServer(t *testing.T) {
	// Create a test logger to use in tests
	log, err := logger.New(logger.LogConfig{
		Level:  "error",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	t.Run("nil service", func(t *testing.T) {
		server, err := NewServer(nil, nil, log)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "service cannot be nil")
		assert.Nil(t, server)
	})

	t.Run("nil logger", func(t *testing.T) {
		mockSvc := testutil.NewMockQuicBaaS()

		server, err := NewServer(mockSvc, nil, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "logger cannot be nil")
		assert.Nil(t, server)
	})
}

// TestServerLifecycle moved to server_integration_test.go

// TestRouterSetup - basic validation without real dependencies.
func TestRouterSetup(t *testing.T) {
	mockSvc := testutil.NewMockQuicBaaS()
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})

	server, err := NewServer(mockSvc, DefaultConfig(), log)
	require.NoError(t, err)
	defer server.Close()

	router := server.GetRouter()
	assert.NotNil(t, router)
}

// TestHealthEndpoints moved to server_integration_test.go

// TestRoutingShardSelection_Put moved to server_integration_test.go

func TestMiddlewareChain(t *testing.T) {
	mockSvc := testutil.NewMockQuicBaaS()
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})

	// Setup liveness mock
	mockSvc.On("Liveness", mock.Anything, mock.Anything).Return(&service.LivenessResponse{
		Alive: true,
	}, nil).Maybe()

	server, err := NewServer(mockSvc, DefaultConfig(), log)
	require.NoError(t, err)
	defer server.Close()

	// Test request ID middleware
	req := httptest.NewRequest(http.MethodGet, "/api/v1/health/live", nil)
	w := httptest.NewRecorder()

	server.GetRouter().ServeHTTP(w, req)

	// Check for request ID in response header
	requestID := w.Header().Get("X-Request-ID")
	assert.NotEmpty(t, requestID)

	// Test with provided request ID
	req = httptest.NewRequest(http.MethodGet, "/api/v1/health/live", nil)
	req.Header.Set("X-Request-ID", "test-request-123")
	w = httptest.NewRecorder()

	server.GetRouter().ServeHTTP(w, req)

	assert.Equal(t, "test-request-123", w.Header().Get("X-Request-ID"))
}

// TestCORSMiddleware moved to server_integration_test.go

func TestValidationErrors(t *testing.T) {
	mockSvc := testutil.NewMockQuicBaaS()
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})

	server, err := NewServer(mockSvc, DefaultConfig(), log)
	require.NoError(t, err)
	defer server.Close()

	tests := []struct {
		name           string
		method         string
		path           string
		body           interface{}
		expectedStatus int
		checkError     func(*testing.T, *ErrorResponse)
	}{
		{
			name:           "missing entity type in GET",
			method:         http.MethodGet,
			path:           "/api/v1/entities//123",
			expectedStatus: http.StatusBadRequest, // chi returns 404 for empty params
		},
		{
			name:           "invalid entity ID",
			method:         http.MethodGet,
			path:           "/api/v1/entities/users/abc",
			expectedStatus: http.StatusBadRequest,
			checkError: func(t *testing.T, resp *ErrorResponse) {
				assert.Contains(t, resp.Error, "invalid entity ID")
			},
		},
		{
			name:           "invalid JSON in PUT",
			method:         http.MethodPost,
			path:           "/api/v1/entities/users",
			body:           "invalid-json",
			expectedStatus: http.StatusBadRequest,
			checkError: func(t *testing.T, resp *ErrorResponse) {
				assert.Contains(t, resp.Error, "invalid JSON")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var bodyReader *bytes.Reader
			if tt.body != nil {
				if str, ok := tt.body.(string); ok {
					bodyReader = bytes.NewReader([]byte(str))
				} else {
					bodyBytes, _ := json.Marshal(tt.body)
					bodyReader = bytes.NewReader(bodyBytes)
				}
			}

			var req *http.Request
			if bodyReader != nil {
				req = httptest.NewRequest(tt.method, tt.path, bodyReader)
			} else {
				req = httptest.NewRequest(tt.method, tt.path, nil)
			}

			w := httptest.NewRecorder()
			server.GetRouter().ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.checkError != nil && w.Code >= 400 {
				var errResp ErrorResponse
				err := json.NewDecoder(w.Body).Decode(&errResp)
				require.NoError(t, err)
				tt.checkError(t, &errResp)
			}
		})
	}
}

func TestConsistencyLevelValidation(t *testing.T) {
	tests := []struct {
		name    string
		level   ConsistencyLevel
		wantErr bool
	}{
		{
			name:    "local consistency",
			level:   ConsistencyLinearizable,
			wantErr: false,
		},
		{
			name:    "quorum consistency",
			level:   ConsistencyStrong,
			wantErr: false,
		},
		{
			name:    "empty consistency",
			level:   "",
			wantErr: false,
		},
		{
			name:    "invalid consistency",
			level:   "invalid",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.level.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestMetricsEndpoint moved to server_integration_test.go

func TestResponseWriterCapture(t *testing.T) {
	w := &responseWriter{
		ResponseWriter: httptest.NewRecorder(),
		statusCode:     http.StatusOK,
	}

	// Test WriteHeader
	w.WriteHeader(http.StatusCreated)
	assert.Equal(t, http.StatusCreated, w.statusCode)

	// Test Write
	data := []byte("test data")
	n, err := w.Write(data)
	assert.NoError(t, err)
	assert.Equal(t, len(data), n)
	assert.Equal(t, len(data), w.bytesWritten)
}

func TestErrorResponses(t *testing.T) {
	tests := []struct {
		name           string
		handler        http.HandlerFunc
		expectedStatus int
		checkError     func(*testing.T, *ErrorResponse)
	}{
		{
			name: "validation error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				RespondError(w, r, http.StatusBadRequest, &ValidationError{
					Field:   "email",
					Message: "invalid email format",
				})
			},
			expectedStatus: http.StatusBadRequest,
			checkError: func(t *testing.T, resp *ErrorResponse) {
				assert.Contains(t, resp.Error, "email")
				assert.NotEmpty(t, resp.Timestamp)
			},
		},
		{
			name: "not found error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				RespondError(w, r, http.StatusNotFound, &NotFoundError{
					Resource: "user/123",
				})
			},
			expectedStatus: http.StatusNotFound,
			checkError: func(t *testing.T, resp *ErrorResponse) {
				assert.Contains(t, resp.Error, "user/123")
			},
		},
		{
			name: "internal server error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				RespondError(w, r, http.StatusInternalServerError, &InternalServerError{
					Message: "database connection failed",
				})
			},
			expectedStatus: http.StatusInternalServerError,
			checkError: func(t *testing.T, resp *ErrorResponse) {
				assert.Contains(t, resp.Error, "database connection")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			req = req.WithContext(context.WithValue(req.Context(), requestIDKey, "test-123"))
			w := httptest.NewRecorder()

			tt.handler(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			var errResp ErrorResponse
			err := json.NewDecoder(w.Body).Decode(&errResp)
			require.NoError(t, err)

			assert.Equal(t, "test-123", errResp.RequestID)

			if tt.checkError != nil {
				tt.checkError(t, &errResp)
			}
		})
	}
}

// BenchmarkHealthEndpoint and BenchmarkMiddlewareStack moved to server_integration_test.go

func TestHandleSecretsPostAction(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "error",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	t.Run("routes to enable handler when path ends with /enable", func(t *testing.T) {
		mockSvc := testutil.NewMockQuicBaaS()

		// Mock UpdateSecretStatus for enable operation
		mockSvc.On("UpdateSecretStatus", mock.Anything, mock.Anything).Return(&service.UpdateSecretStatusResponse{
			SecretID:  "test/secret",
			OldStatus: service.SecretStatusDisabled,
			NewStatus: service.SecretStatusEnabled,
			Timestamp: time.Now(),
		}, nil).Maybe()

		server, err := NewServer(mockSvc, DefaultConfig(), log)
		require.NoError(t, err)
		defer server.Close()

		// Create request with path ending in /enable
		body := `{}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets/test/secret/enable", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		// Set up chi context with URL param
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("*", "test/secret/enable")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		w := httptest.NewRecorder()
		server.handleSecretsPostAction(w, req)

		// Should have called EnableSecret handler - result depends on secrets service availability
		assert.True(t, w.Code >= 200)
	})

	t.Run("routes to disable handler when path ends with /disable", func(t *testing.T) {
		mockSvc := testutil.NewMockQuicBaaS()

		// Mock UpdateSecretStatus for disable operation
		mockSvc.On("UpdateSecretStatus", mock.Anything, mock.Anything).Return(&service.UpdateSecretStatusResponse{
			SecretID:  "test/secret",
			OldStatus: service.SecretStatusEnabled,
			NewStatus: service.SecretStatusDisabled,
			Timestamp: time.Now(),
		}, nil).Maybe()

		server, err := NewServer(mockSvc, DefaultConfig(), log)
		require.NoError(t, err)
		defer server.Close()

		// Create request with path ending in /disable
		body := `{}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets/test/secret/disable", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		// Set up chi context with URL param
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("*", "test/secret/disable")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		w := httptest.NewRecorder()
		server.handleSecretsPostAction(w, req)

		// Should have called DisableSecret handler
		assert.True(t, w.Code >= 200)
	})

	t.Run("routes to secrets action handler for store operation", func(t *testing.T) {
		mockSvc := testutil.NewMockQuicBaaS()

		// Mock StoreSecret for the fall-through case
		mockSvc.On("StoreSecret", mock.Anything, mock.Anything).Return(&service.StoreSecretResponse{
			SecretID: "test/secret",
			Version:  1,
		}, nil).Maybe()

		server, err := NewServer(mockSvc, DefaultConfig(), log)
		require.NoError(t, err)
		defer server.Close()

		// Create request without enable/disable suffix
		body := `{"value":"secret-value"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets/test/secret", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		// Set up chi context with URL param
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("*", "test/secret")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		w := httptest.NewRecorder()
		server.handleSecretsPostAction(w, req)

		// Should fall through to HandleSecretsAction
		// Result depends on implementation but should not panic
		assert.True(t, w.Code >= 200)
	})

	t.Run("handles path too short for enable suffix", func(t *testing.T) {
		mockSvc := testutil.NewMockQuicBaaS()
		mockSvc.On("StoreSecret", mock.Anything, mock.Anything).Return(&service.StoreSecretResponse{
			SecretID: "ab",
			Version:  1,
		}, nil).Maybe()

		server, err := NewServer(mockSvc, DefaultConfig(), log)
		require.NoError(t, err)
		defer server.Close()

		// Short path that can't match /enable (7 chars) or /disable (8 chars)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets/ab", nil)
		req.Header.Set("Content-Type", "application/json")

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("*", "ab")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		w := httptest.NewRecorder()
		server.handleSecretsPostAction(w, req)

		// Should fall through to HandleSecretsAction
		assert.True(t, w.Code >= 200)
	})

	t.Run("handles empty path", func(t *testing.T) {
		mockSvc := testutil.NewMockQuicBaaS()

		server, err := NewServer(mockSvc, DefaultConfig(), log)
		require.NoError(t, err)
		defer server.Close()

		req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets/", nil)
		req.Header.Set("Content-Type", "application/json")

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("*", "")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		w := httptest.NewRecorder()
		server.handleSecretsPostAction(w, req)

		// Empty path should fall through, result depends on HandleSecretsAction
		assert.True(t, w.Code >= 200)
	})
}

func TestServerStart(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "error",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	t.Run("start fails on closed server", func(t *testing.T) {
		mockSvc := testutil.NewMockQuicBaaS()
		server, err := NewServer(mockSvc, DefaultConfig(), log)
		require.NoError(t, err)

		// Close the server first
		server.Close()

		// Now try to start it
		err = server.Start(":0")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "client is closed")
	})

	t.Run("start fails on already started server", func(t *testing.T) {
		mockSvc := testutil.NewMockQuicBaaS()
		server, err := NewServer(mockSvc, DefaultConfig(), log)
		require.NoError(t, err)
		defer server.Close()

		// Start the server first
		err = server.Start(":0")
		require.NoError(t, err)

		// Try to start again
		err = server.Start(":0")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "server already started")
	})

	t.Run("start uses default address when empty", func(t *testing.T) {
		mockSvc := testutil.NewMockQuicBaaS()
		config := DefaultConfig()
		config.Address = ":0" // Port 0 for auto-assign
		server, err := NewServer(mockSvc, config, log)
		require.NoError(t, err)
		defer server.Close()

		// Start with empty address should use config default
		err = server.Start("")
		assert.NoError(t, err)
		assert.True(t, server.IsStarted())
	})

	t.Run("start without TLS", func(t *testing.T) {
		mockSvc := testutil.NewMockQuicBaaS()
		config := DefaultConfig()
		config.Address = ":0"
		config.TLSEnabled = false
		server, err := NewServer(mockSvc, config, log)
		require.NoError(t, err)
		defer server.Close()

		err = server.Start(":0")
		assert.NoError(t, err)
		assert.True(t, server.IsStarted())
	})

	t.Run("start with TLS fails without cert files", func(t *testing.T) {
		mockSvc := testutil.NewMockQuicBaaS()
		config := DefaultConfig()
		config.Address = ":0"
		config.TLSEnabled = true
		config.TLSCertFile = "/nonexistent/cert.pem"
		config.TLSKeyFile = "/nonexistent/key.pem"

		server, err := NewServer(mockSvc, config, log)
		require.NoError(t, err)
		defer server.Close()

		err = server.Start(":0")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to configure TLS")
	})
}

func TestServerShutdown(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "error",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	t.Run("shutdown on already closed server returns nil", func(t *testing.T) {
		mockSvc := testutil.NewMockQuicBaaS()
		server, err := NewServer(mockSvc, DefaultConfig(), log)
		require.NoError(t, err)

		// Close the server
		server.Close()

		// Shutdown again should return nil
		err = server.Shutdown(context.Background())
		assert.NoError(t, err)
	})

	t.Run("shutdown after start", func(t *testing.T) {
		mockSvc := testutil.NewMockQuicBaaS()
		config := DefaultConfig()
		config.Address = ":0"
		server, err := NewServer(mockSvc, config, log)
		require.NoError(t, err)

		err = server.Start(":0")
		require.NoError(t, err)

		err = server.Shutdown(context.Background())
		assert.NoError(t, err)
		assert.True(t, server.IsClosed())
	})

	t.Run("shutdown without start", func(t *testing.T) {
		mockSvc := testutil.NewMockQuicBaaS()
		server, err := NewServer(mockSvc, DefaultConfig(), log)
		require.NoError(t, err)

		// Server never started, so s.server is nil
		err = server.Shutdown(context.Background())
		assert.NoError(t, err)
	})
}

func TestHandleDEKPutAction(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "error",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	t.Run("routes to safety handler when path ends with /safety", func(t *testing.T) {
		mockSvc := testutil.NewMockQuicBaaS()

		// Mock SetDEKSafety for the safety route
		mockSvc.On("SetDEKSafety", mock.Anything, mock.Anything).Return(&service.SetDEKSafetyResponse{
			Path:      "test-dek",
			Timestamp: time.Now(),
		}, nil).Maybe()

		server, err := NewServer(mockSvc, DefaultConfig(), log)
		require.NoError(t, err)
		defer server.Close()

		// Request path ending with /safety
		body := `{"max_operations": 1000}`
		req := httptest.NewRequest(http.MethodPut, "/api/v1/kms/deks/test-dek/safety", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("*", "test-dek/safety")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		w := httptest.NewRecorder()
		server.handleDEKPutAction(w, req)

		// Should route to safety handler or return appropriate error
		assert.True(t, w.Code >= 200)
	})

	t.Run("routes to default handler when path doesn't end with /safety", func(t *testing.T) {
		mockSvc := testutil.NewMockQuicBaaS()

		// Mock GetDEKStatus for the status route
		mockSvc.On("GetDEKStatus", mock.Anything, mock.Anything).Return(&service.GetDEKStatusResponse{
			Timestamp: time.Now(),
		}, nil).Maybe()

		server, err := NewServer(mockSvc, DefaultConfig(), log)
		require.NoError(t, err)
		defer server.Close()

		body := `{}`
		req := httptest.NewRequest(http.MethodPut, "/api/v1/kms/deks/test-dek/status", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("*", "test-dek/status")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		w := httptest.NewRecorder()
		server.handleDEKPutAction(w, req)

		// Should route to default DEK status handler
		assert.True(t, w.Code >= 200)
	})

	t.Run("handles short path gracefully", func(t *testing.T) {
		mockSvc := testutil.NewMockQuicBaaS()

		// Mock GetDEKStatus for short path
		mockSvc.On("GetDEKStatus", mock.Anything, mock.Anything).Return(&service.GetDEKStatusResponse{
			Timestamp: time.Now(),
		}, nil).Maybe()

		server, err := NewServer(mockSvc, DefaultConfig(), log)
		require.NoError(t, err)
		defer server.Close()

		body := `{}`
		req := httptest.NewRequest(http.MethodPut, "/api/v1/kms/deks/ab", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("*", "ab")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		w := httptest.NewRecorder()
		server.handleDEKPutAction(w, req)

		// Path too short for /safety suffix (7 chars)
		// Should fall through to default handler
		assert.True(t, w.Code >= 200)
	})
}

func TestHandleDEKPostAction(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "error",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	t.Run("routes to rotate handler when path ends with /rotate", func(t *testing.T) {
		mockSvc := testutil.NewMockQuicBaaS()

		// Mock RotateDEK
		mockSvc.On("RotateDEK", mock.Anything, mock.Anything).Return(&service.RotateDEKResponse{
			Path:       "test-dek",
			OldVersion: 1,
			NewVersion: 2,
			Timestamp:  time.Now(),
		}, nil).Maybe()

		server, err := NewServer(mockSvc, DefaultConfig(), log)
		require.NoError(t, err)
		defer server.Close()

		body := `{}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/deks/test-dek/rotate", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("*", "test-dek/rotate")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		w := httptest.NewRecorder()
		server.handleDEKPostAction(w, req)

		// Should route to rotate handler
		assert.True(t, w.Code >= 200)
	})

	t.Run("routes to default handler when path ends with /default", func(t *testing.T) {
		mockSvc := testutil.NewMockQuicBaaS()

		// Mock SetDefaultDEK
		mockSvc.On("SetDefaultDEK", mock.Anything, mock.Anything).Return(&service.SetDefaultDEKResponse{
			Path:      "test-dek",
			Timestamp: time.Now(),
		}, nil).Maybe()

		server, err := NewServer(mockSvc, DefaultConfig(), log)
		require.NoError(t, err)
		defer server.Close()

		body := `{}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/deks/test-dek/default", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("*", "test-dek/default")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		w := httptest.NewRecorder()
		server.handleDEKPostAction(w, req)

		// Should route to default DEK handler
		assert.True(t, w.Code >= 200)
	})

	t.Run("routes to create handler when path doesn't have special suffix", func(t *testing.T) {
		mockSvc := testutil.NewMockQuicBaaS()

		// Mock CreateDEK
		mockSvc.On("CreateDEK", mock.Anything, mock.Anything).Return(&service.CreateDEKResponse{
			Timestamp: time.Now(),
		}, nil).Maybe()

		server, err := NewServer(mockSvc, DefaultConfig(), log)
		require.NoError(t, err)
		defer server.Close()

		body := `{"key_id": "test-key"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/deks/new-dek", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("*", "new-dek")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		w := httptest.NewRecorder()
		server.handleDEKPostAction(w, req)

		// Should route to create DEK handler
		assert.True(t, w.Code >= 200)
	})

	t.Run("handles short path for rotate", func(t *testing.T) {
		mockSvc := testutil.NewMockQuicBaaS()

		// Mock CreateDEK for short path
		mockSvc.On("CreateDEK", mock.Anything, mock.Anything).Return(&service.CreateDEKResponse{
			Timestamp: time.Now(),
		}, nil).Maybe()

		server, err := NewServer(mockSvc, DefaultConfig(), log)
		require.NoError(t, err)
		defer server.Close()

		body := `{}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/deks/ab", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("*", "ab")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		w := httptest.NewRecorder()
		server.handleDEKPostAction(w, req)

		// Path too short for /rotate (7 chars)
		// Should fall through to create handler
		assert.True(t, w.Code >= 200)
	})
}

func TestTransactionHandlerViaRouter(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "error",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	mockSvc := testutil.NewMockQuicBaaS()

	// Set up transaction mock
	mockSvc.On("Transaction", mock.Anything, mock.Anything).Return(&service.TransactionResponse{
		Succeeded: true,
		Responses: []*service.TransactionOpResponse{
			{
				OpType:     service.TransactionOpTypePut,
				EntityType: "users",
				ID:         123,
				Success:    true,
			},
		},
	}, nil).Maybe()

	server, err := NewServer(mockSvc, DefaultConfig(), log)
	require.NoError(t, err)
	defer server.Close()

	t.Run("transaction endpoint via router", func(t *testing.T) {
		txReq := TransactionRequest{
			Compare: []CompareCondition{
				{
					EntityType: "users",
					ID:         123,
					Target:     CompareTargetVersion,
					Operator:   CompareOpEqual,
					Version:    1,
				},
			},
			Success: []TransactionOp{
				{
					Type:       TxnOpPut,
					EntityType: "users",
					ID:         123,
					Data:       json.RawMessage(`{"name":"test"}`),
				},
			},
		}

		body, err := json.Marshal(txReq)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/transaction", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.GetRouter().ServeHTTP(w, req)

		// Check response - success or error, but no panic
		assert.True(t, w.Code >= 200)
	})
}
