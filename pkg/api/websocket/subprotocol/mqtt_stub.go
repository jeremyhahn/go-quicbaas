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

//go:build !quicbaas_mqtt

// Package subprotocol provides WebSocket subprotocol handlers.
package subprotocol

import (
	"context"
	"errors"

	"github.com/coder/websocket"

	ws "github.com/jeremyhahn/go-quicbaas/pkg/api/websocket"
)

// ErrMQTTNotCompiled indicates the MQTT subprotocol was not compiled into the binary.
var ErrMQTTNotCompiled = errors.New("mqtt subprotocol not compiled: build with -tags quicbaas_mqtt")

// MQTTHandler is a stub implementation when MQTT is not compiled.
type MQTTHandler struct{}

// NewMQTTHandler creates a stub MQTT handler that returns an error.
func NewMQTTHandler(hub interface{}) *MQTTHandler {
	return &MQTTHandler{}
}

// Name returns the subprotocol name.
func (h *MQTTHandler) Name() string {
	return "mqtt"
}

// HandleConnection returns an error indicating MQTT is not compiled.
func (h *MQTTHandler) HandleConnection(ctx context.Context, conn *websocket.Conn, session *ws.Session) error {
	return ErrMQTTNotCompiled
}

// MessagesReceived returns 0 for the stub.
func (h *MQTTHandler) MessagesReceived() uint64 {
	return 0
}

// MessagesSent returns 0 for the stub.
func (h *MQTTHandler) MessagesSent() uint64 {
	return 0
}

// ResetCounters is a no-op for the stub.
func (h *MQTTHandler) ResetCounters() {}

// ClientCount returns 0 for the stub.
func (h *MQTTHandler) ClientCount() int {
	return 0
}
