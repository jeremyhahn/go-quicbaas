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

package quicbaas

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/cas"
	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
)

// ClaimsPrefix stores one firing-claim mark per job.
//
// RAW KV, data-access.md §3 case 5 (internal cluster coordination metadata),
// with case 6 identity (the key is externally derived, not generated). This is
// a permanent classification, not an interim shortcut:
//
//   - The record coordinates the SCHEDULER'S OWN dispatch across nodes. It
//     carries no tenant payload — a job ID and a firing instant, nothing
//     barrier-worthy — so every claim lives in one SYSTEM keyspace regardless
//     of which tenant owns the job. Routing claims per job owner would put two
//     contending nodes in two different keyspaces, leaving nothing to
//     arbitrate.
//   - Its identity is the job ID, externally derived, so there is no generated
//     ID for a DAO to assign.
//   - The DAO shape was ALSO measured broken for this record class when it was
//     written — the tenant stores' PutIfAbsent silently degraded to
//     check-then-put (go-qrdb#30) and their QueryIndex was a no-op returning
//     (nil, nil) (go-qrdb#31). Both are FIXED as of go-qrdb v0.8.0-alpha, so
//     this is history, not a live reason. The classification above does not
//     depend on it: §3 makes raw CAS the sanctioned form for a record of this
//     class whether or not the DAO path works.
//
// The "__sys:" prefix is go-qrdb's reserved internal namespace
// (service.InternalKeyPrefixSystem), and it is load-bearing rather than
// decorative: keys under it are excluded from user-facing List and Scan and
// refused by MVCC restore (service.IsInternalKey), so scheduler arbitration
// state cannot be enumerated by an ordinary tenant sweep or rolled back
// through a restore. The neighbouring job, execution and counter records were
// moved into the same namespace when docs/18 V24 was remediated; nothing in
// this package writes a platform record under a bare "sys:" prefix any more.
//
// It is a DISCOVERY guard, not a confidentiality boundary, and not a write
// ACL: go-qrdb documents the filter as read-only (a caller naming the reserved
// prefix outright is served, and the write path is left to authorization). It
// is claimed here for the enumeration protection, not as an access control.
const ClaimsPrefix = "__sys:scheduler/claims/"

// ClaimTransactor is the go-qrdb surface that decides a firing claim.
//
// It is go-qrdb's own transport.TransactionClient, aliased here only so the
// dependency reads as what it is. Arbitration is an ExecuteTransaction — a
// single Raft proposal whose compare and write are evaluated together inside
// the state machine — so it is atomic by construction and independent of seal
// and barrier configuration. This is the documented consumption pattern:
// go-qrdb's SDK records go-xkms's CA key store reaching PutIfAbsent and
// CompareAndSwap through this same service (sdk/go/types_service.go:86-89).
//
// quicbaas composes a request here; it does not implement the primitive.
type ClaimTransactor = transport.TransactionClient

// ClaimReader reads a job's current mark so the advance can be guarded against
// it. It is go-qrdb's transport.KVClient.
//
// A separate read is needed because qrdb's transaction branches carry only Put
// and Delete — an OpGet in the failure branch is rejected — so the mark cannot
// be fetched in the same proposal that fails to create it. The read is
// unguarded on purpose: it only supplies the value the following transaction
// compares against, and if it is stale that compare fails and the claim is lost,
// which is the safe direction.
type ClaimReader = transport.KVClient

// keyForClaim builds the storage key holding a job's claim mark.
//
// One key per job, not per firing — see claimMark for why.
//
// The scope comes from scopeToken — the same function that scopes job IDs
// everywhere, not a copy of its rule. Job IDs are unique only within a scope, so ID 7 names
// a system job and a job in every tenant; a mark keyed by ID alone conflated
// them, and DeleteJob for a tenant's job 7 erased the system job 7's mark,
// un-deciding a decided firing. Sharing the function is the fix: the mark's
// identity is exactly as unique as the job's, and cannot drift from it.
func keyForClaim(jobID uint64, tenantID string) string {
	return ClaimsPrefix + scopeToken(tenantID) + strconv.FormatUint(jobID, 10)
}

// firingKey renders the firing identity a claim decides.
//
// Nanoseconds, not a formatted time: the value round-trips exactly, and two
// nodes rendering the same instant produce the same string, which a locale- or
// precision-dependent format would not guarantee. Two nodes racing on one due
// job read the same NextRun from the same replicated job record, so they derive
// byte-identical identities without consulting a local clock — the property the
// whole mechanism rests on.
func firingKey(jobID uint64, firing time.Time) string {
	return strconv.FormatUint(jobID, 10) + "/" + strconv.FormatInt(firing.UnixNano(), 10)
}

// claimMark is a job's high-water mark: the latest firing any node has claimed.
//
// One monotonic mark per job, rather than one record per firing, and the choice
// is load-bearing rather than tidy. A per-firing record has to be pruned or it
// grows without bound, and pruning is unsafe: deleting a claim key un-decides a
// decided firing. A contender that read job J at firing T and then stalled — a
// GC pause, a slow proposal, a healing partition — still holds T; if another
// node meanwhile executes T, advances the job and prunes T's key, the stalled
// contender's create-if-absent finds the key absent, wins, and runs T a second
// time. No prune horizon closes that without a liveness signal proving no
// contender is still in flight, and that signal is a clock, excluded from this
// decision by design.
//
// The mark removes the problem instead of bounding it. A stale contender for T
// loses because the mark has already moved past T, and growth is one record per
// job forever, so there is nothing to prune. Found by review before it shipped;
// see docs/18 V4.
//
// The cost, stated because it is real: the mark only moves forward, so a job
// whose next firing is ever moved BELOW the mark becomes unclaimable. That errs
// toward a miss rather than a duplicate, which is the direction this whole
// mechanism is chosen for; TriggerJob is the operator bypass, and a pending job
// whose NextRun sits at or below its mark with no live run is an invariant
// violation for WP10's reaper to surface.
type claimMark struct {
	JobID uint64 `json:"job_id"`

	// FiringNanos is the claimed firing instant, and the only field any
	// decision reads. The rest is diagnostic.
	FiringNanos int64 `json:"firing_nanos"`

	TenantID  string    `json:"tenant_id,omitempty"`
	Owner     string    `json:"owner,omitempty"`
	ClaimedAt time.Time `json:"claimed_at"`
}

// ClaimFiring atomically claims the exclusive right to execute one firing of
// one job, and reports whether this caller won it.
//
// Arbitration is go-qrdb's transaction plane, never this method's. Two shapes,
// both a single proposal whose compare and write are evaluated together:
//
//   - No mark yet: create it, guarded by CompareCreateRevision == 0 ("this key
//     has never been written").
//   - Mark exists and is behind this firing: advance it, guarded by
//     CompareValue equal to the exact bytes just read.
//
// Either guard failing means another node got there first, which is a loss, not
// an error. A mark already at or past this firing is likewise a loss without a
// write — that is the case that makes a stalled contender stand down.
//
// A FAILED CAS IS A LOSS, NEVER A RETRY — and that holds even though the shared
// primitive (pkg/cas) re-reads after a lost swap, because for THIS record the
// re-read can only decline. When two nodes race one firing, the winner advances
// the mark to at-or-past that firing, so the loser's next read lands in the
// refusal branch below and the call ends in a loss. The no-retry rule is a
// property of the high-water mark, expressed in the decide function, rather
// than a special case in the loop.
//
// This is where the claim DIVERGES from pkg/sdlc's leases, which share the same
// loop: a lease can move to a generation still below the writer's, so its
// re-read may legitimately decide to take again. A mark cannot — it only moves
// forward, and forward is past you.
//
// The transaction owns every GRANT. The one decision made client-side is the
// refusal when the mark is already at or past this firing, and it is a refusal
// by construction: there is no proposal to make, because writing would move a
// monotonic mark backwards. Refusing without the store is fail-closed; granting
// without it never happens.
//
// Reading and then writing without a guard would be two proposals with a window
// between them, and that window is exactly the Raft leader-churn race this
// exists to close.
//
// Only an undecidable claim returns an error, and callers must treat that as
// "do not execute".
func (s *ClusteredStorage) ClaimFiring(ctx context.Context, claim *scheduler.FiringClaim) (bool, error) {
	if claim == nil {
		return false, &scheduler.ErrStorageError{Op: "ClaimFiring", Cause: ErrNilClaim}
	}
	if claim.JobID == 0 {
		return false, scheduler.InvalidFiringClaimError{
			Field:   "job_id",
			Message: "a claim must name the job it fires",
		}
	}
	if claim.FiringTime.IsZero() {
		return false, scheduler.InvalidFiringClaimError{
			Field:   "firing_time",
			Message: "a zero firing time would collapse every firing of a job onto one claim",
		}
	}

	key := keyForClaim(claim.JobID, claim.TenantID)
	mark := &claimMark{
		JobID:       claim.JobID,
		FiringNanos: claim.FiringTime.UnixNano(),
		TenantID:    claim.TenantID,
		Owner:       claim.Owner,
		ClaimedAt:   claim.ClaimedAt,
	}
	data, err := json.Marshal(mark)
	if err != nil {
		return false, &scheduler.ErrStorageError{Op: "ClaimFiring.marshal", Cause: err}
	}

	return s.writer.Handover(ctx, key, data, func(held []byte) (cas.Decision, error) {
		var current claimMark
		if err := json.Unmarshal(held, &current); err != nil {
			return cas.Decline, &scheduler.ErrStorageError{Op: "ClaimFiring.unmarshal", Cause: err}
		}
		if current.FiringNanos <= 0 {
			// Decodable but meaningless — "{}" unmarshals happily to a zero
			// mark. Treating that as "behind every firing" would let any claim
			// advance past it, so a mark that decodes without saying where the
			// job stands is refused exactly like one that does not decode.
			return cas.Decline, &scheduler.ErrStorageError{
				Op:    "ClaimFiring.read",
				Cause: ErrUnreadableClaimMark,
			}
		}
		if current.FiringNanos >= mark.FiringNanos {
			// Another node already claimed this firing or a later one. This is
			// the branch a stalled contender lands in, and the branch in which
			// the monotonic mark's one cost becomes visible.
			//
			// It is also what makes the SHARED handover loop behave here the
			// way a claim must: declining is terminal, so a lost swap re-reads,
			// finds the mark at or past this firing, declines again, and the
			// call ends in a loss. The claim's no-retry rule is a consequence
			// of this predicate rather than a special case in the loop.
			s.noteIfStranded(ctx, claim, current.FiringNanos)
			return cas.Decline, nil
		}
		return cas.Take, nil
	})
}

// noteIfStranded records the case where the mark's monotonicity has made a
// firing permanently unclaimable.
//
// The mark only moves forward, so a job whose NextRun is at or below its mark
// can never be claimed again: every node refuses in the branch above and the
// job sits pending forever. That errs toward a miss rather than a duplicate,
// which is the direction this whole mechanism is chosen for, and TriggerJob
// remains the operator bypass — but it is silent, so it is counted.
//
// This is exactly WP10's invariant: a pending job at or below its mark with no
// live run is a stranded firing, and WP10's run-is-authority reaper is what can
// prove the "no live run" half and act on it. Until then the count is the
// signal an operator has.
//
// Best effort by construction: the claim has already been decided, and a failed
// read here must not change that verdict.
func (s *ClusteredStorage) noteIfStranded(ctx context.Context, claim *scheduler.FiringClaim, markNanos int64) {
	if claim.FiringTime.UnixNano() > markNanos {
		return
	}
	job, err := s.GetJob(ctx, claim.JobID, claim.TenantID)
	if err != nil || job == nil {
		return
	}
	if job.Status == scheduler.JobStatusPending && !job.NextRun.After(time.Unix(0, markNanos)) {
		s.strandedFirings.Add(1)
	}
}

// StrandedFirings reports how many claims were refused for a job whose next
// firing sits at or below its mark — firings that can no longer run anywhere.
//
// Non-zero means work is being silently skipped, not merely arbitrated, so it
// is the counter to alert on. See noteIfStranded and docs/18 V4.
func (s *ClusteredStorage) StrandedFirings() uint64 {
	return s.strandedFirings.Load()
}

// DeleteJob removes the job and the claim mark that belongs to it.
//
// One mark per job means the mark outlives the job unless deleted with it, and
// an orphaned mark is debt nobody will ever collect. Deleting it is safe
// because the job record, not the mark, is what authorises execution: a
// contender still holding a claim for this job re-reads the job in
// executeJob's pre-flight and returns without running when it is gone
// (scheduler.go executeJob). So a mark deleted here cannot resurrect a firing.
//
// The mark is deleted after the job. If this call fails in between, the job is
// gone and a mark is orphaned — bounded, harmless debt — whereas the reverse
// order could briefly leave a live job with no mark, which is a firing that
// could be claimed twice.
func (s *ClusteredStorage) DeleteJob(ctx context.Context, id uint64, tenantID string) error {
	if err := s.Storage.DeleteJob(ctx, id, tenantID); err != nil {
		return err
	}
	if err := s.marks.Delete(ctx, keyForClaim(id, tenantID)); err != nil {
		return &scheduler.ErrStorageError{Op: "DeleteJob.claim_mark", Cause: err}
	}
	return nil
}
