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

package unix

import (
	"context"
	"strings"
	"sync/atomic"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/agents"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport/grpc"
)

// Transport wraps the gRPC transport for Unix Domain Socket connections.
// It provides lower latency for local connections by avoiding TCP/IP overhead.
type Transport struct {
	grpc       *grpc.Transport
	socketPath string
	closed     atomic.Bool
}

// New creates a new Unix socket transport.
//
// The socket path can be specified in the following formats:
//   - "/var/run/quicbaas.sock" (raw path)
//   - "unix:///var/run/quicbaas.sock" (explicit prefix)
//
// Returns an error if the socket path is empty or invalid.
func New(cfg *transport.Config) (*Transport, error) {
	if cfg == nil {
		return nil, errors.NewInvalidInput("unix.New", "config is required")
	}

	if cfg.Address == "" {
		return nil, errors.NewInvalidInput("unix.New", "socket path is required")
	}

	// Extract the socket path from the address
	socketPath := normalizeSocketPath(cfg.Address)

	// Validate socket path
	if err := validateSocketPath(socketPath); err != nil {
		return nil, err
	}

	// Create a copy of the config for the gRPC transport
	grpcCfg := *cfg
	grpcCfg.Address = socketPath
	grpcCfg.AddressType = transport.AddressTypeUnix

	// Create the underlying gRPC transport
	grpcTransport, err := grpc.New(&grpcCfg)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeConnection, "unix.New",
			"failed to create gRPC transport for Unix socket")
	}

	return &Transport{
		grpc:       grpcTransport,
		socketPath: socketPath,
	}, nil
}

// normalizeSocketPath extracts the socket path from various formats.
func normalizeSocketPath(address string) string {
	// Handle unix:// prefix
	if strings.HasPrefix(address, "unix://") {
		return strings.TrimPrefix(address, "unix://")
	}
	// Handle unix: prefix (gRPC format)
	if strings.HasPrefix(address, "unix:") {
		return strings.TrimPrefix(address, "unix:")
	}
	// Raw path
	return address
}

// validateSocketPath validates the socket path.
func validateSocketPath(path string) error {
	if path == "" {
		return errors.NewInvalidInput("unix.validateSocketPath", "socket path is empty")
	}

	// Unix socket paths must be absolute (start with /) or abstract (start with @)
	if !strings.HasPrefix(path, "/") && !strings.HasPrefix(path, "@") {
		return errors.NewInvalidInput("unix.validateSocketPath",
			"socket path must be absolute (start with /) or abstract (start with @)")
	}

	// Maximum path length for Unix sockets is typically 108 bytes (including null terminator)
	// We use 107 as the max to leave room for the null terminator
	const maxPathLen = 107
	if len(path) > maxPathLen {
		return errors.NewInvalidInput("unix.validateSocketPath",
			"socket path exceeds maximum length of 107 bytes")
	}

	return nil
}

// Request sends a unary request and returns the response.
func (t *Transport) Request(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
	if t.closed.Load() {
		return nil, errors.NewConnection("unix.Request", "transport is closed")
	}
	if t.grpc == nil {
		return nil, errors.NewConnection("unix.Request", "transport not initialized")
	}
	return t.grpc.Request(ctx, service, method, req)
}

// RequestStream initiates a streaming request.
func (t *Transport) RequestStream(ctx context.Context, service, method string, req interface{}) (transport.Stream, error) {
	if t.closed.Load() {
		return nil, errors.NewConnection("unix.RequestStream", "transport is closed")
	}
	if t.grpc == nil {
		return nil, errors.NewConnection("unix.RequestStream", "transport not initialized")
	}
	return t.grpc.RequestStream(ctx, service, method, req)
}

// Conn returns the underlying gRPC connection.
func (t *Transport) Conn() interface{} {
	if t.grpc == nil {
		return nil
	}
	return t.grpc.Conn()
}

// Close closes the transport connection.
func (t *Transport) Close() error {
	if !t.closed.CompareAndSwap(false, true) {
		return nil
	}
	if t.grpc == nil {
		return nil
	}
	return t.grpc.Close()
}

// Healthy returns true if the transport is healthy.
func (t *Transport) Healthy() bool {
	if t.closed.Load() {
		return false
	}
	if t.grpc == nil {
		return false
	}
	return t.grpc.Healthy()
}

// SocketPath returns the Unix socket path.
func (t *Transport) SocketPath() string {
	return t.socketPath
}

// Factory implements transport.Factory for Unix socket transport.
type Factory struct{}

// Create creates a new Unix socket transport.
func (f *Factory) Create(cfg *transport.Config) (transport.Transport, error) {
	return New(cfg)
}

// SupportsSDLC declares the SDLC service on the Unix transport: every
// request delegates to the gRPC transport verbatim, which carries it.
func (t *Transport) SupportsSDLC() bool { return true }

// SupportsAgents declares the Agents service on the Unix transport: every
// verb is carried by the gRPC transport it delegates to.
func (t *Transport) SupportsAgents() bool { return true }

var _ agents.CapableTransport = (*Transport)(nil)
