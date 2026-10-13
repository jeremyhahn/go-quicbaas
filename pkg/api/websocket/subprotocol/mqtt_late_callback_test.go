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

//go:build quicbaas_mqtt

package subprotocol

import (
	"context"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// TestMQTTHandler_HubCallbackAfterHandlerReturns publishes through the
// client's hub callback after HandleConnection has returned. The handler used
// to close its delivery channel on return, so the late send panicked the
// server with "send on closed channel".
func TestMQTTHandler_HubCallbackAfterHandlerReturns(t *testing.T) {
	hub := newLateCallbackHub()
	handler := NewMQTTHandler(hub)
	conn, handlerDone := serveOneConnection(t, "mqtt", handler.HandleConnection)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := conn.Write(ctx, websocket.MessageBinary, buildMQTTConnectPacket("late-client", 4)); err != nil {
		t.Fatalf("Write CONNECT failed: %v", err)
	}
	if _, _, err := conn.Read(ctx); err != nil {
		t.Fatalf("Read CONNACK failed: %v", err)
	}

	closeAndAwaitHandler(t, conn, handlerDone)
	hub.deliverLate(t, "late/topic")
}
