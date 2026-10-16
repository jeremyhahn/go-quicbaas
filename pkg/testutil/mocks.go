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
	"strconv"

	"github.com/stretchr/testify/mock"

	qrdbsecret "github.com/jeremyhahn/go-qrdb/sdk/go/secret"
	xkmsssh "github.com/jeremyhahn/go-xkms/sdk/go/client/ssh"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/messaging"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc"
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
	sdlcService   *service.SDLCService
	promptService *service.PromptService
	loginService  service.LoginServicer
	mock.Mock

	// Sub-service fields for interfaces with conflicting method signatures.
	// These are returned by the corresponding accessor methods.
	CertificateSvc   service.CertificateService
	TrustSvc         service.TrustService
	AttestationSvc   service.AttestationService
	RegistrySvc      service.RegistryService
	PIVSvc           service.PIVService
	FIDO2Svc         service.FIDO2Service
	ACMESvc          service.ACMEServicer
	EmbeddingService service.EmbeddingService
	OllamaService    service.OllamaService

	// Optional override fields. When set, the accessor returns these
	// instead of the mock itself.
	CryptoService     service.CryptoService
	DynamicSecretsSvc service.DynamicSecretsService
	KMSService        service.KMSService
	ParameterStoreSvc service.ParameterStoreService
	VectorService     service.VectorService

	// DisableKMS causes KMS() to return nil, forcing handlers into the
	// KMSManager() fallback path for testing.
	DisableKMS bool

	// SSHClientOverride is returned by SSHClient. Left nil by default so
	// SSHClient reports "not configured"; tests that exercise SSH routing
	// set this to a real xkmsssh.Client.
	SSHClientOverride xkmsssh.Client

	// InviteStoreOverride is returned by GetInviteStore. The REST invite
	// endpoints resolve the canonical persistent InviteStore via an inline
	// interface assertion on the service (mirroring GetIdentityStore); tests
	// set this to the same store instance they seed so the activation flow and
	// the first-boot/REST creation paths exercise ONE store, not a fork.
	InviteStoreOverride iam.InviteStore

	// AgentServiceOverride is returned by Agent. Left nil by default, so a
	// mock reports no orchestration service; tests that exercise the
	// orchestration seams — the shutdown drain above all — set this to a
	// real service.
	AgentServiceOverride orchestration.AgentService
}

// Ensure MockQuicBaaS implements service.QuicBaaS at compile time.
var _ service.QuicBaaS = (*MockQuicBaaS)(nil)

// NewMockQuicBaaS creates a new MockQuicBaaS with testify mock infrastructure.
func NewMockQuicBaaS() *MockQuicBaaS {
	return &MockQuicBaaS{}
}

// ResolveCallerAccount satisfies service.CallerAccountResolver without an
// identity store. It honours the subject the way the real resolver orders
// it: a verified UserID names the account, then an ID the transport marked
// as a user id; anything else, a username above all, is refused, because
// there is no store here to look a username up in.
func (m *MockQuicBaaS) ResolveCallerAccount(_ context.Context, subject interfaces.Subject) (uint64, error) {
	if subject.UserID != 0 {
		return subject.UserID, nil
	}
	if subject.IDKind != interfaces.SubjectIDUserID {
		return 0, callerAccountRefused()
	}
	userID, err := strconv.ParseUint(subject.ID, 10, 64)
	if err != nil || userID == 0 {
		return 0, callerAccountRefused()
	}
	return userID, nil
}

// callerAccountRefused is the error MockQuicBaaS.ResolveCallerAccount returns
// for a subject that names no user account.
func callerAccountRefused() error {
	return &dberrors.QBError{
		Code: dberrors.ErrForbidden,
		Op:   "MockQuicBaaS.ResolveCallerAccount",
		Err:  &service.CallerAccountRefusedError{Reason: "the subject names no user account"},
	}
}

// GetInviteStore returns the configured invite store override. It satisfies the
// inline interface assertion the REST invite endpoints use to resolve the
// canonical persistent InviteStore from the service. Returns (nil, nil) when no
// override is set so handlers exercise the store-unavailable (fail-closed) path.
func (m *MockQuicBaaS) GetInviteStore(_ context.Context) (iam.InviteStore, error) {
	return m.InviteStoreOverride, nil
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

func (m *MockQuicBaaS) IntrospectToken(ctx context.Context, req *service.IntrospectTokenRequest) (*service.TokenIntrospection, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.TokenIntrospection)
	return resp, args.Error(1)
}

// =============================================================================
// RBAC Service Methods (part of service.QuicBaaS)
// =============================================================================

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

// Trust returns the TrustSvc field (may be nil).
func (m *MockQuicBaaS) Trust() service.TrustService {
	return m.TrustSvc
}

// Attestation returns AttestationSvc when set, otherwise nil.
func (m *MockQuicBaaS) Attestation() service.AttestationService {
	return m.AttestationSvc
}

// Registry returns RegistrySvc when set, otherwise nil.
func (m *MockQuicBaaS) Registry() service.RegistryService {
	return m.RegistrySvc
}

// PIV returns PIVSvc when set, otherwise nil.
func (m *MockQuicBaaS) PIV() service.PIVService {
	return m.PIVSvc
}

// FIDO2 returns FIDO2Svc when set, otherwise nil.
func (m *MockQuicBaaS) FIDO2() service.FIDO2Service {
	return m.FIDO2Svc
}

// ACME returns ACMESvc when set, otherwise nil.
func (m *MockQuicBaaS) ACME() service.ACMEServicer {
	return m.ACMESvc
}

// DynamicSecrets returns DynamicSecretsSvc when set, otherwise nil.
func (m *MockQuicBaaS) DynamicSecrets() service.DynamicSecretsService {
	return m.DynamicSecretsSvc
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

// Placement returns nil: the mock has no placement engine. Tests that need
// one build a placement.Placer over an in-memory or real qrdb backend.
func (m *MockQuicBaaS) Placement() service.PlacementService {
	return nil
}

// SchedulerEngine returns nil for the mock — tests that need a
// real engine construct one via scheduler.New or
// scheduler.NewWithClustering directly.
func (m *MockQuicBaaS) SchedulerEngine() *scheduler.SchedulerEngine {
	return nil
}

// SSHClient returns the caller-provided SSHClientOverride when set and
// nil otherwise. Tests that exercise SSH routing assign a concrete
// xkmsssh.Client (typically built via xkmsssh.NewEmbedded over an
// in-memory storage.Backend) before invoking the handler under test.
// Leaving the field nil is the documented "SSH not configured" path.
func (m *MockQuicBaaS) SSHClient() xkmsssh.Client {
	return m.SSHClientOverride
}

// Agent returns the configured orchestration service, nil when unset.
func (m *MockQuicBaaS) Agent() orchestration.AgentService {
	return m.AgentServiceOverride
}

// Project returns a new in-memory project service for the mock.
func (m *MockQuicBaaS) Project() service.ProjectService {
	args := m.Called()
	svc, _ := args.Get(0).(service.ProjectService)
	return svc
}

// Account returns the pooled provider account service. Server bring-up calls
// Account() unconditionally when registering the gRPC AccountService, so this
// returns nil when the test has registered no expectation instead of panicking
// (the same accommodation Close makes for Server.Shutdown). Tests that assert
// account wiring can still register On("Account") as usual.
func (m *MockQuicBaaS) Account() service.AccountService {
	for _, call := range m.ExpectedCalls {
		if call.Method == "Account" {
			args := m.Called()
			svc, _ := args.Get(0).(service.AccountService)
			return svc
		}
	}
	return nil
}

// FleetAdmin delegates to testify mock infrastructure.
func (m *MockQuicBaaS) FleetAdmin() service.FleetAdminService {
	args := m.Called()
	result, _ := args.Get(0).(service.FleetAdminService)
	return result
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

// SetDynamicSecretsService is a no-op for the mock.
func (m *MockQuicBaaS) SetDynamicSecretsService(svc service.DynamicSecretsService) {
	// No-op for mock
}

// SetAgentService is a no-op for the mock.
func (m *MockQuicBaaS) SetAgentService(agent orchestration.AgentService) {
	// No-op for mock
}

// SDLC returns the mock's SDLC service, nil unless set.
func (m *MockQuicBaaS) SDLC() *service.SDLCService {
	return m.sdlcService
}

// SetSDLCService records the SDLC service.
func (m *MockQuicBaaS) SetSDLCService(svc *service.SDLCService) {
	m.sdlcService = svc
}

// Prompts returns the prompt service the mock was given.
func (m *MockQuicBaaS) Prompts() *service.PromptService {
	return m.promptService
}

// SetPromptService records the prompt service.
func (m *MockQuicBaaS) SetPromptService(svc *service.PromptService) {
	m.promptService = svc
}

// SetProjectBindingReader is a no-op: a test that needs a delete checked
// against bindings wires the reader onto its own project service.
func (m *MockQuicBaaS) SetProjectBindingReader(sdlc.BindingReader) {}

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

// SetSecretRegistry is a no-op for the mock.
func (m *MockQuicBaaS) SetSecretRegistry(reg *qrdbsecret.ResolverRegistry) {
	// No-op for mock
}

// SecretRegistry returns nil for the mock.
func (m *MockQuicBaaS) SecretRegistry() *qrdbsecret.ResolverRegistry {
	return nil
}

// LoginCeremony returns the stored login-ceremony facade (nil unless set).
func (m *MockQuicBaaS) LoginCeremony() service.LoginServicer {
	return m.loginService
}

// SetLoginService stores the login-ceremony facade.
func (m *MockQuicBaaS) SetLoginService(svc service.LoginServicer) {
	m.loginService = svc
}

// Close releases service resources. Server.Shutdown closes the service
// facade on every shutdown path, so Close returns nil when the test has
// not registered an expectation; tests that assert close behavior can
// still register On("Close") as usual.
func (m *MockQuicBaaS) Close() error {
	for _, call := range m.ExpectedCalls {
		if call.Method == "Close" {
			args := m.Called()
			return args.Error(0)
		}
	}
	return nil
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

// RotateRootKey delegates to testify mock infrastructure.
func (m *MockQuicBaaS) RotateRootKey(ctx context.Context, req *service.RotateRootKeyRequest) (*service.RotateRootKeyResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.RotateRootKeyResponse)
	return resp, args.Error(1)
}

// SetKeyRotationPolicy delegates to testify mock infrastructure.
func (m *MockQuicBaaS) SetKeyRotationPolicy(ctx context.Context, req *service.SetKeyRotationPolicyRequest) (*service.SetKeyRotationPolicyResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.SetKeyRotationPolicyResponse)
	return resp, args.Error(1)
}

// GetKeyRotationPolicy delegates to testify mock infrastructure.
func (m *MockQuicBaaS) GetKeyRotationPolicy(ctx context.Context, req *service.GetKeyRotationPolicyRequest) (*service.GetKeyRotationPolicyResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.GetKeyRotationPolicyResponse)
	return resp, args.Error(1)
}

// DeleteKeyRotationPolicy delegates to testify mock infrastructure.
func (m *MockQuicBaaS) DeleteKeyRotationPolicy(ctx context.Context, req *service.DeleteKeyRotationPolicyRequest) (*service.DeleteKeyRotationPolicyResponse, error) {
	args := m.Called(ctx, req)
	resp, _ := args.Get(0).(*service.DeleteKeyRotationPolicyResponse)
	return resp, args.Error(1)
}

// SetSealService is a no-op for the mock. The mock implements the SealService
// methods directly so no inner service needs to be injected.
func (m *MockQuicBaaS) SetSealService(_ service.SealService) {
	// No-op for mock — seal methods are implemented directly on MockQuicBaaS.
}
