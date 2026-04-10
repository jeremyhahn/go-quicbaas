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

package service

import (
	"errors"
	"sync"
	"testing"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

func TestShardRouter_RegisterAndGet(t *testing.T) {
	router := NewVectorShardRouter(RoutingSingleShard)

	shard := DistributedShardInfo{
		ShardID: 1,
		NodeID:  "node-1",
		Address: "localhost:9001",
	}
	router.RegisterShard("test-ns", shard)

	shards, err := router.GetShards("test-ns")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(shards) != 1 {
		t.Fatalf("expected 1 shard, got %d", len(shards))
	}
	if shards[0].ShardID != 1 || shards[0].NodeID != "node-1" || shards[0].Address != "localhost:9001" {
		t.Errorf("shard mismatch: %+v", shards[0])
	}
}

func TestShardRouter_RegisterAndGet_DuplicateRegistration(t *testing.T) {
	router := NewVectorShardRouter(RoutingSingleShard)

	shard := DistributedShardInfo{ShardID: 1, NodeID: "node-1", Address: "localhost:9001"}
	router.RegisterShard("test-ns", shard)
	router.RegisterShard("test-ns", shard) // duplicate

	shards, err := router.GetShards("test-ns")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(shards) != 1 {
		t.Fatalf("expected 1 shard after duplicate registration, got %d", len(shards))
	}
}

func TestShardRouter_MultipleShards(t *testing.T) {
	router := NewVectorShardRouter(RoutingMultiShard)

	router.RegisterShard("multi-ns", DistributedShardInfo{ShardID: 1, NodeID: "node-1", Address: "localhost:9001"})
	router.RegisterShard("multi-ns", DistributedShardInfo{ShardID: 2, NodeID: "node-2", Address: "localhost:9002"})
	router.RegisterShard("multi-ns", DistributedShardInfo{ShardID: 3, NodeID: "node-3", Address: "localhost:9003"})

	shards, err := router.GetShards("multi-ns")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(shards) != 3 {
		t.Fatalf("expected 3 shards, got %d", len(shards))
	}

	// Verify all shard IDs are present.
	ids := make(map[uint64]bool)
	for _, s := range shards {
		ids[s.ShardID] = true
	}
	for _, id := range []uint64{1, 2, 3} {
		if !ids[id] {
			t.Errorf("shard %d not found in results", id)
		}
	}
}

func TestShardRouter_MultipleShards_ReturnsCopy(t *testing.T) {
	router := NewVectorShardRouter(RoutingMultiShard)

	router.RegisterShard("ns", DistributedShardInfo{ShardID: 1, NodeID: "node-1", Address: "localhost:9001"})

	shards, err := router.GetShards("ns")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Mutate the returned slice.
	shards[0].NodeID = "mutated"

	// Original should be unchanged.
	original, err := router.GetShards("ns")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if original[0].NodeID == "mutated" {
		t.Error("GetShards returned a reference to internal state, not a copy")
	}
}

func TestShardRouter_RemoveShard(t *testing.T) {
	router := NewVectorShardRouter(RoutingMultiShard)

	router.RegisterShard("rm-ns", DistributedShardInfo{ShardID: 1, NodeID: "node-1", Address: "localhost:9001"})
	router.RegisterShard("rm-ns", DistributedShardInfo{ShardID: 2, NodeID: "node-2", Address: "localhost:9002"})

	router.RemoveShard("rm-ns", 1)

	shards, err := router.GetShards("rm-ns")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(shards) != 1 {
		t.Fatalf("expected 1 shard after removal, got %d", len(shards))
	}
	if shards[0].ShardID != 2 {
		t.Errorf("expected shard 2, got shard %d", shards[0].ShardID)
	}
}

func TestShardRouter_RemoveShard_LastShard(t *testing.T) {
	router := NewVectorShardRouter(RoutingSingleShard)

	router.RegisterShard("single-ns", DistributedShardInfo{ShardID: 1, NodeID: "node-1", Address: "localhost:9001"})
	router.RemoveShard("single-ns", 1)

	_, err := router.GetShards("single-ns")
	if err == nil {
		t.Fatal("expected error when getting shards after removing the last one")
	}
	var de *dberrors.DragonError
	if !errors.As(err, &de) || de.Code != dberrors.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestShardRouter_RemoveShard_NonExistentNamespace(t *testing.T) {
	router := NewVectorShardRouter(RoutingSingleShard)
	// Should not panic.
	router.RemoveShard("nonexistent", 1)
}

func TestShardRouter_RemoveShard_NonExistentShard(t *testing.T) {
	router := NewVectorShardRouter(RoutingSingleShard)
	router.RegisterShard("ns", DistributedShardInfo{ShardID: 1, NodeID: "node-1", Address: "localhost:9001"})
	// Should not panic or modify anything.
	router.RemoveShard("ns", 999)

	shards, err := router.GetShards("ns")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(shards) != 1 {
		t.Fatalf("expected 1 shard, got %d", len(shards))
	}
}

func TestShardRouter_NotFound(t *testing.T) {
	router := NewVectorShardRouter(RoutingSingleShard)

	_, err := router.GetShards("nonexistent")
	if err == nil {
		t.Fatal("expected error for unregistered namespace")
	}

	var de *dberrors.DragonError
	if !errors.As(err, &de) {
		t.Fatalf("expected DragonError, got %T", err)
	}
	if de.Code != dberrors.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", de.Code)
	}

	var nre *NamespaceNotRoutedError
	if !errors.As(err, &nre) {
		t.Fatalf("expected NamespaceNotRoutedError, got %T", de.Err)
	}
	if nre.Namespace != "nonexistent" {
		t.Errorf("expected namespace 'nonexistent', got %q", nre.Namespace)
	}
}

func TestShardRouter_NotFound_ErrorMessage(t *testing.T) {
	e := &NamespaceNotRoutedError{Namespace: "test"}
	expected := "no shards registered for namespace: test"
	if e.Error() != expected {
		t.Errorf("expected %q, got %q", expected, e.Error())
	}
}

func TestShardRouter_IsDistributed(t *testing.T) {
	router := NewVectorShardRouter(RoutingMultiShard)

	// Unregistered namespace.
	if router.IsDistributed("unknown") {
		t.Error("unregistered namespace should not be distributed")
	}

	// Single shard.
	router.RegisterShard("single", DistributedShardInfo{ShardID: 1, NodeID: "node-1", Address: "localhost:9001"})
	if router.IsDistributed("single") {
		t.Error("single-shard namespace should not be distributed")
	}

	// Multiple shards.
	router.RegisterShard("multi", DistributedShardInfo{ShardID: 1, NodeID: "node-1", Address: "localhost:9001"})
	router.RegisterShard("multi", DistributedShardInfo{ShardID: 2, NodeID: "node-2", Address: "localhost:9002"})
	if !router.IsDistributed("multi") {
		t.Error("multi-shard namespace should be distributed")
	}
}

func TestShardRouter_IsDistributed_AfterRemoval(t *testing.T) {
	router := NewVectorShardRouter(RoutingMultiShard)

	router.RegisterShard("ns", DistributedShardInfo{ShardID: 1, NodeID: "node-1", Address: "localhost:9001"})
	router.RegisterShard("ns", DistributedShardInfo{ShardID: 2, NodeID: "node-2", Address: "localhost:9002"})

	if !router.IsDistributed("ns") {
		t.Error("expected distributed before removal")
	}

	router.RemoveShard("ns", 2)

	if router.IsDistributed("ns") {
		t.Error("expected not distributed after removing second shard")
	}
}

func TestShardRouter_Mode(t *testing.T) {
	single := NewVectorShardRouter(RoutingSingleShard)
	if single.Mode() != RoutingSingleShard {
		t.Errorf("expected RoutingSingleShard, got %d", single.Mode())
	}

	multi := NewVectorShardRouter(RoutingMultiShard)
	if multi.Mode() != RoutingMultiShard {
		t.Errorf("expected RoutingMultiShard, got %d", multi.Mode())
	}
}

func TestShardRouter_ConcurrentAccess(t *testing.T) {
	router := NewVectorShardRouter(RoutingMultiShard)
	const goroutines = 10
	const iterations = 50

	var wg sync.WaitGroup
	wg.Add(goroutines * 3)

	// Writers: register shards.
	for g := 0; g < goroutines; g++ {
		go func(gID int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				router.RegisterShard("concurrent-ns", DistributedShardInfo{
					ShardID: uint64(gID*iterations + i),
					NodeID:  "node",
					Address: "localhost:9001",
				})
			}
		}(g)
	}

	// Readers: get shards.
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				router.GetShards("concurrent-ns")
			}
		}()
	}

	// Removers: remove shards.
	for g := 0; g < goroutines; g++ {
		go func(gID int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				router.RemoveShard("concurrent-ns", uint64(gID*iterations+i))
			}
		}(g)
	}

	wg.Wait()
}
