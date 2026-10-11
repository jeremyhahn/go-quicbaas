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
	"sync/atomic"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// handlerFunc is the function signature for unary request handlers.
type handlerFunc func(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error)

// streamHandlerFunc is the function signature for streaming request handlers.
type streamHandlerFunc func(ctx context.Context, svc service.QuicBaaS, req interface{}, s *stream) error

// Transport implements the transport.Transport interface for embedded in-process access.
type Transport struct {
	service service.QuicBaaS
	closed  atomic.Bool
}

// unaryHandlers maps service/method to handler functions for O(1) dispatch.
var unaryHandlers = map[string]handlerFunc{
	// KV operations
	"quicbaas.KV/Put":             handlePut,
	"quicbaas.KV/Get":             handleGet,
	"quicbaas.KV/Delete":          handleDelete,
	"quicbaas.KV/List":            handleList,
	"quicbaas.KV/ListEntityTypes": handleListEntityTypes,

	// Batch operations
	"quicbaas.Batch/Put":    handleBatchPut,
	"quicbaas.Batch/Get":    handleBatchGet,
	"quicbaas.Batch/Delete": handleBatchDelete,

	// Transaction operations
	"quicbaas.Transaction/Execute": handleTransaction,

	// Cluster operations
	"quicbaas.Cluster/Status":             handleClusterStatus,
	"quicbaas.Cluster/GetLeader":          handleGetLeader,
	"quicbaas.Cluster/AddNode":            handleAddNode,
	"quicbaas.Cluster/RemoveNode":         handleRemoveNode,
	"quicbaas.Cluster/CreateShard":        handleCreateShard,
	"quicbaas.Cluster/Rebalance":          handleRebalanceCluster,
	"quicbaas.Cluster/GetRebalanceStatus": handleGetRebalanceProgress,

	// Backup operations
	"quicbaas.Backup/Create":  handleCreateBackup,
	"quicbaas.Backup/List":    handleListBackups,
	"quicbaas.Backup/Restore": handleRestoreBackup,
	"quicbaas.Backup/Delete":  handleDeleteBackup,
	"quicbaas.Backup/Verify":  handleVerifyBackup,

	// Auth operations
	"quicbaas.Auth/Login":         handleLogin,
	"quicbaas.Auth/Logout":        handleLogout,
	"quicbaas.Auth/RefreshToken":  handleRefreshToken,
	"quicbaas.Auth/ValidateToken": handleValidateToken,

	// Unified login ceremony (BeginLogin / FinishLogin) — routes through the
	// LoginCeremony() facade. Embedded path is in-process trusted.
	"quicbaas.Auth/BeginLogin":  handleBeginLogin,
	"quicbaas.Auth/FinishLogin": handleFinishLogin,

	// RBAC operations
	"quicbaas.RBAC/CreateRole": handleCreateAuthRole,
	"quicbaas.RBAC/DeleteRole": handleDeleteAuthRole,
	"quicbaas.RBAC/ListRoles":  handleListAuthRoles,
	"quicbaas.RBAC/AssignRole": handleAssignRole,
	"quicbaas.RBAC/RevokeRole": handleRevokeRole,

	// Cache operations
	"quicbaas.Cache/GetStats":     handleGetCacheStats,
	"quicbaas.Cache/Clear":        handleClearCache,
	"quicbaas.Cache/GetConfig":    handleGetCacheConfig,
	"quicbaas.Cache/UpdateConfig": handleUpdateCacheConfig,

	// Health operations
	"quicbaas.Health/Check":     handleHealth,
	"quicbaas.Health/Liveness":  handleLiveness,
	"quicbaas.Health/Readiness": handleReadiness,
	"quicbaas.Health/Ping":      handlePing,

	// Secrets operations
	"quicbaas.Secrets/Store":   handleSecretsStore,
	"quicbaas.Secrets/Get":     handleSecretsGet,
	"quicbaas.Secrets/GetData": handleSecretsGetData,
	"quicbaas.Secrets/List":    handleSecretsList,
	"quicbaas.Secrets/Delete":  handleSecretsDelete,
	"quicbaas.Secrets/Ensure":  handleSecretsEnsure,

	// TimeSeries operations
	"quicbaas.TimeSeries/Aggregate":          handleAggregate,
	"quicbaas.TimeSeries/WritePoint":         handleWritePoint,
	"quicbaas.TimeSeries/WriteBatch":         handleWriteBatch,
	"quicbaas.TimeSeries/Query":              handleTSQuery,
	"quicbaas.TimeSeries/DeleteSeries":       handleDeleteSeries,
	"quicbaas.TimeSeries/GetRetentionPolicy": handleGetRetentionPolicy,
	"quicbaas.TimeSeries/SetRetentionPolicy": handleSetRetentionPolicy,
	"quicbaas.TimeSeries/ListMetrics":        handleListMetrics,

	// Operations
	"quicbaas.Operations/MigrateData":          handleMigrateData,
	"quicbaas.Operations/GetMigrationProgress": handleGetMigrationProgress,
	"quicbaas.Operations/ReplaceNode":          handleReplaceNode,
	"quicbaas.Operations/UpgradeNode":          handleUpgradeNode,
	"quicbaas.Operations/GetUpgradeStatus":     handleGetUpgradeStatus,

	// KMS operations
	"quicbaas.KMS/GenerateKey":             handleKMSGenerateKey,
	"quicbaas.KMS/ImportKey":               handleKMSImportKey,
	"quicbaas.KMS/GetKey":                  handleKMSGetKey,
	"quicbaas.KMS/GetPublicKey":            handleKMSGetPublicKey,
	"quicbaas.KMS/ListKeys":                handleKMSListKeys,
	"quicbaas.KMS/RotateKey":               handleKMSRotateKey,
	"quicbaas.KMS/RevokeKey":               handleKMSRevokeKey,
	"quicbaas.KMS/DeleteKey":               handleKMSDeleteKey,
	"quicbaas.KMS/GetKeyMetadata":          handleKMSGetKeyMetadata,
	"quicbaas.KMS/UpdateKeyMetadata":       handleKMSUpdateKeyMetadata,
	"quicbaas.KMS/UpdateKeyStatus":         handleKMSUpdateKeyStatus,
	"quicbaas.KMS/ListKeyVersions":         handleKMSListKeyVersions,
	"quicbaas.KMS/SetKeyPolicy":            handleKMSSetKeyPolicy,
	"quicbaas.KMS/GetKeyPolicy":            handleKMSGetKeyPolicy,
	"quicbaas.KMS/SetKeyRotationPolicy":    handleKMSSetKeyRotationPolicy,
	"quicbaas.KMS/GetKeyRotationPolicy":    handleKMSGetKeyRotationPolicy,
	"quicbaas.KMS/DeleteKeyRotationPolicy": handleKMSDeleteKeyRotationPolicy,
	"quicbaas.KMS/GetSupportedAlgorithms":  handleKMSGetSupportedAlgorithms,
	"quicbaas.KMS/BackendStatus":           handleKMSBackendStatus,
	"quicbaas.KMS/BackendTest":             handleKMSBackendTest,
	"quicbaas.KMS/ExportKey":               handleKMSExportKey,
	"quicbaas.KMS/UnwrapKey":               handleKMSUnwrapKey,
	"quicbaas.KMS/SetKeySafety":            handleKMSSetKeySafety,
	"quicbaas.KMS/ClearKeySafety":          handleKMSClearKeySafety,
	"quicbaas.KMS/SafetyCheck":             handleKMSSafetyCheck,
	"quicbaas.KMS/SafetyReport":            handleKMSSafetyReport,
	"quicbaas.KMS/CreateDEK":               handleKMSCreateDEK,
	"quicbaas.KMS/GetDEK":                  handleKMSGetDEK,
	"quicbaas.KMS/ListDEKs":                handleKMSListDEKs,
	"quicbaas.KMS/RotateDEK":               handleKMSRotateDEK,
	"quicbaas.KMS/DeleteDEK":               handleKMSDeleteDEK,
	"quicbaas.KMS/GetDEKStatus":            handleKMSGetDEKStatus,
	"quicbaas.KMS/SetDefaultDEK":           handleKMSSetDefaultDEK,
	"quicbaas.KMS/SetDEKSafety":            handleKMSSetDEKSafety,
	"quicbaas.KMS/ClearDEKSafety":          handleKMSClearDEKSafety,
	"quicbaas.KMS/StartReencryption":       handleKMSStartReencryption,
	"quicbaas.KMS/GetReencryptionProgress": handleKMSGetReencryptionProgress,

	// DEK operations — the dek/Client SDK package addresses operations
	// at quicbaas.DEK/* whereas the historical KMS service hosted them
	// under quicbaas.KMS/*. Dispatch both to the same handlers so
	// SDK clients constructed via dek.NewClient / kms.NewClient both
	// resolve correctly through embedded transport.
	"quicbaas.DEK/CreateDEK":               handleKMSCreateDEK,
	"quicbaas.DEK/GetDEK":                  handleKMSGetDEK,
	"quicbaas.DEK/ListDEKs":                handleKMSListDEKs,
	"quicbaas.DEK/RotateDEK":               handleKMSRotateDEK,
	"quicbaas.DEK/DeleteDEK":               handleKMSDeleteDEK,
	"quicbaas.DEK/GetDEKStatus":            handleKMSGetDEKStatus,
	"quicbaas.DEK/SetDefaultDEK":           handleKMSSetDefaultDEK,
	"quicbaas.DEK/SetDEKSafety":            handleKMSSetDEKSafety,
	"quicbaas.DEK/ClearDEKSafety":          handleKMSClearDEKSafety,
	"quicbaas.DEK/StartReencryption":       handleKMSStartReencryption,
	"quicbaas.DEK/GetReencryptionProgress": handleKMSGetReencryptionProgress,

	// Crypto operations
	"quicbaas.Crypto/Encrypt":                handleCryptoEncrypt,
	"quicbaas.Crypto/Decrypt":                handleCryptoDecrypt,
	"quicbaas.Crypto/Rewrap":                 handleCryptoRewrap,
	"quicbaas.Crypto/EncryptBatch":           handleCryptoEncryptBatch,
	"quicbaas.Crypto/DecryptBatch":           handleCryptoDecryptBatch,
	"quicbaas.Crypto/GenerateDataKey":        handleCryptoGenerateDataKey,
	"quicbaas.Crypto/GenerateWrappedDataKey": handleCryptoGenerateWrappedDataKey,
	"quicbaas.Crypto/Sign":                   handleCryptoSign,
	"quicbaas.Crypto/Verify":                 handleCryptoVerify,
	"quicbaas.Crypto/GenerateHMAC":           handleCryptoGenerateHMAC,
	"quicbaas.Crypto/VerifyHMAC":             handleCryptoVerifyHMAC,
	"quicbaas.Crypto/GenerateRandom":         handleCryptoGenerateRandom,
	"quicbaas.Crypto/Hash":                   handleCryptoHash,

	// Certificate operations
	"quicbaas.Certificate/IssueCertificate":  handleCertIssueCertificate,
	"quicbaas.Certificate/SignCSR":           handleCertSignCSR,
	"quicbaas.Certificate/RevokeCertificate": handleCertRevokeCertificate,
	"quicbaas.Certificate/VerifyCertificate": handleCertVerifyCertificate,
	"quicbaas.Certificate/GetCACertificate":  handleCertGetCACertificate,
	"quicbaas.Certificate/GetCABundle":       handleCertGetCABundle,
	"quicbaas.Certificate/GetTLSConfig":      handleCertGetTLSConfig,
	"quicbaas.Certificate/IsInitialized":     handleCertIsInitialized,
	"quicbaas.Certificate/GenerateCRL":       handleCertGenerateCRL,
	"quicbaas.Certificate/IsRevoked":         handleCertIsRevoked,
	"quicbaas.Certificate/CAInfo":            handleCertCAInfo,
	"quicbaas.Certificate/ListCertificates":  handleCertListCertificates,

	// Certificate extensions — Security().CertificateExt(). Cross-signing,
	// AIA chain resolution and TCG device identity issuance, all composed
	// over the xkms TrustedCA / TCGCA (see
	// pkg/server/ca_service_adapter_extensions.go).
	"quicbaas.Certificate/CrossSign":           handleCertCrossSign,
	"quicbaas.Certificate/Chain":               handleCertChain,
	"quicbaas.Certificate/IssueDeviceIdentity": handleCertIssueDeviceIdentity,

	// Trust operations (cross-sign / AIA composition over xkms TrustedCA).
	// SSH operations — composed from the go-xkms SSH SDK via the
	// service-layer SSHService.
	"quicbaas.SSH/GenerateKey":         handleSSHGenerateKey,
	"quicbaas.SSH/ImportKey":           handleSSHImportKey,
	"quicbaas.SSH/GetKey":              handleSSHGetKey,
	"quicbaas.SSH/ListKeys":            handleSSHListKeys,
	"quicbaas.SSH/DeleteKey":           handleSSHDeleteKey,
	"quicbaas.SSH/ExportPublicKey":     handleSSHExportPublicKey,
	"quicbaas.SSH/SignDigest":          handleSSHSignDigest,
	"quicbaas.SSH/AddKnownHost":        handleSSHAddKnownHost,
	"quicbaas.SSH/ListKnownHosts":      handleSSHListKnownHosts,
	"quicbaas.SSH/RemoveKnownHost":     handleSSHRemoveKnownHost,
	"quicbaas.SSH/AddAuthorizedKey":    handleSSHAddAuthorizedKey,
	"quicbaas.SSH/ListAuthorizedKeys":  handleSSHListAuthorizedKeys,
	"quicbaas.SSH/RemoveAuthorizedKey": handleSSHRemoveAuthorizedKey,
	"quicbaas.SSH/EmitAuthorizedKey":   handleSSHEmitAuthorizedKey,

	// Attestation: the go-xkms verifier plus the container attestation
	// orchestration seam (Security().Attestation()).
	"quicbaas.Attestation/Verify":          handleAttestationVerify,
	"quicbaas.Attestation/VerifyChain":     handleAttestationVerifyChain,
	"quicbaas.Attestation/AttestContainer": handleAttestationAttestContainer,

	// The attestation Verifier (handlers_attestation_verifier.go).
	attestationService + "/PutEndorsement":        handleAttestationPutEndorsement,
	attestationService + "/ListEndorsements":      handleAttestationListEndorsements,
	attestationService + "/DeleteEndorsement":     handleAttestationDeleteEndorsement,
	attestationService + "/PutReferenceValues":    handleAttestationPutReferenceValues,
	attestationService + "/GetReferenceValues":    handleAttestationGetReferenceValues,
	attestationService + "/ListReferenceValues":   handleAttestationListReferenceValues,
	attestationService + "/DeleteReferenceValues": handleAttestationDeleteReferenceValues,
	attestationService + "/AppraiseQuote":         handleAttestationAppraiseQuote,

	// Dynamic secrets: lease-backed credential generation
	// (Security().DynamicSecrets()).
	"quicbaas.DynamicSecrets/CreateRole":         handleDynamicSecretsCreateRole,
	"quicbaas.DynamicSecrets/GetRole":            handleDynamicSecretsGetRole,
	"quicbaas.DynamicSecrets/ListRoles":          handleDynamicSecretsListRoles,
	"quicbaas.DynamicSecrets/DeleteRole":         handleDynamicSecretsDeleteRole,
	"quicbaas.DynamicSecrets/GenerateCredential": handleDynamicSecretsGenerateCredential,
	"quicbaas.DynamicSecrets/RenewLease":         handleDynamicSecretsRenewLease,
	"quicbaas.DynamicSecrets/RevokeLease":        handleDynamicSecretsRevokeLease,
	"quicbaas.DynamicSecrets/ListLeases":         handleDynamicSecretsListLeases,

	"quicbaas.Trust/CrossSignedRootPool": handleTrustCrossSignedRootPool,
	"quicbaas.Trust/AutoImportAIA":       handleTrustAutoImportAIA,
	"quicbaas.Trust/PublishDANE":         handleTrustPublishDANE,

	// PIV: the backend-scoped go-xkms PIV surface
	// (Security().Auth().PIV()), xkms wire types verbatim.
	"quicbaas.PIV/ListPIVSlots":         handlePIVListSlots,
	"quicbaas.PIV/GetPIVCertificate":    handlePIVGetCertificate,
	"quicbaas.PIV/StorePIVCertificate":  handlePIVStoreCertificate,
	"quicbaas.PIV/DeletePIVCertificate": handlePIVDeleteCertificate,
	"quicbaas.PIV/GeneratePIVKey":       handlePIVGenerateKey,
	"quicbaas.PIV/ImportPIVCertificate": handlePIVImportCertificate,
	"quicbaas.PIV/ExportPIVCertificate": handlePIVExportCertificate,
	"quicbaas.PIV/GeneratePIVCSR":       handlePIVGenerateCSR,

	// FIDO2: the tenant-scoped WebAuthn ceremony surface
	// (Security().Auth().FIDO2()), xkms wire types verbatim; the tenant
	// rides the context (transport.ContextWithTenant).
	"quicbaas.FIDO2/BeginRegistration":    handleFIDO2BeginRegistration,
	"quicbaas.FIDO2/FinishRegistration":   handleFIDO2FinishRegistration,
	"quicbaas.FIDO2/BeginAuthentication":  handleFIDO2BeginAuthentication,
	"quicbaas.FIDO2/FinishAuthentication": handleFIDO2FinishAuthentication,

	// ACME server-side control operations (RFC 8555 composition over the
	// go-xkms ACME server). Only the control verbs round-trip in-process;
	// the protocol-client verbs (NewOrder/FinalizeOrder/Renew) are JWS-signed
	// RFC 8555 exchanges, not transport RPCs, by design.
	"quicbaas.ACME/RegisterChallenge": handleACMERegisterChallenge,
	"quicbaas.ACME/Issue":             handleACMEIssue,
	"quicbaas.ACME/NewOrder":          handleACMENewOrder,
	"quicbaas.ACME/FinalizeOrder":     handleACMEFinalizeOrder,
	"quicbaas.ACME/Renew":             handleACMERenew,

	// Pooled provider accounts (agent credential pool)
	"quicbaas.Account/Create":           handleAccountCreate,
	"quicbaas.Account/Get":              handleAccountGet,
	"quicbaas.Account/List":             handleAccountList,
	"quicbaas.Account/Update":           handleAccountUpdate,
	"quicbaas.Account/Delete":           handleAccountDelete,
	"quicbaas.Account/RotateCredential": handleAccountRotateCredential,
	"quicbaas.Account/Revoke":           handleAccountRevoke,
	"quicbaas.Account/SetDefault":       handleAccountSetDefault,
	"quicbaas.Account/Usage":            handleAccountUsage,
	"quicbaas.Account/Materialize":      handleAccountMaterialize,
	"quicbaas.Account/ReserveSession":   handleAccountReserveSession,
	"quicbaas.Account/ReleaseSession":   handleAccountReleaseSession,

	// Orchestrator — session operations
	"quicbaas.Orchestrator/Inference":              handleOrchestratorInference,
	"quicbaas.Orchestrator/SpawnSession":           handleOrchestratorSpawnSession,
	"quicbaas.Orchestrator/PromptSession":          handleOrchestratorPromptSession,
	"quicbaas.Orchestrator/SessionStatus":          handleOrchestratorSessionStatus,
	"quicbaas.Orchestrator/CloseSession":           handleOrchestratorCloseSession,
	"quicbaas.Orchestrator/RotateSessionAccount":   handleOrchestratorRotateSessionAccount,
	"quicbaas.Orchestrator/ListSessions":           handleOrchestratorListSessions,
	"quicbaas.Orchestrator/GetSessionConversation": handleOrchestratorGetSessionConversation,
	"quicbaas.Orchestrator/GetSpendReport":         handleOrchestratorGetSpendReport,
	"quicbaas.Orchestrator/GetTenantSpendPolicy":   handleOrchestratorGetTenantSpendPolicy,
	"quicbaas.Orchestrator/SetTenantSpendPolicy":   handleOrchestratorSetTenantSpendPolicy,

	// Orchestrator — task operations
	"quicbaas.Orchestrator/CreateTask":   handleOrchestratorCreateTask,
	"quicbaas.Orchestrator/GetTask":      handleOrchestratorGetTask,
	"quicbaas.Orchestrator/ListTasks":    handleOrchestratorListTasks,
	"quicbaas.Orchestrator/ClaimTask":    handleOrchestratorClaimTask,
	"quicbaas.Orchestrator/CompleteTask": handleOrchestratorCompleteTask,
	"quicbaas.Orchestrator/BlockTask":    handleOrchestratorBlockTask,
	"quicbaas.Orchestrator/ReadmitTask":  handleOrchestratorReadmitTask,
	"quicbaas.Orchestrator/DeleteTask":   handleOrchestratorDeleteTask,

	// Orchestrator — the attention inbox
	"quicbaas.Orchestrator/AttentionList":    handleOrchestratorAttentionList,
	"quicbaas.Orchestrator/AttentionResolve": handleOrchestratorAttentionResolve,

	// Orchestrator — task artifact operations
	"quicbaas.Orchestrator/SubmitTaskArtifacts": handleOrchestratorSubmitTaskArtifacts,
	"quicbaas.Orchestrator/GetTaskArtifact":     handleOrchestratorGetTaskArtifact,
	"quicbaas.Orchestrator/ListTaskArtifacts":   handleOrchestratorListTaskArtifacts,

	// Orchestrator — agent registry operations
	"quicbaas.Orchestrator/RegisterFleetNode":  handleFleetRegister,
	"quicbaas.Orchestrator/GetFleetNode":       handleFleetGet,
	"quicbaas.Orchestrator/ListFleetNodes":     handleFleetList,
	"quicbaas.Orchestrator/RemoveFleetNode":    handleFleetRemove,
	"quicbaas.Orchestrator/FleetNodeHeartbeat": handleFleetHeartbeat,

	// Orchestrator — agent admin operations
	"quicbaas.Orchestrator/DisableFleetNode": handleFleetDisable,
	"quicbaas.Orchestrator/EnableFleetNode":  handleFleetEnable,

	// Orchestrator — job operations
	"quicbaas.Orchestrator/StartJob":      handleOrchestratorStartJob,
	"quicbaas.Orchestrator/GetJobStatus":  handleOrchestratorGetJobStatus,
	"quicbaas.Orchestrator/ListJobs":      handleOrchestratorListJobs,
	"quicbaas.Orchestrator/CancelJob":     handleOrchestratorCancelJob,
	"quicbaas.Orchestrator/SpawnChildJob": handleOrchestratorSpawnChildJob,

	// Orchestrator — utility operations
	"quicbaas.Orchestrator/Health":            handleOrchestratorHealth,
	"quicbaas.Orchestrator/ListBackends":      handleOrchestratorListBackends,
	"quicbaas.Orchestrator/ListRunners":       handleOrchestratorListRunners,
	"quicbaas.Orchestrator/ListWorkflows":     handleOrchestratorListWorkflows,
	"quicbaas.Orchestrator/ListBackendModels": handleOrchestratorListBackendModels,
	"quicbaas.Orchestrator/DecomposeTask":     handleOrchestratorDecomposeTask,
	"quicbaas.Orchestrator/GetCostSettings":   handleOrchestratorGetCostSettings,
	"quicbaas.Orchestrator/SetCostSettings":   handleOrchestratorSetCostSettings,

	// Project operations
	"quicbaas.Project/Create":              handleProjectCreate,
	"quicbaas.Project/Get":                 handleProjectGet,
	"quicbaas.Project/GetByName":           handleProjectGetByName,
	"quicbaas.Project/Update":              handleProjectUpdate,
	"quicbaas.Project/Delete":              handleProjectDelete,
	"quicbaas.Project/List":                handleProjectList,
	"quicbaas.Project/GrantFleetNodeRole":  handleProjectGrantFleetNodeRole,
	"quicbaas.Project/RevokeFleetNodeRole": handleProjectRevokeFleetNodeRole,

	// Agents: the tenant AI agent catalog and its failover pools
	agentsService + "/ListAgents":  handleAgentsList,
	agentsService + "/GetAgent":    handleAgentsGet,
	agentsService + "/CreateAgent": handleAgentsCreate,
	agentsService + "/UpdateAgent": handleAgentsUpdate,
	agentsService + "/DeleteAgent": handleAgentsDelete,
	agentsService + "/AgentUsedBy": handleAgentsUsedBy,
	agentsService + "/ListPools":   handleAgentPoolsList,
	agentsService + "/GetPool":     handleAgentPoolsGet,
	agentsService + "/CreatePool":  handleAgentPoolsCreate,
	agentsService + "/UpdatePool":  handleAgentPoolsUpdate,
	agentsService + "/DeletePool":  handleAgentPoolsDelete,
	agentsService + "/PoolUsedBy":  handleAgentPoolsUsedBy,

	// SDLC module: forge bindings and deliveries
	"quicbaas.SDLC/Bind":        handleSDLCBind,
	"quicbaas.SDLC/Unbind":      handleSDLCUnbind,
	"quicbaas.SDLC/Binding":     handleSDLCBinding,
	"quicbaas.SDLC/Delivery":    handleSDLCDelivery,
	"quicbaas.SDLC/ListForges":  handleSDLCForges,
	"quicbaas.SDLC/Onboard":     handleSDLCOnboard,
	"quicbaas.SDLC/Conformance": handleSDLCConformance,
	"quicbaas.SDLC/OpenWork":    handleSDLCOpenWork,

	"quicbaas.SDLC/Reviewer":           handleSDLCReviewer,
	"quicbaas.SDLC/SetReviewer":        handleSDLCSetReviewer,
	"quicbaas.SDLC/AdoptIssue":         handleSDLCAdoptIssue,
	"quicbaas.SDLC/ClearReviewer":      handleSDLCClearReviewer,
	"quicbaas.SDLC/WorkerImageStatus":  handleSDLCWorkerImageStatus,
	"quicbaas.SDLC/RefreshWorkerImage": handleSDLCRefreshWorkerImage,
	"quicbaas.SDLC/FileIssue":          handleSDLCFileIssue,
	"quicbaas.SDLC/Collaborators":      handleSDLCCollaborators,

	// Service registry
	registryService + "/Register":   handleRegistryRegister,
	registryService + "/Renew":      handleRegistryRenew,
	registryService + "/Deregister": handleRegistryDeregister,
	registryService + "/Resolve":    handleRegistryResolve,

	// ObjectStorage operations
	"quicbaas.ObjectStorage/PutObject":          handleObjPutObject,
	"quicbaas.ObjectStorage/GetObject":          handleObjGetObject,
	"quicbaas.ObjectStorage/DeleteObject":       handleObjDeleteObject,
	"quicbaas.ObjectStorage/HeadObject":         handleObjHeadObject,
	"quicbaas.ObjectStorage/ListObjects":        handleObjListObjects,
	"quicbaas.ObjectStorage/ListByTag":          handleObjListByTag,
	"quicbaas.ObjectStorage/ListByTenant":       handleObjListByTenant,
	"quicbaas.ObjectStorage/CopyObject":         handleObjCopyObject,
	"quicbaas.ObjectStorage/ReplicateObject":    handleObjReplicateObject,
	"quicbaas.ObjectStorage/MigrateObject":      handleObjMigrateObject,
	"quicbaas.ObjectStorage/ArchiveObject":      handleObjArchiveObject,
	"quicbaas.ObjectStorage/RestoreObject":      handleObjRestoreObject,
	"quicbaas.ObjectStorage/VerifyObject":       handleObjVerifyObject,
	"quicbaas.ObjectStorage/SetObjectPolicy":    handleObjSetObjectPolicy,
	"quicbaas.ObjectStorage/GetObjectPolicy":    handleObjGetObjectPolicy,
	"quicbaas.ObjectStorage/ListBackends":       handleObjListBackends,
	"quicbaas.ObjectStorage/GetBackendHealth":   handleObjGetBackendHealth,
	"quicbaas.ObjectStorage/GetTenantUsage":     handleObjGetTenantUsage,
	"quicbaas.ObjectStorage/GetTenantQuotas":    handleObjGetTenantQuotas,
	"quicbaas.ObjectStorage/SetTenantQuotas":    handleObjSetTenantQuotas,
	"quicbaas.ObjectStorage/BatchPutObjects":    handleObjBatchPutObjects,
	"quicbaas.ObjectStorage/BatchGetObjects":    handleObjBatchGetObjects,
	"quicbaas.ObjectStorage/BatchDeleteObjects": handleObjBatchDeleteObjects,
}

// streamHandlers maps service/method to stream handler functions for O(1) dispatch.
var streamHandlers = map[string]streamHandlerFunc{
	"quicbaas.KV/List":                         handleListStream,
	"quicbaas.KV/Watch":                        handleWatchStream,
	"quicbaas.Query/Query":                     handleQueryStream,
	"quicbaas.Query/ScanIndex":                 handleScanIndexStream,
	"quicbaas.TimeSeries/Query":                handleTimeSeriesQueryStream,
	"quicbaas.Orchestrator/SubscribeJobEvents": handleSubscribeJobEventsStream,
	"quicbaas.Orchestrator/InferenceStream":    handleOrchestratorInferenceStream,
	registryService + "/Watch":                 handleRegistryWatchStream,
}

// New creates a new embedded transport with the given service.
func New(svc service.QuicBaaS) (*Transport, error) {
	if svc == nil {
		return nil, ErrNilService
	}
	return &Transport{
		service: svc,
	}, nil
}

// Request sends a unary request and returns the response.
func (t *Transport) Request(ctx context.Context, svcName, method string, req interface{}) (interface{}, error) {
	if t.closed.Load() {
		return nil, ErrTransportClosed
	}

	key := svcName + "/" + method
	handler, ok := unaryHandlers[key]
	if !ok {
		return nil, NewUnknownMethodError(svcName, method)
	}

	return handler(inProcessTenant(ctx), t.service, req)
}

// inProcessTenant binds the tenant an embedded call acts for, under go-qrdb's
// central key (pkg/tenantctx), which every service reads.
//
// The embedded transport is the in-process path: there is no wire and no
// authenticator, and the caller is the program that composed the service
// facade. It acts for the tenant it named with transport.ContextWithTenant,
// for a tenant it already bound, and otherwise for the platform itself — the
// reach it has always had, now stated as an explicit system binding because
// the services no longer read an absent tenant as system (go-quicbaas#1150).
func inProcessTenant(ctx context.Context) context.Context {
	if _, ok := tenantctx.Lookup(ctx); ok {
		return ctx
	}
	if tenantID := transport.TenantFromContext(ctx); tenantID != "" {
		return tenantctx.WithTenant(ctx, tenantID)
	}
	return tenantctx.WithSystem(ctx)
}

// RequestStream initiates a streaming request.
func (t *Transport) RequestStream(ctx context.Context, svcName, method string, req interface{}) (transport.Stream, error) {
	if t.closed.Load() {
		return nil, ErrTransportClosed
	}

	key := svcName + "/" + method
	handler, ok := streamHandlers[key]
	if !ok {
		return nil, NewStreamNotSupportedError(svcName, method)
	}

	ctx = inProcessTenant(ctx)
	s := newStream(ctx, 100)

	// Start the stream handler in a goroutine
	go func() {
		err := handler(ctx, t.service, req, s)
		if err != nil {
			// Try to send error through the stream if it's still open
			// Check closed state before attempting to send to avoid panic
			if !s.isClosed() {
				select {
				case s.recvCh <- err:
				case <-s.ctx.Done():
				default:
					// Channel full or closed, error is lost but that's acceptable
				}
			}
		}
		// Close the stream after the handler completes
		s.Close()
	}()

	return s, nil
}

// Conn returns the underlying connection (the service instance for embedded transport).
func (t *Transport) Conn() interface{} {
	return t.service
}

// Close closes the transport.
func (t *Transport) Close() error {
	t.closed.Store(true)
	return nil
}

// Healthy returns true if the transport is healthy.
func (t *Transport) Healthy() bool {
	return !t.closed.Load() && t.service != nil
}

// Service returns the underlying QuicBaaS service for direct access.
func (t *Transport) Service() service.QuicBaaS {
	return t.service
}

// Factory implements transport.Factory for embedded transport.
type Factory struct {
	service service.QuicBaaS
}

// NewFactory creates a new embedded transport factory.
func NewFactory(svc service.QuicBaaS) *Factory {
	return &Factory{service: svc}
}

// Create creates a new embedded transport.
func (f *Factory) Create(cfg *transport.Config) (transport.Transport, error) {
	return New(f.service)
}
