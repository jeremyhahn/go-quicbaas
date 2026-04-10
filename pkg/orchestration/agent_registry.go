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
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// agentEntry wraps serialized agent data in a pointer-comparable struct.
// sync.Map.CompareAndSwap requires comparable values; []byte is not
// comparable, but *agentEntry pointers are compared by identity which
// provides the exact CAS semantics needed for lock-free updates.
type agentEntry struct {
	data []byte
}

// inMemoryAgentRegistry implements AgentRegistry using sync.Map for lock-free
// concurrent access. This is the default registry used when no external
// persistence backend is configured.
type inMemoryAgentRegistry struct {
	agents sync.Map // map[string]*agentEntry (keyed by "tenantID/agentID")
	count  atomic.Int64
}

// NewInMemoryAgentRegistry creates a new in-memory agent registry.
func NewInMemoryAgentRegistry() AgentRegistry {
	return &inMemoryAgentRegistry{}
}

// agentKey builds a composite key from tenant and agent IDs.
func agentKey(tenantID, agentID string) string {
	return tenantID + "/" + agentID
}

// RegisterAgent registers a new agent in the fleet registry.
// Returns error if the agent ID already exists within the tenant namespace.
func (r *inMemoryAgentRegistry) RegisterAgent(ctx context.Context, agent *AgentRegistration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if agent == nil {
		return &AgentRegistryError{
			Sentinel: ErrAgentRegistryInvalidInput,
			Message:  "agent must not be nil",
		}
	}
	if agent.TenantID == "" {
		return &AgentRegistryError{
			Sentinel: ErrAgentRegistryInvalidInput,
			Message:  "tenant ID must not be empty",
		}
	}
	if agent.ID == "" {
		return &AgentRegistryError{
			Sentinel: ErrAgentRegistryInvalidInput,
			Message:  "agent ID must not be empty",
		}
	}

	now := time.Now().UnixNano()
	agent.RegisteredAt = now
	agent.LastSeen = now
	agent.Version = 1

	if agent.Status == "" {
		agent.Status = AgentStatusRegistered
	}

	data, err := json.Marshal(agent)
	if err != nil {
		return &AgentRegistryError{
			Sentinel: ErrAgentRegistryMarshal,
			Message:  "failed to marshal agent: " + err.Error(),
		}
	}

	key := agentKey(agent.TenantID, agent.ID)
	if _, loaded := r.agents.LoadOrStore(key, &agentEntry{data: data}); loaded {
		return &AgentAlreadyExistsError{AgentID: agent.ID}
	}

	r.count.Add(1)
	return nil
}

// GetAgent retrieves an agent by ID within a tenant namespace.
func (r *inMemoryAgentRegistry) GetAgent(ctx context.Context, tenantID, agentID string) (*AgentRegistration, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if tenantID == "" {
		return nil, &AgentRegistryError{
			Sentinel: ErrAgentRegistryInvalidInput,
			Message:  "tenant ID must not be empty",
		}
	}
	if agentID == "" {
		return nil, &AgentRegistryError{
			Sentinel: ErrAgentRegistryInvalidInput,
			Message:  "agent ID must not be empty",
		}
	}

	key := agentKey(tenantID, agentID)
	val, ok := r.agents.Load(key)
	if !ok {
		return nil, &AgentNotFoundError{TenantID: tenantID, AgentID: agentID}
	}

	entry, ok := val.(*agentEntry)
	if !ok {
		return nil, &AgentRegistryError{
			Sentinel: ErrAgentRegistryMarshal,
			Message:  "agent value is not *agentEntry",
		}
	}

	var agent AgentRegistration
	if err := json.Unmarshal(entry.data, &agent); err != nil {
		return nil, &AgentRegistryError{
			Sentinel: ErrAgentRegistryMarshal,
			Message:  "failed to unmarshal agent: " + err.Error(),
		}
	}

	return &agent, nil
}

// ListAgents lists agents matching the filter within a tenant.
func (r *inMemoryAgentRegistry) ListAgents(ctx context.Context, tenantID string, filter *AgentFilter) ([]*AgentRegistration, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if tenantID == "" {
		return nil, &AgentRegistryError{
			Sentinel: ErrAgentRegistryInvalidInput,
			Message:  "tenant ID must not be empty",
		}
	}

	prefix := tenantID + "/"
	agents := make([]*AgentRegistration, 0)
	var unmarshalErr error

	r.agents.Range(func(key, value interface{}) bool {
		if ctx.Err() != nil {
			return false
		}

		keyStr, ok := key.(string)
		if !ok || !strings.HasPrefix(keyStr, prefix) {
			return true
		}

		entry, ok := value.(*agentEntry)
		if !ok {
			return true
		}

		var agent AgentRegistration
		if err := json.Unmarshal(entry.data, &agent); err != nil {
			unmarshalErr = &AgentRegistryError{
				Sentinel: ErrAgentRegistryMarshal,
				Message:  "failed to unmarshal agent: " + err.Error(),
			}
			return false
		}

		// Apply filters
		if filter != nil {
			if filter.Host != "" && agent.Host != filter.Host {
				return true
			}
			if filter.Status != "" && agent.Status != filter.Status {
				return true
			}
			if filter.AgentType != "" && agent.AgentType != filter.AgentType {
				return true
			}
			if filter.Backend != "" && agent.Backend != filter.Backend {
				return true
			}
			if filter.Model != "" && agent.Model != filter.Model {
				return true
			}
		}

		agents = append(agents, &agent)

		// Apply limit
		if filter != nil && filter.Limit > 0 && len(agents) >= filter.Limit {
			return false
		}

		return true
	})

	if unmarshalErr != nil {
		return nil, unmarshalErr
	}

	return agents, nil
}

// UpdateAgent updates an existing agent registration. Uses CAS for conflict
// detection. The agent's Version field must match the stored version;
// otherwise an error is returned.
func (r *inMemoryAgentRegistry) UpdateAgent(ctx context.Context, agent *AgentRegistration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if agent == nil {
		return &AgentRegistryError{
			Sentinel: ErrAgentRegistryInvalidInput,
			Message:  "agent must not be nil",
		}
	}
	if agent.ID == "" {
		return &AgentRegistryError{
			Sentinel: ErrAgentRegistryInvalidInput,
			Message:  "agent ID must not be empty",
		}
	}
	if agent.TenantID == "" {
		return &AgentRegistryError{
			Sentinel: ErrAgentRegistryInvalidInput,
			Message:  "tenant ID must not be empty",
		}
	}

	key := agentKey(agent.TenantID, agent.ID)

	// Load-compare-swap loop for lock-free CAS
	for {
		existing, ok := r.agents.Load(key)
		if !ok {
			return &AgentNotFoundError{TenantID: agent.TenantID, AgentID: agent.ID}
		}

		oldEntry, ok := existing.(*agentEntry)
		if !ok {
			return &AgentRegistryError{
				Sentinel: ErrAgentRegistryMarshal,
				Message:  "agent value is not *agentEntry",
			}
		}

		var current AgentRegistration
		if err := json.Unmarshal(oldEntry.data, &current); err != nil {
			return &AgentRegistryError{
				Sentinel: ErrAgentRegistryMarshal,
				Message:  "failed to unmarshal existing agent: " + err.Error(),
			}
		}

		if agent.Version != current.Version {
			return &AgentRegistryError{
				Sentinel: ErrAgentRegistryInvalidInput,
				Message:  "version conflict: expected=" + uitoa(agent.Version) + " actual=" + uitoa(current.Version),
			}
		}

		agent.Version = current.Version + 1
		agent.LastSeen = time.Now().UnixNano()

		newData, err := json.Marshal(agent)
		if err != nil {
			return &AgentRegistryError{
				Sentinel: ErrAgentRegistryMarshal,
				Message:  "failed to marshal updated agent: " + err.Error(),
			}
		}

		if r.agents.CompareAndSwap(key, oldEntry, &agentEntry{data: newData}) {
			return nil
		}
		// CompareAndSwap failed (another goroutine changed it), retry
	}
}

// RemoveAgent removes an agent from the registry.
func (r *inMemoryAgentRegistry) RemoveAgent(ctx context.Context, tenantID, agentID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tenantID == "" {
		return &AgentRegistryError{
			Sentinel: ErrAgentRegistryInvalidInput,
			Message:  "tenant ID must not be empty",
		}
	}
	if agentID == "" {
		return &AgentRegistryError{
			Sentinel: ErrAgentRegistryInvalidInput,
			Message:  "agent ID must not be empty",
		}
	}

	key := agentKey(tenantID, agentID)
	if _, loaded := r.agents.LoadAndDelete(key); !loaded {
		return &AgentNotFoundError{TenantID: tenantID, AgentID: agentID}
	}

	r.count.Add(-1)
	return nil
}

// Heartbeat updates the last-seen timestamp for an agent and transitions
// the agent status to running if currently registered or idle.
func (r *inMemoryAgentRegistry) Heartbeat(ctx context.Context, tenantID, agentID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tenantID == "" {
		return &AgentRegistryError{
			Sentinel: ErrAgentRegistryInvalidInput,
			Message:  "tenant ID must not be empty",
		}
	}
	if agentID == "" {
		return &AgentRegistryError{
			Sentinel: ErrAgentRegistryInvalidInput,
			Message:  "agent ID must not be empty",
		}
	}

	key := agentKey(tenantID, agentID)

	// CAS loop for lock-free heartbeat update
	for {
		existing, ok := r.agents.Load(key)
		if !ok {
			return &AgentNotFoundError{TenantID: tenantID, AgentID: agentID}
		}

		oldEntry, ok := existing.(*agentEntry)
		if !ok {
			return &AgentRegistryError{
				Sentinel: ErrAgentRegistryMarshal,
				Message:  "agent value is not *agentEntry",
			}
		}

		var agent AgentRegistration
		if err := json.Unmarshal(oldEntry.data, &agent); err != nil {
			return &AgentRegistryError{
				Sentinel: ErrAgentRegistryMarshal,
				Message:  "failed to unmarshal agent: " + err.Error(),
			}
		}

		agent.LastSeen = time.Now().UnixNano()

		if agent.Status == AgentStatusRegistered || agent.Status == AgentStatusIdle {
			agent.Status = AgentStatusRunning
		}

		agent.Version++

		newData, err := json.Marshal(&agent)
		if err != nil {
			return &AgentRegistryError{
				Sentinel: ErrAgentRegistryMarshal,
				Message:  "failed to marshal heartbeat agent: " + err.Error(),
			}
		}

		if r.agents.CompareAndSwap(key, oldEntry, &agentEntry{data: newData}) {
			return nil
		}
		// CAS failed, retry
	}
}

// DeleteTenantAgents deletes ALL agents for a tenant (cascade delete).
func (r *inMemoryAgentRegistry) DeleteTenantAgents(ctx context.Context, tenantID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tenantID == "" {
		return &AgentRegistryError{
			Sentinel: ErrAgentRegistryInvalidInput,
			Message:  "tenant ID must not be empty",
		}
	}

	prefix := tenantID + "/"
	var deleted int64

	r.agents.Range(func(key, value interface{}) bool {
		if ctx.Err() != nil {
			return false
		}

		keyStr, ok := key.(string)
		if !ok {
			return true
		}

		if strings.HasPrefix(keyStr, prefix) {
			r.agents.Delete(key)
			deleted++
		}

		return true
	})

	r.count.Add(-deleted)
	return nil
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

// Compile-time interface satisfaction check.
var _ AgentRegistry = (*inMemoryAgentRegistry)(nil)
