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

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// =============================================================================
// Error-path coverage for QRDBUserStorage and convert.go pure helpers. Every
// test below drives a specific failure branch (resolver, qrdb verb, profile
// store, KV index read/write, malformed index value, paging edge) so the
// composition's fail-closed contract is regression-locked.
// =============================================================================

// -----------------------------------------------------------------------------
// Error-injecting doubles
// -----------------------------------------------------------------------------

var errProfileInjected = errors.New("injected profile failure")

// stubProfileStore lets each verb (Save/Get/Delete) be made to fail or return a
// canned profile/not-found independently, so the storage's profile-store error
// branches are reachable without real I/O.
type stubProfileStore struct {
	saveErr   error
	getErr    error
	deleteErr error
	getResult *iam.BrokerProfile
	// getNotFound, when true, returns iam.ErrBrokerProfileNotFound from GetProfile
	// (the "no side-record" path that profile() must translate to a nil profile).
	getNotFound bool
}

func (s *stubProfileStore) SaveProfile(_ context.Context, _ *iam.BrokerProfile) error {
	return s.saveErr
}

func (s *stubProfileStore) GetProfile(_ context.Context, _ string, _ uint64) (*iam.BrokerProfile, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	if s.getNotFound {
		return nil, iam.ErrBrokerProfileNotFound
	}
	return s.getResult, nil
}

func (s *stubProfileStore) DeleteProfile(_ context.Context, _ string, _ uint64) error {
	return s.deleteErr
}

var _ iam.BrokerProfileStore = (*stubProfileStore)(nil)

// keyFaultKV wraps routingTestKV and injects a failure on Get/Put/Delete for any
// key whose substring matches a configured marker. This targets the broker-index
// read/write branches precisely (email vs legacy-PIV) without coupling to call
// ordering.
type keyFaultKV struct {
	*routingTestKV
	getFailSub    string
	putFailSub    string
	deleteFailSub string
}

func newKeyFaultKV(tenants ...string) *keyFaultKV {
	base := newRoutingTestKV()
	for _, tn := range tenants {
		base.barrierTenant(tn)
	}
	return &keyFaultKV{routingTestKV: base}
}

func contains(s, sub string) bool {
	if sub == "" {
		return false
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func (f *keyFaultKV) Get(ctx context.Context, key string) ([]byte, error) {
	if contains(key, f.getFailSub) {
		return nil, errInjected
	}
	return f.routingTestKV.Get(ctx, key)
}

func (f *keyFaultKV) Put(ctx context.Context, key string, value []byte) error {
	if contains(key, f.putFailSub) {
		return errInjected
	}
	return f.routingTestKV.Put(ctx, key, value)
}

func (f *keyFaultKV) Delete(ctx context.Context, key string) error {
	if contains(key, f.deleteFailSub) {
		return errInjected
	}
	return f.routingTestKV.Delete(ctx, key)
}

var _ interfaces.KVStore = (*keyFaultKV)(nil)

// newCustomUserStorage wires a QRDBUserStorage over caller-supplied collaborators
// so an individual error branch can be isolated.
func newCustomUserStorage(svc qrdbsdk.UserServicer, profiles iam.BrokerProfileStore, idmap IDMap, kv interfaces.KVStore) *QRDBUserStorage {
	return NewQRDBUserStorage(&fakeResolver{svc: svc}, profiles, idmap, kv)
}

// -----------------------------------------------------------------------------
// convert.go: isLegacyHexThumbprint truth table
// -----------------------------------------------------------------------------

func TestIsLegacyHexThumbprint(t *testing.T) {
	const valid = "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef" // 64 lowercase hex chars
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"valid 64 lowercase hex", valid, true},
		{"too short", "deadbeef", false},
		{"too long", valid + "0", false},
		{"uppercase hex rejected (64 chars)", "D" + valid[1:], false},
		{"non-hex char g (64 chars)", "g" + valid[1:], false},
		{"base64url spki (43 chars)", testSPKIThumbprint, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isLegacyHexThumbprint(tc.in); got != tc.want {
				t.Fatalf("isLegacyHexThumbprint(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// mapQRDBUserError: every typed mapping branch
// -----------------------------------------------------------------------------

func TestMapQRDBUserError(t *testing.T) {
	t.Run("nil passes through", func(t *testing.T) {
		if err := mapQRDBUserError("op", nil); err != nil {
			t.Fatalf("nil input = %v, want nil", err)
		}
	})
	t.Run("user-not-found maps to ErrUserNotFound", func(t *testing.T) {
		in := &qrdbsdk.UserNotFoundError{ID: 7}
		if err := mapQRDBUserError("get", in); !errors.Is(err, iam.ErrUserNotFound) {
			t.Fatalf("not-found mapping = %v, want ErrUserNotFound", err)
		}
	})
	t.Run("username-exists maps to ErrAlreadyExists", func(t *testing.T) {
		in := &qrdbsdk.UsernameExistsError{Username: "alice", TenantID: "acme"}
		if err := mapQRDBUserError("create", in); !errors.Is(err, iam.ErrAlreadyExists) {
			t.Fatalf("exists mapping = %v, want ErrAlreadyExists", err)
		}
	})
	t.Run("unknown error is wrapped not swallowed", func(t *testing.T) {
		err := mapQRDBUserError("list", errInjected)
		if err == nil {
			t.Fatal("unknown error must not be swallowed")
		}
		if !errors.Is(err, errInjected) {
			t.Fatalf("wrapped error lost its cause: %v", err)
		}
		var qb *dberrors.QBError
		if !errors.As(err, &qb) || qb.Op != "iam.user.qrdb.list" {
			t.Fatalf("expected QBError with op iam.user.qrdb.list, got %v", err)
		}
	})
}

// -----------------------------------------------------------------------------
// servicer(): resolver failure path
// -----------------------------------------------------------------------------

func TestQRDBUserStorage_ResolverError(t *testing.T) {
	store := NewQRDBUserStorage(&fakeResolver{err: errInjected}, &stubProfileStore{}, NewBackendIDMap(newRoutingTestKV()), newRoutingTestKV())
	if _, err := store.servicer(context.Background()); !errors.Is(err, errInjected) {
		t.Fatalf("resolver error = %v, want injected failure", err)
	}
}

// -----------------------------------------------------------------------------
// CreateUser error branches
// -----------------------------------------------------------------------------

func TestQRDBUserStorage_CreateUser_CoreError(t *testing.T) {
	svc := newFakeUserServicer()
	svc.forceErr = errInjected
	store, _ := newTestUserStorage(t, svc, "acme")
	u := &iam.User{ID: "u1", TenantID: "acme", Name: "alice"}
	if err := store.CreateUser(context.Background(), u); !errors.Is(err, errInjected) {
		t.Fatalf("CreateUser core error = %v, want injected failure", err)
	}
}

func TestQRDBUserStorage_CreateUser_DuplicateMapsToExists(t *testing.T) {
	svc := newFakeUserServicer()
	store, _ := newTestUserStorage(t, svc, "acme")
	ctx := context.Background()
	first := &iam.User{ID: "u1", TenantID: "acme", Name: "alice", Email: "a@acme.test"}
	if err := store.CreateUser(ctx, first); err != nil {
		t.Fatalf("first CreateUser: %v", err)
	}
	dup := &iam.User{ID: "u2", TenantID: "acme", Name: "alice", Email: "b@acme.test"}
	if err := store.CreateUser(ctx, dup); !errors.Is(err, iam.ErrAlreadyExists) {
		t.Fatalf("duplicate CreateUser = %v, want ErrAlreadyExists", err)
	}
}

// errOnUpdateServicer succeeds on CreateUser but fails the follow-up UpdateUser
// that applies attributes/cert-bindings, covering CreateUser's create.update
// error branch.
type errOnUpdateServicer struct {
	*fakeUserServicer
}

func (s *errOnUpdateServicer) UpdateUser(_ context.Context, _ *qrdbsdk.UpdateUserRequest) (*qrdbsdk.User, error) {
	return nil, errInjected
}

func TestQRDBUserStorage_CreateUser_FollowUpUpdateError(t *testing.T) {
	svc := &errOnUpdateServicer{fakeUserServicer: newFakeUserServicer()}
	store, _ := newTestUserStorage(t, svc, "acme")
	// Attributes force the follow-up UpdateUser call.
	u := &iam.User{ID: "u1", TenantID: "acme", Name: "alice", Attributes: map[string]interface{}{"dept": "eng"}}
	if err := store.CreateUser(context.Background(), u); !errors.Is(err, errInjected) {
		t.Fatalf("CreateUser follow-up update error = %v, want injected failure", err)
	}
}

func TestQRDBUserStorage_CreateUser_ProfileSaveError(t *testing.T) {
	svc := newFakeUserServicer()
	kv := newRoutingTestKV()
	kv.barrierTenant("acme")
	store := newCustomUserStorage(svc, &stubProfileStore{saveErr: errProfileInjected}, NewBackendIDMap(kv), kv)
	u := &iam.User{ID: "u1", TenantID: "acme", Name: "alice"}
	if err := store.CreateUser(context.Background(), u); !errors.Is(err, errProfileInjected) {
		t.Fatalf("CreateUser profile-save error = %v, want injected profile failure", err)
	}
}

func TestQRDBUserStorage_CreateUser_IDMapLinkError(t *testing.T) {
	svc := newFakeUserServicer()
	// Pre-link the same UUID to a different qrdb id so Link fails with a conflict.
	kv := newRoutingTestKV()
	kv.barrierTenant("acme")
	idmap := NewBackendIDMap(kv)
	if err := idmap.Link(context.Background(), "acme", "u1", 555); err != nil {
		t.Fatalf("seed Link: %v", err)
	}
	store := newCustomUserStorage(svc, &stubProfileStore{}, idmap, kv)
	u := &iam.User{ID: "u1", TenantID: "acme", Name: "alice"}
	if err := store.CreateUser(context.Background(), u); !errors.Is(err, ErrIDMapConflict) {
		t.Fatalf("CreateUser idmap-link error = %v, want ErrIDMapConflict", err)
	}
}

func TestQRDBUserStorage_CreateUser_IndexWriteError(t *testing.T) {
	svc := newFakeUserServicer()
	kv := newKeyFaultKV("acme")
	kv.putFailSub = "qrdbuser:email:"
	store := newCustomUserStorage(svc, &stubProfileStore{}, NewBackendIDMap(kv.routingTestKV), kv)
	u := &iam.User{ID: "u1", TenantID: "acme", Name: "alice", Email: "a@acme.test"}
	if err := store.CreateUser(context.Background(), u); !errors.Is(err, errInjected) {
		t.Fatalf("CreateUser index-write error = %v, want injected failure", err)
	}
}

// -----------------------------------------------------------------------------
// GetUser error branches
// -----------------------------------------------------------------------------

func TestQRDBUserStorage_GetUser_IDMapNotFound(t *testing.T) {
	svc := newFakeUserServicer()
	store, _ := newTestUserStorage(t, svc, "acme")
	if _, err := store.GetUser(context.Background(), "acme", "ghost"); !errors.Is(err, iam.ErrUserNotFound) {
		t.Fatalf("GetUser unknown id = %v, want ErrUserNotFound", err)
	}
}

func TestQRDBUserStorage_GetUser_IDMapError(t *testing.T) {
	svc := newFakeUserServicer()
	base := newRoutingTestKV()
	base.barrierTenant("acme")
	kv := &failGetKV{routingTestKV: base}
	store := newCustomUserStorage(svc, &stubProfileStore{}, NewBackendIDMap(kv), kv)
	if _, err := store.GetUser(context.Background(), "acme", "u1"); !errors.Is(err, errInjected) {
		t.Fatalf("GetUser idmap error = %v, want injected failure", err)
	}
}

func TestQRDBUserStorage_GetUser_ProfileError(t *testing.T) {
	svc := newFakeUserServicer()
	kv := newRoutingTestKV()
	kv.barrierTenant("acme")
	idmap := NewBackendIDMap(kv)
	if err := idmap.Link(context.Background(), "acme", "u1", 1001); err != nil {
		t.Fatalf("seed Link: %v", err)
	}
	if _, err := svc.CreateUser(context.Background(), &qrdbsdk.CreateUserRequest{Username: "alice", TenantID: "acme"}); err != nil {
		t.Fatalf("seed core: %v", err)
	}
	// Point the idmap at the actual qrdb id the fake just minted.
	store := newCustomUserStorage(svc, &stubProfileStore{getErr: errProfileInjected}, idmap, kv)
	// Re-link to the real minted id (1001 -> minted). The fake mints nextID++ from
	// 1000, so the first user is 1001, matching the seed Link above.
	if _, err := store.GetUser(context.Background(), "acme", "u1"); !errors.Is(err, errProfileInjected) {
		t.Fatalf("GetUser profile error = %v, want injected profile failure", err)
	}
}

// -----------------------------------------------------------------------------
// GetUserByPIVThumbprint error branches
// -----------------------------------------------------------------------------

// errSPKIServicer returns a non-not-found error from UserByCertThumbprint so the
// SPKI branch propagates a mapped internal error rather than falling through to
// the legacy index.
type errSPKIServicer struct {
	*fakeUserServicer
}

func (s *errSPKIServicer) UserByCertThumbprint(_ context.Context, _ string) (*qrdbsdk.User, error) {
	return nil, errInjected
}

func TestQRDBUserStorage_PIV_SPKIInternalError(t *testing.T) {
	svc := &errSPKIServicer{fakeUserServicer: newFakeUserServicer()}
	store, _ := newTestUserStorage(t, svc, "acme")
	ctx := iam.ContextWithTenant(context.Background(), "acme")
	if _, err := store.GetUserByPIVThumbprint(ctx, testSPKIThumbprint); !errors.Is(err, errInjected) {
		t.Fatalf("PIV SPKI internal error = %v, want injected failure", err)
	}
}

func TestQRDBUserStorage_PIV_ServicerResolverError(t *testing.T) {
	store := NewQRDBUserStorage(&fakeResolver{err: errInjected}, &stubProfileStore{}, NewBackendIDMap(newRoutingTestKV()), newRoutingTestKV())
	if _, err := store.GetUserByPIVThumbprint(context.Background(), testSPKIThumbprint); !errors.Is(err, errInjected) {
		t.Fatalf("PIV resolver error = %v, want injected failure", err)
	}
}

func TestQRDBUserStorage_PIV_SPKIHit_IDMapMissing(t *testing.T) {
	// The qrdb core resolves by SPKI, but no IDMap entry exists (the user was
	// never persisted through this store), so ToBroker reports not-found.
	svc := newFakeUserServicer()
	if _, err := svc.CreateUser(context.Background(), &qrdbsdk.CreateUserRequest{Username: "alice", TenantID: "acme"}); err != nil {
		t.Fatalf("seed core: %v", err)
	}
	for _, u := range svc.users {
		u.CertBindings = []qrdbsdk.CertBinding{{Thumbprint: testSPKIThumbprint, Kind: qrdbsdk.CertBindingSPKIThumbprint}}
	}
	store, _ := newTestUserStorage(t, svc, "acme")
	ctx := iam.ContextWithTenant(context.Background(), "acme")
	if _, err := store.GetUserByPIVThumbprint(ctx, testSPKIThumbprint); !errors.Is(err, ErrIDMapNotFound) {
		t.Fatalf("PIV SPKI hit without idmap = %v, want ErrIDMapNotFound", err)
	}
}

func TestQRDBUserStorage_PIV_SPKIHit_ProfileError(t *testing.T) {
	svc := newFakeUserServicer()
	kv := newRoutingTestKV()
	kv.barrierTenant("acme")
	idmap := NewBackendIDMap(kv)
	created, err := svc.CreateUser(context.Background(), &qrdbsdk.CreateUserRequest{Username: "alice", TenantID: "acme"})
	if err != nil {
		t.Fatalf("seed core: %v", err)
	}
	for _, u := range svc.users {
		u.CertBindings = []qrdbsdk.CertBinding{{Thumbprint: testSPKIThumbprint, Kind: qrdbsdk.CertBindingSPKIThumbprint}}
	}
	if err := idmap.Link(context.Background(), "acme", "u1", created.ID); err != nil {
		t.Fatalf("seed Link: %v", err)
	}
	store := newCustomUserStorage(svc, &stubProfileStore{getErr: errProfileInjected}, idmap, kv)
	ctx := iam.ContextWithTenant(context.Background(), "acme")
	if _, err := store.GetUserByPIVThumbprint(ctx, testSPKIThumbprint); !errors.Is(err, errProfileInjected) {
		t.Fatalf("PIV SPKI hit profile error = %v, want injected profile failure", err)
	}
}

func TestQRDBUserStorage_PIV_LegacyIndexReadError(t *testing.T) {
	// SPKI misses (not-found), and the broker legacy-PIV index read fails.
	svc := newFakeUserServicer()
	kv := newKeyFaultKV("acme")
	kv.getFailSub = "qrdbuser:legacypiv:"
	store := newCustomUserStorage(svc, &stubProfileStore{}, NewBackendIDMap(kv.routingTestKV), kv)
	ctx := iam.ContextWithTenant(context.Background(), "acme")
	if _, err := store.GetUserByPIVThumbprint(ctx, testLegacyThumbprint); !errors.Is(err, errInjected) {
		t.Fatalf("PIV legacy index read error = %v, want injected failure", err)
	}
}

// -----------------------------------------------------------------------------
// GetUserByUsername error branches
// -----------------------------------------------------------------------------

func TestQRDBUserStorage_GetUserByUsername_ResolverError(t *testing.T) {
	store := NewQRDBUserStorage(&fakeResolver{err: errInjected}, &stubProfileStore{}, NewBackendIDMap(newRoutingTestKV()), newRoutingTestKV())
	if _, err := store.GetUserByUsername(context.Background(), "acme", "alice"); !errors.Is(err, errInjected) {
		t.Fatalf("GetUserByUsername resolver error = %v, want injected failure", err)
	}
}

func TestQRDBUserStorage_GetUserByUsername_IDMapMissing(t *testing.T) {
	// qrdb resolves the user by username, but no IDMap entry exists.
	svc := newFakeUserServicer()
	if _, err := svc.CreateUser(context.Background(), &qrdbsdk.CreateUserRequest{Username: "alice", TenantID: "acme"}); err != nil {
		t.Fatalf("seed core: %v", err)
	}
	store, _ := newTestUserStorage(t, svc, "acme")
	if _, err := store.GetUserByUsername(context.Background(), "acme", "alice"); !errors.Is(err, ErrIDMapNotFound) {
		t.Fatalf("GetUserByUsername without idmap = %v, want ErrIDMapNotFound", err)
	}
}

func TestQRDBUserStorage_GetUserByUsername_ProfileError(t *testing.T) {
	svc := newFakeUserServicer()
	kv := newRoutingTestKV()
	kv.barrierTenant("acme")
	idmap := NewBackendIDMap(kv)
	created, err := svc.CreateUser(context.Background(), &qrdbsdk.CreateUserRequest{Username: "alice", TenantID: "acme"})
	if err != nil {
		t.Fatalf("seed core: %v", err)
	}
	if err := idmap.Link(context.Background(), "acme", "u1", created.ID); err != nil {
		t.Fatalf("seed Link: %v", err)
	}
	store := newCustomUserStorage(svc, &stubProfileStore{getErr: errProfileInjected}, idmap, kv)
	if _, err := store.GetUserByUsername(context.Background(), "acme", "alice"); !errors.Is(err, errProfileInjected) {
		t.Fatalf("GetUserByUsername profile error = %v, want injected profile failure", err)
	}
}

// -----------------------------------------------------------------------------
// UpdateUser error branches
// -----------------------------------------------------------------------------

func TestQRDBUserStorage_UpdateUser_IDMapNotFound(t *testing.T) {
	svc := newFakeUserServicer()
	store, _ := newTestUserStorage(t, svc, "acme")
	u := &iam.User{ID: "ghost", TenantID: "acme", Name: "alice"}
	if err := store.UpdateUser(context.Background(), u); !errors.Is(err, iam.ErrUserNotFound) {
		t.Fatalf("UpdateUser unknown id = %v, want ErrUserNotFound", err)
	}
}

func TestQRDBUserStorage_UpdateUser_IDMapError(t *testing.T) {
	svc := newFakeUserServicer()
	base := newRoutingTestKV()
	base.barrierTenant("acme")
	kv := &failGetKV{routingTestKV: base}
	store := newCustomUserStorage(svc, &stubProfileStore{}, NewBackendIDMap(kv), kv)
	u := &iam.User{ID: "u1", TenantID: "acme", Name: "alice"}
	if err := store.UpdateUser(context.Background(), u); !errors.Is(err, errInjected) {
		t.Fatalf("UpdateUser idmap error = %v, want injected failure", err)
	}
}

func TestQRDBUserStorage_UpdateUser_PriorProfileError(t *testing.T) {
	svc := newFakeUserServicer()
	kv := newRoutingTestKV()
	kv.barrierTenant("acme")
	idmap := NewBackendIDMap(kv)
	created, err := svc.CreateUser(context.Background(), &qrdbsdk.CreateUserRequest{Username: "alice", TenantID: "acme"})
	if err != nil {
		t.Fatalf("seed core: %v", err)
	}
	if err := idmap.Link(context.Background(), "acme", "u1", created.ID); err != nil {
		t.Fatalf("seed Link: %v", err)
	}
	// The prior-profile read returns a non-not-found error, which must abort.
	store := newCustomUserStorage(svc, &stubProfileStore{getErr: errProfileInjected}, idmap, kv)
	u := &iam.User{ID: "u1", TenantID: "acme", Name: "alice"}
	if err := store.UpdateUser(context.Background(), u); !errors.Is(err, errProfileInjected) {
		t.Fatalf("UpdateUser prior-profile error = %v, want injected profile failure", err)
	}
}

func TestQRDBUserStorage_UpdateUser_CoreError(t *testing.T) {
	svc := newFakeUserServicer()
	kv := newRoutingTestKV()
	kv.barrierTenant("acme")
	idmap := NewBackendIDMap(kv)
	created, err := svc.CreateUser(context.Background(), &qrdbsdk.CreateUserRequest{Username: "alice", TenantID: "acme"})
	if err != nil {
		t.Fatalf("seed core: %v", err)
	}
	if err := idmap.Link(context.Background(), "acme", "u1", created.ID); err != nil {
		t.Fatalf("seed Link: %v", err)
	}
	svc.forceErr = errInjected
	store := newCustomUserStorage(svc, &stubProfileStore{getNotFound: true}, idmap, kv)
	u := &iam.User{ID: "u1", TenantID: "acme", Name: "alice"}
	if err := store.UpdateUser(context.Background(), u); !errors.Is(err, errInjected) {
		t.Fatalf("UpdateUser core error = %v, want injected failure", err)
	}
}

func TestQRDBUserStorage_UpdateUser_ProfileSaveError(t *testing.T) {
	svc := newFakeUserServicer()
	kv := newRoutingTestKV()
	kv.barrierTenant("acme")
	idmap := NewBackendIDMap(kv)
	created, err := svc.CreateUser(context.Background(), &qrdbsdk.CreateUserRequest{Username: "alice", TenantID: "acme"})
	if err != nil {
		t.Fatalf("seed core: %v", err)
	}
	if err := idmap.Link(context.Background(), "acme", "u1", created.ID); err != nil {
		t.Fatalf("seed Link: %v", err)
	}
	store := newCustomUserStorage(svc, &stubProfileStore{getNotFound: true, saveErr: errProfileInjected}, idmap, kv)
	u := &iam.User{ID: "u1", TenantID: "acme", Name: "alice"}
	if err := store.UpdateUser(context.Background(), u); !errors.Is(err, errProfileInjected) {
		t.Fatalf("UpdateUser profile-save error = %v, want injected profile failure", err)
	}
}

func TestQRDBUserStorage_UpdateUser_ReconcileIndexError(t *testing.T) {
	// A prior profile with a different email forces reconcileIndexes to delete the
	// stale email index; the Delete fails.
	svc := newFakeUserServicer()
	kv := newKeyFaultKV("acme")
	store := newCustomUserStorage(svc, iam.NewBackendBrokerProfileStore(kv.routingTestKV), NewBackendIDMap(kv.routingTestKV), kv)
	ctx := context.Background()
	u := &iam.User{ID: "u1", TenantID: "acme", Name: "alice", Email: "old@acme.test"}
	if err := store.CreateUser(ctx, u); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	// Now arm the stale-email delete to fail, then change the email so
	// reconcileIndexes must delete the prior "old@acme.test" index entry.
	kv.deleteFailSub = "qrdbuser:email:"
	u.Email = "new@acme.test"
	if err := store.UpdateUser(ctx, u); !errors.Is(err, errInjected) {
		t.Fatalf("UpdateUser reconcile-delete error = %v, want injected failure", err)
	}
}

// -----------------------------------------------------------------------------
// DeleteUser error branches
// -----------------------------------------------------------------------------

func TestQRDBUserStorage_DeleteUser_ResolverError(t *testing.T) {
	store := NewQRDBUserStorage(&fakeResolver{err: errInjected}, &stubProfileStore{}, NewBackendIDMap(newRoutingTestKV()), newRoutingTestKV())
	if err := store.DeleteUser(context.Background(), "acme", "u1"); !errors.Is(err, errInjected) {
		t.Fatalf("DeleteUser resolver error = %v, want injected failure", err)
	}
}

func TestQRDBUserStorage_DeleteUser_PriorProfileError(t *testing.T) {
	svc := newFakeUserServicer()
	kv := newRoutingTestKV()
	kv.barrierTenant("acme")
	idmap := NewBackendIDMap(kv)
	created, err := svc.CreateUser(context.Background(), &qrdbsdk.CreateUserRequest{Username: "alice", TenantID: "acme"})
	if err != nil {
		t.Fatalf("seed core: %v", err)
	}
	if err := idmap.Link(context.Background(), "acme", "u1", created.ID); err != nil {
		t.Fatalf("seed Link: %v", err)
	}
	store := newCustomUserStorage(svc, &stubProfileStore{getErr: errProfileInjected}, idmap, kv)
	if err := store.DeleteUser(context.Background(), "acme", "u1"); !errors.Is(err, errProfileInjected) {
		t.Fatalf("DeleteUser prior-profile error = %v, want injected profile failure", err)
	}
}

func TestQRDBUserStorage_DeleteUser_CoreError(t *testing.T) {
	svc := newFakeUserServicer()
	kv := newRoutingTestKV()
	kv.barrierTenant("acme")
	idmap := NewBackendIDMap(kv)
	created, err := svc.CreateUser(context.Background(), &qrdbsdk.CreateUserRequest{Username: "alice", TenantID: "acme"})
	if err != nil {
		t.Fatalf("seed core: %v", err)
	}
	if err := idmap.Link(context.Background(), "acme", "u1", created.ID); err != nil {
		t.Fatalf("seed Link: %v", err)
	}
	svc.forceErr = errInjected
	store := newCustomUserStorage(svc, &stubProfileStore{getNotFound: true}, idmap, kv)
	if err := store.DeleteUser(context.Background(), "acme", "u1"); !errors.Is(err, errInjected) {
		t.Fatalf("DeleteUser core error = %v, want injected failure", err)
	}
}

func TestQRDBUserStorage_DeleteUser_ProfileDeleteError(t *testing.T) {
	svc := newFakeUserServicer()
	kv := newRoutingTestKV()
	kv.barrierTenant("acme")
	idmap := NewBackendIDMap(kv)
	created, err := svc.CreateUser(context.Background(), &qrdbsdk.CreateUserRequest{Username: "alice", TenantID: "acme"})
	if err != nil {
		t.Fatalf("seed core: %v", err)
	}
	if err := idmap.Link(context.Background(), "acme", "u1", created.ID); err != nil {
		t.Fatalf("seed Link: %v", err)
	}
	store := newCustomUserStorage(svc, &stubProfileStore{getNotFound: true, deleteErr: errProfileInjected}, idmap, kv)
	if err := store.DeleteUser(context.Background(), "acme", "u1"); !errors.Is(err, errProfileInjected) {
		t.Fatalf("DeleteUser profile-delete error = %v, want injected profile failure", err)
	}
}

func TestQRDBUserStorage_DeleteUser_IndexDeleteError(t *testing.T) {
	// A prior profile carries an email index that must be deleted; the Delete fails.
	svc := newFakeUserServicer()
	kv := newKeyFaultKV("acme")
	store := newCustomUserStorage(svc, iam.NewBackendBrokerProfileStore(kv.routingTestKV), NewBackendIDMap(kv.routingTestKV), kv)
	ctx := context.Background()
	u := &iam.User{ID: "u1", TenantID: "acme", Name: "alice", Email: "a@acme.test"}
	if err := store.CreateUser(ctx, u); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	kv.deleteFailSub = "qrdbuser:email:"
	if err := store.DeleteUser(ctx, "acme", "u1"); !errors.Is(err, errInjected) {
		t.Fatalf("DeleteUser index-delete error = %v, want injected failure", err)
	}
}

// validLegacyHex is a genuine 64-char lowercase-hex digest so the legacy-PIV
// index is actually written (isLegacyHexThumbprint accepts it), exercising the
// legacy-PIV write/delete branches of writeIndexes/reconcileIndexes/deleteIndexes.
const validLegacyHex = "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"

// writeIndexes: the legacy-PIV global Put fails (second Put in the loop).
func TestQRDBUserStorage_CreateUser_LegacyPIVGlobalIndexWriteError(t *testing.T) {
	svc := newFakeUserServicer()
	kv := newKeyFaultKV("acme")
	kv.putFailSub = "qrdbuser:legacypiv:_global:"
	store := newCustomUserStorage(svc, iam.NewBackendBrokerProfileStore(kv.routingTestKV), NewBackendIDMap(kv.routingTestKV), kv)
	u := &iam.User{
		ID: "u1", TenantID: "acme", Name: "alice",
		CertBindings: []qrdbsdk.CertBinding{{Thumbprint: validLegacyHex, Kind: qrdbsdk.CertBindingPIVAttested}},
	}
	if err := store.CreateUser(context.Background(), u); !errors.Is(err, errInjected) {
		t.Fatalf("CreateUser legacy-PIV global index write error = %v, want injected failure", err)
	}
}

// reconcileIndexes: a stale legacy-PIV thumbprint must be deleted on update; the
// global delete fails.
func TestQRDBUserStorage_UpdateUser_StaleLegacyPIVGlobalDeleteError(t *testing.T) {
	svc := newFakeUserServicer()
	kv := newKeyFaultKV("acme")
	store := newCustomUserStorage(svc, iam.NewBackendBrokerProfileStore(kv.routingTestKV), NewBackendIDMap(kv.routingTestKV), kv)
	ctx := context.Background()
	u := &iam.User{
		ID: "u1", TenantID: "acme", Name: "alice",
		CertBindings: []qrdbsdk.CertBinding{{Thumbprint: validLegacyHex, Kind: qrdbsdk.CertBindingPIVAttested}},
	}
	if err := store.CreateUser(ctx, u); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	// Drop the legacy binding so reconcileIndexes deletes the stale entry, and arm
	// the global-key delete to fail.
	kv.deleteFailSub = "qrdbuser:legacypiv:_global:"
	u.CertBindings = nil
	if err := store.UpdateUser(ctx, u); !errors.Is(err, errInjected) {
		t.Fatalf("UpdateUser stale legacy-PIV global delete error = %v, want injected failure", err)
	}
}

// reconcileIndexes: the stale legacy-PIV tenant-scoped delete fails.
func TestQRDBUserStorage_UpdateUser_StaleLegacyPIVScopedDeleteError(t *testing.T) {
	svc := newFakeUserServicer()
	kv := newKeyFaultKV("acme")
	store := newCustomUserStorage(svc, iam.NewBackendBrokerProfileStore(kv.routingTestKV), NewBackendIDMap(kv.routingTestKV), kv)
	ctx := context.Background()
	u := &iam.User{
		ID: "u1", TenantID: "acme", Name: "alice",
		CertBindings: []qrdbsdk.CertBinding{{Thumbprint: validLegacyHex, Kind: qrdbsdk.CertBindingPIVAttested}},
	}
	if err := store.CreateUser(ctx, u); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	// The tenant-scoped key embeds "legacypiv:acme:"; arm only that one to fail.
	kv.deleteFailSub = "qrdbuser:legacypiv:acme:"
	u.CertBindings = nil
	if err := store.UpdateUser(ctx, u); !errors.Is(err, errInjected) {
		t.Fatalf("UpdateUser stale legacy-PIV scoped delete error = %v, want injected failure", err)
	}
}

// deleteIndexes: the legacy-PIV scoped delete fails on user removal.
func TestQRDBUserStorage_DeleteUser_LegacyPIVScopedDeleteError(t *testing.T) {
	svc := newFakeUserServicer()
	kv := newKeyFaultKV("acme")
	store := newCustomUserStorage(svc, iam.NewBackendBrokerProfileStore(kv.routingTestKV), NewBackendIDMap(kv.routingTestKV), kv)
	ctx := context.Background()
	u := &iam.User{
		ID: "u1", TenantID: "acme", Name: "alice",
		CertBindings: []qrdbsdk.CertBinding{{Thumbprint: validLegacyHex, Kind: qrdbsdk.CertBindingPIVAttested}},
	}
	if err := store.CreateUser(ctx, u); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	kv.deleteFailSub = "qrdbuser:legacypiv:acme:"
	if err := store.DeleteUser(ctx, "acme", "u1"); !errors.Is(err, errInjected) {
		t.Fatalf("DeleteUser legacy-PIV scoped delete error = %v, want injected failure", err)
	}
}

// deleteIndexes: the legacy-PIV global delete fails on user removal.
func TestQRDBUserStorage_DeleteUser_LegacyPIVGlobalDeleteError(t *testing.T) {
	svc := newFakeUserServicer()
	kv := newKeyFaultKV("acme")
	store := newCustomUserStorage(svc, iam.NewBackendBrokerProfileStore(kv.routingTestKV), NewBackendIDMap(kv.routingTestKV), kv)
	ctx := context.Background()
	u := &iam.User{
		ID: "u1", TenantID: "acme", Name: "alice",
		CertBindings: []qrdbsdk.CertBinding{{Thumbprint: validLegacyHex, Kind: qrdbsdk.CertBindingPIVAttested}},
	}
	if err := store.CreateUser(ctx, u); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	kv.deleteFailSub = "qrdbuser:legacypiv:_global:"
	if err := store.DeleteUser(ctx, "acme", "u1"); !errors.Is(err, errInjected) {
		t.Fatalf("DeleteUser legacy-PIV global delete error = %v, want injected failure", err)
	}
}

// legacyPIVThumbprintsFromBindings filters by kind, non-empty, and legacy-hex
// shape; non-qualifying bindings are excluded.
func TestLegacyPIVThumbprintsFromBindings(t *testing.T) {
	in := []qrdbsdk.CertBinding{
		{Thumbprint: validLegacyHex, Kind: qrdbsdk.CertBindingPIVAttested},     // kept
		{Thumbprint: testSPKIThumbprint, Kind: qrdbsdk.CertBindingPIVAttested}, // wrong shape (43 chars)
		{Thumbprint: validLegacyHex, Kind: qrdbsdk.CertBindingSPKIThumbprint},  // wrong kind
		{Thumbprint: "", Kind: qrdbsdk.CertBindingPIVAttested},                 // empty
	}
	out := legacyPIVThumbprintsFromBindings(in)
	if len(out) != 1 || out[0] != validLegacyHex {
		t.Fatalf("legacyPIVThumbprintsFromBindings = %v, want exactly [%s]", out, validLegacyHex)
	}
	if got := legacyPIVThumbprintsFromBindings(nil); len(got) != 0 {
		t.Fatalf("nil bindings = %v, want empty", got)
	}
}

// -----------------------------------------------------------------------------
// ListUsers error branches + paging
// -----------------------------------------------------------------------------

func TestQRDBUserStorage_ListUsers_ResolverError(t *testing.T) {
	store := NewQRDBUserStorage(&fakeResolver{err: errInjected}, &stubProfileStore{}, NewBackendIDMap(newRoutingTestKV()), newRoutingTestKV())
	if _, err := store.ListUsers(context.Background(), "acme", iam.ListOptions{}); !errors.Is(err, errInjected) {
		t.Fatalf("ListUsers resolver error = %v, want injected failure", err)
	}
}

func TestQRDBUserStorage_ListUsers_CoreError(t *testing.T) {
	svc := newFakeUserServicer()
	svc.forceErr = errInjected
	store, _ := newTestUserStorage(t, svc, "acme")
	if _, err := store.ListUsers(context.Background(), "acme", iam.ListOptions{}); !errors.Is(err, errInjected) {
		t.Fatalf("ListUsers core error = %v, want injected failure", err)
	}
}

func TestQRDBUserStorage_ListUsers_SkipsNilAndUnmappedUsers(t *testing.T) {
	// One user has an IDMap entry; one nil entry and one unmapped entry must be
	// skipped rather than failing the whole listing.
	svc := newFakeUserServicer()
	kv := newRoutingTestKV()
	kv.barrierTenant("acme")
	idmap := NewBackendIDMap(kv)
	mapped, err := svc.CreateUser(context.Background(), &qrdbsdk.CreateUserRequest{Username: "alice", TenantID: "acme"})
	if err != nil {
		t.Fatalf("seed mapped: %v", err)
	}
	if _, err := svc.CreateUser(context.Background(), &qrdbsdk.CreateUserRequest{Username: "bob", TenantID: "acme"}); err != nil {
		t.Fatalf("seed unmapped: %v", err)
	}
	// Inject a nil entry into the returned slice via a wrapper servicer.
	if err := idmap.Link(context.Background(), "acme", "u-alice", mapped.ID); err != nil {
		t.Fatalf("seed Link: %v", err)
	}
	store := newCustomUserStorage(&nilEntryListServicer{fakeUserServicer: svc}, iam.NewBackendBrokerProfileStore(kv), idmap, kv)
	users, err := store.ListUsers(context.Background(), "acme", iam.ListOptions{})
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users) != 1 || users[0].ID != "u-alice" {
		t.Fatalf("expected only the mapped user, got %+v", users)
	}
}

// nilEntryListServicer prepends a nil user to the listing to cover the nil-skip
// branch in ListUsers.
type nilEntryListServicer struct {
	*fakeUserServicer
}

func (s *nilEntryListServicer) ListUsers(ctx context.Context, req *qrdbsdk.ListUsersRequest) (*qrdbsdk.ListUsersResponse, error) {
	resp, err := s.fakeUserServicer.ListUsers(ctx, req)
	if err != nil {
		return nil, err
	}
	resp.Users = append([]*qrdbsdk.User{nil}, resp.Users...)
	return resp, nil
}

func TestQRDBUserStorage_ListUsers_IDMapError(t *testing.T) {
	// A non-not-found idmap error must abort the listing.
	svc := newFakeUserServicer()
	if _, err := svc.CreateUser(context.Background(), &qrdbsdk.CreateUserRequest{Username: "alice", TenantID: "acme"}); err != nil {
		t.Fatalf("seed core: %v", err)
	}
	base := newRoutingTestKV()
	base.barrierTenant("acme")
	kv := &failGetKV{routingTestKV: base}
	store := newCustomUserStorage(svc, &stubProfileStore{}, NewBackendIDMap(kv), kv)
	if _, err := store.ListUsers(context.Background(), "acme", iam.ListOptions{}); !errors.Is(err, errInjected) {
		t.Fatalf("ListUsers idmap error = %v, want injected failure", err)
	}
}

func TestQRDBUserStorage_ListUsers_ProfileError(t *testing.T) {
	svc := newFakeUserServicer()
	kv := newRoutingTestKV()
	kv.barrierTenant("acme")
	idmap := NewBackendIDMap(kv)
	created, err := svc.CreateUser(context.Background(), &qrdbsdk.CreateUserRequest{Username: "alice", TenantID: "acme"})
	if err != nil {
		t.Fatalf("seed core: %v", err)
	}
	if err := idmap.Link(context.Background(), "acme", "u1", created.ID); err != nil {
		t.Fatalf("seed Link: %v", err)
	}
	store := newCustomUserStorage(svc, &stubProfileStore{getErr: errProfileInjected}, idmap, kv)
	if _, err := store.ListUsers(context.Background(), "acme", iam.ListOptions{}); !errors.Is(err, errProfileInjected) {
		t.Fatalf("ListUsers profile error = %v, want injected profile failure", err)
	}
}

// -----------------------------------------------------------------------------
// pageFromOffset edge cases
// -----------------------------------------------------------------------------

func TestPageFromOffset(t *testing.T) {
	cases := []struct {
		name          string
		offset, limit int
		want          int
	}{
		{"zero limit -> page 1", 50, 0, 1},
		{"negative limit -> page 1", 50, -10, 1},
		{"zero offset -> page 1", 0, 10, 1},
		{"negative offset -> page 1", -5, 10, 1},
		{"offset 10 limit 10 -> page 2", 10, 10, 2},
		{"offset 25 limit 10 -> page 3", 25, 10, 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := pageFromOffset(tc.offset, tc.limit); got != tc.want {
				t.Fatalf("pageFromOffset(%d,%d) = %d, want %d", tc.offset, tc.limit, got, tc.want)
			}
		})
	}
}

// ListUsers with a paging window forwards the computed page to qrdb.
func TestQRDBUserStorage_ListUsers_PagingForwarded(t *testing.T) {
	svc := &pageCapturingServicer{fakeUserServicer: newFakeUserServicer()}
	store, _ := newTestUserStorage(t, svc, "acme")
	if _, err := store.ListUsers(context.Background(), "acme", iam.ListOptions{Offset: 20, Limit: 10}); err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if svc.lastPage != 3 || svc.lastPageSize != 10 {
		t.Fatalf("paging forwarded page=%d size=%d, want page=3 size=10", svc.lastPage, svc.lastPageSize)
	}
}

type pageCapturingServicer struct {
	*fakeUserServicer
	lastPage     int
	lastPageSize int
}

func (s *pageCapturingServicer) ListUsers(ctx context.Context, req *qrdbsdk.ListUsersRequest) (*qrdbsdk.ListUsersResponse, error) {
	s.lastPage = req.Page
	s.lastPageSize = req.PageSize
	return s.fakeUserServicer.ListUsers(ctx, req)
}

// -----------------------------------------------------------------------------
// splitIndexValue + indexLookup malformed-value branches
// -----------------------------------------------------------------------------

func TestSplitIndexValue(t *testing.T) {
	cases := []struct {
		name  string
		in    string
		wantA string
		wantB string
	}{
		{"well-formed", "acme:uuid-1", "acme", "uuid-1"},
		{"no colon", "noseparator", "", ""},
		{"empty", "", "", ""},
		{"leading colon -> empty tenant", ":uuid", "", "uuid"},
		{"trailing colon -> empty uuid", "acme:", "acme", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := splitIndexValue(tc.in)
			if got[0] != tc.wantA || got[1] != tc.wantB {
				t.Fatalf("splitIndexValue(%q) = %v, want [%q %q]", tc.in, got, tc.wantA, tc.wantB)
			}
		})
	}
}

// A malformed broker index value surfaces as an internal error from the lookup,
// distinct from a clean miss.
func TestQRDBUserStorage_MalformedIndexValue(t *testing.T) {
	svc := newFakeUserServicer()
	kv := newRoutingTestKV()
	store := newCustomUserStorage(svc, &stubProfileStore{}, NewBackendIDMap(kv), kv)
	// Write a malformed (no-colon) value at the global email index key directly.
	if err := kv.Put(context.Background(), userEmailGlobalKey("bad@acme.test"), []byte("no-separator")); err != nil {
		t.Fatalf("seed malformed index: %v", err)
	}
	_, err := store.GetUserByEmail(context.Background(), "bad@acme.test")
	if err == nil {
		t.Fatal("malformed index value must surface an error")
	}
	if errors.Is(err, iam.ErrUserNotFound) {
		t.Fatalf("malformed value reported as not-found: %v", err)
	}
	var qb *dberrors.QBError
	if !errors.As(err, &qb) || qb.Op != "iam.user.qrdb.index" {
		t.Fatalf("expected QBError op iam.user.qrdb.index, got %v", err)
	}
}

// A scoped-key index read error (non-not-found) propagates from resolveIndex.
func TestQRDBUserStorage_ScopedIndexReadError(t *testing.T) {
	svc := newFakeUserServicer()
	kv := newKeyFaultKV("acme")
	kv.getFailSub = "qrdbuser:email:"
	store := newCustomUserStorage(svc, &stubProfileStore{}, NewBackendIDMap(kv.routingTestKV), kv)
	ctx := iam.ContextWithTenant(context.Background(), "acme")
	if _, err := store.GetUserByEmail(ctx, "a@acme.test"); !errors.Is(err, errInjected) {
		t.Fatalf("scoped index read error = %v, want injected failure", err)
	}
}

// -----------------------------------------------------------------------------
// orEmptyMap / orEmptySlice / orEmptyCertBindings
// -----------------------------------------------------------------------------

func TestOrEmptyHelpers(t *testing.T) {
	if m := orEmptyMap(nil); m == nil || len(m) != 0 {
		t.Fatalf("orEmptyMap(nil) = %v, want empty non-nil map", m)
	}
	in := map[string]string{"k": "v"}
	if m := orEmptyMap(in); len(m) != 1 || m["k"] != "v" {
		t.Fatalf("orEmptyMap(non-nil) lost data: %v", m)
	}
	if s := orEmptySlice(nil); s == nil || len(s) != 0 {
		t.Fatalf("orEmptySlice(nil) = %v, want empty non-nil slice", s)
	}
	if s := orEmptySlice([]string{"a"}); len(s) != 1 {
		t.Fatalf("orEmptySlice(non-nil) lost data: %v", s)
	}
	if b := orEmptyCertBindings(nil); b == nil || len(b) != 0 {
		t.Fatalf("orEmptyCertBindings(nil) = %v, want empty non-nil slice", b)
	}
	if b := orEmptyCertBindings([]qrdbsdk.CertBinding{{Thumbprint: "t"}}); len(b) != 1 {
		t.Fatalf("orEmptyCertBindings(non-nil) lost data: %v", b)
	}
}
