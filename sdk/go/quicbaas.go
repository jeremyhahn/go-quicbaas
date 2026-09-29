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

package quicbaas

import (
	"crypto/tls"
	"strings"
	"sync"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/spend"

	"github.com/jeremyhahn/go-quicbaas/pkg/registry"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/account"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/agents"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/auth"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/backup"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/batch"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/cache"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/certificate"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/cluster"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/crypto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/dek"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/dynamicsecrets"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/embedding"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/health"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/kms"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/kv"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/messaging"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/objectstorage"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/objectstorageadmin"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/objecttransfer"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/ollama"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/operations"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/parameterstore"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/project"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/query"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/rag"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/rbac"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/scheduler"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/sdlc"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/seal"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/secrets"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/timeseries"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/transaction"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/vector"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport/embedded"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport/grpc"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport/mcp"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport/quic"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport/rest"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport/unix"
)

// DiscoveryRegistry is the service-discovery registry returned by
// Client.Discovery. Re-exported (Layer-5 SDK-only boundary) so consumers can
// reference the return type without importing go-quicbaas/pkg/registry directly.
type DiscoveryRegistry = registry.Registry

// Client is the unified QuicBaaS client interface providing access to all services.
// It composes service sub-interfaces for organized access while maintaining
// all direct service accessor methods for backward compatibility.
type Client interface {
	// Sub-interface accessors (organized API)
	CoreData() CoreDataService
	ClusterMgmt() ClusterManagementService
	Security() SecurityService
	Identity() IdentityService
	Search() SearchService
	Storage() StorageService
	Async() AsyncService
	AI() AIService
	BarrierCeremonyMgmt() BarrierCeremonyService
	TenantMgmtService() TenantService

	// Core data operations (backward compatible)
	KV() KVClient
	Batch() BatchClient
	Transaction() TransactionClient
	Query() QueryClient

	// Cluster operations (backward compatible)
	Cluster() ClusterClient
	Health() HealthClient
	Operations() OperationsClient
	Cache() CacheClient

	// Security operations are reachable exclusively through Security().*:
	// Security().Auth(), Security().Seal(), Security().KMS(),
	// Security().Secrets(), Security().Crypto(), Security().RBAC(),
	// Security().DynamicSecrets(), Security().Certificate(),
	// Security().DEK(). There is exactly one canonical entry point per
	// capability (the locked-architecture rule); the flat Client.X()
	// shortcuts were removed.
	//
	// IAM follows the same rule: Identity().IAM() is the only entry point.
	// The flat Client.IAM() shortcut was removed in issue #905.

	// Specialized data operations (backward compatible)
	Vector() VectorClient
	TimeSeries() TimeSeriesClient
	Backup() BackupClient
	ObjectStorage() ObjectStorageClient
	ObjectStorageAdmin() ObjectStorageAdminClient
	ObjectTransfer() ObjectTransferClient
	ParameterStore() ParameterStoreClient

	// Scheduler operations (backward compatible)
	Scheduler() SchedulerClient

	// Messaging operations (backward compatible)
	Messaging() MessagingClient

	// AI/ML operations (backward compatible)
	Embedding() EmbeddingClient
	Ollama() OllamaClient
	RAG() RAGClient

	// Project operations (agentic project management)
	Project() ProjectClient

	// SDLC operations (the SDLC module: forge bindings and deliveries)
	SDLC() SDLCClient

	// Agents operations: the tenant's AI agent catalog — the named Agents
	// (runtime + model + provider) every role slot references, and the
	// ordered pools a slot fails over along. Not the fleet of daemons.
	Agents() AgentsClient

	// Account operations (pooled provider accounts — the per-tenant pool of
	// Anthropic/Claude, Codex, and OpenCode logins an agent session can be
	// spawned against). This is the single canonical entry point for the
	// surface; no flat or duplicate accessor exists.
	Account() AccountClient

	// Orchestrator operations (fleet management)
	Orchestrator() OrchestratorClient

	// Barrier + ceremony moved to Security().Barrier() / .Ceremony() —
	// the single canonical SDK entry point for crypto/auth surfaces.
	SealStore() (QRDBSealStoreClient, error)
	TenantMgmt() (QRDBTenantClient, error)
	CustodianGroup() (QRDBCustodianGroupClient, error)
	Share() (QRDBShareClient, error)

	// Discovery returns the service-discovery registry, built on the QRDB
	// lease + watch + KV primitives (register/renew/deregister/resolve/watch).
	// The return type is aliased as DiscoveryRegistry so SDK consumers can
	// reference it without importing go-quicbaas/pkg/registry directly.
	Discovery() (*DiscoveryRegistry, error)

	// DataStore returns a KVStore suitable for constructing DAOs.
	// Uses the QRDB transport layer.
	DataStore() (QRDBKVStore, error)

	// Connection management
	Transport() Transport
	Close() error
}

// Transport is an alias for transport.Transport interface.
type Transport = transport.Transport

// Stream is an alias for transport.Stream interface.
type Stream = transport.Stream

// KVClient is an alias for the kv.Client interface.
type KVClient = kv.Client

// KV request/response type aliases
type (
	// PutRequest is an alias for service.PutRequest.
	PutRequest = service.PutRequest
	// PutResponse is an alias for service.PutResponse.
	PutResponse = service.PutResponse
	// GetRequest is an alias for service.GetRequest.
	GetRequest = service.GetRequest
	// GetResponse is an alias for service.GetResponse.
	GetResponse = service.GetResponse
	// DeleteRequest is an alias for service.DeleteRequest.
	DeleteRequest = service.DeleteRequest
	// DeleteResponse is an alias for service.DeleteResponse.
	DeleteResponse = service.DeleteResponse
	// ListRequest is an alias for service.ListRequest.
	ListRequest = service.ListRequest
	// ListResponse is an alias for service.ListResponse.
	ListResponse = service.ListResponse
)

// AuthClient is an alias for the auth.Client interface.
type AuthClient = auth.Client

// BackupClient is an alias for the backup.Client interface.
type BackupClient = backup.Client

// BackupRequest is an alias for the backup.BackupRequest type.
type BackupRequest = backup.BackupRequest

// BackupResponse is an alias for the backup.BackupResponse type.
type BackupResponse = backup.BackupResponse

// RestoreRequest is an alias for the backup.RestoreRequest type.
type RestoreRequest = backup.RestoreRequest

// RestoreResponse is an alias for the backup.RestoreResponse type.
type RestoreResponse = backup.RestoreResponse

// ListBackupsResponse is an alias for the backup.ListBackupsResponse type.
type ListBackupsResponse = backup.ListBackupsResponse

// BackupInfo is an alias for the backup.BackupInfo type.
type BackupInfo = backup.BackupInfo

// TimeSeriesClient is an alias for the timeseries.Client interface.
type TimeSeriesClient = timeseries.Client

// TimeSeriesWriteRequest is an alias for the timeseries.WriteRequest type.
type TimeSeriesWriteRequest = timeseries.WriteRequest

// TimeSeriesQueryRequest is an alias for the timeseries.QueryRequest type.
type TimeSeriesQueryRequest = timeseries.QueryRequest

// TimeSeriesQueryResponse is an alias for the timeseries.QueryResponse type.
type TimeSeriesQueryResponse = timeseries.QueryResponse

// TimeSeriesPoint is an alias for the timeseries.Point type.
type TimeSeriesPoint = timeseries.Point

// AggregateFunction is an alias for the timeseries.AggregateFunction type.
type AggregateFunction = timeseries.AggregateFunction

// Aggregate function constants
const (
	AggregateNone  = timeseries.AggregateNone
	AggregateSum   = timeseries.AggregateSum
	AggregateAvg   = timeseries.AggregateAvg
	AggregateMin   = timeseries.AggregateMin
	AggregateMax   = timeseries.AggregateMax
	AggregateCount = timeseries.AggregateCount
)

// BatchClient is an alias for the batch.Client interface.
type BatchClient = batch.Client

// BatchRequest is an alias for batch.BatchRequest.
type BatchRequest = batch.BatchRequest

// BatchOperation is an alias for batch.BatchOperation.
type BatchOperation = batch.BatchOperation

// BatchOpType is an alias for batch.OpType.
type BatchOpType = batch.OpType

// BatchResponse is an alias for batch.BatchResponse.
type BatchResponse = batch.BatchResponse

// BatchResult is an alias for batch.BatchResult.
type BatchResult = batch.BatchResult

// Batch operation type constants
const (
	BatchOpPut    = batch.OpPut
	BatchOpDelete = batch.OpDelete
)

// TransactionClient is an alias for the transaction.Client interface.
type TransactionClient = transaction.Client

// TxnRequest is an alias for transaction.TxnRequest.
type TxnRequest = transaction.TxnRequest

// TxnOp is an alias for transaction.TxnOp.
type TxnOp = transaction.TxnOp

// Compare is an alias for transaction.Compare.
type Compare = transaction.Compare

// CompareTarget is an alias for transaction.CompareTarget.
type CompareTarget = transaction.CompareTarget

// CompareResult is an alias for transaction.CompareResult.
type CompareResult = transaction.CompareResult

// TxnResponse is an alias for transaction.TxnResponse.
type TxnResponse = transaction.TxnResponse

// Compare target constants
const (
	CompareVersion        = transaction.CompareVersion
	CompareValue          = transaction.CompareValue
	CompareModRevision    = transaction.CompareModRevision
	CompareCreateRevision = transaction.CompareCreateRevision
)

// Compare result constants
const (
	CompareEqual    = transaction.CompareEqual
	CompareNotEqual = transaction.CompareNotEqual
	CompareLess     = transaction.CompareLess
	CompareGreater  = transaction.CompareGreater
)

// Transaction operation type constants
const (
	TxnOpPut    = transaction.TxnOpPut
	TxnOpGet    = transaction.TxnOpGet
	TxnOpDelete = transaction.TxnOpDelete
)

// TxnOpType is an alias for transaction.TxnOpType.
type TxnOpType = transaction.TxnOpType

// TxnOpResponse is an alias for transaction.TxnOpResponse.
type TxnOpResponse = transaction.TxnOpResponse

// QueryClient is an alias for the query.Client interface.
type QueryClient = query.Client

// QueryRequest is an alias for query.QueryRequest.
type QueryRequest = query.QueryRequest

// QueryResponse is an alias for query.QueryResponse.
type QueryResponse = query.QueryResponse

// QueryResult is an alias for query.QueryResult.
type QueryResult = query.QueryResult

// QueryOperator is an alias for query.Operator.
type QueryOperator = query.Operator

// Query operator constants
const (
	QueryOpEqual            = query.OpEqual
	QueryOpNotEqual         = query.OpNotEqual
	QueryOpLessThan         = query.OpLessThan
	QueryOpLessThanOrEqual  = query.OpLessThanOrEqual
	QueryOpGreaterThan      = query.OpGreaterThan
	QueryOpGreaterThanEqual = query.OpGreaterThanOrEqual
	QueryOpPrefix           = query.OpPrefix
	QueryOpRange            = query.OpRange
)

// ClusterClient is an alias for the cluster.Client interface.
type ClusterClient = cluster.Client

// ClusterStatus is an alias for cluster.ClusterStatus.
type ClusterStatus = cluster.ClusterStatus

// ClusterMember is an alias for cluster.ClusterMember.
type ClusterMember = cluster.ClusterMember

// NodesResponse is an alias for cluster.NodesResponse.
type NodesResponse = cluster.NodesResponse

// NodeInfo is an alias for cluster.NodeInfo.
type NodeInfo = cluster.NodeInfo

// NodeState is an alias for cluster.NodeState.
type NodeState = cluster.NodeState

// AddNodeRequest is an alias for cluster.AddNodeRequest.
type AddNodeRequest = cluster.AddNodeRequest

// RemoveNodeRequest is an alias for cluster.RemoveNodeRequest.
type RemoveNodeRequest = cluster.RemoveNodeRequest

// ClusterShard is an alias for cluster.ClusterShard.
type ClusterShard = cluster.ClusterShard

// RebalanceClusterRequest is an alias for cluster.RebalanceClusterRequest.
type RebalanceClusterRequest = cluster.RebalanceClusterRequest

// RebalanceClusterResponse is an alias for cluster.RebalanceClusterResponse.
type RebalanceClusterResponse = cluster.RebalanceClusterResponse

// GetRebalanceProgressRequest is an alias for cluster.GetRebalanceProgressRequest.
type GetRebalanceProgressRequest = cluster.GetRebalanceProgressRequest

// GetRebalanceProgressResponse is an alias for cluster.GetRebalanceProgressResponse.
type GetRebalanceProgressResponse = cluster.GetRebalanceProgressResponse

// Node state constants
const (
	NodeStateUnknown   = cluster.NodeStateUnknown
	NodeStateHealthy   = cluster.NodeStateHealthy
	NodeStateUnhealthy = cluster.NodeStateUnhealthy
	NodeStateLeaving   = cluster.NodeStateLeaving
)

// HealthClient is an alias for the health.Client interface.
type HealthClient = health.Client

// HealthResponse is an alias for health.HealthResponse.
type HealthResponse = health.HealthResponse

// SealClient is an alias for the seal.Client interface.
type SealClient = seal.Client

// SealStatus is an alias for seal.SealStatus.
type SealStatus = seal.SealStatus

// InitRequest is an alias for seal.InitRequest.
type InitRequest = seal.InitRequest

// InitResponse is an alias for seal.InitResponse.
type InitResponse = seal.InitResponse

// UnsealRequest is an alias for seal.UnsealRequest.
type UnsealRequest = seal.UnsealRequest

// UnsealResponse is an alias for seal.UnsealResponse.
type UnsealResponse = seal.UnsealResponse

// KMSClient is an alias for the kms.Client interface.
type KMSClient = kms.Client

// GenerateKeyRequest is an alias for kms.GenerateKeyRequest.
type GenerateKeyRequest = kms.GenerateKeyRequest

// GenerateKeyResponse is an alias for kms.GenerateKeyResponse.
type GenerateKeyResponse = kms.GenerateKeyResponse

// KeyInfo is an alias for kms.KeyInfo.
type KeyInfo = kms.KeyInfo

// ListKeysResponse is an alias for kms.ListKeysResponse.
type ListKeysResponse = kms.ListKeysResponse

// RotateKeyResponse is an alias for kms.RotateKeyResponse.
type RotateKeyResponse = kms.RotateKeyResponse

// KeyAlgorithm is an alias for kms.KeyAlgorithm.
type KeyAlgorithm = kms.KeyAlgorithm

// KeyUsage is an alias for kms.KeyUsage.
type KeyUsage = kms.KeyUsage

// Key algorithm constants
const (
	AlgorithmAES256GCM        = kms.AlgorithmAES256GCM
	AlgorithmChaCha20Poly1305 = kms.AlgorithmChaCha20Poly1305
	AlgorithmRSA2048          = kms.AlgorithmRSA2048
	AlgorithmRSA4096          = kms.AlgorithmRSA4096
	AlgorithmECDSAP256        = kms.AlgorithmECDSAP256
	AlgorithmECDSAP384        = kms.AlgorithmECDSAP384
	AlgorithmED25519          = kms.AlgorithmED25519
)

// Key usage constants
const (
	KeyUsageEncrypt = kms.KeyUsageEncrypt
	KeyUsageDecrypt = kms.KeyUsageDecrypt
	KeyUsageSign    = kms.KeyUsageSign
	KeyUsageVerify  = kms.KeyUsageVerify
	KeyUsageWrap    = kms.KeyUsageWrap
	KeyUsageUnwrap  = kms.KeyUsageUnwrap
)

// KMS extended type aliases

// ImportKeyRequest is an alias for kms.ImportKeyRequest.
type ImportKeyRequest = kms.ImportKeyRequest

// ImportKeyResponse is an alias for kms.ImportKeyResponse.
type ImportKeyResponse = kms.ImportKeyResponse

// ExportKeyRequest is an alias for kms.ExportKeyRequest.
type ExportKeyRequest = kms.ExportKeyRequest

// ExportKeyResponse is an alias for kms.ExportKeyResponse.
type ExportKeyResponse = kms.ExportKeyResponse

// UnwrapKeyRequest is an alias for kms.UnwrapKeyRequest.
type UnwrapKeyRequest = kms.UnwrapKeyRequest

// UnwrapKeyResponse is an alias for kms.UnwrapKeyResponse.
type UnwrapKeyResponse = kms.UnwrapKeyResponse

// KeyMetadataResponse is an alias for kms.KeyMetadataResponse.
type KeyMetadataResponse = kms.KeyMetadataResponse

// UpdateKeyMetadataRequest is an alias for kms.UpdateKeyMetadataRequest.
type UpdateKeyMetadataRequest = kms.UpdateKeyMetadataRequest

// UpdateKeyStatusRequest is an alias for kms.UpdateKeyStatusRequest.
type UpdateKeyStatusRequest = kms.UpdateKeyStatusRequest

// UpdateKeyStatusResponse is an alias for kms.UpdateKeyStatusResponse.
type UpdateKeyStatusResponse = kms.UpdateKeyStatusResponse

// ListKeyVersionsResponse is an alias for kms.ListKeyVersionsResponse.
type ListKeyVersionsResponse = kms.ListKeyVersionsResponse

// KeyVersionInfo is an alias for kms.KeyVersionInfo.
type KeyVersionInfo = kms.KeyVersionInfo

// KeyPolicy is an alias for kms.KeyPolicy.
type KeyPolicy = kms.KeyPolicy

// SetKeyPolicyRequest is an alias for kms.SetKeyPolicyRequest.
type SetKeyPolicyRequest = kms.SetKeyPolicyRequest

// SupportedAlgorithmsResponse is an alias for kms.SupportedAlgorithmsResponse.
type SupportedAlgorithmsResponse = kms.SupportedAlgorithmsResponse

// BackendStatusResponse is an alias for kms.BackendStatusResponse.
type BackendStatusResponse = kms.BackendStatusResponse

// KeyStatus is an alias for kms.KeyStatus.
type KeyStatus = kms.KeyStatus

// Key status constants
const (
	KeyStatusEnabled         = kms.KeyStatusEnabled
	KeyStatusDisabled        = kms.KeyStatusDisabled
	KeyStatusPendingDeletion = kms.KeyStatusPendingDeletion
	KeyStatusDeleted         = kms.KeyStatusDeleted
	KeyStatusRotating        = kms.KeyStatusRotating
	KeyStatusCompromised     = kms.KeyStatusCompromised
)

// SecretsClient is an alias for the secrets.Client interface.
type SecretsClient = secrets.Client

// StoreSecretRequest is an alias for secrets.StoreSecretRequest.
type StoreSecretRequest = secrets.StoreSecretRequest

// StoreSecretResponse is an alias for secrets.StoreSecretResponse.
type StoreSecretResponse = secrets.StoreSecretResponse

// Secret is an alias for secrets.Secret.
type Secret = secrets.Secret

// ListSecretsResponse is an alias for secrets.ListSecretsResponse.
type ListSecretsResponse = secrets.ListSecretsResponse

// SecretOption is an alias for secrets.GetOption.
type SecretOption = secrets.GetOption

// WithSecretVersion returns a secret with a specific version.
var WithSecretVersion = secrets.WithVersion

// Secrets extended type aliases

// GetSecretVersionRequest is an alias for secrets.GetSecretVersionRequest.
type GetSecretVersionRequest = secrets.GetSecretVersionRequest

// SecretVersionInfo is an alias for secrets.SecretVersionInfo.
type SecretVersionInfo = secrets.SecretVersionInfo

// ListSecretVersionsResponse is an alias for secrets.ListSecretVersionsResponse.
type ListSecretVersionsResponse = secrets.ListSecretVersionsResponse

// RotateSecretRequest is an alias for secrets.RotateSecretRequest.
type RotateSecretRequest = secrets.RotateSecretRequest

// RotateSecretResponse is an alias for secrets.RotateSecretResponse.
type RotateSecretResponse = secrets.RotateSecretResponse

// RollbackSecretRequest is an alias for secrets.RollbackSecretRequest.
type RollbackSecretRequest = secrets.RollbackSecretRequest

// RollbackSecretResponse is an alias for secrets.RollbackSecretResponse.
type RollbackSecretResponse = secrets.RollbackSecretResponse

// UpdateSecretStatusRequest is an alias for secrets.UpdateSecretStatusRequest.
type UpdateSecretStatusRequest = secrets.UpdateSecretStatusRequest

// SecretRotationPolicy is an alias for secrets.RotationPolicy.
type SecretRotationPolicy = secrets.RotationPolicy

// SetSecretRotationPolicyRequest is an alias for secrets.SetRotationPolicyRequest.
type SetSecretRotationPolicyRequest = secrets.SetRotationPolicyRequest

// CryptoClient is an alias for the crypto.Client interface.
type CryptoClient = crypto.Client

// EncryptRequest is an alias for crypto.EncryptRequest.
type EncryptRequest = crypto.EncryptRequest

// EncryptResponse is an alias for crypto.EncryptResponse.
type EncryptResponse = crypto.EncryptResponse

// DecryptRequest is an alias for crypto.DecryptRequest.
type DecryptRequest = crypto.DecryptRequest

// DecryptResponse is an alias for crypto.DecryptResponse.
type DecryptResponse = crypto.DecryptResponse

// SignRequest is an alias for crypto.SignRequest.
type SignRequest = crypto.SignRequest

// SignResponse is an alias for crypto.SignResponse.
type SignResponse = crypto.SignResponse

// VerifyRequest is an alias for crypto.VerifyRequest.
type VerifyRequest = crypto.VerifyRequest

// VerifyResponse is an alias for crypto.VerifyResponse.
type VerifyResponse = crypto.VerifyResponse

// HashAlgorithm is an alias for crypto.HashAlgorithm.
type HashAlgorithm = crypto.HashAlgorithm

// Hash algorithm constants
const (
	HashSHA256 = crypto.HashSHA256
	HashSHA384 = crypto.HashSHA384
	HashSHA512 = crypto.HashSHA512
)

// Crypto extended type aliases

// RewrapRequest is an alias for crypto.RewrapRequest.
type RewrapRequest = crypto.RewrapRequest

// RewrapResponse is an alias for crypto.RewrapResponse.
type RewrapResponse = crypto.RewrapResponse

// EncryptBatchRequest is an alias for crypto.EncryptBatchRequest.
type EncryptBatchRequest = crypto.EncryptBatchRequest

// EncryptBatchResponse is an alias for crypto.EncryptBatchResponse.
type EncryptBatchResponse = crypto.EncryptBatchResponse

// DecryptBatchRequest is an alias for crypto.DecryptBatchRequest.
type DecryptBatchRequest = crypto.DecryptBatchRequest

// DecryptBatchResponse is an alias for crypto.DecryptBatchResponse.
type DecryptBatchResponse = crypto.DecryptBatchResponse

// GenerateDataKeyRequest is an alias for crypto.GenerateDataKeyRequest.
type GenerateDataKeyRequest = crypto.GenerateDataKeyRequest

// GenerateDataKeyResponse is an alias for crypto.GenerateDataKeyResponse.
type GenerateDataKeyResponse = crypto.GenerateDataKeyResponse

// GenerateWrappedDataKeyRequest is an alias for crypto.GenerateWrappedDataKeyRequest.
type GenerateWrappedDataKeyRequest = crypto.GenerateWrappedDataKeyRequest

// GenerateWrappedDataKeyResponse is an alias for crypto.GenerateWrappedDataKeyResponse.
type GenerateWrappedDataKeyResponse = crypto.GenerateWrappedDataKeyResponse

// GenerateHMACRequest is an alias for crypto.GenerateHMACRequest.
type GenerateHMACRequest = crypto.GenerateHMACRequest

// GenerateHMACResponse is an alias for crypto.GenerateHMACResponse.
type GenerateHMACResponse = crypto.GenerateHMACResponse

// VerifyHMACRequest is an alias for crypto.VerifyHMACRequest.
type VerifyHMACRequest = crypto.VerifyHMACRequest

// VerifyHMACResponse is an alias for crypto.VerifyHMACResponse.
type VerifyHMACResponse = crypto.VerifyHMACResponse

// GenerateRandomRequest is an alias for crypto.GenerateRandomRequest.
type GenerateRandomRequest = crypto.GenerateRandomRequest

// GenerateRandomResponse is an alias for crypto.GenerateRandomResponse.
type GenerateRandomResponse = crypto.GenerateRandomResponse

// HashRequest is an alias for crypto.HashRequest.
type HashRequest = crypto.HashRequest

// HashResponse is an alias for crypto.HashResponse.
type HashResponse = crypto.HashResponse

// VectorClient is an alias for the vector.Client interface.
type VectorClient = vector.Client

// VectorUpsertRequest is an alias for vector.UpsertRequest.
type VectorUpsertRequest = vector.UpsertRequest

// VectorUpsertResponse is an alias for vector.UpsertResponse.
type VectorUpsertResponse = vector.UpsertResponse

// VectorSearchRequest is an alias for vector.SearchSimilarRequest.
type VectorSearchRequest = vector.SearchSimilarRequest

// VectorSearchResponse is an alias for vector.SearchSimilarResponse.
type VectorSearchResponse = vector.SearchSimilarResponse

// VectorSearchResult is an alias for vector.SearchResult.
type VectorSearchResult = vector.SearchResult

// VectorDeleteRequest is an alias for vector.DeleteRequest.
type VectorDeleteRequest = vector.DeleteRequest

// IAMClient is an alias for the iam.Client interface.
type IAMClient = iam.Client

// IAM sub-client type aliases
type (
	// IAMTenantClient is an alias for iam.TenantClient.
	IAMTenantClient = iam.TenantClient
	// IAMUserClient is an alias for iam.UserClient.
	IAMUserClient = iam.UserClient
	// IAMRoleClient is an alias for iam.RoleClient.
	IAMRoleClient = iam.RoleClient
	// IAMPolicyClient is an alias for iam.PolicyClient.
	IAMPolicyClient = iam.PolicyClient
	// IAMPEPClient is an alias for iam.PEPClient.
	IAMPEPClient = iam.PEPClient
	// IAMAppClient is an alias for iam.AppClient.
	IAMAppClient = iam.AppClient
	// IAMGDPRClient is an alias for iam.GDPRClient.
	IAMGDPRClient = iam.GDPRClient
	// IAMAuditClient is an alias for iam.AuditClient.
	IAMAuditClient = iam.AuditClient
	// IAMAdminClient is an alias for iam.AdminClient.
	IAMAdminClient = iam.AdminClient
)

// IAM entity type aliases
type (
	// Tenant is an alias for iam.Tenant.
	Tenant = iam.Tenant
	// IAMUser is an alias for iam.User.
	IAMUser = iam.User
	// IAMRole is an alias for iam.Role.
	IAMRole = iam.Role
	// IAMPolicy is an alias for iam.Policy.
	IAMPolicy = iam.Policy
	// IAMCheckRequest is an alias for iam.CheckRequest.
	IAMCheckRequest = iam.CheckRequest
	// IAMCheckResponse is an alias for iam.CheckResponse.
	IAMCheckResponse = iam.CheckResponse
)

// Watcher is an alias for the kv.Watcher interface.
type Watcher = kv.Watcher

// WatchEvent is an alias for the kv.WatchEvent type.
type WatchEvent = kv.WatchEvent

// EventType is an alias for the kv.EventType type.
type EventType = kv.EventType

// Watch option constants
const (
	EventPut    = kv.EventPut
	EventDelete = kv.EventDelete
)

// =============================================================================
// Cache Client Type Aliases
// =============================================================================

// CacheClient is an alias for the cache.Client interface.
type CacheClient = cache.Client

// Cache request/response type aliases
type (
	// GetCacheStatsRequest is an alias for cache.GetCacheStatsRequest.
	GetCacheStatsRequest = cache.GetCacheStatsRequest
	// GetCacheStatsResponse is an alias for cache.GetCacheStatsResponse.
	GetCacheStatsResponse = cache.GetCacheStatsResponse
	// ClearCacheRequest is an alias for cache.ClearCacheRequest.
	ClearCacheRequest = cache.ClearCacheRequest
	// ClearCacheResponse is an alias for cache.ClearCacheResponse.
	ClearCacheResponse = cache.ClearCacheResponse
	// GetCacheConfigRequest is an alias for cache.GetCacheConfigRequest.
	GetCacheConfigRequest = cache.GetCacheConfigRequest
	// GetCacheConfigResponse is an alias for cache.GetCacheConfigResponse.
	GetCacheConfigResponse = cache.GetCacheConfigResponse
	// UpdateCacheConfigRequest is an alias for cache.UpdateCacheConfigRequest.
	UpdateCacheConfigRequest = cache.UpdateCacheConfigRequest
	// UpdateCacheConfigResponse is an alias for cache.UpdateCacheConfigResponse.
	UpdateCacheConfigResponse = cache.UpdateCacheConfigResponse
)

// =============================================================================
// Operations Client Type Aliases
// =============================================================================

// OperationsClient is an alias for the operations.Client interface.
type OperationsClient = operations.Client

// Operations request/response type aliases
type (
	// MigrateDataRequest is an alias for operations.MigrateDataRequest.
	MigrateDataRequest = operations.MigrateDataRequest
	// MigrateDataResponse is an alias for operations.MigrateDataResponse.
	MigrateDataResponse = operations.MigrateDataResponse
	// GetMigrationProgressRequest is an alias for operations.GetMigrationProgressRequest.
	GetMigrationProgressRequest = operations.GetMigrationProgressRequest
	// GetMigrationProgressResponse is an alias for operations.GetMigrationProgressResponse.
	GetMigrationProgressResponse = operations.GetMigrationProgressResponse
	// ReplaceNodeRequest is an alias for operations.ReplaceNodeRequest.
	ReplaceNodeRequest = operations.ReplaceNodeRequest
	// ReplaceNodeResponse is an alias for operations.ReplaceNodeResponse.
	ReplaceNodeResponse = operations.ReplaceNodeResponse
	// UpgradeNodeRequest is an alias for operations.UpgradeNodeRequest.
	UpgradeNodeRequest = operations.UpgradeNodeRequest
	// UpgradeNodeResponse is an alias for operations.UpgradeNodeResponse.
	UpgradeNodeResponse = operations.UpgradeNodeResponse
	// GetUpgradeStatusRequest is an alias for operations.GetUpgradeStatusRequest.
	GetUpgradeStatusRequest = operations.GetUpgradeStatusRequest
	// GetUpgradeStatusResponse is an alias for operations.GetUpgradeStatusResponse.
	GetUpgradeStatusResponse = operations.GetUpgradeStatusResponse
)

// =============================================================================
// RBAC Client Type Aliases
// =============================================================================

// RBACClient is an alias for the rbac.Client interface.
type RBACClient = rbac.Client

// RBAC request/response type aliases
type (
	// RBACCreateRoleRequest is an alias for rbac.CreateRoleRequest.
	RBACCreateRoleRequest = rbac.CreateRoleRequest
	// RBACCreateRoleResponse is an alias for rbac.CreateRoleResponse.
	RBACCreateRoleResponse = rbac.CreateRoleResponse
	// RBACDeleteRoleRequest is an alias for rbac.DeleteRoleRequest.
	RBACDeleteRoleRequest = rbac.DeleteRoleRequest
	// RBACDeleteRoleResponse is an alias for rbac.DeleteRoleResponse.
	RBACDeleteRoleResponse = rbac.DeleteRoleResponse
	// RBACListRolesRequest is an alias for rbac.ListRolesRequest.
	RBACListRolesRequest = rbac.ListRolesRequest
	// RBACListRolesResponse is an alias for rbac.ListRolesResponse.
	RBACListRolesResponse = rbac.ListRolesResponse
	// RBACRoleInfo is an alias for rbac.RoleInfo.
	RBACRoleInfo = rbac.RoleInfo
	// RBACAssignRoleRequest is an alias for rbac.AssignRoleRequest.
	RBACAssignRoleRequest = rbac.AssignRoleRequest
	// RBACAssignRoleResponse is an alias for rbac.AssignRoleResponse.
	RBACAssignRoleResponse = rbac.AssignRoleResponse
	// RBACRevokeRoleRequest is an alias for rbac.RevokeRoleRequest.
	RBACRevokeRoleRequest = rbac.RevokeRoleRequest
	// RBACRevokeRoleResponse is an alias for rbac.RevokeRoleResponse.
	RBACRevokeRoleResponse = rbac.RevokeRoleResponse
)

// =============================================================================
// DynamicSecrets Client Type Aliases
// =============================================================================

// DynamicSecretsClient is an alias for the dynamicsecrets.Client interface.
type DynamicSecretsClient = dynamicsecrets.Client

// DynamicSecrets type aliases
type (
	// DynamicSecretsRole is an alias for dynamicsecrets.Role.
	DynamicSecretsRole = dynamicsecrets.Role
	// DynamicSecretsCredential is an alias for dynamicsecrets.Credential.
	DynamicSecretsCredential = dynamicsecrets.Credential
	// DynamicSecretsLease is an alias for dynamicsecrets.Lease.
	DynamicSecretsLease = dynamicsecrets.Lease
	// DynamicSecretsCreateRoleRequest is an alias for dynamicsecrets.CreateRoleRequest.
	DynamicSecretsCreateRoleRequest = dynamicsecrets.CreateRoleRequest
	// DynamicSecretsCreateRoleResponse is an alias for dynamicsecrets.CreateRoleResponse.
	DynamicSecretsCreateRoleResponse = dynamicsecrets.CreateRoleResponse
	// DynamicSecretsGetRoleRequest is an alias for dynamicsecrets.GetRoleRequest.
	DynamicSecretsGetRoleRequest = dynamicsecrets.GetRoleRequest
	// DynamicSecretsGetRoleResponse is an alias for dynamicsecrets.GetRoleResponse.
	DynamicSecretsGetRoleResponse = dynamicsecrets.GetRoleResponse
	// DynamicSecretsListRolesRequest is an alias for dynamicsecrets.ListRolesRequest.
	DynamicSecretsListRolesRequest = dynamicsecrets.ListRolesRequest
	// DynamicSecretsListRolesResponse is an alias for dynamicsecrets.ListRolesResponse.
	DynamicSecretsListRolesResponse = dynamicsecrets.ListRolesResponse
	// DynamicSecretsDeleteRoleRequest is an alias for dynamicsecrets.DeleteRoleRequest.
	DynamicSecretsDeleteRoleRequest = dynamicsecrets.DeleteRoleRequest
	// DynamicSecretsDeleteRoleResponse is an alias for dynamicsecrets.DeleteRoleResponse.
	DynamicSecretsDeleteRoleResponse = dynamicsecrets.DeleteRoleResponse
	// GenerateCredentialRequest is an alias for dynamicsecrets.GenerateCredentialRequest.
	GenerateCredentialRequest = dynamicsecrets.GenerateCredentialRequest
	// GenerateCredentialResponse is an alias for dynamicsecrets.GenerateCredentialResponse.
	GenerateCredentialResponse = dynamicsecrets.GenerateCredentialResponse
	// RenewLeaseRequest is an alias for dynamicsecrets.RenewLeaseRequest.
	RenewLeaseRequest = dynamicsecrets.RenewLeaseRequest
	// RenewLeaseResponse is an alias for dynamicsecrets.RenewLeaseResponse.
	RenewLeaseResponse = dynamicsecrets.RenewLeaseResponse
	// RevokeLeaseRequest is an alias for dynamicsecrets.RevokeLeaseRequest.
	RevokeLeaseRequest = dynamicsecrets.RevokeLeaseRequest
	// RevokeLeaseResponse is an alias for dynamicsecrets.RevokeLeaseResponse.
	RevokeLeaseResponse = dynamicsecrets.RevokeLeaseResponse
	// ListLeasesRequest is an alias for dynamicsecrets.ListLeasesRequest.
	ListLeasesRequest = dynamicsecrets.ListLeasesRequest
	// ListLeasesResponse is an alias for dynamicsecrets.ListLeasesResponse.
	ListLeasesResponse = dynamicsecrets.ListLeasesResponse
)

// =============================================================================
// Certificate Client Type Aliases
// =============================================================================

// CertificateClient is an alias for the certificate.Client interface.
type CertificateClient = certificate.Client

// Certificate request/response type aliases
type (
	// IssueCertificateRequest is an alias for certificate.IssueCertificateRequest.
	IssueCertificateRequest = certificate.IssueCertificateRequest
	// IssueCertificateResponse is an alias for certificate.IssueCertificateResponse.
	IssueCertificateResponse = certificate.IssueCertificateResponse
	// SignCSRRequest is an alias for certificate.SignCSRRequest.
	SignCSRRequest = certificate.SignCSRRequest
	// SignCSRResponse is an alias for certificate.SignCSRResponse.
	SignCSRResponse = certificate.SignCSRResponse
	// RevokeCertificateRequest is an alias for certificate.RevokeCertificateRequest.
	RevokeCertificateRequest = certificate.RevokeCertificateRequest
	// RevokeCertificateResponse is an alias for certificate.RevokeCertificateResponse.
	RevokeCertificateResponse = certificate.RevokeCertificateResponse
	// VerifyCertificateRequest is an alias for certificate.VerifyCertificateRequest.
	VerifyCertificateRequest = certificate.VerifyCertificateRequest
	// VerifyCertificateResponse is an alias for certificate.VerifyCertificateResponse.
	VerifyCertificateResponse = certificate.VerifyCertificateResponse
	// GetCACertificateRequest is an alias for certificate.GetCACertificateRequest.
	GetCACertificateRequest = certificate.GetCACertificateRequest
	// GetCACertificateResponse is an alias for certificate.GetCACertificateResponse.
	GetCACertificateResponse = certificate.GetCACertificateResponse
	// GetTLSConfigRequest is an alias for certificate.GetTLSConfigRequest.
	GetTLSConfigRequest = certificate.GetTLSConfigRequest
	// GetTLSConfigResponse is an alias for certificate.GetTLSConfigResponse.
	GetTLSConfigResponse = certificate.GetTLSConfigResponse
)

// DEKClient is an alias for the dek.Client interface.
type DEKClient = dek.Client

// DEK request/response type aliases

// DEKInfo is an alias for dek.DEKInfo.
type DEKInfo = dek.DEKInfo

// CreateDEKRequest is an alias for dek.CreateDEKRequest.
type CreateDEKSDKRequest = dek.CreateDEKRequest

// CreateDEKResponse is an alias for dek.CreateDEKResponse.
type CreateDEKSDKResponse = dek.CreateDEKResponse

// ListDEKsRequest is an alias for dek.ListDEKsRequest.
type ListDEKsSDKRequest = dek.ListDEKsRequest

// ListDEKsResponse is an alias for dek.ListDEKsResponse.
type ListDEKsSDKResponse = dek.ListDEKsResponse

// RotateDEKRequest is an alias for dek.RotateDEKRequest.
type RotateDEKSDKRequest = dek.RotateDEKRequest

// RotateDEKResponse is an alias for dek.RotateDEKResponse.
type RotateDEKSDKResponse = dek.RotateDEKResponse

// DeleteDEKRequest is an alias for dek.DeleteDEKRequest.
type DeleteDEKSDKRequest = dek.DeleteDEKRequest

// DeleteDEKResponse is an alias for dek.DeleteDEKResponse.
type DeleteDEKSDKResponse = dek.DeleteDEKResponse

// DEKStatusResponse is an alias for dek.DEKStatusResponse.
type DEKStatusResponse = dek.DEKStatusResponse

// =============================================================================
// ObjectStorage Client Type Aliases
// =============================================================================

// ObjectStorageClient is an alias for the objectstorage.Client interface.
type ObjectStorageClient = objectstorage.Client

// ObjectStorage request/response type aliases
type (
	// ObjectPutRequest is an alias for objectstorage.PutObjectRequest.
	ObjectPutRequest = objectstorage.PutObjectRequest
	// ObjectPutResponse is an alias for objectstorage.PutObjectResponse.
	ObjectPutResponse = objectstorage.PutObjectResponse
	// ObjectGetRequest is an alias for objectstorage.GetObjectRequest.
	ObjectGetRequest = objectstorage.GetObjectRequest
	// ObjectGetResponse is an alias for objectstorage.GetObjectResponse.
	ObjectGetResponse = objectstorage.GetObjectResponse
	// ObjectDeleteRequest is an alias for objectstorage.DeleteObjectRequest.
	ObjectDeleteRequest = objectstorage.DeleteObjectRequest
	// ObjectDeleteResponse is an alias for objectstorage.DeleteObjectResponse.
	ObjectDeleteResponse = objectstorage.DeleteObjectResponse
	// ObjectHeadRequest is an alias for objectstorage.HeadObjectRequest.
	ObjectHeadRequest = objectstorage.HeadObjectRequest
	// ObjectHeadResponse is an alias for objectstorage.HeadObjectResponse.
	ObjectHeadResponse = objectstorage.HeadObjectResponse
	// ObjectListRequest is an alias for objectstorage.ListObjectsRequest.
	ObjectListRequest = objectstorage.ListObjectsRequest
	// ObjectListResponse is an alias for objectstorage.ListObjectsResponse.
	ObjectListResponse = objectstorage.ListObjectsResponse
	// ObjectSummary is an alias for objectstorage.ObjectSummary.
	ObjectSummary = objectstorage.ObjectSummary
	// ReplicationPolicy is an alias for objectstorage.ReplicationPolicy.
	ReplicationPolicy = objectstorage.ReplicationPolicy
	// ArchivePolicy is an alias for objectstorage.ArchivePolicy.
	ArchivePolicy = objectstorage.ArchivePolicy
	// RetentionPolicy is an alias for objectstorage.RetentionPolicy.
	RetentionPolicy = objectstorage.RetentionPolicy
	// BackendInfoObject is an alias for objectstorage.BackendInfo.
	BackendInfoObject = objectstorage.BackendInfo
)

// ReplicationMode is an alias for objectstorage.ReplicationMode.
type ReplicationMode = objectstorage.ReplicationMode

// Replication mode constants
const (
	ReplicationModeSync   = objectstorage.ReplicationModeSync
	ReplicationModeAsync  = objectstorage.ReplicationModeAsync
	ReplicationModeQuorum = objectstorage.ReplicationModeQuorum
)

// =============================================================================
// ObjectStorageAdmin Client Type Aliases
// =============================================================================

// ObjectStorageAdminClient is an alias for the objectstorageadmin.Client interface.
type ObjectStorageAdminClient = objectstorageadmin.Client

// ObjectStorageAdmin request/response type aliases
type (
	// AdminCreateBackendRequest is an alias for objectstorageadmin.CreateBackendRequest.
	AdminCreateBackendRequest = objectstorageadmin.CreateBackendRequest
	// AdminCreateBackendResponse is an alias for objectstorageadmin.CreateBackendResponse.
	AdminCreateBackendResponse = objectstorageadmin.CreateBackendResponse
	// AdminUpdateBackendRequest is an alias for objectstorageadmin.UpdateBackendRequest.
	AdminUpdateBackendRequest = objectstorageadmin.UpdateBackendRequest
	// AdminUpdateBackendResponse is an alias for objectstorageadmin.UpdateBackendResponse.
	AdminUpdateBackendResponse = objectstorageadmin.UpdateBackendResponse
	// AdminDeleteBackendRequest is an alias for objectstorageadmin.DeleteBackendRequest.
	AdminDeleteBackendRequest = objectstorageadmin.DeleteBackendRequest
	// AdminDeleteBackendResponse is an alias for objectstorageadmin.DeleteBackendResponse.
	AdminDeleteBackendResponse = objectstorageadmin.DeleteBackendResponse
	// AdminSetBackendStatusRequest is an alias for objectstorageadmin.SetBackendStatusRequest.
	AdminSetBackendStatusRequest = objectstorageadmin.SetBackendStatusRequest
	// AdminSetBackendStatusResponse is an alias for objectstorageadmin.SetBackendStatusResponse.
	AdminSetBackendStatusResponse = objectstorageadmin.SetBackendStatusResponse
	// AdminGetStorageStatsRequest is an alias for objectstorageadmin.GetStorageStatsRequest.
	AdminGetStorageStatsRequest = objectstorageadmin.GetStorageStatsRequest
	// AdminGetStorageStatsResponse is an alias for objectstorageadmin.GetStorageStatsResponse.
	AdminGetStorageStatsResponse = objectstorageadmin.GetStorageStatsResponse
	// AdminRunGarbageCollectionRequest is an alias for objectstorageadmin.RunGarbageCollectionRequest.
	AdminRunGarbageCollectionRequest = objectstorageadmin.RunGarbageCollectionRequest
	// AdminRunGarbageCollectionResponse is an alias for objectstorageadmin.RunGarbageCollectionResponse.
	AdminRunGarbageCollectionResponse = objectstorageadmin.RunGarbageCollectionResponse
	// AdminGetGarbageCollectionProgressRequest is an alias for objectstorageadmin.GetGarbageCollectionProgressRequest.
	AdminGetGarbageCollectionProgressRequest = objectstorageadmin.GetGarbageCollectionProgressRequest
	// AdminGetGarbageCollectionProgressResponse is an alias for objectstorageadmin.GetGarbageCollectionProgressResponse.
	AdminGetGarbageCollectionProgressResponse = objectstorageadmin.GetGarbageCollectionProgressResponse
	// AdminRepairObjectRequest is an alias for objectstorageadmin.RepairObjectRequest.
	AdminRepairObjectRequest = objectstorageadmin.RepairObjectRequest
	// AdminRepairObjectResponse is an alias for objectstorageadmin.RepairObjectResponse.
	AdminRepairObjectResponse = objectstorageadmin.RepairObjectResponse
	// AdminCompactBackendRequest is an alias for objectstorageadmin.CompactBackendRequest.
	AdminCompactBackendRequest = objectstorageadmin.CompactBackendRequest
	// AdminCompactBackendResponse is an alias for objectstorageadmin.CompactBackendResponse.
	AdminCompactBackendResponse = objectstorageadmin.CompactBackendResponse
	// AdminBackendInfo is an alias for objectstorageadmin.BackendInfo.
	AdminBackendInfo = objectstorageadmin.BackendInfo
	// AdminTenantUsage is an alias for objectstorageadmin.TenantUsage.
	AdminTenantUsage = objectstorageadmin.TenantUsage
)

// AdminBackendType is an alias for objectstorageadmin.BackendType.
type AdminBackendType = objectstorageadmin.BackendType

// Admin backend type constants
const (
	AdminBackendTypeUnspecified = objectstorageadmin.BackendTypeUnspecified
	AdminBackendTypeLocal       = objectstorageadmin.BackendTypeLocal
	AdminBackendTypeS3          = objectstorageadmin.BackendTypeS3
	AdminBackendTypeGCS         = objectstorageadmin.BackendTypeGCS
	AdminBackendTypeAzure       = objectstorageadmin.BackendTypeAzure
	AdminBackendTypeMinio       = objectstorageadmin.BackendTypeMinio
	AdminBackendTypePebble      = objectstorageadmin.BackendTypePebble
	AdminBackendTypeRaft        = objectstorageadmin.BackendTypeRaft
)

// AdminStorageTier is an alias for objectstorageadmin.StorageTier.
type AdminStorageTier = objectstorageadmin.StorageTier

// Admin storage tier constants
const (
	AdminStorageTierUnspecified = objectstorageadmin.StorageTierUnspecified
	AdminStorageTierHot         = objectstorageadmin.StorageTierHot
	AdminStorageTierWarm        = objectstorageadmin.StorageTierWarm
	AdminStorageTierCold        = objectstorageadmin.StorageTierCold
	AdminStorageTierArchive     = objectstorageadmin.StorageTierArchive
)

// AdminGarbageCollectionStatus is an alias for objectstorageadmin.GarbageCollectionStatus.
type AdminGarbageCollectionStatus = objectstorageadmin.GarbageCollectionStatus

// Admin garbage collection status constants
const (
	AdminGCStatusRunning   = objectstorageadmin.GCStatusRunning
	AdminGCStatusCompleted = objectstorageadmin.GCStatusCompleted
	AdminGCStatusFailed    = objectstorageadmin.GCStatusFailed
	AdminGCStatusCanceled  = objectstorageadmin.GCStatusCanceled
)

// =============================================================================
// ObjectTransfer Client Type Aliases
// =============================================================================

// ObjectTransferClient is an alias for the objecttransfer.Client interface.
type ObjectTransferClient = objecttransfer.Client

// ObjectTransfer request/response type aliases
type (
	// FetchObjectRequest is an alias for objecttransfer.FetchObjectRequest.
	FetchObjectRequest = objecttransfer.FetchObjectRequest
	// FetchObjectResponse is an alias for objecttransfer.FetchObjectResponse.
	FetchObjectResponse = objecttransfer.FetchObjectResponse
	// StreamFetchObjectsRequest is an alias for objecttransfer.StreamFetchObjectsRequest.
	StreamFetchObjectsRequest = objecttransfer.StreamFetchObjectsRequest
	// ObjectFetchItem is an alias for objecttransfer.ObjectFetchItem.
	ObjectFetchItem = objecttransfer.ObjectFetchItem
)

// CompressionAlgo is an alias for objecttransfer.CompressionAlgo.
type CompressionAlgo = objecttransfer.CompressionAlgo

// Compression algorithm constants
const (
	CompressionNone   = objecttransfer.CompressionNone
	CompressionLZ4    = objecttransfer.CompressionLZ4
	CompressionZstd   = objecttransfer.CompressionZstd
	CompressionSnappy = objecttransfer.CompressionSnappy
	CompressionGzip   = objecttransfer.CompressionGzip
)

// =============================================================================
// ParameterStore Client Type Aliases
// =============================================================================

// ParameterStoreClient is an alias for the parameterstore.Client interface.
type ParameterStoreClient = parameterstore.Client

// ParameterStore request/response type aliases
type (
	// GetParameterRequest is an alias for parameterstore.GetParameterRequest.
	GetParameterRequest = parameterstore.GetParameterRequest
	// GetParameterResponse is an alias for parameterstore.GetParameterResponse.
	GetParameterResponse = parameterstore.GetParameterResponse
	// SetParameterRequest is an alias for parameterstore.SetParameterRequest.
	SetParameterRequest = parameterstore.SetParameterRequest
	// SetParameterResponse is an alias for parameterstore.SetParameterResponse.
	SetParameterResponse = parameterstore.SetParameterResponse
	// DeleteParameterRequest is an alias for parameterstore.DeleteParameterRequest.
	DeleteParameterRequest = parameterstore.DeleteParameterRequest
	// DeleteParameterResponse is an alias for parameterstore.DeleteParameterResponse.
	DeleteParameterResponse = parameterstore.DeleteParameterResponse
	// GetParametersRequest is an alias for parameterstore.GetParametersRequest.
	GetParametersRequest = parameterstore.GetParametersRequest
	// GetParametersResponse is an alias for parameterstore.GetParametersResponse.
	GetParametersResponse = parameterstore.GetParametersResponse
	// SetParametersRequest is an alias for parameterstore.SetParametersRequest.
	SetParametersRequest = parameterstore.SetParametersRequest
	// SetParametersResponse is an alias for parameterstore.SetParametersResponse.
	SetParametersResponse = parameterstore.SetParametersResponse
	// DeleteParametersRequest is an alias for parameterstore.DeleteParametersRequest.
	DeleteParametersRequest = parameterstore.DeleteParametersRequest
	// DeleteParametersResponse is an alias for parameterstore.DeleteParametersResponse.
	DeleteParametersResponse = parameterstore.DeleteParametersResponse
	// ListParametersRequest is an alias for parameterstore.ListParametersRequest.
	ListParametersRequest = parameterstore.ListParametersRequest
	// ListParametersResponse is an alias for parameterstore.ListParametersResponse.
	ListParametersResponse = parameterstore.ListParametersResponse
	// SearchByTagsRequest is an alias for parameterstore.SearchByTagsRequest.
	SearchByTagsRequest = parameterstore.SearchByTagsRequest
	// SearchByTagsResponse is an alias for parameterstore.SearchByTagsResponse.
	SearchByTagsResponse = parameterstore.SearchByTagsResponse
	// ParameterValue is an alias for parameterstore.ParameterValue.
	ParameterValue = parameterstore.ParameterValue
)

// =============================================================================
// Scheduler Client Type Aliases
// =============================================================================

// SchedulerClient is an alias for the scheduler.Client interface.
type SchedulerClient = scheduler.Client

// Scheduler request/response type aliases
type (
	// SchedulerJobInfo is an alias for scheduler.JobInfo.
	SchedulerJobInfo = scheduler.JobInfo
	// SchedulerCreateJobRequest is an alias for scheduler.CreateJobRequest.
	SchedulerCreateJobRequest = scheduler.CreateJobRequest
	// SchedulerCreateJobResponse is an alias for scheduler.CreateJobResponse.
	SchedulerCreateJobResponse = scheduler.CreateJobResponse
	// SchedulerUpdateJobRequest is an alias for scheduler.UpdateJobRequest.
	SchedulerUpdateJobRequest = scheduler.UpdateJobRequest
	// SchedulerListJobsOptions is an alias for scheduler.ListJobsOptions.
	SchedulerListJobsOptions = scheduler.ListJobsOptions
	// SchedulerListJobsResponse is an alias for scheduler.ListJobsResponse.
	SchedulerListJobsResponse = scheduler.ListJobsResponse
	// SchedulerTriggerJobResponse is an alias for scheduler.TriggerJobResponse.
	SchedulerTriggerJobResponse = scheduler.TriggerJobResponse
	// SchedulerPauseJobResponse is an alias for scheduler.PauseJobResponse.
	SchedulerPauseJobResponse = scheduler.PauseJobResponse
	// SchedulerResumeJobResponse is an alias for scheduler.ResumeJobResponse.
	SchedulerResumeJobResponse = scheduler.ResumeJobResponse
	// SchedulerCancelJobResponse is an alias for scheduler.CancelJobResponse.
	SchedulerCancelJobResponse = scheduler.CancelJobResponse
	// SchedulerHistoryOptions is an alias for scheduler.HistoryOptions.
	SchedulerHistoryOptions = scheduler.HistoryOptions
	// SchedulerJobHistoryResponse is an alias for scheduler.JobHistoryResponse.
	SchedulerJobHistoryResponse = scheduler.JobHistoryResponse
	// SchedulerJobExecution is an alias for scheduler.JobExecution.
	SchedulerJobExecution = scheduler.JobExecution
	// SchedulerStatus is an alias for scheduler.SchedulerStatus.
	SchedulerStatus = scheduler.SchedulerStatus
)

// SchedulerJobType is an alias for scheduler.JobType.
type SchedulerJobType = scheduler.JobType

// Scheduler job type constants
const (
	SchedulerJobTypeOneShot   = scheduler.JobTypeOneShot
	SchedulerJobTypeRecurring = scheduler.JobTypeRecurring
	SchedulerJobTypeDelayed   = scheduler.JobTypeDelayed
)

// SchedulerJobStatus is an alias for scheduler.JobStatus.
type SchedulerJobStatus = scheduler.JobStatus

// Scheduler job status constants
const (
	SchedulerJobStatusPending   = scheduler.JobStatusPending
	SchedulerJobStatusRunning   = scheduler.JobStatusRunning
	SchedulerJobStatusCompleted = scheduler.JobStatusCompleted
	SchedulerJobStatusFailed    = scheduler.JobStatusFailed
	SchedulerJobStatusPaused    = scheduler.JobStatusPaused
	SchedulerJobStatusCanceled  = scheduler.JobStatusCanceled
)

// SchedulerOwnerType is an alias for scheduler.OwnerType.
type SchedulerOwnerType = scheduler.OwnerType

// Scheduler owner type constants
const (
	SchedulerOwnerTypeSystem    = scheduler.OwnerTypeSystem
	SchedulerOwnerTypeTenant    = scheduler.OwnerTypeTenant
	SchedulerOwnerTypeDelegated = scheduler.OwnerTypeDelegated
)

// =============================================================================
// Messaging Client Type Aliases
// =============================================================================

// MessagingClient is an alias for the messaging.Client interface.
type MessagingClient = messaging.Client

// Messaging type aliases
type (
	// MessagingChannel is an alias for messaging.Channel.
	MessagingChannel = messaging.Channel
	// MessagingSubscription is an alias for messaging.Subscription.
	MessagingSubscription = messaging.Subscription
	// MessagingMessage is an alias for messaging.Message.
	MessagingMessage = messaging.Message
	// MessagingDeltaPayload is an alias for messaging.DeltaPayload.
	MessagingDeltaPayload = messaging.DeltaPayload
	// MessagingFieldChange is an alias for messaging.FieldChange.
	MessagingFieldChange = messaging.FieldChange
	// MessagingStats is an alias for messaging.Stats.
	MessagingStats = messaging.Stats
	// MessagingDedupStats is an alias for messaging.DedupStats.
	MessagingDedupStats = messaging.DedupStats
	// MessagingChannelStats is an alias for messaging.ChannelStats.
	MessagingChannelStats = messaging.ChannelStats
	// MessagingSubscriptionFilter is an alias for messaging.SubscriptionFilter.
	MessagingSubscriptionFilter = messaging.SubscriptionFilter
	// MessagingDedupInfo is an alias for messaging.DedupInfo.
	MessagingDedupInfo = messaging.DedupInfo
	// MessagingCreateChannelRequest is an alias for messaging.CreateChannelRequest.
	MessagingCreateChannelRequest = messaging.CreateChannelRequest
	// MessagingSubscribeRequest is an alias for messaging.SubscribeRequest.
	MessagingSubscribeRequest = messaging.SubscribeRequest
	// MessagingPublishRequest is an alias for messaging.PublishRequest.
	MessagingPublishRequest = messaging.PublishRequest
	// MessagingPublishDeltaRequest is an alias for messaging.PublishDeltaRequest.
	MessagingPublishDeltaRequest = messaging.PublishDeltaRequest
	// MessagingWatchChannelRequest is an alias for messaging.WatchChannelRequest.
	MessagingWatchChannelRequest = messaging.WatchChannelRequest
	// MessagingListChannelsResponse is an alias for messaging.ListChannelsResponse.
	MessagingListChannelsResponse = messaging.ListChannelsResponse
)

// MessagingEventType is an alias for messaging.EventType.
type MessagingEventType = messaging.EventType

// Messaging event type constants
const (
	MessagingEventTypePut    = messaging.EventTypePut
	MessagingEventTypeDelete = messaging.EventTypeDelete
)

// MessagingMessageType is an alias for messaging.MessageType.
type MessagingMessageType = messaging.MessageType

// Messaging message type constants
const (
	MessagingMessageTypeSubscribe    = messaging.MessageTypeSubscribe
	MessagingMessageTypeUnsubscribe  = messaging.MessageTypeUnsubscribe
	MessagingMessageTypePublish      = messaging.MessageTypePublish
	MessagingMessageTypeNotification = messaging.MessageTypeNotification
	MessagingMessageTypePing         = messaging.MessageTypePing
	MessagingMessageTypePong         = messaging.MessageTypePong
	MessagingMessageTypeAck          = messaging.MessageTypeAck
	MessagingMessageTypeError        = messaging.MessageTypeError
)

// MessagingOpType is an alias for messaging.OpType.
type MessagingOpType = messaging.OpType

// Messaging operation type constants
const (
	MessagingOpSet       = messaging.OpSet
	MessagingOpDelete    = messaging.OpDelete
	MessagingOpAppend    = messaging.OpAppend
	MessagingOpIncrement = messaging.OpIncrement
)

// Messaging codec constants
const (
	MessagingCodecJSON     = messaging.CodecJSON
	MessagingCodecMsgpack  = messaging.CodecMsgpack
	MessagingCodecProtobuf = messaging.CodecProtobuf
)

// =============================================================================
// Embedding Client Type Aliases
// =============================================================================

// EmbeddingClient is an alias for the embedding.Client interface.
type EmbeddingClient = embedding.Client

// Embedding request/response type aliases
type (
	// EmbeddingGenerateRequest is an alias for embedding.GenerateEmbeddingRequest.
	EmbeddingGenerateRequest = embedding.GenerateEmbeddingRequest
	// EmbeddingGenerateResponse is an alias for embedding.GenerateEmbeddingResponse.
	EmbeddingGenerateResponse = embedding.GenerateEmbeddingResponse
	// BatchEmbeddingItem is an alias for embedding.BatchEmbeddingItem.
	BatchEmbeddingItem = embedding.BatchEmbeddingItem
	// BatchEmbeddingRequest is an alias for embedding.BatchGenerateEmbeddingsRequest.
	BatchEmbeddingRequest = embedding.BatchGenerateEmbeddingsRequest
	// BatchEmbeddingResult is an alias for embedding.BatchEmbeddingResult.
	BatchEmbeddingResult = embedding.BatchEmbeddingResult
	// BatchEmbeddingResponse is an alias for embedding.BatchGenerateEmbeddingsResponse.
	BatchEmbeddingResponse = embedding.BatchGenerateEmbeddingsResponse
	// EmbeddingMetadata is an alias for embedding.Metadata.
	EmbeddingMetadata = embedding.Metadata
	// EmbeddingProviderInfo is an alias for embedding.ProviderInfo.
	EmbeddingProviderInfo = embedding.ProviderInfo
)

// EmbeddingProvider is an alias for embedding.Provider.
type EmbeddingProvider = embedding.Provider

// Embedding provider constants
const (
	EmbeddingProviderVoyage   = embedding.ProviderVoyage
	EmbeddingProviderOpenAI   = embedding.ProviderOpenAI
	EmbeddingProviderOllama   = embedding.ProviderOllama
	EmbeddingProviderLMStudio = embedding.ProviderLMStudio

	// EmbeddingProviderNPU selects the Intel NPU via the OpenVINO sidecar.
	EmbeddingProviderNPU = embedding.ProviderNPU
)

// EmbeddingInputType is an alias for embedding.InputType.
type EmbeddingInputType = embedding.InputType

// Embedding input type constants
const (
	EmbeddingInputTypeDocument = embedding.InputTypeDocument
	EmbeddingInputTypeQuery    = embedding.InputTypeQuery
	EmbeddingInputTypeCode     = embedding.InputTypeCode
)

// EmbeddingProviderStatus is an alias for embedding.Status.
type EmbeddingProviderStatus = embedding.Status

// Embedding provider status constants
const (
	EmbeddingStatusAvailable   = embedding.StatusAvailable
	EmbeddingStatusUnavailable = embedding.StatusUnavailable
	EmbeddingStatusRateLimited = embedding.StatusRateLimited
	EmbeddingStatusError       = embedding.StatusError
)

// =============================================================================
// Ollama Client Type Aliases
// =============================================================================

// OllamaClient is an alias for the ollama.Client interface.
type OllamaClient = ollama.Client

// Ollama request/response type aliases
type (
	// OllamaEmbedRequest is an alias for ollama.EmbedRequest.
	OllamaEmbedRequest = ollama.EmbedRequest
	// OllamaEmbedWithModelRequest is an alias for ollama.EmbedWithModelRequest.
	OllamaEmbedWithModelRequest = ollama.EmbedWithModelRequest
	// OllamaEmbedResponse is an alias for ollama.EmbedResponse.
	OllamaEmbedResponse = ollama.EmbedResponse
	// OllamaEmbedBatchRequest is an alias for ollama.EmbedBatchRequest.
	OllamaEmbedBatchRequest = ollama.EmbedBatchRequest
	// OllamaEmbedBatchResponse is an alias for ollama.EmbedBatchResponse.
	OllamaEmbedBatchResponse = ollama.EmbedBatchResponse
	// OllamaListModelsRequest is an alias for ollama.ListModelsRequest.
	OllamaListModelsRequest = ollama.ListModelsRequest
	// OllamaListModelsResponse is an alias for ollama.ListModelsResponse.
	OllamaListModelsResponse = ollama.ListModelsResponse
	// OllamaModelInfo is an alias for ollama.ModelInfo.
	OllamaModelInfo = ollama.ModelInfo
	// OllamaModelDetails is an alias for ollama.ModelDetails.
	OllamaModelDetails = ollama.ModelDetails
	// OllamaGetModelInfoRequest is an alias for ollama.GetModelInfoRequest.
	OllamaGetModelInfoRequest = ollama.GetModelInfoRequest
	// OllamaGetModelInfoResponse is an alias for ollama.GetModelInfoResponse.
	OllamaGetModelInfoResponse = ollama.GetModelInfoResponse
	// OllamaHealthRequest is an alias for ollama.HealthRequest.
	OllamaHealthRequest = ollama.HealthRequest
	// OllamaHealthResponse is an alias for ollama.HealthResponse.
	OllamaHealthResponse = ollama.HealthResponse
)

// =============================================================================
// RAG Client Type Aliases
// =============================================================================

// RAGClient is an alias for the rag.Client interface.
type RAGClient = rag.Client

// RAG request/response type aliases
type (
	// RAGQueryRequest is an alias for rag.QueryRequest.
	RAGQueryRequest = rag.QueryRequest
	// RAGQueryResponse is an alias for rag.QueryResponse.
	RAGQueryResponse = rag.QueryResponse
	// RAGBuildContextRequest is an alias for rag.BuildContextRequest.
	RAGBuildContextRequest = rag.BuildContextRequest
	// RAGBuildContextResponse is an alias for rag.BuildContextResponse.
	RAGBuildContextResponse = rag.BuildContextResponse
	// RAGCitation is an alias for rag.Citation.
	RAGCitation = rag.Citation
	// RAGResultInput is an alias for rag.ResultInput.
	RAGResultInput = rag.ResultInput
)

// =============================================================================
// Orchestrator Client
// =============================================================================

// OrchestratorClient is an alias for the orchestrator.Client interface.
type OrchestratorClient = orchestrator.Client

// =============================================================================
// Project Client Type Aliases
// =============================================================================

// ProjectClient is an alias for the project.Client interface.
type ProjectClient = project.Client

// SDLCClient is an alias for the sdlc.Client interface.
type SDLCClient = sdlc.Client

// AgentsClient is an alias for the agents.Client interface.
type AgentsClient = agents.Client

// =============================================================================
// Account Client Type Aliases (pooled provider accounts)
// =============================================================================

// AccountClient is an alias for the account.Client interface.
type AccountClient = account.Client

type (
	// AccountInfo is an alias for account.AccountInfo.
	AccountInfo = account.AccountInfo

	// AccountCredential is an alias for account.Credential. Request-only:
	// no SDK verb ever returns one.
	AccountCredential = account.Credential

	// AccountProvider is an alias for account.Provider.
	AccountProvider = account.Provider

	// AccountStatus is an alias for account.Status.
	AccountStatus = account.Status

	// AccountRefreshPolicy is an alias for account.RefreshPolicy.
	AccountRefreshPolicy = account.RefreshPolicy

	// CreateAccountRequest is an alias for account.CreateAccountRequest.
	CreateAccountRequest = account.CreateAccountRequest

	// UpdateAccountRequest is an alias for account.UpdateAccountRequest.
	UpdateAccountRequest = account.UpdateAccountRequest

	// ListAccountsRequest is an alias for account.ListAccountsRequest.
	ListAccountsRequest = account.ListAccountsRequest

	// ListAccountsResponse is an alias for account.ListAccountsResponse.
	ListAccountsResponse = account.ListAccountsResponse

	// RotateAccountCredentialRequest is an alias for
	// account.RotateCredentialRequest.
	RotateAccountCredentialRequest = account.RotateCredentialRequest

	// RevokeAccountRequest is an alias for account.RevokeAccountRequest.
	RevokeAccountRequest = account.RevokeAccountRequest

	// AccountUsageReport is an alias for account.UsageReport.
	AccountUsageReport = account.UsageReport

	// MaterializeAccountCredentialRequest is an alias for
	// account.MaterializeCredentialRequest.
	MaterializeAccountCredentialRequest = account.MaterializeCredentialRequest

	// MaterializedAccountCredential is an alias for
	// account.MaterializedCredential — the RELEASED credential envelope plus
	// the metadata a caching caller needs. It is the only account response
	// shape in this SDK that carries secret material.
	MaterializedAccountCredential = account.MaterializedCredential

	// ReleasedAccountCredential is an alias for account.ReleasedCredential:
	// the short-lived half of a pooled credential, with no refresh grant.
	ReleasedAccountCredential = account.ReleasedCredential
)

const (
	// AccountProviderClaude pools Anthropic/Claude Code accounts.
	AccountProviderClaude = account.ProviderClaude

	// AccountProviderCodex pools OpenAI/Codex accounts.
	AccountProviderCodex = account.ProviderCodex

	// AccountProviderOpenCode pools OpenCode provider configurations.
	AccountProviderOpenCode = account.ProviderOpenCode

	// AccountStatusActive marks an account eligible for selection.
	AccountStatusActive = account.StatusActive

	// AccountStatusRevoked marks an account explicitly revoked.
	AccountStatusRevoked = account.StatusRevoked

	// AccountStatusError marks an account whose last refresh failed.
	AccountStatusError = account.StatusError

	// AccountRefreshNone marks a credential that never needs refreshing.
	AccountRefreshNone = account.RefreshNone

	// AccountRefreshOAuth2 marks a credential renewed via an RFC 6749 grant.
	AccountRefreshOAuth2 = account.RefreshOAuth2
)

type (
	// ProjectInfo is an alias for project.ProjectInfo.
	ProjectInfo = project.ProjectInfo

	// ProjectWorkspace is an alias for project.ProjectWorkspace.
	ProjectWorkspace = project.ProjectWorkspace

	// ProjectGit is an alias for project.ProjectGit.
	ProjectGit = project.ProjectGit

	// ProjectPermissions is an alias for project.ProjectPermissions.
	ProjectPermissions = project.ProjectPermissions

	// ProjectQuotas is an alias for project.ProjectQuotas.
	ProjectQuotas = project.ProjectQuotas

	// SpendLimits is an alias for spend.Limits: the sliding token and dollar
	// windows an account, a project and a tenant are metered by.
	SpendLimits = spend.Limits

	// SpendWindow is an alias for spend.Window.
	SpendWindow = spend.Window

	// SpendBreakerPolicy is an alias for spend.BreakerPolicy.
	SpendBreakerPolicy = spend.BreakerPolicy

	// AccountSpendPolicy is an alias for spend.AccountPolicy.
	AccountSpendPolicy = spend.AccountPolicy

	// ProjectRole is an alias for project.ProjectRole.
	ProjectRole = project.ProjectRole

	// CreateProjectRequest is an alias for project.CreateProjectRequest.
	CreateProjectRequest = project.CreateProjectRequest

	// UpdateProjectRequest is an alias for project.UpdateProjectRequest.
	UpdateProjectRequest = project.UpdateProjectRequest

	// ListProjectsResponse is an alias for project.ListProjectsResponse.
	ListProjectsResponse = project.ListProjectsResponse
)

const (
	// ProjectRoleLead gives full control over the project.
	ProjectRoleLead = project.ProjectRoleLead

	// ProjectRoleWorker can execute tasks within the project.
	ProjectRoleWorker = project.ProjectRoleWorker

	// ProjectRoleObserver has read-only access to the project.
	ProjectRoleObserver = project.ProjectRoleObserver

	// ProjectRoleDenied explicitly prohibits access.
	ProjectRoleDenied = project.ProjectRoleDenied
)

// client implements the Client interface.
type client struct {
	transport Transport
	opts      *clientOptions

	mu                       sync.RWMutex
	closed                   bool
	kvClient                 KVClient
	batchClient              BatchClient
	txnClient                TransactionClient
	queryClient              QueryClient
	clusterClient            ClusterClient
	healthClient             HealthClient
	authClient               AuthClient
	sealClient               SealClient
	kmsClient                KMSClient
	secretsClient            SecretsClient
	cryptoClient             CryptoClient
	vectorClient             VectorClient
	tsClient                 TimeSeriesClient
	backupClient             BackupClient
	iamClient                IAMClient
	cacheClient              CacheClient
	operationsClient         OperationsClient
	rbacClient               RBACClient
	dynamicSecretsClient     DynamicSecretsClient
	certificateClient        CertificateClient
	dekClient                DEKClient
	objectStorageClient      ObjectStorageClient
	objectStorageAdminClient ObjectStorageAdminClient
	objectTransferClient     ObjectTransferClient
	parameterStoreClient     ParameterStoreClient
	schedulerClient          SchedulerClient
	messagingClient          MessagingClient
	embeddingClient          EmbeddingClient
	ollamaClient             OllamaClient
	ragClient                RAGClient
	orchestratorClient       OrchestratorClient
	projectClient            ProjectClient
	sdlcClient               SDLCClient
	agentsClient             AgentsClient
	accountClient            AccountClient
	qrdb                     qrdbState
	address                  string
	acmeTransportOnce        sync.Once
	acmeTransportCached      Transport
	acmeTransportErr         error

	// M10 — extended security service (lazily initialized).
	securityOnce sync.Once
	security     *securityService
}

// NewClient creates a new QuicBaaS client connected to the specified address.
//
// The address can be:
//   - TCP address: "localhost:50051"
//   - Unix socket: "/var/run/quicbaas.sock" (auto-detected)
//   - Unix socket (explicit): "unix:///var/run/quicbaas.sock"
//   - Unix abstract: "unix-abstract://quicbaas"
//   - HTTP/HTTPS: "http://localhost:8080" (triggers REST transport)
//
// Example:
//
//	client, err := quicbaas.NewClient("/var/run/quicbaas.sock")
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer client.Close()
func NewClient(address string, opts ...Option) (Client, error) {
	if address == "" {
		return nil, errors.NewInvalidInput("NewClient", "address cannot be empty")
	}

	// Apply default options
	options := defaultOptions()

	// Apply user options
	for _, opt := range opts {
		if err := opt(options); err != nil {
			return nil, err
		}
	}

	// Embedded mode: when the caller did not inject a secret registry, adopt
	// the one the embedded QuicBaaS service exposes. The secrets manager is a
	// core, always-on service (the server builds its resolver registry in every
	// mode); auto-wiring it here means Security().SecretResolver() is reachable
	// through any embedded client without an explicit WithSecretRegistry, so
	// kms:// / credential:// URIs resolve instead of failing closed.
	if options.secretRegistry == nil && options.embeddedService != nil {
		if svc, ok := options.embeddedService.(service.QuicBaaS); ok {
			options.secretRegistry = svc.SecretRegistry()
		}
	}

	// Auto-detect REST transport from HTTP URLs
	if strings.HasPrefix(address, "http://") || strings.HasPrefix(address, "https://") {
		if options.transportType == TransportGRPC {
			options.transportType = TransportREST
		}
	}

	// Create transport based on type
	transport, err := createTransport(address, options)
	if err != nil {
		return nil, err
	}

	c := &client{
		transport: transport,
		opts:      options,
		address:   address,
	}

	return c, nil
}

// resolveTLSConfig returns the effective TLS config based on client options.
// QuicBaaS bans insecure TLS — the only way to get a TLS-configured client is
// via WithTLSConfig with explicit CA trust. The legacy opts.insecure path is
// dead.
func resolveTLSConfig(opts *clientOptions) *tls.Config {
	return opts.tlsConfig
}

// acmeTransport resolves the transport used for ACME (RFC 8555) server-side
// verbs. ACME is HTTP-native: when the client's main transport is REST or the
// in-process embedded transport, that transport already speaks ACME, so it is
// reused. For any other main transport (gRPC / QUIC / MCP / Unix) ACME cannot
// ride the wire protocol, so a dedicated REST transport to the same address is
// lazily constructed and cached. This makes Security().ACME() work regardless
// of the configured main transport without adding ACME opcodes to the other
// protocols.
func (c *client) acmeTransport() Transport {
	// A REST or embedded main transport already speaks ACME; reuse it. A
	// directly-injected transport (no resolvable network address — e.g. via
	// NewClientWithTransport) is also used as-is: there is nothing to build a
	// REST transport from, and the caller explicitly chose that transport.
	if c.opts == nil ||
		c.opts.transportType == TransportREST ||
		c.opts.transportType == TransportEmbedded ||
		c.address == "" {
		return c.transport
	}
	c.acmeTransportOnce.Do(func() {
		t, err := createRESTTransport(c.address, c.opts)
		if err != nil {
			c.acmeTransportErr = err
			return
		}
		c.acmeTransportCached = t
	})
	if c.acmeTransportCached != nil {
		return c.acmeTransportCached
	}
	// Fall back to the main transport when a REST transport could not be
	// built; the ACME service will surface the resulting error on first use.
	return c.transport
}

// createTransport creates the appropriate transport based on options.
func createTransport(address string, opts *clientOptions) (Transport, error) {
	switch opts.transportType {
	case TransportGRPC:
		return createGRPCTransport(address, opts)
	case TransportREST:
		return createRESTTransport(address, opts)
	case TransportQUIC:
		return createQUICTransport(address, opts)
	case TransportUnix:
		return createUnixTransport(opts)
	case TransportEmbedded:
		return createEmbeddedTransport(opts)
	case TransportMCP:
		return createMCPTransport(address, opts)
	default:
		return nil, errors.NewInvalidInput("createTransport", "unsupported transport type")
	}
}

// createGRPCTransport creates a gRPC transport.
func createGRPCTransport(address string, opts *clientOptions) (Transport, error) {
	cfg := &transport.Config{
		Address:          address,
		Timeout:          opts.timeout,
		TLSConfig:        resolveTLSConfig(opts),
		KeepaliveTime:    opts.keepaliveTime,
		KeepaliveTimeout: opts.keepaliveTimeout,
		MaxRetries:       opts.maxRetries,
		RetryBackoff:     opts.retryBackoff,
		MaxRetryBackoff:  opts.maxRetryBackoff,
		PoolEnabled:      opts.poolEnabled,
		PoolMinSize:      opts.poolMinSize,
		PoolMaxSize:      opts.poolMaxSize,
		PoolMaxAge:       opts.poolMaxAge,
		AuthToken:        opts.authToken,
	}

	// Parse the address to determine its type
	parsed := transport.ParseAddress(address)
	cfg.AddressType = parsed.Type

	return grpc.New(cfg)
}

// createRESTTransport creates a REST transport.
func createRESTTransport(address string, opts *clientOptions) (Transport, error) {
	cfg := &transport.Config{
		Address:          address,
		Timeout:          opts.timeout,
		TLSConfig:        resolveTLSConfig(opts),
		KeepaliveTime:    opts.keepaliveTime,
		KeepaliveTimeout: opts.keepaliveTimeout,
		MaxRetries:       opts.maxRetries,
		RetryBackoff:     opts.retryBackoff,
		MaxRetryBackoff:  opts.maxRetryBackoff,
		AuthToken:        opts.authToken,
	}

	// Parse the address to determine its type
	parsed := transport.ParseAddress(address)
	cfg.AddressType = parsed.Type

	var restOpts []rest.Option
	if opts.authToken != "" {
		restOpts = append(restOpts, rest.WithAuthToken(opts.authToken))
	}
	// The per-tenant routes (the WebAuthn ceremony among them) read the tenant
	// from the transport when the request itself does not carry one.
	if opts.tenantID != "" {
		restOpts = append(restOpts, rest.WithDefaultTenant(opts.tenantID))
	}

	return rest.New(cfg, restOpts...)
}

// createQUICTransport creates a QUIC transport.
func createQUICTransport(address string, opts *clientOptions) (Transport, error) {
	cfg := &transport.Config{
		Address:          address,
		Timeout:          opts.timeout,
		TLSConfig:        resolveTLSConfig(opts),
		KeepaliveTime:    opts.keepaliveTime,
		KeepaliveTimeout: opts.keepaliveTimeout,
		MaxRetries:       opts.maxRetries,
		RetryBackoff:     opts.retryBackoff,
		MaxRetryBackoff:  opts.maxRetryBackoff,
		AuthToken:        opts.authToken,
	}

	// Parse the address to determine its type
	parsed := transport.ParseAddress(address)
	cfg.AddressType = parsed.Type

	var quicOpts []quic.Option
	if opts.authToken != "" {
		quicOpts = append(quicOpts, quic.WithAuthToken(opts.authToken))
	}

	return quic.New(cfg, quicOpts...)
}

// createUnixTransport creates a Unix socket transport (gRPC over UDS).
func createUnixTransport(opts *clientOptions) (Transport, error) {
	cfg := &transport.Config{
		Address:          opts.unixSocketPath,
		AddressType:      transport.AddressTypeUnix,
		Timeout:          opts.timeout,
		TLSConfig:        resolveTLSConfig(opts),
		KeepaliveTime:    opts.keepaliveTime,
		KeepaliveTimeout: opts.keepaliveTimeout,
		MaxRetries:       opts.maxRetries,
		RetryBackoff:     opts.retryBackoff,
		MaxRetryBackoff:  opts.maxRetryBackoff,
		AuthToken:        opts.authToken,
	}

	return unix.New(cfg)
}

// createEmbeddedTransport creates an embedded in-process transport.
func createEmbeddedTransport(opts *clientOptions) (Transport, error) {
	// Type assert the embedded service to service.QuicBaaS
	svc, ok := opts.embeddedService.(service.QuicBaaS)
	if !ok {
		return nil, errors.NewInvalidInput("createEmbeddedTransport",
			"embedded service must implement service.QuicBaaS interface")
	}

	return embedded.New(svc)
}

// createMCPTransport creates an MCP (Model Context Protocol) transport.
func createMCPTransport(address string, opts *clientOptions) (Transport, error) {
	cfg := &transport.Config{
		Address:          address,
		Timeout:          opts.timeout,
		TLSConfig:        resolveTLSConfig(opts),
		KeepaliveTime:    opts.keepaliveTime,
		KeepaliveTimeout: opts.keepaliveTimeout,
		MaxRetries:       opts.maxRetries,
		RetryBackoff:     opts.retryBackoff,
		MaxRetryBackoff:  opts.maxRetryBackoff,
	}

	// Parse the address to determine its type
	parsed := transport.ParseAddress(address)
	cfg.AddressType = parsed.Type

	// Build MCP transport options
	var mcpOpts []mcp.Option
	if opts.mcpAuthToken != "" {
		// Use the MCP-specific token if provided.
		mcpOpts = append(mcpOpts, mcp.WithAuthToken(opts.mcpAuthToken))
	} else if opts.authToken != "" {
		// Fall back to the transport-agnostic platform token.
		mcpOpts = append(mcpOpts, mcp.WithAuthToken(opts.authToken))
	} else if opts.mcpUsername != "" && opts.mcpPassword != "" {
		// Use credentials for auto-authentication
		mcpOpts = append(mcpOpts, mcp.WithCredentials(opts.mcpUsername, opts.mcpPassword))
	}

	return mcp.New(cfg, mcpOpts...)
}

// Transport returns the underlying transport.
func (c *client) Transport() Transport {
	return c.transport
}

// Close closes the client and releases all resources.
func (c *client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil
	}
	c.closed = true

	if c.transport != nil {
		return c.transport.Close()
	}
	return nil
}

// KV returns the KV client.
func (c *client) KV() KVClient {
	c.mu.RLock()
	if c.kvClient != nil {
		c.mu.RUnlock()
		return c.kvClient
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.kvClient == nil {
		c.kvClient = newKVClient(c.transport)
	}
	return c.kvClient
}

// Batch returns the Batch client.
func (c *client) Batch() BatchClient {
	c.mu.RLock()
	if c.batchClient != nil {
		c.mu.RUnlock()
		return c.batchClient
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.batchClient == nil {
		c.batchClient = newBatchClient(c.transport)
	}
	return c.batchClient
}

// Transaction returns the Transaction client.
func (c *client) Transaction() TransactionClient {
	c.mu.RLock()
	if c.txnClient != nil {
		c.mu.RUnlock()
		return c.txnClient
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.txnClient == nil {
		c.txnClient = newTransactionClient(c.transport)
	}
	return c.txnClient
}

// Query returns the Query client.
func (c *client) Query() QueryClient {
	c.mu.RLock()
	if c.queryClient != nil {
		c.mu.RUnlock()
		return c.queryClient
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.queryClient == nil {
		c.queryClient = newQueryClient(c.transport)
	}
	return c.queryClient
}

// Cluster returns the Cluster client.
func (c *client) Cluster() ClusterClient {
	c.mu.RLock()
	if c.clusterClient != nil {
		c.mu.RUnlock()
		return c.clusterClient
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.clusterClient == nil {
		c.clusterClient = newClusterClient(c.transport)
	}
	return c.clusterClient
}

// Health returns the Health client.
func (c *client) Health() HealthClient {
	c.mu.RLock()
	if c.healthClient != nil {
		c.mu.RUnlock()
		return c.healthClient
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.healthClient == nil {
		c.healthClient = newHealthClient(c.transport)
	}
	return c.healthClient
}

// auth returns the Auth client. Reachable via Security().Auth().
func (c *client) auth() AuthClient {
	c.mu.RLock()
	if c.authClient != nil {
		c.mu.RUnlock()
		return c.authClient
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.authClient == nil {
		c.authClient = newAuthClient(c.transport)
	}
	return c.authClient
}

// seal returns the Seal client. Reachable via Security().Seal().
func (c *client) seal() SealClient {
	c.mu.RLock()
	if c.sealClient != nil {
		c.mu.RUnlock()
		return c.sealClient
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.sealClient == nil {
		c.sealClient = newSealClient(c.transport)
	}
	return c.sealClient
}

// kms returns the KMS client. Reachable via Security().KMS().
func (c *client) kms() KMSClient {
	c.mu.RLock()
	if c.kmsClient != nil {
		c.mu.RUnlock()
		return c.kmsClient
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.kmsClient == nil {
		c.kmsClient = newKMSClient(c.transport)
	}
	return c.kmsClient
}

// secrets returns the Secrets client. Reachable via Security().Secrets().
func (c *client) secrets() SecretsClient {
	c.mu.RLock()
	if c.secretsClient != nil {
		c.mu.RUnlock()
		return c.secretsClient
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.secretsClient == nil {
		c.secretsClient = newSecretsClient(c.transport)
	}
	return c.secretsClient
}

// crypto returns the Crypto client. Reachable via Security().Crypto().
func (c *client) crypto() CryptoClient {
	c.mu.RLock()
	if c.cryptoClient != nil {
		c.mu.RUnlock()
		return c.cryptoClient
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cryptoClient == nil {
		c.cryptoClient = newCryptoClient(c.transport)
	}
	return c.cryptoClient
}

// Vector returns the Vector client.
func (c *client) Vector() VectorClient {
	c.mu.RLock()
	if c.vectorClient != nil {
		c.mu.RUnlock()
		return c.vectorClient
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.vectorClient == nil {
		c.vectorClient = newVectorClient(c.transport)
	}
	return c.vectorClient
}

// TimeSeries returns the TimeSeries client.
func (c *client) TimeSeries() TimeSeriesClient {
	c.mu.RLock()
	if c.tsClient != nil {
		c.mu.RUnlock()
		return c.tsClient
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.tsClient == nil {
		c.tsClient = newTimeSeriesClient(c.transport)
	}
	return c.tsClient
}

// Backup returns the Backup client.
func (c *client) Backup() BackupClient {
	c.mu.RLock()
	if c.backupClient != nil {
		c.mu.RUnlock()
		return c.backupClient
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.backupClient == nil {
		c.backupClient = newBackupClient(c.transport)
	}
	return c.backupClient
}

// iam returns the IAM client for identity and access management. Reachable via
// Identity().IAM().
func (c *client) iam() IAMClient {
	c.mu.RLock()
	if c.iamClient != nil {
		c.mu.RUnlock()
		return c.iamClient
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.iamClient == nil {
		c.iamClient = newIAMClient(c.transport)
	}
	return c.iamClient
}

// Cache returns the Cache client for cache management.
func (c *client) Cache() CacheClient {
	c.mu.RLock()
	if c.cacheClient != nil {
		c.mu.RUnlock()
		return c.cacheClient
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cacheClient == nil {
		c.cacheClient = newCacheClient(c.transport)
	}
	return c.cacheClient
}

// Operations returns the Operations client for cluster operations.
func (c *client) Operations() OperationsClient {
	c.mu.RLock()
	if c.operationsClient != nil {
		c.mu.RUnlock()
		return c.operationsClient
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.operationsClient == nil {
		c.operationsClient = newOperationsClient(c.transport)
	}
	return c.operationsClient
}

// rbac returns the RBAC client. Reachable via Security().RBAC().
func (c *client) rbac() RBACClient {
	c.mu.RLock()
	if c.rbacClient != nil {
		c.mu.RUnlock()
		return c.rbacClient
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.rbacClient == nil {
		c.rbacClient = newRBACClient(c.transport)
	}
	return c.rbacClient
}

// dynamicSecrets returns the DynamicSecrets client. Reachable via Security().DynamicSecrets().
func (c *client) dynamicSecrets() DynamicSecretsClient {
	c.mu.RLock()
	if c.dynamicSecretsClient != nil {
		c.mu.RUnlock()
		return c.dynamicSecretsClient
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.dynamicSecretsClient == nil {
		c.dynamicSecretsClient = newDynamicSecretsClient(c.transport)
	}
	return c.dynamicSecretsClient
}

// certificate returns the Certificate client. Reachable via Security().Certificate().
func (c *client) certificate() CertificateClient {
	c.mu.RLock()
	if c.certificateClient != nil {
		c.mu.RUnlock()
		return c.certificateClient
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.certificateClient == nil {
		c.certificateClient = newCertificateClient(c.transport)
	}
	return c.certificateClient
}

// dek returns the DEK client. Reachable via Security().DEK().
func (c *client) dek() DEKClient {
	c.mu.RLock()
	if c.dekClient != nil {
		c.mu.RUnlock()
		return c.dekClient
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.dekClient == nil {
		c.dekClient = newDEKClient(c.transport)
	}
	return c.dekClient
}

// ObjectStorage returns the ObjectStorage client for object storage operations.
func (c *client) ObjectStorage() ObjectStorageClient {
	c.mu.RLock()
	if c.objectStorageClient != nil {
		c.mu.RUnlock()
		return c.objectStorageClient
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.objectStorageClient == nil {
		c.objectStorageClient = newObjectStorageClient(c.transport)
	}
	return c.objectStorageClient
}

// ObjectStorageAdmin returns the ObjectStorageAdmin client for admin operations.
func (c *client) ObjectStorageAdmin() ObjectStorageAdminClient {
	c.mu.RLock()
	if c.objectStorageAdminClient != nil {
		c.mu.RUnlock()
		return c.objectStorageAdminClient
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.objectStorageAdminClient == nil {
		c.objectStorageAdminClient = newObjectStorageAdminClient(c.transport)
	}
	return c.objectStorageAdminClient
}

// ObjectTransfer returns the ObjectTransfer client for object transfer operations.
func (c *client) ObjectTransfer() ObjectTransferClient {
	c.mu.RLock()
	if c.objectTransferClient != nil {
		c.mu.RUnlock()
		return c.objectTransferClient
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.objectTransferClient == nil {
		c.objectTransferClient = newObjectTransferClient(c.transport)
	}
	return c.objectTransferClient
}

// ParameterStore returns the ParameterStore client for parameter management.
func (c *client) ParameterStore() ParameterStoreClient {
	c.mu.RLock()
	if c.parameterStoreClient != nil {
		c.mu.RUnlock()
		return c.parameterStoreClient
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.parameterStoreClient == nil {
		c.parameterStoreClient = newParameterStoreClient(c.transport)
	}
	return c.parameterStoreClient
}

// Scheduler returns the Scheduler client for job scheduling.
func (c *client) Scheduler() SchedulerClient {
	c.mu.RLock()
	if c.schedulerClient != nil {
		c.mu.RUnlock()
		return c.schedulerClient
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.schedulerClient == nil {
		c.schedulerClient = newSchedulerClient(c.transport)
	}
	return c.schedulerClient
}

// Messaging returns the Messaging client for pub/sub and push notifications.
func (c *client) Messaging() MessagingClient {
	c.mu.RLock()
	if c.messagingClient != nil {
		c.mu.RUnlock()
		return c.messagingClient
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.messagingClient == nil {
		c.messagingClient = newMessagingClient(c.transport)
	}
	return c.messagingClient
}

// Embedding returns the Embedding client for embedding generation.
func (c *client) Embedding() EmbeddingClient {
	c.mu.RLock()
	if c.embeddingClient != nil {
		c.mu.RUnlock()
		return c.embeddingClient
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.embeddingClient == nil {
		c.embeddingClient = newEmbeddingClient(c.transport)
	}
	return c.embeddingClient
}

// Ollama returns the Ollama client for Ollama embedding operations.
func (c *client) Ollama() OllamaClient {
	c.mu.RLock()
	if c.ollamaClient != nil {
		c.mu.RUnlock()
		return c.ollamaClient
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.ollamaClient == nil {
		c.ollamaClient = newOllamaClient(c.transport)
	}
	return c.ollamaClient
}

// RAG returns the RAG client for retrieval-augmented generation operations.
func (c *client) RAG() RAGClient {
	c.mu.RLock()
	if c.ragClient != nil {
		c.mu.RUnlock()
		return c.ragClient
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.ragClient == nil {
		c.ragClient = newRAGClient(c.transport)
	}
	return c.ragClient
}

// Orchestrator returns the orchestrator client for fleet management operations.
func (c *client) Orchestrator() OrchestratorClient {
	c.mu.RLock()
	if c.orchestratorClient != nil {
		c.mu.RUnlock()
		return c.orchestratorClient
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.orchestratorClient == nil {
		c.orchestratorClient = newOrchestratorClient(c.transport)
	}
	return c.orchestratorClient
}

// Project returns the Project client for managing agentic project entities.
func (c *client) Project() ProjectClient {
	c.mu.RLock()
	if c.projectClient != nil {
		c.mu.RUnlock()
		return c.projectClient
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.projectClient == nil {
		c.projectClient = newProjectClient(c.transport)
	}
	return c.projectClient
}

// SDLC returns the client for the SDLC module: forge bindings and deliveries.
func (c *client) SDLC() SDLCClient {
	c.mu.RLock()
	if c.sdlcClient != nil {
		c.mu.RUnlock()
		return c.sdlcClient
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.sdlcClient == nil {
		c.sdlcClient = newSDLCClient(c.transport)
	}
	return c.sdlcClient
}

// Agents returns the client for the tenant AI agent catalog: Agents and the
// pools role slots fail over along.
func (c *client) Agents() AgentsClient {
	c.mu.RLock()
	if c.agentsClient != nil {
		c.mu.RUnlock()
		return c.agentsClient
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.agentsClient == nil {
		c.agentsClient = newAgentsClient(c.transport)
	}
	return c.agentsClient
}

// Account returns the Account client for managing pooled provider accounts.
func (c *client) Account() AccountClient {
	c.mu.RLock()
	if c.accountClient != nil {
		c.mu.RUnlock()
		return c.accountClient
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.accountClient == nil {
		c.accountClient = newAccountClient(c.transport)
	}
	return c.accountClient
}

// Client constructors - wire up all client implementations
func newKVClient(t Transport) KVClient                         { return kv.NewClient(t) }
func newBatchClient(t Transport) BatchClient                   { return batch.NewClient(t) }
func newTransactionClient(t Transport) TransactionClient       { return transaction.NewClient(t) }
func newQueryClient(t Transport) QueryClient                   { return query.NewClient(t) }
func newClusterClient(t Transport) ClusterClient               { return cluster.NewClient(t) }
func newHealthClient(t Transport) HealthClient                 { return health.NewClient(t) }
func newAuthClient(t Transport) AuthClient                     { return auth.NewClient(t) }
func newSealClient(t Transport) SealClient                     { return seal.NewClient(t) }
func newKMSClient(t Transport) KMSClient                       { return kms.NewClient(t) }
func newSecretsClient(t Transport) SecretsClient               { return secrets.NewClient(t) }
func newCryptoClient(t Transport) CryptoClient                 { return crypto.NewClient(t) }
func newVectorClient(t Transport) VectorClient                 { return vector.NewClient(t) }
func newTimeSeriesClient(t Transport) TimeSeriesClient         { return timeseries.NewClient(t) }
func newBackupClient(t Transport) BackupClient                 { return backup.NewClient(t) }
func newIAMClient(t Transport) IAMClient                       { return iam.NewClient(t) }
func newCacheClient(t Transport) CacheClient                   { return cache.NewClient(t) }
func newOperationsClient(t Transport) OperationsClient         { return operations.NewClient(t) }
func newRBACClient(t Transport) RBACClient                     { return rbac.NewClient(t) }
func newDynamicSecretsClient(t Transport) DynamicSecretsClient { return dynamicsecrets.NewClient(t) }
func newCertificateClient(t Transport) CertificateClient       { return certificate.NewClient(t) }
func newDEKClient(t Transport) DEKClient                       { return dek.NewClient(t) }
func newObjectStorageClient(t Transport) ObjectStorageClient   { return objectstorage.NewClient(t) }
func newObjectStorageAdminClient(t Transport) ObjectStorageAdminClient {
	return objectstorageadmin.NewClient(t)
}
func newObjectTransferClient(t Transport) ObjectTransferClient { return objecttransfer.NewClient(t) }
func newParameterStoreClient(t Transport) ParameterStoreClient { return parameterstore.NewClient(t) }
func newSchedulerClient(t Transport) SchedulerClient           { return scheduler.NewClient(t) }
func newMessagingClient(t Transport) MessagingClient           { return messaging.NewClient(t) }
func newEmbeddingClient(t Transport) EmbeddingClient           { return embedding.NewClient(t) }
func newOllamaClient(t Transport) OllamaClient                 { return ollama.NewClient(t) }
func newRAGClient(t Transport) RAGClient                       { return rag.NewClient(t) }
func newOrchestratorClient(t Transport) OrchestratorClient     { return orchestrator.NewClient(t) }
func newProjectClient(t Transport) ProjectClient               { return project.NewClient(t) }
func newSDLCClient(t Transport) SDLCClient                     { return sdlc.NewClient(t) }
func newAgentsClient(t Transport) AgentsClient                 { return agents.NewClient(t) }
func newAccountClient(t Transport) AccountClient               { return account.NewClient(t) }
