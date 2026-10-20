// Copyright (c) 2025 Jeremy Hahn
// SPDX-License-Identifier: AGPL-3.0-or-later

package grpc

import (
	"context"
	"time"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
	sdktransport "github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// federatedTokenType is the token type the server issues on a federated
// callback; the RPC response does not carry it.
const federatedTokenType = "Bearer"

// providerPageSize is the page ListProviders requests; it matches the
// server's default limit.
const providerPageSize int32 = 100

// authProviderTypes maps the wire provider type to the SDK provider type.
var authProviderTypes = map[iampb.ProviderType]service.ProviderType{
	iampb.ProviderType_PROVIDER_TYPE_UNSPECIFIED: service.ProviderTypeUnspecified,
	iampb.ProviderType_PROVIDER_TYPE_OIDC:        service.ProviderTypeOIDC,
	iampb.ProviderType_PROVIDER_TYPE_SAML:        service.ProviderTypeSAML,
	iampb.ProviderType_PROVIDER_TYPE_OAUTH2:      service.ProviderTypeOAuth2,
}

// refuseAuthVerb returns a verb that refuses every call because the server
// has no RPC for it.
func refuseAuthVerb(method, reason string) authVerb {
	return func(_ *authHandler, _ context.Context, _ interface{}) (interface{}, error) {
		return nil, errors.NewUnimplemented("authHandler."+method, reason)
	}
}

// authTenant returns tenantID, or the tenant carried by ctx when it is empty.
func authTenant(ctx context.Context, tenantID string) string {
	if tenantID != "" {
		return tenantID
	}
	return sdktransport.TenantFromContext(ctx)
}

// authRequestMap reads an unexported auth client request through its JSON tags.
func authRequestMap(op string, req interface{}) (map[string]interface{}, error) {
	m, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput(op, "invalid request type")
	}
	return m, nil
}

func (h *authHandler) handleBeginFederatedLogin(ctx context.Context, req interface{}) (interface{}, error) {
	const op = "authHandler.handleBeginFederatedLogin"
	r, ok := req.(*service.BeginFederatedLoginRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput(op, "invalid request type, expected *service.BeginFederatedLoginRequest")
	}
	if r.State != "" || len(r.Scopes) > 0 || r.Prompt != "" || r.LoginHint != "" {
		return nil, errors.NewUnimplemented(op,
			"state, scopes, prompt and login hint have no gRPC field; the server chooses them")
	}

	tenantID := authTenant(ctx, r.TenantID)
	// Begin-login is public, so the server resolves the provider ID: an
	// anonymous caller would be refused the authorized GetProvider.
	resp, err := iampb.NewFederationServiceClient(h.conn).BeginFederatedLogin(ctx, &iampb.BeginFederatedLoginRequest{
		TenantId:    tenantID,
		ProviderId:  r.ProviderID,
		RedirectUri: r.RedirectURI,
	})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}

	return &service.BeginFederatedLoginResponse{
		AuthorizationURL: resp.GetAuthUrl(),
		State:            resp.GetState(),
	}, nil
}

func (h *authHandler) handleHandleFederatedCallback(ctx context.Context, req interface{}) (interface{}, error) {
	const op = "authHandler.handleHandleFederatedCallback"
	r, ok := req.(*service.HandleFederatedCallbackRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput(op, "invalid request type, expected *service.HandleFederatedCallbackRequest")
	}
	if r.RedirectURI != "" {
		return nil, errors.NewUnimplemented(op,
			"redirect URI has no gRPC field; the server binds it to the login state")
	}

	resp, err := iampb.NewFederationServiceClient(h.conn).HandleFederatedCallback(ctx,
		&iampb.HandleFederatedCallbackRequest{State: r.State, Code: r.Code})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}

	// The state binds the callback to a provider; refuse an answer for
	// another provider or tenant than the caller named.
	identity := resp.GetFederatedIdentity()
	if identity.GetProviderId() != r.ProviderID {
		return nil, errors.NewInvalidInput(op, "callback completed for a different provider")
	}
	if r.TenantID != "" && identity.GetTenantId() != r.TenantID {
		return nil, errors.NewInvalidInput(op, "callback completed for a different tenant")
	}

	out := &service.HandleFederatedCallbackResponse{
		AccessToken:  resp.GetAccessToken(),
		RefreshToken: resp.GetRefreshToken(),
		TokenType:    federatedTokenType,
		UserID:       resp.GetUser().GetId(),
		Username:     resp.GetUser().GetUsername(),
		Email:        resp.GetUser().GetEmail(),
		NewUser:      resp.GetProvisioned(),
	}
	if resp.GetExpiresIn() > 0 {
		out.ExpiresAt = time.Now().Add(time.Duration(resp.GetExpiresIn()) * time.Second)
	}
	return out, nil
}

func (h *authHandler) handleListProviders(ctx context.Context, req interface{}) (interface{}, error) {
	const op = "authHandler.handleListProviders"
	m, err := authRequestMap(op, req)
	if err != nil {
		return nil, err
	}
	tenantID, _ := getString(m, "tenant_id")
	tenantID = authTenant(ctx, tenantID)

	// The SDK method has no pagination, so fetch every page; a failed
	// page fails the call rather than returning a truncated list.
	client := iampb.NewFederationServiceClient(h.conn)
	providers := make([]service.AuthProvider, 0)
	for {
		resp, err := client.ListProviders(ctx, &iampb.ListProvidersRequest{
			TenantId: tenantID, Limit: providerPageSize, Offset: int32(len(providers)),
		})
		if err != nil {
			return nil, convertGRPCError(err, op)
		}
		if len(resp.GetProviders()) == 0 {
			break
		}
		for _, p := range resp.GetProviders() {
			providers = append(providers, authProviderFromProto(p))
		}
	}
	return &service.AuthListProvidersResponse{Providers: providers}, nil
}

func (h *authHandler) handleGetProvider(ctx context.Context, req interface{}) (interface{}, error) {
	const op = "authHandler.handleGetProvider"
	m, err := authRequestMap(op, req)
	if err != nil {
		return nil, err
	}
	tenantID, _ := getString(m, "tenant_id")
	providerID, _ := getString(m, "provider_id")

	resp, err := iampb.NewFederationServiceClient(h.conn).GetProvider(ctx, &iampb.GetProviderRequest{
		TenantId:   authTenant(ctx, tenantID),
		ProviderId: providerID,
	})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}

	provider := authProviderFromProto(resp)
	return &provider, nil
}

func (h *authHandler) handleGetSession(ctx context.Context, req interface{}) (interface{}, error) {
	const op = "authHandler.handleGetSession"
	m, err := authRequestMap(op, req)
	if err != nil {
		return nil, err
	}
	sessionID, _ := getString(m, "session_id")

	resp, err := iampb.NewSessionServiceClient(h.conn).GetSession(ctx,
		&iampb.GetSessionRequest{SessionId: sessionID})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}

	return &service.AuthSession{
		ID:          resp.GetId(),
		UserID:      resp.GetUserId(),
		TenantID:    resp.GetTenantId(),
		CreatedAt:   convertTimestampFromProto(resp.GetCreatedAt()),
		LastActive:  convertTimestampFromProto(resp.GetLastActive()),
		ExpiresAt:   convertTimestampFromProto(resp.GetExpiresAt()),
		MFAVerified: resp.GetMfaVerified(),
		IPAddress:   resp.GetIpAddress(),
		UserAgent:   resp.GetUserAgent(),
		DeviceInfo:  resp.GetDeviceInfo(),
	}, nil
}

func (h *authHandler) handleRevokeSession(ctx context.Context, req interface{}) (interface{}, error) {
	const op = "authHandler.handleRevokeSession"
	m, err := authRequestMap(op, req)
	if err != nil {
		return nil, err
	}
	sessionID, _ := getString(m, "session_id")

	if _, err := iampb.NewSessionServiceClient(h.conn).RevokeSession(ctx,
		&iampb.RevokeSessionRequest{SessionId: sessionID}); err != nil {
		return nil, convertGRPCError(err, op)
	}
	return nil, nil
}

// authProviderFromProto converts a wire provider to the SDK provider. The
// client ID, client secret and claim mappings have no SDK field.
func authProviderFromProto(p *iampb.ExternalProvider) service.AuthProvider {
	return service.AuthProvider{
		ID:            p.GetId(),
		TenantID:      p.GetTenantId(),
		Name:          p.GetName(),
		Type:          authProviderTypes[p.GetType()],
		Enabled:       p.GetEnabled(),
		IssuerURL:     p.GetIssuerUrl(),
		AuthURL:       p.GetAuthUrl(),
		TokenURL:      p.GetTokenUrl(),
		UserInfoURL:   p.GetUserinfoUrl(),
		Scopes:        p.GetScopes(),
		AutoProvision: p.GetAutoProvision(),
		AllowLinking:  p.GetAllowLinking(),
		DefaultRoles:  p.GetDefaultRoles(),
		CreatedAt:     convertTimestampFromProto(p.GetCreatedAt()),
		UpdatedAt:     convertTimestampFromProto(p.GetUpdatedAt()),
	}
}
