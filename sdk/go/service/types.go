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

// Package service re-exports the pkg/service surface as type aliases so
// consumers can change imports from:
//
//	"github.com/jeremyhahn/go-quicbaas/pkg/service"
//
// to:
//
//	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
//
// without any type conversion. Because these are aliases and not wrappers,
// changes to pkg/service types propagate to SDK consumers automatically.
//
// # Completeness contract
//
// The guarantee this package makes is closure, not literal totality: every
// exported pkg/service identifier reachable from an alias declared here —
// through an interface method signature, an exported struct field, or a
// constructor signature — is itself aliased here. A consumer that can name a
// type can therefore always name everything it needs to call it. Some
// server-internal pkg/service identifiers (wiring adapters, shard routers,
// DAO factories and the typed errors of subsystems reached only through
// sdk/go.Security()) are deliberately not re-exported.
//
// TestAliasClosure in types_test.go enforces the contract mechanically: it
// fails the build when a new pkg/service type becomes reachable from this
// package without being aliased.
package service

import (
	pkgservice "github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Core Service Interfaces
// =============================================================================

type (
	// QuicBaaS is the unified service facade providing ALL database operations.
	QuicBaaS = pkgservice.QuicBaaS

	// KVService defines basic key-value store operations.
	KVService = pkgservice.KVService

	// BatchService defines atomic batch operations.
	BatchService = pkgservice.BatchService

	// TransactionService defines atomic compare-and-swap transactions.
	TransactionService = pkgservice.TransactionService

	// QueryService defines query and index operations.
	QueryService = pkgservice.QueryService

	// ClusterService defines cluster management operations.
	ClusterService = pkgservice.ClusterService

	// BackupService defines backup and restore operations.
	BackupService = pkgservice.BackupService

	// AuthService defines authentication operations.
	AuthService = pkgservice.AuthService

	// RBACService defines role-based access control operations.
	RBACService = pkgservice.RBACService

	// CacheService defines cache management operations.
	CacheService = pkgservice.CacheService

	// HealthService defines health check operations.
	HealthService = pkgservice.HealthService

	// TimeSeriesService defines time-series operations.
	TimeSeriesService = pkgservice.TimeSeriesService

	// OperationsService defines operational management operations.
	OperationsService = pkgservice.OperationsService

	// KMSService defines KMS operations.
	KMSService = pkgservice.KMSService

	// SecretsService handles secret storage and retrieval with versioning and rotation.
	SecretsService = pkgservice.SecretsService

	// CryptoService defines crypto/transit operations.
	CryptoService = pkgservice.CryptoService

	// VectorService defines vector database operations.
	VectorService = pkgservice.VectorService

	// EmbeddingService provides semantic embedding generation and management.
	EmbeddingService = pkgservice.EmbeddingService

	// OllamaService provides access to Ollama API operations.
	OllamaService = pkgservice.OllamaService

	// CertificateService provides certificate operations.
	CertificateService = pkgservice.CertificateService

	// DynamicSecretsService handles dynamic credential generation.
	DynamicSecretsService = pkgservice.DynamicSecretsService

	// ObjectStorageService provides object storage operations.
	ObjectStorageService = pkgservice.ObjectStorageService

	// ObjectStorageAdminService provides object storage admin operations.
	ObjectStorageAdminService = pkgservice.ObjectStorageAdminService

	// ParameterStoreService provides parameter store operations.
	ParameterStoreService = pkgservice.ParameterStoreService

	// ObjectTransferService provides peer-to-peer object data transfer.
	ObjectTransferService = pkgservice.ObjectTransferService

	// SchedulerService handles job scheduling and execution.
	SchedulerService = pkgservice.SchedulerService

	// SDLCService is the SDLC module's binding service.
	SDLCService = pkgservice.SDLCService

	// BindOptions carries the intent of one SDLCService.Bind request, such
	// as binding a project paused.
	BindOptions = pkgservice.BindOptions

	// ProjectWriter persists a project record. Binding a project to the
	// SDLC module turns it autonomous, and that has to be written back.
	ProjectWriter = pkgservice.ProjectWriter

	// WorkerImageRefresher is the Software Factory worker image refresh loop
	// as the SDLC service reaches it (SDLCService.SetWorkerImageRefresher).
	WorkerImageRefresher = pkgservice.WorkerImageRefresher

	// SSHKeyGetter loads one SSH key's metadata; the SDLC service checks a
	// repo's SSH key reference through it (SDLCServiceConfig.SSHKeys).
	SSHKeyGetter = pkgservice.SSHKeyGetter

	// FIDO2Service provides the tenant-scoped FIDO2/WebAuthn ceremony surface.
	FIDO2Service = pkgservice.FIDO2Service

	// PIVService provides the xkms backend-scoped PIV certificate operations.
	PIVService = pkgservice.PIVService

	// IAMComponents provides IAM components.
	IAMComponents = pkgservice.IAMComponents

	// TenantProvisioner provisions a tenant and its admin in one step.
	TenantProvisioner = pkgservice.TenantProvisioner

	// IAMProvisionTenantRequest is a tenant-provisioning request.
	IAMProvisionTenantRequest = pkgservice.IAMProvisionTenantRequest

	// IAMProvisionTenantResponse is the result of provisioning a tenant.
	IAMProvisionTenantResponse = pkgservice.IAMProvisionTenantResponse

	// IAMTenantAdminEnrollment selects how a provisioned tenant admin enrolls.
	IAMTenantAdminEnrollment = pkgservice.IAMTenantAdminEnrollment
)

// =============================================================================
// Common Types and Enums
// =============================================================================

type (
	// ConsistencyLevel defines read/write consistency guarantees.
	ConsistencyLevel = pkgservice.ConsistencyLevel

	// WriteMode defines write durability guarantees.
	WriteMode = pkgservice.WriteMode

	// HealthStatus defines component health status.
	HealthStatus = pkgservice.HealthStatus

	// AggregateFunction defines time-series aggregation functions.
	AggregateFunction = pkgservice.AggregateFunction

	// CompareTarget defines transaction comparison targets.
	CompareTarget = pkgservice.CompareTarget

	// CompareOperator defines transaction comparison operators.
	CompareOperator = pkgservice.CompareOperator

	// TransactionOpType defines transaction operation types.
	TransactionOpType = pkgservice.TransactionOpType

	// WatchEventType represents the type of watch event.
	WatchEventType = pkgservice.WatchEventType
)

// ConsistencyLevel constants.
const (
	ConsistencyLevelUnspecified  = pkgservice.ConsistencyLevelUnspecified
	ConsistencyLevelEventual     = pkgservice.ConsistencyLevelEventual
	ConsistencyLevelLinearizable = pkgservice.ConsistencyLevelLinearizable
	ConsistencyLevelStrong       = pkgservice.ConsistencyLevelStrong
)

// WriteMode constants.
const (
	WriteModeSync       = pkgservice.WriteModeSync
	WriteModeWALDurable = pkgservice.WriteModeWALDurable
	WriteModeBatched    = pkgservice.WriteModeBatched
)

// HealthStatus constants.
const (
	HealthStatusUnknown   = pkgservice.HealthStatusUnknown
	HealthStatusHealthy   = pkgservice.HealthStatusHealthy
	HealthStatusDegraded  = pkgservice.HealthStatusDegraded
	HealthStatusUnhealthy = pkgservice.HealthStatusUnhealthy
)

// AggregateFunction constants.
const (
	AggregateFunctionUnspecified = pkgservice.AggregateFunctionUnspecified
	AggregateFunctionSum         = pkgservice.AggregateFunctionSum
	AggregateFunctionAvg         = pkgservice.AggregateFunctionAvg
	AggregateFunctionMin         = pkgservice.AggregateFunctionMin
	AggregateFunctionMax         = pkgservice.AggregateFunctionMax
	AggregateFunctionCount       = pkgservice.AggregateFunctionCount
)

// CompareTarget constants.
const (
	CompareTargetUnspecified = pkgservice.CompareTargetUnspecified
	CompareTargetVersion     = pkgservice.CompareTargetVersion
	CompareTargetValue       = pkgservice.CompareTargetValue
	CompareTargetExists      = pkgservice.CompareTargetExists
)

// CompareOperator constants.
const (
	CompareOperatorUnspecified  = pkgservice.CompareOperatorUnspecified
	CompareOperatorEqual        = pkgservice.CompareOperatorEqual
	CompareOperatorNotEqual     = pkgservice.CompareOperatorNotEqual
	CompareOperatorGreater      = pkgservice.CompareOperatorGreater
	CompareOperatorLess         = pkgservice.CompareOperatorLess
	CompareOperatorGreaterEqual = pkgservice.CompareOperatorGreaterEqual
	CompareOperatorLessEqual    = pkgservice.CompareOperatorLessEqual
)

// TransactionOpType constants.
const (
	TransactionOpTypeUnspecified = pkgservice.TransactionOpTypeUnspecified
	TransactionOpTypePut         = pkgservice.TransactionOpTypePut
	TransactionOpTypeGet         = pkgservice.TransactionOpTypeGet
	TransactionOpTypeDelete      = pkgservice.TransactionOpTypeDelete
)

// WatchEventType constants.
const (
	WatchEventTypeUnspecified = pkgservice.WatchEventTypeUnspecified
	WatchEventTypePut         = pkgservice.WatchEventTypePut
	WatchEventTypeDelete      = pkgservice.WatchEventTypeDelete
)

// =============================================================================
// KV Operations Types
// =============================================================================

type (
	PutRequest              = pkgservice.PutRequest
	PutResponse             = pkgservice.PutResponse
	GetRequest              = pkgservice.GetRequest
	GetResponse             = pkgservice.GetResponse
	DeleteRequest           = pkgservice.DeleteRequest
	DeleteResponse          = pkgservice.DeleteResponse
	ListRequest             = pkgservice.ListRequest
	ListResponse            = pkgservice.ListResponse
	Entity                  = pkgservice.Entity
	ListEntityTypesRequest  = pkgservice.ListEntityTypesRequest
	ListEntityTypesResponse = pkgservice.ListEntityTypesResponse
)

// =============================================================================
// Watch Operations Types
// =============================================================================

type (
	WatchRequest  = pkgservice.WatchRequest
	WatchResponse = pkgservice.WatchResponse
	WatchEvent    = pkgservice.WatchEvent
)

// =============================================================================
// Batch Operations Types
// =============================================================================

type (
	BatchPutRequest     = pkgservice.BatchPutRequest
	BatchPutResponse    = pkgservice.BatchPutResponse
	BatchGetRequest     = pkgservice.BatchGetRequest
	BatchGetResponse    = pkgservice.BatchGetResponse
	BatchDeleteRequest  = pkgservice.BatchDeleteRequest
	BatchDeleteResponse = pkgservice.BatchDeleteResponse
)

// =============================================================================
// Transaction Operations Types
// =============================================================================

type (
	TransactionRequest    = pkgservice.TransactionRequest
	TransactionResponse   = pkgservice.TransactionResponse
	CompareCondition      = pkgservice.CompareCondition
	TransactionOp         = pkgservice.TransactionOp
	TransactionOpResponse = pkgservice.TransactionOpResponse
)

// =============================================================================
// Query Operations Types
// =============================================================================

type (
	QueryRequest  = pkgservice.QueryRequest
	QueryResponse = pkgservice.QueryResponse
	ScanRequest   = pkgservice.ScanRequest
	ScanResponse  = pkgservice.ScanResponse
)

// =============================================================================
// Cluster Operations Types
// =============================================================================

type (
	ClusterStatusRequest         = pkgservice.ClusterStatusRequest
	ClusterStatusResponse        = pkgservice.ClusterStatusResponse
	NodeInfo                     = pkgservice.NodeInfo
	ShardInfo                    = pkgservice.ShardInfo
	GetLeaderRequest             = pkgservice.GetLeaderRequest
	GetLeaderResponse            = pkgservice.GetLeaderResponse
	AddNodeRequest               = pkgservice.AddNodeRequest
	AddNodeResponse              = pkgservice.AddNodeResponse
	RemoveNodeRequest            = pkgservice.RemoveNodeRequest
	RemoveNodeResponse           = pkgservice.RemoveNodeResponse
	CreateShardRequest           = pkgservice.CreateShardRequest
	CreateShardResponse          = pkgservice.CreateShardResponse
	RebalanceClusterRequest      = pkgservice.RebalanceClusterRequest
	RebalanceClusterResponse     = pkgservice.RebalanceClusterResponse
	GetRebalanceProgressRequest  = pkgservice.GetRebalanceProgressRequest
	GetRebalanceProgressResponse = pkgservice.GetRebalanceProgressResponse
)

// =============================================================================
// Backup Operations Types
// =============================================================================

type (
	CreateBackupRequest   = pkgservice.CreateBackupRequest
	CreateBackupResponse  = pkgservice.CreateBackupResponse
	ListBackupsRequest    = pkgservice.ListBackupsRequest
	ListBackupsResponse   = pkgservice.ListBackupsResponse
	BackupMetadata        = pkgservice.BackupMetadata
	RestoreBackupRequest  = pkgservice.RestoreBackupRequest
	RestoreBackupResponse = pkgservice.RestoreBackupResponse
	DeleteBackupRequest   = pkgservice.DeleteBackupRequest
	DeleteBackupResponse  = pkgservice.DeleteBackupResponse
	VerifyBackupRequest   = pkgservice.VerifyBackupRequest
	VerifyBackupResponse  = pkgservice.VerifyBackupResponse
)

// =============================================================================
// Auth Operations Types (Basic)
// =============================================================================

type (
	LoginRequest          = pkgservice.LoginRequest
	LoginResponse         = pkgservice.LoginResponse
	LogoutRequest         = pkgservice.LogoutRequest
	LogoutResponse        = pkgservice.LogoutResponse
	RefreshTokenRequest   = pkgservice.RefreshTokenRequest
	RefreshTokenResponse  = pkgservice.RefreshTokenResponse
	ValidateTokenRequest  = pkgservice.ValidateTokenRequest
	ValidateTokenResponse = pkgservice.ValidateTokenResponse
	// IntrospectTokenRequest carries the token an IntrospectToken call inspects.
	IntrospectTokenRequest = pkgservice.IntrospectTokenRequest
)

// =============================================================================
// Auth Extended Types (MFA, OIDC, Sessions, etc.)
// =============================================================================

type (
	// MFAType represents the type of multi-factor authentication.
	MFAType = pkgservice.MFAType

	// MFAChallenge contains MFA verification requirements.
	MFAChallenge = pkgservice.MFAChallenge

	// ExtendedLoginRequest contains credentials for authentication with MFA support.
	ExtendedLoginRequest = pkgservice.ExtendedLoginRequest

	// ExtendedLoginResponse contains the authentication tokens with MFA support.
	ExtendedLoginResponse = pkgservice.ExtendedLoginResponse

	// ExtendedRefreshTokenResponse contains the new access token with rotation support.
	ExtendedRefreshTokenResponse = pkgservice.ExtendedRefreshTokenResponse

	// TokenClaims contains the validated token claims.
	TokenClaims = pkgservice.TokenClaims

	// ProviderType represents the type of identity provider.
	ProviderType = pkgservice.ProviderType

	// BeginFederatedLoginRequest initiates an OIDC/OAuth2 flow.
	BeginFederatedLoginRequest = pkgservice.BeginFederatedLoginRequest

	// BeginFederatedLoginResponse contains the authorization URL.
	BeginFederatedLoginResponse = pkgservice.BeginFederatedLoginResponse

	// HandleFederatedCallbackRequest processes the IdP callback.
	HandleFederatedCallbackRequest = pkgservice.HandleFederatedCallbackRequest

	// HandleFederatedCallbackResponse contains the authentication result.
	HandleFederatedCallbackResponse = pkgservice.HandleFederatedCallbackResponse

	// AuthProvider represents an identity provider configuration.
	AuthProvider = pkgservice.AuthProvider

	// AuthListProvidersResponse contains the list of providers.
	AuthListProvidersResponse = pkgservice.AuthListProvidersResponse

	// AuthSession represents a user session.
	AuthSession = pkgservice.AuthSession

	// AuthListSessionsResponse contains the list of sessions.
	AuthListSessionsResponse = pkgservice.AuthListSessionsResponse

	// TokenIntrospection contains detailed token information.
	TokenIntrospection = pkgservice.TokenIntrospection

	// JWKS represents a JSON Web Key Set.
	JWKS = pkgservice.JWKS

	// JWK represents a JSON Web Key.
	JWK = pkgservice.JWK

	// OpenIDConfiguration represents the OIDC discovery document.
	OpenIDConfiguration = pkgservice.OpenIDConfiguration
)

// MFAType constants.
const (
	MFATypeTOTP       = pkgservice.MFATypeTOTP
	MFATypeWebAuthn   = pkgservice.MFATypeWebAuthn
	MFATypeSMS        = pkgservice.MFATypeSMS
	MFATypeEmail      = pkgservice.MFATypeEmail
	MFATypeBackupCode = pkgservice.MFATypeBackupCode
)

// ProviderType constants.
const (
	ProviderTypeUnspecified = pkgservice.ProviderTypeUnspecified
	ProviderTypeOIDC        = pkgservice.ProviderTypeOIDC
	ProviderTypeSAML        = pkgservice.ProviderTypeSAML
	ProviderTypeOAuth2      = pkgservice.ProviderTypeOAuth2
)

// =============================================================================
// Cache Operations Types
// =============================================================================

type (
	GetCacheStatsRequest      = pkgservice.GetCacheStatsRequest
	GetCacheStatsResponse     = pkgservice.GetCacheStatsResponse
	ClearCacheRequest         = pkgservice.ClearCacheRequest
	ClearCacheResponse        = pkgservice.ClearCacheResponse
	GetCacheConfigRequest     = pkgservice.GetCacheConfigRequest
	GetCacheConfigResponse    = pkgservice.GetCacheConfigResponse
	UpdateCacheConfigRequest  = pkgservice.UpdateCacheConfigRequest
	UpdateCacheConfigResponse = pkgservice.UpdateCacheConfigResponse
)

// =============================================================================
// Health Operations Types
// =============================================================================

type (
	HealthRequest     = pkgservice.HealthRequest
	HealthResponse    = pkgservice.HealthResponse
	ComponentHealth   = pkgservice.ComponentHealth
	LivenessRequest   = pkgservice.LivenessRequest
	LivenessResponse  = pkgservice.LivenessResponse
	ReadinessRequest  = pkgservice.ReadinessRequest
	ReadinessResponse = pkgservice.ReadinessResponse
)

// =============================================================================
// TimeSeries Operations Types
// =============================================================================

type (
	AggregateRequest             = pkgservice.AggregateRequest
	AggregateResponse            = pkgservice.AggregateResponse
	TimeSeriesQueryRequest       = pkgservice.TimeSeriesQueryRequest
	TimeSeriesQueryResponse      = pkgservice.TimeSeriesQueryResponse
	TimeSeriesPoint              = pkgservice.TimeSeriesPoint
	WritePointRequest            = pkgservice.WritePointRequest
	WritePointResponse           = pkgservice.WritePointResponse
	WriteBatchRequest            = pkgservice.WriteBatchRequest
	WriteBatchResponse           = pkgservice.WriteBatchResponse
	TSQueryRequest               = pkgservice.TSQueryRequest
	TSQueryResponse              = pkgservice.TSQueryResponse
	TSDataPoint                  = pkgservice.TSDataPoint
	TSGroupedResult              = pkgservice.TSGroupedResult
	TSDeleteRequest              = pkgservice.TSDeleteRequest
	TSDeleteResponse             = pkgservice.TSDeleteResponse
	TSGetRetentionPolicyRequest  = pkgservice.TSGetRetentionPolicyRequest
	TSGetRetentionPolicyResponse = pkgservice.TSGetRetentionPolicyResponse
	TSSetRetentionPolicyRequest  = pkgservice.TSSetRetentionPolicyRequest
	TSSetRetentionPolicyResponse = pkgservice.TSSetRetentionPolicyResponse
	TSRetentionPolicy            = pkgservice.TSRetentionPolicy
	TSListMetricsRequest         = pkgservice.TSListMetricsRequest
	TSListMetricsResponse        = pkgservice.TSListMetricsResponse
)

// =============================================================================
// Operations Types
// =============================================================================

type (
	MigrateDataRequest           = pkgservice.MigrateDataRequest
	MigrateDataResponse          = pkgservice.MigrateDataResponse
	GetMigrationProgressRequest  = pkgservice.GetMigrationProgressRequest
	GetMigrationProgressResponse = pkgservice.GetMigrationProgressResponse
	ReplaceNodeRequest           = pkgservice.ReplaceNodeRequest
	ReplaceNodeResponse          = pkgservice.ReplaceNodeResponse
	UpgradeNodeRequest           = pkgservice.UpgradeNodeRequest
	UpgradeNodeResponse          = pkgservice.UpgradeNodeResponse
	GetUpgradeStatusRequest      = pkgservice.GetUpgradeStatusRequest
	GetUpgradeStatusResponse     = pkgservice.GetUpgradeStatusResponse
)

// =============================================================================
// KMS Types and Constants
// =============================================================================

type (
	// KeyAlgorithm specifies the cryptographic algorithm for a key.
	KeyAlgorithm = pkgservice.KeyAlgorithm

	// KeyUsage specifies permitted operations for a key.
	KeyUsage = pkgservice.KeyUsage

	// KeyStatus represents the lifecycle state of a key.
	KeyStatus = pkgservice.KeyStatus

	// HashAlgorithm specifies a cryptographic hash function.
	HashAlgorithm = pkgservice.HashAlgorithm

	// KeyHandle is an opaque reference to a key.
	KeyHandle = pkgservice.KeyHandle

	// KeyMetadata contains non-sensitive metadata about a key.
	KeyMetadata = pkgservice.KeyMetadata

	// KeyPolicy defines access control and usage policies for a key.
	KeyPolicy = pkgservice.KeyPolicy

	// RotationPolicy defines automatic key rotation behavior.
	RotationPolicy = pkgservice.RotationPolicy

	// KeyVersionInfo represents information about a specific key version.
	KeyVersionInfo = pkgservice.KeyVersionInfo
)

// KeyAlgorithm constants.
const (
	KeyAlgAES128GCM         = pkgservice.KeyAlgAES128GCM
	KeyAlgAES192GCM         = pkgservice.KeyAlgAES192GCM
	KeyAlgAES256GCM         = pkgservice.KeyAlgAES256GCM
	KeyAlgChaCha20Poly1305  = pkgservice.KeyAlgChaCha20Poly1305
	KeyAlgXChaCha20Poly1305 = pkgservice.KeyAlgXChaCha20Poly1305
	KeyAlgRSA2048           = pkgservice.KeyAlgRSA2048
	KeyAlgRSA3072           = pkgservice.KeyAlgRSA3072
	KeyAlgRSA4096           = pkgservice.KeyAlgRSA4096
	KeyAlgECDSAP256         = pkgservice.KeyAlgECDSAP256
	KeyAlgECDSAP384         = pkgservice.KeyAlgECDSAP384
	KeyAlgECDSAP521         = pkgservice.KeyAlgECDSAP521
	KeyAlgEd25519           = pkgservice.KeyAlgEd25519
	KeyAlgEd448             = pkgservice.KeyAlgEd448
	KeyAlgX25519            = pkgservice.KeyAlgX25519
	KeyAlgX448              = pkgservice.KeyAlgX448
	KeyAlgHMACSHA256        = pkgservice.KeyAlgHMACSHA256
	KeyAlgHMACSHA384        = pkgservice.KeyAlgHMACSHA384
	KeyAlgHMACSHA512        = pkgservice.KeyAlgHMACSHA512
)

// KeyUsage constants.
const (
	KeyUsageEncrypt     = pkgservice.KeyUsageEncrypt
	KeyUsageDecrypt     = pkgservice.KeyUsageDecrypt
	KeyUsageSign        = pkgservice.KeyUsageSign
	KeyUsageVerify      = pkgservice.KeyUsageVerify
	KeyUsageWrapKey     = pkgservice.KeyUsageWrapKey
	KeyUsageDeriveKey   = pkgservice.KeyUsageDeriveKey
	KeyUsageGenerateMAC = pkgservice.KeyUsageGenerateMAC
	KeyUsageVerifyMAC   = pkgservice.KeyUsageVerifyMAC
)

// KeyStatus constants.
const (
	KeyStatusEnabled         = pkgservice.KeyStatusEnabled
	KeyStatusDisabled        = pkgservice.KeyStatusDisabled
	KeyStatusPendingDeletion = pkgservice.KeyStatusPendingDeletion
	KeyStatusDeleted         = pkgservice.KeyStatusDeleted
	KeyStatusRotating        = pkgservice.KeyStatusRotating
	KeyStatusCompromised     = pkgservice.KeyStatusCompromised
)

// HashAlgorithm constants.
const (
	HashAlgSHA256     = pkgservice.HashAlgSHA256
	HashAlgSHA384     = pkgservice.HashAlgSHA384
	HashAlgSHA512     = pkgservice.HashAlgSHA512
	HashAlgSHA3_256   = pkgservice.HashAlgSHA3_256
	HashAlgSHA3_384   = pkgservice.HashAlgSHA3_384
	HashAlgSHA3_512   = pkgservice.HashAlgSHA3_512
	HashAlgBLAKE2b256 = pkgservice.HashAlgBLAKE2b256
	HashAlgBLAKE2b512 = pkgservice.HashAlgBLAKE2b512
)

// =============================================================================
// KMS Request/Response Types
// =============================================================================

type (
	GenerateKeyRequest              = pkgservice.GenerateKeyRequest
	GenerateKeyResponse             = pkgservice.GenerateKeyResponse
	ImportKeyRequest                = pkgservice.ImportKeyRequest
	ImportKeyResponse               = pkgservice.ImportKeyResponse
	GetKeyRequest                   = pkgservice.GetKeyRequest
	GetKeyResponse                  = pkgservice.GetKeyResponse
	ListKeysRequest                 = pkgservice.ListKeysRequest
	ListKeysResponse                = pkgservice.ListKeysResponse
	RotateKeyRequest                = pkgservice.RotateKeyRequest
	RotateKeyResponse               = pkgservice.RotateKeyResponse
	ListKeyVersionsRequest          = pkgservice.ListKeyVersionsRequest
	ListKeyVersionsResponse         = pkgservice.ListKeyVersionsResponse
	UpdateKeyStatusRequest          = pkgservice.UpdateKeyStatusRequest
	UpdateKeyStatusResponse         = pkgservice.UpdateKeyStatusResponse
	RevokeKeyRequest                = pkgservice.RevokeKeyRequest
	RevokeKeyResponse               = pkgservice.RevokeKeyResponse
	DeleteKeyRequest                = pkgservice.DeleteKeyRequest
	DeleteKeyResponse               = pkgservice.DeleteKeyResponse
	GetKeyMetadataRequest           = pkgservice.GetKeyMetadataRequest
	GetKeyMetadataResponse          = pkgservice.GetKeyMetadataResponse
	UpdateKeyMetadataRequest        = pkgservice.UpdateKeyMetadataRequest
	UpdateKeyMetadataResponse       = pkgservice.UpdateKeyMetadataResponse
	SetKeyPolicyRequest             = pkgservice.SetKeyPolicyRequest
	SetKeyPolicyResponse            = pkgservice.SetKeyPolicyResponse
	GetKeyPolicyRequest             = pkgservice.GetKeyPolicyRequest
	GetKeyPolicyResponse            = pkgservice.GetKeyPolicyResponse
	DeleteKeyPolicyRequest          = pkgservice.DeleteKeyPolicyRequest
	DeleteKeyPolicyResponse         = pkgservice.DeleteKeyPolicyResponse
	SetKeyRotationPolicyRequest     = pkgservice.SetKeyRotationPolicyRequest
	SetKeyRotationPolicyResponse    = pkgservice.SetKeyRotationPolicyResponse
	GetKeyRotationPolicyRequest     = pkgservice.GetKeyRotationPolicyRequest
	GetKeyRotationPolicyResponse    = pkgservice.GetKeyRotationPolicyResponse
	DeleteKeyRotationPolicyRequest  = pkgservice.DeleteKeyRotationPolicyRequest
	DeleteKeyRotationPolicyResponse = pkgservice.DeleteKeyRotationPolicyResponse
	ExportKeyRequest                = pkgservice.ExportKeyRequest
	ExportKeyResponse               = pkgservice.ExportKeyResponse
	UnwrapKeyRequest                = pkgservice.UnwrapKeyRequest
	UnwrapKeyResponse               = pkgservice.UnwrapKeyResponse
	SetKeySafetyRequest             = pkgservice.SetKeySafetyRequest
	SetKeySafetyResponse            = pkgservice.SetKeySafetyResponse
	SetDefaultKeyRequest            = pkgservice.SetDefaultKeyRequest
	SetDefaultKeyResponse           = pkgservice.SetDefaultKeyResponse
	ClearKeySafetyRequest           = pkgservice.ClearKeySafetyRequest
	ClearKeySafetyResponse          = pkgservice.ClearKeySafetyResponse
	GetPublicKeyRequest             = pkgservice.GetPublicKeyRequest
	GetPublicKeyResponse            = pkgservice.GetPublicKeyResponse
	ClearDEKSafetyRequest           = pkgservice.ClearDEKSafetyRequest
	ClearDEKSafetyResponse          = pkgservice.ClearDEKSafetyResponse
)

// =============================================================================
// Secrets Types and Constants
// =============================================================================

type (
	// Secret represents a versioned secret with metadata.
	Secret = pkgservice.Secret

	// SecretVersion represents a specific version of a secret.
	SecretVersion = pkgservice.SecretVersion

	// SecretRotationPolicy defines automatic secret rotation behavior.
	SecretRotationPolicy = pkgservice.SecretRotationPolicy

	// SecretStatus represents the status of a secret.
	SecretStatus = pkgservice.SecretStatus

	// SecretChangeType indicates what kind of change occurred.
	SecretChangeType = pkgservice.SecretChangeType

	// SecretChange represents a change to a secret.
	SecretChange = pkgservice.SecretChange
)

// SecretStatus constants.
const (
	SecretStatusEnabled  = pkgservice.SecretStatusEnabled
	SecretStatusDisabled = pkgservice.SecretStatusDisabled
	SecretStatusDeleted  = pkgservice.SecretStatusDeleted
)

// SecretChangeType constants.
const (
	SecretChangeCreated = pkgservice.SecretChangeCreated
	SecretChangeUpdated = pkgservice.SecretChangeUpdated
	SecretChangeDeleted = pkgservice.SecretChangeDeleted
)

// =============================================================================
// Secrets Request/Response Types
// =============================================================================

type (
	StoreSecretRequest         = pkgservice.StoreSecretRequest
	StoreSecretResponse        = pkgservice.StoreSecretResponse
	EnsureSecretRequest        = pkgservice.EnsureSecretRequest
	EnsureSecretResponse       = pkgservice.EnsureSecretResponse
	GetSecretRequest           = pkgservice.GetSecretRequest
	GetSecretResponse          = pkgservice.GetSecretResponse
	ListSecretsRequest         = pkgservice.ListSecretsRequest
	ListSecretsResponse        = pkgservice.ListSecretsResponse
	DeleteSecretRequest        = pkgservice.DeleteSecretRequest
	DeleteSecretResponse       = pkgservice.DeleteSecretResponse
	GetSecretVersionRequest    = pkgservice.GetSecretVersionRequest
	GetSecretVersionResponse   = pkgservice.GetSecretVersionResponse
	ListSecretVersionsRequest  = pkgservice.ListSecretVersionsRequest
	ListSecretVersionsResponse = pkgservice.ListSecretVersionsResponse
	RollbackSecretRequest      = pkgservice.RollbackSecretRequest
	RollbackSecretResponse     = pkgservice.RollbackSecretResponse
	RotateSecretRequest        = pkgservice.RotateSecretRequest
	RotateSecretResponse       = pkgservice.RotateSecretResponse
	SetRotationPolicyRequest   = pkgservice.SetRotationPolicyRequest
	SetRotationPolicyResponse  = pkgservice.SetRotationPolicyResponse
	GetRotationPolicyRequest   = pkgservice.GetRotationPolicyRequest
	GetRotationPolicyResponse  = pkgservice.GetRotationPolicyResponse
	UpdateSecretStatusRequest  = pkgservice.UpdateSecretStatusRequest
	UpdateSecretStatusResponse = pkgservice.UpdateSecretStatusResponse
	GetSecretsByPrefixRequest  = pkgservice.GetSecretsByPrefixRequest
	GetSecretsByPrefixResponse = pkgservice.GetSecretsByPrefixResponse
	WatchSecretsRequest        = pkgservice.WatchSecretsRequest
	ReencryptionProgress       = pkgservice.ReencryptionProgress
)

// =============================================================================
// Crypto/Transit Request/Response Types
// =============================================================================

type (
	EncryptRequest                 = pkgservice.EncryptRequest
	EncryptResponse                = pkgservice.EncryptResponse
	DecryptRequest                 = pkgservice.DecryptRequest
	DecryptResponse                = pkgservice.DecryptResponse
	RewrapRequest                  = pkgservice.RewrapRequest
	RewrapResponse                 = pkgservice.RewrapResponse
	BatchEncryptItem               = pkgservice.BatchEncryptItem
	EncryptBatchRequest            = pkgservice.EncryptBatchRequest
	BatchEncryptResult             = pkgservice.BatchEncryptResult
	EncryptBatchResponse           = pkgservice.EncryptBatchResponse
	BatchDecryptItem               = pkgservice.BatchDecryptItem
	DecryptBatchRequest            = pkgservice.DecryptBatchRequest
	BatchDecryptResult             = pkgservice.BatchDecryptResult
	DecryptBatchResponse           = pkgservice.DecryptBatchResponse
	GenerateDataKeyRequest         = pkgservice.GenerateDataKeyRequest
	GenerateDataKeyResponse        = pkgservice.GenerateDataKeyResponse
	GenerateWrappedDataKeyRequest  = pkgservice.GenerateWrappedDataKeyRequest
	GenerateWrappedDataKeyResponse = pkgservice.GenerateWrappedDataKeyResponse
	SignRequest                    = pkgservice.SignRequest
	SignResponse                   = pkgservice.SignResponse
	VerifyRequest                  = pkgservice.VerifyRequest
	VerifyResponse                 = pkgservice.VerifyResponse
	HMACRequest                    = pkgservice.HMACRequest
	HMACResponse                   = pkgservice.HMACResponse
	VerifyHMACRequest              = pkgservice.VerifyHMACRequest
	VerifyHMACResponse             = pkgservice.VerifyHMACResponse
	GenerateRandomRequest          = pkgservice.GenerateRandomRequest
	GenerateRandomResponse         = pkgservice.GenerateRandomResponse
	HashRequest                    = pkgservice.HashRequest
	HashResponse                   = pkgservice.HashResponse
)

// =============================================================================
// Seal Types
// =============================================================================

type (
	SealStatusResponse          = pkgservice.SealStatusResponse
	SealInitRequest             = pkgservice.SealInitRequest
	SealInitResponse            = pkgservice.SealInitResponse
	UnsealRequest               = pkgservice.UnsealRequest
	UnsealResponse              = pkgservice.UnsealResponse
	RekeyRequest                = pkgservice.RekeyRequest
	RekeyResponse               = pkgservice.RekeyResponse
	GenerateRecoveryKeyRequest  = pkgservice.GenerateRecoveryKeyRequest
	GenerateRecoveryKeyResponse = pkgservice.GenerateRecoveryKeyResponse
	DeleteRecoveryKeyRequest    = pkgservice.DeleteRecoveryKeyRequest
	DeleteRecoveryKeyResponse   = pkgservice.DeleteRecoveryKeyResponse
	GenerateRootTokenRequest    = pkgservice.GenerateRootTokenRequest
	GenerateRootTokenResponse   = pkgservice.GenerateRootTokenResponse
)

// =============================================================================
// DEK Types and Constants
// =============================================================================

type (
	// DEKStatus represents the status of a DEK.
	DEKStatus = pkgservice.DEKStatus

	// DEKMetadata contains metadata about a Data Encryption Key.
	DEKMetadata = pkgservice.DEKMetadata

	// DEKSafetyConfig contains AEAD safety configuration for a DEK.
	DEKSafetyConfig = pkgservice.DEKSafetyConfig
)

// DEKStatus constants.
const (
	DEKStatusActive   = pkgservice.DEKStatusActive
	DEKStatusDisabled = pkgservice.DEKStatusDisabled
	DEKStatusDeleted  = pkgservice.DEKStatusDeleted
	DEKStatusRotating = pkgservice.DEKStatusRotating
)

// =============================================================================
// DEK Request/Response Types
// =============================================================================

type (
	CreateDEKRequest      = pkgservice.CreateDEKRequest
	CreateDEKResponse     = pkgservice.CreateDEKResponse
	GetDEKRequest         = pkgservice.GetDEKRequest
	GetDEKResponse        = pkgservice.GetDEKResponse
	ListDEKsRequest       = pkgservice.ListDEKsRequest
	ListDEKsResponse      = pkgservice.ListDEKsResponse
	RotateDEKRequest      = pkgservice.RotateDEKRequest
	RotateDEKResponse     = pkgservice.RotateDEKResponse
	DeleteDEKRequest      = pkgservice.DeleteDEKRequest
	DeleteDEKResponse     = pkgservice.DeleteDEKResponse
	GetDEKStatusRequest   = pkgservice.GetDEKStatusRequest
	GetDEKStatusResponse  = pkgservice.GetDEKStatusResponse
	SetDefaultDEKRequest  = pkgservice.SetDefaultDEKRequest
	SetDefaultDEKResponse = pkgservice.SetDefaultDEKResponse
	SetDEKSafetyRequest   = pkgservice.SetDEKSafetyRequest
	SetDEKSafetyResponse  = pkgservice.SetDEKSafetyResponse
)

// =============================================================================
// Safety Types
// =============================================================================

type (
	SafetyCheckItem      = pkgservice.SafetyCheckItem
	SafetyCheckRequest   = pkgservice.SafetyCheckRequest
	SafetyCheckResponse  = pkgservice.SafetyCheckResponse
	SafetyReportRequest  = pkgservice.SafetyReportRequest
	SafetyReportResponse = pkgservice.SafetyReportResponse
	AEADServerConfig     = pkgservice.AEADServerConfig
	AEADSafetyConfigDTO  = pkgservice.AEADSafetyConfigDTO

	GetReencryptionProgressRequest  = pkgservice.GetReencryptionProgressRequest
	GetReencryptionProgressResponse = pkgservice.GetReencryptionProgressResponse
)

// =============================================================================
// Backend Types
// =============================================================================

type (
	BackendStatusResponse = pkgservice.BackendStatusResponse
	BackendTestResponse   = pkgservice.BackendTestResponse
)

// =============================================================================
// Embedding Types and Constants
// =============================================================================

type (
	// EmbeddingProvider specifies the embedding provider type.
	EmbeddingProvider = pkgservice.EmbeddingProvider

	// EmbeddingInputType specifies the type of text being embedded.
	EmbeddingInputType = pkgservice.EmbeddingInputType

	// EmbeddingStatus indicates provider availability status.
	EmbeddingStatus = pkgservice.EmbeddingStatus

	// EmbeddingMetadata contains information about an embedding request.
	EmbeddingMetadata = pkgservice.EmbeddingMetadata

	// ProviderInfo contains detailed information about an embedding provider.
	ProviderInfo = pkgservice.ProviderInfo

	// EmbeddingCacheConfig contains cache configuration settings.
	EmbeddingCacheConfig = pkgservice.EmbeddingCacheConfig
)

// EmbeddingProvider constants.
const (
	EmbeddingProviderVoyage   = pkgservice.EmbeddingProviderVoyage
	EmbeddingProviderOpenAI   = pkgservice.EmbeddingProviderOpenAI
	EmbeddingProviderOllama   = pkgservice.EmbeddingProviderOllama
	EmbeddingProviderLMStudio = pkgservice.EmbeddingProviderLMStudio

	// EmbeddingProviderNPU selects the Intel NPU via the OpenVINO sidecar.
	EmbeddingProviderNPU = pkgservice.EmbeddingProviderNPU
)

// EmbeddingInputType constants.
const (
	EmbeddingInputTypeDocument = pkgservice.EmbeddingInputTypeDocument
	EmbeddingInputTypeQuery    = pkgservice.EmbeddingInputTypeQuery
	EmbeddingInputTypeCode     = pkgservice.EmbeddingInputTypeCode
)

// EmbeddingStatus constants.
const (
	EmbeddingStatusAvailable   = pkgservice.EmbeddingStatusAvailable
	EmbeddingStatusUnavailable = pkgservice.EmbeddingStatusUnavailable
	EmbeddingStatusRateLimited = pkgservice.EmbeddingStatusRateLimited
	EmbeddingStatusError       = pkgservice.EmbeddingStatusError
)

// =============================================================================
// Embedding Request/Response Types
// =============================================================================

type (
	GenerateEmbeddingRequest        = pkgservice.GenerateEmbeddingRequest
	GenerateEmbeddingResponse       = pkgservice.GenerateEmbeddingResponse
	BatchEmbeddingItem              = pkgservice.BatchEmbeddingItem
	BatchGenerateEmbeddingsRequest  = pkgservice.BatchGenerateEmbeddingsRequest
	BatchEmbeddingResult            = pkgservice.BatchEmbeddingResult
	BatchGenerateEmbeddingsResponse = pkgservice.BatchGenerateEmbeddingsResponse
	ListProvidersRequest            = pkgservice.ListProvidersRequest
	ListProvidersResponse           = pkgservice.ListProvidersResponse
	GetProviderInfoRequest          = pkgservice.GetProviderInfoRequest
	GetProviderInfoResponse         = pkgservice.GetProviderInfoResponse
	SetDefaultProviderRequest       = pkgservice.SetDefaultProviderRequest
	SetDefaultProviderResponse      = pkgservice.SetDefaultProviderResponse
	GetEmbeddingCacheStatsRequest   = pkgservice.GetEmbeddingCacheStatsRequest
	GetEmbeddingCacheStatsResponse  = pkgservice.GetEmbeddingCacheStatsResponse
	ClearEmbeddingCacheRequest      = pkgservice.ClearEmbeddingCacheRequest
	ClearEmbeddingCacheResponse     = pkgservice.ClearEmbeddingCacheResponse
)

// =============================================================================
// Scheduler Types and Constants
// =============================================================================

type (
	// SchedulerJobType defines the scheduling type.
	SchedulerJobType = pkgservice.SchedulerJobType

	// SchedulerJobStatus defines the job lifecycle state.
	SchedulerJobStatus = pkgservice.SchedulerJobStatus

	// SchedulerOwnerType defines job ownership.
	SchedulerOwnerType = pkgservice.SchedulerOwnerType

	// SchedulerJob represents a scheduled job.
	SchedulerJob = pkgservice.SchedulerJob

	// SchedulerJobExecution represents a job execution record.
	SchedulerJobExecution = pkgservice.SchedulerJobExecution

	// SchedulerStats contains scheduler statistics.
	SchedulerStats = pkgservice.SchedulerStats
)

// SchedulerJobType constants.
const (
	SchedulerJobTypeOneShot   = pkgservice.SchedulerJobTypeOneShot
	SchedulerJobTypeRecurring = pkgservice.SchedulerJobTypeRecurring
	SchedulerJobTypeDelayed   = pkgservice.SchedulerJobTypeDelayed
)

// SchedulerJobStatus constants.
const (
	SchedulerJobStatusPending   = pkgservice.SchedulerJobStatusPending
	SchedulerJobStatusRunning   = pkgservice.SchedulerJobStatusRunning
	SchedulerJobStatusCompleted = pkgservice.SchedulerJobStatusCompleted
	SchedulerJobStatusFailed    = pkgservice.SchedulerJobStatusFailed
	SchedulerJobStatusPaused    = pkgservice.SchedulerJobStatusPaused
	SchedulerJobStatusCanceled  = pkgservice.SchedulerJobStatusCanceled
)

// SchedulerOwnerType constants.
const (
	SchedulerOwnerTypeSystem    = pkgservice.SchedulerOwnerTypeSystem
	SchedulerOwnerTypeTenant    = pkgservice.SchedulerOwnerTypeTenant
	SchedulerOwnerTypeDelegated = pkgservice.SchedulerOwnerTypeDelegated
)

// =============================================================================
// Scheduler Request/Response Types
// =============================================================================

type (
	CreateSchedulerJobRequest      = pkgservice.CreateSchedulerJobRequest
	CreateSchedulerJobResponse     = pkgservice.CreateSchedulerJobResponse
	GetSchedulerJobRequest         = pkgservice.GetSchedulerJobRequest
	GetSchedulerJobResponse        = pkgservice.GetSchedulerJobResponse
	UpdateSchedulerJobRequest      = pkgservice.UpdateSchedulerJobRequest
	UpdateSchedulerJobResponse     = pkgservice.UpdateSchedulerJobResponse
	DeleteSchedulerJobRequest      = pkgservice.DeleteSchedulerJobRequest
	DeleteSchedulerJobResponse     = pkgservice.DeleteSchedulerJobResponse
	ListSchedulerJobsRequest       = pkgservice.ListSchedulerJobsRequest
	ListSchedulerJobsResponse      = pkgservice.ListSchedulerJobsResponse
	TriggerSchedulerJobRequest     = pkgservice.TriggerSchedulerJobRequest
	TriggerSchedulerJobResponse    = pkgservice.TriggerSchedulerJobResponse
	PauseSchedulerJobRequest       = pkgservice.PauseSchedulerJobRequest
	PauseSchedulerJobResponse      = pkgservice.PauseSchedulerJobResponse
	ResumeSchedulerJobRequest      = pkgservice.ResumeSchedulerJobRequest
	ResumeSchedulerJobResponse     = pkgservice.ResumeSchedulerJobResponse
	CancelSchedulerJobRequest      = pkgservice.CancelSchedulerJobRequest
	CancelSchedulerJobResponse     = pkgservice.CancelSchedulerJobResponse
	GetSchedulerJobHistoryRequest  = pkgservice.GetSchedulerJobHistoryRequest
	GetSchedulerJobHistoryResponse = pkgservice.GetSchedulerJobHistoryResponse
	GetSchedulerStatusResponse     = pkgservice.GetSchedulerStatusResponse
)

// =============================================================================
// Service Configuration
// =============================================================================

type (
	// ServiceConfig contains configuration for the service facade.
	ServiceConfig = pkgservice.ServiceConfig

	// ServiceBuilder builds a QuicBaaS instance.
	ServiceBuilder = pkgservice.ServiceBuilder

	// SchedulerClustering carries the handles the scheduler needs to arbitrate
	// firings across nodes: the leader check and the two go-qrdb planes its
	// claim rides. It is reachable from ServiceBuilder, so an SDK consumer
	// building a clustered service cannot construct one without this alias.
	SchedulerClustering = pkgservice.SchedulerClustering

	// StreamWriter is a helper interface for streaming responses.
	StreamWriter = pkgservice.StreamWriter

	// StreamReader is a helper interface for streaming requests.
	StreamReader = pkgservice.StreamReader
)

// NewServiceBuilder creates a new service builder.
var NewServiceBuilder = pkgservice.NewServiceBuilder

// =============================================================================
// Ollama Types
// =============================================================================

type (
	OllamaEmbedRequest          = pkgservice.OllamaEmbedRequest
	OllamaEmbedWithModelRequest = pkgservice.OllamaEmbedWithModelRequest
	OllamaEmbedResponse         = pkgservice.OllamaEmbedResponse
	OllamaEmbedBatchRequest     = pkgservice.OllamaEmbedBatchRequest
	OllamaEmbedBatchResponse    = pkgservice.OllamaEmbedBatchResponse
	OllamaListModelsRequest     = pkgservice.OllamaListModelsRequest
	OllamaModelInfo             = pkgservice.OllamaModelInfo
	OllamaListModelsResponse    = pkgservice.OllamaListModelsResponse
	OllamaGetModelInfoRequest   = pkgservice.OllamaGetModelInfoRequest
	OllamaModelDetails          = pkgservice.OllamaModelDetails
	OllamaGetModelInfoResponse  = pkgservice.OllamaGetModelInfoResponse
	OllamaHealthRequest         = pkgservice.OllamaHealthRequest
	OllamaHealthResponse        = pkgservice.OllamaHealthResponse
)

// =============================================================================
// Certificate Types
// =============================================================================

type (
	IssueCertificateRequest   = pkgservice.IssueCertificateRequest
	IssueCertificateResponse  = pkgservice.IssueCertificateResponse
	SignCSRRequest            = pkgservice.SignCSRRequest
	SignCSRResponse           = pkgservice.SignCSRResponse
	RevokeCertificateRequest  = pkgservice.RevokeCertificateRequest
	RevokeCertificateResponse = pkgservice.RevokeCertificateResponse
	VerifyCertificateRequest  = pkgservice.VerifyCertificateRequest
	VerifyCertificateResponse = pkgservice.VerifyCertificateResponse
	GetCACertificateRequest   = pkgservice.GetCACertificateRequest
	GetCACertificateResponse  = pkgservice.GetCACertificateResponse
	GetTLSConfigRequest       = pkgservice.GetTLSConfigRequest
	GetTLSConfigResponse      = pkgservice.GetTLSConfigResponse
)

// =============================================================================
// KMS Re-encryption Types (shared between KMS DEK service and transports)
// =============================================================================

type (
	StartReencryptionRequest  = pkgservice.StartReencryptionRequest
	StartReencryptionResponse = pkgservice.StartReencryptionResponse
)

// =============================================================================
// Service Registry
// =============================================================================

type (
	// RegistryService is the lease-based service registry served on every
	// transport (pkg/service.RegistryService).
	RegistryService = pkgservice.RegistryService

	// RegistryServiceInstance is a registered instance of a named service.
	RegistryServiceInstance = pkgservice.RegistryServiceInstance

	// RegistryEvent is one registration change delivered by Watch.
	RegistryEvent = pkgservice.RegistryEvent

	// RegisterServiceInstanceRequest registers an instance under a lease.
	RegisterServiceInstanceRequest = pkgservice.RegisterServiceInstanceRequest

	// RegisterServiceInstanceResponse carries the lease backing a registration.
	RegisterServiceInstanceResponse = pkgservice.RegisterServiceInstanceResponse

	// RenewServiceLeaseRequest renews a registration's lease.
	RenewServiceLeaseRequest = pkgservice.RenewServiceLeaseRequest

	// RenewServiceLeaseResponse reports a renewed lease.
	RenewServiceLeaseResponse = pkgservice.RenewServiceLeaseResponse

	// DeregisterServiceInstanceRequest removes a registration.
	DeregisterServiceInstanceRequest = pkgservice.DeregisterServiceInstanceRequest

	// ResolveServiceRequest looks up a service.
	ResolveServiceRequest = pkgservice.ResolveServiceRequest

	// ResolveServiceResponse lists a service's instances and their revision.
	ResolveServiceResponse = pkgservice.ResolveServiceResponse

	// WatchServiceRequest opens a watch from a revision.
	WatchServiceRequest = pkgservice.WatchServiceRequest
)

// NewRegistryServiceFromBackend runs the registry service over a storage
// backend of the embedder's choosing.
var NewRegistryServiceFromBackend = pkgservice.NewRegistryServiceFromBackend

// =============================================================================
// Scheduler Placement Service
// =============================================================================

type (
	// PlacementService is the scheduler's workload placement service: node
	// facts on qrdb leases, cordon and drain, and race-free placement
	// decisions (pkg/scheduler/placement).
	PlacementService = pkgservice.PlacementService

	// RegisterPlacementNodeRequest registers a node's facts under a lease.
	RegisterPlacementNodeRequest = pkgservice.RegisterPlacementNodeRequest
)

// NewPlacementService creates the placement service over a placement engine.
var NewPlacementService = pkgservice.NewPlacementService

// =============================================================================
// Attestation Service
// =============================================================================

type (
	// AttestationService is the verify-and-compose surface exposed under
	// Security().Attestation(). It is a pure verifier over the go-xkms
	// attestation package plus an orchestration seam (AttestContainer)
	// that forwards to an application-supplied ContainerAttestor.
	AttestationService = pkgservice.AttestationService

	// ContainerAttestor is the application-supplied orchestration hook the
	// attestation service calls to obtain container attestation evidence.
	// Embedders register an implementation with the server option
	// quicbaas.WithContainerAttestor. Until one is registered,
	// AttestContainer returns ErrAttestationNoAttestor.
	ContainerAttestor = pkgservice.ContainerAttestor

	// VerifyAttestationRequest carries a wire-portable attestation
	// statement plus the options governing its verification.
	VerifyAttestationRequest = pkgservice.VerifyAttestationRequest

	// VerifyAttestationOptions is the wire-portable form of the xkms
	// attestation VerifyOptions.
	VerifyAttestationOptions = pkgservice.VerifyAttestationOptions

	// VerifyAttestationResponse reports the outcome of a verification.
	VerifyAttestationResponse = pkgservice.VerifyAttestationResponse

	// VerifyAttestationChainRequest carries the certificate chain to validate.
	VerifyAttestationChainRequest = pkgservice.VerifyAttestationChainRequest

	// VerifyAttestationChainResponse reports whether the chain validated.
	VerifyAttestationChainResponse = pkgservice.VerifyAttestationChainResponse

	// AttestContainerRequest describes the container whose execution
	// environment is to be attested.
	AttestContainerRequest = pkgservice.AttestContainerRequest

	// AttestContainerResponse is the attestation evidence produced for a
	// container.
	AttestContainerResponse = pkgservice.AttestContainerResponse
)

// Attestation Verifier types (RFC 9334): endorsements, signed reference
// values and attestation results.
type (
	AttestationOption            = pkgservice.AttestationOption
	AttestationStore             = pkgservice.AttestationStore
	EndorsementKind              = pkgservice.EndorsementKind
	Endorsement                  = pkgservice.Endorsement
	PutEndorsementRequest        = pkgservice.PutEndorsementRequest
	ListEndorsementsRequest      = pkgservice.ListEndorsementsRequest
	ListEndorsementsResponse     = pkgservice.ListEndorsementsResponse
	DeleteEndorsementRequest     = pkgservice.DeleteEndorsementRequest
	ReferenceValues              = pkgservice.ReferenceValues
	PutReferenceValuesRequest    = pkgservice.PutReferenceValuesRequest
	GetReferenceValuesRequest    = pkgservice.GetReferenceValuesRequest
	ListReferenceValuesResponse  = pkgservice.ListReferenceValuesResponse
	DeleteReferenceValuesRequest = pkgservice.DeleteReferenceValuesRequest
	AppraiseQuoteRequest         = pkgservice.AppraiseQuoteRequest
	AppraisalCheck               = pkgservice.AppraisalCheck
	AppraisalOutcome             = pkgservice.AppraisalOutcome
	AttestationResult            = pkgservice.AttestationResult
)

// Attestation Verifier constructors and options.
var (
	// WithAttestationStore gives the Verifier its endorsement and
	// reference-value store.
	WithAttestationStore = pkgservice.WithAttestationStore

	// WithAttestationClock sets the appraisal clock.
	WithAttestationClock = pkgservice.WithAttestationClock

	// NewQRDBAttestationStore stores Verifier records in qrdb KV.
	NewQRDBAttestationStore = pkgservice.NewQRDBAttestationStore
)

// NewAttestationService creates the attestation service over the supplied
// trust anchors and container attestor. A nil attestor leaves the
// orchestration seam empty and AttestContainer returns
// ErrAttestationNoAttestor.
var NewAttestationService = pkgservice.NewAttestationService

// NewHostAppraiser returns the Verifier over an attestation store as the
// orchestration HostAppraiser a TPMAttestingRuntime appraises host quotes
// with. The store must hold the tenant's ak-root and reference-signer
// endorsements and the named reference values.
var NewHostAppraiser = pkgservice.NewHostAppraiser

// NewRuntimeContainerAttestor builds the standalone ContainerAttestor from an
// attesting container runtime. The runtime parameter type is not yet on the
// SDK surface, so this constructor is currently usable only by callers that
// already hold a runtime value.
var NewRuntimeContainerAttestor = pkgservice.NewRuntimeContainerAttestor

// Typed sentinel errors returned by the attestation service.
var (
	// ErrAttestationNilRequest is returned when a request is nil.
	ErrAttestationNilRequest = pkgservice.ErrAttestationNilRequest

	// ErrAttestationEmptyChain is returned when no certificate chain was supplied.
	ErrAttestationEmptyChain = pkgservice.ErrAttestationEmptyChain

	// ErrAttestationEmptyFormat is returned when the attestation format is empty.
	ErrAttestationEmptyFormat = pkgservice.ErrAttestationEmptyFormat

	// ErrAttestationNoSignature is returned when the statement carries no signature.
	ErrAttestationNoSignature = pkgservice.ErrAttestationNoSignature

	// ErrAttestationNoAttestedKey is returned when no attested key was supplied.
	ErrAttestationNoAttestedKey = pkgservice.ErrAttestationNoAttestedKey

	// ErrAttestationEmptyImage is returned when AttestContainer gets no image reference.
	ErrAttestationEmptyImage = pkgservice.ErrAttestationEmptyImage

	// ErrAttestationNoAttestor is returned when no ContainerAttestor has been
	// registered with quicbaas.WithContainerAttestor.
	ErrAttestationNoAttestor = pkgservice.ErrAttestationNoAttestor
)

// =============================================================================
// ACME Service
// =============================================================================

type (
	ACMEFinalizeOrderRequest      = pkgservice.ACMEFinalizeOrderRequest
	ACMEFinalizeOrderResponse     = pkgservice.ACMEFinalizeOrderResponse
	ACMEIssueRequest              = pkgservice.ACMEIssueRequest
	ACMEIssueResponse             = pkgservice.ACMEIssueResponse
	ACMENewOrderRequest           = pkgservice.ACMENewOrderRequest
	ACMENewOrderResponse          = pkgservice.ACMENewOrderResponse
	ACMEOrderIdentifier           = pkgservice.ACMEOrderIdentifier
	ACMERegisterChallengeRequest  = pkgservice.ACMERegisterChallengeRequest
	ACMERegisterChallengeResponse = pkgservice.ACMERegisterChallengeResponse
	ACMERenewRequest              = pkgservice.ACMERenewRequest
	ACMERenewResponse             = pkgservice.ACMERenewResponse
	ACMEServicer                  = pkgservice.ACMEServicer
)

// =============================================================================
// Account Service (root/service account lifecycle)
// =============================================================================

type (
	AccountService                  = pkgservice.AccountService
	CreateAccountRequest            = pkgservice.CreateAccountRequest
	CreateAccountResponse           = pkgservice.CreateAccountResponse
	DeleteAccountRequest            = pkgservice.DeleteAccountRequest
	DeleteAccountResponse           = pkgservice.DeleteAccountResponse
	GetAccountRequest               = pkgservice.GetAccountRequest
	GetAccountResponse              = pkgservice.GetAccountResponse
	GetAccountUsageRequest          = pkgservice.GetAccountUsageRequest
	GetAccountUsageResponse         = pkgservice.GetAccountUsageResponse
	ListAccountsRequest             = pkgservice.ListAccountsRequest
	ListAccountsResponse            = pkgservice.ListAccountsResponse
	RevokeAccountRequest            = pkgservice.RevokeAccountRequest
	RevokeAccountResponse           = pkgservice.RevokeAccountResponse
	RotateAccountCredentialRequest  = pkgservice.RotateAccountCredentialRequest
	RotateAccountCredentialResponse = pkgservice.RotateAccountCredentialResponse

	// MaterializeAccountCredentialRequest / ...Response are the pooled-account
	// credential RELEASE shapes. The response is the only account shape in the
	// service vocabulary that carries an AccountCredential, deliberately: it
	// exists so a pooled credential can reach the worker host running the
	// agent (go-quicbaas#734).
	MaterializeAccountCredentialRequest  = pkgservice.MaterializeAccountCredentialRequest
	MaterializeAccountCredentialResponse = pkgservice.MaterializeAccountCredentialResponse

	// ReserveAccountSessionRequest / ...Response and ReleaseAccountSession...
	// are the session-slot reservation shapes a worker daemon uses to make
	// max_concurrent_sessions hold across hosts (go-quicbaas#1047).
	ReserveAccountSessionRequest  = pkgservice.ReserveAccountSessionRequest
	ReserveAccountSessionResponse = pkgservice.ReserveAccountSessionResponse
	ReleaseAccountSessionRequest  = pkgservice.ReleaseAccountSessionRequest
	ReleaseAccountSessionResponse = pkgservice.ReleaseAccountSessionResponse

	SetDefaultAccountRequest  = pkgservice.SetDefaultAccountRequest
	SetDefaultAccountResponse = pkgservice.SetDefaultAccountResponse
	UpdateAccountRequest      = pkgservice.UpdateAccountRequest
	UpdateAccountResponse     = pkgservice.UpdateAccountResponse
)

// =============================================================================
// Agent Administration Service
// =============================================================================

type (
	FleetAdminService                  = pkgservice.FleetAdminService
	DisableFleetNodeRequest            = pkgservice.DisableFleetNodeRequest
	DisableFleetNodeResponse           = pkgservice.DisableFleetNodeResponse
	EnableFleetNodeRequest             = pkgservice.EnableFleetNodeRequest
	EnableFleetNodeResponse            = pkgservice.EnableFleetNodeResponse
	GetFleetNodeRegistrationRequest    = pkgservice.GetFleetNodeRegistrationRequest
	GetFleetNodeRegistrationResponse   = pkgservice.GetFleetNodeRegistrationResponse
	ListFleetNodeRegistrationsRequest  = pkgservice.ListFleetNodeRegistrationsRequest
	ListFleetNodeRegistrationsResponse = pkgservice.ListFleetNodeRegistrationsResponse
)

// =============================================================================
// Certificate Authority Service
// =============================================================================

type (
	CAGenerateCRLRequest        = pkgservice.CAGenerateCRLRequest
	CAGenerateCRLResponse       = pkgservice.CAGenerateCRLResponse
	CAGetBundleRequest          = pkgservice.CAGetBundleRequest
	CAGetBundleResponse         = pkgservice.CAGetBundleResponse
	CAGetCertificateRequest     = pkgservice.CAGetCertificateRequest
	CAGetCertificateResponse    = pkgservice.CAGetCertificateResponse
	CAIsRevokedRequest          = pkgservice.CAIsRevokedRequest
	CAIsRevokedResponse         = pkgservice.CAIsRevokedResponse
	CAIssueCertificateRequest   = pkgservice.CAIssueCertificateRequest
	CAIssueCertificateResponse  = pkgservice.CAIssueCertificateResponse
	CARevokeCertificateRequest  = pkgservice.CARevokeCertificateRequest
	CARevokeCertificateResponse = pkgservice.CARevokeCertificateResponse
	CAService                   = pkgservice.CAService
	CASignCSRRequest            = pkgservice.CASignCSRRequest
	CASignCSRResponse           = pkgservice.CASignCSRResponse
)

// =============================================================================
// Certificate Extensions (chain, cross-sign, device identity)
// =============================================================================

type (
	CertificateChainRequest     = pkgservice.CertificateChainRequest
	CertificateChainResponse    = pkgservice.CertificateChainResponse
	CrossSignRequest            = pkgservice.CrossSignRequest
	CrossSignResponse           = pkgservice.CrossSignResponse
	IssueDeviceIdentityRequest  = pkgservice.IssueDeviceIdentityRequest
	IssueDeviceIdentityResponse = pkgservice.IssueDeviceIdentityResponse
)

// =============================================================================
// Certificate Service (CA info, CRL, revocation, listing)
// =============================================================================

type (
	CAInfoRequest            = pkgservice.CAInfoRequest
	CAInfoResponse           = pkgservice.CAInfoResponse
	CertificateSummary       = pkgservice.CertificateSummary
	GenerateCRLRequest       = pkgservice.GenerateCRLRequest
	GenerateCRLResponse      = pkgservice.GenerateCRLResponse
	GetCABundleRequest       = pkgservice.GetCABundleRequest
	GetCABundleResponse      = pkgservice.GetCABundleResponse
	IsRevokedRequest         = pkgservice.IsRevokedRequest
	IsRevokedResponse        = pkgservice.IsRevokedResponse
	ListCertificatesRequest  = pkgservice.ListCertificatesRequest
	ListCertificatesResponse = pkgservice.ListCertificatesResponse
)

// =============================================================================
// Database Service
// =============================================================================

type (
	CreateDatabaseRequest    = pkgservice.CreateDatabaseRequest
	DatabaseBarrierConfig    = pkgservice.DatabaseBarrierConfig
	DatabaseConnectionConfig = pkgservice.DatabaseConnectionConfig
	DatabaseEtcdConfig       = pkgservice.DatabaseEtcdConfig
	DatabaseInfo             = pkgservice.DatabaseInfo
	DatabaseProtocolConfig   = pkgservice.DatabaseProtocolConfig
	DatabaseRaftConfig       = pkgservice.DatabaseRaftConfig
	DatabaseService          = pkgservice.DatabaseService
)

// =============================================================================
// Dynamic Secrets Roles, Leases and Credential Roles
// =============================================================================

type (
	CreateRoleRequest            = pkgservice.CreateRoleRequest
	CreateRoleResponse           = pkgservice.CreateRoleResponse
	CredentialRoleCreateRequest  = pkgservice.CredentialRoleCreateRequest
	CredentialRoleCreateResponse = pkgservice.CredentialRoleCreateResponse
	CredentialRoleDeleteRequest  = pkgservice.CredentialRoleDeleteRequest
	CredentialRoleDeleteResponse = pkgservice.CredentialRoleDeleteResponse
	CredentialRoleGetRequest     = pkgservice.CredentialRoleGetRequest
	CredentialRoleGetResponse    = pkgservice.CredentialRoleGetResponse
	CredentialRolesListRequest   = pkgservice.CredentialRolesListRequest
	CredentialRolesListResponse  = pkgservice.CredentialRolesListResponse
	DeleteRoleRequest            = pkgservice.DeleteRoleRequest
	DeleteRoleResponse           = pkgservice.DeleteRoleResponse
	GenerateCredentialRequest    = pkgservice.GenerateCredentialRequest
	GenerateCredentialResponse   = pkgservice.GenerateCredentialResponse
	GetRoleRequest               = pkgservice.GetRoleRequest
	GetRoleResponse              = pkgservice.GetRoleResponse
	ListLeasesRequest            = pkgservice.ListLeasesRequest
	ListLeasesResponse           = pkgservice.ListLeasesResponse
	ListRolesRequest             = pkgservice.ListRolesRequest
	ListRolesResponse            = pkgservice.ListRolesResponse
	RenewLeaseRequest            = pkgservice.RenewLeaseRequest
	RenewLeaseResponse           = pkgservice.RenewLeaseResponse
	RevokeLeaseRequest           = pkgservice.RevokeLeaseRequest
	RevokeLeaseResponse          = pkgservice.RevokeLeaseResponse
)

// =============================================================================
// Embedding Provider Status
// =============================================================================

type (
	SetProviderStatusRequest  = pkgservice.SetProviderStatusRequest
	SetProviderStatusResponse = pkgservice.SetProviderStatusResponse
)

// =============================================================================
// Facade Providers
// =============================================================================

type (
	QRDBClientProvider = pkgservice.QRDBClientProvider
)

// =============================================================================
// IAM Component Interfaces
// =============================================================================

type (
	MFAManager      = pkgservice.MFAManager
	WebAuthnManager = pkgservice.WebAuthnManager
)

// =============================================================================
// KMS Root Key Rotation
// =============================================================================

type (
	RotateRootKeyRequest  = pkgservice.RotateRootKeyRequest
	RotateRootKeyResponse = pkgservice.RotateRootKeyResponse
)

// =============================================================================
// Login Service
// =============================================================================

type (
	FinishLoginResult = pkgservice.FinishLoginResult
	LoginServicer     = pkgservice.LoginServicer
)

// =============================================================================
// Object Storage
// =============================================================================

type (
	ArchiveObjectRequest       = pkgservice.ArchiveObjectRequest
	ArchiveObjectResponse      = pkgservice.ArchiveObjectResponse
	BackendHealthInfo          = pkgservice.BackendHealthInfo
	BackendInfo                = pkgservice.BackendInfo
	BatchDeleteObjectsRequest  = pkgservice.BatchDeleteObjectsRequest
	BatchDeleteObjectsResponse = pkgservice.BatchDeleteObjectsResponse
	BatchDeleteResult          = pkgservice.BatchDeleteResult
	BatchGetObjectsRequest     = pkgservice.BatchGetObjectsRequest
	BatchGetObjectsResponse    = pkgservice.BatchGetObjectsResponse
	BatchGetResult             = pkgservice.BatchGetResult
	BatchPutObjectsRequest     = pkgservice.BatchPutObjectsRequest
	BatchPutObjectsResponse    = pkgservice.BatchPutObjectsResponse
	BatchPutResult             = pkgservice.BatchPutResult
	CopyObjectRequest          = pkgservice.CopyObjectRequest
	CopyObjectResponse         = pkgservice.CopyObjectResponse
	DeleteObjectRequest        = pkgservice.DeleteObjectRequest
	DeleteObjectResponse       = pkgservice.DeleteObjectResponse
	GetBackendHealthRequest    = pkgservice.GetBackendHealthRequest
	GetBackendHealthResponse   = pkgservice.GetBackendHealthResponse
	GetObjectPolicyRequest     = pkgservice.GetObjectPolicyRequest
	GetObjectPolicyResponse    = pkgservice.GetObjectPolicyResponse
	GetObjectRequest           = pkgservice.GetObjectRequest
	GetObjectResponse          = pkgservice.GetObjectResponse
	GetTenantQuotasRequest     = pkgservice.GetTenantQuotasRequest
	GetTenantQuotasResponse    = pkgservice.GetTenantQuotasResponse
	GetTenantUsageRequest      = pkgservice.GetTenantUsageRequest
	GetTenantUsageResponse     = pkgservice.GetTenantUsageResponse
	HeadObjectRequest          = pkgservice.HeadObjectRequest
	HeadObjectResponse         = pkgservice.HeadObjectResponse
	ListBackendsRequest        = pkgservice.ListBackendsRequest
	ListBackendsResponse       = pkgservice.ListBackendsResponse
	ListByTagRequest           = pkgservice.ListByTagRequest
	ListByTagResponse          = pkgservice.ListByTagResponse
	ListByTenantRequest        = pkgservice.ListByTenantRequest
	ListByTenantResponse       = pkgservice.ListByTenantResponse
	ListObjectsRequest         = pkgservice.ListObjectsRequest
	ListObjectsResponse        = pkgservice.ListObjectsResponse
	MigrateObjectRequest       = pkgservice.MigrateObjectRequest
	MigrateObjectResponse      = pkgservice.MigrateObjectResponse
	ObjectSummary              = pkgservice.ObjectSummary
	PutObjectRequest           = pkgservice.PutObjectRequest
	PutObjectResponse          = pkgservice.PutObjectResponse
	ReplicateObjectRequest     = pkgservice.ReplicateObjectRequest
	ReplicateObjectResponse    = pkgservice.ReplicateObjectResponse
	ReplicationResult          = pkgservice.ReplicationResult
	RestoreObjectRequest       = pkgservice.RestoreObjectRequest
	RestoreObjectResponse      = pkgservice.RestoreObjectResponse
	SetObjectPolicyRequest     = pkgservice.SetObjectPolicyRequest
	SetObjectPolicyResponse    = pkgservice.SetObjectPolicyResponse
	SetTenantQuotasRequest     = pkgservice.SetTenantQuotasRequest
	SetTenantQuotasResponse    = pkgservice.SetTenantQuotasResponse
	VerificationResult         = pkgservice.VerificationResult
	VerifyObjectRequest        = pkgservice.VerifyObjectRequest
	VerifyObjectResponse       = pkgservice.VerifyObjectResponse
)

// =============================================================================
// Object Storage Administration
// =============================================================================

type (
	BackendType                          = pkgservice.BackendType
	CompactBackendRequest                = pkgservice.CompactBackendRequest
	CompactBackendResponse               = pkgservice.CompactBackendResponse
	CreateBackendRequest                 = pkgservice.CreateBackendRequest
	CreateBackendResponse                = pkgservice.CreateBackendResponse
	DeleteBackendRequest                 = pkgservice.DeleteBackendRequest
	DeleteBackendResponse                = pkgservice.DeleteBackendResponse
	GCStatus                             = pkgservice.GCStatus
	GetGarbageCollectionProgressRequest  = pkgservice.GetGarbageCollectionProgressRequest
	GetGarbageCollectionProgressResponse = pkgservice.GetGarbageCollectionProgressResponse
	GetStorageStatsRequest               = pkgservice.GetStorageStatsRequest
	GetStorageStatsResponse              = pkgservice.GetStorageStatsResponse
	RepairObjectRequest                  = pkgservice.RepairObjectRequest
	RepairObjectResponse                 = pkgservice.RepairObjectResponse
	RunGarbageCollectionRequest          = pkgservice.RunGarbageCollectionRequest
	RunGarbageCollectionResponse         = pkgservice.RunGarbageCollectionResponse
	SetBackendStatusRequest              = pkgservice.SetBackendStatusRequest
	SetBackendStatusResponse             = pkgservice.SetBackendStatusResponse
	StorageTier                          = pkgservice.StorageTier
	TenantStorageStats                   = pkgservice.TenantStorageStats
	UpdateBackendRequest                 = pkgservice.UpdateBackendRequest
	UpdateBackendResponse                = pkgservice.UpdateBackendResponse
)

// =============================================================================
// Object Transfer Service
// =============================================================================

type (
	FetchObjectRequest         = pkgservice.FetchObjectRequest
	FetchObjectResponse        = pkgservice.FetchObjectResponse
	ObjectFetchItem            = pkgservice.ObjectFetchItem
	StreamFetchObjectsRequest  = pkgservice.StreamFetchObjectsRequest
	StreamFetchObjectsResponse = pkgservice.StreamFetchObjectsResponse
)

// =============================================================================
// Project Service
// =============================================================================

type (
	AuthorizeFleetNodeRequest   = pkgservice.AuthorizeFleetNodeRequest
	CreateProjectRequest        = pkgservice.CreateProjectRequest
	CreateProjectResponse       = pkgservice.CreateProjectResponse
	DeleteProjectRequest        = pkgservice.DeleteProjectRequest
	DeleteProjectResponse       = pkgservice.DeleteProjectResponse
	GetProjectByNameRequest     = pkgservice.GetProjectByNameRequest
	GitSigningRetirement        = pkgservice.GitSigningRetirement
	GetProjectRequest           = pkgservice.GetProjectRequest
	GetProjectResponse          = pkgservice.GetProjectResponse
	GrantFleetNodeRoleRequest   = pkgservice.GrantFleetNodeRoleRequest
	GrantFleetNodeRoleResponse  = pkgservice.GrantFleetNodeRoleResponse
	ListProjectsRequest         = pkgservice.ListProjectsRequest
	ListProjectsResponse        = pkgservice.ListProjectsResponse
	ProjectService              = pkgservice.ProjectService
	RevokeFleetNodeRoleRequest  = pkgservice.RevokeFleetNodeRoleRequest
	RevokeFleetNodeRoleResponse = pkgservice.RevokeFleetNodeRoleResponse
	UpdateProjectRequest        = pkgservice.UpdateProjectRequest
	UpdateProjectResponse       = pkgservice.UpdateProjectResponse
)

// =============================================================================
// RBAC Request/Response Types
// =============================================================================

type (
	AuthAssignRoleRequest  = pkgservice.AuthAssignRoleRequest
	AuthAssignRoleResponse = pkgservice.AuthAssignRoleResponse
	AuthCreateRoleRequest  = pkgservice.AuthCreateRoleRequest
	AuthCreateRoleResponse = pkgservice.AuthCreateRoleResponse
	AuthDeleteRoleRequest  = pkgservice.AuthDeleteRoleRequest
	AuthDeleteRoleResponse = pkgservice.AuthDeleteRoleResponse
	AuthListRolesRequest   = pkgservice.AuthListRolesRequest
	AuthListRolesResponse  = pkgservice.AuthListRolesResponse
	AuthRevokeRoleRequest  = pkgservice.AuthRevokeRoleRequest
	AuthRevokeRoleResponse = pkgservice.AuthRevokeRoleResponse
	RoleInfo               = pkgservice.RoleInfo
)

// =============================================================================
// Seal Service
// =============================================================================

type (
	SealService = pkgservice.SealService
)

// =============================================================================
// Trust Service (DANE, cross-signed roots, AIA)
// =============================================================================

type (
	AutoImportAIARequest        = pkgservice.AutoImportAIARequest
	AutoImportAIAResponse       = pkgservice.AutoImportAIAResponse
	CrossSignedRootPoolRequest  = pkgservice.CrossSignedRootPoolRequest
	CrossSignedRootPoolResponse = pkgservice.CrossSignedRootPoolResponse
	DANEPublisher               = pkgservice.DANEPublisher
	PublishDANERequest          = pkgservice.PublishDANERequest
	PublishDANEResponse         = pkgservice.PublishDANEResponse
	TrustService                = pkgservice.TrustService
)

// =============================================================================
// Vector Service
// =============================================================================

type (
	BatchDeleteVectorsRequest     = pkgservice.BatchDeleteVectorsRequest
	BatchDeleteVectorsResponse    = pkgservice.BatchDeleteVectorsResponse
	BatchUpsertRequest            = pkgservice.BatchUpsertRequest
	BatchUpsertResponse           = pkgservice.BatchUpsertResponse
	CompactNamespaceRequest       = pkgservice.CompactNamespaceRequest
	CompactNamespaceResponse      = pkgservice.CompactNamespaceResponse
	CreateNamespaceRequest        = pkgservice.CreateNamespaceRequest
	CreateNamespaceResponse       = pkgservice.CreateNamespaceResponse
	DeleteNamespaceRequest        = pkgservice.DeleteNamespaceRequest
	DeleteNamespaceResponse       = pkgservice.DeleteNamespaceResponse
	DeleteVectorRequest           = pkgservice.DeleteVectorRequest
	DeleteVectorResponse          = pkgservice.DeleteVectorResponse
	GetCompactionProgressRequest  = pkgservice.GetCompactionProgressRequest
	GetCompactionProgressResponse = pkgservice.GetCompactionProgressResponse
	GetIndexStatsRequest          = pkgservice.GetIndexStatsRequest
	GetIndexStatsResponse         = pkgservice.GetIndexStatsResponse
	GetNamespaceInfoRequest       = pkgservice.GetNamespaceInfoRequest
	GetNamespaceInfoResponse      = pkgservice.GetNamespaceInfoResponse
	GetRebuildProgressRequest     = pkgservice.GetRebuildProgressRequest
	GetRebuildProgressResponse    = pkgservice.GetRebuildProgressResponse
	GetVectorRequest              = pkgservice.GetVectorRequest
	GetVectorResponse             = pkgservice.GetVectorResponse
	HybridSearchRequest           = pkgservice.HybridSearchRequest
	HybridSearchResponse          = pkgservice.HybridSearchResponse
	IndexPerformanceMetrics       = pkgservice.IndexPerformanceMetrics
	ListNamespacesRequest         = pkgservice.ListNamespacesRequest
	ListNamespacesResponse        = pkgservice.ListNamespacesResponse
	ListVectorsRequest            = pkgservice.ListVectorsRequest
	ListVectorsResponse           = pkgservice.ListVectorsResponse
	NamespaceInfo                 = pkgservice.NamespaceInfo
	RebuildIndexRequest           = pkgservice.RebuildIndexRequest
	RebuildIndexResponse          = pkgservice.RebuildIndexResponse
	SearchByIDRequest             = pkgservice.SearchByIDRequest
	SearchByIDResponse            = pkgservice.SearchByIDResponse
	SearchByMetadataRequest       = pkgservice.SearchByMetadataRequest
	SearchByMetadataResponse      = pkgservice.SearchByMetadataResponse
	SearchSimilarRequest          = pkgservice.SearchSimilarRequest
	SearchSimilarResponse         = pkgservice.SearchSimilarResponse
	UpdateVectorMetadataRequest   = pkgservice.UpdateVectorMetadataRequest
	UpdateVectorMetadataResponse  = pkgservice.UpdateVectorMetadataResponse
	UpsertVectorRequest           = pkgservice.UpsertVectorRequest
	UpsertVectorResponse          = pkgservice.UpsertVectorResponse
	VectorData                    = pkgservice.VectorData
	VectorSearchResult            = pkgservice.VectorSearchResult
)
