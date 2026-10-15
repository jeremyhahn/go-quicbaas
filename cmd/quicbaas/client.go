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
	"context"
	"crypto/tls"
	"crypto/x509"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

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
	// Supported lists the transports the failing command accepts; empty
	// means every CLI transport.
	Supported []string
}

// Error implements the error interface.
func (e *UnsupportedTransportError) Error() string {
	supported := "grpc, rest, quic, unix, mcp, embedded"
	if len(e.Supported) > 0 {
		supported = strings.Join(e.Supported, ", ")
	}
	return "unsupported transport: " + e.Transport + " (supported: " + supported + ")"
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

// buildSDKClientOptions builds the SDK client options based on global
// configuration, attaching the stored (renewed if necessary) session token.
func buildSDKClientOptions() ([]quicbaas.Option, error) {
	// Load the stored platform token once. It authenticates every network
	// transport against the server's default-deny scope -> RBAC gate. The
	// embedded transport dispatches in-process and needs no token.
	return buildSDKClientOptionsWithToken(loadStoredAuthToken())
}

// buildSDKClientOptionsWithToken builds the SDK client options with an
// explicitly supplied bearer. An empty token yields an unauthenticated client,
// which is what the session-renewal path needs (the refresh endpoint is public,
// and resolving a token there would recurse).
func buildSDKClientOptionsWithToken(authToken string) ([]quicbaas.Option, error) {
	var opts []quicbaas.Option

	// --tenant scopes the client, and some routes cannot work without it: the
	// WebAuthn ceremony is mounted per tenant and its handler reads the tenant
	// from the transport. Until this was wired, `auth factor add --tenant X`
	// failed with "tenant is required; configure rest.WithDefaultTenant" — the
	// flag was parsed and then had nowhere to go.
	if globalTenantID != "" {
		opts = append(opts, quicbaas.WithTenant(globalTenantID))
	}

	// Settle the transport from --transport and --address (selectTransport).
	transport = selectTransport()

	// Configure transport type
	switch transport {
	case "rest":
		opts = append(opts, quicbaas.WithREST())
		if authToken != "" {
			opts = append(opts, quicbaas.WithAuthToken(authToken))
		}
	case "quic":
		opts = append(opts, quicbaas.WithQUIC())
		if authToken != "" {
			opts = append(opts, quicbaas.WithAuthToken(authToken))
		}
	case "unix":
		opts = append(opts, quicbaas.WithUnix(address))
		if authToken != "" {
			opts = append(opts, quicbaas.WithAuthToken(authToken))
		}
	case "mcp":
		opts = append(opts, quicbaas.WithMCP())
		// MCP keeps its dedicated token option for transport parity.
		if authToken != "" {
			opts = append(opts, quicbaas.WithMCPAuthToken(authToken))
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
		// gRPC is the default transport, no transport option needed.
		if authToken != "" {
			opts = append(opts, quicbaas.WithAuthToken(authToken))
		}
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

// storedTokenPath resolves the session envelope path: --token-file when set,
// otherwise the default ~/.quicbaas/token.
func storedTokenPath() (string, error) {
	if authTokenFile != "" {
		return authTokenFile, nil
	}
	return cli.GetDefaultTokenPath()
}

// loadStoredAuthToken loads the stored platform authentication token used to
// authenticate data-plane requests across every transport (REST / gRPC / QUIC /
// Unix / MCP) against the server's default-deny scope -> RBAC gate.
//
// Access tokens live 15 minutes by design, so an expired token is RENEWED from
// its refresh credential (and the rotated pair written back) rather than
// discarded. That is what lets `quicbaas mcp serve --stdio` — which Claude Code
// keeps alive for hours — survive past the first expiry instead of failing with
// "authentication required".
//
// It returns an empty string when there is no session at all or renewal fails;
// the caller then proceeds unauthenticated and the server's default-deny gate
// produces the actionable error.
func loadStoredAuthToken() string {
	tokenPath, err := storedTokenPath()
	if err != nil {
		return ""
	}
	if _, err := os.Stat(tokenPath); os.IsNotExist(err) {
		return ""
	}

	ctx, cancel := context.WithTimeout(context.Background(), sessionRefreshTimeout)
	defer cancel()

	token, err := cli.NewTokenManager(tokenPath, cli.SessionRefresherFunc(refreshStoredSession)).AccessToken(ctx)
	if err != nil {
		return ""
	}
	return token
}

// sessionRefreshTimeout bounds the renewal round-trip so a hung server cannot
// stall every CLI invocation.
const sessionRefreshTimeout = 15 * time.Second

// refreshStoredSession redeems a refresh credential against the server.
//
// It dials with a DELIBERATELY token-less client: /api/v1/iam/refresh is a
// public endpoint (the refresh credential is itself the authentication), and
// building an authenticated client here would recurse straight back into
// loadStoredAuthToken.
func refreshStoredSession(ctx context.Context, refreshToken string) (*cli.TokenData, error) {
	opts, err := buildSDKClientOptionsWithToken("")
	if err != nil {
		return nil, err
	}
	client, err := quicbaas.NewClient(address, opts...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = client.Close() }()

	resp, err := client.Security().Auth().RefreshToken(ctx, refreshToken)
	if err != nil {
		return nil, err
	}
	return &cli.TokenData{
		AccessToken:  resp.AccessToken,
		RefreshToken: resp.RefreshToken,
		ExpiresAt:    resp.ExpiresAt,
		TokenType:    resp.TokenType,
	}, nil
}
