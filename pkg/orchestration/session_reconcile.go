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
	"time"
)

// =============================================================================
// Session reconciliation — the record catches up with the world
// =============================================================================
//
// A SESSION RECORD IS DURABLE AND A SESSION PROCESS IS NOT, and until this
// file existed only ONE route closed the gap between them: a job that reached
// its discard hook asked the operator verb to close what it held. Every other
// way a session can stop — the job failing, the node restarting, the daemon
// that ran the agent leaving the fleet — left the record saying ACTIVE with
// nothing behind it. On the burn-in node that was 37 of 44 sessions, the
// oldest six days stale, and an operator reading the list could not tell which
// of them was real.
//
// The repair is not another closer. It is the observation that "this session
// is over" is knowable from three facts the daemon ALREADY writes down, and
// that each of them wants the SAME terminal transition:
//
//   - THE NODE RESTARTED. A process dies with the daemon that forked it. The
//     reconstitution path (orchestrator_resume.go) already decides which
//     records it can bring back; every record it cannot is a record whose
//     process is gone. A session on ANOTHER daemon is not such a record — this
//     node's restart did not touch it — so it is concluded only on positive
//     evidence that its host has left. See concludeStartupSession and
//     remoteSessionGone.
//   - THE JOB IS OVER. A session exists to do a job's work. Once the job
//     reaches a terminal state there is no work left, whichever route it took
//     to get there — completed, failed, abandoned by the reaper. See
//     ConcludeSessionsForJob.
//   - THE AGENT IS GONE. A session pinned to a daemon that unregistered, or
//     that has stopped heartbeating, is on a host nothing here can reach. See
//     ConcludeSessionsForFleetNode and SweepSessions.
//
// ONE TRANSITION SERVES ALL THREE, and it is the one session_conclusion.go
// already owns. A record this process holds live is concluded through that
// file's compare-and-swap, so a reconciliation can never become a second
// owner of an ending an operator or an exit observer is already writing. A
// record it does NOT hold live — the ordinary case after a restart, and the
// only case for another node's session — has no live entry to arbitrate on,
// so the arbitration is the store's own: the reason is written only onto a
// record that is not already closed, under a per-session lock, and the write
// itself refuses to overwrite an existing conclusion (see
// sessionStore.ConcludeSession). Two sweeps racing produce one ending.

// SessionReconcileVerdict is what a reconciliation pass decided about one
// session record.
type SessionReconcileVerdict string

const (
	// SessionReconcileLive means the record was left alone: its process is
	// re-attachable, or the session is not yet past the point at which the
	// pass is entitled to an opinion.
	SessionReconcileLive SessionReconcileVerdict = "live"

	// SessionReconcileConcluded means the record was moved to closed and
	// carries the reason it ended.
	SessionReconcileConcluded SessionReconcileVerdict = "concluded"

	// SessionReconcileAlreadyConcluded means somebody else had already
	// concluded the session; nothing was written.
	SessionReconcileAlreadyConcluded SessionReconcileVerdict = "already_concluded"

	// SessionReconcileFailed means the conclusion could not be recorded. The
	// record is unchanged and the next pass retries it.
	SessionReconcileFailed SessionReconcileVerdict = "failed"
)

// String implements fmt.Stringer.
func (v SessionReconcileVerdict) String() string { return string(v) }

// SessionReconcileFinding is one session's outcome from a pass.
type SessionReconcileFinding struct {
	TenantID  string
	SessionID string
	JobID     string

	// Reason is the conclusion the pass wrote, empty when it wrote none.
	Reason SessionConclusionReason

	Verdict SessionReconcileVerdict

	// Evidence states what the pass observed, so an operator reading a
	// closed session can see why rather than only when.
	Evidence string
}

// SessionReconcileReport is the outcome of one pass. A session left alone is
// not listed: the report is what CHANGED, plus what could not be changed.
type SessionReconcileReport struct {
	Findings []SessionReconcileFinding
}

// Count returns how many findings reached verdict v.
func (r SessionReconcileReport) Count(v SessionReconcileVerdict) int {
	n := 0
	for _, f := range r.Findings {
		if f.Verdict == v {
			n++
		}
	}
	return n
}

// =============================================================================
// Enumerating the records a pass must visit
// =============================================================================

// SetTenantLister gives recovery and maintenance passes the tenants they must
// visit.
//
// A CROSS-TENANT SCAN IS NOT CROSS-TENANT ON A TENANT-ROUTED STORE. The
// durable KV under the orchestration stores routes every read to the tenant
// on the CONTEXT (pkg/server/tenant_kvstore.go), so a scan issued on a
// maintenance ticker's own context — which carries no principal — sees the
// system tenant's records and nothing else. A pass that has to reason about
// every session therefore has to name every tenant, exactly as the task
// dispatcher and the task-source poller already do through this same seam.
//
// Optional: a service over a single, unrouted store (the embedded and
// in-memory cases) sees everything from one scan and needs no lister.
func (s *agentServiceImpl) SetTenantLister(tenants TenantLister) {
	s.tenants = tenants
}

// eachSessionRecord visits every durable session record a pass can reach,
// handing each one a context scoped to its owning tenant.
//
// The context matters as much as the record: every conclusion that follows
// writes back through the same routed store, so it has to be issued under the
// tenant that owns the record rather than under the pass's own.
//
// One tenant's failure does not end the walk. A tenant whose barrier is sealed
// cannot be listed, and a maintenance pass that gave up there would leave every
// later tenant unswept because of one that is legitimately unavailable.
func (s *agentServiceImpl) eachSessionRecord(
	ctx context.Context, visit func(context.Context, *SessionRecord)) error {
	if s.store == nil {
		return nil
	}
	if s.tenants == nil {
		records, err := s.store.ListAllSessions(ctx)
		if err != nil {
			return err
		}
		for _, record := range records {
			visit(ctx, record)
		}
		return nil
	}

	tenantIDs, err := s.tenants.ListTenantIDs(ctx)
	if err != nil {
		return err
	}
	// The system tenant owns every record written without one and is not in
	// the tenant manager's list, so it is added here; the visited set is what
	// keeps a manager that DOES list it from being swept twice.
	visited := make(map[string]struct{}, len(tenantIDs)+1)
	for _, tenantID := range append([]string{SystemTenantID}, tenantIDs...) {
		if _, seen := visited[tenantID]; seen {
			continue
		}
		visited[tenantID] = struct{}{}
		tenantCtx := ContextWithTenant(ctx, tenantID)
		records, listErr := s.store.ListSessions(tenantCtx, tenantID)
		if listErr != nil {
			s.logger.Warnf("orchestration: tenant %s: its sessions could not be listed: %s",
				tenantID, listErr.Error())
			continue
		}
		for _, record := range records {
			if record.TenantID == "" {
				record.TenantID = tenantID
			}
			visit(tenantCtx, record)
		}
	}
	return nil
}

// =============================================================================
// The one terminal transition, for a record with or without a live entry
// =============================================================================

// concludeSessionRecord ends one session and records why, routing through the
// live arbitration when this process holds the session and through the store's
// own when it does not.
//
// The bool reports whether THIS call wrote the ending. False with a nil error
// means the session was already concluded — which is a success for every
// caller here, all of which are stating a fact about the world rather than
// claiming to have acted on it.
func (s *agentServiceImpl) concludeSessionRecord(
	ctx context.Context,
	record *SessionRecord,
	reason SessionConclusionReason,
) (bool, error) {
	if record == nil || record.SessionID == "" {
		return false, &AgentServiceError{
			Sentinel: ErrSessionIDRequired,
			Message:  "a session conclusion names the record it concludes",
		}
	}
	if !reason.Valid() {
		return false, &AgentServiceError{
			Sentinel: ErrSessionConclusionReasonRequired,
			Message:  "session " + record.SessionID + " cannot be concluded without a known reason",
		}
	}
	if record.Status == SessionStateClosed {
		return false, nil
	}

	tenantID := record.TenantID
	storageTenant := normalizeStoreTenant(record.TenantID)

	// A LIVE RECORD MEANS THE LIVE ARBITRATION OWNS THE ENDING. Reconciliation
	// is a nudge in exactly session_conclusion.go's sense: it reports what it
	// saw, and losing to a close that is already running is the correct
	// outcome. Going around that file would tear down a process a winner is
	// already tearing down, and write a second terminal event behind it.
	if _, live := s.liveSessionRecord(record.SessionID); live {
		return s.concludeSession(
			ctx, tenantID, storageTenant, record.SessionID,
			reason, conclusionNudge, s.backendForSession(ctx, record.SessionID))
	}

	return s.concludeAbsentSession(ctx, tenantID, storageTenant, record.SessionID, reason)
}

// concludeAbsentSession ends a session this process does not hold live.
//
// THERE IS NOTHING TO TEAR DOWN AND THEREFORE NOTHING TO ARBITRATE IN MEMORY.
// The process died with another daemon, or with this one before it restarted;
// what is left is a record that says ACTIVE and a log that was never closed.
// So the transition here is the DURABLE half of session_conclusion.go's, taken
// under a per-session lock and gated on a fresh read of the record: a pass
// that finds the session already closed writes nothing, which is what makes a
// second pass, or a sweep racing an operator, produce one ending rather than
// two.
//
// The transient live record exists only to satisfy finishDurable's contract —
// it serializes the log write and gates the exactly-once eviction. It is never
// published in s.sessions, so it cannot be mistaken for a session that is
// running: the whole point of this path is that none is.
func (s *agentServiceImpl) concludeAbsentSession(
	ctx context.Context,
	tenantID, storageTenant, sessionID string,
	reason SessionConclusionReason,
) (bool, error) {
	unlock := s.reconcileMu.lock(storageTenant + "/" + sessionID)
	defer unlock()

	// RE-READ UNDER THE LOCK. The record the caller listed may be minutes old
	// by the time a sweep reaches it, and the session may have been concluded
	// — or spawned afresh and made live — in between.
	current, err := s.store.GetSession(ctx, storageTenant, sessionID)
	if err != nil {
		if errors.Is(err, ErrAgentStoreNotFound) {
			// A record that is gone has no ending left to write.
			return false, nil
		}
		return false, err
	}
	if current.Status == SessionStateClosed {
		return false, nil
	}
	if _, live := s.liveSessionRecord(sessionID); live {
		// Installed since the caller listed. The live arbitration owns it now,
		// and this pass is not entitled to conclude a session with a process.
		return false, nil
	}

	mem := &sessionRecord{
		Backend:     current.Backend,
		ProjectID:   current.ProjectID,
		Model:       current.Model,
		TenantID:    storageTenant,
		ContainerID: current.ContainerID,
		CreatedAt:   current.CreatedAt,
		JobID:       current.JobID,
		Persistent:  current.Persistent,
	}
	// THE DELIVERY GRANTS COME OFF THE DURABLE RECORD, and this is the case
	// they were put there for. A session concluded on this path has nothing in
	// memory — the node restarted, the job failed, the daemon left the fleet —
	// so the only place its credential grants can be read from is the store.
	// Without this the reaper would tear down every trace of the session except
	// the one that still authorises a host to hold a provider login.
	mem.SetCredentialLeaseIDs(current.CredentialLeaseIDs)
	// A record with no live entry has no process, so the backend teardown is
	// not owed — but a container the orchestrator launched may still be up,
	// and it is released here for the same reason the live path releases it.
	mem.resumable.Store(true)
	if err := s.concludeBackend(ctx, mem, storageTenant, sessionID, nil); err != nil {
		return false, err
	}
	if err := s.finishDurable(ctx, mem, tenantID, storageTenant, sessionID, reason); err != nil {
		return false, err
	}
	return true, nil
}

// recordSessionConclusionOnJob writes the session's ending into the job's own
// audit trail and drops it from the job's session list.
//
// THE JOB IS WHERE AN OPERATOR LOOKS FIRST. A session concluded by a sweep is
// invisible on the job that spawned it unless the job says so, and a job whose
// SessionIDs still name a closed session is the same false liveness one level
// up.
//
// IT RUNS INSIDE THE CONCLUSION, BEFORE THE COMMIT POINT, and its failure is
// RETURNED. It used to run in the reconciliation wrapper, after the session
// record had already been written closed and only when that write reported
// success — so a job store that would not answer left the job naming a session
// that no longer existed, permanently, because the next pass reads the closed
// record and has nothing left to do. Returning the failure instead leaves the
// session record open, which is the state every later pass retries.
//
// IDEMPOTENT, so that retry repeats work rather than duplicating it: the
// session is dropped from the list only while it is on it, and the audit line
// is appended only while the trail does not already name this session's
// ending. That is what makes the job-side cleanup happen exactly once across
// any number of attempts.
//
// A job the KV does not HOLD is nothing to tell — vacuously recorded, the same
// answer concludeLog gives a session with no log metadata. Blocking on it
// would strand every session whose job record has been reaped.
//
// A JOB THE KV CANNOT READ IS A DIFFERENT ANSWER ENTIRELY, and collapsing the
// two was a hole the commit-point ordering opened: a transient read failure or
// a corrupt record let this step report "nothing to tell", finishDurable
// walked past it and closed the session, and the invariant that makes the
// ordering work — closed means complete — then guaranteed no later pass would
// revisit it. The job would point at that session forever. So only genuine
// absence is vacuous (jobStatusAbsent); every failure to ANSWER is returned,
// which stops the conclusion before the commit and leaves the next pass to
// retry both halves.
func (s *agentServiceImpl) recordSessionConclusionOnJob(
	ctx context.Context, mem *sessionRecord, tenantID, sessionID string,
	reason SessionConclusionReason) error {
	jobID, err := s.sessionJobID(ctx, mem, tenantID, sessionID)
	if err != nil {
		return err
	}
	if jobID == "" || s.kvStore == nil {
		return nil
	}
	err = s.updateJobStatus(tenantID, jobID, func(status *JobStatus) bool {
		remaining := make([]string, 0, len(status.SessionIDs))
		for _, id := range status.SessionIDs {
			if id != sessionID {
				remaining = append(remaining, id)
			}
		}
		if len(remaining) == len(status.SessionIDs) && sessionHasConclusionEvent(status, sessionID) {
			// Nothing to drop and the ending is already on the trail: a repeat
			// pass must not append a second line saying the same thing.
			return false
		}
		status.SessionIDs = remaining
		status.Events = append(status.Events, JobAuditEvent{
			Timestamp: time.Now().UnixNano(),
			Type:      JobAuditTypeSessionConcluded,
			Details:   "session " + sessionID + " concluded: " + reason.String(),
		})
		status.UpdatedAt = time.Now().UnixNano()
		return true
	})
	switch {
	case jobStatusAbsent(err):
		// Reaped or never persisted: there is no job left to tell.
		return nil
	case err != nil:
		return &AgentServiceError{
			Sentinel: ErrAgentServiceUnavailable,
			Message: "session " + sessionID + " was concluded (" + reason.String() +
				") and its job " + jobID + " could not be told: " + err.Error(),
		}
	}
	return nil
}

// sessionJobID resolves the job a session works for.
//
// The live record carries it — mirrored from the durable one at spawn and at
// restore — so the ordinary conclusion costs no read. The store is asked only
// when the live record has none.
//
// A STORE THAT CANNOT ANSWER IS AN ERROR, not "no job". Returning the empty
// string there would skip the job cleanup and let the conclusion reach its
// commit point, which is the same hole as swallowing a failed job-status read:
// the session would be durably closed while its job still named it, and the
// closed record guarantees nothing revisits the pair. A record that is
// genuinely absent, or one that names no job, really does have nothing to
// tell.
func (s *agentServiceImpl) sessionJobID(
	ctx context.Context, mem *sessionRecord, tenantID, sessionID string) (string, error) {
	if mem != nil && mem.JobID != "" {
		return mem.JobID, nil
	}
	if s.store == nil {
		return "", nil
	}
	record, err := s.store.GetSession(ctx, normalizeStoreTenant(tenantID), sessionID)
	switch {
	case errors.Is(err, ErrAgentStoreNotFound):
		return "", nil
	case err != nil:
		return "", &AgentServiceError{
			Sentinel: ErrAgentServiceUnavailable,
			Message: "session " + sessionID + " was concluded and the job it works for " +
				"could not be resolved: " + err.Error(),
		}
	}
	return record.JobID, nil
}

// sessionHasConclusionEvent reports whether the job's trail already names this
// session's ending.
func sessionHasConclusionEvent(status *JobStatus, sessionID string) bool {
	// The trailing " concluded" keeps session "s1" from matching the ending of
	// session "s10".
	prefix := "session " + sessionID + " concluded"
	for _, event := range status.Events {
		if event.Type == JobAuditTypeSessionConcluded && strings.HasPrefix(event.Details, prefix) {
			return true
		}
	}
	return false
}

// JobAuditTypeSessionConcluded is the JobAuditEvent.Type of the line a job
// carries when one of its sessions was ended by something other than the job
// itself.
const JobAuditTypeSessionConcluded = "session_concluded"

// conclude applies one reason to one record and turns the outcome into a
// finding, so every entry point produces the same shaped report.
func (s *agentServiceImpl) conclude(
	ctx context.Context,
	record *SessionRecord,
	reason SessionConclusionReason,
	evidence string,
) SessionReconcileFinding {
	finding := SessionReconcileFinding{
		TenantID:  record.TenantID,
		SessionID: record.SessionID,
		JobID:     record.JobID,
		Reason:    reason,
		Evidence:  evidence,
	}
	written, err := s.concludeSessionRecord(ctx, record, reason)
	switch {
	case err != nil:
		finding.Verdict = SessionReconcileFailed
		finding.Reason = ""
		finding.Evidence = evidence + "; the conclusion could not be recorded: " + err.Error()
	case written:
		// The job-side cleanup is NOT done here. It is part of the conclusion
		// itself now (finishDurable), ahead of the commit point, so that a
		// job store that will not answer leaves the session retryable instead
		// of leaving the job naming a session that no longer exists.
		finding.Verdict = SessionReconcileConcluded
	default:
		finding.Verdict = SessionReconcileAlreadyConcluded
		finding.Reason = ""
	}
	return finding
}

// =============================================================================
// Startup — the node restarted
// =============================================================================

// concludeStartupSession concludes one record the reconstitution path declined
// to bring back.
//
// IT IS THE OTHER HALF OF LoadPersistedSessions, not a second scan: that
// function walks every record once and decides, per record, whether this
// daemon can address the session again. A record it restores has a process or
// a documented way back to one. A record it does not restore has neither, and
// leaving it ACTIVE is what produced the stranded list — the record outlives
// the process, so somebody has to write down that the process is gone.
//
// THE REASON IS THE CALLER'S because the two startup endings are different
// facts. `node_restart` is "the process died with the daemon that forked it",
// which only a LOCAL session's process did. `agent_gone` is "the host holding
// it has left the fleet", which is the only thing that can be said about a
// remote one — and saying the first about the second would attribute another
// node's departure to this node's restart.
func (s *agentServiceImpl) concludeStartupSession(
	ctx context.Context, record *SessionRecord, reason SessionConclusionReason, evidence string) {
	finding := s.conclude(ctx, record, reason, evidence)
	switch finding.Verdict {
	case SessionReconcileFailed:
		s.logger.Warnf("orchestration: session %s could not be concluded on restart: %s",
			record.SessionID, finding.Evidence)
	case SessionReconcileConcluded:
		s.logger.Infof("orchestration: session %s concluded (%s): %s",
			record.SessionID, reason, evidence)
	}
}

// =============================================================================
// A job that ended — every route, not only the discard hook
// =============================================================================

// ConcludeSessionsForJob ends every session still bound to a job that is over.
//
// DRIVEN FROM THE STORE, NOT FROM THE RUN. The JobRun tracks the sessions it
// spawned, but a run that panicked, was aborted mid-stage or died with its
// node either never untracked them or is not here to be asked; and the reaper
// concludes jobs whose run object never existed in this process at all. The
// durable records name their job, so the binding survives everything the run
// does not.
func (s *agentServiceImpl) ConcludeSessionsForJob(
	ctx context.Context, tenantID, jobID string) (SessionReconcileReport, error) {
	var report SessionReconcileReport
	if jobID == "" {
		return report, &AgentServiceError{
			Sentinel: ErrJobIDRequired,
			Message:  "a job's sessions are found by the job they name",
		}
	}
	records, err := s.store.ListSessions(ctx, tenantID)
	if err != nil {
		return report, err
	}
	evidence := "job " + jobID + " reached a terminal state, so the work the session was spawned for is over"
	for _, record := range records {
		if record.JobID != jobID || !sessionRecordUnfinished(record) {
			continue
		}
		report.Findings = append(report.Findings, s.conclude(ctx, record, ConcludedByJobConcluded, evidence))
	}
	return report, nil
}

// concludeJobSessions is ConcludeSessionsForJob for the engine's own terminal
// paths, which have nowhere to return a report to and must not fail a job
// because a session record would not close.
func (s *agentServiceImpl) concludeJobSessions(tenantID, jobID string) {
	if jobID == "" || s.store == nil {
		return
	}
	ctx, cancel := context.WithTimeout(ContextWithTenant(context.Background(), tenantID), jobConcludeTimeout)
	defer cancel()

	report, err := s.ConcludeSessionsForJob(ctx, tenantID, jobID)
	if err != nil {
		s.logger.Warnf("orchestration: job %s: its sessions could not be listed for conclusion: %s",
			jobID, err.Error())
		return
	}
	if closed := report.Count(SessionReconcileConcluded); closed > 0 {
		s.logger.Infof("orchestration: job %s concluded %d session(s) it still held", jobID, closed)
	}
	for _, finding := range report.Findings {
		if finding.Verdict == SessionReconcileFailed {
			s.logger.Warnf("orchestration: job %s: session %s: %s",
				jobID, finding.SessionID, finding.Evidence)
		}
	}
}

// =============================================================================
// An agent that left the fleet
// =============================================================================

// ConcludeSessionsForFleetNode ends every session pinned to a daemon that is no
// longer serving.
//
// THE PIN IS THE BINDING. A session dispatched to another daemon records the
// host it landed on (SessionRecord.HostAffinity) so a follow-up call re-resolves
// to the same instance; when that instance leaves the fleet the pin is a
// dangling reference and the session behind it is unreachable by construction.
// A session with no pin ran here, and this node's own restarts are the startup
// path's business rather than this one's.
func (s *agentServiceImpl) ConcludeSessionsForFleetNode(
	ctx context.Context, tenantID string, agent *FleetNode, evidence string) (SessionReconcileReport, error) {
	var report SessionReconcileReport
	if agent == nil {
		return report, &AgentServiceError{
			Sentinel: ErrAgentIDRequired,
			Message:  "an agent's sessions are found by the registration they are pinned to",
		}
	}
	records, err := s.store.ListSessions(ctx, tenantID)
	if err != nil {
		return report, err
	}
	for _, record := range records {
		// RESUMABLE IS EXCLUDED HERE ON PURPOSE, and the reason is the pin.
		// Only a LOCAL persistent session is ever restored resumable
		// (SessionRecord.Resumable requires BackendLocal), so the daemon it
		// belongs to is this one — which is, by construction, still serving.
		// An agent that left the fleet is some other daemon, and a resumable
		// record that happens to carry its old pin has no process there to
		// lose. The path that DOES end a resumable session is its job
		// concluding; see sessionRecordUnfinished.
		if !sessionRecordOpen(record) || !sessionPinnedToAgent(record, agent) {
			continue
		}
		report.Findings = append(report.Findings, s.conclude(ctx, record, ConcludedByFleetNodeGone, evidence))
	}
	return report, nil
}

// sessionPinnedToAgent reports whether a session's host affinity names this
// agent. Both identifiers are accepted because both are what a spawn may have
// resolved the host to: the operator names an agent by Name, the dispatcher
// resolves it to Host.
func sessionPinnedToAgent(record *SessionRecord, agent *FleetNode) bool {
	if record.HostAffinity == "" {
		return false
	}
	return record.HostAffinity == agent.Name || record.HostAffinity == agent.Host || record.HostAffinity == agent.ID
}

// sessionRecordOpen reports whether a record still claims to have something
// behind it. `resumable` and `error` are deliberately excluded: the first is a
// record the reconstitution path already decided it can bring back, and the
// second already carries its explanation.
func sessionRecordOpen(record *SessionRecord) bool {
	return record != nil &&
		(record.Status == SessionStateActive || record.Status == SessionStateIdle)
}

// sessionRecordUnfinished reports whether a record has any future left — which
// is the open states PLUS resumable.
//
// A RESUMABLE SESSION IS WAITING FOR SOMETHING, AND WHEN THAT SOMETHING IS
// OVER IT IS NOT WAITING ANY MORE. Every restart turns a persistent local
// session resumable, and `open` excludes resumable — so a job that concluded
// after a restart walked its sessions, matched none of them, and left every
// one resume-ready for work that had already ended. The periodic sweep used
// the same gate, so nothing repaired them later either: they simply
// accumulated, which is the stranded listing this whole change exists to
// remove.
//
// It is a SEPARATE predicate rather than a widening of sessionRecordOpen
// because the distinction is real: resumable means "no process, by design",
// and only a caller that knows the session has nothing left to be resumed FOR
// may end it. Idleness alone may not — an operator's resumable session is
// quiet precisely because it is waiting for them.
func sessionRecordUnfinished(record *SessionRecord) bool {
	return sessionRecordOpen(record) ||
		(record != nil && record.Status == SessionStateResumable)
}

// =============================================================================
// The maintenance sweep
// =============================================================================

// SweepSessions concludes the session records the periodic pass can prove are
// over, across every tenant.
//
// TWO SIGNALS, AND NEITHER IS THE CLOCK ALONE — the same house rule the stall
// detector follows. A session is concluded only when it is BOTH quiet for
// longer than the longest bound any job state carries AND corroborated by a
// second fact: its job has reached a terminal state, or the daemon it is
// pinned to is no longer registered. A session that is merely quiet is left
// alone, because a long-running agent that has not spoken for an hour is
// exactly what a long-running agent looks like.
//
// The cross-tenant scan is deliberate and matches the reaper's, which lists
// every tenant's leases in one pass: a maintenance sweep runs on no caller's
// behalf and has no tenant of its own, and a per-tenant sweep would first have
// to enumerate tenants it has no authority to enumerate.
func (s *agentServiceImpl) SweepSessions(ctx context.Context) (SessionReconcileReport, error) {
	var report SessionReconcileReport
	quietFor := s.idleThresholds.longest()
	now := utcNow()
	err := s.eachSessionRecord(ctx, func(tenantCtx context.Context, record *SessionRecord) {
		// Resumable records are admitted so the job-terminal verdict can repair
		// the ones a restart left behind. sweepVerdict is what refuses to end
		// them on any other evidence.
		if !sessionRecordUnfinished(record) {
			return
		}
		idle := now.Sub(time.Unix(0, sessionLastActivity(record)))
		if quietFor > 0 && idle < quietFor {
			return
		}
		reason, evidence, found := s.sweepVerdict(tenantCtx, record, idle, quietFor)
		if !found {
			return
		}
		report.Findings = append(report.Findings, s.conclude(tenantCtx, record, reason, evidence))
	})
	if err != nil {
		return report, err
	}
	return report, nil
}

// sweepVerdict looks for the second fact behind a quiet session and reports
// what it found. The bool is false when there is none, which leaves the
// session alone.
func (s *agentServiceImpl) sweepVerdict(
	ctx context.Context, record *SessionRecord, idle, quietFor time.Duration) (SessionConclusionReason, string, bool) {
	quiet := "session has been quiet for " + idle.String() +
		", past the longest job idle bound of " + quietFor.String()

	if state, terminal := s.jobTerminalState(record); terminal {
		return ConcludedByJobConcluded,
			quiet + " and its job " + record.JobID + " is " + string(state), true
	}
	// ONLY THE JOB MAY END A RESUMABLE SESSION. It has no process by design, so
	// "the daemon holding it is gone" is not a fact about it, and silence is
	// its normal condition rather than evidence of anything. Ending it on
	// either would destroy a session an operator is deliberately keeping.
	if !sessionRecordOpen(record) {
		return "", "", false
	}
	if agent, gone := s.sessionHostGone(ctx, record); gone {
		return ConcludedByFleetNodeGone,
			quiet + " and the daemon it is pinned to (" + agent + ") is no longer registered", true
	}
	return "", "", false
}

// jobTerminalState reports the state of the session's job when that job has
// ended.
//
// A JOB THAT CANNOT BE READ IS NOT A TERMINAL JOB. Fail-closed: a KV that is
// briefly unavailable must not be read as "every job has ended", which would
// let one bad sweep close every live session on the node.
func (s *agentServiceImpl) jobTerminalState(record *SessionRecord) (JobState, bool) {
	if record.JobID == "" || s.kvStore == nil {
		return "", false
	}
	if s.JobRunning(record.TenantID, record.JobID) {
		return "", false
	}
	status, err := s.loadJobStatus(record.TenantID, record.JobID)
	if err != nil || status == nil {
		return "", false
	}
	return status.State, IsTerminalJobState(status.State)
}

// sessionHostGone reports whether the daemon a session is pinned to has left
// the fleet, and names it.
//
// GONE MEANS UNREGISTERED OR SILENT, and the two are the same fact to a session
// that can only be reached through that daemon: an agent removed from the
// registry has said it is going, and one that has stopped heartbeating for
// longer than the operator's timeout has stopped saying anything. The
// registration surviving in the store is not liveness — it is a record, the
// same kind of record this whole file exists because processes outlive.
func (s *agentServiceImpl) sessionHostGone(ctx context.Context, record *SessionRecord) (string, bool) {
	return s.sessionHostAbsent(ctx, record, true)
}

// sessionHostUnregistered is the BOOT-TIME question, and it is deliberately
// weaker: is there a registration for this daemon at all?
//
// HEARTBEAT AGE IS NOT EVIDENCE AT STARTUP. Every registration this node can
// read was written before it restarted, so every live peer looks silent until
// it checks in again — and concluding on that would close the sessions of
// every remote worker that is still running, which is the false terminal state
// #792 exists to remove, inverted. Absence of a registration is evidence;
// staleness is not, and the maintenance sweep asks the stronger question once
// the node has been up long enough for a live daemon to have spoken.
func (s *agentServiceImpl) sessionHostUnregistered(ctx context.Context, record *SessionRecord) (string, bool) {
	return s.sessionHostAbsent(ctx, record, false)
}

// sessionHostAbsent is the shared body. requireRecentHeartbeat selects whether
// silence counts as absence.
//
// A REGISTRY THAT CANNOT ANSWER MEANS NOTHING IS GONE, for the same fail-closed
// reason as jobTerminalState: an unreadable registry is not an empty one.
func (s *agentServiceImpl) sessionHostAbsent(
	ctx context.Context, record *SessionRecord, requireRecentHeartbeat bool) (string, bool) {
	if record.HostAffinity == "" || s.fleetRegistry == nil {
		return "", false
	}
	agents, err := s.fleetRegistry.ListFleetNodes(
		fleetTenantContext(ctx, record.TenantID), record.TenantID, nil)
	if err != nil {
		return "", false
	}
	timeout := s.effectiveFleetHeartbeatTimeout()
	now := utcNow()
	for _, agent := range agents {
		if !sessionPinnedToAgent(record, agent) {
			continue
		}
		if agent.Status == FleetNodeStatusOffline {
			continue
		}
		if requireRecentHeartbeat && agent.LastSeen > 0 && now.Sub(time.Unix(0, agent.LastSeen)) > timeout {
			continue
		}
		return "", false
	}
	return record.HostAffinity, true
}

// remoteSessionGone reports whether the daemon that owns a REMOTE session has
// left the fleet, and names it.
//
// POSITIVE EVIDENCE ONLY, and the asymmetry is the whole point. A remote
// session's process belongs to another host, which this daemon's restart did
// not touch (see SessionRecord.Resumable) — so the local backend table cannot
// answer anything about it, and treating "not in my map" as "gone" marks a
// still-running worker closed and destroys the addressability an operator
// needs to reach it. The question is therefore asked of the things that can
// actually answer it: the remote resolver, which resolves a backend only when
// the daemon serving it is reachable, and the fleet registry.
//
// The resolver is consulted first and is the stronger signal. It is nil during
// boot — it is installed after the restoration — so at startup the registry
// decides, on presence rather than on heartbeat age (sessionHostUnregistered).
// A session with no pin and no resolver answer is left alone: no evidence
// either way is not evidence of death, and the maintenance sweep revisits it.
// THE PROBE ASKS ABOUT THE PINNED DAEMON, NOT ABOUT THE BACKEND. A resolver
// given no affinity answers "yes, something serves this backend" — and in a
// fleet where daemon B serves the same backend as daemon A, that is a yes for
// a session pinned to a DEAD A. The record was then restored as live, and the
// prompt, status and close paths that follow re-pin to the stored host
// (sessionRoutingContext) and fail against the daemon that is actually gone.
// So the record's affinity goes onto the probe context, and STRICTLY: a soft
// hint is precisely what lets the resolver fall back to B and manufacture the
// wrong answer.
//
// A pinned daemon that will not resolve is not yet proof of death — the
// resolver may simply be unable to answer — so that case falls through to the
// registry, which still requires positive evidence of absence.
func (s *agentServiceImpl) remoteSessionGone(ctx context.Context, record *SessionRecord) (string, bool) {
	if s.remoteResolver != nil {
		probeCtx := ctx
		if record.HostAffinity != "" {
			probeCtx = WithHostAffinityStrict(WithHostAffinity(ctx, record.HostAffinity))
		}
		if backend, ok := s.remoteResolver(probeCtx, record.Backend, record.ProjectID); ok && backend != nil {
			return "", false
		}
	}
	return s.sessionHostUnregistered(ctx, record)
}

// DefaultFleetHeartbeatTimeout is how long an agent may go without
// heartbeating before the sweep treats it as gone. It matches the
// orchestration configuration's own default so an operator who sets neither
// gets one answer rather than two.
const DefaultFleetHeartbeatTimeout = 90 * time.Second

// SetFleetHeartbeatTimeout configures the age past which a registered agent's
// silence counts as absence. A non-positive value selects the default.
func (s *agentServiceImpl) SetFleetHeartbeatTimeout(timeout time.Duration) {
	s.fleetHeartbeatTimeout = timeout
}

// effectiveFleetHeartbeatTimeout resolves the configured timeout.
func (s *agentServiceImpl) effectiveFleetHeartbeatTimeout() time.Duration {
	if s.fleetHeartbeatTimeout > 0 {
		return s.fleetHeartbeatTimeout
	}
	return DefaultFleetHeartbeatTimeout
}

// sessionLastActivity is the instant a session was last known to be doing
// something, falling back to its creation time. A record with neither is
// treated as created now, so a malformed one is never swept on its first
// sighting.
func sessionLastActivity(record *SessionRecord) int64 {
	switch {
	case record.LastActivity > 0:
		return record.LastActivity
	case record.CreatedAt > 0:
		return record.CreatedAt
	default:
		return time.Now().UnixNano()
	}
}

// logSessionSweep renders one pass for the operator log.
func (s *agentServiceImpl) logSessionSweep(report SessionReconcileReport) {
	concluded := report.Count(SessionReconcileConcluded)
	failed := report.Count(SessionReconcileFailed)
	if concluded == 0 && failed == 0 {
		return
	}
	s.logger.Infof("orchestration: session sweep concluded %s session(s), %s could not be recorded",
		strconv.Itoa(concluded), strconv.Itoa(failed))
	for _, finding := range report.Findings {
		switch finding.Verdict {
		case SessionReconcileConcluded:
			s.logger.Infof("orchestration: session %s concluded (%s): %s",
				finding.SessionID, finding.Reason, finding.Evidence)
		case SessionReconcileFailed:
			s.logger.Warnf("orchestration: session %s: %s", finding.SessionID, finding.Evidence)
		}
	}
}

// concludeAgentSessions is ConcludeSessionsForFleetNode for the engine's own
// paths, which have nowhere to return a report to and must not fail an
// operator's request because a session record would not close.
func (s *agentServiceImpl) concludeAgentSessions(
	ctx context.Context, tenantID string, agent *FleetNode, evidence string) {
	if s.store == nil || agent == nil {
		return
	}
	report, err := s.ConcludeSessionsForFleetNode(ctx, tenantID, agent, evidence)
	if err != nil {
		s.logger.Warnf("orchestration: agent %s: its sessions could not be listed for conclusion: %s",
			agent.ID, err.Error())
		return
	}
	if closed := report.Count(SessionReconcileConcluded); closed > 0 {
		s.logger.Infof("orchestration: agent %s took %d session(s) with it", agent.ID, closed)
	}
	for _, finding := range report.Findings {
		if finding.Verdict == SessionReconcileFailed {
			s.logger.Warnf("orchestration: agent %s: session %s: %s",
				agent.ID, finding.SessionID, finding.Evidence)
		}
	}
}
