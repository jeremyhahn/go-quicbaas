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
	"testing"
	"time"
)

// TestStart_ACounterThatCannotLoadIsNotAStart pins that Start loads the job ID
// counter before anything can be scheduled, fails with a typed error when it
// cannot, and leaves the engine startable once the store recovers.
func TestStart_ACounterThatCannotLoadIsNotAStart(t *testing.T) {
	storage := newMockJobStorage()
	cause := errors.New("store unavailable")
	storage.initIDsErr = cause
	engine := New(DefaultConfig(), storage)

	err := engine.Start(context.Background())
	var storageErr ErrStorageError
	if !errors.As(err, &storageErr) || storageErr.Op != "InitializeJobIDCounter" {
		t.Fatalf("expected an InitializeJobIDCounter storage error, got %T: %v", err, err)
	}
	if !errors.Is(err, cause) {
		t.Errorf("the start error must carry its cause, got %v", err)
	}
	if engine.IsRunning() {
		t.Fatal("engine reports running after a failed start")
	}

	storage.errMu.Lock()
	storage.initIDsErr = nil
	storage.errMu.Unlock()
	if err := engine.Start(context.Background()); err != nil {
		t.Fatalf("Start after the store recovered: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := engine.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

// TestScheduleJob_RefusesSystemJobIDs pins the disjointness of the ID ranges
// at the scheduling boundary: a scheduled job may not claim an ID reserved for
// system jobs, where its record would overwrite the system job's.
func TestScheduleJob_RefusesSystemJobIDs(t *testing.T) {
	storage := newMockJobStorage()
	engine := New(DefaultConfig(), storage)
	if err := engine.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := engine.Stop(ctx); err != nil {
			t.Errorf("Stop: %v", err)
		}
	})

	reserved := &Job{
		ID: SystemJobIDFloor, Name: "squatter", Type: JobTypeOneShot, Handler: "h",
		ExecuteAt: time.Now().Add(time.Hour),
	}
	err := engine.ScheduleJob(context.Background(), reserved)
	var invalid InvalidJobError
	if !errors.As(err, &invalid) || invalid.Field != "id" {
		t.Fatalf("expected InvalidJobError on id, got %T: %v", err, err)
	}
	if storage.createJobCount.Load() != 0 {
		t.Error("a refused job reached storage")
	}

	allowed := &Job{Name: "tenant", Type: JobTypeOneShot, Handler: "h", ExecuteAt: time.Now().Add(time.Hour)}
	if err := engine.ScheduleJob(context.Background(), allowed); err != nil {
		t.Fatalf("ScheduleJob: %v", err)
	}
	if allowed.ID == 0 || allowed.ID >= SystemJobIDFloor {
		t.Errorf("allocated ID %d is outside the tenant range", allowed.ID)
	}
}
