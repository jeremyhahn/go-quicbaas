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
	ws "github.com/jeremyhahn/go-quicbaas/pkg/api/websocket"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// NewMessagingServer builds the WebSocket server the API listener mounts, with
// the MQTT, AMQP and STOMP subprotocols registered.
//
// Authentication and authorization happen in front of the server, on the
// route that mounts it: the upgrade request passes the same middleware as any
// other API call before HandleHTTP runs. That is why the server's own
// Authenticator is nil.
//
// The three subprotocols share one in-memory pub/sub hub, so a message
// published over one protocol reaches subscribers on the others.
//
// The error from ws.NewServer (FIPS-only mode refuses the server) is returned
// unchanged.
func NewMessagingServer(log *logger.Logger) (*ws.Server, error) {
	srv, err := ws.NewServer(ws.DefaultServerConfig(), nil, log)
	if err != nil {
		return nil, err
	}
	hub := NewInMemoryPubSubHub()
	srv.RegisterSubprotocol(NewMQTTHandler(hub))
	srv.RegisterSubprotocol(NewAMQPHandler(hub))
	srv.RegisterSubprotocol(NewSTOMPHandler(hub))
	return srv, nil
}
