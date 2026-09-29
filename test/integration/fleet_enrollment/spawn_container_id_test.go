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

package fleet_enrollment_test

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	"github.com/jeremyhahn/go-quicbaas/pkg/fleet"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// The worker's container comes home (go-quicbaas#740).
//
// The burn-in refused every remote worker with "runs no container" while the
// daemon that spawned it had, in fact, put the agent in one. Two hand-written
// mappings on either side of a gRPC call had both skipped the field:
// GRPCSpawnServer.Spawn never set SpawnSessionResponse.container_id and
// RemoteACPBackend.Spawn never read it. Neither omission produced an error;
// the id simply arrived empty and the SDLC runner, which will not let a worker
// begin a delivery it could never commit and push inside a container, refused
// it before its first turn.
//
// The unit tier pins both mappings. This scenario pins the thing neither can:
// that the id the control plane ends up holding is the id of a container that
// WAS ACTUALLY CREATED. The container runtime here records every container it
// launches, so the assertion is against the runtime's own record rather than
// against a value the test also supplied — the fake-runtime equivalent of
// reading `docker events` on the daemon host, which is what the issue asked
// for and what the burn-in could not produce.
//
// EVERY LAYER BETWEEN THE TWO ENDS IS PRODUCTION: a real ACPBackend taking the
// sandboxed branch, a real fleet.Runtime resolving the sandbox policy and the
// image map, a real GRPCSpawnServer, real gRPC over a real listener, and a
// real RemoteACPBackend as the client.
// =============================================================================

const (
	spawnContainerImage = "registry.test/quicbaas-worker:0.3.0-alpha"
	spawnContainerTest  = "tenant-spawn"
)

// recordingContainerRuntime is a ContainerRuntime that launches an in-process
// ACP responder for each interactive container and REMEMBERS every container it
// created. It is the test's `docker events`.
type recordingContainerRuntime struct {
	t *testing.T

	mu      sync.Mutex
	created []string
	specs   []*orchestration.ContainerSpec

	seq int
}

// containers returns the ids of every container this runtime created, in order.
func (r *recordingContainerRuntime) containers() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.created...)
}

// lastSpec returns the spec of the most recently launched container.
func (r *recordingContainerRuntime) lastSpec() *orchestration.ContainerSpec {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.specs) == 0 {
		return nil
	}
	return r.specs[len(r.specs)-1]
}

func (r *recordingContainerRuntime) RunInteractive(
	ctx context.Context, spec *orchestration.ContainerSpec,
) (*orchestration.InteractiveProcess, error) {
	// The stdio the agent inside the container would own. os.Pipe rather than
	// io.Pipe because the production transport reads with a bufio.Scanner over
	// an io.ReadCloser and closes it on teardown.
	serverRead, clientWrite, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	clientRead, serverWrite, err := os.Pipe()
	if err != nil {
		_ = clientWrite.Close()
		_ = serverRead.Close()
		return nil, err
	}

	r.mu.Lock()
	r.seq++
	containerID := "ctr-" + spec.Image + "-" + strconv.Itoa(r.seq)
	r.created = append(r.created, containerID)
	r.specs = append(r.specs, spec)
	r.mu.Unlock()

	done := make(chan struct{})
	go serveACPHandshake(serverRead, serverWrite, done)
	go func() {
		<-ctx.Done()
		close(done)
		_ = serverRead.Close()
		_ = serverWrite.Close()
	}()

	return &orchestration.InteractiveProcess{
		ContainerID: containerID,
		Stdin:       clientWrite,
		Stdout:      clientRead,
	}, nil
}

func (r *recordingContainerRuntime) Run(_ context.Context, spec *orchestration.ContainerSpec) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seq++
	id := "ctr-" + spec.Image + "-" + strconv.Itoa(r.seq)
	r.created = append(r.created, id)
	r.specs = append(r.specs, spec)
	return id, nil
}

// Exec refuses. Nothing in this scenario runs a command inside the worker's
// container — that path has no remote seam at all yet (go-quicbaas#743) — so an
// answer here would be the test asserting behaviour it never arranged.
func (r *recordingContainerRuntime) Exec(
	context.Context, string, *orchestration.ExecSpec,
) (*orchestration.ExecResult, error) {
	r.t.Fatal("Exec is not part of this scenario")
	return nil, nil
}

func (r *recordingContainerRuntime) Stop(context.Context, string) error   { return nil }
func (r *recordingContainerRuntime) Remove(context.Context, string) error { return nil }
func (r *recordingContainerRuntime) Logs(context.Context, string) (io.ReadCloser, error) {
	return io.NopCloser(nil), nil
}
func (r *recordingContainerRuntime) IsRunning(context.Context, string) (bool, error) {
	return true, nil
}
func (r *recordingContainerRuntime) IsAvailable() bool                  { return true }
func (r *recordingContainerRuntime) Pull(context.Context, string) error { return nil }

var _ orchestration.ContainerRuntime = (*recordingContainerRuntime)(nil)

// serveACPHandshake answers the two JSON-RPC calls ACPBackend makes against a
// freshly launched container — initialize, then session/new — so the spawn
// completes the way it does against a real agent image.
func serveACPHandshake(in io.Reader, out io.Writer, done <-chan struct{}) {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		select {
		case <-done:
			return
		default:
		}
		var msg struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			return
		}
		result := map[string]any{}
		if msg.Method == "session/new" {
			result["sessionId"] = "acp-session-in-container"
		}
		reply, err := json.Marshal(map[string]any{
			"jsonrpc": "2.0",
			"id":      msg.ID,
			"result":  result,
		})
		if err != nil {
			return
		}
		if _, err := out.Write(append(reply, '\n')); err != nil {
			return
		}
	}
}

// spawnBoundary stands up the daemon half (a real ACPBackend over the recording
// runtime, behind a real fleet.Runtime and GRPCSpawnServer) and the control
// plane half (a real RemoteACPBackend) with real gRPC between them.
func spawnBoundary(t *testing.T, workDir string) (*orchestration.RemoteACPBackend, *recordingContainerRuntime) {
	t.Helper()

	runtimeDouble := &recordingContainerRuntime{t: t}

	// A NON-CLI ACP backend, deliberately: this scenario is about the container
	// id crossing the gRPC boundary, so it takes the JSON-RPC container path.
	// The CLI container path (go-quicbaas#741) has its own scenario over a real
	// docker in sandbox_cli_container_test.go.
	backend := orchestration.NewACPBackend(orchestration.ACPBackendConfig{
		Command:      "opencode",
		Args:         []string{"acp"},
		DefaultModel: "model-from-daemon",
	})
	backend.SetContainerRuntime(runtimeDouble)

	daemonRuntime, err := fleet.NewRuntime(fleet.RuntimeConfig{
		Backends:         map[string]orchestration.AgentBackend{"acp": backend},
		SandboxPolicy:    orchestration.SandboxPolicyRequired,
		MountPolicy:      &orchestration.MountPolicy{AllowedRoots: []string{workDir}},
		ContainerRuntime: runtimeDouble,
		ImageMap:         map[string]string{"acp": spawnContainerImage},
		Logger:           spawnLogger(t),
	})
	require.NoError(t, err)

	lis := bufconn.Listen(1024 * 1024)
	server, err := fleet.NewGRPCSpawnServer(fleet.GRPCSpawnServerDeps{
		Runtime:          daemonRuntime,
		BindAddr:         "bufnet",
		Logger:           spawnLogger(t),
		ListenerOverride: lis,
	})
	require.NoError(t, err)
	server.SetIdentity(&orchestration.AgentIdentity{
		AgentID:          "agent-spawn",
		ServiceAccountID: "sa-spawn",
		TenantID:         spawnContainerTest,
		Role:             "worker",
		Scopes:           []string{orchestration.AgentScopeRuntimeSpawn},
		ClientID:         "client-spawn",
	})
	require.NoError(t, server.Start(context.Background()))

	central, err := orchestration.NewRemoteACPBackend(orchestration.RemoteACPBackendDeps{
		Registration: &orchestration.FleetNode{
			ID:       "agent-spawn",
			TenantID: spawnContainerTest,
			Name:     "spawn-daemon",
			Host:     "bufnet",
			Backend:  "acp",
			Kind:     orchestration.FleetNodeKindDaemon,
			Status:   orchestration.FleetNodeStatusRunning,
		},
		Backend: "acp",
		Logger:  spawnLogger(t),
		ConnDialer: func(_ context.Context, target string) (*grpc.ClientConn, error) {
			return grpc.NewClient(
				"passthrough:///"+target,
				grpc.WithTransportCredentials(insecure.NewCredentials()),
				grpc.WithContextDialer(func(_ context.Context, _ string) (net.Conn, error) {
					return lis.Dial()
				}),
			)
		},
	})
	require.NoError(t, err)

	t.Cleanup(func() {
		require.NoError(t, central.CloseAll())
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		require.NoError(t, server.Stop(stopCtx))
	})
	return central, runtimeDouble
}

// spawnLogger builds the production logger adapter both ends run behind, the
// same way daemonAccountPool does.
func spawnLogger(t *testing.T) interfaces.Logger {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json"})
	require.NoError(t, err)
	return logger.NewAdapter(log)
}

// TestE2E_RemoteSpawn_ReturnsTheContainerTheDaemonCreated is the scenario the
// burn-in blocked on. The control plane's session must name the container the
// daemon's runtime actually launched — proven against the runtime's own record,
// not against a value this test handed the daemon.
func TestE2E_RemoteSpawn_ReturnsTheContainerTheDaemonCreated(t *testing.T) {
	workDir := t.TempDir()
	central, runtimeDouble := spawnBoundary(t, workDir)

	session, err := central.Spawn(context.Background(), &orchestration.SpawnRequest{
		ProjectID:   "proj-burnin",
		WorkDir:     workDir,
		SandboxMode: orchestration.SandboxModeRequired,
	})
	require.NoError(t, err, "the sandboxed remote spawn failed")

	created := runtimeDouble.containers()
	require.Len(t, created, 1,
		"the daemon reported a session without launching exactly one container; "+
			"this is the burn-in's second observation — docker events recorded no worker container")

	require.NotEmpty(t, session.ContainerID,
		"the spawn came back with no container id: the control plane cannot tell this worker "+
			"from a host process, and pkg/sdlc refuses it as 'runs no container' (go-quicbaas#740)")
	require.Equal(t, created[0], session.ContainerID,
		"the id that came home is not the container the daemon's runtime created")

	// The container the daemon launched is the one the operator's image map
	// names — the daemon resolves the image, never the caller.
	spec := runtimeDouble.lastSpec()
	require.NotNil(t, spec)
	require.Equal(t, spawnContainerImage, spec.Image)
}

// TestE2E_RemoteSpawn_AnUnsandboxedSessionReportsNoContainer is the bad-input
// twin, and it guards the direction that matters most. Empty is a REAL answer:
// it is the evidence the SDLC runner refuses a worker on, so a boundary that
// manufactured an id here would let a delivery begin with its commit, its
// signature and its push aimed at a container that does not exist.
func TestE2E_RemoteSpawn_AnUnsandboxedSessionReportsNoContainer(t *testing.T) {
	workDir := t.TempDir()
	central, runtimeDouble := spawnBoundary(t, workDir)

	// The daemon's policy is Required, so a caller asking for None is refused
	// rather than quietly downgraded — and no container is created either way.
	_, err := central.Spawn(context.Background(), &orchestration.SpawnRequest{
		ProjectID:   "proj-burnin",
		WorkDir:     workDir,
		SandboxMode: orchestration.SandboxModeNone,
	})
	require.Error(t, err, "the daemon's required policy accepted an unsandboxed spawn")
	require.Empty(t, runtimeDouble.containers(),
		"a refused spawn still launched a container")
}
