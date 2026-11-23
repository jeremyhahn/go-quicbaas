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

//go:build !integration && raft && local && quic && rest && mcp && vector && timeseries && rag

package server

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

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// allowAllScopes is the scope authorizer the test installs on the REST
// server: it admits every operation after the upgrade.
type allowAllScopes struct{}

func (allowAllScopes) Authorize(context.Context, *qrdbsdk.Identity, string, string) error {
	return nil
}

// asTenantA puts what go-qrdb's middleware puts on an authenticated request
// on the request context: the identity and its tenant.
func asTenantA(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := qrdbsdk.WithIdentity(r.Context(), &qrdbsdk.Identity{Subject: "alice", TenantID: "tenant-a"})
		ctx = qrdbsdk.ContextWithTenant(ctx, "tenant-a")
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// wsHandshake sends the opening frame of subprotocol and checks the reply.
func wsHandshake(t *testing.T, ctx context.Context, conn *websocket.Conn, subprotocol string) {
	t.Helper()
	var hello []byte
	var check func([]byte) bool
	switch subprotocol {
	case "mqtt":
		// MQTT 3.1.1 CONNECT, clean session, client id "c".
		hello = []byte{0x10, 0x0d, 0x00, 0x04, 'M', 'Q', 'T', 'T', 0x04, 0x02, 0x00, 0x3c, 0x00, 0x01, 'c'}
		check = func(b []byte) bool { return len(b) >= 4 && b[0] == 0x20 && b[3] == 0x00 }
	case "amqp":
		hello = []byte{'A', 'M', 'Q', 'P', 0, 0, 9, 1}
		check = func(b []byte) bool { return len(b) > 0 && b[0] == 1 } // a method frame (Connection.Start)
	case "stomp":
		hello = []byte("CONNECT\naccept-version:1.2\nhost:localhost\n\n\x00")
		check = func(b []byte) bool { return strings.HasPrefix(string(b), "CONNECTED") }
	}
	require.NoError(t, conn.Write(ctx, websocket.MessageBinary, hello))
	_, reply, err := conn.Read(ctx)
	require.NoError(t, err, "%s handshake reply", subprotocol)
	require.Truef(t, check(reply), "%s handshake reply %q", subprotocol, reply)
}

// The server mounts the WebSocket messaging endpoint on the REST instance it
// starts, each subprotocol completes its handshake through it, and Shutdown
// closes the open connections.
func TestStartRESTServer_MountsWebSocketAndShutdownClosesIt(t *testing.T) {
	if fips140.Enforced() {
		t.Skip("FIPS-only mode refuses the WebSocket server")
	}
	saveAndRestoreSeams(t)

	srv := withTempServer(t)
	srv.config.Server.Embedded = false
	srv.config.Server.REST.Enabled = true
	srv.config.Server.REST.Port = getFreePort(t)
	require.NoError(t, srv.initialize())
	restSrv := srv.getRESTServer()
	require.NotNil(t, restSrv)
	restSrv.SetBarrierScopeAuthorizer(allowAllScopes{})

	hs := httptest.NewServer(asTenantA(http.HandlerFunc(restSrv.HandleWebSocket)))
	t.Cleanup(hs.Close)
	url := "ws" + strings.TrimPrefix(hs.URL, "http")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conns := map[string]*websocket.Conn{}
	for _, name := range []string{"mqtt", "amqp", "stomp"} {
		conn, resp, err := websocket.Dial(ctx, url, &websocket.DialOptions{Subprotocols: []string{name}})
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		require.NoError(t, err, "dial %s", name)
		require.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)
		require.Equal(t, name, conn.Subprotocol())
		t.Cleanup(func() { _ = conn.CloseNow() })
		wsHandshake(t, ctx, conn, name)
		conns[name] = conn
	}

	require.NoError(t, srv.Shutdown(ctx))

	for name, conn := range conns {
		readCtx, readCancel := context.WithTimeout(context.Background(), 10*time.Second)
		_, data, err := conn.Read(readCtx)
		readCancel()
		require.Errorf(t, err, "%s connection still open after Shutdown (read %q)", name, data)
		require.NotErrorIs(t, err, context.DeadlineExceeded, "%s connection was not closed by Shutdown", name)
	}
}
