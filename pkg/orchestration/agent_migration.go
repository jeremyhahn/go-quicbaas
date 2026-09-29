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
	"strconv"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// =============================================================================
// Boot migration: free-string agent tuples become Agents
// =============================================================================
//
// PRE-V1, ONE WIRE FORMAT, STORED DATA MIGRATED ONCE AT BOOT. A binding written
// before Agents existed names its worker, reviewer and triage as a backend, a
// model and an image; the migration turns each distinct tuple into one Agent
// of the tenant and points the slot at it. It is idempotent by construction: a
// record that already holds references carries no tuple to migrate.
//
// The migration writes through the STORE, not the directory. At boot the fleet
// nodes serving the backends may not have registered yet, and the live-catalog
// check an operator's write goes through would refuse an Agent whose backend
// is merely not up yet.

// ErrAgentSeed indicates a tuple could not become an Agent.
var ErrAgentSeed = errors.New("agent migration: tuple cannot become an agent")

// AgentSeedError reports a tuple the migration could not turn into an Agent,
// and why. The slot is left for an operator.
type AgentSeedError struct {
	Backend string
	Role    AgentRole
	Reason  string
}

// Error implements the error interface.
func (e *AgentSeedError) Error() string {
	return ErrAgentSeed.Error() + ": " + e.Role.String() + " on backend " + strconv.Quote(e.Backend) + ": " + e.Reason
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *AgentSeedError) Unwrap() error { return ErrAgentSeed }

// AgentTuple is a legacy slot's runtime, model, image and effort.
type AgentTuple struct {
	Backend         string
	Model           string
	Image           string
	ReasoningEffort string
}

// IsZero reports whether the tuple names no backend: a slot that was never
// configured.
func (t AgentTuple) IsZero() bool { return strings.TrimSpace(t.Backend) == "" }

// DefaultModelForBackend is the model a legacy tuple that named none ran on:
// the backend's own default, which for the Claude runtimes is
// DefaultClaudeModel and otherwise the first model of the backend's curated
// catalog. False when the backend has no known default — a tuple like that
// cannot become an Agent, because an Agent always names its model.
func DefaultModelForBackend(backend string) (string, bool) {
	switch strings.TrimSpace(backend) {
	case "claude", "claude-code":
		return DefaultClaudeModel, true
	}
	if models := acpModelCatalog[strings.TrimSpace(backend)]; len(models) > 0 {
		return models[0], true
	}
	return "", false
}

// agentRoleNouns name a seeded Agent after the role it was seeded for.
var agentRoleNouns = map[AgentRole]string{
	AgentRoleCoding: "coder", AgentRoleReview: "reviewer", AgentRoleTriage: "triage",
	AgentRoleLead: "lead", AgentRoleProjectManager: "project manager", AgentRoleGeneral: "agent",
}

// backendDisplayNames are how a seeded Agent's name spells a known backend.
var backendDisplayNames = map[string]string{
	"claude": "Claude", "claude-code": "Claude", "codex": "Codex", "opencode": "OpenCode",
	"openvino": "OpenVINO", "ollama": "Ollama", "ollama-agent": "Ollama", "vllm": "vLLM",
}

// AgentSeeder turns legacy tuples into a tenant's Agents, one Agent per
// distinct tuple, reusing an Agent the tenant already has for the same tuple.
type AgentSeeder struct {
	store    AgentCatalogStore
	tenantID string
	agents   map[string]Agent
	byTuple  map[AgentTuple]string

	// Created are the IDs of the Agents this seeder created, in order.
	Created []string
}

// NewAgentSeeder loads tenantID's catalog so seeding reuses what exists.
func NewAgentSeeder(ctx context.Context, store AgentCatalogStore, tenantID string) (*AgentSeeder, error) {
	catalogue, err := store.Catalogue(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	seeder := &AgentSeeder{store: store, tenantID: tenantID, agents: catalogue.Agents, byTuple: map[AgentTuple]string{}}
	for _, id := range sortedKeys(catalogue.Agents) {
		agent := catalogue.Agents[id]
		tuple := AgentTuple{Backend: agent.Backend, Model: agent.Model, Image: agent.Image, ReasoningEffort: agent.ReasoningEffort}
		if _, seen := seeder.byTuple[tuple]; !seen {
			seeder.byTuple[tuple] = id
		}
	}
	return seeder, nil
}

// Seed returns the ID of the Agent tuple becomes for role: an existing Agent
// with the same tuple (gaining role if it lacked it), or a new one.
func (s *AgentSeeder) Seed(ctx context.Context, tuple AgentTuple, role AgentRole) (string, error) {
	tuple.Backend = strings.TrimSpace(tuple.Backend)
	if tuple.Backend == "" {
		return "", &AgentSeedError{Role: role, Reason: "the slot names no backend"}
	}
	if strings.TrimSpace(tuple.Model) == "" {
		model, ok := DefaultModelForBackend(tuple.Backend)
		if !ok {
			return "", &AgentSeedError{Backend: tuple.Backend, Role: role, Reason: "it named no model and the backend has no known default"}
		}
		tuple.Model = model
	}
	if id, ok := s.byTuple[tuple]; ok {
		return id, s.ensureRole(ctx, id, role)
	}
	agent := Agent{
		ID:              s.uniqueID(slugify(tuple.Backend) + "-" + slugify(agentRoleNouns[role])),
		Name:            displayBackend(tuple.Backend) + " " + agentRoleNouns[role],
		Description:     "Seeded from a " + role.String() + " slot that named backend " + tuple.Backend + " before Agents existed.",
		Roles:           []AgentRole{role},
		Backend:         tuple.Backend,
		Model:           tuple.Model,
		Image:           tuple.Image,
		ReasoningEffort: tuple.ReasoningEffort,
	}
	if err := s.store.CreateAgent(ctx, s.tenantID, &agent); err != nil {
		return "", err
	}
	s.agents[agent.ID] = agent
	s.byTuple[tuple] = agent.ID
	s.Created = append(s.Created, agent.ID)
	return agent.ID, nil
}

// ensureRole adds role to an Agent that lacks it.
func (s *AgentSeeder) ensureRole(ctx context.Context, id string, role AgentRole) error {
	if agent := s.agents[id]; agent.HasRole(role) {
		return nil
	}
	_, err := s.store.ModifyAgent(ctx, s.tenantID, id, func(agent *Agent) bool {
		if agent.HasRole(role) {
			return false
		}
		agent.Roles = append(agent.Roles, role)
		return true
	})
	if err != nil {
		return err
	}
	agent := s.agents[id]
	agent.Roles = append(agent.Roles, role)
	s.agents[id] = agent
	return nil
}

// uniqueID returns base, or base with the first free numeric suffix.
func (s *AgentSeeder) uniqueID(base string) string {
	if len(base) > maxCatalogID-3 {
		base = strings.TrimRight(base[:maxCatalogID-3], "-._")
	}
	id := base
	for n := 2; ; n++ {
		if _, taken := s.agents[id]; !taken {
			return id
		}
		id = base + "-" + strconv.Itoa(n)
	}
}

// slugify lowercases text and keeps only catalog-ID characters, joining runs
// of anything else with a single '-'.
func slugify(text string) string {
	var b strings.Builder
	dash := false
	for _, c := range strings.ToLower(strings.TrimSpace(text)) {
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '.', c == '_':
			b.WriteRune(c)
			dash = false
		case !dash && b.Len() > 0:
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.Trim(b.String(), "-._")
}

// displayBackend spells a backend for an Agent's name.
func displayBackend(backend string) string {
	if name, ok := backendDisplayNames[backend]; ok {
		return name
	}
	return strings.ToUpper(backend[:1]) + backend[1:]
}

// =============================================================================
// Project default leads
// =============================================================================

// MigrateProjectLeads rewrites every project record of tenantID whose
// default_lead_agent is still the legacy free string. The string named no
// Agent — Agents did not exist — so it cannot be resolved; it is dropped, and
// the IDs of the projects that carried one are returned for the operator to
// point at an Agent. It reads and writes raw records because a legacy record
// does not decode as a Project any more.
func MigrateProjectLeads(ctx context.Context, kv interfaces.KVStore, tenantID string) ([]string, error) {
	prefix, err := tenantPrefix(projectKeyspace, normalizeStoreTenant(tenantID))
	if err != nil {
		return nil, projectKeyError(err)
	}
	type rewrite struct {
		key  string
		data []byte
	}
	var rewrites []rewrite
	var dropped []string
	scanErr := kv.Scan(ctx, prefix, func(key string, value []byte) error {
		var record map[string]json.RawMessage
		if err := json.Unmarshal(value, &record); err != nil {
			return nil
		}
		lead, ok := record["default_lead_agent"]
		if !ok || len(lead) == 0 || lead[0] != '"' {
			return nil
		}
		delete(record, "default_lead_agent")
		data, err := json.Marshal(record)
		if err != nil {
			return err
		}
		var legacy string
		if json.Unmarshal(lead, &legacy) == nil && strings.TrimSpace(legacy) != "" {
			dropped = append(dropped, idFromKey(key, prefix)+"="+legacy)
		}
		rewrites = append(rewrites, rewrite{key: key, data: data})
		return nil
	})
	if scanErr != nil {
		return nil, &ProjectStoreError{Message: "migrating project leads: " + scanErr.Error()}
	}
	for _, r := range rewrites {
		if err := kv.Put(ctx, r.key, r.data); err != nil {
			return dropped, &ProjectStoreError{Message: "migrating project leads: " + err.Error()}
		}
	}
	return dropped, nil
}
