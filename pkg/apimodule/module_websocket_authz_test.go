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
	"errors"
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
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

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

	wsSrv, err := subprotocol.NewTenantMessagingServer(newTestLogger(t))
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
		"publish-token":   {subject: "svc-publisher", tenant: "tenant-a", scopes: []string{"messaging:subscribe", "messaging:publish"}},
		"tenant-b-token":  {subject: "svc-publisher", tenant: "tenant-b", scopes: []string{"messaging:subscribe", "messaging:publish"}},
	}})
	restSrv.SetBarrierScopeAuthorizer(publisherAuthorizer{})
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

// dialWebSocket dials the upgrade route and returns the connection, the
// handshake response's status code (0 when there was no response) and the
// dial error. It closes the handshake response body.
func dialWebSocket(t *testing.T, url string, protocols []string, header http.Header) (*websocket.Conn, int, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	conn, resp, err := websocket.Dial(ctx, url, &websocket.DialOptions{Subprotocols: protocols, HTTPHeader: header})
	if resp == nil {
		return conn, 0, err
	}
	if resp.Body != nil {
		_ = resp.Body.Close()
	}
	return conn, resp.StatusCode, err
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
			conn, status, err := dialWebSocket(t, url, []string{name}, bearer("subscribe-token"))
			require.NoError(t, err)
			require.Equal(t, http.StatusSwitchingProtocols, status)
			require.Equal(t, name, conn.Subprotocol())
			_ = conn.Close(websocket.StatusNormalClosure, "")
		})
	}
}

// TestWebSocket_UpgradeRequiresCredential: an upgrade without a credential is
// refused with 401 before the WebSocket server runs.
func TestWebSocket_UpgradeRequiresCredential(t *testing.T) {
	_, status, err := dialWebSocket(t, webSocketURL(t), []string{"mqtt"}, nil)
	require.Error(t, err)
	require.Equal(t, http.StatusUnauthorized, status)
}

// TestWebSocket_UpgradeRequiresSubscribeGrant: a credential without
// messaging:subscribe is refused with 403.
func TestWebSocket_UpgradeRequiresSubscribeGrant(t *testing.T) {
	_, status, err := dialWebSocket(t, webSocketURL(t), []string{"mqtt"}, bearer("watch-token"))
	require.Error(t, err)
	require.Equal(t, http.StatusForbidden, status)
}

// TestWebSocket_UnknownSubprotocolRefused: an authorized upgrade asking only
// for an unsupported subprotocol is refused with 400.
func TestWebSocket_UnknownSubprotocolRefused(t *testing.T) {
	_, status, err := dialWebSocket(t, webSocketURL(t), []string{"xmpp"}, bearer("subscribe-token"))
	require.Error(t, err)
	require.Equal(t, http.StatusBadRequest, status)
}

// publisherAuthorizer is the scope authorizer the REST server consults for
// operations after the upgrade: it grants messaging:publish to svc-publisher
// only.
type publisherAuthorizer struct{}

func (publisherAuthorizer) Authorize(_ context.Context, identity *qrdbsdk.Identity, resource, action string) error {
	if identity != nil && identity.Subject == "svc-publisher" && resource == "messaging" && action == "publish" {
		return nil
	}
	return errors.New("forbidden")
}

func stompTestFrame(command string, headers map[string]string, body string) []byte {
	var b strings.Builder
	b.WriteString(command + "\n")
	for k, v := range headers {
		b.WriteString(k + ":" + v + "\n")
	}
	b.WriteString("\n" + body)
	b.WriteByte(0)
	return []byte(b.String())
}

// stompConnect dials url as token and completes the STOMP CONNECT.
func stompConnect(t *testing.T, url, token string) *websocket.Conn {
	t.Helper()
	conn, status, err := dialWebSocket(t, url, []string{"stomp"}, bearer(token))
	require.NoError(t, err)
	require.Equal(t, http.StatusSwitchingProtocols, status)
	t.Cleanup(func() { _ = conn.Close(websocket.StatusNormalClosure, "") })
	stompRoundTrip(t, conn, stompTestFrame("CONNECT", map[string]string{"accept-version": "1.2", "host": "localhost"}, ""), "CONNECTED")
	return conn
}

// stompRoundTrip writes frame and requires the reply to start with want.
func stompRoundTrip(t *testing.T, conn *websocket.Conn, frame []byte, want string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, conn.Write(ctx, websocket.MessageText, frame))
	_, data, err := conn.Read(ctx)
	require.NoError(t, err)
	require.Truef(t, strings.HasPrefix(string(data), want), "got %q, want a %s frame", data, want)
}

// stompSubscribe connects as token and subscribes to destination.
func stompSubscribe(t *testing.T, url, token, destination string) *websocket.Conn {
	t.Helper()
	conn := stompConnect(t, url, token)
	stompRoundTrip(t, conn, stompTestFrame("SUBSCRIBE", map[string]string{"id": "sub-1", "destination": destination, "receipt": "r-sub"}, ""), "RECEIPT")
	return conn
}

// stompPublish connects as token, sends body to destination and requires
// the reply to start with want (RECEIPT or ERROR).
func stompPublish(t *testing.T, url, token, destination, body, want string) {
	t.Helper()
	conn := stompConnect(t, url, token)
	stompRoundTrip(t, conn, stompTestFrame("SEND", map[string]string{"destination": destination, "receipt": "r-send"}, body), want)
}

// readMessage reads one frame from conn within wait.
func readMessage(conn *websocket.Conn, wait time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	_, data, err := conn.Read(ctx)
	return string(data), err
}

func TestWebSocket_PublishNeedsPublishPermission(t *testing.T) {
	url := webSocketURL(t)
	sub := stompSubscribe(t, url, "subscribe-token", "/topic/orders")

	stompPublish(t, url, "subscribe-token", "/topic/orders", "refused-body", "ERROR")
	msg, err := readMessage(sub, 500*time.Millisecond)
	require.Errorf(t, err, "a send without messaging:publish was delivered: %q", msg)
}

func TestWebSocket_PublishWithPublishPermissionIsDelivered(t *testing.T) {
	url := webSocketURL(t)
	sub := stompSubscribe(t, url, "subscribe-token", "/topic/orders")

	stompPublish(t, url, "publish-token", "/topic/orders", "granted-body", "RECEIPT")
	msg, err := readMessage(sub, 5*time.Second)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(msg, "MESSAGE"), "got %q", msg)
	require.Contains(t, msg, "granted-body")
}

func TestWebSocket_TenantsDoNotShareMessages(t *testing.T) {
	url := webSocketURL(t)
	sub := stompSubscribe(t, url, "subscribe-token", "/topic/orders")

	stompPublish(t, url, "tenant-b-token", "/topic/orders", "tenant-b-body", "RECEIPT")
	stompPublish(t, url, "publish-token", "/topic/orders", "tenant-a-body", "RECEIPT")

	msg, err := readMessage(sub, 5*time.Second)
	require.NoError(t, err)
	require.Contains(t, msg, "tenant-a-body", "the first message tenant-a receives must be its own")
	msg, err = readMessage(sub, 500*time.Millisecond)
	require.Errorf(t, err, "tenant-a received a further message: %q", msg)
}
