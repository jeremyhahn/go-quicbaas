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
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
)

// =============================================================================
// HandleCryptoEncrypt Validation Tests
// =============================================================================

// TestCryptoEncryptValidations tests encrypt handler validation.
func TestCryptoEncryptValidations(t *testing.T) {
	tests := []struct {
		name    string
		keyName string
		body    string
		errMsg  string
	}{
		{
			name:    "missing key name",
			keyName: "",
			body:    `{"plaintext": "dGVzdA=="}`,
			errMsg:  "key name is required",
		},
		{
			name:    "invalid JSON",
			keyName: "test-key",
			body:    `not-json`,
			errMsg:  "",
		},
		{
			name:    "missing plaintext",
			keyName: "test-key",
			body:    `{}`,
			errMsg:  "plaintext is required",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server, _, cleanup := setupMockServer(t)
			defer cleanup()

			req := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/encrypt/"+tc.keyName, bytes.NewReader([]byte(tc.body)))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("name", tc.keyName)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			server.HandleCryptoEncrypt(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code)
			if tc.errMsg != "" {
				assert.Contains(t, w.Body.String(), tc.errMsg)
			}
		})
	}
}

// =============================================================================
// HandleCryptoDecrypt Validation Tests
// =============================================================================

// TestCryptoDecryptValidations tests decrypt handler validation.
func TestCryptoDecryptValidations(t *testing.T) {
	tests := []struct {
		name    string
		keyName string
		body    string
		errMsg  string
	}{
		{
			name:    "missing key name",
			keyName: "",
			body:    `{"ciphertext": "Y2lwaGVy"}`,
			errMsg:  "key name is required",
		},
		{
			name:    "invalid JSON",
			keyName: "test-key",
			body:    `not-json`,
			errMsg:  "",
		},
		{
			name:    "missing ciphertext",
			keyName: "test-key",
			body:    `{}`,
			errMsg:  "ciphertext is required",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server, _, cleanup := setupMockServer(t)
			defer cleanup()

			req := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/decrypt/"+tc.keyName, bytes.NewReader([]byte(tc.body)))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("name", tc.keyName)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			server.HandleCryptoDecrypt(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code)
			if tc.errMsg != "" {
				assert.Contains(t, w.Body.String(), tc.errMsg)
			}
		})
	}
}

// =============================================================================
// HandleCryptoRewrap Validation Tests
// =============================================================================

// TestCryptoRewrapValidations tests rewrap handler validation.
func TestCryptoRewrapValidations(t *testing.T) {
	tests := []struct {
		name    string
		keyName string
		body    string
		errMsg  string
	}{
		{
			name:    "missing key name",
			keyName: "",
			body:    `{"ciphertext": "Y2lwaGVy"}`,
			errMsg:  "key name is required",
		},
		{
			name:    "invalid JSON",
			keyName: "test-key",
			body:    `not-json`,
			errMsg:  "",
		},
		{
			name:    "missing ciphertext",
			keyName: "test-key",
			body:    `{}`,
			errMsg:  "ciphertext is required",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server, _, cleanup := setupMockServer(t)
			defer cleanup()

			req := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/rewrap/"+tc.keyName, bytes.NewReader([]byte(tc.body)))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("name", tc.keyName)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			server.HandleCryptoRewrap(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code)
			if tc.errMsg != "" {
				assert.Contains(t, w.Body.String(), tc.errMsg)
			}
		})
	}
}

// =============================================================================
// HandleCryptoSign Validation Tests
// =============================================================================

// TestCryptoSignValidations tests sign handler validation.
func TestCryptoSignValidations(t *testing.T) {
	tests := []struct {
		name    string
		keyName string
		body    string
		errMsg  string
	}{
		{
			name:    "missing key name",
			keyName: "",
			body:    `{"message": "dGVzdA=="}`,
			errMsg:  "key name is required",
		},
		{
			name:    "invalid JSON",
			keyName: "test-key",
			body:    `not-json`,
			errMsg:  "",
		},
		{
			name:    "missing message",
			keyName: "test-key",
			body:    `{}`,
			errMsg:  "message is required",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server, _, cleanup := setupMockServer(t)
			defer cleanup()

			req := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/sign/"+tc.keyName, bytes.NewReader([]byte(tc.body)))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("name", tc.keyName)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			server.HandleCryptoSign(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code)
			if tc.errMsg != "" {
				assert.Contains(t, w.Body.String(), tc.errMsg)
			}
		})
	}
}

// =============================================================================
// HandleCryptoVerify Validation Tests
// =============================================================================

// TestCryptoVerifyValidations tests verify handler validation.
func TestCryptoVerifyValidations(t *testing.T) {
	tests := []struct {
		name    string
		keyName string
		body    string
		errMsg  string
	}{
		{
			name:    "missing key name",
			keyName: "",
			body:    `{"message": "dGVzdA==", "signature": "c2ln"}`,
			errMsg:  "key name is required",
		},
		{
			name:    "invalid JSON",
			keyName: "test-key",
			body:    `not-json`,
			errMsg:  "",
		},
		{
			name:    "missing message",
			keyName: "test-key",
			body:    `{"signature": "c2ln"}`,
			errMsg:  "message is required",
		},
		{
			name:    "missing signature",
			keyName: "test-key",
			body:    `{"message": "dGVzdA=="}`,
			errMsg:  "signature is required",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server, _, cleanup := setupMockServer(t)
			defer cleanup()

			req := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/verify/"+tc.keyName, bytes.NewReader([]byte(tc.body)))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("name", tc.keyName)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			server.HandleCryptoVerify(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code)
			if tc.errMsg != "" {
				assert.Contains(t, w.Body.String(), tc.errMsg)
			}
		})
	}
}

// =============================================================================
// HandleCryptoHash Validation Tests
// =============================================================================

// TestCryptoHashValidations tests hash handler validation.
func TestCryptoHashValidations(t *testing.T) {
	tests := []struct {
		name   string
		body   string
		errMsg string
	}{
		{
			name:   "invalid JSON",
			body:   `not-json`,
			errMsg: "",
		},
		{
			name:   "missing data",
			body:   `{}`,
			errMsg: "data is required",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server, _, cleanup := setupMockServer(t)
			defer cleanup()

			req := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/hash", bytes.NewReader([]byte(tc.body)))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			server.HandleCryptoHash(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code)
			if tc.errMsg != "" {
				assert.Contains(t, w.Body.String(), tc.errMsg)
			}
		})
	}
}

// =============================================================================
// HandleCryptoRandom Validation Tests
// =============================================================================

// TestCryptoRandomInvalidJSON tests random handler with invalid JSON.
func TestCryptoRandomInvalidJSON(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	body := `not-json`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/random", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleCryptoRandom(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestCryptoRandomNumBytesTooLarge tests random handler with too large num_bytes.
func TestCryptoRandomNumBytesTooLarge(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	body := `{"num_bytes": 2000000}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/random", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleCryptoRandom(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "num_bytes must be between 1 and 1048576")
}

// =============================================================================
// HandleCryptoDataKey Validation Tests
// =============================================================================

// TestCryptoDataKeyValidations tests data key handler validation.
func TestCryptoDataKeyValidations(t *testing.T) {
	tests := []struct {
		name    string
		keyName string
		body    string
		errMsg  string
	}{
		{
			name:    "missing key name",
			keyName: "",
			body:    `{}`,
			errMsg:  "key name is required",
		},
		{
			name:    "invalid JSON",
			keyName: "test-key",
			body:    `not-json`,
			errMsg:  "",
		},
		{
			name:    "key_size_bits too large",
			keyName: "test-key",
			body:    `{"key_size_bits": 4096}`,
			errMsg:  "key_size_bits must be between 8 and 2048",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server, _, cleanup := setupMockServer(t)
			defer cleanup()

			req := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/datakey/"+tc.keyName, bytes.NewReader([]byte(tc.body)))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("name", tc.keyName)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			server.HandleCryptoDataKey(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code)
			if tc.errMsg != "" {
				assert.Contains(t, w.Body.String(), tc.errMsg)
			}
		})
	}
}

// =============================================================================
// HandleCryptoBatchEncrypt Validation Tests
// =============================================================================

// TestCryptoBatchEncryptValidations tests batch encrypt handler validation.
func TestCryptoBatchEncryptValidations(t *testing.T) {
	tests := []struct {
		name    string
		keyName string
		body    string
		errMsg  string
	}{
		{
			name:    "missing key name",
			keyName: "",
			body:    `{"items": [{"plaintext": "dGVzdA=="}]}`,
			errMsg:  "key name is required",
		},
		{
			name:    "invalid JSON",
			keyName: "test-key",
			body:    `not-json`,
			errMsg:  "",
		},
		{
			name:    "missing items",
			keyName: "test-key",
			body:    `{}`,
			errMsg:  "items are required",
		},
		{
			name:    "empty items",
			keyName: "test-key",
			body:    `{"items": []}`,
			errMsg:  "items are required",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server, _, cleanup := setupMockServer(t)
			defer cleanup()

			req := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/encrypt/"+tc.keyName+"/batch", bytes.NewReader([]byte(tc.body)))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("name", tc.keyName)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			server.HandleCryptoBatchEncrypt(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code)
			if tc.errMsg != "" {
				assert.Contains(t, w.Body.String(), tc.errMsg)
			}
		})
	}
}

// =============================================================================
// HandleCryptoBatchDecrypt Validation Tests
// =============================================================================

// TestCryptoBatchDecryptValidations tests batch decrypt handler validation.
func TestCryptoBatchDecryptValidations(t *testing.T) {
	tests := []struct {
		name    string
		keyName string
		body    string
		errMsg  string
	}{
		{
			name:    "missing key name",
			keyName: "",
			body:    `{"items": [{"ciphertext": "Y2lwaGVy"}]}`,
			errMsg:  "key name is required",
		},
		{
			name:    "invalid JSON",
			keyName: "test-key",
			body:    `not-json`,
			errMsg:  "",
		},
		{
			name:    "missing items",
			keyName: "test-key",
			body:    `{}`,
			errMsg:  "items are required",
		},
		{
			name:    "empty items",
			keyName: "test-key",
			body:    `{"items": []}`,
			errMsg:  "items are required",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server, _, cleanup := setupMockServer(t)
			defer cleanup()

			req := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/decrypt/"+tc.keyName+"/batch", bytes.NewReader([]byte(tc.body)))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("name", tc.keyName)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			server.HandleCryptoBatchDecrypt(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code)
			if tc.errMsg != "" {
				assert.Contains(t, w.Body.String(), tc.errMsg)
			}
		})
	}
}
