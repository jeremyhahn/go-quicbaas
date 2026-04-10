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

package auth

import (
	"context"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

const (
	serviceName = "quicbaas.Auth"
)

// Client provides authentication and authorization operations.
//
// The client supports multiple authentication methods:
// - Username/password authentication with JWT tokens
// - Token refresh and validation
// - OIDC/OAuth2 federated authentication
// - Session management
type Client interface {
	// JWT Token Operations

	// Login authenticates a user with username and password, returning JWT tokens.
	Login(ctx context.Context, req *service.ExtendedLoginRequest) (*service.ExtendedLoginResponse, error)

	// RefreshToken exchanges a refresh token for a new access token.
	RefreshToken(ctx context.Context, refreshToken string) (*service.ExtendedRefreshTokenResponse, error)

	// ValidateToken validates an access token and returns the claims.
	ValidateToken(ctx context.Context, accessToken string) (*service.TokenClaims, error)

	// Logout revokes the access and refresh tokens, ending the session.
	Logout(ctx context.Context, accessToken string) error

	// OIDC/Federation Operations

	// BeginFederatedLogin initiates an OIDC/OAuth2 authentication flow.
	// Returns an authorization URL to redirect the user to the identity provider.
	BeginFederatedLogin(ctx context.Context, req *service.BeginFederatedLoginRequest) (*service.BeginFederatedLoginResponse, error)

	// HandleFederatedCallback handles the callback from an identity provider.
	// Exchanges the authorization code for tokens and creates/links user account.
	HandleFederatedCallback(ctx context.Context, req *service.HandleFederatedCallbackRequest) (*service.HandleFederatedCallbackResponse, error)

	// ListProviders returns the list of configured identity providers.
	ListProviders(ctx context.Context, tenantID string) (*service.AuthListProvidersResponse, error)

	// GetProvider returns details about a specific identity provider.
	GetProvider(ctx context.Context, tenantID, providerID string) (*service.AuthProvider, error)

	// Session Management

	// GetSession returns information about a specific session.
	GetSession(ctx context.Context, sessionID string) (*service.AuthSession, error)

	// ListSessions returns all active sessions for the current user.
	ListSessions(ctx context.Context) (*service.AuthListSessionsResponse, error)

	// RevokeSession revokes a specific session.
	RevokeSession(ctx context.Context, sessionID string) error

	// RevokeAllSessions revokes all sessions for the current user.
	RevokeAllSessions(ctx context.Context) error

	// Token Introspection (for service-to-service validation)

	// IntrospectToken returns detailed information about a token.
	// This is typically used by resource servers to validate tokens.
	IntrospectToken(ctx context.Context, token string) (*service.TokenIntrospection, error)

	// JWKS Operations

	// GetJWKS returns the JSON Web Key Set for token verification.
	GetJWKS(ctx context.Context) (*service.JWKS, error)

	// OIDC Discovery

	// GetOpenIDConfiguration returns the OIDC discovery document.
	// This provides endpoints and capabilities of the authentication server.
	GetOpenIDConfiguration(ctx context.Context) (*service.OpenIDConfiguration, error)
}

// Internal request/response types for transport - these are for non-core auth methods.

type getSessionRequest struct {
	SessionID string `json:"session_id"`
}

type listSessionsRequest struct{}

type revokeSessionRequest struct {
	SessionID string `json:"session_id"`
}

type revokeSessionResponse struct{}

type revokeAllSessionsRequest struct{}

type revokeAllSessionsResponse struct{}

type introspectTokenRequest struct {
	Token string `json:"token"`
}

type getJWKSRequest struct{}

type getOpenIDConfigurationRequest struct{}

type listProvidersRequest struct {
	TenantID string `json:"tenant_id"`
}

type getProviderRequest struct {
	TenantID   string `json:"tenant_id"`
	ProviderID string `json:"provider_id"`
}

// client implements the Client interface.
type client struct {
	transport transport.Transport
}

// NewClient creates a new Auth client.
//
// Returns nil if the transport is nil.
func NewClient(t transport.Transport) Client {
	if t == nil {
		return nil
	}
	return &client{
		transport: t,
	}
}

// Login authenticates a user with username and password.
func (c *client) Login(ctx context.Context, req *service.ExtendedLoginRequest) (*service.ExtendedLoginResponse, error) {
	if err := validateLoginRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("auth.Login", "context is required")
	}

	// Pass the ExtendedLoginRequest directly to the transport.
	// The gRPC auth handler knows how to convert this to protobuf types.
	resp, err := c.transport.Request(ctx, serviceName, "Login", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("auth.Login", "nil response from server")
	}

	loginResp, ok := resp.(*service.ExtendedLoginResponse)
	if !ok {
		return nil, errors.NewInternal("auth.Login", "invalid response type")
	}

	return loginResp, nil
}

// RefreshToken exchanges a refresh token for a new access token.
func (c *client) RefreshToken(ctx context.Context, refreshToken string) (*service.ExtendedRefreshTokenResponse, error) {
	if refreshToken == "" {
		return nil, errors.NewInvalidInput("auth.RefreshToken", "refresh token is required")
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("auth.RefreshToken", "context is required")
	}

	// Use the service type that the gRPC handler recognizes.
	resp, err := c.transport.Request(ctx, serviceName, "RefreshToken", &service.RefreshTokenRequest{
		RefreshToken: refreshToken,
	})
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("auth.RefreshToken", "nil response from server")
	}

	refreshResp, ok := resp.(*service.ExtendedRefreshTokenResponse)
	if !ok {
		return nil, errors.NewInternal("auth.RefreshToken", "invalid response type")
	}

	return refreshResp, nil
}

// ValidateToken validates an access token and returns the claims.
func (c *client) ValidateToken(ctx context.Context, accessToken string) (*service.TokenClaims, error) {
	if accessToken == "" {
		return nil, errors.NewInvalidInput("auth.ValidateToken", "access token is required")
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("auth.ValidateToken", "context is required")
	}

	// Use the service type that the gRPC handler recognizes.
	resp, err := c.transport.Request(ctx, serviceName, "ValidateToken", &service.ValidateTokenRequest{
		AccessToken: accessToken,
	})
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("auth.ValidateToken", "nil response from server")
	}

	claims, ok := resp.(*service.TokenClaims)
	if !ok {
		return nil, errors.NewInternal("auth.ValidateToken", "invalid response type")
	}

	return claims, nil
}

// Logout revokes the access and refresh tokens.
func (c *client) Logout(ctx context.Context, accessToken string) error {
	if accessToken == "" {
		return errors.NewInvalidInput("auth.Logout", "access token is required")
	}

	if ctx == nil {
		return errors.NewInvalidInput("auth.Logout", "context is required")
	}

	// Use the service type that the gRPC handler recognizes.
	_, err := c.transport.Request(ctx, serviceName, "Logout", &service.LogoutRequest{
		AccessToken: accessToken,
	})
	if err != nil {
		return err
	}

	return nil
}

// BeginFederatedLogin initiates an OIDC/OAuth2 authentication flow.
func (c *client) BeginFederatedLogin(ctx context.Context, req *service.BeginFederatedLoginRequest) (*service.BeginFederatedLoginResponse, error) {
	if err := validateBeginFederatedLoginRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("auth.BeginFederatedLogin", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "BeginFederatedLogin", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("auth.BeginFederatedLogin", "nil response from server")
	}

	beginResp, ok := resp.(*service.BeginFederatedLoginResponse)
	if !ok {
		return nil, errors.NewInternal("auth.BeginFederatedLogin", "invalid response type")
	}

	return beginResp, nil
}

// HandleFederatedCallback handles the callback from an identity provider.
func (c *client) HandleFederatedCallback(ctx context.Context, req *service.HandleFederatedCallbackRequest) (*service.HandleFederatedCallbackResponse, error) {
	if err := validateHandleFederatedCallbackRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("auth.HandleFederatedCallback", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "HandleFederatedCallback", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("auth.HandleFederatedCallback", "nil response from server")
	}

	callbackResp, ok := resp.(*service.HandleFederatedCallbackResponse)
	if !ok {
		return nil, errors.NewInternal("auth.HandleFederatedCallback", "invalid response type")
	}

	return callbackResp, nil
}

// ListProviders returns the list of configured identity providers.
func (c *client) ListProviders(ctx context.Context, tenantID string) (*service.AuthListProvidersResponse, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("auth.ListProviders", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "ListProviders", &listProvidersRequest{
		TenantID: tenantID,
	})
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return &service.AuthListProvidersResponse{Providers: []service.AuthProvider{}}, nil
	}

	listResp, ok := resp.(*service.AuthListProvidersResponse)
	if !ok {
		return nil, errors.NewInternal("auth.ListProviders", "invalid response type")
	}

	return listResp, nil
}

// GetProvider returns details about a specific identity provider.
func (c *client) GetProvider(ctx context.Context, tenantID, providerID string) (*service.AuthProvider, error) {
	if providerID == "" {
		return nil, errors.NewInvalidInput("auth.GetProvider", "provider ID is required")
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("auth.GetProvider", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "GetProvider", &getProviderRequest{
		TenantID:   tenantID,
		ProviderID: providerID,
	})
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewNotFound("auth.GetProvider", "provider not found")
	}

	provider, ok := resp.(*service.AuthProvider)
	if !ok {
		return nil, errors.NewInternal("auth.GetProvider", "invalid response type")
	}

	return provider, nil
}

// GetSession returns information about a specific session.
func (c *client) GetSession(ctx context.Context, sessionID string) (*service.AuthSession, error) {
	if sessionID == "" {
		return nil, errors.NewInvalidInput("auth.GetSession", "session ID is required")
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("auth.GetSession", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "GetSession", &getSessionRequest{
		SessionID: sessionID,
	})
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewNotFound("auth.GetSession", "session not found")
	}

	session, ok := resp.(*service.AuthSession)
	if !ok {
		return nil, errors.NewInternal("auth.GetSession", "invalid response type")
	}

	return session, nil
}

// ListSessions returns all active sessions for the current user.
func (c *client) ListSessions(ctx context.Context) (*service.AuthListSessionsResponse, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("auth.ListSessions", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "ListSessions", &listSessionsRequest{})
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return &service.AuthListSessionsResponse{Sessions: []service.AuthSession{}}, nil
	}

	listResp, ok := resp.(*service.AuthListSessionsResponse)
	if !ok {
		return nil, errors.NewInternal("auth.ListSessions", "invalid response type")
	}

	return listResp, nil
}

// RevokeSession revokes a specific session.
func (c *client) RevokeSession(ctx context.Context, sessionID string) error {
	if sessionID == "" {
		return errors.NewInvalidInput("auth.RevokeSession", "session ID is required")
	}

	if ctx == nil {
		return errors.NewInvalidInput("auth.RevokeSession", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "RevokeSession", &revokeSessionRequest{
		SessionID: sessionID,
	})
	if err != nil {
		return err
	}

	// Accept nil response or valid response
	if resp == nil {
		return nil
	}

	if _, ok := resp.(*revokeSessionResponse); !ok {
		return errors.NewInternal("auth.RevokeSession", "invalid response type")
	}

	return nil
}

// RevokeAllSessions revokes all sessions for the current user.
func (c *client) RevokeAllSessions(ctx context.Context) error {
	if ctx == nil {
		return errors.NewInvalidInput("auth.RevokeAllSessions", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "RevokeAllSessions", &revokeAllSessionsRequest{})
	if err != nil {
		return err
	}

	// Accept nil response or valid response
	if resp == nil {
		return nil
	}

	if _, ok := resp.(*revokeAllSessionsResponse); !ok {
		return errors.NewInternal("auth.RevokeAllSessions", "invalid response type")
	}

	return nil
}

// IntrospectToken returns detailed information about a token.
func (c *client) IntrospectToken(ctx context.Context, token string) (*service.TokenIntrospection, error) {
	if token == "" {
		return nil, errors.NewInvalidInput("auth.IntrospectToken", "token is required")
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("auth.IntrospectToken", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "IntrospectToken", &introspectTokenRequest{
		Token: token,
	})
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("auth.IntrospectToken", "nil response from server")
	}

	introspection, ok := resp.(*service.TokenIntrospection)
	if !ok {
		return nil, errors.NewInternal("auth.IntrospectToken", "invalid response type")
	}

	return introspection, nil
}

// GetJWKS returns the JSON Web Key Set for token verification.
func (c *client) GetJWKS(ctx context.Context) (*service.JWKS, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("auth.GetJWKS", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "GetJWKS", &getJWKSRequest{})
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("auth.GetJWKS", "nil response from server")
	}

	jwks, ok := resp.(*service.JWKS)
	if !ok {
		return nil, errors.NewInternal("auth.GetJWKS", "invalid response type")
	}

	return jwks, nil
}

// GetOpenIDConfiguration returns the OIDC discovery document.
func (c *client) GetOpenIDConfiguration(ctx context.Context) (*service.OpenIDConfiguration, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("auth.GetOpenIDConfiguration", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "GetOpenIDConfiguration", &getOpenIDConfigurationRequest{})
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("auth.GetOpenIDConfiguration", "nil response from server")
	}

	config, ok := resp.(*service.OpenIDConfiguration)
	if !ok {
		return nil, errors.NewInternal("auth.GetOpenIDConfiguration", "invalid response type")
	}

	return config, nil
}

// Validation functions

func validateLoginRequest(req *service.ExtendedLoginRequest) error {
	if req == nil {
		return errors.NewInvalidInput("auth.Login", "request is required")
	}
	if req.Username == "" {
		return errors.NewInvalidInput("auth.Login", "username is required")
	}
	if req.Password == "" {
		return errors.NewInvalidInput("auth.Login", "password is required")
	}
	return nil
}

func validateBeginFederatedLoginRequest(req *service.BeginFederatedLoginRequest) error {
	if req == nil {
		return errors.NewInvalidInput("auth.BeginFederatedLogin", "request is required")
	}
	if req.ProviderID == "" {
		return errors.NewInvalidInput("auth.BeginFederatedLogin", "provider ID is required")
	}
	if req.RedirectURI == "" {
		return errors.NewInvalidInput("auth.BeginFederatedLogin", "redirect URI is required")
	}
	return nil
}

func validateHandleFederatedCallbackRequest(req *service.HandleFederatedCallbackRequest) error {
	if req == nil {
		return errors.NewInvalidInput("auth.HandleFederatedCallback", "request is required")
	}
	if req.ProviderID == "" {
		return errors.NewInvalidInput("auth.HandleFederatedCallback", "provider ID is required")
	}
	if req.Code == "" {
		return errors.NewInvalidInput("auth.HandleFederatedCallback", "authorization code is required")
	}
	if req.State == "" {
		return errors.NewInvalidInput("auth.HandleFederatedCallback", "state is required")
	}
	return nil
}
