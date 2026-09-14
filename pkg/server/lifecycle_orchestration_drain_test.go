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
	"errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// newDrainTestLogger builds the quiet logger the drain tests run under.
func newDrainTestLogger(t *testing.T) *logger.Logger {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	if err != nil {
		t.Fatalf("logger.New: %v", err)
	}
	return log
}

// newDrainTestServer builds a server whose facade carries a real
// orchestration service, which is the only part of the stack the drain
// touches.
func newDrainTestServer(t *testing.T) (*Server, orchestration.AgentService) {
	t.Helper()
	srv := &Server{logger: newDrainTestLogger(t)}
	agentSvc, err := orchestration.NewAgentService(
		orchestration.NewInMemoryAgentStore(), nil, logger.NewAdapter(srv.logger))
	if err != nil {
		t.Fatalf("NewAgentService: %v", err)
	}
	mock := testutil.NewMockQuicBaaS()
	mock.AgentServiceOverride = agentSvc
	srv.quicBaaSService = mock
	return srv, agentSvc
}

// TestDrainOrchestrationJobs_ReachesTheOrchestrationService proves the
// shutdown step is wired to something: after it runs, the service has begun
// draining and refuses to start work the daemon has no time left to do.
func TestDrainOrchestrationJobs_ReachesTheOrchestrationService(t *testing.T) {
	t.Parallel()
	srv, agentSvc := newDrainTestServer(t)

	srv.drainOrchestrationJobs()

	ctx := orchestration.ContextWithTenant(context.Background(), "acme")
	_, err := agentSvc.StartJob(ctx, &orchestration.StartJobRequest{
		TenantID:    "acme",
		Description: "started after the drain",
	})
	var shuttingDown *orchestration.ServiceShuttingDownError
	if !errors.As(err, &shuttingDown) {
		t.Fatalf("StartJob after the drain = %v, want a ServiceShuttingDownError", err)
	}
}

// TestDrainOrchestrationJobs_WithoutAnOrchestrationServiceIsANoOp proves the
// step is safe on the shutdown paths that have no orchestration at all — a
// facade that was never built, and a facade with no agent service — because
// Shutdown runs on every one of them.
func TestDrainOrchestrationJobs_WithoutAnOrchestrationServiceIsANoOp(t *testing.T) {
	t.Parallel()
	srv := &Server{logger: newDrainTestLogger(t)}

	srv.quicBaaSService = nil
	srv.drainOrchestrationJobs()

	srv.quicBaaSService = testutil.NewMockQuicBaaS()
	srv.drainOrchestrationJobs()
}
