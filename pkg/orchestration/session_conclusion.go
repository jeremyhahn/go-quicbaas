// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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
	"sync/atomic"
	"time"

	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

// =============================================================================
// Session conclusion — one owner, one transition
// =============================================================================
//
// A session ends in exactly one way, and until this file existed it ended in
// three. An operator's CloseSession tore down the backend, wrote the store and
// appended a terminal event. The supervisor's exit observer wrote the store and
// appended a terminal event. The spawn-time reconcile wrote the store. Each of
// them owned a FRAGMENT of the same transition, and every repair to one fragment
// broke an invariant of another:
//
//   - Closing a session ends its process, which fires the observer — so an
//     ordinary close wrote its terminal event and the observer wrote a second,
//     and an attacher saw the session end twice.
//   - Guarding that with a metadata read-then-write left the simultaneous case
//     open, because GetMeta, Append and PutMeta are three operations under three
//     locks with nothing owning the sequence.
//   - Deleting the live map entry on an exit made a later CloseSession
//     unreachable, and with it the backend teardown that releases the ephemeral
//     MCP certificate directory, the pooled account's credential tree and the
//     container env file.
//
// None of those is fixable in the fragment that shows it. What fixes all three
// is a single terminal transition that every caller routes through, decided by
// ONE compare-and-swap:
//
//	concluded.CompareAndSwap(false, true)
//
// The winner does everything — backend teardown, container stop, store, log
// metadata, and exactly one terminal event. Every later caller loses the swap
// and does NOTHING, and is told the session is already concluded rather than
// told it does not exist.
//
// WHY A CAS ON THE LIVE RECORD RATHER THAN A CHECK AGAINST THE STORE. The store
// has no compare-and-set on status, so a store-based check is the same
// read-then-write that failed. The live record is per session, is already shared
// by pointer through s.sessions, and an atomic.Bool on it is a real
// compare-and-swap: two callers arriving at the same instant produce one winner
// by construction rather than by timing.
//
// THE RECORD IS THEREFORE NOT DELETED. It carries the decision, so removing it
// would let a second conclusion win — and it is what lets a late CloseSession
// answer "already closed" instead of "not found". ListSessions takes the durable
// CLOSED status over the live map's assumption, so a retained record cannot make
// a concluded session look live.

// SessionConclusionReason names WHO ended a session and WHY, and is carried on
// the terminal event.
//
// An event that records only that a session ended leaves its reader to infer
// the cause from what happened next — which is exactly how a dispatch death got
// read as a hang. The reason is the attribution: it says what actually happened,
// at the moment it happened.
type SessionConclusionReason string

const (
	// ConcludedByOperator is a close somebody asked for.
	ConcludedByOperator SessionConclusionReason = "operator"

	// ConcludedByProcessExit is the agent process ending on its own — a crash,
	// an exhausted credential, a kill from outside the daemon.
	ConcludedByProcessExit SessionConclusionReason = "process-exit"

	// ConcludedByReconcile is a process found already gone at the moment its
	// record was persisted: it died inside the spawn's own window.
	ConcludedByReconcile SessionConclusionReason = "reconcile"

	// ConcludedByNodeRestart is a session whose process died with the daemon
	// that ran it and which the reconstitution path cannot bring back. The
	// record outlived the process; this is the record catching up.
	ConcludedByNodeRestart SessionConclusionReason = "node_restart"

	// ConcludedByJobConcluded is a session whose job reached a terminal state.
	// The work it was spawned for is over, so the session is too — whichever
	// route the job took to get there.
	ConcludedByJobConcluded SessionConclusionReason = "job_concluded"

	// ConcludedByFleetNodeGone is a session bound to a daemon that unregistered or
	// stopped heartbeating. The process is on a host that is no longer in the
	// fleet, so nothing here can reach it again.
	ConcludedByFleetNodeGone SessionConclusionReason = "agent_gone"

	// ConcludedBySessionIdle is a session bound to no job, not marked kept,
	// that stayed quiet past the idle bound. With no job to end it and no
	// operator holding it, nothing else ever would.
	ConcludedBySessionIdle SessionConclusionReason = "session_idle"
)

// sessionConclusionReasons is every reason a session may carry, keyed for
// O(1) validation of a value decoded from a durable record or a wire message.
var sessionConclusionReasons = map[SessionConclusionReason]struct{}{
	ConcludedByOperator:      {},
	ConcludedByProcessExit:   {},
	ConcludedByReconcile:     {},
	ConcludedByNodeRestart:   {},
	ConcludedByJobConcluded:  {},
	ConcludedByFleetNodeGone: {},
	ConcludedBySessionIdle:   {},
}

// Valid reports whether r is one of the reasons this daemon writes. An empty
// reason is NOT valid: a conclusion that names no cause is the state the
// attribution exists to remove.
func (r SessionConclusionReason) Valid() bool {
	_, ok := sessionConclusionReasons[r]
	return ok
}

// String implements fmt.Stringer.
func (r SessionConclusionReason) String() string { return string(r) }

// sessionConclusion is what the caller that won the arbitration publishes about
// the ending it owns, so every other caller can see how far it has got.
//
// THE REASON LIVES HERE, NOT IN THE ARGUMENT LIST, because the durable record
// and the terminal event must name the conclusion that actually happened. A
// loser retrying the persist carries its own reason — an exit observer racing
// an operator close would otherwise record the operator's ending as
// `process-exit`, attributing a deliberate act to a crash.
type sessionConclusion struct {
	// reason is the winner's, and the only one anything records.
	reason SessionConclusionReason

	// done is closed when the winner's teardown ATTEMPT has finished, whether
	// it succeeded or not. Until then nobody else may write anything durable.
	done chan struct{}

	// failed reports that the teardown did not succeed, so ownership was
	// released and there is nothing to persist. Read only after done is closed.
	failed atomic.Bool

	// err is what went wrong, for a caller that must report the outcome as its
	// own rather than merely react to it. Written before done is closed and
	// read only after, so the channel carries the happens-before.
	err error
}

// conclusionContract says what a caller's return value CLAIMS about the world,
// and it is why the two caller classes behave differently on losing.
//
// The semantics differ because the CONTRACT differs. An operator's close is a
// CLAIM: answering it is telling somebody that the session they asked to close
// is closed, so it may not be answered from an assumption about work that is
// still running. The supervisor's exit observer and the spawn-time reconcile are
// NUDGES: they report something they saw, carry their own retry cadence, and
// losing to whoever is already doing the work is the correct outcome for them.
type conclusionContract uint8

const (
	// conclusionNudge is a system caller. Losing is fire-and-forget.
	conclusionNudge conclusionContract = iota

	// conclusionClaim is an operator-facing caller. Losing means waiting for
	// the owner's result, within the caller's own deadline, and reporting it.
	conclusionClaim
)

// source renders the event source for a conclusion.
func (r SessionConclusionReason) source(creator string) string {
	if r == ConcludedByOperator {
		return "operator:" + creator
	}
	return "system:orchestrator"
}

// concludeSession performs the terminal transition exactly once.
//
// It reports whether THIS call won. A false return is not a failure: it means
// somebody else concluded the session and this caller must do nothing further —
// no second teardown, no second store write, no second terminal event.
//
// The order is deliberate and is the recovery argument. The backend teardown
// comes first, because it is the only step that releases resources outside this
// process and the only one whose omission leaks. Then the DURABLE half, in
// finishDurable: the log's terminal transition, the job's tidy-up, and LAST the
// session record — which is the commit point, so that "closed" means "complete"
// and every failure before it leaves a record the next pass retries in full.
//
// This paragraph used to say the store came second and the log last, "because
// the log is the operator's narrative and the least load-bearing of the three".
// That reasoning ranked the three writes by importance when what actually
// matters is which one other readers TRUST: a record marked closed is believed
// by everything, so writing it before the rest were done produced conclusions
// that were half applied and, for a session with no live entry, unrepairable.
// See finishDurable.
func (s *agentServiceImpl) concludeSession(
	ctx context.Context,
	tenantID, storageTenant, sessionID string,
	reason SessionConclusionReason,
	contract conclusionContract,
	backend AgentBackend,
) (bool, error) {
	if sessionID == "" {
		return false, nil
	}

	// NO LIVE RECORD MEANS THE DURABLE RECORD ANSWERS. Once a conclusion is
	// confirmed the live entry is deleted and the store becomes the tombstone
	// (see finishDurable), so a late caller asks the store: closed means
	// somebody already concluded this and there is nothing to do. Anything else
	// is a session this process cannot arbitrate — most likely one that died
	// inside the spawn window, before either record existed — and concluding it
	// un-arbitrated would be a second owner by another name. reconcile closes
	// that window from the other side, after the records exist.
	mem, hasMem := s.liveSessionRecord(sessionID)
	if !hasMem {
		return false, nil
	}

	owned := &sessionConclusion{reason: reason, done: make(chan struct{})}
	mem.conclusionMu.Lock()
	if !mem.concluded.CompareAndSwap(false, true) {
		owner := mem.conclusion.Load()
		mem.conclusionMu.Unlock()
		if contract == conclusionNudge {
			// A LOSER MAY ONLY RETRY THE PERSIST, AND ONLY ONCE THE TEARDOWN IS
			// KNOWN TO HAVE SUCCEEDED. Losing arbitration forbids re-doing the
			// work; it does not excuse leaving the record wrong. But "the winner
			// owns this" is not the same fact as "the teardown is finished", and
			// acting on the first while the second is still open is how a loser
			// marks a record CLOSED under its own reason while the winner is
			// still inside backend.Close.
			switch {
			case owner == nil:
				// A malformed or legacy in-flight record has no owner to follow.
				// This caller is only a nudge, so it leaves the next pass to sort
				// out the record.
				return false, nil
			case !concluded(owner):
				// Still tearing down. The winner persists when it is done.
				return false, nil
			case owner.failed.Load():
				// The teardown failed and ownership was released. Nothing was
				// concluded, so nothing is owed here; the next caller
				// re-arbitrates.
				return false, nil
			case mem.persisted.Load():
				return false, nil
			}
			// Teardown succeeded and the durable half is still owed — the
			// winner's store write failed. Retry that alone, under the WINNER's
			// reason.
			return false, s.finishDurable(ctx, mem, tenantID, storageTenant, sessionID, owner.reason)
		}

		// A CLAIM MAY NOT BE ANSWERED FROM AN ASSUMPTION. This caller lost, so
		// it does none of the work — but it reports the OWNER's outcome rather
		// than reporting success on the strength of somebody else having
		// started. Reporting success here while the winner was still inside
		// backend.Close meant an operator could hold a successful close for a
		// session whose teardown then failed and left the agent running.
		if owner != nil {
			return false, s.awaitConclusion(ctx, mem, owner, tenantID, storageTenant, sessionID)
		}

		// conclusionMu makes an elected-but-unpublished owner unreachable in
		// normal service code. If a manually installed or legacy record still
		// presents that shape, the close cannot learn the outcome and must not
		// claim success.
		if err := ctx.Err(); err != nil {
			return false, conclusionInProgress(sessionID, err)
		}
		return false, conclusionInProgress(sessionID, nil)
	}

	// Publish the conclusion BEFORE the teardown, so a caller that loses the
	// swap in the next instant sees an owner in flight rather than an absence
	// it might read as "nobody is doing this".
	mem.conclusion.Store(owned)
	mem.conclusionMu.Unlock()

	teardownErr := s.concludeBackend(ctx, mem, storageTenant, sessionID, backend)
	if teardownErr != nil {
		// The reason is recorded before the announcement, so a caller waiting on
		// this conclusion reports what actually failed rather than that
		// something did.
		owned.err = teardownErr
		owned.failed.Store(true)
	}
	// Announce the attempt's end before anything reads its result.
	close(owned.done)

	if teardownErr != nil {
		// THE RIGHT TO CONCLUDE IS GIVEN BACK. A teardown that failed for a
		// reason other than "already gone" means the process may still be
		// running, so this session is NOT concluded — and holding the swap would
		// make it permanently unconcludable, which is the same stranded state
		// the whole issue is about. Releasing it turns a failed close into a
		// retryable one.
		//
		// The handoff lock keeps a new winner from publishing while this failed
		// owner is being withdrawn.
		mem.conclusionMu.Lock()
		if mem.conclusion.Load() == owned {
			mem.concluded.Store(false)
			mem.conclusion.Store(nil)
		}
		mem.conclusionMu.Unlock()
		return false, teardownErr
	}

	if err := s.finishDurable(ctx, mem, tenantID, storageTenant, sessionID, owned.reason); err != nil {
		// The teardown stands — `concluded` is deliberately NOT released, or
		// the next caller would tear down a process that is already gone. What
		// is owed is the durable half, and the next caller will retry exactly
		// that.
		return false, err
	}
	return true, nil
}

// finishDurable completes everything a conclusion owes and, last, records the
// terminal state.
//
// THE CLOSED RECORD IS THE COMMIT POINT, AND IT IS WRITTEN LAST. This order is
// the whole recovery argument, and it was the other way round until a reviewer
// followed the failure through: the store write came first, so a `concludeLog`
// that then failed left a record saying CLOSED over a log that was still open
// and a terminal event that was never appended — and NOTHING revisited it. The
// live path could retry, because its retry runs off the live record; but a
// session with no live entry is revisited only by the next reconciliation
// pass, which reads the record, sees `closed`, and returns immediately
// (session_reconcile.go). Half a conclusion, unrepairable, which is precisely
// the class of partial write this whole change exists to remove.
//
// Writing the status LAST makes "closed" mean "complete" by construction, so
// the cheap test every other reader already performs — is this record closed?
// — becomes a correct completeness test, and no auxiliary scan has to exist to
// find half-finished endings. Everything before the commit is idempotent, so a
// pass that resumes a partial conclusion repeats work rather than duplicating
// it:
//
//   - the log gate is shut only while it is open, and the terminal event is
//     appended only while the log does not already end in one;
//   - the job's audit line is appended only while the trail does not already
//     name this session's ending.
//
// The cost is the inverse window — a record still ACTIVE whose log is already
// closed — and it is the harmless one: the session's process is already gone
// (the teardown ran first), the shut gate refuses any further event, and the
// next pass finishes the record. A false ACTIVE is retried; a false CLOSED is
// believed.
//
// RETRYABLE UNTIL CONFIRMED. Every step is attempted by whichever caller
// arrives while `persisted` is still false — the original winner first, and any
// later loser after it — so a store that was briefly unavailable does not leave
// a dead session durably ACTIVE.
//
// THE EVICTION HAPPENS EXACTLY ONCE, gated by the second swap.
func (s *agentServiceImpl) finishDurable(
	ctx context.Context,
	mem *sessionRecord,
	tenantID, storageTenant, sessionID string,
	reason SessionConclusionReason,
) error {
	// THE LOG TRANSITION IS PART OF THE CONCLUSION, not a postscript to it, and
	// its failure is returned rather than logged.
	//
	// PutMeta is what closes the gate that stops every other writer appending
	// after the ending. Treating its failure as a warning and appending the
	// terminal event anyway left the gate OPEN behind a session that had
	// already ended — so an ordinary output event could still land after it —
	// and then marking the conclusion persisted and evicting the live record
	// threw away the state a retry would have needed. The failure is the
	// caller's to see, and the conclusion stays retryable until the log agrees.
	// THE LOG DECIDES THE ATTRIBUTION, AND THE OTHER TWO WRITES FOLLOW IT.
	//
	// Each of the three durable artifacts is arbitrated, but they were
	// arbitrated INDEPENDENTLY, so different daemons could win different ones:
	// a sweep could win the log's terminal event while an operator's close won
	// the record's compare-and-set, leaving the transcript naming one cause and
	// the tombstone another — contradictory durable evidence, which is worse
	// for an auditor than either answer alone.
	//
	// The log's append is the arbitration every daemon already shares, so it is
	// made the single one: concludeLog reports the reason that ACTUALLY landed —
	// this caller's if it won, the winner's if it did not — and the job audit
	// and the record below are written under that. The ordering is unchanged:
	// the record is still the last write and still the commit point.
	effective, err := s.concludeLog(ctx, mem, tenantID, sessionID, reason)
	if err != nil {
		return err
	}
	reason = effective

	// THE JOB IS TOLD BEFORE THE RECORD IS CLOSED, for the same reason. A job
	// whose SessionIDs still name a closed session is the false-liveness bug
	// one level up, and doing this after the commit point would make it the
	// step no retry could reach — the hole that had just been closed for the
	// log, reopened one line further down. It is also why this lives here
	// rather than in the reconciliation wrapper: an operator's close and a
	// supervisor's exit observer owe the job the same tidy-up as a sweep does.
	if err := s.recordSessionConclusionOnJob(ctx, mem, tenantID, sessionID, reason); err != nil {
		return err
	}

	if s.store != nil {
		// A record the store does not have is one there is nothing to mark
		// closed — vacuously recorded, not a failure. Treating it as a failure
		// would make such a session permanently unconcludable, which is the
		// stranded state in the other direction: retryable-until-confirmed only
		// helps if "confirmed" is reachable.
		if err := s.store.ConcludeSession(ctx, storageTenant, sessionID, reason); err != nil &&
			!errors.Is(err, ErrAgentStoreNotFound) {
			return &AgentServiceError{
				Sentinel: ErrAgentServiceUnavailable,
				Message: "session " + sessionID + " was concluded (" + reason.String() +
					") and its terminal state could not be recorded: " + err.Error(),
			}
		}
	}

	if !mem.persisted.CompareAndSwap(false, true) {
		// Another retry confirmed it first. Everything above is idempotent; the
		// eviction below is that caller's.
		return nil
	}

	// THE DURABLE RECORD IS NOW THE TOMBSTONE, so the live entry is released.
	// Keeping it was what made idempotence work before this split, and it made
	// the map grow without bound — one retained sessionRecord, with its
	// tenant-supplied strings, per session the daemon ever ran. Now that the
	// store is confirmed CLOSED, a late caller reading it there gets the same
	// answer the retained entry used to give, and the memory is bounded by the
	// number of LIVE sessions rather than by the number of sessions ever.
	s.sessions.Delete(sessionID)
	return nil
}

// concludeBackend releases the agent process and anything the spawn provisioned
// alongside it.
//
// A RESUMABLE session has no process — it was reconstituted from the durable
// record at boot and its agent is recreated on the next prompt — so asking the
// backend to close it fails with "session not found" and would leave the record
// open. Concluding it is the operator declining the resume, which is a
// legitimate ending and needs no teardown.
func (s *agentServiceImpl) concludeBackend(
	ctx context.Context,
	mem *sessionRecord,
	storageTenant, sessionID string,
	backend AgentBackend,
) error {
	if backend != nil && !mem.resumable.Load() {
		// A teardown failure is RETURNED, not logged and stepped over: the
		// process may still be running, and a conclusion written over a live
		// agent is the false-liveness state inverted. "Already gone" is the one
		// failure that is really a success — it is what a process that has
		// already exited leaves behind.
		if err := backend.Close(ctx, sessionID); err != nil && !isSessionGoneError(err) {
			return err
		}
	}
	// The container stop is best effort by contrast, and the asymmetry is
	// deliberate: the agent process is the thing whose survival would make the
	// conclusion false, while a container that will not stop is a resource to
	// chase rather than a session that is still running.
	if mem.ContainerID != "" && s.containerRuntime != nil {
		if err := s.containerRuntime.Stop(ctx, mem.ContainerID); err != nil {
			s.logger.Warnf("session %s: container %s could not be stopped: %s",
				sessionID, truncateID(mem.ContainerID), err.Error())
		}
	}

	// THE CREDENTIAL'S DELIVERY GRANT IS WITHDRAWN HERE, and its failure is
	// RETURNED.
	//
	// This is what makes the ending a security control rather than only
	// hygiene. The process is gone and the materialized credential directory
	// has been released — but the account's envelope still exists in the vault,
	// and until the grant is revoked the host that held this copy may ask for
	// another one (pkg/service/account_materialize.go refuses only a revoked
	// grant). A session whose grant could not be withdrawn is therefore NOT
	// concluded: the caller sees the failure, the record stays as it was, and
	// the next pass retries the whole teardown. The alternative — logging it and
	// marking the session closed — is the one outcome nobody can repair,
	// because a closed record is never revisited.
	//
	// Revocation is idempotent (pkg/lease tombstones rather than deletes), so a
	// retry converges instead of failing on the grant the previous attempt
	// already withdrew.
	if err := s.revokeSessionCredentialLeases(ctx, mem, storageTenant, sessionID); err != nil {
		return err
	}
	// The named workspace's spec directory goes with the process that read
	// it: it holds the forge token, and nothing after the teardown needs it.
	mem.workspace.Release()
	return nil
}

// revokeSessionCredentialLeases withdraws every delivery grant this session
// holds and no other live session still holds.
//
// It reads the grants from the LIVE record, which both conclusion paths
// populate: the ordinary path from the spawn, and the absent path from the
// durable record it reconstructed the live shell out of
// (session_reconcile.go). That is what makes "revoked on session close" true
// for a node restart and a failed job as well as for an operator's close.
//
// A GRANT CAN BE SHARED, AND A SHARED ONE IS NOT WITHDRAWN UNTIL ITS LAST
// HOLDER ENDS. An in-process spawn leases its own hand-over, so its grant is
// its own and the scan below finds no other holder. A worker daemon's
// credential cache is keyed by account by design — a burst of spawns onto one
// account collapses onto one release, and therefore onto one grant
// (pkg/orchestration/fleet/account_grant.go) — so two concurrent sessions on that host can be
// serving under the same grant. Withdrawing it on the first conclusion would
// refuse the other session's next refresh and break a job nobody ended, which
// is a worse failure than the one the grant exists to prevent. So the last
// holder's conclusion is what withdraws it, and "revoked on session close"
// reads, precisely: revoked once no live session is still being served under it.
func (s *agentServiceImpl) revokeSessionCredentialLeases(
	ctx context.Context, mem *sessionRecord, storageTenant, sessionID string) error {
	if s.accountLeases == nil {
		return nil
	}
	leaseIDs := mem.CredentialLeaseIDs()
	if len(leaseIDs) == 0 {
		return nil
	}

	held, err := s.credentialLeasesHeldElsewhere(ctx, storageTenant, sessionID)
	if err != nil {
		return err
	}

	revoked := 0
	for _, leaseID := range leaseIDs {
		if _, shared := held[leaseID]; shared {
			s.logger.Debugf(
				"session %s: credential delivery lease %s is still held by another live session; not revoking",
				sessionID, leaseID)
			continue
		}
		if err := s.accountLeases.Revoke(ctx, leaseID); err != nil {
			return &AgentServiceError{
				Sentinel: ErrAgentServiceUnavailable,
				Message: "session " + sessionID +
					" cannot be concluded while its credential delivery lease " + leaseID +
					" is still live: " + err.Error(),
			}
		}
		revoked++
	}
	s.logger.Debugf("session %s: revoked %d of %d credential delivery lease(s)",
		sessionID, revoked, len(leaseIDs))
	return nil
}

// credentialLeasesHeldElsewhere reports which delivery grants a session OTHER
// than sessionID is still being served under.
//
// It reads the DURABLE records rather than the live map because the live map is
// one node's view: a grant taken out for a session that landed on another node
// of the cluster is invisible there, and treating it as unheld would withdraw
// it underneath that node. The records are the fleet-wide fact.
//
// A store that cannot be read FAILS the conclusion. The alternative — assuming
// nothing else holds the grant — revokes on a guess, which is the one outcome
// that breaks a running session; the conclusion is retryable and the next pass
// reads the store again.
//
// ONE BENIGN RACE, ACCEPTED DELIBERATELY. This runs BEFORE the concluding record
// is marked closed, so if the last two sessions holding a shared grant conclude
// simultaneously on two nodes, each scan still sees the other's record open, both
// skip the revocation, and the grant survives until its TTL lapses — after which
// Validate refuses it from its own ExpiresAt and the reaper tombstones it. That
// is the safe direction, and every other ordering trades it for something worse:
// revoking before checking breaks a running session, marking the record closed
// first reintroduces the unrepairable state concludeBackend exists to avoid, and
// locking across two nodes' conclusions puts distributed coordination on the
// teardown path to shorten a one-hour window on a grant that authorises nothing
// but a RE-DELIVERY to a host with no live session left on it.
func (s *agentServiceImpl) credentialLeasesHeldElsewhere(
	ctx context.Context, storageTenant, sessionID string) (map[string]struct{}, error) {
	held := make(map[string]struct{})
	if s.store == nil {
		return held, nil
	}

	records, err := s.store.ListSessions(ctx, storageTenant)
	if err != nil {
		return nil, &AgentServiceError{
			Sentinel: ErrAgentServiceUnavailable,
			Message: "session " + sessionID +
				" cannot be concluded because the sessions still holding its credential" +
				" delivery leases could not be read: " + err.Error(),
		}
	}

	for _, record := range records {
		if record == nil || record.SessionID == sessionID {
			continue
		}
		// A session that has already ended holds nothing. CLOSED is the
		// tombstone; ERROR is a persistent session whose resume failed and
		// which will not run again until an operator acts on it (see
		// SessionRecord.Resumable) -- it has no process to refresh with, and a
		// resume that does eventually happen re-resolves and takes a grant of
		// its own. Every other state, RESUMABLE included, is a session that can
		// still be served under the grant.
		if record.Status == SessionStateClosed || record.Status == SessionStateError {
			continue
		}
		for _, leaseID := range record.CredentialLeaseIDs {
			if leaseID != "" {
				held[leaseID] = struct{}{}
			}
		}
	}
	return held, nil
}

// concludeLog writes the session's narrative ending: exactly one terminal
// event, carrying who ended it and why, and the metadata that a listing reads.
//
// Retryable in both halves and exactly-once in its effect: the metadata gate is
// shut only while it is open, and the terminal event is appended only while the
// log does not already end in one. A retry that arrives after either half
// completed repeats no work; a retry that arrives after either half FAILED
// finishes it.
func (s *agentServiceImpl) concludeLog(
	ctx context.Context,
	mem *sessionRecord,
	tenantID, sessionID string,
	reason SessionConclusionReason,
) (SessionConclusionReason, error) {
	if s.sessionLog == nil {
		// No log means no arbiter, and none is needed: the store's own
		// compare-and-set is then the only durable decision, and it is
		// single-valued by construction.
		return reason, nil
	}

	// ONE CRITICAL SECTION FOR READ, CLOSE, APPEND. The metadata is what gates
	// every other writer, so deciding to close it and writing the ending have
	// to be one step: two conclusion callers that both read ACTIVE would both
	// append a terminal event, and "always last" would hold for neither.
	mem.logMu.Lock()
	defer mem.logMu.Unlock()

	// THE METADATA IS READ FIRST, AND ITS ABSENCE MEANS NO TERMINAL EVENT.
	//
	// KindSessionClosed is the last event of a session, and the KV log derives
	// the next sequence from the metadata — so an event appended before the
	// session is registered lands at a sequence the registration then resets,
	// and the next append overwrites it. A session that ended before it was
	// registered would have its ending erased by its own beginning. A log with
	// no metadata has no session to terminate.
	meta, err := s.sessionLog.GetMeta(ctx, tenantID, sessionID)
	if errors.Is(err, ErrSessionLogNotFound) {
		// A log with no metadata has no session to terminate, and that is an
		// answer rather than a failure.
		return reason, nil
	}
	if err != nil {
		// A METADATA READ THAT FAILED IS NOT A SESSION THAT WAS NEVER
		// REGISTERED. Both were treated as "nothing to end" here, so a store
		// that could not answer produced a conclusion marked persisted over a
		// log that was never closed — and the error naming the real cause was
		// discarded on the way past.
		return "", &AgentServiceError{
			Sentinel: ErrAgentServiceUnavailable,
			Message: "session " + sessionID + " was concluded (" + reason.String() +
				") and its log metadata could not be read: " + err.Error(),
		}
	}

	now := time.Now().UTC()

	// THE GATE IS SHUT BEFORE THE ENDING IS WRITTEN. The append gate refuses a
	// non-terminal event to a session whose metadata says closed; writing the
	// ending first left a window in which a racing chunk read ACTIVE, passed
	// the gate, and landed after it. And if this write fails there is no
	// ending: appending one behind an open gate would put the invariant in a
	// state nothing could repair.
	if meta.Status != SessionStateClosed {
		meta.Status = SessionStateClosed
		meta.LastActivity = now
		if putErr := s.sessionLog.PutMeta(ctx, meta); putErr != nil {
			return "", &AgentServiceError{
				Sentinel: ErrAgentServiceUnavailable,
				Message: "session " + sessionID + " was concluded (" + reason.String() +
					") and its log could not be closed: " + putErr.Error(),
			}
		}
	}

	// THE ENDING ITSELF IS THE EXACTLY-ONCE MARKER, NOT THE GATE THAT PRECEDES
	// IT. Reading CLOSED metadata and returning was treating "the gate is shut"
	// as "the ending is written", and the two are separated by a failure: an
	// append that did not land left a session whose metadata said closed and
	// whose log had no ending, and every retry read that metadata and declined
	// to write the one event that was missing. So the log is asked what it
	// actually holds, and a read that cannot answer is an error rather than an
	// assumption — this decides whether the conclusion is durably recorded.
	//
	// IT IS ALSO WHERE THE WINNING REASON COMES FROM. The ending already on the
	// log was written by whichever caller won the append, and that caller may
	// be on ANOTHER DAEMON with a different reason -- a sweep racing an
	// operator's close. Reporting this caller's own reason back would let the
	// record and the job audit be attributed to a conclusion that did not win,
	// so the transcript said one cause and the tombstone another.
	endedReason, ended, err := sessionLogEnding(ctx, s.sessionLog, tenantID, sessionID)
	if err != nil {
		return "", &AgentServiceError{
			Sentinel: ErrAgentServiceUnavailable,
			Message: "session " + sessionID + " was concluded (" + reason.String() +
				") and its log ending could not be read: " + err.Error(),
		}
	}
	if ended {
		// A previous caller wrote it. Shutting an already-shut gate above was a
		// no-op, and this is the retry observing its own completed work -- or
		// another daemon's.
		return effectiveConclusionReason(endedReason, reason), nil
	}

	if appendErr := AppendNextSessionEvent(ctx, s.sessionLog, &SessionEvent{
		SessionID: sessionID,
		TenantID:  tenantID,
		Timestamp: now,
		Source:    reason.source(sessionCreator(ctx)),
		Kind:      KindSessionClosed,
		Closed: &SessionClosedPayload{
			Reason: reason.String(),
		},
	}); appendErr != nil {
		if errors.Is(appendErr, ErrSessionLogConcluded) {
			// THE LOG REFUSING A SECOND ENDING IS THIS STEP SUCCEEDING. An
			// ending appeared between the probe above and this append — another
			// process concluding the same session, since callers inside this one
			// are serialized by logMu. What this step is for is that the session
			// has an ending, not that this caller wrote it; treating the refusal
			// as a failure would leave a conclusion retrying forever against a
			// log that is already complete.
			//
			// The winner's reason is read back for the same purpose as above:
			// this caller lost, so the attribution it carries downstream is the
			// one that landed, not the one it proposed.
			racedReason, racedEnded, readErr := sessionLogEnding(ctx, s.sessionLog, tenantID, sessionID)
			if readErr != nil || !racedEnded {
				// The ending is there -- the append just said so -- but it could
				// not be read back. Reporting an unverified attribution would be
				// a guess; the caller's own reason is the honest answer, and the
				// store's compare-and-set still refuses to overwrite a complete
				// ending.
				return reason, nil
			}
			return effectiveConclusionReason(racedReason, reason), nil
		}
		return "", &AgentServiceError{
			Sentinel: ErrAgentServiceUnavailable,
			Message: "session " + sessionID + " was concluded (" + reason.String() +
				") and its ending could not be appended: " + appendErr.Error(),
		}
	}
	// This caller won the append, so its reason is the one the fleet agrees on.
	return reason, nil
}

// effectiveConclusionReason picks the attribution a conclusion carries onward.
//
// The reason already on the log wins, because the log's append is the one
// decision every daemon shares. An EMPTY recorded reason does not win: an
// ending with no cause on it is not an attribution, and letting it displace a
// real one would lose information rather than arbitrate it.
func effectiveConclusionReason(recorded, own SessionConclusionReason) SessionConclusionReason {
	if recorded == "" {
		return own
	}
	return recorded
}

// sessionLogEnded reports whether the session's terminal event has actually
// landed in the log.
//
// KindSessionClosed is the last event a session can hold — the append path
// refuses anything else once the metadata gate is shut — so the LAST event is
// the only one that has to be read to answer this.
//
// LatestSequence IS A HINT HERE, NOT THE TAIL. The KV log answers it from the
// session metadata, and rolls that metadata forward AFTER the event lands, in a
// second write whose failure it does not report — so the cheap answer can sit
// behind events that are really there. Reading the one event at that sequence
// would then read past neither: it would read BEHIND the ending and report a
// concluded session as unfinished, and the retry would append a second ending.
// So the hint is used as a starting point and the remainder is read from it;
// when the metadata is current — the ordinary case — that remainder is one
// event.
// sessionLogEnding reports the ending already written to a session's log, if
// there is one, together with the reason it carries.
//
// THE LOG IS THE ARBITER OF ATTRIBUTION, so its terminal event is where the
// winning reason is read back from. AppendNextSessionEvent admits exactly one
// KindSessionClosed per session across every writer -- the sequence CAS and the
// terminal gate between them leave no interleaving in which two land -- so the
// reason on that event is the one thing about a conclusion that a whole fleet
// already agrees on.
func sessionLogEnding(
	ctx context.Context, log SessionEventLog, tenantID, sessionID string,
) (SessionConclusionReason, bool, error) {
	latest, err := log.LatestSequence(ctx, tenantID, sessionID)
	if err != nil {
		return "", false, err
	}
	events, _, err := log.Range(ctx, tenantID, sessionID, latest, 0)
	if err != nil {
		return "", false, err
	}
	if len(events) == 0 {
		return "", false, nil
	}
	last := events[len(events)-1]
	if last.Kind != KindSessionClosed {
		return "", false, nil
	}
	if last.Closed == nil {
		// An ending with no payload still ends the session; it simply names no
		// cause, and an empty reason is not allowed to overwrite a real one.
		return "", true, nil
	}
	return SessionConclusionReason(last.Closed.Reason), true, nil
}

func sessionLogEnded(ctx context.Context, log SessionEventLog, tenantID, sessionID string) (bool, error) {
	latest, err := log.LatestSequence(ctx, tenantID, sessionID)
	if err != nil {
		return false, err
	}
	events, _, err := log.Range(ctx, tenantID, sessionID, latest, 0)
	if err != nil {
		return false, err
	}
	if len(events) == 0 {
		return false, nil
	}
	return events[len(events)-1].Kind == KindSessionClosed, nil
}

// conclusionInProgress reports that somebody else owns this session's ending
// and this caller could not learn the outcome. It is a failure to ANSWER, not a
// failure of the session, and it is never reported as a close that succeeded.
func conclusionInProgress(sessionID string, cause error) error {
	msg := "session " + sessionID + " is being concluded by another caller"
	if cause != nil {
		msg += ": " + cause.Error()
	}
	return &AgentServiceError{Sentinel: ErrSessionConclusionInProgress, Message: msg}
}

// awaitConclusion waits for the owner's ending and reports ITS result as this
// caller's own.
//
// BOUNDED BY THE CALLER'S OWN DEADLINE, not by a timeout of its own: the wait
// belongs to whoever is waiting, and an operator's close already carries the
// deadline that says how long they are prepared to wait. A ctx that expires
// mid-wait yields the in-progress error — the caller has learned that a
// conclusion is running and nothing more, which is exactly what it then says.
//
// A teardown failure becomes this caller's failure. Success is reported only
// once the durable half is confirmed: either the owner already recorded it, or
// this caller records it here and answers on the result of that.
func (s *agentServiceImpl) awaitConclusion(
	ctx context.Context,
	mem *sessionRecord,
	owner *sessionConclusion,
	tenantID, storageTenant, sessionID string,
) error {
	select {
	case <-owner.done:
	case <-ctx.Done():
		return conclusionInProgress(sessionID, ctx.Err())
	}

	if owner.failed.Load() {
		// The agent process may still be running and ownership has been given
		// back, so the session is NOT closed. Saying so is the whole point of
		// waiting.
		if owner.err != nil {
			return owner.err
		}
		return conclusionInProgress(sessionID, nil)
	}

	if mem.persisted.Load() {
		return nil
	}

	// The teardown succeeded and the durable half may still be owed. Recording
	// it is idempotent and is what turns "the owner said it worked" into a
	// confirmed close; if it fails, that failure is this caller's answer.
	return s.finishDurable(ctx, mem, tenantID, storageTenant, sessionID, owner.reason)
}

// concluded reports whether a published conclusion's teardown attempt has
// finished. Non-blocking on purpose: a caller that merely lost the arbitration
// must not be parked for the length of somebody else's backend teardown, and
// the winner will do the durable write itself when it gets there.
func concluded(owner *sessionConclusion) bool {
	select {
	case <-owner.done:
		return true
	default:
		return false
	}
}

// isSessionGoneError reports whether a backend refused a teardown because the
// session was already gone.
//
// That is a SUCCESS for a conclusion, not a failure: it is the state a process
// that has already exited leaves behind, and treating it as an error would make
// the ordinary exit path log a warning every time.
func isSessionGoneError(err error) bool {
	if err == nil {
		return false
	}
	var notFound *SessionNotFoundError
	if errors.As(err, &notFound) || errors.Is(err, ErrSessionNotFound) {
		return true
	}
	// A REMOTE BACKEND'S "GONE" DOES NOT ARRIVE AS A SENTINEL. The daemon
	// answers NotFound and RemoteACPBackend wraps that status in a
	// RemoteBackendRPCError, so neither errors.Is nor errors.As above can see
	// it: a sentinel does not survive a wire. The consequence was the one an
	// operator actually meets — closing a session whose remote daemon is long
	// gone returned "acp: session not found" and refused, so the records that
	// most needed closing were the only ones the close verb could not touch.
	// That is the stranded listing, defended by the verb meant to clear it.
	if st, ok := grpcstatus.FromError(err); ok && st.Code() == codes.NotFound {
		return true
	}
	return false
}

// sessionConcluded reports whether a session has already ended.
//
// IT ASKS BOTH PLACES, because a conclusion moves the answer from one to the
// other. While the ending is in flight — and while a failed durable write keeps
// it in flight — the live record carries it; once the terminal state is
// confirmed the live record is released and the durable one is the tombstone.
// A reader that consulted only the map would call a concluded session live the
// moment it was tidied away, which is the false-liveness bug inverted.
//
// A store that cannot answer yields false: refusing a turn on the strength of
// an unreadable record would take a working session away on a failed read.
func (s *agentServiceImpl) sessionConcluded(ctx context.Context, sessionID string) bool {
	if mem, ok := s.liveSessionRecord(sessionID); ok {
		return mem.concluded.Load()
	}
	if s.store == nil {
		return false
	}
	record, err := s.store.GetSession(ctx, StorageTenant(ctx), sessionID)
	return err == nil && record.Status == SessionStateClosed
}

// RecordSessionExit concludes a session whose agent process ended without
// anyone asking it to.
//
// The observer the process supervisor calls. It is a thin routing shim over the
// one transition, because "the process died" and "an operator closed it" differ
// only in the reason they record — everything else a session's ending has to do
// is the same, and giving the two paths their own copies of it is what produced
// the duplicate events and the orphaned teardown this file replaced.
func (s *agentServiceImpl) RecordSessionExit(ctx context.Context, tenantID, sessionID, reason string) {
	backend := s.backendForSession(ctx, sessionID)
	concluded, err := s.concludeSession(ctx, tenantID, tenantID, sessionID, ConcludedByProcessExit, conclusionNudge, backend)
	switch {
	case err != nil:
		s.logger.Warnf("session %s exited and could not be concluded: %s", sessionID, err.Error())
	case concluded:
		s.logger.Warnf("session %s concluded: %s", sessionID, reason)
	}
}

// backendForSession resolves the backend holding a session, or nil.
//
// Nil is an ordinary answer rather than an error: a conclusion still has a
// store record and a log to close when the backend that held the process can no
// longer be resolved, and refusing to conclude on that basis would leave the
// record open precisely when the process is most certainly gone.
func (s *agentServiceImpl) backendForSession(ctx context.Context, sessionID string) AgentBackend {
	name, err := s.findSessionBackend(sessionID)
	if err != nil {
		return nil
	}
	backend, err := s.lookupBackendCtx(s.pinSessionBackend(ctx, sessionID), name, "")
	if err != nil {
		return nil
	}
	return backend
}
