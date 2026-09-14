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

//go:build integration && rest

package agent_enrollment_test

import (
	"bytes"
	"context"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	proto "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/agent"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// newTestLogger returns the daemon-scale logger these tests hand to the agent
// runtime — the same construction the harness uses for the control plane.
func newTestLogger(t *testing.T) interfaces.Logger {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json"})
	require.NoError(t, err)
	return logger.NewAdapter(log)
}

// The enrollment E2E used to stop at heartbeat, so the stage after it — the
// daemon actually being dispatched a session — was never exercised here. That
// is how go-quicbaas#732 reached the fleet: `agent serve` parsed
// --container-runtime, never handed it to the runtime, and every spawn under
// the default "required" policy was refused with "no container runtime
// available on target host". The tests below take an enrolled daemon's runtime
// all the way through a gRPC Spawn so that class of break fails here first.

const sandboxSpawnBufSize = 1024 * 1024

// stubSandboxBackend is the AgentBackend an enrolled daemon serves in these
// tests: it records the request the runtime resolved and claims a session,
// which is all the sandbox path needs to be observed.
type stubSandboxBackend struct {
	spawned chan *orchestration.SpawnRequest
}

func newStubSandboxBackend() *stubSandboxBackend {
	return &stubSandboxBackend{spawned: make(chan *orchestration.SpawnRequest, 1)}
}

func (b *stubSandboxBackend) Spawn(_ context.Context, req *orchestration.SpawnRequest) (*orchestration.Session, error) {
	b.spawned <- req
	return &orchestration.Session{ID: "sess-sandboxed", WorkDir: req.WorkDir}, nil
}

func (b *stubSandboxBackend) Prompt(_ context.Context, req *orchestration.PromptRequest) (*orchestration.PromptResponse, error) {
	return &orchestration.PromptResponse{SessionID: req.SessionID}, nil
}

func (b *stubSandboxBackend) Status(_ context.Context, id string) (*orchestration.SessionStatus, error) {
	return &orchestration.SessionStatus{SessionID: id}, nil
}

func (b *stubSandboxBackend) Close(_ context.Context, _ string) error { return nil }

func (b *stubSandboxBackend) Capabilities() orchestration.BackendCapabilities {
	return orchestration.BackendCapabilities{Kind: orchestration.BackendKindAgent}
}

func (b *stubSandboxBackend) Type() string         { return "claude" }
func (b *stubSandboxBackend) ToolGuidance() string { return "" }

var _ orchestration.AgentBackend = (*stubSandboxBackend)(nil)

// availableContainerRuntime stands in for the docker the vs04 daemon has
// installed: present and usable. It answers the one question the sandbox
// resolver asks — IsAvailable — and refuses anything that would actually
// launch a container, because no test here runs one.
type availableContainerRuntime struct{}

func (r *availableContainerRuntime) Run(context.Context, *orchestration.ContainerSpec) (string, error) {
	return "", &orchestration.ContainerRuntimeNotAvailableError{Message: "this test launches no container"}
}

func (r *availableContainerRuntime) RunInteractive(context.Context, *orchestration.ContainerSpec) (*orchestration.InteractiveProcess, error) {
	return nil, &orchestration.ContainerRuntimeNotAvailableError{Message: "this test launches no container"}
}

func (r *availableContainerRuntime) Exec(context.Context, string, *orchestration.ExecSpec) (*orchestration.ExecResult, error) {
	return nil, &orchestration.ContainerRuntimeNotAvailableError{Message: "this test runs nothing in a container"}
}

func (r *availableContainerRuntime) Stop(context.Context, string) error   { return nil }
func (r *availableContainerRuntime) Remove(context.Context, string) error { return nil }

func (r *availableContainerRuntime) Logs(context.Context, string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(nil)), nil
}

func (r *availableContainerRuntime) IsRunning(context.Context, string) (bool, error) {
	return false, nil
}

func (r *availableContainerRuntime) IsAvailable() bool                  { return true }
func (r *availableContainerRuntime) Pull(context.Context, string) error { return nil }
func (r *availableContainerRuntime) Command() string                    { return "docker" }

var _ orchestration.ContainerRuntime = (*availableContainerRuntime)(nil)

// spawnClientFor serves the runtime over an in-process AgentRuntime gRPC
// service — the same server `agent serve` binds — and returns a client for it.
func spawnClientFor(t *testing.T, runtime *agent.Runtime) proto.AgentRuntimeClient {
	t.Helper()

	listener := bufconn.Listen(sandboxSpawnBufSize)
	server, err := agent.NewGRPCSpawnServer(agent.GRPCSpawnServerDeps{
		Runtime:          runtime,
		BindAddr:         "bufnet",
		Logger:           newTestLogger(t),
		ListenerOverride: listener,
	})
	require.NoError(t, err)

	server.SetIdentity(&orchestration.AgentIdentity{
		AgentID:  enrollAgent,
		TenantID: enrollTenant,
		Role:     "worker",
		Scopes: []string{
			orchestration.AgentScopeRuntimeSpawn,
			orchestration.AgentScopeRuntimePrompt,
			orchestration.AgentScopeRuntimeStatus,
		},
	})
	require.NoError(t, server.Start(context.Background()))

	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		}),
	)
	require.NoError(t, err)

	t.Cleanup(func() {
		require.NoError(t, conn.Close())
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		require.NoError(t, server.Stop(stopCtx))
	})

	return proto.NewAgentRuntimeClient(conn)
}

// TestE2E_AgentSpawn_SandboxResolverSeesTheContainerRuntime is the stage after
// the heartbeat: a dispatched Spawn on a daemon configured exactly as the fleet
// configures it (--container-runtime docker --sandbox-policy required) is
// containerized and delivered, not refused. Drop the ContainerRuntime out of
// the runtime the way the serve path did and this fails with "no container
// runtime available on target host" — the live burn-in error, reproduced at the
// RPC boundary.
func TestE2E_AgentSpawn_SandboxResolverSeesTheContainerRuntime(t *testing.T) {
	backend := newStubSandboxBackend()

	runtime, err := agent.NewRuntime(agent.RuntimeConfig{
		Backends:         map[string]orchestration.AgentBackend{"claude": backend},
		ContainerRuntime: &availableContainerRuntime{},
		SandboxPolicy:    orchestration.SandboxPolicyRequired,
		ImageMap:         map[string]string{"claude": "quicbaas/claude-code:v1"},
		Logger:           newTestLogger(t),
		MountPolicy:      &orchestration.MountPolicy{AllowedRoots: []string{t.TempDir()}},
	})
	require.NoError(t, err)
	require.True(t, runtime.SandboxResolver().HasContainerRuntime(),
		"the daemon's container runtime never reached the sandbox resolver")

	client := spawnClientFor(t, runtime)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, spawnErr := client.Spawn(ctx, &proto.SpawnSessionRequest{
		Backend:     "claude",
		TenantId:    enrollTenant,
		SandboxMode: proto.SandboxMode_SANDBOX_MODE_REQUIRED,
	})
	require.NoError(t, spawnErr, "a required spawn on a daemon that has a runtime must not be refused")
	require.Equal(t, "sess-sandboxed", resp.GetSessionId())

	select {
	case req := <-backend.spawned:
		require.Equal(t, orchestration.SandboxModeRequired, req.SandboxMode)
		require.True(t, req.Sandboxed, "the spawn reached the backend unsandboxed")
		require.Equal(t, "quicbaas/claude-code:v1", req.ContainerImage,
			"the daemon's image map must name the image for the backend")
	case <-ctx.Done():
		t.Fatal("the backend never received the spawn")
	}
}

// TestE2E_AgentSpawn_SandboxRequiredWithoutRuntimeIsRefusedAtBoot is the
// control that makes the case above evidence: the same daemon on a host with
// no container engine does not enroll, heartbeat and then refuse work one task
// at a time — it does not start at all, and says why.
func TestE2E_AgentSpawn_SandboxRequiredWithoutRuntimeIsRefusedAtBoot(t *testing.T) {
	logger := newTestLogger(t)

	containerRuntime, resolveErr := agent.ResolveContainerRuntime(agent.ContainerRuntimeNone, logger)
	require.NoError(t, resolveErr)
	require.Nil(t, containerRuntime, "selector none must resolve to no container runtime")

	runtime, err := agent.NewRuntime(agent.RuntimeConfig{
		Backends:         map[string]orchestration.AgentBackend{"claude": newStubSandboxBackend()},
		ContainerRuntime: containerRuntime,
		SandboxPolicy:    orchestration.SandboxPolicyRequired,
		Logger:           logger,
	})
	require.Nil(t, runtime)
	require.ErrorIs(t, err, agent.ErrRuntimeSandboxRequiredNoRuntime)
	require.True(t, strings.Contains(err.Error(), "no container runtime"),
		"the boot refusal must name the missing capability: %v", err)
}
