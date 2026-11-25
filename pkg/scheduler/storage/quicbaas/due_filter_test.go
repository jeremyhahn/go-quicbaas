package quicbaas

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
)

// TestGetJobsDueForExecution_FilteredJobsDoNotFillThePage checks that jobs
// the caller cannot dispatch do not count toward limit: acme's three jobs
// pinned to another node come first in the index, and zeta's job behind
// them is still returned.
func TestGetJobsDueForExecution_FilteredJobsDoNotFillThePage(t *testing.T) {
	ctx := context.Background()
	kv := &sortedScanKV{mockKVStore: newMockKVStore()}
	storage := NewStorage(kv)
	if err := storage.InitializeJobIDCounter(ctx); err != nil {
		t.Fatalf("open: %v", err)
	}
	now := time.Now()

	for _, name := range []string{"ghost-1", "ghost-2", "ghost-3"} {
		job := createTestJob(name, scheduler.OwnerTypeTenant, "acme")
		job.NodeAffinity = "ghost"
		job.NextRun = now.Add(-time.Minute)
		if err := storage.CreateJob(ctx, job); err != nil {
			t.Fatalf("CreateJob %s: %v", name, err)
		}
	}
	zeta := createTestJob("zeta-job", scheduler.OwnerTypeTenant, "zeta")
	zeta.NextRun = now.Add(-time.Minute)
	if err := storage.CreateJob(ctx, zeta); err != nil {
		t.Fatalf("CreateJob zeta: %v", err)
	}

	filter := func(j *scheduler.Job) bool {
		return j.NodeAffinity != "ghost"
	}
	due, err := storage.GetJobsDueForExecution(ctx, now, 2, filter)
	if err != nil {
		t.Fatalf("filtered scan: %v", err)
	}
	if len(due) != 1 || due[0].Name != "zeta-job" {
		t.Fatalf("filtered scan = %v, want zeta-job only", jobNames(due))
	}

	due, err = storage.GetJobsDueForExecution(ctx, now, 2, nil)
	if err != nil {
		t.Fatalf("unfiltered scan: %v", err)
	}
	// Without the filter the ghost jobs count: the page is full, and holds
	// at least one of them (the tenant the scan starts at rotates).
	ghosts := 0
	for _, j := range due {
		if j.NodeAffinity == "ghost" {
			ghosts++
		}
	}
	if len(due) != 2 || ghosts == 0 {
		t.Fatalf("unfiltered scan = %v, want a full page with ghost jobs", jobNames(due))
	}
}

// A sweep canceled mid-scan stops at the next entry: the store does not check
// the context between entries, so the scan must.
func TestGetJobsDueForExecution_CanceledSweepStopsScanning(t *testing.T) {
	ctx := context.Background()
	kv := &sortedScanKV{mockKVStore: newMockKVStore()}
	storage := NewStorage(kv)
	if err := storage.InitializeJobIDCounter(ctx); err != nil {
		t.Fatalf("open: %v", err)
	}
	now := time.Now()
	for _, tenant := range []string{"t1", "t2", "t3", "t4"} {
		for _, name := range []string{"a", "b", "c"} {
			job := createTestJob(tenant+"-"+name, scheduler.OwnerTypeTenant, tenant)
			job.NextRun = now.Add(-time.Minute)
			if err := storage.CreateJob(ctx, job); err != nil {
				t.Fatalf("CreateJob: %v", err)
			}
		}
	}

	sweep, cancel := context.WithCancel(ctx)
	defer cancel()
	calls := 0
	filter := func(j *scheduler.Job) bool {
		calls++
		cancel() // the sweep's deadline passes while this candidate is checked
		return false
	}
	_, err := storage.GetJobsDueForExecution(sweep, now, 100, filter)
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled sweep: err = %v, want context.Canceled", err)
	}
	if calls != 1 {
		t.Fatalf("filter ran %d times after the sweep was canceled, want 1", calls)
	}
}

// sortedScanKV scans in key order, as the real store does, so the index
// puts tenant "acme" before tenant "zeta".
type sortedScanKV struct {
	*mockKVStore
}

func (s *sortedScanKV) Scan(_ context.Context, prefix string, fn func(key string, value []byte) error) error {
	s.mu.RLock()
	keys := make([]string, 0, len(s.data))
	for k := range s.data {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	values := make(map[string][]byte, len(keys))
	for _, k := range keys {
		values[k] = s.data[k]
	}
	s.mu.RUnlock()
	sort.Strings(keys)
	for _, k := range keys {
		if err := fn(k, values[k]); err != nil {
			if errors.Is(err, ErrLimitReached) {
				return nil
			}
			return err
		}
	}
	return nil
}

func jobNames(jobs []*scheduler.Job) []string {
	names := make([]string, 0, len(jobs))
	for _, job := range jobs {
		names = append(names, job.Name)
	}
	return names
}
