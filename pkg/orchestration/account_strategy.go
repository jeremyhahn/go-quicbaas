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

import "context"

// =============================================================================
// AccountSelectionStrategy — pluggable "which account should this spawn use"
// policy.
//
// A strategy never talks to the KV or SecretsManager directly: it receives
// the already-loaded candidate list and returns one of them (or an error).
// This keeps a strategy pure, trivially unit-testable, and free to be a
// drop-in (round-robin, quota-aware, priority-tiered, ...) without touching
// AccountResolver's plumbing.
// =============================================================================

// AccountSelectionRequest describes the spawn a strategy is choosing an
// account for.
type AccountSelectionRequest struct {
	TenantID  string
	ProjectID string
	Provider  AccountProvider

	// PinnedAccountID, when set, is not resolved through a strategy at all —
	// AccountResolver short-circuits straight to that account. It is carried
	// here only so a strategy implementation can see it for logging/metrics;
	// StaticAccountStrategy is the one strategy that also treats it as an
	// input.
	PinnedAccountID string
}

// AccountSelectionStrategy chooses one account from a set of eligible
// candidates (already filtered to Status == AccountStatusActive by
// AccountResolver).
type AccountSelectionStrategy interface {
	// Name identifies the strategy for observability (recorded on the
	// resolved credential and audit event so a cost anomaly can be traced
	// back to which policy picked the account).
	Name() string

	// Select returns one candidate, or NoAccountAvailableError if none is
	// eligible.
	Select(ctx context.Context, candidates []*Account, req AccountSelectionRequest) (*Account, error)
}

// =============================================================================
// StaticAccountStrategy — "use account X", pinned per request or falling
// back to the tenant/project/provider Default account.
// =============================================================================

// StaticAccountStrategyName is the registry key for StaticAccountStrategy.
const StaticAccountStrategyName = "static"

// StaticAccountStrategy selects req.PinnedAccountID when present, otherwise
// the candidate with Default == true, otherwise the single remaining
// candidate when there is exactly one. It never guesses among multiple
// undesignated candidates — ambiguity is an error, not a silent pick.
type StaticAccountStrategy struct{}

// Name implements AccountSelectionStrategy.
func (StaticAccountStrategy) Name() string { return StaticAccountStrategyName }

// Select implements AccountSelectionStrategy.
func (StaticAccountStrategy) Select(_ context.Context, candidates []*Account, req AccountSelectionRequest) (*Account, error) {
	if req.PinnedAccountID != "" {
		for _, c := range candidates {
			if c.ID == req.PinnedAccountID {
				return c, nil
			}
		}
		return nil, &NoAccountAvailableError{TenantID: req.TenantID, ProjectID: req.ProjectID, Provider: req.Provider, Strategy: StaticAccountStrategyName}
	}
	for _, c := range candidates {
		if c.Default {
			return c, nil
		}
	}
	if len(candidates) == 1 {
		return candidates[0], nil
	}
	return nil, &NoAccountAvailableError{TenantID: req.TenantID, ProjectID: req.ProjectID, Provider: req.Provider, Strategy: StaticAccountStrategyName}
}

// =============================================================================
// LeastUsageAccountStrategy — pick the candidate with the lowest recorded
// usage.
//
// "Usage" is defined as CUMULATIVE TOKENS attributed to the account via
// CostTracker.GetAccountUsage (RecordTurnUsage / RecordAccountUsage — see
// cost_tracker.go). Tokens, not spend or request count, because tokens are
// the only unit the existing cost-tracking subsystem actually measures today
// (there is no per-model dollar-cost table in this codebase, and request
// count would treat a one-line prompt and an hour-long agentic session as
// equal load). This makes LeastUsageAccountStrategy a genuine load-balancer
// across a pool of otherwise-equivalent accounts (e.g. five Claude Max
// seats), not a cost-minimizer.
// =============================================================================

// LeastUsageAccountStrategyName is the registry key for
// LeastUsageAccountStrategy.
const LeastUsageAccountStrategyName = "least_usage"

// AccountUsageLookup is the read-only usage query LeastUsageAccountStrategy
// depends on. CostTracker satisfies it via GetAccountUsage.
type AccountUsageLookup interface {
	// AccountTokens returns the cumulative tokens recorded against
	// tenantID/accountID. An account with no recorded usage returns (0, nil).
	AccountTokens(ctx context.Context, tenantID, accountID string) (int64, error)
}

// LeastUsageAccountStrategy selects the candidate with the fewest cumulative
// tokens recorded. Ties are broken by Account.ID ordering (stable, decoupled
// from map/slice iteration order) so repeated selection under a tie does not
// thrash between two accounts.
type LeastUsageAccountStrategy struct {
	Usage AccountUsageLookup
}

// Name implements AccountSelectionStrategy.
func (LeastUsageAccountStrategy) Name() string { return LeastUsageAccountStrategyName }

// Select implements AccountSelectionStrategy.
func (s LeastUsageAccountStrategy) Select(ctx context.Context, candidates []*Account, req AccountSelectionRequest) (*Account, error) {
	if len(candidates) == 0 {
		return nil, &NoAccountAvailableError{TenantID: req.TenantID, ProjectID: req.ProjectID, Provider: req.Provider, Strategy: LeastUsageAccountStrategyName}
	}
	if s.Usage == nil {
		return nil, &NoAccountAvailableError{TenantID: req.TenantID, ProjectID: req.ProjectID, Provider: req.Provider, Strategy: LeastUsageAccountStrategyName}
	}

	var best *Account
	var bestTokens int64
	for _, c := range candidates {
		tokens, err := s.Usage.AccountTokens(ctx, req.TenantID, c.ID)
		if err != nil {
			// A usage-lookup failure for one candidate must not abort
			// selection for the whole pool; treat it as "no usage seen yet"
			// so a fresh account is still selectable.
			tokens = 0
		}
		if best == nil || tokens < bestTokens || (tokens == bestTokens && c.ID < best.ID) {
			best = c
			bestTokens = tokens
		}
	}
	return best, nil
}

// =============================================================================
// AccountStrategyRegistry — pluggable strategy lookup by name.
// =============================================================================

// AccountStrategyRegistry maps a strategy name to its implementation. New
// strategies (round-robin, quota-aware, priority-tiered) register themselves
// without AccountResolver changing at all.
type AccountStrategyRegistry struct {
	strategies map[string]AccountSelectionStrategy
	defaultKey string
}

// NewAccountStrategyRegistry returns a registry pre-populated with
// StaticAccountStrategy (the default) and, when usage is non-nil,
// LeastUsageAccountStrategy.
func NewAccountStrategyRegistry(usage AccountUsageLookup) *AccountStrategyRegistry {
	r := &AccountStrategyRegistry{
		strategies: make(map[string]AccountSelectionStrategy),
		defaultKey: StaticAccountStrategyName,
	}
	r.Register(StaticAccountStrategy{})
	if usage != nil {
		r.Register(LeastUsageAccountStrategy{Usage: usage})
	}
	return r
}

// Register adds or replaces a named strategy.
func (r *AccountStrategyRegistry) Register(s AccountSelectionStrategy) {
	if r.strategies == nil {
		r.strategies = make(map[string]AccountSelectionStrategy)
	}
	r.strategies[s.Name()] = s
}

// SetDefault changes which registered strategy Get("") returns.
func (r *AccountStrategyRegistry) SetDefault(name string) {
	r.defaultKey = name
}

// Get returns the named strategy, or the registry default when name is
// empty. Returns false when the name is not registered.
func (r *AccountStrategyRegistry) Get(name string) (AccountSelectionStrategy, bool) {
	if name == "" {
		name = r.defaultKey
	}
	s, ok := r.strategies[name]
	return s, ok
}
