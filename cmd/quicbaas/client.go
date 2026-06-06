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

package main

import (
	"crypto/tls"
	"crypto/x509"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// sdkClientHolder holds the singleton SDK client using atomic operations for thread safety.
var sdkClientHolder atomic.Pointer[quicbaas.Client]

// ErrSDKClientClosed is returned when attempting to use a closed SDK client.
var ErrSDKClientClosed = errors.New(errors.ErrInternal, "createSDKClient", "SDK client has been closed")

// ErrEmptyAddress is returned when an empty address is provided.
var ErrEmptyAddress = errors.New(errors.ErrConfiguration, "createSDKClient", "address cannot be empty")

// UnsupportedTransportError is returned when an unsupported transport type is specified.
type UnsupportedTransportError struct {
	Transport string
}

// Error implements the error interface.
func (e *UnsupportedTransportError) Error() string {
	return "unsupported transport: " + e.Transport + " (supported: grpc, rest, quic, unix, mcp, embedded)"
}

// createSDKClient creates and returns the singleton SDK client.
// It uses the global address, transport, and globalTimeout variables.
// Thread-safe for concurrent access.
func createSDKClient() (quicbaas.Client, error) {
	// Fast path: check if client already exists
	if clientPtr := sdkClientHolder.Load(); clientPtr != nil {
		return *clientPtr, nil
	}

	// Validate address
	if address == "" {
		return nil, ErrEmptyAddress
	}

	// Build client options based on global configuration
	opts, err := buildSDKClientOptions()
	if err != nil {
		return nil, err
	}

	// Create the SDK client
	client, err := quicbaas.NewClient(address, opts...)
	if err != nil {
		return nil, errors.Wrapf(err, errors.ErrUnavailable, "createSDKClient", "failed to create SDK client")
	}

	// Store the client atomically (compare-and-swap to handle races)
	if !sdkClientHolder.CompareAndSwap(nil, &client) {
		// Another goroutine created the client first, close ours and return theirs
		closeErr := client.Close()
		if closeErr != nil {
			// Log but don't fail - we're returning the existing client
			_ = closeErr
		}
		if existingPtr := sdkClientHolder.Load(); existingPtr != nil {
			return *existingPtr, nil
		}
		// Edge case: the other client was closed between our CAS and Load
		return nil, ErrSDKClientClosed
	}

	return client, nil
}

// buildSDKClientOptions builds the SDK client options based on global configuration.
func buildSDKClientOptions() ([]quicbaas.Option, error) {
	var opts []quicbaas.Option

	// Auto-detect Unix transport when address is a socket path.
	if transport == "" || transport == "grpc" {
		if strings.HasSuffix(address, ".sock") {
			transport = "unix"
		}
	}

	// Configure transport type
	switch transport {
	case "rest":
		opts = append(opts, quicbaas.WithREST())
	case "quic":
		opts = append(opts, quicbaas.WithQUIC())
	case "unix":
		opts = append(opts, quicbaas.WithUnix(address))
	case "mcp":
		opts = append(opts, quicbaas.WithMCP())
		// Load stored token for MCP authentication
		if token := loadMCPAuthToken(); token != "" {
			opts = append(opts, quicbaas.WithMCPAuthToken(token))
		}
	case "embedded":
		// Embedded transport uses an in-process QuicBaaS server.
		// Get or create the embedded server and use its service.
		es, err := getOrCreateEmbeddedServer()
		if err != nil {
			return nil, err
		}
		opts = append(opts, quicbaas.WithEmbedded(es.service))
		// Wire the qrdb sub-client to dispatch in-process against the
		// embedded server's per-shard baasTransport (via the BaaS KV +
		// Admin adapters) plus the bootstrap ceremony services. Without
		// WithEmbeddedQRDB, the qrdb sub-client has no listener to dial
		// in embedded mode and Security().Ceremony() / Security().Barrier()
		// would fail with a "produced zero addresses" transport error.
		qrdbOpt, err := embeddedQRDBOption(es)
		if err != nil {
			return nil, err
		}
		opts = append(opts, qrdbOpt)
	case "grpc", "":
		// gRPC is the default transport, no option needed
	default:
		return nil, &UnsupportedTransportError{Transport: transport}
	}

	// Add timeout if set
	if globalTimeout > 0 {
		opts = append(opts, quicbaas.WithTimeout(globalTimeout))
	}

	// TLS configuration. QuicBaaS does not support insecure TLS — every
	// client must trust the cluster's CA (auto-discovered from data-dir
	// or set via --tls-ca).
	tlsCfg, err := buildTLSConfig()
	if err != nil {
		return nil, err
	}
	if tlsCfg != nil {
		opts = append(opts, quicbaas.WithTLSConfig(tlsCfg))
	}

	return opts, nil
}

// buildTLSConfig creates a TLS configuration from CLI flags.
// If no explicit CA cert is provided, it attempts to auto-discover the CA
// certificate from the server's data directory (data-dir/certs/ca-chain.crt).
func buildTLSConfig() (*tls.Config, error) {
	caFile := tlsCA

	// Auto-discover CA cert from data directory if not explicitly set
	if caFile == "" {
		autoCA := filepath.Join(dataDir, "certs", "ca-chain.crt")
		if _, err := os.Stat(autoCA); err == nil {
			caFile = autoCA
		}
	}

	// No CA cert available - return nil to let the SDK use system defaults
	if caFile == "" {
		return nil, nil
	}

	// Load CA certificate pool
	caCert, err := os.ReadFile(caFile)
	if err != nil {
		return nil, errors.Wrapf(err, errors.ErrConfiguration, "buildTLSConfig",
			"failed to read CA certificate")
	}

	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(caCert) {
		return nil, errors.New(errors.ErrConfiguration, "buildTLSConfig",
			"failed to parse CA certificate")
	}

	tlsCfg := &tls.Config{
		RootCAs:    caCertPool,
		MinVersion: tls.VersionTLS13,
	}

	// Load client certificate if both cert and key are provided (mTLS)
	if tlsCert != "" && tlsKey != "" {
		cert, err := tls.LoadX509KeyPair(tlsCert, tlsKey)
		if err != nil {
			return nil, errors.Wrapf(err, errors.ErrConfiguration, "buildTLSConfig",
				"failed to load client certificate")
		}
		tlsCfg.Certificates = []tls.Certificate{cert}
	}

	return tlsCfg, nil
}

// closeSDKClient closes the singleton SDK client and resets the holder.
// Thread-safe for concurrent access.
// Also shuts down any embedded server if running.
func closeSDKClient() error {
	clientPtr := sdkClientHolder.Swap(nil)
	var clientErr error
	if clientPtr != nil {
		client := *clientPtr
		if client != nil {
			clientErr = client.Close()
		}
	}

	// Also shutdown embedded server if running
	embeddedErr := shutdownEmbeddedServer()

	// Return first error
	if clientErr != nil {
		return clientErr
	}
	return embeddedErr
}

// getSDKClient returns the current SDK client without creating one.
// Returns nil if no client has been created.
func getSDKClient() quicbaas.Client {
	if clientPtr := sdkClientHolder.Load(); clientPtr != nil {
		return *clientPtr
	}
	return nil
}

// resetSDKClient resets the SDK client holder for testing purposes.
// This should only be used in tests.
func resetSDKClient() {
	sdkClientHolder.Store(nil)
}

// loadMCPAuthToken loads the authentication token for MCP transport.
// Returns empty string if no token is available or if loading fails.
func loadMCPAuthToken() string {
	// Determine token file path
	tokenPath := authTokenFile
	if tokenPath == "" {
		var err error
		tokenPath, err = cli.GetDefaultTokenPath()
		if err != nil {
			return ""
		}
	}

	// Check if token file exists
	if _, err := os.Stat(tokenPath); os.IsNotExist(err) {
		return ""
	}

	// Load token
	token, err := cli.LoadToken(tokenPath)
	if err != nil {
		return ""
	}

	// Validate token is not expired
	if !cli.IsTokenValid(token) {
		return ""
	}

	return token.AccessToken
}
