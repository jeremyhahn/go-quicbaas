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
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBodyLimitMiddleware_AllowsRequestWithinLimit(t *testing.T) {
	const limit int64 = 1024 // 1 KB

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		w.WriteHeader(http.StatusOK)
		_, writeErr := w.Write(body)
		require.NoError(t, writeErr)
	})

	middleware := BodyLimitMiddleware(limit)
	wrapped := middleware(handler)

	// Send a body within the limit
	payload := strings.Repeat("a", 512)
	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(payload))
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, payload, w.Body.String())
}

func TestBodyLimitMiddleware_RejectsOversizedRequest(t *testing.T) {
	const limit int64 = 64 // 64 bytes

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := io.ReadAll(r.Body)
		if err != nil {
			if IsMaxBytesError(err) {
				RespondPayloadTooLarge(w, r, limit)
				return
			}
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	middleware := BodyLimitMiddleware(limit)
	wrapped := middleware(handler)

	// Send a body exceeding the limit
	payload := strings.Repeat("x", 128)
	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(payload))
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, req)

	assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code)

	// Verify the error response is valid JSON with the expected message
	var errResp ErrorResponse
	decodeErr := json.NewDecoder(w.Body).Decode(&errResp)
	require.NoError(t, decodeErr)
	assert.Contains(t, errResp.Error, "request body too large")
	assert.Contains(t, errResp.Error, "64")
}

func TestBodyLimitMiddleware_PassthroughWhenDisabled(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		w.WriteHeader(http.StatusOK)
		_, writeErr := w.Write(body)
		require.NoError(t, writeErr)
	})

	// Limit <= 0 disables body limiting
	for _, limit := range []int64{0, -1, -100} {
		middleware := BodyLimitMiddleware(limit)
		wrapped := middleware(handler)

		payload := strings.Repeat("z", 10000)
		req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(payload))
		w := httptest.NewRecorder()

		wrapped.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, payload, w.Body.String())
	}
}

func TestBodyLimitMiddleware_SkipsNilBody(t *testing.T) {
	handlerCalled := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	})

	middleware := BodyLimitMiddleware(64)
	wrapped := middleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, req)

	assert.True(t, handlerCalled)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestBodyLimitMiddleware_SkipsNoBody(t *testing.T) {
	handlerCalled := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	})

	middleware := BodyLimitMiddleware(64)
	wrapped := middleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, req)

	assert.True(t, handlerCalled)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestBodyLimitMiddleware_ExactLimitAllowed(t *testing.T) {
	const limit int64 = 100

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		w.WriteHeader(http.StatusOK)
		_, writeErr := w.Write(body)
		require.NoError(t, writeErr)
	})

	middleware := BodyLimitMiddleware(limit)
	wrapped := middleware(handler)

	// Body exactly at the limit should be allowed
	payload := strings.Repeat("b", int(limit))
	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(payload))
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, payload, w.Body.String())
}

func TestBodyLimitMiddleware_OneBytePastLimitRejected(t *testing.T) {
	const limit int64 = 100

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := io.ReadAll(r.Body)
		if err != nil {
			if IsMaxBytesError(err) {
				RespondPayloadTooLarge(w, r, limit)
				return
			}
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	middleware := BodyLimitMiddleware(limit)
	wrapped := middleware(handler)

	// One byte past the limit should be rejected
	payload := strings.Repeat("c", int(limit)+1)
	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(payload))
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, req)

	assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
}

func TestBodyLimitMiddleware_JSONDecoderDetectsOversize(t *testing.T) {
	const limit int64 = 50

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var data map[string]interface{}
		err := json.NewDecoder(r.Body).Decode(&data)
		if err != nil {
			if IsMaxBytesError(err) {
				RespondPayloadTooLarge(w, r, limit)
				return
			}
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	middleware := BodyLimitMiddleware(limit)
	wrapped := middleware(handler)

	// Create a JSON payload exceeding the limit
	largeJSON := `{"key":"` + strings.Repeat("v", 100) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(largeJSON))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, req)

	assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
}

func TestIsMaxBytesError_TrueForMaxBytesError(t *testing.T) {
	// Trigger an actual MaxBytesError by reading from a limited reader
	body := strings.NewReader(strings.Repeat("x", 100))
	limited := http.MaxBytesReader(httptest.NewRecorder(), io.NopCloser(body), 10)
	_, err := io.ReadAll(limited)
	require.Error(t, err)
	assert.True(t, IsMaxBytesError(err))
}

func TestIsMaxBytesError_FalseForOtherErrors(t *testing.T) {
	assert.False(t, IsMaxBytesError(io.EOF))
	assert.False(t, IsMaxBytesError(io.ErrUnexpectedEOF))
	assert.False(t, IsMaxBytesError(&ValidationError{Message: "test"}))
}

func TestPayloadTooLargeError_ErrorMessage(t *testing.T) {
	err := &PayloadTooLargeError{MaxBytes: 10485760}
	assert.Equal(t, "request body too large: maximum size is 10485760 bytes", err.Error())
}

func TestPayloadTooLargeError_ZeroBytes(t *testing.T) {
	err := &PayloadTooLargeError{MaxBytes: 0}
	assert.Equal(t, "request body too large: maximum size is 0 bytes", err.Error())
}

func TestRespondPayloadTooLarge(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	w := httptest.NewRecorder()

	RespondPayloadTooLarge(w, req, DefaultMaxBodySize)

	assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code)

	var errResp ErrorResponse
	decodeErr := json.NewDecoder(w.Body).Decode(&errResp)
	require.NoError(t, decodeErr)
	assert.Contains(t, errResp.Error, "request body too large")
	assert.Contains(t, errResp.Error, "10485760")
	assert.False(t, errResp.Retryable)
}

func TestDefaultMaxBodySize_Constants(t *testing.T) {
	assert.Equal(t, int64(10<<20), DefaultMaxBodySize)
	assert.Equal(t, int64(100<<20), DefaultMaxUploadBodySize)
}

func TestDefaultConfig_IncludesBodyLimits(t *testing.T) {
	cfg := DefaultConfig()
	assert.Equal(t, DefaultMaxBodySize, cfg.MaxBodySize)
	assert.Equal(t, DefaultMaxUploadBodySize, cfg.MaxUploadBodySize)
}

func TestBodyLimitMiddleware_DifferentHTTPMethods(t *testing.T) {
	const limit int64 = 64

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := io.ReadAll(r.Body)
		if err != nil {
			if IsMaxBytesError(err) {
				RespondPayloadTooLarge(w, r, limit)
				return
			}
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	middleware := BodyLimitMiddleware(limit)
	wrapped := middleware(handler)

	oversized := strings.Repeat("d", 128)

	methods := []string{
		http.MethodPost,
		http.MethodPut,
		http.MethodPatch,
	}

	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/test", strings.NewReader(oversized))
			w := httptest.NewRecorder()
			wrapped.ServeHTTP(w, req)
			assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
		})
	}
}

func TestBodyLimitMiddleware_EmptyBody(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		assert.Empty(t, body)
		w.WriteHeader(http.StatusOK)
	})

	middleware := BodyLimitMiddleware(64)
	wrapped := middleware(handler)

	req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewReader(nil))
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}
