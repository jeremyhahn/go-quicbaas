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

package grpc

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// Session ownership fixture: the caller is user 101 of tenant t1. Each
// session is named for whom it belongs to.
const (
	ownSessionID         = "own"
	ownSecondSessionID   = "own-2"
	otherUserSessionID   = "other-user"
	otherTenantSessionID = "other-tenant"
	ownerUserID          = uint64(101)
	otherUserID          = uint64(102)
)

// fakeCallerAccounts resolves a subject to the account mapped under its
// tenant and name, the way service.ResolveCallerAccount looks the caller up
// in the identity store. An unmapped subject is refused as not a user.
type fakeCallerAccounts map[string]uint64

func (f fakeCallerAccounts) ResolveCallerAccount(_ context.Context, subject interfaces.Subject) (uint64, error) {
	userID, ok := f[subject.TenantID+"/"+subject.ID]
	if !ok {
		return 0, &service.CallerAccountRefusedError{Reason: "the authenticated caller does not name a user account"}
	}
	return userID, nil
}

// userIDCallerAccounts resolves a subject whose name is its account id, the
// way a token carrying the user-id claim names its caller.
type userIDCallerAccounts struct{}

func (userIDCallerAccounts) ResolveCallerAccount(_ context.Context, subject interfaces.Subject) (uint64, error) {
	userID, err := strconv.ParseUint(subject.ID, 10, 64)
	if err != nil {
		return 0, &service.CallerAccountRefusedError{Reason: "the authenticated caller does not name a user account"}
	}
	return userID, nil
}

// sessionOwnerContext authenticates the caller as user userID of tenantID,
// for a server built with userIDCallerAccounts.
func sessionOwnerContext(tenantID string, userID uint64) context.Context {
	return sessionCallerContext(tenantID, strconv.FormatUint(userID, 10))
}

// sessionOwnershipFixture holds sessions for the caller, another user in the
// caller's tenant and a user in another tenant, served by the real adapter.
func sessionOwnershipFixture(t *testing.T) (*SessionServer, *iam.MemorySessionStorage) {
	t.Helper()
	storage := iam.NewMemorySessionStorage()
	now := time.Now()
	for _, s := range []*iam.Session{
		{ID: ownSessionID, TenantID: "t1", UserID: ownerUserID},
		{ID: ownSecondSessionID, TenantID: "t1", UserID: ownerUserID},
		{ID: otherUserSessionID, TenantID: "t1", UserID: otherUserID},
		{ID: otherTenantSessionID, TenantID: "t2", UserID: ownerUserID},
	} {
		s.Created, s.LastActivity, s.Expires = now, now, now.Add(time.Hour)
		require.NoError(t, storage.CreateSession(context.Background(), s))
	}
	callers := fakeCallerAccounts{"t1/alice": ownerUserID}
	return NewSessionServer(NewSessionServiceAdapter(storage), callers, newTestSessionLogger()), storage
}

// sessionCallerContext is what the authorization interceptor leaves for the
// handler: the authenticated subject and its tenant.
func sessionCallerContext(tenantID, name string) context.Context {
	ctx := tenantctx.WithTenant(context.Background(), tenantID)
	return auth.SubjectIntoContext(ctx, interfaces.Subject{ID: name, TenantID: tenantID, Authenticated: true})
}

func requireSessionKept(t *testing.T, storage *iam.MemorySessionStorage, ids ...string) {
	t.Helper()
	for _, id := range ids {
		_, err := storage.GetSession(context.Background(), id)
		require.NoError(t, err, "a refused call must leave session %s in place", id)
	}
}

func requirePermissionDenied(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
}

func TestSessionOwnership_GetSession(t *testing.T) {
	server, _ := sessionOwnershipFixture(t)
	ctx := sessionCallerContext("t1", "alice")

	got, err := server.GetSession(ctx, &iampb.GetSessionRequest{SessionId: ownSessionID})
	require.NoError(t, err)
	assert.Equal(t, ownSessionID, got.Id)

	for _, id := range []string{otherUserSessionID, otherTenantSessionID} {
		_, err := server.GetSession(ctx, &iampb.GetSessionRequest{SessionId: id})
		requirePermissionDenied(t, err)
	}
}

func TestSessionOwnership_RevokeSession(t *testing.T) {
	server, storage := sessionOwnershipFixture(t)
	ctx := sessionCallerContext("t1", "alice")

	for _, id := range []string{otherUserSessionID, otherTenantSessionID} {
		_, err := server.RevokeSession(ctx, &iampb.RevokeSessionRequest{SessionId: id})
		requirePermissionDenied(t, err)
	}
	requireSessionKept(t, storage, otherUserSessionID, otherTenantSessionID)

	_, err := server.RevokeSession(ctx, &iampb.RevokeSessionRequest{SessionId: ownSessionID})
	require.NoError(t, err)
	_, err = storage.GetSession(context.Background(), ownSessionID)
	require.Error(t, err, "the owner's revoke must delete the session")
}

func TestSessionOwnership_RevokeAllSessions(t *testing.T) {
	server, storage := sessionOwnershipFixture(t)
	ctx := sessionCallerContext("t1", "alice")

	_, err := server.RevokeAllSessions(ctx, &iampb.RevokeAllSessionsRequest{TenantId: "t1", UserId: "102"})
	requirePermissionDenied(t, err)
	_, err = server.RevokeAllSessions(ctx, &iampb.RevokeAllSessionsRequest{TenantId: "t2", UserId: "101"})
	requirePermissionDenied(t, err)
	requireSessionKept(t, storage, otherUserSessionID, otherTenantSessionID)

	resp, err := server.RevokeAllSessions(ctx, &iampb.RevokeAllSessionsRequest{TenantId: "t1", UserId: "101"})
	require.NoError(t, err)
	assert.Equal(t, int32(2), resp.RevokedCount)
	requireSessionKept(t, storage, otherUserSessionID, otherTenantSessionID)
}

func TestSessionOwnership_RevokeAllSessions_DefaultsToCaller(t *testing.T) {
	server, storage := sessionOwnershipFixture(t)

	resp, err := server.RevokeAllSessions(sessionCallerContext("t1", "alice"), &iampb.RevokeAllSessionsRequest{TenantId: "t1"})
	require.NoError(t, err)
	assert.Equal(t, int32(2), resp.RevokedCount)
	requireSessionKept(t, storage, otherUserSessionID, otherTenantSessionID)
}

func TestSessionOwnership_RevokeOtherSessions(t *testing.T) {
	server, storage := sessionOwnershipFixture(t)
	ctx := sessionCallerContext("t1", "alice")

	for _, keep := range []string{otherUserSessionID, otherTenantSessionID} {
		_, err := server.RevokeOtherSessions(ctx, &iampb.RevokeOtherSessionsRequest{TenantId: "t1", KeepSessionId: keep})
		requirePermissionDenied(t, err)
	}
	requireSessionKept(t, storage, ownSessionID, ownSecondSessionID, otherUserSessionID, otherTenantSessionID)

	resp, err := server.RevokeOtherSessions(ctx, &iampb.RevokeOtherSessionsRequest{TenantId: "t1", KeepSessionId: ownSessionID})
	require.NoError(t, err)
	assert.Equal(t, int32(1), resp.RevokedCount)
	requireSessionKept(t, storage, ownSessionID, otherUserSessionID, otherTenantSessionID)
}

func TestSessionOwnership_ListSessions(t *testing.T) {
	server, _ := sessionOwnershipFixture(t)
	ctx := sessionCallerContext("t1", "alice")

	_, err := server.ListSessions(ctx, &iampb.ListSessionsRequest{TenantId: "t1", UserId: "102"})
	requirePermissionDenied(t, err)

	for _, userID := range []string{"", "101"} {
		resp, err := server.ListSessions(ctx, &iampb.ListSessionsRequest{TenantId: "t1", UserId: userID})
		require.NoError(t, err)
		assert.Equal(t, int32(2), resp.Total, "user_id %q lists the caller's own sessions", userID)
	}
}

func TestSessionOwnership_CallerNotAUser(t *testing.T) {
	server, storage := sessionOwnershipFixture(t)
	ctx := sessionCallerContext("t1", "service-account")

	_, err := server.GetSession(ctx, &iampb.GetSessionRequest{SessionId: ownSessionID})
	requirePermissionDenied(t, err)
	_, err = server.RevokeSession(ctx, &iampb.RevokeSessionRequest{SessionId: ownSessionID})
	requirePermissionDenied(t, err)
	requireSessionKept(t, storage, ownSessionID)
}

func TestSessionOwnership_Unauthenticated(t *testing.T) {
	server, storage := sessionOwnershipFixture(t)

	_, err := server.RevokeSession(callerContext("t1"), &iampb.RevokeSessionRequest{SessionId: ownSessionID})
	require.Error(t, err)
	assert.Equal(t, codes.Unauthenticated, status.Code(err))
	requireSessionKept(t, storage, ownSessionID)
}
