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

package service

import (
	"context"
	stderrors "errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// callerAccounts is an in-memory identity store holding the accounts the
// resolver tests look up.
type callerAccounts struct {
	store *iam.IdentityStore
	admin *iam.User // tenant-a, ID 12345, username "admin"
	// numeric is in tenant-a and its username is admin's user ID, so reading
	// the username as an ID would select admin instead.
	numeric *iam.User // tenant-a, ID 67890, username "12345"
	other   *iam.User // tenant-b, ID 24680, username "bob"
}

func newCallerAccounts(t *testing.T) *callerAccounts {
	t.Helper()
	mem := iam.NewMemoryStorage()
	store := iam.NewIdentityStore(iam.IdentityStoreOptions{UserStorage: mem, RoleStorage: mem})
	create := func(tenantID, id, username, email string) *iam.User {
		user, err := store.CreateUser(context.Background(), tenantID, iam.CreateUserOptions{ID: id, Email: email, Username: username})
		if err != nil {
			t.Fatalf("CreateUser(%s/%s): %v", tenantID, username, err)
		}
		return user
	}
	return &callerAccounts{
		store:   store,
		admin:   create("tenant-a", "12345", "admin", "admin@example.com"),
		numeric: create("tenant-a", "67890", "12345", "numeric@example.com"),
		other:   create("tenant-b", "24680", "bob", "bob@example.com"),
	}
}

func qrdbID(t *testing.T, user *iam.User) uint64 {
	t.Helper()
	id, err := user.QRDBID()
	if err != nil {
		t.Fatalf("QRDBID(%s): %v", user.ID, err)
	}
	return id
}

// An mTLS administrator arrives as its username and resolves to its account.
func TestResolveCallerAccount_MTLSUsername(t *testing.T) {
	accounts := newCallerAccounts(t)
	subject := interfaces.Subject{ID: "admin", TenantID: "tenant-a", IDKind: interfaces.SubjectIDUsername}

	got, err := resolveCallerAccount(context.Background(), accounts.store, subject, "test")
	if err != nil {
		t.Fatalf("resolveCallerAccount: %v", err)
	}
	if want := qrdbID(t, accounts.admin); got != want {
		t.Fatalf("resolved %d, want admin %d", got, want)
	}
}

// An all-digit username resolves to its own account, not to the user whose ID
// those digits spell.
func TestResolveCallerAccount_NumericUsernameIsNotAnID(t *testing.T) {
	accounts := newCallerAccounts(t)
	subject := interfaces.Subject{ID: "12345", TenantID: "tenant-a", IDKind: interfaces.SubjectIDUsername}

	got, err := resolveCallerAccount(context.Background(), accounts.store, subject, "test")
	if err != nil {
		t.Fatalf("resolveCallerAccount: %v", err)
	}
	if want := qrdbID(t, accounts.numeric); got != want {
		t.Fatalf("username %q resolved to %d, want its own account %d (admin is %d)",
			subject.ID, got, want, qrdbID(t, accounts.admin))
	}
}

// An in-process caller that names its account by ID resolves by ID.
func TestResolveCallerAccount_EmbeddedUserID(t *testing.T) {
	accounts := newCallerAccounts(t)
	subject := interfaces.Subject{ID: accounts.numeric.ID, TenantID: "tenant-a", IDKind: interfaces.SubjectIDUserID}

	got, err := resolveCallerAccount(context.Background(), accounts.store, subject, "test")
	if err != nil {
		t.Fatalf("resolveCallerAccount: %v", err)
	}
	if want := qrdbID(t, accounts.numeric); got != want {
		t.Fatalf("resolved %d, want %d", got, want)
	}
}

// A session token's verified uid names the account, whatever its sub spells:
// the numeric username "12345" is admin's ID, yet the session resolves to the
// account the uid names.
func TestResolveCallerAccount_TokenUserIDWinsOverSub(t *testing.T) {
	accounts := newCallerAccounts(t)
	subject := interfaces.Subject{
		ID:       "12345",
		TenantID: "tenant-a",
		IDKind:   interfaces.SubjectIDUsername,
		UserID:   accounts.numeric.ID,
	}

	got, err := resolveCallerAccount(context.Background(), accounts.store, subject, "test")
	if err != nil {
		t.Fatalf("resolveCallerAccount: %v", err)
	}
	if want := qrdbID(t, accounts.numeric); got != want {
		t.Fatalf("resolved %d, want the uid's account %d (admin is %d)", got, want, qrdbID(t, accounts.admin))
	}
}

// A token without a uid whose sub is the login email resolves by email.
func TestResolveCallerAccount_EmailSubject(t *testing.T) {
	accounts := newCallerAccounts(t)
	subject := interfaces.Subject{ID: "numeric@example.com", TenantID: "tenant-a", IDKind: interfaces.SubjectIDEmail}

	got, err := resolveCallerAccount(context.Background(), accounts.store, subject, "test")
	if err != nil {
		t.Fatalf("resolveCallerAccount: %v", err)
	}
	if want := qrdbID(t, accounts.numeric); got != want {
		t.Fatalf("resolved %d, want %d", got, want)
	}
}

// Every subject that does not name an account in its own tenant is refused
// with the typed Forbidden caller refusal.
func TestResolveCallerAccount_Refusals(t *testing.T) {
	accounts := newCallerAccounts(t)

	cases := map[string]interfaces.Subject{
		"unknown username":        {ID: "nobody", TenantID: "tenant-a", IDKind: interfaces.SubjectIDUsername},
		"unknown user id":         {ID: "999999", TenantID: "tenant-a", IDKind: interfaces.SubjectIDUserID},
		"user id not numeric":     {ID: "admin", TenantID: "tenant-a", IDKind: interfaces.SubjectIDUserID},
		"cross-tenant username":   {ID: "bob", TenantID: "tenant-a", IDKind: interfaces.SubjectIDUsername},
		"cross-tenant user id":    {ID: accounts.other.ID, TenantID: "tenant-a", IDKind: interfaces.SubjectIDUserID},
		"kind unresolved":         {ID: "admin", TenantID: "tenant-a"},
		"no subject id":           {TenantID: "tenant-a", IDKind: interfaces.SubjectIDUsername},
		"username in wrong scope": {ID: "admin", TenantID: "tenant-b", IDKind: interfaces.SubjectIDUsername},
		"unknown token uid":       {ID: "admin", TenantID: "tenant-a", IDKind: interfaces.SubjectIDUsername, UserID: "999999"},
		"token uid not numeric":   {ID: "admin", TenantID: "tenant-a", IDKind: interfaces.SubjectIDUsername, UserID: "admin"},
		"cross-tenant token uid":  {ID: "bob", TenantID: "tenant-a", IDKind: interfaces.SubjectIDUsername, UserID: accounts.other.ID},
		"unknown email":           {ID: "nobody@example.com", TenantID: "tenant-a", IDKind: interfaces.SubjectIDEmail},
		"malformed email":         {ID: "not@an-email", TenantID: "tenant-a", IDKind: interfaces.SubjectIDEmail},
		"cross-tenant email":      {ID: "bob@example.com", TenantID: "tenant-a", IDKind: interfaces.SubjectIDEmail},
	}
	for name, subject := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := resolveCallerAccount(context.Background(), accounts.store, subject, "test")
			if !IsFIDO2CallerRefused(err) || !stderrors.Is(err, &errors.QBError{Code: errors.ErrForbidden}) {
				t.Fatalf("resolveCallerAccount = %v, want a forbidden caller refusal", err)
			}
		})
	}
}

func TestResolveCallerAccount_NoStore(t *testing.T) {
	subject := interfaces.Subject{ID: "admin", TenantID: "tenant-a", IDKind: interfaces.SubjectIDUsername}

	_, err := resolveCallerAccount(context.Background(), nil, subject, "test")
	if !stderrors.Is(err, &errors.QBError{Code: errors.ErrConfiguration}) {
		t.Fatalf("resolveCallerAccount = %v, want a configuration error", err)
	}
}
