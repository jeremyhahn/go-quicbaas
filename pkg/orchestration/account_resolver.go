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

	env := make(map[string]string, len(cred.Env)+1)
	for k, v := range cred.Env {
		env[k] = v
	}
	if cred.AccessTokenEnvKey != "" && cred.AccessToken != "" {
		env[cred.AccessTokenEnvKey] = cred.AccessToken
	}

	return &ResolvedAccountCredential{
		AccountID:   account.ID,
		DisplayName: account.DisplayName,
		Strategy:    strategyName,
		Env:         env,
		Files:       cred.Files,
	}, nil
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
	active := eligible.filter(candidates, now, nil)
	if len(active) == 0 {
		// Project-scoped pool empty: fall back to the tenant-wide pool for
		// this provider before giving up, so a project need not duplicate
		// every account explicitly.
		if req.ProjectID != "" {
			tenantWide, twErr := r.store.ListByTenant(ctx, req.TenantID, req.Provider, "")
			if twErr == nil {
				active = eligible.filter(tenantWide, now, func(c *Account) bool { return c.ProjectID == "" })
			}
		}
	}
	if len(active) == 0 {
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

// eligible reports whether one candidate may be selected at now.
func (e accountEligibility) eligible(c *Account, now time.Time) bool {
	if !c.Selectable(now) {
		return false
	}
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

func (r *accountResolver) audit(ctx context.Context, tenantID, accountID string, err error) {
	emitStoreAudit(ctx, r.auditor, StoreAuditEvent{
		TenantID:   tenantID,
		Resource:   AuditResourceAccount,
		ResourceID: accountID,
		Action:     AuditActionUse,
		Success:    err == nil,
	})
}
