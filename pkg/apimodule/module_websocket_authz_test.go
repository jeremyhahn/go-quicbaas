//go:build rest

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

package apimodule

import (
	"context"
	"crypto/fips140"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-qrdb/pkg/auth/middleware"
	"github.com/jeremyhahn/go-qrdb/pkg/auth/rbac"
	"github.com/jeremyhahn/go-qrdb/pkg/permission"
	"github.com/jeremyhahn/go-qrdb/pkg/user"

	restapi "github.com/jeremyhahn/go-quicbaas/pkg/api/rest"
	"github.com/jeremyhahn/go-quicbaas/pkg/api/websocket/subprotocol"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

const webSocketPattern = "/api/v1/ws"

// webSocketHandler returns the module's GET /api/v1/ws route behind the
// production go-qrdb authn/authz middleware, with the messaging WebSocket
// server mounted the way buildRESTServer mounts it.
func webSocketHandler(t *testing.T) http.Handler {
	t.Helper()

	wsSrv, err := subprotocol.NewMessagingServer(newTestLogger(t))
	if fips140.Enforced() {
		require.Error(t, err)
		t.Skip("FIPS-only mode refuses the WebSocket server")
	}
	require.NoError(t, err)
	t.Cleanup(func() { _ = wsSrv.Close() })

	restSrv, err := restapi.NewServer(testutil.NewMockQuicBaaS(), restapi.DefaultConfig(), newTestLogger(t))
	require.NoError(t, err)
	restSrv.SetModuleTokenAuthorizer(&fakeTokenAuthorizer{tokens: map[string]fakeToken{
		"subscribe-token": {subject: "svc-app", tenant: "tenant-a", scopes: []string{"messaging:subscribe"}},
		"watch-token":     {subject: "svc-app", tenant: "tenant-a", scopes: []string{"messaging:watch"}},
	}})
	restSrv.SetWebSocketHandler(wsSrv)
	m := newHTTPModule(t, func() *restapi.Server { return restSrv })

	registrar, recs := newCaptureRegistrar()
	m.RegisterHTTP(registrar)
	var route *captured
	for i := range *recs {
		rec := &(*recs)[i]
		if rec.method == http.MethodGet && rec.pattern == webSocketPattern {
			route = rec
		}
	}
	require.NotNil(t, route, "the module does not register GET %s", webSocketPattern)
	require.Equal(t, "messaging:subscribe", string(route.perm))

	am := middleware.New(rbac.New(&fakeRoleServicer{}),
		middleware.WithAuthenticator(neverAuthenticates{}),
		middleware.WithUserService(&fakeUserServicer{users: map[string]*user.User{}}),
	)
	routes := middleware.NewRESTRouteRegistry()
	routes.Register(http.MethodGet, webSocketPattern, permission.Permission(route.perm))
	routes.SetFallback(http.MethodGet, webSocketPattern, m)
	return am.RESTMiddleware(routes, route.handler)
}

// webSocketURL serves the upgrade route and returns its ws:// URL.
func webSocketURL(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(webSocketHandler(t))
	t.Cleanup(srv.Close)
	return "ws" + strings.TrimPrefix(srv.URL, "http") + webSocketPattern
}

func dialWebSocket(t *testing.T, url string, protocols []string, header http.Header) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return websocket.Dial(ctx, url, &websocket.DialOptions{Subprotocols: protocols, HTTPHeader: header})
}

func bearer(token string) http.Header {
	return http.Header{"Authorization": {"Bearer " + token}}
}

// TestWebSocket_DialEachSubprotocol: a caller holding messaging:subscribe
// upgrades through the production middleware and negotiates each subprotocol.
func TestWebSocket_DialEachSubprotocol(t *testing.T) {
	url := webSocketURL(t)
	for _, name := range []string{"mqtt", "amqp", "stomp"} {
		t.Run(name, func(t *testing.T) {
			conn, resp, err := dialWebSocket(t, url, []string{name}, bearer("subscribe-token"))
			require.NoError(t, err)
			require.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)
			require.Equal(t, name, conn.Subprotocol())
			_ = conn.Close(websocket.StatusNormalClosure, "")
		})
	}
}

// TestWebSocket_UpgradeRequiresCredential: an upgrade without a credential is
// refused with 401 before the WebSocket server runs.
func TestWebSocket_UpgradeRequiresCredential(t *testing.T) {
	_, resp, err := dialWebSocket(t, webSocketURL(t), []string{"mqtt"}, nil)
	require.Error(t, err)
	require.NotNil(t, resp)
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

// TestWebSocket_UpgradeRequiresSubscribeGrant: a credential without
// messaging:subscribe is refused with 403.
func TestWebSocket_UpgradeRequiresSubscribeGrant(t *testing.T) {
	_, resp, err := dialWebSocket(t, webSocketURL(t), []string{"mqtt"}, bearer("watch-token"))
	require.Error(t, err)
	require.NotNil(t, resp)
	require.Equal(t, http.StatusForbidden, resp.StatusCode)
}

// TestWebSocket_UnknownSubprotocolRefused: an authorized upgrade asking only
// for an unsupported subprotocol is refused with 400.
func TestWebSocket_UnknownSubprotocolRefused(t *testing.T) {
	_, resp, err := dialWebSocket(t, webSocketURL(t), []string{"xmpp"}, bearer("subscribe-token"))
	require.Error(t, err)
	require.NotNil(t, resp)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}
