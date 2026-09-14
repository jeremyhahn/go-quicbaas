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
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// =============================================================================
// AccountResolver — the single seam ACPBackend.Spawn calls to turn an
// AccountID/AccountAuto request into concrete, ready-to-inject credential
// material.
// =============================================================================

// AccountResolveRequest describes what a spawn is asking AccountResolver to
// resolve.
type AccountResolveRequest struct {
	TenantID  string
	ProjectID string

	// Provider identifies the requesting backend (see AccountProvider
	// constants), derived from the backend's command basename.
	Provider AccountProvider

	// AccountID pins a specific account. When set, Auto is ignored and no
	// selection strategy runs — Resolve either uses exactly this account or
	// fails.
	AccountID string

	// Auto requests strategy-based selection when AccountID is empty.
	Auto bool

	// StrategyName selects a specific registered strategy for this
	// resolution. Empty selects the registry default.
	StrategyName string

	// AllowedAccountIDs, when non-empty, restricts strategy-based selection to
	// exactly these accounts — the rotation pool an operator chose for one
	// session or agent. It is a FILTER, never a source: an ID listed here that
	// the tenant does not own, or that belongs to another provider, is simply
	// not a candidate. Empty means the whole tenant/project/provider pool is
	// eligible, which is the pre-existing behavior.
	AllowedAccountIDs []string

	// JobID names the orchestration job the spawn works for, empty when the
	// caller is not running one. It is not a selection input: it is the SCOPE
	// of the delivery grant this resolution takes out (see account_lease.go),
	// so a job's credential deliveries can be listed and revoked as a set.
	JobID string

	// ExcludeAccountIDs removes accounts from consideration for this one
	// resolution. Rotation uses it to guarantee forward progress: the account
	// that just hit a limit is already marked limited durably, but excluding it
	// here as well means a rotation can never hand back the same account even
	// if its hold lapsed between the two steps.
	ExcludeAccountIDs []string
}

// ResolvedAccountCredential is what AccountResolver hands back to a spawn:
// everything needed to materialize the chosen account's credential into the
// subprocess, plus the observability fields the owner asked for ("the
// session record and transcript should show WHICH account was chosen and by
// which strategy").
type ResolvedAccountCredential struct {
	AccountID   string
	DisplayName string
	Strategy    string
	Env         map[string]string
	Files       map[string][]byte

	// LeaseID is the delivery grant this resolution took out over the
	// credential material in Env / Files, empty when the deployment wired no
	// leaser (see account_lease.go).
	//
	// The caller's obligation is to RECORD it somewhere the session's ending
	// can find — SpawnRequest -> Session -> SessionRecord.CredentialLeaseIDs —
	// so the reaper that tears the session down also withdraws the grant. A
	// lease nobody recorded is a lease nobody revokes.
	LeaseID string

	// LeaseExpiresAt is when the delivery grant stops being authorised without
	// renewal. It is NOT the credential's own expiry, which the OAuth
	// refresher owns; see account_lease.go.
	LeaseExpiresAt time.Time
}

// pinnedAccountStrategyName is the strategy label recorded when the caller
// named the account outright. It is a MARKER, not a registered strategy: no
// AccountSelectionStrategy answers to it, so a later resolution must never
// replay it as StrategyName (see rotationSpawnRequestFromRecord).
const pinnedAccountStrategyName = "pinned"

// AccountResolver resolves an AccountResolveRequest into usable credential
// material. A nil AccountID and Auto == false is never passed to Resolve —
// ACPBackend.Spawn only calls it when the caller actually asked for account
// selection (see acp_backend.go, resolveAccount).
type AccountResolver interface {
	Resolve(ctx context.Context, req AccountResolveRequest) (*ResolvedAccountCredential, error)
}

// accountResolver is the default, production AccountResolver: it composes an
// AccountStore, an AccountTokenManager, and an AccountStrategyRegistry.
type accountResolver struct {
	store      AccountStore
	tokens     *AccountTokenManager
	strategies *AccountStrategyRegistry
	auditor    StoreAuditor
	logger     interfaces.Logger

	// grantor supplies the delivery grant that makes this resolution's
	// credential revocable. Nil leaves releases unleased; see
	// WithAccountDeliveryGrantor.
	grantor AccountDeliveryGrantor

	// spend is the cap this resolution is checked against before any
	// credential is produced. Nil leaves the release guarded only by the
	// account's lifecycle state; see WithSpendAuthority.
	spend SpendAuthority
}

// Compile-time interface satisfaction check.
var _ AccountResolver = (*accountResolver)(nil)

// NewAccountResolver returns the default AccountResolver.
func NewAccountResolver(store AccountStore, tokens *AccountTokenManager, strategies *AccountStrategyRegistry, opts ...StoreOption) AccountResolver {
	cfg := newStoreConfig(opts...)
	return &accountResolver{
		store:      store,
		tokens:     tokens,
		strategies: strategies,
		auditor:    cfg.auditor,
		logger:     cfg.logger,
		grantor:    cfg.grantor,
		spend:      cfg.spend,
	}
}

// logTouchFailure reports a failed usage stamp. It is a bookkeeping fault
// rather than a resolution failure, so it is surfaced on the log and then
// swallowed as far as the caller is concerned. A deployment that wired no
// logger loses the report, which is why the caller does not depend on it.
func (r *accountResolver) logTouchFailure(tenantID, accountID string, err error) {
	if r.logger != nil {
		r.logger.Warnf("orchestration: failed to stamp usage on account %s (tenant %s): %s",
			accountID, tenantID, err.Error())
	}
}

// Resolve implements AccountResolver.
func (r *accountResolver) Resolve(ctx context.Context, req AccountResolveRequest) (*ResolvedAccountCredential, error) {
	account, err := r.selectAccount(ctx, req)
	if err != nil {
		r.audit(ctx, req.TenantID, "", err)
		return nil, err
	}
	if account.Status == AccountStatusRevoked {
		err := &AccountRevokedError{AccountID: account.ID}
		r.audit(ctx, req.TenantID, account.ID, err)
		return nil, err
	}
	// An errored account is held out of selection until an operator
	// investigates (see AccountStatusError). The strategy path already excludes
	// it through Account.Selectable; a PINNED account reaches this check
	// instead, and the two paths must agree — otherwise naming an account
	// outright is a way to use a credential the pool has already judged broken.
	if account.Status == AccountStatusError {
		err := &AccountErroredError{AccountID: account.ID, LastError: account.LastError}
		r.audit(ctx, req.TenantID, account.ID, err)
		return nil, err
	}
	// A pinned account under a live limit hold fails LOUDLY rather than being
	// silently swapped for another. The caller pinned it on purpose; only an
	// explicit rotation (which resolves with Auto, not a pin) may move off it.
	// The error carries AvailableAt so the caller can decide between waiting
	// and rotating instead of having to guess.
	if account.LimitHoldActive(time.Now().UTC()) {
		err := &AccountLimitedError{
			AccountID:   account.ID,
			Provider:    account.Provider,
			AvailableAt: account.AvailableAt,
			Signal:      account.LastError,
		}
		r.audit(ctx, req.TenantID, account.ID, err)
		return nil, err
	}

	// THE CAP IS CHECKED BEFORE THE CREDENTIAL EXISTS, not after. Everything
	// above this line asks whether the PROVIDER has refused the account;
	// this asks whether our own ledger says the operator's allowance is
	// already spent. It runs before tokens.Resolve deliberately: resolving
	// first would renew an oauth account — a provider round trip, and a
	// rotation of live secret material — on behalf of a spawn that is about
	// to be refused anyway.
	if err := r.authorizeSpend(ctx, account); err != nil {
		r.audit(ctx, req.TenantID, account.ID, err)
		return nil, err
	}

	cred, err := r.tokens.Resolve(ctx, account)
	if err != nil {
		r.audit(ctx, req.TenantID, account.ID, err)
		return nil, err
	}

	strategyName := req.StrategyName
	if req.AccountID != "" {
		strategyName = pinnedAccountStrategyName
	} else if strategyName == "" {
		if s, ok := r.strategies.Get(""); ok {
			strategyName = s.Name()
		}
	}

	// The usage stamp this writes is what LeastUsageAccountStrategy balances
	// on: a Touch that fails silently leaves the account looking perpetually
	// least-used and every subsequent selection lands on it again, so the
	// failure is logged rather than dropped. It does not fail the resolution —
	// the credential is already resolved and refusing to hand it over would
	// turn a bookkeeping fault into an outage.
	if touchErr := r.store.Touch(
		ctx, req.TenantID, account.ID, TouchAccountOptions{MarkUsed: true}); touchErr != nil {
		r.logTouchFailure(req.TenantID, account.ID, touchErr)
	}
	r.audit(ctx, req.TenantID, account.ID, nil)

	// THE DELIVERY IS LEASED BEFORE THE MATERIAL LEAVES THIS FUNCTION, and a
	// failure to lease it fails the resolution. Handing the credential over
	// and then noticing the grant did not land would produce exactly what the
	// lease exists to prevent: a live provider login on a worker host that
	// nothing can withdraw.
	delivery, leaseErr := r.leaseDelivery(ctx, req, account)
	if leaseErr != nil {
		r.audit(ctx, req.TenantID, account.ID, leaseErr)
		return nil, leaseErr
	}

	env := make(map[string]string, len(cred.Env)+1)
	for k, v := range cred.Env {
		env[k] = v
	}
	if cred.AccessTokenEnvKey != "" && cred.AccessToken != "" {
		env[cred.AccessTokenEnvKey] = cred.AccessToken
	}

	resolved := &ResolvedAccountCredential{
		AccountID:   account.ID,
		DisplayName: account.DisplayName,
		Strategy:    strategyName,
		Env:         env,
		Files:       cred.Files,
	}
	if delivery != nil {
		resolved.LeaseID = delivery.LeaseID
		resolved.LeaseExpiresAt = delivery.ExpiresAt
	}
	return resolved, nil
}

// leaseDelivery takes out the grant covering this resolution's hand-over.
// A deployment with no grantor wired resolves unleased, which is the behaviour
// every caller had before delivery grants existed.
func (r *accountResolver) leaseDelivery(
	ctx context.Context,
	req AccountResolveRequest,
	account *Account,
) (*AccountCredentialLease, error) {
	if r.grantor == nil {
		return nil, nil
	}
	return r.grantor.GrantDelivery(ctx, AccountLeaseScope{
		TenantID:  req.TenantID,
		AccountID: account.ID,
		Provider:  account.Provider,
		ProjectID: req.ProjectID,
		JobID:     req.JobID,
	})
}

// authorizeSpend asks the spend authority whether account may serve more
// work. A deployment with no authority wired is unmetered and admits.
func (r *accountResolver) authorizeSpend(ctx context.Context, account *Account) error {
	if r.spend == nil {
		return nil
	}
	// Zero estimates: a spawn knows how much a session may cost no better
	// than anyone else, so the question it can honestly ask is whether the
	// account is ALREADY over. Inventing a per-session estimate would refuse
	// accounts on a figure nobody measured.
	return r.spend.Authorize(ctx, SpendAuthorizeRequest{Account: account}, time.Now().UTC())
}

// selectAccount resolves req to exactly one Account, either by pinned ID or
// by running the requested/default strategy over the tenant's active
// candidates for req.Provider (and req.ProjectID, when the caller scoped the
// request to one project).
func (r *accountResolver) selectAccount(ctx context.Context, req AccountResolveRequest) (*Account, error) {
	if req.AccountID != "" {
		account, err := r.store.Get(ctx, req.TenantID, req.AccountID)
		if err != nil {
			return nil, err
		}
		if req.Provider != "" && account.Provider != req.Provider {
			return nil, &AccountProviderMismatchError{AccountID: account.ID, AccountProvider: account.Provider, RequestProvider: req.Provider}
		}
		return account, nil
	}
	if !req.Auto {
		return nil, &AccountInvalidError{Message: "resolve request must set either account_id or auto"}
	}

	candidates, err := r.store.ListByTenant(ctx, req.TenantID, req.Provider, req.ProjectID)
	if err != nil {
		return nil, err
	}

	eligible := newAccountEligibility(req)
	now := time.Now().UTC()

	// considered is the candidate list the final answer was drawn from, kept so
	// that "nothing was eligible" can say WHY — see coolingDown below.
	considered := candidates
	active := eligible.filter(candidates, now, nil)
	if len(active) == 0 {
		// Project-scoped pool empty: fall back to the tenant-wide pool for
		// this provider before giving up, so a project need not duplicate
		// every account explicitly.
		if req.ProjectID != "" {
			tenantWide, twErr := r.store.ListByTenant(ctx, req.TenantID, req.Provider, "")
			if twErr != nil {
				// A STORE THAT COULD NOT ANSWER IS NOT AN ANSWER, and swallowing
				// this used to make it one. The fallback read was best-effort
				// while "no account available" meant only "try again later";
				// go-quicbaas#235 gave that error a consequence — the SDLC
				// module now reads it as a proven configuration fault and parks
				// the work item for a human. So a transient store failure
				// stranded work behind a person even when an eligible
				// tenant-wide account was sitting right there, unread.
				//
				// Returned instead, so it stays what it is: a backend failure,
				// charged and retried like any other, and distinguishable by
				// every caller from the pool genuinely being empty.
				return nil, twErr
			}
			unscoped := func(c *Account) bool { return c.ProjectID == "" }
			active = eligible.filter(tenantWide, now, unscoped)
			if len(active) == 0 {
				considered = append(append([]*Account{}, candidates...),
					filterAccounts(tenantWide, unscoped)...)
			}
		}
	}
	if len(active) == 0 {
		// AN EMPTY POOL AND A COOLING-DOWN POOL ARE DIFFERENT ANSWERS, and
		// they used to be the same one. The first is a misconfiguration that
		// nothing but an operator will fix; the second is the rotation working
		// as designed, and it clears itself at AvailableAt. The SDLC module
		// parks a work item for a human on the first and must not on the
		// second, or it strands work that would have been dispatchable minutes
		// later. See AccountsCoolingDownError.
		// held > 0 means: nothing is available NOW (active is empty) and at
		// least one permitted candidate returns at a stated time. That is
		// enough to wait on, even in a pool that also holds accounts needing a
		// human — the one that returns is what makes progress possible.
		if held, earliest := coolingDown(considered, eligible, now); held > 0 {
			return nil, &AccountsCoolingDownError{
				TenantID: req.TenantID, ProjectID: req.ProjectID, Provider: req.Provider,
				Candidates: held, AvailableAt: earliest,
			}
		}
		return nil, &NoAccountAvailableError{TenantID: req.TenantID, ProjectID: req.ProjectID, Provider: req.Provider, Strategy: req.StrategyName}
	}

	strategy, ok := r.strategies.Get(req.StrategyName)
	if !ok {
		return nil, &NoAccountAvailableError{TenantID: req.TenantID, ProjectID: req.ProjectID, Provider: req.Provider, Strategy: req.StrategyName}
	}
	selected, err := strategy.Select(ctx, active, AccountSelectionRequest{
		TenantID:  req.TenantID,
		ProjectID: req.ProjectID,
		Provider:  req.Provider,
	})
	if err != nil {
		return nil, err
	}
	r.persistCooldownExpiry(ctx, selected)
	return selected, nil
}

// persistCooldownExpiry writes back an account the read path just normalized
// out of a lapsed limit hold.
//
// The normalization itself happens in memory on every decode
// (Account.expireCooldown), so selection is already correct without this. The
// write exists so an operator listing the pool immediately after a rotation
// sees "active" durably rather than a record that only looks recovered to
// whoever happens to read it next. Best effort: a failure costs nothing but a
// repeat of the same in-memory normalization on the following read — it is
// reported rather than discarded so a store failing every write stays visible.
func (r *accountResolver) persistCooldownExpiry(ctx context.Context, account *Account) {
	if account == nil || !account.AvailableAt.IsZero() || account.Status != AccountStatusActive {
		return
	}
	if err := r.store.Touch(ctx, account.TenantID, account.ID, TouchAccountOptions{
		SetStatusValid: true,
		SetStatus:      AccountStatusActive,
		ClearLastError: true,
	}); err != nil {
		r.logTouchFailure(account.TenantID, account.ID, err)
	}
}

// filterAccounts returns the candidates matching keep. A plain helper, used to
// assemble the list a failed resolution reports on.
func filterAccounts(candidates []*Account, keep func(*Account) bool) []*Account {
	out := make([]*Account, 0, len(candidates))
	for _, c := range candidates {
		if keep == nil || keep(c) {
			out = append(out, c)
		}
	}
	return out
}

// coolingDown reports how many of the considered candidates will RETURN ON
// THEIR OWN AT A KNOWN TIME, and when the first of them does.
//
// TWO TESTS, AND EACH RULES OUT A DIFFERENT WAY OF STRANDING WORK.
//
// The first is "held out ONLY by the hold". An account excluded by the
// caller's allow-list, or revoked, or errored, is not coming back by itself and
// must not make a misconfigured pool look like a busy one — the caller treats a
// cooldown as a reason to wait rather than to tell a human, so a pool that will
// never recover must not qualify.
//
// The second is "and the hold NAMES ITS EXPIRY", which review of #235 caught
// and which the first test alone does not cover. Account.LimitHoldActive treats
// a zero AvailableAt as held INDEFINITELY: nothing promised when it would come
// back, which is precisely what "self-clearing" cannot mean. Counting one would
// tell the SDLC module to requeue and try again forever against a pool that has no
// return date — a livelock, cheap per iteration and endless.
//
// So an indefinite hold is treated like a revoked account: it does not count,
// and a pool of nothing but indefinite holds is reported as no-account-available
// and parks for a human. A pool with one indefinite hold and one that expires
// at T still returns at T, so it IS a cooldown and T is the answer.
//
// The consequence worth stating: when this reports a cooldown, the time is
// never zero. A caller can rely on being told when.
func coolingDown(considered []*Account, eligible accountEligibility, now time.Time) (int, time.Time) {
	var (
		returning int
		earliest  time.Time
	)
	for _, c := range considered {
		if c == nil || c.Status != AccountStatusLimited || !c.LimitHoldActive(now) {
			continue
		}
		// The rotation policy still applies: an account this request excluded
		// is not one it is waiting for.
		if !eligible.permitted(c) {
			continue
		}
		// Held with no stated expiry: not coming back on its own.
		if c.AvailableAt.IsZero() {
			continue
		}
		returning++
		if earliest.IsZero() || c.AvailableAt.Before(earliest) {
			earliest = c.AvailableAt
		}
	}
	return returning, earliest
}

// =============================================================================
// Candidate eligibility
// =============================================================================

// accountEligibility applies one resolve request's rotation policy — the
// allow-list, the exclusions, and the limit-hold cooldown — to a candidate
// list. It is built once per resolution so the per-candidate test stays an O(1)
// map probe rather than a scan of two slices per account.
type accountEligibility struct {
	allowed  map[string]struct{}
	excluded map[string]struct{}
}

// newAccountEligibility indexes req's rotation policy.
func newAccountEligibility(req AccountResolveRequest) accountEligibility {
	e := accountEligibility{}
	if len(req.AllowedAccountIDs) > 0 {
		e.allowed = make(map[string]struct{}, len(req.AllowedAccountIDs))
		for _, id := range req.AllowedAccountIDs {
			e.allowed[id] = struct{}{}
		}
	}
	if len(req.ExcludeAccountIDs) > 0 {
		e.excluded = make(map[string]struct{}, len(req.ExcludeAccountIDs))
		for _, id := range req.ExcludeAccountIDs {
			e.excluded[id] = struct{}{}
		}
	}
	return e
}

// filter returns the candidates eligible at now, optionally narrowed further by
// extra (used for the tenant-wide fallback, which additionally requires the
// account to be unscoped).
func (e accountEligibility) filter(candidates []*Account, now time.Time, extra func(*Account) bool) []*Account {
	out := make([]*Account, 0, len(candidates))
	for _, c := range candidates {
		if !e.eligible(c, now) {
			continue
		}
		if extra != nil && !extra(c) {
			continue
		}
		out = append(out, c)
	}
	return out
}

// permitted reports whether this request's ROTATION POLICY admits c, ignoring
// whether the account is selectable right now.
//
// Split out from eligible so a failed resolution can ask "would this candidate
// have been allowed, if it were not on hold" — the question that separates a
// pool which is busy from one which is misconfigured.
func (e accountEligibility) permitted(c *Account) bool {
	if e.allowed != nil {
		if _, ok := e.allowed[c.ID]; !ok {
			return false
		}
	}
	if e.excluded != nil {
		if _, ok := e.excluded[c.ID]; ok {
			return false
		}
	}
	return true
}

// eligible reports whether one candidate may be selected at now.
func (e accountEligibility) eligible(c *Account, now time.Time) bool {
	if !c.Selectable(now) {
		return false
	}
	return e.permitted(c)
}

func (r *accountResolver) audit(ctx context.Context, tenantID, accountID string, err error) {
	emitStoreAudit(ctx, r.auditor, StoreAuditEvent{
		TenantID:   tenantID,
		Resource:   AuditResourceAccount,
		ResourceID: accountID,
		Action:     AuditActionUse,
		Success:    err == nil,
	})
}
