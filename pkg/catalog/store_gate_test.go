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

package catalog

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestStoreGate_EnterLeaveWhileOpen(t *testing.T) {
	gate := newStoreGate()

	if err := gate.enter(); err != nil {
		t.Fatalf("enter on an open gate: %v", err)
	}
	if gate.isClosed() {
		t.Error("an open gate must not report itself closed")
	}
	// Operations nest: an entry read may be issued from inside a delete.
	if err := gate.enter(); err != nil {
		t.Fatalf("nested enter on an open gate: %v", err)
	}
	gate.leave()
	gate.leave()

	if got := gate.inFlight.Load(); got != 0 {
		t.Errorf("in-flight count after balanced leaves = %d, want 0", got)
	}
}

func TestStoreGate_EnterAfterCloseFails(t *testing.T) {
	gate := newStoreGate()

	closeCalls := 0
	if err := gate.close(func() error { closeCalls++; return nil }); err != nil {
		t.Fatalf("close on an idle gate: %v", err)
	}

	if !gate.isClosed() {
		t.Error("a closed gate must report itself closed")
	}
	if err := gate.enter(); !IsCatalogClosed(err) {
		t.Errorf("enter after close = %v, want a catalog closed error", err)
	}
	if got := gate.inFlight.Load(); got != 0 {
		t.Errorf("a rejected enter must not leave an in-flight slot behind, got %d", got)
	}

	// Close is idempotent and runs the real close exactly once.
	if err := gate.close(func() error { closeCalls++; return nil }); err != nil {
		t.Errorf("second close = %v, want nil", err)
	}
	if closeCalls != 1 {
		t.Errorf("close function ran %d times, want 1", closeCalls)
	}
}

func TestStoreGate_CloseWaitsForInFlightOperation(t *testing.T) {
	gate := newStoreGate()

	if err := gate.enter(); err != nil {
		t.Fatalf("enter on an open gate: %v", err)
	}

	// closedWhileInFlight records whether the real close ran while the
	// operation still held the gate, which is exactly what must not happen.
	var leftGate, closedWhileInFlight atomic.Bool
	closed := make(chan error, 1)
	go func() {
		closed <- gate.close(func() error {
			if !leftGate.Load() {
				closedWhileInFlight.Store(true)
			}
			return nil
		})
	}()

	// The close cannot complete until this operation leaves the gate.
	leftGate.Store(true)
	gate.leave()

	select {
	case err := <-closed:
		if err != nil {
			t.Fatalf("close: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("close did not return after the in-flight operation left the gate")
	}

	if closedWhileInFlight.Load() {
		t.Error("the database was closed while an operation was still in flight")
	}
}

// TestPebbleStorageCloseWaitsForInFlightRead is the storage-level statement of
// go-quicbaas#986: Close is a join. An iteration that is running when Close is
// called finishes first, so PebbleDB is never closed under a live iterator.
func TestPebbleStorageCloseWaitsForInFlightRead(t *testing.T) {
	storage := createTestPebbleStorage(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if err := storage.PutEntry(ctx, createPebbleTestEntry(fmt.Sprintf("ns1:gate/object-%d", i))); err != nil {
			t.Fatalf("failed to seed entry: %v", err)
		}
	}

	inIteration := make(chan struct{})
	release := make(chan struct{})
	var callbackFinished atomic.Bool
	iterateErr := make(chan error, 1)

	go func() {
		iterateErr <- storage.Iterate(ctx, "", func(entry *CatalogEntry) bool {
			close(inIteration)
			<-release
			callbackFinished.Store(true)
			return false // stop after the first entry
		})
	}()

	<-inIteration

	closeErr := make(chan error, 1)
	go func() { closeErr <- storage.Close() }()

	close(release)

	select {
	case err := <-closeErr:
		if err != nil {
			t.Fatalf("close: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("Close did not return after the iteration finished")
	}

	if !callbackFinished.Load() {
		t.Error("Close returned while the iteration was still reading the store")
	}
	if err := <-iterateErr; err != nil {
		t.Errorf("iterate returned %v, want nil", err)
	}

	// Reads that arrive after Close get an ordinary closed-catalog error.
	if _, err := storage.GetEntry(ctx, "ns1:gate/object-0"); !IsCatalogClosed(err) {
		t.Errorf("GetEntry after Close = %v, want a catalog closed error", err)
	}
}

// TestPebbleStorageReadsDuringCloseDoNotPanic drives the shape that took the
// pkg/server suite down in go-quicbaas#986: readers racing the close of the
// store. Every reader must come back with either a result or a catalog-closed
// error -- never a panic out of PebbleDB.
func TestPebbleStorageReadsDuringCloseDoNotPanic(t *testing.T) {
	storage := createTestPebbleStorage(t)
	ctx := context.Background()

	for i := 0; i < 20; i++ {
		if err := storage.PutEntry(ctx, createPebbleTestEntry(fmt.Sprintf("ns1:race/object-%d", i))); err != nil {
			t.Fatalf("failed to seed entry: %v", err)
		}
	}

	indexes := storage.GetPebbleIndexManager()

	const readers = 8
	var ready sync.WaitGroup
	var done sync.WaitGroup
	ready.Add(readers)
	done.Add(readers)

	start := make(chan struct{})
	errs := make(chan error, readers*3)

	for i := 0; i < readers; i++ {
		go func(i int) {
			defer done.Done()
			ready.Done()
			<-start

			if _, err := indexes.ListByStatus(ctx, StatusActive, &ListOptions{Limit: 100}); err != nil && !IsCatalogClosed(err) {
				errs <- err
			}
			if _, err := indexes.CountByStatus(ctx, StatusActive); err != nil && !IsCatalogClosed(err) {
				errs <- err
			}
			if _, err := storage.GetEntry(ctx, fmt.Sprintf("ns1:race/object-%d", i)); err != nil && !IsCatalogClosed(err) {
				errs <- err
			}
		}(i)
	}

	ready.Wait()
	close(start)

	if err := storage.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	done.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("reader racing Close returned an unexpected error: %v", err)
	}
}
