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

// Package service provides the unified service facade for QuicBaaS.
// It serves as the SINGLE point of access for all API transports (gRPC, REST, QUIC, MCP)
// and wraps all backend components with consistent cross-cutting concerns.
//
// The service facade provides:
// - Unified interface for all database operations
// - Authentication and authorization enforcement
// - Metrics collection and observability
// - Structured logging and tracing
// - Error handling and status code mapping
// - Transaction and consistency management
//
// Architecture:
//   - QuicBaaS: Main interface defining all operations
//   - ServiceImpl: Concrete implementation orchestrating backend components
//   - Sub-services: Specialized service groups for different operation domains
//   - Middleware: Cross-cutting concerns applied uniformly
package service

import (
	"context"
	"crypto/x509"
	"time"

	qrdbsecret "github.com/jeremyhahn/go-qrdb/sdk/go/secret"
	xkmsssh "github.com/jeremyhahn/go-xkms/sdk/go/client/ssh"

	"github.com/jeremyhahn/go-quicbaas/pkg/catalog"
	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/messaging"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc"
	"github.com/jeremyhahn/go-quicbaas/pkg/watch"
)

// QuicBaaS is the unified service facade providing ALL database operations.
// This interface serves as the single point of access for all API transports.
// All operations accept context for cancellation, timeouts, and metadata propagation.
type QuicBaaS interface {
	// KV Operations - Basic key-value store operations
	KVService

	// Batch Operations - Atomic multi-operation requests
	BatchService

	// Transaction Operations - Compare-and-swap transactions
	TransactionService

	// Query Operations - Secondary index queries and scans
	QueryService

	// Cluster Operations - Cluster management and topology
	ClusterService

	// Backup Operations - Backup and restore functionality
	BackupService

	// Auth Operations - Authentication and session management
	AuthService

	// RBAC Operations - User and role management
	RBACService

	// Cache Operations - Cache management and statistics
	CacheService

	// Health Operations - Health checks and liveness probes
	HealthService

	// Time-Series Operations - Time-series data aggregation
	TimeSeriesService

	// Operations Management - Migration, rebalancing, upgrades
	OperationsService

	// KMS returns the KMS service (returns nil if not configured)
	KMS() KMSService

	// KMSManager returns the underlying KMS manager for direct access
	// This provides the interfaces.KMSManager implementation used by API layers
	KMSManager() interfaces.KMSManager

	// Secrets returns the Secrets service (returns nil if not configured)
	Secrets() SecretsService

	// Transit returns the Crypto service (returns nil if not configured)
	Crypto() CryptoService

	// Vector returns the Vector service (returns nil if not configured)
	Vector() VectorService

	// Embedding returns the Embedding service (returns nil if not configured)
	Embedding() EmbeddingService

	// Ollama returns the Ollama service (returns nil if not configured)
	Ollama() OllamaService

	// Certificate returns the Certificate service (returns nil if not configured)
	Certificate() CertificateService

	// Trust returns the Trust composition service (returns nil if not
	// configured). It exposes the cross-sign / AIA verbs surfaced under
	// Security().Trust(), backed by the xkms TrustedCA via the CA adapter.
	Trust() TrustService

	// Attestation returns the Attestation service surfaced under
	// Security().Attestation(). It is always configured: the verifier half
	// composes go-xkms pkg/attestation and needs no external dependency,
	// while the orchestration half returns a typed not-configured error
	// until an application registers a ContainerAttestor.
	Attestation() AttestationService

	// Registry returns the service registry: lease-based service-instance
	// endpoints (register, renew, deregister, resolve, watch). It is always
	// configured; a store that cannot be reached is reported per call.
	Registry() RegistryService

	// PIV returns the backend-scoped PIV composition service surfaced
	// under Security().Auth().PIV(). Always configured: every verb
	// delegates to the composed go-xkms service, whose typed errors
	// report the not-initialized case.
	PIV() PIVService

	// FIDO2 returns the tenant-scoped WebAuthn ceremony composition
	// service surfaced under Security().Auth().FIDO2(). Always
	// configured: it resolves the per-tenant xkms relying-party service
	// at call time and returns a typed not-configured error until the
	// xkms-backed WebAuthn manager is wired.
	FIDO2() FIDO2Service

	// ACME returns the ACME (RFC 8555) composition service (returns nil if
	// not configured). It exposes the server-side challenge-registration and
	// issuance verbs surfaced under Security().ACME(). ACME is HTTP-native:
	// reachable over REST and the in-process embedded transport only.
	ACME() ACMEServicer

	// DynamicSecrets returns the Dynamic Secrets service (returns nil if not configured)
	// Dynamic secrets handle credential generation with lease-based TTL management.
	DynamicSecrets() DynamicSecretsService

	// ObjectStorageService returns the Object Storage service (returns nil if not configured)
	ObjectStorageService() ObjectStorageService

	// ObjectStorageAdminService returns the Object Storage Admin service (returns nil if not configured)
	// ObjectStorageAdminService provides administrative operations for storage management.
	ObjectStorageAdminService() ObjectStorageAdminService

	// ParameterStoreService returns the Parameter Store service (returns nil if not configured)
	ParameterStoreService() ParameterStoreService

	// ObjectTransferService returns the Object Transfer service (returns nil if not configured)
	// ObjectTransferService provides peer-to-peer object data transfer for lazy snapshot recovery.
	ObjectTransferService() ObjectTransferService

	// Scheduler returns the Scheduler service (returns nil if not configured)
	// Scheduler provides job scheduling and execution with multi-tenant support.
	Scheduler() SchedulerService

	// Placement returns the scheduler's workload placement service (nil
	// when no qrdb client is configured). It is surfaced to SDK callers as
	// the placement operations of Async().Scheduler().
	Placement() PlacementService

	// SchedulerEngine returns the raw scheduler engine for in-process handler
	// registration (returns nil if not configured). External (over-the-wire)
	// callers should use Scheduler() instead; this accessor is intended for
	// embedded consumers (e.g. platformd) that need to call RegisterHandler
	// after Build() completes.
	SchedulerEngine() *scheduler.SchedulerEngine

	// SSHClient returns the SSH key, known-hosts, and authorized-keys
	// client (xkms-backed) supplied through WithSSHClient. Returns nil
	// when the node has no SSH key store: no remote xkmsd is configured
	// and the data plane is not barrier-sealed.
	SSHClient() xkmsssh.Client

	// Project returns the Project service for managing agentic project entities.
	// Projects sit between Tenant and Workflow in the hierarchy.
	Project() ProjectService

	// Account returns the Account service for managing pooled provider
	// accounts (the per-tenant pool of Anthropic/Claude, Codex, and OpenCode
	// logins an agent session can be spawned against). Never returns
	// credential material — see pkg/service/account_service.go.
	Account() AccountService

	// FleetAdmin returns the Agent Admin service for global agent lifecycle management.
	// This service handles enabling and disabling agents across all projects.
	FleetAdmin() FleetAdminService

	// Agent returns the Agent service (returns nil if not configured)
	// Agent provides AI agent orchestration across multiple backends.
	Agent() orchestration.AgentService

	// Database returns the Database service (returns nil if not configured)
	// Database provides tenant-scoped database lifecycle management.
	Database() DatabaseService

	// Messaging returns the Messaging notification service (returns nil if not configured)
	// Messaging provides real-time push notifications and event streaming.
	Messaging() messaging.Service

	// WatchManager returns the internal watch.Manager for bridge integration.
	WatchManager() *watch.Manager

	// SetIAMProvider sets the IAM components provider for REST handlers.
	// This is called by the server after IAM components are initialized.
	SetIAMProvider(provider IAMComponents)

	// SetMessagingService sets the Messaging notification service.
	// This is called after the messaging service is initialized externally.
	SetMessagingService(ms messaging.Service)

	// SetAgentService sets the Agent orchestration service.
	// This is called during initialization when orchestration is configured.
	SetAgentService(agent orchestration.AgentService)

	// SDLC returns the SDLC module's project service (returns nil when the
	// module is not running on this node). Projects are registered, listed
	// and removed through it; the engines that work them are the server's.
	SDLC() *SDLCService

	// SetSDLCService attaches the SDLC module's service. Called by the server
	// once the module has started, the way SetAgentService is.
	SetSDLCService(svc *SDLCService)

	// Prompts returns the prompt service (nil when the prompt store is not
	// mounted on this node). Every transport answers 503 / Unavailable then.
	Prompts() *PromptService

	// SetPromptService attaches the prompt service. Called by the server once
	// the prompt store is mounted.
	SetPromptService(svc *PromptService)

	// SetProjectBindingReader attaches the SDLC binding store a project
	// delete is checked against. Called by the server on every node whose
	// qrdb node has a tenant store resolver, with or without the SDLC module.
	SetProjectBindingReader(bindings sdlc.BindingReader)

	// SetDatabaseService sets the Database lifecycle management service.
	// This is called after the database manager is initialized.
	SetDatabaseService(ds DatabaseService)

	// SetQRDBClient sets the qrdb SDK client provider for core KV operations.
	// This is called after the ShardedClient is initialized.
	SetQRDBClient(provider QRDBClientProvider)

	// SetDynamicSecretsService wires the Dynamic Secrets service into the
	// facade at runtime.
	//
	// It is a setter rather than a builder option because the service is composed during server bring-up, over
	// the durable qrdb-backed KV and the credential generator registry, which
	// is strictly later than the service container that owns the facade.
	// Until it is called, Security().DynamicSecrets() reports the capability
	// as unconfigured rather than panicking.
	SetDynamicSecretsService(svc DynamicSecretsService)

	// LoginCeremony returns the unified login-ceremony facade
	// (BeginLogin / FinishLogin). It forwards to the composed go-qrdb node's
	// in-process LoginClient and is the single home for the mTLS anti-spoofing
	// security model. Returns nil only before SetLoginService has been called.
	LoginCeremony() LoginServicer

	// SetLoginService wires the unified login-ceremony facade into the service
	// at runtime. Called by the server once the qrdb node (which hosts the
	// in-process LoginClient) is constructed.
	SetLoginService(svc LoginServicer)

	// SetSecretRegistry wires the unified secret resolver registry (built by
	// the server's buildSecretRegistry in every mode) into the service so that
	// embedded SDK clients auto-populate Security().SecretResolver() without an
	// explicit WithSecretRegistry. The secrets manager is a core, always-on
	// service; this makes its resolver reachable through the embedded facade.
	SetSecretRegistry(reg *qrdbsecret.ResolverRegistry)

	// SecretRegistry returns the unified secret resolver registry, or nil
	// before SetSecretRegistry has wired it.
	SecretRegistry() *qrdbsecret.ResolverRegistry

	// Close gracefully shuts down the service and all backend components
	Close() error
}

// =============================================================================
// KV Service - Basic Key-Value Operations
// =============================================================================

// KVService defines basic key-value store operations.
type KVService interface {
	// Put stores a key-value pair with optional consistency guarantees.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Put request with entity type, ID, data, consistency level
	// Returns:
	//   - PutResponse: Contains version, timestamp, leader ID
	//   - error: ErrInvalidInput, ErrTimeout, ErrRaft
	Put(ctx context.Context, req *PutRequest) (*PutResponse, error)

	// Get retrieves a value by key with configurable consistency.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Get request with entity type, ID, consistency level
	// Returns:
	//   - GetResponse: Contains data, version, timestamp, found status
	//   - error: ErrNotFound, ErrTimeout, ErrRaft
	Get(ctx context.Context, req *GetRequest) (*GetResponse, error)

	// Delete removes a key-value pair.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Delete request with entity type, ID, consistency level
	// Returns:
	//   - DeleteResponse: Contains timestamp and deleted status
	//   - error: ErrNotFound, ErrTimeout, ErrRaft
	Delete(ctx context.Context, req *DeleteRequest) (*DeleteResponse, error)

	// List returns all entities of a type with pagination.
	// This is a streaming operation that sends results in batches.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: List request with entity type, page size, page token
	//   - stream: Channel to send paginated results; the implementation closes it
	//     before returning, so callers must not
	// Returns:
	//   - error: ErrInvalidInput, ErrTimeout, ErrRaft
	List(ctx context.Context, req *ListRequest, stream chan<- *ListResponse) error

	// Watch monitors a key or key prefix for changes.
	// This is a streaming operation that sends events as they occur.
	// Parameters:
	//   - ctx: Request context with timeout and cancellation
	//   - req: Watch request with key, prefix flag, and options
	//   - stream: Channel to send watch events
	// Returns:
	//   - WatchID: Identifier for the watch subscription
	//   - error: ErrInvalidInput, ErrTimeout, ErrRaft
	Watch(ctx context.Context, req *WatchRequest, stream chan<- *WatchResponse) (int64, error)

	// ListEntityTypes returns all unique entity types stored in the database.
	// This is useful for discovering what data structures exist in the store.
	// In multi-tenant mode, only returns entity types within the tenant's keyspace.
	// Parameters:
	//   - ctx: Request context with tenant information and timeout
	//   - req: ListEntityTypes request with pagination options
	// Returns:
	//   - ListEntityTypesResponse: Contains unique entity type names
	//   - error: ErrTimeout, ErrRaft, ErrSystemKeyspaceViolation
	ListEntityTypes(ctx context.Context, req *ListEntityTypesRequest) (*ListEntityTypesResponse, error)
}

// =============================================================================
// Batch Service - Atomic Multi-Operation Requests
// =============================================================================

// BatchService defines atomic batch operations.
type BatchService interface {
	// BatchPut performs multiple Put operations atomically.
	// All operations succeed or all fail (atomicity guaranteed by Raft).
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Batch put request with multiple put operations
	// Returns:
	//   - BatchPutResponse: Contains responses for each operation
	//   - error: ErrInvalidInput, ErrTimeout, ErrRaft
	BatchPut(ctx context.Context, req *BatchPutRequest) (*BatchPutResponse, error)

	// BatchGet retrieves multiple values in a single request.
	// Operations are independent and may have different results.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Batch get request with multiple get operations
	// Returns:
	//   - BatchGetResponse: Contains responses for each operation
	//   - error: ErrInvalidInput, ErrTimeout, ErrRaft
	BatchGet(ctx context.Context, req *BatchGetRequest) (*BatchGetResponse, error)

	// BatchDelete removes multiple keys atomically.
	// All operations succeed or all fail (atomicity guaranteed by Raft).
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Batch delete request with multiple delete operations
	// Returns:
	//   - BatchDeleteResponse: Contains responses for each operation
	//   - error: ErrInvalidInput, ErrTimeout, ErrRaft
	BatchDelete(ctx context.Context, req *BatchDeleteRequest) (*BatchDeleteResponse, error)
}

// =============================================================================
// Transaction Service - Compare-and-Swap Transactions
// =============================================================================

// TransactionService defines atomic compare-and-swap transactions.
type TransactionService interface {
	// Transaction performs an atomic compare-and-swap transaction.
	// Evaluates comparison conditions and executes success or failure operations.
	// All operations within success/failure blocks are atomic.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Transaction request with compare conditions and operations
	// Returns:
	//   - TransactionResponse: Contains succeeded flag and operation responses
	//   - error: ErrInvalidInput, ErrTimeout, ErrRaft
	Transaction(ctx context.Context, req *TransactionRequest) (*TransactionResponse, error)
}

// =============================================================================
// Query Service - Secondary Index Queries and Scans
// =============================================================================

// QueryService defines query and index operations.
type QueryService interface {
	// Query searches for entities using secondary indexes.
	// This is a streaming operation that sends results in batches.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Query request with entity type, index name, value
	//   - stream: Channel to send query results; the implementation closes it
	//     before returning, so callers must not
	// Returns:
	//   - error: ErrInvalidInput, ErrTimeout, ErrRaft
	Query(ctx context.Context, req *QueryRequest, stream chan<- *QueryResponse) error

	// ScanIndex performs a full index scan.
	// This is a streaming operation that sends results in batches.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Scan request with entity type and index name
	//   - stream: Channel to send scan results; the implementation closes it
	//     before returning, so callers must not
	// Returns:
	//   - error: ErrInvalidInput, ErrTimeout, ErrRaft
	ScanIndex(ctx context.Context, req *ScanRequest, stream chan<- *ScanResponse) error
}

// =============================================================================
// Cluster Service - Cluster Management and Topology
// =============================================================================

// ClusterService defines cluster management operations.
type ClusterService interface {
	// ClusterStatus returns the current state of all cluster nodes.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Cluster status request (optional filters)
	// Returns:
	//   - ClusterStatusResponse: Contains cluster ID, nodes, shards, timestamp
	//   - error: ErrTimeout, ErrRaft
	ClusterStatus(ctx context.Context, req *ClusterStatusRequest) (*ClusterStatusResponse, error)

	// GetLeader returns the current leader for a specific shard.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Get leader request with shard ID
	// Returns:
	//   - GetLeaderResponse: Contains shard ID and leader node ID
	//   - error: ErrLeaderNotAvailable, ErrTimeout
	GetLeader(ctx context.Context, req *GetLeaderRequest) (*GetLeaderResponse, error)

	// AddNode adds a node to a Raft shard.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Add node request with shard ID, node ID, address
	// Returns:
	//   - AddNodeResponse: Contains success status and message
	//   - error: ErrInvalidInput, ErrTimeout, ErrRaft
	AddNode(ctx context.Context, req *AddNodeRequest) (*AddNodeResponse, error)

	// RemoveNode removes a node from a Raft shard.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Remove node request with shard ID and node ID
	// Returns:
	//   - RemoveNodeResponse: Contains success status and message
	//   - error: ErrInvalidInput, ErrTimeout, ErrRaft
	RemoveNode(ctx context.Context, req *RemoveNodeRequest) (*RemoveNodeResponse, error)

	// CreateShard creates a new Raft shard with initial members.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Create shard request with shard ID and initial members
	// Returns:
	//   - CreateShardResponse: Contains shard ID and status
	//   - error: ErrInvalidInput, ErrAlreadyExists, ErrRaft
	CreateShard(ctx context.Context, req *CreateShardRequest) (*CreateShardResponse, error)

	// RebalanceCluster redistributes data across shards for better balance.
	// This is an async operation that starts rebalancing in the background.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Rebalance request with target distribution
	// Returns:
	//   - RebalanceClusterResponse: Contains operation ID for progress tracking
	//   - error: ErrInvalidInput, ErrTimeout, ErrRaft
	RebalanceCluster(ctx context.Context, req *RebalanceClusterRequest) (*RebalanceClusterResponse, error)

	// GetRebalanceProgress returns the status of an ongoing rebalance operation.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Get rebalance progress request with operation ID
	// Returns:
	//   - GetRebalanceProgressResponse: Contains progress percentage, status, metrics
	//   - error: ErrNotFound, ErrTimeout
	GetRebalanceProgress(ctx context.Context, req *GetRebalanceProgressRequest) (*GetRebalanceProgressResponse, error)
}

// =============================================================================
// Backup Service - Backup and Restore Functionality
// =============================================================================

// BackupService defines backup and restore operations.
type BackupService interface {
	// CreateBackup creates a consistent backup of the database.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Create backup request with metadata and options
	// Returns:
	//   - CreateBackupResponse: Contains backup ID, size, timestamp
	//   - error: ErrInvalidInput, ErrTimeout, ErrInternal
	CreateBackup(ctx context.Context, req *CreateBackupRequest) (*CreateBackupResponse, error)

	// ListBackups returns available backups with optional filtering.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: List backups request with optional filters
	// Returns:
	//   - ListBackupsResponse: Contains backup metadata list
	//   - error: ErrTimeout, ErrInternal
	ListBackups(ctx context.Context, req *ListBackupsRequest) (*ListBackupsResponse, error)

	// RestoreBackup restores the database from a backup.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Restore backup request with backup ID
	// Returns:
	//   - RestoreBackupResponse: Contains restore status and progress
	//   - error: ErrNotFound, ErrTimeout, ErrInternal
	RestoreBackup(ctx context.Context, req *RestoreBackupRequest) (*RestoreBackupResponse, error)

	// DeleteBackup removes a backup from storage.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Delete backup request with backup ID
	// Returns:
	//   - DeleteBackupResponse: Contains deletion status
	//   - error: ErrNotFound, ErrTimeout, ErrInternal
	DeleteBackup(ctx context.Context, req *DeleteBackupRequest) (*DeleteBackupResponse, error)

	// VerifyBackup checks backup integrity and validity.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Verify backup request with backup ID
	// Returns:
	//   - VerifyBackupResponse: Contains verification status and details
	//   - error: ErrNotFound, ErrTimeout, ErrInternal
	VerifyBackup(ctx context.Context, req *VerifyBackupRequest) (*VerifyBackupResponse, error)
}

// =============================================================================
// Auth Service - Authentication and Session Management
// =============================================================================

// AuthService defines authentication operations.
type AuthService interface {
	// Login authenticates a user and returns access/refresh tokens.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Login request with username and password
	// Returns:
	//   - LoginResponse: Contains access token, refresh token, expiry
	//   - error: ErrUnauthorized, ErrTimeout
	Login(ctx context.Context, req *LoginRequest) (*LoginResponse, error)

	// Logout invalidates the current session and tokens.
	// Parameters:
	//   - ctx: Request context with timeout and metadata (includes auth token)
	//   - req: Logout request
	// Returns:
	//   - LogoutResponse: Contains logout status
	//   - error: ErrUnauthorized, ErrTimeout
	Logout(ctx context.Context, req *LogoutRequest) (*LogoutResponse, error)

	// RefreshToken refreshes an access token using a refresh token.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Refresh token request with refresh token
	// Returns:
	//   - RefreshTokenResponse: Contains new access token and expiry
	//   - error: ErrUnauthorized, ErrTimeout
	RefreshToken(ctx context.Context, req *RefreshTokenRequest) (*RefreshTokenResponse, error)

	// ValidateToken validates an access token and returns user info.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Validate token request with access token
	// Returns:
	//   - ValidateTokenResponse: Contains user info and token validity
	//   - error: ErrUnauthorized, ErrTimeout
	ValidateToken(ctx context.Context, req *ValidateTokenRequest) (*ValidateTokenResponse, error)

	// IntrospectToken reports an access token's RFC 7662 introspection
	// state for a resource server. An inactive, expired, revoked or
	// unparseable token is not an error: it yields exactly Active=false.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Introspect token request with the token to inspect
	// Returns:
	//   - TokenIntrospection: Active plus the token's claims when active
	//   - error: ErrInvalidInput (no token), ErrConfiguration
	IntrospectToken(ctx context.Context, req *IntrospectTokenRequest) (*TokenIntrospection, error)
}

// =============================================================================
// RBAC Service - User and Role Management
// =============================================================================

// RBACService defines role-based access control operations over roles and role
// bindings. It has no user verbs: users belong to go-qrdb and are administered
// through the IAM surface (IAMService below, Identity().IAM().User() on the
// SDK). The CreateUser/DeleteUser/ListUsers trio that used to sit here was
// backed by a local auth manager production never wired, so every call failed
// with "authentication manager not configured"; issue #905 deleted it with the
// rest of the parallel identity model.
type RBACService interface {
	// CreateAuthRole creates a new RBAC role with permissions.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Create role request with role name and permissions
	// Returns:
	//   - AuthCreateRoleResponse: Contains role ID and creation timestamp
	//   - error: ErrAlreadyExists, ErrUnauthorized, ErrTimeout
	CreateAuthRole(ctx context.Context, req *AuthCreateRoleRequest) (*AuthCreateRoleResponse, error)

	// DeleteAuthRole removes an RBAC role from the system.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Delete role request with role name
	// Returns:
	//   - AuthDeleteRoleResponse: Contains deletion status
	//   - error: ErrNotFound, ErrUnauthorized, ErrTimeout
	DeleteAuthRole(ctx context.Context, req *AuthDeleteRoleRequest) (*AuthDeleteRoleResponse, error)

	// ListAuthRoles returns all RBAC roles with optional filtering.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: List roles request with optional filters
	// Returns:
	//   - AuthListRolesResponse: Contains role list with permissions
	//   - error: ErrUnauthorized, ErrTimeout
	ListAuthRoles(ctx context.Context, req *AuthListRolesRequest) (*AuthListRolesResponse, error)

	// AssignRole assigns a role to a user.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Assign role request with username and role name
	// Returns:
	//   - AuthAssignRoleResponse: Contains assignment status
	//   - error: ErrNotFound, ErrUnauthorized, ErrTimeout
	AssignRole(ctx context.Context, req *AuthAssignRoleRequest) (*AuthAssignRoleResponse, error)

	// RevokeRole removes a role from a user.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Revoke role request with username and role name
	// Returns:
	//   - AuthRevokeRoleResponse: Contains revocation status
	//   - error: ErrNotFound, ErrUnauthorized, ErrTimeout
	RevokeRole(ctx context.Context, req *AuthRevokeRoleRequest) (*AuthRevokeRoleResponse, error)
}

// =============================================================================
// Cache Service - Cache Management and Statistics
// =============================================================================

// CacheService defines cache management operations.
type CacheService interface {
	// GetCacheStats returns current cache statistics.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Get cache stats request (optional entity type filter)
	// Returns:
	//   - GetCacheStatsResponse: Contains hit rate, size, evictions, etc.
	//   - error: ErrTimeout
	GetCacheStats(ctx context.Context, req *GetCacheStatsRequest) (*GetCacheStatsResponse, error)

	// ClearCache invalidates all or specific cache entries.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Clear cache request with optional entity type filter
	// Returns:
	//   - ClearCacheResponse: Contains entries cleared count
	//   - error: ErrTimeout
	ClearCache(ctx context.Context, req *ClearCacheRequest) (*ClearCacheResponse, error)

	// GetCacheConfig returns current cache configuration.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Get cache config request
	// Returns:
	//   - GetCacheConfigResponse: Contains cache size, TTL, policy
	//   - error: ErrTimeout
	GetCacheConfig(ctx context.Context, req *GetCacheConfigRequest) (*GetCacheConfigResponse, error)

	// UpdateCacheConfig updates cache configuration.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Update cache config request with new settings
	// Returns:
	//   - UpdateCacheConfigResponse: Contains updated configuration
	//   - error: ErrInvalidInput, ErrTimeout
	UpdateCacheConfig(ctx context.Context, req *UpdateCacheConfigRequest) (*UpdateCacheConfigResponse, error)
}

// =============================================================================
// Health Service - Health Checks and Liveness Probes
// =============================================================================

// HealthService defines health check operations.
type HealthService interface {
	// Health returns the health status of the node with optional details.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Health request with optional include details flag
	// Returns:
	//   - HealthResponse: Contains status, version, uptime, component health
	//   - error: None (always returns response with status)
	Health(ctx context.Context, req *HealthRequest) (*HealthResponse, error)

	// Liveness returns a simple liveness probe (is the service running?).
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Liveness request
	// Returns:
	//   - LivenessResponse: Contains alive status
	//   - error: None (always returns response)
	Liveness(ctx context.Context, req *LivenessRequest) (*LivenessResponse, error)

	// Readiness returns a readiness probe (is the service ready to serve?).
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Readiness request
	// Returns:
	//   - ReadinessResponse: Contains ready status and reason if not ready
	//   - error: None (always returns response)
	Readiness(ctx context.Context, req *ReadinessRequest) (*ReadinessResponse, error)
}

// =============================================================================
// Time-Series Service - Time-Series Data Aggregation
// =============================================================================

// TimeSeriesService defines time-series operations.
type TimeSeriesService interface {
	// Aggregate performs time-series aggregation (sum, avg, min, max, count).
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Aggregate request with entity type, field, function, time range
	// Returns:
	//   - AggregateResponse: Contains aggregated result and count
	//   - error: ErrInvalidInput, ErrTimeout
	Aggregate(ctx context.Context, req *AggregateRequest) (*AggregateResponse, error)

	// TimeSeriesQuery retrieves time-series data within a time range.
	// This is a streaming operation that sends results in batches.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Time-series query request with entity type and time range
	//   - stream: Channel to send time-series data points; the implementation
	//     closes it before returning, so callers must not
	// Returns:
	//   - error: ErrInvalidInput, ErrTimeout
	TimeSeriesQuery(ctx context.Context, req *TimeSeriesQueryRequest, stream chan<- *TimeSeriesQueryResponse) error

	// WritePoint writes a single data point to time series storage.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Write point request with metric, tags, value, timestamp
	// Returns:
	//   - WritePointResponse: Contains success status and timestamp
	//   - error: ErrInvalidInput, ErrTimeout, ErrRaft
	WritePoint(ctx context.Context, req *WritePointRequest) (*WritePointResponse, error)

	// WriteBatch writes multiple data points atomically.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Write batch request with multiple data points
	// Returns:
	//   - WriteBatchResponse: Contains success status and points written count
	//   - error: ErrInvalidInput, ErrTimeout, ErrRaft
	WriteBatch(ctx context.Context, req *WriteBatchRequest) (*WriteBatchResponse, error)

	// Query performs time range queries with optional aggregation.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Query request with metric, time range, aggregation options
	// Returns:
	//   - QueryResultResponse: Contains matching data points and aggregated values
	//   - error: ErrInvalidInput, ErrTimeout, ErrRaft
	TSQuery(ctx context.Context, req *TSQueryRequest) (*TSQueryResponse, error)

	// DeleteSeries deletes a time series or specific data points.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Delete request with metric and optional time range
	// Returns:
	//   - DeleteSeriesResponse: Contains success status and timestamp
	//   - error: ErrInvalidInput, ErrTimeout, ErrRaft
	DeleteSeries(ctx context.Context, req *TSDeleteRequest) (*TSDeleteResponse, error)

	// GetRetentionPolicy retrieves the retention policy for a metric.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Get retention policy request with metric name
	// Returns:
	//   - GetRetentionPolicyResponse: Contains policy details
	//   - error: ErrNotFound, ErrTimeout, ErrRaft
	GetRetentionPolicy(ctx context.Context, req *TSGetRetentionPolicyRequest) (*TSGetRetentionPolicyResponse, error)

	// SetRetentionPolicy sets the retention policy for a metric.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Set retention policy request with metric and policy
	// Returns:
	//   - SetRetentionPolicyResponse: Contains success status and timestamp
	//   - error: ErrInvalidInput, ErrTimeout, ErrRaft
	SetRetentionPolicy(ctx context.Context, req *TSSetRetentionPolicyRequest) (*TSSetRetentionPolicyResponse, error)

	// ListMetrics lists all available metrics.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: List metrics request with optional prefix and tag filters
	// Returns:
	//   - ListMetricsResponse: Contains metric names and pagination cursor
	//   - error: ErrTimeout, ErrRaft
	ListMetrics(ctx context.Context, req *TSListMetricsRequest) (*TSListMetricsResponse, error)
}

// =============================================================================
// Operations Service - Migration, Rebalancing, Upgrades
// =============================================================================

// OperationsService defines operational management operations.
type OperationsService interface {
	// MigrateData migrates data between shards or nodes.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Migrate data request with source, target, entity types
	// Returns:
	//   - MigrateDataResponse: Contains migration ID and status
	//   - error: ErrInvalidInput, ErrTimeout
	MigrateData(ctx context.Context, req *MigrateDataRequest) (*MigrateDataResponse, error)

	// GetMigrationProgress returns the status of an ongoing migration.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Get migration progress request with migration ID
	// Returns:
	//   - GetMigrationProgressResponse: Contains progress percentage, ETA
	//   - error: ErrNotFound, ErrTimeout
	GetMigrationProgress(ctx context.Context, req *GetMigrationProgressRequest) (*GetMigrationProgressResponse, error)

	// ReplaceNode replaces a failed node with a new node.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Replace node request with old node ID, new node ID, address
	// Returns:
	//   - ReplaceNodeResponse: Contains replacement status
	//   - error: ErrInvalidInput, ErrTimeout
	ReplaceNode(ctx context.Context, req *ReplaceNodeRequest) (*ReplaceNodeResponse, error)

	// UpgradeNode performs a rolling upgrade of a node.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Upgrade node request with node ID and target version
	// Returns:
	//   - UpgradeNodeResponse: Contains upgrade status
	//   - error: ErrInvalidInput, ErrTimeout
	UpgradeNode(ctx context.Context, req *UpgradeNodeRequest) (*UpgradeNodeResponse, error)

	// GetUpgradeStatus returns the status of an ongoing upgrade.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Get upgrade status request with node ID
	// Returns:
	//   - GetUpgradeStatusResponse: Contains upgrade progress and status
	//   - error: ErrNotFound, ErrTimeout
	GetUpgradeStatus(ctx context.Context, req *GetUpgradeStatusRequest) (*GetUpgradeStatusResponse, error)
}

// =============================================================================
// Common Types and Enums
// =============================================================================

// ConsistencyLevel defines read/write consistency guarantees.
type ConsistencyLevel int

const (
	// ConsistencyLevelUnspecified uses default (Linearizable).
	ConsistencyLevelUnspecified ConsistencyLevel = iota

	// ConsistencyLevelEventual allows stale reads (fastest, lowest latency).
	ConsistencyLevelEventual

	// ConsistencyLevelLinearizable uses ReadIndex (fast, linearizable reads).
	ConsistencyLevelLinearizable

	// ConsistencyLevelStrong uses full Raft consensus (slowest, strongest).
	ConsistencyLevelStrong
)

// WriteMode defines write durability guarantees.
type WriteMode int

const (
	// WriteModeSync waits for quorum commit and fsync (strongest durability).
	WriteModeSync WriteMode = iota

	// WriteModeWALDurable waits for local WAL write only (good durability).
	WriteModeWALDurable

	// WriteModeBatched queues writes for batch processing (highest throughput).
	WriteModeBatched
)

// HealthStatus defines component health status.
type HealthStatus int

const (
	HealthStatusUnknown HealthStatus = iota
	HealthStatusHealthy
	HealthStatusDegraded
	HealthStatusUnhealthy
)

// AggregateFunction defines time-series aggregation functions.
type AggregateFunction int

const (
	AggregateFunctionUnspecified AggregateFunction = iota
	AggregateFunctionSum
	AggregateFunctionAvg
	AggregateFunctionMin
	AggregateFunctionMax
	AggregateFunctionCount
)

// CompareTarget defines transaction comparison targets.
type CompareTarget int

const (
	CompareTargetUnspecified CompareTarget = iota
	CompareTargetVersion
	CompareTargetValue
	CompareTargetExists
)

// CompareOperator defines transaction comparison operators.
type CompareOperator int

const (
	CompareOperatorUnspecified CompareOperator = iota
	CompareOperatorEqual
	CompareOperatorNotEqual
	CompareOperatorGreater
	CompareOperatorLess
	CompareOperatorGreaterEqual
	CompareOperatorLessEqual
)

// TransactionOpType defines transaction operation types.
type TransactionOpType int

const (
	TransactionOpTypeUnspecified TransactionOpType = iota
	TransactionOpTypePut
	TransactionOpTypeGet
	TransactionOpTypeDelete
)

// =============================================================================
// Request/Response Types
// =============================================================================
// Note: These types are defined in separate files to keep this interface
// definition clean and focused. Each type includes detailed field
// documentation and validation rules.
//
// See:
// - kv_types.go: KV service request/response types (Put, Get, Delete, List, Watch)
// - batch_types.go: Batch service request/response types
// - transaction_types.go: Transaction service request/response types
// - query_types.go: Query service request/response types
// - cluster_types.go: Cluster service request/response types
// - backup_types.go: Backup service request/response types
// - auth_types.go: Auth/RBAC service request/response types
// - cache_types.go: Cache service request/response types
// - health_types.go: Health service request/response types
// - timeseries_types.go: Time-series service request/response types
// - operations_types.go: Operations service request/response types
// =============================================================================

// =============================================================================
// Batch Operations Types
// =============================================================================

// Batch Operations Types.
type (
	BatchPutRequest struct {
		Requests []*PutRequest
		ShardID  uint64
	}
	BatchPutResponse struct {
		Responses []*PutResponse
		Timestamp int64
	}
	BatchGetRequest struct {
		Requests []*GetRequest
		ShardID  uint64
	}
	BatchGetResponse   struct{ Responses []*GetResponse }
	BatchDeleteRequest struct {
		Requests []*DeleteRequest
		ShardID  uint64
	}
	BatchDeleteResponse struct {
		Responses []*DeleteResponse
		Timestamp int64
	}
)

// =============================================================================
// Transaction Operations Types
// =============================================================================

// Transaction Operations Types.
type (
	TransactionRequest struct {
		Compare []*CompareCondition
		Success []*TransactionOp
		Failure []*TransactionOp
		ShardID uint64
	}
	TransactionResponse struct {
		Succeeded bool
		Responses []*TransactionOpResponse
		Timestamp int64
		Message   string
		Revision  uint64
	}
	CompareCondition struct {
		EntityType string
		ID         uint64
		Target     CompareTarget
		Operator   CompareOperator
		Version    uint64
		Value      []byte
	}
	TransactionOp struct {
		OpType     TransactionOpType
		EntityType string
		ID         uint64
		Data       []byte
	}
	TransactionOpResponse struct {
		OpType     TransactionOpType
		EntityType string
		ID         uint64
		Data       []byte
		Success    bool
		Version    uint64
		Timestamp  int64
		Error      string
	}
)

// =============================================================================
// Query Operations Types
// =============================================================================

// Query Operations Types.
type (
	QueryRequest struct {
		EntityType  string
		IndexName   string
		Value       []byte
		Consistency ConsistencyLevel
		ShardID     uint64
	}
	QueryResponse struct {
		Entities      []*Entity
		TotalCount    uint64
		NextPageToken string
	}
	ScanRequest struct {
		EntityType string
		IndexName  string
		StartKey   []byte
		EndKey     []byte
		Limit      uint64
		ShardID    uint64
	}
	ScanResponse struct {
		Entities      []*Entity
		TotalCount    uint64
		NextPageToken string
	}
)

// =============================================================================
// Cluster Operations Types
// =============================================================================

// Cluster Operations Types.
type (
	ClusterStatusRequest struct {
		IncludeDetails bool `json:"include_details"`
	}
	ClusterStatusResponse struct {
		ClusterID string       `json:"cluster_id"`
		Nodes     []*NodeInfo  `json:"nodes"`
		Shards    []*ShardInfo `json:"shards"`
		Timestamp int64        `json:"timestamp"`
	}
	NodeInfo struct {
		NodeID        uint64       `json:"node_id"`
		Address       string       `json:"address"`
		IsLeader      bool         `json:"is_leader"`
		HealthStatus  HealthStatus `json:"health_status"`
		Version       string       `json:"version"`
		UptimeSeconds uint64       `json:"uptime_seconds"`
	}
	ShardInfo struct {
		ShardID  uint64       `json:"shard_id"`
		LeaderID uint64       `json:"leader_id"`
		Replicas []uint64     `json:"replicas"`
		Status   HealthStatus `json:"status"`
		DataSize uint64       `json:"data_size"`
	}
	GetLeaderRequest struct {
		ShardID uint64 `json:"shard_id"`
	}
	GetLeaderResponse struct {
		ShardID  uint64 `json:"shard_id"`
		LeaderID uint64 `json:"leader_id"`
		Address  string `json:"address"`
	}
	AddNodeRequest struct {
		ShardID uint64
		NodeID  uint64
		Address string
		// Identity is the replica's Raft member identity
		// (spki:<sha256-hex>, as `qrdbd node-identity --cert` prints it),
		// or "" to use the identity the cluster already committed for the
		// replica or its host. A node no committed membership names needs it.
		Identity string
	}
	AddNodeResponse struct {
		Success bool
		Message string
	}
	RemoveNodeRequest struct {
		ShardID uint64
		NodeID  uint64
	}
	RemoveNodeResponse struct {
		Success bool
		Message string
	}
	CreateShardRequest struct {
		ShardID        uint64
		InitialMembers map[uint64]string
	}
	CreateShardResponse struct {
		ShardID  uint64
		Status   string
		LeaderID uint64
	}
	RebalanceClusterRequest struct {
		TargetShardCount uint64 `json:"target_shard_count"`
		Strategy         string `json:"strategy"`
	}
	RebalanceClusterResponse struct {
		JobID               string  `json:"job_id"`
		Progress            float64 `json:"progress"`
		EstimatedCompletion int64   `json:"estimated_completion"`
	}
	GetRebalanceProgressRequest struct {
		OperationID string `json:"operation_id"`
	}
	GetRebalanceProgressResponse struct {
		OperationID    string  `json:"operation_id"`
		Status         string  `json:"status"`   // pending, in_progress, completed, failed
		Progress       float64 `json:"progress"` // 0-100
		Message        string  `json:"message"`
		ShardsMoved    int     `json:"shards_moved"`
		TotalShards    int     `json:"total_shards"`
		StartTime      int64   `json:"start_time"`
		ElapsedSeconds float64 `json:"elapsed_seconds"`
		Error          string  `json:"error,omitempty"`
	}
)

// =============================================================================
// Backup Operations Types
// =============================================================================

// Backup Operations Types.
type (
	CreateBackupRequest struct {
		Name          string
		Description   string
		Compression   string
		Encryption    string
		IncludeShards []uint64
	}
	CreateBackupResponse struct {
		BackupID  string
		Size      uint64
		Timestamp int64
		Status    string
	}
	ListBackupsRequest struct {
		Limit  uint64
		Offset uint64
	}
	ListBackupsResponse struct {
		Backups    []*BackupMetadata
		TotalCount uint64
	}
	BackupMetadata struct {
		BackupID    string
		Name        string
		Description string
		Size        uint64
		Timestamp   int64
		Status      string
		Shards      []uint64
	}
	RestoreBackupRequest struct {
		BackupID     string
		TargetShards []uint64
	}
	RestoreBackupResponse struct {
		JobID    string
		Status   string
		Progress float64
	}
	DeleteBackupRequest  struct{ BackupID string }
	DeleteBackupResponse struct {
		Success bool
		Message string
	}
	VerifyBackupRequest  struct{ BackupID string }
	VerifyBackupResponse struct {
		Valid         bool
		ChecksumMatch bool
		Issues        []string
	}
)

// =============================================================================
// Auth Operations Types
// =============================================================================

// Auth Operations Types.
type (
	LoginRequest struct {
		Username string
	}
	LoginResponse struct {
		AccessToken  string
		RefreshToken string
		ExpiresAt    int64
		UserID       string
	}
	LogoutRequest  struct{ AccessToken string }
	LogoutResponse struct {
		Success bool
		Message string
	}
	// RefreshTokenRequest renews a session from its opaque refresh credential.
	// The DPoP fields carry an RFC 9449 proof presented with the request; they
	// are REQUIRED when the session being renewed is sender-constrained.
	RefreshTokenRequest struct {
		RefreshToken string
		DPoPProof    string
		DPoPMethod   string
		DPoPURI      string
	}
	// RefreshTokenResponse carries the renewed access token and the ROTATED
	// refresh credential. Clients MUST persist RefreshToken: the credential
	// they presented is revoked by the renewal. Timestamps are Unix seconds.
	RefreshTokenResponse struct {
		AccessToken      string
		ExpiresAt        int64
		RefreshToken     string
		RefreshExpiresAt int64
		TokenType        string
	}
	ValidateTokenRequest  struct{ AccessToken string }
	ValidateTokenResponse struct {
		Valid    bool
		UserID   string
		Username string
		// TenantID is the tenant the bearer was issued into (its tenant_id
		// claim); empty for a token minted without tenant context.
		TenantID  string
		Roles     []string
		ExpiresAt int64
	}
	// IntrospectTokenRequest carries the token a resource server inspects.
	IntrospectTokenRequest struct{ Token string }
)

// RBAC Operations Types - defined in rbac_types.go with type aliases for backwards compatibility.

// =============================================================================
// Cache Operations Types
// =============================================================================

// Cache Operations Types.
type (
	GetCacheStatsRequest  struct{ EntityType string }
	GetCacheStatsResponse struct {
		HitRate   float64
		Size      uint64
		MaxSize   uint64
		Evictions uint64
		Hits      uint64
		Misses    uint64
	}
	ClearCacheRequest struct {
		EntityType string
		All        bool
	}
	ClearCacheResponse     struct{ EntriesCleared uint64 }
	GetCacheConfigRequest  struct{}
	GetCacheConfigResponse struct {
		L1Size          int
		L1TTL           time.Duration
		CleanupInterval time.Duration
		Enabled         bool
	}
	UpdateCacheConfigRequest struct {
		L1Size          *int
		L1TTL           *time.Duration
		CleanupInterval *time.Duration
	}
	UpdateCacheConfigResponse struct {
		Updated   bool
		NewConfig *GetCacheConfigResponse
	}
)

// =============================================================================
// Health Operations Types
// =============================================================================

// Health Operations Types.
type (
	HealthRequest  struct{ IncludeDetails bool }
	HealthResponse struct {
		Status        HealthStatus
		NodeID        uint64
		Version       string
		UptimeSeconds uint64
		Details       map[string]*ComponentHealth
	}
	ComponentHealth struct {
		Status    HealthStatus
		Message   string
		LastCheck int64
	}
	LivenessRequest   struct{}
	LivenessResponse  struct{ Alive bool }
	ReadinessRequest  struct{}
	ReadinessResponse struct {
		Ready  bool
		Reason string
	}
)

// =============================================================================
// Time-Series Operations Types
// =============================================================================

// Time-Series Operations Types.
type (
	AggregateRequest struct {
		EntityType string
		FieldName  string
		Function   AggregateFunction
		StartTime  int64
		EndTime    int64
		Interval   uint64
	}
	AggregateResponse struct {
		Result    float64
		Count     uint64
		StartTime int64
		EndTime   int64
	}
	TimeSeriesQueryRequest struct {
		EntityType string
		StartTime  int64
		EndTime    int64
		Interval   uint64
		Limit      uint64
	}
	TimeSeriesQueryResponse struct {
		DataPoints    []*TimeSeriesPoint
		TotalCount    uint64
		NextPageToken string
	}
	// TimeSeriesPoint represents a single time-series data point.
	// It contains the entity identifier, timestamp, numeric value, raw data,
	// and optional metadata for enriched querying and filtering.
	TimeSeriesPoint struct {
		// ID is the unique entity identifier for this data point.
		ID uint64
		// Timestamp is when the data point was recorded (Unix nanoseconds).
		Timestamp int64
		// Value is the extracted numeric value for aggregation operations.
		Value float64
		// Data contains the raw serialized entity data.
		Data []byte
		// Metadata contains optional key-value pairs for filtering and annotation.
		Metadata map[string]string
	}
	// New types for timeseries data operations.
	WritePointRequest struct {
		Metric    string            `json:"metric"`
		Tags      map[string]string `json:"tags,omitempty"`
		Value     float64           `json:"value"`
		Timestamp time.Time         `json:"timestamp"`
	}
	WritePointResponse struct {
		Success   bool      `json:"success"`
		Timestamp time.Time `json:"timestamp"`
	}
	WriteBatchRequest struct {
		Points []*WritePointRequest `json:"points"`
	}
	WriteBatchResponse struct {
		Success       bool      `json:"success"`
		PointsWritten int       `json:"points_written"`
		Timestamp     time.Time `json:"timestamp"`
	}
	TSQueryRequest struct {
		Metric      string            `json:"metric"`
		Tags        map[string]string `json:"tags,omitempty"`
		StartTime   time.Time         `json:"start_time"`
		EndTime     time.Time         `json:"end_time"`
		Aggregation string            `json:"aggregation,omitempty"`
		IntervalSec int64             `json:"interval_sec,omitempty"`
		GroupBy     []string          `json:"group_by,omitempty"`
		Limit       int               `json:"limit,omitempty"`
		Percentile  float64           `json:"percentile,omitempty"`
	}
	TSQueryResponse struct {
		Metric          string             `json:"metric"`
		Tags            map[string]string  `json:"tags,omitempty"`
		Points          []*TSDataPoint     `json:"points,omitempty"`
		AggregatedValue *float64           `json:"aggregated_value,omitempty"`
		GroupedResults  []*TSGroupedResult `json:"grouped_results,omitempty"`
		Timestamp       time.Time          `json:"timestamp"`
	}
	TSDataPoint struct {
		Metric    string            `json:"metric"`
		Tags      map[string]string `json:"tags,omitempty"`
		Value     float64           `json:"value"`
		Timestamp time.Time         `json:"timestamp"`
	}
	TSGroupedResult struct {
		Tags            map[string]string `json:"tags,omitempty"`
		Points          []*TSDataPoint    `json:"points,omitempty"`
		AggregatedValue *float64          `json:"aggregated_value,omitempty"`
	}
	TSDeleteRequest struct {
		Metric    string            `json:"metric"`
		Tags      map[string]string `json:"tags,omitempty"`
		StartTime *time.Time        `json:"start_time,omitempty"`
		EndTime   *time.Time        `json:"end_time,omitempty"`
	}
	TSDeleteResponse struct {
		Success   bool      `json:"success"`
		Timestamp time.Time `json:"timestamp"`
	}
	TSGetRetentionPolicyRequest struct {
		Metric string `json:"metric"`
	}
	TSGetRetentionPolicyResponse struct {
		Metric    string             `json:"metric"`
		Policy    *TSRetentionPolicy `json:"policy,omitempty"`
		Timestamp time.Time          `json:"timestamp"`
	}
	TSSetRetentionPolicyRequest struct {
		Metric string             `json:"metric"`
		Policy *TSRetentionPolicy `json:"policy"`
	}
	TSSetRetentionPolicyResponse struct {
		Success   bool      `json:"success"`
		Timestamp time.Time `json:"timestamp"`
	}
	TSRetentionPolicy struct {
		Duration    time.Duration `json:"duration"`
		Resolution  time.Duration `json:"resolution,omitempty"`
		Aggregation string        `json:"aggregation,omitempty"`
	}
	TSListMetricsRequest struct {
		Prefix string            `json:"prefix,omitempty"`
		Tags   map[string]string `json:"tags,omitempty"`
		Limit  int               `json:"limit,omitempty"`
		Cursor string            `json:"cursor,omitempty"`
	}
	TSListMetricsResponse struct {
		Metrics    []string  `json:"metrics"`
		NextCursor string    `json:"next_cursor,omitempty"`
		Timestamp  time.Time `json:"timestamp"`
	}
)

// =============================================================================
// Operations Types
// =============================================================================

// Operations Types.
type (
	MigrateDataRequest struct {
		SourceShard uint64
		TargetShard uint64
		EntityTypes []string
		Concurrency int
	}
	MigrateDataResponse struct {
		MigrationID       string
		Status            string
		EstimatedDuration int64
	}
	GetMigrationProgressRequest  struct{ MigrationID string }
	GetMigrationProgressResponse struct {
		MigrationID   string
		Progress      float64
		Status        string
		ItemsMigrated uint64
		TotalItems    uint64
		ETA           int64
	}
	ReplaceNodeRequest struct {
		ShardID    uint64
		OldNodeID  uint64
		NewNodeID  uint64
		NewAddress string
	}
	ReplaceNodeResponse struct {
		JobID  string
		Status string
	}
	UpgradeNodeRequest struct {
		NodeID        uint64
		TargetVersion string
	}
	UpgradeNodeResponse struct {
		JobID  string
		Status string
	}
	GetUpgradeStatusRequest  struct{ JobID string }
	GetUpgradeStatusResponse struct {
		JobID          string
		Progress       float64
		Status         string
		CurrentVersion string
		TargetVersion  string
	}
)

// =============================================================================
// Service Configuration
// =============================================================================

// ServiceConfig contains configuration for the service facade.
type ServiceConfig struct {
	// NodeID is the unique identifier for this node
	NodeID uint64

	// Mode specifies the server operation mode (standalone, cluster, devcluster)
	Mode string

	// Version is the QuicBaaS version
	Version string

	// Shards is the list of shard IDs this node is responsible for.
	// Used for readiness checks to verify all shards have elected leaders.
	Shards []uint64

	// EnableAuth enables authentication and authorization
	EnableAuth bool

	// EnableMetrics enables Prometheus metrics collection
	EnableMetrics bool

	// EnableTracing enables distributed tracing
	EnableTracing bool

	// LogLevel controls service logging verbosity
	LogLevel string

	// RequestTimeout is the default timeout for operations
	RequestTimeout time.Duration

	// MaxConcurrentRequests limits concurrent requests
	MaxConcurrentRequests int

	// EnableScheduler enables the job scheduler
	EnableScheduler bool

	// SchedulerTickInterval is how often the scheduler checks for jobs (default: 1s)
	SchedulerTickInterval time.Duration

	// SchedulerMaxConcurrentJobs limits parallel job execution (default: 10)
	SchedulerMaxConcurrentJobs int

	// KMSRotationScanInterval controls how often the KMS rotation system job
	// checks for keys whose cryptoperiod has expired (PCI DSS 3.6). It must be
	// a whole number of seconds; zero selects
	// handlers.DefaultKMSRotationScanInterval (5 minutes). Only active when
	// the KMS service and the scheduler are configured.
	KMSRotationScanInterval time.Duration

	// SecretsRotationScanInterval controls how often the secrets rotation
	// system job performs the rotation policies whose next rotation is due.
	// It must be a whole number of seconds; zero selects
	// handlers.DefaultSecretsRotationScanInterval (5 minutes). Only active
	// when the secrets service and the scheduler are configured.
	SecretsRotationScanInterval time.Duration

	// TimeSeriesShardID is the Raft shard used for time series write operations.
	// When zero, defaults to 1.
	TimeSeriesShardID uint64

	// TenantIsolation configures the tenant isolation strategy used when
	// tenants are created or removed. Defaults to keyspace mode.
	TenantIsolation config.TenantIsolationConfig
}

// =============================================================================
// Service Factory
// =============================================================================

// ServiceBuilder builds a QuicBaaS instance.
type ServiceBuilder interface {
	// WithRaftNode sets the Raft node
	WithRaftNode(raftNode interface{}) ServiceBuilder

	// WithStorage sets the storage engine
	WithStorage(storage interface{}) ServiceBuilder

	// WithIndexManager sets the index manager
	WithIndexManager(indexManager interface{}) ServiceBuilder

	// WithCacheManager sets the cache manager
	WithCacheManager(cacheManager interface{}) ServiceBuilder

	// WithBackupManager sets the backup manager
	WithBackupManager(backupManager interface{}) ServiceBuilder

	// WithLogger sets the logger
	WithLogger(logger interface{}) ServiceBuilder

	// WithConfig sets the service configuration
	WithConfig(config *ServiceConfig) ServiceBuilder

	// WithTimeSeriesStorage sets the time series storage
	WithTimeSeriesStorage(tsStorage interface{}) ServiceBuilder

	// WithKMSManager sets the KMS manager for cryptographic operations
	WithKMSManager(kmsManager interface{}) ServiceBuilder

	// WithSecretsManager sets the secrets manager
	WithSecretsManager(secretsManager interface{}) ServiceBuilder

	// WithCryptoService sets the crypto/transit service
	WithCryptoService(cryptoService interface{}) ServiceBuilder

	// WithCryptoManager sets the crypto manager for encryption operations
	WithCryptoManager(cryptoManager interfaces.CryptoManager) ServiceBuilder

	// WithKVStoreProvider sets the KVStore provider for DAO factory
	WithKVStoreProvider(provider func(shardID uint64) interface{}) ServiceBuilder

	// WithSchedulerClustering supplies the leader checker and claim arbiter a
	// clustered scheduler needs. Supplied only on a clustered boot; a
	// standalone boot leaves it unset and gets the single-node scheduler. An
	// incomplete value fails service construction rather than degrading.
	WithSchedulerClustering(clustering *SchedulerClustering) ServiceBuilder

	// WithOrchestrationKVStore sets the store that backs durable orchestration
	// state — projects, agent registrations and tasks. The server passes its
	// tenant-routed store so a barrier-isolated tenant's orchestration records
	// live in that tenant's own barrier. When unset, orchestration state is
	// held in a process-local memory store and does not survive a restart.
	WithOrchestrationKVStore(store interfaces.KVStore) ServiceBuilder

	// WithOrchestrationAuditor routes create/update/delete of orchestration
	// records to the platform audit trail. When unset, those mutations are not
	// audited.
	WithOrchestrationAuditor(auditor orchestration.StoreAuditor) ServiceBuilder

	// WithCAService sets the CA service for PKI operations
	WithCAService(caService interface{}) ServiceBuilder

	// WithDANEPublisher sets the application-supplied DANE TLSA publisher
	// used by Trust().PublishDANE. xkms provides no DNS implementation; when
	// unset, PublishDANE returns a typed not-configured error.
	WithDANEPublisher(publisher DANEPublisher) ServiceBuilder

	// WithContainerAttestor sets the application-supplied container
	// attestor used by Attestation().AttestContainer. QuicBaaS ships the
	// standalone tiers in pkg/orchestration and can fill this seam with
	// NewRuntimeContainerAttestor; a fleet manager such as
	// go-trusted-platform supplies its own. When unset, AttestContainer
	// returns a typed not-configured error.
	WithContainerAttestor(attestor ContainerAttestor) ServiceBuilder

	// WithAttestationTrustAnchors sets the root certificates every
	// attestation verification is anchored against. When unset, only the
	// per-request anchors supplied in the verify options apply.
	WithAttestationTrustAnchors(anchors []*x509.Certificate) ServiceBuilder

	// WithACMEService sets the composed ACME (RFC 8555) servicer that backs
	// Security().ACME(). The servicer is built in pkg/server where the CA,
	// qrdb KVStore (for ACME persistence) and the externally reachable base
	// URL are available. ACME is HTTP-native — REST + embedded only.
	WithACMEService(acmeService ACMEServicer) ServiceBuilder

	// WithCatalogManager sets the catalog manager for object storage
	WithCatalogManager(catalogManager catalog.CatalogManager) ServiceBuilder

	// WithBackendManager sets the backend manager for object storage
	WithBackendManager(backendManager catalog.BackendManager) ServiceBuilder

	// WithObjectStoreConfigResolver sets the config resolver for object storage
	WithObjectStoreConfigResolver(resolver config.ObjectStoreConfigResolver) ServiceBuilder

	// WithWorkerManager sets the worker manager for background tasks
	WithWorkerManager(workerManager *catalog.WorkerManager) ServiceBuilder

	// WithVectorService sets the vector service for vector database operations
	WithVectorService(vectorService VectorService) ServiceBuilder

	// WithMessagingService sets the messaging notification service
	WithMessagingService(messagingService messaging.Service) ServiceBuilder

	// WithEmbeddingManager sets the embedding manager for vector embeddings
	WithEmbeddingManager(embeddingManager interfaces.EmbeddingManager) ServiceBuilder

	// WithQRDBClient sets the qrdb SDK client for core KV operations
	WithQRDBClient(client interface{}) ServiceBuilder

	// WithOllamaService sets the Ollama AI service for model inference
	WithOllamaService(ollamaService OllamaService) ServiceBuilder

	// WithSSHClient sets the SSH key store client behind Security().SSH().
	// The server composes it: embedded over the barrier-sealed store, or
	// REST to a remote xkmsd. When unset every SSH verb fails with
	// *SSHNotConfiguredError. The service owns the client once Build
	// succeeds and closes it on shutdown.
	WithSSHClient(client xkmsssh.Client) ServiceBuilder

	// Build creates the service instance
	Build() (QuicBaaS, error)
}

// NewServiceBuilder creates a new service builder.
func NewServiceBuilder() ServiceBuilder {
	return &serviceBuilder{
		config: &ServiceConfig{
			EnableAuth:            true,
			EnableMetrics:         true,
			EnableTracing:         false,
			LogLevel:              "info",
			RequestTimeout:        30 * time.Second,
			MaxConcurrentRequests: 10000,
		},
	}
}

type serviceBuilder struct {
	raftNode                  interface{}
	storage                   interface{}
	indexManager              interface{}
	cacheManager              interface{}
	backupManager             interface{}
	logger                    interface{}
	config                    *ServiceConfig
	tsStorage                 interface{}
	kmsManager                interface{}
	secretsManager            interface{}
	cryptoService             interface{}
	cryptoManager             interfaces.CryptoManager
	kvStoreProvider           func(shardID uint64) interface{}
	schedulerClustering       *SchedulerClustering
	orchestrationKVStore      interfaces.KVStore
	orchestrationAuditor      orchestration.StoreAuditor
	caService                 interface{}
	danePublisher             DANEPublisher
	containerAttestor         ContainerAttestor
	attestationTrustAnchors   []*x509.Certificate
	acmeService               ACMEServicer
	catalogManager            catalog.CatalogManager
	backendManager            catalog.BackendManager
	objectStoreConfigResolver config.ObjectStoreConfigResolver
	workerManager             *catalog.WorkerManager
	vectorService             VectorService
	messagingService          messaging.Service
	embeddingManager          interfaces.EmbeddingManager
	qrdbClient                interface{}
	ollamaService             OllamaService
	sshClient                 xkmsssh.Client
}

func (b *serviceBuilder) WithRaftNode(raftNode interface{}) ServiceBuilder {
	b.raftNode = raftNode
	return b
}

func (b *serviceBuilder) WithStorage(storage interface{}) ServiceBuilder {
	b.storage = storage
	return b
}

func (b *serviceBuilder) WithIndexManager(indexManager interface{}) ServiceBuilder {
	b.indexManager = indexManager
	return b
}

func (b *serviceBuilder) WithCacheManager(cacheManager interface{}) ServiceBuilder {
	b.cacheManager = cacheManager
	return b
}

func (b *serviceBuilder) WithBackupManager(backupManager interface{}) ServiceBuilder {
	b.backupManager = backupManager
	return b
}

func (b *serviceBuilder) WithLogger(logger interface{}) ServiceBuilder {
	b.logger = logger
	return b
}

func (b *serviceBuilder) WithConfig(config *ServiceConfig) ServiceBuilder {
	b.config = config
	return b
}

func (b *serviceBuilder) WithTimeSeriesStorage(tsStorage interface{}) ServiceBuilder {
	b.tsStorage = tsStorage
	return b
}

func (b *serviceBuilder) WithKMSManager(kmsManager interface{}) ServiceBuilder {
	b.kmsManager = kmsManager
	return b
}

func (b *serviceBuilder) WithSecretsManager(secretsManager interface{}) ServiceBuilder {
	b.secretsManager = secretsManager
	return b
}

func (b *serviceBuilder) WithCryptoService(cryptoService interface{}) ServiceBuilder {
	b.cryptoService = cryptoService
	return b
}

func (b *serviceBuilder) WithCryptoManager(cryptoManager interfaces.CryptoManager) ServiceBuilder {
	b.cryptoManager = cryptoManager
	return b
}

func (b *serviceBuilder) WithKVStoreProvider(provider func(shardID uint64) interface{}) ServiceBuilder {
	b.kvStoreProvider = provider
	return b
}

func (b *serviceBuilder) WithSchedulerClustering(clustering *SchedulerClustering) ServiceBuilder {
	b.schedulerClustering = clustering
	return b
}

func (b *serviceBuilder) WithOrchestrationKVStore(store interfaces.KVStore) ServiceBuilder {
	b.orchestrationKVStore = store
	return b
}

func (b *serviceBuilder) WithOrchestrationAuditor(auditor orchestration.StoreAuditor) ServiceBuilder {
	b.orchestrationAuditor = auditor
	return b
}

func (b *serviceBuilder) WithCAService(caService interface{}) ServiceBuilder {
	b.caService = caService
	return b
}

func (b *serviceBuilder) WithDANEPublisher(publisher DANEPublisher) ServiceBuilder {
	b.danePublisher = publisher
	return b
}

func (b *serviceBuilder) WithContainerAttestor(attestor ContainerAttestor) ServiceBuilder {
	b.containerAttestor = attestor
	return b
}

func (b *serviceBuilder) WithAttestationTrustAnchors(anchors []*x509.Certificate) ServiceBuilder {
	b.attestationTrustAnchors = anchors
	return b
}

func (b *serviceBuilder) WithACMEService(acmeService ACMEServicer) ServiceBuilder {
	b.acmeService = acmeService
	return b
}

func (b *serviceBuilder) WithCatalogManager(catalogManager catalog.CatalogManager) ServiceBuilder {
	b.catalogManager = catalogManager
	return b
}

func (b *serviceBuilder) WithBackendManager(backendManager catalog.BackendManager) ServiceBuilder {
	b.backendManager = backendManager
	return b
}

func (b *serviceBuilder) WithObjectStoreConfigResolver(resolver config.ObjectStoreConfigResolver) ServiceBuilder {
	b.objectStoreConfigResolver = resolver
	return b
}

func (b *serviceBuilder) WithWorkerManager(workerManager *catalog.WorkerManager) ServiceBuilder {
	b.workerManager = workerManager
	return b
}

func (b *serviceBuilder) WithVectorService(vectorService VectorService) ServiceBuilder {
	b.vectorService = vectorService
	return b
}

func (b *serviceBuilder) WithMessagingService(messagingService messaging.Service) ServiceBuilder {
	b.messagingService = messagingService
	return b
}

func (b *serviceBuilder) WithEmbeddingManager(embeddingManager interfaces.EmbeddingManager) ServiceBuilder {
	b.embeddingManager = embeddingManager
	return b
}

func (b *serviceBuilder) WithQRDBClient(client interface{}) ServiceBuilder {
	b.qrdbClient = client
	return b
}

func (b *serviceBuilder) WithOllamaService(ollamaService OllamaService) ServiceBuilder {
	b.ollamaService = ollamaService
	return b
}

func (b *serviceBuilder) WithSSHClient(client xkmsssh.Client) ServiceBuilder {
	b.sshClient = client
	return b
}

func (b *serviceBuilder) Build() (QuicBaaS, error) {
	// Implementation in quicbaas.go
	return newQuicBaaS(b)
}

// =============================================================================
// Streaming Helpers
// =============================================================================

// StreamWriter is a helper interface for streaming responses.
type StreamWriter interface {
	// Send sends a response to the stream
	Send(interface{}) error

	// Close closes the stream
	Close() error
}

// StreamReader is a helper interface for streaming requests.
type StreamReader interface {
	// Recv receives a request from the stream
	Recv() (interface{}, error)

	// Close closes the stream
	Close() error
}
