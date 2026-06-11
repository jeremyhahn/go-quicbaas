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

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
)

func iamTenantUserTestInitLogger(t *testing.T) func() {
	origLog := log
	logConfig := logger.LogConfig{
		Level:     "error",
		Output:    "stderr",
		Component: "test",
	}
	testLog, err := logger.New(logConfig)
	require.NoError(t, err)
	log = testLog
	return func() {
		log = origLog
	}
}

// =============================================================================
// IAM Tenant Command Tests - Factory Error Paths
// =============================================================================

func TestIAMTenantCreateCommand_FactoryError(t *testing.T) {
	cleanup := iamTenantUserTestInitLogger(t)
	defer cleanup()

	origFactory := iamClientFactory
	defer func() { iamClientFactory = origFactory }()

	iamClientFactory = func() (iam.Client, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "connection failed")
	}

	cmd := newIAMTenantCreateCommand()
	tenantName = "Test Tenant"

	err := runIAMTenantCreateCommand(cmd, []string{"test-tenant"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "connection failed")
}

func TestIAMTenantCreateCommand_EmptyTenantID(t *testing.T) {
	cleanup := iamTenantUserTestInitLogger(t)
	defer cleanup()

	cmd := newIAMTenantCreateCommand()

	err := runIAMTenantCreateCommand(cmd, []string{""})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "tenant ID is required")
}

func TestIAMTenantGetCommand_FactoryError(t *testing.T) {
	cleanup := iamTenantUserTestInitLogger(t)
	defer cleanup()

	origFactory := iamClientFactory
	defer func() { iamClientFactory = origFactory }()

	iamClientFactory = func() (iam.Client, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "connection failed")
	}

	cmd := newIAMTenantGetCommand()

	err := runIAMTenantGetCommand(cmd, []string{"test-tenant"})
	assert.Error(t, err)
}

func TestIAMTenantUpdateCommand_FactoryError(t *testing.T) {
	cleanup := iamTenantUserTestInitLogger(t)
	defer cleanup()

	origFactory := iamClientFactory
	defer func() { iamClientFactory = origFactory }()

	iamClientFactory = func() (iam.Client, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "connection failed")
	}

	cmd := newIAMTenantUpdateCommand()
	cmd.Flags().Set("name", "New Name")

	err := runIAMTenantUpdateCommand(cmd, []string{"test-tenant"})
	assert.Error(t, err)
}

func TestIAMTenantUpdateCommand_NoFlagsProvided(t *testing.T) {
	cleanup := iamTenantUserTestInitLogger(t)
	defer cleanup()

	cmd := newIAMTenantUpdateCommand()

	err := runIAMTenantUpdateCommand(cmd, []string{"test-tenant"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "at least one of")
}

func TestIAMTenantDeleteCommand_FactoryError(t *testing.T) {
	cleanup := iamTenantUserTestInitLogger(t)
	defer cleanup()

	origFactory := iamClientFactory
	defer func() { iamClientFactory = origFactory }()

	iamClientFactory = func() (iam.Client, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "connection failed")
	}

	cmd := newIAMTenantDeleteCommand()
	iamForce = true

	err := runIAMTenantDeleteCommand(cmd, []string{"test-tenant"})
	assert.Error(t, err)
}

func TestIAMTenantListCommand_FactoryError(t *testing.T) {
	cleanup := iamTenantUserTestInitLogger(t)
	defer cleanup()

	origFactory := iamClientFactory
	defer func() { iamClientFactory = origFactory }()

	iamClientFactory = func() (iam.Client, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "connection failed")
	}

	cmd := newIAMTenantListCommand()

	err := runIAMTenantListCommand(cmd, []string{})
	assert.Error(t, err)
}

// =============================================================================
// IAM User Command Tests - Factory Error Paths
// =============================================================================

func TestIAMUserCreateCommand_FactoryError(t *testing.T) {
	cleanup := iamTenantUserTestInitLogger(t)
	defer cleanup()

	origFactory := iamClientFactory
	defer func() { iamClientFactory = origFactory }()

	iamClientFactory = func() (iam.Client, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "connection failed")
	}

	cmd := newIAMUserCreateCommand()
	globalTenantID = "test-tenant"
	iamUserName = "Test User"

	err := runIAMUserCreateCommand(cmd, []string{"testuser@example.com"})
	assert.Error(t, err)
}

func TestIAMUserCreateCommand_MissingTenant(t *testing.T) {
	cleanup := iamTenantUserTestInitLogger(t)
	defer cleanup()

	cmd := newIAMUserCreateCommand()
	globalTenantID = ""
	iamUserName = "Test User"

	err := runIAMUserCreateCommand(cmd, []string{"testuser@example.com"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--tenant flag is required")
}

func TestIAMUserGetCommand_FactoryError(t *testing.T) {
	cleanup := iamTenantUserTestInitLogger(t)
	defer cleanup()

	origFactory := iamClientFactory
	defer func() { iamClientFactory = origFactory }()

	iamClientFactory = func() (iam.Client, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "connection failed")
	}

	cmd := newIAMUserGetCommand()
	globalTenantID = "test-tenant"

	err := runIAMUserGetCommand(cmd, []string{"user_abc123"})
	assert.Error(t, err)
}

func TestIAMUserGetCommand_MissingTenant(t *testing.T) {
	cleanup := iamTenantUserTestInitLogger(t)
	defer cleanup()

	cmd := newIAMUserGetCommand()
	globalTenantID = ""

	err := runIAMUserGetCommand(cmd, []string{"user_abc123"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--tenant flag is required")
}

func TestIAMUserUpdateCommand_FactoryError(t *testing.T) {
	cleanup := iamTenantUserTestInitLogger(t)
	defer cleanup()

	origFactory := iamClientFactory
	defer func() { iamClientFactory = origFactory }()

	iamClientFactory = func() (iam.Client, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "connection failed")
	}

	cmd := newIAMUserUpdateCommand()
	cmd.Flags().Set("name", "New Name")
	globalTenantID = "test-tenant"

	err := runIAMUserUpdateCommand(cmd, []string{"user_abc123"})
	assert.Error(t, err)
}

func TestIAMUserUpdateCommand_MissingTenant(t *testing.T) {
	cleanup := iamTenantUserTestInitLogger(t)
	defer cleanup()

	cmd := newIAMUserUpdateCommand()
	cmd.Flags().Set("name", "New Name")
	globalTenantID = ""

	err := runIAMUserUpdateCommand(cmd, []string{"user_abc123"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--tenant flag is required")
}

func TestIAMUserUpdateCommand_NoFlagsProvided(t *testing.T) {
	cleanup := iamTenantUserTestInitLogger(t)
	defer cleanup()

	cmd := newIAMUserUpdateCommand()
	globalTenantID = "test-tenant"

	err := runIAMUserUpdateCommand(cmd, []string{"user_abc123"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "at least one of")
}

func TestIAMUserDeleteCommand_FactoryError(t *testing.T) {
	cleanup := iamTenantUserTestInitLogger(t)
	defer cleanup()

	origFactory := iamClientFactory
	defer func() { iamClientFactory = origFactory }()

	iamClientFactory = func() (iam.Client, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "connection failed")
	}

	cmd := newIAMUserDeleteCommand()
	globalTenantID = "test-tenant"
	iamForce = true

	err := runIAMUserDeleteCommand(cmd, []string{"user_abc123"})
	assert.Error(t, err)
}

func TestIAMUserDeleteCommand_MissingTenant(t *testing.T) {
	cleanup := iamTenantUserTestInitLogger(t)
	defer cleanup()

	cmd := newIAMUserDeleteCommand()
	globalTenantID = ""
	iamForce = true

	err := runIAMUserDeleteCommand(cmd, []string{"user_abc123"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--tenant flag is required")
}

func TestIAMUserListCommand_FactoryError(t *testing.T) {
	cleanup := iamTenantUserTestInitLogger(t)
	defer cleanup()

	origFactory := iamClientFactory
	defer func() { iamClientFactory = origFactory }()

	iamClientFactory = func() (iam.Client, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "connection failed")
	}

	cmd := newIAMUserListCommand()
	globalTenantID = "test-tenant"

	err := runIAMUserListCommand(cmd, []string{})
	assert.Error(t, err)
}

func TestIAMUserListCommand_MissingTenant(t *testing.T) {
	cleanup := iamTenantUserTestInitLogger(t)
	defer cleanup()

	cmd := newIAMUserListCommand()
	globalTenantID = ""

	err := runIAMUserListCommand(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--tenant flag is required")
}

// =============================================================================
// IAM User Role Command Tests - Factory Error Paths
// =============================================================================

func TestIAMUserRoleAssignCommand_FactoryError(t *testing.T) {
	cleanup := iamTenantUserTestInitLogger(t)
	defer cleanup()

	origFactory := iamClientFactory
	defer func() { iamClientFactory = origFactory }()

	iamClientFactory = func() (iam.Client, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "connection failed")
	}

	cmd := newIAMUserRoleAssignCommand()
	globalTenantID = "test-tenant"
	iamUserRole = "admin"
	iamRoleUserID = "user_abc123"

	err := runIAMUserRoleAssignCommand(cmd, []string{})
	assert.Error(t, err)
}

func TestIAMUserRoleAssignCommand_MissingTenant(t *testing.T) {
	cleanup := iamTenantUserTestInitLogger(t)
	defer cleanup()

	cmd := newIAMUserRoleAssignCommand()
	globalTenantID = ""
	iamUserRole = "admin"
	iamRoleUserID = "user_abc123"

	err := runIAMUserRoleAssignCommand(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--tenant flag is required")
}

func TestIAMUserRoleRevokeCommand_FactoryError(t *testing.T) {
	cleanup := iamTenantUserTestInitLogger(t)
	defer cleanup()

	origFactory := iamClientFactory
	defer func() { iamClientFactory = origFactory }()

	iamClientFactory = func() (iam.Client, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "connection failed")
	}

	cmd := newIAMUserRoleRevokeCommand()
	globalTenantID = "test-tenant"
	iamUserRole = "admin"
	iamRoleUserID = "user_abc123"

	err := runIAMUserRoleRevokeCommand(cmd, []string{})
	assert.Error(t, err)
}

func TestIAMUserRoleRevokeCommand_MissingTenant(t *testing.T) {
	cleanup := iamTenantUserTestInitLogger(t)
	defer cleanup()

	cmd := newIAMUserRoleRevokeCommand()
	globalTenantID = ""
	iamUserRole = "admin"
	iamRoleUserID = "user_abc123"

	err := runIAMUserRoleRevokeCommand(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--tenant flag is required")
}

// =============================================================================
// IAM Tenant Command Structure Tests
// =============================================================================

func TestNewIAMTenantCommand_FullStructure(t *testing.T) {
	cmd := newIAMTenantCommand()
	require.NotNil(t, cmd)
	assert.Equal(t, "tenant", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotEmpty(t, cmd.Long)

	// Verify subcommands exist
	expectedSubcmds := []string{"create", "get", "update", "delete", "list"}
	for _, expected := range expectedSubcmds {
		found := false
		for _, sub := range cmd.Commands() {
			if sub.Name() == expected {
				found = true
				break
			}
		}
		assert.True(t, found, "Expected subcommand %s not found", expected)
	}
}

func TestNewIAMTenantCreateCommand_Flags(t *testing.T) {
	cmd := newIAMTenantCreateCommand()
	require.NotNil(t, cmd)
	assert.Contains(t, cmd.Use, "create")

	// Check required flags exist
	assert.NotNil(t, cmd.Flags().Lookup("name"))
	assert.NotNil(t, cmd.Flags().Lookup("max-users"))
	assert.NotNil(t, cmd.Flags().Lookup("mfa-required"))
}

func TestNewIAMTenantUpdateCommand_Flags(t *testing.T) {
	cmd := newIAMTenantUpdateCommand()
	require.NotNil(t, cmd)
	assert.Contains(t, cmd.Use, "update")

	// Check flags exist
	assert.NotNil(t, cmd.Flags().Lookup("name"))
	assert.NotNil(t, cmd.Flags().Lookup("max-users"))
	assert.NotNil(t, cmd.Flags().Lookup("mfa-required"))
}

// =============================================================================
// IAM User Command Structure Tests
// =============================================================================

func TestNewIAMUserCommand_FullStructure(t *testing.T) {
	cmd := newIAMUserCommand()
	require.NotNil(t, cmd)
	assert.Equal(t, "user", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotEmpty(t, cmd.Long)

	// Verify subcommands exist
	expectedSubcmds := []string{"create", "get", "update", "delete", "list", "role"}
	for _, expected := range expectedSubcmds {
		found := false
		for _, sub := range cmd.Commands() {
			if sub.Name() == expected {
				found = true
				break
			}
		}
		assert.True(t, found, "Expected subcommand %s not found", expected)
	}
}

func TestNewIAMUserCreateCommand_Flags(t *testing.T) {
	cmd := newIAMUserCreateCommand()
	require.NotNil(t, cmd)
	assert.Contains(t, cmd.Use, "create")

	// Check flags exist. QuicBaaS is passwordless — no --password flag.
	assert.NotNil(t, cmd.Flags().Lookup("name"))
	assert.NotNil(t, cmd.Flags().Lookup("roles"))
	assert.NotNil(t, cmd.Flags().Lookup("role"))
	assert.NotNil(t, cmd.Flags().Lookup("groups"))
	assert.Nil(t, cmd.Flags().Lookup("password"), "--password flag must not exist; QuicBaaS uses invite-based FIDO2/PIV activation")
}

func TestNewIAMUserUpdateCommand_Flags(t *testing.T) {
	cmd := newIAMUserUpdateCommand()
	require.NotNil(t, cmd)
	assert.Contains(t, cmd.Use, "update")

	// Check flags exist
	assert.NotNil(t, cmd.Flags().Lookup("name"))
	assert.NotNil(t, cmd.Flags().Lookup("roles"))
}

func TestNewIAMUserListCommand_Flags(t *testing.T) {
	cmd := newIAMUserListCommand()
	require.NotNil(t, cmd)
	assert.Equal(t, "list", cmd.Use)

	// Check filter flags exist
	assert.NotNil(t, cmd.Flags().Lookup("role"))
	assert.NotNil(t, cmd.Flags().Lookup("group"))
}

func TestNewIAMUserRoleCommand_Structure(t *testing.T) {
	cmd := newIAMUserRoleCommand()
	require.NotNil(t, cmd)
	assert.Equal(t, "role", cmd.Use)
	assert.NotEmpty(t, cmd.Short)

	// Verify subcommands exist
	expectedSubcmds := []string{"assign", "revoke"}
	for _, expected := range expectedSubcmds {
		found := false
		for _, sub := range cmd.Commands() {
			if sub.Name() == expected {
				found = true
				break
			}
		}
		assert.True(t, found, "Expected subcommand %s not found", expected)
	}
}

func TestNewIAMUserRoleAssignCommand_Flags(t *testing.T) {
	cmd := newIAMUserRoleAssignCommand()
	require.NotNil(t, cmd)
	assert.Equal(t, "assign", cmd.Use)

	// Check required flags exist
	assert.NotNil(t, cmd.Flags().Lookup("role"))
	assert.NotNil(t, cmd.Flags().Lookup("user"))
}

func TestNewIAMUserRoleRevokeCommand_Flags(t *testing.T) {
	cmd := newIAMUserRoleRevokeCommand()
	require.NotNil(t, cmd)
	assert.Equal(t, "revoke", cmd.Use)

	// Check required flags exist
	assert.NotNil(t, cmd.Flags().Lookup("role"))
	assert.NotNil(t, cmd.Flags().Lookup("user"))
}
