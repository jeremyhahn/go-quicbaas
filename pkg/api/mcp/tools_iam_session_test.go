//go:build mcp

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

package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Mock Session Manager
// =============================================================================

type mockSessionManager struct {
	createFn         func(ctx context.Context, session *iam.SessionExt) error
	listFn           func(ctx context.Context, userID uint64, tenantID string) ([]*iam.SessionExt, error)
	getFn            func(ctx context.Context, sessionID string) (*iam.SessionExt, error)
	revokeFn         func(ctx context.Context, sessionID string) error
	revokeAllFn      func(ctx context.Context, userID uint64, tenantID string) error
	updateActivityFn func(ctx context.Context, sessionID string) error
	revokeExceptFn   func(ctx context.Context, userID uint64, tenantID, exceptSessionID string) error
}

func (m *mockSessionManager) Create(ctx context.Context, session *iam.SessionExt) error {
	if m.createFn != nil {
		return m.createFn(ctx, session)
	}
	return nil
}

func (m *mockSessionManager) List(ctx context.Context, userID uint64, tenantID string) ([]*iam.SessionExt, error) {
	if m.listFn != nil {
		return m.listFn(ctx, userID, tenantID)
	}
	return []*iam.SessionExt{
		{ID: "sess-1", UserID: userID, TenantID: tenantID, Created: time.Now()},
	}, nil
}

func (m *mockSessionManager) Get(ctx context.Context, sessionID string) (*iam.SessionExt, error) {
	if m.getFn != nil {
		return m.getFn(ctx, sessionID)
	}
	return &iam.SessionExt{
		ID:         sessionID,
		UserID:     10001,
		TenantID:   "tenant-1",
		Created:    time.Now().Add(-1 * time.Hour),
		LastActive: time.Now(),
	}, nil
}

func (m *mockSessionManager) Revoke(ctx context.Context, sessionID string) error {
	if m.revokeFn != nil {
		return m.revokeFn(ctx, sessionID)
	}
	return nil
}

func (m *mockSessionManager) UpdateActivity(ctx context.Context, sessionID string) error {
	if m.updateActivityFn != nil {
		return m.updateActivityFn(ctx, sessionID)
	}
	return nil
}

func (m *mockSessionManager) RevokeAllForUser(ctx context.Context, userID uint64, tenantID string) error {
	if m.revokeAllFn != nil {
		return m.revokeAllFn(ctx, userID, tenantID)
	}
	return nil
}

func (m *mockSessionManager) RevokeAllExcept(ctx context.Context, userID uint64, tenantID, exceptSessionID string) error {
	if m.revokeExceptFn != nil {
		return m.revokeExceptFn(ctx, userID, tenantID, exceptSessionID)
	}
	return nil
}

var _ service.SessionManager = (*mockSessionManager)(nil)

// =============================================================================
// Session Mock Service
// =============================================================================

type sessionMockService struct {
	mockService
	sessionMgr service.SessionManager
}

// ResolveCallerAccount resolves the caller as the service does for a subject
// carrying a verified account ID: UserID names the account, never Subject.ID.
// A subject without one is refused.
func (s *sessionMockService) ResolveCallerAccount(_ context.Context, subject interfaces.Subject) (uint64, error) {
	if subject.UserID == 0 {
		return 0, &qberrors.QBError{
			Code: qberrors.ErrForbidden,
			Op:   "Service.ResolveCallerAccount",
			Err:  &service.CallerAccountRefusedError{Reason: "the authenticated caller does not name a user account"},
		}
	}
	return subject.UserID, nil
}

func (m *sessionMockService) GetSessionManager(_ context.Context) (service.SessionManager, error) {
	return m.sessionMgr, nil
}

func newSessionRegistry(t *testing.T, mgr service.SessionManager) *ToolRegistry {
	t.Helper()
	log := newTestLogger(t)
	svc := &sessionMockService{sessionMgr: mgr}
	return &ToolRegistry{service: svc, logger: log}
}

// =============================================================================
// getSessionManager Tests
// =============================================================================

func TestGetSessionManager_ServiceDoesNotSupport(t *testing.T) {
	log := newTestLogger(t)
	reg := &ToolRegistry{service: &mockService{}, logger: log}

	_, err := reg.getSessionManager(context.Background())
	require.Error(t, err)
}

func TestGetSessionManager_NilManager(t *testing.T) {
	reg := newSessionRegistry(t, nil)

	_, err := reg.getSessionManager(context.Background())
	require.Error(t, err)
}

func TestGetSessionManager_Success(t *testing.T) {
	mgr := &mockSessionManager{}
	reg := newSessionRegistry(t, mgr)

	sm, err := reg.getSessionManager(context.Background())
	require.NoError(t, err)
	require.NotNil(t, sm)
}

// =============================================================================
// Self-service caller helpers
// =============================================================================

const (
	sessionCallerID     = "10001"
	sessionCallerTenant = "tenant-1"
)

// sessionCallerCtx binds the authenticated caller the self-service session
// tools act for; the mock manager's default sessions belong to this caller.
func sessionCallerCtx() context.Context {
	return auth.SubjectIntoContext(context.Background(), interfaces.Subject{
		ID:            sessionCallerID,
		TenantID:      sessionCallerTenant,
		UserID:        10001,
		Authenticated: true,
	})
}

// hashedSession is a stored session carrying both credential hashes, so a
// test can prove neither reaches a tool result.
func hashedSession(id string, userID uint64, tenantID string) *iam.SessionExt {
	return &iam.SessionExt{
		ID:          id,
		UserID:      userID,
		TenantID:    tenantID,
		TokenHash:   "token-hash-secret",
		RefreshHash: "refresh-hash-secret",
		Created:     time.Now().Add(-time.Hour),
		LastActive:  time.Now(),
		ExpiresAt:   time.Now().Add(time.Hour),
	}
}

// requireForbidden asserts err is the typed insufficient-permissions refusal.
func requireForbidden(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	var qbErr *qberrors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, qberrors.ErrForbidden, qbErr.Code)
}

// requireNoHashes asserts the JSON form of a tool result carries no token or
// refresh hash.
func requireNoHashes(t *testing.T, result interface{}) {
	t.Helper()
	raw, err := json.Marshal(result)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "token_hash")
	assert.NotContains(t, string(raw), "refresh_hash")
	assert.NotContains(t, string(raw), "hash-secret")
}

// noCallerContexts are the contexts a self-service tool must refuse: none,
// an unauthenticated subject, and an authenticated subject with no ID.
func noCallerContexts() map[string]context.Context {
	return map[string]context.Context{
		"no subject": context.Background(),
		"unauthenticated": auth.SubjectIntoContext(context.Background(), interfaces.Subject{
			ID: sessionCallerID, TenantID: sessionCallerTenant,
		}),
		"empty subject ID": auth.SubjectIntoContext(context.Background(), interfaces.Subject{
			TenantID: sessionCallerTenant, Authenticated: true,
		}),
	}
}

// =============================================================================
// Self-service grants
// =============================================================================

// TestSelfServiceSessionTools_DeclareAuthSessionGrant pins the four
// self-service tools to the grant the embedded transport's quicbaas.Auth
// session verbs declare (sdk/go/transport/embedded/authorize.go): one
// authorization semantics per operation on every transport (ruling 207).
func TestSelfServiceSessionTools_DeclareAuthSessionGrant(t *testing.T) {
	tools := []string{
		"quicbaas_iam_session_get",
		"quicbaas_iam_session_list",
		"quicbaas_iam_session_revoke",
		"quicbaas_iam_session_revoke_all",
	}
	for _, tool := range tools {
		resource, action, ok := getToolQRDBPermission(tool)
		require.True(t, ok, "%s is unmapped", tool)
		assert.Equal(t, "user_authfactors:login", resource+":"+action, tool)
	}
}

func TestSelfServiceSessionTools_AreTenantScoped(t *testing.T) {
	for _, tool := range []string{"quicbaas_iam_session_list", "quicbaas_iam_session_revoke_all"} {
		_, scoped := iamTenantScopedTools[tool]
		assert.True(t, scoped, "%s must check tenant scope", tool)
	}
}

// =============================================================================
// Session Get Tests
// =============================================================================

func TestExecuteIAMSessionGet_Success(t *testing.T) {
	mgr := &mockSessionManager{
		getFn: func(_ context.Context, sessionID string) (*iam.SessionExt, error) {
			return hashedSession(sessionID, 10001, sessionCallerTenant), nil
		},
	}
	reg := newSessionRegistry(t, mgr)

	result, err := reg.executeIAMSessionGet(sessionCallerCtx(), map[string]interface{}{
		"session_id": "sess-123",
	})
	require.NoError(t, err)
	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "sess-123", resultMap["id"])
	requireNoHashes(t, result)
}

func TestExecuteIAMSessionGet_Error(t *testing.T) {
	mgr := &mockSessionManager{
		getFn: func(_ context.Context, _ string) (*iam.SessionExt, error) {
			return nil, errors.New("session not found")
		},
	}
	reg := newSessionRegistry(t, mgr)

	_, err := reg.executeIAMSessionGet(sessionCallerCtx(), map[string]interface{}{
		"session_id": "nonexistent",
	})
	require.Error(t, err)
}

func TestExecuteIAMSessionGet_RefusesForeignSession(t *testing.T) {
	foreign := map[string]*iam.SessionExt{
		"other user":   hashedSession("sess-x", 20002, sessionCallerTenant),
		"other tenant": hashedSession("sess-x", 10001, "tenant-2"),
		"nil session":  nil,
	}
	for name, session := range foreign {
		t.Run(name, func(t *testing.T) {
			mgr := &mockSessionManager{
				getFn: func(_ context.Context, _ string) (*iam.SessionExt, error) {
					return session, nil
				},
			}
			reg := newSessionRegistry(t, mgr)

			result, err := reg.executeIAMSessionGet(sessionCallerCtx(), map[string]interface{}{
				"session_id": "sess-x",
			})
			requireForbidden(t, err)
			assert.Nil(t, result)
		})
	}
}

func TestExecuteIAMSessionGet_RefusesNoCaller(t *testing.T) {
	for name, ctx := range noCallerContexts() {
		t.Run(name, func(t *testing.T) {
			mgr := &mockSessionManager{
				getFn: func(_ context.Context, _ string) (*iam.SessionExt, error) {
					t.Fatal("a call with no caller must be refused before any lookup")
					return nil, nil
				},
			}
			reg := newSessionRegistry(t, mgr)

			_, err := reg.executeIAMSessionGet(ctx, map[string]interface{}{
				"session_id": "sess-123",
			})
			requireForbidden(t, err)
		})
	}
}

// =============================================================================
// Session List Tests
// =============================================================================

func TestExecuteIAMSessionList_ActsOnCaller(t *testing.T) {
	argSets := map[string]map[string]interface{}{
		"no arguments":     {},
		"caller's own IDs": {"tenant_id": sessionCallerTenant, "user_id": sessionCallerID},
	}
	for name, args := range argSets {
		t.Run(name, func(t *testing.T) {
			var gotUser uint64
			var gotTenant string
			mgr := &mockSessionManager{
				listFn: func(_ context.Context, userID uint64, tenantID string) ([]*iam.SessionExt, error) {
					gotUser, gotTenant = userID, tenantID
					return []*iam.SessionExt{hashedSession("sess-1", userID, tenantID), nil}, nil
				},
			}
			reg := newSessionRegistry(t, mgr)

			result, err := reg.executeIAMSessionList(sessionCallerCtx(), args)
			require.NoError(t, err)
			assert.Equal(t, uint64(10001), gotUser)
			assert.Equal(t, sessionCallerTenant, gotTenant)

			resultMap, ok := result.(map[string]interface{})
			require.True(t, ok)
			sessions, ok := resultMap["sessions"].([]interface{})
			require.True(t, ok)
			assert.Len(t, sessions, 1, "a nil stored session is dropped")
			requireNoHashes(t, result)
		})
	}
}

func TestExecuteIAMSessionList_RefusesForeignOwner(t *testing.T) {
	argSets := map[string]map[string]interface{}{
		"other user_id":   {"user_id": "20002"},
		"empty user_id":   {"user_id": ""},
		"other tenant_id": {"tenant_id": "tenant-2"},
	}
	for name, args := range argSets {
		t.Run(name, func(t *testing.T) {
			mgr := &mockSessionManager{
				listFn: func(_ context.Context, _ uint64, _ string) ([]*iam.SessionExt, error) {
					t.Fatal("a foreign owner must be refused before any lookup")
					return nil, nil
				},
			}
			reg := newSessionRegistry(t, mgr)

			_, err := reg.executeIAMSessionList(sessionCallerCtx(), args)
			requireForbidden(t, err)
		})
	}
}

func TestExecuteIAMSessionList_RefusesNoCaller(t *testing.T) {
	for name, ctx := range noCallerContexts() {
		t.Run(name, func(t *testing.T) {
			reg := newSessionRegistry(t, &mockSessionManager{})

			_, err := reg.executeIAMSessionList(ctx, map[string]interface{}{})
			requireForbidden(t, err)
		})
	}
}

func TestExecuteIAMSessionList_RefusesNonNumericSubject(t *testing.T) {
	reg := newSessionRegistry(t, &mockSessionManager{})
	ctx := auth.SubjectIntoContext(context.Background(), interfaces.Subject{
		ID: "alice", TenantID: sessionCallerTenant, Authenticated: true,
	})

	_, err := reg.executeIAMSessionList(ctx, map[string]interface{}{})
	requireForbidden(t, err)
}

// loginNameCallerCtx binds a caller whose subject name is its login name, as
// auth.SubjectFromIdentity projects an mTLS or bearer identity: the account
// ID arrives only as UserID.
func loginNameCallerCtx(name string) context.Context {
	return auth.SubjectIntoContext(context.Background(), interfaces.Subject{
		ID:            name,
		TenantID:      sessionCallerTenant,
		UserID:        10001,
		Authenticated: true,
	})
}

func TestSessionTools_ServeLoginNameCaller(t *testing.T) {
	var listedUser uint64
	var revokedID string
	mgr := &mockSessionManager{
		listFn: func(_ context.Context, userID uint64, tenantID string) ([]*iam.SessionExt, error) {
			listedUser = userID
			return []*iam.SessionExt{hashedSession("sess-1", userID, tenantID)}, nil
		},
		revokeFn: func(_ context.Context, sessionID string) error {
			revokedID = sessionID
			return nil
		},
	}
	reg := newSessionRegistry(t, mgr)
	ctx := loginNameCallerCtx("alice")

	_, err := reg.executeIAMSessionList(ctx, map[string]interface{}{"user_id": sessionCallerID})
	require.NoError(t, err)
	assert.Equal(t, uint64(10001), listedUser)

	_, err = reg.executeIAMSessionGet(ctx, map[string]interface{}{"session_id": "sess-1"})
	require.NoError(t, err)

	_, err = reg.executeIAMSessionRevoke(ctx, map[string]interface{}{"session_id": "sess-1"})
	require.NoError(t, err)
	assert.Equal(t, "sess-1", revokedID)
}

func TestSessionTools_NumericUsernameNeverReachesThatAccount(t *testing.T) {
	const otherAccount = 20002
	var listedUser, revokedAllUser uint64
	revoked := false
	mgr := &mockSessionManager{
		listFn: func(_ context.Context, userID uint64, _ string) ([]*iam.SessionExt, error) {
			listedUser = userID
			return nil, nil
		},
		getFn: func(_ context.Context, sessionID string) (*iam.SessionExt, error) {
			return hashedSession(sessionID, otherAccount, sessionCallerTenant), nil
		},
		revokeFn: func(_ context.Context, _ string) error {
			revoked = true
			return nil
		},
		revokeAllFn: func(_ context.Context, userID uint64, _ string) error {
			revokedAllUser = userID
			return nil
		},
	}
	reg := newSessionRegistry(t, mgr)
	ctx := loginNameCallerCtx("20002")

	_, err := reg.executeIAMSessionList(ctx, map[string]interface{}{"user_id": "20002"})
	requireForbidden(t, err)
	_, err = reg.executeIAMSessionList(ctx, map[string]interface{}{})
	require.NoError(t, err)
	assert.Equal(t, uint64(10001), listedUser, "the caller's own account, not the one its username spells")

	_, err = reg.executeIAMSessionGet(ctx, map[string]interface{}{"session_id": "sess-other"})
	requireForbidden(t, err)

	_, err = reg.executeIAMSessionRevoke(ctx, map[string]interface{}{"session_id": "sess-other"})
	requireForbidden(t, err)
	assert.False(t, revoked)

	_, err = reg.executeIAMSessionRevokeAll(ctx, map[string]interface{}{"user_id": "20002"})
	requireForbidden(t, err)
	_, err = reg.executeIAMSessionRevokeAll(ctx, map[string]interface{}{})
	require.NoError(t, err)
	assert.Equal(t, uint64(10001), revokedAllUser)
}

func TestExecuteIAMSessionList_Error(t *testing.T) {
	mgr := &mockSessionManager{
		listFn: func(_ context.Context, _ uint64, _ string) ([]*iam.SessionExt, error) {
			return nil, errors.New("list failed")
		},
	}
	reg := newSessionRegistry(t, mgr)

	_, err := reg.executeIAMSessionList(sessionCallerCtx(), map[string]interface{}{})
	require.Error(t, err)
}

// =============================================================================
// Session Revoke Tests
// =============================================================================

func TestExecuteIAMSessionRevoke_Success(t *testing.T) {
	var revokedID string
	mgr := &mockSessionManager{
		revokeFn: func(_ context.Context, sessionID string) error {
			revokedID = sessionID
			return nil
		},
	}
	reg := newSessionRegistry(t, mgr)

	result, err := reg.executeIAMSessionRevoke(sessionCallerCtx(), map[string]interface{}{
		"session_id": "sess-123",
	})
	require.NoError(t, err)
	assert.Equal(t, "sess-123", revokedID)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, true, resultMap["revoked"])
	requireNoHashes(t, result)
}

func TestExecuteIAMSessionRevoke_RefusesForeignSession(t *testing.T) {
	foreign := map[string]*iam.SessionExt{
		"other user":   hashedSession("sess-x", 20002, sessionCallerTenant),
		"other tenant": hashedSession("sess-x", 10001, "tenant-2"),
	}
	for name, session := range foreign {
		t.Run(name, func(t *testing.T) {
			mgr := &mockSessionManager{
				getFn: func(_ context.Context, _ string) (*iam.SessionExt, error) {
					return session, nil
				},
				revokeFn: func(_ context.Context, _ string) error {
					t.Fatal("another user's session must never be revoked")
					return nil
				},
			}
			reg := newSessionRegistry(t, mgr)

			_, err := reg.executeIAMSessionRevoke(sessionCallerCtx(), map[string]interface{}{
				"session_id": "sess-x",
			})
			requireForbidden(t, err)
		})
	}
}

func TestExecuteIAMSessionRevoke_RefusesNoCaller(t *testing.T) {
	for name, ctx := range noCallerContexts() {
		t.Run(name, func(t *testing.T) {
			mgr := &mockSessionManager{
				revokeFn: func(_ context.Context, _ string) error {
					t.Fatal("a call with no caller must never revoke")
					return nil
				},
			}
			reg := newSessionRegistry(t, mgr)

			_, err := reg.executeIAMSessionRevoke(ctx, map[string]interface{}{
				"session_id": "sess-123",
			})
			requireForbidden(t, err)
		})
	}
}

func TestExecuteIAMSessionRevoke_GetError(t *testing.T) {
	mgr := &mockSessionManager{
		getFn: func(_ context.Context, _ string) (*iam.SessionExt, error) {
			return nil, errors.New("session not found")
		},
	}
	reg := newSessionRegistry(t, mgr)

	_, err := reg.executeIAMSessionRevoke(sessionCallerCtx(), map[string]interface{}{
		"session_id": "nonexistent",
	})
	require.Error(t, err)
}

func TestExecuteIAMSessionRevoke_RevokeError(t *testing.T) {
	mgr := &mockSessionManager{
		revokeFn: func(_ context.Context, _ string) error {
			return errors.New("revoke failed")
		},
	}
	reg := newSessionRegistry(t, mgr)

	_, err := reg.executeIAMSessionRevoke(sessionCallerCtx(), map[string]interface{}{
		"session_id": "sess-123",
	})
	require.Error(t, err)
}

// =============================================================================
// Session Revoke All Tests
// =============================================================================

func TestExecuteIAMSessionRevokeAll_ActsOnCaller(t *testing.T) {
	argSets := map[string]map[string]interface{}{
		"no arguments":     {},
		"caller's own IDs": {"tenant_id": sessionCallerTenant, "user_id": sessionCallerID},
	}
	for name, args := range argSets {
		t.Run(name, func(t *testing.T) {
			var gotUser uint64
			var gotTenant string
			mgr := &mockSessionManager{
				revokeAllFn: func(_ context.Context, userID uint64, tenantID string) error {
					gotUser, gotTenant = userID, tenantID
					return nil
				},
			}
			reg := newSessionRegistry(t, mgr)

			result, err := reg.executeIAMSessionRevokeAll(sessionCallerCtx(), args)
			require.NoError(t, err)
			assert.Equal(t, uint64(10001), gotUser)
			assert.Equal(t, sessionCallerTenant, gotTenant)

			resultMap, ok := result.(map[string]interface{})
			require.True(t, ok)
			assert.Equal(t, true, resultMap["revoked"])
		})
	}
}

func TestExecuteIAMSessionRevokeAll_RefusesForeignOwner(t *testing.T) {
	argSets := map[string]map[string]interface{}{
		"other user_id":   {"tenant_id": sessionCallerTenant, "user_id": "30001"},
		"other tenant_id": {"tenant_id": "t1", "user_id": sessionCallerID},
	}
	for name, args := range argSets {
		t.Run(name, func(t *testing.T) {
			mgr := &mockSessionManager{
				revokeAllFn: func(_ context.Context, _ uint64, _ string) error {
					t.Fatal("another user's sessions must never be revoked")
					return nil
				},
			}
			reg := newSessionRegistry(t, mgr)

			_, err := reg.executeIAMSessionRevokeAll(sessionCallerCtx(), args)
			requireForbidden(t, err)
		})
	}
}

func TestExecuteIAMSessionRevokeAll_RefusesNoCaller(t *testing.T) {
	for name, ctx := range noCallerContexts() {
		t.Run(name, func(t *testing.T) {
			mgr := &mockSessionManager{
				revokeAllFn: func(_ context.Context, _ uint64, _ string) error {
					t.Fatal("a call with no caller must never revoke")
					return nil
				},
			}
			reg := newSessionRegistry(t, mgr)

			_, err := reg.executeIAMSessionRevokeAll(ctx, map[string]interface{}{})
			requireForbidden(t, err)
		})
	}
}

func TestExecuteIAMSessionRevokeAll_Error(t *testing.T) {
	mgr := &mockSessionManager{
		revokeAllFn: func(_ context.Context, _ uint64, _ string) error {
			return errors.New("revoke all failed")
		},
	}
	reg := newSessionRegistry(t, mgr)

	_, err := reg.executeIAMSessionRevokeAll(sessionCallerCtx(), map[string]interface{}{})
	require.Error(t, err)
}

// =============================================================================
// Session Revoke Others Tests
// =============================================================================

func TestExecuteIAMSessionRevokeOthers_Success(t *testing.T) {
	mgr := &mockSessionManager{}
	reg := newSessionRegistry(t, mgr)

	result, err := reg.executeIAMSessionRevokeOthers(context.Background(), map[string]interface{}{
		"tenant_id":       "t1",
		"user_id":         "30001",
		"keep_session_id": "sess-keep",
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, true, resultMap["revoked"])
}

func TestExecuteIAMSessionRevokeOthers_Error(t *testing.T) {
	mgr := &mockSessionManager{
		revokeExceptFn: func(_ context.Context, _ uint64, _, _ string) error {
			return errors.New("revoke others failed")
		},
	}
	reg := newSessionRegistry(t, mgr)

	_, err := reg.executeIAMSessionRevokeOthers(context.Background(), map[string]interface{}{
		"tenant_id": "t1", "user_id": "30001", "keep_session_id": "sess-keep",
	})
	require.Error(t, err)
}

// =============================================================================
// Session Touch Tests
// =============================================================================

func TestExecuteIAMSessionTouch_Success(t *testing.T) {
	var touchedID string
	mgr := &mockSessionManager{
		updateActivityFn: func(_ context.Context, sessionID string) error {
			touchedID = sessionID
			return nil
		},
		createFn: func(_ context.Context, _ *iam.SessionExt) error {
			t.Fatal("Touch must not call Create")
			return nil
		},
	}
	reg := newSessionRegistry(t, mgr)

	result, err := reg.executeIAMSessionTouch(context.Background(), map[string]interface{}{
		"session_id": "sess-123",
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "sess-123", touchedID)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, true, resultMap["touched"])
	assert.Contains(t, resultMap, "timestamp")
}

func TestExecuteIAMSessionTouch_EmptySessionID(t *testing.T) {
	mgr := &mockSessionManager{
		updateActivityFn: func(_ context.Context, _ string) error {
			t.Fatal("an empty session_id must be refused before any service call")
			return nil
		},
	}
	reg := newSessionRegistry(t, mgr)

	_, err := reg.executeIAMSessionTouch(context.Background(), map[string]interface{}{})
	require.Error(t, err)
	var qbErr *qberrors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, qberrors.ErrInvalidInput, qbErr.Code)
}

func TestExecuteIAMSessionTouch_UpdateActivityError(t *testing.T) {
	mgr := &mockSessionManager{
		updateActivityFn: func(_ context.Context, _ string) error {
			return iam.ErrSessionExpired
		},
	}
	reg := newSessionRegistry(t, mgr)

	_, err := reg.executeIAMSessionTouch(context.Background(), map[string]interface{}{
		"session_id": "sess-123",
	})
	assert.ErrorIs(t, err, iam.ErrSessionExpired)
}
