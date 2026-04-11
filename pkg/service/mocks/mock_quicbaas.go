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

// Package mocks provides mock implementations of QuicBaaS service interfaces for testing.
package mocks

import (
	"context"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/messaging"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/security"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/watch"
)

// MockQuicBaaS is a mock implementation of the service.QuicBaaS interface.
// All methods delegate to configurable function fields, enabling flexible test scenarios.
// If a function field is nil, the method returns default empty values.
type MockQuicBaaS struct {
	// KV Service function fields
	PutFunc             func(ctx context.Context, req *service.PutRequest) (*service.PutResponse, error)
	GetFunc             func(ctx context.Context, req *service.GetRequest) (*service.GetResponse, error)
	DeleteFunc          func(ctx context.Context, req *service.DeleteRequest) (*service.DeleteResponse, error)
	ListFunc            func(ctx context.Context, req *service.ListRequest, stream chan<- *service.ListResponse) error
	WatchFunc           func(ctx context.Context, req *service.WatchRequest, stream chan<- *service.WatchResponse) (int64, error)
	ListEntityTypesFunc func(ctx context.Context, req *service.ListEntityTypesRequest) (*service.ListEntityTypesResponse, error)

	// Batch Service function fields
	BatchPutFunc    func(ctx context.Context, req *service.BatchPutRequest) (*service.BatchPutResponse, error)
	BatchGetFunc    func(ctx context.Context, req *service.BatchGetRequest) (*service.BatchGetResponse, error)
	BatchDeleteFunc func(ctx context.Context, req *service.BatchDeleteRequest) (*service.BatchDeleteResponse, error)

	// Transaction Service function fields
	TransactionFunc func(ctx context.Context, req *service.TransactionRequest) (*service.TransactionResponse, error)

	// Query Service function fields
	QueryFunc     func(ctx context.Context, req *service.QueryRequest, stream chan<- *service.QueryResponse) error
	ScanIndexFunc func(ctx context.Context, req *service.ScanRequest, stream chan<- *service.ScanResponse) error

	// Cluster Service function fields
	ClusterStatusFunc        func(ctx context.Context, req *service.ClusterStatusRequest) (*service.ClusterStatusResponse, error)
	GetLeaderFunc            func(ctx context.Context, req *service.GetLeaderRequest) (*service.GetLeaderResponse, error)
	AddNodeFunc              func(ctx context.Context, req *service.AddNodeRequest) (*service.AddNodeResponse, error)
	RemoveNodeFunc           func(ctx context.Context, req *service.RemoveNodeRequest) (*service.RemoveNodeResponse, error)
	CreateShardFunc          func(ctx context.Context, req *service.CreateShardRequest) (*service.CreateShardResponse, error)
	RebalanceClusterFunc     func(ctx context.Context, req *service.RebalanceClusterRequest) (*service.RebalanceClusterResponse, error)
	GetRebalanceProgressFunc func(ctx context.Context, req *service.GetRebalanceProgressRequest) (*service.GetRebalanceProgressResponse, error)

	// Backup Service function fields
	CreateBackupFunc  func(ctx context.Context, req *service.CreateBackupRequest) (*service.CreateBackupResponse, error)
	ListBackupsFunc   func(ctx context.Context, req *service.ListBackupsRequest) (*service.ListBackupsResponse, error)
	RestoreBackupFunc func(ctx context.Context, req *service.RestoreBackupRequest) (*service.RestoreBackupResponse, error)
	DeleteBackupFunc  func(ctx context.Context, req *service.DeleteBackupRequest) (*service.DeleteBackupResponse, error)
	VerifyBackupFunc  func(ctx context.Context, req *service.VerifyBackupRequest) (*service.VerifyBackupResponse, error)

	// Auth Service function fields
	LoginFunc         func(ctx context.Context, req *service.LoginRequest) (*service.LoginResponse, error)
	LogoutFunc        func(ctx context.Context, req *service.LogoutRequest) (*service.LogoutResponse, error)
	RefreshTokenFunc  func(ctx context.Context, req *service.RefreshTokenRequest) (*service.RefreshTokenResponse, error)
	ValidateTokenFunc func(ctx context.Context, req *service.ValidateTokenRequest) (*service.ValidateTokenResponse, error)

	// RBAC Service function fields
	CreateUserFunc     func(ctx context.Context, req *service.AuthCreateUserRequest) (*service.AuthCreateUserResponse, error)
	DeleteUserFunc     func(ctx context.Context, req *service.AuthDeleteUserRequest) (*service.AuthDeleteUserResponse, error)
	ListUsersFunc      func(ctx context.Context, req *service.AuthListUsersRequest) (*service.AuthListUsersResponse, error)
	CreateAuthRoleFunc func(ctx context.Context, req *service.AuthCreateRoleRequest) (*service.AuthCreateRoleResponse, error)
	DeleteAuthRoleFunc func(ctx context.Context, req *service.AuthDeleteRoleRequest) (*service.AuthDeleteRoleResponse, error)
	ListAuthRolesFunc  func(ctx context.Context, req *service.AuthListRolesRequest) (*service.AuthListRolesResponse, error)
	AssignRoleFunc     func(ctx context.Context, req *service.AuthAssignRoleRequest) (*service.AuthAssignRoleResponse, error)
	RevokeRoleFunc     func(ctx context.Context, req *service.AuthRevokeRoleRequest) (*service.AuthRevokeRoleResponse, error)

	// Cache Service function fields
	GetCacheStatsFunc     func(ctx context.Context, req *service.GetCacheStatsRequest) (*service.GetCacheStatsResponse, error)
	ClearCacheFunc        func(ctx context.Context, req *service.ClearCacheRequest) (*service.ClearCacheResponse, error)
	GetCacheConfigFunc    func(ctx context.Context, req *service.GetCacheConfigRequest) (*service.GetCacheConfigResponse, error)
	UpdateCacheConfigFunc func(ctx context.Context, req *service.UpdateCacheConfigRequest) (*service.UpdateCacheConfigResponse, error)

	// Health Service function fields
	HealthFunc    func(ctx context.Context, req *service.HealthRequest) (*service.HealthResponse, error)
	LivenessFunc  func(ctx context.Context, req *service.LivenessRequest) (*service.LivenessResponse, error)
	ReadinessFunc func(ctx context.Context, req *service.ReadinessRequest) (*service.ReadinessResponse, error)

	// Time-Series Service function fields
	AggregateFunc          func(ctx context.Context, req *service.AggregateRequest) (*service.AggregateResponse, error)
	TimeSeriesQueryFunc    func(ctx context.Context, req *service.TimeSeriesQueryRequest, stream chan<- *service.TimeSeriesQueryResponse) error
	WritePointFunc         func(ctx context.Context, req *service.WritePointRequest) (*service.WritePointResponse, error)
	WriteBatchFunc         func(ctx context.Context, req *service.WriteBatchRequest) (*service.WriteBatchResponse, error)
	TSQueryFunc            func(ctx context.Context, req *service.TSQueryRequest) (*service.TSQueryResponse, error)
	DeleteSeriesFunc       func(ctx context.Context, req *service.TSDeleteRequest) (*service.TSDeleteResponse, error)
	GetRetentionPolicyFunc func(ctx context.Context, req *service.TSGetRetentionPolicyRequest) (*service.TSGetRetentionPolicyResponse, error)
	SetRetentionPolicyFunc func(ctx context.Context, req *service.TSSetRetentionPolicyRequest) (*service.TSSetRetentionPolicyResponse, error)
	ListMetricsFunc        func(ctx context.Context, req *service.TSListMetricsRequest) (*service.TSListMetricsResponse, error)

	// Operations Service function fields
	MigrateDataFunc          func(ctx context.Context, req *service.MigrateDataRequest) (*service.MigrateDataResponse, error)
	GetMigrationProgressFunc func(ctx context.Context, req *service.GetMigrationProgressRequest) (*service.GetMigrationProgressResponse, error)
	ReplaceNodeFunc          func(ctx context.Context, req *service.ReplaceNodeRequest) (*service.ReplaceNodeResponse, error)
	UpgradeNodeFunc          func(ctx context.Context, req *service.UpgradeNodeRequest) (*service.UpgradeNodeResponse, error)
	GetUpgradeStatusFunc     func(ctx context.Context, req *service.GetUpgradeStatusRequest) (*service.GetUpgradeStatusResponse, error)

	// Sub-service accessor function fields
	KMSServiceFunc                func() service.KMSService
	KMSManagerServiceFunc         func() interfaces.KMSManager
	SecretsServiceFunc            func() service.SecretsService
	CryptoServiceFunc             func() service.CryptoService
	VectorServiceFunc             func() service.VectorService
	EmbeddingServiceFunc          func() service.EmbeddingService
	OllamaServiceFunc             func() service.OllamaService
	CertificateServiceFunc        func() service.CertificateService
	DynamicSecretsServiceFunc     func() service.DynamicSecretsService
	ClusterDEKServiceFunc         func() service.ClusterDEKService
	ObjectStorageServiceFunc      func() service.ObjectStorageService
	ObjectStorageAdminServiceFunc func() service.ObjectStorageAdminService
	ParameterStoreServiceFunc     func() service.ParameterStoreService
	ObjectTransferServiceFunc     func() service.ObjectTransferService
	SchedulerServiceFunc          func() service.SchedulerService
	MessagingServiceFunc          func() messaging.Service
	WatchManagerFunc              func() *watch.Manager

	// Lifecycle function fields
	SetIAMProviderFunc      func(provider service.IAMComponents)
	SetMessagingServiceFunc func(ms messaging.Service)
	CloseFunc               func() error

	// Mock sub-services (can be injected for convenience)
	MockKMSService                *MockKMSService
	MockSecretsService            *MockSecretsService
	MockCryptoService             *MockCryptoService
	MockVectorService             *MockVectorService
	MockEmbeddingService          *MockEmbeddingService
	MockOllamaService             *MockOllamaService
	MockCertificateService        *MockCertificateService
	MockDynamicSecretsService     *MockDynamicSecretsService
	MockClusterDEKService         *MockClusterDEKService
	MockObjectStorageService      *MockObjectStorageService
	MockObjectStorageAdminService *MockObjectStorageAdminService
	MockParameterStoreService     *MockParameterStoreService
	MockObjectTransferService     *MockObjectTransferService
	MockSchedulerService          *MockSchedulerService
	MockMessagingService          *MockMessagingService
}

// Ensure MockQuicBaaS implements service.QuicBaaS at compile time.
var _ service.QuicBaaS = (*MockQuicBaaS)(nil)

// =============================================================================
// KV Service Methods
// =============================================================================

func (m *MockQuicBaaS) Put(ctx context.Context, req *service.PutRequest) (*service.PutResponse, error) {
	if m.PutFunc != nil {
		return m.PutFunc(ctx, req)
	}
	return &service.PutResponse{}, nil
}

func (m *MockQuicBaaS) Get(ctx context.Context, req *service.GetRequest) (*service.GetResponse, error) {
	if m.GetFunc != nil {
		return m.GetFunc(ctx, req)
	}
	return &service.GetResponse{}, nil
}

func (m *MockQuicBaaS) Delete(ctx context.Context, req *service.DeleteRequest) (*service.DeleteResponse, error) {
	if m.DeleteFunc != nil {
		return m.DeleteFunc(ctx, req)
	}
	return &service.DeleteResponse{}, nil
}

func (m *MockQuicBaaS) List(ctx context.Context, req *service.ListRequest, stream chan<- *service.ListResponse) error {
	if m.ListFunc != nil {
		return m.ListFunc(ctx, req, stream)
	}
	return nil
}

func (m *MockQuicBaaS) Watch(ctx context.Context, req *service.WatchRequest, stream chan<- *service.WatchResponse) (int64, error) {
	if m.WatchFunc != nil {
		return m.WatchFunc(ctx, req, stream)
	}
	return 0, nil
}

func (m *MockQuicBaaS) ListEntityTypes(ctx context.Context, req *service.ListEntityTypesRequest) (*service.ListEntityTypesResponse, error) {
	if m.ListEntityTypesFunc != nil {
		return m.ListEntityTypesFunc(ctx, req)
	}
	return &service.ListEntityTypesResponse{}, nil
}

// =============================================================================
// Batch Service Methods
// =============================================================================

func (m *MockQuicBaaS) BatchPut(ctx context.Context, req *service.BatchPutRequest) (*service.BatchPutResponse, error) {
	if m.BatchPutFunc != nil {
		return m.BatchPutFunc(ctx, req)
	}
	return &service.BatchPutResponse{}, nil
}

func (m *MockQuicBaaS) BatchGet(ctx context.Context, req *service.BatchGetRequest) (*service.BatchGetResponse, error) {
	if m.BatchGetFunc != nil {
		return m.BatchGetFunc(ctx, req)
	}
	return &service.BatchGetResponse{}, nil
}

func (m *MockQuicBaaS) BatchDelete(ctx context.Context, req *service.BatchDeleteRequest) (*service.BatchDeleteResponse, error) {
	if m.BatchDeleteFunc != nil {
		return m.BatchDeleteFunc(ctx, req)
	}
	return &service.BatchDeleteResponse{}, nil
}

// =============================================================================
// Transaction Service Methods
// =============================================================================

func (m *MockQuicBaaS) Transaction(ctx context.Context, req *service.TransactionRequest) (*service.TransactionResponse, error) {
	if m.TransactionFunc != nil {
		return m.TransactionFunc(ctx, req)
	}
	return &service.TransactionResponse{}, nil
}

// =============================================================================
// Query Service Methods
// =============================================================================

func (m *MockQuicBaaS) Query(ctx context.Context, req *service.QueryRequest, stream chan<- *service.QueryResponse) error {
	if m.QueryFunc != nil {
		return m.QueryFunc(ctx, req, stream)
	}
	return nil
}

func (m *MockQuicBaaS) ScanIndex(ctx context.Context, req *service.ScanRequest, stream chan<- *service.ScanResponse) error {
	if m.ScanIndexFunc != nil {
		return m.ScanIndexFunc(ctx, req, stream)
	}
	return nil
}

// =============================================================================
// Cluster Service Methods
// =============================================================================

func (m *MockQuicBaaS) ClusterStatus(ctx context.Context, req *service.ClusterStatusRequest) (*service.ClusterStatusResponse, error) {
	if m.ClusterStatusFunc != nil {
		return m.ClusterStatusFunc(ctx, req)
	}
	return &service.ClusterStatusResponse{}, nil
}

func (m *MockQuicBaaS) GetLeader(ctx context.Context, req *service.GetLeaderRequest) (*service.GetLeaderResponse, error) {
	if m.GetLeaderFunc != nil {
		return m.GetLeaderFunc(ctx, req)
	}
	return &service.GetLeaderResponse{}, nil
}

func (m *MockQuicBaaS) AddNode(ctx context.Context, req *service.AddNodeRequest) (*service.AddNodeResponse, error) {
	if m.AddNodeFunc != nil {
		return m.AddNodeFunc(ctx, req)
	}
	return &service.AddNodeResponse{}, nil
}

func (m *MockQuicBaaS) RemoveNode(ctx context.Context, req *service.RemoveNodeRequest) (*service.RemoveNodeResponse, error) {
	if m.RemoveNodeFunc != nil {
		return m.RemoveNodeFunc(ctx, req)
	}
	return &service.RemoveNodeResponse{}, nil
}

func (m *MockQuicBaaS) CreateShard(ctx context.Context, req *service.CreateShardRequest) (*service.CreateShardResponse, error) {
	if m.CreateShardFunc != nil {
		return m.CreateShardFunc(ctx, req)
	}
	return &service.CreateShardResponse{}, nil
}

func (m *MockQuicBaaS) RebalanceCluster(ctx context.Context, req *service.RebalanceClusterRequest) (*service.RebalanceClusterResponse, error) {
	if m.RebalanceClusterFunc != nil {
		return m.RebalanceClusterFunc(ctx, req)
	}
	return &service.RebalanceClusterResponse{}, nil
}

func (m *MockQuicBaaS) GetRebalanceProgress(ctx context.Context, req *service.GetRebalanceProgressRequest) (*service.GetRebalanceProgressResponse, error) {
	if m.GetRebalanceProgressFunc != nil {
		return m.GetRebalanceProgressFunc(ctx, req)
	}
	return &service.GetRebalanceProgressResponse{}, nil
}

// =============================================================================
// Backup Service Methods
// =============================================================================

func (m *MockQuicBaaS) CreateBackup(ctx context.Context, req *service.CreateBackupRequest) (*service.CreateBackupResponse, error) {
	if m.CreateBackupFunc != nil {
		return m.CreateBackupFunc(ctx, req)
	}
	return &service.CreateBackupResponse{}, nil
}

func (m *MockQuicBaaS) ListBackups(ctx context.Context, req *service.ListBackupsRequest) (*service.ListBackupsResponse, error) {
	if m.ListBackupsFunc != nil {
		return m.ListBackupsFunc(ctx, req)
	}
	return &service.ListBackupsResponse{}, nil
}

func (m *MockQuicBaaS) RestoreBackup(ctx context.Context, req *service.RestoreBackupRequest) (*service.RestoreBackupResponse, error) {
	if m.RestoreBackupFunc != nil {
		return m.RestoreBackupFunc(ctx, req)
	}
	return &service.RestoreBackupResponse{}, nil
}

func (m *MockQuicBaaS) DeleteBackup(ctx context.Context, req *service.DeleteBackupRequest) (*service.DeleteBackupResponse, error) {
	if m.DeleteBackupFunc != nil {
		return m.DeleteBackupFunc(ctx, req)
	}
	return &service.DeleteBackupResponse{}, nil
}

func (m *MockQuicBaaS) VerifyBackup(ctx context.Context, req *service.VerifyBackupRequest) (*service.VerifyBackupResponse, error) {
	if m.VerifyBackupFunc != nil {
		return m.VerifyBackupFunc(ctx, req)
	}
	return &service.VerifyBackupResponse{}, nil
}

// =============================================================================
// Auth Service Methods
// =============================================================================

func (m *MockQuicBaaS) Login(ctx context.Context, req *service.LoginRequest) (*service.LoginResponse, error) {
	if m.LoginFunc != nil {
		return m.LoginFunc(ctx, req)
	}
	return &service.LoginResponse{}, nil
}

func (m *MockQuicBaaS) Logout(ctx context.Context, req *service.LogoutRequest) (*service.LogoutResponse, error) {
	if m.LogoutFunc != nil {
		return m.LogoutFunc(ctx, req)
	}
	return &service.LogoutResponse{}, nil
}

func (m *MockQuicBaaS) RefreshToken(ctx context.Context, req *service.RefreshTokenRequest) (*service.RefreshTokenResponse, error) {
	if m.RefreshTokenFunc != nil {
		return m.RefreshTokenFunc(ctx, req)
	}
	return &service.RefreshTokenResponse{}, nil
}

func (m *MockQuicBaaS) ValidateToken(ctx context.Context, req *service.ValidateTokenRequest) (*service.ValidateTokenResponse, error) {
	if m.ValidateTokenFunc != nil {
		return m.ValidateTokenFunc(ctx, req)
	}
	return &service.ValidateTokenResponse{}, nil
}

// =============================================================================
// RBAC Service Methods
// =============================================================================

func (m *MockQuicBaaS) CreateUser(ctx context.Context, req *service.AuthCreateUserRequest) (*service.AuthCreateUserResponse, error) {
	if m.CreateUserFunc != nil {
		return m.CreateUserFunc(ctx, req)
	}
	return &service.AuthCreateUserResponse{}, nil
}

func (m *MockQuicBaaS) DeleteUser(ctx context.Context, req *service.AuthDeleteUserRequest) (*service.AuthDeleteUserResponse, error) {
	if m.DeleteUserFunc != nil {
		return m.DeleteUserFunc(ctx, req)
	}
	return &service.AuthDeleteUserResponse{}, nil
}

func (m *MockQuicBaaS) ListUsers(ctx context.Context, req *service.AuthListUsersRequest) (*service.AuthListUsersResponse, error) {
	if m.ListUsersFunc != nil {
		return m.ListUsersFunc(ctx, req)
	}
	return &service.AuthListUsersResponse{}, nil
}

func (m *MockQuicBaaS) CreateAuthRole(ctx context.Context, req *service.AuthCreateRoleRequest) (*service.AuthCreateRoleResponse, error) {
	if m.CreateAuthRoleFunc != nil {
		return m.CreateAuthRoleFunc(ctx, req)
	}
	return &service.AuthCreateRoleResponse{}, nil
}

func (m *MockQuicBaaS) DeleteAuthRole(ctx context.Context, req *service.AuthDeleteRoleRequest) (*service.AuthDeleteRoleResponse, error) {
	if m.DeleteAuthRoleFunc != nil {
		return m.DeleteAuthRoleFunc(ctx, req)
	}
	return &service.AuthDeleteRoleResponse{}, nil
}

func (m *MockQuicBaaS) ListAuthRoles(ctx context.Context, req *service.AuthListRolesRequest) (*service.AuthListRolesResponse, error) {
	if m.ListAuthRolesFunc != nil {
		return m.ListAuthRolesFunc(ctx, req)
	}
	return &service.AuthListRolesResponse{}, nil
}

func (m *MockQuicBaaS) AssignRole(ctx context.Context, req *service.AuthAssignRoleRequest) (*service.AuthAssignRoleResponse, error) {
	if m.AssignRoleFunc != nil {
		return m.AssignRoleFunc(ctx, req)
	}
	return &service.AuthAssignRoleResponse{}, nil
}

func (m *MockQuicBaaS) RevokeRole(ctx context.Context, req *service.AuthRevokeRoleRequest) (*service.AuthRevokeRoleResponse, error) {
	if m.RevokeRoleFunc != nil {
		return m.RevokeRoleFunc(ctx, req)
	}
	return &service.AuthRevokeRoleResponse{}, nil
}

// =============================================================================
// Cache Service Methods
// =============================================================================

func (m *MockQuicBaaS) GetCacheStats(ctx context.Context, req *service.GetCacheStatsRequest) (*service.GetCacheStatsResponse, error) {
	if m.GetCacheStatsFunc != nil {
		return m.GetCacheStatsFunc(ctx, req)
	}
	return &service.GetCacheStatsResponse{}, nil
}

func (m *MockQuicBaaS) ClearCache(ctx context.Context, req *service.ClearCacheRequest) (*service.ClearCacheResponse, error) {
	if m.ClearCacheFunc != nil {
		return m.ClearCacheFunc(ctx, req)
	}
	return &service.ClearCacheResponse{}, nil
}

func (m *MockQuicBaaS) GetCacheConfig(ctx context.Context, req *service.GetCacheConfigRequest) (*service.GetCacheConfigResponse, error) {
	if m.GetCacheConfigFunc != nil {
		return m.GetCacheConfigFunc(ctx, req)
	}
	return &service.GetCacheConfigResponse{}, nil
}

func (m *MockQuicBaaS) UpdateCacheConfig(ctx context.Context, req *service.UpdateCacheConfigRequest) (*service.UpdateCacheConfigResponse, error) {
	if m.UpdateCacheConfigFunc != nil {
		return m.UpdateCacheConfigFunc(ctx, req)
	}
	return &service.UpdateCacheConfigResponse{}, nil
}

// =============================================================================
// Health Service Methods
// =============================================================================

func (m *MockQuicBaaS) Health(ctx context.Context, req *service.HealthRequest) (*service.HealthResponse, error) {
	if m.HealthFunc != nil {
		return m.HealthFunc(ctx, req)
	}
	return &service.HealthResponse{Status: service.HealthStatusHealthy}, nil
}

func (m *MockQuicBaaS) Liveness(ctx context.Context, req *service.LivenessRequest) (*service.LivenessResponse, error) {
	if m.LivenessFunc != nil {
		return m.LivenessFunc(ctx, req)
	}
	return &service.LivenessResponse{Alive: true}, nil
}

func (m *MockQuicBaaS) Readiness(ctx context.Context, req *service.ReadinessRequest) (*service.ReadinessResponse, error) {
	if m.ReadinessFunc != nil {
		return m.ReadinessFunc(ctx, req)
	}
	return &service.ReadinessResponse{Ready: true}, nil
}

// =============================================================================
// Time-Series Service Methods
// =============================================================================

func (m *MockQuicBaaS) Aggregate(ctx context.Context, req *service.AggregateRequest) (*service.AggregateResponse, error) {
	if m.AggregateFunc != nil {
		return m.AggregateFunc(ctx, req)
	}
	return &service.AggregateResponse{}, nil
}

func (m *MockQuicBaaS) TimeSeriesQuery(ctx context.Context, req *service.TimeSeriesQueryRequest, stream chan<- *service.TimeSeriesQueryResponse) error {
	if m.TimeSeriesQueryFunc != nil {
		return m.TimeSeriesQueryFunc(ctx, req, stream)
	}
	return nil
}

func (m *MockQuicBaaS) WritePoint(ctx context.Context, req *service.WritePointRequest) (*service.WritePointResponse, error) {
	if m.WritePointFunc != nil {
		return m.WritePointFunc(ctx, req)
	}
	return &service.WritePointResponse{}, nil
}

func (m *MockQuicBaaS) WriteBatch(ctx context.Context, req *service.WriteBatchRequest) (*service.WriteBatchResponse, error) {
	if m.WriteBatchFunc != nil {
		return m.WriteBatchFunc(ctx, req)
	}
	return &service.WriteBatchResponse{}, nil
}

func (m *MockQuicBaaS) TSQuery(ctx context.Context, req *service.TSQueryRequest) (*service.TSQueryResponse, error) {
	if m.TSQueryFunc != nil {
		return m.TSQueryFunc(ctx, req)
	}
	return &service.TSQueryResponse{}, nil
}

func (m *MockQuicBaaS) DeleteSeries(ctx context.Context, req *service.TSDeleteRequest) (*service.TSDeleteResponse, error) {
	if m.DeleteSeriesFunc != nil {
		return m.DeleteSeriesFunc(ctx, req)
	}
	return &service.TSDeleteResponse{}, nil
}

func (m *MockQuicBaaS) GetRetentionPolicy(ctx context.Context, req *service.TSGetRetentionPolicyRequest) (*service.TSGetRetentionPolicyResponse, error) {
	if m.GetRetentionPolicyFunc != nil {
		return m.GetRetentionPolicyFunc(ctx, req)
	}
	return &service.TSGetRetentionPolicyResponse{}, nil
}

func (m *MockQuicBaaS) SetRetentionPolicy(ctx context.Context, req *service.TSSetRetentionPolicyRequest) (*service.TSSetRetentionPolicyResponse, error) {
	if m.SetRetentionPolicyFunc != nil {
		return m.SetRetentionPolicyFunc(ctx, req)
	}
	return &service.TSSetRetentionPolicyResponse{}, nil
}

func (m *MockQuicBaaS) ListMetrics(ctx context.Context, req *service.TSListMetricsRequest) (*service.TSListMetricsResponse, error) {
	if m.ListMetricsFunc != nil {
		return m.ListMetricsFunc(ctx, req)
	}
	return &service.TSListMetricsResponse{}, nil
}

// =============================================================================
// Operations Service Methods
// =============================================================================

func (m *MockQuicBaaS) MigrateData(ctx context.Context, req *service.MigrateDataRequest) (*service.MigrateDataResponse, error) {
	if m.MigrateDataFunc != nil {
		return m.MigrateDataFunc(ctx, req)
	}
	return &service.MigrateDataResponse{}, nil
}

func (m *MockQuicBaaS) GetMigrationProgress(ctx context.Context, req *service.GetMigrationProgressRequest) (*service.GetMigrationProgressResponse, error) {
	if m.GetMigrationProgressFunc != nil {
		return m.GetMigrationProgressFunc(ctx, req)
	}
	return &service.GetMigrationProgressResponse{}, nil
}

func (m *MockQuicBaaS) ReplaceNode(ctx context.Context, req *service.ReplaceNodeRequest) (*service.ReplaceNodeResponse, error) {
	if m.ReplaceNodeFunc != nil {
		return m.ReplaceNodeFunc(ctx, req)
	}
	return &service.ReplaceNodeResponse{}, nil
}

func (m *MockQuicBaaS) UpgradeNode(ctx context.Context, req *service.UpgradeNodeRequest) (*service.UpgradeNodeResponse, error) {
	if m.UpgradeNodeFunc != nil {
		return m.UpgradeNodeFunc(ctx, req)
	}
	return &service.UpgradeNodeResponse{}, nil
}

func (m *MockQuicBaaS) GetUpgradeStatus(ctx context.Context, req *service.GetUpgradeStatusRequest) (*service.GetUpgradeStatusResponse, error) {
	if m.GetUpgradeStatusFunc != nil {
		return m.GetUpgradeStatusFunc(ctx, req)
	}
	return &service.GetUpgradeStatusResponse{}, nil
}

// =============================================================================
// Sub-Service Accessor Methods
// =============================================================================

func (m *MockQuicBaaS) KMS() service.KMSService {
	if m.KMSServiceFunc != nil {
		return m.KMSServiceFunc()
	}
	if m.MockKMSService != nil {
		return m.MockKMSService
	}
	return nil
}

func (m *MockQuicBaaS) KMSManager() interfaces.KMSManager {
	if m.KMSManagerServiceFunc != nil {
		return m.KMSManagerServiceFunc()
	}
	return nil
}

func (m *MockQuicBaaS) Secrets() service.SecretsService {
	if m.SecretsServiceFunc != nil {
		return m.SecretsServiceFunc()
	}
	if m.MockSecretsService != nil {
		return m.MockSecretsService
	}
	return nil
}

func (m *MockQuicBaaS) Crypto() service.CryptoService {
	if m.CryptoServiceFunc != nil {
		return m.CryptoServiceFunc()
	}
	if m.MockCryptoService != nil {
		return m.MockCryptoService
	}
	return nil
}

func (m *MockQuicBaaS) Vector() service.VectorService {
	if m.VectorServiceFunc != nil {
		return m.VectorServiceFunc()
	}
	if m.MockVectorService != nil {
		return m.MockVectorService
	}
	return nil
}

func (m *MockQuicBaaS) Embedding() service.EmbeddingService {
	if m.EmbeddingServiceFunc != nil {
		return m.EmbeddingServiceFunc()
	}
	if m.MockEmbeddingService != nil {
		return m.MockEmbeddingService
	}
	return nil
}

func (m *MockQuicBaaS) Ollama() service.OllamaService {
	if m.OllamaServiceFunc != nil {
		return m.OllamaServiceFunc()
	}
	if m.MockOllamaService != nil {
		return m.MockOllamaService
	}
	return nil
}

func (m *MockQuicBaaS) Certificate() service.CertificateService {
	if m.CertificateServiceFunc != nil {
		return m.CertificateServiceFunc()
	}
	if m.MockCertificateService != nil {
		return m.MockCertificateService
	}
	return nil
}

func (m *MockQuicBaaS) DynamicSecrets() service.DynamicSecretsService {
	if m.DynamicSecretsServiceFunc != nil {
		return m.DynamicSecretsServiceFunc()
	}
	if m.MockDynamicSecretsService != nil {
		return m.MockDynamicSecretsService
	}
	return nil
}

func (m *MockQuicBaaS) ClusterDEK() service.ClusterDEKService {
	if m.ClusterDEKServiceFunc != nil {
		return m.ClusterDEKServiceFunc()
	}
	if m.MockClusterDEKService != nil {
		return m.MockClusterDEKService
	}
	return nil
}

func (m *MockQuicBaaS) ObjectStorageService() service.ObjectStorageService {
	if m.ObjectStorageServiceFunc != nil {
		return m.ObjectStorageServiceFunc()
	}
	if m.MockObjectStorageService != nil {
		return m.MockObjectStorageService
	}
	return nil
}

func (m *MockQuicBaaS) ObjectStorageAdminService() service.ObjectStorageAdminService {
	if m.ObjectStorageAdminServiceFunc != nil {
		return m.ObjectStorageAdminServiceFunc()
	}
	if m.MockObjectStorageAdminService != nil {
		return m.MockObjectStorageAdminService
	}
	return nil
}

func (m *MockQuicBaaS) ParameterStoreService() service.ParameterStoreService {
	if m.ParameterStoreServiceFunc != nil {
		return m.ParameterStoreServiceFunc()
	}
	if m.MockParameterStoreService != nil {
		return m.MockParameterStoreService
	}
	return nil
}

func (m *MockQuicBaaS) ObjectTransferService() service.ObjectTransferService {
	if m.ObjectTransferServiceFunc != nil {
		return m.ObjectTransferServiceFunc()
	}
	if m.MockObjectTransferService != nil {
		return m.MockObjectTransferService
	}
	return nil
}

func (m *MockQuicBaaS) Scheduler() service.SchedulerService {
	if m.SchedulerServiceFunc != nil {
		return m.SchedulerServiceFunc()
	}
	if m.MockSchedulerService != nil {
		return m.MockSchedulerService
	}
	return nil
}

func (m *MockQuicBaaS) Agent() orchestration.AgentService {
	return nil
}

func (m *MockQuicBaaS) Messaging() messaging.Service {
	if m.MessagingServiceFunc != nil {
		return m.MessagingServiceFunc()
	}
	if m.MockMessagingService != nil {
		return m.MockMessagingService
	}
	return nil
}

func (m *MockQuicBaaS) WatchManager() *watch.Manager {
	if m.WatchManagerFunc != nil {
		return m.WatchManagerFunc()
	}
	return nil
}

// =============================================================================
// Lifecycle Methods
// =============================================================================

func (m *MockQuicBaaS) SetIAMProvider(provider service.IAMComponents) {
	if m.SetIAMProviderFunc != nil {
		m.SetIAMProviderFunc(provider)
	}
}

func (m *MockQuicBaaS) SetMessagingService(ms messaging.Service) {
	if m.SetMessagingServiceFunc != nil {
		m.SetMessagingServiceFunc(ms)
	}
}

func (m *MockQuicBaaS) SetAgentService(agent orchestration.AgentService) {
	// No-op in mock - agent service is not used in unit tests
}

func (m *MockQuicBaaS) Database() service.DatabaseService {
	return nil
}

func (m *MockQuicBaaS) SetDatabaseService(ds service.DatabaseService) {
	// No-op in mock
}

func (m *MockQuicBaaS) SetQRDBClient(provider service.QRDBClientProvider) {
	// No-op in mock
}

// SetTenantIsolationManager is a no-op in the mock.
func (m *MockQuicBaaS) SetTenantIsolationManager(_ security.TenantIsolationManager) {}

func (m *MockQuicBaaS) Close() error {
	if m.CloseFunc != nil {
		return m.CloseFunc()
	}
	return nil
}

// =============================================================================
// Test Helper Methods
// =============================================================================

// NewMockQuicBaaS creates a new MockQuicBaaS with default empty implementations.
func NewMockQuicBaaS() *MockQuicBaaS {
	return &MockQuicBaaS{}
}

// NewMockQuicBaaSWithAllSubservices creates a MockQuicBaaS with all mock sub-services initialized.
func NewMockQuicBaaSWithAllSubservices() *MockQuicBaaS {
	return &MockQuicBaaS{
		MockKMSService:                &MockKMSService{},
		MockSecretsService:            &MockSecretsService{},
		MockCryptoService:             &MockCryptoService{},
		MockVectorService:             &MockVectorService{},
		MockEmbeddingService:          &MockEmbeddingService{},
		MockOllamaService:             &MockOllamaService{},
		MockCertificateService:        &MockCertificateService{},
		MockDynamicSecretsService:     &MockDynamicSecretsService{},
		MockClusterDEKService:         &MockClusterDEKService{},
		MockObjectStorageService:      &MockObjectStorageService{},
		MockObjectStorageAdminService: &MockObjectStorageAdminService{},
		MockParameterStoreService:     &MockParameterStoreService{},
		MockObjectTransferService:     &MockObjectTransferService{},
		MockSchedulerService:          &MockSchedulerService{},
		MockMessagingService:          &MockMessagingService{},
	}
}

// WithPutResponse configures the mock to return a specific response for Put operations.
func (m *MockQuicBaaS) WithPutResponse(resp *service.PutResponse, err error) *MockQuicBaaS {
	m.PutFunc = func(ctx context.Context, req *service.PutRequest) (*service.PutResponse, error) {
		return resp, err
	}
	return m
}

// WithGetResponse configures the mock to return a specific response for Get operations.
func (m *MockQuicBaaS) WithGetResponse(resp *service.GetResponse, err error) *MockQuicBaaS {
	m.GetFunc = func(ctx context.Context, req *service.GetRequest) (*service.GetResponse, error) {
		return resp, err
	}
	return m
}

// WithDeleteResponse configures the mock to return a specific response for Delete operations.
func (m *MockQuicBaaS) WithDeleteResponse(resp *service.DeleteResponse, err error) *MockQuicBaaS {
	m.DeleteFunc = func(ctx context.Context, req *service.DeleteRequest) (*service.DeleteResponse, error) {
		return resp, err
	}
	return m
}

// WithHealthyStatus configures the mock to return healthy status for all health checks.
func (m *MockQuicBaaS) WithHealthyStatus() *MockQuicBaaS {
	m.HealthFunc = func(ctx context.Context, req *service.HealthRequest) (*service.HealthResponse, error) {
		return &service.HealthResponse{Status: service.HealthStatusHealthy}, nil
	}
	m.LivenessFunc = func(ctx context.Context, req *service.LivenessRequest) (*service.LivenessResponse, error) {
		return &service.LivenessResponse{Alive: true}, nil
	}
	m.ReadinessFunc = func(ctx context.Context, req *service.ReadinessRequest) (*service.ReadinessResponse, error) {
		return &service.ReadinessResponse{Ready: true}, nil
	}
	return m
}

// WithUnhealthyStatus configures the mock to return unhealthy status for all health checks.
func (m *MockQuicBaaS) WithUnhealthyStatus(reason string) *MockQuicBaaS {
	m.HealthFunc = func(ctx context.Context, req *service.HealthRequest) (*service.HealthResponse, error) {
		return &service.HealthResponse{Status: service.HealthStatusUnhealthy}, nil
	}
	m.LivenessFunc = func(ctx context.Context, req *service.LivenessRequest) (*service.LivenessResponse, error) {
		return &service.LivenessResponse{Alive: false}, nil
	}
	m.ReadinessFunc = func(ctx context.Context, req *service.ReadinessRequest) (*service.ReadinessResponse, error) {
		return &service.ReadinessResponse{Ready: false, Reason: reason}, nil
	}
	return m
}
