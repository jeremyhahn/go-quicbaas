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

package qrdbadapter

import (
	"context"
	"errors"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// -----------------------------------------------------------------------------
// Fake qrdb UserServicer — an in-memory implementation of the qrdb user core
// store with username and cert-thumbprint secondary indexes. It mirrors qrdb's
// server-assigned-uint64 semantics: CreateUser mints the next id, UpdateUser
// applies the partial-replace contract, and UserByCertThumbprint resolves the
// index UpdateUser maintains.
// -----------------------------------------------------------------------------

type fakeUserServicer struct {
	users  map[uint64]*qrdbsdk.User
	nextID uint64
	// forceErr, when set, is returned by every method (drives error-path tests).
	forceErr error
}

func newFakeUserServicer() *fakeUserServicer {
	return &fakeUserServicer{users: make(map[uint64]*qrdbsdk.User), nextID: 1000}
}

func (f *fakeUserServicer) CreateUser(_ context.Context, req *qrdbsdk.CreateUserRequest) (*qrdbsdk.User, error) {
	if f.forceErr != nil {
		return nil, f.forceErr
	}
	for _, u := range f.users {
		if u.TenantID == req.TenantID && u.Username == req.Username {
			return nil, &qrdbsdk.UsernameExistsError{Username: req.Username, TenantID: req.TenantID}
		}
	}
	f.nextID++
	u := &qrdbsdk.User{
		ID:       f.nextID,
		Username: req.Username,
		TenantID: req.TenantID,
		RoleIDs:  append([]string(nil), req.RoleIDs...),
		Enabled:  true,
	}
	f.users[u.ID] = u
	return cloneUser(u), nil
}

func (f *fakeUserServicer) GetUser(_ context.Context, id uint64) (*qrdbsdk.User, error) {
	if f.forceErr != nil {
		return nil, f.forceErr
	}
	u, ok := f.users[id]
	if !ok {
		return nil, &qrdbsdk.UserNotFoundError{ID: id}
	}
	return cloneUser(u), nil
}

func (f *fakeUserServicer) GetUserByUsername(_ context.Context, tenantID, username string) (*qrdbsdk.User, error) {
	if f.forceErr != nil {
		return nil, f.forceErr
	}
	for _, u := range f.users {
		if u.TenantID == tenantID && u.Username == username {
			return cloneUser(u), nil
		}
	}
	return nil, &qrdbsdk.UserNotFoundError{Lookup: tenantID + "/" + username}
}

func (f *fakeUserServicer) UserByCertThumbprint(_ context.Context, thumbprint string) (*qrdbsdk.User, error) {
	if f.forceErr != nil {
		return nil, f.forceErr
	}
	for _, u := range f.users {
		for _, b := range u.CertBindings {
			if b.Thumbprint == thumbprint {
				return cloneUser(u), nil
			}
		}
	}
	return nil, &qrdbsdk.UserNotFoundError{Lookup: "cert_spki/" + thumbprint}
}

func (f *fakeUserServicer) AddCertBinding(_ context.Context, userID uint64, thumbprint string, _ int64) (*qrdbsdk.User, error) {
	u, ok := f.users[userID]
	if !ok {
		return nil, &qrdbsdk.UserNotFoundError{ID: userID}
	}
	u.CertBindings = append(u.CertBindings, qrdbsdk.CertBinding{Thumbprint: thumbprint})
	return cloneUser(u), nil
}

func (f *fakeUserServicer) UpdateUser(_ context.Context, req *qrdbsdk.UpdateUserRequest) (*qrdbsdk.User, error) {
	if f.forceErr != nil {
		return nil, f.forceErr
	}
	u, ok := f.users[req.ID]
	if !ok {
		return nil, &qrdbsdk.UserNotFoundError{ID: req.ID}
	}
	if req.Username != "" {
		u.Username = req.Username
	}
	if req.RoleIDs != nil {
		u.RoleIDs = append([]string(nil), req.RoleIDs...)
	}
	if req.Attributes != nil {
		u.Attributes = req.Attributes
	}
	if req.CertBindings != nil {
		u.CertBindings = append([]qrdbsdk.CertBinding(nil), req.CertBindings...)
	}
	return cloneUser(u), nil
}

func (f *fakeUserServicer) DeleteUser(_ context.Context, id uint64) error {
	if f.forceErr != nil {
		return f.forceErr
	}
	if _, ok := f.users[id]; !ok {
		return &qrdbsdk.UserNotFoundError{ID: id}
	}
	delete(f.users, id)
	return nil
}

func (f *fakeUserServicer) ListUsers(_ context.Context, req *qrdbsdk.ListUsersRequest) (*qrdbsdk.ListUsersResponse, error) {
	if f.forceErr != nil {
		return nil, f.forceErr
	}
	out := make([]*qrdbsdk.User, 0)
	for _, u := range f.users {
		if u.TenantID == req.TenantID {
			out = append(out, cloneUser(u))
		}
	}
	return &qrdbsdk.ListUsersResponse{Users: out, Total: len(out)}, nil
}

func (f *fakeUserServicer) EnableUser(_ context.Context, _ uint64) error  { return nil }
func (f *fakeUserServicer) DisableUser(_ context.Context, _ uint64) error { return nil }

var _ qrdbsdk.UserServicer = (*fakeUserServicer)(nil)

func cloneUser(u *qrdbsdk.User) *qrdbsdk.User {
	cp := *u
	cp.RoleIDs = append([]string(nil), u.RoleIDs...)
	cp.CertBindings = append([]qrdbsdk.CertBinding(nil), u.CertBindings...)
	if u.Attributes != nil {
		cp.Attributes = make(map[string]string, len(u.Attributes))
		for k, v := range u.Attributes {
			cp.Attributes[k] = v
		}
	}
	return &cp
}

// fakeResolver returns a fixed servicer (or nil + error) for the nil-resolver
// fail-closed test.
type fakeResolver struct {
	svc qrdbsdk.UserServicer
	err error
}

func (r *fakeResolver) UserServicer(_ context.Context) (qrdbsdk.UserServicer, error) {
	return r.svc, r.err
}

// newTestUserStorage wires a QRDBUserStorage over the in-memory fakes using the
// barrier-tenant routing KV double from idmap_test.go.
func newTestUserStorage(t *testing.T, svc qrdbsdk.UserServicer, tenants ...string) (*QRDBUserStorage, *routingTestKV) {
	t.Helper()
	kv := newRoutingTestKV()
	for _, tn := range tenants {
		kv.barrierTenant(tn)
	}
	store := NewQRDBUserStorage(
		&fakeResolver{svc: svc},
		iam.NewBackendBrokerProfileStore(kv),
		NewBackendIDMap(kv),
		kv,
	)
	return store, kv
}

// -----------------------------------------------------------------------------
// CRUD round-trip + conversion fidelity
// -----------------------------------------------------------------------------

func TestQRDBUserStorage_CreateGetRoundTrip(t *testing.T) {
	svc := newFakeUserServicer()
	store, _ := newTestUserStorage(t, svc, "acme")
	ctx := context.Background()

	u := &iam.User{
		ID:           "uuid-1",
		TenantID:     "acme",
		Name:         "alice",
		Email:        "alice@acme.test",
		Roles:        []string{"role-a", "role-b"},
		Attributes:   map[string]interface{}{"dept": "eng"},
		State:        iam.UserStateActive,
		CertBindings: []qrdbsdk.CertBinding{{Thumbprint: "spki-abc", Kind: qrdbsdk.CertBindingSPKIThumbprint}},
	}
	if err := store.CreateUser(ctx, u); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	got, err := store.GetUser(ctx, "acme", "uuid-1")
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if got.ID != "uuid-1" || got.Name != "alice" || got.Email != "alice@acme.test" {
		t.Fatalf("core fields lost: %+v", got)
	}
	if got.State != iam.UserStateActive {
		t.Fatalf("broker State lost: %q", got.State)
	}
	if len(got.Roles) != 2 || got.Roles[0] != "role-a" {
		t.Fatalf("roles lost: %+v", got.Roles)
	}
	if got.Attributes["dept"] != "eng" {
		t.Fatalf("attributes lost: %+v", got.Attributes)
	}
	if len(got.CertBindings) != 1 || got.CertBindings[0].Thumbprint != "spki-abc" {
		t.Fatalf("cert bindings lost: %+v", got.CertBindings)
	}
}

func TestQRDBUserStorage_UpdateUser(t *testing.T) {
	svc := newFakeUserServicer()
	store, _ := newTestUserStorage(t, svc, "acme")
	ctx := context.Background()

	u := &iam.User{ID: "uuid-1", TenantID: "acme", Name: "alice", Email: "alice@acme.test", Roles: []string{"r1"}}
	if err := store.CreateUser(ctx, u); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	u.Roles = []string{"r1", "r2"}
	u.Email = "alice2@acme.test"
	if err := store.UpdateUser(ctx, u); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}

	got, err := store.GetUser(ctx, "acme", "uuid-1")
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if len(got.Roles) != 2 {
		t.Fatalf("role update lost: %+v", got.Roles)
	}
	if got.Email != "alice2@acme.test" {
		t.Fatalf("email update lost: %q", got.Email)
	}
	// The old email index must no longer resolve.
	tctx := iam.ContextWithTenant(ctx, "acme")
	if _, err := store.GetUserByEmail(tctx, "alice@acme.test"); !errors.Is(err, iam.ErrUserNotFound) {
		t.Fatalf("old email index should be removed, got %v", err)
	}
	if byEmail, err := store.GetUserByEmail(tctx, "alice2@acme.test"); err != nil || byEmail.ID != "uuid-1" {
		t.Fatalf("new email index lookup failed: %v / %+v", err, byEmail)
	}
}

func TestQRDBUserStorage_DeleteUser(t *testing.T) {
	svc := newFakeUserServicer()
	store, _ := newTestUserStorage(t, svc, "acme")
	ctx := context.Background()

	u := &iam.User{ID: "uuid-1", TenantID: "acme", Name: "alice", Email: "alice@acme.test"}
	if err := store.CreateUser(ctx, u); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := store.DeleteUser(ctx, "acme", "uuid-1"); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if _, err := store.GetUser(ctx, "acme", "uuid-1"); !errors.Is(err, iam.ErrUserNotFound) {
		t.Fatalf("expected ErrUserNotFound after delete, got %v", err)
	}
	// Deleting an unknown user fails closed.
	if err := store.DeleteUser(ctx, "acme", "uuid-unknown"); !errors.Is(err, iam.ErrUserNotFound) {
		t.Fatalf("expected ErrUserNotFound for unknown delete, got %v", err)
	}
}

func TestQRDBUserStorage_ListUsers(t *testing.T) {
	svc := newFakeUserServicer()
	store, _ := newTestUserStorage(t, svc, "acme")
	ctx := context.Background()

	for _, n := range []string{"alice", "bob"} {
		u := &iam.User{ID: "uuid-" + n, TenantID: "acme", Name: n, Email: n + "@acme.test"}
		if err := store.CreateUser(ctx, u); err != nil {
			t.Fatalf("CreateUser %s: %v", n, err)
		}
	}
	users, err := store.ListUsers(ctx, "acme", iam.ListOptions{})
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("expected 2 users, got %d", len(users))
	}
}

// -----------------------------------------------------------------------------
// Email + username lookups
// -----------------------------------------------------------------------------

func TestQRDBUserStorage_GetUserByEmail(t *testing.T) {
	svc := newFakeUserServicer()
	store, _ := newTestUserStorage(t, svc, "acme")
	ctx := iam.ContextWithTenant(context.Background(), "acme")

	u := &iam.User{ID: "uuid-1", TenantID: "acme", Name: "alice", Email: "alice@acme.test"}
	if err := store.CreateUser(ctx, u); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	got, err := store.GetUserByEmail(ctx, "alice@acme.test")
	if err != nil || got.ID != "uuid-1" {
		t.Fatalf("GetUserByEmail: %v / %+v", err, got)
	}
	// Unknown email fails closed.
	if _, err := store.GetUserByEmail(ctx, "nobody@acme.test"); !errors.Is(err, iam.ErrUserNotFound) {
		t.Fatalf("expected ErrUserNotFound, got %v", err)
	}
}

// TestQRDBUserStorage_GetUserByEmail_NoTenantContext is the first-boot
// regression: the bootstrap admin is created with a tenant, but the read-back
// queries by the globally unique email with NO tenant in context. The global
// email index must resolve it (and recover the owning tenant for GetUser).
func TestQRDBUserStorage_GetUserByEmail_NoTenantContext(t *testing.T) {
	svc := newFakeUserServicer()
	// system is a prefix tenant: barrierTenant routes it to a dedicated store,
	// but the global key is written there AND read back via the same store when
	// the routing falls through. Use the shared store path by NOT registering a
	// barrier tenant for "system" so both writes and the no-tenant read land in
	// the shared store (mirroring the prefix/system tenant's shared-store routing).
	store, _ := newTestUserStorage(t, svc)
	createCtx := context.Background()

	u := &iam.User{ID: "uuid-admin", TenantID: "system", Name: "admin", Email: "admin@system.qrdb.local"}
	if err := store.CreateUser(createCtx, u); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	// Read back with NO tenant in context (the first-boot pattern).
	got, err := store.GetUserByEmail(context.Background(), "admin@system.qrdb.local")
	if err != nil {
		t.Fatalf("GetUserByEmail (no tenant ctx): %v", err)
	}
	if got.ID != "uuid-admin" || got.TenantID != "system" {
		t.Fatalf("global email lookup resolved wrong user: %+v", got)
	}
}

func TestQRDBUserStorage_GetUserByUsername(t *testing.T) {
	svc := newFakeUserServicer()
	store, _ := newTestUserStorage(t, svc, "acme")
	ctx := context.Background()

	u := &iam.User{ID: "uuid-1", TenantID: "acme", Name: "alice", Email: "alice@acme.test"}
	if err := store.CreateUser(ctx, u); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	got, err := store.GetUserByUsername(ctx, "acme", "alice")
	if err != nil || got.ID != "uuid-1" {
		t.Fatalf("GetUserByUsername: %v / %+v", err, got)
	}
	if _, err := store.GetUserByUsername(ctx, "acme", "ghost"); !errors.Is(err, iam.ErrUserNotFound) {
		t.Fatalf("expected ErrUserNotFound for unknown username, got %v", err)
	}
}

// -----------------------------------------------------------------------------
// IDMap stability across reads
// -----------------------------------------------------------------------------

func TestQRDBUserStorage_IDMapStability(t *testing.T) {
	svc := newFakeUserServicer()
	store, kv := newTestUserStorage(t, svc, "acme")
	ctx := context.Background()

	u := &iam.User{ID: "uuid-1", TenantID: "acme", Name: "alice", Email: "alice@acme.test"}
	if err := store.CreateUser(ctx, u); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	idmap := NewBackendIDMap(kv)
	qid1, err := idmap.ToQRDB(ctx, "acme", "uuid-1")
	if err != nil {
		t.Fatalf("ToQRDB: %v", err)
	}
	// Update should not change the qrdb id.
	u.Roles = []string{"r1"}
	if err := store.UpdateUser(ctx, u); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	qid2, err := idmap.ToQRDB(ctx, "acme", "uuid-1")
	if err != nil {
		t.Fatalf("ToQRDB: %v", err)
	}
	if qid1 != qid2 {
		t.Fatalf("qrdb id changed across update: %d -> %d", qid1, qid2)
	}
}

// -----------------------------------------------------------------------------
// PIV / SPKI resolution
// -----------------------------------------------------------------------------

const (
	testSPKIThumbprint   = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA" // 43 chars, base64url-ish
	testLegacyThumbprint = "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef0"
)

func TestQRDBUserStorage_PIVSPKIResolves(t *testing.T) {
	svc := newFakeUserServicer()
	store, _ := newTestUserStorage(t, svc, "acme")
	ctx := context.Background()

	u := &iam.User{
		ID: "uuid-1", TenantID: "acme", Name: "alice", Email: "alice@acme.test",
		CertBindings: []qrdbsdk.CertBinding{
			{Thumbprint: testSPKIThumbprint, Kind: qrdbsdk.CertBindingPIVAttested},
			{Thumbprint: testLegacyThumbprint, Kind: qrdbsdk.CertBindingPIVAttested},
		},
	}
	if err := store.CreateUser(ctx, u); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	tctx := iam.ContextWithTenant(ctx, "acme")
	got, err := store.GetUserByPIVThumbprint(tctx, testSPKIThumbprint)
	if err != nil || got.ID != "uuid-1" {
		t.Fatalf("SPKI lookup failed: %v / %+v", err, got)
	}
}

func TestQRDBUserStorage_PIVLegacyFallbackResolves(t *testing.T) {
	svc := newFakeUserServicer()
	store, _ := newTestUserStorage(t, svc, "acme")
	ctx := context.Background()

	// Legacy-only enrollment: no SPKI binding, only the hex digest binding. The
	// SPKI index will miss; the broker-side legacy index must resolve it.
	u := &iam.User{
		ID: "uuid-1", TenantID: "acme", Name: "alice", Email: "alice@acme.test",
		CertBindings: []qrdbsdk.CertBinding{
			{Thumbprint: testLegacyThumbprint, Kind: qrdbsdk.CertBindingPIVAttested},
		},
	}
	if err := store.CreateUser(ctx, u); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	tctx := iam.ContextWithTenant(ctx, "acme")
	got, err := store.GetUserByPIVThumbprint(tctx, testLegacyThumbprint)
	if err != nil || got.ID != "uuid-1" {
		t.Fatalf("legacy fallback lookup failed: %v / %+v", err, got)
	}
}

func TestQRDBUserStorage_PIVUnknownFailsClosed(t *testing.T) {
	svc := newFakeUserServicer()
	store, _ := newTestUserStorage(t, svc, "acme")
	ctx := iam.ContextWithTenant(context.Background(), "acme")

	if _, err := store.GetUserByPIVThumbprint(ctx, "no-such-thumbprint"); !errors.Is(err, iam.ErrUserNotFound) {
		t.Fatalf("expected ErrUserNotFound for unknown thumbprint, got %v", err)
	}
}

func TestQRDBUserStorage_PIVRenewalResolvesViaSPKI(t *testing.T) {
	svc := newFakeUserServicer()
	store, _ := newTestUserStorage(t, svc, "acme")
	ctx := context.Background()

	u := &iam.User{
		ID: "uuid-1", TenantID: "acme", Name: "alice", Email: "alice@acme.test",
		CertBindings: []qrdbsdk.CertBinding{
			{Thumbprint: testSPKIThumbprint, Kind: qrdbsdk.CertBindingPIVAttested},
			{Thumbprint: testLegacyThumbprint, Kind: qrdbsdk.CertBindingPIVAttested},
		},
	}
	if err := store.CreateUser(ctx, u); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	// Renewal: same key (SPKI unchanged), new cert DER (legacy digest changes).
	const newLegacy = "0fedcba9876543210fedcba9876543210fedcba9876543210fedcba987654321"
	u.SetPIVThumbprints(testSPKIThumbprint, newLegacy)
	if err := store.UpdateUser(ctx, u); err != nil {
		t.Fatalf("UpdateUser (renewal): %v", err)
	}

	tctx := iam.ContextWithTenant(ctx, "acme")
	got, err := store.GetUserByPIVThumbprint(tctx, testSPKIThumbprint)
	if err != nil || got.ID != "uuid-1" {
		t.Fatalf("SPKI lookup after renewal failed: %v / %+v", err, got)
	}
	// The old legacy digest must no longer resolve (index reconciled).
	if _, err := store.GetUserByPIVThumbprint(tctx, testLegacyThumbprint); !errors.Is(err, iam.ErrUserNotFound) {
		t.Fatalf("stale legacy thumbprint should not resolve, got %v", err)
	}
	// The new legacy digest resolves via the broker-side index.
	if got, err := store.GetUserByPIVThumbprint(tctx, newLegacy); err != nil || got.ID != "uuid-1" {
		t.Fatalf("new legacy thumbprint lookup failed: %v / %+v", err, got)
	}
}

// -----------------------------------------------------------------------------
// Fail-closed: nil servicer
// -----------------------------------------------------------------------------

func TestQRDBUserStorage_NilServicerFailsClosed(t *testing.T) {
	kv := newRoutingTestKV()
	store := NewQRDBUserStorage(
		&fakeResolver{svc: nil},
		iam.NewBackendBrokerProfileStore(kv),
		NewBackendIDMap(kv),
		kv,
	)
	ctx := context.Background()
	u := &iam.User{ID: "uuid-1", TenantID: "acme", Name: "alice", Email: "alice@acme.test"}
	if err := store.CreateUser(ctx, u); err == nil {
		t.Fatal("expected CreateUser to fail closed with nil servicer")
	}
	if _, err := store.GetUser(ctx, "acme", "uuid-1"); err == nil {
		t.Fatal("expected GetUser to fail closed with nil servicer")
	}
}

func TestQRDBUserStorage_NilResolverFailsClosed(t *testing.T) {
	store := NewQRDBUserStorage(nil, nil, nil, nil)
	if _, err := store.servicer(context.Background()); err == nil {
		t.Fatal("expected nil resolver to fail closed")
	}
}
