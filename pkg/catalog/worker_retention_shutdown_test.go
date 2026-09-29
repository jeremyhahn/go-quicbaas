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
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// newRetentionTestCatalog builds a real PebbleDB-backed catalog manager seeded
// with active entries, so a retention scan has something to list and read.
func newRetentionTestCatalog(t *testing.T, entries int) CatalogManager {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "retention-shutdown")
	storage, err := NewPebbleStorage(&PebbleStorageConfig{Path: dbPath, SyncWrites: false})
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}

	manager, err := NewCatalogManager(&ManagerConfig{Storage: storage})
	if err != nil {
		if closeErr := storage.Close(); closeErr != nil {
			t.Errorf("failed to close storage: %v", closeErr)
		}
		t.Fatalf("failed to create manager: %v", err)
	}
	t.Cleanup(func() {
		// Close is idempotent; tests that close the catalog themselves still
		// leave a well-defined cleanup here.
		if err := manager.Close(); err != nil {
			t.Errorf("failed to close catalog manager: %v", err)
		}
	})

	ctx := context.Background()
	for i := 0; i < entries; i++ {
		entry := createTestEntry(fmt.Sprintf("ns1:retention/object-%d", i))
		if err := manager.Put(ctx, entry); err != nil {
			t.Fatalf("failed to seed catalog entry: %v", err)
		}
	}

	return manager
}

// retentionTestWorkerConfig returns a worker configuration whose scan interval
// is long enough that only the scan performed at startup runs.
func retentionTestWorkerConfig() *RetentionWorkerConfig {
	return &RetentionWorkerConfig{
		WorkerConfig: &WorkerConfig{
			Workers:                1,
			BatchSize:              10,
			PollInterval:           time.Millisecond,
			MaxRetries:             1,
			RetryDelay:             time.Millisecond,
			RetryBackoffMultiplier: 1.0,
			TaskTimeout:            5 * time.Second,
		},
		ScanInterval: time.Hour,
	}
}

// closeUnderScanCatalog suspends a retention scan inside the catalog call it is
// making, so the test can close the store at exactly that moment and observe
// what the scan does next.
type closeUnderScanCatalog struct {
	CatalogManager

	// scanning is closed when a scan first reaches ListByStatus.
	scanning chan struct{}
	// storeClosed is closed by the test once the catalog store is closed.
	storeClosed chan struct{}

	once sync.Once
}

func (c *closeUnderScanCatalog) ListByStatus(ctx context.Context, status EntryStatus, opts *ListOptions) (*ListResult, error) {
	// Read the keys while the store is still open, then hold the scan here
	// until the store has been closed underneath it. Everything the scan does
	// from this point on runs against a closed store.
	result, err := c.CatalogManager.ListByStatus(ctx, status, opts)

	c.once.Do(func() { close(c.scanning) })
	<-c.storeClosed

	return result, err
}

// TestRetentionWorkerScanSurvivesStoreCloseUnderIt pins go-quicbaas#986: a scan
// that is in flight when the catalog store closes must end as an ordinary
// end-of-life condition. Before the fix the scan iterated a closed PebbleDB,
// which panics on the worker's goroutine and takes the whole test binary with
// it rather than failing a single test.
func TestRetentionWorkerScanSurvivesStoreCloseUnderIt(t *testing.T) {
	catalogMgr := newRetentionTestCatalog(t, 5)

	gated := &closeUnderScanCatalog{
		CatalogManager: catalogMgr,
		scanning:       make(chan struct{}),
		storeClosed:    make(chan struct{}),
	}

	worker, err := NewRetentionWorker(gated, newMockBackendManager(), retentionTestWorkerConfig(), &mockLogger{})
	if err != nil {
		t.Fatalf("failed to create retention worker: %v", err)
	}

	if err := worker.Start(context.Background()); err != nil {
		t.Fatalf("failed to start retention worker: %v", err)
	}

	// Wait until a scan is genuinely in flight, then pull the store out from
	// under it.
	<-gated.scanning
	if err := catalogMgr.Close(); err != nil {
		t.Fatalf("failed to close catalog manager: %v", err)
	}
	close(gated.storeClosed)

	// Stop joins the scan goroutine: if it returns, the scan finished without
	// panicking through the closed store.
	if err := worker.Stop(); err != nil {
		t.Fatalf("failed to stop retention worker: %v", err)
	}

	if worker.IsRunning() {
		t.Error("worker must not report running after Stop")
	}

	// The closed store reports itself as closed rather than failing some other
	// way, which is what lets the scan treat it as end of life.
	if _, err := catalogMgr.ListByStatus(context.Background(), StatusActive, &ListOptions{Limit: 1}); !IsCatalogClosed(err) {
		t.Errorf("expected catalog closed error after Close, got %v", err)
	}
}

// scanTrackingCatalog records whether a scan is inside the catalog and how many
// scans have run, so a test can assert what Stop joined.
type scanTrackingCatalog struct {
	CatalogManager

	inScan  atomic.Int64
	scans   atomic.Int64
	entered chan struct{}
	once    sync.Once
}

func (c *scanTrackingCatalog) ListByStatus(ctx context.Context, status EntryStatus, opts *ListOptions) (*ListResult, error) {
	c.inScan.Add(1)
	defer c.inScan.Add(-1)
	c.scans.Add(1)
	c.once.Do(func() { close(c.entered) })

	return c.CatalogManager.ListByStatus(ctx, status, opts)
}

// TestRetentionWorkerStopJoinsScanBeforeStoreCloses asserts the shutdown
// ordering the server relies on: once Stop returns, no scan is still reading
// the catalog, so the owner of the store may close it safely.
func TestRetentionWorkerStopJoinsScanBeforeStoreCloses(t *testing.T) {
	catalogMgr := newRetentionTestCatalog(t, 3)

	tracked := &scanTrackingCatalog{
		CatalogManager: catalogMgr,
		entered:        make(chan struct{}),
	}

	worker, err := NewRetentionWorker(tracked, newMockBackendManager(), retentionTestWorkerConfig(), &mockLogger{})
	if err != nil {
		t.Fatalf("failed to create retention worker: %v", err)
	}

	if err := worker.Start(context.Background()); err != nil {
		t.Fatalf("failed to start retention worker: %v", err)
	}

	<-tracked.entered
	if err := worker.Stop(); err != nil {
		t.Fatalf("failed to stop retention worker: %v", err)
	}

	if got := tracked.inScan.Load(); got != 0 {
		t.Errorf("Stop returned with %d scans still reading the catalog, want 0", got)
	}
	if got := tracked.scans.Load(); got < 1 {
		t.Errorf("expected at least the startup scan to have run, got %d", got)
	}

	// Closing after the join is the supported order and must succeed.
	if err := catalogMgr.Close(); err != nil {
		t.Errorf("failed to close catalog manager after worker stopped: %v", err)
	}
}

// TestRetentionWorkerStartRejectsSecondStart covers the error path of the
// goroutine accounting added to Start: a rejected start must not leave a
// pending wait-group counter behind, or the next Stop would block forever.
func TestRetentionWorkerStartRejectsSecondStart(t *testing.T) {
	catalogMgr := newRetentionTestCatalog(t, 1)

	worker, err := NewRetentionWorker(catalogMgr, newMockBackendManager(), retentionTestWorkerConfig(), &mockLogger{})
	if err != nil {
		t.Fatalf("failed to create retention worker: %v", err)
	}

	if err := worker.Start(context.Background()); err != nil {
		t.Fatalf("failed to start retention worker: %v", err)
	}

	if err := worker.Start(context.Background()); err == nil {
		t.Error("expected an error when starting an already running worker")
	}

	stopped := make(chan error, 1)
	go func() { stopped <- worker.Stop() }()

	select {
	case err := <-stopped:
		if err != nil {
			t.Fatalf("failed to stop retention worker: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("Stop did not return: the rejected Start left goroutine accounting behind")
	}
}
