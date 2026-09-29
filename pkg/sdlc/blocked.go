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

package sdlc

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// Surfacing — the park reason reaches the issue a person is watching
// =============================================================================
//
// A task the engine parks — a question only a person can settle, a review
// that did not converge, a budget spent — has stopped and will not start on
// its own. The Blocked hook posts why as a comment on the forge issue, so
// the person watching the issue sees it, and moves the display label. Only
// the RETRY is configured; what the comment says is the module's.

// SurfacePolicy bounds the delivery of a park reason to the forge.
type SurfacePolicy struct {
	// RetryAttempts is how many posts are tried before the hook fails.
	RetryAttempts int `mapstructure:"retry_attempts"`

	// RetryInterval paces the retries.
	RetryInterval time.Duration `mapstructure:"retry_interval"`

	// PostTimeout bounds one post.
	PostTimeout time.Duration `mapstructure:"post_timeout"`
}

// DefaultSurfacePolicy is the shipped retry.
var DefaultSurfacePolicy = SurfacePolicy{
	RetryAttempts: 5,
	RetryInterval: 2 * time.Minute,
	PostTimeout:   30 * time.Second,
}

// MinPostTimeout and MaxPostTimeout bound an explicit PostTimeout.
const (
	MinPostTimeout = time.Millisecond
	MaxPostTimeout = 24 * time.Hour
)

// Resolved fills every zero field from the default.
func (p SurfacePolicy) Resolved() SurfacePolicy {
	resolved := p
	if resolved.RetryAttempts == 0 {
		resolved.RetryAttempts = DefaultSurfacePolicy.RetryAttempts
	}
	if resolved.RetryInterval == 0 {
		resolved.RetryInterval = DefaultSurfacePolicy.RetryInterval
	}
	if resolved.PostTimeout == 0 {
		resolved.PostTimeout = DefaultSurfacePolicy.PostTimeout
	}
	return resolved
}

// Validate refuses a policy that would abandon every park or never post.
func (p SurfacePolicy) Validate() error {
	switch {
	case p.RetryAttempts < 0:
		return &InvalidInputError{Field: "surfacing.retry_attempts", Message: "a negative budget abandons every park before its first post; use zero to take the default"}
	case p.RetryInterval < 0:
		return &InvalidInputError{Field: "surfacing.retry_interval", Message: "a negative interval paces nothing; use zero to take the default"}
	case p.PostTimeout < 0:
		return &InvalidInputError{Field: "surfacing.post_timeout", Message: "a negative deadline cancels every post before it is made; use zero to take the default"}
	case p.PostTimeout > 0 && p.PostTimeout < MinPostTimeout:
		return &InvalidInputError{Field: "surfacing.post_timeout", Message: "a deadline this short cannot carry a forge round trip; use zero to take the default"}
	case p.PostTimeout > MaxPostTimeout:
		return &InvalidInputError{Field: "surfacing.post_timeout", Message: "a single post allowed more than a day is not a deadline"}
	}
	return nil
}

// surfacingMarker tags the comment so a re-read can find it.
const surfacingMarker = "quicbaas-sdlc:blocked"

// blockedMarker is the marker as it appears in a posted comment: the task,
// the attempt and the kind together identify ONE park.
//
// The attempt is what makes it an identity rather than a category. A task
// readmitted after a park is attempted again and may park again, and a
// resumed run asking the issue whether it has already posted must not find
// an earlier attempt's comment and stay silent.
func blockedMarker(taskID string, attempt int, kind OutcomeKind) string {
	marker := surfacingMarker
	if taskID != "" {
		marker += " task=" + taskID
	}
	if attempt > 0 {
		marker += " attempt=" + strconv.Itoa(attempt)
	}
	return marker + " kind=" + string(kind)
}

// blockedOutcome maps the engine's block kind to what the module announces
// for it. An unknown kind reads as the plain block, which is the honest
// answer: something parked the task and did not say which of the three it was.
func blockedOutcome(kind orchestration.TaskBlockKind) OutcomeKind {
	if outcome, ok := blockKindOutcomes[kind]; ok {
		return outcome
	}
	return OutcomeBlocked
}

// blockedLabelKey maps the engine's block kind to the display label key.
func blockedLabelKey(kind orchestration.TaskBlockKind) string {
	return outcomeSpecs[blockedOutcome(kind)].labelKey
}

// renderBlocked renders the comment the Blocked moment posts to the issue.
//
// It is renderOutcome with the row the block kind selects. The headline, the
// meaning, the label and the remedy all come from the one table in outcome.go
// that every ending is rendered from, so the escalation comment and the
// exhaustion comment are the same renderer with different rows.
func renderBlocked(binding *ProjectBinding, task *orchestration.Task, attempt int, reason orchestration.BlockReason, delivery *Delivery) string {
	return renderOutcome(outcomeReport{
		binding: binding, task: task, attempt: attempt,
		kind:    blockedOutcome(reason.Kind),
		message: reason.Message, iterations: reason.Iterations, delivery: delivery,
		faultClass: reason.FaultClass,
	})
}

// blockedCause names the environment fault a park came from, in one clause.
//
// WITHOUT IT THE OUTCOME LINE IS THE SAME SENTENCE FOR EVERY PARK. "The task
// waits on an external condition" is true of a spent spend cap, a saturated
// account pool and a full project alike, and the three want three different
// responses from the person reading the issue — raise the cap, add an
// account, wait. The engine already classified which one it was; this is
// where that classification stops being an internal field.
//
// IT IS NOT AN UnblockCondition AND MUST NOT BECOME ONE. A condition says
// what would RETURN the task to the queue and is checked by the sweep; this
// says what STOPPED it, and is a fact about an attempt that has already
// ended. The two are recorded in different places for that reason — the
// class on the task, the condition in the reason — and a fault class folded
// into the typed model would be a condition nothing can check.
//
// The meaning comes from the engine rather than from a table here, so a fault
// class added there cannot render as a bare identifier on somebody's issue.
// A class this build does not recognize — an agent's own terminal reason, or
// one added upstream of this binary — is shown as itself rather than
// explained wrongly.
//
// THE TASK'S RECORD WINS WHEN IT HAS ONE, and the run's own judgement stands in
// when it does not. The two orderings are the two moments this renders at: the
// conclusion seam reads a task the accounting has already written the class
// onto, while the Blocked moment posts BEFORE that write and has only the park
// the run is holding. Preferring the record keeps the durable copy
// authoritative; falling back to the park is what stops a comment naming no
// cause at the one moment the cause was just proved.
func blockedCause(task *orchestration.Task, parkClass string) string {
	class := parkClass
	if task != nil && task.BlockFaultClass != "" {
		class = task.BlockFaultClass
	}
	if class == "" {
		return ""
	}
	line := "**Cause:** `" + class + "`"
	if meaning, known := orchestration.FaultClassMeaning(class); known {
		line += " — " + meaning
	}
	return line
}

// blockedCondition reads the typed condition and the human detail out of a
// park reason, redacting the detail for publication either way.
//
// REDACTION HAPPENS AFTER THE SPLIT rather than before it. publicOutput
// bounds and scrubs text meant for a public issue and may rewrite it, so
// parsing the redacted form would let a redaction decide whether a condition
// was recorded at all.
//
// AN EMPTY ANSWER IS LEFT EMPTY. What a reason with nothing in it should say
// depends on the outcome being rendered — a park with no stated reason still
// has to tell the reader that, while a readmission has no park reason to
// invent — so the decision belongs to outcomeEvidence, which knows the row.
func blockedCondition(message string) (UnblockCondition, string, bool) {
	condition, detail, err := ParseBlockedReason(message)
	stated := publicOutput(detail)
	if err != nil {
		return UnblockCondition{}, stated, false
	}
	if stated == "" {
		// The condition IS the statement; a caller who composed one without
		// a detail said everything there was to say.
		stated = condition.Describe()
	}
	return condition, stated, true
}

// blockedClearedBy names what returns the task to the queue.
func blockedClearedBy(kind UnblockKind) string {
	if awaiting := kind.Awaiting(); awaiting != "" {
		return awaiting
	}
	return sdlcPrompts.MustRender("outcome_cleared_by_sweep", nil)
}

func quoteReason(reason string) string {
	lines := strings.Split(strings.TrimRight(reason, "\n"), "\n")
	var b strings.Builder
	for _, line := range lines {
		b.WriteString(strings.TrimRight("> "+line, " "))
		b.WriteString("\n")
	}
	return b.String()
}

func attemptLine(task *orchestration.Task) string {
	line := strconv.Itoa(task.ChargedAttempts())
	if budget := task.EffectiveAttemptBudget(); budget > 0 {
		line += " of " + strconv.Itoa(budget)
	}
	// THE REFUNDS ARE NAMED SEPARATELY because they mean opposite things to
	// whoever reads the comment: an environment fault is the factory being
	// broken, a question is the worker doing exactly what it should and
	// waiting on a person. Reporting a question as an environment fault would
	// send an operator to check a deployment that is fine.
	var refunds []string
	if task.EnvironmentFaults > 0 {
		refunds = append(refunds, countOf(task.EnvironmentFaults, "environment fault", "environment faults"))
	}
	if task.QuestionAttempts > 0 {
		refunds = append(refunds, countOf(task.QuestionAttempts, "question", "questions"))
	}
	if len(refunds) > 0 {
		line += " (" + strconv.Itoa(task.Attempts) + " dispatches, " +
			strings.Join(refunds, " and ") + ", not charged)"
	}
	return line
}

// countOf renders "1 question" / "3 questions". The attempt line is read by a
// person on a forge comment, so it is written like a sentence.
func countOf(n int, singular, plural string) string {
	if n == 1 {
		return "1 " + singular
	}
	return strconv.Itoa(n) + " " + plural
}

// labelTransition renders how the issue's labels move on a park.
func labelTransition(binding *ProjectBinding, key string) string {
	if binding == nil {
		return ""
	}
	trigger := strings.TrimSpace(binding.Forge.TriggerLabel)
	parked := binding.Forge.Label(key)
	switch {
	case trigger == "" && parked == "":
		return ""
	case key == TriggerLabelKey:
		// A readmission: the issue goes back to the trigger, the display
		// state of queued work, whatever park label it carried.
		return "`" + trigger + "` kept until the next attempt starts"
	case parked == "":
		return "`" + trigger + "` removed"
	case trigger == "":
		return "`" + parked + "` applied"
	default:
		return "`" + trigger + "` → `" + parked + "`"
	}
}

// postWithRetry posts the comment, retrying on the policy's schedule. Each
// attempt is bounded by PostTimeout; the wait between attempts ends early
// when ctx does.
func postWithRetry(ctx context.Context, forge ForgeBackend, repo RepoRef, number int, body string, policy SurfacePolicy) (*ForgeComment, error) {
	resolved := policy.Resolved()
	var last error
	for attempt := 1; attempt <= resolved.RetryAttempts; attempt++ {
		postCtx, cancel := context.WithTimeout(ctx, resolved.PostTimeout)
		comment, err := forge.CommentOnIssue(postCtx, repo, number, body)
		cancel()
		if err == nil && comment != nil && comment.ID > 0 {
			return comment, nil
		}
		if err == nil {
			err = &ForgeWriteNotAppliedError{Type: forge.Type(), Operation: "CommentOnIssue", Subject: repo.String() + "#" + strconv.Itoa(number)}
		}
		last = err
		if attempt == resolved.RetryAttempts {
			break
		}
		timer := time.NewTimer(resolved.RetryInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	return nil, &SurfacingUndeliveredError{IssueNumber: number, Attempts: resolved.RetryAttempts, Cause: last}
}

// SurfacingUndeliveredError reports a park reason that never reached the
// issue within the retry budget.
type SurfacingUndeliveredError struct {
	IssueNumber int
	Attempts    int
	Cause       error
}

// Error implements the error interface.
func (e *SurfacingUndeliveredError) Error() string {
	msg := "sdlc: the park reason did not reach issue #" + strconv.Itoa(e.IssueNumber) +
		" after " + strconv.Itoa(e.Attempts) + " attempt(s)"
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap returns the cause for errors.Is/As support.
func (e *SurfacingUndeliveredError) Unwrap() error { return e.Cause }
