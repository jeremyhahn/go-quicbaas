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
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// SessionServer implements the Session gRPC service.
type SessionServer struct {
	iampb.UnimplementedSessionServiceServer
	sessionService SessionService
	logger         *logger.Logger
}

// SessionService defines the interface for session operations.
type SessionService interface {
	ListSessions(ctx context.Context, tenantID, userID string, limit, offset int) ([]*iam.Session, int, error)
	GetSession(ctx context.Context, sessionID string) (*iam.Session, error)
	RevokeSession(ctx context.Context, sessionID string) error
	RevokeAllSessions(ctx context.Context, tenantID, userID string) (int, error)
	RevokeOtherSessions(ctx context.Context, tenantID, keepSessionID string) (int, error)
}

// NewSessionServer creates a new Session gRPC server.
func NewSessionServer(sessionService SessionService, log *logger.Logger) *SessionServer {
	return &SessionServer{
		sessionService: sessionService,
		logger:         log.WithField("component", "session-grpc-server"),
	}
}

// ListSessions lists sessions for a tenant/user.
func (s *SessionServer) ListSessions(ctx context.Context, req *iampb.ListSessionsRequest) (*iampb.ListSessionsResponse, error) {
	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}

	sessions, total, err := s.sessionService.ListSessions(ctx, req.TenantId, req.UserId, int(req.Limit), int(req.Offset))
	if err != nil {
		s.logger.WithError(err).Error("Failed to list sessions")
		return nil, status.Errorf(codes.Internal, "failed to list sessions: %v", err)
	}

	pbSessions := make([]*iampb.Session, len(sessions))
	for i, sess := range sessions {
		pbSessions[i] = sessionToProto(sess)
	}

	return &iampb.ListSessionsResponse{
		Sessions: pbSessions,
		Total:    int32(total),
	}, nil
}

// GetSession retrieves a specific session.
func (s *SessionServer) GetSession(ctx context.Context, req *iampb.GetSessionRequest) (*iampb.Session, error) {
	if req.SessionId == "" {
		return nil, status.Error(codes.InvalidArgument, "session_id is required")
	}

	session, err := s.sessionService.GetSession(ctx, req.SessionId)
	if err != nil {
		s.logger.WithError(err).Error("Failed to get session")
		return nil, status.Errorf(codes.NotFound, "session not found: %v", err)
	}

	return sessionToProto(session), nil
}

// RevokeSession revokes a specific session.
func (s *SessionServer) RevokeSession(ctx context.Context, req *iampb.RevokeSessionRequest) (*iampb.RevokeSessionResponse, error) {
	if req.SessionId == "" {
		return nil, status.Error(codes.InvalidArgument, "session_id is required")
	}

	err := s.sessionService.RevokeSession(ctx, req.SessionId)
	if err != nil {
		s.logger.WithError(err).Error("Failed to revoke session")
		return nil, status.Errorf(codes.Internal, "failed to revoke session: %v", err)
	}

	return &iampb.RevokeSessionResponse{Success: true}, nil
}

// RevokeAllSessions revokes all sessions for a user/tenant.
func (s *SessionServer) RevokeAllSessions(ctx context.Context, req *iampb.RevokeAllSessionsRequest) (*iampb.RevokeAllSessionsResponse, error) {
	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}

	count, err := s.sessionService.RevokeAllSessions(ctx, req.TenantId, req.UserId)
	if err != nil {
		s.logger.WithError(err).Error("Failed to revoke all sessions")
		return nil, status.Errorf(codes.Internal, "failed to revoke all sessions: %v", err)
	}

	return &iampb.RevokeAllSessionsResponse{RevokedCount: int32(count)}, nil
}

// RevokeOtherSessions revokes all sessions except the specified one.
func (s *SessionServer) RevokeOtherSessions(ctx context.Context, req *iampb.RevokeOtherSessionsRequest) (*iampb.RevokeOtherSessionsResponse, error) {
	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	if req.KeepSessionId == "" {
		return nil, status.Error(codes.InvalidArgument, "keep_session_id is required")
	}

	count, err := s.sessionService.RevokeOtherSessions(ctx, req.TenantId, req.KeepSessionId)
	if err != nil {
		s.logger.WithError(err).Error("Failed to revoke other sessions")
		return nil, status.Errorf(codes.Internal, "failed to revoke other sessions: %v", err)
	}

	return &iampb.RevokeOtherSessionsResponse{RevokedCount: int32(count)}, nil
}

// sessionToProto converts an iam.Session to a proto Session.
func sessionToProto(s *iam.Session) *iampb.Session {
	if s == nil {
		return nil
	}

	return &iampb.Session{
		Id:          s.ID,
		UserId:      s.UserID,
		TenantId:    s.TenantID,
		CreatedAt:   timestamppb.New(s.Created),
		LastActive:  timestamppb.New(s.LastActivity),
		ExpiresAt:   timestamppb.New(s.Expires),
		MfaVerified: s.MFAVerified,
		IpAddress:   s.IPAddress,
		UserAgent:   s.UserAgent,
		DeviceInfo:  s.AuthMethod, // Map AuthMethod to DeviceInfo for client display
	}
}

// SessionServiceAdapter adapts iam components to SessionService interface.
type SessionServiceAdapter struct {
	sessionStorage iam.SessionStorage
}

// NewSessionServiceAdapter creates a new session service adapter.
func NewSessionServiceAdapter(sessionStorage iam.SessionStorage) *SessionServiceAdapter {
	return &SessionServiceAdapter{
		sessionStorage: sessionStorage,
	}
}

// ListSessions lists sessions for a tenant/user.
func (a *SessionServiceAdapter) ListSessions(ctx context.Context, tenantID, userID string, limit, offset int) ([]*iam.Session, int, error) {
	sessions, err := a.sessionStorage.GetSessionsByUser(ctx, tenantID, userID)
	if err != nil {
		return nil, 0, err
	}

	// Apply pagination
	total := len(sessions)
	if offset >= len(sessions) {
		return []*iam.Session{}, total, nil
	}
	sessions = sessions[offset:]
	if limit > 0 && len(sessions) > limit {
		sessions = sessions[:limit]
	}

	return sessions, total, nil
}

// GetSession retrieves a specific session.
func (a *SessionServiceAdapter) GetSession(ctx context.Context, sessionID string) (*iam.Session, error) {
	return a.sessionStorage.GetSession(ctx, sessionID)
}

// RevokeSession revokes a specific session.
func (a *SessionServiceAdapter) RevokeSession(ctx context.Context, sessionID string) error {
	return a.sessionStorage.DeleteSession(ctx, sessionID)
}

// RevokeAllSessions revokes all sessions for a user/tenant.
func (a *SessionServiceAdapter) RevokeAllSessions(ctx context.Context, tenantID, userID string) (int, error) {
	sessions, err := a.sessionStorage.GetSessionsByUser(ctx, tenantID, userID)
	if err != nil {
		return 0, err
	}

	count := 0
	for _, s := range sessions {
		if err := a.sessionStorage.DeleteSession(ctx, s.ID); err == nil {
			count++
		}
	}

	return count, nil
}

// RevokeOtherSessions revokes all sessions except the specified one.
func (a *SessionServiceAdapter) RevokeOtherSessions(ctx context.Context, tenantID, keepSessionID string) (int, error) {
	// Get the session to keep to find the user
	keepSession, err := a.sessionStorage.GetSession(ctx, keepSessionID)
	if err != nil {
		return 0, err
	}

	sessions, err := a.sessionStorage.GetSessionsByUser(ctx, tenantID, keepSession.UserID)
	if err != nil {
		return 0, err
	}

	count := 0
	for _, s := range sessions {
		if s.ID != keepSessionID {
			if err := a.sessionStorage.DeleteSession(ctx, s.ID); err == nil {
				count++
			}
		}
	}

	return count, nil
}

// Compile-time check that SessionServiceAdapter implements SessionService.
var _ SessionService = (*SessionServiceAdapter)(nil)

// MemorySessionService is an in-memory implementation for testing.
type MemorySessionService struct {
	sessions map[string]*iam.Session
}

// NewMemorySessionService creates a new in-memory session service.
func NewMemorySessionService() *MemorySessionService {
	return &MemorySessionService{
		sessions: make(map[string]*iam.Session),
	}
}

// ListSessions lists all sessions.
func (m *MemorySessionService) ListSessions(ctx context.Context, tenantID, userID string, limit, offset int) ([]*iam.Session, int, error) {
	var result []*iam.Session
	for _, s := range m.sessions {
		if (tenantID == "" || s.TenantID == tenantID) && (userID == "" || s.UserID == userID) {
			result = append(result, s)
		}
	}

	total := len(result)
	if offset >= len(result) {
		return []*iam.Session{}, total, nil
	}
	result = result[offset:]
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}

	return result, total, nil
}

// GetSession gets a session by ID.
func (m *MemorySessionService) GetSession(ctx context.Context, sessionID string) (*iam.Session, error) {
	s, ok := m.sessions[sessionID]
	if !ok {
		return nil, status.Error(codes.NotFound, "session not found")
	}
	return s, nil
}

// RevokeSession revokes a session.
func (m *MemorySessionService) RevokeSession(ctx context.Context, sessionID string) error {
	delete(m.sessions, sessionID)
	return nil
}

// RevokeAllSessions revokes all sessions for a tenant/user.
func (m *MemorySessionService) RevokeAllSessions(ctx context.Context, tenantID, userID string) (int, error) {
	count := 0
	for id, s := range m.sessions {
		if (tenantID == "" || s.TenantID == tenantID) && (userID == "" || s.UserID == userID) {
			delete(m.sessions, id)
			count++
		}
	}
	return count, nil
}

// RevokeOtherSessions revokes all sessions except the kept one.
func (m *MemorySessionService) RevokeOtherSessions(ctx context.Context, tenantID, keepSessionID string) (int, error) {
	count := 0
	for id, s := range m.sessions {
		if id != keepSessionID && (tenantID == "" || s.TenantID == tenantID) {
			delete(m.sessions, id)
			count++
		}
	}
	return count, nil
}

// AddSession adds a session for testing.
func (m *MemorySessionService) AddSession(s *iam.Session) {
	m.sessions[s.ID] = s
}

// CreateTestSession creates a test session.
func (m *MemorySessionService) CreateTestSession(tenantID, userID string) *iam.Session {
	now := time.Now()
	s := &iam.Session{
		ID:           "sess-" + tenantID + "-" + userID,
		UserID:       userID,
		TenantID:     tenantID,
		Created:      now,
		LastActivity: now,
		Expires:      now.Add(24 * time.Hour),
		MFAVerified:  false,
		IPAddress:    "127.0.0.1",
		UserAgent:    "test-agent",
		AuthMethod:   "password",
	}
	m.sessions[s.ID] = s
	return s
}

// Compile-time check that MemorySessionService implements SessionService.
var _ SessionService = (*MemorySessionService)(nil)
