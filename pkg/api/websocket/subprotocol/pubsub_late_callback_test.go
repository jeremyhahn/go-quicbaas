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

//go:build quicbaas_mqtt || quicbaas_stomp || quicbaas_amqp

package subprotocol

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	ws "github.com/jeremyhahn/go-quicbaas/pkg/api/websocket"
)

// lateDeliveries is more than any handler's delivery buffer holds, so a late
// publish exercises both the buffered send and the drop-when-full path.
const lateDeliveries = 250

// lateCallbackHub is an InMemoryPubSubHub that remembers every callback it
// was given. InMemoryPubSubHub.Publish collects callbacks under its lock and
// runs them after releasing it, so a publisher can still be inside a client's
// callback after that client has unregistered and its handler has returned.
// The hub lets a test play that publisher deterministically.
type lateCallbackHub struct {
	*InMemoryPubSubHub

	mu        sync.Mutex
	callbacks []func(topic string, payload []byte, qos byte, retain bool)
}

func newLateCallbackHub() *lateCallbackHub {
	return &lateCallbackHub{InMemoryPubSubHub: NewInMemoryPubSubHub()}
}

// RegisterClient records the callback, then registers it with the real hub.
func (h *lateCallbackHub) RegisterClient(clientID string, callback func(topic string, payload []byte, qos byte, retain bool)) error {
	h.mu.Lock()
	h.callbacks = append(h.callbacks, callback)
	h.mu.Unlock()
	return h.InMemoryPubSubHub.RegisterClient(clientID, callback)
}

// deliverLate invokes every recorded callback lateDeliveries times, as an
// in-flight Publish would after the client went away.
func (h *lateCallbackHub) deliverLate(t *testing.T, topic string) {
	t.Helper()

	h.mu.Lock()
	callbacks := slices.Clone(h.callbacks)
	h.mu.Unlock()
	if len(callbacks) == 0 {
		t.Fatal("handler never registered a hub callback")
	}

	for _, callback := range callbacks {
		for i := 0; i < lateDeliveries; i++ {
			callback(topic, []byte("late"), 0, false)
		}
	}
}

// connectionHandler is the HandleConnection method shared by the subprotocol
// handlers.
type connectionHandler func(ctx context.Context, conn *websocket.Conn, session *ws.Session) error

// serveOneConnection serves handle on a test WebSocket server and dials it.
// The returned channel is closed once HandleConnection has returned.
func serveOneConnection(t *testing.T, subprotocol string, handle connectionHandler) (*websocket.Conn, <-chan struct{}) {
	t.Helper()

	handlerDone := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("Accept failed: %v", err)
			close(handlerDone)
			return
		}
		session := ws.NewSession("late-callback-session", subprotocol, conn)
		if err := handle(session.Context(), conn, session); err != nil {
			t.Logf("HandleConnection returned: %v", err)
		}
		close(handlerDone)
	}))
	t.Cleanup(server.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, resp, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("WebSocket dial failed: %v", err)
	}
	return conn, handlerDone
}

// closeAndAwaitHandler closes the client side and waits for HandleConnection
// to return.
func closeAndAwaitHandler(t *testing.T, conn *websocket.Conn, handlerDone <-chan struct{}) {
	t.Helper()

	if err := conn.Close(websocket.StatusNormalClosure, "client leaving"); err != nil {
		t.Logf("client close: %v", err)
	}
	select {
	case <-handlerDone:
	case <-time.After(5 * time.Second):
		t.Fatal("HandleConnection did not return after the client closed")
	}
}
