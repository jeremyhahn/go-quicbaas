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

package mocks

import (
	"context"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/messaging"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Interface Compliance Tests
// =============================================================================

// TestMockQuicBaaSImplementsInterface verifies MockQuicBaaS implements service.QuicBaaS.
func TestMockQuicBaaSImplementsInterface(t *testing.T) {
	var _ service.QuicBaaS = (*MockQuicBaaS)(nil)
}

// TestMockKMSServiceImplementsInterface verifies MockKMSService implements service.KMSService.
func TestMockKMSServiceImplementsInterface(t *testing.T) {
	var _ service.KMSService = (*MockKMSService)(nil)
}

// TestMockSecretsServiceImplementsInterface verifies MockSecretsService implements service.SecretsService.
func TestMockSecretsServiceImplementsInterface(t *testing.T) {
	var _ service.SecretsService = (*MockSecretsService)(nil)
}

// TestMockCryptoServiceImplementsInterface verifies MockCryptoService implements service.CryptoService.
func TestMockCryptoServiceImplementsInterface(t *testing.T) {
	var _ service.CryptoService = (*MockCryptoService)(nil)
}

// TestMockVectorServiceImplementsInterface verifies MockVectorService implements service.VectorService.
func TestMockVectorServiceImplementsInterface(t *testing.T) {
	var _ service.VectorService = (*MockVectorService)(nil)
}

// TestMockSchedulerServiceImplementsInterface verifies MockSchedulerService implements service.SchedulerService.
func TestMockSchedulerServiceImplementsInterface(t *testing.T) {
	var _ service.SchedulerService = (*MockSchedulerService)(nil)
}

// TestMockMessagingServiceImplementsInterface verifies MockMessagingService implements messaging.Service.
func TestMockMessagingServiceImplementsInterface(t *testing.T) {
	var _ messaging.Service = (*MockMessagingService)(nil)
}

// TestMockEmbeddingServiceImplementsInterface verifies MockEmbeddingService implements service.EmbeddingService.
func TestMockEmbeddingServiceImplementsInterface(t *testing.T) {
	var _ service.EmbeddingService = (*MockEmbeddingService)(nil)
}

// TestMockOllamaServiceImplementsInterface verifies MockOllamaService implements service.OllamaService.
func TestMockOllamaServiceImplementsInterface(t *testing.T) {
	var _ service.OllamaService = (*MockOllamaService)(nil)
}

// TestMockCertificateServiceImplementsInterface verifies MockCertificateService implements service.CertificateService.
func TestMockCertificateServiceImplementsInterface(t *testing.T) {
	var _ service.CertificateService = (*MockCertificateService)(nil)
}

// TestMockDynamicSecretsServiceImplementsInterface verifies MockDynamicSecretsService implements service.DynamicSecretsService.
func TestMockDynamicSecretsServiceImplementsInterface(t *testing.T) {
	var _ service.DynamicSecretsService = (*MockDynamicSecretsService)(nil)
}

// TestMockObjectStorageServiceImplementsInterface verifies MockObjectStorageService implements service.ObjectStorageService.
func TestMockObjectStorageServiceImplementsInterface(t *testing.T) {
	var _ service.ObjectStorageService = (*MockObjectStorageService)(nil)
}

// TestMockObjectStorageAdminServiceImplementsInterface verifies MockObjectStorageAdminService implements service.ObjectStorageAdminService.
func TestMockObjectStorageAdminServiceImplementsInterface(t *testing.T) {
	var _ service.ObjectStorageAdminService = (*MockObjectStorageAdminService)(nil)
}

// TestMockParameterStoreServiceImplementsInterface verifies MockParameterStoreService implements service.ParameterStoreService.
func TestMockParameterStoreServiceImplementsInterface(t *testing.T) {
	var _ service.ParameterStoreService = (*MockParameterStoreService)(nil)
}

// TestMockObjectTransferServiceImplementsInterface verifies MockObjectTransferService implements service.ObjectTransferService.
func TestMockObjectTransferServiceImplementsInterface(t *testing.T) {
	var _ service.ObjectTransferService = (*MockObjectTransferService)(nil)
}

// =============================================================================
// Functional Tests
// =============================================================================

// TestMockQuicBaaSDefaultBehavior verifies default method implementations return empty/nil values.
func TestMockQuicBaaSDefaultBehavior(t *testing.T) {
	mock := NewMockQuicBaaS()
	ctx := context.Background()

	// Test Put returns empty response
	putResp, err := mock.Put(ctx, &service.PutRequest{EntityType: "test", ID: 1})
	if err != nil {
		t.Errorf("Put() returned unexpected error: %v", err)
	}
	if putResp == nil {
		t.Error("Put() returned nil response")
	}

	// Test Get returns empty response
	getResp, err := mock.Get(ctx, &service.GetRequest{EntityType: "test", ID: 1})
	if err != nil {
		t.Errorf("Get() returned unexpected error: %v", err)
	}
	if getResp == nil {
		t.Error("Get() returned nil response")
	}

	// Test Delete returns empty response
	delResp, err := mock.Delete(ctx, &service.DeleteRequest{EntityType: "test", ID: 1})
	if err != nil {
		t.Errorf("Delete() returned unexpected error: %v", err)
	}
	if delResp == nil {
		t.Error("Delete() returned nil response")
	}

	// Test Health returns healthy status by default
	healthResp, err := mock.Health(ctx, &service.HealthRequest{})
	if err != nil {
		t.Errorf("Health() returned unexpected error: %v", err)
	}
	if healthResp.Status != service.HealthStatusHealthy {
		t.Errorf("Health() expected healthy status, got %v", healthResp.Status)
	}

	// Test Liveness returns alive by default
	livenessResp, err := mock.Liveness(ctx, &service.LivenessRequest{})
	if err != nil {
		t.Errorf("Liveness() returned unexpected error: %v", err)
	}
	if !livenessResp.Alive {
		t.Error("Liveness() expected Alive=true by default")
	}

	// Test Readiness returns ready by default
	readinessResp, err := mock.Readiness(ctx, &service.ReadinessRequest{})
	if err != nil {
		t.Errorf("Readiness() returned unexpected error: %v", err)
	}
	if !readinessResp.Ready {
		t.Error("Readiness() expected Ready=true by default")
	}

	// Test Close returns nil by default
	if err := mock.Close(); err != nil {
		t.Errorf("Close() returned unexpected error: %v", err)
	}

	// Test sub-services return nil by default
	if mock.KMS() != nil {
		t.Error("KMS() expected nil by default")
	}
	if mock.Secrets() != nil {
		t.Error("Secrets() expected nil by default")
	}
	if mock.Crypto() != nil {
		t.Error("Crypto() expected nil by default")
	}
	if mock.Vector() != nil {
		t.Error("Vector() expected nil by default")
	}
	if mock.Scheduler() != nil {
		t.Error("Scheduler() expected nil by default")
	}
	if mock.Messaging() != nil {
		t.Error("Messaging() expected nil by default")
	}
}

// TestMockQuicBaaSCustomFunctions verifies configurable function fields work correctly.
func TestMockQuicBaaSCustomFunctions(t *testing.T) {
	ctx := context.Background()
	expectedErr := &testError{msg: "test error"}

	mock := NewMockQuicBaaS()

	// Configure custom Put behavior
	mock.PutFunc = func(ctx context.Context, req *service.PutRequest) (*service.PutResponse, error) {
		return &service.PutResponse{ID: 42, Version: 1}, nil
	}

	putResp, err := mock.Put(ctx, &service.PutRequest{EntityType: "test"})
	if err != nil {
		t.Errorf("Put() returned unexpected error: %v", err)
	}
	if putResp.ID != 42 {
		t.Errorf("Put() expected ID=42, got %d", putResp.ID)
	}
	if putResp.Version != 1 {
		t.Errorf("Put() expected Version=1, got %d", putResp.Version)
	}

	// Configure error behavior
	mock.GetFunc = func(ctx context.Context, req *service.GetRequest) (*service.GetResponse, error) {
		return nil, expectedErr
	}

	_, err = mock.Get(ctx, &service.GetRequest{EntityType: "test", ID: 1})
	if err != expectedErr {
		t.Errorf("Get() expected error %v, got %v", expectedErr, err)
	}
}

// TestMockQuicBaaSHelperMethods verifies helper methods work correctly.
func TestMockQuicBaaSHelperMethods(t *testing.T) {
	ctx := context.Background()

	// Test WithPutResponse helper
	mock := NewMockQuicBaaS().WithPutResponse(&service.PutResponse{ID: 100}, nil)
	resp, err := mock.Put(ctx, &service.PutRequest{})
	if err != nil {
		t.Errorf("WithPutResponse: unexpected error: %v", err)
	}
	if resp.ID != 100 {
		t.Errorf("WithPutResponse: expected ID=100, got %d", resp.ID)
	}

	// Test WithGetResponse helper
	mock = NewMockQuicBaaS().WithGetResponse(&service.GetResponse{Found: true, Data: []byte("test")}, nil)
	getResp, err := mock.Get(ctx, &service.GetRequest{})
	if err != nil {
		t.Errorf("WithGetResponse: unexpected error: %v", err)
	}
	if !getResp.Found {
		t.Error("WithGetResponse: expected Found=true")
	}
	if string(getResp.Data) != "test" {
		t.Errorf("WithGetResponse: expected Data='test', got %s", string(getResp.Data))
	}

	// Test WithHealthyStatus helper
	mock = NewMockQuicBaaS().WithHealthyStatus()
	healthResp, _ := mock.Health(ctx, &service.HealthRequest{})
	if healthResp.Status != service.HealthStatusHealthy {
		t.Error("WithHealthyStatus: expected healthy status")
	}

	// Test WithUnhealthyStatus helper
	mock = NewMockQuicBaaS().WithUnhealthyStatus("maintenance mode")
	readinessResp, _ := mock.Readiness(ctx, &service.ReadinessRequest{})
	if readinessResp.Ready {
		t.Error("WithUnhealthyStatus: expected Ready=false")
	}
	if readinessResp.Reason != "maintenance mode" {
		t.Errorf("WithUnhealthyStatus: expected reason='maintenance mode', got '%s'", readinessResp.Reason)
	}
}

// TestMockQuicBaaSWithAllSubservices verifies subservices are properly initialized.
func TestMockQuicBaaSWithAllSubservices(t *testing.T) {
	mock := NewMockQuicBaaSWithAllSubservices()

	if mock.KMS() == nil {
		t.Error("KMS() should not be nil when created with all subservices")
	}
	if mock.Secrets() == nil {
		t.Error("Secrets() should not be nil when created with all subservices")
	}
	if mock.Crypto() == nil {
		t.Error("Crypto() should not be nil when created with all subservices")
	}
	if mock.Vector() == nil {
		t.Error("Vector() should not be nil when created with all subservices")
	}
	if mock.Scheduler() == nil {
		t.Error("Scheduler() should not be nil when created with all subservices")
	}
	if mock.Messaging() == nil {
		t.Error("Messaging() should not be nil when created with all subservices")
	}
	if mock.Embedding() == nil {
		t.Error("Embedding() should not be nil when created with all subservices")
	}
	if mock.Ollama() == nil {
		t.Error("Ollama() should not be nil when created with all subservices")
	}
	if mock.Certificate() == nil {
		t.Error("Certificate() should not be nil when created with all subservices")
	}
	if mock.DynamicSecrets() == nil {
		t.Error("DynamicSecrets() should not be nil when created with all subservices")
	}
	if mock.ObjectStorageService() == nil {
		t.Error("ObjectStorageService() should not be nil when created with all subservices")
	}
	if mock.ObjectStorageAdminService() == nil {
		t.Error("ObjectStorageAdminService() should not be nil when created with all subservices")
	}
	if mock.ParameterStoreService() == nil {
		t.Error("ParameterStoreService() should not be nil when created with all subservices")
	}
	if mock.ObjectTransferService() == nil {
		t.Error("ObjectTransferService() should not be nil when created with all subservices")
	}
}

// TestMockSubserviceFunctionOverridesInstance verifies function fields take precedence.
func TestMockSubserviceFunctionOverridesInstance(t *testing.T) {
	ctx := context.Background()
	customKMS := &MockKMSService{}

	mock := NewMockQuicBaaSWithAllSubservices()
	mock.KMSServiceFunc = func() service.KMSService {
		return customKMS
	}

	// Function should take precedence over MockKMSService field
	if mock.KMS() != customKMS {
		t.Error("KMSServiceFunc should override MockKMSService")
	}

	// Test custom function behavior on subservice
	customKMS.GenerateKeyFunc = func(ctx context.Context, req *service.GenerateKeyRequest) (*service.GenerateKeyResponse, error) {
		return &service.GenerateKeyResponse{
			Key: &service.KeyHandle{
				KeyID:     "custom-key",
				Algorithm: service.KeyAlgAES256GCM,
				Status:    service.KeyStatusEnabled,
				CreatedAt: time.Now(),
			},
			Timestamp: time.Now(),
		}, nil
	}

	kmsService := mock.KMS().(*MockKMSService)
	resp, _ := kmsService.GenerateKey(ctx, &service.GenerateKeyRequest{})
	if resp.Key == nil {
		t.Fatal("Expected Key to be non-nil")
	}
	if resp.Key.KeyID != "custom-key" {
		t.Errorf("Expected KeyID='custom-key', got '%s'", resp.Key.KeyID)
	}
}

// TestMockVectorServiceOperations verifies vector service mock operations.
func TestMockVectorServiceOperations(t *testing.T) {
	ctx := context.Background()
	mock := &MockVectorService{}

	// Test default behavior
	resp, err := mock.CreateNamespace(ctx, &service.CreateNamespaceRequest{Namespace: "test"})
	if err != nil {
		t.Errorf("CreateNamespace() returned unexpected error: %v", err)
	}
	if !resp.Success {
		t.Error("CreateNamespace() expected Success=true by default")
	}

	// Test custom search behavior
	mock.SearchSimilarFunc = func(ctx context.Context, req *service.SearchSimilarRequest) (*service.SearchSimilarResponse, error) {
		return &service.SearchSimilarResponse{
			Results: []*service.VectorSearchResult{
				{ID: 1, Score: 0.95},
				{ID: 2, Score: 0.88},
			},
		}, nil
	}

	searchResp, _ := mock.SearchSimilar(ctx, &service.SearchSimilarRequest{
		Namespace:   "test",
		QueryVector: []float32{0.1, 0.2, 0.3},
		K:           2,
	})
	if len(searchResp.Results) != 2 {
		t.Errorf("SearchSimilar() expected 2 results, got %d", len(searchResp.Results))
	}
	if searchResp.Results[0].Score != 0.95 {
		t.Errorf("SearchSimilar() expected first result score=0.95, got %f", searchResp.Results[0].Score)
	}
}

// TestMockSchedulerServiceOperations verifies scheduler service mock operations.
func TestMockSchedulerServiceOperations(t *testing.T) {
	ctx := context.Background()
	mock := &MockSchedulerService{}

	// Test default behavior
	statusResp, err := mock.GetStatus(ctx)
	if err != nil {
		t.Errorf("GetStatus() returned unexpected error: %v", err)
	}
	if !statusResp.Running {
		t.Error("GetStatus() expected Running=true by default")
	}

	// Test custom job creation
	mock.CreateJobFunc = func(ctx context.Context, req *service.CreateSchedulerJobRequest) (*service.CreateSchedulerJobResponse, error) {
		return &service.CreateSchedulerJobResponse{
			Job: &service.SchedulerJob{
				ID:      123,
				Name:    req.Name,
				Handler: req.Handler,
				Status:  service.SchedulerJobStatusPending,
			},
		}, nil
	}

	createResp, _ := mock.CreateJob(ctx, &service.CreateSchedulerJobRequest{
		Name:    "test-job",
		Handler: "test.handler",
	})
	if createResp.Job.ID != 123 {
		t.Errorf("CreateJob() expected Job.ID=123, got %d", createResp.Job.ID)
	}
	if createResp.Job.Status != service.SchedulerJobStatusPending {
		t.Errorf("CreateJob() expected Status=pending, got %s", createResp.Job.Status)
	}
}

// TestMockParameterStoreServiceOperations verifies parameter store mock operations.
func TestMockParameterStoreServiceOperations(t *testing.T) {
	ctx := context.Background()
	mock := &MockParameterStoreService{}

	// Test default behavior
	resp, err := mock.GetParameter(ctx, &interfaces.GetParameterRequest{Path: "/test/param"})
	if err != nil {
		t.Errorf("GetParameter() returned unexpected error: %v", err)
	}
	if resp == nil {
		t.Error("GetParameter() returned nil response")
	}

	// Test custom set parameter
	mock.SetParameterFunc = func(ctx context.Context, req *interfaces.SetParameterRequest) (*interfaces.SetParameterResponse, error) {
		return &interfaces.SetParameterResponse{
			Path:      req.Path,
			Created:   true,
			Timestamp: time.Now(),
		}, nil
	}

	setResp, _ := mock.SetParameter(ctx, &interfaces.SetParameterRequest{
		Path:  "/test/param",
		Value: []byte("test-value"),
	})
	if setResp.Path != "/test/param" {
		t.Errorf("SetParameter() expected Path='/test/param', got '%s'", setResp.Path)
	}
	if !setResp.Created {
		t.Error("SetParameter() expected Created=true")
	}
}

// TestMockMessagingServiceOperations verifies messaging service mock operations.
func TestMockMessagingServiceOperations(t *testing.T) {
	ctx := context.Background()
	mock := &MockMessagingService{}

	// Test default behavior
	if !mock.IsRunning() {
		t.Error("IsRunning() expected true by default")
	}

	// Test channel creation
	channel, err := mock.CreateChannel(ctx, "test-channel")
	if err != nil {
		t.Errorf("CreateChannel() returned unexpected error: %v", err)
	}
	if channel.Name != "test-channel" {
		t.Errorf("CreateChannel() expected Name='test-channel', got '%s'", channel.Name)
	}

	// Test custom publish
	publishCalled := false
	mock.PublishFunc = func(ctx context.Context, channel string, payload interface{}, opts ...messaging.PublishOption) (*messaging.PublishResult, error) {
		publishCalled = true
		if channel != "events" {
			t.Errorf("Publish() expected channel='events', got '%s'", channel)
		}
		return &messaging.PublishResult{MessageID: "mock-id"}, nil
	}

	_, err = mock.Publish(ctx, "events", map[string]string{"type": "test"})
	if err != nil {
		t.Errorf("Publish() returned unexpected error: %v", err)
	}
	if !publishCalled {
		t.Error("Publish() was not called")
	}
}

// =============================================================================
// Error Helper
// =============================================================================

type testError struct {
	msg string
}

func (e *testError) Error() string {
	return e.msg
}
