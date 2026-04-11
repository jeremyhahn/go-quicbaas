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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

func TestRespondJSON(t *testing.T) {
	tests := []struct {
		name       string
		data       interface{}
		statusCode int
	}{
		{
			name: "simple object",
			data: map[string]string{
				"message": "hello",
			},
			statusCode: http.StatusOK,
		},
		{
			name: "complex object",
			data: PutResponse{
				ID:         123,
				EntityType: "users",
				Created:    true,
				Timestamp:  time.Now(),
			},
			statusCode: http.StatusCreated,
		},
		{
			name:       "nil data",
			data:       nil,
			statusCode: http.StatusNoContent,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			w := httptest.NewRecorder()

			RespondJSON(w, req, tt.statusCode, tt.data)

			assert.Equal(t, tt.statusCode, w.Code)
			assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

			if tt.data != nil {
				var result interface{}
				err := json.NewDecoder(w.Body).Decode(&result)
				require.NoError(t, err)
			}
		})
	}
}

func TestRespondError(t *testing.T) {
	tests := []struct {
		name          string
		err           error
		statusCode    int
		checkResponse func(*testing.T, *ErrorResponse)
	}{
		{
			name: "dragon error with context",
			err: &errors.QBError{
				Code: errors.ErrNotFound,
				Op:   "test.operation",
				Err:  &errors.MessageError{Message: "resource not found"},
				Context: map[string]interface{}{
					"resource_id": "123",
					"type":        "user",
				},
			},
			statusCode: http.StatusNotFound,
			checkResponse: func(t *testing.T, resp *ErrorResponse) {
				assert.NotEmpty(t, resp.Error)
				assert.Equal(t, "not_found", resp.Code)
				assert.NotNil(t, resp.Details)
				assert.Equal(t, "123", resp.Details["resource_id"])
			},
		},
		{
			name: "dragon error without context",
			err: &errors.QBError{
				Code: errors.ErrInternal,
				Op:   "test.operation",
				Err:  &errors.MessageError{Message: "internal error"},
			},
			statusCode: http.StatusInternalServerError,
			checkResponse: func(t *testing.T, resp *ErrorResponse) {
				assert.NotEmpty(t, resp.Error)
				assert.Equal(t, "internal", resp.Code)
			},
		},
		{
			name:       "generic error",
			err:        &errors.MessageError{Message: "generic error"},
			statusCode: http.StatusBadRequest,
			checkResponse: func(t *testing.T, resp *ErrorResponse) {
				assert.Equal(t, "generic error", resp.Error)
				assert.Empty(t, resp.Code)
				assert.False(t, resp.Retryable)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			req = req.WithContext(context.WithValue(req.Context(), requestIDKey, "test-123"))
			w := httptest.NewRecorder()

			RespondError(w, req, tt.statusCode, tt.err)

			assert.Equal(t, tt.statusCode, w.Code)
			assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

			var errResp ErrorResponse
			err := json.NewDecoder(w.Body).Decode(&errResp)
			require.NoError(t, err)

			assert.Equal(t, "test-123", errResp.RequestID)
			assert.NotZero(t, errResp.Timestamp)

			if tt.checkResponse != nil {
				tt.checkResponse(t, &errResp)
			}
		})
	}
}

func TestRespondSuccess(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()

	data := map[string]interface{}{
		"user_id": 123,
		"name":    "Alice",
	}

	RespondSuccess(w, req, "Operation successful", data)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var resp SuccessResponse
	err := json.NewDecoder(w.Body).Decode(&resp)
	require.NoError(t, err)

	assert.True(t, resp.Success)
	assert.Equal(t, "Operation successful", resp.Message)
	assert.NotNil(t, resp.Data)
	assert.NotZero(t, resp.Timestamp)
}

func TestRespondCreated(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	w := httptest.NewRecorder()

	data := map[string]interface{}{
		"id": 123,
	}

	RespondCreated(w, req, data)

	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var result map[string]interface{}
	err := json.NewDecoder(w.Body).Decode(&result)
	require.NoError(t, err)
	assert.Equal(t, float64(123), result["id"])
}

func TestRespondNoContent(t *testing.T) {
	req := httptest.NewRequest(http.MethodDelete, "/test", nil)
	w := httptest.NewRecorder()

	RespondNoContent(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.Empty(t, w.Body.String())
}

func TestRespondBadRequest(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req = req.WithContext(context.WithValue(req.Context(), requestIDKey, "test-123"))
	w := httptest.NewRecorder()

	RespondBadRequest(w, req, "Invalid input")

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var errResp ErrorResponse
	err := json.NewDecoder(w.Body).Decode(&errResp)
	require.NoError(t, err)
	assert.Contains(t, errResp.Error, "Invalid input")
}

func TestRespondNotFound(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req = req.WithContext(context.WithValue(req.Context(), requestIDKey, "test-123"))
	w := httptest.NewRecorder()

	RespondNotFound(w, req, "user/123")

	assert.Equal(t, http.StatusNotFound, w.Code)

	var errResp ErrorResponse
	err := json.NewDecoder(w.Body).Decode(&errResp)
	require.NoError(t, err)
	assert.Contains(t, errResp.Error, "user/123")
}

func TestRespondConflict(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	req = req.WithContext(context.WithValue(req.Context(), requestIDKey, "test-123"))
	w := httptest.NewRecorder()

	RespondConflict(w, req, "Resource already exists")

	assert.Equal(t, http.StatusConflict, w.Code)

	var errResp ErrorResponse
	err := json.NewDecoder(w.Body).Decode(&errResp)
	require.NoError(t, err)
	assert.Contains(t, errResp.Error, "already exists")
}

func TestRespondInternalError(t *testing.T) {
	t.Run("returns generic message to client", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req = req.WithContext(context.WithValue(req.Context(), requestIDKey, "test-123"))
		w := httptest.NewRecorder()

		RespondInternalError(w, req, &errors.MessageError{Message: "database connection failed"})

		assert.Equal(t, http.StatusInternalServerError, w.Code)

		var errResp ErrorResponse
		err := json.NewDecoder(w.Body).Decode(&errResp)
		require.NoError(t, err)

		// The response must NOT contain the actual error message
		assert.NotContains(t, errResp.Error, "database connection failed")
		// The response must contain a generic message
		assert.Equal(t, "internal server error", errResp.Error)
		// The request ID must be present for correlation
		assert.Equal(t, "test-123", errResp.RequestID)
	})

	t.Run("does not leak error details to client", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/sensitive", nil)
		req = req.WithContext(context.WithValue(req.Context(), requestIDKey, "req-456"))
		w := httptest.NewRecorder()

		RespondInternalError(w, req, &errors.MessageError{
			Message: "pebbledb: corruption at offset 0x4F in /var/data/shard-3.db",
		})

		assert.Equal(t, http.StatusInternalServerError, w.Code)

		var errResp ErrorResponse
		err := json.NewDecoder(w.Body).Decode(&errResp)
		require.NoError(t, err)

		// Must not leak storage internals
		assert.NotContains(t, errResp.Error, "pebbledb")
		assert.NotContains(t, errResp.Error, "/var/data")
		assert.Equal(t, "internal server error", errResp.Error)
		assert.Equal(t, "req-456", errResp.RequestID)
	})
}

func TestRespondServiceUnavailable(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req = req.WithContext(context.WithValue(req.Context(), requestIDKey, "test-123"))
	w := httptest.NewRecorder()

	RespondServiceUnavailable(w, req, "Service is temporarily unavailable")

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)

	var errResp ErrorResponse
	err := json.NewDecoder(w.Body).Decode(&errResp)
	require.NoError(t, err)
	assert.Contains(t, errResp.Error, "temporarily unavailable")
}

func TestConflictError(t *testing.T) {
	err := &ConflictError{
		Message: "resource conflict",
	}

	assert.Equal(t, "resource conflict", err.Error())
}

func BenchmarkRespondJSON(b *testing.B) {
	data := map[string]interface{}{
		"id":      123,
		"name":    "test",
		"active":  true,
		"created": time.Now(),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		w := httptest.NewRecorder()
		RespondJSON(w, req, http.StatusOK, data)
	}
}

func BenchmarkRespondError(b *testing.B) {
	err := &errors.QBError{
		Code: errors.ErrNotFound,
		Op:   "test",
		Err:  &errors.MessageError{Message: "not found"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req = req.WithContext(context.WithValue(req.Context(), requestIDKey, "test-123"))
		w := httptest.NewRecorder()
		RespondError(w, req, http.StatusNotFound, err)
	}
}
