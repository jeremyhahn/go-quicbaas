//go:build quic

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

package quic

import (
	"context"
	"errors"
	"testing"

	qrdbsecret "github.com/jeremyhahn/go-qrdb/sdk/go/secret"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/messaging"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"

	xkmsssh "github.com/jeremyhahn/go-xkms/sdk/go/client/ssh"

	schedulerpkg "github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
	"github.com/jeremyhahn/go-quicbaas/pkg/watch"
)

// =============================================================================
// Mock Service Implementation
// =============================================================================

type mockService struct {
	loginSvc       service.LoginServicer
	attestationSvc service.AttestationService
	pivSvc         service.PIVService
	fido2Svc       service.FIDO2Service
	// KV Operations
	putFn    func(ctx context.Context, req *service.PutRequest) (*service.PutResponse, error)
	getFn    func(ctx context.Context, req *service.GetRequest) (*service.GetResponse, error)
	deleteFn func(ctx context.Context, req *service.DeleteRequest) (*service.DeleteResponse, error)
	listFn   func(ctx context.Context, req *service.ListRequest, stream chan<- *service.ListResponse) error
	watchFn  func(ctx context.Context, req *service.WatchRequest, stream chan<- *service.WatchResponse) (int64, error)

	// Batch Operations
	batchPutFn    func(ctx context.Context, req *service.BatchPutRequest) (*service.BatchPutResponse, error)
	batchGetFn    func(ctx context.Context, req *service.BatchGetRequest) (*service.BatchGetResponse, error)
	batchDeleteFn func(ctx context.Context, req *service.BatchDeleteRequest) (*service.BatchDeleteResponse, error)

	// Transaction Operations
	transactionFn func(ctx context.Context, req *service.TransactionRequest) (*service.TransactionResponse, error)

	// Query Operations
	queryFn     func(ctx context.Context, req *service.QueryRequest, stream chan<- *service.QueryResponse) error
	scanIndexFn func(ctx context.Context, req *service.ScanRequest, stream chan<- *service.ScanResponse) error

	// Cluster Operations
	clusterStatusFn        func(ctx context.Context, req *service.ClusterStatusRequest) (*service.ClusterStatusResponse, error)
	getLeaderFn            func(ctx context.Context, req *service.GetLeaderRequest) (*service.GetLeaderResponse, error)
	addNodeFn              func(ctx context.Context, req *service.AddNodeRequest) (*service.AddNodeResponse, error)
	removeNodeFn           func(ctx context.Context, req *service.RemoveNodeRequest) (*service.RemoveNodeResponse, error)
	createShardFn          func(ctx context.Context, req *service.CreateShardRequest) (*service.CreateShardResponse, error)
	rebalanceClusterFn     func(ctx context.Context, req *service.RebalanceClusterRequest) (*service.RebalanceClusterResponse, error)
	getRebalanceProgressFn func(ctx context.Context, req *service.GetRebalanceProgressRequest) (*service.GetRebalanceProgressResponse, error)

	// Backup Operations
	createBackupFn  func(ctx context.Context, req *service.CreateBackupRequest) (*service.CreateBackupResponse, error)
	listBackupsFn   func(ctx context.Context, req *service.ListBackupsRequest) (*service.ListBackupsResponse, error)
	restoreBackupFn func(ctx context.Context, req *service.RestoreBackupRequest) (*service.RestoreBackupResponse, error)
	deleteBackupFn  func(ctx context.Context, req *service.DeleteBackupRequest) (*service.DeleteBackupResponse, error)
	verifyBackupFn  func(ctx context.Context, req *service.VerifyBackupRequest) (*service.VerifyBackupResponse, error)

	// Auth Operations
	loginFn         func(ctx context.Context, req *service.LoginRequest) (*service.LoginResponse, error)
	logoutFn        func(ctx context.Context, req *service.LogoutRequest) (*service.LogoutResponse, error)
	refreshTokenFn  func(ctx context.Context, req *service.RefreshTokenRequest) (*service.RefreshTokenResponse, error)
	validateTokenFn func(ctx context.Context, req *service.ValidateTokenRequest) (*service.ValidateTokenResponse, error)

	// RBAC Operations
	createUserFn     func(ctx context.Context, req *service.AuthCreateUserRequest) (*service.AuthCreateUserResponse, error)
	deleteUserFn     func(ctx context.Context, req *service.AuthDeleteUserRequest) (*service.AuthDeleteUserResponse, error)
	listUsersFn      func(ctx context.Context, req *service.AuthListUsersRequest) (*service.AuthListUsersResponse, error)
	createAuthRoleFn func(ctx context.Context, req *service.AuthCreateRoleRequest) (*service.AuthCreateRoleResponse, error)
	deleteAuthRoleFn func(ctx context.Context, req *service.AuthDeleteRoleRequest) (*service.AuthDeleteRoleResponse, error)
	listAuthRolesFn  func(ctx context.Context, req *service.AuthListRolesRequest) (*service.AuthListRolesResponse, error)
	assignRoleFn     func(ctx context.Context, req *service.AssignRoleRequest) (*service.AssignRoleResponse, error)
	revokeRoleFn     func(ctx context.Context, req *service.RevokeRoleRequest) (*service.RevokeRoleResponse, error)

	// Cache Operations
	getCacheStatsFn     func(ctx context.Context, req *service.GetCacheStatsRequest) (*service.GetCacheStatsResponse, error)
	clearCacheFn        func(ctx context.Context, req *service.ClearCacheRequest) (*service.ClearCacheResponse, error)
	getCacheConfigFn    func(ctx context.Context, req *service.GetCacheConfigRequest) (*service.GetCacheConfigResponse, error)
	updateCacheConfigFn func(ctx context.Context, req *service.UpdateCacheConfigRequest) (*service.UpdateCacheConfigResponse, error)

	// Health Operations
	healthFn    func(ctx context.Context, req *service.HealthRequest) (*service.HealthResponse, error)
	livenessFn  func(ctx context.Context, req *service.LivenessRequest) (*service.LivenessResponse, error)
	readinessFn func(ctx context.Context, req *service.ReadinessRequest) (*service.ReadinessResponse, error)

	// Time-Series Operations
	aggregateFn       func(ctx context.Context, req *service.AggregateRequest) (*service.AggregateResponse, error)
	timeSeriesQueryFn func(ctx context.Context, req *service.TimeSeriesQueryRequest, stream chan<- *service.TimeSeriesQueryResponse) error

	// Operations Management
	migrateDataFn          func(ctx context.Context, req *service.MigrateDataRequest) (*service.MigrateDataResponse, error)
	getMigrationProgressFn func(ctx context.Context, req *service.GetMigrationProgressRequest) (*service.GetMigrationProgressResponse, error)
	replaceNodeFn          func(ctx context.Context, req *service.ReplaceNodeRequest) (*service.ReplaceNodeResponse, error)
	upgradeNodeFn          func(ctx context.Context, req *service.UpgradeNodeRequest) (*service.UpgradeNodeResponse, error)
	getUpgradeStatusFn     func(ctx context.Context, req *service.GetUpgradeStatusRequest) (*service.GetUpgradeStatusResponse, error)

	// ListEntityTypes
	listEntityTypesFn func(ctx context.Context, req *service.ListEntityTypesRequest) (*service.ListEntityTypesResponse, error)
}

// Compile-time interface check.
var _ service.QuicBaaS = (*mockService)(nil)

// =============================================================================
// KV Operations
// =============================================================================

func (m *mockService) Put(ctx context.Context, req *service.PutRequest) (*service.PutResponse, error) {
	if m.putFn != nil {
		return m.putFn(ctx, req)
	}
	return nil, errors.New("not implemented")
}

func (m *mockService) Get(ctx context.Context, req *service.GetRequest) (*service.GetResponse, error) {
	if m.getFn != nil {
		return m.getFn(ctx, req)
	}
	return nil, errors.New("not implemented")
}

func (m *mockService) Delete(ctx context.Context, req *service.DeleteRequest) (*service.DeleteResponse, error) {
	if m.deleteFn != nil {
		return m.deleteFn(ctx, req)
	}
	return nil, errors.New("not implemented")
}

func (m *mockService) List(ctx context.Context, req *service.ListRequest, stream chan<- *service.ListResponse) error {
	if m.listFn != nil {
		return m.listFn(ctx, req, stream)
	}
	return errors.New("not implemented")
}

func (m *mockService) Watch(ctx context.Context, req *service.WatchRequest, stream chan<- *service.WatchResponse) (int64, error) {
	if m.watchFn != nil {
		return m.watchFn(ctx, req, stream)
	}
	return 0, errors.New("not implemented")
}

// =============================================================================
// Batch Operations
// =============================================================================

func (m *mockService) BatchPut(ctx context.Context, req *service.BatchPutRequest) (*service.BatchPutResponse, error) {
	if m.batchPutFn != nil {
		return m.batchPutFn(ctx, req)
	}
	return nil, errors.New("not implemented")
}

func (m *mockService) BatchGet(ctx context.Context, req *service.BatchGetRequest) (*service.BatchGetResponse, error) {
	if m.batchGetFn != nil {
		return m.batchGetFn(ctx, req)
	}
	return nil, errors.New("not implemented")
}

func (m *mockService) BatchDelete(ctx context.Context, req *service.BatchDeleteRequest) (*service.BatchDeleteResponse, error) {
	if m.batchDeleteFn != nil {
		return m.batchDeleteFn(ctx, req)
	}
	return nil, errors.New("not implemented")
}

// =============================================================================
// Transaction Operations
// =============================================================================

func (m *mockService) Transaction(ctx context.Context, req *service.TransactionRequest) (*service.TransactionResponse, error) {
	if m.transactionFn != nil {
		return m.transactionFn(ctx, req)
	}
	return nil, errors.New("not implemented")
}

// =============================================================================
// Query Operations
// =============================================================================

func (m *mockService) Query(ctx context.Context, req *service.QueryRequest, stream chan<- *service.QueryResponse) error {
	if m.queryFn != nil {
		return m.queryFn(ctx, req, stream)
	}
	return errors.New("not implemented")
}

func (m *mockService) ScanIndex(ctx context.Context, req *service.ScanRequest, stream chan<- *service.ScanResponse) error {
	if m.scanIndexFn != nil {
		return m.scanIndexFn(ctx, req, stream)
	}
	return errors.New("not implemented")
}

// =============================================================================
// Cluster Operations
// =============================================================================

func (m *mockService) ClusterStatus(ctx context.Context, req *service.ClusterStatusRequest) (*service.ClusterStatusResponse, error) {
	if m.clusterStatusFn != nil {
		return m.clusterStatusFn(ctx, req)
	}
	return nil, errors.New("not implemented")
}

func (m *mockService) GetLeader(ctx context.Context, req *service.GetLeaderRequest) (*service.GetLeaderResponse, error) {
	if m.getLeaderFn != nil {
		return m.getLeaderFn(ctx, req)
	}
	return nil, errors.New("not implemented")
}

func (m *mockService) AddNode(ctx context.Context, req *service.AddNodeRequest) (*service.AddNodeResponse, error) {
	if m.addNodeFn != nil {
		return m.addNodeFn(ctx, req)
	}
	return nil, errors.New("not implemented")
}

func (m *mockService) RemoveNode(ctx context.Context, req *service.RemoveNodeRequest) (*service.RemoveNodeResponse, error) {
	if m.removeNodeFn != nil {
		return m.removeNodeFn(ctx, req)
	}
	return nil, errors.New("not implemented")
}

func (m *mockService) CreateShard(ctx context.Context, req *service.CreateShardRequest) (*service.CreateShardResponse, error) {
	if m.createShardFn != nil {
		return m.createShardFn(ctx, req)
	}
	return nil, errors.New("not implemented")
}

func (m *mockService) RebalanceCluster(ctx context.Context, req *service.RebalanceClusterRequest) (*service.RebalanceClusterResponse, error) {
	if m.rebalanceClusterFn != nil {
		return m.rebalanceClusterFn(ctx, req)
	}
	return nil, errors.New("not implemented")
}

func (m *mockService) GetRebalanceProgress(ctx context.Context, req *service.GetRebalanceProgressRequest) (*service.GetRebalanceProgressResponse, error) {
	if m.getRebalanceProgressFn != nil {
		return m.getRebalanceProgressFn(ctx, req)
	}
	return nil, errors.New("not implemented")
}

// =============================================================================
// Backup Operations
// =============================================================================

func (m *mockService) CreateBackup(ctx context.Context, req *service.CreateBackupRequest) (*service.CreateBackupResponse, error) {
	if m.createBackupFn != nil {
		return m.createBackupFn(ctx, req)
	}
	return nil, errors.New("not implemented")
}

func (m *mockService) ListBackups(ctx context.Context, req *service.ListBackupsRequest) (*service.ListBackupsResponse, error) {
	if m.listBackupsFn != nil {
		return m.listBackupsFn(ctx, req)
	}
	return nil, errors.New("not implemented")
}

func (m *mockService) RestoreBackup(ctx context.Context, req *service.RestoreBackupRequest) (*service.RestoreBackupResponse, error) {
	if m.restoreBackupFn != nil {
		return m.restoreBackupFn(ctx, req)
	}
	return nil, errors.New("not implemented")
}

func (m *mockService) DeleteBackup(ctx context.Context, req *service.DeleteBackupRequest) (*service.DeleteBackupResponse, error) {
	if m.deleteBackupFn != nil {
		return m.deleteBackupFn(ctx, req)
	}
	return nil, errors.New("not implemented")
}

func (m *mockService) VerifyBackup(ctx context.Context, req *service.VerifyBackupRequest) (*service.VerifyBackupResponse, error) {
	if m.verifyBackupFn != nil {
		return m.verifyBackupFn(ctx, req)
	}
	return nil, errors.New("not implemented")
}

// =============================================================================
// Auth Operations
// =============================================================================

func (m *mockService) Login(ctx context.Context, req *service.LoginRequest) (*service.LoginResponse, error) {
	if m.loginFn != nil {
		return m.loginFn(ctx, req)
	}
	return nil, errors.New("not implemented")
}

func (m *mockService) Logout(ctx context.Context, req *service.LogoutRequest) (*service.LogoutResponse, error) {
	if m.logoutFn != nil {
		return m.logoutFn(ctx, req)
	}
	return nil, errors.New("not implemented")
}

func (m *mockService) RefreshToken(ctx context.Context, req *service.RefreshTokenRequest) (*service.RefreshTokenResponse, error) {
	if m.refreshTokenFn != nil {
		return m.refreshTokenFn(ctx, req)
	}
	return nil, errors.New("not implemented")
}

func (m *mockService) ValidateToken(ctx context.Context, req *service.ValidateTokenRequest) (*service.ValidateTokenResponse, error) {
	if m.validateTokenFn != nil {
		return m.validateTokenFn(ctx, req)
	}
	return nil, errors.New("not implemented")
}

// =============================================================================
// RBAC Operations
// =============================================================================

func (m *mockService) CreateUser(ctx context.Context, req *service.AuthCreateUserRequest) (*service.AuthCreateUserResponse, error) {
	if m.createUserFn != nil {
		return m.createUserFn(ctx, req)
	}
	return nil, errors.New("not implemented")
}

func (m *mockService) DeleteUser(ctx context.Context, req *service.AuthDeleteUserRequest) (*service.AuthDeleteUserResponse, error) {
	if m.deleteUserFn != nil {
		return m.deleteUserFn(ctx, req)
	}
	return nil, errors.New("not implemented")
}

func (m *mockService) ListUsers(ctx context.Context, req *service.AuthListUsersRequest) (*service.AuthListUsersResponse, error) {
	if m.listUsersFn != nil {
		return m.listUsersFn(ctx, req)
	}
	return nil, errors.New("not implemented")
}

func (m *mockService) CreateAuthRole(ctx context.Context, req *service.AuthCreateRoleRequest) (*service.AuthCreateRoleResponse, error) {
	if m.createAuthRoleFn != nil {
		return m.createAuthRoleFn(ctx, req)
	}
	return nil, errors.New("not implemented")
}

func (m *mockService) DeleteAuthRole(ctx context.Context, req *service.AuthDeleteRoleRequest) (*service.AuthDeleteRoleResponse, error) {
	if m.deleteAuthRoleFn != nil {
		return m.deleteAuthRoleFn(ctx, req)
	}
	return nil, errors.New("not implemented")
}

func (m *mockService) ListAuthRoles(ctx context.Context, req *service.AuthListRolesRequest) (*service.AuthListRolesResponse, error) {
	if m.listAuthRolesFn != nil {
		return m.listAuthRolesFn(ctx, req)
	}
	return nil, errors.New("not implemented")
}

func (m *mockService) AssignRole(ctx context.Context, req *service.AssignRoleRequest) (*service.AssignRoleResponse, error) {
	if m.assignRoleFn != nil {
		return m.assignRoleFn(ctx, req)
	}
	return nil, errors.New("not implemented")
}

func (m *mockService) RevokeRole(ctx context.Context, req *service.RevokeRoleRequest) (*service.RevokeRoleResponse, error) {
	if m.revokeRoleFn != nil {
		return m.revokeRoleFn(ctx, req)
	}
	return nil, errors.New("not implemented")
}

// =============================================================================
// Cache Operations
// =============================================================================

func (m *mockService) GetCacheStats(ctx context.Context, req *service.GetCacheStatsRequest) (*service.GetCacheStatsResponse, error) {
	if m.getCacheStatsFn != nil {
		return m.getCacheStatsFn(ctx, req)
	}
	return nil, errors.New("not implemented")
}

func (m *mockService) ClearCache(ctx context.Context, req *service.ClearCacheRequest) (*service.ClearCacheResponse, error) {
	if m.clearCacheFn != nil {
		return m.clearCacheFn(ctx, req)
	}
	return nil, errors.New("not implemented")
}

func (m *mockService) GetCacheConfig(ctx context.Context, req *service.GetCacheConfigRequest) (*service.GetCacheConfigResponse, error) {
	if m.getCacheConfigFn != nil {
		return m.getCacheConfigFn(ctx, req)
	}
	return nil, errors.New("not implemented")
}

func (m *mockService) UpdateCacheConfig(ctx context.Context, req *service.UpdateCacheConfigRequest) (*service.UpdateCacheConfigResponse, error) {
	if m.updateCacheConfigFn != nil {
		return m.updateCacheConfigFn(ctx, req)
	}
	return nil, errors.New("not implemented")
}

// =============================================================================
// Health Operations
// =============================================================================

func (m *mockService) Health(ctx context.Context, req *service.HealthRequest) (*service.HealthResponse, error) {
	if m.healthFn != nil {
		return m.healthFn(ctx, req)
	}
	return nil, errors.New("not implemented")
}

func (m *mockService) Liveness(ctx context.Context, req *service.LivenessRequest) (*service.LivenessResponse, error) {
	if m.livenessFn != nil {
		return m.livenessFn(ctx, req)
	}
	return nil, errors.New("not implemented")
}

func (m *mockService) Readiness(ctx context.Context, req *service.ReadinessRequest) (*service.ReadinessResponse, error) {
	if m.readinessFn != nil {
		return m.readinessFn(ctx, req)
	}
	return nil, errors.New("not implemented")
}

// =============================================================================
// Time-Series Operations
// =============================================================================

func (m *mockService) Aggregate(ctx context.Context, req *service.AggregateRequest) (*service.AggregateResponse, error) {
	if m.aggregateFn != nil {
		return m.aggregateFn(ctx, req)
	}
	return nil, errors.New("not implemented")
}

func (m *mockService) TimeSeriesQuery(ctx context.Context, req *service.TimeSeriesQueryRequest, stream chan<- *service.TimeSeriesQueryResponse) error {
	if m.timeSeriesQueryFn != nil {
		return m.timeSeriesQueryFn(ctx, req, stream)
	}
	return errors.New("not implemented")
}

func (m *mockService) WritePoint(ctx context.Context, req *service.WritePointRequest) (*service.WritePointResponse, error) {
	return nil, errors.New("not implemented")
}

func (m *mockService) WriteBatch(ctx context.Context, req *service.WriteBatchRequest) (*service.WriteBatchResponse, error) {
	return nil, errors.New("not implemented")
}

func (m *mockService) TSQuery(ctx context.Context, req *service.TSQueryRequest) (*service.TSQueryResponse, error) {
	return nil, errors.New("not implemented")
}

func (m *mockService) DeleteSeries(ctx context.Context, req *service.TSDeleteRequest) (*service.TSDeleteResponse, error) {
	return nil, errors.New("not implemented")
}

func (m *mockService) GetRetentionPolicy(ctx context.Context, req *service.TSGetRetentionPolicyRequest) (*service.TSGetRetentionPolicyResponse, error) {
	return nil, errors.New("not implemented")
}

func (m *mockService) SetRetentionPolicy(ctx context.Context, req *service.TSSetRetentionPolicyRequest) (*service.TSSetRetentionPolicyResponse, error) {
	return nil, errors.New("not implemented")
}

func (m *mockService) ListMetrics(ctx context.Context, req *service.TSListMetricsRequest) (*service.TSListMetricsResponse, error) {
	return nil, errors.New("not implemented")
}

// =============================================================================
// Operations Management
// =============================================================================

func (m *mockService) MigrateData(ctx context.Context, req *service.MigrateDataRequest) (*service.MigrateDataResponse, error) {
	if m.migrateDataFn != nil {
		return m.migrateDataFn(ctx, req)
	}
	return nil, errors.New("not implemented")
}

func (m *mockService) GetMigrationProgress(ctx context.Context, req *service.GetMigrationProgressRequest) (*service.GetMigrationProgressResponse, error) {
	if m.getMigrationProgressFn != nil {
		return m.getMigrationProgressFn(ctx, req)
	}
	return nil, errors.New("not implemented")
}

func (m *mockService) ReplaceNode(ctx context.Context, req *service.ReplaceNodeRequest) (*service.ReplaceNodeResponse, error) {
	if m.replaceNodeFn != nil {
		return m.replaceNodeFn(ctx, req)
	}
	return nil, errors.New("not implemented")
}

func (m *mockService) UpgradeNode(ctx context.Context, req *service.UpgradeNodeRequest) (*service.UpgradeNodeResponse, error) {
	if m.upgradeNodeFn != nil {
		return m.upgradeNodeFn(ctx, req)
	}
	return nil, errors.New("not implemented")
}

func (m *mockService) GetUpgradeStatus(ctx context.Context, req *service.GetUpgradeStatusRequest) (*service.GetUpgradeStatusResponse, error) {
	if m.getUpgradeStatusFn != nil {
		return m.getUpgradeStatusFn(ctx, req)
	}
	return nil, errors.New("not implemented")
}

// =============================================================================
// Service Accessors
// =============================================================================

func (m *mockService) KMS() service.KMSService {
	return nil
}

func (m *mockService) KMSManager() interfaces.KMSManager {
	return nil
}

func (m *mockService) Secrets() service.SecretsService {
	return nil
}

func (m *mockService) Crypto() service.CryptoService {
	return nil
}

func (m *mockService) Vector() service.VectorService {
	return nil
}

func (m *mockService) Embedding() service.EmbeddingService {
	return nil
}

func (m *mockService) Ollama() service.OllamaService {
	return nil
}

func (m *mockService) Certificate() service.CertificateService {
	return nil
}

func (m *mockService) Trust() service.TrustService {
	return nil
}

func (m *mockService) Attestation() service.AttestationService {
	return m.attestationSvc
}

func (m *mockService) PIV() service.PIVService {
	return m.pivSvc
}

func (m *mockService) FIDO2() service.FIDO2Service {
	return m.fido2Svc
}

func (m *mockService) ACME() service.ACMEServicer {
	return nil
}

func (m *mockService) DynamicSecrets() service.DynamicSecretsService {
	return nil
}

func (m *mockService) ObjectStorageService() service.ObjectStorageService {
	return nil
}

func (m *mockService) ObjectStorageAdminService() service.ObjectStorageAdminService {
	return nil
}

func (m *mockService) ParameterStoreService() service.ParameterStoreService {
	return nil
}

func (m *mockService) ObjectTransferService() service.ObjectTransferService {
	return nil
}

func (m *mockService) Scheduler() service.SchedulerService {
	return nil
}

// SchedulerEngine is a no-op for the quic mock.
func (m *mockService) SchedulerEngine() *schedulerpkg.SchedulerEngine { return nil }

// SSHClient is a no-op for the quic mock; tests that exercise SSH
// routing wire a real xkmsssh.Client directly.
func (m *mockService) SSHClient() xkmsssh.Client { return nil }

func (m *mockService) Agent() orchestration.AgentService {
	return nil
}

func (m *mockService) Project() service.ProjectService {
	return nil
}

func (m *mockService) Account() service.AccountService {
	return nil
}

func (m *mockService) SecretRegistry() *qrdbsecret.ResolverRegistry { return nil }

func (m *mockService) SetSecretRegistry(reg *qrdbsecret.ResolverRegistry) {}

func (m *mockService) AgentAdmin() service.AgentAdminService {
	return nil
}

func (m *mockService) Messaging() messaging.Service {
	return nil
}

func (m *mockService) WatchManager() *watch.Manager {
	return nil
}

func (m *mockService) SetIAMProvider(_ service.IAMComponents) {
}

func (m *mockService) SetMessagingService(ms messaging.Service) {
}

func (m *mockService) SetAgentService(agent orchestration.AgentService) {
}

func (m *mockService) Database() service.DatabaseService {
	return nil
}

func (m *mockService) SetDatabaseService(ds service.DatabaseService) {
}

func (m *mockService) SetQRDBClient(provider service.QRDBClientProvider) {
}

func (m *mockService) LoginCeremony() service.LoginServicer { return m.loginSvc }

func (m *mockService) SetLoginService(svc service.LoginServicer) { m.loginSvc = svc }

func (m *mockService) Close() error {
	return nil
}

func (m *mockService) ListEntityTypes(ctx context.Context, req *service.ListEntityTypesRequest) (*service.ListEntityTypesResponse, error) {
	if m.listEntityTypesFn != nil {
		return m.listEntityTypesFn(ctx, req)
	}
	return nil, errors.New("not implemented")
}

// SetSealService is a no-op for the mock.
func (m *mockService) SetSealService(_ service.SealService) {}

// SealService default stubs to satisfy the QuicBaaS interface.
// Tests that exercise seal behavior compose e2eMockService or supply
// a dedicated SealService implementation.
func (m *mockService) Status(_ context.Context) (*service.SealStatusResponse, error) {
	return &service.SealStatusResponse{}, nil
}

func (m *mockService) Initialize(_ context.Context, _ *service.SealInitRequest) (*service.SealInitResponse, error) {
	return &service.SealInitResponse{}, nil
}

func (m *mockService) Unseal(_ context.Context, _ *service.UnsealRequest) (*service.UnsealResponse, error) {
	return &service.UnsealResponse{}, nil
}

func (m *mockService) Seal(_ context.Context) error { return nil }

func (m *mockService) Rekey(_ context.Context, _ *service.RekeyRequest) (*service.RekeyResponse, error) {
	return &service.RekeyResponse{}, nil
}

func (m *mockService) GenerateRecoveryKey(_ context.Context, _ *service.GenerateRecoveryKeyRequest) (*service.GenerateRecoveryKeyResponse, error) {
	return &service.GenerateRecoveryKeyResponse{}, nil
}

func (m *mockService) DeleteRecoveryKey(_ context.Context, _ *service.DeleteRecoveryKeyRequest) (*service.DeleteRecoveryKeyResponse, error) {
	return &service.DeleteRecoveryKeyResponse{Deleted: true}, nil
}

func (m *mockService) GenerateRootToken(_ context.Context, _ *service.GenerateRootTokenRequest) (*service.GenerateRootTokenResponse, error) {
	return &service.GenerateRootTokenResponse{}, nil
}

func (m *mockService) RotateRootKey(_ context.Context, _ *service.RotateRootKeyRequest) (*service.RotateRootKeyResponse, error) {
	return &service.RotateRootKeyResponse{Success: true}, nil
}

// =============================================================================
// Helper Functions
// =============================================================================

func createTestLogger() *logger.Logger {
	cfg := logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "quic-test",
	}
	log, _ := logger.New(cfg)
	return log
}

func createTestHandler() *Handler {
	return NewHandler(&mockService{}, nil, nil, nil, createTestLogger())
}

// =============================================================================
// Basic Handler Tests
// =============================================================================

func TestNewHandler(t *testing.T) {
	handler := createTestHandler()
	if handler == nil {
		t.Fatal("Expected non-nil handler")
	}
}

func TestHandler_HandleRequest_UnknownOpcode(t *testing.T) {
	handler := createTestHandler()
	req := &Request{
		Opcode:  0xFF,
		Payload: nil,
	}
	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("Expected error status for unknown opcode")
	}
}

func TestHandler_HandleRequest_Put_Success(t *testing.T) {
	svc := &mockService{
		putFn: func(ctx context.Context, req *service.PutRequest) (*service.PutResponse, error) {
			return &service.PutResponse{
				ID:      req.ID,
				Version: 1,
			}, nil
		},
	}
	handler := NewHandler(svc, nil, nil, nil, createTestLogger())

	payload, err := EncodePayload(&service.PutRequest{
		EntityType: "user",
		ID:         1,
		Data:       []byte(`{"name":"test"}`),
	})
	if err != nil {
		t.Fatalf("Failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpPut,
		Payload: payload,
	}
	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("Expected status OK, got: 0x%02X", resp.Status)
	}
}

func TestHandler_HandleRequest_Put_Error(t *testing.T) {
	svc := &mockService{
		putFn: func(ctx context.Context, req *service.PutRequest) (*service.PutResponse, error) {
			return nil, errors.New("put failed")
		},
	}
	handler := NewHandler(svc, nil, nil, nil, createTestLogger())

	payload, err := EncodePayload(&service.PutRequest{
		EntityType: "user",
		ID:         1,
		Data:       []byte(`{"name":"test"}`),
	})
	if err != nil {
		t.Fatalf("Failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpPut,
		Payload: payload,
	}
	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("Expected error status for failed put")
	}
}

func TestHandler_HandleRequest_Get_Success(t *testing.T) {
	svc := &mockService{
		getFn: func(ctx context.Context, req *service.GetRequest) (*service.GetResponse, error) {
			return &service.GetResponse{
				ID:    req.ID,
				Data:  []byte("test-data"),
				Found: true,
			}, nil
		},
	}
	handler := NewHandler(svc, nil, nil, nil, createTestLogger())

	payload, err := EncodePayload(&service.GetRequest{
		EntityType: "user",
		ID:         1,
	})
	if err != nil {
		t.Fatalf("Failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpGet,
		Payload: payload,
	}
	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("Expected status OK, got: 0x%02X", resp.Status)
	}
}

func TestHandler_HandleRequest_Get_Error(t *testing.T) {
	svc := &mockService{
		getFn: func(ctx context.Context, req *service.GetRequest) (*service.GetResponse, error) {
			return nil, errors.New("get failed")
		},
	}
	handler := NewHandler(svc, nil, nil, nil, createTestLogger())

	payload, err := EncodePayload(&service.GetRequest{
		EntityType: "user",
		ID:         1,
	})
	if err != nil {
		t.Fatalf("Failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpGet,
		Payload: payload,
	}
	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("Expected error status for failed get")
	}
}

func TestHandler_HandleRequest_Delete_Success(t *testing.T) {
	svc := &mockService{
		deleteFn: func(ctx context.Context, req *service.DeleteRequest) (*service.DeleteResponse, error) {
			return &service.DeleteResponse{
				Deleted: true,
			}, nil
		},
	}
	handler := NewHandler(svc, nil, nil, nil, createTestLogger())

	payload, err := EncodePayload(&service.DeleteRequest{
		EntityType: "user",
		ID:         1,
	})
	if err != nil {
		t.Fatalf("Failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpDelete,
		Payload: payload,
	}
	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("Expected status OK, got: 0x%02X", resp.Status)
	}
}

func TestHandler_HandleRequest_Delete_Error(t *testing.T) {
	svc := &mockService{
		deleteFn: func(ctx context.Context, req *service.DeleteRequest) (*service.DeleteResponse, error) {
			return nil, errors.New("delete failed")
		},
	}
	handler := NewHandler(svc, nil, nil, nil, createTestLogger())

	payload, err := EncodePayload(&service.DeleteRequest{
		EntityType: "user",
		ID:         1,
	})
	if err != nil {
		t.Fatalf("Failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpDelete,
		Payload: payload,
	}
	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("Expected error status for failed delete")
	}
}

func TestHandler_HandleRequest_InvalidPayload(t *testing.T) {
	handler := createTestHandler()
	req := &Request{
		Opcode:  OpPut,
		Payload: []byte("invalid-payload"),
	}
	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("Expected error status for invalid payload")
	}
}
