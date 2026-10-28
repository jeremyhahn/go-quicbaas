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

// Package grpc provides gRPC client and server implementations for QuicBaaS.
package grpc

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"

	"github.com/jeremyhahn/go-quicbaas/pkg/common/circuitbreaker"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// ClientOption is a function that configures a Client.
// This follows the functional options pattern for flexible, readable configuration.
type ClientOption func(*clientOptions) error

// clientOptions holds all configuration options for the gRPC client.
type clientOptions struct {
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
	certFile   string
	keyFile    string
	caFile     string

	// gRPC dial options
	dialOptions []grpc.DialOption

	// Logger
	logger *logger.Logger

	// Metadata
	metadata map[string]string
	md       metadata.MD

	// Connection pool settings
	usePool         bool
	poolMinSize     int
	poolMaxSize     int
	poolMaxAge      time.Duration
	poolMaxRequests uint64
	poolHealthCheck time.Duration

	// Circuit breaker settings
	circuitBreakerConfig *circuitbreaker.Config
}

// defaultClientOptions returns a clientOptions struct with sensible defaults.
func defaultClientOptions() *clientOptions {
	return &clientOptions{
		timeout:          30 * time.Second,
		keepaliveTime:    30 * time.Second,
		keepaliveTimeout: 10 * time.Second,
		maxRetries:       3,
		retryBackoff:     100 * time.Millisecond,
		maxRetryBackoff:  5 * time.Second,
		tlsEnabled:       false,
		dialOptions:      make([]grpc.DialOption, 0),
		metadata:         make(map[string]string),
		usePool:          false,
		poolMinSize:      5,
		poolMaxSize:      20,
		poolMaxAge:       30 * time.Minute,
		poolMaxRequests:  100000,
		poolHealthCheck:  30 * time.Second,
	}
}

// WithTimeout sets the default timeout for RPC calls.
func WithTimeout(timeout time.Duration) ClientOption {
	return func(opts *clientOptions) error {
		if timeout <= 0 {
			return errors.Newf(
				errors.ErrInvalidInput,
				"grpc.WithTimeout",
				"timeout must be positive, got: %v", timeout,
			)
		}
		opts.timeout = timeout
		return nil
	}
}

// WithKeepalive sets keepalive parameters for the connection.
func WithKeepalive(time, timeout time.Duration) ClientOption {
	return func(opts *clientOptions) error {
		if time <= 0 || timeout <= 0 {
			return errors.Newf(
				errors.ErrInvalidInput,
				"grpc.WithKeepalive",
				"keepalive time and timeout must be positive",
			)
		}
		opts.keepaliveTime = time
		opts.keepaliveTimeout = timeout
		return nil
	}
}

// WithRetry configures retry behavior.
func WithRetry(maxRetries int, backoff time.Duration) ClientOption {
	return func(opts *clientOptions) error {
		if maxRetries < 0 {
			return errors.Newf(
				errors.ErrInvalidInput,
				"grpc.WithRetry",
				"maxRetries cannot be negative, got: %d", maxRetries,
			)
		}
		if backoff <= 0 {
			return errors.Newf(
				errors.ErrInvalidInput,
				"grpc.WithRetry",
				"backoff must be positive, got: %v", backoff,
			)
		}
		opts.maxRetries = maxRetries
		opts.retryBackoff = backoff
		return nil
	}
}

// WithMaxRetryBackoff sets the maximum backoff duration for retries.
func WithMaxRetryBackoff(maxBackoff time.Duration) ClientOption {
	return func(opts *clientOptions) error {
		if maxBackoff <= 0 {
			return errors.Newf(
				errors.ErrInvalidInput,
				"grpc.WithMaxRetryBackoff",
				"maxBackoff must be positive, got: %v", maxBackoff,
			)
		}
		opts.maxRetryBackoff = maxBackoff
		return nil
	}
}

// WithTLS enables TLS with the specified certificate, key, and CA files.
func WithTLS(certFile, keyFile, caFile string) ClientOption {
	return func(opts *clientOptions) error {
		if certFile == "" || keyFile == "" {
			return errors.Newf(
				errors.ErrInvalidInput,
				"grpc.WithTLS",
				"certFile and keyFile are required",
			)
		}

		opts.tlsEnabled = true
		opts.certFile = certFile
		opts.keyFile = keyFile
		opts.caFile = caFile

		// Load client certificate
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return errors.Wrapf(
				err,
				errors.ErrConfiguration,
				"grpc.WithTLS",
				"failed to load client certificate",
			)
		}

		// Create TLS config
		tlsConfig := &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS13, // Enforce TLS 1.3+
		}

		// Load CA certificate if provided
		if caFile != "" {
			caCert, err := os.ReadFile(caFile)
			if err != nil {
				return errors.Wrapf(
					err,
					errors.ErrConfiguration,
					"grpc.WithTLS",
					"failed to read CA certificate",
				)
			}

			caCertPool := x509.NewCertPool()
			if !caCertPool.AppendCertsFromPEM(caCert) {
				return errors.Newf(
					errors.ErrConfiguration,
					"grpc.WithTLS",
					"failed to append CA certificate",
				)
			}
			tlsConfig.RootCAs = caCertPool
		}

		opts.tlsConfig = tlsConfig
		return nil
	}
}

// WithTLSConfig sets a custom TLS configuration.
func WithTLSConfig(tlsConfig *tls.Config) ClientOption {
	return func(opts *clientOptions) error {
		if tlsConfig == nil {
			return errors.Newf(
				errors.ErrInvalidInput,
				"grpc.WithTLSConfig",
				"tlsConfig cannot be nil",
			)
		}
		opts.tlsEnabled = true
		opts.tlsConfig = tlsConfig
		return nil
	}
}

// WithLogger sets a custom logger.
func WithLogger(log *logger.Logger) ClientOption {
	return func(opts *clientOptions) error {
		if log == nil {
			return errors.Newf(
				errors.ErrInvalidInput,
				"grpc.WithLogger",
				"logger cannot be nil",
			)
		}
		opts.logger = log
		return nil
	}
}

// WithMetadata adds custom metadata to all RPC calls.
func WithMetadata(key, value string) ClientOption {
	return func(opts *clientOptions) error {
		if key == "" {
			return errors.Newf(
				errors.ErrInvalidInput,
				"grpc.WithMetadata",
				"metadata key cannot be empty",
			)
		}
		opts.metadata[key] = value
		// also populate prebuilt metadata.MD for cheaper reuse
		if opts.md == nil {
			opts.md = metadata.MD{}
		}
		opts.md.Set(key, value)
		return nil
	}
}

// bearerCredentials presents a platform bearer as the "authorization"
// metadata of every RPC, unary or streaming, made over the connection.
type bearerCredentials struct {
	authorization string
}

// GetRequestMetadata returns the "Bearer <token>" authorization header.
func (b bearerCredentials) GetRequestMetadata(context.Context, ...string) (map[string]string, error) {
	return map[string]string{"authorization": b.authorization}, nil
}

// RequireTransportSecurity reports that the bearer is only sent over TLS.
func (bearerCredentials) RequireTransportSecurity() bool {
	return true
}

// WithBearerToken presents token as the platform bearer on every RPC the
// client makes. It is installed as per-RPC credentials on the connection, so
// it reaches every service stub, not only the calls that add client metadata.
func WithBearerToken(token string) ClientOption {
	return func(opts *clientOptions) error {
		if token == "" {
			return errors.Newf(
				errors.ErrInvalidInput,
				"grpc.WithBearerToken",
				"bearer token cannot be empty",
			)
		}
		opts.dialOptions = append(opts.dialOptions,
			grpc.WithPerRPCCredentials(bearerCredentials{authorization: "Bearer " + token}))
		return nil
	}
}

// WithDialOptions adds custom gRPC dial options.
func WithDialOptions(dialOpts ...grpc.DialOption) ClientOption {
	return func(opts *clientOptions) error {
		opts.dialOptions = append(opts.dialOptions, dialOpts...)
		return nil
	}
}

// WithConnectionPool enables connection pooling with the specified min and max pool sizes.
//
// Connection pooling provides significant performance benefits for high-throughput workloads
// by distributing load across multiple connections and reducing head-of-line blocking.
//
// Performance improvements:
//   - Write operations: 2-5x throughput improvement
//   - Read operations: 1.5-3x throughput improvement
//   - Reduced latency variance under load
//
// Example:
//
//	client, err := NewClient("localhost:50051",
//	    WithConnectionPool(10, 20),  // 10-20 connections
//	    WithTLS("cert.pem", "key.pem", "ca.pem"),
//	)
func WithConnectionPool(minSize, maxSize int) ClientOption {
	return func(opts *clientOptions) error {
		if minSize < 1 {
			return errors.Newf(
				errors.ErrInvalidInput,
				"grpc.WithConnectionPool",
				"minSize must be at least 1, got: %d", minSize,
			)
		}
		if maxSize < minSize {
			return errors.Newf(
				errors.ErrInvalidInput,
				"grpc.WithConnectionPool",
				"maxSize must be >= minSize, got: minSize=%d maxSize=%d", minSize, maxSize,
			)
		}
		opts.usePool = true
		opts.poolMinSize = minSize
		opts.poolMaxSize = maxSize
		return nil
	}
}

// WithPoolMaxConnectionAge sets the maximum age for pooled connections.
// Connections older than this duration will be recycled automatically.
//
// Default: 30 minutes
//
// Use shorter durations (5-10 minutes) if you experience:
//   - Connection staleness issues
//   - Server-side connection timeout problems
//   - Load balancer connection draining
//
// Use longer durations (1-2 hours) for:
//   - Stable, long-lived connections
//   - Reduced connection churn overhead
func WithPoolMaxConnectionAge(duration time.Duration) ClientOption {
	return func(opts *clientOptions) error {
		if duration <= 0 {
			return errors.Newf(
				errors.ErrInvalidInput,
				"grpc.WithPoolMaxConnectionAge",
				"duration must be positive, got: %v", duration,
			)
		}
		opts.poolMaxAge = duration
		return nil
	}
}

// WithPoolMaxRequestsPerConn sets the maximum number of requests per pooled connection.
// After serving this many requests, the connection will be recycled.
//
// Default: 100,000 requests
//
// Use lower values (10,000-50,000) to:
//   - Prevent resource leaks in long-running connections
//   - Force periodic connection refresh
//   - Distribute load more evenly
//
// Use higher values (500,000+) for:
//   - Reduced connection overhead
//   - Maximum performance when connections are stable
func WithPoolMaxRequestsPerConn(count uint64) ClientOption {
	return func(opts *clientOptions) error {
		if count == 0 {
			return errors.Newf(
				errors.ErrInvalidInput,
				"grpc.WithPoolMaxRequestsPerConn",
				"count must be positive, got: %d", count,
			)
		}
		opts.poolMaxRequests = count
		return nil
	}
}

// WithPoolHealthCheckInterval sets the interval for periodic health checks on pooled connections.
// Set to 0 to disable periodic health checks (not recommended for production).
//
// Default: 30 seconds
//
// Use shorter intervals (10-15 seconds) for:
//   - Mission-critical systems requiring immediate failure detection
//   - Unstable network environments
//
// Use longer intervals (60-120 seconds) for:
//   - Stable production environments
//   - Reduced health check overhead
func WithPoolHealthCheckInterval(duration time.Duration) ClientOption {
	return func(opts *clientOptions) error {
		if duration < 0 {
			return errors.Newf(
				errors.ErrInvalidInput,
				"grpc.WithPoolHealthCheckInterval",
				"duration cannot be negative, got: %v", duration,
			)
		}
		opts.poolHealthCheck = duration
		return nil
	}
}

// WithCircuitBreaker enables circuit breaker protection on outgoing RPC calls.
//
// The circuit breaker tracks consecutive failures and opens the circuit when the
// failure threshold is reached, rejecting subsequent calls immediately with
// ErrCircuitOpen until the open timeout elapses. After the timeout, limited probe
// requests are allowed through (HalfOpen state). If probes succeed, the circuit
// closes; if they fail, it reopens.
//
// Example:
//
//	client, err := NewClient("localhost:50051",
//	    WithCircuitBreaker(circuitbreaker.Config{
//	        Name:             "grpc-node-1",
//	        FailureThreshold: 5,
//	        SuccessThreshold: 2,
//	        OpenTimeout:      30 * time.Second,
//	    }),
//	    WithTLSConfig(tlsCfg),
//	)
func WithCircuitBreaker(cfg circuitbreaker.Config) ClientOption {
	return func(opts *clientOptions) error {
		// Validate eagerly so errors surface at construction time
		if cfg.Name == "" {
			return errors.New(
				errors.ErrInvalidInput,
				"grpc.WithCircuitBreaker",
				"circuit breaker name must not be empty",
			)
		}
		if cfg.FailureThreshold <= 0 {
			return errors.Newf(
				errors.ErrInvalidInput,
				"grpc.WithCircuitBreaker",
				"failure threshold must be positive, got: %d", cfg.FailureThreshold,
			)
		}
		if cfg.SuccessThreshold <= 0 {
			return errors.Newf(
				errors.ErrInvalidInput,
				"grpc.WithCircuitBreaker",
				"success threshold must be positive, got: %d", cfg.SuccessThreshold,
			)
		}
		if cfg.OpenTimeout <= 0 {
			return errors.Newf(
				errors.ErrInvalidInput,
				"grpc.WithCircuitBreaker",
				"open timeout must be positive, got: %v", cfg.OpenTimeout,
			)
		}
		opts.circuitBreakerConfig = &cfg
		return nil
	}
}

// buildDialOptions constructs the final set of dial options.
func (opts *clientOptions) buildDialOptions() ([]grpc.DialOption, error) {
	dialOptions := make([]grpc.DialOption, 0, len(opts.dialOptions)+5)

	// Add transport credentials - TLS is required
	switch {
	case opts.tlsConfig != nil:
		// Use provided TLS config
		creds := credentials.NewTLS(opts.tlsConfig)
		dialOptions = append(dialOptions, grpc.WithTransportCredentials(creds))
	case opts.certFile != "" && opts.keyFile != "":
		// Load TLS credentials from files
		tlsCfg := &TLSConfig{
			Enabled:  true,
			CertFile: opts.certFile,
			KeyFile:  opts.keyFile,
			CAFile:   opts.caFile,
		}
		creds, err := LoadClientTLSCredentials(tlsCfg)
		if err != nil {
			return nil, errors.Wrapf(
				err,
				errors.ErrConfiguration,
				"grpc.buildDialOptions",
				"failed to load TLS credentials",
			)
		}
		dialOptions = append(dialOptions, grpc.WithTransportCredentials(creds))
	default:
		return nil, errors.Newf(
			errors.ErrConfiguration,
			"grpc.buildDialOptions",
			"TLS configuration is required",
		)
	}

	// Add keepalive parameters
	kaParams := keepalive.ClientParameters{
		Time:                opts.keepaliveTime,
		Timeout:             opts.keepaliveTimeout,
		PermitWithoutStream: true,
	}
	// PERFORMANCE OPTIMIZATION: the default gRPC HTTP/2 flow control windows
	// are 64KB, far too small for high-throughput workloads; 16MB windows keep
	// flow control from becoming the bottleneck. Larger read/write buffers cut
	// syscalls, and the larger message sizes admit 10MB payloads.
	dialOptions = append(dialOptions,
		grpc.WithKeepaliveParams(kaParams),
		grpc.WithInitialWindowSize(16*1024*1024),     // 16 MB per stream
		grpc.WithInitialConnWindowSize(16*1024*1024), // 16 MB per connection
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(10*1024*1024), // 10 MB receive
			grpc.MaxCallSendMsgSize(10*1024*1024), // 10 MB send
		),
		grpc.WithWriteBufferSize(512*1024), // 512 KB write buffer
		grpc.WithReadBufferSize(512*1024),  // 512 KB read buffer
	)

	// Add default service config for retry and load balancing
	// Convert durations to seconds with decimal precision (required by gRPC service config)
	initialBackoffSeconds := opts.retryBackoff.Seconds()
	maxBackoffSeconds := opts.maxRetryBackoff.Seconds()

	serviceConfig := fmt.Sprintf(`{
		"loadBalancingConfig": [{"round_robin":{}}],
		"methodConfig": [{
			"name": [{"service": "quicbaas.v1.QuicBaaS"}],
			"retryPolicy": {
				"maxAttempts": %d,
				"initialBackoff": "%.3fs",
				"maxBackoff": "%.3fs",
				"backoffMultiplier": 2.0,
				"retryableStatusCodes": ["UNAVAILABLE", "DEADLINE_EXCEEDED", "RESOURCE_EXHAUSTED"]
			}
		}]
	}`, opts.maxRetries, initialBackoffSeconds, maxBackoffSeconds)

	dialOptions = append(dialOptions, grpc.WithDefaultServiceConfig(serviceConfig))

	// Add custom dial options
	dialOptions = append(dialOptions, opts.dialOptions...)

	return dialOptions, nil
}
