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
	"testing"
)

// TestUserInfoSubject_PrefersTheImmutableUIDClaim: when the token carries uid,
// it names the record directly and no string matching happens at all.
func TestUserInfoSubject_PrefersTheImmutableUIDClaim(t *testing.T) {
	provider, storage := newCeremonyProvider(t, nil)
	ctx := context.Background()

	if err := storage.CreateUser(ctx, &User{
		ID: "2001", TenantID: burninTenant,
		Name: "someone", Email: "someone@automatethethings.com", Lifecycle: LifecycleActive,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	got, err := provider.userInfoSubject(ctx, &Claims{
		TenantID: burninTenant,
		UserID:   "2001",
		// A subject that would resolve to nothing on its own.
		Subject: "stale-name-from-before-a-rename",
	})
	if err != nil {
		t.Fatalf("userInfoSubject: %v", err)
	}
	if got.ID != "2001" {
		t.Fatalf("resolved %q, want the uid claim's %q", got.ID, "2001")
	}
}

// TestUserInfoSubject_RequiresTheUIDClaim pins what replaced the subject-string
// fallback: a token with no uid resolves to no user at all.
//
// The fallback tried the subject as a user id and then as a USERNAME. Deleting
// it closed more than the pre-#359 population it was written for — see the
// service-principal case below, which the UUID guard that used to sit there did
// not cover.
func TestUserInfoSubject_RequiresTheUIDClaim(t *testing.T) {
	provider, storage := newCeremonyProvider(t, nil)
	ctx := context.Background()

	if err := storage.CreateUser(ctx, &User{
		ID: "4001", TenantID: burninTenant,
		Username: "someone", Name: "Someone", Email: "someone@automatethethings.com",
		Lifecycle: LifecycleActive,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	for _, subject := range []string{
		"someone",                              // the account's username
		"4001",                                 // the account's id
		"someone@automatethethings.com",        // the account's email
		"3f2504e0-4f89-11d3-9a0c-0305e82c3301", // a pre-#359 UUID subject
	} {
		got, err := provider.userInfoSubject(ctx, &Claims{TenantID: burninTenant, Subject: subject})
		if err == nil {
			t.Fatalf("subject %q resolved to %v without a uid claim; a token that does not name a record must not have one guessed for it",
				subject, got)
		}
	}
}

// TestUserInfoSubject_ServicePrincipalGetsNoUserProfile is the case the UUID
// guard did not cover, and the reason removing that guard without removing the
// fallback would have been the wrong half to delete.
//
// A client_credentials token carries no uid and a subject that is the client id
// — "app_<tenant>_<suffix>". That is inside the username charset, so a user who
// takes it as their username used to have this endpoint answer the CLIENT's
// userinfo request with their own profile: email, roles, groups and attributes,
// under the token's own sub. A UUID-shaped-subject guard never saw it, because
// a client id is not UUID-shaped.
func TestUserInfoSubject_ServicePrincipalGetsNoUserProfile(t *testing.T) {
	provider, storage := newCeremonyProvider(t, nil)
	ctx := context.Background()

	const clientID = "app_burnin_AbCd1234"

	// A user whose username is exactly the client id.
	if err := storage.CreateUser(ctx, &User{
		ID: "5001", TenantID: burninTenant,
		Username: clientID, Name: "Collider", Email: "collider@automatethethings.com",
		Lifecycle: LifecycleActive,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	got, err := provider.userInfoSubject(ctx, &Claims{
		TenantID:      burninTenant,
		Subject:       clientID,
		PrincipalType: PrincipalTypeService,
		ClientID:      clientID,
	})
	if err == nil {
		t.Fatalf("a service token resolved a user profile (%v); a client principal has no user by construction", got)
	}
}

// TestPrincipalLive_NeitherClaimIsDenied pins the liveness half of the same
// deletion. A token carrying neither uid nor ptyp predates the claims contract;
// the path that used to guess its liveness from the subject string is gone, and
// an unrecognised token is denied rather than resolved.
func TestPrincipalLive_NeitherClaimIsDenied(t *testing.T) {
	provider, storage := newCeremonyProvider(t, nil)
	ctx := context.Background()

	// Seed an ENABLED account whose username is the subject: under the deleted
	// path this token would have been read as live.
	if err := storage.CreateUser(ctx, &User{
		ID: "6001", TenantID: burninTenant,
		Username: "legacy-subject", Name: "Legacy", Email: "legacy@automatethethings.com",
		Lifecycle: LifecycleActive,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	live, err := provider.principalLive(ctx, burninTenant, "legacy-subject", "", "")
	if err != nil {
		t.Fatalf("principalLive: %v", err)
	}
	if live {
		t.Fatal("a token carrying neither uid nor ptyp must not be live")
	}
}

// TestPrincipalLive_StillHonoursBothClaims proves the denial above is specific:
// the two claims a current mint sets still decide liveness.
func TestPrincipalLive_StillHonoursBothClaims(t *testing.T) {
	provider, storage := newCeremonyProvider(t, nil)
	ctx := context.Background()

	if err := storage.CreateUser(ctx, &User{
		ID: "6002", TenantID: burninTenant,
		Username: "active-user", Name: "Active", Email: "active@automatethethings.com",
		Lifecycle: LifecycleActive,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	live, err := provider.principalLive(ctx, burninTenant, "", "6002", "")
	if err != nil {
		t.Fatalf("principalLive (uid): %v", err)
	}
	if !live {
		t.Fatal("a uid naming an active account must be live")
	}

	live, err = provider.principalLive(ctx, burninTenant, "app_burnin_x", "", PrincipalTypeService)
	if err != nil {
		t.Fatalf("principalLive (service): %v", err)
	}
	if !live {
		t.Fatal("a service principal must be live")
	}
}

// TestUserInfo_RefusesAnAccountThatMayNotAuthenticate: a bearer outliving its
// subject's right to use it is what the lifecycle exists to catch, and
// revocation cannot retroactively un-issue one.
func TestUserInfo_RefusesAnAccountThatMayNotAuthenticate(t *testing.T) {
	for _, state := range []Lifecycle{LifecycleDisabled, LifecycleSuspended, LifecycleLocked, LifecycleDeleting} {
		t.Run(string(state), func(t *testing.T) {
			provider, storage := newCeremonyProvider(t, nil)
			ctx := context.Background()

			if err := storage.CreateUser(ctx, &User{
				ID: "4001", TenantID: burninTenant,
				Name: "shut-out", Email: "shut-out@automatethethings.com",
				Lifecycle: state,
			}); err != nil {
				t.Fatalf("seed: %v", err)
			}

			user, err := provider.userInfoSubject(ctx, &Claims{TenantID: burninTenant, UserID: "4001"})
			if err != nil {
				t.Fatalf("userInfoSubject: %v", err)
			}
			if userEnabled(user) {
				t.Fatalf("%q must not be treated as enabled", state)
			}
		})
	}
}
