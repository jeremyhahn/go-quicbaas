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

// TestIdentityStore_RoleNameChecksAreCoded covers the inline role-name checks
// in CreateRole and UpdateRole, which do not go through validateRoleName.
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
