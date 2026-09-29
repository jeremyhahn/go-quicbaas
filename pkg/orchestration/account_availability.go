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
	"slices"
	"strconv"
	"strings"
	"time"
)

// =============================================================================
// Account availability — can an Agent draw a usable account at all?
//
// A caller about to create work whose ONLY possible outcome is a failed spawn
// asks this first. The review dispatcher is the case that motivated it: with
// the reviewer's only codex login dead, every sweep started a review job that
// failed at resolution, every five minutes, for as long as nobody re-logged it
// (go-codec watcher, 2026-09-28).
//
// "USABLE" IS NARROWER THAN "HAS HEADROOM". An account that is merely busy (at
// its session cap) or cooling down from a limit returns on its own, and the
// work waits for it the way it always has, through the resolver's own
// saturated and cooling-down refusals. What makes work pointless is a pool
// where no account CAN serve: every account errored, revoked, or refused by
// its spend window. Those are the accounts this reports on.
//
// It is read-only and reuses the admission reader's pool assembly (the
// project's accounts plus the tenant-wide ones it inherits), so "usable" here
// and "in the pool" at admission are the same set.
// =============================================================================

// AccountAvailability is whether an Agent can draw a usable pooled account.
type AccountAvailability struct {
	// Usable reports that at least one account could serve a session now or
	// once it returns by itself, or that the Agent draws on no pool at all.
	Usable bool `json:"usable"`

	// Reason says why nothing is usable, for the record the caller keeps and
	// for a person reading it. Empty when Usable.
	Reason string `json:"reason,omitempty"`
}

// AccountAvailabilityQuery names the pool one Agent draws on.
type AccountAvailabilityQuery struct {
	TenantID  string
	ProjectID string
	Provider  AccountProvider

	// Allow narrows the pool to these accounts, as an Agent's account policy
	// does. Empty means the whole pool.
	Allow []string
}

// AccountAvailabilityReader answers AccountAvailability for one pool.
type AccountAvailabilityReader interface {
	AccountAvailability(ctx context.Context, query AccountAvailabilityQuery, now time.Time) (AccountAvailability, error)
}

// Compile-time interface satisfaction check.
var _ AccountAvailabilityReader = (*accountPoolHeadroom)(nil)

// AccountAvailability implements AccountAvailabilityReader over the
// admission reader's pool.
func (h *accountPoolHeadroom) AccountAvailability(ctx context.Context, query AccountAvailabilityQuery, now time.Time) (AccountAvailability, error) {
	if query.TenantID == "" {
		return AccountAvailability{}, &AdmissionConfigError{Field: "tenant_id", Message: "a pool is scoped to one tenant"}
	}
	tenantCtx := ContextWithTenant(ctx, query.TenantID)
	pooled, err := h.pool(tenantCtx, query.TenantID, query.ProjectID)
	if err != nil {
		return AccountAvailability{}, err
	}
	var relogin, errored, revoked, spent int
	candidates := 0
	for _, account := range pooled {
		if account.Provider != query.Provider || (len(query.Allow) > 0 && !slices.Contains(query.Allow, account.ID)) {
			continue
		}
		candidates++
		switch account.Status {
		case AccountStatusRevoked:
			revoked++
			continue
		case AccountStatusError:
			errored++
			if strings.HasPrefix(account.LastError, AccountReloginNeededPrefix) {
				relogin++
			}
			continue
		}
		// A limit hold returns by itself at its reset: usable, and the
		// resolver's own cooldown refusal is what the work waits on.
		if account.Status == AccountStatusLimited && account.LimitHoldActive(now) {
			return AccountAvailability{Usable: true}, nil
		}
		decision, admitErr := h.quota.Admit(ctx, QuotaAdmitRequest{Account: account, Policy: account.Spend.Quota}, now)
		if admitErr != nil {
			return AccountAvailability{}, admitErr
		}
		if decision.Allowed {
			return AccountAvailability{Usable: true}, nil
		}
		spent++
	}
	if candidates == 0 {
		return AccountAvailability{Reason: "no pooled " + string(query.Provider) + " account is defined for this project"}, nil
	}
	return AccountAvailability{Reason: unusablePoolReason(query.Provider, relogin, errored, revoked, spent)}, nil
}

// unusablePoolReason says, most actionable first, why no account can serve.
func unusablePoolReason(provider AccountProvider, relogin, errored, revoked, spent int) string {
	var parts []string
	if relogin > 0 {
		parts = append(parts, "re-login needed on "+strconv.Itoa(relogin))
	}
	if other := errored - relogin; other > 0 {
		parts = append(parts, strconv.Itoa(other)+" with a credential error")
	}
	if revoked > 0 {
		parts = append(parts, strconv.Itoa(revoked)+" revoked")
	}
	if spent > 0 {
		parts = append(parts, strconv.Itoa(spent)+" over budget")
	}
	return "no usable " + string(provider) + " account: " + strings.Join(parts, ", ")
}

// AgentAccountAvailabilityReader is the agent service's availability verb, as
// a caller that creates work on an Agent (the review dispatcher) asks it.
type AgentAccountAvailabilityReader interface {
	AgentAccountAvailability(ctx context.Context, tenantID, projectID string, ref AgentRef, role AgentRole) (AccountAvailability, error)
	SetAccountAvailability(reader AccountAvailabilityReader)
}

// Compile-time interface satisfaction check: the server wires the service by
// asserting this, so a signature drift fails the build instead of silently
// skipping the wiring.
var _ AgentAccountAvailabilityReader = (*agentServiceImpl)(nil)

// SetAccountAvailability installs the reader AgentAccountAvailability asks.
// Without one every Agent is reported usable, which is how a deployment with
// no admission reader has always dispatched.
func (s *agentServiceImpl) SetAccountAvailability(reader AccountAvailabilityReader) {
	s.accountAvailability = reader
}

// AgentAccountAvailability answers whether the Agent (or any Agent of the
// pool) ref names can draw a usable pooled account for projectID in role.
//
// ONE USABLE AGENT IS ENOUGH, because a spawn walks the reference and fails
// over (spawnByAgentRef). An Agent that draws on no pool — a local model — is
// usable by definition. The reason returned is the last Agent's, which for a
// single Agent is its own.
func (s *agentServiceImpl) AgentAccountAvailability(ctx context.Context, tenantID, projectID string, ref AgentRef, role AgentRole) (AccountAvailability, error) {
	if s.accountAvailability == nil || s.agentDirectory == nil || ref.IsZero() {
		return AccountAvailability{Usable: true}, nil
	}
	catalogue, err := s.agentDirectory.store.Catalogue(ctx, tenantID)
	if err != nil {
		return AccountAvailability{}, err
	}
	agents, err := catalogue.Candidates(ref)
	if err != nil {
		return AccountAvailability{}, err
	}
	verdict := AccountAvailability{Usable: true}
	now := time.Now().UTC()
	for _, agent := range agents {
		if !agentDrawsOnPool(agent) {
			return AccountAvailability{Usable: true}, nil
		}
		verdict, err = s.accountAvailability.AccountAvailability(ctx, AccountAvailabilityQuery{
			TenantID:  tenantID,
			ProjectID: projectID,
			Provider:  accountProviderForBackend(agent.Backend),
			Allow:     agent.Account.Accounts,
		}, now)
		if err != nil {
			return AccountAvailability{}, err
		}
		if verdict.Usable {
			return verdict, nil
		}
	}
	return verdict, nil
}

// agentDrawsOnPool reports whether a spawn as agent resolves a pooled account,
// by the same rules its spawn applies (forAgent, ApplyWorkerSpawnEnvironment).
func agentDrawsOnPool(agent Agent) bool {
	return WorkerSpawnAccountAuto(agent.Backend, agent.Model) ||
		strings.TrimSpace(agent.Account.Strategy) != "" || len(agent.Account.Accounts) > 0
}

// accountProviderForBackend is the provider whose accounts a backend draws on.
// The claude-code alias runs the claude CLI.
func accountProviderForBackend(backend string) AccountProvider {
	if backend == "claude-code" {
		return AccountProviderClaude
	}
	return AccountProvider(backend)
}
