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
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

func TestHandleCryptoEncrypt(t *testing.T) {
	tests := []struct {
		name           string
		keyName        string
		body           interface{}
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
	}{
		{
			name:    "encrypt successfully",
			keyName: "test-key",
			body: map[string]interface{}{
				"plaintext": []byte("secret-data"),
				"context":   []byte("additional-context"),
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("Encrypt", mock.Anything, mock.MatchedBy(func(req *service.EncryptRequest) bool {
					return req.KeyID == "test-key" && len(req.Plaintext) > 0
				})).Return(&service.EncryptResponse{
					Ciphertext: []byte("encrypted-data"),
					KeyVersion: 1,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:           "missing key name",
			keyName:        "",
			body:           map[string]interface{}{"plaintext": []byte("secret-data")},
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "missing plaintext",
			keyName:        "test-key",
			body:           map[string]interface{}{},
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "invalid JSON body",
			keyName:        "test-key",
			body:           "invalid-json",
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:    "service error",
			keyName: "test-key",
			body: map[string]interface{}{
				"plaintext": []byte("secret-data"),
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("Encrypt", mock.Anything, mock.Anything).Return(nil, &errors.DragonError{
					Code: errors.ErrInternal,
					Op:   "Encrypt",
					Err:  assert.AnError,
				}).Once()
			},
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockService, cleanup := setupMockServer(t)
			defer cleanup()
			tt.setupMock(mockService)

			var bodyBytes []byte
			if str, ok := tt.body.(string); ok {
				bodyBytes = []byte(str)
			} else {
				bodyBytes, _ = json.Marshal(tt.body)
			}

			req := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/encrypt/"+tt.keyName, bytes.NewReader(bodyBytes))
			w := httptest.NewRecorder()

			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("name", tt.keyName)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			server.HandleCryptoEncrypt(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			mockService.AssertExpectations(t)
		})
	}
}

func TestHandleCryptoDecrypt(t *testing.T) {
	tests := []struct {
		name           string
		keyName        string
		body           interface{}
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
	}{
		{
			name:    "decrypt successfully",
			keyName: "test-key",
			body: map[string]interface{}{
				"ciphertext": []byte("encrypted-data"),
				"context":    []byte("additional-context"),
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("Decrypt", mock.Anything, mock.MatchedBy(func(req *service.DecryptRequest) bool {
					return req.KeyID == "test-key" && len(req.Ciphertext) > 0
				})).Return(&service.DecryptResponse{
					Plaintext:  []byte("secret-data"),
					KeyVersion: 1,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:           "missing key name",
			keyName:        "",
			body:           map[string]interface{}{"ciphertext": []byte("encrypted-data")},
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "missing ciphertext",
			keyName:        "test-key",
			body:           map[string]interface{}{},
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "invalid JSON body",
			keyName:        "test-key",
			body:           "invalid-json",
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:    "service error",
			keyName: "test-key",
			body: map[string]interface{}{
				"ciphertext": []byte("encrypted-data"),
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("Decrypt", mock.Anything, mock.Anything).Return(nil, &errors.DragonError{
					Code: errors.ErrInternal,
					Op:   "Decrypt",
					Err:  assert.AnError,
				}).Once()
			},
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockService, cleanup := setupMockServer(t)
			defer cleanup()
			tt.setupMock(mockService)

			var bodyBytes []byte
			if str, ok := tt.body.(string); ok {
				bodyBytes = []byte(str)
			} else {
				bodyBytes, _ = json.Marshal(tt.body)
			}

			req := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/decrypt/"+tt.keyName, bytes.NewReader(bodyBytes))
			w := httptest.NewRecorder()

			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("name", tt.keyName)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			server.HandleCryptoDecrypt(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			mockService.AssertExpectations(t)
		})
	}
}

func TestHandleCryptoRewrap(t *testing.T) {
	tests := []struct {
		name           string
		keyName        string
		body           interface{}
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
	}{
		{
			name:    "rewrap successfully",
			keyName: "test-key",
			body: map[string]interface{}{
				"ciphertext": []byte("encrypted-data"),
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("Rewrap", mock.Anything, mock.MatchedBy(func(req *service.RewrapRequest) bool {
					return req.KeyID == "test-key" && len(req.Ciphertext) > 0
				})).Return(&service.RewrapResponse{
					Ciphertext:    []byte("re-encrypted-data"),
					OldKeyVersion: 1,
					NewKeyVersion: 2,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:           "missing key name",
			keyName:        "",
			body:           map[string]interface{}{"ciphertext": []byte("encrypted-data")},
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "missing ciphertext",
			keyName:        "test-key",
			body:           map[string]interface{}{},
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "invalid JSON body",
			keyName:        "test-key",
			body:           "invalid-json",
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:    "service error",
			keyName: "test-key",
			body: map[string]interface{}{
				"ciphertext": []byte("encrypted-data"),
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("Rewrap", mock.Anything, mock.Anything).Return(nil, &errors.DragonError{
					Code: errors.ErrInternal,
					Op:   "Rewrap",
					Err:  assert.AnError,
				}).Once()
			},
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockService, cleanup := setupMockServer(t)
			defer cleanup()
			tt.setupMock(mockService)

			var bodyBytes []byte
			if str, ok := tt.body.(string); ok {
				bodyBytes = []byte(str)
			} else {
				bodyBytes, _ = json.Marshal(tt.body)
			}

			req := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/rewrap/"+tt.keyName, bytes.NewReader(bodyBytes))
			w := httptest.NewRecorder()

			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("name", tt.keyName)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			server.HandleCryptoRewrap(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			mockService.AssertExpectations(t)
		})
	}
}

func TestHandleCryptoSign(t *testing.T) {
	tests := []struct {
		name           string
		keyName        string
		body           interface{}
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
	}{
		{
			name:    "sign successfully",
			keyName: "test-key",
			body: map[string]interface{}{
				"message":        []byte("message-to-sign"),
				"hash_algorithm": "SHA256",
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("Sign", mock.Anything, mock.MatchedBy(func(req *service.SignRequest) bool {
					return req.KeyID == "test-key" && len(req.Message) > 0
				})).Return(&service.SignResponse{
					Signature:  []byte("signature-data"),
					KeyVersion: 1,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:           "missing key name",
			keyName:        "",
			body:           map[string]interface{}{"message": []byte("message-to-sign")},
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "missing message",
			keyName:        "test-key",
			body:           map[string]interface{}{},
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "invalid JSON body",
			keyName:        "test-key",
			body:           "invalid-json",
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:    "service error",
			keyName: "test-key",
			body: map[string]interface{}{
				"message": []byte("message-to-sign"),
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("Sign", mock.Anything, mock.Anything).Return(nil, &errors.DragonError{
					Code: errors.ErrInternal,
					Op:   "Sign",
					Err:  assert.AnError,
				}).Once()
			},
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockService, cleanup := setupMockServer(t)
			defer cleanup()
			tt.setupMock(mockService)

			var bodyBytes []byte
			if str, ok := tt.body.(string); ok {
				bodyBytes = []byte(str)
			} else {
				bodyBytes, _ = json.Marshal(tt.body)
			}

			req := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/sign/"+tt.keyName, bytes.NewReader(bodyBytes))
			w := httptest.NewRecorder()

			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("name", tt.keyName)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			server.HandleCryptoSign(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			mockService.AssertExpectations(t)
		})
	}
}

func TestHandleCryptoVerify(t *testing.T) {
	tests := []struct {
		name           string
		keyName        string
		body           interface{}
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
	}{
		{
			name:    "verify successfully",
			keyName: "test-key",
			body: map[string]interface{}{
				"message":   []byte("message-to-verify"),
				"signature": []byte("signature-data"),
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("Verify", mock.Anything, mock.MatchedBy(func(req *service.VerifyRequest) bool {
					return req.KeyID == "test-key" && len(req.Message) > 0 && len(req.Signature) > 0
				})).Return(&service.VerifyResponse{
					Valid: true,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:           "missing key name",
			keyName:        "",
			body:           map[string]interface{}{"message": []byte("message-to-verify"), "signature": []byte("signature-data")},
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:    "missing message",
			keyName: "test-key",
			body: map[string]interface{}{
				"signature": []byte("signature-data"),
			},
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:    "missing signature",
			keyName: "test-key",
			body: map[string]interface{}{
				"message": []byte("message-to-verify"),
			},
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "invalid JSON body",
			keyName:        "test-key",
			body:           "invalid-json",
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:    "service error",
			keyName: "test-key",
			body: map[string]interface{}{
				"message":   []byte("message-to-verify"),
				"signature": []byte("signature-data"),
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("Verify", mock.Anything, mock.Anything).Return(nil, &errors.DragonError{
					Code: errors.ErrInternal,
					Op:   "Verify",
					Err:  assert.AnError,
				}).Once()
			},
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockService, cleanup := setupMockServer(t)
			defer cleanup()
			tt.setupMock(mockService)

			var bodyBytes []byte
			if str, ok := tt.body.(string); ok {
				bodyBytes = []byte(str)
			} else {
				bodyBytes, _ = json.Marshal(tt.body)
			}

			req := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/verify/"+tt.keyName, bytes.NewReader(bodyBytes))
			w := httptest.NewRecorder()

			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("name", tt.keyName)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			server.HandleCryptoVerify(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			mockService.AssertExpectations(t)
		})
	}
}

func TestHandleCryptoHash(t *testing.T) {
	tests := []struct {
		name           string
		body           interface{}
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
	}{
		{
			name: "hash successfully",
			body: map[string]interface{}{
				"data":      []byte("data-to-hash"),
				"algorithm": "SHA256",
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("Hash", mock.Anything, mock.MatchedBy(func(req *service.HashRequest) bool {
					return len(req.Data) > 0
				})).Return(&service.HashResponse{
					Hash: []byte("hash-result"),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name: "hash with default algorithm",
			body: map[string]interface{}{
				"data": []byte("data-to-hash"),
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("Hash", mock.Anything, mock.MatchedBy(func(req *service.HashRequest) bool {
					return len(req.Data) > 0 && req.Algorithm == service.HashAlgSHA256
				})).Return(&service.HashResponse{
					Hash: []byte("hash-result"),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:           "missing data",
			body:           map[string]interface{}{},
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "invalid JSON body",
			body:           "invalid-json",
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "service error",
			body: map[string]interface{}{
				"data": []byte("data-to-hash"),
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("Hash", mock.Anything, mock.Anything).Return(nil, &errors.DragonError{
					Code: errors.ErrInternal,
					Op:   "Hash",
					Err:  assert.AnError,
				}).Once()
			},
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockService, cleanup := setupMockServer(t)
			defer cleanup()
			tt.setupMock(mockService)

			var bodyBytes []byte
			if str, ok := tt.body.(string); ok {
				bodyBytes = []byte(str)
			} else {
				bodyBytes, _ = json.Marshal(tt.body)
			}

			req := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/hash", bytes.NewReader(bodyBytes))
			w := httptest.NewRecorder()

			server.HandleCryptoHash(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			mockService.AssertExpectations(t)
		})
	}
}

func TestHandleCryptoRandom(t *testing.T) {
	tests := []struct {
		name           string
		body           interface{}
		queryParams    map[string]string
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
	}{
		{
			name: "generate random with body",
			body: map[string]interface{}{
				"num_bytes": float64(64),
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("GenerateRandom", mock.Anything, mock.MatchedBy(func(req *service.GenerateRandomRequest) bool {
					return req.NumBytes == 64
				})).Return(&service.GenerateRandomResponse{
					RandomBytes: make([]byte, 64),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name: "generate random with default",
			body: map[string]interface{}{},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("GenerateRandom", mock.Anything, mock.MatchedBy(func(req *service.GenerateRandomRequest) bool {
					return req.NumBytes == 32
				})).Return(&service.GenerateRandomResponse{
					RandomBytes: make([]byte, 32),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name: "generate random with query param",
			queryParams: map[string]string{
				"bytes": "128",
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("GenerateRandom", mock.Anything, mock.MatchedBy(func(req *service.GenerateRandomRequest) bool {
					return req.NumBytes == 128
				})).Return(&service.GenerateRandomResponse{
					RandomBytes: make([]byte, 128),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name: "num_bytes too large",
			body: map[string]interface{}{
				"num_bytes": float64(2 * 1024 * 1024),
			},
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "invalid JSON body",
			body:           "invalid-json",
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "service error",
			body: map[string]interface{}{
				"num_bytes": float64(32),
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("GenerateRandom", mock.Anything, mock.Anything).Return(nil, &errors.DragonError{
					Code: errors.ErrInternal,
					Op:   "GenerateRandom",
					Err:  assert.AnError,
				}).Once()
			},
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockService, cleanup := setupMockServer(t)
			defer cleanup()
			tt.setupMock(mockService)

			var bodyBytes []byte
			if tt.body != nil {
				if str, ok := tt.body.(string); ok {
					bodyBytes = []byte(str)
				} else {
					bodyBytes, _ = json.Marshal(tt.body)
				}
			}

			url := "/api/v1/crypto/random"
			if len(tt.queryParams) > 0 {
				url += "?"
				for k, v := range tt.queryParams {
					url += k + "=" + v + "&"
				}
			}

			var req *http.Request
			if len(bodyBytes) > 0 {
				req = httptest.NewRequest(http.MethodPost, url, bytes.NewReader(bodyBytes))
			} else {
				req = httptest.NewRequest(http.MethodPost, url, nil)
			}
			w := httptest.NewRecorder()

			server.HandleCryptoRandom(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			mockService.AssertExpectations(t)
		})
	}
}

func TestHandleCryptoDataKey(t *testing.T) {
	tests := []struct {
		name           string
		keyName        string
		body           interface{}
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
	}{
		{
			name:    "generate data key successfully",
			keyName: "test-key",
			body: map[string]interface{}{
				"key_spec": map[string]interface{}{
					"algorithm": "AES256-GCM",
				},
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				mockKMSMgr := &MockKMSManager{}
				mockKMSMgr.On("GenerateDataKey", mock.Anything, mock.MatchedBy(func(attrs *xkmssdk.KeyAttributes) bool {
					return attrs.CN == "test-key"
				}), 32).Return([]byte("plaintext-key"), []byte("wrapped-key"), nil).Once()
				m.On("KMSManager").Return(mockKMSMgr)
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:           "missing key name",
			keyName:        "",
			body:           map[string]interface{}{},
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "invalid JSON body",
			keyName:        "test-key",
			body:           "invalid-json",
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:    "service error",
			keyName: "test-key",
			body:    map[string]interface{}{},
			setupMock: func(m *testutil.MockQuicBaaS) {
				mockKMSMgr := &MockKMSManager{}
				mockKMSMgr.On("GenerateDataKey", mock.Anything, mock.Anything, mock.Anything).Return(nil, nil, &errors.DragonError{
					Code: errors.ErrInternal,
					Op:   "GenerateDataKey",
					Err:  assert.AnError,
				}).Once()
				m.On("KMSManager").Return(mockKMSMgr)
			},
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockService, cleanup := setupMockServer(t)
			defer cleanup()
			tt.setupMock(mockService)

			var bodyBytes []byte
			if str, ok := tt.body.(string); ok {
				bodyBytes = []byte(str)
			} else {
				bodyBytes, _ = json.Marshal(tt.body)
			}

			req := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/datakey/"+tt.keyName, bytes.NewReader(bodyBytes))
			w := httptest.NewRecorder()

			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("name", tt.keyName)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			server.HandleCryptoDataKey(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
		})
	}
}

func TestHandleCryptoBatchEncrypt(t *testing.T) {
	tests := []struct {
		name           string
		keyName        string
		body           interface{}
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
	}{
		{
			name:    "batch encrypt successfully",
			keyName: "test-key",
			body: map[string]interface{}{
				"items": []map[string]interface{}{
					{"plaintext": []byte("data1")},
					{"plaintext": []byte("data2")},
				},
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("EncryptBatch", mock.Anything, mock.MatchedBy(func(req *service.EncryptBatchRequest) bool {
					return req.KeyID == "test-key" && len(req.Items) > 0
				})).Return(&service.EncryptBatchResponse{
					Results: []service.BatchEncryptResult{
						{Ciphertext: []byte("encrypted1")},
						{Ciphertext: []byte("encrypted2")},
					},
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:           "missing key name",
			keyName:        "",
			body:           map[string]interface{}{"items": []map[string]interface{}{}},
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "missing items",
			keyName:        "test-key",
			body:           map[string]interface{}{},
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "invalid JSON body",
			keyName:        "test-key",
			body:           "invalid-json",
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:    "service error",
			keyName: "test-key",
			body: map[string]interface{}{
				"items": []map[string]interface{}{
					{"plaintext": []byte("data1")},
				},
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("EncryptBatch", mock.Anything, mock.Anything).Return(nil, &errors.DragonError{
					Code: errors.ErrInternal,
					Op:   "EncryptBatch",
					Err:  assert.AnError,
				}).Once()
			},
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockService, cleanup := setupMockServer(t)
			defer cleanup()
			tt.setupMock(mockService)

			var bodyBytes []byte
			if str, ok := tt.body.(string); ok {
				bodyBytes = []byte(str)
			} else {
				bodyBytes, _ = json.Marshal(tt.body)
			}

			req := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/encrypt/"+tt.keyName+"/batch", bytes.NewReader(bodyBytes))
			w := httptest.NewRecorder()

			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("name", tt.keyName)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			server.HandleCryptoBatchEncrypt(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			mockService.AssertExpectations(t)
		})
	}
}

func TestHandleCryptoBatchDecrypt(t *testing.T) {
	tests := []struct {
		name           string
		keyName        string
		body           interface{}
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
	}{
		{
			name:    "batch decrypt successfully",
			keyName: "test-key",
			body: map[string]interface{}{
				"items": []map[string]interface{}{
					{"ciphertext": []byte("encrypted1")},
					{"ciphertext": []byte("encrypted2")},
				},
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("DecryptBatch", mock.Anything, mock.MatchedBy(func(req *service.DecryptBatchRequest) bool {
					return req.KeyID == "test-key" && len(req.Items) > 0
				})).Return(&service.DecryptBatchResponse{
					Results: []service.BatchDecryptResult{
						{Plaintext: []byte("data1")},
						{Plaintext: []byte("data2")},
					},
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:           "missing key name",
			keyName:        "",
			body:           map[string]interface{}{"items": []map[string]interface{}{}},
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "missing items",
			keyName:        "test-key",
			body:           map[string]interface{}{},
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "invalid JSON body",
			keyName:        "test-key",
			body:           "invalid-json",
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:    "service error",
			keyName: "test-key",
			body: map[string]interface{}{
				"items": []map[string]interface{}{
					{"ciphertext": []byte("encrypted1")},
				},
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("DecryptBatch", mock.Anything, mock.Anything).Return(nil, &errors.DragonError{
					Code: errors.ErrInternal,
					Op:   "DecryptBatch",
					Err:  assert.AnError,
				}).Once()
			},
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockService, cleanup := setupMockServer(t)
			defer cleanup()
			tt.setupMock(mockService)

			var bodyBytes []byte
			if str, ok := tt.body.(string); ok {
				bodyBytes = []byte(str)
			} else {
				bodyBytes, _ = json.Marshal(tt.body)
			}

			req := httptest.NewRequest(http.MethodPost, "/api/v1/crypto/decrypt/"+tt.keyName+"/batch", bytes.NewReader(bodyBytes))
			w := httptest.NewRecorder()

			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("name", tt.keyName)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			server.HandleCryptoBatchDecrypt(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			mockService.AssertExpectations(t)
		})
	}
}
