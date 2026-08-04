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

// Package quic provides a high-performance QUIC transport layer for QuicBaaS.
// It implements a binary protocol over QUIC streams with MessagePack encoding,
// offering low-latency, multiplexed communication particularly suited for:
// - Mobile clients (connection migration)
// - High-latency networks (0-RTT connection establishment)
// - Concurrent operations (stream multiplexing without head-of-line blocking)
package quic

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strings"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// Protocol Constants.
const (
	// Protocol version.
	ProtocolVersion byte = 0x01

	// Status codes.
	StatusOK                byte = 0x00
	StatusInvalidRequest    byte = 0x01
	StatusNotFound          byte = 0x02
	StatusUnauthorized      byte = 0x03
	StatusInternalError     byte = 0x04
	StatusTimeout           byte = 0x05
	StatusAlreadyExists     byte = 0x06
	StatusPermissionDenied  byte = 0x07
	StatusResourceExhausted byte = 0x08
	StatusInvalidArgument   byte = 0x09
	StatusUnavailable       byte = 0x0A

	// Maximum payload size (16MB).
	MaxPayloadSize uint32 = 16 * 1024 * 1024

	// MaxTokenSize is the maximum auth token size (64KB - 1).
	//
	// Production tokens are RFC 9068 at+jwt access tokens whose scp claim
	// carries the subject's qrdb-resolved effective RBAC permissions and
	// whose roles claim carries the subject's role bindings; both scale
	// with the number of grants, and real tokens have been observed at
	// 11,060 bytes (well past the previous 8KB cap). The wire format
	// encodes the token length as a 2-byte big-endian prefix, so 65535
	// (64KB - 1) is the natural ceiling of the existing frame layout:
	// raising the constant to the full range of that field gives ~6x
	// headroom over the observed worst case with no frame-layout change,
	// while still tightly bounding the per-frame read allocation.
	MaxTokenSize uint16 = 64*1024 - 1
)

// Operation codes - mapping all QuicBaaS operations.
const (
	// KV Operations (0x01-0x04).
	OpPut    byte = 0x01
	OpGet    byte = 0x02
	OpDelete byte = 0x03
	OpList   byte = 0x04

	// Additional Secrets Operations (0x05-0x06).
	OpSecretsGetByPrefix byte = 0x05
	OpSecretsWatch       byte = 0x06

	// DEK Re-encryption Operations (0x07-0x08).
	OpKMSDEKStartReencryption    byte = 0x07
	OpKMSDEKGetReencryptProgress byte = 0x08

	// Extended KMS Operations (0x09-0x0F).
	OpKMSExportKey         byte = 0x09
	OpKMSUnwrapKey         byte = 0x0A
	OpKMSGetKeyMetadata    byte = 0x0B
	OpKMSUpdateKeyMetadata byte = 0x0C
	OpKMSSetKeyPolicy      byte = 0x0D
	OpKMSGetKeyPolicy      byte = 0x0E
	OpKMSSetKeySafety      byte = 0x0F

	// Batch Operations (0x10-0x12).
	OpBatchPut    byte = 0x10
	OpBatchGet    byte = 0x11
	OpBatchDelete byte = 0x12

	// Extended KMS Safety Operations (0x13-0x15).
	OpKMSClearKeySafety byte = 0x13
	OpKMSSafetyCheck    byte = 0x14
	OpKMSSafetyReport   byte = 0x15

	// Vector Operations (0x16-0x27).
	OpVectorCreateNamespace       byte = 0x16
	OpVectorDeleteNamespace       byte = 0x17
	OpVectorListNamespaces        byte = 0x18
	OpVectorGetNamespaceInfo      byte = 0x19
	OpVectorUpsert                byte = 0x1A
	OpVectorGet                   byte = 0x1B
	OpVectorDelete                byte = 0x1C
	OpVectorBatchUpsert           byte = 0x1D
	OpVectorSearchSimilar         byte = 0x1E
	OpVectorSearchByMetadata      byte = 0x1F
	OpVectorHybridSearch          byte = 0x21
	OpVectorSearchByID            byte = 0x22
	OpVectorRebuildIndex          byte = 0x23
	OpVectorGetRebuildProgress    byte = 0x24
	OpVectorGetIndexStats         byte = 0x25
	OpVectorCompact               byte = 0x26
	OpVectorGetCompactionProgress byte = 0x27

	// Transaction Operations (0x20).
	OpTransaction byte = 0x20

	// ObjectStorage Operations (0x28-0x3B).
	OpOSPutObject            byte = 0x28
	OpOSGetObject            byte = 0x29
	OpOSDeleteObject         byte = 0x2A
	OpOSHeadObject           byte = 0x2B
	OpOSListObjects          byte = 0x2C
	OpOSListByTag            byte = 0x2D
	OpOSListByTenant         byte = 0x2E
	OpOSCopyObject           byte = 0x2F
	OpOSReplicateObject      byte = 0x32
	OpOSMigrateObject        byte = 0x33
	OpOSArchiveObject        byte = 0x34
	OpOSRestoreObject        byte = 0x35
	OpOSVerifyObject         byte = 0x36
	OpOSGetObjectMetadata    byte = 0x37
	OpOSUpdateObjectMetadata byte = 0x38
	OpOSGetStorageStats      byte = 0x39
	OpOSSetLifecyclePolicy   byte = 0x3A
	OpOSGetLifecyclePolicy   byte = 0x3B

	// Query Operations (0x30-0x31).
	OpQuery     byte = 0x30
	OpScanIndex byte = 0x31

	// Ollama Operations (0x3C-0x3F, 0x4B-0x4C).
	OpOllamaEmbed          byte = 0x3C
	OpOllamaEmbedWithModel byte = 0x3D
	OpOllamaEmbedBatch     byte = 0x3E
	OpOllamaListModels     byte = 0x3F
	OpOllamaGetModelInfo   byte = 0x4B
	OpOllamaHealth         byte = 0x4C

	// Cluster Operations (0x40-0x4A).
	OpClusterStatus      byte = 0x40
	OpGetLeader          byte = 0x41
	OpAddNode            byte = 0x42
	OpRemoveNode         byte = 0x43
	OpCreateShard        byte = 0x44
	OpRebalanceCluster   byte = 0x45
	OpListNodes          byte = 0x46
	OpRebalanceStatus    byte = 0x47
	OpRebalancePlan      byte = 0x48
	OpCancelMigration    byte = 0x49
	OpNodeReplaceRequest byte = 0x4A

	// Embedding Operations (0x4D-0x4F, 0x55-0x58).
	OpEmbeddingGenerate      byte = 0x4D
	OpEmbeddingBatchGenerate byte = 0x4E
	OpEmbeddingListProviders byte = 0x4F
	OpEmbeddingGetProvider   byte = 0x55
	OpEmbeddingSetDefault    byte = 0x56
	OpEmbeddingGetCacheStats byte = 0x57
	OpEmbeddingClearCache    byte = 0x58

	// Backup Operations (0x50-0x54).
	OpCreateBackup  byte = 0x50
	OpListBackups   byte = 0x51
	OpRestoreBackup byte = 0x52
	OpDeleteBackup  byte = 0x53
	OpVerifyBackup  byte = 0x54

	// RAG Operations (0x59-0x5A).
	OpRAGQuery        byte = 0x59
	OpRAGBuildContext byte = 0x5A

	// PEP Operations (0x5B-0x5D) - Policy Enforcement Point.
	OpPEPCheck      byte = 0x5B
	OpPEPCheckBatch byte = 0x5C
	OpPEPSubscribe  byte = 0x5D

	// ObjectStorage Extended Operations (0x5E-0x5F, 0x93-0x94, 0xFA-0xFC).
	OpOSGetObjectPolicy    byte = 0x5E
	OpOSListBackends       byte = 0x5F
	OpOSGetBackendHealth   byte = 0x93
	OpOSSetTenantQuotas    byte = 0x94
	OpOSBatchPutObjects    byte = 0xFA
	OpOSBatchGetObjects    byte = 0xFB
	OpOSBatchDeleteObjects byte = 0xFC

	// Auth Operations (0x60-0x63). 0x60 (former password login) is reserved.
	OpLogout        byte = 0x61
	OpRefreshToken  byte = 0x62
	OpValidateToken byte = 0x63

	// Unified login ceremony (0x78-0x79). Free on both SDK and server.
	OpAuthBeginLogin  byte = 0x78
	OpAuthFinishLogin byte = 0x79

	// Dynamic Secrets Operations (0x64-0x6B).
	OpDynamicSecretsCreateRole  byte = 0x64
	OpDynamicSecretsGetRole     byte = 0x65
	OpDynamicSecretsListRoles   byte = 0x66
	OpDynamicSecretsDeleteRole  byte = 0x67
	OpDynamicSecretsGenerate    byte = 0x68
	OpDynamicSecretsRenewLease  byte = 0x69
	OpDynamicSecretsRevokeLease byte = 0x6A
	OpDynamicSecretsListLeases  byte = 0x6B

	// RBAC Operations (0x70-0x77).
	OpCreateUser byte = 0x70
	OpDeleteUser byte = 0x71
	OpListUsers  byte = 0x72
	OpCreateRole byte = 0x73
	OpDeleteRole byte = 0x74
	OpListRoles  byte = 0x75
	OpAssignRole byte = 0x76
	OpRevokeRole byte = 0x77

	// Parameter Store Operations (0x7D-0x7F, 0x8A-0x8E).
	OpParameterGet         byte = 0x7D
	OpParameterSet         byte = 0x7E
	OpParameterDelete      byte = 0x7F
	OpParameterList        byte = 0x8A
	OpParameterBatchGet    byte = 0x8B
	OpParameterBatchSet    byte = 0x8C
	OpParameterBatchDelete byte = 0x8D
	OpParameterSearch      byte = 0x8E

	// Cache Operations (0x80-0x83).
	OpGetCacheStats     byte = 0x80
	OpClearCache        byte = 0x81
	OpGetCacheConfig    byte = 0x82
	OpUpdateCacheConfig byte = 0x83

	// Certificate Operations (0x84-0x89).
	OpCertIssue        byte = 0x84
	OpCertSignCSR      byte = 0x85
	OpCertRevoke       byte = 0x86
	OpCertVerify       byte = 0x87
	OpCertGetCA        byte = 0x88
	OpCertGetTLSConfig byte = 0x89

	// Trust Operations (0x7A-0x7C). Cross-sign / AIA / DANE composition over
	// the CA. Must mirror sdk/go/transport/quic/protocol.go exactly.
	OpTrustCrossSignedRootPool byte = 0x7A
	OpTrustAutoImportAIA       byte = 0x7B
	OpTrustPublishDANE         byte = 0x7C

	// Health Operations (0x90-0x92).
	OpHealth    byte = 0x90
	OpLiveness  byte = 0x91
	OpReadiness byte = 0x92

	// IAM Tenant Operations (0x95-0x99).
	OpIAMCreateTenant byte = 0x95
	OpIAMGetTenant    byte = 0x96
	OpIAMUpdateTenant byte = 0x97
	OpIAMDeleteTenant byte = 0x98
	OpIAMListTenants  byte = 0x99

	// IAM User Operations (0x9A-0x9E).
	OpIAMCreateUser byte = 0x9A
	OpIAMGetUser    byte = 0x9B
	OpIAMUpdateUser byte = 0x9C
	OpIAMDeleteUser byte = 0x9D
	OpIAMListUsers  byte = 0x9E

	// IAM Group Operations (0x9F, 0xAB-0xAF, 0xB5).
	OpIAMCreateGroup         byte = 0x9F
	OpIAMGetGroup            byte = 0xAB // Skip 0xA0-0xA8 (TimeSeries)
	OpIAMUpdateGroup         byte = 0xAC
	OpIAMDeleteGroup         byte = 0xAD
	OpIAMListGroups          byte = 0xAE
	OpIAMAddUserToGroup      byte = 0xAF
	OpIAMRemoveUserFromGroup byte = 0xB5 // Skip 0xB0-0xB4 (Operations)

	// Time-Series Operations (0xA0-0xA8).
	OpAggregate            byte = 0xA0
	OpTimeSeriesQuery      byte = 0xA1
	OpTSWritePoint         byte = 0xA2
	OpTSWriteBatch         byte = 0xA3
	OpTSQuery              byte = 0xA4
	OpTSDeleteSeries       byte = 0xA5
	OpTSGetRetentionPolicy byte = 0xA6
	OpTSSetRetentionPolicy byte = 0xA7
	OpTSListMetrics        byte = 0xA8

	// ObjectTransfer Operations (0xA9-0xAA) - peer-to-peer object data transfer for lazy snapshot recovery.
	// These operations enable recovering nodes to fetch object data from peers after
	// metadata-only snapshot recovery.
	OpFetchObject        byte = 0xA9
	OpStreamFetchObjects byte = 0xAA

	// Operations Management (0xB0-0xB4).
	OpMigrateData          byte = 0xB0
	OpGetMigrationProgress byte = 0xB1
	OpReplaceNode          byte = 0xB2
	OpUpgradeNode          byte = 0xB3
	OpGetUpgradeStatus     byte = 0xB4

	// IAM Role Operations (0xB6-0xBC).
	OpIAMCreateRole byte = 0xB6
	OpIAMGetRole    byte = 0xB7
	OpIAMUpdateRole byte = 0xB8
	OpIAMDeleteRole byte = 0xB9
	OpIAMListRoles  byte = 0xBA
	OpIAMAssignRole byte = 0xBB
	OpIAMRevokeRole byte = 0xBC

	// IAM App Operations (0xBD-0xBF, 0xC5-0xC7).
	OpIAMRegisterApp     byte = 0xBD
	OpIAMGetApp          byte = 0xBE
	OpIAMUpdateApp       byte = 0xBF
	OpIAMDeleteApp       byte = 0xC5 // Skip 0xC0-0xC4 (Barrier/Ceremony, deprecated Seal)
	OpIAMListApps        byte = 0xC6
	OpIAMRotateAppSecret byte = 0xC7

	// Barrier Operations - uses OpBarrier (0xC0) as the dispatch opcode with
	// sub-operation code in the first byte of payload (same pattern as Scheduler).
	// Replaces the deprecated seal-tree opcodes (0xC0-0xC4) removed in Phase 8c.
	OpBarrier byte = 0xC0 // Barrier dispatch opcode (sub-op in payload[0])

	// Ceremony Operations - uses OpCeremony (0xC1) as the dispatch opcode with
	// sub-operation code in the first byte of payload. Mirrors /api/v1/init.
	OpCeremony byte = 0xC1 // Ceremony dispatch opcode (sub-op in payload[0])

	// Opcodes 0xC2-0xC4 are reserved (formerly Unseal/Seal/SealRekey under the
	// deprecated seal subsystem). Reuse requires bumping the protocol version.

	// IAM Policy Operations (0xC8-0xCE).
	OpIAMCreatePolicy   byte = 0xC8
	OpIAMGetPolicy      byte = 0xC9
	OpIAMUpdatePolicy   byte = 0xCA
	OpIAMDeletePolicy   byte = 0xCB
	OpIAMListPolicies   byte = 0xCC
	OpIAMValidatePolicy byte = 0xCD
	OpIAMTestPolicy     byte = 0xCE

	// ObjectStorage Admin Operations (0xCF) - uses sub-operation dispatch pattern.
	// Sub-operation code in the first byte of payload (same pattern as Scheduler).
	OpOSAdmin byte = 0xCF

	// KMS Operations (0xD0-0xD7).
	OpKMSGenerateKey byte = 0xD0
	OpKMSImportKey   byte = 0xD1
	OpKMSGetKey      byte = 0xD2
	OpKMSListKeys    byte = 0xD3
	OpKMSRotateKey   byte = 0xD4
	OpKMSDeleteKey   byte = 0xD5
	OpKMSEncrypt     byte = 0xD6
	OpKMSDecrypt     byte = 0xD7

	// KMS DEK Operations (0xD8-0xDF).
	OpKMSDEKCreate     byte = 0xD8
	OpKMSDEKGet        byte = 0xD9
	OpKMSDEKList       byte = 0xDA
	OpKMSDEKRotate     byte = 0xDB
	OpKMSDEKDelete     byte = 0xDC
	OpKMSDEKStatus     byte = 0xDD
	OpKMSDEKSetDefault byte = 0xDE
	OpKMSDEKSetSafety  byte = 0xDF

	// Secrets Operations (0xE0-0xE6).
	OpSecretsStore        byte = 0xE0
	OpSecretsGet          byte = 0xE1
	OpSecretsList         byte = 0xE2
	OpSecretsDelete       byte = 0xE3
	OpSecretsListVersions byte = 0xE4
	OpSecretsRollback     byte = 0xE5
	OpSecretsRotate       byte = 0xE6

	// KMS Backend and Extended DEK Operations (0xE7-0xEB).
	OpKMSDEKClearSafety byte = 0xE7
	OpKMSBackendStatus  byte = 0xE8
	OpKMSBackendTest    byte = 0xE9
	OpKMSKeyVersions    byte = 0xEA
	OpKMSKeyStatus      byte = 0xEB

	// Extended Secrets Operations (0xEC-0xEF).
	OpSecretsGetVersion   byte = 0xEC
	OpSecretsSetRotation  byte = 0xED
	OpSecretsGetRotation  byte = 0xEE
	OpSecretsUpdateStatus byte = 0xEF

	// Secrets Sub-Dispatch (0x6F) - uses OpSecretsSubDispatch as the dispatch
	// opcode with sub-operation code in the first byte of payload, mirroring
	// the OpBarrier / OpCeremony / OpOrchestrator sub-dispatch pattern.
	//
	// Wire layout:
	//   [opcode=OpSecretsSubDispatch][tokenLen][token][payloadLen][subOp:1][body...]
	//
	// payload[0] selects the sub-operation (see OpSecretsSubEnsure below);
	// payload[1:] carries the JSON-encoded request body.
	//
	// Byte 0x6F is free: it sits between OpKMSDeleteRotationPolicy (0x6E)
	// and OpCreateUser (0x70), confirmed against the full opcode table in
	// protocol.go + protocol_iam.go.
	OpSecretsSubDispatch byte = 0x6F

	// KMS Rotation Policy Operations (0x6C-0x6E).
	OpKMSSetRotationPolicy    byte = 0x6C
	OpKMSGetRotationPolicy    byte = 0x6D
	OpKMSDeleteRotationPolicy byte = 0x6E

	// KMS Key Administration Operations (0xC2-0xC4). These bytes were freed
	// when the deprecated seal-tree opcodes (0xC0-0xC4) were removed in
	// Phase 8c; 0xC0/0xC1 remain the Barrier/Ceremony dispatch opcodes.
	OpKMSRevokeKey              byte = 0xC2
	OpKMSGetPublicKey           byte = 0xC3
	OpKMSGetSupportedAlgorithms byte = 0xC4

	// Transit Operations (0xF0-0xF9).
	OpCryptoEncrypt    byte = 0xF0
	OpCryptoDecrypt    byte = 0xF1
	OpCryptoRewrap     byte = 0xF2
	OpCryptoSign       byte = 0xF3
	OpCryptoVerify     byte = 0xF4
	OpCryptoHash       byte = 0xF5
	OpCryptoRandom     byte = 0xF6
	OpCryptoDataKey    byte = 0xF7
	OpCryptoHMAC       byte = 0xF8
	OpCryptoVerifyHMAC byte = 0xF9

	// Scheduler Operations - uses OpScheduler (0x00) as dispatch opcode
	// with sub-operation code in the first byte of payload.
	OpScheduler byte = 0x00 // Scheduler dispatch opcode (sub-op in payload[0])

	// Messaging Operations - uses OpMsg (0x00) as dispatch opcode
	// with sub-operation code in the first byte of payload.
	// Sub-opcode range: 0x20-0x2F (non-overlapping with scheduler 0x01-0x1F)
	OpMsg byte = 0x00 // Msg dispatch opcode (sub-op in payload[0])

	// Orchestrator Operations - uses OpOrchestrator (0x8F) as dispatch opcode
	// with sub-operation code in the first byte of payload.
	OpOrchestrator byte = 0x8F // Orchestrator dispatch opcode (sub-op in payload[0])
)

// Scheduler sub-operation codes (used in payload[0] when OpScheduler is the main opcode).
// These are NOT wire opcodes - they are decoded from the request payload.
const (
	SubOpSchedulerCreateJob     byte = 0x01
	SubOpSchedulerGetJob        byte = 0x02
	SubOpSchedulerUpdateJob     byte = 0x03
	SubOpSchedulerDeleteJob     byte = 0x04
	SubOpSchedulerListJobs      byte = 0x05
	SubOpSchedulerTriggerJob    byte = 0x06
	SubOpSchedulerPauseJob      byte = 0x07
	SubOpSchedulerResumeJob     byte = 0x08
	SubOpSchedulerCancelJob     byte = 0x09
	SubOpSchedulerGetJobHistory byte = 0x0A
	SubOpSchedulerGetStatus     byte = 0x0B
)

// Vector Extended sub-operation codes (used in payload[0] when OpScheduler is the main opcode).
// These are NOT wire opcodes - they are decoded from the request payload.
// Sub-opcode range: 0x40-0x4F (non-overlapping with scheduler 0x01-0x1F and messaging 0x20-0x2F)
const (
	SubOpVectorBatchDelete    byte = 0x40
	SubOpVectorListVectors    byte = 0x41
	SubOpVectorUpdateMetadata byte = 0x42
)

// ObjectStorage Admin sub-operation codes (used in payload[0] when OpOSAdmin is the main opcode).
// These are NOT wire opcodes - they are decoded from the request payload.
const (
	SubOpOSAdminCreate    byte = 0x01
	SubOpOSAdminUpdate    byte = 0x02
	SubOpOSAdminDelete    byte = 0x03
	SubOpOSAdminSetStatus byte = 0x04
	SubOpOSAdminGetStats  byte = 0x05
	SubOpOSAdminRunGC     byte = 0x06
	SubOpOSAdminGetGC     byte = 0x07
	SubOpOSAdminRepair    byte = 0x08
	SubOpOSAdminCompact   byte = 0x09
)

// Orchestrator sub-operation codes (used in payload[0] when OpOrchestrator is the main opcode).
// These are NOT wire opcodes - they are decoded from the request payload.
const (
	// Session operations (0x01-0x05).
	SubOpOrchestratorSpawn         byte = 0x01
	SubOpOrchestratorPrompt        byte = 0x02
	SubOpOrchestratorSessionStatus byte = 0x03
	SubOpOrchestratorCloseSession  byte = 0x04
	SubOpOrchestratorListSessions  byte = 0x05

	// Task operations (0x06-0x0A).
	SubOpOrchestratorCreateTask   byte = 0x06
	SubOpOrchestratorGetTask      byte = 0x07
	SubOpOrchestratorListTasks    byte = 0x08
	SubOpOrchestratorClaimTask    byte = 0x09
	SubOpOrchestratorCompleteTask byte = 0x0A

	// Backend operations (0x0B).
	SubOpOrchestratorListBackends byte = 0x0B

	// Health (0x0C).
	SubOpOrchestratorHealth byte = 0x0C

	// Inference (0x0D) - stateless fleet-routed inference (aggregate response).
	SubOpOrchestratorInference byte = 0x0D
)

// Secrets sub-operation codes (used in payload[0] when OpSecretsSubDispatch is the main opcode).
// These are NOT wire opcodes - they are decoded from the request payload.
const (
	// OpSecretsSubEnsure is the Vault-style generate-on-first-access primitive:
	// returns the existing value if found, otherwise generates and stores a new
	// random secret according to the supplied SecretSpec.
	OpSecretsSubEnsure byte = 0x01
)

// Barrier sub-operation codes (used in payload[0] when OpBarrier is the main opcode).
// These are NOT wire opcodes - they are decoded from the request payload.
// Mirrors the /api/v1/barrier REST surface. Sub-op codes 0x08-0x0B were
// retired with persisted Shamir share management; their byte values are
// intentionally left unassigned to preserve wire compatibility.
const (
	SubOpBarrierStatus                   byte = 0x01
	SubOpBarrierInitialize               byte = 0x02
	SubOpBarrierUnseal                   byte = 0x03
	SubOpBarrierSeal                     byte = 0x04
	SubOpBarrierShamirInitialize         byte = 0x05
	SubOpBarrierShamirUnsealShare        byte = 0x06
	SubOpBarrierShamirUnsealShares       byte = 0x07
	SubOpBarrierRekey                    byte = 0x0C
	SubOpBarrierRecoveryGenerate         byte = 0x0D
	SubOpBarrierRecoveryRecover          byte = 0x0E
	SubOpBarrierRecoveryDelete           byte = 0x0F
	SubOpBarrierRecoveryStatus           byte = 0x10
	SubOpBarrierRootToken                byte = 0x11
	SubOpBarrierEnableEncryption         byte = 0x12
	SubOpBarrierUpdateTenantQuotas       byte = 0x13
	SubOpBarrierUpdateTenantEntitlements byte = 0x14
)

// Ceremony sub-operation codes (used in payload[0] when OpCeremony is the main opcode).
// These are NOT wire opcodes - they are decoded from the request payload.
// Mirrors the /api/v1/init REST surface (8 sub-ops).
const (
	SubOpCeremonyInitialize            byte = 0x01
	SubOpCeremonyStatus                byte = 0x02
	SubOpCeremonyClaimCertBegin        byte = 0x03
	SubOpCeremonyClaimCertComplete     byte = 0x04
	SubOpCeremonyClaimShare            byte = 0x05
	SubOpCeremonySignCSR               byte = 0x06
	SubOpCeremonyListOfficers          byte = 0x07
	SubOpCeremonyEnrollExternalOfficer byte = 0x08
)

// BarrierSubOpcodeToString returns a human-readable name for a barrier
// sub-opcode. Used by logging and error messages.
func BarrierSubOpcodeToString(subOpcode byte) string {
	switch subOpcode {
	case SubOpBarrierStatus:
		return "BarrierStatus"
	case SubOpBarrierInitialize:
		return "BarrierInitialize"
	case SubOpBarrierUnseal:
		return "BarrierUnseal"
	case SubOpBarrierSeal:
		return "BarrierSeal"
	case SubOpBarrierShamirInitialize:
		return "BarrierShamirInitialize"
	case SubOpBarrierShamirUnsealShare:
		return "BarrierShamirUnsealShare"
	case SubOpBarrierShamirUnsealShares:
		return "BarrierShamirUnsealShares"
	case SubOpBarrierRekey:
		return "BarrierRekey"
	case SubOpBarrierRecoveryGenerate:
		return "BarrierRecoveryGenerate"
	case SubOpBarrierRecoveryRecover:
		return "BarrierRecoveryRecover"
	case SubOpBarrierRecoveryDelete:
		return "BarrierRecoveryDelete"
	case SubOpBarrierRecoveryStatus:
		return "BarrierRecoveryStatus"
	case SubOpBarrierRootToken:
		return "BarrierRootToken"
	case SubOpBarrierEnableEncryption:
		return "BarrierEnableEncryption"
	case SubOpBarrierUpdateTenantQuotas:
		return "BarrierUpdateTenantQuotas"
	case SubOpBarrierUpdateTenantEntitlements:
		return "BarrierUpdateTenantEntitlements"
	default:
		return fmt.Sprintf("UnknownBarrierOp(0x%02X)", subOpcode)
	}
}

// CeremonySubOpcodeToString returns a human-readable name for a ceremony
// sub-opcode. Used by logging and error messages.
func CeremonySubOpcodeToString(subOpcode byte) string {
	switch subOpcode {
	case SubOpCeremonyInitialize:
		return "CeremonyInitialize"
	case SubOpCeremonyStatus:
		return "CeremonyStatus"
	case SubOpCeremonyClaimCertBegin:
		return "CeremonyClaimCertBegin"
	case SubOpCeremonyClaimCertComplete:
		return "CeremonyClaimCertComplete"
	case SubOpCeremonyClaimShare:
		return "CeremonyClaimShare"
	case SubOpCeremonySignCSR:
		return "CeremonySignCSR"
	case SubOpCeremonyListOfficers:
		return "CeremonyListOfficers"
	case SubOpCeremonyEnrollExternalOfficer:
		return "CeremonyEnrollExternalOfficer"
	default:
		return fmt.Sprintf("UnknownCeremonyOp(0x%02X)", subOpcode)
	}
}

// Request represents a QUIC protocol request.
// Wire format: [opcode:1][tokenLen:2][token:N][payloadLen:4][payload:M].
type Request struct {
	Opcode  byte
	Token   string // Authentication token (JWT or similar)
	Payload []byte
}

// Response represents a QUIC protocol response.
// Wire format: [status:1][payloadLen:4][payload:N].
type Response struct {
	Status  byte
	Payload []byte
}

// clampUint32 converts a non-negative int length to uint32 for error
// reporting, saturating at math.MaxUint32 instead of wrapping.
func clampUint32(n int) uint32 {
	if uint64(n) > math.MaxUint32 {
		return math.MaxUint32
	}
	return uint32(n) //#nosec G115 -- bounds checked above
}

// WriteRequest writes a request to the stream.
// Returns an error if the write fails or payload exceeds maximum size.
func WriteRequest(w io.Writer, req *Request) error {
	if req == nil {
		return api.NewNilInputError("quic.WriteRequest", "request")
	}

	// Validate lengths before narrowing conversions so oversized inputs
	// cannot wrap around the fixed-width wire length prefixes.
	if len(req.Payload) > int(MaxPayloadSize) {
		return api.NewSizeLimitError("quic.WriteRequest", "payload",
			clampUint32(len(req.Payload)), MaxPayloadSize)
	}
	payloadLen := uint32(len(req.Payload)) //#nosec G115 -- bounds checked above

	if len(req.Token) > int(MaxTokenSize) {
		return api.NewSizeLimitError("quic.WriteRequest", "token",
			clampUint32(len(req.Token)), uint32(MaxTokenSize))
	}
	tokenLen := uint16(len(req.Token)) //#nosec G115 -- bounds checked above

	// Write opcode
	if _, err := w.Write([]byte{req.Opcode}); err != nil {
		return api.NewWriteError("quic.WriteRequest", "opcode", err)
	}

	// Write token length (big-endian, 2 bytes)
	tokenLenBuf := make([]byte, 2)
	binary.BigEndian.PutUint16(tokenLenBuf, tokenLen)
	if _, err := w.Write(tokenLenBuf); err != nil {
		return api.NewWriteError("quic.WriteRequest", "token length", err)
	}

	// Write token
	if tokenLen > 0 {
		if _, err := w.Write([]byte(req.Token)); err != nil {
			return api.NewWriteError("quic.WriteRequest", "token", err)
		}
	}

	// Write payload length (big-endian)
	lenBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(lenBuf, payloadLen)
	if _, err := w.Write(lenBuf); err != nil {
		return api.NewWriteError("quic.WriteRequest", "payload length", err)
	}

	// Write payload
	if payloadLen > 0 {
		if _, err := w.Write(req.Payload); err != nil {
			return api.NewWriteError("quic.WriteRequest", "payload", err)
		}
	}

	return nil
}

// ReadRequest reads a request from the stream.
// Returns an error if the read fails or the request is malformed.
func ReadRequest(r io.Reader) (*Request, error) {
	// Read opcode
	opcodeBuf := make([]byte, 1)
	if _, err := io.ReadFull(r, opcodeBuf); err != nil {
		return nil, api.NewReadError("quic.ReadRequest", "opcode", err)
	}

	// Read token length
	tokenLenBuf := make([]byte, 2)
	if _, err := io.ReadFull(r, tokenLenBuf); err != nil {
		return nil, api.NewReadError("quic.ReadRequest", "token length", err)
	}
	tokenLen := binary.BigEndian.Uint16(tokenLenBuf)

	// Validate token length. With MaxTokenSize at the ceiling of the 2-byte
	// wire prefix this cannot trigger; it stays as defense in depth should
	// the bound ever be lowered below the prefix range.
	if int(tokenLen) > int(MaxTokenSize) {
		return nil, api.NewSizeLimitError("quic.ReadRequest", "token", uint32(tokenLen), uint32(MaxTokenSize))
	}

	// Read token
	var token string
	if tokenLen > 0 {
		tokenBuf := make([]byte, tokenLen)
		if _, err := io.ReadFull(r, tokenBuf); err != nil {
			return nil, api.NewReadError("quic.ReadRequest", "token", err)
		}
		token = string(tokenBuf)
	}

	// Read payload length
	lenBuf := make([]byte, 4)
	if _, err := io.ReadFull(r, lenBuf); err != nil {
		return nil, api.NewReadError("quic.ReadRequest", "payload length", err)
	}
	payloadLen := binary.BigEndian.Uint32(lenBuf)

	// Validate payload length
	if payloadLen > MaxPayloadSize {
		return nil, api.NewSizeLimitError("quic.ReadRequest", "payload", payloadLen, MaxPayloadSize)
	}

	// Read payload
	var payload []byte
	if payloadLen > 0 {
		payload = make([]byte, payloadLen)
		if _, err := io.ReadFull(r, payload); err != nil {
			return nil, api.NewReadError("quic.ReadRequest", "payload", err)
		}
	}

	return &Request{
		Opcode:  opcodeBuf[0],
		Token:   token,
		Payload: payload,
	}, nil
}

// WriteResponse writes a response to the stream.
// Returns an error if the write fails or payload exceeds maximum size.
func WriteResponse(w io.Writer, resp *Response) error {
	if resp == nil {
		return api.NewNilInputError("quic.WriteResponse", "response")
	}

	payloadLen := uint32(len(resp.Payload)) //nolint:gosec // G115: payload length bounded by protocol
	if payloadLen > MaxPayloadSize {
		return api.NewSizeLimitError("quic.WriteResponse", "payload", payloadLen, MaxPayloadSize)
	}

	// Write status
	if _, err := w.Write([]byte{resp.Status}); err != nil {
		return api.NewWriteError("quic.WriteResponse", "status", err)
	}

	// Write payload length (big-endian)
	lenBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(lenBuf, payloadLen)
	if _, err := w.Write(lenBuf); err != nil {
		return api.NewWriteError("quic.WriteResponse", "payload length", err)
	}

	// Write payload
	if payloadLen > 0 {
		if _, err := w.Write(resp.Payload); err != nil {
			return api.NewWriteError("quic.WriteResponse", "payload", err)
		}
	}

	return nil
}

// ReadResponse reads a response from the stream.
// Returns an error if the read fails or the response is malformed.
func ReadResponse(r io.Reader) (*Response, error) {
	// Read status
	statusBuf := make([]byte, 1)
	if _, err := io.ReadFull(r, statusBuf); err != nil {
		return nil, api.NewReadError("quic.ReadResponse", "status", err)
	}

	// Read payload length
	lenBuf := make([]byte, 4)
	if _, err := io.ReadFull(r, lenBuf); err != nil {
		return nil, api.NewReadError("quic.ReadResponse", "payload length", err)
	}
	payloadLen := binary.BigEndian.Uint32(lenBuf)

	// Validate payload length
	if payloadLen > MaxPayloadSize {
		return nil, api.NewSizeLimitError("quic.ReadResponse", "payload", payloadLen, MaxPayloadSize)
	}

	// Read payload
	var payload []byte
	if payloadLen > 0 {
		payload = make([]byte, payloadLen)
		if _, err := io.ReadFull(r, payload); err != nil {
			return nil, api.NewReadError("quic.ReadResponse", "payload", err)
		}
	}

	return &Response{
		Status:  statusBuf[0],
		Payload: payload,
	}, nil
}

// EncodePayload encodes an entity to JSON bytes for use as a request/response payload.
// JSON is used instead of msgpack because it handles interface types (like elliptic.Curve)
// more gracefully by encoding them as their concrete representations.
func EncodePayload(entity interface{}) ([]byte, error) {
	if entity == nil {
		return nil, nil
	}

	buf, err := json.Marshal(entity)
	if err != nil {
		return nil, api.NewEncodeError("quic.EncodePayload", "payload", err)
	}

	return buf, nil
}

// DecodePayload decodes JSON bytes into an entity.
// The entity parameter must be a pointer to the target struct.
func DecodePayload(data []byte, entity interface{}) error {
	if len(data) == 0 {
		return nil
	}

	if entity == nil {
		return api.NewNilInputError("quic.DecodePayload", "entity")
	}

	if err := json.Unmarshal(data, entity); err != nil {
		return api.NewDecodeError("quic.DecodePayload", "payload", err)
	}

	return nil
}

// ErrorToStatus converts a QuicBaaS error to a status code.
func ErrorToStatus(err error) byte {
	if err == nil {
		return StatusOK
	}

	// Check if it's a QBError first
	var qbErr *errors.QBError
	if errors.As(err, &qbErr) {
		switch qbErr.Code {
		case errors.ErrNotFound:
			return StatusNotFound
		case errors.ErrUnauthorized:
			return StatusUnauthorized
		case errors.ErrInvalidInput:
			return StatusInvalidArgument
		case errors.ErrAlreadyExists:
			return StatusAlreadyExists
		case errors.ErrTimeout:
			return StatusTimeout
		case errors.ErrForbidden:
			return StatusPermissionDenied
		case errors.ErrResourceExhausted:
			return StatusResourceExhausted
		case errors.ErrUnavailable:
			return StatusUnavailable
		default:
			return StatusInternalError
		}
	}

	// Check for "not found" patterns in error messages for IAM errors
	// and other domain-specific errors that use custom error types
	errMsg := strings.ToLower(err.Error())
	if strings.Contains(errMsg, "not found") {
		return StatusNotFound
	}
	if strings.Contains(errMsg, "already exists") {
		return StatusAlreadyExists
	}
	if strings.Contains(errMsg, "unauthorized") || strings.Contains(errMsg, "authentication") {
		return StatusUnauthorized
	}
	if strings.Contains(errMsg, "forbidden") || strings.Contains(errMsg, "permission denied") {
		return StatusPermissionDenied
	}

	return StatusInternalError
}

// StatusToError converts a status code to a QuicBaaS error.
func StatusToError(status byte, message string) error {
	if status == StatusOK {
		return nil
	}

	var code errors.ErrorCode
	switch status {
	case StatusNotFound:
		code = errors.ErrNotFound
	case StatusUnauthorized:
		code = errors.ErrUnauthorized
	case StatusInvalidArgument:
		code = errors.ErrInvalidInput
	case StatusAlreadyExists:
		code = errors.ErrAlreadyExists
	case StatusTimeout:
		code = errors.ErrTimeout
	case StatusPermissionDenied:
		code = errors.ErrForbidden
	case StatusResourceExhausted:
		code = errors.ErrResourceExhausted
	case StatusUnavailable:
		code = errors.ErrUnavailable
	default:
		code = errors.ErrInternal
	}

	return api.NewProtocolStatusError("quic.protocol", code, message)
}

// opcodeNames is the O(1), constant-time opcode -> human-readable name lookup
// table. It replaces the former 243-case switch in OpcodeToString, mirroring
// the package's map-dispatch convention (see msgHandlers). Entries stay grouped
// by subsystem to track the wire opcode definitions above. The opcode space is
// dense and well under 256 entries, so a map keyed by the byte opcode is the
// idiomatic constant-time replacement for the linear switch.
var opcodeNames = map[byte]string{
	// Scheduler Operations (prefix-dispatched)
	OpScheduler: "Scheduler",

	// KV Operations
	OpPut:    "Put",
	OpGet:    "Get",
	OpDelete: "Delete",
	OpList:   "List",

	// Batch Operations
	OpBatchPut:    "BatchPut",
	OpBatchGet:    "BatchGet",
	OpBatchDelete: "BatchDelete",

	// Transaction Operations
	OpTransaction: "Transaction",

	// Query Operations
	OpQuery:     "Query",
	OpScanIndex: "ScanIndex",

	// Cluster Operations
	OpClusterStatus:      "ClusterStatus",
	OpGetLeader:          "GetLeader",
	OpAddNode:            "AddNode",
	OpRemoveNode:         "RemoveNode",
	OpCreateShard:        "CreateShard",
	OpRebalanceCluster:   "RebalanceCluster",
	OpListNodes:          "ListNodes",
	OpRebalanceStatus:    "RebalanceStatus",
	OpRebalancePlan:      "RebalancePlan",
	OpCancelMigration:    "CancelMigration",
	OpNodeReplaceRequest: "NodeReplaceRequest",

	// Backup Operations
	OpCreateBackup:  "CreateBackup",
	OpListBackups:   "ListBackups",
	OpRestoreBackup: "RestoreBackup",
	OpDeleteBackup:  "DeleteBackup",
	OpVerifyBackup:  "VerifyBackup",

	// Auth Operations
	OpLogout:        "Logout",
	OpRefreshToken:  "RefreshToken",
	OpValidateToken: "ValidateToken",

	// Dynamic Secrets Operations
	OpDynamicSecretsCreateRole:  "DynamicSecretsCreateRole",
	OpDynamicSecretsGetRole:     "DynamicSecretsGetRole",
	OpDynamicSecretsListRoles:   "DynamicSecretsListRoles",
	OpDynamicSecretsDeleteRole:  "DynamicSecretsDeleteRole",
	OpDynamicSecretsGenerate:    "DynamicSecretsGenerate",
	OpDynamicSecretsRenewLease:  "DynamicSecretsRenewLease",
	OpDynamicSecretsRevokeLease: "DynamicSecretsRevokeLease",
	OpDynamicSecretsListLeases:  "DynamicSecretsListLeases",

	// Parameter Store Operations
	OpParameterGet:         "ParameterGet",
	OpParameterSet:         "ParameterSet",
	OpParameterDelete:      "ParameterDelete",
	OpParameterList:        "ParameterList",
	OpParameterBatchGet:    "ParameterBatchGet",
	OpParameterBatchSet:    "ParameterBatchSet",
	OpParameterBatchDelete: "ParameterBatchDelete",
	OpParameterSearch:      "ParameterSearch",

	// RBAC Operations
	OpCreateUser: "CreateUser",
	OpDeleteUser: "DeleteUser",
	OpListUsers:  "ListUsers",
	OpCreateRole: "CreateRole",
	OpDeleteRole: "DeleteRole",
	OpListRoles:  "ListRoles",
	OpAssignRole: "AssignRole",
	OpRevokeRole: "RevokeRole",

	// Cache Operations
	OpGetCacheStats:     "GetCacheStats",
	OpClearCache:        "ClearCache",
	OpGetCacheConfig:    "GetCacheConfig",
	OpUpdateCacheConfig: "UpdateCacheConfig",

	// Certificate Operations
	OpCertIssue:        "CertIssue",
	OpCertSignCSR:      "CertSignCSR",
	OpCertRevoke:       "CertRevoke",
	OpCertVerify:       "CertVerify",
	OpCertGetCA:        "CertGetCA",
	OpCertGetTLSConfig: "CertGetTLSConfig",

	// Health Operations
	OpHealth:    "Health",
	OpLiveness:  "Liveness",
	OpReadiness: "Readiness",

	// IAM Tenant Operations
	OpIAMCreateTenant: "IAMCreateTenant",
	OpIAMGetTenant:    "IAMGetTenant",
	OpIAMUpdateTenant: "IAMUpdateTenant",
	OpIAMDeleteTenant: "IAMDeleteTenant",
	OpIAMListTenants:  "IAMListTenants",

	// IAM User Operations
	OpIAMCreateUser: "IAMCreateUser",
	OpIAMGetUser:    "IAMGetUser",
	OpIAMUpdateUser: "IAMUpdateUser",
	OpIAMDeleteUser: "IAMDeleteUser",
	OpIAMListUsers:  "IAMListUsers",

	// IAM Group Operations
	OpIAMCreateGroup:         "IAMCreateGroup",
	OpIAMGetGroup:            "IAMGetGroup",
	OpIAMUpdateGroup:         "IAMUpdateGroup",
	OpIAMDeleteGroup:         "IAMDeleteGroup",
	OpIAMListGroups:          "IAMListGroups",
	OpIAMAddUserToGroup:      "IAMAddUserToGroup",
	OpIAMRemoveUserFromGroup: "IAMRemoveUserFromGroup",

	// IAM Role Operations
	OpIAMCreateRole: "IAMCreateRole",
	OpIAMGetRole:    "IAMGetRole",
	OpIAMUpdateRole: "IAMUpdateRole",
	OpIAMDeleteRole: "IAMDeleteRole",
	OpIAMListRoles:  "IAMListRoles",
	OpIAMAssignRole: "IAMAssignRole",
	OpIAMRevokeRole: "IAMRevokeRole",

	// IAM App Operations
	OpIAMRegisterApp:     "IAMRegisterApp",
	OpIAMGetApp:          "IAMGetApp",
	OpIAMUpdateApp:       "IAMUpdateApp",
	OpIAMDeleteApp:       "IAMDeleteApp",
	OpIAMListApps:        "IAMListApps",
	OpIAMRotateAppSecret: "IAMRotateAppSecret",

	// IAM Policy Operations
	OpIAMCreatePolicy:   "IAMCreatePolicy",
	OpIAMGetPolicy:      "IAMGetPolicy",
	OpIAMUpdatePolicy:   "IAMUpdatePolicy",
	OpIAMDeletePolicy:   "IAMDeletePolicy",
	OpIAMListPolicies:   "IAMListPolicies",
	OpIAMValidatePolicy: "IAMValidatePolicy",
	OpIAMTestPolicy:     "IAMTestPolicy",

	// Time-Series Operations
	OpAggregate:            "Aggregate",
	OpTimeSeriesQuery:      "TimeSeriesQuery",
	OpTSWritePoint:         "TSWritePoint",
	OpTSWriteBatch:         "TSWriteBatch",
	OpTSQuery:              "TSQuery",
	OpTSDeleteSeries:       "TSDeleteSeries",
	OpTSGetRetentionPolicy: "TSGetRetentionPolicy",
	OpTSSetRetentionPolicy: "TSSetRetentionPolicy",
	OpTSListMetrics:        "TSListMetrics",

	// ObjectTransfer Operations
	OpFetchObject:        "FetchObject",
	OpStreamFetchObjects: "StreamFetchObjects",

	// Operations Management
	OpMigrateData:          "MigrateData",
	OpGetMigrationProgress: "GetMigrationProgress",
	OpReplaceNode:          "ReplaceNode",
	OpUpgradeNode:          "UpgradeNode",
	OpGetUpgradeStatus:     "GetUpgradeStatus",

	// Barrier Operations (sub-op dispatched)
	OpBarrier: "Barrier",

	// Ceremony Operations (sub-op dispatched)
	OpCeremony: "Ceremony",

	// KMS Operations
	OpKMSGenerateKey: "KMSGenerateKey",
	OpKMSImportKey:   "KMSImportKey",
	OpKMSGetKey:      "KMSGetKey",
	OpKMSListKeys:    "KMSListKeys",
	OpKMSRotateKey:   "KMSRotateKey",
	OpKMSDeleteKey:   "KMSDeleteKey",
	OpKMSEncrypt:     "KMSEncrypt",
	OpKMSDecrypt:     "KMSDecrypt",

	// KMS DEK Operations
	OpKMSDEKCreate:               "KMSDEKCreate",
	OpKMSDEKGet:                  "KMSDEKGet",
	OpKMSDEKList:                 "KMSDEKList",
	OpKMSDEKRotate:               "KMSDEKRotate",
	OpKMSDEKDelete:               "KMSDEKDelete",
	OpKMSDEKStatus:               "KMSDEKStatus",
	OpKMSDEKSetDefault:           "KMSDEKSetDefault",
	OpKMSDEKSetSafety:            "KMSDEKSetSafety",
	OpKMSDEKClearSafety:          "KMSDEKClearSafety",
	OpKMSDEKStartReencryption:    "KMSDEKStartReencryption",
	OpKMSDEKGetReencryptProgress: "KMSDEKGetReencryptProgress",

	// KMS Backend Operations
	OpKMSBackendStatus: "KMSBackendStatus",
	OpKMSBackendTest:   "KMSBackendTest",
	OpKMSKeyVersions:   "KMSKeyVersions",
	OpKMSKeyStatus:     "KMSKeyStatus",

	// Extended KMS Operations
	OpKMSSetKeyPolicy: "KMSSetKeyPolicy",
	OpKMSGetKeyPolicy: "KMSGetKeyPolicy",
	// Rotation policy ops are handled in the 0xFD-0xFF block above
	OpKMSSetKeySafety:         "KMSSetKeySafety",
	OpKMSClearKeySafety:       "KMSClearKeySafety",
	OpKMSSafetyCheck:          "KMSSafetyCheck",
	OpKMSSafetyReport:         "KMSSafetyReport",
	OpKMSExportKey:            "KMSExportKey",
	OpKMSUnwrapKey:            "KMSUnwrapKey",
	OpKMSGetKeyMetadata:       "KMSGetKeyMetadata",
	OpKMSUpdateKeyMetadata:    "KMSUpdateKeyMetadata",
	OpKMSSetRotationPolicy:    "KMSSetRotationPolicy",
	OpKMSGetRotationPolicy:    "KMSGetRotationPolicy",
	OpKMSDeleteRotationPolicy: "KMSDeleteRotationPolicy",

	// KMS Key Administration Operations
	OpKMSRevokeKey:              "KMSRevokeKey",
	OpKMSGetPublicKey:           "KMSGetPublicKey",
	OpKMSGetSupportedAlgorithms: "KMSGetSupportedAlgorithms",

	// Secrets Sub-Dispatch (sub-op in payload)
	OpSecretsSubDispatch: "SecretsSubDispatch",

	// Secrets Operations
	OpSecretsStore:        "SecretsStore",
	OpSecretsGet:          "SecretsGet",
	OpSecretsList:         "SecretsList",
	OpSecretsDelete:       "SecretsDelete",
	OpSecretsListVersions: "SecretsListVersions",
	OpSecretsRollback:     "SecretsRollback",
	OpSecretsRotate:       "SecretsRotate",
	OpSecretsGetVersion:   "SecretsGetVersion",
	OpSecretsSetRotation:  "SecretsSetRotation",
	OpSecretsGetRotation:  "SecretsGetRotation",
	OpSecretsUpdateStatus: "SecretsUpdateStatus",
	OpSecretsGetByPrefix:  "SecretsGetByPrefix",
	OpSecretsWatch:        "SecretsWatch",

	// Crypto Operations
	OpCryptoEncrypt:    "CryptoEncrypt",
	OpCryptoDecrypt:    "CryptoDecrypt",
	OpCryptoRewrap:     "CryptoRewrap",
	OpCryptoSign:       "CryptoSign",
	OpCryptoVerify:     "CryptoVerify",
	OpCryptoHash:       "CryptoHash",
	OpCryptoRandom:     "CryptoRandom",
	OpCryptoDataKey:    "CryptoDataKey",
	OpCryptoHMAC:       "CryptoHMAC",
	OpCryptoVerifyHMAC: "CryptoVerifyHMAC",

	// Vector Operations
	OpVectorCreateNamespace:       "VectorCreateNamespace",
	OpVectorDeleteNamespace:       "VectorDeleteNamespace",
	OpVectorListNamespaces:        "VectorListNamespaces",
	OpVectorGetNamespaceInfo:      "VectorGetNamespaceInfo",
	OpVectorUpsert:                "VectorUpsert",
	OpVectorGet:                   "VectorGet",
	OpVectorDelete:                "VectorDelete",
	OpVectorBatchUpsert:           "VectorBatchUpsert",
	OpVectorSearchSimilar:         "VectorSearchSimilar",
	OpVectorSearchByMetadata:      "VectorSearchByMetadata",
	OpVectorHybridSearch:          "VectorHybridSearch",
	OpVectorSearchByID:            "VectorSearchByID",
	OpVectorRebuildIndex:          "VectorRebuildIndex",
	OpVectorGetRebuildProgress:    "VectorGetRebuildProgress",
	OpVectorGetIndexStats:         "VectorGetIndexStats",
	OpVectorCompact:               "VectorCompact",
	OpVectorGetCompactionProgress: "VectorGetCompactionProgress",

	// ObjectStorage Operations
	OpOSPutObject:            "OSPutObject",
	OpOSGetObject:            "OSGetObject",
	OpOSDeleteObject:         "OSDeleteObject",
	OpOSHeadObject:           "OSHeadObject",
	OpOSListObjects:          "OSListObjects",
	OpOSListByTag:            "OSListByTag",
	OpOSListByTenant:         "OSListByTenant",
	OpOSCopyObject:           "OSCopyObject",
	OpOSReplicateObject:      "OSReplicateObject",
	OpOSMigrateObject:        "OSMigrateObject",
	OpOSArchiveObject:        "OSArchiveObject",
	OpOSRestoreObject:        "OSRestoreObject",
	OpOSVerifyObject:         "OSVerifyObject",
	OpOSGetObjectMetadata:    "OSGetObjectMetadata",
	OpOSUpdateObjectMetadata: "OSUpdateObjectMetadata",
	OpOSGetStorageStats:      "OSGetStorageStats",
	OpOSSetLifecyclePolicy:   "OSSetLifecyclePolicy",
	OpOSGetLifecyclePolicy:   "OSGetLifecyclePolicy",
	OpOSGetObjectPolicy:      "OSGetObjectPolicy",
	OpOSListBackends:         "OSListBackends",
	OpOSGetBackendHealth:     "OSGetBackendHealth",
	OpOSSetTenantQuotas:      "OSSetTenantQuotas",
	OpOSBatchPutObjects:      "OSBatchPutObjects",
	OpOSBatchGetObjects:      "OSBatchGetObjects",
	OpOSBatchDeleteObjects:   "OSBatchDeleteObjects",

	// ObjectStorage Admin Operations (sub-op dispatch)
	OpOSAdmin: "OSAdmin",
	// Ollama Operations
	OpOllamaEmbed:          "OllamaEmbed",
	OpOllamaEmbedWithModel: "OllamaEmbedWithModel",
	OpOllamaEmbedBatch:     "OllamaEmbedBatch",
	OpOllamaListModels:     "OllamaListModels",
	OpOllamaGetModelInfo:   "OllamaGetModelInfo",
	OpOllamaHealth:         "OllamaHealth",

	// Embedding Operations
	OpEmbeddingGenerate:      "EmbeddingGenerate",
	OpEmbeddingBatchGenerate: "EmbeddingBatchGenerate",
	OpEmbeddingListProviders: "EmbeddingListProviders",
	OpEmbeddingGetProvider:   "EmbeddingGetProvider",
	OpEmbeddingSetDefault:    "EmbeddingSetDefault",
	OpEmbeddingGetCacheStats: "EmbeddingGetCacheStats",
	OpEmbeddingClearCache:    "EmbeddingClearCache",

	// RAG Operations
	OpRAGQuery:        "RAGQuery",
	OpRAGBuildContext: "RAGBuildContext",

	// PEP Operations
	OpPEPCheck:      "PEPCheck",
	OpPEPCheckBatch: "PEPCheckBatch",
	OpPEPSubscribe:  "PEPSubscribe",
}

// OpcodeToString returns a human-readable name for an opcode via an O(1) map
// lookup. Unknown opcodes render as their hex value, preserving the exact
// behavior of the former switch's default case.
func OpcodeToString(opcode byte) string {
	if name, ok := opcodeNames[opcode]; ok {
		return name
	}
	return fmt.Sprintf("Unknown(0x%02X)", opcode)
}

// SchedulerSubOpcodeToString returns a human-readable name for a scheduler sub-opcode.
func SchedulerSubOpcodeToString(subOpcode byte) string {
	switch subOpcode {
	case SubOpSchedulerCreateJob:
		return "SchedulerCreateJob"
	case SubOpSchedulerGetJob:
		return "SchedulerGetJob"
	case SubOpSchedulerUpdateJob:
		return "SchedulerUpdateJob"
	case SubOpSchedulerDeleteJob:
		return "SchedulerDeleteJob"
	case SubOpSchedulerListJobs:
		return "SchedulerListJobs"
	case SubOpSchedulerTriggerJob:
		return "SchedulerTriggerJob"
	case SubOpSchedulerPauseJob:
		return "SchedulerPauseJob"
	case SubOpSchedulerResumeJob:
		return "SchedulerResumeJob"
	case SubOpSchedulerCancelJob:
		return "SchedulerCancelJob"
	case SubOpSchedulerGetJobHistory:
		return "SchedulerGetJobHistory"
	case SubOpSchedulerGetStatus:
		return "SchedulerGetStatus"
	default:
		return fmt.Sprintf("UnknownSchedulerOp(0x%02X)", subOpcode)
	}
}

// SecretsSubOpcodeToString returns a human-readable name for a secrets
// sub-opcode. Used by logging and error messages when OpSecretsSubDispatch
// is the wire opcode.
func SecretsSubOpcodeToString(subOpcode byte) string {
	switch subOpcode {
	case OpSecretsSubEnsure:
		return "SecretsEnsure"
	default:
		return fmt.Sprintf("UnknownSecretsSubOp(0x%02X)", subOpcode)
	}
}

// VectorExtendedSubOpcodeToString returns a human-readable name for a vector extended sub-opcode.
func VectorExtendedSubOpcodeToString(subOpcode byte) string {
	switch subOpcode {
	case SubOpVectorBatchDelete:
		return "VectorBatchDelete"
	case SubOpVectorListVectors:
		return "VectorListVectors"
	case SubOpVectorUpdateMetadata:
		return "VectorUpdateMetadata"
	default:
		return fmt.Sprintf("UnknownVectorExtendedOp(0x%02X)", subOpcode)
	}
}
