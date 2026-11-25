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

package subprotocol

import (
	"context"
	"crypto/fips140"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/coder/websocket"

	ws "github.com/jeremyhahn/go-quicbaas/pkg/api/websocket"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// newGrantedSession is ws.NewSession with a grant that allows every
// operation, for handler tests that exercise publishing.
func newGrantedSession(id, subprotocol string, conn *websocket.Conn) *ws.Session {
	s := ws.NewSession(id, subprotocol, conn)
	s.SetGrant(ws.Grant{
		TenantID:  "test-tenant",
		Authorize: func(context.Context, string, string) error { return nil },
	})
	return s
}

// newDeniedSession is ws.NewSession with a grant that refuses every
// operation.
func newDeniedSession(id, subprotocol string, conn *websocket.Conn) *ws.Session {
	s := ws.NewSession(id, subprotocol, conn)
	s.SetGrant(ws.Grant{
		TenantID: "test-tenant",
		Authorize: func(_ context.Context, resource, action string) error {
			return errors.New("denied " + resource + ":" + action)
		},
	})
	return s
}

// subscribeTestHub registers a hub client subscribed to topic and returns
// the payloads it receives.
func subscribeTestHub(t *testing.T, hub PubSubHub, clientID, topic string) <-chan []byte {
	t.Helper()
	got := make(chan []byte, 16)
	if err := hub.RegisterClient(clientID, func(_ string, payload []byte, _ byte, _ bool) {
		select {
		case got <- payload:
		default:
		}
	}); err != nil {
		t.Fatalf("RegisterClient: %v", err)
	}
	if err := hub.Subscribe(clientID, topic, 0); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	t.Cleanup(func() { _ = hub.UnregisterClient(clientID) })
	return got
}

func testMessagingLogger(t *testing.T) *logger.Logger {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "debug", Format: "console", Output: "stdout"})
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	return log
}

func TestNewMessagingServer_RegistersThreeSubprotocols(t *testing.T) {
	srv, err := NewMessagingServer(testMessagingLogger(t))
	if fips140.Enforced() {
		if err == nil {
			t.Fatal("NewMessagingServer succeeded under FIPS-only mode")
		}
		t.Skip("FIPS-only mode refuses the WebSocket server")
	}
	if err != nil {
		t.Fatalf("NewMessagingServer: %v", err)
	}

	got := srv.GetSubprotocols()
	sort.Strings(got)
	want := []string{"amqp", "mqtt", "stomp"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("GetSubprotocols() = %v, want %v", got, want)
	}
	if err := srv.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// tenantTestServer serves a TenantMessagingServer behind a wrapper that
// grants the tenant named by the request's tenant query parameter (no
// parameter, no grant) and allows every operation.
func tenantTestServer(t *testing.T) (*TenantMessagingServer, string) {
	t.Helper()
	tms, err := NewTenantMessagingServer(testMessagingLogger(t))
	if fips140.Enforced() {
		if err == nil {
			t.Fatal("NewTenantMessagingServer succeeded under FIPS-only mode")
		}
		t.Skip("FIPS-only mode refuses the WebSocket server")
	}
	if err != nil {
		t.Fatalf("NewTenantMessagingServer: %v", err)
	}
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if tenant := r.URL.Query().Get("tenant"); tenant != "" {
			authorize := func(context.Context, string, string) error { return nil }
			if r.URL.Query().Get("deny") != "" {
				authorize = func(_ context.Context, resource, action string) error {
					return errors.New("denied " + resource + ":" + action)
				}
			}
			r = r.WithContext(ws.WithGrant(r.Context(), ws.Grant{
				TenantID:  tenant,
				Authorize: authorize,
			}))
		}
		tms.ServeHTTP(w, r)
	}))
	t.Cleanup(func() {
		_ = tms.Close()
		hs.Close()
	})
	return tms, "ws" + strings.TrimPrefix(hs.URL, "http")
}

func TestTenantMessagingServer_RefusesWithoutGrantAndAfterClose(t *testing.T) {
	tms, url := tenantTestServer(t)
	httpURL := "http" + strings.TrimPrefix(url, "ws")

	resp, err := http.Get(httpURL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("no grant: status = %d, want 403", resp.StatusCode)
	}

	if err := tms.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	resp, err = http.Get(httpURL + "?tenant=tenant-a")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("after Close: status = %d, want 503", resp.StatusCode)
	}
}
