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
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// mockRoundTripper implements http.RoundTripper for testing HTTP client.
type mockRoundTripper struct {
	response    *http.Response
	err         error
	requestLog  []*http.Request
	statusCode  int
	body        interface{}
	contentType string
}

func newMockRoundTripper(statusCode int, body interface{}) *mockRoundTripper {
	return &mockRoundTripper{
		statusCode:  statusCode,
		body:        body,
		contentType: "application/json",
		requestLog:  make([]*http.Request, 0),
	}
}

func (m *mockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	m.requestLog = append(m.requestLog, req)
	if m.err != nil {
		return nil, m.err
	}

	if m.response != nil {
		return m.response, nil
	}

	bodyJSON, _ := json.Marshal(m.body)
	return &http.Response{
		StatusCode: m.statusCode,
		Body:       NewMockReadCloser(bodyJSON),
		Header:     http.Header{"Content-Type": []string{m.contentType}},
		Request:    req,
	}, nil
}

// MockReadCloser implements io.ReadCloser for test responses.
type MockReadCloser struct {
	data   []byte
	offset int
}

func NewMockReadCloser(data []byte) *MockReadCloser {
	return &MockReadCloser{data: data}
}

func (m *MockReadCloser) Read(p []byte) (n int, err error) {
	if m.offset >= len(m.data) {
		return 0, io.EOF
	}
	n = copy(p, m.data[m.offset:])
	m.offset += n
	return n, nil
}

func (m *MockReadCloser) Close() error {
	return nil
}

// createTestClientWithMock creates a RESTClient with injected mock transport.
func createTestClientWithMock(t *testing.T, mock *mockRoundTripper) *RESTClient {
	log, err := logger.New(logger.LogConfig{Level: "debug", Format: "console"})
	require.NoError(t, err)

	client, err := NewRESTClient("http://test-server:8080", log)
	require.NoError(t, err)

	client.httpClient.Transport = mock
	return client
}

// =============================================================================
// KMS Client Method Tests
// =============================================================================

func TestRESTClientMock_KMSEncrypt_Success(t *testing.T) {
	expectedResp := &service.EncryptResponse{
		KeyID:      "test-key",
		KeyVersion: 1,
		Ciphertext: []byte("encrypted-data"),
		Timestamp:  time.Now(),
	}

	mock := newMockRoundTripper(http.StatusOK, expectedResp)
	client := createTestClientWithMock(t, mock)

	resp, err := client.KMSEncrypt(context.Background(), &service.EncryptRequest{
		KeyID:     "test-key",
		Plaintext: []byte("secret"),
	})

	require.NoError(t, err)
	assert.Equal(t, "test-key", resp.KeyID)
	assert.Len(t, mock.requestLog, 1)
	assert.Equal(t, "/api/v1/kms/encrypt/test-key", mock.requestLog[0].URL.Path)
}

func TestRESTClientMock_KMSEncrypt_NilRequest(t *testing.T) {
	mock := newMockRoundTripper(http.StatusOK, nil)
	client := createTestClientWithMock(t, mock)

	resp, err := client.KMSEncrypt(context.Background(), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
}

func TestRESTClientMock_KMSEncrypt_ServerError(t *testing.T) {
	errResp := &ErrorResponse{
		Error:     "encryption failed",
		Code:      "internal",
		Timestamp: time.Now(),
	}
	mock := newMockRoundTripper(http.StatusInternalServerError, errResp)
	client := createTestClientWithMock(t, mock)

	resp, err := client.KMSEncrypt(context.Background(), &service.EncryptRequest{
		KeyID:     "test-key",
		Plaintext: []byte("secret"),
	})

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestRESTClientMock_KMSDecrypt_Success(t *testing.T) {
	expectedResp := &service.DecryptResponse{
		KeyID:     "test-key",
		Plaintext: []byte("decrypted-data"),
		Timestamp: time.Now(),
	}

	mock := newMockRoundTripper(http.StatusOK, expectedResp)
	client := createTestClientWithMock(t, mock)

	resp, err := client.KMSDecrypt(context.Background(), &service.DecryptRequest{
		KeyID:      "test-key",
		Ciphertext: []byte("encrypted"),
	})

	require.NoError(t, err)
	assert.Equal(t, "test-key", resp.KeyID)
}

func TestRESTClientMock_KMSDecrypt_NilRequest(t *testing.T) {
	mock := newMockRoundTripper(http.StatusOK, nil)
	client := createTestClientWithMock(t, mock)

	resp, err := client.KMSDecrypt(context.Background(), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestRESTClientMock_KMSGenerateDataKey_Success(t *testing.T) {
	expectedResp := &DataKeyRESTResponse{
		Plaintext:      []byte("plain-key"),
		CiphertextBlob: []byte("encrypted-key"),
		KeyID:          "default",
	}

	mock := newMockRoundTripper(http.StatusOK, expectedResp)
	client := createTestClientWithMock(t, mock)

	resp, err := client.KMSGenerateDataKey(context.Background(), &DataKeyRESTRequest{
		KeySizeBits: 256,
	})

	require.NoError(t, err)
	assert.NotNil(t, resp)
}

func TestRESTClientMock_KMSGenerateDataKey_NilRequest(t *testing.T) {
	mock := newMockRoundTripper(http.StatusOK, nil)
	client := createTestClientWithMock(t, mock)

	resp, err := client.KMSGenerateDataKey(context.Background(), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestRESTClientMock_KMSGenerateDataKeyWithKEK_Success(t *testing.T) {
	expectedResp := &DataKeyRESTResponse{
		Plaintext:      []byte("plain-key"),
		CiphertextBlob: []byte("encrypted-key"),
		KeyID:          "my-kek",
	}

	mock := newMockRoundTripper(http.StatusOK, expectedResp)
	client := createTestClientWithMock(t, mock)

	resp, err := client.KMSGenerateDataKeyWithKEK(context.Background(), "my-kek", &DataKeyRESTRequest{
		KeySizeBits: 256,
	})

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Contains(t, mock.requestLog[0].URL.Path, "my-kek")
}

func TestRESTClientMock_KMSGenerateDataKeyWithKEK_EmptyKEKName(t *testing.T) {
	mock := newMockRoundTripper(http.StatusOK, nil)
	client := createTestClientWithMock(t, mock)

	resp, err := client.KMSGenerateDataKeyWithKEK(context.Background(), "", &DataKeyRESTRequest{})

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestRESTClientMock_KMSGenerateDataKeyWithKEK_NilRequest(t *testing.T) {
	mock := newMockRoundTripper(http.StatusOK, nil)
	client := createTestClientWithMock(t, mock)

	resp, err := client.KMSGenerateDataKeyWithKEK(context.Background(), "my-kek", nil)

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestRESTClientMock_KMSGenerateKey_Success(t *testing.T) {
	expectedResp := &GenerateKeyRESTResponse{
		Key: &KeyHandleResponse{
			KeyID:     "new-key",
			Version:   1,
			Algorithm: "AES-256-GCM",
			Status:    "enabled",
		},
	}

	mock := newMockRoundTripper(http.StatusOK, expectedResp)
	client := createTestClientWithMock(t, mock)

	resp, err := client.KMSGenerateKey(context.Background(), &GenerateKeyRESTRequest{
		Name: "test-key",
	})

	require.NoError(t, err)
	assert.Equal(t, "new-key", resp.Key.KeyID)
}

func TestRESTClientMock_KMSGenerateKey_NilRequest(t *testing.T) {
	mock := newMockRoundTripper(http.StatusOK, nil)
	client := createTestClientWithMock(t, mock)

	resp, err := client.KMSGenerateKey(context.Background(), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestRESTClientMock_KMSImportKey_Success(t *testing.T) {
	expectedResp := &GenerateKeyRESTResponse{
		Key: &KeyHandleResponse{
			KeyID:     "imported-key",
			Version:   1,
			Algorithm: "AES-256-GCM",
		},
	}

	mock := newMockRoundTripper(http.StatusOK, expectedResp)
	client := createTestClientWithMock(t, mock)

	resp, err := client.KMSImportKey(context.Background(), &ImportKeyRESTRequest{
		Name:        "imported-key",
		KeyMaterial: []byte("raw-key-material"),
	})

	require.NoError(t, err)
	assert.Equal(t, "imported-key", resp.Key.KeyID)
}

func TestRESTClientMock_KMSImportKey_NilRequest(t *testing.T) {
	mock := newMockRoundTripper(http.StatusOK, nil)
	client := createTestClientWithMock(t, mock)

	resp, err := client.KMSImportKey(context.Background(), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestRESTClientMock_KMSGetKey_Success(t *testing.T) {
	expectedResp := &GetKeyRESTResponse{
		Key: &KeyHandleResponse{
			KeyID:     "test-key",
			Algorithm: "AES-256-GCM",
			Status:    "enabled",
		},
	}

	mock := newMockRoundTripper(http.StatusOK, expectedResp)
	client := createTestClientWithMock(t, mock)

	resp, err := client.KMSGetKey(context.Background(), "test-key")

	require.NoError(t, err)
	assert.Equal(t, "test-key", resp.Key.KeyID)
}

func TestRESTClientMock_KMSGetKey_EmptyKeyID(t *testing.T) {
	mock := newMockRoundTripper(http.StatusOK, nil)
	client := createTestClientWithMock(t, mock)

	resp, err := client.KMSGetKey(context.Background(), "")

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestRESTClientMock_KMSListKeys_Success(t *testing.T) {
	expectedResp := &ListKeysRESTResponse{
		Keys: []*KeyHandleResponse{
			{KeyID: "key-1", Status: "enabled"},
			{KeyID: "key-2", Status: "enabled"},
		},
	}

	mock := newMockRoundTripper(http.StatusOK, expectedResp)
	client := createTestClientWithMock(t, mock)

	resp, err := client.KMSListKeys(context.Background(), 100, "")

	require.NoError(t, err)
	assert.Len(t, resp.Keys, 2)
}

func TestRESTClientMock_KMSListKeys_WithCursor(t *testing.T) {
	expectedResp := &ListKeysRESTResponse{
		Keys:       []*KeyHandleResponse{},
		NextCursor: "",
	}

	mock := newMockRoundTripper(http.StatusOK, expectedResp)
	client := createTestClientWithMock(t, mock)

	resp, err := client.KMSListKeys(context.Background(), 50, "next-cursor")

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Contains(t, mock.requestLog[0].URL.RawQuery, "cursor=next-cursor")
}

func TestRESTClientMock_KMSRotateKey_Success(t *testing.T) {
	expectedResp := &RotateKeyRESTResponse{
		KeyID:      "test-key",
		NewVersion: 2,
	}

	mock := newMockRoundTripper(http.StatusOK, expectedResp)
	client := createTestClientWithMock(t, mock)

	resp, err := client.KMSRotateKey(context.Background(), "test-key")

	require.NoError(t, err)
	assert.Equal(t, 2, resp.NewVersion)
}

func TestRESTClientMock_KMSRotateKey_EmptyKeyID(t *testing.T) {
	mock := newMockRoundTripper(http.StatusOK, nil)
	client := createTestClientWithMock(t, mock)

	resp, err := client.KMSRotateKey(context.Background(), "")

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestRESTClientMock_KMSDeleteKey_Success(t *testing.T) {
	expectedResp := &DeleteKeyRESTResponse{
		KeyID:  "test-key",
		Status: "deleted",
	}

	mock := newMockRoundTripper(http.StatusOK, expectedResp)
	client := createTestClientWithMock(t, mock)

	resp, err := client.KMSDeleteKey(context.Background(), "test-key")

	require.NoError(t, err)
	assert.Equal(t, "deleted", resp.Status)
}

func TestRESTClientMock_KMSDeleteKey_EmptyKeyID(t *testing.T) {
	mock := newMockRoundTripper(http.StatusOK, nil)
	client := createTestClientWithMock(t, mock)

	resp, err := client.KMSDeleteKey(context.Background(), "")

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestRESTClientMock_KMSListKeyVersions_Success(t *testing.T) {
	expectedResp := &ListKeyVersionsRESTResponse{
		Versions: []*KeyVersionResponse{
			{Version: 1, Status: "enabled"},
			{Version: 2, Status: "enabled"},
		},
	}

	mock := newMockRoundTripper(http.StatusOK, expectedResp)
	client := createTestClientWithMock(t, mock)

	resp, err := client.KMSListKeyVersions(context.Background(), "test-key")

	require.NoError(t, err)
	assert.Len(t, resp.Versions, 2)
}

func TestRESTClientMock_KMSListKeyVersions_EmptyKeyID(t *testing.T) {
	mock := newMockRoundTripper(http.StatusOK, nil)
	client := createTestClientWithMock(t, mock)

	resp, err := client.KMSListKeyVersions(context.Background(), "")

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestRESTClientMock_KMSUpdateKeyStatus_Success(t *testing.T) {
	expectedResp := &UpdateKeyStatusRESTResponse{
		KeyID:     "test-key",
		Status:    "disabled",
		Timestamp: time.Now().Unix(),
	}

	mock := newMockRoundTripper(http.StatusOK, expectedResp)
	client := createTestClientWithMock(t, mock)

	resp, err := client.KMSUpdateKeyStatus(context.Background(), "test-key", &UpdateKeyStatusRESTRequest{
		Status: "disabled",
	})

	require.NoError(t, err)
	assert.Equal(t, "disabled", resp.Status)
}

func TestRESTClientMock_KMSUpdateKeyStatus_EmptyKeyID(t *testing.T) {
	mock := newMockRoundTripper(http.StatusOK, nil)
	client := createTestClientWithMock(t, mock)

	resp, err := client.KMSUpdateKeyStatus(context.Background(), "", &UpdateKeyStatusRESTRequest{})

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestRESTClientMock_KMSUpdateKeyStatus_NilRequest(t *testing.T) {
	mock := newMockRoundTripper(http.StatusOK, nil)
	client := createTestClientWithMock(t, mock)

	resp, err := client.KMSUpdateKeyStatus(context.Background(), "test-key", nil)

	require.Error(t, err)
	assert.Nil(t, resp)
}

// =============================================================================
// DEK Client Method Tests
// =============================================================================

func TestRESTClientMock_KMSCreateDEK_Success(t *testing.T) {
	expectedResp := &service.CreateDEKResponse{
		DEK: &service.DEKMetadata{
			Path:    "/app/db",
			Version: 1,
		},
		Timestamp: time.Now(),
	}

	mock := newMockRoundTripper(http.StatusOK, expectedResp)
	client := createTestClientWithMock(t, mock)

	resp, err := client.KMSCreateDEK(context.Background(), &service.CreateDEKRequest{
		Path: "/app/db",
	})

	require.NoError(t, err)
	require.NotNil(t, resp.DEK)
	assert.Equal(t, "/app/db", resp.DEK.Path)
}

func TestRESTClientMock_KMSCreateDEK_NilRequest(t *testing.T) {
	mock := newMockRoundTripper(http.StatusOK, nil)
	client := createTestClientWithMock(t, mock)

	resp, err := client.KMSCreateDEK(context.Background(), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestRESTClientMock_KMSGetDEK_Success(t *testing.T) {
	expectedResp := &service.GetDEKResponse{
		DEK: &service.DEKMetadata{
			Path:    "/app/db",
			Version: 1,
		},
		Timestamp: time.Now(),
	}

	mock := newMockRoundTripper(http.StatusOK, expectedResp)
	client := createTestClientWithMock(t, mock)

	resp, err := client.KMSGetDEK(context.Background(), &service.GetDEKRequest{
		Path: "/app/db",
	})

	require.NoError(t, err)
	require.NotNil(t, resp.DEK)
	assert.Equal(t, "/app/db", resp.DEK.Path)
}

func TestRESTClientMock_KMSGetDEK_NilRequest(t *testing.T) {
	mock := newMockRoundTripper(http.StatusOK, nil)
	client := createTestClientWithMock(t, mock)

	resp, err := client.KMSGetDEK(context.Background(), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestRESTClientMock_KMSListDEKs_Success(t *testing.T) {
	expectedResp := &service.ListDEKsResponse{
		DEKs: []*service.DEKMetadata{
			{Path: "/app/db1"},
			{Path: "/app/db2"},
		},
	}

	mock := newMockRoundTripper(http.StatusOK, expectedResp)
	client := createTestClientWithMock(t, mock)

	resp, err := client.KMSListDEKs(context.Background(), &service.ListDEKsRequest{
		Prefix: "/app",
	})

	require.NoError(t, err)
	assert.Len(t, resp.DEKs, 2)
}

func TestRESTClientMock_KMSListDEKs_NilRequest(t *testing.T) {
	expectedResp := &service.ListDEKsResponse{
		DEKs: []*service.DEKMetadata{},
	}

	mock := newMockRoundTripper(http.StatusOK, expectedResp)
	client := createTestClientWithMock(t, mock)

	resp, err := client.KMSListDEKs(context.Background(), nil)

	require.NoError(t, err)
	assert.NotNil(t, resp)
}

func TestRESTClientMock_KMSRotateDEK_Success(t *testing.T) {
	expectedResp := &service.RotateDEKResponse{
		Path:       "/app/db",
		NewVersion: 2,
	}

	mock := newMockRoundTripper(http.StatusOK, expectedResp)
	client := createTestClientWithMock(t, mock)

	resp, err := client.KMSRotateDEK(context.Background(), &service.RotateDEKRequest{
		Path: "/app/db",
	})

	require.NoError(t, err)
	assert.Equal(t, uint64(2), resp.NewVersion)
}

func TestRESTClientMock_KMSRotateDEK_NilRequest(t *testing.T) {
	mock := newMockRoundTripper(http.StatusOK, nil)
	client := createTestClientWithMock(t, mock)

	resp, err := client.KMSRotateDEK(context.Background(), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestRESTClientMock_KMSDeleteDEK_Success(t *testing.T) {
	expectedResp := &service.DeleteDEKResponse{
		Deleted: true,
	}

	mock := newMockRoundTripper(http.StatusOK, expectedResp)
	client := createTestClientWithMock(t, mock)

	resp, err := client.KMSDeleteDEK(context.Background(), &service.DeleteDEKRequest{
		Path: "/app/db",
	})

	require.NoError(t, err)
	assert.True(t, resp.Deleted)
}

func TestRESTClientMock_KMSDeleteDEK_NilRequest(t *testing.T) {
	mock := newMockRoundTripper(http.StatusOK, nil)
	client := createTestClientWithMock(t, mock)

	resp, err := client.KMSDeleteDEK(context.Background(), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestRESTClientMock_KMSGetDEKStatus_Success(t *testing.T) {
	expectedResp := &service.GetDEKStatusResponse{
		DEK: &service.DEKMetadata{
			Path:    "/app/db",
			Version: 1,
		},
		Timestamp: time.Now(),
	}

	mock := newMockRoundTripper(http.StatusOK, expectedResp)
	client := createTestClientWithMock(t, mock)

	resp, err := client.KMSGetDEKStatus(context.Background(), &service.GetDEKStatusRequest{
		Path: "/app/db",
	})

	require.NoError(t, err)
	require.NotNil(t, resp.DEK)
	assert.Equal(t, "/app/db", resp.DEK.Path)
}

func TestRESTClientMock_KMSGetDEKStatus_NilRequest(t *testing.T) {
	mock := newMockRoundTripper(http.StatusOK, nil)
	client := createTestClientWithMock(t, mock)

	resp, err := client.KMSGetDEKStatus(context.Background(), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestRESTClientMock_KMSSetDefaultDEK_Success(t *testing.T) {
	expectedResp := &service.SetDefaultDEKResponse{
		Path:            "/app/db",
		PreviousDefault: "/app/old",
		Timestamp:       time.Now(),
	}

	mock := newMockRoundTripper(http.StatusOK, expectedResp)
	client := createTestClientWithMock(t, mock)

	resp, err := client.KMSSetDefaultDEK(context.Background(), &service.SetDefaultDEKRequest{
		Path: "/app/db",
	})

	require.NoError(t, err)
	assert.Equal(t, "/app/db", resp.Path)
}

func TestRESTClientMock_KMSSetDefaultDEK_NilRequest(t *testing.T) {
	mock := newMockRoundTripper(http.StatusOK, nil)
	client := createTestClientWithMock(t, mock)

	resp, err := client.KMSSetDefaultDEK(context.Background(), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestRESTClientMock_KMSSetDEKSafety_Success(t *testing.T) {
	expectedResp := &service.SetDEKSafetyResponse{
		Path: "/app/db",
		Config: &service.DEKSafetyConfig{
			MaxOperations:  1000000,
			MaxBytes:       1073741824,
			WarningPercent: 80,
		},
		Timestamp: time.Now(),
	}

	mock := newMockRoundTripper(http.StatusOK, expectedResp)
	client := createTestClientWithMock(t, mock)

	resp, err := client.KMSSetDEKSafety(context.Background(), &service.SetDEKSafetyRequest{
		Path: "/app/db",
	})

	require.NoError(t, err)
	assert.Equal(t, "/app/db", resp.Path)
	require.NotNil(t, resp.Config)
}

func TestRESTClientMock_KMSSetDEKSafety_NilRequest(t *testing.T) {
	mock := newMockRoundTripper(http.StatusOK, nil)
	client := createTestClientWithMock(t, mock)

	resp, err := client.KMSSetDEKSafety(context.Background(), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestRESTClientMock_KMSClearDEKSafety_Success(t *testing.T) {
	expectedResp := &service.ClearDEKSafetyResponse{
		Path:      "/app/db",
		Cleared:   true,
		Timestamp: time.Now(),
	}

	mock := newMockRoundTripper(http.StatusOK, expectedResp)
	client := createTestClientWithMock(t, mock)

	resp, err := client.KMSClearDEKSafety(context.Background(), &service.ClearDEKSafetyRequest{
		Path: "/app/db",
	})

	require.NoError(t, err)
	assert.True(t, resp.Cleared)
}

func TestRESTClientMock_KMSClearDEKSafety_NilRequest(t *testing.T) {
	mock := newMockRoundTripper(http.StatusOK, nil)
	client := createTestClientWithMock(t, mock)

	resp, err := client.KMSClearDEKSafety(context.Background(), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
}

// =============================================================================
// Seal Client Method Tests
// =============================================================================

func TestRESTClientMock_SealStatus_Success(t *testing.T) {
	expectedResp := &service.SealStatusResponse{
		Sealed:      false,
		Threshold:   3,
		TotalShares: 5,
	}

	mock := newMockRoundTripper(http.StatusOK, expectedResp)
	client := createTestClientWithMock(t, mock)

	resp, err := client.SealStatus(context.Background())

	require.NoError(t, err)
	assert.False(t, resp.Sealed)
}

func TestRESTClientMock_SealInit_Success(t *testing.T) {
	expectedResp := &service.SealInitResponse{
		Shares:    [][]byte{[]byte("key1"), []byte("key2"), []byte("key3")},
		RootToken: "root-token",
	}

	mock := newMockRoundTripper(http.StatusOK, expectedResp)
	client := createTestClientWithMock(t, mock)

	resp, err := client.SealInit(context.Background(), &service.SealInitRequest{
		TotalShares: 5,
		Threshold:   3,
	})

	require.NoError(t, err)
	assert.Len(t, resp.Shares, 3)
}

func TestRESTClientMock_SealInit_NilRequest(t *testing.T) {
	mock := newMockRoundTripper(http.StatusOK, nil)
	client := createTestClientWithMock(t, mock)

	resp, err := client.SealInit(context.Background(), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestRESTClientMock_Unseal_Success(t *testing.T) {
	expectedResp := &service.UnsealResponse{
		Sealed:         false,
		SharesProvided: 3,
	}

	mock := newMockRoundTripper(http.StatusOK, expectedResp)
	client := createTestClientWithMock(t, mock)

	resp, err := client.Unseal(context.Background(), &service.UnsealRequest{
		Share: []byte("unseal-key"),
	})

	require.NoError(t, err)
	assert.False(t, resp.Sealed)
}

func TestRESTClientMock_Unseal_NilRequest(t *testing.T) {
	mock := newMockRoundTripper(http.StatusOK, nil)
	client := createTestClientWithMock(t, mock)

	resp, err := client.Unseal(context.Background(), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestRESTClientMock_Seal_Success(t *testing.T) {
	mock := newMockRoundTripper(http.StatusOK, nil)
	client := createTestClientWithMock(t, mock)

	err := client.Seal(context.Background())

	require.NoError(t, err)
}

func TestRESTClientMock_Rekey_Success(t *testing.T) {
	expectedResp := &service.RekeyResponse{
		Shares: [][]byte{[]byte("new-key1"), []byte("new-key2")},
	}

	mock := newMockRoundTripper(http.StatusOK, expectedResp)
	client := createTestClientWithMock(t, mock)

	resp, err := client.Rekey(context.Background(), &service.RekeyRequest{
		NewTotalShares: 5,
		NewThreshold:   3,
	})

	require.NoError(t, err)
	assert.Len(t, resp.Shares, 2)
}

func TestRESTClientMock_Rekey_NilRequest(t *testing.T) {
	mock := newMockRoundTripper(http.StatusOK, nil)
	client := createTestClientWithMock(t, mock)

	resp, err := client.Rekey(context.Background(), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
}

// =============================================================================
// Secrets Client Method Tests
// =============================================================================

func TestRESTClientMock_StoreSecret_Success(t *testing.T) {
	expectedResp := &service.StoreSecretResponse{
		Version: 1,
	}

	mock := newMockRoundTripper(http.StatusOK, expectedResp)
	client := createTestClientWithMock(t, mock)

	resp, err := client.StoreSecret(context.Background(), &service.StoreSecretRequest{
		Name: "db-password",
		Data: []byte("secret-data"),
	})

	require.NoError(t, err)
	assert.Equal(t, uint64(1), resp.Version)
}

func TestRESTClientMock_StoreSecret_NilRequest(t *testing.T) {
	mock := newMockRoundTripper(http.StatusOK, nil)
	client := createTestClientWithMock(t, mock)

	resp, err := client.StoreSecret(context.Background(), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestRESTClientMock_GetSecret_Success(t *testing.T) {
	expectedResp := &service.GetSecretResponse{
		SecretID: "db-password",
		Version:  1,
		Data:     []byte("secret-data"),
	}

	mock := newMockRoundTripper(http.StatusOK, expectedResp)
	client := createTestClientWithMock(t, mock)

	resp, err := client.GetSecret(context.Background(), &service.GetSecretRequest{
		SecretID: "db-password",
	})

	require.NoError(t, err)
	assert.Equal(t, "db-password", resp.SecretID)
}

func TestRESTClientMock_GetSecret_NilRequest(t *testing.T) {
	mock := newMockRoundTripper(http.StatusOK, nil)
	client := createTestClientWithMock(t, mock)

	resp, err := client.GetSecret(context.Background(), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestRESTClientMock_GetSecret_WithVersion(t *testing.T) {
	expectedResp := &service.GetSecretResponse{
		SecretID: "db-password",
		Version:  2,
	}

	mock := newMockRoundTripper(http.StatusOK, expectedResp)
	client := createTestClientWithMock(t, mock)

	resp, err := client.GetSecret(context.Background(), &service.GetSecretRequest{
		SecretID: "db-password",
		Version:  2,
	})

	require.NoError(t, err)
	assert.Equal(t, uint64(2), resp.Version)
	assert.Contains(t, mock.requestLog[0].URL.RawQuery, "version=2")
}

func TestRESTClientMock_ListSecrets_Success(t *testing.T) {
	expectedResp := &service.ListSecretsResponse{
		Secrets: []*service.Secret{
			{SecretID: "secret1"},
			{SecretID: "secret2"},
		},
	}

	mock := newMockRoundTripper(http.StatusOK, expectedResp)
	client := createTestClientWithMock(t, mock)

	resp, err := client.ListSecrets(context.Background(), &service.ListSecretsRequest{
		Limit: 100,
	})

	require.NoError(t, err)
	assert.Len(t, resp.Secrets, 2)
}

func TestRESTClientMock_ListSecrets_NilRequest(t *testing.T) {
	expectedResp := &service.ListSecretsResponse{
		Secrets: []*service.Secret{},
	}

	mock := newMockRoundTripper(http.StatusOK, expectedResp)
	client := createTestClientWithMock(t, mock)

	resp, err := client.ListSecrets(context.Background(), nil)

	require.NoError(t, err)
	assert.NotNil(t, resp)
}

func TestRESTClientMock_DeleteSecret_Success(t *testing.T) {
	expectedResp := &service.DeleteSecretResponse{
		Deleted: true,
	}

	mock := newMockRoundTripper(http.StatusOK, expectedResp)
	client := createTestClientWithMock(t, mock)

	resp, err := client.DeleteSecret(context.Background(), &service.DeleteSecretRequest{
		SecretID: "db-password",
	})

	require.NoError(t, err)
	assert.True(t, resp.Deleted)
}

func TestRESTClientMock_DeleteSecret_NilRequest(t *testing.T) {
	mock := newMockRoundTripper(http.StatusOK, nil)
	client := createTestClientWithMock(t, mock)

	resp, err := client.DeleteSecret(context.Background(), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestRESTClientMock_DeleteSecret_PermanentDelete(t *testing.T) {
	expectedResp := &service.DeleteSecretResponse{
		Deleted: true,
	}

	mock := newMockRoundTripper(http.StatusOK, expectedResp)
	client := createTestClientWithMock(t, mock)

	resp, err := client.DeleteSecret(context.Background(), &service.DeleteSecretRequest{
		SecretID:          "db-password",
		PermanentlyDelete: true,
	})

	require.NoError(t, err)
	assert.True(t, resp.Deleted)
	assert.Contains(t, mock.requestLog[0].URL.RawQuery, "destroy=true")
}

func TestRESTClientMock_ListSecretVersions_Success(t *testing.T) {
	expectedResp := &service.ListSecretVersionsResponse{
		Versions: []*service.SecretVersion{
			{Version: 1},
			{Version: 2},
		},
	}

	mock := newMockRoundTripper(http.StatusOK, expectedResp)
	client := createTestClientWithMock(t, mock)

	resp, err := client.ListSecretVersions(context.Background(), &service.ListSecretVersionsRequest{
		SecretID: "db-password",
	})

	require.NoError(t, err)
	assert.Len(t, resp.Versions, 2)
}

func TestRESTClientMock_ListSecretVersions_NilRequest(t *testing.T) {
	mock := newMockRoundTripper(http.StatusOK, nil)
	client := createTestClientWithMock(t, mock)

	resp, err := client.ListSecretVersions(context.Background(), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestRESTClientMock_RollbackSecret_Success(t *testing.T) {
	expectedResp := &service.RollbackSecretResponse{
		NewVersion: 3,
	}

	mock := newMockRoundTripper(http.StatusOK, expectedResp)
	client := createTestClientWithMock(t, mock)

	resp, err := client.RollbackSecret(context.Background(), &service.RollbackSecretRequest{
		SecretID:      "db-password",
		TargetVersion: 1,
	})

	require.NoError(t, err)
	assert.Equal(t, uint64(3), resp.NewVersion)
}

func TestRESTClientMock_RollbackSecret_NilRequest(t *testing.T) {
	mock := newMockRoundTripper(http.StatusOK, nil)
	client := createTestClientWithMock(t, mock)

	resp, err := client.RollbackSecret(context.Background(), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestRESTClientMock_RotateSecret_Success(t *testing.T) {
	expectedResp := &service.RotateSecretResponse{
		SecretID:   "db-password",
		NewVersion: 2,
	}

	mock := newMockRoundTripper(http.StatusOK, expectedResp)
	client := createTestClientWithMock(t, mock)

	resp, err := client.RotateSecret(context.Background(), &service.RotateSecretRequest{
		SecretID: "db-password",
	})

	require.NoError(t, err)
	assert.Equal(t, uint64(2), resp.NewVersion)
}

func TestRESTClientMock_RotateSecret_NilRequest(t *testing.T) {
	mock := newMockRoundTripper(http.StatusOK, nil)
	client := createTestClientWithMock(t, mock)

	resp, err := client.RotateSecret(context.Background(), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestRESTClientMock_UpdateSecretStatus_Success(t *testing.T) {
	expectedResp := &service.UpdateSecretStatusResponse{
		SecretID:  "db-password",
		OldStatus: service.SecretStatusEnabled,
		NewStatus: service.SecretStatusDisabled,
		Timestamp: time.Now(),
	}

	mock := newMockRoundTripper(http.StatusOK, expectedResp)
	client := createTestClientWithMock(t, mock)

	resp, err := client.UpdateSecretStatus(context.Background(), &service.UpdateSecretStatusRequest{
		SecretID: "db-password",
		Status:   service.SecretStatusDisabled,
	})

	require.NoError(t, err)
	assert.Equal(t, service.SecretStatusDisabled, resp.NewStatus)
}

func TestRESTClientMock_UpdateSecretStatus_NilRequest(t *testing.T) {
	mock := newMockRoundTripper(http.StatusOK, nil)
	client := createTestClientWithMock(t, mock)

	resp, err := client.UpdateSecretStatus(context.Background(), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestRESTClientMock_GetSecretsByPrefix_Success(t *testing.T) {
	expectedResp := &service.GetSecretsByPrefixResponse{
		Secrets: map[string]map[string][]byte{
			"app/secret1": {"key": []byte("value1")},
			"app/secret2": {"key": []byte("value2")},
		},
	}

	mock := newMockRoundTripper(http.StatusOK, expectedResp)
	client := createTestClientWithMock(t, mock)

	resp, err := client.GetSecretsByPrefix(context.Background(), &service.GetSecretsByPrefixRequest{
		Prefix: "app/",
	})

	require.NoError(t, err)
	assert.Len(t, resp.Secrets, 2)
}

func TestRESTClientMock_GetSecretsByPrefix_NilRequest(t *testing.T) {
	mock := newMockRoundTripper(http.StatusOK, nil)
	client := createTestClientWithMock(t, mock)

	resp, err := client.GetSecretsByPrefix(context.Background(), nil)

	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestRESTClientMock_GetSecretsByPrefix_EmptyPrefix(t *testing.T) {
	mock := newMockRoundTripper(http.StatusOK, nil)
	client := createTestClientWithMock(t, mock)

	resp, err := client.GetSecretsByPrefix(context.Background(), &service.GetSecretsByPrefixRequest{
		Prefix: "",
	})

	require.Error(t, err)
	assert.Nil(t, resp)
}

// =============================================================================
// Additional Client Tests
// =============================================================================

func TestRESTClientMock_Ping_Success(t *testing.T) {
	expectedResp := &HealthResponse{
		Status: "healthy",
	}

	mock := newMockRoundTripper(http.StatusOK, expectedResp)
	client := createTestClientWithMock(t, mock)

	err := client.Ping(context.Background())

	require.NoError(t, err)
}

func TestRESTClientMock_Ping_Unhealthy(t *testing.T) {
	expectedResp := &HealthResponse{
		Status: "unhealthy",
	}

	mock := newMockRoundTripper(http.StatusOK, expectedResp)
	client := createTestClientWithMock(t, mock)

	err := client.Ping(context.Background())

	require.Error(t, err)
}

func TestRESTClientMock_ClusterStatus_Success(t *testing.T) {
	expectedResp := &ClusterStatusResponse{
		Healthy:    true,
		LeaderID:   "1",
		TotalNodes: 3,
	}

	mock := newMockRoundTripper(http.StatusOK, expectedResp)
	client := createTestClientWithMock(t, mock)

	resp, err := client.ClusterStatus(context.Background())

	require.NoError(t, err)
	assert.True(t, resp.Healthy)
	assert.Equal(t, uint64(1), resp.LeaderID)
}

// =============================================================================
// HTTP Status Code Mapping Tests
// =============================================================================

func TestRESTClientMock_ErrorResponse_StatusCodeMapping(t *testing.T) {
	tests := []struct {
		name         string
		statusCode   int
		expectedCode errors.ErrorCode
	}{
		{"400 Bad Request", http.StatusBadRequest, errors.ErrInvalidInput},
		{"401 Unauthorized", http.StatusUnauthorized, errors.ErrUnauthorized},
		{"403 Forbidden", http.StatusForbidden, errors.ErrForbidden},
		{"404 Not Found", http.StatusNotFound, errors.ErrNotFound},
		{"409 Conflict", http.StatusConflict, errors.ErrAlreadyExists},
		{"408 Request Timeout", http.StatusRequestTimeout, errors.ErrTimeout},
		{"429 Too Many Requests", http.StatusTooManyRequests, errors.ErrResourceExhausted},
		{"503 Service Unavailable", http.StatusServiceUnavailable, errors.ErrLeaderNotAvailable},
		{"501 Not Implemented", http.StatusNotImplemented, errors.ErrNotImplemented},
		{"500 Internal Server Error", http.StatusInternalServerError, errors.ErrInternal},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			errResp := &ErrorResponse{
				Error:     "test error",
				Code:      tc.expectedCode.String(),
				Timestamp: time.Now(),
			}

			mock := newMockRoundTripper(tc.statusCode, errResp)
			client := createTestClientWithMock(t, mock)

			resp, err := client.KMSGetKey(context.Background(), "test-key")

			require.Error(t, err)
			assert.Nil(t, resp)

			var qbErr *errors.QBError
			require.ErrorAs(t, err, &qbErr)
			assert.Equal(t, tc.expectedCode, qbErr.Code)
		})
	}
}

// =============================================================================
// KMS Validation Tests
// =============================================================================

func TestRESTClientMock_KMSExportKey_Validation(t *testing.T) {
	t.Run("nil request", func(t *testing.T) {
		mock := newMockRoundTripper(http.StatusOK, nil)
		client := createTestClientWithMock(t, mock)

		resp, err := client.KMSExportKey(context.Background(), nil)

		require.Error(t, err)
		assert.Nil(t, resp)

		var qbErr *errors.QBError
		require.ErrorAs(t, err, &qbErr)
		assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
	})

	t.Run("empty key ID", func(t *testing.T) {
		mock := newMockRoundTripper(http.StatusOK, nil)
		client := createTestClientWithMock(t, mock)

		resp, err := client.KMSExportKey(context.Background(), &service.ExportKeyRequest{})

		require.Error(t, err)
		assert.Nil(t, resp)

		var qbErr *errors.QBError
		require.ErrorAs(t, err, &qbErr)
		assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
	})
}

func TestRESTClientMock_KMSUnwrapKey_Validation(t *testing.T) {
	t.Run("nil request", func(t *testing.T) {
		mock := newMockRoundTripper(http.StatusOK, nil)
		client := createTestClientWithMock(t, mock)

		resp, err := client.KMSUnwrapKey(context.Background(), nil)

		require.Error(t, err)
		assert.Nil(t, resp)

		var qbErr *errors.QBError
		require.ErrorAs(t, err, &qbErr)
		assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
	})
}

func TestRESTClientMock_KMSSetKeySafety_Validation(t *testing.T) {
	t.Run("nil request", func(t *testing.T) {
		mock := newMockRoundTripper(http.StatusOK, nil)
		client := createTestClientWithMock(t, mock)

		resp, err := client.KMSSetKeySafety(context.Background(), nil)

		require.Error(t, err)
		assert.Nil(t, resp)

		var qbErr *errors.QBError
		require.ErrorAs(t, err, &qbErr)
		assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
	})

	t.Run("empty key ID", func(t *testing.T) {
		mock := newMockRoundTripper(http.StatusOK, nil)
		client := createTestClientWithMock(t, mock)

		resp, err := client.KMSSetKeySafety(context.Background(), &service.SetKeySafetyRequest{})

		require.Error(t, err)
		assert.Nil(t, resp)

		var qbErr *errors.QBError
		require.ErrorAs(t, err, &qbErr)
		assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
	})
}

// =============================================================================
// Client Option Tests
// =============================================================================

func TestClientOption_WithTLSConfig_NilConfig_MockTest(t *testing.T) {
	log, err := logger.New(logger.LogConfig{Level: "debug", Format: "console"})
	require.NoError(t, err)

	_, err = NewRESTClient("https://test-server:8443", log, WithTLSConfig(nil))

	require.Error(t, err)
	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrConfiguration, qbErr.Code)
}

// =============================================================================
// Server-Based Client Integration Tests
// =============================================================================

func TestRESTClientMock_WithRealServer_TimeSeries(t *testing.T) {
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json"})
	require.NoError(t, err)

	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/timeseries/write" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success":   true,
				"timestamp": time.Now().UnixNano(),
			})
			return
		}
		if r.URL.Path == "/api/v1/timeseries/query" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"points": []map[string]interface{}{
					{"timestamp": time.Now().UnixNano(), "value": 42.0},
				},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client, err := NewRESTClient(server.URL, log)
	require.NoError(t, err)
	defer client.Close()

	// Test time series operations
	ctx := context.Background()

	// Test TSWritePoint - this tests the actual HTTP round trip
	writeResp, err := client.TSWritePoint(ctx, nil)
	require.Error(t, err) // nil request should fail
	assert.Nil(t, writeResp)

	// Test TSQuery - this tests the actual HTTP round trip
	queryResp, err := client.TSQuery(ctx, nil)
	require.Error(t, err) // nil request should fail
	assert.Nil(t, queryResp)
}
