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
	"time"
)

// =============================================================================
// The provider axis — subscription usage, never a price
// =============================================================================
//
// ADMISSION'S THIRD INPUT IS "CAN THE POOL SERVE ANOTHER SESSION AT ALL". It is
// not a budget in money, and this file must never become one: the owner's
// ruling is subscription-based provider accounts only, so no turn has a price,
// ConsumptionEntry.CostMicros has no production writer, and a cost axis
// summing zeroes would report unlimited headroom while looking like a cap
// (see QuotaWindow and go-quicbaas#791).
//
// The signals it reads all already exist and all already mean this:
//
//   - Account.Selectable — the account's lifecycle state and the AvailableAt
//     cooldown a provider refusal wrote. This is the ONE authority on whether
//     an account may be handed to a spawn; the resolver reads the same method.
//   - QuotaAuthority.Admit — the sliding token windows of the account's own
//     AccountSpendPolicy, summed from the consumption ledger. The same
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
}

// Compile-time interface satisfaction check.
var _ PoolHeadroomReader = (*accountPoolHeadroom)(nil)

// NewAccountPoolHeadroom returns the PoolHeadroomReader backed by the tenant's
// pooled accounts and the ledger-backed quota authority.
func NewAccountPoolHeadroom(accounts AccountStore, quota QuotaAuthority) (PoolHeadroomReader, error) {
	switch {
	case accounts == nil:
		return nil, &AdmissionConfigError{Field: "accounts", Message: "the pool a project draws on is read from the account store"}
	case quota == nil:
		return nil, &AdmissionConfigError{Field: "quota", Message: "a subscription's usage window is decided by the quota authority"}
	}
	return &accountPoolHeadroom{accounts: accounts, quota: quota}, nil
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

	headroom := PoolHeadroom{Pooled: len(pooled)}
	held := 0
	for _, account := range pooled {
		if !account.Selectable(now) {
			held++
			headroom.noteRetry(account.AvailableAt)
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
			continue
		}
		held++
		headroom.noteRetry(decision.RetryAt)
	}
	if headroom.Ready == 0 {
		headroom.Reason = "none of the " + strconv.Itoa(headroom.Pooled) +
			" pooled provider account(s) has usage headroom: " + strconv.Itoa(held) +
			" are held by a cooldown or a spent usage window"
		if !headroom.RetryAt.IsZero() {
			headroom.Reason += ", the first returning at " + headroom.RetryAt.UTC().Format(time.RFC3339)
		}
	}
	return headroom, nil
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
