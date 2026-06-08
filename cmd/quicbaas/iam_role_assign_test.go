// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
)

// withRoleAssignState saves and restores the role assign/revoke globals.
func withRoleAssignState(t *testing.T) {
	t.Helper()
	cleanup := testInitLogger(t)
	origUser := iamRoleUserID
	origRole := iamRoleID
	origTenant := globalTenantID
	origForce := iamForce
	origFactory := iamClientFactory
	t.Cleanup(func() {
		iamRoleUserID = origUser
		iamRoleID = origRole
		globalTenantID = origTenant
		iamForce = origForce
		iamClientFactory = origFactory
		cleanup()
	})
	iamRoleUserID = ""
	iamRoleID = ""
	iamForce = true
}

func TestRunIAMRoleAssign_MissingUser(t *testing.T) {
	withRoleAssignState(t)
	cmd := newIAMRoleAssignCommand()
	globalTenantID = "acme"

	err := runIAMRoleAssignCommand(cmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "user ID is required")
}

func TestRunIAMRoleAssign_MissingRole(t *testing.T) {
	withRoleAssignState(t)
	cmd := newIAMRoleAssignCommand()
	globalTenantID = "acme"
	iamRoleUserID = "user_1"

	err := runIAMRoleAssignCommand(cmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "role ID is required")
}

func TestRunIAMRoleAssign_MissingTenant(t *testing.T) {
	withRoleAssignState(t)
	cmd := newIAMRoleAssignCommand()
	globalTenantID = ""

	err := runIAMRoleAssignCommand(cmd, []string{"user_1", "admin"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--tenant")
}

func TestRunIAMRoleAssign_SDKError(t *testing.T) {
	withRoleAssignState(t)
	mock := newTestMockIAMClient()
	mock.userClient.AssignRoleFunc = func(_ context.Context, _, _, _ string) error {
		return errors.New(errors.ErrInternal, "iam", "assign failed")
	}
	iamClientFactory = func() (iam.Client, error) { return mock, nil }

	cmd := newIAMRoleAssignCommand()
	globalTenantID = "acme"

	require.Error(t, runIAMRoleAssignCommand(cmd, []string{"user_1", "admin"}))
}

func TestRunIAMRoleAssign_FactoryError(t *testing.T) {
	withRoleAssignState(t)
	iamClientFactory = func() (iam.Client, error) {
		return nil, errors.New(errors.ErrInternal, "iam", "factory boom")
	}

	cmd := newIAMRoleAssignCommand()
	globalTenantID = "acme"

	require.Error(t, runIAMRoleAssignCommand(cmd, []string{"user_1", "admin"}))
}

func TestRunIAMRoleRevoke_MissingUser(t *testing.T) {
	withRoleAssignState(t)
	cmd := newIAMRoleRevokeCommand()
	globalTenantID = "acme"

	err := runIAMRoleRevokeCommand(cmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "user ID is required")
}

func TestRunIAMRoleRevoke_SDKError(t *testing.T) {
	withRoleAssignState(t)
	mock := newTestMockIAMClient()
	mock.userClient.RevokeRoleFunc = func(_ context.Context, _, _, _ string) error {
		return errors.New(errors.ErrInternal, "iam", "revoke failed")
	}
	iamClientFactory = func() (iam.Client, error) { return mock, nil }

	cmd := newIAMRoleRevokeCommand()
	globalTenantID = "acme"

	require.Error(t, runIAMRoleRevokeCommand(cmd, []string{"user_1", "admin"}))
}
