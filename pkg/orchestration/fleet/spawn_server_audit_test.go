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

package fleet

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	proto "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/security"
)

// recordingAudit captures every event passed to Log. It satisfies the
// pkg/orchestration/fleet.AuditSink contract used by GRPCSpawnServer.
type recordingAudit struct {
	mu     sync.Mutex
	events []security.AuditEvent
}

func (r *recordingAudit) Log(_ context.Context, event *security.AuditEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if event == nil {
		return nil
	}
	// Defensive copy — the handler may mutate its map after the call.
	cp := *event
	if event.Details != nil {
		cp.Details = make(map[string]string, len(event.Details))
		for k, v := range event.Details {
			cp.Details[k] = v
		}
	}
	r.events = append(r.events, cp)
	return nil
}

func (r *recordingAudit) snapshot() []security.AuditEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]security.AuditEvent, len(r.events))
	copy(out, r.events)
	return out
}

// buildAuditServer stands up a spawn server wired to an in-memory audit sink
// plus fresh metric collectors.
func buildAuditServer(t *testing.T, backend orchestration.AgentBackend, identity *orchestration.AgentIdentity) (*recordingAudit, proto.FleetRuntimeClient, func()) {
	t.Helper()

	rt, err := NewRuntime(RuntimeConfig{
		Backends:      map[string]orchestration.AgentBackend{"acp": backend},
		SandboxPolicy: orchestration.SandboxPolicyOptIn,
		Logger:        &testLogger{t: t},
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	audit := &recordingAudit{}
	lis := bufconn.Listen(spawnServerBufSize)
	srv, err := NewGRPCSpawnServer(GRPCSpawnServerDeps{
		Runtime:          rt,
		BindAddr:         "bufnet",
		Logger:           &testLogger{t: t},
		ListenerOverride: lis,
		Audit:            audit,
	})
	if err != nil {
		t.Fatalf("NewGRPCSpawnServer: %v", err)
	}
	if identity != nil {
		srv.SetIdentity(identity)
	}
	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(_ context.Context, _ string) (net.Conn, error) {
			return lis.Dial()
		}),
	)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	client := proto.NewFleetRuntimeClient(conn)
	cleanup := func() {
		_ = conn.Close()
		stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Stop(stopCtx)
	}
	return audit, client, cleanup
}

func findEvent(events []security.AuditEvent, action string) *security.AuditEvent {
	for i := range events {
		if events[i].Action == action {
			return &events[i]
		}
	}
	return nil
}

func TestGRPCSpawnServer_AuditSpawnSuccess(t *testing.T) {
	t.Parallel()

	audit, client, cleanup := buildAuditServer(t, &spawnBackend{}, newTestIdentity())
	defer cleanup()

	resp, err := client.Spawn(context.Background(), &proto.SpawnSessionRequest{
		Backend:  "acp",
		TenantId: "tenant-a",
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	ev := findEvent(audit.snapshot(), "agent.runtime.spawn")
	if ev == nil {
		t.Fatal("expected agent.runtime.spawn audit event, got none")
	}
	if ev.Result != security.ResultSuccess {
		t.Fatalf("Result: got %q want %q", ev.Result, security.ResultSuccess)
	}
	if ev.Details["tenant_id"] != "tenant-a" {
		t.Fatalf("tenant_id detail: got %q", ev.Details["tenant_id"])
	}
	if ev.Details["agent_id"] != "agent-test" {
		t.Fatalf("agent_id detail: got %q", ev.Details["agent_id"])
	}
	if ev.Details["session_id"] != resp.GetSessionId() {
		t.Fatalf("session_id detail: got %q want %q", ev.Details["session_id"], resp.GetSessionId())
	}
}

func TestGRPCSpawnServer_AuditTenantMismatchIsDenied(t *testing.T) {
	t.Parallel()

	audit, client, cleanup := buildAuditServer(t, &spawnBackend{}, newTestIdentity())
	defer cleanup()

	_, err := client.Spawn(context.Background(), &proto.SpawnSessionRequest{
		Backend:  "acp",
		TenantId: "wrong-tenant",
	})
	if err == nil {
		t.Fatal("expected tenant mismatch error")
	}

	ev := findEvent(audit.snapshot(), "agent.runtime.spawn")
	if ev == nil {
		t.Fatal("expected spawn audit event for denied tenant")
	}
	if ev.Result != security.ResultDenied {
		t.Fatalf("Result: got %q want %q", ev.Result, security.ResultDenied)
	}
	if ev.ErrorMsg == "" {
		t.Fatal("expected ErrorMsg to be populated on denial")
	}
}

func TestGRPCSpawnServer_AuditMissingScopeIsDenied(t *testing.T) {
	t.Parallel()

	id := newTestIdentity()
	id.Scopes = []string{orchestration.AgentScopeRuntimeStatus} // spawn scope removed
	audit, client, cleanup := buildAuditServer(t, &spawnBackend{}, id)
	defer cleanup()

	_, err := client.Spawn(context.Background(), &proto.SpawnSessionRequest{
		Backend:  "acp",
		TenantId: "tenant-a",
	})
	if err == nil {
		t.Fatal("expected missing-scope error")
	}

	ev := findEvent(audit.snapshot(), "agent.runtime.spawn")
	if ev == nil {
		t.Fatal("expected spawn audit event for missing scope")
	}
	if ev.Result != security.ResultDenied {
		t.Fatalf("Result: got %q want %q", ev.Result, security.ResultDenied)
	}
}

func TestGRPCSpawnServer_AuditStatusAndClose(t *testing.T) {
	t.Parallel()

	audit, client, cleanup := buildAuditServer(t, &spawnBackend{}, newTestIdentity())
	defer cleanup()

	if _, err := client.Status(context.Background(), &proto.SessionStatusRequest{
		SessionId: "sess-1",
		TenantId:  "tenant-a",
	}); err != nil {
		t.Fatalf("Status: %v", err)
	}
	if _, err := client.Close(context.Background(), &proto.CloseSessionRequest{
		SessionId: "sess-1",
		TenantId:  "tenant-a",
	}); err != nil {
		t.Fatalf("Close: %v", err)
	}

	events := audit.snapshot()
	if ev := findEvent(events, "agent.runtime.status"); ev == nil || ev.Result != security.ResultSuccess {
		t.Fatalf("status event: %+v", ev)
	}
	if ev := findEvent(events, "agent.runtime.close"); ev == nil || ev.Result != security.ResultSuccess {
		t.Fatalf("close event: %+v", ev)
	}
}

func TestGRPCSpawnServer_NilAuditSinkIsNoOp(t *testing.T) {
	t.Parallel()

	rt, err := NewRuntime(RuntimeConfig{
		Backends:      map[string]orchestration.AgentBackend{"acp": &spawnBackend{}},
		SandboxPolicy: orchestration.SandboxPolicyOptIn,
		Logger:        &testLogger{t: t},
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	lis := bufconn.Listen(spawnServerBufSize)
	srv, err := NewGRPCSpawnServer(GRPCSpawnServerDeps{
		Runtime:          rt,
		BindAddr:         "bufnet",
		Logger:           &testLogger{t: t},
		ListenerOverride: lis,
	})
	if err != nil {
		t.Fatalf("NewGRPCSpawnServer: %v", err)
	}
	srv.SetIdentity(newTestIdentity())
	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = srv.Stop(stopCtx)
	}()

	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(_ context.Context, _ string) (net.Conn, error) {
			return lis.Dial()
		}),
	)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	client := proto.NewFleetRuntimeClient(conn)
	if _, err := client.Spawn(context.Background(), &proto.SpawnSessionRequest{
		Backend:  "acp",
		TenantId: "tenant-a",
	}); err != nil {
		t.Fatalf("Spawn with nil audit: %v", err)
	}
	// Metrics still record even without an audit sink.
	if got := readHistogramCount(t, RPCDuration); got == 0 {
		t.Fatal("expected RPCDuration observation even without audit sink")
	}
}

func TestGRPCSpawnServer_RPCDurationObservesEveryRPC(t *testing.T) {
	t.Parallel()
	_, client, cleanup := buildAuditServer(t, &spawnBackend{}, newTestIdentity())
	defer cleanup()

	before := readHistogramCount(t, RPCDuration)
	if _, err := client.Spawn(context.Background(), &proto.SpawnSessionRequest{
		Backend:  "acp",
		TenantId: "tenant-a",
	}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	after := readHistogramCount(t, RPCDuration)
	if after <= before {
		t.Fatalf("RPCDuration not observed: before=%d after=%d", before, after)
	}
}
