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
	"testing"

	qrdbrole "github.com/jeremyhahn/go-qrdb/pkg/role"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// =============================================================================
// A client error must not be reported as a server fault.
//
// mapQRDBRoleError ends in ErrInternal, so every qrdb role error it does not
// name becomes a 500. Three of them are things a caller did, not things that
// broke: a duplicate caller-supplied id (#287), an attempt to modify a built-in
// role, and a create that takes a reserved name. Each told the caller the
// server had failed, which is both wrong and unactionable — a 500 invites a
// retry that cannot succeed.
//
// The codes are chosen per condition rather than copied from the neighbouring
// branch; see the sentinels in errors.go for why each is what it is.
// =============================================================================

func TestMapQRDBRoleError_ClientErrorsAreNotInternal(t *testing.T) {
	for _, tc := range []struct {
		name     string
		op       string
		src      error
		wantCode dberrors.ErrorCode
		wantMsg  string
		why      string
	}{
		{
			name:     "duplicate caller-supplied role id",
			op:       "create",
			src:      &qrdbrole.RoleIDExistsError{ID: "role-1", TenantID: "acme"},
			wantCode: dberrors.ErrAlreadyExists,
			wantMsg:  "role id already exists",
			why:      "a conflict the caller can resolve, not a server failure (#287)",
		},
		{
			name:     "renaming a built-in role",
			op:       "update",
			src:      &qrdbrole.SystemRoleImmutableError{ID: "admin", Op: "renamed"},
			wantCode: dberrors.ErrForbidden,
			wantMsg:  "system roles cannot be modified or deleted",
			why:      "refused for every caller at all times, so there is no conflict to resolve",
		},
		{
			name:     "deleting a built-in role",
			op:       "delete",
			src:      &qrdbrole.SystemRoleImmutableError{ID: "admin", Op: "deleted"},
			wantCode: dberrors.ErrForbidden,
			wantMsg:  "system roles cannot be modified or deleted",
			why:      "delete reaches the same mapper as update and must classify the same",
		},
		{
			name:     "creating a role with a reserved name",
			op:       "create",
			src:      &qrdbrole.ReservedRoleNameError{Name: "Admin", TenantID: "acme", Detail: "reserved"},
			wantCode: dberrors.ErrInvalidInput,
			wantMsg:  "role name is reserved for a system role",
			why:      "refused by policy rather than by an existing row, so it is a value problem",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := mapQRDBRoleError(tc.op, tc.src)
			require.Error(t, err)

			var qbErr *dberrors.QBError
			require.ErrorAs(t, err, &qbErr)

			// The status assertion is the one the issue is about, and it is
			// stated before anything else so a regression fails HERE rather
			// than on a message or a wrapping detail.
			assert.NotEqual(t, dberrors.ErrInternal, qbErr.Code,
				"a client error is reported as a server fault: %s", tc.why)
			assert.Equal(t, tc.wantCode, qbErr.Code, tc.why)
			assert.Contains(t, err.Error(), tc.wantMsg,
				"the message must say which condition this is, not just that something conflicted")
		})
	}
}

// TestMapQRDBRoleError_DuplicateIDIsDistinctFromDuplicateName keeps the two
// conflicts apart.
//
// Both are ErrAlreadyExists because both are genuine conflicts at the same
// status, but they are different conditions with different remedies: a
// duplicate name means choose another name, a duplicate id means the caller
// collided with an existing role or is retrying a create. Collapsing them would
// tell an operator "role already exists" when the name they picked is free.
func TestMapQRDBRoleError_DuplicateIDIsDistinctFromDuplicateName(t *testing.T) {
	byID := mapQRDBRoleError("create", &qrdbrole.RoleIDExistsError{ID: "role-1", TenantID: "acme"})
	byName := mapQRDBRoleError("create", &qrdbrole.RoleNameExistsError{Name: "auditor", TenantID: "acme"})

	var idErr, nameErr *dberrors.QBError
	require.ErrorAs(t, byID, &idErr)
	require.ErrorAs(t, byName, &nameErr)

	assert.Equal(t, dberrors.ErrAlreadyExists, idErr.Code, "both are conflicts")
	assert.Equal(t, dberrors.ErrAlreadyExists, nameErr.Code, "both are conflicts")
	assert.NotEqual(t, byID.Error(), byName.Error(),
		"the two conflicts are indistinguishable to a caller, so the wrong remedy looks correct")
}

// TestMapQRDBRoleError_UnknownErrorsStayInternal is the control. Narrowing what
// counts as a server fault must not empty the category: an error this package
// does not recognise is still internal, because pretending to classify it would
// be worse than admitting it is unexpected.
func TestMapQRDBRoleError_UnknownErrorsStayInternal(t *testing.T) {
	err := mapQRDBRoleError("create", assert.AnError)

	var qbErr *dberrors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, dberrors.ErrInternal, qbErr.Code)
	assert.Equal(t, "iam.role.qrdb.create", qbErr.Op)
	assert.ErrorIs(t, err, assert.AnError, "the original error must stay in the chain")
}
