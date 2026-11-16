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

package grpc

import (
	"context"
	stderrors "errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"

	"github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/common/circuitbreaker"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// Unix socket address prefixes.
const (
	// UnixPrefix is the standard gRPC Unix socket prefix.
	UnixPrefix = "unix://"
	// UnixAbstractPrefix is for abstract Unix sockets (Linux only).
	UnixAbstractPrefix = "unix-abstract://"
)

// IsUnixSocketAddress returns true if the address is a Unix domain socket path.
// Unix socket addresses can be:
//   - "unix:///path/to/socket" (standard gRPC format)
//   - "unix-abstract://name" (abstract Unix socket, Linux only)
//   - "/path/to/socket" (absolute path, will be converted to gRPC format)
//   - "./path/to/socket" (relative path, will be converted to gRPC format).
func IsUnixSocketAddress(address string) bool {
	if strings.HasPrefix(address, UnixPrefix) {
		return true
	}
	if strings.HasPrefix(address, UnixAbstractPrefix) {
		return true
	}
	// Check for Unix socket path ending in .sock
	// Supports both absolute paths (/path/to.sock) and relative paths (./path/to.sock)
	if strings.HasSuffix(address, ".sock") {
		// Absolute path
		if strings.HasPrefix(address, "/") {
			return true
		}
		// Relative path (starts with . or just a filename)
		if strings.HasPrefix(address, "./") || strings.HasPrefix(address, "../") || !strings.Contains(address, ":") {
			return true
		}
	}
	return false
}

// NormalizeUnixSocketAddress converts a Unix socket path to the gRPC-compatible format.
// Input formats:
//   - "/path/to/socket" -> "unix:///path/to/socket"
//   - "./path/to/socket" -> "unix:///absolute/path/to/socket"
//   - "unix:///path/to/socket" -> "unix:///path/to/socket" (unchanged)
//   - "localhost:50051" -> "localhost:50051" (unchanged, TCP).
func NormalizeUnixSocketAddress(address string) string {
	// Already in gRPC format
	if strings.HasPrefix(address, UnixPrefix) || strings.HasPrefix(address, UnixAbstractPrefix) {
		return address
	}
	// Absolute Unix socket path
	if strings.HasPrefix(address, "/") && strings.HasSuffix(address, ".sock") {
		return UnixPrefix + address
	}
	// Relative Unix socket path - convert to absolute
	if strings.HasSuffix(address, ".sock") && !strings.Contains(address, ":") {
		absPath, err := filepath.Abs(address)
		if err == nil {
			return UnixPrefix + absPath
		}
		// Fallback: just prepend unix:// if we can't get absolute path
		return UnixPrefix + address
	}
	// TCP address, return unchanged
	return address
}

// Client is a gRPC client for QuicBaaS that exposes SDK-based methods.
//
// The client supports two modes of operation:
//  1. Single connection mode (default): Uses a single gRPC connection with HTTP/2 multiplexing
//  2. Connection pool mode: Uses multiple connections for higher throughput (recommended for high-load scenarios)
//
// The client also supports two transport types:
//  1. TCP: Standard TCP connections (host:port format)
//  2. Unix socket: Unix domain socket connections (faster for local connections)
//
// Connection pool mode can provide significant performance improvements (2-5x) for write-heavy
// workloads by distributing load across multiple connections and reducing head-of-line blocking.
//
// An optional circuit breaker can be enabled via WithCircuitBreaker to protect against
// cascading failures in inter-node communication. When enabled, the circuit breaker
// wraps all outgoing RPC calls and trips open after consecutive failures exceed the
// configured threshold.
type Client struct {
	address string
	conn    *grpc.ClientConn
	client  proto.QuicBaaSClient
	pool    *ConnectionPool // Optional connection pool
	opts    *clientOptions
	logger  *logger.Logger
	isUnix  bool // True if using Unix socket transport

	// Circuit breaker for RPC call protection (nil if not configured)
	cb *circuitbreaker.CircuitBreaker

	// Connection state
	mu        sync.RWMutex
	connected bool
	closed    bool
}

// NewClient creates a new gRPC client for QuicBaaS.
//
// The client supports both TCP and Unix socket connections:
//   - TCP: "localhost:50051" or "host:port"
//   - Unix: "/tmp/quicbaas.sock" or "unix:///path/to/socket"
//
// TLS configuration is required for all connections. Use WithTLSConfig() to provide
// TLS credentials:
//
//	client, err := NewClient("localhost:50051",
//	    WithTLSConfig(&tls.Config{...}),
//	    WithConnectionPool(10, 20), // min 10, max 20 connections
//	)
func NewClient(address string, opts ...ClientOption) (*Client, error) {
	// Normalize Unix socket address to gRPC format
	address = NormalizeUnixSocketAddress(address)

	// Start with default options
	clientOpts := defaultClientOptions()

	// Apply custom options
	for _, opt := range opts {
		if err := opt(clientOpts); err != nil {
			return nil, errors.Wrapf(
				err,
				errors.ErrConfiguration,
				"grpc.NewClient",
				"failed to apply client option",
			)
		}
	}

	// Create logger if not provided
	if clientOpts.logger == nil {
		logConfig := logger.LogConfig{
			Level:  "info",
			Format: "json",
			Output: "stderr",
		}
		log, err := logger.New(logConfig)
		if err != nil {
			return nil, errors.Wrapf(
				err,
				errors.ErrConfiguration,
				"grpc.NewClient",
				"failed to create logger",
			)
		}
		clientOpts.logger = log
	}

	// Determine if using Unix socket
	isUnix := strings.HasPrefix(address, UnixPrefix) || strings.HasPrefix(address, UnixAbstractPrefix)

	client := &Client{
		address: address,
		opts:    clientOpts,
		logger:  clientOpts.logger,
		isUnix:  isUnix,
	}

	// Initialize circuit breaker if configured
	if clientOpts.circuitBreakerConfig != nil {
		cb, err := circuitbreaker.New(*clientOpts.circuitBreakerConfig)
		if err != nil {
			return nil, errors.Wrapf(
				err,
				errors.ErrConfiguration,
				"grpc.NewClient",
				"failed to create circuit breaker",
			)
		}
		client.cb = cb

		clientOpts.logger.WithFields(map[string]interface{}{
			"name":              clientOpts.circuitBreakerConfig.Name,
			"failure_threshold": clientOpts.circuitBreakerConfig.FailureThreshold,
			"success_threshold": clientOpts.circuitBreakerConfig.SuccessThreshold,
			"open_timeout":      clientOpts.circuitBreakerConfig.OpenTimeout.String(),
		}).Info("circuit breaker enabled for gRPC client")
	}

	// Check if connection pool should be used
	if clientOpts.usePool {
		// Create connection pool
		poolOpts := []PoolOption{
			WithPoolSize(clientOpts.poolMinSize, clientOpts.poolMaxSize),
			WithPoolClientOptions(clientOpts),
		}

		// Add optional pool options if configured
		if clientOpts.poolMaxAge > 0 {
			poolOpts = append(poolOpts, WithMaxConnectionAge(clientOpts.poolMaxAge))
		}
		if clientOpts.poolMaxRequests > 0 {
			poolOpts = append(poolOpts, WithMaxRequestsPerConn(clientOpts.poolMaxRequests))
		}
		if clientOpts.poolHealthCheck > 0 {
			poolOpts = append(poolOpts, WithHealthCheckInterval(clientOpts.poolHealthCheck))
		}

		pool, err := NewConnectionPool(address, poolOpts...)
		if err != nil {
			return nil, errors.Wrapf(
				err,
				errors.ErrConfiguration,
				"grpc.NewClient",
				"failed to create connection pool",
			)
		}
		client.pool = pool
		client.connected = true

		transport := "tcp"
		if isUnix {
			transport = "unix"
		}
		client.logger.WithFields(map[string]interface{}{
			"address":      address,
			"transport":    transport,
			"tls_enabled":  clientOpts.tlsEnabled,
			"pool_enabled": true,
			"pool_min":     clientOpts.poolMinSize,
			"pool_max":     clientOpts.poolMaxSize,
		}).Info("gRPC client created successfully with connection pool")
	} else {
		// Establish single connection
		if err := client.Connect(context.Background()); err != nil {
			return nil, err
		}

		transport := "tcp"
		if isUnix {
			transport = "unix"
		}
		client.logger.WithFields(map[string]interface{}{
			"address":     address,
			"transport":   transport,
			"tls_enabled": clientOpts.tlsEnabled,
		}).Info("gRPC client created successfully")
	}

	return client, nil
}

// CircuitBreaker returns the client's circuit breaker instance.
// Returns nil if no circuit breaker is configured.
func (c *Client) CircuitBreaker() *circuitbreaker.CircuitBreaker {
	return c.cb
}

// Connect establishes a connection to the QuicBaaS server.
func (c *Client) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.connected {
		return nil
	}

	if c.closed {
		return errors.Newf(
			errors.ErrInvalidInput,
			"grpc.Client.Connect",
			"client is closed",
		)
	}

	// Ensure client options are initialized
	if c.opts == nil {
		return errors.Newf(
			errors.ErrInvalidInput,
			"grpc.Client.Connect",
			"client options not initialized",
		)
	}

	// Build dial options
	dialOpts := c.opts.dialOptions

	// TLS configuration is required for all connections
	if !c.opts.tlsEnabled || c.opts.tlsConfig == nil {
		return errors.Newf(
			errors.ErrConfiguration,
			"grpc.Client.Connect",
			"TLS configuration is required for all connections",
		)
	}
	creds := credentials.NewTLS(c.opts.tlsConfig)
	dialOpts = append(dialOpts, grpc.WithTransportCredentials(creds))
	dialOpts = append(dialOpts, c.opts.metadataDialOptions()...)

	// Dial the server
	conn, err := grpc.DialContext(ctx, c.address, dialOpts...)
	if err != nil {
		return errors.Wrapf(
			err,
			errors.ErrUnavailable,
			"grpc.Client.Connect",
			"failed to connect to server: %s", c.address,
		)
	}

	c.conn = conn
	c.client = proto.NewQuicBaaSClient(conn)
	c.connected = true

	return nil
}

// Close closes the client connection.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil
	}

	c.closed = true
	c.connected = false

	// Close connection pool if used
	if c.pool != nil {
		return c.pool.Close()
	}

	// Close single connection
	if c.conn != nil {
		return c.conn.Close()
	}

	return nil
}

// Disconnect is an alias for Close for backwards compatibility.
func (c *Client) Disconnect() error {
	return c.Close()
}

// Conn returns the underlying gRPC client connection.
// Returns nil if the client is not connected.
func (c *Client) Conn() *grpc.ClientConn {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.conn
}

// isConnected returns true if the client is connected and not closed.
func (c *Client) isConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.connected && !c.closed
}

// ensureConnected verifies the client is connected.
func (c *Client) ensureConnected(ctx context.Context) error {
	c.mu.RLock()
	connected := c.connected
	closed := c.closed
	c.mu.RUnlock()

	if closed {
		return errors.Newf(
			errors.ErrInvalidInput,
			"grpc.Client",
			"client has been closed",
		)
	}

	if !connected {
		return c.Connect(ctx)
	}

	return nil
}

// getClient returns the proto client (from pool or single connection).
func (c *Client) getClient() (proto.QuicBaaSClient, error) {
	// Use pool if configured
	if c.pool != nil {
		pooledConn, err := c.pool.Get()
		if err != nil {
			return nil, err
		}
		// Use the client from the pooled connection
		return pooledConn.client, nil
	}

	// Use single connection
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.client == nil {
		return nil, errors.Newf(
			errors.ErrInvalidInput,
			"grpc.Client",
			"client is not connected",
		)
	}

	return c.client, nil
}

// addMetadata adds configured metadata to the context.
func (c *Client) addMetadata(ctx context.Context) context.Context {
	if c.opts.md != nil {
		return metadata.NewOutgoingContext(ctx, c.opts.md)
	}
	if len(c.opts.metadata) == 0 {
		return ctx
	}
	// Lazy initialization of metadata
	md := metadata.New(c.opts.metadata)
	c.opts.md = md
	return metadata.NewOutgoingContext(ctx, md)
}

// executeRPC executes an RPC call with circuit breaker protection (if configured)
// and retry-with-backoff. This is the primary entry point for all outgoing RPC calls
// that need resilience.
//
// When no circuit breaker is configured, this is equivalent to calling retryWithBackoff
// directly. When a circuit breaker is configured, the entire retry sequence is wrapped
// so that the circuit breaker tracks per-attempt outcomes and can trip open to
// fail-fast when the remote is consistently unavailable.
func (c *Client) executeRPC(ctx context.Context, fn func() error) error {
	rpcFn := func() error {
		return retryWithBackoff(
			ctx,
			c.opts.maxRetries,
			c.opts.retryBackoff,
			c.opts.maxRetryBackoff,
			fn,
		)
	}

	if c.cb != nil {
		return c.cb.Execute(rpcFn)
	}

	return rpcFn()
}

// RebalanceCluster initiates a cluster rebalance operation.
func (c *Client) RebalanceCluster(ctx context.Context, req *proto.RebalanceClusterRequest) (*proto.RebalanceClusterResponse, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.opts.timeout)
		defer cancel()
	}

	ctx = c.addMetadata(ctx)

	// Get client (from pool or single connection)
	client, err := c.getClient()
	if err != nil {
		return nil, convertGRPCError(err, "RebalanceCluster")
	}

	var resp *proto.RebalanceClusterResponse
	err = c.executeRPC(ctx, func() error {
		var rpcErr error
		resp, rpcErr = client.RebalanceCluster(ctx, req)
		return rpcErr
	})
	if err != nil {
		return nil, convertGRPCError(err, "RebalanceCluster")
	}

	return resp, nil
}

// GetRebalanceProgress gets the progress of a rebalance operation.
func (c *Client) GetRebalanceProgress(ctx context.Context, req *proto.GetRebalanceProgressRequest) (*proto.GetRebalanceProgressResponse, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.opts.timeout)
		defer cancel()
	}

	ctx = c.addMetadata(ctx)

	// Get client (from pool or single connection)
	client, err := c.getClient()
	if err != nil {
		return nil, convertGRPCError(err, "GetRebalanceProgress")
	}

	var resp *proto.GetRebalanceProgressResponse
	err = c.executeRPC(ctx, func() error {
		var rpcErr error
		resp, rpcErr = client.GetRebalanceProgress(ctx, req)
		return rpcErr
	})
	if err != nil {
		return nil, convertGRPCError(err, "GetRebalanceProgress")
	}

	return resp, nil
}

// UpgradeNode initiates a rolling upgrade for a specific node.
func (c *Client) UpgradeNode(ctx context.Context, req *proto.UpgradeNodeRequest) (*proto.UpgradeNodeResponse, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.opts.timeout)
		defer cancel()
	}

	ctx = c.addMetadata(ctx)

	// Get client (from pool or single connection)
	client, err := c.getClient()
	if err != nil {
		return nil, convertGRPCError(err, "UpgradeNode")
	}

	var resp *proto.UpgradeNodeResponse
	err = c.executeRPC(ctx, func() error {
		var rpcErr error
		resp, rpcErr = client.UpgradeNode(ctx, req)
		return rpcErr
	})
	if err != nil {
		return nil, convertGRPCError(err, "UpgradeNode")
	}

	return resp, nil
}

// GetUpgradeStatus gets the status of an ongoing upgrade operation.
func (c *Client) GetUpgradeStatus(ctx context.Context, req *proto.GetUpgradeStatusRequest) (*proto.GetUpgradeStatusResponse, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.opts.timeout)
		defer cancel()
	}

	ctx = c.addMetadata(ctx)

	// Get client (from pool or single connection)
	client, err := c.getClient()
	if err != nil {
		return nil, convertGRPCError(err, "GetUpgradeStatus")
	}

	var resp *proto.GetUpgradeStatusResponse
	err = c.executeRPC(ctx, func() error {
		var rpcErr error
		resp, rpcErr = client.GetUpgradeStatus(ctx, req)
		return rpcErr
	})
	if err != nil {
		return nil, convertGRPCError(err, "GetUpgradeStatus")
	}

	return resp, nil
}

// convertGRPCError converts gRPC errors to QuicBaaS errors.
func convertGRPCError(err error, operation string) error {
	if err == nil {
		return nil
	}

	// Check if it's already a QuicBaaS error
	var qbErr *errors.QBError
	if stderrors.As(err, &qbErr) {
		return err
	}

	// Convert gRPC status to QuicBaaS error
	s, ok := FromGRPCError(err)
	if !ok {
		return errors.Wrapf(
			err,
			errors.ErrInternal,
			fmt.Sprintf("grpc.Client.%s", operation),
			"unknown gRPC error",
		)
	}

	return errors.Wrapf(
		err,
		s,
		fmt.Sprintf("grpc.Client.%s", operation),
		"gRPC operation failed",
	)
}
