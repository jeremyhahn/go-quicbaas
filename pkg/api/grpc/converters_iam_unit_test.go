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
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Tenant Conversion Tests
// =============================================================================

func TestConvertProtoToTenant(t *testing.T) {
	tests := []struct {
		name     string
		input    *iampb.Tenant
		validate func(t *testing.T, result *iam.Tenant)
	}{
		{
			name:  "nil_input",
			input: nil,
			validate: func(t *testing.T, result *iam.Tenant) {
				assert.Nil(t, result)
			},
		},
		{
			name: "full_tenant",
			input: &iampb.Tenant{
				Id:   "tenant-1",
				Name: "Test Tenant",
				Settings: &iampb.TenantSettings{
					MaxUsers:    100,
					MfaRequired: true,
					NamedNetworks: map[string]string{
						"office": "10.0.0.0/8",
					},
				},
				CreatedAt: timestamppb.New(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)),
				UpdatedAt: timestamppb.New(time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC)),
			},
			validate: func(t *testing.T, result *iam.Tenant) {
				require.NotNil(t, result)
				assert.Equal(t, "tenant-1", result.ID)
				assert.Equal(t, "Test Tenant", result.Name)
				assert.Equal(t, 100, result.Settings.MaxUsers)
				assert.True(t, result.Settings.MFARequired)
				assert.Contains(t, result.Settings.NamedNetworks, "office")
				assert.Equal(t, []string{"10.0.0.0/8"}, result.Settings.NamedNetworks["office"])
			},
		},
		{
			name: "tenant_without_settings",
			input: &iampb.Tenant{
				Id:   "tenant-2",
				Name: "Minimal Tenant",
			},
			validate: func(t *testing.T, result *iam.Tenant) {
				require.NotNil(t, result)
				assert.Equal(t, "tenant-2", result.ID)
				assert.Equal(t, "Minimal Tenant", result.Name)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertProtoToTenant(tt.input)
			tt.validate(t, result)
		})
	}
}

func TestConvertTenantToProto(t *testing.T) {
	tests := []struct {
		name     string
		input    *iam.Tenant
		validate func(t *testing.T, result *iampb.Tenant)
	}{
		{
			name:  "nil_input",
			input: nil,
			validate: func(t *testing.T, result *iampb.Tenant) {
				assert.Nil(t, result)
			},
		},
		{
			name: "full_tenant",
			input: &iam.Tenant{
				ID:   "tenant-1",
				Name: "Test Tenant",
				Settings: iam.TenantSettings{
					MaxUsers:    100,
					MFARequired: true,
					NamedNetworks: map[string][]string{
						"office": {"10.0.0.0/8", "192.168.0.0/16"},
					},
				},
				Created: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
				Updated: time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
			},
			validate: func(t *testing.T, result *iampb.Tenant) {
				require.NotNil(t, result)
				assert.Equal(t, "tenant-1", result.Id)
				assert.Equal(t, "Test Tenant", result.Name)
				require.NotNil(t, result.Settings)
				assert.Equal(t, int32(100), result.Settings.MaxUsers)
				assert.True(t, result.Settings.MfaRequired)
				// Only first CIDR is taken
				assert.Equal(t, "10.0.0.0/8", result.Settings.NamedNetworks["office"])
			},
		},
		{
			name: "tenant_with_empty_networks",
			input: &iam.Tenant{
				ID:   "tenant-2",
				Name: "Minimal Tenant",
				Settings: iam.TenantSettings{
					NamedNetworks: map[string][]string{
						"empty": {},
					},
				},
			},
			validate: func(t *testing.T, result *iampb.Tenant) {
				require.NotNil(t, result)
				// Empty network list should result in no entry
				assert.Empty(t, result.Settings.NamedNetworks["empty"])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertTenantToProto(tt.input)
			tt.validate(t, result)
		})
	}
}

// =============================================================================
// User Conversion Tests
// =============================================================================

func TestConvertProtoToUser(t *testing.T) {
	now := time.Now()
	lastAuth := now.Add(-30 * time.Minute)

	tests := []struct {
		name     string
		input    *iampb.User
		validate func(t *testing.T, result *iam.User)
	}{
		{
			name:  "nil_input",
			input: nil,
			validate: func(t *testing.T, result *iam.User) {
				assert.Nil(t, result)
			},
		},
		{
			name: "full_user",
			input: &iampb.User{
				Id:       "user-1",
				TenantId: "tenant-1",
				Email:    "test@example.com",
				Name:     "Test User",
				Roles:    []string{"admin", "user"},
				Groups:   []string{"group-1", "group-2"},
				Attributes: map[string]string{
					"department": "engineering",
				},
				CreatedAt: timestamppb.New(now),
				UpdatedAt: timestamppb.New(now),
				LastAuth:  timestamppb.New(lastAuth),
			},
			validate: func(t *testing.T, result *iam.User) {
				require.NotNil(t, result)
				assert.Equal(t, "user-1", result.ID)
				assert.Equal(t, "tenant-1", result.TenantID)
				assert.Equal(t, "test@example.com", result.Email)
				assert.Equal(t, "Test User", result.Name)
				assert.Equal(t, []string{"admin", "user"}, result.Roles)
				assert.Equal(t, []string{"group-1", "group-2"}, result.Groups)
				assert.Equal(t, "engineering", result.Attributes["department"])
				require.NotNil(t, result.LastAuth)
			},
		},
		{
			name: "user_without_optional_fields",
			input: &iampb.User{
				Id:       "user-2",
				TenantId: "tenant-1",
				Email:    "minimal@example.com",
			},
			validate: func(t *testing.T, result *iam.User) {
				require.NotNil(t, result)
				assert.Equal(t, "user-2", result.ID)
				assert.Nil(t, result.LastAuth)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertProtoToUser(tt.input)
			tt.validate(t, result)
		})
	}
}

func TestConvertUserToProto(t *testing.T) {
	now := time.Now()
	lastAuth := now.Add(-30 * time.Minute)

	tests := []struct {
		name     string
		input    *iam.User
		validate func(t *testing.T, result *iampb.User)
	}{
		{
			name:  "nil_input",
			input: nil,
			validate: func(t *testing.T, result *iampb.User) {
				assert.Nil(t, result)
			},
		},
		{
			name: "full_user",
			input: &iam.User{
				ID:       "user-1",
				TenantID: "tenant-1",
				Email:    "test@example.com",
				Name:     "Test User",
				Roles:    []string{"admin"},
				Groups:   []string{"group-1"},
				// Attributes are string-valued on both sides now, so nothing is
				// skipped on the way across: a value the wire could not carry is
				// refused by the type rather than dropped in the converter.
				Attributes: map[string]string{
					"department": "engineering",
					"level":      "123",
				},
				Created:  now,
				Updated:  now,
				LastAuth: &lastAuth,
			},
			validate: func(t *testing.T, result *iampb.User) {
				require.NotNil(t, result)
				assert.Equal(t, "user-1", result.Id)
				assert.Equal(t, "test@example.com", result.Email)
				assert.Equal(t, "engineering", result.Attributes["department"])
				// Nothing is skipped any more: attributes are string-valued on
				// both sides, so a value the wire cannot carry is refused by the
				// type rather than dropped in transit.
				assert.Equal(t, "123", result.Attributes["level"])
				assert.NotNil(t, result.LastAuth)
			},
		},
		{
			name: "user_without_optional_timestamps",
			input: &iam.User{
				ID:       "user-2",
				TenantID: "tenant-1",
				Email:    "minimal@example.com",
				Created:  now,
				Updated:  now,
			},
			validate: func(t *testing.T, result *iampb.User) {
				require.NotNil(t, result)
				assert.Nil(t, result.LastAuth)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertUserToProto(tt.input)
			tt.validate(t, result)
		})
	}
}

// =============================================================================
// Group Conversion Tests
// =============================================================================

func TestConvertProtoToGroup(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name     string
		input    *iampb.Group
		validate func(t *testing.T, result *iam.Group)
	}{
		{
			name:  "nil_input",
			input: nil,
			validate: func(t *testing.T, result *iam.Group) {
				assert.Nil(t, result)
			},
		},
		{
			name: "full_group",
			input: &iampb.Group{
				Id:          "group-1",
				TenantId:    "tenant-1",
				Name:        "Admins",
				Description: "Administrator group",
				Members:     []string{"user-1", "user-2"},
				CreatedAt:   timestamppb.New(now),
				UpdatedAt:   timestamppb.New(now),
			},
			validate: func(t *testing.T, result *iam.Group) {
				require.NotNil(t, result)
				assert.Equal(t, "group-1", result.ID)
				assert.Equal(t, "tenant-1", result.TenantID)
				assert.Equal(t, "Admins", result.Name)
				assert.Equal(t, "Administrator group", result.Description)
				assert.Equal(t, []string{"user-1", "user-2"}, result.Members)
			},
		},
		{
			name: "group_without_timestamps",
			input: &iampb.Group{
				Id:   "group-2",
				Name: "Users",
			},
			validate: func(t *testing.T, result *iam.Group) {
				require.NotNil(t, result)
				assert.Equal(t, "group-2", result.ID)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertProtoToGroup(tt.input)
			tt.validate(t, result)
		})
	}
}

func TestConvertGroupToProto(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name     string
		input    *iam.Group
		validate func(t *testing.T, result *iampb.Group)
	}{
		{
			name:  "nil_input",
			input: nil,
			validate: func(t *testing.T, result *iampb.Group) {
				assert.Nil(t, result)
			},
		},
		{
			name: "full_group",
			input: &iam.Group{
				ID:          "group-1",
				TenantID:    "tenant-1",
				Name:        "Admins",
				Description: "Administrator group",
				Members:     []string{"user-1", "user-2"},
				Created:     now,
				Updated:     now,
			},
			validate: func(t *testing.T, result *iampb.Group) {
				require.NotNil(t, result)
				assert.Equal(t, "group-1", result.Id)
				assert.Equal(t, "tenant-1", result.TenantId)
				assert.Equal(t, "Admins", result.Name)
				assert.Equal(t, "Administrator group", result.Description)
				assert.Equal(t, []string{"user-1", "user-2"}, result.Members)
				assert.NotNil(t, result.CreatedAt)
				assert.NotNil(t, result.UpdatedAt)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertGroupToProto(tt.input)
			tt.validate(t, result)
		})
	}
}

// =============================================================================
// Role Conversion Tests
// =============================================================================

func TestConvertProtoToRole(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name     string
		input    *iampb.Role
		validate func(t *testing.T, result *iam.Role)
	}{
		{
			name:  "nil_input",
			input: nil,
			validate: func(t *testing.T, result *iam.Role) {
				assert.Nil(t, result)
			},
		},
		{
			name: "full_role",
			input: &iampb.Role{
				Id:          "role-1",
				TenantId:    "tenant-1",
				Name:        "admin",
				Description: "Administrator role",
				Permissions: []string{"read", "write", "delete"},
				CreatedAt:   timestamppb.New(now),
				UpdatedAt:   timestamppb.New(now),
			},
			validate: func(t *testing.T, result *iam.Role) {
				require.NotNil(t, result)
				assert.Equal(t, "role-1", result.ID)
				assert.Equal(t, "tenant-1", result.TenantID)
				assert.Equal(t, "admin", result.Name)
				assert.Equal(t, "Administrator role", result.Description)
				assert.Equal(t, []string{"read", "write", "delete"}, result.Permissions)
			},
		},
		{
			name: "role_without_timestamps",
			input: &iampb.Role{
				Id:   "role-2",
				Name: "viewer",
			},
			validate: func(t *testing.T, result *iam.Role) {
				require.NotNil(t, result)
				assert.Equal(t, "role-2", result.ID)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertProtoToRole(tt.input)
			tt.validate(t, result)
		})
	}
}

func TestConvertRoleToProto(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name     string
		input    *iam.Role
		validate func(t *testing.T, result *iampb.Role)
	}{
		{
			name:  "nil_input",
			input: nil,
			validate: func(t *testing.T, result *iampb.Role) {
				assert.Nil(t, result)
			},
		},
		{
			name: "full_role",
			input: &iam.Role{
				ID:          "role-1",
				TenantID:    "tenant-1",
				Name:        "admin",
				Description: "Administrator role",
				Permissions: []string{"read", "write", "delete"},
				Created:     now,
				Updated:     now,
			},
			validate: func(t *testing.T, result *iampb.Role) {
				require.NotNil(t, result)
				assert.Equal(t, "role-1", result.Id)
				assert.Equal(t, "tenant-1", result.TenantId)
				assert.Equal(t, "admin", result.Name)
				assert.Equal(t, "Administrator role", result.Description)
				assert.Equal(t, []string{"read", "write", "delete"}, result.Permissions)
				assert.NotNil(t, result.CreatedAt)
				assert.NotNil(t, result.UpdatedAt)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertRoleToProto(tt.input)
			tt.validate(t, result)
		})
	}
}

// =============================================================================
// Application Conversion Tests
// =============================================================================

func TestConvertProtoToApplication(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name     string
		input    *iampb.Application
		validate func(t *testing.T, result *iam.Application)
	}{
		{
			name:  "nil_input",
			input: nil,
			validate: func(t *testing.T, result *iam.Application) {
				assert.Nil(t, result)
			},
		},
		{
			name: "full_application",
			input: &iampb.Application{
				ClientId:       "client-123",
				ClientSecret:   "secret-456",
				TenantId:       "tenant-1",
				Name:           "Test App",
				RedirectUris:   []string{"https://example.com/callback"},
				ResourcePrefix: "api/v1",
				GrantTypes:     []string{"authorization_code", "refresh_token"},
				ResponseTypes:  []string{"code"},
				CreatedAt:      timestamppb.New(now),
				UpdatedAt:      timestamppb.New(now),
			},
			validate: func(t *testing.T, result *iam.Application) {
				require.NotNil(t, result)
				assert.Equal(t, "client-123", result.ClientID)
				assert.Equal(t, "secret-456", result.ClientSecret)
				assert.Equal(t, "tenant-1", result.TenantID)
				assert.Equal(t, "Test App", result.Name)
				assert.Equal(t, []string{"https://example.com/callback"}, result.RedirectURIs)
				assert.Equal(t, "api/v1", result.ResourcePrefix)
				assert.Equal(t, []string{"authorization_code", "refresh_token"}, result.GrantTypes)
				assert.Equal(t, []string{"code"}, result.ResponseTypes)
			},
		},
		{
			name: "minimal_application",
			input: &iampb.Application{
				ClientId: "client-minimal",
				Name:     "Minimal App",
			},
			validate: func(t *testing.T, result *iam.Application) {
				require.NotNil(t, result)
				assert.Equal(t, "client-minimal", result.ClientID)
				assert.Equal(t, "Minimal App", result.Name)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertProtoToApplication(tt.input)
			tt.validate(t, result)
		})
	}
}

func TestConvertApplicationToProto(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name     string
		input    *iam.Application
		validate func(t *testing.T, result *iampb.Application)
	}{
		{
			name:  "nil_input",
			input: nil,
			validate: func(t *testing.T, result *iampb.Application) {
				assert.Nil(t, result)
			},
		},
		{
			name: "full_application",
			input: &iam.Application{
				ClientID:       "client-123",
				ClientSecret:   "secret-456",
				TenantID:       "tenant-1",
				Name:           "Test App",
				RedirectURIs:   []string{"https://example.com/callback"},
				ResourcePrefix: "api/v1",
				GrantTypes:     []string{"authorization_code"},
				ResponseTypes:  []string{"code"},
				Created:        now,
				Updated:        now,
			},
			validate: func(t *testing.T, result *iampb.Application) {
				require.NotNil(t, result)
				assert.Equal(t, "client-123", result.ClientId)
				assert.Equal(t, "secret-456", result.ClientSecret)
				assert.Equal(t, "tenant-1", result.TenantId)
				assert.Equal(t, "Test App", result.Name)
				assert.NotNil(t, result.CreatedAt)
				assert.NotNil(t, result.UpdatedAt)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertApplicationToProto(tt.input)
			tt.validate(t, result)
		})
	}
}

// =============================================================================
// Policy Conversion Tests
// =============================================================================

func TestConvertProtoToPolicy(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name     string
		input    *iampb.Policy
		validate func(t *testing.T, result *iam.Policy)
	}{
		{
			name:  "nil_input",
			input: nil,
			validate: func(t *testing.T, result *iam.Policy) {
				assert.Nil(t, result)
			},
		},
		{
			name: "full_policy",
			input: &iampb.Policy{
				Name:        "AllowRead",
				TenantId:    "tenant-1",
				Description: "Allow read access",
				Labels: map[string]string{
					"env": "prod",
				},
				Priority:  100,
				CreatedAt: timestamppb.New(now),
				UpdatedAt: timestamppb.New(now),
			},
			validate: func(t *testing.T, result *iam.Policy) {
				require.NotNil(t, result)
				assert.Equal(t, "AllowRead", result.Metadata.Name)
				assert.Equal(t, "tenant-1", result.Metadata.Tenant)
				assert.Equal(t, "Allow read access", result.Metadata.Description)
				assert.Equal(t, "prod", result.Metadata.Labels["env"])
				assert.Equal(t, 100, result.Metadata.Priority)
				assert.Equal(t, "iam.quicbaas.io/v1", result.APIVersion)
				assert.Equal(t, "Policy", result.Kind)
			},
		},
		{
			name: "minimal_policy",
			input: &iampb.Policy{
				Name:     "MinimalPolicy",
				TenantId: "tenant-1",
			},
			validate: func(t *testing.T, result *iam.Policy) {
				require.NotNil(t, result)
				assert.Equal(t, "MinimalPolicy", result.Metadata.Name)
				assert.Equal(t, "tenant-1", result.Metadata.Tenant)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertProtoToPolicy(tt.input)
			tt.validate(t, result)
		})
	}
}

func TestConvertPolicyToProto(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name     string
		input    *iam.Policy
		validate func(t *testing.T, result *iampb.Policy)
	}{
		{
			name:  "nil_input",
			input: nil,
			validate: func(t *testing.T, result *iampb.Policy) {
				assert.Nil(t, result)
			},
		},
		{
			name: "full_policy",
			input: &iam.Policy{
				APIVersion: "iam.quicbaas.io/v1",
				Kind:       "Policy",
				Metadata: iam.PolicyMetadata{
					Name:        "AllowRead",
					Tenant:      "tenant-1",
					Description: "Allow read access",
					Labels: map[string]string{
						"env": "prod",
					},
					Priority: 100,
					Created:  now,
					Updated:  now,
				},
			},
			validate: func(t *testing.T, result *iampb.Policy) {
				require.NotNil(t, result)
				assert.Equal(t, "AllowRead", result.Name)
				assert.Equal(t, "tenant-1", result.TenantId)
				assert.Equal(t, "Allow read access", result.Description)
				assert.Equal(t, "prod", result.Labels["env"])
				assert.Equal(t, int32(100), result.Priority)
				assert.NotNil(t, result.CreatedAt)
				assert.NotNil(t, result.UpdatedAt)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertPolicyToProto(tt.input)
			tt.validate(t, result)
		})
	}
}

// =============================================================================
// Subject Conversion Tests (PEP Subject - different from IAM Subject)
// =============================================================================

func TestConvertProtoToSubject(t *testing.T) {
	tests := []struct {
		name     string
		input    *iampb.Subject
		validate func(t *testing.T, result *iam.Subject)
	}{
		{
			name:  "nil_input",
			input: nil,
			validate: func(t *testing.T, result *iam.Subject) {
				assert.Nil(t, result)
			},
		},
		{
			name: "full_subject",
			input: &iampb.Subject{
				Id:       "user-1",
				TenantId: "tenant-1",
				Roles:    []string{"admin", "user"},
				Groups:   []string{"group-1"},
				Attributes: map[string]string{
					"department": "engineering",
				},
			},
			validate: func(t *testing.T, result *iam.Subject) {
				require.NotNil(t, result)
				assert.Equal(t, "user-1", result.ID)
				assert.Equal(t, "tenant-1", result.TenantID)
				assert.Equal(t, []string{"admin", "user"}, result.Roles)
				assert.Equal(t, []string{"group-1"}, result.Groups)
				assert.Equal(t, "engineering", result.Attributes["department"])
			},
		},
		{
			name: "minimal_subject",
			input: &iampb.Subject{
				Id:       "user-2",
				TenantId: "tenant-1",
			},
			validate: func(t *testing.T, result *iam.Subject) {
				require.NotNil(t, result)
				assert.Equal(t, "user-2", result.ID)
				assert.Equal(t, "tenant-1", result.TenantID)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertProtoToSubject(tt.input)
			tt.validate(t, result)
		})
	}
}

func TestConvertSubjectToProto(t *testing.T) {
	tests := []struct {
		name     string
		input    *iam.Subject
		validate func(t *testing.T, result *iampb.Subject)
	}{
		{
			name:  "nil_input",
			input: nil,
			validate: func(t *testing.T, result *iampb.Subject) {
				assert.Nil(t, result)
			},
		},
		{
			name: "full_subject",
			input: &iam.Subject{
				ID:       "user-1",
				TenantID: "tenant-1",
				Roles:    []string{"admin", "user"},
				Groups:   []string{"group-1"},
				Attributes: map[string]interface{}{
					"department": "engineering",
					"level":      123, // non-string will be skipped
				},
			},
			validate: func(t *testing.T, result *iampb.Subject) {
				require.NotNil(t, result)
				assert.Equal(t, "user-1", result.Id)
				assert.Equal(t, "tenant-1", result.TenantId)
				assert.Equal(t, []string{"admin", "user"}, result.Roles)
				assert.Equal(t, []string{"group-1"}, result.Groups)
				assert.Equal(t, "engineering", result.Attributes["department"])
				// Non-string should be skipped
				_, hasLevel := result.Attributes["level"]
				assert.False(t, hasLevel)
			},
		},
		{
			name: "minimal_subject",
			input: &iam.Subject{
				ID:       "user-2",
				TenantID: "tenant-1",
			},
			validate: func(t *testing.T, result *iampb.Subject) {
				require.NotNil(t, result)
				assert.Equal(t, "user-2", result.Id)
				assert.Equal(t, "tenant-1", result.TenantId)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertSubjectToProto(tt.input)
			tt.validate(t, result)
		})
	}
}

// =============================================================================
// Admin Role Conversion Tests
// =============================================================================

func TestConvertProtoAdminRole(t *testing.T) {
	tests := []struct {
		name     string
		input    iampb.AdminRole
		expected iam.AdminRole
	}{
		{"super_admin", iampb.AdminRole_ADMIN_ROLE_SUPER_ADMIN, iam.AdminRoleSuperAdmin},
		{"tenant_admin", iampb.AdminRole_ADMIN_ROLE_TENANT_ADMIN, iam.AdminRoleTenantAdmin},
		{"user_admin", iampb.AdminRole_ADMIN_ROLE_USER_ADMIN, iam.AdminRoleUserAdmin},
		{"group_admin", iampb.AdminRole_ADMIN_ROLE_GROUP_ADMIN, iam.AdminRoleGroupAdmin},
		{"policy_admin", iampb.AdminRole_ADMIN_ROLE_POLICY_ADMIN, iam.AdminRolePolicyAdmin},
		{"audit_admin", iampb.AdminRole_ADMIN_ROLE_AUDIT_ADMIN, iam.AdminRoleAuditAdmin},
		{"read_only", iampb.AdminRole_ADMIN_ROLE_READ_ONLY, iam.AdminRoleReadOnly},
		{"unspecified", iampb.AdminRole_ADMIN_ROLE_UNSPECIFIED, ""},
		{"unknown", iampb.AdminRole(999), ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertProtoAdminRole(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConvertAdminRoleToProto(t *testing.T) {
	tests := []struct {
		name     string
		input    iam.AdminRole
		expected iampb.AdminRole
	}{
		{"super_admin", iam.AdminRoleSuperAdmin, iampb.AdminRole_ADMIN_ROLE_SUPER_ADMIN},
		{"tenant_admin", iam.AdminRoleTenantAdmin, iampb.AdminRole_ADMIN_ROLE_TENANT_ADMIN},
		{"user_admin", iam.AdminRoleUserAdmin, iampb.AdminRole_ADMIN_ROLE_USER_ADMIN},
		{"group_admin", iam.AdminRoleGroupAdmin, iampb.AdminRole_ADMIN_ROLE_GROUP_ADMIN},
		{"policy_admin", iam.AdminRolePolicyAdmin, iampb.AdminRole_ADMIN_ROLE_POLICY_ADMIN},
		{"audit_admin", iam.AdminRoleAuditAdmin, iampb.AdminRole_ADMIN_ROLE_AUDIT_ADMIN},
		{"read_only", iam.AdminRoleReadOnly, iampb.AdminRole_ADMIN_ROLE_READ_ONLY},
		{"unknown", iam.AdminRole("unknown"), iampb.AdminRole_ADMIN_ROLE_UNSPECIFIED},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertAdminRoleToProto(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// --- Merged from converters_iam_coverage_test.go ---

// =============================================================================
// Tenant Conversions
// =============================================================================

func TestConvertProtoToTenant_Nil(t *testing.T) {
	assert.Nil(t, convertProtoToTenant(nil))
}

func TestConvertProtoToTenant_Full(t *testing.T) {
	now := time.Now()
	pb := &iampb.Tenant{
		Id:   "tenant-1",
		Name: "Test Tenant",
		Settings: &iampb.TenantSettings{
			MaxUsers:      100,
			MfaRequired:   true,
			NamedNetworks: map[string]string{"office": "10.0.0.0/8"},
		},
		CreatedAt: timestamppb.New(now),
		UpdatedAt: timestamppb.New(now),
	}

	result := convertProtoToTenant(pb)
	assert.Equal(t, "tenant-1", result.ID)
	assert.Equal(t, "Test Tenant", result.Name)
	assert.Equal(t, 100, result.Settings.MaxUsers)
	assert.True(t, result.Settings.MFARequired)
	assert.Contains(t, result.Settings.NamedNetworks, "office")
}

func TestConvertTenantToProto_Nil(t *testing.T) {
	assert.Nil(t, convertTenantToProto(nil))
}

func TestConvertTenantToProto_Full(t *testing.T) {
	tenant := &iam.Tenant{
		ID:   "tenant-1",
		Name: "Test Tenant",
		Settings: iam.TenantSettings{
			MaxUsers:      50,
			MFARequired:   true,
			NamedNetworks: map[string][]string{"office": {"10.0.0.0/8"}},
		},
		Created: time.Now(),
		Updated: time.Now(),
	}

	result := convertTenantToProto(tenant)
	assert.Equal(t, "tenant-1", result.Id)
	assert.Equal(t, "Test Tenant", result.Name)
	assert.Equal(t, int32(50), result.Settings.MaxUsers)
	assert.True(t, result.Settings.MfaRequired)
	assert.Equal(t, "10.0.0.0/8", result.Settings.NamedNetworks["office"])
}

// =============================================================================
// User Conversions
// =============================================================================

func TestConvertProtoToUser_Nil(t *testing.T) {
	assert.Nil(t, convertProtoToUser(nil))
}

func TestConvertProtoToUser_Full(t *testing.T) {
	now := time.Now()
	pb := &iampb.User{
		Id:         "user-1",
		TenantId:   "tenant-1",
		Email:      "test@example.com",
		Name:       "Test User",
		Roles:      []string{"admin"},
		Groups:     []string{"developers"},
		Attributes: map[string]string{"key": "value"},
		CreatedAt:  timestamppb.New(now),
		UpdatedAt:  timestamppb.New(now),
		LastAuth:   timestamppb.New(now),
	}

	result := convertProtoToUser(pb)
	assert.Equal(t, "user-1", result.ID)
	assert.Equal(t, "test@example.com", result.Email)
	assert.Equal(t, "value", result.Attributes["key"])
	assert.NotNil(t, result.LastAuth)
}

func TestConvertUserToProto_Nil(t *testing.T) {
	assert.Nil(t, convertUserToProto(nil))
}

func TestConvertUserToProto_Full(t *testing.T) {
	now := time.Now()
	user := &iam.User{
		ID:         "user-1",
		TenantID:   "tenant-1",
		Email:      "test@example.com",
		Name:       "Test User",
		Roles:      []string{"admin"},
		Groups:     []string{"devs"},
		Attributes: map[string]string{"key": "value", "num": "42"},
		Created:    now,
		Updated:    now,
		LastAuth:   &now,
	}

	result := convertUserToProto(user)
	assert.Equal(t, "user-1", result.Id)
	assert.Equal(t, "value", result.Attributes["key"])
	assert.Equal(t, "42", result.Attributes["num"])
	assert.NotNil(t, result.LastAuth)
}

// =============================================================================
// Group Conversions
// =============================================================================

func TestConvertProtoToGroup_Nil(t *testing.T) {
	assert.Nil(t, convertProtoToGroup(nil))
}

func TestConvertProtoToGroup_Full(t *testing.T) {
	now := time.Now()
	pb := &iampb.Group{
		Id:          "group-1",
		TenantId:    "tenant-1",
		Name:        "Developers",
		Description: "Dev team",
		Members:     []string{"user-1", "user-2"},
		CreatedAt:   timestamppb.New(now),
		UpdatedAt:   timestamppb.New(now),
	}

	result := convertProtoToGroup(pb)
	assert.Equal(t, "group-1", result.ID)
	assert.Equal(t, "Developers", result.Name)
	assert.Len(t, result.Members, 2)
}

func TestConvertGroupToProto_Nil(t *testing.T) {
	assert.Nil(t, convertGroupToProto(nil))
}

func TestConvertGroupToProto_Full(t *testing.T) {
	group := &iam.Group{
		ID: "group-1", TenantID: "tenant-1", Name: "Devs",
		Description: "Dev team", Members: []string{"u1"},
		Created: time.Now(), Updated: time.Now(),
	}
	result := convertGroupToProto(group)
	assert.Equal(t, "group-1", result.Id)
	assert.Equal(t, "Devs", result.Name)
}

// =============================================================================
// Role Conversions
// =============================================================================

func TestConvertProtoToRole_Nil(t *testing.T) {
	assert.Nil(t, convertProtoToRole(nil))
}

func TestConvertProtoToRole_Full(t *testing.T) {
	now := time.Now()
	pb := &iampb.Role{
		Id: "role-1", TenantId: "t1", Name: "Admin",
		Description: "Admin role", Permissions: []string{"read", "write"},
		CreatedAt: timestamppb.New(now), UpdatedAt: timestamppb.New(now),
	}
	result := convertProtoToRole(pb)
	assert.Equal(t, "role-1", result.ID)
	assert.Equal(t, "Admin", result.Name)
	assert.Len(t, result.Permissions, 2)
}

func TestConvertRoleToProto_Nil(t *testing.T) {
	assert.Nil(t, convertRoleToProto(nil))
}

func TestConvertRoleToProto_Full(t *testing.T) {
	role := &iam.Role{
		ID: "role-1", TenantID: "t1", Name: "Admin",
		Permissions: []string{"read"}, Created: time.Now(), Updated: time.Now(),
	}
	result := convertRoleToProto(role)
	assert.Equal(t, "role-1", result.Id)
}

// TestConvertRoleToProto_RetiredPermissions pins that the drift a role carries
// survives the hop to the wire: retired permissions are derived on read, so a
// converter that dropped them would hide the drift from every gRPC caller.
func TestConvertRoleToProto_RetiredPermissions(t *testing.T) {
	role := &iam.Role{
		ID: "role-1", TenantID: "t1", Name: "admin",
		Permissions:        []string{"kv:get", "kv:legacy"},
		RetiredPermissions: []string{"kv:legacy"},
	}

	result := convertRoleToProto(role)

	require.NotNil(t, result)
	assert.Equal(t, []string{"kv:legacy"}, result.RetiredPermissions)
}

// TestConvertRoleToProto_NoRetiredPermissions is the agreeing-with-the-build
// case: nothing retired means nothing on the wire.
func TestConvertRoleToProto_NoRetiredPermissions(t *testing.T) {
	result := convertRoleToProto(&iam.Role{ID: "role-1", TenantID: "t1", Name: "admin"})

	require.NotNil(t, result)
	assert.Empty(t, result.RetiredPermissions)
}

func TestConvertProtoToRole_RetiredPermissions(t *testing.T) {
	result := convertProtoToRole(&iampb.Role{
		Id: "role-1", TenantId: "t1", Name: "admin",
		Permissions:        []string{"kv:get", "kv:legacy"},
		RetiredPermissions: []string{"kv:legacy"},
	})

	require.NotNil(t, result)
	assert.Equal(t, []string{"kv:legacy"}, result.RetiredPermissions)
}

func TestConvertRoleReconcileReportToProto_Full(t *testing.T) {
	report := &iam.RoleReconcileReport{
		TenantID: "tenant-1",
		Roles: []iam.RoleReconcileResult{
			{RoleID: "r-admin", Name: "admin", Added: []string{"roles:create"}},
			{RoleID: "r-viewer", Name: "viewer", Created: true, Removed: []string{"kv:legacy"}},
		},
	}

	result := convertRoleReconcileReportToProto(report)

	require.NotNil(t, result)
	assert.Equal(t, "tenant-1", result.TenantId)
	assert.True(t, result.Changed)
	require.Len(t, result.Roles, 2)
	assert.Equal(t, "r-admin", result.Roles[0].RoleId)
	assert.Equal(t, []string{"roles:create"}, result.Roles[0].Added)
	assert.True(t, result.Roles[1].Created)
	assert.Equal(t, []string{"kv:legacy"}, result.Roles[1].Removed)
}

// TestConvertRoleReconcileReportToProto_Unchanged pins that an empty report
// converts to changed=false rather than to a nil or misleading response.
func TestConvertRoleReconcileReportToProto_Unchanged(t *testing.T) {
	result := convertRoleReconcileReportToProto(&iam.RoleReconcileReport{TenantID: "tenant-1"})

	require.NotNil(t, result)
	assert.Equal(t, "tenant-1", result.TenantId)
	assert.False(t, result.Changed)
	assert.Empty(t, result.Roles)
}

func TestConvertRoleReconcileReportToProto_Nil(t *testing.T) {
	assert.Nil(t, convertRoleReconcileReportToProto(nil))
}

func TestConvertRoleReconcileResultToProto_Full(t *testing.T) {
	result := convertRoleReconcileResultToProto(&iam.RoleReconcileResult{
		RoleID:  "r-admin",
		Name:    "admin",
		Created: true,
		Added:   []string{"kv:get"},
		Removed: []string{"kv:legacy"},
	})

	require.NotNil(t, result)
	assert.Equal(t, "r-admin", result.RoleId)
	assert.Equal(t, "admin", result.Name)
	assert.True(t, result.Created)
	assert.Equal(t, []string{"kv:get"}, result.Added)
	assert.Equal(t, []string{"kv:legacy"}, result.Removed)
}

func TestConvertRoleReconcileResultToProto_Nil(t *testing.T) {
	assert.Nil(t, convertRoleReconcileResultToProto(nil))
}

// =============================================================================
// Application Conversions
// =============================================================================

func TestConvertProtoToApplication_Nil(t *testing.T) {
	assert.Nil(t, convertProtoToApplication(nil))
}

func TestConvertProtoToApplication_Full(t *testing.T) {
	now := time.Now()
	pb := &iampb.Application{
		ClientId: "app-1", ClientSecret: "secret", TenantId: "t1",
		Name: "MyApp", RedirectUris: []string{"http://localhost"},
		ResourcePrefix: "myapp", GrantTypes: []string{"authorization_code"},
		ResponseTypes: []string{"code"},
		CreatedAt:     timestamppb.New(now), UpdatedAt: timestamppb.New(now),
	}
	result := convertProtoToApplication(pb)
	assert.Equal(t, "app-1", result.ClientID)
	assert.Equal(t, "MyApp", result.Name)
}

func TestConvertApplicationToProto_Nil(t *testing.T) {
	assert.Nil(t, convertApplicationToProto(nil))
}

func TestConvertApplicationToProto_Full(t *testing.T) {
	app := &iam.Application{
		ClientID: "app-1", TenantID: "t1", Name: "MyApp",
		RedirectURIs: []string{"http://localhost"},
		Created:      time.Now(), Updated: time.Now(),
	}
	result := convertApplicationToProto(app)
	assert.Equal(t, "app-1", result.ClientId)
}

// =============================================================================
// Policy Conversions
// =============================================================================

func TestConvertProtoToPolicy_Nil(t *testing.T) {
	assert.Nil(t, convertProtoToPolicy(nil))
}

func TestConvertProtoToPolicy_Full(t *testing.T) {
	now := time.Now()
	pb := &iampb.Policy{
		Name: "policy-1", TenantId: "t1", Description: "Test policy",
		Labels: map[string]string{"env": "test"}, Priority: 10,
		CreatedAt: timestamppb.New(now), UpdatedAt: timestamppb.New(now),
	}
	result := convertProtoToPolicy(pb)
	assert.Equal(t, "policy-1", result.Metadata.Name)
	assert.Equal(t, 10, result.Metadata.Priority)
}

func TestConvertPolicyToProto_Nil(t *testing.T) {
	assert.Nil(t, convertPolicyToProto(nil))
}

func TestConvertPolicyToProto_Full(t *testing.T) {
	policy := &iam.Policy{
		Metadata: iam.PolicyMetadata{
			Name: "p1", Tenant: "t1", Description: "desc",
			Labels: map[string]string{"a": "b"}, Priority: 5,
			Created: time.Now(), Updated: time.Now(),
		},
	}
	result := convertPolicyToProto(policy)
	assert.Equal(t, "p1", result.Name)
	assert.Equal(t, int32(5), result.Priority)
}

// =============================================================================
// Subject Conversions
// =============================================================================

func TestConvertProtoToSubject_Nil(t *testing.T) {
	assert.Nil(t, convertProtoToSubject(nil))
}

func TestConvertProtoToSubject_Full(t *testing.T) {
	pb := &iampb.Subject{
		Id: "sub-1", TenantId: "t1", Roles: []string{"admin"},
		Groups: []string{"devs"}, MfaVerified: true,
		Attributes: map[string]string{"ip": "1.2.3.4"},
	}
	result := convertProtoToSubject(pb)
	assert.Equal(t, "sub-1", result.ID)
	assert.Equal(t, "1.2.3.4", result.Attributes["ip"])
}

func TestConvertSubjectToProto_Nil(t *testing.T) {
	assert.Nil(t, convertSubjectToProto(nil))
}

func TestConvertSubjectToProto_Full(t *testing.T) {
	subject := &iam.Subject{
		ID: "sub-1", TenantID: "t1", Roles: []string{"admin"},
		Groups: []string{"devs"}, MFAVerified: true,
		Attributes: map[string]interface{}{"ip": "1.2.3.4", "num": 42},
	}
	result := convertSubjectToProto(subject)
	assert.Equal(t, "sub-1", result.Id)
	assert.Equal(t, "1.2.3.4", result.Attributes["ip"])
	// Non-string attribute skipped
	_, exists := result.Attributes["num"]
	assert.False(t, exists)
}

// =============================================================================
// Admin Role Conversions
// =============================================================================

func TestConvertProtoAdminRole_AllTypes(t *testing.T) {
	tests := []struct {
		proto    iampb.AdminRole
		expected iam.AdminRole
	}{
		{iampb.AdminRole_ADMIN_ROLE_SUPER_ADMIN, iam.AdminRoleSuperAdmin},
		{iampb.AdminRole_ADMIN_ROLE_TENANT_ADMIN, iam.AdminRoleTenantAdmin},
		{iampb.AdminRole_ADMIN_ROLE_USER_ADMIN, iam.AdminRoleUserAdmin},
		{iampb.AdminRole_ADMIN_ROLE_GROUP_ADMIN, iam.AdminRoleGroupAdmin},
		{iampb.AdminRole_ADMIN_ROLE_POLICY_ADMIN, iam.AdminRolePolicyAdmin},
		{iampb.AdminRole_ADMIN_ROLE_AUDIT_ADMIN, iam.AdminRoleAuditAdmin},
		{iampb.AdminRole_ADMIN_ROLE_READ_ONLY, iam.AdminRoleReadOnly},
		{iampb.AdminRole_ADMIN_ROLE_UNSPECIFIED, ""},
	}
	for _, tc := range tests {
		assert.Equal(t, tc.expected, convertProtoAdminRole(tc.proto))
	}
}

func TestConvertAdminRoleToProto_AllTypes(t *testing.T) {
	tests := []struct {
		domain   iam.AdminRole
		expected iampb.AdminRole
	}{
		{iam.AdminRoleSuperAdmin, iampb.AdminRole_ADMIN_ROLE_SUPER_ADMIN},
		{iam.AdminRoleTenantAdmin, iampb.AdminRole_ADMIN_ROLE_TENANT_ADMIN},
		{iam.AdminRoleUserAdmin, iampb.AdminRole_ADMIN_ROLE_USER_ADMIN},
		{iam.AdminRoleGroupAdmin, iampb.AdminRole_ADMIN_ROLE_GROUP_ADMIN},
		{iam.AdminRolePolicyAdmin, iampb.AdminRole_ADMIN_ROLE_POLICY_ADMIN},
		{iam.AdminRoleAuditAdmin, iampb.AdminRole_ADMIN_ROLE_AUDIT_ADMIN},
		{iam.AdminRoleReadOnly, iampb.AdminRole_ADMIN_ROLE_READ_ONLY},
		{"unknown", iampb.AdminRole_ADMIN_ROLE_UNSPECIFIED},
	}
	for _, tc := range tests {
		assert.Equal(t, tc.expected, convertAdminRoleToProto(tc.domain))
	}
}

// =============================================================================
// Admin Assignment Conversions
// =============================================================================

func TestConvertAdminAssignmentToProto_Nil(t *testing.T) {
	assert.Nil(t, convertAdminAssignmentToProto(nil))
}

func TestConvertAdminAssignmentToProto_Full(t *testing.T) {
	now := time.Now()
	expires := now.Add(24 * time.Hour)
	a := &iam.AdminAssignment{
		ID: "aa-1", UserID: "u1", AdminRole: iam.AdminRoleSuperAdmin,
		TenantIDs: []string{"t1"}, GrantedBy: "admin",
		GrantedAt: now, Active: true,
		Permissions:       []iam.AdminPermission{"read", "write"},
		DeniedPermissions: []iam.AdminPermission{"delete"},
		ResourceScopes: []iam.ResourceScope{
			{ResourceType: "kv", ResourcePattern: "*"},
		},
		ExpiresAt:  &expires,
		LastUsedAt: &now,
		Metadata:   map[string]string{"reason": "test"},
	}
	result := convertAdminAssignmentToProto(a)
	assert.Equal(t, "aa-1", result.Id)
	assert.Len(t, result.Permissions, 2)
	assert.Len(t, result.DeniedPermissions, 1)
	assert.Len(t, result.ResourceScopes, 1)
	assert.NotNil(t, result.ExpiresAt)
	assert.NotNil(t, result.LastUsedAt)
}

func TestConvertProtoToAdminAssignment_Nil(t *testing.T) {
	assert.Nil(t, convertProtoToAdminAssignment(nil))
}

func TestConvertProtoToAdminAssignment_Full(t *testing.T) {
	now := time.Now()
	pb := &iampb.AdminAssignment{
		Id: "aa-1", UserId: "u1",
		AdminRole: iampb.AdminRole_ADMIN_ROLE_SUPER_ADMIN,
		TenantIds: []string{"t1"}, GrantedBy: "admin",
		GrantedAt: timestamppb.New(now), Active: true,
		Permissions:       []string{"read"},
		DeniedPermissions: []string{"delete"},
		ResourceScopes: []*iampb.ResourceScope{
			{ResourceType: "kv", ResourcePattern: "*"},
		},
		ExpiresAt:  timestamppb.New(now.Add(24 * time.Hour)),
		LastUsedAt: timestamppb.New(now),
		Metadata:   map[string]string{"reason": "test"},
	}
	result := convertProtoToAdminAssignment(pb)
	assert.Equal(t, "aa-1", result.ID)
	assert.Equal(t, iam.AdminRoleSuperAdmin, result.AdminRole)
	assert.Len(t, result.Permissions, 1)
	assert.Len(t, result.DeniedPermissions, 1)
	assert.Len(t, result.ResourceScopes, 1)
	assert.NotNil(t, result.ExpiresAt)
	assert.NotNil(t, result.LastUsedAt)
}

// =============================================================================
// Impersonation Session Conversions
// =============================================================================

func TestConvertImpersonationSessionToProto_Nil(t *testing.T) {
	assert.Nil(t, convertImpersonationSessionToProto(nil))
}

func TestConvertImpersonationSessionToProto_Full(t *testing.T) {
	now := time.Now()
	ended := now.Add(time.Hour)
	s := &iam.ImpersonationSession{
		ID: "imp-1", AdminUserID: "admin-1", TargetUserID: "user-1",
		TenantID: "t1", Reason: "support ticket",
		StartedAt: now, ExpiresAt: now.Add(2 * time.Hour),
		EndedAt: &ended, AuditLogID: "audit-1",
	}
	result := convertImpersonationSessionToProto(s)
	assert.Equal(t, "imp-1", result.Id)
	assert.Equal(t, "admin-1", result.AdminUserId)
	assert.NotNil(t, result.EndedAt)
}

func TestConvertProtoToImpersonationSession_Nil(t *testing.T) {
	assert.Nil(t, convertProtoToImpersonationSession(nil))
}

func TestConvertProtoToImpersonationSession_Full(t *testing.T) {
	now := time.Now()
	pb := &iampb.ImpersonationSession{
		Id: "imp-1", AdminUserId: "admin-1", TargetUserId: "user-1",
		TenantId: "t1", Reason: "support",
		StartedAt: timestamppb.New(now), ExpiresAt: timestamppb.New(now.Add(time.Hour)),
		EndedAt: timestamppb.New(now.Add(30 * time.Minute)), AuditLogId: "a1",
	}
	result := convertProtoToImpersonationSession(pb)
	assert.Equal(t, "imp-1", result.ID)
	assert.NotNil(t, result.EndedAt)
}

// =============================================================================
// Consent Purpose Conversions
// =============================================================================

func TestConvertProtoConsentPurpose_AllTypes(t *testing.T) {
	tests := []struct {
		proto    iampb.ConsentPurpose
		expected iam.ConsentPurpose
	}{
		{iampb.ConsentPurpose_CONSENT_PURPOSE_MARKETING, iam.ConsentPurposeMarketing},
		{iampb.ConsentPurpose_CONSENT_PURPOSE_ANALYTICS, iam.ConsentPurposeAnalytics},
		{iampb.ConsentPurpose_CONSENT_PURPOSE_PERSONALIZATION, iam.ConsentPurposePersonalization},
		{iampb.ConsentPurpose_CONSENT_PURPOSE_THIRD_PARTY, iam.ConsentPurposeThirdParty},
		{iampb.ConsentPurpose_CONSENT_PURPOSE_DATA_PROCESSING, iam.ConsentPurposeDataProcessing},
		{iampb.ConsentPurpose_CONSENT_PURPOSE_DATA_RETENTION, iam.ConsentPurposeDataRetention},
		{iampb.ConsentPurpose_CONSENT_PURPOSE_FUNCTIONAL, iam.ConsentPurposeFunctional},
		{iampb.ConsentPurpose_CONSENT_PURPOSE_UNSPECIFIED, ""},
	}
	for _, tc := range tests {
		assert.Equal(t, tc.expected, convertProtoConsentPurpose(tc.proto))
	}
}

func TestConvertConsentPurposeToProto_AllTypes(t *testing.T) {
	tests := []struct {
		domain   iam.ConsentPurpose
		expected iampb.ConsentPurpose
	}{
		{iam.ConsentPurposeMarketing, iampb.ConsentPurpose_CONSENT_PURPOSE_MARKETING},
		{iam.ConsentPurposeAnalytics, iampb.ConsentPurpose_CONSENT_PURPOSE_ANALYTICS},
		{iam.ConsentPurposePersonalization, iampb.ConsentPurpose_CONSENT_PURPOSE_PERSONALIZATION},
		{iam.ConsentPurposeThirdParty, iampb.ConsentPurpose_CONSENT_PURPOSE_THIRD_PARTY},
		{iam.ConsentPurposeDataProcessing, iampb.ConsentPurpose_CONSENT_PURPOSE_DATA_PROCESSING},
		{iam.ConsentPurposeDataRetention, iampb.ConsentPurpose_CONSENT_PURPOSE_DATA_RETENTION},
		{iam.ConsentPurposeFunctional, iampb.ConsentPurpose_CONSENT_PURPOSE_FUNCTIONAL},
		{"unknown", iampb.ConsentPurpose_CONSENT_PURPOSE_UNSPECIFIED},
	}
	for _, tc := range tests {
		assert.Equal(t, tc.expected, convertConsentPurposeToProto(tc.domain))
	}
}

// =============================================================================
// Consent Status Conversions
// =============================================================================

func TestConvertProtoConsentStatus_AllTypes(t *testing.T) {
	tests := []struct {
		proto    iampb.ConsentStatus
		expected iam.ConsentStatus
	}{
		{iampb.ConsentStatus_CONSENT_STATUS_GRANTED, iam.ConsentStatusGranted},
		{iampb.ConsentStatus_CONSENT_STATUS_DENIED, iam.ConsentStatusDenied},
		{iampb.ConsentStatus_CONSENT_STATUS_WITHDRAWN, iam.ConsentStatusWithdrawn},
		{iampb.ConsentStatus_CONSENT_STATUS_PENDING, iam.ConsentStatusPending},
		{iampb.ConsentStatus_CONSENT_STATUS_UNSPECIFIED, ""},
	}
	for _, tc := range tests {
		assert.Equal(t, tc.expected, convertProtoConsentStatus(tc.proto))
	}
}

func TestConvertConsentStatusToProto_AllTypes(t *testing.T) {
	tests := []struct {
		domain   iam.ConsentStatus
		expected iampb.ConsentStatus
	}{
		{iam.ConsentStatusGranted, iampb.ConsentStatus_CONSENT_STATUS_GRANTED},
		{iam.ConsentStatusDenied, iampb.ConsentStatus_CONSENT_STATUS_DENIED},
		{iam.ConsentStatusWithdrawn, iampb.ConsentStatus_CONSENT_STATUS_WITHDRAWN},
		{iam.ConsentStatusPending, iampb.ConsentStatus_CONSENT_STATUS_PENDING},
		{"unknown", iampb.ConsentStatus_CONSENT_STATUS_UNSPECIFIED},
	}
	for _, tc := range tests {
		assert.Equal(t, tc.expected, convertConsentStatusToProto(tc.domain))
	}
}

// =============================================================================
// Consent Conversions
// =============================================================================

func TestConvertConsentToProto_Nil(t *testing.T) {
	assert.Nil(t, convertConsentToProto(nil))
}

func TestConvertConsentToProto_Full(t *testing.T) {
	now := time.Now()
	granted := now
	withdrawn := now.Add(time.Hour)
	expires := now.Add(24 * time.Hour)
	c := &iam.Consent{
		ID: "consent-1", UserID: "u1", TenantID: "t1",
		Purpose: iam.ConsentPurposeMarketing, Status: iam.ConsentStatusGranted,
		Version: "v1", IPAddress: "127.0.0.1", UserAgent: "test",
		Metadata: map[string]string{"k": "v"},
		Created:  now, Updated: now,
		GrantedAt: &granted, WithdrawnAt: &withdrawn, ExpiresAt: &expires,
	}
	result := convertConsentToProto(c)
	assert.Equal(t, "consent-1", result.Id)
	assert.NotNil(t, result.GrantedAt)
	assert.NotNil(t, result.WithdrawnAt)
	assert.NotNil(t, result.ExpiresAt)
}

func TestConvertProtoToConsent_Nil(t *testing.T) {
	assert.Nil(t, convertProtoToConsent(nil))
}

func TestConvertProtoToConsent_Full(t *testing.T) {
	now := time.Now()
	pb := &iampb.Consent{
		Id: "consent-1", UserId: "u1", TenantId: "t1",
		Purpose: iampb.ConsentPurpose_CONSENT_PURPOSE_MARKETING,
		Status:  iampb.ConsentStatus_CONSENT_STATUS_GRANTED,
		Version: "v1", IpAddress: "127.0.0.1", UserAgent: "test",
		Metadata:  map[string]string{"k": "v"},
		CreatedAt: timestamppb.New(now), UpdatedAt: timestamppb.New(now),
		GrantedAt: timestamppb.New(now), WithdrawnAt: timestamppb.New(now),
		ExpiresAt: timestamppb.New(now.Add(time.Hour)),
	}
	result := convertProtoToConsent(pb)
	assert.Equal(t, "consent-1", result.ID)
	assert.NotNil(t, result.GrantedAt)
	assert.NotNil(t, result.WithdrawnAt)
	assert.NotNil(t, result.ExpiresAt)
}

// =============================================================================
// Data Export Format Conversions
// =============================================================================

func TestConvertProtoDataExportFormat_AllTypes(t *testing.T) {
	tests := []struct {
		proto    iampb.DataExportFormat
		expected iam.DataExportFormat
	}{
		{iampb.DataExportFormat_DATA_EXPORT_FORMAT_JSON, iam.DataExportFormatJSON},
		{iampb.DataExportFormat_DATA_EXPORT_FORMAT_CSV, iam.DataExportFormatCSV},
		{iampb.DataExportFormat_DATA_EXPORT_FORMAT_XML, iam.DataExportFormatXML},
		{iampb.DataExportFormat_DATA_EXPORT_FORMAT_UNSPECIFIED, iam.DataExportFormatJSON},
	}
	for _, tc := range tests {
		assert.Equal(t, tc.expected, convertProtoDataExportFormat(tc.proto))
	}
}

func TestConvertDataExportFormatToProto_AllTypes(t *testing.T) {
	tests := []struct {
		domain   iam.DataExportFormat
		expected iampb.DataExportFormat
	}{
		{iam.DataExportFormatJSON, iampb.DataExportFormat_DATA_EXPORT_FORMAT_JSON},
		{iam.DataExportFormatCSV, iampb.DataExportFormat_DATA_EXPORT_FORMAT_CSV},
		{iam.DataExportFormatXML, iampb.DataExportFormat_DATA_EXPORT_FORMAT_XML},
		{"unknown", iampb.DataExportFormat_DATA_EXPORT_FORMAT_UNSPECIFIED},
	}
	for _, tc := range tests {
		assert.Equal(t, tc.expected, convertDataExportFormatToProto(tc.domain))
	}
}

// =============================================================================
// Data Export Status Conversions
// =============================================================================

func TestConvertProtoDataExportStatus_AllTypes(t *testing.T) {
	tests := []struct {
		proto    iampb.DataExportStatus
		expected iam.DataExportStatus
	}{
		{iampb.DataExportStatus_DATA_EXPORT_STATUS_PENDING, iam.DataExportStatusPending},
		{iampb.DataExportStatus_DATA_EXPORT_STATUS_PROCESSING, iam.DataExportStatusProcessing},
		{iampb.DataExportStatus_DATA_EXPORT_STATUS_COMPLETED, iam.DataExportStatusCompleted},
		{iampb.DataExportStatus_DATA_EXPORT_STATUS_FAILED, iam.DataExportStatusFailed},
		{iampb.DataExportStatus_DATA_EXPORT_STATUS_EXPIRED, iam.DataExportStatusExpired},
		{iampb.DataExportStatus_DATA_EXPORT_STATUS_UNSPECIFIED, ""},
	}
	for _, tc := range tests {
		assert.Equal(t, tc.expected, convertProtoDataExportStatus(tc.proto))
	}
}

func TestConvertDataExportStatusToProto_AllTypes(t *testing.T) {
	tests := []struct {
		domain   iam.DataExportStatus
		expected iampb.DataExportStatus
	}{
		{iam.DataExportStatusPending, iampb.DataExportStatus_DATA_EXPORT_STATUS_PENDING},
		{iam.DataExportStatusProcessing, iampb.DataExportStatus_DATA_EXPORT_STATUS_PROCESSING},
		{iam.DataExportStatusCompleted, iampb.DataExportStatus_DATA_EXPORT_STATUS_COMPLETED},
		{iam.DataExportStatusFailed, iampb.DataExportStatus_DATA_EXPORT_STATUS_FAILED},
		{iam.DataExportStatusExpired, iampb.DataExportStatus_DATA_EXPORT_STATUS_EXPIRED},
		{"unknown", iampb.DataExportStatus_DATA_EXPORT_STATUS_UNSPECIFIED},
	}
	for _, tc := range tests {
		assert.Equal(t, tc.expected, convertDataExportStatusToProto(tc.domain))
	}
}

// =============================================================================
// Data Export Request Conversions
// =============================================================================

func TestConvertDataExportRequestToProto_Nil(t *testing.T) {
	assert.Nil(t, convertDataExportRequestToProto(nil))
}

func TestConvertDataExportRequestToProto_Full(t *testing.T) {
	now := time.Now()
	started := now
	completed := now.Add(time.Hour)
	expires := now.Add(24 * time.Hour)
	r := &iam.DataExportRequest{
		ID: "exp-1", UserID: "u1", TenantID: "t1",
		RequestedBy: "admin", Format: iam.DataExportFormatJSON,
		Status:            iam.DataExportStatusCompleted,
		IncludeCategories: []string{"profile"}, ExcludeCategories: []string{"audit"},
		RequestedAt: now, DownloadURL: "https://example.com/download",
		FileSizeBytes: 1024, ErrorMessage: "",
		Metadata:  map[string]string{"k": "v"},
		StartedAt: &started, CompletedAt: &completed, ExpiresAt: &expires,
	}
	result := convertDataExportRequestToProto(r)
	assert.Equal(t, "exp-1", result.Id)
	assert.NotNil(t, result.StartedAt)
	assert.NotNil(t, result.CompletedAt)
	assert.NotNil(t, result.ExpiresAt)
}

func TestConvertProtoToDataExportRequest_Nil(t *testing.T) {
	assert.Nil(t, convertProtoToDataExportRequest(nil))
}

func TestConvertProtoToDataExportRequest_Full(t *testing.T) {
	now := time.Now()
	pb := &iampb.DataExportRequest{
		Id: "exp-1", UserId: "u1", TenantId: "t1",
		RequestedBy: "admin",
		Format:      iampb.DataExportFormat_DATA_EXPORT_FORMAT_JSON,
		Status:      iampb.DataExportStatus_DATA_EXPORT_STATUS_COMPLETED,
		RequestedAt: timestamppb.New(now),
		StartedAt:   timestamppb.New(now), CompletedAt: timestamppb.New(now),
		ExpiresAt: timestamppb.New(now.Add(time.Hour)),
	}
	result := convertProtoToDataExportRequest(pb)
	assert.Equal(t, "exp-1", result.ID)
	assert.NotNil(t, result.StartedAt)
	assert.NotNil(t, result.CompletedAt)
	assert.NotNil(t, result.ExpiresAt)
}

// =============================================================================
// Deletion Status Conversions
// =============================================================================

func TestConvertProtoDeletionStatus_AllTypes(t *testing.T) {
	tests := []struct {
		proto    iampb.DeletionStatus
		expected iam.DeletionStatus
	}{
		{iampb.DeletionStatus_DELETION_STATUS_PENDING, iam.DeletionStatusPending},
		{iampb.DeletionStatus_DELETION_STATUS_PROCESSING, iam.DeletionStatusProcessing},
		{iampb.DeletionStatus_DELETION_STATUS_COMPLETED, iam.DeletionStatusCompleted},
		{iampb.DeletionStatus_DELETION_STATUS_FAILED, iam.DeletionStatusFailed},
		{iampb.DeletionStatus_DELETION_STATUS_CANCELLED, iam.DeletionStatusCancelled},
		{iampb.DeletionStatus_DELETION_STATUS_UNSPECIFIED, ""},
	}
	for _, tc := range tests {
		assert.Equal(t, tc.expected, convertProtoDeletionStatus(tc.proto))
	}
}

func TestConvertDeletionStatusToProto_AllTypes(t *testing.T) {
	tests := []struct {
		domain   iam.DeletionStatus
		expected iampb.DeletionStatus
	}{
		{iam.DeletionStatusPending, iampb.DeletionStatus_DELETION_STATUS_PENDING},
		{iam.DeletionStatusProcessing, iampb.DeletionStatus_DELETION_STATUS_PROCESSING},
		{iam.DeletionStatusCompleted, iampb.DeletionStatus_DELETION_STATUS_COMPLETED},
		{iam.DeletionStatusFailed, iampb.DeletionStatus_DELETION_STATUS_FAILED},
		{iam.DeletionStatusCancelled, iampb.DeletionStatus_DELETION_STATUS_CANCELLED},
		{"unknown", iampb.DeletionStatus_DELETION_STATUS_UNSPECIFIED},
	}
	for _, tc := range tests {
		assert.Equal(t, tc.expected, convertDeletionStatusToProto(tc.domain))
	}
}

// =============================================================================
// Data Deletion Request Conversions
// =============================================================================

func TestConvertDataDeletionRequestToProto_Nil(t *testing.T) {
	assert.Nil(t, convertDataDeletionRequestToProto(nil))
}

func TestConvertDataDeletionRequestToProto_Full(t *testing.T) {
	now := time.Now()
	grace := now.Add(7 * 24 * time.Hour)
	started := now
	completed := now.Add(time.Hour)
	cancelled := now.Add(30 * time.Minute)
	r := &iam.DataDeletionRequest{
		ID: "del-1", UserID: "u1", TenantID: "t1",
		RequestedBy: "admin", Reason: "user request",
		Status:           iam.DeletionStatusCompleted,
		DeleteCategories: []string{"profile"}, PreserveCategories: []string{"audit"},
		RequestedAt: now, CancelledBy: "",
		GracePeriodEndsAt: &grace, StartedAt: &started,
		CompletedAt: &completed, CancelledAt: &cancelled,
		DeletedDataSummary: &iam.DeletionSummary{
			ProfileDeleted: true, SessionsDeleted: 5,
			AdditionalData: map[string]int{"custom": 10},
		},
		Metadata: map[string]string{"k": "v"},
	}
	result := convertDataDeletionRequestToProto(r)
	assert.Equal(t, "del-1", result.Id)
	assert.NotNil(t, result.GracePeriodEndsAt)
	assert.NotNil(t, result.StartedAt)
	assert.NotNil(t, result.CompletedAt)
	assert.NotNil(t, result.CancelledAt)
	assert.NotNil(t, result.DeletedDataSummary)
	assert.True(t, result.DeletedDataSummary.ProfileDeleted)
}

func TestConvertProtoToDataDeletionRequest_Nil(t *testing.T) {
	assert.Nil(t, convertProtoToDataDeletionRequest(nil))
}

func TestConvertProtoToDataDeletionRequest_Full(t *testing.T) {
	now := time.Now()
	pb := &iampb.DataDeletionRequest{
		Id: "del-1", UserId: "u1", TenantId: "t1",
		RequestedBy: "admin", Reason: "test",
		Status:             iampb.DeletionStatus_DELETION_STATUS_COMPLETED,
		DeleteCategories:   []string{"profile"},
		PreserveCategories: []string{"audit"},
		RequestedAt:        timestamppb.New(now),
		GracePeriodEndsAt:  timestamppb.New(now.Add(7 * 24 * time.Hour)),
		StartedAt:          timestamppb.New(now),
		CompletedAt:        timestamppb.New(now.Add(time.Hour)),
		CancelledAt:        timestamppb.New(now),
		DeletedDataSummary: &iampb.DeletionSummary{
			ProfileDeleted: true, SessionsDeleted: 3,
			AdditionalData: map[string]int32{"custom": 5},
		},
	}
	result := convertProtoToDataDeletionRequest(pb)
	assert.Equal(t, "del-1", result.ID)
	assert.NotNil(t, result.GracePeriodEndsAt)
	assert.NotNil(t, result.StartedAt)
	assert.NotNil(t, result.CompletedAt)
	assert.NotNil(t, result.CancelledAt)
	assert.NotNil(t, result.DeletedDataSummary)
	assert.True(t, result.DeletedDataSummary.ProfileDeleted)
	assert.Equal(t, 5, result.DeletedDataSummary.AdditionalData["custom"])
}

// =============================================================================
// Deletion Summary Conversions
// =============================================================================

func TestConvertDeletionSummaryToProto_Nil(t *testing.T) {
	assert.Nil(t, convertDeletionSummaryToProto(nil))
}

func TestConvertProtoToDeletionSummary_Nil(t *testing.T) {
	assert.Nil(t, convertProtoToDeletionSummary(nil))
}

// =============================================================================
// User Data Export Conversions
// =============================================================================

func TestConvertUserDataExportToProto_Nil(t *testing.T) {
	assert.Nil(t, convertUserDataExportToProto(nil))
}

func TestConvertUserDataExportToProto_Full(t *testing.T) {
	now := time.Now()
	lastAuth := now.Add(-time.Hour)
	ended := now.Add(-30 * time.Minute)
	granted := now.Add(-2 * time.Hour)
	withdrawn := now.Add(-time.Hour)
	enrolled := now.Add(-24 * time.Hour)

	export := &iam.UserDataExport{
		ExportMetadata: iam.ExportMetadata{
			ExportID: "exp-1", UserID: "u1", TenantID: "t1",
			ExportedAt: now, Format: iam.DataExportFormatJSON,
			Categories: []string{"all"}, SchemaVersion: "1.0",
		},
		Profile: &iam.UserProfileExport{
			ID: "u1", Email: "test@example.com", Name: "Test",
			Created: now, Updated: now, LastAuth: &lastAuth,
			Attributes: map[string]string{"key": "val"},
		},
		Sessions: []*iam.SessionExport{
			{
				ID: "s1", StartedAt: now, ExpiresAt: now.Add(time.Hour),
				LastActive: now, IPAddress: "127.0.0.1", UserAgent: "test",
				AuthMethod: "password", EndedAt: &ended,
			},
		},
		Consents: []*iam.ConsentExport{
			{
				Purpose: iam.ConsentPurposeMarketing,
				Status:  iam.ConsentStatusGranted,
				Version: "v1", GrantedAt: &granted, WithdrawnAt: &withdrawn,
			},
		},
		Groups: []*iam.GroupMembershipExport{
			{GroupID: "g1", GroupName: "devs", JoinedAt: now},
		},
		Roles: []*iam.RoleAssignmentExport{
			{RoleID: "r1", RoleName: "admin", AssignedAt: now},
		},
		AuditLogs: []*iam.AuditLogExport{
			{Timestamp: now, Action: "login", Resource: "user:u1",
				IPAddress: "127.0.0.1", Success: true},
		},
		Credentials: []*iam.CredentialExport{
			{ID: "cred-1", Type: "webauthn", Name: "yubikey",
				CreatedAt: now, LastUsedAt: now,
				Attestation: "fido2", Transports: []string{"usb"}},
		},
		MFAInfo: &iam.MFAInfoExport{
			Enabled: true, Method: "totp", BackupCodes: 8,
			EnrolledAt: &enrolled,
		},
	}

	result := convertUserDataExportToProto(export)
	assert.NotNil(t, result)
	assert.NotNil(t, result.ExportMetadata)
	assert.NotNil(t, result.Profile)
	assert.Len(t, result.Sessions, 1)
	assert.Len(t, result.Consents, 1)
	assert.Len(t, result.Groups, 1)
	assert.Len(t, result.Roles, 1)
	assert.Len(t, result.AuditLogs, 1)
	assert.Len(t, result.Credentials, 1)
	assert.NotNil(t, result.MfaInfo)
	assert.True(t, result.MfaInfo.Enabled)
}

// =============================================================================
// Helper Function Tests
// =============================================================================

func TestConvertProtoTimestamp_Nil(t *testing.T) {
	result := convertProtoTimestamp(nil)
	assert.True(t, result.IsZero())
}

func TestConvertProtoTimestamp_Valid(t *testing.T) {
	now := time.Now()
	ts := timestamppb.New(now)
	result := convertProtoTimestamp(ts)
	assert.False(t, result.IsZero())
}

func TestConvertTimeToProto_Zero(t *testing.T) {
	result := convertTimeToProto(time.Time{})
	assert.Nil(t, result)
}

func TestConvertTimeToProto_Valid(t *testing.T) {
	now := time.Now()
	result := convertTimeToProto(now)
	assert.NotNil(t, result)
}

// =============================================================================
// PEP convertPolicyUpdateType Tests
// =============================================================================

func TestConvertPolicyUpdateType_AllTypes(t *testing.T) {
	assert.Equal(t, iampb.PolicyUpdate_SNAPSHOT, convertPolicyUpdateType(PolicyUpdateSnapshot))
	assert.Equal(t, iampb.PolicyUpdate_CREATED, convertPolicyUpdateType(PolicyUpdateCreated))
	assert.Equal(t, iampb.PolicyUpdate_UPDATED, convertPolicyUpdateType(PolicyUpdateUpdated))
	assert.Equal(t, iampb.PolicyUpdate_DELETED, convertPolicyUpdateType(PolicyUpdateDeleted))
	// Default case
	assert.Equal(t, iampb.PolicyUpdate_SNAPSHOT, convertPolicyUpdateType(PolicyUpdateType(99)))
}

// =============================================================================
// PEP convertPEPError Tests
// =============================================================================

func TestConvertPEPError_Nil(t *testing.T) {
	assert.Nil(t, convertPEPError(nil))
}

func TestConvertPEPError_NonQBError(t *testing.T) {
	err := convertPEPError(assert.AnError)
	assert.Equal(t, codes.Internal, status.Code(err))
}

func TestConvertPEPError_QBError_AllCodes(t *testing.T) {
	tests := []struct {
		code     dberrors.ErrorCode
		expected codes.Code
	}{
		{dberrors.ErrNotFound, codes.NotFound},
		{dberrors.ErrAlreadyExists, codes.AlreadyExists},
		{dberrors.ErrInvalidInput, codes.InvalidArgument},
		{dberrors.ErrValidation, codes.InvalidArgument},
		{dberrors.ErrUnauthorized, codes.Unauthenticated},
		{dberrors.ErrForbidden, codes.PermissionDenied},
		{dberrors.ErrTimeout, codes.DeadlineExceeded},
		{dberrors.ErrDeadlineExceeded, codes.DeadlineExceeded},
		{dberrors.ErrResourceExhausted, codes.ResourceExhausted},
		{dberrors.ErrUnavailable, codes.Unavailable},
		{dberrors.ErrConflict, codes.FailedPrecondition},
		{dberrors.ErrUnimplemented, codes.Unimplemented},
		{dberrors.ErrNotImplemented, codes.Unimplemented},
		{dberrors.ErrCanceled, codes.Canceled},
		{dberrors.ErrInternal, codes.Internal},
	}
	for _, tc := range tests {
		qbErr := &dberrors.QBError{
			Code: tc.code,
			Op:   "test",
		}
		err := convertPEPError(qbErr)
		assert.Equal(t, tc.expected, status.Code(err), "code %v should map to %v", tc.code, tc.expected)
	}
}

// --- Merged from converters_iam_extended_test.go ---

// =============================================================================
// Admin Assignment Conversion Tests
// =============================================================================

func TestConvertAdminAssignmentToProto(t *testing.T) {
	now := time.Now()
	expires := now.Add(24 * time.Hour)
	lastUsed := now.Add(-1 * time.Hour)

	tests := []struct {
		name     string
		input    *iam.AdminAssignment
		validate func(t *testing.T, result *iampb.AdminAssignment)
	}{
		{
			name:  "nil_input",
			input: nil,
			validate: func(t *testing.T, result *iampb.AdminAssignment) {
				assert.Nil(t, result)
			},
		},
		{
			name: "full_assignment",
			input: &iam.AdminAssignment{
				ID:        "assign-1",
				UserID:    "user-1",
				AdminRole: iam.AdminRoleSuperAdmin,
				TenantIDs: []string{"tenant-1", "tenant-2"},
				Permissions: []iam.AdminPermission{
					iam.AdminPermission("user.create"),
					iam.AdminPermission("user.delete"),
				},
				DeniedPermissions: []iam.AdminPermission{
					iam.AdminPermission("tenant.delete"),
				},
				ResourceScopes: []iam.ResourceScope{
					{ResourceType: "user", ResourcePattern: "*"},
				},
				GrantedBy:  "admin-1",
				GrantedAt:  now,
				ExpiresAt:  &expires,
				LastUsedAt: &lastUsed,
				Active:     true,
				Metadata:   map[string]string{"reason": "promotion"},
			},
			validate: func(t *testing.T, result *iampb.AdminAssignment) {
				require.NotNil(t, result)
				assert.Equal(t, "assign-1", result.Id)
				assert.Equal(t, "user-1", result.UserId)
				assert.Equal(t, iampb.AdminRole_ADMIN_ROLE_SUPER_ADMIN, result.AdminRole)
				assert.Equal(t, []string{"tenant-1", "tenant-2"}, result.TenantIds)
				assert.Len(t, result.Permissions, 2)
				assert.Len(t, result.DeniedPermissions, 1)
				assert.Len(t, result.ResourceScopes, 1)
				assert.Equal(t, "admin-1", result.GrantedBy)
				assert.True(t, result.Active)
				assert.NotNil(t, result.ExpiresAt)
				assert.NotNil(t, result.LastUsedAt)
				assert.Equal(t, "promotion", result.Metadata["reason"])
			},
		},
		{
			name: "minimal_assignment",
			input: &iam.AdminAssignment{
				ID:        "assign-2",
				UserID:    "user-2",
				AdminRole: iam.AdminRoleReadOnly,
				GrantedAt: now,
			},
			validate: func(t *testing.T, result *iampb.AdminAssignment) {
				require.NotNil(t, result)
				assert.Equal(t, "assign-2", result.Id)
				assert.Nil(t, result.ExpiresAt)
				assert.Nil(t, result.LastUsedAt)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertAdminAssignmentToProto(tt.input)
			tt.validate(t, result)
		})
	}
}

func TestConvertProtoToAdminAssignment(t *testing.T) {
	now := time.Now()
	expires := now.Add(24 * time.Hour)
	lastUsed := now.Add(-1 * time.Hour)

	tests := []struct {
		name     string
		input    *iampb.AdminAssignment
		validate func(t *testing.T, result *iam.AdminAssignment)
	}{
		{
			name:  "nil_input",
			input: nil,
			validate: func(t *testing.T, result *iam.AdminAssignment) {
				assert.Nil(t, result)
			},
		},
		{
			name: "full_assignment",
			input: &iampb.AdminAssignment{
				Id:        "assign-1",
				UserId:    "user-1",
				AdminRole: iampb.AdminRole_ADMIN_ROLE_SUPER_ADMIN,
				TenantIds: []string{"tenant-1", "tenant-2"},
				Permissions: []string{
					"user.create",
					"user.delete",
				},
				DeniedPermissions: []string{
					"tenant.delete",
				},
				ResourceScopes: []*iampb.ResourceScope{
					{ResourceType: "user", ResourcePattern: "*"},
				},
				GrantedBy:  "admin-1",
				GrantedAt:  timestamppb.New(now),
				ExpiresAt:  timestamppb.New(expires),
				LastUsedAt: timestamppb.New(lastUsed),
				Active:     true,
				Metadata:   map[string]string{"reason": "promotion"},
			},
			validate: func(t *testing.T, result *iam.AdminAssignment) {
				require.NotNil(t, result)
				assert.Equal(t, "assign-1", result.ID)
				assert.Equal(t, "user-1", result.UserID)
				assert.Equal(t, iam.AdminRoleSuperAdmin, result.AdminRole)
				assert.Equal(t, []string{"tenant-1", "tenant-2"}, result.TenantIDs)
				assert.Len(t, result.Permissions, 2)
				assert.Len(t, result.DeniedPermissions, 1)
				assert.Len(t, result.ResourceScopes, 1)
				assert.Equal(t, "admin-1", result.GrantedBy)
				assert.True(t, result.Active)
				require.NotNil(t, result.ExpiresAt)
				require.NotNil(t, result.LastUsedAt)
				assert.Equal(t, "promotion", result.Metadata["reason"])
			},
		},
		{
			name: "minimal_assignment",
			input: &iampb.AdminAssignment{
				Id:        "assign-2",
				UserId:    "user-2",
				AdminRole: iampb.AdminRole_ADMIN_ROLE_READ_ONLY,
			},
			validate: func(t *testing.T, result *iam.AdminAssignment) {
				require.NotNil(t, result)
				assert.Equal(t, "assign-2", result.ID)
				assert.Nil(t, result.ExpiresAt)
				assert.Nil(t, result.LastUsedAt)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertProtoToAdminAssignment(tt.input)
			tt.validate(t, result)
		})
	}
}

// =============================================================================
// Impersonation Session Conversion Tests
// =============================================================================

func TestConvertImpersonationSessionToProto(t *testing.T) {
	now := time.Now()
	expires := now.Add(1 * time.Hour)
	ended := now.Add(30 * time.Minute)

	tests := []struct {
		name     string
		input    *iam.ImpersonationSession
		validate func(t *testing.T, result *iampb.ImpersonationSession)
	}{
		{
			name:  "nil_input",
			input: nil,
			validate: func(t *testing.T, result *iampb.ImpersonationSession) {
				assert.Nil(t, result)
			},
		},
		{
			name: "active_session",
			input: &iam.ImpersonationSession{
				ID:           "session-1",
				AdminUserID:  "admin-1",
				TargetUserID: "user-1",
				TenantID:     "tenant-1",
				Reason:       "Customer support request",
				StartedAt:    now,
				ExpiresAt:    expires,
				AuditLogID:   "audit-123",
			},
			validate: func(t *testing.T, result *iampb.ImpersonationSession) {
				require.NotNil(t, result)
				assert.Equal(t, "session-1", result.Id)
				assert.Equal(t, "admin-1", result.AdminUserId)
				assert.Equal(t, "user-1", result.TargetUserId)
				assert.Equal(t, "tenant-1", result.TenantId)
				assert.Equal(t, "Customer support request", result.Reason)
				assert.NotNil(t, result.StartedAt)
				assert.NotNil(t, result.ExpiresAt)
				assert.Nil(t, result.EndedAt)
				assert.Equal(t, "audit-123", result.AuditLogId)
			},
		},
		{
			name: "ended_session",
			input: &iam.ImpersonationSession{
				ID:           "session-2",
				AdminUserID:  "admin-1",
				TargetUserID: "user-2",
				TenantID:     "tenant-1",
				StartedAt:    now,
				ExpiresAt:    expires,
				EndedAt:      &ended,
			},
			validate: func(t *testing.T, result *iampb.ImpersonationSession) {
				require.NotNil(t, result)
				assert.NotNil(t, result.EndedAt)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertImpersonationSessionToProto(tt.input)
			tt.validate(t, result)
		})
	}
}

func TestConvertProtoToImpersonationSession(t *testing.T) {
	now := time.Now()
	expires := now.Add(1 * time.Hour)
	ended := now.Add(30 * time.Minute)

	tests := []struct {
		name     string
		input    *iampb.ImpersonationSession
		validate func(t *testing.T, result *iam.ImpersonationSession)
	}{
		{
			name:  "nil_input",
			input: nil,
			validate: func(t *testing.T, result *iam.ImpersonationSession) {
				assert.Nil(t, result)
			},
		},
		{
			name: "active_session",
			input: &iampb.ImpersonationSession{
				Id:           "session-1",
				AdminUserId:  "admin-1",
				TargetUserId: "user-1",
				TenantId:     "tenant-1",
				Reason:       "Customer support request",
				StartedAt:    timestamppb.New(now),
				ExpiresAt:    timestamppb.New(expires),
				AuditLogId:   "audit-123",
			},
			validate: func(t *testing.T, result *iam.ImpersonationSession) {
				require.NotNil(t, result)
				assert.Equal(t, "session-1", result.ID)
				assert.Equal(t, "admin-1", result.AdminUserID)
				assert.Equal(t, "user-1", result.TargetUserID)
				assert.Equal(t, "tenant-1", result.TenantID)
				assert.Equal(t, "Customer support request", result.Reason)
				assert.Nil(t, result.EndedAt)
				assert.Equal(t, "audit-123", result.AuditLogID)
			},
		},
		{
			name: "ended_session",
			input: &iampb.ImpersonationSession{
				Id:           "session-2",
				AdminUserId:  "admin-1",
				TargetUserId: "user-2",
				TenantId:     "tenant-1",
				StartedAt:    timestamppb.New(now),
				ExpiresAt:    timestamppb.New(expires),
				EndedAt:      timestamppb.New(ended),
			},
			validate: func(t *testing.T, result *iam.ImpersonationSession) {
				require.NotNil(t, result)
				require.NotNil(t, result.EndedAt)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertProtoToImpersonationSession(tt.input)
			tt.validate(t, result)
		})
	}
}

// =============================================================================
// Consent Purpose Conversion Tests
// =============================================================================

func TestConvertProtoConsentPurpose(t *testing.T) {
	tests := []struct {
		name     string
		input    iampb.ConsentPurpose
		expected iam.ConsentPurpose
	}{
		{"marketing", iampb.ConsentPurpose_CONSENT_PURPOSE_MARKETING, iam.ConsentPurposeMarketing},
		{"analytics", iampb.ConsentPurpose_CONSENT_PURPOSE_ANALYTICS, iam.ConsentPurposeAnalytics},
		{"personalization", iampb.ConsentPurpose_CONSENT_PURPOSE_PERSONALIZATION, iam.ConsentPurposePersonalization},
		{"third_party", iampb.ConsentPurpose_CONSENT_PURPOSE_THIRD_PARTY, iam.ConsentPurposeThirdParty},
		{"data_processing", iampb.ConsentPurpose_CONSENT_PURPOSE_DATA_PROCESSING, iam.ConsentPurposeDataProcessing},
		{"data_retention", iampb.ConsentPurpose_CONSENT_PURPOSE_DATA_RETENTION, iam.ConsentPurposeDataRetention},
		{"functional", iampb.ConsentPurpose_CONSENT_PURPOSE_FUNCTIONAL, iam.ConsentPurposeFunctional},
		{"unspecified", iampb.ConsentPurpose_CONSENT_PURPOSE_UNSPECIFIED, ""},
		{"unknown", iampb.ConsentPurpose(999), ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertProtoConsentPurpose(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConvertConsentPurposeToProto(t *testing.T) {
	tests := []struct {
		name     string
		input    iam.ConsentPurpose
		expected iampb.ConsentPurpose
	}{
		{"marketing", iam.ConsentPurposeMarketing, iampb.ConsentPurpose_CONSENT_PURPOSE_MARKETING},
		{"analytics", iam.ConsentPurposeAnalytics, iampb.ConsentPurpose_CONSENT_PURPOSE_ANALYTICS},
		{"personalization", iam.ConsentPurposePersonalization, iampb.ConsentPurpose_CONSENT_PURPOSE_PERSONALIZATION},
		{"third_party", iam.ConsentPurposeThirdParty, iampb.ConsentPurpose_CONSENT_PURPOSE_THIRD_PARTY},
		{"data_processing", iam.ConsentPurposeDataProcessing, iampb.ConsentPurpose_CONSENT_PURPOSE_DATA_PROCESSING},
		{"data_retention", iam.ConsentPurposeDataRetention, iampb.ConsentPurpose_CONSENT_PURPOSE_DATA_RETENTION},
		{"functional", iam.ConsentPurposeFunctional, iampb.ConsentPurpose_CONSENT_PURPOSE_FUNCTIONAL},
		{"unknown", iam.ConsentPurpose("unknown"), iampb.ConsentPurpose_CONSENT_PURPOSE_UNSPECIFIED},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertConsentPurposeToProto(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// =============================================================================
// Consent Status Conversion Tests
// =============================================================================

func TestConvertProtoConsentStatus(t *testing.T) {
	tests := []struct {
		name     string
		input    iampb.ConsentStatus
		expected iam.ConsentStatus
	}{
		{"granted", iampb.ConsentStatus_CONSENT_STATUS_GRANTED, iam.ConsentStatusGranted},
		{"denied", iampb.ConsentStatus_CONSENT_STATUS_DENIED, iam.ConsentStatusDenied},
		{"withdrawn", iampb.ConsentStatus_CONSENT_STATUS_WITHDRAWN, iam.ConsentStatusWithdrawn},
		{"pending", iampb.ConsentStatus_CONSENT_STATUS_PENDING, iam.ConsentStatusPending},
		{"unspecified", iampb.ConsentStatus_CONSENT_STATUS_UNSPECIFIED, ""},
		{"unknown", iampb.ConsentStatus(999), ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertProtoConsentStatus(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConvertConsentStatusToProto(t *testing.T) {
	tests := []struct {
		name     string
		input    iam.ConsentStatus
		expected iampb.ConsentStatus
	}{
		{"granted", iam.ConsentStatusGranted, iampb.ConsentStatus_CONSENT_STATUS_GRANTED},
		{"denied", iam.ConsentStatusDenied, iampb.ConsentStatus_CONSENT_STATUS_DENIED},
		{"withdrawn", iam.ConsentStatusWithdrawn, iampb.ConsentStatus_CONSENT_STATUS_WITHDRAWN},
		{"pending", iam.ConsentStatusPending, iampb.ConsentStatus_CONSENT_STATUS_PENDING},
		{"unknown", iam.ConsentStatus("unknown"), iampb.ConsentStatus_CONSENT_STATUS_UNSPECIFIED},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertConsentStatusToProto(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// =============================================================================
// Consent Conversion Tests
// =============================================================================

func TestConvertConsentToProto(t *testing.T) {
	now := time.Now()
	granted := now.Add(-1 * time.Hour)
	withdrawn := now.Add(-30 * time.Minute)
	expires := now.Add(24 * time.Hour)

	tests := []struct {
		name     string
		input    *iam.Consent
		validate func(t *testing.T, result *iampb.Consent)
	}{
		{
			name:  "nil_input",
			input: nil,
			validate: func(t *testing.T, result *iampb.Consent) {
				assert.Nil(t, result)
			},
		},
		{
			name: "full_consent",
			input: &iam.Consent{
				ID:          "consent-1",
				UserID:      "user-1",
				TenantID:    "tenant-1",
				Purpose:     iam.ConsentPurposeMarketing,
				Status:      iam.ConsentStatusGranted,
				Version:     "1.0",
				IPAddress:   "192.168.1.1",
				UserAgent:   "browser/1.0",
				Metadata:    map[string]string{"source": "web"},
				Created:     now,
				Updated:     now,
				GrantedAt:   &granted,
				WithdrawnAt: &withdrawn,
				ExpiresAt:   &expires,
			},
			validate: func(t *testing.T, result *iampb.Consent) {
				require.NotNil(t, result)
				assert.Equal(t, "consent-1", result.Id)
				assert.Equal(t, "user-1", result.UserId)
				assert.Equal(t, "tenant-1", result.TenantId)
				assert.Equal(t, iampb.ConsentPurpose_CONSENT_PURPOSE_MARKETING, result.Purpose)
				assert.Equal(t, iampb.ConsentStatus_CONSENT_STATUS_GRANTED, result.Status)
				assert.Equal(t, "1.0", result.Version)
				assert.NotNil(t, result.GrantedAt)
				assert.NotNil(t, result.WithdrawnAt)
				assert.NotNil(t, result.ExpiresAt)
			},
		},
		{
			name: "minimal_consent",
			input: &iam.Consent{
				ID:       "consent-2",
				UserID:   "user-2",
				TenantID: "tenant-1",
				Purpose:  iam.ConsentPurposeAnalytics,
				Status:   iam.ConsentStatusPending,
				Created:  now,
				Updated:  now,
			},
			validate: func(t *testing.T, result *iampb.Consent) {
				require.NotNil(t, result)
				assert.Nil(t, result.GrantedAt)
				assert.Nil(t, result.WithdrawnAt)
				assert.Nil(t, result.ExpiresAt)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertConsentToProto(tt.input)
			tt.validate(t, result)
		})
	}
}

func TestConvertProtoToConsent(t *testing.T) {
	now := time.Now()
	granted := now.Add(-1 * time.Hour)
	withdrawn := now.Add(-30 * time.Minute)
	expires := now.Add(24 * time.Hour)

	tests := []struct {
		name     string
		input    *iampb.Consent
		validate func(t *testing.T, result *iam.Consent)
	}{
		{
			name:  "nil_input",
			input: nil,
			validate: func(t *testing.T, result *iam.Consent) {
				assert.Nil(t, result)
			},
		},
		{
			name: "full_consent",
			input: &iampb.Consent{
				Id:          "consent-1",
				UserId:      "user-1",
				TenantId:    "tenant-1",
				Purpose:     iampb.ConsentPurpose_CONSENT_PURPOSE_MARKETING,
				Status:      iampb.ConsentStatus_CONSENT_STATUS_GRANTED,
				Version:     "1.0",
				IpAddress:   "192.168.1.1",
				UserAgent:   "browser/1.0",
				Metadata:    map[string]string{"source": "web"},
				CreatedAt:   timestamppb.New(now),
				UpdatedAt:   timestamppb.New(now),
				GrantedAt:   timestamppb.New(granted),
				WithdrawnAt: timestamppb.New(withdrawn),
				ExpiresAt:   timestamppb.New(expires),
			},
			validate: func(t *testing.T, result *iam.Consent) {
				require.NotNil(t, result)
				assert.Equal(t, "consent-1", result.ID)
				assert.Equal(t, "user-1", result.UserID)
				assert.Equal(t, "tenant-1", result.TenantID)
				assert.Equal(t, iam.ConsentPurposeMarketing, result.Purpose)
				assert.Equal(t, iam.ConsentStatusGranted, result.Status)
				assert.Equal(t, "1.0", result.Version)
				require.NotNil(t, result.GrantedAt)
				require.NotNil(t, result.WithdrawnAt)
				require.NotNil(t, result.ExpiresAt)
			},
		},
		{
			name: "minimal_consent",
			input: &iampb.Consent{
				Id:       "consent-2",
				UserId:   "user-2",
				TenantId: "tenant-1",
				Purpose:  iampb.ConsentPurpose_CONSENT_PURPOSE_ANALYTICS,
				Status:   iampb.ConsentStatus_CONSENT_STATUS_PENDING,
			},
			validate: func(t *testing.T, result *iam.Consent) {
				require.NotNil(t, result)
				assert.Nil(t, result.GrantedAt)
				assert.Nil(t, result.WithdrawnAt)
				assert.Nil(t, result.ExpiresAt)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertProtoToConsent(tt.input)
			tt.validate(t, result)
		})
	}
}

// =============================================================================
// Data Export Format Conversion Tests
// =============================================================================

func TestConvertProtoDataExportFormat(t *testing.T) {
	tests := []struct {
		name     string
		input    iampb.DataExportFormat
		expected iam.DataExportFormat
	}{
		{"json", iampb.DataExportFormat_DATA_EXPORT_FORMAT_JSON, iam.DataExportFormatJSON},
		{"csv", iampb.DataExportFormat_DATA_EXPORT_FORMAT_CSV, iam.DataExportFormatCSV},
		{"xml", iampb.DataExportFormat_DATA_EXPORT_FORMAT_XML, iam.DataExportFormatXML},
		{"unspecified", iampb.DataExportFormat_DATA_EXPORT_FORMAT_UNSPECIFIED, iam.DataExportFormatJSON},
		{"unknown", iampb.DataExportFormat(999), iam.DataExportFormatJSON},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertProtoDataExportFormat(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConvertDataExportFormatToProto(t *testing.T) {
	tests := []struct {
		name     string
		input    iam.DataExportFormat
		expected iampb.DataExportFormat
	}{
		{"json", iam.DataExportFormatJSON, iampb.DataExportFormat_DATA_EXPORT_FORMAT_JSON},
		{"csv", iam.DataExportFormatCSV, iampb.DataExportFormat_DATA_EXPORT_FORMAT_CSV},
		{"xml", iam.DataExportFormatXML, iampb.DataExportFormat_DATA_EXPORT_FORMAT_XML},
		{"unknown", iam.DataExportFormat("unknown"), iampb.DataExportFormat_DATA_EXPORT_FORMAT_UNSPECIFIED},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertDataExportFormatToProto(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// =============================================================================
// Data Export Status Conversion Tests
// =============================================================================

func TestConvertProtoDataExportStatus(t *testing.T) {
	tests := []struct {
		name     string
		input    iampb.DataExportStatus
		expected iam.DataExportStatus
	}{
		{"pending", iampb.DataExportStatus_DATA_EXPORT_STATUS_PENDING, iam.DataExportStatusPending},
		{"processing", iampb.DataExportStatus_DATA_EXPORT_STATUS_PROCESSING, iam.DataExportStatusProcessing},
		{"completed", iampb.DataExportStatus_DATA_EXPORT_STATUS_COMPLETED, iam.DataExportStatusCompleted},
		{"failed", iampb.DataExportStatus_DATA_EXPORT_STATUS_FAILED, iam.DataExportStatusFailed},
		{"expired", iampb.DataExportStatus_DATA_EXPORT_STATUS_EXPIRED, iam.DataExportStatusExpired},
		{"unspecified", iampb.DataExportStatus_DATA_EXPORT_STATUS_UNSPECIFIED, ""},
		{"unknown", iampb.DataExportStatus(999), ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertProtoDataExportStatus(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConvertDataExportStatusToProto(t *testing.T) {
	tests := []struct {
		name     string
		input    iam.DataExportStatus
		expected iampb.DataExportStatus
	}{
		{"pending", iam.DataExportStatusPending, iampb.DataExportStatus_DATA_EXPORT_STATUS_PENDING},
		{"processing", iam.DataExportStatusProcessing, iampb.DataExportStatus_DATA_EXPORT_STATUS_PROCESSING},
		{"completed", iam.DataExportStatusCompleted, iampb.DataExportStatus_DATA_EXPORT_STATUS_COMPLETED},
		{"failed", iam.DataExportStatusFailed, iampb.DataExportStatus_DATA_EXPORT_STATUS_FAILED},
		{"expired", iam.DataExportStatusExpired, iampb.DataExportStatus_DATA_EXPORT_STATUS_EXPIRED},
		{"unknown", iam.DataExportStatus("unknown"), iampb.DataExportStatus_DATA_EXPORT_STATUS_UNSPECIFIED},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertDataExportStatusToProto(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// --- Merged from coverage_extended_test.go ---

// =============================================================================
// Pool Options Tests - Cover All Option Functions
// =============================================================================

func TestWithPoolSizeInvalidMinSize(t *testing.T) {
	opts := defaultPoolOptions()
	opt := WithPoolSize(0, 10)

	err := opt(opts)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "minSize must be at least 1")
}

func TestWithPoolSizeMaxLessThanMin(t *testing.T) {
	opts := defaultPoolOptions()
	opt := WithPoolSize(10, 5)

	err := opt(opts)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "maxSize must be >= minSize")
}

func TestWithPoolSizeValid(t *testing.T) {
	opts := defaultPoolOptions()
	opt := WithPoolSize(5, 20)

	err := opt(opts)

	assert.NoError(t, err)
	assert.Equal(t, 5, opts.minSize)
	assert.Equal(t, 20, opts.maxSize)
}

func TestWithMaxConnectionAgeNegative(t *testing.T) {
	opts := defaultPoolOptions()
	opt := WithMaxConnectionAge(-1 * time.Second)

	err := opt(opts)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "duration must be positive")
}

func TestWithMaxConnectionAgeValid(t *testing.T) {
	opts := defaultPoolOptions()
	opt := WithMaxConnectionAge(10 * time.Minute)

	err := opt(opts)

	assert.NoError(t, err)
	assert.Equal(t, 10*time.Minute, opts.maxAge)
}

func TestWithMaxRequestsPerConnZero(t *testing.T) {
	opts := defaultPoolOptions()
	opt := WithMaxRequestsPerConn(0)

	err := opt(opts)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "count must be positive")
}

func TestWithMaxRequestsPerConnValid(t *testing.T) {
	opts := defaultPoolOptions()
	opt := WithMaxRequestsPerConn(50000)

	err := opt(opts)

	assert.NoError(t, err)
	assert.Equal(t, uint64(50000), opts.maxRequests)
}

func TestWithHealthCheckIntervalNegative(t *testing.T) {
	opts := defaultPoolOptions()
	opt := WithHealthCheckInterval(-1 * time.Second)

	err := opt(opts)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "duration cannot be negative")
}

func TestWithHealthCheckIntervalZero(t *testing.T) {
	opts := defaultPoolOptions()
	opt := WithHealthCheckInterval(0)

	err := opt(opts)

	assert.NoError(t, err)
	assert.Equal(t, time.Duration(0), opts.healthCheck)
}

func TestWithHealthCheckIntervalValid(t *testing.T) {
	opts := defaultPoolOptions()
	opt := WithHealthCheckInterval(1 * time.Minute)

	err := opt(opts)

	assert.NoError(t, err)
	assert.Equal(t, 1*time.Minute, opts.healthCheck)
}

func TestWithConnectionWarmupEnabled(t *testing.T) {
	opts := defaultPoolOptions()
	opt := WithConnectionWarmup(true)

	err := opt(opts)

	assert.NoError(t, err)
	assert.True(t, opts.warmupConns)
}

func TestWithConnectionWarmupDisabled(t *testing.T) {
	opts := defaultPoolOptions()
	opt := WithConnectionWarmup(false)

	err := opt(opts)

	assert.NoError(t, err)
	assert.False(t, opts.warmupConns)
}

func TestWithPoolClientOptionsNil(t *testing.T) {
	opts := defaultPoolOptions()
	opt := WithPoolClientOptions(nil)

	err := opt(opts)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "clientOpts cannot be nil")
}

func TestWithPoolClientOptionsValid(t *testing.T) {
	opts := defaultPoolOptions()
	clientOpts := defaultClientOptions()
	opt := WithPoolClientOptions(clientOpts)

	err := opt(opts)

	assert.NoError(t, err)
	assert.Equal(t, clientOpts, opts.clientOpts)
}

// =============================================================================
// Pool Connection Tests - needsRecycling Edge Cases
// =============================================================================

func TestNeedsRecyclingUnhealthyConnection(t *testing.T) {
	pool := &ConnectionPool{
		maxAge:      30 * time.Minute,
		maxRequests: 100000,
		metrics:     newPoolMetrics("localhost:50051"),
	}

	conn := &pooledConn{
		created: time.Now(),
	}
	conn.healthy.Store(false)
	conn.requests.Store(10)

	// Unhealthy connection should need recycling
	assert.True(t, pool.needsRecycling(conn))
}

func TestNeedsRecyclingMaxAgeExceeded(t *testing.T) {
	pool := &ConnectionPool{
		maxAge:      1 * time.Millisecond,
		maxRequests: 100000,
		metrics:     newPoolMetrics("localhost:50051"),
	}

	conn := &pooledConn{
		created: time.Now().Add(-1 * time.Hour), // Created an hour ago
	}
	conn.healthy.Store(true)
	conn.requests.Store(10)

	// Old connection should need recycling
	assert.True(t, pool.needsRecycling(conn))
}

func TestNeedsRecyclingMaxRequestsExceeded(t *testing.T) {
	pool := &ConnectionPool{
		maxAge:      30 * time.Minute,
		maxRequests: 100,
		metrics:     newPoolMetrics("localhost:50051"),
	}

	conn := &pooledConn{
		created: time.Now(),
	}
	conn.healthy.Store(true)
	conn.requests.Store(101) // More than maxRequests

	// High-usage connection should need recycling
	assert.True(t, pool.needsRecycling(conn))
}

// =============================================================================
// Client AddMetadata Tests
// =============================================================================

func TestClientAddMetadataEmptyOpts(t *testing.T) {
	client := &Client{
		opts: &clientOptions{
			metadata: nil,
		},
	}

	ctx := context.Background()
	result := client.addMetadata(ctx)

	// Should return same context when no metadata
	assert.Equal(t, ctx, result)
}

func TestClientAddMetadataWithDataOpts(t *testing.T) {
	client := &Client{
		opts: &clientOptions{
			metadata: map[string]string{
				"key1": "value1",
				"key2": "value2",
			},
		},
	}

	ctx := context.Background()
	result := client.addMetadata(ctx)

	// Should have metadata in context
	md, ok := metadata.FromOutgoingContext(result)
	assert.True(t, ok)
	assert.Equal(t, []string{"value1"}, md.Get("key1"))
	assert.Equal(t, []string{"value2"}, md.Get("key2"))
}

func TestClientAddMetadataReusesBuiltMDOpts(t *testing.T) {
	// Create pre-built metadata
	prebuiltMD := metadata.New(map[string]string{"prebuilt": "value"})

	client := &Client{
		opts: &clientOptions{
			metadata: map[string]string{"key": "value"},
			md:       prebuiltMD,
		},
	}

	ctx := context.Background()
	result := client.addMetadata(ctx)

	// Should use pre-built metadata
	md, ok := metadata.FromOutgoingContext(result)
	assert.True(t, ok)
	assert.Equal(t, []string{"value"}, md.Get("prebuilt"))
}

// =============================================================================
// Client isConnected Tests
// =============================================================================

func TestClientIsConnectedTrueState(t *testing.T) {
	client := &Client{
		connected: true,
		closed:    false,
	}

	assert.True(t, client.isConnected())
}

func TestClientIsConnectedFalseNotConnectedState(t *testing.T) {
	client := &Client{
		connected: false,
		closed:    false,
	}

	assert.False(t, client.isConnected())
}

func TestClientIsConnectedFalseClosedState(t *testing.T) {
	client := &Client{
		connected: true,
		closed:    true,
	}

	assert.False(t, client.isConnected())
}

// =============================================================================
// Client Disconnect Tests
// =============================================================================

func TestClientDisconnectWithPoolMode(t *testing.T) {
	pool := &ConnectionPool{
		metrics:         newPoolMetrics("localhost:50051"),
		stopHealthCheck: make(chan struct{}),
	}

	client := &Client{
		pool: pool,
	}

	err := client.Disconnect()

	// Should be no-op with pool
	assert.NoError(t, err)
}

func TestClientDisconnectNotConnectedState(t *testing.T) {
	client := &Client{
		connected: false,
		conn:      nil,
	}

	err := client.Disconnect()

	// Should be no-op when not connected
	assert.NoError(t, err)
}

// =============================================================================
// Client Close Tests
// =============================================================================

func TestClientCloseAlreadyClosedState(t *testing.T) {
	client := &Client{
		closed: true,
	}

	err := client.Close()

	// Should be no-op when already closed
	assert.NoError(t, err)
}

// =============================================================================
// Peer Credentials Tests - Additional Coverage
// =============================================================================

func TestPeerCredentialsIsRootCheck(t *testing.T) {
	creds := &PeerCredentials{
		PID: 1234,
		UID: 0,
		GID: 0,
	}

	assert.True(t, creds.IsRoot())

	creds.UID = 1000
	assert.False(t, creds.IsRoot())
}

func TestPeerCredentialsStringFormat(t *testing.T) {
	creds := &PeerCredentials{
		PID: 1234,
		UID: 1000,
		GID: 1000,
	}

	str := creds.String()

	assert.Contains(t, str, "PID: 1234")
	assert.Contains(t, str, "UID: 1000")
	assert.Contains(t, str, "GID: 1000")
}

func TestPeerCredentialsAuthInfoAuthTypeValue(t *testing.T) {
	authInfo := &PeerCredentialsAuthInfo{
		Credentials: &PeerCredentials{PID: 1, UID: 0, GID: 0},
	}

	assert.Equal(t, "unix_peercred", authInfo.AuthType())
}

func TestPeerCredentialsUnavailableErrorMsg(t *testing.T) {
	err := &PeerCredentialsUnavailableError{
		Cause: nil,
	}

	assert.Equal(t, "peer credentials unavailable", err.Error())
	assert.Nil(t, err.Unwrap())

	innerErr := assert.AnError
	err.Cause = innerErr
	assert.Contains(t, err.Error(), innerErr.Error())
	assert.Equal(t, innerErr, err.Unwrap())
}

func TestPeerCredentialsVerificationErrorMsg(t *testing.T) {
	err := &PeerCredentialsVerificationError{
		Reason: "test reason",
	}

	assert.Contains(t, err.Error(), "test reason")
}

func TestNewUnixTransportCredentialsInstance(t *testing.T) {
	creds := NewUnixTransportCredentials()

	assert.NotNil(t, creds)

	info := creds.Info()
	assert.Equal(t, "unix_peercred", info.SecurityProtocol)
	assert.Equal(t, "1.0", info.SecurityVersion)

	clone := creds.Clone()
	assert.NotNil(t, clone)

	err := creds.OverrideServerName("test")
	assert.NoError(t, err)
}

func TestUnixTransportCredentialsClientHandshakeMethod(t *testing.T) {
	creds := &UnixTransportCredentials{}

	ctx := context.Background()
	conn, authInfo, err := creds.ClientHandshake(ctx, "authority", nil)

	assert.NoError(t, err)
	assert.Nil(t, conn)
	assert.Nil(t, authInfo)
}

func TestAllowAllVerifierPasses(t *testing.T) {
	verifier := &AllowAllVerifier{}

	creds := &PeerCredentials{PID: 1, UID: 1000, GID: 1000}
	err := verifier.Verify(creds, "/test.Method")

	assert.NoError(t, err)
}

func TestRootOnlyVerifierCheck(t *testing.T) {
	verifier := &RootOnlyVerifier{}

	// Root should pass
	rootCreds := &PeerCredentials{PID: 1, UID: 0, GID: 0}
	err := verifier.Verify(rootCreds, "/test.Method")
	assert.NoError(t, err)

	// Non-root should fail
	userCreds := &PeerCredentials{PID: 1, UID: 1000, GID: 1000}
	err = verifier.Verify(userCreds, "/test.Method")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "root privileges required")
}

func TestUIDVerifierCheck(t *testing.T) {
	verifier := NewUIDVerifier(1000, 1001)

	// Root should always pass
	rootCreds := &PeerCredentials{PID: 1, UID: 0, GID: 0}
	err := verifier.Verify(rootCreds, "/test.Method")
	assert.NoError(t, err)

	// Allowed UID should pass
	allowedCreds := &PeerCredentials{PID: 1, UID: 1000, GID: 1000}
	err = verifier.Verify(allowedCreds, "/test.Method")
	assert.NoError(t, err)

	// Disallowed UID should fail
	disallowedCreds := &PeerCredentials{PID: 1, UID: 9999, GID: 1000}
	err = verifier.Verify(disallowedCreds, "/test.Method")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "user not authorized")
}

func TestGIDVerifierCheck(t *testing.T) {
	verifier := NewGIDVerifier(1000, 1001)

	// Root should always pass
	rootCreds := &PeerCredentials{PID: 1, UID: 0, GID: 0}
	err := verifier.Verify(rootCreds, "/test.Method")
	assert.NoError(t, err)

	// Allowed GID should pass
	allowedCreds := &PeerCredentials{PID: 1, UID: 1000, GID: 1000}
	err = verifier.Verify(allowedCreds, "/test.Method")
	assert.NoError(t, err)

	// Disallowed GID should fail
	disallowedCreds := &PeerCredentials{PID: 1, UID: 1000, GID: 9999}
	err = verifier.Verify(disallowedCreds, "/test.Method")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "group not authorized")
}

func TestChainVerifierCheck(t *testing.T) {
	uidVerifier := NewUIDVerifier(1000)
	gidVerifier := NewGIDVerifier(1000)
	chainVerifier := NewChainVerifier(uidVerifier, gidVerifier)

	// Both conditions met should pass
	validCreds := &PeerCredentials{PID: 1, UID: 1000, GID: 1000}
	err := chainVerifier.Verify(validCreds, "/test.Method")
	assert.NoError(t, err)

	// First verifier fails
	invalidUID := &PeerCredentials{PID: 1, UID: 9999, GID: 1000}
	err = chainVerifier.Verify(invalidUID, "/test.Method")
	assert.Error(t, err)

	// Second verifier fails
	invalidGID := &PeerCredentials{PID: 1, UID: 1000, GID: 9999}
	err = chainVerifier.Verify(invalidGID, "/test.Method")
	assert.Error(t, err)
}

func TestContextWithPeerCredentialsStorage(t *testing.T) {
	ctx := context.Background()
	creds := &PeerCredentials{PID: 1, UID: 1000, GID: 1000}

	newCtx := ContextWithPeerCredentials(ctx, creds)

	extractedCreds := PeerCredentialsFromContext(newCtx)
	assert.NotNil(t, extractedCreds)
	assert.Equal(t, creds.PID, extractedCreds.PID)
	assert.Equal(t, creds.UID, extractedCreds.UID)
	assert.Equal(t, creds.GID, extractedCreds.GID)
}

func TestPeerCredentialsFromContextNoCredsAvailable(t *testing.T) {
	ctx := context.Background()

	creds := PeerCredentialsFromContext(ctx)

	assert.Nil(t, creds)
}

// =============================================================================
// IAM Converter Tests - GDPR Export Functions
// =============================================================================

func TestConvertDataExportRequestToProtoNilInput(t *testing.T) {
	result := convertDataExportRequestToProto(nil)
	assert.Nil(t, result)
}

func TestConvertDataExportRequestToProtoComplete(t *testing.T) {
	now := time.Now()
	startedAt := now.Add(-1 * time.Hour)
	completedAt := now.Add(-30 * time.Minute)
	expiresAt := now.Add(24 * time.Hour)

	req := &iam.DataExportRequest{
		ID:                "export-1",
		UserID:            "user-1",
		TenantID:          "tenant-1",
		RequestedBy:       "admin",
		Format:            iam.DataExportFormatJSON,
		Status:            iam.DataExportStatusCompleted,
		IncludeCategories: []string{"profile", "sessions"},
		ExcludeCategories: []string{"audit"},
		RequestedAt:       now,
		StartedAt:         &startedAt,
		CompletedAt:       &completedAt,
		ExpiresAt:         &expiresAt,
		DownloadURL:       "https://example.com/export.zip",
		FileSizeBytes:     1024,
		ErrorMessage:      "",
		Metadata:          map[string]string{"key": "value"},
	}

	result := convertDataExportRequestToProto(req)

	assert.NotNil(t, result)
	assert.Equal(t, "export-1", result.Id)
	assert.Equal(t, "user-1", result.UserId)
	assert.Equal(t, "tenant-1", result.TenantId)
	assert.Equal(t, iampb.DataExportFormat_DATA_EXPORT_FORMAT_JSON, result.Format)
	assert.Equal(t, iampb.DataExportStatus_DATA_EXPORT_STATUS_COMPLETED, result.Status)
	assert.NotNil(t, result.StartedAt)
	assert.NotNil(t, result.CompletedAt)
	assert.NotNil(t, result.ExpiresAt)
}

func TestConvertProtoToDataExportRequestNilInput(t *testing.T) {
	result := convertProtoToDataExportRequest(nil)
	assert.Nil(t, result)
}

func TestConvertProtoToDataExportRequestComplete(t *testing.T) {
	now := time.Now()

	pb := &iampb.DataExportRequest{
		Id:                "export-1",
		UserId:            "user-1",
		TenantId:          "tenant-1",
		RequestedBy:       "admin",
		Format:            iampb.DataExportFormat_DATA_EXPORT_FORMAT_CSV,
		Status:            iampb.DataExportStatus_DATA_EXPORT_STATUS_PROCESSING,
		IncludeCategories: []string{"profile"},
		ExcludeCategories: []string{"audit"},
		RequestedAt:       timestamppb.New(now),
		StartedAt:         timestamppb.New(now.Add(-1 * time.Hour)),
		CompletedAt:       timestamppb.New(now.Add(-30 * time.Minute)),
		ExpiresAt:         timestamppb.New(now.Add(24 * time.Hour)),
		DownloadUrl:       "https://example.com/export.csv",
		FileSizeBytes:     2048,
		ErrorMessage:      "",
		Metadata:          map[string]string{"key": "value"},
	}

	result := convertProtoToDataExportRequest(pb)

	assert.NotNil(t, result)
	assert.Equal(t, "export-1", result.ID)
	assert.Equal(t, iam.DataExportFormatCSV, result.Format)
	assert.Equal(t, iam.DataExportStatusProcessing, result.Status)
	assert.NotNil(t, result.StartedAt)
	assert.NotNil(t, result.CompletedAt)
	assert.NotNil(t, result.ExpiresAt)
}

func TestConvertUserDataExportToProtoNilInput(t *testing.T) {
	result := convertUserDataExportToProto(nil)
	assert.Nil(t, result)
}

func TestConvertUserDataExportToProtoComplete(t *testing.T) {
	now := time.Now()
	lastAuth := now.Add(-1 * time.Hour)
	endedAt := now.Add(-30 * time.Minute)
	grantedAt := now.Add(-24 * time.Hour)
	withdrawnAt := now.Add(-12 * time.Hour)
	enrolledAt := now.Add(-7 * 24 * time.Hour)

	export := &iam.UserDataExport{
		ExportMetadata: iam.ExportMetadata{
			ExportID:      "export-1",
			UserID:        "user-1",
			TenantID:      "tenant-1",
			ExportedAt:    now,
			Format:        iam.DataExportFormatJSON,
			Categories:    []string{"profile", "sessions"},
			SchemaVersion: "1.0",
		},
		Profile: &iam.UserProfileExport{
			ID:       "user-1",
			Email:    "test@example.com",
			Name:     "Test User",
			Created:  now.Add(-30 * 24 * time.Hour),
			Updated:  now,
			LastAuth: &lastAuth,
			Attributes: map[string]string{
				"department": "Engineering",
				"level":      "Senior",
			},
		},
		Sessions: []*iam.SessionExport{
			{
				ID:         "session-1",
				StartedAt:  now.Add(-2 * time.Hour),
				ExpiresAt:  now.Add(22 * time.Hour),
				LastActive: now,
				EndedAt:    &endedAt,
				IPAddress:  "192.168.1.1",
				UserAgent:  "Mozilla/5.0",
				AuthMethod: "password",
			},
		},
		Consents: []*iam.ConsentExport{
			{
				Purpose:     iam.ConsentPurposeMarketing,
				Status:      iam.ConsentStatusGranted,
				Version:     "1.0",
				GrantedAt:   &grantedAt,
				WithdrawnAt: &withdrawnAt,
			},
		},
		Groups: []*iam.GroupMembershipExport{
			{
				GroupID:   "group-1",
				GroupName: "Engineers",
				JoinedAt:  now.Add(-15 * 24 * time.Hour),
			},
		},
		Roles: []*iam.RoleAssignmentExport{
			{
				RoleID:     "role-1",
				RoleName:   "Developer",
				AssignedAt: now.Add(-10 * 24 * time.Hour),
			},
		},
		AuditLogs: []*iam.AuditLogExport{
			{
				Timestamp: now.Add(-1 * time.Hour),
				Action:    "login",
				Resource:  "auth",
				IPAddress: "192.168.1.1",
				Success:   true,
			},
		},
		Credentials: []*iam.CredentialExport{
			{
				ID:         "cred-1",
				Type:       "password",
				Name:       "Primary",
				CreatedAt:  now.Add(-30 * 24 * time.Hour),
				LastUsedAt: now.Add(-5 * 24 * time.Hour),
			},
		},
		MFAInfo: &iam.MFAInfoExport{
			Enabled:     true,
			Method:      "totp",
			EnrolledAt:  &enrolledAt,
			BackupCodes: 5,
		},
	}

	result := convertUserDataExportToProto(export)

	assert.NotNil(t, result)
	assert.NotNil(t, result.ExportMetadata)
	assert.NotNil(t, result.Profile)
	assert.Len(t, result.Sessions, 1)
	assert.Len(t, result.Consents, 1)
	assert.Len(t, result.Groups, 1)
	assert.Len(t, result.Roles, 1)
	assert.Len(t, result.AuditLogs, 1)
	assert.Len(t, result.Credentials, 1)
	assert.NotNil(t, result.MfaInfo)
}

func TestConvertExportMetadataToProtoNilInput(t *testing.T) {
	result := convertExportMetadataToProto(nil)
	assert.Nil(t, result)
}

func TestConvertUserProfileExportToProtoNilInput(t *testing.T) {
	result := convertUserProfileExportToProto(nil)
	assert.Nil(t, result)
}

func TestConvertSessionExportToProtoNilInput(t *testing.T) {
	result := convertSessionExportToProto(nil)
	assert.Nil(t, result)
}

func TestConvertConsentExportToProtoNilInput(t *testing.T) {
	result := convertConsentExportToProto(nil)
	assert.Nil(t, result)
}

func TestConvertGroupMembershipExportToProtoNilInput(t *testing.T) {
	result := convertGroupMembershipExportToProto(nil)
	assert.Nil(t, result)
}

func TestConvertRoleAssignmentExportToProtoNilInput(t *testing.T) {
	result := convertRoleAssignmentExportToProto(nil)
	assert.Nil(t, result)
}

func TestConvertAuditLogExportToProtoNilInput(t *testing.T) {
	result := convertAuditLogExportToProto(nil)
	assert.Nil(t, result)
}

func TestConvertCredentialExportToProtoNilInput(t *testing.T) {
	result := convertCredentialExportToProto(nil)
	assert.Nil(t, result)
}

func TestConvertMFAInfoExportToProtoNilInput(t *testing.T) {
	result := convertMFAInfoExportToProto(nil)
	assert.Nil(t, result)
}

// =============================================================================
// IAM Deletion Status Converter Tests
// =============================================================================

func TestConvertProtoDeletionStatusAllValues(t *testing.T) {
	tests := []struct {
		name     string
		input    iampb.DeletionStatus
		expected iam.DeletionStatus
	}{
		{"pending", iampb.DeletionStatus_DELETION_STATUS_PENDING, iam.DeletionStatusPending},
		{"processing", iampb.DeletionStatus_DELETION_STATUS_PROCESSING, iam.DeletionStatusProcessing},
		{"completed", iampb.DeletionStatus_DELETION_STATUS_COMPLETED, iam.DeletionStatusCompleted},
		{"failed", iampb.DeletionStatus_DELETION_STATUS_FAILED, iam.DeletionStatusFailed},
		{"cancelled", iampb.DeletionStatus_DELETION_STATUS_CANCELLED, iam.DeletionStatusCancelled},
		{"unspecified", iampb.DeletionStatus_DELETION_STATUS_UNSPECIFIED, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertProtoDeletionStatus(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConvertDeletionStatusToProtoAllValues(t *testing.T) {
	tests := []struct {
		name     string
		input    iam.DeletionStatus
		expected iampb.DeletionStatus
	}{
		{"pending", iam.DeletionStatusPending, iampb.DeletionStatus_DELETION_STATUS_PENDING},
		{"processing", iam.DeletionStatusProcessing, iampb.DeletionStatus_DELETION_STATUS_PROCESSING},
		{"completed", iam.DeletionStatusCompleted, iampb.DeletionStatus_DELETION_STATUS_COMPLETED},
		{"failed", iam.DeletionStatusFailed, iampb.DeletionStatus_DELETION_STATUS_FAILED},
		{"cancelled", iam.DeletionStatusCancelled, iampb.DeletionStatus_DELETION_STATUS_CANCELLED},
		{"unknown", iam.DeletionStatus("unknown"), iampb.DeletionStatus_DELETION_STATUS_UNSPECIFIED},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertDeletionStatusToProto(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConvertDataDeletionRequestToProtoNilInput(t *testing.T) {
	result := convertDataDeletionRequestToProto(nil)
	assert.Nil(t, result)
}

func TestConvertDataDeletionRequestToProtoComplete(t *testing.T) {
	now := time.Now()
	gracePeriodEndsAt := now.Add(7 * 24 * time.Hour)
	startedAt := now.Add(8 * 24 * time.Hour)
	completedAt := now.Add(9 * 24 * time.Hour)
	cancelledAt := now.Add(10 * 24 * time.Hour)

	req := &iam.DataDeletionRequest{
		ID:                 "deletion-1",
		UserID:             "user-1",
		TenantID:           "tenant-1",
		RequestedBy:        "user-1",
		Status:             iam.DeletionStatusPending,
		Reason:             "User requested deletion",
		RequestedAt:        now,
		GracePeriodEndsAt:  &gracePeriodEndsAt,
		StartedAt:          &startedAt,
		CompletedAt:        &completedAt,
		CancelledAt:        &cancelledAt,
		CancelledBy:        "admin",
		DeleteCategories:   []string{"all"},
		PreserveCategories: []string{"audit"},
		ErrorMessage:       "",
		Metadata:           map[string]string{"source": "web"},
	}

	result := convertDataDeletionRequestToProto(req)

	assert.NotNil(t, result)
	assert.Equal(t, "deletion-1", result.Id)
	assert.Equal(t, iampb.DeletionStatus_DELETION_STATUS_PENDING, result.Status)
	assert.NotNil(t, result.GracePeriodEndsAt)
	assert.NotNil(t, result.StartedAt)
	assert.NotNil(t, result.CompletedAt)
	assert.NotNil(t, result.CancelledAt)
}

func TestConvertProtoToDataDeletionRequestNilInput(t *testing.T) {
	result := convertProtoToDataDeletionRequest(nil)
	assert.Nil(t, result)
}

func TestConvertProtoToDataDeletionRequestComplete(t *testing.T) {
	now := time.Now()

	pb := &iampb.DataDeletionRequest{
		Id:                 "deletion-1",
		UserId:             "user-1",
		TenantId:           "tenant-1",
		RequestedBy:        "user-1",
		Status:             iampb.DeletionStatus_DELETION_STATUS_COMPLETED,
		Reason:             "GDPR request",
		RequestedAt:        timestamppb.New(now),
		GracePeriodEndsAt:  timestamppb.New(now.Add(7 * 24 * time.Hour)),
		StartedAt:          timestamppb.New(now.Add(8 * 24 * time.Hour)),
		CompletedAt:        timestamppb.New(now.Add(9 * 24 * time.Hour)),
		CancelledAt:        timestamppb.New(now.Add(10 * 24 * time.Hour)),
		CancelledBy:        "admin",
		DeleteCategories:   []string{"profile"},
		PreserveCategories: []string{"audit"},
		ErrorMessage:       "",
		Metadata:           map[string]string{"approved_by": "admin"},
	}

	result := convertProtoToDataDeletionRequest(pb)

	assert.NotNil(t, result)
	assert.Equal(t, "deletion-1", result.ID)
	assert.Equal(t, iam.DeletionStatusCompleted, result.Status)
	assert.NotNil(t, result.GracePeriodEndsAt)
	assert.NotNil(t, result.StartedAt)
	assert.NotNil(t, result.CompletedAt)
	assert.NotNil(t, result.CancelledAt)
}

func TestConvertDeletionSummaryToProtoNilInput(t *testing.T) {
	result := convertDeletionSummaryToProto(nil)
	assert.Nil(t, result)
}

func TestConvertDeletionSummaryToProtoComplete(t *testing.T) {
	summary := &iam.DeletionSummary{
		ProfileDeleted:          true,
		SessionsDeleted:         10,
		AuditLogsAnonymized:     50,
		ConsentsDeleted:         5,
		CredentialsDeleted:      2,
		GroupMembershipsRemoved: 3,
		RoleAssignmentsRemoved:  4,
		TokensRevoked:           8,
		AdditionalData: map[string]int{
			"custom_data": 100,
		},
	}

	result := convertDeletionSummaryToProto(summary)

	assert.NotNil(t, result)
	assert.True(t, result.ProfileDeleted)
	assert.Equal(t, int32(10), result.SessionsDeleted)
	assert.Equal(t, int32(50), result.AuditLogsAnonymized)
	assert.Equal(t, int32(5), result.ConsentsDeleted)
	assert.Equal(t, int32(2), result.CredentialsDeleted)
	assert.Equal(t, int32(3), result.GroupMembershipsRemoved)
	assert.Equal(t, int32(4), result.RoleAssignmentsRemoved)
	assert.Equal(t, int32(8), result.TokensRevoked)
	assert.Contains(t, result.AdditionalData, "custom_data")
}

func TestConvertProtoToDeletionSummaryNilInput(t *testing.T) {
	result := convertProtoToDeletionSummary(nil)
	assert.Nil(t, result)
}

func TestConvertProtoToDeletionSummaryComplete(t *testing.T) {
	pb := &iampb.DeletionSummary{
		ProfileDeleted:          true,
		SessionsDeleted:         10,
		AuditLogsAnonymized:     50,
		ConsentsDeleted:         5,
		CredentialsDeleted:      2,
		GroupMembershipsRemoved: 3,
		RoleAssignmentsRemoved:  4,
		TokensRevoked:           8,
		AdditionalData: map[string]int32{
			"custom_data": 100,
		},
	}

	result := convertProtoToDeletionSummary(pb)

	assert.NotNil(t, result)
	assert.True(t, result.ProfileDeleted)
	assert.Equal(t, 10, result.SessionsDeleted)
	assert.Equal(t, 50, result.AuditLogsAnonymized)
	assert.Equal(t, 5, result.ConsentsDeleted)
	assert.Equal(t, 2, result.CredentialsDeleted)
	assert.Equal(t, 3, result.GroupMembershipsRemoved)
	assert.Equal(t, 4, result.RoleAssignmentsRemoved)
	assert.Equal(t, 8, result.TokensRevoked)
	assert.Contains(t, result.AdditionalData, "custom_data")
}

// =============================================================================
// Timestamp Converter Tests
// =============================================================================

func TestConvertProtoTimestampValid(t *testing.T) {
	now := time.Now()
	pb := timestamppb.New(now)

	result := convertProtoTimestamp(pb)

	assert.False(t, result.IsZero())
	assert.Equal(t, now.Unix(), result.Unix())
}

func TestConvertProtoTimestampNilInput(t *testing.T) {
	result := convertProtoTimestamp(nil)

	assert.True(t, result.IsZero())
}

func TestConvertTimeToProtoValid(t *testing.T) {
	now := time.Now()

	result := convertTimeToProto(now)

	assert.NotNil(t, result)
	assert.Equal(t, now.Unix(), result.AsTime().Unix())
}

func TestConvertTimeToProtoZeroTime(t *testing.T) {
	result := convertTimeToProto(time.Time{})

	assert.Nil(t, result)
}

// =============================================================================
// Pool Health Check Loop Tests
// =============================================================================

func TestPoolHealthCheckLoopStopsGracefully(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "error",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	pool := &ConnectionPool{
		conns:           make([]*pooledConn, 0),
		metrics:         newPoolMetrics("localhost:50051"),
		healthCheck:     10 * time.Millisecond,
		stopHealthCheck: make(chan struct{}),
		logger:          log,
	}

	// Start health check loop
	pool.wg.Add(1)
	go pool.healthCheckLoop()

	// Stop immediately
	close(pool.stopHealthCheck)
	pool.wg.Wait()

	// Should complete without hanging
}

// =============================================================================
// Pool Update Metrics Tests
// =============================================================================

func TestPoolUpdateMetricsWithConnections(t *testing.T) {
	pool := &ConnectionPool{
		conns: []*pooledConn{
			{created: time.Now()},
			{created: time.Now()},
			{created: time.Now()},
		},
		metrics:         newPoolMetrics("localhost:50051"),
		stopHealthCheck: make(chan struct{}),
	}

	// Set various states
	pool.conns[0].inUse.Store(true)
	pool.conns[1].inUse.Store(false)
	pool.conns[2].inUse.Store(true)

	pool.updateMetrics()

	// Metrics should be updated without error
	// The actual values are verified by the metrics package
}

// =============================================================================
// Proto Secret Converter Tests
// =============================================================================

func TestConvertProtoSecretNilInput(t *testing.T) {
	result := convertProtoSecret(nil)
	assert.Nil(t, result)
}

func TestConvertProtoSecretVersionNilInput(t *testing.T) {
	result := convertProtoSecretVersion(nil)
	assert.Nil(t, result)
}

func TestConvertProtoSecretVersionWithDatesComplete(t *testing.T) {
	now := time.Now()

	ver := &pb.SecretVersion{
		SecretId:  "secret-1",
		Version:   1,
		CreatedAt: now.Unix(),
		CreatedBy: "admin",
		DeletedAt: now.Add(-1 * time.Hour).Unix(),
		ExpiresAt: now.Add(24 * time.Hour).Unix(),
		Metadata:  map[string]string{"env": "prod"},
	}

	result := convertProtoSecretVersion(ver)

	assert.NotNil(t, result)
	assert.Equal(t, "secret-1", result.SecretID)
	assert.Equal(t, uint64(1), result.Version)
	assert.NotNil(t, result.DeletedAt)
	assert.NotNil(t, result.ExpiresAt)
}

func TestConvertServiceSecretStatusAllValues(t *testing.T) {
	tests := []struct {
		name     string
		input    service.SecretStatus
		expected pb.SecretStatus
	}{
		{"enabled", service.SecretStatusEnabled, pb.SecretStatus_SECRET_STATUS_ENABLED},
		{"disabled", service.SecretStatusDisabled, pb.SecretStatus_SECRET_STATUS_DISABLED},
		{"unknown", service.SecretStatus("unknown"), pb.SecretStatus_SECRET_STATUS_UNSPECIFIED},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertServiceSecretStatus(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConvertProtoSecretStatusToServiceAllValues(t *testing.T) {
	tests := []struct {
		name     string
		input    pb.SecretStatus
		expected service.SecretStatus
	}{
		{"enabled", pb.SecretStatus_SECRET_STATUS_ENABLED, service.SecretStatusEnabled},
		{"disabled", pb.SecretStatus_SECRET_STATUS_DISABLED, service.SecretStatusDisabled},
		{"unspecified", pb.SecretStatus_SECRET_STATUS_UNSPECIFIED, service.SecretStatusEnabled},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertProtoSecretStatusToService(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// =============================================================================
// Wrapped Server Stream Tests (for Stream Interceptors)
// =============================================================================

type testMockServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (m *testMockServerStream) Context() context.Context {
	return m.ctx
}

func (m *testMockServerStream) SetHeader(metadata.MD) error {
	return nil
}

func (m *testMockServerStream) SendHeader(metadata.MD) error {
	return nil
}

func (m *testMockServerStream) SetTrailer(metadata.MD) {}

func (m *testMockServerStream) SendMsg(interface{}) error {
	return nil
}

func (m *testMockServerStream) RecvMsg(interface{}) error {
	return nil
}

func TestWrapServerStreamContext(t *testing.T) {
	originalCtx := context.Background()
	newCtx := context.WithValue(originalCtx, peerCredentialsContextKey{}, &PeerCredentials{PID: 1})

	original := &testMockServerStream{ctx: originalCtx}
	wrapped := wrapServerStream(original, newCtx)

	// Wrapped stream should return new context
	assert.NotEqual(t, originalCtx, wrapped.Context())
}

// =============================================================================
// Data Export Format Converter Tests
// =============================================================================

func TestConvertProtoDataExportFormatAllValues(t *testing.T) {
	tests := []struct {
		name     string
		input    iampb.DataExportFormat
		expected iam.DataExportFormat
	}{
		{"json", iampb.DataExportFormat_DATA_EXPORT_FORMAT_JSON, iam.DataExportFormatJSON},
		{"csv", iampb.DataExportFormat_DATA_EXPORT_FORMAT_CSV, iam.DataExportFormatCSV},
		{"xml", iampb.DataExportFormat_DATA_EXPORT_FORMAT_XML, iam.DataExportFormatXML},
		{"unspecified", iampb.DataExportFormat_DATA_EXPORT_FORMAT_UNSPECIFIED, iam.DataExportFormatJSON},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertProtoDataExportFormat(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConvertDataExportFormatToProtoAllValues(t *testing.T) {
	tests := []struct {
		name     string
		input    iam.DataExportFormat
		expected iampb.DataExportFormat
	}{
		{"json", iam.DataExportFormatJSON, iampb.DataExportFormat_DATA_EXPORT_FORMAT_JSON},
		{"csv", iam.DataExportFormatCSV, iampb.DataExportFormat_DATA_EXPORT_FORMAT_CSV},
		{"xml", iam.DataExportFormatXML, iampb.DataExportFormat_DATA_EXPORT_FORMAT_XML},
		{"unknown", iam.DataExportFormat("unknown"), iampb.DataExportFormat_DATA_EXPORT_FORMAT_UNSPECIFIED},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertDataExportFormatToProto(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConvertProtoDataExportStatusAllValues(t *testing.T) {
	tests := []struct {
		name     string
		input    iampb.DataExportStatus
		expected iam.DataExportStatus
	}{
		{"pending", iampb.DataExportStatus_DATA_EXPORT_STATUS_PENDING, iam.DataExportStatusPending},
		{"processing", iampb.DataExportStatus_DATA_EXPORT_STATUS_PROCESSING, iam.DataExportStatusProcessing},
		{"completed", iampb.DataExportStatus_DATA_EXPORT_STATUS_COMPLETED, iam.DataExportStatusCompleted},
		{"failed", iampb.DataExportStatus_DATA_EXPORT_STATUS_FAILED, iam.DataExportStatusFailed},
		{"expired", iampb.DataExportStatus_DATA_EXPORT_STATUS_EXPIRED, iam.DataExportStatusExpired},
		{"unspecified", iampb.DataExportStatus_DATA_EXPORT_STATUS_UNSPECIFIED, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertProtoDataExportStatus(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConvertDataExportStatusToProtoAllValues(t *testing.T) {
	tests := []struct {
		name     string
		input    iam.DataExportStatus
		expected iampb.DataExportStatus
	}{
		{"pending", iam.DataExportStatusPending, iampb.DataExportStatus_DATA_EXPORT_STATUS_PENDING},
		{"processing", iam.DataExportStatusProcessing, iampb.DataExportStatus_DATA_EXPORT_STATUS_PROCESSING},
		{"completed", iam.DataExportStatusCompleted, iampb.DataExportStatus_DATA_EXPORT_STATUS_COMPLETED},
		{"failed", iam.DataExportStatusFailed, iampb.DataExportStatus_DATA_EXPORT_STATUS_FAILED},
		{"expired", iam.DataExportStatusExpired, iampb.DataExportStatus_DATA_EXPORT_STATUS_EXPIRED},
		{"unknown", iam.DataExportStatus("unknown"), iampb.DataExportStatus_DATA_EXPORT_STATUS_UNSPECIFIED},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertDataExportStatusToProto(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// =============================================================================
// Connection Pool Mutex Coverage
// =============================================================================

func TestPoolConcurrentAccessSafe(t *testing.T) {
	pool := &ConnectionPool{
		conns: []*pooledConn{
			{created: time.Now()},
		},
		metrics:         newPoolMetrics("localhost:50051"),
		stopHealthCheck: make(chan struct{}),
		maxAge:          30 * time.Minute,
		maxRequests:     100000,
	}
	pool.conns[0].healthy.Store(true)

	// Concurrent stats access
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = pool.Stats()
			pool.updateMetrics()
		}()
	}
	wg.Wait()
}

// =============================================================================
// Client Options Metadata Build
// =============================================================================

func TestClientOptionsAddMetadataBuildsOnceCache(t *testing.T) {
	client := &Client{
		opts: &clientOptions{
			metadata: map[string]string{"key": "value"},
			md:       nil, // Not pre-built
		},
	}

	ctx1 := client.addMetadata(context.Background())
	ctx2 := client.addMetadata(context.Background())

	// After first call, md should be cached
	assert.NotNil(t, client.opts.md)

	md1, _ := metadata.FromOutgoingContext(ctx1)
	md2, _ := metadata.FromOutgoingContext(ctx2)

	assert.Equal(t, md1, md2)
}

// =============================================================================
// Pool Metrics Tests
// =============================================================================

func TestNewPoolMetricsCreation(t *testing.T) {
	metrics := newPoolMetrics("test-address:50051")

	assert.NotNil(t, metrics)
	assert.NotNil(t, metrics.poolSize)
	assert.NotNil(t, metrics.activeConns)
	assert.NotNil(t, metrics.idleConns)
	assert.NotNil(t, metrics.totalRequests)
	assert.NotNil(t, metrics.connectionReuse)
	assert.NotNil(t, metrics.connectionCreated)
	assert.NotNil(t, metrics.connectionRecycled)
	assert.NotNil(t, metrics.healthChecksFailed)
	assert.NotNil(t, metrics.getLatency)
}

// =============================================================================
// Default Pool Options Tests
// =============================================================================

func TestDefaultPoolOptionsValues(t *testing.T) {
	opts := defaultPoolOptions()

	assert.Equal(t, 5, opts.minSize)
	assert.Equal(t, 20, opts.maxSize)
	assert.Equal(t, 30*time.Minute, opts.maxAge)
	assert.Equal(t, uint64(100000), opts.maxRequests)
	assert.Equal(t, 30*time.Second, opts.healthCheck)
	assert.True(t, opts.warmupConns)
	assert.NotNil(t, opts.clientOpts)
}
