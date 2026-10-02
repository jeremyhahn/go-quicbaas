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
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// =============================================================================
// The tenant AI agent catalog
// =============================================================================

// ErrAgentCatalogWiring indicates the agent catalog could not be wired.
var ErrAgentCatalogWiring = errors.New("server: agent catalog cannot be wired")

// AgentCatalogWiringError names what the agent catalog lacked.
type AgentCatalogWiringError struct {
	Component string
	Cause     error
}

// Error implements the error interface.
func (e *AgentCatalogWiringError) Error() string {
	msg := ErrAgentCatalogWiring.Error() + ": " + e.Component
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap returns the sentinel and the cause.
func (e *AgentCatalogWiringError) Unwrap() []error { return []error{ErrAgentCatalogWiring, e.Cause} }

// agentDirectoryWirer is the agent service's catalog setter.
type agentDirectoryWirer interface {
	SetAgentDirectory(directory *orchestration.AgentDirectory)
}

// startAgentCatalog builds the tenant agent catalog, installs it on the agent
// service (where a spawn naming an Agent or pool resolves) and on the project
// service (where a default lead is checked), and migrates the project records
// that still carry a legacy default lead.
//
// IT RUNS BEFORE ANY PROJECT IS READ. A legacy project record's
// default_lead_agent is a string, which no longer decodes as a Project, so the
// rewrite has to land before recovery, the sweeps or a request reads one.
func (s *Server) startAgentCatalog(
	ctx context.Context, agentSvc orchestration.AgentService, kv interfaces.KVStore,
	auditor orchestration.StoreAuditor, projects orchestration.ProjectStore, storeOpts []orchestration.StoreOption,
) error {
	models, ok := agentSvc.(orchestration.AgentModelCatalog)
	if !ok {
		return &AgentCatalogWiringError{Component: "live model catalog"}
	}
	wirer, ok := agentSvc.(agentDirectoryWirer)
	if !ok {
		return &AgentCatalogWiringError{Component: "agent service"}
	}
	cfg := orchestration.AgentDirectoryConfig{
		Store:   orchestration.NewAgentCatalogStore(kv, storeOpts...),
		Models:  models,
		Auditor: auditor,
	}
	if s.orchAccountStore != nil {
		cfg.Accounts = s.orchAccountStore
	}
	if load, loadOK := s.orchJobLeaseStore.(orchestration.ProfileLoadReader); loadOK {
		cfg.Load = load
	}
	directory, err := orchestration.NewAgentDirectory(cfg)
	if err != nil {
		return &AgentCatalogWiringError{Component: "directory", Cause: err}
	}
	directory.RegisterUsageReporter(orchestration.NewProjectAgentUsageReporter(projects))
	directory.RegisterUsageReporter(configAgentReferences(s.config))
	wirer.SetAgentDirectory(directory)
	if s.quicBaaSService != nil {
		if checker, checkerOK := s.quicBaaSService.Project().(interface {
			SetAgentRefChecker(service.AgentRefChecker)
		}); checkerOK {
			checker.SetAgentRefChecker(directory)
		}
	}
	s.orchAgentDirectory = directory
	s.migrateProjectLeads(ctx, kv)
	return nil
}

// configAgentReferences reports the node configuration's references into the
// catalog: the lead agent (a system-tenant Agent) and the default triage
// agent (named in every tenant that defines it).
func configAgentReferences(cfg *config.Config) orchestration.AgentUsageReporter {
	var references []orchestration.AgentReference
	if cfg == nil {
		return orchestration.StaticAgentUsageReporter{}
	}
	lead := agentRefFromConfig(cfg.Orchestration.LeadAgent.AgentRefConfig)
	if !lead.IsZero() {
		references = append(references, orchestration.AgentReference{Ref: lead, Usage: orchestration.AgentUsage{
			Kind: orchestration.AgentUsageKindConfig, ID: "orchestration.lead_agent", Slot: orchestration.AgentUsageSlotLead,
		}})
	}
	reporter := orchestration.StaticAgentUsageReporter{TenantID: orchestration.SystemTenantID, References: references}
	triage := agentRefFromConfig(cfg.SDLC.TriageAgent)
	if triage.IsZero() {
		return reporter
	}
	return agentUsageReporters{reporter, orchestration.StaticAgentUsageReporter{
		AllTenants: true,
		References: []orchestration.AgentReference{{Ref: triage, Usage: orchestration.AgentUsage{
			Kind: orchestration.AgentUsageKindConfig, ID: "sdlc.triage_agent", Slot: orchestration.AgentUsageSlotTriage,
		}}},
	}}
}

// agentUsageReporters reports every member's references.
type agentUsageReporters []orchestration.AgentUsageReporter

// AgentReferences implements orchestration.AgentUsageReporter.
func (r agentUsageReporters) AgentReferences(ctx context.Context, tenantID string) ([]orchestration.AgentReference, error) {
	var all []orchestration.AgentReference
	for _, reporter := range r {
		references, err := reporter.AgentReferences(ctx, tenantID)
		if err != nil {
			return nil, err
		}
		all = append(all, references...)
	}
	return all, nil
}

// agentRefFromConfig converts a configured reference.
func agentRefFromConfig(ref config.AgentRefConfig) orchestration.AgentRef {
	return orchestration.AgentRef{AgentID: strings.TrimSpace(ref.AgentID), PoolID: strings.TrimSpace(ref.PoolID)}
}

// agentMigrationTenants lists every tenant a boot migration visits: the
// system tenant, and every tenant the IAM tenant manager knows.
func (s *Server) agentMigrationTenants(ctx context.Context) []string {
	tenants := []string{orchestration.SystemTenantID}
	if s.iamComponents == nil || s.iamComponents.GetTenantManager() == nil {
		return tenants
	}
	listed, err := (&iamTenantLister{tenants: s.iamComponents.GetTenantManager()}).ListTenantIDs(ctx)
	if err != nil {
		s.logger.Warnf("agent migration: tenants could not be listed, migrating the system tenant only: %v", err)
		return tenants
	}
	for _, tenantID := range listed {
		if !slices.Contains(tenants, tenantID) {
			tenants = append(tenants, tenantID)
		}
	}
	return tenants
}

// migrationContext scopes ctx to tenantID under both tenant keys, which is
// what the tenant-routed stores address.
func migrationContext(ctx context.Context, tenantID string) context.Context {
	return orchestration.ContextWithTenant(tenantctx.WithTenant(ctx, tenantID), tenantID)
}

// tenantUnsealFunc adapts a function to service.TenantUnsealObserver.
type tenantUnsealFunc func(ctx context.Context, tenantID string)

// TenantUnsealed implements service.TenantUnsealObserver.
func (f tenantUnsealFunc) TenantUnsealed(ctx context.Context, tenantID string) { f(ctx, tenantID) }

// migrateProjectLeads drops every legacy default lead in every tenant it can
// read now, and again in each tenant whose barrier is unsealed later: a
// tenant sealed at boot is unreadable until then.
func (s *Server) migrateProjectLeads(ctx context.Context, kv interfaces.KVStore) {
	for _, tenantID := range s.agentMigrationTenants(ctx) {
		s.migrateTenantProjectLeads(ctx, kv, tenantID)
	}
	s.tenantUnsealHooks.Register(tenantUnsealFunc(func(ctx context.Context, tenantID string) {
		s.migrateTenantProjectLeads(ctx, kv, tenantID)
	}))
}

// migrateTenantProjectLeads drops one tenant's legacy default leads, naming
// each project so an operator can point it at an Agent.
func (s *Server) migrateTenantProjectLeads(ctx context.Context, kv interfaces.KVStore, tenantID string) {
	dropped, err := orchestration.MigrateProjectLeads(migrationContext(ctx, tenantID), kv, tenantID)
	if err != nil {
		s.logger.Errorf("agent migration: tenant %s: project default leads: %v", tenantID, err)
		return
	}
	for _, project := range dropped {
		s.logger.Warnf("agent migration: tenant %s: project %s had a free-string default lead, which names no Agent; "+
			"it was cleared — set default_lead_agent to an Agent or pool", tenantID, project)
	}
}

// migrateBindingAgents turns every binding's agent tuples into Agents and
// references in every tenant it can read now, and again in each tenant whose
// barrier is unsealed later.
func (s *Server) migrateBindingAgents(ctx context.Context, store *sdlc.Store) {
	if s.orchAgentDirectory == nil {
		return
	}
	for _, tenantID := range s.agentMigrationTenants(ctx) {
		s.migrateTenantBindingAgents(ctx, store, tenantID)
	}
	s.tenantUnsealHooks.Register(tenantUnsealFunc(func(ctx context.Context, tenantID string) {
		s.migrateTenantBindingAgents(ctx, store, tenantID)
	}))
}

// migrateTenantBindingAgents migrates one tenant's bindings, and seeds a
// Claude lead when it migrated any so the tenant's jobs have a lead Agent to
// name.
func (s *Server) migrateTenantBindingAgents(ctx context.Context, store *sdlc.Store, tenantID string) {
	tenantCtx := migrationContext(ctx, tenantID)
	seeder, err := orchestration.NewAgentSeeder(tenantCtx, s.orchAgentDirectory.Store(), tenantID)
	if err != nil {
		s.logger.Errorf("agent migration: tenant %s: catalog unreadable: %v", tenantID, err)
		return
	}
	report, err := sdlc.MigrateBindingAgents(tenantCtx, store, seeder, tenantID)
	if err != nil {
		s.logger.Errorf("agent migration: tenant %s: bindings unreadable: %v", tenantID, err)
		return
	}
	for project, cause := range report.Failed {
		s.logger.Errorf("agent migration: tenant %s: binding %s left for an operator to rebind: %v", tenantID, project, cause)
	}
	if len(report.Migrated) == 0 {
		return
	}
	if _, err := seeder.Seed(tenantCtx, orchestration.AgentTuple{Backend: "claude"}, orchestration.AgentRoleLead); err != nil {
		s.logger.Errorf("agent migration: tenant %s: seeding the Claude lead: %v", tenantID, err)
	}
	s.logger.Infof("agent migration: tenant %s: %d binding(s) now reference Agents %v; agents created %v",
		tenantID, len(report.Migrated), report.Migrated, seeder.Created)
}

// agentCatalogService is the service over the catalog, for the SDLC module's
// worker image refresh.
func (s *Server) agentCatalogService() (*service.AgentCatalogService, error) {
	return service.NewAgentCatalogService(s.orchAgentDirectory)
}

// leadAgentWirer is the agent service's lead agent setters.
type leadAgentWirer interface {
	SetLeadAgent(config *orchestration.LeadAgentConfig)
	SetLeadSessionID(id string)
}

// startLeadAgent spawns the configured lead agent — a system-tenant Agent or
// pool — and records what it resolved to. A lead that cannot be spawned is
// reported and the node runs in manual mode, as it always has.
func (s *Server) startLeadAgent(agentSvc orchestration.AgentService, wirer leadAgentWirer) {
	ref := agentRefFromConfig(s.config.Orchestration.LeadAgent.AgentRefConfig)
	if ref.IsZero() {
		return
	}
	if err := ref.Validate("orchestration.lead_agent"); err != nil {
		s.logger.Errorf("Lead agent not started: %v", err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), s.config.Server.ShutdownTimeout)
	defer cancel()
	resp, err := agentSvc.SpawnSession(ctx, &orchestration.SpawnSessionRequest{
		AgentID: ref.AgentID, PoolID: ref.PoolID, Role: orchestration.AgentRoleLead,
	})
	if err != nil {
		s.logger.Warnf("Failed to spawn lead agent session (%s): %v", ref.String(), err)
		return
	}
	wirer.SetLeadAgent(&orchestration.LeadAgentConfig{Agent: ref, Backend: resp.Backend, Model: resp.Model})
	wirer.SetLeadSessionID(resp.SessionID)
	s.logger.Infof("Lead agent session spawned: session=%s agent=%s backend=%s model=%s",
		resp.SessionID, resp.ResolvedAgentID, resp.Backend, resp.Model)
}
