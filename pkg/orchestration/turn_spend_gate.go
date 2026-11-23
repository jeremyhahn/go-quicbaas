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
	"strings"
	"sync"
	"time"
)

// ErrSpendCheckUnavailable is a turn refused because the account record or the
// spend ledger could not be read, so the cap could not be checked. It is an
// environment fault: the turn never ran and the attempt is not charged.
var ErrSpendCheckUnavailable = errors.New("account: the spend cap could not be read")

// SpendCheckUnavailableError is the per-turn spend check failing on a read of
// the account record or the spend ledger. It unwraps to both
// ErrSpendCheckUnavailable and the read error, so errors.Is still finds
// ErrAccountNotFound or the store's own error.
type SpendCheckUnavailableError struct {
	SessionID string
	AccountID string
	Err       error
}

func (e *SpendCheckUnavailableError) Error() string {
	var b strings.Builder
	b.WriteString(ErrSpendCheckUnavailable.Error())
	b.WriteString(": session ")
	b.WriteString(strconv.Quote(e.SessionID))
	b.WriteString(", account ")
	b.WriteString(strconv.Quote(e.AccountID))
	if e.Err != nil {
		b.WriteString(": ")
		b.WriteString(e.Err.Error())
	}
	return b.String()
}

// Unwrap returns the sentinel and the read error.
func (e *SpendCheckUnavailableError) Unwrap() []error {
	return []error{ErrSpendCheckUnavailable, e.Err}
}

// spendCheckFault classifies an error from the per-turn spend check. A
// cancelled caller, a typed refusal (quota exhausted, breaker, account limited,
// consumption pending and the rest) and a code or wiring defect (unknown
// verdict, authority config) pass through unchanged; any other error is a read
// of the account or the ledger that failed, and becomes a
// *SpendCheckUnavailableError.
func spendCheckFault(ctx context.Context, sessionID, accountID string, err error) error {
	if err == nil || ctx.Err() != nil {
		return err
	}
	if _, typed := classifyEnvironmentFault(err); typed {
		return err
	}
	var unknown *AccountSpendUnknownVerdictError
	var config *SpendAuthorityConfigError
	if errors.As(err, &unknown) || errors.As(err, &config) {
		return err
	}
	return &SpendCheckUnavailableError{SessionID: sessionID, AccountID: accountID, Err: err}
}

// =============================================================================
// The per-turn spend gate — the cap is asked again before every turn.
//
// The spawn gate (accountResolver.authorizeSpend) and the remote release gate
// (AccountService.MaterializeAccountCredential) ask the spend authority ONCE,
// when the credential is handed out. A session then runs as many turns as it
// likes against that credential, so one that outlives its account's window —
// an hour-long worker crossing a five-hour cap part way through — is never
// asked again (go-quicbaas#782).
//
// This gate asks the SAME authority before each turn, from the account the
// live session record names now (rotation moves it), and refuses the turn
// before the backend runs it. A refused turn is therefore never charged, and
// the refusal is the spawn gate's own typed error, so it classifies as
// account_quota_exhausted exactly as a refused spawn does.
//
// Every turn is driven from here, including a turn on a session a remote
// daemon hosts: the control plane prompts it through runPromptTurn, so the
// envelope that daemon cached at materialization stops being usable for the
// next turn the moment the account is over its cap.
//
// THE COST IS ONE ACCOUNT READ AND ONE BOUNDED LEDGER SCAN PER TURN, and no
// cached verdict sits in front of it: a cache would be a second, staler
// authority, which is the disagreement asking the one authority avoids. A turn
// is minutes of agent work; the scan is not what it waits on.
// =============================================================================

// SetSpendAuthority wires the spend cap every pooled session's turn is checked
// against. It is the authority the spawn gate asks, so the two cannot disagree
// about whether an account is over its cap. Leaving it unset gates a session
// only at spawn.
func (s *agentServiceImpl) SetSpendAuthority(spend SpendAuthority) {
	s.turnSpend = spend
}

// authorizeTurnSpend asks the spend authority whether the pooled account
// serving sessionID may run one more turn.
//
// A session with no pooled account, or a service with no authority or account
// store wired, is admitted: there is no cap to check. On a registry miss the
// durable session record names the account (turnSessionAccount), so a turn
// sent through a node that does not hold the session live is still checked.
// A session record, an account record or a spend ledger that cannot be read
// refuses the turn — a cap that cannot be
// checked is not a cap that was met, the same fail-closed rule the authority
// applies — as a *SpendCheckUnavailableError: a spend_check_unavailable
// environment fault, requeued and never charged, since no turn ran. A typed
// refusal (quota exhausted, breaker, account limited, consumption pending)
// keeps its own class, and a cancelled caller gets its context's error.
//
// The estimate is zero, as at spawn: nothing measures what a turn will cost
// before it runs, so the honest question is whether the account is ALREADY
// over.
func (s *agentServiceImpl) authorizeTurnSpend(ctx context.Context, sessionID string) error {
	if s.turnSpend == nil || s.accountStore == nil || sessionID == "" {
		return nil
	}
	tenantID, accountID, err := s.turnSessionAccount(ctx, sessionID)
	if err != nil {
		return spendCheckFault(ctx, sessionID, "", err)
	}
	if accountID == "" {
		return nil
	}
	account, err := s.accountStore.Get(ctx, tenantID, accountID)
	if err != nil {
		return spendCheckFault(ctx, sessionID, accountID, err)
	}
	err = s.turnSpend.Authorize(ctx, SpendAuthorizeRequest{Account: account}, time.Now().UTC())
	return spendCheckFault(ctx, sessionID, accountID, err)
}

// turnSessionAccount names the tenant and pooled account serving sessionID.
// The live registry answers first. On a miss the durable record answers: a
// session spawned through another node of the cluster has a record but no
// entry in this node's registry, and its turn is still capped. A record
// that does not exist means no node spawned the session, so it has no
// account. A record that cannot be read is returned as the error.
func (s *agentServiceImpl) turnSessionAccount(ctx context.Context, sessionID string) (tenantID, accountID string, err error) {
	if mem, ok := s.liveSessionRecord(sessionID); ok {
		return mem.TenantID, mem.AccountID(), nil
	}
	tenantID = TenantFromContext(ctx)
	if s.store == nil {
		return tenantID, "", nil
	}
	record, err := s.store.GetSession(ctx, StorageTenant(ctx), sessionID)
	switch {
	case err == nil:
		return record.TenantID, record.AccountID, nil
	case errors.Is(err, ErrAgentStoreNotFound):
		return tenantID, "", nil
	default:
		return tenantID, "", err
	}
}

// lockSessionTurn waits until no other turn is running on sessionID and
// returns the function that lets the next one in.
//
// The spend check is only as good as the ledger it reads, and a turn's usage
// reaches the ledger only when the turn is over. Turns on one session that
// overlap — several PromptSessionAsync calls, which a backend then queues
// behind its own session lock — would each pass the check against a ledger
// that does not yet hold the turns ahead of them. Holding this lock from the
// check to the usage record makes every turn ask after the one before it was
// charged.
//
// A caller whose context ends while it waits gets the context's error and runs
// nothing. An empty session ID is not locked: there is no session to share.
// A session's entry is removed when its last holder or waiter leaves, so the
// registry holds only sessions with a turn running or queued.
func (s *agentServiceImpl) lockSessionTurn(ctx context.Context, sessionID string) (func(), error) {
	if sessionID == "" {
		return func() {}, nil
	}
	lock := s.turnLocks.acquire(sessionID)
	select {
	case lock.slot <- struct{}{}:
		return func() {
			<-lock.slot
			s.turnLocks.release(sessionID, lock)
		}, nil
	case <-ctx.Done():
		s.turnLocks.release(sessionID, lock)
		return nil, ctx.Err()
	}
}

// sessionTurnLocks is one turn lock per session ID, kept only while a turn
// holds it or waits on it. The zero value is ready to use.
type sessionTurnLocks struct {
	mu    sync.Mutex
	locks map[string]*sessionTurnLock
}

// sessionTurnLock is one session's slot and how many callers hold or wait on it.
type sessionTurnLock struct {
	slot chan struct{}
	refs int
}

// acquire returns sessionID's lock, creating it, and counts the caller in.
func (l *sessionTurnLocks) acquire(sessionID string) *sessionTurnLock {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.locks == nil {
		l.locks = make(map[string]*sessionTurnLock)
	}
	lock, ok := l.locks[sessionID]
	if !ok {
		lock = &sessionTurnLock{slot: make(chan struct{}, 1)}
		l.locks[sessionID] = lock
	}
	lock.refs++
	return lock
}

// release counts the caller out and drops the entry when nobody is left.
func (l *sessionTurnLocks) release(sessionID string, lock *sessionTurnLock) {
	l.mu.Lock()
	defer l.mu.Unlock()
	lock.refs--
	if lock.refs == 0 {
		delete(l.locks, sessionID)
	}
}

// size is how many sessions have a lock entry now.
func (l *sessionTurnLocks) size() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.locks)
}
