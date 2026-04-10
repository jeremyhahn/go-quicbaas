//go:build quic && iam

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
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// getIAMResourceAndAction maps IAM opcodes to resources and actions for authorization.
// This function handles both direct IAM opcodes (0x95-0xCE) and legacy sub-operation
// pattern opcodes (0xF0-0xFF).
func getIAMResourceAndAction(opcode byte) (resource, action string, handled bool) {
	switch opcode {
	// ==========================================================================
	// IAM Tenant Operations (0x95-0x99)
	// ==========================================================================
	case OpIAMCreateTenant:
		return interfaces.ResourceIAMTenant, interfaces.ActionWrite, true
	case OpIAMGetTenant, OpIAMListTenants:
		return interfaces.ResourceIAMTenant, interfaces.ActionRead, true
	case OpIAMUpdateTenant:
		return interfaces.ResourceIAMTenant, interfaces.ActionWrite, true
	case OpIAMDeleteTenant:
		return interfaces.ResourceIAMTenant, interfaces.ActionDelete, true

	// ==========================================================================
	// IAM User Operations (0x9A-0x9E)
	// ==========================================================================
	case OpIAMCreateUser:
		return interfaces.ResourceIAMUser, interfaces.ActionWrite, true
	case OpIAMGetUser, OpIAMListUsers:
		return interfaces.ResourceIAMUser, interfaces.ActionRead, true
	case OpIAMUpdateUser:
		return interfaces.ResourceIAMUser, interfaces.ActionWrite, true
	case OpIAMDeleteUser:
		return interfaces.ResourceIAMUser, interfaces.ActionDelete, true

	// ==========================================================================
	// IAM Group Operations (0x9F, 0xAB-0xAF, 0xB5)
	// ==========================================================================
	case OpIAMCreateGroup:
		return interfaces.ResourceIAMGroup, interfaces.ActionWrite, true
	case OpIAMGetGroup, OpIAMListGroups:
		return interfaces.ResourceIAMGroup, interfaces.ActionRead, true
	case OpIAMUpdateGroup:
		return interfaces.ResourceIAMGroup, interfaces.ActionWrite, true
	case OpIAMDeleteGroup:
		return interfaces.ResourceIAMGroup, interfaces.ActionDelete, true
	case OpIAMAddUserToGroup, OpIAMRemoveUserFromGroup:
		return interfaces.ResourceIAMGroup, interfaces.ActionWrite, true

	// ==========================================================================
	// IAM Role Operations (0xB6-0xBC)
	// ==========================================================================
	case OpIAMCreateRole:
		return interfaces.ResourceIAMRole, interfaces.ActionWrite, true
	case OpIAMGetRole, OpIAMListRoles:
		return interfaces.ResourceIAMRole, interfaces.ActionRead, true
	case OpIAMUpdateRole:
		return interfaces.ResourceIAMRole, interfaces.ActionWrite, true
	case OpIAMDeleteRole:
		return interfaces.ResourceIAMRole, interfaces.ActionDelete, true
	case OpIAMAssignRole, OpIAMRevokeRole:
		return interfaces.ResourceIAMRole, interfaces.ActionWrite, true

	// ==========================================================================
	// IAM App Operations (0xBD-0xBF, 0xC5-0xC7)
	// ==========================================================================
	case OpIAMRegisterApp:
		return interfaces.ResourceIAMApp, interfaces.ActionWrite, true
	case OpIAMGetApp, OpIAMListApps:
		return interfaces.ResourceIAMApp, interfaces.ActionRead, true
	case OpIAMUpdateApp:
		return interfaces.ResourceIAMApp, interfaces.ActionWrite, true
	case OpIAMDeleteApp:
		return interfaces.ResourceIAMApp, interfaces.ActionDelete, true
	case OpIAMRotateAppSecret:
		return interfaces.ResourceIAMApp, interfaces.ActionAdmin, true

	// ==========================================================================
	// IAM Policy Operations (0xC8-0xCE)
	// ==========================================================================
	case OpIAMCreatePolicy:
		return interfaces.ResourceIAMPolicy, interfaces.ActionWrite, true
	case OpIAMGetPolicy, OpIAMListPolicies:
		return interfaces.ResourceIAMPolicy, interfaces.ActionRead, true
	case OpIAMUpdatePolicy:
		return interfaces.ResourceIAMPolicy, interfaces.ActionWrite, true
	case OpIAMDeletePolicy:
		return interfaces.ResourceIAMPolicy, interfaces.ActionDelete, true
	case OpIAMValidatePolicy, OpIAMTestPolicy:
		return interfaces.ResourceIAMPolicy, interfaces.ActionRead, true

	// ==========================================================================
	// Legacy Sub-Operation Pattern (0xF0-0xFF)
	// These use a parent opcode with sub-operations in the payload
	// ==========================================================================
	case OpIAMAdmin:
		return interfaces.ResourceIAM, interfaces.ActionAdmin, true
	case OpIAMGDPR:
		return interfaces.ResourceIAM, interfaces.ActionAdmin, true
	case OpIAMFederation:
		return interfaces.ResourceIAM, interfaces.ActionWrite, true
	case OpIAMTenant:
		return interfaces.ResourceIAMTenant, interfaces.ActionWrite, true
	case OpIAMUser:
		return interfaces.ResourceIAMUser, interfaces.ActionWrite, true
	case OpIAMGroup:
		return interfaces.ResourceIAMGroup, interfaces.ActionWrite, true
	case OpIAMRole:
		return interfaces.ResourceIAMRole, interfaces.ActionWrite, true
	case OpIAMPolicy:
		return interfaces.ResourceIAMPolicy, interfaces.ActionWrite, true
	case OpIAMApp:
		return interfaces.ResourceIAMApp, interfaces.ActionWrite, true
	case OpMFA:
		return interfaces.ResourceIAM, interfaces.ActionWrite, true
	case OpPEP:
		return interfaces.ResourceIAM, interfaces.ActionRead, true

	// ==========================================================================
	// Direct PEP Operations (0x5B-0x5D)
	// ==========================================================================
	case OpPEPCheck, OpPEPCheckBatch, OpPEPSubscribe:
		return interfaces.ResourceIAM, interfaces.ActionRead, true

	default:
		return "", "", false
	}
}
