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
	"math"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
)

// =============================================================================
// Balancing a pool across its live sessions
// =============================================================================
//
// A POOL OF EQUIVALENT ACCOUNTS IS ONLY A POOL IF WORK SPREADS ACROSS IT. The
// static strategy answers the scope's default account every time, so a tenant
// with three pooled Claude accounts ran every worker on one of them until the
// provider refused it, and the other two were never selected (burn-in,
// 2026-09-22, go-quicbaas#1046). This file supplies what selection was
// missing: how many sessions each account is serving right now, and whether
// the operator capped that number.
//
// THE COUNT IS READ FROM THE SESSION RECORDS, NOT FROM THE DELIVERY LEASES.
// The leases looked like the obvious signal: one is written per credential
// delivery and revoked when the session ends. But a worker daemon caches a
// released credential per account and serves every session it spawns on that
// account under the SAME grant (pkg/fleet/account_grant.go). So a daemon
// running three sessions on one account holds one lease, and a lease count
// would undercount by exactly the concurrency this exists to measure. The
// durable SessionRecord names the account every session was spawned on,
// whichever host resolved it, and it is already the record the session
// conclusion scans to decide whether a shared grant is still in use. Counting
// it adds no bookkeeping and cannot drift from what is running.

// AccountCredentialExpiryBuffer is how close to its recorded expiry an account
// may be and still be handed to a NEW session.
//
// Two minutes: long enough that a session does not start on a credential that
// lapses before its first turn completes the handshake, and short enough that
// it does not meaningfully shrink the pool. It applies only to credentials
// QuicBaaS cannot renew (see Account.AcceptsNewSession). An in-flight session
// is never affected; this is a selection filter, not a revocation.
const AccountCredentialExpiryBuffer = 2 * time.Minute

// liveSessionStates are the session states that occupy an account. A resumable
// session has no process until it is resumed, and a resume selects its account
// again. A closed or errored session holds nothing.
var liveSessionStates = map[SessionState]bool{
	SessionStateActive: true,
	SessionStateIdle:   true,
}

// MaxAccountSessionCap is the largest cap an account may store. It is the
// wire's bound: the gRPC field is an int32, and a stored cap the wire cannot
// carry would read back as a different number, or as zero, which means
// unmetered.
const MaxAccountSessionCap = math.MaxInt32

// validateSessionCap rejects a cap that is not a count the wire can carry.
func (a *Account) validateSessionCap() error {
	if a.MaxConcurrentSessions < 0 || a.MaxConcurrentSessions > MaxAccountSessionCap {
		return &AccountInvalidError{
			Field:   "max_concurrent_sessions",
			Message: "must be zero (unmetered) or a positive session count no greater than " + strconv.Itoa(MaxAccountSessionCap),
		}
	}
	return nil
}

// AtSessionCap reports whether an account serving live sessions has no room
// for another one. An unmetered account (MaxConcurrentSessions == 0) is never
// at its cap.
func (a *Account) AtSessionCap(live int) bool {
	return a != nil && a.MaxConcurrentSessions > 0 && live >= a.MaxConcurrentSessions
}

// AcceptsNewSession reports whether the account may be handed to a NEW session
// at now. It is Selectable plus the expiry buffer.
//
// THE BUFFER APPLIES ONLY TO A CREDENTIAL NOBODY WILL RENEW. An oauth_refresh
// account is renewed by the token manager when a spawn resolves it, so its
// mirrored expiry being close is the refresher's business and excluding it
// would take a working account out of the pool for no reason. A static token
// close to its declared expiry has no such rescue, and a session started on it
// would fail partway through. An account whose expiry is unknown (zero) is not
// excluded, because zero means unknown, not expired.
func (a *Account) AcceptsNewSession(now time.Time) bool {
	return a.Selectable(now) && !a.CredentialExpiresSoon(now)
}

// CredentialExpiresSoon reports whether the account's credential expires
// inside AccountCredentialExpiryBuffer of now and nobody will renew it — the
// half of AcceptsNewSession a pinned new session is held to as well.
func (a *Account) CredentialExpiresSoon(now time.Time) bool {
	if a.RefreshPolicy.IsOAuthRefresh() || a.CredentialExpiresAt.IsZero() {
		return false
	}
	return !a.CredentialExpiresAt.After(now.Add(AccountCredentialExpiryBuffer))
}

// AccountSessionLookup answers how many live sessions each account is serving.
//
// It is BATCHED by design: selection asks about a whole candidate list at once,
// and the production answer is one scan of the tenant's session records,
// whatever the candidate count.
type AccountSessionLookup interface {
	// LiveSessions returns the live-session count for each of accountIDs. An
	// account serving nothing may be absent from the map; a caller reads a
	// missing entry as zero.
	LiveSessions(ctx context.Context, tenantID string, accountIDs []string) (map[string]int, error)
}

// SessionRecordLister is the one AgentStore method the session counter reads.
// Narrowed so the counter can be built over any store that lists sessions.
type SessionRecordLister interface {
	ListSessions(ctx context.Context, tenantID string) ([]*SessionRecord, error)
}

// sessionRecordCounter counts live sessions from the durable session records,
// and is the RESERVATION TABLE that makes the count true while spawns are in
// flight.
//
// COUNTING RECORDS ALONE LAGS THE TRUTH. A session record is written only after
// the spawn it describes has succeeded, so a burst of resolutions all read the
// pre-spawn count: every one of them passes a cap of one, and every one of them
// picks the same "least busy" account (Codex, go-quicbaas#1047 rounds 2-3). The
// fix is the ordinary one: every resolution CLAIMS a slot on the account it
// chose, under one lock with the count it checks, and the claim is counted by
// every reader before the spawn has produced anything.
//
// OCCUPANCY IS TRACKED FOR EVERY ACCOUNT; THE CAP IS ENFORCED ONLY WHERE ONE IS
// SET. An unmetered account is reserved like any other, because balancing needs
// its in-flight spawns counted exactly as much as a cap does.
//
// A reservation lives until one of three things happens:
//
//   - the spawn's session record NAMES it (SessionRecord.ReservationIDs): the
//     ID travels from the resolution onto the Session, across the daemon's
//     spawn response, and onto the durable record, and the record then counts
//     that session itself. Nothing is matched by account, job or time;
//   - the spawn fails: the resolver or the backend releases it (Release);
//   - AccountSessionReservationTTL passes: a reservation whose spawn neither
//     produced a record nor was released (a crashed daemon, a lost release) is
//     dropped rather than holding a slot forever.
//
// The table lives in this process. There is one control plane per tenant pool
// in every deployment this ships to; a second control-plane node would keep a
// table of its own, and the counts would then hold per node rather than across
// them. That is recorded rather than hidden (docs/software-factory/accounts.md).
type sessionRecordCounter struct {
	sessions SessionRecordLister
	now      func() time.Time
	newID    func() string

	// tenantMu serializes every read-decide-claim sequence within a tenant.
	// Reservations are tenant-scoped, so tenants never contend.
	tenantMu keyedMutex

	// mu guards pending itself (the map), not the decisions made over it:
	// those are serialized per tenant by tenantMu.
	mu      sync.Mutex
	pending map[string][]*sessionReservation
}

// AccountSessionReservationTTL bounds how long a reservation holds a session
// slot without being named by a session record or released.
//
// Fifteen minutes: longer than any spawn takes to produce its record (a
// container start, a checkout and the CLI's handshake, which is minutes at
// worst), and short enough that a reservation orphaned by a crashed daemon
// frees its slot well inside one subscription window.
const AccountSessionReservationTTL = 15 * time.Minute

// sessionReservation is one claimed, not-yet-recorded session slot.
type sessionReservation struct {
	id        string
	accountID string
	at        time.Time
}

// SessionReservationRequest asks for one session slot.
type SessionReservationRequest struct {
	// Account is the account the slot is claimed on. Its MaxConcurrentSessions
	// is the cap enforced; zero enforces none, and the slot is still counted.
	Account *Account

	// ExpectLive makes the claim conditional: it is refused with
	// AccountSessionCountChangedError unless the account's live count is
	// still ExpectedLive, the count the caller selected on. This is what
	// makes selection and claim one decision: a resolution that chose on a
	// count another claim has since moved re-selects instead of piling onto
	// the account that was least busy a moment ago.
	ExpectLive   bool
	ExpectedLive int

	// ReplacesSessionID names a session this claim reconstitutes, whose own
	// record must not count against the slot it is claiming. After a daemon
	// restart a persistent session keeps its durable idle status while it has
	// no process; its resume re-claims the same pinned account, and counting
	// its own record there made a session on a cap-one account refuse itself
	// as saturated (Codex, #1047 round 4). Empty excludes nothing.
	ReplacesSessionID string
}

// AccountSessionReserver is an AccountSessionLookup that can also claim a
// session slot before the session exists, and give it back.
type AccountSessionReserver interface {
	AccountSessionLookup

	// Reserve claims one session slot and returns the reservation's ID. An
	// account already at a MaxConcurrentSessions cap is refused with
	// AccountsSaturatedError; a conditional claim whose expected count no
	// longer holds is refused with AccountSessionCountChangedError.
	Reserve(ctx context.Context, tenantID string, req SessionReservationRequest) (string, error)

	// Release gives a reservation back. Releasing an empty or unknown ID
	// succeeds: a reservation that was satisfied or expired has nothing left
	// to give back.
	Release(ctx context.Context, tenantID, accountID, reservationID string) error
}

// Compile-time interface satisfaction check.
var _ AccountSessionReserver = (*sessionRecordCounter)(nil)

// NewAccountSessionCounter returns the production AccountSessionReserver over
// the session records.
func NewAccountSessionCounter(sessions SessionRecordLister) (AccountSessionReserver, error) {
	if sessions == nil {
		return nil, &AccountInvalidError{Field: "sessions", Message: "the live-session count is read from the session store"}
	}
	return &sessionRecordCounter{
		sessions: sessions,
		now:      func() time.Time { return time.Now().UTC() },
		newID:    uuid.NewString,
		pending:  make(map[string][]*sessionReservation),
	}, nil
}

// LiveSessions implements AccountSessionLookup: live session records plus the
// reservations no record names yet.
func (c *sessionRecordCounter) LiveSessions(ctx context.Context, tenantID string, accountIDs []string) (map[string]int, error) {
	counts := make(map[string]int, len(accountIDs))
	if len(accountIDs) == 0 {
		return counts, nil
	}
	unlock := c.tenantMu.lock(tenantID)
	defer unlock()

	all, err := c.tally(ctx, tenantID, "")
	if err != nil {
		return nil, err
	}
	for _, id := range accountIDs {
		if n := all[id]; n > 0 {
			counts[id] = n
		}
	}
	return counts, nil
}

// Reserve implements AccountSessionReserver.
func (c *sessionRecordCounter) Reserve(ctx context.Context, tenantID string, req SessionReservationRequest) (string, error) {
	account := req.Account
	if account == nil {
		return "", &AccountInvalidError{Field: "account", Message: "a reservation is taken on an account"}
	}
	unlock := c.tenantMu.lock(tenantID)
	defer unlock()

	// THE COUNT IS READ INSIDE THE LOCK. Reading it outside and claiming
	// inside would reopen the race this exists to close.
	all, err := c.tally(ctx, tenantID, req.ReplacesSessionID)
	if err != nil {
		return "", err
	}
	live := all[account.ID]
	if req.ExpectLive && live != req.ExpectedLive {
		return "", &AccountSessionCountChangedError{AccountID: account.ID, Expected: req.ExpectedLive, Actual: live}
	}
	if account.AtSessionCap(live) {
		return "", &AccountsSaturatedError{
			TenantID: tenantID, ProjectID: account.ProjectID, Provider: account.Provider, Candidates: 1,
			Occupancy: sessionCapSummary([]*Account{account}, all),
		}
	}
	reservation := &sessionReservation{id: c.newID(), accountID: account.ID, at: c.now()}
	c.mu.Lock()
	c.pending[tenantID] = append(c.pending[tenantID], reservation)
	c.mu.Unlock()
	return reservation.id, nil
}

// Release implements AccountSessionReserver.
func (c *sessionRecordCounter) Release(_ context.Context, tenantID, accountID, reservationID string) error {
	if reservationID == "" {
		return nil
	}
	unlock := c.tenantMu.lock(tenantID)
	defer unlock()
	c.mu.Lock()
	defer c.mu.Unlock()
	kept := make([]*sessionReservation, 0, len(c.pending[tenantID]))
	for _, r := range c.pending[tenantID] {
		if r.id == reservationID && r.accountID == accountID {
			continue
		}
		kept = append(kept, r)
	}
	c.storePendingLocked(tenantID, kept)
	return nil
}

// tally counts every account's live sessions in tenantID: live records plus
// reservations no record names and that have not expired. The record of
// excludeSessionID, when set, is not counted (SessionReservationRequest). It drops the
// reservations it finds named or expired. The caller holds tenantMu for
// tenantID.
func (c *sessionRecordCounter) tally(ctx context.Context, tenantID, excludeSessionID string) (map[string]int, error) {
	records, err := c.sessions.ListSessions(ContextWithTenant(ctx, tenantID), tenantID)
	if err != nil {
		return nil, &AccountSessionCountError{TenantID: tenantID, Cause: err}
	}
	counts := make(map[string]int)
	// named is every reservation a record claims, whatever the record's
	// state: a session that has already ended still satisfied its
	// reservation, and counting the reservation again would resurrect it.
	named := make(map[string]struct{})
	for _, record := range records {
		if record == nil {
			continue
		}
		for _, id := range record.ReservationIDs {
			named[id] = struct{}{}
		}
		if excludeSessionID != "" && record.SessionID == excludeSessionID {
			// The session being reconstituted: the slot it holds is the one
			// being claimed, so it is not counted twice.
			continue
		}
		if record.AccountID != "" && liveSessionStates[record.Status] {
			counts[record.AccountID]++
		}
	}

	now := c.now()
	c.mu.Lock()
	defer c.mu.Unlock()
	kept := make([]*sessionReservation, 0, len(c.pending[tenantID]))
	for _, r := range c.pending[tenantID] {
		if now.Sub(r.at) >= AccountSessionReservationTTL {
			continue
		}
		if _, ok := named[r.id]; ok {
			continue
		}
		counts[r.accountID]++
		kept = append(kept, r)
	}
	c.storePendingLocked(tenantID, kept)
	return counts, nil
}

// storePendingLocked writes back a tenant's reservation list, dropping the
// entry when it is empty so an idle tenant holds no memory. c.mu is held.
func (c *sessionRecordCounter) storePendingLocked(tenantID string, kept []*sessionReservation) {
	if len(kept) == 0 {
		delete(c.pending, tenantID)
		return
	}
	c.pending[tenantID] = kept
}

// AccountSessionCountChangedError refuses a conditional reservation whose
// account's live count moved after the caller selected on it. It is not a
// failure: the caller re-selects on the current counts.
type AccountSessionCountChangedError struct {
	AccountID string
	Expected  int
	Actual    int
}

// Error implements the error interface.
func (e *AccountSessionCountChangedError) Error() string {
	return ErrAccountSessionCountChanged.Error() + ": account=" + e.AccountID +
		" expected=" + strconv.Itoa(e.Expected) + " actual=" + strconv.Itoa(e.Actual)
}

// Unwrap returns the sentinel for errors.Is support.
func (e *AccountSessionCountChangedError) Unwrap() error { return ErrAccountSessionCountChanged }

// ErrAccountSessionCountChanged is the sentinel of
// AccountSessionCountChangedError.
var ErrAccountSessionCountChanged = errors.New("account: live-session count changed since selection")

// AccountSessionCountError reports a live-session count that could not be read.
//
// Selection FAILS CLOSED on it. A count that could not be read is not a count
// of zero, and treating it as one would put every session on whichever account
// the cap was protecting.
type AccountSessionCountError struct {
	TenantID string
	Cause    error
}

// Error implements the error interface.
func (e *AccountSessionCountError) Error() string {
	msg := "account: live sessions of tenant " + e.TenantID + " could not be counted"
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap returns the store failure.
func (e *AccountSessionCountError) Unwrap() error { return e.Cause }

// sessionCapSummary renders how a saturated pool is occupied, for the reason
// an operator reads.
func sessionCapSummary(accounts []*Account, live map[string]int) string {
	summary := ""
	for _, account := range accounts {
		if summary != "" {
			summary += ", "
		}
		summary += account.ID + " " + strconv.Itoa(live[account.ID]) + "/" +
			strconv.Itoa(account.MaxConcurrentSessions)
	}
	return summary
}
