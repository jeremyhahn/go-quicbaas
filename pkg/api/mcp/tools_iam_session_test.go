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
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Mock Session Manager
// =============================================================================

type mockSessionManager struct {
	createFn       func(ctx context.Context, session *iam.SessionExt) error
	listFn         func(ctx context.Context, userID uint64, tenantID string) ([]*iam.SessionExt, error)
	getFn          func(ctx context.Context, sessionID string) (*iam.SessionExt, error)
	revokeFn       func(ctx context.Context, sessionID string) error
	revokeAllFn    func(ctx context.Context, userID uint64, tenantID string) error
	revokeExceptFn func(ctx context.Context, userID uint64, tenantID, exceptSessionID string) error
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
// Session Get Tests
// =============================================================================

func TestExecuteIAMSessionGet_Success(t *testing.T) {
	mgr := &mockSessionManager{}
	reg := newSessionRegistry(t, mgr)

	result, err := reg.executeIAMSessionGet(context.Background(), map[string]interface{}{
		"session_id": "sess-123",
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestExecuteIAMSessionGet_Error(t *testing.T) {
	mgr := &mockSessionManager{
		getFn: func(_ context.Context, _ string) (*iam.SessionExt, error) {
			return nil, errors.New("session not found")
		},
	}
	reg := newSessionRegistry(t, mgr)

	_, err := reg.executeIAMSessionGet(context.Background(), map[string]interface{}{
		"session_id": "nonexistent",
	})
	require.Error(t, err)
}

// =============================================================================
// Session List Tests
// =============================================================================

func TestExecuteIAMSessionList_Success(t *testing.T) {
	mgr := &mockSessionManager{}
	reg := newSessionRegistry(t, mgr)

	result, err := reg.executeIAMSessionList(context.Background(), map[string]interface{}{
		"tenant_id": "t1",
		"user_id":   "30001",
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.NotNil(t, resultMap["sessions"])
}

func TestExecuteIAMSessionList_Error(t *testing.T) {
	mgr := &mockSessionManager{
		listFn: func(_ context.Context, _ uint64, _ string) ([]*iam.SessionExt, error) {
			return nil, errors.New("list failed")
		},
	}
	reg := newSessionRegistry(t, mgr)

	_, err := reg.executeIAMSessionList(context.Background(), map[string]interface{}{
		"tenant_id": "t1", "user_id": "30001",
	})
	require.Error(t, err)
}

// =============================================================================
// Session Revoke Tests
// =============================================================================

func TestExecuteIAMSessionRevoke_Success(t *testing.T) {
	mgr := &mockSessionManager{}
	reg := newSessionRegistry(t, mgr)

	result, err := reg.executeIAMSessionRevoke(context.Background(), map[string]interface{}{
		"session_id": "sess-123",
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, true, resultMap["revoked"])
}

func TestExecuteIAMSessionRevoke_GetError(t *testing.T) {
	mgr := &mockSessionManager{
		getFn: func(_ context.Context, _ string) (*iam.SessionExt, error) {
			return nil, errors.New("session not found")
		},
	}
	reg := newSessionRegistry(t, mgr)

	_, err := reg.executeIAMSessionRevoke(context.Background(), map[string]interface{}{
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

	_, err := reg.executeIAMSessionRevoke(context.Background(), map[string]interface{}{
		"session_id": "sess-123",
	})
	require.Error(t, err)
}

// =============================================================================
// Session Revoke All Tests
// =============================================================================

func TestExecuteIAMSessionRevokeAll_Success(t *testing.T) {
	mgr := &mockSessionManager{}
	reg := newSessionRegistry(t, mgr)

	result, err := reg.executeIAMSessionRevokeAll(context.Background(), map[string]interface{}{
		"tenant_id": "t1",
		"user_id":   "30001",
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, true, resultMap["revoked"])
}

func TestExecuteIAMSessionRevokeAll_Error(t *testing.T) {
	mgr := &mockSessionManager{
		revokeAllFn: func(_ context.Context, _ uint64, _ string) error {
			return errors.New("revoke all failed")
		},
	}
	reg := newSessionRegistry(t, mgr)

	_, err := reg.executeIAMSessionRevokeAll(context.Background(), map[string]interface{}{
		"tenant_id": "t1", "user_id": "30001",
	})
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
	mgr := &mockSessionManager{}
	reg := newSessionRegistry(t, mgr)

	result, err := reg.executeIAMSessionTouch(context.Background(), map[string]interface{}{
		"session_id": "sess-123",
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, true, resultMap["touched"])
}

func TestExecuteIAMSessionTouch_GetError(t *testing.T) {
	mgr := &mockSessionManager{
		getFn: func(_ context.Context, _ string) (*iam.SessionExt, error) {
			return nil, errors.New("session not found")
		},
	}
	reg := newSessionRegistry(t, mgr)

	_, err := reg.executeIAMSessionTouch(context.Background(), map[string]interface{}{
		"session_id": "nonexistent",
	})
	require.Error(t, err)
}

func TestExecuteIAMSessionTouch_CreateError(t *testing.T) {
	mgr := &mockSessionManager{
		createFn: func(_ context.Context, _ *iam.SessionExt) error {
			return errors.New("create failed")
		},
	}
	reg := newSessionRegistry(t, mgr)

	_, err := reg.executeIAMSessionTouch(context.Background(), map[string]interface{}{
		"session_id": "sess-123",
	})
	require.Error(t, err)
}
