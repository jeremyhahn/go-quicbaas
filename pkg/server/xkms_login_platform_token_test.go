//go:build !integration

// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC

package server

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/apimodule"
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
		[]string{platformTokenLoginRoute})
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
	_, err := newXKMSLoginPlatformTokenModule(&Server{}, nil, nil)
	var nilModule *apimodule.NilModuleError
	assert.True(t, errors.As(err, &nilModule), "nil module: got %v", err)

	_, err = newXKMSLoginPlatformTokenModule(nil, &platformTokenStubModule{}, nil)
	var nilServer *XKMSLoginPlatformTokenNilServerError
	assert.True(t, errors.As(err, &nilServer), "nil server: got %v", err)

	_, err = newXKMSLoginPlatformTokenModule(&Server{}, &platformTokenBareModule{}, nil)
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

func TestXKMSLoginPlatformToken_BearerFallbackDefersToModule(t *testing.T) {
	hosted, err := newXKMSLoginPlatformTokenModule(&Server{}, &platformTokenStubModule{}, nil)
	require.NoError(t, err)
	fallback, ok := hosted.(qrdbsdk.ModuleAuthFallback)
	require.True(t, ok)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer tok-test")
	_, err = fallback.AuthenticateHTTP(req)

	assert.ErrorIs(t, err, errPlatformTokenStubAuth)
}
