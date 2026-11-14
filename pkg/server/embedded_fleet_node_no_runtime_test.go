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
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Containers run in the fleet node agent co-located with quicbaasd, which
// holds the one in-process runtime; quicbaasd builds none of its own. With
// docker on PATH and a sandbox policy that reaches containers, the embedded
// node still starts without a container runtime: it has nothing to reap, and
// a sandboxed session placed on it fails closed rather than shelling out.
func TestStartEmbeddedFleetNode_BuildsNoContainerRuntime(t *testing.T) {
	bin := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(bin, "docker"), []byte("#!/bin/sh\nexit 0\n"), 0o755))
	t.Setenv("PATH", bin)

	srv := embeddedFleetNodeServer(t, t.TempDir())
	srv.config.Server.EmbeddedFleetNode.SandboxPolicy = "required"
	srv.config.Server.EmbeddedFleetNode.SandboxImage = "registry.example/worker:1"
	require.NoError(t, srv.startEmbeddedFleetNode())
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.stopEmbeddedFleetNode(ctx)
	})

	reaper, err := srv.embeddedFleetNodeState.runtime.ContainerReaper(time.Minute)
	require.NoError(t, err)
	require.Nil(t, reaper, "quicbaasd built a container runtime of its own")
}

// Under a policy that sandboxes sessions, the backends that run a CLI belong
// to the co-located agent; the embedded node keeps only those that spawn
// nothing, and advertises only those.
func TestStartEmbeddedFleetNode_LeavesSandboxedBackendsToTheAgent(t *testing.T) {
	srv := embeddedFleetNodeServer(t, t.TempDir())
	srv.config.Server.EmbeddedFleetNode.SandboxPolicy = "preferred"
	srv.config.Server.EmbeddedFleetNode.Backends = []string{"claude", "ollama"}
	require.NoError(t, srv.startEmbeddedFleetNode())
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.stopEmbeddedFleetNode(ctx)
	})

	reg := srv.embeddedFleetNodeState.registration
	require.Equal(t, []string{"ollama"}, reg.Capabilities)
	require.Equal(t, "ollama", reg.Backend)
}

// With every configured backend sandboxed, the embedded node has nothing to
// run: quicbaasd starts without it and leaves its work dir to the agent.
func TestStartEmbeddedFleetNode_StartsNothingWhenEveryBackendIsSandboxed(t *testing.T) {
	workDir := t.TempDir()
	srv := embeddedFleetNodeServer(t, workDir)
	srv.config.Server.EmbeddedFleetNode.SandboxPolicy = "required"
	srv.config.Server.EmbeddedFleetNode.Backends = []string{"claude"}
	require.NoError(t, srv.startEmbeddedFleetNode())

	require.Nil(t, srv.embeddedFleetNodeState)
	requireWorkDirFree(t, workDir)
}
