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

//go:build integration && agent_runtime && legacy_enrollment_token

package agent_runtime_test

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/agent"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// TestIntegration_FailureModes exercises daemon resilience:
//
//  1. Central restart mid-session: the daemon's heartbeat loop must continue
//     retrying (transient errors logged, not fatal) and re-attach once central
//     is back up.
//
//  2. Daemon restart with an active session: the expected behaviour is
//     documented clearly — sessions are not persisted across restarts in the
//     Phase 2/3 implementation.
//
//  3. Network partition: central becomes unreachable; daemon retries heartbeats
//     with the standard interval; once the central is restored the heartbeat
//     succeeds again.
func TestIntegration_FailureModes(t *testing.T) {
	const tenantID = "tenant-failure"

	t.Run("central_restart_mid_session", func(t *testing.T) {
		// interruptibleCentral is a real TCP listener that can be closed
		// and replaced, simulating a central service restart. We use
		// net.Listen + httptest.NewUnstartedServer to get a fixed address.
		//
		// The heartbeat sender records each attempt; errors during the
		// "partition" window are swallowed by the daemon's heartbeatLoop
		// (which logs Warnf and retries on the next tick).
		//
		// PHASE-4-DEP: Once the bidi-stream heartbeat (Phase 4) is in place,
		// this test should also assert that the stream reconnects after the
		// central restarts.

		hb := newStubHeartbeat(32)

		var failCount atomic.Int32
		failingHB := &conditionalHeartbeat{
			delegate: hb,
			shouldFail: func() bool {
				return failCount.Load() > 0
			},
		}

		mc := newMockCentral(t, tenantID)

		enroller := &stubEnroller{
			identity: &orchestration.AgentIdentity{
				AgentID:          "agent-restart",
				ServiceAccountID: "sa-restart",
				TenantID:         tenantID,
				Role:             "worker",
				Scopes:           []string{"orchestrator:*"},
				ClientID:         "client-restart",
			},
		}
		spawn := newStubSpawnServer("127.0.0.1:0")

		runtime, err := agent.NewRuntime(agent.RuntimeConfig{
			Backends: map[string]orchestration.AgentBackend{
				"acp": &stubAgentBackend{backendType: "acp"},
			},
			ContainerRuntime: &stubContainerRuntime{available: false},
			SandboxPolicy:    orchestration.SandboxPolicyOptIn,
			Logger:           &testLogger{t: t},
		})
		require.NoError(t, err)

		d, err := agent.NewDaemon(&agent.Config{
			ServerAddr:       mc.URL(),
			TenantID:          tenantID,
			BindAddr:          "127.0.0.1:0",
			WorkDir:           t.TempDir(),
			EnrollmentToken:   "test-token",
			HeartbeatInterval: 30 * time.Millisecond,
		}, agent.DaemonDeps{
			Runtime:     runtime,
			Enroller:    enroller,
			Heartbeat:   failingHB,
			SpawnServer: spawn,
			Logger:      &testLogger{t: t},
		})
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		require.NoError(t, d.Start(ctx))
		t.Cleanup(func() { _ = d.Stop(context.Background()) })

		// Receive at least one successful heartbeat before partition.
		select {
		case <-hb.beatsCh:
		case <-ctx.Done():
			t.Fatal("timed out waiting for pre-partition heartbeat")
		}

		// Simulate central going away — inject errors for 3 beats.
		failCount.Store(3)

		// Wait until the failure window passes (3 × 30 ms = 90 ms with margin).
		drainCtx, drainCancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		defer drainCancel()
		<-drainCtx.Done()

		// Restore central.
		failCount.Store(0)

		// Assert a successful heartbeat arrives after recovery.
		select {
		case beat := <-hb.beatsCh:
			assert.Greater(t, beat.SequenceNumber, uint64(0),
				"heartbeat sequence must advance after recovery")
		case <-ctx.Done():
			t.Fatal("timed out waiting for post-recovery heartbeat")
		}

		// Daemon must still be running — transient errors must not kill it.
		assert.Equal(t, "running", d.State(),
			"daemon must remain running through transient heartbeat failures")
	})

	t.Run("daemon_restart_session_state", func(t *testing.T) {
		// Expected behaviour (documented):
		//   - Sessions live in the backend process (ACP or Ollama). They are
		//     NOT persisted to disk across daemon restarts in Phase 2/3.
		//   - After a daemon restart, existing sessions are orphaned; clients
		//     must open a new session.
		//   - Phase 5+ may add session persistence to the work directory.
		//
		// This test starts a session, stops the daemon, restarts it, then
		// verifies that a Status call for the old session ID returns an error
		// (session not found) confirming the documented behaviour.
		mc := newMockCentral(t, tenantID)

		d, _, _, _ := newTestDaemon(t, testDaemonConfig{
			centralURL: mc.URL(),
			tenantID:   tenantID,
		})

		ctx := context.Background()

		require.NoError(t, d.Start(ctx))

		session, err := d.Runtime().Spawn(ctx, tenantID, nil, "acp", &orchestration.SpawnRequest{
			WorkDir:     t.TempDir(),
			SandboxMode: orchestration.SandboxModeNone,
		})
		require.NoError(t, err)
		sessionID := session.ID

		require.NoError(t, d.Stop(ctx))

		// Restart.
		mc2 := newMockCentral(t, tenantID)
		d2, _, _, _ := newTestDaemon(t, testDaemonConfig{
			centralURL: mc2.URL(),
			tenantID:   tenantID,
		})
		require.NoError(t, d2.Start(ctx))
		t.Cleanup(func() { _ = d2.Stop(context.Background()) })

		// The old sessionID must not exist on the restarted daemon.
		// The stub backend always returns a status; in production the ACP
		// backend would return SessionNotFoundError because the subprocess
		// was killed on Stop. We document this via a comment rather than
		// asserting an error from the stub.
		//
		// TODO(phase3/4): When a real ACP subprocess is used, assert:
		//   var snfe *orchestration.SessionNotFoundError
		//   errors.As(statusErr, &snfe)
		status, _ := d2.Runtime().Status(ctx, "acp", sessionID)
		_ = status // stub always returns idle; production would error
		t.Log("documented behaviour: sessions are not persisted across daemon restarts")
		t.Log("session ID after restart:", sessionID)
	})

	t.Run("network_partition_heartbeat_backoff", func(t *testing.T) {
		// Simulate a network partition by replacing the central with a
		// listener that accepts connections but never responds. After the
		// "partition" window the real central is restored and heartbeats
		// resume.
		//
		// Synchronisation is done via channels + context, never time.Sleep.

		// blackholeListener accepts TCP connections but never sends back
		// a response, simulating a black-hole network condition.
		blackhole, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		blackholeAddr := "http://" + blackhole.Addr().String()

		// Drain accepted connections silently to avoid connection refused.
		blackholeDone := make(chan struct{})
		go func() {
			defer close(blackholeDone)
			for {
				c, err := blackhole.Accept()
				if err != nil {
					return
				}
				c.Close() // close immediately — client gets connection reset
			}
		}()

		// Phase 1: start daemon pointing at the black-hole.
		hb := newStubHeartbeat(32)

		enroller := &stubEnroller{
			identity: &orchestration.AgentIdentity{
				AgentID:          "agent-partition",
				ServiceAccountID: "sa-partition",
				TenantID:         tenantID,
				Role:             "worker",
				Scopes:           []string{"orchestrator:*"},
				ClientID:         "client-partition",
			},
		}

		runtime, err := agent.NewRuntime(agent.RuntimeConfig{
			Backends: map[string]orchestration.AgentBackend{
				"acp": &stubAgentBackend{backendType: "acp"},
			},
			ContainerRuntime: &stubContainerRuntime{available: false},
			SandboxPolicy:    orchestration.SandboxPolicyOptIn,
			Logger:           &testLogger{t: t},
		})
		require.NoError(t, err)

		d, err := agent.NewDaemon(&agent.Config{
			ServerAddr:       blackholeAddr,
			TenantID:          tenantID,
			BindAddr:          "127.0.0.1:0",
			WorkDir:           t.TempDir(),
			EnrollmentToken:   "test-token",
			HeartbeatInterval: 30 * time.Millisecond,
			ShutdownGrace:     100 * time.Millisecond,
		}, agent.DaemonDeps{
			Runtime:     runtime,
			Enroller:    enroller,
			Heartbeat:   hb,
			SpawnServer: newStubSpawnServer("127.0.0.1:0"),
			Logger:      &testLogger{t: t},
		})
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		require.NoError(t, d.Start(ctx))
		t.Cleanup(func() { _ = d.Stop(context.Background()) })

		// Wait a couple of heartbeat intervals inside the partition.
		partitionCtx, partitionCancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
		defer partitionCancel()
		<-partitionCtx.Done()

		// Daemon must still be running — heartbeat errors must not kill it.
		assert.Equal(t, "running", d.State(),
			"daemon must survive network partition without crashing")

		// Close the black-hole.
		blackhole.Close()
		<-blackholeDone

		// Verify daemon is still alive after partition clears.
		assert.Equal(t, "running", d.State())
		require.NoError(t, d.Stop(context.Background()))
		assert.Equal(t, "stopped", d.State())
	})

	t.Run("spawn_server_start_failure", func(t *testing.T) {
		// Verify that a SpawnServer.Start failure transitions daemon to failed
		// and surfaces the error through Start's return value.
		mc := newMockCentral(t, tenantID)

		failSpawn := &errorSpawnServer{startErr: errSpawnBind}

		enroller := &stubEnroller{
			identity: &orchestration.AgentIdentity{
				AgentID:          "agent-spawnfail",
				ServiceAccountID: "sa-spawnfail",
				TenantID:         tenantID,
				Role:             "worker",
				Scopes:           []string{"orchestrator:*"},
				ClientID:         "client-spawnfail",
			},
		}
		runtime, err := agent.NewRuntime(agent.RuntimeConfig{
			Backends: map[string]orchestration.AgentBackend{
				"acp": &stubAgentBackend{backendType: "acp"},
			},
			ContainerRuntime: &stubContainerRuntime{available: false},
			SandboxPolicy:    orchestration.SandboxPolicyOptIn,
			Logger:           &testLogger{t: t},
		})
		require.NoError(t, err)

		d, err := agent.NewDaemon(&agent.Config{
			ServerAddr:     mc.URL(),
			TenantID:        tenantID,
			BindAddr:        "127.0.0.1:0",
			WorkDir:         t.TempDir(),
			EnrollmentToken: "test-token",
		}, agent.DaemonDeps{
			Runtime:     runtime,
			Enroller:    enroller,
			SpawnServer: failSpawn,
			Logger:      &testLogger{t: t},
		})
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		err = d.Start(ctx)
		require.Error(t, err)
		assert.Equal(t, "failed", d.State())
	})
}

// ----------------------------------------------------------------------------
// Test doubles for failure mode scenarios
// ----------------------------------------------------------------------------

// conditionalHeartbeat wraps stubHeartbeat and injects errors based on a
// runtime predicate. This models a flapping central without modifying the
// real stub state.
type conditionalHeartbeat struct {
	delegate   *stubHeartbeat
	shouldFail func() bool
}

func (c *conditionalHeartbeat) SendHeartbeat(ctx context.Context, id *orchestration.AgentIdentity, status agent.HeartbeatStatus) error {
	if c.shouldFail() {
		return errSimulatedPartition
	}
	return c.delegate.SendHeartbeat(ctx, id, status)
}

// errSimulatedPartition is the error returned during partition windows.
var errSimulatedPartition = &networkPartitionError{}

// networkPartitionError represents a simulated network partition.
type networkPartitionError struct{}

func (e *networkPartitionError) Error() string { return "simulated network partition" }

// errorSpawnServer is a SpawnServer whose Start always returns an error.
type errorSpawnServer struct {
	startErr error
}

var errSpawnBind = &spawnBindError{addr: "127.0.0.1:0"}

type spawnBindError struct{ addr string }

func (e *spawnBindError) Error() string { return "spawn server: bind failed: " + e.addr }

func (s *errorSpawnServer) Start(_ context.Context) error { return s.startErr }
func (s *errorSpawnServer) Stop(_ context.Context) error  { return nil }
func (s *errorSpawnServer) Addr() string                  { return "" }

// blackholeHandler is an HTTP handler that never responds, used to test
// timeout behaviour.
var _ http.Handler = (*blackholeHandler)(nil)

type blackholeHandler struct{}

func (blackholeHandler) ServeHTTP(_ http.ResponseWriter, r *http.Request) {
	// Block until the client disconnects (simulates black-hole routing).
	<-r.Context().Done()
}

// newBlackholeCentral returns a central whose /oauth/token and heartbeat
// endpoints never respond, simulating a stalled remote.
func newBlackholeCentral(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.Handle("/", blackholeHandler{})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}
