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
	"crypto/fips140"
	"errors"
	"net/http"
	"sync"

	ws "github.com/jeremyhahn/go-quicbaas/pkg/api/websocket"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// NewMessagingServer builds one WebSocket server with the MQTT, AMQP and
// STOMP subprotocols registered over one in-memory pub/sub hub, so a message
// published over one protocol reaches subscribers on the others.
//
// The hub has no notion of tenant: one server serves exactly one tenant.
// The API listener mounts TenantMessagingServer, which builds one of these
// per verified tenant.
//
// Authentication and authorization happen in front of the server, on the
// route that mounts it; that is why the server's own Authenticator is nil.
// Operations after the upgrade (publishing) are authorized through the
// session's grant.
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

// TenantMessagingServer is the WebSocket messaging endpoint the API listener
// mounts. It keeps one messaging server, with its own hub, per tenant, so
// subscriptions, publications, retained messages and client registrations of
// different tenants never meet.
//
// The tenant comes from the ws.Grant the mounting route puts on the request
// context after it has verified the caller. A request without a grant, or
// with an empty tenant, is refused with 403.
type TenantMessagingServer struct {
	log *logger.Logger

	mu      sync.Mutex
	servers map[string]*ws.Server
	closed  bool
}

// NewTenantMessagingServer returns an empty per-tenant messaging server. In
// FIPS-only mode it returns the error ws.NewServer returns there.
func NewTenantMessagingServer(log *logger.Logger) (*TenantMessagingServer, error) {
	if fips140.Enforced() {
		return nil, ws.NewWebSocketUnavailableInFIPSOnlyModeError("subprotocol.NewTenantMessagingServer")
	}
	return &TenantMessagingServer{log: log, servers: make(map[string]*ws.Server)}, nil
}

// ServeHTTP dispatches the upgrade to the messaging server of the request's
// verified tenant, building it on first use.
func (t *TenantMessagingServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	grant, ok := ws.GrantFromContext(r.Context())
	if !ok || grant.TenantID == "" {
		http.Error(w, "no verified tenant for the websocket connection", http.StatusForbidden)
		return
	}
	srv, err := t.serverFor(grant.TenantID)
	if err != nil {
		http.Error(w, "the websocket server is not available", http.StatusServiceUnavailable)
		return
	}
	srv.HandleHTTP(w, r)
}

var errTenantMessagingServerClosed = errors.New("tenant messaging server closed")

func (t *TenantMessagingServer) serverFor(tenantID string) (*ws.Server, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil, errTenantMessagingServerClosed
	}
	if srv, ok := t.servers[tenantID]; ok {
		return srv, nil
	}
	srv, err := NewMessagingServer(t.log)
	if err != nil {
		return nil, err
	}
	t.servers[tenantID] = srv
	return srv, nil
}

// TenantCount returns how many tenants have a messaging server.
func (t *TenantMessagingServer) TenantCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.servers)
}

// Close closes every tenant's messaging server and refuses later upgrades
// with 503. The errors of the individual closes are joined.
func (t *TenantMessagingServer) Close() error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil
	}
	t.closed = true
	servers := t.servers
	t.servers = make(map[string]*ws.Server)
	t.mu.Unlock()

	var errs []error
	for _, srv := range servers {
		if err := srv.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
