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
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// workspaceSweepInterval is how often the daemon removes the session
// workspaces of concluded tasks while it serves (go-quicbaas#777). The discard
// stage removes most of them as their attempts end; the sweep takes what it
// could not decide or could not reach — the last attempt of a task whose
// budget ran out, a removal a fleet node refused — so it can be slow.
const workspaceSweepInterval = 15 * time.Minute

// concludedWorkspaceSweeper removes the session workspaces of concluded tasks.
// *orchestration.WorkspaceManager implements it.
type concludedWorkspaceSweeper interface {
	SweepConcludedWorkspaces() ([]string, error)
}

// remoteWorkspaceReconciler removes the workspaces concluded tasks left on
// fleet nodes. The agent service implements it.
type remoteWorkspaceReconciler interface {
	ReconcileRemoteWorkspaces(ctx context.Context) (orchestration.RemoteWorkspaceReport, error)
}

// startWorkspaceSweep sweeps the workspaces of concluded tasks every
// workspaceSweepInterval until shutdown: the ones under this daemon's root,
// and, when remote is set, the ones pinned to fleet nodes. The startup sweep
// has already run (SweepSessionSpecs), so the first tick is one interval out.
func (s *Server) startWorkspaceSweep(workspaces concludedWorkspaceSweeper, remote remoteWorkspaceReconciler) {
	// A re-initialised orchestrator replaces the loop rather than leaking it.
	s.stopWorkspaceSweep()
	ctx, stop := context.WithCancel(context.Background())
	loop := &orchestrationSweeps{stop: stop, done: make(chan struct{})}
	s.orchWorkspaceSweep = loop
	go func() {
		defer close(loop.done)
		ticker := time.NewTicker(workspaceSweepInterval)
		defer ticker.Stop()
		s.runWorkspaceSweep(ctx, workspaces, remote, ticker.C)
	}()
	s.logger.Infof("Concluded-workspace sweep started (interval=%s)", workspaceSweepInterval)
}

// runWorkspaceSweep sweeps once per tick until ctx is done.
func (s *Server) runWorkspaceSweep(ctx context.Context, workspaces concludedWorkspaceSweeper,
	remote remoteWorkspaceReconciler, tick <-chan time.Time) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick:
			s.sweepConcludedWorkspaces(ctx, workspaces, remote)
		}
	}
}

// sweepConcludedWorkspaces runs one sweep. A failure is logged: the next tick
// retries it, and nothing the daemon serves depends on it.
func (s *Server) sweepConcludedWorkspaces(ctx context.Context, workspaces concludedWorkspaceSweeper,
	remote remoteWorkspaceReconciler) {
	removed, err := workspaces.SweepConcludedWorkspaces()
	if err != nil {
		s.logger.WithError(err).Warn("orchestration: concluded-workspace sweep failed")
	}
	if len(removed) > 0 {
		s.logger.Infof("Removed %d session workspace(s) of concluded tasks", len(removed))
	}
	if remote == nil {
		return
	}
	report, err := remote.ReconcileRemoteWorkspaces(ctx)
	if err != nil {
		s.logger.WithError(err).Warn("orchestration: fleet node workspaces of concluded tasks not all removed")
	}
	if report.Removed > 0 {
		s.logger.Infof("Removed %d fleet node workspace(s) of concluded tasks", report.Removed)
	}
}

// stopWorkspaceSweep stops the sweep and waits for it.
func (s *Server) stopWorkspaceSweep() {
	if s.orchWorkspaceSweep == nil {
		return
	}
	s.orchWorkspaceSweep.stop()
	<-s.orchWorkspaceSweep.done
	s.orchWorkspaceSweep = nil
}
