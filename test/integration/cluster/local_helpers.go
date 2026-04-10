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

//go:build integration && raft

package cluster

import (
	"context"
	"net"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/discovery"
)

// isMulticastAvailable checks if multicast is available on this system
func isMulticastAvailable() bool {
	// Try to create a multicast listener on a test address
	addr, err := net.ResolveUDPAddr("udp4", "224.0.0.1:0")
	if err != nil {
		return false
	}
	conn, err := net.ListenMulticastUDP("udp4", nil, addr)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// getAvailableMulticastPort finds an available port for multicast
func getAvailableMulticastPort() int {
	addr, _ := net.ResolveUDPAddr("udp4", ":0")
	conn, err := net.ListenUDP("udp4", addr)
	if err != nil {
		return 9999
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).Port
}

// getPeersWithRetry gets peers from a multicast discovery with retry logic
func getPeersWithRetry(md *discovery.MulticastDiscovery, minPeers int, timeout time.Duration) []discovery.Peer {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Return whatever we have on timeout
			peers, _ := md.Discover(context.Background())
			return peers
		case <-ticker.C:
			peers, err := md.Discover(context.Background())
			if err == nil && len(peers) >= minPeers {
				return peers
			}
		}
	}
}
