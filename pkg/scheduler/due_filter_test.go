package scheduler

import (
	"context"
	"sync"
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
}

func (f *filterRecordingStorage) GetJobsDueForExecution(ctx context.Context, before time.Time, limit int) ([]*Job, error) {
	f.mu.Lock()
	f.filters = append(f.filters, DueJobFilterFrom(ctx))
	f.mu.Unlock()
	return f.mockJobStorage.GetJobsDueForExecution(ctx, before, limit)
}

func TestDueJobFilterFrom_NilWhenUnset(t *testing.T) {
	assert.Nil(t, DueJobFilterFrom(context.Background()))
}

func TestProcessDueJobs_PassesCanDispatchFilter(t *testing.T) {
	storage := &filterRecordingStorage{mockJobStorage: newMockJobStorage()}
	engine := newEngine(DefaultConfig(), storage, "node-a", followerLeader{}, nil)

	engine.processDueJobs(context.Background())

	storage.mu.Lock()
	defer storage.mu.Unlock()
	require.Len(t, storage.filters, 1)
	filter := storage.filters[0]
	require.NotNil(t, filter, "processDueJobs must pass a DueJobFilter")

	jobs := []*Job{
		{ID: 1, Name: "other-node", NodeAffinity: "node-b"},
		{ID: 2, Name: "this-node", NodeAffinity: "node-a"},
		{ID: 3, Name: "cluster-wide"},
	}
	for _, job := range jobs {
		want, _ := engine.canDispatch(job)
		assert.Equal(t, want, filter(job), "filter(%s) must match canDispatch", job.Name)
	}
	assert.False(t, filter(jobs[0]), "a job affine to another node is not dispatchable here")
	assert.True(t, filter(jobs[1]), "a job affine to this node is dispatchable")
}
