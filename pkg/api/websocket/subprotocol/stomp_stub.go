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

//go:build !quicbaas_stomp

// Package subprotocol provides WebSocket subprotocol handlers.
package subprotocol

import (
	"context"
	"errors"

	"github.com/coder/websocket"

	ws "github.com/jeremyhahn/go-quicbaas/pkg/api/websocket"
)

// ErrSTOMPNotCompiled indicates the STOMP subprotocol was not compiled into the binary.
var ErrSTOMPNotCompiled = errors.New("stomp subprotocol not compiled: build with -tags quicbaas_stomp")

// STOMPHandler is a stub implementation when STOMP is not compiled.
type STOMPHandler struct{}

// NewSTOMPHandler creates a stub STOMP handler that returns an error.
func NewSTOMPHandler(hub interface{}) *STOMPHandler {
	return &STOMPHandler{}
}

// Name returns the subprotocol name.
func (h *STOMPHandler) Name() string {
	return "stomp"
}

// HandleConnection returns an error indicating STOMP is not compiled.
func (h *STOMPHandler) HandleConnection(ctx context.Context, conn *websocket.Conn, session *ws.Session) error {
	return ErrSTOMPNotCompiled
}

// MessagesReceived returns 0 for the stub.
func (h *STOMPHandler) MessagesReceived() uint64 {
	return 0
}

// MessagesSent returns 0 for the stub.
func (h *STOMPHandler) MessagesSent() uint64 {
	return 0
}

// ResetCounters is a no-op for the stub.
func (h *STOMPHandler) ResetCounters() {}

// ClientCount returns 0 for the stub.
func (h *STOMPHandler) ClientCount() int {
	return 0
}
