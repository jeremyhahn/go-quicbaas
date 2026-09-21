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
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// The runner's rig
// =============================================================================
//
// It is the hooks rig plus the three things only the runner needs: an agent
// service to spawn and prompt through, the forge token the container clones
// with, and the signing agent it commits through. The forge, the store and
// the workspace are the module's own fakes, so what the runner is measured
// against is the module's real behaviour and not a second description of it.

// issueContract is the Task description the source renders: what a person
// wrote, whole. The test asserts THIS text reaches the worker.
const issueContract = `Repository: acme/api
Issue: #25

## Fix the thing

The thing is broken in a specific and load-bearing way.

## Discussion

### Comment 1 — @jeremyhahn

Please keep the fix inside pkg/thing.
`

// liveReviewValidator is the ReviewValidator the process-registered
// forge_review kind forwards to. The registry is process-global and
// append-only, exactly as it is in the daemon, so the kind is registered once
// and pointed at whichever rig is running.
var (
	liveReviewValidator atomic.Pointer[ReviewValidator]
	registerReviewOnce  sync.Once
)

func registerTestReviewValidator(t *testing.T) {
	t.Helper()
	registerReviewOnce.Do(func() {
		if err := orchestration.RegisterValidator(ValidatorKindContainerCommand, ValidateInContainer); err != nil {
			t.Fatalf("registering %s: %v", ValidatorKindContainerCommand, err)
		}
		err := orchestration.RegisterValidator(ValidatorKindForgeReview, func(ctx context.Context, job *orchestration.JobContext, criterion orchestration.ReviewCriterion) (orchestration.ValidationResult, error) {
			validator := liveReviewValidator.Load()
			if validator == nil {
				return orchestration.ValidationResult{Name: criterion.Name}, ErrUnavailable
			}
			return validator.Validate(ctx, job, criterion)
		})
		if err != nil {
			t.Fatalf("registering %s: %v", ValidatorKindForgeReview, err)
		}
	})
}

// fakeAgents is the agent service the runner spawns and prompts through.
//
// The embedded interface is nil: every method the runner does not use panics
// rather than answering, so a runner that reached for the fleet, the registry
// or a second backend fails the test loudly instead of being quietly served.
type fakeAgents struct {
	orchestration.AgentService

	mu      sync.Mutex
	spawns  []*orchestration.SpawnSessionRequest
	prompts []*orchestration.PromptSessionRequest
	closed  []string
	seq     int

	// onPrompt runs for each prompt, so a test can act as the worker does.
	onPrompt func(req *orchestration.PromptSessionRequest)

	// replyFn is what the worker ANSWERS with, by turn number starting at
	// one. Nil answers with no content, which is what a backend whose CLI
	// published nothing looks like.
	replyFn func(turn int) *orchestration.PromptSessionResponse

	spawnErr  error
	promptErr error

	// container is the worker's container. A spawn reports its id, and an
	// exec is dispatched into it: the runner reaches git only this way.
	container *fakeContainer

	// noContainer makes a spawn answer without one, which is what a worker
	// running as a host process looks like.
	noContainer bool

	// containerID overrides the id a spawn reports. A REMOTE worker's id is
	// the daemon's, reported over the wire and never derived here — see
	// go-quicbaas#740, where it was dropped in transit and every remote worker
	// was refused as running no container.
	containerID string
}

func (f *fakeAgents) SpawnSession(_ context.Context, req *orchestration.SpawnSessionRequest) (*orchestration.SpawnSessionResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.spawnErr != nil {
		return nil, f.spawnErr
	}
	f.seq++
	f.spawns = append(f.spawns, req)
	// The spawn is where the container learns which host directory its
	// checkout root is bind-mounted from, exactly as a real one does.
	if f.container != nil {
		f.container.mu.Lock()
		f.container.hostRoot = req.WorkDir
		// AND WHICH CREDENTIALS IT HOLDS. SSH_AUTH_SOCK is how a client key
		// reaches a container, so copying it here is what lets the container
		// refuse a clone it could not have authenticated — the whole of
		// go-quicbaas#929.
		f.container.sshAgentSocket = req.Env[orchestration.WorkspaceSigningAgentEnv]
		f.container.mu.Unlock()
	}
	resp := &orchestration.SpawnSessionResponse{SessionID: "session-" + itoa(f.seq), Backend: req.Backend}
	if !f.noContainer {
		resp.ContainerID = "container-" + itoa(f.seq)
		if f.containerID != "" {
			resp.ContainerID = f.containerID
		}
	}
	return resp, nil
}

// ExecSession makes this agent service a SessionExecutor, which is how the
// runner reaches the worker's container. A service that were not one would
// leave the runner with nowhere to commit, which is the point: there is no
// second path.
func (f *fakeAgents) ExecSession(_ context.Context, req *orchestration.ExecSessionRequest) (*orchestration.ExecResult, error) {
	f.mu.Lock()
	container := f.container
	f.mu.Unlock()
	if container == nil {
		return nil, &orchestration.SessionNotContainerizedError{SessionID: req.SessionID}
	}
	return container.exec("container-1", req.Spec)
}

var _ orchestration.SessionExecutor = (*fakeAgents)(nil)

func (f *fakeAgents) PromptSession(_ context.Context, req *orchestration.PromptSessionRequest) (*orchestration.PromptSessionResponse, error) {
	f.mu.Lock()
	onPrompt := f.onPrompt
	replyFn := f.replyFn
	f.prompts = append(f.prompts, req)
	turn := len(f.prompts)
	err := f.promptErr
	f.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if onPrompt != nil {
		onPrompt(req)
	}
	if replyFn != nil {
		resp := replyFn(turn)
		if resp != nil {
			resp.SessionID = req.SessionID
			return resp, nil
		}
	}
	return &orchestration.PromptSessionResponse{SessionID: req.SessionID}, nil
}

// answers makes the worker say text on every turn.
func (f *fakeAgents) answers(text string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.replyFn = func(int) *orchestration.PromptSessionResponse { return workerSaid(text) }
}

// workerSaid is one turn's answer, as a backend delivers it: the worker's
// text in a content block, and a stop reason saying the turn ended.
func workerSaid(text string) *orchestration.PromptSessionResponse {
	return &orchestration.PromptSessionResponse{
		Content:    []orchestration.ContentBlock{{Type: "text", Text: text}},
		StopReason: orchestration.StopReasonEndTurn,
	}
}

// CloseSession HONOURS THE CONTEXT, as a real agent service does: the close is
// a call to the daemon, and a dead context fails it before the container is
// asked to stop. Ignoring it here would make every teardown look successful in
// a test and leak a container in production — a runner that passed its own
// cancelled context to the close would be indistinguishable from one that did
// not, which is the whole property the teardown paths exist to hold.
func (f *fakeAgents) CloseSession(ctx context.Context, req *orchestration.CloseSessionRequest) (*orchestration.CloseSessionResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = append(f.closed, req.SessionID)
	return &orchestration.CloseSessionResponse{}, nil
}

func (f *fakeAgents) sessionCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.spawns)
}

func (f *fakeAgents) promptTexts() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.prompts))
	for _, req := range f.prompts {
		out = append(out, req.Prompt)
	}
	return out
}

// fakeTokens is the forge token the container clones with.
type fakeTokens struct {
	token []byte
	err   error
}

func (f *fakeTokens) ForgeToken(context.Context, string, ForgeConfig) ([]byte, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.token, nil
}

type runnerRig struct {
	*hooksRig
	forgeResolver *fakeForges
	runner        *Runner
	agents        *fakeAgents
	tokens        *fakeTokens
	container     *fakeContainer
	signing       *fakeSigningAgent
	root          string
	socketsRoot   string

	// agentService overrides the service the job's sessions go through, so a
	// test can put the worker on a REMOTE daemon instead of this process.
	// Nil leaves the local fakeAgents in place.
	agentService orchestration.AgentService

	// contextRepos are put on the job's project, so a test can assert the
	// checkout clones them beside it.
	contextRepos []orchestration.ContextRepo

	// repoURL overrides the job project's remote, so a test can run the
	// attempt against an ssh forge rather than the https one the fixture
	// carries. Empty keeps the fixture's.
	repoURL string

	// storage is the storage tier put on the job's project. The zero value is
	// what every project has today — a host bind mount that survives the
	// container — so a test only sets it to reach the ephemeral tier's own
	// behaviour, the post-turn publication.
	storage orchestration.ProjectStorage

	// audits is every audit event the run recorded, taken through the
	// engine's own callback so a run that ended before its next snapshot
	// still has its evidence.
	auditMu sync.Mutex
	audits  []orchestration.JobAuditEvent

	// snapshotMu guards snapshots, which is every status the run published,
	// taken through the engine's own PersistFn. It is how a test observes
	// what the run's Decorate put on a snapshot IN a given state — the
	// published status only ever holds the latest.
	snapshotMu sync.Mutex
	snapshots  []orchestration.JobStatus
}

// snapshotsIn is every status the run published while it was in one state. It
// is a slice rather than a count because the question the session-backed
// declaration answers is whether the worker is there for the WHOLE state, and
// a run that releases the session halfway through publishes both answers.
func (r *runnerRig) snapshotsIn(state orchestration.JobState) []orchestration.JobStatus {
	r.snapshotMu.Lock()
	defer r.snapshotMu.Unlock()
	var in []orchestration.JobStatus
	for _, status := range r.snapshots {
		if status.State == state {
			in = append(in, status)
		}
	}
	return in
}

// deliveredRun drives one issue all the way to merged and answers the rig, so
// the tests that read what the run published in a state share one arrangement.
func deliveredRun(t *testing.T) *runnerRig {
	t.Helper()
	rig := newRunnerRig(t)
	rig.rebindVerification(t, "true")
	rig.workerWrote()
	rig.verdictOnPublish("APPROVED", "lgtm")
	if err := rig.runner.Run(context.Background(), rig.job(t)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	return rig
}

// auditDetails is what the run recorded under one audit event type.
func (r *runnerRig) auditDetails(eventType string) []string {
	r.auditMu.Lock()
	defer r.auditMu.Unlock()
	var out []string
	for _, event := range r.audits {
		if event.Type == eventType {
			out = append(out, event.Details)
		}
	}
	return out
}

// newRunnerRig builds the module over in-memory stores and a scripted forge,
// with a project whose workspace is a directory this test owns.
func newRunnerRig(t *testing.T) *runnerRig {
	t.Helper()
	registerTestReviewValidator(t)
	hooks := newHooksRig(t)

	task := testTaskRecord(25)
	task.Description = issueContract
	hooks.tasks.replace(task)

	root := t.TempDir()
	projects := &fakeProjects{projects: map[string]*orchestration.Project{testProject: testOrchProject()}}
	projects.projects[testProject].Workspace = orchestration.ProjectWorkspace{Path: root}
	forges := &fakeForges{forge: hooks.forge}
	rebuilt, err := NewHooks(HooksConfig{
		Projects: projects, Bindings: hooks.store, Deliveries: hooks.store, Tasks: hooks.tasks,
		Forges:        forges,
		MergeAttempts: 3, Waiter: hooks.waiter,
		Surfacing: SurfacePolicy{RetryAttempts: 2, RetryInterval: 1, PostTimeout: 1e9},
	})
	if err != nil {
		t.Fatal(err)
	}
	hooks.hooks = rebuilt

	review, err := NewReviewValidator(hooks.store, hooks.store, forges)
	if err != nil {
		t.Fatal(err)
	}
	liveReviewValidator.Store(review)
	t.Cleanup(func() { liveReviewValidator.Store(nil) })

	container := &fakeContainer{checkout: filepath.Join(orchestration.WorkspaceCheckoutRoot, testOrchProject().Name)}
	agents := &fakeAgents{container: container}
	tokens := &fakeTokens{token: []byte("a-forge-api-token")}
	signing := &fakeSigningAgent{}
	runner, err := NewRunner(RunnerConfig{
		Hooks: rebuilt, Tokens: tokens, Signing: signing, SocketsRoot: testSocketsRoot(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	return &runnerRig{
		hooksRig: hooks, forgeResolver: forges, runner: runner, agents: agents,
		tokens: tokens, container: container, signing: signing, root: root,
		socketsRoot: runner.access.socketsRoot,
	}
}

// workerWrote is what the worker leaving uncommitted work in the checkout
// looks like from the runner's side.
func (r *runnerRig) workerWrote() {
	r.container.mu.Lock()
	defer r.container.mu.Unlock()
	r.container.changed = true
}

// job builds the JobRun the runner drives, with the operator policy that
// admits the verification commands the binding names.
func (r *runnerRig) job(t *testing.T, allowed ...string) *orchestration.JobRun {
	t.Helper()
	return r.buildJob(t, nil, "", allowed...)
}

// resumedJob is the JobRun a recovered daemon drives: the persisted status the
// interrupted run left behind, entered at the state the runner's own recovery
// table answers for it. It is built exactly as launchJob builds one.
func (r *runnerRig) resumedJob(t *testing.T, interrupted *orchestration.JobStatus, allowed ...string) *orchestration.JobRun {
	t.Helper()
	return r.buildJob(t, interrupted, RunnerResumeState(interrupted.State), allowed...)
}

// interruptedStatus is what the daemon persisted before it died: the state the
// job was in and the audit trail the snapshot entering it carried.
func (r *runnerRig) interruptedStatus(state orchestration.JobState, events ...orchestration.JobAuditEvent) *orchestration.JobStatus {
	return &orchestration.JobStatus{
		JobID:    "job-1",
		TenantID: testTenant,
		State:    state,
		Runner:   JobRunnerName,
		TaskID:   testTask,
		Attempt:  1,
		Config:   &orchestration.JobConfig{TenantID: testTenant, ProjectID: testProject, TaskID: testTask, Attempt: 1},
		Events:   events,
	}
}

// parkComments are the comments on the issue carrying THIS attempt's park
// marker — the escalation's one visible effect.
func (r *runnerRig) parkComments(t *testing.T, attempt int) []ForgeComment {
	t.Helper()
	return r.blockComments(t, attempt, orchestration.TaskBlockKindEscalated)
}

// blockComments is every comment on the issue carrying the marker for one
// attempt parked with one BLOCK KIND. The kind decides the marker, so a park a
// worker's declaration earned is found by its own outcome rather than by the
// engine's escalation.
func (r *runnerRig) blockComments(t *testing.T, attempt int, kind orchestration.TaskBlockKind) []ForgeComment {
	t.Helper()
	comments, err := r.forge.ListIssueComments(context.Background(), testRepoRef, 25)
	if err != nil {
		t.Fatal(err)
	}
	marker := blockedMarker(testTask, attempt, blockedOutcome(kind))
	var parks []ForgeComment
	for _, comment := range comments {
		if strings.Contains(comment.Body, marker) {
			parks = append(parks, comment)
		}
	}
	return parks
}

func (r *runnerRig) buildJob(t *testing.T, resumed *orchestration.JobStatus, start orchestration.JobState, allowed ...string) *orchestration.JobRun {
	t.Helper()
	guard, err := orchestration.NewExecutionGuard(append([]string{"true"}, allowed...))
	if err != nil {
		t.Fatal(err)
	}
	base, err := logger.New(logger.LogConfig{Level: "error", Output: "stdout", Format: "console"})
	if err != nil {
		t.Fatal(err)
	}
	project := testOrchProject()
	project.Workspace = orchestration.ProjectWorkspace{Path: r.root}
	project.ContextRepos = r.contextRepos
	project.Storage = r.storage
	if r.repoURL != "" {
		project.Git.RepoURL = r.repoURL
	}
	var agents orchestration.AgentService = r.agents
	if r.agentService != nil {
		agents = r.agentService
	}
	return orchestration.NewJobRun(orchestration.JobRunConfig{
		JobID:      "job-1",
		Runner:     JobRunnerName,
		Config:     &orchestration.JobConfig{TenantID: testTenant, ProjectID: testProject, TaskID: testTask, Attempt: 1},
		Project:    project,
		StartState: start,
		Resumed:    resumed,
		Agents:     agents,
		Logger:     logger.NewAdapter(base),
		Policy:     &orchestration.JobLifecyclePolicy{Guard: guard},
		AuditFn: func(eventType, details string) {
			r.auditMu.Lock()
			r.audits = append(r.audits, orchestration.JobAuditEvent{Type: eventType, Details: details})
			r.auditMu.Unlock()
		},
		PersistFn: func(status *orchestration.JobStatus) {
			r.snapshotMu.Lock()
			r.snapshots = append(r.snapshots, *status)
			r.snapshotMu.Unlock()
		},
	})
}

// rebindVerification points the binding at commands the guard admits.
func (r *runnerRig) rebindVerification(t *testing.T, commands ...string) {
	t.Helper()
	if err := r.rebind(context.Background(), func(b *ProjectBinding) { b.Verification = commands }); err != nil {
		t.Fatal(err)
	}
}

// verdictOnPublish arms the reviewer and the pipeline to answer the moment a
// pull request is opened, so the gate is decisive on its first read and no
// test waits on a poll interval.
//
// It is armed rather than applied because the pull request does not exist
// until Reviewable publishes: a test that granted an approval before the run
// would be approving nothing.
func (r *runnerRig) verdictOnPublish(state, body string) {
	r.forge.onCreatePullRequest = func(pr *ForgePullRequest) {
		r.forge.reviews[pr.Number] = []ForgeReview{{ID: 1, Reviewer: "codex", State: state, Body: body, HeadSHA: pr.HeadSHA}}
		r.forge.checks[pr.HeadSHA] = []ForgeCheckRun{{ID: "1", Name: "ci", HeadSHA: pr.HeadSHA, Status: "completed", Conclusion: "success"}}
	}
}

// approveOpenPullRequests grants the approval and the green check on every
// pull request that is already open, at its current head.
func (r *runnerRig) approveOpenPullRequests() {
	r.forge.mu.Lock()
	defer r.forge.mu.Unlock()
	for number, pr := range r.forge.prs {
		r.forge.reviews[number] = append(r.forge.reviews[number],
			ForgeReview{ID: int64(len(r.forge.reviews[number]) + 10), Reviewer: "codex", State: "APPROVED", HeadSHA: pr.HeadSHA})
		r.forge.checks[pr.HeadSHA] = []ForgeCheckRun{{ID: "1", Name: "ci", HeadSHA: pr.HeadSHA, Status: "completed", Conclusion: "success"}}
	}
}

// approveOnANewHead is what a second round actually looks like: the worker
// pushed more commits, the head moved, and the reviewer approved the NEW head.
// The outstanding changes-request stays on the old head, where the gate
// correctly stops counting it.
func (r *runnerRig) approveOnANewHead(head string) {
	r.container.mu.Lock()
	r.container.head = head
	r.container.mu.Unlock()
	r.forge.mu.Lock()
	defer r.forge.mu.Unlock()
	for number, pr := range r.forge.prs {
		pr.HeadSHA = head
		r.forge.reviews[number] = append(r.forge.reviews[number],
			ForgeReview{ID: int64(len(r.forge.reviews[number]) + 10), Reviewer: "codex", State: "APPROVED", HeadSHA: head})
		r.forge.checks[head] = []ForgeCheckRun{{ID: "1", Name: "ci", HeadSHA: head, Status: "completed", Conclusion: "success"}}
	}
}

// requestChangesOnANewHead is a round that did not converge: the worker
// pushed more commits and the reviewer refused the NEW head too. It is the
// counterpart of approveOnANewHead, and the verdict it leaves is a DISTINCT
// forge review — its own id — so the delivery records it as another round
// rather than deduplicating it against the first.
func (r *runnerRig) requestChangesOnANewHead(head, body string) {
	r.container.mu.Lock()
	r.container.head = head
	r.container.mu.Unlock()
	r.forge.mu.Lock()
	defer r.forge.mu.Unlock()
	for number, pr := range r.forge.prs {
		pr.HeadSHA = head
		r.forge.reviews[number] = append(r.forge.reviews[number],
			ForgeReview{ID: int64(len(r.forge.reviews[number]) + 10), Reviewer: "codex",
				State: "REQUEST_CHANGES", Body: body, HeadSHA: head})
		r.forge.checks[head] = []ForgeCheckRun{{ID: "1", Name: "ci", HeadSHA: head, Status: "completed", Conclusion: "success"}}
	}
}

// =============================================================================
// Shape
// =============================================================================

// TestRunner_ShapeHasNoLeadAndNoDecomposition pins the property the runner
// exists for: an issue is not a DAG, so the sequence carries no decomposing,
// scheduling or assigning state and the work happens in one session-backed
// state.
func TestRunner_ShapeHasNoLeadAndNoDecomposition(t *testing.T) {
	rig := newRunnerRig(t)
	if rig.runner.Name() != JobRunnerName {
		t.Fatalf("name = %q", rig.runner.Name())
	}
	states := rig.runner.States()
	if len(states) == 0 || states[0].State != orchestration.JobStateInitializing {
		t.Fatalf("entry state = %+v", states)
	}
	forbidden := map[orchestration.JobState]bool{
		orchestration.JobStateDecomposing: true,
		orchestration.JobStateScheduling:  true,
		orchestration.JobStateAssigning:   true,
	}
	seen := map[orchestration.JobState]bool{}
	for _, spec := range states {
		if forbidden[spec.State] {
			t.Errorf("the sdlc runner declares %q; an issue is not decomposed", spec.State)
		}
		if spec.Label == "" {
			t.Errorf("state %q has no label for a person", spec.State)
		}
		if seen[spec.State] {
			t.Errorf("state %q declared twice", spec.State)
		}
		seen[spec.State] = true
	}
	if !seen[orchestration.JobStateExecuting] || !seen[orchestration.JobStateCompleting] || !seen[orchestration.JobStateEscalating] {
		t.Fatalf("the sequence is incomplete: %+v", seen)
	}
	// ONE SESSION IS NOT ONE SESSION-BACKED STATE. WorkerSessionCount is how
	// many sessions the job holds; the states it is held THROUGH are more
	// than one, and counting the declarations against it is what let
	// reviewing go undeclared (go-quicbaas#668). What the declaration means
	// is pinned by TestRunner_TheStatesTheWorkerIsWaitedOnInAreDeclared*.
	if got := declaredSessionBackedStates(); len(got) == 0 {
		t.Fatal("no state is session-backed; the stall detector could never conclude an sdlc job had lost its worker")
	}
}

// declaredSessionBackedStates is the set the stall detector derives from this
// runner ALONE — the engine's registry merges the workflow runner's
// declarations in, and a test that read the merge would pass on the other
// runner's answer.
func declaredSessionBackedStates() map[orchestration.JobState]bool {
	backed := make(map[orchestration.JobState]bool)
	for _, spec := range RunnerStates() {
		if spec.SessionBacked {
			backed[spec.State] = true
		}
	}
	return backed
}

// TestRunner_TheStatesTheWorkerIsWaitedOnInAreDeclaredSessionBacked is the
// go-quicbaas#668 guard, from the side that catches an UNDER-declaration.
//
// Reviewing is the state the run parks the worker in: the container has
// pushed, and the daemon then waits on a forge verdict that never touches the
// worker, so nothing in the state notices a worker that has died — the next
// round would, and only after the verdict finally arrives. That wait is
// exactly what the stall detector is for, and it can only conclude about a
// state its runner declared session-backed.
//
// The declaration is read from RunnerStates(), NOT from the engine's registry:
// the workflow runner declares reviewing session-backed too and
// mergeJobStateSpec takes the disjunction, so a test that read the merge would
// have passed throughout the whole period sdlc declared the opposite.
func TestRunner_TheStatesTheWorkerIsWaitedOnInAreDeclaredSessionBacked(t *testing.T) {
	rig := deliveredRun(t)

	reviewing := rig.snapshotsIn(orchestration.JobStateReviewing)
	if len(reviewing) == 0 {
		t.Fatal("the run published no status in reviewing; this test can prove nothing")
	}
	for _, status := range reviewing {
		if len(status.SessionIDs) != WorkerSessionCount {
			t.Fatalf("a reviewing status carries %d session(s), want the worker's %d: %+v",
				len(status.SessionIDs), WorkerSessionCount, status.SessionIDs)
		}
	}
	if !declaredSessionBackedStates()[orchestration.JobStateReviewing] {
		t.Fatal("the run holds the worker session through reviewing but the runner does not declare " +
			"the state session-backed: a review parked on a worker that has died would be reported " +
			"forever instead of ended")
	}
	// The state the worker is prompted in, for the same reason and by the
	// same evidence.
	for _, status := range rig.snapshotsIn(orchestration.JobStateExecuting) {
		if len(status.SessionIDs) != WorkerSessionCount {
			t.Fatalf("an executing status carries %d session(s): %+v", len(status.SessionIDs), status.SessionIDs)
		}
	}
	if !declaredSessionBackedStates()[orchestration.JobStateExecuting] {
		t.Fatal("executing is not declared session-backed")
	}
}

// TestRunner_NoStateIsDeclaredSessionBackedAfterTheSessionIsGone is the same
// guard from the side that catches an OVER-declaration, which is the more
// expensive direction: the detector ENDS a job in a session-backed state whose
// sessions are unreachable, so a state declared past the teardown would kill
// jobs that were finishing correctly.
//
// The evidence is what the run published, not what the code reads like: a
// state is admissible only if EVERY status the run published in it still
// carried the worker. Cleanup is the counter-example the run itself produces —
// it publishes one status with the session and one without, because closing
// the session is the state's own work — and it must not be declared.
func TestRunner_NoStateIsDeclaredSessionBackedAfterTheSessionIsGone(t *testing.T) {
	rig := deliveredRun(t)

	for state := range declaredSessionBackedStates() {
		published := rig.snapshotsIn(state)
		if len(published) == 0 {
			t.Errorf("%s is declared session-backed but the run published no status in it; "+
				"the detector would judge a state nothing is known about", state)
			continue
		}
		for _, status := range published {
			if len(status.SessionIDs) == 0 {
				t.Errorf("%s is declared session-backed but the run published a status in it with no "+
					"session: a job there would be ended for having lost a worker it had already released", state)
			}
		}
	}

	// The counter-example, stated so a blanket declaration cannot pass.
	cleanup := rig.snapshotsIn(orchestration.JobStateCleanup)
	released := false
	for _, status := range cleanup {
		if len(status.SessionIDs) == 0 {
			released = true
		}
	}
	if !released {
		t.Fatal("cleanup never published a status without the session; this test no longer has its counter-example")
	}
	if declaredSessionBackedStates()[orchestration.JobStateCleanup] {
		t.Error("cleanup is declared session-backed, but it is where the session is closed")
	}
}

// TestRunner_ResumeState pins the recovery table STATE BY STATE. Each answer
// is a claim about what is already true on the forge when the daemon comes
// back, and a catch-all is how #626 sent an escalated job — parked precisely
// so a person would look at it — back through its whole pipeline.
func TestRunner_ResumeState(t *testing.T) {
	rig := newRunnerRig(t)
	for interrupted, want := range map[orchestration.JobState]orchestration.JobState{
		// Nothing reached the forge and the container is gone; the only
		// path back to a worker is a fresh spawn.
		orchestration.JobStateInitializing: orchestration.JobStateInitializing,
		orchestration.JobStateExecuting:    orchestration.JobStateInitializing,
		orchestration.JobStateValidating:   orchestration.JobStateInitializing,
		orchestration.JobStateRedelegating: orchestration.JobStateInitializing,
		// The head is on the forge; what is left is the daemon's own half.
		orchestration.JobStateReviewing: orchestration.JobStateReviewing,
		// Head-bound and idempotent.
		orchestration.JobStateCompleting: orchestration.JobStateCompleting,
		// The merge and the post-merge steps are done; teardown needs no
		// session.
		orchestration.JobStateCleanup: orchestration.JobStateCleanup,
		// The escalation FINISHES rather than re-runs.
		orchestration.JobStateEscalating: orchestration.JobStateEscalating,
	} {
		if got := rig.runner.ResumeState(interrupted); got != want {
			t.Errorf("%q resumed at %q, want %q", interrupted, got, want)
		}
	}
	for _, spec := range RunnerStates() {
		if _, ok := runnerResumeStates[spec.State]; !ok {
			t.Errorf("%q is a declared state with no recovery answer of its own", spec.State)
		}
	}
	if got := RunnerResumeState(orchestration.JobStateDecomposing); got != orchestration.JobStateInitializing {
		t.Errorf("a state this runner never declares resumed at %q, want initializing", got)
	}
	// A terminal answer is not an option for escalating: resumeJob never
	// launches a run for one, so the park reason would never reach the issue.
	if orchestration.IsTerminalJobState(rig.runner.ResumeState(orchestration.JobStateEscalating)) {
		t.Error("escalating resumed at a terminal state; the park would never be delivered")
	}
}

// TestRunner_ValidateRefusesWhatItCannotDrive: an sdlc job is an attempt at a
// task, and the refusal happens before a goroutine or a lease exists.
func TestRunner_ValidateRefusesWhatItCannotDrive(t *testing.T) {
	rig := newRunnerRig(t)
	ctx := context.Background()
	for name, cfg := range map[string]*orchestration.JobConfig{
		"nil":        nil,
		"no task":    {TenantID: testTenant, ProjectID: testProject},
		"no project": {TenantID: testTenant, TaskID: testTask},
	} {
		if err := rig.runner.Validate(ctx, cfg); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("%s: err = %v, want ErrInvalidInput", name, err)
		}
	}
	if err := rig.runner.Validate(ctx, &orchestration.JobConfig{TenantID: testTenant, ProjectID: testProject, TaskID: testTask}); err != nil {
		t.Fatalf("a bound attempt was refused: %v", err)
	}
}

// TestNewRunner_RefusesASocketsRootNoSocketCouldBindUnder is the startup
// refusal go-quicbaas#628 asked for.
//
// The module is built at boot, so a sockets root past the kernel's sun_path
// budget stops the daemon with a message naming the root and the limit — the
// alternative is a deployment that starts, looks configured, and fails every
// delivery at initializing with a listen error that names nothing.
func TestNewRunner_RefusesASocketsRootNoSocketCouldBindUnder(t *testing.T) {
	hooks := newHooksRig(t)
	base := testSocketsRoot(t)
	long := filepath.Join(base, strings.Repeat("d", MaxSigningSocketRoot+1-len(base)-1))
	_, err := NewRunner(RunnerConfig{
		Hooks: hooks.hooks, Tokens: &fakeTokens{}, Signing: &fakeSigningAgent{}, SocketsRoot: long,
	})
	var overLong *SigningSocketRootError
	if !errors.As(err, &overLong) {
		t.Fatalf("NewRunner err = %v, want *SigningSocketRootError", err)
	}
	if overLong.Root != long || overLong.Limit != MaxSigningSocketPath {
		t.Errorf("the refusal names root %q limit %d", overLong.Root, overLong.Limit)
	}
}

// TestNewRunner_SweepsTheSessionsAKilledPredecessorLeft is go-quicbaas#633
// from the runner's side.
//
// Building the runner is the daemon's only chance to clean the sockets root:
// it is the one moment the daemon knows the root and has nothing of its own
// bound under it. A predecessor killed mid-attempt left a session directory
// there, the root outlives the daemon, and nothing else would ever remove it.
//
// The removal is reported rather than logged here, because the module holds
// no logger: the host that started it says so next to the module.
func TestNewRunner_SweepsTheSessionsAKilledPredecessorLeft(t *testing.T) {
	hooks := newHooksRig(t)
	root := testSocketsRoot(t)
	orphan := deadSigningSession(t, root)

	runner, err := NewRunner(RunnerConfig{
		Hooks: hooks.hooks, Tokens: &fakeTokens{}, Signing: &fakeSigningAgent{}, SocketsRoot: root,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	sweep := runner.SocketSweep()
	if len(sweep.Removed) != 1 || sweep.Removed[0] != filepath.Base(orphan) {
		t.Fatalf("SocketSweep().Removed = %v, want just %q", sweep.Removed, filepath.Base(orphan))
	}
	if sweep.Root != filepath.Clean(root) {
		t.Errorf("SocketSweep().Root = %q, want %q", sweep.Root, filepath.Clean(root))
	}
	if _, statErr := os.Stat(orphan); !os.IsNotExist(statErr) {
		t.Errorf("the predecessor's session survived the runner's construction: %v", statErr)
	}

	// A runner built on a root that has nothing to sweep reports nothing, so
	// the host never logs a sweep that did not happen.
	quiet, err := NewRunner(RunnerConfig{
		Hooks: hooks.hooks, Tokens: &fakeTokens{}, Signing: &fakeSigningAgent{}, SocketsRoot: root,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	if got := quiet.SocketSweep(); len(got.Removed) != 0 {
		t.Errorf("a second runner swept %v, but the first left nothing behind", got.Removed)
	}
}

// TestNewRunner_RefusesAnIncompleteConfiguration: every collaborator is
// required, and the default checkout root is the engine's.
func TestNewRunner_RefusesAnIncompleteConfiguration(t *testing.T) {
	hooks := newHooksRig(t)
	sockets := testSocketsRoot(t)
	for name, cfg := range map[string]RunnerConfig{
		"no hooks":            {Tokens: &fakeTokens{}, Signing: &fakeSigningAgent{}, SocketsRoot: sockets},
		"no tokens":           {Hooks: hooks.hooks, Signing: &fakeSigningAgent{}, SocketsRoot: sockets},
		"no signing":          {Hooks: hooks.hooks, Tokens: &fakeTokens{}, SocketsRoot: sockets},
		"no sockets root":     {Hooks: hooks.hooks, Tokens: &fakeTokens{}, Signing: &fakeSigningAgent{}},
		"relative sockets":    {Hooks: hooks.hooks, Tokens: &fakeTokens{}, Signing: &fakeSigningAgent{}, SocketsRoot: "sockets"},
		"sockets root is dot": {Hooks: hooks.hooks, Tokens: &fakeTokens{}, Signing: &fakeSigningAgent{}, SocketsRoot: "."},
	} {
		if _, err := NewRunner(cfg); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("%s: err = %v, want ErrInvalidInput", name, err)
		}
	}
	runner, err := NewRunner(RunnerConfig{
		Hooks: hooks.hooks, Tokens: &fakeTokens{}, Signing: &fakeSigningAgent{}, SocketsRoot: sockets,
	})
	if err != nil {
		t.Fatal(err)
	}
	if runner.access.checkoutRoot != orchestration.WorkspaceCheckoutRoot {
		t.Fatalf("checkout root = %q, want the engine's default", runner.access.checkoutRoot)
	}
	if runner.access.socketsRoot != sockets {
		t.Fatalf("sockets root = %q, want %q", runner.access.socketsRoot, sockets)
	}
	pinned, err := NewRunner(RunnerConfig{
		Hooks: hooks.hooks, Tokens: &fakeTokens{}, Signing: &fakeSigningAgent{},
		SocketsRoot: sockets, CheckoutRoot: "/srv/work",
	})
	if err != nil {
		t.Fatal(err)
	}
	if pinned.access.checkoutRoot != "/srv/work" {
		t.Fatalf("checkout root = %q", pinned.access.checkoutRoot)
	}
	if err := runner.Run(context.Background(), nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("a nil job was accepted: %v", err)
	}
}

// =============================================================================
// The delivery
// =============================================================================

// TestRunner_DeliversTheIssueThroughExactlyOneSession is the property the
// whole runner exists for.
//
// ONE session, spawned once. The issue contract reaches the worker VERBATIM —
// not an LLM's paraphrase of it framed as "Execute the following task", which
// is what the lead-driven loop handed a worker. And the container is handed a
// workspace spec naming the project, its branch plan and a forge token file,
// because the daemon clones nothing.
func TestRunner_DeliversTheIssueThroughExactlyOneSession(t *testing.T) {
	rig := newRunnerRig(t)
	rig.rebindVerification(t, "true")
	rig.workerWrote()
	rig.verdictOnPublish("APPROVED", "lgtm")

	job := rig.job(t)
	if err := rig.runner.Run(context.Background(), job); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got := rig.agents.sessionCount(); got != WorkerSessionCount {
		t.Fatalf("sessions = %d, want %d", got, WorkerSessionCount)
	}
	prompts := rig.agents.promptTexts()
	if len(prompts) != 1 {
		t.Fatalf("prompts = %d, want one turn on the first round", len(prompts))
	}
	if !strings.Contains(prompts[0], issueContract) {
		t.Fatalf("the worker was not handed the issue contract verbatim:\n%s", prompts[0])
	}
	if strings.Contains(prompts[0], "Execute the following task") {
		t.Fatal("the worker was handed a paraphrase framing rather than the issue")
	}

	spawn := rig.agents.spawns[0]
	if spawn.SystemPrompt != WorkerCharter() {
		t.Error("the worker was spawned without the module charter as its system prompt")
	}
	if !spawn.Persistent {
		t.Error("the worker session is not persistent; a review round would lose its context")
	}
	if spawn.Backend != "claude" || spawn.Model != "claude-opus-5" {
		t.Errorf("worker spawned as %q/%q, want the binding's", spawn.Backend, spawn.Model)
	}
	if !strings.HasPrefix(spawn.WorkDir, rig.root) {
		t.Errorf("work dir %q is not under the project's workspace", spawn.WorkDir)
	}

	// The container is told what to clone and given the credentials to clone
	// with; no credential value is on the request itself.
	specPath := spawn.Env[orchestration.WorkspaceSpecEnv]
	if specPath != filepath.Join(orchestration.WorkspaceSpecDir, orchestration.WorkspaceSpecFile) {
		t.Fatalf("spec env = %q", specPath)
	}
	var specDir string
	for _, mount := range spawn.ExtraMounts {
		if mount.Target == orchestration.WorkspaceSpecDir {
			specDir = mount.Source
			if !mount.ReadOnly {
				t.Error("the workspace spec is mounted writable")
			}
		}
	}
	if specDir == "" {
		t.Fatalf("no workspace spec was mounted: %+v", spawn.ExtraMounts)
	}
	for _, value := range spawn.Env {
		if strings.Contains(value, "a-forge-api-token") {
			t.Fatal("the forge token value reached the container's environment")
		}
	}

	delivery, err := rig.store.GetDelivery(context.Background(), testTenant, testTask)
	if err != nil {
		t.Fatal(err)
	}
	if delivery.Branch != "issue/25-fix-the-thing" || delivery.PullRequest.State != PullRequestMerged {
		t.Fatalf("delivery = %+v, want the branch merged", delivery)
	}
	if delivery.WorkDir != filepath.Join(orchestration.WorkspaceCheckoutRoot, "api") {
		t.Errorf("delivery records %q, want the directory inside the container", delivery.WorkDir)
	}
	if labels := rig.forge.issueLabels(25); !containsLabel(labels, "factory:merged") {
		t.Errorf("issue labels = %v, want the change shown as merged", labels)
	}
	if len(rig.agents.closed) != 1 {
		t.Errorf("the worker session was not closed: %v", rig.agents.closed)
	}
	if _, err := os.Stat(specDir); !os.IsNotExist(err) {
		t.Errorf("the materialized workspace spec outlived the job: %v", err)
	}
	if status := job.Status(); status == nil || status.State != orchestration.JobStateCompleted {
		t.Fatalf("final status = %+v", status)
	}
}

// TestRunner_WorkspaceSpecNamesTheProjectAndItsContextRepos: what the
// container is told to clone is the project plus every context repository the
// project names, on the planned branch.
func TestRunner_WorkspaceSpecNamesTheProjectAndItsContextRepos(t *testing.T) {
	rig := newRunnerRig(t)
	rig.rebindVerification(t, "true")
	rig.workerWrote()
	rig.verdictOnPublish("APPROVED", "lgtm")

	job := rig.job(t)
	job.Project.ContextRepos = []orchestration.ContextRepo{{Name: "handbook", RepoURL: "https://forge.test/acme/handbook.git", Ref: "main"}}
	var captured orchestration.WorkspaceSpec
	if err := rig.runner.Run(context.Background(), job); err != nil {
		t.Fatalf("Run: %v", err)
	}
	// The spec directory is removed with the job, so it is read from the copy
	// the mount named while the container still had it: the runner writes it
	// before the spawn, and the spawn is what the fake recorded.
	spawn := rig.agents.spawns[0]
	for _, mount := range spawn.ExtraMounts {
		if mount.Target != orchestration.WorkspaceSpecDir {
			continue
		}
		if raw, err := os.ReadFile(filepath.Join(mount.Source, orchestration.WorkspaceSpecFile)); err == nil {
			if err := json.Unmarshal(raw, &captured); err != nil {
				t.Fatal(err)
			}
		}
	}
	if captured.Project.Name != "" && captured.Project.Name != "api" {
		t.Fatalf("spec project = %+v", captured.Project)
	}
	if spawn.ContainerImage != "" {
		t.Errorf("a binding naming no image asked for one: %q", spawn.ContainerImage)
	}
	if spawn.SandboxMode != orchestration.SandboxModeDefault {
		t.Errorf("sandbox mode = %v, want the operator's own policy", spawn.SandboxMode)
	}
}

// TestRunner_TheBindingsModulePolicyReachesTheWorkersGo: a binding naming a
// private-module prefix puts GOPRIVATE and the forge rewrite in the worker's
// environment, derived from the project's own remote; one naming none leaves
// the environment as the image ships it.
func TestRunner_TheBindingsModulePolicyReachesTheWorkersGo(t *testing.T) {
	rig := newRunnerRig(t)
	rig.repoURL = sshForgeRepoURL
	if err := rig.rebind(context.Background(), func(b *ProjectBinding) {
		b.Verification = []string{"true"}
		b.Forge.SSHHostKey = testSSHHostKey
		b.Modules.RewritePrefix = "https://github.com/acme/"
	}); err != nil {
		t.Fatal(err)
	}
	rig.workerWrote()
	rig.verdictOnPublish("APPROVED", "lgtm")
	if err := rig.runner.Run(context.Background(), rig.job(t)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	spawn := rig.agents.spawns[0]
	if got := spawn.Env["GOPRIVATE"]; got != "github.com/acme/*" {
		t.Errorf("GOPRIVATE = %q", got)
	}
	if got := spawn.Env["GIT_CONFIG_KEY_0"]; got != "url.ssh://git@forge.test:2222/acme/.insteadOf" {
		t.Errorf("GIT_CONFIG_KEY_0 = %q", got)
	}
	if got := spawn.Env["GIT_CONFIG_VALUE_0"]; got != "https://github.com/acme/" {
		t.Errorf("GIT_CONFIG_VALUE_0 = %q", got)
	}

	plain := newRunnerRig(t)
	plain.rebindVerification(t, "true")
	plain.workerWrote()
	plain.verdictOnPublish("APPROVED", "lgtm")
	if err := plain.runner.Run(context.Background(), plain.job(t)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, k := range []string{"GOPRIVATE", "GIT_CONFIG_COUNT", "GIT_CONFIG_KEY_0", "GIT_CONFIG_VALUE_0"} {
		if v, ok := plain.agents.spawns[0].Env[k]; ok {
			t.Errorf("a binding naming no modules set %s=%q", k, v)
		}
	}
}

// TestRunner_TheBindingsImageSandboxesTheWorker: naming an image requires the
// sandbox, because the container is where the checkout is made, verified and
// published.
func TestRunner_TheBindingsImageSandboxesTheWorker(t *testing.T) {
	rig := newRunnerRig(t)
	rig.rebindVerification(t, "true")
	if err := rig.rebind(context.Background(), func(b *ProjectBinding) {
		b.Verification = []string{"true"}
		b.Worker.Image = "registry.test/sdlc-worker:1"
	}); err != nil {
		t.Fatal(err)
	}
	rig.workerWrote()
	rig.verdictOnPublish("APPROVED", "lgtm")
	if err := rig.runner.Run(context.Background(), rig.job(t)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	spawn := rig.agents.spawns[0]
	if spawn.ContainerImage != "registry.test/sdlc-worker:1" {
		t.Errorf("container image = %q", spawn.ContainerImage)
	}
	if spawn.SandboxMode != orchestration.SandboxModeRequired {
		t.Errorf("sandbox mode = %v, want required", spawn.SandboxMode)
	}
	// AND IT GIVES THE WORKER FULL CONTROL OF THAT CONTAINER (go-quicbaas#927).
	// The container is the isolation boundary; inside it the worker has to build
	// what it wrote, run the binding's verification commands and install any
	// toolchain the image does not carry. Handed acceptEdits it could write
	// files and run nothing, and the measured result was an attempt that spent
	// 13m46s on six blocks of prose and made zero tool calls.
	if !slices.Contains(spawn.ExtraArgs, orchestration.WorkerFullControlFlag) {
		t.Errorf("the worker cannot run a command in its own container: %v", spawn.ExtraArgs)
	}
	if slices.Contains(spawn.ExtraArgs, "--permission-mode") {
		t.Errorf("the worker was rationed inside its container: %v", spawn.ExtraArgs)
	}
}

// =============================================================================
// Rounds
// =============================================================================

// TestRunner_VerificationFailureGoesBackToTheWorkerWithTheOutput: a failed
// verification is a round, and what the command said is what the worker is
// told.
func TestRunner_VerificationFailureGoesBackToTheWorkerWithTheOutput(t *testing.T) {
	rig := newRunnerRig(t)
	rig.rebindVerification(t, "false")
	if err := rig.rebind(context.Background(), func(b *ProjectBinding) {
		b.Verification = []string{"false"}
		b.MaxReviewRounds = 1
		b.Forge.Labels[LabelEscalated] = "factory:escalated"
	}); err != nil {
		t.Fatal(err)
	}
	rig.workerWrote()

	err := rig.runner.Run(context.Background(), rig.job(t, "false"))
	var escalated *EscalatedError
	if !errors.As(err, &escalated) {
		t.Fatalf("Run err = %v, want an escalation", err)
	}
	if !strings.Contains(escalated.Reason, "verification failed") {
		t.Errorf("escalation reason = %q", escalated.Reason)
	}
	prompts := rig.agents.promptTexts()
	if len(prompts) != 2 {
		t.Fatalf("prompts = %d, want the first turn and one round", len(prompts))
	}
	if !strings.Contains(prompts[1], issueContract) {
		t.Error("the round dropped the issue contract")
	}
	if !strings.Contains(prompts[1], "false") {
		t.Errorf("the round did not carry the failing command:\n%s", prompts[1])
	}
	if rig.agents.sessionCount() != WorkerSessionCount {
		t.Fatalf("a round spawned a second session: %d", rig.agents.sessionCount())
	}
	if comments := rig.forge.commentsOn(25); len(comments) == 0 {
		t.Error("the escalation was not posted to the issue")
	}
	if labels := rig.forge.issueLabels(25); !containsLabel(labels, "factory:escalated") && !containsLabel(labels, "factory:blocked") {
		t.Errorf("issue labels = %v, want the park shown", labels)
	}
}

// TestRunner_TheParkCarriesATypedUnblockCondition is the producer half of
// #759: the escalation states what would clear it in a form the sweep can
// read, and the comment a person reads is derived from that.
//
// Review that did not converge is a call reserved for the owner, so the
// condition says owner_ruling rather than leaving prose the sweep would have
// to report as undiagnosed.
func TestRunner_TheParkCarriesATypedUnblockCondition(t *testing.T) {
	rig := newRunnerRig(t)
	rig.rebindVerification(t, "false")
	if err := rig.rebind(context.Background(), func(b *ProjectBinding) {
		b.Verification = []string{"false"}
		b.MaxReviewRounds = 1
		b.Forge.Labels[LabelEscalated] = "factory:escalated"
	}); err != nil {
		t.Fatal(err)
	}
	rig.workerWrote()

	err := rig.runner.Run(context.Background(), rig.job(t, "false"))
	var escalated *EscalatedError
	if !errors.As(err, &escalated) {
		t.Fatalf("Run err = %v, want an escalation", err)
	}
	condition, detail, parseErr := ParseBlockedReason(escalated.Reason)
	if parseErr != nil {
		t.Fatalf("the park reason carries no typed condition (%v): %q", parseErr, escalated.Reason)
	}
	if condition.Kind != UnblockOwnerRuling {
		t.Fatalf("the park condition is %s, want owner_ruling", condition.Kind)
	}
	if !strings.Contains(detail, "verification failed") {
		t.Fatalf("the detail lost what actually happened: %q", detail)
	}

	comments := rig.forge.commentsOn(25)
	if len(comments) == 0 {
		t.Fatal("the escalation was not posted to the issue")
	}
	body := comments[len(comments)-1].Body
	if !strings.Contains(body, "**Cleared by:** the owner") {
		t.Fatalf("the comment does not name who clears the park:\n%s", body)
	}
	if strings.Contains(body, unblockMarkerOpen) {
		t.Fatalf("the machine marker reached the issue:\n%s", body)
	}
}

// TestRunner_ChangesRequestedRunsAnotherRound: the findings reach the worker,
// the round is counted, and the same session carries the conversation.
func TestRunner_ChangesRequestedRunsAnotherRound(t *testing.T) {
	rig := newRunnerRig(t)
	rig.rebindVerification(t, "true")
	rig.workerWrote()

	rig.verdictOnPublish("REQUEST_CHANGES", "keep the fix inside pkg/thing")
	round := 0
	rig.agents.onPrompt = func(*orchestration.PromptSessionRequest) {
		round++
		if round > 1 {
			rig.approveOnANewHead("head-round-2")
		}
	}
	if err := rig.runner.Run(context.Background(), rig.job(t)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	prompts := rig.agents.promptTexts()
	if len(prompts) != 2 {
		t.Fatalf("prompts = %d, want the first turn and one review round", len(prompts))
	}
	if !strings.Contains(prompts[1], "Round 1 findings") {
		t.Errorf("the round is not announced to the worker:\n%s", prompts[1])
	}
	if rig.agents.sessionCount() != WorkerSessionCount {
		t.Fatalf("a review round spawned a second session: %d", rig.agents.sessionCount())
	}
}

// TestRunner_TheLastReviewRoundReachesTheJobPage: an attempt that ends on a
// refusal publishes that refusal.
//
// Reviewable publishes the delivery BEFORE the gate runs, so the round the
// gate records is one publish behind. An approval is republished by Complete
// and an intermediate refusal by the next round's Reviewable, but the LAST
// refusal — the one the attempt escalates on — had no publish after it, so
// the findings a person needs in order to rule on the park never reached the
// job page at all (go-quicbaas#796).
func TestRunner_TheLastReviewRoundReachesTheJobPage(t *testing.T) {
	rig := newRunnerRig(t)
	if err := rig.rebind(context.Background(), func(b *ProjectBinding) {
		b.Verification = []string{"true"}
		b.MaxReviewRounds = 1
		b.Forge.Labels[LabelEscalated] = "factory:escalated"
	}); err != nil {
		t.Fatal(err)
	}
	rig.workerWrote()
	rig.verdictOnPublish("REQUEST_CHANGES", "keep the fix inside pkg/thing")

	// Round 2 pushes a new head and is refused again. That second refusal is
	// the attempt's last word, and the round the budget then escalates on.
	round := 0
	rig.agents.onPrompt = func(*orchestration.PromptSessionRequest) {
		round++
		if round > 1 {
			rig.requestChangesOnANewHead("head-round-2", "the test still does not fail without the fix")
		}
	}

	err := rig.runner.Run(context.Background(), rig.job(t))
	var escalated *EscalatedError
	if !errors.As(err, &escalated) {
		t.Fatalf("Run err = %v, want an escalation once the rounds are spent", err)
	}

	// The engine's own reader is the contract: the job page decodes the task
	// artifacts and never this module's Delivery.
	delivery, decodeErr := orchestration.JobDeliveryFromArtifacts(rig.tasks.submitted(testTask), 1)
	if decodeErr != nil {
		t.Fatalf("JobDeliveryFromArtifacts: %v", decodeErr)
	}
	if delivery == nil {
		t.Fatal("the attempt published no delivery at all")
	}
	if len(delivery.Reviews) != 2 {
		t.Fatalf("published rounds = %+v, want both refusals", delivery.Reviews)
	}
	last := delivery.Reviews[len(delivery.Reviews)-1]
	if last.Verdict != VerdictRequestChanges.String() {
		t.Errorf("last verdict = %q, want the refusal the attempt ended on", last.Verdict)
	}
	if !strings.Contains(last.Findings, "the test still does not fail without the fix") {
		t.Errorf("the last round's findings did not reach the page: %+v", last)
	}
	if last.HeadSHA != "head-round-2" {
		t.Errorf("last round head = %q, want the head it was refused on", last.HeadSHA)
	}
}

// TestRunner_AnApprovedReviewRoundIsPublishedToo: the same republish on the
// path that passes, so the round that approved the change is on the page
// rather than only in the merge's own record.
func TestRunner_AnApprovedReviewRoundIsPublishedToo(t *testing.T) {
	rig := newRunnerRig(t)
	rig.rebindVerification(t, "true")
	rig.workerWrote()
	rig.verdictOnPublish("APPROVED", "")

	if err := rig.runner.Run(context.Background(), rig.job(t)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	delivery, err := orchestration.JobDeliveryFromArtifacts(rig.tasks.submitted(testTask), 1)
	if err != nil {
		t.Fatalf("JobDeliveryFromArtifacts: %v", err)
	}
	if delivery == nil || len(delivery.Reviews) != 1 {
		t.Fatalf("published rounds = %+v, want the approval", delivery)
	}
	if delivery.Reviews[0].Verdict != VerdictApprove.String() {
		t.Errorf("verdict = %q, want the approval", delivery.Reviews[0].Verdict)
	}
}

// =============================================================================
// Strategies
// =============================================================================

// TestRunner_CarveOutLandsWithoutAPullRequest is the trunk-based direct-push
// path: a change class the strategy exempted never reaches the review gate.
func TestRunner_CarveOutLandsWithoutAPullRequest(t *testing.T) {
	rig := newRunnerRig(t)
	if err := rig.rebind(context.Background(), func(b *ProjectBinding) {
		b.Strategy = TrunkBasedName
		b.Verification = []string{"true"}
	}); err != nil {
		t.Fatal(err)
	}
	rig.forge.putIssue(25, "Fix the thing", "task:ready", "docs")
	rig.workerWrote()

	if err := rig.runner.Run(context.Background(), rig.job(t)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(rig.forge.prs) != 0 {
		t.Fatalf("a carve-out opened a pull request: %v", rig.forge.prs)
	}
	if len(rig.container.pushes) == 0 {
		t.Fatal("nothing was pushed")
	}
	if got := rig.container.pushes[0].RemoteBranch; got != "main" {
		t.Fatalf("pushed to %q, want the trunk", got)
	}
}

// TestRunner_GitflowHotfixBackMergesAfterTheMerge: the strategy DECLARES the
// back-merge and the runner is what executes it, through the forge.
func TestRunner_GitflowHotfixBackMergesAfterTheMerge(t *testing.T) {
	rig := newRunnerRig(t)
	if err := rig.rebind(context.Background(), func(b *ProjectBinding) {
		b.Strategy = GitFlowName
		b.Verification = []string{"true"}
	}); err != nil {
		t.Fatal(err)
	}
	rig.forge.putIssue(25, "Fix the thing", "task:ready", "hotfix")
	rig.workerWrote()
	rig.verdictOnPublish("APPROVED", "lgtm")

	if err := rig.runner.Run(context.Background(), rig.job(t)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	var backMerge *ForgePullRequest
	for _, pr := range rig.forge.pullRequests() {
		if pr.BaseRef == GitFlowDevelopBranch && pr.HeadRef == "main" {
			backMerge = pr
		}
	}
	if backMerge == nil {
		t.Fatalf("no back-merge was opened: %+v", rig.forge.pullRequests())
	}
	if len(rig.forge.merges) != 2 {
		t.Fatalf("merges = %d, want the hotfix and its back-merge", len(rig.forge.merges))
	}
}

// TestRunner_AnUnknownPostMergeStepIsRefused: a step nothing executes is a
// refusal, never a skip — a back-merge silently not performed is a fix lost
// from the development line.
func TestRunner_AnUnknownPostMergeStepIsRefused(t *testing.T) {
	rig := newRunnerRig(t)
	run := &runnerRun{runner: rig.runner, job: rig.job(t)}
	ctx := context.Background()
	if err := run.resolve(ctx); err != nil {
		t.Fatal(err)
	}
	err := run.runStep(ctx, Step{Kind: "no-such-step", Source: "a", Target: "b"})
	var unknown *UnknownPostMergeStepError
	if !errors.As(err, &unknown) || !errors.Is(err, ErrRunner) {
		t.Fatalf("err = %v, want UnknownPostMergeStepError", err)
	}
}

// =============================================================================
// Refusals and recovery
// =============================================================================

// TestRunner_AProjectWithNoWorkspaceIsParkedForAnOperatorAndNotCharged: the
// worker has nowhere to work, and guessing a directory would put a tenant's
// checkout somewhere the operator's mount policy never admitted.
//
// IT IS THE OPERATOR'S FAULT AND IS ATTRIBUTED AS ONE. Measured on the burn-in
// node: this exact refusal was raised three times in one minute and charged
// against the task each time (go-quicbaas#908). The park carries the workspace
// fault class, so the engine refunds the attempt and the issue tells whoever
// runs the fleet what to fix.
func TestRunner_AProjectWithNoWorkspaceIsParkedForAnOperatorAndNotCharged(t *testing.T) {
	rig := newRunnerRig(t)
	rig.rebindVerification(t, "true")
	job := rig.job(t)
	job.Project.Workspace.Path = ""
	err := rig.runner.Run(context.Background(), job)
	if !errors.Is(err, ErrWorkspaceUnfit) && !errors.Is(err, ErrRunner) {
		t.Fatalf("err = %v, want the run to end on the workspace fault", err)
	}
	if rig.agents.sessionCount() != 0 {
		t.Fatal("a worker was spawned for a project with nowhere to work")
	}
	parked := job.Parked()
	if parked == nil {
		t.Fatal("a project with nowhere to work was not parked for anybody")
	}
	if parked.FaultClass != orchestration.FaultWorkspaceUnfit {
		t.Fatalf("park fault class = %q, want %q", parked.FaultClass, orchestration.FaultWorkspaceUnfit)
	}
	if parked.Kind != orchestration.TaskBlockKindBlocked {
		t.Fatalf("park kind = %q, want blocked", parked.Kind)
	}
	if !strings.Contains(parked.Message, "no tokens were spent") {
		t.Fatalf("the park does not say the fault cost nothing: %q", parked.Message)
	}
	condition, err := ParseUnblockCondition(parked.Message)
	if err != nil || condition.Kind != UnblockOperatorRepair {
		t.Fatalf("condition = %+v, err = %v, want operator_repair", condition, err)
	}
}

// TestRunner_AJobWithNoProjectRecordIsParkedForAnOperatorAndNotCharged: the
// workspace spec is described from the project, so a job that carries none
// cannot describe one — and that is the FACTORY's fault, not the work's.
//
// THIS TEST USED TO ASSERT THE DEFECT. It required ErrInvalidInput, which is
// what requireProject returned, and ErrInvalidInput is not ErrWorkspaceUnfit —
// so prepareWorkspace never parked, the error left Run as a plain terminal
// failure, classifyJobFailure did not recognise it, and the task was charged an
// attempt for a fault it did not commit. Measured on the burn-in node: two jobs
// resumed after a daemon restart, both reported as "charged as a failure of the
// work" for a project record the resume path had failed to read.
//
// The assertion is now the same one its sibling above makes, because the two
// faults are the same fault: a workspace nothing could work in, provable before
// a container existed, refunded and parked naming the repair.
func TestRunner_AJobWithNoProjectRecordIsParkedForAnOperatorAndNotCharged(t *testing.T) {
	rig := newRunnerRig(t)
	job := rig.job(t)
	job.Project = nil
	err := rig.runner.Run(context.Background(), job)
	if !errors.Is(err, ErrRunner) {
		t.Fatalf("err = %v, want a runner refusal", err)
	}
	if rig.agents.sessionCount() != 0 {
		t.Fatal("a worker was spawned for a job with no project to describe its checkout")
	}
	parked := job.Parked()
	if parked == nil {
		t.Fatal("a job with no project record was not parked for anybody")
	}
	if parked.FaultClass != orchestration.FaultWorkspaceUnfit {
		t.Fatalf("park fault class = %q, want %q; without it the attempt is charged", parked.FaultClass, orchestration.FaultWorkspaceUnfit)
	}
	if parked.Kind != orchestration.TaskBlockKindBlocked {
		t.Fatalf("park kind = %q, want blocked", parked.Kind)
	}
	condition, err := ParseUnblockCondition(parked.Message)
	if err != nil || condition.Kind != UnblockOperatorRepair {
		t.Fatalf("condition = %+v, err = %v, want operator_repair", condition, err)
	}
	// The park is what an operator reads, so it has to name the project that was
	// absent — "the workspace is unfit" alone would send them to the checkout.
	if !strings.Contains(parked.Message, "no project record") {
		t.Fatalf("the park does not say what was missing: %q", parked.Message)
	}
	if !strings.Contains(parked.Message, "no tokens were spent") {
		t.Fatalf("the park does not say the fault cost nothing: %q", parked.Message)
	}
}

// TestRequireProject_IsAWorkspaceFault is the unit-level guard on the
// classification, independent of the runner rig: an InvalidInputError here is
// what charged two resumed jobs on the burn-in node for the factory's own fault,
// because prepareWorkspace parks on ErrWorkspaceUnfit and nothing else.
func TestRequireProject_IsAWorkspaceFault(t *testing.T) {
	err := (&ProjectRecordMissingError{TaskID: "task-1", ProjectID: "proj-1"}).Unwrap()
	var unfit, runner bool
	for _, e := range err {
		if errors.Is(e, ErrWorkspaceUnfit) {
			unfit = true
		}
		if errors.Is(e, ErrRunner) {
			runner = true
		}
	}
	if !unfit {
		t.Error("a missing project record must be ErrWorkspaceUnfit, or the attempt is charged to the work")
	}
	if !runner {
		t.Error("a missing project record must be ErrRunner")
	}
	// It must NOT be ErrInvalidInput, which is what it used to be: that sentinel
	// is what prepareWorkspace's park test does not recognise.
	if errors.Is(&ProjectRecordMissingError{}, ErrInvalidInput) {
		t.Error("ErrInvalidInput is the classification that charged the task; it must not come back")
	}
	// The message names both, because the park an operator reads is built from it.
	msg := (&ProjectRecordMissingError{TaskID: "task-1", ProjectID: "proj-1"}).Error()
	if !strings.Contains(msg, "proj-1") || !strings.Contains(msg, "task-1") {
		t.Errorf("Error() = %q, want it to name the project and the task", msg)
	}
}

// TestRunner_ATaskWithNoIssueContractIsRefused: the contract IS the work, and
// a worker prompted with nothing would invent it.
func TestRunner_ATaskWithNoIssueContractIsRefused(t *testing.T) {
	rig := newRunnerRig(t)
	rig.rebindVerification(t, "true")
	task := testTaskRecord(25)
	task.Description = ""
	rig.tasks.replace(task)
	if err := rig.runner.Run(context.Background(), rig.job(t)); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("err = %v, want ErrInvalidInput", err)
	}
}

// TestRunner_UnknownStateIsRefused: a job resumed into a state this runner
// does not drive is a refusal naming the state, not a silent restart.
func TestRunner_UnknownStateIsRefused(t *testing.T) {
	rig := newRunnerRig(t)
	job := rig.job(t)
	run := &runnerRun{runner: rig.runner, job: job}
	job.StartState = orchestration.JobStateDecomposing
	err := run.drive(context.Background())
	var unknown *UnknownRunnerStateError
	if !errors.As(err, &unknown) || unknown.State != string(orchestration.JobStateDecomposing) {
		t.Fatalf("err = %v, want UnknownRunnerStateError naming decomposing", err)
	}
}

// TestRunner_ResumingInCompletingMergesWithoutRespawningAWorker: the whole
// point of resuming there. The container is gone, the review approved a head,
// and what is left is the merge.
func TestRunner_ResumingInCompletingMergesWithoutRespawningAWorker(t *testing.T) {
	rig := newRunnerRig(t)
	ctx := context.Background()
	// An earlier attempt got as far as an approved pull request.
	if _, err := rig.hooks.Prepare(ctx, testJob(1)); err != nil {
		t.Fatal(err)
	}
	rig.publishHead(t, ctx, testPublishedHead)
	if err := rig.hooks.Reviewable(ctx, testJob(1)); err != nil {
		t.Fatal(err)
	}
	rig.approveOpenPullRequests()

	job := rig.job(t)
	job.StartState = orchestration.JobStateCompleting
	if err := rig.runner.Run(ctx, job); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rig.agents.sessionCount() != 0 {
		t.Fatal("resuming into the merge respawned a worker")
	}
	if len(rig.forge.merges) != 1 {
		t.Fatalf("merges = %d, want one", len(rig.forge.merges))
	}
}

// TestRunner_ResumingInEscalatingFinishesTheParkExactlyOnce is #626.
//
// A daemon that died between entering escalating and the Blocked hook
// completing used to restart the job at initializing: a new worker session, a
// new prompt, a refreshed pull request and a re-labelled issue, spending a
// fresh attempt's tokens on work a person had already been asked to look at.
// The escalation is not work to redo, it is work to CONCLUDE — the park
// reason reaches the issue exactly once and the job ends failed.
func TestRunner_ResumingInEscalatingFinishesTheParkExactlyOnce(t *testing.T) {
	rig := newRunnerRig(t)
	ctx := context.Background()
	if err := rig.rebind(ctx, func(b *ProjectBinding) {
		b.Forge.Labels[LabelEscalated] = "factory:escalated"
	}); err != nil {
		t.Fatal(err)
	}
	// The interrupted run got as far as planning the branch and deciding to
	// park; the reason is on the audit trail the snapshot persisted.
	if _, err := rig.hooks.Prepare(ctx, testJob(1)); err != nil {
		t.Fatal(err)
	}
	const reason = "changes requested after 3 review round(s); this change needs a person"
	interrupted := rig.interruptedStatus(orchestration.JobStateEscalating,
		orchestration.JobAuditEvent{Type: auditEscalated, Details: reason})

	var escalated *EscalatedError
	if err := rig.runner.Run(ctx, rig.resumedJob(t, interrupted)); !errors.As(err, &escalated) {
		t.Fatalf("Run err = %v, want the escalation the job ends on", err)
	}
	if escalated.Reason != reason {
		t.Errorf("reason = %q, want the one the interrupted run recorded", escalated.Reason)
	}
	if rig.agents.sessionCount() != 0 || len(rig.agents.promptTexts()) != 0 {
		t.Fatalf("the resumed escalation re-ran the pipeline: %d session(s), %d prompt(s)",
			rig.agents.sessionCount(), len(rig.agents.promptTexts()))
	}
	if len(rig.forge.prs) != 0 {
		t.Errorf("the resumed escalation refreshed a pull request: %v", rig.forge.prs)
	}
	parks := rig.parkComments(t, 1)
	if len(parks) != 1 {
		t.Fatalf("park comments = %d, want exactly one", len(parks))
	}
	if !strings.Contains(parks[0].Body, reason) {
		t.Errorf("the park does not carry the reason:\n%s", parks[0].Body)
	}
	if labels := rig.forge.issueLabels(25); !containsLabel(labels, "factory:escalated") {
		t.Errorf("issue labels = %v, want the park shown", labels)
	}

	// The daemon dies AGAIN, this time after the post. The park is already
	// on the issue, and the second resumed run adds nothing to it.
	if err := rig.runner.Run(ctx, rig.resumedJob(t, interrupted)); !errors.As(err, &escalated) {
		t.Fatalf("the second resume: err = %v, want the same escalation", err)
	}
	if parks := rig.parkComments(t, 1); len(parks) != 1 {
		t.Fatalf("park comments = %d after two resumes, want exactly one", len(parks))
	}
	if labels := rig.forge.issueLabels(25); !containsLabel(labels, "factory:escalated") {
		t.Errorf("issue labels = %v after the second resume", labels)
	}
}

// TestRunner_AParkFromAnEarlierAttemptDoesNotSilenceThisOne: the marker names
// the ATTEMPT as well as the task, so a task readmitted after a park can park
// again. A resumed run that matched on the task alone would find the earlier
// attempt's comment and post nothing.
func TestRunner_AParkFromAnEarlierAttemptDoesNotSilenceThisOne(t *testing.T) {
	rig := newRunnerRig(t)
	ctx := context.Background()
	if _, err := rig.hooks.Prepare(ctx, testJob(1)); err != nil {
		t.Fatal(err)
	}
	// Attempt one parked and the task was readmitted.
	if err := rig.hooks.Blocked(ctx, testJob(1), orchestration.BlockReason{
		Kind: orchestration.TaskBlockKindEscalated, Message: "the first attempt gave up",
	}); err != nil {
		t.Fatal(err)
	}
	// The readmitted task is attempt two, and it parks in its turn.
	job := rig.resumedJob(t, rig.interruptedStatus(orchestration.JobStateEscalating,
		orchestration.JobAuditEvent{Type: auditEscalated, Details: "the second attempt gave up"}))
	job.Config.Attempt = 2
	if err := rig.runner.Run(ctx, job); err == nil {
		t.Fatal("the resumed escalation completed the job")
	}
	parks := rig.parkComments(t, 2)
	if len(parks) != 1 {
		t.Fatalf("this attempt's park comments = %d, want exactly one", len(parks))
	}
	if !strings.Contains(parks[0].Body, "the second attempt gave up") {
		t.Errorf("the earlier attempt's park silenced this one:\n%s", parks[0].Body)
	}
}

// TestRunner_ResumingInReviewingAsksForTheVerdictOnThePublishedHead: the
// branch is pushed and the pull request is open, so the resumed run does the
// daemon's half — refresh the pull request, ask for the verdict — rather than
// re-cloning and re-executing work the forge already has.
func TestRunner_ResumingInReviewingAsksForTheVerdictOnThePublishedHead(t *testing.T) {
	rig := newRunnerRig(t)
	ctx := context.Background()
	if _, err := rig.hooks.Prepare(ctx, testJob(1)); err != nil {
		t.Fatal(err)
	}
	rig.publishHead(t, ctx, testPublishedHead)
	if err := rig.hooks.Reviewable(ctx, testJob(1)); err != nil {
		t.Fatal(err)
	}
	rig.approveOpenPullRequests()

	job := rig.resumedJob(t, rig.interruptedStatus(orchestration.JobStateReviewing))
	if job.StartState != orchestration.JobStateReviewing {
		t.Fatalf("resumed at %q, want reviewing", job.StartState)
	}
	if err := rig.runner.Run(ctx, job); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rig.agents.sessionCount() != 0 || len(rig.agents.promptTexts()) != 0 {
		t.Fatalf("the resumed review re-executed the attempt: %d session(s), %d prompt(s)",
			rig.agents.sessionCount(), len(rig.agents.promptTexts()))
	}
	if len(rig.container.pushes) != 0 {
		t.Errorf("published again from a container that no longer exists: %v", rig.container.pushes)
	}
	if len(rig.forge.merges) != 1 {
		t.Fatalf("merges = %d, want the approved head merged once", len(rig.forge.merges))
	}
	if rig.forge.merges[0].head != testPublishedHead {
		t.Errorf("merged head = %q, want the head the review approved", rig.forge.merges[0].head)
	}
}

// TestRunner_ResumingInReviewingWithNothingPublishedExecutesAgain is the other
// half of the reviewing answer: the interruption landed before the container
// pushed, so the Delivery records no head and the worker's edits went with the
// container. There is nothing to review, and the attempt is executed again.
func TestRunner_ResumingInReviewingWithNothingPublishedExecutesAgain(t *testing.T) {
	rig := newRunnerRig(t)
	ctx := context.Background()
	rig.rebindVerification(t, "true")
	rig.workerWrote()
	rig.verdictOnPublish("APPROVED", "lgtm")
	if _, err := rig.hooks.Prepare(ctx, testJob(1)); err != nil {
		t.Fatal(err)
	}

	if err := rig.runner.Run(ctx, rig.resumedJob(t, rig.interruptedStatus(orchestration.JobStateReviewing))); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rig.agents.sessionCount() != WorkerSessionCount {
		t.Fatalf("sessions = %d, want the one worker the attempt is executed again in", rig.agents.sessionCount())
	}
	if len(rig.forge.merges) != 1 {
		t.Fatalf("merges = %d, want the re-executed attempt to land", len(rig.forge.merges))
	}
}

// TestRunner_AResumedReviewAskedForAnotherRoundSpawnsAWorker: the gate on a
// resumed run can still say request_changes, and a round needs a container the
// resumed run does not hold. It goes to initializing for one rather than
// failing on a session that died with the daemon.
func TestRunner_AResumedReviewAskedForAnotherRoundSpawnsAWorker(t *testing.T) {
	rig := newRunnerRig(t)
	ctx := context.Background()
	rig.rebindVerification(t, "true")
	rig.workerWrote()
	if _, err := rig.hooks.Prepare(ctx, testJob(1)); err != nil {
		t.Fatal(err)
	}
	rig.publishHead(t, ctx, testPublishedHead)
	if err := rig.hooks.Reviewable(ctx, testJob(1)); err != nil {
		t.Fatal(err)
	}
	rig.forge.mu.Lock()
	for number, pr := range rig.forge.prs {
		rig.forge.reviews[number] = []ForgeReview{{ID: 1, Reviewer: "codex", State: "REQUEST_CHANGES",
			Body: "keep the fix inside pkg/thing", HeadSHA: pr.HeadSHA}}
		rig.forge.checks[pr.HeadSHA] = []ForgeCheckRun{{ID: "1", Name: "ci", HeadSHA: pr.HeadSHA,
			Status: "completed", Conclusion: "success"}}
	}
	rig.forge.mu.Unlock()
	rig.agents.onPrompt = func(*orchestration.PromptSessionRequest) { rig.approveOnANewHead("head-round-2") }

	if err := rig.runner.Run(ctx, rig.resumedJob(t, rig.interruptedStatus(orchestration.JobStateReviewing))); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rig.agents.sessionCount() != WorkerSessionCount {
		t.Fatalf("sessions = %d, want the one worker the round is run in", rig.agents.sessionCount())
	}
	prompts := rig.agents.promptTexts()
	if len(prompts) != 1 {
		t.Fatalf("prompts = %d, want the one round the gate asked for", len(prompts))
	}
	if !strings.Contains(prompts[0], "Round 1 findings") || !strings.Contains(prompts[0], "keep the fix inside pkg/thing") {
		t.Errorf("the round did not carry the findings:\n%s", prompts[0])
	}
	if len(rig.forge.merges) != 1 {
		t.Fatalf("merges = %d, want the approved round to land", len(rig.forge.merges))
	}
}

// TestRunner_AResumedEscalationRefusesToGuessWhetherItPosted: the ISSUE is
// what says whether the park already reached a person, and a forge that cannot
// be read is not an answer. The run fails rather than risking a second comment
// on a task a person is already looking at.
func TestRunner_AResumedEscalationRefusesToGuessWhetherItPosted(t *testing.T) {
	rig := newRunnerRig(t)
	ctx := context.Background()
	if _, err := rig.hooks.Prepare(ctx, testJob(1)); err != nil {
		t.Fatal(err)
	}
	rig.forge.listCommentsErr = errors.New("the forge is unreachable")
	if err := rig.runner.Run(ctx, rig.resumedJob(t, rig.interruptedStatus(orchestration.JobStateEscalating))); err == nil {
		t.Fatal("the resumed escalation concluded without reading the issue")
	}
	if comments := rig.forge.commentsOn(25); len(comments) != 0 {
		t.Errorf("posted without knowing whether a park was already there: %v", comments)
	}
}

// TestRunner_AResumedEscalationWithNoRecordedReasonStillReachesTheIssue: the
// reason is restored from the audit trail, and a status written without one
// still parks the task. Silence would leave the person watching the issue
// nothing at all.
func TestRunner_AResumedEscalationWithNoRecordedReasonStillReachesTheIssue(t *testing.T) {
	rig := newRunnerRig(t)
	ctx := context.Background()
	if _, err := rig.hooks.Prepare(ctx, testJob(1)); err != nil {
		t.Fatal(err)
	}
	if err := rig.runner.Run(ctx, rig.resumedJob(t, rig.interruptedStatus(orchestration.JobStateEscalating))); err == nil {
		t.Fatal("the resumed escalation completed the job")
	}
	parks := rig.parkComments(t, 1)
	if len(parks) != 1 {
		t.Fatalf("park comments = %d, want exactly one", len(parks))
	}
	if !strings.Contains(parks[0].Body, "recorded no reason") {
		t.Errorf("the park says nothing about the missing reason:\n%s", parks[0].Body)
	}
}

// TestRunner_ResumingInReviewingWithoutADeliveryIsRefused: the Delivery is what
// says whether the work reached the forge, and a run resumed without one
// cannot answer either way.
func TestRunner_ResumingInReviewingWithoutADeliveryIsRefused(t *testing.T) {
	rig := newRunnerRig(t)
	// No Prepare ran, so there is no Delivery to read a head off.
	if err := rig.runner.Run(context.Background(),
		rig.resumedJob(t, rig.interruptedStatus(orchestration.JobStateReviewing))); err == nil {
		t.Fatal("a review resumed against a delivery that does not exist")
	}
	if rig.agents.sessionCount() != 0 {
		t.Errorf("spawned a worker for a review it could not resolve: %d", rig.agents.sessionCount())
	}
}

// TestRunner_ResumingInCleanupTearsDownWithoutMergingAgain: by the time
// cleanup is the persisted state the merge and the strategy's post-merge steps
// have run, so all that remains is a teardown that needs no session.
func TestRunner_ResumingInCleanupTearsDownWithoutMergingAgain(t *testing.T) {
	rig := newRunnerRig(t)
	ctx := context.Background()
	if _, err := rig.hooks.Prepare(ctx, testJob(1)); err != nil {
		t.Fatal(err)
	}
	rig.publishHead(t, ctx, testPublishedHead)
	if err := rig.hooks.Reviewable(ctx, testJob(1)); err != nil {
		t.Fatal(err)
	}
	if err := rig.runner.Run(ctx, rig.resumedJob(t, rig.interruptedStatus(orchestration.JobStateCleanup))); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(rig.forge.merges) != 0 {
		t.Errorf("a delivered change was merged again: %v", rig.forge.merges)
	}
	if rig.agents.sessionCount() != 0 {
		t.Errorf("the teardown spawned a worker: %d", rig.agents.sessionCount())
	}
}

// containsLabel reports whether labels carries want.
func containsLabel(labels []string, want string) bool {
	for _, label := range labels {
		if label == want {
			return true
		}
	}
	return false
}

// =============================================================================
// Every collaborator's refusal reaches the job
// =============================================================================

// TestRunner_ARefusalFromAnyCollaboratorFailsTheJob walks the failures a run
// can meet and asserts each one ENDS the job rather than being logged past.
//
// A runner that continued after the token could not be read, or after the
// spawn was refused, would leave a job in a state no person could act on: the
// issue labelled in progress, no worker, and nothing on the record saying so.
func TestRunner_ARefusalFromAnyCollaboratorFailsTheJob(t *testing.T) {
	cause := errors.New("the collaborator refused")
	cases := map[string]func(rig *runnerRig){
		"the issue is gone": func(rig *runnerRig) {
			rig.forge.issues = map[int]*ForgeIssue{}
		},
		"the forge is unregistered": func(rig *runnerRig) {
			rig.forgeResolver.forge = nil
		},
		"the forge token cannot be read": func(rig *runnerRig) {
			rig.tokens.err = cause
		},
		"the spawn is refused": func(rig *runnerRig) {
			rig.agents.spawnErr = cause
		},
		"the worker cannot be prompted": func(rig *runnerRig) {
			rig.agents.promptErr = cause
		},
		"the publish fails": func(rig *runnerRig) {
			rig.container.pushErr = cause
		},
		"the signing agent cannot start": func(rig *runnerRig) {
			rig.signing.err = cause
		},
		"the worker session runs no container": func(rig *runnerRig) {
			rig.agents.noContainer = true
		},
	}
	for name, arrange := range cases {
		t.Run(name, func(t *testing.T) {
			rig := newRunnerRig(t)
			rig.rebindVerification(t, "true")
			rig.workerWrote()
			rig.verdictOnPublish("APPROVED", "lgtm")
			arrange(rig)
			if err := rig.runner.Run(context.Background(), rig.job(t)); err == nil {
				t.Fatal("the job completed despite the refusal")
			}
		})
	}
}

// TestRunner_NoVerificationCommandsLeavesTheForgeAsTheGate: a binding naming
// none is a project whose forge checks are the whole verification, not a
// project whose work is unverified by accident.
func TestRunner_NoVerificationCommandsLeavesTheForgeAsTheGate(t *testing.T) {
	rig := newRunnerRig(t)
	if err := rig.rebind(context.Background(), func(b *ProjectBinding) { b.Verification = nil }); err != nil {
		t.Fatal(err)
	}
	rig.workerWrote()
	rig.verdictOnPublish("APPROVED", "lgtm")
	if err := rig.runner.Run(context.Background(), rig.job(t)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(rig.forge.merges) != 1 {
		t.Fatalf("merges = %d, want one", len(rig.forge.merges))
	}
}

// TestRunner_ACommandTheOperatorNeverAllowedIsRefused: verification is tenant
// data bound for a shell, and the operator's allowlist is what decides
// whether it runs. The refusal fails the job rather than passing the criterion.
func TestRunner_ACommandTheOperatorNeverAllowedIsRefused(t *testing.T) {
	rig := newRunnerRig(t)
	if err := rig.rebind(context.Background(), func(b *ProjectBinding) {
		b.Verification = []string{"rm -rf /"}
	}); err != nil {
		t.Fatal(err)
	}
	rig.workerWrote()
	if err := rig.runner.Run(context.Background(), rig.job(t)); err == nil {
		t.Fatal("a command outside the operator's allowlist was run")
	}
	if len(rig.forge.prs) != 0 {
		t.Error("work that never verified was published for review")
	}
}

// TestRunner_TheEscalationPublishesTheBranchBeforeTheContainerGoes: the
// container is the only thing that made a checkout, so an unpushed commit dies
// with it. The push comes first on every non-completed exit.
func TestRunner_TheEscalationPublishesTheBranchBeforeTheContainerGoes(t *testing.T) {
	rig := newRunnerRig(t)
	if err := rig.rebind(context.Background(), func(b *ProjectBinding) {
		b.Verification = []string{"false"}
		b.MaxReviewRounds = 1
	}); err != nil {
		t.Fatal(err)
	}
	rig.workerWrote()
	rig.container.ahead = true

	err := rig.runner.Run(context.Background(), rig.job(t, "false"))
	if err == nil {
		t.Fatal("a spent review budget completed the job")
	}
	if len(rig.container.pushes) == 0 {
		t.Fatal("the branch was not published before teardown; the worker's commits are lost")
	}
	if len(rig.agents.closed) != 1 {
		t.Errorf("the worker session was not closed: %v", rig.agents.closed)
	}
}

// TestRunner_ATeardownPushFailureDoesNotHideTheOutcome: the job's outcome is
// already decided by the time teardown runs, so a failing publish is reported
// and the original verdict stands.
func TestRunner_ATeardownPushFailureDoesNotHideTheOutcome(t *testing.T) {
	rig := newRunnerRig(t)
	if err := rig.rebind(context.Background(), func(b *ProjectBinding) {
		b.Verification = []string{"false"}
		b.MaxReviewRounds = 1
	}); err != nil {
		t.Fatal(err)
	}
	rig.workerWrote()
	rig.container.ahead = true
	rig.container.pushErr = errors.New("the remote refused")

	err := rig.runner.Run(context.Background(), rig.job(t, "false"))
	var escalated *EscalatedError
	if !errors.As(err, &escalated) {
		t.Fatalf("err = %v, want the escalation the job actually ended on", err)
	}
}

// TestRunnerRun_DecorateAndTaskIDTolerateAnEmptyRun: the snapshot decorator
// and the task accessor are called from the envelope on a run that may not
// have reached its first state yet.
func TestRunnerRun_DecorateAndTaskIDTolerateAnEmptyRun(t *testing.T) {
	rig := newRunnerRig(t)
	run := &runnerRun{runner: rig.runner, job: rig.job(t)}
	run.decorate(nil)
	status := &orchestration.JobStatus{}
	run.decorate(status)
	if len(status.SessionIDs) != 0 {
		t.Fatalf("a run with no session decorated one: %v", status.SessionIDs)
	}
	run.sessionID = "session-1"
	run.decorate(status)
	if len(status.SessionIDs) != 1 || status.SessionIDs[0] != "session-1" {
		t.Fatalf("session ids = %v", status.SessionIDs)
	}
	run.job.Config = nil
	if run.taskID() != "" {
		t.Fatal("a run with no config named a task")
	}
	if run.attemptNumber() != 0 {
		t.Fatal("a run with no config numbered an attempt")
	}
	if got := run.roundFindings(); got != "" {
		t.Fatalf("the first round carried findings: %q", got)
	}
}

// TestRunner_ACancelledContextEndsTheRun: the envelope cancels a run it is
// ending, and the runner answers the cancellation rather than driving on.
func TestRunner_ACancelledContextEndsTheRun(t *testing.T) {
	rig := newRunnerRig(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := rig.runner.Run(ctx, rig.job(t)); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if rig.agents.sessionCount() != 0 {
		t.Fatal("a cancelled run spawned a worker")
	}
}

// =============================================================================
// The steps, one at a time
// =============================================================================

// newRunnerRun builds a run positioned as if initializing had finished, so a
// test can drive one step and read exactly what it did.
func newRunnerRun(t *testing.T, rig *runnerRig) *runnerRun {
	t.Helper()
	ctx := context.Background()
	run := &runnerRun{runner: rig.runner, job: rig.job(t)}
	if err := run.resolve(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := rig.hooks.Prepare(ctx, run.job.Context()); err != nil {
		t.Fatal(err)
	}
	if err := run.loadDelivery(ctx); err != nil {
		t.Fatal(err)
	}
	run.sessionID = "session-1"
	run.containerDir = filepath.Join(orchestration.WorkspaceCheckoutRoot, testOrchProject().Name)
	if err := run.job.SetContainer(run.sessionID, run.containerDir); err != nil {
		t.Fatal(err)
	}
	return run
}

// TestRunnerRun_CompleteSurfacesTheMergeRefusal: Complete is bound to the
// reviewed head, and a head that moved is a refusal the job carries rather
// than a merge of something nobody reviewed.
func TestRunnerRun_CompleteSurfacesTheMergeRefusal(t *testing.T) {
	rig := newRunnerRig(t)
	run := newRunnerRun(t, rig)
	ctx := context.Background()
	if _, err := run.complete(ctx); err == nil {
		t.Fatal("Complete accepted a delivery with no pull request")
	}
}

// TestRunnerRun_PostMergeIsSkippedForACarveOut: a change that never had a
// pull request has nothing after it either.
func TestRunnerRun_PostMergeIsSkippedForACarveOut(t *testing.T) {
	rig := newRunnerRig(t)
	run := newRunnerRun(t, rig)
	run.delivery.RequiresPullRequest = false
	if err := run.runPostMerge(context.Background()); err != nil {
		t.Fatalf("runPostMerge on a carve-out: %v", err)
	}
	if len(rig.forge.prs) != 0 {
		t.Fatal("a carve-out opened a post-merge pull request")
	}
}

// TestRunnerRun_PostMergeSurfacesTheForgeRefusals: every way a back-merge can
// fail ends the step, because a back-merge silently not performed is a fix
// lost from the development line.
func TestRunnerRun_PostMergeSurfacesTheForgeRefusals(t *testing.T) {
	cause := errors.New("the forge refused")
	step := Step{Kind: StepBackMerge, Source: "main", Target: GitFlowDevelopBranch, Title: "back-merge"}
	t.Run("the pull request cannot be opened", func(t *testing.T) {
		rig := newRunnerRig(t)
		run := newRunnerRun(t, rig)
		rig.forge.createPRErr = cause
		if err := run.runStep(context.Background(), step); !errors.Is(err, cause) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("the merge is refused", func(t *testing.T) {
		rig := newRunnerRig(t)
		run := newRunnerRun(t, rig)
		rig.forge.mergeErr = cause
		if err := run.runStep(context.Background(), step); !errors.Is(err, cause) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("the issue behind the merge is gone", func(t *testing.T) {
		rig := newRunnerRig(t)
		run := newRunnerRun(t, rig)
		rig.forge.issues = map[int]*ForgeIssue{}
		if err := run.runPostMerge(context.Background()); err == nil {
			t.Fatal("a post-merge that could not read the issue reported success")
		}
	})
}

// TestRunnerRun_EscalateSurfacesTheParkFailure: the park is how a person
// learns the engine gave up, so a park that could not be posted fails the
// teardown rather than being swallowed.
func TestRunnerRun_EscalateSurfacesTheParkFailure(t *testing.T) {
	rig := newRunnerRig(t)
	run := newRunnerRun(t, rig)
	rig.forge.commentErr = errors.New("the forge refused the comment")
	rig.forge.postFailures = 99
	run.job.Escalate("nothing worked")
	if _, err := run.escalate(context.Background()); err == nil {
		t.Fatal("a park nobody could read reported success")
	}
}

// TestRunnerRun_ExecuteRefusesWithoutASession: the worker session is what a
// prompt goes to, and a job that lost it has nothing to drive.
func TestRunnerRun_ExecuteRefusesWithoutASession(t *testing.T) {
	rig := newRunnerRig(t)
	run := newRunnerRun(t, rig)
	run.sessionID = ""
	var sessionErr *WorkerSessionError
	if _, err := run.execute(context.Background()); !errors.As(err, &sessionErr) {
		t.Fatalf("err = %v, want WorkerSessionError", err)
	}
}

// TestRunnerRun_RoundFindingsFallBackToTheRecordedFeedback: memory is what a
// round carries, and the feedback the step recorded is what a round says when
// memory holds nothing yet.
func TestRunnerRun_RoundFindingsFallBackToTheRecordedFeedback(t *testing.T) {
	rig := newRunnerRig(t)
	run := newRunnerRun(t, rig)
	run.feedback = "the build failed"
	run.job.NextIteration()
	if got := run.roundFindings(); !strings.Contains(got, "Round 1 findings") {
		t.Fatalf("findings = %q", got)
	}
	run.job.Memory().AddFeedback("keep the fix inside pkg/thing")
	if got := run.roundFindings(); !strings.Contains(got, "keep the fix inside pkg/thing") {
		t.Fatalf("findings = %q", got)
	}
}

// TestVerificationCriteria_SkipsBlanksAndRendersOnlyFailures: an empty entry
// in the binding's list is not a command, and a round's feedback names what
// failed rather than everything that ran.
func TestVerificationCriteria_SkipsBlanksAndRendersOnlyFailures(t *testing.T) {
	criteria := verificationCriteria([]string{"make build", "   ", ""})
	if len(criteria) != 1 || criteria[0].Config != "make build" {
		t.Fatalf("criteria = %+v", criteria)
	}
	if criteria[0].Validator != ValidatorKindContainerCommand || !criteria[0].Required {
		t.Fatalf("criterion = %+v, want a required container command", criteria[0])
	}
	rendered := renderFailures([]orchestration.ValidationResult{
		{Name: "make build", Passed: true, Message: "fine"},
		{Name: "make test", Passed: false, Message: "one test failed", Details: "exit code: 1"},
	})
	if strings.Contains(rendered, "make build") {
		t.Errorf("a passing command was reported as a finding:\n%s", rendered)
	}
	if !strings.Contains(rendered, "one test failed") || !strings.Contains(rendered, "exit code: 1") {
		t.Errorf("the failure's output was dropped:\n%s", rendered)
	}
}

// TestRunnerRun_NextRoundEscalatesOnTheDefaultBoundWhenTheBindingNamesNone.
func TestRunnerRun_NextRoundEscalatesOnTheDefaultBoundWhenTheBindingNamesNone(t *testing.T) {
	rig := newRunnerRig(t)
	if err := rig.rebind(context.Background(), func(b *ProjectBinding) { b.MaxReviewRounds = 0 }); err != nil {
		t.Fatal(err)
	}
	run := newRunnerRun(t, rig)
	ctx := context.Background()
	for round := 1; round <= DefaultMaxReviewRounds; round++ {
		next, err := run.nextRound(ctx, "changes requested")
		if err != nil || next != orchestration.JobStateExecuting {
			t.Fatalf("round %d: %v %q", round, err, next)
		}
	}
	next, err := run.nextRound(ctx, "changes requested")
	if err != nil || next != orchestration.JobStateEscalating {
		t.Fatalf("a spent budget answered %q (%v)", next, err)
	}
	if !strings.Contains(run.job.EscalationReason(), itoa(DefaultMaxReviewRounds)) {
		t.Errorf("the escalation does not name the bound: %q", run.job.EscalationReason())
	}
}

// =============================================================================
// The signing agent and the container
// =============================================================================

// TestRunner_TheSigningSocketIsMountedAndGoesWithTheJob is the whole of the
// key-custody story from the runner's side.
//
// The agent is bound under the DAEMON'S SOCKETS ROOT — short, dedicated, and
// admitted by the mount policy by construction, because a socket under the
// project's workspace does not fit in the kernel's sun_path field — the
// socket is mounted into the container and named through SSH_AUTH_SOCK, and
// the directory is gone when the job ends. The key itself is never anywhere:
// what the container can reach is a socket that answers signature requests
// for exactly as long as this attempt lives.
func TestRunner_TheSigningSocketIsMountedAndGoesWithTheJob(t *testing.T) {
	rig := newRunnerRig(t)
	rig.rebindVerification(t, "true")
	rig.workerWrote()
	rig.verdictOnPublish("APPROVED", "lgtm")

	if err := rig.runner.Run(context.Background(), rig.job(t)); err != nil {
		t.Fatalf("Run: %v", err)
	}

	dirs := rig.signing.startedDirs()
	if len(dirs) != 1 {
		t.Fatalf("signing agents started = %d, want one per attempt", len(dirs))
	}
	if got := filepath.Dir(dirs[0]); got != rig.socketsRoot {
		t.Errorf("the socket was bound under %q, want the daemon's sockets root %q", got, rig.socketsRoot)
	}
	if strings.HasPrefix(dirs[0], rig.root) {
		t.Errorf("the socket was bound under the project workspace %q, which does not fit sun_path", rig.root)
	}
	if name := filepath.Base(dirs[0]); len(name) != SigningSocketDirNameLen {
		t.Errorf("session directory %q is %d characters, want %d", name, len(name), SigningSocketDirNameLen)
	}
	socket := filepath.Join(dirs[0], SigningSocketFile)
	if len(socket) > MaxSigningSocketPath {
		t.Errorf("socket path is %d bytes, over the %d-byte limit", len(socket), MaxSigningSocketPath)
	}

	spawn := rig.agents.spawns[0]
	if got := spawn.Env[orchestration.WorkspaceSigningAgentEnv]; got != orchestration.WorkspaceSigningAgentSocket {
		t.Errorf("SSH_AUTH_SOCK = %q, want the socket's path inside the container", got)
	}
	mounted := false
	for _, mount := range spawn.ExtraMounts {
		if mount.Target == orchestration.WorkspaceSigningAgentSocket {
			mounted = true
			if mount.Source != socket {
				t.Errorf("mounted %q, want the agent's own socket %q", mount.Source, socket)
			}
		}
	}
	if !mounted {
		t.Fatalf("the signing socket was not mounted: %+v", spawn.ExtraMounts)
	}
	if rig.signing.stopCount() != 1 {
		t.Errorf("signing agents stopped = %d, want the attempt's own torn down", rig.signing.stopCount())
	}
	if _, err := os.Stat(dirs[0]); !os.IsNotExist(err) {
		t.Errorf("the agent's directory outlived the job: %v", err)
	}
}

// TestRunner_AWorkerWithNoContainerIsRefusedBeforeItIsPrompted: the delivery
// is committed, signed and pushed inside the worker's container. A session
// that is a host process has nowhere to do that, and doing it on the daemon
// instead is the exact thing the container exists to prevent — so the refusal
// comes at the spawn, before a worker spends a turn on work nothing could
// publish.
func TestRunner_AWorkerWithNoContainerIsRefusedBeforeItIsPrompted(t *testing.T) {
	rig := newRunnerRig(t)
	rig.agents.noContainer = true

	err := rig.runner.Run(context.Background(), rig.job(t))
	var refused *WorkerNotContainerizedError
	if !errors.As(err, &refused) || !errors.Is(err, ErrRunner) {
		t.Fatalf("err = %v, want WorkerNotContainerizedError", err)
	}
	if len(rig.agents.prompts) != 0 {
		t.Fatalf("the worker was prompted anyway: %d turns", len(rig.agents.prompts))
	}
}

// TestRunner_AWorkerWhoseContainerCameFromTheDaemonIsAccepted is the twin of
// the refusal above, and the case go-quicbaas#740 turned into the refusal.
//
// A REMOTE worker's container is created by the agent daemon, and its id
// reaches the runner only by being reported on the spawn response and read off
// it (pkg/agent GRPCSpawnServer.Spawn -> RemoteACPBackend.Spawn). While either
// end dropped it, a perfectly healthy containerized worker was indistinguishable
// from a host process and was refused before its first turn. The id here is
// deliberately one the runner could not have derived: it comes from the
// response or from nowhere.
func TestRunner_AWorkerWhoseContainerCameFromTheDaemonIsAccepted(t *testing.T) {
	rig := newRunnerRig(t)
	rig.agents.containerID = "ctr-daemon-0123456789abcdef"
	rig.rebindVerification(t, "true")
	rig.workerWrote()
	rig.verdictOnPublish("APPROVED", "lgtm")

	if err := rig.runner.Run(context.Background(), rig.job(t)); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got := rig.agents.sessionCount(); got != 1 {
		t.Fatalf("spawns = %d, want the attempt's single worker", got)
	}
	if len(rig.agents.prompts) == 0 {
		t.Fatal("the worker was never prompted, so the containerization gate refused it")
	}
}

// TestRunner_AProjectThatSignsNothingIsRefused: autonomous development
// publishes under an identity, and a project whose signing is off has none.
// Answering with an unsigned commit would be a provenance claim that is
// wrong and permanent.
func TestRunner_AProjectThatSignsNothingIsRefused(t *testing.T) {
	rig := newRunnerRig(t)
	job := rig.job(t)
	job.Project.Git.Signing.Enabled = false

	if err := rig.runner.Run(context.Background(), job); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("err = %v, want ErrInvalidInput", err)
	}
	if rig.agents.sessionCount() != 0 {
		t.Error("a worker was spawned for a project with no publishing identity")
	}
}

// TestRunner_ThePublishRunsEveryGitCommandInsideTheContainer is the boundary
// this phase moved, asserted end to end: across a whole delivery, every git
// command the runner ran was an exec into the worker's session, in the
// CONTAINER's checkout — never a host path, and never on the daemon.
func TestRunner_ThePublishRunsEveryGitCommandInsideTheContainer(t *testing.T) {
	rig := newRunnerRig(t)
	rig.rebindVerification(t, "true")
	rig.workerWrote()
	rig.verdictOnPublish("APPROVED", "lgtm")

	if err := rig.runner.Run(context.Background(), rig.job(t)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	containerDir := filepath.Join(orchestration.WorkspaceCheckoutRoot, "api")
	// Two container directories, and no third: the checkout ROOT, which is
	// where the clone puts the project and its context repositories, and the
	// project checkout itself, which is where everything after it runs.
	want := map[string]bool{orchestration.WorkspaceCheckoutRoot: true, containerDir: true}
	for _, dir := range rig.container.dirsUsed() {
		if !want[dir] {
			t.Fatalf("git ran in %q, which is neither the checkout root nor the container's checkout %q", dir, containerDir)
		}
	}
	commands := rig.container.commandsRun()
	var sawCommit, sawPush bool
	for _, cmd := range commands {
		// git and the binding's verification are the only two things the
		// runner runs in a worker's container, and both run there.
		if !strings.HasPrefix(cmd, "git ") && cmd != "sh -c true" {
			t.Errorf("the runner ran something unexpected in the container: %q", cmd)
		}
		if strings.Contains(cmd, " commit ") {
			sawCommit = true
			if !strings.Contains(cmd, "commit.gpgsign=true") {
				t.Errorf("the commit was not signed: %q", cmd)
			}
		}
		if strings.Contains(cmd, " push ") {
			sawPush = true
		}
	}
	if !sawCommit || !sawPush {
		t.Fatalf("the delivery did not commit and push in the container: %v", commands)
	}
	for _, e := range rig.container.execs {
		if strings.HasPrefix(e.WorkDir, rig.root) {
			t.Fatalf("a command named the daemon's host path %q", e.WorkDir)
		}
	}
}

// =============================================================================
// Verification
// =============================================================================

// TestRunner_VerificationRunsInsideTheWorkersContainer is the boundary this
// change moved, asserted end to end: the binding's verification command was
// an exec into the worker's session, in the CONTAINER's checkout, and the
// runner published no daemon directory for one to have run in instead.
func TestRunner_VerificationRunsInsideTheWorkersContainer(t *testing.T) {
	rig := newRunnerRig(t)
	rig.rebindVerification(t, "make build")
	rig.container.scriptVerification("make build", &orchestration.ExecResult{Stdout: "build ok\n"})
	rig.workerWrote()
	rig.verdictOnPublish("APPROVED", "lgtm")

	job := rig.job(t, "make build")
	if err := rig.runner.Run(context.Background(), job); err != nil {
		t.Fatalf("Run: %v", err)
	}
	containerDir := filepath.Join(orchestration.WorkspaceCheckoutRoot, "api")
	var verified *containerExec
	for i, call := range rig.container.execs {
		if len(call.Cmd) == 3 && call.Cmd[0] == verificationShell && call.Cmd[2] == "make build" {
			verified = &rig.container.execs[i]
		}
	}
	if verified == nil {
		t.Fatalf("the verification command never ran in the container: %v", rig.container.commandsRun())
	}
	if verified.WorkDir != containerDir {
		t.Errorf("verification ran in %q, want the container's checkout %q", verified.WorkDir, containerDir)
	}
	// The job still RECORDS the host side of the bind mount for a reader on
	// the daemon, and nothing ran there: the daemon directory is named by no
	// command this run executed, in the container or anywhere else.
	daemonDir := job.WorkDir()
	if daemonDir == "" {
		t.Fatal("the job recorded no work directory at all")
	}
	for _, call := range rig.container.execs {
		if call.WorkDir == daemonDir {
			t.Errorf("a command named the daemon directory %q", daemonDir)
		}
	}
	if job.Container() == nil || job.Container().WorkDir != containerDir {
		t.Errorf("job container = %+v, want the container's checkout", job.Container())
	}
}

// TestRunner_AFailedVerificationSendsTheContainersOutputBackAsARound: what
// the command said INSIDE the container — on both streams — is what the
// worker is told, it is kept in the job's memory, and it costs a round.
func TestRunner_AFailedVerificationSendsTheContainersOutputBackAsARound(t *testing.T) {
	rig := newRunnerRig(t)
	rig.rebindVerification(t, "make test")
	rig.container.scriptVerification("make test", &orchestration.ExecResult{
		ExitCode: 2,
		Stdout:   "--- FAIL: TestThing\n",
		Stderr:   "make: *** [test] Error 1\n",
	})
	rig.workerWrote()
	rig.verdictOnPublish("APPROVED", "lgtm")

	// The worker fixes it: the second turn's verification passes, so the run
	// proceeds and the round is the only one it spends.
	turns := 0
	rig.agents.onPrompt = func(*orchestration.PromptSessionRequest) {
		turns++
		if turns > 1 {
			rig.container.scriptVerification("make test", &orchestration.ExecResult{})
		}
	}

	job := rig.job(t, "make test")
	if err := rig.runner.Run(context.Background(), job); err != nil {
		t.Fatalf("Run: %v", err)
	}
	prompts := rig.agents.promptTexts()
	if len(prompts) != 2 {
		t.Fatalf("prompts = %d, want the first turn and one round", len(prompts))
	}
	for _, want := range []string{"make: *** [test] Error 1", "exit code: 2", "--- FAIL: TestThing"} {
		if !strings.Contains(prompts[1], want) {
			t.Errorf("the round dropped %q from the container's output:\n%s", want, prompts[1])
		}
	}
	if memory := job.Memory().Serialize(); !strings.Contains(memory, "--- FAIL: TestThing") {
		t.Errorf("the failure is not in the job's memory:\n%s", memory)
	}
	if job.Iteration() != 1 {
		t.Errorf("iteration = %d, want the failed verification counted as one round", job.Iteration())
	}
	if rig.agents.sessionCount() != WorkerSessionCount {
		t.Errorf("a round spawned a second session: %d", rig.agents.sessionCount())
	}
}

// =============================================================================
// The ssh checkout — the forge is pinned, never learned (go-quicbaas#749)
// =============================================================================

// sshForgeRepoURL is the shape the Software Factory actually clones: an ssh
// remote on a non-default port. The host is a reserved test name, not any
// deployment's address.
const sshForgeRepoURL = "ssh://git@forge.test:2222/acme/api.git"

// TestRunner_SSHCheckoutPinsTheForgeHostKey proves the whole ssh path of one
// attempt: the key the operator pinned on the binding becomes a known_hosts
// beside the workspace spec, mounted read-only, and the worker's git is told
// to verify the forge strictly against it.
//
// The burn-in failed at `git clone ssh://…` with no ssh in the image and no
// known_hosts anywhere. The image half is the Dockerfile's build-time smoke
// check; this is the daemon half.
func TestRunner_SSHCheckoutPinsTheForgeHostKey(t *testing.T) {
	rig := newRunnerRig(t)
	rig.repoURL = sshForgeRepoURL
	if err := rig.rebind(context.Background(), func(b *ProjectBinding) {
		b.Verification = []string{"true"}
		b.Forge.SSHHostKey = testSSHHostKey
	}); err != nil {
		t.Fatal(err)
	}
	rig.workerWrote()
	rig.verdictOnPublish("APPROVED", "lgtm")

	if err := rig.runner.Run(context.Background(), rig.job(t)); err != nil {
		t.Fatalf("Run: %v", err)
	}

	spawn := rig.agents.spawns[0]
	wantCommand := orchestration.WorkspaceSSHCommand(
		filepath.Join(orchestration.WorkspaceSpecDir, orchestration.WorkspaceKnownHostsFile))
	if got := spawn.Env[orchestration.WorkspaceSSHCommandEnv]; got != wantCommand {
		t.Fatalf("%s = %q, want %q", orchestration.WorkspaceSSHCommandEnv, got, wantCommand)
	}
	if strings.Contains(spawn.Env[orchestration.WorkspaceSSHCommandEnv], "accept-new") {
		t.Fatal("the worker was told to accept an unknown forge on first sight")
	}
}

// TestRunner_SSHCheckoutWithoutAPinnedKeyIsRefusedBeforeTheSpawn: an ssh forge
// whose binding pinned nothing must not produce a container at all. The
// refusal names the field the operator fills in, and no worker session is
// ever spawned — there is no point at which a clone could learn a key.
func TestRunner_SSHCheckoutWithoutAPinnedKeyIsRefusedBeforeTheSpawn(t *testing.T) {
	rig := newRunnerRig(t)
	rig.repoURL = sshForgeRepoURL
	rig.rebindVerification(t, "true")

	err := rig.runner.Run(context.Background(), rig.job(t))
	if err == nil {
		t.Fatal("an ssh checkout with no pinned host key ran")
	}
	var typed *orchestration.SSHHostKeyError
	if !errors.As(err, &typed) || !errors.Is(err, orchestration.ErrSSHHostKeyInvalid) {
		t.Fatalf("got %T %v, want *orchestration.SSHHostKeyError", err, err)
	}
	if typed.Field() != "forge.ssh_host_key" {
		t.Fatalf("the refusal must name the missing field: %q", typed.Field())
	}
	if got := rig.agents.sessionCount(); got != 0 {
		t.Fatalf("sessions = %d, want the attempt refused before any container started", got)
	}
}

// TestRunnerRun_ACIHostDefectParksInsteadOfRedelegating is the runner's half of
// go-quicbaas#898.
//
// The gate now classifies a red pipeline, and a red that no change could fix —
// a registry the host cannot reach, a container the runtime refuses, a
// module-resolution environment that was never provisioned — arrives as a
// *CIHostDefectError. Re-delegating it is what cost 1.5M tokens on 2026-09-12;
// failing the run would lose the account a person needs. It parks, and the
// park's own machinery posts the account to the issue.
func TestRunnerRun_ACIHostDefectParksInsteadOfRedelegating(t *testing.T) {
	rig := newRunnerRig(t)
	rig.workerWrote()
	// Approved by an independent reviewer, so nothing but the pipeline can
	// block: what is under test is what a red MEANS, not whether it blocks.
	rig.forge.onCreatePullRequest = func(pr *ForgePullRequest) {
		rig.forge.reviews[pr.Number] = []ForgeReview{
			{ID: 1, Reviewer: "codex", State: "APPROVED", HeadSHA: pr.HeadSHA},
		}
		rig.forge.checks[pr.HeadSHA] = []ForgeCheckRun{{
			ID: "1", Name: "lint", HeadSHA: pr.HeadSHA, Status: "completed", Conclusion: "failure",
			Completed: time.Now().Add(-time.Hour),
		}}
		rig.forge.logs["lint"] = logHostDefect
	}

	run := newRunnerRun(t, rig)
	state, err := run.review(context.Background())
	if err != nil {
		t.Fatalf("review: %v", err)
	}
	if state != orchestration.JobStateEscalating {
		t.Fatalf("state = %q, want escalating: a machine fault must not be re-delegated to the worker", state)
	}
	if run.parked.kind != orchestration.TaskBlockKindEscalated {
		t.Errorf("park kind = %q, want escalated", run.parked.kind)
	}
	// The kind alone is not the point. An unregistered or absent fault class is
	// read as a failure of the WORK by design, which would charge the attempt
	// and re-delegate to a coding agent -- the behaviour this change exists to
	// end. So assert the attribution, not just that a park happened.
	if run.parked.faultClass != orchestration.FaultCIHostDefect {
		t.Errorf("faultClass = %q, want %q", run.parked.faultClass, orchestration.FaultCIHostDefect)
	}
	if !orchestration.OperatorFaultClass(run.parked.faultClass) {
		t.Errorf("faultClass %q is not registered as an operator fault, so the attempt would be charged to the work", run.parked.faultClass)
	}
	if reason := run.job.EscalationReason(); !strings.Contains(reason, "container_create_failed") {
		t.Errorf("the park reason does not name the signature:\n%s", reason)
	}
	if run.feedback != "" {
		t.Errorf("a machine fault was turned into worker feedback: %q", run.feedback)
	}
}

// TestRunnerRun_AGenuineRedStillGoesBackToTheWorker is the other side of the
// same branch, and the one that must not regress: the change's own failure is
// re-delegated with the findings, exactly as before the classifier existed.
func TestRunnerRun_AGenuineRedStillGoesBackToTheWorker(t *testing.T) {
	rig := newRunnerRig(t)
	rig.workerWrote()
	rig.forge.onCreatePullRequest = func(pr *ForgePullRequest) {
		rig.forge.reviews[pr.Number] = []ForgeReview{
			{ID: 1, Reviewer: "codex", State: "APPROVED", HeadSHA: pr.HeadSHA},
		}
		rig.forge.checks[pr.HeadSHA] = []ForgeCheckRun{{
			ID: "1", Name: "build", HeadSHA: pr.HeadSHA, Status: "completed", Conclusion: "failure",
			Completed: time.Now().Add(-time.Hour),
		}}
		rig.forge.logs["build"] = logGenuine
	}

	run := newRunnerRun(t, rig)
	state, err := run.review(context.Background())
	if err != nil {
		t.Fatalf("review: %v", err)
	}
	if state != orchestration.JobStateRedelegating {
		t.Fatalf("state = %q, want re-delegating", state)
	}
	if !strings.Contains(run.feedback, "FAILED: build") {
		t.Errorf("the worker was not told what failed:\n%s", run.feedback)
	}
	if run.parked.kind != "" {
		t.Errorf("a real test failure parked the task: %q", run.parked.kind)
	}
}

// TestRunner_AForgeCredentialTheSpawnDoesNotHoldIsParkedNotCharged: the
// delivery half of go-quicbaas#929.
//
// A credential the remote would not accept is decidable from the spec on the
// daemon, so it costs no container — and it is a CONFIGURATION, so failing the
// job would retry the identical refusal for the whole attempt budget. It parks,
// attributed to the factory, with the repository and the missing credential in
// the reason.
func TestRunner_AForgeCredentialTheSpawnDoesNotHoldIsParkedNotCharged(t *testing.T) {
	rig := newRunnerRig(t)
	rig.rebindVerification(t, "true")
	rig.tokens.token = nil

	job := rig.job(t)
	err := rig.runner.Run(context.Background(), job)
	// The run ends on the PARK, which is the runner's answer to an unfit
	// workspace; the fault itself is on the task, where an operator reads it.
	if !errors.Is(err, ErrRunner) {
		t.Fatalf("err = %v, want the run to end on the park", err)
	}
	if got := rig.agents.sessionCount(); got != 0 {
		t.Fatalf("sessions = %d, want the attempt refused before any container started", got)
	}
	parked := job.Parked()
	if parked == nil {
		t.Fatal("a spawn that could not have cloned was not parked for anybody")
	}
	if parked.FaultClass != orchestration.FaultWorkspaceUnfit {
		t.Fatalf("park fault class = %q, want %q", parked.FaultClass, orchestration.FaultWorkspaceUnfit)
	}
	if parked.Kind != orchestration.TaskBlockKindBlocked {
		t.Fatalf("park kind = %q, want blocked", parked.Kind)
	}
	// The reason names the repository, the remote and the credential, which is
	// what the operator has to act on — and says the fault cost nothing.
	for _, want := range []string{"no credential", "https://forge.test/acme/api.git", "forge token", "no tokens were spent"} {
		if !strings.Contains(parked.Message, want) {
			t.Errorf("the park does not name %q: %q", want, parked.Message)
		}
	}
}

// TestDeliveryHeadRefused_IsTheDurableFormOfARequestedRound covers the three
// answers the resume depends on: a refusal at the current head, a refusal that
// binds to an older head, and a later approval that overrides an earlier
// refusal at the same head.
func TestDeliveryHeadRefused_IsTheDurableFormOfARequestedRound(t *testing.T) {
	base := time.Date(2026, 9, 13, 19, 0, 0, 0, time.UTC)
	cases := []struct {
		name    string
		head    string
		reviews []ReviewRecord
		want    bool
	}{
		{
			name:    "no reviews at all",
			head:    "aaa",
			reviews: nil,
			want:    false,
		},
		{
			name: "the current head is refused",
			head: "aaa",
			reviews: []ReviewRecord{
				{Verdict: VerdictRequestChanges, HeadSHA: "aaa", SubmittedAt: base},
			},
			want: true,
		},
		{
			name: "the refusal binds to an earlier head",
			head: "bbb",
			reviews: []ReviewRecord{
				{Verdict: VerdictRequestChanges, HeadSHA: "aaa", SubmittedAt: base},
			},
			want: false,
		},
		{
			name: "a later approval at the same head overrides the refusal",
			head: "aaa",
			reviews: []ReviewRecord{
				{Verdict: VerdictRequestChanges, HeadSHA: "aaa", SubmittedAt: base},
				{Verdict: VerdictApprove, HeadSHA: "aaa", SubmittedAt: base.Add(time.Hour)},
			},
			want: false,
		},
		{
			name: "an earlier approval does not rescue a later refusal",
			head: "aaa",
			reviews: []ReviewRecord{
				{Verdict: VerdictApprove, HeadSHA: "aaa", SubmittedAt: base},
				{Verdict: VerdictRequestChanges, HeadSHA: "aaa", SubmittedAt: base.Add(time.Hour)},
			},
			want: true,
		},
		{
			name: "no head means nothing to bind a verdict to",
			head: "",
			reviews: []ReviewRecord{
				{Verdict: VerdictRequestChanges, HeadSHA: "aaa", SubmittedAt: base},
			},
			want: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			delivery := &Delivery{HeadSHA: c.head, Reviews: c.reviews}
			if got := delivery.HeadRefused(); got != c.want {
				t.Fatalf("HeadRefused() = %v, want %v", got, c.want)
			}
		})
	}
}

// TestResumeReview_ARefusedHeadDoesNotResumeIntoReview is the orphaned pull
// request, and the case roundRequested cannot cover.
//
// roundRequested is one JOB's memory of a rejected head. A task readmitted
// after its job ended arrives with the flag clear and a pull request whose
// CURRENT head a reviewer already refused. Resuming into reviewing then asks
// for the identical verdict on the identical tree with no worker running to
// address it, which surfaces as "there is no worker session to prompt", one
// attempt at a time, until the budget is gone -- leaving the pull request
// open, refused and unattended. Measured on go-truststrap PRs #64, #65 and
// #66; reproduced 2026-09-16 by readmitting task 61e3aa53, which spent three
// attempts in 91 seconds.
//
// An empty state and no error is "do not resume", which sends the run on to
// spawn the worker that addresses the findings.
func TestResumeReview_ARefusedHeadDoesNotResumeIntoReview(t *testing.T) {
	refused := &runnerRun{delivery: &Delivery{
		RequiresPullRequest: true,
		HeadSHA:             "e3fc67d8",
		PullRequest:         DeliveryPullRequest{Number: 66, State: PullRequestOpen},
		Reviews: []ReviewRecord{{
			Reviewer: "codex", Verdict: VerdictRequestChanges, HeadSHA: "e3fc67d8",
			SubmittedAt: time.Date(2026, 9, 13, 20, 43, 36, 0, time.UTC),
		}},
	}}
	// The forge is deliberately nil: reaching it would mean the refusal did
	// not short-circuit, and the nil would surface as a panic rather than as a
	// quietly wrong resume.
	state, err := refused.resumeReview(context.Background())
	if err != nil {
		t.Fatalf("resumeReview: %v", err)
	}
	if state != "" {
		t.Fatalf("state = %q, want the run to fall through and spawn a worker for the refused head", state)
	}
}

// TestRoundFindings_AReadmittedTaskIsHandedTheRecordedRefusal is the second
// half of the orphaned pull request.
//
// Spawning the worker is not enough: a readmitted task has a fresh job, so
// iteration and memory are both empty and the worker used to receive the bare
// issue contract. It would read a branch that already holds the work, answer
// "the branch already carries the complete implementation", and be charged a
// failure for producing no changes — with the reviewer's objection delivered
// to nobody. Measured on go-truststrap task 61e3aa53 / PR #66.
func TestRoundFindings_AReadmittedTaskIsHandedTheRecordedRefusal(t *testing.T) {
	rig := newRunnerRig(t)
	run := &runnerRun{
		job: rig.job(t, "true"),
		delivery: &Delivery{
			RequiresPullRequest: true,
			HeadSHA:             "e3fc67d8",
			PullRequest:         DeliveryPullRequest{Number: 66, State: PullRequestOpen},
			Reviews: []ReviewRecord{{
				Reviewer: "codex", Verdict: VerdictRequestChanges, HeadSHA: "e3fc67d8", Round: 0,
				Findings:    "Make cancellation effective before removing connection shutdown",
				SubmittedAt: time.Date(2026, 9, 13, 20, 43, 36, 0, time.UTC),
			}},
		},
	}
	findings := run.roundFindings()
	if findings == "" {
		t.Fatal("a readmitted task was handed no findings; the worker would receive the bare issue contract")
	}
	if !strings.Contains(findings, "Make cancellation effective") {
		t.Fatalf("the reviewer's objection did not reach the prompt:\n%s", findings)
	}
	// A fresh job numbers its first iteration zero, and "round 0" reads to the
	// worker as a first attempt rather than as an answer to a review.
	if !strings.Contains(findings, "1") {
		t.Fatalf("the round was not numbered from the refusal:\n%s", findings)
	}
}

// TestRoundFindings_AnApprovedHeadAddsNothing keeps the durable source from
// firing when there is no objection in force: the worker would be handed an
// answered review as if it were outstanding.
func TestRoundFindings_AnApprovedHeadAddsNothing(t *testing.T) {
	rig := newRunnerRig(t)
	base := time.Date(2026, 9, 13, 20, 0, 0, 0, time.UTC)
	run := &runnerRun{
		job: rig.job(t, "true"),
		delivery: &Delivery{
			RequiresPullRequest: true,
			HeadSHA:             "e3fc67d8",
			PullRequest:         DeliveryPullRequest{Number: 66, State: PullRequestOpen},
			Reviews: []ReviewRecord{
				{Reviewer: "codex", Verdict: VerdictRequestChanges, HeadSHA: "e3fc67d8",
					Findings: "an objection since answered", SubmittedAt: base},
				{Reviewer: "codex", Verdict: VerdictApprove, HeadSHA: "e3fc67d8",
					SubmittedAt: base.Add(time.Hour)},
			},
		},
	}
	if findings := run.roundFindings(); findings != "" {
		t.Fatalf("an approved head produced round findings:\n%s", findings)
	}
}
