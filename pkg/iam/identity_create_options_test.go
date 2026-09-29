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
	"errors"
	"testing"
)

// assertNoUserPersisted is the half of a validation test that a bare
// "did it return an error" assertion misses. An implementation that writes the
// user and then rejects the input returns the same error to the caller and
// leaves an account behind — the exact class #327 was about — so a validation
// test that only checks the error passes against it.
func assertNoUserPersisted(t *testing.T, mem *MemoryStorage, tenantID string) {
	t.Helper()
	users, err := mem.ListUsers(context.Background(), tenantID, ListOptions{})
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users) != 0 {
		t.Errorf("a rejected create left %d user(s) behind: %+v", len(users), users)
	}
}

// assertFieldValidationError checks the caller gets a typed validation error
// naming the offending field, not an opaque failure. The error is wrapped in
// the coded error transports map to 400, so this reaches through the chain.
func assertFieldValidationError(t *testing.T, err error, field string) {
	t.Helper()
	if err == nil {
		t.Fatal("want a validation error, got nil")
	}
	var verr *ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("want *ValidationError in the chain, got %T: %v", err, err)
	}
	if verr.Field != field {
		t.Errorf("ValidationError.Field = %q, want %q", verr.Field, field)
	}
}

func newCreateOptionsStore() (*IdentityStore, *MemoryStorage) {
	mem := NewMemoryStorage()
	return NewIdentityStore(IdentityStoreOptions{
		UserStorage: mem,
		RoleStorage: mem,
	}), mem
}

// TestCreateUser_HonoursAuthMethods proves the option reaches the record.
//
// It used to be accepted and discarded, so a caller that asked for PIV got a
// user with no auth methods at all and no indication why.
func TestCreateUser_HonoursAuthMethods(t *testing.T) {
	store, mem := newCreateOptionsStore()
	ctx := context.Background()

	user, err := store.CreateUser(ctx, "acme", CreateUserOptions{
		Email:       "alice@acme.com",
		Username:    "alice",
		AuthMethods: []string{AuthMethodPIV},
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if len(user.AuthMethods) != 1 || user.AuthMethods[0] != AuthMethodPIV {
		t.Errorf("returned AuthMethods = %v, want [%s]", user.AuthMethods, AuthMethodPIV)
	}

	stored, err := mem.GetUser(ctx, "acme", user.ID)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if len(stored.AuthMethods) != 1 || stored.AuthMethods[0] != AuthMethodPIV {
		t.Errorf("persisted AuthMethods = %v, want [%s]", stored.AuthMethods, AuthMethodPIV)
	}
}

// TestCreateUser_DefaultsAuthMethodsToWebAuthn proves the documented default is
// applied rather than merely described in a comment.
func TestCreateUser_DefaultsAuthMethodsToWebAuthn(t *testing.T) {
	store, _ := newCreateOptionsStore()

	user, err := store.CreateUser(context.Background(), "acme", CreateUserOptions{
		Email:    "alice@acme.com",
		Username: "alice",
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if len(user.AuthMethods) != 1 || user.AuthMethods[0] != AuthMethodWebAuthn {
		t.Errorf("AuthMethods = %v, want [%s]", user.AuthMethods, AuthMethodWebAuthn)
	}
}

// TestCreateUser_SetsInitialState proves a created user has a lifecycle state.
//
// Every user used to be written with an EMPTY state — not pending, not
// anything. That is not a lifecycle: PIVActivator refuses a user that is not
// pending, so an account created anywhere but REST could not be PIV-activated
// at all, and REST only worked because it issued a second write afterwards.
func TestCreateUser_SetsInitialState(t *testing.T) {
	store, mem := newCreateOptionsStore()
	ctx := context.Background()

	user, err := store.CreateUser(ctx, "acme", CreateUserOptions{
		Email:    "alice@acme.com",
		Username: "alice",
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if user.Lifecycle != LifecyclePending {
		t.Errorf("State = %q, want %q", user.Lifecycle, LifecyclePending)
	}

	stored, err := mem.GetUser(ctx, "acme", user.ID)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if stored.Lifecycle != LifecyclePending {
		t.Errorf("persisted State = %q, want %q", stored.Lifecycle, LifecyclePending)
	}
}

// TestCreateUser_HonoursExplicitState proves a caller can say the account is
// not awaiting an invite — first-boot's cert-enrolled admin and a federated
// user authenticated by their IdP are both active on arrival.
func TestCreateUser_HonoursExplicitState(t *testing.T) {
	store, _ := newCreateOptionsStore()

	user, err := store.CreateUser(context.Background(), "acme", CreateUserOptions{
		Email:     "alice@acme.com",
		Username:  "alice",
		Lifecycle: LifecycleActive,
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if user.Lifecycle != LifecycleActive {
		t.Errorf("State = %q, want %q", user.Lifecycle, LifecycleActive)
	}
}

// TestCreateUser_RejectsUnsupportedAuthMethods covers the input path this
// change opened. AuthMethods was accepted and discarded before, so an
// unsupported value was inert and nothing had to validate it; persisting it
// makes it live. An account whose auth method is "password" can never be
// activated — ActivateFIDO2 and PIVActivator are the only mechanisms — so the
// invite it is issued advertises something that does not exist.
func TestCreateUser_RejectsUnsupportedAuthMethods(t *testing.T) {
	cases := []struct {
		name    string
		methods []string
	}{
		{"unsupported method", []string{"password"}},
		{"unsupported among supported", []string{AuthMethodWebAuthn, "password"}},
		{"blank entry", []string{""}},
		{"whitespace-only entry", []string{"   "}},
		{"case variant is not the constant", []string{"WebAuthn"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store, mem := newCreateOptionsStore()

			user, err := store.CreateUser(context.Background(), "acme", CreateUserOptions{
				Email:       "alice@acme.com",
				Username:    "alice",
				AuthMethods: tc.methods,
			})
			assertFieldValidationError(t, err, "auth_methods")
			if user != nil {
				t.Errorf("a rejected create returned a user: %+v", user)
			}
			assertNoUserPersisted(t, mem, "acme")
		})
	}
}

// TestCreateUser_RejectsUnsupportedState covers the second field this change
// made effective. State is not decorative: PIVActivator refuses a user that is
// not exactly `pending`, and the "may this user authenticate" check excludes
// only `disabled`, so a typo yields an account that can neither be activated
// nor be held out by the state it claims.
func TestCreateUser_RejectsUnsupportedState(t *testing.T) {
	store, mem := newCreateOptionsStore()

	user, err := store.CreateUser(context.Background(), "acme", CreateUserOptions{
		Email:     "alice@acme.com",
		Username:  "alice",
		Lifecycle: Lifecycle("actve"),
	})
	assertFieldValidationError(t, err, "state")
	if user != nil {
		t.Errorf("a rejected create returned a user: %+v", user)
	}
	assertNoUserPersisted(t, mem, "acme")
}

// TestCreateUser_NormalizesAuthMethods proves the accepted list is stored in a
// canonical shape: surrounding whitespace removed and a repeat dropped, so a
// record cannot claim the same credential kind twice.
func TestCreateUser_NormalizesAuthMethods(t *testing.T) {
	store, mem := newCreateOptionsStore()
	ctx := context.Background()

	user, err := store.CreateUser(ctx, "acme", CreateUserOptions{
		Email:       "alice@acme.com",
		Username:    "alice",
		AuthMethods: []string{" webauthn ", AuthMethodPIV, AuthMethodWebAuthn},
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	stored, err := mem.GetUser(ctx, "acme", user.ID)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	want := []string{AuthMethodWebAuthn, AuthMethodPIV}
	if len(stored.AuthMethods) != len(want) {
		t.Fatalf("persisted AuthMethods = %v, want %v", stored.AuthMethods, want)
	}
	for i, method := range want {
		if stored.AuthMethods[i] != method {
			t.Errorf("persisted AuthMethods[%d] = %q, want %q", i, stored.AuthMethods[i], method)
		}
	}
}

// TestUpdateUser_RejectsUnsupportedAuthMethods holds the update path to the
// same closed set. Validating only create would make the rule true of one door
// into the same record: the REST create handler writes the caller's
// auth_methods a second time through UpdateUser to attach the invite.
func TestUpdateUser_RejectsUnsupportedAuthMethods(t *testing.T) {
	store, mem := newCreateOptionsStore()
	ctx := context.Background()

	user, err := store.CreateUser(ctx, "acme", CreateUserOptions{
		Email:    "alice@acme.com",
		Username: "alice",
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	methods := []string{"password"}
	updated, err := store.UpdateUser(ctx, "acme", user.ID, UpdateUserOptions{
		AuthMethods: &methods,
	})
	assertFieldValidationError(t, err, "auth_methods")
	if updated != nil {
		t.Errorf("a rejected update returned a user: %+v", updated)
	}

	stored, err := mem.GetUser(ctx, "acme", user.ID)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if len(stored.AuthMethods) != 1 || stored.AuthMethods[0] != AuthMethodWebAuthn {
		t.Errorf("a rejected update changed the record: AuthMethods = %v", stored.AuthMethods)
	}
}

// TestCreateUser_EnabledDefaultsToNotSuspended is the one that matters most.
//
// Enabled is a POINTER so that saying nothing cannot disable anyone. As a
// plain bool it took the zero value from every caller that did not set it —
// including the REST create handler, whose `enabled` JSON field is absent on
// most requests — so honouring it naively would have suspended nearly every
// user the moment the field started being read.
func TestCreateUser_EnabledDefaultsToNotSuspended(t *testing.T) {
	store, _ := newCreateOptionsStore()

	user, err := store.CreateUser(context.Background(), "acme", CreateUserOptions{
		Email:    "alice@acme.com",
		Username: "alice",
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	// The account is PENDING, not disabled. Both are states in which it cannot
	// authenticate, and the difference is the whole point: pending is waiting
	// for its owner to activate, disabled is an administrator's decision. The
	// old model could not tell them apart, which is why saying nothing about
	// Enabled had to be checked for silently disabling people.
	if user.Lifecycle != LifecyclePending {
		t.Errorf("Lifecycle = %q, want pending for a user created without an explicit Enabled", user.Lifecycle)
	}
}

// TestCreateUser_HonoursExplicitEnabled proves an explicit false does suspend,
// mapping to Suspended exactly as UpdateUser does.
func TestCreateUser_HonoursExplicitEnabled(t *testing.T) {
	store, mem := newCreateOptionsStore()
	ctx := context.Background()

	disabled := false
	user, err := store.CreateUser(ctx, "acme", CreateUserOptions{
		Email:    "alice@acme.com",
		Username: "alice",
		Enabled:  &disabled,
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if user.Lifecycle != LifecycleDisabled {
		t.Errorf("Lifecycle = %q, want disabled when the caller said Enabled=false", user.Lifecycle)
	}

	stored, err := mem.GetUser(ctx, "acme", user.ID)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if stored.Lifecycle != LifecycleDisabled || stored.CanAuthenticate() {
		t.Errorf("the disabled state was not persisted: %q", stored.Lifecycle)
	}
}
