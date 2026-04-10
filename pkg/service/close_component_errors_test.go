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
	"context"
	stderrors "errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
)

// =============================================================================
// Mock Scheduler Engine for Close Tests
// Implements interfaces.SchedulerEngine with all required methods.
// =============================================================================

type closeTestSchedulerEngine struct {
	stopFn func(ctx context.Context) error
}

func (m *closeTestSchedulerEngine) RegisterHandler(string, scheduler.JobHandler) error {
	return nil
}
func (m *closeTestSchedulerEngine) ScheduleJob(context.Context, *scheduler.Job) error {
	return nil
}
func (m *closeTestSchedulerEngine) GetJobWithTenant(context.Context, uint64, string) (*scheduler.Job, error) {
	return nil, nil
}
func (m *closeTestSchedulerEngine) UpdateJob(context.Context, uint64, string, *scheduler.JobUpdateParams) (*scheduler.Job, error) {
	return nil, nil
}
func (m *closeTestSchedulerEngine) DeleteJob(context.Context, uint64, string, bool) error {
	return nil
}
func (m *closeTestSchedulerEngine) ListJobsWithCursor(context.Context, *scheduler.JobQuery) ([]*scheduler.Job, string, error) {
	return nil, "", nil
}
func (m *closeTestSchedulerEngine) TriggerJob(context.Context, uint64, string) error { return nil }
func (m *closeTestSchedulerEngine) PauseJobWithStatus(context.Context, uint64, string) (scheduler.JobStatus, error) {
	return "", nil
}
func (m *closeTestSchedulerEngine) ResumeJobWithStatus(context.Context, uint64, string) (scheduler.JobStatus, error) {
	return "", nil
}
func (m *closeTestSchedulerEngine) CancelJobWithTenant(context.Context, uint64, string, string) (scheduler.JobStatus, error) {
	return "", nil
}
func (m *closeTestSchedulerEngine) GetJobHistory(context.Context, uint64, string, int) ([]*scheduler.JobExecution, error) {
	return nil, nil
}
func (m *closeTestSchedulerEngine) Stats() *scheduler.SchedulerStats { return nil }
func (m *closeTestSchedulerEngine) IsRunning() bool                  { return false }
func (m *closeTestSchedulerEngine) Start(context.Context) error      { return nil }
func (m *closeTestSchedulerEngine) Stop(ctx context.Context) error {
	if m.stopFn != nil {
		return m.stopFn(ctx)
	}
	return nil
}

// =============================================================================
// Close - Scheduler Stop Error Path
// =============================================================================

func TestQuicBaaS_Close_SchedulerStopError(t *testing.T) {
	log := createTestLogger(t)
	db := &quicBaaS{
		logger: log,
	}

	db.schedulerEngine = &closeTestSchedulerEngine{
		stopFn: func(ctx context.Context) error {
			return stderrors.New("scheduler failed to stop")
		},
	}

	// Scheduler stop errors are logged but do not propagate as return errors
	err := db.Close()
	assert.NoError(t, err)
	assert.True(t, db.closed)
}

func TestQuicBaaS_Close_SchedulerStopSuccess(t *testing.T) {
	log := createTestLogger(t)
	db := &quicBaaS{
		logger: log,
	}

	stopped := false
	db.schedulerEngine = &closeTestSchedulerEngine{
		stopFn: func(ctx context.Context) error {
			stopped = true
			return nil
		},
	}

	err := db.Close()
	assert.NoError(t, err)
	assert.True(t, stopped)
	assert.True(t, db.closed)
}

// =============================================================================
// Close - Scheduler stop error with nil logger (warn path)
// =============================================================================

func TestQuicBaaS_Close_SchedulerStopError_NilLogger(t *testing.T) {
	db := &quicBaaS{}

	db.schedulerEngine = &closeTestSchedulerEngine{
		stopFn: func(ctx context.Context) error {
			return stderrors.New("scheduler stop failed without logger")
		},
	}

	// Even with nil logger and scheduler error, Close should not panic
	err := db.Close()
	assert.NoError(t, err)
	assert.True(t, db.closed)
}

// =============================================================================
// Close - With cache manager and all nil components
// =============================================================================

func TestQuicBaaS_Close_CacheManagerSuccess(t *testing.T) {
	log := createTestLogger(t)
	cacheMock := newKVAuthCacheMock()
	db := &quicBaaS{
		logger:       log,
		cacheManager: cacheMock,
	}

	err := db.Close()
	assert.NoError(t, err)
	assert.True(t, db.closed)
}
