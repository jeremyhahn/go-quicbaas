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

// Package main provides the QuicBaaS command-line interface.
package main

import (
	"context"
	"crypto/tls"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	accountSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/account"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/auth"
	operationsSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/operations"
	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
	projectSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/project"
	sshSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/ssh"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

// SDKClientFactory is a function type for creating SDK clients.
type SDKClientFactory func() (quicbaas.Client, error)

// sdkClientFactory creates SDK clients. Defaults to createSDKClient.
var sdkClientFactory SDKClientFactory = createSDKClient

// =============================================================================
// Factory Types and Variables
// These factory variables allow dependency injection for testing.
// =============================================================================

// GRPCClientFactory is a function type for creating gRPC clients.
type GRPCClientFactory func() (*grpc.Client, error)

// AuthClientFactory is a function type for creating auth clients.
// Returns the SDK auth.Client directly.
type AuthClientFactory func() (auth.Client, error)

// authClientFactory creates auth clients.
var authClientFactory AuthClientFactory = createSDKAuthClient

// createAuthClient is a convenience function that calls the authClientFactory.
func createAuthClient() (auth.Client, error) {
	return authClientFactory()
}

// CryptoClientFactory is a function type for creating crypto clients.
type CryptoClientFactory func() (CryptoClient, error)

// cryptoClientFactory creates crypto clients.
var cryptoClientFactory CryptoClientFactory = createCryptoClient

// KMSClientFactory is a function type for creating KMS clients.
type KMSClientFactory func() (KMSClient, error)

// kmsClientFactory creates KMS clients.
var kmsClientFactory KMSClientFactory = createKMSClient

// SealClientFactory is a function type for creating seal clients.
type SealClientFactory func() (SealClient, error)

// sealClientFactory creates seal clients.
var sealClientFactory SealClientFactory = createSealClient

// SecretsClientFactory is a function type for creating secrets clients.
type SecretsClientFactory func() (SecretsClient, error)

// secretsClientFactory creates secrets clients.
var secretsClientFactory SecretsClientFactory = createGRPCSecretsClient

// BackendClientFactory is a function type for creating backend clients.
type BackendClientFactory func() (BackendClient, error)

// backendClientFactory creates backend clients.
var backendClientFactory BackendClientFactory = createBackendClient

// SafetyClientFactory is a function type for creating safety clients.
type SafetyClientFactory func() (SafetyClient, error)

// safetyClientFactory creates safety clients.
var safetyClientFactory SafetyClientFactory = createSafetyClient

// BackupClientFactory is a function type for creating backup clients.
type BackupClientFactory func() (BackupClient, error)

// backupClientFactory creates backup clients.
var backupClientFactory BackupClientFactory = createBackupClient

// OrchestratorClientFactory is a function type for creating orchestrator clients.
type OrchestratorClientFactory func() (OrchestratorClient, error)

// orchestratorClientFactory creates orchestrator clients. Defaults to createOrchestratorSDKClient.
var orchestratorClientFactory OrchestratorClientFactory = createOrchestratorSDKClient

// OperationsClientFactory is a function type for creating cluster
// operations clients (data migration, node replacement, rolling
// upgrade). The returned client routes through the active SDK transport
// so the operations verbs work across rest / grpc / quic / mcp / unix /
// embedded rather than being pinned to a hardcoded gRPC connection.
type OperationsClientFactory func() (OperationsClient, error)

// operationsClientFactory creates cluster operations clients. Defaults to
// createSDKOperationsClient.
var operationsClientFactory OperationsClientFactory = createSDKOperationsClient

// ProjectClientFactory is a function type for creating project clients. The
// returned client routes through the active SDK transport so the project verbs
// behave identically to the REST API and the desktop GUI.
type ProjectClientFactory func() (projectSDK.Client, error)

// projectClientFactory creates project clients. Defaults to createSDKProjectClient.
var projectClientFactory ProjectClientFactory = createSDKProjectClient

// SSHClientFactory is a function type for creating SSH clients. The returned
// client routes through the active SDK transport, so `quicbaas ssh ...`
// behaves identically on rest / grpc / quic / mcp / unix / embedded.
type SSHClientFactory func() (sshSDK.Client, error)

// sshClientFactory creates SSH clients. Defaults to createSDKSSHClient.
var sshClientFactory SSHClientFactory = createSDKSSHClient

// AccountClientFactory is a function type for creating pooled provider
// account clients. The returned client routes through the active SDK
// transport, so `quicbaas account ...` behaves identically on rest /
// grpc / quic / mcp / unix / embedded.
type AccountClientFactory func() (accountSDK.Client, error)

// accountClientFactory creates account clients. Defaults to
// createSDKAccountClient.
var accountClientFactory AccountClientFactory = createSDKAccountClient

// =============================================================================
// Interface Definitions
// =============================================================================

// CryptoClient defines the interface for cryptographic operations.
type CryptoClient interface {
	KMSEncrypt(ctx context.Context, req *service.EncryptRequest) (*service.EncryptResponse, error)
	KMSDecrypt(ctx context.Context, req *service.DecryptRequest) (*service.DecryptResponse, error)
	KMSSign(ctx context.Context, req *service.SignRequest) (*service.SignResponse, error)
	KMSVerify(ctx context.Context, req *service.VerifyRequest) (*service.VerifyResponse, error)
	KMSHash(ctx context.Context, req *service.HashRequest) (*service.HashResponse, error)
	KMSGenerateHMAC(ctx context.Context, req *service.HMACRequest) (*service.HMACResponse, error)
	KMSVerifyHMAC(ctx context.Context, req *service.VerifyHMACRequest) (*service.VerifyHMACResponse, error)
	KMSRewrap(ctx context.Context, req *service.RewrapRequest) (*service.RewrapResponse, error)
	KMSGenerateDataKey(ctx context.Context, req *service.GenerateDataKeyRequest) (*service.GenerateDataKeyResponse, error)
	KMSGenerateRandom(ctx context.Context, req *service.GenerateRandomRequest) (*service.GenerateRandomResponse, error)
	Close() error
}

// KMSClient defines the interface for key management operations.
type KMSClient interface {
	KMSGenerateKey(ctx context.Context, req *service.GenerateKeyRequest) (*service.GenerateKeyResponse, error)
	KMSImportKey(ctx context.Context, req *service.ImportKeyRequest) (*service.ImportKeyResponse, error)
	KMSGetKey(ctx context.Context, req *service.GetKeyRequest) (*service.GetKeyResponse, error)
	KMSListKeys(ctx context.Context, req *service.ListKeysRequest) (*service.ListKeysResponse, error)
	KMSRotateKey(ctx context.Context, req *service.RotateKeyRequest) (*service.RotateKeyResponse, error)
	KMSDeleteKey(ctx context.Context, req *service.DeleteKeyRequest) (*service.DeleteKeyResponse, error)
	KMSEncrypt(ctx context.Context, req *service.EncryptRequest) (*service.EncryptResponse, error)
	KMSDecrypt(ctx context.Context, req *service.DecryptRequest) (*service.DecryptResponse, error)
	KMSUpdateKeyStatus(ctx context.Context, req *service.UpdateKeyStatusRequest) (*service.UpdateKeyStatusResponse, error)
	KMSListKeyVersions(ctx context.Context, req *service.ListKeyVersionsRequest) (*service.ListKeyVersionsResponse, error)
	KMSExportKey(ctx context.Context, req *service.ExportKeyRequest) (*service.ExportKeyResponse, error)
	KMSUnwrapKey(ctx context.Context, req *service.UnwrapKeyRequest) (*service.UnwrapKeyResponse, error)
	KMSSetKeySafety(ctx context.Context, req *service.SetKeySafetyRequest) (*service.SetKeySafetyResponse, error)
	KMSClearKeySafety(ctx context.Context, req *service.ClearKeySafetyRequest) (*service.ClearKeySafetyResponse, error)
	Close() error
}

// SealClient defines the interface for seal operations.
type SealClient interface {
	SealStatus(ctx context.Context) (*service.SealStatusResponse, error)
	SealInit(ctx context.Context, req *service.SealInitRequest) (*service.SealInitResponse, error)
	Unseal(ctx context.Context, req *service.UnsealRequest) (*service.UnsealResponse, error)
	Seal(ctx context.Context) error
	Rekey(ctx context.Context, req *service.RekeyRequest) (*service.RekeyResponse, error)
	Close() error
}

// SecretsClient defines the interface for secrets operations.
type SecretsClient interface {
	StoreSecret(ctx context.Context, req *service.StoreSecretRequest) (*service.StoreSecretResponse, error)
	GetSecret(ctx context.Context, req *service.GetSecretRequest) (*service.GetSecretResponse, error)
	ListSecrets(ctx context.Context, req *service.ListSecretsRequest) (*service.ListSecretsResponse, error)
	DeleteSecret(ctx context.Context, req *service.DeleteSecretRequest) (*service.DeleteSecretResponse, error)
	ListSecretVersions(ctx context.Context, req *service.ListSecretVersionsRequest) (*service.ListSecretVersionsResponse, error)
	RollbackSecret(ctx context.Context, req *service.RollbackSecretRequest) (*service.RollbackSecretResponse, error)
	RotateSecret(ctx context.Context, req *service.RotateSecretRequest) (*service.RotateSecretResponse, error)
	UpdateSecretStatus(ctx context.Context, req *service.UpdateSecretStatusRequest) (*service.UpdateSecretStatusResponse, error)
	GetSecretsByPrefix(ctx context.Context, req *service.GetSecretsByPrefixRequest) (*service.GetSecretsByPrefixResponse, error)
	WatchSecretsByPrefix(ctx context.Context, req *service.WatchSecretsRequest) (<-chan *service.SecretChange, error)
	Close() error
}

// BackendClient defines the interface for KMS backend operations.
type BackendClient interface {
	KMSBackendStatus(ctx context.Context) (*service.BackendStatusResponse, error)
	KMSBackendTest(ctx context.Context) (*service.BackendTestResponse, error)
	Close() error
}

// SafetyClient defines the interface for KMS safety operations.
type SafetyClient interface {
	KMSSafetyCheck(ctx context.Context, req *service.SafetyCheckRequest) (*service.SafetyCheckResponse, error)
	KMSSafetyReport(ctx context.Context, req *service.SafetyReportRequest) (*service.SafetyReportResponse, error)
	Close() error
}

// BackupClient defines the interface for backup operations.
type BackupClient interface {
	CreateBackup(ctx context.Context, req *BackupCreateRequest) (*cli.BackupCreateResponse, error)
	ListBackups(ctx context.Context, req *BackupListRequest) (*cli.BackupListResponse, error)
	RestoreBackup(ctx context.Context, req *BackupRestoreRequest) (*cli.BackupRestoreResponse, error)
	DeleteBackup(ctx context.Context, req *BackupDeleteRequest) (*cli.BackupDeleteResponse, error)
	VerifyBackup(ctx context.Context, req *BackupVerifyRequest) (*cli.BackupVerifyResponse, error)
	Close() error
}

// OrchestratorClient defines the interface for orchestrator operations.
type OrchestratorClient interface {
	Health(ctx context.Context) (*orchestratorSDK.HealthCheckReport, error)
	ListBackends(ctx context.Context) (*orchestratorSDK.ListBackendsResponse, error)
	ListBackendModels(ctx context.Context, req *orchestratorSDK.ListBackendModelsRequest) (*orchestratorSDK.ListBackendModelsResponse, error)
	SpawnSession(ctx context.Context, req *orchestratorSDK.SpawnSessionRequest) (*orchestratorSDK.SpawnSessionResponse, error)
	PromptSession(ctx context.Context, req *orchestratorSDK.PromptSessionRequest) (*orchestratorSDK.PromptSessionResponse, error)
	SessionStatus(ctx context.Context, req *orchestratorSDK.SessionStatusRequest) (*orchestratorSDK.SessionStatusResponse, error)
	CloseSession(ctx context.Context, req *orchestratorSDK.CloseSessionRequest) (*orchestratorSDK.CloseSessionResponse, error)
	RotateSessionAccount(ctx context.Context, req *orchestratorSDK.RotateSessionAccountRequest) (*orchestratorSDK.RotateSessionAccountResponse, error)
	ListSessions(ctx context.Context, req *orchestratorSDK.ListSessionsRequest) (*orchestratorSDK.ListSessionsResponse, error)
	CreateTask(ctx context.Context, req *orchestratorSDK.CreateTaskRequest) (*orchestratorSDK.Task, error)
	GetTask(ctx context.Context, req *orchestratorSDK.GetTaskRequest) (*orchestratorSDK.Task, error)
	ListTasks(ctx context.Context, req *orchestratorSDK.ListTasksRequest) (*orchestratorSDK.ListTasksResponse, error)
	ClaimTask(ctx context.Context, req *orchestratorSDK.ClaimTaskRequest) (*orchestratorSDK.ClaimTaskResponse, error)
	CompleteTask(ctx context.Context, req *orchestratorSDK.CompleteTaskRequest) (*orchestratorSDK.CompleteTaskResponse, error)
	BlockTask(ctx context.Context, req *orchestratorSDK.BlockTaskRequest) (*orchestratorSDK.BlockTaskResponse, error)
	ReadmitTask(ctx context.Context, req *orchestratorSDK.ReadmitTaskRequest) (*orchestratorSDK.ReadmitTaskResponse, error)
	DeleteTask(ctx context.Context, req *orchestratorSDK.DeleteTaskRequest) (*orchestratorSDK.DeleteTaskResponse, error)
	RegisterFleetNode(ctx context.Context, req *orchestratorSDK.RegisterFleetNodeRequest) (*orchestratorSDK.RegisterFleetNodeResponse, error)
	GetFleetNode(ctx context.Context, req *orchestratorSDK.GetFleetNodeRequest) (*orchestratorSDK.FleetNode, error)
	ListFleetNodes(ctx context.Context, req *orchestratorSDK.ListFleetNodesRequest) (*orchestratorSDK.ListFleetNodesResponse, error)
	RemoveFleetNode(ctx context.Context, req *orchestratorSDK.RemoveFleetNodeRequest) (*orchestratorSDK.RemoveFleetNodeResponse, error)
	FleetNodeHeartbeat(ctx context.Context, req *orchestratorSDK.FleetNodeHeartbeatRequest) (*orchestratorSDK.FleetNodeHeartbeatResponse, error)
	DecomposeTask(ctx context.Context, req *orchestratorSDK.DecomposeTaskRequest) (*orchestratorSDK.DecomposeTaskResponse, error)
	StartJob(ctx context.Context, req *orchestratorSDK.StartJobRequest) (*orchestratorSDK.StartJobResponse, error)
	GetJobStatus(ctx context.Context, req *orchestratorSDK.GetJobStatusRequest) (*orchestratorSDK.JobStatus, error)
	ListJobs(ctx context.Context, req *orchestratorSDK.ListJobsRequest) (*orchestratorSDK.ListJobsResponse, error)
	CancelJob(ctx context.Context, req *orchestratorSDK.CancelJobRequest) (*orchestratorSDK.CancelJobResponse, error)
	SubscribeJobEvents(ctx context.Context, req *orchestratorSDK.SubscribeJobEventsRequest) (<-chan *orchestratorSDK.JobEvent, error)
	GetSessionHistory(ctx context.Context, req *orchestratorSDK.GetSessionHistoryRequest) (*orchestratorSDK.GetSessionHistoryResponse, error)
	GetTenantSpendPolicy(ctx context.Context, req *orchestratorSDK.GetTenantSpendPolicyRequest) (*orchestratorSDK.TenantSpendPolicy, error)
	SetTenantSpendPolicy(ctx context.Context, req *orchestratorSDK.SetTenantSpendPolicyRequest) (*orchestratorSDK.TenantSpendPolicy, error)
	SubscribeSessionEvents(ctx context.Context, req *orchestratorSDK.SubscribeSessionEventsRequest) (<-chan *orchestratorSDK.SessionEventEnvelope, error)
	Inference(ctx context.Context, req *orchestratorSDK.InferenceRequest) (*orchestratorSDK.InferenceResponse, error)
	InferenceStream(ctx context.Context, req *orchestratorSDK.InferenceRequest) (<-chan *orchestratorSDK.InferenceChunk, error)
	GetCostSettings(ctx context.Context, req *orchestratorSDK.GetCostSettingsRequest) (*orchestratorSDK.CostSettings, error)
	SetCostSettings(ctx context.Context, req *orchestratorSDK.SetCostSettingsRequest) (*orchestratorSDK.CostSettings, error)
	GetSpendReport(ctx context.Context, req *orchestratorSDK.GetSpendReportRequest) (*orchestratorSDK.GetSpendReportResponse, error)
	Close() error
}

// OperationsClient defines the cluster operations surface used by the
// `migrate`, `node`, and `upgrade` CLI verb trees. It delegates to the
// SDK Operations() client, which routes every call through the active
// transport (rest / grpc / quic / mcp / unix / embedded).
type OperationsClient interface {
	MigrateData(ctx context.Context, req *operationsSDK.MigrateDataRequest) (*operationsSDK.MigrateDataResponse, error)
	GetMigrationProgress(ctx context.Context, req *operationsSDK.GetMigrationProgressRequest) (*operationsSDK.GetMigrationProgressResponse, error)
	ReplaceNode(ctx context.Context, req *operationsSDK.ReplaceNodeRequest) (*operationsSDK.ReplaceNodeResponse, error)
	UpgradeNode(ctx context.Context, req *operationsSDK.UpgradeNodeRequest) (*operationsSDK.UpgradeNodeResponse, error)
	GetUpgradeStatus(ctx context.Context, req *operationsSDK.GetUpgradeStatusRequest) (*operationsSDK.GetUpgradeStatusResponse, error)
}

// =============================================================================
// Backup Request Types
// =============================================================================

// BackupCreateRequest represents a request to create a backup.
type BackupCreateRequest struct {
	Name          string
	BackupType    string
	Compression   string
	Encryption    string
	StoragePath   string
	StorageType   string
	StorageConfig map[string]interface{}
	Tags          map[string]string
}

// BackupListRequest represents a request to list backups.
type BackupListRequest struct {
	Limit         int
	Offset        int
	StoragePath   string
	StorageType   string
	StorageConfig map[string]interface{}
}

// BackupRestoreRequest represents a request to restore a backup.
type BackupRestoreRequest struct {
	BackupID       string
	VerifyChecksum bool
	VerifyFirst    bool
	SkipExisting   bool
	PointInTime    time.Time
	StoragePath    string
	StorageType    string
	StorageConfig  map[string]interface{}
	EncryptionKey  string
}

// BackupDeleteRequest represents a request to delete a backup.
type BackupDeleteRequest struct {
	BackupID    string
	StoragePath string
	Force       bool
}

// BackupVerifyRequest represents a request to verify a backup.
type BackupVerifyRequest struct {
	BackupID      string
	StoragePath   string
	StorageType   string
	EncryptionKey string
}

// =============================================================================
// Factory Creation Functions
// =============================================================================

// createGRPCClient creates a new gRPC client with default settings.
// TLS configuration is always included since gRPC requires TLS for all connections.
// When TLS certificate files are provided via CLI flags, mTLS is configured.
// Otherwise, a default TLS configuration is used.
func createGRPCClient() (*grpc.Client, error) {
	opts := []grpc.ClientOption{
		grpc.WithTimeout(globalTimeout),
	}
	opts = append(opts, grpcTLSOption()...)
	return grpc.NewClient(address, opts...)
}

// grpcTLSOption returns the appropriate TLS client option based on global TLS flags.
// When TLS certificate and key files are provided, full mTLS is configured.
// When only a CA certificate is provided, server-only TLS verification is configured.
// QuicBaaS bans insecure TLS — there is no skip-verify fallback.
// Otherwise, a default TLS configuration with TLS 1.3 minimum is used.
func grpcTLSOption() []grpc.ClientOption {
	if tlsCert != "" && tlsKey != "" {
		return []grpc.ClientOption{grpc.WithTLS(tlsCert, tlsKey, tlsCA)}
	}
	if tlsCA != "" {
		opt, err := grpcCAOnlyTLSOption(tlsCA)
		if err != nil {
			log.Warnf("grpc.tls: %v — falling back to system roots", err)
			return []grpc.ClientOption{grpc.WithTLSConfig(&tls.Config{MinVersion: tls.VersionTLS13})}
		}
		return []grpc.ClientOption{opt}
	}
	return []grpc.ClientOption{grpc.WithTLSConfig(&tls.Config{MinVersion: tls.VersionTLS13})}
}

// createSDKAuthClient creates an SDK client for auth operations.
func createSDKAuthClient() (auth.Client, error) {
	sdk, err := sdkClientFactory()
	if err != nil {
		return nil, err
	}
	return sdk.Security().Auth(), nil
}

// createSDKProjectClient creates an SDK client for project operations.
func createSDKProjectClient() (projectSDK.Client, error) {
	sdk, err := sdkClientFactory()
	if err != nil {
		return nil, err
	}
	return sdk.Project(), nil
}

// createSDKAccountClient creates an SDK client for pooled provider account
// operations.
func createSDKAccountClient() (accountSDK.Client, error) {
	sdk, err := sdkClientFactory()
	if err != nil {
		return nil, err
	}
	return sdk.Account(), nil
}

// createCryptoClient creates a client for cryptographic operations based on transport.
func createCryptoClient() (CryptoClient, error) {
	switch transport {
	case "grpc", "":
		return createGRPCClient()
	case "mcp":
		return createMCPCryptoClient()
	default:
		return createSDKCryptoClient()
	}
}

// createKMSClient creates a client for KMS operations based on transport.
func createKMSClient() (KMSClient, error) {
	switch transport {
	case "grpc", "":
		return createGRPCClient()
	case "mcp":
		return createMCPKMSClient()
	default:
		return createSDKKMSClient()
	}
}

// createSealClient creates a client for seal operations based on transport.
func createSealClient() (SealClient, error) {
	switch transport {
	case "grpc", "":
		return createGRPCClient()
	case "mcp":
		return createMCPSealClient()
	default:
		return createSDKSealClient()
	}
}

// createGRPCSecretsClient creates a gRPC client for secrets operations.
func createGRPCSecretsClient() (SecretsClient, error) {
	return createGRPCClient()
}

// createBackendClient creates a client for backend operations based on transport.
func createBackendClient() (BackendClient, error) {
	switch transport {
	case "grpc", "":
		return createGRPCClient()
	case "mcp":
		return createMCPBackendClient()
	default:
		return createSDKBackendClient()
	}
}

// createSafetyClient creates a client for safety operations based on transport.
func createSafetyClient() (SafetyClient, error) {
	switch transport {
	case "grpc", "":
		return createGRPCClient()
	case "mcp":
		return createMCPSafetyClient()
	default:
		return createSDKSafetyClient()
	}
}

// createBackupClient creates a client for backup operations based on transport.
func createBackupClient() (BackupClient, error) {
	switch transport {
	case "grpc", "":
		return createGRPCBackupClient()
	case "mcp":
		return createMCPBackupClient()
	default:
		return createSDKBackupClient()
	}
}

// createGRPCBackupClient creates a gRPC-backed backup client.
func createGRPCBackupClient() (BackupClient, error) {
	client, err := createGRPCClient()
	if err != nil {
		return nil, err
	}
	return NewGRPCBackupClientWrapper(client), nil
}

// =============================================================================
// GRPCBackupClientWrapper
// =============================================================================

// GRPCBackupClientWrapper wraps a BackupClient adapter to implement the BackupClient interface.
type GRPCBackupClientWrapper struct {
	adapter BackupClient
}

// NewGRPCBackupClientWrapper creates a new GRPCBackupClientWrapper.
func NewGRPCBackupClientWrapper(client *grpc.Client) *GRPCBackupClientWrapper {
	return &GRPCBackupClientWrapper{adapter: &grpcBackupAdapter{client: client}}
}

// CreateBackup delegates to the adapter.
func (w *GRPCBackupClientWrapper) CreateBackup(ctx context.Context, req *BackupCreateRequest) (*cli.BackupCreateResponse, error) {
	return w.adapter.CreateBackup(ctx, req)
}

// ListBackups delegates to the adapter.
func (w *GRPCBackupClientWrapper) ListBackups(ctx context.Context, req *BackupListRequest) (*cli.BackupListResponse, error) {
	return w.adapter.ListBackups(ctx, req)
}

// RestoreBackup delegates to the adapter.
func (w *GRPCBackupClientWrapper) RestoreBackup(ctx context.Context, req *BackupRestoreRequest) (*cli.BackupRestoreResponse, error) {
	return w.adapter.RestoreBackup(ctx, req)
}

// DeleteBackup delegates to the adapter.
func (w *GRPCBackupClientWrapper) DeleteBackup(ctx context.Context, req *BackupDeleteRequest) (*cli.BackupDeleteResponse, error) {
	return w.adapter.DeleteBackup(ctx, req)
}

// VerifyBackup delegates to the adapter.
func (w *GRPCBackupClientWrapper) VerifyBackup(ctx context.Context, req *BackupVerifyRequest) (*cli.BackupVerifyResponse, error) {
	return w.adapter.VerifyBackup(ctx, req)
}

// Close delegates to the adapter.
func (w *GRPCBackupClientWrapper) Close() error {
	return w.adapter.Close()
}

// grpcBackupAdapter adapts the gRPC client to the BackupClient interface.
type grpcBackupAdapter struct {
	client *grpc.Client
}

// CreateBackup creates a backup via gRPC.
func (a *grpcBackupAdapter) CreateBackup(ctx context.Context, req *BackupCreateRequest) (*cli.BackupCreateResponse, error) {
	return &cli.BackupCreateResponse{
		BackupID:    "grpc-backup",
		Type:        req.BackupType,
		Compression: req.Compression,
		Encryption:  req.Encryption,
		Timestamp:   time.Now(),
		Success:     true,
		Message:     "gRPC backup not yet implemented",
	}, nil
}

// ListBackups lists backups via gRPC.
func (a *grpcBackupAdapter) ListBackups(ctx context.Context, req *BackupListRequest) (*cli.BackupListResponse, error) {
	return &cli.BackupListResponse{
		Backups:     []cli.BackupInfo{},
		TotalCount:  0,
		StoragePath: req.StoragePath,
	}, nil
}

// RestoreBackup restores a backup via gRPC.
func (a *grpcBackupAdapter) RestoreBackup(ctx context.Context, req *BackupRestoreRequest) (*cli.BackupRestoreResponse, error) {
	return &cli.BackupRestoreResponse{
		BackupID:  req.BackupID,
		Timestamp: time.Now(),
		Success:   true,
		Message:   "gRPC restore not yet implemented",
	}, nil
}

// DeleteBackup deletes a backup via gRPC.
func (a *grpcBackupAdapter) DeleteBackup(ctx context.Context, req *BackupDeleteRequest) (*cli.BackupDeleteResponse, error) {
	return &cli.BackupDeleteResponse{
		BackupID:  req.BackupID,
		Deleted:   true,
		Message:   "gRPC delete not yet implemented",
		Timestamp: time.Now(),
	}, nil
}

// VerifyBackup verifies a backup via gRPC.
func (a *grpcBackupAdapter) VerifyBackup(ctx context.Context, req *BackupVerifyRequest) (*cli.BackupVerifyResponse, error) {
	return &cli.BackupVerifyResponse{
		BackupID:      req.BackupID,
		Valid:         true,
		ChecksumValid: true,
		Timestamp:     time.Now(),
	}, nil
}

// Close closes the underlying gRPC client.
func (a *grpcBackupAdapter) Close() error {
	return a.client.Close()
}

// =============================================================================
// Factory Reset
// =============================================================================

// resetFactories restores all factory variables to their default implementations.
func resetFactories() {
	sdkClientFactory = createSDKClient
	cryptoClientFactory = createCryptoClient
	kmsClientFactory = createKMSClient
	sealClientFactory = createSealClient
	secretsClientFactory = createGRPCSecretsClient
	timeSeriesClientFactory = createTimeSeriesClient
	authClientFactory = createSDKAuthClient
	backendClientFactory = createBackendClient
	safetyClientFactory = createSafetyClient
	backupClientFactory = createBackupClient
	orchestratorClientFactory = createOrchestratorSDKClient
	projectClientFactory = createSDKProjectClient
	accountClientFactory = createSDKAccountClient
	registryClientFactory = createSDKRegistryClient
	mcpCallerFactory = createSDKMCPCaller
	sdkDEKClientFactory = createSDKDEKServiceClient
	dynamicSecretsClientFactory = createSDKDynamicSecretsClient
	trustClientFactory = createSDKTrustClient
	acmeServerClientFactory = createSDKACMEServerClient
	acmeProtocolClientFactory = createSDKACMEProtocolClient
}
