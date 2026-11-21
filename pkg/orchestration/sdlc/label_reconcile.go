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

package sdlc

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// Label reconciliation — the forge is re-read, not assumed to have been written
// =============================================================================
//
// THE CONCLUSION SEAM MAKES EVERY NEW ENDING REACH THE FORGE. IT CANNOT FIX AN
// ENDING THAT ALREADY DID NOT.
//
// go-quicbaas#788 added the conclusion observer in outcome.go: every way a task
// stops now posts its outcome and moves the issue's label. That closes the hole
// going forward and leaves two open behind it, both measured on the burn-in node
// — seven go-truststrap issues (#25, #26, #52, #53, #54, #55, #56) carrying
// `factory:in-progress` for tasks that had been terminal for days:
//
//   - A CONCLUSION WHOSE FORGE WRITE FAILED IS NEVER REVISITED.
//     TaskConcluded returns SurfacingUndeliveredError when the forge refuses
//     every retry, and the engine treats an observer error as a report that did
//     not land — correctly, because the task is already written and durable.
//     Nothing tries again. The issue keeps the in-progress label for ever.
//
//   - A CONCLUSION THAT PREDATES THE SEAM WAS NEVER POSTED AT ALL.
//     Every task that stopped before #788 shipped left its issue claiming live
//     work, and no code path visits those issues again.
//
// There is no liveness check from the forge's side either: if the node dies
// between concluding the task and posting the comment, the label is stuck with
// nobody watching. From the forge an operator cannot tell a live run from a
// corpse, which is the whole failure — the label is the only thing a person
// outside the daemon can read.
//
// SO THE FORGE IS RE-READ. This sweep walks every bound project, asks the forge
// which issues are CLAIMING live work, and compares that claim with the engine's
// own records. It converges in both directions:
//
//	claim                  engine record          action
//	---------------------  ---------------------  ---------------------------------
//	in_progress/in_review  task is handed over    nothing; this is the healthy state
//	in_progress/in_review  task is queued         report it; the claim is premature
//	in_progress/in_review  task is terminal       post the outcome, swap the label
//	in_progress/in_review  the worker parked it   restore the park's label, post nothing
//	in_progress/in_review  no task at all         post `abandoned`, swap the label
//	no lifecycle label     task is handed over    re-apply in_progress, post nothing
//	a park's label         task was readmitted    clear the label, post nothing
//
// THE TWO ROWS THAT WRITE ARE NEVER DECIDED FROM THE PASS'S OPENING SNAPSHOT.
// Both the terminal row and the "no task at all" row re-read the engine's
// record — after the claim that provoked the question, before anything is
// written — so neither can announce an ending over a run adopted while the pass
// was in flight. See taskSinceSnapshot.
//
// IT WRITES NOTHING OF ITS OWN. The comment is rendered by renderOutcome from
// the one table in outcome.go, and the post is made by Hooks.TaskConcluded —
// the same path #788 built, reached from a sweep rather than from the engine.
// A second vocabulary here would be a second thing to keep in step, and the
// drift between them would be invisible in exactly the way this defect was.
//
// IT IS IDEMPOTENT ON THE COMMENT. Before posting, the issue's discussion is
// read and searched for THIS task's, THIS attempt's, THIS outcome's marker —
// blockedMarker, the same identity a resumed run checks (runnerRun.parkPosted).
// That is what stops a sweep running every five minutes against a project whose
// forge write failed once from leaving five hundred comments behind it, and it
// is a complete answer to repetition in time.
//
// IT IS IDEMPOTENT ACROSS NODES BY A RESERVATION. The sweep starts on every
// SDLC-enabled server and its boot pass runs immediately, so two nodes can read
// the same discussion, both find the marker absent, and both post: a read-then-
// write across a forge with no conditional create cannot be made atomic. So
// after the marker check and BEFORE the post, the node reserves (issue,
// reconcile key) in the shared store with a put-if-absent (OutcomeReserver).
// Only the winner posts; a node that loses skips and its next sweep finds the
// marker.
//
// THE RESERVATION CARRIES A TTL, because one without a lease is worse than the
// duplicate it prevents: a node that dies between reserving and posting would
// hold the claim for ever and the outcome would never be announced — silent and
// permanent, the defect class this whole sweep exists to end. When the TTL
// lapses another node displaces the dead holder, and the comment-marker check is
// the read-side idempotency for that case: a holder that posted and then
// expired leaves its marker, and nobody posts again.
//
// A RESERVATION THAT FAILS AT THE STORE SKIPS THE POST. It fails closed and is
// logged; the finding is deferred and the next sweep tries again. With no
// OutcomeReserver configured only the marker check applies, which holds within
// one node and leaves the cosmetic duplicate across nodes.
//
// IT IS FAIL-CLOSED AND NEVER FATAL. A forge that cannot be read is a finding
// the operator sees and the next sweep retries, never a crash and never a hot
// loop: the pass is paced, every step is bounded, and a project that cannot be
// swept does not stop the projects after it.

// DefaultLabelReconcileInterval is the shipped cadence of the sweep.
//
// It is slower than the task-source poller's because it is a REPAIR pass over
// state the ordinary paths already maintain: everything it fixes has been wrong
// since a write failed, and five more minutes of a stale label costs an operator
// nothing next to the days the defect measured.
const DefaultLabelReconcileInterval = 5 * time.Minute

// DefaultForgeCallInterval is the minimum wall time between one reconciliation
// step's forge calls and the next's.
//
// THE BURST IS WHAT IS BOUNDED, not the steady state. A converged project makes
// two listings and stops; a project whose every issue drifted — the shape the
// first sweep after this ships will meet — would otherwise post as fast as the
// forge would accept, and a forge that starts refusing under that load turns one
// repair pass into a rate-limit ban for the whole node.
const DefaultForgeCallInterval = 200 * time.Millisecond

// =============================================================================
// Ports
// =============================================================================

// TaskLister lists a project's tasks. orchestration.TaskStore satisfies it.
type TaskLister interface {
	ListTasks(ctx context.Context, tenantID string, filter *orchestration.TaskFilter) ([]*orchestration.Task, error)
}

// TaskRecordLister lists a project's tasks AND names the records it could not
// read. orchestration.TaskStore satisfies it.
//
// THE LABEL SWEEP NEEDS THE SECOND HALF because it decides from ABSENCE: an
// issue claiming live work with no task answering for it is an orphan, and a
// spent task answers for its issue only when no newer one does. A record the
// store could not decode is missing from a plain listing exactly as a deleted
// one is, so a sweep reading one would announce an ending over live work
// (go-quicbaas#848). The re-read before every write refuses on an incomplete
// listing instead.
type TaskRecordLister interface {
	ListTaskRecords(ctx context.Context, tenantID string, filter *orchestration.TaskFilter) (*orchestration.TaskListing, error)
}

// ForgePacer bounds how often a sweep may touch the forge.
//
// An interface rather than a duration so a test drives the pass without
// wall-clock waits, the way RetryWaiter does for the merge retry.
type ForgePacer interface {
	// Pace returns when the caller may make its next forge call, or the
	// context's error when the sweep was cancelled while waiting.
	Pace(ctx context.Context) error
}

// NewForgePacer returns the shipped pacer: at most one forge step per
// interval. A non-positive interval takes DefaultForgeCallInterval, because
// zero in a configuration file means "take the default" everywhere else in
// this module and an unpaced sweep is not a thing an operator asks for by
// leaving a field out.
func NewForgePacer(interval time.Duration) ForgePacer {
	if interval <= 0 {
		interval = DefaultForgeCallInterval
	}
	return &intervalPacer{interval: interval, now: time.Now}
}

// intervalPacer admits one forge step per interval.
//
// Lock-free: the only shared state is the instant of the last admitted step,
// advanced by compare-and-swap. Two goroutines racing the same slot leave
// exactly one holder and the loser waits, which is the behaviour a mutex would
// have given at the cost of parking the loser inside the critical section.
type intervalPacer struct {
	interval time.Duration
	now      func() time.Time
	last     atomic.Int64
}

// Pace implements ForgePacer.
func (p *intervalPacer) Pace(ctx context.Context) error {
	if p.interval <= 0 {
		return ctx.Err()
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		now := p.now().UnixNano()
		last := p.last.Load()
		earliest := last + int64(p.interval)
		if last == 0 || now >= earliest {
			if p.last.CompareAndSwap(last, now) {
				return nil
			}
			continue
		}
		timer := time.NewTimer(time.Duration(earliest - now))
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

// =============================================================================
// The reconciler
// =============================================================================

// LabelReconcilerConfig wires the sweep.
type LabelReconcilerConfig struct {
	// Tenants enumerates the tenants whose projects are swept. It is the
	// SAME lister the task-source poller reads, so the sweep sees exactly
	// the projects the factory adopts work from — a reconciliation with
	// narrower reach than the thing it reconciles would leave a class of
	// issues permanently unvisited.
	Tenants orchestration.TenantLister

	// Bindings lists a tenant's bound projects and reads one binding.
	Bindings BindingReader

	// Tasks lists a project's tasks, live and terminal alike, and names the
	// records it could not read.
	Tasks TaskRecordLister

	// Hooks is the module's own lifecycle hook set, and the sweep's ONLY
	// writer: the outcome comment is posted through its TaskConcluded and the
	// labels are moved through its label mover, so a reconciled ending is
	// byte-for-byte the ending the engine would have written itself.
	Hooks *Hooks

	// Pacer bounds the forge call rate; nil takes DefaultForgeCallInterval.
	Pacer ForgePacer

	// Reservations decides which node posts an outcome comment, so two nodes
	// sweeping one project post it once. Nil leaves only the comment-marker
	// check, which is idempotent within one node.
	Reservations OutcomeReserver

	// Logger receives the reservations this node lost or could not take; nil
	// takes slog.Default.
	Logger *slog.Logger
}

// LabelReconciler converges a bound project's forge labels onto the engine's
// own record of its tasks.
type LabelReconciler struct {
	tenants  orchestration.TenantLister
	bindings BindingReader
	tasks    TaskRecordLister
	hooks    *Hooks
	pacer    ForgePacer

	reservations OutcomeReserver
	logger       *slog.Logger
}

// NewLabelReconciler builds the sweep; every collaborator is required.
func NewLabelReconciler(cfg LabelReconcilerConfig) (*LabelReconciler, error) {
	switch {
	case cfg.Tenants == nil:
		return nil, &InvalidInputError{Field: "tenants", Message: "the sweep visits every tenant's bound projects and has no way to enumerate them"}
	case cfg.Bindings == nil:
		return nil, &InvalidInputError{Field: "bindings", Message: "the forge and its label map live on the binding"}
	case cfg.Tasks == nil:
		return nil, &InvalidInputError{Field: "tasks", Message: "a label is compared against the engine's own record of the task"}
	case cfg.Hooks == nil:
		return nil, &InvalidInputError{Field: "hooks", Message: "the outcome is posted through the same hooks the engine posts it through"}
	}
	pacer := cfg.Pacer
	if pacer == nil {
		pacer = &intervalPacer{interval: DefaultForgeCallInterval, now: time.Now}
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &LabelReconciler{
		tenants: cfg.Tenants, bindings: cfg.Bindings, tasks: cfg.Tasks, hooks: cfg.Hooks, pacer: pacer,
		reservations: cfg.Reservations, logger: logger,
	}, nil
}

// =============================================================================
// Verdicts
// =============================================================================

// LabelReconcileVerdict is what one pass decided about one issue.
type LabelReconcileVerdict string

const (
	// LabelReconcileConsistent means the issue's labels already agree with
	// the engine's record. Nothing was written.
	LabelReconcileConsistent LabelReconcileVerdict = "consistent"

	// LabelReconcileSurfaced means the outcome was posted and the label
	// swapped: an ending that never reached the forge, now on it.
	LabelReconcileSurfaced LabelReconcileVerdict = "surfaced"

	// LabelReconcileRelabelled means the outcome comment was ALREADY on the
	// issue and only the label was wrong. It is the second run of a sweep
	// whose post landed and whose label move did not, and the verdict is
	// distinct from Surfaced precisely so an operator can tell the two apart:
	// one says a report was missing, the other says only its display was.
	LabelReconcileRelabelled LabelReconcileVerdict = "relabelled"

	// LabelReconcileRestored means a task the engine still considers live
	// had no lifecycle label on its issue — a lost hand-over — and the
	// in-progress label was re-applied. No comment is posted: nothing ended.
	LabelReconcileRestored LabelReconcileVerdict = "restored"

	// LabelReconcileDeferred means the forge refused and nothing moved. The
	// finding carries the typed cause and the next sweep tries again.
	LabelReconcileDeferred LabelReconcileVerdict = "deferred"

	// LabelReconcileUndecidable means the sweep found no truthful write to
	// make on the issue, so it made none and said what it found instead.
	//
	// FAIL-CLOSED, AND REPORTED RATHER THAN SILENT. Two states reach here.
	// A task whose record names no conclusion at all — one an operator failed
	// by hand rather than the engine concluding it — because
	// announcing "this task has spent its attempt budget" would be a false
	// statement on somebody's issue. And an issue claiming live work over a
	// QUEUED task, because the claim is premature rather than false and the
	// only write available would un-claim a run that is being dispatched.
	// Saying nothing at all is what neither may do: silence is the defect
	// this sweep exists to end.
	LabelReconcileUndecidable LabelReconcileVerdict = "undecidable"
)

// String returns the verdict's name.
func (v LabelReconcileVerdict) String() string { return string(v) }

// Wrote reports whether this verdict changed anything on the forge.
func (v LabelReconcileVerdict) Wrote() bool {
	switch v {
	case LabelReconcileSurfaced, LabelReconcileRelabelled, LabelReconcileRestored:
		return true
	default:
		return false
	}
}

// LabelReconcileFinding is one issue's outcome from a pass.
type LabelReconcileFinding struct {
	// TenantID and ProjectID scope the finding.
	TenantID  string
	ProjectID string

	// Issue is the forge issue examined, zero when the finding is about the
	// project rather than about one issue.
	Issue int

	// TaskID is the task the issue mirrors, empty when there is none.
	TaskID string

	// Outcome is what was announced, empty when nothing was.
	Outcome OutcomeKind

	// Verdict is what the sweep decided.
	Verdict LabelReconcileVerdict

	// Evidence is the operator-facing reason for the verdict.
	Evidence string
}

// Subject renders the finding's subject for a log line.
func (f LabelReconcileFinding) Subject() string {
	subject := f.TenantID + "/" + f.ProjectID
	if f.Issue > 0 {
		subject += "#" + strconv.Itoa(f.Issue)
	}
	return subject
}

// LabelReconcileReport is a whole pass.
type LabelReconcileReport struct {
	// Findings is one entry per issue examined, ordered by tenant, project
	// and issue number so two passes over the same state produce comparable
	// reports.
	Findings []LabelReconcileFinding
}

// Count returns how many findings carry a verdict.
func (r LabelReconcileReport) Count(v LabelReconcileVerdict) int {
	n := 0
	for _, finding := range r.Findings {
		if finding.Verdict == v {
			n++
		}
	}
	return n
}

// Drifted returns the findings the sweep did not read as already consistent —
// what an operator wants logged and what a test asserts on.
func (r LabelReconcileReport) Drifted() []LabelReconcileFinding {
	drifted := make([]LabelReconcileFinding, 0, len(r.Findings))
	for _, finding := range r.Findings {
		if finding.Verdict != LabelReconcileConsistent {
			drifted = append(drifted, finding)
		}
	}
	return drifted
}

// sort orders the findings so two passes are comparable.
func (r *LabelReconcileReport) sort() {
	sort.SliceStable(r.Findings, func(i, j int) bool {
		a, b := r.Findings[i], r.Findings[j]
		if a.TenantID != b.TenantID {
			return a.TenantID < b.TenantID
		}
		if a.ProjectID != b.ProjectID {
			return a.ProjectID < b.ProjectID
		}
		return a.Issue < b.Issue
	})
}

// =============================================================================
// The pass
// =============================================================================

// Reconcile sweeps every bound project of every tenant.
//
// Its error return is for the ONE fault that ends the whole pass: the tenants
// could not be enumerated, so the sweep does not know what it was meant to
// visit. Everything narrower — a tenant whose bindings cannot be listed, a
// project whose forge refuses — is a deferred FINDING, because one unreachable
// forge must not stop every other project being reconciled.
func (r *LabelReconciler) Reconcile(ctx context.Context) (LabelReconcileReport, error) {
	tenantIDs, err := r.tenants.ListTenantIDs(ctx)
	if err != nil {
		return LabelReconcileReport{}, &LabelReconcileUnsweptError{Cause: err}
	}
	report := LabelReconcileReport{}
	for _, tenantID := range tenantIDs {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		bindings, err := r.bindings.ListBindings(ctx, tenantID)
		if err != nil {
			report.Findings = append(report.Findings, LabelReconcileFinding{
				TenantID: tenantID,
				Verdict:  LabelReconcileDeferred,
				Evidence: "the tenant's bound projects could not be listed, so none were reconciled: " + err.Error(),
			})
			continue
		}
		for _, binding := range bindings {
			if binding == nil {
				continue
			}
			if err := ctx.Err(); err != nil {
				report.sort()
				return report, err
			}
			projectReport, err := r.ReconcileProject(ctx, tenantID, binding.ProjectID)
			report.Findings = append(report.Findings, projectReport.Findings...)
			if err != nil {
				if ctxErr := ctx.Err(); ctxErr != nil {
					report.sort()
					return report, ctxErr
				}
				report.Findings = append(report.Findings, LabelReconcileFinding{
					TenantID: tenantID, ProjectID: binding.ProjectID,
					Verdict:  LabelReconcileDeferred,
					Evidence: "the project could not be reconciled and is retried on the next sweep: " + err.Error(),
				})
			}
		}
	}
	report.sort()
	return report, nil
}

// ReconcileProject converges one bound project's forge labels.
//
// Its error return is for faults that end the PROJECT's pass — a binding that
// cannot be read, a task store that cannot list, a forge that cannot be
// resolved or whose issue listing fails. One ISSUE that cannot be settled is a
// finding, so a single unreadable discussion does not abandon the rest.
func (r *LabelReconciler) ReconcileProject(ctx context.Context, tenantID, projectID string) (LabelReconcileReport, error) {
	if tenantID == "" || projectID == "" {
		return LabelReconcileReport{}, &InvalidInputError{
			Field:   "project",
			Message: "a reconciliation is scoped to one project of one tenant",
		}
	}
	binding, err := r.bindings.GetBinding(ctx, tenantID, projectID)
	if err != nil {
		return LabelReconcileReport{}, err
	}
	if binding == nil {
		return LabelReconcileReport{}, &NotFoundError{Entity: bindingEntityName, TenantID: tenantID, ID: projectID}
	}
	// THE SNAPSHOT MAY BE INCOMPLETE and is still used: nothing is written
	// from it, and every direction that writes re-reads through
	// taskSinceSnapshot, which refuses an incomplete listing. Failing the
	// whole project here would let one unreadable record stop the sweep
	// from even reporting on the rest.
	listing, err := r.tasks.ListTaskRecords(ctx, tenantID, &orchestration.TaskFilter{ProjectID: projectID})
	if err != nil {
		return LabelReconcileReport{}, err
	}
	var tasks []*orchestration.Task
	if listing != nil {
		tasks = listing.Tasks
	}
	forge, err := r.hooks.forges.ForgeFor(ctx, tenantID, binding.Forge)
	if err != nil {
		return LabelReconcileReport{}, err
	}
	if forge == nil {
		return LabelReconcileReport{}, NewForgeNotRegisteredError(binding.Forge.Type)
	}

	byIssue := indexTasksByIssue(binding.Repo(), tasks)
	claimed, err := r.claimedIssues(ctx, forge, binding)
	if err != nil {
		return LabelReconcileReport{}, err
	}

	pass := &labelPass{
		reconciler: r, tenantID: tenantID, projectID: projectID,
		binding: binding, forge: forge, repo: binding.Repo(), byIssue: byIssue, claimed: claimed,
	}
	report := LabelReconcileReport{}
	report.Findings = append(report.Findings, pass.examineClaims(ctx)...)
	report.Findings = append(report.Findings, pass.examineHandovers(ctx)...)
	report.Findings = append(report.Findings, pass.examineReadmissions(ctx)...)
	report.sort()
	// A CANCELLED PASS IS AN INCOMPLETE PASS AND SAYS SO. Both directions stop
	// where they are when the sweep is cancelled, so the findings are a prefix
	// rather than the project's state; returning nil would let a caller — the
	// fleet walk above included — read a shutdown as a reconciled project.
	return report, ctx.Err()
}

// claimedIssues asks the forge which issues carry a label CLAIMING live work.
//
// ONLY THE CLAIMING LABELS ARE LISTED, and that is the difference between a
// reconciliation and a demolition. `in_progress` and `in_review` assert that
// the Software Factory is working on the issue right now, so one of them on a
// terminal task is a lie the sweep must correct. Every other lifecycle label
// asserts the opposite — the work STOPPED — and an issue resting on `escalated`
// or `merged` with no task is the documented, correct end state of a task that
// spent its budget and released its reservation (see outcomeRemedy). Listing
// those too would have the sweep post `abandoned` over every issue the factory
// ever finished with.
func (r *LabelReconciler) claimedIssues(ctx context.Context, forge ForgeBackend, binding *ProjectBinding) (map[int]ForgeIssue, error) {
	claimed := make(map[int]ForgeIssue)
	for _, label := range claimingLabels(binding) {
		if err := r.pacer.Pace(ctx); err != nil {
			return nil, err
		}
		// STATE "all": an issue somebody closed by hand while its task was
		// running still carries the claim, and is exactly the issue whose
		// stale label nobody is going to notice.
		//
		// UNBOUNDED: a Limit would return a prefix in an order the adapter
		// chooses, and an issue outside it would be invisible to every sweep
		// rather than to this one. The claiming set is bounded by the
		// project's live work instead, and an adapter that cannot deliver the
		// whole listing says so (IssueFilter.Limit) rather than truncating.
		issues, err := forge.ListIssues(ctx, binding.Repo(), IssueFilter{State: "all", Labels: []string{label}})
		if err != nil {
			return nil, &LabelReconcileForgeError{
				Repo: binding.Repo().String(), Operation: "ListIssues", Cause: err,
			}
		}
		for _, issue := range issues {
			claimed[issue.Number] = issue
		}
	}
	return claimed, nil
}

// claimingLabels returns the forge labels that assert live work, deduplicated
// and in a stable order.
func claimingLabels(binding *ProjectBinding) []string {
	seen := make(map[string]bool, len(claimingLabelKeys))
	labels := make([]string, 0, len(claimingLabelKeys))
	for _, key := range claimingLabelKeys {
		label := binding.Forge.Label(key)
		if label == "" || seen[label] {
			continue
		}
		seen[label] = true
		labels = append(labels, label)
	}
	return labels
}

// claimingLabelKeys are the binding label keys whose presence on an issue
// asserts that the Software Factory is working on it right now. They are a
// SUBSET of the module's existing vocabulary, not an addition to it.
var claimingLabelKeys = []string{LabelInProgress, LabelInReview}

// labelPass is one project's pass: everything resolved once and read by both
// directions of the comparison.
type labelPass struct {
	reconciler *LabelReconciler
	tenantID   string
	projectID  string
	binding    *ProjectBinding
	forge      ForgeBackend
	repo       RepoRef

	// byIssue is the engine's record, indexed by the issue each task was
	// adopted from.
	byIssue map[int]*orchestration.Task

	// claimed is the forge's claim, indexed by issue number.
	claimed map[int]ForgeIssue
}

// finding starts a finding already scoped to this pass.
func (p *labelPass) finding(issue int, task *orchestration.Task) LabelReconcileFinding {
	f := LabelReconcileFinding{TenantID: p.tenantID, ProjectID: p.projectID, Issue: issue}
	if task != nil {
		f.TaskID = task.ID
	}
	return f
}

// examineClaims walks the issues the forge says are being worked on and
// settles each against the engine's record.
func (p *labelPass) examineClaims(ctx context.Context) []LabelReconcileFinding {
	numbers := make([]int, 0, len(p.claimed))
	for number := range p.claimed {
		numbers = append(numbers, number)
	}
	sort.Ints(numbers)

	findings := make([]LabelReconcileFinding, 0, len(numbers))
	for _, number := range numbers {
		if err := ctx.Err(); err != nil {
			return findings
		}
		findings = append(findings, p.examineClaim(ctx, p.claimed[number]))
	}
	return findings
}

// examineClaim settles ONE claiming issue.
//
// THE THREE ANSWERS ARE HANDED OVER, QUEUED AND TERMINAL, and only the first
// of them makes the claim true. A handed-over task is the healthy state and
// costs nothing. A terminal one is the measured defect and is surfaced. A
// QUEUED one is neither: see reportQueued.
//
// NOTHING DESTRUCTIVE IS DECIDED FROM THE OPENING SNAPSHOT. The two answers
// that WRITE — a terminal task's outcome, and the orphan's `abandoned` — are
// both taken only after the record has been read again; see taskSinceSnapshot
// for why the re-read is a different answer rather than a narrower window. The
// two that write nothing keep the snapshot's answer, because a record that
// cannot be acted on does not need to be fresh, and a live task can only have
// become MORE terminal since, which the next sweep settles.
func (p *labelPass) examineClaim(ctx context.Context, issue ForgeIssue) LabelReconcileFinding {
	task := p.byIssue[issue.Number]
	if task == nil || !liveTaskStatuses[task.Status] {
		fresh, err := p.taskSinceSnapshot(ctx, issue.Number)
		if err != nil {
			return p.deferFinding(p.finding(issue.Number, task), err)
		}
		task = fresh
	}
	if task == nil {
		return p.surfaceOrphan(ctx, issue)
	}
	if handedOverTaskStatuses[task.Status] {
		finding := p.finding(issue.Number, task)
		finding.Verdict = LabelReconcileConsistent
		finding.Evidence = "the task is " + string(task.Status) + " and the claim is true"
		return finding
	}
	if liveTaskStatuses[task.Status] {
		return p.reportQueued(issue, task)
	}
	return p.surfaceTerminal(ctx, issue, task)
}

// taskSinceSnapshot re-reads the engine's record for ONE claiming issue and
// returns whatever answers for it now, which may be nothing.
//
// THE SNAPSHOT IS TAKEN BEFORE THE CLAIM IS READ AND EVERY WRITE HERE IS FINAL,
// so the snapshot alone cannot carry those decisions. It is asked before BOTH
// destructive answers, not only the orphan: a terminal task releases its source
// reservation, so re-labelling the issue adopts a FRESH task while the spent one
// stays on the record (see indexTasksByIssue), and a pass holding only the spent
// one would post its ending and move the issue to a terminal label over a run
// that is live right now. The re-read is indexed by the same live-supersedes
// rule, so the newly adopted task wins exactly as it would have in the snapshot
// had the snapshot been late enough to see it. ReconcileProject lists the
// tasks and then lists the forge, and a pass over a drifted project spends a
// paced forge round trip on every issue before it — so between the snapshot and
// the moment this issue is settled there is a window of seconds in which an
// ordinary adoption happens: the task-source poller persists a new task
// (CreateTask) and the run it dispatches labels the issue in progress (the
// Prepare hook). Writing from the snapshot would announce an ending on that
// live run and move its issue to a terminal label — strictly worse than the
// stale label the sweep exists to correct, because a lie that a sweep WRITES is
// one an operator has no reason to doubt.
//
// THE RE-READ IS NOT A NARROWER WINDOW, IT IS A DIFFERENT ANSWER, and that is
// why it is here rather than a reordering of the two listings. The engine
// persists a task BEFORE anything labels its issue, so a claim observed by the
// forge listing at T implies its task was durable before T; a record read
// strictly after T therefore SEES it. Listing the forge first and the tasks
// second would close this one interleaving and leave the seconds a paced pass
// spends between the listing and this issue wide open — and would buy, in
// exchange, a false "lost hand-over" on every task adopted during the window.
//
// A STORE THAT CANNOT ANSWER DEFERS. Fail-closed: an unreadable record is not
// evidence that no record exists, so nothing is written and the next sweep
// re-examines the same issue.
//
// SO DOES A STORE THAT ANSWERS ONLY IN PART (go-quicbaas#848). A record the
// store could not decode cannot be filtered, so it may be this issue's — the
// live task that should answer for it, or the newer one that supersedes a spent
// one — and the listing cannot say which. Every write this pass makes is
// decided from what the listing does NOT contain as much as from what it does,
// so an incomplete listing defers the write exactly as a refused one does.
func (p *labelPass) taskSinceSnapshot(ctx context.Context, number int) (*orchestration.Task, error) {
	listing, err := p.reconciler.tasks.ListTaskRecords(ctx, p.tenantID, &orchestration.TaskFilter{ProjectID: p.projectID})
	if err == nil {
		err = listing.Err(p.tenantID)
	}
	if err != nil {
		return nil, &LabelReconcileRecordError{
			TenantID: p.tenantID, ProjectID: p.projectID, Issue: number, Cause: err,
		}
	}
	// Indexed by the same rule the pass indexed its snapshot by, so a fresh
	// read and a snapshot read of the same state answer identically.
	return indexTasksByIssue(p.repo, listing.Tasks)[number], nil
}

// reportQueued settles a claiming issue whose task is QUEUED rather than
// running: it says what it found and writes nothing.
//
// PENDING IS NOT A HANDED-OVER RUN — examineHandovers refuses to APPLY the
// in-progress label to a queued task for exactly that reason — so reading an
// existing claim over one as the healthy state was the same statement made
// backwards. It is reachable both ways round: a reaped attempt with budget left
// is requeued to pending while its issue still carries the label its previous
// attempt applied, and a re-labelled issue whose stale claim was never cleared
// is adopted as a fresh pending task that auto-spawn may refuse for as long as
// no agent can take it. The second is precisely the shape this sweep exists to
// end, and `consistent` — the sweep's silent verdict, filtered out of
// Drifted() — would certify it as healthy for ever.
//
// IT STILL WRITES NOTHING, and that is deliberate rather than timid. There is
// no truthful write available: the task has not started, so the claim is
// premature; but the task is LIVE and about to start, so moving the issue back
// onto the trigger label would un-claim a run the engine is dispatching and
// flap the label on every task the sweep happened to catch mid-dispatch. So the
// verdict is the one the module already has for "no honest write exists":
// undecidable, reported to the operator, label untouched.
func (p *labelPass) reportQueued(issue ForgeIssue, task *orchestration.Task) LabelReconcileFinding {
	finding := p.finding(issue.Number, task)
	finding.Verdict = LabelReconcileUndecidable
	finding.Evidence = "the issue claims live work but the task is " + string(task.Status) +
		", which is queued rather than handed over, so the claim is premature rather than false " +
		"and the label was left alone"
	return finding
}

// examineReadmissions walks the tasks the engine put BACK in the queue and
// clears the park label their readmission never managed to take off.
//
// IT IS THE SAME DEFECT AS THE OTHERS, REACHED FROM A THIRD SIDE. A readmission
// is a conclusion: ReadmitTask persists `readmitted` on the record and then
// surfaces it, and the readmitted row's label key is EMPTY precisely so the park
// label comes off and the next attempt's Prepare can label the issue afresh. If
// that forge write fails — the whole reason this sweep exists — the record says
// the task is queued again and the issue still says `factory:blocked`.
//
// NEITHER OTHER DIRECTION SEES IT, which is why it needs its own. The claim
// direction lists only `in_progress` and `in_review`, and a park label is
// neither, so the issue is never examined. The hand-over direction skips pending
// tasks, because a queued task has not been handed over. So the issue sat with a
// label that lied until some later attempt happened to start — exactly the shape
// of the seven issues this sweep was written for.
//
// ONLY WHILE THE TASK IS STILL QUEUED. The reason survives into the next attempt
// — ClaimTask does not clear it — so the status is what says the park label is
// still stale; see awaitingRedispatch.
//
// A RE-TRIGGERED ISSUE IS NOT THIS. The trigger label is not a label this module
// owns as an outcome, and it is `labelsAgree` that decides whether anything must
// move: an issue already displaying no owned lifecycle label is consistent and
// costs one read.
func (p *labelPass) examineReadmissions(ctx context.Context) []LabelReconcileFinding {
	numbers := make([]int, 0, len(p.byIssue))
	for number, task := range p.byIssue {
		// An issue that claims live work is the claim direction's, and a
		// readmitted task whose next attempt has already started is the
		// healthy state it settles.
		if _, claimedAlready := p.claimed[number]; claimedAlready {
			continue
		}
		if !awaitingRedispatch(task) {
			continue
		}
		numbers = append(numbers, number)
	}
	sort.Ints(numbers)

	findings := make([]LabelReconcileFinding, 0, len(numbers))
	for _, number := range numbers {
		if err := ctx.Err(); err != nil {
			return findings
		}
		findings = append(findings, p.settleReadmission(ctx, number, p.byIssue[number]))
	}
	return findings
}

// awaitingRedispatch reports whether a task is a readmission STILL WAITING for
// its next attempt, which is the only state whose park label is stale.
//
// THE REASON ALONE IS NOT ENOUGH, AND THAT ASYMMETRY IS THE WHOLE POINT.
// ReadmitTask writes `readmitted` and ClaimTask does NOT clear it, so a task
// readmitted, then claimed, then run still carries the reason — it describes the
// last thing that concluded the task, which is exactly what a job page wants it
// for. Keying the repair on the reason alone therefore fires on LIVE work: the
// hand-over direction restores `factory:in-progress` for a running attempt whose
// issue lost its label, and this direction would then read the same stale reason
// and strip the label straight back off, leaving active work unlabelled. That is
// the display drift this sweep exists to end, caused by the sweep.
//
// So the STATUS decides whether the repair applies and the REASON decides which
// repair it is. Gating on pending also makes the two directions disjoint by
// construction rather than by luck — handedOverTaskStatuses is {claimed,
// in_progress} and this is {pending} — which
// TestLabelReconciler_HandoversAndReadmissionsCannotBothFire holds.
func awaitingRedispatch(task *orchestration.Task) bool {
	return task != nil &&
		task.Status == orchestration.TaskStatusPending &&
		task.ConclusionReason == orchestration.TaskConcludedReadmitted
}

// settleReadmission clears one readmitted task's issue of the park label.
//
// IT WRITES, SO IT RE-ESTABLISHES ITS PREMISE FIRST, the same as every other
// direction that writes: the record is read again and must still say
// `readmitted`, because a task readmitted and then parked again while the pass
// was in flight has a NEW park whose label is correct, and clearing it would
// undo a fresh ending. The issue is read too, because the claim listing says
// nothing about which non-claiming labels an issue carries.
func (p *labelPass) settleReadmission(ctx context.Context, number int, task *orchestration.Task) LabelReconcileFinding {
	finding := p.finding(number, task)
	finding.Outcome = OutcomeReadmitted

	fresh, err := p.taskSinceSnapshot(ctx, number)
	if err != nil {
		return p.deferFinding(finding, err)
	}
	if !awaitingRedispatch(fresh) {
		finding = p.finding(number, fresh)
		finding.Verdict = LabelReconcileUndecidable
		finding.Evidence = "the task stopped waiting for its next attempt while the sweep was in flight (" +
			restingDescription(fresh) + "), so its park label was left where it is"
		return finding
	}
	finding = p.finding(number, fresh)
	finding.Outcome = OutcomeReadmitted

	if err := p.reconciler.pacer.Pace(ctx); err != nil {
		return p.deferFinding(finding, err)
	}
	issue, err := p.forge.GetIssue(ctx, p.repo, number)
	if err != nil {
		return p.deferFinding(finding, &LabelReconcileForgeError{
			Repo: p.repo.String(), Operation: "GetIssue", Cause: err,
		})
	}
	if issue == nil {
		finding.Verdict = LabelReconcileUndecidable
		finding.Evidence = "the issue this task was adopted from is no longer readable on the forge, so no label was cleared"
		return finding
	}

	// The readmitted row's label key is the trigger, so settleLabels reads
	// "the trigger and nothing else this module owns" as agreement and moves
	// the issue there otherwise. The same helper the claim direction settles
	// through, for the same reason: one place decides what agreement means.
	return p.settleLabels(ctx, finding, *issue, outcomeSpecs[OutcomeReadmitted].labelKey,
		"the task was readmitted and the issue still carried the label its park left")
}

// surfaceTerminal settles a claiming issue whose task has stopped.
func (p *labelPass) surfaceTerminal(ctx context.Context, issue ForgeIssue, task *orchestration.Task) LabelReconcileFinding {
	finding := p.finding(issue.Number, task)

	reason, named := restingConclusion(task)
	if !named {
		finding.Verdict = LabelReconcileUndecidable
		finding.Evidence = "the task rests in status " + string(task.Status) +
			" and its record names no conclusion, so the label was left alone"
		return finding
	}
	kind, known := OutcomeOfConclusion(reason)
	if !known {
		finding.Verdict = LabelReconcileUndecidable
		finding.Evidence = (&UnknownConclusionError{Reason: string(reason), TaskID: task.ID}).Error()
		return finding
	}
	finding.Outcome = kind

	spec, announced := lookupOutcomeSpec(kind)
	if !announced {
		return p.settleSilent(ctx, finding, issue, task, reason)
	}

	posted, err := p.outcomePosted(ctx, issue.Number, task, kind)
	if err != nil {
		return p.deferFinding(finding, err)
	}
	if posted {
		return p.settleLabels(ctx, finding, issue, spec.labelKey,
			"the `"+string(kind)+"` outcome is already on the issue and only the label had not moved")
	}
	ctx, err = p.reserveOutcome(ctx, issue.Number, task, kind)
	if err != nil {
		return p.deferFinding(finding, err)
	}

	if err := p.reconciler.pacer.Pace(ctx); err != nil {
		return p.deferFinding(finding, err)
	}
	// THE ONE POSTER. TaskConcluded renders through renderOutcome, posts
	// through postWithRetry, records the comment on the task and moves the
	// label — the whole of #788's path, reached from a sweep. A reconciled
	// ending is therefore indistinguishable from one the engine posted
	// itself, which is the only way the two cannot drift.
	if err := p.reconciler.hooks.TaskConcluded(ctx, orchestration.TaskConclusion{
		TenantID: p.tenantID, Reason: reason, Task: task,
	}); err != nil {
		return p.deferFinding(finding, err)
	}
	finding.Verdict = LabelReconcileSurfaced
	finding.Evidence = "the task concluded `" + string(reason) + "` with nothing on the issue; the `" +
		string(kind) + "` outcome was posted and the label moved"
	return finding
}

// settleSilent settles an ending this module does not announce.
//
// SILENT IS NOT ONE ENDING, IT IS TWO, AND THEY LAND ON OPPOSITE LABELS. The
// outcome table maps both `completed` and `runner_parked` to OutcomeSilent,
// because in both cases the moment that caused the conclusion already wrote
// whatever the forge needed — and that is the only thing they have in common.
// `completed` means the pull request merged; `runner_parked` means the worker
// stopped and posted its own park comment, under the very marker the conclusion
// seam would have used, so the task is BLOCKED and waiting on a person.
//
// Reading the silence alone and settling on `merged` would tell the reader of a
// blocked issue that their work had landed. That is a worse lie than the stale
// claim it replaced: a stale `in_progress` is at least visibly unfinished, while
// `merged` closes the question. So the REASON decides, not the silence.
//
// A runner park lands on the label its own BlockKind names — the same
// blockedLabelKey the Blocked moment used, so the sweep restores exactly the
// label that moment was displaying — and nothing is posted, because the worker's
// comment is already there.
func (p *labelPass) settleSilent(ctx context.Context, finding LabelReconcileFinding, issue ForgeIssue,
	task *orchestration.Task, reason orchestration.TaskConclusionReason) LabelReconcileFinding {
	switch reason {
	case orchestration.TaskConcludedCompleted:
		return p.settleLabels(ctx, finding, issue, LabelMerged,
			"the task completed and the issue was still claiming live work")

	case orchestration.TaskConcludedRunnerParked:
		// FAIL-CLOSED ON AN INCOHERENT RECORD. The park label is read off
		// BlockKind, which only means anything while the task is actually
		// parked; a record naming a runner park while resting anywhere else is
		// one the sweep should report rather than relabel from.
		if task == nil || task.Status != orchestration.TaskStatusBlocked {
			finding.Verdict = LabelReconcileUndecidable
			finding.Evidence = "the record names a runner park but the task rests in status " +
				string(taskStatus(task)) + ", so the label its park would name cannot be trusted"
			return finding
		}
		return p.settleLabels(ctx, finding, issue, blockedLabelKey(task.BlockKind),
			"the worker posted its own park comment and the issue was still claiming live work; "+
				"the label the park names was restored and nothing was posted")

	default:
		// A silent reason the table gains without a row here would otherwise
		// fall through to whichever label the last branch happened to name.
		finding.Verdict = LabelReconcileUndecidable
		finding.Evidence = "the task concluded `" + string(reason) +
			"`, which this module does not announce and which names no label to settle on"
		return finding
	}
}

// taskStatus is a task's status, empty for no task.
func taskStatus(task *orchestration.Task) orchestration.TaskStatus {
	if task == nil {
		return ""
	}
	return task.Status
}

// surfaceOrphan settles a claiming issue with no task record at all.
//
// THE TASK IS GONE AND THE ISSUE STILL SAYS IT IS RUNNING. Whatever removed the
// record — an operator's delete, a store that lost it — took the only thing that
// could ever have concluded the issue with it, so the claim would stand for
// ever. `abandoned` is the table's row for an ending whose evidence is missing,
// and it is what the reader is owed: the work stopped, nothing diagnosed why.
//
// It cannot go through TaskConcluded, which requires the concluded record a
// caller here does not have, so it renders and posts the same row directly.
func (p *labelPass) surfaceOrphan(ctx context.Context, issue ForgeIssue) LabelReconcileFinding {
	finding := p.finding(issue.Number, nil)
	finding.Outcome = OutcomeAbandoned
	spec := outcomeSpecs[OutcomeAbandoned]

	posted, err := p.outcomePosted(ctx, issue.Number, nil, OutcomeAbandoned)
	if err != nil {
		return p.deferFinding(finding, err)
	}
	if posted {
		return p.settleLabels(ctx, finding, issue, spec.labelKey,
			"the `abandoned` outcome is already on the issue and only the label had not moved")
	}
	ctx, err = p.reserveOutcome(ctx, issue.Number, nil, OutcomeAbandoned)
	if err != nil {
		return p.deferFinding(finding, err)
	}

	if err := p.reconciler.pacer.Pace(ctx); err != nil {
		return p.deferFinding(finding, err)
	}
	body := renderOutcome(outcomeReport{
		binding: p.binding, task: nil, kind: OutcomeAbandoned, message: orphanEvidence(p.binding, issue),
	})
	if _, err := postWithRetry(ctx, p.forge, p.repo, issue.Number, body, p.reconciler.hooks.surfacing); err != nil {
		return p.deferFinding(finding, err)
	}
	if err := p.moveLabelsOn(ctx, issue, spec.labelKey); err != nil {
		return p.deferFinding(finding, err)
	}
	finding.Verdict = LabelReconcileSurfaced
	finding.Evidence = "no task record answers for this issue; the `abandoned` outcome was posted and the label moved"
	return finding
}

// orphanEvidence states, for the reader of the issue, what the sweep found.
func orphanEvidence(binding *ProjectBinding, issue ForgeIssue) string {
	claims := claimingLabels(binding)
	carried := make([]string, 0, len(claims))
	for _, label := range claims {
		for _, on := range issue.Labels {
			if on == label {
				carried = append(carried, "`"+label+"`")
			}
		}
	}
	evidence := "This issue was labelled "
	if len(carried) == 0 {
		evidence += "as work in progress"
	} else {
		evidence += strings.Join(carried, ", ")
	}
	evidence += ", but the Software Factory holds no task record for it: the record was removed or lost " +
		"while the issue still claimed live work, so nothing was ever going to conclude it.\n\n" +
		"To have the work attempted again, re-apply the trigger label `" + binding.Forge.TriggerLabel +
		"`; the next sweep adopts a fresh task."
	return evidence
}

// examineHandovers walks the tasks the engine is still working and re-applies
// the in-progress label to any whose issue carries none.
//
// THE LOST HAND-OVER IS THE CONVERSE DEFECT and it is not an outcome. Nothing
// ended: the engine is running the task right now, and the issue simply does
// not say so — the Prepare moment's label move failed, or the node died between
// claiming the task and making it. Posting an outcome would announce an ending
// that has not happened; re-applying the label says the true thing.
//
// ONLY A HANDED-OVER TASK QUALIFIES. A pending task is queued, not started, and
// its issue correctly still carries the trigger label; labelling it in progress
// would claim a run that has not begun.
func (p *labelPass) examineHandovers(ctx context.Context) []LabelReconcileFinding {
	// A BINDING THAT NAMES NO IN-PROGRESS LABEL HAS NOTHING TO RESTORE, and
	// without this the direction would fire on every live task of every sweep:
	// with no claiming label to list, no issue is ever found claiming, so
	// every handed-over task would look like a lost hand-over for ever and
	// buy a forge round trip each pass to move labels onto nothing.
	if p.binding.Forge.Label(LabelInProgress) == "" {
		return nil
	}
	numbers := make([]int, 0, len(p.byIssue))
	for number, task := range p.byIssue {
		if _, claimedAlready := p.claimed[number]; claimedAlready {
			continue
		}
		if !handedOverTaskStatuses[task.Status] {
			continue
		}
		numbers = append(numbers, number)
	}
	sort.Ints(numbers)

	findings := make([]LabelReconcileFinding, 0, len(numbers))
	for _, number := range numbers {
		if err := ctx.Err(); err != nil {
			return findings
		}
		findings = append(findings, p.restoreHandover(ctx, number, p.byIssue[number]))
	}
	return findings
}

// restoreHandover re-applies the in-progress label to one live task's issue.
//
// THIS DIRECTION WRITES TOO, so it asks the same question the claim direction
// asks before ITS writes: is the snapshot still true? Both halves of the
// premise are re-established, because either going stale turns the repair into
// damage.
//
// THE TASK MUST STILL BE HANDED OVER. A task that concluded while the pass was
// in flight has already had its ending posted and its issue moved onto a
// terminal label by TaskConcluded — and restoring from the snapshot would strip
// that label back off and re-assert live work for a task that has stopped,
// undoing a correct ending and leaving the forge lying until a later sweep
// repaired the repair.
//
// THE ISSUE MUST STILL LACK A CLAIM. The ordinary Prepare hook may have landed
// since the claims were listed, in which case the hand-over is not lost at all
// and the healthy state needs no write.
func (p *labelPass) restoreHandover(ctx context.Context, number int, task *orchestration.Task) LabelReconcileFinding {
	finding := p.finding(number, task)

	fresh, err := p.taskSinceSnapshot(ctx, number)
	if err != nil {
		return p.deferFinding(finding, err)
	}
	if fresh == nil || !handedOverTaskStatuses[fresh.Status] {
		finding = p.finding(number, fresh)
		finding.Verdict = LabelReconcileUndecidable
		finding.Evidence = "the task stopped being a hand-over while the sweep was in flight (" +
			restingDescription(fresh) + "), so the in-progress label was NOT re-applied over its ending"
		return finding
	}
	finding = p.finding(number, fresh)

	if err := p.reconciler.pacer.Pace(ctx); err != nil {
		return p.deferFinding(finding, err)
	}
	issue, err := p.forge.GetIssue(ctx, p.repo, number)
	if err != nil {
		return p.deferFinding(finding, &LabelReconcileForgeError{
			Repo: p.repo.String(), Operation: "GetIssue", Cause: err,
		})
	}
	if issue == nil {
		finding.Verdict = LabelReconcileUndecidable
		finding.Evidence = "the issue this task was adopted from is no longer readable on the forge, so nothing was restored"
		return finding
	}
	if carriesClaim(p.binding, *issue) {
		finding.Verdict = LabelReconcileConsistent
		finding.Evidence = "the issue claims live work again; the hand-over was not lost after all"
		return finding
	}

	if err := p.reconciler.pacer.Pace(ctx); err != nil {
		return p.deferFinding(finding, err)
	}
	if err := p.moveLabelsOn(ctx, *issue, LabelInProgress); err != nil {
		return p.deferFinding(finding, err)
	}
	finding.Verdict = LabelReconcileRestored
	finding.Evidence = "the engine is still working this task (" + string(fresh.Status) +
		") and the issue carried no lifecycle label; the hand-over was lost and the in-progress label was re-applied"
	return finding
}

// carriesClaim reports whether the issue displays any label asserting live work.
func carriesClaim(binding *ProjectBinding, issue ForgeIssue) bool {
	for _, claim := range claimingLabels(binding) {
		for _, on := range issue.Labels {
			if on == claim {
				return true
			}
		}
	}
	return false
}

// restingDescription names where a task now rests, for a finding's evidence.
func restingDescription(task *orchestration.Task) string {
	if task == nil {
		return "its record is gone"
	}
	return "it is now " + string(task.Status)
}

// settleLabels moves the issue onto the label key its outcome names, and says
// whether anything had to move.
func (p *labelPass) settleLabels(ctx context.Context, finding LabelReconcileFinding, issue ForgeIssue, key, evidence string) LabelReconcileFinding {
	if labelsAgree(p.binding, issue, key) {
		finding.Verdict = LabelReconcileConsistent
		finding.Evidence = "the issue already carries the label its outcome names"
		return finding
	}
	if err := p.reconciler.pacer.Pace(ctx); err != nil {
		return p.deferFinding(finding, err)
	}
	if err := p.moveLabelsOn(ctx, issue, key); err != nil {
		return p.deferFinding(finding, err)
	}
	finding.Verdict = LabelReconcileRelabelled
	finding.Evidence = evidence
	return finding
}

// moveLabelsOn moves the issue's module-owned labels onto key, reusing the
// issue record the listing already returned rather than reading it again.
func (p *labelPass) moveLabelsOn(ctx context.Context, issue ForgeIssue, key string) error {
	return p.reconciler.hooks.moveLabelsOn(ctx, &attempt{
		binding: p.binding, repo: p.repo, issue: issue.Number, forge: p.forge,
	}, &issue, key)
}

// deferFinding records a fault that left the issue exactly as it was found.
//
// EVERY FORGE FAULT ARRIVES HERE and none of them end the pass: the finding
// carries the typed cause for the operator, and the next sweep re-examines the
// same issue from scratch. A cancelled sweep is reported the same way, because
// from the issue's side an interrupted pass and a refused forge are the same
// thing — nothing was written and it will be tried again.
func (p *labelPass) deferFinding(finding LabelReconcileFinding, cause error) LabelReconcileFinding {
	finding.Verdict = LabelReconcileDeferred
	finding.Evidence = (&LabelReconcileDeferredError{
		Repo: p.repo.String(), Issue: finding.Issue, TaskID: finding.TaskID, Cause: cause,
	}).Error()
	return finding
}

// outcomePosted reports whether THIS outcome is already on the issue.
//
// The marker names the task, the attempt and the kind together — the same
// identity a resumed run checks before re-posting its park (runnerRun.parkPosted)
// — so a sweep cannot mistake an earlier attempt's comment for this one's, and
// cannot post a second comment for the one it already wrote.
//
// IT IS A READ, SO IT ORDERS NOTHING AGAINST ANOTHER NODE. Two sweeps can
// answer false here for the same issue; reserveOutcome is what then lets only
// one of them post. See the note on idempotence at the top of this file.
func (p *labelPass) outcomePosted(ctx context.Context, number int, task *orchestration.Task, kind OutcomeKind) (bool, error) {
	if err := p.reconciler.pacer.Pace(ctx); err != nil {
		return false, err
	}
	comments, err := p.forge.ListIssueComments(ctx, p.repo, number)
	if err != nil {
		return false, &LabelReconcileForgeError{
			Repo: p.repo.String(), Operation: "ListIssueComments", Cause: err,
		}
	}
	marker := blockedMarker(taskID(task), taskAttempt(task), kind)
	for _, comment := range comments {
		if strings.Contains(comment.Body, marker) {
			return true, nil
		}
	}
	return false, nil
}

// reserveOutcome takes the cross-node reservation for posting THIS outcome and
// returns nil only when this node won it.
//
// IT FAILS CLOSED. A store that cannot answer, or a reservation held by another
// node, both return an error and nothing is posted; the next sweep asks again,
// and once the holder's TTL lapses the comment-marker check in outcomePosted
// tells a posted outcome from one whose holder died.
//
// The returned context carries the winner's lease, which postWithRetry confirms
// and renews before every attempt, so a node whose retry budget outlasts its
// reservation stops rather than posting beside the node that displaced it. The
// TTL is derived from the surfacing policy for the same reason.
func (p *labelPass) reserveOutcome(ctx context.Context, number int, task *orchestration.Task, kind OutcomeKind) (context.Context, error) {
	if p.reconciler.reservations == nil {
		return ctx, nil
	}
	marker := blockedMarker(taskID(task), taskAttempt(task), kind)
	ttl := OutcomeReservationTTL(p.reconciler.hooks.surfacing)
	lease, err := p.reconciler.reservations.Reserve(ctx, p.tenantID, p.projectID, number, marker, ttl)
	var elsewhere *OutcomeReservedElsewhereError
	switch {
	case errors.As(err, &elsewhere):
		p.reconciler.logger.InfoContext(ctx, "outcome reserved by another node; skipping the post",
			"repo", p.repo.String(), "issue", number, "kind", string(kind))
		return ctx, err
	case err != nil:
		p.reconciler.logger.ErrorContext(ctx, "outcome reservation failed; skipping the post",
			"repo", p.repo.String(), "issue", number, "kind", string(kind), "error", err)
		return ctx, err
	}
	return withOutcomeLease(ctx, lease), nil
}

// taskAttempt is the attempt number an outcome comment is marked with, zero
// for an issue with no task record.
func taskAttempt(task *orchestration.Task) int {
	if task == nil {
		return 0
	}
	return task.Attempts
}

// =============================================================================
// Reading the engine's record
// =============================================================================

// liveTaskStatuses are the statuses in which the engine still intends to work
// the task. Everything else is terminal — the transition table admits nothing
// out of completed, failed or cancelled, and a blocked task moves only when a
// person or the unblock sweep readmits it.
var liveTaskStatuses = map[orchestration.TaskStatus]bool{
	orchestration.TaskStatusPending:    true,
	orchestration.TaskStatusClaimed:    true,
	orchestration.TaskStatusInProgress: true,
}

// handedOverTaskStatuses are the live statuses in which the issue has ALREADY
// been handed to the module, so its issue should be displaying the in-progress
// label. It is a SUBSET of liveTaskStatuses, and the one status between the two
// sets — pending — is the whole of the distinction: a queued task neither
// earns the in-progress label (examineHandovers) nor makes an existing claim
// true (reportQueued). Both directions read the same two maps, so the
// statement cannot be made one way in one of them and the other way in the
// other; TestLabelReconciler_HandedOverIsASubsetOfLive holds the containment.
var handedOverTaskStatuses = map[orchestration.TaskStatus]bool{
	orchestration.TaskStatusClaimed:    true,
	orchestration.TaskStatusInProgress: true,
}

// restingConclusion reads the conclusion the task's own record names, and
// whether it names one at all.
//
// THE ENGINE PERSISTS THE REASON, SO THE SWEEP READS IT RATHER THAN GUESSING.
// Task.ConclusionReason is written by the same write that concluded the task and
// cleared by a requeue or a readmit, so a non-empty value always describes the
// record it is on (orchestration#796). This function used to re-derive the
// reason from the resting status, and that was wrong in the way the field's own
// documentation warns about: two different endings rest in the SAME shape — an
// engine escalation and a reaped job whose budget is spent are both
// blocked/escalated — so a reader guessing from the status announces the
// escalation's diagnosis over a death nobody diagnosed. The record settles it,
// and a second derivation here would be a second thing to keep in step.
//
// AN EMPTY REASON ANSWERS FALSE and the issue is left alone. It is what a
// requeue leaves, what a task that never concluded has, and what a record
// written before the field existed carries. None of those is an ending, and the
// sweep reports undecidable rather than inventing one.
func restingConclusion(task *orchestration.Task) (orchestration.TaskConclusionReason, bool) {
	if task == nil || task.ConclusionReason == "" {
		return "", false
	}
	return task.ConclusionReason, true
}

// indexTasksByIssue maps each of the project's issues to the task that answers
// for it.
//
// AN ISSUE CAN HAVE TWO TASKS AND ONLY ONE OF THEM ANSWERS. A terminal task
// releases its source reservation (taskSourceReleasable), so re-labelling the
// issue adopts a FRESH task while the spent one stays on the record. The live
// task is the one the issue's label is about; between two terminal ones the
// most recently created is. Anything that is not this repository's forge issue
// is not this sweep's business and is skipped.
func indexTasksByIssue(repo RepoRef, tasks []*orchestration.Task) map[int]*orchestration.Task {
	byIssue := make(map[int]*orchestration.Task, len(tasks))
	for _, task := range tasks {
		if task == nil {
			continue
		}
		taskRepo, number, err := issueOfTask(task)
		if err != nil || taskRepo != repo {
			continue
		}
		held, taken := byIssue[number]
		if !taken || supersedesHolder(held, task) {
			byIssue[number] = task
		}
	}
	return byIssue
}

// supersedesHolder reports whether candidate answers for the issue in place of
// held: a live task always does, and between two of a kind the newer one does.
func supersedesHolder(held, candidate *orchestration.Task) bool {
	heldLive, candidateLive := liveTaskStatuses[held.Status], liveTaskStatuses[candidate.Status]
	if heldLive != candidateLive {
		return candidateLive
	}
	return candidate.CreatedAt > held.CreatedAt
}

// labelsAgree reports whether the issue already displays exactly the module's
// label for key and none of its others.
func labelsAgree(binding *ProjectBinding, issue ForgeIssue, key string) bool {
	owned := binding.Forge.OwnedLabels()
	desired := binding.Forge.Label(key)
	found := false
	for _, label := range issue.Labels {
		if label == desired {
			found = true
			continue
		}
		if owned[strings.ToLower(strings.TrimSpace(label))] {
			return false
		}
	}
	return found == (desired != "")
}

// =============================================================================
// Errors
// =============================================================================

var (
	// ErrLabelReconcileUnswept indicates a sweep that could not establish
	// what it was meant to visit.
	ErrLabelReconcileUnswept = errors.New("sdlc: forge labels were not reconciled")

	// ErrLabelReconcileForge indicates a forge call the sweep depended on.
	ErrLabelReconcileForge = errors.New("sdlc: the forge refused a reconciliation read")

	// ErrLabelReconcileDeferred indicates one issue the sweep left exactly as
	// it found it, to be re-examined on the next pass.
	ErrLabelReconcileDeferred = errors.New("sdlc: issue left for the next reconciliation sweep")

	// ErrLabelReconcileRecord indicates the engine's own record of a task
	// could not be read, so the sweep does not know what it is looking at.
	ErrLabelReconcileRecord = errors.New("sdlc: the engine's task record could not be re-read")
)

// LabelReconcileRecordError reports a re-read of the engine's own record that
// the task store refused, or answered only in part (an incomplete listing).
//
// IT IS DISTINCT FROM LabelReconcileForgeError because the two send an operator
// to opposite halves of the system: one says the forge would not answer, this
// one says the node's own store would not. It arrives on the path that was
// about to declare an issue an orphan, and the declaration is what it stops.
type LabelReconcileRecordError struct {
	TenantID  string
	ProjectID string
	Issue     int
	Cause     error
}

// Error implements the error interface.
func (e *LabelReconcileRecordError) Error() string {
	msg := ErrLabelReconcileRecord.Error() + ": " + e.TenantID + "/" + e.ProjectID +
		"#" + strconv.Itoa(e.Issue) + " was not orphaned on a record nobody could read"
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap returns the sentinel and the cause for errors.Is/As support.
func (e *LabelReconcileRecordError) Unwrap() []error {
	if e.Cause == nil {
		return []error{ErrLabelReconcileRecord}
	}
	return []error{ErrLabelReconcileRecord, e.Cause}
}

// LabelReconcileUnsweptError reports a sweep that never started: the tenants
// could not be enumerated, so no project was visited and none may be assumed
// consistent.
type LabelReconcileUnsweptError struct {
	Cause error
}

// Error implements the error interface.
func (e *LabelReconcileUnsweptError) Error() string {
	msg := ErrLabelReconcileUnswept.Error() + ": the tenants to sweep could not be enumerated"
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap returns the sentinel and the cause for errors.Is/As support.
func (e *LabelReconcileUnsweptError) Unwrap() []error {
	if e.Cause == nil {
		return []error{ErrLabelReconcileUnswept}
	}
	return []error{ErrLabelReconcileUnswept, e.Cause}
}

// LabelReconcileForgeError reports a forge read the sweep could not make. It
// names the operation because "the forge refused" sends an operator reading the
// whole adapter.
type LabelReconcileForgeError struct {
	Repo      string
	Operation string
	Cause     error
}

// Error implements the error interface.
func (e *LabelReconcileForgeError) Error() string {
	msg := ErrLabelReconcileForge.Error() + ": " + e.Operation + " on " + e.Repo
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap returns the sentinel and the cause for errors.Is/As support.
func (e *LabelReconcileForgeError) Unwrap() []error {
	if e.Cause == nil {
		return []error{ErrLabelReconcileForge}
	}
	return []error{ErrLabelReconcileForge, e.Cause}
}

// LabelReconcileDeferredError reports one issue the sweep could not settle.
// Nothing was written; the next sweep tries the same issue again.
type LabelReconcileDeferredError struct {
	Repo   string
	Issue  int
	TaskID string
	Cause  error
}

// Error implements the error interface.
func (e *LabelReconcileDeferredError) Error() string {
	msg := ErrLabelReconcileDeferred.Error() + ": " + e.Repo + "#" + strconv.Itoa(e.Issue)
	if e.TaskID != "" {
		msg += " task=" + e.TaskID
	}
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap returns the sentinel and the cause for errors.Is/As support.
func (e *LabelReconcileDeferredError) Unwrap() []error {
	if e.Cause == nil {
		return []error{ErrLabelReconcileDeferred}
	}
	return []error{ErrLabelReconcileDeferred, e.Cause}
}
