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

package orchestration

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"sync"
)

// =============================================================================
// AgentDirectory — the tenant's agent catalog as operators and slots see it
// =============================================================================
//
// THE STORE KEEPS THE CATALOG CONSISTENT WITH ITSELF; THE DIRECTORY KEEPS IT
// CONSISTENT WITH EVERYTHING THAT POINTS INTO IT. A binding's worker, a
// project's default lead and the node's lead agent all reference Agents, and
// none of them live in this package. Each owner registers an
// AgentUsageReporter, and the directory asks all of them before an Agent or a
// pool is deleted — so "delete the triage agent" is refused with the list of
// projects still triaging with it, rather than discovered as a failed spawn.
//
// It also holds the one rule the store cannot check: an Agent names a backend
// and a model the LIVE catalog offers. The store validates shape; the
// directory validates reality, at the write an operator makes. A boot
// migration writes through the store directly, because at boot the fleet
// nodes serving the backends may not have registered yet.

// AgentModelCatalog is the live backend and model catalog an Agent is checked
// against when it is saved.
type AgentModelCatalog interface {
	// ListBackendModels returns the models the named backend offers, and an
	// error matching ErrBackendNotFound when no node serves it. An empty list
	// is a backend with no curated catalog, which accepts any model.
	ListBackendModels(ctx context.Context, backend string) ([]string, error)
}

// AgentAccountReader reads a pooled provider account, so an Agent naming
// accounts is checked against accounts that exist.
type AgentAccountReader interface {
	Get(ctx context.Context, tenantID, accountID string) (*Account, error)
}

// AgentReference is one reference a holder keeps into the agent catalog.
type AgentReference struct {
	Ref   AgentRef
	Usage AgentUsage
}

// AgentUsageReporter lists every reference one owner holds into a tenant's
// agent catalog: the SDLC bindings, the project records, the node
// configuration.
type AgentUsageReporter interface {
	AgentReferences(ctx context.Context, tenantID string) ([]AgentReference, error)
}

// AgentDirectoryConfig wires an AgentDirectory.
type AgentDirectoryConfig struct {
	// Store persists the catalog. Required.
	Store AgentCatalogStore

	// Models is the live backend and model catalog. Required.
	Models AgentModelCatalog

	// Accounts reads provider accounts. Optional: without it an Agent that
	// names accounts is refused, because nothing could confirm they exist.
	Accounts AgentAccountReader

	// Load reads live jobs per backend, for MaxConcurrent. Optional: nil makes
	// every Agent look idle.
	Load ProfileLoadReader

	// Auditor records every failover a spawn makes. Optional: nil records
	// nothing, the default for unit tests and embedded use.
	Auditor StoreAuditor
}

// AgentDirectory is the tenant agent catalog's business layer.
type AgentDirectory struct {
	store    AgentCatalogStore
	models   AgentModelCatalog
	accounts AgentAccountReader
	resolver *AgentResolver
	auditor  StoreAuditor

	reportersMu sync.RWMutex
	reporters   []AgentUsageReporter
}

// NewAgentDirectory builds a directory.
func NewAgentDirectory(cfg AgentDirectoryConfig) (*AgentDirectory, error) {
	switch {
	case cfg.Store == nil:
		return nil, &InputValidationError{Field: "store", Message: "the directory persists the catalog through a store"}
	case cfg.Models == nil:
		return nil, &InputValidationError{Field: "models", Message: "an agent is checked against the live backend and model catalog"}
	}
	return &AgentDirectory{
		store:    cfg.Store,
		models:   cfg.Models,
		accounts: cfg.Accounts,
		resolver: NewAgentResolver(cfg.Load),
		auditor:  cfg.Auditor,
	}, nil
}

// Store returns the backing store, for the boot migration that seeds the
// catalog before the fleet has registered its backends.
func (d *AgentDirectory) Store() AgentCatalogStore { return d.store }

// RegisterUsageReporter adds an owner whose references a delete must respect.
func (d *AgentDirectory) RegisterUsageReporter(reporter AgentUsageReporter) {
	if reporter == nil {
		return
	}
	d.reportersMu.Lock()
	defer d.reportersMu.Unlock()
	d.reporters = append(d.reporters, reporter)
}

// =============================================================================
// Agents
// =============================================================================

// CreateAgent validates an Agent against the live catalog and stores it.
func (d *AgentDirectory) CreateAgent(ctx context.Context, tenantID string, agent *Agent) (*Agent, error) {
	if err := d.checkAgent(ctx, tenantID, agent); err != nil {
		return nil, err
	}
	if err := d.store.CreateAgent(ctx, tenantID, agent); err != nil {
		return nil, err
	}
	return agent, nil
}

// UpdateAgent validates a replacement Agent against the live catalog and
// stores it. The Agent's existing references keep their meaning, so a role
// the Agent stops declaring is refused while a slot of that role uses it.
func (d *AgentDirectory) UpdateAgent(ctx context.Context, tenantID string, agent *Agent) (*Agent, error) {
	if err := d.checkAgent(ctx, tenantID, agent); err != nil {
		return nil, err
	}
	if err := d.checkRolesStillServed(ctx, tenantID, agent); err != nil {
		return nil, err
	}
	if err := d.store.UpdateAgent(ctx, tenantID, agent); err != nil {
		return nil, err
	}
	return agent, nil
}

// GetAgent reads one Agent.
func (d *AgentDirectory) GetAgent(ctx context.Context, tenantID, id string) (*Agent, error) {
	return d.store.GetAgent(ctx, tenantID, id)
}

// ListAgents lists the tenant's Agents.
func (d *AgentDirectory) ListAgents(ctx context.Context, tenantID string) ([]*Agent, error) {
	return d.store.ListAgents(ctx, tenantID)
}

// DeleteAgent removes an Agent nothing references.
func (d *AgentDirectory) DeleteAgent(ctx context.Context, tenantID, id string) error {
	if err := d.refuseInUse(ctx, tenantID, AgentRefTo(id)); err != nil {
		return err
	}
	return d.store.DeleteAgent(ctx, tenantID, id)
}

// ReplaceAgentImage points an Agent whose image is one of stale at image
// instead, and reports whether anything changed. It is the worker image
// refresh's promotion: an image is the Agent's, so one write repoints every
// slot that runs it. The live catalog is not consulted — a new image does not
// change which backend or model the Agent names.
func (d *AgentDirectory) ReplaceAgentImage(ctx context.Context, tenantID, agentID string, stale []string, image string) (bool, error) {
	if strings.TrimSpace(image) == "" {
		return false, &InputValidationError{Field: "image", Message: "an agent cannot be repointed to no image"}
	}
	if len(stale) == 0 {
		return false, &InputValidationError{Field: "stale", Message: "name the images being replaced"}
	}
	return d.store.ModifyAgent(ctx, tenantID, agentID, func(agent *Agent) bool {
		if agent.Image == "" || agent.Image == image || !slices.Contains(stale, agent.Image) {
			return false
		}
		agent.Image = image
		return true
	})
}

// checkAgent is the write-time check: shape first, then the live catalog.
func (d *AgentDirectory) checkAgent(ctx context.Context, tenantID string, agent *Agent) error {
	if agent == nil {
		return &InputValidationError{Field: "agent", Message: "must not be nil"}
	}
	if err := agent.Validate(); err != nil {
		return err
	}
	if err := d.checkLiveCatalog(ctx, agent); err != nil {
		return err
	}
	return d.checkAccounts(ctx, tenantID, agent)
}

// checkLiveCatalog refuses a backend no node serves and a model its backend
// does not offer.
func (d *AgentDirectory) checkLiveCatalog(ctx context.Context, agent *Agent) error {
	models, err := d.models.ListBackendModels(ctx, agent.Backend)
	if err != nil {
		if errors.Is(err, ErrBackendNotFound) {
			return &AgentCatalogInvalidError{Field: "agent.backend", Value: agent.Backend, Message: "no node serves this backend"}
		}
		return err
	}
	if len(models) > 0 && !slices.Contains(models, agent.Model) {
		return &AgentCatalogInvalidError{
			Field: "agent.model", Value: agent.Model, Known: models,
			Message: "backend " + agent.Backend + " does not offer this model",
		}
	}
	return nil
}

// checkAccounts refuses an account strategy nobody registers, an account
// that does not exist, and an account the Agent's backend would never draw
// on: one of another provider's, or any account at all on a backend that
// pools none. Either would save cleanly and then leave every spawn of the
// Agent with an empty pool.
func (d *AgentDirectory) checkAccounts(ctx context.Context, tenantID string, agent *Agent) error {
	if strategy := strings.TrimSpace(agent.Account.Strategy); strategy != "" && !agentAccountStrategies[strategy] {
		return &InputValidationError{Field: "agent.account.strategy", Message: "must be " + StaticAccountStrategyName + " or " + LeastUsageAccountStrategyName}
	}
	if len(agent.Account.Accounts) == 0 {
		return nil
	}
	if d.accounts == nil {
		return &InputValidationError{Field: "agent.account.accounts", Message: "this deployment keeps no provider accounts to name"}
	}
	provider, pooled := PooledAccountProvider(agent.Backend)
	if !pooled {
		return &InputValidationError{
			Field:   "agent.account.accounts",
			Message: "backend " + agent.Backend + " draws on no pooled provider accounts",
		}
	}
	for _, accountID := range agent.Account.Accounts {
		account, err := d.accounts.Get(ctx, normalizeStoreTenant(tenantID), accountID)
		if err != nil {
			return err
		}
		if account.Provider != provider {
			return &InputValidationError{
				Field: "agent.account.accounts",
				Message: strconv.Quote(accountID) + " is a " + string(account.Provider) + " account; backend " +
					agent.Backend + " draws on " + string(provider) + " accounts",
			}
		}
	}
	return nil
}

// agentAccountStrategies are the selection strategies an Agent may name: the
// ones every production registry registers.
var agentAccountStrategies = map[string]bool{
	StaticAccountStrategyName:     true,
	LeastUsageAccountStrategyName: true,
}

// checkRolesStillServed refuses an update that drops a role a slot binding
// the Agent needs. Pool members are judged the same way as direct references.
func (d *AgentDirectory) checkRolesStillServed(ctx context.Context, tenantID string, agent *Agent) error {
	references, err := d.references(ctx, tenantID)
	if err != nil {
		return err
	}
	catalogue, err := d.store.Catalogue(ctx, tenantID)
	if err != nil {
		return err
	}
	for _, reference := range references {
		role := slotRole(reference.Usage.Slot)
		if role == "" || agent.HasRole(role) || !refReaches(catalogue, reference.Ref, agent.ID) {
			continue
		}
		return &AgentRoleMismatchError{Field: reference.Usage.String(), AgentID: agent.ID, Role: role}
	}
	return nil
}

// slotRole is the role a usage slot binds, empty for a slot of no role.
func slotRole(slot string) AgentRole {
	name, _, _ := strings.Cut(slot, ":")
	return agentSlotRoles[name]
}

// agentSlotRoles maps the slots owners report to the role each binds.
var agentSlotRoles = map[string]AgentRole{
	AgentUsageSlotWorker:      AgentRoleCoding,
	AgentUsageSlotTriage:      AgentRoleTriage,
	AgentUsageSlotReviewer:    AgentRoleReview,
	AgentUsageSlotLead:        AgentRoleLead,
	AgentUsageSlotDefaultLead: AgentRoleLead,
}

// refReaches reports whether ref resolves to the Agent agentID, directly or as
// a member of the pool it names.
func refReaches(catalogue AgentCatalogue, ref AgentRef, agentID string) bool {
	if !ref.IsPool() {
		return strings.TrimSpace(ref.AgentID) == agentID
	}
	pool, ok := catalogue.Pools[strings.TrimSpace(ref.PoolID)]
	return ok && slices.Contains(pool.Agents, agentID)
}

// =============================================================================
// Pools
// =============================================================================

// CreatePool stores a new pool.
func (d *AgentDirectory) CreatePool(ctx context.Context, tenantID string, pool *AgentPool) (*AgentPool, error) {
	if err := d.store.CreatePool(ctx, tenantID, pool); err != nil {
		return nil, err
	}
	return pool, nil
}

// UpdatePool replaces a pool. Every member must still declare the role of each
// slot the pool is bound to.
func (d *AgentDirectory) UpdatePool(ctx context.Context, tenantID string, pool *AgentPool) (*AgentPool, error) {
	if pool == nil {
		return nil, &InputValidationError{Field: "pool", Message: "must not be nil"}
	}
	if err := d.checkPoolRoles(ctx, tenantID, pool); err != nil {
		return nil, err
	}
	if err := d.store.UpdatePool(ctx, tenantID, pool); err != nil {
		return nil, err
	}
	return pool, nil
}

// checkPoolRoles refuses a pool whose members no longer serve a slot bound to
// it.
func (d *AgentDirectory) checkPoolRoles(ctx context.Context, tenantID string, pool *AgentPool) error {
	references, err := d.references(ctx, tenantID)
	if err != nil {
		return err
	}
	catalogue, err := d.store.Catalogue(ctx, tenantID)
	if err != nil {
		return err
	}
	catalogue.Pools[pool.ID] = *pool
	for _, reference := range references {
		if !reference.Ref.Names(PoolRefTo(pool.ID)) {
			continue
		}
		if err := catalogue.CheckRef(reference.Ref, reference.Usage.String(), slotRole(reference.Usage.Slot)); err != nil {
			return err
		}
	}
	return nil
}

// GetPool reads one pool.
func (d *AgentDirectory) GetPool(ctx context.Context, tenantID, id string) (*AgentPool, error) {
	return d.store.GetPool(ctx, tenantID, id)
}

// ListPools lists the tenant's pools.
func (d *AgentDirectory) ListPools(ctx context.Context, tenantID string) ([]*AgentPool, error) {
	return d.store.ListPools(ctx, tenantID)
}

// DeletePool removes a pool nothing references.
func (d *AgentDirectory) DeletePool(ctx context.Context, tenantID, id string) error {
	if err := d.refuseInUse(ctx, tenantID, PoolRefTo(id)); err != nil {
		return err
	}
	return d.store.DeletePool(ctx, tenantID, id)
}

// =============================================================================
// References
// =============================================================================

// UsedBy lists everything that references ref: the pools listing an Agent,
// and every slot any registered owner binds to it. The Agent or pool must
// exist.
func (d *AgentDirectory) UsedBy(ctx context.Context, tenantID string, ref AgentRef) ([]AgentUsage, error) {
	if err := RequireAgentRef(ref, "ref"); err != nil {
		return nil, err
	}
	catalogue, err := d.store.Catalogue(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	var usages []AgentUsage
	if ref.IsPool() {
		if _, ok := catalogue.Pools[strings.TrimSpace(ref.PoolID)]; !ok {
			return nil, &UnknownAgentPoolError{ID: strings.TrimSpace(ref.PoolID)}
		}
	} else {
		if _, ok := catalogue.Agent(ref.AgentID); !ok {
			return nil, &UnknownAgentError{ID: strings.TrimSpace(ref.AgentID)}
		}
		usages = poolUsages(catalogue.Pools, strings.TrimSpace(ref.AgentID))
	}
	references, err := d.references(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	for _, reference := range references {
		if reference.Ref.Names(ref) {
			usages = append(usages, reference.Usage)
		}
	}
	return usages, nil
}

// refuseInUse returns AgentInUseError when anything references ref.
func (d *AgentDirectory) refuseInUse(ctx context.Context, tenantID string, ref AgentRef) error {
	usages, err := d.UsedBy(ctx, tenantID, ref)
	if err != nil {
		return err
	}
	if len(usages) > 0 {
		return &AgentInUseError{Ref: ref, UsedBy: usages}
	}
	return nil
}

// references collects every registered owner's references for the tenant.
func (d *AgentDirectory) references(ctx context.Context, tenantID string) ([]AgentReference, error) {
	d.reportersMu.RLock()
	reporters := slices.Clone(d.reporters)
	d.reportersMu.RUnlock()
	var all []AgentReference
	for _, reporter := range reporters {
		references, err := reporter.AgentReferences(ctx, tenantID)
		if err != nil {
			return nil, err
		}
		all = append(all, references...)
	}
	return all, nil
}

// Catalogue returns the tenant's catalog.
func (d *AgentDirectory) Catalogue(ctx context.Context, tenantID string) (AgentCatalogue, error) {
	return d.store.Catalogue(ctx, tenantID)
}

// CheckRef refuses a reference to nothing, or to an Agent that does not
// declare role. It is what every owner calls when a slot is saved.
func (d *AgentDirectory) CheckRef(ctx context.Context, tenantID string, ref AgentRef, field string, role AgentRole) error {
	if ref.IsZero() {
		return nil
	}
	catalogue, err := d.store.Catalogue(ctx, tenantID)
	if err != nil {
		return err
	}
	return catalogue.CheckRef(ref, field, role)
}

// Resolve returns the Agents ref resolves to that may take work of role now,
// in failover order, and the ones passed over with why.
func (d *AgentDirectory) Resolve(ctx context.Context, tenantID string, ref AgentRef, role AgentRole) ([]Agent, []AgentAttempt, error) {
	catalogue, err := d.store.Catalogue(ctx, tenantID)
	if err != nil {
		return nil, nil, err
	}
	candidates, err := catalogue.Candidates(ref)
	if err != nil {
		return nil, nil, err
	}
	return d.resolver.Admit(ctx, tenantID, candidates, role)
}

// =============================================================================
// Project references
// =============================================================================

// projectAgentReporter reports every project's default lead.
type projectAgentReporter struct {
	projects ProjectStore
}

// NewProjectAgentUsageReporter returns the reporter of every reference a
// tenant's project records hold into its agent catalog: each project's
// default lead.
func NewProjectAgentUsageReporter(projects ProjectStore) AgentUsageReporter {
	return &projectAgentReporter{projects: projects}
}

// AgentReferences implements AgentUsageReporter.
func (r *projectAgentReporter) AgentReferences(ctx context.Context, tenantID string) ([]AgentReference, error) {
	if r.projects == nil {
		return nil, nil
	}
	projects, err := r.projects.ListByTenant(ctx, normalizeStoreTenant(tenantID))
	if err != nil {
		return nil, err
	}
	var references []AgentReference
	for _, project := range projects {
		if project == nil || project.DefaultLeadAgent.IsZero() {
			continue
		}
		references = append(references, AgentReference{
			Ref:   project.DefaultLeadAgent,
			Usage: AgentUsage{Kind: AgentUsageKindProject, ID: project.ID, Slot: AgentUsageSlotDefaultLead},
		})
	}
	return references, nil
}

// StaticAgentUsageReporter reports a fixed set of references: the node
// configuration's. They belong to one tenant (the lead agent names a
// system-tenant Agent), or to every tenant (the default triage agent names an
// Agent each tenant may define).
type StaticAgentUsageReporter struct {
	TenantID   string
	AllTenants bool
	References []AgentReference
}

// AgentReferences implements AgentUsageReporter.
func (r StaticAgentUsageReporter) AgentReferences(_ context.Context, tenantID string) ([]AgentReference, error) {
	if !r.AllTenants && normalizeStoreTenant(tenantID) != normalizeStoreTenant(r.TenantID) {
		return nil, nil
	}
	return slices.Clone(r.References), nil
}
