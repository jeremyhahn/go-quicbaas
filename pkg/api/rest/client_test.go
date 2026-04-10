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

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testLogger creates a test logger.
func testLogger(t *testing.T) *logger.Logger {
	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "console",
	})
	require.NoError(t, err)
	return log
}

// TestNewRESTClient tests client creation with various options.
func TestNewRESTClient(t *testing.T) {
	log := testLogger(t)

	tests := []struct {
		name      string
		baseURL   string
		logger    *logger.Logger
		opts      []ClientOption
		wantError bool
		errorCode errors.ErrorCode
	}{
		{
			name:      "valid client",
			baseURL:   "http://localhost:8080",
			logger:    log,
			wantError: false,
		},
		{
			name:      "empty baseURL",
			baseURL:   "",
			logger:    log,
			wantError: true,
			errorCode: errors.ErrInvalidInput,
		},
		{
			name:      "nil logger",
			baseURL:   "http://localhost:8080",
			logger:    nil,
			wantError: true,
			errorCode: errors.ErrInvalidInput,
		},
		{
			name:    "with timeout",
			baseURL: "http://localhost:8080",
			logger:  log,
			opts: []ClientOption{
				WithTimeout(10 * time.Second),
			},
			wantError: false,
		},
		{
			name:    "with invalid timeout",
			baseURL: "http://localhost:8080",
			logger:  log,
			opts: []ClientOption{
				WithTimeout(-1 * time.Second),
			},
			wantError: true,
			errorCode: errors.ErrConfiguration,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewRESTClient(tt.baseURL, tt.logger, tt.opts...)

			if tt.wantError {
				require.Error(t, err)
				var dragonErr *errors.DragonError
				require.ErrorAs(t, err, &dragonErr)
				assert.Equal(t, tt.errorCode, dragonErr.Code)
			} else {
				require.NoError(t, err)
				require.NotNil(t, client)
				assert.Equal(t, tt.baseURL, client.baseURL)
				assert.NotNil(t, client.httpClient)
				assert.NotNil(t, client.logger)
			}
		})
	}
}

// TestRESTClient_KMS tests KMS operations.
func TestRESTClient_KMS(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	// Test GenerateKey - uses REST-specific types
	t.Run("GenerateKey", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Equal(t, "/api/v1/kms/keys", r.URL.Path)

			// Return REST-specific response
			resp := GenerateKeyRESTResponse{
				Key: &KeyHandleResponse{
					KeyID:     "key-123",
					Version:   1,
					Algorithm: "AES-256-GCM",
					Status:    "enabled",
					CreatedAt: time.Now().Unix(),
				},
				Timestamp: time.Now().Unix(),
			}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client, err := NewRESTClient(server.URL, log)
		require.NoError(t, err)

		// Use REST-specific request type (no KeySpec dependency)
		resp, err := client.KMSGenerateKey(ctx, &GenerateKeyRESTRequest{
			Name: "test-key",
			KeyAttributes: &KeyAttributesRequest{
				Algorithm: "AES-256-GCM",
				KeyUsage:  []string{"encrypt", "decrypt"},
			},
		})

		require.NoError(t, err)
		assert.Equal(t, "key-123", resp.Key.KeyID)
	})

	// Test Encrypt
	t.Run("Encrypt", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Equal(t, "/api/v1/kms/encrypt/test-key", r.URL.Path)

			resp := service.EncryptResponse{
				KeyID:      "test-key",
				KeyVersion: 1,
				Ciphertext: []byte("encrypted-data"),
				Timestamp:  time.Now(),
			}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client, err := NewRESTClient(server.URL, log)
		require.NoError(t, err)

		resp, err := client.KMSEncrypt(ctx, &service.EncryptRequest{
			KeyID:     "test-key",
			Plaintext: []byte("secret-data"),
		})

		require.NoError(t, err)
		assert.Equal(t, "test-key", resp.KeyID)
		assert.NotEmpty(t, resp.Ciphertext)
	})

	// Test Decrypt
	t.Run("Decrypt", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Equal(t, "/api/v1/kms/decrypt/test-key", r.URL.Path)

			resp := service.DecryptResponse{
				KeyID:      "test-key",
				KeyVersion: 1,
				Plaintext:  []byte("secret-data"),
				Timestamp:  time.Now(),
			}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client, err := NewRESTClient(server.URL, log)
		require.NoError(t, err)

		resp, err := client.KMSDecrypt(ctx, &service.DecryptRequest{
			KeyID:      "test-key",
			Ciphertext: []byte("encrypted-data"),
		})

		require.NoError(t, err)
		assert.Equal(t, "test-key", resp.KeyID)
		assert.Equal(t, []byte("secret-data"), resp.Plaintext)
	})

	// Test ListKeys - uses REST-specific types
	t.Run("ListKeys", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodGet, r.Method)
			assert.Contains(t, r.URL.Path, "/api/v1/kms/keys")

			// Return REST-specific response
			resp := ListKeysRESTResponse{
				Keys: []*KeyHandleResponse{
					{KeyID: "key-1", Version: 1, Algorithm: "AES-256-GCM", Status: "enabled"},
					{KeyID: "key-2", Version: 1, Algorithm: "AES-256-GCM", Status: "enabled"},
				},
				HasMore:   false,
				Timestamp: time.Now().Unix(),
			}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client, err := NewRESTClient(server.URL, log)
		require.NoError(t, err)

		// Use simplified parameters instead of service.ListKeysRequest
		resp, err := client.KMSListKeys(ctx, 10, "")

		require.NoError(t, err)
		assert.Len(t, resp.Keys, 2)
	})

	// Test RotateKey - uses REST-specific types
	t.Run("RotateKey", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Equal(t, "/api/v1/kms/keys/test-key/rotate", r.URL.Path)

			resp := RotateKeyRESTResponse{
				KeyID:      "test-key",
				OldVersion: 1,
				NewVersion: 2,
				Timestamp:  time.Now().Unix(),
			}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client, err := NewRESTClient(server.URL, log)
		require.NoError(t, err)

		resp, err := client.KMSRotateKey(ctx, "test-key")

		require.NoError(t, err)
		assert.Equal(t, "test-key", resp.KeyID)
		assert.Equal(t, 2, resp.NewVersion)
	})

	// Test DeleteKey - uses REST-specific types
	t.Run("DeleteKey", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodDelete, r.Method)
			assert.Equal(t, "/api/v1/kms/keys/test-key", r.URL.Path)

			resp := DeleteKeyRESTResponse{
				KeyID:     "test-key",
				Status:    "pending_deletion",
				Timestamp: time.Now().Unix(),
			}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client, err := NewRESTClient(server.URL, log)
		require.NoError(t, err)

		resp, err := client.KMSDeleteKey(ctx, "test-key")

		require.NoError(t, err)
		assert.Equal(t, "test-key", resp.KeyID)
		assert.Equal(t, "pending_deletion", resp.Status)
	})

	// Test GetKey - uses REST-specific types
	t.Run("GetKey", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodGet, r.Method)
			assert.Equal(t, "/api/v1/kms/keys/test-key", r.URL.Path)

			resp := GetKeyRESTResponse{
				Key: &KeyHandleResponse{
					KeyID:     "test-key",
					Version:   1,
					Algorithm: "AES-256-GCM",
					Status:    "enabled",
				},
				Timestamp: time.Now().Unix(),
			}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client, err := NewRESTClient(server.URL, log)
		require.NoError(t, err)

		resp, err := client.KMSGetKey(ctx, "test-key")

		require.NoError(t, err)
		assert.Equal(t, "test-key", resp.Key.KeyID)
	})
}

// TestRESTClient_DEK tests DEK operations.
func TestRESTClient_DEK(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	// Test CreateDEK
	t.Run("CreateDEK", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Equal(t, "/api/v1/kms/deks", r.URL.Path)

			resp := service.CreateDEKResponse{
				DEK: &service.DEKMetadata{
					Path:      "/app/data",
					Algorithm: service.KeyAlgAES256GCM,
					Version:   1,
				},
				Timestamp: time.Now(),
			}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client, err := NewRESTClient(server.URL, log)
		require.NoError(t, err)

		resp, err := client.KMSCreateDEK(ctx, &service.CreateDEKRequest{
			Path:      "/app/data",
			Algorithm: service.KeyAlgAES256GCM,
		})

		require.NoError(t, err)
		assert.Equal(t, "/app/data", resp.DEK.Path)
	})

	// Test GetDEK
	t.Run("GetDEK", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodGet, r.Method)
			assert.Contains(t, r.URL.Path, "/api/v1/kms/deks/")

			resp := service.GetDEKResponse{
				DEK: &service.DEKMetadata{
					Path:      "/app/data",
					Algorithm: service.KeyAlgAES256GCM,
					Version:   1,
				},
				Timestamp: time.Now(),
			}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client, err := NewRESTClient(server.URL, log)
		require.NoError(t, err)

		resp, err := client.KMSGetDEK(ctx, &service.GetDEKRequest{
			Path: "/app/data",
		})

		require.NoError(t, err)
		assert.Equal(t, "/app/data", resp.DEK.Path)
	})
}

// TestRESTClient_DataKey tests data key generation.
func TestRESTClient_DataKey(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	// Test GenerateDataKey
	t.Run("GenerateDataKey", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Contains(t, r.URL.Path, "/api/v1/crypto/datakey/")

			resp := DataKeyRESTResponse{
				KeyID:          "test-kek",
				Plaintext:      []byte("plaintext-dek"),
				CiphertextBlob: []byte("wrapped-dek"),
				Timestamp:      time.Now().Unix(),
			}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client, err := NewRESTClient(server.URL, log)
		require.NoError(t, err)

		resp, err := client.KMSGenerateDataKey(ctx, &DataKeyRESTRequest{
			KeySizeBits: 256,
		})

		require.NoError(t, err)
		assert.NotEmpty(t, resp.Plaintext)
		assert.NotEmpty(t, resp.CiphertextBlob)
	})

	// Test GenerateDataKeyWithKEK
	t.Run("GenerateDataKeyWithKEK", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Equal(t, "/api/v1/crypto/datakey/my-kek", r.URL.Path)

			resp := DataKeyRESTResponse{
				KeyID:          "my-kek",
				Plaintext:      []byte("plaintext-dek"),
				CiphertextBlob: []byte("wrapped-dek"),
				Timestamp:      time.Now().Unix(),
			}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client, err := NewRESTClient(server.URL, log)
		require.NoError(t, err)

		resp, err := client.KMSGenerateDataKeyWithKEK(ctx, "my-kek", &DataKeyRESTRequest{
			KeySizeBits: 256,
		})

		require.NoError(t, err)
		assert.Equal(t, "my-kek", resp.KeyID)
		assert.NotEmpty(t, resp.Plaintext)
	})
}

// TestRESTClient_Secrets tests secrets operations.
func TestRESTClient_Secrets(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	// Test StoreSecret
	t.Run("StoreSecret", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Contains(t, r.URL.Path, "/api/v1/secrets/")

			resp := service.StoreSecretResponse{
				SecretID:  "db/password",
				Version:   1,
				Timestamp: time.Now(),
			}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client, err := NewRESTClient(server.URL, log)
		require.NoError(t, err)

		resp, err := client.StoreSecret(ctx, &service.StoreSecretRequest{
			Name: "db/password",
			Data: []byte(`{"password":"secret123"}`),
		})

		require.NoError(t, err)
		assert.Equal(t, "db/password", resp.SecretID)
		assert.Equal(t, uint64(1), resp.Version)
	})

	// Test GetSecret
	t.Run("GetSecret", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodGet, r.Method)
			assert.Contains(t, r.URL.Path, "/api/v1/secrets/")

			resp := service.GetSecretResponse{
				SecretID: "db/password",
				Version:  1,
				Data:     []byte(`{"password":"secret123"}`),
			}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client, err := NewRESTClient(server.URL, log)
		require.NoError(t, err)

		resp, err := client.GetSecret(ctx, &service.GetSecretRequest{
			SecretID: "db/password",
		})

		require.NoError(t, err)
		assert.Equal(t, "db/password", resp.SecretID)
		assert.NotEmpty(t, resp.Data)
	})

	// Test DeleteSecret
	t.Run("DeleteSecret", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodDelete, r.Method)
			assert.Contains(t, r.URL.Path, "/api/v1/secrets/")

			resp := service.DeleteSecretResponse{
				SecretID:  "db/password",
				Deleted:   true,
				Timestamp: time.Now(),
			}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client, err := NewRESTClient(server.URL, log)
		require.NoError(t, err)

		resp, err := client.DeleteSecret(ctx, &service.DeleteSecretRequest{
			SecretID: "db/password",
		})

		require.NoError(t, err)
		assert.True(t, resp.Deleted)
	})
}

// TestRESTClient_Seal tests seal operations.
func TestRESTClient_Seal(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	// Test SealStatus
	t.Run("SealStatus", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodGet, r.Method)
			assert.Equal(t, "/api/v1/seal/status", r.URL.Path)

			resp := service.SealStatusResponse{
				Sealed:      false,
				Threshold:   3,
				TotalShares: 5,
			}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client, err := NewRESTClient(server.URL, log)
		require.NoError(t, err)

		resp, err := client.SealStatus(ctx)

		require.NoError(t, err)
		assert.False(t, resp.Sealed)
		assert.Equal(t, 3, resp.Threshold)
	})

	// Test Unseal
	t.Run("Unseal", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Equal(t, "/api/v1/seal/unseal", r.URL.Path)

			resp := service.UnsealResponse{
				Sealed:         false,
				SharesProvided: 3,
			}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client, err := NewRESTClient(server.URL, log)
		require.NoError(t, err)

		resp, err := client.Unseal(ctx, &service.UnsealRequest{
			Share: []byte("unseal-key-1"),
		})

		require.NoError(t, err)
		assert.False(t, resp.Sealed)
	})
}

// TestRESTClient_TimeSeries tests time series operations.
func TestRESTClient_TimeSeries(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	// Test WritePoint
	t.Run("WritePoint", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Equal(t, "/api/v1/timeseries/write", r.URL.Path)

			resp := cli.WritePointResponse{
				Success:   true,
				Timestamp: time.Now(),
			}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client, err := NewRESTClient(server.URL, log)
		require.NoError(t, err)

		resp, err := client.TSWritePoint(ctx, &cli.WritePointRequest{
			Metric:    "cpu.usage",
			Value:     75.5,
			Timestamp: time.Now(),
		})

		require.NoError(t, err)
		assert.True(t, resp.Success)
	})

	// Test Query
	t.Run("Query", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Equal(t, "/api/v1/timeseries/query", r.URL.Path)

			resp := cli.TSQueryResponse{
				Metric: "cpu.usage",
				Points: []cli.TSDataPoint{
					{Value: 75.5, Timestamp: time.Now()},
				},
			}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client, err := NewRESTClient(server.URL, log)
		require.NoError(t, err)

		resp, err := client.TSQuery(ctx, &cli.TSQueryRequest{
			Metric:    "cpu.usage",
			StartTime: time.Now().Add(-1 * time.Hour),
			EndTime:   time.Now(),
		})

		require.NoError(t, err)
		assert.Len(t, resp.Points, 1)
	})
}

// TestRESTClient_ErrorHandling tests error handling.
func TestRESTClient_ErrorHandling(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	tests := []struct {
		name       string
		statusCode int
		errorCode  errors.ErrorCode
	}{
		{"BadRequest", http.StatusBadRequest, errors.ErrInvalidInput},
		{"Unauthorized", http.StatusUnauthorized, errors.ErrUnauthorized},
		{"Forbidden", http.StatusForbidden, errors.ErrForbidden},
		{"NotFound", http.StatusNotFound, errors.ErrNotFound},
		{"Conflict", http.StatusConflict, errors.ErrAlreadyExists},
		{"Timeout", http.StatusRequestTimeout, errors.ErrTimeout},
		{"TooManyRequests", http.StatusTooManyRequests, errors.ErrResourceExhausted},
		{"ServiceUnavailable", http.StatusServiceUnavailable, errors.ErrLeaderNotAvailable},
		{"NotImplemented", http.StatusNotImplemented, errors.ErrNotImplemented},
		{"InternalServerError", http.StatusInternalServerError, errors.ErrInternal},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				errResp := ErrorResponse{
					Error:     tt.name,
					Code:      tt.name,
					Timestamp: time.Now(),
				}
				json.NewEncoder(w).Encode(errResp)
			}))
			defer server.Close()

			client, err := NewRESTClient(server.URL, log)
			require.NoError(t, err)

			// Use KMSGetKey to test error handling (method that still exists)
			_, err = client.KMSGetKey(ctx, "test-key")

			require.Error(t, err)
			var dragonErr *errors.DragonError
			require.ErrorAs(t, err, &dragonErr)
			assert.Equal(t, tt.errorCode, dragonErr.Code)
		})
	}
}

// TestRESTClient_NilRequests tests nil request handling.
func TestRESTClient_NilRequests(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := NewRESTClient(server.URL, log)
	require.NoError(t, err)

	tests := []struct {
		name string
		fn   func() error
	}{
		{"KMSEncrypt", func() error { _, err := client.KMSEncrypt(ctx, nil); return err }},
		{"KMSDecrypt", func() error { _, err := client.KMSDecrypt(ctx, nil); return err }},
		{"KMSGenerateKey", func() error { _, err := client.KMSGenerateKey(ctx, nil); return err }},
		{"KMSGenerateDataKey", func() error { _, err := client.KMSGenerateDataKey(ctx, nil); return err }},
		{"StoreSecret", func() error { _, err := client.StoreSecret(ctx, nil); return err }},
		{"GetSecret", func() error { _, err := client.GetSecret(ctx, nil); return err }},
		{"TSWritePoint", func() error { _, err := client.TSWritePoint(ctx, nil); return err }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.fn()
			require.Error(t, err)
			var dragonErr *errors.DragonError
			require.ErrorAs(t, err, &dragonErr)
			assert.Equal(t, errors.ErrInvalidInput, dragonErr.Code)
		})
	}
}

// TestRESTClient_EmptyKeyID tests empty key ID handling.
func TestRESTClient_EmptyKeyID(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := NewRESTClient(server.URL, log)
	require.NoError(t, err)

	tests := []struct {
		name string
		fn   func() error
	}{
		{"KMSGetKey", func() error { _, err := client.KMSGetKey(ctx, ""); return err }},
		{"KMSRotateKey", func() error { _, err := client.KMSRotateKey(ctx, ""); return err }},
		{"KMSDeleteKey", func() error { _, err := client.KMSDeleteKey(ctx, ""); return err }},
		{"KMSListKeyVersions", func() error { _, err := client.KMSListKeyVersions(ctx, ""); return err }},
		{"KMSUpdateKeyStatus", func() error {
			_, err := client.KMSUpdateKeyStatus(ctx, "", &UpdateKeyStatusRESTRequest{Status: "enabled"})
			return err
		}},
		{"KMSGenerateDataKeyWithKEK", func() error {
			_, err := client.KMSGenerateDataKeyWithKEK(ctx, "", &DataKeyRESTRequest{})
			return err
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.fn()
			require.Error(t, err)
			var dragonErr *errors.DragonError
			require.ErrorAs(t, err, &dragonErr)
			assert.Equal(t, errors.ErrInvalidInput, dragonErr.Code)
		})
	}
}

// TestRESTClient_ClusterStatus tests cluster status.
func TestRESTClient_ClusterStatus(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/v1/cluster/status", r.URL.Path)

		resp := ClusterStatusResponse{
			NodeID:     1,
			Healthy:    true,
			TotalNodes: 3,
			Shards: []ShardInfo{
				{ShardID: 1, LeaderID: 1, IsLeader: true, State: "healthy"},
			},
			Timestamp: time.Now(),
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, err := NewRESTClient(server.URL, log)
	require.NoError(t, err)

	resp, err := client.ClusterStatus(ctx)

	require.NoError(t, err)
	assert.True(t, resp.Healthy)
	assert.Equal(t, 3, resp.NodeCount)
}

// TestRESTClient_Ping tests server connectivity.
func TestRESTClient_Ping(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	t.Run("healthy server", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resp := HealthResponse{
				Status: "healthy",
			}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client, err := NewRESTClient(server.URL, log)
		require.NoError(t, err)

		err = client.Ping(ctx)
		require.NoError(t, err)
	})

	t.Run("unhealthy server", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resp := HealthResponse{
				Status: "unhealthy",
			}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client, err := NewRESTClient(server.URL, log)
		require.NoError(t, err)

		err = client.Ping(ctx)
		require.Error(t, err)
	})
}

// TestRESTClient_Close tests client cleanup.
func TestRESTClient_Close(t *testing.T) {
	log := testLogger(t)

	client, err := NewRESTClient("http://localhost:8080", log)
	require.NoError(t, err)

	err = client.Close()
	require.NoError(t, err)
}

func TestTSWritePointSuccess(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/v1/timeseries/write", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(cli.WritePointResponse{
			Success: true,
		})
	}))
	defer server.Close()

	client, err := NewRESTClient(server.URL, log)
	require.NoError(t, err)

	resp, err := client.TSWritePoint(ctx, &cli.WritePointRequest{
		Metric:    "cpu_usage",
		Value:     85.5,
		Timestamp: time.Now(),
	})
	require.NoError(t, err)
	assert.True(t, resp.Success)
}

func TestTSWritePointNilRequest(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	client, err := NewRESTClient("http://localhost:9999", log)
	require.NoError(t, err)

	resp, err := client.TSWritePoint(ctx, nil)
	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestTSWritePointServerError(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "write failed"})
	}))
	defer server.Close()

	client, err := NewRESTClient(server.URL, log)
	require.NoError(t, err)

	resp, err := client.TSWritePoint(ctx, &cli.WritePointRequest{
		Metric: "cpu_usage",
		Value:  85.5,
	})
	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestTSQuerySuccess(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/v1/timeseries/query", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(cli.TSQueryResponse{})
	}))
	defer server.Close()

	client, err := NewRESTClient(server.URL, log)
	require.NoError(t, err)

	resp, err := client.TSQuery(ctx, &cli.TSQueryRequest{
		Metric: "cpu_usage",
	})
	require.NoError(t, err)
	assert.NotNil(t, resp)
}

func TestTSQueryNilRequest(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	client, err := NewRESTClient("http://localhost:9999", log)
	require.NoError(t, err)

	resp, err := client.TSQuery(ctx, nil)
	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestTSQueryServerError(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "invalid query"})
	}))
	defer server.Close()

	client, err := NewRESTClient(server.URL, log)
	require.NoError(t, err)

	resp, err := client.TSQuery(ctx, &cli.TSQueryRequest{Metric: ""})
	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestClusterStatusSuccess(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/v1/cluster/status", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ClusterStatusResponse{
			Healthy:    true,
			LeaderID:   "42",
			TotalNodes: 3,
		})
	}))
	defer server.Close()

	client, err := NewRESTClient(server.URL, log)
	require.NoError(t, err)

	resp, err := client.ClusterStatus(ctx)
	require.NoError(t, err)
	assert.True(t, resp.Healthy)
	assert.Equal(t, uint64(42), resp.LeaderID)
	assert.Equal(t, 3, resp.NodeCount)
}

func TestClusterStatusServerError(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "cluster unavailable"})
	}))
	defer server.Close()

	client, err := NewRESTClient(server.URL, log)
	require.NoError(t, err)

	resp, err := client.ClusterStatus(ctx)
	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestClusterStatusEmptyLeaderID(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ClusterStatusResponse{
			Healthy:    false,
			LeaderID:   "",
			TotalNodes: 1,
		})
	}))
	defer server.Close()

	client, err := NewRESTClient(server.URL, log)
	require.NoError(t, err)

	resp, err := client.ClusterStatus(ctx)
	require.NoError(t, err)
	assert.False(t, resp.Healthy)
	assert.Equal(t, uint64(0), resp.LeaderID)
}

func TestPingSuccess(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/v1/health", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(HealthResponse{Status: "healthy"})
	}))
	defer server.Close()

	client, err := NewRESTClient(server.URL, log)
	require.NoError(t, err)

	err = client.Ping(ctx)
	require.NoError(t, err)
}

func TestPingUnhealthy(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(HealthResponse{Status: "degraded"})
	}))
	defer server.Close()

	client, err := NewRESTClient(server.URL, log)
	require.NoError(t, err)

	err = client.Ping(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unhealthy")
}

func TestPingServerError(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "server error"})
	}))
	defer server.Close()

	client, err := NewRESTClient(server.URL, log)
	require.NoError(t, err)

	err = client.Ping(ctx)
	require.Error(t, err)
}

func TestSchedulerUpdateJobSuccess(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	now := time.Now()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		assert.Contains(t, r.URL.Path, "/api/v1/scheduler/jobs/")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(UpdateSchedulerJobRESTResponse{
			Job: &SchedulerJobRESTResponse{
				ID:          42,
				Name:        "updated-job",
				Description: "Updated description",
				Status:      "scheduled",
				CreatedAt:   now,
				UpdatedAt:   now,
			},
			Timestamp: now,
		})
	}))
	defer server.Close()

	client, err := NewRESTClient(server.URL, log)
	require.NoError(t, err)

	resp, err := client.SchedulerUpdateJob(ctx, &service.UpdateSchedulerJobRequest{
		JobID:       42,
		Name:        "updated-job",
		Description: "Updated description",
		TenantID:    "tenant-1",
		ExecuteAt:   now.Add(1 * time.Hour),
	})
	require.NoError(t, err)
	assert.NotNil(t, resp.Job)
	assert.Equal(t, "updated-job", resp.Job.Name)
}

func TestSchedulerUpdateJobNilRequest(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	client, err := NewRESTClient("http://localhost:9999", log)
	require.NoError(t, err)

	resp, err := client.SchedulerUpdateJob(ctx, nil)
	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestSchedulerCancelJobWithReason(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	now := time.Now()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Contains(t, r.URL.Path, "/cancel")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(CancelSchedulerJobRESTResponse{
			JobID:     42,
			OldStatus: "scheduled",
			NewStatus: "cancelled",
			Timestamp: now,
		})
	}))
	defer server.Close()

	client, err := NewRESTClient(server.URL, log)
	require.NoError(t, err)

	resp, err := client.SchedulerCancelJob(ctx, &service.CancelSchedulerJobRequest{
		JobID:    42,
		TenantID: "tenant-1",
		Reason:   "no longer needed",
	})
	require.NoError(t, err)
	assert.Equal(t, uint64(42), resp.JobID)
	assert.Equal(t, service.SchedulerJobStatus("cancelled"), resp.NewStatus)
}

func TestSchedulerCancelJobNilRequest(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	client, err := NewRESTClient("http://localhost:9999", log)
	require.NoError(t, err)

	resp, err := client.SchedulerCancelJob(ctx, nil)
	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestSchedulerGetJobHistorySuccess(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	now := time.Now()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.Path, "/history")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(GetSchedulerJobHistoryRESTResponse{
			Executions: []*SchedulerJobExecutionRESTResponse{
				{
					ID:        1,
					JobID:     42,
					Status:    "completed",
					StartedAt: now.Add(-1 * time.Minute),
					EndedAt:   now,
				},
			},
			NextCursor: "cursor-2",
			HasMore:    true,
			Timestamp:  now,
		})
	}))
	defer server.Close()

	client, err := NewRESTClient(server.URL, log)
	require.NoError(t, err)

	resp, err := client.SchedulerGetJobHistory(ctx, &service.GetSchedulerJobHistoryRequest{
		JobID:    42,
		TenantID: "tenant-1",
		Limit:    10,
		Cursor:   "cursor-1",
	})
	require.NoError(t, err)
	assert.Len(t, resp.Executions, 1)
	assert.True(t, resp.HasMore)
	assert.Equal(t, "cursor-2", resp.NextCursor)
}

func TestSchedulerGetJobHistoryNilRequest(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	client, err := NewRESTClient("http://localhost:9999", log)
	require.NoError(t, err)

	resp, err := client.SchedulerGetJobHistory(ctx, nil)
	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestSchedulerGetStatusSuccess(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	now := time.Now()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/v1/scheduler/status", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(GetSchedulerStatusRESTResponse{
			Running: true,
			Stats: &SchedulerStatsRESTResponse{
				JobCount:     10,
				RunningJobs:  2,
				SuccessCount: 50,
				FailureCount: 3,
				LastTickTime: now,
				StartedAt:    now.Add(-1 * time.Hour),
			},
			Timestamp: now,
		})
	}))
	defer server.Close()

	client, err := NewRESTClient(server.URL, log)
	require.NoError(t, err)

	resp, err := client.SchedulerGetStatus(ctx)
	require.NoError(t, err)
	assert.True(t, resp.Running)
	assert.NotNil(t, resp.Stats)
	assert.Equal(t, uint64(10), resp.Stats.JobCount)
	assert.Equal(t, uint64(2), resp.Stats.RunningJobs)
}

func TestSchedulerGetStatusWithNilStats(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(GetSchedulerStatusRESTResponse{
			Running:   false,
			Stats:     nil,
			Timestamp: time.Now(),
		})
	}))
	defer server.Close()

	client, err := NewRESTClient(server.URL, log)
	require.NoError(t, err)

	resp, err := client.SchedulerGetStatus(ctx)
	require.NoError(t, err)
	assert.False(t, resp.Running)
	assert.Nil(t, resp.Stats)
}

func TestSchedulerGetStatusServerError(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "internal error"})
	}))
	defer server.Close()

	client, err := NewRESTClient(server.URL, log)
	require.NoError(t, err)

	resp, err := client.SchedulerGetStatus(ctx)
	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestSchedulerListJobsWithFilters(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	now := time.Now()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.Path, "/api/v1/scheduler/jobs")

		// Verify query parameters are passed
		query := r.URL.Query()
		assert.Equal(t, "user", query.Get("owner_type"))
		assert.Equal(t, "user-123", query.Get("owner_id"))
		assert.Equal(t, "backup", query.Get("handler"))
		assert.Equal(t, "10", query.Get("limit"))

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ListSchedulerJobsRESTResponse{
			Jobs: []*SchedulerJobRESTResponse{
				{
					ID:        1,
					Name:      "backup-job",
					Status:    "scheduled",
					CreatedAt: now,
					UpdatedAt: now,
				},
			},
			TotalCount: 1,
			Timestamp:  now,
		})
	}))
	defer server.Close()

	client, err := NewRESTClient(server.URL, log)
	require.NoError(t, err)

	resp, err := client.SchedulerListJobs(ctx, &service.ListSchedulerJobsRequest{
		OwnerType:      "user",
		OwnerID:        "user-123",
		Handler:        "backup",
		Limit:          10,
		Cursor:         "abc",
		TargetTenantID: "t-1",
		Status:         "scheduled",
	})
	require.NoError(t, err)
	assert.Len(t, resp.Jobs, 1)
	assert.Equal(t, "backup-job", resp.Jobs[0].Name)
}

func TestSchedulerListJobsNilRequest(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ListSchedulerJobsRESTResponse{
			Jobs:      []*SchedulerJobRESTResponse{},
			Timestamp: time.Now(),
		})
	}))
	defer server.Close()

	client, err := NewRESTClient(server.URL, log)
	require.NoError(t, err)

	resp, err := client.SchedulerListJobs(ctx, nil)
	require.NoError(t, err)
	assert.Empty(t, resp.Jobs)
}

func TestSchedulerTriggerJobSuccess(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	now := time.Now()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Contains(t, r.URL.Path, "/trigger")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(TriggerSchedulerJobRESTResponse{
			JobID:     42,
			Status:    "running",
			Timestamp: now,
		})
	}))
	defer server.Close()

	client, err := NewRESTClient(server.URL, log)
	require.NoError(t, err)

	resp, err := client.SchedulerTriggerJob(ctx, &service.TriggerSchedulerJobRequest{
		JobID:    42,
		TenantID: "tenant-1",
	})
	require.NoError(t, err)
	assert.Equal(t, uint64(42), resp.JobID)
	assert.Equal(t, service.SchedulerJobStatus("running"), resp.Status)
}

func TestSchedulerTriggerJobNilRequest(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	client, err := NewRESTClient("http://localhost:9999", log)
	require.NoError(t, err)

	resp, err := client.SchedulerTriggerJob(ctx, nil)
	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestSchedulerPauseJobSuccess(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	now := time.Now()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Contains(t, r.URL.Path, "/pause")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(PauseSchedulerJobRESTResponse{
			JobID:     42,
			OldStatus: "scheduled",
			NewStatus: "paused",
			Timestamp: now,
		})
	}))
	defer server.Close()

	client, err := NewRESTClient(server.URL, log)
	require.NoError(t, err)

	resp, err := client.SchedulerPauseJob(ctx, &service.PauseSchedulerJobRequest{
		JobID:    42,
		TenantID: "tenant-1",
	})
	require.NoError(t, err)
	assert.Equal(t, service.SchedulerJobStatus("paused"), resp.NewStatus)
}

func TestSchedulerPauseJobNilRequest(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	client, err := NewRESTClient("http://localhost:9999", log)
	require.NoError(t, err)

	resp, err := client.SchedulerPauseJob(ctx, nil)
	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestSchedulerResumeJobSuccess(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	now := time.Now()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Contains(t, r.URL.Path, "/resume")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ResumeSchedulerJobRESTResponse{
			JobID:     42,
			OldStatus: "paused",
			NewStatus: "scheduled",
			Timestamp: now,
		})
	}))
	defer server.Close()

	client, err := NewRESTClient(server.URL, log)
	require.NoError(t, err)

	resp, err := client.SchedulerResumeJob(ctx, &service.ResumeSchedulerJobRequest{
		JobID:    42,
		TenantID: "tenant-1",
	})
	require.NoError(t, err)
	assert.Equal(t, service.SchedulerJobStatus("scheduled"), resp.NewStatus)
}

func TestSchedulerResumeJobNilRequest(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	client, err := NewRESTClient("http://localhost:9999", log)
	require.NoError(t, err)

	resp, err := client.SchedulerResumeJob(ctx, nil)
	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestSchedulerDeleteJobWithForce(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	now := time.Now()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Contains(t, r.URL.Path, "/api/v1/scheduler/jobs/42")
		assert.Equal(t, "true", r.URL.Query().Get("force"))
		assert.Equal(t, "tenant-1", r.URL.Query().Get("tenant_id"))

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(DeleteSchedulerJobRESTResponse{
			JobID:     42,
			Deleted:   true,
			Timestamp: now,
		})
	}))
	defer server.Close()

	client, err := NewRESTClient(server.URL, log)
	require.NoError(t, err)

	resp, err := client.SchedulerDeleteJob(ctx, &service.DeleteSchedulerJobRequest{
		JobID:    42,
		TenantID: "tenant-1",
		Force:    true,
	})
	require.NoError(t, err)
	assert.True(t, resp.Deleted)
}

func TestDoRequestServerErrorWithNonJSONBody(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte("Bad Gateway"))
	}))
	defer server.Close()

	client, err := NewRESTClient(server.URL, log)
	require.NoError(t, err)

	err = client.Ping(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Bad Gateway")
}

func TestDoRequestWithInvalidResponseJSON(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("{invalid json"))
	}))
	defer server.Close()

	client, err := NewRESTClient(server.URL, log)
	require.NoError(t, err)

	resp, err := client.ClusterStatus(ctx)
	require.Error(t, err)
	assert.Nil(t, resp)
}

// TestKMSEncryptWithContext tests KMSEncrypt with encryption context set.
func TestKMSEncryptWithContext(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		assert.NotNil(t, body["context"], "context should be present in request body")

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(service.EncryptResponse{
			Ciphertext: []byte("encrypted"),
		})
	}))
	defer server.Close()

	client, err := NewRESTClient(server.URL, log)
	require.NoError(t, err)

	resp, err := client.KMSEncrypt(ctx, &service.EncryptRequest{
		Plaintext: []byte("secret"),
		KeyID:     "key-1",
		Context:   []byte(`{"tenant":"org-1"}`),
	})
	require.NoError(t, err)
	assert.NotNil(t, resp)
}

// TestKMSDecryptWithContext tests KMSDecrypt with encryption context set.
func TestKMSDecryptWithContext(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		assert.NotNil(t, body["context"], "context should be present in request body")

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(service.DecryptResponse{
			Plaintext: []byte("decrypted"),
		})
	}))
	defer server.Close()

	client, err := NewRESTClient(server.URL, log)
	require.NoError(t, err)

	resp, err := client.KMSDecrypt(ctx, &service.DecryptRequest{
		Ciphertext: []byte("encrypted"),
		KeyID:      "key-1",
		Context:    []byte(`{"tenant":"org-1"}`),
	})
	require.NoError(t, err)
	assert.Equal(t, []byte("decrypted"), resp.Plaintext)
}

// TestKMSListKeysWithCursor tests KMSListKeys with cursor pagination.
func TestKMSListKeysWithCursor(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.String(), "cursor=abc")

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ListKeysRESTResponse{})
	}))
	defer server.Close()

	client, err := NewRESTClient(server.URL, log)
	require.NoError(t, err)

	resp, err := client.KMSListKeys(ctx, 50, "abc")
	require.NoError(t, err)
	assert.NotNil(t, resp)
}

// TestKMSListKeysNegativeLimit tests KMSListKeys with negative limit defaults to 100.
func TestKMSListKeysNegativeLimit(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.String(), "limit=100")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ListKeysRESTResponse{})
	}))
	defer server.Close()

	client, err := NewRESTClient(server.URL, log)
	require.NoError(t, err)

	resp, err := client.KMSListKeys(ctx, -1, "")
	require.NoError(t, err)
	assert.NotNil(t, resp)
}

// TestSealStatusServerError tests SealStatus error handling.
func TestSealStatusServerError(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "internal error"})
	}))
	defer server.Close()

	client, err := NewRESTClient(server.URL, log)
	require.NoError(t, err)

	resp, err := client.SealStatus(ctx)
	require.Error(t, err)
	assert.Nil(t, resp)
}

// TestSealServerError tests Seal error handling.
func TestSealServerError(t *testing.T) {
	log := testLogger(t)
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "forbidden"})
	}))
	defer server.Close()

	client, err := NewRESTClient(server.URL, log)
	require.NoError(t, err)

	err = client.Seal(ctx)
	require.Error(t, err)
}
