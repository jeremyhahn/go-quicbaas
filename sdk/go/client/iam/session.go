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
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

const (
	sessionServiceName = "quicbaas.IAM.Session"
)

// SessionClient provides session management operations.
//
// The client supports:
//   - Listing active sessions for a user
//   - Revoking individual or all sessions
//   - Session activity tracking
type SessionClient interface {
	// Get retrieves a session by ID.
	Get(ctx context.Context, sessionID string) (*Session, error)

	// List lists sessions for a user.
	List(ctx context.Context, req *ListSessionsRequest) (*ListSessionsResponse, error)

	// Revoke revokes a specific session.
	Revoke(ctx context.Context, sessionID string) error

	// RevokeAll revokes all sessions for a user.
	RevokeAll(ctx context.Context, tenantID, userID string) error

	// RevokeOthers revokes all sessions except the current one.
	RevokeOthers(ctx context.Context, keepSessionID string) error

	// Touch updates the last active timestamp for a session.
	Touch(ctx context.Context, sessionID string) error
}

// ListSessionsRequest is a request to list sessions.
type ListSessionsRequest struct {
	// TenantID is the tenant to list sessions for.
	TenantID string `json:"tenant_id"`

	// UserID is the user to list sessions for.
	UserID string `json:"user_id"`

	// IncludeExpired includes expired sessions.
	IncludeExpired bool `json:"include_expired,omitempty"`

	// Limit is the maximum number of sessions to return.
	Limit int `json:"limit,omitempty"`

	// Offset is the pagination offset.
	Offset int `json:"offset,omitempty"`
}

// ListSessionsResponse is the response from listing sessions.
type ListSessionsResponse struct {
	// Sessions is the list of sessions.
	Sessions []Session `json:"sessions"`

	// TotalCount is the total number of sessions.
	TotalCount int `json:"total_count"`

	// HasMore indicates if there are more sessions.
	HasMore bool `json:"has_more"`
}

// Internal request/response types for transport.

type getSessionRequest struct {
	SessionID string `json:"session_id"`
}

type listSessionsRequestInternal struct {
	Request *ListSessionsRequest `json:"request"`
}

type revokeSessionRequest struct {
	SessionID string `json:"session_id"`
}

type revokeSessionResponse struct{}

type revokeAllSessionsRequest struct {
	TenantID string `json:"tenant_id"`
	UserID   string `json:"user_id"`
}

type revokeAllSessionsResponse struct {
	RevokedCount int `json:"revoked_count"`
}

type revokeOthersRequest struct {
	KeepSessionID string `json:"keep_session_id"`
}

type revokeOthersResponse struct {
	RevokedCount int `json:"revoked_count"`
}

type touchSessionRequest struct {
	SessionID string    `json:"session_id"`
	Timestamp time.Time `json:"timestamp"`
}

type touchSessionResponse struct{}

// sessionClient implements the SessionClient interface.
type sessionClient struct {
	transport transport.Transport
}

// NewSessionClient creates a new session management client.
//
// Returns nil if the transport is nil.
func NewSessionClient(t transport.Transport) SessionClient {
	if t == nil {
		return nil
	}
	return &sessionClient{
		transport: t,
	}
}

// Get retrieves a session by ID.
func (c *sessionClient) Get(ctx context.Context, sessionID string) (*Session, error) {
	if sessionID == "" {
		return nil, errors.NewInvalidInput("iam.session.Get", "session ID is required")
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("iam.session.Get", "context is required")
	}

	resp, err := c.transport.Request(ctx, sessionServiceName, "Get", &getSessionRequest{
		SessionID: sessionID,
	})
	if err != nil {
		return nil, err
	}

	return parseSessionResponse(resp)
}

// List lists sessions for a user.
func (c *sessionClient) List(ctx context.Context, req *ListSessionsRequest) (*ListSessionsResponse, error) {
	if err := validateListSessionsRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("iam.session.List", "context is required")
	}

	// Set reasonable defaults.
	if req.Limit <= 0 {
		req.Limit = 100
	}
	if req.Limit > 1000 {
		req.Limit = 1000
	}

	resp, err := c.transport.Request(ctx, sessionServiceName, "List", &listSessionsRequestInternal{
		Request: req,
	})
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return &ListSessionsResponse{Sessions: []Session{}}, nil
	}

	return parseListSessionsResponse(resp)
}

// Revoke revokes a specific session.
func (c *sessionClient) Revoke(ctx context.Context, sessionID string) error {
	if sessionID == "" {
		return errors.NewInvalidInput("iam.session.Revoke", "session ID is required")
	}

	if ctx == nil {
		return errors.NewInvalidInput("iam.session.Revoke", "context is required")
	}

	resp, err := c.transport.Request(ctx, sessionServiceName, "Revoke", &revokeSessionRequest{
		SessionID: sessionID,
	})
	if err != nil {
		return err
	}

	// Accept nil response or valid response.
	if resp == nil {
		return nil
	}

	// Revoke is a void operation. Any non-error response indicates success.
	_ = resp
	return nil
}

// RevokeAll revokes all sessions for a user.
func (c *sessionClient) RevokeAll(ctx context.Context, tenantID, userID string) error {
	if tenantID == "" {
		return errors.NewInvalidInput("iam.session.RevokeAll", "tenant ID is required")
	}
	if userID == "" {
		return errors.NewInvalidInput("iam.session.RevokeAll", "user ID is required")
	}

	if ctx == nil {
		return errors.NewInvalidInput("iam.session.RevokeAll", "context is required")
	}

	resp, err := c.transport.Request(ctx, sessionServiceName, "RevokeAll", &revokeAllSessionsRequest{
		TenantID: tenantID,
		UserID:   userID,
	})
	if err != nil {
		return err
	}

	// Accept nil response or valid response.
	if resp == nil {
		return nil
	}

	// RevokeAll is a void operation. Any non-error response indicates success.
	_ = resp
	return nil
}

// RevokeOthers revokes all sessions except the current one.
func (c *sessionClient) RevokeOthers(ctx context.Context, keepSessionID string) error {
	if keepSessionID == "" {
		return errors.NewInvalidInput("iam.session.RevokeOthers", "keep session ID is required")
	}

	if ctx == nil {
		return errors.NewInvalidInput("iam.session.RevokeOthers", "context is required")
	}

	resp, err := c.transport.Request(ctx, sessionServiceName, "RevokeOthers", &revokeOthersRequest{
		KeepSessionID: keepSessionID,
	})
	if err != nil {
		return err
	}

	// Accept nil response or valid response.
	if resp == nil {
		return nil
	}

	// RevokeOthers is a void operation. Any non-error response indicates success.
	_ = resp
	return nil
}

// Touch updates the last active timestamp for a session.
func (c *sessionClient) Touch(ctx context.Context, sessionID string) error {
	if sessionID == "" {
		return errors.NewInvalidInput("iam.session.Touch", "session ID is required")
	}

	if ctx == nil {
		return errors.NewInvalidInput("iam.session.Touch", "context is required")
	}

	resp, err := c.transport.Request(ctx, sessionServiceName, "Touch", &touchSessionRequest{
		SessionID: sessionID,
		Timestamp: time.Now().UTC(),
	})
	if err != nil {
		return err
	}

	// Accept nil response or valid response.
	if resp == nil {
		return nil
	}

	// Touch is a void operation. Any non-error response indicates success.
	_ = resp
	return nil
}

// Validation functions.

func validateListSessionsRequest(req *ListSessionsRequest) error {
	if req == nil {
		return errors.NewInvalidInput("iam.session.List", "request is required")
	}
	if req.TenantID == "" {
		return errors.NewInvalidInput("iam.session.List", "tenant ID is required")
	}
	if req.UserID == "" {
		return errors.NewInvalidInput("iam.session.List", "user ID is required")
	}
	return nil
}

// parseListSessionsResponse parses the response from ListSessions.
func parseListSessionsResponse(resp interface{}) (*ListSessionsResponse, error) {
	if resp == nil {
		return &ListSessionsResponse{Sessions: []Session{}}, nil
	}

	// If already a *ListSessionsResponse, return it directly.
	if listResp, ok := resp.(*ListSessionsResponse); ok {
		return listResp, nil
	}

	// Handle map response (from gRPC or REST).
	respMap, ok := resp.(map[string]interface{})
	if !ok {
		return nil, errors.NewInternal("iam.session.List", "invalid response type")
	}

	result := &ListSessionsResponse{
		Sessions: []Session{},
	}

	// Parse sessions array.
	if sessionsVal, ok := respMap["sessions"]; ok {
		if sessions, ok := sessionsVal.([]interface{}); ok {
			for _, s := range sessions {
				if sessionMap, ok := s.(map[string]interface{}); ok {
					session := parseSessionFromMap(sessionMap)
					result.Sessions = append(result.Sessions, session)
				}
			}
		}
	}

	// Parse pagination/total count.
	if pagination, ok := respMap["pagination"].(map[string]interface{}); ok {
		if total, ok := pagination["total_count"].(float64); ok {
			result.TotalCount = int(total)
		}
	}
	if total, ok := respMap["total_count"].(float64); ok {
		result.TotalCount = int(total)
	}

	return result, nil
}

// parseSessionResponse parses the response for a single session.
func parseSessionResponse(resp interface{}) (*Session, error) {
	if resp == nil {
		return nil, errors.NewNotFound("iam.session", "session not found")
	}

	// If already a *Session, return it directly.
	if session, ok := resp.(*Session); ok {
		return session, nil
	}

	// Handle map response.
	respMap, ok := resp.(map[string]interface{})
	if !ok {
		return nil, errors.NewInternal("iam.session", "invalid response type")
	}

	session := parseSessionFromMap(respMap)
	return &session, nil
}

// parseSessionFromMap parses a Session from a map.
func parseSessionFromMap(m map[string]interface{}) Session {
	session := Session{}

	if v, ok := m["id"].(string); ok {
		session.ID = v
	}
	if v, ok := m["user_id"].(string); ok {
		session.UserID = v
	}
	if v, ok := m["tenant_id"].(string); ok {
		session.TenantID = v
	}
	if v, ok := m["mfa_verified"].(bool); ok {
		session.MFAVerified = v
	}
	if v, ok := m["ip_address"].(string); ok {
		session.IPAddress = v
	}
	if v, ok := m["user_agent"].(string); ok {
		session.UserAgent = v
	}
	if v, ok := m["device_info"].(string); ok {
		session.DeviceInfo = v
	}

	// Parse time fields.
	session.Created = parseTimeFromMap(m, "created_at", "created")
	session.LastActive = parseTimeFromMap(m, "last_active", "last_active_at")
	session.ExpiresAt = parseTimeFromMap(m, "expires_at", "expiry")

	return session
}

// parseTimeFromMap parses a time value from a map, trying multiple keys.
func parseTimeFromMap(m map[string]interface{}, keys ...string) time.Time {
	for _, key := range keys {
		if v, ok := m[key]; ok {
			switch t := v.(type) {
			case time.Time:
				return t
			case string:
				if parsed, err := time.Parse(time.RFC3339, t); err == nil {
					return parsed
				}
				if parsed, err := time.Parse(time.RFC3339Nano, t); err == nil {
					return parsed
				}
			}
		}
	}
	return time.Time{}
}
