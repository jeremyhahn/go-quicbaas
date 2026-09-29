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
	"strconv"
	"sync"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// =============================================================================
// The provider axis — can the pooled accounts serve another session
// =============================================================================
//
// ADMISSION'S THIRD INPUT IS "CAN THE POOL SERVE ANOTHER SESSION AT ALL". It
// is a question about the pooled ACCOUNTS: their provider holds, session caps
// and each account's own spend windows. A project's and a tenant's own limits
// are the fourth input, answered separately (admission_spend.go).
//
// The signals it reads all already exist and all already mean this:
//
//   - Account.Selectable — the account's lifecycle state and the AvailableAt
//     cooldown a provider refusal wrote. This is the ONE authority on whether
//     an account may be handed to a spawn; the resolver reads the same method.
//   - QuotaAuthority.Admit — the sliding windows (tokens and list-price
//     dollars) of the account's own AccountSpendPolicy, summed from the
//     consumption ledger. The same
//     authority the resolver's SpendAuthority consults before releasing a
//     credential.
//
// SO ADMISSION ASKS EXACTLY THE QUESTION THE SPAWN WILL ASK, one step earlier.
// That is the whole value: the alternative is spawning an agent, paying for a
// container start and a checkout, and having the first turn refused by the
// provider. What this cannot do is promise the answer still holds at spawn —
// nothing can — so it is an admission gate, not a reservation.
//
// AN ESTIMATE IS NOT INVENTED. The quota question is asked with zero estimated
// tokens, exactly as accountResolver.authorizeSpend asks it, because a session's
// size is no better known here than there and a made-up figure would refuse
// accounts on a number nobody measured.

// accountPoolHeadroom answers the provider axis from the account store and the
// quota authority.
type accountPoolHeadroom struct {
	accounts AccountStore
	quota    QuotaAuthority

	// sessions answers the session-cap half of "can this account serve another
	// session". Nil enforces no cap, which is how a deployment with no session
	// store behaves. See WithHeadroomSessions.
	sessions AccountSessionLookup

	// auditor and logger receive the pool-exhausted report; see report and
	// WithHeadroomReporting. The metric is always counted.
	auditor StoreAuditor
	logger  interfaces.Logger

	// reported remembers, per tenant/project, the reason its pool was last
	// reported exhausted, so a pool that stays at zero is reported once.
	reportMu sync.Mutex
	reported map[string]string
}

// PoolHeadroomOption configures NewAccountPoolHeadroom.
type PoolHeadroomOption func(*accountPoolHeadroom)

// WithHeadroomReporting routes the report of a project whose whole pool is
// unavailable at admission to the audit trail and the log, the same sinks the
// resolver reports a spawn-time exhaustion to. Either may be nil.
func WithHeadroomReporting(auditor StoreAuditor, logger interfaces.Logger) PoolHeadroomOption {
	return func(h *accountPoolHeadroom) {
		h.auditor = auditor
		h.logger = logger
	}
}

// WithHeadroomSessions makes admission read each account's live sessions
// against its MaxConcurrentSessions, from the SAME lookup the balancing
// strategy selects with. A pool whose every account is at its cap then defers
// the project, exactly as a pool whose usage windows are spent does, instead
// of dispatching a job whose spawn would find nothing to run on.
func WithHeadroomSessions(sessions AccountSessionLookup) PoolHeadroomOption {
	return func(h *accountPoolHeadroom) { h.sessions = sessions }
}

// Compile-time interface satisfaction check.
var _ PoolHeadroomReader = (*accountPoolHeadroom)(nil)

// NewAccountPoolHeadroom returns the PoolHeadroomReader backed by the tenant's
// pooled accounts and the ledger-backed quota authority.
func NewAccountPoolHeadroom(accounts AccountStore, quota QuotaAuthority, opts ...PoolHeadroomOption) (PoolHeadroomReader, error) {
	switch {
	case accounts == nil:
		return nil, &AdmissionConfigError{Field: "accounts", Message: "the pool a project draws on is read from the account store"}
	case quota == nil:
		return nil, &AdmissionConfigError{Field: "quota", Message: "a subscription's usage window is decided by the quota authority"}
	}
	h := &accountPoolHeadroom{accounts: accounts, quota: quota, reported: make(map[string]string)}
	for _, opt := range opts {
		opt(h)
	}
	return h, nil
}

// Headroom implements PoolHeadroomReader.
//
// THE POOL IS THE ONE A SPAWN WOULD DRAW ON, assembled the way
// accountResolver.selectAccount assembles it: the project's own accounts, plus
// the tenant-wide unscoped accounts a project inherits. Reading only the
// project-scoped half would defer a project that has a perfectly good
// tenant-wide account waiting for it.
//
// EVERY PROVIDER AT ONCE. A pool is asked whether it can serve "another
// session", and which provider that session will name is not known until the
// project's binding and the task's runner are resolved. Narrowing to one
// provider here would need this file to predict that, and predicting it wrongly
// would defer on a pool the spawn was never going to use.
func (h *accountPoolHeadroom) Headroom(ctx context.Context, tenantID, projectID string, now time.Time) (PoolHeadroom, error) {
	if tenantID == "" {
		return PoolHeadroom{}, &AdmissionConfigError{Field: "tenant_id", Message: "a pool is scoped to one tenant"}
	}
	tenantCtx := ContextWithTenant(ctx, tenantID)

	pooled, err := h.pool(tenantCtx, tenantID, projectID)
	if err != nil {
		return PoolHeadroom{}, err
	}
	if len(pooled) == 0 {
		// Unmetered: nobody pooled an account here, so this axis has nothing
		// to say. See PoolHeadroom.Unmetered.
		return PoolHeadroom{}, nil
	}

	live, err := h.liveSessions(tenantCtx, tenantID, pooled)
	if err != nil {
		return PoolHeadroom{}, err
	}

	headroom := PoolHeadroom{Pooled: len(pooled)}
	held, capped, unmetered := 0, 0, false
	for _, account := range pooled {
		// AcceptsNewSession, not Selectable: admission asks about a NEW
		// session, and a credential inside its expiry buffer will not be
		// handed to one. The resolver applies the same predicate.
		if !account.AcceptsNewSession(now) {
			held++
			headroom.noteRetry(account.AvailableAt)
			continue
		}
		if account.AtSessionCap(live[account.ID]) {
			capped++
			continue
		}
		decision, admitErr := h.quota.Admit(ctx, QuotaAdmitRequest{
			Account: account,
			Policy:  account.Spend.Quota,
		}, now)
		if admitErr != nil {
			// FAIL CLOSED, WITH AN ERROR AND NO ANSWER. A window that could
			// not be summed is not a window that admits: treating an
			// unreadable ledger as headroom is the fallback the quota
			// authority refuses for the same reason, one layer down.
			return PoolHeadroom{}, admitErr
		}
		if decision.Allowed {
			headroom.Ready++
			if account.MaxConcurrentSessions > 0 {
				headroom.SessionSlots += account.MaxConcurrentSessions - live[account.ID]
			} else {
				unmetered = true
			}
			continue
		}
		held++
		headroom.noteRetry(decision.RetryAt)
	}
	// Slots bound the project only when EVERY ready account is capped. One
	// unmetered account can take any number of sessions, so the sum of the
	// others' free slots says nothing about how many more the pool can serve.
	headroom.SessionBounded = headroom.Ready > 0 && !unmetered
	if !headroom.SessionBounded {
		headroom.SessionSlots = 0
	}
	if headroom.Ready == 0 {
		headroom.Reason = "none of the " + strconv.Itoa(headroom.Pooled) +
			" pooled provider account(s) has headroom: " + strconv.Itoa(held) +
			" are held by a cooldown, an expiring credential or a spent usage window, and " +
			strconv.Itoa(capped) + " are serving their maximum concurrent sessions"
		if !headroom.RetryAt.IsZero() {
			headroom.Reason += "; the first hold lifts at " + headroom.RetryAt.UTC().Format(time.RFC3339)
		}
	}
	h.report(ctx, tenantID, projectID, headroom, capped)
	return headroom, nil
}

// report tells an operator when a project's pool goes to zero at admission,
// through the same reporter the resolver uses (account_pool_exhaustion.go).
//
// ON THE TRANSITION, NOT ON EVERY SWEEP. Admission asks this question every
// sweep, and a pool that stays exhausted for an hour would otherwise write an
// audit event per project every thirty seconds. The event marks the moment the
// pool went to zero (or changed why it is at zero); the Deferred decision and
// its Reason carry the ongoing state. A project whose pool recovers is
// forgotten, so the next exhaustion is reported again.
func (h *accountPoolHeadroom) report(ctx context.Context, tenantID, projectID string, headroom PoolHeadroom, capped int) {
	key := tenantID + keySeparator + projectID
	h.reportMu.Lock()
	defer h.reportMu.Unlock()
	if headroom.HasHeadroom() {
		delete(h.reported, key)
		return
	}
	reason := AccountPoolCoolingDown
	if capped > 0 {
		reason = AccountPoolSaturated
	}
	if h.reported[key] == reason {
		return
	}
	h.reported[key] = reason
	reportAccountPoolExhausted(ctx, h.auditor, h.logger, accountPoolExhaustion{
		TenantID: tenantID, ProjectID: projectID, Provider: accountPoolAnyProvider,
		Reason: reason, Detail: headroom.Reason,
	})
}

// liveSessions reads the live-session count of every pooled account, or an
// empty count when no session lookup is wired.
//
// FAIL CLOSED, for the reason the quota read does: a count that could not be
// read is not zero, and admitting on it would dispatch work onto accounts the
// operator capped.
func (h *accountPoolHeadroom) liveSessions(ctx context.Context, tenantID string, pooled []*Account) (map[string]int, error) {
	if h.sessions == nil {
		return map[string]int{}, nil
	}
	ids := make([]string, 0, len(pooled))
	for _, account := range pooled {
		ids = append(ids, account.ID)
	}
	live, err := h.sessions.LiveSessions(ctx, tenantID, ids)
	if err != nil {
		return nil, &AdmissionUndecidableError{Input: "live sessions of tenant " + tenantID + "'s pooled accounts", Cause: err}
	}
	return live, nil
}

// pool assembles the accounts a spawn for this project could draw on: the
// project's own, plus the tenant-wide accounts it inherits.
func (h *accountPoolHeadroom) pool(ctx context.Context, tenantID, projectID string) ([]*Account, error) {
	tenantWide, err := h.accounts.ListByTenant(ctx, tenantID, "", "")
	if err != nil {
		return nil, &AdmissionUndecidableError{Input: "pooled accounts of tenant " + tenantID, Cause: err}
	}
	pooled := make([]*Account, 0, len(tenantWide))
	seen := make(map[string]struct{}, len(tenantWide))
	for _, account := range tenantWide {
		if account == nil {
			continue
		}
		// An account scoped to ANOTHER project is not in this project's pool.
		// The unscoped ones are, which is the inheritance the resolver's
		// tenant-wide fallback implements.
		if account.ProjectID != "" && account.ProjectID != projectID {
			continue
		}
		if _, duplicate := seen[account.ID]; duplicate {
			continue
		}
		seen[account.ID] = struct{}{}
		pooled = append(pooled, account)
	}
	return pooled, nil
}

// noteRetry keeps the earliest stated return time, ignoring an unstated one.
//
// A ZERO IS NOT A TIME, and treating it as one would report every deferral as
// clearing at the epoch — a retry instant permanently in the past, which reads
// downstream as "retry now" and turns a deferral into a spin.
func (h *PoolHeadroom) noteRetry(at time.Time) {
	if at.IsZero() {
		return
	}
	if h.RetryAt.IsZero() || at.Before(h.RetryAt) {
		h.RetryAt = at
	}
}
