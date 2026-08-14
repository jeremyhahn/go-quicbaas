//go:build mcp

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

package mcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
	// Trust composition (cross-sign / AIA)
	trustSvc       service.TrustService
	attestationSvc service.AttestationService
	pivSvc         service.PIVService
	fido2Svc       service.FIDO2Service

	// KV Operations
	putFunc    func(ctx context.Context, req *service.PutRequest) (*service.PutResponse, error)
	getFunc    func(ctx context.Context, req *service.GetRequest) (*service.GetResponse, error)
	deleteFunc func(ctx context.Context, req *service.DeleteRequest) (*service.DeleteResponse, error)
	listFunc   func(ctx context.Context, req *service.ListRequest, stream chan<- *service.ListResponse) error

	// Cluster Operations
	clusterStatusFunc func(ctx context.Context, req *service.ClusterStatusRequest) (*service.ClusterStatusResponse, error)
	getLeaderFunc     func(ctx context.Context, req *service.GetLeaderRequest) (*service.GetLeaderResponse, error)
	addNodeFunc       func(ctx context.Context, req *service.AddNodeRequest) (*service.AddNodeResponse, error)
	removeNodeFunc    func(ctx context.Context, req *service.RemoveNodeRequest) (*service.RemoveNodeResponse, error)

	// Backup Operations
	listBackupsFunc  func(ctx context.Context, req *service.ListBackupsRequest) (*service.ListBackupsResponse, error)
	deleteBackupFunc func(ctx context.Context, req *service.DeleteBackupRequest) (*service.DeleteBackupResponse, error)
	verifyBackupFunc func(ctx context.Context, req *service.VerifyBackupRequest) (*service.VerifyBackupResponse, error)

	// Auth Operations
	loginFunc  func(ctx context.Context, req *service.LoginRequest) (*service.LoginResponse, error)
	logoutFunc func(ctx context.Context, req *service.LogoutRequest) (*service.LogoutResponse, error)

	// RBAC Operations
	deleteUserFunc func(ctx context.Context, req *service.DeleteUserRequest) (*service.DeleteUserResponse, error)
	listUsersFunc  func(ctx context.Context, req *service.ListUsersRequest) (*service.ListUsersResponse, error)
	deleteRoleFunc func(ctx context.Context, req *service.AuthDeleteRoleRequest) (*service.AuthDeleteRoleResponse, error)
	listRolesFunc  func(ctx context.Context, req *service.AuthListRolesRequest) (*service.AuthListRolesResponse, error)
	assignRoleFunc func(ctx context.Context, req *service.AssignRoleRequest) (*service.AssignRoleResponse, error)
	revokeRoleFunc func(ctx context.Context, req *service.RevokeRoleRequest) (*service.RevokeRoleResponse, error)

	// Cache Operations
	getCacheStatsFunc func(ctx context.Context, req *service.GetCacheStatsRequest) (*service.GetCacheStatsResponse, error)
	clearCacheFunc    func(ctx context.Context, req *service.ClearCacheRequest) (*service.ClearCacheResponse, error)

	// Health Operations
	healthFunc    func(ctx context.Context, req *service.HealthRequest) (*service.HealthResponse, error)
	livenessFunc  func(ctx context.Context, req *service.LivenessRequest) (*service.LivenessResponse, error)
	readinessFunc func(ctx context.Context, req *service.ReadinessRequest) (*service.ReadinessResponse, error)

	// Operations Management
	getMigrationProgressFunc func(ctx context.Context, req *service.GetMigrationProgressRequest) (*service.GetMigrationProgressResponse, error)

	// Object Storage
	objectStorageSvc  service.ObjectStorageService
	objectTransferSvc service.ObjectTransferService
}

// KV Operations.
func (m *mockService) Put(ctx context.Context, req *service.PutRequest) (*service.PutResponse, error) {
	if m.putFunc != nil {
		return m.putFunc(ctx, req)
	}
	return &service.PutResponse{ID: req.ID, Version: 1, Timestamp: time.Now().Unix()}, nil
}

func (m *mockService) Get(ctx context.Context, req *service.GetRequest) (*service.GetResponse, error) {
	if m.getFunc != nil {
		return m.getFunc(ctx, req)
	}
	return &service.GetResponse{ID: req.ID, Data: []byte(`{"test":"data"}`), Found: true}, nil
}

func (m *mockService) Delete(ctx context.Context, req *service.DeleteRequest) (*service.DeleteResponse, error) {
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, req)
	}
	return &service.DeleteResponse{Deleted: true, Timestamp: time.Now().Unix()}, nil
}

func (m *mockService) List(ctx context.Context, req *service.ListRequest, stream chan<- *service.ListResponse) error {
	if m.listFunc != nil {
		return m.listFunc(ctx, req, stream)
	}
	stream <- &service.ListResponse{
		Entities: []*service.Entity{
			{EntityType: req.EntityType, ID: 1, Data: []byte(`{"test":"data1"}`)},
		},
	}
	close(stream)
	return nil
}

func (m *mockService) Watch(ctx context.Context, req *service.WatchRequest, stream chan<- *service.WatchResponse) (int64, error) {
	return 0, nil
}

// Batch Operations (not implemented).
func (m *mockService) BatchPut(ctx context.Context, req *service.BatchPutRequest) (*service.BatchPutResponse, error) {
	return nil, nil
}

func (m *mockService) BatchGet(ctx context.Context, req *service.BatchGetRequest) (*service.BatchGetResponse, error) {
	return nil, nil
}

func (m *mockService) BatchDelete(ctx context.Context, req *service.BatchDeleteRequest) (*service.BatchDeleteResponse, error) {
	return nil, nil
}

// Transaction Operations (not implemented).
func (m *mockService) Transaction(ctx context.Context, req *service.TransactionRequest) (*service.TransactionResponse, error) {
	return nil, nil
}

// Query Operations (not implemented).
func (m *mockService) Query(ctx context.Context, req *service.QueryRequest, stream chan<- *service.QueryResponse) error {
	return nil
}

func (m *mockService) ScanIndex(ctx context.Context, req *service.ScanRequest, stream chan<- *service.ScanResponse) error {
	return nil
}

// Cluster Operations.
func (m *mockService) ClusterStatus(ctx context.Context, req *service.ClusterStatusRequest) (*service.ClusterStatusResponse, error) {
	if m.clusterStatusFunc != nil {
		return m.clusterStatusFunc(ctx, req)
	}
	return &service.ClusterStatusResponse{
		ClusterID: "test-cluster",
		Nodes: []*service.NodeInfo{
			{NodeID: 1, Address: "127.0.0.1:8001", HealthStatus: service.HealthStatusHealthy},
		},
		Shards:    []*service.ShardInfo{{ShardID: 1, LeaderID: 1}},
		Timestamp: time.Now().Unix(),
	}, nil
}

func (m *mockService) GetLeader(ctx context.Context, req *service.GetLeaderRequest) (*service.GetLeaderResponse, error) {
	if m.getLeaderFunc != nil {
		return m.getLeaderFunc(ctx, req)
	}
	return &service.GetLeaderResponse{ShardID: req.ShardID, LeaderID: 1}, nil
}

func (m *mockService) AddNode(ctx context.Context, req *service.AddNodeRequest) (*service.AddNodeResponse, error) {
	if m.addNodeFunc != nil {
		return m.addNodeFunc(ctx, req)
	}
	return &service.AddNodeResponse{Success: true}, nil
}

func (m *mockService) RemoveNode(ctx context.Context, req *service.RemoveNodeRequest) (*service.RemoveNodeResponse, error) {
	if m.removeNodeFunc != nil {
		return m.removeNodeFunc(ctx, req)
	}
	return &service.RemoveNodeResponse{Success: true}, nil
}

func (m *mockService) CreateShard(ctx context.Context, req *service.CreateShardRequest) (*service.CreateShardResponse, error) {
	return nil, nil
}

func (m *mockService) RebalanceCluster(ctx context.Context, req *service.RebalanceClusterRequest) (*service.RebalanceClusterResponse, error) {
	return nil, nil
}

func (m *mockService) GetRebalanceProgress(ctx context.Context, req *service.GetRebalanceProgressRequest) (*service.GetRebalanceProgressResponse, error) {
	return &service.GetRebalanceProgressResponse{
		OperationID: req.OperationID,
		Status:      "completed",
		Progress:    100.0,
	}, nil
}

// Backup Operations.
func (m *mockService) CreateBackup(ctx context.Context, req *service.CreateBackupRequest) (*service.CreateBackupResponse, error) {
	return nil, nil
}

func (m *mockService) ListBackups(ctx context.Context, req *service.ListBackupsRequest) (*service.ListBackupsResponse, error) {
	if m.listBackupsFunc != nil {
		return m.listBackupsFunc(ctx, req)
	}
	return &service.ListBackupsResponse{
		Backups: []*service.BackupMetadata{
			{BackupID: "backup-1", Name: "test-backup"},
		},
	}, nil
}

func (m *mockService) RestoreBackup(ctx context.Context, req *service.RestoreBackupRequest) (*service.RestoreBackupResponse, error) {
	return nil, nil
}

func (m *mockService) DeleteBackup(ctx context.Context, req *service.DeleteBackupRequest) (*service.DeleteBackupResponse, error) {
	if m.deleteBackupFunc != nil {
		return m.deleteBackupFunc(ctx, req)
	}
	return &service.DeleteBackupResponse{Success: true}, nil
}

func (m *mockService) VerifyBackup(ctx context.Context, req *service.VerifyBackupRequest) (*service.VerifyBackupResponse, error) {
	if m.verifyBackupFunc != nil {
		return m.verifyBackupFunc(ctx, req)
	}
	return &service.VerifyBackupResponse{Valid: true, ChecksumMatch: true}, nil
}

// Auth Operations.
func (m *mockService) Login(ctx context.Context, req *service.LoginRequest) (*service.LoginResponse, error) {
	if m.loginFunc != nil {
		return m.loginFunc(ctx, req)
	}
	return &service.LoginResponse{AccessToken: "test-token", UserID: "user-1"}, nil
}

func (m *mockService) Logout(ctx context.Context, req *service.LogoutRequest) (*service.LogoutResponse, error) {
	if m.logoutFunc != nil {
		return m.logoutFunc(ctx, req)
	}
	return &service.LogoutResponse{Success: true}, nil
}

func (m *mockService) RefreshToken(ctx context.Context, req *service.RefreshTokenRequest) (*service.RefreshTokenResponse, error) {
	return nil, nil
}

func (m *mockService) ValidateToken(ctx context.Context, req *service.ValidateTokenRequest) (*service.ValidateTokenResponse, error) {
	return nil, nil
}

// RBAC Operations.
func (m *mockService) CreateUser(ctx context.Context, req *service.CreateUserRequest) (*service.CreateUserResponse, error) {
	return nil, nil
}

func (m *mockService) DeleteUser(ctx context.Context, req *service.DeleteUserRequest) (*service.DeleteUserResponse, error) {
	if m.deleteUserFunc != nil {
		return m.deleteUserFunc(ctx, req)
	}
	return &service.DeleteUserResponse{Success: true}, nil
}

func (m *mockService) ListUsers(ctx context.Context, req *service.ListUsersRequest) (*service.ListUsersResponse, error) {
	if m.listUsersFunc != nil {
		return m.listUsersFunc(ctx, req)
	}
	return &service.ListUsersResponse{
		Users: []*service.UserInfo{
			{UserID: "user-1", Username: "testuser"},
		},
	}, nil
}

func (m *mockService) CreateAuthRole(ctx context.Context, req *service.AuthCreateRoleRequest) (*service.AuthCreateRoleResponse, error) {
	return nil, nil
}

func (m *mockService) DeleteAuthRole(ctx context.Context, req *service.AuthDeleteRoleRequest) (*service.AuthDeleteRoleResponse, error) {
	if m.deleteRoleFunc != nil {
		return m.deleteRoleFunc(ctx, req)
	}
	return &service.AuthDeleteRoleResponse{Success: true}, nil
}

func (m *mockService) ListAuthRoles(ctx context.Context, req *service.AuthListRolesRequest) (*service.AuthListRolesResponse, error) {
	if m.listRolesFunc != nil {
		return m.listRolesFunc(ctx, req)
	}
	return &service.AuthListRolesResponse{
		Roles: []*service.RoleInfo{
			{RoleID: "role-1", RoleName: "admin"},
		},
	}, nil
}

func (m *mockService) AssignRole(ctx context.Context, req *service.AssignRoleRequest) (*service.AssignRoleResponse, error) {
	if m.assignRoleFunc != nil {
		return m.assignRoleFunc(ctx, req)
	}
	return &service.AssignRoleResponse{Success: true}, nil
}

func (m *mockService) RevokeRole(ctx context.Context, req *service.RevokeRoleRequest) (*service.RevokeRoleResponse, error) {
	if m.revokeRoleFunc != nil {
		return m.revokeRoleFunc(ctx, req)
	}
	return &service.RevokeRoleResponse{Success: true}, nil
}

// Cache Operations.
func (m *mockService) GetCacheStats(ctx context.Context, req *service.GetCacheStatsRequest) (*service.GetCacheStatsResponse, error) {
	if m.getCacheStatsFunc != nil {
		return m.getCacheStatsFunc(ctx, req)
	}
	return &service.GetCacheStatsResponse{HitRate: 0.85, Size: 1000, Hits: 850, Misses: 150}, nil
}

func (m *mockService) ClearCache(ctx context.Context, req *service.ClearCacheRequest) (*service.ClearCacheResponse, error) {
	if m.clearCacheFunc != nil {
		return m.clearCacheFunc(ctx, req)
	}
	return &service.ClearCacheResponse{EntriesCleared: 100}, nil
}

func (m *mockService) GetCacheConfig(ctx context.Context, req *service.GetCacheConfigRequest) (*service.GetCacheConfigResponse, error) {
	return nil, nil
}

func (m *mockService) UpdateCacheConfig(ctx context.Context, req *service.UpdateCacheConfigRequest) (*service.UpdateCacheConfigResponse, error) {
	return nil, nil
}

// Health Operations.
func (m *mockService) Health(ctx context.Context, req *service.HealthRequest) (*service.HealthResponse, error) {
	if m.healthFunc != nil {
		return m.healthFunc(ctx, req)
	}
	return &service.HealthResponse{
		Status:        service.HealthStatusHealthy,
		NodeID:        1,
		Version:       "1.0.0",
		UptimeSeconds: 3600,
	}, nil
}

func (m *mockService) Liveness(ctx context.Context, req *service.LivenessRequest) (*service.LivenessResponse, error) {
	if m.livenessFunc != nil {
		return m.livenessFunc(ctx, req)
	}
	return &service.LivenessResponse{Alive: true}, nil
}

func (m *mockService) Readiness(ctx context.Context, req *service.ReadinessRequest) (*service.ReadinessResponse, error) {
	if m.readinessFunc != nil {
		return m.readinessFunc(ctx, req)
	}
	return &service.ReadinessResponse{Ready: true}, nil
}

// Time-Series Operations (not implemented).
func (m *mockService) Aggregate(ctx context.Context, req *service.AggregateRequest) (*service.AggregateResponse, error) {
	return nil, nil
}

func (m *mockService) TimeSeriesQuery(ctx context.Context, req *service.TimeSeriesQueryRequest, stream chan<- *service.TimeSeriesQueryResponse) error {
	return nil
}

// Operations Management.
func (m *mockService) MigrateData(ctx context.Context, req *service.MigrateDataRequest) (*service.MigrateDataResponse, error) {
	return nil, nil
}

func (m *mockService) GetMigrationProgress(ctx context.Context, req *service.GetMigrationProgressRequest) (*service.GetMigrationProgressResponse, error) {
	if m.getMigrationProgressFunc != nil {
		return m.getMigrationProgressFunc(ctx, req)
	}
	return &service.GetMigrationProgressResponse{
		MigrationID:   req.MigrationID,
		Progress:      0.75,
		Status:        "in_progress",
		ItemsMigrated: 750,
		TotalItems:    1000,
	}, nil
}

func (m *mockService) ReplaceNode(ctx context.Context, req *service.ReplaceNodeRequest) (*service.ReplaceNodeResponse, error) {
	return nil, nil
}

func (m *mockService) UpgradeNode(ctx context.Context, req *service.UpgradeNodeRequest) (*service.UpgradeNodeResponse, error) {
	return nil, nil
}

func (m *mockService) GetUpgradeStatus(ctx context.Context, req *service.GetUpgradeStatusRequest) (*service.GetUpgradeStatusResponse, error) {
	return nil, nil
}

func (m *mockService) Close() error {
	return nil
}

// SetIAMProvider implements the QuicBaaS interface for setting IAM components.
func (m *mockService) SetIAMProvider(provider service.IAMComponents) {
	// No-op for mock
}

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

func (m *mockService) Ollama() service.OllamaService {
	return nil
}

func (m *mockService) Embedding() service.EmbeddingService {
	return nil
}

// Timeseries service methods
func (m *mockService) WritePoint(ctx context.Context, req *service.WritePointRequest) (*service.WritePointResponse, error) {
	return &service.WritePointResponse{Success: true}, nil
}

func (m *mockService) WriteBatch(ctx context.Context, req *service.WriteBatchRequest) (*service.WriteBatchResponse, error) {
	return &service.WriteBatchResponse{Success: true, PointsWritten: len(req.Points)}, nil
}

func (m *mockService) TSQuery(ctx context.Context, req *service.TSQueryRequest) (*service.TSQueryResponse, error) {
	return &service.TSQueryResponse{Metric: req.Metric, Points: []*service.TSDataPoint{}}, nil
}

func (m *mockService) DeleteSeries(ctx context.Context, req *service.TSDeleteRequest) (*service.TSDeleteResponse, error) {
	return &service.TSDeleteResponse{Success: true}, nil
}

func (m *mockService) GetRetentionPolicy(ctx context.Context, req *service.TSGetRetentionPolicyRequest) (*service.TSGetRetentionPolicyResponse, error) {
	return &service.TSGetRetentionPolicyResponse{Policy: nil}, nil
}

func (m *mockService) SetRetentionPolicy(ctx context.Context, req *service.TSSetRetentionPolicyRequest) (*service.TSSetRetentionPolicyResponse, error) {
	return &service.TSSetRetentionPolicyResponse{Success: true}, nil
}

func (m *mockService) ListMetrics(ctx context.Context, req *service.TSListMetricsRequest) (*service.TSListMetricsResponse, error) {
	return &service.TSListMetricsResponse{Metrics: []string{}}, nil
}

func (m *mockService) ListEntityTypes(ctx context.Context, req *service.ListEntityTypesRequest) (*service.ListEntityTypesResponse, error) {
	return &service.ListEntityTypesResponse{EntityTypes: []string{}}, nil
}

func (m *mockService) ObjectStorageService() service.ObjectStorageService {
	return m.objectStorageSvc
}

func (m *mockService) ObjectStorageAdminService() service.ObjectStorageAdminService {
	return nil
}

func (m *mockService) Certificate() service.CertificateService {
	return nil
}

func (m *mockService) Trust() service.TrustService {
	return m.trustSvc
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

func (m *mockService) ParameterStoreService() service.ParameterStoreService {
	return nil
}

func (m *mockService) ObjectTransferService() service.ObjectTransferService {
	return m.objectTransferSvc
}

func (m *mockService) Scheduler() service.SchedulerService {
	return nil
}

// SchedulerEngine is a no-op for the mcp mock; MCP tests don't drive
// the scheduler engine directly.
func (m *mockService) SchedulerEngine() *schedulerpkg.SchedulerEngine { return nil }

func (m *mockService) Project() service.ProjectService {
	return nil
}

func (m *mockService) Account() service.AccountService {
	return nil
}

func (m *mockService) SecretRegistry() *qrdbsecret.ResolverRegistry { return nil }

func (m *mockService) SetSecretRegistry(reg *qrdbsecret.ResolverRegistry) {}

// SSHClient returns nil for the mcp mock; tests that exercise SSH
// routing wire a real xkmsssh.Client directly.
func (m *mockService) SSHClient() xkmsssh.Client { return nil }

func (m *mockService) Agent() orchestration.AgentService {
	return nil
}

func (m *mockService) Messaging() messaging.Service {
	return nil
}

func (m *mockService) SetMessagingService(ms messaging.Service) {
	// No-op for mock
}

func (m *mockService) SetAgentService(agent orchestration.AgentService) {
	// No-op for mock
}

func (m *mockService) Database() service.DatabaseService {
	return nil
}

func (m *mockService) SetDatabaseService(ds service.DatabaseService) {
	// No-op for mock
}

func (m *mockService) SetQRDBClient(provider service.QRDBClientProvider) {
	// No-op for mock
}

func (m *mockService) LoginCeremony() service.LoginServicer { return nil }

func (m *mockService) SetLoginService(svc service.LoginServicer) {}

func (m *mockService) WatchManager() *watch.Manager {
	return nil
}

// SetSealService is a no-op for the mock.
func (m *mockService) SetSealService(_ service.SealService) {}

// SealService default stubs to satisfy the QuicBaaS interface.
// Tests that exercise seal behavior compose mockServiceWithSeal or supply
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

func (m *mockService) AgentAdmin() service.AgentAdminService { return nil }

// =============================================================================
// Comprehensive Server Tests
// =============================================================================

func TestComprehensive_handleInitialize(t *testing.T) {
	tests := []struct {
		name         string
		config       *Config
		wantProtocol string
		wantServer   string
		wantVersion  string
	}{
		{
			name:         "default config",
			config:       DefaultConfig(),
			wantProtocol: MCPVersion,
			wantServer:   "QuicBaaS MCP Server",
			wantVersion:  "1.0.0",
		},
		{
			name: "custom config",
			config: &Config{
				ServerName:    "Custom Server",
				ServerVersion: "2.0.0",
			},
			wantProtocol: MCPVersion,
			wantServer:   "Custom Server",
			wantVersion:  "2.0.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &mockService{}
			log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
			server := NewServer(svc, tt.config, log)

			req := &JSONRPCRequest{
				JSONRPC: "2.0",
				ID:      1,
				Method:  "initialize",
			}

			resp := server.handleInitialize(context.Background(), req)

			assert.NotNil(t, resp)
			assert.Equal(t, "2.0", resp.JSONRPC)
			assert.Equal(t, 1, resp.ID)
			assert.Nil(t, resp.Error)

			result, ok := resp.Result.(map[string]interface{})
			require.True(t, ok, "result should be a map")
			assert.Equal(t, tt.wantProtocol, result["protocolVersion"])

			serverInfo, ok := result["serverInfo"].(map[string]interface{})
			require.True(t, ok, "serverInfo should be a map")
			assert.Equal(t, tt.wantServer, serverInfo["name"])
			assert.Equal(t, tt.wantVersion, serverInfo["version"])

			capabilities, ok := result["capabilities"].(map[string]interface{})
			require.True(t, ok, "capabilities should be a map")
			assert.NotNil(t, capabilities["tools"])
			assert.NotNil(t, capabilities["resources"])
		})
	}
}

func TestComprehensive_handleToolsList(t *testing.T) {
	svc := &mockService{}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	server := NewServer(svc, DefaultConfig(), log)

	req := &JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/list",
	}

	resp := server.handleToolsList(context.Background(), req)

	assert.NotNil(t, resp)
	assert.Equal(t, "2.0", resp.JSONRPC)
	assert.Nil(t, resp.Error)

	result, ok := resp.Result.(map[string]interface{})
	require.True(t, ok, "result should be a map")

	tools, ok := result["tools"].([]ToolDefinition)
	require.True(t, ok, "tools should be a slice of ToolDefinition")

	// Verify we have all tools (base 97, plus optional IAM tools if built with -tags iam)
	assert.GreaterOrEqual(t, len(tools), 97, "should have at least 97 tools registered")

	// Verify tool names are unique
	toolNames := make(map[string]bool)
	for _, tool := range tools {
		assert.NotEmpty(t, tool.Name, "tool name should not be empty")
		assert.NotEmpty(t, tool.Description, "tool description should not be empty")
		assert.NotNil(t, tool.InputSchema, "tool input schema should not be nil")

		assert.False(t, toolNames[tool.Name], "tool name should be unique: %s", tool.Name)
		toolNames[tool.Name] = true

		// Verify schema is valid
		schema, ok := tool.InputSchema["type"]
		assert.True(t, ok, "input schema should have type field")
		assert.Equal(t, "object", schema, "input schema type should be object")
	}

	// Verify specific tools exist
	expectedTools := []string{
		"qrdb_put", "qrdb_get", "qrdb_delete", "qrdb_list",
		"quicbaas_batch_put", "quicbaas_batch_get", "quicbaas_batch_delete",
		"quicbaas_transaction", "quicbaas_query", "quicbaas_scan_index",
		"quicbaas_cluster_status", "quicbaas_get_leader", "quicbaas_add_node",
		"quicbaas_backup_create", "qrdb_user_create",
		"qrdb_health", "quicbaas_cache_stats",
	}

	for _, expectedTool := range expectedTools {
		assert.True(t, toolNames[expectedTool], "expected tool should exist: %s", expectedTool)
	}
}

func TestComprehensive_handleToolsCall(t *testing.T) {
	tests := []struct {
		name     string
		toolName string
		args     map[string]interface{}
		wantErr  bool
		setup    func(*mockService)
	}{
		{
			name:     "qrdb_put success",
			toolName: "qrdb_put",
			args: map[string]interface{}{
				"entityType": "user",
				"id":         float64(1),
				"data":       `{"name":"test"}`,
			},
			wantErr: false,
		},
		{
			name:     "qrdb_get success",
			toolName: "qrdb_get",
			args: map[string]interface{}{
				"entityType": "user",
				"id":         float64(1),
			},
			wantErr: false,
		},
		{
			name:     "qrdb_delete success",
			toolName: "qrdb_delete",
			args: map[string]interface{}{
				"entityType": "user",
				"id":         float64(1),
			},
			wantErr: false,
		},
		{
			name:     "qrdb_list success",
			toolName: "qrdb_list",
			args: map[string]interface{}{
				"entityType": "user",
			},
			wantErr: false,
		},
		{
			name:     "quicbaas_cluster_status success",
			toolName: "quicbaas_cluster_status",
			args:     map[string]interface{}{},
			wantErr:  false,
		},
		{
			name:     "quicbaas_get_leader success",
			toolName: "quicbaas_get_leader",
			args: map[string]interface{}{
				"shardID": float64(1),
			},
			wantErr: false,
		},
		{
			name:     "quicbaas_add_node success",
			toolName: "quicbaas_add_node",
			args: map[string]interface{}{
				"shardID": float64(1),
				"nodeID":  float64(2),
				"address": "127.0.0.1:8002",
			},
			wantErr: false,
		},
		{
			name:     "quicbaas_remove_node success",
			toolName: "quicbaas_remove_node",
			args: map[string]interface{}{
				"shardID": float64(1),
				"nodeID":  float64(2),
			},
			wantErr: false,
		},
		{
			name:     "quicbaas_backup_list success",
			toolName: "quicbaas_backup_list",
			args:     map[string]interface{}{},
			wantErr:  false,
		},
		{
			name:     "quicbaas_backup_delete success",
			toolName: "quicbaas_backup_delete",
			args: map[string]interface{}{
				"backupID": "backup-1",
			},
			wantErr: false,
		},
		{
			name:     "quicbaas_backup_verify success",
			toolName: "quicbaas_backup_verify",
			args: map[string]interface{}{
				"backupID": "backup-1",
			},
			wantErr: false,
		},
		{
			name:     "quicbaas_logout success",
			toolName: "quicbaas_logout",
			args: map[string]interface{}{
				"accessToken": "test-token",
			},
			wantErr: false,
		},
		{
			name:     "qrdb_user_delete success",
			toolName: "qrdb_user_delete",
			args: map[string]interface{}{
				"username": "testuser",
			},
			wantErr: false,
		},
		{
			name:     "qrdb_user_list success",
			toolName: "qrdb_user_list",
			args:     map[string]interface{}{},
			wantErr:  false,
		},
		{
			name:     "qrdb_role_delete success",
			toolName: "qrdb_role_delete",
			args: map[string]interface{}{
				"roleName": "admin",
			},
			wantErr: false,
		},
		{
			name:     "qrdb_role_list success",
			toolName: "qrdb_role_list",
			args:     map[string]interface{}{},
			wantErr:  false,
		},
		{
			name:     "qrdb_role_assign success",
			toolName: "qrdb_role_assign",
			args: map[string]interface{}{
				"username": "testuser",
				"roleName": "admin",
			},
			wantErr: false,
		},
		{
			name:     "quicbaas_role_revoke success",
			toolName: "quicbaas_role_revoke",
			args: map[string]interface{}{
				"username": "testuser",
				"roleName": "admin",
			},
			wantErr: false,
		},
		{
			name:     "quicbaas_cache_stats success",
			toolName: "quicbaas_cache_stats",
			args:     map[string]interface{}{},
			wantErr:  false,
		},
		{
			name:     "quicbaas_cache_clear success",
			toolName: "quicbaas_cache_clear",
			args: map[string]interface{}{
				"all": true,
			},
			wantErr: false,
		},
		{
			name:     "qrdb_health success",
			toolName: "qrdb_health",
			args:     map[string]interface{}{},
			wantErr:  false,
		},
		{
			name:     "quicbaas_liveness success",
			toolName: "quicbaas_liveness",
			args:     map[string]interface{}{},
			wantErr:  false,
		},
		{
			name:     "quicbaas_readiness success",
			toolName: "quicbaas_readiness",
			args:     map[string]interface{}{},
			wantErr:  false,
		},
		{
			name:     "quicbaas_migration_progress success",
			toolName: "quicbaas_migration_progress",
			args: map[string]interface{}{
				"migrationID": "migration-1",
			},
			wantErr: false,
		},
		{
			name:     "unknown tool",
			toolName: "quicbaas_unknown",
			args:     map[string]interface{}{},
			wantErr:  true,
		},
		{
			name:     "not yet implemented tool",
			toolName: "quicbaas_batch_put",
			args:     map[string]interface{}{},
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &mockService{}
			if tt.setup != nil {
				tt.setup(svc)
			}

			log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
			server := NewServer(svc, DefaultConfig(), log)

			req := &JSONRPCRequest{
				JSONRPC: "2.0",
				ID:      1,
				Method:  "tools/call",
				Params: map[string]interface{}{
					"name":      tt.toolName,
					"arguments": tt.args,
				},
			}

			resp := server.handleToolsCall(context.Background(), req)

			assert.NotNil(t, resp)
			assert.Equal(t, "2.0", resp.JSONRPC)

			if tt.wantErr {
				assert.NotNil(t, resp.Error, "expected error for tool: %s", tt.toolName)
			} else {
				assert.Nil(t, resp.Error, "expected no error for tool: %s", tt.toolName)
				assert.NotNil(t, resp.Result, "expected result for tool: %s", tt.toolName)
			}
		})
	}
}

func TestComprehensive_handleToolsCall_InvalidParams(t *testing.T) {
	tests := []struct {
		name   string
		params interface{}
	}{
		{
			name:   "params not an object",
			params: "invalid",
		},
		{
			name: "missing tool name",
			params: map[string]interface{}{
				"arguments": map[string]interface{}{},
			},
		},
		{
			name: "invalid tool name type",
			params: map[string]interface{}{
				"name": 123,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &mockService{}
			log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
			server := NewServer(svc, DefaultConfig(), log)

			req := &JSONRPCRequest{
				JSONRPC: "2.0",
				ID:      1,
				Method:  "tools/call",
				Params:  tt.params,
			}

			resp := server.handleToolsCall(context.Background(), req)

			assert.NotNil(t, resp)
			assert.NotNil(t, resp.Error)
			assert.Equal(t, InvalidParams, resp.Error.Code)
		})
	}
}

func TestComprehensive_handleResourcesList(t *testing.T) {
	svc := &mockService{}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	server := NewServer(svc, DefaultConfig(), log)

	req := &JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "resources/list",
	}

	resp := server.handleResourcesList(context.Background(), req)

	assert.NotNil(t, resp)
	assert.Equal(t, "2.0", resp.JSONRPC)
	assert.Nil(t, resp.Error)

	result, ok := resp.Result.(map[string]interface{})
	require.True(t, ok, "result should be a map")

	resources, ok := result["resources"].([]ResourceDefinition)
	require.True(t, ok, "resources should be a slice of ResourceDefinition")

	// Verify we have all resources
	assert.Greater(t, len(resources), 0, "should have resources registered")

	// Verify resource URIs are valid and unique
	resourceURIs := make(map[string]bool)
	for _, resource := range resources {
		assert.NotEmpty(t, resource.URI, "resource URI should not be empty")
		assert.NotEmpty(t, resource.Name, "resource name should not be empty")
		assert.NotEmpty(t, resource.Description, "resource description should not be empty")
		assert.Equal(t, "application/json", resource.MimeType, "resource mime type should be application/json")

		assert.False(t, resourceURIs[resource.URI], "resource URI should be unique: %s", resource.URI)
		resourceURIs[resource.URI] = true

		// Verify URI starts with quicbaas://
		assert.Contains(t, resource.URI, "quicbaas://", "resource URI should start with quicbaas://")
	}
}

func TestComprehensive_handleResourcesRead(t *testing.T) {
	tests := []struct {
		name    string
		uri     string
		wantErr bool
	}{
		{
			name:    "cluster status",
			uri:     "quicbaas://cluster/status",
			wantErr: false,
		},
		{
			name:    "cluster nodes",
			uri:     "quicbaas://cluster/nodes",
			wantErr: false,
		},
		{
			name:    "cluster shards",
			uri:     "quicbaas://cluster/shards",
			wantErr: false,
		},
		{
			name:    "metrics summary",
			uri:     "quicbaas://metrics/summary",
			wantErr: false,
		},
		{
			name:    "cache stats",
			uri:     "quicbaas://cache/stats",
			wantErr: false,
		},
		{
			name:    "health",
			uri:     "quicbaas://health",
			wantErr: false,
		},
		{
			name:    "backups",
			uri:     "quicbaas://backups",
			wantErr: false,
		},
		{
			name:    "users",
			uri:     "quicbaas://users",
			wantErr: false,
		},
		{
			name:    "roles",
			uri:     "quicbaas://roles",
			wantErr: false,
		},
		{
			name:    "entities by type",
			uri:     "quicbaas://entities/user",
			wantErr: false,
		},
		{
			name:    "specific entity",
			uri:     "quicbaas://entities/user/1",
			wantErr: false,
		},
		{
			name:    "invalid URI scheme",
			uri:     "http://cluster/status",
			wantErr: true,
		},
		{
			name:    "empty path",
			uri:     "quicbaas://",
			wantErr: true,
		},
		{
			name:    "unknown resource type",
			uri:     "quicbaas://unknown/resource",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &mockService{}
			log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
			server := NewServer(svc, DefaultConfig(), log)

			req := &JSONRPCRequest{
				JSONRPC: "2.0",
				ID:      1,
				Method:  "resources/read",
				Params: map[string]interface{}{
					"uri": tt.uri,
				},
			}

			resp := server.handleResourcesRead(context.Background(), req)

			assert.NotNil(t, resp)
			assert.Equal(t, "2.0", resp.JSONRPC)

			if tt.wantErr {
				assert.NotNil(t, resp.Error, "expected error for URI: %s", tt.uri)
			} else {
				assert.Nil(t, resp.Error, "expected no error for URI: %s", tt.uri)
				assert.NotNil(t, resp.Result, "expected result for URI: %s", tt.uri)

				// Verify result structure
				result, ok := resp.Result.(map[string]interface{})
				require.True(t, ok, "result should be a map")

				contents, ok := result["contents"]
				assert.True(t, ok, "result should have contents field")
				assert.NotNil(t, contents, "contents should not be nil")
			}
		})
	}
}

func TestComprehensive_handleResourcesRead_InvalidParams(t *testing.T) {
	tests := []struct {
		name   string
		params interface{}
	}{
		{
			name:   "params not an object",
			params: "invalid",
		},
		{
			name:   "missing URI",
			params: map[string]interface{}{},
		},
		{
			name: "invalid URI type",
			params: map[string]interface{}{
				"uri": 123,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &mockService{}
			log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
			server := NewServer(svc, DefaultConfig(), log)

			req := &JSONRPCRequest{
				JSONRPC: "2.0",
				ID:      1,
				Method:  "resources/read",
				Params:  tt.params,
			}

			resp := server.handleResourcesRead(context.Background(), req)

			assert.NotNil(t, resp)
			assert.NotNil(t, resp.Error)
			assert.Equal(t, InvalidParams, resp.Error.Code)
		})
	}
}

func TestComprehensive_handleRequest(t *testing.T) {
	tests := []struct {
		name      string
		method    string
		jsonrpc   string
		wantError bool
		errorCode int
		errorMsg  string
	}{
		{
			name:      "initialize",
			method:    "initialize",
			jsonrpc:   "2.0",
			wantError: false,
		},
		{
			name:      "tools/list",
			method:    "tools/list",
			jsonrpc:   "2.0",
			wantError: false,
		},
		{
			name:      "resources/list",
			method:    "resources/list",
			jsonrpc:   "2.0",
			wantError: false,
		},
		{
			name:      "ping",
			method:    "ping",
			jsonrpc:   "2.0",
			wantError: false,
		},
		{
			name:      "unknown method",
			method:    "unknown",
			jsonrpc:   "2.0",
			wantError: true,
			errorCode: MethodNotFound,
			errorMsg:  "Method not found",
		},
		{
			name:      "invalid JSON-RPC version",
			method:    "ping",
			jsonrpc:   "1.0",
			wantError: true,
			errorCode: InvalidRequest,
			errorMsg:  "Invalid request",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &mockService{}
			log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
			server := NewServer(svc, DefaultConfig(), log)

			req := &JSONRPCRequest{
				JSONRPC: tt.jsonrpc,
				ID:      1,
				Method:  tt.method,
			}

			resp := server.handleRequest(context.Background(), req)

			assert.NotNil(t, resp)
			assert.Equal(t, "2.0", resp.JSONRPC)

			if tt.wantError {
				assert.NotNil(t, resp.Error)
				assert.Equal(t, tt.errorCode, resp.Error.Code)
				assert.Contains(t, resp.Error.Message, tt.errorMsg)
			} else {
				assert.Nil(t, resp.Error)
				assert.NotNil(t, resp.Result)
			}
		})
	}
}

func TestComprehensive_ToolRegistry(t *testing.T) {
	svc := &mockService{}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)

	t.Run("all tools registered", func(t *testing.T) {
		tools := registry.ListTools()
		// Base count is 97 tools; IAM build tag adds 69 more (166 total)
		assert.GreaterOrEqual(t, len(tools), 97, "should have at least 97 tools (base)")
	})

	t.Run("tool names are unique", func(t *testing.T) {
		tools := registry.ListTools()
		names := make(map[string]bool)
		for _, tool := range tools {
			assert.False(t, names[tool.Name], "duplicate tool name: %s", tool.Name)
			names[tool.Name] = true
		}
	})

	t.Run("tool descriptions are non-empty", func(t *testing.T) {
		tools := registry.ListTools()
		for _, tool := range tools {
			assert.NotEmpty(t, tool.Description, "tool %s should have description", tool.Name)
		}
	})

	t.Run("tool schemas are valid", func(t *testing.T) {
		tools := registry.ListTools()
		for _, tool := range tools {
			assert.NotNil(t, tool.InputSchema, "tool %s should have schema", tool.Name)

			// Verify schema can be marshaled to JSON
			data, err := json.Marshal(tool.InputSchema)
			assert.NoError(t, err, "tool %s schema should be JSON serializable", tool.Name)
			assert.NotEmpty(t, data)

			// Verify schema has type field
			schemaType, ok := tool.InputSchema["type"]
			assert.True(t, ok, "tool %s schema should have type field", tool.Name)
			assert.Equal(t, "object", schemaType, "tool %s schema type should be object", tool.Name)
		}
	})
}

func TestComprehensive_ResourceRegistry(t *testing.T) {
	svc := &mockService{}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewResourceRegistry(svc, log)

	t.Run("all resources registered", func(t *testing.T) {
		resources := registry.ListResources()
		assert.Greater(t, len(resources), 0, "should have resources")
	})

	t.Run("resource URIs are valid", func(t *testing.T) {
		resources := registry.ListResources()
		for _, resource := range resources {
			assert.NotEmpty(t, resource.URI, "resource should have URI")
			assert.Contains(t, resource.URI, "quicbaas://", "URI should start with quicbaas://")
		}
	})

	t.Run("resource URIs are unique", func(t *testing.T) {
		resources := registry.ListResources()
		uris := make(map[string]bool)
		for _, resource := range resources {
			assert.False(t, uris[resource.URI], "duplicate resource URI: %s", resource.URI)
			uris[resource.URI] = true
		}
	})

	t.Run("resource names are non-empty", func(t *testing.T) {
		resources := registry.ListResources()
		for _, resource := range resources {
			assert.NotEmpty(t, resource.Name, "resource should have name")
			assert.NotEmpty(t, resource.Description, "resource should have description")
		}
	})

	t.Run("resource mime types are valid", func(t *testing.T) {
		resources := registry.ListResources()
		for _, resource := range resources {
			assert.Equal(t, "application/json", resource.MimeType, "resource mime type should be application/json")
		}
	})
}

// =============================================================================
// Helper Function Tests
// =============================================================================

func TestComprehensive_getString(t *testing.T) {
	tests := []struct {
		name string
		args map[string]interface{}
		key  string
		want string
	}{
		{
			name: "valid string",
			args: map[string]interface{}{"key": "value"},
			key:  "key",
			want: "value",
		},
		{
			name: "missing key",
			args: map[string]interface{}{},
			key:  "key",
			want: "",
		},
		{
			name: "wrong type",
			args: map[string]interface{}{"key": 123},
			key:  "key",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getString(tt.args, tt.key)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestComprehensive_getUint64(t *testing.T) {
	tests := []struct {
		name string
		args map[string]interface{}
		key  string
		want uint64
	}{
		{
			name: "float64",
			args: map[string]interface{}{"key": float64(123)},
			key:  "key",
			want: 123,
		},
		{
			name: "int",
			args: map[string]interface{}{"key": 123},
			key:  "key",
			want: 123,
		},
		{
			name: "int64",
			args: map[string]interface{}{"key": int64(123)},
			key:  "key",
			want: 123,
		},
		{
			name: "uint64",
			args: map[string]interface{}{"key": uint64(123)},
			key:  "key",
			want: 123,
		},
		{
			name: "missing key",
			args: map[string]interface{}{},
			key:  "key",
			want: 0,
		},
		{
			name: "wrong type",
			args: map[string]interface{}{"key": "string"},
			key:  "key",
			want: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getUint64(tt.args, tt.key)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestComprehensive_getBool(t *testing.T) {
	tests := []struct {
		name string
		args map[string]interface{}
		key  string
		want bool
	}{
		{
			name: "true",
			args: map[string]interface{}{"key": true},
			key:  "key",
			want: true,
		},
		{
			name: "false",
			args: map[string]interface{}{"key": false},
			key:  "key",
			want: false,
		},
		{
			name: "missing key",
			args: map[string]interface{}{},
			key:  "key",
			want: false,
		},
		{
			name: "wrong type",
			args: map[string]interface{}{"key": "true"},
			key:  "key",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getBool(tt.args, tt.key)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestComprehensive_marshalResult(t *testing.T) {
	tests := []struct {
		name    string
		input   interface{}
		wantErr bool
	}{
		{
			name: "simple map",
			input: map[string]interface{}{
				"key": "value",
			},
			wantErr: false,
		},
		{
			name: "nested structure",
			input: map[string]interface{}{
				"level1": map[string]interface{}{
					"level2": "value",
				},
			},
			wantErr: false,
		},
		{
			name: "with numbers",
			input: map[string]interface{}{
				"int":   123,
				"float": 123.45,
			},
			wantErr: false,
		},
		{
			name: "with array",
			input: map[string]interface{}{
				"array": []string{"a", "b", "c"},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := marshalResult(tt.input)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
			}
		})
	}
}
