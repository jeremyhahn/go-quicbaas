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

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// assertCodedFieldValidationError proves err is a field validation error every
// transport can classify: the *ValidationError names the field, and the chain
// carries the ErrInvalidInput code the REST, gRPC and Unix mappers key on. A
// bare *ValidationError has no code and surfaced as 500 / Internal (#335).
func assertCodedFieldValidationError(t *testing.T, err error, field string) {
	t.Helper()
	assertFieldValidationError(t, err, field)
	var coded *dberrors.QBError
	if !errors.As(err, &coded) {
		t.Fatalf("want a coded error in the chain, got %T: %v", err, err)
	}
	if coded.Code != dberrors.ErrInvalidInput {
		t.Errorf("error code = %v, want ErrInvalidInput", coded.Code)
	}
}

func TestFieldValidators_ReturnCodedFieldErrors(t *testing.T) {
	tests := []struct {
		name     string
		field    string
		validate func() error
	}{
		{"empty email", "email", func() error { return validateEmail("") }},
		{"malformed email", "email", func() error { return validateEmail("not-an-email") }},
		{"empty username", "username", func() error { return validateUsername("") }},
		{"malformed username", "username", func() error { return validateUsername("a b") }},
		{"email-shaped display name", "name", func() error { return validateDisplayName("bob@example.com") }},
		{"empty tenant ID", "tenant_id", func() error { return validateTenantID("") }},
		{"empty user ID", "user_id", func() error { return validateUserID("") }},
		{"empty role ID", "role_id", func() error { return validateRoleID("") }},
		{"empty role name", "name", func() error { return validateRoleName("") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertCodedFieldValidationError(t, tt.validate(), tt.field)
		})
	}
}

func TestFieldValidators_AcceptValidInput(t *testing.T) {
	tests := []struct {
		name     string
		validate func() error
	}{
		{"email", func() error { return validateEmail("bob@example.com") }},
		{"username", func() error { return validateUsername("bob_smith") }},
		{"display name", func() error { return validateDisplayName("Bob Smith") }},
		{"tenant ID", func() error { return validateTenantID("acme") }},
		{"user ID", func() error { return validateUserID("u-1") }},
		{"role ID", func() error { return validateRoleID("r-1") }},
		{"role name", func() error { return validateRoleName("auditor") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.validate(); err != nil {
				t.Errorf("valid input refused: %v", err)
			}
		})
	}
}

// TestIdentityStore_RoleNameChecksAreCoded covers the role-name checks in
// CreateRole and UpdateRole.
func TestIdentityStore_RoleNameChecksAreCoded(t *testing.T) {
	mem := NewMemoryStorage()
	store := NewIdentityStore(IdentityStoreOptions{UserStorage: mem, RoleStorage: mem})
	ctx := context.Background()

	_, err := store.CreateRole(ctx, "acme", CreateRoleOptions{Name: ""})
	assertCodedFieldValidationError(t, err, "name")

	role, err := store.CreateRole(ctx, "acme", CreateRoleOptions{Name: "auditor"})
	if err != nil {
		t.Fatalf("CreateRole with a valid name failed: %v", err)
	}
	empty := ""
	_, err = store.UpdateRole(ctx, "acme", role.ID, UpdateRoleOptions{Name: &empty})
	assertCodedFieldValidationError(t, err, "name")
}

// TestCreateInvitedUser_InvalidInputBeatsMissingInviteStore: a malformed
// request is answered as malformed whatever the server's wiring. Checking the
// invite store first told the caller to retry later, and a retry against a
// wired server would only have been refused for the field it should have been
// told about the first time (#335).
func TestCreateInvitedUser_InvalidInputBeatsMissingInviteStore(t *testing.T) {
	tests := []struct {
		name  string
		opts  CreateUserOptions
		field string
	}{
		{"malformed email", CreateUserOptions{Email: "not-an-email", Username: "cyd"}, "email"},
		{"missing username", CreateUserOptions{Email: "cy@acme.com"}, "username"},
		{"email-shaped display name", CreateUserOptions{Email: "cy@acme.com", Username: "cyd", Name: "cy@acme.com"}, "name"},
		{"unsupported auth method", CreateUserOptions{Email: "cy@acme.com", Username: "cyd", AuthMethods: []string{"password"}}, "auth_methods"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mem := NewMemoryStorage()
			store := NewIdentityStore(IdentityStoreOptions{UserStorage: mem, RoleStorage: mem})

			_, _, _, err := store.CreateInvitedUser(context.Background(), "acme", tt.opts, nil, 0)
			if errors.Is(err, ErrInviteStoreUnavailable) {
				t.Fatalf("the invite store was checked before the request: %v", err)
			}
			assertCodedFieldValidationError(t, err, tt.field)
		})
	}
}

// TestIdentityStore_UpdateUserValidatesBeforeLookup: an update naming a user
// or role that does not exist, with a malformed field, is refused for the
// field. Looking the records up first answered not-found, so the caller fixed
// the id and was then refused for a field it could have fixed in the same go.
func TestIdentityStore_UpdateUserValidatesBeforeLookup(t *testing.T) {
	badEmail := "not-an-email"
	emailName := "kim@acme.test"
	badUsername := "a b"
	badLifecycle := Lifecycle("bogus")
	badMethods := []string{"password"}
	missingRole := []string{"no-such-role"}
	tests := []struct {
		name  string
		opts  UpdateUserOptions
		field string
	}{
		{"malformed email", UpdateUserOptions{Email: &badEmail, Roles: &missingRole}, "email"},
		{"email-shaped display name", UpdateUserOptions{Name: &emailName, Roles: &missingRole}, "name"},
		{"malformed username", UpdateUserOptions{Username: &badUsername, Roles: &missingRole}, "username"},
		{"unsupported lifecycle", UpdateUserOptions{Lifecycle: &badLifecycle, Roles: &missingRole}, "lifecycle"},
		{"unsupported auth method", UpdateUserOptions{AuthMethods: &badMethods, Roles: &missingRole}, "auth_methods"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mem := NewMemoryStorage()
			store := NewIdentityStore(IdentityStoreOptions{UserStorage: mem, RoleStorage: mem})

			_, err := store.UpdateUser(context.Background(), "acme", "no-such-user", tt.opts)
			assertCodedFieldValidationError(t, err, tt.field)
		})
	}
}

// TestIdentityStore_UpdateRoleValidatesBeforeLookup: an empty role name is
// refused for the name even when the role does not exist.
func TestIdentityStore_UpdateRoleValidatesBeforeLookup(t *testing.T) {
	mem := NewMemoryStorage()
	store := NewIdentityStore(IdentityStoreOptions{UserStorage: mem, RoleStorage: mem})
	empty := " "

	_, err := store.UpdateRole(context.Background(), "acme", "no-such-role", UpdateRoleOptions{Name: &empty})
	assertCodedFieldValidationError(t, err, "name")
}

// The exported validators need no store, so the service wrapper can judge a
// request before it resolves one (#335).
func TestValidateCreateUserOptions(t *testing.T) {
	valid := CreateUserOptions{Email: "sam@example.com", Username: "sam"}
	if err := ValidateCreateUserOptions("t-1", valid); err != nil {
		t.Fatalf("valid create: %v", err)
	}

	malformed := valid
	malformed.Email = "not-an-email"
	assertCodedFieldValidationError(t, ValidateCreateUserOptions("t-1", malformed), "email")
}

func TestValidateUpdateUserOptions(t *testing.T) {
	name := "Sam"
	if err := ValidateUpdateUserOptions(UpdateUserOptions{Name: &name}); err != nil {
		t.Fatalf("valid update: %v", err)
	}

	email := "not-an-email"
	assertCodedFieldValidationError(t, ValidateUpdateUserOptions(UpdateUserOptions{Email: &email}), "email")
}

func TestBatchValidate_ReturnsCodedFieldErrors(t *testing.T) {
	tests := []struct {
		name     string
		field    string
		validate func() error
	}{
		{"import request tenant", "tenant_id", (&BatchImportRequest{}).Validate},
		{"import options batch size", "batch_size", (&BatchImportOptions{BatchSize: -1}).Validate},
		{"import options parallelism", "parallelism", (&BatchImportOptions{Parallelism: -1}).Validate},
		{"export request tenant", "tenant_id", (&BatchExportRequest{}).Validate},
		{"operation ID", "id", (&BatchOperation{}).Validate},
		{"options rate limit", "rate_limit", (&BatchOptions{RateLimit: -1}).Validate},
		{"user item email", "email", (&UserImportItem{}).Validate},
		{"role item name", "name", (&RoleImportItem{}).Validate},
		{"policy item", "policy/name", (&PolicyImportItem{}).Validate},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertCodedFieldValidationError(t, tt.validate(), tt.field)
		})
	}
}

func TestBatchManager_ExportRequiresTenantWithCodedError(t *testing.T) {
	bm := NewBatchManager(BatchManagerOptions{})
	ctx := context.Background()

	_, err := bm.ExportUsers(ctx, "", ExportFilter{})
	assertCodedFieldValidationError(t, err, "tenant_id")
	_, err = bm.ImportUsers(ctx, "", nil, BatchImportOptions{})
	assertCodedFieldValidationError(t, err, "tenant_id")
}
