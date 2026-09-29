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
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// TestOrchestratorContainerOwner_IsDistinctFromTheEmbeddedAgents: the
// orchestrator's backends and the embedded agent's hold different sessions,
// so they must claim containers by different owner labels.
func TestOrchestratorContainerOwner_IsDistinctFromTheEmbeddedAgents(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	owner, err := orchestratorContainerOwner(dataDir)
	if err != nil {
		t.Fatalf("orchestratorContainerOwner: %v", err)
	}
	if owner == dataDir || !strings.HasPrefix(owner, "orchestrator:") {
		t.Fatalf("owner = %q, want a label distinct from the embedded agent's %q", owner, dataDir)
	}
	if !filepath.IsAbs(strings.TrimPrefix(owner, "orchestrator:")) {
		t.Fatalf("owner %q does not name an absolute data dir", owner)
	}
}

// TestStartContainerReaper_RunsUntilShutdown: the reaper starts with the
// orchestrator's container runtime and stops without leaking its goroutine.
func TestStartContainerReaper_RunsUntilShutdown(t *testing.T) {
	t.Parallel()
	srv := &Server{logger: newDrainTestLogger(t)}
	rt, err := orchestration.NewExecContainerRuntimeWithCommand("true", logger.NewAdapter(srv.logger))
	if err != nil {
		t.Fatalf("runtime: %v", err)
	}
	if err := srv.startContainerReaper(rt, "orchestrator:/data", nil); err != nil {
		t.Fatalf("startContainerReaper: %v", err)
	}
	if srv.orchContainerReaper == nil {
		t.Fatal("no reaper loop recorded for shutdown")
	}
	srv.stopContainerReaper()
	if srv.orchContainerReaper != nil {
		t.Fatal("the reaper loop survived shutdown")
	}
	srv.stopContainerReaper() // idempotent
}

// TestStartContainerReaper_RefusesAnUnownedReaper is the error path: an
// unowned reaper could only reap everything.
func TestStartContainerReaper_RefusesAnUnownedReaper(t *testing.T) {
	t.Parallel()
	srv := &Server{logger: newDrainTestLogger(t)}
	rt, err := orchestration.NewExecContainerRuntimeWithCommand("true", logger.NewAdapter(srv.logger))
	if err != nil {
		t.Fatalf("runtime: %v", err)
	}
	if err := srv.startContainerReaper(rt, " ", nil); !errors.Is(err, orchestration.ErrContainerSweep) {
		t.Fatalf("startContainerReaper = %v, want a refusal", err)
	}
	if srv.orchContainerReaper != nil {
		t.Fatal("a refused reaper was recorded")
	}
}
