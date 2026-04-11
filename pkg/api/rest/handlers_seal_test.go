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
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

func TestHandleSealStatus(t *testing.T) {
	tests := []struct {
		name           string
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
	}{
		{
			name: "get seal status successfully - sealed",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("Status", mock.Anything).Return(&service.SealStatusResponse{
					Sealed:      true,
					Initialized: true,
					Mode:        "manual",
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name: "get seal status successfully - unsealed",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("Status", mock.Anything).Return(&service.SealStatusResponse{
					Sealed:      false,
					Initialized: true,
					Mode:        "auto",
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name: "service error",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("Status", mock.Anything).Return(nil, &errors.QBError{
					Code: errors.ErrInternal,
					Op:   "SealStatus",
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

			req := httptest.NewRequest(http.MethodGet, "/api/v1/seal/status", nil)
			w := httptest.NewRecorder()

			server.HandleSealStatus(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			mockService.AssertExpectations(t)
		})
	}
}

func TestHandleSealInit(t *testing.T) {
	tests := []struct {
		name           string
		body           interface{}
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
	}{
		{
			name: "initialize seal successfully",
			body: map[string]interface{}{
				"threshold":    float64(3),
				"total_shares": float64(5),
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("Initialize", mock.Anything, mock.MatchedBy(func(req *service.SealInitRequest) bool {
					return req.Threshold == 3 && req.TotalShares == 5
				})).Return(&service.SealInitResponse{
					Shares:       [][]byte{[]byte("share1"), []byte("share2"), []byte("share3"), []byte("share4"), []byte("share5")},
					RecoveryKeys: [][]byte{[]byte("recovery-key")},
					RootToken:    "root-token-123",
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name: "zero threshold",
			body: map[string]interface{}{
				"threshold":    float64(0),
				"total_shares": float64(5),
			},
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "zero total shares",
			body: map[string]interface{}{
				"threshold":    float64(3),
				"total_shares": float64(0),
			},
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "threshold exceeds total shares",
			body: map[string]interface{}{
				"threshold":    float64(6),
				"total_shares": float64(5),
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
				"threshold":    float64(3),
				"total_shares": float64(5),
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("Initialize", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
					Code: errors.ErrInternal,
					Op:   "SealInitialize",
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

			req := httptest.NewRequest(http.MethodPost, "/api/v1/seal/init", bytes.NewReader(bodyBytes))
			w := httptest.NewRecorder()

			server.HandleSealInit(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			mockService.AssertExpectations(t)
		})
	}
}

func TestHandleUnseal(t *testing.T) {
	tests := []struct {
		name           string
		body           interface{}
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
	}{
		{
			name: "unseal successfully",
			body: map[string]interface{}{
				"share": []byte("key-share-123"),
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("Unseal", mock.Anything, mock.MatchedBy(func(req *service.UnsealRequest) bool {
					return len(req.Share) > 0
				})).Return(&service.UnsealResponse{
					Sealed:         false,
					SharesProvided: 3,
					Threshold:      3,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:           "missing key share",
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
				"share": []byte("key-share-123"),
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("Unseal", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
					Code: errors.ErrInternal,
					Op:   "Unseal",
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

			req := httptest.NewRequest(http.MethodPost, "/api/v1/seal/unseal", bytes.NewReader(bodyBytes))
			w := httptest.NewRecorder()

			server.HandleUnseal(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			mockService.AssertExpectations(t)
		})
	}
}

func TestHandleSeal(t *testing.T) {
	tests := []struct {
		name           string
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
	}{
		{
			name: "seal successfully",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("Seal", mock.Anything).Return(nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name: "service error",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("Seal", mock.Anything).Return(&errors.QBError{
					Code: errors.ErrInternal,
					Op:   "Seal",
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

			req := httptest.NewRequest(http.MethodPost, "/api/v1/seal/seal", nil)
			w := httptest.NewRecorder()

			server.HandleSeal(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			mockService.AssertExpectations(t)
		})
	}
}

func TestHandleRekey(t *testing.T) {
	tests := []struct {
		name           string
		body           interface{}
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
	}{
		{
			name: "rekey successfully",
			body: map[string]interface{}{
				"new_threshold":    float64(3),
				"new_total_shares": float64(5),
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("Rekey", mock.Anything, mock.MatchedBy(func(req *service.RekeyRequest) bool {
					return req.NewThreshold == 3 && req.NewTotalShares == 5
				})).Return(&service.RekeyResponse{
					Shares: [][]byte{[]byte("share1"), []byte("share2"), []byte("share3"), []byte("share4"), []byte("share5")},
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name: "zero new threshold",
			body: map[string]interface{}{
				"new_threshold":    float64(0),
				"new_total_shares": float64(5),
			},
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "zero new total shares",
			body: map[string]interface{}{
				"new_threshold":    float64(3),
				"new_total_shares": float64(0),
			},
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "threshold exceeds total shares",
			body: map[string]interface{}{
				"new_threshold":    float64(6),
				"new_total_shares": float64(5),
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
				"new_threshold":    float64(3),
				"new_total_shares": float64(5),
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("Rekey", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
					Code: errors.ErrInternal,
					Op:   "Rekey",
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

			req := httptest.NewRequest(http.MethodPost, "/api/v1/seal/rekey", bytes.NewReader(bodyBytes))
			w := httptest.NewRecorder()

			server.HandleRekey(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			mockService.AssertExpectations(t)
		})
	}
}

func TestHandleGenerateRecoveryKey(t *testing.T) {
	tests := []struct {
		name           string
		body           interface{}
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
	}{
		{
			name: "generate recovery key successfully",
			body: map[string]interface{}{
				"recovery_threshold":  float64(2),
				"recovery_total_keys": float64(3),
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("GenerateRecoveryKey", mock.Anything, mock.MatchedBy(func(req *service.GenerateRecoveryKeyRequest) bool {
					return req.RecoveryThreshold == 2 && req.RecoveryTotalKeys == 3
				})).Return(&service.GenerateRecoveryKeyResponse{
					RecoveryKeys: [][]byte{[]byte("rkey1"), []byte("rkey2"), []byte("rkey3")},
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name: "zero recovery threshold",
			body: map[string]interface{}{
				"recovery_threshold":  float64(0),
				"recovery_total_keys": float64(3),
			},
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "zero recovery total keys",
			body: map[string]interface{}{
				"recovery_threshold":  float64(2),
				"recovery_total_keys": float64(0),
			},
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "threshold exceeds total keys",
			body: map[string]interface{}{
				"recovery_threshold":  float64(4),
				"recovery_total_keys": float64(3),
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
				"recovery_threshold":  float64(2),
				"recovery_total_keys": float64(3),
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("GenerateRecoveryKey", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
					Code: errors.ErrInternal,
					Op:   "GenerateRecoveryKey",
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

			req := httptest.NewRequest(http.MethodPost, "/api/v1/seal/recovery/generate", bytes.NewReader(bodyBytes))
			w := httptest.NewRecorder()

			server.HandleGenerateRecoveryKey(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			mockService.AssertExpectations(t)
		})
	}
}

func TestHandleDeleteRecoveryKey(t *testing.T) {
	tests := []struct {
		name           string
		body           interface{}
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
	}{
		{
			name: "delete recovery key successfully",
			body: map[string]interface{}{
				"confirm": true,
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("DeleteRecoveryKey", mock.Anything, mock.MatchedBy(func(req *service.DeleteRecoveryKeyRequest) bool {
					return req.Confirm
				})).Return(&service.DeleteRecoveryKeyResponse{
					Deleted: true,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name: "confirm not set",
			body: map[string]interface{}{
				"confirm": false,
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
				"confirm": true,
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("DeleteRecoveryKey", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
					Code: errors.ErrInternal,
					Op:   "DeleteRecoveryKey",
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

			req := httptest.NewRequest(http.MethodDelete, "/api/v1/seal/recovery", bytes.NewReader(bodyBytes))
			w := httptest.NewRecorder()

			server.HandleDeleteRecoveryKey(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			mockService.AssertExpectations(t)
		})
	}
}

func TestHandleGenerateRootToken(t *testing.T) {
	tests := []struct {
		name           string
		body           interface{}
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
	}{
		{
			name: "generate root token successfully",
			body: map[string]interface{}{
				"strategy": "recovery",
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("GenerateRootToken", mock.Anything, mock.MatchedBy(func(req *service.GenerateRootTokenRequest) bool {
					return req.Strategy == "recovery"
				})).Return(&service.GenerateRootTokenResponse{
					RootToken: "root-token-123",
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:           "missing strategy",
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
				"strategy": "recovery",
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("GenerateRootToken", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
					Code: errors.ErrInternal,
					Op:   "GenerateRootToken",
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

			req := httptest.NewRequest(http.MethodPost, "/api/v1/seal/root/generate", bytes.NewReader(bodyBytes))
			w := httptest.NewRecorder()

			server.HandleGenerateRootToken(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			mockService.AssertExpectations(t)
		})
	}
}

// TestHandleSealStatusServiceError tests error handling
func TestHandleSealStatusServiceError(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("Status", mock.Anything).Return(nil, &errors.QBError{
		Code: errors.ErrInternal,
		Op:   "SealStatus",
		Err:  assert.AnError,
	}).Once()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/seal/status", nil)
	w := httptest.NewRecorder()

	server.HandleSealStatus(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	mockService.AssertExpectations(t)
}

// TestHandleSealError tests seal error handling
func TestHandleSealError(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("Seal", mock.Anything).Return(&errors.QBError{
		Code: errors.ErrInternal,
		Op:   "Seal",
		Err:  assert.AnError,
	}).Once()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/seal/seal", nil)
	w := httptest.NewRecorder()

	server.HandleSeal(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	mockService.AssertExpectations(t)
}
