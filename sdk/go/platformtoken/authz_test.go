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

package platformtoken

import (
	"context"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// TestAuthorizeProjectsUsername verifies the name claim is surfaced as
// AuthorizedContext.Username so resource servers can match principals by
// username in addition to subject id.
func TestAuthorizeProjectsUsername(t *testing.T) {
	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: "user-123"},
		TenantID:         "tenant-a",
		SCP:              []string{"keys:get"},
		Roles:            []string{"reader"},
		Name:             "alice",
	}

	ac := Authorize(claims)

	if ac.Subject != "user-123" {
		t.Fatalf("Subject = %q, want %q", ac.Subject, "user-123")
	}
	if ac.Username != "alice" {
		t.Fatalf("Username = %q, want %q", ac.Username, "alice")
	}
	if ac.TenantID != "tenant-a" {
		t.Fatalf("TenantID = %q, want %q", ac.TenantID, "tenant-a")
	}
}

// The verified uid claim is projected so a resource server resolves a user's
// account by it rather than by sub; a token without it projects none.
func TestAuthorizeProjectsUserID(t *testing.T) {
	withUID := Authorize(&Claims{RegisteredClaims: jwt.RegisteredClaims{Subject: "12345"}, UserID: 67890})
	if withUID.Subject != "12345" || withUID.UserID != 67890 {
		t.Fatalf("Authorize = %+v, want Subject 12345 and UserID 67890", withUID)
	}
	if service := Authorize(&Claims{RegisteredClaims: jwt.RegisteredClaims{Subject: "svc"}}); service.UserID != 0 {
		t.Fatalf("UserID = %d, want 0 without a uid claim", service.UserID)
	}
}

// TestAuthorizeUsernameEmptyWhenNoName verifies a token without a name claim
// yields an empty Username rather than a spurious value.
func TestAuthorizeUsernameEmptyWhenNoName(t *testing.T) {
	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: "svc-1"},
	}

	ac := Authorize(claims)

	if ac.Username != "" {
		t.Fatalf("Username = %q, want empty", ac.Username)
	}
}

// TestHasScopeWildcardSemantics verifies HasScope honors the same whole-segment
// "*" wildcard grants as qrdb's RBAC authorizer: a resource wildcard, an action
// wildcard, and the full wildcard each cover a matching concrete scope, while a
// wildcard never over-grants an unrelated scope. This is the offline mirror of
// the live qrdb PDP so a token carrying admin's resolved "orchestrator:*" grant
// authorizes a concrete "orchestrator:status" check.
func TestHasScopeWildcardSemantics(t *testing.T) {
	tests := []struct {
		name    string
		granted []string
		want    string
		ok      bool
	}{
		{
			name:    "resource wildcard grants matching concrete",
			granted: []string{"orchestrator:*"},
			want:    "orchestrator:status",
			ok:      true,
		},
		{
			name:    "resource wildcard does not grant other resource",
			granted: []string{"orchestrator:*"},
			want:    "keys:get",
			ok:      false,
		},
		{
			name:    "full wildcard grants any concrete scope",
			granted: []string{"*:*"},
			want:    "orchestrator:status",
			ok:      true,
		},
		{
			name:    "full wildcard grants unrelated concrete scope",
			granted: []string{"*:*"},
			want:    "keys:get",
			ok:      true,
		},
		{
			name:    "action wildcard grants matching action across resources",
			granted: []string{"*:status"},
			want:    "orchestrator:status",
			ok:      true,
		},
		{
			name:    "action wildcard does not grant different action",
			granted: []string{"*:status"},
			want:    "orchestrator:start",
			ok:      false,
		},
		{
			name:    "exact concrete scope still grants itself",
			granted: []string{"orchestrator:status"},
			want:    "orchestrator:status",
			ok:      true,
		},
		{
			name:    "exact concrete scope does not grant unrelated concrete",
			granted: []string{"orchestrator:status"},
			want:    "keys:get",
			ok:      false,
		},
		{
			name:    "narrow concrete grant denies unrelated wildcard-shaped request",
			granted: []string{"keys:get"},
			want:    "orchestrator:status",
			ok:      false,
		},
		{
			name:    "bare star grant does not satisfy concrete (qrdb honors only *:*)",
			granted: []string{"*"},
			want:    "orchestrator:status",
			ok:      false,
		},
		{
			name:    "multiple grants with one covering wildcard",
			granted: []string{"keys:get", "orchestrator:*"},
			want:    "orchestrator:status",
			ok:      true,
		},
		{
			name:    "empty grant set denies",
			granted: nil,
			want:    "orchestrator:status",
			ok:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ac := &AuthorizedContext{Scopes: tt.granted}
			if got := ac.HasScope(tt.want); got != tt.ok {
				t.Fatalf("HasScope(%q) with grants %v = %v, want %v",
					tt.want, tt.granted, got, tt.ok)
			}
		})
	}
}

// TestHasScopeMatchesQRDBPermissionGranted asserts the platformtoken matcher
// produces the same allow/deny verdict as qrdb's exported permission split
// helpers describe, across the canonical wildcard forms. It is the parity guard
// keeping the offline token check aligned with the qrdb RBAC vocabulary.
func TestHasScopeMatchesQRDBPermissionGranted(t *testing.T) {
	// Each granted form is the qrdb-canonical wildcard shape; the want column
	// encodes whether it covers the concrete "orchestrator:status".
	cases := []struct {
		grant string
		ok    bool
	}{
		{"orchestrator:status", true}, // exact
		{"orchestrator:*", true},      // resource wildcard
		{"*:status", true},            // action wildcard
		{"*:*", true},                 // full wildcard
		{"orchestrator:start", false}, // different action
		{"keys:status", false},        // different resource
		{"*:start", false},            // action wildcard, wrong action
		{"keys:*", false},             // resource wildcard, wrong resource
	}

	const required = "orchestrator:status"
	for _, c := range cases {
		ac := &AuthorizedContext{Scopes: []string{c.grant}}
		if got := ac.HasScope(required); got != c.ok {
			t.Fatalf("grant %q covers %q = %v, want %v", c.grant, required, got, c.ok)
		}
	}
}

// TestRequireScopeWildcardGrant verifies RequireScope returns nil when a
// covering wildcard grant is present, mirroring the admin path that previously
// 403'd on orchestrator routes.
func TestRequireScopeWildcardGrant(t *testing.T) {
	ac := &AuthorizedContext{Scopes: []string{"orchestrator:*"}}
	if err := ac.RequireScope("orchestrator:status"); err != nil {
		t.Fatalf("RequireScope = %v, want nil (wildcard should grant)", err)
	}
}

// TestRequireScopeDeniedNamesConcreteScope verifies a denial returns a
// ScopeDeniedError naming the concrete REQUIRED scope (not a wildcard), so 403
// diagnostics stay meaningful.
func TestRequireScopeDeniedNamesConcreteScope(t *testing.T) {
	ac := &AuthorizedContext{Scopes: []string{"keys:get"}}
	err := ac.RequireScope("orchestrator:status")
	if err == nil {
		t.Fatal("RequireScope = nil, want ScopeDeniedError")
	}
	denied, ok := err.(*ScopeDeniedError)
	if !ok {
		t.Fatalf("error type = %T, want *ScopeDeniedError", err)
	}
	if denied.Required != "orchestrator:status" {
		t.Fatalf("Required = %q, want %q", denied.Required, "orchestrator:status")
	}
}

// TestEndToEnd_AdminWildcardTokenAuthorizesOrchestratorScope is the functional
// proof for the orchestrator-403 fix: it mints a real ES256-signed platform
// token carrying admin's qrdb-RESOLVED wildcard grant ("orchestrator:*", as
// emitted by oidc_tokens.go from qrdb RBAC ListPermissions), runs the full
// Validator.Authorize path (validate signature/claims -> project ->
// RequireScope), and asserts the concrete "orchestrator:status" check that the
// REST default-deny PEP enforces now passes. Before the fix the literal matcher
// returned ScopeDeniedError -> 403 for exactly this token.
func TestEndToEnd_AdminWildcardTokenAuthorizesOrchestratorScope(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	key := newIssuerKey(t, testKID)
	// Offline mode isolates the scope decision (the part this fix touches): the
	// matcher runs without a liveness network call. The scope check always runs
	// offline and BEFORE any liveness call regardless of mode.
	v := newAuthzValidator(t, key, now, Options{
		ModeResolver: func(string) AuthzMode { return AuthzModeOffline },
	})

	adminToken := key.sign(t, tokenClaims{
		iss: testIssuer, aud: []string{testAudience}, sub: "admin",
		tid: "system", scp: []string{"orchestrator:*"},
		exp: now.Add(10 * time.Minute), iat: now,
	})

	// The exact scope the REST orchestrator PEP requires.
	ac, err := v.Authorize(context.Background(), adminToken, "orchestrator:status", "", "", "")
	if err != nil {
		t.Fatalf("Authorize(orchestrator:status) for admin orchestrator:* token = %v, want nil (no 403)", err)
	}
	if ac.Subject != "admin" || ac.TenantID != "system" {
		t.Fatalf("projected context = %+v, want admin/system", ac)
	}

	// Negative parity: a narrow token must still 403 on the same route.
	narrowToken := key.sign(t, tokenClaims{
		iss: testIssuer, aud: []string{testAudience}, sub: "reader",
		tid: "system", scp: []string{"keys:get"},
		exp: now.Add(10 * time.Minute), iat: now,
	})
	if _, err := v.Authorize(context.Background(), narrowToken, "orchestrator:status", "", "", ""); err == nil {
		t.Fatal("Authorize(orchestrator:status) for keys:get-only token = nil, want ScopeDeniedError (still 403)")
	}
}

// TestHasAnyScopeWildcardInherits verifies the wildcard semantics flow through
// HasAnyScope/RequireAnyScope without separate wildcard logic.
func TestHasAnyScopeWildcardInherits(t *testing.T) {
	ac := &AuthorizedContext{Scopes: []string{"orchestrator:*"}}
	if !ac.HasAnyScope("keys:get", "orchestrator:status") {
		t.Fatal("HasAnyScope should match via covering wildcard")
	}
	if err := ac.RequireAnyScope("keys:get", "orchestrator:status"); err != nil {
		t.Fatalf("RequireAnyScope = %v, want nil", err)
	}
	if ac.HasAnyScope("keys:get", "secrets:read") {
		t.Fatal("HasAnyScope should not match unrelated concretes under orchestrator:* grant")
	}
}
