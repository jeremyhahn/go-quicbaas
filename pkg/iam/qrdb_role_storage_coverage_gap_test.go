// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

package iam

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbrole "github.com/jeremyhahn/go-qrdb/pkg/role"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// =============================================================================
// QRDBRoleStorage coverage: this RoleStorage decorator delegates every role
// operation to qrdb's canonical RoleServicer over the SDK RoleClient. The whole
// file was uncovered. These tests exercise the resolver-not-configured fail-
// closed path, the qrdb error -> IAM-sentinel translation (RoleNotFound /
// RoleNameExists / generic wrap), the happy-path delegation, and the
// offset/limit -> page pagination math.
// =============================================================================

// fakeRoleClient is a controllable qrdbtransport.RoleClient double. Only the
// role CRUD/list methods QRDBRoleStorage uses carry behaviour; the remaining
// interface methods are inert stubs so the type satisfies the interface.
type fakeRoleClient struct {
	createResp *qrdbtransport.Role
	createErr  error
	getResp    *qrdbtransport.Role
	getErr     error
	updateErr  error
	deleteErr  error
	listResp   *qrdbtransport.ListRolesResponse
	listErr    error
	byNameResp *qrdbtransport.Role
	byNameErr  error
	recResp    *qrdbtransport.ReconcileRolesResponse
	recErr     error

	gotCreate *qrdbtransport.CreateRoleRequest
	gotUpdate *qrdbtransport.UpdateRoleRequest
	gotDelete string
	gotGet    string
	gotList   *qrdbtransport.ListRolesRequest
	gotRec    *qrdbtransport.ReconcileRolesRequest
	gotByName *qrdbtransport.GetRoleByNameRequest
}

func (f *fakeRoleClient) CreateRole(_ context.Context, req *qrdbtransport.CreateRoleRequest) (*qrdbtransport.Role, error) {
	f.gotCreate = req
	return f.createResp, f.createErr
}

func (f *fakeRoleClient) GetRole(_ context.Context, id string) (*qrdbtransport.Role, error) {
	f.gotGet = id
	return f.getResp, f.getErr
}

func (f *fakeRoleClient) UpdateRole(_ context.Context, req *qrdbtransport.UpdateRoleRequest) (*qrdbtransport.Role, error) {
	f.gotUpdate = req
	return nil, f.updateErr
}

func (f *fakeRoleClient) DeleteRole(_ context.Context, id string) error {
	f.gotDelete = id
	return f.deleteErr
}

func (f *fakeRoleClient) ListRoles(_ context.Context, req *qrdbtransport.ListRolesRequest) (*qrdbtransport.ListRolesResponse, error) {
	f.gotList = req
	return f.listResp, f.listErr
}

func (f *fakeRoleClient) ReconcileBuiltInRoles(_ context.Context, req *qrdbtransport.ReconcileRolesRequest) (*qrdbtransport.ReconcileRolesResponse, error) {
	f.gotRec = req
	return f.recResp, f.recErr
}

func (f *fakeRoleClient) GetRoleByName(_ context.Context, req *qrdbtransport.GetRoleByNameRequest) (*qrdbtransport.Role, error) {
	f.gotByName = req
	return f.byNameResp, f.byNameErr
}

// Inert stubs to satisfy the RoleClient interface.
func (f *fakeRoleClient) AssignRole(context.Context, uint64, string) error   { return nil }
func (f *fakeRoleClient) UnassignRole(context.Context, uint64, string) error { return nil }
func (f *fakeRoleClient) ListPermissions(context.Context) (*qrdbtransport.ListPermissionsResponse, error) {
	return nil, nil
}
func (f *fakeRoleClient) ResolvePermissions(context.Context, *qrdbtransport.ResolvePermissionsRequest) (*qrdbtransport.ResolvePermissionsResponse, error) {
	return nil, nil
}

var _ qrdbtransport.RoleClient = (*fakeRoleClient)(nil)

// fakeRoleResolver hands back a fixed RoleClient, or a resolve error.
type fakeRoleResolver struct {
	client qrdbtransport.RoleClient
	err    error
}

func (r *fakeRoleResolver) RoleClient(context.Context) (qrdbtransport.RoleClient, error) {
	return r.client, r.err
}

var _ RoleClientResolver = (*fakeRoleResolver)(nil)

func TestQRDBRoleStorage_ClientNotConfigured(t *testing.T) {
	ctx := context.Background()

	// Nil resolver: every call fails closed with a configuration error.
	s := NewQRDBRoleStorage(nil)
	err := s.CreateRole(ctx, &Role{ID: "r1"})
	var qbErr *dberrors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, dberrors.ErrConfiguration, qbErr.Code)

	// Resolver returns an error -> wrapped as a generic internal error.
	s = NewQRDBRoleStorage(&fakeRoleResolver{err: errors.New("shard offline")})
	_, err = s.GetRole(ctx, "t", "r1")
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, dberrors.ErrInternal, qbErr.Code)

	// Resolver returns a nil client -> configuration error.
	s = NewQRDBRoleStorage(&fakeRoleResolver{client: nil})
	err = s.DeleteRole(ctx, "t", "r1")
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, dberrors.ErrConfiguration, qbErr.Code)
}

func TestQRDBRoleStorage_CreateRole(t *testing.T) {
	ctx := context.Background()

	// Happy path: the broker ID is passed straight through (single ID space).
	fc := &fakeRoleClient{createResp: &qrdbtransport.Role{ID: "r1"}}
	s := NewQRDBRoleStorage(&fakeRoleResolver{client: fc})
	require.NoError(t, s.CreateRole(ctx, &Role{
		ID: "r1", Name: "admin", TenantID: "t1", Permissions: []string{"kv:get"},
	}))
	require.NotNil(t, fc.gotCreate)
	assert.Equal(t, "r1", fc.gotCreate.ID)
	assert.Equal(t, "admin", fc.gotCreate.Name)
	assert.Equal(t, []string{"kv:get"}, fc.gotCreate.Permissions)

	// Name collision maps to the broker's ErrRoleAlreadyExists sentinel.
	//
	// ASSERTED ON THE ERROR'S FIELDS, BECAUSE errors.Is CANNOT TELL THESE APART.
	// Every sentinel in this package carrying dberrors.ErrAlreadyExists matches
	// every other one — QBError.Is compares the CODE alone — so
	// errors.Is(err, ErrRoleAlreadyExists) is equally true for a group
	// conflict, a user conflict, or a duplicate role ID. That is what this
	// assertion used to be, and it could not fail for the reason it named:
	// mapping a name collision to ErrGroupAlreadyExists left it green (#449).
	//
	// Op separates role from group and user; the condition message separates a
	// duplicate NAME from a duplicate ID, which share both Code and Op. Neither
	// is a substitute for making the sentinels matchable — that is #449's own
	// scope and an owner call, since it changes how every consumer tests for
	// "already exists" — but a test standing in for the guarantee must not be
	// blind to it in the meantime.
	fc = &fakeRoleClient{createErr: &qrdbrole.RoleNameExistsError{Name: "admin", TenantID: "t1"}}
	s = NewQRDBRoleStorage(&fakeRoleResolver{client: fc})
	err := s.CreateRole(ctx, &Role{ID: "r1", Name: "admin", TenantID: "t1"})

	var createErr *dberrors.QBError
	require.ErrorAs(t, err, &createErr)
	assert.Equal(t, dberrors.ErrAlreadyExists, createErr.Code)
	assert.Equal(t, "iam.role", createErr.Op,
		"a group or user conflict carries a different Op and would otherwise pass here")
	require.NotNil(t, createErr.Err)
	assert.EqualError(t, createErr.Err, "role already exists",
		"a duplicate ID shares this Code and Op; only the condition distinguishes them")
}

func TestQRDBRoleStorage_GetRole(t *testing.T) {
	ctx := context.Background()

	// Happy path: the transport role projects onto the broker model.
	fc := &fakeRoleClient{getResp: &qrdbtransport.Role{
		ID: "r1", Name: "admin", TenantID: "t1", Permissions: []string{"kv:get", "kv:put"},
	}}
	s := NewQRDBRoleStorage(&fakeRoleResolver{client: fc})
	role, err := s.GetRole(ctx, "t1", "r1")
	require.NoError(t, err)
	assert.Equal(t, "r1", fc.gotGet)
	assert.Equal(t, "admin", role.Name)
	assert.Equal(t, []string{"kv:get", "kv:put"}, role.Permissions)

	// Not found maps to ErrRoleNotFound.
	fc = &fakeRoleClient{getErr: &qrdbrole.RoleNotFoundError{ID: "r1"}}
	s = NewQRDBRoleStorage(&fakeRoleResolver{client: fc})
	_, err = s.GetRole(ctx, "t1", "r1")
	assert.True(t, errors.Is(err, ErrRoleNotFound), "want ErrRoleNotFound, got %v", err)
}

func TestQRDBRoleStorage_UpdateRole(t *testing.T) {
	ctx := context.Background()

	fc := &fakeRoleClient{}
	s := NewQRDBRoleStorage(&fakeRoleResolver{client: fc})
	require.NoError(t, s.UpdateRole(ctx, &Role{
		ID: "r1", Name: "admin2", Permissions: []string{"kv:get"},
	}))
	require.NotNil(t, fc.gotUpdate)
	assert.Equal(t, "r1", fc.gotUpdate.ID)
	assert.Equal(t, "admin2", fc.gotUpdate.Name)

	// Generic qrdb error is wrapped (never swallowed) as ErrInternal.
	fc = &fakeRoleClient{updateErr: errors.New("storage write failed")}
	s = NewQRDBRoleStorage(&fakeRoleResolver{client: fc})
	err := s.UpdateRole(ctx, &Role{ID: "r1"})
	var qbErr *dberrors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, dberrors.ErrInternal, qbErr.Code)
}

func TestQRDBRoleStorage_DeleteRole(t *testing.T) {
	ctx := context.Background()

	fc := &fakeRoleClient{}
	s := NewQRDBRoleStorage(&fakeRoleResolver{client: fc})
	require.NoError(t, s.DeleteRole(ctx, "t1", "r1"))
	assert.Equal(t, "r1", fc.gotDelete)

	// Not found maps to ErrRoleNotFound.
	fc = &fakeRoleClient{deleteErr: &qrdbrole.RoleNotFoundError{ID: "r1"}}
	s = NewQRDBRoleStorage(&fakeRoleResolver{client: fc})
	err := s.DeleteRole(ctx, "t1", "r1")
	assert.True(t, errors.Is(err, ErrRoleNotFound), "want ErrRoleNotFound, got %v", err)
}

func TestQRDBRoleStorage_ListRoles(t *testing.T) {
	ctx := context.Background()

	fc := &fakeRoleClient{listResp: &qrdbtransport.ListRolesResponse{
		Roles: []*qrdbtransport.Role{
			{ID: "r1", Name: "admin", TenantID: "t1"},
			{ID: "r2", Name: "viewer", TenantID: "t1"},
		},
	}}
	s := NewQRDBRoleStorage(&fakeRoleResolver{client: fc})
	roles, err := s.ListRoles(ctx, "t1", ListOptions{Limit: 10, Offset: 20})
	require.NoError(t, err)
	require.Len(t, roles, 2)
	assert.Equal(t, "admin", roles[0].Name)
	// Offset 20 / Limit 10 -> page 3 (offset/limit + 1).
	require.NotNil(t, fc.gotList)
	assert.Equal(t, 3, fc.gotList.Page)
	assert.Equal(t, 10, fc.gotList.PageSize)

	// List error wraps as ErrInternal.
	fc = &fakeRoleClient{listErr: errors.New("scan failed")}
	s = NewQRDBRoleStorage(&fakeRoleResolver{client: fc})
	_, err = s.ListRoles(ctx, "t1", ListOptions{})
	var qbErr *dberrors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, dberrors.ErrInternal, qbErr.Code)
}

func TestPageFromOffset(t *testing.T) {
	// No limit -> page 1.
	assert.Equal(t, 1, pageFromOffset(50, 0))
	// No offset -> page 1.
	assert.Equal(t, 1, pageFromOffset(0, 10))
	// Negative inputs -> page 1.
	assert.Equal(t, 1, pageFromOffset(-5, 10))
	// Exact multiple -> computed page.
	assert.Equal(t, 3, pageFromOffset(20, 10))
}

func TestTransportRoleToIAM_Nil(t *testing.T) {
	// A nil transport role projects to a nil broker role (no panic).
	assert.Nil(t, transportRoleToIAM(nil))
}

func TestMapQRDBRoleError_Nil(t *testing.T) {
	// A nil input error is passed through as nil.
	assert.NoError(t, mapQRDBRoleError("op", nil))
}

func TestMapQRDBRoleError_InvalidPermission(t *testing.T) {
	// qrdb's invalid-permission rejection is a client-input error: it must be
	// classified as ErrInvalidInput (-> 400 / InvalidArgument), never internal.
	src := &qrdbrole.InvalidPermissionError{Permission: "read"}
	err := mapQRDBRoleError("create", src)

	var qbErr *dberrors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, dberrors.ErrInvalidInput, qbErr.Code)
	assert.Equal(t, "iam.role.qrdb.create", qbErr.Op)
	// The original typed error stays in the chain.
	var perm *qrdbrole.InvalidPermissionError
	assert.ErrorAs(t, err, &perm)
}

func TestMapQRDBRoleError_Validation(t *testing.T) {
	// qrdb role request validation failures are client-input errors too.
	src := &qrdbrole.ValidationError{Field: "name", Reason: "empty"}
	err := mapQRDBRoleError("update", src)

	var qbErr *dberrors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, dberrors.ErrInvalidInput, qbErr.Code)
	assert.Equal(t, "iam.role.qrdb.update", qbErr.Op)
}
