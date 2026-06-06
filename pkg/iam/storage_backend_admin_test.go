//go:build iam

// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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
	"time"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// -----------------------------------------------------------------------------
// BackendAdminStorage
// -----------------------------------------------------------------------------

func TestBackendAdminStorage_CreateGetAssignment(t *testing.T) {
	ctx := context.Background()
	store := NewBackendAdminStorage(newMockKVStore())

	a := &AdminAssignment{
		ID:        "assign-1",
		UserID:    "user-1",
		AdminRole: AdminRole("super"),
		Active:    true,
		GrantedAt: time.Now().UTC(),
	}

	require.NoError(t, store.CreateAssignment(ctx, a))

	got, err := store.GetAssignment(ctx, "assign-1")
	require.NoError(t, err)
	assert.Equal(t, "user-1", got.UserID)
	assert.True(t, got.Active)
}

func TestBackendAdminStorage_CreateAssignment_Errors(t *testing.T) {
	ctx := context.Background()
	store := NewBackendAdminStorage(newMockKVStore())

	// nil assignment
	if err := store.CreateAssignment(ctx, nil); err == nil {
		t.Fatal("expected error for nil assignment")
	}
	// empty ID
	if err := store.CreateAssignment(ctx, &AdminAssignment{}); err == nil {
		t.Fatal("expected error for empty ID")
	}
	// duplicate
	a := &AdminAssignment{ID: "dup", UserID: "u", Active: true}
	require.NoError(t, store.CreateAssignment(ctx, a))
	if err := store.CreateAssignment(ctx, a); err == nil {
		t.Fatal("expected duplicate error")
	}
}

func TestBackendAdminStorage_GetAssignment_Errors(t *testing.T) {
	ctx := context.Background()
	store := NewBackendAdminStorage(newMockKVStore())

	if _, err := store.GetAssignment(ctx, ""); err == nil {
		t.Fatal("expected error for empty id")
	}
	_, err := store.GetAssignment(ctx, "missing")
	require.ErrorIs(t, err, ErrAdminAssignmentNotFound)
}

func TestBackendAdminStorage_UpdateAssignment(t *testing.T) {
	ctx := context.Background()
	store := NewBackendAdminStorage(newMockKVStore())

	a := &AdminAssignment{ID: "u1", UserID: "user-1", Active: true}
	require.NoError(t, store.CreateAssignment(ctx, a))

	a.Active = false
	require.NoError(t, store.UpdateAssignment(ctx, a))

	got, err := store.GetAssignment(ctx, "u1")
	require.NoError(t, err)
	assert.False(t, got.Active)
}

func TestBackendAdminStorage_UpdateAssignment_Errors(t *testing.T) {
	ctx := context.Background()
	store := NewBackendAdminStorage(newMockKVStore())

	if err := store.UpdateAssignment(ctx, nil); err == nil {
		t.Fatal("expected error for nil assignment")
	}
	err := store.UpdateAssignment(ctx, &AdminAssignment{ID: "missing", UserID: "u"})
	require.ErrorIs(t, err, ErrAdminAssignmentNotFound)
}

func TestBackendAdminStorage_DeleteAssignment(t *testing.T) {
	ctx := context.Background()
	store := NewBackendAdminStorage(newMockKVStore())

	a := &AdminAssignment{ID: "d1", UserID: "user-1", Active: true}
	require.NoError(t, store.CreateAssignment(ctx, a))
	require.NoError(t, store.DeleteAssignment(ctx, "d1"))

	_, err := store.GetAssignment(ctx, "d1")
	require.ErrorIs(t, err, ErrAdminAssignmentNotFound)
}

func TestBackendAdminStorage_DeleteAssignment_Errors(t *testing.T) {
	ctx := context.Background()
	store := NewBackendAdminStorage(newMockKVStore())

	if err := store.DeleteAssignment(ctx, ""); err == nil {
		t.Fatal("expected error for empty id")
	}
	err := store.DeleteAssignment(ctx, "missing")
	require.ErrorIs(t, err, ErrAdminAssignmentNotFound)
}

func TestBackendAdminStorage_ListAssignments_Filters(t *testing.T) {
	ctx := context.Background()
	store := NewBackendAdminStorage(newMockKVStore())

	future := time.Now().Add(time.Hour)
	past := time.Now().Add(-time.Hour)

	require.NoError(t, store.CreateAssignment(ctx, &AdminAssignment{
		ID: "a1", UserID: "alice", AdminRole: "super", Active: true,
		TenantIDs: []string{"t1"}, ExpiresAt: &future,
	}))
	require.NoError(t, store.CreateAssignment(ctx, &AdminAssignment{
		ID: "a2", UserID: "bob", AdminRole: "tenant", Active: true,
		TenantIDs: []string{"t2"},
	}))
	require.NoError(t, store.CreateAssignment(ctx, &AdminAssignment{
		ID: "a3", UserID: "alice", AdminRole: "super", Active: false,
		TenantIDs: []string{"t3"},
	}))
	require.NoError(t, store.CreateAssignment(ctx, &AdminAssignment{
		ID: "a4", UserID: "carol", AdminRole: "super", Active: true,
		TenantIDs: []string{"t4"}, ExpiresAt: &past,
	}))

	// Filter by UserID
	got, err := store.ListAssignments(ctx, ListAdminAssignmentsOptions{UserID: "alice"})
	require.NoError(t, err)
	assert.Len(t, got, 2)

	// Filter by AdminRole
	got, err = store.ListAssignments(ctx, ListAdminAssignmentsOptions{AdminRole: "tenant"})
	require.NoError(t, err)
	assert.Len(t, got, 1)

	// Filter by TenantID. Assignments with an explicit, non-matching tenant
	// list are excluded; only a1 (t1) matches here.
	got, err = store.ListAssignments(ctx, ListAdminAssignmentsOptions{TenantID: "t1"})
	require.NoError(t, err)
	assert.Len(t, got, 1)

	// ActiveOnly excludes inactive and expired
	got, err = store.ListAssignments(ctx, ListAdminAssignmentsOptions{ActiveOnly: true})
	require.NoError(t, err)
	for _, a := range got {
		assert.True(t, a.Active)
		assert.NotEqual(t, "a4", a.ID) // expired excluded
	}
}

func TestBackendAdminStorage_ListAssignments_Pagination(t *testing.T) {
	ctx := context.Background()
	store := NewBackendAdminStorage(newMockKVStore())

	for _, id := range []string{"p1", "p2", "p3"} {
		require.NoError(t, store.CreateAssignment(ctx, &AdminAssignment{
			ID: id, UserID: "alice", Active: true,
		}))
	}

	// Limit
	got, err := store.ListAssignments(ctx, ListAdminAssignmentsOptions{UserID: "alice", Limit: 2})
	require.NoError(t, err)
	assert.Len(t, got, 2)

	// Offset beyond range returns empty
	got, err = store.ListAssignments(ctx, ListAdminAssignmentsOptions{UserID: "alice", Offset: 99})
	require.NoError(t, err)
	assert.Empty(t, got)

	// Offset within range
	got, err = store.ListAssignments(ctx, ListAdminAssignmentsOptions{UserID: "alice", Offset: 1})
	require.NoError(t, err)
	assert.Len(t, got, 2)
}

func TestBackendAdminStorage_GetUserAssignments(t *testing.T) {
	ctx := context.Background()
	store := NewBackendAdminStorage(newMockKVStore())

	require.NoError(t, store.CreateAssignment(ctx, &AdminAssignment{ID: "u1", UserID: "alice", Active: true}))
	require.NoError(t, store.CreateAssignment(ctx, &AdminAssignment{ID: "u2", UserID: "alice", Active: false}))
	require.NoError(t, store.CreateAssignment(ctx, &AdminAssignment{ID: "u3", UserID: "bob", Active: true}))

	got, err := store.GetUserAssignments(ctx, "alice")
	require.NoError(t, err)
	assert.Len(t, got, 1) // only active
	assert.Equal(t, "u1", got[0].ID)
}

func TestBackendAdminStorage_GetUserAssignments_Error(t *testing.T) {
	ctx := context.Background()
	store := NewBackendAdminStorage(newMockKVStore())

	if _, err := store.GetUserAssignments(ctx, ""); err == nil {
		t.Fatal("expected error for empty user id")
	}
}

// -----------------------------------------------------------------------------
// BackendImpersonationStorage
// -----------------------------------------------------------------------------

func newImpersonationSession(id string) *ImpersonationSession {
	return &ImpersonationSession{
		ID:           id,
		AdminUserID:  "admin-1",
		TargetUserID: "target-1",
		TenantID:     "t1",
		Reason:       "investigation",
		StartedAt:    time.Now().UTC(),
		ExpiresAt:    time.Now().Add(time.Hour),
	}
}

func TestBackendImpersonationStorage_CreateGet(t *testing.T) {
	ctx := context.Background()
	store := NewBackendImpersonationStorage(newMockKVStore())

	sess := newImpersonationSession("s1")
	require.NoError(t, store.CreateImpersonationSession(ctx, sess))

	got, err := store.GetImpersonationSession(ctx, "s1")
	require.NoError(t, err)
	assert.Equal(t, "admin-1", got.AdminUserID)
}

func TestBackendImpersonationStorage_Create_Errors(t *testing.T) {
	ctx := context.Background()
	store := NewBackendImpersonationStorage(newMockKVStore())

	if err := store.CreateImpersonationSession(ctx, nil); err == nil {
		t.Fatal("expected error for nil session")
	}
	if err := store.CreateImpersonationSession(ctx, &ImpersonationSession{}); err == nil {
		t.Fatal("expected error for empty ID")
	}
	sess := newImpersonationSession("dup")
	require.NoError(t, store.CreateImpersonationSession(ctx, sess))
	if err := store.CreateImpersonationSession(ctx, sess); err == nil {
		t.Fatal("expected duplicate error")
	}
}

func TestBackendImpersonationStorage_Get_Errors(t *testing.T) {
	ctx := context.Background()
	store := NewBackendImpersonationStorage(newMockKVStore())

	if _, err := store.GetImpersonationSession(ctx, ""); err == nil {
		t.Fatal("expected error for empty id")
	}
	_, err := store.GetImpersonationSession(ctx, "missing")
	require.Error(t, err)
	assert.True(t, dberrors.IsNotFound(err))
}

func TestBackendImpersonationStorage_End(t *testing.T) {
	ctx := context.Background()
	store := NewBackendImpersonationStorage(newMockKVStore())

	require.NoError(t, store.CreateImpersonationSession(ctx, newImpersonationSession("e1")))
	require.NoError(t, store.EndImpersonationSession(ctx, "e1"))

	got, err := store.GetImpersonationSession(ctx, "e1")
	require.NoError(t, err)
	require.NotNil(t, got.EndedAt)
}

func TestBackendImpersonationStorage_End_Errors(t *testing.T) {
	ctx := context.Background()
	store := NewBackendImpersonationStorage(newMockKVStore())

	if err := store.EndImpersonationSession(ctx, ""); err == nil {
		t.Fatal("expected error for empty id")
	}
	if err := store.EndImpersonationSession(ctx, "missing"); err == nil {
		t.Fatal("expected error for missing session")
	}
}

func TestBackendImpersonationStorage_IncrementOperationCount(t *testing.T) {
	ctx := context.Background()
	store := NewBackendImpersonationStorage(newMockKVStore())

	require.NoError(t, store.CreateImpersonationSession(ctx, newImpersonationSession("i1")))

	n, err := store.IncrementOperationCount(ctx, "i1")
	require.NoError(t, err)
	assert.Equal(t, int64(1), n)

	n, err = store.IncrementOperationCount(ctx, "i1")
	require.NoError(t, err)
	assert.Equal(t, int64(2), n)
}

func TestBackendImpersonationStorage_IncrementOperationCount_Errors(t *testing.T) {
	ctx := context.Background()
	store := NewBackendImpersonationStorage(newMockKVStore())

	if _, err := store.IncrementOperationCount(ctx, ""); err == nil {
		t.Fatal("expected error for empty id")
	}
	if _, err := store.IncrementOperationCount(ctx, "missing"); err == nil {
		t.Fatal("expected error for missing session")
	}
}

func TestBackendImpersonationStorage_ListActive(t *testing.T) {
	ctx := context.Background()
	store := NewBackendImpersonationStorage(newMockKVStore())

	require.NoError(t, store.CreateImpersonationSession(ctx, newImpersonationSession("act1")))
	ended := newImpersonationSession("act2")
	require.NoError(t, store.CreateImpersonationSession(ctx, ended))
	require.NoError(t, store.EndImpersonationSession(ctx, "act2"))

	other := newImpersonationSession("act3")
	other.AdminUserID = "admin-2"
	require.NoError(t, store.CreateImpersonationSession(ctx, other))

	got, err := store.ListActiveImpersonations(ctx, "admin-1")
	require.NoError(t, err)
	assert.Len(t, got, 1)
	assert.Equal(t, "act1", got[0].ID)
}

func TestBackendImpersonationStorage_ListActive_Error(t *testing.T) {
	ctx := context.Background()
	store := NewBackendImpersonationStorage(newMockKVStore())

	if _, err := store.ListActiveImpersonations(ctx, ""); err == nil {
		t.Fatal("expected error for empty admin id")
	}
}

func TestBackendImpersonationStorage_List(t *testing.T) {
	ctx := context.Background()
	store := NewBackendImpersonationStorage(newMockKVStore())

	require.NoError(t, store.CreateImpersonationSession(ctx, newImpersonationSession("l1")))
	expired := newImpersonationSession("l2")
	expired.ExpiresAt = time.Now().Add(-time.Hour)
	require.NoError(t, store.CreateImpersonationSession(ctx, expired))

	// All sessions, no filter
	got, err := store.ListImpersonations(ctx, false, "")
	require.NoError(t, err)
	assert.Len(t, got, 2)

	// activeOnly excludes expired
	got, err = store.ListImpersonations(ctx, true, "")
	require.NoError(t, err)
	assert.Len(t, got, 1)
	assert.Equal(t, "l1", got[0].ID)

	// Filter by admin user
	got, err = store.ListImpersonations(ctx, false, "admin-1")
	require.NoError(t, err)
	assert.Len(t, got, 2)

	got, err = store.ListImpersonations(ctx, false, "nobody")
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestBackendImpersonationStorage_DeleteExpiredSessions(t *testing.T) {
	ctx := context.Background()
	store := NewBackendImpersonationStorage(newMockKVStore())

	require.NoError(t, store.CreateImpersonationSession(ctx, newImpersonationSession("keep")))
	expired := newImpersonationSession("expired")
	expired.ExpiresAt = time.Now().Add(-time.Hour)
	require.NoError(t, store.CreateImpersonationSession(ctx, expired))
	ended := newImpersonationSession("ended")
	require.NoError(t, store.CreateImpersonationSession(ctx, ended))
	require.NoError(t, store.EndImpersonationSession(ctx, "ended"))

	require.NoError(t, store.DeleteExpiredSessions(ctx))

	remaining, err := store.ListImpersonations(ctx, false, "")
	require.NoError(t, err)
	assert.Len(t, remaining, 1)
	assert.Equal(t, "keep", remaining[0].ID)
}

// -----------------------------------------------------------------------------
// BackendInviteStore
// -----------------------------------------------------------------------------

func TestBackendInviteStore_CreateGetByToken(t *testing.T) {
	ctx := context.Background()
	store := NewBackendInviteStore(newMockKVStore())

	token := "plaintext-token"
	rec := &InviteRecord{
		TokenHash: TokenHashExported(token),
		UserID:    "user-1",
		TenantID:  "t1",
		ExpiresAt: time.Now().Add(time.Hour),
	}
	require.NoError(t, store.Create(ctx, rec))

	got, err := store.GetByToken(ctx, token)
	require.NoError(t, err)
	assert.Equal(t, "user-1", got.UserID)
}

func TestBackendInviteStore_Create_Error(t *testing.T) {
	ctx := context.Background()
	store := NewBackendInviteStore(newMockKVStore())

	if err := store.Create(ctx, nil); err == nil {
		t.Fatal("expected error for nil record")
	}
	if err := store.Create(ctx, &InviteRecord{}); err == nil {
		t.Fatal("expected error for empty token hash")
	}
}

func TestBackendInviteStore_GetByToken_NotFound(t *testing.T) {
	ctx := context.Background()
	store := NewBackendInviteStore(newMockKVStore())

	_, err := store.GetByToken(ctx, "no-such-token")
	require.ErrorIs(t, err, ErrInviteNotFound)
}

func TestBackendInviteStore_MarkUsed(t *testing.T) {
	ctx := context.Background()
	store := NewBackendInviteStore(newMockKVStore())

	token := "t-used"
	require.NoError(t, store.Create(ctx, &InviteRecord{
		TokenHash: TokenHashExported(token),
		UserID:    "u",
		ExpiresAt: time.Now().Add(time.Hour),
	}))

	require.NoError(t, store.MarkUsed(ctx, token))

	got, err := store.GetByToken(ctx, token)
	require.NoError(t, err)
	assert.True(t, got.Used)

	// double-use rejected
	err = store.MarkUsed(ctx, token)
	require.ErrorIs(t, err, ErrInviteUsed)
}

func TestBackendInviteStore_MarkUsed_NotFound(t *testing.T) {
	ctx := context.Background()
	store := NewBackendInviteStore(newMockKVStore())

	err := store.MarkUsed(ctx, "missing")
	require.ErrorIs(t, err, ErrInviteNotFound)
}

func TestBackendInviteStore_GC(t *testing.T) {
	ctx := context.Background()
	store := NewBackendInviteStore(newMockKVStore())

	keep := "keep"
	require.NoError(t, store.Create(ctx, &InviteRecord{
		TokenHash: TokenHashExported(keep), UserID: "u", ExpiresAt: time.Now().Add(time.Hour),
	}))
	expired := "expired"
	require.NoError(t, store.Create(ctx, &InviteRecord{
		TokenHash: TokenHashExported(expired), UserID: "u", ExpiresAt: time.Now().Add(-time.Hour),
	}))
	used := "used"
	require.NoError(t, store.Create(ctx, &InviteRecord{
		TokenHash: TokenHashExported(used), UserID: "u", ExpiresAt: time.Now().Add(time.Hour),
	}))
	require.NoError(t, store.MarkUsed(ctx, used))

	require.NoError(t, store.GC(ctx))

	_, err := store.GetByToken(ctx, keep)
	require.NoError(t, err)
	_, err = store.GetByToken(ctx, expired)
	require.ErrorIs(t, err, ErrInviteNotFound)
	_, err = store.GetByToken(ctx, used)
	require.ErrorIs(t, err, ErrInviteNotFound)
}
