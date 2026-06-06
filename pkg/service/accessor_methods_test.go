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

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/messaging"
	"github.com/jeremyhahn/go-quicbaas/pkg/watch"
)

// =============================================================================
// Accessor Method Tests - QuicBaaS service accessors for sub-services
// =============================================================================

func TestQuicBaaS_KMSAccessor_ReturnsNilWhenNotConfigured(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	result := svc.KMS()
	assert.Nil(t, result)
}

func TestQuicBaaS_KMSManagerAccessor_ReturnsNilWhenNotConfigured(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	result := svc.KMSManager()
	assert.Nil(t, result)
}

func TestQuicBaaS_SecretsAccessor_ReturnsNilWhenNotConfigured(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	result := svc.Secrets()
	assert.Nil(t, result)
}

func TestQuicBaaS_CryptoAccessor_ReturnsNilWhenNotConfigured(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	result := svc.Crypto()
	assert.Nil(t, result)
}

func TestQuicBaaS_VectorAccessor_ReturnsNilWhenNotConfigured(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	result := svc.Vector()
	assert.Nil(t, result)
}

func TestQuicBaaS_EmbeddingAccessor_ReturnsNilWhenNotConfigured(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	result := svc.Embedding()
	assert.Nil(t, result)
}

func TestQuicBaaS_OllamaAccessor_ReturnsNilWhenNotConfigured(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	result := svc.Ollama()
	assert.Nil(t, result)
}

func TestQuicBaaS_CertificateAccessor_ReturnsNilWhenNotConfigured(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	result := svc.Certificate()
	assert.Nil(t, result)
}

func TestQuicBaaS_DynamicSecretsAccessor_ReturnsNilWhenNotConfigured(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	result := svc.DynamicSecrets()
	assert.Nil(t, result)
}

func TestQuicBaaS_ObjectStorageServiceAccessor_ReturnsNilWhenNotConfigured(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	result := svc.ObjectStorageService()
	assert.Nil(t, result)
}

func TestQuicBaaS_ObjectStorageAdminServiceAccessor_ReturnsNilWhenNotConfigured(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	result := svc.ObjectStorageAdminService()
	assert.Nil(t, result)
}

func TestQuicBaaS_ParameterStoreServiceAccessor_AutoInitialized(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	// ParameterStoreService is auto-initialized with the raft node
	result := svc.ParameterStoreService()
	assert.NotNil(t, result)
}

func TestQuicBaaS_ObjectTransferServiceAccessor_ReturnsNilWhenNotConfigured(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	result := svc.ObjectTransferService()
	assert.Nil(t, result)
}

func TestQuicBaaS_SchedulerAccessor_ReturnsNilWhenNotConfigured(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	result := svc.Scheduler()
	assert.Nil(t, result)
}

func TestQuicBaaS_MessagingAccessor_ReturnsNilWhenNotConfigured(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	result := svc.Messaging()
	assert.Nil(t, result)
}

func TestQuicBaaS_WatchManagerAccessor_ReturnsNonNil(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	result := svc.WatchManager()
	assert.NotNil(t, result)
	assert.IsType(t, &watch.Manager{}, result)
}

func TestQuicBaaS_DAOAccessor_ReturnsNilWhenNotConfigured(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	impl, ok := svc.(*quicBaaS)
	require.True(t, ok)

	result := impl.DAO()
	assert.Nil(t, result)
}

// =============================================================================
// SetMessagingService Tests
// =============================================================================

// mockMessagingService implements messaging.Service for testing.
type mockMessagingService struct{}

func (m *mockMessagingService) CreateChannel(_ context.Context, _ string, _ ...messaging.CreateChannelOption) (*messaging.Channel, error) {
	return nil, nil
}
func (m *mockMessagingService) DeleteChannel(_ context.Context, _ string) error { return nil }
func (m *mockMessagingService) GetChannel(_ context.Context, _ string) (*messaging.Channel, error) {
	return nil, nil
}
func (m *mockMessagingService) ListChannels(_ context.Context) ([]*messaging.Channel, error) {
	return nil, nil
}
func (m *mockMessagingService) Subscribe(_ context.Context, _ string, _ ...messaging.SubscribeOption) (*messaging.Subscription, error) {
	return nil, nil
}
func (m *mockMessagingService) Unsubscribe(_ context.Context, _ string) error { return nil }
func (m *mockMessagingService) Publish(_ context.Context, _ string, _ interface{}, _ ...messaging.PublishOption) (*messaging.PublishResult, error) {
	return nil, nil
}
func (m *mockMessagingService) PublishDelta(_ context.Context, _ string, _, _ interface{}, _ ...messaging.PublishOption) (*messaging.PublishResult, error) {
	return nil, nil
}
func (m *mockMessagingService) WatchChannel(_ context.Context, _ string, _ ...messaging.SubscribeOption) (*messaging.Subscription, error) {
	return nil, nil
}
func (m *mockMessagingService) Start(_ context.Context) error { return nil }
func (m *mockMessagingService) Stop(_ context.Context) error  { return nil }
func (m *mockMessagingService) IsRunning() bool               { return false }
func (m *mockMessagingService) GetStats() *messaging.Stats    { return nil }

func TestQuicBaaS_SetMessagingService_SetsService(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	// Initially nil
	assert.Nil(t, svc.Messaging())

	// Set service
	ms := &mockMessagingService{}
	svc.SetMessagingService(ms)

	// Verify it is now set
	result := svc.Messaging()
	require.NotNil(t, result)
	assert.Same(t, ms, result)
}

func TestQuicBaaS_SetMessagingService_OverwriteExisting(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	ms1 := &mockMessagingService{}
	ms2 := &mockMessagingService{}

	svc.SetMessagingService(ms1)
	assert.Same(t, ms1, svc.Messaging())

	svc.SetMessagingService(ms2)
	assert.Same(t, ms2, svc.Messaging())
}

// =============================================================================
// KMS Delegation Method Tests - Return error when KMS not configured
// These methods are on the struct, not the interface, so we cast first.
// =============================================================================

func TestQuicBaaS_GenerateKey_ReturnsErrorWhenKMSNotConfigured(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	impl := svc.(*quicBaaS)
	resp, err := impl.GenerateKey(context.Background(), &GenerateKeyRequest{
		Name: "test-key",
	})

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "KMS service not initialized")
}

func TestQuicBaaS_ImportKey_ReturnsErrorWhenKMSNotConfigured(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	impl := svc.(*quicBaaS)
	resp, err := impl.ImportKey(context.Background(), &ImportKeyRequest{
		Name:        "test-key",
		KeyMaterial: []byte("test-material"),
	})

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "KMS service not initialized")
}

func TestQuicBaaS_GetKey_ReturnsErrorWhenKMSNotConfigured(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	impl := svc.(*quicBaaS)
	resp, err := impl.GetKey(context.Background(), &GetKeyRequest{
		KeyID: "test-key-id",
	})

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "KMS service not initialized")
}

func TestQuicBaaS_ListKeys_ReturnsErrorWhenKMSNotConfigured(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	impl := svc.(*quicBaaS)
	resp, err := impl.ListKeys(context.Background(), &ListKeysRequest{})

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "KMS service not initialized")
}

func TestQuicBaaS_RotateKey_ReturnsErrorWhenKMSNotConfigured(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	impl := svc.(*quicBaaS)
	resp, err := impl.RotateKey(context.Background(), &RotateKeyRequest{
		KeyID: "test-key-id",
	})

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "KMS service not initialized")
}

func TestQuicBaaS_RevokeKey_ReturnsErrorWhenKMSNotConfigured(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	impl := svc.(*quicBaaS)
	resp, err := impl.RevokeKey(context.Background(), &RevokeKeyRequest{
		KeyID: "test-key-id",
	})

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "KMS service not initialized")
}

func TestQuicBaaS_DeleteKey_ReturnsErrorWhenKMSNotConfigured(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	impl := svc.(*quicBaaS)
	resp, err := impl.DeleteKey(context.Background(), &DeleteKeyRequest{
		KeyID: "test-key-id",
	})

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "KMS service not initialized")
}

func TestQuicBaaS_GetKeyMetadata_ReturnsErrorWhenKMSNotConfigured(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	impl := svc.(*quicBaaS)
	resp, err := impl.GetKeyMetadata(context.Background(), &GetKeyMetadataRequest{
		KeyID: "test-key-id",
	})

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "KMS service not initialized")
}

// =============================================================================
// Crypto Delegation Method Tests - Return error when Crypto not configured
// =============================================================================

func TestQuicBaaS_Encrypt_ReturnsErrorWhenCryptoNotConfigured(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	impl := svc.(*quicBaaS)
	resp, err := impl.Encrypt(context.Background(), &EncryptRequest{
		KeyID:     "test-key",
		Plaintext: []byte("test-data"),
	})

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "Crypto service not initialized")
}

func TestQuicBaaS_Decrypt_ReturnsErrorWhenCryptoNotConfigured(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	impl := svc.(*quicBaaS)
	resp, err := impl.Decrypt(context.Background(), &DecryptRequest{
		KeyID:      "test-key",
		Ciphertext: []byte("encrypted-data"),
	})

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "Crypto service not initialized")
}

// =============================================================================
// Scheduler Logger Adapter Tests
// =============================================================================

func TestSchedulerLoggerAdapter_Debugf_WithNilLogger(t *testing.T) {
	adapter := &schedulerLoggerAdapter{logger: nil}
	// Should not panic
	adapter.Debugf("test %s", "message")
}

func TestSchedulerLoggerAdapter_Warnf_WithNilLogger(t *testing.T) {
	adapter := &schedulerLoggerAdapter{logger: nil}
	// Should not panic
	adapter.Warnf("test %s", "message")
}

func TestSchedulerLoggerAdapter_Errorf_WithNilLogger(t *testing.T) {
	adapter := &schedulerLoggerAdapter{logger: nil}
	// Should not panic
	adapter.Errorf("test %s", "message")
}

func TestSchedulerLoggerAdapter_Debugf_WithLogger(t *testing.T) {
	log := createTestLogger(t)
	adapter := &schedulerLoggerAdapter{logger: log}
	// Should not panic
	adapter.Debugf("debug %s", "msg")
}

func TestSchedulerLoggerAdapter_Warnf_WithLogger(t *testing.T) {
	log := createTestLogger(t)
	adapter := &schedulerLoggerAdapter{logger: log}
	// Should not panic
	adapter.Warnf("warn %s", "msg")
}

func TestSchedulerLoggerAdapter_Errorf_WithLogger(t *testing.T) {
	log := createTestLogger(t)
	adapter := &schedulerLoggerAdapter{logger: log}
	// Should not panic
	adapter.Errorf("error %s", "msg")
}

// =============================================================================
// KMS Grace Period Adapter Tests
// =============================================================================

func TestKMSGracePeriodAdapter_SetJobID(t *testing.T) {
	adapter := &kmsGracePeriodAdapter{}
	adapter.SetJobID("job-123")
	assert.Equal(t, "job-123", adapter.jobID)
}

func TestKMSGracePeriodAdapter_SetJobID_Overwrite(t *testing.T) {
	adapter := &kmsGracePeriodAdapter{}
	adapter.SetJobID("job-1")
	adapter.SetJobID("job-2")
	assert.Equal(t, "job-2", adapter.jobID)
}
