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
	"strconv"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/prompts"
)

// =============================================================================
// Outcomes — one table, one renderer, one poster
// =============================================================================
//
// EVERY WAY A TASK CAN STOP REACHES THE ISSUE THE SAME WAY.
//
// Until go-quicbaas#788 only the RUN's escalation did: the Blocked lifecycle
// moment posted "This task needs the owner" and moved the label, and every
// other ending — the attempt budget spent, three environment faults in a row,
// a node that died with nothing left to try, an operator's own park — wrote
// nothing at all. The measured cost was a forge issue carrying
// `factory:in-progress` and no comment two hours after the engine had given
// up on it, which reads from the owner's side as work in progress.
//
// So there is ONE table. It names every member of orchestration's closed
// conclusion vocabulary, the comment each posts, the label each leaves and
// whether a person can readmit the task afterwards. The Blocked moment reads
// the same specs through the block kind it is given, so the escalation comment
// and the exhaustion comment are the same renderer with different rows — no
// per-reason branch anywhere. A conclusion reason with no row fails
// TestConclusionOutcomes_CoverEveryReason rather than going silently unposted.

// OutcomeKind names what an outcome comment announces. The first three are
// spelled exactly as orchestration's TaskBlockKind values so a comment posted
// through the Blocked moment and one posted through the conclusion seam carry
// the same marker vocabulary.
type OutcomeKind string

const (
	// OutcomeNeedsInfo is a stop only a person can settle.
	OutcomeNeedsInfo OutcomeKind = "needs_info"

	// OutcomeBlocked is a wait on an external condition.
	OutcomeBlocked OutcomeKind = "blocked"

	// OutcomeEscalated is the engine handing the task to the owner.
	OutcomeEscalated OutcomeKind = "escalated"

	// OutcomeExhausted is the attempt budget spent. The task is failed and
	// cannot be readmitted.
	OutcomeExhausted OutcomeKind = "exhausted"

	// OutcomeAbandoned is a job whose node stopped, with no attempts left.
	OutcomeAbandoned OutcomeKind = "abandoned"

	// OutcomeReadmitted is a parked task put back in the queue.
	OutcomeReadmitted OutcomeKind = "readmitted"

	// OutcomeSilent is the absence of an outcome comment: the conclusion was
	// already surfaced by the moment that caused it, so this path writes
	// nothing. It is a member of the table rather than a missing row, because
	// "nothing to say" and "nobody wrote the row" must not look alike.
	// OutcomeAlreadySatisfied says the work was already done before the
	// worker started — the issue is stale, or an earlier attempt of this task
	// landed it. It is the ONE terminal outcome that is not a problem, and it
	// exists so a correct "nothing to do" stops being reported as an
	// escalation the owner must rule on.
	OutcomeAlreadySatisfied OutcomeKind = "already_satisfied"

	OutcomeSilent OutcomeKind = ""
)

// String returns the kind's name.
func (k OutcomeKind) String() string { return string(k) }

// outcomeSpec is one row: what the comment says, where the label lands, and
// whether the task can come back.
type outcomeSpec struct {
	// headline is the comment's first line.
	headline string

	// meaning states, in a sentence, what the outcome means for the reader.
	meaning string

	// labelKey is the binding label key the issue is moved to. Empty removes
	// every label the module owns and adds none.
	labelKey string

	// terminal says the task has stopped. False is the readmission, which is
	// the undoing of a stop rather than one.
	terminal bool

	// resumable says the task store will accept a readmit afterwards. A
	// failed task is terminal in the store's own transition table, so an
	// outcome that fails the task is not resumable however much budget is
	// added.
	resumable bool
}

// outcomeSpecs is the rendering half of the table, keyed by what the comment
// announces. Two lookup maps below reach it — one from the Blocked moment's
// block kind, one from the engine's conclusion reason — so a row is written
// once and read by both.
var outcomeSpecs = map[OutcomeKind]outcomeSpec{
	OutcomeNeedsInfo: {
		headline:  "The agent has a question",
		meaning:   "the run stopped on something only a human can settle, and is waiting.",
		labelKey:  LabelNeedsInfo,
		terminal:  true,
		resumable: true,
	},
	OutcomeBlocked: {
		headline:  "This task is blocked",
		meaning:   "the task waits on an external condition, so nothing will retry it on its own.",
		labelKey:  LabelBlocked,
		terminal:  true,
		resumable: true,
	},
	OutcomeEscalated: {
		headline:  "This task needs the owner",
		meaning:   "the engine gave up on this task — review did not converge or the attempt budget is spent — and is waiting.",
		labelKey:  LabelEscalated,
		terminal:  true,
		resumable: true,
	},
	OutcomeExhausted: {
		headline: "This task has spent its attempt budget",
		meaning:  "every attempt the task was allowed has been charged and failed, so the Software Factory has stopped working on it.",
		// The binding's escalated label, not a key of its own. A new key
		// would be absent from every binding already in the field, and
		// ForgeConfig.Label answers an unknown key with the empty string —
		// so the fix for a silent conclusion would have been an issue left
		// with NO label at all until every operator edited their binding.
		// "The engine gave up" is what the escalated label already says.
		labelKey:  LabelEscalated,
		terminal:  true,
		resumable: false,
	},
	OutcomeAbandoned: {
		headline:  "This task lost the node that was running it",
		meaning:   "the daemon running the last attempt stopped without reporting, and the task has no attempts left to try again.",
		labelKey:  LabelEscalated,
		terminal:  true,
		resumable: true,
	},
	OutcomeAlreadySatisfied: {
		headline: "This issue is already satisfied",
		meaning: "the worker read the repository, found the change this issue asks for " +
			"already present, and made none. Nothing is wrong and no attempt was " +
			"charged — but nothing will happen here either, so the issue wants " +
			"closing or restating.",
		// NO LABEL. The binding's labels all say something is wrong or pending
		// (blocked, needs_info, escalated) and none of them is true here; a
		// stale issue is not a blocked one. Borrowing `escalated` would put it
		// back in the queue this outcome exists to keep it out of, and a new
		// key would be absent from every binding already in the field and so
		// resolve to the empty string anyway. The comment carries the meaning.
		labelKey: "",
		// TERMINAL: the engine is done with it. Resumable because a person who
		// restates the issue readmits the task, which is the honest way back —
		// the factory must not decide on its own that a stale issue became
		// fresh.
		terminal:  true,
		resumable: true,
	},
	OutcomeReadmitted: {
		headline: "This task has been readmitted",
		meaning:  "the park was cleared and the task is back in the queue for another attempt.",
		// Empty clears what the park left: the next attempt's Prepare is what
		// labels the issue in progress again.
		labelKey:  "",
		terminal:  false,
		resumable: true,
	},
}

// blockKindOutcomes maps the Blocked moment's block kind to what its comment
// announces.
var blockKindOutcomes = map[orchestration.TaskBlockKind]OutcomeKind{
	orchestration.TaskBlockKindNeedsInfo: OutcomeNeedsInfo,
	orchestration.TaskBlockKindBlocked:   OutcomeBlocked,
	orchestration.TaskBlockKindEscalated: OutcomeEscalated,
}

// conclusionOutcomes is THE table go-quicbaas#788 asks for: every member of
// orchestration's closed conclusion vocabulary and the forge write it makes.
//
//	conclusion            comment      label swap                readmit
//	--------------------  -----------  ------------------------  -------
//	completed             none         already `merged`          n/a
//	budget_exhausted      exhausted    in_progress → escalated   no
//	abandoned             abandoned    in_progress → escalated   yes
//	fault_bounded         blocked      in_progress → blocked     yes
//	fault_held            blocked      in_progress → blocked     yes
//	parked_needs_info     needs_info   in_progress → needs_info  yes
//	parked_blocked        blocked      in_progress → blocked     yes
//	parked_escalated      escalated    in_progress → escalated   yes
//	runner_parked         none         already made by the run    yes
//	readmitted            readmitted   every owned label removed n/a
//
// `completed` is OutcomeSilent because the Complete moment has already
// merged the pull request and moved the issue to `merged`; a second comment
// saying so would be the module talking to itself.
//
// `runner_parked` is OutcomeSilent for the same reason, one seam earlier. The
// three `parked_*` reasons are the operator's explicit BlockTask, which
// writes nothing to the issue, so this table is the only thing that can. A
// park the RUN decided already reached the issue: surfacePark posted it, with
// the worker's own text and its typed unblock condition, under
// blockedMarker(task, attempt, blockedOutcome(kind)) — the very marker
// renderOutcome would write. Giving it a row would put two comments with one
// marker on the issue (go-quicbaas#787). It is also the only park path that
// can re-check an interrupted post, which the conclusion seam cannot: see
// runnerRun.parkPosted.
//
// AND `runner_parked` IS ONLY REACHED WHEN THAT POST ACTUALLY LANDED. A run
// whose surfacing failed concludes as the plain `parked_<kind>` its block
// kind names (orchestration's parkConclusion), so it takes one of the three
// rows above and the comment is written HERE instead. Silence is the right
// answer only when somebody else has already spoken; the case where the
// runner could not speak is exactly the case a reader must not be left to
// infer from an issue that says nothing.
var conclusionOutcomes = map[orchestration.TaskConclusionReason]OutcomeKind{
	orchestration.TaskConcludedCompleted:        OutcomeSilent,
	orchestration.TaskConcludedBudgetExhausted:  OutcomeExhausted,
	orchestration.TaskConcludedAbandoned:        OutcomeAbandoned,
	orchestration.TaskConcludedFaultBounded:     OutcomeBlocked,
	orchestration.TaskConcludedFaultHeld:        OutcomeBlocked,
	orchestration.TaskConcludedParkedNeedsInfo:  OutcomeNeedsInfo,
	orchestration.TaskConcludedParkedBlocked:    OutcomeBlocked,
	orchestration.TaskConcludedParkedEscalated:  OutcomeEscalated,
	orchestration.TaskConcludedRunnerParked:     OutcomeSilent,
	orchestration.TaskConcludedReadmitted:       OutcomeReadmitted,
	orchestration.TaskConcludedAlreadySatisfied: OutcomeAlreadySatisfied,
}

// OutcomeOfConclusion returns what the module posts for a conclusion reason,
// and whether the reason has a row at all.
//
// EXPORTED so the coverage test reads the table through the same door the
// reporter does; a reason orchestration adds without a row here is answered
// false, and the reporter refuses rather than silently posting nothing.
func OutcomeOfConclusion(reason orchestration.TaskConclusionReason) (OutcomeKind, bool) {
	kind, ok := conclusionOutcomes[reason]
	return kind, ok
}

// lookupOutcomeSpec returns the rendering for a kind.
func lookupOutcomeSpec(kind OutcomeKind) (outcomeSpec, bool) {
	spec, ok := outcomeSpecs[kind]
	return spec, ok
}

// =============================================================================
// Rendering
// =============================================================================

// outcomeReport is everything one outcome comment is rendered from.
type outcomeReport struct {
	binding *ProjectBinding
	task    *orchestration.Task

	// attempt numbers the comment's marker, so a task parked twice carries
	// two identifiable comments rather than one the second park cannot tell
	// from its own.
	attempt int

	kind OutcomeKind

	// message is the evidence: the park reason, the last attempt's error, or
	// whatever the worker's final reply left on the task result. Rendered
	// through publicOutput, never raw.
	message string

	// iterations is how many review rounds ran, zero when none did.
	iterations int

	// faultClass is the fault class the RUN itself proved, for a comment
	// posted before the conclusion has written one onto the task.
	//
	// IT EXISTS BECAUSE THE COMMENT COMES FIRST. The Blocked moment posts
	// while the run still holds the job; the class reaches the task record
	// only when the attempt is concluded, which is after. A comment that read
	// the task alone would therefore name no cause on exactly the parks whose
	// cause the run had just established — and naming the cause is the
	// difference between "an operator must repair something" and "a context
	// repository this project declared is not there".
	faultClass string

	// delivery is the attempt's branch and pull request, nil when there is
	// none.
	delivery *Delivery
}

// renderOutcome renders one outcome comment.
//
// THE ONE RENDERER. The Blocked moment reaches it through renderBlocked and
// the conclusion seam through TaskConcluded, so an issue's park comment and
// its exhaustion comment differ only by the row they read.
func renderOutcome(r outcomeReport) string {
	spec, known := lookupOutcomeSpec(r.kind)
	if !known {
		spec = outcomeSpecs[OutcomeBlocked]
	}
	condition, detail, conditioned := blockedCondition(r.message)

	evidence := ""
	if quoted := outcomeEvidence(detail, spec); quoted != "" {
		evidence = quoted + "\n"
	}
	var b strings.Builder
	// CAUSE, THEN CONDITION, THEN WHO CLEARS IT — three lines answering three
	// different questions, read as one chain: what stopped this, what would
	// let it go, and who does that.
	//
	// THE CAUSE IS NOT AN OUTCOME ROW, and that is deliberate. An OutcomeKind
	// is the ANNOUNCEMENT — blocked, escalated, exhausted — and it selects the
	// headline, the meaning, the label and the remedy above. A fault class is
	// the CAUSE, carried on the task rather than on the reason, and the
	// announcement for a spend refusal genuinely IS "blocked": the reader is
	// told the task is blocked, and this line says why. Minting
	// OutcomeQuotaExhausted would claim a distinct announcement with its own
	// headline and remedy, and would need a TaskConclusionReason that
	// applyConclusion never returns. conclusionOutcomes already routes these
	// correctly through fault_bounded and fault_held.
	//
	// The two are independent: a fault that hit the consecutive bound has a
	// cause and no condition, an owner ruling has a condition and no fault,
	// and a readmission has neither because requeueing clears the class.
	// Each renders only when it is there.
	if cause := blockedCause(r.task, r.faultClass); cause != "" {
		b.WriteString(cause + "\n")
	}
	if conditioned {
		b.WriteString("**Unblocks when:** " + condition.Describe() + "\n")
		b.WriteString("**Cleared by:** " + blockedClearedBy(condition.Kind) + "\n")
	}
	if r.task != nil {
		b.WriteString("**Attempt:** " + attemptLine(r.task) + "\n")
		b.WriteString("**Task:** `" + r.task.ID + "`\n")
		if history := r.task.AttemptHistory(); len(history) > 0 {
			b.WriteString("**Attempt history:** `" + strings.Join(history, "`, `") + "`\n")
		}
	}
	if r.delivery != nil && r.delivery.PullRequest.Opened() {
		b.WriteString("**Pull request:** " + r.delivery.PullRequest.URL + "\n")
	}
	// THE ORPHANS ARE NAMED ON THE ISSUE because the person reading it is the
	// one who would otherwise find those runs on the forge and re-run them —
	// which on Gitea cancels the run on the current head. Recording the
	// displacement on the Delivery is what makes this line possible; this line
	// is what makes the record reach somebody.
	if orphans := supersessionLine(r.delivery); orphans != "" {
		b.WriteString(orphans + "\n")
	}
	if r.iterations > 0 {
		b.WriteString("**Review rounds:** " + strconv.Itoa(r.iterations) + "\n")
	}
	if transition := labelTransition(r.binding, spec.labelKey); transition != "" {
		b.WriteString("**Labels:** " + transition + "\n")
	}
	taskID := ""
	if r.task != nil {
		taskID = r.task.ID
	}
	return sdlcPrompts.MustRender("outcome_comment", prompts.Vars{
		"headline": spec.headline,
		"kind":     string(r.kind),
		"meaning":  spec.meaning,
		"evidence": evidence,
		"facts":    b.String(),
		"remedy":   outcomeRemedy(spec, r.task, condition, conditioned),
		"marker":   blockedMarker(taskID, r.attempt, r.kind),
	}) + "\n"
}

// outcomeEvidence renders the quoted evidence block, or nothing when a
// non-terminal outcome has no evidence to quote.
//
// A TERMINAL OUTCOME ALWAYS QUOTES SOMETHING, even if only that nothing was
// recorded: a stop with no stated reason is itself the thing the reader has to
// know. A readmission is not a stop and has nothing to explain.
func outcomeEvidence(detail string, spec outcomeSpec) string {
	if detail != "" {
		return quoteReason(detail)
	}
	if !spec.terminal {
		return ""
	}
	return quoteReason(sdlcPrompts.MustRender("outcome_no_reason_recorded", nil))
}

// outcomeRemedy renders what the reader can do about the outcome.
//
// THE ORDER IS THE PRECEDENCE, and every case above the last is a case where
// the obvious advice would be wrong:
//
//   - a readmission asks for nothing; it IS the resumption;
//   - a task the budget FAILED cannot be readmitted at all — taskTransitions
//     has no edge out of failed — so naming the readmit command would name a
//     command the store refuses;
//   - a task PARKED with its budget spent may be readmitted, but will not be
//     dispatched until the budget is raised (go-quicbaas#784's override, kept
//     ahead of the machine-checkable case for the same reason: the sweep
//     cannot return a task with no attempts left, however satisfied its
//     condition is);
//   - a machine-checkable condition needs no person at all: the sweep
//     re-examines it and returns the task itself.
func outcomeRemedy(spec outcomeSpec, task *orchestration.Task, condition UnblockCondition, conditioned bool) string {
	argument := ""
	if task != nil {
		argument = " " + task.ID
	}
	readmit := sdlcPrompts.MustRender("outcome_readmit_action", prompts.Vars{"task_argument": argument})
	switch {
	case !spec.terminal:
		return sdlcPrompts.MustRender("outcome_remedy_readmitted", nil)
	case !spec.resumable:
		return sdlcPrompts.MustRender("outcome_remedy_failed", nil)
	case task != nil && task.BudgetExhausted():
		return sdlcPrompts.MustRender("outcome_remedy_budget_spent", prompts.Vars{"readmit": readmit})
	case conditioned && condition.Kind.MachineCheckable():
		return sdlcPrompts.MustRender("outcome_remedy_self_clearing", nil)
	default:
		return sdlcPrompts.MustRender("outcome_remedy_resume", prompts.Vars{"readmit": readmit})
	}
}

// =============================================================================
// The conclusion seam
// =============================================================================

// conclusionArtifactName is the Task artifact recording the forge write an
// outcome made. One name means a task carries the LATEST outcome comment:
// SubmitTaskArtifacts overwrites by name, and a task concludes once.
const conclusionArtifactName = "outcome_comment"

var _ orchestration.TaskConclusionObserver = (*Hooks)(nil)

// TaskConcluded implements orchestration.TaskConclusionObserver: a task the
// module adopted has stopped, so the issue it mirrors is told.
//
// IT IS NOT A LIFECYCLE MOMENT. The moments belong to a job and need a live
// JobContext; this fires after the run has returned, and on the reaper's
// goroutine when the node that held the run is gone — the two places
// go-quicbaas#788 measured a task stopping with nothing written to the forge.
//
// A TASK THIS MODULE DID NOT ADOPT IS NOT ITS BUSINESS, and the cheap,
// definitive test is the task's own provenance: no forge_issue source, nothing
// to comment on, no error. Every other refusal — a project that cannot be
// read, a binding that names no registered forge — is reported, because those
// are tasks that ARE the module's and could not be served.
//
// NOTHING IS DEDUPLICATED HERE and nothing needs to be: a task concludes once
// under the store's compare-and-swap, and the block and readmit verbs are
// refused by the transition table on a second call. The resumed-escalation
// path is where an interrupted post is re-checked, because that one really can
// run twice (see runnerRun.parkPosted).
func (h *Hooks) TaskConcluded(ctx context.Context, conclusion orchestration.TaskConclusion) error {
	if err := conclusion.Validate(); err != nil {
		return err
	}
	kind, known := OutcomeOfConclusion(conclusion.Reason)
	if !known {
		return &UnknownConclusionError{Reason: string(conclusion.Reason), TaskID: conclusion.Task.ID}
	}
	spec, rendered := lookupOutcomeSpec(kind)
	if !rendered {
		// OutcomeSilent: the moment that caused the conclusion has already
		// written to the forge.
		return nil
	}
	task := conclusion.Task
	if _, _, err := issueOfTask(task); err != nil {
		if errors.Is(err, ErrTaskNotForgeIssue) {
			return nil
		}
		return err
	}
	a, err := h.resolveConcluded(ctx, conclusion.TenantID, task)
	if err != nil {
		return err
	}
	delivery, err := h.deliveries.GetDelivery(ctx, conclusion.TenantID, task.ID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	body := renderOutcome(outcomeReport{
		binding: a.binding, task: task, attempt: task.Attempts, kind: kind,
		message: conclusionEvidence(task), delivery: delivery,
	})
	comment, err := postWithRetry(ctx, a.forge, a.repo, a.issue, body, h.surfacing)
	if err != nil {
		return err
	}
	if err := h.recordOutcome(ctx, conclusion.TenantID, task, kind, comment); err != nil {
		return err
	}
	return h.moveLabels(ctx, a, spec.labelKey)
}

// resolveConcluded is Hooks.resolve for a caller that holds the task record
// and has no job: the project, the binding and the forge, assembled around a
// task that has already stopped.
//
// It shares resolve's split between a task-supplied repo and a binding-supplied
// identity, and go-quicbaas#841 closes both together.
func (h *Hooks) resolveConcluded(ctx context.Context, tenantID string, task *orchestration.Task) (*attempt, error) {
	project, err := h.projects.Get(ctx, tenantID, task.ProjectID)
	if err != nil {
		return nil, err
	}
	binding, err := h.bindings.GetBinding(ctx, tenantID, task.ProjectID)
	if err != nil {
		return nil, err
	}
	repo, issue, err := issueOfTask(task)
	if err != nil {
		return nil, err
	}
	forge, err := h.forges.ForgeFor(ctx, tenantID, binding.Forge)
	if err != nil {
		return nil, err
	}
	if forge == nil {
		return nil, NewForgeNotRegisteredError(binding.Forge.Type)
	}
	return &attempt{project: project, binding: binding, task: task, repo: repo, issue: issue, forge: forge}, nil
}

// conclusionEvidence is what the concluded task itself says about why it
// stopped: the park reason for a parked task, and otherwise the last attempt's
// result — which is where the runner records the failure that ended the
// attempt, and where a worker's final reply is carried when the attempt kept
// one.
func conclusionEvidence(task *orchestration.Task) string {
	if reason := strings.TrimSpace(task.BlockedReason); reason != "" {
		return reason
	}
	if task.Result != nil {
		return strings.TrimSpace(task.Result.Output)
	}
	return ""
}

// recordOutcome records the forge write on the task, the way the pull request
// and the head are recorded: one named artifact.
//
// AN ARTIFACT POINTING NOWHERE IS WORSE THAN NONE — the same reasoning as
// Hooks.publish, where a carve-out change records no pull_request artifact
// rather than one with an empty URL. A task whose provenance carries no issue
// URL gets the metadata on no artifact at all rather than a `url` artifact
// with nothing to open.
func (h *Hooks) recordOutcome(ctx context.Context, tenantID string, task *orchestration.Task, kind OutcomeKind, comment *ForgeComment) error {
	if task.Source == nil || task.Source.URL == "" {
		return nil
	}
	metadata := map[string]string{
		"outcome": string(kind),
		"issue":   task.Source.Ref,
	}
	if comment != nil {
		metadata["comment_id"] = strconv.FormatInt(comment.ID, 10)
	}
	return h.tasks.SubmitTaskArtifacts(ctx, tenantID, task.ID, []orchestration.Artifact{{
		Kind:     orchestration.ArtifactKindURL,
		Name:     conclusionArtifactName,
		Ref:      orchestration.ArtifactRef{Path: task.Source.URL},
		Metadata: metadata,
	}})
}

// =============================================================================
// Errors
// =============================================================================

// ErrUnknownConclusion indicates a conclusion reason with no row in the
// outcome table.
var ErrUnknownConclusion = errors.New("sdlc: conclusion reason has no outcome")

// UnknownConclusionError reports a conclusion the module cannot render.
//
// IT IS AN ERROR RATHER THAN A SHRUG on purpose: a reason orchestration adds
// without a row here would otherwise conclude a task in silence, which is the
// exact failure this table exists to remove.
type UnknownConclusionError struct {
	Reason string
	TaskID string
}

// Error implements the error interface.
func (e *UnknownConclusionError) Error() string {
	return ErrUnknownConclusion.Error() + ": reason=" + e.Reason + " task=" + e.TaskID
}

// Unwrap returns the sentinel for errors.Is support.
func (e *UnknownConclusionError) Unwrap() error { return ErrUnknownConclusion }
