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
	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Health Status Type and Constants
// =============================================================================

// HealthStatus represents the health state of a component or node.
type HealthStatus = service.HealthStatus

// Health status constants define the possible health states.
const (
	// HealthStatusUnknown indicates the health status has not been determined.
	HealthStatusUnknown = service.HealthStatusUnknown

	// HealthStatusHealthy indicates the component is fully operational.
	HealthStatusHealthy = service.HealthStatusHealthy

	// HealthStatusDegraded indicates the component is operational but impaired.
	HealthStatusDegraded = service.HealthStatusDegraded

	// HealthStatusUnhealthy indicates the component is not operational.
	HealthStatusUnhealthy = service.HealthStatusUnhealthy
)

// =============================================================================
// Watch Event Type and Constants
// =============================================================================

// WatchEventType represents the type of a watch event (put, delete, etc.).
type WatchEventType = service.WatchEventType

// Watch event type constants define the possible watch event types.
const (
	// WatchEventTypeUnspecified is the default/unset value for a watch event type.
	WatchEventTypeUnspecified = service.WatchEventTypeUnspecified

	// WatchEventTypePut indicates a key was created or updated.
	WatchEventTypePut = service.WatchEventTypePut

	// WatchEventTypeDelete indicates a key was deleted.
	WatchEventTypeDelete = service.WatchEventTypeDelete
)

// =============================================================================
// Server Mode Constants
// =============================================================================

// Server mode constants determine how the server operates.
const (
	// ServerModeStandalone runs without Raft consensus as a single node.
	ServerModeStandalone = config.ServerModeStandalone

	// ServerModeCluster runs with Raft consensus for distributed high availability.
	ServerModeCluster = config.ServerModeCluster

	// ServerModeDevCluster runs multiple local nodes for development testing.
	ServerModeDevCluster = config.ServerModeDevCluster
)

// =============================================================================
// Storage Backend Constants
// =============================================================================

// Storage backend constants determine the storage engine used.
const (
	// StorageBackendMemory uses in-memory storage with no persistence.
	// Data is lost on restart. Best for testing and ephemeral workloads.
	StorageBackendMemory = config.StorageBackendMemory

	// StorageBackendFile uses simple file-based storage where each
	// key-value pair is stored as a separate file.
	StorageBackendFile = config.StorageBackendFile

	// StorageBackendPebble uses PebbleDB for high-performance local storage.
	// Best for production single-node and cluster deployments.
	StorageBackendPebble = config.StorageBackendPebble
)
