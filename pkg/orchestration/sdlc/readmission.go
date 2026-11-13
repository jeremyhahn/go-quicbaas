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
	"errors"
	"strings"
	"time"
)

// =============================================================================
// Readmission conditions — what a blocked task waits on, in a checkable form
// =============================================================================
//
// A BLOCKED TASK MUST NAME WHAT WOULD UNBLOCK IT, IN A FORM SOMETHING CAN CHECK.
//
// Prose has one failure mode and it is total: nothing can evaluate it, so
// nothing ever re-examines the task, and the condition clears without the
// task moving. A block written as "waiting on go-frost" is indistinguishable
// from work nobody filed the moment the person who wrote it looks away.
//
// So a condition here is a VALUE: a kind from a closed set, a subject and a
// target, which UnblockSweeper evaluates on every pass. Two of the kinds are
// decided by a CHECK against the forge; two are decided by a PERSON and say
// so, which is the part prose could not do either — "waiting on the owner for
// a ruling" and "waiting on nothing" used to render identically.
//
// NOT EVERY BLOCKED TASK CAN CARRY ONE, AND THAT IS CORRECT RATHER THAN A GAP.
// The engine blocks a reaped death because NOTHING DIAGNOSED IT — that is the
// stated reason a death with a spent budget goes to blocked rather than back
// to pending (`orchestration/task_attempt.go`, applyDeath) — so there is no
// condition to state, and manufacturing a kind for it would fabricate a
// diagnosis nobody made. Such a task is reported as awaiting a human
// diagnosis, on every pass, with the evidence it carries. The difference from
// the prose era is not that everything is typed now; it is that an untyped
// task is RE-EXAMINED and REPORTED instead of sitting where nothing looks.
//
// WHERE IT IS STORED, and this is a deliberate choice rather than the obvious
// one. The condition rides Task.BlockedReason, encoded as a marker inside the
// operator-facing text, rather than living in a record of its own. A separate
// record would be a SECOND WRITE: `block()` sets the task's status and its
// reason in one update precisely so no window exists where one landed and the
// other did not, and splitting them would reintroduce that window at the exact
// seam whose symptom is a task blocked with no stated condition — the failure
// this whole mechanism exists to remove. One field, one write, one truth.
//
// THE MARKER IS FOUND, NOT ASSUMED TO BE LAST. A reason composed here leads
// with the sentence a person reads and ends with the marker, but the engine
// wraps a park reason in prose of its own on both sides (applyDeath appends
// the budget note), so an end-anchored parser would read the engine's own
// death evidence as unconditioned.
//
// The verb that readmits a task is orchestration's (orchestrator:task_readmit)
// and the sweeper calls it; this file is the vocabulary and its encoding.

// UnblockKind names what must happen before a blocked task is readmitted.
//
// A CLOSED SET. An unrecognised kind is refused rather than passed through,
// because the only thing a caller can do with a condition it cannot classify
// is guess, and the guess that costs least to make — "nothing to check" —
// reads downstream as a satisfied condition.
type UnblockKind string

const (
	// UnblockPipelineGreen waits for the forge's checks on a commit to go
	// green: Subject is the head SHA.
	//
	// Bound to a SHA rather than to a branch for the reason a review verdict
	// is: a branch's green run says nothing about the tree this task waits on
	// once the branch has moved.
	UnblockPipelineGreen UnblockKind = "pipeline_green"

	// UnblockForgeIssueState waits for another forge issue to reach a state:
	// Subject is the issue reference (owner/repo#number), Target the state.
	//
	// This is dependency ordering as the factory actually meets it — an issue
	// that cannot be worked until the one it consumes is closed, in this
	// repository or in another on the same forge.
	UnblockForgeIssueState UnblockKind = "forge_issue_state"

	// UnblockReleaseTag waits for a version tag to exist in a repository:
	// Subject is the repository (owner/repo), Target the tag.
	//
	// MERGED IS NOT RELEASED, AND THIS IS THE DIFFERENCE. A dependency whose
	// fix is merged closes its issue and satisfies forge_issue_state, while
	// every consumer that pins a module version still resolves the OLD code —
	// because a version pin resolves against a TAG, and the merge created no
	// tag. A consumer unblocked on the close would rebuild against the
	// dependency it was waiting to stop using, get the same failure, and have
	// nothing in its record explaining why.
	//
	// Bound to a tag NAME rather than to a version range. A range needs an
	// ordering over versions, and this tree's dependency convention pins exact
	// tags, so a range would be a comparison nobody's build performs —
	// answering a question no consumer asked.
	UnblockReleaseTag UnblockKind = "release_tag"

	// UnblockOwnerRuling waits for the owner to decide something no agent may
	// decide — architecture, priority, a policy conflict.
	//
	// NEVER machine-satisfiable, and that is the honest encoding rather than a
	// limitation. The SDLC convention reserves these calls for the owner, so a
	// factory that could clear this condition automatically would be clearing
	// exactly the class of question it is forbidden to answer.
	UnblockOwnerRuling UnblockKind = "owner_ruling"

	// UnblockOperatorRepair waits for a person to repair a fault in the
	// factory itself — a runner that cannot reach its forge, a worker whose
	// death nobody has diagnosed.
	//
	// Distinct from an owner ruling because the two summon different people:
	// this needs whoever runs the fleet, not whoever decides the architecture.
	UnblockOperatorRepair UnblockKind = "operator_repair"
)

// String returns the kind's name.
func (k UnblockKind) String() string { return string(k) }

// UnblockCondition is one typed condition: what would return one blocked task
// to the queue.
//
// Subject and Target are constrained to a reserved-character-free charset at
// ENCODE time rather than escaped at parse time. That makes the encoding
// unambiguous by construction: there is no input for which the parser has to
// decide whether a separator was data, so it has no escaping rules to get
// wrong — the careful behaviour moved to the writer.
type UnblockCondition struct {
	// Kind classifies the condition and decides how it is checked.
	Kind UnblockKind

	// Subject is what the condition is about: a commit SHA for
	// pipeline_green, an issue reference for forge_issue_state. Empty for
	// kinds that read none.
	Subject string

	// Target is the state the subject must reach, for kinds that name one.
	Target string
}

// =============================================================================
// The kind registry
// =============================================================================

// unblockScope is everything a check is given: the forge the project's
// binding names, the repository the blocked task came from, and the clock.
//
// The repository is the task's OWN, and is the default a subject that names
// no repository resolves against. A subject may name another one — that is
// the cross-repository dependency the whole kind exists for — and the adapter
// takes the repository per call, so one forge serves both.
type unblockScope struct {
	forge ForgeBackend
	repo  RepoRef
	now   func() time.Time
}

// unblockChecker decides whether one condition is satisfied.
//
// It answers (false, error) for undecidable — never (false, nil) — because a
// caller cannot distinguish "not yet" from "could not tell" if both arrive as
// an unsatisfied condition, and the two differ by whether an operator has an
// outage to fix.
type unblockChecker func(ctx context.Context, scope unblockScope, c UnblockCondition) (bool, error)

// fieldRule says whether a condition's Subject or Target must be present.
//
// Present-and-forbidden are both stated, and FORBIDDEN IS THE HALF THAT EARNS
// ITS KEEP: a field a kind does not read is a field an operator can set and
// watch be ignored. An owner ruling carrying a Subject naming the issue it
// depends on would read as a dependency to anyone looking at the record, and
// no check would ever consult it.
type fieldRule int

const (
	fieldForbidden fieldRule = iota
	fieldRequired
)

// unblockKindSpec is everything the module knows about one kind of condition.
//
// ONE MAP, and the invariant it enforces is that every kind is decided by
// EXACTLY ONE of two things: a check, or a named person. A kind with neither
// is a kind nothing will ever evaluate, and a permissive default would let it
// through as "nothing to check" — indistinguishable downstream from a
// condition that has been met. So the seam refuses instead, and a kind added
// without a decider stops the first sweep that meets it rather than releasing
// a task on a condition nobody evaluated.
type unblockKindSpec struct {
	// subject and target say which fields this kind reads.
	subject fieldRule
	target  fieldRule

	// awaiting names the person who clears this condition, and is non-empty
	// EXACTLY when check is nil.
	awaiting string

	// check decides the condition, and is nil exactly when awaiting is set.
	check unblockChecker

	// describe renders the condition for a person.
	describe func(c UnblockCondition) string
}

// unblockKinds is the vocabulary: which fields each kind reads, who or what
// decides it, and how it reads to a person.
//
// Map-based rather than a switch so the set is data a completeness test can
// walk and a reader can audit at a glance.
var unblockKinds = map[UnblockKind]unblockKindSpec{
	UnblockPipelineGreen: {
		subject: fieldRequired,
		target:  fieldForbidden,
		check:   checkPipelineGreen,
		describe: func(c UnblockCondition) string {
			return "the forge's checks on commit " + c.Subject + " must go green"
		},
	},
	UnblockForgeIssueState: {
		subject: fieldRequired,
		target:  fieldRequired,
		check:   checkForgeIssueState,
		describe: func(c UnblockCondition) string {
			return "issue " + c.Subject + " must be " + c.Target
		},
	},
	UnblockReleaseTag: {
		subject: fieldRequired,
		target:  fieldRequired,
		check:   checkReleaseTag,
		describe: func(c UnblockCondition) string {
			return "repository " + c.Subject + " must have released tag " + c.Target
		},
	},
	UnblockOwnerRuling: {
		subject:  fieldForbidden,
		target:   fieldForbidden,
		awaiting: "the owner",
		describe: func(UnblockCondition) string {
			return "the owner must rule on the question recorded with this task"
		},
	},
	UnblockOperatorRepair: {
		subject:  fieldForbidden,
		target:   fieldForbidden,
		awaiting: "an operator",
		describe: func(UnblockCondition) string {
			return "an operator must repair the fault recorded with this task"
		},
	},
}

// forgeIssueStates is the closed set of states a forge_issue_state condition
// may name, matching what a forge reports on ForgeIssue.State.
var forgeIssueStates = map[string]struct{}{
	"open":   {},
	"closed": {},
}

// Valid reports whether the kind is in the vocabulary.
func (k UnblockKind) Valid() bool {
	_, ok := unblockKinds[k]
	return ok
}

// Awaiting names the person who clears this kind, and is empty for a kind a
// check decides.
//
// The empty answer is a fact about the CONDITION rather than about this
// package's completeness: a pipeline going green waits on no person, so a
// caller that reads "" here should evaluate the check, not file a gap.
func (k UnblockKind) Awaiting() string {
	return unblockKinds[k].awaiting
}

// MachineCheckable reports whether a check can decide this kind.
func (k UnblockKind) MachineCheckable() bool {
	spec, ok := unblockKinds[k]
	return ok && spec.check != nil
}

// Describe renders the condition for a person.
func (c UnblockCondition) Describe() string {
	spec, ok := unblockKinds[c.Kind]
	if !ok {
		return "an unknown condition of kind " + string(c.Kind)
	}
	return spec.describe(c)
}

// =============================================================================
// Checks
// =============================================================================

// checkPipelineGreen decides a dependency on a commit's checks.
//
// GREEN IS THE GATE'S OWN GREEN, evaluated through evaluateChecks so a
// condition cannot mean something weaker here than it does at the merge gate.
// A commit with no runs at all is NOT green — the absence of a failure is not
// a success, and the two are the same record set from a query's view.
func checkPipelineGreen(ctx context.Context, scope unblockScope, c UnblockCondition) (bool, error) {
	runs, err := scope.forge.ListCheckRuns(ctx, scope.repo, c.Subject)
	if err != nil {
		return false, err
	}
	reason, _ := evaluateChecks(checkRecordsFor(runs, c.Subject, scope.now()), c.Subject)
	return reason == GateClear, nil
}

// checkForgeIssueState decides a dependency on another issue's state.
//
// AN ISSUE THAT IS NOT THERE IS UNDECIDABLE, NOT SATISFIED. A task waiting on
// a record that has gone is waiting on something nobody can evaluate, and
// resolving it silently — either way — hides a dangling reference that will
// never clear.
//
// The subject names its repository in full, even when it is the task's own.
// A bare number would be shorter and would mean different issues depending
// on which task carried it, which is the ambiguity a cross-repository
// dependency exists to state — so a subject that is not a full reference is
// reported here, on every pass, rather than refused once at encode time and
// then never mentioned again.
func checkForgeIssueState(ctx context.Context, scope unblockScope, c UnblockCondition) (bool, error) {
	repo, number, err := ParseIssueRef(c.Subject)
	if err != nil {
		return false, err
	}
	issue, err := scope.forge.GetIssue(ctx, repo, number)
	if err != nil {
		return false, err
	}
	// AN ADAPTER THAT ANSWERS NEITHER A RECORD NOR AN ERROR HAS ANSWERED NOTHING,
	// and the condition is undecidable rather than unsatisfied. The distinction
	// used to be moot because nothing but a sweep reached this check and a sweep
	// only ever read the repository's own issues; the dependency walk reaches it
	// for issues in OTHER repositories, through whatever adapter that forge has,
	// and a nil dereference there would end a sweep for every project.
	if issue == nil {
		return false, &ForgeWriteNotAppliedError{
			Type:      scope.forge.Type(),
			Operation: "GetIssue",
			Subject:   c.Subject,
		}
	}
	return strings.EqualFold(strings.TrimSpace(issue.State), c.Target), nil
}

// checkReleaseTag decides a dependency on a published version.
//
// EXISTENCE IS THE WHOLE TEST, and it is asked of the forge rather than of a
// build: the tag is what a version pin resolves against, so a tag that exists is
// a version a consumer can select and a tag that does not is one it cannot. No
// artifact registry is consulted, because this tree's dependency convention
// resolves modules from forge tags directly.
//
// AN ABSENT TAG IS UNSATISFIED, NOT UNDECIDABLE — the one place in this file
// where a NotFound is an answer rather than a fault. A repository reporting no
// v1.2.3 has told us precisely what we asked: it has not been released. Every
// other failure stays undecidable and leaves the task blocked.
//
// The subject names the repository in full. A bare name would mean different
// repositories depending on which task carried it, which is the ambiguity a
// cross-repository dependency exists to state.
func checkReleaseTag(ctx context.Context, scope unblockScope, c UnblockCondition) (bool, error) {
	repo, err := ParseRepoRef(c.Subject)
	if err != nil {
		return false, err
	}
	tag, err := scope.forge.GetTag(ctx, repo, c.Target)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	// A forge answering 200 with no tag name has not confirmed the tag. Reading
	// that as a release would satisfy the condition on an empty body, which is
	// the one shape a malfunctioning proxy produces most easily.
	return tag != nil && strings.TrimSpace(tag.Name) != "", nil
}

// =============================================================================
// Validation and encoding
// =============================================================================

const (
	// unblockMarkerOpen introduces the machine-readable half of a blocked
	// reason and unblockMarkerClose ends it.
	unblockMarkerOpen  = "unblock["
	unblockMarkerClose = "]"

	// unblockReserved are the characters the encoding uses as structure, and
	// which therefore may not appear inside a value. Refused at the door
	// rather than escaped: see UnblockCondition.
	unblockReserved = "[];=" + " \t\r\n"
)

// Validate reports whether the condition is complete, encodable, and of a
// kind something will decide.
func (c UnblockCondition) Validate() error {
	spec, ok := unblockKinds[c.Kind]
	if !ok {
		return &UnblockKindError{Kind: string(c.Kind), Sentinel: ErrUnblockKindUnknown}
	}
	if (spec.check == nil) == (spec.awaiting == "") {
		// Neither decided nor delegated, or claimed as both. The vocabulary
		// is wrong rather than the caller, and it is caught here so it cannot
		// reach a sweep as a task released on an unevaluated condition.
		return &UnblockKindError{Kind: string(c.Kind), Sentinel: ErrUnblockKindUnhandled}
	}
	if err := validateUnblockField("subject", spec.subject, c.Subject); err != nil {
		return err
	}
	if err := validateUnblockField("target", spec.target, c.Target); err != nil {
		return err
	}
	if c.Kind == UnblockForgeIssueState {
		if _, ok := forgeIssueStates[strings.ToLower(c.Target)]; !ok {
			return &InvalidInputError{
				Field:   "target",
				Message: "an issue condition must name a state a forge reports (open, closed), got " + c.Target,
			}
		}
	}
	// WHETHER THE SUBJECT NAMES ANYTHING IS THE CHECK'S QUESTION, not this
	// one. A reference that is well formed as text and names nothing is
	// indistinguishable here from one that names something, so refusing the
	// malformed half at encode time would only move a dangling reference's
	// discovery from "undecidable, on every pass" to "silently absent".
	return nil
}

// validateUnblockField applies one field's rule and the charset.
func validateUnblockField(field string, rule fieldRule, value string) error {
	switch {
	case rule == fieldRequired && value == "":
		return &InvalidInputError{Field: field, Message: "this condition kind is meaningless without one"}
	case rule == fieldForbidden && value != "":
		return &InvalidInputError{
			Field:   field,
			Message: "this condition kind reads no " + field + ", and a value nothing reads is one an operator will believe",
		}
	case strings.ContainsAny(value, unblockReserved):
		return &InvalidInputError{
			Field:   field,
			Message: "must not contain the encoding's reserved characters (" + unblockReserved + ")",
		}
	}
	return nil
}

// Encode renders the condition as the marker a sweep parses back.
//
// It validates first, so an ill-formed condition cannot reach a stored
// record. The alternative — rendering whatever it was given and failing at
// parse — puts the diagnosis on the reader of a record written hours earlier
// by a caller nobody can identify.
func (c UnblockCondition) Encode() (string, error) {
	if err := c.Validate(); err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString(unblockMarkerOpen)
	b.WriteString("kind=")
	b.WriteString(string(c.Kind))
	if c.Subject != "" {
		b.WriteString(";subject=")
		b.WriteString(c.Subject)
	}
	if c.Target != "" {
		b.WriteString(";target=")
		b.WriteString(c.Target)
	}
	b.WriteString(unblockMarkerClose)
	return b.String(), nil
}

// ComposeBlockedReason renders the reason a parked task carries: the
// condition as a sentence, the caller's detail, and the marker.
//
// The marker goes LAST because everything before it is what a person reads,
// and an operator scanning a queue should not have to step over machine text
// to reach the sentence. Parsing does not depend on the position.
func ComposeBlockedReason(condition UnblockCondition, detail string) (string, error) {
	marker, err := condition.Encode()
	if err != nil {
		return "", err
	}
	trimmed := strings.TrimSpace(detail)
	if strings.Contains(trimmed, unblockMarkerOpen) {
		return "", &UnblockConditionError{
			Reason:   trimmed,
			Detail:   "the detail text may not contain an unblock marker; it would inject a condition",
			Sentinel: ErrUnblockConditionMalformed,
		}
	}
	var b strings.Builder
	b.WriteString(condition.Describe())
	if trimmed != "" {
		b.WriteString(": ")
		b.WriteString(trimmed)
	}
	b.WriteString(" ")
	b.WriteString(marker)
	return b.String(), nil
}

// ParseUnblockCondition recovers the condition a blocked reason carries.
//
// Absence is reported as its own error rather than as a zero condition,
// because the two demand different things: a zero condition would be checked,
// found unsatisfiable, and reported as a kind nothing handles, sending an
// operator after a parser bug when the actual finding is that nothing
// recorded a condition at all.
func ParseUnblockCondition(reason string) (UnblockCondition, error) {
	condition, _, err := parseUnblockMarker(reason)
	return condition, err
}

// parseUnblockMarker is ParseUnblockCondition plus the raw marker text it
// read, which the surface needs so it can remove exactly what it parsed
// rather than re-encoding the condition and hoping the two agree.
func parseUnblockMarker(reason string) (UnblockCondition, string, error) {
	open := strings.LastIndex(reason, unblockMarkerOpen)
	if open < 0 {
		return UnblockCondition{}, "", &UnblockConditionError{
			Reason:   reason,
			Detail:   "no unblock condition is recorded in the reason",
			Sentinel: ErrUnblockConditionAbsent,
		}
	}
	rest := reason[open+len(unblockMarkerOpen):]
	end := strings.Index(rest, unblockMarkerClose)
	if end < 0 {
		return UnblockCondition{}, "", &UnblockConditionError{
			Reason:   reason,
			Detail:   "the unblock marker is not closed",
			Sentinel: ErrUnblockConditionMalformed,
		}
	}
	marker := reason[open : open+len(unblockMarkerOpen)+end+len(unblockMarkerClose)]
	var condition UnblockCondition
	seen := make(map[string]bool, 3)
	for _, field := range strings.Split(rest[:end], ";") {
		key, value, ok := strings.Cut(field, "=")
		if !ok {
			return UnblockCondition{}, "", &UnblockConditionError{
				Reason: reason, Detail: "field " + field + " is not key=value", Sentinel: ErrUnblockConditionMalformed,
			}
		}
		if seen[key] {
			// Two values for one field, and no rule says which wins. Taking
			// either would make the record mean something it does not say.
			return UnblockCondition{}, "", &UnblockConditionError{
				Reason: reason, Detail: "field " + key + " appears more than once", Sentinel: ErrUnblockConditionMalformed,
			}
		}
		seen[key] = true
		setter, known := unblockFieldSetters[key]
		if !known {
			return UnblockCondition{}, "", &UnblockConditionError{
				Reason: reason, Detail: "unknown field " + key, Sentinel: ErrUnblockConditionMalformed,
			}
		}
		setter(&condition, value)
	}
	if err := condition.Validate(); err != nil {
		if errors.Is(err, ErrUnblockKindUnknown) || errors.Is(err, ErrUnblockKindUnhandled) {
			return UnblockCondition{}, "", err
		}
		return UnblockCondition{}, "", &UnblockConditionError{
			Reason: reason, Detail: "the recorded condition is not usable: " + err.Error(), Sentinel: ErrUnblockConditionMalformed,
		}
	}
	return condition, marker, nil
}

// unblockFieldSetters is the marker's field vocabulary, keyed for O(1)
// dispatch: a key absent from it is a field this package does not define.
var unblockFieldSetters = map[string]func(*UnblockCondition, string){
	"kind":    func(c *UnblockCondition, v string) { c.Kind = UnblockKind(v) },
	"subject": func(c *UnblockCondition, v string) { c.Subject = v },
	"target":  func(c *UnblockCondition, v string) { c.Target = v },
}

// ParseBlockedReason splits a composed reason back into the condition and the
// detail a person wrote, with the machine marker and the condition's own
// sentence removed.
//
// It exists for the SURFACE: the comment posted to the issue derives its
// prose from the typed condition, and quoting the stored reason verbatim
// would show a reader the marker and say the condition twice. A reason no
// composer here wrote — the engine's own prose park — yields its whole text
// as the detail and the absent-condition error, which is the honest reading
// of a block that states no condition.
func ParseBlockedReason(reason string) (UnblockCondition, string, error) {
	condition, marker, err := parseUnblockMarker(reason)
	if err != nil {
		return UnblockCondition{}, strings.TrimSpace(reason), err
	}
	detail := strings.TrimSpace(strings.Replace(reason, marker, "", 1))
	detail = strings.TrimSpace(strings.TrimPrefix(detail, condition.Describe()))
	return condition, strings.TrimSpace(strings.TrimPrefix(detail, ":")), nil
}
