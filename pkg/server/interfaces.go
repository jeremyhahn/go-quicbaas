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

// Package server provides the main orchestration layer for QuicBaaS.
// This file defines all interfaces needed by the Server for dependency injection,
// enabling comprehensive unit testing through mock implementations.
package server

import (
	"context"
	"net"
)

// =============================================================================
// Core Component Interfaces
// =============================================================================

// StorageInterface defines the storage operations needed by Server.
// Server primarily needs lifecycle management and metrics for health checks.
// Business operations are handled through the service layer.
type StorageInterface interface {
	// Metrics returns storage engine metrics for health monitoring.
	// The returned type is implementation-specific (e.g., *pebble.Metrics).
	Metrics() interface{}

	// Close releases all storage resources and persists any pending writes.
	Close() error
}

// IndexManagerInterface defines index operations needed by Server.
// Server only needs lifecycle management - query operations are in service layer.
type IndexManagerInterface interface {
	// Close releases all index resources.
	Close() error
}

// DiscoveryManagerInterface defines discovery operations for automatic cluster formation.
// Used to discover peer nodes in Kubernetes, Consul, DNS, or static configurations.
type DiscoveryManagerInterface interface {
	// GetPeers returns currently discovered peer addresses.
	// Used for health checks and cluster topology visibility.
	GetPeers() ([]string, error)

	// Close stops discovery and releases resources.
	Close() error
}

// BootstrapperInterface defines cluster bootstrap operations.
// Used for initial cluster formation and membership setup.
type BootstrapperInterface interface {
	// Bootstrap performs cluster initialization.
	// Coordinates with discovered peers to form initial groups.
	Bootstrap(ctx context.Context) error
}

// =============================================================================
// API Server Interfaces
// =============================================================================

// GRPCServerInterface defines gRPC server operations needed by Server.
// Abstracts the google.golang.org/grpc.Server for lifecycle management.
type GRPCServerInterface interface {
	// Serve starts serving gRPC requests on the provided listener.
	// Blocks until Stop() or GracefulStop() is called.
	Serve(lis net.Listener) error

	// GracefulStop gracefully stops the server.
	// Active RPCs are allowed to complete before shutdown.
	GracefulStop()

	// Stop immediately stops the server.
	// Active RPCs are terminated.
	Stop()
}

// GRPCAPIInterface defines QuicBaaS gRPC service operations needed by Server.
// Used for configuration after service creation.
type GRPCAPIInterface interface {
	// SetShardRouter configures the shard routing strategy.
	// Must be called before serving requests.
	SetShardRouter(router ShardRouterInterface)
}

// RESTServerInterface defines REST API server operations needed by Server.
// Provides HTTP endpoints for all QuicBaaS operations.
type RESTServerInterface interface {
	// Start starts the HTTP server on the specified address.
	// Returns immediately after server starts listening.
	Start(address string) error

	// Shutdown gracefully shuts down the server.
	// Active requests are allowed to complete within context deadline.
	Shutdown(ctx context.Context) error

	// IsStarted returns true if the server is running.
	IsStarted() bool

	// IsClosed returns true if the server has been shut down.
	IsClosed() bool

	// SetShardRouter configures the shard routing strategy.
	// Must be called before serving requests.
	SetShardRouter(router ShardRouterInterface)
}

// ShardRouterInterface provides shard selection and routing logic.
// Determines which shard should handle a given key or entity.
type ShardRouterInterface interface {
	// SetEntityShard pins an entity type to a specific shard.
	// All entities of this type will route to the specified shard.
	SetEntityShard(entityType string, shardID uint64)

	// GetShardForKey returns the shard ID for the given key.
	GetShardForKey(key []byte) uint64

	// GetShardForEntity returns the shard ID for the given entity type.
	GetShardForEntity(entityType string) uint64

	// ListShards returns all shard IDs.
	ListShards() []uint64
}

// =============================================================================
// Health Checking Interface
// =============================================================================

// HealthCheckerInterface defines health checking operations.
// Monitors all server components and provides overall health status.
type HealthCheckerInterface interface {
	// CheckGRPC returns health status of the gRPC server.
	CheckGRPC() ComponentHealthStatus

	// CheckREST returns health status of the REST server.
	CheckREST() ComponentHealthStatus

	// GetOverallHealth aggregates all component health checks.
	// Returns overall status and individual component statuses.
	GetOverallHealth() *OverallHealthStatus

	// IsHealthy returns true if all components are healthy.
	IsHealthy() bool

	// IsReady returns true if the server is ready to serve requests.
	// Checks critical components (gRPC, REST).
	IsReady() bool
}

// ComponentHealthStatus represents the health status of a single component.
type ComponentHealthStatus struct {
	Name      string
	Status    HealthStatusType
	Message   string
	LastCheck int64
	Error     string
}

// OverallHealthStatus represents the complete health status of the server.
type OverallHealthStatus struct {
	Status     HealthStatusType
	Timestamp  int64
	Uptime     int64
	Components map[string]ComponentHealthStatus
}

// HealthStatusType represents component health state.
type HealthStatusType string

const (
	HealthStatusTypeHealthy   HealthStatusType = "healthy"
	HealthStatusTypeDegraded  HealthStatusType = "degraded"
	HealthStatusTypeUnhealthy HealthStatusType = "unhealthy"
	HealthStatusTypeUnknown   HealthStatusType = "unknown"
)

// =============================================================================
// Logger Interface
// =============================================================================

// LoggerInterface defines logging operations needed by Server.
// Abstracts the logger for dependency injection.
type LoggerInterface interface {
	// Debug logs a debug message.
	Debug(msg string)

	// Debugf logs a formatted debug message.
	Debugf(format string, args ...interface{})

	// Info logs an info message.
	Info(msg string)

	// Infof logs a formatted info message.
	Infof(format string, args ...interface{})

	// Warn logs a warning message.
	Warn(msg string)

	// Warnf logs a formatted warning message.
	Warnf(format string, args ...interface{})

	// Error logs an error message.
	Error(msg string)

	// Errorf logs a formatted error message.
	Errorf(format string, args ...interface{})

	// WithError returns a logger with the error attached.
	WithError(err error) LoggerInterface

	// WithField returns a logger with the field attached.
	WithField(key string, value interface{}) LoggerInterface
}

// =============================================================================
// Factory Interfaces (for creating components during startup)
// =============================================================================

// StorageFactory creates a storage engine instance.
type StorageFactory func(config interface{}, logger interface{}) (StorageInterface, error)

// IndexManagerFactory creates an index manager instance.
type IndexManagerFactory func(storage interface{}, logger interface{}) (IndexManagerInterface, error)

// DiscoveryManagerFactory creates a discovery manager instance.
type DiscoveryManagerFactory func(config interface{}, logger interface{}) (DiscoveryManagerInterface, error)

// BootstrapperFactory creates a bootstrapper instance.
type BootstrapperFactory func(config interface{}, discovery interface{}, logger interface{}) (BootstrapperInterface, error)

// =============================================================================
// Server Dependencies for Dependency Injection
// =============================================================================

// ServerDependencies holds all injectable dependencies for the Server.
// Used by NewServerWithDependencies to enable unit testing with mocks.
type ServerDependencies struct {
	// Core components
	Storage      StorageInterface
	IndexManager IndexManagerInterface

	// Bootstrap components
	DiscoveryManager DiscoveryManagerInterface
	Bootstrapper     BootstrapperInterface

	// API components
	GRPCServer GRPCServerInterface
	GRPCAPI    GRPCAPIInterface
	RESTServer RESTServerInterface

	// Health checking
	HealthChecker HealthCheckerInterface

	// Listener for gRPC
	GRPCListener net.Listener
}
