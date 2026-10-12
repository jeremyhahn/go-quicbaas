// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

package server

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// countingWorkspaceSweeper reports each sweep on swept and answers with err.
type countingWorkspaceSweeper struct {
	swept chan struct{}
	err   error
}

func (c *countingWorkspaceSweeper) SweepConcludedWorkspaces() ([]string, error) {
	c.swept <- struct{}{}
	if c.err != nil {
		return nil, c.err
	}
	return []string{"/projects/t1/p1/workspace/task-a1"}, nil
}

// countingRemoteReconciler reports each reconcile on reconciled and answers
// with report and err.
type countingRemoteReconciler struct {
	reconciled chan struct{}
	report     orchestration.RemoteWorkspaceReport
	err        error
}

func (c *countingRemoteReconciler) ReconcileRemoteWorkspaces(context.Context) (orchestration.RemoteWorkspaceReport, error) {
	c.reconciled <- struct{}{}
	return c.report, c.err
}

// TestRunWorkspaceSweep_SweepsOnEveryTickUntilCancelled: each tick is one
// local sweep and one fleet node reconcile, a failing one does not stop the
// next, and cancellation ends the loop.
func TestRunWorkspaceSweep_SweepsOnEveryTickUntilCancelled(t *testing.T) {
	t.Parallel()
	srv := &Server{logger: newDrainTestLogger(t)}
	sweeper := &countingWorkspaceSweeper{swept: make(chan struct{}, 2), err: errors.New("disk gone")}
	remote := &countingRemoteReconciler{
		reconciled: make(chan struct{}, 2),
		report:     orchestration.RemoteWorkspaceReport{Removed: 1},
		err:        errors.New("node unreachable"),
	}
	tick := make(chan time.Time)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.runWorkspaceSweep(ctx, sweeper, remote, tick)
	}()

	for range 2 {
		tick <- time.Time{}
		<-sweeper.swept
		<-remote.reconciled
	}
	cancel()
	<-done
}

// TestStartWorkspaceSweep_RestartsAndStopsCleanly: a second start replaces
// the loop rather than leaking it, and stopping is idempotent.
func TestStartWorkspaceSweep_RestartsAndStopsCleanly(t *testing.T) {
	t.Parallel()
	srv := &Server{logger: newDrainTestLogger(t)}
	sweeper := &countingWorkspaceSweeper{swept: make(chan struct{}, 1)}

	srv.startWorkspaceSweep(sweeper, nil)
	first := srv.orchWorkspaceSweep
	srv.startWorkspaceSweep(sweeper, nil)
	if first == nil || srv.orchWorkspaceSweep == first {
		t.Fatal("a restart did not replace the sweep loop")
	}
	select {
	case <-first.done:
	default:
		t.Fatal("the replaced loop is still running")
	}

	srv.stopWorkspaceSweep()
	srv.stopWorkspaceSweep()
	if srv.orchWorkspaceSweep != nil {
		t.Fatal("a stopped sweep is still recorded")
	}
}
