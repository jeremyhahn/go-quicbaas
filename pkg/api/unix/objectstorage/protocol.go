//go:build unix

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

// Package objectstorage provides Unix socket API handlers for object storage operations.
// It implements a binary protocol with JSON-encoded payloads for high-performance
// local inter-process communication.
package objectstorage

// Protocol constants for ObjectStorage Unix socket API.
const (
	// ProtocolVersion is the current protocol version.
	ProtocolVersion byte = 0x01

	// MaxPayloadSize is the maximum payload size (64MB for large objects).
	MaxPayloadSize uint32 = 64 * 1024 * 1024

	// MaxTokenSize is the maximum authentication token size (64KB - 1).
	//
	// Production tokens are RFC 9068 at+jwt access tokens whose scp and
	// roles claims scale with the subject's qrdb RBAC grants; real tokens
	// have been observed at 11,060 bytes (past the previous 8KB cap).
	// 65535 is the ceiling of the 2-byte wire length prefix, so this is a
	// pure constant change with no frame-layout change. Must stay
	// identical to the QUIC codec bound in pkg/api/quic and
	// sdk/go/transport/quic.
	MaxTokenSize uint16 = 64*1024 - 1
)

// Operation codes for object storage operations.
// Starting at 0x70 to avoid conflicts with other Unix socket APIs.
const (
	// CRUD Operations (0x70-0x73).
	OpPutObject    byte = 0x70
	OpGetObject    byte = 0x71
	OpDeleteObject byte = 0x72
	OpHeadObject   byte = 0x73

	// Listing Operations (0x74-0x76).
	OpListObjects  byte = 0x74
	OpListByTag    byte = 0x75
	OpListByTenant byte = 0x76

	// Copy Operations (0x77).
	OpCopyObject byte = 0x77

	// Replication & Migration Operations (0x78-0x79).
	OpReplicateObject byte = 0x78
	OpMigrateObject   byte = 0x79

	// Archive & Restore Operations (0x7A-0x7B).
	OpArchiveObject byte = 0x7A
	OpRestoreObject byte = 0x7B

	// Verification Operations (0x7C).
	OpVerifyObject byte = 0x7C

	// Policy Operations (0x7D-0x7E).
	OpSetObjectPolicy byte = 0x7D
	OpGetObjectPolicy byte = 0x7E

	// Backend Management Operations (0x7F-0x80).
	OpListBackends     byte = 0x7F
	OpGetBackendHealth byte = 0x80

	// Tenant Management Operations (0x81-0x83).
	OpGetTenantUsage  byte = 0x81
	OpGetTenantQuotas byte = 0x82
	OpSetTenantQuotas byte = 0x83

	// Batch Operations (0x84-0x86).
	OpBatchPutObjects    byte = 0x84
	OpBatchGetObjects    byte = 0x85
	OpBatchDeleteObjects byte = 0x86

	// Admin Backend Operations (0x90-0x93).
	OpAdminCreateBackend    byte = 0x90
	OpAdminUpdateBackend    byte = 0x91
	OpAdminDeleteBackend    byte = 0x92
	OpAdminSetBackendStatus byte = 0x93

	// Admin Statistics Operations (0x94).
	OpAdminGetStorageStats byte = 0x94

	// Admin GC Operations (0x95-0x96).
	OpAdminRunGC         byte = 0x95
	OpAdminGetGCProgress byte = 0x96

	// Admin Repair Operations (0x97).
	OpAdminRepairObject byte = 0x97

	// Admin Compaction Operations (0x98).
	OpAdminCompactBackend byte = 0x98

	// Object Transfer Operations (0xA0-0xA1).
	// Used for peer-to-peer object data transfer during lazy snapshot recovery.
	OpFetchObject        byte = 0xA0
	OpStreamFetchObjects byte = 0xA1
)

// Status codes for responses.
const (
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
	StatusForbidden         byte = 0x0B
	StatusStorage           byte = 0x0C
)

// OpcodeToString returns a human-readable name for an opcode.
func OpcodeToString(opcode byte) string {
	names := map[byte]string{
		OpPutObject:             "PutObject",
		OpGetObject:             "GetObject",
		OpDeleteObject:          "DeleteObject",
		OpHeadObject:            "HeadObject",
		OpListObjects:           "ListObjects",
		OpListByTag:             "ListByTag",
		OpListByTenant:          "ListByTenant",
		OpCopyObject:            "CopyObject",
		OpReplicateObject:       "ReplicateObject",
		OpMigrateObject:         "MigrateObject",
		OpArchiveObject:         "ArchiveObject",
		OpRestoreObject:         "RestoreObject",
		OpVerifyObject:          "VerifyObject",
		OpSetObjectPolicy:       "SetObjectPolicy",
		OpGetObjectPolicy:       "GetObjectPolicy",
		OpListBackends:          "ListBackends",
		OpGetBackendHealth:      "GetBackendHealth",
		OpGetTenantUsage:        "GetTenantUsage",
		OpGetTenantQuotas:       "GetTenantQuotas",
		OpSetTenantQuotas:       "SetTenantQuotas",
		OpBatchPutObjects:       "BatchPutObjects",
		OpBatchGetObjects:       "BatchGetObjects",
		OpBatchDeleteObjects:    "BatchDeleteObjects",
		OpAdminCreateBackend:    "AdminCreateBackend",
		OpAdminUpdateBackend:    "AdminUpdateBackend",
		OpAdminDeleteBackend:    "AdminDeleteBackend",
		OpAdminSetBackendStatus: "AdminSetBackendStatus",
		OpAdminGetStorageStats:  "AdminGetStorageStats",
		OpAdminRunGC:            "AdminRunGC",
		OpAdminGetGCProgress:    "AdminGetGCProgress",
		OpAdminRepairObject:     "AdminRepairObject",
		OpAdminCompactBackend:   "AdminCompactBackend",
		OpFetchObject:           "FetchObject",
		OpStreamFetchObjects:    "StreamFetchObjects",
	}

	if name, ok := names[opcode]; ok {
		return name
	}
	return "Unknown"
}
