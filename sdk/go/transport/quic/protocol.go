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

// Package quic provides the QUIC transport implementation for the QuicBaaS SDK.
// QUIC offers low-latency, multiplexed communication with built-in TLS 1.3,
// making it ideal for mobile clients and high-latency networks.
package quic

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"

	sshclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/ssh"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// Protocol Constants.
const (
	// ProtocolVersion is the current protocol version.
	ProtocolVersion byte = 0x01

	// Status codes matching the server protocol.
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

	// MaxPayloadSize is the maximum payload size (16MB).
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
	// while still tightly bounding the per-frame read allocation. Must
	// stay identical to the server-side bound in pkg/api/quic.
	MaxTokenSize uint16 = 64*1024 - 1
)

// Operation codes - mapping all QuicBaaS operations.
const (
	// KV Operations (0x01-0x06).
	OpPut             byte = 0x01
	OpGet             byte = 0x02
	OpDelete          byte = 0x03
	OpList            byte = 0x04
	OpListEntityTypes byte = 0x05
	OpWatch           byte = 0x06

	// Batch Operations (0x10-0x12).
	OpBatchPut    byte = 0x10
	OpBatchGet    byte = 0x11
	OpBatchDelete byte = 0x12

	// Transaction Operations (0x20).
	OpTransaction byte = 0x20

	// Query Operations (0x30-0x31).
	OpQuery     byte = 0x30
	OpScanIndex byte = 0x31

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

	// Embedding Operations (0x4D-0x4F, 0x55-0x58) - matching server protocol.
	OpEmbeddingGenerate      byte = 0x4D
	OpEmbeddingBatchGenerate byte = 0x4E
	OpEmbeddingListProviders byte = 0x4F
	OpEmbeddingGetProvider   byte = 0x55
	OpEmbeddingSetDefault    byte = 0x56
	OpEmbeddingGetCacheStats byte = 0x57
	OpEmbeddingClearCache    byte = 0x58

	// Backup Operations (0x50-0x54).
	OpBackupCreate  byte = 0x50
	OpBackupList    byte = 0x51
	OpBackupRestore byte = 0x52
	OpBackupDelete  byte = 0x53
	OpBackupVerify  byte = 0x54

	// Auth Operations (0x60-0x65).
	OpAuthLogin         byte = 0x60
	OpAuthLogout        byte = 0x61
	OpAuthRefreshToken  byte = 0x62
	OpAuthValidateToken byte = 0x63
	// Unified login ceremony (0x78-0x79, free on both SDK and server).
	OpAuthBeginLogin  byte = 0x78
	OpAuthFinishLogin byte = 0x79

	// Dynamic Secrets Operations (0x64-0x6B). These MUST match
	// pkg/api/quic/protocol.go OpDynamicSecrets* byte-for-byte; the server
	// dispatches them as top-level opcodes (no sub-op byte), so no slice of
	// the OpOrchestrator sub-op space is consumed. TestDynamicSecretsOpcodes
	// pins the values.
	OpDynamicSecretsCreateRole  byte = 0x64
	OpDynamicSecretsGetRole     byte = 0x65
	OpDynamicSecretsListRoles   byte = 0x66
	OpDynamicSecretsDeleteRole  byte = 0x67
	OpDynamicSecretsGenerate    byte = 0x68
	OpDynamicSecretsRenewLease  byte = 0x69
	OpDynamicSecretsRevokeLease byte = 0x6A
	OpDynamicSecretsListLeases  byte = 0x6B

	// RBAC Operations (0x70-0x77).
	OpRBACCreateUser byte = 0x70
	OpRBACDeleteUser byte = 0x71
	OpRBACListUsers  byte = 0x72
	OpRBACCreateRole byte = 0x73
	OpRBACDeleteRole byte = 0x74
	OpRBACListRoles  byte = 0x75
	OpRBACAssignRole byte = 0x76
	OpRBACRevokeRole byte = 0x77

	// Cache Operations (0x80-0x83).
	OpCacheGetStats     byte = 0x80
	OpCacheClear        byte = 0x81
	OpCacheGetConfig    byte = 0x82
	OpCacheUpdateConfig byte = 0x83

	// Certificate Operations (0x84-0x89) - matching server pkg/api/quic/protocol.go.
	OpCertIssue        byte = 0x84
	OpCertSignCSR      byte = 0x85
	OpCertRevoke       byte = 0x86
	OpCertVerify       byte = 0x87
	OpCertGetCA        byte = 0x88
	OpCertGetTLSConfig byte = 0x89

	// Trust Operations (0x7A-0x7C) - matching server pkg/api/quic/protocol.go.
	OpTrustCrossSignedRootPool byte = 0x7A
	OpTrustAutoImportAIA       byte = 0x7B
	OpTrustPublishDANE         byte = 0x7C

	// Health Operations (0x90-0x92).
	OpHealth    byte = 0x90
	OpLiveness  byte = 0x91
	OpReadiness byte = 0x92

	// TimeSeries Operations (0xA0-0xA8) - matching server pkg/api/quic/protocol.go.
	// The server reserves 0xA0/0xA1 for the legacy Aggregate / TimeSeriesQuery
	// opcodes and assigns the modern TS verbs 0xA2-0xA8. The SDK MUST mirror these
	// values exactly; an earlier revision shifted them down by two (0xA0-0xA7),
	// which mis-routed every TimeSeries call on the wire.
	OpAggregate            byte = 0xA0
	OpTimeSeriesQuery      byte = 0xA1
	OpTSWritePoint         byte = 0xA2
	OpTSWriteBatch         byte = 0xA3
	OpTSQuery              byte = 0xA4
	OpTSDeleteSeries       byte = 0xA5
	OpTSGetRetentionPolicy byte = 0xA6
	OpTSSetRetentionPolicy byte = 0xA7
	OpTSListMetrics        byte = 0xA8

	// Operations/Migration Operations (0xB0-0xB4).
	OpMigrateData          byte = 0xB0
	OpGetMigrationProgress byte = 0xB1
	OpReplaceNode          byte = 0xB2
	OpUpgradeNode          byte = 0xB3
	OpGetUpgradeStatus     byte = 0xB4

	// KMS Operations (matching server protocol).
	// Note: Opcodes 0xC0-0xC4 are reserved for Seal operations
	// Note: Opcodes 0xC5-0xC7 are reserved for IAM App operations
	// Note: Opcodes 0xC8-0xCE are reserved for IAM Policy operations
	OpKMSGenerateKey    byte = 0xD0
	OpKMSImportKey      byte = 0xD1
	OpKMSGetKey         byte = 0xD2
	OpKMSListKeys       byte = 0xD3
	OpKMSRotateKey      byte = 0xD4
	OpKMSDeleteKey      byte = 0xD5
	OpKMSEncrypt        byte = 0xD6
	OpKMSDecrypt        byte = 0xD7
	OpKMSCreateDEK      byte = 0xD8
	OpKMSGetDEK         byte = 0xD9
	OpKMSListDEKs       byte = 0xDA
	OpKMSRotateDEK      byte = 0xDB
	OpKMSDeleteDEK      byte = 0xDC
	OpKMSGetDEKStatus   byte = 0xDD
	OpKMSSetDefaultDEK  byte = 0xDE
	OpKMSSetDEKSafety   byte = 0xDF
	OpKMSClearDEKSafety byte = 0xE7

	// DEK Re-encryption Operations (0x07-0x08) - matching server protocol.go.
	OpKMSDEKStartReencryption    byte = 0x07
	OpKMSDEKGetReencryptProgress byte = 0x08
	OpKMSBackendStatus           byte = 0xE8
	OpKMSBackendTest             byte = 0xE9
	OpKMSKeyVersions             byte = 0xEA
	OpKMSKeyStatus               byte = 0xEB
	OpKMSExportKey               byte = 0x09
	OpKMSUnwrapKey               byte = 0x0A
	OpKMSGetKeyMetadata          byte = 0x0B
	OpKMSUpdateKeyMetadata       byte = 0x0C
	OpKMSSetKeyPolicy            byte = 0x0D
	OpKMSGetKeyPolicy            byte = 0x0E
	OpKMSSetKeySafety            byte = 0x0F
	OpKMSClearKeySafety          byte = 0x13
	OpKMSSafetyCheck             byte = 0x14
	OpKMSSafetyReport            byte = 0x15
	OpKMSSetRotationPolicy       byte = 0x6C
	OpKMSGetRotationPolicy       byte = 0x6D
	OpKMSDeleteRotationPolicy    byte = 0x6E
	OpKMSRevokeKey               byte = 0xC2
	OpKMSGetPublicKey            byte = 0xC3
	OpKMSGetSupportedAlgorithms  byte = 0xC4

	// ObjectStorage Operations (0x28-0x3B) - matching server pkg/api/quic/protocol.go.
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

	// ObjectStorageAdmin Operations (0xE1-0xEE, avoiding conflict with KMS at 0xE7-0xEB).
	OpOSACreateBackend                byte = 0xE1
	OpOSAUpdateBackend                byte = 0xE2
	OpOSADeleteBackend                byte = 0xE3
	OpOSASetBackendStatus             byte = 0xE4
	OpOSAGetStorageStats              byte = 0xE5
	OpOSARunGarbageCollection         byte = 0xE6
	OpOSAGetGarbageCollectionProgress byte = 0xEC
	OpOSARepairObject                 byte = 0xED
	OpOSACompactBackend               byte = 0xEE

	// Secrets Operations (0xE0-0xEF) - matching server protocol.go.
	OpSecretsStore        byte = 0xE0
	OpSecretsGet          byte = 0xE1
	OpSecretsList         byte = 0xE2
	OpSecretsDelete       byte = 0xE3
	OpSecretsListVersions byte = 0xE4
	OpSecretsRollback     byte = 0xE5
	OpSecretsRotate       byte = 0xE6
	OpSecretsGetVersion   byte = 0xEC
	OpSecretsSetRotation  byte = 0xED
	OpSecretsGetRotation  byte = 0xEE
	OpSecretsUpdateStatus byte = 0xEF

	// Crypto (transit) Operations (0xF0-0xF9) - matching server protocol.go.
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

	// Scheduler Operations - uses OpScheduler (0x00) as the dispatch opcode
	// with the sub-operation code in the first byte of the payload, mirroring
	// the server (pkg/api/quic/protocol.go OpScheduler + pkg/api/quic/handler.go
	// handleSchedulerDispatch). The legacy per-method opcodes (0xF0-0xFA) were
	// removed: they collided with the Crypto/transit opcodes (OpCryptoEncrypt..
	// OpCryptoVerifyHMAC = 0xF0-0xF9) and the server never dispatched scheduler
	// on those values.
	OpScheduler byte = 0x00

	// Messaging Operations - uses OpMsg (0x00) as the dispatch opcode with the
	// sub-operation code in the first byte of the payload. The messaging sub-op
	// range (0x20-0x2F) is disjoint from the scheduler (0x01-0x1F) and
	// extended-vector (0x40-0x4F) ranges that share the same 0x00 dispatch byte
	// on the server, so the server distinguishes them by the sub-op value.
	// Matches server-side OpMsg in pkg/api/quic/protocol.go.
	OpMsg byte = 0x00

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
	OpIAMGetGroup            byte = 0xAB
	OpIAMUpdateGroup         byte = 0xAC
	OpIAMDeleteGroup         byte = 0xAD
	OpIAMListGroups          byte = 0xAE
	OpIAMAddUserToGroup      byte = 0xAF
	OpIAMRemoveUserFromGroup byte = 0xB5

	// IAM Role Operations (0xB6-0xBC).
	OpIAMCreateRole byte = 0xB6
	OpIAMGetRole    byte = 0xB7
	OpIAMUpdateRole byte = 0xB8
	OpIAMDeleteRole byte = 0xB9
	OpIAMListRoles  byte = 0xBA
	OpIAMAssignRole byte = 0xBB
	OpIAMRevokeRole byte = 0xBC

	// IAM App Operations (0xBD-0xC7).
	OpIAMRegisterApp     byte = 0xBD
	OpIAMGetApp          byte = 0xBE
	OpIAMUpdateApp       byte = 0xBF
	OpIAMDeleteApp       byte = 0xC5
	OpIAMListApps        byte = 0xC6
	OpIAMRotateAppSecret byte = 0xC7

	// IAM Policy Operations (0xC8-0xCE).
	OpIAMCreatePolicy   byte = 0xC8
	OpIAMGetPolicy      byte = 0xC9
	OpIAMUpdatePolicy   byte = 0xCA
	OpIAMDeletePolicy   byte = 0xCB
	OpIAMListPolicies   byte = 0xCC
	OpIAMValidatePolicy byte = 0xCD
	OpIAMTestPolicy     byte = 0xCE

	// IAM MFA Operations (0xFE) - dispatch opcode with sub-operation in payload.
	// Uses the IAMSubRequest envelope to carry the sub-op code and inner payload.
	// Matches server-side OpMFA in protocol_iam.go.
	OpMFA byte = 0xFE

	// Orchestrator Operations (0x8F) - dispatch opcode with sub-operation in payload[0].
	// Matches server-side OpOrchestrator in pkg/api/quic/protocol.go.
	// Wire format: [0x8F][tokenLen][token][payloadLen][subOp:1][body...].
	OpOrchestrator byte = 0x8F

	// Barrier Operations - uses OpBarrier (0xC0) as the dispatch opcode with
	// sub-operation code in the first byte of payload. Mirrors the QuicBaaS
	// /api/v1/barrier surface (17 sub-ops). Phase 8c replacement for the
	// deprecated seal-tree opcodes (0xC0-0xC4).
	OpBarrier byte = 0xC0

	// Ceremony Operations - uses OpCeremony (0xC1) as the dispatch opcode
	// with sub-operation code in the first byte of payload. Mirrors the
	// QuicBaaS /api/v1/init surface (8 sub-ops).
	OpCeremony byte = 0xC1

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
	OpVectorRebuildIndex          byte = 0x23
	OpVectorGetRebuildProgress    byte = 0x24
	OpVectorGetIndexStats         byte = 0x25
	OpVectorCompact               byte = 0x26
	OpVectorGetCompactionProgress byte = 0x27
)

// MFA sub-operation codes for OpMFA (0xFE).
// These match the server-side SubOp constants in protocol_iam.go.
const (
	SubOpMFABeginEnroll         byte = 0x00
	SubOpMFACompleteEnroll      byte = 0x01
	SubOpMFACancelEnroll        byte = 0x02
	SubOpMFAGetStatus           byte = 0x03
	SubOpMFADisable             byte = 0x04
	SubOpMFABeginChallenge      byte = 0x05
	SubOpMFAVerifyChallenge     byte = 0x06
	SubOpMFAGenerateBackupCodes byte = 0x07
	SubOpMFAVerifyBackupCode    byte = 0x08
	SubOpMFAGetBackupCodeStatus byte = 0x09
)

// Orchestrator sub-operation codes for OpOrchestrator (0x8F).
// These match the server-side SubOpOrchestrator* constants in pkg/api/quic/protocol.go.
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

// SubOpOrchestratorRotateAccount carries the session pooled-account rotation
// verb on the OpOrchestrator (0x8F) dispatch byte, in its own disjoint 0x20
// slot rather than at the end of the contiguous 0x01-0x0D orchestrator range.
//
// The placement is load-bearing, not cosmetic: the server authorizes the
// contiguous range at the surface level as orchestrator:status, and rotation is
// a write that must resolve orchestrator:spawn. A sub-op inside that range
// could not do so. 0x20 is free on this dispatch byte — accounts start at 0x30,
// projects at 0x40, SSH at 0x50, certificate extensions at 0x60 and attestation
// at 0x70.
//
// Cross-reference: pkg/api/quic/protocol.go SubOpOrchestratorRotateAccount —
// the two files MUST stay in sync, and the parity test in
// pkg/api/quic asserts the literal byte on both sides.
const (
	SubOpOrchestratorRotateAccount byte = 0x20
)

// orchestratorMethodToSubOp maps Orchestrator method names to their sub-operation
// codes for O(1) lookup. All entries use OpOrchestrator (0x8F) as the wire opcode
// with the SubOp byte prepended to the payload.
var orchestratorMethodToSubOp = map[string]byte{
	"SpawnSession":  SubOpOrchestratorSpawn,
	"PromptSession": SubOpOrchestratorPrompt,
	"SessionStatus": SubOpOrchestratorSessionStatus,
	"CloseSession":  SubOpOrchestratorCloseSession,

	"RotateSessionAccount": SubOpOrchestratorRotateAccount,

	"ListSessions": SubOpOrchestratorListSessions,
	"CreateTask":   SubOpOrchestratorCreateTask,
	"GetTask":      SubOpOrchestratorGetTask,
	"ListTasks":    SubOpOrchestratorListTasks,
	"ClaimTask":    SubOpOrchestratorClaimTask,
	"CompleteTask": SubOpOrchestratorCompleteTask,
	"ListBackends": SubOpOrchestratorListBackends,
	"Health":       SubOpOrchestratorHealth,
	"Inference":    SubOpOrchestratorInference,
}

// OrchestratorMethodToSubOp converts an Orchestrator method name to its sub-operation code.
// Returns false if the method is not a known Orchestrator sub-operation.
func OrchestratorMethodToSubOp(method string) (byte, bool) {
	subOp, ok := orchestratorMethodToSubOp[method]
	return subOp, ok
}

// Pooled provider account sub-operation codes. They ride the SAME
// OpOrchestrator (0x8F) dispatch byte in a disjoint 0x30-0x38 range; the
// server resolves them in handleOrchestratorDispatch and authorizes them
// against credentials:* rather than orchestrator:*. These MUST match
// pkg/api/quic/protocol.go SubOpAccount* byte-for-byte.
const (
	SubOpAccountCreate     byte = 0x30
	SubOpAccountGet        byte = 0x31
	SubOpAccountList       byte = 0x32
	SubOpAccountUpdate     byte = 0x33
	SubOpAccountDelete     byte = 0x34
	SubOpAccountRotate     byte = 0x35
	SubOpAccountRevoke     byte = 0x36
	SubOpAccountSetDefault byte = 0x37
	SubOpAccountUsage      byte = 0x38
)

// accountMethodToSubOp maps SDK Account method names to their sub-operation
// codes for O(1) lookup.
var accountMethodToSubOp = map[string]byte{
	"Create":           SubOpAccountCreate,
	"Get":              SubOpAccountGet,
	"List":             SubOpAccountList,
	"Update":           SubOpAccountUpdate,
	"Delete":           SubOpAccountDelete,
	"RotateCredential": SubOpAccountRotate,
	"Revoke":           SubOpAccountRevoke,
	"SetDefault":       SubOpAccountSetDefault,
	"Usage":            SubOpAccountUsage,
}

// AccountMethodToSubOp converts an Account method name to its sub-operation
// code. Returns false when the method is not a known account sub-operation.
func AccountMethodToSubOp(method string) (byte, bool) {
	subOp, ok := accountMethodToSubOp[method]
	return subOp, ok
}

// Project sub-operation codes. They ride the SAME OpOrchestrator (0x8F)
// dispatch byte in a disjoint 0x40-0x47 range; the server resolves them in
// handleOrchestratorDispatch and authorizes them against project:* rather than
// orchestrator:*. These MUST match pkg/api/quic/protocol.go SubOpProject*
// byte-for-byte.
const (
	SubOpProjectCreate          byte = 0x40
	SubOpProjectGet             byte = 0x41
	SubOpProjectGetByName       byte = 0x42
	SubOpProjectList            byte = 0x43
	SubOpProjectUpdate          byte = 0x44
	SubOpProjectDelete          byte = 0x45
	SubOpProjectGrantAgentRole  byte = 0x46
	SubOpProjectRevokeAgentRole byte = 0x47
)

// projectMethodToSubOp maps SDK Project method names to their sub-operation
// codes for O(1) lookup.
var projectMethodToSubOp = map[string]byte{
	"Create":          SubOpProjectCreate,
	"Get":             SubOpProjectGet,
	"GetByName":       SubOpProjectGetByName,
	"List":            SubOpProjectList,
	"Update":          SubOpProjectUpdate,
	"Delete":          SubOpProjectDelete,
	"GrantAgentRole":  SubOpProjectGrantAgentRole,
	"RevokeAgentRole": SubOpProjectRevokeAgentRole,
}

// ProjectMethodToSubOp converts a Project method name to its sub-operation
// code. Returns false when the method is not a known project sub-operation.
func ProjectMethodToSubOp(method string) (byte, bool) {
	subOp, ok := projectMethodToSubOp[method]
	return subOp, ok
}

// SSH sub-operation codes. They ride the SAME OpOrchestrator (0x8F) dispatch
// byte in a disjoint 0x50-0x5D range; the server resolves them in
// handleOrchestratorDispatch and authorizes them against keys:* rather than
// orchestrator:*. These MUST match pkg/api/quic/protocol.go SubOpSSH*
// byte-for-byte.
const (
	SubOpSSHGenerateKey         byte = 0x50
	SubOpSSHImportKey           byte = 0x51
	SubOpSSHGetKey              byte = 0x52
	SubOpSSHListKeys            byte = 0x53
	SubOpSSHDeleteKey           byte = 0x54
	SubOpSSHExportPublicKey     byte = 0x55
	SubOpSSHSignDigest          byte = 0x56
	SubOpSSHAddKnownHost        byte = 0x57
	SubOpSSHListKnownHosts      byte = 0x58
	SubOpSSHRemoveKnownHost     byte = 0x59
	SubOpSSHAddAuthorizedKey    byte = 0x5A
	SubOpSSHListAuthorizedKeys  byte = 0x5B
	SubOpSSHRemoveAuthorizedKey byte = 0x5C
	SubOpSSHEmitAuthorizedKey   byte = 0x5D
)

// sshMethodToSubOp maps SDK SSH method names to their sub-operation codes for
// O(1) lookup.
var sshMethodToSubOp = map[string]byte{
	sshclient.MethodGenerateKey:         SubOpSSHGenerateKey,
	sshclient.MethodImportKey:           SubOpSSHImportKey,
	sshclient.MethodGetKey:              SubOpSSHGetKey,
	sshclient.MethodListKeys:            SubOpSSHListKeys,
	sshclient.MethodDeleteKey:           SubOpSSHDeleteKey,
	sshclient.MethodExportPublicKey:     SubOpSSHExportPublicKey,
	sshclient.MethodSignDigest:          SubOpSSHSignDigest,
	sshclient.MethodAddKnownHost:        SubOpSSHAddKnownHost,
	sshclient.MethodListKnownHosts:      SubOpSSHListKnownHosts,
	sshclient.MethodRemoveKnownHost:     SubOpSSHRemoveKnownHost,
	sshclient.MethodAddAuthorizedKey:    SubOpSSHAddAuthorizedKey,
	sshclient.MethodListAuthorizedKeys:  SubOpSSHListAuthorizedKeys,
	sshclient.MethodRemoveAuthorizedKey: SubOpSSHRemoveAuthorizedKey,
	sshclient.MethodEmitAuthorizedKey:   SubOpSSHEmitAuthorizedKey,
}

// SSHMethodToSubOp converts an SSH method name to its sub-operation code.
// Returns false when the method is not a known SSH sub-operation.
func SSHMethodToSubOp(method string) (byte, bool) {
	subOp, ok := sshMethodToSubOp[method]
	return subOp, ok
}

// IAMSubRequest wraps a sub-operation code with the request payload.
// Used by MFA and other IAM operations that use the sub-operation dispatch pattern.
type IAMSubRequest struct {
	SubOp   byte            `json:"subop"`
	Payload json.RawMessage `json:"payload"`
}

// mfaMethodToSubOp maps MFA method names to sub-operation codes for O(1) lookup.
var mfaMethodToSubOp = map[string]byte{
	// SDK client method names (used by quicbaas.IAM.MFA service)
	"GetMFAStatus":          SubOpMFAGetStatus,
	"BeginMFAEnrollment":    SubOpMFABeginEnroll,
	"CompleteMFAEnrollment": SubOpMFACompleteEnroll,
	"DisableMFA":            SubOpMFADisable,
	"GetBackupCodeStatus":   SubOpMFAGetBackupCodeStatus,
	"GenerateBackupCodes":   SubOpMFAGenerateBackupCodes,
	"CancelMFAEnrollment":   SubOpMFACancelEnroll,
	"BeginMFAChallenge":     SubOpMFABeginChallenge,
	"VerifyMFAChallenge":    SubOpMFAVerifyChallenge,
	"VerifyBackupCode":      SubOpMFAVerifyBackupCode,

	// Integration test method names (used by IAM.MFA service)
	"GetStatus":             SubOpMFAGetStatus,
	"BeginEnrollment":       SubOpMFABeginEnroll,
	"CompleteEnrollment":    SubOpMFACompleteEnroll,
	"CancelEnrollment":      SubOpMFACancelEnroll,
	"ValidateCode":          SubOpMFAVerifyChallenge,
	"GenerateRecoveryCodes": SubOpMFAGenerateBackupCodes,
	"Disable":               SubOpMFADisable,
	"BeginChallenge":        SubOpMFABeginChallenge,
	"VerifyChallenge":       SubOpMFAVerifyChallenge,
	"GetBackupCodes":        SubOpMFAGetBackupCodeStatus,
}

// MFAMethodToSubOp converts an MFA method name to a sub-operation code.
func MFAMethodToSubOp(method string) (byte, bool) {
	subOp, ok := mfaMethodToSubOp[method]
	return subOp, ok
}

// Scheduler sub-operation codes (used in payload[0] when OpScheduler is the
// main opcode). Must stay in lock-step with pkg/api/quic/protocol.go.
// Sub-opcode range: 0x01-0x1F (the server reserves 0x20-0x2F for messaging and
// 0x40-0x4F for extended vector ops on the same OpScheduler=0x00 dispatch byte).
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

// schedulerMethodToSubOp maps Scheduler service methods to their sub-operation
// codes for O(1) lookup. The wire opcode is OpScheduler (0x00) for every entry;
// the SubOp byte is prepended to the JSON request payload before sending, and
// the server's handleSchedulerDispatch reads payload[0] then unmarshals the
// remaining bytes as the request body.
var schedulerMethodToSubOp = map[string]byte{
	"CreateJob":     SubOpSchedulerCreateJob,
	"GetJob":        SubOpSchedulerGetJob,
	"UpdateJob":     SubOpSchedulerUpdateJob,
	"DeleteJob":     SubOpSchedulerDeleteJob,
	"ListJobs":      SubOpSchedulerListJobs,
	"TriggerJob":    SubOpSchedulerTriggerJob,
	"PauseJob":      SubOpSchedulerPauseJob,
	"ResumeJob":     SubOpSchedulerResumeJob,
	"CancelJob":     SubOpSchedulerCancelJob,
	"GetJobHistory": SubOpSchedulerGetJobHistory,
	"GetStatus":     SubOpSchedulerGetStatus,
}

// SchedulerMethodToSubOp converts a Scheduler method name to a sub-operation code.
// Returns false if the method is not a known Scheduler sub-operation.
func SchedulerMethodToSubOp(method string) (byte, bool) {
	subOp, ok := schedulerMethodToSubOp[method]
	return subOp, ok
}

// Messaging sub-operation codes (used in payload[0] when OpMsg is the main
// opcode). Must stay in lock-step with pkg/api/quic/handler_messaging.go.
// Sub-opcode range: 0x20-0x2F (disjoint from scheduler 0x01-0x1F and
// extended-vector 0x40-0x4F on the same OpMsg=0x00 dispatch byte).
const (
	SubOpMsgCreateChannel byte = 0x20
	SubOpMsgDeleteChannel byte = 0x21
	SubOpMsgGetChannel    byte = 0x22
	SubOpMsgListChannels  byte = 0x23
	SubOpMsgSubscribe     byte = 0x24
	SubOpMsgUnsubscribe   byte = 0x25
	SubOpMsgPublish       byte = 0x26
	SubOpMsgPublishDelta  byte = 0x27
	SubOpMsgGetStats      byte = 0x28
	SubOpMsgDedupStats    byte = 0x29
)

// messagingMethodToSubOp maps Messaging service methods to their sub-operation
// codes for O(1) lookup. The wire opcode is OpMsg (0x00) for every entry; the
// SubOp byte is prepended to the JSON request payload before sending.
//
// GetChannelStats and GetDedupStats both route to SubOpMsgDedupStats: the server
// derives channel statistics from the deduplication store, and GetDedupStats is
// the only stats-by-channel sub-op the QUIC handler exposes.
var messagingMethodToSubOp = map[string]byte{
	"CreateChannel":   SubOpMsgCreateChannel,
	"DeleteChannel":   SubOpMsgDeleteChannel,
	"GetChannel":      SubOpMsgGetChannel,
	"ListChannels":    SubOpMsgListChannels,
	"Subscribe":       SubOpMsgSubscribe,
	"Unsubscribe":     SubOpMsgUnsubscribe,
	"Publish":         SubOpMsgPublish,
	"PublishDelta":    SubOpMsgPublishDelta,
	"GetStats":        SubOpMsgGetStats,
	"GetDedupStats":   SubOpMsgDedupStats,
	"GetChannelStats": SubOpMsgDedupStats,
}

// MessagingMethodToSubOp converts a Messaging method name to a sub-operation code.
// Returns false if the method is not a known Messaging sub-operation.
func MessagingMethodToSubOp(method string) (byte, bool) {
	subOp, ok := messagingMethodToSubOp[method]
	return subOp, ok
}

// Barrier sub-operation codes (used in payload[0] when OpBarrier is the
// main opcode). Must stay in lock-step with pkg/api/quic/protocol.go.
const (
	SubOpBarrierStatus             byte = 0x01
	SubOpBarrierInitialize         byte = 0x02
	SubOpBarrierUnseal             byte = 0x03
	SubOpBarrierSeal               byte = 0x04
	SubOpBarrierShamirInitialize   byte = 0x05
	SubOpBarrierShamirUnsealShare  byte = 0x06
	SubOpBarrierShamirUnsealShares byte = 0x07
	// 0x08-0x0B retired with persisted Shamir share management; left unassigned.
	SubOpBarrierRekey            byte = 0x0C
	SubOpBarrierRecoveryGenerate byte = 0x0D
	SubOpBarrierRecoveryRecover  byte = 0x0E
	SubOpBarrierRecoveryDelete   byte = 0x0F
	SubOpBarrierRecoveryStatus   byte = 0x10
	SubOpBarrierRootToken        byte = 0x11
)

// Ceremony sub-operation codes (used in payload[0] when OpCeremony is the
// main opcode). Must stay in lock-step with pkg/api/quic/protocol.go.
const (
	SubOpCeremonyInitialize            byte = 0x01
	SubOpCeremonyStatus                byte = 0x02
	SubOpCeremonyClaimCertBegin        byte = 0x03
	SubOpCeremonyClaimCertComplete     byte = 0x04
	SubOpCeremonyBeginClaimShare       byte = 0x05
	SubOpCeremonySignCSR               byte = 0x06
	SubOpCeremonyListOfficers          byte = 0x07
	SubOpCeremonyEnrollExternalOfficer byte = 0x08
	SubOpCeremonyCompleteClaimShare    byte = 0x09
)

// barrierMethodToSubOp maps Barrier service methods to their sub-operation
// codes for O(1) lookup. The wire opcode is OpBarrier (0xC0) for every
// entry; the SubOp byte is prepended to the JSON payload before sending.
var barrierMethodToSubOp = map[string]byte{
	"BarrierStatus":             SubOpBarrierStatus,
	"BarrierInitialize":         SubOpBarrierInitialize,
	"BarrierUnseal":             SubOpBarrierUnseal,
	"BarrierSeal":               SubOpBarrierSeal,
	"BarrierShamirInitialize":   SubOpBarrierShamirInitialize,
	"BarrierShamirUnsealShare":  SubOpBarrierShamirUnsealShare,
	"BarrierShamirUnsealShares": SubOpBarrierShamirUnsealShares,
	"BarrierRekey":              SubOpBarrierRekey,
	"BarrierRecoveryGenerate":   SubOpBarrierRecoveryGenerate,
	"BarrierRecoveryRecover":    SubOpBarrierRecoveryRecover,
	"BarrierRecoveryDelete":     SubOpBarrierRecoveryDelete,
	"BarrierRecoveryStatus":     SubOpBarrierRecoveryStatus,
	"BarrierRootToken":          SubOpBarrierRootToken,
}

// BarrierMethodToSubOp converts a Barrier method name to a sub-operation code.
// Certificate extension sub-operation codes. The CrossSign / Chain /
// IssueDeviceIdentity verbs behind Security().CertificateExt() ride the
// OpOrchestrator (0x8F) dispatch byte in their own disjoint 0x60-0x62 range,
// the same "shared opcode, disjoint sub-op range" arrangement accounts
// (0x30-0x38), projects (0x40-0x47) and SSH (0x50-0x5D) use. The top-level
// opcode space is fully allocated — note that 0x60 is still OpAuthLogin here.
//
// Cross-reference: pkg/api/quic/protocol.go SubOpCertExt* — the two files MUST
// stay in sync.
const (
	SubOpCertExtCrossSign           byte = 0x60
	SubOpCertExtChain               byte = 0x61
	SubOpCertExtIssueDeviceIdentity byte = 0x62
)

// certExtMethodToSubOp maps the Certificate service extension methods to their
// sub-operation codes for O(1) lookup. The wire opcode is OpOrchestrator.
var certExtMethodToSubOp = map[string]byte{
	"CrossSign":           SubOpCertExtCrossSign,
	"Chain":               SubOpCertExtChain,
	"IssueDeviceIdentity": SubOpCertExtIssueDeviceIdentity,
}

// CertExtMethodToSubOp returns the sub-operation code for a certificate
// extension method, and whether the method rides the OpCertExt dispatch byte.
// The six base certificate verbs have their own top-level opcodes and return
// ok=false here.
func CertExtMethodToSubOp(method string) (byte, bool) {
	subOp, ok := certExtMethodToSubOp[method]
	return subOp, ok
}

// Attestation sub-operation codes. The Verify / VerifyChain / AttestContainer
// verbs behind Security().Attestation() ride the OpOrchestrator (0x8F)
// dispatch byte in their own disjoint 0x70-0x72 range, the same "shared
// opcode, disjoint sub-op range" arrangement accounts (0x30-0x38), projects
// (0x40-0x47), SSH (0x50-0x5D) and certificate extensions (0x60-0x62) use.
//
// Cross-reference: pkg/api/quic/protocol.go SubOpAttestation* — the two files
// MUST stay in sync.
const (
	SubOpAttestationVerify          byte = 0x70
	SubOpAttestationVerifyChain     byte = 0x71
	SubOpAttestationAttestContainer byte = 0x72
)

// attestationMethodToSubOp maps Attestation service methods to their
// sub-operation codes for O(1) lookup. The wire opcode is OpOrchestrator.
var attestationMethodToSubOp = map[string]byte{
	"Verify":          SubOpAttestationVerify,
	"VerifyChain":     SubOpAttestationVerifyChain,
	"AttestContainer": SubOpAttestationAttestContainer,
}

// AttestationMethodToSubOp returns the sub-operation code for an attestation
// method and whether the method is wired over QUIC.
func AttestationMethodToSubOp(method string) (byte, bool) {
	subOp, ok := attestationMethodToSubOp[method]
	return subOp, ok
}

// PIV sub-operation codes. The eight backend-scoped PIV verbs behind
// Security().Auth().PIV() — the go-xkms PIVService contract verbatim — ride
// the OpOrchestrator (0x8F) dispatch byte in their own disjoint 0x80-0x87
// range.
//
// Cross-reference: pkg/api/quic/protocol.go SubOpPIV* — the two files MUST
// stay in sync.
const (
	SubOpPIVListSlots   byte = 0x80
	SubOpPIVGetCert     byte = 0x81
	SubOpPIVStoreCert   byte = 0x82
	SubOpPIVDeleteCert  byte = 0x83
	SubOpPIVGenerateKey byte = 0x84
	SubOpPIVImportCert  byte = 0x85
	SubOpPIVExportCert  byte = 0x86
	SubOpPIVGenerateCSR byte = 0x87
)

// pivMethodToSubOp maps PIV service methods to their sub-operation codes for
// O(1) lookup. The wire opcode is OpOrchestrator. The method vocabulary is
// the xkms PIVService interface verbatim.
var pivMethodToSubOp = map[string]byte{
	"ListPIVSlots":         SubOpPIVListSlots,
	"GetPIVCertificate":    SubOpPIVGetCert,
	"StorePIVCertificate":  SubOpPIVStoreCert,
	"DeletePIVCertificate": SubOpPIVDeleteCert,
	"GeneratePIVKey":       SubOpPIVGenerateKey,
	"ImportPIVCertificate": SubOpPIVImportCert,
	"ExportPIVCertificate": SubOpPIVExportCert,
	"GeneratePIVCSR":       SubOpPIVGenerateCSR,
}

// PIVMethodToSubOp returns the sub-operation code for a PIV method and
// whether the method is wired over QUIC.
func PIVMethodToSubOp(method string) (byte, bool) {
	subOp, ok := pivMethodToSubOp[method]
	return subOp, ok
}

// FIDO2 sub-operation codes. The four tenant-scoped WebAuthn ceremony verbs
// behind Security().Auth().FIDO2() ride the OpOrchestrator (0x8F) dispatch
// byte in their own disjoint 0x90-0x93 range. The tenant scope travels as a
// "tenant_id" field injected into the JSON body from the request context
// (transport.TenantFromContext), the same enrichment the orchestrator verbs
// use.
//
// Cross-reference: pkg/api/quic/protocol.go SubOpFIDO2* — the two files MUST
// stay in sync.
const (
	SubOpFIDO2BeginRegistration    byte = 0x90
	SubOpFIDO2FinishRegistration   byte = 0x91
	SubOpFIDO2BeginAuthentication  byte = 0x92
	SubOpFIDO2FinishAuthentication byte = 0x93
)

// fido2MethodToSubOp maps FIDO2 service methods to their sub-operation codes
// for O(1) lookup. The wire opcode is OpOrchestrator. The method vocabulary
// is the xkms FIDO2Service interface verbatim.
var fido2MethodToSubOp = map[string]byte{
	"BeginRegistration":    SubOpFIDO2BeginRegistration,
	"FinishRegistration":   SubOpFIDO2FinishRegistration,
	"BeginAuthentication":  SubOpFIDO2BeginAuthentication,
	"FinishAuthentication": SubOpFIDO2FinishAuthentication,
}

// FIDO2MethodToSubOp returns the sub-operation code for a FIDO2 method and
// whether the method is wired over QUIC.
func FIDO2MethodToSubOp(method string) (byte, bool) {
	subOp, ok := fido2MethodToSubOp[method]
	return subOp, ok
}

func BarrierMethodToSubOp(method string) (byte, bool) {
	subOp, ok := barrierMethodToSubOp[method]
	return subOp, ok
}

// ceremonyMethodToSubOp maps Ceremony service methods to their sub-operation
// codes for O(1) lookup. The wire opcode is OpCeremony (0xC1).
var ceremonyMethodToSubOp = map[string]byte{
	"CeremonyInitialize":            SubOpCeremonyInitialize,
	"CeremonyStatus":                SubOpCeremonyStatus,
	"CeremonyClaimCertBegin":        SubOpCeremonyClaimCertBegin,
	"CeremonyClaimCertComplete":     SubOpCeremonyClaimCertComplete,
	"CeremonyBeginClaimShare":       SubOpCeremonyBeginClaimShare,
	"CeremonyCompleteClaimShare":    SubOpCeremonyCompleteClaimShare,
	"CeremonySignCSR":               SubOpCeremonySignCSR,
	"CeremonyListOfficers":          SubOpCeremonyListOfficers,
	"CeremonyEnrollExternalOfficer": SubOpCeremonyEnrollExternalOfficer,
}

// CeremonyMethodToSubOp converts a Ceremony method name to a sub-operation code.
func CeremonyMethodToSubOp(method string) (byte, bool) {
	subOp, ok := ceremonyMethodToSubOp[method]
	return subOp, ok
}

// Request represents a QUIC protocol request.
// Wire format: [opcode:1][tokenLen:2][token:N][payloadLen:4][payload:M].
type Request struct {
	Opcode  byte
	Token   string
	Payload []byte
}

// Response represents a QUIC protocol response.
// Wire format: [status:1][payloadLen:4][payload:N].
type Response struct {
	Status  byte
	Payload []byte
}

// WriteRequest writes a request to the stream.
func WriteRequest(w io.Writer, req *Request) error {
	if req == nil {
		return errors.NewInvalidInput("quic.WriteRequest", "request cannot be nil")
	}

	// Validate lengths before narrowing conversions so oversized inputs
	// cannot wrap around the fixed-width wire length prefixes.
	if len(req.Payload) > int(MaxPayloadSize) {
		return errors.NewInvalidInput("quic.WriteRequest",
			fmt.Sprintf("payload size %d exceeds maximum %d", len(req.Payload), MaxPayloadSize))
	}
	payloadLen := uint32(len(req.Payload)) //#nosec G115 -- bounds checked above

	if len(req.Token) > int(MaxTokenSize) {
		return errors.NewInvalidInput("quic.WriteRequest",
			fmt.Sprintf("token size %d exceeds maximum %d", len(req.Token), MaxTokenSize))
	}
	tokenLen := uint16(len(req.Token)) //#nosec G115 -- bounds checked above

	// Write opcode
	if _, err := w.Write([]byte{req.Opcode}); err != nil {
		return errors.Wrap(err, errors.CodeTransport, "quic.WriteRequest",
			"failed to write opcode")
	}

	// Write token length (big-endian, 2 bytes)
	tokenLenBuf := make([]byte, 2)
	binary.BigEndian.PutUint16(tokenLenBuf, tokenLen)
	if _, err := w.Write(tokenLenBuf); err != nil {
		return errors.Wrap(err, errors.CodeTransport, "quic.WriteRequest",
			"failed to write token length")
	}

	// Write token
	if tokenLen > 0 {
		if _, err := w.Write([]byte(req.Token)); err != nil {
			return errors.Wrap(err, errors.CodeTransport, "quic.WriteRequest",
				"failed to write token")
		}
	}

	// Write payload length (big-endian)
	lenBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(lenBuf, payloadLen)
	if _, err := w.Write(lenBuf); err != nil {
		return errors.Wrap(err, errors.CodeTransport, "quic.WriteRequest",
			"failed to write payload length")
	}

	// Write payload
	if payloadLen > 0 {
		if _, err := w.Write(req.Payload); err != nil {
			return errors.Wrap(err, errors.CodeTransport, "quic.WriteRequest",
				"failed to write payload")
		}
	}

	return nil
}

// ReadRequest reads a request from the stream.
func ReadRequest(r io.Reader) (*Request, error) {
	// Read opcode
	opcodeBuf := make([]byte, 1)
	if _, err := io.ReadFull(r, opcodeBuf); err != nil {
		return nil, errors.Wrap(err, errors.CodeTransport, "quic.ReadRequest",
			"failed to read opcode")
	}

	// Read token length
	tokenLenBuf := make([]byte, 2)
	if _, err := io.ReadFull(r, tokenLenBuf); err != nil {
		return nil, errors.Wrap(err, errors.CodeTransport, "quic.ReadRequest",
			"failed to read token length")
	}
	tokenLen := binary.BigEndian.Uint16(tokenLenBuf)

	// Validate token length. With MaxTokenSize at the ceiling of the 2-byte
	// wire prefix this cannot trigger; it stays as defense in depth should
	// the bound ever be lowered below the prefix range.
	if int(tokenLen) > int(MaxTokenSize) {
		return nil, errors.NewInvalidInput("quic.ReadRequest",
			"token size exceeds maximum")
	}

	// Read token
	var token string
	if tokenLen > 0 {
		tokenBuf := make([]byte, tokenLen)
		if _, err := io.ReadFull(r, tokenBuf); err != nil {
			return nil, errors.Wrap(err, errors.CodeTransport, "quic.ReadRequest",
				"failed to read token")
		}
		token = string(tokenBuf)
	}

	// Read payload length
	lenBuf := make([]byte, 4)
	if _, err := io.ReadFull(r, lenBuf); err != nil {
		return nil, errors.Wrap(err, errors.CodeTransport, "quic.ReadRequest",
			"failed to read payload length")
	}
	payloadLen := binary.BigEndian.Uint32(lenBuf)

	// Validate payload length
	if payloadLen > MaxPayloadSize {
		return nil, errors.NewInvalidInput("quic.ReadRequest",
			"payload size exceeds maximum")
	}

	// Read payload
	var payload []byte
	if payloadLen > 0 {
		payload = make([]byte, payloadLen)
		if _, err := io.ReadFull(r, payload); err != nil {
			return nil, errors.Wrap(err, errors.CodeTransport, "quic.ReadRequest",
				"failed to read payload")
		}
	}

	return &Request{
		Opcode:  opcodeBuf[0],
		Token:   token,
		Payload: payload,
	}, nil
}

// WriteResponse writes a response to the stream.
func WriteResponse(w io.Writer, resp *Response) error {
	if resp == nil {
		return errors.NewInvalidInput("quic.WriteResponse", "response cannot be nil")
	}

	payloadLen := uint32(len(resp.Payload))
	if payloadLen > MaxPayloadSize {
		return errors.NewInvalidInput("quic.WriteResponse",
			"payload size exceeds maximum")
	}

	// Write status
	if _, err := w.Write([]byte{resp.Status}); err != nil {
		return errors.Wrap(err, errors.CodeTransport, "quic.WriteResponse",
			"failed to write status")
	}

	// Write payload length (big-endian)
	lenBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(lenBuf, payloadLen)
	if _, err := w.Write(lenBuf); err != nil {
		return errors.Wrap(err, errors.CodeTransport, "quic.WriteResponse",
			"failed to write payload length")
	}

	// Write payload
	if payloadLen > 0 {
		if _, err := w.Write(resp.Payload); err != nil {
			return errors.Wrap(err, errors.CodeTransport, "quic.WriteResponse",
				"failed to write payload")
		}
	}

	return nil
}

// ReadResponse reads a response from the stream.
func ReadResponse(r io.Reader) (*Response, error) {
	// Read status
	statusBuf := make([]byte, 1)
	if _, err := io.ReadFull(r, statusBuf); err != nil {
		return nil, errors.Wrap(err, errors.CodeTransport, "quic.ReadResponse",
			"failed to read status")
	}

	// Read payload length
	lenBuf := make([]byte, 4)
	if _, err := io.ReadFull(r, lenBuf); err != nil {
		return nil, errors.Wrap(err, errors.CodeTransport, "quic.ReadResponse",
			"failed to read payload length")
	}
	payloadLen := binary.BigEndian.Uint32(lenBuf)

	// Validate payload length
	if payloadLen > MaxPayloadSize {
		return nil, errors.NewInvalidInput("quic.ReadResponse",
			"payload size exceeds maximum")
	}

	// Read payload
	var payload []byte
	if payloadLen > 0 {
		payload = make([]byte, payloadLen)
		if _, err := io.ReadFull(r, payload); err != nil {
			return nil, errors.Wrap(err, errors.CodeTransport, "quic.ReadResponse",
				"failed to read payload")
		}
	}

	return &Response{
		Status:  statusBuf[0],
		Payload: payload,
	}, nil
}

// EncodePayload encodes an entity to JSON bytes.
// Uses JSON to match the server-side QUIC protocol implementation.
func EncodePayload(entity interface{}) ([]byte, error) {
	if entity == nil {
		return nil, nil
	}

	buf, err := json.Marshal(entity)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "quic.EncodePayload",
			"failed to encode payload")
	}

	return buf, nil
}

// DecodePayload decodes JSON bytes into an entity.
// Uses JSON to match the server-side QUIC protocol implementation.
func DecodePayload(data []byte, entity interface{}) error {
	if len(data) == 0 {
		return nil
	}

	if entity == nil {
		return errors.NewInvalidInput("quic.DecodePayload", "entity cannot be nil")
	}

	if err := json.Unmarshal(data, entity); err != nil {
		return errors.Wrap(err, errors.CodeInternal, "quic.DecodePayload",
			"failed to decode payload")
	}

	return nil
}

// StatusToError converts a status code to a QBError.
func StatusToError(status byte, message string) error {
	if status == StatusOK {
		return nil
	}

	var code errors.ErrorCode
	switch status {
	case StatusNotFound:
		code = errors.CodeNotFound
	case StatusUnauthorized:
		code = errors.CodeUnauthorized
	case StatusInvalidArgument, StatusInvalidRequest:
		code = errors.CodeInvalidInput
	case StatusAlreadyExists:
		code = errors.CodeAlreadyExists
	case StatusTimeout:
		code = errors.CodeTimeout
	case StatusPermissionDenied:
		code = errors.CodeForbidden
	case StatusResourceExhausted:
		code = errors.CodeResourceExhausted
	case StatusUnavailable:
		code = errors.CodeUnavailable
	default:
		code = errors.CodeInternal
	}

	return errors.New(code, "quic.protocol", message)
}

// ErrorToStatus converts a QBError to a status code.
func ErrorToStatus(err error) byte {
	if err == nil {
		return StatusOK
	}

	code := errors.Code(err)
	switch code {
	case errors.CodeNotFound:
		return StatusNotFound
	case errors.CodeUnauthorized:
		return StatusUnauthorized
	case errors.CodeInvalidInput:
		return StatusInvalidArgument
	case errors.CodeAlreadyExists:
		return StatusAlreadyExists
	case errors.CodeTimeout:
		return StatusTimeout
	case errors.CodeForbidden:
		return StatusPermissionDenied
	case errors.CodeResourceExhausted:
		return StatusResourceExhausted
	case errors.CodeUnavailable:
		return StatusUnavailable
	default:
		return StatusInternalError
	}
}

// methodToOpcode maps service/method combinations to opcodes for O(1) lookup.
// Supports both legacy format (QuicBaaS/Put) and new format (quicbaas.KV/Put).
var methodToOpcode = map[string]byte{
	// ===========================================
	// Legacy format (backward compatibility)
	// ===========================================

	// KV Operations (legacy)
	"QuicBaaS/Put":             OpPut,
	"QuicBaaS/Get":             OpGet,
	"QuicBaaS/Delete":          OpDelete,
	"QuicBaaS/List":            OpList,
	"QuicBaaS/ListEntityTypes": OpListEntityTypes,
	"QuicBaaS/Watch":           OpWatch,

	// Batch Operations (legacy)
	"QuicBaaS/BatchPut":    OpBatchPut,
	"QuicBaaS/BatchGet":    OpBatchGet,
	"QuicBaaS/BatchDelete": OpBatchDelete,

	// Transaction Operations (legacy)
	"QuicBaaS/Transaction": OpTransaction,

	// Query Operations (legacy)
	"QuicBaaS/Query":     OpQuery,
	"QuicBaaS/ScanIndex": OpScanIndex,

	// Cluster Operations (legacy)
	"Cluster/Status":             OpClusterStatus,
	"Cluster/GetLeader":          OpGetLeader,
	"Cluster/AddNode":            OpAddNode,
	"Cluster/RemoveNode":         OpRemoveNode,
	"Cluster/CreateShard":        OpCreateShard,
	"Cluster/Rebalance":          OpRebalanceCluster,
	"Cluster/ListNodes":          OpListNodes,
	"Cluster/GetRebalanceStatus": OpRebalanceStatus,

	// Embedding Operations (legacy)
	"Embedding/Generate":      OpEmbeddingGenerate,
	"Embedding/BatchGenerate": OpEmbeddingBatchGenerate,
	"Embedding/ListProviders": OpEmbeddingListProviders,
	"Embedding/GetProvider":   OpEmbeddingGetProvider,
	"Embedding/SetDefault":    OpEmbeddingSetDefault,
	"Embedding/GetCacheStats": OpEmbeddingGetCacheStats,
	"Embedding/ClearCache":    OpEmbeddingClearCache,

	// Health Operations (legacy)
	"Health/Check":     OpHealth,
	"Health/Liveness":  OpLiveness,
	"Health/Readiness": OpReadiness,

	// Backup Operations (legacy)
	"Backup/Create":  OpBackupCreate,
	"Backup/List":    OpBackupList,
	"Backup/Restore": OpBackupRestore,
	"Backup/Delete":  OpBackupDelete,
	"Backup/Verify":  OpBackupVerify,

	// Auth Operations (legacy)
	"Auth/Login":         OpAuthLogin,
	"Auth/Logout":        OpAuthLogout,
	"Auth/RefreshToken":  OpAuthRefreshToken,
	"Auth/ValidateToken": OpAuthValidateToken,

	// RBAC Operations (legacy)
	"RBAC/CreateUser": OpRBACCreateUser,
	"RBAC/DeleteUser": OpRBACDeleteUser,
	"RBAC/ListUsers":  OpRBACListUsers,
	"RBAC/CreateRole": OpRBACCreateRole,
	"RBAC/DeleteRole": OpRBACDeleteRole,
	"RBAC/ListRoles":  OpRBACListRoles,
	"RBAC/AssignRole": OpRBACAssignRole,
	"RBAC/RevokeRole": OpRBACRevokeRole,

	// Cache Operations (legacy)
	"Cache/GetStats":     OpCacheGetStats,
	"Cache/Clear":        OpCacheClear,
	"Cache/GetConfig":    OpCacheGetConfig,
	"Cache/UpdateConfig": OpCacheUpdateConfig,

	// TimeSeries Operations (legacy)
	"TimeSeries/WritePoint":         OpTSWritePoint,
	"TimeSeries/WriteBatch":         OpTSWriteBatch,
	"TimeSeries/Query":              OpTSQuery,
	"TimeSeries/Aggregate":          OpAggregate,
	"TimeSeries/DeleteSeries":       OpTSDeleteSeries,
	"TimeSeries/GetRetentionPolicy": OpTSGetRetentionPolicy,
	"TimeSeries/SetRetentionPolicy": OpTSSetRetentionPolicy,
	"TimeSeries/ListMetrics":        OpTSListMetrics,

	// Certificate Operations (legacy)
	"Certificate/IssueCertificate":  OpCertIssue,
	"Certificate/SignCSR":           OpCertSignCSR,
	"Certificate/RevokeCertificate": OpCertRevoke,
	"Certificate/VerifyCertificate": OpCertVerify,
	"Certificate/GetCACertificate":  OpCertGetCA,
	"Certificate/GetTLSConfig":      OpCertGetTLSConfig,

	// Certificate extensions ride the OpOrchestrator dispatch byte with the
	// sub-op in payload[0]; see CertExtMethodToSubOp.
	"Certificate/CrossSign":           OpOrchestrator,
	"Certificate/Chain":               OpOrchestrator,
	"Certificate/IssueDeviceIdentity": OpOrchestrator,

	// Attestation rides the OpOrchestrator dispatch byte with the sub-op in
	// payload[0]; see AttestationMethodToSubOp.
	"Attestation/Verify":          OpOrchestrator,
	"Attestation/VerifyChain":     OpOrchestrator,
	"Attestation/AttestContainer": OpOrchestrator,

	// Trust Operations (legacy)
	"Trust/CrossSignedRootPool": OpTrustCrossSignedRootPool,
	"Trust/AutoImportAIA":       OpTrustAutoImportAIA,
	"Trust/PublishDANE":         OpTrustPublishDANE,

	// ObjectStorage Operations (legacy)
	"ObjectStorage/PutObject":       OpOSPutObject,
	"ObjectStorage/GetObject":       OpOSGetObject,
	"ObjectStorage/DeleteObject":    OpOSDeleteObject,
	"ObjectStorage/HeadObject":      OpOSHeadObject,
	"ObjectStorage/ListObjects":     OpOSListObjects,
	"ObjectStorage/ListByTag":       OpOSListByTag,
	"ObjectStorage/ListByTenant":    OpOSListByTenant,
	"ObjectStorage/CopyObject":      OpOSCopyObject,
	"ObjectStorage/ReplicateObject": OpOSReplicateObject,
	"ObjectStorage/MigrateObject":   OpOSMigrateObject,
	"ObjectStorage/ArchiveObject":   OpOSArchiveObject,
	"ObjectStorage/RestoreObject":   OpOSRestoreObject,
	"ObjectStorage/VerifyObject":    OpOSVerifyObject,
	"ObjectStorage/SetObjectPolicy": OpOSUpdateObjectMetadata,
	"ObjectStorage/GetObjectPolicy": OpOSGetLifecyclePolicy,

	// Operations/Migration Operations (legacy)
	"Operations/MigrateData":          OpMigrateData,
	"Operations/GetMigrationProgress": OpGetMigrationProgress,
	"Operations/ReplaceNode":          OpReplaceNode,
	"Operations/UpgradeNode":          OpUpgradeNode,
	"Operations/GetUpgradeStatus":     OpGetUpgradeStatus,

	// KMS Operations (legacy - matching server protocol)
	"KMS/GenerateKey":             OpKMSGenerateKey,
	"KMS/ImportKey":               OpKMSImportKey,
	"KMS/GetKey":                  OpKMSGetKey,
	"KMS/ListKeys":                OpKMSListKeys,
	"KMS/RotateKey":               OpKMSRotateKey,
	"KMS/DeleteKey":               OpKMSDeleteKey,
	"KMS/Encrypt":                 OpKMSEncrypt,
	"KMS/Decrypt":                 OpKMSDecrypt,
	"KMS/CreateDEK":               OpKMSCreateDEK,
	"KMS/GetDEK":                  OpKMSGetDEK,
	"KMS/ListDEKs":                OpKMSListDEKs,
	"KMS/RotateDEK":               OpKMSRotateDEK,
	"KMS/DeleteDEK":               OpKMSDeleteDEK,
	"KMS/GetDEKStatus":            OpKMSGetDEKStatus,
	"KMS/SetDefaultDEK":           OpKMSSetDefaultDEK,
	"KMS/SetDEKSafety":            OpKMSSetDEKSafety,
	"KMS/ClearDEKSafety":          OpKMSClearDEKSafety,
	"KMS/BackendStatus":           OpKMSBackendStatus,
	"KMS/BackendTest":             OpKMSBackendTest,
	"KMS/KeyVersions":             OpKMSKeyVersions,
	"KMS/KeyStatus":               OpKMSKeyStatus,
	"KMS/ExportKey":               OpKMSExportKey,
	"KMS/UnwrapKey":               OpKMSUnwrapKey,
	"KMS/GetKeyMetadata":          OpKMSGetKeyMetadata,
	"KMS/UpdateKeyMetadata":       OpKMSUpdateKeyMetadata,
	"KMS/SetKeyPolicy":            OpKMSSetKeyPolicy,
	"KMS/GetKeyPolicy":            OpKMSGetKeyPolicy,
	"KMS/SetKeySafety":            OpKMSSetKeySafety,
	"KMS/ClearKeySafety":          OpKMSClearKeySafety,
	"KMS/SafetyCheck":             OpKMSSafetyCheck,
	"KMS/SafetyReport":            OpKMSSafetyReport,
	"KMS/SetKeyRotationPolicy":    OpKMSSetRotationPolicy,
	"KMS/GetKeyRotationPolicy":    OpKMSGetRotationPolicy,
	"KMS/DeleteKeyRotationPolicy": OpKMSDeleteRotationPolicy,
	"KMS/RevokeKey":               OpKMSRevokeKey,
	"KMS/GetPublicKey":            OpKMSGetPublicKey,
	"KMS/GetSupportedAlgorithms":  OpKMSGetSupportedAlgorithms,

	// ObjectStorageAdmin Operations (legacy)
	"ObjectStorageAdmin/CreateBackend":                OpOSACreateBackend,
	"ObjectStorageAdmin/UpdateBackend":                OpOSAUpdateBackend,
	"ObjectStorageAdmin/DeleteBackend":                OpOSADeleteBackend,
	"ObjectStorageAdmin/SetBackendStatus":             OpOSASetBackendStatus,
	"ObjectStorageAdmin/GetStorageStats":              OpOSAGetStorageStats,
	"ObjectStorageAdmin/RunGarbageCollection":         OpOSARunGarbageCollection,
	"ObjectStorageAdmin/GetGarbageCollectionProgress": OpOSAGetGarbageCollectionProgress,
	"ObjectStorageAdmin/RepairObject":                 OpOSARepairObject,
	"ObjectStorageAdmin/CompactBackend":               OpOSACompactBackend,

	// Scheduler Operations (legacy)
	// Scheduler operations all map to the single dispatch opcode OpScheduler
	// (0x00); the per-method sub-op byte is prepended to the payload at send
	// time. See schedulerMethodToSubOp.
	"Scheduler/CreateJob":     OpScheduler,
	"Scheduler/GetJob":        OpScheduler,
	"Scheduler/UpdateJob":     OpScheduler,
	"Scheduler/DeleteJob":     OpScheduler,
	"Scheduler/ListJobs":      OpScheduler,
	"Scheduler/TriggerJob":    OpScheduler,
	"Scheduler/PauseJob":      OpScheduler,
	"Scheduler/ResumeJob":     OpScheduler,
	"Scheduler/CancelJob":     OpScheduler,
	"Scheduler/GetJobHistory": OpScheduler,
	"Scheduler/GetStatus":     OpScheduler,

	// IAM Operations (legacy)
	"IAM.Tenant/Create": OpIAMCreateTenant,
	"IAM.Tenant/Get":    OpIAMGetTenant,
	"IAM.Tenant/Update": OpIAMUpdateTenant,
	"IAM.Tenant/Delete": OpIAMDeleteTenant,
	"IAM.Tenant/List":   OpIAMListTenants,

	"IAM.User/Create":     OpIAMCreateUser,
	"IAM.User/Get":        OpIAMGetUser,
	"IAM.User/Update":     OpIAMUpdateUser,
	"IAM.User/Delete":     OpIAMDeleteUser,
	"IAM.User/List":       OpIAMListUsers,
	"IAM.User/AssignRole": OpIAMAssignRole,
	"IAM.User/RevokeRole": OpIAMRevokeRole,

	"IAM.Group/Create":       OpIAMCreateGroup,
	"IAM.Group/Get":          OpIAMGetGroup,
	"IAM.Group/Update":       OpIAMUpdateGroup,
	"IAM.Group/Delete":       OpIAMDeleteGroup,
	"IAM.Group/List":         OpIAMListGroups,
	"IAM.Group/AddMember":    OpIAMAddUserToGroup,
	"IAM.Group/RemoveMember": OpIAMRemoveUserFromGroup,

	"IAM.Role/Create": OpIAMCreateRole,
	"IAM.Role/Get":    OpIAMGetRole,
	"IAM.Role/Update": OpIAMUpdateRole,
	"IAM.Role/Delete": OpIAMDeleteRole,
	"IAM.Role/List":   OpIAMListRoles,

	// IAM MFA Operations (legacy) - all map to OpMFA with sub-op dispatch
	"IAM.MFA/GetStatus":             OpMFA,
	"IAM.MFA/BeginEnrollment":       OpMFA,
	"IAM.MFA/CompleteEnrollment":    OpMFA,
	"IAM.MFA/CancelEnrollment":      OpMFA,
	"IAM.MFA/Disable":               OpMFA,
	"IAM.MFA/BeginChallenge":        OpMFA,
	"IAM.MFA/VerifyChallenge":       OpMFA,
	"IAM.MFA/GenerateRecoveryCodes": OpMFA,
	"IAM.MFA/ValidateCode":          OpMFA,
	"IAM.MFA/GetMFAStatus":          OpMFA,
	"IAM.MFA/BeginMFAEnrollment":    OpMFA,
	"IAM.MFA/CompleteMFAEnrollment": OpMFA,
	"IAM.MFA/CancelMFAEnrollment":   OpMFA,
	"IAM.MFA/DisableMFA":            OpMFA,
	"IAM.MFA/BeginMFAChallenge":     OpMFA,
	"IAM.MFA/VerifyMFAChallenge":    OpMFA,
	"IAM.MFA/GenerateBackupCodes":   OpMFA,
	"IAM.MFA/VerifyBackupCode":      OpMFA,
	"IAM.MFA/GetBackupCodeStatus":   OpMFA,
	"IAM.MFA/GetBackupCodes":        OpMFA,

	// Vector Operations (legacy)
	"Vector/CreateNamespace":       OpVectorCreateNamespace,
	"Vector/DeleteNamespace":       OpVectorDeleteNamespace,
	"Vector/ListNamespaces":        OpVectorListNamespaces,
	"Vector/GetNamespaceInfo":      OpVectorGetNamespaceInfo,
	"Vector/Upsert":                OpVectorUpsert,
	"Vector/Get":                   OpVectorGet,
	"Vector/Delete":                OpVectorDelete,
	"Vector/BatchUpsert":           OpVectorBatchUpsert,
	"Vector/SearchSimilar":         OpVectorSearchSimilar,
	"Vector/SearchByMetadata":      OpVectorSearchByMetadata,
	"Vector/HybridSearch":          OpVectorHybridSearch,
	"Vector/RebuildIndex":          OpVectorRebuildIndex,
	"Vector/GetRebuildProgress":    OpVectorGetRebuildProgress,
	"Vector/GetIndexStats":         OpVectorGetIndexStats,
	"Vector/Compact":               OpVectorCompact,
	"Vector/GetCompactionProgress": OpVectorGetCompactionProgress,

	// ===========================================
	// New format (matching embedded transport)
	// ===========================================

	// KV Operations
	"quicbaas.KV/Put":             OpPut,
	"quicbaas.KV/Get":             OpGet,
	"quicbaas.KV/Delete":          OpDelete,
	"quicbaas.KV/List":            OpList,
	"quicbaas.KV/ListEntityTypes": OpListEntityTypes,
	"quicbaas.KV/Watch":           OpWatch,

	// Batch Operations
	"quicbaas.Batch/Put":    OpBatchPut,
	"quicbaas.Batch/Get":    OpBatchGet,
	"quicbaas.Batch/Delete": OpBatchDelete,

	// Transaction Operations
	"quicbaas.Transaction/Execute": OpTransaction,

	// Query Operations
	"quicbaas.Query/Query":     OpQuery,
	"quicbaas.Query/ScanIndex": OpScanIndex,

	// Cluster Operations
	"quicbaas.Cluster/Status":             OpClusterStatus,
	"quicbaas.Cluster/GetLeader":          OpGetLeader,
	"quicbaas.Cluster/AddNode":            OpAddNode,
	"quicbaas.Cluster/RemoveNode":         OpRemoveNode,
	"quicbaas.Cluster/CreateShard":        OpCreateShard,
	"quicbaas.Cluster/Rebalance":          OpRebalanceCluster,
	"quicbaas.Cluster/ListNodes":          OpListNodes,
	"quicbaas.Cluster/GetRebalanceStatus": OpRebalanceStatus,

	// Embedding Operations
	"quicbaas.Embedding/GenerateEmbedding":       OpEmbeddingGenerate,
	"quicbaas.Embedding/BatchGenerateEmbeddings": OpEmbeddingBatchGenerate,
	"quicbaas.Embedding/ListProviders":           OpEmbeddingListProviders,
	"quicbaas.Embedding/GetProviderInfo":         OpEmbeddingGetProvider,
	"quicbaas.Embedding/SetDefaultProvider":      OpEmbeddingSetDefault,
	"quicbaas.Embedding/GetCacheStats":           OpEmbeddingGetCacheStats,
	"quicbaas.Embedding/ClearCache":              OpEmbeddingClearCache,

	// Backup Operations
	"quicbaas.Backup/Create":  OpBackupCreate,
	"quicbaas.Backup/List":    OpBackupList,
	"quicbaas.Backup/Restore": OpBackupRestore,
	"quicbaas.Backup/Delete":  OpBackupDelete,
	"quicbaas.Backup/Verify":  OpBackupVerify,

	// Auth Operations
	"quicbaas.Auth/Login":         OpAuthLogin,
	"quicbaas.Auth/Logout":        OpAuthLogout,
	"quicbaas.Auth/RefreshToken":  OpAuthRefreshToken,
	"quicbaas.Auth/ValidateToken": OpAuthValidateToken,
	"quicbaas.Auth/BeginLogin":    OpAuthBeginLogin,
	"quicbaas.Auth/FinishLogin":   OpAuthFinishLogin,

	// RBAC Operations
	"quicbaas.RBAC/CreateUser": OpRBACCreateUser,
	"quicbaas.RBAC/DeleteUser": OpRBACDeleteUser,
	"quicbaas.RBAC/ListUsers":  OpRBACListUsers,
	"quicbaas.RBAC/CreateRole": OpRBACCreateRole,
	"quicbaas.RBAC/DeleteRole": OpRBACDeleteRole,
	"quicbaas.RBAC/ListRoles":  OpRBACListRoles,
	"quicbaas.RBAC/AssignRole": OpRBACAssignRole,
	"quicbaas.RBAC/RevokeRole": OpRBACRevokeRole,

	// Cache Operations
	"quicbaas.Cache/GetStats":     OpCacheGetStats,
	"quicbaas.Cache/Clear":        OpCacheClear,
	"quicbaas.Cache/GetConfig":    OpCacheGetConfig,
	"quicbaas.Cache/UpdateConfig": OpCacheUpdateConfig,

	// Health Operations
	"quicbaas.Health/Check":     OpHealth,
	"quicbaas.Health/Liveness":  OpLiveness,
	"quicbaas.Health/Readiness": OpReadiness,

	// TimeSeries Operations. The high-level TimeSeries SDK client
	// (sdk/go/client/timeseries) sends the short method names "Write" and
	// "Query"; the server registers WritePoint / TSQuery on opcodes 0xA2 / 0xA4.
	// Both spellings are mapped so the CLI's TS verbs and any direct callers
	// reach the correct opcode.
	"quicbaas.TimeSeries/Write":              OpTSWritePoint,
	"quicbaas.TimeSeries/WritePoint":         OpTSWritePoint,
	"quicbaas.TimeSeries/WriteBatch":         OpTSWriteBatch,
	"quicbaas.TimeSeries/Query":              OpTSQuery,
	"quicbaas.TimeSeries/TSQuery":            OpTSQuery,
	"quicbaas.TimeSeries/Aggregate":          OpAggregate,
	"quicbaas.TimeSeries/TimeSeriesQuery":    OpTimeSeriesQuery,
	"quicbaas.TimeSeries/DeleteSeries":       OpTSDeleteSeries,
	"quicbaas.TimeSeries/GetRetentionPolicy": OpTSGetRetentionPolicy,
	"quicbaas.TimeSeries/SetRetentionPolicy": OpTSSetRetentionPolicy,
	"quicbaas.TimeSeries/ListMetrics":        OpTSListMetrics,

	// Certificate Operations. Only the six verbs the server wires over QUIC are
	// mapped (Issue / SignCSR / Revoke / Verify / GetCA / GetTLSConfig); the
	// remaining client methods (GetCABundle, IsInitialized, GenerateCRL,
	// IsRevoked, CAInfo, ListCertificates) have no QUIC opcode and surface as
	// "unknown method" rather than being silently misrouted.
	"quicbaas.Certificate/IssueCertificate":  OpCertIssue,
	"quicbaas.Certificate/SignCSR":           OpCertSignCSR,
	"quicbaas.Certificate/RevokeCertificate": OpCertRevoke,
	"quicbaas.Certificate/VerifyCertificate": OpCertVerify,
	"quicbaas.Certificate/GetCACertificate":  OpCertGetCA,
	"quicbaas.Certificate/GetTLSConfig":      OpCertGetTLSConfig,

	// Certificate extensions ride the OpOrchestrator dispatch byte with the
	// sub-op in payload[0]; see CertExtMethodToSubOp.
	"quicbaas.Certificate/CrossSign":           OpOrchestrator,
	"quicbaas.Certificate/Chain":               OpOrchestrator,
	"quicbaas.Certificate/IssueDeviceIdentity": OpOrchestrator,

	// Attestation rides the OpOrchestrator dispatch byte with the sub-op in
	// payload[0]; see AttestationMethodToSubOp.
	"quicbaas.Attestation/Verify":          OpOrchestrator,
	"quicbaas.Attestation/VerifyChain":     OpOrchestrator,
	"quicbaas.Attestation/AttestContainer": OpOrchestrator,

	// PIV rides the OpOrchestrator dispatch byte with the sub-op in
	// payload[0]; see PIVMethodToSubOp. The verbs and payloads are the
	// go-xkms PIVService transport contract verbatim.
	"quicbaas.PIV/ListPIVSlots":         OpOrchestrator,
	"quicbaas.PIV/GetPIVCertificate":    OpOrchestrator,
	"quicbaas.PIV/StorePIVCertificate":  OpOrchestrator,
	"quicbaas.PIV/DeletePIVCertificate": OpOrchestrator,
	"quicbaas.PIV/GeneratePIVKey":       OpOrchestrator,
	"quicbaas.PIV/ImportPIVCertificate": OpOrchestrator,
	"quicbaas.PIV/ExportPIVCertificate": OpOrchestrator,
	"quicbaas.PIV/GeneratePIVCSR":       OpOrchestrator,

	// FIDO2 rides the OpOrchestrator dispatch byte with the sub-op in
	// payload[0]; see FIDO2MethodToSubOp. The verbs and payloads are the
	// go-xkms FIDO2 transport contract verbatim, tenant-scoped via the
	// injected tenant_id field.
	"quicbaas.FIDO2/BeginRegistration":    OpOrchestrator,
	"quicbaas.FIDO2/FinishRegistration":   OpOrchestrator,
	"quicbaas.FIDO2/BeginAuthentication":  OpOrchestrator,
	"quicbaas.FIDO2/FinishAuthentication": OpOrchestrator,

	// Dynamic Secrets Operations (0x64-0x6B). The server exposes these as
	// top-level opcodes (pkg/api/quic/handler.go), so each verb resolves
	// directly without an OpOrchestrator sub-op byte.
	"quicbaas.DynamicSecrets/CreateRole":         OpDynamicSecretsCreateRole,
	"quicbaas.DynamicSecrets/GetRole":            OpDynamicSecretsGetRole,
	"quicbaas.DynamicSecrets/ListRoles":          OpDynamicSecretsListRoles,
	"quicbaas.DynamicSecrets/DeleteRole":         OpDynamicSecretsDeleteRole,
	"quicbaas.DynamicSecrets/GenerateCredential": OpDynamicSecretsGenerate,
	"quicbaas.DynamicSecrets/RenewLease":         OpDynamicSecretsRenewLease,
	"quicbaas.DynamicSecrets/RevokeLease":        OpDynamicSecretsRevokeLease,
	"quicbaas.DynamicSecrets/ListLeases":         OpDynamicSecretsListLeases,

	// Trust Operations (0x7A-0x7C). CrossSignedRootPool, AutoImportAIA and
	// PublishDANE are wired across the QUIC transport.
	"quicbaas.Trust/CrossSignedRootPool": OpTrustCrossSignedRootPool,
	"quicbaas.Trust/AutoImportAIA":       OpTrustAutoImportAIA,
	"quicbaas.Trust/PublishDANE":         OpTrustPublishDANE,

	// ObjectStorage Operations (0x28-0x3B). SetObjectPolicy/GetObjectPolicy reuse
	// the server's UpdateObjectMetadata / GetLifecyclePolicy handlers, which
	// decode service.SetObjectPolicyRequest / service.GetObjectPolicyRequest.
	"quicbaas.ObjectStorage/PutObject":       OpOSPutObject,
	"quicbaas.ObjectStorage/GetObject":       OpOSGetObject,
	"quicbaas.ObjectStorage/DeleteObject":    OpOSDeleteObject,
	"quicbaas.ObjectStorage/HeadObject":      OpOSHeadObject,
	"quicbaas.ObjectStorage/ListObjects":     OpOSListObjects,
	"quicbaas.ObjectStorage/ListByTag":       OpOSListByTag,
	"quicbaas.ObjectStorage/ListByTenant":    OpOSListByTenant,
	"quicbaas.ObjectStorage/CopyObject":      OpOSCopyObject,
	"quicbaas.ObjectStorage/ReplicateObject": OpOSReplicateObject,
	"quicbaas.ObjectStorage/MigrateObject":   OpOSMigrateObject,
	"quicbaas.ObjectStorage/ArchiveObject":   OpOSArchiveObject,
	"quicbaas.ObjectStorage/RestoreObject":   OpOSRestoreObject,
	"quicbaas.ObjectStorage/VerifyObject":    OpOSVerifyObject,
	"quicbaas.ObjectStorage/SetObjectPolicy": OpOSUpdateObjectMetadata,
	"quicbaas.ObjectStorage/GetObjectPolicy": OpOSGetLifecyclePolicy,

	// Operations/Migration Operations
	"quicbaas.Operations/MigrateData":          OpMigrateData,
	"quicbaas.Operations/GetMigrationProgress": OpGetMigrationProgress,
	"quicbaas.Operations/ReplaceNode":          OpReplaceNode,
	"quicbaas.Operations/UpgradeNode":          OpUpgradeNode,
	"quicbaas.Operations/GetUpgradeStatus":     OpGetUpgradeStatus,

	// KMS Operations (matching server protocol)
	"quicbaas.KMS/GenerateKey":             OpKMSGenerateKey,
	"quicbaas.KMS/ImportKey":               OpKMSImportKey,
	"quicbaas.KMS/GetKey":                  OpKMSGetKey,
	"quicbaas.KMS/ListKeys":                OpKMSListKeys,
	"quicbaas.KMS/RotateKey":               OpKMSRotateKey,
	"quicbaas.KMS/DeleteKey":               OpKMSDeleteKey,
	"quicbaas.KMS/Encrypt":                 OpKMSEncrypt,
	"quicbaas.KMS/Decrypt":                 OpKMSDecrypt,
	"quicbaas.KMS/CreateDEK":               OpKMSCreateDEK,
	"quicbaas.KMS/GetDEK":                  OpKMSGetDEK,
	"quicbaas.KMS/ListDEKs":                OpKMSListDEKs,
	"quicbaas.KMS/RotateDEK":               OpKMSRotateDEK,
	"quicbaas.KMS/DeleteDEK":               OpKMSDeleteDEK,
	"quicbaas.KMS/GetDEKStatus":            OpKMSGetDEKStatus,
	"quicbaas.KMS/SetDefaultDEK":           OpKMSSetDefaultDEK,
	"quicbaas.KMS/SetDEKSafety":            OpKMSSetDEKSafety,
	"quicbaas.KMS/ClearDEKSafety":          OpKMSClearDEKSafety,
	"quicbaas.KMS/BackendStatus":           OpKMSBackendStatus,
	"quicbaas.KMS/BackendTest":             OpKMSBackendTest,
	"quicbaas.KMS/KeyVersions":             OpKMSKeyVersions,
	"quicbaas.KMS/ListKeyVersions":         OpKMSKeyVersions,
	"quicbaas.KMS/KeyStatus":               OpKMSKeyStatus,
	"quicbaas.KMS/UpdateKeyStatus":         OpKMSKeyStatus,
	"quicbaas.KMS/ExportKey":               OpKMSExportKey,
	"quicbaas.KMS/UnwrapKey":               OpKMSUnwrapKey,
	"quicbaas.KMS/GetKeyMetadata":          OpKMSGetKeyMetadata,
	"quicbaas.KMS/UpdateKeyMetadata":       OpKMSUpdateKeyMetadata,
	"quicbaas.KMS/SetKeyPolicy":            OpKMSSetKeyPolicy,
	"quicbaas.KMS/GetKeyPolicy":            OpKMSGetKeyPolicy,
	"quicbaas.KMS/SetKeySafety":            OpKMSSetKeySafety,
	"quicbaas.KMS/ClearKeySafety":          OpKMSClearKeySafety,
	"quicbaas.KMS/SafetyCheck":             OpKMSSafetyCheck,
	"quicbaas.KMS/SafetyReport":            OpKMSSafetyReport,
	"quicbaas.KMS/SetKeyRotationPolicy":    OpKMSSetRotationPolicy,
	"quicbaas.KMS/GetKeyRotationPolicy":    OpKMSGetRotationPolicy,
	"quicbaas.KMS/DeleteKeyRotationPolicy": OpKMSDeleteRotationPolicy,
	"quicbaas.KMS/RevokeKey":               OpKMSRevokeKey,
	"quicbaas.KMS/GetPublicKey":            OpKMSGetPublicKey,
	"quicbaas.KMS/GetSupportedAlgorithms":  OpKMSGetSupportedAlgorithms,

	// DEK Operations (server OpKMSDEK* range). The DEK SDK service shares the
	// server's KMS DEK opcodes; these aliases let the DEK client resolve them
	// under its own quicbaas.DEK service name.
	"quicbaas.DEK/CreateDEK":               OpKMSCreateDEK,
	"quicbaas.DEK/GetDEK":                  OpKMSGetDEK,
	"quicbaas.DEK/ListDEKs":                OpKMSListDEKs,
	"quicbaas.DEK/RotateDEK":               OpKMSRotateDEK,
	"quicbaas.DEK/DeleteDEK":               OpKMSDeleteDEK,
	"quicbaas.DEK/GetDEKStatus":            OpKMSGetDEKStatus,
	"quicbaas.DEK/SetDefaultDEK":           OpKMSSetDefaultDEK,
	"quicbaas.DEK/SetDEKSafety":            OpKMSSetDEKSafety,
	"quicbaas.DEK/ClearDEKSafety":          OpKMSClearDEKSafety,
	"quicbaas.DEK/StartReencryption":       OpKMSDEKStartReencryption,
	"quicbaas.DEK/GetReencryptionProgress": OpKMSDEKGetReencryptProgress,

	// ObjectStorageAdmin Operations
	"quicbaas.ObjectStorageAdmin/CreateBackend":                OpOSACreateBackend,
	"quicbaas.ObjectStorageAdmin/UpdateBackend":                OpOSAUpdateBackend,
	"quicbaas.ObjectStorageAdmin/DeleteBackend":                OpOSADeleteBackend,
	"quicbaas.ObjectStorageAdmin/SetBackendStatus":             OpOSASetBackendStatus,
	"quicbaas.ObjectStorageAdmin/GetStorageStats":              OpOSAGetStorageStats,
	"quicbaas.ObjectStorageAdmin/RunGarbageCollection":         OpOSARunGarbageCollection,
	"quicbaas.ObjectStorageAdmin/GetGarbageCollectionProgress": OpOSAGetGarbageCollectionProgress,
	"quicbaas.ObjectStorageAdmin/RepairObject":                 OpOSARepairObject,
	"quicbaas.ObjectStorageAdmin/CompactBackend":               OpOSACompactBackend,

	// Secrets Operations. "Ensure" is intentionally absent: the server does not
	// support EnsureSecret over QUIC (ErrEnsureSecretUnsupportedQUIC), so it must
	// surface as an unknown method rather than be silently rerouted.
	"quicbaas.Secrets/Store":             OpSecretsStore,
	"quicbaas.Secrets/Get":               OpSecretsGet,
	"quicbaas.Secrets/List":              OpSecretsList,
	"quicbaas.Secrets/Delete":            OpSecretsDelete,
	"quicbaas.Secrets/ListVersions":      OpSecretsListVersions,
	"quicbaas.Secrets/Rollback":          OpSecretsRollback,
	"quicbaas.Secrets/Rotate":            OpSecretsRotate,
	"quicbaas.Secrets/GetVersion":        OpSecretsGetVersion,
	"quicbaas.Secrets/SetRotationPolicy": OpSecretsSetRotation,
	"quicbaas.Secrets/GetRotationPolicy": OpSecretsGetRotation,
	"quicbaas.Secrets/UpdateStatus":      OpSecretsUpdateStatus,

	// Crypto (transit) Operations.
	"quicbaas.Crypto/Encrypt":         OpCryptoEncrypt,
	"quicbaas.Crypto/Decrypt":         OpCryptoDecrypt,
	"quicbaas.Crypto/Rewrap":          OpCryptoRewrap,
	"quicbaas.Crypto/Sign":            OpCryptoSign,
	"quicbaas.Crypto/Verify":          OpCryptoVerify,
	"quicbaas.Crypto/Hash":            OpCryptoHash,
	"quicbaas.Crypto/GenerateRandom":  OpCryptoRandom,
	"quicbaas.Crypto/GenerateDataKey": OpCryptoDataKey,
	"quicbaas.Crypto/GenerateHMAC":    OpCryptoHMAC,
	"quicbaas.Crypto/VerifyHMAC":      OpCryptoVerifyHMAC,

	"quicbaas.Scheduler/CreateJob":     OpScheduler,
	"quicbaas.Scheduler/GetJob":        OpScheduler,
	"quicbaas.Scheduler/UpdateJob":     OpScheduler,
	"quicbaas.Scheduler/DeleteJob":     OpScheduler,
	"quicbaas.Scheduler/ListJobs":      OpScheduler,
	"quicbaas.Scheduler/TriggerJob":    OpScheduler,
	"quicbaas.Scheduler/PauseJob":      OpScheduler,
	"quicbaas.Scheduler/ResumeJob":     OpScheduler,
	"quicbaas.Scheduler/CancelJob":     OpScheduler,
	"quicbaas.Scheduler/GetJobHistory": OpScheduler,
	"quicbaas.Scheduler/GetStatus":     OpScheduler,

	// Messaging Operations - all map to OpMsg (0x00); the SubOp byte is prepended
	// to the payload at send time. See messagingMethodToSubOp. The sub-op range
	// (0x20-0x2F) keeps these disjoint from Scheduler (0x01-0x1F) on the wire.
	"quicbaas.Messaging/CreateChannel":   OpMsg,
	"quicbaas.Messaging/DeleteChannel":   OpMsg,
	"quicbaas.Messaging/GetChannel":      OpMsg,
	"quicbaas.Messaging/ListChannels":    OpMsg,
	"quicbaas.Messaging/Subscribe":       OpMsg,
	"quicbaas.Messaging/Unsubscribe":     OpMsg,
	"quicbaas.Messaging/Publish":         OpMsg,
	"quicbaas.Messaging/PublishDelta":    OpMsg,
	"quicbaas.Messaging/GetStats":        OpMsg,
	"quicbaas.Messaging/GetDedupStats":   OpMsg,
	"quicbaas.Messaging/GetChannelStats": OpMsg,

	// Barrier operations - all map to OpBarrier (0xC0); the SubOp byte is
	// prepended to the payload at send time. See barrierMethodToSubOp.
	"quicbaas.Barrier/BarrierStatus":             OpBarrier,
	"quicbaas.Barrier/BarrierInitialize":         OpBarrier,
	"quicbaas.Barrier/BarrierUnseal":             OpBarrier,
	"quicbaas.Barrier/BarrierSeal":               OpBarrier,
	"quicbaas.Barrier/BarrierShamirInitialize":   OpBarrier,
	"quicbaas.Barrier/BarrierShamirUnsealShare":  OpBarrier,
	"quicbaas.Barrier/BarrierShamirUnsealShares": OpBarrier,
	"quicbaas.Barrier/BarrierRekey":              OpBarrier,
	"quicbaas.Barrier/BarrierRecoveryGenerate":   OpBarrier,
	"quicbaas.Barrier/BarrierRecoveryRecover":    OpBarrier,
	"quicbaas.Barrier/BarrierRecoveryDelete":     OpBarrier,
	"quicbaas.Barrier/BarrierRecoveryStatus":     OpBarrier,
	"quicbaas.Barrier/BarrierRootToken":          OpBarrier,

	// Ceremony operations - all map to OpCeremony (0xC1).
	"quicbaas.Ceremony/CeremonyInitialize":            OpCeremony,
	"quicbaas.Ceremony/CeremonyStatus":                OpCeremony,
	"quicbaas.Ceremony/CeremonyClaimCertBegin":        OpCeremony,
	"quicbaas.Ceremony/CeremonyClaimCertComplete":     OpCeremony,
	"quicbaas.Ceremony/CeremonyBeginClaimShare":       OpCeremony,
	"quicbaas.Ceremony/CeremonyCompleteClaimShare":    OpCeremony,
	"quicbaas.Ceremony/CeremonySignCSR":               OpCeremony,
	"quicbaas.Ceremony/CeremonyListOfficers":          OpCeremony,
	"quicbaas.Ceremony/CeremonyEnrollExternalOfficer": OpCeremony,

	// IAM Tenant Operations
	"quicbaas.IAM.Tenant/Create": OpIAMCreateTenant,
	"quicbaas.IAM.Tenant/Get":    OpIAMGetTenant,
	"quicbaas.IAM.Tenant/Update": OpIAMUpdateTenant,
	"quicbaas.IAM.Tenant/Delete": OpIAMDeleteTenant,
	"quicbaas.IAM.Tenant/List":   OpIAMListTenants,

	// IAM User Operations
	"quicbaas.IAM.User/Create":     OpIAMCreateUser,
	"quicbaas.IAM.User/Get":        OpIAMGetUser,
	"quicbaas.IAM.User/Update":     OpIAMUpdateUser,
	"quicbaas.IAM.User/Delete":     OpIAMDeleteUser,
	"quicbaas.IAM.User/List":       OpIAMListUsers,
	"quicbaas.IAM.User/AssignRole": OpIAMAssignRole,
	"quicbaas.IAM.User/RevokeRole": OpIAMRevokeRole,

	// IAM Group Operations
	"quicbaas.IAM.Group/Create":       OpIAMCreateGroup,
	"quicbaas.IAM.Group/Get":          OpIAMGetGroup,
	"quicbaas.IAM.Group/Update":       OpIAMUpdateGroup,
	"quicbaas.IAM.Group/Delete":       OpIAMDeleteGroup,
	"quicbaas.IAM.Group/List":         OpIAMListGroups,
	"quicbaas.IAM.Group/AddMember":    OpIAMAddUserToGroup,
	"quicbaas.IAM.Group/RemoveMember": OpIAMRemoveUserFromGroup,

	// IAM Role Operations
	"quicbaas.IAM.Role/Create": OpIAMCreateRole,
	"quicbaas.IAM.Role/Get":    OpIAMGetRole,
	"quicbaas.IAM.Role/Update": OpIAMUpdateRole,
	"quicbaas.IAM.Role/Delete": OpIAMDeleteRole,
	"quicbaas.IAM.Role/List":   OpIAMListRoles,

	// IAM App Operations
	"quicbaas.IAM.App/Register":     OpIAMRegisterApp,
	"quicbaas.IAM.App/Get":          OpIAMGetApp,
	"quicbaas.IAM.App/Update":       OpIAMUpdateApp,
	"quicbaas.IAM.App/Delete":       OpIAMDeleteApp,
	"quicbaas.IAM.App/List":         OpIAMListApps,
	"quicbaas.IAM.App/RotateSecret": OpIAMRotateAppSecret,
	"IAM.App/Register":              OpIAMRegisterApp,
	"IAM.App/Get":                   OpIAMGetApp,
	"IAM.App/Update":                OpIAMUpdateApp,
	"IAM.App/Delete":                OpIAMDeleteApp,
	"IAM.App/List":                  OpIAMListApps,
	"IAM.App/RotateSecret":          OpIAMRotateAppSecret,

	// IAM Policy Operations
	"quicbaas.IAM.Policy/Create":   OpIAMCreatePolicy,
	"quicbaas.IAM.Policy/Get":      OpIAMGetPolicy,
	"quicbaas.IAM.Policy/Update":   OpIAMUpdatePolicy,
	"quicbaas.IAM.Policy/Delete":   OpIAMDeletePolicy,
	"quicbaas.IAM.Policy/List":     OpIAMListPolicies,
	"quicbaas.IAM.Policy/Validate": OpIAMValidatePolicy,
	"quicbaas.IAM.Policy/Test":     OpIAMTestPolicy,
	"IAM.Policy/Create":            OpIAMCreatePolicy,
	"IAM.Policy/Get":               OpIAMGetPolicy,
	"IAM.Policy/Update":            OpIAMUpdatePolicy,
	"IAM.Policy/Delete":            OpIAMDeletePolicy,
	"IAM.Policy/List":              OpIAMListPolicies,
	"IAM.Policy/Validate":          OpIAMValidatePolicy,
	"IAM.Policy/Test":              OpIAMTestPolicy,

	// IAM MFA Operations - all map to OpMFA with sub-op dispatch
	"quicbaas.IAM.MFA/GetStatus":             OpMFA,
	"quicbaas.IAM.MFA/BeginEnrollment":       OpMFA,
	"quicbaas.IAM.MFA/CompleteEnrollment":    OpMFA,
	"quicbaas.IAM.MFA/CancelEnrollment":      OpMFA,
	"quicbaas.IAM.MFA/Disable":               OpMFA,
	"quicbaas.IAM.MFA/BeginChallenge":        OpMFA,
	"quicbaas.IAM.MFA/VerifyChallenge":       OpMFA,
	"quicbaas.IAM.MFA/GenerateRecoveryCodes": OpMFA,
	"quicbaas.IAM.MFA/ValidateCode":          OpMFA,
	"quicbaas.IAM.MFA/GetMFAStatus":          OpMFA,
	"quicbaas.IAM.MFA/BeginMFAEnrollment":    OpMFA,
	"quicbaas.IAM.MFA/CompleteMFAEnrollment": OpMFA,
	"quicbaas.IAM.MFA/CancelMFAEnrollment":   OpMFA,
	"quicbaas.IAM.MFA/DisableMFA":            OpMFA,
	"quicbaas.IAM.MFA/BeginMFAChallenge":     OpMFA,
	"quicbaas.IAM.MFA/VerifyMFAChallenge":    OpMFA,
	"quicbaas.IAM.MFA/GenerateBackupCodes":   OpMFA,
	"quicbaas.IAM.MFA/VerifyBackupCode":      OpMFA,
	"quicbaas.IAM.MFA/GetBackupCodeStatus":   OpMFA,
	"quicbaas.IAM.MFA/GetBackupCodes":        OpMFA,

	// Vector Operations
	"quicbaas.Vector/CreateNamespace":       OpVectorCreateNamespace,
	"quicbaas.Vector/DeleteNamespace":       OpVectorDeleteNamespace,
	"quicbaas.Vector/ListNamespaces":        OpVectorListNamespaces,
	"quicbaas.Vector/GetNamespaceInfo":      OpVectorGetNamespaceInfo,
	"quicbaas.Vector/Upsert":                OpVectorUpsert,
	"quicbaas.Vector/Get":                   OpVectorGet,
	"quicbaas.Vector/Delete":                OpVectorDelete,
	"quicbaas.Vector/BatchUpsert":           OpVectorBatchUpsert,
	"quicbaas.Vector/SearchSimilar":         OpVectorSearchSimilar,
	"quicbaas.Vector/SearchByMetadata":      OpVectorSearchByMetadata,
	"quicbaas.Vector/HybridSearch":          OpVectorHybridSearch,
	"quicbaas.Vector/RebuildIndex":          OpVectorRebuildIndex,
	"quicbaas.Vector/GetRebuildProgress":    OpVectorGetRebuildProgress,
	"quicbaas.Vector/GetIndexStats":         OpVectorGetIndexStats,
	"quicbaas.Vector/Compact":               OpVectorCompact,
	"quicbaas.Vector/GetCompactionProgress": OpVectorGetCompactionProgress,

	// Orchestrator Operations - all map to OpOrchestrator (0x8F); the SubOp byte is
	// prepended to the payload by the transport layer before sending on the wire.
	"quicbaas.Orchestrator/SpawnSession":  OpOrchestrator,
	"quicbaas.Orchestrator/PromptSession": OpOrchestrator,
	"quicbaas.Orchestrator/SessionStatus": OpOrchestrator,
	"quicbaas.Orchestrator/CloseSession":  OpOrchestrator,
	"quicbaas.Orchestrator/ListSessions":  OpOrchestrator,
	"quicbaas.Orchestrator/CreateTask":    OpOrchestrator,
	"quicbaas.Orchestrator/GetTask":       OpOrchestrator,
	"quicbaas.Orchestrator/ListTasks":     OpOrchestrator,
	"quicbaas.Orchestrator/ClaimTask":     OpOrchestrator,
	"quicbaas.Orchestrator/CompleteTask":  OpOrchestrator,
	"quicbaas.Orchestrator/ListBackends":  OpOrchestrator,
	"quicbaas.Orchestrator/Health":        OpOrchestrator,
	"quicbaas.Orchestrator/Inference":     OpOrchestrator,

	// Pooled provider Account Operations - share OpOrchestrator (0x8F); the
	// SubOp byte (0x30-0x38) selects the account sub-operation.
	"quicbaas.Account/Create":           OpOrchestrator,
	"quicbaas.Account/Get":              OpOrchestrator,
	"quicbaas.Account/List":             OpOrchestrator,
	"quicbaas.Account/Update":           OpOrchestrator,
	"quicbaas.Account/Delete":           OpOrchestrator,
	"quicbaas.Account/RotateCredential": OpOrchestrator,
	"quicbaas.Account/Revoke":           OpOrchestrator,
	"quicbaas.Account/SetDefault":       OpOrchestrator,
	"quicbaas.Account/Usage":            OpOrchestrator,

	// Project Operations - share OpOrchestrator (0x8F); the SubOp byte
	// (0x40-0x47) selects the project sub-operation.
	"quicbaas.Project/Create":          OpOrchestrator,
	"quicbaas.Project/Get":             OpOrchestrator,
	"quicbaas.Project/GetByName":       OpOrchestrator,
	"quicbaas.Project/List":            OpOrchestrator,
	"quicbaas.Project/Update":          OpOrchestrator,
	"quicbaas.Project/Delete":          OpOrchestrator,
	"quicbaas.Project/GrantAgentRole":  OpOrchestrator,
	"quicbaas.Project/RevokeAgentRole": OpOrchestrator,

	// SSH Operations - share OpOrchestrator (0x8F); the SubOp byte
	// (0x50-0x5D) selects the SSH sub-operation.
	"quicbaas.SSH/GenerateKey":         OpOrchestrator,
	"quicbaas.SSH/ImportKey":           OpOrchestrator,
	"quicbaas.SSH/GetKey":              OpOrchestrator,
	"quicbaas.SSH/ListKeys":            OpOrchestrator,
	"quicbaas.SSH/DeleteKey":           OpOrchestrator,
	"quicbaas.SSH/ExportPublicKey":     OpOrchestrator,
	"quicbaas.SSH/SignDigest":          OpOrchestrator,
	"quicbaas.SSH/AddKnownHost":        OpOrchestrator,
	"quicbaas.SSH/ListKnownHosts":      OpOrchestrator,
	"quicbaas.SSH/RemoveKnownHost":     OpOrchestrator,
	"quicbaas.SSH/AddAuthorizedKey":    OpOrchestrator,
	"quicbaas.SSH/ListAuthorizedKeys":  OpOrchestrator,
	"quicbaas.SSH/RemoveAuthorizedKey": OpOrchestrator,
	"quicbaas.SSH/EmitAuthorizedKey":   OpOrchestrator,

	// Legacy short-form Orchestrator keys (without quicbaas. prefix).
	"Orchestrator/SpawnSession":  OpOrchestrator,
	"Orchestrator/PromptSession": OpOrchestrator,
	"Orchestrator/SessionStatus": OpOrchestrator,
	"Orchestrator/CloseSession":  OpOrchestrator,
	"Orchestrator/ListSessions":  OpOrchestrator,
	"Orchestrator/CreateTask":    OpOrchestrator,
	"Orchestrator/GetTask":       OpOrchestrator,
	"Orchestrator/ListTasks":     OpOrchestrator,
	"Orchestrator/ClaimTask":     OpOrchestrator,
	"Orchestrator/CompleteTask":  OpOrchestrator,
	"Orchestrator/ListBackends":  OpOrchestrator,
	"Orchestrator/Health":        OpOrchestrator,
	"Orchestrator/Inference":     OpOrchestrator,
}

// opcodeToMethod maps opcodes to service/method combinations for O(1) reverse lookup.
// Uses the new quicbaas.* format for consistency with embedded transport.
var opcodeToMethod = map[byte]string{
	// KV Operations
	OpPut:             "quicbaas.KV/Put",
	OpGet:             "quicbaas.KV/Get",
	OpDelete:          "quicbaas.KV/Delete",
	OpList:            "quicbaas.KV/List",
	OpListEntityTypes: "quicbaas.KV/ListEntityTypes",
	OpWatch:           "quicbaas.KV/Watch",

	// Batch Operations
	OpBatchPut:    "quicbaas.Batch/Put",
	OpBatchGet:    "quicbaas.Batch/Get",
	OpBatchDelete: "quicbaas.Batch/Delete",

	// Transaction Operations
	OpTransaction: "quicbaas.Transaction/Execute",

	// Query Operations
	OpQuery:     "quicbaas.Query/Query",
	OpScanIndex: "quicbaas.Query/ScanIndex",

	// Cluster Operations
	OpClusterStatus:      "quicbaas.Cluster/Status",
	OpGetLeader:          "quicbaas.Cluster/GetLeader",
	OpAddNode:            "quicbaas.Cluster/AddNode",
	OpRemoveNode:         "quicbaas.Cluster/RemoveNode",
	OpCreateShard:        "quicbaas.Cluster/CreateShard",
	OpRebalanceCluster:   "quicbaas.Cluster/Rebalance",
	OpListNodes:          "quicbaas.Cluster/ListNodes",
	OpRebalanceStatus:    "quicbaas.Cluster/GetRebalanceStatus",
	OpRebalancePlan:      "quicbaas.Cluster/GetRebalancePlan",
	OpCancelMigration:    "quicbaas.Cluster/CancelMigration",
	OpNodeReplaceRequest: "quicbaas.Cluster/NodeReplaceRequest",

	// Embedding Operations
	OpEmbeddingGenerate:      "quicbaas.Embedding/GenerateEmbedding",
	OpEmbeddingBatchGenerate: "quicbaas.Embedding/BatchGenerateEmbeddings",
	OpEmbeddingListProviders: "quicbaas.Embedding/ListProviders",
	OpEmbeddingGetProvider:   "quicbaas.Embedding/GetProviderInfo",
	OpEmbeddingSetDefault:    "quicbaas.Embedding/SetDefaultProvider",
	OpEmbeddingGetCacheStats: "quicbaas.Embedding/GetCacheStats",
	OpEmbeddingClearCache:    "quicbaas.Embedding/ClearCache",

	// Backup Operations
	OpBackupCreate:  "quicbaas.Backup/Create",
	OpBackupList:    "quicbaas.Backup/List",
	OpBackupRestore: "quicbaas.Backup/Restore",
	OpBackupDelete:  "quicbaas.Backup/Delete",
	OpBackupVerify:  "quicbaas.Backup/Verify",

	// Auth Operations
	OpAuthLogin:         "quicbaas.Auth/Login",
	OpAuthLogout:        "quicbaas.Auth/Logout",
	OpAuthRefreshToken:  "quicbaas.Auth/RefreshToken",
	OpAuthValidateToken: "quicbaas.Auth/ValidateToken",
	OpAuthBeginLogin:    "quicbaas.Auth/BeginLogin",
	OpAuthFinishLogin:   "quicbaas.Auth/FinishLogin",

	// RBAC Operations
	OpRBACCreateUser: "quicbaas.RBAC/CreateUser",
	OpRBACDeleteUser: "quicbaas.RBAC/DeleteUser",
	OpRBACListUsers:  "quicbaas.RBAC/ListUsers",
	OpRBACCreateRole: "quicbaas.RBAC/CreateRole",
	OpRBACDeleteRole: "quicbaas.RBAC/DeleteRole",
	OpRBACListRoles:  "quicbaas.RBAC/ListRoles",
	OpRBACAssignRole: "quicbaas.RBAC/AssignRole",
	OpRBACRevokeRole: "quicbaas.RBAC/RevokeRole",

	// Cache Operations
	OpCacheGetStats:     "quicbaas.Cache/GetStats",
	OpCacheClear:        "quicbaas.Cache/Clear",
	OpCacheGetConfig:    "quicbaas.Cache/GetConfig",
	OpCacheUpdateConfig: "quicbaas.Cache/UpdateConfig",

	// Health Operations
	OpHealth:    "quicbaas.Health/Check",
	OpLiveness:  "quicbaas.Health/Liveness",
	OpReadiness: "quicbaas.Health/Readiness",

	// TimeSeries Operations. Reverse map uses the high-level client method names
	// (Write / Query) since those are what the SDK client emits.
	OpTSWritePoint:         "quicbaas.TimeSeries/Write",
	OpTSWriteBatch:         "quicbaas.TimeSeries/WriteBatch",
	OpTSQuery:              "quicbaas.TimeSeries/Query",
	OpAggregate:            "quicbaas.TimeSeries/Aggregate",
	OpTimeSeriesQuery:      "quicbaas.TimeSeries/TimeSeriesQuery",
	OpTSDeleteSeries:       "quicbaas.TimeSeries/DeleteSeries",
	OpTSGetRetentionPolicy: "quicbaas.TimeSeries/GetRetentionPolicy",
	OpTSSetRetentionPolicy: "quicbaas.TimeSeries/SetRetentionPolicy",
	OpTSListMetrics:        "quicbaas.TimeSeries/ListMetrics",

	// Certificate Operations
	OpCertIssue:        "quicbaas.Certificate/IssueCertificate",
	OpCertSignCSR:      "quicbaas.Certificate/SignCSR",
	OpCertRevoke:       "quicbaas.Certificate/RevokeCertificate",
	OpCertVerify:       "quicbaas.Certificate/VerifyCertificate",
	OpCertGetCA:        "quicbaas.Certificate/GetCACertificate",
	OpCertGetTLSConfig: "quicbaas.Certificate/GetTLSConfig",

	// Dynamic Secrets Operations
	OpDynamicSecretsCreateRole:  "quicbaas.DynamicSecrets/CreateRole",
	OpDynamicSecretsGetRole:     "quicbaas.DynamicSecrets/GetRole",
	OpDynamicSecretsListRoles:   "quicbaas.DynamicSecrets/ListRoles",
	OpDynamicSecretsDeleteRole:  "quicbaas.DynamicSecrets/DeleteRole",
	OpDynamicSecretsGenerate:    "quicbaas.DynamicSecrets/GenerateCredential",
	OpDynamicSecretsRenewLease:  "quicbaas.DynamicSecrets/RenewLease",
	OpDynamicSecretsRevokeLease: "quicbaas.DynamicSecrets/RevokeLease",
	OpDynamicSecretsListLeases:  "quicbaas.DynamicSecrets/ListLeases",

	// Trust Operations
	OpTrustCrossSignedRootPool: "quicbaas.Trust/CrossSignedRootPool",
	OpTrustAutoImportAIA:       "quicbaas.Trust/AutoImportAIA",
	OpTrustPublishDANE:         "quicbaas.Trust/PublishDANE",

	// ObjectStorage Operations
	OpOSPutObject:            "quicbaas.ObjectStorage/PutObject",
	OpOSGetObject:            "quicbaas.ObjectStorage/GetObject",
	OpOSDeleteObject:         "quicbaas.ObjectStorage/DeleteObject",
	OpOSHeadObject:           "quicbaas.ObjectStorage/HeadObject",
	OpOSListObjects:          "quicbaas.ObjectStorage/ListObjects",
	OpOSListByTag:            "quicbaas.ObjectStorage/ListByTag",
	OpOSListByTenant:         "quicbaas.ObjectStorage/ListByTenant",
	OpOSCopyObject:           "quicbaas.ObjectStorage/CopyObject",
	OpOSReplicateObject:      "quicbaas.ObjectStorage/ReplicateObject",
	OpOSMigrateObject:        "quicbaas.ObjectStorage/MigrateObject",
	OpOSArchiveObject:        "quicbaas.ObjectStorage/ArchiveObject",
	OpOSRestoreObject:        "quicbaas.ObjectStorage/RestoreObject",
	OpOSVerifyObject:         "quicbaas.ObjectStorage/VerifyObject",
	OpOSUpdateObjectMetadata: "quicbaas.ObjectStorage/SetObjectPolicy",
	OpOSGetLifecyclePolicy:   "quicbaas.ObjectStorage/GetObjectPolicy",

	// Operations/Migration Operations
	OpMigrateData:          "quicbaas.Operations/MigrateData",
	OpGetMigrationProgress: "quicbaas.Operations/GetMigrationProgress",
	OpReplaceNode:          "quicbaas.Operations/ReplaceNode",
	OpUpgradeNode:          "quicbaas.Operations/UpgradeNode",
	OpGetUpgradeStatus:     "quicbaas.Operations/GetUpgradeStatus",

	// KMS Operations (matching server protocol)
	OpKMSGenerateKey:            "quicbaas.KMS/GenerateKey",
	OpKMSImportKey:              "quicbaas.KMS/ImportKey",
	OpKMSGetKey:                 "quicbaas.KMS/GetKey",
	OpKMSListKeys:               "quicbaas.KMS/ListKeys",
	OpKMSRotateKey:              "quicbaas.KMS/RotateKey",
	OpKMSDeleteKey:              "quicbaas.KMS/DeleteKey",
	OpKMSEncrypt:                "quicbaas.KMS/Encrypt",
	OpKMSDecrypt:                "quicbaas.KMS/Decrypt",
	OpKMSCreateDEK:              "quicbaas.KMS/CreateDEK",
	OpKMSGetDEK:                 "quicbaas.KMS/GetDEK",
	OpKMSListDEKs:               "quicbaas.KMS/ListDEKs",
	OpKMSRotateDEK:              "quicbaas.KMS/RotateDEK",
	OpKMSDeleteDEK:              "quicbaas.KMS/DeleteDEK",
	OpKMSGetDEKStatus:           "quicbaas.KMS/GetDEKStatus",
	OpKMSSetDefaultDEK:          "quicbaas.KMS/SetDefaultDEK",
	OpKMSSetDEKSafety:           "quicbaas.KMS/SetDEKSafety",
	OpKMSClearDEKSafety:         "quicbaas.KMS/ClearDEKSafety",
	OpKMSBackendStatus:          "quicbaas.KMS/BackendStatus",
	OpKMSBackendTest:            "quicbaas.KMS/BackendTest",
	OpKMSKeyVersions:            "quicbaas.KMS/KeyVersions",
	OpKMSKeyStatus:              "quicbaas.KMS/KeyStatus",
	OpKMSExportKey:              "quicbaas.KMS/ExportKey",
	OpKMSUnwrapKey:              "quicbaas.KMS/UnwrapKey",
	OpKMSGetKeyMetadata:         "quicbaas.KMS/GetKeyMetadata",
	OpKMSSetRotationPolicy:      "quicbaas.KMS/SetKeyRotationPolicy",
	OpKMSGetRotationPolicy:      "quicbaas.KMS/GetKeyRotationPolicy",
	OpKMSDeleteRotationPolicy:   "quicbaas.KMS/DeleteKeyRotationPolicy",
	OpKMSRevokeKey:              "quicbaas.KMS/RevokeKey",
	OpKMSGetPublicKey:           "quicbaas.KMS/GetPublicKey",
	OpKMSGetSupportedAlgorithms: "quicbaas.KMS/GetSupportedAlgorithms",
	OpKMSUpdateKeyMetadata:      "quicbaas.KMS/UpdateKeyMetadata",
	OpKMSSetKeyPolicy:           "quicbaas.KMS/SetKeyPolicy",
	OpKMSGetKeyPolicy:           "quicbaas.KMS/GetKeyPolicy",
	OpKMSSetKeySafety:           "quicbaas.KMS/SetKeySafety",
	OpKMSClearKeySafety:         "quicbaas.KMS/ClearKeySafety",
	OpKMSSafetyCheck:            "quicbaas.KMS/SafetyCheck",
	OpKMSSafetyReport:           "quicbaas.KMS/SafetyReport",

	// ObjectStorageAdmin Operations
	OpOSACreateBackend:                "quicbaas.ObjectStorageAdmin/CreateBackend",
	OpOSAUpdateBackend:                "quicbaas.ObjectStorageAdmin/UpdateBackend",
	OpOSADeleteBackend:                "quicbaas.ObjectStorageAdmin/DeleteBackend",
	OpOSASetBackendStatus:             "quicbaas.ObjectStorageAdmin/SetBackendStatus",
	OpOSAGetStorageStats:              "quicbaas.ObjectStorageAdmin/GetStorageStats",
	OpOSARunGarbageCollection:         "quicbaas.ObjectStorageAdmin/RunGarbageCollection",
	OpOSAGetGarbageCollectionProgress: "quicbaas.ObjectStorageAdmin/GetGarbageCollectionProgress",
	OpOSARepairObject:                 "quicbaas.ObjectStorageAdmin/RepairObject",
	OpOSACompactBackend:               "quicbaas.ObjectStorageAdmin/CompactBackend",

	// Scheduler / Messaging / Barrier / Ceremony are sub-op dispatched on a single
	// opcode (OpScheduler=0x00 shared with OpMsg, OpBarrier=0xC0, OpCeremony=0xC1)
	// and therefore have no 1:1 opcode->method reverse mapping; they are
	// intentionally omitted here. OpScheduler (0x00) is reverse-mapped to the
	// scheduler dispatch name below.
	OpScheduler: "quicbaas.Scheduler",

	// Crypto (transit) Operations (0xF0-0xF9). These were previously shadowed
	// in this reverse map by the stale per-method Scheduler opcodes (0xF0-0xFA);
	// with those removed the canonical Crypto names are restored.
	OpCryptoEncrypt:    "quicbaas.Crypto/Encrypt",
	OpCryptoDecrypt:    "quicbaas.Crypto/Decrypt",
	OpCryptoRewrap:     "quicbaas.Crypto/Rewrap",
	OpCryptoSign:       "quicbaas.Crypto/Sign",
	OpCryptoVerify:     "quicbaas.Crypto/Verify",
	OpCryptoHash:       "quicbaas.Crypto/Hash",
	OpCryptoRandom:     "quicbaas.Crypto/GenerateRandom",
	OpCryptoDataKey:    "quicbaas.Crypto/GenerateDataKey",
	OpCryptoHMAC:       "quicbaas.Crypto/GenerateHMAC",
	OpCryptoVerifyHMAC: "quicbaas.Crypto/VerifyHMAC",

	// IAM Tenant Operations
	OpIAMCreateTenant: "quicbaas.IAM.Tenant/Create",
	OpIAMGetTenant:    "quicbaas.IAM.Tenant/Get",
	OpIAMUpdateTenant: "quicbaas.IAM.Tenant/Update",
	OpIAMDeleteTenant: "quicbaas.IAM.Tenant/Delete",
	OpIAMListTenants:  "quicbaas.IAM.Tenant/List",

	// IAM User Operations
	OpIAMCreateUser: "quicbaas.IAM.User/Create",
	OpIAMGetUser:    "quicbaas.IAM.User/Get",
	OpIAMUpdateUser: "quicbaas.IAM.User/Update",
	OpIAMDeleteUser: "quicbaas.IAM.User/Delete",
	OpIAMListUsers:  "quicbaas.IAM.User/List",
	OpIAMAssignRole: "quicbaas.IAM.User/AssignRole",
	OpIAMRevokeRole: "quicbaas.IAM.User/RevokeRole",

	// IAM Group Operations
	OpIAMCreateGroup:         "quicbaas.IAM.Group/Create",
	OpIAMGetGroup:            "quicbaas.IAM.Group/Get",
	OpIAMUpdateGroup:         "quicbaas.IAM.Group/Update",
	OpIAMDeleteGroup:         "quicbaas.IAM.Group/Delete",
	OpIAMListGroups:          "quicbaas.IAM.Group/List",
	OpIAMAddUserToGroup:      "quicbaas.IAM.Group/AddMember",
	OpIAMRemoveUserFromGroup: "quicbaas.IAM.Group/RemoveMember",

	// IAM Role Operations
	OpIAMCreateRole: "quicbaas.IAM.Role/Create",
	OpIAMGetRole:    "quicbaas.IAM.Role/Get",
	OpIAMUpdateRole: "quicbaas.IAM.Role/Update",
	OpIAMDeleteRole: "quicbaas.IAM.Role/Delete",
	OpIAMListRoles:  "quicbaas.IAM.Role/List",

	// IAM App Operations
	OpIAMRegisterApp:     "quicbaas.IAM.App/Register",
	OpIAMGetApp:          "quicbaas.IAM.App/Get",
	OpIAMUpdateApp:       "quicbaas.IAM.App/Update",
	OpIAMDeleteApp:       "quicbaas.IAM.App/Delete",
	OpIAMListApps:        "quicbaas.IAM.App/List",
	OpIAMRotateAppSecret: "quicbaas.IAM.App/RotateSecret",

	// IAM Policy Operations
	OpIAMCreatePolicy:   "quicbaas.IAM.Policy/Create",
	OpIAMGetPolicy:      "quicbaas.IAM.Policy/Get",
	OpIAMUpdatePolicy:   "quicbaas.IAM.Policy/Update",
	OpIAMDeletePolicy:   "quicbaas.IAM.Policy/Delete",
	OpIAMListPolicies:   "quicbaas.IAM.Policy/List",
	OpIAMValidatePolicy: "quicbaas.IAM.Policy/Validate",
	OpIAMTestPolicy:     "quicbaas.IAM.Policy/Test",

	// IAM MFA Operations
	OpMFA: "quicbaas.IAM.MFA/Dispatch",

	// Orchestrator Operations (sub-op dispatch; reverse lookup gives the family)
	OpOrchestrator: "quicbaas.Orchestrator/Dispatch",

	// Vector Operations
	OpVectorCreateNamespace:       "quicbaas.Vector/CreateNamespace",
	OpVectorDeleteNamespace:       "quicbaas.Vector/DeleteNamespace",
	OpVectorListNamespaces:        "quicbaas.Vector/ListNamespaces",
	OpVectorGetNamespaceInfo:      "quicbaas.Vector/GetNamespaceInfo",
	OpVectorUpsert:                "quicbaas.Vector/Upsert",
	OpVectorGet:                   "quicbaas.Vector/Get",
	OpVectorDelete:                "quicbaas.Vector/Delete",
	OpVectorBatchUpsert:           "quicbaas.Vector/BatchUpsert",
	OpVectorSearchSimilar:         "quicbaas.Vector/SearchSimilar",
	OpVectorSearchByMetadata:      "quicbaas.Vector/SearchByMetadata",
	OpVectorHybridSearch:          "quicbaas.Vector/HybridSearch",
	OpVectorRebuildIndex:          "quicbaas.Vector/RebuildIndex",
	OpVectorGetRebuildProgress:    "quicbaas.Vector/GetRebuildProgress",
	OpVectorGetIndexStats:         "quicbaas.Vector/GetIndexStats",
	OpVectorCompact:               "quicbaas.Vector/Compact",
	OpVectorGetCompactionProgress: "quicbaas.Vector/GetCompactionProgress",
}

// MethodToOpcode converts a service/method combination to an opcode.
func MethodToOpcode(service, method string) (byte, bool) {
	key := service + "/" + method
	opcode, ok := methodToOpcode[key]
	return opcode, ok
}

// OpcodeToMethod converts an opcode to a service/method combination.
func OpcodeToMethod(opcode byte) (string, bool) {
	method, ok := opcodeToMethod[opcode]
	return method, ok
}
