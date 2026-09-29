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
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// =============================================================================
// The spend authority — one gate in front of every credential release
// =============================================================================
//
// THIS IS WHERE A CAP BECOMES A REFUSAL. The ledger records what was spent,
// the quota authority says whether more may be, and the breaker says whether
// the account is working; none of those three stops anything on its own. This
// composes them into the single question the two credential-release paths ask
// before they hand a live paid credential to a worker:
//
//	in-process spawn  — accountResolver.Resolve (account_resolver.go)
//	remote daemon     — AccountService.MaterializeAccountCredential
//
// Both were previously guarded only by Account.Selectable, which asks whether
// the PROVIDER has refused us. That is a report of a limit already hit. This
// asks whether OUR OWN records say the next call is within the allowance the
// operator set, which is a refusal before the money is spent.
//
// FAIL CLOSED, AND NEVER WITH A NUMBER. Every path that cannot establish the
// answer returns an error and no admission. A gate that admitted whenever it
// could not read the ledger would open exactly when the store is in trouble,
// which is indistinguishable from a healthy quiet account right up to the
// invoice. Refusing instead costs a spawn that could have run, which the next
// sweep retries.
//
// AN UNMETERED ACCOUNT COSTS NOTHING TO ADMIT. A policy that meters nothing
// short-circuits before the ledger is read at all, so wiring the gate into a
// deployment that has configured no allowance adds no read to the spawn path.
//
// WHAT IT DOES NOT DECIDE. Whether the account is revoked, errored or under a
// provider hold stays with Account.Selectable and the resolver's own checks —
// this gate would otherwise be a second answer to a question that already has
// one, and the two would drift.

// spendProbeTTL bounds one half-open trial.
//
// It is the trial's ONLY release. A spawn admitted on a half-open breaker has
// no single moment at which "the trial is over" — the session it starts may
// run for minutes and its outcome reaches the ledger, not this file — so the
// probe is reclaimed by expiry rather than by a matching release call. The
// value is the cost of being wrong in each direction: too short and two hosts
// both trial a failing account, too long and a recovered account stays parked
// after its trial succeeded. A minute is long enough for a spawn to reach its
// first turn and short enough that a recovered account is retried promptly.
const spendProbeTTL = time.Minute

// SpendAuthorizeRequest is one "may this account serve more work" question.
type SpendAuthorizeRequest struct {
	// Account is the pooled account being asked about. Required, and passed
	// in rather than re-read so the decision judges the same record the
	// caller is about to release — re-reading would let this disagree with
	// the resolver about an account whose hold lapsed between the two reads.
	Account *Account

	// EstimatedTokens is what the caller is about to spend, added to the
	// window before the comparison so a spawn that would cross the limit is
	// refused before it runs. Zero — which is what a spawn knows — asks only
	// whether the account is already over.
	EstimatedTokens int64
}

// SpendAuthority admits or refuses one account's next unit of work.
type SpendAuthority interface {
	// Authorize returns nil when the account may serve more work, a typed
	// refusal when it may not, and an error when the answer could not be
	// established. A refusal and a failure are never the same value: an
	// operator seeing work stop needs to tell "the cap is spent" from "the
	// ledger could not be read".
	Authorize(ctx context.Context, req SpendAuthorizeRequest, now time.Time) error

	// Check answers the same question WITHOUT TAKING ANYTHING: a half-open
	// breaker admits rather than claiming its single trial. It is what a
	// selection asks of every candidate before a strategy ranks them
	// (go-quicbaas#1058, ruling F6); claiming a trial for each candidate
	// would hold the probe on every account the strategy then passed over.
	// Authorize, asked of the one account selected, is still the gate.
	Check(ctx context.Context, req SpendAuthorizeRequest, now time.Time) error
}

// spendAuthority composes the quota authority and the breaker over one ledger.
type spendAuthority struct {
	quota   QuotaAuthority
	breaker CircuitBreaker

	// holder identifies this host on a half-open probe. Required: an empty
	// holder would make every host's trial look like every other host's, and
	// the probe exists precisely to tell them apart.
	holder string
}

// Compile-time interface satisfaction check.
var _ SpendAuthority = (*spendAuthority)(nil)

// NewSpendAuthority returns the SpendAuthority for ledger, arbitrating
// half-open probes on kv as holder.
//
// holder is this host's identity (the daemon's node name). It is required for
// the same reason a lease names its node: "exactly one caller may try" is
// meaningless if every caller presents the same name.
func NewSpendAuthority(ledger ConsumptionLedger, kv interfaces.KVStore, holder string) (SpendAuthority, error) {
	if ledger == nil {
		return nil, &SpendAuthorityConfigError{Field: "ledger"}
	}
	if kv == nil {
		return nil, &SpendAuthorityConfigError{Field: "kv"}
	}
	if holder == "" {
		return nil, &SpendAuthorityConfigError{
			Field:   "holder",
			Message: "must name this host; a probe every host spells the same way arbitrates nothing",
		}
	}
	return &spendAuthority{
		quota:   NewQuotaAuthority(ledger),
		breaker: NewCircuitBreaker(ledger, kv),
		holder:  holder,
	}, nil
}

// Authorize implements SpendAuthority.
func (a *spendAuthority) Authorize(ctx context.Context, req SpendAuthorizeRequest, now time.Time) error {
	return a.decide(ctx, req, now, true)
}

// Check implements SpendAuthority.
func (a *spendAuthority) Check(ctx context.Context, req SpendAuthorizeRequest, now time.Time) error {
	return a.decide(ctx, req, now, false)
}

// decide is Authorize and Check. claimTrial says whether a half-open breaker
// is admitted by winning its single trial (Authorize) or simply admitted as a
// candidate that may be tried (Check).
func (a *spendAuthority) decide(ctx context.Context, req SpendAuthorizeRequest, now time.Time, claimTrial bool) error {
	if req.Account == nil {
		return &SpendAuthorityConfigError{Field: "account", Message: "must not be nil"}
	}
	policy := req.Account.Spend
	if !policy.Metered() {
		return nil
	}

	// The money question first. A breaker-open account that has also spent
	// its allowance is refused for the cap, which is the answer that does not
	// change when the account starts working again.
	if err := a.authorizeQuota(ctx, req, policy, now); err != nil {
		return err
	}
	return a.authorizeBreaker(ctx, req.Account, policy, now, claimTrial)
}

// authorizeQuota asks the quota authority and turns its refusal into this
// package's typed one.
func (a *spendAuthority) authorizeQuota(ctx context.Context, req SpendAuthorizeRequest, policy AccountSpendPolicy, now time.Time) error {
	if len(policy.Quota.Windows) == 0 {
		return nil
	}
	decision, err := a.quota.Admit(ctx, QuotaAdmitRequest{
		Account:         req.Account,
		Policy:          policy.Quota,
		EstimatedTokens: req.EstimatedTokens,
	}, now)
	if err != nil {
		return err
	}
	if decision.Allowed {
		return nil
	}
	refuse, known := quotaRefusalsByReason[decision.Reason]
	if !known {
		// A reason this file does not recognize is refused rather than
		// admitted. The set is closed and both files are in this package, so
		// reaching here means one of them grew a case the other did not —
		// and the safe direction for an unrecognized SPEND verdict is to
		// stop, not to let the release through unexamined.
		return &AccountSpendUnknownVerdictError{AccountID: req.Account.ID, Reason: string(decision.Reason)}
	}
	return refuse(req.Account, decision)
}

// quotaRefusalsByReason maps each refusing quota reason to the typed error the
// callers of this gate understand.
//
// A map rather than a switch so the set is enumerable: the dispatcher's reason
// text, the fault classifier and the forge comment all need to agree about
// which refusals exist, and a switch cannot be ranged over to prove they do.
var quotaRefusalsByReason = map[QuotaReason]func(account *Account, decision *QuotaDecision) error{
	// The provider's own refusal is reported in the pool's EXISTING
	// vocabulary rather than in a second one. AccountLimitedError is what
	// every caller already handles for a held account, and minting a
	// parallel error for the same condition would give one state two names.
	QuotaReasonProviderHold: func(account *Account, decision *QuotaDecision) error {
		return &AccountLimitedError{
			AccountID:   account.ID,
			Provider:    account.Provider,
			AvailableAt: decision.RetryAt,
			Signal:      account.LastError,
		}
	},
	QuotaReasonTokenLimit: func(account *Account, decision *QuotaDecision) error {
		return newQuotaExhaustedError(account, decision, QuotaAxisTokens)
	},
	QuotaReasonCostLimit: func(account *Account, decision *QuotaDecision) error {
		return newQuotaExhaustedError(account, decision, QuotaAxisEquivalentMicros)
	},
}

// newQuotaExhaustedError builds the refusal for a spent window.
func newQuotaExhaustedError(account *Account, decision *QuotaDecision, axis string) error {
	return &AccountQuotaExhaustedError{
		AccountID: account.ID,
		Provider:  account.Provider,
		Window:    decision.Window,
		Axis:      axis,
		Used:      decision.Used,
		Limit:     decision.Limit,
		RetryAt:   decision.RetryAt,
	}
}

// authorizeBreaker derives the account's breaker state and admits a half-open
// account only to the single probe holder — or, when claimTrial is false, to
// any caller, which takes nothing.
func (a *spendAuthority) authorizeBreaker(ctx context.Context, account *Account, policy AccountSpendPolicy, now time.Time, claimTrial bool) error {
	if policy.Breaker == nil {
		return nil
	}
	verdict, err := a.breaker.Evaluate(ctx, account.TenantID, account.ID, *policy.Breaker, now)
	if err != nil {
		return err
	}
	if verdict.Admits() {
		return nil
	}
	if verdict.State == BreakerOpen {
		return &AccountBreakerOpenError{
			AccountID: account.ID,
			Provider:  account.Provider,
			Failures:  verdict.Failures,
			OpenUntil: verdict.OpenUntil,
		}
	}

	if !claimTrial {
		return nil
	}

	// Half-open: exactly one caller finds out whether the account works
	// again. Winning admits this release; losing is an ordinary refusal, not
	// an error, and the loser's work waits for the trial's outcome to reach
	// the ledger.
	won, err := a.breaker.AcquireProbe(ctx, account.TenantID, account.ID, a.holder, now, spendProbeTTL)
	if err != nil {
		return err
	}
	if won {
		return nil
	}
	return &AccountBreakerTrialHeldError{
		AccountID: account.ID,
		Provider:  account.Provider,
		Failures:  verdict.Failures,
	}
}

// =============================================================================
// Errors
// =============================================================================
//
// Each refusal has its own type rather than one "account not authorized". The
// three say three different things to whoever is waiting: a spent window frees
// up at a stated moment, an open breaker frees up at a different one, and a
// held trial frees up as soon as somebody else's probe reports. A single
// message would hide which applies, and every one of the three is rendered to
// a human on a forge issue.

// Sentinel errors for the spend authority.
var (
	// ErrAccountQuotaExhausted indicates an account whose metered allowance
	// for a window is spent.
	ErrAccountQuotaExhausted = errors.New("account: spend quota exhausted")

	// ErrAccountBreakerOpen indicates an account taken out of service by its
	// own failure history.
	ErrAccountBreakerOpen = errors.New("account: circuit breaker open")

	// ErrAccountBreakerTrialHeld indicates a half-open account whose single
	// trial is held by another caller.
	ErrAccountBreakerTrialHeld = errors.New("account: circuit breaker trial held elsewhere")

	// ErrAccountSpendUnknownVerdict indicates a quota verdict this gate does
	// not recognize, which is refused rather than admitted.
	ErrAccountSpendUnknownVerdict = errors.New("account: unrecognized spend verdict")

	// ErrSpendAuthorityConfig indicates a spend authority that cannot be
	// built or a question it cannot be asked.
	ErrSpendAuthorityConfig = errors.New("account: spend authority misconfigured")
)

// AccountQuotaExhaustedError reports an account whose window allowance is
// spent. It carries the evidence because a refusal that cannot show its
// working is indistinguishable from a broken gate.
type AccountQuotaExhaustedError struct {
	AccountID string
	Provider  AccountProvider

	// Window names the metered interval that refused, and Axis which of its
	// two allowances ran out.
	Window string
	Axis   string

	Used  int64
	Limit int64

	// RetryAt is when the window's oldest entry ages out and the decision
	// changes. Zero when nothing can say — an empty window that still
	// refused, which means the estimate alone exceeds the allowance.
	RetryAt time.Time
}

// Error implements the error interface.
func (e *AccountQuotaExhaustedError) Error() string {
	msg := ErrAccountQuotaExhausted.Error() + ": account " + e.AccountID +
		" spent " + strconv.FormatInt(e.Used, 10) + " of " + strconv.FormatInt(e.Limit, 10) +
		" " + e.Axis + " in window " + strconv.Quote(e.Window)
	if !e.RetryAt.IsZero() {
		msg += "; frees up at " + e.RetryAt.UTC().Format(time.RFC3339)
	}
	return msg
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *AccountQuotaExhaustedError) Unwrap() error { return ErrAccountQuotaExhausted }

// AccountBreakerOpenError reports an account whose breaker tripped and whose
// cooldown is still running.
type AccountBreakerOpenError struct {
	AccountID string
	Provider  AccountProvider

	// Failures is how many failures in the policy's window tripped it.
	Failures int

	// OpenUntil is when the breaker becomes half-open.
	OpenUntil time.Time
}

// Error implements the error interface.
func (e *AccountBreakerOpenError) Error() string {
	msg := ErrAccountBreakerOpen.Error() + ": account " + e.AccountID +
		" failed " + strconv.Itoa(e.Failures) + " times with no success since"
	if !e.OpenUntil.IsZero() {
		msg += "; retried from " + e.OpenUntil.UTC().Format(time.RFC3339)
	}
	return msg
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *AccountBreakerOpenError) Unwrap() error { return ErrAccountBreakerOpen }

// AccountBreakerTrialHeldError reports a half-open account whose single trial
// belongs to somebody else right now.
//
// It is deliberately NOT AccountBreakerOpenError: the account may be perfectly
// healthy and about to be proved so, and a caller that treated this as an open
// breaker would report a cooldown that is not running.
type AccountBreakerTrialHeldError struct {
	AccountID string
	Provider  AccountProvider
	Failures  int
}

// Error implements the error interface.
func (e *AccountBreakerTrialHeldError) Error() string {
	return ErrAccountBreakerTrialHeld.Error() + ": account " + e.AccountID +
		" is being retried by another caller after " + strconv.Itoa(e.Failures) + " failures"
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *AccountBreakerTrialHeldError) Unwrap() error { return ErrAccountBreakerTrialHeld }

// AccountSpendUnknownVerdictError reports a quota verdict the gate cannot map.
type AccountSpendUnknownVerdictError struct {
	AccountID string
	Reason    string
}

// Error implements the error interface.
func (e *AccountSpendUnknownVerdictError) Error() string {
	return ErrAccountSpendUnknownVerdict.Error() + ": account " + e.AccountID +
		" was refused for " + strconv.Quote(e.Reason) + ", which this gate cannot explain"
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *AccountSpendUnknownVerdictError) Unwrap() error { return ErrAccountSpendUnknownVerdict }

// SpendAuthorityConfigError reports a spend authority that cannot be built or
// a question it cannot be asked.
type SpendAuthorityConfigError struct {
	Field   string
	Message string
}

// Error implements the error interface.
func (e *SpendAuthorityConfigError) Error() string {
	msg := ErrSpendAuthorityConfig.Error() + ": " + e.Field
	if e.Message != "" {
		return msg + ": " + e.Message
	}
	return msg + " is required"
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *SpendAuthorityConfigError) Unwrap() error { return ErrSpendAuthorityConfig }
