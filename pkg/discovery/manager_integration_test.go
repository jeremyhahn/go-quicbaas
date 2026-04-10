//go:build raft && integration

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
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// TestManager_WaitForQuorum tests the WaitForQuorum method with real peer discovery.
// This is an integration test because it blocks waiting for peers to be discovered.
func TestManager_WaitForQuorum(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "info",
		Output: "stdout",
		Format: "console",
	})
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	self := Peer{
		NodeID:  1,
		Address: "192.168.1.100:63000",
	}

	staticDiscovery, err := NewStaticDiscovery([]string{
		"1:192.168.1.100:63000",
		"2:192.168.1.101:63000",
		"3:192.168.1.102:63000",
	})
	if err != nil {
		t.Fatalf("NewStaticDiscovery() error = %v", err)
	}

	config := DefaultConfig()
	config.Methods = []Method{staticDiscovery}
	config.MinPeers = 3
	config.RetryInterval = 1 * time.Second

	mgr, err := NewManager(config, self, log)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	defer mgr.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	peers, err := mgr.WaitForQuorum(ctx)
	if err != nil {
		t.Fatalf("WaitForQuorum() error = %v", err)
	}

	if len(peers) != 3 {
		t.Errorf("WaitForQuorum() returned %d peers, want 3", len(peers))
	}
}

// TestManager_WaitForQuorumTimeout tests timeout behavior when quorum cannot be achieved.
// This is an integration test because it blocks for the full timeout duration.
func TestManager_WaitForQuorumTimeout(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "info",
		Output: "stdout",
		Format: "console",
	})
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	self := Peer{
		NodeID:  1,
		Address: "192.168.1.100:63000",
	}

	// Only 2 peers but require 3
	staticDiscovery, err := NewStaticDiscovery([]string{
		"1:192.168.1.100:63000",
		"2:192.168.1.101:63000",
	})
	if err != nil {
		t.Fatalf("NewStaticDiscovery() error = %v", err)
	}

	config := DefaultConfig()
	config.Methods = []Method{staticDiscovery}
	config.MinPeers = 3
	config.RetryInterval = 1 * time.Second

	mgr, err := NewManager(config, self, log)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	defer mgr.Close()

	// Short timeout so test doesn't hang
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	_, err = mgr.WaitForQuorum(ctx)
	if err == nil {
		t.Error("WaitForQuorum() should timeout with insufficient peers")
	}
}
