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

package quicbaas

import (
	"crypto/tls"
	"crypto/x509"
	"os"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// TransportType identifies the transport protocol.
type TransportType int

const (
	// TransportGRPC uses gRPC protocol (default).
	TransportGRPC TransportType = iota
	// TransportREST uses REST/HTTP protocol.
	TransportREST
	// TransportQUIC uses QUIC protocol.
	TransportQUIC
	// TransportUnix uses gRPC over Unix Domain Socket.
	TransportUnix
	// TransportEmbedded uses in-process direct service access.
	TransportEmbedded
	// TransportMCP uses Model Context Protocol (JSON-RPC 2.0 over HTTP).
	TransportMCP
)

// TLSConfig holds TLS configuration for mTLS authentication.
type TLSConfig struct {
	// CertFile is the path to the client certificate file.
	CertFile string
	// KeyFile is the path to the client private key file.
	KeyFile string
	// CAFile is the path to the CA certificate file for server verification.
	CAFile string
	// ServerName overrides the server name for certificate verification.
	ServerName string
}

// clientOptions holds all client configuration options.
type clientOptions struct {
	// Transport selection
	transportType TransportType

	// Connection settings
	timeout          time.Duration
	keepaliveTime    time.Duration
	keepaliveTimeout time.Duration

	// Retry settings
	maxRetries      int
	retryBackoff    time.Duration
	maxRetryBackoff time.Duration

	// TLS settings
	tlsEnabled bool
	tlsConfig  *tls.Config
	insecure   bool

	// Connection pooling (gRPC only)
	poolEnabled        bool
	poolMinSize        int
	poolMaxSize        int
	poolMaxAge         time.Duration
	poolHealthInterval time.Duration

	// Unix socket settings
	unixSocketPath string

	// Embedded settings
	// Note: Uses interface{} to avoid import cycles. The actual type
	// (service.QuicBaaS) is checked when creating the transport.
	embeddedService interface{}

	// MCP authentication settings
	mcpUsername  string
	mcpPassword  string
	mcpAuthToken string
}

// Option is a functional option for configuring the client.
type Option func(*clientOptions) error

// defaultOptions returns clientOptions with sensible defaults.
func defaultOptions() *clientOptions {
	return &clientOptions{
		transportType:      TransportGRPC,
		timeout:            30 * time.Second,
		keepaliveTime:      0, // Disabled by default to avoid gRPC "too_many_pings" errors
		keepaliveTimeout:   20 * time.Second,
		maxRetries:         3,
		retryBackoff:       100 * time.Millisecond,
		maxRetryBackoff:    5 * time.Second,
		tlsEnabled:         false,
		poolEnabled:        false,
		poolMinSize:        5,
		poolMaxSize:        50,
		poolMaxAge:         time.Hour,
		poolHealthInterval: 30 * time.Second,
	}
}

// WithTimeout sets the request timeout.
func WithTimeout(d time.Duration) Option {
	return func(o *clientOptions) error {
		if d <= 0 {
			return errors.NewInvalidInput("WithTimeout", "timeout must be positive")
		}
		o.timeout = d
		return nil
	}
}

// WithKeepalive sets the keepalive parameters.
func WithKeepalive(time, timeout time.Duration) Option {
	return func(o *clientOptions) error {
		if time <= 0 {
			return errors.NewInvalidInput("WithKeepalive", "keepalive time must be positive")
		}
		if timeout <= 0 {
			return errors.NewInvalidInput("WithKeepalive", "keepalive timeout must be positive")
		}
		o.keepaliveTime = time
		o.keepaliveTimeout = timeout
		return nil
	}
}

// WithRetry configures retry behavior.
func WithRetry(maxRetries int, backoff time.Duration) Option {
	return func(o *clientOptions) error {
		if maxRetries < 0 {
			return errors.NewInvalidInput("WithRetry", "maxRetries cannot be negative")
		}
		if backoff < 0 {
			return errors.NewInvalidInput("WithRetry", "backoff cannot be negative")
		}
		o.maxRetries = maxRetries
		o.retryBackoff = backoff
		return nil
	}
}

// WithMaxRetryBackoff sets the maximum retry backoff duration.
func WithMaxRetryBackoff(d time.Duration) Option {
	return func(o *clientOptions) error {
		if d <= 0 {
			return errors.NewInvalidInput("WithMaxRetryBackoff", "max backoff must be positive")
		}
		o.maxRetryBackoff = d
		return nil
	}
}

// WithTLS configures mTLS authentication using certificate files.
func WithTLS(cfg TLSConfig) Option {
	return func(o *clientOptions) error {
		tlsConfig, err := loadTLSConfig(cfg)
		if err != nil {
			return err
		}
		o.tlsEnabled = true
		o.tlsConfig = tlsConfig
		return nil
	}
}

// WithTLSConfig sets a custom tls.Config directly.
func WithTLSConfig(cfg *tls.Config) Option {
	return func(o *clientOptions) error {
		if cfg == nil {
			return errors.NewInvalidInput("WithTLSConfig", "tls config cannot be nil")
		}
		o.tlsEnabled = true
		o.tlsConfig = cfg
		return nil
	}
}

// WithInsecure disables TLS certificate verification.
// This is useful for development and testing with self-signed certificates.
// WARNING: Do not use in production - this disables security checks.
func WithInsecure() Option {
	return func(o *clientOptions) error {
		o.insecure = true
		return nil
	}
}

// WithConnectionPool enables connection pooling with the specified min/max sizes.
// Only applicable for gRPC transport.
func WithConnectionPool(minSize, maxSize int) Option {
	return func(o *clientOptions) error {
		if minSize <= 0 {
			return errors.NewInvalidInput("WithConnectionPool", "minSize must be positive")
		}
		if maxSize < minSize {
			return errors.NewInvalidInput("WithConnectionPool", "maxSize must be >= minSize")
		}
		o.poolEnabled = true
		o.poolMinSize = minSize
		o.poolMaxSize = maxSize
		return nil
	}
}

// WithPoolMaxAge sets the maximum age of pooled connections.
func WithPoolMaxAge(d time.Duration) Option {
	return func(o *clientOptions) error {
		if d <= 0 {
			return errors.NewInvalidInput("WithPoolMaxAge", "max age must be positive")
		}
		o.poolMaxAge = d
		return nil
	}
}

// WithPoolHealthInterval sets the health check interval for pooled connections.
func WithPoolHealthInterval(d time.Duration) Option {
	return func(o *clientOptions) error {
		if d <= 0 {
			return errors.NewInvalidInput("WithPoolHealthInterval", "interval must be positive")
		}
		o.poolHealthInterval = d
		return nil
	}
}

// WithREST configures the client to use REST transport.
func WithREST() Option {
	return func(o *clientOptions) error {
		o.transportType = TransportREST
		return nil
	}
}

// WithQUIC configures the client to use QUIC transport.
func WithQUIC() Option {
	return func(o *clientOptions) error {
		o.transportType = TransportQUIC
		return nil
	}
}

// WithUnix configures the client to use Unix socket transport (gRPC over UDS).
func WithUnix(socketPath string) Option {
	return func(o *clientOptions) error {
		if socketPath == "" {
			return errors.NewInvalidInput("WithUnix", "socket path cannot be empty")
		}
		o.transportType = TransportUnix
		o.unixSocketPath = socketPath
		return nil
	}
}

// WithEmbedded configures the client for in-process service access.
// The svc parameter should be of type service.QuicBaaS but uses interface{}
// to avoid import cycles. Type checking is performed when creating the transport.
func WithEmbedded(svc interface{}) Option {
	return func(o *clientOptions) error {
		if svc == nil {
			return errors.NewInvalidInput("WithEmbedded", "service cannot be nil")
		}
		o.transportType = TransportEmbedded
		o.embeddedService = svc
		return nil
	}
}

// WithMCP configures the client to use MCP (Model Context Protocol) transport.
// MCP uses JSON-RPC 2.0 over HTTP for communication with the server.
func WithMCP() Option {
	return func(o *clientOptions) error {
		o.transportType = TransportMCP
		return nil
	}
}

// WithMCPCredentials sets the credentials for MCP transport authentication.
// The transport will automatically authenticate using the quicbaas_login tool
// before making requests that require authentication.
func WithMCPCredentials(username, password string) Option {
	return func(o *clientOptions) error {
		o.mcpUsername = username
		o.mcpPassword = password
		return nil
	}
}

// WithMCPAuthToken sets a pre-existing authentication token for MCP transport.
// This is useful when the token has already been obtained (e.g., from a stored session).
func WithMCPAuthToken(token string) Option {
	return func(o *clientOptions) error {
		o.mcpAuthToken = token
		return nil
	}
}

// loadTLSConfig loads TLS configuration from files.
func loadTLSConfig(cfg TLSConfig) (*tls.Config, error) {
	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS13,
	}

	if cfg.ServerName != "" {
		tlsConfig.ServerName = cfg.ServerName
	}

	// Load client certificate if provided
	if cfg.CertFile != "" && cfg.KeyFile != "" {
		cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
		if err != nil {
			return nil, errors.Wrap(err, errors.CodeConfiguration, "loadTLSConfig",
				"failed to load client certificate")
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	// Load CA certificate if provided
	if cfg.CAFile != "" {
		caCert, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return nil, errors.Wrap(err, errors.CodeConfiguration, "loadTLSConfig",
				"failed to read CA certificate")
		}
		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			return nil, errors.New(errors.CodeConfiguration, "loadTLSConfig",
				"failed to parse CA certificate")
		}
		tlsConfig.RootCAs = caCertPool
	}

	return tlsConfig, nil
}
