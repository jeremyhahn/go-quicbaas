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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// =============================================================================
// Mock Federation Manager
// =============================================================================

type mockFederationManager struct {
	registerProviderFn func(ctx context.Context, provider *iam.ExternalProvider) error
	getProviderFn      func(ctx context.Context, tenantID, providerID string) (*iam.ExternalProvider, error)
	updateProviderFn   func(ctx context.Context, provider *iam.ExternalProvider) error
	deleteProviderFn   func(ctx context.Context, tenantID, providerID string) error
	listProvidersFn    func(ctx context.Context, tenantID string, opts iam.ListOptions) ([]*iam.ExternalProvider, error)
	beginLoginFn       func(ctx context.Context, tenantID, providerName, redirectURI string) (*iam.LoginRedirect, error)
	handleCallbackFn   func(ctx context.Context, stateToken, authCode string) (*iam.FederationResult, error)
	linkAccountFn      func(ctx context.Context, userID uint64, tenantID, providerID, externalToken string) error
	unlinkAccountFn    func(ctx context.Context, userID uint64, tenantID, providerID string) error
	getUserLinksFn     func(ctx context.Context, userID uint64, tenantID string) ([]*iam.FederatedIdentity, error)
}

func (m *mockFederationManager) RegisterProvider(ctx context.Context, provider *iam.ExternalProvider) error {
	if m.registerProviderFn != nil {
		return m.registerProviderFn(ctx, provider)
	}
	return nil
}

func (m *mockFederationManager) GetProvider(ctx context.Context, tenantID, providerID string) (*iam.ExternalProvider, error) {
	if m.getProviderFn != nil {
		return m.getProviderFn(ctx, tenantID, providerID)
	}
	return &iam.ExternalProvider{
		ID:       providerID,
		TenantID: tenantID,
		Name:     "test-provider",
		Type:     iam.ProviderTypeOIDC,
		Enabled:  true,
	}, nil
}

func (m *mockFederationManager) GetProviderByName(ctx context.Context, tenantID, providerName string) (*iam.ExternalProvider, error) {
	return &iam.ExternalProvider{
		ID:       "provider-1",
		TenantID: tenantID,
		Name:     providerName,
		Type:     iam.ProviderTypeOIDC,
	}, nil
}

func (m *mockFederationManager) UpdateProvider(ctx context.Context, provider *iam.ExternalProvider) error {
	if m.updateProviderFn != nil {
		return m.updateProviderFn(ctx, provider)
	}
	return nil
}

func (m *mockFederationManager) DeleteProvider(ctx context.Context, tenantID, providerID string) error {
	if m.deleteProviderFn != nil {
		return m.deleteProviderFn(ctx, tenantID, providerID)
	}
	return nil
}

func (m *mockFederationManager) ListProviders(ctx context.Context, tenantID string, opts iam.ListOptions) ([]*iam.ExternalProvider, error) {
	if m.listProvidersFn != nil {
		return m.listProvidersFn(ctx, tenantID, opts)
	}
	return []*iam.ExternalProvider{
		{ID: "provider-1", TenantID: tenantID, Name: "google", Type: iam.ProviderTypeOIDC},
	}, nil
}

func (m *mockFederationManager) BeginLogin(ctx context.Context, tenantID, providerName, redirectURI string) (*iam.LoginRedirect, error) {
	if m.beginLoginFn != nil {
		return m.beginLoginFn(ctx, tenantID, providerName, redirectURI)
	}
	return &iam.LoginRedirect{
		AuthURL: "https://provider.example.com/auth",
		State:   "state-token-123",
	}, nil
}

func (m *mockFederationManager) HandleCallback(ctx context.Context, stateToken, authCode string) (*iam.FederationResult, error) {
	if m.handleCallbackFn != nil {
		return m.handleCallbackFn(ctx, stateToken, authCode)
	}
	return &iam.FederationResult{
		AccessToken: "access-token-123",
		ExpiresIn:   3600,
	}, nil
}

func (m *mockFederationManager) LinkAccount(ctx context.Context, userID uint64, tenantID, providerID, externalToken string) error {
	if m.linkAccountFn != nil {
		return m.linkAccountFn(ctx, userID, tenantID, providerID, externalToken)
	}
	return nil
}

func (m *mockFederationManager) UnlinkAccount(ctx context.Context, userID uint64, tenantID, providerID string) error {
	if m.unlinkAccountFn != nil {
		return m.unlinkAccountFn(ctx, userID, tenantID, providerID)
	}
	return nil
}

func (m *mockFederationManager) GetUserLinks(ctx context.Context, userID uint64, tenantID string) ([]*iam.FederatedIdentity, error) {
	if m.getUserLinksFn != nil {
		return m.getUserLinksFn(ctx, userID, tenantID)
	}
	return []*iam.FederatedIdentity{
		{UserID: userID, ProviderID: "google", ExternalID: "ext-123"},
	}, nil
}

var _ iam.FederationManager = (*mockFederationManager)(nil)

// =============================================================================
// Federation Mock Service
// =============================================================================

type federationMockService struct {
	mockService
	fedMgr iam.FederationManager
}

func (m *federationMockService) GetFederationManager(_ context.Context) (iam.FederationManager, error) {
	return m.fedMgr, nil
}

func newFederationRegistry(t *testing.T, fedMgr iam.FederationManager) *ToolRegistry {
	t.Helper()
	log := newTestLogger(t)
	svc := &federationMockService{fedMgr: fedMgr}
	return &ToolRegistry{service: svc, logger: log}
}

// =============================================================================
// Error Type Tests
// =============================================================================

func TestFederationServiceUnavailableError(t *testing.T) {
	err := &FederationServiceUnavailableError{Reason: "not configured"}
	assert.Contains(t, err.Error(), "federation service unavailable")
	assert.Contains(t, err.Error(), "not configured")
}

func TestIdentityServiceUnavailableError(t *testing.T) {
	err := &IdentityServiceUnavailableError{Reason: "not ready"}
	assert.Contains(t, err.Error(), "identity service unavailable")
	assert.Contains(t, err.Error(), "not ready")
}

// =============================================================================
// getFederationManager Tests
// =============================================================================

func TestGetFederationManager(t *testing.T) {
	tests := []struct {
		name            string
		reg             func(t *testing.T) *ToolRegistry
		wantErr         bool
		wantUnavailable bool
	}{
		{
			name: "service does not support federation",
			reg: func(t *testing.T) *ToolRegistry {
				return &ToolRegistry{service: &mockService{}, logger: newTestLogger(t)}
			},
			wantErr:         true,
			wantUnavailable: true,
		},
		{
			name:    "nil manager",
			reg:     func(t *testing.T) *ToolRegistry { return newFederationRegistry(t, nil) },
			wantErr: true,
		},
		{
			name: "success",
			reg: func(t *testing.T) *ToolRegistry {
				return newFederationRegistry(t, &mockFederationManager{})
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr, err := tt.reg(t).getFederationManager(context.Background())
			if !tt.wantErr {
				require.NoError(t, err)
				require.NotNil(t, mgr)
				return
			}
			require.Error(t, err)
			if tt.wantUnavailable {
				var fedErr *FederationServiceUnavailableError
				assert.ErrorAs(t, err, &fedErr)
			}
		})
	}
}

// =============================================================================
// getProviderIDFromArgs Tests
// =============================================================================

func TestGetProviderIDFromArgs(t *testing.T) {
	tests := []struct {
		name string
		args map[string]interface{}
		want string
	}{
		{name: "id", args: map[string]interface{}{"id": "provider-1"}, want: "provider-1"},
		{name: "provider_id", args: map[string]interface{}{"provider_id": "provider-2"}, want: "provider-2"},
		{name: "prefers id", args: map[string]interface{}{"id": "id-1", "provider_id": "pid-2"}, want: "id-1"},
		{name: "empty", args: map[string]interface{}{}, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, getProviderIDFromArgs(tt.args))
		})
	}
}

// =============================================================================
// Federation Execution Handler Tests - Success Paths
// =============================================================================

// unavailableFederationRegistry returns a registry whose service does not
// support federation, so every federation tool fails before reaching a manager.
func unavailableFederationRegistry(t *testing.T) *ToolRegistry {
	return &ToolRegistry{service: &mockService{}, logger: newTestLogger(t)}
}

func TestExecuteIAMFederationCreateProvider(t *testing.T) {
	args := map[string]interface{}{
		"id":            "google",
		"tenant_id":     "t1",
		"name":          "Google OIDC",
		"type":          "oidc",
		"client_id":     "client-123",
		"client_secret": "secret-abc",
		"issuer_url":    "https://accounts.google.com",
		"scopes":        []interface{}{"openid", "email"},
	}
	tests := []struct {
		name    string
		reg     func(t *testing.T) *ToolRegistry
		wantErr bool
	}{
		{
			name: "success",
			reg: func(t *testing.T) *ToolRegistry {
				return newFederationRegistry(t, &mockFederationManager{})
			},
		},
		{
			name: "service error",
			reg: func(t *testing.T) *ToolRegistry {
				return newFederationRegistry(t, &mockFederationManager{
					registerProviderFn: func(_ context.Context, _ *iam.ExternalProvider) error {
						return errors.New("registration failed")
					},
				})
			},
			wantErr: true,
		},
		{name: "service unavailable", reg: unavailableFederationRegistry, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tt.reg(t).executeIAMFederationCreateProvider(context.Background(), args)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			resultMap, ok := result.(map[string]interface{})
			require.True(t, ok)
			assert.Equal(t, "google", resultMap["id"])
		})
	}
}

func TestExecuteIAMFederationGetProvider(t *testing.T) {
	args := map[string]interface{}{"tenant_id": "t1", "provider_id": "google"}
	tests := []struct {
		name    string
		reg     func(t *testing.T) *ToolRegistry
		wantErr bool
	}{
		{
			name: "success",
			reg: func(t *testing.T) *ToolRegistry {
				return newFederationRegistry(t, &mockFederationManager{})
			},
		},
		{
			name: "not found",
			reg: func(t *testing.T) *ToolRegistry {
				return newFederationRegistry(t, &mockFederationManager{
					getProviderFn: func(_ context.Context, _, _ string) (*iam.ExternalProvider, error) {
						return nil, errors.New("not found")
					},
				})
			},
			wantErr: true,
		},
		{name: "service unavailable", reg: unavailableFederationRegistry, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tt.reg(t).executeIAMFederationGetProvider(context.Background(), args)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, result)
		})
	}
}

func TestExecuteIAMFederationUpdateProvider(t *testing.T) {
	minimalArgs := map[string]interface{}{"id": "google", "tenant_id": "t1"}
	tests := []struct {
		name    string
		mgr     *mockFederationManager
		args    map[string]interface{}
		wantErr bool
	}{
		{
			name: "success with every field",
			mgr:  &mockFederationManager{},
			args: map[string]interface{}{
				"id":             "google",
				"tenant_id":      "t1",
				"name":           "Updated Google",
				"enabled":        false,
				"client_id":      "new-client",
				"client_secret":  "new-secret",
				"issuer_url":     "https://updated.example.com",
				"scopes":         []interface{}{"openid"},
				"auto_provision": true,
				"allow_linking":  true,
			},
		},
		{
			name: "get error",
			mgr: &mockFederationManager{
				getProviderFn: func(_ context.Context, _, _ string) (*iam.ExternalProvider, error) {
					return nil, errors.New("provider not found")
				},
			},
			args:    minimalArgs,
			wantErr: true,
		},
		{
			name: "update error",
			mgr: &mockFederationManager{
				updateProviderFn: func(_ context.Context, _ *iam.ExternalProvider) error {
					return errors.New("update failed")
				},
			},
			args:    minimalArgs,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := newFederationRegistry(t, tt.mgr)
			result, err := reg.executeIAMFederationUpdateProvider(context.Background(), tt.args)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, result)
		})
	}
}

func TestExecuteIAMFederationDeleteProvider(t *testing.T) {
	args := map[string]interface{}{"tenant_id": "t1", "provider_id": "google"}
	tests := []struct {
		name    string
		reg     func(t *testing.T) *ToolRegistry
		wantErr bool
	}{
		{
			name: "success",
			reg: func(t *testing.T) *ToolRegistry {
				return newFederationRegistry(t, &mockFederationManager{})
			},
		},
		{
			name: "delete error",
			reg: func(t *testing.T) *ToolRegistry {
				return newFederationRegistry(t, &mockFederationManager{
					deleteProviderFn: func(_ context.Context, _, _ string) error {
						return errors.New("delete failed")
					},
				})
			},
			wantErr: true,
		},
		{name: "service unavailable", reg: unavailableFederationRegistry, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tt.reg(t).executeIAMFederationDeleteProvider(context.Background(), args)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			resultMap, ok := result.(map[string]interface{})
			require.True(t, ok)
			assert.Equal(t, "deleted", resultMap["status"])
		})
	}
}

func TestExecuteIAMFederationListProviders_Success(t *testing.T) {
	fedMgr := &mockFederationManager{}
	reg := newFederationRegistry(t, fedMgr)

	result, err := reg.executeIAMFederationListProviders(context.Background(), map[string]interface{}{
		"tenant_id": "t1",
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.NotNil(t, resultMap["providers"])
}

// assertNoProviderSecret fails when a federation tool result, encoded as the
// MCP client receives it, carries the upstream client secret or any
// non-empty client_secret value.
func assertNoProviderSecret(t *testing.T, result interface{}, secret string) {
	t.Helper()
	body, err := json.Marshal(result)
	require.NoError(t, err)
	assert.NotContains(t, string(body), secret)
	assert.NotRegexp(t, `"client_secret":\s*"[^"]`, string(body))
}

func TestExecuteIAMFederationProviderTools_RedactClientSecret(t *testing.T) {
	const secret = "upstream-idp-secret"
	storedProvider := &iam.ExternalProvider{
		ID:           "google",
		TenantID:     "t1",
		Name:         "Google",
		Type:         iam.ProviderTypeOIDC,
		Enabled:      true,
		ClientID:     "client-id",
		ClientSecret: secret,
		IssuerURL:    "https://accounts.example.com",
	}
	fedMgr := &mockFederationManager{
		getProviderFn: func(ctx context.Context, tenantID, providerID string) (*iam.ExternalProvider, error) {
			return storedProvider, nil
		},
		listProvidersFn: func(ctx context.Context, tenantID string, opts iam.ListOptions) ([]*iam.ExternalProvider, error) {
			return []*iam.ExternalProvider{storedProvider}, nil
		},
	}
	reg := newFederationRegistry(t, fedMgr)
	ctx := context.Background()

	t.Run("create", func(t *testing.T) {
		result, err := reg.executeIAMFederationCreateProvider(ctx, map[string]interface{}{
			"tenant_id":     "t1",
			"name":          "Google",
			"type":          "oidc",
			"client_id":     "client-id",
			"client_secret": secret,
			"issuer_url":    "https://accounts.example.com",
		})
		require.NoError(t, err)
		assertNoProviderSecret(t, result, secret)
	})

	t.Run("get", func(t *testing.T) {
		result, err := reg.executeIAMFederationGetProvider(ctx, map[string]interface{}{
			"id":        "google",
			"tenant_id": "t1",
		})
		require.NoError(t, err)
		assertNoProviderSecret(t, result, secret)
	})

	t.Run("list", func(t *testing.T) {
		result, err := reg.executeIAMFederationListProviders(ctx, map[string]interface{}{
			"tenant_id": "t1",
		})
		require.NoError(t, err)
		body, err := json.Marshal(result)
		require.NoError(t, err)
		assert.Contains(t, string(body), `"id":"google"`)
		assertNoProviderSecret(t, result, secret)
	})

	t.Run("update", func(t *testing.T) {
		result, err := reg.executeIAMFederationUpdateProvider(ctx, map[string]interface{}{
			"id":        "google",
			"tenant_id": "t1",
			"name":      "Google Renamed",
		})
		require.NoError(t, err)
		assertNoProviderSecret(t, result, secret)
	})

	assert.Equal(t, secret, storedProvider.ClientSecret, "the stored provider must keep the secret")
}

func TestExecuteIAMFederationListProviders_WithPagination(t *testing.T) {
	fedMgr := &mockFederationManager{}
	reg := newFederationRegistry(t, fedMgr)

	result, err := reg.executeIAMFederationListProviders(context.Background(), map[string]interface{}{
		"tenant_id": "t1",
		"limit":     float64(50),
		"offset":    float64(10),
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestExecuteIAMFederationListProviders_Error(t *testing.T) {
	fedMgr := &mockFederationManager{
		listProvidersFn: func(_ context.Context, _ string, _ iam.ListOptions) ([]*iam.ExternalProvider, error) {
			return nil, errors.New("list failed")
		},
	}
	reg := newFederationRegistry(t, fedMgr)

	_, err := reg.executeIAMFederationListProviders(context.Background(), map[string]interface{}{
		"tenant_id": "t1",
	})
	require.Error(t, err)
}

func TestExecuteIAMFederationBeginLogin_Success(t *testing.T) {
	fedMgr := &mockFederationManager{}
	reg := newFederationRegistry(t, fedMgr)

	result, err := reg.executeIAMFederationBeginLogin(context.Background(), map[string]interface{}{
		"tenant_id":     "t1",
		"provider_name": "google",
		"redirect_uri":  "https://app.example.com/callback",
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestExecuteIAMFederationBeginLogin_Error(t *testing.T) {
	fedMgr := &mockFederationManager{
		beginLoginFn: func(_ context.Context, _, _, _ string) (*iam.LoginRedirect, error) {
			return nil, errors.New("provider not found")
		},
	}
	reg := newFederationRegistry(t, fedMgr)

	_, err := reg.executeIAMFederationBeginLogin(context.Background(), map[string]interface{}{
		"tenant_id": "t1", "provider_name": "unknown", "redirect_uri": "https://example.com",
	})
	require.Error(t, err)
}

func TestExecuteIAMFederationHandleCallback_Success(t *testing.T) {
	fedMgr := &mockFederationManager{}
	reg := newFederationRegistry(t, fedMgr)

	result, err := reg.executeIAMFederationHandleCallback(context.Background(), map[string]interface{}{
		"state_token": "state-123",
		"auth_code":   "code-abc",
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestExecuteIAMFederationHandleCallback_Error(t *testing.T) {
	fedMgr := &mockFederationManager{
		handleCallbackFn: func(_ context.Context, _, _ string) (*iam.FederationResult, error) {
			return nil, errors.New("invalid state token")
		},
	}
	reg := newFederationRegistry(t, fedMgr)

	_, err := reg.executeIAMFederationHandleCallback(context.Background(), map[string]interface{}{
		"state_token": "invalid", "auth_code": "code",
	})
	require.Error(t, err)
}

func TestExecuteIAMFederationLinkAccount_Success(t *testing.T) {
	fedMgr := &mockFederationManager{}
	reg := newFederationRegistry(t, fedMgr)

	result, err := reg.executeIAMFederationLinkAccount(context.Background(), map[string]interface{}{
		"user_id":        "30001",
		"tenant_id":      "t1",
		"provider_id":    "google",
		"external_token": "ext-token-123",
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "linked", resultMap["status"])
}

func TestExecuteIAMFederationLinkAccount_Error(t *testing.T) {
	fedMgr := &mockFederationManager{
		linkAccountFn: func(_ context.Context, _ uint64, _, _, _ string) error {
			return errors.New("link failed")
		},
	}
	reg := newFederationRegistry(t, fedMgr)

	_, err := reg.executeIAMFederationLinkAccount(context.Background(), map[string]interface{}{
		"user_id": "30001", "tenant_id": "t1", "provider_id": "google", "external_token": "tok",
	})
	require.Error(t, err)
}

func TestExecuteIAMFederationUnlinkAccount_Success(t *testing.T) {
	fedMgr := &mockFederationManager{}
	reg := newFederationRegistry(t, fedMgr)

	result, err := reg.executeIAMFederationUnlinkAccount(context.Background(), map[string]interface{}{
		"user_id":     "30001",
		"tenant_id":   "t1",
		"provider_id": "google",
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "unlinked", resultMap["status"])
}

func TestExecuteIAMFederationUnlinkAccount_Error(t *testing.T) {
	fedMgr := &mockFederationManager{
		unlinkAccountFn: func(_ context.Context, _ uint64, _, _ string) error {
			return errors.New("unlink failed")
		},
	}
	reg := newFederationRegistry(t, fedMgr)

	_, err := reg.executeIAMFederationUnlinkAccount(context.Background(), map[string]interface{}{
		"user_id": "30001", "tenant_id": "t1", "provider_id": "google",
	})
	require.Error(t, err)
}

func TestExecuteIAMFederationGetUserLinks_Success(t *testing.T) {
	fedMgr := &mockFederationManager{}
	reg := newFederationRegistry(t, fedMgr)

	result, err := reg.executeIAMFederationGetUserLinks(context.Background(), map[string]interface{}{
		"user_id":   "30001",
		"tenant_id": "t1",
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.NotNil(t, resultMap["links"])
}

func TestExecuteIAMFederationGetUserLinks_Error(t *testing.T) {
	fedMgr := &mockFederationManager{
		getUserLinksFn: func(_ context.Context, _ uint64, _ string) ([]*iam.FederatedIdentity, error) {
			return nil, errors.New("get links failed")
		},
	}
	reg := newFederationRegistry(t, fedMgr)

	_, err := reg.executeIAMFederationGetUserLinks(context.Background(), map[string]interface{}{
		"user_id": "30001", "tenant_id": "t1",
	})
	require.Error(t, err)
}

// =============================================================================
// Identity Execution Handler Tests
// =============================================================================

func TestExecuteIAMIdentityLink_Success(t *testing.T) {
	fedMgr := &mockFederationManager{}
	reg := newFederationRegistry(t, fedMgr)

	result, err := reg.executeIAMIdentityLink(context.Background(), map[string]interface{}{
		"user_id":        "30001",
		"tenant_id":      "t1",
		"provider_id":    "google",
		"external_token": "ext-token-123",
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "linked", resultMap["status"])
}

func TestExecuteIAMIdentityLink_Error(t *testing.T) {
	fedMgr := &mockFederationManager{
		linkAccountFn: func(_ context.Context, _ uint64, _, _, _ string) error {
			return errors.New("identity link failed")
		},
	}
	reg := newFederationRegistry(t, fedMgr)

	_, err := reg.executeIAMIdentityLink(context.Background(), map[string]interface{}{
		"user_id": "30001", "tenant_id": "t1", "provider_id": "google", "external_token": "tok",
	})
	require.Error(t, err)
}

func TestExecuteIAMIdentityUnlink_Success(t *testing.T) {
	fedMgr := &mockFederationManager{}
	reg := newFederationRegistry(t, fedMgr)

	result, err := reg.executeIAMIdentityUnlink(context.Background(), map[string]interface{}{
		"user_id":     "30001",
		"tenant_id":   "t1",
		"provider_id": "google",
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "unlinked", resultMap["status"])
}

func TestExecuteIAMIdentityUnlink_Error(t *testing.T) {
	fedMgr := &mockFederationManager{
		unlinkAccountFn: func(_ context.Context, _ uint64, _, _ string) error {
			return errors.New("identity unlink failed")
		},
	}
	reg := newFederationRegistry(t, fedMgr)

	_, err := reg.executeIAMIdentityUnlink(context.Background(), map[string]interface{}{
		"user_id": "30001", "tenant_id": "t1", "provider_id": "google",
	})
	require.Error(t, err)
}

func TestExecuteIAMIdentityListUserIdentities_Success(t *testing.T) {
	fedMgr := &mockFederationManager{}
	reg := newFederationRegistry(t, fedMgr)

	result, err := reg.executeIAMIdentityListUserIdentities(context.Background(), map[string]interface{}{
		"user_id":   "30001",
		"tenant_id": "t1",
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.NotNil(t, resultMap["identities"])
}

func TestExecuteIAMIdentityListUserIdentities_Error(t *testing.T) {
	fedMgr := &mockFederationManager{
		getUserLinksFn: func(_ context.Context, _ uint64, _ string) ([]*iam.FederatedIdentity, error) {
			return nil, errors.New("list identities failed")
		},
	}
	reg := newFederationRegistry(t, fedMgr)

	_, err := reg.executeIAMIdentityListUserIdentities(context.Background(), map[string]interface{}{
		"user_id": "30001", "tenant_id": "t1",
	})
	require.Error(t, err)
}
