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

//go:build iam

package grpc

import (
	stderrors "errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// =============================================================================
// convertProtoProviderType Tests
// =============================================================================

func TestConvertProtoProviderType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    iampb.ProviderType
		expected iam.ProviderType
	}{
		{
			name:     "OIDC provider",
			input:    iampb.ProviderType_PROVIDER_TYPE_OIDC,
			expected: iam.ProviderTypeOIDC,
		},
		{
			name:     "SAML provider",
			input:    iampb.ProviderType_PROVIDER_TYPE_SAML,
			expected: iam.ProviderTypeSAML,
		},
		{
			name:     "OAuth2 provider",
			input:    iampb.ProviderType_PROVIDER_TYPE_OAUTH2,
			expected: iam.ProviderTypeOAuth2,
		},
		{
			name:     "unspecified defaults to OIDC",
			input:    iampb.ProviderType_PROVIDER_TYPE_UNSPECIFIED,
			expected: iam.ProviderTypeOIDC,
		},
		{
			name:     "unknown value defaults to OIDC",
			input:    iampb.ProviderType(999),
			expected: iam.ProviderTypeOIDC,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertProtoProviderType(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// =============================================================================
// convertProviderTypeToProto Tests
// =============================================================================

func TestConvertProviderTypeToProto(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    iam.ProviderType
		expected iampb.ProviderType
	}{
		{
			name:     "OIDC provider",
			input:    iam.ProviderTypeOIDC,
			expected: iampb.ProviderType_PROVIDER_TYPE_OIDC,
		},
		{
			name:     "SAML provider",
			input:    iam.ProviderTypeSAML,
			expected: iampb.ProviderType_PROVIDER_TYPE_SAML,
		},
		{
			name:     "OAuth2 provider",
			input:    iam.ProviderTypeOAuth2,
			expected: iampb.ProviderType_PROVIDER_TYPE_OAUTH2,
		},
		{
			name:     "unknown value defaults to unspecified",
			input:    iam.ProviderType("unknown"),
			expected: iampb.ProviderType_PROVIDER_TYPE_UNSPECIFIED,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertProviderTypeToProto(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// =============================================================================
// convertProtoToExternalProvider Tests
// =============================================================================

func TestConvertProtoToExternalProvider(t *testing.T) {
	t.Parallel()

	t.Run("basic conversion without claim mappings", func(t *testing.T) {
		req := &iampb.CreateProviderRequest{
			Id:            "provider-1",
			TenantId:      "tenant-1",
			Name:          "Test Provider",
			Type:          iampb.ProviderType_PROVIDER_TYPE_OIDC,
			Enabled:       true,
			ClientId:      "client-123",
			ClientSecret:  "secret-456",
			IssuerUrl:     "https://issuer.example.com",
			AuthUrl:       "https://auth.example.com",
			TokenUrl:      "https://token.example.com",
			UserinfoUrl:   "https://userinfo.example.com",
			Scopes:        []string{"openid", "profile", "email"},
			AutoProvision: true,
			AllowLinking:  true,
			DefaultRoles:  []string{"viewer"},
			DefaultGroups: []string{"users"},
		}

		result := convertProtoToExternalProvider(req)

		assert.NotNil(t, result)
		assert.Equal(t, "provider-1", result.ID)
		assert.Equal(t, "tenant-1", result.TenantID)
		assert.Equal(t, "Test Provider", result.Name)
		assert.Equal(t, iam.ProviderTypeOIDC, result.Type)
		assert.True(t, result.Enabled)
		assert.Equal(t, "client-123", result.ClientID)
		assert.Equal(t, "secret-456", result.ClientSecret)
		assert.Equal(t, "https://issuer.example.com", result.IssuerURL)
		assert.Equal(t, "https://auth.example.com", result.AuthURL)
		assert.Equal(t, "https://token.example.com", result.TokenURL)
		assert.Equal(t, "https://userinfo.example.com", result.UserInfoURL)
		assert.Equal(t, []string{"openid", "profile", "email"}, result.Scopes)
		assert.True(t, result.AutoProvision)
		assert.True(t, result.AllowLinking)
		assert.Equal(t, []string{"viewer"}, result.DefaultRoles)
		assert.Equal(t, []string{"users"}, result.DefaultGroups)
		assert.Nil(t, result.ClaimMappings)
	})

	t.Run("conversion with claim mappings", func(t *testing.T) {
		req := &iampb.CreateProviderRequest{
			Id:       "provider-2",
			TenantId: "tenant-2",
			Name:     "Provider With Claims",
			Type:     iampb.ProviderType_PROVIDER_TYPE_SAML,
			ClaimMappings: &iampb.ClaimMappings{
				Subject:    "sub",
				Email:      "email",
				Name:       "name",
				Groups:     "groups",
				Roles:      "roles",
				Attributes: map[string]string{"custom": "value"},
			},
		}

		result := convertProtoToExternalProvider(req)

		assert.NotNil(t, result)
		assert.NotNil(t, result.ClaimMappings)
		assert.Equal(t, "sub", result.ClaimMappings.Subject)
		assert.Equal(t, "email", result.ClaimMappings.Email)
		assert.Equal(t, "name", result.ClaimMappings.Name)
		assert.Equal(t, "groups", result.ClaimMappings.Groups)
		assert.Equal(t, "roles", result.ClaimMappings.Roles)
		assert.Equal(t, map[string]string{"custom": "value"}, result.ClaimMappings.Attributes)
	})
}

// =============================================================================
// mergeProtoUpdateToExternalProvider Tests
// =============================================================================

func TestMergeProtoUpdateToExternalProvider(t *testing.T) {
	t.Parallel()

	baseProvider := &iam.ExternalProvider{
		ID:           "provider-update-1",
		TenantID:     "tenant-update-1",
		Name:         "Original Provider",
		Type:         iam.ProviderTypeOIDC,
		Enabled:      true,
		ClientID:     "original-client",
		ClientSecret: "original-secret",
		IssuerURL:    "https://original-issuer.example.com",
		AuthURL:      "https://original-auth.example.com",
		TokenURL:     "https://original-token.example.com",
		UserInfoURL:  "https://original-userinfo.example.com",
		Scopes:       []string{"openid"},
	}

	t.Run("basic update conversion", func(t *testing.T) {
		req := &iampb.UpdateProviderRequest{
			ProviderId:    "provider-update-1",
			TenantId:      "tenant-update-1",
			Name:          "Updated Provider",
			Type:          iampb.ProviderType_PROVIDER_TYPE_OAUTH2,
			Enabled:       false,
			ClientId:      "updated-client",
			ClientSecret:  "updated-secret",
			IssuerUrl:     "https://updated-issuer.example.com",
			AuthUrl:       "https://updated-auth.example.com",
			TokenUrl:      "https://updated-token.example.com",
			UserinfoUrl:   "https://updated-userinfo.example.com",
			Scopes:        []string{"read", "write"},
			AutoProvision: false,
			AllowLinking:  false,
			DefaultRoles:  []string{"admin"},
			DefaultGroups: []string{"admins"},
		}

		result := mergeProtoUpdateToExternalProvider(req, baseProvider)

		assert.NotNil(t, result)
		assert.Equal(t, "provider-update-1", result.ID)
		assert.Equal(t, "tenant-update-1", result.TenantID)
		assert.Equal(t, "Updated Provider", result.Name)
		assert.Equal(t, iam.ProviderTypeOAuth2, result.Type)
		assert.False(t, result.Enabled)
		assert.Equal(t, []string{"read", "write"}, result.Scopes)
		assert.False(t, result.AutoProvision)
	})

	t.Run("partial update preserves existing fields", func(t *testing.T) {
		req := &iampb.UpdateProviderRequest{
			ProviderId: "provider-update-1",
			TenantId:   "tenant-update-1",
			Name:       "Partially Updated",
		}

		result := mergeProtoUpdateToExternalProvider(req, baseProvider)

		assert.NotNil(t, result)
		assert.Equal(t, "Partially Updated", result.Name)
		assert.Equal(t, "original-client", result.ClientID, "client_id should be preserved")
		assert.Equal(t, "original-secret", result.ClientSecret, "client_secret should be preserved")
		assert.Equal(t, "https://original-issuer.example.com", result.IssuerURL, "issuer_url should be preserved")
		assert.Equal(t, iam.ProviderTypeOIDC, result.Type, "type should be preserved")
	})

	t.Run("update with claim mappings", func(t *testing.T) {
		req := &iampb.UpdateProviderRequest{
			ProviderId: "provider-update-2",
			TenantId:   "tenant-update-2",
			Name:       "Updated With Claims",
			ClaimMappings: &iampb.ClaimMappings{
				Subject: "updated-sub",
				Email:   "updated-email",
			},
		}

		result := mergeProtoUpdateToExternalProvider(req, baseProvider)

		assert.NotNil(t, result)
		assert.NotNil(t, result.ClaimMappings)
		assert.Equal(t, "updated-sub", result.ClaimMappings.Subject)
		assert.Equal(t, "updated-email", result.ClaimMappings.Email)
	})
}

// =============================================================================
// convertExternalProviderToProto Tests
// =============================================================================

func TestConvertExternalProviderToProto(t *testing.T) {
	t.Parallel()

	t.Run("nil provider returns nil", func(t *testing.T) {
		result := convertExternalProviderToProto(nil)
		assert.Nil(t, result)
	})

	t.Run("basic conversion", func(t *testing.T) {
		now := time.Now()
		provider := &iam.ExternalProvider{
			ID:            "ext-provider-1",
			TenantID:      "ext-tenant-1",
			Name:          "External Provider",
			Type:          iam.ProviderTypeSAML,
			Enabled:       true,
			ClientID:      "ext-client",
			ClientSecret:  "ext-secret", // Should not be included in response
			IssuerURL:     "https://ext-issuer.example.com",
			AuthURL:       "https://ext-auth.example.com",
			TokenURL:      "https://ext-token.example.com",
			UserInfoURL:   "https://ext-userinfo.example.com",
			Scopes:        []string{"scope1", "scope2"},
			AutoProvision: true,
			AllowLinking:  true,
			DefaultRoles:  []string{"role1"},
			DefaultGroups: []string{"group1"},
			Created:       now,
			Updated:       now,
		}

		result := convertExternalProviderToProto(provider)

		assert.NotNil(t, result)
		assert.Equal(t, "ext-provider-1", result.Id)
		assert.Equal(t, "ext-tenant-1", result.TenantId)
		assert.Equal(t, "External Provider", result.Name)
		assert.Equal(t, iampb.ProviderType_PROVIDER_TYPE_SAML, result.Type)
		assert.True(t, result.Enabled)
		assert.Equal(t, "ext-client", result.ClientId)
		// Client secret should NOT be in response
		assert.Empty(t, result.ClientSecret)
		assert.Equal(t, []string{"scope1", "scope2"}, result.Scopes)
		assert.NotNil(t, result.CreatedAt)
		assert.NotNil(t, result.UpdatedAt)
	})

	t.Run("conversion with claim mappings", func(t *testing.T) {
		provider := &iam.ExternalProvider{
			ID:       "ext-provider-claims",
			TenantID: "ext-tenant-claims",
			Name:     "Provider With Claims",
			ClaimMappings: &iam.ClaimMappings{
				Subject:    "custom-sub",
				Email:      "custom-email",
				Name:       "custom-name",
				Groups:     "custom-groups",
				Roles:      "custom-roles",
				Attributes: map[string]string{"attr1": "val1"},
			},
		}

		result := convertExternalProviderToProto(provider)

		assert.NotNil(t, result)
		assert.NotNil(t, result.ClaimMappings)
		assert.Equal(t, "custom-sub", result.ClaimMappings.Subject)
		assert.Equal(t, "custom-email", result.ClaimMappings.Email)
		assert.Equal(t, "custom-name", result.ClaimMappings.Name)
		assert.Equal(t, "custom-groups", result.ClaimMappings.Groups)
		assert.Equal(t, "custom-roles", result.ClaimMappings.Roles)
		assert.Equal(t, map[string]string{"attr1": "val1"}, result.ClaimMappings.Attributes)
	})
}

// =============================================================================
// convertFederatedIdentityToProto Tests
// =============================================================================

func TestConvertFederatedIdentityToProto(t *testing.T) {
	t.Parallel()

	t.Run("nil identity returns nil", func(t *testing.T) {
		result := convertFederatedIdentityToProto(nil)
		assert.Nil(t, result)
	})

	t.Run("basic conversion", func(t *testing.T) {
		now := time.Now()
		fi := &iam.FederatedIdentity{
			ID:            "fed-id-1",
			UserID:        "user-1",
			TenantID:      "tenant-1",
			ProviderID:    "provider-1",
			ExternalID:    "ext-id-1",
			ExternalEmail: "user@external.com",
			ExternalName:  "External User",
			LastLogin:     now,
			Created:       now,
			Updated:       now,
		}

		result := convertFederatedIdentityToProto(fi)

		assert.NotNil(t, result)
		assert.Equal(t, "fed-id-1", result.Id)
		assert.Equal(t, "user-1", result.UserId)
		assert.Equal(t, "tenant-1", result.TenantId)
		assert.Equal(t, "provider-1", result.ProviderId)
		assert.Equal(t, "ext-id-1", result.ExternalId)
		assert.Equal(t, "user@external.com", result.ExternalEmail)
		assert.Equal(t, "External User", result.ExternalName)
		assert.NotNil(t, result.LastLogin)
		assert.NotNil(t, result.CreatedAt)
		assert.NotNil(t, result.UpdatedAt)
	})

	t.Run("conversion with string claims", func(t *testing.T) {
		fi := &iam.FederatedIdentity{
			ID:     "fed-id-claims",
			UserID: "user-claims",
			Claims: map[string]interface{}{
				"string_claim": "value1",
				"int_claim":    123,  // Non-string should be skipped
				"bool_claim":   true, // Non-string should be skipped
			},
		}

		result := convertFederatedIdentityToProto(fi)

		assert.NotNil(t, result)
		assert.NotNil(t, result.Claims)
		assert.Equal(t, "value1", result.Claims["string_claim"])
		// Non-string claims should not be present
		_, hasInt := result.Claims["int_claim"]
		assert.False(t, hasInt)
		_, hasBool := result.Claims["bool_claim"]
		assert.False(t, hasBool)
	})

	t.Run("conversion with empty claims", func(t *testing.T) {
		fi := &iam.FederatedIdentity{
			ID:     "fed-id-empty-claims",
			UserID: "user-empty-claims",
			Claims: map[string]interface{}{},
		}

		result := convertFederatedIdentityToProto(fi)

		assert.NotNil(t, result)
		// Empty claims should result in nil or empty map
		assert.True(t, result.Claims == nil || len(result.Claims) == 0)
	})
}

// =============================================================================
// convertFederationError Tests
// =============================================================================

func TestConvertFederationError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		err          error
		expectedCode codes.Code
	}{
		{
			name:         "nil error returns nil",
			err:          nil,
			expectedCode: codes.OK,
		},
		{
			name: "not found error",
			err: &errors.DragonError{
				Code: errors.ErrNotFound,
				Op:   "federation.test",
				Err:  stderrors.New("provider not found"),
			},
			expectedCode: codes.NotFound,
		},
		{
			name: "already exists error",
			err: &errors.DragonError{
				Code: errors.ErrAlreadyExists,
				Op:   "federation.test",
				Err:  stderrors.New("link already exists"),
			},
			expectedCode: codes.AlreadyExists,
		},
		{
			name: "invalid input error",
			err: &errors.DragonError{
				Code: errors.ErrInvalidInput,
				Op:   "federation.test",
				Err:  stderrors.New("invalid provider config"),
			},
			expectedCode: codes.InvalidArgument,
		},
		{
			name: "validation error",
			err: &errors.DragonError{
				Code: errors.ErrValidation,
				Op:   "federation.test",
				Err:  stderrors.New("validation failed"),
			},
			expectedCode: codes.InvalidArgument,
		},
		{
			name: "unauthorized error",
			err: &errors.DragonError{
				Code: errors.ErrUnauthorized,
				Op:   "federation.test",
				Err:  stderrors.New("not authenticated"),
			},
			expectedCode: codes.Unauthenticated,
		},
		{
			name: "forbidden error",
			err: &errors.DragonError{
				Code: errors.ErrForbidden,
				Op:   "federation.test",
				Err:  stderrors.New("access denied"),
			},
			expectedCode: codes.PermissionDenied,
		},
		{
			name: "timeout error",
			err: &errors.DragonError{
				Code: errors.ErrTimeout,
				Op:   "federation.test",
				Err:  stderrors.New("operation timed out"),
			},
			expectedCode: codes.DeadlineExceeded,
		},
		{
			name: "deadline exceeded error",
			err: &errors.DragonError{
				Code: errors.ErrDeadlineExceeded,
				Op:   "federation.test",
				Err:  stderrors.New("deadline exceeded"),
			},
			expectedCode: codes.DeadlineExceeded,
		},
		{
			name: "resource exhausted error",
			err: &errors.DragonError{
				Code: errors.ErrResourceExhausted,
				Op:   "federation.test",
				Err:  stderrors.New("too many requests"),
			},
			expectedCode: codes.ResourceExhausted,
		},
		{
			name: "unavailable error",
			err: &errors.DragonError{
				Code: errors.ErrUnavailable,
				Op:   "federation.test",
				Err:  stderrors.New("service unavailable"),
			},
			expectedCode: codes.Unavailable,
		},
		{
			name: "conflict error",
			err: &errors.DragonError{
				Code: errors.ErrConflict,
				Op:   "federation.test",
				Err:  stderrors.New("conflict detected"),
			},
			expectedCode: codes.FailedPrecondition,
		},
		{
			name: "unimplemented error",
			err: &errors.DragonError{
				Code: errors.ErrUnimplemented,
				Op:   "federation.test",
				Err:  stderrors.New("not implemented"),
			},
			expectedCode: codes.Unimplemented,
		},
		{
			name: "not implemented error",
			err: &errors.DragonError{
				Code: errors.ErrNotImplemented,
				Op:   "federation.test",
				Err:  stderrors.New("feature not implemented"),
			},
			expectedCode: codes.Unimplemented,
		},
		{
			name: "internal error",
			err: &errors.DragonError{
				Code: errors.ErrInternal,
				Op:   "federation.test",
				Err:  stderrors.New("internal error"),
			},
			expectedCode: codes.Internal,
		},
		{
			name:         "non-DragonError returns internal",
			err:          stderrors.New("generic error"),
			expectedCode: codes.Internal,
		},
		{
			name: "unknown error code defaults to internal",
			err: &errors.DragonError{
				Code: errors.ErrorCode(8888),
				Op:   "federation.test",
				Err:  stderrors.New("unknown code"),
			},
			expectedCode: codes.Internal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertFederationError(tt.err)

			if tt.err == nil {
				assert.Nil(t, result)
				return
			}

			assert.NotNil(t, result)
			st, ok := status.FromError(result)
			assert.True(t, ok)
			assert.Equal(t, tt.expectedCode, st.Code())
		})
	}
}
