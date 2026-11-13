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

//go:build linux

package server

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/fleet"
)

// embeddedFleetNodeServer is a server whose embedded fleet node would start
// on workDir.
func embeddedFleetNodeServer(t *testing.T, workDir string) *Server {
	t.Helper()
	srv := withTempServer(t)
	srv.fleetRegistry = orchestration.NewInMemoryFleetRegistry()
	srv.config.Server.EmbeddedFleetNode.Enabled = true
	srv.config.Server.EmbeddedFleetNode.Backends = []string{"ollama"}
	srv.config.Server.EmbeddedFleetNode.OllamaURL = "http://127.0.0.1:11434"
	srv.config.Server.EmbeddedFleetNode.DataDir = workDir
	return srv
}

// requireWorkDirFree proves no runtime holds workDir by taking and dropping
// its lock.
func requireWorkDirFree(t *testing.T, workDir string) {
	t.Helper()
	lock, err := fleet.LockWorkDir(workDir)
	require.NoError(t, err, "the work dir must be free")
	require.NoError(t, lock.Release())
}

func TestStartEmbeddedFleetNode_RefusesAHeldWorkDir(t *testing.T) {
	t.Parallel()
	workDir := t.TempDir()
	held, err := fleet.LockWorkDir(workDir)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, held.Release()) })

	srv := embeddedFleetNodeServer(t, workDir)
	err = srv.startEmbeddedFleetNode()
	var locked *fleet.WorkDirLockedError
	require.ErrorAs(t, err, &locked)
	assert.Equal(t, workDir, locked.Dir)
	assert.Nil(t, srv.embeddedFleetNodeState, "a refused start must leave no agent state")
}

func TestStartEmbeddedFleetNode_HoldsTheWorkDirUntilStop(t *testing.T) {
	t.Parallel()
	workDir := t.TempDir()
	srv := embeddedFleetNodeServer(t, workDir)
	require.NoError(t, srv.startEmbeddedFleetNode())

	_, err := fleet.LockWorkDir(workDir)
	require.ErrorIs(t, err, fleet.ErrWorkDirLocked, "a running embedded node must hold its work dir")

	stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.stopEmbeddedFleetNode(stopCtx)
	requireWorkDirFree(t, workDir)
}

func TestStartEmbeddedFleetNode_FailedStartReleasesTheWorkDir(t *testing.T) {
	t.Parallel()
	workDir := t.TempDir()
	srv := embeddedFleetNodeServer(t, workDir)
	srv.config.Server.EmbeddedFleetNode.SandboxPolicy = "no-such-policy"

	require.Error(t, srv.startEmbeddedFleetNode())
	requireWorkDirFree(t, workDir)
}
