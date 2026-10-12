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
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// everyWorkspaceConcluded reports every workspace's task concluded, so the
// sweep removes whatever no live session holds.
type everyWorkspaceConcluded struct{}

func (everyWorkspaceConcluded) WorkspaceConcluded(context.Context, string, string, string) (bool, error) {
	return true, nil
}

// orchestratorWorkspaces installs the orchestrator's manager over the
// server's projects root, the way startOrchestration does, with a sweep that
// takes every unheld workspace.
func orchestratorWorkspaces(t *testing.T, srv *Server) *orchestration.WorkspaceManager {
	t.Helper()
	root := srv.config.Orchestration.EffectiveProjectsRoot(srv.config.Storage.DataDir)
	workspaces, err := orchestration.NewWorkspaceManager(root)
	require.NoError(t, err)
	workspaces.SetConclusions(everyWorkspaceConcluded{})
	srv.sessionWorkspaces = workspaces
	return workspaces
}

// TestEmbeddedFleetNodeWorkspaces_HoldBlocksTheSweep: a session the embedded
// fleet node provisions holds its workspace against the orchestrator's
// concluded-workspace sweep, because both provision through one manager.
func TestEmbeddedFleetNodeWorkspaces_HoldBlocksTheSweep(t *testing.T) {
	t.Parallel()
	srv := withTempServer(t)
	orchestrator := orchestratorWorkspaces(t, srv)

	embedded, err := srv.embeddedFleetNodeWorkspaces(orchestrator.Root())
	require.NoError(t, err)
	held, err := embedded.ProvisionSession(&orchestration.SessionWorkspace{TenantID: "t1", ProjectID: "p1", Key: "task-a1"})
	require.NoError(t, err)

	srv.sweepConcludedWorkspaces(context.Background(), orchestrator, nil)
	assert.DirExists(t, held.Dir, "the sweep removed a workspace an embedded session holds")

	held.Release()
	srv.sweepConcludedWorkspaces(context.Background(), orchestrator, nil)
	_, statErr := os.Stat(held.Dir)
	assert.ErrorIs(t, statErr, os.ErrNotExist, "a released workspace of a concluded task stays")
}

// TestEmbeddedFleetNodeWorkspaces_OwnManagerWithoutOrchestrator: with no
// orchestrator manager, or one over another root, the embedded node gets a
// manager of its own; a root the manager refuses is the typed error.
func TestEmbeddedFleetNodeWorkspaces_OwnManagerWithoutOrchestrator(t *testing.T) {
	t.Parallel()
	srv := withTempServer(t)
	root := t.TempDir()

	own, err := srv.embeddedFleetNodeWorkspaces(root)
	require.NoError(t, err)
	assert.Equal(t, root, own.Root())

	orchestrator := orchestratorWorkspaces(t, srv)
	other, err := srv.embeddedFleetNodeWorkspaces(root)
	require.NoError(t, err)
	assert.NotSame(t, orchestrator, other)

	_, err = srv.embeddedFleetNodeWorkspaces("relative/root")
	var rootErr *orchestration.WorkspaceRootError
	require.ErrorAs(t, err, &rootErr)
}

// TestStartEmbeddedFleetNode_SharesTheOrchestratorHolds: the started fleet
// node removes through the orchestrator's manager, so a workspace a session
// holds there is refused to the node's own RemoveWorkspace.
func TestStartEmbeddedFleetNode_SharesTheOrchestratorHolds(t *testing.T) {
	t.Parallel()
	srv := withTempServer(t)
	srv.fleetRegistry = orchestration.NewInMemoryFleetRegistry()
	srv.config.Server.EmbeddedFleetNode.Enabled = true
	srv.config.Server.EmbeddedFleetNode.Backends = []string{"ollama"}
	srv.config.Server.EmbeddedFleetNode.OllamaURL = "http://127.0.0.1:11434"
	srv.config.Server.EmbeddedFleetNode.DataDir = t.TempDir()
	orchestrator := orchestratorWorkspaces(t, srv)

	require.NoError(t, srv.startEmbeddedFleetNode())
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.stopEmbeddedFleetNode(ctx)
	})

	ws := &orchestration.SessionWorkspace{TenantID: "t1", ProjectID: "p1", Key: "task-a1"}
	held, err := orchestrator.ProvisionSession(ws)
	require.NoError(t, err)
	defer held.Release()

	_, err = srv.embeddedFleetNodeState.runtime.RemoveWorkspace("t1", ws)
	var heldErr *orchestration.WorkspaceHeldError
	require.ErrorAs(t, err, &heldErr)
	assert.DirExists(t, held.Dir)
}
