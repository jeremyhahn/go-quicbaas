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
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

const testTenant = "acme"

var errInjected = errors.New("injected store failure")

func newTestUser() *iam.User {
	return &iam.User{
		TenantID:    testTenant,
		Email:       "alice@acme.test",
		Username:    "alice",
		Name:        "Alice",
		Roles:       []string{"role-admin"},
		AuthMethods: []string{"webauthn"},
		Lifecycle:   iam.LifecycleActive,
		Attributes:  map[string]string{"dept": "eng"},
	}
}

// -----------------------------------------------------------------------------
// CreateUser
// -----------------------------------------------------------------------------

// TestCreateUser_StampsTheStoreAssignedIdentity is the heart of #359: the store
// mints the identity and the record carries it, so nothing has to correlate two
// identifier spaces afterwards.
func TestCreateUser_StampsTheStoreAssignedIdentity(t *testing.T) {
	svc := newFakeUserServicer()
	store := newTestUserStorage(t, svc)
	ctx := context.Background()

	u := newTestUser()
	if err := store.CreateUser(ctx, u); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if u.ID == "" {
		t.Fatal("CreateUser must stamp the store-assigned id onto the record")
	}
	id, err := iam.ParseUserID(u.ID)
	if err != nil {
		t.Fatalf("the stamped id must be a qrdb snowflake: %v", err)
	}
	stored, err := svc.GetUser(ctx, id)
	if err != nil {
		t.Fatalf("the stamped id must address the record qrdb wrote: %v", err)
	}
	if stored.Email != "alice@acme.test" {
		t.Fatalf("Email = %q, want the canonical login key to reach the store", stored.Email)
	}
	if stored.Username != "alice" {
		t.Fatalf("Username = %q, want the account's handle", stored.Username)
	}
	if stored.Name != "Alice" {
		t.Fatalf("Name = %q, want the display name preserved separately", stored.Name)
	}
}

// TestCreateUser_SendsEmailOrDeclaresItsAbsence covers the runtime break a green
// build says nothing about: qrdb refuses a create that omits the login key
// unless the caller declares the omission.
func TestCreateUser_SendsEmailOrDeclaresItsAbsence(t *testing.T) {
	svc := newFakeUserServicer()
	store := newTestUserStorage(t, svc)
	ctx := context.Background()

	// A user with no email is legitimate — an mTLS- or PIV-only identity that
	// authenticates by certificate — and the adapter declares it rather than
	// letting the store refuse.
	u := &iam.User{
		TenantID: testTenant, Username: "node-daemon", Name: "Node Daemon", Lifecycle: iam.LifecycleActive,
		CertBindings: []qrdbsdk.CertBinding{{Thumbprint: "spki-node-daemon", Kind: qrdbsdk.CertBindingSPKIThumbprint}},
	}
	if err := store.CreateUser(ctx, u); err != nil {
		t.Fatalf("a user with no email must be created with the omission declared: %v", err)
	}
	id, _ := iam.ParseUserID(u.ID)
	stored, err := svc.GetUser(ctx, id)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if stored.Email != "" {
		t.Fatalf("Email = %q, want empty", stored.Email)
	}
	if stored.Username != "node-daemon" {
		t.Fatalf("Username = %q, want the handle", stored.Username)
	}

	// Neither an email nor a certificate binding: nothing could log in as it,
	// so it is refused before qrdb is asked to write it.
	ghost := &iam.User{TenantID: testTenant, Username: "ghost", Name: "Ghost", Lifecycle: iam.LifecycleActive}
	var noKey *iam.PrincipalWithoutLoginKeyError
	if err := store.CreateUser(ctx, ghost); !errors.As(err, &noKey) {
		t.Fatalf("CreateUser err = %v, want PrincipalWithoutLoginKeyError", err)
	}
	if _, err := svc.GetUserByUsername(ctx, testTenant, "ghost"); err == nil {
		t.Fatal("a refused create reached the qrdb store")
	}
}

func TestCreateUser_DuplicateEmailMapsToAlreadyExists(t *testing.T) {
	svc := newFakeUserServicer()
	store := newTestUserStorage(t, svc)
	ctx := context.Background()

	if err := store.CreateUser(ctx, newTestUser()); err != nil {
		t.Fatalf("first CreateUser: %v", err)
	}
	err := store.CreateUser(ctx, newTestUser())
	if !errors.Is(err, iam.ErrAlreadyExists) {
		t.Fatalf("err = %v, want iam.ErrAlreadyExists for a duplicate login key", err)
	}
}

func TestCreateUser_MalformedIDIsRefused(t *testing.T) {
	store := newTestUserStorage(t, newFakeUserServicer())
	u := newTestUser()
	u.ID = "not-a-snowflake"
	if err := store.CreateUser(context.Background(), u); !errors.Is(err, iam.ErrInvalidUserID) {
		t.Fatalf("err = %v, want iam.ErrInvalidUserID", err)
	}
}

func TestCreateUser_CoreErrorIsWrapped(t *testing.T) {
	svc := newFakeUserServicer()
	svc.forceErr = errInjected
	store := newTestUserStorage(t, svc)
	if err := store.CreateUser(context.Background(), newTestUser()); !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want the store failure surfaced", err)
	}
}

// -----------------------------------------------------------------------------
// GetUser / GetUserByEmail / GetUserByUsername
// -----------------------------------------------------------------------------

func TestGetUser_RoundTripsEveryProjectedField(t *testing.T) {
	svc := newFakeUserServicer()
	store := newTestUserStorage(t, svc)
	ctx := context.Background()

	u := newTestUser()
	if err := store.CreateUser(ctx, u); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	got, err := store.GetUser(ctx, testTenant, u.ID)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if got.Email != u.Email || got.Name != u.Name || got.TenantID != u.TenantID {
		t.Fatalf("identity fields did not round trip: %+v", got)
	}
	if len(got.Roles) != 1 || got.Roles[0] != "role-admin" {
		t.Fatalf("Roles = %v", got.Roles)
	}
	if len(got.AuthMethods) != 1 || got.AuthMethods[0] != "webauthn" {
		t.Fatalf("AuthMethods = %v", got.AuthMethods)
	}
	if got.Lifecycle != iam.LifecycleActive {
		t.Fatalf("Lifecycle = %q, want active", got.Lifecycle)
	}
	if got.Attributes["dept"] != "eng" {
		t.Fatalf("Attributes = %v", got.Attributes)
	}
}

func TestGetUser_MalformedIDIsNotFound(t *testing.T) {
	store := newTestUserStorage(t, newFakeUserServicer())
	if _, err := store.GetUser(context.Background(), testTenant, "nonsense"); !errors.Is(err, iam.ErrUserNotFound) {
		t.Fatalf("err = %v, want iam.ErrUserNotFound", err)
	}
}

// TestGetUserByEmail_ResolvesThroughTheCanonicalIndex is the burn-in regression.
// The login ceremony resolves a verified factor by email; before this change the
// lookup went through a broker-owned side-index that a user created any other
// way was simply absent from, so the record existed and the login was refused.
func TestGetUserByEmail_ResolvesThroughTheCanonicalIndex(t *testing.T) {
	svc := newFakeUserServicer()
	store := newTestUserStorage(t, svc)
	ctx := context.Background()

	// Seeded straight into qrdb — never through this adapter, so no broker-side
	// index could have been written for it.
	svc.seed(&qrdbsdk.User{
		Username: "burnin-operator@automatethethings.com",
		Email:    "burnin-operator@automatethethings.com",
		Name:     "Burn-in Operator",
		TenantID: testTenant,
		RoleIDs:  []string{"role-operator"},
	})

	got, err := store.GetUserByEmail(ctx, testTenant, "burnin-operator@automatethethings.com")
	if err != nil {
		t.Fatalf("a record present in qrdb must resolve by its canonical email: %v", err)
	}
	if got.Email != "burnin-operator@automatethethings.com" {
		t.Fatalf("Email = %q", got.Email)
	}
	if len(got.Roles) != 1 || got.Roles[0] != "role-operator" {
		t.Fatalf("Roles = %v, want the record's own roles", got.Roles)
	}
}

func TestGetUserByEmail_IsTenantScoped(t *testing.T) {
	svc := newFakeUserServicer()
	store := newTestUserStorage(t, svc)
	ctx := context.Background()

	svc.seed(&qrdbsdk.User{Username: "shared@x.test", Email: "shared@x.test", TenantID: "other"})

	// The same address in a tenant the caller did not ask about must not answer.
	if _, err := store.GetUserByEmail(ctx, testTenant, "shared@x.test"); !errors.Is(err, iam.ErrUserNotFound) {
		t.Fatalf("err = %v, want a foreign-tenant record to be invisible", err)
	}
	if _, err := store.GetUserByEmail(ctx, "other", "shared@x.test"); err != nil {
		t.Fatalf("the owning tenant must still resolve it: %v", err)
	}
}

func TestGetUserByEmail_NormalizesAndRejectsEmptyInput(t *testing.T) {
	svc := newFakeUserServicer()
	store := newTestUserStorage(t, svc)
	ctx := context.Background()

	svc.seed(&qrdbsdk.User{Username: "mixed@case.test", Email: "mixed@case.test", TenantID: testTenant})

	if _, err := store.GetUserByEmail(ctx, testTenant, "  Mixed@Case.TEST "); err != nil {
		t.Fatalf("the canonical normalization must be applied before lookup: %v", err)
	}
	if _, err := store.GetUserByEmail(ctx, testTenant, ""); !errors.Is(err, iam.ErrUserNotFound) {
		t.Fatalf("err = %v, want an empty email refused", err)
	}
	if _, err := store.GetUserByEmail(ctx, "", "mixed@case.test"); !errors.Is(err, iam.ErrUserNotFound) {
		t.Fatalf("err = %v, want a lookup with no tenant refused rather than answered globally", err)
	}
}

func TestGetUserByUsername_ResolvesAndFailsClosed(t *testing.T) {
	svc := newFakeUserServicer()
	store := newTestUserStorage(t, svc)
	ctx := context.Background()

	svc.seed(&qrdbsdk.User{Username: "admin", TenantID: testTenant, Name: "Bootstrap Admin"})

	got, err := store.GetUserByUsername(ctx, testTenant, "admin")
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}
	if got.Name != "Bootstrap Admin" {
		t.Fatalf("Name = %q", got.Name)
	}
	if _, err := store.GetUserByUsername(ctx, testTenant, "nobody"); !errors.Is(err, iam.ErrUserNotFound) {
		t.Fatalf("err = %v, want iam.ErrUserNotFound", err)
	}
}

// -----------------------------------------------------------------------------
// UpdateUser
// -----------------------------------------------------------------------------

func TestUpdateUser_AppliesAFullReplacement(t *testing.T) {
	svc := newFakeUserServicer()
	store := newTestUserStorage(t, svc)
	ctx := context.Background()

	u := newTestUser()
	if err := store.CreateUser(ctx, u); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	u.Name = "Alice Renamed"
	u.Roles = []string{"role-auditor"}
	if err := store.UpdateUser(ctx, u); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	got, err := store.GetUser(ctx, testTenant, u.ID)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if got.Name != "Alice Renamed" {
		t.Fatalf("Name = %q", got.Name)
	}
	if len(got.Roles) != 1 || got.Roles[0] != "role-auditor" {
		t.Fatalf("Roles = %v", got.Roles)
	}
}

// TestUpdateUser_SendsLifecycleAsATransition pins that the state is handed to
// qrdb's transition table rather than written as a field, which is what makes an
// illegal move the store's decision instead of this adapter's.
func TestUpdateUser_SendsLifecycleAsATransition(t *testing.T) {
	svc := newFakeUserServicer()
	store := newTestUserStorage(t, svc)
	ctx := context.Background()

	u := newTestUser()
	u.Lifecycle = iam.LifecyclePending
	if err := store.CreateUser(ctx, u); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	u.Lifecycle = iam.LifecycleActive
	if err := store.UpdateUser(ctx, u); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	if svc.lastUpdate == nil || svc.lastUpdate.Lifecycle != qrdbsdk.LifecycleActive {
		t.Fatalf("the update must carry the lifecycle transition, got %+v", svc.lastUpdate)
	}
	got, _ := store.GetUser(ctx, testTenant, u.ID)
	if !got.CanAuthenticate() {
		t.Fatal("an activated account must be able to authenticate")
	}
}

func TestUpdateUser_UnknownAndMalformedIDs(t *testing.T) {
	store := newTestUserStorage(t, newFakeUserServicer())
	ctx := context.Background()

	u := newTestUser()
	u.ID = ""
	if err := store.UpdateUser(ctx, u); !errors.Is(err, iam.ErrUserNotFound) {
		t.Fatalf("err = %v, want iam.ErrUserNotFound for a record with no id", err)
	}
	u.ID = "not-a-snowflake"
	if err := store.UpdateUser(ctx, u); !errors.Is(err, iam.ErrInvalidUserID) {
		t.Fatalf("err = %v, want iam.ErrInvalidUserID", err)
	}
	u.ID = "999999"
	if err := store.UpdateUser(ctx, u); !errors.Is(err, iam.ErrUserNotFound) {
		t.Fatalf("err = %v, want iam.ErrUserNotFound for an id nothing is stored under", err)
	}
}

// -----------------------------------------------------------------------------
// DeleteUser
// -----------------------------------------------------------------------------

// TestMarkUserDeleting_SendsTheLifecycleAndNothingElse is the shape the store's
// terminal rule requires.
//
// A record that is already `deleting` refuses any update that would CHANGE it,
// permitting only a re-assertion of the state it holds. A marker that carried
// the rest of the user would therefore be rejected the SECOND time it ran — and
// the second time is a retried deletion, which is the case the terminal state
// exists to make resumable. Asserting the request shape is what stops a
// well-meaning "send the whole record" edit from reintroducing that.
func TestMarkUserDeleting_SendsTheLifecycleAndNothingElse(t *testing.T) {
	svc := newFakeUserServicer()
	store := newTestUserStorage(t, svc)
	ctx := context.Background()

	u := newTestUser()
	if err := store.CreateUser(ctx, u); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := store.MarkUserDeleting(ctx, testTenant, u.ID); err != nil {
		t.Fatalf("MarkUserDeleting: %v", err)
	}
	req := svc.lastUpdate
	if req == nil || req.Lifecycle != qrdbsdk.LifecycleDeleting {
		t.Fatalf("the marker must carry the deleting transition, got %+v", req)
	}
	if !onlyReassertsLifecycle(req) {
		t.Fatalf("the marker must carry the lifecycle and nothing else, got %+v", req)
	}

	// And it is idempotent, which is what a retried deletion depends on.
	if err := store.MarkUserDeleting(ctx, testTenant, u.ID); err != nil {
		t.Fatalf("re-marking an already-marked record must be a no-op, got %v", err)
	}

	got, err := store.GetUser(ctx, testTenant, u.ID)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if got.CanAuthenticate() {
		t.Fatal("a record claimed for deletion must not be able to authenticate")
	}
}

func TestMarkUserDeleting_UnknownAndMalformedIDs(t *testing.T) {
	store := newTestUserStorage(t, newFakeUserServicer())
	ctx := context.Background()

	if err := store.MarkUserDeleting(ctx, testTenant, "nonsense"); !errors.Is(err, iam.ErrUserNotFound) {
		t.Fatalf("err = %v, want iam.ErrUserNotFound", err)
	}
	if err := store.MarkUserDeleting(ctx, testTenant, "424242"); !errors.Is(err, iam.ErrUserNotFound) {
		t.Fatalf("err = %v, want iam.ErrUserNotFound", err)
	}
}

// TestDeleteUser_RemovesTheRecord. The deletion CLAIM is made by
// IdentityStore.DeleteUser before it sweeps credentials, and by qrdb's own
// DeleteUser as its first act; this layer does not add a third marker.
func TestDeleteUser_RemovesTheRecord(t *testing.T) {
	svc := newFakeUserServicer()
	store := newTestUserStorage(t, svc)
	ctx := context.Background()

	u := newTestUser()
	if err := store.CreateUser(ctx, u); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := store.DeleteUser(ctx, testTenant, u.ID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if _, err := store.GetUser(ctx, testTenant, u.ID); !errors.Is(err, iam.ErrUserNotFound) {
		t.Fatalf("err = %v, want the record gone", err)
	}
}

// TestDeleteUser_SurvivesAMarkedRecord: a deletion that was interrupted after
// the claim must still be completable. It is the reason the claim is a
// lifecycle-only request rather than a full-record write.
func TestDeleteUser_SurvivesAMarkedRecord(t *testing.T) {
	svc := newFakeUserServicer()
	store := newTestUserStorage(t, svc)
	ctx := context.Background()

	u := newTestUser()
	if err := store.CreateUser(ctx, u); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := store.MarkUserDeleting(ctx, testTenant, u.ID); err != nil {
		t.Fatalf("MarkUserDeleting: %v", err)
	}
	if err := store.DeleteUser(ctx, testTenant, u.ID); err != nil {
		t.Fatalf("a deletion resumed after its claim must complete: %v", err)
	}
}

func TestDeleteUser_UnknownAndMalformedIDs(t *testing.T) {
	store := newTestUserStorage(t, newFakeUserServicer())
	ctx := context.Background()

	if err := store.DeleteUser(ctx, testTenant, "nonsense"); !errors.Is(err, iam.ErrUserNotFound) {
		t.Fatalf("err = %v, want iam.ErrUserNotFound", err)
	}
	if err := store.DeleteUser(ctx, testTenant, "424242"); !errors.Is(err, iam.ErrUserNotFound) {
		t.Fatalf("err = %v, want iam.ErrUserNotFound", err)
	}
}

// -----------------------------------------------------------------------------
// ListUsers
// -----------------------------------------------------------------------------

// TestListUsers_ReturnsEveryTenantRecord is a regression against the old
// listing, which silently skipped any user that had no correlation entry — so a
// user created outside this adapter was invisible to an operator listing them.
func TestListUsers_ReturnsEveryTenantRecord(t *testing.T) {
	svc := newFakeUserServicer()
	store := newTestUserStorage(t, svc)
	ctx := context.Background()

	if err := store.CreateUser(ctx, newTestUser()); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	svc.seed(&qrdbsdk.User{Username: "seeded@acme.test", Email: "seeded@acme.test", TenantID: testTenant})
	svc.seed(&qrdbsdk.User{Username: "elsewhere@x.test", Email: "elsewhere@x.test", TenantID: "other"})

	users, err := store.ListUsers(ctx, testTenant, iam.ListOptions{})
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("len(users) = %d, want both of the tenant's records", len(users))
	}
	for _, u := range users {
		if u.TenantID != testTenant {
			t.Fatalf("listing leaked a record from %q", u.TenantID)
		}
	}
}

func TestListUsers_CoreErrorIsSurfaced(t *testing.T) {
	svc := newFakeUserServicer()
	svc.forceErr = errInjected
	store := newTestUserStorage(t, svc)
	if _, err := store.ListUsers(context.Background(), testTenant, iam.ListOptions{}); !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want the store failure surfaced", err)
	}
}

func TestPageFromOffset(t *testing.T) {
	for _, tc := range []struct {
		offset, limit, want int
	}{
		{0, 0, 1},
		{0, 10, 1},
		{10, 10, 2},
		{25, 10, 3},
		{5, 0, 1},
		{-1, 10, 1},
	} {
		if got := pageFromOffset(tc.offset, tc.limit); got != tc.want {
			t.Errorf("pageFromOffset(%d,%d) = %d, want %d", tc.offset, tc.limit, got, tc.want)
		}
	}
}

// -----------------------------------------------------------------------------
// PIV resolution
// -----------------------------------------------------------------------------

func TestGetUserByPIVThumbprint_SPKIResolvesFirst(t *testing.T) {
	svc := newFakeUserServicer()
	store := newTestUserStorage(t, svc)
	ctx := context.Background()

	const spki = "AAAABBBBCCCCDDDDEEEEFFFFGGGGHHHHIIIIJJJJKKK"
	svc.seed(&qrdbsdk.User{
		Username: "piv@acme.test", Email: "piv@acme.test", TenantID: testTenant,
		CertBindings: []qrdbsdk.CertBinding{{Thumbprint: spki, Kind: qrdbsdk.CertBindingPIVAttested}},
	})

	got, err := store.GetUserByPIVThumbprint(ctx, spki)
	if err != nil {
		t.Fatalf("GetUserByPIVThumbprint: %v", err)
	}
	if got.Email != "piv@acme.test" {
		t.Fatalf("Email = %q", got.Email)
	}
}

func TestGetUserByPIVThumbprint_UnknownFailsClosed(t *testing.T) {
	store := newTestUserStorage(t, newFakeUserServicer())
	if _, err := store.GetUserByPIVThumbprint(context.Background(), "nothing-bound-here"); !errors.Is(err, iam.ErrUserNotFound) {
		t.Fatalf("err = %v, want iam.ErrUserNotFound", err)
	}
}

// A racing update used to be able to leave a broker-side index entry naming
// the user for a binding the record no longer carries: the losing writer
// published last. The record is now the only authority, so a thumbprint it does
// not carry resolves nobody and there is no second lookup to fall through to.
func TestGetUserByPIVThumbprint_ResolvesOnlyWhatTheRecordCarries(t *testing.T) {
	svc := newFakeUserServicer()
	store := newTestUserStorage(t, svc)
	ctx := qrdbsdk.ContextWithTenant(context.Background(), testTenant)

	const (
		loser  = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		winner = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	)
	u := newTestUser()
	u.SetPIVThumbprints(winner)
	if err := store.CreateUser(ctx, u); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	if _, err := store.GetUserByPIVThumbprint(ctx, loser); !errors.Is(err, iam.ErrUserNotFound) {
		t.Fatalf("err = %v, want a binding the record does not carry to resolve nobody", err)
	}
	if got, err := store.GetUserByPIVThumbprint(ctx, winner); err != nil || got.ID != u.ID {
		t.Fatalf("winner resolved (%v, %v), want %q", got, err, u.ID)
	}
}

func TestGetUserByPIVThumbprint_StoreFailureIsSurfacedNotReadAsAMiss(t *testing.T) {
	svc := newFakeUserServicer()
	svc.forceErr = errInjected
	store := newTestUserStorage(t, svc)

	_, err := store.GetUserByPIVThumbprint(context.Background(), "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	if !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want the store failure surfaced", err)
	}
	if errors.Is(err, iam.ErrUserNotFound) {
		t.Fatal("a store failure must not be reported as an unknown thumbprint")
	}
}

func TestGetUserByPIVThumbprint_EmptyThumbprintFailsClosed(t *testing.T) {
	svc := newFakeUserServicer()
	store := newTestUserStorage(t, svc)
	svc.seed(&qrdbsdk.User{
		Username: "piv@acme.test", Email: "piv@acme.test", TenantID: testTenant,
		CertBindings: []qrdbsdk.CertBinding{{Thumbprint: "", Kind: qrdbsdk.CertBindingPIVAttested}},
	})

	if _, err := store.GetUserByPIVThumbprint(context.Background(), ""); !errors.Is(err, iam.ErrUserNotFound) {
		t.Fatalf("err = %v, want iam.ErrUserNotFound", err)
	}
}

func TestUpdateUser_StoreFailureIsSurfaced(t *testing.T) {
	svc := newFakeUserServicer()
	store := newTestUserStorage(t, svc)
	ctx := context.Background()

	u := newTestUser()
	if err := store.CreateUser(ctx, u); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	svc.forceErr = errInjected
	u.SetPIVThumbprints("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	if err := store.UpdateUser(ctx, u); !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want the store failure surfaced", err)
	}
}

// -----------------------------------------------------------------------------
// Fail-closed wiring
// -----------------------------------------------------------------------------

func TestQRDBUserStorage_FailsClosedWithoutAServicer(t *testing.T) {
	ctx := context.Background()

	nilResolver := NewQRDBUserStorage(nil)
	if _, err := nilResolver.GetUser(ctx, testTenant, "1001"); err == nil {
		t.Fatal("a storage with no resolver must fail closed rather than panic")
	}

	nilServicer := NewQRDBUserStorage(&fakeResolver{svc: nil})
	if _, err := nilServicer.GetUser(ctx, testTenant, "1001"); err == nil {
		t.Fatal("a resolver that yields no servicer must fail closed")
	}

	errResolver := NewQRDBUserStorage(&fakeResolver{err: errInjected})
	if _, err := errResolver.GetUser(ctx, testTenant, "1001"); !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want the resolver failure surfaced", err)
	}
}

// -----------------------------------------------------------------------------
// Error mapping
// -----------------------------------------------------------------------------

func TestMapQRDBUserError(t *testing.T) {
	if got := mapQRDBUserError("op", nil); got != nil {
		t.Fatalf("a nil error must map to nil, got %v", got)
	}
	if got := mapQRDBUserError("get", &qrdbsdk.UserNotFoundError{ID: 7}); !errors.Is(got, iam.ErrUserNotFound) {
		t.Fatalf("got = %v, want iam.ErrUserNotFound", got)
	}
	if got := mapQRDBUserError("create", &qrdbsdk.UsernameExistsError{Username: "a"}); !errors.Is(got, iam.ErrAlreadyExists) {
		t.Fatalf("got = %v, want iam.ErrAlreadyExists", got)
	}
	// An error the mapper does not recognise is wrapped, never swallowed.
	got := mapQRDBUserError("update", errInjected)
	if !errors.Is(got, errInjected) {
		t.Fatalf("got = %v, want the original error preserved", got)
	}
}

func TestLastAuthIsAHighWaterMark(t *testing.T) {
	svc := newFakeUserServicer()
	store := newTestUserStorage(t, svc)
	ctx := context.Background()

	u := newTestUser()
	if err := store.CreateUser(ctx, u); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	later := time.Now().UTC()
	earlier := later.Add(-time.Hour)

	u.LastAuth = &later
	if err := store.UpdateUser(ctx, u); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	// A delayed login reporting an older time must not roll the account back.
	u.LastAuth = &earlier
	if err := store.UpdateUser(ctx, u); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	got, err := store.GetUser(ctx, testTenant, u.ID)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if got.LastAuth == nil || !got.LastAuth.Equal(later) {
		t.Fatalf("LastAuth = %v, want the high-water mark %v", got.LastAuth, later)
	}
}

// TestCreateUser_HonoursTheRequestedLifecycle guards the self-registration
// finding: a caller that names the state must get it, because the surface that
// creates an already-verified account then hands it a session immediately. A
// record persisted as pending holding an active bearer is the two disagreeing.
func TestCreateUser_HonoursTheRequestedLifecycle(t *testing.T) {
	svc := newFakeUserServicer()
	store := newTestUserStorage(t, svc)
	ctx := context.Background()

	u := newTestUser()
	u.Lifecycle = iam.LifecycleActive
	if err := store.CreateUser(ctx, u); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	got, err := store.GetUser(ctx, testTenant, u.ID)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if got.Lifecycle != iam.LifecycleActive {
		t.Fatalf("Lifecycle = %q, want the requested active", got.Lifecycle)
	}
	if !got.CanAuthenticate() {
		t.Fatal("a user created active must be able to authenticate")
	}
}

// TestCoreUsername_MatchesTheStoresUsernameIndex pins that what the projection
// WRITES as the qrdb username is the same thing GetUserByUsername resolves. If
// these drift, a role assignment by username silently stops finding users.
func TestCoreUsername_MatchesTheStoresUsernameIndex(t *testing.T) {
	svc := newFakeUserServicer()
	store := newTestUserStorage(t, svc)
	ctx := context.Background()

	u := newTestUser()
	if err := store.CreateUser(ctx, u); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	got, err := store.GetUserByUsername(ctx, testTenant, CoreUsername(u))
	if err != nil {
		t.Fatalf("the name the projection wrote must be the name the index resolves: %v", err)
	}
	if got.ID != u.ID {
		t.Fatalf("ID = %q, want %q", got.ID, u.ID)
	}
}

// TestCreateUser_ConcurrentDuplicateEmailIsAConflictNotAnInternalError.
//
// IdentityStore checks the email before creating, but the STORE is the one that
// enforces uniqueness: two concurrent creates both pass that check, qrdb accepts
// one and refuses the other. The loser must surface as the ordinary conflict
// every transport already maps, not as a server failure.
func TestCreateUser_ConcurrentDuplicateEmailIsAConflictNotAnInternalError(t *testing.T) {
	svc := newFakeUserServicer()
	store := newTestUserStorage(t, svc)
	ctx := context.Background()

	// The winner is already in the store, and carries a DIFFERENT username so
	// the collision can only be on the email.
	svc.seed(&qrdbsdk.User{
		Username: "someone-else", Email: "alice@acme.test", TenantID: testTenant,
	})

	err := store.CreateUser(ctx, newTestUser())
	if !errors.Is(err, iam.ErrEmailAlreadyExists) {
		t.Fatalf("err = %v, want iam.ErrEmailAlreadyExists", err)
	}
}

// failAfterCreateServicer commits the create and then fails the follow-up, which
// is the window in which a record exists that the caller was told did not.
type failAfterCreateServicer struct {
	*fakeUserServicer
	failUpdate error
}

func (f *failAfterCreateServicer) UpdateUser(ctx context.Context, req *qrdbsdk.UpdateUserRequest) (*qrdbsdk.User, error) {
	if f.failUpdate != nil {
		return nil, f.failUpdate
	}
	return f.fakeUserServicer.UpdateUser(ctx, req)
}

// TestCreateUser_CompensatesAFailedFollowUp.
//
// qrdb has already committed the canonical record by the time the follow-up
// runs. Returning an error without undoing it leaves a half-initialised account
// the caller was told did not exist — and a retry then collides with its own
// email and username, so the caller can neither use it nor recreate it.
func TestCreateUser_CompensatesAFailedFollowUp(t *testing.T) {
	fake := newFakeUserServicer()
	svc := &failAfterCreateServicer{fakeUserServicer: fake, failUpdate: errInjected}
	store := newTestUserStorage(t, svc)
	ctx := context.Background()

	u := newTestUser() // carries Attributes, so the follow-up update runs
	err := store.CreateUser(ctx, u)
	if !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want the follow-up failure surfaced", err)
	}
	if u.ID != "" {
		t.Errorf("ID = %q, want it cleared: the create did not stand", u.ID)
	}
	if len(fake.users) != 0 {
		t.Fatalf("a failed create left %d record(s) behind", len(fake.users))
	}

	// And the proof that matters: the same create can be retried.
	svc.failUpdate = nil
	if err := store.CreateUser(ctx, newTestUser()); err != nil {
		t.Fatalf("a retry after a compensated create must succeed, got %v", err)
	}
}

// A revoked certificate must stop resolving the user the moment the binding
// leaves the record.
func TestUpdateUser_RevokedThumbprintResolvesNobody(t *testing.T) {
	svc := newFakeUserServicer()
	store := newTestUserStorage(t, svc)
	ctx := context.Background()

	const legacy = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	u := newTestUser()
	u.SetPIVThumbprints(legacy)
	if err := store.CreateUser(ctx, u); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if _, err := store.GetUserByPIVThumbprint(ctx, legacy); err != nil {
		t.Fatalf("the binding must resolve before it is revoked: %v", err)
	}

	// Revoke it.
	u.CertBindings = nil
	if err := store.UpdateUser(ctx, u); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	if _, err := store.GetUserByPIVThumbprint(ctx, legacy); !errors.Is(err, iam.ErrUserNotFound) {
		t.Fatalf("err = %v, want the revoked thumbprint to resolve nobody", err)
	}
}

// A deleted user's certificate must resolve nobody.
func TestDeleteUser_DeletedUsersThumbprintResolvesNobody(t *testing.T) {
	svc := newFakeUserServicer()
	store := newTestUserStorage(t, svc)
	ctx := context.Background()

	const legacy = "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
	u := newTestUser()
	u.SetPIVThumbprints(legacy)
	if err := store.CreateUser(ctx, u); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := store.DeleteUser(ctx, testTenant, u.ID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if _, err := store.GetUserByPIVThumbprint(ctx, legacy); !errors.Is(err, iam.ErrUserNotFound) {
		t.Fatalf("err = %v, want the thumbprint gone with its user", err)
	}
}

// TestUpdateUser_AnEmptyAuthMethodSetClearsTheRestriction.
//
// The model defines an empty AuthMethods as "no restriction", so clearing one
// has to be expressible. It was not: every transport dropped the field on
// LENGTH rather than presence, so an account restricted to ["piv"] accepted the
// clear, answered success, and stayed restricted.
func TestUpdateUser_AnEmptyAuthMethodSetClearsTheRestriction(t *testing.T) {
	svc := newFakeUserServicer()
	store := newTestUserStorage(t, svc)
	ctx := context.Background()

	u := newTestUser()
	u.AuthMethods = []string{"piv"}
	if err := store.CreateUser(ctx, u); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	got, err := store.GetUser(ctx, testTenant, u.ID)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if len(got.AuthMethods) != 1 || got.AuthMethods[0] != "piv" {
		t.Fatalf("AuthMethods = %v, want the restriction applied", got.AuthMethods)
	}

	// An explicitly EMPTY set — not a nil one — must reach the store as a clear.
	u.AuthMethods = []string{}
	if err := store.UpdateUser(ctx, u); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	got, err = store.GetUser(ctx, testTenant, u.ID)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if len(got.AuthMethods) != 0 {
		t.Fatalf("AuthMethods = %v, want the restriction cleared", got.AuthMethods)
	}
}
