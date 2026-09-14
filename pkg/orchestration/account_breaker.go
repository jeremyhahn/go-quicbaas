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
	"time"

	qerrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// =============================================================================
// The account circuit breaker
// =============================================================================
//
// DERIVED, NOT KEPT. The open/closed decision is a pure function of the
// consumption ledger: the failures already recorded there, over the policy's
// window, at the instant the question is asked. Nothing about the breaker's
// state is stored, so there is no second record to contest, no counter for two
// hosts to increment into each other, and no reconciliation to write. Two hosts
// asking the same question of the same ledger get the same answer because they
// read the same facts, not because they agreed about a number.
//
// That is the whole reason failure entries are in the ledger at all. A failed
// turn spends tokens, so it belongs on the invoice; keeping it distinguishable
// from a successful one is what lets this file exist without a plane of its own.
//
// THE ONE THING THAT CANNOT BE DERIVED is which host gets the half-open probe.
// "Exactly one caller may try" is a decision, not an observation, and the
// ledger has no opinion about it. So the probe — and only the probe — is a
// guarded create on a key of its own, released by a guarded delete. It is the
// same primitive the task claim uses, for the same reason: two hosts each
// believing they hold the single probe is the failure the breaker exists to
// prevent, multiplied.
//
// WHAT "TRIPPED" MEANS HERE, precisely, because a vaguer word would invite
// each reader to assume a different rule: the window holds at least
// FailureThreshold failures AND no success has been recorded since the most
// recent failure. It is deliberately NOT "N consecutive failures" — the ledger
// is summed, not replayed in order, and a rule this file cannot evaluate
// exactly is a rule it would have to approximate silently.

// Probe keyspace. One key per account; the value names its holder so a release
// can only remove the holder's own probe.
//
//	orch/costprobe/{tenant}/{account} -> breakerProbe (JSON)
const breakerProbeKeyspace = "orch/costprobe/"

// BreakerState is the account's position in the breaker cycle.
type BreakerState string

const (
	// BreakerClosed admits: the account is not failing enough to stop.
	BreakerClosed BreakerState = "closed"

	// BreakerOpen refuses: the account tripped and its cooldown is running.
	BreakerOpen BreakerState = "open"

	// BreakerHalfOpen refuses everyone except the single holder of the probe.
	// The cooldown has elapsed, so ONE caller may find out whether the account
	// works again; the rest keep away until it reports.
	BreakerHalfOpen BreakerState = "half_open"
)

// BreakerPolicy is the tripping rule for one account.
type BreakerPolicy struct {
	// Window is how far back failures are counted. Required and positive.
	Window time.Duration `json:"window"`

	// FailureThreshold is how many failures in the window trip the breaker.
	// Required and positive: a threshold of zero would trip on an account
	// that has never failed, which is the fail-closed overreach that looks
	// like caution and is an outage.
	FailureThreshold int `json:"failure_threshold"`

	// Cooldown is how long the breaker stays open after the latest failure,
	// measured from that failure rather than from the moment it tripped —
	// the account's own last bad turn is the fact every host can see, while
	// "when we noticed" differs per host.
	Cooldown time.Duration `json:"cooldown"`
}

// BreakerVerdict is the derived state plus the evidence it was derived from.
//
// The evidence is returned rather than kept private because a breaker that
// refuses without showing its working is indistinguishable from one that is
// broken, and the account it parked may be the last one in the pool.
type BreakerVerdict struct {
	State BreakerState `json:"state"`

	Failures  int `json:"failures"`
	Successes int `json:"successes"`

	LastFailureAt time.Time `json:"last_failure_at,omitempty"`
	LastSuccessAt time.Time `json:"last_success_at,omitempty"`

	// OpenUntil is when an open breaker becomes half-open. Zero unless the
	// state is BreakerOpen.
	OpenUntil time.Time `json:"open_until,omitempty"`
}

// Admits reports whether the verdict lets an ordinary caller proceed. A
// half-open breaker does NOT: only the probe holder may, and it learns that
// from AcquireProbe rather than from this.
func (v *BreakerVerdict) Admits() bool {
	return v != nil && v.State == BreakerClosed
}

// CircuitBreaker derives an account's breaker state and arbitrates the single
// half-open probe.
type CircuitBreaker interface {
	// Evaluate derives the state from the ledger. It returns an error and NO
	// verdict when the ledger could not be read: an unreadable failure history
	// is not a healthy account.
	Evaluate(ctx context.Context, tenantID, accountID string, policy BreakerPolicy, now time.Time) (*BreakerVerdict, error)

	// AcquireProbe wins the single half-open trial for holder, reporting
	// whether it was won. Losing is an ordinary outcome, not an error.
	AcquireProbe(ctx context.Context, tenantID, accountID, holder string, now time.Time, ttl time.Duration) (bool, error)

	// ReleaseProbe gives the probe back. It removes only a probe this holder
	// still owns, so a late release cannot clear someone else's.
	ReleaseProbe(ctx context.Context, tenantID, accountID, holder string) error
}

// breakerProbe is the stored half-open claim.
type breakerProbe struct {
	Holder    string    `json:"holder"`
	ExpiresAt time.Time `json:"expires_at"`
}

// ledgerCircuitBreaker is the ledger-derived CircuitBreaker.
type ledgerCircuitBreaker struct {
	ledger ConsumptionLedger
	kv     interfaces.KVStore

	creator guardedCreator
	deleter guardedDeleter
}

// Compile-time interface satisfaction check.
var _ CircuitBreaker = (*ledgerCircuitBreaker)(nil)

// NewCircuitBreaker returns a CircuitBreaker deriving state from ledger and
// arbitrating probes on kv.
//
// It needs BOTH guarded verbs. A guarded create paired with an unguarded delete
// is half a primitive: two releasers both observe the same holder, the first
// deletes, a third party legitimately claims the now-absent key, and the second
// delete removes THEIR probe.
func NewCircuitBreaker(ledger ConsumptionLedger, kv interfaces.KVStore) CircuitBreaker {
	breaker := &ledgerCircuitBreaker{ledger: ledger, kv: kv}
	if creator, ok := kv.(guardedCreator); ok {
		breaker.creator = creator
	}
	if deleter, ok := kv.(guardedDeleter); ok {
		breaker.deleter = deleter
	}
	return breaker
}

// =============================================================================
// Evaluate — the derived half
// =============================================================================

// Evaluate implements CircuitBreaker.
func (b *ledgerCircuitBreaker) Evaluate(ctx context.Context, tenantID, accountID string, policy BreakerPolicy, now time.Time) (*BreakerVerdict, error) {
	return EvaluateBreaker(ctx, b.ledger, tenantID, accountID, policy, now)
}

// EvaluateBreaker derives an account's breaker state from the ledger alone.
//
// IT IS THE WHOLE OF Evaluate, exported because the derivation needs nothing
// this file keeps. The breaker's only stored state is the half-open probe,
// which arbitrates who may TRY an account; a caller that merely wants to
// REPORT the state — the spend report does — has no business acquiring one,
// and would otherwise have to build a CircuitBreaker around a KV store it
// never touches. The probe stays behind the CircuitBreaker interface where it
// belongs.
func EvaluateBreaker(ctx context.Context, ledger ConsumptionLedger, tenantID, accountID string, policy BreakerPolicy, now time.Time) (*BreakerVerdict, error) {
	if ledger == nil {
		return nil, &BreakerInvalidError{Field: "ledger", Message: "must not be nil"}
	}
	if err := policy.validate(); err != nil {
		return nil, err
	}

	usage, err := ledger.WindowUsage(ctx, tenantID, accountID, now.Add(-policy.Window), now)
	if err != nil {
		// No verdict. An unreadable failure history is not a healthy account,
		// and a breaker that reported "closed" whenever it could not see would
		// open the pool up precisely when the store is in trouble.
		return nil, err
	}

	verdict := &BreakerVerdict{
		Failures:      usage.Failures,
		Successes:     usage.Successes,
		LastFailureAt: usage.LastFailureAt,
		LastSuccessAt: usage.LastSuccessAt,
		State:         BreakerClosed,
	}

	if !policy.tripped(usage) {
		return verdict, nil
	}

	// Measured from the account's own last failure rather than from the moment
	// this host noticed: the failure is a fact every host reads identically,
	// while "when we noticed" differs per host and would give each one its own
	// cooldown.
	opensAt := usage.LastFailureAt.Add(policy.Cooldown)
	if now.Before(opensAt) {
		verdict.State = BreakerOpen
		verdict.OpenUntil = opensAt
		return verdict, nil
	}
	verdict.State = BreakerHalfOpen
	return verdict, nil
}

// tripped applies the rule stated at the top of this file.
func (p BreakerPolicy) tripped(usage *ConsumptionRollup) bool {
	if usage.Failures < p.FailureThreshold {
		return false
	}
	// A success after the latest failure means the account is working again,
	// whatever the window's totals say. Without this the breaker would keep
	// tripping on failures the account has already recovered from, for as long
	// as they sat inside the window.
	return !usage.LastSuccessAt.After(usage.LastFailureAt)
}

// validate refuses a policy that cannot mean what it appears to.
func (p BreakerPolicy) validate() error {
	if p.Window <= 0 {
		return &BreakerInvalidError{Field: "window", Message: "must be positive"}
	}
	if p.FailureThreshold <= 0 {
		return &BreakerInvalidError{
			Field:   "failure_threshold",
			Message: "must be positive; a threshold of zero trips an account that has never failed",
		}
	}
	if p.Cooldown <= 0 {
		return &BreakerInvalidError{
			Field:   "cooldown",
			Message: "must be positive; a zero cooldown makes an open breaker half-open at once",
		}
	}
	return nil
}

// =============================================================================
// The probe — the one decision that cannot be derived
// =============================================================================

// AcquireProbe implements CircuitBreaker.
func (b *ledgerCircuitBreaker) AcquireProbe(ctx context.Context, tenantID, accountID, holder string, now time.Time, ttl time.Duration) (bool, error) {
	if b.creator == nil || b.deleter == nil {
		return false, &BreakerUnguardedError{
			Message: "a single half-open probe needs a store that can decide a create and a release; this one cannot",
		}
	}
	if err := validateProbeIdentity(tenantID, accountID, holder); err != nil {
		return false, err
	}
	if ttl <= 0 {
		return false, &BreakerInvalidError{
			Field:   "ttl",
			Message: "must be positive; a probe with no lifetime is abandoned the instant it is taken",
		}
	}

	key := breakerProbeKey(tenantID, accountID)
	mine, err := json.Marshal(breakerProbe{Holder: holder, ExpiresAt: now.Add(ttl).UTC()})
	if err != nil {
		return false, &BreakerInvalidError{Field: "probe", Message: err.Error()}
	}

	won, err := b.creator.PutIfAbsent(ctx, key, mine)
	if err != nil {
		return false, &BreakerProbeError{AccountID: accountID, Key: key, Cause: err}
	}
	if won {
		return true, nil
	}

	held, err := b.readProbe(ctx, key)
	if err != nil {
		// Losing the create and then finding nothing is a real interleaving:
		// the previous holder released between the two calls. We do not hold
		// the probe, which is what false says, and it is the truth rather
		// than a guess — a retry is the caller's to make.
		if errors.Is(err, ErrBreakerProbeAbsent) {
			return false, nil
		}
		return false, err
	}
	// Already ours and still live: re-entrant, so a caller that asks twice
	// while holding the probe is not told it lost its own claim.
	if held.probe.Holder == holder && now.Before(held.probe.ExpiresAt) {
		return true, nil
	}
	if now.Before(held.probe.ExpiresAt) {
		return false, nil
	}

	// The holder's probe has expired, which means it died mid-trial. Reclaim
	// it by deleting exactly the value observed and then creating our own.
	//
	// This DOES manufacture a moment when the key is absent, which the account
	// store refuses to do for its default pointer. The trade is different here
	// and the difference is the blast radius: losing that race costs one extra
	// probe turn against an account already suspected of failing, whereas a
	// vacant default pointer loses a claim another account legitimately took.
	// A guarded swap would remove even this; go-qrdb#56 tracks it.
	removed, err := b.deleter.CompareAndDelete(ctx, key, held.raw)
	if err != nil {
		return false, &BreakerProbeError{AccountID: accountID, Key: key, Cause: err}
	}
	if !removed {
		// Someone else reclaimed it first. They hold the probe; we do not.
		return false, nil
	}
	won, err = b.creator.PutIfAbsent(ctx, key, mine)
	if err != nil {
		return false, &BreakerProbeError{AccountID: accountID, Key: key, Cause: err}
	}
	return won, nil
}

// ReleaseProbe implements CircuitBreaker.
func (b *ledgerCircuitBreaker) ReleaseProbe(ctx context.Context, tenantID, accountID, holder string) error {
	if b.deleter == nil {
		return &BreakerUnguardedError{
			Message: "releasing a probe needs a store that can delete conditionally; this one cannot",
		}
	}
	// Validated on the release path too, and not only on acquire. A guard
	// written where one caller enters is a guard the other caller walks past:
	// an empty tenant here would build a key belonging to no account, find
	// nothing there, and report a successful release of a probe that is still
	// held.
	if err := validateProbeIdentity(tenantID, accountID, holder); err != nil {
		return err
	}
	key := breakerProbeKey(tenantID, accountID)
	held, err := b.readProbe(ctx, key)
	if err != nil {
		if errors.Is(err, ErrBreakerProbeAbsent) {
			// Nothing to release. Idempotent by design: a caller retrying a
			// release must not be made to handle a failure for succeeding.
			return nil
		}
		return err
	}
	if held.probe.Holder != holder {
		return &BreakerProbeNotHeldError{AccountID: accountID, Holder: holder, Actual: held.probe.Holder}
	}
	// Against the exact bytes observed: a release must remove the probe it
	// read and not whatever replaced it in between.
	if _, err := b.deleter.CompareAndDelete(ctx, key, held.raw); err != nil {
		return &BreakerProbeError{AccountID: accountID, Key: key, Cause: err}
	}
	return nil
}

// heldProbe is a decoded probe together with the exact bytes it decoded from,
// which are what a conditional delete must be given.
type heldProbe struct {
	probe breakerProbe
	raw   []byte
}

// readProbe loads and decodes the stored probe.
func (b *ledgerCircuitBreaker) readProbe(ctx context.Context, key string) (heldProbe, error) {
	raw, err := b.kv.Get(ctx, key)
	if err != nil {
		if qerrors.IsNotFound(err) {
			return heldProbe{}, &BreakerProbeAbsentError{Key: key}
		}
		return heldProbe{}, &BreakerProbeError{Key: key, Cause: err}
	}
	// An absent key is reported by some stores as a typed not-found and by
	// others as an empty value with no error. Both mean the same thing here,
	// and only one of them was handled: decoding the empty value produced
	// "unexpected end of JSON input", which a release then reported as a probe
	// fault rather than as nothing to release.
	if len(raw) == 0 {
		return heldProbe{}, &BreakerProbeAbsentError{Key: key}
	}
	var probe breakerProbe
	if err := json.Unmarshal(raw, &probe); err != nil {
		return heldProbe{}, &BreakerProbeError{Key: key, Cause: err}
	}
	return heldProbe{probe: probe, raw: raw}, nil
}

// breakerProbeKey builds the probe key for one account.
func breakerProbeKey(tenantID, accountID string) string {
	return breakerProbeKeyspace + tenantID + keySeparator + accountID
}

// validateProbeIdentity checks the three identifiers a probe operation is
// addressed by. Shared between acquire and release so the two cannot drift
// into disagreeing about what a valid probe address is.
func validateProbeIdentity(tenantID, accountID, holder string) error {
	for _, field := range []struct{ name, value string }{
		{"tenant_id", tenantID}, {"account_id", accountID}, {"holder", holder},
	} {
		if err := validateKeySegment(field.name, field.value); err != nil {
			var segment *KeySegmentError
			if errors.As(err, &segment) {
				return &BreakerInvalidError{Field: field.name, Message: segment.Message}
			}
			return &BreakerInvalidError{Field: field.name, Message: err.Error()}
		}
	}
	return nil
}

// =============================================================================
// Errors
// =============================================================================

// Sentinel errors for the circuit breaker.
var (
	// ErrBreakerInvalid indicates a breaker question this file refuses to
	// answer, as distinct from an account it refuses to admit.
	ErrBreakerInvalid = errors.New("breaker: invalid request")

	// ErrBreakerUnguarded indicates a store that cannot decide the single
	// half-open probe.
	ErrBreakerUnguarded = errors.New("breaker: store cannot decide a contested probe")

	// ErrBreakerProbe indicates the probe could not be read or written.
	ErrBreakerProbe = errors.New("breaker: probe operation failed")

	// ErrBreakerProbeAbsent indicates no probe is currently held.
	ErrBreakerProbeAbsent = errors.New("breaker: no probe is held")

	// ErrBreakerProbeNotHeld indicates a release by someone who is not the
	// holder.
	ErrBreakerProbeNotHeld = errors.New("breaker: probe is held by someone else")
)

// BreakerInvalidError reports a malformed breaker request or policy.
type BreakerInvalidError struct {
	Field   string
	Message string
}

// Error implements the error interface.
func (e *BreakerInvalidError) Error() string {
	return ErrBreakerInvalid.Error() + ": " + e.Field + ": " + e.Message
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *BreakerInvalidError) Unwrap() error { return ErrBreakerInvalid }

// BreakerUnguardedError reports a store that cannot arbitrate the probe.
//
// NO EMULATION, for the reason the whole probe exists: read-then-write would
// let two hosts each believe they hold the single trial, which is the failure
// the breaker is there to prevent, doubled.
type BreakerUnguardedError struct {
	Message string
}

// Error implements the error interface.
func (e *BreakerUnguardedError) Error() string {
	return ErrBreakerUnguarded.Error() + ": " + e.Message
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *BreakerUnguardedError) Unwrap() error { return ErrBreakerUnguarded }

// BreakerProbeError reports a failed probe read or write.
type BreakerProbeError struct {
	AccountID string

	// Key is the probe record the operation was addressed to. Without it a
	// read failure names nothing an operator can go and look at, which is the
	// difference between a report and a lead.
	Key string

	Cause error
}

// Error implements the error interface.
func (e *BreakerProbeError) Error() string {
	base := ErrBreakerProbe.Error()
	if e.AccountID != "" {
		base += " for account " + e.AccountID
	}
	if e.Key != "" {
		base += " at " + e.Key
	}
	if e.Cause != nil {
		return base + ": " + e.Cause.Error()
	}
	return base
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *BreakerProbeError) Unwrap() error { return ErrBreakerProbe }

// BreakerProbeAbsentError reports that no probe is held.
type BreakerProbeAbsentError struct {
	Key string
}

// Error implements the error interface.
func (e *BreakerProbeAbsentError) Error() string {
	return ErrBreakerProbeAbsent.Error() + ": " + e.Key
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *BreakerProbeAbsentError) Unwrap() error { return ErrBreakerProbeAbsent }

// BreakerProbeNotHeldError reports a release attempted by a non-holder.
type BreakerProbeNotHeldError struct {
	AccountID string
	Holder    string
	Actual    string
}

// Error implements the error interface.
func (e *BreakerProbeNotHeldError) Error() string {
	return ErrBreakerProbeNotHeld.Error() + ": " + e.Holder +
		" tried to release account " + e.AccountID + "'s probe, held by " + e.Actual
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *BreakerProbeNotHeldError) Unwrap() error { return ErrBreakerProbeNotHeld }
