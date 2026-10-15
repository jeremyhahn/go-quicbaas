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

package auth

import (
	"math"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// An identity qrdb built from an mTLS certificate is unmarked, so its subject
// projects as a username, even when the username is all digits or contains
// "@".
func TestSubjectFromIdentity_UnmarkedIsUsername(t *testing.T) {
	for _, name := range []string{"admin", "12345", "operator@example"} {
		subject := SubjectFromIdentity(&qrdbsdk.Identity{
			Subject:  name,
			TenantID: "t1",
			Roles:    []string{"admin"},
		})
		if subject.IDKind != interfaces.SubjectIDUsername || subject.UserID != 0 {
			t.Fatalf("%q: IDKind = %v, UserID = %d, want a username with no user ID", name, subject.IDKind, subject.UserID)
		}
		if subject.ID != name || subject.Username != name || subject.TenantID != "t1" {
			t.Fatalf("%q: projected %+v", name, subject)
		}
		if !subject.Authenticated || len(subject.Roles) != 1 {
			t.Fatalf("%q: projected %+v, want authenticated with roles", name, subject)
		}
	}
}

// A token's sub is the ceremony's email or username; it is classified by what
// it is, never as a user ID, and the verified uid travels separately.
func TestSubjectFromIdentity_TokenSubject(t *testing.T) {
	cases := map[string]struct {
		sub      string
		userID   uint64
		wantKind interfaces.SubjectIDKind
	}{
		"email with uid":            {sub: "alice@example.com", userID: 42, wantKind: interfaces.SubjectIDEmail},
		"numeric username with uid": {sub: "12345", userID: 42, wantKind: interfaces.SubjectIDUsername},
		"uid above 2^53":            {sub: "alice", userID: 1<<53 + 1, wantKind: interfaces.SubjectIDUsername},
		"largest uid":               {sub: "alice", userID: math.MaxUint64, wantKind: interfaces.SubjectIDUsername},
		"username without uid":      {sub: "12345", wantKind: interfaces.SubjectIDUsername},
	}
	for name, tc := range cases {
		subject := SubjectFromIdentity(MarkTokenSubject(&qrdbsdk.Identity{Subject: tc.sub, TenantID: "t1"}, tc.userID))
		if subject.IDKind != tc.wantKind || subject.ID != tc.sub || subject.UserID != tc.userID {
			t.Fatalf("%s: projected %+v, want IDKind %v, ID %q, UserID %d", name, subject, tc.wantKind, tc.sub, tc.userID)
		}
	}
}

// A metadata entry other than the mark must not supply a user ID.
func TestSubjectFromIdentity_ForeignMetadataHasNoUserID(t *testing.T) {
	identity := &qrdbsdk.Identity{
		Subject:  "42",
		Metadata: map[string]string{"quicbaas.subject_kind": "user_id", "factor_type": "webauthn"},
	}
	subject := SubjectFromIdentity(identity)
	if subject.IDKind != interfaces.SubjectIDUsername || subject.UserID != 0 {
		t.Fatalf("projected %+v, want a username with no user ID", subject)
	}
}

// A mark that is not a decimal user id names no account: the subject keeps
// its kind but carries no user ID.
func TestSubjectFromIdentity_MalformedMarkHasNoUserID(t *testing.T) {
	for _, mark := range []string{"", "alice", "-1", "18446744073709551616"} {
		subject := SubjectFromIdentity(&qrdbsdk.Identity{
			Subject:  "alice@example.com",
			Metadata: map[string]string{identityUserIDKey: mark},
		})
		if subject.IDKind != interfaces.SubjectIDEmail || subject.UserID != 0 {
			t.Fatalf("mark %q: projected %+v, want an email with no user ID", mark, subject)
		}
	}
}

// The mark is merged into existing metadata, never replacing it: the login
// session issuer stores the factor type there too.
func TestMarkTokenSubject_KeepsExistingMetadata(t *testing.T) {
	identity := MarkTokenSubject(&qrdbsdk.Identity{
		Subject:  "alice",
		Metadata: map[string]string{"factor_type": "webauthn"},
	}, 42)
	if identity.Metadata["factor_type"] != "webauthn" {
		t.Fatalf("existing metadata lost: %v", identity.Metadata)
	}
	if identity.Metadata[identityUserIDKey] != "42" {
		t.Fatalf("user ID missing: %v", identity.Metadata)
	}
}

func TestMarkTokenSubject_NilIdentity(t *testing.T) {
	if got := MarkTokenSubject(nil, 42); got != nil {
		t.Fatalf("MarkTokenSubject(nil) = %+v, want nil", got)
	}
}

func TestSubjectIDKindOf(t *testing.T) {
	cases := map[string]interfaces.SubjectIDKind{
		"alice@example.com": interfaces.SubjectIDEmail,
		"admin":             interfaces.SubjectIDUsername,
		"12345":             interfaces.SubjectIDUsername,
	}
	for name, want := range cases {
		if got := SubjectIDKindOf(name); got != want {
			t.Fatalf("SubjectIDKindOf(%q) = %v, want %v", name, got, want)
		}
	}
}

// TestIsSystemSubject_OnlySystemTenantID pins #348: only a subject bound to
// the system tenant id is system; an empty tenant is not, although
// qrdbsdk.IsSystemTenant("") is true.
func TestIsSystemSubject_OnlySystemTenantID(t *testing.T) {
	system := interfaces.Subject{ID: "root", TenantID: qrdbsdk.SystemTenantID, Authenticated: true}
	if !IsSystemSubject(system) {
		t.Fatal("IsSystemSubject(system tenant subject) = false, want true")
	}
}

func TestIsSystemSubject_EmptyAndTenantAreNotSystem(t *testing.T) {
	cases := map[string]string{
		"empty tenant": "",
		"tenant":       "acme",
	}
	for name, tenantID := range cases {
		subject := interfaces.Subject{ID: "alice", TenantID: tenantID, Authenticated: true}
		if IsSystemSubject(subject) {
			t.Fatalf("%s: IsSystemSubject(TenantID=%q) = true, want false", name, tenantID)
		}
	}
}
