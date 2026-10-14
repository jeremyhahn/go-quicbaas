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

//go:build !integration

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
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

func createTestSessionServer(t *testing.T) (*SessionServer, *MemorySessionService) {
	t.Helper()

	logConfig := logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "session-test",
	}
	log, err := logger.New(logConfig)
	require.NoError(t, err)

	memService := NewMemorySessionService()
	srv := NewSessionServer(memService, log)
	return srv, memService
}

func TestNewSessionServer(t *testing.T) {
	logConfig := logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "session-test",
	}
	log, err := logger.New(logConfig)
	require.NoError(t, err)

	memService := NewMemorySessionService()
	srv := NewSessionServer(memService, log)

	assert.NotNil(t, srv)
	assert.NotNil(t, srv.sessionService)
	assert.NotNil(t, srv.logger)
}

func TestSessionServer_ListSessions(t *testing.T) {
	srv, memService := createTestSessionServer(t)
	ctx := callerContext("tenant1")

	t.Run("success with sessions", func(t *testing.T) {
		memService.CreateTestSession("tenant1", 20001)
		memService.CreateTestSession("tenant1", 20002)

		resp, err := srv.ListSessions(ctx, &iampb.ListSessionsRequest{
			TenantId: "tenant1",
		})

		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.GreaterOrEqual(t, len(resp.Sessions), 1)
	})

	t.Run("success with user filter", func(t *testing.T) {
		resp, err := srv.ListSessions(ctx, &iampb.ListSessionsRequest{
			TenantId: "tenant1",
			UserId:   "20001",
		})

		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("success with pagination", func(t *testing.T) {
		resp, err := srv.ListSessions(ctx, &iampb.ListSessionsRequest{
			TenantId: "tenant1",
			Limit:    1,
			Offset:   0,
		})

		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.LessOrEqual(t, int32(len(resp.Sessions)), int32(1))
	})

	t.Run("error missing tenant_id", func(t *testing.T) {
		resp, err := srv.ListSessions(ctx, &iampb.ListSessionsRequest{
			TenantId: "",
		})

		require.Error(t, err)
		assert.Nil(t, resp)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})
}

func TestSessionServer_GetSession(t *testing.T) {
	srv, memService := createTestSessionServer(t)
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		session := memService.CreateTestSession("tenant1", 20001)

		resp, err := srv.GetSession(ctx, &iampb.GetSessionRequest{
			SessionId: session.ID,
		})

		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.Equal(t, session.ID, resp.Id)
		assert.Equal(t, strconv.FormatUint(session.UserID, 10), resp.UserId)
		assert.Equal(t, session.TenantID, resp.TenantId)
	})

	t.Run("error missing session_id", func(t *testing.T) {
		resp, err := srv.GetSession(ctx, &iampb.GetSessionRequest{
			SessionId: "",
		})

		require.Error(t, err)
		assert.Nil(t, resp)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("error session not found", func(t *testing.T) {
		resp, err := srv.GetSession(ctx, &iampb.GetSessionRequest{
			SessionId: "nonexistent-session-id",
		})

		require.Error(t, err)
		assert.Nil(t, resp)
		assert.Equal(t, codes.NotFound, status.Code(err))
	})
}

func TestSessionServer_RevokeSession(t *testing.T) {
	srv, memService := createTestSessionServer(t)
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		session := memService.CreateTestSession("tenant1", 50001)

		resp, err := srv.RevokeSession(ctx, &iampb.RevokeSessionRequest{
			SessionId: session.ID,
		})

		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.True(t, resp.Success)

		// Verify session is revoked
		_, err = memService.GetSession(ctx, session.ID)
		assert.Error(t, err)
	})

	t.Run("error missing session_id", func(t *testing.T) {
		resp, err := srv.RevokeSession(ctx, &iampb.RevokeSessionRequest{
			SessionId: "",
		})

		require.Error(t, err)
		assert.Nil(t, resp)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})
}

func TestSessionServer_RevokeAllSessions(t *testing.T) {
	srv, memService := createTestSessionServer(t)
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		memService.CreateTestSession("tenant-all", 50002)
		memService.CreateTestSession("tenant-all", 50003)

		resp, err := srv.RevokeAllSessions(callerContext("tenant-all"), &iampb.RevokeAllSessionsRequest{
			TenantId: "tenant-all",
		})

		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.GreaterOrEqual(t, resp.RevokedCount, int32(1))
	})

	t.Run("success with user filter", func(t *testing.T) {
		memService.CreateTestSession("tenant-filter", 50004)
		memService.CreateTestSession("tenant-filter", 50005)

		resp, err := srv.RevokeAllSessions(callerContext("tenant-filter"), &iampb.RevokeAllSessionsRequest{
			TenantId: "tenant-filter",
			UserId:   "50004",
		})

		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("error missing tenant_id", func(t *testing.T) {
		resp, err := srv.RevokeAllSessions(ctx, &iampb.RevokeAllSessionsRequest{
			TenantId: "",
		})

		require.Error(t, err)
		assert.Nil(t, resp)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})
}

func TestSessionServer_RevokeOtherSessions(t *testing.T) {
	srv, memService := createTestSessionServer(t)
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		session1 := memService.CreateTestSession("tenant-other", 50006)
		memService.AddSession(&iam.Session{
			ID:           "sess-other-2",
			UserID:       50006,
			TenantID:     "tenant-other",
			Created:      time.Now(),
			LastActivity: time.Now(),
			Expires:      time.Now().Add(24 * time.Hour),
		})

		resp, err := srv.RevokeOtherSessions(callerContext("tenant-other"), &iampb.RevokeOtherSessionsRequest{
			TenantId:      "tenant-other",
			KeepSessionId: session1.ID,
		})

		require.NoError(t, err)
		assert.NotNil(t, resp)
		// The kept session should still exist
		_, err = memService.GetSession(ctx, session1.ID)
		assert.NoError(t, err)
	})

	t.Run("error missing tenant_id", func(t *testing.T) {
		resp, err := srv.RevokeOtherSessions(ctx, &iampb.RevokeOtherSessionsRequest{
			TenantId:      "",
			KeepSessionId: "some-id",
		})

		require.Error(t, err)
		assert.Nil(t, resp)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("error missing keep_session_id", func(t *testing.T) {
		resp, err := srv.RevokeOtherSessions(callerContext("tenant1"), &iampb.RevokeOtherSessionsRequest{
			TenantId:      "tenant1",
			KeepSessionId: "",
		})

		require.Error(t, err)
		assert.Nil(t, resp)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})
}

func TestSessionToProto(t *testing.T) {
	t.Run("nil session returns nil", func(t *testing.T) {
		result := sessionToProto(nil)
		assert.Nil(t, result)
	})

	t.Run("valid session conversion", func(t *testing.T) {
		now := time.Now()
		expires := now.Add(24 * time.Hour)
		session := &iam.Session{
			ID:           "test-sess-id",
			UserID:       50007,
			TenantID:     "test-tenant-id",
			Created:      now,
			LastActivity: now,
			Expires:      expires,
			MFAVerified:  true,
			IPAddress:    "192.168.1.1",
			UserAgent:    "TestAgent/1.0",
			AuthMethod:   "oauth2",
		}

		result := sessionToProto(session)

		assert.NotNil(t, result)
		assert.Equal(t, session.ID, result.Id)
		assert.Equal(t, strconv.FormatUint(session.UserID, 10), result.UserId)
		assert.Equal(t, session.TenantID, result.TenantId)
		assert.True(t, result.MfaVerified)
		assert.Equal(t, session.IPAddress, result.IpAddress)
		assert.Equal(t, session.UserAgent, result.UserAgent)
	})
}

func TestMemorySessionService(t *testing.T) {
	ctx := context.Background()

	t.Run("NewMemorySessionService", func(t *testing.T) {
		svc := NewMemorySessionService()
		assert.NotNil(t, svc)
		assert.NotNil(t, svc.sessions)
	})

	t.Run("AddSession and GetSession", func(t *testing.T) {
		svc := NewMemorySessionService()
		session := &iam.Session{
			ID:       "add-test",
			UserID:   20001,
			TenantID: "tenant1",
		}
		svc.AddSession(session)

		retrieved, err := svc.GetSession(ctx, "add-test")
		require.NoError(t, err)
		assert.Equal(t, session.ID, retrieved.ID)
	})

	t.Run("CreateTestSession", func(t *testing.T) {
		svc := NewMemorySessionService()
		session := svc.CreateTestSession("tenant1", 20001)

		assert.NotEmpty(t, session.ID)
		assert.Equal(t, uint64(20001), session.UserID)
		assert.Equal(t, "tenant1", session.TenantID)
		assert.False(t, session.Created.IsZero())
	})

	t.Run("ListSessions with filtering", func(t *testing.T) {
		svc := NewMemorySessionService()
		svc.CreateTestSession("t1", 30001)
		svc.CreateTestSession("t1", 30002)
		svc.CreateTestSession("t2", 30001)

		// Filter by tenant
		sessions, total, err := svc.ListSessions(ctx, "t1", 0, 10, 0)
		require.NoError(t, err)
		assert.Equal(t, 2, total)
		assert.Len(t, sessions, 2)

		// Filter by tenant and user
		sessions, total, err = svc.ListSessions(ctx, "t1", 30001, 10, 0)
		require.NoError(t, err)
		assert.Equal(t, 1, total)
		assert.Len(t, sessions, 1)
	})

	t.Run("ListSessions with pagination", func(t *testing.T) {
		svc := NewMemorySessionService()
		for i := 0; i < 5; i++ {
			svc.AddSession(&iam.Session{
				ID:       "pag-" + string(rune('a'+i)),
				TenantID: "pag-tenant",
				UserID:   50008,
			})
		}

		// Test limit
		sessions, _, err := svc.ListSessions(ctx, "pag-tenant", 0, 2, 0)
		require.NoError(t, err)
		assert.Len(t, sessions, 2)

		// Test offset beyond range
		sessions, total, err := svc.ListSessions(ctx, "pag-tenant", 0, 10, 100)
		require.NoError(t, err)
		assert.Len(t, sessions, 0)
		assert.Equal(t, 5, total)
	})

	t.Run("RevokeSession", func(t *testing.T) {
		svc := NewMemorySessionService()
		session := svc.CreateTestSession("rev-tenant", 50009)

		err := svc.RevokeSession(ctx, session.ID)
		require.NoError(t, err)

		_, err = svc.GetSession(ctx, session.ID)
		assert.Error(t, err)
	})

	t.Run("RevokeAllSessions", func(t *testing.T) {
		svc := NewMemorySessionService()
		svc.CreateTestSession("all-tenant", 50010)
		svc.CreateTestSession("all-tenant", 50011)
		svc.CreateTestSession("other-tenant", 50012)

		count, err := svc.RevokeAllSessions(ctx, "all-tenant", 0)
		require.NoError(t, err)
		assert.Equal(t, 2, count)

		// Verify other tenant's session still exists
		sessions, _, _ := svc.ListSessions(ctx, "other-tenant", 0, 10, 0)
		assert.Len(t, sessions, 1)
	})

	t.Run("RevokeOtherSessions", func(t *testing.T) {
		svc := NewMemorySessionService()
		keep := &iam.Session{ID: "keep-me", TenantID: "ro-tenant", UserID: 50013}
		other1 := &iam.Session{ID: "revoke-1", TenantID: "ro-tenant", UserID: 50013}
		other2 := &iam.Session{ID: "revoke-2", TenantID: "ro-tenant", UserID: 50013}

		svc.AddSession(keep)
		svc.AddSession(other1)
		svc.AddSession(other2)

		count, err := svc.RevokeOtherSessions(ctx, "ro-tenant", "keep-me")
		require.NoError(t, err)
		assert.Equal(t, 2, count)

		// Verify kept session still exists
		_, err = svc.GetSession(ctx, "keep-me")
		assert.NoError(t, err)

		// Verify others are gone
		_, err = svc.GetSession(ctx, "revoke-1")
		assert.Error(t, err)
	})
}

// --- Merged from server_session_coverage_test.go ---

// =============================================================================
// SessionServer Tests
// =============================================================================

func newTestSessionServer(t *testing.T) (*SessionServer, *MemorySessionService) {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	require.NoError(t, err)
	svc := NewMemorySessionService()
	srv := NewSessionServer(svc, log)
	return srv, svc
}

func TestSessionServer_NewSessionServer(t *testing.T) {
	srv, _ := newTestSessionServer(t)
	assert.NotNil(t, srv)
	assert.NotNil(t, srv.sessionService)
	assert.NotNil(t, srv.logger)
}

func TestSessionServer_ListSessions_Success(t *testing.T) {
	srv, svc := newTestSessionServer(t)
	svc.CreateTestSession("tenant-1", 10001)
	svc.CreateTestSession("tenant-1", 10002)

	resp, err := srv.ListSessions(callerContext("tenant-1"), &iampb.ListSessionsRequest{
		TenantId: "tenant-1",
	})
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.GreaterOrEqual(t, int(resp.Total), 1)
}

func TestSessionServer_ListSessions_EmptyTenantID(t *testing.T) {
	srv, _ := newTestSessionServer(t)
	_, err := srv.ListSessions(callerContext("tenant-1"), &iampb.ListSessionsRequest{
		TenantId: "",
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestSessionServer_ListSessions_FilterByUser(t *testing.T) {
	srv, svc := newTestSessionServer(t)
	svc.CreateTestSession("tenant-1", 10001)
	svc.CreateTestSession("tenant-1", 10002)

	resp, err := srv.ListSessions(callerContext("tenant-1"), &iampb.ListSessionsRequest{
		TenantId: "tenant-1",
		UserId:   "10001",
	})
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, int32(1), resp.Total)
}

func TestSessionServer_GetSession_Success(t *testing.T) {
	srv, svc := newTestSessionServer(t)
	sess := svc.CreateTestSession("tenant-1", 10001)

	resp, err := srv.GetSession(context.Background(), &iampb.GetSessionRequest{
		SessionId: sess.ID,
	})
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, sess.ID, resp.Id)
	assert.Equal(t, "10001", resp.UserId)
	assert.Equal(t, "tenant-1", resp.TenantId)
}

func TestSessionServer_GetSession_EmptyID(t *testing.T) {
	srv, _ := newTestSessionServer(t)
	_, err := srv.GetSession(context.Background(), &iampb.GetSessionRequest{
		SessionId: "",
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestSessionServer_GetSession_NotFound(t *testing.T) {
	srv, _ := newTestSessionServer(t)
	_, err := srv.GetSession(context.Background(), &iampb.GetSessionRequest{
		SessionId: "nonexistent-session",
	})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestSessionServer_RevokeSession_Success(t *testing.T) {
	srv, svc := newTestSessionServer(t)
	sess := svc.CreateTestSession("tenant-1", 10001)

	resp, err := srv.RevokeSession(context.Background(), &iampb.RevokeSessionRequest{
		SessionId: sess.ID,
	})
	require.NoError(t, err)
	assert.True(t, resp.Success)

	// Verify session is gone
	_, err = srv.GetSession(context.Background(), &iampb.GetSessionRequest{
		SessionId: sess.ID,
	})
	require.Error(t, err)
}

func TestSessionServer_RevokeSession_EmptyID(t *testing.T) {
	srv, _ := newTestSessionServer(t)
	_, err := srv.RevokeSession(context.Background(), &iampb.RevokeSessionRequest{
		SessionId: "",
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestSessionServer_RevokeAllSessions_Success(t *testing.T) {
	srv, svc := newTestSessionServer(t)
	svc.CreateTestSession("tenant-1", 10001)

	// Add a second session for the same user manually
	svc.AddSession(&iam.Session{
		ID:       "sess-extra",
		UserID:   10001,
		TenantID: "tenant-1",
		Created:  time.Now(),
		Expires:  time.Now().Add(24 * time.Hour),
	})

	resp, err := srv.RevokeAllSessions(callerContext("tenant-1"), &iampb.RevokeAllSessionsRequest{
		TenantId: "tenant-1",
		UserId:   "10001",
	})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, resp.RevokedCount, int32(1))
}

func TestSessionServer_RevokeAllSessions_EmptyTenantID(t *testing.T) {
	srv, _ := newTestSessionServer(t)
	_, err := srv.RevokeAllSessions(callerContext("tenant-1"), &iampb.RevokeAllSessionsRequest{
		TenantId: "",
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestSessionServer_RevokeOtherSessions_Success(t *testing.T) {
	srv, svc := newTestSessionServer(t)
	keepSess := svc.CreateTestSession("tenant-1", 10001)
	svc.AddSession(&iam.Session{
		ID:       "sess-other-1",
		UserID:   10001,
		TenantID: "tenant-1",
		Created:  time.Now(),
		Expires:  time.Now().Add(24 * time.Hour),
	})

	resp, err := srv.RevokeOtherSessions(callerContext("tenant-1"), &iampb.RevokeOtherSessionsRequest{
		TenantId:      "tenant-1",
		KeepSessionId: keepSess.ID,
	})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, resp.RevokedCount, int32(1))

	// Verify the kept session still exists
	getResp, err := srv.GetSession(context.Background(), &iampb.GetSessionRequest{
		SessionId: keepSess.ID,
	})
	require.NoError(t, err)
	assert.Equal(t, keepSess.ID, getResp.Id)
}

func TestSessionServer_RevokeOtherSessions_EmptyTenantID(t *testing.T) {
	srv, _ := newTestSessionServer(t)
	_, err := srv.RevokeOtherSessions(callerContext("tenant-1"), &iampb.RevokeOtherSessionsRequest{
		TenantId:      "",
		KeepSessionId: "sess-1",
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestSessionServer_RevokeOtherSessions_EmptyKeepSessionID(t *testing.T) {
	srv, _ := newTestSessionServer(t)
	_, err := srv.RevokeOtherSessions(callerContext("tenant-1"), &iampb.RevokeOtherSessionsRequest{
		TenantId:      "tenant-1",
		KeepSessionId: "",
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

// =============================================================================
// sessionToProto Tests
// =============================================================================

func TestSessionToProto_NilSession(t *testing.T) {
	result := sessionToProto(nil)
	assert.Nil(t, result)
}

func TestSessionToProto_FullSession(t *testing.T) {
	now := time.Now()
	sess := &iam.Session{
		ID:           "sess-123",
		UserID:       10001,
		TenantID:     "tenant-1",
		Created:      now,
		LastActivity: now.Add(5 * time.Minute),
		Expires:      now.Add(24 * time.Hour),
		MFAVerified:  true,
		IPAddress:    "192.168.1.1",
		UserAgent:    "Mozilla/5.0",
		AuthMethod:   "password",
	}

	result := sessionToProto(sess)
	assert.NotNil(t, result)
	assert.Equal(t, "sess-123", result.Id)
	assert.Equal(t, "10001", result.UserId)
	assert.Equal(t, "tenant-1", result.TenantId)
	assert.True(t, result.MfaVerified)
	assert.Equal(t, "192.168.1.1", result.IpAddress)
	assert.Equal(t, "Mozilla/5.0", result.UserAgent)
	assert.Equal(t, "password", result.DeviceInfo)
	assert.NotNil(t, result.CreatedAt)
	assert.NotNil(t, result.LastActive)
	assert.NotNil(t, result.ExpiresAt)
}

// =============================================================================
// SessionServiceAdapter Tests
// =============================================================================

func TestNewSessionServiceAdapter(t *testing.T) {
	adapter := NewSessionServiceAdapter(nil)
	assert.NotNil(t, adapter)
}

// =============================================================================
// MemorySessionService Tests
// =============================================================================

func TestMemorySessionService_CreateAndGet(t *testing.T) {
	svc := NewMemorySessionService()
	assert.NotNil(t, svc)

	sess := svc.CreateTestSession("tenant-1", 10001)
	assert.NotNil(t, sess)
	assert.Equal(t, uint64(10001), sess.UserID)
	assert.Equal(t, "tenant-1", sess.TenantID)

	got, err := svc.GetSession(context.Background(), sess.ID)
	require.NoError(t, err)
	assert.Equal(t, sess.ID, got.ID)
}

func TestMemorySessionService_GetSession_NotFound(t *testing.T) {
	svc := NewMemorySessionService()
	_, err := svc.GetSession(context.Background(), "nonexistent")
	require.Error(t, err)
}

func TestMemorySessionService_ListSessions_AllForTenant(t *testing.T) {
	svc := NewMemorySessionService()
	svc.CreateTestSession("tenant-1", 10001)
	svc.AddSession(&iam.Session{
		ID:       "sess-extra",
		UserID:   10002,
		TenantID: "tenant-1",
		Created:  time.Now(),
		Expires:  time.Now().Add(time.Hour),
	})

	sessions, total, err := svc.ListSessions(context.Background(), "tenant-1", 0, 0, 0)
	require.NoError(t, err)
	assert.Equal(t, 2, total)
	assert.Len(t, sessions, 2)
}

func TestMemorySessionService_ListSessions_WithPagination(t *testing.T) {
	svc := NewMemorySessionService()
	for i := 0; i < 5; i++ {
		svc.AddSession(&iam.Session{
			ID:       "sess-" + string(rune('a'+i)),
			UserID:   10001,
			TenantID: "tenant-1",
			Created:  time.Now(),
			Expires:  time.Now().Add(time.Hour),
		})
	}

	// With limit
	sessions, total, err := svc.ListSessions(context.Background(), "tenant-1", 10001, 2, 0)
	require.NoError(t, err)
	assert.Equal(t, 5, total)
	assert.Len(t, sessions, 2)

	// With offset past end
	sessions, total, err = svc.ListSessions(context.Background(), "tenant-1", 10001, 0, 100)
	require.NoError(t, err)
	assert.Equal(t, 5, total)
	assert.Empty(t, sessions)
}

func TestMemorySessionService_RevokeSession(t *testing.T) {
	svc := NewMemorySessionService()
	sess := svc.CreateTestSession("tenant-1", 10001)

	err := svc.RevokeSession(context.Background(), sess.ID)
	require.NoError(t, err)

	_, err = svc.GetSession(context.Background(), sess.ID)
	require.Error(t, err)
}

func TestMemorySessionService_RevokeAllSessions(t *testing.T) {
	svc := NewMemorySessionService()
	svc.CreateTestSession("tenant-1", 10001)
	svc.AddSession(&iam.Session{
		ID: "extra-sess", UserID: 10001, TenantID: "tenant-1",
		Created: time.Now(), Expires: time.Now().Add(time.Hour),
	})
	// Different tenant session should not be revoked
	svc.CreateTestSession("tenant-2", 10001)

	count, err := svc.RevokeAllSessions(context.Background(), "tenant-1", 10001)
	require.NoError(t, err)
	assert.Equal(t, 2, count)

	// Verify tenant-2 session remains
	sessions, _, err := svc.ListSessions(context.Background(), "tenant-2", 0, 0, 0)
	require.NoError(t, err)
	assert.Len(t, sessions, 1)
}

func TestMemorySessionService_RevokeOtherSessions(t *testing.T) {
	svc := NewMemorySessionService()
	keepSess := svc.CreateTestSession("tenant-1", 10001)
	svc.AddSession(&iam.Session{
		ID: "other-1", UserID: 10001, TenantID: "tenant-1",
		Created: time.Now(), Expires: time.Now().Add(time.Hour),
	})
	svc.AddSession(&iam.Session{
		ID: "other-2", UserID: 10001, TenantID: "tenant-1",
		Created: time.Now(), Expires: time.Now().Add(time.Hour),
	})

	count, err := svc.RevokeOtherSessions(context.Background(), "tenant-1", keepSess.ID)
	require.NoError(t, err)
	assert.Equal(t, 2, count)

	// Verify kept session still exists
	got, err := svc.GetSession(context.Background(), keepSess.ID)
	require.NoError(t, err)
	assert.Equal(t, keepSess.ID, got.ID)
}

func TestMemorySessionService_AddSession(t *testing.T) {
	svc := NewMemorySessionService()
	sess := &iam.Session{
		ID:       "manual-sess",
		UserID:   10001,
		TenantID: "tenant-1",
		Created:  time.Now(),
		Expires:  time.Now().Add(time.Hour),
	}
	svc.AddSession(sess)

	got, err := svc.GetSession(context.Background(), "manual-sess")
	require.NoError(t, err)
	assert.Equal(t, "manual-sess", got.ID)
}
