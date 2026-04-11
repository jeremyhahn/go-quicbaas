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

// Package testutil provides shared test utilities and mock implementations
// for use across QuicBaaS API test packages (gRPC, REST, MCP).
package testutil

import (
	"context"

	"github.com/stretchr/testify/mock"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/messaging"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/security"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/watch"
)

// MockQuicBaaS is a testify mock implementation of service.QuicBaaS.
// It embeds mock.Mock and delegates all method calls through testify's
// expectation framework, enabling m.On("MethodName", ...).Return(...)
// patterns in test code.
//
// For sub-services without method name conflicts (Secrets, Crypto, KMS, etc.),
// the accessor returns the mock itself so tests can set up expectations
// directly on the MockQuicBaaS instance.
//
// For sub-services with conflicting methods (Embedding, Ollama, Certificate),
// dedicated fields are provided so a separate mock can be injected.
type MockQuicBaaS struct {
	mock.Mock

	// Sub-service fields for interfaces with conflicting method signatures.
	// These are returned by the corresponding accessor methods.
	CertificateSvc   service.CertificateService
	EmbeddingService service.EmbeddingService
	OllamaService    service.OllamaService

	// Optional override fields. When set, the accessor returns these
	// instead of the mock itself.
	CryptoService     service.CryptoService
	DynamicSecretsSvc service.DynamicSecretsService
	KMSService        service.KMSService
	ParameterStoreSvc service.ParameterStoreService
	VectorService     service.VectorService
	ClusterDEKSvc     service.ClusterDEKService

	// DisableKMS causes KMS() to return nil, forcing handlers into the
	// KMSManager() fallback path for testing.
	DisableKMS bool
}

// Ensure MockQuicBaaS implements service.QuicBaaS at compile time.
var _ service.QuicBaaS = (*MockQuicBaaS)(nil)

// NewMockQuicBaaS creates a new MockQuicBaaS with testify mock infrastructure.
func NewMockQuicBaaS() *MockQuicBaaS {
	return &MockQuicBaaS{}
}

// =============================================================================
// KV Service Methods (part of service.QuicBaaS)
// =============================================================================

func (m *MockQuicBaaS) Put(ctx context.Context, req *service.PutRequest) (*service.PutResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.PutResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) Get(ctx context.Context, req *service.GetRequest) (*service.GetResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.GetResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) Delete(ctx context.Context, req *service.DeleteRequest) (*service.DeleteResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.DeleteResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) List(ctx context.Context, req *service.ListRequest, stream chan<- *service.ListResponse) error {
	args := m.Called(ctx, req, stream)
	return args.Error(0)
}

func (m *MockQuicBaaS) Watch(ctx context.Context, req *service.WatchRequest, stream chan<- *service.WatchResponse) (int64, error) {
	args := m.Called(ctx, req, stream)
	return args.Get(0).(int64), args.Error(1)
}

func (m *MockQuicBaaS) ListEntityTypes(ctx context.Context, req *service.ListEntityTypesRequest) (*service.ListEntityTypesResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.ListEntityTypesResponse)
	return resp, args.Error(1)
}

// =============================================================================
// Batch Service Methods (part of service.QuicBaaS)
// =============================================================================

func (m *MockQuicBaaS) BatchPut(ctx context.Context, req *service.BatchPutRequest) (*service.BatchPutResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.BatchPutResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) BatchGet(ctx context.Context, req *service.BatchGetRequest) (*service.BatchGetResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.BatchGetResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) BatchDelete(ctx context.Context, req *service.BatchDeleteRequest) (*service.BatchDeleteResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.BatchDeleteResponse)
	return resp, args.Error(1)
}

// =============================================================================
// Transaction Service Methods (part of service.QuicBaaS)
// =============================================================================

func (m *MockQuicBaaS) Transaction(ctx context.Context, req *service.TransactionRequest) (*service.TransactionResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.TransactionResponse)
	return resp, args.Error(1)
}

// =============================================================================
// Query Service Methods (part of service.QuicBaaS)
// =============================================================================

func (m *MockQuicBaaS) Query(ctx context.Context, req *service.QueryRequest, stream chan<- *service.QueryResponse) error {
	args := m.Called(ctx, req, stream)
	return args.Error(0)
}

func (m *MockQuicBaaS) ScanIndex(ctx context.Context, req *service.ScanRequest, stream chan<- *service.ScanResponse) error {
	args := m.Called(ctx, req, stream)
	return args.Error(0)
}

// =============================================================================
// Cluster Service Methods (part of service.QuicBaaS)
// =============================================================================

func (m *MockQuicBaaS) ClusterStatus(ctx context.Context, req *service.ClusterStatusRequest) (*service.ClusterStatusResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.ClusterStatusResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) GetLeader(ctx context.Context, req *service.GetLeaderRequest) (*service.GetLeaderResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.GetLeaderResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) AddNode(ctx context.Context, req *service.AddNodeRequest) (*service.AddNodeResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.AddNodeResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) RemoveNode(ctx context.Context, req *service.RemoveNodeRequest) (*service.RemoveNodeResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.RemoveNodeResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) CreateShard(ctx context.Context, req *service.CreateShardRequest) (*service.CreateShardResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.CreateShardResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) RebalanceCluster(ctx context.Context, req *service.RebalanceClusterRequest) (*service.RebalanceClusterResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.RebalanceClusterResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) GetRebalanceProgress(ctx context.Context, req *service.GetRebalanceProgressRequest) (*service.GetRebalanceProgressResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.GetRebalanceProgressResponse)
	return resp, args.Error(1)
}

// =============================================================================
// Backup Service Methods (part of service.QuicBaaS)
// =============================================================================

func (m *MockQuicBaaS) CreateBackup(ctx context.Context, req *service.CreateBackupRequest) (*service.CreateBackupResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.CreateBackupResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) ListBackups(ctx context.Context, req *service.ListBackupsRequest) (*service.ListBackupsResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.ListBackupsResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) RestoreBackup(ctx context.Context, req *service.RestoreBackupRequest) (*service.RestoreBackupResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.RestoreBackupResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) DeleteBackup(ctx context.Context, req *service.DeleteBackupRequest) (*service.DeleteBackupResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.DeleteBackupResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) VerifyBackup(ctx context.Context, req *service.VerifyBackupRequest) (*service.VerifyBackupResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.VerifyBackupResponse)
	return resp, args.Error(1)
}

// =============================================================================
// Auth Service Methods (part of service.QuicBaaS)
// =============================================================================

func (m *MockQuicBaaS) Login(ctx context.Context, req *service.LoginRequest) (*service.LoginResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.LoginResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) Logout(ctx context.Context, req *service.LogoutRequest) (*service.LogoutResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.LogoutResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) RefreshToken(ctx context.Context, req *service.RefreshTokenRequest) (*service.RefreshTokenResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.RefreshTokenResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) ValidateToken(ctx context.Context, req *service.ValidateTokenRequest) (*service.ValidateTokenResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.ValidateTokenResponse)
	return resp, args.Error(1)
}

// =============================================================================
// RBAC Service Methods (part of service.QuicBaaS)
// =============================================================================

func (m *MockQuicBaaS) CreateUser(ctx context.Context, req *service.AuthCreateUserRequest) (*service.AuthCreateUserResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.AuthCreateUserResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) DeleteUser(ctx context.Context, req *service.AuthDeleteUserRequest) (*service.AuthDeleteUserResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.AuthDeleteUserResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) ListUsers(ctx context.Context, req *service.AuthListUsersRequest) (*service.AuthListUsersResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.AuthListUsersResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) CreateAuthRole(ctx context.Context, req *service.AuthCreateRoleRequest) (*service.AuthCreateRoleResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.AuthCreateRoleResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) DeleteAuthRole(ctx context.Context, req *service.AuthDeleteRoleRequest) (*service.AuthDeleteRoleResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.AuthDeleteRoleResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) ListAuthRoles(ctx context.Context, req *service.AuthListRolesRequest) (*service.AuthListRolesResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.AuthListRolesResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) AssignRole(ctx context.Context, req *service.AuthAssignRoleRequest) (*service.AuthAssignRoleResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.AuthAssignRoleResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) RevokeRole(ctx context.Context, req *service.AuthRevokeRoleRequest) (*service.AuthRevokeRoleResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.AuthRevokeRoleResponse)
	return resp, args.Error(1)
}

// =============================================================================
// Cache Service Methods (part of service.QuicBaaS)
// =============================================================================

func (m *MockQuicBaaS) GetCacheStats(ctx context.Context, req *service.GetCacheStatsRequest) (*service.GetCacheStatsResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.GetCacheStatsResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) ClearCache(ctx context.Context, req *service.ClearCacheRequest) (*service.ClearCacheResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.ClearCacheResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) GetCacheConfig(ctx context.Context, req *service.GetCacheConfigRequest) (*service.GetCacheConfigResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.GetCacheConfigResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) UpdateCacheConfig(ctx context.Context, req *service.UpdateCacheConfigRequest) (*service.UpdateCacheConfigResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.UpdateCacheConfigResponse)
	return resp, args.Error(1)
}

// =============================================================================
// Health Service Methods (part of service.QuicBaaS)
// =============================================================================

func (m *MockQuicBaaS) Health(ctx context.Context, req *service.HealthRequest) (*service.HealthResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.HealthResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) Liveness(ctx context.Context, req *service.LivenessRequest) (*service.LivenessResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.LivenessResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) Readiness(ctx context.Context, req *service.ReadinessRequest) (*service.ReadinessResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.ReadinessResponse)
	return resp, args.Error(1)
}

// =============================================================================
// Time-Series Service Methods (part of service.QuicBaaS)
// =============================================================================

func (m *MockQuicBaaS) Aggregate(ctx context.Context, req *service.AggregateRequest) (*service.AggregateResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.AggregateResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) TimeSeriesQuery(ctx context.Context, req *service.TimeSeriesQueryRequest, stream chan<- *service.TimeSeriesQueryResponse) error {
	args := m.Called(ctx, req, stream)
	return args.Error(0)
}

func (m *MockQuicBaaS) WritePoint(ctx context.Context, req *service.WritePointRequest) (*service.WritePointResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.WritePointResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) WriteBatch(ctx context.Context, req *service.WriteBatchRequest) (*service.WriteBatchResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.WriteBatchResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) TSQuery(ctx context.Context, req *service.TSQueryRequest) (*service.TSQueryResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.TSQueryResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) DeleteSeries(ctx context.Context, req *service.TSDeleteRequest) (*service.TSDeleteResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.TSDeleteResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) GetRetentionPolicy(ctx context.Context, req *service.TSGetRetentionPolicyRequest) (*service.TSGetRetentionPolicyResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.TSGetRetentionPolicyResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) SetRetentionPolicy(ctx context.Context, req *service.TSSetRetentionPolicyRequest) (*service.TSSetRetentionPolicyResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.TSSetRetentionPolicyResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) ListMetrics(ctx context.Context, req *service.TSListMetricsRequest) (*service.TSListMetricsResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.TSListMetricsResponse)
	return resp, args.Error(1)
}

// =============================================================================
// Operations Service Methods (part of service.QuicBaaS)
// =============================================================================

func (m *MockQuicBaaS) MigrateData(ctx context.Context, req *service.MigrateDataRequest) (*service.MigrateDataResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.MigrateDataResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) GetMigrationProgress(ctx context.Context, req *service.GetMigrationProgressRequest) (*service.GetMigrationProgressResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.GetMigrationProgressResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) ReplaceNode(ctx context.Context, req *service.ReplaceNodeRequest) (*service.ReplaceNodeResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.ReplaceNodeResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) UpgradeNode(ctx context.Context, req *service.UpgradeNodeRequest) (*service.UpgradeNodeResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.UpgradeNodeResponse)
	return resp, args.Error(1)
}

func (m *MockQuicBaaS) GetUpgradeStatus(ctx context.Context, req *service.GetUpgradeStatusRequest) (*service.GetUpgradeStatusResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.GetUpgradeStatusResponse)
	return resp, args.Error(1)
}

// =============================================================================
// Sub-Service Accessor Methods
// =============================================================================

// KMS returns the KMSService field if set, otherwise returns the mock itself.
// When DisableKMS is true, returns nil to force the KMSManager fallback path.
func (m *MockQuicBaaS) KMS() service.KMSService {
	if m.DisableKMS {
		return nil
	}
	if m.KMSService != nil {
		return m.KMSService
	}
	return m
}

// KMSManager delegates to testify mock infrastructure.
func (m *MockQuicBaaS) KMSManager() interfaces.KMSManager {
	args := m.Called()
	result, _ := args.Get(0).(interfaces.KMSManager)
	return result
}

// Secrets returns the mock itself as a SecretsService.
func (m *MockQuicBaaS) Secrets() service.SecretsService {
	return m
}

// Crypto returns the CryptoService field if set, otherwise returns the mock itself.
func (m *MockQuicBaaS) Crypto() service.CryptoService {
	if m.CryptoService != nil {
		return m.CryptoService
	}
	return m
}

// Vector returns the VectorService field if set, otherwise returns the mock itself.
func (m *MockQuicBaaS) Vector() service.VectorService {
	if m.VectorService != nil {
		return m.VectorService
	}
	return m
}

// Embedding returns the EmbeddingService field (may be nil).
func (m *MockQuicBaaS) Embedding() service.EmbeddingService {
	return m.EmbeddingService
}

// Ollama returns the OllamaService field (may be nil).
func (m *MockQuicBaaS) Ollama() service.OllamaService {
	return m.OllamaService
}

// Certificate returns the CertificateSvc field (may be nil).
func (m *MockQuicBaaS) Certificate() service.CertificateService {
	return m.CertificateSvc
}

// DynamicSecrets returns DynamicSecretsSvc when set, otherwise nil.
func (m *MockQuicBaaS) DynamicSecrets() service.DynamicSecretsService {
	return m.DynamicSecretsSvc
}

// ClusterDEK returns ClusterDEKSvc when set, otherwise nil.
func (m *MockQuicBaaS) ClusterDEK() service.ClusterDEKService {
	return m.ClusterDEKSvc
}

// ObjectStorageService returns the mock itself as an ObjectStorageService.
func (m *MockQuicBaaS) ObjectStorageService() service.ObjectStorageService {
	return m
}

// ObjectStorageAdminService returns the mock itself as an ObjectStorageAdminService.
func (m *MockQuicBaaS) ObjectStorageAdminService() service.ObjectStorageAdminService {
	return m
}

// ParameterStoreService returns the ParameterStoreSvc field if set, otherwise
// returns the mock itself.
func (m *MockQuicBaaS) ParameterStoreService() service.ParameterStoreService {
	if m.ParameterStoreSvc != nil {
		return m.ParameterStoreSvc
	}
	return m
}

// ObjectTransferService returns the mock itself as an ObjectTransferService.
func (m *MockQuicBaaS) ObjectTransferService() service.ObjectTransferService {
	return m
}

// Scheduler returns the mock itself as a SchedulerService.
func (m *MockQuicBaaS) Scheduler() service.SchedulerService {
	return m
}

// Agent returns nil for the mock.
func (m *MockQuicBaaS) Agent() orchestration.AgentService {
	return nil
}

// Messaging delegates to testify mock infrastructure.
func (m *MockQuicBaaS) Messaging() messaging.Service {
	args := m.Called()
	result, _ := args.Get(0).(messaging.Service)
	return result
}

// WatchManager delegates to testify mock infrastructure.
func (m *MockQuicBaaS) WatchManager() *watch.Manager {
	args := m.Called()
	result, _ := args.Get(0).(*watch.Manager)
	return result
}

// =============================================================================
// Lifecycle Methods
// =============================================================================

func (m *MockQuicBaaS) SetIAMProvider(provider service.IAMComponents) {
	m.Called(provider)
}

func (m *MockQuicBaaS) SetMessagingService(ms messaging.Service) {
	m.Called(ms)
}

// SetAgentService is a no-op for the mock.
func (m *MockQuicBaaS) SetAgentService(agent orchestration.AgentService) {
	// No-op for mock
}

// Database returns nil for the mock.
func (m *MockQuicBaaS) Database() service.DatabaseService {
	return nil
}

// SetDatabaseService is a no-op for the mock.
func (m *MockQuicBaaS) SetDatabaseService(ds service.DatabaseService) {
	// No-op for mock
}

// SetQRDBClient is a no-op for the mock.
func (m *MockQuicBaaS) SetQRDBClient(provider service.QRDBClientProvider) {
	// No-op for mock
}

// SetTenantIsolationManager is a no-op for the mock.
func (m *MockQuicBaaS) SetTenantIsolationManager(_ security.TenantIsolationManager) {
	// No-op for mock
}

func (m *MockQuicBaaS) Close() error {
	args := m.Called()
	return args.Error(0)
}

// =============================================================================
// Seal Service Methods (for type-assertion based handler access)
// =============================================================================

// SealStatus delegates to testify mock infrastructure.
func (m *MockQuicBaaS) SealStatus(ctx context.Context) (*service.SealStatusResponse, error) {
	args := m.Called(ctx)
	resp, _ := args.Get(0).(*service.SealStatusResponse)
	return resp, args.Error(1)
}

// Status delegates to testify mock infrastructure (alias for SealStatus).
func (m *MockQuicBaaS) Status(ctx context.Context) (*service.SealStatusResponse, error) {
	args := m.Called(ctx)
	resp, _ := args.Get(0).(*service.SealStatusResponse)
	return resp, args.Error(1)
}

// SealInitialize delegates to testify mock infrastructure.
func (m *MockQuicBaaS) SealInitialize(ctx context.Context, req *service.SealInitRequest) (*service.SealInitResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.SealInitResponse)
	return resp, args.Error(1)
}

// Initialize delegates to testify mock infrastructure (alias for SealInitialize).
func (m *MockQuicBaaS) Initialize(ctx context.Context, req *service.SealInitRequest) (*service.SealInitResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.SealInitResponse)
	return resp, args.Error(1)
}

// Unseal delegates to testify mock infrastructure.
func (m *MockQuicBaaS) Unseal(ctx context.Context, req *service.UnsealRequest) (*service.UnsealResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.UnsealResponse)
	return resp, args.Error(1)
}

// Seal delegates to testify mock infrastructure.
func (m *MockQuicBaaS) Seal(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

// Rekey delegates to testify mock infrastructure.
func (m *MockQuicBaaS) Rekey(ctx context.Context, req *service.RekeyRequest) (*service.RekeyResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.RekeyResponse)
	return resp, args.Error(1)
}

// GenerateRecoveryKey delegates to testify mock infrastructure.
func (m *MockQuicBaaS) GenerateRecoveryKey(ctx context.Context, req *service.GenerateRecoveryKeyRequest) (*service.GenerateRecoveryKeyResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.GenerateRecoveryKeyResponse)
	return resp, args.Error(1)
}

// DeleteRecoveryKey delegates to testify mock infrastructure.
func (m *MockQuicBaaS) DeleteRecoveryKey(ctx context.Context, req *service.DeleteRecoveryKeyRequest) (*service.DeleteRecoveryKeyResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.DeleteRecoveryKeyResponse)
	return resp, args.Error(1)
}

// GenerateRootToken delegates to testify mock infrastructure.
func (m *MockQuicBaaS) GenerateRootToken(ctx context.Context, req *service.GenerateRootTokenRequest) (*service.GenerateRootTokenResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.GenerateRootTokenResponse)
	return resp, args.Error(1)
}
