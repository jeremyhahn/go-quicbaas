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
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"

	stderrors "errors"
)

// mockFederationManager is a testify mock implementing iam.FederationManager.
type mockFederationManager struct {
	mock.Mock
}

func (m *mockFederationManager) RegisterProvider(ctx context.Context, provider *iam.ExternalProvider) error {
	return m.Called(ctx, provider).Error(0)
}

func (m *mockFederationManager) GetProvider(ctx context.Context, tenantID, providerID string) (*iam.ExternalProvider, error) {
	args := m.Called(ctx, tenantID, providerID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*iam.ExternalProvider), args.Error(1)
}

func (m *mockFederationManager) GetProviderByName(ctx context.Context, tenantID, providerName string) (*iam.ExternalProvider, error) {
	args := m.Called(ctx, tenantID, providerName)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*iam.ExternalProvider), args.Error(1)
}

func (m *mockFederationManager) UpdateProvider(ctx context.Context, provider *iam.ExternalProvider) error {
	return m.Called(ctx, provider).Error(0)
}

func (m *mockFederationManager) DeleteProvider(ctx context.Context, tenantID, providerID string) error {
	return m.Called(ctx, tenantID, providerID).Error(0)
}

func (m *mockFederationManager) ListProviders(ctx context.Context, tenantID string, opts iam.ListOptions) ([]*iam.ExternalProvider, error) {
	args := m.Called(ctx, tenantID, opts)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*iam.ExternalProvider), args.Error(1)
}

func (m *mockFederationManager) BeginLogin(ctx context.Context, tenantID, providerName, redirectURI string) (*iam.LoginRedirect, error) {
	args := m.Called(ctx, tenantID, providerName, redirectURI)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*iam.LoginRedirect), args.Error(1)
}

func (m *mockFederationManager) HandleCallback(ctx context.Context, stateToken, authCode string) (*iam.FederationResult, error) {
	args := m.Called(ctx, stateToken, authCode)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*iam.FederationResult), args.Error(1)
}

func (m *mockFederationManager) LinkAccount(ctx context.Context, userID, tenantID, providerID, externalToken string) error {
	return m.Called(ctx, userID, tenantID, providerID, externalToken).Error(0)
}

func (m *mockFederationManager) UnlinkAccount(ctx context.Context, userID, tenantID, providerID string) error {
	return m.Called(ctx, userID, tenantID, providerID).Error(0)
}

func (m *mockFederationManager) GetUserLinks(ctx context.Context, userID, tenantID string) ([]*iam.FederatedIdentity, error) {
	args := m.Called(ctx, userID, tenantID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*iam.FederatedIdentity), args.Error(1)
}

func newTestFederationServer(t *testing.T) (*FederationServer, *mockFederationManager) {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)
	mgr := &mockFederationManager{}
	srv := NewFederationServer(mgr, log)
	return srv, mgr
}

// =============================================================================
// Provider Management Tests
// =============================================================================

func TestFederationServer_CreateProvider_Success(t *testing.T) {
	srv, mgr := newTestFederationServer(t)

	mgr.On("RegisterProvider", mock.Anything, mock.AnythingOfType("*iam.ExternalProvider")).Return(nil)

	resp, err := srv.CreateProvider(context.Background(), &iampb.CreateProviderRequest{
		TenantId:      "t1",
		Name:          "google",
		ClientId:      "client-123",
		ClientSecret:  "secret-456",
		Type:          iampb.ProviderType_PROVIDER_TYPE_OIDC,
		Enabled:       true,
		IssuerUrl:     "https://accounts.google.com",
		AuthUrl:       "https://accounts.google.com/o/oauth2/auth",
		TokenUrl:      "https://oauth2.googleapis.com/token",
		UserinfoUrl:   "https://openidconnect.googleapis.com/v1/userinfo",
		Scopes:        []string{"openid", "email"},
		AutoProvision: true,
		AllowLinking:  true,
		DefaultRoles:  []string{"user"},
		DefaultGroups: []string{"all-users"},
		ClaimMappings: &iampb.ClaimMappings{
			Subject: "sub",
			Email:   "email",
			Name:    "name",
			Groups:  "groups",
			Roles:   "roles",
		},
	})

	require.NoError(t, err)
	assert.Equal(t, "t1", resp.TenantId)
	assert.Equal(t, "google", resp.Name)
	mgr.AssertExpectations(t)
}

func TestFederationServer_CreateProvider_MissingFields(t *testing.T) {
	srv, _ := newTestFederationServer(t)
	ctx := context.Background()

	_, err := srv.CreateProvider(ctx, &iampb.CreateProviderRequest{Name: "test", ClientId: "c"})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())

	_, err = srv.CreateProvider(ctx, &iampb.CreateProviderRequest{TenantId: "t1", ClientId: "c"})
	require.Error(t, err)

	_, err = srv.CreateProvider(ctx, &iampb.CreateProviderRequest{TenantId: "t1", Name: "test"})
	require.Error(t, err)
}

func TestFederationServer_CreateProvider_ServiceError(t *testing.T) {
	srv, mgr := newTestFederationServer(t)

	mgr.On("RegisterProvider", mock.Anything, mock.Anything).
		Return(&errors.QBError{Code: errors.ErrAlreadyExists, Op: "RegisterProvider", Err: stderrors.New("exists")})

	_, err := srv.CreateProvider(context.Background(), &iampb.CreateProviderRequest{
		TenantId: "t1", Name: "google", ClientId: "c1",
	})

	st, _ := status.FromError(err)
	assert.Equal(t, codes.AlreadyExists, st.Code())
	mgr.AssertExpectations(t)
}

func TestFederationServer_GetProvider_Success(t *testing.T) {
	srv, mgr := newTestFederationServer(t)

	mgr.On("GetProvider", mock.Anything, "t1", "p1").
		Return(&iam.ExternalProvider{ID: "p1", TenantID: "t1", Name: "google"}, nil)

	resp, err := srv.GetProvider(context.Background(), &iampb.GetProviderRequest{TenantId: "t1", ProviderId: "p1"})

	require.NoError(t, err)
	assert.Equal(t, "p1", resp.Id)
	mgr.AssertExpectations(t)
}

func TestFederationServer_GetProvider_MissingFields(t *testing.T) {
	srv, _ := newTestFederationServer(t)

	_, err := srv.GetProvider(context.Background(), &iampb.GetProviderRequest{ProviderId: "p1"})
	require.Error(t, err)

	_, err = srv.GetProvider(context.Background(), &iampb.GetProviderRequest{TenantId: "t1"})
	require.Error(t, err)
}

func TestFederationServer_UpdateProvider_Success(t *testing.T) {
	srv, mgr := newTestFederationServer(t)

	mgr.On("UpdateProvider", mock.Anything, mock.AnythingOfType("*iam.ExternalProvider")).Return(nil)
	mgr.On("GetProvider", mock.Anything, "t1", "p1").
		Return(&iam.ExternalProvider{ID: "p1", TenantID: "t1", Name: "updated", Type: iam.ProviderTypeOIDC, ClientID: "existing-client", IssuerURL: "https://issuer.example.com"}, nil)

	resp, err := srv.UpdateProvider(context.Background(), &iampb.UpdateProviderRequest{
		TenantId:   "t1",
		ProviderId: "p1",
		Name:       "updated",
		Type:       iampb.ProviderType_PROVIDER_TYPE_SAML,
		Enabled:    true,
		ClaimMappings: &iampb.ClaimMappings{
			Subject: "sub",
			Email:   "mail",
		},
	})

	require.NoError(t, err)
	assert.Equal(t, "p1", resp.Id)
	mgr.AssertExpectations(t)
}

func TestFederationServer_UpdateProvider_MissingFields(t *testing.T) {
	srv, _ := newTestFederationServer(t)

	_, err := srv.UpdateProvider(context.Background(), &iampb.UpdateProviderRequest{ProviderId: "p1"})
	require.Error(t, err)

	_, err = srv.UpdateProvider(context.Background(), &iampb.UpdateProviderRequest{TenantId: "t1"})
	require.Error(t, err)
}

func TestFederationServer_DeleteProvider_Success(t *testing.T) {
	srv, mgr := newTestFederationServer(t)

	mgr.On("DeleteProvider", mock.Anything, "t1", "p1").Return(nil)

	resp, err := srv.DeleteProvider(context.Background(), &iampb.DeleteProviderRequest{TenantId: "t1", ProviderId: "p1"})

	require.NoError(t, err)
	assert.True(t, resp.Success)
	mgr.AssertExpectations(t)
}

func TestFederationServer_DeleteProvider_MissingFields(t *testing.T) {
	srv, _ := newTestFederationServer(t)

	_, err := srv.DeleteProvider(context.Background(), &iampb.DeleteProviderRequest{ProviderId: "p1"})
	require.Error(t, err)

	_, err = srv.DeleteProvider(context.Background(), &iampb.DeleteProviderRequest{TenantId: "t1"})
	require.Error(t, err)
}

func TestFederationServer_ListProviders_Success(t *testing.T) {
	srv, mgr := newTestFederationServer(t)

	mgr.On("ListProviders", mock.Anything, "t1", mock.AnythingOfType("iam.ListOptions")).
		Return([]*iam.ExternalProvider{
			{ID: "p1", TenantID: "t1", Name: "google"},
			{ID: "p2", TenantID: "t1", Name: "github"},
		}, nil)

	resp, err := srv.ListProviders(context.Background(), &iampb.ListProvidersRequest{TenantId: "t1"})

	require.NoError(t, err)
	assert.Len(t, resp.Providers, 2)
	assert.Equal(t, int32(2), resp.Total)
	mgr.AssertExpectations(t)
}

func TestFederationServer_ListProviders_MissingTenantID(t *testing.T) {
	srv, _ := newTestFederationServer(t)

	_, err := srv.ListProviders(context.Background(), &iampb.ListProvidersRequest{})
	require.Error(t, err)
}

// =============================================================================
// Authentication Flow Tests
// =============================================================================

func TestFederationServer_BeginFederatedLogin_Success(t *testing.T) {
	srv, mgr := newTestFederationServer(t)

	mgr.On("BeginLogin", mock.Anything, "t1", "google", "http://localhost/callback").
		Return(&iam.LoginRedirect{
			AuthURL:   "https://accounts.google.com/auth?client_id=xyz",
			State:     "state-token-123",
			ExpiresAt: time.Now().Add(10 * time.Minute),
		}, nil)

	resp, err := srv.BeginFederatedLogin(context.Background(), &iampb.BeginFederatedLoginRequest{
		TenantId:     "t1",
		ProviderName: "google",
		RedirectUri:  "http://localhost/callback",
	})

	require.NoError(t, err)
	assert.Contains(t, resp.AuthUrl, "accounts.google.com")
	assert.Equal(t, "state-token-123", resp.State)
	assert.NotNil(t, resp.ExpiresAt)
	mgr.AssertExpectations(t)
}

func TestFederationServer_BeginFederatedLogin_MissingFields(t *testing.T) {
	srv, _ := newTestFederationServer(t)

	_, err := srv.BeginFederatedLogin(context.Background(), &iampb.BeginFederatedLoginRequest{ProviderName: "google"})
	require.Error(t, err)

	_, err = srv.BeginFederatedLogin(context.Background(), &iampb.BeginFederatedLoginRequest{TenantId: "t1"})
	require.Error(t, err)
}

func TestFederationServer_HandleFederatedCallback_Success(t *testing.T) {
	srv, mgr := newTestFederationServer(t)

	mgr.On("HandleCallback", mock.Anything, "state-123", "auth-code-456").
		Return(&iam.FederationResult{
			User:              &iam.User{ID: "u1", Email: "user@example.com"},
			FederatedIdentity: &iam.FederatedIdentity{ID: "fi1", ExternalID: "ext-123"},
			AccessToken:       "access-token",
			RefreshToken:      "refresh-token",
			ExpiresIn:         3600,
			Provisioned:       true,
		}, nil)

	resp, err := srv.HandleFederatedCallback(context.Background(), &iampb.HandleFederatedCallbackRequest{
		State: "state-123",
		Code:  "auth-code-456",
	})

	require.NoError(t, err)
	assert.NotNil(t, resp.User)
	assert.Equal(t, "u1", resp.User.Id)
	assert.Equal(t, "access-token", resp.AccessToken)
	assert.True(t, resp.Provisioned)
	mgr.AssertExpectations(t)
}

func TestFederationServer_HandleFederatedCallback_MissingFields(t *testing.T) {
	srv, _ := newTestFederationServer(t)

	_, err := srv.HandleFederatedCallback(context.Background(), &iampb.HandleFederatedCallbackRequest{Code: "code"})
	require.Error(t, err)

	_, err = srv.HandleFederatedCallback(context.Background(), &iampb.HandleFederatedCallbackRequest{State: "state"})
	require.Error(t, err)
}

// =============================================================================
// Account Linking Tests
// =============================================================================

func TestFederationServer_LinkAccount_Success(t *testing.T) {
	srv, mgr := newTestFederationServer(t)

	mgr.On("LinkAccount", mock.Anything, "u1", "t1", "p1", "ext-token").Return(nil)

	resp, err := srv.LinkAccount(context.Background(), &iampb.LinkAccountRequest{
		UserId: "u1", TenantId: "t1", ProviderId: "p1", ExternalToken: "ext-token",
	})

	require.NoError(t, err)
	assert.True(t, resp.Success)
	mgr.AssertExpectations(t)
}

func TestFederationServer_LinkAccount_MissingFields(t *testing.T) {
	srv, _ := newTestFederationServer(t)
	ctx := context.Background()

	_, err := srv.LinkAccount(ctx, &iampb.LinkAccountRequest{TenantId: "t1", ProviderId: "p1", ExternalToken: "x"})
	require.Error(t, err)

	_, err = srv.LinkAccount(ctx, &iampb.LinkAccountRequest{UserId: "u1", ProviderId: "p1", ExternalToken: "x"})
	require.Error(t, err)

	_, err = srv.LinkAccount(ctx, &iampb.LinkAccountRequest{UserId: "u1", TenantId: "t1", ExternalToken: "x"})
	require.Error(t, err)

	_, err = srv.LinkAccount(ctx, &iampb.LinkAccountRequest{UserId: "u1", TenantId: "t1", ProviderId: "p1"})
	require.Error(t, err)
}

func TestFederationServer_UnlinkAccount_Success(t *testing.T) {
	srv, mgr := newTestFederationServer(t)

	mgr.On("UnlinkAccount", mock.Anything, "u1", "t1", "p1").Return(nil)

	resp, err := srv.UnlinkAccount(context.Background(), &iampb.UnlinkAccountRequest{
		UserId: "u1", TenantId: "t1", ProviderId: "p1",
	})

	require.NoError(t, err)
	assert.True(t, resp.Success)
	mgr.AssertExpectations(t)
}

func TestFederationServer_UnlinkAccount_MissingFields(t *testing.T) {
	srv, _ := newTestFederationServer(t)
	ctx := context.Background()

	_, err := srv.UnlinkAccount(ctx, &iampb.UnlinkAccountRequest{TenantId: "t1", ProviderId: "p1"})
	require.Error(t, err)

	_, err = srv.UnlinkAccount(ctx, &iampb.UnlinkAccountRequest{UserId: "u1", ProviderId: "p1"})
	require.Error(t, err)

	_, err = srv.UnlinkAccount(ctx, &iampb.UnlinkAccountRequest{UserId: "u1", TenantId: "t1"})
	require.Error(t, err)
}

func TestFederationServer_GetUserLinks_Success(t *testing.T) {
	srv, mgr := newTestFederationServer(t)

	mgr.On("GetUserLinks", mock.Anything, "u1", "t1").
		Return([]*iam.FederatedIdentity{
			{ID: "fi1", UserID: "u1", ProviderID: "p1", ExternalID: "ext-1", Claims: map[string]interface{}{"email": "user@example.com"}},
		}, nil)

	resp, err := srv.GetUserLinks(context.Background(), &iampb.GetUserLinksRequest{UserId: "u1", TenantId: "t1"})

	require.NoError(t, err)
	assert.Len(t, resp.Identities, 1)
	assert.Equal(t, "fi1", resp.Identities[0].Id)
	mgr.AssertExpectations(t)
}

func TestFederationServer_GetUserLinks_MissingFields(t *testing.T) {
	srv, _ := newTestFederationServer(t)

	_, err := srv.GetUserLinks(context.Background(), &iampb.GetUserLinksRequest{TenantId: "t1"})
	require.Error(t, err)

	_, err = srv.GetUserLinks(context.Background(), &iampb.GetUserLinksRequest{UserId: "u1"})
	require.Error(t, err)
}

func TestConvertExternalProviderToProto_Nil(t *testing.T) {
	result := convertExternalProviderToProto(nil)
	assert.Nil(t, result)
}

func TestConvertFederatedIdentityToProto_Nil(t *testing.T) {
	result := convertFederatedIdentityToProto(nil)
	assert.Nil(t, result)
}

func TestNewFederationServer(t *testing.T) {
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)

	mgr := &mockFederationManager{}
	srv := NewFederationServer(mgr, log)

	assert.NotNil(t, srv)
}
