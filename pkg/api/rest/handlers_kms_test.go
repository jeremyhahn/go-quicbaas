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
	"crypto"
	"crypto/elliptic"
	"crypto/x509"
	"encoding/json"
	stderrors "errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/kms"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// MockKMSManager is a mock implementation of interfaces.KMSManager for testing.
type MockKMSManager struct {
	mock.Mock
}

func (m *MockKMSManager) GenerateKey(ctx context.Context, attrs *xkmssdk.KeyAttributes) (interface{}, error) {
	args := m.Called(ctx, attrs)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0), args.Error(1)
}

func (m *MockKMSManager) ImportKey(ctx context.Context, keyID string, material []byte, algorithm string) (interface{}, error) {
	args := m.Called(ctx, keyID, material, algorithm)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0), args.Error(1)
}

func (m *MockKMSManager) GetKey(ctx context.Context, keyID string) (interface{}, error) {
	args := m.Called(ctx, keyID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0), args.Error(1)
}

func (m *MockKMSManager) UpdateStoredMetadata(metadata interface{}) error {
	args := m.Called(metadata)
	return args.Error(0)
}

func (m *MockKMSManager) ListKeys(ctx context.Context, prefix string) ([]interface{}, error) {
	args := m.Called(ctx, prefix)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]interface{}), args.Error(1)
}

func (m *MockKMSManager) RotateKey(ctx context.Context, keyID string) (interface{}, error) {
	args := m.Called(ctx, keyID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0), args.Error(1)
}

func (m *MockKMSManager) RevokeKey(ctx context.Context, keyID string) error {
	args := m.Called(ctx, keyID)
	return args.Error(0)
}

func (m *MockKMSManager) EnableKey(ctx context.Context, keyID string) error {
	args := m.Called(ctx, keyID)
	return args.Error(0)
}

func (m *MockKMSManager) DisableKey(ctx context.Context, keyID string) error {
	args := m.Called(ctx, keyID)
	return args.Error(0)
}

func (m *MockKMSManager) DeleteKey(ctx context.Context, keyID string) error {
	args := m.Called(ctx, keyID)
	return args.Error(0)
}

func (m *MockKMSManager) Encrypt(ctx context.Context, keyID string, plaintext, aad []byte) ([]byte, error) {
	args := m.Called(ctx, keyID, plaintext, aad)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]byte), args.Error(1)
}

func (m *MockKMSManager) Decrypt(ctx context.Context, keyID string, ciphertext, aad []byte) ([]byte, error) {
	args := m.Called(ctx, keyID, ciphertext, aad)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]byte), args.Error(1)
}

func (m *MockKMSManager) Sign(ctx context.Context, keyID string, digest []byte, algorithm string) ([]byte, error) {
	args := m.Called(ctx, keyID, digest, algorithm)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]byte), args.Error(1)
}

func (m *MockKMSManager) Verify(ctx context.Context, keyID string, digest, signature []byte, algorithm string) (bool, error) {
	args := m.Called(ctx, keyID, digest, signature, algorithm)
	return args.Bool(0), args.Error(1)
}

func (m *MockKMSManager) GenerateDataKey(ctx context.Context, attrs *xkmssdk.KeyAttributes, keyBytes int) ([]byte, []byte, error) {
	args := m.Called(ctx, attrs, keyBytes)
	if args.Get(0) == nil {
		return nil, nil, args.Error(2)
	}
	return args.Get(0).([]byte), args.Get(1).([]byte), args.Error(2)
}

func (m *MockKMSManager) GetBackendType() config.BackendType {
	args := m.Called()
	return args.Get(0).(config.BackendType)
}

func (m *MockKMSManager) IsAvailable(ctx context.Context) bool {
	args := m.Called(ctx)
	return args.Bool(0)
}

func (m *MockKMSManager) GetSupportedAlgorithms(ctx context.Context) ([]string, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]string), args.Error(1)
}

func (m *MockKMSManager) GetPublicKey(ctx context.Context, keyID string) ([]byte, error) {
	args := m.Called(ctx, keyID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]byte), args.Error(1)
}

func (m *MockKMSManager) Close() error {
	args := m.Called()
	return args.Error(0)
}

// MockCryptoService is a mock implementation of service.CryptoService for testing.
type MockCryptoService struct {
	mock.Mock
}

func (m *MockCryptoService) Encrypt(ctx context.Context, req *service.EncryptRequest) (*service.EncryptResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.EncryptResponse), args.Error(1)
}

func (m *MockCryptoService) Decrypt(ctx context.Context, req *service.DecryptRequest) (*service.DecryptResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.DecryptResponse), args.Error(1)
}

func (m *MockCryptoService) Rewrap(ctx context.Context, req *service.RewrapRequest) (*service.RewrapResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.RewrapResponse), args.Error(1)
}

func (m *MockCryptoService) Sign(ctx context.Context, req *service.SignRequest) (*service.SignResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.SignResponse), args.Error(1)
}

func (m *MockCryptoService) Verify(ctx context.Context, req *service.VerifyRequest) (*service.VerifyResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.VerifyResponse), args.Error(1)
}

func (m *MockCryptoService) GenerateHMAC(ctx context.Context, req *service.HMACRequest) (*service.HMACResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.HMACResponse), args.Error(1)
}

func (m *MockCryptoService) VerifyHMAC(ctx context.Context, req *service.VerifyHMACRequest) (*service.VerifyHMACResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.VerifyHMACResponse), args.Error(1)
}

func (m *MockCryptoService) Hash(ctx context.Context, req *service.HashRequest) (*service.HashResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.HashResponse), args.Error(1)
}

func (m *MockCryptoService) GenerateRandom(ctx context.Context, req *service.GenerateRandomRequest) (*service.GenerateRandomResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.GenerateRandomResponse), args.Error(1)
}

func (m *MockCryptoService) GenerateDataKey(ctx context.Context, req *service.GenerateDataKeyRequest) (*service.GenerateDataKeyResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.GenerateDataKeyResponse), args.Error(1)
}

func (m *MockCryptoService) GenerateWrappedDataKey(ctx context.Context, req *service.GenerateWrappedDataKeyRequest) (*service.GenerateWrappedDataKeyResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.GenerateWrappedDataKeyResponse), args.Error(1)
}

func (m *MockCryptoService) EncryptBatch(ctx context.Context, req *service.EncryptBatchRequest) (*service.EncryptBatchResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.EncryptBatchResponse), args.Error(1)
}

func (m *MockCryptoService) DecryptBatch(ctx context.Context, req *service.DecryptBatchRequest) (*service.DecryptBatchResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.DecryptBatchResponse), args.Error(1)
}

func TestHandleGenerateKey(t *testing.T) {
	tests := []struct {
		name           string
		body           interface{}
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
	}{
		{
			name: "generate key successfully via KMS manager",
			body: map[string]interface{}{
				"key_attributes": map[string]interface{}{
					"algorithm": "AES256-GCM",
					"key_usage": []string{"encrypt", "decrypt"},
				},
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				mockKMSMgr := &MockKMSManager{}
				mockKMSMgr.On("GenerateKey", mock.Anything, mock.MatchedBy(func(attrs *xkmssdk.KeyAttributes) bool {
					return attrs != nil && attrs.CN != ""
				})).Return(&kms.KeyMetadata{
					KeyID:     "test-key-id",
					Algorithm: "AES256-GCM",
					Version:   1,
					State:     kms.KeyStateEnabled,
					Created:   time.Now(),
					Updated:   time.Now(),
				}, nil).Once()
				m.DisableKMS = true
				m.On("KMSManager").Return(mockKMSMgr)
			},
			expectedStatus: http.StatusCreated,
		},
		{
			name: "generate key threads exportable and deletion_protection",
			body: map[string]interface{}{
				"name": "protected-key",
				"key_attributes": map[string]interface{}{
					"algorithm":           "AES-256-GCM",
					"key_usage":           []string{"encrypt", "decrypt"},
					"exportable":          true,
					"deletion_protection": true,
				},
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				now := time.Now()
				m.On("GenerateKey", mock.Anything, mock.MatchedBy(func(req *service.GenerateKeyRequest) bool {
					return req.Name == "protected-key" && req.Exportable && req.DeletionProtection
				})).Return(&service.GenerateKeyResponse{
					Key: &service.KeyHandle{
						KeyID:     "protected-key",
						Version:   1,
						Algorithm: service.KeyAlgorithm("aes256-gcm"),
						Status:    service.KeyStatusEnabled,
						CreatedAt: now,
					},
					Metadata: &service.KeyMetadata{
						KeyID:              "protected-key",
						Name:               "protected-key",
						Algorithm:          service.KeyAlgorithm("aes256-gcm"),
						Status:             service.KeyStatusEnabled,
						Usage:              []service.KeyUsage{"encrypt", "decrypt"},
						Version:            1,
						Exportable:         true,
						DeletionProtection: true,
						CreatedAt:          now,
						UpdatedAt:          now,
					},
					Timestamp: now,
				}, nil).Once()
			},
			expectedStatus: http.StatusCreated,
		},
		{
			name: "generate key via KMS service layer",
			body: map[string]interface{}{
				"name":        "my-service-key",
				"description": "A test key via service layer",
				"key_attributes": map[string]interface{}{
					"algorithm": "AES-256-GCM",
					"key_usage": []string{"encrypt", "decrypt"},
				},
				"tags":  map[string]string{"env": "test"},
				"owner": "test-user",
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				now := time.Now()
				m.On("GenerateKey", mock.Anything, mock.MatchedBy(func(req *service.GenerateKeyRequest) bool {
					return req.Name == "my-service-key" && req.Owner == "test-user"
				})).Return(&service.GenerateKeyResponse{
					Key: &service.KeyHandle{
						KeyID:     "my-service-key",
						Version:   1,
						Algorithm: service.KeyAlgorithm("aes256-gcm"),
						Status:    service.KeyStatusEnabled,
						CreatedAt: now,
					},
					Metadata: &service.KeyMetadata{
						KeyID:       "my-service-key",
						Name:        "my-service-key",
						Description: "A test key via service layer",
						Algorithm:   service.KeyAlgorithm("aes256-gcm"),
						Status:      service.KeyStatusEnabled,
						Usage:       []service.KeyUsage{"encrypt", "decrypt"},
						Version:     1,
						CreatedAt:   now,
						UpdatedAt:   now,
						Tags:        map[string]string{"env": "test"},
						Owner:       "test-user",
					},
					Timestamp: now,
				}, nil).Once()
			},
			expectedStatus: http.StatusCreated,
		},
		{
			name: "generate key via KMS service layer - service error",
			body: map[string]interface{}{
				"key_attributes": map[string]interface{}{
					"algorithm": "AES-256-GCM",
					"key_usage": []string{"encrypt"},
				},
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("GenerateKey", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
					Code: errors.ErrInternal,
					Op:   "GenerateKey",
					Err:  stderrors.New("internal failure"),
				}).Once()
			},
			expectedStatus: http.StatusInternalServerError,
		},
		{
			name: "missing algorithm",
			body: map[string]interface{}{
				"key_attributes": map[string]interface{}{
					"key_usage": []string{"encrypt"},
				},
			},
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "missing usage",
			body: map[string]interface{}{
				"key_attributes": map[string]interface{}{
					"algorithm": "AES256-GCM",
				},
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
			name: "service error via KMS manager",
			body: map[string]interface{}{
				"key_attributes": map[string]interface{}{
					"algorithm": "AES256-GCM",
					"key_usage": []string{"encrypt"},
				},
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				mockKMSMgr := &MockKMSManager{}
				mockKMSMgr.On("GenerateKey", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
					Code: errors.ErrInternal,
					Op:   "GenerateKey",
					Err:  assert.AnError,
				}).Once()
				m.DisableKMS = true
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

			req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/keys", bytes.NewReader(bodyBytes))
			w := httptest.NewRecorder()

			server.HandleGenerateKey(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
		})
	}
}

func TestHandleImportKey(t *testing.T) {
	tests := []struct {
		name           string
		body           interface{}
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
	}{
		{
			name: "import key successfully",
			body: map[string]interface{}{
				"key_material":    []byte("encrypted-key-material"),
				"wrapping_key_id": "wrapping-key-123",
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				mockKMSMgr := &MockKMSManager{}
				mockKMSMgr.On("ImportKey", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(&kms.KeyMetadata{
					KeyID:     "imported-key-id",
					Algorithm: "AES-256-GCM",
					Version:   1,
					State:     kms.KeyStateEnabled,
					Created:   time.Now(),
					Updated:   time.Now(),
				}, nil).Once()
				m.DisableKMS = true
				m.On("KMSManager").Return(mockKMSMgr)
			},
			expectedStatus: http.StatusCreated,
		},
		{
			name: "missing key material",
			body: map[string]interface{}{
				"wrapping_key_id": "wrapping-key-123",
			},
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "missing wrapping key id",
			body: map[string]interface{}{
				"key_material": []byte("encrypted-key-material"),
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
				"key_material":    []byte("encrypted-key-material"),
				"wrapping_key_id": "wrapping-key-123",
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				mockKMSMgr := &MockKMSManager{}
				mockKMSMgr.On("ImportKey", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil, &errors.QBError{
					Code: errors.ErrInternal,
					Op:   "ImportKey",
					Err:  assert.AnError,
				}).Once()
				m.DisableKMS = true
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

			req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/keys/import", bytes.NewReader(bodyBytes))
			w := httptest.NewRecorder()

			server.HandleImportKey(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
		})
	}
}

func TestHandleGetKey(t *testing.T) {
	tests := []struct {
		name           string
		keyName        string
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
	}{
		{
			name:    "get key successfully via KMS manager",
			keyName: "test-key-123",
			setupMock: func(m *testutil.MockQuicBaaS) {
				mockKMSMgr := &MockKMSManager{}
				mockKMSMgr.On("GetKey", mock.Anything, "test-key-123").Return(&kms.KeyMetadata{
					KeyID:     "test-key-123",
					Algorithm: "AES256-GCM",
					Version:   1,
					State:     kms.KeyStateEnabled,
					Created:   time.Now(),
					Updated:   time.Now(),
				}, nil).Once()
				m.DisableKMS = true
				m.On("KMSManager").Return(mockKMSMgr)
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:    "get key via KMS service layer",
			keyName: "svc-key-456",
			setupMock: func(m *testutil.MockQuicBaaS) {
				now := time.Now()
				lastUsed := now.Add(-1 * time.Hour)
				m.On("GetKey", mock.Anything, mock.MatchedBy(func(req *service.GetKeyRequest) bool {
					return req.KeyID == "svc-key-456"
				})).Return(&service.GetKeyResponse{
					Key: &service.KeyHandle{
						KeyID:     "svc-key-456",
						Version:   2,
						Algorithm: service.KeyAlgorithm("aes256-gcm"),
						Status:    service.KeyStatusEnabled,
						CreatedAt: now,
					},
					Metadata: &service.KeyMetadata{
						KeyID:       "svc-key-456",
						Name:        "My Key",
						Description: "A service key",
						Algorithm:   service.KeyAlgorithm("aes256-gcm"),
						Status:      service.KeyStatusEnabled,
						Usage:       []service.KeyUsage{"encrypt", "decrypt"},
						Version:     2,
						Exportable:  true,
						CreatedAt:   now,
						UpdatedAt:   now,
						LastUsedAt:  &lastUsed,
						Tags:        map[string]string{"env": "prod"},
						Owner:       "admin",
					},
					Timestamp: now,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:    "get key via KMS service layer - not found error",
			keyName: "missing-key",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("GetKey", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
					Code: errors.ErrNotFound,
					Op:   "GetKey",
					Err:  stderrors.New("key not found"),
				}).Once()
			},
			expectedStatus: http.StatusNotFound,
		},
		{
			name:           "missing key name",
			keyName:        "",
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:    "service error via KMS manager",
			keyName: "test-key-123",
			setupMock: func(m *testutil.MockQuicBaaS) {
				mockKMSMgr := &MockKMSManager{}
				mockKMSMgr.On("GetKey", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
					Code: errors.ErrNotFound,
					Op:   "GetKey",
					Err:  assert.AnError,
				}).Once()
				m.DisableKMS = true
				m.On("KMSManager").Return(mockKMSMgr)
			},
			expectedStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockService, cleanup := setupMockServer(t)
			defer cleanup()
			tt.setupMock(mockService)

			req := httptest.NewRequest(http.MethodGet, "/api/v1/kms/keys/"+tt.keyName, nil)
			w := httptest.NewRecorder()

			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("name", tt.keyName)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			server.HandleGetKey(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
		})
	}
}

func TestHandleListKeys(t *testing.T) {
	tests := []struct {
		name           string
		queryParams    map[string]string
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
	}{
		{
			name: "list keys successfully via KMS manager",
			setupMock: func(m *testutil.MockQuicBaaS) {
				mockKMSMgr := &MockKMSManager{}
				mockKMSMgr.On("ListKeys", mock.Anything, "").Return([]interface{}{
					&kms.KeyMetadata{KeyID: "key-1", Algorithm: "AES-256-GCM", Created: time.Now(), Updated: time.Now()},
					&kms.KeyMetadata{KeyID: "key-2", Algorithm: "AES-256-GCM", Created: time.Now(), Updated: time.Now()},
				}, nil).Once()
				m.DisableKMS = true
				m.On("KMSManager").Return(mockKMSMgr)
			},
			expectedStatus: http.StatusOK,
		},
		{
			name: "list keys via KMS service layer",
			setupMock: func(m *testutil.MockQuicBaaS) {
				now := time.Now()
				m.On("ListKeys", mock.Anything, mock.Anything).Return(&service.ListKeysResponse{
					Keys: []*service.KeyHandle{
						{KeyID: "key-a", Version: 1, Algorithm: service.KeyAlgorithm("aes256-gcm"), Status: service.KeyStatusEnabled, CreatedAt: now},
						{KeyID: "key-b", Version: 2, Algorithm: service.KeyAlgorithm("aes256-gcm"), Status: service.KeyStatusEnabled, CreatedAt: now},
					},
					NextCursor: "next-token",
					HasMore:    true,
					TotalCount: 5,
					Timestamp:  now,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name: "list keys via KMS service layer with prefix filter",
			queryParams: map[string]string{
				"prefix": "my-prefix/",
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				now := time.Now()
				m.On("ListKeys", mock.Anything, mock.MatchedBy(func(req *service.ListKeysRequest) bool {
					return req.Filter["prefix"] == "my-prefix/"
				})).Return(&service.ListKeysResponse{
					Keys:      []*service.KeyHandle{},
					Timestamp: now,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name: "list keys via KMS service layer - error",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("ListKeys", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
					Code: errors.ErrInternal,
					Op:   "ListKeys",
					Err:  stderrors.New("internal error"),
				}).Once()
			},
			expectedStatus: http.StatusInternalServerError,
		},
		{
			name: "list with prefix via KMS manager",
			queryParams: map[string]string{
				"prefix": "my-keys/",
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				mockKMSMgr := &MockKMSManager{}
				mockKMSMgr.On("ListKeys", mock.Anything, "my-keys/").Return([]interface{}{}, nil).Once()
				m.DisableKMS = true
				m.On("KMSManager").Return(mockKMSMgr)
			},
			expectedStatus: http.StatusOK,
		},
		{
			name: "service error via KMS manager",
			setupMock: func(m *testutil.MockQuicBaaS) {
				mockKMSMgr := &MockKMSManager{}
				mockKMSMgr.On("ListKeys", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
					Code: errors.ErrInternal,
					Op:   "ListKeys",
					Err:  assert.AnError,
				}).Once()
				m.DisableKMS = true
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

			url := "/api/v1/kms/keys"
			if len(tt.queryParams) > 0 {
				url += "?"
				for k, v := range tt.queryParams {
					url += k + "=" + v + "&"
				}
			}

			req := httptest.NewRequest(http.MethodGet, url, nil)
			w := httptest.NewRecorder()

			server.HandleListKeys(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
		})
	}
}

func TestHandleRotateKey(t *testing.T) {
	tests := []struct {
		name           string
		keyName        string
		body           interface{}
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
	}{
		{
			name:    "rotate key successfully via KMS manager",
			keyName: "test-key-123",
			body:    map[string]interface{}{},
			setupMock: func(m *testutil.MockQuicBaaS) {
				mockKMSMgr := &MockKMSManager{}
				mockKMSMgr.On("GetKey", mock.Anything, "test-key-123").Return(&kms.KeyMetadata{
					KeyID:   "test-key-123",
					Version: 1,
					Created: time.Now(),
					Updated: time.Now(),
				}, nil).Once()
				mockKMSMgr.On("RotateKey", mock.Anything, "test-key-123").Return(&kms.KeyMetadata{
					KeyID:   "test-key-123",
					Version: 2,
					Created: time.Now(),
					Updated: time.Now(),
				}, nil).Once()
				m.DisableKMS = true
				m.On("KMSManager").Return(mockKMSMgr)
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:    "rotate key via KMS service layer",
			keyName: "svc-rotate-key",
			body:    map[string]interface{}{},
			setupMock: func(m *testutil.MockQuicBaaS) {
				now := time.Now()
				m.On("RotateKey", mock.Anything, mock.MatchedBy(func(req *service.RotateKeyRequest) bool {
					return req.KeyID == "svc-rotate-key"
				})).Return(&service.RotateKeyResponse{
					KeyID:      "svc-rotate-key",
					OldVersion: 1,
					NewVersion: 2,
					Timestamp:  now,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:    "rotate key via KMS service layer - error",
			keyName: "fail-rotate",
			body:    map[string]interface{}{},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("RotateKey", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
					Code: errors.ErrNotFound,
					Op:   "RotateKey",
					Err:  stderrors.New("key not found"),
				}).Once()
			},
			expectedStatus: http.StatusNotFound,
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
			keyName:        "test-key-123",
			body:           "invalid-json",
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:    "service error on rotate via KMS manager",
			keyName: "test-key-123",
			body:    map[string]interface{}{},
			setupMock: func(m *testutil.MockQuicBaaS) {
				mockKMSMgr := &MockKMSManager{}
				mockKMSMgr.On("GetKey", mock.Anything, "test-key-123").Return(&kms.KeyMetadata{
					KeyID:   "test-key-123",
					Version: 1,
					Created: time.Now(),
					Updated: time.Now(),
				}, nil).Once()
				mockKMSMgr.On("RotateKey", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
					Code: errors.ErrInternal,
					Op:   "RotateKey",
					Err:  assert.AnError,
				}).Once()
				m.DisableKMS = true
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

			req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/keys/"+tt.keyName+"/rotate", bytes.NewReader(bodyBytes))
			w := httptest.NewRecorder()

			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("name", tt.keyName)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			server.HandleRotateKey(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
		})
	}
}

func TestHandleDeleteKey(t *testing.T) {
	tests := []struct {
		name           string
		keyName        string
		body           interface{}
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
	}{
		{
			name:    "delete key successfully via KMS manager",
			keyName: "test-key-123",
			body:    map[string]interface{}{},
			setupMock: func(m *testutil.MockQuicBaaS) {
				mockKMSMgr := &MockKMSManager{}
				mockKMSMgr.On("DisableKey", mock.Anything, "test-key-123").Return(nil).Once()
				mockKMSMgr.On("DeleteKey", mock.Anything, "test-key-123").Return(nil).Once()
				m.DisableKMS = true
				m.On("KMSManager").Return(mockKMSMgr)
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:    "delete key with force via KMS manager",
			keyName: "test-key-123",
			body:    map[string]interface{}{"force": true},
			setupMock: func(m *testutil.MockQuicBaaS) {
				mockKMSMgr := &MockKMSManager{}
				mockKMSMgr.On("DeleteKey", mock.Anything, "test-key-123").Return(nil).Once()
				m.DisableKMS = true
				m.On("KMSManager").Return(mockKMSMgr)
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:    "delete key via KMS service layer",
			keyName: "svc-del-key",
			body:    map[string]interface{}{"force": true, "pending_window_days": 14},
			setupMock: func(m *testutil.MockQuicBaaS) {
				now := time.Now()
				m.On("DeleteKey", mock.Anything, mock.MatchedBy(func(req *service.DeleteKeyRequest) bool {
					return req.KeyID == "svc-del-key" && req.Force && req.PendingWindowDays == 14
				})).Return(&service.DeleteKeyResponse{
					KeyID:        "svc-del-key",
					Status:       service.KeyStatusPendingDeletion,
					DeletionDate: now.AddDate(0, 0, 14),
					Timestamp:    now,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:    "delete key via KMS service layer - error",
			keyName: "protected-key",
			body:    map[string]interface{}{},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("DeleteKey", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
					Code: errors.ErrForbidden,
					Op:   "DeleteKey",
					Err:  stderrors.New("deletion protection enabled"),
				}).Once()
			},
			expectedStatus: http.StatusForbidden,
		},
		{
			// Regression: deleting a key that does not exist (e.g. already
			// deleted by a prior fixture cleanup) must map to 404, not 500.
			// The service layer stamps ErrNotFound; the handler must preserve
			// it through handleKMSError.
			name:    "delete missing key via KMS service layer returns 404",
			keyName: "already-deleted-key",
			body:    map[string]interface{}{},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("DeleteKey", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
					Code: errors.ErrNotFound,
					Op:   "kms.DeleteKey",
					Err:  kms.ErrKeyNotFound,
				}).Once()
			},
			expectedStatus: http.StatusNotFound,
		},
		{
			// Regression: the manager fallback path must also map a missing
			// key to 404. The KMS manager reports not-found as a QBError with
			// ErrNotFound wrapping the kms.ErrKeyNotFound sentinel.
			name:    "delete missing key via KMS manager returns 404",
			keyName: "already-deleted-key",
			body:    map[string]interface{}{"force": true},
			setupMock: func(m *testutil.MockQuicBaaS) {
				mockKMSMgr := &MockKMSManager{}
				mockKMSMgr.On("DeleteKey", mock.Anything, mock.Anything).Return(&errors.QBError{
					Code: errors.ErrNotFound,
					Op:   "KMSService.DeleteKey",
					Err:  kms.ErrKeyNotFound,
				}).Once()
				m.DisableKMS = true
				m.On("KMSManager").Return(mockKMSMgr)
			},
			expectedStatus: http.StatusNotFound,
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
			keyName:        "test-key-123",
			body:           "invalid-json",
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:    "service error via KMS manager",
			keyName: "test-key-123",
			body:    map[string]interface{}{"force": true},
			setupMock: func(m *testutil.MockQuicBaaS) {
				mockKMSMgr := &MockKMSManager{}
				mockKMSMgr.On("DeleteKey", mock.Anything, mock.Anything).Return(&errors.QBError{
					Code: errors.ErrInternal,
					Op:   "DeleteKey",
					Err:  assert.AnError,
				}).Once()
				m.DisableKMS = true
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

			req := httptest.NewRequest(http.MethodDelete, "/api/v1/kms/keys/"+tt.keyName, bytes.NewReader(bodyBytes))
			w := httptest.NewRecorder()

			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("name", tt.keyName)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			server.HandleDeleteKey(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
		})
	}
}

func TestHandleKMSEncrypt(t *testing.T) {
	tests := []struct {
		name           string
		keyName        string
		body           interface{}
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
	}{
		{
			name:    "encrypt successfully",
			keyName: "test-key-123",
			body: map[string]interface{}{
				"plaintext": []byte("secret-data"),
				"context":   []byte("additional-context"),
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				mockCrypto := &MockCryptoService{}
				mockCrypto.On("Encrypt", mock.Anything, mock.MatchedBy(func(req *service.EncryptRequest) bool {
					return req.KeyID == "test-key-123" && len(req.Plaintext) > 0
				})).Return(&service.EncryptResponse{
					Ciphertext: []byte("encrypted-data"),
					KeyVersion: 1,
				}, nil).Once()
				m.CryptoService = mockCrypto
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
			keyName:        "test-key-123",
			body:           map[string]interface{}{},
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "invalid JSON body",
			keyName:        "test-key-123",
			body:           "invalid-json",
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:    "service error",
			keyName: "test-key-123",
			body: map[string]interface{}{
				"plaintext": []byte("secret-data"),
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				mockCrypto := &MockCryptoService{}
				mockCrypto.On("Encrypt", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
					Code: errors.ErrInternal,
					Op:   "Encrypt",
					Err:  assert.AnError,
				}).Once()
				m.CryptoService = mockCrypto
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

			req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/encrypt/"+tt.keyName, bytes.NewReader(bodyBytes))
			w := httptest.NewRecorder()

			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("name", tt.keyName)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			server.HandleKMSEncrypt(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
		})
	}
}

func TestHandleKMSDecrypt(t *testing.T) {
	tests := []struct {
		name           string
		keyName        string
		body           interface{}
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
	}{
		{
			name:    "decrypt successfully",
			keyName: "test-key-123",
			body: map[string]interface{}{
				"ciphertext": []byte("encrypted-data"),
				"context":    []byte("additional-context"),
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				mockCrypto := &MockCryptoService{}
				mockCrypto.On("Decrypt", mock.Anything, mock.MatchedBy(func(req *service.DecryptRequest) bool {
					return req.KeyID == "test-key-123" && len(req.Ciphertext) > 0
				})).Return(&service.DecryptResponse{
					Plaintext:  []byte("secret-data"),
					KeyVersion: 1,
				}, nil).Once()
				m.CryptoService = mockCrypto
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
			keyName:        "test-key-123",
			body:           map[string]interface{}{},
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "invalid JSON body",
			keyName:        "test-key-123",
			body:           "invalid-json",
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:    "service error",
			keyName: "test-key-123",
			body: map[string]interface{}{
				"ciphertext": []byte("encrypted-data"),
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				mockCrypto := &MockCryptoService{}
				mockCrypto.On("Decrypt", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
					Code: errors.ErrInternal,
					Op:   "Decrypt",
					Err:  assert.AnError,
				}).Once()
				m.CryptoService = mockCrypto
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

			req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/decrypt/"+tt.keyName, bytes.NewReader(bodyBytes))
			w := httptest.NewRecorder()

			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("name", tt.keyName)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			server.HandleKMSDecrypt(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
		})
	}
}

func TestHandleUpdateKeyStatus(t *testing.T) {
	tests := []struct {
		name           string
		keyName        string
		body           interface{}
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
	}{
		{
			name:    "enable key successfully",
			keyName: "test-key-123",
			body:    map[string]interface{}{"status": "enabled"},
			setupMock: func(m *testutil.MockQuicBaaS) {
				mockKMSMgr := &MockKMSManager{}
				mockKMSMgr.On("EnableKey", mock.Anything, "test-key-123").Return(nil).Once()
				m.DisableKMS = true
				m.On("KMSManager").Return(mockKMSMgr)
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:    "disable key successfully",
			keyName: "test-key-123",
			body:    map[string]interface{}{"status": "disabled"},
			setupMock: func(m *testutil.MockQuicBaaS) {
				mockKMSMgr := &MockKMSManager{}
				mockKMSMgr.On("DisableKey", mock.Anything, "test-key-123").Return(nil).Once()
				m.DisableKMS = true
				m.On("KMSManager").Return(mockKMSMgr)
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:           "missing key name",
			keyName:        "",
			body:           map[string]interface{}{"status": "enabled"},
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "missing status",
			keyName:        "test-key-123",
			body:           map[string]interface{}{},
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "invalid status value",
			keyName:        "test-key-123",
			body:           map[string]interface{}{"status": "invalid"},
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
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

			req := httptest.NewRequest(http.MethodPut, "/api/v1/kms/keys/"+tt.keyName+"/status", bytes.NewReader(bodyBytes))
			w := httptest.NewRecorder()

			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("name", tt.keyName)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			server.HandleUpdateKeyStatus(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
		})
	}
}

func TestHandleListKeyVersions(t *testing.T) {
	tests := []struct {
		name           string
		keyName        string
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
	}{
		{
			name:    "list versions successfully",
			keyName: "test-key-123",
			setupMock: func(m *testutil.MockQuicBaaS) {
				mockKMSMgr := &MockKMSManager{}
				mockKMSMgr.On("GetKey", mock.Anything, "test-key-123").Return(&kms.KeyMetadata{
					KeyID:     "test-key-123",
					Algorithm: "AES-256-GCM",
					Version:   3,
					State:     kms.KeyStateEnabled,
					Created:   time.Now(),
					Updated:   time.Now(),
				}, nil).Once()
				m.DisableKMS = true
				m.On("KMSManager").Return(mockKMSMgr)
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:           "missing key name",
			keyName:        "",
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:    "key not found",
			keyName: "nonexistent-key",
			setupMock: func(m *testutil.MockQuicBaaS) {
				mockKMSMgr := &MockKMSManager{}
				mockKMSMgr.On("GetKey", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
					Code: errors.ErrNotFound,
					Op:   "GetKey",
					Err:  assert.AnError,
				}).Once()
				m.DisableKMS = true
				m.On("KMSManager").Return(mockKMSMgr)
			},
			expectedStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockService, cleanup := setupMockServer(t)
			defer cleanup()
			tt.setupMock(mockService)

			req := httptest.NewRequest(http.MethodGet, "/api/v1/kms/keys/"+tt.keyName+"/versions", nil)
			w := httptest.NewRecorder()

			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("name", tt.keyName)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			server.HandleListKeyVersions(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
		})
	}
}

func TestKeyAttributesRequestToXKMS(t *testing.T) {
	tests := []struct {
		name        string
		keyID       string
		req         *KeyAttributesRequest
		expectError bool
		validate    func(*testing.T, *xkmssdk.KeyAttributes)
	}{
		{
			name:  "AES-256-GCM key",
			keyID: "test-key",
			req: &KeyAttributesRequest{
				Algorithm:  "AES-256-GCM",
				KeyUsage:   []string{"encrypt", "decrypt"},
				Exportable: true,
			},
			validate: func(t *testing.T, attrs *xkmssdk.KeyAttributes) {
				assert.Equal(t, "test-key", attrs.CN)
				assert.Equal(t, xkmssdk.SymmetricAES256GCM, attrs.SymmetricAlgorithm)
				assert.Equal(t, xkmssdk.KeyTypeEncryption, attrs.KeyType)
				assert.True(t, attrs.Exportable)
			},
		},
		{
			name:  "RSA-4096 key",
			keyID: "rsa-key",
			req: &KeyAttributesRequest{
				Algorithm: "RSA-4096",
				KeyUsage:  []string{"sign", "verify"},
			},
			validate: func(t *testing.T, attrs *xkmssdk.KeyAttributes) {
				assert.Equal(t, "rsa-key", attrs.CN)
				assert.Equal(t, xkmssdk.KeyTypeSigning, attrs.KeyType)
				assert.NotNil(t, attrs.RSAAttributes)
				assert.Equal(t, 4096, attrs.RSAAttributes.KeySize)
			},
		},
		{
			name:  "Ed25519 key",
			keyID: "ed25519-key",
			req: &KeyAttributesRequest{
				Algorithm: "ED25519",
				KeyUsage:  []string{"sign"},
			},
			validate: func(t *testing.T, attrs *xkmssdk.KeyAttributes) {
				assert.Equal(t, "ed25519-key", attrs.CN)
				assert.Equal(t, xkmssdk.KeyTypeSigning, attrs.KeyType)
			},
		},
		{
			name:  "with metadata fields that are not set in KeyAttributes",
			keyID: "metadata-key",
			req: &KeyAttributesRequest{
				Algorithm:      "AES-256-GCM",
				KeyUsage:       []string{"encrypt"},
				AutoRotate:     true,
				RotationPeriod: "720h",
				ExpiresAt:      "2030-01-01T00:00:00Z",
				Labels:         map[string]string{"env": "prod"},
			},
			validate: func(t *testing.T, attrs *xkmssdk.KeyAttributes) {
				assert.Equal(t, "metadata-key", attrs.CN)
				assert.Equal(t, xkmssdk.SymmetricAES256GCM, attrs.SymmetricAlgorithm)
				assert.Equal(t, xkmssdk.KeyTypeEncryption, attrs.KeyType)
			},
		},
		{
			name:        "nil request",
			keyID:       "test-key",
			req:         nil,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attrs, err := keyAttributesRequestToXKMS(tt.keyID, tt.req)

			if tt.expectError {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			if tt.validate != nil {
				tt.validate(t, attrs)
			}
		})
	}
}

// TestKeyAttributesRequestToXKMS_AllAlgorithms tests all algorithm branches for full coverage.
func TestKeyAttributesRequestToXKMS_AllAlgorithms(t *testing.T) {
	tests := []struct {
		name      string
		algorithm string
		validate  func(*testing.T, *xkmssdk.KeyAttributes)
	}{
		{
			name:      "AES-128-GCM",
			algorithm: "AES-128-GCM",
			validate: func(t *testing.T, attrs *xkmssdk.KeyAttributes) {
				assert.Equal(t, xkmssdk.SymmetricAES128GCM, attrs.SymmetricAlgorithm)
				assert.Equal(t, xkmssdk.KeyTypeEncryption, attrs.KeyType)
			},
		},
		{
			name:      "AES128 (alternative)",
			algorithm: "AES128",
			validate: func(t *testing.T, attrs *xkmssdk.KeyAttributes) {
				assert.Equal(t, xkmssdk.SymmetricAES128GCM, attrs.SymmetricAlgorithm)
				assert.Equal(t, xkmssdk.KeyTypeEncryption, attrs.KeyType)
			},
		},
		{
			name:      "AES-192-GCM",
			algorithm: "AES-192-GCM",
			validate: func(t *testing.T, attrs *xkmssdk.KeyAttributes) {
				assert.Equal(t, xkmssdk.SymmetricAES192GCM, attrs.SymmetricAlgorithm)
				assert.Equal(t, xkmssdk.KeyTypeEncryption, attrs.KeyType)
			},
		},
		{
			name:      "AES192 (alternative)",
			algorithm: "AES192",
			validate: func(t *testing.T, attrs *xkmssdk.KeyAttributes) {
				assert.Equal(t, xkmssdk.SymmetricAES192GCM, attrs.SymmetricAlgorithm)
				assert.Equal(t, xkmssdk.KeyTypeEncryption, attrs.KeyType)
			},
		},
		{
			name:      "AES256 (alternative)",
			algorithm: "AES256",
			validate: func(t *testing.T, attrs *xkmssdk.KeyAttributes) {
				assert.Equal(t, xkmssdk.SymmetricAES256GCM, attrs.SymmetricAlgorithm)
				assert.Equal(t, xkmssdk.KeyTypeEncryption, attrs.KeyType)
			},
		},
		{
			name:      "ChaCha20-Poly1305",
			algorithm: "CHACHA20-POLY1305",
			validate: func(t *testing.T, attrs *xkmssdk.KeyAttributes) {
				assert.Equal(t, xkmssdk.SymmetricChaCha20Poly1305, attrs.SymmetricAlgorithm)
				assert.Equal(t, xkmssdk.KeyTypeEncryption, attrs.KeyType)
			},
		},
		{
			name:      "XChaCha20-Poly1305",
			algorithm: "XCHACHA20-POLY1305",
			validate: func(t *testing.T, attrs *xkmssdk.KeyAttributes) {
				assert.Equal(t, xkmssdk.SymmetricXChaCha20Poly1305, attrs.SymmetricAlgorithm)
				assert.Equal(t, xkmssdk.KeyTypeEncryption, attrs.KeyType)
			},
		},
		{
			name:      "RSA-2048",
			algorithm: "RSA-2048",
			validate: func(t *testing.T, attrs *xkmssdk.KeyAttributes) {
				assert.Equal(t, x509.RSA, attrs.KeyAlgorithm)
				assert.Equal(t, xkmssdk.KeyTypeSigning, attrs.KeyType)
				assert.NotNil(t, attrs.RSAAttributes)
				assert.Equal(t, 2048, attrs.RSAAttributes.KeySize)
			},
		},
		{
			name:      "RSA2048 (alternative)",
			algorithm: "RSA2048",
			validate: func(t *testing.T, attrs *xkmssdk.KeyAttributes) {
				assert.Equal(t, x509.RSA, attrs.KeyAlgorithm)
				assert.Equal(t, xkmssdk.KeyTypeSigning, attrs.KeyType)
				assert.NotNil(t, attrs.RSAAttributes)
				assert.Equal(t, 2048, attrs.RSAAttributes.KeySize)
			},
		},
		{
			name:      "RSA-3072",
			algorithm: "RSA-3072",
			validate: func(t *testing.T, attrs *xkmssdk.KeyAttributes) {
				assert.Equal(t, x509.RSA, attrs.KeyAlgorithm)
				assert.NotNil(t, attrs.RSAAttributes)
				assert.Equal(t, 3072, attrs.RSAAttributes.KeySize)
			},
		},
		{
			name:      "RSA3072 (alternative)",
			algorithm: "RSA3072",
			validate: func(t *testing.T, attrs *xkmssdk.KeyAttributes) {
				assert.Equal(t, x509.RSA, attrs.KeyAlgorithm)
				assert.NotNil(t, attrs.RSAAttributes)
				assert.Equal(t, 3072, attrs.RSAAttributes.KeySize)
			},
		},
		{
			name:      "RSA4096 (alternative)",
			algorithm: "RSA4096",
			validate: func(t *testing.T, attrs *xkmssdk.KeyAttributes) {
				assert.Equal(t, x509.RSA, attrs.KeyAlgorithm)
				assert.NotNil(t, attrs.RSAAttributes)
				assert.Equal(t, 4096, attrs.RSAAttributes.KeySize)
			},
		},
		{
			name:      "ECDSA-P256",
			algorithm: "ECDSA-P256",
			validate: func(t *testing.T, attrs *xkmssdk.KeyAttributes) {
				assert.Equal(t, x509.ECDSA, attrs.KeyAlgorithm)
				assert.NotNil(t, attrs.ECCAttributes)
				assert.Equal(t, elliptic.P256(), attrs.ECCAttributes.Curve)
			},
		},
		{
			name:      "P256 (alternative)",
			algorithm: "P256",
			validate: func(t *testing.T, attrs *xkmssdk.KeyAttributes) {
				assert.Equal(t, x509.ECDSA, attrs.KeyAlgorithm)
				assert.NotNil(t, attrs.ECCAttributes)
				assert.Equal(t, elliptic.P256(), attrs.ECCAttributes.Curve)
			},
		},
		{
			name:      "ECDSA-P384",
			algorithm: "ECDSA-P384",
			validate: func(t *testing.T, attrs *xkmssdk.KeyAttributes) {
				assert.Equal(t, x509.ECDSA, attrs.KeyAlgorithm)
				assert.NotNil(t, attrs.ECCAttributes)
				assert.Equal(t, elliptic.P384(), attrs.ECCAttributes.Curve)
			},
		},
		{
			name:      "P384 (alternative)",
			algorithm: "P384",
			validate: func(t *testing.T, attrs *xkmssdk.KeyAttributes) {
				assert.Equal(t, x509.ECDSA, attrs.KeyAlgorithm)
				assert.NotNil(t, attrs.ECCAttributes)
				assert.Equal(t, elliptic.P384(), attrs.ECCAttributes.Curve)
			},
		},
		{
			name:      "ECDSA-P521",
			algorithm: "ECDSA-P521",
			validate: func(t *testing.T, attrs *xkmssdk.KeyAttributes) {
				assert.Equal(t, x509.ECDSA, attrs.KeyAlgorithm)
				assert.NotNil(t, attrs.ECCAttributes)
				assert.Equal(t, elliptic.P521(), attrs.ECCAttributes.Curve)
			},
		},
		{
			name:      "P521 (alternative)",
			algorithm: "P521",
			validate: func(t *testing.T, attrs *xkmssdk.KeyAttributes) {
				assert.Equal(t, x509.ECDSA, attrs.KeyAlgorithm)
				assert.NotNil(t, attrs.ECCAttributes)
				assert.Equal(t, elliptic.P521(), attrs.ECCAttributes.Curve)
			},
		},
		{
			name:      "Ed25519",
			algorithm: "ED25519",
			validate: func(t *testing.T, attrs *xkmssdk.KeyAttributes) {
				assert.Equal(t, x509.Ed25519, attrs.KeyAlgorithm)
				assert.Equal(t, xkmssdk.KeyTypeSigning, attrs.KeyType)
			},
		},
		{
			name:      "Unknown algorithm defaults to AES-256-GCM",
			algorithm: "UNKNOWN-ALGORITHM",
			validate: func(t *testing.T, attrs *xkmssdk.KeyAttributes) {
				assert.Equal(t, xkmssdk.SymmetricAES256GCM, attrs.SymmetricAlgorithm)
				assert.Equal(t, xkmssdk.KeyTypeEncryption, attrs.KeyType)
			},
		},
		{
			name:      "Empty algorithm defaults to AES-256-GCM",
			algorithm: "",
			validate: func(t *testing.T, attrs *xkmssdk.KeyAttributes) {
				assert.Equal(t, xkmssdk.SymmetricAES256GCM, attrs.SymmetricAlgorithm)
				assert.Equal(t, xkmssdk.KeyTypeEncryption, attrs.KeyType)
			},
		},
		{
			name:      "Lowercase algorithm (case insensitivity)",
			algorithm: "aes-256-gcm",
			validate: func(t *testing.T, attrs *xkmssdk.KeyAttributes) {
				assert.Equal(t, xkmssdk.SymmetricAES256GCM, attrs.SymmetricAlgorithm)
				assert.Equal(t, xkmssdk.KeyTypeEncryption, attrs.KeyType)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &KeyAttributesRequest{
				Algorithm: tt.algorithm,
				KeyUsage:  []string{"encrypt", "decrypt"},
			}
			attrs, err := keyAttributesRequestToXKMS("test-key", req)

			assert.NoError(t, err)
			assert.NotNil(t, attrs)
			assert.Equal(t, "test-key", attrs.CN)
			tt.validate(t, attrs)
		})
	}
}

// TestHandleKMSError tests the handleKMSError function with all error codes.
func TestHandleKMSError(t *testing.T) {
	tests := []struct {
		name           string
		err            error
		expectedStatus int
	}{
		{
			name:           "nil error - no response",
			err:            nil,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "non-QBError returns 500",
			err:            stderrors.New("generic error"),
			expectedStatus: http.StatusInternalServerError,
		},
		{
			name: "ErrNotFound returns 404",
			err: &errors.QBError{
				Code: errors.ErrNotFound,
				Op:   "test",
				Err:  stderrors.New("key not found"),
			},
			expectedStatus: http.StatusNotFound,
		},
		{
			name: "ErrAlreadyExists returns 409",
			err: &errors.QBError{
				Code: errors.ErrAlreadyExists,
				Op:   "test",
				Err:  stderrors.New("key already exists"),
			},
			expectedStatus: http.StatusConflict,
		},
		{
			name: "ErrInvalidInput returns 400",
			err: &errors.QBError{
				Code: errors.ErrInvalidInput,
				Op:   "test",
				Err:  stderrors.New("invalid input"),
			},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "ErrUnauthorized returns 401",
			err: &errors.QBError{
				Code: errors.ErrUnauthorized,
				Op:   "test",
				Err:  stderrors.New("unauthorized"),
			},
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name: "ErrForbidden returns 403",
			err: &errors.QBError{
				Code: errors.ErrForbidden,
				Op:   "test",
				Err:  stderrors.New("forbidden"),
			},
			expectedStatus: http.StatusForbidden,
		},
		{
			name: "ErrTimeout returns 504",
			err: &errors.QBError{
				Code: errors.ErrTimeout,
				Op:   "test",
				Err:  stderrors.New("timeout"),
			},
			expectedStatus: http.StatusGatewayTimeout,
		},
		{
			name: "ErrResourceExhausted returns 429",
			err: &errors.QBError{
				Code: errors.ErrResourceExhausted,
				Op:   "test",
				Err:  stderrors.New("rate limited"),
			},
			expectedStatus: http.StatusTooManyRequests,
		},
		{
			name: "ErrUnavailable returns 503",
			err: &errors.QBError{
				Code: errors.ErrUnavailable,
				Op:   "test",
				Err:  stderrors.New("service unavailable"),
			},
			expectedStatus: http.StatusServiceUnavailable,
		},
		{
			name: "ErrInternal returns 500",
			err: &errors.QBError{
				Code: errors.ErrInternal,
				Op:   "test",
				Err:  stderrors.New("internal error"),
			},
			expectedStatus: http.StatusInternalServerError,
		},
		{
			name: "Unknown QBError code returns 500",
			err: &errors.QBError{
				Code: errors.ErrorCode(9999),
				Op:   "test",
				Err:  stderrors.New("unknown error"),
			},
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, _, cleanup := setupMockServer(t)
			defer cleanup()

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			w := httptest.NewRecorder()

			server.handleKMSError(w, req, tt.err)

			if tt.err == nil {
				assert.Equal(t, http.StatusOK, w.Code)
			} else {
				assert.Equal(t, tt.expectedStatus, w.Code)
			}
		})
	}
}

// =============================================================================
// Service-layer conversion helper tests
// =============================================================================

func TestSvcKeyHandleToResponse(t *testing.T) {
	t.Run("converts key handle with all fields", func(t *testing.T) {
		now := time.Now()
		kh := &service.KeyHandle{
			KeyID:     "test-key-001",
			Version:   3,
			Algorithm: service.KeyAlgorithm("aes256-gcm"),
			Status:    service.KeyStatusEnabled,
			CreatedAt: now,
		}

		resp := svcKeyHandleToResponse(kh)

		require.NotNil(t, resp)
		assert.Equal(t, "test-key-001", resp.KeyID)
		assert.Equal(t, 3, resp.Version)
		assert.Equal(t, string(service.KeyAlgorithm("aes256-gcm")), resp.Algorithm)
		assert.Equal(t, string(service.KeyStatusEnabled), resp.Status)
		assert.Equal(t, now.Unix(), resp.CreatedAt)
	})

	t.Run("converts key handle with zero version", func(t *testing.T) {
		kh := &service.KeyHandle{
			KeyID:     "zero-ver",
			Version:   0,
			Algorithm: "RSA-2048",
			Status:    service.KeyStatusDisabled,
			CreatedAt: time.Unix(0, 0),
		}

		resp := svcKeyHandleToResponse(kh)

		require.NotNil(t, resp)
		assert.Equal(t, 0, resp.Version)
		assert.Equal(t, string(service.KeyStatusDisabled), resp.Status)
	})
}

func TestSvcMetadataToRESTResponse(t *testing.T) {
	t.Run("converts metadata with all fields including LastUsedAt", func(t *testing.T) {
		now := time.Now()
		lastUsed := now.Add(-2 * time.Hour)
		md := &service.KeyMetadata{
			KeyID:              "meta-key-001",
			Name:               "My Key",
			Description:        "Test key for unit tests",
			Algorithm:          service.KeyAlgorithm("aes256-gcm"),
			Status:             service.KeyStatusEnabled,
			Usage:              []service.KeyUsage{"encrypt", "decrypt", "wrap"},
			Version:            5,
			Exportable:         true,
			DeletionProtection: true,
			CreatedAt:          now,
			UpdatedAt:          now,
			LastUsedAt:         &lastUsed,
			Tags:               map[string]string{"env": "test", "team": "security"},
			Owner:              "admin-user",
		}

		resp := svcMetadataToRESTResponse(md)

		require.NotNil(t, resp)
		assert.Equal(t, "meta-key-001", resp.KeyID)
		assert.Equal(t, "My Key", resp.Name)
		assert.Equal(t, "Test key for unit tests", resp.Description)
		assert.Equal(t, string(service.KeyAlgorithm("aes256-gcm")), resp.Algorithm)
		assert.Equal(t, string(service.KeyStatusEnabled), resp.Status)
		assert.Equal(t, []string{"encrypt", "decrypt", "wrap"}, resp.Usage)
		assert.Equal(t, 5, resp.Version)
		assert.True(t, resp.Exportable)
		assert.True(t, resp.DeletionProtection)
		assert.Equal(t, now.Unix(), resp.CreatedAt)
		assert.Equal(t, now.Unix(), resp.UpdatedAt)
		assert.Equal(t, lastUsed.Unix(), resp.LastUsedAt)
		assert.Equal(t, map[string]string{"env": "test", "team": "security"}, resp.Tags)
		assert.Equal(t, "admin-user", resp.Owner)
	})

	t.Run("converts metadata without LastUsedAt", func(t *testing.T) {
		now := time.Now()
		md := &service.KeyMetadata{
			KeyID:     "meta-key-002",
			Algorithm: service.KeyAlgorithm("aes256-gcm"),
			Status:    service.KeyStatusEnabled,
			Usage:     []service.KeyUsage{},
			CreatedAt: now,
			UpdatedAt: now,
		}

		resp := svcMetadataToRESTResponse(md)

		require.NotNil(t, resp)
		assert.Equal(t, int64(0), resp.LastUsedAt)
		assert.Empty(t, resp.Usage)
	})
}

func TestKmsMetadataToResponseWithUsage(t *testing.T) {
	t.Run("converts with LastUsedAt set", func(t *testing.T) {
		now := time.Now()
		lastUsed := now.Add(-30 * time.Minute)
		md := &kms.KeyMetadata{
			KeyID:              "kms-key-001",
			Algorithm:          "AES-256-GCM",
			Version:            2,
			State:              kms.KeyStateEnabled,
			Usage:              []kms.KeyUsage{"encrypt", "decrypt"},
			Exportable:         true,
			DeletionProtection: false,
			Created:            now,
			Updated:            now,
			LastUsedAt:         &lastUsed,
		}

		resp := kmsMetadataToResponse(md, "My KMS Key", "A test description", map[string]string{"env": "dev"}, "owner-123")

		require.NotNil(t, resp)
		assert.Equal(t, "kms-key-001", resp.KeyID)
		assert.Equal(t, "My KMS Key", resp.Name)
		assert.Equal(t, "A test description", resp.Description)
		assert.Equal(t, []string{"encrypt", "decrypt"}, resp.Usage)
		assert.Equal(t, lastUsed.Unix(), resp.LastUsedAt)
		assert.Equal(t, "owner-123", resp.Owner)
	})

	t.Run("converts without LastUsedAt", func(t *testing.T) {
		now := time.Now()
		md := &kms.KeyMetadata{
			KeyID:   "kms-key-002",
			Created: now,
			Updated: now,
		}

		resp := kmsMetadataToResponse(md, "", "", nil, "")

		require.NotNil(t, resp)
		assert.Equal(t, int64(0), resp.LastUsedAt)
	})
}

func TestKmsMetadataToResponseFullWithUsage(t *testing.T) {
	t.Run("converts full metadata with all fields", func(t *testing.T) {
		now := time.Now()
		lastUsed := now.Add(-5 * time.Minute)
		md := &kms.KeyMetadata{
			KeyID:              "full-key-001",
			Name:               "Full Key",
			Description:        "Fully populated key",
			Algorithm:          "RSA-4096",
			Version:            10,
			State:              kms.KeyStateEnabled,
			Usage:              []kms.KeyUsage{"sign", "verify"},
			Exportable:         false,
			DeletionProtection: true,
			Created:            now,
			Updated:            now,
			LastUsedAt:         &lastUsed,
			Tags:               map[string]string{"tier": "premium"},
			Owner:              "admin",
		}

		resp := kmsMetadataToResponseFull(md)

		require.NotNil(t, resp)
		assert.Equal(t, "full-key-001", resp.KeyID)
		assert.Equal(t, "Full Key", resp.Name)
		assert.Equal(t, "Fully populated key", resp.Description)
		assert.Equal(t, "RSA-4096", resp.Algorithm)
		assert.Equal(t, 10, resp.Version)
		assert.Equal(t, []string{"sign", "verify"}, resp.Usage)
		assert.False(t, resp.Exportable)
		assert.True(t, resp.DeletionProtection)
		assert.Equal(t, lastUsed.Unix(), resp.LastUsedAt)
		assert.Equal(t, map[string]string{"tier": "premium"}, resp.Tags)
		assert.Equal(t, "admin", resp.Owner)
	})

	t.Run("converts full metadata without optional fields", func(t *testing.T) {
		now := time.Now()
		md := &kms.KeyMetadata{
			KeyID:   "full-key-002",
			Created: now,
			Updated: now,
		}

		resp := kmsMetadataToResponseFull(md)

		require.NotNil(t, resp)
		assert.Equal(t, int64(0), resp.LastUsedAt)
		assert.Empty(t, resp.Usage)
	})
}

func TestHandleRotateKeyGetKeyError(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockKMSMgr := &MockKMSManager{}
	mockKMSMgr.On("GetKey", mock.Anything, "test-key").Return(nil, &errors.QBError{
		Code: errors.ErrNotFound,
		Op:   "GetKey",
		Err:  assert.AnError,
	}).Once()
	mockService.DisableKMS = true
	mockService.On("KMSManager").Return(mockKMSMgr)

	body := `{}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/keys/test-key/rotate", bytes.NewReader([]byte(body)))
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "test-key")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleRotateKey(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleRotateKeyWrongTypeFromGetKey(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockKMSMgr := &MockKMSManager{}
	// Return a string instead of *kms.KeyMetadata
	mockKMSMgr.On("GetKey", mock.Anything, "test-key").Return("wrong-type", nil).Once()
	mockService.DisableKMS = true
	mockService.On("KMSManager").Return(mockKMSMgr)

	body := `{}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/keys/test-key/rotate", bytes.NewReader([]byte(body)))
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "test-key")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleRotateKey(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "unexpected response type")
}

func TestHandleRotateKeyWrongTypeFromRotateKey(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockKMSMgr := &MockKMSManager{}
	mockKMSMgr.On("GetKey", mock.Anything, "test-key").Return(&kms.KeyMetadata{
		KeyID:   "test-key",
		Version: 1,
		Created: time.Now(),
		Updated: time.Now(),
	}, nil).Once()
	// Return a string instead of *kms.KeyMetadata
	mockKMSMgr.On("RotateKey", mock.Anything, "test-key").Return("wrong-type", nil).Once()
	mockService.DisableKMS = true
	mockService.On("KMSManager").Return(mockKMSMgr)

	body := `{}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/keys/test-key/rotate", bytes.NewReader([]byte(body)))
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "test-key")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleRotateKey(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "unexpected response type")
}

func TestHandleRotateKeyKMSManagerNil(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.DisableKMS = true
	mockService.On("KMSManager").Return(nil)

	body := `{}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/keys/test-key/rotate", bytes.NewReader([]byte(body)))
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "test-key")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleRotateKey(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "KMS service not available")
}

func TestHandleGenerateKeyKMSManagerNil(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.DisableKMS = true
	mockService.On("KMSManager").Return(nil)

	body := `{"key_attributes":{"algorithm":"AES-256-GCM","key_usage":["encrypt","decrypt"]}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/keys", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleGenerateKey(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "KMS service not available")
}

func TestHandleGenerateKeyWrongReturnType(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockKMSMgr := &MockKMSManager{}
	mockKMSMgr.On("GenerateKey", mock.Anything, mock.Anything).Return("wrong-type", nil).Once()
	mockService.DisableKMS = true
	mockService.On("KMSManager").Return(mockKMSMgr)

	body := `{"key_attributes":{"algorithm":"AES-256-GCM","key_usage":["encrypt","decrypt"]}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/keys", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleGenerateKey(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "unexpected response type")
}

func TestHandleGenerateKeyServiceError(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockKMSMgr := &MockKMSManager{}
	mockKMSMgr.On("GenerateKey", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
		Code: errors.ErrInternal,
		Op:   "GenerateKey",
		Err:  assert.AnError,
	}).Once()
	mockService.DisableKMS = true
	mockService.On("KMSManager").Return(mockKMSMgr)

	body := `{"key_attributes":{"algorithm":"AES-256-GCM","key_usage":["encrypt","decrypt"]}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/keys", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleGenerateKey(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestHandleGetKeyKMSManagerNil(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.DisableKMS = true
	mockService.On("KMSManager").Return(nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/kms/keys/test-key", nil)
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "test-key")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleGetKey(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "KMS service not available")
}

func TestHandleGetKeyWrongReturnType(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockKMSMgr := &MockKMSManager{}
	mockKMSMgr.On("GetKey", mock.Anything, "test-key").Return("wrong-type", nil).Once()
	mockService.DisableKMS = true
	mockService.On("KMSManager").Return(mockKMSMgr)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/kms/keys/test-key", nil)
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "test-key")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleGetKey(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "unexpected response type")
}

func TestHandleDeleteKeyKMSManagerNil(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.DisableKMS = true
	mockService.On("KMSManager").Return(nil)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/kms/keys/test-key", nil)
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "test-key")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleDeleteKey(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "KMS service not available")
}

func TestHandleDeleteKeyNotFound(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockKMSMgr := &MockKMSManager{}
	// DisableKey is called first before DeleteKey
	mockKMSMgr.On("DisableKey", mock.Anything, "test-key").Return(nil).Once()
	mockKMSMgr.On("DeleteKey", mock.Anything, "test-key").Return(&errors.QBError{
		Code: errors.ErrNotFound,
		Op:   "DeleteKey",
		Err:  assert.AnError,
	}).Once()
	mockService.DisableKMS = true
	mockService.On("KMSManager").Return(mockKMSMgr)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/kms/keys/test-key", nil)
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "test-key")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleDeleteKey(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleUpdateKeyStatusKMSManagerNil(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("KMSManager").Return(nil)

	body := `{"status":"enabled"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/kms/keys/test-key/status", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "test-key")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleUpdateKeyStatus(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "KMS service not available")
}

func TestHandleUpdateKeyStatusInvalidStatus(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockKMSMgr := &MockKMSManager{}
	mockService.On("KMSManager").Return(mockKMSMgr)

	body := `{"status":"invalid-status"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/kms/keys/test-key/status", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "test-key")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleUpdateKeyStatus(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "unsupported status")
}

func TestHandleListKeyVersionsKMSManagerNil(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("KMSManager").Return(nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/kms/keys/test-key/versions", nil)
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "test-key")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleListKeyVersions(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "KMS service not available")
}

func TestHandleListKeyVersionsWrongReturnType(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockKMSMgr := &MockKMSManager{}
	mockKMSMgr.On("GetKey", mock.Anything, "test-key").Return("wrong-type", nil).Once()
	mockService.On("KMSManager").Return(mockKMSMgr)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/kms/keys/test-key/versions", nil)
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "test-key")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleListKeyVersions(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "unexpected response type")
}

func TestHandleSetDefaultDEKFromBodyEmptyPath(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	body := `{"path":""}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/kms/deks/default", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.HandleSetDefaultDEKFromBody(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "path is required")
}

func TestHandleListDEKsError(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("ListDEKs", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
		Code: errors.ErrInternal,
		Op:   "ListDEKs",
		Err:  assert.AnError,
	}).Once()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/kms/deks", nil)
	w := httptest.NewRecorder()

	server.HandleListDEKs(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestHandleGetRebalancePlanNilResponse(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("ClusterStatus", mock.Anything, mock.Anything).Return(nil, nil).Once()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cluster/rebalance/plan", nil)
	w := httptest.NewRecorder()

	server.HandleGetRebalancePlan(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "cluster status unavailable")
}

func TestHandleGetRebalancePlanServiceError(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("ClusterStatus", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
		Code: errors.ErrInternal,
		Op:   "ClusterStatus",
		Err:  assert.AnError,
	}).Once()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cluster/rebalance/plan", nil)
	w := httptest.NewRecorder()

	server.HandleGetRebalancePlan(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// Signer returns a crypto.Signer for the given keyID. Tests that don't
// exercise the signer path receive a nil signer and no error; tests
// that do can stub this by redefining the method on a wrapped type.
func (m *MockKMSManager) Signer(_ context.Context, _ string) (crypto.Signer, error) {
	return nil, nil
}

func TestHandleSetDefaultKey(t *testing.T) {
	tests := []struct {
		name           string
		keyName        string
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
	}{
		{
			name:    "set default key successfully",
			keyName: "my-key",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("SetDefaultKey", mock.Anything, &service.SetDefaultKeyRequest{Name: "my-key"}).
					Return(&service.SetDefaultKeyResponse{
						Name:      "my-key",
						IsDefault: true,
					}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:    "key not found returns 404",
			keyName: "missing-key",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("SetDefaultKey", mock.Anything, &service.SetDefaultKeyRequest{Name: "missing-key"}).
					Return(nil, &errors.QBError{Code: errors.ErrNotFound, Op: "kms.SetDefaultKey"}).Once()
			},
			expectedStatus: http.StatusNotFound,
		},
		{
			name:           "missing key name returns 400",
			keyName:        "",
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockService, cleanup := setupMockServer(t)
			defer cleanup()
			tt.setupMock(mockService)

			req := httptest.NewRequest(http.MethodPut, "/api/v1/kms/keys/"+tt.keyName+"/default", nil)
			w := httptest.NewRecorder()

			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("name", tt.keyName)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			server.HandleSetDefaultKey(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
		})
	}
}

// ---------------------------------------------------------------------------
// Regression tests: isSystemKey filter (Issue 2 fix)
// ---------------------------------------------------------------------------

// TestIsSystemKey ensures the predicate correctly classifies system keys
// so the KMS list API excludes daemon-internal keys from operator responses.
func TestIsSystemKey(t *testing.T) {
	tests := []struct {
		name      string
		keyID     string
		algorithm string
		want      bool
	}{
		// System keys — must be excluded
		{"empty algorithm (CA cert)", "QuicBaaS Root CA", "", true},
		{"empty algorithm (TLS server cert)", "localhost", "", true},
		{"empty algorithm (TLS IP cert)", "127.0.0.1", "", true},
		{"empty algorithm (agent identity)", "agent-embedded-host-abc12345", "", true},
		{"well-known CA name without algorithm", "QuicBaaS Root CA", "", true},
		{"agent-embedded prefix", "agent-embedded-vm-host-2-0c592403", "", true},
		{"agent-embedded prefix with algorithm empty", "agent-embedded-myhost", "", true},
		// Legitimate operator keys — must be included
		{"tenant DEK key", "quicbaas-tenant-dek-kek", "AES-256-GCM", false},
		{"operator key AES", "my-encryption-key", "AES-256-GCM", false},
		{"operator key RSA", "signing-key", "RSA-2048", false},
		{"operator key ECDSA", "jwt-key", "ECDSA-P256", false},
		// Edge: agent-embedded-like name but has algorithm (hypothetical future case)
		{"agent-embedded with algorithm", "agent-embedded-host", "AES-256-GCM", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isSystemKey(tt.keyID, tt.algorithm)
			assert.Equal(t, tt.want, got, "isSystemKey(%q, %q)", tt.keyID, tt.algorithm)
		})
	}
}

// TestHandleListKeys_ExcludesSystemKeys verifies that HandleListKeys
// never returns keys with empty algorithm or known system-key IDs.
func TestHandleListKeys_ExcludesSystemKeys(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	epoch := time.Unix(-62135596800, 0)
	now := time.Now()

	// Mix of system keys and one legitimate operator key.
	svcKeys := []*service.KeyHandle{
		{KeyID: "QuicBaaS Root CA", Algorithm: service.KeyAlgorithm(""), Version: 1, Status: service.KeyStatusEnabled, CreatedAt: epoch},
		{KeyID: "localhost", Algorithm: service.KeyAlgorithm(""), Version: 1, Status: service.KeyStatusEnabled, CreatedAt: epoch},
		{KeyID: "127.0.0.1", Algorithm: service.KeyAlgorithm(""), Version: 1, Status: service.KeyStatusEnabled, CreatedAt: epoch},
		{KeyID: "agent-embedded-host-abc12345", Algorithm: service.KeyAlgorithm(""), Version: 1, Status: service.KeyStatusEnabled, CreatedAt: epoch},
		{KeyID: "quicbaas-tenant-dek-kek", Algorithm: service.KeyAlgorithm("AES-256-GCM"), Version: 1, Status: service.KeyStatusEnabled, CreatedAt: now},
	}
	mockService.On("ListKeys", mock.Anything, mock.Anything).Return(&service.ListKeysResponse{
		Keys:       svcKeys,
		TotalCount: len(svcKeys),
		Timestamp:  now,
	}, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/kms/keys", nil)
	w := httptest.NewRecorder()
	server.HandleListKeys(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var resp ListKeysRESTResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))

	// Only the operator key should remain.
	require.Len(t, resp.Keys, 1, "only operator keys must be returned")
	assert.Equal(t, "quicbaas-tenant-dek-kek", resp.Keys[0].KeyID)
	assert.Equal(t, "AES-256-GCM", resp.Keys[0].Algorithm)
}

// TestHandleListKeys_LimitDoesNotTruncateBeforeSystemKeyFilter is a regression
// test for the bug where ?limit=N would truncate the service-layer response
// before the REST handler stripped system keys, producing 0 operator-visible
// keys when system keys sorted first. The handler must fetch unbounded from
// the service, filter system keys, then apply the caller's limit.
func TestHandleListKeys_LimitDoesNotTruncateBeforeSystemKeyFilter(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	epoch := time.Unix(-62135596800, 0)
	now := time.Now()

	// Two system keys sort first, followed by three operator keys.
	svcKeys := []*service.KeyHandle{
		{KeyID: "QuicBaaS Root CA", Algorithm: service.KeyAlgorithm(""), Status: service.KeyStatusEnabled, CreatedAt: epoch},
		{KeyID: "localhost", Algorithm: service.KeyAlgorithm(""), Status: service.KeyStatusEnabled, CreatedAt: epoch},
		{KeyID: "user-key-a", Algorithm: service.KeyAlgorithm("AES-256-GCM"), Status: service.KeyStatusEnabled, CreatedAt: now},
		{KeyID: "user-key-b", Algorithm: service.KeyAlgorithm("AES-256-GCM"), Status: service.KeyStatusEnabled, CreatedAt: now},
		{KeyID: "user-key-c", Algorithm: service.KeyAlgorithm("AES-256-GCM"), Status: service.KeyStatusEnabled, CreatedAt: now},
	}

	// Capture the request the handler sends to the service so we can assert it
	// did NOT forward the caller's limit (which would re-introduce the bug).
	var capturedReq *service.ListKeysRequest
	mockService.On("ListKeys", mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			capturedReq = args.Get(1).(*service.ListKeysRequest)
		}).
		Return(&service.ListKeysResponse{
			Keys:       svcKeys,
			TotalCount: len(svcKeys),
			Timestamp:  now,
		}, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/kms/keys?limit=2", nil)
	w := httptest.NewRecorder()
	server.HandleListKeys(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var resp ListKeysRESTResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))

	require.Len(t, resp.Keys, 2, "limit=2 must return 2 operator keys, not 0 after system-key filter")
	assert.Equal(t, "user-key-a", resp.Keys[0].KeyID)
	assert.Equal(t, "user-key-b", resp.Keys[1].KeyID)
	assert.True(t, resp.HasMore, "third operator key exists; HasMore must be true")
	assert.Equal(t, 3, resp.TotalCount, "TotalCount reflects operator-visible total")

	require.NotNil(t, capturedReq, "service.ListKeys must be invoked")
	assert.NotEqual(t, 2, capturedReq.Limit,
		"handler must not forward caller's limit=2 to the service (filter happens after)")
}

// TestHandleListKeyVersions_NonZeroCreatedAt asserts that every version entry in
// the response carries a non-zero created_at Unix timestamp (bug #88).  A zero
// value would cause the frontend to display "1/21/1970" instead of the actual
// creation date.
func TestHandleListKeyVersions_NonZeroCreatedAt(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	keyCreated := time.Date(2025, 5, 3, 0, 0, 0, 0, time.UTC)

	mockKMSMgr := &MockKMSManager{}
	mockKMSMgr.On("GetKey", mock.Anything, "my-key").Return(&kms.KeyMetadata{
		KeyID:     "my-key",
		Algorithm: "AES-256-GCM",
		Version:   2,
		State:     kms.KeyStateEnabled,
		Created:   keyCreated,
		Updated:   keyCreated,
	}, nil).Once()
	mockService.DisableKMS = true
	mockService.On("KMSManager").Return(mockKMSMgr)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/kms/keys/my-key/versions", nil)
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "my-key")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleListKeyVersions(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var resp ListKeyVersionsRESTResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))

	require.Len(t, resp.Versions, 2, "expected one entry per version")
	for _, v := range resp.Versions {
		assert.Greater(t, v.CreatedAt, int64(0),
			"version %d: created_at must be a non-zero Unix timestamp (got %d)", v.Version, v.CreatedAt)
	}
	// Sanity-check: the timestamp must encode the correct year (2025).
	expectedUnix := keyCreated.Unix()
	assert.Equal(t, expectedUnix, resp.Versions[0].CreatedAt)
}
