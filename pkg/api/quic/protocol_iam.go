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

// IAM Operation codes (0xF0-0xFF) - enabled only when iam build tag is set.
// Note: Byte values are limited to 0-255, so we use the remaining opcodes 0xF0-0xFF
// for IAM operations. This provides 16 primary slots which we'll use creatively.
const (
	// IAM Operations Base (0xF0)
	OpIAMBase byte = 0xF0

	// Extended IAM Operations (using lower range)
	// Admin operations: 0xF0 + subop in payload
	OpIAMAdmin byte = 0xF0
	// GDPR operations: 0xF1 + subop in payload
	OpIAMGDPR byte = 0xF1
	// Federation operations: 0xF2 + subop in payload
	OpIAMFederation byte = 0xF2

	// Core IAM Operations (original range)
	// Tenant Operations: 0xF8 + subop in payload
	OpIAMTenant byte = 0xF8
	// User Operations: 0xF9 + subop in payload
	OpIAMUser byte = 0xF9
	// Group Operations: 0xFA + subop in payload
	OpIAMGroup byte = 0xFA
	// Role Operations: 0xFB + subop in payload
	OpIAMRole byte = 0xFB
	// Policy Operations: 0xFC + subop in payload
	OpIAMPolicy byte = 0xFC
	// App Operations: 0xFD + subop in payload
	OpIAMApp byte = 0xFD
	// MFA Operations: 0xFE + subop in payload
	OpMFA byte = 0xFE
	// PEP Operations: 0xFF + subop in payload
	OpPEP byte = 0xFF
)

// IAM Sub-operations for OpIAMAdmin (0xF0).
const (
	SubOpCreateAdminAssignment   byte = 0x00
	SubOpGetAdminAssignment      byte = 0x01
	SubOpUpdateAdminAssignment   byte = 0x02
	SubOpDeleteAdminAssignment   byte = 0x03
	SubOpListAdminAssignments    byte = 0x04
	SubOpCheckAdminPermission    byte = 0x05
	SubOpStartImpersonation      byte = 0x06
	SubOpEndImpersonation        byte = 0x07
	SubOpGetActiveImpersonations byte = 0x08
)

// IAM Sub-operations for OpIAMGDPR (0xF1).
const (
	// Consent sub-operations (0x00-0x0F)
	SubOpGrantConsent     byte = 0x00
	SubOpWithdrawConsent  byte = 0x01
	SubOpGetConsent       byte = 0x02
	SubOpListUserConsents byte = 0x03

	// Export sub-operations (0x10-0x1F)
	SubOpRequestExport   byte = 0x10
	SubOpGetExportStatus byte = 0x11
	SubOpGenerateExport  byte = 0x12

	// Deletion sub-operations (0x20-0x2F)
	SubOpRequestDeletion   byte = 0x20
	SubOpCancelDeletion    byte = 0x21
	SubOpProcessDeletion   byte = 0x22
	SubOpGetDeletionStatus byte = 0x23
)

// IAM Sub-operations for OpIAMFederation (0xF2).
const (
	// Provider management sub-operations (0x00-0x0F).
	SubOpFederationCreateProvider byte = 0x00
	SubOpFederationGetProvider    byte = 0x01
	SubOpFederationUpdateProvider byte = 0x02
	SubOpFederationDeleteProvider byte = 0x03
	SubOpFederationListProviders  byte = 0x04

	// Authentication flow sub-operations (0x10-0x1F).
	SubOpFederationBeginLogin     byte = 0x10
	SubOpFederationHandleCallback byte = 0x11

	// Account linking sub-operations (0x20-0x2F).
	SubOpFederationLinkAccount   byte = 0x20
	SubOpFederationUnlinkAccount byte = 0x21
	SubOpFederationGetUserLinks  byte = 0x22
)

// IAM Sub-operations for OpIAMTenant (0xF8).
const (
	SubOpCreateTenant byte = 0x00
	SubOpGetTenant    byte = 0x01
	SubOpUpdateTenant byte = 0x02
	SubOpDeleteTenant byte = 0x03
	SubOpListTenants  byte = 0x04
)

// IAM Sub-operations for OpIAMUser (0xF9).
const (
	SubOpCreateUser     byte = 0x00
	SubOpGetUser        byte = 0x01
	SubOpUpdateUser     byte = 0x02
	SubOpDeleteUser     byte = 0x03
	SubOpListUsers      byte = 0x04
	SubOpAssignUserRole byte = 0x05
	SubOpRevokeUserRole byte = 0x06
)

// IAM Sub-operations for OpIAMGroup (0xFA).
const (
	SubOpCreateGroup       byte = 0x00
	SubOpGetGroup          byte = 0x01
	SubOpUpdateGroup       byte = 0x02
	SubOpDeleteGroup       byte = 0x03
	SubOpListGroups        byte = 0x04
	SubOpAddGroupMember    byte = 0x05
	SubOpRemoveGroupMember byte = 0x06
	SubOpListGroupMembers  byte = 0x07
)

// IAM Sub-operations for OpIAMRole (0xFB).
const (
	SubOpCreateRole byte = 0x00
	SubOpGetRole    byte = 0x01
	SubOpUpdateRole byte = 0x02
	SubOpDeleteRole byte = 0x03
	SubOpListRoles  byte = 0x04
)

// IAM Sub-operations for OpIAMPolicy (0xFC).
const (
	SubOpCreatePolicy   byte = 0x00
	SubOpGetPolicy      byte = 0x01
	SubOpUpdatePolicy   byte = 0x02
	SubOpDeletePolicy   byte = 0x03
	SubOpListPolicies   byte = 0x04
	SubOpValidatePolicy byte = 0x05
	SubOpTestPolicy     byte = 0x06
)

// IAM Sub-operations for OpIAMApp (0xFD).
const (
	SubOpRegisterApp     byte = 0x00
	SubOpGetApp          byte = 0x01
	SubOpUpdateApp       byte = 0x02
	SubOpDeleteApp       byte = 0x03
	SubOpListApps        byte = 0x04
	SubOpRotateAppSecret byte = 0x05
)

// IAM Sub-operations for OpMFA (0xFE).
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

// IAM Sub-operations for OpPEP (0xFF).
const (
	SubOpPEPCheck      byte = 0x00
	SubOpPEPCheckBatch byte = 0x01
)

// OpcodeToStringIAM extends OpcodeToString with IAM-specific opcodes.
// This function is called from the base OpcodeToString when build tag iam is set.
func OpcodeToStringIAM(opcode byte) string {
	switch opcode {
	case OpIAMAdmin:
		return "IAMAdmin"
	case OpIAMGDPR:
		return "IAMGDPR"
	case OpIAMFederation:
		return "IAMFederation"
	case OpIAMTenant:
		return "IAMTenant"
	case OpIAMUser:
		return "IAMUser"
	case OpIAMGroup:
		return "IAMGroup"
	case OpIAMRole:
		return "IAMRole"
	case OpIAMPolicy:
		return "IAMPolicy"
	case OpIAMApp:
		return "IAMApp"
	case OpMFA:
		return "MFA"
	case OpPEP:
		return "PEP"
	default:
		return ""
	}
}
