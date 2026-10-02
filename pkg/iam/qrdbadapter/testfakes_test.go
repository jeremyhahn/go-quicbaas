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
	"strings"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// -----------------------------------------------------------------------------
// Routing KVStore test double — mirrors pkg/iam's tenant-routing test double:
// it routes by the context tenant, isolates barrier tenants, and fails closed
// for a sealed tenant (NEVER falling back to the shared store).
// -----------------------------------------------------------------------------

type sealedError struct{}

func (sealedError) Error() string { return "tenant sealed" }

var errTenantSealedTest = sealedError{}

type memStore struct{ data map[string][]byte }

func newMemStore() *memStore { return &memStore{data: make(map[string][]byte)} }

func (m *memStore) Put(_ context.Context, key string, value []byte) error {
	cp := make([]byte, len(value))
	copy(cp, value)
	m.data[key] = cp
	return nil
}

func (m *memStore) Get(_ context.Context, key string) ([]byte, error) {
	v, ok := m.data[key]
	if !ok {
		return nil, &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "memstore.get"}
	}
	cp := make([]byte, len(v))
	copy(cp, v)
	return cp, nil
}

func (m *memStore) Delete(_ context.Context, key string) error { delete(m.data, key); return nil }

func (m *memStore) Scan(_ context.Context, prefix string, fn func(key string, value []byte) error) error {
	for k, v := range m.data {
		if strings.HasPrefix(k, prefix) {
			if err := fn(k, v); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *memStore) List(_ context.Context, prefix string) ([]string, error) {
	var keys []string
	for k := range m.data {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	return keys, nil
}

func (m *memStore) Exists(_ context.Context, key string) (bool, error) {
	_, ok := m.data[key]
	return ok, nil
}

func (m *memStore) keyCount() int { return len(m.data) }

type routingTestKV struct {
	shared *memStore
	stores map[string]*memStore
	sealed map[string]bool
}

func newRoutingTestKV() *routingTestKV {
	return &routingTestKV{shared: newMemStore(), stores: make(map[string]*memStore), sealed: make(map[string]bool)}
}

func (r *routingTestKV) barrierTenant(tenantID string) *memStore {
	st := newMemStore()
	r.stores[tenantID] = st
	return st
}

func (r *routingTestKV) sealTenant(tenantID string) { r.sealed[tenantID] = true }

func (r *routingTestKV) resolve(ctx context.Context) (*memStore, error) {
	tenantID := boundTenant(ctx)
	if r.sealed[tenantID] {
		return nil, errTenantSealedTest
	}
	if tenantID == "" {
		return r.shared, nil
	}
	if st, ok := r.stores[tenantID]; ok {
		return st, nil
	}
	return r.shared, nil
}

func (r *routingTestKV) Put(ctx context.Context, key string, value []byte) error {
	st, err := r.resolve(ctx)
	if err != nil {
		return err
	}
	return st.Put(ctx, key, value)
}

func (r *routingTestKV) Get(ctx context.Context, key string) ([]byte, error) {
	st, err := r.resolve(ctx)
	if err != nil {
		return nil, err
	}
	return st.Get(ctx, key)
}

func (r *routingTestKV) Delete(ctx context.Context, key string) error {
	st, err := r.resolve(ctx)
	if err != nil {
		return err
	}
	return st.Delete(ctx, key)
}

func (r *routingTestKV) Scan(ctx context.Context, prefix string, fn func(key string, value []byte) error) error {
	st, err := r.resolve(ctx)
	if err != nil {
		return err
	}
	return st.Scan(ctx, prefix, fn)
}

func (r *routingTestKV) List(ctx context.Context, prefix string) ([]string, error) {
	st, err := r.resolve(ctx)
	if err != nil {
		return nil, err
	}
	return st.List(ctx, prefix)
}

func (r *routingTestKV) Exists(ctx context.Context, key string) (bool, error) {
	st, err := r.resolve(ctx)
	if err != nil {
		return false, err
	}
	return st.Exists(ctx, key)
}

var _ interfaces.KVStore = (*routingTestKV)(nil)

// -----------------------------------------------------------------------------
// Fake qrdb UserServicer — an in-memory stand-in for the canonical user store.
// It mirrors the semantics the adapter depends on: the store assigns the
// Snowflake, Email is tenant-unique and required unless the caller declares
// WithoutEmailLoginKey, Lifecycle moves are checked and Enabled is DERIVED from
// the result rather than written.
// -----------------------------------------------------------------------------

type fakeUserServicer struct {
	users  map[uint64]*qrdbsdk.User
	nextID uint64
	// forceErr, when set, is returned by every method (drives error-path tests).
	forceErr error
	// lastUpdate records the most recent UpdateUser request so a test can assert
	// what the adapter actually sent, not merely what it stored afterwards.
	lastUpdate *qrdbsdk.UpdateUserRequest
}

func newFakeUserServicer() *fakeUserServicer {
	return &fakeUserServicer{users: make(map[uint64]*qrdbsdk.User), nextID: 1000}
}

// seed inserts a record directly, bypassing create-time validation, so a test
// can stand up pre-existing data.
func (f *fakeUserServicer) seed(u *qrdbsdk.User) *qrdbsdk.User {
	if u.ID == 0 {
		f.nextID++
		u.ID = f.nextID
	}
	if u.Lifecycle == "" {
		u.Lifecycle = qrdbsdk.LifecycleActive
	}
	f.users[u.ID] = u
	return cloneUser(u)
}

func (f *fakeUserServicer) CreateUser(_ context.Context, req *qrdbsdk.CreateUserRequest) (*qrdbsdk.User, error) {
	if f.forceErr != nil {
		return nil, f.forceErr
	}
	if req.Email == "" && !req.WithoutEmailLoginKey {
		return nil, &qrdbsdk.UserValidationError{Field: "email", Reason: "must not be empty; set WithoutEmailLoginKey to create without one"}
	}
	for _, u := range f.users {
		if u.TenantID != req.TenantID {
			continue
		}
		if u.Username == req.Username {
			return nil, &qrdbsdk.UsernameExistsError{Username: req.Username, TenantID: req.TenantID}
		}
		if req.Email != "" && u.Email == req.Email {
			return nil, &qrdbsdk.UsernameExistsError{Username: req.Email, TenantID: req.TenantID}
		}
	}
	lifecycle := req.Lifecycle
	if lifecycle == "" {
		lifecycle = qrdbsdk.LifecycleActive
	}
	f.nextID++
	u := &qrdbsdk.User{
		ID:          f.nextID,
		Username:    req.Username,
		Email:       req.Email,
		Name:        req.Name,
		TenantID:    req.TenantID,
		RoleIDs:     append([]string(nil), req.RoleIDs...),
		AuthMethods: append([]string(nil), req.AuthMethods...),
		Lifecycle:   lifecycle,
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

func (f *fakeUserServicer) GetUserByEmail(_ context.Context, tenantID, email string) (*qrdbsdk.User, error) {
	if f.forceErr != nil {
		return nil, f.forceErr
	}
	for _, u := range f.users {
		if u.TenantID == tenantID && u.Email != "" && u.Email == email {
			return cloneUser(u), nil
		}
	}
	return nil, &qrdbsdk.UserNotFoundError{Lookup: tenantID + "/" + email}
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

func (f *fakeUserServicer) RemoveCertBinding(_ context.Context, userID uint64, thumbprint string) (*qrdbsdk.User, error) {
	u, ok := f.users[userID]
	if !ok {
		return nil, &qrdbsdk.UserNotFoundError{ID: userID}
	}
	kept := u.CertBindings[:0]
	for _, b := range u.CertBindings {
		if b.Thumbprint != thumbprint {
			kept = append(kept, b)
		}
	}
	u.CertBindings = kept
	return cloneUser(u), nil
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
	f.lastUpdate = req
	if f.forceErr != nil {
		return nil, f.forceErr
	}
	u, ok := f.users[req.ID]
	if !ok {
		return nil, &qrdbsdk.UserNotFoundError{ID: req.ID}
	}
	// A terminal record refuses any update that would CHANGE it; only a
	// re-assertion of the state it already holds is permitted, which is what
	// makes a retried deletion resumable. Modelled here because a marker that
	// carries the rest of the user passes a naive double and is rejected by the
	// real store — the exact shape of bug a test double is supposed to catch.
	if u.Lifecycle == qrdbsdk.LifecycleDeleting && !onlyReassertsLifecycle(req) {
		return nil, &qrdbsdk.LifecycleTransitionError{ID: u.ID, From: u.Lifecycle, To: req.Lifecycle}
	}
	if req.Username != "" {
		u.Username = req.Username
	}
	if req.Email != "" {
		u.Email = req.Email
	}
	if req.Name != "" {
		u.Name = req.Name
	}
	if req.RoleIDs != nil {
		u.RoleIDs = append([]string(nil), req.RoleIDs...)
	}
	if req.AuthMethods != nil {
		u.AuthMethods = append([]string(nil), req.AuthMethods...)
	}
	if req.Attributes != nil {
		u.Attributes = req.Attributes
	}
	if req.CertBindings != nil {
		u.CertBindings = append([]qrdbsdk.CertBinding(nil), req.CertBindings...)
	}
	// LastAuth is a high-water mark: a value at or before the stored one writes
	// nothing, so a delayed login cannot roll the account's last-seen backwards.
	if req.LastAuth != nil && (u.LastAuth == nil || req.LastAuth.After(*u.LastAuth)) {
		t := *req.LastAuth
		u.LastAuth = &t
	}
	if req.Lifecycle != "" {
		u.Lifecycle = req.Lifecycle
	}
	return cloneUser(u), nil
}

// DeleteUser claims the record as its first act, exactly as the real store's
// markDeleting does, then removes it.
func (f *fakeUserServicer) DeleteUser(_ context.Context, id uint64) error {
	if f.forceErr != nil {
		return f.forceErr
	}
	u, ok := f.users[id]
	if !ok {
		return &qrdbsdk.UserNotFoundError{ID: id}
	}
	u.Lifecycle = qrdbsdk.LifecycleDeleting
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

// onlyReassertsLifecycle mirrors the store's rule: a request that asks for
// nothing except the lifecycle state.
func onlyReassertsLifecycle(req *qrdbsdk.UpdateUserRequest) bool {
	return req.Lifecycle != "" &&
		req.Username == "" &&
		req.Email == "" &&
		req.Name == "" &&
		req.LastAuth == nil &&
		req.RoleIDs == nil &&
		req.RoleBindings == nil &&
		req.Attributes == nil &&
		req.AuthMethods == nil
}

func cloneUser(u *qrdbsdk.User) *qrdbsdk.User {
	cp := *u
	cp.RoleIDs = append([]string(nil), u.RoleIDs...)
	cp.AuthMethods = append([]string(nil), u.AuthMethods...)
	cp.CertBindings = append([]qrdbsdk.CertBinding(nil), u.CertBindings...)
	if u.Attributes != nil {
		cp.Attributes = make(map[string]string, len(u.Attributes))
		for k, v := range u.Attributes {
			cp.Attributes[k] = v
		}
	}
	return &cp
}

// fakeResolver returns a fixed servicer (or nil + error) for the fail-closed
// wiring tests.
type fakeResolver struct {
	svc qrdbsdk.UserServicer
	err error
}

func (r *fakeResolver) UserServicer(_ context.Context) (qrdbsdk.UserServicer, error) {
	return r.svc, r.err
}

// newTestUserStorage wires a QRDBUserStorage over the in-memory fakes, giving
// each named tenant its own barrier store so routing is exercised rather than
// assumed.
func newTestUserStorage(t *testing.T, svc qrdbsdk.UserServicer, tenants ...string) (*QRDBUserStorage, *routingTestKV) {
	t.Helper()
	kv := newRoutingTestKV()
	for _, tn := range tenants {
		kv.barrierTenant(tn)
	}
	return NewQRDBUserStorage(&fakeResolver{svc: svc}, kv), kv
}
