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
	"sync"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// Manager orchestrates multiple discovery methods and manages peer discovery.
type Manager struct {
	config *Config
	logger *logger.Logger
	self   Peer

	// peers stores the currently discovered peers
	peers   []Peer
	peersMu sync.RWMutex

	// closeChan signals shutdown
	closeChan chan struct{}
	closeOnce sync.Once
	closed    bool
	mu        sync.RWMutex
}

// NewManager creates a new discovery manager.
func NewManager(config *Config, self Peer, log *logger.Logger) (*Manager, error) {
	if config == nil {
		return nil, &DiscoveryError{
			Op:  "NewManager",
			Err: "config cannot be nil",
		}
	}

	if err := config.Validate(); err != nil {
		return nil, err
	}

	if log == nil {
		return nil, &DiscoveryError{
			Op:  "NewManager",
			Err: "logger cannot be nil",
		}
	}

	// Validate self peer
	if self.NodeID == 0 {
		return nil, &DiscoveryError{
			Op:  "NewManager",
			Err: "self peer NodeID cannot be 0",
		}
	}

	if self.Address == "" {
		return nil, &DiscoveryError{
			Op:  "NewManager",
			Err: "self peer Address cannot be empty",
		}
	}

	// Ensure RaftAddr is set (alias for Address)
	if self.RaftAddr == "" {
		self.RaftAddr = self.Address
	}

	return &Manager{
		config:    config,
		logger:    log.WithField("component", "discovery"),
		self:      self,
		peers:     make([]Peer, 0),
		closeChan: make(chan struct{}),
	}, nil
}

// Bootstrap attempts to discover peers and form a cluster
// This is the main entry point for cluster initialization.
func (m *Manager) Bootstrap(ctx context.Context) error {
	m.mu.RLock()
	if m.closed {
		m.mu.RUnlock()
		return &DiscoveryError{
			Op:  "Bootstrap",
			Err: "manager is closed",
		}
	}
	m.mu.RUnlock()

	m.logger.Info("Starting cluster discovery bootstrap")

	// Try each discovery method in order
	var lastErr error
	for _, method := range m.config.Methods {
		m.logger.Infof("Attempting discovery via %s", method.Name())

		// Create timeout context for this method
		methodCtx, cancel := context.WithTimeout(ctx, m.config.DiscoveryTimeout)
		defer cancel()

		// Try to discover peers
		peers, err := method.Discover(methodCtx)
		if err != nil {
			m.logger.Warnf("Discovery via %s failed: %v", method.Name(), err)
			lastErr = &DiscoveryError{
				Op:     "Bootstrap",
				Method: method.Name(),
				Err:    err.Error(),
			}
			continue
		}

		// Check if we have enough peers for quorum
		if len(peers) < m.config.MinPeers {
			m.logger.Warnf("Discovery via %s found %d peers, need %d for quorum",
				method.Name(), len(peers), m.config.MinPeers)
			lastErr = ErrNoQuorum
			continue
		}

		// Success! Store discovered peers
		m.peersMu.Lock()
		m.peers = peers
		m.peersMu.Unlock()

		m.logger.Infof("Discovery via %s succeeded: found %d peers", method.Name(), len(peers))

		// Register ourselves with this method
		if err := method.Register(methodCtx, m.self); err != nil {
			m.logger.Warnf("Failed to register with %s: %v", method.Name(), err)
			// Don't fail bootstrap if registration fails - we can still join
		} else {
			m.logger.Infof("Successfully registered with %s", method.Name())
		}

		return nil
	}

	// All methods failed
	if lastErr == nil {
		lastErr = ErrNoMethodSucceeded
	}

	m.logger.Errorf("All discovery methods failed: %v", lastErr)
	return lastErr
}

// Discover performs a one-time discovery without modifying internal state
// Useful for health checks or refreshing peer list.
func (m *Manager) Discover(ctx context.Context) ([]Peer, error) {
	m.mu.RLock()
	if m.closed {
		m.mu.RUnlock()
		return nil, &DiscoveryError{
			Op:  "Discover",
			Err: "manager is closed",
		}
	}
	m.mu.RUnlock()

	// Try each method in order
	for _, method := range m.config.Methods {
		methodCtx, cancel := context.WithTimeout(ctx, m.config.DiscoveryTimeout)
		defer cancel()

		peers, err := method.Discover(methodCtx)
		if err != nil {
			m.logger.Debugf("Discovery via %s failed: %v", method.Name(), err)
			continue
		}

		if len(peers) > 0 {
			return peers, nil
		}
	}

	return nil, ErrNoMethodSucceeded
}

// GetPeers returns the currently discovered peers.
func (m *Manager) GetPeers() []Peer {
	m.peersMu.RLock()
	defer m.peersMu.RUnlock()

	// Return a copy to prevent external modification
	peers := make([]Peer, len(m.peers))
	copy(peers, m.peers)
	return peers
}

// UpdatePeers updates the peer list (typically called when membership changes).
func (m *Manager) UpdatePeers(peers []Peer) {
	m.peersMu.Lock()
	defer m.peersMu.Unlock()

	m.peers = peers
	m.logger.Infof("Peer list updated: now have %d peers", len(peers))
}

// GetSelf returns this node's peer information.
func (m *Manager) GetSelf() Peer {
	return m.self
}

// Close gracefully shuts down the discovery manager.
func (m *Manager) Close() error {
	var closeErr error

	m.closeOnce.Do(func() {
		m.mu.Lock()
		m.closed = true
		m.mu.Unlock()

		close(m.closeChan)

		// Deregister from all methods
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		for _, method := range m.config.Methods {
			if err := method.Deregister(ctx); err != nil {
				m.logger.Warnf("Failed to deregister from %s: %v", method.Name(), err)
			}

			if err := method.Close(); err != nil {
				m.logger.Warnf("Failed to close %s: %v", method.Name(), err)
			}
		}

		m.logger.Info("Discovery manager closed")
	})

	return closeErr
}

// WaitForQuorum waits until enough peers are discovered to form a quorum
// Returns list of peers or error on timeout.
func (m *Manager) WaitForQuorum(ctx context.Context) ([]Peer, error) {
	m.mu.RLock()
	if m.closed {
		m.mu.RUnlock()
		return nil, &DiscoveryError{
			Op:  "WaitForQuorum",
			Err: "manager is closed",
		}
	}
	m.mu.RUnlock()

	ticker := time.NewTicker(m.config.RetryInterval)
	defer ticker.Stop()

	for {
		// Try discovery
		peers, err := m.Discover(ctx)
		if err == nil && len(peers) >= m.config.MinPeers {
			// Store the discovered peers
			m.peersMu.Lock()
			m.peers = peers
			m.peersMu.Unlock()

			m.logger.Infof("Quorum achieved: %d peers discovered", len(peers))
			return peers, nil
		}

		// Log progress
		if err != nil {
			m.logger.Debugf("Discovery attempt failed: %v", err)
		} else {
			m.logger.Infof("Found %d peers, waiting for %d to achieve quorum",
				len(peers), m.config.MinPeers)
		}

		// Wait for retry interval or context cancellation
		select {
		case <-ticker.C:
			continue
		case <-ctx.Done():
			return nil, &DiscoveryError{
				Op:  "WaitForQuorum",
				Err: "timeout waiting for quorum: " + ctx.Err().Error(),
			}
		case <-m.closeChan:
			return nil, &DiscoveryError{
				Op:  "WaitForQuorum",
				Err: "manager closed while waiting for quorum",
			}
		}
	}
}

// Stats returns statistics about the discovery manager.
type Stats struct {
	PeerCount        int
	DiscoveryMethods int
	Self             Peer
}

// GetStats returns current statistics.
func (m *Manager) GetStats() Stats {
	m.peersMu.RLock()
	peerCount := len(m.peers)
	m.peersMu.RUnlock()

	return Stats{
		PeerCount:        peerCount,
		DiscoveryMethods: len(m.config.Methods),
		Self:             m.self,
	}
}
