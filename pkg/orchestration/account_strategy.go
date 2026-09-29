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

	// LiveSessions is each candidate's live-session count as the resolver
	// already measured it while applying the session cap, so ranking reads
	// the same count the cap was checked against. Nil tells a strategy to
	// measure for itself.
	LiveSessions map[string]int
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
// LeastUsageAccountStrategy — spread sessions across the pool: the least busy
// account now, then the least used.
//
// The ranking, most significant first:
//
//  1. LIVE SESSIONS (AccountSessionLookup). An account serving nothing beats
//     one serving two, whatever their history. This is what spreads concurrent
//     work: without it every spawn in a burst lands on whichever account the
//     usage counters currently favour.
//  2. RECORDED USAGE: cumulative tokens attributed to the account through
//     CostTracker.GetAccountUsage. Tokens, not spend or request count, because
//     tokens are what the cost-tracking subsystem measures (nothing prices a
//     turn, go-quicbaas#791), and request count would treat a one-line prompt
//     and an hour-long agentic session as equal load. This spreads SERIAL work:
//     the account that just ran a turn has more recorded than the ones that
//     did not.
//  3. THE DEFAULT ACCOUNT. `default: true` means preferred among equals,
//     never "always". It breaks a tie that nothing measured could break.
//  4. LEAST RECENTLY USED (Account.LastUsedAt, where zero is "never"), so a
//     pool whose counters agree still rotates instead of thrashing on one.
//  5. Account.ID, so the answer is deterministic under a total tie.
//
// An account at its MaxConcurrentSessions cap is not a candidate at all. A
// pool whose every account is capped answers AccountsSaturatedError, which is
// a busy pool rather than an empty one.
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

// LeastUsageAccountStrategy selects the least busy, then least used, candidate.
// See the section comment for the full ranking.
type LeastUsageAccountStrategy struct {
	// Usage answers the recorded-usage term. Required.
	Usage AccountUsageLookup

	// Sessions answers the live-session term and the session cap. Nil ranks
	// every account as serving nothing and enforces no cap, which is how a
	// deployment with no session store behaves.
	Sessions AccountSessionLookup
}

// Name implements AccountSelectionStrategy.
func (LeastUsageAccountStrategy) Name() string { return LeastUsageAccountStrategyName }

// accountRank is one candidate's position in the ranking.
type accountRank struct {
	account *Account
	live    int
	tokens  int64
}

// before reports whether r ranks ahead of other.
func (r accountRank) before(other accountRank) bool {
	switch {
	case r.live != other.live:
		return r.live < other.live
	case r.tokens != other.tokens:
		return r.tokens < other.tokens
	case r.account.Default != other.account.Default:
		return r.account.Default
	case !r.account.LastUsedAt.Equal(other.account.LastUsedAt):
		return r.account.LastUsedAt.Before(other.account.LastUsedAt)
	default:
		return r.account.ID < other.account.ID
	}
}

// Select implements AccountSelectionStrategy.
func (s LeastUsageAccountStrategy) Select(ctx context.Context, candidates []*Account, req AccountSelectionRequest) (*Account, error) {
	if len(candidates) == 0 || s.Usage == nil {
		return nil, &NoAccountAvailableError{TenantID: req.TenantID, ProjectID: req.ProjectID, Provider: req.Provider, Strategy: LeastUsageAccountStrategyName}
	}

	live, err := s.liveSessions(ctx, candidates, req)
	if err != nil {
		return nil, err
	}

	var (
		best   *accountRank
		capped []*Account
	)
	for _, c := range candidates {
		if c.AtSessionCap(live[c.ID]) {
			capped = append(capped, c)
			continue
		}
		tokens, usageErr := s.Usage.AccountTokens(ctx, req.TenantID, c.ID)
		if usageErr != nil {
			// A usage-lookup failure for one candidate must not abort
			// selection for the whole pool; treat it as "no usage seen yet"
			// so a fresh account is still selectable.
			tokens = 0
		}
		rank := accountRank{account: c, live: live[c.ID], tokens: tokens}
		if best == nil || rank.before(*best) {
			best = &rank
		}
	}
	if best == nil {
		return nil, &AccountsSaturatedError{
			TenantID: req.TenantID, ProjectID: req.ProjectID, Provider: req.Provider,
			Candidates: len(capped), Occupancy: sessionCapSummary(capped, live),
		}
	}
	return best.account, nil
}

// liveSessions reads the live-session count for every candidate, or an empty
// count when no lookup is wired.
func (s LeastUsageAccountStrategy) liveSessions(ctx context.Context, candidates []*Account, req AccountSelectionRequest) (map[string]int, error) {
	if req.LiveSessions != nil {
		return req.LiveSessions, nil
	}
	if s.Sessions == nil {
		return map[string]int{}, nil
	}
	ids := make([]string, 0, len(candidates))
	for _, c := range candidates {
		ids = append(ids, c.ID)
	}
	// FAIL CLOSED. A count that could not be read is not zero; see
	// AccountSessionCountError.
	return s.Sessions.LiveSessions(ctx, req.TenantID, ids)
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

// NewBalancedAccountStrategyRegistry returns the production registry:
// LeastUsageAccountStrategy, ranking on live sessions and recorded usage, is
// the DEFAULT, and StaticAccountStrategy stays registered for a caller that
// names it.
//
// The default is the point. With static as the default every unpinned spawn
// used the scope's default account, and a pool of several accounts was a pool
// of one until the provider refused it (go-quicbaas#1046).
func NewBalancedAccountStrategyRegistry(usage AccountUsageLookup, sessions AccountSessionLookup) (*AccountStrategyRegistry, error) {
	switch {
	case usage == nil:
		return nil, &AccountInvalidError{Field: "usage", Message: "balancing ranks on recorded usage, which needs a usage lookup"}
	case sessions == nil:
		return nil, &AccountInvalidError{Field: "sessions", Message: "balancing ranks on live sessions, which needs a session lookup"}
	}
	r := NewAccountStrategyRegistry(nil)
	r.Register(LeastUsageAccountStrategy{Usage: usage, Sessions: sessions})
	r.SetDefault(LeastUsageAccountStrategyName)
	return r, nil
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
