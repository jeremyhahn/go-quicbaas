package quicbaas

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
)

// TestListJobs_CursorResumes pages through more jobs than one page holds and
// sees every job exactly once, in pages of the requested size.
func TestListJobs_CursorResumes(t *testing.T) {
	cases := []struct {
		name      string
		ownerType scheduler.OwnerType
		ownerID   string
		query     scheduler.JobQuery
	}{
		{name: "system", ownerType: scheduler.OwnerTypeSystem},
		{
			name: "tenant", ownerType: scheduler.OwnerTypeTenant, ownerID: "acme",
			query: scheduler.JobQuery{OwnerType: scheduler.OwnerTypeTenant, OwnerID: "acme"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			storage := NewStorage(newMockKVStore())
			const total = 250
			want := make(map[uint64]bool, total)
			for i := 0; i < total; i++ {
				job := createTestJob("job", tc.ownerType, tc.ownerID)
				if err := storage.CreateJob(ctx, job); err != nil {
					t.Fatalf("CreateJob: %v", err)
				}
				want[job.ID] = true
			}

			seen := make(map[uint64]int, total)
			var sizes []int
			query := tc.query
			query.Limit = 100
			for page := 0; ; page++ {
				if page > 5 {
					t.Fatalf("paging did not end after %d pages: sizes %v", page, sizes)
				}
				jobs, cursor, err := storage.ListJobs(ctx, &query)
				if err != nil {
					t.Fatalf("ListJobs: %v", err)
				}
				sizes = append(sizes, len(jobs))
				for _, job := range jobs {
					seen[job.ID]++
				}
				if cursor == "" {
					break
				}
				query.Cursor = cursor
			}

			if len(sizes) != 3 || sizes[0] != 100 || sizes[1] != 100 || sizes[2] != 50 {
				t.Fatalf("page sizes %v, want [100 100 50]", sizes)
			}
			for id := range want {
				if seen[id] != 1 {
					t.Fatalf("job %d seen %d times, want 1", id, seen[id])
				}
			}
			if len(seen) != total {
				t.Fatalf("saw %d jobs, want %d", len(seen), total)
			}
		})
	}
}

// TestListJobs_CursorUnparsable rejects a cursor that is not a job ID rather
// than restarting the scan from the first job.
func TestListJobs_CursorUnparsable(t *testing.T) {
	ctx := context.Background()
	storage := NewStorage(newMockKVStore())
	if err := storage.CreateJob(ctx, createTestJob("job", scheduler.OwnerTypeSystem, "")); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	_, _, err := storage.ListJobs(ctx, &scheduler.JobQuery{Cursor: "not-an-id"})
	var storageErr *scheduler.ErrStorageError
	if !errors.As(err, &storageErr) {
		t.Fatalf("ListJobs with an unparsable cursor: err %v, want an ErrStorageError", err)
	}
}

// TestListRemoteRuns_PastFirstPage puts more expired running remote jobs than
// one storage page holds ahead of a live one, and still finds the live run.
func TestListRemoteRuns_PastFirstPage(t *testing.T) {
	const handler = "remote:renew-cert"
	ctx := context.Background()
	storage := NewStorage(newMockKVStore())
	engine := scheduler.New(scheduler.DefaultConfig(), storage)

	newRunning := func(id uint64, startedAt time.Time) *scheduler.Job {
		return &scheduler.Job{
			ID:           id,
			Name:         "renew-cert",
			OwnerType:    scheduler.OwnerTypeSystem,
			Type:         scheduler.JobTypeOneShot,
			Handler:      handler,
			Status:       scheduler.JobStatusRunning,
			Timeout:      time.Minute,
			RunStartedAt: startedAt,
		}
	}
	// IDs of one width, so key order is ID order: the expired jobs come first.
	for id := uint64(1000); id <= 1100; id++ {
		if err := storage.CreateJob(ctx, newRunning(id, time.Now().Add(-time.Hour))); err != nil {
			t.Fatalf("CreateJob: %v", err)
		}
	}
	live := newRunning(2000, time.Now())
	if err := storage.CreateJob(ctx, live); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	runs, err := engine.ListRemoteRuns(ctx, "", []string{handler})
	if err != nil {
		t.Fatalf("ListRemoteRuns: %v", err)
	}
	if len(runs) != 1 || runs[0].JobID != live.ID {
		t.Fatalf("ListRemoteRuns returned %d runs, want exactly the live job %d", len(runs), live.ID)
	}
}
