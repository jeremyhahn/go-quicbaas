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

//go:build quicbaas_stomp

package subprotocol

import (
	"context"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// TestSTOMPHandler_HubCallbackAfterHandlerReturns publishes to a subscribed
// destination through the client's hub callback after HandleConnection has
// returned. The handler used to close its delivery channel on return, so the
// late send panicked the server with "send on closed channel".
func TestSTOMPHandler_HubCallbackAfterHandlerReturns(t *testing.T) {
	const destination = "/queue/late"

	hub := newLateCallbackHub()
	handler := NewSTOMPHandler(hub)
	conn, handlerDone := serveOneConnection(t, "stomp", handler.HandleConnection)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	frames := [][]byte{
		buildSTOMPFrame("CONNECT", map[string]string{"accept-version": "1.2", "host": "localhost"}, ""),
		buildSTOMPFrame("SUBSCRIBE", map[string]string{"id": "sub-1", "destination": destination, "receipt": "r-1"}, ""),
	}
	for _, frame := range frames {
		if err := conn.Write(ctx, websocket.MessageText, frame); err != nil {
			t.Fatalf("Write frame failed: %v", err)
		}
		// CONNECTED, then the RECEIPT confirming the subscription.
		if _, _, err := conn.Read(ctx); err != nil {
			t.Fatalf("Read reply failed: %v", err)
		}
	}

	closeAndAwaitHandler(t, conn, handlerDone)
	hub.deliverLate(t, destination)
}
