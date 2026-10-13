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
	"sync"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// The triage JobRunner — which issues are ready to be worked
// =============================================================================
//
// TRIAGE IS A JUDGEMENT, SO IT IS A JOB. It needs a session to read the
// repository through, a prompt, a budget, a recorded verdict and an audit
// trail — which is the list of things the engine's job envelope already
// provides — so it is a third JobRunner beside "workflow" and "sdlc" rather
// than a sweep, a cron or a script. Nothing here re-implements the envelope.
//
// IT IS THE FRONT HALF OF THE LOOP. The sdlc runner turns a TRIGGERED issue
// into a pull request; this runner decides which issues get triggered. Those
// are different shapes of work — one is per-issue and long, one is
// per-project and short — and they are deliberately not the same runner.
//
// WHAT IT MAY CHANGE IS LABELS, AND ONLY LABELS. It applies the binding's
// trigger label to admit an issue, applies needs-info with a comment saying
// what is missing, or comments with evidence and leaves the issue for a
// person. It NEVER closes an issue: a label is reversible and carries no
// social weight, and a close carries plenty. The ForgeBackend interface has
// no close verb, so the guarantee is structural as well as stated.

// TriageRunnerName is the name a Job selects this runner by.
const TriageRunnerName = "triage"

// TriageSessionCount is how many sessions a triage job ever holds: one, for
// however many issues it judges.
//
// A session per issue would pay the container start cost per issue, and the
// checkout the session reads is the same checkout for every issue in the
// project. It is a named constant because a test asserts against it rather
// than a literal.
const TriageSessionCount = 1

// DefaultTriageIssueLimit bounds how many issues one triage job lists.
const DefaultTriageIssueLimit = 100

// triageCommentMarker tags a comment this runner posted, so a later pass can
// find its own last word on an issue without re-reading the whole thread as
// prose.
const triageCommentMarker = "quicbaas-sdlc:triage"

// triageHeadMarker prefixes the commit a verdict was formed against, inside
// the same HTML comment.
//
// A VERDICT IS A JUDGEMENT ABOUT A MOVING REPOSITORY, and recording nothing
// about what it saw made it permanent. "Not actionable yet", "blocked on
// another change", "the prerequisite has not landed" are all answers about a
// tree, and they stop being true the moment that tree moves — which is
// precisely what a merge is. Without this, the only thing that could reopen a
// parked issue was a person commenting on it, so a backlog the factory parked
// stayed parked however much the repository changed underneath it. Thirteen
// go-luks2 issues sat that way while the factory merged a change to main.
const triageHeadMarker = "head="

// triageVerdictMarker prefixes the verdict a comment recorded, so a later pass
// can tell WHAT the issue is waiting on.
//
// A MERGE CANNOT SUPPLY A FACT A PERSON OWES. needs-info names a specific thing
// only a person can give — which of three overloads, what the threshold should
// be — and no amount of movement in the repository answers it. Reopening those
// on every merge spends a model turn per issue to re-reach the same verdict,
// which on a parked backlog is most of the fleet's triage cost and buys
// nothing. not-actionable and escalated are the opposite: "blocked on another
// change", "the prerequisite has not landed" are answers about a tree, and a
// merge is exactly what makes them wrong.
const triageVerdictMarker = "verdict="

// stateLabels for the triage runner's own states.
const (
	labelTriageInitializing = "Initializing"
	labelTriageJudging      = "Judging"
	labelTriageCleanup      = "Cleanup"
)

// TriageLeaseSubject is the lease subject every triage job of one project
// shares. It is what bounds triage to ONE JOB PER PROJECT at a time: two
// jobs naming the same subject contend, and the loser is refused with
// ErrJobLeaseTaken.
//
// It is a function rather than a string built at the call site so the two
// places that must agree — the runner's lease policy and any caller checking
// whether a project is already being triaged — cannot spell it differently.
func TriageLeaseSubject(projectID string) string {
	return TriageRunnerName + ":" + projectID
}

// TriageRunnerConfig wires the runner to what it reads the forge through.
type TriageRunnerConfig struct {
	// Bindings resolves the project's forge binding: the repository, the
	// trigger label, the display labels and the verification commands.
	Bindings BindingReader

	// Forges resolves the adapter the issues are read and written through.
	Forges ForgeResolver

	// Tokens resolves the forge token the session's container clones with.
	Tokens ForgeTokenReader

	// Signing starts the ephemeral, KMS-backed ssh agent the session's
	// container AUTHENTICATES ITS CLONE against when the project's remote is
	// ssh.
	//
	// IT IS REQUIRED, and triage's need for it is what go-quicbaas#929 was:
	// a runner built without one cannot clone an ssh remote at all, and the
	// only place that became visible was a container that had already been
	// spawned. The scope it asks for is SigningScopeCheckout — the access key
	// and no commit signing key — and the agent is stopped as soon as the
	// checkout is made.
	Signing SigningAgent

	// CheckoutRoot is the directory the checkout is mounted at, as the
	// CONTAINER sees it. Empty takes the engine's default.
	CheckoutRoot string

	// Guard is the operator's repository allowlist; see RunnerConfig.Guard.
	Guard RepoHostGuard

	// SocketsRoot is the DAEMON's root for per-session agent sockets
	// (orchestration.sockets_root, resolved by the host). It is required and
	// has no default here; see RunnerConfig.SocketsRoot.
	SocketsRoot string

	// IssueLimit bounds one job's issue listing; zero takes the default.
	IssueLimit int

	// TriageAgents decides the Agent or pool each project triages with: its
	// binding's own, then the node's default where the tenant defines it,
	// then its worker. See TriageAgentPolicy.
	TriageAgents TriageAgentPolicy

	// Clusters records which ready issues triage adopted as one task, and is
	// read back to keep a clustered member out of every later pass. Required:
	// see triage_cluster.go.
	Clusters IssueClusterStore
}

// TriageRunner is the module's triage orchestration.JobRunner.
type TriageRunner struct {
	bindings BindingReader
	forges   ForgeResolver
	clusters IssueClusterStore

	// access is the one seam every runner's container reaches the forge
	// through — the same one the delivery runner and the reviewer hold — so
	// the three cannot authenticate differently (go-quicbaas#929).
	access forgeAccess

	issueLimit int

	// unusable counts, per issue at a given default-branch head, the replies
	// this runner has set aside as not being verdicts. See judge for why it
	// is consulted and triageUnusableRetries for the bound.
	unusable   map[string]int
	unusableMu sync.Mutex

	// evaluated maps repo#issue -> the head at which triage last judged the
	// issue and reached the verdict it had already posted, so that verdict is
	// not re-judged at the same head. See comment.
	evaluated   map[string]string
	evaluatedMu sync.Mutex

	// triageAgents decides each project's triage agent; see
	// TriageRunnerConfig.
	triageAgents TriageAgentPolicy
}

var (
	_ orchestration.JobRunner      = (*TriageRunner)(nil)
	_ orchestration.JobLeasePolicy = (*TriageRunner)(nil)
)

// NewTriageRunner builds the runner; every collaborator is required.
func NewTriageRunner(cfg TriageRunnerConfig) (*TriageRunner, error) {
	switch {
	case cfg.Bindings == nil:
		return nil, &InvalidInputError{Field: "bindings", Message: "triage reads the trigger label and the repository from the binding"}
	case cfg.Forges == nil:
		return nil, &InvalidInputError{Field: "forges", Message: "triage reads and labels issues through the forge adapter"}
	case cfg.Clusters == nil:
		return nil, &InvalidInputError{Field: "clusters", Message: "triage records the issues it adopts as one task, and reads the record back"}
	}
	access, err := newForgeAccess(cfg.Tokens, cfg.Signing, cfg.Guard, cfg.CheckoutRoot, cfg.SocketsRoot)
	if err != nil {
		return nil, err
	}
	limit := cfg.IssueLimit
	if limit <= 0 {
		limit = DefaultTriageIssueLimit
	}
	return &TriageRunner{
		bindings:     cfg.Bindings,
		forges:       cfg.Forges,
		clusters:     cfg.Clusters,
		access:       access,
		issueLimit:   limit,
		triageAgents: cfg.TriageAgents,
	}, nil
}

// Name implements orchestration.JobRunner.
func (*TriageRunner) Name() string { return TriageRunnerName }

// States implements orchestration.JobRunner.
func (*TriageRunner) States() []orchestration.JobStateSpec { return TriageRunnerStates() }

// TriageRunnerStates is the triage sequence, in order.
//
// THE BOUNDS ARE MINUTES, NOT HOURS, and that is the point of declaring them
// here rather than inheriting the sdlc runner's. A triage turn reads an issue
// and greps a checkout; a delivery turn writes and tests a change. A triage
// job still sitting in its judging state after half an hour has lost its
// session, and the stall detector should end it rather than hold the
// project's one triage slot for the three hours a delivery is allowed.
//
// It is a package function as well as a method so the host can declare the
// states before a runtime exists to drive them: the stall detector's
// thresholds and the UI's stepper read the registry at registration time.
func TriageRunnerStates() []orchestration.JobStateSpec {
	return []orchestration.JobStateSpec{
		{State: orchestration.JobStateInitializing, Label: labelTriageInitializing, IdleThreshold: 5 * time.Minute},
		{State: orchestration.JobStateExecuting, Label: labelTriageJudging, SessionBacked: true, IdleThreshold: 30 * time.Minute},
		{State: orchestration.JobStateCleanup, Label: labelTriageCleanup, IdleThreshold: 5 * time.Minute},
	}
}

// Validate implements orchestration.JobRunner: a triage job is about a
// PROJECT, never a task.
func (*TriageRunner) Validate(_ context.Context, cfg *orchestration.JobConfig) error {
	if cfg == nil {
		return &InvalidInputError{Field: "config", Message: "must not be nil"}
	}
	if strings.TrimSpace(cfg.ProjectID) == "" {
		return &InvalidInputError{
			Field:   "project_id",
			Message: "triage is per project; the binding and the repository both hang off it",
		}
	}
	if strings.TrimSpace(cfg.TaskID) != "" {
		return &InvalidInputError{
			Field: "task_id",
			Message: "a triage job is not an attempt at a task; it decides which issues " +
				"become tasks at all",
		}
	}
	return nil
}

// LeaseSubject implements orchestration.JobLeasePolicy: every triage job of
// one project shares a subject, so exactly one runs at a time.
func (*TriageRunner) LeaseSubject(cfg *orchestration.JobConfig) string {
	if cfg == nil || strings.TrimSpace(cfg.ProjectID) == "" {
		return ""
	}
	return TriageLeaseSubject(strings.TrimSpace(cfg.ProjectID))
}

// OffProjectBudget implements orchestration.JobLeasePolicy.
//
// TRIAGE MUST NOT QUEUE BEHIND A CODE REVIEW. Quotas.MaxConcurrency bounds
// the workers a project runs at once, and the whole value of triage is that
// it is frequent, small and answers quickly. A project at max_concurrency 1
// that could not triage while its one worker ran would triage once every few
// hours, which is indistinguishable from not triaging at all. The per-project
// bound that DOES apply is the lease subject above.
func (*TriageRunner) OffProjectBudget() bool { return true }

// Run implements orchestration.JobRunner.
func (r *TriageRunner) Run(ctx context.Context, job *orchestration.JobRun) error {
	if job == nil {
		return &InvalidInputError{Field: "job", Message: "must not be nil"}
	}
	run := &triageRun{runner: r, job: job}
	return run.drive(ctx)
}

// =============================================================================
// One triage job
// =============================================================================

// triageRun is the state one triage job accumulates. It lives for exactly
// one Run.
type triageRun struct {
	runner *TriageRunner
	job    *orchestration.JobRun

	binding   *ProjectBinding
	forge     ForgeBackend
	repo      RepoRef
	sessionID string

	// headSHA is the tip of the project's default branch when this pass
	// started. It is stamped into every verdict this run posts, so a later
	// pass can tell whether the tree the verdict judged is still the tree
	// that is there. Empty when the branch could not be read, which makes
	// this run's verdicts unstamped and therefore always re-examinable —
	// the safe direction.
	headSHA string

	// spec is the workspace spec that was MATERIALIZED for the spawn, which
	// is the one description the checkout is then driven from: the container
	// reads the same bytes, so neither side invents a sibling file.
	spec orchestration.WorkspaceSpec

	// containerDir is the checkout as the CONTAINER addresses it. It is never
	// a daemon path.
	containerDir string

	// candidates are the issues this job judges, in issue order.
	candidates []ForgeIssue

	// open is every open issue in the project: the set a duplicate is
	// judged against, which is wider than the candidates because an issue
	// already in the factory is still something a new issue can duplicate.
	open []ForgeIssue

	// discussions is each candidate's comment thread, READ ONCE. The
	// candidate filter has to read it — triage's own last word is what says
	// whether the issue has already been answered — and the prompt needs the
	// same bytes, so reading it twice would be two forge calls per issue and
	// two views of one thread that a concurrent comment can disagree about.
	discussions map[int][]ForgeComment

	// workspace is the session's materialized workspace and the ssh agent its
	// checkout authenticated with. The agent is stopped as soon as the
	// checkout is made; the spec directory lives until the session ends.
	workspace *containerWorkspace

	// footprints are each judged issue's measured footprint (TriageFootprint),
	// which is what clustering groups the ready issues by.
	footprints map[int][]string

	// ready are the issues judged ready this pass, admitted together once
	// every candidate is judged so related ones can share a task.
	ready []ForgeIssue

	// teardown releases the materialized workspace spec.
	teardown []func()
}

// drive runs the job's states to a terminal one. Map-based dispatch so a
// state answers in constant time and the table reads as the sequence it is.
func (s *triageRun) drive(ctx context.Context) error {
	handlers := map[orchestration.JobState]func(context.Context) (orchestration.JobState, error){
		orchestration.JobStateInitializing: s.initialize,
		orchestration.JobStateExecuting:    s.judge,
		orchestration.JobStateCleanup:      s.cleanup,
	}
	// EVERY EXIT GIVES THE SESSION BACK, not only the planned one. A refusal
	// after the spawn, a cancellation and a panic are exits this run does
	// not plan, and each would otherwise leave a container alive with a
	// session record the daemon still held.
	defer s.teardownSession(ctx)

	// The session is published on every snapshot, so the stall detector has
	// something addressable to abort and a person has something to attach
	// to. A SessionBacked state whose snapshots name no session reads as a
	// job with no session at all.
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

// initialize resolves the binding, lists what there is to triage, and spawns
// the one session — but only if there is anything to judge.
func (s *triageRun) initialize(ctx context.Context) (orchestration.JobState, error) {
	if err := s.resolve(ctx); err != nil {
		return "", err
	}
	if err := s.listCandidates(ctx); err != nil {
		return "", err
	}
	if len(s.candidates) == 0 {
		// THE CHEAP PATH STAYS CHEAP. A project with nothing to triage is
		// the common case on a periodic pass, and paying for a container
		// start to discover it would make the backstop cost more than the
		// work it exists to catch.
		s.job.Emit(orchestration.JobStateCleanup, "no untriaged open issues; nothing to judge")
		return orchestration.JobStateCleanup, nil
	}
	if err := s.spawnSession(ctx); err != nil {
		return "", err
	}
	s.job.Emit(orchestration.JobStateExecuting,
		"triaging "+strconv.Itoa(len(s.candidates))+" open issue(s) of "+s.repo.String())
	return orchestration.JobStateExecuting, nil
}

// resolve loads the project's binding and forge adapter.
func (s *triageRun) resolve(ctx context.Context) error {
	project := s.job.Project
	if project == nil {
		return &InvalidInputError{Field: "project", Message: "a triage job runs for a project the daemon holds a record for"}
	}
	binding, err := s.runner.bindings.GetBinding(ctx, s.job.TenantID, project.ID)
	if err != nil {
		return triageBindingError(project.ID, err)
	}
	if binding == nil {
		return &TriageNotBoundError{ProjectID: project.ID}
	}
	forge, err := s.runner.forges.ForgeFor(ctx, s.job.TenantID, binding.Forge)
	if err != nil {
		return err
	}
	if forge == nil {
		return NewForgeNotRegisteredError(binding.Forge.Type)
	}
	s.binding, s.forge, s.repo = binding, forge, binding.Repo()
	s.readHead(ctx)
	return nil
}

// readHead records the tip of the project's default branch, which every
// verdict this run posts is stamped with.
//
// IT NEVER FAILS THE RUN. A branch that cannot be read costs this pass its
// stamps, and an unstamped verdict is re-examinable rather than permanent —
// so the failure mode is one extra judgement later, not a triage pass that
// refuses to judge anything.
func (s *triageRun) readHead(ctx context.Context) {
	base := strings.TrimSpace(s.job.Project.Git.DefaultBranch)
	if base == "" {
		base = FallbackDefaultBranch
	}
	branch, err := s.forge.GetBranch(ctx, s.repo, base)
	if err != nil || branch == nil {
		return
	}
	s.headSHA = strings.TrimSpace(branch.SHA)
}

// triageBindingError turns an absent binding into the runner's own refusal
// and passes every other failure through unchanged.
func triageBindingError(projectID string, err error) error {
	if errors.Is(err, ErrNotFound) {
		return &TriageNotBoundError{ProjectID: projectID}
	}
	return err
}

// listCandidates reads the project's open issues and keeps the ones this job
// should judge.
func (s *triageRun) listCandidates(ctx context.Context) error {
	issues, err := s.forge.ListIssues(ctx, s.repo, IssueFilter{State: "open", Limit: s.runner.issueLimit})
	if err != nil {
		return unavailable(err)
	}
	s.open = issues
	s.discussions = make(map[int][]ForgeComment, len(issues))
	// The owned-label set is built ONCE for the sweep rather than per label
	// of per issue: it is derived from the binding, which does not change
	// while the job runs.
	owned := s.binding.Forge.OwnedLabels()
	for i := range issues {
		issue := issues[i]
		// A HOLD IS CHECKED FIRST AND RECORDED. The owned-label rule already
		// passes over the binding's own blocked label, but it knows nothing of
		// the portfolio hold — which is how a `task:blocked` issue reached a
		// verdict on go-quicbaas#1058. Skipping it with nothing written would
		// leave an operator unable to tell a held issue from an unread one.
		if hold := triageHold(s.binding, &issue); hold != "" {
			s.job.Audit("sdlc:triage_skipped", "issue="+strconv.Itoa(issue.Number)+
				" reason=held label="+hold)
			continue
		}
		candidate, err := s.isCandidate(ctx, &issue, owned)
		if err != nil {
			// ONE ISSUE'S UNREADABLE THREAD IS NOT THE SWEEP'S FAILURE, and
			// it is not silence either: an issue dropped from the sweep with
			// nothing recorded is a project that looks triaged and is not.
			s.job.Audit("sdlc:triage_failed", "issue="+strconv.Itoa(issue.Number)+" error="+err.Error())
			s.job.Emit(orchestration.JobStateInitializing,
				"issue #"+strconv.Itoa(issue.Number)+" could not be triaged: "+err.Error())
			continue
		}
		if candidate {
			s.candidates = append(s.candidates, issue)
		}
	}
	return nil
}

// isCandidate reports whether one issue is this job's to judge.
//
// AN ISSUE ALREADY MOVING IS NOT RE-JUDGED. Carrying the trigger label, or
// any of the module's progress labels, means the factory has already taken a
// position on it — re-triaging would fight the runner that owns it. A closed
// issue is not judged either: there is no work to admit and nobody waiting
// on a verdict.
//
// AN ISSUE TRIAGE HAS ALREADY ANSWERED IS NOT RE-JUDGED EITHER, and that is
// not a property of any label. not-actionable and duplicate apply none at
// all — they leave the issue alone by design — so a rule that read only
// labels would see a bare open issue, spend a turn on it and post the same
// verdict again on every pass, for ever, on every repository in the fleet.
// The record of the verdict is triage's own comment, which is what
// spokenToSinceTriage reads.
func (s *triageRun) isCandidate(ctx context.Context, issue *ForgeIssue, owned map[string]bool) (bool, error) {
	if issueClosed(issue) {
		return false, nil
	}
	if issueTriggered(s.binding, issue) {
		return false, nil
	}
	clustered, err := s.clusteredElsewhere(ctx, issue.Number)
	if err != nil || clustered {
		return false, err
	}
	// needs-info does NOT settle the question. The verdict is a REQUEST for a
	// missing fact, and the whole point of making it is that somebody
	// answers; the answer arrives as a comment, which is what
	// spokenToSinceTriage looks for. Every other owned label marks work the
	// engine itself is carrying, and re-judging that would race the runner
	// that owns it.
	//
	// ESCALATED IS NOT EXEMPT (#1060). It was, so that a comment or a merge
	// re-judged an escalated issue; but escalated names a person as the next
	// step, and a verdict is not that person, so it is a hold (triageHold)
	// and stays out of triage until a person removes the label.
	asking := map[string]bool{}
	if label := s.binding.Forge.Label(LabelNeedsInfo); label != "" {
		asking[label] = true
	}
	for _, label := range issue.Labels {
		if asking[label] {
			continue
		}
		if owned[strings.ToLower(label)] {
			return false, nil
		}
	}
	return s.spokenToSinceTriage(ctx, issue)
}

// spokenToSinceTriage reports whether anybody has said anything on the issue
// since triage last did — which is what makes a second verdict worth paying
// for, whatever the first verdict was.
//
// THE NEWEST COMMENT IS THE WHOLE TEST, and it is deliberately not a
// timestamp comparison against the issue's own Updated. Every verdict writes
// a comment and needs-info then writes a LABEL, which advances Updated past
// the comment triage had just posted: an Updated-based rule would read
// triage's own second write as the reporter's answer and re-ask the question
// on the very next pass. Who spoke last cannot be moved by triage's own
// labelling, so it answers the question actually being asked.
//
// An issue nobody has commented on at all is a candidate: triage has never
// spoken, so there is nothing to wait on. An issue whose newest comment is
// somebody else's is a candidate too — a new comment is new information,
// whether it answers a needs-info question or disputes a not-actionable
// verdict.
//
// The cost of the narrowing is a body-only edit with no comment, which is
// not noticed until somebody comments. That is the right way round: a missed
// re-triage costs one human nudge, and the alternative costs a model turn
// and a duplicate comment on every issue in the fleet every sweep.
func (s *triageRun) spokenToSinceTriage(ctx context.Context, issue *ForgeIssue) (bool, error) {
	comments, err := s.forge.ListIssueComments(ctx, s.repo, issue.Number)
	if err != nil {
		// A thread that cannot be read is not evidence that anything
		// changed, so the issue is not judged; the caller records why.
		// Judging it blind would post a duplicate verdict.
		return false, &TriageVerdictError{Issue: issue.Number, Reason: "the discussion could not be read", Cause: err}
	}
	s.discussions[issue.Number] = comments
	var newest ForgeComment
	for _, comment := range comments {
		if comment.Created.Before(newest.Created) {
			continue
		}
		newest = comment
	}
	if newest.Created.IsZero() {
		return true, nil
	}
	if !strings.Contains(newest.Body, triageCommentMarker) {
		// Somebody else spoke last: new information, whatever it says.
		return true, nil
	}
	// Triage spoke last. Its verdict stands only while the tree it judged is
	// still the tree that is there — or while this runner has already
	// re-judged it at this head and found the same verdict (see comment).
	if s.runner != nil && s.runner.evaluatedAt(s.evaluatedKey(*issue), strings.TrimSpace(s.headSHA)) {
		return false, nil
	}
	return s.headMovedSince(newest), nil
}

// headMovedSince reports whether the project's default branch has moved since
// the verdict in this comment was formed.
//
// THIS IS WHAT REOPENS A PARKED BACKLOG, and nothing else did. A verdict of
// "blocked on another change" or "the prerequisite has not landed" is an answer
// about a tree, and a merge is exactly the event that makes it wrong — yet the
// only thing that could reopen an issue was a person commenting on it. Thirteen
// go-luks2 issues stayed parked through the factory merging a change to main,
// and the whole fleet quietly converged on having nothing to do.
//
// AN UNSTAMPED VERDICT IS ALWAYS RE-EXAMINABLE. Comments written before the
// head was recorded carry no marker, and a verdict whose tree cannot be
// identified is not evidence that the tree has not moved. They are re-judged
// once, which is also how a backlog parked by an older build gets a second
// reading.
//
// A HEAD THIS RUN COULD NOT READ REOPENS NOTHING EXTRA: an empty headSHA
// compares unequal to every stamp, so the pass errs toward re-judging rather
// than toward silence.
func (s *triageRun) headMovedSince(verdict ForgeComment) bool {
	// A QUESTION FOR A PERSON IS NOT REOPENED BY A MERGE. needs-info names
	// something only a person can supply, so the tree moving says nothing
	// about it; reopening it would re-ask the same question every time
	// anything landed.
	if triageVerdictCode(verdict.Body) == string(TriageOutcomeNeedsInfo) {
		return false
	}
	stamped := triageVerdictHead(verdict.Body)
	if stamped == "" {
		return true
	}
	return !strings.EqualFold(stamped, strings.TrimSpace(s.headSHA))
}

// triageVerdictCode reads the verdict a comment recorded, and "" when it
// recorded none.
func triageVerdictCode(body string) string {
	_, rest, found := strings.Cut(body, triageVerdictMarker)
	if !found {
		return ""
	}
	code, _, _ := strings.Cut(rest, " ")
	code, _, _ = strings.Cut(code, "-->")
	return strings.TrimSpace(code)
}

// triageVerdictHead reads the commit a verdict recorded, and "" when it
// recorded none.
func triageVerdictHead(body string) string {
	_, rest, found := strings.Cut(body, triageHeadMarker)
	if !found {
		return ""
	}
	sha, _, _ := strings.Cut(rest, " ")
	sha, _, _ = strings.Cut(sha, "-->")
	return strings.TrimSpace(sha)
}

// =============================================================================
// The session
// =============================================================================

// spawnSession starts the one session, with a checkout of the project's
// default branch for the agent to read.
//
// NO BRANCH AND NO COMMIT SIGNING KEY. Triage reads; it never commits, so
// there is no branch to create and nothing for a signing key to sign. What it
// does need is the project's ACCESS key, because a clone over ssh is
// authenticated by nothing else — and being handed no agent at all is what
// made every sweep fail inside a container it had already paid for
// (go-quicbaas#929). So it asks for SigningScopeCheckout, and the agent is
// stopped the moment the checkout is made: the socket the container holds for
// the rest of its life answers nothing, which is the posture the "no live
// signing socket beside untrusted issue text" rule was written to get.
func (s *triageRun) spawnSession(ctx context.Context) error {
	project := s.job.Project
	req, err := s.spawnRequest(ctx)
	if err != nil {
		return err
	}
	resp, err := s.job.Spawn(ctx, req)
	if err != nil {
		return workspaceSpawnFault(project.ID, err)
	}
	if resp == nil || resp.SessionID == "" {
		return &TriageSessionError{ProjectID: project.ID, Message: "the spawn returned no session"}
	}
	s.sessionID = resp.SessionID
	if strings.TrimSpace(resp.ContainerID) == "" {
		return &TriageSessionError{
			ProjectID: project.ID,
			Message: "session " + resp.SessionID + " is not containerized; triage reads issue text written by " +
				"anybody who can open an issue, which is untrusted input, and must not read it on the daemon",
		}
	}
	if resp.Workspace == nil {
		return &TriageSessionError{ProjectID: project.ID, Message: "the spawn reported no workspace for the checkout"}
	}
	s.containerDir = path.Join(s.spec.CheckoutRoot, project.Name)
	// The host side of the checkout, on whichever host ran the session.
	s.job.SetWorkDir(filepath.Join(resp.Workspace.Dir, project.Name))
	if err := s.job.SetContainer(s.sessionID, s.containerDir); err != nil {
		return err
	}
	return s.openCheckout(ctx)
}

// openCheckout clones the project, proves the clone, and stops the agent that
// authenticated it — in that order, and the stop happens whether the clone
// succeeded or not.
//
// THE CHARTER PROMISES A CHECKOUT — "a read-only checkout of the project's
// repository at its default branch, in your working directory. Read it." —
// and the very first thing triage is asked to decide is whether the change an
// issue asks for is ALREADY ON the default branch. Without the clone that
// check is unanswerable, and the duplicate-and-stale detection this runner
// exists for degrades to reading issue text. The daemon makes no checkout and
// no image entrypoint makes one either (see checkout.go): the runner drives
// it, inside the container, through the same seam the delivery and review
// runners use.
//
// AND IT IS PROVEN BEFORE A TURN IS SPENT ON IT. The pre-flight is the same
// one the delivery runner and the reviewer run (preflight.go): a checkout that
// is absent, empty, or holding another repository is a fault an operator
// repairs, and a model asked to judge issues against it would spend a turn
// establishing that by hand.
//
// THE AGENT GOES DOWN BEFORE THE MODEL COMES UP. Nothing after the checkout
// reaches the forge over git: the verdicts are forge API calls the DAEMON
// makes. So the one thing the container could do with a live agent is
// something triage never asked for, and the window is closed here rather than
// at the end of the session.
func (s *triageRun) openCheckout(ctx context.Context) error {
	checkout := s.checkout()
	err := checkout.Make(ctx)
	s.workspace.StopSigning(context.WithoutCancel(ctx))
	if err != nil {
		return err
	}
	return checkout.Verify(ctx)
}

// checkout is this session's checkout, driven through the one seam the
// delivery runner and the reviewer also drive.
func (s *triageRun) checkout() containerCheckout {
	return containerCheckout{
		job:        s.job,
		credential: s.workspace.forgeCredential(),
		sessionID:  s.sessionID,
		spec:       s.spec,
		project:    s.job.Project,
		taskID:     TriageLeaseSubject(s.job.Project.ID),
	}
}

// decorate adds the triage session to every snapshot, so the stall detector
// and a person watching the job see the session the turns run on. It is the
// job's only session and so its lead (go-quicbaas#863).
func (s *triageRun) decorate(status *orchestration.JobStatus) {
	if status == nil || s.sessionID == "" {
		return
	}
	status.SessionIDs = []string{s.sessionID}
	status.LeadSessionID = s.sessionID
}

// spawnRequest builds the session's spawn: a read-only checkout of the
// default branch, the triage charter as the system prompt.
func (s *triageRun) spawnRequest(ctx context.Context) (*orchestration.SpawnSessionRequest, error) {
	charter := TriageCharter()
	if strings.TrimSpace(charter) == "" {
		return nil, &TriageSessionError{
			ProjectID: s.job.Project.ID,
			Message:   "the triage charter is empty; the prompt was not embedded in this binary",
		}
	}
	project := s.job.Project
	branch := strings.TrimSpace(project.Git.DefaultBranch)
	if branch == "" {
		return nil, &InvalidInputError{
			Field:   "project.git.default_branch",
			Message: "triage reads the repository at its default branch and the project names none",
		}
	}
	agent, err := s.triageAgent(ctx)
	if err != nil {
		return nil, err
	}
	// THE AGENT SUPPLIES THE RUNTIME, THE MODEL AND THE IMAGE, and a pool
	// fails over at the spawn: a saturated NPU spills to the next Agent
	// rather than stalling the sweep.
	req := &orchestration.SpawnSessionRequest{
		AgentID:      agent.AgentID,
		PoolID:       agent.PoolID,
		Role:         orchestration.AgentRoleTriage,
		ProjectID:    project.ID,
		SystemPrompt: charter,
		PromptAppend: s.binding.TriagePromptAppend,
		Persistent:   true,
	}
	// TRIAGE STATES ITS OWN GRANT, AND IT STATES THE NARROWER ONE. A
	// sandboxed spawn otherwise takes full control of its container
	// (orchestration.WorkerFullControlFlag), because that is what a worker
	// needs to build and test what it wrote. Triage reads the repository at
	// its default branch and writes nothing — its prompt says the checkout is
	// read-only — so it keeps the host grant, which withholds Bash. Stated
	// here rather than derived, because ApplyWorkerSpawnEnvironment leaves an
	// already-stated grant alone and that is the whole opt-out.
	req.ExtraArgs = append(req.ExtraArgs, "--permission-mode", orchestration.WorkerPermissionMode)
	workspace, err := s.runner.access.Open(ctx, req, workspacePlan{
		TenantID: s.job.TenantID,
		Project:  project,
		Forge:    s.binding.Forge,
		Branch:   orchestration.WorkspaceBranchPlan{Name: branch},
		Key:      "triage-" + project.ID,
		Scope:    SigningScopeCheckout,
	})
	if err != nil {
		return nil, err
	}
	stopCtx := context.WithoutCancel(ctx)
	s.teardown = append(s.teardown, func() { workspace.Release(stopCtx) })
	s.workspace = workspace
	s.spec = workspace.Spec
	return req, nil
}

// teardownSession closes the session and releases the materialized spec, in
// reverse order of acquisition. It is idempotent.
func (s *triageRun) teardownSession(ctx context.Context) {
	if s.sessionID != "" {
		// WITHOUT CANCEL, because every unplanned exit arrives here with the
		// context already cancelled — a shutdown, a supersession, a stall
		// abort — and a close that inherits it fails immediately and leaves
		// the container running with nothing left holding its id.
		closeSession(context.WithoutCancel(ctx), s.job, s.sessionID, s.workspace)
		s.sessionID = ""
	}
	for i := len(s.teardown) - 1; i >= 0; i-- {
		s.teardown[i]()
	}
	s.teardown = nil
}

// =============================================================================
// The verdicts
// =============================================================================

// judge takes one turn per candidate issue and applies each verdict.
//
// ONE ISSUE'S FAILURE IS NOT THE SWEEP'S. A forge write that fails, or an
// issue whose prompt cannot be rendered, is recorded against that issue and
// the run moves on: the sweep is the only path the other issues have, and
// losing all of them to one bad record is the failure mode this job exists to
// remove, not to reproduce.
// triageUnusableRetries is how many replies the runner sets aside for one
// issue at one head before it stops asking until something changes.
//
// AN UNUSABLE REPLY LEAVES NO MARK ON THE ISSUE, which is the point of it --
// and also means the issue is a candidate again on every pass. A model that
// fails an issue the same way each time would then be asked every fifteen
// minutes for as long as the head stands: measured on go-luks2#106 and #79,
// 27- and 54-byte replies on three passes running. Two tries is the bound;
// after that the issue waits for the head to move or for somebody to speak,
// both of which change what the model is shown.
const triageUnusableRetries = 2

// unusableKey names one issue at one head.
func (s *triageRun) unusableKey(issue ForgeIssue) string {
	return s.repo.String() + "#" + strconv.Itoa(issue.Number) + "@" + s.headSHA
}

func (r *TriageRunner) unusableCount(key string) int {
	r.unusableMu.Lock()
	defer r.unusableMu.Unlock()
	return r.unusable[key]
}

func (r *TriageRunner) noteUnusable(key string) int {
	r.unusableMu.Lock()
	defer r.unusableMu.Unlock()
	if r.unusable == nil {
		r.unusable = map[string]int{}
	}
	r.unusable[key]++
	return r.unusable[key]
}

func (s *triageRun) judge(ctx context.Context) (orchestration.JobState, error) {
	for i := range s.candidates {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		issue := s.candidates[i]
		if s.runner != nil && s.runner.unusableCount(s.unusableKey(issue)) >= triageUnusableRetries {
			s.job.Audit("sdlc:triage_skipped", "issue="+strconv.Itoa(issue.Number)+
				" reason=the model gave no usable verdict on "+strconv.Itoa(triageUnusableRetries)+
				" passes at head "+s.headSHA+"; waiting for the head to move or a comment")
			continue
		}
		if err := s.judgeIssue(ctx, issue); err != nil {
			var held *TriageHeldError
			if errors.As(err, &held) {
				s.job.Audit("sdlc:triage_skipped", "issue="+strconv.Itoa(issue.Number)+
					" reason=held label="+held.Label)
				s.job.Emit(orchestration.JobStateExecuting,
					"issue #"+strconv.Itoa(issue.Number)+" is held by "+held.Label+"; the verdict was not applied")
				continue
			}
			s.job.Audit("sdlc:triage_failed", "issue="+strconv.Itoa(issue.Number)+" error="+err.Error())
			s.job.Emit(orchestration.JobStateExecuting,
				"issue #"+strconv.Itoa(issue.Number)+" could not be triaged: "+err.Error())
		}
	}
	s.admitReady(ctx)
	return orchestration.JobStateCleanup, nil
}

// judgeIssue prompts for one issue, classifies the reply and applies the
// verdict.
func (s *triageRun) judgeIssue(ctx context.Context, issue ForgeIssue) error {
	facts, measured := s.measureFacts(ctx, issue)
	rendered := ""
	if measured {
		rendered = facts.Render()
		if s.footprints == nil {
			s.footprints = map[int][]string{}
		}
		s.footprints[issue.Number] = TriageFootprint(facts)
	}
	prompt, err := TriagePrompt(TriagePromptSpec{
		Repo:         s.repo,
		Issue:        issue,
		Comments:     s.discussions[issue.Number],
		OpenIssues:   s.open,
		Verification: s.binding.Verification,
		MayClose:     s.binding.TriageMayClose,
		Facts:        rendered,
	})
	if err != nil {
		return err
	}
	resp, err := s.job.Prompt(ctx, &orchestration.PromptSessionRequest{SessionID: s.sessionID, Prompt: prompt})
	if err != nil {
		return &TriageVerdictError{Issue: issue.Number, Reason: "the triage turn failed", Cause: err}
	}
	turn := ReadAgentTurn(resp)
	verdict := ClassifyTriage(ParseTriageDeclaration(turn.Reply))
	s.recordVerdict(issue, verdict, turn)
	if reason := s.unusable(issue, verdict, turn.Reply); reason != "" {
		s.recordUnusable(issue, verdict, turn, reason)
		return nil
	}
	return s.apply(ctx, issue, verdict, turn.Reply)
}

// unusable says why a reply is not a verdict at all, or "" when it is one.
//
// A REPLY THE BINARY CANNOT READ IS THE MODEL'S FAILURE, NOT A FINDING ABOUT
// THE ISSUE. It used to be posted as one: "triage declared a verdict this
// binary cannot act on" went on the issue with the escalated label, and the
// comment then became triage's last word, so the issue was never judged again
// until a person spoke. Measured on go-luks2 on the NPU, 2026-09-21: one pass
// posted a self-referencing duplicate with a misspelt marker (#167), a
// not-actionable carrying no evidence at all (#106, a 27-byte reply), and a
// duplicate of an unrelated issue -- three verdicts a person now has to undo,
// none of which said anything about the work. Every one of them is a model
// that ran out of rounds or lost the marker vocabulary, and the right answer
// to that is to say nothing on the forge and ask again next pass.
//
// The charter demands evidence for every verdict that is not ready, so a
// reply whose prose is only the declaration fails the same test: a person
// reading "the evidence:" followed by nothing has been told nothing. A
// duplicate must name another OPEN issue -- itself, a closed one, or a number
// the pass never listed is a reference a person cannot follow.
func (s *triageRun) unusable(issue ForgeIssue, verdict TriageVerdict, reply string) string {
	switch verdict.Outcome {
	case TriageOutcomeUndeclared:
		return "the reply declared no verdict"
	case TriageOutcomeUnreadable:
		return "the reply's declaration could not be read"
	case TriageOutcomeReady:
		return ""
	case TriageOutcomeDuplicate:
		if verdict.DuplicateOf == issue.Number {
			return "the reply names the issue as a duplicate of itself"
		}
		if !s.isOpenCandidate(verdict.DuplicateOf) {
			return "the reply names #" + strconv.Itoa(verdict.DuplicateOf) +
				" as the duplicate, which is not an open issue of this project"
		}
	}
	if triageEvidence(reply) == "" {
		return "the reply gives no evidence for its verdict"
	}
	return ""
}

// isOpenCandidate reports whether number is among the open issues this pass
// listed: the set the prompt offered as duplicate candidates.
func (s *triageRun) isOpenCandidate(number int) bool {
	for i := range s.open {
		if s.open[i].Number == number && !issueClosed(&s.open[i]) {
			return true
		}
	}
	return false
}

// recordUnusable writes why a reply was set aside, so the audit trail says
// the pass judged the issue and got nothing it could act on -- which is what
// distinguishes a model that keeps failing from a pass that never ran.
func (s *triageRun) recordUnusable(issue ForgeIssue, verdict TriageVerdict, turn AgentTurn, reason string) {
	tries := 0
	if s.runner != nil {
		tries = s.runner.noteUnusable(s.unusableKey(issue))
	}
	s.job.Audit("sdlc:triage_unusable", "issue="+strconv.Itoa(issue.Number)+
		" outcome="+verdict.Outcome.String()+" reply_bytes="+strconv.Itoa(turn.ReplyBytes)+
		" tries="+strconv.Itoa(tries)+" reason="+reason)
	s.job.Emit(orchestration.JobStateExecuting,
		"issue #"+strconv.Itoa(issue.Number)+": the reply was not a verdict ("+reason+
			"); nothing was written to the issue and the next pass judges it again")
}

// measureFacts asks the checkout about what the issue names, and reports
// whether there is anything to render: nothing named, or a measurement that
// failed, answers false.
//
// FAIL-SOFT, AND AUDITED. The facts are evidence the model would otherwise
// have to fetch; a checkout that will not answer leaves the model where it
// was, which is worse than before only if the failure is silent. So the
// failure is written to the audit trail and the prompt goes out without the
// section, rather than the issue going unjudged.
func (s *triageRun) measureFacts(ctx context.Context, issue ForgeIssue) (TriageFacts, bool) {
	probes := ExtractTriageProbes(issue.Title, issue.Body)
	if len(probes.Paths) == 0 && len(probes.Identifiers) == 0 {
		return TriageFacts{}, false
	}
	facts, err := MeasureTriageFacts(sessionExec(ctx, s.job, s.sessionID), s.containerDir, s.headSHA, probes)
	if err != nil {
		s.job.Audit("sdlc:triage_facts_failed", "issue="+strconv.Itoa(issue.Number)+" error="+err.Error())
		return TriageFacts{}, false
	}
	s.job.Audit("sdlc:triage_facts", "issue="+strconv.Itoa(issue.Number)+
		" paths="+strconv.Itoa(len(facts.Paths))+" identifiers="+strconv.Itoa(len(facts.Identifiers)))
	return facts, true
}

// recordVerdict writes the verdict to the job's audit trail and its event
// stream, so an operator can say what triage concluded about an issue and
// why without re-reading the forge.
func (s *triageRun) recordVerdict(issue ForgeIssue, verdict TriageVerdict, turn AgentTurn) {
	details := "issue=" + strconv.Itoa(issue.Number) +
		" outcome=" + verdict.Outcome.String() +
		" disposition=" + verdict.Disposition.String()
	if verdict.DuplicateOf > 0 {
		details += " duplicate_of=" + strconv.Itoa(verdict.DuplicateOf)
	}
	details += " reply_bytes=" + strconv.Itoa(turn.ReplyBytes) +
		" tool_uses=" + turn.DescribeToolUses() +
		" stop_reason=" + turn.StopReason
	s.job.Audit("sdlc:triage_verdict", details)
	s.job.Emit(orchestration.JobStateExecuting,
		"issue #"+strconv.Itoa(issue.Number)+": "+verdict.Outcome.String())
}

// triageAppliers is the disposition table: what each verdict DOES to the
// issue. Map-based so a disposition answers in constant time and the set of
// things triage may do to an issue is data a reader can enumerate — there are
// three, and none of them closes anything.
var triageAppliers = map[TriageDisposition]func(*triageRun, context.Context, ForgeIssue, TriageVerdict, string) error{
	TriageDispositionReady:         (*triageRun).deferReady,
	TriageDispositionNeedsInfo:     (*triageRun).applyNeedsInfo,
	TriageDispositionNotActionable: (*triageRun).applyNotActionable,
}

// apply carries out the verdict.
//
// A HELD ISSUE IS NEVER WRITTEN TO, whatever the verdict. Candidate selection
// already passes held issues over; this is the second line, because a turn
// takes minutes and a hold applied while the model was reading is a hold all
// the same. The issue is therefore re-read here, immediately before the first
// write, and a hold on either the listed or the fresh copy refuses the whole
// verdict — no comment, no label. A re-read that fails refuses it too: a
// verdict that cannot prove the issue is not held is not applied.
func (s *triageRun) apply(ctx context.Context, issue ForgeIssue, verdict TriageVerdict, reply string) error {
	applier, ok := triageAppliers[verdict.Disposition]
	if !ok {
		return &TriageVerdictError{
			Issue:  issue.Number,
			Reason: "disposition " + verdict.Disposition.String() + " has no applier",
		}
	}
	if err := s.refuseHeld(ctx, issue); err != nil {
		return err
	}
	return applier(s, ctx, issue, verdict, reply)
}

// refuseHeld returns a TriageHeldError when the issue carries a hold, as
// listed or as the forge holds it now.
func (s *triageRun) refuseHeld(ctx context.Context, issue ForgeIssue) error {
	if hold := triageHold(s.binding, &issue); hold != "" {
		return &TriageHeldError{Issue: issue.Number, Label: hold}
	}
	fresh, err := s.forge.GetIssue(ctx, s.repo, issue.Number)
	if err != nil {
		return &TriageVerdictError{Issue: issue.Number, Reason: "the issue could not be re-read to rule out a hold", Cause: err}
	}
	if fresh == nil {
		return &TriageVerdictError{Issue: issue.Number, Reason: "the forge returned no issue to rule out a hold"}
	}
	if hold := triageHold(s.binding, fresh); hold != "" {
		return &TriageHeldError{Issue: issue.Number, Label: hold}
	}
	return nil
}

// applyReady admits the issue to the factory: the trigger label goes on, and
// a needs-info label an earlier pass left is taken off.
//
// THE LABEL GOES ON LAST. Removing needs-info first means a crash between the
// two writes leaves an issue with neither label — untriaged, and picked up
// again by the next pass, which is the recoverable order. The other way round
// leaves an issue that is both ready and blocked.
func (s *triageRun) applyReady(ctx context.Context, issue ForgeIssue) error {
	if needsInfo := s.binding.Forge.Label(LabelNeedsInfo); needsInfo != "" && issueHasLabel(issue, needsInfo) {
		if err := s.forge.RemoveIssueLabel(ctx, s.repo, issue.Number, needsInfo); err != nil {
			return &TriageVerdictError{Issue: issue.Number, Reason: "the needs-info label could not be cleared", Cause: err}
		}
	}
	trigger := strings.TrimSpace(s.binding.Forge.TriggerLabel)
	if trigger == "" {
		return &TriageVerdictError{Issue: issue.Number, Reason: "the binding names no trigger label"}
	}
	if err := s.forge.AddIssueLabels(ctx, s.repo, issue.Number, []string{trigger}); err != nil {
		return &TriageVerdictError{Issue: issue.Number, Reason: "the trigger label could not be applied", Cause: err}
	}
	return nil
}

// applyNeedsInfo labels the issue and says exactly what is missing.
//
// THE COMMENT GOES FIRST. A label with no comment is the vague "clarify
// please" the design forbids, so if only one of the two writes lands it must
// be the one that carries the question.
func (s *triageRun) applyNeedsInfo(ctx context.Context, issue ForgeIssue, verdict TriageVerdict, reply string) error {
	if err := s.comment(ctx, issue, verdict, reply); err != nil {
		return err
	}
	label := s.binding.Forge.Label(LabelNeedsInfo)
	if label == "" {
		// The binding displays no needs-info label. The comment carried the
		// question, which is the part a person acts on.
		return nil
	}
	s.clearOtherDispositions(ctx, issue, LabelNeedsInfo)
	if err := s.forge.AddIssueLabels(ctx, s.repo, issue.Number, []string{label}); err != nil {
		return &TriageVerdictError{Issue: issue.Number, Reason: "the needs-info label could not be applied", Cause: err}
	}
	return nil
}

// triageDispositionLabels are the label keys one verdict can leave behind.
// Exactly one of them may stand at a time: they are alternative answers to the
// same question, not facts that accumulate.
var triageDispositionLabels = []string{LabelNeedsInfo, LabelEscalated}

// clearOtherDispositions removes the disposition labels this verdict is NOT,
// so an issue carries one answer rather than a pile of them.
//
// A VERDICT REPLACES THE ONE BEFORE IT. Each disposition only ever ADDED its
// label, so an issue judged needs-info after being escalated ended up carrying
// both — two contradictory answers, and an operator scanning the queue cannot
// tell which is current. Measured on go-luks2 after one re-triage pass: six of
// eleven open issues carried `factory:escalated` and `factory:needs-info` at
// once.
//
// A REMOVAL THAT FAILS IS NOT A FAILED VERDICT. The comment carries the
// reasoning and the new label is applied regardless; a stale second label is a
// display defect, and refusing the verdict over it would trade a confusing
// issue for an unjudged one.
func (s *triageRun) clearOtherDispositions(ctx context.Context, issue ForgeIssue, keep string) {
	held := map[string]bool{}
	for _, label := range issue.Labels {
		held[strings.ToLower(strings.TrimSpace(label))] = true
	}
	for _, key := range triageDispositionLabels {
		if key == keep {
			continue
		}
		label := s.binding.Forge.Label(key)
		if label == "" || !held[strings.ToLower(label)] {
			continue
		}
		if err := s.forge.RemoveIssueLabel(ctx, s.repo, issue.Number, label); err != nil {
			// A stale display label is not worth failing the verdict over; record it.
			s.job.Audit("sdlc:triage_label_stale", "issue="+strconv.Itoa(issue.Number)+" label="+label+" error="+err.Error())
		}
	}
}

// applyNotActionable writes the evidence, and then leaves the issue in a state
// somebody can find: closed when the work is already done and the binding
// allows it, escalated otherwise.
//
// LEAVING NO LABEL WAS THE DEFECT THAT IDLED THE FLEET. A not-actionable
// verdict used to comment and stop, so the issue stayed open carrying no
// factory label at all -- and because the newest comment was then triage's
// own, spokenToSinceTriage ruled it not a candidate on every later pass. The
// issue was invisible to the factory and unmarked for a person: nobody was
// waiting on it and nothing would look at it again. Measured on go-luks2,
// 2026-09-16: five issues in exactly that state, and a fleet that had
// triaged nothing for 34 hours because every judgeable issue had reached it.
//
// ESCALATED IS THE HONEST NAME FOR WHAT THIS VERDICT IS. "Not actionable by a
// worker" and "a person must decide" are the same sentence from two ends; the
// verdicts say so themselves ("needs one ruling before a worker should touch
// it"). Escalated is a triage hold (#1060): the issue is out of triage's
// reach until a person removes the label, which is the person's answer.
//
// BOTH CONDITIONS ARE REQUIRED. A not-actionable that named no reason is never
// closed however permissive the binding, and a binding that has not opted in
// never closes however confident the agent — so turning the toggle on cannot
// retroactively close the issues an earlier pass judged, and an agent that
// invents the marker cannot close anything in a project that did not ask for
// it.
func (s *triageRun) applyNotActionable(ctx context.Context, issue ForgeIssue, verdict TriageVerdict, reply string) error {
	if err := s.comment(ctx, issue, verdict, reply); err != nil {
		return err
	}
	// THE COMMENT GOES FIRST, AND THAT ORDER IS THE RECOVERABLE ONE. A pass
	// that crashes between the two leaves an open issue carrying the evidence
	// — which the next pass can act on — where closing first would leave a
	// closed issue saying nothing about why.
	if verdict.ClosesAsAlreadyDone && s.binding.TriageMayClose {
		if err := s.forge.CloseIssue(ctx, s.repo, issue.Number); err != nil {
			return &TriageVerdictError{Issue: issue.Number, Reason: "the issue could not be closed", Cause: err}
		}
		return nil
	}
	// The issue stays open, so it must carry the one label that says a person
	// is the next step. A binding that displays no escalated label keeps the
	// old behaviour: the comment is the record, which is the part a person
	// acts on.
	label := s.binding.Forge.Label(LabelEscalated)
	if label == "" {
		return nil
	}
	s.clearOtherDispositions(ctx, issue, LabelEscalated)
	if issueHasLabel(issue, label) {
		return nil
	}
	if err := s.forge.AddIssueLabels(ctx, s.repo, issue.Number, []string{label}); err != nil {
		return &TriageVerdictError{Issue: issue.Number, Reason: "the escalated label could not be applied", Cause: err}
	}
	return nil
}

// comment posts the verdict a person reads, tagged so a later pass can find
// triage's own last word on the issue.
//
// A VERDICT MATERIALLY IDENTICAL TO TRIAGE'S LAST ONE IS NOT POSTED AGAIN.
// A merge reopens a parked verdict (headMovedSince), which is right — the tree
// it judged is gone — but a re-judgement that reaches the same verdict has
// nothing to tell the reader, and posting it anyway put the same "duplicate of
// #981" on go-quicbaas#1013 once per merge, sixteen times over. "The same" is
// deliberately narrow (sameTriageVerdict): a duplicate of the same issue, or
// any other outcome with the same body — so a new needs-info question is
// always posted.
//
// A SUPPRESSED VERDICT STILL RECORDS THE HEAD IT WAS FORMED AGAINST. Posting is
// what normally records it (the comment's head marker); without a comment the
// next sweep would see the head still "moved" and spend a model turn reaching
// the same verdict again, every sweep. The runner remembers the evaluated head
// instead (TriageRunner.noteEvaluated), which spokenToSinceTriage consults.
func (s *triageRun) comment(ctx context.Context, issue ForgeIssue, verdict TriageVerdict, reply string) error {
	evidence := triageEvidence(reply)
	body := TriageComment(verdict, evidence)
	if strings.TrimSpace(body) == "" {
		return nil
	}
	if last, ok := lastTriageComment(s.discussions[issue.Number]); ok && sameTriageVerdict(last.Body, verdict, evidence) {
		if s.runner != nil {
			s.runner.noteEvaluated(s.evaluatedKey(issue), strings.TrimSpace(s.headSHA))
		}
		s.job.Audit("sdlc:triage_unchanged", "issue="+strconv.Itoa(issue.Number)+
			" outcome="+verdict.Outcome.String()+" head="+s.headSHA+
			" reason=the verdict is the one already posted; not posted again")
		return nil
	}
	body += "\n<!-- " + triageCommentMarker +
		" " + triageHeadMarker + s.headSHA +
		" " + triageVerdictMarker + string(verdict.Outcome) + " -->\n"
	if _, err := s.forge.CommentOnIssue(ctx, s.repo, issue.Number, body); err != nil {
		return &TriageVerdictError{Issue: issue.Number, Reason: "the verdict could not be posted", Cause: err}
	}
	return nil
}

// evaluatedKey names one issue of this run's repository, whatever the head.
func (s *triageRun) evaluatedKey(issue ForgeIssue) string {
	return s.repo.String() + "#" + strconv.Itoa(issue.Number)
}

// noteEvaluated records that the issue at key was judged at head and its
// verdict was the one already posted. One entry per issue, overwritten as the
// head moves, so the record is bounded by the backlog, not by history.
func (r *TriageRunner) noteEvaluated(key, head string) {
	r.evaluatedMu.Lock()
	defer r.evaluatedMu.Unlock()
	if r.evaluated == nil {
		r.evaluated = map[string]string{}
	}
	r.evaluated[key] = head
}

// evaluatedAt reports whether the issue at key was last judged at head.
func (r *TriageRunner) evaluatedAt(key, head string) bool {
	r.evaluatedMu.Lock()
	defer r.evaluatedMu.Unlock()
	return head != "" && r.evaluated[key] == head
}

// lastTriageComment returns the newest comment triage posted on a thread.
func lastTriageComment(comments []ForgeComment) (ForgeComment, bool) {
	var last ForgeComment
	found := false
	for _, comment := range comments {
		if !strings.Contains(comment.Body, triageCommentMarker) {
			continue
		}
		if found && comment.Created.Before(last.Created) {
			continue
		}
		last = comment
		found = true
	}
	return last, found
}

// sameTriageVerdict reports whether a posted triage comment is materially the
// verdict about to be posted, given its evidence:
//
//   - a DUPLICATE is the same when it names the same issue — the headline line
//     carries the target — because the prose around "this is #981's work" is
//     not news;
//   - every other outcome is the same only when its whole body is, compared
//     with whitespace normalised. A needs-info comment IS its question, so two
//     different questions are two verdicts and both are posted.
func sameTriageVerdict(posted string, verdict TriageVerdict, evidence string) bool {
	if triageVerdictCode(posted) != string(verdict.Outcome) {
		return false
	}
	postedBody, _, _ := strings.Cut(posted, "<!-- "+triageCommentMarker)
	if verdict.Outcome == TriageOutcomeDuplicate {
		headline, _, _ := strings.Cut(TriageComment(verdict, ""), "\n")
		postedHeadline, _, _ := strings.Cut(postedBody, "\n")
		return strings.TrimSpace(headline) != "" && strings.TrimSpace(headline) == strings.TrimSpace(postedHeadline)
	}
	want := normalizeTriageBody(TriageComment(verdict, evidence))
	return want != "" && want == normalizeTriageBody(postedBody)
}

// normalizeTriageBody collapses whitespace so a re-wrapped line is not a new
// verdict.
func normalizeTriageBody(body string) string {
	return strings.Join(strings.Fields(body), " ")
}

// triageEvidence is the agent's own text with its declaration lines removed:
// what a person reads, without the machine-readable markers that mean
// nothing to them.
func triageEvidence(reply string) string {
	lines := strings.Split(reply, "\n")
	kept := make([]string, 0, len(lines))
	for _, raw := range lines {
		line := triageDeclarationLine(raw)
		if strings.HasPrefix(line, TriageDuplicateMarker) || strings.HasPrefix(line, TriageDeclarationMarker) {
			continue
		}
		kept = append(kept, raw)
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
}

// issueHasLabel reports whether an issue carries a label, compared exactly:
// labels are records, not prose.
func issueHasLabel(issue ForgeIssue, label string) bool {
	for _, held := range issue.Labels {
		if held == label {
			return true
		}
	}
	return false
}

// cleanup gives the session back and ends the job.
func (s *triageRun) cleanup(ctx context.Context) (orchestration.JobState, error) {
	s.teardownSession(ctx)
	return "", nil
}

// triageAgent picks the Agent or pool this project triages with; see
// TriageAgentPolicy.For for the order and why the node default matters.
func (s *triageRun) triageAgent(ctx context.Context) (orchestration.AgentRef, error) {
	var policy TriageAgentPolicy
	if s.runner != nil {
		policy = s.runner.triageAgents
	}
	return policy.For(ctx, s.job.TenantID, s.binding)
}
