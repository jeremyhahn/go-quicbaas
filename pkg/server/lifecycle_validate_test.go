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

package server

import "testing"

func TestValidateRouting_OK(t *testing.T) {
	shards := []uint64{1, 2, 3}
	pins := map[string]uint64{"users": 2, "orders": 3}
	if err := validateRouting(shards, pins); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestValidateRouting_UnknownShard(t *testing.T) {
	shards := []uint64{1, 2}
	pins := map[string]uint64{"users": 3}
	if err := validateRouting(shards, pins); err == nil {
		t.Fatalf("expected error for unknown shard pin")
	}
}
