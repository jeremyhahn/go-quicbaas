// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//go:build integration

package helpers

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// testEntities builds n entities with keys k0..k<n-1>.
func testEntities(n int) []TestEntity {
	out := make([]TestEntity, n)
	for i := range out {
		out[i] = TestEntity{EntityType: "t", Key: "k" + strconv.Itoa(i)}
	}
	return out
}

// readCounts counts reads per (node, key).
type readCounts struct {
	mu sync.Mutex
	n  map[string]int
}

// add records one read and returns how many reads of the pair there were,
// this one included.
func (c *readCounts) add(node int, key string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.n == nil {
		c.n = map[string]int{}
	}
	k := strconv.Itoa(node) + "/" + key
	c.n[k]++
	return c.n[k]
}

func (c *readCounts) get(node int, key string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n[strconv.Itoa(node)+"/"+key]
}

func (c *readCounts) total() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := 0
	for _, v := range c.n {
		t += v
	}
	return t
}

// errTestRead is a read failure the fake readers return.
type errTestRead struct{ key string }

func (e *errTestRead) Error() string { return "test read of " + e.key + " failed" }

func TestReplicationWait_FoundPairsAreNotReadAgain(t *testing.T) {
	var counts readCounts
	w := NewReplicationWait(3, testEntities(10), func(_ context.Context, node int, e TestEntity) (bool, error) {
		n := counts.add(node, e.Key)
		if node == 1 && e.Key == "k3" && n == 1 {
			return false, nil
		}
		return true, nil
	}, time.Second)

	done, err := w.Check(context.Background())
	if done || err != nil {
		t.Fatalf("first Check = (%v, %v), want (false, nil)", done, err)
	}
	if got := w.Pending(); got != 1 {
		t.Fatalf("Pending after first Check = %d, want 1", got)
	}
	done, err = w.Check(context.Background())
	if !done || err != nil {
		t.Fatalf("second Check = (%v, %v), want (true, nil)", done, err)
	}
	if got := counts.total(); got != 31 {
		t.Fatalf("total reads = %d, want 31", got)
	}
	if got := counts.get(1, "k3"); got != 2 {
		t.Fatalf("reads of k3 on node 1 = %d, want 2", got)
	}
}

func TestReplicationWait_SweepCutByContextKeepsProgress(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var reads atomic.Int64
	w := NewReplicationWait(2, testEntities(100), func(rctx context.Context, _ int, _ TestEntity) (bool, error) {
		if reads.Add(1) == 50 {
			cancel()
		}
		if err := rctx.Err(); err != nil {
			return false, err
		}
		return true, nil
	}, time.Second)

	if done, _ := w.Check(ctx); done {
		t.Fatal("Check on a context cancelled mid-sweep reported done")
	}
	done, err := w.Check(context.Background())
	if !done || err != nil {
		t.Fatalf("second Check = (%v, %v), want (true, nil)", done, err)
	}
	// Every pair is read once, plus the reads in flight on each node when
	// ctx ended (they fail and are read again). Re-reading the cut sweep's
	// finds would cost at least 50+200 reads.
	if limit := int64(200 + 2*replicationReadsPerNode); reads.Load() > limit {
		t.Fatalf("reads over both Checks = %d, want at most %d: the cut sweep's finds were read again", reads.Load(), limit)
	}
}

func TestReplicationWait_EachReadHasItsOwnDeadline(t *testing.T) {
	var counts readCounts
	w := NewReplicationWait(1, testEntities(3), func(ctx context.Context, node int, e TestEntity) (bool, error) {
		counts.add(node, e.Key)
		if e.Key == "k1" {
			<-ctx.Done()
			return false, ctx.Err()
		}
		return true, nil
	}, 20*time.Millisecond)

	start := time.Now()
	done, err := w.Check(context.Background())
	elapsed := time.Since(start)
	if done {
		t.Fatal("Check reported done with k1 unread")
	}
	var pending *ReplicationPendingError
	if !errors.As(err, &pending) {
		t.Fatalf("Check error = %v, want a *ReplicationPendingError", err)
	}
	if pending.Pending != 1 {
		t.Fatalf("Pending = %d, want 1", pending.Pending)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Check error = %v, want it to wrap context.DeadlineExceeded", err)
	}
	if counts.get(0, "k2") != 1 {
		t.Fatal("k2 was not read after the slow k1 read")
	}
	if elapsed >= time.Second {
		t.Fatalf("Check took %s, want under 1s", elapsed)
	}
}

func TestReplicationWait_ReadErrorKeepsPairPending(t *testing.T) {
	var failed atomic.Bool
	w := NewReplicationWait(2, testEntities(3), func(_ context.Context, node int, e TestEntity) (bool, error) {
		if node == 1 && e.Key == "k2" && failed.CompareAndSwap(false, true) {
			return false, &errTestRead{key: e.Key}
		}
		return true, nil
	}, time.Second)

	done, err := w.Check(context.Background())
	if done {
		t.Fatal("Check reported done with a failed read")
	}
	var pending *ReplicationPendingError
	if !errors.As(err, &pending) || pending.Pending != 1 {
		t.Fatalf("Check error = %v, want a *ReplicationPendingError with Pending 1", err)
	}
	var readErr *errTestRead
	if !errors.As(err, &readErr) || readErr.key != "k2" {
		t.Fatalf("Check error = %v, want it to wrap the read error for k2", err)
	}
	done, err = w.Check(context.Background())
	if !done || err != nil {
		t.Fatalf("second Check = (%v, %v), want (true, nil)", done, err)
	}
}

func TestReplicationWait_NodesAreReadConcurrently(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	node1Read := make(chan struct{})
	w := NewReplicationWait(2, testEntities(1), func(rctx context.Context, node int, _ TestEntity) (bool, error) {
		if node == 1 {
			close(node1Read)
			return true, nil
		}
		select {
		case <-node1Read:
			return true, nil
		case <-rctx.Done():
			return false, rctx.Err()
		}
	}, 5*time.Second)

	done, err := w.Check(ctx)
	if !done || err != nil {
		t.Fatalf("Check = (%v, %v), want (true, nil): node 0's read waited on node 1's", done, err)
	}
}

// TestReplicationWait_ReadsWithinANodeAreConcurrent pins that one node's
// pending entities are read concurrently, so a sweep is not bound by one
// round trip per entity.
func TestReplicationWait_ReadsWithinANodeAreConcurrent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var inFlight, peak atomic.Int64
	gate := make(chan struct{})
	w := NewReplicationWait(1, testEntities(2*replicationReadsPerNode), func(rctx context.Context, _ int, _ TestEntity) (bool, error) {
		n := inFlight.Add(1)
		defer inFlight.Add(-1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		select {
		case <-gate:
			return true, nil
		case <-rctx.Done():
			return false, rctx.Err()
		}
	}, 5*time.Second)

	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(gate) }) }
	defer release()
	go func() {
		deadline := time.After(2 * time.Second)
		tick := time.NewTicker(time.Millisecond)
		defer tick.Stop()
		for peak.Load() < replicationReadsPerNode {
			select {
			case <-deadline:
				release()
				return
			case <-tick.C:
			}
		}
		release()
	}()

	done, err := w.Check(ctx)
	got := peak.Load()
	if got <= 1 || got > replicationReadsPerNode {
		t.Fatalf("peak reads in flight on one node = %d, want 2..%d", got, replicationReadsPerNode)
	}
	if !done || err != nil {
		t.Fatalf("Check = (%v, %v), want (true, nil)", done, err)
	}
}

func TestReplicationWait_DoesNotModifyCallerEntities(t *testing.T) {
	entities := testEntities(5)
	want := slices.Clone(entities)
	w := NewReplicationWait(2, entities, func(_ context.Context, _ int, e TestEntity) (bool, error) {
		return e.Key != "k4", nil
	}, time.Second)

	if done, err := w.Check(context.Background()); done || err != nil {
		t.Fatalf("Check = (%v, %v), want (false, nil)", done, err)
	}
	if !reflect.DeepEqual(entities, want) {
		t.Fatalf("caller entities changed: got %v, want %v", entities, want)
	}
}

func TestReplicationWait_WithPoll(t *testing.T) {
	var counts readCounts
	w := NewReplicationWait(3, testEntities(4), func(_ context.Context, node int, e TestEntity) (bool, error) {
		return counts.add(node, e.Key) > 1, nil
	}, time.Second)

	if err := Poll(context.Background(), time.Millisecond, time.Second, w.Check); err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if got := counts.total(); got != 24 {
		t.Fatalf("total reads = %d, want 24 (two sweeps)", got)
	}
}
