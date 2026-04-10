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
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// TestKMSDecryptSuccess tests KMSDecrypt success path.
func TestKMSDecryptSuccess(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/api/v1/kms/decrypt/")
		json.NewEncoder(w).Encode(service.DecryptResponse{
			Plaintext: []byte("decrypted-data"),
		})
	}))
	defer server.Close()

	client, _ := NewRESTClient(server.URL, log)
	resp, err := client.KMSDecrypt(ctx, &service.DecryptRequest{
		Ciphertext: []byte("encrypted-data"),
		KeyID:      "key-123",
	})
	require.NoError(t, err)
	assert.Equal(t, []byte("decrypted-data"), resp.Plaintext)
}

// TestKMSEncryptSuccess tests KMSEncrypt success path.
func TestKMSEncryptSuccess(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/api/v1/kms/encrypt/")
		json.NewEncoder(w).Encode(service.EncryptResponse{
			Ciphertext: []byte("encrypted-data"),
		})
	}))
	defer server.Close()

	client, _ := NewRESTClient(server.URL, log)
	resp, err := client.KMSEncrypt(ctx, &service.EncryptRequest{
		Plaintext: []byte("plaintext-data"),
		KeyID:     "key-123",
	})
	require.NoError(t, err)
	assert.Equal(t, []byte("encrypted-data"), resp.Ciphertext)
}

// TestKMSGetKeySuccess tests KMSGetKey success path.
func TestKMSGetKeySuccess(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/api/v1/kms/keys/")
		json.NewEncoder(w).Encode(GetKeyRESTResponse{
			Key: &KeyHandleResponse{
				KeyID:     "key-123",
				Status:    "enabled",
				Algorithm: "AES256",
				Version:   1,
			},
			Timestamp: time.Now().UnixNano(),
		})
	}))
	defer server.Close()

	client, _ := NewRESTClient(server.URL, log)
	resp, err := client.KMSGetKey(ctx, "key-123")
	require.NoError(t, err)
	assert.NotNil(t, resp.Key)
	assert.Equal(t, "key-123", resp.Key.KeyID)
	assert.Equal(t, "enabled", resp.Key.Status)
}

// TestKMSRotateKeySuccess tests KMSRotateKey success path.
func TestKMSRotateKeySuccess(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/api/v1/kms/keys/")
		assert.Contains(t, r.URL.Path, "/rotate")
		json.NewEncoder(w).Encode(RotateKeyRESTResponse{
			KeyID:      "key-123",
			NewVersion: 2,
		})
	}))
	defer server.Close()

	client, _ := NewRESTClient(server.URL, log)
	resp, err := client.KMSRotateKey(ctx, "key-123")
	require.NoError(t, err)
	assert.Equal(t, "key-123", resp.KeyID)
	assert.Equal(t, 2, resp.NewVersion)
}

// TestKMSDeleteKeySuccess tests KMSDeleteKey success path.
func TestKMSDeleteKeySuccess(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/api/v1/kms/keys/")
		assert.Equal(t, http.MethodDelete, r.Method)
		json.NewEncoder(w).Encode(DeleteKeyRESTResponse{
			KeyID:     "key-123",
			Status:    "deleted",
			Timestamp: time.Now().UnixNano(),
		})
	}))
	defer server.Close()

	client, _ := NewRESTClient(server.URL, log)
	resp, err := client.KMSDeleteKey(ctx, "key-123")
	require.NoError(t, err)
	assert.Equal(t, "key-123", resp.KeyID)
	assert.Equal(t, "deleted", resp.Status)
}

// TestStoreSecretSuccess tests StoreSecret success path.
func TestStoreSecretSuccess(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/api/v1/secrets/")
		json.NewEncoder(w).Encode(service.StoreSecretResponse{
			SecretID:  "app/db/password",
			Version:   1,
			Timestamp: time.Now(),
		})
	}))
	defer server.Close()

	client, _ := NewRESTClient(server.URL, log)
	resp, err := client.StoreSecret(ctx, &service.StoreSecretRequest{
		Name: "app/db/password",
		Data: []byte("secret-password"),
	})
	require.NoError(t, err)
	assert.Equal(t, "app/db/password", resp.SecretID)
	assert.Equal(t, uint64(1), resp.Version)
}

// TestGetSecretSuccess tests GetSecret success path.
func TestGetSecretSuccess(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/api/v1/secrets/")
		json.NewEncoder(w).Encode(service.GetSecretResponse{
			SecretID:  "app/db/password",
			Version:   1,
			Data:      []byte("secret-password"),
			Timestamp: time.Now(),
		})
	}))
	defer server.Close()

	client, _ := NewRESTClient(server.URL, log)
	resp, err := client.GetSecret(ctx, &service.GetSecretRequest{SecretID: "app/db/password"})
	require.NoError(t, err)
	assert.Equal(t, "app/db/password", resp.SecretID)
	assert.Equal(t, []byte("secret-password"), resp.Data)
}

// TestDeleteSecretSuccess tests DeleteSecret success path.
func TestDeleteSecretSuccess(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/api/v1/secrets/")
		assert.Equal(t, http.MethodDelete, r.Method)
		json.NewEncoder(w).Encode(service.DeleteSecretResponse{
			SecretID:  "app/db/password",
			Deleted:   true,
			Timestamp: time.Now(),
		})
	}))
	defer server.Close()

	client, _ := NewRESTClient(server.URL, log)
	resp, err := client.DeleteSecret(ctx, &service.DeleteSecretRequest{SecretID: "app/db/password"})
	require.NoError(t, err)
	assert.Equal(t, "app/db/password", resp.SecretID)
	assert.True(t, resp.Deleted)
}

// TestGetSecretsByPrefixSuccess tests GetSecretsByPrefix success path.
func TestGetSecretsByPrefixSuccess(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/api/v1/secrets/prefix/")
		json.NewEncoder(w).Encode(service.GetSecretsByPrefixResponse{
			Secrets: map[string]map[string][]byte{
				"app/db/password": {"data": []byte("secret")},
			},
		})
	}))
	defer server.Close()

	client, _ := NewRESTClient(server.URL, log)
	resp, err := client.GetSecretsByPrefix(ctx, &service.GetSecretsByPrefixRequest{Prefix: "app/"})
	require.NoError(t, err)
	assert.NotNil(t, resp.Secrets)
}

// TestDoRequestServerError tests doRequest with server error.
func TestDoRequestServerError(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{
			Code:  "internal",
			Error: "server error",
		})
	}))
	defer server.Close()

	client, _ := NewRESTClient(server.URL, log)
	_, err := client.KMSGetKey(ctx, "key-123")
	require.Error(t, err)
	var dragonErr *errors.DragonError
	require.ErrorAs(t, err, &dragonErr)
}

// TestDoRequestNotFound tests doRequest with 404 response.
func TestDoRequestNotFound(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(ErrorResponse{
			Code:  "not_found",
			Error: "key not found",
		})
	}))
	defer server.Close()

	client, _ := NewRESTClient(server.URL, log)
	_, err := client.KMSGetKey(ctx, "nonexistent-key")
	require.Error(t, err)
	var dragonErr *errors.DragonError
	require.ErrorAs(t, err, &dragonErr)
	assert.Equal(t, errors.ErrNotFound, dragonErr.Code)
}

// TestDoRequestBadRequest tests doRequest with 400 response.
func TestDoRequestBadRequest(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{
			Code:  "invalid_input",
			Error: "invalid key format",
		})
	}))
	defer server.Close()

	client, _ := NewRESTClient(server.URL, log)
	_, err := client.KMSEncrypt(ctx, &service.EncryptRequest{Plaintext: []byte("data"), KeyID: "key"})
	require.Error(t, err)
	var dragonErr *errors.DragonError
	require.ErrorAs(t, err, &dragonErr)
	assert.Equal(t, errors.ErrInvalidInput, dragonErr.Code)
}

// TestDoRequestUnauthorized tests doRequest with 401 response.
func TestDoRequestUnauthorized(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(ErrorResponse{
			Code:  "unauthorized",
			Error: "authentication required",
		})
	}))
	defer server.Close()

	client, _ := NewRESTClient(server.URL, log)
	_, err := client.KMSGetKey(ctx, "key-123")
	require.Error(t, err)
	var dragonErr *errors.DragonError
	require.ErrorAs(t, err, &dragonErr)
	assert.Equal(t, errors.ErrUnauthorized, dragonErr.Code)
}

// TestDoRequestForbidden tests doRequest with 403 response.
func TestDoRequestForbidden(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(ErrorResponse{
			Code:  "forbidden",
			Error: "access denied",
		})
	}))
	defer server.Close()

	client, _ := NewRESTClient(server.URL, log)
	_, err := client.KMSGetKey(ctx, "key-123")
	require.Error(t, err)
	var dragonErr *errors.DragonError
	require.ErrorAs(t, err, &dragonErr)
	assert.Equal(t, errors.ErrForbidden, dragonErr.Code)
}

// TestKMSGenerateKeySuccess tests KMSGenerateKey success path.
func TestKMSGenerateKeySuccess(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/kms/keys", r.URL.Path)
		json.NewEncoder(w).Encode(GenerateKeyRESTResponse{
			Key: &KeyHandleResponse{
				KeyID:     "new-key-123",
				Status:    "enabled",
				Algorithm: "AES256",
				Version:   1,
				CreatedAt: time.Now().UnixNano(),
			},
			Timestamp: time.Now().UnixNano(),
		})
	}))
	defer server.Close()

	client, _ := NewRESTClient(server.URL, log)
	resp, err := client.KMSGenerateKey(ctx, &GenerateKeyRESTRequest{
		Name: "my-key",
		KeyAttributes: &KeyAttributesRequest{
			Algorithm: "AES256",
			KeySize:   256,
		},
	})
	require.NoError(t, err)
	assert.NotNil(t, resp.Key)
	assert.Equal(t, "new-key-123", resp.Key.KeyID)
}

// TestKMSImportKeySuccess tests KMSImportKey success path.
func TestKMSImportKeySuccess(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/kms/keys/import", r.URL.Path)
		json.NewEncoder(w).Encode(GenerateKeyRESTResponse{
			Key: &KeyHandleResponse{
				KeyID:     "imported-key-123",
				Status:    "enabled",
				Algorithm: "AES256",
				Version:   1,
				CreatedAt: time.Now().UnixNano(),
			},
			Timestamp: time.Now().UnixNano(),
		})
	}))
	defer server.Close()

	client, _ := NewRESTClient(server.URL, log)
	resp, err := client.KMSImportKey(ctx, &ImportKeyRESTRequest{
		KeyMaterial:   []byte("key-material"),
		WrappingKeyID: "wrapping-key-123",
	})
	require.NoError(t, err)
	assert.NotNil(t, resp.Key)
	assert.Equal(t, "imported-key-123", resp.Key.KeyID)
}

// TestKMSListKeyVersionsSuccess tests KMSListKeyVersions success path.
func TestKMSListKeyVersionsSuccess(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/api/v1/kms/keys/")
		assert.Contains(t, r.URL.Path, "/versions")
		json.NewEncoder(w).Encode(ListKeyVersionsRESTResponse{
			KeyID:    "key-123",
			Versions: []*KeyVersionResponse{{Version: 1}},
		})
	}))
	defer server.Close()

	client, _ := NewRESTClient(server.URL, log)
	resp, err := client.KMSListKeyVersions(ctx, "key-123")
	require.NoError(t, err)
	assert.Equal(t, "key-123", resp.KeyID)
	assert.Len(t, resp.Versions, 1)
}

// TestKMSUpdateKeyStatusSuccess tests KMSUpdateKeyStatus success path.
func TestKMSUpdateKeyStatusSuccess(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/api/v1/kms/keys/")
		assert.Contains(t, r.URL.Path, "/status")
		json.NewEncoder(w).Encode(UpdateKeyStatusRESTResponse{
			KeyID:     "key-123",
			Status:    "enabled",
			Timestamp: time.Now().UnixNano(),
		})
	}))
	defer server.Close()

	client, _ := NewRESTClient(server.URL, log)
	resp, err := client.KMSUpdateKeyStatus(ctx, "key-123", &UpdateKeyStatusRESTRequest{Status: "enabled"})
	require.NoError(t, err)
	assert.Equal(t, "key-123", resp.KeyID)
	assert.Equal(t, "enabled", resp.Status)
}

// TestKMSGenerateDataKeySuccess tests KMSGenerateDataKey success path.
func TestKMSGenerateDataKeySuccess(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/api/v1/crypto/datakey/")
		json.NewEncoder(w).Encode(DataKeyRESTResponse{
			KeyID:          "kek-123",
			Plaintext:      []byte("plain-dek"),
			CiphertextBlob: []byte("encrypted-dek"),
			Timestamp:      time.Now().UnixNano(),
		})
	}))
	defer server.Close()

	client, _ := NewRESTClient(server.URL, log)
	resp, err := client.KMSGenerateDataKey(ctx, &DataKeyRESTRequest{KeySizeBits: 256})
	require.NoError(t, err)
	assert.NotNil(t, resp.Plaintext)
	assert.NotNil(t, resp.CiphertextBlob)
}

// TestKMSGenerateDataKeyWithKEKSuccess tests KMSGenerateDataKeyWithKEK success path.
func TestKMSGenerateDataKeyWithKEKSuccess(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/api/v1/crypto/datakey/")
		json.NewEncoder(w).Encode(DataKeyRESTResponse{
			KeyID:          "kek-123",
			Plaintext:      []byte("plain-dek"),
			CiphertextBlob: []byte("encrypted-dek"),
			Timestamp:      time.Now().UnixNano(),
		})
	}))
	defer server.Close()

	client, _ := NewRESTClient(server.URL, log)
	resp, err := client.KMSGenerateDataKeyWithKEK(ctx, "kek-123", &DataKeyRESTRequest{KeySizeBits: 256})
	require.NoError(t, err)
	assert.NotNil(t, resp.Plaintext)
	assert.NotNil(t, resp.CiphertextBlob)
}

// TestSealInitSuccess tests SealInit success path.
func TestSealInitSuccess(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/seal/init", r.URL.Path)
		json.NewEncoder(w).Encode(service.SealInitResponse{
			Shares:    [][]byte{[]byte("share1"), []byte("share2"), []byte("share3")},
			RootToken: "root-token-123",
			Version:   1,
			Timestamp: time.Now(),
		})
	}))
	defer server.Close()

	client, _ := NewRESTClient(server.URL, log)
	resp, err := client.SealInit(ctx, &service.SealInitRequest{
		Threshold:   2,
		TotalShares: 3,
	})
	require.NoError(t, err)
	assert.Len(t, resp.Shares, 3)
	assert.Equal(t, "root-token-123", resp.RootToken)
}

// TestRekeySuccess tests Rekey success path.
func TestRekeySuccess(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/seal/rekey", r.URL.Path)
		json.NewEncoder(w).Encode(service.RekeyResponse{
			Shares:    [][]byte{[]byte("newshare1"), []byte("newshare2")},
			Version:   2,
			Timestamp: time.Now(),
		})
	}))
	defer server.Close()

	client, _ := NewRESTClient(server.URL, log)
	resp, err := client.Rekey(ctx, &service.RekeyRequest{
		NewThreshold:   2,
		NewTotalShares: 5,
	})
	require.NoError(t, err)
	assert.Len(t, resp.Shares, 2)
	assert.Equal(t, uint64(2), resp.Version)
}

// TestKMSListKeysSuccess tests KMSListKeys success path.
func TestKMSListKeysSuccess(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/api/v1/kms/keys")
		json.NewEncoder(w).Encode(ListKeysRESTResponse{
			Keys:       []*KeyHandleResponse{{KeyID: "key-1"}, {KeyID: "key-2"}},
			TotalCount: 2,
			HasMore:    false,
		})
	}))
	defer server.Close()

	client, _ := NewRESTClient(server.URL, log)
	resp, err := client.KMSListKeys(ctx, 100, "")
	require.NoError(t, err)
	assert.Len(t, resp.Keys, 2)
}

// TestListSecretVersionsSuccess tests ListSecretVersions success path.
func TestListSecretVersionsSuccess(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/api/v1/secrets/")
		assert.Contains(t, r.URL.Path, "/versions")
		json.NewEncoder(w).Encode(service.ListSecretVersionsResponse{
			Versions: []*service.SecretVersion{{Version: 1}, {Version: 2}},
			HasMore:  false,
		})
	}))
	defer server.Close()

	client, _ := NewRESTClient(server.URL, log)
	resp, err := client.ListSecretVersions(ctx, &service.ListSecretVersionsRequest{SecretID: "app/db/password"})
	require.NoError(t, err)
	assert.Len(t, resp.Versions, 2)
}

// TestRollbackSecretSuccess tests RollbackSecret success path.
func TestRollbackSecretSuccess(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/api/v1/secrets/")
		assert.Contains(t, r.URL.Path, "/rollback")
		json.NewEncoder(w).Encode(service.RollbackSecretResponse{
			SecretID:   "app/db/password",
			NewVersion: 3,
			Timestamp:  time.Now(),
		})
	}))
	defer server.Close()

	client, _ := NewRESTClient(server.URL, log)
	resp, err := client.RollbackSecret(ctx, &service.RollbackSecretRequest{SecretID: "app/db/password", TargetVersion: 1})
	require.NoError(t, err)
	assert.Equal(t, uint64(3), resp.NewVersion)
}

// TestRotateSecretSuccess tests RotateSecret success path.
func TestRotateSecretSuccess(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/api/v1/secrets/")
		assert.Contains(t, r.URL.Path, "/rotate")
		json.NewEncoder(w).Encode(service.RotateSecretResponse{
			SecretID:   "app/db/password",
			NewVersion: 2,
			Timestamp:  time.Now(),
		})
	}))
	defer server.Close()

	client, _ := NewRESTClient(server.URL, log)
	resp, err := client.RotateSecret(ctx, &service.RotateSecretRequest{SecretID: "app/db/password", NewData: []byte("new-password")})
	require.NoError(t, err)
	assert.Equal(t, uint64(2), resp.NewVersion)
}

// TestSealStatusSuccess tests SealStatus success path.
func TestSealStatusSuccess(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/seal/status", r.URL.Path)
		json.NewEncoder(w).Encode(service.SealStatusResponse{
			Sealed:    false,
			Threshold: 2,
		})
	}))
	defer server.Close()

	client, _ := NewRESTClient(server.URL, log)
	resp, err := client.SealStatus(ctx)
	require.NoError(t, err)
	assert.False(t, resp.Sealed)
	assert.Equal(t, 2, resp.Threshold)
}

// TestSealSuccess tests Seal success path.
func TestSealSuccess(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/seal/seal", r.URL.Path)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, _ := NewRESTClient(server.URL, log)
	err := client.Seal(ctx)
	require.NoError(t, err)
}
