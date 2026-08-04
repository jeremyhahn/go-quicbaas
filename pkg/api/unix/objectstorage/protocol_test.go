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

package objectstorage

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestOpcodeToString(t *testing.T) {
	tests := []struct {
		name     string
		opcode   byte
		expected string
	}{
		{name: "PutObject", opcode: OpPutObject, expected: "PutObject"},
		{name: "GetObject", opcode: OpGetObject, expected: "GetObject"},
		{name: "DeleteObject", opcode: OpDeleteObject, expected: "DeleteObject"},
		{name: "HeadObject", opcode: OpHeadObject, expected: "HeadObject"},
		{name: "ListObjects", opcode: OpListObjects, expected: "ListObjects"},
		{name: "ListByTag", opcode: OpListByTag, expected: "ListByTag"},
		{name: "ListByTenant", opcode: OpListByTenant, expected: "ListByTenant"},
		{name: "CopyObject", opcode: OpCopyObject, expected: "CopyObject"},
		{name: "ReplicateObject", opcode: OpReplicateObject, expected: "ReplicateObject"},
		{name: "MigrateObject", opcode: OpMigrateObject, expected: "MigrateObject"},
		{name: "ArchiveObject", opcode: OpArchiveObject, expected: "ArchiveObject"},
		{name: "RestoreObject", opcode: OpRestoreObject, expected: "RestoreObject"},
		{name: "VerifyObject", opcode: OpVerifyObject, expected: "VerifyObject"},
		{name: "SetObjectPolicy", opcode: OpSetObjectPolicy, expected: "SetObjectPolicy"},
		{name: "GetObjectPolicy", opcode: OpGetObjectPolicy, expected: "GetObjectPolicy"},
		{name: "ListBackends", opcode: OpListBackends, expected: "ListBackends"},
		{name: "GetBackendHealth", opcode: OpGetBackendHealth, expected: "GetBackendHealth"},
		{name: "GetTenantUsage", opcode: OpGetTenantUsage, expected: "GetTenantUsage"},
		{name: "GetTenantQuotas", opcode: OpGetTenantQuotas, expected: "GetTenantQuotas"},
		{name: "SetTenantQuotas", opcode: OpSetTenantQuotas, expected: "SetTenantQuotas"},
		{name: "BatchPutObjects", opcode: OpBatchPutObjects, expected: "BatchPutObjects"},
		{name: "BatchGetObjects", opcode: OpBatchGetObjects, expected: "BatchGetObjects"},
		{name: "BatchDeleteObjects", opcode: OpBatchDeleteObjects, expected: "BatchDeleteObjects"},
		{name: "AdminCreateBackend", opcode: OpAdminCreateBackend, expected: "AdminCreateBackend"},
		{name: "AdminUpdateBackend", opcode: OpAdminUpdateBackend, expected: "AdminUpdateBackend"},
		{name: "AdminDeleteBackend", opcode: OpAdminDeleteBackend, expected: "AdminDeleteBackend"},
		{name: "AdminSetBackendStatus", opcode: OpAdminSetBackendStatus, expected: "AdminSetBackendStatus"},
		{name: "AdminGetStorageStats", opcode: OpAdminGetStorageStats, expected: "AdminGetStorageStats"},
		{name: "AdminRunGC", opcode: OpAdminRunGC, expected: "AdminRunGC"},
		{name: "AdminGetGCProgress", opcode: OpAdminGetGCProgress, expected: "AdminGetGCProgress"},
		{name: "AdminRepairObject", opcode: OpAdminRepairObject, expected: "AdminRepairObject"},
		{name: "AdminCompactBackend", opcode: OpAdminCompactBackend, expected: "AdminCompactBackend"},
		{name: "Unknown", opcode: 0xFF, expected: "Unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := OpcodeToString(tt.opcode)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestOpcodeConstants(t *testing.T) {
	// Verify opcodes start at 0x70 and don't conflict
	assert.Equal(t, byte(0x70), OpPutObject)
	assert.Equal(t, byte(0x71), OpGetObject)
	assert.Equal(t, byte(0x72), OpDeleteObject)
	assert.Equal(t, byte(0x73), OpHeadObject)

	// Verify admin opcodes start at 0x90
	assert.Equal(t, byte(0x90), OpAdminCreateBackend)
	assert.Equal(t, byte(0x98), OpAdminCompactBackend)
}

func TestStatusConstants(t *testing.T) {
	assert.Equal(t, byte(0x00), StatusOK)
	assert.Equal(t, byte(0x01), StatusInvalidRequest)
	assert.Equal(t, byte(0x02), StatusNotFound)
	assert.Equal(t, byte(0x03), StatusUnauthorized)
	assert.Equal(t, byte(0x04), StatusInternalError)
	assert.Equal(t, byte(0x05), StatusTimeout)
	assert.Equal(t, byte(0x06), StatusAlreadyExists)
	assert.Equal(t, byte(0x07), StatusPermissionDenied)
	assert.Equal(t, byte(0x08), StatusResourceExhausted)
	assert.Equal(t, byte(0x09), StatusInvalidArgument)
	assert.Equal(t, byte(0x0A), StatusUnavailable)
	assert.Equal(t, byte(0x0B), StatusForbidden)
	assert.Equal(t, byte(0x0C), StatusStorage)
}

func TestProtocolConstants(t *testing.T) {
	assert.Equal(t, byte(0x01), ProtocolVersion)
	assert.Equal(t, uint32(64*1024*1024), MaxPayloadSize)
	assert.Equal(t, uint16(64*1024-1), MaxTokenSize)
}
