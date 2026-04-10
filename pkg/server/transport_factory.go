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

package server

import (
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	grpctransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport/grpc"
	mcptransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport/mcp"
	quictransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport/quic"
	resttransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport/rest"
	unixtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport/unix"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// TransportType represents a supported qrdb SDK transport mechanism.
type TransportType string

const (
	// TransportEmbedded uses in-process calls via the embedded transport.
	TransportEmbedded TransportType = "embedded"

	// TransportGRPC uses gRPC to communicate with a remote qrdb server.
	TransportGRPC TransportType = "grpc"

	// TransportREST uses REST/HTTP to communicate with a remote qrdb server.
	TransportREST TransportType = "rest"

	// TransportQUIC uses QUIC to communicate with a remote qrdb server.
	TransportQUIC TransportType = "quic"

	// TransportMCP uses MCP (Model Context Protocol) to communicate with a remote qrdb server.
	TransportMCP TransportType = "mcp"

	// TransportUnix uses a Unix domain socket to communicate with a remote qrdb server.
	TransportUnix TransportType = "unix"
)

// networkTransportConstructor creates a network transport from options.
type networkTransportConstructor func(opts ...transport.Option) (transport.Transport, error)

// networkTransportHandlers maps each network transport type to its constructor.
// Embedded is intentionally excluded because it requires service references
// rather than network options.
var networkTransportHandlers = map[TransportType]networkTransportConstructor{
	TransportGRPC: func(opts ...transport.Option) (transport.Transport, error) {
		return grpctransport.New(opts...)
	},
	TransportREST: func(opts ...transport.Option) (transport.Transport, error) {
		return resttransport.New(opts...)
	},
	TransportQUIC: func(opts ...transport.Option) (transport.Transport, error) {
		return quictransport.New(opts...)
	},
	TransportMCP: func(opts ...transport.Option) (transport.Transport, error) {
		return mcptransport.New(opts...)
	},
	TransportUnix: func(opts ...transport.Option) (transport.Transport, error) {
		return unixtransport.New(opts...)
	},
}

// transportFactory encapsulates the logic for creating qrdb SDK clients
// based on the configured transport type. It validates configuration and
// delegates to the appropriate transport constructor.
type transportFactory struct {
	cfg          *config.QRDBConfig
	kvService    interface{}
	adminService interface{}
}

// newTransportFactory creates a new transport factory. The kvService and
// adminService parameters are used for embedded transport creation and
// may be nil for network transports.
func newTransportFactory(cfg *config.QRDBConfig, kvService, adminService interface{}) *transportFactory {
	return &transportFactory{
		cfg:          cfg,
		kvService:    kvService,
		adminService: adminService,
	}
}

// isEmbedded returns true if the configured transport is embedded or unset.
func (f *transportFactory) isEmbedded() bool {
	if f.cfg == nil {
		return true
	}
	return f.cfg.IsEmbedded()
}

// transportType returns the resolved TransportType from config.
func (f *transportFactory) transportType() TransportType {
	if f.cfg == nil || f.cfg.Transport == "" {
		return TransportEmbedded
	}
	return TransportType(f.cfg.Transport)
}

// validateNetworkConfig validates that the necessary configuration is
// present for the given network transport type.
func (f *transportFactory) validateNetworkConfig(tt TransportType) error {
	if tt == TransportUnix {
		if f.cfg == nil || f.cfg.UnixSocketPath == "" {
			return &UnixTransportMissingSocketError{}
		}
		return nil
	}

	// All other network transports require an address.
	if f.cfg == nil || f.cfg.Address == "" {
		return &NetworkTransportMissingAddressError{Transport: string(tt)}
	}
	return nil
}

// createNetworkClient creates an SDK client for a network transport type.
// It validates configuration, builds transport options, constructs the
// transport, and wraps it in an SDK client.
func (f *transportFactory) createNetworkClient() (*qrdbsdk.Client, error) {
	tt := f.transportType()

	if err := f.validateNetworkConfig(tt); err != nil {
		return nil, err
	}

	constructor, ok := networkTransportHandlers[tt]
	if !ok {
		return nil, &UnsupportedTransportError{Transport: string(tt)}
	}

	opts := buildTransportOptions(f.cfg)
	t, err := constructor(opts...)
	if err != nil {
		return nil, &TransportCreationError{Transport: string(tt), Cause: err}
	}

	client, err := qrdbsdk.NewClient(t)
	if err != nil {
		return nil, &TransportCreationError{Transport: string(tt), Cause: err}
	}

	return client, nil
}

// buildTransportOptions converts a QRDBConfig into transport.Option values
// for network transport constructors.
func buildTransportOptions(cfg *config.QRDBConfig) []transport.Option {
	if cfg == nil {
		return nil
	}

	var opts []transport.Option

	// For Unix transport, use the socket path as the address.
	if cfg.Transport == config.QRDBTransportUnix {
		if cfg.UnixSocketPath != "" {
			opts = append(opts, transport.WithAddress(cfg.UnixSocketPath))
		}
	} else if cfg.Address != "" {
		opts = append(opts, transport.WithAddress(cfg.Address))
	}

	if cfg.TLSEnabled {
		opts = append(opts, transport.WithTLS(cfg.TLSCertFile, cfg.TLSKeyFile, cfg.TLSCAFile))
	}

	return opts
}
