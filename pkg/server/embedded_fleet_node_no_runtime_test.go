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

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
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

// thisHostEmbeddedID is the id startEmbeddedFleetNode registers under here.
func thisHostEmbeddedID() string {
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		hostname = "localhost"
	}
	return embeddedFleetNodeID(hostname)
}

// registerIdleFleetNode writes an idle registration as a previous run left it.
func registerIdleFleetNode(t *testing.T, registry orchestration.FleetRegistry, id string, kind orchestration.FleetNodeKind) {
	t.Helper()
	require.NoError(t, registry.RegisterFleetNode(context.Background(), &orchestration.FleetNode{
		ID:           id,
		TenantID:     embeddedFleetNodeTenantID,
		Name:         id,
		Host:         "h",
		Backend:      "claude",
		NodeType:     "embedded",
		Kind:         kind,
		Status:       orchestration.FleetNodeStatusIdle,
		Capabilities: []string{"claude"},
	}))
}

func TestStartEmbeddedFleetNode_StartsNothingRemovesThePreviousRegistration(t *testing.T) {
	srv := embeddedFleetNodeServer(t, t.TempDir())
	srv.config.Server.EmbeddedFleetNode.SandboxPolicy = "required"
	srv.config.Server.EmbeddedFleetNode.Backends = []string{"claude"}
	other := embeddedFleetNodeID("another-host")
	registerIdleFleetNode(t, srv.fleetRegistry, thisHostEmbeddedID(), orchestration.FleetNodeKindEmbedded)
	registerIdleFleetNode(t, srv.fleetRegistry, other, orchestration.FleetNodeKindEmbedded)

	require.NoError(t, srv.startEmbeddedFleetNode())

	require.Nil(t, srv.embeddedFleetNodeState)
	ctx := context.Background()
	_, err := srv.fleetRegistry.GetFleetNode(ctx, embeddedFleetNodeTenantID, thisHostEmbeddedID())
	require.ErrorIs(t, err, orchestration.ErrFleetNodeNotFound)
	_, err = srv.fleetRegistry.GetFleetNode(ctx, embeddedFleetNodeTenantID, other)
	require.NoError(t, err, "another host's registration is not this node's")
}

func TestStartEmbeddedFleetNode_StartsNothingKeepsAnotherKindUnderTheID(t *testing.T) {
	srv := embeddedFleetNodeServer(t, t.TempDir())
	srv.config.Server.EmbeddedFleetNode.SandboxPolicy = "required"
	srv.config.Server.EmbeddedFleetNode.Backends = []string{"claude"}
	registerIdleFleetNode(t, srv.fleetRegistry, thisHostEmbeddedID(), orchestration.FleetNodeKindDaemon)

	require.NoError(t, srv.startEmbeddedFleetNode())

	_, err := srv.fleetRegistry.GetFleetNode(context.Background(), embeddedFleetNodeTenantID, thisHostEmbeddedID())
	require.NoError(t, err)
}

func TestStartEmbeddedFleetNode_StartsNothingFailsWhenTheRegistrationCannotBeRemoved(t *testing.T) {
	workDir := t.TempDir()
	srv := embeddedFleetNodeServer(t, workDir)
	srv.config.Server.EmbeddedFleetNode.SandboxPolicy = "required"
	srv.config.Server.EmbeddedFleetNode.Backends = []string{"claude"}
	registerIdleFleetNode(t, srv.fleetRegistry, thisHostEmbeddedID(), orchestration.FleetNodeKindEmbedded)
	srv.fleetRegistry = removeErrRegistry{srv.fleetRegistry}

	err := srv.startEmbeddedFleetNode()

	require.ErrorContains(t, err, "remove the previous embedded registration")
	require.Nil(t, srv.embeddedFleetNodeState)
	requireWorkDirFree(t, workDir)
}
