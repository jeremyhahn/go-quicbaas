//go:build iam

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
	"reflect"
	"testing"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

func fullIAMUser() *iam.User {
	enrolled := time.Now().Add(-72 * time.Hour).UTC()
	lastAuth := time.Now().Add(-2 * time.Hour).UTC()
	mfaTS := time.Now().Add(-90 * time.Minute).UTC()
	invite := time.Now().Add(24 * time.Hour).UTC()
	created := time.Now().Add(-240 * time.Hour).UTC()
	updated := time.Now().Add(-1 * time.Hour).UTC()
	return &iam.User{
		ID:                "uuid-abc",
		Email:             "bob@example.com",
		Name:              "bob",
		TenantID:          "acme",
		Roles:             []string{"admin", "operator"},
		Groups:            []string{"g1", "g2"},
		Attributes:        map[string]interface{}{"dept": "eng", "clearance": "high"},
		Created:           created,
		Updated:           updated,
		LastAuth:          &lastAuth,
		MFAEnabled:        true,
		MFAMethod:         iam.MFAMethodWebAuthn,
		MFAEnrolledAt:     &enrolled,
		MFAVerified:       true,
		MFATimestamp:      &mfaTS,
		Suspended:         false,
		Locked:            false,
		State:             iam.UserStateActive,
		AuthMethods:       []string{"webauthn", "piv"},
		PIVCertThumbprint: "deadbeef",
		InviteToken:       "inv-tok",
		InviteExpiresAt:   &invite,
	}
}

// iam.User -> (qrdb core + BrokerProfile + IDMap entry) -> iam.User reproduces
// the original on every field.
func TestUserRoundTrip_FullFidelity(t *testing.T) {
	const qid = uint64(555)
	orig := fullIAMUser()

	core, profile, entry := FromIAMUser(orig, qid)

	// Core carries qrdb-owned fields and the assigned id.
	if core.ID != qid || core.Username != "bob" || core.TenantID != "acme" {
		t.Fatalf("core mismatch: %+v", core)
	}
	if !core.Enabled {
		t.Fatal("active, unsuspended, unlocked user must be Enabled in qrdb core")
	}
	if !reflect.DeepEqual(core.RoleIDs, []string{"admin", "operator"}) {
		t.Fatalf("core RoleIDs = %v", core.RoleIDs)
	}
	if core.Attributes["dept"] != "eng" || core.Attributes["clearance"] != "high" {
		t.Fatalf("core attributes = %v", core.Attributes)
	}

	// Entry records the correlation.
	if entry.UUID != "uuid-abc" || entry.QRDBUserID != qid || entry.TenantID != "acme" {
		t.Fatalf("entry mismatch: %+v", entry)
	}

	// Broker-only fields live on the profile, keyed by the uint64.
	if profile.QRDBUserID != qid || profile.Email != "bob@example.com" {
		t.Fatalf("profile mismatch: %+v", profile)
	}

	got := ToIAMUser(core, profile, entry.UUID)
	assertUserEqual(t, orig, got)
}

func assertUserEqual(t *testing.T, want, got *iam.User) {
	t.Helper()
	if got.ID != want.ID || got.Email != want.Email || got.Name != want.Name || got.TenantID != want.TenantID {
		t.Fatalf("identity mismatch:\n got %+v\nwant %+v", got, want)
	}
	if !reflect.DeepEqual(got.Roles, want.Roles) || !reflect.DeepEqual(got.Groups, want.Groups) {
		t.Fatalf("roles/groups mismatch: roles %v groups %v", got.Roles, got.Groups)
	}
	if !reflect.DeepEqual(got.AuthMethods, want.AuthMethods) {
		t.Fatalf("authmethods mismatch: %v", got.AuthMethods)
	}
	if !reflect.DeepEqual(got.Attributes, want.Attributes) {
		t.Fatalf("attributes mismatch: %v want %v", got.Attributes, want.Attributes)
	}
	if got.State != want.State || got.Suspended != want.Suspended || got.Locked != want.Locked {
		t.Fatalf("lifecycle mismatch: %+v", got)
	}
	if got.MFAEnabled != want.MFAEnabled || got.MFAMethod != want.MFAMethod || got.MFAVerified != want.MFAVerified {
		t.Fatalf("mfa mismatch: %+v", got)
	}
	if got.PIVCertThumbprint != want.PIVCertThumbprint || got.InviteToken != want.InviteToken {
		t.Fatalf("piv/invite mismatch: %+v", got)
	}
	assertTimeEq(t, "Created", got.Created, want.Created)
	assertTimeEq(t, "Updated", got.Updated, want.Updated)
	assertTimePtrEq(t, "LastAuth", got.LastAuth, want.LastAuth)
	assertTimePtrEq(t, "MFAEnrolledAt", got.MFAEnrolledAt, want.MFAEnrolledAt)
	assertTimePtrEq(t, "MFATimestamp", got.MFATimestamp, want.MFATimestamp)
	assertTimePtrEq(t, "InviteExpiresAt", got.InviteExpiresAt, want.InviteExpiresAt)
}

func assertTimeEq(t *testing.T, field string, got, want time.Time) {
	t.Helper()
	if !got.Equal(want) {
		t.Fatalf("%s mismatch: %v want %v", field, got, want)
	}
}

func assertTimePtrEq(t *testing.T, field string, got, want *time.Time) {
	t.Helper()
	if (got == nil) != (want == nil) {
		t.Fatalf("%s nil-ness mismatch: %v want %v", field, got, want)
	}
	if got != nil && !got.Equal(*want) {
		t.Fatalf("%s mismatch: %v want %v", field, got, want)
	}
}

// Field-gap edge cases: a minimal user (no email, no groups, no MFA, not yet
// persisted) round-trips with all broker-only fields zero and Enabled true.
func TestUserRoundTrip_MinimalUser(t *testing.T) {
	orig := &iam.User{
		ID:       "uuid-min",
		Name:     "min",
		TenantID: "acme",
	}
	core, profile, entry := FromIAMUser(orig, 0)
	if entry.QRDBUserID != 0 || core.ID != 0 || profile.QRDBUserID != 0 {
		t.Fatalf("not-yet-persisted user must carry zero qrdb id: core=%d entry=%d profile=%d", core.ID, entry.QRDBUserID, profile.QRDBUserID)
	}
	if !core.Enabled {
		t.Fatal("a fresh user with no disabled/suspended/locked flag must be Enabled")
	}
	got := ToIAMUser(core, profile, entry.UUID)
	if got.Email != "" || len(got.Groups) != 0 || got.MFAEnabled {
		t.Fatalf("minimal user gained broker fields: %+v", got)
	}
	if got.ID != "uuid-min" || got.Name != "min" || got.TenantID != "acme" {
		t.Fatalf("minimal user identity mismatch: %+v", got)
	}
}

// A disabled/suspended/locked user maps to Enabled=false in the qrdb core.
func TestFromIAMUser_EnabledMapping(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*iam.User)
	}{
		{"suspended", func(u *iam.User) { u.Suspended = true }},
		{"locked", func(u *iam.User) { u.Locked = true }},
		{"disabled state", func(u *iam.User) { u.State = iam.UserStateDisabled }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			u := &iam.User{ID: "x", Name: "x", TenantID: "acme", State: iam.UserStateActive}
			tc.mut(u)
			core, _, _ := FromIAMUser(u, 1)
			if core.Enabled {
				t.Fatalf("%s user should be disabled in qrdb core", tc.name)
			}
		})
	}
}

// ToIAMUser with a nil profile yields only qrdb-core fields and defaults Name to
// the qrdb Username.
func TestToIAMUser_NilProfile(t *testing.T) {
	core := qrdbsdk.User{
		ID:       9,
		Username: "svc",
		TenantID: "acme",
		RoleIDs:  []string{"operator"},
		Enabled:  true,
	}
	got := ToIAMUser(core, nil, "uuid-svc")
	if got.ID != "uuid-svc" || got.Name != "svc" || got.TenantID != "acme" {
		t.Fatalf("nil-profile user mismatch: %+v", got)
	}
	if got.Email != "" || len(got.Groups) != 0 {
		t.Fatalf("nil-profile user must have no broker fields: %+v", got)
	}
	if !reflect.DeepEqual(got.Roles, []string{"operator"}) {
		t.Fatalf("roles mismatch: %v", got.Roles)
	}
}

// Role round-trip preserves identity and converts permission representations.
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
	assertTimeEq(t, "role.Created", got.Created, orig.Created)
	assertTimeEq(t, "role.Updated", got.Updated, orig.Updated)
}

// FromIAMRole skips malformed permission strings (no "resource:action" colon).
func TestFromIAMRole_SkipsMalformedPermissions(t *testing.T) {
	r := &iam.Role{
		ID:          "role-2",
		Name:        "mixed",
		TenantID:    "acme",
		Permissions: []string{"kv:put", "garbage", "", "kv:get"},
	}
	core := FromIAMRole(r)
	if len(core.Permissions) != 2 {
		t.Fatalf("expected 2 valid permissions, got %d: %v", len(core.Permissions), core.Permissions)
	}
}

// FromIAMRole with no parseable permissions yields a nil slice (not empty).
func TestFromIAMRole_AllMalformed(t *testing.T) {
	r := &iam.Role{ID: "r", Name: "n", TenantID: "acme", Permissions: []string{"bad", "alsobad"}}
	core := FromIAMRole(r)
	if core.Permissions != nil {
		t.Fatalf("all-malformed permissions should yield nil, got %v", core.Permissions)
	}
}

// Non-string broker attributes are dropped when mapping to qrdb's typed map.
func TestAttributes_NonStringDropped(t *testing.T) {
	u := &iam.User{
		ID:         "u",
		Name:       "u",
		TenantID:   "acme",
		Attributes: map[string]interface{}{"keep": "yes", "drop": 42},
	}
	core, _, _ := FromIAMUser(u, 1)
	if core.Attributes["keep"] != "yes" {
		t.Fatalf("string attribute dropped: %v", core.Attributes)
	}
	if _, ok := core.Attributes["drop"]; ok {
		t.Fatal("non-string attribute should be dropped from qrdb core")
	}
}

// An attribute map containing only non-string values maps to a nil qrdb map
// (not an empty one), keeping empty round-trips nil-equal.
func TestAttributes_AllNonStringYieldsNil(t *testing.T) {
	u := &iam.User{ID: "u", Name: "u", TenantID: "acme", Attributes: map[string]interface{}{"n": 1, "b": true}}
	core, _, _ := FromIAMUser(u, 1)
	if core.Attributes != nil {
		t.Fatalf("all-non-string attributes should yield nil, got %v", core.Attributes)
	}
}

// A role with no permissions round-trips with nil permission slices.
func TestRole_EmptyPermissions(t *testing.T) {
	r := &iam.Role{ID: "r", Name: "n", TenantID: "acme"}
	core := FromIAMRole(r)
	if core.Permissions != nil {
		t.Fatalf("empty permissions should yield nil qrdb slice, got %v", core.Permissions)
	}
	back := ToIAMRole(core)
	if back.Permissions != nil {
		t.Fatalf("empty permissions should round-trip to nil broker slice, got %v", back.Permissions)
	}
}
