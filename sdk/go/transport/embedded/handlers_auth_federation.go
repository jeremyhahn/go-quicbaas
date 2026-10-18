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

package embedded

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// Auth federation, provider, session and discovery handlers
// (quicbaas.Auth/*).
//
// The auth SDK client has always carried these verbs, but the embedded route
// map had no key for any of them, so an in-process host got UnknownMethodError.
// Each handler resolves the same manager the REST and gRPC servers use —
// iam.FederationManager (pkg/api/grpc/server_iam_federation.go), the session
// manager (pkg/api/rest/iam_sessions.go) and the OIDC provider
// (pkg/api/rest/iam_oidc.go) — through the structural type assertions the
// rest of the embedded IAM dispatch uses, and only maps fields.

const authServiceName = "quicbaas.Auth"

// authFederationServiceName names the federation verbs in a
// not-configured error.
const authFederationServiceName = "Auth.Federation"

// federationManagerProvider is satisfied by the QuicBaaS service; the REST,
// gRPC and MCP transports reach the same federation manager through it.
type federationManagerProvider interface {
	GetFederationManager(ctx context.Context) (iam.FederationManager, error)
}

type oidcProviderSource interface {
	GetOIDCProvider(ctx context.Context) (*iam.OIDCProvider, error)
}

// federationManagerFor resolves the federation manager, reporting a server
// without one as not configured under the calling service's name.
func federationManagerFor(ctx context.Context, svc service.QuicBaaS, serviceName string) (iam.FederationManager, error) {
	p, ok := svc.(federationManagerProvider)
	if !ok {
		return nil, NewServiceNotConfiguredError(serviceName)
	}
	mgr, err := p.GetFederationManager(ctx)
	if err != nil {
		return nil, err
	}
	if mgr == nil {
		return nil, NewServiceNotConfiguredError(serviceName)
	}
	return mgr, nil
}

// oidcProviderFor resolves the OIDC provider, reporting a server without one
// as not configured.
func oidcProviderFor(ctx context.Context, svc service.QuicBaaS) (*iam.OIDCProvider, error) {
	p, ok := svc.(oidcProviderSource)
	if !ok {
		return nil, NewServiceNotConfiguredError("Auth.OIDC")
	}
	provider, err := p.GetOIDCProvider(ctx)
	if err != nil {
		return nil, err
	}
	if provider == nil {
		return nil, NewServiceNotConfiguredError("Auth.OIDC")
	}
	return provider, nil
}

// authSubject returns the authenticated subject a "current user" verb acts
// for. Without one there is no user to answer for, so the call is denied.
func authSubject(ctx context.Context, method string) (interfaces.Subject, error) {
	subject, bound := auth.SubjectFromContext(ctx)
	if !bound {
		return subject, NewPermissionDeniedError(authServiceName, method, "no authenticated subject")
	}
	return subject, nil
}

func handleBeginFederatedLogin(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*service.BeginFederatedLoginRequest)
	if !ok || r == nil {
		return nil, NewInvalidRequestTypeError(authServiceName, "BeginFederatedLogin",
			"*service.BeginFederatedLoginRequest", typeNameOf(req))
	}
	mgr, err := federationManagerFor(ctx, svc, authFederationServiceName)
	if err != nil {
		return nil, err
	}
	// The SDK names the provider by ID; the manager begins a login by name.
	provider, err := mgr.GetProvider(ctx, r.TenantID, r.ProviderID)
	if err != nil {
		return nil, err
	}
	redirect, err := mgr.BeginLogin(ctx, r.TenantID, provider.Name, r.RedirectURI)
	if err != nil {
		return nil, err
	}
	return &service.BeginFederatedLoginResponse{
		AuthorizationURL: redirect.AuthURL,
		State:            redirect.State,
	}, nil
}

func handleHandleFederatedCallback(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*service.HandleFederatedCallbackRequest)
	if !ok || r == nil {
		return nil, NewInvalidRequestTypeError(authServiceName, "HandleFederatedCallback",
			"*service.HandleFederatedCallbackRequest", typeNameOf(req))
	}
	mgr, err := federationManagerFor(ctx, svc, authFederationServiceName)
	if err != nil {
		return nil, err
	}
	result, err := mgr.HandleCallback(ctx, r.State, r.Code)
	if err != nil {
		return nil, err
	}
	resp := &service.HandleFederatedCallbackResponse{
		AccessToken:  result.AccessToken,
		RefreshToken: result.RefreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(result.ExpiresIn) * time.Second),
		TokenType:    "Bearer",
		NewUser:      result.Provisioned,
	}
	if result.User != nil {
		resp.UserID = strconv.FormatUint(result.User.ID, 10)
		resp.Username = result.User.Username
		resp.Email = result.User.Email
	}
	return resp, nil
}

func handleAuthListProviders(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	tenantID, ok := iamRequestField(req, "TenantID")
	if !ok {
		return nil, NewInvalidRequestTypeError(authServiceName, "ListProviders",
			"struct with TenantID field", typeNameOf(req))
	}
	tenantID, err := providerTenant(ctx, "ListProviders", tenantID)
	if err != nil {
		return nil, err
	}
	mgr, err := federationManagerFor(ctx, svc, authFederationServiceName)
	if err != nil {
		return nil, err
	}
	providers, err := mgr.ListProviders(ctx, tenantID, iam.ListOptions{})
	if err != nil {
		return nil, err
	}
	out := &service.AuthListProvidersResponse{Providers: make([]service.AuthProvider, 0, len(providers))}
	for _, p := range providers {
		if p == nil {
			continue
		}
		out.Providers = append(out.Providers, *authProviderToSDK(p))
	}
	return out, nil
}

func handleAuthGetProvider(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	providerID, ok := iamRequestField(req, "ProviderID")
	if !ok {
		return nil, NewInvalidRequestTypeError(authServiceName, "GetProvider",
			"struct with TenantID and ProviderID fields", typeNameOf(req))
	}
	tenantID, ok := iamRequestField(req, "TenantID")
	if !ok {
		return nil, NewInvalidRequestTypeError(authServiceName, "GetProvider",
			"struct with TenantID and ProviderID fields", typeNameOf(req))
	}
	tenantID, err := providerTenant(ctx, "GetProvider", tenantID)
	if err != nil {
		return nil, err
	}
	mgr, err := federationManagerFor(ctx, svc, authFederationServiceName)
	if err != nil {
		return nil, err
	}
	p, err := mgr.GetProvider(ctx, tenantID, providerID)
	if err != nil {
		return nil, err
	}
	return authProviderToSDK(p), nil
}

// providerTenant resolves the tenant a provider verb reads: the tenant the
// call is bound to. A request naming another tenant is refused, the same
// tenantctx.Target rule the gRPC federation server applies through
// requireCallerTenant, because the federation store does not authorize and
// would otherwise serve any tenant's providers.
func providerTenant(ctx context.Context, method, requested string) (string, error) {
	return tenantctx.Target(ctx, authServiceName+"/"+method, requested)
}

// providerTypes maps the federation store's provider type onto the SDK enum.
var providerTypes = map[iam.ProviderType]service.ProviderType{
	iam.ProviderTypeOIDC:   service.ProviderTypeOIDC,
	iam.ProviderTypeOAuth2: service.ProviderTypeOAuth2,
	iam.ProviderTypeSAML:   service.ProviderTypeSAML,
}

// authProviderToSDK maps a stored provider onto the SDK shape. The client
// secret is never copied: the SDK provider type has no field for it.
func authProviderToSDK(p *iam.ExternalProvider) *service.AuthProvider {
	return &service.AuthProvider{
		ID:            p.ID,
		TenantID:      p.TenantID,
		Name:          p.Name,
		Type:          providerTypes[p.Type],
		Enabled:       p.Enabled,
		IssuerURL:     p.IssuerURL,
		AuthURL:       p.AuthURL,
		TokenURL:      p.TokenURL,
		UserInfoURL:   p.UserInfoURL,
		Scopes:        p.Scopes,
		AutoProvision: p.AutoProvision,
		AllowLinking:  p.AllowLinking,
		DefaultRoles:  p.DefaultRoles,
		CreatedAt:     p.Created,
		UpdatedAt:     p.Updated,
	}
}

func handleAuthGetSession(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	id, ok := iamRequestField(req, "SessionID")
	if !ok {
		return nil, NewInvalidRequestTypeError(authServiceName, "GetSession",
			"struct with SessionID field", typeNameOf(req))
	}
	mgr, err := sessionManagerFor(ctx, svc)
	if err != nil {
		return nil, err
	}
	s, err := mgr.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if subject, bound := auth.SubjectFromContext(ctx); bound {
		if err := requireSessionOwner(subject, s, "GetSession"); err != nil {
			return nil, err
		}
	}
	return authSessionToSDK(s), nil
}

func handleAuthListSessions(ctx context.Context, svc service.QuicBaaS, _ interface{}) (interface{}, error) {
	subject, err := authSubject(ctx, "ListSessions")
	if err != nil {
		return nil, err
	}
	userID, err := parseIAMUserID(authServiceName, "ListSessions", subject.ID)
	if err != nil {
		return nil, err
	}
	mgr, err := sessionManagerFor(ctx, svc)
	if err != nil {
		return nil, err
	}
	sessions, err := mgr.List(ctx, userID, subject.TenantID)
	if err != nil {
		return nil, err
	}
	out := &service.AuthListSessionsResponse{Sessions: make([]service.AuthSession, 0, len(sessions))}
	for _, s := range sessions {
		if s == nil {
			continue
		}
		out.Sessions = append(out.Sessions, *authSessionToSDK(s))
	}
	return out, nil
}

// handleAuthRevokeSession answers nil on success: the client accepts a nil
// response and has no exported response type for this verb.
func handleAuthRevokeSession(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	id, ok := iamRequestField(req, "SessionID")
	if !ok {
		return nil, NewInvalidRequestTypeError(authServiceName, "RevokeSession",
			"struct with SessionID field", typeNameOf(req))
	}
	mgr, err := sessionManagerFor(ctx, svc)
	if err != nil {
		return nil, err
	}
	// Ownership is checked before the revoke: a denied caller must not be
	// able to destroy a session it was never allowed to see.
	if subject, bound := auth.SubjectFromContext(ctx); bound {
		s, getErr := mgr.Get(ctx, id)
		if getErr != nil {
			return nil, getErr
		}
		if err := requireSessionOwner(subject, s, "RevokeSession"); err != nil {
			return nil, err
		}
	}
	return nil, mgr.Revoke(ctx, id)
}

// handleAuthRevokeAllSessions revokes every session of the authenticated
// subject and, like RevokeSession, answers nil on success.
func handleAuthRevokeAllSessions(ctx context.Context, svc service.QuicBaaS, _ interface{}) (interface{}, error) {
	subject, err := authSubject(ctx, "RevokeAllSessions")
	if err != nil {
		return nil, err
	}
	userID, err := parseIAMUserID(authServiceName, "RevokeAllSessions", subject.ID)
	if err != nil {
		return nil, err
	}
	mgr, err := sessionManagerFor(ctx, svc)
	if err != nil {
		return nil, err
	}
	return nil, mgr.RevokeAllForUser(ctx, userID, subject.TenantID)
}

// authSessionToSDK maps a stored session onto the SDK shape. The token and
// refresh hashes are never copied.
func authSessionToSDK(s *iam.SessionExt) *service.AuthSession {
	out := &service.AuthSession{
		ID:          s.ID,
		UserID:      strconv.FormatUint(s.UserID, 10),
		TenantID:    s.TenantID,
		CreatedAt:   s.Created,
		LastActive:  s.LastActive,
		ExpiresAt:   s.ExpiresAt,
		MFAVerified: s.MFAVerified,
		IPAddress:   s.IPAddress,
		UserAgent:   s.UserAgent,
		AuthMethod:  s.Metadata["auth_method"],
	}
	if s.DeviceInfo != nil {
		out.DeviceInfo = s.DeviceInfo.Name
	}
	return out
}

func handleAuthGetJWKS(ctx context.Context, svc service.QuicBaaS, _ interface{}) (interface{}, error) {
	provider, err := oidcProviderFor(ctx, svc)
	if err != nil {
		return nil, err
	}
	out := &service.JWKS{Keys: []service.JWK{}}
	set := provider.GetJWKS()
	if set == nil {
		return out, nil
	}
	for _, key := range set.Keys {
		jwk, err := jwkToSDK(key)
		if err != nil {
			return nil, err
		}
		out.Keys = append(out.Keys, jwk)
	}
	return out, nil
}

// wireJWK is the RFC 7517 member set of a public JWK, the encoding the REST
// JWKS endpoint serves.
type wireJWK struct {
	KTY string   `json:"kty"`
	Use string   `json:"use"`
	KID string   `json:"kid"`
	ALG string   `json:"alg"`
	N   string   `json:"n"`
	E   string   `json:"e"`
	CRV string   `json:"crv"`
	X   string   `json:"x"`
	Y   string   `json:"y"`
	X5C []string `json:"x5c"`
	X5T string   `json:"x5t"`
}

// jwkToSDK takes the key's RFC 7517 members from its own JSON encoding, so
// the SDK sees exactly what the REST endpoint publishes.
func jwkToSDK(key json.Marshaler) (service.JWK, error) {
	raw, err := key.MarshalJSON()
	if err != nil {
		return service.JWK{}, NewJWKEncodeError(err)
	}
	var w wireJWK
	if err := json.Unmarshal(raw, &w); err != nil {
		return service.JWK{}, NewJWKEncodeError(err)
	}
	return service.JWK{
		KTY: w.KTY, Use: w.Use, KID: w.KID, ALG: w.ALG,
		N: w.N, E: w.E, CRV: w.CRV, X: w.X, Y: w.Y,
		X5C: w.X5C, X5T: w.X5T,
	}, nil
}

func handleAuthGetOpenIDConfiguration(ctx context.Context, svc service.QuicBaaS, _ interface{}) (interface{}, error) {
	provider, err := oidcProviderFor(ctx, svc)
	if err != nil {
		return nil, err
	}
	doc := provider.GetConfiguration()
	return &service.OpenIDConfiguration{
		Issuer:                            docString(doc, "issuer"),
		AuthorizationEndpoint:             docString(doc, "authorization_endpoint"),
		TokenEndpoint:                     docString(doc, "token_endpoint"),
		UserInfoEndpoint:                  docString(doc, "userinfo_endpoint"),
		JWKSURI:                           docString(doc, "jwks_uri"),
		RegistrationEndpoint:              docString(doc, "registration_endpoint"),
		RevocationEndpoint:                docString(doc, "revocation_endpoint"),
		IntrospectionEndpoint:             docString(doc, "introspection_endpoint"),
		ScopesSupported:                   docStrings(doc, "scopes_supported"),
		ResponseTypesSupported:            docStrings(doc, "response_types_supported"),
		GrantTypesSupported:               docStrings(doc, "grant_types_supported"),
		SubjectTypesSupported:             docStrings(doc, "subject_types_supported"),
		IDTokenSigningAlgValuesSupported:  docStrings(doc, "id_token_signing_alg_values_supported"),
		TokenEndpointAuthMethodsSupported: docStrings(doc, "token_endpoint_auth_methods_supported"),
		ClaimsSupported:                   docStrings(doc, "claims_supported"),
		CodeChallengeMethodsSupported:     docStrings(doc, "code_challenge_methods_supported"),
	}, nil
}

func docString(doc map[string]interface{}, key string) string {
	if s, ok := doc[key].(string); ok {
		return s
	}
	return ""
}

func docStrings(doc map[string]interface{}, key string) []string {
	if s, ok := doc[key].([]string); ok {
		return s
	}
	return nil
}
