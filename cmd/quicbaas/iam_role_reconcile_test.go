// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
)

// withRoleReconcileState saves and restores the globals the role reconcile and
// role get commands read.
func withRoleReconcileState(t *testing.T) {
	t.Helper()
	cleanup := testInitLogger(t)
	origTenant := globalTenantID
	origFormat := iamOutputFormat
	origFactory := iamClientFactory
	t.Cleanup(func() {
		globalTenantID = origTenant
		iamOutputFormat = origFormat
		iamClientFactory = origFactory
		cleanup()
	})
	iamOutputFormat = "table"
}

// captureRoleStdout runs fn with os.Stdout redirected and returns what it wrote.
func captureRoleStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	runErr := fn()

	require.NoError(t, w.Close())
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, copyErr := io.Copy(&buf, r)
	require.NoError(t, copyErr)
	return buf.String(), runErr
}

func TestRunIAMRoleReconcile_TableOutput(t *testing.T) {
	withRoleReconcileState(t)
	mock, cleanup := setupIAMMock(t)
	defer cleanup()
	mock.roleClient.ReconcileFunc = func(_ context.Context, tenantID string) (*iam.ReconcileRolesResponse, error) {
		return &iam.ReconcileRolesResponse{
			TenantID: tenantID,
			Roles: []iam.RoleReconcileResult{
				{RoleID: "role_001", Name: "admin", Added: []string{"kv:write"}, Removed: []string{"kv:put"}},
				{RoleID: "role_002", Name: "operator", Created: true, Added: []string{"kv:read"}},
			},
			Changed: true,
		}, nil
	}

	cmd := newIAMRoleReconcileCommand()
	globalTenantID = "acme"

	output, err := captureRoleStdout(t, func() error { return runIAMRoleReconcileCommand(cmd, nil) })
	require.NoError(t, err)
	assert.Contains(t, output, "acme")
	assert.Contains(t, output, "admin")
	assert.Contains(t, output, "+[kv:write]")
	assert.Contains(t, output, "-[kv:put]")
	assert.Contains(t, output, "operator")
	assert.Contains(t, output, "(created)")
	assert.Contains(t, output, "Total: 2 roles changed")
}

func TestRunIAMRoleReconcile_NoChanges(t *testing.T) {
	withRoleReconcileState(t)
	mock, cleanup := setupIAMMock(t)
	defer cleanup()
	mock.roleClient.ReconcileFunc = func(_ context.Context, tenantID string) (*iam.ReconcileRolesResponse, error) {
		return &iam.ReconcileRolesResponse{TenantID: tenantID, Changed: false}, nil
	}

	cmd := newIAMRoleReconcileCommand()
	globalTenantID = "acme"

	output, err := captureRoleStdout(t, func() error { return runIAMRoleReconcileCommand(cmd, nil) })
	require.NoError(t, err)
	assert.Contains(t, output, "no changes; roles already match this build")
}

func TestRunIAMRoleReconcile_JSONOutput(t *testing.T) {
	withRoleReconcileState(t)
	mock, cleanup := setupIAMMock(t)
	defer cleanup()
	mock.roleClient.ReconcileFunc = func(_ context.Context, tenantID string) (*iam.ReconcileRolesResponse, error) {
		return &iam.ReconcileRolesResponse{
			TenantID: tenantID,
			Roles:    []iam.RoleReconcileResult{{RoleID: "role_001", Name: "admin", Added: []string{"kv:write"}}},
			Changed:  true,
		}, nil
	}

	cmd := newIAMRoleReconcileCommand()
	globalTenantID = "acme"
	iamOutputFormat = "json"

	output, err := captureRoleStdout(t, func() error { return runIAMRoleReconcileCommand(cmd, nil) })
	require.NoError(t, err)
	assert.Contains(t, output, `"tenant_id": "acme"`)
	assert.Contains(t, output, `"role_id": "role_001"`)
	assert.Contains(t, output, `"changed": true`)
}

func TestRunIAMRoleReconcile_MissingTenant(t *testing.T) {
	withRoleReconcileState(t)
	cmd := newIAMRoleReconcileCommand()
	globalTenantID = ""

	err := runIAMRoleReconcileCommand(cmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--tenant")
}

func TestRunIAMRoleReconcile_SDKError(t *testing.T) {
	withRoleReconcileState(t)
	mock, cleanup := setupIAMMock(t)
	defer cleanup()
	mock.roleClient.ReconcileFunc = func(_ context.Context, _ string) (*iam.ReconcileRolesResponse, error) {
		return nil, errors.New(errors.ErrForbidden, "iam", "reconcile denied")
	}

	cmd := newIAMRoleReconcileCommand()
	globalTenantID = "acme"

	require.Error(t, runIAMRoleReconcileCommand(cmd, nil))
}

func TestRunIAMRoleReconcile_FactoryError(t *testing.T) {
	withRoleReconcileState(t)
	iamClientFactory = func() (iam.Client, error) {
		return nil, errors.New(errors.ErrInternal, "iam", "factory boom")
	}

	cmd := newIAMRoleReconcileCommand()
	globalTenantID = "acme"

	require.Error(t, runIAMRoleReconcileCommand(cmd, nil))
}

func TestRunIAMRoleReconcile_InvalidOutputFormat(t *testing.T) {
	withRoleReconcileState(t)
	_, cleanup := setupIAMMock(t)
	defer cleanup()

	cmd := newIAMRoleReconcileCommand()
	globalTenantID = "acme"
	iamOutputFormat = "not-a-format"

	require.Error(t, runIAMRoleReconcileCommand(cmd, nil))
}

// The reconcile verb exists because a role can drift from the build. Role get
// must name that drift so an operator knows reconcile is needed.
func TestRunIAMRoleGet_ShowsRetiredPermissions(t *testing.T) {
	withRoleReconcileState(t)
	mock, cleanup := setupIAMMock(t)
	defer cleanup()
	mock.roleClient.GetFunc = func(_ context.Context, tenantID, roleID string) (*iam.Role, error) {
		return &iam.Role{
			ID:                 roleID,
			Name:               roleID,
			DisplayName:        "Admin",
			TenantID:           tenantID,
			Permissions:        []string{"kv:write", "kv:put"},
			RetiredPermissions: []string{"kv:put"},
		}, nil
	}

	cmd := newIAMRoleGetCommand()
	globalTenantID = "acme"

	output, err := captureRoleStdout(t, func() error { return runIAMRoleGetCommand(cmd, []string{"admin"}) })
	require.NoError(t, err)
	assert.Contains(t, output, "Retired Permissions: kv:put")
}

func TestRunIAMRoleGet_OmitsRetiredPermissionsWhenClean(t *testing.T) {
	withRoleReconcileState(t)
	mock, cleanup := setupIAMMock(t)
	defer cleanup()
	mock.roleClient.GetFunc = func(_ context.Context, tenantID, roleID string) (*iam.Role, error) {
		return &iam.Role{
			ID:          roleID,
			Name:        roleID,
			DisplayName: "Admin",
			TenantID:    tenantID,
			Permissions: []string{"kv:write"},
		}, nil
	}

	cmd := newIAMRoleGetCommand()
	globalTenantID = "acme"

	output, err := captureRoleStdout(t, func() error { return runIAMRoleGetCommand(cmd, []string{"admin"}) })
	require.NoError(t, err)
	assert.NotContains(t, output, "Retired Permissions")
}
