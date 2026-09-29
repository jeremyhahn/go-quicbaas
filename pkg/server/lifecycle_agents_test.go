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
	"slices"
	"testing"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

func agentsTestServer(t *testing.T, cfg *config.Config) *Server {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	if err != nil {
		t.Fatalf("logger: %v", err)
	}
	if cfg == nil {
		cfg = &config.Config{}
	}
	return &Server{logger: log, config: cfg}
}

func TestConfigAgentReferences_ReportsTheLeadAndTheTriageDefault(t *testing.T) {
	ctx := context.Background()
	cfg := &config.Config{}
	cfg.Orchestration.LeadAgent.AgentID = " claude-lead "
	cfg.SDLC.TriageAgent.PoolID = "triagers"
	reporter := configAgentReferences(cfg)

	system, err := reporter.AgentReferences(ctx, orchestration.SystemTenantID)
	if err != nil {
		t.Fatal(err)
	}
	if len(system) != 2 || system[0].Ref.AgentID != "claude-lead" || system[0].Usage.Slot != orchestration.AgentUsageSlotLead {
		t.Fatalf("system references = %+v", system)
	}
	tenant, err := reporter.AgentReferences(ctx, "acme")
	if err != nil {
		t.Fatal(err)
	}
	if len(tenant) != 1 || tenant[0].Ref.PoolID != "triagers" || tenant[0].Usage.ID != "sdlc.triage_agent" {
		t.Fatalf("the triage default is named in every tenant, the lead only in the system tenant: %+v", tenant)
	}

	if refs, err := configAgentReferences(nil).AgentReferences(ctx, "acme"); err != nil || len(refs) != 0 {
		t.Fatalf("no configuration references nothing: %v, %v", refs, err)
	}
	if refs, err := configAgentReferences(&config.Config{}).AgentReferences(ctx, orchestration.SystemTenantID); err != nil || len(refs) != 0 {
		t.Fatalf("an empty configuration references nothing: %v, %v", refs, err)
	}
}

// failingReporter refuses every query.
type failingReporter struct{}

func (failingReporter) AgentReferences(context.Context, string) ([]orchestration.AgentReference, error) {
	return nil, errors.New("projects unreadable")
}

func TestAgentUsageReporters_StopsAtTheFirstFailure(t *testing.T) {
	reporters := agentUsageReporters{orchestration.StaticAgentUsageReporter{AllTenants: true}, failingReporter{}}
	if _, err := reporters.AgentReferences(context.Background(), "acme"); err == nil {
		t.Fatal("a reporter that cannot answer must fail the query, not under-report users")
	}
}

func TestAgentMigrationTenants_WithoutIAMVisitsTheSystemTenant(t *testing.T) {
	s := agentsTestServer(t, nil)
	if got := s.agentMigrationTenants(context.Background()); !slices.Equal(got, []string{orchestration.SystemTenantID}) {
		t.Fatalf("tenants = %v", got)
	}
}

// unsealingTenantClient is a qrdb tenant client whose barriers unseal.
type unsealingTenantClient struct {
	qrdbtransport.TenantClient
}

func (unsealingTenantClient) TenantBarrierUnseal(context.Context, *qrdbtransport.TenantBarrierUnsealRequest) error {
	return nil
}

func tenantAdminCtx(tenantID string) context.Context {
	return auth.SubjectIntoContext(context.Background(), interfaces.Subject{
		ID: "admin", TenantID: tenantID, Authenticated: true,
	})
}

// TestMigrateProjectLeads_ReachesATenantSealedAtBoot: the boot pass cannot
// see a tenant it does not list (as it cannot read one that is sealed), and
// the unseal of that tenant's barrier runs the migration for it.
func TestMigrateProjectLeads_ReachesATenantSealedAtBoot(t *testing.T) {
	s := agentsTestServer(t, nil)
	kv := kvstore.NewMemory()
	ctx := context.Background()
	key := "orch/project/acme/p1"
	legacy := `{"id":"p1","tenant_id":"acme","name":"one","default_lead_agent":"agent-7"}`
	if err := kv.Put(ctx, key, []byte(legacy)); err != nil {
		t.Fatal(err)
	}

	s.migrateProjectLeads(ctx, kv)
	if raw, err := kv.Get(ctx, key); err != nil || string(raw) != legacy {
		t.Fatalf("a tenant the boot pass does not reach is untouched, got %s, %v", raw, err)
	}

	barrier := service.NewTenantBarrierService(unsealingTenantClient{}, service.WithTenantUnsealHooks(&s.tenantUnsealHooks))
	if err := barrier.TenantBarrierUnseal(tenantAdminCtx("acme"), "acme", []byte("key")); err != nil {
		t.Fatalf("unseal: %v", err)
	}
	if _, err := orchestration.NewProjectStore(kv).Get(orchestration.ContextWithTenant(ctx, "acme"), "acme", "p1"); err != nil {
		t.Fatalf("after the unseal the project decodes: %v", err)
	}
}

// memoryTenantResolver gives each tenant its own in-memory store.
type memoryTenantResolver struct {
	stores map[string]qrdbsdk.KVStore
	err    error
}

func (r *memoryTenantResolver) Resolve(_ context.Context, tenantID string) (qrdbsdk.KVStore, error) {
	if r.err != nil {
		return nil, r.err
	}
	store, ok := r.stores[tenantID]
	if !ok {
		store = qrdbsdk.NewMemoryKVStore()
		r.stores[tenantID] = store
	}
	return store, nil
}

func TestMigrateBindingAgents_RegistersForTenantsUnsealedLater(t *testing.T) {
	s := agentsTestServer(t, nil)
	ctx := context.Background()
	resolver := &memoryTenantResolver{stores: map[string]qrdbsdk.KVStore{}}
	store, err := sdlc.NewStore(resolver)
	if err != nil {
		t.Fatal(err)
	}

	s.migrateBindingAgents(ctx, store) // no catalog: nothing to migrate into, nothing registered
	s.orchAgentDirectory, err = orchestration.NewAgentDirectory(orchestration.AgentDirectoryConfig{
		Store: orchestration.NewInMemoryAgentCatalogStore(), Models: noAgentModels{},
	})
	if err != nil {
		t.Fatal(err)
	}
	s.migrateBindingAgents(ctx, store)

	// An unseal of a tenant with no bindings seeds nothing, and a tenant
	// whose store cannot be read is reported rather than fatal.
	barrier := service.NewTenantBarrierService(unsealingTenantClient{}, service.WithTenantUnsealHooks(&s.tenantUnsealHooks))
	if err := barrier.TenantBarrierUnseal(tenantAdminCtx("acme"), "acme", []byte("key")); err != nil {
		t.Fatal(err)
	}
	resolver.err = errors.New("tenant sealed")
	s.migrateTenantBindingAgents(ctx, store, "acme")
	agents, err := s.orchAgentDirectory.Store().ListAgents(orchestration.ContextWithTenant(ctx, "acme"), "acme")
	if err != nil || len(agents) != 0 {
		t.Fatalf("agents = %v, %v", agents, err)
	}
}

// noAgentModels serves every backend with no curated model list.
type noAgentModels struct{}

func (noAgentModels) ListBackendModels(context.Context, string) ([]string, error) { return nil, nil }

// leadRecorder records what the lead agent was wired with.
type leadRecorder struct {
	lead    *orchestration.LeadAgentConfig
	session string
}

func (r *leadRecorder) SetLeadAgent(cfg *orchestration.LeadAgentConfig) { r.lead = cfg }
func (r *leadRecorder) SetLeadSessionID(id string)                      { r.session = id }

// leadSpawner answers SpawnSession with resp or err, recording the request.
type leadSpawner struct {
	orchestration.AgentService
	req  *orchestration.SpawnSessionRequest
	resp *orchestration.SpawnSessionResponse
	err  error
}

func (s *leadSpawner) SpawnSession(_ context.Context, req *orchestration.SpawnSessionRequest) (*orchestration.SpawnSessionResponse, error) {
	s.req = req
	return s.resp, s.err
}

func TestStartLeadAgent(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.ShutdownTimeout = time.Second
	cfg.Orchestration.LeadAgent.PoolID = "leads"
	s := agentsTestServer(t, cfg)

	spawner := &leadSpawner{resp: &orchestration.SpawnSessionResponse{SessionID: "s1", Backend: "claude", Model: "claude-opus-5-5", ResolvedAgentID: "claude-lead"}}
	recorder := &leadRecorder{}
	s.startLeadAgent(spawner, recorder)
	if spawner.req.PoolID != "leads" || spawner.req.Role != orchestration.AgentRoleLead {
		t.Fatalf("spawned %+v", spawner.req)
	}
	if recorder.session != "s1" || recorder.lead.Agent.PoolID != "leads" || recorder.lead.Model != "claude-opus-5-5" {
		t.Fatalf("wired %+v / %q", recorder.lead, recorder.session)
	}

	// A lead that cannot be spawned leaves the node in manual mode.
	failed := &leadRecorder{}
	s.startLeadAgent(&leadSpawner{err: errors.New("pool exhausted")}, failed)
	if failed.lead != nil {
		t.Fatal("a lead that failed to spawn must not be wired")
	}

	// No lead configured, or both an Agent and a pool: nothing is spawned.
	for _, ref := range []config.AgentRefConfig{{}, {AgentID: "a", PoolID: "p"}} {
		cfg.Orchestration.LeadAgent.AgentRefConfig = ref
		untouched := &leadSpawner{}
		s.startLeadAgent(untouched, &leadRecorder{})
		if untouched.req != nil {
			t.Fatalf("lead %+v was spawned", ref)
		}
	}
}

// catalogLessAgents is an agent service with neither a model catalog nor a
// catalog setter.
type catalogLessAgents struct{ orchestration.AgentService }

func TestStartAgentCatalog_RefusesAnAgentServiceItCannotWire(t *testing.T) {
	s := agentsTestServer(t, nil)
	err := s.startAgentCatalog(context.Background(), catalogLessAgents{}, kvstore.NewMemory(), nil, orchestration.NewInMemoryProjectStore(), nil)
	var wiring *AgentCatalogWiringError
	if !errors.As(err, &wiring) || !errors.Is(err, ErrAgentCatalogWiring) {
		t.Fatalf("err = %v", err)
	}
	if s.orchAgentDirectory != nil {
		t.Fatal("no catalog may be installed")
	}
	if _, err := s.agentCatalogService(); !errors.Is(err, orchestration.ErrAgentCatalogUnavailable) {
		t.Fatalf("the catalog service without a catalog = %v", err)
	}
}
