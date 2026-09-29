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

package cli

import (
	"context"
	"time"
)

// =============================================================================
// IAM Client Interfaces and Mocks
// =============================================================================

// IAMClient defines the interface for IAM operations.
type IAMClient interface {
	Close() error
	ListTenants(ctx context.Context) ([]*TenantInfo, error)
	ListUsers(ctx context.Context, tenantID string, limit int) ([]*UserInfo, error)
	ListRoles(ctx context.Context, tenantID string, limit int) ([]*RoleInfo, error)
	ListSessions(ctx context.Context, tenantID string, limit int) ([]*SessionInfo, error)
}

// MockIAMClient is a mock implementation of IAMClient for testing.
type MockIAMClient struct {
	ListTenantsFunc  func(ctx context.Context) ([]*TenantInfo, error)
	ListUsersFunc    func(ctx context.Context, tenantID string, limit int) ([]*UserInfo, error)
	ListRolesFunc    func(ctx context.Context, tenantID string, limit int) ([]*RoleInfo, error)
	ListSessionsFunc func(ctx context.Context, tenantID string, limit int) ([]*SessionInfo, error)
}

// Close implements IAMClient.
func (m *MockIAMClient) Close() error {
	return nil
}

// ListTenants implements IAMClient.
func (m *MockIAMClient) ListTenants(ctx context.Context) ([]*TenantInfo, error) {
	if m.ListTenantsFunc != nil {
		return m.ListTenantsFunc(ctx)
	}
	return nil, nil
}

// ListUsers implements IAMClient.
func (m *MockIAMClient) ListUsers(ctx context.Context, tenantID string, limit int) ([]*UserInfo, error) {
	if m.ListUsersFunc != nil {
		return m.ListUsersFunc(ctx, tenantID, limit)
	}
	return nil, nil
}

// ListRoles implements IAMClient.
func (m *MockIAMClient) ListRoles(ctx context.Context, tenantID string, limit int) ([]*RoleInfo, error) {
	if m.ListRolesFunc != nil {
		return m.ListRolesFunc(ctx, tenantID, limit)
	}
	return nil, nil
}

// ListSessions implements IAMClient.
func (m *MockIAMClient) ListSessions(ctx context.Context, tenantID string, limit int) ([]*SessionInfo, error) {
	if m.ListSessionsFunc != nil {
		return m.ListSessionsFunc(ctx, tenantID, limit)
	}
	return nil, nil
}

// =============================================================================
// IAM Entity Types for CLI
// =============================================================================

// TenantInfo represents tenant information for CLI display.
type TenantInfo struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	MaxUsers    int       `json:"max_users"`
	MFARequired bool      `json:"mfa_required"`
	Created     time.Time `json:"created"`
	Updated     time.Time `json:"updated"`
}

// UserInfo represents user information for CLI display.
type UserInfo struct {
	ID          string    `json:"id"`
	TenantID    string    `json:"tenant_id"`
	Username    string    `json:"username"`
	Email       string    `json:"email"`
	DisplayName string    `json:"display_name"`
	Status      string    `json:"status"`
	MFAEnabled  bool      `json:"mfa_enabled"`
	Created     time.Time `json:"created"`
	Updated     time.Time `json:"updated"`
}

// RoleInfo represents role information for CLI display.
type RoleInfo struct {
	ID          string    `json:"id"`
	TenantID    string    `json:"tenant_id"`
	Name        string    `json:"name"`
	DisplayName string    `json:"display_name"`
	Description string    `json:"description"`
	IsSystem    bool      `json:"is_system"`
	Created     time.Time `json:"created"`
	Updated     time.Time `json:"updated"`
}

// SessionInfo represents session information for CLI display.
type SessionInfo struct {
	ID          string    `json:"id"`
	UserID      string    `json:"user_id"`
	TenantID    string    `json:"tenant_id"`
	Created     time.Time `json:"created"`
	LastActive  time.Time `json:"last_active"`
	ExpiresAt   time.Time `json:"expires_at"`
	MFAVerified bool      `json:"mfa_verified"`
	IPAddress   string    `json:"ip_address"`
	UserAgent   string    `json:"user_agent"`
}

// =============================================================================
// Auth Client Interfaces and Mocks
// =============================================================================

// AuthClient defines the interface for authentication operations.
type AuthClient interface {
	Close() error
}

// MockAuthClient is a mock implementation of AuthClient for testing.
type MockAuthClient struct{}

// Close implements AuthClient.
func (m *MockAuthClient) Close() error {
	return nil
}
