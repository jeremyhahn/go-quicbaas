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
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// breakAuditLogIDEntropy fails the audit-ID CSPRNG for the rest of the test
// and returns the error it fails with, so a caller can assert the cause
// survives wrapping.
func breakAuditLogIDEntropy(t *testing.T) error {
	t.Helper()

	broken := errors.New("rng unavailable")
	original := auditLogIDEntropy
	auditLogIDEntropy = func(int) ([]byte, error) { return nil, broken }
	t.Cleanup(func() { auditLogIDEntropy = original })
	return broken
}

func TestGenerateAuditLogID_RefusesWhenTheCSPRNGFails(t *testing.T) {
	broken := breakAuditLogIDEntropy(t)

	id, err := generateAuditLogID()

	var idErr *AuditLogIDGenerationError
	require.ErrorAs(t, err, &idErr)
	assert.ErrorIs(t, err, broken)
	assert.Empty(t, id, "no fallback ID may be handed out")
}

// adminAuditFixture is an AdminManager holding one assignment and one active
// impersonation session, so every audited operation has something to act on.
type adminAuditFixture struct {
	manager      *AdminManager
	auditLogger  *mockAuditLogger
	assignmentID string
	sessionID    string
}

func newAdminAuditFixture(t *testing.T) *adminAuditFixture {
	t.Helper()

	ctx := context.Background()
	manager, auditLogger := createTestAdminManager(t)

	assignment, err := manager.CreateAssignment(ctx, CreateAdminAssignmentOptions{
		UserID:      "admin_user",
		AdminRole:   AdminRoleSuperAdmin,
		TenantIDs:   []string{"tenant1"},
		Permissions: []AdminPermission{PermUserImpersonate},
		GrantedBy:   "system",
	})
	require.NoError(t, err)

	session, err := manager.StartImpersonation(ctxWithActor("admin_user"), &StartImpersonationOptions{
		TargetUserID: "target_user",
		TenantID:     "tenant1",
		Reason:       "audit id test",
		Duration:     time.Hour,
	})
	require.NoError(t, err)

	auditLogger.events = nil
	return &adminAuditFixture{
		manager:      manager,
		auditLogger:  auditLogger,
		assignmentID: assignment.ID,
		sessionID:    session.ID,
	}
}

// TestAdminManager_AuditedOperationsFailClosedWithoutAuditID pins #450: when
// no audit event ID can be minted, each audited operation returns
// AuditLogIDGenerationError, writes no audit record, and leaves the state it
// would have changed untouched -- an unaudited admin action is worse than a
// refused one.
func TestAdminManager_AuditedOperationsFailClosedWithoutAuditID(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name string
		run  func(f *adminAuditFixture) error
		// unchanged asserts the operation's own effect did not happen.
		unchanged func(t *testing.T, f *adminAuditFixture)
	}{
		{
			name: "CreateAssignment",
			run: func(f *adminAuditFixture) error {
				_, err := f.manager.CreateAssignment(ctx, CreateAdminAssignmentOptions{
					UserID:    "second_admin",
					AdminRole: AdminRoleSuperAdmin,
					GrantedBy: "system",
				})
				return err
			},
			unchanged: func(t *testing.T, f *adminAuditFixture) {
				got, err := f.manager.ListAssignments(ctx, ListAdminAssignmentsOptions{UserID: "second_admin"})
				require.NoError(t, err)
				assert.Empty(t, got, "the assignment was stored without its audit record")
			},
		},
		{
			name: "UpdateAssignment",
			run: func(f *adminAuditFixture) error {
				inactive := false
				_, err := f.manager.UpdateAssignment(ctx, f.assignmentID, UpdateAdminAssignmentOptions{Active: &inactive})
				return err
			},
			unchanged: func(t *testing.T, f *adminAuditFixture) {
				got, err := f.manager.GetAssignment(ctx, f.assignmentID)
				require.NoError(t, err)
				assert.True(t, got.Active, "the update was stored without its audit record")
			},
		},
		{
			name: "DeleteAssignment",
			run: func(f *adminAuditFixture) error {
				return f.manager.DeleteAssignment(ctx, f.assignmentID)
			},
			unchanged: func(t *testing.T, f *adminAuditFixture) {
				_, err := f.manager.GetAssignment(ctx, f.assignmentID)
				assert.NoError(t, err, "the delete happened without its audit record")
			},
		},
		{
			name: "StartImpersonation",
			run: func(f *adminAuditFixture) error {
				_, err := f.manager.StartImpersonation(ctxWithActor("admin_user"), &StartImpersonationOptions{
					TargetUserID: "other_user",
					TenantID:     "tenant1",
					Reason:       "audit id test",
				})
				return err
			},
			unchanged: func(t *testing.T, f *adminAuditFixture) {
				got, err := f.manager.GetActiveImpersonations(ctx, "admin_user")
				require.NoError(t, err)
				assert.Len(t, got, 1, "a session was started without its audit record")
			},
		},
		{
			name: "EndImpersonation",
			run: func(f *adminAuditFixture) error {
				return f.manager.EndImpersonation(ctx, f.sessionID)
			},
			unchanged: func(t *testing.T, f *adminAuditFixture) {
				got, err := f.manager.GetImpersonationSession(ctx, f.sessionID)
				require.NoError(t, err)
				assert.Nil(t, got.EndedAt, "the session was ended without its audit record")
			},
		},
		{
			name: "RecordImpersonatedOperation",
			run: func(f *adminAuditFixture) error {
				return f.manager.RecordImpersonatedOperation(ctx, f.sessionID, "document.read", nil, AuditOutcomeSuccess, nil)
			},
			unchanged: func(t *testing.T, f *adminAuditFixture) {
				got, err := f.manager.GetImpersonationSession(ctx, f.sessionID)
				require.NoError(t, err)
				assert.Zero(t, got.OperationCount, "the operation was counted without its audit record")
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newAdminAuditFixture(t)
			broken := breakAuditLogIDEntropy(t)

			err := tc.run(f)

			var idErr *AuditLogIDGenerationError
			require.ErrorAs(t, err, &idErr)
			assert.ErrorIs(t, err, broken)
			assert.Empty(t, f.auditLogger.events, "an audit record was written without a unique ID")
			tc.unchanged(t, f)
		})
	}
}
