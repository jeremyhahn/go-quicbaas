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

package discovery

import (
	"context"
	"net"
	"strconv"
	"strings"
	"time"
)

// StaticDiscovery implements discovery using a static list of peers
// This is the simplest method - peers are configured at startup and never change.
type StaticDiscovery struct {
	peers []Peer
}

// StaticPeerFormatError is returned when a static peer address has an invalid format.
type StaticPeerFormatError struct {
	Address string
	Detail  string
}

// Error implements the error interface.
func (e *StaticPeerFormatError) Error() string {
	if e.Address != "" {
		return "invalid static peer format '" + e.Address + "': " + e.Detail
	}
	return "invalid static peer: " + e.Detail
}

// StaticPeerNodeIDError is returned when a static peer has an invalid node ID.
type StaticPeerNodeIDError struct {
	NodeID string
	Cause  error
}

// Error implements the error interface.
func (e *StaticPeerNodeIDError) Error() string {
	if e.Cause != nil {
		return "invalid node ID '" + e.NodeID + "': " + e.Cause.Error()
	}
	return "invalid node ID '" + e.NodeID + "'"
}

// Unwrap returns the underlying cause.
func (e *StaticPeerNodeIDError) Unwrap() error {
	return e.Cause
}

// StaticPeerPortError is returned when a static peer has an invalid port.
type StaticPeerPortError struct {
	Port  string
	Cause error
}

// Error implements the error interface.
func (e *StaticPeerPortError) Error() string {
	if e.Cause != nil {
		return "invalid port '" + e.Port + "': " + e.Cause.Error()
	}
	return "invalid port '" + e.Port + "'"
}

// Unwrap returns the underlying cause.
func (e *StaticPeerPortError) Unwrap() error {
	return e.Cause
}

// StaticPeerPortRangeError is returned when a static peer port is out of range.
type StaticPeerPortRangeError struct {
	Port int
}

// Error implements the error interface.
func (e *StaticPeerPortRangeError) Error() string {
	return "port " + strconv.Itoa(e.Port) + " out of range (1-65535)"
}

// NewStaticDiscovery creates a new static discovery method from a list of peer addresses
// Addresses should be in format "nodeID:host:port" (e.g., "1:192.168.1.100:63000").
func NewStaticDiscovery(addresses []string) (*StaticDiscovery, error) {
	if len(addresses) == 0 {
		return nil, &DiscoveryError{
			Op:     "NewStaticDiscovery",
			Method: "static",
			Err:    "peer list cannot be empty",
		}
	}

	peers := make([]Peer, 0, len(addresses))

	for i, addr := range addresses {
		peer, err := parseStaticPeer(addr)
		if err != nil {
			return nil, &DiscoveryError{
				Op:     "NewStaticDiscovery",
				Method: "static",
				Err:    "invalid peer address at index " + strconv.Itoa(i) + ": " + err.Error(),
			}
		}
		peers = append(peers, peer)
	}

	return &StaticDiscovery{
		peers: peers,
	}, nil
}

// NewStaticDiscoveryFromPeers creates a new static discovery method from a list of peers.
func NewStaticDiscoveryFromPeers(peers []Peer) (*StaticDiscovery, error) {
	if len(peers) == 0 {
		return nil, &DiscoveryError{
			Op:     "NewStaticDiscoveryFromPeers",
			Method: "static",
			Err:    "peer list cannot be empty",
		}
	}

	// Validate all peers
	for i, peer := range peers {
		if peer.NodeID == 0 {
			return nil, &DiscoveryError{
				Op:     "NewStaticDiscoveryFromPeers",
				Method: "static",
				Err:    "peer at index " + strconv.Itoa(i) + " has invalid NodeID",
			}
		}

		if peer.Address == "" {
			return nil, &DiscoveryError{
				Op:     "NewStaticDiscoveryFromPeers",
				Method: "static",
				Err:    "peer at index " + strconv.Itoa(i) + " has empty address",
			}
		}

		// Ensure RaftAddr is set
		if peer.RaftAddr == "" {
			peer.RaftAddr = peer.Address
			peers[i] = peer
		}
	}

	return &StaticDiscovery{
		peers: peers,
	}, nil
}

// Name returns the name of this discovery method.
func (s *StaticDiscovery) Name() string {
	return "static"
}

// Discover returns the static list of peers.
func (s *StaticDiscovery) Discover(ctx context.Context) ([]Peer, error) {
	// Add LastSeen timestamp
	now := time.Now()
	peers := make([]Peer, len(s.peers))
	for i, peer := range s.peers {
		peer.LastSeen = now
		peer.TTL = 0 // Static peers don't expire
		peers[i] = peer
	}

	return peers, nil
}

// Register is a no-op for static discovery
// Static peers are configured, not registered dynamically.
func (s *StaticDiscovery) Register(ctx context.Context, self Peer) error {
	// No-op - static discovery doesn't require registration
	return nil
}

// Deregister is a no-op for static discovery.
func (s *StaticDiscovery) Deregister(ctx context.Context) error {
	// No-op - static discovery doesn't require deregistration
	return nil
}

// Close releases resources (no-op for static discovery).
func (s *StaticDiscovery) Close() error {
	// No resources to release
	return nil
}

// parseStaticPeer parses a peer address in format "nodeID:host:port"
// Examples:
//   - "1:192.168.1.100:63000"
//   - "2:node2.example.com:63000"
//   - "3:10.0.0.50:8001"
func parseStaticPeer(addr string) (Peer, error) {
	// Split into parts
	parts := strings.SplitN(addr, ":", 3)
	if len(parts) != 3 {
		return Peer{}, &StaticPeerFormatError{Address: addr, Detail: "expected 'nodeID:host:port'"}
	}

	// Parse node ID.
	nodeID, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil {
		return Peer{}, &StaticPeerNodeIDError{NodeID: parts[0], Cause: err}
	}

	if nodeID == 0 {
		return Peer{}, &StaticPeerFormatError{Detail: "node ID must be greater than 0"}
	}

	// Validate host:port format
	host := parts[1]
	port := parts[2]

	if host == "" {
		return Peer{}, &StaticPeerFormatError{Detail: "host cannot be empty"}
	}

	if port == "" {
		return Peer{}, &StaticPeerFormatError{Detail: "port cannot be empty"}
	}

	// Validate port is a number.
	portNum, err := strconv.Atoi(port)
	if err != nil {
		return Peer{}, &StaticPeerPortError{Port: port, Cause: err}
	}

	if portNum < 1 || portNum > 65535 {
		return Peer{}, &StaticPeerPortRangeError{Port: portNum}
	}

	// Construct full address
	address := net.JoinHostPort(host, port)

	return Peer{
		NodeID:   nodeID,
		Address:  address,
		RaftAddr: address,
		Metadata: make(map[string]string),
	}, nil
}

// AddPeer adds a peer to the static list
// This can be used to dynamically add peers after initialization.
func (s *StaticDiscovery) AddPeer(peer Peer) error {
	if peer.NodeID == 0 {
		return &DiscoveryError{
			Op:     "AddPeer",
			Method: "static",
			Err:    "peer NodeID cannot be 0",
		}
	}

	if peer.Address == "" {
		return &DiscoveryError{
			Op:     "AddPeer",
			Method: "static",
			Err:    "peer address cannot be empty",
		}
	}

	// Ensure RaftAddr is set
	if peer.RaftAddr == "" {
		peer.RaftAddr = peer.Address
	}

	// Check for duplicates
	for _, existing := range s.peers {
		if existing.NodeID == peer.NodeID {
			return &DiscoveryError{
				Op:     "AddPeer",
				Method: "static",
				Err:    "peer with NodeID " + strconv.FormatUint(peer.NodeID, 10) + " already exists",
			}
		}
	}

	s.peers = append(s.peers, peer)
	return nil
}

// RemovePeer removes a peer from the static list.
func (s *StaticDiscovery) RemovePeer(nodeID uint64) error {
	for i, peer := range s.peers {
		if peer.NodeID == nodeID {
			// Remove peer by slicing
			s.peers = append(s.peers[:i], s.peers[i+1:]...)
			return nil
		}
	}

	return &DiscoveryError{
		Op:     "RemovePeer",
		Method: "static",
		Err:    "peer with NodeID " + strconv.FormatUint(nodeID, 10) + " not found",
	}
}

// GetPeers returns the current list of static peers.
func (s *StaticDiscovery) GetPeers() []Peer {
	peers := make([]Peer, len(s.peers))
	copy(peers, s.peers)
	return peers
}
