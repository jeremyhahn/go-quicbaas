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

//go:build integration && fleet_runtime

package fleet_runtime_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/fleet"
)

// TestIntegration_DaemonLifecycle exercises the full daemon lifecycle:
//
//	Start → enrolled state → spawn session → prompt → close → Stop
//
// The mock central server (httptest) handles /api/v1/agents/enroll and
// /oauth/token so no external service is required.
//
// TODO(phase3): swap the stubEnroller for fleet.NewProductionEnroller(cfg)
// once enrollment.go is merged. The production enroller will POST to
// mc.URL()+"/api/v1/agents/enroll" and decode the enrollResponse JSON.
func TestIntegration_DaemonLifecycle(t *testing.T) {
	const tenantID = "tenant-lifecycle"

	t.Run("start_and_enroll", func(t *testing.T) {
		mc := newMockCentral(t, tenantID)

		d, enroller, _, spawn := newTestDaemon(t, testDaemonConfig{
			centralURL: mc.URL(),
			tenantID:   tenantID,
		})

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		require.NoError(t, d.Start(ctx))
		assert.Equal(t, "running", d.State(), "daemon must be running after Start")
		assert.Equal(t, 1, enroller.enrollCalls(), "Enroll must be called exactly once")
		assert.NotNil(t, d.Identity(), "Identity must be populated after enrollment")
		assert.True(t, spawn.started.Load(), "SpawnServer must be started")

		require.NoError(t, d.Stop(ctx))
		assert.Equal(t, "stopped", d.State())
		assert.True(t, spawn.stopped.Load(), "SpawnServer must be stopped")
	})

	t.Run("spawn_prompt_close", func(t *testing.T) {
		mc := newMockCentral(t, tenantID)

		d, _, _, _ := newTestDaemon(t, testDaemonConfig{
			centralURL: mc.URL(),
			tenantID:   tenantID,
		})

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		require.NoError(t, d.Start(ctx))
		t.Cleanup(func() { _ = d.Stop(context.Background()) })

		// Spawn a session via the runtime directly (production path goes
		// through the gRPC spawn server — Phase 4). Here we exercise the
		// runtime layer that the spawn server delegates to.
		session, err := d.Runtime().Spawn(ctx, tenantID, nil, "acp", &orchestration.SpawnRequest{
			WorkDir:     t.TempDir(),
			SandboxMode: orchestration.SandboxModeNone,
		})
		require.NoError(t, err, "Spawn must succeed")
		require.NotEmpty(t, session.ID, "Session must have an ID")

		// Prompt the session.
		resp, err := d.Runtime().Prompt(ctx, "acp", &orchestration.PromptRequest{
			SessionID: session.ID,
			Prompt:    "respond with: hello",
		})
		require.NoError(t, err, "Prompt must succeed")
		require.NotEmpty(t, resp.Content, "Prompt must return content")

		// Close the session.
		require.NoError(t, d.Runtime().Close(ctx, "acp", session.ID))
	})

	t.Run("stop_then_start_resumes", func(t *testing.T) {
		// TODO(phase3): Once enrollment.go persists credentials to WorkDir,
		// this sub-test should rebuild the daemon from the same WorkDir and
		// assert that Enroll is NOT called again (credentials loaded from disk).
		// For now the stub enroller accepts multiple calls, and we verify that
		// a second Start on a new daemon instance succeeds.
		mc := newMockCentral(t, tenantID)

		workDir := t.TempDir()

		buildDaemon := func() *fleet.Daemon {
			enroller := &stubEnroller{
				identity: &orchestration.AgentIdentity{
					AgentID:          "test-agent-id",
					ServiceAccountID: "test-sa-id",
					TenantID:         tenantID,
					Role:             "worker",
					Scopes:           []string{"orchestrator:*"},
					ClientID:         "test-client-id",
				},
			}
			runtime, err := fleet.NewRuntime(fleet.RuntimeConfig{
				Backends: map[string]orchestration.AgentBackend{
					"acp": &stubAgentBackend{backendType: "acp"},
				},
				ContainerRuntime: &stubContainerRuntime{available: false},
				SandboxPolicy:    orchestration.SandboxPolicyOptIn,
				Logger:           &testLogger{t: t},
			})
			require.NoError(t, err)

			d, err := fleet.NewDaemon(withAgentIdentity(t, fleet.Config{
				ServerAddr: mc.URL(),
				TenantID:   tenantID,
				BindAddr:   "127.0.0.1:0",
				WorkDir:    workDir,
			}), fleet.DaemonDeps{
				Runtime:     runtime,
				Enroller:    enroller,
				SpawnServer: newStubSpawnServer("127.0.0.1:0"),
				Logger:      &testLogger{t: t},
			})
			require.NoError(t, err)
			return d
		}

		ctx := context.Background()

		d1 := buildDaemon()
		require.NoError(t, d1.Start(ctx))
		assert.Equal(t, "running", d1.State())
		require.NoError(t, d1.Stop(ctx))
		assert.Equal(t, "stopped", d1.State())

		// Restart with a fresh daemon instance (same WorkDir).
		d2 := buildDaemon()
		require.NoError(t, d2.Start(ctx))
		assert.Equal(t, "running", d2.State(),
			"daemon must reach running state on restart")
		require.NoError(t, d2.Stop(ctx))
	})

	t.Run("enrollment_error_transitions_to_failed", func(t *testing.T) {
		mc := newMockCentral(t, tenantID)

		errEnroll := &orchestration.InputValidationError{
			Field:   "node_cert_file",
			Message: "certificate is not bound to this tenant",
		}

		d, _, _, _ := newTestDaemon(t, testDaemonConfig{
			centralURL: mc.URL(),
			tenantID:   tenantID,
			enrollErr:  errEnroll,
		})

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		err := d.Start(ctx)
		require.Error(t, err, "Start must propagate enrollment error")
		assert.Equal(t, "failed", d.State(), "daemon must reach failed state on enrollment error")
		assert.NotNil(t, d.RunError(), "RunError must be set after failed enrollment")
	})

	t.Run("start_idempotent", func(t *testing.T) {
		// Second Start call must be a no-op (startOnce guard).
		mc := newMockCentral(t, tenantID)

		d, enroller, _, _ := newTestDaemon(t, testDaemonConfig{
			centralURL: mc.URL(),
			tenantID:   tenantID,
		})

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		require.NoError(t, d.Start(ctx))
		require.NoError(t, d.Start(ctx), "second Start must not error")
		assert.Equal(t, 1, enroller.enrollCalls(), "Enroll must be called exactly once despite two Start calls")

		require.NoError(t, d.Stop(ctx))
	})

	t.Run("stop_idempotent", func(t *testing.T) {
		mc := newMockCentral(t, tenantID)

		d, _, _, _ := newTestDaemon(t, testDaemonConfig{
			centralURL: mc.URL(),
			tenantID:   tenantID,
		})

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		require.NoError(t, d.Start(ctx))
		require.NoError(t, d.Stop(ctx))
		require.NoError(t, d.Stop(ctx), "second Stop must not error")
		assert.Equal(t, "stopped", d.State())
	})

	t.Run("heartbeat_delivered", func(t *testing.T) {
		mc := newMockCentral(t, tenantID)

		enroller := &stubEnroller{
			identity: &orchestration.AgentIdentity{
				AgentID:          "test-agent-hb",
				ServiceAccountID: "sa-hb",
				TenantID:         tenantID,
				Role:             "worker",
				Scopes:           []string{"orchestrator:*"},
				ClientID:         "client-hb",
			},
		}
		hb := newStubHeartbeat(8)
		spawn := newStubSpawnServer("127.0.0.1:0")

		runtime, err := fleet.NewRuntime(fleet.RuntimeConfig{
			Backends: map[string]orchestration.AgentBackend{
				"acp": &stubAgentBackend{backendType: "acp"},
			},
			ContainerRuntime: &stubContainerRuntime{available: false},
			SandboxPolicy:    orchestration.SandboxPolicyOptIn,
			Logger:           &testLogger{t: t},
		})
		require.NoError(t, err)

		// Use a very short heartbeat interval so the test loop fires quickly.
		d, err := fleet.NewDaemon(withAgentIdentity(t, fleet.Config{
			ServerAddr:        mc.URL(),
			TenantID:          tenantID,
			BindAddr:          "127.0.0.1:0",
			WorkDir:           t.TempDir(),
			HeartbeatInterval: 50 * time.Millisecond,
		}), fleet.DaemonDeps{
			Runtime:     runtime,
			Enroller:    enroller,
			Heartbeat:   hb,
			SpawnServer: spawn,
			Logger:      &testLogger{t: t},
		})
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		require.NoError(t, d.Start(ctx))

		// Wait for at least one heartbeat via channel (no time.Sleep).
		select {
		case beat := <-hb.beatsCh:
			assert.Equal(t, orchestration.SandboxPolicyOptIn, beat.SandboxPolicy,
				"heartbeat must carry the daemon's sandbox policy")
			assert.Greater(t, beat.SequenceNumber, uint64(0))
		case <-ctx.Done():
			t.Fatal("timed out waiting for first heartbeat")
		}

		require.NoError(t, d.Stop(context.Background()))
	})
}
