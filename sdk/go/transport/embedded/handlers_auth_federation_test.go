// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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
	"crypto/rand"
	"crypto/rsa"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	authclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/auth"
)

// authTestService is a QuicBaaS double exposing the three managers the Auth
// federation, session and discovery handlers resolve structurally.
type authTestService struct {
	service.QuicBaaS
	federation iam.FederationManager
	sessions   service.SessionManager
	oidc       *iam.OIDCProvider
}

func (s *authTestService) GetFederationManager(_ context.Context) (iam.FederationManager, error) {
	return s.federation, nil
}

func (s *authTestService) GetSessionManager(_ context.Context) (service.SessionManager, error) {
	return s.sessions, nil
}

func (s *authTestService) GetOIDCProvider(_ context.Context) (*iam.OIDCProvider, error) {
	return s.oidc, nil
}

// fakeFederationManager serves a fixed provider set and records the login
// arguments it was given.
type fakeFederationManager struct {
	iam.FederationManager
	providers   map[string]*iam.ExternalProvider
	beginName   string
	beginTenant string
	callbackArg [2]string
}

func (f *fakeFederationManager) GetProvider(_ context.Context, tenantID, providerID string) (*iam.ExternalProvider, error) {
	p, ok := f.providers[providerID]
	if !ok || p.TenantID != tenantID {
		return nil, iam.ErrProviderNotFound
	}
	return p, nil
}

func (f *fakeFederationManager) ListProviders(_ context.Context, tenantID string, _ iam.ListOptions) ([]*iam.ExternalProvider, error) {
	out := []*iam.ExternalProvider{}
	for _, p := range f.providers {
		if p.TenantID == tenantID {
			out = append(out, p)
		}
	}
	return out, nil
}

func (f *fakeFederationManager) BeginLogin(_ context.Context, tenantID, providerName, _ string) (*iam.LoginRedirect, error) {
	f.beginTenant, f.beginName = tenantID, providerName
	return &iam.LoginRedirect{AuthURL: "https://idp.example.com/authorize", State: "state-1"}, nil
}

func (f *fakeFederationManager) HandleCallback(_ context.Context, state, code string) (*iam.FederationResult, error) {
	f.callbackArg = [2]string{state, code}
	if state != "state-1" {
		return nil, iam.ErrStateInvalid
	}
	return &iam.FederationResult{
		User:         &iam.User{ID: 42, Username: "alice", Email: "alice@example.com"},
		AccessToken:  "access",
		RefreshToken: "refresh",
		ExpiresIn:    3600,
		Provisioned:  true,
	}, nil
}

func newFederationManager() *fakeFederationManager {
	return &fakeFederationManager{providers: map[string]*iam.ExternalProvider{
		"prov-1": {
			ID: "prov-1", TenantID: "t-1", Name: "corp-idp", Type: iam.ProviderTypeOIDC,
			Enabled: true, IssuerURL: "https://idp.example.com", ClientSecret: "never-copied",
			Scopes: []string{"openid"},
		},
	}}
}

// newAuthClient returns the real SDK auth client over an embedded transport
// backed by svc.
func newAuthClient(t *testing.T, svc service.QuicBaaS) authclient.Client {
	t.Helper()
	tr, err := New(svc)
	require.NoError(t, err)
	return authclient.NewClient(tr)
}

// unconfiguredAuthClient is backed by a service exposing none of the managers.
func unconfiguredAuthClient(t *testing.T) authclient.Client {
	t.Helper()
	return newAuthClient(t, &mockQuicBaaS{})
}

func requireNotConfigured(t *testing.T, err error) {
	t.Helper()
	var notConfigured *ServiceNotConfiguredError
	require.ErrorAs(t, err, &notConfigured)
}

func TestAuthFederationDispatchRegistered(t *testing.T) {
	for _, method := range []string{
		"BeginFederatedLogin", "HandleFederatedCallback", "ListProviders", "GetProvider",
		"GetSession", "ListSessions", "RevokeSession", "RevokeAllSessions",
		"GetJWKS", "GetOpenIDConfiguration",
	} {
		if _, ok := unaryHandlers["quicbaas.Auth/"+method]; !ok {
			t.Errorf("quicbaas.Auth/%s is not registered in the embedded dispatch table", method)
		}
	}
}

func TestEmbeddedAuth_BeginFederatedLogin(t *testing.T) {
	fed := newFederationManager()
	client := newAuthClient(t, &authTestService{federation: fed})

	resp, err := client.BeginFederatedLogin(context.Background(), &service.BeginFederatedLoginRequest{
		TenantID: "t-1", ProviderID: "prov-1", RedirectURI: "https://app.example.com/cb",
	})
	require.NoError(t, err)
	assert.Equal(t, "https://idp.example.com/authorize", resp.AuthorizationURL)
	assert.Equal(t, "state-1", resp.State)
	// The SDK names the provider by ID; the manager must be given its name.
	assert.Equal(t, "corp-idp", fed.beginName)
	assert.Equal(t, "t-1", fed.beginTenant)
}

func TestEmbeddedAuth_BeginFederatedLogin_Errors(t *testing.T) {
	req := &service.BeginFederatedLoginRequest{TenantID: "t-1", ProviderID: "missing", RedirectURI: "https://app/cb"}

	_, err := newAuthClient(t, &authTestService{federation: newFederationManager()}).BeginFederatedLogin(context.Background(), req)
	require.ErrorIs(t, err, iam.ErrProviderNotFound)

	_, err = unconfiguredAuthClient(t).BeginFederatedLogin(context.Background(), req)
	requireNotConfigured(t, err)

	_, err = handleBeginFederatedLogin(context.Background(), &authTestService{}, "wrong")
	var invalid *InvalidRequestTypeError
	require.ErrorAs(t, err, &invalid)
}

func TestEmbeddedAuth_HandleFederatedCallback(t *testing.T) {
	fed := newFederationManager()
	client := newAuthClient(t, &authTestService{federation: fed})

	resp, err := client.HandleFederatedCallback(context.Background(), &service.HandleFederatedCallbackRequest{
		ProviderID: "prov-1", Code: "code-1", State: "state-1",
	})
	require.NoError(t, err)
	assert.Equal(t, [2]string{"state-1", "code-1"}, fed.callbackArg)
	assert.Equal(t, "access", resp.AccessToken)
	assert.Equal(t, "refresh", resp.RefreshToken)
	assert.Equal(t, "Bearer", resp.TokenType)
	assert.Equal(t, "42", resp.UserID)
	assert.Equal(t, "alice", resp.Username)
	assert.Equal(t, "alice@example.com", resp.Email)
	assert.True(t, resp.NewUser)
	assert.WithinDuration(t, time.Now().Add(time.Hour), resp.ExpiresAt, time.Minute)
}

func TestEmbeddedAuth_HandleFederatedCallback_Errors(t *testing.T) {
	req := &service.HandleFederatedCallbackRequest{ProviderID: "prov-1", Code: "code-1", State: "forged"}

	_, err := newAuthClient(t, &authTestService{federation: newFederationManager()}).HandleFederatedCallback(context.Background(), req)
	require.ErrorIs(t, err, iam.ErrStateInvalid)

	_, err = unconfiguredAuthClient(t).HandleFederatedCallback(context.Background(), req)
	requireNotConfigured(t, err)
}

func TestEmbeddedAuth_ListProviders(t *testing.T) {
	client := newAuthClient(t, &authTestService{federation: newFederationManager()})

	resp, err := client.ListProviders(tenantT1(), "t-1")
	require.NoError(t, err)
	require.Len(t, resp.Providers, 1)
	assert.Equal(t, "corp-idp", resp.Providers[0].Name)
	assert.Equal(t, service.ProviderTypeOIDC, resp.Providers[0].Type)

	// An empty tenant means the caller's own tenant.
	own, err := client.ListProviders(tenantT1(), "")
	require.NoError(t, err)
	assert.Len(t, own.Providers, 1)
}

func TestEmbeddedAuth_ListProviders_NotConfigured(t *testing.T) {
	_, err := unconfiguredAuthClient(t).ListProviders(tenantT1(), "t-1")
	requireNotConfigured(t, err)
}

func TestEmbeddedAuth_GetProvider(t *testing.T) {
	client := newAuthClient(t, &authTestService{federation: newFederationManager()})

	p, err := client.GetProvider(tenantT1(), "t-1", "prov-1")
	require.NoError(t, err)
	assert.Equal(t, "prov-1", p.ID)
	assert.Equal(t, "https://idp.example.com", p.IssuerURL)
	assert.Equal(t, []string{"openid"}, p.Scopes)
}

func TestEmbeddedAuth_GetProvider_Errors(t *testing.T) {
	_, err := newAuthClient(t, &authTestService{federation: newFederationManager()}).GetProvider(tenantT1(), "t-1", "missing")
	require.ErrorIs(t, err, iam.ErrProviderNotFound)

	_, err = handleAuthGetProvider(context.Background(), &authTestService{}, "wrong")
	var invalid *InvalidRequestTypeError
	require.ErrorAs(t, err, &invalid)
}

// tenantT1 is a call bound to tenant t-1, the tenant the provider double holds.
func tenantT1() context.Context {
	return tenantctx.WithTenant(context.Background(), "t-1")
}

// TestEmbeddedAuth_ProviderVerbs_RefuseOtherTenant proves a caller bound to
// one tenant cannot list or read another tenant's identity providers: the
// federation store does not authorize, so the transport must.
func TestEmbeddedAuth_ProviderVerbs_RefuseOtherTenant(t *testing.T) {
	fed := newFederationManager()
	fed.providers["prov-2"] = &iam.ExternalProvider{ID: "prov-2", TenantID: "t-2", Name: "victim-idp"}
	client := newAuthClient(t, &authTestService{federation: fed})
	tenantT2 := tenantctx.WithTenant(context.Background(), "t-2")

	_, err := client.ListProviders(tenantT1(), "t-2")
	assert.True(t, isCrossTenant(err), "ListProviders naming another tenant: got %v", err)
	_, err = client.GetProvider(tenantT1(), "t-2", "prov-2")
	assert.True(t, isCrossTenant(err), "GetProvider naming another tenant: got %v", err)

	// The owning tenant still reads its own provider.
	p, err := client.GetProvider(tenantT2, "t-2", "prov-2")
	require.NoError(t, err)
	assert.Equal(t, "victim-idp", p.Name)
}

// TestEmbeddedAuth_TypedNilRequest proves a typed-nil request pointer is
// refused as an invalid request instead of panicking on its fields.
func TestEmbeddedAuth_TypedNilRequest(t *testing.T) {
	svc := &authTestService{federation: newFederationManager()}
	handlers := map[string]handlerFunc{
		"BeginFederatedLogin":     handleBeginFederatedLogin,
		"HandleFederatedCallback": handleHandleFederatedCallback,
	}
	requests := map[string]interface{}{
		"BeginFederatedLogin":     (*service.BeginFederatedLoginRequest)(nil),
		"HandleFederatedCallback": (*service.HandleFederatedCallbackRequest)(nil),
	}
	for name, handler := range handlers {
		t.Run(name, func(t *testing.T) {
			err, recovered := callRecovering(func() error {
				_, err := handler(tenantT1(), svc, requests[name])
				return err
			})
			require.Nil(t, recovered, "handler panicked on a typed-nil request")
			var invalid *InvalidRequestTypeError
			require.ErrorAs(t, err, &invalid)
		})
	}
}

func subjectContext(userID, tenantID string) context.Context {
	return auth.SubjectIntoContext(context.Background(), interfaces.Subject{ID: userID, TenantID: tenantID})
}

func TestEmbeddedAuth_GetSession(t *testing.T) {
	svc, mgr := newSessionTestService(t)
	seedSession(t, mgr, "sess-1", 10001, "t-1")
	client := newAuthClient(t, &authTestService{sessions: svc.sessions})

	s, err := client.GetSession(subjectContext("10001", "t-1"), "sess-1")
	require.NoError(t, err)
	assert.Equal(t, "sess-1", s.ID)
	assert.Equal(t, "10001", s.UserID)
	assert.Equal(t, "workstation", s.DeviceInfo)
	assert.Equal(t, "mtls", s.AuthMethod)
}

func TestEmbeddedAuth_GetSession_Errors(t *testing.T) {
	svc, mgr := newSessionTestService(t)
	seedSession(t, mgr, "sess-1", 10001, "t-1")
	client := newAuthClient(t, &authTestService{sessions: svc.sessions})

	_, err := client.GetSession(context.Background(), "missing")
	require.Error(t, err)

	// Another subject's session is denied, not served.
	_, err = client.GetSession(subjectContext("20002", "t-1"), "sess-1")
	var denied *PermissionDeniedError
	require.ErrorAs(t, err, &denied)

	_, err = unconfiguredAuthClient(t).GetSession(context.Background(), "sess-1")
	requireNotConfigured(t, err)
}

func TestEmbeddedAuth_ListSessions(t *testing.T) {
	svc, mgr := newSessionTestService(t)
	seedSession(t, mgr, "sess-1", 10001, "t-1")
	seedSession(t, mgr, "sess-2", 20002, "t-1")
	client := newAuthClient(t, &authTestService{sessions: svc.sessions})

	resp, err := client.ListSessions(subjectContext("10001", "t-1"))
	require.NoError(t, err)
	require.Len(t, resp.Sessions, 1)
	assert.Equal(t, "sess-1", resp.Sessions[0].ID)
}

func TestEmbeddedAuth_ListSessions_Errors(t *testing.T) {
	svc, _ := newSessionTestService(t)
	client := newAuthClient(t, &authTestService{sessions: svc.sessions})

	// Without a subject there is no current user to list for.
	_, err := client.ListSessions(context.Background())
	var denied *PermissionDeniedError
	require.ErrorAs(t, err, &denied)

	_, err = unconfiguredAuthClient(t).ListSessions(subjectContext("10001", "t-1"))
	requireNotConfigured(t, err)
}

func TestEmbeddedAuth_RevokeSession(t *testing.T) {
	svc, mgr := newSessionTestService(t)
	seedSession(t, mgr, "sess-1", 10001, "t-1")
	client := newAuthClient(t, &authTestService{sessions: svc.sessions})

	require.NoError(t, client.RevokeSession(subjectContext("10001", "t-1"), "sess-1"))
	_, err := mgr.Get(context.Background(), "sess-1")
	require.Error(t, err, "revoked session must be gone")
}

func TestEmbeddedAuth_RevokeSession_Errors(t *testing.T) {
	svc, mgr := newSessionTestService(t)
	seedSession(t, mgr, "sess-1", 10001, "t-1")
	client := newAuthClient(t, &authTestService{sessions: svc.sessions})

	err := client.RevokeSession(subjectContext("20002", "t-1"), "sess-1")
	var denied *PermissionDeniedError
	require.ErrorAs(t, err, &denied)
	_, err = mgr.Get(context.Background(), "sess-1")
	require.NoError(t, err, "a denied revoke must leave the session in place")

	requireNotConfigured(t, unconfiguredAuthClient(t).RevokeSession(context.Background(), "sess-1"))
}

func TestEmbeddedAuth_RevokeAllSessions(t *testing.T) {
	svc, mgr := newSessionTestService(t)
	seedSession(t, mgr, "sess-1", 10001, "t-1")
	seedSession(t, mgr, "sess-2", 10001, "t-1")
	seedSession(t, mgr, "sess-3", 20002, "t-1")
	client := newAuthClient(t, &authTestService{sessions: svc.sessions})

	require.NoError(t, client.RevokeAllSessions(subjectContext("10001", "t-1")))
	mine, err := mgr.List(context.Background(), 10001, "t-1")
	require.NoError(t, err)
	assert.Empty(t, mine)
	others, err := mgr.List(context.Background(), 20002, "t-1")
	require.NoError(t, err)
	assert.Len(t, others, 1)
}

func TestEmbeddedAuth_RevokeAllSessions_Errors(t *testing.T) {
	svc, _ := newSessionTestService(t)
	err := newAuthClient(t, &authTestService{sessions: svc.sessions}).RevokeAllSessions(context.Background())
	var denied *PermissionDeniedError
	require.ErrorAs(t, err, &denied)

	requireNotConfigured(t, unconfiguredAuthClient(t).RevokeAllSessions(subjectContext("10001", "t-1")))
}

// stubAppStorage and stubTokenStorage satisfy the OIDC provider's required
// dependencies; the discovery verbs never reach them.
type stubAppStorage struct{ iam.AppStorage }

type stubTokenStorage struct{ iam.TokenStorage }

func newTestOIDCProvider(t *testing.T) *iam.OIDCProvider {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	keys, err := iam.NewSigningKeyManagerFromSigner(key, "RS256")
	require.NoError(t, err)
	mem := iam.NewMemoryStorage()
	provider, err := iam.NewOIDCProvider(&iam.OIDCConfig{
		Issuer:            "https://issuer.example.com",
		SigningAlgorithm:  "RS256",
		SigningKeyManager: keys,
		AccessTokenTTL:    time.Hour,
		RefreshTokenTTL:   time.Hour,
		IDTokenTTL:        time.Hour,
		AuthCodeTTL:       time.Minute,
	}, iam.OIDCDependencies{
		IdentityStore: iam.NewIdentityStore(iam.IdentityStoreOptions{UserStorage: mem, RoleStorage: mem}),
		AppStorage:    stubAppStorage{},
		TokenStorage:  stubTokenStorage{},
	})
	require.NoError(t, err)
	return provider
}

func TestEmbeddedAuth_GetJWKS(t *testing.T) {
	client := newAuthClient(t, &authTestService{oidc: newTestOIDCProvider(t)})

	jwks, err := client.GetJWKS(context.Background())
	require.NoError(t, err)
	require.NotEmpty(t, jwks.Keys)
	assert.Equal(t, "RSA", jwks.Keys[0].KTY)
	assert.Equal(t, "RS256", jwks.Keys[0].ALG)
	assert.NotEmpty(t, jwks.Keys[0].KID)
	assert.NotEmpty(t, jwks.Keys[0].N)
	assert.Equal(t, "AQAB", jwks.Keys[0].E)
}

func TestEmbeddedAuth_GetJWKS_NotConfigured(t *testing.T) {
	_, err := unconfiguredAuthClient(t).GetJWKS(context.Background())
	requireNotConfigured(t, err)

	// A server that exposes the accessor but holds no provider is not configured either.
	_, err = newAuthClient(t, &authTestService{}).GetJWKS(context.Background())
	requireNotConfigured(t, err)
}

func TestEmbeddedAuth_GetOpenIDConfiguration(t *testing.T) {
	client := newAuthClient(t, &authTestService{oidc: newTestOIDCProvider(t)})

	cfg, err := client.GetOpenIDConfiguration(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "https://issuer.example.com", cfg.Issuer)
	assert.Equal(t, "https://issuer.example.com/.well-known/jwks.json", cfg.JWKSURI)
	assert.Equal(t, "https://issuer.example.com/oauth2/token", cfg.TokenEndpoint)
	assert.Contains(t, cfg.ScopesSupported, "openid")
	assert.Equal(t, []string{"S256"}, cfg.CodeChallengeMethodsSupported)
}

func TestEmbeddedAuth_GetOpenIDConfiguration_NotConfigured(t *testing.T) {
	_, err := unconfiguredAuthClient(t).GetOpenIDConfiguration(context.Background())
	requireNotConfigured(t, err)
}
