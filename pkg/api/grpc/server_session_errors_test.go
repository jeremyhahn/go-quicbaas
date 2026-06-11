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
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// mockSessionService implements SessionService for testing error paths.
type mockSessionService struct {
	listSessionsFunc  func(ctx context.Context, tenantID, userID string, limit, offset int) ([]*iam.Session, int, error)
	getSessionFunc    func(ctx context.Context, sessionID string) (*iam.Session, error)
	revokeSessionFunc func(ctx context.Context, sessionID string) error
	revokeAllFunc     func(ctx context.Context, tenantID, userID string) (int, error)
	revokeOtherFunc   func(ctx context.Context, tenantID, keepSessionID string) (int, error)
}

func (m *mockSessionService) ListSessions(ctx context.Context, tenantID, userID string, limit, offset int) ([]*iam.Session, int, error) {
	if m.listSessionsFunc != nil {
		return m.listSessionsFunc(ctx, tenantID, userID, limit, offset)
	}
	return nil, 0, nil
}

func (m *mockSessionService) GetSession(ctx context.Context, sessionID string) (*iam.Session, error) {
	if m.getSessionFunc != nil {
		return m.getSessionFunc(ctx, sessionID)
	}
	return nil, errors.New("not found")
}

func (m *mockSessionService) RevokeSession(ctx context.Context, sessionID string) error {
	if m.revokeSessionFunc != nil {
		return m.revokeSessionFunc(ctx, sessionID)
	}
	return nil
}

func (m *mockSessionService) RevokeAllSessions(ctx context.Context, tenantID, userID string) (int, error) {
	if m.revokeAllFunc != nil {
		return m.revokeAllFunc(ctx, tenantID, userID)
	}
	return 0, nil
}

func (m *mockSessionService) RevokeOtherSessions(ctx context.Context, tenantID, keepSessionID string) (int, error) {
	if m.revokeOtherFunc != nil {
		return m.revokeOtherFunc(ctx, tenantID, keepSessionID)
	}
	return 0, nil
}

func newMockSessionServer(t *testing.T, svc SessionService) *SessionServer {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	require.NoError(t, err)
	return NewSessionServer(svc, log)
}

func TestSessionServerErrors_ListSessions_ServiceError(t *testing.T) {
	svc := &mockSessionService{
		listSessionsFunc: func(_ context.Context, _, _ string, _, _ int) ([]*iam.Session, int, error) {
			return nil, 0, errors.New("database unavailable")
		},
	}
	srv := newMockSessionServer(t, svc)

	_, err := srv.ListSessions(context.Background(), &iampb.ListSessionsRequest{
		TenantId: "tenant-1",
	})
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
}

func TestSessionServerErrors_RevokeSession_ServiceError(t *testing.T) {
	svc := &mockSessionService{
		revokeSessionFunc: func(_ context.Context, _ string) error {
			return errors.New("revoke failed")
		},
	}
	srv := newMockSessionServer(t, svc)

	_, err := srv.RevokeSession(context.Background(), &iampb.RevokeSessionRequest{
		SessionId: "sess-1",
	})
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
}

func TestSessionServerErrors_RevokeAllSessions_ServiceError(t *testing.T) {
	svc := &mockSessionService{
		revokeAllFunc: func(_ context.Context, _, _ string) (int, error) {
			return 0, errors.New("revoke all failed")
		},
	}
	srv := newMockSessionServer(t, svc)

	_, err := srv.RevokeAllSessions(context.Background(), &iampb.RevokeAllSessionsRequest{
		TenantId: "tenant-1",
		UserId:   "user-1",
	})
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
}

func TestSessionServerErrors_RevokeOtherSessions_ServiceError(t *testing.T) {
	svc := &mockSessionService{
		revokeOtherFunc: func(_ context.Context, _, _ string) (int, error) {
			return 0, errors.New("revoke other failed")
		},
	}
	srv := newMockSessionServer(t, svc)

	_, err := srv.RevokeOtherSessions(context.Background(), &iampb.RevokeOtherSessionsRequest{
		TenantId:      "tenant-1",
		KeepSessionId: "sess-keep",
	})
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
}
