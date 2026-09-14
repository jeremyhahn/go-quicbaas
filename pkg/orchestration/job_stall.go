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
	"sort"
	"strconv"
	"sync"
	"time"
)

// =============================================================================
// The stall detector
// =============================================================================
//
// THE REAPER SEES A NODE THAT DIED. THIS SEES A NODE THAT IS ALIVE AND GETTING
// NOWHERE. A loop whose agent process died holds its lease exactly as firmly
// as one mid-implementation — the heartbeat is the loop's, not the agent's —
// and would keep heartbeating forever over a session nothing is working.
//
// Two facts per job, and the precedence between them is explicit:
//
//   - THE THRESHOLD SAYS WHEN TO LOOK. Each state a loop can sit in has a
//     bound on how long it may sit there; a job under its bound is healthy
//     and nothing is asked about it.
//   - THE SESSIONS SAY WHETHER TO ACT. Past the bound, a state that is
//     session-backed is asked whether any of its sessions is still
//     addressable. One that is means the agent is there and slow: the wait is
//     REPORTED and the job left alone. None that is means the agent is gone:
//     the job is ENDED, through the loop's own abort, so its attempt is
//     concluded by the same path every other failure takes.
//
// A state that is not session-backed — decomposing on a stateless backend,
// validating — is only ever reported past its bound, never ended: there is no
// second signal there to corroborate the clock, and the house rule is that
// the clock alone decides nothing.

// JobIdleThresholds is how long a job may sit in each state before the wait
// is worth reporting.
//
// ZERO MEANS UNSET and takes the default. A NEGATIVE value means DO NOT WATCH
// this state — an explicit off switch, because zero cannot honestly mean both
// "I did not configure this" and "never report it".
type JobIdleThresholds map[JobState]time.Duration

// DefaultJobIdleThreshold bounds a state a runner declared but did not put a
// bound on. Ten minutes is the modal engine bound and errs towards REPORTING
// a wait that is not yet a problem, which is the cheap direction: a report
// costs an operator one line, and only a session-backed state can end a job.
const DefaultJobIdleThreshold = 10 * time.Minute

// DefaultJobIdleThresholds is the defaults layer, DERIVED FROM THE REGISTERED
// RUNNERS rather than kept here. Each runner declares what its states wait
// for on its own JobStateSpecs (see JobStateSpec.IdleThreshold); a table
// beside the detector would be a second answer to a question the runner has
// already answered, and the two drifted the moment a runner was added.
//
// It is a function rather than a variable because a runner registers at
// module start: a variable would be the vocabulary of whichever runners had
// registered when the package was initialized.
func DefaultJobIdleThresholds() JobIdleThresholds {
	return jobIdleThresholdsFromSpecs(KnownJobStates())
}

// jobIdleThresholdsFromSpecs is DefaultJobIdleThresholds over explicit specs.
func jobIdleThresholdsFromSpecs(specs []JobStateSpec) JobIdleThresholds {
	thresholds := make(JobIdleThresholds, len(specs))
	for _, spec := range specs {
		threshold := spec.IdleThreshold
		if threshold == 0 {
			threshold = DefaultJobIdleThreshold
		}
		thresholds[spec.State] = threshold
	}
	return thresholds
}

// sessionBackedFromSpecs is the set of states in which a job must be backed
// by an addressable session: an agent is doing the work, and a job past its
// bound with no agent to ask is a job to end. Everything else is reported
// only. It too is the runners' own declaration (JobStateSpec.SessionBacked),
// merged by mergeJobStateSpec.
func sessionBackedFromSpecs(specs []JobStateSpec) map[JobState]bool {
	backed := make(map[JobState]bool, len(specs))
	for _, spec := range specs {
		if spec.SessionBacked {
			backed[spec.State] = true
		}
	}
	return backed
}

// Resolved fills every unset state from the defaults, keeping explicit
// negatives as off switches.
func (t JobIdleThresholds) Resolved() JobIdleThresholds {
	return t.resolvedAgainst(DefaultJobIdleThresholds())
}

// resolvedAgainst is Resolved against an already-derived defaults layer, so a
// caller resolving more than once walks the registry once.
func (t JobIdleThresholds) resolvedAgainst(defaults JobIdleThresholds) JobIdleThresholds {
	resolved := make(JobIdleThresholds, len(defaults))
	for state, fallback := range defaults {
		if configured, ok := t[state]; ok && configured != 0 {
			resolved[state] = configured
			continue
		}
		resolved[state] = fallback
	}
	return resolved
}

// For returns the threshold on one state, and whether that state is watched.
// A terminal state has no entry and is not watched: a job that has ended is
// not a job that is waiting.
func (t JobIdleThresholds) For(state JobState) (time.Duration, bool) {
	threshold, ok := t.Resolved()[state]
	if !ok || threshold < 0 {
		return 0, false
	}
	return threshold, true
}

// longest returns the largest watched threshold; it is the age past which a
// ledger entry is certainly spent.
func (t JobIdleThresholds) longest() time.Duration {
	return longestOf(t.Resolved())
}

// longestOf is longest over an already-resolved layer.
func longestOf(resolved JobIdleThresholds) time.Duration {
	var longest time.Duration
	for _, threshold := range resolved {
		if threshold > longest {
			longest = threshold
		}
	}
	return longest
}

// JobIdleThresholdsFrom builds thresholds from configured durations keyed by
// state name. An unknown key is REFUSED: a threshold the operator set and the
// engine silently dropped leaves the state unwatched while the configuration
// reads as though it is watched.
func JobIdleThresholdsFrom(configured map[string]time.Duration) (JobIdleThresholds, error) {
	watched := DefaultJobIdleThresholds()
	thresholds := make(JobIdleThresholds, len(configured))
	for name, threshold := range configured {
		state := JobState(name)
		if _, known := watched[state]; !known {
			return nil, &JobIdleThresholdError{State: name}
		}
		thresholds[state] = threshold
	}
	return thresholds, nil
}

// WatchedJobStates lists every state a threshold may name, sorted, so a
// configuration validator can read the set rather than keep a copy of it.
func WatchedJobStates() []string {
	specs := KnownJobStates()
	states := make([]string, 0, len(specs))
	for _, spec := range specs {
		states = append(states, string(spec.State))
	}
	sort.Strings(states)
	return states
}

// JobStallVerdict is what the detector decided about one job.
type JobStallVerdict string

const (
	// JobStallHealthy means the job is under its state's threshold.
	JobStallHealthy JobStallVerdict = "healthy"

	// JobStallIdle means the job is past its threshold and was reported, not
	// ended: its state is not session-backed, or a session is addressable.
	JobStallIdle JobStallVerdict = "idle"

	// JobStallSessionUnknown means a session's state could not be read, so
	// nothing was concluded — fail-closed.
	JobStallSessionUnknown JobStallVerdict = "session_unknown"

	// JobStallStalled means the job was past its threshold in a
	// session-backed state with no addressable session, and was ended.
	JobStallStalled JobStallVerdict = "stalled"
)

// String implements fmt.Stringer.
func (v JobStallVerdict) String() string { return string(v) }

// JobStallFinding is one job's outcome from a detection pass.
type JobStallFinding struct {
	TenantID string
	JobID    string
	TaskID   string
	State    JobState

	// Idle is how long the job has sat in State.
	Idle time.Duration

	Verdict  JobStallVerdict
	Evidence string
}

// JobStallReport is the outcome of one pass. Healthy jobs are not listed;
// idle findings are listed once per threshold period, not once per sweep.
type JobStallReport struct {
	Findings []JobStallFinding
}

// Count returns how many findings reached verdict v.
func (r JobStallReport) Count(v JobStallVerdict) int {
	n := 0
	for _, f := range r.Findings {
		if f.Verdict == v {
			n++
		}
	}
	return n
}

// JobRunSource lists the jobs a detector examines: this node's running,
// task-bound jobs.
type JobRunSource interface {
	RunningJobs() []*JobRun
}

// SessionAddresser answers whether a session can still be reached.
type SessionAddresser interface {
	SessionStatus(ctx context.Context, req *SessionStatusRequest) (*SessionStatusResponse, error)
}

// JobStallDetector finds jobs that are past their state's bound and ends the
// ones nothing is working.
type JobStallDetector struct {
	runs          JobRunSource
	sessions      SessionAddresser
	thresholds    JobIdleThresholds
	sessionBacked map[JobState]bool
	longest       time.Duration
	now           jobLeaseClock
	ledger        *idleLedger
}

// NewJobStallDetector builds a detector. Both dependencies are required:
// without the loops there is nothing to examine, and without the session
// authority the clock alone would end live work.
//
// The state vocabulary is read from the registered runners ONCE, here: a
// sweep must not depend on what registered since it started, and re-deriving
// per job would put a registry walk on the per-job path.
func NewJobStallDetector(runs JobRunSource, sessions SessionAddresser, thresholds JobIdleThresholds) (*JobStallDetector, error) {
	return newJobStallDetector(runs, sessions, thresholds, KnownJobStates())
}

// newJobStallDetector is NewJobStallDetector over explicit runner specs, so
// the derivation can be exercised against states a test declares rather than
// the engine's process-wide registry.
func newJobStallDetector(runs JobRunSource, sessions SessionAddresser, thresholds JobIdleThresholds, specs []JobStateSpec) (*JobStallDetector, error) {
	if runs == nil {
		return nil, &JobLeaseError{Sentinel: ErrJobLeaseInvalidInput, Message: "a stall detector needs the jobs it examines"}
	}
	if sessions == nil {
		return nil, &JobLeaseError{Sentinel: ErrJobLeaseInvalidInput, Message: "a stall detector needs the session authority; the clock alone must not end live work"}
	}
	resolved := thresholds.resolvedAgainst(jobIdleThresholdsFromSpecs(specs))
	return &JobStallDetector{
		runs:          runs,
		sessions:      sessions,
		thresholds:    resolved,
		sessionBacked: sessionBackedFromSpecs(specs),
		longest:       longestOf(resolved),
		now:           utcNow,
		ledger:        newIdleLedger(),
	}, nil
}

// Detect examines every running job.
func (d *JobStallDetector) Detect(ctx context.Context) JobStallReport {
	var report JobStallReport
	now := d.now()
	live := map[string]bool{}
	for _, run := range d.runs.RunningJobs() {
		finding, reportable := d.examine(ctx, run, now)
		if finding.Verdict != JobStallHealthy {
			live[idleKey(finding.TenantID, finding.JobID)] = true
		}
		if reportable {
			report.Findings = append(report.Findings, finding)
		}
	}
	d.ledger.retain(live, now, d.longest)
	sort.Slice(report.Findings, func(i, j int) bool {
		return report.Findings[i].JobID < report.Findings[j].JobID
	})
	return report
}

// examine decides one job's fate, and whether the finding is worth listing.
func (d *JobStallDetector) examine(ctx context.Context, run *JobRun, now time.Time) (JobStallFinding, bool) {
	status := run.Status()
	if status == nil {
		return JobStallFinding{}, false
	}
	finding := JobStallFinding{
		TenantID: status.TenantID,
		JobID:    status.JobID,
		TaskID:   status.TaskID,
		State:    status.State,
		Idle:     now.Sub(time.Unix(0, status.UpdatedAt)),
	}
	threshold, watched := d.watch(status.State)
	if !watched || finding.Idle < threshold {
		finding.Verdict = JobStallHealthy
		return finding, false
	}

	idle := "job has been " + string(status.State) + " for " + finding.Idle.String() +
		", past its " + threshold.String() + " bound"
	if !d.sessionBacked[status.State] {
		finding.Verdict = JobStallIdle
		finding.Evidence = idle + "; the state is not session-backed, so the wait is reported and the job left running"
		return finding, d.ledger.shouldReport(idleKey(finding.TenantID, finding.JobID), now, threshold)
	}

	addressable, unknown := d.addressable(ctx, status.TenantID, status.SessionIDs)
	switch {
	case unknown != nil:
		finding.Verdict = JobStallSessionUnknown
		finding.Evidence = idle + "; a session's state could not be read, so nothing was concluded: " + unknown.Error()
		return finding, true
	case addressable:
		finding.Verdict = JobStallIdle
		finding.Evidence = idle + "; a session is still addressable, so the agent is slow rather than gone"
		return finding, d.ledger.shouldReport(idleKey(finding.TenantID, finding.JobID), now, threshold)
	}

	finding.Verdict = JobStallStalled
	finding.Evidence = idle + " with " + strconv.Itoa(len(status.SessionIDs)) +
		" session(s) and none addressable: the agent is gone (detected by the job stall detector at " +
		now.Format(time.RFC3339) + ")"
	run.Abort(&JobStalledError{JobID: status.JobID, State: status.State, Idle: finding.Idle, Evidence: finding.Evidence})
	return finding, true
}

// watch returns the bound on one state and whether it is watched at all. It
// reads the layer resolved at construction, so it is a map lookup rather than
// JobIdleThresholds.For's re-resolve, which would run per job per sweep.
func (d *JobStallDetector) watch(state JobState) (time.Duration, bool) {
	threshold, ok := d.thresholds[state]
	if !ok || threshold < 0 {
		return 0, false
	}
	return threshold, true
}

// addressable reports whether any session can still be reached. A session
// whose state cannot be read is an error, never "not addressable": half a
// conclusion drawn from a network timeout is worse than none.
//
// THE SESSION IS ASKED FOR UNDER THE JOB'S OWN TENANT, not under the sweep's.
// A sweep runs on the daemon's maintenance ticker and carries no principal, so
// the context it hands down names no tenant; the session service resolves
// tenancy from the context and from nowhere else, and answered every one of
// these probes with "tenant ID required" (go-quicbaas#776) — a permanent
// SessionUnknown that made the detector inert in production. The job knows
// which tenant its sessions were filed under, so the tenant is bound here, the
// same way every other untenanted caller in this package binds it
// (RecordJobDeath, job_admission.go).
func (d *JobStallDetector) addressable(ctx context.Context, tenantID string, sessionIDs []string) (bool, error) {
	if tenantID != "" {
		ctx = ContextWithTenant(ctx, tenantID)
	}
	for _, id := range sessionIDs {
		resp, err := d.sessions.SessionStatus(ctx, &SessionStatusRequest{SessionID: id})
		if err != nil {
			var missing *SessionNotFoundError
			if errors.As(err, &missing) {
				// A session the service no longer knows is not addressable,
				// and that is an answer rather than an unknown.
				continue
			}
			return false, err
		}
		switch resp.State {
		case SessionStateActive, SessionStateIdle, SessionStateResumable:
			return true, nil
		}
	}
	return false, nil
}

// =============================================================================
// Repetition
// =============================================================================

// idleLedger remembers when each job was last reported idle, so a standing
// wait is raised once per threshold period rather than once per sweep.
// Process-local and deliberately not durable: a restart re-reports every idle
// job once, which is what the operator who just restarted the daemon needs.
type idleLedger struct {
	mu       sync.Mutex
	reported map[string]time.Time
}

func newIdleLedger() *idleLedger {
	return &idleLedger{reported: map[string]time.Time{}}
}

// shouldReport records a sighting and reports whether it is worth raising.
func (l *idleLedger) shouldReport(key string, now time.Time, threshold time.Duration) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	last, seen := l.reported[key]
	if seen && now.Sub(last) < threshold {
		return false
	}
	l.reported[key] = now
	return true
}

// retain drops jobs that are no longer idle, so a recovery followed by a
// second stall is reported afresh, and entries older than staleAfter, which
// would let the next sighting report anyway.
func (l *idleLedger) retain(live map[string]bool, now time.Time, staleAfter time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for key, reportedAt := range l.reported {
		if !live[key] || (staleAfter > 0 && now.Sub(reportedAt) > staleAfter) {
			delete(l.reported, key)
		}
	}
}

// idleKey names one job. Length-prefixed so no pair can collide by sharing a
// separator.
func idleKey(tenantID, jobID string) string {
	return strconv.Itoa(len(tenantID)) + ":" + tenantID + "/" + jobID
}

// =============================================================================
// Errors
// =============================================================================

var (
	// ErrJobStalled indicates a job the stall detector ended.
	ErrJobStalled = errors.New("orchestration: job stalled")

	// ErrJobIdleThreshold indicates a threshold naming no watched state.
	ErrJobIdleThreshold = errors.New("orchestration: idle threshold names no watched job state")
)

// JobStalledError is the terminal error of a job the stall detector ended.
type JobStalledError struct {
	JobID    string
	State    JobState
	Idle     time.Duration
	Evidence string
}

// Error implements the error interface.
func (e *JobStalledError) Error() string {
	return ErrJobStalled.Error() + ": job=" + e.JobID + " state=" + string(e.State) + " idle=" + e.Idle.String()
}

// Unwrap returns the sentinel for errors.Is support.
func (e *JobStalledError) Unwrap() error { return ErrJobStalled }

// JobIdleThresholdError reports a configured threshold on a state the
// detector does not watch.
type JobIdleThresholdError struct {
	State string
}

// Error implements the error interface.
func (e *JobIdleThresholdError) Error() string {
	return ErrJobIdleThreshold.Error() + ": " + e.State
}

// Unwrap returns the sentinel for errors.Is support.
func (e *JobIdleThresholdError) Unwrap() error { return ErrJobIdleThreshold }
