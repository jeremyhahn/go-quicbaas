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
	"strings"
	"testing"
	"time"
)

// =============================================================================
// Tenant.Validate Tests
// =============================================================================

func TestTenant_Validate(t *testing.T) {
	tests := []struct {
		name    string
		tenant  *Tenant
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid tenant",
			tenant: &Tenant{
				ID:   "tenant-123",
				Name: "Test Tenant",
			},
			wantErr: false,
		},
		{
			name: "missing ID",
			tenant: &Tenant{
				Name: "Test Tenant",
			},
			wantErr: true,
			errMsg:  "tenant ID is required",
		},
		{
			name: "empty ID",
			tenant: &Tenant{
				ID:   "",
				Name: "Test Tenant",
			},
			wantErr: true,
			errMsg:  "tenant ID is required",
		},
		{
			name: "missing name",
			tenant: &Tenant{
				ID: "tenant-123",
			},
			wantErr: true,
			errMsg:  "tenant name is required",
		},
		{
			name: "empty name",
			tenant: &Tenant{
				ID:   "tenant-123",
				Name: "",
			},
			wantErr: true,
			errMsg:  "tenant name is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.tenant.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Tenant.Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && err != nil && !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("Tenant.Validate() error = %v, want error containing %v", err, tt.errMsg)
			}
		})
	}
}

// =============================================================================
// User.Validate Tests
// =============================================================================

func TestUser_Validate(t *testing.T) {
	tests := []struct {
		name    string
		user    *User
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid user",
			user: &User{
				ID:       "user-123",
				Email:    "user@example.com",
				TenantID: "tenant-123",
			},
			wantErr: false,
		},
		{
			name: "missing ID",
			user: &User{
				Email:    "user@example.com",
				TenantID: "tenant-123",
			},
			wantErr: true,
			errMsg:  "user ID is required",
		},
		{
			name: "empty ID",
			user: &User{
				ID:       "",
				Email:    "user@example.com",
				TenantID: "tenant-123",
			},
			wantErr: true,
			errMsg:  "user ID is required",
		},
		{
			name: "missing email",
			user: &User{
				ID:       "user-123",
				TenantID: "tenant-123",
			},
			wantErr: true,
			errMsg:  "user email is required",
		},
		{
			name: "empty email",
			user: &User{
				ID:       "user-123",
				Email:    "",
				TenantID: "tenant-123",
			},
			wantErr: true,
			errMsg:  "user email is required",
		},
		{
			name: "missing tenant ID",
			user: &User{
				ID:    "user-123",
				Email: "user@example.com",
			},
			wantErr: true,
			errMsg:  "user tenant ID is required",
		},
		{
			name: "empty tenant ID",
			user: &User{
				ID:       "user-123",
				Email:    "user@example.com",
				TenantID: "",
			},
			wantErr: true,
			errMsg:  "user tenant ID is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.user.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("User.Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && err != nil && !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("User.Validate() error = %v, want error containing %v", err, tt.errMsg)
			}
		})
	}
}

// =============================================================================
// Group.Validate Tests
// =============================================================================

func TestGroup_Validate(t *testing.T) {
	tests := []struct {
		name    string
		group   *Group
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid group",
			group: &Group{
				ID:       "group-123",
				Name:     "Engineering",
				TenantID: "tenant-123",
			},
			wantErr: false,
		},
		{
			name: "missing ID",
			group: &Group{
				Name:     "Engineering",
				TenantID: "tenant-123",
			},
			wantErr: true,
			errMsg:  "group ID is required",
		},
		{
			name: "empty ID",
			group: &Group{
				ID:       "",
				Name:     "Engineering",
				TenantID: "tenant-123",
			},
			wantErr: true,
			errMsg:  "group ID is required",
		},
		{
			name: "missing name",
			group: &Group{
				ID:       "group-123",
				TenantID: "tenant-123",
			},
			wantErr: true,
			errMsg:  "group name is required",
		},
		{
			name: "empty name",
			group: &Group{
				ID:       "group-123",
				Name:     "",
				TenantID: "tenant-123",
			},
			wantErr: true,
			errMsg:  "group name is required",
		},
		{
			name: "missing tenant ID",
			group: &Group{
				ID:   "group-123",
				Name: "Engineering",
			},
			wantErr: true,
			errMsg:  "group tenant ID is required",
		},
		{
			name: "empty tenant ID",
			group: &Group{
				ID:       "group-123",
				Name:     "Engineering",
				TenantID: "",
			},
			wantErr: true,
			errMsg:  "group tenant ID is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.group.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Group.Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && err != nil && !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("Group.Validate() error = %v, want error containing %v", err, tt.errMsg)
			}
		})
	}
}

// =============================================================================
// Role.Validate Tests
// =============================================================================

func TestRole_Validate(t *testing.T) {
	tests := []struct {
		name    string
		role    *Role
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid role",
			role: &Role{
				ID:       "role-123",
				Name:     "admin",
				TenantID: "tenant-123",
			},
			wantErr: false,
		},
		{
			name: "missing ID",
			role: &Role{
				Name:     "admin",
				TenantID: "tenant-123",
			},
			wantErr: true,
			errMsg:  "role ID is required",
		},
		{
			name: "empty ID",
			role: &Role{
				ID:       "",
				Name:     "admin",
				TenantID: "tenant-123",
			},
			wantErr: true,
			errMsg:  "role ID is required",
		},
		{
			name: "missing name",
			role: &Role{
				ID:       "role-123",
				TenantID: "tenant-123",
			},
			wantErr: true,
			errMsg:  "role name is required",
		},
		{
			name: "empty name",
			role: &Role{
				ID:       "role-123",
				Name:     "",
				TenantID: "tenant-123",
			},
			wantErr: true,
			errMsg:  "role name is required",
		},
		{
			name: "missing tenant ID",
			role: &Role{
				ID:   "role-123",
				Name: "admin",
			},
			wantErr: true,
			errMsg:  "role tenant ID is required",
		},
		{
			name: "empty tenant ID",
			role: &Role{
				ID:       "role-123",
				Name:     "admin",
				TenantID: "",
			},
			wantErr: true,
			errMsg:  "role tenant ID is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.role.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Role.Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && err != nil && !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("Role.Validate() error = %v, want error containing %v", err, tt.errMsg)
			}
		})
	}
}

// =============================================================================
// Policy.Validate Tests
// =============================================================================

func TestPolicy_Validate(t *testing.T) {
	validRule := Rule{
		Name:      "test-rule",
		Effect:    EffectAllow,
		Actions:   []string{"read"},
		Resources: []string{"/test"},
	}

	invalidRule := Rule{
		Name: "invalid-rule",
		// Missing Effect, Actions, and Resources
	}

	tests := []struct {
		name    string
		policy  *Policy
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid policy",
			policy: &Policy{
				APIVersion: "v1",
				Kind:       "Policy",
				Metadata: PolicyMetadata{
					Name:   "test-policy",
					Tenant: "tenant-123",
				},
				Spec: PolicySpec{
					Rules: []Rule{validRule},
				},
			},
			wantErr: false,
		},
		{
			name: "missing API version",
			policy: &Policy{
				Kind: "Policy",
				Metadata: PolicyMetadata{
					Name:   "test-policy",
					Tenant: "tenant-123",
				},
				Spec: PolicySpec{
					Rules: []Rule{validRule},
				},
			},
			wantErr: true,
			errMsg:  "policy apiVersion is required",
		},
		{
			name: "empty API version",
			policy: &Policy{
				APIVersion: "",
				Kind:       "Policy",
				Metadata: PolicyMetadata{
					Name:   "test-policy",
					Tenant: "tenant-123",
				},
				Spec: PolicySpec{
					Rules: []Rule{validRule},
				},
			},
			wantErr: true,
			errMsg:  "policy apiVersion is required",
		},
		{
			name: "invalid kind",
			policy: &Policy{
				APIVersion: "v1",
				Kind:       "NotPolicy",
				Metadata: PolicyMetadata{
					Name:   "test-policy",
					Tenant: "tenant-123",
				},
				Spec: PolicySpec{
					Rules: []Rule{validRule},
				},
			},
			wantErr: true,
			errMsg:  "policy kind must be 'Policy'",
		},
		{
			name: "missing name",
			policy: &Policy{
				APIVersion: "v1",
				Kind:       "Policy",
				Metadata: PolicyMetadata{
					Tenant: "tenant-123",
				},
				Spec: PolicySpec{
					Rules: []Rule{validRule},
				},
			},
			wantErr: true,
			errMsg:  "policy name is required",
		},
		{
			name: "empty name",
			policy: &Policy{
				APIVersion: "v1",
				Kind:       "Policy",
				Metadata: PolicyMetadata{
					Name:   "",
					Tenant: "tenant-123",
				},
				Spec: PolicySpec{
					Rules: []Rule{validRule},
				},
			},
			wantErr: true,
			errMsg:  "policy name is required",
		},
		{
			name: "missing tenant",
			policy: &Policy{
				APIVersion: "v1",
				Kind:       "Policy",
				Metadata: PolicyMetadata{
					Name: "test-policy",
				},
				Spec: PolicySpec{
					Rules: []Rule{validRule},
				},
			},
			wantErr: true,
			errMsg:  "policy tenant is required",
		},
		{
			name: "empty tenant",
			policy: &Policy{
				APIVersion: "v1",
				Kind:       "Policy",
				Metadata: PolicyMetadata{
					Name:   "test-policy",
					Tenant: "",
				},
				Spec: PolicySpec{
					Rules: []Rule{validRule},
				},
			},
			wantErr: true,
			errMsg:  "policy tenant is required",
		},
		{
			name: "missing rules",
			policy: &Policy{
				APIVersion: "v1",
				Kind:       "Policy",
				Metadata: PolicyMetadata{
					Name:   "test-policy",
					Tenant: "tenant-123",
				},
				Spec: PolicySpec{},
			},
			wantErr: true,
			errMsg:  "policy must have at least one rule",
		},
		{
			name: "empty rules",
			policy: &Policy{
				APIVersion: "v1",
				Kind:       "Policy",
				Metadata: PolicyMetadata{
					Name:   "test-policy",
					Tenant: "tenant-123",
				},
				Spec: PolicySpec{
					Rules: []Rule{},
				},
			},
			wantErr: true,
			errMsg:  "policy must have at least one rule",
		},
		{
			name: "invalid rule in list",
			policy: &Policy{
				APIVersion: "v1",
				Kind:       "Policy",
				Metadata: PolicyMetadata{
					Name:   "test-policy",
					Tenant: "tenant-123",
				},
				Spec: PolicySpec{
					Rules: []Rule{validRule, invalidRule},
				},
			},
			wantErr: true,
			errMsg:  "invalid rule at index",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.policy.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Policy.Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && err != nil && !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("Policy.Validate() error = %v, want error containing %v", err, tt.errMsg)
			}
		})
	}
}

// =============================================================================
// Rule.Validate Tests
// =============================================================================

func TestRule_Validate(t *testing.T) {
	tests := []struct {
		name    string
		rule    *Rule
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid allow rule",
			rule: &Rule{
				Name:      "test-rule",
				Effect:    EffectAllow,
				Actions:   []string{"read", "write"},
				Resources: []string{"/test"},
			},
			wantErr: false,
		},
		{
			name: "valid deny rule",
			rule: &Rule{
				Name:      "test-rule",
				Effect:    EffectDeny,
				Actions:   []string{"delete"},
				Resources: []string{"/admin/*"},
			},
			wantErr: false,
		},
		{
			name: "missing name",
			rule: &Rule{
				Effect:    EffectAllow,
				Actions:   []string{"read"},
				Resources: []string{"/test"},
			},
			wantErr: true,
			errMsg:  "rule name is required",
		},
		{
			name: "empty name",
			rule: &Rule{
				Name:      "",
				Effect:    EffectAllow,
				Actions:   []string{"read"},
				Resources: []string{"/test"},
			},
			wantErr: true,
			errMsg:  "rule name is required",
		},
		{
			name: "invalid effect",
			rule: &Rule{
				Name:      "test-rule",
				Effect:    "invalid",
				Actions:   []string{"read"},
				Resources: []string{"/test"},
			},
			wantErr: true,
			errMsg:  "rule effect must be 'allow' or 'deny'",
		},
		{
			name: "missing effect",
			rule: &Rule{
				Name:      "test-rule",
				Actions:   []string{"read"},
				Resources: []string{"/test"},
			},
			wantErr: true,
			errMsg:  "rule effect must be 'allow' or 'deny'",
		},
		{
			name: "missing actions",
			rule: &Rule{
				Name:      "test-rule",
				Effect:    EffectAllow,
				Resources: []string{"/test"},
			},
			wantErr: true,
			errMsg:  "rule must have at least one action",
		},
		{
			name: "empty actions",
			rule: &Rule{
				Name:      "test-rule",
				Effect:    EffectAllow,
				Actions:   []string{},
				Resources: []string{"/test"},
			},
			wantErr: true,
			errMsg:  "rule must have at least one action",
		},
		{
			name: "missing resources",
			rule: &Rule{
				Name:    "test-rule",
				Effect:  EffectAllow,
				Actions: []string{"read"},
			},
			wantErr: true,
			errMsg:  "rule must have at least one resource",
		},
		{
			name: "empty resources",
			rule: &Rule{
				Name:      "test-rule",
				Effect:    EffectAllow,
				Actions:   []string{"read"},
				Resources: []string{},
			},
			wantErr: true,
			errMsg:  "rule must have at least one resource",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.rule.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Rule.Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && err != nil && !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("Rule.Validate() error = %v, want error containing %v", err, tt.errMsg)
			}
		})
	}
}

// =============================================================================
// Session.IsExpired Tests
// =============================================================================

func TestSession_IsExpired(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name    string
		session *Session
		want    bool
	}{
		{
			name: "not expired - future expiry",
			session: &Session{
				Expires:      now.Add(1 * time.Hour),
				LastActivity: now,
				IdleTimeout:  30 * time.Minute,
			},
			want: false,
		},
		{
			name: "expired - past expiry",
			session: &Session{
				Expires:      now.Add(-1 * time.Hour),
				LastActivity: now,
				IdleTimeout:  30 * time.Minute,
			},
			want: true,
		},
		{
			name: "expired - idle timeout exceeded",
			session: &Session{
				Expires:      now.Add(1 * time.Hour),
				LastActivity: now.Add(-1 * time.Hour),
				IdleTimeout:  30 * time.Minute,
			},
			want: true,
		},
		{
			name: "not expired - idle timeout not exceeded",
			session: &Session{
				Expires:      now.Add(1 * time.Hour),
				LastActivity: now.Add(-10 * time.Minute),
				IdleTimeout:  30 * time.Minute,
			},
			want: false,
		},
		{
			name: "not expired - no idle timeout",
			session: &Session{
				Expires:      now.Add(1 * time.Hour),
				LastActivity: now.Add(-2 * time.Hour),
				IdleTimeout:  0,
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.session.IsExpired(); got != tt.want {
				t.Errorf("Session.IsExpired() = %v, want %v", got, tt.want)
			}
		})
	}
}

// =============================================================================
// Token.IsExpired Tests
// =============================================================================

func TestToken_IsExpired(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name  string
		token *Token
		want  bool
	}{
		{
			name: "not expired - future expiry",
			token: &Token{
				Expires: now.Add(1 * time.Hour),
			},
			want: false,
		},
		{
			name: "expired - past expiry",
			token: &Token{
				Expires: now.Add(-1 * time.Hour),
			},
			want: true,
		},
		{
			name: "expired - one second ago",
			token: &Token{
				Expires: now.Add(-1 * time.Second),
			},
			want: true,
		},
		{
			name: "not expired - one second from now",
			token: &Token{
				Expires: now.Add(1 * time.Second),
			},
			want: false,
		},
		{
			name: "not expired - far future",
			token: &Token{
				Expires: now.Add(24 * time.Hour),
			},
			want: false,
		},
		{
			name: "expired - far past",
			token: &Token{
				Expires: now.Add(-24 * time.Hour),
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.token.IsExpired(); got != tt.want {
				t.Errorf("Token.IsExpired() = %v, want %v", got, tt.want)
			}
		})
	}
}
