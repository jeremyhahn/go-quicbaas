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

package service

import (
	"context"
	stderrors "errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// newRoleStoreTestService wires a real MemoryStorage-backed IdentityStore onto
// a quicBaaS so the rbacService role-store verbs exercise the qrdb-backed path
// end to end. It returns the public service plus the tenant-scoped context.
func newRoleStoreTestService(t *testing.T, tenantID string) (QuicBaaS, context.Context) {
	t.Helper()

	storage := iam.NewMemoryStorage()
	is := iam.NewIdentityStore(iam.IdentityStoreOptions{
		UserStorage:  storage,
		GroupStorage: storage,
		RoleStorage:  storage,
	})

	return newRoleStoreTestServiceWithStore(t, tenantID, is)
}

// newRoleStoreTestServiceWithStore wires the provided IdentityStore onto a
// quicBaaS and returns a tenant-scoped context.
func newRoleStoreTestServiceWithStore(t *testing.T, tenantID string, is *iam.IdentityStore) (QuicBaaS, context.Context) {
	t.Helper()

	svc := createTestService(t)
	db := svc.(*quicBaaS)
	db.iamProvider = &mockIAMServiceProvider{identityStore: is}

	ctx := iam.ContextWithTenant(context.Background(), tenantID)
	return svc, ctx
}

// TestRoleStore_CreateListDelete_RoundTrip drives create -> list -> delete
// against the IdentityStore and asserts permissions survive as colon strings
// with no auth.ResourceKeys defaulting.
func TestRoleStore_CreateListDelete_RoundTrip(t *testing.T) {
	svc, ctx := newRoleStoreTestService(t, "tenant-roles")

	perms := []string{"keys:read", "secrets:write", "data:*"}
	createResp, err := svc.CreateAuthRole(ctx, &AuthCreateRoleRequest{
		RoleName:    "editor",
		Permissions: perms,
	})
	require.NoError(t, err)
	require.NotNil(t, createResp)
	assert.NotEmpty(t, createResp.RoleID)
	assert.NotZero(t, createResp.Timestamp)

	listResp, err := svc.ListAuthRoles(ctx, &AuthListRolesRequest{})
	require.NoError(t, err)
	require.NotNil(t, listResp)
	require.Len(t, listResp.Roles, 1)

	role := listResp.Roles[0]
	assert.Equal(t, createResp.RoleID, role.RoleID)
	assert.Equal(t, "editor", role.RoleName)
	assert.NotZero(t, role.CreatedAt)
	// Permissions pass through verbatim — no resource defaulting.
	assert.Equal(t, perms, role.Permissions)

	delResp, err := svc.DeleteAuthRole(ctx, &AuthDeleteRoleRequest{RoleName: "editor"})
	require.NoError(t, err)
	require.NotNil(t, delResp)
	assert.True(t, delResp.Success)

	listResp, err = svc.ListAuthRoles(ctx, &AuthListRolesRequest{})
	require.NoError(t, err)
	assert.Empty(t, listResp.Roles)
}

// TestRoleStore_ListRoles_Pagination confirms the requested Offset/Limit window
// is passed through to the store.
func TestRoleStore_ListRoles_Pagination(t *testing.T) {
	svc, ctx := newRoleStoreTestService(t, "tenant-page")

	for _, name := range []string{"r1", "r2", "r3"} {
		_, err := svc.CreateAuthRole(ctx, &AuthCreateRoleRequest{RoleName: name})
		require.NoError(t, err)
	}

	resp, err := svc.ListAuthRoles(ctx, &AuthListRolesRequest{Offset: 1, Limit: 1})
	require.NoError(t, err)
	require.Len(t, resp.Roles, 1)
	assert.Equal(t, uint64(1), resp.TotalCount)
}

// TestRoleStore_CreateRole_DuplicateName rejects a second role with the same
// name in the tenant.
func TestRoleStore_CreateRole_DuplicateName(t *testing.T) {
	svc, ctx := newRoleStoreTestService(t, "tenant-dup")

	_, err := svc.CreateAuthRole(ctx, &AuthCreateRoleRequest{RoleName: "viewer"})
	require.NoError(t, err)

	resp, err := svc.CreateAuthRole(ctx, &AuthCreateRoleRequest{RoleName: "viewer"})
	assert.Nil(t, resp)
	require.Error(t, err)

	var qErr *dberrors.QBError
	require.True(t, dberrors.As(err, &qErr))
	assert.Equal(t, dberrors.ErrInvalidInput, qErr.Code)
}

// TestRoleStore_CreateRole_EmptyName rejects an empty role name before
// touching the store.
func TestRoleStore_CreateRole_EmptyName(t *testing.T) {
	svc, ctx := newRoleStoreTestService(t, "tenant-empty")

	resp, err := svc.CreateAuthRole(ctx, &AuthCreateRoleRequest{RoleName: ""})
	assert.Nil(t, resp)
	require.Error(t, err)

	var qErr *dberrors.QBError
	require.True(t, dberrors.As(err, &qErr))
	assert.Equal(t, dberrors.ErrInvalidInput, qErr.Code)
}

// TestRoleStore_TenantAbsent_FailsClosed asserts every verb fails closed with a
// missing-tenant error when no tenant is present in the context.
func TestRoleStore_TenantAbsent_FailsClosed(t *testing.T) {
	svc, _ := newRoleStoreTestService(t, "ignored")
	ctx := context.Background() // no tenant

	t.Run("CreateRole", func(t *testing.T) {
		resp, err := svc.CreateAuthRole(ctx, &AuthCreateRoleRequest{RoleName: "x"})
		assert.Nil(t, resp)
		assertMissingTenant(t, err)
	})

	t.Run("ListRoles", func(t *testing.T) {
		resp, err := svc.ListAuthRoles(ctx, &AuthListRolesRequest{})
		assert.Nil(t, resp)
		assertMissingTenant(t, err)
	})

	t.Run("DeleteRole", func(t *testing.T) {
		resp, err := svc.DeleteAuthRole(ctx, &AuthDeleteRoleRequest{RoleName: "x"})
		assert.Nil(t, resp)
		assertMissingTenant(t, err)
	})
}

// TestRoleStore_IdentityStoreUnconfigured fails closed when the provider yields
// a nil identity store.
func TestRoleStore_IdentityStoreUnconfigured(t *testing.T) {
	svc := createTestService(t)
	db := svc.(*quicBaaS)
	db.iamProvider = &mockIAMServiceProvider{identityStore: nil}

	ctx := iam.ContextWithTenant(context.Background(), "tenant-x")

	resp, err := svc.ListAuthRoles(ctx, &AuthListRolesRequest{})
	assert.Nil(t, resp)
	require.Error(t, err)

	var qErr *dberrors.QBError
	require.True(t, dberrors.As(err, &qErr))
	assert.Equal(t, dberrors.ErrConfiguration, qErr.Code)

	var notConfigured *api.ServiceNotConfiguredError
	require.True(t, stderrors.As(err, &notConfigured))
	assert.Equal(t, "identity store", notConfigured.Service)
}

// TestRoleStore_IdentityStoreError surfaces a provider error as a configuration
// QBError.
func TestRoleStore_IdentityStoreError(t *testing.T) {
	svc := createTestService(t)
	db := svc.(*quicBaaS)
	db.iamProvider = &mockIAMServiceProvider{identityStoreErr: stderrors.New("provider down")}

	ctx := iam.ContextWithTenant(context.Background(), "tenant-x")

	resp, err := svc.ListAuthRoles(ctx, &AuthListRolesRequest{})
	assert.Nil(t, resp)
	require.Error(t, err)

	var qErr *dberrors.QBError
	require.True(t, dberrors.As(err, &qErr))
	assert.Equal(t, dberrors.ErrConfiguration, qErr.Code)
}

// TestRoleStore_DeleteRole_NameNotFound returns a typed NotFoundError when no
// role matches the requested name.
func TestRoleStore_DeleteRole_NameNotFound(t *testing.T) {
	svc, ctx := newRoleStoreTestService(t, "tenant-nf")

	resp, err := svc.DeleteAuthRole(ctx, &AuthDeleteRoleRequest{RoleName: "ghost"})
	assert.Nil(t, resp)
	require.Error(t, err)

	var qErr *dberrors.QBError
	require.True(t, dberrors.As(err, &qErr))
	assert.Equal(t, dberrors.ErrNotFound, qErr.Code)

	var nf *api.NotFoundError
	require.True(t, stderrors.As(err, &nf))
	assert.Equal(t, "role", nf.Resource)
	assert.Equal(t, "ghost", nf.ID)
}

// TestRoleStore_DeleteRole_EmptyName rejects an empty name before lookup.
func TestRoleStore_DeleteRole_EmptyName(t *testing.T) {
	svc, ctx := newRoleStoreTestService(t, "tenant-de")

	resp, err := svc.DeleteAuthRole(ctx, &AuthDeleteRoleRequest{RoleName: ""})
	assert.Nil(t, resp)
	require.Error(t, err)

	var qErr *dberrors.QBError
	require.True(t, dberrors.As(err, &qErr))
	assert.Equal(t, dberrors.ErrInvalidInput, qErr.Code)
}

// TestRoleStore_BackendErrors exercises the store-error wrap branches of every
// verb via a RoleStorage that fails on list/create/delete.
func TestRoleStore_BackendErrors(t *testing.T) {
	t.Run("ListRoles", func(t *testing.T) {
		is := iam.NewIdentityStore(iam.IdentityStoreOptions{
			RoleStorage: &failingRoleStorage{failList: true},
		})
		svc, ctx := newRoleStoreTestServiceWithStore(t, "t-fail", is)

		resp, err := svc.ListAuthRoles(ctx, &AuthListRolesRequest{})
		assert.Nil(t, resp)
		assertInternalError(t, err)
	})

	t.Run("CreateRole", func(t *testing.T) {
		is := iam.NewIdentityStore(iam.IdentityStoreOptions{
			RoleStorage: &failingRoleStorage{failCreate: true},
		})
		svc, ctx := newRoleStoreTestServiceWithStore(t, "t-fail", is)

		resp, err := svc.CreateAuthRole(ctx, &AuthCreateRoleRequest{RoleName: "x"})
		assert.Nil(t, resp)
		assertInternalError(t, err)
	})

	t.Run("DeleteRole", func(t *testing.T) {
		is := iam.NewIdentityStore(iam.IdentityStoreOptions{
			RoleStorage: &failingRoleStorage{
				listResult: []*iam.Role{{ID: "r-1", Name: "x"}},
				failDelete: true,
			},
			UserStorage: iam.NewMemoryStorage(),
		})
		svc, ctx := newRoleStoreTestServiceWithStore(t, "t-fail", is)

		resp, err := svc.DeleteAuthRole(ctx, &AuthDeleteRoleRequest{RoleName: "x"})
		assert.Nil(t, resp)
		assertInternalError(t, err)
	})
}

// TestRoleStore_DeleteRole_ListError surfaces a list failure during the
// name->ID resolution as an internal error.
func TestRoleStore_DeleteRole_ListError(t *testing.T) {
	is := iam.NewIdentityStore(iam.IdentityStoreOptions{
		RoleStorage: &failingRoleStorage{failList: true},
		UserStorage: iam.NewMemoryStorage(),
	})
	svc, ctx := newRoleStoreTestServiceWithStore(t, "t-fail", is)

	resp, err := svc.DeleteAuthRole(ctx, &AuthDeleteRoleRequest{RoleName: "x"})
	assert.Nil(t, resp)
	assertInternalError(t, err)
}

// TestRoleStore_AssignRevokeRole_RoundTrip binds a role to a user by username
// through the qrdb-backed IdentityStore, then unbinds it — exercising the C1-E
// AssignRole/RevokeRole redirect end to end.
func TestRoleStore_AssignRevokeRole_RoundTrip(t *testing.T) {
	storage := iam.NewMemoryStorage()
	is := iam.NewIdentityStore(iam.IdentityStoreOptions{
		UserStorage:  storage,
		GroupStorage: storage,
		RoleStorage:  storage,
	})
	svc, ctx := newRoleStoreTestServiceWithStore(t, "tenant-bind", is)

	// Seed a user (Name is the login username) and a role.
	user := &iam.User{ID: "u-1", TenantID: "tenant-bind", Name: "alice", Email: "alice@bind.test"}
	require.NoError(t, storage.CreateUser(ctx, user))
	role := &iam.Role{ID: "r-1", TenantID: "tenant-bind", Name: "editor"}
	require.NoError(t, storage.CreateRole(ctx, role))

	assignResp, err := svc.AssignRole(ctx, &AuthAssignRoleRequest{Username: "alice", RoleName: "editor"})
	require.NoError(t, err)
	require.NotNil(t, assignResp)
	assert.True(t, assignResp.Success)

	got, err := storage.GetUser(ctx, "tenant-bind", "u-1")
	require.NoError(t, err)
	assert.Contains(t, got.Roles, "r-1")

	revokeResp, err := svc.RevokeRole(ctx, &AuthRevokeRoleRequest{Username: "alice", RoleName: "editor"})
	require.NoError(t, err)
	require.NotNil(t, revokeResp)
	assert.True(t, revokeResp.Success)

	got, err = storage.GetUser(ctx, "tenant-bind", "u-1")
	require.NoError(t, err)
	assert.NotContains(t, got.Roles, "r-1")
}

// TestRoleStore_AssignRole_UnknownUser fails closed with a typed NotFoundError
// when the username does not resolve.
func TestRoleStore_AssignRole_UnknownUser(t *testing.T) {
	storage := iam.NewMemoryStorage()
	is := iam.NewIdentityStore(iam.IdentityStoreOptions{
		UserStorage:  storage,
		GroupStorage: storage,
		RoleStorage:  storage,
	})
	svc, ctx := newRoleStoreTestServiceWithStore(t, "tenant-bind", is)
	require.NoError(t, storage.CreateRole(ctx, &iam.Role{ID: "r-1", TenantID: "tenant-bind", Name: "editor"}))

	resp, err := svc.AssignRole(ctx, &AuthAssignRoleRequest{Username: "ghost", RoleName: "editor"})
	assert.Nil(t, resp)
	require.Error(t, err)

	var nf *api.NotFoundError
	require.True(t, stderrors.As(err, &nf))
	assert.Equal(t, "user", nf.Resource)
}

// TestRoleStore_AssignRole_UnknownRole fails closed when the role name does not
// resolve.
func TestRoleStore_AssignRole_UnknownRole(t *testing.T) {
	storage := iam.NewMemoryStorage()
	is := iam.NewIdentityStore(iam.IdentityStoreOptions{
		UserStorage:  storage,
		GroupStorage: storage,
		RoleStorage:  storage,
	})
	svc, ctx := newRoleStoreTestServiceWithStore(t, "tenant-bind", is)
	require.NoError(t, storage.CreateUser(ctx, &iam.User{ID: "u-1", TenantID: "tenant-bind", Name: "alice", Email: "alice@bind.test"}))

	resp, err := svc.AssignRole(ctx, &AuthAssignRoleRequest{Username: "alice", RoleName: "ghost"})
	assert.Nil(t, resp)
	require.Error(t, err)

	var nf *api.NotFoundError
	require.True(t, stderrors.As(err, &nf))
	assert.Equal(t, "role", nf.Resource)
}

// TestRoleStore_AssignRevokeRole_InvalidInput rejects an empty username or role
// name before touching the store.
func TestRoleStore_AssignRevokeRole_InvalidInput(t *testing.T) {
	svc, ctx := newRoleStoreTestService(t, "tenant-bind")

	t.Run("AssignEmptyUsername", func(t *testing.T) {
		resp, err := svc.AssignRole(ctx, &AuthAssignRoleRequest{Username: "", RoleName: "editor"})
		assert.Nil(t, resp)
		require.Error(t, err)
		var qErr *dberrors.QBError
		require.True(t, dberrors.As(err, &qErr))
		assert.Equal(t, dberrors.ErrInvalidInput, qErr.Code)
	})

	t.Run("RevokeEmptyRoleName", func(t *testing.T) {
		resp, err := svc.RevokeRole(ctx, &AuthRevokeRoleRequest{Username: "alice", RoleName: ""})
		assert.Nil(t, resp)
		require.Error(t, err)
		var qErr *dberrors.QBError
		require.True(t, dberrors.As(err, &qErr))
		assert.Equal(t, dberrors.ErrInvalidInput, qErr.Code)
	})
}

// TestRoleStore_AssignRole_MissingTenantContext fails closed when no tenant
// is present in the context, verifying the op name appears in the error.
func TestRoleStore_AssignRole_MissingTenantContext(t *testing.T) {
	svc, _ := newRoleStoreTestService(t, "tenant-ctx")
	ctx := context.Background() // no tenant

	resp, err := svc.AssignRole(ctx, &AuthAssignRoleRequest{Username: "alice", RoleName: "editor"})
	assert.Nil(t, resp)
	require.Error(t, err)

	assert.Contains(t, err.Error(), "missing_tenant_context")
	assert.Contains(t, err.Error(), "service.AssignRole")
}

// TestRoleStore_RevokeRole_MissingTenantContext fails closed when no tenant
// is present in the context, verifying the op name appears in the error.
func TestRoleStore_RevokeRole_MissingTenantContext(t *testing.T) {
	svc, _ := newRoleStoreTestService(t, "tenant-ctx")
	ctx := context.Background() // no tenant

	resp, err := svc.RevokeRole(ctx, &AuthRevokeRoleRequest{Username: "alice", RoleName: "editor"})
	assert.Nil(t, resp)
	require.Error(t, err)

	assert.Contains(t, err.Error(), "missing_tenant_context")
	assert.Contains(t, err.Error(), "service.RevokeRole")
}

// TestRoleStore_AssignRole_UserNotFound fails closed with a typed NotFoundError
// when the requested username does not exist in the tenant.
func TestRoleStore_AssignRole_UserNotFound(t *testing.T) {
	storage := iam.NewMemoryStorage()
	is := iam.NewIdentityStore(iam.IdentityStoreOptions{
		UserStorage:  storage,
		GroupStorage: storage,
		RoleStorage:  storage,
	})
	svc, ctx := newRoleStoreTestServiceWithStore(t, "tenant-nf-user", is)
	require.NoError(t, storage.CreateRole(ctx, &iam.Role{ID: "r-1", TenantID: "tenant-nf-user", Name: "editor"}))

	resp, err := svc.AssignRole(ctx, &AuthAssignRoleRequest{Username: "ghost", RoleName: "editor"})
	assert.Nil(t, resp)
	require.Error(t, err)

	var nf *api.NotFoundError
	require.True(t, stderrors.As(err, &nf))
	assert.Equal(t, "user", nf.Resource)
}

// TestRoleStore_AssignRole_RoleNotFound fails closed with a typed NotFoundError
// when the requested role name does not exist in the tenant.
func TestRoleStore_AssignRole_RoleNotFound(t *testing.T) {
	storage := iam.NewMemoryStorage()
	is := iam.NewIdentityStore(iam.IdentityStoreOptions{
		UserStorage:  storage,
		GroupStorage: storage,
		RoleStorage:  storage,
	})
	svc, ctx := newRoleStoreTestServiceWithStore(t, "tenant-nf-role", is)
	require.NoError(t, storage.CreateUser(ctx, &iam.User{ID: "u-1", TenantID: "tenant-nf-role", Name: "alice", Email: "alice@nf.test"}))

	resp, err := svc.AssignRole(ctx, &AuthAssignRoleRequest{Username: "alice", RoleName: "ghost"})
	assert.Nil(t, resp)
	require.Error(t, err)

	var nf *api.NotFoundError
	require.True(t, stderrors.As(err, &nf))
	assert.Equal(t, "role", nf.Resource)
}

// failingRoleStorage is a RoleStorage whose list/create/delete operations can
// be configured to fail, driving the error-wrap branches of the role store.
type failingRoleStorage struct {
	failList   bool
	failCreate bool
	failDelete bool
	listResult []*iam.Role
}

func (f *failingRoleStorage) CreateRole(_ context.Context, _ *iam.Role) error {
	if f.failCreate {
		return stderrors.New("create failed")
	}
	return nil
}

func (f *failingRoleStorage) GetRole(_ context.Context, _, _ string) (*iam.Role, error) {
	return nil, stderrors.New("not found")
}

func (f *failingRoleStorage) UpdateRole(_ context.Context, _ *iam.Role) error {
	return nil
}

func (f *failingRoleStorage) DeleteRole(_ context.Context, _, _ string) error {
	if f.failDelete {
		return stderrors.New("delete failed")
	}
	return nil
}

func (f *failingRoleStorage) ListRoles(_ context.Context, _ string, _ iam.ListOptions) ([]*iam.Role, error) {
	if f.failList {
		return nil, stderrors.New("list failed")
	}
	return f.listResult, nil
}

// assertInternalError asserts the error is an internal QBError.
func assertInternalError(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	var qErr *dberrors.QBError
	require.True(t, dberrors.As(err, &qErr))
	assert.Equal(t, dberrors.ErrInternal, qErr.Code)
}

// assertMissingTenant asserts the error is the qrdb missing-tenant-context
// error.
func assertMissingTenant(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	expected := dberrors.NewMissingTenantContextError("probe")
	assert.Equal(t, expected.Code, dberrors.Code(err))
}
