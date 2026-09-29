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
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	qerrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
)

// =============================================================================
// AgentCatalogStore — interface
// =============================================================================

// AgentCatalogStore persists a tenant's Agents and pools.
//
// Every method is tenant-explicit, so one tenant's catalog is unreachable from
// another's. An empty tenant is the system tenant.
//
// THE STORE KEEPS ONLY ITS OWN INVARIANTS: a pool names Agents that exist, and
// an Agent a pool names cannot be deleted. Whether a binding, a project or the
// node configuration still references an Agent is a question for the
// AgentDirectory, which is what an operator's delete goes through.
type AgentCatalogStore interface {
	// CreateAgent stores a new Agent. It refuses an ID already taken.
	CreateAgent(ctx context.Context, tenantID string, agent *Agent) error

	// UpdateAgent replaces an existing Agent. It refuses an unknown ID.
	UpdateAgent(ctx context.Context, tenantID string, agent *Agent) error

	// GetAgent reads one Agent by ID.
	GetAgent(ctx context.Context, tenantID, id string) (*Agent, error)

	// ListAgents returns every Agent of tenantID, ID-ordered.
	ListAgents(ctx context.Context, tenantID string) ([]*Agent, error)

	// DeleteAgent removes one Agent. It refuses while a pool lists it.
	DeleteAgent(ctx context.Context, tenantID, id string) error

	// ModifyAgent reads one Agent, applies modify, and writes the result
	// back when modify reports a change — all under the tenant's catalog
	// lock, so a concurrent operator write is never lost between the read
	// and the write. It reports whether anything was written.
	ModifyAgent(ctx context.Context, tenantID, id string, modify func(*Agent) bool) (bool, error)

	// CreatePool stores a new pool. It refuses an ID already taken, and a
	// pool naming an Agent that does not exist.
	CreatePool(ctx context.Context, tenantID string, pool *AgentPool) error

	// UpdatePool replaces an existing pool, under the same rule as CreatePool.
	UpdatePool(ctx context.Context, tenantID string, pool *AgentPool) error

	// GetPool reads one pool by ID.
	GetPool(ctx context.Context, tenantID, id string) (*AgentPool, error)

	// ListPools returns every pool of tenantID, ID-ordered.
	ListPools(ctx context.Context, tenantID string) ([]*AgentPool, error)

	// DeletePool removes one pool.
	DeletePool(ctx context.Context, tenantID, id string) error

	// Catalogue returns the tenant's whole catalog in one read, which is what
	// resolution needs: walking a pool must see every Agent it can name.
	Catalogue(ctx context.Context, tenantID string) (AgentCatalogue, error)
}

// =============================================================================
// Key derivation
//
// orch/agentdef/{tenant}/{id}  -> Agent (JSON)
// orch/agentpool/{tenant}/{id} -> AgentPool (JSON)
// =============================================================================

func agentDefinitionKey(tenantID, id string) (string, error) {
	key, err := entityKey(agentDefinitionKeyspace, normalizeStoreTenant(tenantID), "agent_id", id)
	if err != nil {
		return "", catalogKeyError(err)
	}
	return key, nil
}

func agentPoolKey(tenantID, id string) (string, error) {
	key, err := entityKey(agentPoolKeyspace, normalizeStoreTenant(tenantID), "pool_id", id)
	if err != nil {
		return "", catalogKeyError(err)
	}
	return key, nil
}

// catalogKeyError converts a key-layout violation into this store's own
// validation vocabulary, mirroring accountKeyError.
func catalogKeyError(err error) error {
	var segment *KeySegmentError
	if !errors.As(err, &segment) {
		return err
	}
	return &InputValidationError{Field: segment.Field, Message: segment.Message}
}

// =============================================================================
// agentCatalogStore — the single KV-backed AgentCatalogStore
// =============================================================================

// agentCatalogStore holds no data of its own: durability, replication and
// storage-barrier encryption are properties of the backing KV. The records
// are METADATA ONLY — an Agent names an endpoint and accounts, never a
// credential — which is what keeps the catalog safe to list from the GUI.
type agentCatalogStore struct {
	kv      interfaces.KVStore
	auditor StoreAuditor
	now     func() time.Time

	// recordMu serializes the read-modify-write sequences that span records:
	// a pool write validating against the Agents it names, and an Agent
	// delete validating against the pools that name it. Held on the TENANT,
	// because the invariant is a property of the tenant's catalog as a whole.
	recordMu keyedMutex
}

// Compile-time interface satisfaction check.
var _ AgentCatalogStore = (*agentCatalogStore)(nil)

// NewAgentCatalogStore returns an AgentCatalogStore backed by kv.
func NewAgentCatalogStore(kv interfaces.KVStore, opts ...StoreOption) AgentCatalogStore {
	cfg := newStoreConfig(opts...)
	return &agentCatalogStore{kv: kv, auditor: cfg.auditor, now: func() time.Time { return time.Now().UTC() }}
}

// NewInMemoryAgentCatalogStore returns a memory-backed binding of the store
// returned by NewAgentCatalogStore — not a second implementation. It exists for
// tests and for embedded use that needs no durability.
func NewInMemoryAgentCatalogStore() AgentCatalogStore {
	return NewAgentCatalogStore(kvstore.NewMemory())
}

// begin runs the checks every call makes before touching the KV.
func (s *agentCatalogStore) begin(ctx context.Context, tenantID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.kv == nil {
		return &AgentCatalogStoreError{Message: "kv store must not be nil"}
	}
	return scopeCheck(ctx, normalizeStoreTenant(tenantID))
}

// =============================================================================
// Agents
// =============================================================================

// CreateAgent stores a new Agent.
func (s *agentCatalogStore) CreateAgent(ctx context.Context, tenantID string, agent *Agent) error {
	err := s.writeAgent(ctx, tenantID, agent, true)
	s.audit(ctx, AuditResourceAgentDefinition, AuditActionCreate, tenantID, agentIDOf(agent), err)
	return err
}

// UpdateAgent replaces an existing Agent.
func (s *agentCatalogStore) UpdateAgent(ctx context.Context, tenantID string, agent *Agent) error {
	err := s.writeAgent(ctx, tenantID, agent, false)
	s.audit(ctx, AuditResourceAgentDefinition, AuditActionUpdate, tenantID, agentIDOf(agent), err)
	return err
}

func agentIDOf(agent *Agent) string {
	if agent == nil {
		return ""
	}
	return agent.ID
}

// writeAgent is CreateAgent and UpdateAgent: create refuses an existing ID,
// update refuses a missing one and keeps the original CreatedAt.
func (s *agentCatalogStore) writeAgent(ctx context.Context, tenantID string, agent *Agent, create bool) error {
	if err := s.begin(ctx, tenantID); err != nil {
		return err
	}
	if agent == nil {
		return &InputValidationError{Field: "agent", Message: "must not be nil"}
	}
	// AN AGENT IS REFUSED AT THE WRITE, NEVER AT THE SPAWN. An endpoint with
	// no dialect or an agent with no model is only detectable as a defect
	// here; by the time a spawn reads it, the operator believes it works.
	if err := agent.Validate(); err != nil {
		return err
	}
	key, err := agentDefinitionKey(tenantID, agent.ID)
	if err != nil {
		return err
	}

	unlock := s.recordMu.lock(normalizeStoreTenant(tenantID))
	defer unlock()

	held, err := s.loadAgent(ctx, key, agent.ID)
	switch {
	case err == nil && create:
		return &AgentExistsError{Kind: "agent", ID: agent.ID}
	case err != nil && (!create || !errors.Is(err, ErrAgentUnknown)):
		return err
	}
	now := s.now()
	agent.UpdatedAt = now
	agent.CreatedAt = now
	if held != nil {
		agent.CreatedAt = held.CreatedAt
	}
	data, err := json.Marshal(agent)
	if err != nil {
		return &AgentCatalogStoreError{Message: "failed to marshal agent: " + err.Error(), Cause: err}
	}
	if err := s.kv.Put(ctx, key, data); err != nil {
		return &AgentCatalogStoreError{Message: "failed to write agent: " + err.Error(), Cause: err}
	}
	return nil
}

// GetAgent reads one Agent by ID.
func (s *agentCatalogStore) GetAgent(ctx context.Context, tenantID, id string) (*Agent, error) {
	if err := s.begin(ctx, tenantID); err != nil {
		return nil, err
	}
	key, err := agentDefinitionKey(tenantID, id)
	if err != nil {
		return nil, err
	}
	return s.loadAgent(ctx, key, id)
}

func (s *agentCatalogStore) loadAgent(ctx context.Context, key, id string) (*Agent, error) {
	data, err := s.kv.Get(ctx, key)
	if err != nil {
		if qerrors.IsNotFound(err) {
			return nil, &UnknownAgentError{ID: id}
		}
		return nil, &AgentCatalogStoreError{Message: "failed to read agent: " + err.Error(), Cause: err}
	}
	if len(data) == 0 {
		return nil, &UnknownAgentError{ID: id}
	}
	var agent Agent
	if err := json.Unmarshal(data, &agent); err != nil {
		return nil, &AgentCatalogStoreError{Message: "failed to unmarshal agent: " + err.Error(), Cause: err}
	}
	agent.ID = id
	return &agent, nil
}

// ListAgents returns every Agent of tenantID, ID-ordered.
func (s *agentCatalogStore) ListAgents(ctx context.Context, tenantID string) ([]*Agent, error) {
	if err := s.begin(ctx, tenantID); err != nil {
		return nil, err
	}
	agents, err := s.scanAgents(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	out := make([]*Agent, 0, len(agents))
	for _, id := range sortedKeys(agents) {
		agent := agents[id]
		out = append(out, &agent)
	}
	return out, nil
}

func (s *agentCatalogStore) scanAgents(ctx context.Context, tenantID string) (map[string]Agent, error) {
	return scanCatalog(ctx, s.kv, agentDefinitionKeyspace, tenantID, "agent", func(agent *Agent, id string) { agent.ID = id })
}

// scanCatalog reads every record of one keyspace for a tenant. The key is the
// authority on the identifier: a record whose body disagrees would otherwise
// be unaddressable — listed under one ID and read back under another.
func scanCatalog[T any](ctx context.Context, kv interfaces.KVStore, keyspace, tenantID, kind string, setID func(*T, string)) (map[string]T, error) {
	prefix, err := tenantPrefix(keyspace, normalizeStoreTenant(tenantID))
	if err != nil {
		return nil, catalogKeyError(err)
	}
	records := make(map[string]T)
	var decodeErr error
	scanErr := kv.Scan(ctx, prefix, func(key string, value []byte) error {
		var record T
		if err := json.Unmarshal(value, &record); err != nil {
			decodeErr = &AgentCatalogStoreError{Message: "failed to unmarshal " + kind + ": " + err.Error(), Cause: err}
			return decodeErr
		}
		id := idFromKey(key, prefix)
		setID(&record, id)
		records[id] = record
		return nil
	})
	if decodeErr != nil {
		return nil, decodeErr
	}
	if scanErr != nil {
		return nil, &AgentCatalogStoreError{Message: "failed to list " + kind + "s: " + scanErr.Error(), Cause: scanErr}
	}
	return records, nil
}

// ModifyAgent applies modify to one Agent under the tenant's catalog lock.
func (s *agentCatalogStore) ModifyAgent(ctx context.Context, tenantID, id string, modify func(*Agent) bool) (bool, error) {
	changed, err := s.modifyAgent(ctx, tenantID, id, modify)
	if changed || err != nil {
		s.audit(ctx, AuditResourceAgentDefinition, AuditActionUpdate, tenantID, id, err)
	}
	return changed, err
}

func (s *agentCatalogStore) modifyAgent(ctx context.Context, tenantID, id string, modify func(*Agent) bool) (bool, error) {
	if err := s.begin(ctx, tenantID); err != nil {
		return false, err
	}
	if modify == nil {
		return false, &InputValidationError{Field: "modify", Message: "must not be nil"}
	}
	key, err := agentDefinitionKey(tenantID, id)
	if err != nil {
		return false, err
	}

	unlock := s.recordMu.lock(normalizeStoreTenant(tenantID))
	defer unlock()

	agent, err := s.loadAgent(ctx, key, id)
	if err != nil {
		return false, err
	}
	if !modify(agent) {
		return false, nil
	}
	agent.ID = id
	if err := agent.Validate(); err != nil {
		return false, err
	}
	agent.UpdatedAt = s.now()
	data, err := json.Marshal(agent)
	if err != nil {
		return false, &AgentCatalogStoreError{Message: "failed to marshal agent: " + err.Error(), Cause: err}
	}
	if err := s.kv.Put(ctx, key, data); err != nil {
		return false, &AgentCatalogStoreError{Message: "failed to write agent: " + err.Error(), Cause: err}
	}
	return true, nil
}

// DeleteAgent removes one Agent.
func (s *agentCatalogStore) DeleteAgent(ctx context.Context, tenantID, id string) error {
	err := s.deleteAgent(ctx, tenantID, id)
	s.audit(ctx, AuditResourceAgentDefinition, AuditActionDelete, tenantID, id, err)
	return err
}

func (s *agentCatalogStore) deleteAgent(ctx context.Context, tenantID, id string) error {
	if err := s.begin(ctx, tenantID); err != nil {
		return err
	}
	key, err := agentDefinitionKey(tenantID, id)
	if err != nil {
		return err
	}

	unlock := s.recordMu.lock(normalizeStoreTenant(tenantID))
	defer unlock()

	if _, err := s.loadAgent(ctx, key, id); err != nil {
		return err
	}
	// A DANGLING POOL MEMBER IS REFUSED HERE, NOT DISCOVERED AT A SPAWN.
	// Naming the pools at the delete puts the reason where the decision is.
	pools, err := s.scanPools(ctx, tenantID)
	if err != nil {
		return err
	}
	if usages := poolUsages(pools, id); len(usages) > 0 {
		return &AgentInUseError{Ref: AgentRefTo(id), UsedBy: usages}
	}
	if err := s.kv.Delete(ctx, key); err != nil {
		return &AgentCatalogStoreError{Message: "failed to delete agent: " + err.Error(), Cause: err}
	}
	return nil
}

// poolUsages lists the pools naming agentID, ID-ordered.
func poolUsages(pools map[string]AgentPool, agentID string) []AgentUsage {
	var usages []AgentUsage
	for _, poolID := range sortedKeys(pools) {
		for _, member := range pools[poolID].Agents {
			if member == agentID {
				usages = append(usages, AgentUsage{Kind: AgentUsageKindPool, ID: poolID, Slot: AgentUsageSlotMember})
				break
			}
		}
	}
	return usages
}

// =============================================================================
// Pools
// =============================================================================

// CreatePool stores a new pool.
func (s *agentCatalogStore) CreatePool(ctx context.Context, tenantID string, pool *AgentPool) error {
	err := s.writePool(ctx, tenantID, pool, true)
	s.audit(ctx, AuditResourceAgentPool, AuditActionCreate, tenantID, poolIDOf(pool), err)
	return err
}

// UpdatePool replaces an existing pool.
func (s *agentCatalogStore) UpdatePool(ctx context.Context, tenantID string, pool *AgentPool) error {
	err := s.writePool(ctx, tenantID, pool, false)
	s.audit(ctx, AuditResourceAgentPool, AuditActionUpdate, tenantID, poolIDOf(pool), err)
	return err
}

func poolIDOf(pool *AgentPool) string {
	if pool == nil {
		return ""
	}
	return pool.ID
}

func (s *agentCatalogStore) writePool(ctx context.Context, tenantID string, pool *AgentPool, create bool) error {
	if err := s.begin(ctx, tenantID); err != nil {
		return err
	}
	if pool == nil {
		return &InputValidationError{Field: "pool", Message: "must not be nil"}
	}
	if err := pool.Validate(); err != nil {
		return err
	}
	key, err := agentPoolKey(tenantID, pool.ID)
	if err != nil {
		return err
	}

	unlock := s.recordMu.lock(normalizeStoreTenant(tenantID))
	defer unlock()

	held, err := s.loadPool(ctx, key, pool.ID)
	switch {
	case err == nil && create:
		return &AgentExistsError{Kind: "pool", ID: pool.ID}
	case err != nil && (!create || !errors.Is(err, ErrAgentPoolUnknown)):
		return err
	}
	// EVERY NAMED AGENT MUST EXIST. A name that resolves to nothing is a
	// dispatch that fails at the moment the fleet needs the fallback most,
	// which is when the preferred Agent is already refusing work.
	agents, err := s.scanAgents(ctx, tenantID)
	if err != nil {
		return err
	}
	for _, id := range pool.Agents {
		if _, ok := agents[id]; !ok {
			return &UnknownAgentError{ID: id, Pool: pool.ID}
		}
	}
	now := s.now()
	pool.UpdatedAt = now
	pool.CreatedAt = now
	if held != nil {
		pool.CreatedAt = held.CreatedAt
	}
	data, err := json.Marshal(pool)
	if err != nil {
		return &AgentCatalogStoreError{Message: "failed to marshal pool: " + err.Error(), Cause: err}
	}
	if err := s.kv.Put(ctx, key, data); err != nil {
		return &AgentCatalogStoreError{Message: "failed to write pool: " + err.Error(), Cause: err}
	}
	return nil
}

// GetPool reads one pool by ID.
func (s *agentCatalogStore) GetPool(ctx context.Context, tenantID, id string) (*AgentPool, error) {
	if err := s.begin(ctx, tenantID); err != nil {
		return nil, err
	}
	key, err := agentPoolKey(tenantID, id)
	if err != nil {
		return nil, err
	}
	return s.loadPool(ctx, key, id)
}

func (s *agentCatalogStore) loadPool(ctx context.Context, key, id string) (*AgentPool, error) {
	data, err := s.kv.Get(ctx, key)
	if err != nil {
		if qerrors.IsNotFound(err) {
			return nil, &UnknownAgentPoolError{ID: id}
		}
		return nil, &AgentCatalogStoreError{Message: "failed to read pool: " + err.Error(), Cause: err}
	}
	if len(data) == 0 {
		return nil, &UnknownAgentPoolError{ID: id}
	}
	var pool AgentPool
	if err := json.Unmarshal(data, &pool); err != nil {
		return nil, &AgentCatalogStoreError{Message: "failed to unmarshal pool: " + err.Error(), Cause: err}
	}
	pool.ID = id
	return &pool, nil
}

// ListPools returns every pool of tenantID, ID-ordered.
func (s *agentCatalogStore) ListPools(ctx context.Context, tenantID string) ([]*AgentPool, error) {
	if err := s.begin(ctx, tenantID); err != nil {
		return nil, err
	}
	pools, err := s.scanPools(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	out := make([]*AgentPool, 0, len(pools))
	for _, id := range sortedKeys(pools) {
		pool := pools[id]
		out = append(out, &pool)
	}
	return out, nil
}

func (s *agentCatalogStore) scanPools(ctx context.Context, tenantID string) (map[string]AgentPool, error) {
	return scanCatalog(ctx, s.kv, agentPoolKeyspace, tenantID, "pool", func(pool *AgentPool, id string) { pool.ID = id })
}

// DeletePool removes one pool.
func (s *agentCatalogStore) DeletePool(ctx context.Context, tenantID, id string) error {
	err := s.deletePool(ctx, tenantID, id)
	s.audit(ctx, AuditResourceAgentPool, AuditActionDelete, tenantID, id, err)
	return err
}

func (s *agentCatalogStore) deletePool(ctx context.Context, tenantID, id string) error {
	if err := s.begin(ctx, tenantID); err != nil {
		return err
	}
	key, err := agentPoolKey(tenantID, id)
	if err != nil {
		return err
	}

	unlock := s.recordMu.lock(normalizeStoreTenant(tenantID))
	defer unlock()

	if _, err := s.loadPool(ctx, key, id); err != nil {
		return err
	}
	if err := s.kv.Delete(ctx, key); err != nil {
		return &AgentCatalogStoreError{Message: "failed to delete pool: " + err.Error(), Cause: err}
	}
	return nil
}

// =============================================================================
// Catalogue
// =============================================================================

// Catalogue returns the tenant's Agents and pools in one read.
func (s *agentCatalogStore) Catalogue(ctx context.Context, tenantID string) (AgentCatalogue, error) {
	if err := s.begin(ctx, tenantID); err != nil {
		return AgentCatalogue{}, err
	}
	agents, err := s.scanAgents(ctx, tenantID)
	if err != nil {
		return AgentCatalogue{}, err
	}
	pools, err := s.scanPools(ctx, tenantID)
	if err != nil {
		return AgentCatalogue{}, err
	}
	return AgentCatalogue{Agents: agents, Pools: pools}, nil
}

// audit emits exactly one store event per mutating call, recording a refused
// attempt with Success false.
func (s *agentCatalogStore) audit(ctx context.Context, resource, action, tenantID, id string, err error) {
	emitStoreAudit(ctx, s.auditor, StoreAuditEvent{
		TenantID:   normalizeStoreTenant(tenantID),
		Resource:   resource,
		ResourceID: id,
		Action:     action,
		Success:    err == nil,
	})
}

// sortedKeys returns a map's keys in ascending order. Listing is ordered so the
// GUI and the CLI render a stable catalog rather than one that reshuffles on
// every poll.
func sortedKeys[V any](m map[string]V) []string {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool { return strings.Compare(names[i], names[j]) < 0 })
	return names
}
