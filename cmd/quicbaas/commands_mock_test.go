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

package main

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

// =============================================================================
// Additional Mock Client Tests (not duplicating existing tests)
// =============================================================================

func TestMockKMSClientInterfaceCompliance(t *testing.T) {
	t.Run("implements_all_methods", func(t *testing.T) {
		var client KMSClient = &MockKMSClient{}
		assert.NotNil(t, client)

		// Verify all methods are accessible
		ctx := context.Background()

		_, _ = client.KMSGenerateKey(ctx, &service.GenerateKeyRequest{})
		_, _ = client.KMSImportKey(ctx, &service.ImportKeyRequest{})
		_, _ = client.KMSGetKey(ctx, &service.GetKeyRequest{})
		_, _ = client.KMSListKeys(ctx, &service.ListKeysRequest{})
		_, _ = client.KMSRotateKey(ctx, &service.RotateKeyRequest{})
		_, _ = client.KMSDeleteKey(ctx, &service.DeleteKeyRequest{})
		_, _ = client.KMSUpdateKeyStatus(ctx, &service.UpdateKeyStatusRequest{})
		_, _ = client.KMSListKeyVersions(ctx, &service.ListKeyVersionsRequest{})
		_, _ = client.KMSExportKey(ctx, &service.ExportKeyRequest{})
		_, _ = client.KMSUnwrapKey(ctx, &service.UnwrapKeyRequest{})
		_, _ = client.KMSSetKeySafety(ctx, &service.SetKeySafetyRequest{})
		_, _ = client.KMSClearKeySafety(ctx, &service.ClearKeySafetyRequest{})
		_, _ = client.KMSEncrypt(ctx, &service.EncryptRequest{})
		_, _ = client.KMSDecrypt(ctx, &service.DecryptRequest{})
		_ = client.Close()
	})

	t.Run("custom_func_implementations", func(t *testing.T) {
		generateCalled := false
		closeCalled := false

		client := &MockKMSClient{
			KMSGenerateKeyFunc: func(ctx context.Context, req *service.GenerateKeyRequest) (*service.GenerateKeyResponse, error) {
				generateCalled = true
				return &service.GenerateKeyResponse{
					Key: &service.KeyHandle{KeyID: "custom-key"},
				}, nil
			},
			CloseFunc: func() error {
				closeCalled = true
				return nil
			},
		}

		resp, err := client.KMSGenerateKey(context.Background(), &service.GenerateKeyRequest{Name: "test"})
		require.NoError(t, err)
		assert.True(t, generateCalled)
		assert.Equal(t, "custom-key", resp.Key.KeyID)

		err = client.Close()
		require.NoError(t, err)
		assert.True(t, closeCalled)
	})
}

func TestMockSecretsClientInterfaceCompliance(t *testing.T) {
	t.Run("implements_all_methods", func(t *testing.T) {
		var client SecretsClient = &MockSecretsClient{}
		assert.NotNil(t, client)

		ctx := context.Background()

		_, _ = client.StoreSecret(ctx, &service.StoreSecretRequest{})
		_, _ = client.GetSecret(ctx, &service.GetSecretRequest{})
		_, _ = client.DeleteSecret(ctx, &service.DeleteSecretRequest{})
		_, _ = client.ListSecrets(ctx, &service.ListSecretsRequest{})
		_, _ = client.GetSecretsByPrefix(ctx, &service.GetSecretsByPrefixRequest{})
		_, _ = client.RotateSecret(ctx, &service.RotateSecretRequest{})
		_, _ = client.ListSecretVersions(ctx, &service.ListSecretVersionsRequest{})
		_, _ = client.RollbackSecret(ctx, &service.RollbackSecretRequest{})
		_, _ = client.UpdateSecretStatus(ctx, &service.UpdateSecretStatusRequest{})
		_, _ = client.WatchSecretsByPrefix(ctx, &service.WatchSecretsRequest{})
		_ = client.Close()
	})

	t.Run("custom_func_implementations", func(t *testing.T) {
		storeCalled := false

		client := &MockSecretsClient{
			StoreSecretFunc: func(ctx context.Context, req *service.StoreSecretRequest) (*service.StoreSecretResponse, error) {
				storeCalled = true
				return &service.StoreSecretResponse{
					SecretID: req.Name,
					Version:  1,
				}, nil
			},
		}

		resp, err := client.StoreSecret(context.Background(), &service.StoreSecretRequest{Name: "db/password"})
		require.NoError(t, err)
		assert.True(t, storeCalled)
		assert.Equal(t, "db/password", resp.SecretID)
	})
}

func TestMockCryptoClientInterfaceCompliance(t *testing.T) {
	t.Run("implements_all_methods", func(t *testing.T) {
		var client CryptoClient = &MockCryptoClient{}
		assert.NotNil(t, client)

		ctx := context.Background()

		_, _ = client.KMSEncrypt(ctx, &service.EncryptRequest{})
		_, _ = client.KMSDecrypt(ctx, &service.DecryptRequest{})
		_, _ = client.KMSSign(ctx, &service.SignRequest{})
		_, _ = client.KMSVerify(ctx, &service.VerifyRequest{})
		_, _ = client.KMSHash(ctx, &service.HashRequest{})
		_, _ = client.KMSGenerateHMAC(ctx, &service.HMACRequest{})
		_, _ = client.KMSVerifyHMAC(ctx, &service.VerifyHMACRequest{})
		_, _ = client.KMSRewrap(ctx, &service.RewrapRequest{})
		_, _ = client.KMSGenerateDataKey(ctx, &service.GenerateDataKeyRequest{})
		_, _ = client.KMSGenerateRandom(ctx, &service.GenerateRandomRequest{})
		_ = client.Close()
	})

	t.Run("custom_func_implementations", func(t *testing.T) {
		signCalled := false

		client := &MockCryptoClient{
			KMSSignFunc: func(ctx context.Context, req *service.SignRequest) (*service.SignResponse, error) {
				signCalled = true
				return &service.SignResponse{
					KeyID:     req.KeyID,
					Signature: []byte("test-signature"),
				}, nil
			},
		}

		resp, err := client.KMSSign(context.Background(), &service.SignRequest{KeyID: "signing-key"})
		require.NoError(t, err)
		assert.True(t, signCalled)
		assert.Equal(t, "signing-key", resp.KeyID)
		assert.Equal(t, []byte("test-signature"), resp.Signature)
	})
}

func TestMockBackendClientInterfaceCompliance(t *testing.T) {
	t.Run("implements_all_methods", func(t *testing.T) {
		var client BackendClient = &MockBackendClient{}
		assert.NotNil(t, client)

		ctx := context.Background()

		_, _ = client.KMSBackendStatus(ctx)
		_, _ = client.KMSBackendTest(ctx)
		_ = client.Close()
	})

	t.Run("custom_func_implementations", func(t *testing.T) {
		statusCalled := false

		client := &MockBackendClient{
			KMSBackendStatusFunc: func(ctx context.Context) (*service.BackendStatusResponse, error) {
				statusCalled = true
				return &service.BackendStatusResponse{
					BackendType: "software",
					Status:      "healthy",
				}, nil
			},
		}

		resp, err := client.KMSBackendStatus(context.Background())
		require.NoError(t, err)
		assert.True(t, statusCalled)
		assert.Equal(t, "software", resp.BackendType)
	})
}

func TestMockSafetyClientInterfaceCompliance(t *testing.T) {
	t.Run("implements_all_methods", func(t *testing.T) {
		var client SafetyClient = &MockSafetyClient{}
		assert.NotNil(t, client)

		ctx := context.Background()

		_, _ = client.KMSSafetyCheck(ctx, &service.SafetyCheckRequest{})
		_, _ = client.KMSSafetyReport(ctx, &service.SafetyReportRequest{})
		_ = client.Close()
	})

	t.Run("custom_func_implementations", func(t *testing.T) {
		checkCalled := false

		client := &MockSafetyClient{
			KMSSafetyCheckFunc: func(ctx context.Context, req *service.SafetyCheckRequest) (*service.SafetyCheckResponse, error) {
				checkCalled = true
				return &service.SafetyCheckResponse{
					Items:            []service.SafetyCheckItem{},
					TotalKeysChecked: 10,
				}, nil
			},
		}

		resp, err := client.KMSSafetyCheck(context.Background(), &service.SafetyCheckRequest{ThresholdPercent: 80})
		require.NoError(t, err)
		assert.True(t, checkCalled)
		assert.Equal(t, 10, resp.TotalKeysChecked)
	})
}

func TestMockBackupClientInterfaceCompliance(t *testing.T) {
	t.Run("implements_all_methods", func(t *testing.T) {
		var client BackupClient = &MockBackupClient{}
		assert.NotNil(t, client)

		ctx := context.Background()

		_, _ = client.CreateBackup(ctx, &BackupCreateRequest{})
		_, _ = client.ListBackups(ctx, &BackupListRequest{})
		_, _ = client.RestoreBackup(ctx, &BackupRestoreRequest{})
		_, _ = client.DeleteBackup(ctx, &BackupDeleteRequest{})
		_, _ = client.VerifyBackup(ctx, &BackupVerifyRequest{})
		_ = client.Close()
	})

	t.Run("custom_func_implementations", func(t *testing.T) {
		createCalled := false

		client := &MockBackupClient{
			CreateBackupFunc: func(ctx context.Context, req *BackupCreateRequest) (*cli.BackupCreateResponse, error) {
				createCalled = true
				return &cli.BackupCreateResponse{
					BackupID:  "backup-123",
					Timestamp: time.Now(),
					Success:   true,
				}, nil
			},
		}

		resp, err := client.CreateBackup(context.Background(), &BackupCreateRequest{Name: "daily"})
		require.NoError(t, err)
		assert.True(t, createCalled)
		assert.Equal(t, "backup-123", resp.BackupID)
	})
}

func TestMockTimeSeriesClientInterfaceCompliance(t *testing.T) {
	t.Run("implements_close_method", func(t *testing.T) {
		// Without timeseries build tag, only Close is available
		var client TimeSeriesClient = &MockTimeSeriesClient{}
		assert.NotNil(t, client)

		err := client.Close()
		require.NoError(t, err)
	})

	t.Run("custom_close_implementation", func(t *testing.T) {
		closeCalled := false

		client := &MockTimeSeriesClient{
			CloseFunc: func() error {
				closeCalled = true
				return nil
			},
		}

		err := client.Close()
		require.NoError(t, err)
		assert.True(t, closeCalled)
	})
}

// =============================================================================
// Mock Client Error Handling Tests
// =============================================================================

func TestMockClientErrorHandling(t *testing.T) {
	t.Run("kms_client_error", func(t *testing.T) {
		client := &MockKMSClient{
			KMSGenerateKeyFunc: func(ctx context.Context, req *service.GenerateKeyRequest) (*service.GenerateKeyResponse, error) {
				return nil, errors.New(errors.ErrInvalidInput, "test", "invalid key name")
			},
		}

		_, err := client.KMSGenerateKey(context.Background(), &service.GenerateKeyRequest{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid key name")
	})

	t.Run("secrets_client_error", func(t *testing.T) {
		client := &MockSecretsClient{
			GetSecretFunc: func(ctx context.Context, req *service.GetSecretRequest) (*service.GetSecretResponse, error) {
				return nil, errors.New(errors.ErrNotFound, "test", "secret not found")
			},
		}

		_, err := client.GetSecret(context.Background(), &service.GetSecretRequest{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "secret not found")
	})

	t.Run("crypto_client_error", func(t *testing.T) {
		client := &MockCryptoClient{
			KMSSignFunc: func(ctx context.Context, req *service.SignRequest) (*service.SignResponse, error) {
				return nil, errors.New(errors.ErrForbidden, "test", "signing not allowed")
			},
		}

		_, err := client.KMSSign(context.Background(), &service.SignRequest{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "signing not allowed")
	})

	t.Run("backend_client_error", func(t *testing.T) {
		client := &MockBackendClient{
			KMSBackendTestFunc: func(ctx context.Context) (*service.BackendTestResponse, error) {
				return nil, errors.New(errors.ErrUnavailable, "test", "backend unavailable")
			},
		}

		_, err := client.KMSBackendTest(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "backend unavailable")
	})

	t.Run("safety_client_error", func(t *testing.T) {
		client := &MockSafetyClient{
			KMSSafetyReportFunc: func(ctx context.Context, req *service.SafetyReportRequest) (*service.SafetyReportResponse, error) {
				return nil, errors.New(errors.ErrInternal, "test", "internal error")
			},
		}

		_, err := client.KMSSafetyReport(context.Background(), &service.SafetyReportRequest{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "internal error")
	})

	t.Run("backup_client_error", func(t *testing.T) {
		client := &MockBackupClient{
			RestoreBackupFunc: func(ctx context.Context, req *BackupRestoreRequest) (*cli.BackupRestoreResponse, error) {
				return nil, errors.New(errors.ErrNotFound, "test", "backup not found")
			},
		}

		_, err := client.RestoreBackup(context.Background(), &BackupRestoreRequest{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "backup not found")
	})

	t.Run("timeseries_client_close_error", func(t *testing.T) {
		client := &MockTimeSeriesClient{
			CloseFunc: func() error {
				return errors.New(errors.ErrInternal, "test", "close failed")
			},
		}

		err := client.Close()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "close failed")
	})
}

// =============================================================================
// Mock Client Default Behavior Tests
// =============================================================================

func TestMockClientDefaultBehavior(t *testing.T) {
	t.Run("kms_client_defaults", func(t *testing.T) {
		client := &MockKMSClient{}

		// Without custom funcs, should return empty responses (not errors for most)
		resp, err := client.KMSGenerateKey(context.Background(), &service.GenerateKeyRequest{})
		require.NoError(t, err)
		assert.NotNil(t, resp)

		err = client.Close()
		require.NoError(t, err)
	})

	t.Run("crypto_client_defaults", func(t *testing.T) {
		client := &MockCryptoClient{}

		// Sign should return mock signature by default
		resp, err := client.KMSSign(context.Background(), &service.SignRequest{KeyID: "test"})
		require.NoError(t, err)
		assert.Equal(t, "test", resp.KeyID)
		assert.NotEmpty(t, resp.Signature)

		// Verify should return true by default
		verifyResp, err := client.KMSVerify(context.Background(), &service.VerifyRequest{KeyID: "test"})
		require.NoError(t, err)
		assert.True(t, verifyResp.Valid)
	})

	t.Run("secrets_client_defaults_unimplemented", func(t *testing.T) {
		client := &MockSecretsClient{}

		// Secrets methods return unimplemented by default
		_, err := client.StoreSecret(context.Background(), &service.StoreSecretRequest{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not yet implemented")
	})

	t.Run("backend_client_defaults_unimplemented", func(t *testing.T) {
		client := &MockBackendClient{}

		// Backend methods return unimplemented by default
		_, err := client.KMSBackendStatus(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not yet implemented")
	})

	t.Run("backup_client_verify_default", func(t *testing.T) {
		client := &MockBackupClient{}

		// VerifyBackup has a default implementation
		resp, err := client.VerifyBackup(context.Background(), &BackupVerifyRequest{BackupID: "test"})
		require.NoError(t, err)
		assert.Equal(t, "test", resp.BackupID)
		assert.True(t, resp.Valid)
	})
}

// =============================================================================
// Factory Function Tests
// =============================================================================

func TestFactoryFunctionReplacements(t *testing.T) {
	t.Run("replace_kms_factory", func(t *testing.T) {
		origFactory := kmsClientFactory
		defer func() { kmsClientFactory = origFactory }()

		customClient := &MockKMSClient{
			KMSGenerateKeyFunc: func(ctx context.Context, req *service.GenerateKeyRequest) (*service.GenerateKeyResponse, error) {
				return &service.GenerateKeyResponse{
					Key: &service.KeyHandle{KeyID: "factory-test-key"},
				}, nil
			},
		}

		kmsClientFactory = func() (KMSClient, error) {
			return customClient, nil
		}

		client, err := kmsClientFactory()
		require.NoError(t, err)

		resp, err := client.KMSGenerateKey(context.Background(), &service.GenerateKeyRequest{})
		require.NoError(t, err)
		assert.Equal(t, "factory-test-key", resp.Key.KeyID)
	})

	t.Run("factory_returns_error", func(t *testing.T) {
		origFactory := kmsClientFactory
		defer func() { kmsClientFactory = origFactory }()

		kmsClientFactory = func() (KMSClient, error) {
			return nil, errors.New(errors.ErrUnavailable, "test", "connection refused")
		}

		_, err := kmsClientFactory()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "connection refused")
	})
}

// =============================================================================
// Context Propagation Tests
// =============================================================================

func TestMockClientContextPropagation(t *testing.T) {
	t.Run("context_with_timeout", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		client := &MockKMSClient{
			KMSGenerateKeyFunc: func(innerCtx context.Context, req *service.GenerateKeyRequest) (*service.GenerateKeyResponse, error) {
				// Verify context is passed through
				deadline, ok := innerCtx.Deadline()
				assert.True(t, ok)
				assert.False(t, deadline.IsZero())
				return &service.GenerateKeyResponse{Key: &service.KeyHandle{}}, nil
			},
		}

		_, err := client.KMSGenerateKey(ctx, &service.GenerateKeyRequest{})
		require.NoError(t, err)
	})

	t.Run("context_with_cancel", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())

		client := &MockKMSClient{
			KMSGenerateKeyFunc: func(innerCtx context.Context, req *service.GenerateKeyRequest) (*service.GenerateKeyResponse, error) {
				select {
				case <-innerCtx.Done():
					return nil, innerCtx.Err()
				default:
					return &service.GenerateKeyResponse{Key: &service.KeyHandle{}}, nil
				}
			},
		}

		// First call should succeed
		_, err := client.KMSGenerateKey(ctx, &service.GenerateKeyRequest{})
		require.NoError(t, err)

		// Cancel and verify subsequent call detects cancellation
		cancel()
		_, err = client.KMSGenerateKey(ctx, &service.GenerateKeyRequest{})
		require.Error(t, err)
		assert.Equal(t, context.Canceled, err)
	})

	t.Run("context_with_values", func(t *testing.T) {
		type contextKey string
		const requestIDKey contextKey = "request_id"

		ctx := context.WithValue(context.Background(), requestIDKey, "req-123")

		client := &MockKMSClient{
			KMSGenerateKeyFunc: func(innerCtx context.Context, req *service.GenerateKeyRequest) (*service.GenerateKeyResponse, error) {
				// Verify context value is passed through
				requestID := innerCtx.Value(requestIDKey)
				assert.Equal(t, "req-123", requestID)
				return &service.GenerateKeyResponse{Key: &service.KeyHandle{}}, nil
			},
		}

		_, err := client.KMSGenerateKey(ctx, &service.GenerateKeyRequest{})
		require.NoError(t, err)
	})
}

// =============================================================================
// Concurrent Access Tests
// =============================================================================

func TestMockClientConcurrentAccess(t *testing.T) {
	t.Run("concurrent_reads", func(t *testing.T) {
		client := &MockKMSClient{
			KMSGetKeyFunc: func(ctx context.Context, req *service.GetKeyRequest) (*service.GetKeyResponse, error) {
				return &service.GetKeyResponse{
					Metadata: &service.KeyMetadata{KeyID: req.KeyID},
				}, nil
			},
		}

		done := make(chan struct{})
		for i := 0; i < 10; i++ {
			go func(id int) {
				defer func() { done <- struct{}{} }()
				keyID := string(rune('A' + id))
				resp, err := client.KMSGetKey(context.Background(), &service.GetKeyRequest{KeyID: keyID})
				require.NoError(t, err)
				assert.Equal(t, keyID, resp.Metadata.KeyID)
			}(i)
		}

		// Wait for all goroutines
		for i := 0; i < 10; i++ {
			<-done
		}
	})
}
