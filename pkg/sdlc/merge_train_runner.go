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
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/cifault"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// The merge train JobRunner — one project's queue, one train at a time
// =============================================================================
//
// A TRAIN IS WORK, SO IT IS A JOB. It needs a container to build in, a lease
// that keeps it to one per project, a stage bound, an audit trail and a record
// that survives a restart — which is what the engine's job envelope already
// provides — so it is a fourth JobRunner beside delivery, review and triage.
//
// ITS STATES ARE THE TRAIN'S PHASES:
//
//	initializing  form a train from the queue, or resume the one in flight
//	executing     build its branch in a container and push it (the only
//	              state that holds a container; it is closed before the wait)
//	validating    wait on CI for one prefix after another until the planner
//	              decides what lands (merge_train.go's observe), then wait on
//	              the project's reviewer for a verdict on that prefix's head
//	completing    eject the culprit, land the verified prefix, requeue the rest
//	cleanup       release everything; start the next train if the queue has one
//
// EVERY DECISION IS ON THE LEDGER BEFORE IT IS ACTED ON, which is what makes a
// restart resume rather than repeat: a train resumed in validating reads its
// probes and verdicts back, and one resumed in completing finishes the landing
// it had already decided.
//
// THE TRAIN IS REVIEWED LIKE ANY OTHER PULL REQUEST. Each change in it was
// approved at its own head, but what lands is their union on the current
// target, and a review is only honest about the head it read. So once CI has
// verified a prefix, the train asks the project's counting reviewer profile
// for a verdict on that prefix's pull request through the ordinary review job,
// and lands only on an APPROVE on exactly that head. The train never writes a
// review or a review status of its own.

// TrainRunnerName is the name a Job selects this runner by.
const TrainRunnerName = "merge_train"

// TrainLeaseSubject is the lease subject every train job of one project
// shares: one train per project at a time.
func TrainLeaseSubject(projectID string) string {
	return TrainRunnerName + ":" + projectID
}

// DefaultTrainPollInterval paces the wait on a train's pipeline.
const DefaultTrainPollInterval = 30 * time.Second

// Labels for a person reading a train job.
const (
	labelTrainForming  = "Forming train"
	labelTrainBuilding = "Building train"
	labelTrainTesting  = "Testing and reviewing train"
	labelTrainLanding  = "Landing train"
	labelTrainCleanup  = "Cleanup"
)

// Audit events the train records.
const (
	auditTrainFormed     = "sdlc:train_formed"
	auditTrainBuilt      = "sdlc:train_built"
	auditTrainProbe      = "sdlc:train_probe"
	auditTrainVerdict    = "sdlc:train_verdict"
	auditTrainLandedPR   = "sdlc:train_landed_train"
	auditTrainEjectedPR  = "sdlc:train_ejected_change"
	auditTrainAbandoned  = "sdlc:train_abandoned"
	auditTrainReview     = "sdlc:train_review_requested"
	auditTrainReviewHeld = "sdlc:train_review_held"
	auditTrainRefused    = "sdlc:train_refused"
	auditTrainReasked    = "sdlc:train_review_reasked"
	auditTrainNoVerdict  = "sdlc:train_review_exhausted"
	auditTrainCleanup    = "sdlc:train_cleanup_failed"
)

// trainEjectedMarker tags the comment an ejected change's pull request gets,
// so a landing resumed after the comment does not post it twice.
const trainEjectedMarker = "quicbaas-sdlc:train-ejected"

// TrainRunnerConfig wires the train runner.
type TrainRunnerConfig struct {
	// Bindings supplies the project's train policy, strategy and reviewer.
	Bindings BindingReader

	// Forges builds the project's forge adapter, whose identity owns the
	// train's branches and pull requests.
	Forges ForgeResolver

	// Jobs starts the review job that judges the train's pull request — the
	// same review runner every other pull request is judged by — and reads
	// whether it has ended.
	Jobs TrainReviewJobs

	// Trains is the queue and the ledger.
	Trains TrainStore

	// Tokens, Signing, Guard, CheckoutRoot and SocketsRoot are the forge
	// access seam the build's container clones, signs and pushes through —
	// the same one every other runner holds.
	Tokens       ForgeTokenReader
	Signing      SigningAgent
	Guard        RepoHostGuard
	CheckoutRoot string
	SocketsRoot  string

	// MergeAttempts bounds the not-ready retries of the landing merge; zero
	// takes DefaultMergeAttempts.
	MergeAttempts int

	// Waiter paces those retries; nil takes exponential backoff.
	Waiter RetryWaiter

	// PollInterval paces the wait on a pipeline; zero takes
	// DefaultTrainPollInterval.
	PollInterval time.Duration

	// Now is the clock; nil takes time.Now in UTC.
	Now func() time.Time
}

// TrainRunner is the merge train's orchestration.JobRunner.
type TrainRunner struct {
	bindings BindingReader
	forges   ForgeResolver
	trains   TrainStore
	jobs     TrainReviewJobs
	access   forgeAccess
	attempts int
	waiter   RetryWaiter
	poll     time.Duration
	now      func() time.Time

	// newBuilder builds the train's git half over a session's container. It
	// is a field so a test can drive the train's decisions without modelling
	// git, and the git half is tested on its own.
	newBuilder func(ctx context.Context, run *trainRun) trainBuilder
}

var (
	_ orchestration.JobRunner      = (*TrainRunner)(nil)
	_ orchestration.JobResumer     = (*TrainRunner)(nil)
	_ orchestration.JobLeasePolicy = (*TrainRunner)(nil)
)

// NewTrainRunner builds the runner; every collaborator is required.
func NewTrainRunner(cfg TrainRunnerConfig) (*TrainRunner, error) {
	switch {
	case cfg.Bindings == nil:
		return nil, &InvalidInputError{Field: "bindings", Message: "the train policy lives on the binding"}
	case cfg.Forges == nil:
		return nil, &InvalidInputError{Field: "forges", Message: "a train is branches, pull requests and checks on the forge"}
	case cfg.Trains == nil:
		return nil, &InvalidInputError{Field: "trains", Message: "a train is formed from the queue and recorded on the ledger"}
	case cfg.Jobs == nil:
		return nil, &InvalidInputError{Field: "jobs", Message: "a train lands only on its reviewer's verdict, and the review runs as a job"}
	case cfg.MergeAttempts < 0:
		return nil, &InvalidInputError{Field: "merge_attempts", Message: "must not be negative"}
	case cfg.PollInterval < 0:
		return nil, &InvalidInputError{Field: "poll_interval", Message: "must not be negative"}
	}
	access, err := newForgeAccess(cfg.Tokens, cfg.Signing, cfg.Guard, cfg.CheckoutRoot, cfg.SocketsRoot)
	if err != nil {
		return nil, err
	}
	runner := &TrainRunner{
		bindings: cfg.Bindings, forges: cfg.Forges, trains: cfg.Trains, jobs: cfg.Jobs, access: access,
		attempts: cfg.MergeAttempts, waiter: cfg.Waiter, poll: cfg.PollInterval, now: cfg.Now,
	}
	if runner.attempts == 0 {
		runner.attempts = DefaultMergeAttempts
	}
	if runner.waiter == nil {
		runner.waiter = backoffWaiter{base: DefaultMergeBackoff}
	}
	if runner.poll == 0 {
		runner.poll = DefaultTrainPollInterval
	}
	if runner.now == nil {
		runner.now = func() time.Time { return time.Now().UTC() }
	}
	runner.newBuilder = func(ctx context.Context, run *trainRun) trainBuilder { return run.containerBuilder(ctx) }
	return runner, nil
}

// Name implements orchestration.JobRunner.
func (*TrainRunner) Name() string { return TrainRunnerName }

// States implements orchestration.JobRunner.
func (*TrainRunner) States() []orchestration.JobStateSpec { return TrainRunnerStates() }

// TrainRunnerStates is the train's sequence, in order.
//
// ONLY BUILDING IS SESSION-BACKED: it is the only state with a container, and
// the container is closed before the pipeline wait.
//
// THE PIPELINE WAIT IS VALIDATING, NOT REVIEWING, and that choice is about the
// stall detector. A state's bounds and session-backing are merged across every
// runner that declares it, and the delivery runner declares reviewing
// session-backed — so a train waiting there with no session would be read as
// a job whose agent is gone and ended once the bound passed, which a bisected
// train, several pipelines long, can reach. No runner declares validating
// session-backed: a train past its bound there is reported and left running.
//
// NO BOUND HERE IS WIDER THAN THE DELIVERY RUNNER'S for the same state, for
// the same reason in the other direction: the merge takes the longest, and a
// train's bound must not loosen the stall detector for every delivery.
//
// A package function as well as a method so the host can declare the states
// before a runtime exists to drive them.
func TrainRunnerStates() []orchestration.JobStateSpec {
	return []orchestration.JobStateSpec{
		{State: orchestration.JobStateInitializing, Label: labelTrainForming, IdleThreshold: 10 * time.Minute},
		{State: orchestration.JobStateExecuting, Label: labelTrainBuilding, SessionBacked: true, IdleThreshold: time.Hour},
		{State: orchestration.JobStateValidating, Label: labelTrainTesting, IdleThreshold: 2 * time.Hour},
		{State: orchestration.JobStateCompleting, Label: labelTrainLanding, IdleThreshold: 10 * time.Minute},
		{State: orchestration.JobStateCleanup, Label: labelTrainCleanup, IdleThreshold: 10 * time.Minute},
	}
}

// trainResumeStates is where an interrupted train job restarts. A build
// restarts at forming, which abandons the half-built train: its container died
// with the node and its branch name is never pushed twice.
var trainResumeStates = map[orchestration.JobState]orchestration.JobState{
	orchestration.JobStateInitializing: orchestration.JobStateInitializing,
	orchestration.JobStateExecuting:    orchestration.JobStateInitializing,
	orchestration.JobStateValidating:   orchestration.JobStateValidating,
	orchestration.JobStateCompleting:   orchestration.JobStateCompleting,
	orchestration.JobStateCleanup:      orchestration.JobStateCleanup,
}

// TrainResumeState is where an interrupted train job restarts.
func TrainResumeState(interrupted orchestration.JobState) orchestration.JobState {
	if resumed, ok := trainResumeStates[interrupted]; ok {
		return resumed
	}
	return orchestration.JobStateInitializing
}

// ResumeState implements orchestration.JobResumer.
func (*TrainRunner) ResumeState(interrupted orchestration.JobState) orchestration.JobState {
	return TrainResumeState(interrupted)
}

// Validate implements orchestration.JobRunner: a train is about a PROJECT.
func (*TrainRunner) Validate(_ context.Context, cfg *orchestration.JobConfig) error {
	return ValidateTrainConfig(cfg)
}

// ValidateTrainConfig refuses a train job that names no project, or a task.
func ValidateTrainConfig(cfg *orchestration.JobConfig) error {
	if cfg == nil {
		return &InvalidInputError{Field: "config", Message: "must not be nil"}
	}
	if strings.TrimSpace(cfg.ProjectID) == "" {
		return &InvalidInputError{Field: "project_id", Message: "a merge train is per project"}
	}
	if strings.TrimSpace(cfg.TaskID) != "" {
		return &InvalidInputError{Field: "task_id", Message: "a merge train carries many tasks' changes and is bound to none"}
	}
	return nil
}

// LeaseSubject implements orchestration.JobLeasePolicy.
func (*TrainRunner) LeaseSubject(cfg *orchestration.JobConfig) string {
	if cfg == nil || strings.TrimSpace(cfg.ProjectID) == "" {
		return ""
	}
	return TrainLeaseSubject(strings.TrimSpace(cfg.ProjectID))
}

// OffProjectBudget implements orchestration.JobLeasePolicy: a train must not
// wait behind the workers whose changes it carries.
func (*TrainRunner) OffProjectBudget() bool { return true }

// Run implements orchestration.JobRunner.
func (r *TrainRunner) Run(ctx context.Context, job *orchestration.JobRun) error {
	if job == nil {
		return &InvalidInputError{Field: "job", Message: "must not be nil"}
	}
	run := &trainRun{runner: r, job: job}
	return run.drive(ctx)
}

// =============================================================================
// One train job
// =============================================================================

// trainRun is what one train job accumulates. It lives for one Run;
// everything durable is on the ledger and the entries.
type trainRun struct {
	runner *TrainRunner
	job    *orchestration.JobRun

	binding *ProjectBinding
	forge   ForgeBackend
	repo    RepoRef
	ledger  *TrainLedger

	sessionID    string
	containerDir string
	spec         orchestration.WorkspaceSpec
	workspace    *containerWorkspace
	teardown     []func()

	// heldReason is the reason this run last held its review for, so a hold
	// that persists across passes is reported once and again only when it
	// changes.
	heldReason string
}

// drive runs the train's states. Map-based dispatch so a state answers in
// constant time and the table reads as the sequence it is.
func (s *trainRun) drive(ctx context.Context) error {
	handlers := map[orchestration.JobState]func(context.Context) (orchestration.JobState, error){
		orchestration.JobStateInitializing: s.form,
		orchestration.JobStateExecuting:    s.build,
		orchestration.JobStateValidating:   s.test,
		orchestration.JobStateCompleting:   s.land,
		orchestration.JobStateCleanup:      s.cleanup,
	}
	defer s.teardownSession(ctx)
	s.job.Decorate(s.decorate)

	state := s.job.StartState
	if state == "" {
		state = orchestration.JobStateInitializing
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		step, ok := handlers[state]
		if !ok {
			return &UnknownRunnerStateError{State: string(state)}
		}
		next, err := step(ctx)
		if err != nil {
			return err
		}
		if next == "" {
			return nil
		}
		if err := s.job.SetState(ctx, next); err != nil {
			return err
		}
		state = next
	}
}

// decorate publishes the build's session while there is one.
func (s *trainRun) decorate(status *orchestration.JobStatus) {
	if status == nil || s.sessionID == "" {
		return
	}
	status.SessionIDs = []string{s.sessionID}
	status.LeadSessionID = s.sessionID
}

// resolve loads the binding, the project's forge and the ledger.
func (s *trainRun) resolve(ctx context.Context) error {
	if s.binding != nil {
		return nil
	}
	projectID := strings.TrimSpace(s.job.Config.ProjectID)
	binding, err := s.runner.bindings.GetBinding(ctx, s.job.TenantID, projectID)
	if err != nil {
		return err
	}
	forge, err := s.runner.forges.ForgeFor(ctx, s.job.TenantID, binding.Forge)
	if err != nil {
		return err
	}
	if forge == nil {
		return NewForgeNotRegisteredError(binding.Forge.Type)
	}
	ledger, err := s.runner.trains.GetTrainLedger(ctx, s.job.TenantID, projectID)
	if err != nil {
		return err
	}
	s.binding, s.forge, s.repo, s.ledger = binding, forge, binding.Repo(), ledger
	return nil
}

// saveLedger writes the ledger under its version guard.
func (s *trainRun) saveLedger(ctx context.Context) error {
	return s.runner.trains.SaveTrainLedger(ctx, s.job.TenantID, s.ledger)
}

// train is the train in flight.
func (s *trainRun) train() *MergeTrain { return s.ledger.Active }

// =============================================================================
// initializing — form a train, or resume the one in flight
// =============================================================================

// form resumes the train in flight at its phase, or forms the next one from
// the queue. An empty queue ends the job.
func (s *trainRun) form(ctx context.Context) (orchestration.JobState, error) {
	// Every train reads the binding as it is now: a job that runs train after
	// train must not form the fifth under the policy the first was formed by.
	s.binding = nil
	if err := s.resolve(ctx); err != nil {
		return "", err
	}
	if t := s.train(); t != nil {
		switch t.Phase {
		case TrainTesting, TrainReviewing:
			return orchestration.JobStateValidating, nil
		case TrainLanding:
			return orchestration.JobStateCompleting, nil
		default:
			// A BUILD THAT DIED WITH ITS CONTAINER is not resumed: nothing of
			// it is recorded until it is pushed, and its number is never
			// pushed twice. Its changes go back to the queue and a new train
			// is formed from them below.
			if err := s.abandon(ctx, "the train's build was interrupted"); err != nil {
				return "", err
			}
		}
	}
	entries, err := s.runner.trains.ListTrainEntries(ctx, s.job.TenantID, s.binding.ProjectID)
	if err != nil {
		return "", err
	}
	if err := s.recoverStrays(ctx, entries); err != nil {
		return "", err
	}
	if reason := s.binding.TrainOffReason(); reason != "" {
		s.job.Audit(auditTrainOff, reason)
		s.job.Emit(orchestration.JobStateCleanup, "no merge train is formed: "+reason)
		return "", nil
	}
	cars := s.nextCars(entries)
	if len(cars) == 0 {
		s.job.Emit(orchestration.JobStateCleanup, "the merge queue is empty")
		return "", nil
	}
	number := s.ledger.Next
	t := &MergeTrain{Number: number, Target: cars[0].target, Phase: TrainBuilding, StartedAt: s.runner.now()}
	for _, candidate := range cars {
		boarded, err := s.board(ctx, candidate.entry, number)
		if err != nil {
			return "", err
		}
		if boarded {
			t.Cars = append(t.Cars, carOf(candidate.entry))
		}
	}
	if len(t.Cars) == 0 {
		return orchestration.JobStateCleanup, nil
	}
	s.ledger.Next = number + 1
	s.ledger.Active = t
	if err := s.saveLedger(ctx); err != nil {
		return "", err
	}
	s.job.Audit(auditTrainFormed, "train="+strconv.Itoa(number)+" target="+t.Target+" cars="+carList(t.Cars))
	s.job.Emit(orchestration.JobStateExecuting,
		"merge train #"+strconv.Itoa(number)+" formed with "+strconv.Itoa(len(t.Cars))+" change(s) onto "+t.Target)
	return orchestration.JobStateExecuting, nil
}

// queued is one entry offered to a train, with the target it lands on.
type queued struct {
	entry  *TrainEntry
	target string
}

// nextCars picks the next train's changes: the queue in order, for the target
// of the change at its head, up to the policy's size.
func (s *trainRun) nextCars(entries []*TrainEntry) []queued {
	waiting := make([]*TrainEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.State == TrainEntryQueued {
			waiting = append(waiting, entry)
		}
	}
	orderQueue(waiting)
	if len(waiting) == 0 {
		return nil
	}
	target := waiting[0].Target
	size := s.binding.MergeTrain.Size()
	picked := make([]queued, 0, size)
	for _, entry := range waiting {
		if entry.Target != target {
			continue
		}
		// A CHANGE A REFUSED TRAIN SENT BACK BOUNDS THE TRAIN IT RIDES, and
		// one that would not fit in the train already picked waits for the
		// next, which it heads.
		if entry.MaxTrain > 0 && entry.MaxTrain < size {
			size = entry.MaxTrain
		}
		if len(picked) >= size {
			break
		}
		picked = append(picked, queued{entry: entry, target: target})
		if len(picked) == size {
			break
		}
	}
	return picked
}

// board marks one queued entry as riding train number, and reports whether it
// boarded. An entry the delivery replaced since it was listed stays where the
// delivery put it.
func (s *trainRun) board(ctx context.Context, entry *TrainEntry, number int) (bool, error) {
	entry.State = TrainEntryRiding
	entry.Train = number
	err := s.runner.trains.SaveTrainEntry(ctx, s.job.TenantID, entry)
	if errors.Is(err, ErrVersionConflict) {
		return false, nil
	}
	return err == nil, err
}

// recoverStrays returns to the queue every entry recorded as riding a train
// that is not in flight: a formation whose ledger write lost, or a train whose
// ending was written to the ledger before its entries were.
func (s *trainRun) recoverStrays(ctx context.Context, entries []*TrainEntry) error {
	active := 0
	if t := s.train(); t != nil {
		active = t.Number
	}
	for _, entry := range entries {
		if entry.State != TrainEntryRiding || entry.Train == active {
			continue
		}
		entry.State = TrainEntryQueued
		entry.Train = 0
		if err := s.runner.trains.SaveTrainEntry(ctx, s.job.TenantID, entry); err != nil && !errors.Is(err, ErrVersionConflict) {
			return err
		}
	}
	return nil
}

// carOf is the car an entry rides as.
func carOf(entry *TrainEntry) TrainCar {
	return TrainCar{
		TaskID: entry.TaskID, PullRequest: entry.PullRequest, Title: entry.Title, Branch: entry.Branch,
		HeadSHA: entry.HeadSHA, Closes: append([]int(nil), entry.Closes...),
	}
}

// carList renders cars for an audit line as "#12@abc1234,#15@def5678".
func carList(cars []TrainCar) string {
	parts := make([]string, 0, len(cars))
	for _, car := range cars {
		parts = append(parts, "#"+strconv.Itoa(car.PullRequest)+"@"+shortSHA(car.HeadSHA))
	}
	return strings.Join(parts, ",")
}

// =============================================================================
// executing — build the train's branch
// =============================================================================

// build merges every car onto the target in a container, pushes the result
// and closes the container.
//
// EACH CAR IS CHECKED ON THE FORGE BEFORE IT IS MERGED. A pull request closed
// or moved since it was queued is ejected here, before it can cost the train a
// pipeline; the builder checks the fetched head again because the forge and
// the fetch are two reads.
func (s *trainRun) build(ctx context.Context) (orchestration.JobState, error) {
	if err := s.resolve(ctx); err != nil {
		return "", err
	}
	t := s.train()
	if t == nil || t.Phase != TrainBuilding {
		return orchestration.JobStateInitializing, nil
	}
	if err := s.spawnSession(ctx, t); err != nil {
		return "", err
	}
	builder := s.runner.newBuilder(ctx, s)
	base, err := builder.Base()
	if err != nil {
		return "", s.buildFailed(ctx, t, builder, "the checkout's head could not be read", err)
	}
	t.BaseSHA = base
	kept := make([]TrainCar, 0, len(t.Cars))
	for _, car := range t.Cars {
		ejection, err := s.checkCar(ctx, car)
		if err != nil {
			return "", s.buildFailed(ctx, t, builder, "pull request #"+strconv.Itoa(car.PullRequest)+" could not be read", err)
		}
		if ejection == nil {
			var commit string
			commit, ejection, err = builder.Add(car)
			if err != nil {
				return "", s.buildFailed(ctx, t, builder, "pull request #"+strconv.Itoa(car.PullRequest)+" could not be merged", err)
			}
			car.Commit = commit
		}
		if ejection != nil {
			ejection.Train = t.Number
			if err := s.eject(ctx, t, car, *ejection); err != nil {
				return "", err
			}
			continue
		}
		kept = append(kept, car)
	}
	t.Cars = kept
	if len(t.Cars) == 0 {
		s.job.Audit(auditTrainBuild, builder.Evidence())
		s.teardownSession(ctx)
		if err := s.finish(ctx, "every change was ejected while the train was built"); err != nil {
			return "", err
		}
		return orchestration.JobStateCleanup, nil
	}
	head, err := builder.Push(TrainBranch(t.Number))
	s.job.Audit(auditTrainBuild, builder.Evidence())
	if err != nil {
		return "", s.buildFailed(ctx, t, nil, "the train could not be pushed", err)
	}
	if last := t.Cars[len(t.Cars)-1].Commit; !strings.EqualFold(strings.TrimSpace(head), last) {
		return "", s.buildFailed(ctx, t, nil, "the pushed head "+shortSHA(head)+" is not the train's "+shortSHA(last), nil)
	}
	s.teardownSession(ctx)
	t.Phase = TrainTesting
	t.startProbe(len(t.Cars))
	if err := s.saveLedger(ctx); err != nil {
		return "", err
	}
	s.job.Audit(auditTrainBuilt, "train="+strconv.Itoa(t.Number)+" base="+t.BaseSHA+" head="+head+" cars="+carList(t.Cars))
	s.job.Emit(orchestration.JobStateValidating,
		"merge train #"+strconv.Itoa(t.Number)+" pushed as "+TrainBranch(t.Number)+" at "+shortSHA(head))
	return orchestration.JobStateValidating, nil
}

// buildFailed abandons a train whose build failed for a reason that is no one
// change's, and answers the typed failure. Its changes wait for the next train.
func (s *trainRun) buildFailed(ctx context.Context, t *MergeTrain, builder trainBuilder, reason string, cause error) error {
	if builder != nil {
		s.job.Audit(auditTrainBuild, builder.Evidence())
	}
	s.teardownSession(ctx)
	if err := s.abandon(ctx, reason); err != nil {
		return err
	}
	return &TrainBuildError{ProjectID: s.binding.ProjectID, Train: t.Number, Reason: reason, Cause: cause}
}

// checkCar reads a car's pull request and answers an ejection when it is no
// longer the change that was queued.
func (s *trainRun) checkCar(ctx context.Context, car TrainCar) (*TrainEjection, error) {
	pr, err := s.forge.GetPullRequest(ctx, s.repo, car.PullRequest)
	if err != nil {
		return nil, err
	}
	if pr == nil {
		return nil, &NotFoundError{Entity: "pull request", ID: s.repo.String() + "#" + strconv.Itoa(car.PullRequest)}
	}
	if pullRequestStateFor(pr) != PullRequestOpen {
		return &TrainEjection{Reason: TrainEjectClosed, Detail: "#" + strconv.Itoa(car.PullRequest) + " is " + pullRequestStateFor(pr).String() + "."}, nil
	}
	if !strings.EqualFold(pr.HeadSHA, car.HeadSHA) {
		return &TrainEjection{
			Reason: TrainEjectHeadMoved,
			Detail: "#" + strconv.Itoa(car.PullRequest) + " is at " + shortSHA(pr.HeadSHA) + ", not the approved " + shortSHA(car.HeadSHA) + ".",
		}, nil
	}
	return nil, nil
}

// spawnSession starts the build's container with a checkout of the target on
// the train's branch.
//
// THE DELIVERY SCOPE, because the train commits: each merge is a commit, signed
// by the project's key exactly as a worker's is, and a protected target that
// requires signed commits refuses anything else. The session is never
// prompted; it is a container to run git in.
func (s *trainRun) spawnSession(ctx context.Context, t *MergeTrain) error {
	project := s.job.Project
	if project == nil {
		return &ProjectRecordMissingError{ProjectID: s.binding.ProjectID}
	}
	// The worker's Agent supplies the runtime and the image the train's git
	// runs in; the session is never prompted, so its model is never spent.
	req := &orchestration.SpawnSessionRequest{
		AgentID:    s.binding.Worker.AgentID,
		PoolID:     s.binding.Worker.PoolID,
		Role:       orchestration.AgentRoleCoding,
		ProjectID:  project.ID,
		Persistent: true,
	}
	workspace, err := s.runner.access.Open(ctx, req, workspacePlan{
		TenantID: s.job.TenantID,
		Project:  project,
		Forge:    s.binding.Forge,
		Branch:   orchestration.WorkspaceBranchPlan{Name: TrainBranch(t.Number), Base: t.Target, Create: true},
		Key:      "train-" + project.ID + "-" + strconv.Itoa(t.Number),
		Scope:    SigningScopeDelivery,
	})
	if err != nil {
		return err
	}
	stopCtx := context.WithoutCancel(ctx)
	s.teardown = append(s.teardown, func() { workspace.Release(stopCtx) })
	s.workspace = workspace
	s.spec = workspace.Spec
	resp, err := s.job.Spawn(ctx, req)
	if err != nil {
		return workspaceSpawnFault(project.ID, err)
	}
	if resp == nil || resp.SessionID == "" {
		return &WorkerSessionError{TaskID: TrainLeaseSubject(project.ID), Message: "the spawn returned no session"}
	}
	s.sessionID = resp.SessionID
	if strings.TrimSpace(resp.ContainerID) == "" {
		return &WorkerNotContainerizedError{TaskID: TrainLeaseSubject(project.ID), SessionID: resp.SessionID}
	}
	if resp.Workspace == nil {
		return &WorkerSessionError{TaskID: TrainLeaseSubject(project.ID), Message: "the spawn reported no workspace for the checkout"}
	}
	s.containerDir = path.Join(s.spec.CheckoutRoot, project.Name)
	s.job.SetWorkDir(filepath.Join(resp.Workspace.Dir, project.Name))
	if err := s.job.SetContainer(s.sessionID, s.containerDir); err != nil {
		return err
	}
	checkout := containerCheckout{
		job: s.job, sessionID: s.sessionID, spec: s.spec, project: project, taskID: TrainLeaseSubject(project.ID),
	}
	if err := checkout.Make(ctx); err != nil {
		return err
	}
	return checkout.Verify(ctx)
}

// containerBuilder is the build's git half over the session's container.
func (s *trainRun) containerBuilder(ctx context.Context) trainBuilder {
	exec := sessionExec(ctx, s.job, s.sessionID)
	taskID := TrainLeaseSubject(s.binding.ProjectID)
	return &containerTrainBuilder{
		gitSteps: gitSteps{exec: exec, taskID: taskID, sessionID: s.sessionID},
		publisher: &publisher{
			exec: exec, project: s.job.Project, signing: s.workspace.signingSession(), taskID: taskID,
			admit: func() error { return s.stillActive(ctx) },
		},
		project: s.job.Project,
		signing: s.workspace.signingSession(),
		workDir: s.containerDir,
		target:  s.train().Target,
	}
}

// stillActive refuses a push once the ledger no longer names this train: a
// second job that took the project over has abandoned it.
func (s *trainRun) stillActive(ctx context.Context) error {
	held, err := s.runner.trains.GetTrainLedger(ctx, s.job.TenantID, s.binding.ProjectID)
	if err != nil {
		return err
	}
	if held.Active == nil || held.Active.Number != s.train().Number {
		return &VersionConflictError{Entity: trainLedgerEntityName, TenantID: s.job.TenantID, ID: s.binding.ProjectID, Expected: s.ledger.Version}
	}
	return nil
}

// teardownSession closes the build's session and releases its workspace. It
// is idempotent.
func (s *trainRun) teardownSession(ctx context.Context) {
	if s.sessionID != "" {
		s.job.Close(context.WithoutCancel(ctx), s.sessionID)
		s.sessionID = ""
	}
	for i := len(s.teardown) - 1; i >= 0; i-- {
		s.teardown[i]()
	}
	s.teardown = nil
}

// =============================================================================
// validating — test prefixes until the planner decides
// =============================================================================

// test waits on the current probe's pipeline, feeds each settled verdict to
// the planner, and opens the next probe it asks for, until it decides what
// lands.
func (s *trainRun) test(ctx context.Context) (orchestration.JobState, error) {
	if err := s.resolve(ctx); err != nil {
		return "", err
	}
	t := s.train()
	if t == nil {
		return orchestration.JobStateInitializing, nil
	}
	switch t.Phase {
	case TrainTesting:
	case TrainReviewing:
		return s.review(ctx, t)
	default:
		return orchestration.JobStateInitializing, nil
	}
	for {
		probe := t.Current()
		if err := s.openProbe(ctx, t, probe); err != nil {
			return "", err
		}
		verdict, err := s.readProbe(ctx, t, probe)
		if err != nil {
			return "", err
		}
		if verdict.pending {
			timer := time.NewTimer(s.runner.poll)
			select {
			case <-ctx.Done():
				timer.Stop()
				return "", ctx.Err()
			case <-timer.C:
			}
			continue
		}
		action := t.observe(verdict.green)
		if !verdict.green && t.Bad == probe.Length {
			t.BadFindings = verdict.findings
		}
		s.job.Audit(auditTrainVerdict, "train="+strconv.Itoa(t.Number)+" prefix="+strconv.Itoa(probe.Length)+
			" head="+probe.HeadSHA+" verdict="+probe.Verdict+" good="+strconv.Itoa(t.Good)+" bad="+strconv.Itoa(t.Bad))
		if action.kind == trainProbe {
			t.startProbe(action.probe)
			if err := s.saveLedger(ctx); err != nil {
				return "", err
			}
			s.job.Emit(orchestration.JobStateValidating, "merge train #"+strconv.Itoa(t.Number)+" is red at "+
				strconv.Itoa(t.Bad)+" change(s); testing the first "+strconv.Itoa(action.probe))
			continue
		}
		t.Land, t.Eject = action.land, action.eject
		if t.Land == 0 {
			// NOTHING WAS VERIFIED, SO NOTHING IS REVIEWED: the first change
			// is the culprit, and the landing only ejects it.
			t.Phase = TrainLanding
			if err := s.saveLedger(ctx); err != nil {
				return "", err
			}
			s.job.Emit(orchestration.JobStateCompleting, "merge train #"+strconv.Itoa(t.Number)+": nothing verified"+ejectSuffix(t))
			return orchestration.JobStateCompleting, nil
		}
		t.Phase = TrainReviewing
		if err := s.saveLedger(ctx); err != nil {
			return "", err
		}
		s.job.Emit(orchestration.JobStateValidating, "merge train #"+strconv.Itoa(t.Number)+": "+strconv.Itoa(t.Land)+
			" change(s) verified green"+ejectSuffix(t)+"; waiting on the project's reviewer")
		return s.review(ctx, t)
	}
}

// ejectSuffix renders ", ejecting #n" when the landing ejects a change.
func ejectSuffix(t *MergeTrain) string {
	if t.Eject == 0 {
		return ""
	}
	return ", ejecting #" + strconv.Itoa(t.Cars[t.Eject-1].PullRequest)
}

// openProbe makes the probe's branch and pull request, once. Both are recorded
// before anything waits on them, so a resumed train finds them rather than
// opening a second pull request for the same prefix.
func (s *trainRun) openProbe(ctx context.Context, t *MergeTrain, probe *TrainProbe) error {
	if probe.PullRequest > 0 {
		return nil
	}
	if probe.Branch != TrainBranch(t.Number) {
		if err := s.ensureBranch(ctx, probe.Branch, probe.HeadSHA); err != nil {
			return err
		}
	}
	pr, err := s.forge.CreatePullRequest(ctx, s.repo, PullRequestSpec{
		Title:   trainTitle(t, probe.Length),
		Body:    trainBody(t, probe.Length),
		HeadRef: probe.Branch,
		BaseRef: t.Target,
	})
	if err != nil {
		return err
	}
	if pr == nil {
		return &ForgeWriteNotAppliedError{Type: s.forge.Type(), Operation: "CreatePullRequest", Subject: s.repo.String() + " " + probe.Branch}
	}
	if !strings.EqualFold(pr.HeadSHA, probe.HeadSHA) {
		return &HeadMovedError{Subject: s.repo.String() + "#" + strconv.Itoa(pr.Number), Reviewed: probe.HeadSHA, Current: pr.HeadSHA}
	}
	probe.PullRequest = pr.Number
	if err := s.saveLedger(ctx); err != nil {
		return err
	}
	s.job.Audit(auditTrainProbe, "train="+strconv.Itoa(t.Number)+" prefix="+strconv.Itoa(probe.Length)+
		" branch="+probe.Branch+" pull_request="+strconv.Itoa(pr.Number)+" head="+probe.HeadSHA)
	return nil
}

// ensureBranch cuts a probe's branch at its commit, or accepts the one an
// interrupted run already cut there.
func (s *trainRun) ensureBranch(ctx context.Context, name, sha string) error {
	held, err := s.forge.GetBranch(ctx, s.repo, name)
	switch {
	case err == nil && held != nil && strings.EqualFold(held.SHA, sha):
		return nil
	case err == nil && held != nil:
		return &HeadMovedError{Subject: s.repo.String() + " " + name, Reviewed: sha, Current: held.SHA}
	case err != nil && !errors.Is(err, ErrNotFound):
		return err
	}
	_, err = s.forge.CreateBranch(ctx, s.repo, name, sha)
	return err
}

// probeVerdict is what one read of a probe's pipeline decided.
type probeVerdict struct {
	pending  bool
	green    bool
	findings string
}

// readProbe reads the probe's pipeline once and answers its verdict, or
// pending while only time can change it.
//
// THE GATE'S OWN DEFINITIONS, NOT A SECOND COPY. Green is ChecksGreenForReview
// — the same check half the merge gate and review dispatch read — over the
// runs with every reviewer's own status set aside. A red is read only once it
// has settled, and then triaged as the gate triages one: a void red is re-run
// (bounded), a host defect stops the train for an operator, and only a
// genuine red is the change's.
func (s *trainRun) readProbe(ctx context.Context, t *MergeTrain, probe *TrainProbe) (probeVerdict, error) {
	runs, err := s.forge.ListCheckRuns(ctx, s.repo, probe.HeadSHA)
	if err != nil {
		return probeVerdict{}, err
	}
	runs = runsExcludingContexts(runs, s.binding.ReviewerStatusContexts())
	now := s.runner.now()
	reason, _ := ChecksGreenForReview(s.binding.Review, probe.HeadSHA, runs, now)
	if reason.Clear() {
		return probeVerdict{green: true}, nil
	}
	if reason != GateBlockedChecksFailed {
		return probeVerdict{pending: true}, nil
	}
	failure, settled := evaluateChecksFailure(checkRecordsFor(runs, probe.HeadSHA, now), probe.HeadSHA, now, s.binding.Review.SettleWindow())
	if !settled || (!probe.RerunAt.IsZero() && !failure.SettledAt.After(probe.RerunAt)) {
		return probeVerdict{pending: true}, nil
	}
	logs := checksFailureLogs(ctx, s.forge, s.repo, failure, runs, DefaultChecksClassifyBytes)
	triage := TriageChecksFailure(failure, logs, []string{s.repo.Owner})
	switch triage.Aggregate() {
	case cifault.VerdictGenuine:
		suspicion, err := s.environmentalSuspicion(ctx, t, probe, failure, runs)
		if err != nil {
			return probeVerdict{}, err
		}
		if suspicion != "" {
			if verdict, handled, err := s.retryEnvironmental(ctx, t, probe, suspicion, failure, runs); handled || err != nil {
				return verdict, err
			}
		}
		return probeVerdict{findings: failure.Findings(triage, logs, DefaultChecksLogBytes)}, nil
	case cifault.VerdictHostDefect:
		return probeVerdict{}, s.hostDefect(ctx, t, probe, triage.Account(probe.HeadSHA))
	default:
		return s.rerunVoid(ctx, t, probe, triage, runs)
	}
}

// rerunVoid re-runs a probe's void reds, bounded, and answers pending so the
// wait continues. A spent budget, or a forge that cannot re-run, is a host
// defect: waiting would be waiting for something nobody will trigger.
func (s *trainRun) rerunVoid(ctx context.Context, t *MergeTrain, probe *TrainProbe, triage ChecksTriage, runs []ForgeCheckRun) (probeVerdict, error) {
	void := triage.Of(cifault.VerdictVoid)
	if !s.forge.Capabilities().RerunCheckRuns {
		return probeVerdict{}, s.hostDefect(ctx, t, probe, rerunUnsupported(void, probe.HeadSHA,
			string(s.forge.Type())+" cannot re-run a finished check"))
	}
	if probe.Reruns >= DefaultChecksRerunBudget {
		return probeVerdict{}, s.hostDefect(ctx, t, probe, rerunBudgetSpent(void, probe.HeadSHA, probe.Reruns))
	}
	byName := make(map[string]ForgeCheckRun, len(runs))
	for _, run := range runs {
		byName[run.Name] = run
	}
	for _, entry := range void {
		if err := s.forge.RerunCheckRun(ctx, s.repo, byName[entry.Check.Name]); err != nil {
			return probeVerdict{}, s.hostDefect(ctx, t, probe, "the forge refused to re-run "+entry.Check.Name+": "+err.Error())
		}
	}
	probe.Reruns++
	probe.RerunAt = s.runner.now()
	if err := s.saveLedger(ctx); err != nil {
		return probeVerdict{}, err
	}
	return probeVerdict{pending: true}, nil
}

// hostDefect stops a train whose pipeline cannot answer: its changes go back
// to the queue, and the job fails with the defect for an operator. No change
// is ejected — nothing any worker could do would turn that red green.
func (s *trainRun) hostDefect(ctx context.Context, t *MergeTrain, probe *TrainProbe, account string) error {
	if err := s.abandon(ctx, "the pipeline failed on a host defect"); err != nil {
		return err
	}
	return &CIHostDefectError{HeadSHA: probe.HeadSHA, Reason: "merge train #" + strconv.Itoa(t.Number) + ": " + account}
}

// =============================================================================
// completing — eject, land, requeue
// =============================================================================

// land carries out the planner's decision: the culprit is ejected, the
// verified prefix lands, and every car after the culprit goes back to the
// queue.
//
// IDEMPOTENT, because it is where an interrupted landing resumes: an ejection
// already written is not written twice, a pull request already merged is
// recorded rather than merged again, and an entry already landed stays landed.
func (s *trainRun) land(ctx context.Context) (orchestration.JobState, error) {
	if err := s.resolve(ctx); err != nil {
		return "", err
	}
	t := s.train()
	if t == nil || t.Phase != TrainLanding {
		return orchestration.JobStateInitializing, nil
	}
	if t.Eject > 0 {
		if err := s.ejectCulprit(ctx, t); err != nil {
			return "", err
		}
	}
	if t.Land > 0 {
		landed, err := s.landPrefix(ctx, t)
		if err != nil {
			return "", err
		}
		if !landed {
			// The prefix could not land as tested: the target moved or a
			// change in it lost its approval. abandon has requeued what is
			// left; the next train is formed from the target as it is now.
			return orchestration.JobStateCleanup, nil
		}
	}
	for _, car := range t.Cars[max(t.Land, t.Eject):] {
		if err := s.requeue(ctx, t, car); err != nil {
			return "", err
		}
	}
	if err := s.finish(ctx, "landed "+strconv.Itoa(t.Land)+" change(s)"); err != nil {
		return "", err
	}
	return orchestration.JobStateCleanup, nil
}

// landPrefix lands the verified prefix through its probe's pull request and
// records every change in it as landed. It answers false when the prefix can
// no longer land as it was tested, having abandoned the train.
func (s *trainRun) landPrefix(ctx context.Context, t *MergeTrain) (bool, error) {
	probe := t.ProbeOf(t.Land)
	if probe == nil || probe.PullRequest == 0 {
		return false, &TrainLandingError{ProjectID: s.binding.ProjectID, Train: t.Number, Reason: "the verified prefix has no pull request"}
	}
	pr, err := s.forge.GetPullRequest(ctx, s.repo, probe.PullRequest)
	if err != nil {
		return false, err
	}
	if pr == nil {
		return false, &NotFoundError{Entity: "pull request", ID: s.repo.String() + "#" + strconv.Itoa(probe.PullRequest)}
	}
	if !pr.Merged {
		ready, err := s.readyToLand(ctx, t)
		if err != nil || !ready {
			return false, err
		}
		if err := s.mergeTrain(ctx, t, probe); err != nil {
			return false, err
		}
		if pr, err = s.forge.GetPullRequest(ctx, s.repo, probe.PullRequest); err != nil {
			return false, err
		}
	}
	if pr == nil || !pr.Merged || strings.TrimSpace(pr.MergeSHA) == "" {
		return false, &TrainLandingError{
			ProjectID: s.binding.ProjectID, Train: t.Number, PullRequest: probe.PullRequest,
			Reason: "the forge reports the train's pull request unmerged after the merge",
		}
	}
	for _, car := range t.Cars[:t.Land] {
		if err := s.markLanded(ctx, t, car, pr.MergeSHA, probe.PullRequest); err != nil {
			return false, err
		}
	}
	s.job.Audit(auditTrainLandedPR, "train="+strconv.Itoa(t.Number)+" pull_request="+strconv.Itoa(probe.PullRequest)+
		" merge="+pr.MergeSHA+" cars="+carList(t.Cars[:t.Land]))
	return true, nil
}

// readyToLand re-reads what the landing rests on: the target is still the
// commit the train was built on and reviewed against, and every change in the
// prefix is still open, on its approved head, and still approved there.
//
// A TARGET THAT MOVED IS NOT A CHANGE'S FAULT: nothing is ejected and the
// train is abandoned, so the next one is built on the target as it now is. A
// change that moved or lost its approval IS ejected — its prefix carried code
// nobody has approved — and the rest wait for the next train.
func (s *trainRun) readyToLand(ctx context.Context, t *MergeTrain) (bool, error) {
	target, err := s.forge.GetBranch(ctx, s.repo, t.Target)
	if err != nil {
		return false, err
	}
	if target == nil || !strings.EqualFold(target.SHA, t.BaseSHA) {
		current := ""
		if target != nil {
			current = target.SHA
		}
		return false, s.abandon(ctx, "`"+t.Target+"` moved from "+shortSHA(t.BaseSHA)+" to "+shortSHA(current)+" while the train was tested")
	}
	for _, car := range t.Cars[:t.Land] {
		ejection, err := s.checkCar(ctx, car)
		if err != nil {
			return false, err
		}
		if ejection == nil {
			ejection, err = s.checkApproval(ctx, car)
			if err != nil {
				return false, err
			}
		}
		if ejection != nil {
			ejection.Train = t.Number
			if err := s.eject(ctx, t, car, *ejection); err != nil {
				return false, err
			}
			return false, s.abandon(ctx, "#"+strconv.Itoa(car.PullRequest)+" can no longer land as it was tested")
		}
	}
	return true, nil
}

// checkApproval answers an ejection when a change's approved head no longer
// carries the reviewer's success status: the approval the change was queued on
// is gone, and the train's reviewer was told it stood.
func (s *trainRun) checkApproval(ctx context.Context, car TrainCar) (*TrainEjection, error) {
	runs, err := s.forge.ListCheckRuns(ctx, s.repo, car.HeadSHA)
	if err != nil {
		return nil, err
	}
	wanted := strings.ToLower(s.binding.Reviewer.Context())
	var latest *ForgeCheckRun
	for i := range runs {
		if strings.ToLower(strings.TrimSpace(runs[i].Name)) != wanted {
			continue
		}
		if latest == nil || checkSupersedes(runs[i], *latest) {
			latest = &runs[i]
		}
	}
	if latest != nil && checkStatusFor(*latest) == CheckSuccess {
		return nil, nil
	}
	return &TrainEjection{
		Reason: TrainEjectUnreviewed,
		Detail: "`" + s.binding.Reviewer.Context() + "` is not success on " + shortSHA(car.HeadSHA) + ".",
	}, nil
}

// mergeTrain merges the train's pull request by the project's own strategy,
// retrying only while the forge reports itself not ready.
func (s *trainRun) mergeTrain(ctx context.Context, t *MergeTrain, probe *TrainProbe) error {
	strategy, err := LookupStrategy(s.binding.Strategy)
	if err != nil {
		return err
	}
	spec, err := strategy.PlanMerge(PullRequestRef{
		Number: probe.PullRequest, Title: trainTitle(t, t.Land), SourceBranch: probe.Branch,
	})
	if err != nil {
		return err
	}
	spec.CommitMessage = trainCommitMessage(t.Cars[:t.Land])
	spec.ExpectSourceDeleted = true
	var last error
	for attempt := 1; attempt <= s.runner.attempts; attempt++ {
		err := s.forge.MergePullRequest(ctx, s.repo, probe.PullRequest, spec, probe.HeadSHA)
		if err == nil {
			return nil
		}
		if !errors.Is(err, ErrForgeNotReady) {
			return &TrainLandingError{ProjectID: s.binding.ProjectID, Train: t.Number, PullRequest: probe.PullRequest,
				Reason: "the forge refused the merge", Cause: err}
		}
		last = err
		if attempt == s.runner.attempts {
			break
		}
		if waitErr := s.runner.waiter.Wait(ctx, attempt); waitErr != nil {
			return waitErr
		}
	}
	return &TrainLandingError{ProjectID: s.binding.ProjectID, Train: t.Number, PullRequest: probe.PullRequest,
		Reason: "the forge stayed not ready", Cause: last}
}

// =============================================================================
// Entries
// =============================================================================

// updateEntry applies mutate to a car's entry, but only while the entry still
// records THIS train RIDING with THIS head: a delivery that re-queued a new head
// owns the entry now, and the train must not overwrite it with a verdict about
// a head that is no longer offered. Every train transition starts from riding,
// so an entry already landed, ejected or requeued is left as it is — which is
// what makes a resumed landing safe to run again.
func (s *trainRun) updateEntry(ctx context.Context, t *MergeTrain, car TrainCar, mutate func(*TrainEntry)) error {
	for attempt := 1; ; attempt++ {
		entry, err := s.runner.trains.GetTrainEntry(ctx, s.job.TenantID, s.binding.ProjectID, car.TaskID)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				return nil
			}
			return err
		}
		if entry.HeadSHA != car.HeadSHA || entry.Train != t.Number || entry.State != TrainEntryRiding {
			return nil
		}
		mutate(entry)
		err = s.runner.trains.SaveTrainEntry(ctx, s.job.TenantID, entry)
		if err == nil || !errors.Is(err, ErrVersionConflict) || attempt >= trainEntryWriteAttempts {
			return err
		}
	}
}

// markLanded records one car as landed.
func (s *trainRun) markLanded(ctx context.Context, t *MergeTrain, car TrainCar, mergeSHA string, trainPR int) error {
	return s.updateEntry(ctx, t, car, func(entry *TrainEntry) {
		entry.State = TrainEntryLanded
		entry.MergeSHA = mergeSHA
		entry.TrainPullRequest = trainPR
		entry.Ejection = nil
	})
}

// requeue puts one car back in the queue, keeping its place: it was never
// judged on its own.
func (s *trainRun) requeue(ctx context.Context, t *MergeTrain, car TrainCar) error {
	return s.updateEntry(ctx, t, car, func(entry *TrainEntry) {
		entry.State = TrainEntryQueued
		entry.Train = 0
	})
}

// eject puts one change out of the train: a comment and a label on its pull
// request for the person reading it, then the entry its delivery waits on.
//
// THE ENTRY IS WRITTEN LAST, because it is what sends the change back to its
// worker; a comment that did not land must be retried by a resumed landing,
// which only happens while the entry still says riding.
func (s *trainRun) eject(ctx context.Context, t *MergeTrain, car TrainCar, ejection TrainEjection) error {
	marker := trainEjectedMarker + " train=" + strconv.Itoa(t.Number)
	comments, err := s.forge.ListIssueComments(ctx, s.repo, car.PullRequest)
	if err != nil {
		return err
	}
	posted := false
	for _, comment := range comments {
		if strings.Contains(comment.Body, marker) {
			posted = true
			break
		}
	}
	if !posted {
		body := trainEjectedComment(ejection) + "<!-- " + marker + " -->\n"
		if _, err := s.forge.CommentOnIssue(ctx, s.repo, car.PullRequest, body); err != nil {
			return err
		}
	}
	if label := s.binding.Forge.Label(LabelTrainEjected); label != "" {
		if err := s.forge.AddIssueLabels(ctx, s.repo, car.PullRequest, []string{label}); err != nil {
			return err
		}
	}
	if err := s.updateEntry(ctx, t, car, func(entry *TrainEntry) {
		entry.State = TrainEntryEjected
		recorded := ejection
		entry.Ejection = &recorded
	}); err != nil {
		return err
	}
	s.job.Audit(auditTrainEjectedPR, "train="+strconv.Itoa(t.Number)+" pull_request="+strconv.Itoa(car.PullRequest)+
		" head="+car.HeadSHA+" reason="+string(ejection.Reason))
	return nil
}

// =============================================================================
// Ending a train
// =============================================================================

// abandon ends the train in flight without landing anything more: every car
// still riding goes back to the queue, and the train's branches and pull
// requests are cleared away.
func (s *trainRun) abandon(ctx context.Context, reason string) error {
	t := s.train()
	if t == nil {
		return nil
	}
	for _, car := range t.Cars {
		if err := s.requeue(ctx, t, car); err != nil {
			return err
		}
	}
	s.job.Audit(auditTrainAbandoned, "train="+strconv.Itoa(t.Number)+" reason="+reason)
	return s.finish(ctx, "abandoned: "+reason)
}

// finish clears the train off the ledger and away from the forge.
//
// THE FORGE CLEAN-UP NEVER FAILS THE TRAIN. The decision is already recorded
// on the entries; a probe pull request the forge would not close is a display
// defect an operator can see in the audit trail, and refusing to end the train
// over it would hold every change behind it in the queue.
func (s *trainRun) finish(ctx context.Context, outcome string) error {
	t := s.train()
	if t == nil {
		return nil
	}
	branches := map[string]bool{TrainBranch(t.Number): true}
	for _, probe := range t.Probes {
		branches[probe.Branch] = true
		if probe.PullRequest == 0 {
			continue
		}
		if err := s.forge.ClosePullRequest(ctx, s.repo, probe.PullRequest); err != nil {
			s.job.Audit(auditTrainCleanup, "train="+strconv.Itoa(t.Number)+" pull_request="+
				strconv.Itoa(probe.PullRequest)+" error="+err.Error())
		}
	}
	if t.Phase != TrainBuilding {
		for branch := range branches {
			if err := s.forge.DeleteBranch(ctx, s.repo, branch); err != nil && !errors.Is(err, ErrNotFound) {
				s.job.Audit(auditTrainCleanup, "train="+strconv.Itoa(t.Number)+" branch="+branch+" error="+err.Error())
			}
		}
	}
	s.ledger.Active = nil
	if err := s.saveLedger(ctx); err != nil {
		return err
	}
	s.job.Emit(orchestration.JobStateCleanup, "merge train #"+strconv.Itoa(t.Number)+" "+outcome)
	return nil
}

// =============================================================================
// cleanup — the next train, or the end
// =============================================================================

// cleanup releases the session and forms the next train when the queue holds
// one, so a busy project's changes ride train after train in one job.
func (s *trainRun) cleanup(ctx context.Context) (orchestration.JobState, error) {
	s.teardownSession(ctx)
	if err := s.resolve(ctx); err != nil {
		return "", err
	}
	if s.train() != nil || !s.binding.TrainEnabled() {
		return "", nil
	}
	entries, err := s.runner.trains.ListTrainEntries(ctx, s.job.TenantID, s.binding.ProjectID)
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if entry.State == TrainEntryQueued {
			return orchestration.JobStateInitializing, nil
		}
	}
	return "", nil
}
