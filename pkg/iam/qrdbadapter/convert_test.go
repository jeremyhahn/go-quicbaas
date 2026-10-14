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
	"errors"
	"reflect"
	"testing"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

func assertTimeEq(t *testing.T, what string, got, want time.Time) {
	t.Helper()
	if !got.Equal(want) {
		t.Fatalf("%s = %v, want %v", what, got, want)
	}
}

// -----------------------------------------------------------------------------
// User projection
// -----------------------------------------------------------------------------

func TestUserRoundTrip_FullFidelity(t *testing.T) {
	created := time.Now().Add(-72 * time.Hour).UTC()
	updated := time.Now().Add(-time.Hour).UTC()
	lastAuth := time.Now().Add(-30 * time.Minute).UTC()

	orig := &iam.User{
		ID:          1001,
		Email:       "alice@acme.test",
		Name:        "Alice",
		TenantID:    "acme",
		Roles:       []string{"role-admin", "role-auditor"},
		AuthMethods: []string{"webauthn", "piv"},
		Lifecycle:   iam.LifecycleActive,
		LastAuth:    &lastAuth,
		Attributes:  map[string]string{"dept": "eng", "region": "us-east"},
		CertBindings: []qrdbsdk.CertBinding{
			{Thumbprint: "spki-1", Kind: qrdbsdk.CertBindingSPKIThumbprint},
		},
		Created: created,
		Updated: updated,
	}

	core, err := FromIAMUser(orig)
	if err != nil {
		t.Fatalf("FromIAMUser: %v", err)
	}
	if core.ID != 1001 {
		t.Fatalf("core.ID = %d, want 1001", core.ID)
	}
	got := ToIAMUser(core)

	if got.ID != orig.ID || got.Email != orig.Email || got.Name != orig.Name || got.TenantID != orig.TenantID {
		t.Fatalf("identity did not round trip: %+v", got)
	}
	if !reflect.DeepEqual(got.Roles, orig.Roles) {
		t.Fatalf("Roles = %v, want %v", got.Roles, orig.Roles)
	}
	if !reflect.DeepEqual(got.AuthMethods, orig.AuthMethods) {
		t.Fatalf("AuthMethods = %v, want %v", got.AuthMethods, orig.AuthMethods)
	}
	if got.Lifecycle != orig.Lifecycle {
		t.Fatalf("Lifecycle = %q, want %q", got.Lifecycle, orig.Lifecycle)
	}
	if got.LastAuth == nil || !got.LastAuth.Equal(lastAuth) {
		t.Fatalf("LastAuth = %v, want %v", got.LastAuth, lastAuth)
	}
	if !reflect.DeepEqual(got.Attributes, orig.Attributes) {
		t.Fatalf("Attributes = %v, want %v", got.Attributes, orig.Attributes)
	}
	if !reflect.DeepEqual(got.CertBindings, orig.CertBindings) {
		t.Fatalf("CertBindings = %v, want %v", got.CertBindings, orig.CertBindings)
	}
	assertTimeEq(t, "Created", got.Created, created)
	assertTimeEq(t, "Updated", got.Updated, updated)
}

// TestFromIAMUser_AcceptsAnUncreatedRecord: qrdb assigns the id server-side, so
// an empty id is the normal state of a user on its way to being created.
func TestFromIAMUser_AcceptsAnUncreatedRecord(t *testing.T) {
	core, err := FromIAMUser(&iam.User{TenantID: "acme", Email: "new@acme.test", Name: "New"})
	if err != nil {
		t.Fatalf("FromIAMUser: %v", err)
	}
	if core.ID != 0 {
		t.Fatalf("core.ID = %d, want the zero the store will replace", core.ID)
	}
}

func TestFromIAMUser_RefusesNil(t *testing.T) {
	if _, err := FromIAMUser(nil); !errors.Is(err, iam.ErrInvalidUserID) {
		t.Fatalf("err = %v, want iam.ErrInvalidUserID for a nil user", err)
	}
}

// TestFromIAMUser_NormalizesThroughTheSDK pins that the canonical normalization
// is qrdb's. A consumer folding these sets its own way would disagree with the
// store about what the user may present.
func TestFromIAMUser_NormalizesThroughTheSDK(t *testing.T) {
	core, err := FromIAMUser(&iam.User{
		TenantID:    "acme",
		Email:       "  Alice@ACME.test ",
		AuthMethods: []string{"WebAuthn", "webauthn", "PIV"},
	})
	if err != nil {
		t.Fatalf("FromIAMUser: %v", err)
	}
	if core.Email != "alice@acme.test" {
		t.Fatalf("Email = %q, want the canonical normalization applied", core.Email)
	}
	if len(core.AuthMethods) != 2 || core.AuthMethods[0] != "webauthn" {
		t.Fatalf("AuthMethods = %v, want lowercased and de-duplicated", core.AuthMethods)
	}
}

// TestCoreUsername_IsTheHandleNotTheDisplayName pins that the qrdb username is
// the account's own handle. It used to be the display NAME, which broke the
// field in both directions at once:
//
//   - a create carrying username "alice" and display name "Alice Smith" wrote
//     "Alice Smith" into qrdb's Username, so GetUserByUsername("alice") — the
//     lookup RBAC assign/revoke and a username-subject ceremony both run —
//     missed the record it had just written;
//   - qrdb's Username is tenant-unique, and a display name is not a unique
//     value. Two people called "Alice Smith" collided on a constraint neither
//     had asked to share.
//
// Deriving it from the email would be the other wrong answer, for the first
// reason: every lookup by handle would miss for a user that has an address.
func TestCoreUsername_IsTheHandleNotTheDisplayName(t *testing.T) {
	u := &iam.User{Email: "alice@acme.test", Username: "alice", Name: "Alice Smith"}
	if got := CoreUsername(u); got != "alice" {
		t.Fatalf("CoreUsername = %q, want the handle, not the display name or the email", got)
	}
	if got := CoreUsername(&iam.User{Username: "admin"}); got != "admin" {
		t.Fatalf("CoreUsername = %q, want the handle", got)
	}
}

// TestUserRoundTrip_PreservesTheHandleAndTheDisplayNameSeparately proves the
// projection is faithful in BOTH directions for the two name fields. It was
// lossy in both: the write side fabricated a Username out of the display name,
// and the read side dropped qrdb's Username unless it was standing in for a
// missing display name — so a round trip through qrdb could not return the
// handle the caller created the account with.
func TestUserRoundTrip_PreservesTheHandleAndTheDisplayNameSeparately(t *testing.T) {
	in := &iam.User{
		ID:       1001,
		TenantID: "acme",
		Email:    "alice@acme.test",
		Username: "alice",
		Name:     "Alice Smith",
	}

	core, err := FromIAMUser(in)
	if err != nil {
		t.Fatalf("FromIAMUser: %v", err)
	}
	if core.Username != "alice" {
		t.Fatalf("qrdb Username = %q, want the handle", core.Username)
	}
	if core.Name != "Alice Smith" {
		t.Fatalf("qrdb Name = %q, want the display name", core.Name)
	}

	got := ToIAMUser(core)
	if got.Username != in.Username {
		t.Fatalf("round-tripped Username = %q, want %q", got.Username, in.Username)
	}
	if got.Name != in.Name {
		t.Fatalf("round-tripped Name = %q, want %q", got.Name, in.Name)
	}
}

func TestToIAMUser_DisplayNameFallsBackToUsername(t *testing.T) {
	// A system user carries a username and no display name; the projection must
	// not surface a nameless principal.
	got := ToIAMUser(qrdbsdk.User{ID: 7, Username: "admin", TenantID: "acme"})
	if got.Name != "admin" {
		t.Fatalf("Name = %q, want the username fallback", got.Name)
	}
	// An explicit display name wins.
	got = ToIAMUser(qrdbsdk.User{ID: 7, Username: "alice@acme.test", Name: "Alice", TenantID: "acme"})
	if got.Name != "Alice" {
		t.Fatalf("Name = %q, want the display name", got.Name)
	}
}

func TestUserRoundTrip_EmptyCollectionsStayNil(t *testing.T) {
	core, err := FromIAMUser(&iam.User{ID: 1001, TenantID: "acme", Email: "min@acme.test"})
	if err != nil {
		t.Fatalf("FromIAMUser: %v", err)
	}
	got := ToIAMUser(core)
	if got.Roles != nil || got.AuthMethods != nil ||
		got.Attributes != nil || got.CertBindings != nil {
		t.Fatalf("empty collections must round trip as nil, got %+v", got)
	}
}

// TestUserProjection_DoesNotAliasBackingStorage: the projection and the store
// record must never share a slice or map, or a caller mutating one would
// silently edit the other.
func TestUserProjection_DoesNotAliasBackingStorage(t *testing.T) {
	orig := &iam.User{
		ID: 1001, TenantID: "acme", Email: "alice@acme.test",
		Roles:      []string{"role-admin"},
		Attributes: map[string]string{"dept": "eng"},
		CertBindings: []qrdbsdk.CertBinding{
			{Thumbprint: "spki-1", Kind: qrdbsdk.CertBindingSPKIThumbprint},
		},
	}
	core, err := FromIAMUser(orig)
	if err != nil {
		t.Fatalf("FromIAMUser: %v", err)
	}
	core.RoleIDs[0] = "mutated"
	core.Attributes["dept"] = "mutated"
	core.CertBindings[0].Thumbprint = "mutated"

	if orig.Roles[0] != "role-admin" || orig.Attributes["dept"] != "eng" ||
		orig.CertBindings[0].Thumbprint != "spki-1" {
		t.Fatalf("the projection aliased the source: %+v", orig)
	}
}

func TestUserRoundTrip_MultipleCertBindings(t *testing.T) {
	orig := &iam.User{
		ID: 1001, TenantID: "acme", Email: "piv@acme.test",
		CertBindings: []qrdbsdk.CertBinding{
			{Thumbprint: "spki-old", Kind: qrdbsdk.CertBindingSPKIThumbprint},
			{Thumbprint: "spki-new", Kind: qrdbsdk.CertBindingSPKIThumbprint},
			{Thumbprint: "legacy-hex", Kind: qrdbsdk.CertBindingPIVAttested},
		},
	}
	core, err := FromIAMUser(orig)
	if err != nil {
		t.Fatalf("FromIAMUser: %v", err)
	}
	got := ToIAMUser(core)
	if len(got.CertBindings) != 3 {
		t.Fatalf("len(CertBindings) = %d, want 3 — a renewal must not drop the old binding", len(got.CertBindings))
	}
}

// -----------------------------------------------------------------------------
// Role projection
// -----------------------------------------------------------------------------

func TestRoleRoundTrip(t *testing.T) {
	created := time.Now().Add(-100 * time.Hour).UTC()
	updated := time.Now().Add(-2 * time.Hour).UTC()
	orig := &iam.Role{
		ID:          "role-1",
		Name:        "admin",
		TenantID:    "acme",
		Permissions: []string{"kv:put", "kv:get", "admin:health"},
		Created:     created,
		Updated:     updated,
	}
	core := FromIAMRole(orig)
	if core.ID != "role-1" || core.Name != "admin" || core.TenantID != "acme" {
		t.Fatalf("role core mismatch: %+v", core)
	}
	if len(core.Permissions) != 3 {
		t.Fatalf("permission count = %d, want 3", len(core.Permissions))
	}

	got := ToIAMRole(core)
	if got.ID != orig.ID || got.Name != orig.Name || got.TenantID != orig.TenantID {
		t.Fatalf("role round-trip identity mismatch: %+v", got)
	}
	if !reflect.DeepEqual(got.Permissions, orig.Permissions) {
		t.Fatalf("permission round-trip mismatch: %v want %v", got.Permissions, orig.Permissions)
	}
	assertTimeEq(t, "role.Created", got.Created, created)
	assertTimeEq(t, "role.Updated", got.Updated, updated)
}

// FromIAMRole skips malformed permission strings so a single bad entry cannot
// corrupt the whole role.
func TestFromIAMRole_SkipsMalformedPermissions(t *testing.T) {
	core := FromIAMRole(&iam.Role{
		ID: "role-2", Name: "mixed", TenantID: "acme",
		Permissions: []string{"kv:put", "garbage", "", "kv:get"},
	})
	if len(core.Permissions) != 2 {
		t.Fatalf("expected 2 valid permissions, got %d: %v", len(core.Permissions), core.Permissions)
	}
}

func TestFromIAMRole_AllMalformedYieldsNil(t *testing.T) {
	core := FromIAMRole(&iam.Role{ID: "r", Name: "n", TenantID: "acme", Permissions: []string{"bad", "alsobad"}})
	if core.Permissions != nil {
		t.Fatalf("all-malformed permissions should yield nil, got %v", core.Permissions)
	}
	core = FromIAMRole(&iam.Role{ID: "r", Name: "n", TenantID: "acme"})
	if core.Permissions != nil {
		t.Fatalf("no permissions should yield nil, got %v", core.Permissions)
	}
}
