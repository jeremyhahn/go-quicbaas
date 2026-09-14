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
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// =============================================================================
// go-quicbaas #541 — RoleStorage.GetRoleByName.
//
// "Which role is named N in tenant T" used to be answered by listing the
// tenant's roles and scanning the page, which the qrdb-backed store caps at its
// default page size: a tenant holding more roles than one page could fail to
// resolve a role that exists. These tests pin the lookup on all three stores,
// and pin the index the KVStore-backed store maintains to keep it answerable
// across create, rename and delete.
// =============================================================================

// -----------------------------------------------------------------------------
// BackendStorage — the tenant-scoped name index
// -----------------------------------------------------------------------------

// putTestRole creates one role through the store under test.
func putTestRole(t *testing.T, storage *BackendStorage, tenantID, roleID, name string) *Role {
	t.Helper()
	role := &Role{
		ID:          roleID,
		TenantID:    tenantID,
		Name:        name,
		Permissions: []string{"kv:read"},
	}
	require.NoError(t, storage.CreateRole(context.Background(), role))
	return role
}

// TestBackendStorage_GetRoleByName_Hit resolves a created role by its name.
func TestBackendStorage_GetRoleByName_Hit(t *testing.T) {
	ctx := context.Background()
	storage := NewBackendStorage(newMockKVStore())
	putTestRole(t, storage, "acme", "role-1", "auditor")

	got, err := storage.GetRoleByName(ctx, "acme", "auditor")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "role-1", got.ID)
	assert.Equal(t, "auditor", got.Name)
	assert.Equal(t, []string{"kv:read"}, got.Permissions)
}

// TestBackendStorage_GetRoleByName_Miss reports an absent name as the
// ErrRoleNotFound SENTINEL, so IsRoleNotFound — which reads the typed inner
// error, not a bare code — recognises it.
func TestBackendStorage_GetRoleByName_Miss(t *testing.T) {
	ctx := context.Background()
	storage := NewBackendStorage(newMockKVStore())
	putTestRole(t, storage, "acme", "role-1", "auditor")

	for _, name := range []string{"nosuchrole", ""} {
		got, err := storage.GetRoleByName(ctx, "acme", name)
		assert.Nil(t, got, "name %q", name)
		require.Error(t, err, "name %q", name)
		assert.True(t, IsRoleNotFound(err), "name %q: want ErrRoleNotFound, got %v", name, err)
	}
}

// TestBackendStorage_GetRoleByName_TenantIsolation refuses to answer one
// tenant's name from another tenant's index: the index is tenant-scoped, and a
// role name is only unique WITHIN a tenant.
func TestBackendStorage_GetRoleByName_TenantIsolation(t *testing.T) {
	ctx := context.Background()
	storage := NewBackendStorage(newMockKVStore())
	putTestRole(t, storage, "acme", "role-acme", "auditor")
	putTestRole(t, storage, "globex", "role-globex", "auditor")

	acme, err := storage.GetRoleByName(ctx, "acme", "auditor")
	require.NoError(t, err)
	assert.Equal(t, "role-acme", acme.ID)

	globex, err := storage.GetRoleByName(ctx, "globex", "auditor")
	require.NoError(t, err)
	assert.Equal(t, "role-globex", globex.ID)

	missing, err := storage.GetRoleByName(ctx, "initech", "auditor")
	assert.Nil(t, missing)
	assert.True(t, IsRoleNotFound(err))
}

// TestBackendStorage_CreateRole_DuplicateName refuses a second role wearing a
// name the tenant already holds. The index maps one name to one role, so
// admitting the duplicate would either overwrite the entry or make the new role
// invisible to the lookup — and the canonical qrdb store refuses it too.
func TestBackendStorage_CreateRole_DuplicateName(t *testing.T) {
	ctx := context.Background()
	storage := NewBackendStorage(newMockKVStore())
	putTestRole(t, storage, "acme", "role-1", "auditor")

	err := storage.CreateRole(ctx, &Role{ID: "role-2", TenantID: "acme", Name: "auditor"})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrAlreadyExists)

	// The original still owns the name, and the rejected role was not written.
	got, err := storage.GetRoleByName(ctx, "acme", "auditor")
	require.NoError(t, err)
	assert.Equal(t, "role-1", got.ID)

	_, err = storage.GetRole(ctx, "acme", "role-2")
	require.Error(t, err)
}

// TestBackendStorage_CreateRole_DuplicateNameOtherTenant admits the same name
// in a DIFFERENT tenant: uniqueness is per tenant, not global.
func TestBackendStorage_CreateRole_DuplicateNameOtherTenant(t *testing.T) {
	ctx := context.Background()
	storage := NewBackendStorage(newMockKVStore())
	putTestRole(t, storage, "acme", "role-1", "auditor")

	require.NoError(t, storage.CreateRole(ctx, &Role{ID: "role-2", TenantID: "globex", Name: "auditor"}))
}

// TestBackendStorage_UpdateRole_Rename moves the index with the name: the new
// name resolves and the old one stops resolving.
func TestBackendStorage_UpdateRole_Rename(t *testing.T) {
	ctx := context.Background()
	storage := NewBackendStorage(newMockKVStore())
	role := putTestRole(t, storage, "acme", "role-1", "auditor")

	role.Name = "inspector"
	require.NoError(t, storage.UpdateRole(ctx, role))

	got, err := storage.GetRoleByName(ctx, "acme", "inspector")
	require.NoError(t, err)
	assert.Equal(t, "role-1", got.ID)

	stale, err := storage.GetRoleByName(ctx, "acme", "auditor")
	assert.Nil(t, stale)
	assert.True(t, IsRoleNotFound(err), "the old name must stop resolving, got %v", err)
}

// TestBackendStorage_UpdateRole_RenameOntoTakenName refuses a rename onto a
// name another role holds, and leaves both index entries untouched. Overwriting
// the entry would make the displaced role unresolvable by its own name.
func TestBackendStorage_UpdateRole_RenameOntoTakenName(t *testing.T) {
	ctx := context.Background()
	storage := NewBackendStorage(newMockKVStore())
	putTestRole(t, storage, "acme", "role-1", "auditor")
	other := putTestRole(t, storage, "acme", "role-2", "operator")

	other.Name = "auditor"
	err := storage.UpdateRole(ctx, other)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrAlreadyExists)

	held, err := storage.GetRoleByName(ctx, "acme", "auditor")
	require.NoError(t, err)
	assert.Equal(t, "role-1", held.ID, "the holder of the name must not be displaced")

	kept, err := storage.GetRoleByName(ctx, "acme", "operator")
	require.NoError(t, err)
	assert.Equal(t, "role-2", kept.ID, "the refused rename must not have dropped its own entry")

	stored, err := storage.GetRole(ctx, "acme", "role-2")
	require.NoError(t, err)
	assert.Equal(t, "operator", stored.Name, "the refused rename must not have been written")
}

// TestBackendStorage_UpdateRole_NoRenameKeepsIndex leaves the name resolvable
// when an update changes something other than the name.
func TestBackendStorage_UpdateRole_NoRenameKeepsIndex(t *testing.T) {
	ctx := context.Background()
	storage := NewBackendStorage(newMockKVStore())
	role := putTestRole(t, storage, "acme", "role-1", "auditor")

	role.Permissions = []string{"kv:read", "kv:write"}
	require.NoError(t, storage.UpdateRole(ctx, role))

	got, err := storage.GetRoleByName(ctx, "acme", "auditor")
	require.NoError(t, err)
	assert.Equal(t, "role-1", got.ID)
	assert.Equal(t, []string{"kv:read", "kv:write"}, got.Permissions)
}

// TestBackendStorage_UpdateRole_NotFoundLeavesIndexAlone refuses an update to a
// role that does not exist without inventing an index entry for it.
func TestBackendStorage_UpdateRole_NotFoundLeavesIndexAlone(t *testing.T) {
	ctx := context.Background()
	storage := NewBackendStorage(newMockKVStore())

	err := storage.UpdateRole(ctx, &Role{ID: "ghost", TenantID: "acme", Name: "auditor"})
	require.Error(t, err)

	got, err := storage.GetRoleByName(ctx, "acme", "auditor")
	assert.Nil(t, got)
	assert.True(t, IsRoleNotFound(err))
}

// TestBackendStorage_DeleteRole_ClearsIndex frees the name, so a later create
// can take it and the old name resolves to nothing in between.
func TestBackendStorage_DeleteRole_ClearsIndex(t *testing.T) {
	ctx := context.Background()
	storage := NewBackendStorage(newMockKVStore())
	putTestRole(t, storage, "acme", "role-1", "auditor")

	require.NoError(t, storage.DeleteRole(ctx, "acme", "role-1"))

	gone, err := storage.GetRoleByName(ctx, "acme", "auditor")
	assert.Nil(t, gone)
	assert.True(t, IsRoleNotFound(err))

	// The name is genuinely free, not merely unresolvable.
	require.NoError(t, storage.CreateRole(ctx, &Role{ID: "role-2", TenantID: "acme", Name: "auditor"}))
	reused, err := storage.GetRoleByName(ctx, "acme", "auditor")
	require.NoError(t, err)
	assert.Equal(t, "role-2", reused.ID)
}

// TestBackendStorage_DeleteRole_NotFoundKeepsIndex refuses to delete a role
// that does not exist, and does not disturb another role's index entry.
func TestBackendStorage_DeleteRole_NotFoundKeepsIndex(t *testing.T) {
	ctx := context.Background()
	storage := NewBackendStorage(newMockKVStore())
	putTestRole(t, storage, "acme", "role-1", "auditor")

	require.Error(t, storage.DeleteRole(ctx, "acme", "ghost"))

	got, err := storage.GetRoleByName(ctx, "acme", "auditor")
	require.NoError(t, err)
	assert.Equal(t, "role-1", got.ID)
}

// TestBackendStorage_ListRoles_SkipsNameIndexEntries guards the key LAYOUT: the
// index segment sits outside the role record prefix, so the listing scan cannot
// sweep up an index entry and try to decode it as a record.
func TestBackendStorage_ListRoles_SkipsNameIndexEntries(t *testing.T) {
	ctx := context.Background()
	storage := NewBackendStorage(newMockKVStore())
	putTestRole(t, storage, "acme", "role-1", "auditor")
	putTestRole(t, storage, "acme", "role-2", "operator")

	roles, err := storage.ListRoles(ctx, "acme", ListOptions{})
	require.NoError(t, err)
	require.Len(t, roles, 2, "listing must see the two records and neither index entry")

	names := map[string]string{}
	for _, r := range roles {
		names[r.Name] = r.ID
	}
	assert.Equal(t, map[string]string{"auditor": "role-1", "operator": "role-2"}, names)
}

// TestBackendStorage_GetRoleByName_DanglingIndex surfaces the record read's
// error when an index entry names a record that is gone. Reporting a store
// inconsistency as an ordinary absence would let a caller that keys idempotency
// off not-found take the create path against a store that still holds the name.
func TestBackendStorage_GetRoleByName_DanglingIndex(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore()
	storage := NewBackendStorage(kv)
	putTestRole(t, storage, "acme", "role-1", "auditor")

	// Remove the RECORD behind the index, leaving the pointer.
	require.NoError(t, kv.Delete(ctx, roleKey("acme", "role-1")))

	got, err := storage.GetRoleByName(ctx, "acme", "auditor")
	assert.Nil(t, got)
	require.Error(t, err)
	assert.False(t, IsRoleNotFound(err), "a dangling pointer is not an absent role: %v", err)
}

// failingRoleIndexKV answers every read of the role-name index with a
// non-not-found store error, and delegates everything else.
type failingRoleIndexKV struct {
	interfaces.KVStore
	failure error
}

func (f *failingRoleIndexKV) Get(ctx context.Context, key string) ([]byte, error) {
	if key == roleNameIndexKey("acme", "auditor") {
		return nil, f.failure
	}
	return f.KVStore.Get(ctx, key)
}

// TestBackendStorage_GetRoleByName_StoreFailure keeps a store outage distinct
// from an absent role. qrdb's ResolveSystemRoleID falls back to a DERIVED
// identifier on not-found, so reporting an outage as "no such role" would bind
// a grant to an identifier the tenant may not use.
func TestBackendStorage_GetRoleByName_StoreFailure(t *testing.T) {
	ctx := context.Background()
	outage := &dberrors.QBError{Code: dberrors.ErrInternal, Op: "kv.get", Err: errors.New("shard offline")}
	storage := NewBackendStorage(&failingRoleIndexKV{KVStore: newMockKVStore(), failure: outage})

	got, err := storage.GetRoleByName(ctx, "acme", "auditor")
	assert.Nil(t, got)
	require.Error(t, err)
	assert.False(t, IsRoleNotFound(err), "a store outage must not read as an absent role")
	assert.ErrorIs(t, err, outage)
}

// TestBackendStorage_CreateRole_IndexReadFailure refuses the create rather than
// reading a store outage as "the name is free" and admitting a duplicate the
// index cannot represent.
func TestBackendStorage_CreateRole_IndexReadFailure(t *testing.T) {
	ctx := context.Background()
	outage := &dberrors.QBError{Code: dberrors.ErrInternal, Op: "kv.get", Err: errors.New("shard offline")}
	storage := NewBackendStorage(&failingRoleIndexKV{KVStore: newMockKVStore(), failure: outage})

	err := storage.CreateRole(ctx, &Role{ID: "role-1", TenantID: "acme", Name: "auditor"})
	require.Error(t, err)
	assert.ErrorIs(t, err, outage)
}

// TestBackendStorage_DeleteTenant_ClearsRoleNameIndex leaves no index entry
// behind, so a tenant recreated under the same id does not find its role names
// already taken.
func TestBackendStorage_DeleteTenant_ClearsRoleNameIndex(t *testing.T) {
	ctx := context.Background()
	kv := newMockKVStore()
	storage := NewBackendStorage(kv)
	require.NoError(t, storage.CreateTenant(ctx, &Tenant{ID: "acme", Name: "Acme"}))
	putTestRole(t, storage, "acme", "role-1", "auditor")

	require.NoError(t, storage.DeleteTenant(ctx, "acme"))

	keys, err := kv.List(ctx, KeyPrefixTenant+"acme"+KeyPrefixRoleNameIndex)
	require.NoError(t, err)
	assert.Empty(t, keys, "the tenant cascade must drop the role-name index too")

	got, err := storage.GetRoleByName(ctx, "acme", "auditor")
	assert.Nil(t, got)
	assert.True(t, IsRoleNotFound(err))
}

// TestBackendStorage_GetRoleByName_BeyondOnePage is the regression this issue
// exists for: the lookup resolves a role that a single page of a listing would
// never show.
func TestBackendStorage_GetRoleByName_BeyondOnePage(t *testing.T) {
	ctx := context.Background()
	storage := NewBackendStorage(newMockKVStore())
	const target = "role-249"
	for i := 0; i < 250; i++ {
		id := "role-" + strconv.Itoa(i)
		putTestRole(t, storage, "acme", id, "name-"+strconv.Itoa(i))
	}

	// A caller filtering the first page (qrdb's default) would miss it.
	page, err := storage.ListRoles(ctx, "acme", ListOptions{Limit: 100})
	require.NoError(t, err)
	require.Len(t, page, 100)

	got, err := storage.GetRoleByName(ctx, "acme", "name-249")
	require.NoError(t, err)
	assert.Equal(t, target, got.ID)
}

// -----------------------------------------------------------------------------
// MemoryStorage — the scanning double
// -----------------------------------------------------------------------------

// TestMemoryStorage_GetRoleByName_Hit resolves a created role by name.
func TestMemoryStorage_GetRoleByName_Hit(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryStorage()
	require.NoError(t, storage.CreateRole(ctx, &Role{ID: "role-1", TenantID: "acme", Name: "auditor"}))

	got, err := storage.GetRoleByName(ctx, "acme", "auditor")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "role-1", got.ID)
}

// TestMemoryStorage_GetRoleByName_Miss answers an unknown name, an empty name
// and a foreign tenant with the ErrRoleNotFound sentinel.
func TestMemoryStorage_GetRoleByName_Miss(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryStorage()
	require.NoError(t, storage.CreateRole(ctx, &Role{ID: "role-1", TenantID: "acme", Name: "auditor"}))

	cases := []struct{ tenantID, name string }{
		{"acme", "nosuchrole"},
		{"acme", ""},
		{"globex", "auditor"},
	}
	for _, tc := range cases {
		got, err := storage.GetRoleByName(ctx, tc.tenantID, tc.name)
		assert.Nil(t, got, "%s/%s", tc.tenantID, tc.name)
		require.Error(t, err, "%s/%s", tc.tenantID, tc.name)
		assert.True(t, IsRoleNotFound(err), "%s/%s: got %v", tc.tenantID, tc.name, err)
	}
}

// TestMemoryStorage_GetRoleByName_Rename follows a rename without maintaining a
// second structure: the store scans its map, so the new name resolves and the
// old one does not the moment the record changes.
func TestMemoryStorage_GetRoleByName_Rename(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryStorage()
	require.NoError(t, storage.CreateRole(ctx, &Role{ID: "role-1", TenantID: "acme", Name: "auditor"}))

	require.NoError(t, storage.UpdateRole(ctx, &Role{ID: "role-1", TenantID: "acme", Name: "inspector"}))

	got, err := storage.GetRoleByName(ctx, "acme", "inspector")
	require.NoError(t, err)
	assert.Equal(t, "role-1", got.ID)

	stale, err := storage.GetRoleByName(ctx, "acme", "auditor")
	assert.Nil(t, stale)
	assert.True(t, IsRoleNotFound(err))
}

// TestMemoryStorage_GetRoleByName_AfterDelete stops resolving a deleted role.
func TestMemoryStorage_GetRoleByName_AfterDelete(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryStorage()
	require.NoError(t, storage.CreateRole(ctx, &Role{ID: "role-1", TenantID: "acme", Name: "auditor"}))
	require.NoError(t, storage.DeleteRole(ctx, "acme", "role-1"))

	got, err := storage.GetRoleByName(ctx, "acme", "auditor")
	assert.Nil(t, got)
	assert.True(t, IsRoleNotFound(err))
}

// TestMemoryStorage_GetRoleByName_ReturnsCopy hands back a copy, so a caller
// mutating the answer cannot rewrite the stored record.
func TestMemoryStorage_GetRoleByName_ReturnsCopy(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryStorage()
	require.NoError(t, storage.CreateRole(ctx, &Role{ID: "role-1", TenantID: "acme", Name: "auditor"}))

	got, err := storage.GetRoleByName(ctx, "acme", "auditor")
	require.NoError(t, err)
	got.Name = "tampered"

	again, err := storage.GetRoleByName(ctx, "acme", "auditor")
	require.NoError(t, err)
	assert.Equal(t, "auditor", again.Name)
}

// -----------------------------------------------------------------------------
// QRDBRoleStorage — delegation to qrdb's own name index
// -----------------------------------------------------------------------------

// TestQRDBRoleStorage_GetRoleByName_Delegates passes the tenant and name to
// qrdb and projects the transport role back into the broker model.
func TestQRDBRoleStorage_GetRoleByName_Delegates(t *testing.T) {
	ctx := context.Background()
	fc := &fakeRoleClient{byNameResp: &qrdbtransport.Role{
		ID:          "role-1",
		Name:        "auditor",
		TenantID:    "acme",
		Permissions: []string{"kv:read"},
	}}
	s := NewQRDBRoleStorage(&fakeRoleResolver{client: fc})

	got, err := s.GetRoleByName(ctx, "acme", "auditor")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "role-1", got.ID)
	assert.Equal(t, "auditor", got.Name)
	assert.Equal(t, []string{"kv:read"}, got.Permissions)

	require.NotNil(t, fc.gotByName)
	assert.Equal(t, "acme", fc.gotByName.TenantID)
	assert.Equal(t, "auditor", fc.gotByName.Name)
}

// TestQRDBRoleStorage_GetRoleByName_NotFound translates qrdb's not-found into
// the broker sentinel so IsRoleNotFound recognises it.
func TestQRDBRoleStorage_GetRoleByName_NotFound(t *testing.T) {
	ctx := context.Background()
	fc := &fakeRoleClient{byNameErr: &qrdbsdk.RoleNotFoundError{ID: "auditor"}}
	s := NewQRDBRoleStorage(&fakeRoleResolver{client: fc})

	got, err := s.GetRoleByName(ctx, "acme", "auditor")
	assert.Nil(t, got)
	require.Error(t, err)
	assert.True(t, IsRoleNotFound(err))
}

// TestQRDBRoleStorage_GetRoleByName_Errors fails closed on a resolver that has
// no client, and wraps an unknown store error rather than swallowing it.
func TestQRDBRoleStorage_GetRoleByName_Errors(t *testing.T) {
	ctx := context.Background()

	unconfigured := NewQRDBRoleStorage(nil)
	got, err := unconfigured.GetRoleByName(ctx, "acme", "auditor")
	assert.Nil(t, got)
	require.Error(t, err)
	var qb *dberrors.QBError
	require.ErrorAs(t, err, &qb)
	assert.Equal(t, dberrors.ErrConfiguration, qb.Code)

	outage := errors.New("shard offline")
	failing := NewQRDBRoleStorage(&fakeRoleResolver{client: &fakeRoleClient{byNameErr: outage}})
	got, err = failing.GetRoleByName(ctx, "acme", "auditor")
	assert.Nil(t, got)
	require.Error(t, err)
	assert.False(t, IsRoleNotFound(err), "an outage must not read as an absent role")
	assert.ErrorIs(t, err, outage)
}

// TestQRDBRoleStorage_GetRoleByName_NilRoleNilError refuses the broken
// RoleServicer contract instead of dereferencing nil or reading it as an
// absence — the latter would send first-boot down the derived-identifier path.
func TestQRDBRoleStorage_GetRoleByName_NilRoleNilError(t *testing.T) {
	ctx := context.Background()
	s := NewQRDBRoleStorage(&fakeRoleResolver{client: &fakeRoleClient{}})

	got, err := s.GetRoleByName(ctx, "acme", "auditor")
	assert.Nil(t, got)
	require.Error(t, err)
	assert.False(t, IsRoleNotFound(err))
	var qb *dberrors.QBError
	require.ErrorAs(t, err, &qb)
	assert.Equal(t, dberrors.ErrInternal, qb.Code)
}

// -----------------------------------------------------------------------------
// IdentityStore — the service seam
// -----------------------------------------------------------------------------

// TestIdentityStore_GetRoleByName resolves through the configured storage.
func TestIdentityStore_GetRoleByName(t *testing.T) {
	ctx := context.Background()
	mem := NewMemoryStorage()
	store := NewIdentityStore(IdentityStoreOptions{RoleStorage: mem, UserStorage: mem})
	require.NoError(t, mem.CreateRole(ctx, &Role{ID: "role-1", TenantID: "acme", Name: "auditor"}))

	got, err := store.GetRoleByName(ctx, "acme", "auditor")
	require.NoError(t, err)
	assert.Equal(t, "role-1", got.ID)

	missing, err := store.GetRoleByName(ctx, "acme", "nosuchrole")
	assert.Nil(t, missing)
	assert.True(t, IsRoleNotFound(err))
}

// TestIdentityStore_GetRoleByName_Validation rejects an empty tenant or name
// with a field-named validation error instead of asking the store.
func TestIdentityStore_GetRoleByName_Validation(t *testing.T) {
	ctx := context.Background()
	mem := NewMemoryStorage()
	store := NewIdentityStore(IdentityStoreOptions{RoleStorage: mem, UserStorage: mem})

	cases := []struct{ tenantID, name, field string }{
		{"", "auditor", "tenant_id"},
		{"acme", "", "name"},
	}
	for _, tc := range cases {
		got, err := store.GetRoleByName(ctx, tc.tenantID, tc.name)
		assert.Nil(t, got, "%q/%q", tc.tenantID, tc.name)
		require.Error(t, err, "%q/%q", tc.tenantID, tc.name)
		var v *ValidationError
		require.ErrorAs(t, err, &v)
		assert.Equal(t, tc.field, v.Field)
	}
}
