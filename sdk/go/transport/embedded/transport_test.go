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

package embedded

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsecret "github.com/jeremyhahn/go-qrdb/sdk/go/secret"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/messaging"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"

	xkmsssh "github.com/jeremyhahn/go-xkms/sdk/go/client/ssh"

	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/watch"
	healthclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/health"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// recvStream is the receive half of a transport stream. Declared locally so the
// helper below needs no import that the tests would shadow with their own
// `transport` variable.
type recvStream interface {
	Recv() (interface{}, error)
}

// maxStreamDrainAttempts bounds the drain in requireStreamInvalidRequestType so
// a stream that never terminates fails the test instead of hanging it.
const maxStreamDrainAttempts = 16

// requireStreamInvalidRequestType drains s to completion and requires that an
// *InvalidRequestTypeError was delivered, whichever channel carried it.
//
// A single Recv cannot decide this. The transport delivers a handler error as a
// VALUE on the receive channel and then calls Close, which cancels the stream
// context and closes that channel in the same call. A receiver already blocked
// inside Recv therefore has the buffered value and ctx.Done ready at the same
// moment and may observe either, so one Recv can report context.Canceled while
// the error it was looking for is still sitting in the buffer. Draining settles
// it: a closed channel still hands out its buffered values, so a later call
// recovers the error. io.EOF and ErrRecvOnClosedStream are reported only once
// the buffer is drained, which makes them genuine terminals.
//
// The assertion is deliberately outside every branch. However the stream
// behaves, this fails unless an *InvalidRequestTypeError was actually observed,
// so the subtest cannot pass by taking a path that checks nothing.
func requireStreamInvalidRequestType(t *testing.T, s recvStream) {
	t.Helper()

	var invalidReqErr *InvalidRequestTypeError
	found := false

	for attempt := 0; attempt < maxStreamDrainAttempts && !found; attempt++ {
		resp, recvErr := s.Recv()

		if recvErr != nil {
			if errors.As(recvErr, &invalidReqErr) {
				found = true
				break
			}
			if errors.Is(recvErr, io.EOF) || errors.Is(recvErr, ErrRecvOnClosedStream) {
				// Terminal: the buffer is drained, nothing more is coming.
				break
			}
			// A cancellation racing a buffered value. Retry: the value is
			// still recoverable from the closed channel.
			continue
		}

		if respErr, ok := resp.(error); ok && errors.As(respErr, &invalidReqErr) {
			found = true
		}
	}

	require.True(t, found,
		"stream did not deliver an *InvalidRequestTypeError for a wrong-typed request")
}

// =============================================================================
// Mock Service
// =============================================================================

type mockQuicBaaS struct {
	addNodeFunc           func(ctx context.Context, req *service.AddNodeRequest) (*service.AddNodeResponse, error)
	clusterStatusFunc     func(ctx context.Context, req *service.ClusterStatusRequest) (*service.ClusterStatusResponse, error)
	writePointFunc        func(ctx context.Context, req *service.WritePointRequest) (*service.WritePointResponse, error)
	putFunc               func(ctx context.Context, req *service.PutRequest) (*service.PutResponse, error)
	compareAndSwapFunc    func(ctx context.Context, req *service.CompareAndSwapRequest) (*service.ConditionalPutResponse, error)
	putIfAbsentFunc       func(ctx context.Context, req *service.PutIfAbsentRequest) (*service.ConditionalPutResponse, error)
	getFunc               func(ctx context.Context, req *service.GetRequest) (*service.GetResponse, error)
	deleteFunc            func(ctx context.Context, req *service.DeleteRequest) (*service.DeleteResponse, error)
	listFunc              func(ctx context.Context, req *service.ListRequest, stream chan<- *service.ListResponse) error
	watchFunc             func(ctx context.Context, req *service.WatchRequest, stream chan<- *service.WatchResponse) (int64, error)
	listEntityTypesFunc   func(ctx context.Context, req *service.ListEntityTypesRequest) (*service.ListEntityTypesResponse, error)
	batchPutFunc          func(ctx context.Context, req *service.BatchPutRequest) (*service.BatchPutResponse, error)
	batchGetFunc          func(ctx context.Context, req *service.BatchGetRequest) (*service.BatchGetResponse, error)
	batchDeleteFunc       func(ctx context.Context, req *service.BatchDeleteRequest) (*service.BatchDeleteResponse, error)
	batchExecuteFunc      func(ctx context.Context, req *service.BatchExecuteRequest) (*service.BatchExecuteResponse, error)
	transactionFunc       func(ctx context.Context, req *service.TransactionRequest) (*service.TransactionResponse, error)
	healthFunc            func(ctx context.Context, req *service.HealthRequest) (*service.HealthResponse, error)
	livenessFunc          func(ctx context.Context, req *service.LivenessRequest) (*service.LivenessResponse, error)
	readinessFunc         func(ctx context.Context, req *service.ReadinessRequest) (*service.ReadinessResponse, error)
	queryFunc             func(ctx context.Context, req *service.QueryRequest, stream chan<- *service.QueryResponse) error
	scanIndexFunc         func(ctx context.Context, req *service.ScanRequest, stream chan<- *service.ScanResponse) error
	timeSeriesQueryFunc   func(ctx context.Context, req *service.TimeSeriesQueryRequest, stream chan<- *service.TimeSeriesQueryResponse) error
	validateTokenFunc     func(ctx context.Context, req *service.ValidateTokenRequest) (*service.ValidateTokenResponse, error)
	introspectTokenFunc   func(ctx context.Context, req *service.IntrospectTokenRequest) (*service.TokenIntrospection, error)
	kmsService            service.KMSService
	secretsService        service.SecretsService
	parameterStoreService service.ParameterStoreService
	objectStorageService  service.ObjectStorageService
}

// KV operations
func (m *mockQuicBaaS) Put(ctx context.Context, req *service.PutRequest) (*service.PutResponse, error) {
	if m.putFunc != nil {
		return m.putFunc(ctx, req)
	}
	return &service.PutResponse{ID: req.ID, Version: 1}, nil
}

func (m *mockQuicBaaS) CompareAndSwap(ctx context.Context, req *service.CompareAndSwapRequest) (*service.ConditionalPutResponse, error) {
	if m.compareAndSwapFunc != nil {
		return m.compareAndSwapFunc(ctx, req)
	}
	return &service.ConditionalPutResponse{}, nil
}

func (m *mockQuicBaaS) PutIfAbsent(ctx context.Context, req *service.PutIfAbsentRequest) (*service.ConditionalPutResponse, error) {
	if m.putIfAbsentFunc != nil {
		return m.putIfAbsentFunc(ctx, req)
	}
	return &service.ConditionalPutResponse{}, nil
}

func (m *mockQuicBaaS) Get(ctx context.Context, req *service.GetRequest) (*service.GetResponse, error) {
	if m.getFunc != nil {
		return m.getFunc(ctx, req)
	}
	return &service.GetResponse{ID: req.ID, Data: []byte("test data"), Found: true}, nil
}

func (m *mockQuicBaaS) Delete(ctx context.Context, req *service.DeleteRequest) (*service.DeleteResponse, error) {
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, req)
	}
	return &service.DeleteResponse{Deleted: true}, nil
}

func (m *mockQuicBaaS) List(ctx context.Context, req *service.ListRequest, stream chan<- *service.ListResponse) error {
	if m.listFunc != nil {
		return m.listFunc(ctx, req, stream)
	}
	stream <- &service.ListResponse{
		Entities:   []*service.Entity{{ID: 1, Data: []byte("test")}},
		TotalCount: 1,
	}
	close(stream)
	return nil
}

func (m *mockQuicBaaS) Watch(ctx context.Context, req *service.WatchRequest, stream chan<- *service.WatchResponse) (int64, error) {
	if m.watchFunc != nil {
		return m.watchFunc(ctx, req, stream)
	}
	return 1, nil
}

func (m *mockQuicBaaS) ListEntityTypes(ctx context.Context, req *service.ListEntityTypesRequest) (*service.ListEntityTypesResponse, error) {
	if m.listEntityTypesFunc != nil {
		return m.listEntityTypesFunc(ctx, req)
	}
	return &service.ListEntityTypesResponse{EntityTypes: []string{"test"}}, nil
}

// Batch operations
func (m *mockQuicBaaS) BatchPut(ctx context.Context, req *service.BatchPutRequest) (*service.BatchPutResponse, error) {
	if m.batchPutFunc != nil {
		return m.batchPutFunc(ctx, req)
	}
	return &service.BatchPutResponse{}, nil
}

func (m *mockQuicBaaS) BatchGet(ctx context.Context, req *service.BatchGetRequest) (*service.BatchGetResponse, error) {
	if m.batchGetFunc != nil {
		return m.batchGetFunc(ctx, req)
	}
	return &service.BatchGetResponse{}, nil
}

func (m *mockQuicBaaS) BatchDelete(ctx context.Context, req *service.BatchDeleteRequest) (*service.BatchDeleteResponse, error) {
	if m.batchDeleteFunc != nil {
		return m.batchDeleteFunc(ctx, req)
	}
	return &service.BatchDeleteResponse{}, nil
}

func (m *mockQuicBaaS) BatchExecute(ctx context.Context, req *service.BatchExecuteRequest) (*service.BatchExecuteResponse, error) {
	if m.batchExecuteFunc != nil {
		return m.batchExecuteFunc(ctx, req)
	}
	return &service.BatchExecuteResponse{}, nil
}

// Transaction operations
func (m *mockQuicBaaS) Transaction(ctx context.Context, req *service.TransactionRequest) (*service.TransactionResponse, error) {
	if m.transactionFunc != nil {
		return m.transactionFunc(ctx, req)
	}
	return &service.TransactionResponse{Succeeded: true}, nil
}

// Query operations
func (m *mockQuicBaaS) Query(ctx context.Context, req *service.QueryRequest, stream chan<- *service.QueryResponse) error {
	if m.queryFunc != nil {
		return m.queryFunc(ctx, req, stream)
	}
	stream <- &service.QueryResponse{Entities: []*service.Entity{{ID: 1}}}
	close(stream)
	return nil
}

func (m *mockQuicBaaS) ScanIndex(ctx context.Context, req *service.ScanRequest, stream chan<- *service.ScanResponse) error {
	if m.scanIndexFunc != nil {
		return m.scanIndexFunc(ctx, req, stream)
	}
	stream <- &service.ScanResponse{Entities: []*service.Entity{{ID: 1}}}
	close(stream)
	return nil
}

// Cluster operations
func (m *mockQuicBaaS) ClusterStatus(ctx context.Context, req *service.ClusterStatusRequest) (*service.ClusterStatusResponse, error) {
	if m.clusterStatusFunc != nil {
		return m.clusterStatusFunc(ctx, req)
	}
	return &service.ClusterStatusResponse{ClusterID: "test"}, nil
}

func (m *mockQuicBaaS) GetLeader(ctx context.Context, req *service.GetLeaderRequest) (*service.GetLeaderResponse, error) {
	return &service.GetLeaderResponse{LeaderID: 1}, nil
}

func (m *mockQuicBaaS) AddNode(ctx context.Context, req *service.AddNodeRequest) (*service.AddNodeResponse, error) {
	if m.addNodeFunc != nil {
		return m.addNodeFunc(ctx, req)
	}
	return &service.AddNodeResponse{Success: true}, nil
}

func (m *mockQuicBaaS) RemoveNode(ctx context.Context, req *service.RemoveNodeRequest) (*service.RemoveNodeResponse, error) {
	return &service.RemoveNodeResponse{Success: true}, nil
}

func (m *mockQuicBaaS) CreateShard(ctx context.Context, req *service.CreateShardRequest) (*service.CreateShardResponse, error) {
	return &service.CreateShardResponse{ShardID: 1}, nil
}

func (m *mockQuicBaaS) RebalanceCluster(ctx context.Context, req *service.RebalanceClusterRequest) (*service.RebalanceClusterResponse, error) {
	return &service.RebalanceClusterResponse{JobID: "job1"}, nil
}

func (m *mockQuicBaaS) GetRebalanceProgress(ctx context.Context, req *service.GetRebalanceProgressRequest) (*service.GetRebalanceProgressResponse, error) {
	return &service.GetRebalanceProgressResponse{Status: "completed"}, nil
}

// Backup operations
func (m *mockQuicBaaS) CreateBackup(ctx context.Context, req *service.CreateBackupRequest) (*service.CreateBackupResponse, error) {
	return &service.CreateBackupResponse{BackupID: "backup1"}, nil
}

func (m *mockQuicBaaS) ListBackups(ctx context.Context, req *service.ListBackupsRequest) (*service.ListBackupsResponse, error) {
	return &service.ListBackupsResponse{}, nil
}

func (m *mockQuicBaaS) RestoreBackup(ctx context.Context, req *service.RestoreBackupRequest) (*service.RestoreBackupResponse, error) {
	return &service.RestoreBackupResponse{JobID: "restore1"}, nil
}

func (m *mockQuicBaaS) DeleteBackup(ctx context.Context, req *service.DeleteBackupRequest) (*service.DeleteBackupResponse, error) {
	return &service.DeleteBackupResponse{Success: true}, nil
}

func (m *mockQuicBaaS) VerifyBackup(ctx context.Context, req *service.VerifyBackupRequest) (*service.VerifyBackupResponse, error) {
	return &service.VerifyBackupResponse{Valid: true}, nil
}

// Auth operations
func (m *mockQuicBaaS) Login(ctx context.Context, req *service.LoginRequest) (*service.LoginResponse, error) {
	return &service.LoginResponse{AccessToken: "token"}, nil
}

func (m *mockQuicBaaS) Logout(ctx context.Context, req *service.LogoutRequest) (*service.LogoutResponse, error) {
	return &service.LogoutResponse{Success: true}, nil
}

func (m *mockQuicBaaS) RefreshToken(ctx context.Context, req *service.RefreshTokenRequest) (*service.RefreshTokenResponse, error) {
	return &service.RefreshTokenResponse{AccessToken: "new-token"}, nil
}

func (m *mockQuicBaaS) ValidateToken(ctx context.Context, req *service.ValidateTokenRequest) (*service.ValidateTokenResponse, error) {
	if m.validateTokenFunc != nil {
		return m.validateTokenFunc(ctx, req)
	}
	return &service.ValidateTokenResponse{Valid: true}, nil
}

func (m *mockQuicBaaS) IntrospectToken(ctx context.Context, req *service.IntrospectTokenRequest) (*service.TokenIntrospection, error) {
	if m.introspectTokenFunc != nil {
		return m.introspectTokenFunc(ctx, req)
	}
	return &service.TokenIntrospection{}, nil
}

// RBAC operations

func (m *mockQuicBaaS) CreateAuthRole(ctx context.Context, req *service.AuthCreateRoleRequest) (*service.AuthCreateRoleResponse, error) {
	return &service.AuthCreateRoleResponse{}, nil
}

func (m *mockQuicBaaS) DeleteAuthRole(ctx context.Context, req *service.AuthDeleteRoleRequest) (*service.AuthDeleteRoleResponse, error) {
	return &service.AuthDeleteRoleResponse{}, nil
}

func (m *mockQuicBaaS) ListAuthRoles(ctx context.Context, req *service.AuthListRolesRequest) (*service.AuthListRolesResponse, error) {
	return &service.AuthListRolesResponse{}, nil
}

func (m *mockQuicBaaS) AssignRole(ctx context.Context, req *service.AuthAssignRoleRequest) (*service.AuthAssignRoleResponse, error) {
	return &service.AuthAssignRoleResponse{}, nil
}

func (m *mockQuicBaaS) RevokeRole(ctx context.Context, req *service.AuthRevokeRoleRequest) (*service.AuthRevokeRoleResponse, error) {
	return &service.AuthRevokeRoleResponse{}, nil
}

// Cache operations
func (m *mockQuicBaaS) GetCacheStats(ctx context.Context, req *service.GetCacheStatsRequest) (*service.GetCacheStatsResponse, error) {
	return &service.GetCacheStatsResponse{HitRate: 0.95}, nil
}

func (m *mockQuicBaaS) ClearCache(ctx context.Context, req *service.ClearCacheRequest) (*service.ClearCacheResponse, error) {
	return &service.ClearCacheResponse{EntriesCleared: 100}, nil
}

func (m *mockQuicBaaS) GetCacheConfig(ctx context.Context, req *service.GetCacheConfigRequest) (*service.GetCacheConfigResponse, error) {
	return &service.GetCacheConfigResponse{Enabled: true}, nil
}

func (m *mockQuicBaaS) UpdateCacheConfig(ctx context.Context, req *service.UpdateCacheConfigRequest) (*service.UpdateCacheConfigResponse, error) {
	return &service.UpdateCacheConfigResponse{Updated: true}, nil
}

// Health operations
func (m *mockQuicBaaS) Health(ctx context.Context, req *service.HealthRequest) (*service.HealthResponse, error) {
	if m.healthFunc != nil {
		return m.healthFunc(ctx, req)
	}
	return &service.HealthResponse{Status: service.HealthStatusHealthy}, nil
}

func (m *mockQuicBaaS) Liveness(ctx context.Context, req *service.LivenessRequest) (*service.LivenessResponse, error) {
	if m.livenessFunc != nil {
		return m.livenessFunc(ctx, req)
	}
	return &service.LivenessResponse{Alive: true}, nil
}

func (m *mockQuicBaaS) Readiness(ctx context.Context, req *service.ReadinessRequest) (*service.ReadinessResponse, error) {
	if m.readinessFunc != nil {
		return m.readinessFunc(ctx, req)
	}
	return &service.ReadinessResponse{Ready: true}, nil
}

// TimeSeries operations
func (m *mockQuicBaaS) Aggregate(ctx context.Context, req *service.AggregateRequest) (*service.AggregateResponse, error) {
	return &service.AggregateResponse{Result: 100.0}, nil
}

func (m *mockQuicBaaS) TimeSeriesQuery(ctx context.Context, req *service.TimeSeriesQueryRequest, stream chan<- *service.TimeSeriesQueryResponse) error {
	if m.timeSeriesQueryFunc != nil {
		return m.timeSeriesQueryFunc(ctx, req, stream)
	}
	stream <- &service.TimeSeriesQueryResponse{DataPoints: []*service.TimeSeriesPoint{{ID: 1}}}
	close(stream)
	return nil
}

func (m *mockQuicBaaS) WritePoint(ctx context.Context, req *service.WritePointRequest) (*service.WritePointResponse, error) {
	if m.writePointFunc != nil {
		return m.writePointFunc(ctx, req)
	}
	return &service.WritePointResponse{Success: true}, nil
}

func (m *mockQuicBaaS) WriteBatch(ctx context.Context, req *service.WriteBatchRequest) (*service.WriteBatchResponse, error) {
	return &service.WriteBatchResponse{Success: true}, nil
}

func (m *mockQuicBaaS) TSQuery(ctx context.Context, req *service.TSQueryRequest) (*service.TSQueryResponse, error) {
	return &service.TSQueryResponse{}, nil
}

func (m *mockQuicBaaS) DeleteSeries(ctx context.Context, req *service.TSDeleteRequest) (*service.TSDeleteResponse, error) {
	return &service.TSDeleteResponse{Success: true}, nil
}

func (m *mockQuicBaaS) GetRetentionPolicy(ctx context.Context, req *service.TSGetRetentionPolicyRequest) (*service.TSGetRetentionPolicyResponse, error) {
	return &service.TSGetRetentionPolicyResponse{}, nil
}

func (m *mockQuicBaaS) SetRetentionPolicy(ctx context.Context, req *service.TSSetRetentionPolicyRequest) (*service.TSSetRetentionPolicyResponse, error) {
	return &service.TSSetRetentionPolicyResponse{Success: true}, nil
}

func (m *mockQuicBaaS) ListMetrics(ctx context.Context, req *service.TSListMetricsRequest) (*service.TSListMetricsResponse, error) {
	return &service.TSListMetricsResponse{Metrics: []string{"cpu", "memory"}}, nil
}

// Operations
func (m *mockQuicBaaS) MigrateData(ctx context.Context, req *service.MigrateDataRequest) (*service.MigrateDataResponse, error) {
	return &service.MigrateDataResponse{MigrationID: "mig1"}, nil
}

func (m *mockQuicBaaS) GetMigrationProgress(ctx context.Context, req *service.GetMigrationProgressRequest) (*service.GetMigrationProgressResponse, error) {
	return &service.GetMigrationProgressResponse{Progress: 50.0}, nil
}

func (m *mockQuicBaaS) ReplaceNode(ctx context.Context, req *service.ReplaceNodeRequest) (*service.ReplaceNodeResponse, error) {
	return &service.ReplaceNodeResponse{JobID: "replace1"}, nil
}

func (m *mockQuicBaaS) UpgradeNode(ctx context.Context, req *service.UpgradeNodeRequest) (*service.UpgradeNodeResponse, error) {
	return &service.UpgradeNodeResponse{JobID: "upgrade1"}, nil
}

func (m *mockQuicBaaS) GetUpgradeStatus(ctx context.Context, req *service.GetUpgradeStatusRequest) (*service.GetUpgradeStatusResponse, error) {
	return &service.GetUpgradeStatusResponse{Status: "completed"}, nil
}

// Sub-services
func (m *mockQuicBaaS) KMS() service.KMSService                       { return m.kmsService }
func (m *mockQuicBaaS) KMSManager() interfaces.KMSManager             { return nil }
func (m *mockQuicBaaS) Secrets() service.SecretsService               { return m.secretsService }
func (m *mockQuicBaaS) Crypto() service.CryptoService                 { return nil }
func (m *mockQuicBaaS) Vector() service.VectorService                 { return nil }
func (m *mockQuicBaaS) Embedding() service.EmbeddingService           { return nil }
func (m *mockQuicBaaS) Ollama() service.OllamaService                 { return nil }
func (m *mockQuicBaaS) Certificate() service.CertificateService       { return nil }
func (m *mockQuicBaaS) Trust() service.TrustService                   { return nil }
func (m *mockQuicBaaS) Attestation() service.AttestationService       { return nil }
func (m *mockQuicBaaS) Registry() service.RegistryService             { return nil }
func (m *mockQuicBaaS) PIV() service.PIVService                       { return nil }
func (m *mockQuicBaaS) FIDO2() service.FIDO2Service                   { return nil }
func (m *mockQuicBaaS) ACME() service.ACMEServicer                    { return nil }
func (m *mockQuicBaaS) DynamicSecrets() service.DynamicSecretsService { return nil }
func (m *mockQuicBaaS) ObjectStorageService() service.ObjectStorageService {
	return m.objectStorageService
}
func (m *mockQuicBaaS) ObjectStorageAdminService() service.ObjectStorageAdminService { return nil }
func (m *mockQuicBaaS) ParameterStoreService() service.ParameterStoreService {
	return m.parameterStoreService
}
func (m *mockQuicBaaS) ObjectTransferService() service.ObjectTransferService       { return nil }
func (m *mockQuicBaaS) Scheduler() service.SchedulerService                        { return nil }
func (m *mockQuicBaaS) SchedulerEngine() *scheduler.SchedulerEngine                { return nil }
func (m *mockQuicBaaS) Agent() orchestration.AgentService                          { return nil }
func (m *mockQuicBaaS) SSHClient() xkmsssh.Client                                  { return nil }
func (m *mockQuicBaaS) SetIAMProvider(provider service.IAMComponents)              {}
func (m *mockQuicBaaS) Messaging() messaging.Service                               { return nil }
func (m *mockQuicBaaS) SetMessagingService(ms messaging.Service)                   {}
func (m *mockQuicBaaS) SetDynamicSecretsService(svc service.DynamicSecretsService) {}
func (m *mockQuicBaaS) SetAgentService(agent orchestration.AgentService)           {}
func (m *mockQuicBaaS) SDLC() *service.SDLCService                                 { return nil }
func (m *mockQuicBaaS) SetSDLCService(*service.SDLCService)                        {}
func (m *mockQuicBaaS) Prompts() *service.PromptService                            { return nil }
func (m *mockQuicBaaS) SetPromptService(*service.PromptService)                    {}
func (m *mockQuicBaaS) SetProjectBindingReader(sdlc.BindingReader)                 {}
func (m *mockQuicBaaS) WatchManager() *watch.Manager                               { return nil }
func (m *mockQuicBaaS) SetQRDBClient(provider service.QRDBClientProvider)          {}
func (m *mockQuicBaaS) Database() service.DatabaseService                          { return nil }
func (m *mockQuicBaaS) SetDatabaseService(ds service.DatabaseService)              {}
func (m *mockQuicBaaS) FleetAdmin() service.FleetAdminService                      { return nil }
func (m *mockQuicBaaS) Project() service.ProjectService                            { return nil }
func (m *mockQuicBaaS) Account() service.AccountService                            { return nil }
func (m *mockQuicBaaS) SecretRegistry() *qrdbsecret.ResolverRegistry               { return nil }
func (m *mockQuicBaaS) SetSecretRegistry(reg *qrdbsecret.ResolverRegistry)         {}
func (m *mockQuicBaaS) Close() error                                               { return nil }
func (m *mockQuicBaaS) LoginCeremony() service.LoginServicer                       { return nil }
func (m *mockQuicBaaS) SetLoginService(svc service.LoginServicer)                  {}

// =============================================================================
// Mock KMS Service
// =============================================================================

type mockKMSService struct{}

func (m *mockKMSService) GenerateKey(ctx context.Context, req *service.GenerateKeyRequest) (*service.GenerateKeyResponse, error) {
	return &service.GenerateKeyResponse{}, nil
}

func (m *mockKMSService) ImportKey(ctx context.Context, req *service.ImportKeyRequest) (*service.ImportKeyResponse, error) {
	return &service.ImportKeyResponse{}, nil
}

func (m *mockKMSService) GetKey(ctx context.Context, req *service.GetKeyRequest) (*service.GetKeyResponse, error) {
	return &service.GetKeyResponse{}, nil
}

func (m *mockKMSService) GetPublicKey(ctx context.Context, req *service.GetPublicKeyRequest) (*service.GetPublicKeyResponse, error) {
	return &service.GetPublicKeyResponse{}, nil
}

func (m *mockKMSService) ListKeys(ctx context.Context, req *service.ListKeysRequest) (*service.ListKeysResponse, error) {
	return &service.ListKeysResponse{}, nil
}

func (m *mockKMSService) RotateKey(ctx context.Context, req *service.RotateKeyRequest) (*service.RotateKeyResponse, error) {
	return &service.RotateKeyResponse{}, nil
}

func (m *mockKMSService) RevokeKey(ctx context.Context, req *service.RevokeKeyRequest) (*service.RevokeKeyResponse, error) {
	return &service.RevokeKeyResponse{}, nil
}

func (m *mockKMSService) DeleteKey(ctx context.Context, req *service.DeleteKeyRequest) (*service.DeleteKeyResponse, error) {
	return &service.DeleteKeyResponse{}, nil
}

func (m *mockKMSService) GetKeyMetadata(ctx context.Context, req *service.GetKeyMetadataRequest) (*service.GetKeyMetadataResponse, error) {
	return &service.GetKeyMetadataResponse{}, nil
}

func (m *mockKMSService) UpdateKeyMetadata(ctx context.Context, req *service.UpdateKeyMetadataRequest) (*service.UpdateKeyMetadataResponse, error) {
	return &service.UpdateKeyMetadataResponse{}, nil
}

func (m *mockKMSService) UpdateKeyStatus(ctx context.Context, req *service.UpdateKeyStatusRequest) (*service.UpdateKeyStatusResponse, error) {
	return &service.UpdateKeyStatusResponse{}, nil
}

func (m *mockKMSService) ListKeyVersions(ctx context.Context, req *service.ListKeyVersionsRequest) (*service.ListKeyVersionsResponse, error) {
	return &service.ListKeyVersionsResponse{}, nil
}

func (m *mockKMSService) SetKeyPolicy(ctx context.Context, req *service.SetKeyPolicyRequest) (*service.SetKeyPolicyResponse, error) {
	return &service.SetKeyPolicyResponse{}, nil
}

func (m *mockKMSService) GetKeyPolicy(ctx context.Context, req *service.GetKeyPolicyRequest) (*service.GetKeyPolicyResponse, error) {
	return &service.GetKeyPolicyResponse{}, nil
}

func (m *mockKMSService) GetSupportedAlgorithms(ctx context.Context) ([]string, error) {
	return []string{"AES-256-GCM", "ChaCha20-Poly1305"}, nil
}

func (m *mockKMSService) BackendStatus(ctx context.Context) (*service.BackendStatusResponse, error) {
	return &service.BackendStatusResponse{BackendType: "software", Status: "healthy"}, nil
}

func (m *mockKMSService) BackendTest(ctx context.Context) (*service.BackendTestResponse, error) {
	return &service.BackendTestResponse{Success: true}, nil
}

func (m *mockKMSService) ExportKey(ctx context.Context, req *service.ExportKeyRequest) (*service.ExportKeyResponse, error) {
	return &service.ExportKeyResponse{}, nil
}

func (m *mockKMSService) UnwrapKey(ctx context.Context, req *service.UnwrapKeyRequest) (*service.UnwrapKeyResponse, error) {
	return &service.UnwrapKeyResponse{}, nil
}

func (m *mockKMSService) SetKeySafety(ctx context.Context, req *service.SetKeySafetyRequest) (*service.SetKeySafetyResponse, error) {
	return &service.SetKeySafetyResponse{}, nil
}

func (m *mockKMSService) ClearKeySafety(ctx context.Context, req *service.ClearKeySafetyRequest) (*service.ClearKeySafetyResponse, error) {
	return &service.ClearKeySafetyResponse{}, nil
}

func (m *mockKMSService) SafetyCheck(ctx context.Context, req *service.SafetyCheckRequest) (*service.SafetyCheckResponse, error) {
	return &service.SafetyCheckResponse{}, nil
}

func (m *mockKMSService) SafetyReport(ctx context.Context, req *service.SafetyReportRequest) (*service.SafetyReportResponse, error) {
	return &service.SafetyReportResponse{}, nil
}

func (m *mockKMSService) CreateDEK(ctx context.Context, req *service.CreateDEKRequest) (*service.CreateDEKResponse, error) {
	return &service.CreateDEKResponse{}, nil
}

func (m *mockKMSService) GetDEK(ctx context.Context, req *service.GetDEKRequest) (*service.GetDEKResponse, error) {
	return &service.GetDEKResponse{}, nil
}

func (m *mockKMSService) ListDEKs(ctx context.Context, req *service.ListDEKsRequest) (*service.ListDEKsResponse, error) {
	return &service.ListDEKsResponse{}, nil
}

func (m *mockKMSService) RotateDEK(ctx context.Context, req *service.RotateDEKRequest) (*service.RotateDEKResponse, error) {
	return &service.RotateDEKResponse{}, nil
}

func (m *mockKMSService) DeleteDEK(ctx context.Context, req *service.DeleteDEKRequest) (*service.DeleteDEKResponse, error) {
	return &service.DeleteDEKResponse{}, nil
}

func (m *mockKMSService) GetDEKStatus(ctx context.Context, req *service.GetDEKStatusRequest) (*service.GetDEKStatusResponse, error) {
	return &service.GetDEKStatusResponse{}, nil
}

func (m *mockKMSService) SetDefaultDEK(ctx context.Context, req *service.SetDefaultDEKRequest) (*service.SetDefaultDEKResponse, error) {
	return &service.SetDefaultDEKResponse{}, nil
}

func (m *mockKMSService) SetDEKSafety(ctx context.Context, req *service.SetDEKSafetyRequest) (*service.SetDEKSafetyResponse, error) {
	return &service.SetDEKSafetyResponse{}, nil
}

func (m *mockKMSService) ClearDEKSafety(ctx context.Context, req *service.ClearDEKSafetyRequest) (*service.ClearDEKSafetyResponse, error) {
	return &service.ClearDEKSafetyResponse{}, nil
}

func (m *mockKMSService) StartReencryption(ctx context.Context, req *service.StartReencryptionRequest) (*service.StartReencryptionResponse, error) {
	return &service.StartReencryptionResponse{OperationID: "job1", Started: true}, nil
}

func (m *mockKMSService) GetReencryptionProgress(ctx context.Context, req *service.GetReencryptionProgressRequest) (*service.GetReencryptionProgressResponse, error) {
	return &service.GetReencryptionProgressResponse{Progress: &service.ReencryptionProgress{PercentComplete: 50.0}}, nil
}

func (m *mockKMSService) SetDefaultKey(ctx context.Context, req *service.SetDefaultKeyRequest) (*service.SetDefaultKeyResponse, error) {
	return &service.SetDefaultKeyResponse{}, nil
}
func (m *mockKMSService) DeleteKeyPolicy(ctx context.Context, req *service.DeleteKeyPolicyRequest) (*service.DeleteKeyPolicyResponse, error) {
	return &service.DeleteKeyPolicyResponse{}, nil
}
func (m *mockKMSService) SetKeyRotationPolicy(ctx context.Context, req *service.SetKeyRotationPolicyRequest) (*service.SetKeyRotationPolicyResponse, error) {
	return &service.SetKeyRotationPolicyResponse{}, nil
}
func (m *mockKMSService) GetKeyRotationPolicy(ctx context.Context, req *service.GetKeyRotationPolicyRequest) (*service.GetKeyRotationPolicyResponse, error) {
	return &service.GetKeyRotationPolicyResponse{}, nil
}
func (m *mockKMSService) DeleteKeyRotationPolicy(ctx context.Context, req *service.DeleteKeyRotationPolicyRequest) (*service.DeleteKeyRotationPolicyResponse, error) {
	return &service.DeleteKeyRotationPolicyResponse{}, nil
}

// =============================================================================
// Tests
// =============================================================================

func TestNew(t *testing.T) {
	t.Run("success with valid service", func(t *testing.T) {
		mock := &mockQuicBaaS{}
		transport, err := New(mock)

		require.NoError(t, err)
		assert.NotNil(t, transport)
		assert.Equal(t, mock, transport.Service())
		assert.True(t, transport.Healthy())
	})

	t.Run("error with nil service", func(t *testing.T) {
		transport, err := New(nil)

		assert.Nil(t, transport)
		assert.Equal(t, ErrNilService, err)
	})
}

func TestTransport_Request_KVOperations(t *testing.T) {
	mock := &mockQuicBaaS{}
	transport, err := New(mock)
	require.NoError(t, err)

	ctx := context.Background()

	t.Run("Put success", func(t *testing.T) {
		req := &service.PutRequest{EntityType: "test", ID: 1, Data: []byte("data")}
		resp, err := transport.Request(ctx, "quicbaas.KV", "Put", req)

		require.NoError(t, err)
		putResp, ok := resp.(*service.PutResponse)
		require.True(t, ok)
		assert.Equal(t, uint64(1), putResp.ID)
	})

	t.Run("Get success", func(t *testing.T) {
		req := &service.GetRequest{EntityType: "test", ID: 1}
		resp, err := transport.Request(ctx, "quicbaas.KV", "Get", req)

		require.NoError(t, err)
		getResp, ok := resp.(*service.GetResponse)
		require.True(t, ok)
		assert.True(t, getResp.Found)
	})

	t.Run("Delete success", func(t *testing.T) {
		req := &service.DeleteRequest{EntityType: "test", ID: 1}
		resp, err := transport.Request(ctx, "quicbaas.KV", "Delete", req)

		require.NoError(t, err)
		deleteResp, ok := resp.(*service.DeleteResponse)
		require.True(t, ok)
		assert.True(t, deleteResp.Deleted)
	})

	t.Run("ListEntityTypes success", func(t *testing.T) {
		req := &service.ListEntityTypesRequest{}
		resp, err := transport.Request(ctx, "quicbaas.KV", "ListEntityTypes", req)

		require.NoError(t, err)
		listResp, ok := resp.(*service.ListEntityTypesResponse)
		require.True(t, ok)
		assert.Contains(t, listResp.EntityTypes, "test")
	})
}

func TestTransport_Request_InvalidRequestType(t *testing.T) {
	mock := &mockQuicBaaS{}
	transport, err := New(mock)
	require.NoError(t, err)

	ctx := context.Background()

	t.Run("Put with wrong type", func(t *testing.T) {
		_, err := transport.Request(ctx, "quicbaas.KV", "Put", "invalid")

		require.Error(t, err)
		var invalidReqErr *InvalidRequestTypeError
		assert.True(t, errors.As(err, &invalidReqErr))
		assert.Equal(t, "quicbaas.KV", invalidReqErr.Service)
		assert.Equal(t, "Put", invalidReqErr.Method)
	})

	t.Run("Get with wrong type", func(t *testing.T) {
		_, err := transport.Request(ctx, "quicbaas.KV", "Get", 123)

		require.Error(t, err)
		var invalidReqErr *InvalidRequestTypeError
		assert.True(t, errors.As(err, &invalidReqErr))
	})

	t.Run("Delete with wrong type", func(t *testing.T) {
		_, err := transport.Request(ctx, "quicbaas.KV", "Delete", "invalid")

		require.Error(t, err)
		var invalidReqErr *InvalidRequestTypeError
		assert.True(t, errors.As(err, &invalidReqErr))
	})

	t.Run("ListEntityTypes with wrong type", func(t *testing.T) {
		_, err := transport.Request(ctx, "quicbaas.KV", "ListEntityTypes", "invalid")

		require.Error(t, err)
		var invalidReqErr *InvalidRequestTypeError
		assert.True(t, errors.As(err, &invalidReqErr))
	})
}

func TestTransport_Request_UnknownMethod(t *testing.T) {
	mock := &mockQuicBaaS{}
	transport, err := New(mock)
	require.NoError(t, err)

	ctx := context.Background()

	_, err = transport.Request(ctx, "quicbaas.Unknown", "Method", nil)

	require.Error(t, err)
	var unknownErr *UnknownMethodError
	assert.True(t, errors.As(err, &unknownErr))
	assert.Equal(t, "quicbaas.Unknown", unknownErr.Service)
	assert.Equal(t, "Method", unknownErr.Method)
}

func TestTransport_Request_ClosedTransport(t *testing.T) {
	mock := &mockQuicBaaS{}
	transport, err := New(mock)
	require.NoError(t, err)

	ctx := context.Background()

	err = transport.Close()
	require.NoError(t, err)

	_, err = transport.Request(ctx, "quicbaas.KV", "Put", &service.PutRequest{})
	assert.Equal(t, ErrTransportClosed, err)
}

func TestTransport_Request_ServiceError(t *testing.T) {
	expectedErr := sdkerrors.NewNotFound("test", "entity not found")
	mock := &mockQuicBaaS{
		getFunc: func(ctx context.Context, req *service.GetRequest) (*service.GetResponse, error) {
			return nil, expectedErr
		},
	}
	transport, err := New(mock)
	require.NoError(t, err)

	ctx := context.Background()
	_, err = transport.Request(ctx, "quicbaas.KV", "Get", &service.GetRequest{ID: 1})

	assert.Equal(t, expectedErr, err)
}

func TestTransport_Request_HealthOperations(t *testing.T) {
	mock := &mockQuicBaaS{}
	transport, err := New(mock)
	require.NoError(t, err)

	ctx := context.Background()

	t.Run("Health check", func(t *testing.T) {
		req := &service.HealthRequest{}
		resp, err := transport.Request(ctx, "quicbaas.Health", "Check", req)

		require.NoError(t, err)
		healthResp, ok := resp.(*service.HealthResponse)
		require.True(t, ok)
		assert.Equal(t, service.HealthStatusHealthy, healthResp.Status)
	})

	t.Run("Liveness check", func(t *testing.T) {
		req := &service.LivenessRequest{}
		resp, err := transport.Request(ctx, "quicbaas.Health", "Liveness", req)

		require.NoError(t, err)
		livenessResp, ok := resp.(*healthclient.HealthResponse)
		require.True(t, ok)
		assert.True(t, livenessResp.Healthy)
		assert.Equal(t, "alive", livenessResp.Status)
	})

	t.Run("Readiness check", func(t *testing.T) {
		req := &service.ReadinessRequest{}
		resp, err := transport.Request(ctx, "quicbaas.Health", "Readiness", req)

		require.NoError(t, err)
		readinessResp, ok := resp.(*healthclient.HealthResponse)
		require.True(t, ok)
		assert.True(t, readinessResp.Healthy)
		assert.Equal(t, "ready", readinessResp.Status)
	})

	t.Run("Health check with wrong type", func(t *testing.T) {
		_, err := transport.Request(ctx, "quicbaas.Health", "Check", "invalid")
		require.Error(t, err)
		var invalidReqErr *InvalidRequestTypeError
		assert.True(t, errors.As(err, &invalidReqErr))
	})

	t.Run("Liveness with non-service request type", func(t *testing.T) {
		// The handler accepts any request type for Liveness since the SDK
		// client sends an unexported type with no fields.
		resp, err := transport.Request(ctx, "quicbaas.Health", "Liveness", "any-type")
		require.NoError(t, err)
		livenessResp, ok := resp.(*healthclient.HealthResponse)
		require.True(t, ok)
		assert.True(t, livenessResp.Healthy)
		assert.Equal(t, "alive", livenessResp.Status)
	})

	t.Run("Readiness with non-service request type", func(t *testing.T) {
		// The handler accepts any request type for Readiness since the SDK
		// client sends an unexported type with no fields.
		resp, err := transport.Request(ctx, "quicbaas.Health", "Readiness", "any-type")
		require.NoError(t, err)
		readinessResp, ok := resp.(*healthclient.HealthResponse)
		require.True(t, ok)
		assert.True(t, readinessResp.Healthy)
		assert.Equal(t, "ready", readinessResp.Status)
	})
}

func TestTransport_Request_BatchOperations(t *testing.T) {
	mock := &mockQuicBaaS{}
	transport, err := New(mock)
	require.NoError(t, err)

	ctx := context.Background()

	t.Run("BatchPut", func(t *testing.T) {
		req := &service.BatchPutRequest{Requests: []*service.PutRequest{{ID: 1}}}
		resp, err := transport.Request(ctx, "quicbaas.Batch", "Put", req)

		require.NoError(t, err)
		_, ok := resp.(*service.BatchPutResponse)
		require.True(t, ok)
	})

	t.Run("BatchGet", func(t *testing.T) {
		req := &service.BatchGetRequest{Requests: []*service.GetRequest{{ID: 1}}}
		resp, err := transport.Request(ctx, "quicbaas.Batch", "Get", req)

		require.NoError(t, err)
		_, ok := resp.(*service.BatchGetResponse)
		require.True(t, ok)
	})

	t.Run("BatchDelete", func(t *testing.T) {
		req := &service.BatchDeleteRequest{Requests: []*service.DeleteRequest{{ID: 1}}}
		resp, err := transport.Request(ctx, "quicbaas.Batch", "Delete", req)

		require.NoError(t, err)
		_, ok := resp.(*service.BatchDeleteResponse)
		require.True(t, ok)
	})

	t.Run("BatchPut with wrong type", func(t *testing.T) {
		_, err := transport.Request(ctx, "quicbaas.Batch", "Put", "invalid")
		require.Error(t, err)
		var invalidReqErr *InvalidRequestTypeError
		assert.True(t, errors.As(err, &invalidReqErr))
	})

	t.Run("BatchGet with wrong type", func(t *testing.T) {
		_, err := transport.Request(ctx, "quicbaas.Batch", "Get", "invalid")
		require.Error(t, err)
		var invalidReqErr *InvalidRequestTypeError
		assert.True(t, errors.As(err, &invalidReqErr))
	})

	t.Run("BatchDelete with wrong type", func(t *testing.T) {
		_, err := transport.Request(ctx, "quicbaas.Batch", "Delete", "invalid")
		require.Error(t, err)
		var invalidReqErr *InvalidRequestTypeError
		assert.True(t, errors.As(err, &invalidReqErr))
	})
}

func TestTransport_RequestStream(t *testing.T) {
	mock := &mockQuicBaaS{}
	transport, err := New(mock)
	require.NoError(t, err)

	ctx := context.Background()

	t.Run("List stream success", func(t *testing.T) {
		req := &service.ListRequest{EntityType: "test"}
		stream, err := transport.RequestStream(ctx, "quicbaas.KV", "List", req)

		require.NoError(t, err)
		require.NotNil(t, stream)

		// Read from stream
		resp, err := stream.Recv()
		require.NoError(t, err)
		listResp, ok := resp.(*service.ListResponse)
		require.True(t, ok)
		assert.Equal(t, uint64(1), listResp.TotalCount)

		err = stream.Close()
		assert.NoError(t, err)
	})

	t.Run("Watch stream success", func(t *testing.T) {
		req := &service.WatchRequest{Key: []byte("test")}
		stream, err := transport.RequestStream(ctx, "quicbaas.KV", "Watch", req)

		require.NoError(t, err)
		require.NotNil(t, stream)

		// Read watch confirmation
		resp, err := stream.Recv()
		require.NoError(t, err)
		watchResp, ok := resp.(*service.WatchResponse)
		require.True(t, ok)
		assert.True(t, watchResp.Created)
		assert.Equal(t, int64(1), watchResp.WatchID)

		err = stream.Close()
		assert.NoError(t, err)
	})

	t.Run("Query stream success", func(t *testing.T) {
		req := &service.QueryRequest{EntityType: "test"}
		stream, err := transport.RequestStream(ctx, "quicbaas.Query", "Query", req)

		require.NoError(t, err)
		require.NotNil(t, stream)

		// Read from stream
		resp, err := stream.Recv()
		require.NoError(t, err)
		queryResp, ok := resp.(*service.QueryResponse)
		require.True(t, ok)
		assert.NotEmpty(t, queryResp.Entities)

		err = stream.Close()
		assert.NoError(t, err)
	})

	t.Run("ScanIndex stream success", func(t *testing.T) {
		req := &service.ScanRequest{EntityType: "test"}
		stream, err := transport.RequestStream(ctx, "quicbaas.Query", "ScanIndex", req)

		require.NoError(t, err)
		require.NotNil(t, stream)

		// Read from stream
		resp, err := stream.Recv()
		require.NoError(t, err)
		scanResp, ok := resp.(*service.ScanResponse)
		require.True(t, ok)
		assert.NotEmpty(t, scanResp.Entities)

		err = stream.Close()
		assert.NoError(t, err)
	})

	t.Run("TimeSeries Query stream success", func(t *testing.T) {
		req := &service.TimeSeriesQueryRequest{EntityType: "test"}
		stream, err := transport.RequestStream(ctx, "quicbaas.TimeSeries", "Query", req)

		require.NoError(t, err)
		require.NotNil(t, stream)

		// Read from stream
		resp, err := stream.Recv()
		require.NoError(t, err)
		tsResp, ok := resp.(*service.TimeSeriesQueryResponse)
		require.True(t, ok)
		assert.NotEmpty(t, tsResp.DataPoints)

		err = stream.Close()
		assert.NoError(t, err)
	})

	t.Run("stream not supported error", func(t *testing.T) {
		_, err := transport.RequestStream(ctx, "quicbaas.Unknown", "Stream", nil)

		require.Error(t, err)
		var streamErr *StreamNotSupportedError
		assert.True(t, errors.As(err, &streamErr))
	})

	t.Run("closed transport error", func(t *testing.T) {
		transport2, err := New(mock)
		require.NoError(t, err)
		err = transport2.Close()
		require.NoError(t, err)

		_, err = transport2.RequestStream(ctx, "quicbaas.KV", "List", &service.ListRequest{})
		assert.Equal(t, ErrTransportClosed, err)
	})

	t.Run("List stream with wrong type", func(t *testing.T) {
		stream, err := transport.RequestStream(ctx, "quicbaas.KV", "List", "invalid")
		require.NoError(t, err)
		defer stream.Close()

		requireStreamInvalidRequestType(t, stream)
	})

	t.Run("Watch stream with wrong type", func(t *testing.T) {
		stream, err := transport.RequestStream(ctx, "quicbaas.KV", "Watch", "invalid")
		require.NoError(t, err)
		defer stream.Close()

		requireStreamInvalidRequestType(t, stream)
	})

	t.Run("Query stream with wrong type", func(t *testing.T) {
		stream, err := transport.RequestStream(ctx, "quicbaas.Query", "Query", "invalid")
		require.NoError(t, err)
		defer stream.Close()

		requireStreamInvalidRequestType(t, stream)
	})

	t.Run("ScanIndex stream with wrong type", func(t *testing.T) {
		stream, err := transport.RequestStream(ctx, "quicbaas.Query", "ScanIndex", "invalid")
		require.NoError(t, err)
		defer stream.Close()

		requireStreamInvalidRequestType(t, stream)
	})

	t.Run("TimeSeries Query stream with wrong type", func(t *testing.T) {
		stream, err := transport.RequestStream(ctx, "quicbaas.TimeSeries", "Query", "invalid")
		require.NoError(t, err)
		defer stream.Close()

		requireStreamInvalidRequestType(t, stream)
	})
}

func TestTransport_Conn(t *testing.T) {
	mock := &mockQuicBaaS{}
	transport, err := New(mock)
	require.NoError(t, err)

	conn := transport.Conn()
	assert.Equal(t, mock, conn)
}

func TestTransport_Close(t *testing.T) {
	mock := &mockQuicBaaS{}
	transport, err := New(mock)
	require.NoError(t, err)

	assert.True(t, transport.Healthy())

	err = transport.Close()
	require.NoError(t, err)

	assert.False(t, transport.Healthy())

	// Multiple closes should be safe
	err = transport.Close()
	assert.NoError(t, err)
}

func TestTransport_Healthy(t *testing.T) {
	t.Run("healthy when open", func(t *testing.T) {
		mock := &mockQuicBaaS{}
		transport, err := New(mock)
		require.NoError(t, err)

		assert.True(t, transport.Healthy())
	})

	t.Run("unhealthy when closed", func(t *testing.T) {
		mock := &mockQuicBaaS{}
		transport, err := New(mock)
		require.NoError(t, err)

		err = transport.Close()
		require.NoError(t, err)

		assert.False(t, transport.Healthy())
	})
}

func TestTransport_KMSNotConfigured(t *testing.T) {
	mock := &mockQuicBaaS{kmsService: nil}
	transport, err := New(mock)
	require.NoError(t, err)

	ctx := context.Background()

	req := &service.GenerateKeyRequest{}
	_, err = transport.Request(ctx, "quicbaas.KMS", "GenerateKey", req)

	require.Error(t, err)
	var svcErr *ServiceNotConfiguredError
	assert.True(t, errors.As(err, &svcErr))
	assert.Equal(t, "KMS", svcErr.Service)
}

func TestTransport_AllKMSOperations(t *testing.T) {
	kmsMock := &mockKMSService{}
	mock := &mockQuicBaaS{kmsService: kmsMock}
	transport, err := New(mock)
	require.NoError(t, err)

	ctx := context.Background()

	t.Run("GenerateKey", func(t *testing.T) {
		req := &service.GenerateKeyRequest{}
		resp, err := transport.Request(ctx, "quicbaas.KMS", "GenerateKey", req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("GenerateKey with wrong type", func(t *testing.T) {
		_, err := transport.Request(ctx, "quicbaas.KMS", "GenerateKey", "invalid")
		require.Error(t, err)
		var invalidReqErr *InvalidRequestTypeError
		assert.True(t, errors.As(err, &invalidReqErr))
	})

	t.Run("ImportKey", func(t *testing.T) {
		req := &service.ImportKeyRequest{}
		resp, err := transport.Request(ctx, "quicbaas.KMS", "ImportKey", req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("ImportKey with wrong type", func(t *testing.T) {
		_, err := transport.Request(ctx, "quicbaas.KMS", "ImportKey", "invalid")
		require.Error(t, err)
		var invalidReqErr *InvalidRequestTypeError
		assert.True(t, errors.As(err, &invalidReqErr))
	})

	t.Run("GetKey", func(t *testing.T) {
		req := &service.GetKeyRequest{KeyID: "key1"}
		resp, err := transport.Request(ctx, "quicbaas.KMS", "GetKey", req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("GetKey with wrong type", func(t *testing.T) {
		_, err := transport.Request(ctx, "quicbaas.KMS", "GetKey", "invalid")
		require.Error(t, err)
		var invalidReqErr *InvalidRequestTypeError
		assert.True(t, errors.As(err, &invalidReqErr))
	})

	t.Run("ListKeys", func(t *testing.T) {
		req := &service.ListKeysRequest{}
		resp, err := transport.Request(ctx, "quicbaas.KMS", "ListKeys", req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("ListKeys with wrong type", func(t *testing.T) {
		_, err := transport.Request(ctx, "quicbaas.KMS", "ListKeys", "invalid")
		require.Error(t, err)
		var invalidReqErr *InvalidRequestTypeError
		assert.True(t, errors.As(err, &invalidReqErr))
	})

	t.Run("RotateKey", func(t *testing.T) {
		req := &service.RotateKeyRequest{KeyID: "key1"}
		resp, err := transport.Request(ctx, "quicbaas.KMS", "RotateKey", req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("RotateKey with wrong type", func(t *testing.T) {
		_, err := transport.Request(ctx, "quicbaas.KMS", "RotateKey", "invalid")
		require.Error(t, err)
		var invalidReqErr *InvalidRequestTypeError
		assert.True(t, errors.As(err, &invalidReqErr))
	})

	t.Run("RevokeKey", func(t *testing.T) {
		req := &service.RevokeKeyRequest{KeyID: "key1"}
		resp, err := transport.Request(ctx, "quicbaas.KMS", "RevokeKey", req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("RevokeKey with wrong type", func(t *testing.T) {
		_, err := transport.Request(ctx, "quicbaas.KMS", "RevokeKey", "invalid")
		require.Error(t, err)
		var invalidReqErr *InvalidRequestTypeError
		assert.True(t, errors.As(err, &invalidReqErr))
	})

	t.Run("DeleteKey", func(t *testing.T) {
		req := &service.DeleteKeyRequest{KeyID: "key1"}
		resp, err := transport.Request(ctx, "quicbaas.KMS", "DeleteKey", req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("DeleteKey with wrong type", func(t *testing.T) {
		_, err := transport.Request(ctx, "quicbaas.KMS", "DeleteKey", "invalid")
		require.Error(t, err)
		var invalidReqErr *InvalidRequestTypeError
		assert.True(t, errors.As(err, &invalidReqErr))
	})

	t.Run("GetKeyMetadata", func(t *testing.T) {
		req := &service.GetKeyMetadataRequest{KeyID: "key1"}
		resp, err := transport.Request(ctx, "quicbaas.KMS", "GetKeyMetadata", req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("GetKeyMetadata with wrong type", func(t *testing.T) {
		_, err := transport.Request(ctx, "quicbaas.KMS", "GetKeyMetadata", "invalid")
		require.Error(t, err)
		var invalidReqErr *InvalidRequestTypeError
		assert.True(t, errors.As(err, &invalidReqErr))
	})

	t.Run("UpdateKeyMetadata", func(t *testing.T) {
		req := &service.UpdateKeyMetadataRequest{KeyID: "key1"}
		resp, err := transport.Request(ctx, "quicbaas.KMS", "UpdateKeyMetadata", req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("UpdateKeyMetadata with wrong type", func(t *testing.T) {
		_, err := transport.Request(ctx, "quicbaas.KMS", "UpdateKeyMetadata", "invalid")
		require.Error(t, err)
		var invalidReqErr *InvalidRequestTypeError
		assert.True(t, errors.As(err, &invalidReqErr))
	})

	t.Run("UpdateKeyStatus", func(t *testing.T) {
		req := &service.UpdateKeyStatusRequest{KeyID: "key1"}
		resp, err := transport.Request(ctx, "quicbaas.KMS", "UpdateKeyStatus", req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("UpdateKeyStatus with wrong type", func(t *testing.T) {
		_, err := transport.Request(ctx, "quicbaas.KMS", "UpdateKeyStatus", "invalid")
		require.Error(t, err)
		var invalidReqErr *InvalidRequestTypeError
		assert.True(t, errors.As(err, &invalidReqErr))
	})

	t.Run("ListKeyVersions", func(t *testing.T) {
		req := &service.ListKeyVersionsRequest{KeyID: "key1"}
		resp, err := transport.Request(ctx, "quicbaas.KMS", "ListKeyVersions", req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("ListKeyVersions with wrong type", func(t *testing.T) {
		_, err := transport.Request(ctx, "quicbaas.KMS", "ListKeyVersions", "invalid")
		require.Error(t, err)
		var invalidReqErr *InvalidRequestTypeError
		assert.True(t, errors.As(err, &invalidReqErr))
	})

	t.Run("SetKeyPolicy", func(t *testing.T) {
		req := &service.SetKeyPolicyRequest{KeyID: "key1"}
		resp, err := transport.Request(ctx, "quicbaas.KMS", "SetKeyPolicy", req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("SetKeyPolicy with wrong type", func(t *testing.T) {
		_, err := transport.Request(ctx, "quicbaas.KMS", "SetKeyPolicy", "invalid")
		require.Error(t, err)
		var invalidReqErr *InvalidRequestTypeError
		assert.True(t, errors.As(err, &invalidReqErr))
	})

	t.Run("GetKeyPolicy", func(t *testing.T) {
		req := &service.GetKeyPolicyRequest{KeyID: "key1"}
		resp, err := transport.Request(ctx, "quicbaas.KMS", "GetKeyPolicy", req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("GetKeyPolicy with wrong type", func(t *testing.T) {
		_, err := transport.Request(ctx, "quicbaas.KMS", "GetKeyPolicy", "invalid")
		require.Error(t, err)
		var invalidReqErr *InvalidRequestTypeError
		assert.True(t, errors.As(err, &invalidReqErr))
	})

	t.Run("GetSupportedAlgorithms", func(t *testing.T) {
		// Note: GetSupportedAlgorithms does not take a request argument in the handler
		resp, err := transport.Request(ctx, "quicbaas.KMS", "GetSupportedAlgorithms", nil)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("BackendStatus", func(t *testing.T) {
		resp, err := transport.Request(ctx, "quicbaas.KMS", "BackendStatus", nil)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("BackendTest", func(t *testing.T) {
		resp, err := transport.Request(ctx, "quicbaas.KMS", "BackendTest", nil)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("ExportKey", func(t *testing.T) {
		req := &service.ExportKeyRequest{KeyID: "key1"}
		resp, err := transport.Request(ctx, "quicbaas.KMS", "ExportKey", req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("ExportKey with wrong type", func(t *testing.T) {
		_, err := transport.Request(ctx, "quicbaas.KMS", "ExportKey", "invalid")
		require.Error(t, err)
		var invalidReqErr *InvalidRequestTypeError
		assert.True(t, errors.As(err, &invalidReqErr))
	})

	t.Run("UnwrapKey", func(t *testing.T) {
		req := &service.UnwrapKeyRequest{KeyID: "key1"}
		resp, err := transport.Request(ctx, "quicbaas.KMS", "UnwrapKey", req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("UnwrapKey with wrong type", func(t *testing.T) {
		_, err := transport.Request(ctx, "quicbaas.KMS", "UnwrapKey", "invalid")
		require.Error(t, err)
		var invalidReqErr *InvalidRequestTypeError
		assert.True(t, errors.As(err, &invalidReqErr))
	})

	t.Run("SetKeySafety", func(t *testing.T) {
		req := &service.SetKeySafetyRequest{KeyID: "key1"}
		resp, err := transport.Request(ctx, "quicbaas.KMS", "SetKeySafety", req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("SetKeySafety with wrong type", func(t *testing.T) {
		_, err := transport.Request(ctx, "quicbaas.KMS", "SetKeySafety", "invalid")
		require.Error(t, err)
		var invalidReqErr *InvalidRequestTypeError
		assert.True(t, errors.As(err, &invalidReqErr))
	})

	t.Run("ClearKeySafety", func(t *testing.T) {
		req := &service.ClearKeySafetyRequest{KeyID: "key1"}
		resp, err := transport.Request(ctx, "quicbaas.KMS", "ClearKeySafety", req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("ClearKeySafety with wrong type", func(t *testing.T) {
		_, err := transport.Request(ctx, "quicbaas.KMS", "ClearKeySafety", "invalid")
		require.Error(t, err)
		var invalidReqErr *InvalidRequestTypeError
		assert.True(t, errors.As(err, &invalidReqErr))
	})

	t.Run("SafetyCheck", func(t *testing.T) {
		req := &service.SafetyCheckRequest{ThresholdPercent: 80.0}
		resp, err := transport.Request(ctx, "quicbaas.KMS", "SafetyCheck", req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("SafetyCheck with wrong type", func(t *testing.T) {
		_, err := transport.Request(ctx, "quicbaas.KMS", "SafetyCheck", "invalid")
		require.Error(t, err)
		var invalidReqErr *InvalidRequestTypeError
		assert.True(t, errors.As(err, &invalidReqErr))
	})

	t.Run("SafetyReport", func(t *testing.T) {
		req := &service.SafetyReportRequest{}
		resp, err := transport.Request(ctx, "quicbaas.KMS", "SafetyReport", req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("SafetyReport with wrong type", func(t *testing.T) {
		_, err := transport.Request(ctx, "quicbaas.KMS", "SafetyReport", "invalid")
		require.Error(t, err)
		var invalidReqErr *InvalidRequestTypeError
		assert.True(t, errors.As(err, &invalidReqErr))
	})

	t.Run("CreateDEK", func(t *testing.T) {
		req := &service.CreateDEKRequest{Path: "dek1"}
		resp, err := transport.Request(ctx, "quicbaas.KMS", "CreateDEK", req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("CreateDEK with wrong type", func(t *testing.T) {
		_, err := transport.Request(ctx, "quicbaas.KMS", "CreateDEK", "invalid")
		require.Error(t, err)
		var invalidReqErr *InvalidRequestTypeError
		assert.True(t, errors.As(err, &invalidReqErr))
	})

	t.Run("GetDEK", func(t *testing.T) {
		req := &service.GetDEKRequest{Path: "dek1"}
		resp, err := transport.Request(ctx, "quicbaas.KMS", "GetDEK", req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("GetDEK with wrong type", func(t *testing.T) {
		_, err := transport.Request(ctx, "quicbaas.KMS", "GetDEK", "invalid")
		require.Error(t, err)
		var invalidReqErr *InvalidRequestTypeError
		assert.True(t, errors.As(err, &invalidReqErr))
	})

	t.Run("ListDEKs", func(t *testing.T) {
		req := &service.ListDEKsRequest{}
		resp, err := transport.Request(ctx, "quicbaas.KMS", "ListDEKs", req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("ListDEKs with wrong type", func(t *testing.T) {
		_, err := transport.Request(ctx, "quicbaas.KMS", "ListDEKs", "invalid")
		require.Error(t, err)
		var invalidReqErr *InvalidRequestTypeError
		assert.True(t, errors.As(err, &invalidReqErr))
	})

	t.Run("RotateDEK", func(t *testing.T) {
		req := &service.RotateDEKRequest{Path: "dek1"}
		resp, err := transport.Request(ctx, "quicbaas.KMS", "RotateDEK", req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("RotateDEK with wrong type", func(t *testing.T) {
		_, err := transport.Request(ctx, "quicbaas.KMS", "RotateDEK", "invalid")
		require.Error(t, err)
		var invalidReqErr *InvalidRequestTypeError
		assert.True(t, errors.As(err, &invalidReqErr))
	})

	t.Run("DeleteDEK", func(t *testing.T) {
		req := &service.DeleteDEKRequest{Path: "dek1"}
		resp, err := transport.Request(ctx, "quicbaas.KMS", "DeleteDEK", req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("DeleteDEK with wrong type", func(t *testing.T) {
		_, err := transport.Request(ctx, "quicbaas.KMS", "DeleteDEK", "invalid")
		require.Error(t, err)
		var invalidReqErr *InvalidRequestTypeError
		assert.True(t, errors.As(err, &invalidReqErr))
	})

	t.Run("GetDEKStatus", func(t *testing.T) {
		req := &service.GetDEKStatusRequest{Path: "dek1"}
		resp, err := transport.Request(ctx, "quicbaas.KMS", "GetDEKStatus", req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("GetDEKStatus with wrong type", func(t *testing.T) {
		_, err := transport.Request(ctx, "quicbaas.KMS", "GetDEKStatus", "invalid")
		require.Error(t, err)
		var invalidReqErr *InvalidRequestTypeError
		assert.True(t, errors.As(err, &invalidReqErr))
	})

	t.Run("SetDefaultDEK", func(t *testing.T) {
		req := &service.SetDefaultDEKRequest{Path: "dek1"}
		resp, err := transport.Request(ctx, "quicbaas.KMS", "SetDefaultDEK", req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("SetDefaultDEK with wrong type", func(t *testing.T) {
		_, err := transport.Request(ctx, "quicbaas.KMS", "SetDefaultDEK", "invalid")
		require.Error(t, err)
		var invalidReqErr *InvalidRequestTypeError
		assert.True(t, errors.As(err, &invalidReqErr))
	})

	t.Run("SetDEKSafety", func(t *testing.T) {
		req := &service.SetDEKSafetyRequest{Path: "dek1"}
		resp, err := transport.Request(ctx, "quicbaas.KMS", "SetDEKSafety", req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("SetDEKSafety with wrong type", func(t *testing.T) {
		_, err := transport.Request(ctx, "quicbaas.KMS", "SetDEKSafety", "invalid")
		require.Error(t, err)
		var invalidReqErr *InvalidRequestTypeError
		assert.True(t, errors.As(err, &invalidReqErr))
	})

	t.Run("ClearDEKSafety", func(t *testing.T) {
		req := &service.ClearDEKSafetyRequest{Path: "dek1"}
		resp, err := transport.Request(ctx, "quicbaas.KMS", "ClearDEKSafety", req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("ClearDEKSafety with wrong type", func(t *testing.T) {
		_, err := transport.Request(ctx, "quicbaas.KMS", "ClearDEKSafety", "invalid")
		require.Error(t, err)
		var invalidReqErr *InvalidRequestTypeError
		assert.True(t, errors.As(err, &invalidReqErr))
	})

	t.Run("StartReencryption", func(t *testing.T) {
		req := &service.StartReencryptionRequest{Path: "dek1", FromVersion: 1, ToVersion: 2}
		resp, err := transport.Request(ctx, "quicbaas.KMS", "StartReencryption", req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("StartReencryption with wrong type", func(t *testing.T) {
		_, err := transport.Request(ctx, "quicbaas.KMS", "StartReencryption", "invalid")
		require.Error(t, err)
		var invalidReqErr *InvalidRequestTypeError
		assert.True(t, errors.As(err, &invalidReqErr))
	})

	t.Run("GetReencryptionProgress", func(t *testing.T) {
		req := &service.GetReencryptionProgressRequest{OperationID: "job1"}
		resp, err := transport.Request(ctx, "quicbaas.KMS", "GetReencryptionProgress", req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("GetReencryptionProgress with wrong type", func(t *testing.T) {
		_, err := transport.Request(ctx, "quicbaas.KMS", "GetReencryptionProgress", "invalid")
		require.Error(t, err)
		var invalidReqErr *InvalidRequestTypeError
		assert.True(t, errors.As(err, &invalidReqErr))
	})
}

func TestFactory(t *testing.T) {
	mock := &mockQuicBaaS{}
	factory := NewFactory(mock)

	transport, err := factory.Create(nil)

	require.NoError(t, err)
	assert.NotNil(t, transport)
	assert.True(t, transport.Healthy())
}

func TestStream(t *testing.T) {
	ctx := context.Background()

	t.Run("send and receive", func(t *testing.T) {
		s := newStream(ctx, 10)
		defer s.Close()

		// Send on internal channel to simulate service
		go func() {
			assert.NoError(t, s.deliver("test message"))
		}()

		msg, err := s.Recv()
		require.NoError(t, err)
		assert.Equal(t, "test message", msg)
	})

	t.Run("send error on closed stream", func(t *testing.T) {
		s := newStream(ctx, 10)
		err := s.Close()
		require.NoError(t, err)

		err = s.Send("test")
		assert.Equal(t, ErrSendOnClosedStream, err)
	})

	t.Run("recv error on closed stream", func(t *testing.T) {
		s := newStream(ctx, 10)
		err := s.Close()
		require.NoError(t, err)

		_, err = s.Recv()
		assert.Equal(t, ErrRecvOnClosedStream, err)
	})

	t.Run("CloseSend", func(t *testing.T) {
		s := newStream(ctx, 10)
		defer s.Close()

		err := s.CloseSend()
		require.NoError(t, err)

		// Second close should be no-op
		err = s.CloseSend()
		assert.NoError(t, err)

		// Send after CloseSend should fail
		err = s.Send("test")
		assert.Equal(t, ErrSendOnClosedStream, err)
	})

	t.Run("context cancellation on send", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		s := newStream(ctx, 0) // unbuffered channel
		defer s.Close()

		cancel()

		err := s.Send("test")
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("context cancellation on recv", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		s := newStream(ctx, 10)
		defer s.Close()

		cancel()

		_, err := s.Recv()
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("sendChan returns send channel", func(t *testing.T) {
		s := newStream(ctx, 10)
		defer s.Close()

		ch := s.sendChan()
		assert.NotNil(t, ch)
	})
}

func TestErrorTypes(t *testing.T) {
	t.Run("UnknownMethodError", func(t *testing.T) {
		err := NewUnknownMethodError("service", "method")
		assert.Contains(t, err.Error(), "service/method")
		assert.Contains(t, err.Error(), "unknown method")
	})

	t.Run("InvalidRequestTypeError", func(t *testing.T) {
		err := NewInvalidRequestTypeError("svc", "method", "*Expected", "string")
		assert.Contains(t, err.Error(), "svc/method")
		assert.Contains(t, err.Error(), "*Expected")
		assert.Contains(t, err.Error(), "string")
	})

	t.Run("ServiceNotConfiguredError", func(t *testing.T) {
		err := NewServiceNotConfiguredError("KMS")
		assert.Contains(t, err.Error(), "KMS")
		assert.Contains(t, err.Error(), "not configured")
	})

	t.Run("StreamNotSupportedError", func(t *testing.T) {
		err := NewStreamNotSupportedError("service", "method")
		assert.Contains(t, err.Error(), "service/method")
		assert.Contains(t, err.Error(), "streaming not supported")
	})
}

func TestTransport_AllClusterOperations(t *testing.T) {
	mock := &mockQuicBaaS{}
	transport, err := New(mock)
	require.NoError(t, err)

	ctx := context.Background()

	t.Run("ClusterStatus", func(t *testing.T) {
		req := &service.ClusterStatusRequest{}
		resp, err := transport.Request(ctx, "quicbaas.Cluster", "Status", req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("ClusterStatus with wrong type", func(t *testing.T) {
		_, err := transport.Request(ctx, "quicbaas.Cluster", "Status", "invalid")
		require.Error(t, err)
		var invalidReqErr *InvalidRequestTypeError
		assert.True(t, errors.As(err, &invalidReqErr))
	})

	t.Run("GetLeader", func(t *testing.T) {
		req := &service.GetLeaderRequest{ShardID: 1}
		resp, err := transport.Request(ctx, "quicbaas.Cluster", "GetLeader", req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("GetLeader with wrong type", func(t *testing.T) {
		_, err := transport.Request(ctx, "quicbaas.Cluster", "GetLeader", "invalid")
		require.Error(t, err)
		var invalidReqErr *InvalidRequestTypeError
		assert.True(t, errors.As(err, &invalidReqErr))
	})

	t.Run("AddNode", func(t *testing.T) {
		req := &service.AddNodeRequest{ShardID: 1, NodeID: 2}
		resp, err := transport.Request(ctx, "quicbaas.Cluster", "AddNode", req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("AddNode with wrong type", func(t *testing.T) {
		_, err := transport.Request(ctx, "quicbaas.Cluster", "AddNode", "invalid")
		require.Error(t, err)
		var invalidReqErr *InvalidRequestTypeError
		assert.True(t, errors.As(err, &invalidReqErr))
	})

	t.Run("RemoveNode", func(t *testing.T) {
		req := &service.RemoveNodeRequest{ShardID: 1, NodeID: 2}
		resp, err := transport.Request(ctx, "quicbaas.Cluster", "RemoveNode", req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("RemoveNode with wrong type", func(t *testing.T) {
		_, err := transport.Request(ctx, "quicbaas.Cluster", "RemoveNode", "invalid")
		require.Error(t, err)
		var invalidReqErr *InvalidRequestTypeError
		assert.True(t, errors.As(err, &invalidReqErr))
	})

	t.Run("CreateShard", func(t *testing.T) {
		req := &service.CreateShardRequest{ShardID: 1}
		resp, err := transport.Request(ctx, "quicbaas.Cluster", "CreateShard", req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("CreateShard with wrong type", func(t *testing.T) {
		_, err := transport.Request(ctx, "quicbaas.Cluster", "CreateShard", "invalid")
		require.Error(t, err)
		var invalidReqErr *InvalidRequestTypeError
		assert.True(t, errors.As(err, &invalidReqErr))
	})

	t.Run("Rebalance", func(t *testing.T) {
		req := &service.RebalanceClusterRequest{}
		resp, err := transport.Request(ctx, "quicbaas.Cluster", "Rebalance", req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("Rebalance with wrong type", func(t *testing.T) {
		_, err := transport.Request(ctx, "quicbaas.Cluster", "Rebalance", "invalid")
		require.Error(t, err)
		var invalidReqErr *InvalidRequestTypeError
		assert.True(t, errors.As(err, &invalidReqErr))
	})

	t.Run("GetRebalanceStatus", func(t *testing.T) {
		req := &service.GetRebalanceProgressRequest{}
		resp, err := transport.Request(ctx, "quicbaas.Cluster", "GetRebalanceStatus", req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("GetRebalanceStatus with wrong type", func(t *testing.T) {
		_, err := transport.Request(ctx, "quicbaas.Cluster", "GetRebalanceStatus", "invalid")
		require.Error(t, err)
		var invalidReqErr *InvalidRequestTypeError
		assert.True(t, errors.As(err, &invalidReqErr))
	})
}

func TestTransport_AllBackupOperations(t *testing.T) {
	mock := &mockQuicBaaS{}
	transport, err := New(mock)
	require.NoError(t, err)

	ctx := context.Background()

	t.Run("CreateBackup", func(t *testing.T) {
		req := &service.CreateBackupRequest{}
		resp, err := transport.Request(ctx, "quicbaas.Backup", "Create", req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("CreateBackup with wrong type", func(t *testing.T) {
		_, err := transport.Request(ctx, "quicbaas.Backup", "Create", "invalid")
		require.Error(t, err)
		var invalidReqErr *InvalidRequestTypeError
		assert.True(t, errors.As(err, &invalidReqErr))
	})

	t.Run("ListBackups", func(t *testing.T) {
		req := &service.ListBackupsRequest{}
		resp, err := transport.Request(ctx, "quicbaas.Backup", "List", req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("ListBackups with wrong type", func(t *testing.T) {
		_, err := transport.Request(ctx, "quicbaas.Backup", "List", "invalid")
		require.Error(t, err)
		var invalidReqErr *InvalidRequestTypeError
		assert.True(t, errors.As(err, &invalidReqErr))
	})

	t.Run("RestoreBackup", func(t *testing.T) {
		req := &service.RestoreBackupRequest{}
		resp, err := transport.Request(ctx, "quicbaas.Backup", "Restore", req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("RestoreBackup with wrong type", func(t *testing.T) {
		_, err := transport.Request(ctx, "quicbaas.Backup", "Restore", "invalid")
		require.Error(t, err)
		var invalidReqErr *InvalidRequestTypeError
		assert.True(t, errors.As(err, &invalidReqErr))
	})

	t.Run("DeleteBackup", func(t *testing.T) {
		req := &service.DeleteBackupRequest{}
		resp, err := transport.Request(ctx, "quicbaas.Backup", "Delete", req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("DeleteBackup with wrong type", func(t *testing.T) {
		_, err := transport.Request(ctx, "quicbaas.Backup", "Delete", "invalid")
		require.Error(t, err)
		var invalidReqErr *InvalidRequestTypeError
		assert.True(t, errors.As(err, &invalidReqErr))
	})

	t.Run("VerifyBackup", func(t *testing.T) {
		req := &service.VerifyBackupRequest{}
		resp, err := transport.Request(ctx, "quicbaas.Backup", "Verify", req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("VerifyBackup with wrong type", func(t *testing.T) {
		_, err := transport.Request(ctx, "quicbaas.Backup", "Verify", "invalid")
		require.Error(t, err)
		var invalidReqErr *InvalidRequestTypeError
		assert.True(t, errors.As(err, &invalidReqErr))
	})
}

func TestTransport_AllAuthOperations(t *testing.T) {
	mock := &mockQuicBaaS{}
	transport, err := New(mock)
	require.NoError(t, err)

	ctx := context.Background()

	t.Run("Login", func(t *testing.T) {
		req := &service.LoginRequest{}
		resp, err := transport.Request(ctx, "quicbaas.Auth", "Login", req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("Login with wrong type", func(t *testing.T) {
		_, err := transport.Request(ctx, "quicbaas.Auth", "Login", "invalid")
		require.Error(t, err)
		var invalidReqErr *InvalidRequestTypeError
		assert.True(t, errors.As(err, &invalidReqErr))
	})

	t.Run("Logout", func(t *testing.T) {
		req := &service.LogoutRequest{}
		resp, err := transport.Request(ctx, "quicbaas.Auth", "Logout", req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("Logout with wrong type", func(t *testing.T) {
		_, err := transport.Request(ctx, "quicbaas.Auth", "Logout", "invalid")
		require.Error(t, err)
		var invalidReqErr *InvalidRequestTypeError
		assert.True(t, errors.As(err, &invalidReqErr))
	})

	t.Run("RefreshToken", func(t *testing.T) {
		req := &service.RefreshTokenRequest{}
		resp, err := transport.Request(ctx, "quicbaas.Auth", "RefreshToken", req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("RefreshToken with wrong type", func(t *testing.T) {
		_, err := transport.Request(ctx, "quicbaas.Auth", "RefreshToken", "invalid")
		require.Error(t, err)
		var invalidReqErr *InvalidRequestTypeError
		assert.True(t, errors.As(err, &invalidReqErr))
	})

	t.Run("ValidateToken", func(t *testing.T) {
		req := &service.ValidateTokenRequest{}
		resp, err := transport.Request(ctx, "quicbaas.Auth", "ValidateToken", req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("ValidateToken with wrong type", func(t *testing.T) {
		_, err := transport.Request(ctx, "quicbaas.Auth", "ValidateToken", "invalid")
		require.Error(t, err)
		var invalidReqErr *InvalidRequestTypeError
		assert.True(t, errors.As(err, &invalidReqErr))
	})
}

func TestTransport_AllCacheOperations(t *testing.T) {
	mock := &mockQuicBaaS{}
	transport, err := New(mock)
	require.NoError(t, err)

	ctx := context.Background()

	t.Run("GetStats", func(t *testing.T) {
		req := &service.GetCacheStatsRequest{}
		resp, err := transport.Request(ctx, "quicbaas.Cache", "GetCacheStats", req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("GetStats with wrong type", func(t *testing.T) {
		_, err := transport.Request(ctx, "quicbaas.Cache", "GetCacheStats", "invalid")
		require.Error(t, err)
		var invalidReqErr *InvalidRequestTypeError
		assert.True(t, errors.As(err, &invalidReqErr))
	})

	t.Run("Clear", func(t *testing.T) {
		req := &service.ClearCacheRequest{}
		resp, err := transport.Request(ctx, "quicbaas.Cache", "ClearCache", req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("Clear with wrong type", func(t *testing.T) {
		_, err := transport.Request(ctx, "quicbaas.Cache", "ClearCache", "invalid")
		require.Error(t, err)
		var invalidReqErr *InvalidRequestTypeError
		assert.True(t, errors.As(err, &invalidReqErr))
	})

	t.Run("GetConfig", func(t *testing.T) {
		req := &service.GetCacheConfigRequest{}
		resp, err := transport.Request(ctx, "quicbaas.Cache", "GetCacheConfig", req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("GetConfig with wrong type", func(t *testing.T) {
		_, err := transport.Request(ctx, "quicbaas.Cache", "GetCacheConfig", "invalid")
		require.Error(t, err)
		var invalidReqErr *InvalidRequestTypeError
		assert.True(t, errors.As(err, &invalidReqErr))
	})

	t.Run("UpdateConfig", func(t *testing.T) {
		req := &service.UpdateCacheConfigRequest{}
		resp, err := transport.Request(ctx, "quicbaas.Cache", "UpdateCacheConfig", req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("UpdateConfig with wrong type", func(t *testing.T) {
		_, err := transport.Request(ctx, "quicbaas.Cache", "UpdateCacheConfig", "invalid")
		require.Error(t, err)
		var invalidReqErr *InvalidRequestTypeError
		assert.True(t, errors.As(err, &invalidReqErr))
	})
}

func TestTransport_AllTimeSeriesOperations(t *testing.T) {
	mock := &mockQuicBaaS{}
	transport, err := New(mock)
	require.NoError(t, err)

	ctx := context.Background()

	t.Run("Aggregate", func(t *testing.T) {
		req := &service.AggregateRequest{}
		resp, err := transport.Request(ctx, "quicbaas.TimeSeries", "Aggregate", req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("Aggregate with wrong type", func(t *testing.T) {
		_, err := transport.Request(ctx, "quicbaas.TimeSeries", "Aggregate", "invalid")
		require.Error(t, err)
		var invalidReqErr *InvalidRequestTypeError
		assert.True(t, errors.As(err, &invalidReqErr))
	})

	t.Run("WritePoint", func(t *testing.T) {
		req := &service.WritePointRequest{}
		resp, err := transport.Request(ctx, "quicbaas.TimeSeries", "WritePoint", req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("WritePoint with wrong type", func(t *testing.T) {
		_, err := transport.Request(ctx, "quicbaas.TimeSeries", "WritePoint", "invalid")
		require.Error(t, err)
		var invalidReqErr *InvalidRequestTypeError
		assert.True(t, errors.As(err, &invalidReqErr))
	})

	t.Run("WriteBatch", func(t *testing.T) {
		req := &service.WriteBatchRequest{}
		resp, err := transport.Request(ctx, "quicbaas.TimeSeries", "WriteBatch", req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("WriteBatch with wrong type", func(t *testing.T) {
		_, err := transport.Request(ctx, "quicbaas.TimeSeries", "WriteBatch", "invalid")
		require.Error(t, err)
		var invalidReqErr *InvalidRequestTypeError
		assert.True(t, errors.As(err, &invalidReqErr))
	})

	t.Run("Query", func(t *testing.T) {
		req := &service.TSQueryRequest{}
		resp, err := transport.Request(ctx, "quicbaas.TimeSeries", "Query", req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("Query with wrong type", func(t *testing.T) {
		_, err := transport.Request(ctx, "quicbaas.TimeSeries", "Query", "invalid")
		require.Error(t, err)
		var invalidReqErr *InvalidRequestTypeError
		assert.True(t, errors.As(err, &invalidReqErr))
	})

	t.Run("DeleteSeries", func(t *testing.T) {
		req := &service.TSDeleteRequest{}
		resp, err := transport.Request(ctx, "quicbaas.TimeSeries", "DeleteSeries", req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("DeleteSeries with wrong type", func(t *testing.T) {
		_, err := transport.Request(ctx, "quicbaas.TimeSeries", "DeleteSeries", "invalid")
		require.Error(t, err)
		var invalidReqErr *InvalidRequestTypeError
		assert.True(t, errors.As(err, &invalidReqErr))
	})

	t.Run("GetRetentionPolicy", func(t *testing.T) {
		req := &service.TSGetRetentionPolicyRequest{}
		resp, err := transport.Request(ctx, "quicbaas.TimeSeries", "GetRetentionPolicy", req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("GetRetentionPolicy with wrong type", func(t *testing.T) {
		_, err := transport.Request(ctx, "quicbaas.TimeSeries", "GetRetentionPolicy", "invalid")
		require.Error(t, err)
		var invalidReqErr *InvalidRequestTypeError
		assert.True(t, errors.As(err, &invalidReqErr))
	})

	t.Run("SetRetentionPolicy", func(t *testing.T) {
		req := &service.TSSetRetentionPolicyRequest{}
		resp, err := transport.Request(ctx, "quicbaas.TimeSeries", "SetRetentionPolicy", req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("SetRetentionPolicy with wrong type", func(t *testing.T) {
		_, err := transport.Request(ctx, "quicbaas.TimeSeries", "SetRetentionPolicy", "invalid")
		require.Error(t, err)
		var invalidReqErr *InvalidRequestTypeError
		assert.True(t, errors.As(err, &invalidReqErr))
	})

	t.Run("ListMetrics", func(t *testing.T) {
		req := &service.TSListMetricsRequest{}
		resp, err := transport.Request(ctx, "quicbaas.TimeSeries", "ListMetrics", req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("ListMetrics with wrong type", func(t *testing.T) {
		_, err := transport.Request(ctx, "quicbaas.TimeSeries", "ListMetrics", "invalid")
		require.Error(t, err)
		var invalidReqErr *InvalidRequestTypeError
		assert.True(t, errors.As(err, &invalidReqErr))
	})
}

func TestTransport_AllOperationsHandlers(t *testing.T) {
	mock := &mockQuicBaaS{}
	transport, err := New(mock)
	require.NoError(t, err)

	ctx := context.Background()

	t.Run("MigrateData", func(t *testing.T) {
		req := &service.MigrateDataRequest{}
		resp, err := transport.Request(ctx, "quicbaas.Operations", "MigrateData", req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("MigrateData with wrong type", func(t *testing.T) {
		_, err := transport.Request(ctx, "quicbaas.Operations", "MigrateData", "invalid")
		require.Error(t, err)
		var invalidReqErr *InvalidRequestTypeError
		assert.True(t, errors.As(err, &invalidReqErr))
	})

	t.Run("GetMigrationProgress", func(t *testing.T) {
		req := &service.GetMigrationProgressRequest{}
		resp, err := transport.Request(ctx, "quicbaas.Operations", "GetMigrationProgress", req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("GetMigrationProgress with wrong type", func(t *testing.T) {
		_, err := transport.Request(ctx, "quicbaas.Operations", "GetMigrationProgress", "invalid")
		require.Error(t, err)
		var invalidReqErr *InvalidRequestTypeError
		assert.True(t, errors.As(err, &invalidReqErr))
	})

	t.Run("ReplaceNode", func(t *testing.T) {
		req := &service.ReplaceNodeRequest{}
		resp, err := transport.Request(ctx, "quicbaas.Operations", "ReplaceNode", req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("ReplaceNode with wrong type", func(t *testing.T) {
		_, err := transport.Request(ctx, "quicbaas.Operations", "ReplaceNode", "invalid")
		require.Error(t, err)
		var invalidReqErr *InvalidRequestTypeError
		assert.True(t, errors.As(err, &invalidReqErr))
	})

	t.Run("UpgradeNode", func(t *testing.T) {
		req := &service.UpgradeNodeRequest{}
		resp, err := transport.Request(ctx, "quicbaas.Operations", "UpgradeNode", req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("UpgradeNode with wrong type", func(t *testing.T) {
		_, err := transport.Request(ctx, "quicbaas.Operations", "UpgradeNode", "invalid")
		require.Error(t, err)
		var invalidReqErr *InvalidRequestTypeError
		assert.True(t, errors.As(err, &invalidReqErr))
	})

	t.Run("GetUpgradeStatus", func(t *testing.T) {
		req := &service.GetUpgradeStatusRequest{}
		resp, err := transport.Request(ctx, "quicbaas.Operations", "GetUpgradeStatus", req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("GetUpgradeStatus with wrong type", func(t *testing.T) {
		_, err := transport.Request(ctx, "quicbaas.Operations", "GetUpgradeStatus", "invalid")
		require.Error(t, err)
		var invalidReqErr *InvalidRequestTypeError
		assert.True(t, errors.As(err, &invalidReqErr))
	})
}

func TestTransport_AllRBACOperations(t *testing.T) {
	mock := &mockQuicBaaS{}
	transport, err := New(mock)
	require.NoError(t, err)

	ctx := context.Background()

	// Roles only. quicbaas.RBAC has no user verbs: they routed to a
	// service-layer auth manager production never wired, and were deleted with
	// the parallel identity model in issue #905. Users are administered over
	// quicbaas.IAM.
	for _, method := range []string{"CreateUser", "DeleteUser", "ListUsers"} {
		t.Run(method+" is not routed", func(t *testing.T) {
			_, err := transport.Request(ctx, "quicbaas.RBAC", method, nil)
			require.Error(t, err)
		})
	}

	t.Run("CreateRole", func(t *testing.T) {
		req := &service.AuthCreateRoleRequest{}
		resp, err := transport.Request(ctx, "quicbaas.RBAC", "CreateRole", req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("CreateRole with wrong type", func(t *testing.T) {
		_, err := transport.Request(ctx, "quicbaas.RBAC", "CreateRole", "invalid")
		require.Error(t, err)
		var invalidReqErr *InvalidRequestTypeError
		assert.True(t, errors.As(err, &invalidReqErr))
	})

	t.Run("DeleteRole", func(t *testing.T) {
		req := &service.AuthDeleteRoleRequest{}
		resp, err := transport.Request(ctx, "quicbaas.RBAC", "DeleteRole", req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("DeleteRole with wrong type", func(t *testing.T) {
		_, err := transport.Request(ctx, "quicbaas.RBAC", "DeleteRole", "invalid")
		require.Error(t, err)
		var invalidReqErr *InvalidRequestTypeError
		assert.True(t, errors.As(err, &invalidReqErr))
	})

	t.Run("ListRoles", func(t *testing.T) {
		req := &service.AuthListRolesRequest{}
		resp, err := transport.Request(ctx, "quicbaas.RBAC", "ListRoles", req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("ListRoles with wrong type", func(t *testing.T) {
		_, err := transport.Request(ctx, "quicbaas.RBAC", "ListRoles", "invalid")
		require.Error(t, err)
		var invalidReqErr *InvalidRequestTypeError
		assert.True(t, errors.As(err, &invalidReqErr))
	})

	t.Run("AssignRole", func(t *testing.T) {
		req := &service.AuthAssignRoleRequest{}
		resp, err := transport.Request(ctx, "quicbaas.RBAC", "AssignRole", req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("AssignRole with wrong type", func(t *testing.T) {
		_, err := transport.Request(ctx, "quicbaas.RBAC", "AssignRole", "invalid")
		require.Error(t, err)
		var invalidReqErr *InvalidRequestTypeError
		assert.True(t, errors.As(err, &invalidReqErr))
	})

	t.Run("RevokeRole", func(t *testing.T) {
		req := &service.AuthRevokeRoleRequest{}
		resp, err := transport.Request(ctx, "quicbaas.RBAC", "RevokeRole", req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("RevokeRole with wrong type", func(t *testing.T) {
		_, err := transport.Request(ctx, "quicbaas.RBAC", "RevokeRole", "invalid")
		require.Error(t, err)
		var invalidReqErr *InvalidRequestTypeError
		assert.True(t, errors.As(err, &invalidReqErr))
	})
}

// TestUnaryHandlers_OrchestratorAndProjectKeysRegistered verifies that Gap 5
// is complete: the unaryHandlers map contains entries for every orchestrator
// and project operation so that Request() never returns UnknownMethodError for
// a valid method name. This is a compile-time + map coverage test.
func TestUnaryHandlers_OrchestratorAndProjectKeysRegistered(t *testing.T) {
	orchestratorKeys := []string{
		"quicbaas.Orchestrator/Inference",
		"quicbaas.Orchestrator/SpawnSession",
		"quicbaas.Orchestrator/PromptSession",
		"quicbaas.Orchestrator/SessionStatus",
		"quicbaas.Orchestrator/CloseSession",
		"quicbaas.Orchestrator/ListSessions",
		"quicbaas.Orchestrator/CreateTask",
		"quicbaas.Orchestrator/GetTask",
		"quicbaas.Orchestrator/ListTasks",
		"quicbaas.Orchestrator/ClaimTask",
		"quicbaas.Orchestrator/CompleteTask",
		"quicbaas.Orchestrator/SubmitTaskArtifacts",
		"quicbaas.Orchestrator/GetTaskArtifact",
		"quicbaas.Orchestrator/ListTaskArtifacts",
		"quicbaas.Orchestrator/RegisterFleetNode",
		"quicbaas.Orchestrator/GetFleetNode",
		"quicbaas.Orchestrator/ListFleetNodes",
		"quicbaas.Orchestrator/RemoveFleetNode",
		"quicbaas.Orchestrator/FleetNodeHeartbeat",
		"quicbaas.Orchestrator/DisableFleetNode",
		"quicbaas.Orchestrator/EnableFleetNode",
		"quicbaas.Orchestrator/StartJob",
		"quicbaas.Orchestrator/GetJobStatus",
		"quicbaas.Orchestrator/ListJobs",
		"quicbaas.Orchestrator/CancelJob",
		"quicbaas.Orchestrator/SpawnChildJob",
		"quicbaas.Orchestrator/Health",
		"quicbaas.Orchestrator/ListBackends",
		"quicbaas.Orchestrator/DecomposeTask",
	}
	projectKeys := []string{
		"quicbaas.Project/Create",
		"quicbaas.Project/Get",
		"quicbaas.Project/GetByName",
		"quicbaas.Project/Update",
		"quicbaas.Project/Delete",
		"quicbaas.Project/List",
		"quicbaas.Project/GrantFleetNodeRole",
		"quicbaas.Project/RevokeFleetNodeRole",
	}

	allKeys := append(orchestratorKeys, projectKeys...)

	for _, key := range allKeys {
		if _, ok := unaryHandlers[key]; !ok {
			t.Errorf("unaryHandlers missing key: %q", key)
		}
	}
}

// TestTransport_Request_UnknownOrchestratorMethod verifies that an
// unregistered method returns UnknownMethodError (not a panic or nil).
func TestTransport_Request_UnknownOrchestratorMethod(t *testing.T) {
	mock := &mockQuicBaaS{}
	tr, err := New(mock)
	require.NoError(t, err)

	_, err = tr.Request(context.Background(), "quicbaas.Orchestrator", "NonExistentMethod", nil)
	require.Error(t, err)
	var unknownErr *UnknownMethodError
	assert.True(t, errors.As(err, &unknownErr), "expected *UnknownMethodError, got %T", err)
}

// TestTransport_Request_UnknownProjectMethod verifies that an unregistered
// project method returns UnknownMethodError.
func TestTransport_Request_UnknownProjectMethod(t *testing.T) {
	mock := &mockQuicBaaS{}
	tr, err := New(mock)
	require.NoError(t, err)

	_, err = tr.Request(context.Background(), "quicbaas.Project", "NonExistentMethod", nil)
	require.Error(t, err)
	var unknownErr *UnknownMethodError
	assert.True(t, errors.As(err, &unknownErr), "expected *UnknownMethodError, got %T", err)
}

// Placement returns nil: this double has no placement engine.
func (m *mockQuicBaaS) Placement() service.PlacementService { return nil }
