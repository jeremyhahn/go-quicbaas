// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package service

import (
	"testing"
)

// TestIAMCreateUserRequestValidate tests IAMCreateUserRequest validation.
func TestIAMCreateUserRequestValidate(t *testing.T) {
	tests := []struct {
		name    string
		req     *IAMCreateUserRequest
		wantErr bool
		errMsg  string
	}{
		{
			name:    "missing tenant_id",
			req:     &IAMCreateUserRequest{Email: "test@example.com", Name: "Test User"},
			wantErr: true,
			errMsg:  "tenant_id is required",
		},
		{
			name:    "missing email",
			req:     &IAMCreateUserRequest{TenantID: "tenant-1", Name: "Test User"},
			wantErr: true,
			errMsg:  "email is required",
		},
		{
			name:    "invalid email format",
			req:     &IAMCreateUserRequest{TenantID: "tenant-1", Email: "invalid-email", Name: "Test User"},
			wantErr: true,
			errMsg:  "invalid email format",
		},
		{
			name:    "missing name",
			req:     &IAMCreateUserRequest{TenantID: "tenant-1", Email: "test@example.com"},
			wantErr: true,
			errMsg:  "name is required",
		},
		{
			name:    "whitespace only name",
			req:     &IAMCreateUserRequest{TenantID: "tenant-1", Email: "test@example.com", Name: "   "},
			wantErr: true,
			errMsg:  "name cannot be empty or whitespace",
		},
		{
			name:    "valid request",
			req:     &IAMCreateUserRequest{TenantID: "tenant-1", Email: "test@example.com", Name: "Test User"},
			wantErr: false,
		},
		{
			name: "valid request with all optional fields",
			req: &IAMCreateUserRequest{
				TenantID:   "tenant-1",
				Email:      "test@example.com",
				Name:       "Test User",
				Username:   "testuser",
				Roles:      []string{"admin"},
				Groups:     []string{"engineering"},
				Attributes: map[string]string{"dept": "engineering"},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && tt.errMsg != "" && err.Error() != tt.errMsg {
				t.Errorf("Validate() error = %v, want %v", err.Error(), tt.errMsg)
			}
		})
	}
}

// TestIAMUpdateUserRequestValidate tests IAMUpdateUserRequest validation.
func TestIAMUpdateUserRequestValidate(t *testing.T) {
	strPtr := func(s string) *string { return &s }

	tests := []struct {
		name    string
		req     *IAMUpdateUserRequest
		wantErr bool
		errMsg  string
	}{
		{
			name:    "missing tenant_id",
			req:     &IAMUpdateUserRequest{UserID: "user-1"},
			wantErr: true,
			errMsg:  "tenant_id is required",
		},
		{
			name:    "missing user_id",
			req:     &IAMUpdateUserRequest{TenantID: "tenant-1"},
			wantErr: true,
			errMsg:  "user_id is required",
		},
		{
			name:    "empty email",
			req:     &IAMUpdateUserRequest{TenantID: "tenant-1", UserID: "user-1", Email: strPtr("")},
			wantErr: true,
			errMsg:  "email is required",
		},
		{
			name:    "invalid email format",
			req:     &IAMUpdateUserRequest{TenantID: "tenant-1", UserID: "user-1", Email: strPtr("not-an-email")},
			wantErr: true,
			errMsg:  "invalid email format",
		},
		{
			name:    "whitespace only name",
			req:     &IAMUpdateUserRequest{TenantID: "tenant-1", UserID: "user-1", Name: strPtr("   ")},
			wantErr: true,
			errMsg:  "name cannot be empty or whitespace",
		},
		{
			name:    "valid request with no updates",
			req:     &IAMUpdateUserRequest{TenantID: "tenant-1", UserID: "user-1"},
			wantErr: false,
		},
		{
			name:    "valid request with email update",
			req:     &IAMUpdateUserRequest{TenantID: "tenant-1", UserID: "user-1", Email: strPtr("new@example.com")},
			wantErr: false,
		},
		{
			name:    "valid request with name update",
			req:     &IAMUpdateUserRequest{TenantID: "tenant-1", UserID: "user-1", Name: strPtr("New Name")},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && tt.errMsg != "" && err.Error() != tt.errMsg {
				t.Errorf("Validate() error = %v, want %v", err.Error(), tt.errMsg)
			}
		})
	}
}

// TestCreateGroupRequestValidate tests CreateGroupRequest validation.
func TestCreateGroupRequestValidate(t *testing.T) {
	tests := []struct {
		name    string
		req     *CreateGroupRequest
		wantErr bool
		errMsg  string
	}{
		{
			name:    "missing tenant_id",
			req:     &CreateGroupRequest{Name: "Engineering"},
			wantErr: true,
			errMsg:  "tenant_id is required",
		},
		{
			name:    "missing name",
			req:     &CreateGroupRequest{TenantID: "tenant-1"},
			wantErr: true,
			errMsg:  "name is required",
		},
		{
			name:    "whitespace only name",
			req:     &CreateGroupRequest{TenantID: "tenant-1", Name: "   "},
			wantErr: true,
			errMsg:  "name cannot be empty or whitespace",
		},
		{
			name:    "valid request",
			req:     &CreateGroupRequest{TenantID: "tenant-1", Name: "Engineering"},
			wantErr: false,
		},
		{
			name: "valid request with all fields",
			req: &CreateGroupRequest{
				TenantID:    "tenant-1",
				Name:        "Engineering",
				Description: "Engineering team",
				Members:     []string{"user-1", "user-2"},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && tt.errMsg != "" && err.Error() != tt.errMsg {
				t.Errorf("Validate() error = %v, want %v", err.Error(), tt.errMsg)
			}
		})
	}
}

// TestUpdateGroupRequestValidate tests UpdateGroupRequest validation.
func TestUpdateGroupRequestValidate(t *testing.T) {
	strPtr := func(s string) *string { return &s }

	tests := []struct {
		name    string
		req     *UpdateGroupRequest
		wantErr bool
		errMsg  string
	}{
		{
			name:    "missing tenant_id",
			req:     &UpdateGroupRequest{GroupID: "group-1"},
			wantErr: true,
			errMsg:  "tenant_id is required",
		},
		{
			name:    "missing group_id",
			req:     &UpdateGroupRequest{TenantID: "tenant-1"},
			wantErr: true,
			errMsg:  "group_id is required",
		},
		{
			name:    "whitespace only name",
			req:     &UpdateGroupRequest{TenantID: "tenant-1", GroupID: "group-1", Name: strPtr("   ")},
			wantErr: true,
			errMsg:  "name cannot be empty or whitespace",
		},
		{
			name:    "valid request with no updates",
			req:     &UpdateGroupRequest{TenantID: "tenant-1", GroupID: "group-1"},
			wantErr: false,
		},
		{
			name:    "valid request with name update",
			req:     &UpdateGroupRequest{TenantID: "tenant-1", GroupID: "group-1", Name: strPtr("New Name")},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && tt.errMsg != "" && err.Error() != tt.errMsg {
				t.Errorf("Validate() error = %v, want %v", err.Error(), tt.errMsg)
			}
		})
	}
}

// TestIAMCreateRoleRequestValidate tests IAMCreateRoleRequest validation.
func TestIAMCreateRoleRequestValidate(t *testing.T) {
	tests := []struct {
		name    string
		req     *IAMCreateRoleRequest
		wantErr bool
		errMsg  string
	}{
		{
			name:    "missing tenant_id",
			req:     &IAMCreateRoleRequest{Name: "admin"},
			wantErr: true,
			errMsg:  "tenant_id is required",
		},
		{
			name:    "missing name",
			req:     &IAMCreateRoleRequest{TenantID: "tenant-1"},
			wantErr: true,
			errMsg:  "name is required",
		},
		{
			name:    "whitespace only name",
			req:     &IAMCreateRoleRequest{TenantID: "tenant-1", Name: "   "},
			wantErr: true,
			errMsg:  "name cannot be empty or whitespace",
		},
		{
			name:    "empty permission in list",
			req:     &IAMCreateRoleRequest{TenantID: "tenant-1", Name: "admin", Permissions: []string{"read", "  ", "write"}},
			wantErr: true,
			errMsg:  "permission at index 1 cannot be empty",
		},
		{
			name:    "valid request",
			req:     &IAMCreateRoleRequest{TenantID: "tenant-1", Name: "admin"},
			wantErr: false,
		},
		{
			name: "valid request with all fields",
			req: &IAMCreateRoleRequest{
				TenantID:    "tenant-1",
				Name:        "admin",
				Description: "Administrator role",
				Permissions: []string{"read", "write", "delete"},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && tt.errMsg != "" && err.Error() != tt.errMsg {
				t.Errorf("Validate() error = %v, want %v", err.Error(), tt.errMsg)
			}
		})
	}
}

// TestRegisterAppRequestValidate tests RegisterAppRequest validation.
func TestRegisterAppRequestValidate(t *testing.T) {
	tests := []struct {
		name    string
		req     *RegisterAppRequest
		wantErr bool
		errMsg  string
	}{
		{
			name:    "missing tenant_id",
			req:     &RegisterAppRequest{Name: "MyApp", RedirectURIs: []string{"https://app.example.com/callback"}, GrantTypes: []string{"authorization_code"}, ResponseTypes: []string{"code"}},
			wantErr: true,
			errMsg:  "tenant_id is required",
		},
		{
			name:    "missing name",
			req:     &RegisterAppRequest{TenantID: "tenant-1", RedirectURIs: []string{"https://app.example.com/callback"}, GrantTypes: []string{"authorization_code"}, ResponseTypes: []string{"code"}},
			wantErr: true,
			errMsg:  "name is required",
		},
		{
			name:    "whitespace only name",
			req:     &RegisterAppRequest{TenantID: "tenant-1", Name: "   ", RedirectURIs: []string{"https://app.example.com/callback"}, GrantTypes: []string{"authorization_code"}, ResponseTypes: []string{"code"}},
			wantErr: true,
			errMsg:  "name cannot be empty or whitespace",
		},
		{
			name:    "missing redirect_uris",
			req:     &RegisterAppRequest{TenantID: "tenant-1", Name: "MyApp", GrantTypes: []string{"authorization_code"}, ResponseTypes: []string{"code"}},
			wantErr: true,
			errMsg:  "at least one redirect_uri cannot be empty",
		},
		{
			name:    "empty redirect_uri in list",
			req:     &RegisterAppRequest{TenantID: "tenant-1", Name: "MyApp", RedirectURIs: []string{"https://app.example.com/callback", "  "}, GrantTypes: []string{"authorization_code"}, ResponseTypes: []string{"code"}},
			wantErr: true,
			errMsg:  "redirect_uri at index 1 cannot be empty",
		},
		{
			name:    "missing grant_types",
			req:     &RegisterAppRequest{TenantID: "tenant-1", Name: "MyApp", RedirectURIs: []string{"https://app.example.com/callback"}, ResponseTypes: []string{"code"}},
			wantErr: true,
			errMsg:  "at least one grant_type cannot be empty",
		},
		{
			name:    "empty grant_type in list",
			req:     &RegisterAppRequest{TenantID: "tenant-1", Name: "MyApp", RedirectURIs: []string{"https://app.example.com/callback"}, GrantTypes: []string{"authorization_code", "   "}, ResponseTypes: []string{"code"}},
			wantErr: true,
			errMsg:  "grant_type at index 1 cannot be empty",
		},
		{
			name:    "missing response_types",
			req:     &RegisterAppRequest{TenantID: "tenant-1", Name: "MyApp", RedirectURIs: []string{"https://app.example.com/callback"}, GrantTypes: []string{"authorization_code"}},
			wantErr: true,
			errMsg:  "at least one response_type cannot be empty",
		},
		{
			name:    "empty response_type in list",
			req:     &RegisterAppRequest{TenantID: "tenant-1", Name: "MyApp", RedirectURIs: []string{"https://app.example.com/callback"}, GrantTypes: []string{"authorization_code"}, ResponseTypes: []string{"code", "  "}},
			wantErr: true,
			errMsg:  "response_type at index 1 cannot be empty",
		},
		{
			name: "valid request",
			req: &RegisterAppRequest{
				TenantID:      "tenant-1",
				Name:          "MyApp",
				RedirectURIs:  []string{"https://app.example.com/callback"},
				GrantTypes:    []string{"authorization_code"},
				ResponseTypes: []string{"code"},
			},
			wantErr: false,
		},
		{
			name: "valid request with multiple URIs and grant types",
			req: &RegisterAppRequest{
				TenantID:       "tenant-1",
				Name:           "MyApp",
				RedirectURIs:   []string{"https://app.example.com/callback", "https://app.example.com/oauth2"},
				GrantTypes:     []string{"authorization_code", "refresh_token"},
				ResponseTypes:  []string{"code", "token"},
				ResourcePrefix: "myapp",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && tt.errMsg != "" && err.Error() != tt.errMsg {
				t.Errorf("Validate() error = %v, want %v", err.Error(), tt.errMsg)
			}
		})
	}
}
