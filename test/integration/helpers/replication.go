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
	"slices"
	"strconv"
	"sync"
	"time"
)

// EntityReader reads entity from the node with index node and reports
// whether the node has it.
type EntityReader func(ctx context.Context, node int, entity TestEntity) (bool, error)

// replicationReadsPerNode is the most reads in flight per node during a
// sweep, so a sweep of N entities takes about N/16 round trips instead of N.
const replicationReadsPerNode = 16

// ReplicationWait tracks, for each node, the entities not yet seen there.
// Check is a PollCondition: an entity found on a node is never read from
// that node again, so a sweep cut short by its context still counts.
type ReplicationWait struct {
	read           EntityReader
	requestTimeout time.Duration
	pending        [][]TestEntity // per node; each slice is touched only by its node's goroutine during Check
}

// NewReplicationWait waits for every entity on each of nodes nodes. Each
// read is bounded by requestTimeout.
func NewReplicationWait(nodes int, entities []TestEntity, read EntityReader, requestTimeout time.Duration) *ReplicationWait {
	pending := make([][]TestEntity, nodes)
	for node := range pending {
		pending[node] = slices.Clone(entities)
	}
	return &ReplicationWait{read: read, requestTimeout: requestTimeout, pending: pending}
}

// Pending returns how many (node, entity) pairs have not been seen yet.
func (w *ReplicationWait) Pending() int {
	n := 0
	for _, p := range w.pending {
		n += len(p)
	}
	return n
}

// Check reads every pending pair once, one goroutine per node, and drops
// each pair found. Reads within a node are concurrent too, bounded by
// replicationReadsPerNode. It returns true once nothing is pending. A read
// error leaves the pair pending; Check then returns a
// *ReplicationPendingError carrying a read error. When ctx ends, each node
// stops its sweep and keeps what it has found so far.
//
// Check is not safe to call concurrently with itself; Poll never does.
func (w *ReplicationWait) Check(ctx context.Context) (bool, error) {
	errs := make([]error, len(w.pending))
	var wg sync.WaitGroup
	for node := range w.pending {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[node] = w.sweep(ctx, node)
		}()
	}
	wg.Wait()

	pending := w.Pending()
	if pending == 0 {
		return true, nil
	}
	var lastErr error
	for _, err := range errs {
		if err != nil {
			lastErr = err
		}
	}
	if lastErr != nil {
		return false, &ReplicationPendingError{Pending: pending, LastErr: lastErr}
	}
	return false, nil
}

// sweep reads node's pending entities once, at most replicationReadsPerNode
// at a time, and keeps those not found or whose read errored, in their
// original order. It returns a read error from this sweep (any one, when
// several fail). Once ctx ends no new read starts, and every entity not yet
// read stays pending. Only sweep's goroutine writes w.pending[node], after
// all its reads have finished.
func (w *ReplicationWait) sweep(ctx context.Context, node int) error {
	todo := w.pending[node]
	found := make([]bool, len(todo))
	var (
		mu      sync.Mutex
		lastErr error
		wg      sync.WaitGroup
	)
	sem := make(chan struct{}, replicationReadsPerNode)
	for i, e := range todo {
		// Wait for a free read slot, unless ctx ends first; once it has
		// ended no new read starts.
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
		}
		if ctx.Err() != nil {
			// A slot taken just as ctx ended stays in the buffer unused;
			// the entities not read stay pending.
			break
		}
		wg.Add(1)
		go func(i int, e TestEntity) {
			defer wg.Done()
			defer func() { <-sem }()
			if ctx.Err() != nil {
				// ctx ended before this read began: it stays pending.
				return
			}
			rctx, cancel := context.WithTimeout(ctx, w.requestTimeout)
			ok, err := w.read(rctx, node, e)
			cancel()
			if err != nil {
				mu.Lock()
				lastErr = err
				mu.Unlock()
				return
			}
			found[i] = ok
		}(i, e)
	}
	wg.Wait()

	still := make([]TestEntity, 0, len(todo))
	for i, e := range todo {
		if !found[i] {
			still = append(still, e)
		}
	}
	w.pending[node] = still
	return lastErr
}

// ReplicationPendingError reports pairs still pending after a sweep and the
// last read error seen.
type ReplicationPendingError struct {
	// Pending is the number of (node, entity) pairs not yet seen.
	Pending int

	// LastErr is the last read error of the sweep.
	LastErr error
}

func (e *ReplicationPendingError) Error() string {
	msg := strconv.Itoa(e.Pending) + " entity reads pending"
	if e.LastErr != nil {
		msg += ": " + e.LastErr.Error()
	}
	return msg
}

func (e *ReplicationPendingError) Unwrap() error { return e.LastErr }
