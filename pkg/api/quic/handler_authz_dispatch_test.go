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
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// =============================================================================
// getIAMResourceAndAction Tests - Comprehensive Coverage
// =============================================================================

func TestGetIAMResourceAndAction_TenantOperations(t *testing.T) {
	tests := []struct {
		opcode   byte
		resource string
		action   string
		handled  bool
	}{
		{OpIAMCreateTenant, interfaces.ResourceIAMTenant, interfaces.ActionWrite, true},
		{OpIAMGetTenant, interfaces.ResourceIAMTenant, interfaces.ActionRead, true},
		{OpIAMUpdateTenant, interfaces.ResourceIAMTenant, interfaces.ActionWrite, true},
		{OpIAMDeleteTenant, interfaces.ResourceIAMTenant, interfaces.ActionDelete, true},
		{OpIAMListTenants, interfaces.ResourceIAMTenant, interfaces.ActionRead, true},
	}

	for _, tt := range tests {
		t.Run(OpcodeToString(tt.opcode), func(t *testing.T) {
			resource, action, handled := getIAMResourceAndAction(tt.opcode)
			if resource != tt.resource {
				t.Fatalf("Expected resource %q, got %q", tt.resource, resource)
			}
			if action != tt.action {
				t.Fatalf("Expected action %q, got %q", tt.action, action)
			}
			if handled != tt.handled {
				t.Fatalf("Expected handled %v, got %v", tt.handled, handled)
			}
		})
	}
}

func TestGetIAMResourceAndAction_UserOperations(t *testing.T) {
	tests := []struct {
		opcode   byte
		resource string
		action   string
		handled  bool
	}{
		{OpIAMCreateUser, interfaces.ResourceIAMUser, interfaces.ActionWrite, true},
		{OpIAMGetUser, interfaces.ResourceIAMUser, interfaces.ActionRead, true},
		{OpIAMUpdateUser, interfaces.ResourceIAMUser, interfaces.ActionWrite, true},
		{OpIAMDeleteUser, interfaces.ResourceIAMUser, interfaces.ActionDelete, true},
		{OpIAMListUsers, interfaces.ResourceIAMUser, interfaces.ActionRead, true},
	}

	for _, tt := range tests {
		t.Run(OpcodeToString(tt.opcode), func(t *testing.T) {
			resource, action, handled := getIAMResourceAndAction(tt.opcode)
			if resource != tt.resource {
				t.Fatalf("Expected resource %q, got %q", tt.resource, resource)
			}
			if action != tt.action {
				t.Fatalf("Expected action %q, got %q", tt.action, action)
			}
			if handled != tt.handled {
				t.Fatalf("Expected handled %v, got %v", tt.handled, handled)
			}
		})
	}
}

func TestGetIAMResourceAndAction_GroupOperations(t *testing.T) {
	tests := []struct {
		opcode   byte
		resource string
		action   string
		handled  bool
	}{
		{OpIAMCreateGroup, interfaces.ResourceIAMGroup, interfaces.ActionWrite, true},
		{OpIAMGetGroup, interfaces.ResourceIAMGroup, interfaces.ActionRead, true},
		{OpIAMUpdateGroup, interfaces.ResourceIAMGroup, interfaces.ActionWrite, true},
		{OpIAMDeleteGroup, interfaces.ResourceIAMGroup, interfaces.ActionDelete, true},
		{OpIAMListGroups, interfaces.ResourceIAMGroup, interfaces.ActionRead, true},
		{OpIAMAddUserToGroup, interfaces.ResourceIAMGroup, interfaces.ActionWrite, true},
		{OpIAMRemoveUserFromGroup, interfaces.ResourceIAMGroup, interfaces.ActionWrite, true},
	}

	for _, tt := range tests {
		t.Run(OpcodeToString(tt.opcode), func(t *testing.T) {
			resource, action, handled := getIAMResourceAndAction(tt.opcode)
			if resource != tt.resource {
				t.Fatalf("Expected resource %q, got %q", tt.resource, resource)
			}
			if action != tt.action {
				t.Fatalf("Expected action %q, got %q", tt.action, action)
			}
			if handled != tt.handled {
				t.Fatalf("Expected handled %v, got %v", tt.handled, handled)
			}
		})
	}
}

func TestGetIAMResourceAndAction_RoleOperations(t *testing.T) {
	tests := []struct {
		opcode   byte
		resource string
		action   string
		handled  bool
	}{
		{OpIAMCreateRole, interfaces.ResourceIAMRole, interfaces.ActionWrite, true},
		{OpIAMGetRole, interfaces.ResourceIAMRole, interfaces.ActionRead, true},
		{OpIAMUpdateRole, interfaces.ResourceIAMRole, interfaces.ActionWrite, true},
		{OpIAMDeleteRole, interfaces.ResourceIAMRole, interfaces.ActionDelete, true},
		{OpIAMListRoles, interfaces.ResourceIAMRole, interfaces.ActionRead, true},
		{OpIAMAssignRole, interfaces.ResourceIAMRole, interfaces.ActionWrite, true},
		{OpIAMRevokeRole, interfaces.ResourceIAMRole, interfaces.ActionWrite, true},
	}

	for _, tt := range tests {
		t.Run(OpcodeToString(tt.opcode), func(t *testing.T) {
			resource, action, handled := getIAMResourceAndAction(tt.opcode)
			if resource != tt.resource {
				t.Fatalf("Expected resource %q, got %q", tt.resource, resource)
			}
			if action != tt.action {
				t.Fatalf("Expected action %q, got %q", tt.action, action)
			}
			if handled != tt.handled {
				t.Fatalf("Expected handled %v, got %v", tt.handled, handled)
			}
		})
	}
}

func TestGetIAMResourceAndAction_AppOperations(t *testing.T) {
	tests := []struct {
		opcode   byte
		resource string
		action   string
		handled  bool
	}{
		{OpIAMRegisterApp, interfaces.ResourceIAMApp, interfaces.ActionWrite, true},
		{OpIAMGetApp, interfaces.ResourceIAMApp, interfaces.ActionRead, true},
		{OpIAMUpdateApp, interfaces.ResourceIAMApp, interfaces.ActionWrite, true},
		{OpIAMDeleteApp, interfaces.ResourceIAMApp, interfaces.ActionDelete, true},
		{OpIAMListApps, interfaces.ResourceIAMApp, interfaces.ActionRead, true},
		{OpIAMRotateAppSecret, interfaces.ResourceIAMApp, interfaces.ActionAdmin, true},
	}

	for _, tt := range tests {
		t.Run(OpcodeToString(tt.opcode), func(t *testing.T) {
			resource, action, handled := getIAMResourceAndAction(tt.opcode)
			if resource != tt.resource {
				t.Fatalf("Expected resource %q, got %q", tt.resource, resource)
			}
			if action != tt.action {
				t.Fatalf("Expected action %q, got %q", tt.action, action)
			}
			if handled != tt.handled {
				t.Fatalf("Expected handled %v, got %v", tt.handled, handled)
			}
		})
	}
}

func TestGetIAMResourceAndAction_PolicyOperations(t *testing.T) {
	tests := []struct {
		opcode   byte
		resource string
		action   string
		handled  bool
	}{
		{OpIAMCreatePolicy, interfaces.ResourceIAMPolicy, interfaces.ActionWrite, true},
		{OpIAMGetPolicy, interfaces.ResourceIAMPolicy, interfaces.ActionRead, true},
		{OpIAMUpdatePolicy, interfaces.ResourceIAMPolicy, interfaces.ActionWrite, true},
		{OpIAMDeletePolicy, interfaces.ResourceIAMPolicy, interfaces.ActionDelete, true},
		{OpIAMListPolicies, interfaces.ResourceIAMPolicy, interfaces.ActionRead, true},
		{OpIAMValidatePolicy, interfaces.ResourceIAMPolicy, interfaces.ActionRead, true},
		{OpIAMTestPolicy, interfaces.ResourceIAMPolicy, interfaces.ActionRead, true},
	}

	for _, tt := range tests {
		t.Run(OpcodeToString(tt.opcode), func(t *testing.T) {
			resource, action, handled := getIAMResourceAndAction(tt.opcode)
			if resource != tt.resource {
				t.Fatalf("Expected resource %q, got %q", tt.resource, resource)
			}
			if action != tt.action {
				t.Fatalf("Expected action %q, got %q", tt.action, action)
			}
			if handled != tt.handled {
				t.Fatalf("Expected handled %v, got %v", tt.handled, handled)
			}
		})
	}
}

func TestGetIAMResourceAndAction_LegacyOperations(t *testing.T) {
	tests := []struct {
		opcode   byte
		resource string
		action   string
		handled  bool
	}{
		{OpIAMAdmin, interfaces.ResourceIAM, interfaces.ActionAdmin, true},
		{OpIAMGDPR, interfaces.ResourceIAM, interfaces.ActionAdmin, true},
		{OpIAMFederation, interfaces.ResourceIAM, interfaces.ActionWrite, true},
		{OpIAMTenant, interfaces.ResourceIAMTenant, interfaces.ActionWrite, true},
		{OpIAMUser, interfaces.ResourceIAMUser, interfaces.ActionWrite, true},
		{OpIAMGroup, interfaces.ResourceIAMGroup, interfaces.ActionWrite, true},
		{OpIAMRole, interfaces.ResourceIAMRole, interfaces.ActionWrite, true},
		{OpIAMPolicy, interfaces.ResourceIAMPolicy, interfaces.ActionWrite, true},
		{OpIAMApp, interfaces.ResourceIAMApp, interfaces.ActionWrite, true},
		{OpMFA, interfaces.ResourceIAM, interfaces.ActionWrite, true},
		{OpPEP, interfaces.ResourceIAM, interfaces.ActionRead, true},
	}

	for _, tt := range tests {
		t.Run(OpcodeToStringIAM(tt.opcode), func(t *testing.T) {
			resource, action, handled := getIAMResourceAndAction(tt.opcode)
			if resource != tt.resource {
				t.Fatalf("Expected resource %q, got %q", tt.resource, resource)
			}
			if action != tt.action {
				t.Fatalf("Expected action %q, got %q", tt.action, action)
			}
			if handled != tt.handled {
				t.Fatalf("Expected handled %v, got %v", tt.handled, handled)
			}
		})
	}
}

func TestGetIAMResourceAndAction_PEPOperations(t *testing.T) {
	tests := []struct {
		opcode   byte
		resource string
		action   string
		handled  bool
	}{
		{OpPEPCheck, interfaces.ResourceIAM, interfaces.ActionRead, true},
		{OpPEPCheckBatch, interfaces.ResourceIAM, interfaces.ActionRead, true},
		{OpPEPSubscribe, interfaces.ResourceIAM, interfaces.ActionRead, true},
	}

	for _, tt := range tests {
		t.Run(OpcodeToString(tt.opcode), func(t *testing.T) {
			resource, action, handled := getIAMResourceAndAction(tt.opcode)
			if resource != tt.resource {
				t.Fatalf("Expected resource %q, got %q", tt.resource, resource)
			}
			if action != tt.action {
				t.Fatalf("Expected action %q, got %q", tt.action, action)
			}
			if handled != tt.handled {
				t.Fatalf("Expected handled %v, got %v", tt.handled, handled)
			}
		})
	}
}

func TestGetIAMResourceAndAction_UnknownOpcode(t *testing.T) {
	resource, action, handled := getIAMResourceAndAction(0x00)
	if resource != "" {
		t.Fatalf("Expected empty resource, got %q", resource)
	}
	if action != "" {
		t.Fatalf("Expected empty action, got %q", action)
	}
	if handled != false {
		t.Fatal("Expected handled to be false")
	}
}

// =============================================================================
// extendOpcodeToString Tests
// =============================================================================

func TestExtendOpcodeToString(t *testing.T) {
	tests := []struct {
		opcode   byte
		expected string
	}{
		{OpIAMAdmin, "IAMAdmin"},
		{OpIAMGDPR, "IAMGDPR"},
		{OpIAMFederation, "IAMFederation"},
		{OpIAMTenant, "IAMTenant"},
		{OpIAMUser, "IAMUser"},
		{OpIAMGroup, "IAMGroup"},
		{OpIAMRole, "IAMRole"},
		{OpIAMPolicy, "IAMPolicy"},
		{OpIAMApp, "IAMApp"},
		{OpMFA, "MFA"},
		{OpPEP, "PEP"},
		{0x00, ""}, // Unknown returns empty
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := extendOpcodeToString(tt.opcode)
			if result != tt.expected {
				t.Fatalf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}
