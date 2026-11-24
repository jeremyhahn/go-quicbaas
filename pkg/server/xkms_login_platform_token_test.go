//go:build !integration

// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC

package server

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-jose/go-jose/v4"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/apimodule"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

const (
	platformTokenLoginRoute = "/api/v1/auth/login/piv"
	platformTokenOtherRoute = "/api/v1/auth/other"
	platformTokenModuleBody = `{"access_token":"tok-test","user":"admin"}`
)

// errPlatformTokenStubAuth is the stub module's bearer refusal.
var errPlatformTokenStubAuth = errors.New("stub module refuses every bearer")

// platformTokenBareModule is a ServiceModule with none of the extensions the
// exchange needs.
type platformTokenBareModule struct{}

func (m *platformTokenBareModule) Name() string                              { return "bare" }
func (m *platformTokenBareModule) RegisterGRPC(grpc.ServiceRegistrar)        {}
func (m *platformTokenBareModule) RegisterQUIC(grpc.ServiceRegistrar)        {}
func (m *platformTokenBareModule) RegisterHTTP(*qrdbsdk.ModuleHTTPRegistrar) {}
func (m *platformTokenBareModule) RegisterMCP(*mcpserver.MCPServer)          {}
func (m *platformTokenBareModule) Start(context.Context) error               { return nil }
func (m *platformTokenBareModule) Stop(context.Context) error                { return nil }

// platformTokenStubModule answers both of its public routes with a fixed
// status and body, and carries every extension the exchange forwards.
type platformTokenStubModule struct {
	platformTokenBareModule
	status int
	body   string
}

func (m *platformTokenStubModule) RegisterHTTP(r *qrdbsdk.ModuleHTTPRegistrar) {
	answer := func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Module", "stub")
		w.WriteHeader(m.status)
		_, _ = w.Write([]byte(m.body))
	}
	r.HandlePublic(http.MethodPost, platformTokenLoginRoute, answer)
	r.HandlePublic(http.MethodPost, platformTokenOtherRoute, answer)
}

func (m *platformTokenStubModule) MCPToolPermissions() map[string]qrdbsdk.Permission { return nil }
func (m *platformTokenStubModule) PublicGRPCMethods() []string                       { return nil }
func (m *platformTokenStubModule) AuthenticateHTTP(*http.Request) (*qrdbsdk.Identity, error) {
	return nil, errPlatformTokenStubAuth
}

// platformTokenRouter mounts the stub, wrapped by the exchange for srv, on a
// bare router the way the node's module router does.
func platformTokenRouter(t *testing.T, srv *Server, status int, body string) http.Handler {
	t.Helper()
	hosted, err := newXKMSLoginPlatformTokenModule(srv, &platformTokenStubModule{status: status, body: body},
		[]string{platformTokenLoginRoute}, nil)
	require.NoError(t, err)
	return moduleRouter(hosted)
}

func postPlatformTokenRoute(router http.Handler, path string, tlsState *tls.ConnectionState) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, nil)
	req.TLS = tlsState
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestXKMSLoginPlatformToken_RefusesModuleWithoutExtensions(t *testing.T) {
	_, err := newXKMSLoginPlatformTokenModule(&Server{}, nil, nil, nil)
	var nilModule *apimodule.NilModuleError
	assert.True(t, errors.As(err, &nilModule), "nil module: got %v", err)

	_, err = newXKMSLoginPlatformTokenModule(nil, &platformTokenStubModule{}, nil, nil)
	var nilServer *XKMSLoginPlatformTokenNilServerError
	assert.True(t, errors.As(err, &nilServer), "nil server: got %v", err)

	_, err = newXKMSLoginPlatformTokenModule(&Server{}, &platformTokenBareModule{}, nil, nil)
	var missing *apimodule.ModuleExtensionMissingError
	assert.True(t, errors.As(err, &missing), "bare module: got %v", err)
}

func TestXKMSLoginPlatformToken_PassesNon200Through(t *testing.T) {
	router := platformTokenRouter(t, &Server{}, http.StatusUnauthorized, `{"error":"no"}`)

	rec := postPlatformTokenRoute(router, platformTokenLoginRoute, nil)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Equal(t, `{"error":"no"}`, rec.Body.String())
	assert.Equal(t, "stub", rec.Header().Get("X-Module"))
}

func TestXKMSLoginPlatformToken_LeavesOtherRoutesUnwrapped(t *testing.T) {
	router := platformTokenRouter(t, &Server{}, http.StatusOK, platformTokenModuleBody)

	rec := postPlatformTokenRoute(router, platformTokenOtherRoute, nil)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, platformTokenModuleBody, rec.Body.String())
}

func TestXKMSLoginPlatformToken_NoPeerCertificateNoTokens(t *testing.T) {
	router := platformTokenRouter(t, &Server{}, http.StatusOK, platformTokenModuleBody)

	rec := postPlatformTokenRoute(router, platformTokenLoginRoute, nil)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.NotContains(t, rec.Body.String(), "access_token")
	assert.NotContains(t, rec.Body.String(), "tok-test")
}

func TestXKMSLoginPlatformToken_NoIAMNoTokens(t *testing.T) {
	router := platformTokenRouter(t, &Server{}, http.StatusOK, platformTokenModuleBody)
	leaf := &x509.Certificate{Raw: []byte("leaf")}

	rec := postPlatformTokenRoute(router, platformTokenLoginRoute,
		&tls.ConnectionState{PeerCertificates: []*x509.Certificate{leaf}})

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.NotContains(t, rec.Body.String(), "access_token")
	assert.NotContains(t, rec.Body.String(), "tok-test")
}

// TestXKMSLoginPlatformToken_NoUserNoTokens is the fail-closed refusal with
// IAM attached: the module accepted the login, but no IAM user holds the
// presented certificate, so the exchange answers 401 and none of the
// module's tokens reach the client.
func TestXKMSLoginPlatformToken_NoUserNoTokens(t *testing.T) {
	srv, _, _ := newOperatorTLSIntegrationNode(t)
	router := platformTokenRouter(t, srv, http.StatusOK, platformTokenModuleBody)
	leaf := buildSelfSignedRoot(t, "nobody")

	rec := postPlatformTokenRoute(router, platformTokenLoginRoute,
		&tls.ConnectionState{PeerCertificates: []*x509.Certificate{leaf}})

	assert.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
	assert.NotContains(t, rec.Body.String(), "access_token")
	assert.NotContains(t, rec.Body.String(), "tok-test")
}

func TestXKMSLoginPlatformToken_BearerFallbackDefersToModule(t *testing.T) {
	hosted, err := newXKMSLoginPlatformTokenModule(&Server{}, &platformTokenStubModule{}, nil, nil)
	require.NoError(t, err)
	fallback, ok := hosted.(qrdbsdk.ModuleAuthFallback)
	require.True(t, ok)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer tok-test")
	_, err = fallback.AuthenticateHTTP(req)

	assert.ErrorIs(t, err, errPlatformTokenStubAuth)
}

const (
	passkeyLoginFinishRoute = "/api/v1/webauthn/login/finish"
	passkeySDKFinishRoute   = "/api/v1/webauthn/sdk/authentication/finish"
	passkeyModuleToken      = "xkms-token-test"
	passkeyModuleRefresh    = "xkms-refresh-test"
	passkeyUsername         = "passkey-user-test"
	passkeySDKBody          = `{"success":true,"token":"xkms-token-test","username":"passkey-user-test","message":"ok"}`
	passkeyLoginBody        = `{"token":"xkms-token-test","refresh_token":"xkms-refresh-test"}`
)

// passkeyStubModule answers both go-xkms passkey login routes with a fixed
// status and body, and accepts its own token as a bearer only when identity
// is set, the way go-xkms's fallback accepts its passkey token.
type passkeyStubModule struct {
	platformTokenStubModule
	identity *qrdbsdk.Identity
}

func (m *passkeyStubModule) RegisterHTTP(r *qrdbsdk.ModuleHTTPRegistrar) {
	answer := func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Module", "stub")
		w.WriteHeader(m.status)
		_, _ = w.Write([]byte(m.body))
	}
	r.HandlePublic(http.MethodPost, passkeyLoginFinishRoute, answer)
	r.HandlePublic(http.MethodPost, passkeySDKFinishRoute, answer)
}

func (m *passkeyStubModule) AuthenticateHTTP(req *http.Request) (*qrdbsdk.Identity, error) {
	if m.identity == nil || req.Header.Get("Authorization") != "Bearer "+passkeyModuleToken {
		return nil, errPlatformTokenStubAuth
	}
	return m.identity, nil
}

// passkeyUserHandle encodes id the way go-xkms's qrdb user store does: the
// qrdb user id as 8 big-endian bytes, base64url without padding.
func passkeyUserHandle(id uint64) string {
	var raw [8]byte
	binary.BigEndian.PutUint64(raw[:], id)
	return base64.RawURLEncoding.EncodeToString(raw[:])
}

// passkeyModuleIdentity is the identity go-xkms's fallback returns for its
// passkey token.
func passkeyModuleIdentity(id uint64, subjectKind string) *qrdbsdk.Identity {
	return &qrdbsdk.Identity{
		Subject:  passkeyUserHandle(id),
		TenantID: xkmsModuleWebAuthnTenantID,
		Metadata: map[string]string{"subject_kind": subjectKind, "username": passkeyUsername},
	}
}

// newPasskeyLoginNode builds a node with IAM and one system-tenant user.
func newPasskeyLoginNode(t *testing.T) (*Server, service.IAMComponents, *iam.User) {
	t.Helper()
	srv, components, _ := newOperatorTLSIntegrationNode(t)
	user, err := components.GetIdentityStore().CreateUser(context.Background(), qrdbsdk.SystemTenantID, iam.CreateUserOptions{
		Email:    "passkey-user-test@example.com",
		Username: passkeyUsername,
	})
	require.NoError(t, err)
	require.NotNil(t, user)
	return srv, components, user
}

// passkeyRouter mounts the passkey stub, wrapped by the exchange for srv.
func passkeyRouter(t *testing.T, srv *Server, stub *passkeyStubModule) http.Handler {
	t.Helper()
	hosted, err := newXKMSLoginPlatformTokenModule(srv, stub, []string{platformTokenLoginRoute}, xkmsPasskeyLoginRoutes)
	require.NoError(t, err)
	return moduleRouter(hosted)
}

// verifyAgainstJWKS checks that token is signed under a kid the provider's
// JWKS publishes and returns its verified claims.
func verifyAgainstJWKS(t *testing.T, provider *iam.OIDCProvider, token string) map[string]any {
	t.Helper()
	jws, err := jose.ParseSigned(token, []jose.SignatureAlgorithm{jose.RS256, jose.ES256, jose.ES384, jose.EdDSA})
	require.NoError(t, err)
	require.Len(t, jws.Signatures, 1)
	kid := jws.Signatures[0].Header.KeyID
	keys := provider.GetJWKS().Key(kid)
	require.NotEmpty(t, keys, "the token's kid %q must be in the node's JWKS", kid)
	payload, err := jws.Verify(keys[0])
	require.NoError(t, err, "the token must verify against the JWKS key")
	var claims map[string]any
	require.NoError(t, json.Unmarshal(payload, &claims))
	return claims
}

// assertPasskeyPlatformToken checks that token is a node-issued platform
// token for a passkey login: verified against the JWKS, amr webauthn, no acr.
func assertPasskeyPlatformToken(t *testing.T, components service.IAMComponents, body string) {
	t.Helper()
	header := accessTokenHeader(t, body)
	var kids []string
	for _, key := range components.GetOIDCProvider().GetJWKS().Keys {
		kids = append(kids, key.KeyID)
	}
	assert.Contains(t, kids, header.Kid, "the access token's kid must be in the node's JWKS")

	var tokens struct {
		AccessToken string `json:"access_token"`
		IDToken     string `json:"id_token"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &tokens))
	provider := components.GetOIDCProvider()
	access := verifyAgainstJWKS(t, provider, tokens.AccessToken)
	_, hasACR := access["acr"]
	assert.False(t, hasACR, "a passkey exchange claims no acr")

	// The provider carries the login's amr on the ID token.
	id := verifyAgainstJWKS(t, provider, tokens.IDToken)
	assert.Equal(t, []any{"webauthn"}, id["amr"])
	_, hasACR = id["acr"]
	assert.False(t, hasACR, "a passkey exchange claims no acr")
}

func TestXKMSLoginPlatformToken_PasskeyLoginReturnsPlatformTokens(t *testing.T) {
	srv, components, user := newPasskeyLoginNode(t)

	t.Run("sdk authentication finish", func(t *testing.T) {
		router := passkeyRouter(t, srv, &passkeyStubModule{
			platformTokenStubModule: platformTokenStubModule{status: http.StatusOK, body: passkeySDKBody},
			identity:                passkeyModuleIdentity(user.ID, "user_handle"),
		})

		rec := postPlatformTokenRoute(router, passkeySDKFinishRoute, nil)

		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var body map[string]any
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		assert.Equal(t, true, body["success"])
		assert.Equal(t, passkeyUsername, body["username"])
		assert.Equal(t, "ok", body["message"])
		assert.Equal(t, body["access_token"], body["token"])
		assert.NotEqual(t, passkeyModuleToken, body["token"])
		assert.Equal(t, "stub", rec.Header().Get("X-Module"))
		assertPasskeyPlatformToken(t, components, rec.Body.String())
	})

	t.Run("login finish", func(t *testing.T) {
		router := passkeyRouter(t, srv, &passkeyStubModule{
			platformTokenStubModule: platformTokenStubModule{status: http.StatusOK, body: passkeyLoginBody},
			identity:                passkeyModuleIdentity(user.ID, "user_handle"),
		})

		rec := postPlatformTokenRoute(router, passkeyLoginFinishRoute, nil)

		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var body map[string]any
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		assert.Equal(t, body["access_token"], body["token"])
		assert.NotEqual(t, passkeyModuleToken, body["token"])
		assert.NotEqual(t, passkeyModuleRefresh, body["refresh_token"])
		assert.NotContains(t, rec.Body.String(), passkeyModuleToken)
		assert.NotContains(t, rec.Body.String(), passkeyModuleRefresh)
		assertPasskeyPlatformToken(t, components, rec.Body.String())
	})
}
