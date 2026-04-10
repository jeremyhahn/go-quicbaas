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

// Package subprotocol provides WebSocket subprotocol handlers.
package subprotocol

import (
	"context"
	"sync/atomic"

	"github.com/coder/websocket"

	ws "github.com/jeremyhahn/go-quicbaas/pkg/api/websocket"
)

// EchoHandler implements the "echo" subprotocol for testing and debugging.
// It simply echoes back any message received from the client.
type EchoHandler struct {
	// messagesReceived tracks the total number of messages received (for testing).
	messagesReceived atomic.Uint64

	// messagesSent tracks the total number of messages sent (for testing).
	messagesSent atomic.Uint64
}

// NewEchoHandler creates a new echo subprotocol handler.
func NewEchoHandler() *EchoHandler {
	return &EchoHandler{}
}

// Name returns the subprotocol name.
func (h *EchoHandler) Name() string {
	return "echo"
}

// HandleConnection handles a WebSocket connection using the echo protocol.
// It reads messages and echoes them back until the connection is closed.
func (h *EchoHandler) HandleConnection(ctx context.Context, conn *websocket.Conn, session *ws.Session) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Read message
		msgType, data, err := conn.Read(ctx)
		if err != nil {
			// Check if context was cancelled or connection closed normally
			if ctx.Err() != nil || websocket.CloseStatus(err) == websocket.StatusNormalClosure {
				return nil
			}
			return err
		}

		h.messagesReceived.Add(1)
		session.UpdateActivity()

		// Increment sent counter before writing so the counter is visible
		// to the client by the time it reads the echoed message. On write
		// failure, we return an error and the counter value is irrelevant.
		h.messagesSent.Add(1)

		// Echo the message back
		if err := conn.Write(ctx, msgType, data); err != nil {
			return err
		}
	}
}

// MessagesReceived returns the total number of messages received.
func (h *EchoHandler) MessagesReceived() uint64 {
	return h.messagesReceived.Load()
}

// MessagesSent returns the total number of messages sent.
func (h *EchoHandler) MessagesSent() uint64 {
	return h.messagesSent.Load()
}

// ResetCounters resets the message counters (for testing).
func (h *EchoHandler) ResetCounters() {
	h.messagesReceived.Store(0)
	h.messagesSent.Store(0)
}
