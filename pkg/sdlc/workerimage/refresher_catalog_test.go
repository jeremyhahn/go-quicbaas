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

package workerimage_test

import (
	"context"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc"
	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc/workerimage"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// These tests drive the refresher against the real agent catalog service,
// which refuses any call whose context binds no tenant. A catalog double
// cannot show that the refresher binds each tenant it acts for (#1310).

type noModels struct{}

func (noModels) ListBackendModels(context.Context, string) ([]string, error) { return nil, nil }

func newCatalog(t *testing.T) *service.AgentCatalogService {
	t.Helper()
	dir, err := orchestration.NewAgentDirectory(orchestration.AgentDirectoryConfig{
		Store: orchestration.NewInMemoryAgentCatalogStore(), Models: noModels{},
	})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := service.NewAgentCatalogService(dir)
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}

// tenantCtx binds a tenant the way the server's request path does, for the
// test's own setup and assertions.
func tenantCtx(tenantID string) context.Context {
	return orchestration.ContextWithTenant(tenantctx.WithTenant(context.Background(), tenantID), tenantID)
}

func createStaleCoder(t *testing.T, catalog *service.AgentCatalogService, tenantID string) {
	t.Helper()
	agent := &orchestration.Agent{ID: "coder", Name: "coder", Backend: "claude", Model: "claude-opus-5-5",
		Roles: []orchestration.AgentRole{orchestration.AgentRoleCoding}, Image: workerimage.StaleImage}
	if _, err := catalog.CreateAgent(tenantCtx(tenantID), tenantID, agent); err != nil {
		t.Fatalf("create %s/coder: %v", tenantID, err)
	}
}

func imageOf(t *testing.T, catalog *service.AgentCatalogService, tenantID string) string {
	t.Helper()
	agent, err := catalog.GetAgent(tenantCtx(tenantID), tenantID, "coder")
	if err != nil {
		t.Fatalf("get %s/coder: %v", tenantID, err)
	}
	return agent.Image
}

func coderBinding() []*sdlc.ProjectBinding {
	return []*sdlc.ProjectBinding{{ProjectID: "p1", Worker: orchestration.AgentRefTo("coder")}}
}

func TestRefresh_RepointsOneTenantsAgentsThroughTheRealCatalog(t *testing.T) {
	catalog := newCatalog(t)
	createStaleCoder(t, catalog, "acme")
	// globex has an agent of the same ID on the same stale image, but no
	// binding runs it: a repoint that leaked across tenants would move it.
	createStaleCoder(t, catalog, "globex")
	r := workerimage.NewCatalogRefresher(t, []string{"acme", "globex"},
		map[string][]*sdlc.ProjectBinding{"acme": coderBinding()}, catalog)

	report := r.Refresh(t.Context(), workerimage.TriggerManual)

	if report.Outcome != workerimage.OutcomePromoted || len(report.Skipped) != 0 {
		t.Fatalf("outcome=%s error=%q skipped=%v, want promoted with no tenant skipped",
			report.Outcome, report.Error, report.Skipped)
	}
	if got := imageOf(t, catalog, "acme"); got != workerimage.CandidateTag {
		t.Fatalf("acme/coder = %q, want it repointed to %q", got, workerimage.CandidateTag)
	}
	if got := imageOf(t, catalog, "globex"); got != workerimage.StaleImage {
		t.Fatalf("globex/coder = %q, another tenant's agent must not be touched", got)
	}
}

// sealedTenantError is the failure a tenant's catalog read reports.
type sealedTenantError struct{}

func (sealedTenantError) Error() string { return "tenant barrier is sealed" }

// sealedCatalog is the real catalog with one tenant's reads failing.
type sealedCatalog struct {
	*service.AgentCatalogService
	sealed string
}

func (c sealedCatalog) Catalogue(ctx context.Context, tenantID string) (orchestration.AgentCatalogue, error) {
	if tenantID == c.sealed {
		return orchestration.AgentCatalogue{}, sealedTenantError{}
	}
	return c.AgentCatalogService.Catalogue(ctx, tenantID)
}

func TestRefresh_ATenantWhoseCatalogFailsIsSkippedAndThePassCarriesOn(t *testing.T) {
	catalog := newCatalog(t)
	createStaleCoder(t, catalog, "acme")
	createStaleCoder(t, catalog, "sealed")
	// The failing tenant is listed first, so the pass must carry on past it.
	r := workerimage.NewCatalogRefresher(t, []string{"sealed", "acme"},
		map[string][]*sdlc.ProjectBinding{"acme": coderBinding(), "sealed": coderBinding()},
		sealedCatalog{AgentCatalogService: catalog, sealed: "sealed"})

	report := r.Refresh(t.Context(), workerimage.TriggerManual)

	if report.Outcome != workerimage.OutcomePartial || len(report.Skipped) != 1 ||
		!strings.HasPrefix(report.Skipped[0], "sealed: ") {
		t.Fatalf("outcome=%s skipped=%v, want partial with the sealed tenant skipped", report.Outcome, report.Skipped)
	}
	if got := imageOf(t, catalog, "acme"); got != workerimage.CandidateTag {
		t.Fatalf("acme/coder = %q, want it repointed past the sealed tenant", got)
	}
	if got := imageOf(t, catalog, "sealed"); got != workerimage.StaleImage {
		t.Fatalf("sealed/coder = %q, a skipped tenant must not be touched", got)
	}
}
