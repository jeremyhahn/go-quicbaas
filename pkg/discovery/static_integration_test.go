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
)

// TestStaticDiscovery_ConcurrentAccess tests concurrent access to static discovery.
// This is an integration test because it uses goroutines with time.Sleep for synchronization.
func TestStaticDiscovery_ConcurrentAccess(t *testing.T) {
	addresses := []string{
		"1:192.168.1.100:63000",
		"2:192.168.1.101:63000",
	}

	sd, err := NewStaticDiscovery(addresses)
	if err != nil {
		t.Fatalf("NewStaticDiscovery() error = %v", err)
	}

	ctx := context.Background()

	// Run concurrent Discover calls
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				_, err := sd.Discover(ctx)
				if err != nil {
					t.Errorf("concurrent Discover() error = %v", err)
				}
				time.Sleep(1 * time.Millisecond)
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}
}
