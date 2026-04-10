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

import "testing"

func TestDefaultHNSWConfig(t *testing.T) {
	config := DefaultHNSWConfig()

	if config == nil {
		t.Fatal("DefaultHNSWConfig() returned nil")
	}
	if config.M != 16 {
		t.Errorf("expected M=16, got %d", config.M)
	}
	if config.MMax != 32 {
		t.Errorf("expected MMax=32, got %d", config.MMax)
	}
	if config.EfConstruction != 200 {
		t.Errorf("expected EfConstruction=200, got %d", config.EfConstruction)
	}
	if config.EfSearch != 50 {
		t.Errorf("expected EfSearch=50, got %d", config.EfSearch)
	}
	if config.DistanceMetric != "l2" {
		t.Errorf("expected DistanceMetric=l2, got %s", config.DistanceMetric)
	}
}

func TestDefaultHNSWConfig_ReturnsDistinctInstances(t *testing.T) {
	a := DefaultHNSWConfig()
	b := DefaultHNSWConfig()

	if a == b {
		t.Error("DefaultHNSWConfig() should return distinct instances on each call")
	}

	// Mutating one should not affect the other.
	a.M = 999
	if b.M == 999 {
		t.Error("mutating one config should not affect the other")
	}
}
