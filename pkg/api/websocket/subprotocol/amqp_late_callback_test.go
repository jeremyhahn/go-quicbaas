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

//go:build quicbaas_amqp

package subprotocol

import "testing"

// TestAMQPHandler_HubCallbackAfterHandlerReturns publishes through the
// client's hub callback after HandleConnection has returned. The handler used
// to close its delivery channel on return, so the late send panicked the
// server with "send on closed channel".
func TestAMQPHandler_HubCallbackAfterHandlerReturns(t *testing.T) {
	hub := newLateCallbackHub()
	handler := NewAMQPHandler(hub)
	conn, handlerDone := serveOneConnection(t, "amqp", handler.HandleConnection)

	closeAndAwaitHandler(t, conn, handlerDone)
	hub.deliverLate(t, "late-queue")
}
