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

//go:build desktop

package main

import (
	"context"
	"sync/atomic"

	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// ConnectionBinding manages the SDK client connection to a QuicBaaS server.
// It is exposed to the Wails frontend via IPC for connection management.
// All state is stored using atomic operations for lock-free thread safety.
type ConnectionBinding struct {
	logger    *logger.Logger
	ctx       context.Context
	client    atomic.Pointer[quicbaas.Client]
	address   atomic.Value // stores string
	transport atomic.Value // stores string
}

// NewConnectionBinding creates a new ConnectionBinding with the given logger.
func NewConnectionBinding(log *logger.Logger) *ConnectionBinding {
	b := &ConnectionBinding{
		logger: log,
	}
	b.address.Store("")
	b.transport.Store("")
	return b
}

// startup is called by Wails when the application starts. It stores the
// application context for use in subsequent operations.
func (b *ConnectionBinding) startup(ctx context.Context) {
	b.ctx = ctx
}

// shutdown is called by Wails when the application is closing. It disconnects
// the SDK client if one is active.
func (b *ConnectionBinding) shutdown(ctx context.Context) {
	if clientPtr := b.client.Load(); clientPtr != nil {
		client := *clientPtr
		if client != nil {
			if err := client.Close(); err != nil {
				b.logger.WithError(err).Error("failed to close SDK client on shutdown")
			}
		}
		b.client.Store(nil)
	}
}

// Connect establishes a connection to a QuicBaaS server using the SDK client.
// It creates a new client with the given address and transport, closing any
// existing connection first. The tenantID parameter is reserved for future
// multi-tenant connection scoping.
func (b *ConnectionBinding) Connect(address, transportType, tenantID string) error {
	if address == "" {
		return NewGUIConnectError("ConnectionBinding.Connect", "", nil)
	}

	// Close existing connection if any
	if err := b.Disconnect(); err != nil {
		b.logger.WithError(err).Warn("error disconnecting previous client")
	}

	// Build SDK client options based on transport
	opts, err := buildTransportOptions(transportType, address)
	if err != nil {
		return NewGUIConnectError("ConnectionBinding.Connect", address, err)
	}

	// Create the SDK client
	client, err := quicbaas.NewClient(address, opts...)
	if err != nil {
		return NewGUIConnectError("ConnectionBinding.Connect", address, err)
	}

	// Store atomically
	b.client.Store(&client)
	b.address.Store(address)
	b.transport.Store(transportType)

	b.logger.Infof("GUI connected to server at %s via %s", address, transportType)

	return nil
}

// Disconnect closes the current SDK client connection and clears stored state.
func (b *ConnectionBinding) Disconnect() error {
	clientPtr := b.client.Swap(nil)
	if clientPtr == nil {
		return nil
	}

	client := *clientPtr
	if client == nil {
		return nil
	}

	b.address.Store("")
	b.transport.Store("")

	if err := client.Close(); err != nil {
		return NewGUITransportError("ConnectionBinding.Disconnect", err)
	}

	b.logger.Info("GUI disconnected from server")
	return nil
}

// IsConnected returns true if an SDK client is currently connected.
func (b *ConnectionBinding) IsConnected() bool {
	clientPtr := b.client.Load()
	return clientPtr != nil && *clientPtr != nil
}

// GetAddress returns the address of the currently connected server.
func (b *ConnectionBinding) GetAddress() string {
	val := b.address.Load()
	if val == nil {
		return ""
	}
	addr, ok := val.(string)
	if !ok {
		return ""
	}
	return addr
}

// GetTransport returns the transport type of the current connection.
func (b *ConnectionBinding) GetTransport() string {
	val := b.transport.Load()
	if val == nil {
		return ""
	}
	t, ok := val.(string)
	if !ok {
		return ""
	}
	return t
}

// loadClient returns the active SDK client or a GUINotConnectedError if
// no client is connected. This is used internally by binding methods that
// require an active connection.
func (b *ConnectionBinding) loadClient() (quicbaas.Client, error) {
	clientPtr := b.client.Load()
	if clientPtr == nil {
		return nil, &GUINotConnectedError{}
	}
	client := *clientPtr
	if client == nil {
		return nil, &GUINotConnectedError{}
	}
	return client, nil
}

// buildTransportOptions constructs SDK client options for the given transport type.
func buildTransportOptions(transportType, address string) ([]quicbaas.Option, error) {
	var opts []quicbaas.Option

	switch transportType {
	case "rest":
		opts = append(opts, quicbaas.WithREST())
	case "quic":
		opts = append(opts, quicbaas.WithQUIC())
	case "unix":
		opts = append(opts, quicbaas.WithUnix(address))
	case "mcp":
		opts = append(opts, quicbaas.WithMCP())
	case "grpc", "":
		// gRPC is the default transport
	default:
		return nil, &UnsupportedTransportError{Transport: transportType}
	}

	return opts, nil
}
