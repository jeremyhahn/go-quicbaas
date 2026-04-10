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

package interfaces

// Config defines the interface for configuration management.
// It provides access to all application configuration settings.
type Config interface {
	// Server configuration

	// GetHTTPAddress returns the HTTP server bind address.
	GetHTTPAddress() string

	// GetGRPCAddress returns the gRPC server bind address.
	GetGRPCAddress() string

	// GetNodeID returns the unique identifier for this node.
	GetNodeID() uint64

	// Storage configuration

	// GetDataDir returns the data directory path.
	GetDataDir() string

	// GetWALDir returns the write-ahead log directory path.
	GetWALDir() string

	// Raft configuration

	// GetRaftAddress returns the Raft communication address.
	GetRaftAddress() string

	// GetRaftHeartbeatRTT returns the Raft heartbeat RTT in milliseconds.
	GetRaftHeartbeatRTT() uint64

	// GetRaftElectionRTT returns the Raft election timeout RTT.
	GetRaftElectionRTT() uint64

	// Cluster configuration

	// GetInitialMembers returns the initial cluster members.
	// Returns a map of nodeID to Raft address.
	GetInitialMembers() map[uint64]string

	// Logging configuration

	// GetLogLevel returns the logging level.
	GetLogLevel() string

	// GetLogFormat returns the log format (json, text, etc.).
	GetLogFormat() string

	// Validation

	// Validate checks if the configuration is valid.
	// Returns an error if validation fails.
	Validate() error
}

// ConfigLoader defines the interface for loading configuration.
// It supports multiple configuration sources (files, environment, etc.).
type ConfigLoader interface {
	// Load loads configuration from all configured sources.
	// Returns an error if loading or parsing fails.
	Load() (Config, error)

	// LoadFromFile loads configuration from a specific file.
	LoadFromFile(path string) (Config, error)

	// LoadFromEnv loads configuration from environment variables.
	LoadFromEnv() (Config, error)

	// Reload reloads configuration from all sources.
	// Useful for hot-reloading configuration changes.
	Reload() (Config, error)
}
