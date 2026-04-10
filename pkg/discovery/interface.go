//go:build raft

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

// Package discovery provides cluster node discovery mechanisms for QuicBaaS.
// It enables automatic cluster bootstrapping without manual peer configuration.
package discovery

import (
	"context"
	"time"
)

// Peer represents a discovered node in the cluster.
type Peer struct {
	// NodeID is the unique identifier for this node
	NodeID uint64

	// Address is the host:port for this node's Raft communication
	Address string

	// RaftAddr is an alias for Address (for compatibility)
	RaftAddr string

	// Metadata contains optional key-value pairs about this peer
	// Can include things like region, zone, version, etc.
	Metadata map[string]string

	// LastSeen is the timestamp when this peer was last discovered
	LastSeen time.Time

	// TTL is the time-to-live for this peer record
	// After TTL expires, the peer should be re-discovered
	TTL time.Duration
}

// Method defines the interface for different discovery methods
// Each method (static, DNS, Kubernetes, multicast, etc.) implements this interface.
type Method interface {
	// Name returns the human-readable name of this discovery method
	Name() string

	// Discover attempts to find peers in the cluster
	// Returns list of discovered peers or error if discovery fails
	Discover(ctx context.Context) ([]Peer, error)

	// Register announces this node's presence to other peers
	// Self contains this node's information to advertise
	Register(ctx context.Context, self Peer) error

	// Deregister removes this node's registration
	// Should be called on graceful shutdown
	Deregister(ctx context.Context) error

	// Close releases any resources held by this discovery method
	Close() error
}

// Config contains configuration for the discovery manager.
type Config struct {
	// Methods is the ordered list of discovery methods to try
	// The manager will try each method in order until one succeeds
	Methods []Method

	// MinPeers is the minimum number of peers required to form a cluster
	// Typically this should be (cluster_size / 2) + 1 for Raft quorum
	MinPeers int

	// DiscoveryTimeout is the maximum time to wait for discovery
	DiscoveryTimeout time.Duration

	// RetryInterval is the time to wait between discovery attempts
	RetryInterval time.Duration
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Methods:          []Method{},
		MinPeers:         3,
		DiscoveryTimeout: 30 * time.Second,
		RetryInterval:    5 * time.Second,
	}
}

// Validate checks if the config is valid.
func (c *Config) Validate() error {
	if len(c.Methods) == 0 {
		return &DiscoveryError{
			Op:  "Config.Validate",
			Err: "at least one discovery method must be configured",
		}
	}

	if c.MinPeers < 1 {
		return &DiscoveryError{
			Op:  "Config.Validate",
			Err: "min_peers must be at least 1",
		}
	}

	if c.DiscoveryTimeout < time.Second {
		return &DiscoveryError{
			Op:  "Config.Validate",
			Err: "discovery_timeout must be at least 1 second",
		}
	}

	if c.RetryInterval < time.Second {
		return &DiscoveryError{
			Op:  "Config.Validate",
			Err: "retry_interval must be at least 1 second",
		}
	}

	return nil
}

// DiscoveryError represents an error that occurred during discovery.
type DiscoveryError struct {
	Op     string // Operation that failed
	Method string // Discovery method name (if applicable)
	Err    string // Error description
}

func (e *DiscoveryError) Error() string {
	if e.Method != "" {
		return "discovery." + e.Op + " [" + e.Method + "]: " + e.Err
	}
	return "discovery." + e.Op + ": " + e.Err
}

// ErrNoQuorum is returned when not enough peers are discovered to form a cluster.
var ErrNoQuorum = &DiscoveryError{
	Op:  "Bootstrap",
	Err: "insufficient peers for quorum",
}

// ErrNoMethodSucceeded is returned when all discovery methods fail.
var ErrNoMethodSucceeded = &DiscoveryError{
	Op:  "Bootstrap",
	Err: "all discovery methods failed",
}
