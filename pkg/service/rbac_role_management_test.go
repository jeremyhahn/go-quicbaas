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

//go:build !iam

package service

import (
	"context"
	stderrors "errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// =============================================================================
// RBAC role STORE — non-IAM build assertions.
//
// In non-IAM builds the qrdb-backed IdentityStore is not compiled in, so the
// role-store verbs (CreateRole / DeleteRole / ListRoles, exposed publicly as
// CreateAuthRole / DeleteAuthRole / ListAuthRoles) fail closed with a typed
// not-configured error. There is no fall-back to the legacy in-memory
// the deleted fork role model. The IdentityStore round-trip is covered by the //go:build
// iam test in rbac_role_store_iam_test.go.
// =============================================================================

// assertRoleStoreNotConfigured asserts the error is the typed
// ServiceNotConfiguredError{"identity store"} wrapped in a configuration
// QBError.
func assertRoleStoreNotConfigured(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)

	var qErr *dberrors.QBError
	require.True(t, dberrors.As(err, &qErr), "expected *errors.QBError, got %T", err)
	assert.Equal(t, dberrors.ErrConfiguration, qErr.Code)

	var notConfigured *api.ServiceNotConfiguredError
	require.True(t, stderrors.As(err, &notConfigured), "expected *api.ServiceNotConfiguredError, got %T", qErr.Err)
	assert.Equal(t, "identity store", notConfigured.Service)
}

func TestRBACService_CreateAuthRole_NotConfigured(t *testing.T) {
	svc := createTestService(t)

	resp, err := svc.CreateAuthRole(context.Background(), &AuthCreateRoleRequest{
		RoleName:    "custom-role",
		Permissions: []string{"keys:read", "keys:write"},
	})

	assert.Nil(t, resp)
	assertRoleStoreNotConfigured(t, err)
}

func TestRBACService_DeleteAuthRole_NotConfigured(t *testing.T) {
	svc := createTestService(t)

	resp, err := svc.DeleteAuthRole(context.Background(), &AuthDeleteRoleRequest{
		RoleName: "custom-role",
	})

	assert.Nil(t, resp)
	assertRoleStoreNotConfigured(t, err)
}

func TestRBACService_ListAuthRoles_NotConfigured(t *testing.T) {
	svc := createTestService(t)

	resp, err := svc.ListAuthRoles(context.Background(), &AuthListRolesRequest{})

	assert.Nil(t, resp)
	assertRoleStoreNotConfigured(t, err)
}
