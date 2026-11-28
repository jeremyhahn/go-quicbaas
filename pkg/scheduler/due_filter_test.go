package scheduler

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// filterRecordingStorage records the DueJobFilter each due scan receives.
type filterRecordingStorage struct {
	*mockJobStorage
	mu      sync.Mutex
	filters []DueJobFilter
	// probe jobs are put to the filter during the sweep, as a store's scan
	// does; verdicts records its answers.
	probe    []*Job
	verdicts []bool
}

func (f *filterRecordingStorage) GetJobsDueForExecution(ctx context.Context, before time.Time, limit int, eligible DueJobFilter) ([]*Job, error) {
	f.mu.Lock()
	f.filters = append(f.filters, eligible)
	if eligible != nil {
		for _, job := range f.probe {
			f.verdicts = append(f.verdicts, eligible(job))
		}
	}
	f.mu.Unlock()
	return f.mockJobStorage.GetJobsDueForExecution(ctx, before, limit, eligible)
}

func TestProcessDueJobs_PassesCanDispatchFilter(t *testing.T) {
	jobs := []*Job{
		{ID: 1, Name: "other-node", NodeAffinity: "node-b"},
		{ID: 2, Name: "this-node", NodeAffinity: "node-a"},
		{ID: 3, Name: "cluster-wide"},
	}
	storage := &filterRecordingStorage{mockJobStorage: newMockJobStorage(), probe: jobs}
	engine := newEngine(DefaultConfig(), storage, "node-a", followerLeader{}, nil)

	engine.processDueJobs(context.Background())

	storage.mu.Lock()
	defer storage.mu.Unlock()
	require.Len(t, storage.filters, 1)
	filter := storage.filters[0]
	require.NotNil(t, filter, "processDueJobs must pass a DueJobFilter")
	require.Len(t, storage.verdicts, len(jobs))

	for i, job := range jobs {
		want, _ := engine.canDispatch(job)
		assert.Equal(t, want, storage.verdicts[i], "filter(%s) must match canDispatch", job.Name)
	}
	assert.False(t, storage.verdicts[0], "a job affine to another node is not dispatchable here")
	assert.True(t, storage.verdicts[1], "a job affine to this node is dispatchable")
	assert.False(t, filter(jobs[1]), "after the sweep its filter admits nothing")
}

// slowLeader is a leader checker whose every lookup takes delay; it counts
// lookups.
type slowLeader struct {
	delay time.Duration
	leads bool
	err   error
	calls atomic.Int32
}

func (l *slowLeader) IsLeader() (bool, error) {
	l.calls.Add(1)
	time.Sleep(l.delay)
	return l.leads, l.err
}

// A sweep asks a slow leader checker at most once, however many cluster-wide
// candidates it rejects, and a failed lookup is not retried per candidate.
func TestSweepDueFilter_OneLeaderLookupPerSweep(t *testing.T) {
	for _, tc := range []struct {
		name   string
		leader *slowLeader
		want   bool
	}{
		{"follower", &slowLeader{delay: 20 * time.Millisecond}, false},
		{"lookup fails", &slowLeader{delay: 20 * time.Millisecond, err: errors.New("membership stalled")}, false},
		{"leader", &slowLeader{delay: 20 * time.Millisecond, leads: true}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			engine := newEngine(DefaultConfig(), newMockJobStorage(), "node-a", tc.leader, nil)
			filter := engine.sweepDueFilter(context.Background())

			start := time.Now()
			for i := 0; i < 100; i++ {
				assert.Equal(t, tc.want, filter(&Job{ID: uint64(i + 1), Name: "cluster-wide"}))
			}
			assert.True(t, filter(&Job{ID: 200, NodeAffinity: "node-a"}), "an affine job needs no lookup")
			assert.Equal(t, int32(1), tc.leader.calls.Load(), "one leadership lookup per sweep")
			assert.Less(t, time.Since(start), time.Second, "100 candidates do not wait on 100 lookups")
		})
	}
}

// Once the sweep's context is done the filter admits nothing and asks the
// leader checker nothing.
func TestSweepDueFilter_CanceledSweepAdmitsNothing(t *testing.T) {
	leader := &slowLeader{leads: true}
	engine := newEngine(DefaultConfig(), newMockJobStorage(), "node-a", leader, nil)
	ctx, cancel := context.WithCancel(context.Background())
	filter := engine.sweepDueFilter(ctx)
	cancel()

	assert.False(t, filter(&Job{ID: 1, Name: "cluster-wide"}))
	assert.False(t, filter(&Job{ID: 2, NodeAffinity: "node-a"}))
	assert.Equal(t, int32(0), leader.calls.Load())
}
