//go:build iam

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

// Package main provides structure tests for IAM CLI commands.
package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewIAMGroupCommand_Structure tests group command structure.
func TestNewIAMGroupCommand_Structure(t *testing.T) {
	cmd := newIAMGroupCommand()
	require.NotNil(t, cmd)
	assert.Equal(t, "group", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotEmpty(t, cmd.Long)

	// Verify subcommands
	expectedSubcmds := []string{
		"create", "get", "update", "delete", "list",
		"add-member", "remove-member", "members",
	}

	for _, expected := range expectedSubcmds {
		found := false
		for _, sub := range cmd.Commands() {
			if sub.Use == expected || sub.Name() == expected {
				found = true
				break
			}
			// Handle commands with arguments like "create <id>"
			if len(sub.Use) >= len(expected) && sub.Use[:len(expected)] == expected {
				found = true
				break
			}
		}
		assert.True(t, found, "Expected subcommand %s not found", expected)
	}
}

// TestNewIAMRoleCommand_Structure tests role command structure.
func TestNewIAMRoleCommand_Structure(t *testing.T) {
	cmd := newIAMRoleCommand()
	require.NotNil(t, cmd)
	assert.Equal(t, "role", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
}

// TestNewIAMPolicyCommand_Structure tests policy command structure.
func TestNewIAMPolicyCommand_Structure(t *testing.T) {
	cmd := newIAMPolicyCommand()
	require.NotNil(t, cmd)
	assert.Equal(t, "policy", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
}

// TestNewIAMAppCommand_Structure tests app command structure.
func TestNewIAMAppCommand_Structure(t *testing.T) {
	cmd := newIAMAppCommand()
	require.NotNil(t, cmd)
	assert.Equal(t, "app", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
}

// TestNewIAMSessionCommand_Structure tests session command structure.
func TestNewIAMSessionCommand_Structure(t *testing.T) {
	cmd := newIAMSessionCommand()
	require.NotNil(t, cmd)
	assert.Equal(t, "session", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
}

// TestNewIAMAuditCommand_Structure tests audit command structure.
func TestNewIAMAuditCommand_Structure(t *testing.T) {
	cmd := newIAMAuditCommand()
	require.NotNil(t, cmd)
	assert.Equal(t, "audit", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
}

// TestNewIAMGDPRCommand_Structure tests GDPR command structure.
func TestNewIAMGDPRCommand_Structure(t *testing.T) {
	cmd := newIAMGDPRCommand()
	require.NotNil(t, cmd)
	assert.Equal(t, "gdpr", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
}

// TestNewIAMAdminCommand_Structure tests admin command structure.
func TestNewIAMAdminCommand_Structure(t *testing.T) {
	cmd := newIAMAdminCommand()
	require.NotNil(t, cmd)
	assert.Equal(t, "admin", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
}

// TestNewIAMMFACommand_Structure tests MFA command structure.
func TestNewIAMMFACommand_Structure(t *testing.T) {
	cmd := newIAMMFACommand()
	require.NotNil(t, cmd)
	assert.Equal(t, "mfa", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
}

// TestNewIAMPEPCommand_Structure tests PEP command structure.
func TestNewIAMPEPCommand_Structure(t *testing.T) {
	cmd := newIAMPEPCommand()
	require.NotNil(t, cmd)
	assert.Equal(t, "pep", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
}

// TestNewIAMFederationCommand_Structure tests federation command structure.
func TestNewIAMFederationCommand_Structure(t *testing.T) {
	providerCmd := newIAMProviderCommand()
	require.NotNil(t, providerCmd)
	assert.Equal(t, "provider", providerCmd.Use)

	identityCmd := newIAMIdentityCommand()
	require.NotNil(t, identityCmd)
	assert.Equal(t, "identity", identityCmd.Use)
}

// TestNewIAMTenantCommand_Structure tests tenant command structure.
func TestNewIAMTenantCommand_Structure(t *testing.T) {
	cmd := newIAMTenantCommand()
	require.NotNil(t, cmd)
	assert.Equal(t, "tenant", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
}

// TestNewIAMUserCommand_Structure tests user command structure.
func TestNewIAMUserCommand_Structure(t *testing.T) {
	cmd := newIAMUserCommand()
	require.NotNil(t, cmd)
	assert.Equal(t, "user", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
}
