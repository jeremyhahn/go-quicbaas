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
	"cmp"
	"context"
	"slices"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// -----------------------------------------------------------------------------
// Fake qrdb UserServicer — an in-memory stand-in for the canonical user store.
// It mirrors the semantics the adapter depends on: the store assigns the
// Snowflake, Email is tenant-unique and required unless the caller declares
// WithoutEmailLoginKey, Lifecycle moves are checked and Enabled is DERIVED from
// the result rather than written.
// -----------------------------------------------------------------------------

// fakeDefaultPageSize mirrors the qrdb user store's page size for a request
// that names none.
const fakeDefaultPageSize = 100

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

// ListUsers pages the tenant's users the way the qrdb store does: in a stable
// order, by 1-based Page of PageSize users, PageSize defaulting to
// fakeDefaultPageSize. It honours ctx so a caller that loops on it ends at its
// deadline instead of hanging the test.
func (f *fakeUserServicer) ListUsers(ctx context.Context, req *qrdbsdk.ListUsersRequest) (*qrdbsdk.ListUsersResponse, error) {
	if f.forceErr != nil {
		return nil, f.forceErr
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	all := make([]*qrdbsdk.User, 0)
	for _, u := range f.users {
		if u.TenantID == req.TenantID {
			all = append(all, cloneUser(u))
		}
	}
	slices.SortFunc(all, func(a, b *qrdbsdk.User) int { return cmp.Compare(a.ID, b.ID) })

	page, size := max(req.Page, 1), req.PageSize
	if size <= 0 {
		size = fakeDefaultPageSize
	}
	start := min((page-1)*size, len(all))
	end := min(start+size, len(all))
	return &qrdbsdk.ListUsersResponse{Users: all[start:end], Total: len(all), HasMore: end < len(all)}, nil
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

// newTestUserStorage wires a QRDBUserStorage over the in-memory fake servicer.
func newTestUserStorage(t *testing.T, svc qrdbsdk.UserServicer) *QRDBUserStorage {
	t.Helper()
	return NewQRDBUserStorage(&fakeResolver{svc: svc})
}
