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

//go:build !quicbaas_amqp

// Package subprotocol provides WebSocket subprotocol handlers.
package subprotocol

import (
	"context"
	"errors"

	"github.com/coder/websocket"

	ws "github.com/jeremyhahn/go-quicbaas/pkg/api/websocket"
)

// ErrAMQPNotCompiled indicates the AMQP subprotocol was not compiled into the binary.
var ErrAMQPNotCompiled = errors.New("amqp subprotocol not compiled: build with -tags quicbaas_amqp")

// AMQPHandler is a stub implementation when AMQP is not compiled.
type AMQPHandler struct{}

// NewAMQPHandler creates a stub AMQP handler that returns an error.
func NewAMQPHandler(hub interface{}) *AMQPHandler {
	return &AMQPHandler{}
}

// Name returns the subprotocol name.
func (h *AMQPHandler) Name() string {
	return "amqp"
}

// HandleConnection returns an error indicating AMQP is not compiled.
func (h *AMQPHandler) HandleConnection(ctx context.Context, conn *websocket.Conn, session *ws.Session) error {
	return ErrAMQPNotCompiled
}

// MessagesReceived returns 0 for the stub.
func (h *AMQPHandler) MessagesReceived() uint64 {
	return 0
}

// MessagesSent returns 0 for the stub.
func (h *AMQPHandler) MessagesSent() uint64 {
	return 0
}

// ResetCounters is a no-op for the stub.
func (h *AMQPHandler) ResetCounters() {}

// ClientCount returns 0 for the stub.
func (h *AMQPHandler) ClientCount() int {
	return 0
}
