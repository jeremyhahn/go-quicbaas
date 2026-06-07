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
	"fmt"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/messaging"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// Handler routes QUIC protocol requests to QuicBaaS methods.
// It handles request parsing, service invocation, and response encoding.
type Handler struct {
	service               service.QuicBaaS
	kmsService            service.KMSService
	secretsService        service.SecretsService
	cryptoService         service.CryptoService
	logger                *logger.Logger
	dynamicSecretsService service.DynamicSecretsService

	// barrierService backs the OpBarrier handler tree. Wired by the QUIC
	// server via SetBarrierService once the qrdb SDK BarrierClient is
	// available; nil before lifecycle wiring completes.
	barrierService service.BarrierServicer

	// ceremonyService backs the OpCeremony handler tree. Wired by the QUIC
	// server via SetCeremonyService once the qrdb SDK CeremonyClient is
	// available; nil before lifecycle wiring completes.
	ceremonyService service.CeremonyServicer

	// ObjectTransfer registry for peer-to-peer object data transfer
	objectTransferRegistry *ObjectTransferRegistry

	// Authorization dependencies
	authManager  interfaces.AuthManager
	authzService interfaces.AuthorizationService

	// Messaging service
	msgService *messaging.PushService
}

// HandlerOption is a functional option for configuring the Handler.
type HandlerOption func(*Handler)

// WithHandlerAuthManager sets the authentication manager for the handler.
func WithHandlerAuthManager(auth interfaces.AuthManager) HandlerOption {
	return func(h *Handler) {
		h.authManager = auth
	}
}

// WithHandlerAuthorizationService sets the authorization service for the handler.
func WithHandlerAuthorizationService(authz interfaces.AuthorizationService) HandlerOption {
	return func(h *Handler) {
		h.authzService = authz
	}
}

// WithDynamicSecretsService sets the dynamic secrets service for the handler.
func WithDynamicSecretsService(svc service.DynamicSecretsService) HandlerOption {
	return func(h *Handler) {
		h.dynamicSecretsService = svc
	}
}

// WithObjectTransferRegistry sets the object transfer registry for peer-to-peer operations.
func WithObjectTransferRegistry(registry *ObjectTransferRegistry) HandlerOption {
	return func(h *Handler) {
		h.objectTransferRegistry = registry
	}
}

// WithMsgService sets the messaging service for the handler.
func WithMsgService(svc *messaging.PushService) HandlerOption {
	return func(h *Handler) {
		h.msgService = svc
	}
}

// WithBarrierService installs the BarrierServicer used by the OpBarrier
// handler tree. When nil (the default), every barrier sub-operation responds
// with StatusUnavailable ("barrier service not available"). Lifecycle wiring
// calls this option once the qrdb SDK BarrierClient is online.
func WithBarrierService(svc service.BarrierServicer) HandlerOption {
	return func(h *Handler) {
		h.barrierService = svc
	}
}

// WithCeremonyService installs the CeremonyServicer used by the OpCeremony
// handler tree. When nil (the default), every ceremony sub-operation responds
// with StatusUnavailable ("ceremony service not available"). Lifecycle wiring
// calls this option once the qrdb SDK CeremonyClient is online.
func WithCeremonyService(svc service.CeremonyServicer) HandlerOption {
	return func(h *Handler) {
		h.ceremonyService = svc
	}
}

// NewHandler creates a new request handler with optional service dependencies.
// Pass nil for any service that is not available.
func NewHandler(svc service.QuicBaaS, kms service.KMSService, secrets service.SecretsService, crypto service.CryptoService, log *logger.Logger, opts ...HandlerOption) *Handler {
	h := &Handler{
		service:        svc,
		kmsService:     kms,
		secretsService: secrets,
		cryptoService:  crypto,
		logger:         log,
	}

	// Apply options
	for _, opt := range opts {
		opt(h)
	}

	return h
}

// isPublicOperation returns true if the operation does not require authentication.
func isPublicOperation(opcode byte) bool {
	switch opcode {
	case OpHealth, OpLiveness, OpReadiness:
		return true
	case OpAuthBeginLogin, OpAuthFinishLogin:
		// The login ceremony authenticates the caller (mTLS handshake /
		// signature); no bearer token is available yet.
		return true
	default:
		return false
	}
}

// getResourceAndAction maps an opcode to a resource and action for authorization.
func getResourceAndAction(opcode byte) (resource, action string) {
	switch opcode {
	// KV Operations - Read
	case OpGet, OpList:
		return interfaces.ResourceKeys, interfaces.ActionRead

	// KV Operations - Write
	case OpPut:
		return interfaces.ResourceKeys, interfaces.ActionWrite

	// KV Operations - Delete
	case OpDelete:
		return interfaces.ResourceKeys, interfaces.ActionDelete

	// Batch Operations - Read
	case OpBatchGet:
		return interfaces.ResourceKeys, interfaces.ActionRead

	// Batch Operations - Write
	case OpBatchPut:
		return interfaces.ResourceKeys, interfaces.ActionWrite

	// Batch Operations - Delete
	case OpBatchDelete:
		return interfaces.ResourceKeys, interfaces.ActionDelete

	// Transaction Operations
	case OpTransaction:
		return interfaces.ResourceTransactions, interfaces.ActionWrite

	// Query Operations
	case OpQuery, OpScanIndex:
		return interfaces.ResourceKeys, interfaces.ActionRead

	// Cluster Operations
	case OpClusterStatus, OpGetLeader, OpListNodes, OpRebalanceStatus, OpRebalancePlan:
		return interfaces.ResourceCluster, interfaces.ActionRead
	case OpAddNode, OpRemoveNode, OpCreateShard, OpRebalanceCluster, OpCancelMigration, OpNodeReplaceRequest:
		return interfaces.ResourceCluster, interfaces.ActionAdmin

	// Backup Operations
	case OpListBackups:
		return interfaces.ResourceBackups, interfaces.ActionRead
	case OpCreateBackup, OpRestoreBackup, OpVerifyBackup:
		return interfaces.ResourceBackups, interfaces.ActionWrite
	case OpDeleteBackup:
		return interfaces.ResourceBackups, interfaces.ActionDelete

	// Auth Operations (except Login which is public)
	case OpLogout, OpRefreshToken, OpValidateToken:
		return "auth", interfaces.ActionWrite

	// RBAC Operations
	case OpListUsers, OpListRoles:
		return interfaces.ResourceUsers, interfaces.ActionRead
	case OpCreateUser, OpAssignRole:
		return interfaces.ResourceUsers, interfaces.ActionWrite
	case OpDeleteUser, OpRevokeRole:
		return interfaces.ResourceUsers, interfaces.ActionDelete
	case OpCreateRole:
		return interfaces.ResourceRoles, interfaces.ActionWrite
	case OpDeleteRole:
		return interfaces.ResourceRoles, interfaces.ActionDelete

	// Cache Operations
	case OpGetCacheStats, OpGetCacheConfig:
		return "cache", interfaces.ActionRead
	case OpClearCache, OpUpdateCacheConfig:
		return "cache", interfaces.ActionAdmin

	// Time-Series Operations - Read
	case OpAggregate, OpTimeSeriesQuery, OpTSQuery, OpTSGetRetentionPolicy, OpTSListMetrics:
		return interfaces.ResourceTimeseries, interfaces.ActionRead

	// Time-Series Operations - Write
	case OpTSWritePoint, OpTSWriteBatch, OpTSSetRetentionPolicy:
		return interfaces.ResourceTimeseries, interfaces.ActionWrite

	// Time-Series Operations - Delete
	case OpTSDeleteSeries:
		return interfaces.ResourceTimeseries, interfaces.ActionDelete

	// Operations Management
	case OpGetMigrationProgress, OpGetUpgradeStatus:
		return interfaces.ResourceCluster, interfaces.ActionRead
	case OpMigrateData, OpReplaceNode, OpUpgradeNode:
		return interfaces.ResourceCluster, interfaces.ActionAdmin

	// Barrier Operations (sub-op dispatched, single top-level opcode).
	// Wire authorization is admin-level for the dispatch opcode; per-sub-op
	// authorization decisions are enforced by the underlying BarrierServicer
	// facade after JSON decoding.
	case OpBarrier:
		return interfaces.ResourceBarrier, interfaces.ActionAdmin

	// Ceremony Operations (sub-op dispatched, single top-level opcode).
	case OpCeremony:
		return interfaces.ResourceCeremony, interfaces.ActionAdmin

	// KMS Operations - Read
	case OpKMSGetKey, OpKMSListKeys, OpKMSKeyVersions:
		return interfaces.ResourceKMS, interfaces.ActionRead

	// KMS Operations - Write
	case OpKMSGenerateKey, OpKMSImportKey, OpKMSRotateKey, OpKMSEncrypt, OpKMSDecrypt, OpKMSKeyStatus:
		return interfaces.ResourceKMS, interfaces.ActionWrite

	// KMS Operations - Delete
	case OpKMSDeleteKey:
		return interfaces.ResourceKMS, interfaces.ActionDelete

	// KMS DEK Operations - Read
	case OpKMSDEKGet, OpKMSDEKList, OpKMSDEKStatus, OpKMSDEKGetReencryptProgress:
		return interfaces.ResourceKMS, interfaces.ActionRead

	// KMS DEK Operations - Write
	case OpKMSDEKCreate, OpKMSDEKRotate, OpKMSDEKSetDefault, OpKMSDEKSetSafety, OpKMSDEKClearSafety, OpKMSDEKStartReencryption:
		return interfaces.ResourceKMS, interfaces.ActionWrite

	// KMS DEK Operations - Delete
	case OpKMSDEKDelete:
		return interfaces.ResourceKMS, interfaces.ActionDelete

	// KMS Backend Operations - Read
	case OpKMSBackendStatus, OpKMSBackendTest:
		return interfaces.ResourceKMS, interfaces.ActionRead

	// Secrets Operations - Read
	case OpSecretsGet, OpSecretsList, OpSecretsListVersions, OpSecretsGetVersion, OpSecretsGetRotation, OpSecretsGetByPrefix, OpSecretsWatch:
		return interfaces.ResourceSecrets, interfaces.ActionRead

	// Secrets Operations - Write
	case OpSecretsStore, OpSecretsRollback, OpSecretsRotate, OpSecretsSetRotation, OpSecretsUpdateStatus:
		return interfaces.ResourceSecrets, interfaces.ActionWrite

	// Secrets Operations - Delete
	case OpSecretsDelete:
		return interfaces.ResourceSecrets, interfaces.ActionDelete

	// Crypto Operations
	case OpCryptoEncrypt, OpCryptoDecrypt, OpCryptoRewrap, OpCryptoSign, OpCryptoVerify,
		OpCryptoHash, OpCryptoRandom, OpCryptoDataKey, OpCryptoHMAC, OpCryptoVerifyHMAC:
		return interfaces.ResourceCrypto, interfaces.ActionWrite

	// Vector Operations - Read
	case OpVectorListNamespaces, OpVectorGetNamespaceInfo, OpVectorGet, OpVectorSearchSimilar,
		OpVectorSearchByMetadata, OpVectorHybridSearch, OpVectorSearchByID, OpVectorGetRebuildProgress,
		OpVectorGetIndexStats, OpVectorGetCompactionProgress:
		return interfaces.ResourceVector, interfaces.ActionRead

	// Vector Operations - Write
	case OpVectorCreateNamespace, OpVectorUpsert, OpVectorBatchUpsert, OpVectorRebuildIndex, OpVectorCompact:
		return interfaces.ResourceVector, interfaces.ActionWrite

	// Vector Operations - Delete
	case OpVectorDeleteNamespace, OpVectorDelete:
		return interfaces.ResourceVector, interfaces.ActionDelete

	// ObjectStorage Operations - Read
	case OpOSGetObject, OpOSHeadObject, OpOSListObjects, OpOSListByTag, OpOSListByTenant,
		OpOSGetObjectMetadata, OpOSGetStorageStats, OpOSGetLifecyclePolicy:
		return interfaces.ResourceObjectStore, interfaces.ActionRead

	// ObjectStorage Operations - Write
	case OpOSPutObject, OpOSCopyObject, OpOSReplicateObject, OpOSMigrateObject,
		OpOSArchiveObject, OpOSRestoreObject, OpOSVerifyObject,
		OpOSUpdateObjectMetadata, OpOSSetLifecyclePolicy:
		return interfaces.ResourceObjectStore, interfaces.ActionWrite

	// ObjectStorage Operations - Delete
	case OpOSDeleteObject:
		return interfaces.ResourceObjectStore, interfaces.ActionDelete

	// ObjectStorage Extended Operations - Read
	case OpOSGetObjectPolicy, OpOSListBackends, OpOSGetBackendHealth:
		return interfaces.ResourceObjectStore, interfaces.ActionRead

	// ObjectStorage Batch Operations - Write
	case OpOSBatchPutObjects:
		return interfaces.ResourceObjectStore, interfaces.ActionWrite

	// ObjectStorage Batch Operations - Read
	case OpOSBatchGetObjects:
		return interfaces.ResourceObjectStore, interfaces.ActionRead

	// ObjectStorage Batch Operations - Delete
	case OpOSBatchDeleteObjects:
		return interfaces.ResourceObjectStore, interfaces.ActionDelete

	// ObjectStorage Admin Operations - Admin
	case OpOSSetTenantQuotas, OpOSAdmin:
		return interfaces.ResourceObjectStore, interfaces.ActionAdmin

	// ObjectTransfer Operations - Read (peer-to-peer object data transfer)
	case OpFetchObject, OpStreamFetchObjects:
		return interfaces.ResourceObjectStore, interfaces.ActionRead

	// Embedding Operations - Read
	case OpEmbeddingListProviders, OpEmbeddingGetProvider, OpEmbeddingGetCacheStats:
		return interfaces.ResourceVector, interfaces.ActionRead

	// Embedding Operations - Write
	case OpEmbeddingGenerate, OpEmbeddingBatchGenerate, OpEmbeddingSetDefault, OpEmbeddingClearCache:
		return interfaces.ResourceVector, interfaces.ActionWrite

	// Ollama Operations - Read
	case OpOllamaListModels, OpOllamaGetModelInfo, OpOllamaHealth:
		return interfaces.ResourceVector, interfaces.ActionRead

	// Ollama Operations - Write
	case OpOllamaEmbed, OpOllamaEmbedWithModel, OpOllamaEmbedBatch:
		return interfaces.ResourceVector, interfaces.ActionWrite

	// RAG Operations - Read (uses Vector resource for read access)
	case OpRAGQuery, OpRAGBuildContext:
		return interfaces.ResourceVector, interfaces.ActionRead

	// Dynamic Secrets Operations - Read
	case OpDynamicSecretsGetRole, OpDynamicSecretsListRoles, OpDynamicSecretsListLeases:
		return interfaces.ResourceSecrets, interfaces.ActionRead

	// Dynamic Secrets Operations - Write
	case OpDynamicSecretsCreateRole, OpDynamicSecretsGenerate, OpDynamicSecretsRenewLease:
		return interfaces.ResourceSecrets, interfaces.ActionWrite

	// Dynamic Secrets Operations - Delete
	case OpDynamicSecretsDeleteRole, OpDynamicSecretsRevokeLease:
		return interfaces.ResourceSecrets, interfaces.ActionDelete

	// Certificate Operations - Read
	case OpCertVerify, OpCertGetCA, OpCertGetTLSConfig:
		return interfaces.ResourceCertificate, interfaces.ActionRead

	// Certificate Operations - Write
	case OpCertIssue, OpCertSignCSR:
		return interfaces.ResourceCertificate, interfaces.ActionWrite

	// Certificate Operations - Delete
	case OpCertRevoke:
		return interfaces.ResourceCertificate, interfaces.ActionDelete

	// Trust Operations - Read (cross-sign root pool)
	case OpTrustCrossSignedRootPool:
		return interfaces.ResourceCertificate, interfaces.ActionRead

	// Trust Operations - Write (AIA import mutates the trust store; DANE
	// publication mutates external DNS state)
	case OpTrustAutoImportAIA, OpTrustPublishDANE:
		return interfaces.ResourceCertificate, interfaces.ActionWrite

	// Parameter Store Operations - Read
	case OpParameterGet, OpParameterList, OpParameterBatchGet, OpParameterSearch:
		return interfaces.ResourceParameters, interfaces.ActionRead

	// Parameter Store Operations - Write
	case OpParameterSet, OpParameterBatchSet:
		return interfaces.ResourceParameters, interfaces.ActionWrite

	// Parameter Store Operations - Delete
	case OpParameterDelete, OpParameterBatchDelete:
		return interfaces.ResourceParameters, interfaces.ActionDelete

	// Orchestrator Operations - uses ResourceOrchestrator
	case OpOrchestrator:
		return interfaces.ResourceOrchestrator, interfaces.ActionWrite

	default:
		// Check if this is an IAM opcode (handled by build-tag-conditional function)
		if res, act, handled := getIAMResourceAndAction(opcode); handled {
			return res, act
		}
		return "unknown", interfaces.ActionRead
	}
}

// authorizeAndInject checks authorization for the request and, if authorized,
// returns the context enriched with the authenticated Subject so that
// service-layer per-key authorization can access the principal. On denial
// it returns a non-nil *Response and the original ctx.
func (h *Handler) authorizeAndInject(ctx context.Context, req *Request) (context.Context, *Response) {
	// Skip authorization for public operations
	if isPublicOperation(req.Opcode) {
		return ctx, nil
	}

	// If no auth service configured, allow all requests (backward compatibility)
	if h.authzService == nil && h.authManager == nil {
		return ctx, nil
	}

	// Check for missing token
	if req.Token == "" {
		return ctx, &Response{
			Status:  StatusUnauthorized,
			Payload: []byte("authentication required"),
		}
	}

	// Validate token
	var subject interfaces.Subject
	if h.authManager != nil {
		result, err := h.authManager.ValidateToken(ctx, req.Token)
		if err != nil {
			if h.logger != nil {
				h.logger.WithError(err).Debug("Token validation failed")
			}
			return ctx, &Response{
				Status:  StatusUnauthorized,
				Payload: []byte("invalid token"),
			}
		}

		// Extract subject from validation result
		// The result is *auth.TokenValidationResult
		if validationResult, ok := result.(*auth.TokenValidationResult); ok && validationResult.Valid {
			subject = interfaces.Subject{
				ID:            validationResult.UserID,
				Username:      validationResult.Username,
				Roles:         validationResult.Roles,
				Authenticated: true,
			}
		} else {
			return ctx, &Response{
				Status:  StatusUnauthorized,
				Payload: []byte("invalid token"),
			}
		}
	} else {
		// No auth manager, but authz service is present - use a default subject
		// This allows testing authz without full auth setup
		subject = interfaces.Subject{
			ID:            "unknown",
			Authenticated: true,
		}
	}

	// Perform authorization check if service is available
	if h.authzService != nil {
		resource, action := getResourceAndAction(req.Opcode)

		authzReq := &interfaces.AuthorizationRequest{
			Subject:  subject,
			Resource: resource,
			Action:   action,
		}

		decision, err := h.authzService.Authorize(ctx, authzReq)
		if err != nil {
			if h.logger != nil {
				h.logger.WithError(err).Error("Authorization check failed")
			}
			return ctx, &Response{
				Status:  StatusInternalError,
				Payload: []byte("authorization check failed"),
			}
		}

		if !decision.Allowed {
			reason := decision.Reason
			if reason == "" {
				reason = fmt.Sprintf("access denied to %s:%s", resource, action)
			}
			return ctx, &Response{
				Status:  StatusPermissionDenied,
				Payload: []byte(fmt.Sprintf("permission denied: %s", reason)),
			}
		}
	}

	// Inject Subject into context for service-layer per-key authorization.
	ctx = auth.SubjectIntoContext(ctx, subject)
	return ctx, nil
}

// authorize checks if the request is authorized.
// Returns nil if authorized, or an error response if not.
// Deprecated: use authorizeAndInject which also populates Subject in context.
func (h *Handler) authorize(ctx context.Context, req *Request) *Response {
	// Skip authorization for public operations
	if isPublicOperation(req.Opcode) {
		return nil
	}

	// If no auth service configured, allow all requests (backward compatibility)
	if h.authzService == nil && h.authManager == nil {
		return nil
	}

	// Check for missing token
	if req.Token == "" {
		return &Response{
			Status:  StatusUnauthorized,
			Payload: []byte("authentication required"),
		}
	}

	// Validate token
	var subject interfaces.Subject
	if h.authManager != nil {
		result, err := h.authManager.ValidateToken(ctx, req.Token)
		if err != nil {
			if h.logger != nil {
				h.logger.WithError(err).Debug("Token validation failed")
			}
			return &Response{
				Status:  StatusUnauthorized,
				Payload: []byte("invalid token"),
			}
		}

		// Extract subject from validation result
		// The result is *auth.TokenValidationResult
		if validationResult, ok := result.(*auth.TokenValidationResult); ok && validationResult.Valid {
			subject = interfaces.Subject{
				ID:            validationResult.UserID,
				Username:      validationResult.Username,
				Roles:         validationResult.Roles,
				Authenticated: true,
			}
		} else {
			return &Response{
				Status:  StatusUnauthorized,
				Payload: []byte("invalid token"),
			}
		}
	} else {
		// No auth manager, but authz service is present - use a default subject
		// This allows testing authz without full auth setup
		subject = interfaces.Subject{
			ID:            "unknown",
			Authenticated: true,
		}
	}

	// Perform authorization check if service is available
	if h.authzService != nil {
		resource, action := getResourceAndAction(req.Opcode)

		authzReq := &interfaces.AuthorizationRequest{
			Subject:  subject,
			Resource: resource,
			Action:   action,
		}

		decision, err := h.authzService.Authorize(ctx, authzReq)
		if err != nil {
			if h.logger != nil {
				h.logger.WithError(err).Error("Authorization check failed")
			}
			return &Response{
				Status:  StatusInternalError,
				Payload: []byte("authorization check failed"),
			}
		}

		if !decision.Allowed {
			reason := decision.Reason
			if reason == "" {
				reason = fmt.Sprintf("access denied to %s:%s", resource, action)
			}
			return &Response{
				Status:  StatusPermissionDenied,
				Payload: []byte(fmt.Sprintf("permission denied: %s", reason)),
			}
		}
	}

	return nil
}

// HandleRequest processes a protocol request and returns a response.
// It routes the request to the appropriate service method based on the opcode.
func (h *Handler) HandleRequest(ctx context.Context, req *Request) *Response {
	// Authorize and inject authenticated Subject into context so downstream
	// service-layer per-key authorization can access the principal.
	var authResp *Response
	ctx, authResp = h.authorizeAndInject(ctx, req)
	if authResp != nil {
		return authResp
	}

	switch req.Opcode {
	// KV Operations
	case OpPut:
		return h.handlePut(ctx, req)
	case OpGet:
		return h.handleGet(ctx, req)
	case OpDelete:
		return h.handleDelete(ctx, req)
	case OpList:
		return h.handleList(ctx, req)

	// Batch Operations
	case OpBatchPut:
		return h.handleBatchPut(ctx, req)
	case OpBatchGet:
		return h.handleBatchGet(ctx, req)
	case OpBatchDelete:
		return h.handleBatchDelete(ctx, req)

	// Transaction Operations
	case OpTransaction:
		return h.handleTransaction(ctx, req)

	// Query Operations
	case OpQuery:
		return h.handleQuery(ctx, req)
	case OpScanIndex:
		return h.handleScanIndex(ctx, req)

	// Cluster Operations
	case OpClusterStatus:
		return h.handleClusterStatus(ctx, req)
	case OpGetLeader:
		return h.handleGetLeader(ctx, req)
	case OpAddNode:
		return h.handleAddNode(ctx, req)
	case OpRemoveNode:
		return h.handleRemoveNode(ctx, req)
	case OpCreateShard:
		return h.handleCreateShard(ctx, req)
	case OpRebalanceCluster:
		return h.handleRebalanceCluster(ctx, req)
	case OpListNodes:
		return h.handleListNodes(ctx, req)
	case OpRebalanceStatus:
		return h.handleRebalanceStatus(ctx, req)
	case OpRebalancePlan:
		return h.handleRebalancePlan(ctx, req)
	case OpCancelMigration:
		return h.handleCancelMigration(ctx, req)
	case OpNodeReplaceRequest:
		return h.handleNodeReplaceRequest(ctx, req)

	// Backup Operations
	case OpCreateBackup:
		return h.handleCreateBackup(ctx, req)
	case OpListBackups:
		return h.handleListBackups(ctx, req)
	case OpRestoreBackup:
		return h.handleRestoreBackup(ctx, req)
	case OpDeleteBackup:
		return h.handleDeleteBackup(ctx, req)
	case OpVerifyBackup:
		return h.handleVerifyBackup(ctx, req)

	// Auth Operations (password login removed — use REST WebAuthn/PIV endpoints)
	case OpLogout:
		return h.handleLogout(ctx, req)
	case OpRefreshToken:
		return h.handleRefreshToken(ctx, req)
	case OpValidateToken:
		return h.handleValidateToken(ctx, req)

	// Unified login ceremony (BeginLogin / FinishLogin) -> LoginCeremony facade.
	case OpAuthBeginLogin:
		return h.handleBeginLogin(ctx, req)
	case OpAuthFinishLogin:
		return h.handleFinishLogin(ctx, req)

	// RBAC Operations
	case OpCreateUser:
		return h.handleCreateUser(ctx, req)
	case OpDeleteUser:
		return h.handleDeleteUser(ctx, req)
	case OpListUsers:
		return h.handleListUsers(ctx, req)
	case OpCreateRole:
		return h.handleCreateRole(ctx, req)
	case OpDeleteRole:
		return h.handleDeleteRole(ctx, req)
	case OpListRoles:
		return h.handleListRoles(ctx, req)
	case OpAssignRole:
		return h.handleAssignRole(ctx, req)
	case OpRevokeRole:
		return h.handleRevokeRole(ctx, req)

	// Cache Operations
	case OpGetCacheStats:
		return h.handleGetCacheStats(ctx, req)
	case OpClearCache:
		return h.handleClearCache(ctx, req)
	case OpGetCacheConfig:
		return h.handleGetCacheConfig(ctx, req)
	case OpUpdateCacheConfig:
		return h.handleUpdateCacheConfig(ctx, req)

	// Health Operations
	case OpHealth:
		return h.handleHealth(ctx, req)
	case OpLiveness:
		return h.handleLiveness(ctx, req)
	case OpReadiness:
		return h.handleReadiness(ctx, req)

	// Time-Series Operations
	case OpAggregate:
		return h.handleAggregate(ctx, req)
	case OpTimeSeriesQuery:
		return h.handleTimeSeriesQuery(ctx, req)
	case OpTSWritePoint:
		return h.handleTSWritePoint(ctx, req)
	case OpTSWriteBatch:
		return h.handleTSWriteBatch(ctx, req)
	case OpTSQuery:
		return h.handleTSQuery(ctx, req)
	case OpTSDeleteSeries:
		return h.handleTSDeleteSeries(ctx, req)
	case OpTSGetRetentionPolicy:
		return h.handleTSGetRetentionPolicy(ctx, req)
	case OpTSSetRetentionPolicy:
		return h.handleTSSetRetentionPolicy(ctx, req)
	case OpTSListMetrics:
		return h.handleTSListMetrics(ctx, req)

	// Operations Management
	case OpMigrateData:
		return h.handleMigrateData(ctx, req)
	case OpGetMigrationProgress:
		return h.handleGetMigrationProgress(ctx, req)
	case OpReplaceNode:
		return h.handleReplaceNode(ctx, req)
	case OpUpgradeNode:
		return h.handleUpgradeNode(ctx, req)
	case OpGetUpgradeStatus:
		return h.handleGetUpgradeStatus(ctx, req)

	// Barrier Operations - dispatch based on sub-opcode in payload (replaces
	// the deprecated seal-tree opcodes 0xC0-0xC4 removed in Phase 8c).
	case OpBarrier:
		return h.handleBarrierDispatch(ctx, req)

	// Ceremony Operations - dispatch based on sub-opcode in payload.
	case OpCeremony:
		return h.handleCeremonyDispatch(ctx, req)

	// KMS Operations
	case OpKMSGenerateKey:
		return h.handleKMSGenerateKey(ctx, req)
	case OpKMSImportKey:
		return h.handleKMSImportKey(ctx, req)
	case OpKMSGetKey:
		return h.handleKMSGetKey(ctx, req)
	case OpKMSListKeys:
		return h.handleKMSListKeys(ctx, req)
	case OpKMSRotateKey:
		return h.handleKMSRotateKey(ctx, req)
	case OpKMSDeleteKey:
		return h.handleKMSDeleteKey(ctx, req)
	case OpKMSEncrypt:
		return h.handleKMSEncrypt(ctx, req)
	case OpKMSDecrypt:
		return h.handleKMSDecrypt(ctx, req)

	// KMS DEK Operations
	case OpKMSDEKCreate:
		return h.handleKMSDEKCreate(ctx, req)
	case OpKMSDEKGet:
		return h.handleKMSDEKGet(ctx, req)
	case OpKMSDEKList:
		return h.handleKMSDEKList(ctx, req)
	case OpKMSDEKRotate:
		return h.handleKMSDEKRotate(ctx, req)
	case OpKMSDEKDelete:
		return h.handleKMSDEKDelete(ctx, req)
	case OpKMSDEKStatus:
		return h.handleKMSDEKStatus(ctx, req)
	case OpKMSDEKSetDefault:
		return h.handleKMSDEKSetDefault(ctx, req)
	case OpKMSDEKSetSafety:
		return h.handleKMSDEKSetSafety(ctx, req)
	case OpKMSDEKClearSafety:
		return h.handleKMSDEKClearSafety(ctx, req)
	case OpKMSDEKStartReencryption:
		return h.handleKMSDEKStartReencryption(ctx, req)
	case OpKMSDEKGetReencryptProgress:
		return h.handleKMSDEKGetReencryptProgress(ctx, req)

	// KMS Backend Operations
	case OpKMSBackendStatus:
		return h.handleKMSBackendStatus(ctx, req)
	case OpKMSBackendTest:
		return h.handleKMSBackendTest(ctx, req)
	case OpKMSKeyVersions:
		return h.handleKMSKeyVersions(ctx, req)
	case OpKMSKeyStatus:
		return h.handleKMSKeyStatus(ctx, req)

	// KMS Policy Operations
	case OpKMSSetKeyPolicy:
		return h.handleKMSSetKeyPolicy(ctx, req)
	case OpKMSGetKeyPolicy:
		return h.handleKMSGetKeyPolicy(ctx, req)

	// KMS Rotation Policy Operations
	case OpKMSSetRotationPolicy:
		return h.handleKMSSetRotationPolicy(ctx, req)
	case OpKMSGetRotationPolicy:
		return h.handleKMSGetRotationPolicy(ctx, req)
	case OpKMSDeleteRotationPolicy:
		return h.handleKMSDeleteRotationPolicy(ctx, req)

	// KMS Safety Operations
	case OpKMSSetKeySafety:
		return h.handleKMSSetKeySafety(ctx, req)
	case OpKMSClearKeySafety:
		return h.handleKMSClearKeySafety(ctx, req)
	case OpKMSSafetyCheck:
		return h.handleKMSSafetyCheck(ctx, req)
	case OpKMSSafetyReport:
		return h.handleKMSSafetyReport(ctx, req)

	// KMS Export/Unwrap Operations
	case OpKMSExportKey:
		return h.handleKMSExportKey(ctx, req)
	case OpKMSUnwrapKey:
		return h.handleKMSUnwrapKey(ctx, req)

	// KMS Metadata Operations
	case OpKMSGetKeyMetadata:
		return h.handleKMSGetKeyMetadata(ctx, req)
	case OpKMSUpdateKeyMetadata:
		return h.handleKMSUpdateKeyMetadata(ctx, req)

	// Secrets Operations
	case OpSecretsStore:
		return h.handleSecretsStore(ctx, req)
	case OpSecretsGet:
		return h.handleSecretsGet(ctx, req)
	case OpSecretsList:
		return h.handleSecretsList(ctx, req)
	case OpSecretsDelete:
		return h.handleSecretsDelete(ctx, req)
	case OpSecretsListVersions:
		return h.handleSecretsListVersions(ctx, req)
	case OpSecretsRollback:
		return h.handleSecretsRollback(ctx, req)
	case OpSecretsRotate:
		return h.handleSecretsRotate(ctx, req)
	case OpSecretsGetVersion:
		return h.handleSecretsGetVersion(ctx, req)
	case OpSecretsSetRotation:
		return h.handleSecretsSetRotation(ctx, req)
	case OpSecretsGetRotation:
		return h.handleSecretsGetRotation(ctx, req)
	case OpSecretsUpdateStatus:
		return h.handleSecretsUpdateStatus(ctx, req)
	case OpSecretsGetByPrefix:
		return h.handleSecretsGetByPrefix(ctx, req)
	case OpSecretsWatch:
		return h.handleSecretsWatch(ctx, req)

	// Crypto Operations
	case OpCryptoEncrypt:
		return h.handleCryptoEncrypt(ctx, req)
	case OpCryptoDecrypt:
		return h.handleCryptoDecrypt(ctx, req)
	case OpCryptoRewrap:
		return h.handleCryptoRewrap(ctx, req)
	case OpCryptoSign:
		return h.handleCryptoSign(ctx, req)
	case OpCryptoVerify:
		return h.handleCryptoVerify(ctx, req)
	case OpCryptoHash:
		return h.handleCryptoHash(ctx, req)
	case OpCryptoRandom:
		return h.handleCryptoRandom(ctx, req)
	case OpCryptoDataKey:
		return h.handleCryptoDataKey(ctx, req)
	case OpCryptoHMAC:
		return h.handleCryptoGenerateHMAC(ctx, req)
	case OpCryptoVerifyHMAC:
		return h.handleCryptoVerifyHMAC(ctx, req)

	// Vector Operations - Namespace Management
	case OpVectorCreateNamespace:
		return h.handleVectorCreateNamespace(ctx, req)
	case OpVectorDeleteNamespace:
		return h.handleVectorDeleteNamespace(ctx, req)
	case OpVectorListNamespaces:
		return h.handleVectorListNamespaces(ctx, req)
	case OpVectorGetNamespaceInfo:
		return h.handleVectorGetNamespaceInfo(ctx, req)

	// Vector Operations - CRUD
	case OpVectorUpsert:
		return h.handleVectorUpsert(ctx, req)
	case OpVectorGet:
		return h.handleVectorGet(ctx, req)
	case OpVectorDelete:
		return h.handleVectorDelete(ctx, req)
	case OpVectorBatchUpsert:
		return h.handleVectorBatchUpsert(ctx, req)

	// Vector Operations - Search
	case OpVectorSearchSimilar:
		return h.handleVectorSearchSimilar(ctx, req)
	case OpVectorSearchByMetadata:
		return h.handleVectorSearchByMetadata(ctx, req)
	case OpVectorHybridSearch:
		return h.handleVectorHybridSearch(ctx, req)
	case OpVectorSearchByID:
		return h.handleVectorSearchByID(ctx, req)

	// Vector Operations - Index Management
	case OpVectorRebuildIndex:
		return h.handleVectorRebuildIndex(ctx, req)
	case OpVectorGetRebuildProgress:
		return h.handleVectorGetRebuildProgress(ctx, req)
	case OpVectorGetIndexStats:
		return h.handleVectorGetIndexStats(ctx, req)

	// Vector Operations - Compaction
	case OpVectorCompact:
		return h.handleVectorCompact(ctx, req)
	case OpVectorGetCompactionProgress:
		return h.handleVectorGetCompactionProgress(ctx, req)

	// ObjectStorage Operations
	case OpOSPutObject:
		return h.handleOSPutObject(ctx, req)
	case OpOSGetObject:
		return h.handleOSGetObject(ctx, req)
	case OpOSDeleteObject:
		return h.handleOSDeleteObject(ctx, req)
	case OpOSHeadObject:
		return h.handleOSHeadObject(ctx, req)
	case OpOSListObjects:
		return h.handleOSListObjects(ctx, req)
	case OpOSListByTag:
		return h.handleOSListByTag(ctx, req)
	case OpOSListByTenant:
		return h.handleOSListByTenant(ctx, req)
	case OpOSCopyObject:
		return h.handleOSCopyObject(ctx, req)
	case OpOSReplicateObject:
		return h.handleOSReplicateObject(ctx, req)
	case OpOSMigrateObject:
		return h.handleOSMigrateObject(ctx, req)
	case OpOSArchiveObject:
		return h.handleOSArchiveObject(ctx, req)
	case OpOSRestoreObject:
		return h.handleOSRestoreObject(ctx, req)
	case OpOSVerifyObject:
		return h.handleOSVerifyObject(ctx, req)
	case OpOSGetObjectMetadata:
		return h.handleOSGetObjectMetadata(ctx, req)
	case OpOSUpdateObjectMetadata:
		return h.handleOSUpdateObjectMetadata(ctx, req)
	case OpOSGetStorageStats:
		return h.handleOSGetStorageStats(ctx, req)
	case OpOSSetLifecyclePolicy:
		return h.handleOSSetLifecyclePolicy(ctx, req)
	case OpOSGetLifecyclePolicy:
		return h.handleOSGetLifecyclePolicy(ctx, req)

	// ObjectStorage Extended Operations
	case OpOSGetObjectPolicy:
		return h.handleOSGetObjectPolicy(ctx, req)
	case OpOSListBackends:
		return h.handleOSListBackends(ctx, req)
	case OpOSGetBackendHealth:
		return h.handleOSGetBackendHealth(ctx, req)
	case OpOSSetTenantQuotas:
		return h.handleOSSetTenantQuotas(ctx, req)

	// ObjectStorage Batch Operations
	case OpOSBatchPutObjects:
		return h.handleOSBatchPutObjects(ctx, req)
	case OpOSBatchGetObjects:
		return h.handleOSBatchGetObjects(ctx, req)
	case OpOSBatchDeleteObjects:
		return h.handleOSBatchDeleteObjects(ctx, req)

	// ObjectStorage Admin Operations - sub-op dispatch
	case OpOSAdmin:
		return h.handleOSAdminDispatch(ctx, req)

	// ObjectTransfer Operations (peer-to-peer object data transfer)
	case OpFetchObject:
		return h.handleFetchObject(ctx, req)
	case OpStreamFetchObjects:
		return h.handleStreamFetchObjects(ctx, req)

	// Embedding Operations
	case OpEmbeddingGenerate:
		return h.handleEmbeddingGenerate(ctx, req)
	case OpEmbeddingBatchGenerate:
		return h.handleEmbeddingBatchGenerate(ctx, req)
	case OpEmbeddingListProviders:
		return h.handleEmbeddingListProviders(ctx, req)
	case OpEmbeddingGetProvider:
		return h.handleEmbeddingGetProvider(ctx, req)
	case OpEmbeddingSetDefault:
		return h.handleEmbeddingSetDefault(ctx, req)
	case OpEmbeddingGetCacheStats:
		return h.handleEmbeddingGetCacheStats(ctx, req)
	case OpEmbeddingClearCache:
		return h.handleEmbeddingClearCache(ctx, req)

	// Ollama Operations
	case OpOllamaEmbed:
		return h.handleOllamaEmbed(ctx, req)
	case OpOllamaEmbedWithModel:
		return h.handleOllamaEmbedWithModel(ctx, req)
	case OpOllamaEmbedBatch:
		return h.handleOllamaEmbedBatch(ctx, req)
	case OpOllamaListModels:
		return h.handleOllamaListModels(ctx, req)
	case OpOllamaGetModelInfo:
		return h.handleOllamaGetModelInfo(ctx, req)
	case OpOllamaHealth:
		return h.handleOllamaHealth(ctx, req)

	// RAG Operations
	case OpRAGQuery:
		return h.handleRAGQuery(ctx, req)
	case OpRAGBuildContext:
		return h.handleRAGBuildContext(ctx, req)

	// Certificate Operations
	case OpCertIssue:
		return h.handleCertIssue(ctx, req)
	case OpCertSignCSR:
		return h.handleCertSignCSR(ctx, req)
	case OpCertRevoke:
		return h.handleCertRevoke(ctx, req)
	case OpCertVerify:
		return h.handleCertVerify(ctx, req)
	case OpCertGetCA:
		return h.handleCertGetCA(ctx, req)
	case OpCertGetTLSConfig:
		return h.handleCertGetTLSConfig(ctx, req)

	// Trust Operations
	case OpTrustCrossSignedRootPool:
		return h.handleTrustCrossSignedRootPool(ctx, req)
	case OpTrustAutoImportAIA:
		return h.handleTrustAutoImportAIA(ctx, req)
	case OpTrustPublishDANE:
		return h.handleTrustPublishDANE(ctx, req)

	// Dynamic Secrets Operations
	case OpDynamicSecretsCreateRole:
		return h.handleDynamicSecretsCreateRole(ctx, req)
	case OpDynamicSecretsGetRole:
		return h.handleDynamicSecretsGetRole(ctx, req)
	case OpDynamicSecretsListRoles:
		return h.handleDynamicSecretsListRoles(ctx, req)
	case OpDynamicSecretsDeleteRole:
		return h.handleDynamicSecretsDeleteRole(ctx, req)
	case OpDynamicSecretsGenerate:
		return h.handleDynamicSecretsGenerate(ctx, req)
	case OpDynamicSecretsRenewLease:
		return h.handleDynamicSecretsRenewLease(ctx, req)
	case OpDynamicSecretsRevokeLease:
		return h.handleDynamicSecretsRevokeLease(ctx, req)
	case OpDynamicSecretsListLeases:
		return h.handleDynamicSecretsListLeases(ctx, req)

	// Parameter Store Operations
	case OpParameterGet:
		return h.handleParameterGet(ctx, req)
	case OpParameterSet:
		return h.handleParameterSet(ctx, req)
	case OpParameterDelete:
		return h.handleParameterDelete(ctx, req)
	case OpParameterList:
		return h.handleParameterList(ctx, req)
	case OpParameterBatchGet:
		return h.handleParameterBatchGet(ctx, req)
	case OpParameterBatchSet:
		return h.handleParameterBatchSet(ctx, req)
	case OpParameterBatchDelete:
		return h.handleParameterBatchDelete(ctx, req)
	case OpParameterSearch:
		return h.handleParameterSearch(ctx, req)

	// Scheduler Operations - dispatch based on sub-opcode in payload
	case OpScheduler:
		return h.handleSchedulerDispatch(ctx, req)

	// Orchestrator Operations - dispatch based on sub-opcode in payload
	case OpOrchestrator:
		return h.handleOrchestratorDispatch(ctx, req)

	// IAM Operations - handled via IAM service layer (build-tag conditional)
	// Direct IAM Tenant Operations (0x95-0x99)
	case OpIAMCreateTenant, OpIAMGetTenant, OpIAMUpdateTenant, OpIAMDeleteTenant, OpIAMListTenants,
		// Direct IAM User Operations (0x9A-0x9E)
		OpIAMCreateUser, OpIAMGetUser, OpIAMUpdateUser, OpIAMDeleteUser, OpIAMListUsers,
		// Direct IAM Group Operations (0x9F, 0xAB-0xAF, 0xB5)
		OpIAMCreateGroup, OpIAMGetGroup, OpIAMUpdateGroup, OpIAMDeleteGroup, OpIAMListGroups,
		OpIAMAddUserToGroup, OpIAMRemoveUserFromGroup,
		// Direct IAM Role Operations (0xB6-0xBC)
		OpIAMCreateRole, OpIAMGetRole, OpIAMUpdateRole, OpIAMDeleteRole, OpIAMListRoles,
		OpIAMAssignRole, OpIAMRevokeRole,
		// Direct IAM App Operations (0xBD-0xBF, 0xC5-0xC7)
		OpIAMRegisterApp, OpIAMGetApp, OpIAMUpdateApp, OpIAMDeleteApp, OpIAMListApps,
		OpIAMRotateAppSecret,
		// Direct IAM Policy Operations (0xC8-0xCE)
		OpIAMCreatePolicy, OpIAMGetPolicy, OpIAMUpdatePolicy, OpIAMDeletePolicy, OpIAMListPolicies,
		OpIAMValidatePolicy, OpIAMTestPolicy,
		// Direct PEP Operations (0x5B-0x5D)
		OpPEPCheck, OpPEPCheckBatch, OpPEPSubscribe,
		// IAM sub-dispatch operations (0xFD-0xFF) - App, MFA, PEP
		// These opcodes are defined in protocol_iam.go (iam build tag)
		// but must route here for tryHandleIAMRequest dispatch.
		0xFD, 0xFE, 0xFF:
		if resp := h.tryHandleIAMRequest(ctx, req); resp != nil {
			return resp
		}
		return &Response{
			Status:  StatusUnavailable,
			Payload: []byte("IAM service not available (IAM feature not enabled)"),
		}

	default:
		return &Response{
			Status:  StatusInvalidRequest,
			Payload: []byte(fmt.Sprintf("unknown opcode: 0x%02X", req.Opcode)),
		}
	}
}

// =============================================================================
// KV Operations Handlers
// =============================================================================

func (h *Handler) handlePut(ctx context.Context, req *Request) *Response {
	if h.service == nil {
		return h.errorResponse(api.NewServiceUnavailableError("QuicBaaS"))
	}

	var putReq service.PutRequest
	if err := DecodePayload(req.Payload, &putReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.service.Put(ctx, &putReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleGet(ctx context.Context, req *Request) *Response {
	if h.service == nil {
		return h.errorResponse(api.NewServiceUnavailableError("QuicBaaS"))
	}

	var getReq service.GetRequest
	if err := DecodePayload(req.Payload, &getReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.service.Get(ctx, &getReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleDelete(ctx context.Context, req *Request) *Response {
	if h.service == nil {
		return h.errorResponse(api.NewServiceUnavailableError("QuicBaaS"))
	}

	var delReq service.DeleteRequest
	if err := DecodePayload(req.Payload, &delReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.service.Delete(ctx, &delReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleList(ctx context.Context, req *Request) *Response {
	if h.service == nil {
		return h.errorResponse(api.NewServiceUnavailableError("QuicBaaS"))
	}

	var listReq service.ListRequest
	if err := DecodePayload(req.Payload, &listReq); err != nil {
		return h.errorResponse(err)
	}

	// For streaming operations, we collect results into a single response
	// Note: For true streaming, the client would need to open multiple streams
	// or use a different protocol pattern (e.g., server-initiated streams)
	stream := make(chan *service.ListResponse, 100)
	errChan := make(chan error, 1)

	go func() {
		// Note: service.List closes the stream channel when done, so we don't close it here
		errChan <- h.service.List(ctx, &listReq, stream)
	}()

	// Collect all entities from streaming responses into a single aggregated response
	aggregatedResponse := &service.ListResponse{
		Entities: make([]*service.Entity, 0),
	}

	// Use select to handle context cancellation while collecting stream results
collectLoop:
	for {
		select {
		case <-ctx.Done():
			return h.errorResponse(ctx.Err())
		case listResp, ok := <-stream:
			if !ok {
				break collectLoop
			}
			if listResp != nil && listResp.Entities != nil {
				aggregatedResponse.Entities = append(aggregatedResponse.Entities, listResp.Entities...)
				aggregatedResponse.TotalCount = listResp.TotalCount
				aggregatedResponse.NextPageToken = listResp.NextPageToken
			}
		}
	}

	// Wait for error with context cancellation support
	select {
	case <-ctx.Done():
		return h.errorResponse(ctx.Err())
	case err := <-errChan:
		if err != nil {
			return h.errorResponse(err)
		}
	}

	payload, err := EncodePayload(aggregatedResponse)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// =============================================================================
// Batch Operations Handlers
// =============================================================================

func (h *Handler) handleBatchPut(ctx context.Context, req *Request) *Response {
	if h.service == nil {
		return h.errorResponse(api.NewServiceUnavailableError("QuicBaaS"))
	}

	var batchReq service.BatchPutRequest
	if err := DecodePayload(req.Payload, &batchReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.service.BatchPut(ctx, &batchReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleBatchGet(ctx context.Context, req *Request) *Response {
	if h.service == nil {
		return h.errorResponse(api.NewServiceUnavailableError("QuicBaaS"))
	}

	var batchReq service.BatchGetRequest
	if err := DecodePayload(req.Payload, &batchReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.service.BatchGet(ctx, &batchReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleBatchDelete(ctx context.Context, req *Request) *Response {
	if h.service == nil {
		return h.errorResponse(api.NewServiceUnavailableError("QuicBaaS"))
	}

	var batchReq service.BatchDeleteRequest
	if err := DecodePayload(req.Payload, &batchReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.service.BatchDelete(ctx, &batchReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// =============================================================================
// Transaction Operations Handlers
// =============================================================================

func (h *Handler) handleTransaction(ctx context.Context, req *Request) *Response {
	if h.service == nil {
		return h.errorResponse(api.NewServiceUnavailableError("QuicBaaS"))
	}

	var txnReq service.TransactionRequest
	if err := DecodePayload(req.Payload, &txnReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.service.Transaction(ctx, &txnReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// =============================================================================
// Query Operations Handlers
// =============================================================================

func (h *Handler) handleQuery(ctx context.Context, req *Request) *Response {
	if h.service == nil {
		return h.errorResponse(api.NewServiceUnavailableError("QuicBaaS"))
	}

	var queryReq service.QueryRequest
	if err := DecodePayload(req.Payload, &queryReq); err != nil {
		return h.errorResponse(err)
	}

	stream := make(chan *service.QueryResponse, 100)
	errChan := make(chan error, 1)

	go func() {
		// Note: service.Query closes the stream channel when done, so we don't close it here
		errChan <- h.service.Query(ctx, &queryReq, stream)
	}()

	var results []*service.QueryResponse
	for queryResp := range stream {
		results = append(results, queryResp)
	}

	if err := <-errChan; err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(results)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleScanIndex(ctx context.Context, req *Request) *Response {
	if h.service == nil {
		return h.errorResponse(api.NewServiceUnavailableError("QuicBaaS"))
	}

	var scanReq service.ScanRequest
	if err := DecodePayload(req.Payload, &scanReq); err != nil {
		return h.errorResponse(err)
	}

	stream := make(chan *service.ScanResponse, 100)
	errChan := make(chan error, 1)

	go func() {
		// Note: service.ScanIndex closes the stream channel when done, so we don't close it here
		errChan <- h.service.ScanIndex(ctx, &scanReq, stream)
	}()

	var results []*service.ScanResponse
	for scanResp := range stream {
		results = append(results, scanResp)
	}

	if err := <-errChan; err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(results)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// =============================================================================
// Cluster Operations Handlers
// =============================================================================

func (h *Handler) handleClusterStatus(ctx context.Context, req *Request) *Response {
	if h.service == nil {
		return h.errorResponse(api.NewServiceUnavailableError("QuicBaaS"))
	}

	var statusReq service.ClusterStatusRequest
	if err := DecodePayload(req.Payload, &statusReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.service.ClusterStatus(ctx, &statusReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleGetLeader(ctx context.Context, req *Request) *Response {
	if h.service == nil {
		return h.errorResponse(api.NewServiceUnavailableError("QuicBaaS"))
	}

	var leaderReq service.GetLeaderRequest
	if err := DecodePayload(req.Payload, &leaderReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.service.GetLeader(ctx, &leaderReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleAddNode(ctx context.Context, req *Request) *Response {
	if h.service == nil {
		return h.errorResponse(api.NewServiceUnavailableError("QuicBaaS"))
	}

	var addReq service.AddNodeRequest
	if err := DecodePayload(req.Payload, &addReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.service.AddNode(ctx, &addReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleRemoveNode(ctx context.Context, req *Request) *Response {
	if h.service == nil {
		return h.errorResponse(api.NewServiceUnavailableError("QuicBaaS"))
	}

	var removeReq service.RemoveNodeRequest
	if err := DecodePayload(req.Payload, &removeReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.service.RemoveNode(ctx, &removeReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleCreateShard(ctx context.Context, req *Request) *Response {
	if h.service == nil {
		return h.errorResponse(api.NewServiceUnavailableError("QuicBaaS"))
	}

	var createReq service.CreateShardRequest
	if err := DecodePayload(req.Payload, &createReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.service.CreateShard(ctx, &createReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleRebalanceCluster(ctx context.Context, req *Request) *Response {
	if h.service == nil {
		return h.errorResponse(api.NewServiceUnavailableError("QuicBaaS"))
	}

	var rebalanceReq service.RebalanceClusterRequest
	if err := DecodePayload(req.Payload, &rebalanceReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.service.RebalanceCluster(ctx, &rebalanceReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// =============================================================================
// Backup Operations Handlers
// =============================================================================

func (h *Handler) handleCreateBackup(ctx context.Context, req *Request) *Response {
	if h.service == nil {
		return h.errorResponse(api.NewServiceUnavailableError("QuicBaaS"))
	}

	var backupReq service.CreateBackupRequest
	if err := DecodePayload(req.Payload, &backupReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.service.CreateBackup(ctx, &backupReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleListBackups(ctx context.Context, req *Request) *Response {
	if h.service == nil {
		return h.errorResponse(api.NewServiceUnavailableError("QuicBaaS"))
	}

	var listReq service.ListBackupsRequest
	if err := DecodePayload(req.Payload, &listReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.service.ListBackups(ctx, &listReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleRestoreBackup(ctx context.Context, req *Request) *Response {
	if h.service == nil {
		return h.errorResponse(api.NewServiceUnavailableError("QuicBaaS"))
	}

	var restoreReq service.RestoreBackupRequest
	if err := DecodePayload(req.Payload, &restoreReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.service.RestoreBackup(ctx, &restoreReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleDeleteBackup(ctx context.Context, req *Request) *Response {
	if h.service == nil {
		return h.errorResponse(api.NewServiceUnavailableError("QuicBaaS"))
	}

	var deleteReq service.DeleteBackupRequest
	if err := DecodePayload(req.Payload, &deleteReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.service.DeleteBackup(ctx, &deleteReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleVerifyBackup(ctx context.Context, req *Request) *Response {
	if h.service == nil {
		return h.errorResponse(api.NewServiceUnavailableError("QuicBaaS"))
	}

	var verifyReq service.VerifyBackupRequest
	if err := DecodePayload(req.Payload, &verifyReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.service.VerifyBackup(ctx, &verifyReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// =============================================================================
// Auth Operations Handlers
// =============================================================================

func (h *Handler) handleLogout(ctx context.Context, req *Request) *Response {
	if h.service == nil {
		return h.errorResponse(api.NewServiceUnavailableError("QuicBaaS"))
	}

	var logoutReq service.LogoutRequest
	if err := DecodePayload(req.Payload, &logoutReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.service.Logout(ctx, &logoutReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleRefreshToken(ctx context.Context, req *Request) *Response {
	if h.service == nil {
		return h.errorResponse(api.NewServiceUnavailableError("QuicBaaS"))
	}

	var refreshReq service.RefreshTokenRequest
	if err := DecodePayload(req.Payload, &refreshReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.service.RefreshToken(ctx, &refreshReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleValidateToken(ctx context.Context, req *Request) *Response {
	if h.service == nil {
		return h.errorResponse(api.NewServiceUnavailableError("QuicBaaS"))
	}

	var validateReq service.ValidateTokenRequest
	if err := DecodePayload(req.Payload, &validateReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.service.ValidateToken(ctx, &validateReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// =============================================================================
// RBAC Operations Handlers
// =============================================================================

func (h *Handler) handleCreateUser(ctx context.Context, req *Request) *Response {
	if h.service == nil {
		return h.errorResponse(api.NewServiceUnavailableError("QuicBaaS"))
	}

	var createReq service.CreateUserRequest
	if err := DecodePayload(req.Payload, &createReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.service.CreateUser(ctx, &createReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleDeleteUser(ctx context.Context, req *Request) *Response {
	if h.service == nil {
		return h.errorResponse(api.NewServiceUnavailableError("QuicBaaS"))
	}

	var deleteReq service.DeleteUserRequest
	if err := DecodePayload(req.Payload, &deleteReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.service.DeleteUser(ctx, &deleteReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleListUsers(ctx context.Context, req *Request) *Response {
	if h.service == nil {
		return h.errorResponse(api.NewServiceUnavailableError("QuicBaaS"))
	}

	var listReq service.ListUsersRequest
	if err := DecodePayload(req.Payload, &listReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.service.ListUsers(ctx, &listReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleCreateRole(ctx context.Context, req *Request) *Response {
	if h.service == nil {
		return h.errorResponse(api.NewServiceUnavailableError("QuicBaaS"))
	}

	var createReq service.AuthCreateRoleRequest
	if err := DecodePayload(req.Payload, &createReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.service.CreateAuthRole(ctx, &createReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleDeleteRole(ctx context.Context, req *Request) *Response {
	if h.service == nil {
		return h.errorResponse(api.NewServiceUnavailableError("QuicBaaS"))
	}

	var deleteReq service.AuthDeleteRoleRequest
	if err := DecodePayload(req.Payload, &deleteReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.service.DeleteAuthRole(ctx, &deleteReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleListRoles(ctx context.Context, req *Request) *Response {
	if h.service == nil {
		return h.errorResponse(api.NewServiceUnavailableError("QuicBaaS"))
	}

	var listReq service.AuthListRolesRequest
	if err := DecodePayload(req.Payload, &listReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.service.ListAuthRoles(ctx, &listReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleAssignRole(ctx context.Context, req *Request) *Response {
	if h.service == nil {
		return h.errorResponse(api.NewServiceUnavailableError("QuicBaaS"))
	}

	var assignReq service.AssignRoleRequest
	if err := DecodePayload(req.Payload, &assignReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.service.AssignRole(ctx, &assignReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleRevokeRole(ctx context.Context, req *Request) *Response {
	if h.service == nil {
		return h.errorResponse(api.NewServiceUnavailableError("QuicBaaS"))
	}

	var revokeReq service.RevokeRoleRequest
	if err := DecodePayload(req.Payload, &revokeReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.service.RevokeRole(ctx, &revokeReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// =============================================================================
// Cache Operations Handlers
// =============================================================================

func (h *Handler) handleGetCacheStats(ctx context.Context, req *Request) *Response {
	if h.service == nil {
		return h.errorResponse(api.NewServiceUnavailableError("QuicBaaS"))
	}

	var statsReq service.GetCacheStatsRequest
	if err := DecodePayload(req.Payload, &statsReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.service.GetCacheStats(ctx, &statsReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleClearCache(ctx context.Context, req *Request) *Response {
	if h.service == nil {
		return h.errorResponse(api.NewServiceUnavailableError("QuicBaaS"))
	}

	var clearReq service.ClearCacheRequest
	if err := DecodePayload(req.Payload, &clearReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.service.ClearCache(ctx, &clearReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleGetCacheConfig(ctx context.Context, req *Request) *Response {
	if h.service == nil {
		return h.errorResponse(api.NewServiceUnavailableError("QuicBaaS"))
	}

	var configReq service.GetCacheConfigRequest
	if err := DecodePayload(req.Payload, &configReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.service.GetCacheConfig(ctx, &configReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleUpdateCacheConfig(ctx context.Context, req *Request) *Response {
	if h.service == nil {
		return h.errorResponse(api.NewServiceUnavailableError("QuicBaaS"))
	}

	var updateReq service.UpdateCacheConfigRequest
	if err := DecodePayload(req.Payload, &updateReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.service.UpdateCacheConfig(ctx, &updateReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// =============================================================================
// Health Operations Handlers
// =============================================================================

func (h *Handler) handleHealth(ctx context.Context, req *Request) *Response {
	if h.service == nil {
		return h.errorResponse(api.NewServiceUnavailableError("QuicBaaS"))
	}

	var healthReq service.HealthRequest
	if err := DecodePayload(req.Payload, &healthReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.service.Health(ctx, &healthReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleLiveness(ctx context.Context, req *Request) *Response {
	if h.service == nil {
		return h.errorResponse(api.NewServiceUnavailableError("QuicBaaS"))
	}

	var livenessReq service.LivenessRequest
	if err := DecodePayload(req.Payload, &livenessReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.service.Liveness(ctx, &livenessReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleReadiness(ctx context.Context, req *Request) *Response {
	if h.service == nil {
		return h.errorResponse(api.NewServiceUnavailableError("QuicBaaS"))
	}

	var readinessReq service.ReadinessRequest
	if err := DecodePayload(req.Payload, &readinessReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.service.Readiness(ctx, &readinessReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// =============================================================================
// Time-Series Operations Handlers
// =============================================================================

func (h *Handler) handleAggregate(ctx context.Context, req *Request) *Response {
	if h.service == nil {
		return h.errorResponse(api.NewServiceUnavailableError("QuicBaaS"))
	}

	var aggReq service.AggregateRequest
	if err := DecodePayload(req.Payload, &aggReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.service.Aggregate(ctx, &aggReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleTimeSeriesQuery(ctx context.Context, req *Request) *Response {
	if h.service == nil {
		return h.errorResponse(api.NewServiceUnavailableError("QuicBaaS"))
	}

	var tsReq service.TimeSeriesQueryRequest
	if err := DecodePayload(req.Payload, &tsReq); err != nil {
		return h.errorResponse(err)
	}

	stream := make(chan *service.TimeSeriesQueryResponse, 100)
	errChan := make(chan error, 1)

	go func() {
		// Note: service.TimeSeriesQuery closes the stream channel when done, so we don't close it here
		errChan <- h.service.TimeSeriesQuery(ctx, &tsReq, stream)
	}()

	var results []*service.TimeSeriesQueryResponse
	for tsResp := range stream {
		results = append(results, tsResp)
	}

	if err := <-errChan; err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(results)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// TimeSeriesService interface for type assertion.
type timeseriesServiceInterface interface {
	WritePoint(ctx context.Context, req *service.WritePointRequest) (*service.WritePointResponse, error)
	WriteBatch(ctx context.Context, req *service.WriteBatchRequest) (*service.WriteBatchResponse, error)
	TSQuery(ctx context.Context, req *service.TSQueryRequest) (*service.TSQueryResponse, error)
	DeleteSeries(ctx context.Context, req *service.TSDeleteRequest) (*service.TSDeleteResponse, error)
	GetRetentionPolicy(ctx context.Context, req *service.TSGetRetentionPolicyRequest) (*service.TSGetRetentionPolicyResponse, error)
	SetRetentionPolicy(ctx context.Context, req *service.TSSetRetentionPolicyRequest) (*service.TSSetRetentionPolicyResponse, error)
	ListMetrics(ctx context.Context, req *service.TSListMetricsRequest) (*service.TSListMetricsResponse, error)
}

func (h *Handler) handleTSWritePoint(ctx context.Context, req *Request) *Response {
	if h.service == nil {
		return h.errorResponse(api.NewServiceUnavailableError("QuicBaaS"))
	}

	var writeReq service.WritePointRequest
	if err := DecodePayload(req.Payload, &writeReq); err != nil {
		return h.errorResponse(err)
	}

	tsService, ok := h.service.(timeseriesServiceInterface)
	if !ok {
		return h.errorResponse(api.NewServiceUnavailableError("timeseries"))
	}

	resp, err := tsService.WritePoint(ctx, &writeReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleTSWriteBatch(ctx context.Context, req *Request) *Response {
	if h.service == nil {
		return h.errorResponse(api.NewServiceUnavailableError("QuicBaaS"))
	}

	var batchReq service.WriteBatchRequest
	if err := DecodePayload(req.Payload, &batchReq); err != nil {
		return h.errorResponse(err)
	}

	tsService, ok := h.service.(timeseriesServiceInterface)
	if !ok {
		return h.errorResponse(api.NewServiceUnavailableError("timeseries"))
	}

	resp, err := tsService.WriteBatch(ctx, &batchReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleTSQuery(ctx context.Context, req *Request) *Response {
	if h.service == nil {
		return h.errorResponse(api.NewServiceUnavailableError("QuicBaaS"))
	}

	var queryReq service.TSQueryRequest
	if err := DecodePayload(req.Payload, &queryReq); err != nil {
		return h.errorResponse(err)
	}

	tsService, ok := h.service.(timeseriesServiceInterface)
	if !ok {
		return h.errorResponse(api.NewServiceUnavailableError("timeseries"))
	}

	resp, err := tsService.TSQuery(ctx, &queryReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleTSDeleteSeries(ctx context.Context, req *Request) *Response {
	if h.service == nil {
		return h.errorResponse(api.NewServiceUnavailableError("QuicBaaS"))
	}

	var deleteReq service.TSDeleteRequest
	if err := DecodePayload(req.Payload, &deleteReq); err != nil {
		return h.errorResponse(err)
	}

	tsService, ok := h.service.(timeseriesServiceInterface)
	if !ok {
		return h.errorResponse(api.NewServiceUnavailableError("timeseries"))
	}

	resp, err := tsService.DeleteSeries(ctx, &deleteReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleTSGetRetentionPolicy(ctx context.Context, req *Request) *Response {
	if h.service == nil {
		return h.errorResponse(api.NewServiceUnavailableError("QuicBaaS"))
	}

	var policyReq service.TSGetRetentionPolicyRequest
	if err := DecodePayload(req.Payload, &policyReq); err != nil {
		return h.errorResponse(err)
	}

	tsService, ok := h.service.(timeseriesServiceInterface)
	if !ok {
		return h.errorResponse(api.NewServiceUnavailableError("timeseries"))
	}

	resp, err := tsService.GetRetentionPolicy(ctx, &policyReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleTSSetRetentionPolicy(ctx context.Context, req *Request) *Response {
	if h.service == nil {
		return h.errorResponse(api.NewServiceUnavailableError("QuicBaaS"))
	}

	var policyReq service.TSSetRetentionPolicyRequest
	if err := DecodePayload(req.Payload, &policyReq); err != nil {
		return h.errorResponse(err)
	}

	tsService, ok := h.service.(timeseriesServiceInterface)
	if !ok {
		return h.errorResponse(api.NewServiceUnavailableError("timeseries"))
	}

	resp, err := tsService.SetRetentionPolicy(ctx, &policyReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleTSListMetrics(ctx context.Context, req *Request) *Response {
	if h.service == nil {
		return h.errorResponse(api.NewServiceUnavailableError("QuicBaaS"))
	}

	var listReq service.TSListMetricsRequest
	if err := DecodePayload(req.Payload, &listReq); err != nil {
		return h.errorResponse(err)
	}

	tsService, ok := h.service.(timeseriesServiceInterface)
	if !ok {
		return h.errorResponse(api.NewServiceUnavailableError("timeseries"))
	}

	resp, err := tsService.ListMetrics(ctx, &listReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// =============================================================================
// Operations Management Handlers
// =============================================================================

func (h *Handler) handleMigrateData(ctx context.Context, req *Request) *Response {
	if h.service == nil {
		return h.errorResponse(api.NewServiceUnavailableError("QuicBaaS"))
	}

	var migrateReq service.MigrateDataRequest
	if err := DecodePayload(req.Payload, &migrateReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.service.MigrateData(ctx, &migrateReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleGetMigrationProgress(ctx context.Context, req *Request) *Response {
	if h.service == nil {
		return h.errorResponse(api.NewServiceUnavailableError("QuicBaaS"))
	}

	var progressReq service.GetMigrationProgressRequest
	if err := DecodePayload(req.Payload, &progressReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.service.GetMigrationProgress(ctx, &progressReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleReplaceNode(ctx context.Context, req *Request) *Response {
	if h.service == nil {
		return h.errorResponse(api.NewServiceUnavailableError("QuicBaaS"))
	}

	var replaceReq service.ReplaceNodeRequest
	if err := DecodePayload(req.Payload, &replaceReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.service.ReplaceNode(ctx, &replaceReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleUpgradeNode(ctx context.Context, req *Request) *Response {
	if h.service == nil {
		return h.errorResponse(api.NewServiceUnavailableError("QuicBaaS"))
	}

	var upgradeReq service.UpgradeNodeRequest
	if err := DecodePayload(req.Payload, &upgradeReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.service.UpgradeNode(ctx, &upgradeReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleGetUpgradeStatus(ctx context.Context, req *Request) *Response {
	if h.service == nil {
		return h.errorResponse(api.NewServiceUnavailableError("QuicBaaS"))
	}

	var statusReq service.GetUpgradeStatusRequest
	if err := DecodePayload(req.Payload, &statusReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := h.service.GetUpgradeStatus(ctx, &statusReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// =============================================================================
// Helper Functions
// =============================================================================

// errorResponse creates an error response from an error.
func (h *Handler) errorResponse(err error) *Response {
	status := ErrorToStatus(err)
	return &Response{
		Status:  status,
		Payload: []byte(err.Error()),
	}
}
