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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

func newTestSessionLogger() *logger.Logger {
	log, _ := logger.New(logger.LogConfig{Level: "debug", Format: "json", Output: "stdout"})
	return log
}

// =============================================================================
// MemorySessionService Tests
// =============================================================================

func TestMemorySessionService_NewMemorySessionService(t *testing.T) {
	svc := NewMemorySessionService()
	require.NotNil(t, svc)
	assert.Empty(t, svc.sessions)
}

func TestMemorySessionService_ListSessions_Filtering(t *testing.T) {
	svc := NewMemorySessionService()
	now := time.Now()
	svc.AddSession(&iam.Session{ID: "s1", TenantID: "t1", UserID: 30001, Created: now, LastActivity: now, Expires: now.Add(time.Hour)})
	svc.AddSession(&iam.Session{ID: "s2", TenantID: "t1", UserID: 30002, Created: now, LastActivity: now, Expires: now.Add(time.Hour)})
	svc.AddSession(&iam.Session{ID: "s3", TenantID: "t2", UserID: 30001, Created: now, LastActivity: now, Expires: now.Add(time.Hour)})
	svc.AddSession(&iam.Session{ID: "s4", TenantID: "t1", UserID: 30001, Created: now, LastActivity: now, Expires: now.Add(time.Hour)})
	svc.AddSession(&iam.Session{ID: "s5", TenantID: "t1", UserID: 30001, Created: now, LastActivity: now, Expires: now.Add(time.Hour)})

	t.Run("refuses_an_empty_tenant", func(t *testing.T) {
		_, _, err := svc.ListSessions(callerContext("t1"), "", 30001, 0, 0)
		require.ErrorIs(t, err, ErrSessionScopeRequired)
	})
	t.Run("refuses_user_zero_as_the_whole_tenant", func(t *testing.T) {
		_, _, err := svc.ListSessions(callerContext("t1"), "t1", 0, 0, 0)
		require.ErrorIs(t, err, ErrSessionScopeRequired)
	})
	t.Run("filters_by_tenant_and_user", func(t *testing.T) {
		sessions, total, err := svc.ListSessions(callerContext("t1"), "t1", 30001, 0, 0)
		require.NoError(t, err)
		assert.Equal(t, 3, total)
		assert.Len(t, sessions, 3)
		for _, s := range sessions {
			assert.Equal(t, "t1", s.TenantID)
			assert.Equal(t, uint64(30001), s.UserID)
		}
	})
	t.Run("applies_pagination_with_limit", func(t *testing.T) {
		sessions, total, err := svc.ListSessions(callerContext("t1"), "t1", 30001, 1, 0)
		require.NoError(t, err)
		assert.Equal(t, 3, total)
		assert.Len(t, sessions, 1)
	})
	t.Run("applies_pagination_with_offset", func(t *testing.T) {
		sessions, _, err := svc.ListSessions(callerContext("t1"), "t1", 30001, 0, 2)
		require.NoError(t, err)
		assert.Len(t, sessions, 1)
	})
	t.Run("offset_beyond_results_returns_empty", func(t *testing.T) {
		sessions, _, err := svc.ListSessions(callerContext("t1"), "t1", 30001, 0, 100)
		require.NoError(t, err)
		assert.Empty(t, sessions)
	})
}

func TestMemorySessionService_GetSession_Coverage(t *testing.T) {
	svc := NewMemorySessionService()
	svc.AddSession(&iam.Session{ID: "s1", TenantID: "t1", UserID: 30001, Created: time.Now()})

	t.Run("returns_existing_session", func(t *testing.T) {
		sess, err := svc.GetSession(callerContext("t1"), "s1")
		require.NoError(t, err)
		assert.Equal(t, "s1", sess.ID)
	})
	t.Run("returns_not_found_for_missing", func(t *testing.T) {
		_, err := svc.GetSession(callerContext("t1"), "nonexistent")
		require.Error(t, err)
	})
}

func TestMemorySessionService_RevokeSession_Coverage(t *testing.T) {
	svc := NewMemorySessionService()
	svc.AddSession(&iam.Session{ID: "s1", TenantID: "t1", UserID: 30001, Created: time.Now()})

	t.Run("revokes_existing", func(t *testing.T) {
		require.NoError(t, svc.RevokeSession(callerContext("t1"), "s1"))
		_, err := svc.GetSession(callerContext("t1"), "s1")
		require.Error(t, err)
	})
	t.Run("no_error_revoking_nonexistent", func(t *testing.T) {
		assert.NoError(t, svc.RevokeSession(callerContext("t1"), "nonexistent"))
	})
}

func TestMemorySessionService_RevokeAllSessions_Coverage(t *testing.T) {
	svc := NewMemorySessionService()
	now := time.Now()
	svc.AddSession(&iam.Session{ID: "s1", TenantID: "t1", UserID: 30001, Created: now})
	svc.AddSession(&iam.Session{ID: "s2", TenantID: "t1", UserID: 30001, Created: now})
	count, err := svc.RevokeAllSessions(callerContext("t1"), "t1", 30001)
	require.NoError(t, err)
	assert.Equal(t, 2, count)
}

func TestMemorySessionService_RevokeOtherSessions_Coverage(t *testing.T) {
	svc := NewMemorySessionService()
	now := time.Now()
	svc.AddSession(&iam.Session{ID: "keep", TenantID: "t1", UserID: 30001, Created: now})
	svc.AddSession(&iam.Session{ID: "r1", TenantID: "t1", UserID: 30001, Created: now})
	svc.AddSession(&iam.Session{ID: "r2", TenantID: "t1", UserID: 30001, Created: now})
	count, err := svc.RevokeOtherSessions(callerContext("t1"), "t1", "keep")
	require.NoError(t, err)
	assert.Equal(t, 2, count)
}

func TestMemorySessionService_CreateTestSession_Coverage(t *testing.T) {
	svc := NewMemorySessionService()
	sess := svc.CreateTestSession("tenant1", 20001)
	require.NotNil(t, sess)
	assert.Contains(t, sess.ID, "tenant1")
	fetched, err := svc.GetSession(callerContext("t1"), sess.ID)
	require.NoError(t, err)
	assert.Equal(t, sess.ID, fetched.ID)
}

// =============================================================================
// sessionToProto Tests
// =============================================================================

func TestSessionToProto_Coverage(t *testing.T) {
	t.Run("nil_returns_nil", func(t *testing.T) {
		assert.Nil(t, sessionToProto(nil))
	})
	t.Run("converts_full_session", func(t *testing.T) {
		now := time.Now()
		pb := sessionToProto(&iam.Session{
			ID: "s1", UserID: 30001, TenantID: "t1",
			Created: now, LastActivity: now, Expires: now.Add(time.Hour),
			MFAVerified: true, IPAddress: "10.0.0.1", UserAgent: "agent", AuthMethod: "password",
		})
		require.NotNil(t, pb)
		assert.Equal(t, "s1", pb.Id)
		assert.Equal(t, "30001", pb.UserId)
		assert.True(t, pb.MfaVerified)
	})
}

// =============================================================================
// SessionServer Success-Path Tests
// =============================================================================

func TestSessionServerCoverage_ListSessions_Success(t *testing.T) {
	svc := NewMemorySessionService()
	svc.CreateTestSession("t1", 30001)
	svc.CreateTestSession("t1", 30002)
	server := NewSessionServer(svc, userIDCallerAccounts{}, newTestSessionLogger())

	resp, err := server.ListSessions(sessionOwnerContext("t1", 30001), &iampb.ListSessionsRequest{TenantId: "t1"})
	require.NoError(t, err)
	assert.Equal(t, int32(1), resp.Total, "the caller lists only its own sessions")
}

func TestSessionServerCoverage_GetSession_Success(t *testing.T) {
	svc := NewMemorySessionService()
	sess := svc.CreateTestSession("t1", 30001)
	server := NewSessionServer(svc, userIDCallerAccounts{}, newTestSessionLogger())

	resp, err := server.GetSession(sessionOwnerContext("t1", 30001), &iampb.GetSessionRequest{SessionId: sess.ID})
	require.NoError(t, err)
	assert.Equal(t, sess.ID, resp.Id)
}

func TestSessionServerCoverage_RevokeSession_Success(t *testing.T) {
	svc := NewMemorySessionService()
	sess := svc.CreateTestSession("t1", 30001)
	server := NewSessionServer(svc, userIDCallerAccounts{}, newTestSessionLogger())

	resp, err := server.RevokeSession(sessionOwnerContext("t1", 30001), &iampb.RevokeSessionRequest{SessionId: sess.ID})
	require.NoError(t, err)
	assert.True(t, resp.Success)
}

func TestSessionServerCoverage_RevokeAllSessions_Success(t *testing.T) {
	now := time.Now()
	svc := NewMemorySessionService()
	svc.AddSession(&iam.Session{ID: "a", TenantID: "t1", UserID: 30001, Created: now, LastActivity: now, Expires: now.Add(time.Hour)})
	svc.AddSession(&iam.Session{ID: "b", TenantID: "t1", UserID: 30001, Created: now, LastActivity: now, Expires: now.Add(time.Hour)})
	server := NewSessionServer(svc, userIDCallerAccounts{}, newTestSessionLogger())

	resp, err := server.RevokeAllSessions(sessionOwnerContext("t1", 30001), &iampb.RevokeAllSessionsRequest{TenantId: "t1", UserId: "30001"})
	require.NoError(t, err)
	assert.Equal(t, int32(2), resp.RevokedCount)
}

func TestSessionServerCoverage_RevokeOtherSessions_Success(t *testing.T) {
	now := time.Now()
	svc := NewMemorySessionService()
	keep := svc.CreateTestSession("t1", 30001)
	svc.AddSession(&iam.Session{ID: "o1", TenantID: "t1", UserID: 30001, Created: now, LastActivity: now, Expires: now.Add(time.Hour)})
	svc.AddSession(&iam.Session{ID: "o2", TenantID: "t1", UserID: 30001, Created: now, LastActivity: now, Expires: now.Add(time.Hour)})
	server := NewSessionServer(svc, userIDCallerAccounts{}, newTestSessionLogger())

	resp, err := server.RevokeOtherSessions(sessionOwnerContext("t1", 30001), &iampb.RevokeOtherSessionsRequest{TenantId: "t1", KeepSessionId: keep.ID})
	require.NoError(t, err)
	assert.Equal(t, int32(2), resp.RevokedCount)
}

// =============================================================================
// SessionServer Validation Tests (empty required fields)
// =============================================================================

// An empty tenant_id scopes the list to the caller's tenant and own
// sessions: it never reads as every session of the tenant (#1842).
func TestSessionServerCoverage_ListSessions_EmptyTenantId(t *testing.T) {
	server, _ := sessionOwnershipFixture(t)
	resp, err := server.ListSessions(sessionCallerContext("t1", "alice"), &iampb.ListSessionsRequest{})
	require.NoError(t, err)
	ids := make([]string, 0, len(resp.Sessions))
	for _, s := range resp.Sessions {
		ids = append(ids, s.Id)
	}
	assert.ElementsMatch(t, []string{ownSessionID, ownSecondSessionID}, ids)
}

// A caller whose account resolves to user 0 is refused rather than read as
// the whole tenant (#1842).
func TestSessionServerCoverage_ListSessions_UserZeroCallerRefused(t *testing.T) {
	server := NewSessionServer(NewMemorySessionService(), userIDCallerAccounts{}, newTestSessionLogger())
	_, err := server.ListSessions(sessionOwnerContext("t1", 0), &iampb.ListSessionsRequest{})
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
	_, err = server.RevokeAllSessions(sessionOwnerContext("t1", 0), &iampb.RevokeAllSessionsRequest{})
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
}

func TestSessionServerCoverage_GetSession_EmptySessionId(t *testing.T) {
	server := NewSessionServer(NewMemorySessionService(), userIDCallerAccounts{}, newTestSessionLogger())
	_, err := server.GetSession(callerContext("t1"), &iampb.GetSessionRequest{})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestSessionServerCoverage_RevokeSession_EmptySessionId(t *testing.T) {
	server := NewSessionServer(NewMemorySessionService(), userIDCallerAccounts{}, newTestSessionLogger())
	_, err := server.RevokeSession(callerContext("t1"), &iampb.RevokeSessionRequest{})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

// An empty tenant_id revokes only the caller's own sessions in its tenant
// (#1842).
func TestSessionServerCoverage_RevokeAllSessions_EmptyTenantId(t *testing.T) {
	server, storage := sessionOwnershipFixture(t)
	resp, err := server.RevokeAllSessions(sessionCallerContext("t1", "alice"), &iampb.RevokeAllSessionsRequest{})
	require.NoError(t, err)
	assert.Equal(t, int32(2), resp.RevokedCount)
	requireSessionKept(t, storage, otherUserSessionID, otherTenantSessionID)
}

func TestSessionServerCoverage_RevokeOtherSessions_EmptyTenantId(t *testing.T) {
	server := NewSessionServer(NewMemorySessionService(), userIDCallerAccounts{}, newTestSessionLogger())
	_, err := server.RevokeOtherSessions(callerContext("t1"), &iampb.RevokeOtherSessionsRequest{})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestSessionServerCoverage_RevokeOtherSessions_EmptyKeepSessionId(t *testing.T) {
	server := NewSessionServer(NewMemorySessionService(), userIDCallerAccounts{}, newTestSessionLogger())
	_, err := server.RevokeOtherSessions(callerContext("t1"), &iampb.RevokeOtherSessionsRequest{TenantId: "t1"})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestSessionServerCoverage_GetSession_NotFound(t *testing.T) {
	server := NewSessionServer(NewMemorySessionService(), userIDCallerAccounts{}, newTestSessionLogger())
	_, err := server.GetSession(sessionOwnerContext("t1", 30001), &iampb.GetSessionRequest{SessionId: "nonexistent-id"})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, st.Code())
}
