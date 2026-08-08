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

package orchestration

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	qerrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
)

// =============================================================================
// Key errors
// =============================================================================

// AgentKeyError reports an identifier that cannot address an agent record.
//
// It unwraps to two sentinels: ErrAgentRegistryInvalidInput, the registry's
// caller-facing validation contract, and the underlying *KeySegmentError, which
// names the keyspace rule that was violated. Callers may therefore match either
// the registry contract or the precise cause.
type AgentKeyError struct {
	// Cause is the *KeySegmentError describing the offending segment.
	Cause error
}

// Error implements the error interface.
func (e *AgentKeyError) Error() string {
	return ErrAgentRegistryInvalidInput.Error() + ": " + e.Cause.Error()
}

// Unwrap returns both sentinels for errors.Is support.
func (e *AgentKeyError) Unwrap() []error {
	return []error{ErrAgentRegistryInvalidInput, e.Cause}
}

// errAgentScanDone stops a prefix scan early once a caller-supplied limit has
// been satisfied. It never escapes the method that raises it.
var errAgentScanDone = errors.New("agent registry: scan complete")

// =============================================================================
// Registry
// =============================================================================

// agentRegistry is the one and only AgentRegistry implementation. Durability is
// a property of the injected interfaces.KVStore, not of this type: injecting
// kvstore.NewMemory() yields a fast in-process registry for tests and embedded
// use, and injecting the server's qrdb-backed store makes the very same code
// durable, replicated and encrypted at rest. The registry holds no record state
// of its own, so a restart — or a second registry over the same store — sees
// exactly the same fleet.
//
// Records live at orch/agent/{tenantID}/{agentID}. Because the tenant is the
// first variable key segment, a point read is confined to the caller's tenant
// and a list is a prefix scan of that tenant's subtree; there is no key shape
// under which one tenant can address another tenant's agent.
type agentRegistry struct {
	kv      interfaces.KVStore
	auditor StoreAuditor

	// locks serializes read-modify-write sequences per agent key. KVStore has
	// no compare-and-swap primitive, so register/update/remove/heartbeat read
	// the current record and write its successor under this lock. Writers to
	// different agents never contend.
	locks keyedMutex
}

// Compile-time interface satisfaction check.
var _ AgentRegistry = (*agentRegistry)(nil)

// NewAgentRegistry returns an AgentRegistry backed by kv.
func NewAgentRegistry(kv interfaces.KVStore, opts ...StoreOption) AgentRegistry {
	cfg := newStoreConfig(opts...)
	return &agentRegistry{kv: kv, auditor: cfg.auditor}
}

// NewInMemoryAgentRegistry returns a registry bound to qrdb's in-memory storage
// engine. It is a memory-backed *binding* of the single implementation above —
// not a second implementation — so behaviour is identical to a durable
// deployment in every respect except that the records vanish with the process.
func NewInMemoryAgentRegistry() AgentRegistry {
	return NewAgentRegistry(kvstore.NewMemory())
}

// =============================================================================
// Keys
// =============================================================================

// agentKey addresses one agent record. Both segments are validated, so an
// identifier that is empty or contains the key separator is rejected before it
// can collide with, or escape into, another tenant's subtree.
func agentKey(tenantID, agentID string) (string, error) {
	key, err := entityKey(agentKeyspace, tenantID, "agent_id", agentID)
	if err != nil {
		return "", &AgentKeyError{Cause: err}
	}
	return key, nil
}

// agentPrefix covers every agent record owned by tenantID.
func agentPrefix(tenantID string) (string, error) {
	prefix, err := tenantPrefix(agentKeyspace, tenantID)
	if err != nil {
		return "", &AgentKeyError{Cause: err}
	}
	return prefix, nil
}

// =============================================================================
// AgentRegistry
// =============================================================================

// RegisterAgent registers a new agent in the fleet registry. The agent's
// registration timestamps, initial version and default status are stamped onto
// the supplied record. Returns AgentAlreadyExistsError if the agent ID is
// already taken within the tenant namespace.
func (r *agentRegistry) RegisterAgent(ctx context.Context, agent *AgentRegistration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := r.checkStore(); err != nil {
		return err
	}
	if agent == nil {
		// A nil record carries no tenant, so the failure cannot be attributed
		// to a tenant's audit trail.
		return &AgentRegistryError{
			Sentinel: ErrAgentRegistryInvalidInput,
			Message:  "agent must not be nil",
		}
	}

	key, err := agentKey(agent.TenantID, agent.ID)
	if err != nil {
		r.audit(ctx, agent.TenantID, agent.ID, AuditActionCreate, false)
		return err
	}

	now := time.Now().UnixNano()
	agent.RegisteredAt = now
	agent.LastSeen = now
	agent.Version = 1
	if agent.Status == "" {
		agent.Status = AgentStatusRegistered
	}

	data, err := marshalAgent(agent, "failed to marshal agent")
	if err != nil {
		r.audit(ctx, agent.TenantID, agent.ID, AuditActionCreate, false)
		return err
	}

	unlock := r.locks.lock(key)
	defer unlock()

	// Registration must never overwrite an existing record, so presence is
	// checked under the key lock rather than by a blind Put.
	exists, err := r.kv.Exists(ctx, key)
	if err != nil {
		r.audit(ctx, agent.TenantID, agent.ID, AuditActionCreate, false)
		return err
	}
	if exists {
		r.audit(ctx, agent.TenantID, agent.ID, AuditActionCreate, false)
		return &AgentAlreadyExistsError{AgentID: agent.ID}
	}

	if err := r.kv.Put(ctx, key, data); err != nil {
		r.audit(ctx, agent.TenantID, agent.ID, AuditActionCreate, false)
		return err
	}

	r.audit(ctx, agent.TenantID, agent.ID, AuditActionCreate, true)
	return nil
}

// GetAgent retrieves an agent by ID within a tenant namespace.
func (r *agentRegistry) GetAgent(ctx context.Context, tenantID, agentID string) (*AgentRegistration, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := r.checkStore(); err != nil {
		return nil, err
	}

	key, err := agentKey(tenantID, agentID)
	if err != nil {
		return nil, err
	}

	agent, err := r.loadAgent(ctx, key, tenantID, agentID)
	if err != nil {
		return nil, err
	}
	return agent, nil
}

// ListAgents lists agents matching the filter within a tenant. The scan is
// bounded to the tenant's subtree, so the result can only ever contain that
// tenant's agents. Agents are returned in lexical agent-ID order; a positive
// filter.Limit truncates the result and stops the scan early.
func (r *agentRegistry) ListAgents(ctx context.Context, tenantID string, filter *AgentFilter) ([]*AgentRegistration, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := r.checkStore(); err != nil {
		return nil, err
	}

	prefix, err := agentPrefix(tenantID)
	if err != nil {
		return nil, err
	}

	agents := make([]*AgentRegistration, 0)
	err = r.kv.Scan(ctx, prefix, func(_ string, value []byte) error {
		agent := &AgentRegistration{}
		if unmarshalErr := json.Unmarshal(value, agent); unmarshalErr != nil {
			// One unreadable record must not hide the rest of the fleet from
			// an operator; skip it and keep listing. A KV-level failure below
			// still aborts, because that means the listing is incomplete in
			// ways the caller cannot see.
			return nil
		}
		// Defence in depth: the key already scopes this scan to tenantID, so a
		// record naming a different owner means the key was corrupted or
		// written out of band. Drop it rather than leak a foreign record.
		if agent.TenantID != tenantID {
			return nil
		}
		if !matchesAgentFilter(agent, filter) {
			return nil
		}

		agents = append(agents, agent)
		if filter != nil && filter.Limit > 0 && len(agents) >= filter.Limit {
			return errAgentScanDone
		}
		return nil
	})
	if err != nil && !errors.Is(err, errAgentScanDone) {
		return nil, err
	}

	return agents, nil
}

// UpdateAgent updates an existing agent registration. The supplied Version must
// match the stored version; on success the record's version is incremented and
// its last-seen timestamp refreshed.
func (r *agentRegistry) UpdateAgent(ctx context.Context, agent *AgentRegistration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := r.checkStore(); err != nil {
		return err
	}
	if agent == nil {
		return &AgentRegistryError{
			Sentinel: ErrAgentRegistryInvalidInput,
			Message:  "agent must not be nil",
		}
	}
	// Update reports an unusable agent ID before an unusable tenant ID, which
	// is the order this method has always validated in.
	if err := validateKeySegment("agent_id", agent.ID); err != nil {
		r.audit(ctx, agent.TenantID, agent.ID, AuditActionUpdate, false)
		return &AgentKeyError{Cause: err}
	}

	key, err := agentKey(agent.TenantID, agent.ID)
	if err != nil {
		r.audit(ctx, agent.TenantID, agent.ID, AuditActionUpdate, false)
		return err
	}

	unlock := r.locks.lock(key)
	defer unlock()

	current, err := r.loadAgent(ctx, key, agent.TenantID, agent.ID)
	if err != nil {
		r.audit(ctx, agent.TenantID, agent.ID, AuditActionUpdate, false)
		return err
	}

	if agent.Version != current.Version {
		r.audit(ctx, agent.TenantID, agent.ID, AuditActionUpdate, false)
		return &AgentRegistryError{
			Sentinel: ErrAgentRegistryInvalidInput,
			Message:  "version conflict: expected=" + uitoa(agent.Version) + " actual=" + uitoa(current.Version),
		}
	}

	agent.Version = current.Version + 1
	agent.LastSeen = time.Now().UnixNano()

	data, err := marshalAgent(agent, "failed to marshal updated agent")
	if err != nil {
		r.audit(ctx, agent.TenantID, agent.ID, AuditActionUpdate, false)
		return err
	}

	if err := r.kv.Put(ctx, key, data); err != nil {
		r.audit(ctx, agent.TenantID, agent.ID, AuditActionUpdate, false)
		return err
	}

	r.audit(ctx, agent.TenantID, agent.ID, AuditActionUpdate, true)
	return nil
}

// RemoveAgent removes an agent from the registry.
func (r *agentRegistry) RemoveAgent(ctx context.Context, tenantID, agentID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := r.checkStore(); err != nil {
		return err
	}

	key, err := agentKey(tenantID, agentID)
	if err != nil {
		r.audit(ctx, tenantID, agentID, AuditActionDelete, false)
		return err
	}

	unlock := r.locks.lock(key)
	defer unlock()

	// Delete is idempotent in the KV contract, so presence is confirmed first:
	// removing an agent that is not there is reported, not silently accepted.
	if _, err := r.loadAgent(ctx, key, tenantID, agentID); err != nil {
		r.audit(ctx, tenantID, agentID, AuditActionDelete, false)
		return err
	}

	if err := r.kv.Delete(ctx, key); err != nil {
		r.audit(ctx, tenantID, agentID, AuditActionDelete, false)
		return err
	}

	r.audit(ctx, tenantID, agentID, AuditActionDelete, true)
	return nil
}

// Heartbeat updates the last-seen timestamp for an agent and transitions the
// agent status to running if it is currently registered or idle.
//
// Heartbeats are high-frequency liveness telemetry rather than a state change
// an operator would review, so they are deliberately not audited. The path is
// one locked read plus one write — never a scan.
func (r *agentRegistry) Heartbeat(ctx context.Context, tenantID, agentID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := r.checkStore(); err != nil {
		return err
	}

	key, err := agentKey(tenantID, agentID)
	if err != nil {
		return err
	}

	unlock := r.locks.lock(key)
	defer unlock()

	agent, err := r.loadAgent(ctx, key, tenantID, agentID)
	if err != nil {
		return err
	}

	agent.LastSeen = time.Now().UnixNano()
	if agent.Status == AgentStatusRegistered || agent.Status == AgentStatusIdle {
		agent.Status = AgentStatusRunning
	}
	agent.Version++

	data, err := marshalAgent(agent, "failed to marshal heartbeat agent")
	if err != nil {
		return err
	}

	return r.kv.Put(ctx, key, data)
}

// DeleteTenantAgents deletes ALL agents for a tenant (cascade delete). Deleting
// a tenant that owns no agents succeeds.
func (r *agentRegistry) DeleteTenantAgents(ctx context.Context, tenantID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := r.checkStore(); err != nil {
		return err
	}

	prefix, err := agentPrefix(tenantID)
	if err != nil {
		r.audit(ctx, tenantID, "", AuditActionDelete, false)
		return err
	}

	// The scan streams the tenant's subtree and collects keys only — never
	// record bodies, and never a key outside this tenant. Deletion runs after
	// the scan because mutating a store while one of its iterators is open is
	// outside the KVStore contract.
	keys := make([]string, 0)
	if err := r.kv.Scan(ctx, prefix, func(key string, _ []byte) error {
		keys = append(keys, key)
		return nil
	}); err != nil {
		r.audit(ctx, tenantID, "", AuditActionDelete, false)
		return err
	}

	for _, key := range keys {
		if err := ctx.Err(); err != nil {
			return err
		}
		agentID := idFromKey(key, prefix)

		unlock := r.locks.lock(key)
		err := r.kv.Delete(ctx, key)
		unlock()

		if err != nil {
			r.audit(ctx, tenantID, agentID, AuditActionDelete, false)
			return err
		}
		r.audit(ctx, tenantID, agentID, AuditActionDelete, true)
	}

	return nil
}

// =============================================================================
// Helpers
// =============================================================================

// checkStore rejects a registry that was constructed without a KV store. The
// registry fails closed rather than panicking on the first request.
func (r *agentRegistry) checkStore() error {
	if r.kv == nil {
		return &AgentRegistryError{
			Sentinel: ErrAgentRegistryRequired,
			Message:  "kv store must not be nil",
		}
	}
	return nil
}

// loadAgent reads the record at key. A missing key, a nil value and a record
// owned by another tenant are all reported as not-found.
func (r *agentRegistry) loadAgent(ctx context.Context, key, tenantID, agentID string) (*AgentRegistration, error) {
	data, err := r.kv.Get(ctx, key)
	if err != nil {
		if qerrors.IsNotFound(err) {
			return nil, &AgentNotFoundError{TenantID: tenantID, AgentID: agentID}
		}
		return nil, err
	}
	if data == nil {
		return nil, &AgentNotFoundError{TenantID: tenantID, AgentID: agentID}
	}

	agent := &AgentRegistration{}
	if err := json.Unmarshal(data, agent); err != nil {
		return nil, &AgentRegistryError{
			Sentinel: ErrAgentRegistryMarshal,
			Message:  "failed to unmarshal agent: " + err.Error(),
		}
	}

	// Defence in depth: the key already scopes the read to tenantID, so a
	// record naming a different owner means the key was corrupted or written
	// out of band. Report not-found rather than hand back a foreign record.
	if agent.TenantID != tenantID {
		return nil, &AgentNotFoundError{TenantID: tenantID, AgentID: agentID}
	}

	return agent, nil
}

// marshalAgent serializes a record, reporting failures under the registry's
// marshal sentinel prefixed with the caller's description of the operation.
func marshalAgent(agent *AgentRegistration, what string) ([]byte, error) {
	data, err := json.Marshal(agent)
	if err != nil {
		return nil, &AgentRegistryError{
			Sentinel: ErrAgentRegistryMarshal,
			Message:  what + ": " + err.Error(),
		}
	}
	return data, nil
}

// matchesAgentFilter reports whether agent satisfies every populated field of
// filter. An empty field matches everything; a nil filter matches every agent.
func matchesAgentFilter(agent *AgentRegistration, filter *AgentFilter) bool {
	if filter == nil {
		return true
	}
	if filter.Host != "" && agent.Host != filter.Host {
		return false
	}
	if filter.Status != "" && agent.Status != filter.Status {
		return false
	}
	if filter.AgentType != "" && agent.AgentType != filter.AgentType {
		return false
	}
	if filter.Backend != "" && agent.Backend != filter.Backend {
		return false
	}
	if filter.Model != "" && agent.Model != filter.Model {
		return false
	}
	return true
}

// audit records one store mutation. Reads and heartbeats never call it.
func (r *agentRegistry) audit(ctx context.Context, tenantID, agentID, action string, success bool) {
	emitStoreAudit(ctx, r.auditor, StoreAuditEvent{
		TenantID:   tenantID,
		Resource:   AuditResourceAgent,
		ResourceID: agentID,
		Action:     action,
		Success:    success,
	})
}

// uitoa converts a uint64 to its decimal string representation
// without importing strconv.
func uitoa(val uint64) string {
	if val == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf) - 1
	for val > 0 {
		buf[i] = byte('0' + val%10)
		val /= 10
		i--
	}
	return string(buf[i+1:])
}
