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

package scheduler

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// renderingLogger records every emitted record fully rendered, so a test
// can assert on the values a record carries and not only its format.
type renderingLogger struct {
	mu      sync.Mutex
	records []loggedRecord
}

type loggedRecord struct {
	level string
	msg   string
}

func (l *renderingLogger) add(level, format string, args ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.records = append(l.records, loggedRecord{level: level, msg: fmt.Sprintf(format, args...)})
}

func (l *renderingLogger) Debugf(format string, args ...interface{}) { l.add("debug", format, args...) }
func (l *renderingLogger) Warnf(format string, args ...interface{})  { l.add("warn", format, args...) }
func (l *renderingLogger) Errorf(format string, args ...interface{}) { l.add("error", format, args...) }

// sweepRecords returns the records the due-job sweep itself emitted.
func (l *renderingLogger) sweepRecords() []loggedRecord {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []loggedRecord
	for _, r := range l.records {
		if strings.HasPrefix(r.msg, sweepLogPrefix) {
			out = append(out, r)
		}
	}
	return out
}

func (l *renderingLogger) all() []loggedRecord {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]loggedRecord(nil), l.records...)
}

// followerLeader reports this node as never the leader, so due
// cluster-wide jobs are found by the sweep but not dispatched.
type followerLeader struct{}

func (followerLeader) IsLeader() (bool, error) { return false, nil }

func newSweepLoggingEngine(storage JobStorage) (*SchedulerEngine, *renderingLogger) {
	logger := &renderingLogger{}
	config := DefaultConfig()
	config.Logger = logger
	return newEngine(config, storage, "node-a", followerLeader{}, nil), logger
}

// TestProcessDueJobs_IdleSweepEmitsNoRecord: a tick that finds nothing
// carries no information and must not write a log record (#934).
func TestProcessDueJobs_IdleSweepEmitsNoRecord(t *testing.T) {
	engine, logger := newSweepLoggingEngine(newMockJobStorage())

	for i := 0; i < 5; i++ {
		engine.processDueJobs(context.Background())
	}

	assert.Empty(t, logger.all(), "an idle sweep must emit no record")
}

// TestProcessDueJobs_FoundJobsEmitsOneRecordWithCount: a sweep that finds
// N due jobs emits exactly one sweep record, carrying N, without the old
// bracket prefix or a hand-formatted timestamp.
func TestProcessDueJobs_FoundJobsEmitsOneRecordWithCount(t *testing.T) {
	storage := newMockJobStorage()
	past := time.Now().Add(-time.Minute)
	for id := uint64(1); id <= 3; id++ {
		storage.jobs[id] = &Job{
			ID:      id,
			Name:    fmt.Sprintf("job-%d", id),
			Handler: "noop",
			Status:  JobStatusPending,
			NextRun: past,
		}
	}
	engine, logger := newSweepLoggingEngine(storage)

	engine.processDueJobs(context.Background())

	records := logger.sweepRecords()
	require.Len(t, records, 1, "one sweep record per sweep that finds work")
	assert.Equal(t, "debug", records[0].level)
	assert.Contains(t, records[0].msg, "due=3")
	assert.Contains(t, records[0].msg, "component=scheduler")
	for _, r := range logger.all() {
		assert.NotContains(t, r.msg, "[SCHEDULER TICK]")
		assert.NotContains(t, r.msg, "Checking for due jobs at")
	}
}

// TestProcessDueJobs_SweepErrorEmitsRecordNamingError: a failed sweep is
// logged at error level and names the underlying error.
func TestProcessDueJobs_SweepErrorEmitsRecordNamingError(t *testing.T) {
	storage := newMockJobStorage()
	storage.getJobsDueForExecutionErr = errors.New("raft shard unavailable")
	engine, logger := newSweepLoggingEngine(storage)

	engine.processDueJobs(context.Background())

	records := logger.sweepRecords()
	require.Len(t, records, 1)
	assert.Equal(t, "error", records[0].level)
	assert.Contains(t, records[0].msg, "raft shard unavailable")
	assert.Contains(t, records[0].msg, "component=scheduler")
	assert.Equal(t, uint64(1), engine.storageErrors.Load())
}
