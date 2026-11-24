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
	"strconv"
	"strings"
)

// =============================================================================
// Resolving a reference to the Agents a spawn may use, in order
// =============================================================================
//
// SELECTION IS FIRST-FIT BY PREFERENCE, NOT LEAST-LOADED. Order is the
// operator's policy — cheapest or most private first — and picking the
// least-loaded Agent would silently invert it, sending work to the expensive
// provider whenever the preferred one had any load at all. First-fit keeps the
// declared order and uses load only to decide when an Agent is FULL.

// Kinds and slots an AgentUsage reports.
const (
	// AgentUsageKindPool is a pool listing the Agent.
	AgentUsageKindPool = "pool"

	// AgentUsageKindBinding is an SDLC project binding.
	AgentUsageKindBinding = "binding"

	// AgentUsageKindProject is an orchestration project record.
	AgentUsageKindProject = "project"

	// AgentUsageKindConfig is the node configuration.
	AgentUsageKindConfig = "config"

	// AgentUsageSlotMember is a pool's member list.
	AgentUsageSlotMember = "member"

	// AgentUsageSlotDefaultLead is a project's default lead.
	AgentUsageSlotDefaultLead = "default_lead"

	// AgentUsageSlotWorker is a binding's coding slot.
	AgentUsageSlotWorker = "worker"

	// AgentUsageSlotTriage is a triage slot.
	AgentUsageSlotTriage = "triage"

	// AgentUsageSlotReviewer is a review seat; the seat's name follows a
	// colon ("reviewer:default").
	AgentUsageSlotReviewer = "reviewer"

	// AgentUsageSlotLead is a lead slot.
	AgentUsageSlotLead = "lead"
)

// AgentCatalogue is the set of Agents and pools in force for a tenant.
type AgentCatalogue struct {
	Agents map[string]Agent
	Pools  map[string]AgentPool
}

// Agent returns one Agent by ID.
func (c AgentCatalogue) Agent(id string) (Agent, bool) {
	agent, ok := c.Agents[strings.TrimSpace(id)]
	return agent, ok
}

// Candidates returns the Agents a reference resolves to, in failover order:
// one for an Agent reference, the members for a pool.
func (c AgentCatalogue) Candidates(ref AgentRef) ([]Agent, error) {
	if err := RequireAgentRef(ref, "agent_ref"); err != nil {
		return nil, err
	}
	if !ref.IsPool() {
		agent, ok := c.Agent(ref.AgentID)
		if !ok {
			return nil, &UnknownAgentError{ID: strings.TrimSpace(ref.AgentID)}
		}
		return []Agent{agent}, nil
	}
	poolID := strings.TrimSpace(ref.PoolID)
	pool, ok := c.Pools[poolID]
	if !ok {
		return nil, &UnknownAgentPoolError{ID: poolID}
	}
	candidates := make([]Agent, 0, len(pool.Agents))
	for _, id := range pool.Agents {
		agent, ok := c.Agent(id)
		if !ok {
			return nil, &UnknownAgentError{ID: id, Pool: poolID}
		}
		candidates = append(candidates, agent)
	}
	return candidates, nil
}

// CheckRef refuses a reference that names nothing defined, or an Agent that
// does not declare role. A pool is checked member by member: a pool bound to a
// role slot fails over only to Agents meant for that role. An empty role
// checks existence only. A zero reference is not checked; a slot that requires
// one uses RequireAgentRef first.
func (c AgentCatalogue) CheckRef(ref AgentRef, field string, role AgentRole) error {
	if ref.IsZero() {
		return nil
	}
	if err := ref.Validate(field); err != nil {
		return err
	}
	candidates, err := c.Candidates(ref)
	if err != nil {
		return err
	}
	if role == "" {
		return nil
	}
	for _, agent := range candidates {
		if !agent.HasRole(role) {
			return &AgentRoleMismatchError{Field: field, AgentID: agent.ID, Role: role}
		}
	}
	return nil
}

// AgentResolver decides which of a reference's Agents may take work now.
type AgentResolver struct {
	load   ProfileLoadReader
	demand RoleDemandReader
}

// NewAgentResolver builds a resolver. A nil load reader is allowed and makes
// every Agent look idle, which is the correct degradation for a deployment
// with no lease store: preference order still holds, and the concurrency cap
// still refuses what would overrun.
func NewAgentResolver(load ProfileLoadReader) *AgentResolver {
	return &AgentResolver{load: load}
}

// WithRoleDemand returns a resolver that can also see what each role has
// QUEUED, which is what lets an Agent yield capacity its preferred role is not
// asking for.
func (r *AgentResolver) WithRoleDemand(demand RoleDemandReader) *AgentResolver {
	if r == nil {
		return nil
	}
	clone := *r
	clone.demand = demand
	return &clone
}

// Admit filters candidates to those with capacity that will take work of role,
// keeping their order, and records why each other one was passed over.
//
// THE TWO TESTS ARE SEPARATE AND BOTH MUST PASS. Capacity is arithmetic about
// what is running; placement is a statement about what the Agent is FOR. An
// Agent with a free slot can still decline — it is being held for review, and
// the triage job in front of it should spill to the next one.
func (r *AgentResolver) Admit(ctx context.Context, tenantID string, candidates []Agent, role AgentRole) ([]Agent, []AgentAttempt, error) {
	admitted := make([]Agent, 0, len(candidates))
	var passed []AgentAttempt
	for _, agent := range candidates {
		live := 0
		if agent.MaxConcurrent > 0 {
			var err error
			live, err = r.liveFor(ctx, tenantID, agent.Backend)
			if err != nil {
				return nil, nil, err
			}
			if live >= agent.MaxConcurrent {
				passed = append(passed, AgentAttempt{
					AgentID: agent.ID, Reason: AgentFailoverAtCapacity,
					Detail: strconv.Itoa(live) + "/" + strconv.Itoa(agent.MaxConcurrent) + " jobs live",
				})
				continue
			}
		}
		if ok, why := agent.Placement.admits(ctx, r.demand, tenantID, role, agent.MaxConcurrent, live); !ok {
			passed = append(passed, AgentAttempt{AgentID: agent.ID, Reason: AgentFailoverHeldForRole, Detail: why})
			continue
		}
		admitted = append(admitted, agent)
	}
	return admitted, passed, nil
}

func (r *AgentResolver) liveFor(ctx context.Context, tenantID, backend string) (int, error) {
	if r == nil || r.load == nil {
		return 0, nil
	}
	return r.load.LiveJobsForBackend(ctx, tenantID, backend)
}

// =============================================================================
// Failover classification
// =============================================================================

// Why a spawn moved past one Agent to the next.
const (
	// AgentFailoverAccountExhausted: no pooled account could be used — none
	// defined, all at their session cap, all spent, all revoked or errored.
	AgentFailoverAccountExhausted = "account_exhausted"

	// AgentFailoverAccountExpired: the account's credential expired or could
	// not be refreshed.
	AgentFailoverAccountExpired = "account_expired"

	// AgentFailoverRateLimited: the provider rate- or usage-limited the
	// account, or every candidate account is cooling down from one.
	AgentFailoverRateLimited = "rate_limited"

	// AgentFailoverBackendUnavailable: no node serves the Agent's backend.
	AgentFailoverBackendUnavailable = "backend_unavailable"

	// AgentFailoverAtCapacity: the Agent's backend is at MaxConcurrent.
	AgentFailoverAtCapacity = "at_capacity"

	// AgentFailoverHeldForRole: the Agent's placement holds it for another
	// role.
	AgentFailoverHeldForRole = "held_for_role"
)

// agentFailoverRule maps one refusal to the failover reason it is.
type agentFailoverRule struct {
	cause  error
	reason string
}

// agentFailoverRules are the refusals that move a spawn to the next Agent.
// Anything else — a malformed request, a refused workspace, a policy denial —
// would refuse the next Agent identically, so it stops the walk instead.
var agentFailoverRules = []agentFailoverRule{
	{ErrAccountLimited, AgentFailoverRateLimited},
	{ErrAccountAuthRejected, AgentFailoverAccountExpired},
	{ErrAccountsCoolingDown, AgentFailoverRateLimited},
	{ErrAccountCredentialExpiring, AgentFailoverAccountExpired},
	{ErrAccountRefreshFailed, AgentFailoverAccountExpired},
	{ErrNoAccountAvailable, AgentFailoverAccountExhausted},
	{ErrAccountsSaturated, AgentFailoverAccountExhausted},
	{ErrAccountsSpent, AgentFailoverAccountExhausted},
	{ErrAccountRevoked, AgentFailoverAccountExhausted},
	{ErrAccountErrored, AgentFailoverAccountExhausted},
	{ErrAccountCredentialNotFound, AgentFailoverAccountExhausted},
	{ErrBackendNotFound, AgentFailoverBackendUnavailable},
	{ErrNoMatchingFleetNode, AgentFailoverBackendUnavailable},
	{ErrAgentSpendExhausted, AgentFailoverOverBudget},
}

// AgentFailoverReason reports whether err is a refusal the next Agent of a
// reference might not share, and which. False means failing over is
// pointless: the next Agent would be refused the same way.
func AgentFailoverReason(err error) (string, bool) {
	if err == nil {
		return "", false
	}
	for _, rule := range agentFailoverRules {
		if errors.Is(err, rule.cause) {
			return rule.reason, true
		}
	}
	return "", false
}

// agentFailoverResourceID renders the audit resource of one failover: the
// Agent passed over, the reason, and the Agent that is tried next.
func agentFailoverResourceID(ref AgentRef, attempt AgentAttempt, next string) string {
	id := ref.String() + ": " + attempt.AgentID + " -> " + next + " (" + attempt.Reason + ")"
	return id
}
