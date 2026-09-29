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

package service

import (
	"context"
	stderrors "errors"
	"sort"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// =============================================================================
// go-quicbaas#694 — the RBAC role-store verbs resolve a role NAME by lookup.
//
// findRoleIDByName used to list the tenant's roles and scan the page. Against
// the store the server actually configures that was wrong: the qrdb-backed
// RoleStorage maps `ListOptions{Limit: 0}` to qrdb's `PageSize: 0`, which qrdb
// reads as its DEFAULT page of 100 rather than "all rows". Every role past the
// first hundred was invisible to AssignRole, RevokeRole, DeleteRole and the
// CreateRole duplicate check.
// =============================================================================

// qrdbPagedRoleStorage is a RoleStorage that reproduces qrdb's PAGING contract
// on top of the in-memory double: an unset Limit yields the default page, not
// every row. That is the whole defect under test, so a double that returned
// everything would make these tests pass against the code that was broken.
//
// The page is taken from a NAME-ordered listing. The in-memory store iterates a
// map, so its page is a random 100 of the tenant's roles and a test asserting a
// specific role is hidden would be flaky; a real store pages a stable order.
//
// GetRoleByName is delegated unchanged: it is a lookup, and qrdb answers it
// from its own name index without paging.
type qrdbPagedRoleStorage struct {
	iam.RoleStorage
}

// qrdbDefaultPageSize is the page qrdb serves when a request names none.
const qrdbDefaultPageSize = 100

func (s *qrdbPagedRoleStorage) ListRoles(ctx context.Context, tenantID string, opts iam.ListOptions) ([]*iam.Role, error) {
	all, err := s.RoleStorage.ListRoles(ctx, tenantID, iam.ListOptions{})
	if err != nil {
		return nil, err
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Name < all[j].Name })

	limit := opts.Limit
	if limit <= 0 {
		limit = qrdbDefaultPageSize
	}
	if opts.Offset >= len(all) {
		return []*iam.Role{}, nil
	}
	all = all[opts.Offset:]
	if limit < len(all) {
		all = all[:limit]
	}
	return all, nil
}

// roleName is the zero-padded name of the i-th seeded role. Padding makes the
// NAME order the numeric order, so roleName(140) is genuinely the 141st row and is
// deterministically absent from a 100-row page.
func roleName(i int) string {
	s := strconv.Itoa(i)
	for len(s) < 3 {
		s = "0" + s
	}
	return "role-" + s
}

// newPagedRoleStoreService wires the paging double behind the rbacService and
// seeds `count` roles, returning the service, the tenant context and the
// underlying memory storage for direct seeding.
func newPagedRoleStoreService(t *testing.T, tenantID string, count int) (QuicBaaS, context.Context, *iam.MemoryStorage) {
	t.Helper()

	storage := iam.NewMemoryStorage()
	is := iam.NewIdentityStore(iam.IdentityStoreOptions{
		UserStorage: storage,
		RoleStorage: &qrdbPagedRoleStorage{RoleStorage: storage},
	})
	svc, ctx := newRoleStoreTestServiceWithStore(t, tenantID, is)

	for i := 0; i < count; i++ {
		require.NoError(t, storage.CreateRole(ctx, &iam.Role{
			ID:       roleName(i),
			TenantID: tenantID,
			Name:     roleName(i),
		}))
	}
	return svc, ctx, storage
}

// TestRoleStore_PagingDoubleHidesRolesPastTheFirstPage asserts the FIXTURE
// itself: the double must actually hide the role, or every test below would
// pass against the list-and-filter this change removed.
func TestRoleStore_PagingDoubleHidesRolesPastTheFirstPage(t *testing.T) {
	svc, ctx, _ := newPagedRoleStoreService(t, "tenant-paged", 150)

	listResp, err := svc.ListAuthRoles(ctx, &AuthListRolesRequest{})
	require.NoError(t, err)
	require.NotNil(t, listResp)
	assert.Len(t, listResp.Roles, qrdbDefaultPageSize, "an unset limit must yield one page, as qrdb does")

	seen := make(map[string]struct{}, len(listResp.Roles))
	for _, role := range listResp.Roles {
		seen[role.RoleName] = struct{}{}
	}
	assert.NotContains(t, seen, roleName(140),
		"the fixture is void unless the page genuinely hides the role the tests below resolve")
	assert.Contains(t, seen, roleName(0), "the page must still hold the rows it does show")
}

// TestRoleStore_DeleteRole_PastFirstPage deletes a role the first page never
// shows. Under the old list-and-filter this returned 404 for a role that exists.
func TestRoleStore_DeleteRole_PastFirstPage(t *testing.T) {
	svc, ctx, storage := newPagedRoleStoreService(t, "tenant-paged", 150)

	resp, err := svc.DeleteAuthRole(ctx, &AuthDeleteRoleRequest{RoleName: roleName(140)})
	require.NoError(t, err)
	require.NotNil(t, resp)

	_, err = storage.GetRole(ctx, "tenant-paged", roleName(140))
	require.Error(t, err)
	assert.True(t, iam.IsRoleNotFound(err), "the role must actually be gone: %v", err)
}

// TestRoleStore_AssignRevokeRole_PastFirstPage binds and unbinds a role beyond
// the first page. The revocation is the security-relevant direction: a grant
// that cannot be withdrawn is a grant that stands.
func TestRoleStore_AssignRevokeRole_PastFirstPage(t *testing.T) {
	svc, ctx, storage := newPagedRoleStoreService(t, "tenant-paged", 150)
	require.NoError(t, storage.CreateUser(ctx, &iam.User{
		ID: "1", TenantID: "tenant-paged", Username: "alice", Name: "Alice", Email: "alice@paged.test",
	}))

	assignResp, err := svc.AssignRole(ctx, &AuthAssignRoleRequest{Username: "alice", RoleName: roleName(140)})
	require.NoError(t, err)
	require.NotNil(t, assignResp)

	bound, err := storage.GetUser(ctx, "tenant-paged", "1")
	require.NoError(t, err)
	assert.Contains(t, bound.Roles, roleName(140))

	revokeResp, err := svc.RevokeRole(ctx, &AuthRevokeRoleRequest{Username: "alice", RoleName: roleName(140)})
	require.NoError(t, err)
	require.NotNil(t, revokeResp)

	unbound, err := storage.GetUser(ctx, "tenant-paged", "1")
	require.NoError(t, err)
	assert.NotContains(t, unbound.Roles, roleName(140), "a revocation that cannot find the role leaves the grant standing")
}

// TestRoleStore_CreateRole_DuplicatePastFirstPage refuses a name already taken
// by a role beyond the first page. Under the old scan this was fail-OPEN: the
// duplicate check saw nothing and the create proceeded.
func TestRoleStore_CreateRole_DuplicatePastFirstPage(t *testing.T) {
	svc, ctx, _ := newPagedRoleStoreService(t, "tenant-paged", 150)

	resp, err := svc.CreateAuthRole(ctx, &AuthCreateRoleRequest{
		RoleName:    roleName(140),
		Permissions: []string{"keys:read"},
	})
	assert.Nil(t, resp)
	require.Error(t, err)

	var qErr *dberrors.QBError
	require.True(t, dberrors.As(err, &qErr))
	assert.Equal(t, dberrors.ErrInvalidInput, qErr.Code)
}

// TestRoleStore_RoleNameLookup_StillReportsAbsence keeps the other half of the
// contract: a name the tenant genuinely does not hold is a typed NotFoundError,
// not an internal fault.
func TestRoleStore_RoleNameLookup_StillReportsAbsence(t *testing.T) {
	svc, ctx, _ := newPagedRoleStoreService(t, "tenant-paged", 5)

	resp, err := svc.DeleteAuthRole(ctx, &AuthDeleteRoleRequest{RoleName: "nosuchrole"})
	assert.Nil(t, resp)
	require.Error(t, err)

	var nf *api.NotFoundError
	require.True(t, stderrors.As(err, &nf))
	assert.Equal(t, "role", nf.Resource)
	assert.Equal(t, "nosuchrole", nf.ID)
}

// lookupOutageRoleStorage fails every by-name lookup with a non-not-found
// error, delegating the rest to the embedded store.
type lookupOutageRoleStorage struct {
	iam.RoleStorage
	err error
}

func (s *lookupOutageRoleStorage) GetRoleByName(context.Context, string, string) (*iam.Role, error) {
	return nil, s.err
}

// TestRoleStore_CreateRole_RefusesWhenLookupFails is the fail-closed half of
// the duplicate check.
//
// The check has three outcomes and only one may continue. A store outage is not
// "no duplicate": admitting the create because the lookup FAILED would produce
// exactly the duplicate the check exists to prevent, and the old form — which
// tested only `err == nil` — did precisely that.
func TestRoleStore_CreateRole_RefusesWhenLookupFails(t *testing.T) {
	outage := stderrors.New("shard offline")
	storage := iam.NewMemoryStorage()
	is := iam.NewIdentityStore(iam.IdentityStoreOptions{
		UserStorage: storage,
		RoleStorage: &lookupOutageRoleStorage{RoleStorage: storage, err: outage},
	})
	svc, ctx := newRoleStoreTestServiceWithStore(t, "tenant-outage", is)

	resp, err := svc.CreateAuthRole(ctx, &AuthCreateRoleRequest{
		RoleName:    "editor",
		Permissions: []string{"keys:read"},
	})
	assert.Nil(t, resp)
	require.Error(t, err)
	assert.ErrorIs(t, err, outage)

	var qErr *dberrors.QBError
	require.True(t, dberrors.As(err, &qErr))
	assert.Equal(t, dberrors.ErrInternal, qErr.Code)

	// Nothing was written: the create must not have run.
	roles, err := storage.ListRoles(ctx, "tenant-outage", iam.ListOptions{})
	require.NoError(t, err)
	assert.Empty(t, roles, "a create admitted on a failed lookup is the defect")
}

// TestRoleStore_AssignRole_LookupOutageIsNotAnAbsentRole reports a store
// failure during the role resolution as internal, not as a missing role, so an
// operator is not told a role they hold does not exist.
func TestRoleStore_AssignRole_LookupOutageIsNotAnAbsentRole(t *testing.T) {
	outage := stderrors.New("shard offline")
	storage := iam.NewMemoryStorage()
	is := iam.NewIdentityStore(iam.IdentityStoreOptions{
		UserStorage: storage,
		RoleStorage: &lookupOutageRoleStorage{RoleStorage: storage, err: outage},
	})
	svc, ctx := newRoleStoreTestServiceWithStore(t, "tenant-outage", is)
	require.NoError(t, storage.CreateUser(ctx, &iam.User{
		ID: "1", TenantID: "tenant-outage", Username: "alice", Name: "Alice", Email: "alice@outage.test",
	}))

	resp, err := svc.AssignRole(ctx, &AuthAssignRoleRequest{Username: "alice", RoleName: "editor"})
	assert.Nil(t, resp)
	require.Error(t, err)

	var nf *api.NotFoundError
	assert.False(t, stderrors.As(err, &nf), "an outage must not be reported as an absent role")

	var qErr *dberrors.QBError
	require.True(t, dberrors.As(err, &qErr))
	assert.Equal(t, dberrors.ErrInternal, qErr.Code)
}
