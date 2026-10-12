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
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/prompts"
)

// =============================================================================
// The worker's turn is evidence, and a turn that changed nothing is a verdict
// =============================================================================
//
// WHAT THE WORKER SAID IS KEPT. Every turn is recorded on the job as an event
// — the attempt, the round, how long it took, what the reply carried, and the
// reply itself — because a turn whose text is discarded leaves nobody able to
// say whether the worker asked a question, cited a rule it may not break, or
// died. Three attempts at go-truststrap#26 were spent proving that
// (go-quicbaas#787): each ran the same prompt, each was charged as a failure
// of the work, and the job records held four events, none of them the
// worker's answer.
//
// AND A TURN THAT CHANGED NOTHING IS CLASSIFIED FROM STRUCTURE, NEVER FROM
// PROSE. The engine already refuses to grep a message for a decision that
// costs a task its budget (orchestration/task_attempt.go), and this decision
// costs exactly that. So the charter requires a worker that stops to END ITS
// REPLY WITH A DECLARATION — a marker and a code from a closed set — and this
// file reads that declaration and nothing else. A reply with no declaration
// is a reply claiming it did the work, which on an unchanged tree is the one
// case that is still a failure of the work.
//
// AND A REPLY THAT NEVER READ THE PROMPT IS NOT A VERDICT ON ANYTHING. A
// persistent session can have a turn's worth of its own queued — an expired
// background task, a notification armed two turns ago — and answer THAT when
// the next prompt arrives: text, no tool use, nothing changed, back in half a
// second. Nothing judged the work, so the prompt is sent again rather than
// charged (NonAnswer, MaxNonAnswerTurns).
//
// THE ABSENT CASES ARE NAMED RATHER THAN GUESSED. An empty reply is a worker
// that died mid-turn; a declaration whose code is not in the vocabulary is a
// worker this binary does not understand. Neither is silently retried until
// the budget is gone: the first is retried ONCE and then escalated with the
// evidence, and the second parks for a person, because a guess in either
// direction is a guess about work nobody can see.

// TurnStopMarker introduces the worker's stop declaration. It is the whole
// machine-readable surface of a reply: everything else in the text is for a
// person.
const TurnStopMarker = "SDLC-STOP:"

// The stop codes a worker may declare. A CLOSED SET — a code outside it is
// refused rather than passed through, because the cheapest guess to make
// about an unknown code ("carry on") reads downstream as a worker that had
// nothing to say.
const (
	// TurnStopQuestion is a worker that needs an answer only a person has.
	TurnStopQuestion = "question"

	// TurnStopOwnerDecision is a worker that reached a decision the SDLC
	// convention reserves for the owner — an architecture or feature call, an
	// approval it does not hold, or a company convention the task conflicts
	// with.
	TurnStopOwnerDecision = "owner-decision"

	// TurnStopEnvironment is a worker whose workspace, tooling or
	// dependencies made the work impossible, where no answer would change
	// that.
	TurnStopEnvironment = "environment"

	// TurnStopSatisfied is a worker that read the repository and found the
	// change this task asks for ALREADY PRESENT, so it made none.
	//
	// It is not a question, not a ruling and not a fault: nobody needs to
	// decide anything and nothing is broken. Without a code of its own a
	// worker in this position had only `owner-decision` or silence to choose
	// from, and both are wrong — one fills the owner's queue with items
	// needing no decision, the other spends an attempt and looks like a
	// worker that did nothing.
	TurnStopSatisfied = "satisfied"
)

// TurnOutcome is what one worker turn that changed no file WAS.
type TurnOutcome string

const (
	// TurnOutcomeSilent is a turn that produced no text at all: a worker that
	// died, or a backend that answered with nothing.
	TurnOutcomeSilent TurnOutcome = "silent"

	// TurnOutcomeQuestion is a declared stop on a question for a person.
	TurnOutcomeQuestion TurnOutcome = "question"

	// TurnOutcomeOwnerRuling is a declared stop on a decision only the owner
	// may make.
	TurnOutcomeOwnerRuling TurnOutcome = "owner_ruling"

	// TurnOutcomeEnvironment is a declared stop on the environment.
	TurnOutcomeEnvironment TurnOutcome = "environment"

	// TurnOutcomeUnreadable is a declaration whose code this binary does not
	// know. It parks rather than retrying: the worker stated that it stopped,
	// and only the reason is unreadable.
	TurnOutcomeUnreadable TurnOutcome = "unreadable"

	// TurnOutcomeClaimedWork is a reply that declared no stop — a worker
	// saying it did the work — on a tree it did not touch. This is the ONE
	// no-change turn that is still a failure of the work.
	TurnOutcomeClaimedWork TurnOutcome = "claimed_work"

	// TurnOutcomeSatisfied is a no-change turn that is CORRECT: the work was
	// already done. It is the only outcome here that concludes the task
	// instead of parking or charging it.
	TurnOutcomeSatisfied TurnOutcome = "satisfied"

	// TurnOutcomeNonAnswer is a reply that answered SOMETHING OTHER THAN THE
	// PROMPT: text, no stop declaration, no tool use, no file touched, and
	// back before the prompt could have been read. See NonAnswer.
	TurnOutcomeNonAnswer TurnOutcome = "non_answer"
)

// String returns the outcome's name.
func (o TurnOutcome) String() string { return string(o) }

// stopCodeOutcomes is the vocabulary: the code a worker declares, and what
// that turn was. Map-based so a code answers in constant time and the set is
// data a completeness test can walk.
var stopCodeOutcomes = map[string]TurnOutcome{
	TurnStopQuestion:      TurnOutcomeQuestion,
	TurnStopOwnerDecision: TurnOutcomeOwnerRuling,
	TurnStopEnvironment:   TurnOutcomeEnvironment,
	TurnStopSatisfied:     TurnOutcomeSatisfied,
}

// TurnAction is what the runner DOES with a no-change turn.
type TurnAction string

const (
	// TurnActionPark stops the task and hands it to a person, with the
	// worker's own text as the reason.
	TurnActionPark TurnAction = "park"

	// TurnActionRetry prompts the same session once more, with what happened
	// added to the prompt. It is bounded by MaxSilentTurns.
	TurnActionRetry TurnAction = "retry"

	// TurnActionCharge fails the job as a failure of the work, which spends
	// one of the task's attempts.
	TurnActionCharge TurnAction = "charge"

	// TurnActionConclude ends the task as done-with-nothing-to-do: it charges
	// no attempt, hands nobody a decision, and says so on the issue so a
	// person can close or restate it. The only action here that is neither a
	// park nor a failure.
	TurnActionConclude TurnAction = "conclude"

	// TurnActionReprompt sends the SAME prompt once more with one line
	// saying the previous reply did not address it. It is neither a new
	// round nor a new attempt, and it is bounded by MaxNonAnswerTurns.
	TurnActionReprompt TurnAction = "reprompt"
)

// String returns the action's name.
func (a TurnAction) String() string { return string(a) }

// TurnVerdict is what one classified turn means for the run: what to do, how
// the task is parked when it parks, and the sentence a person reads above the
// worker's own words.
type TurnVerdict struct {
	// Outcome is what the turn was.
	Outcome TurnOutcome

	// Action is what the runner does about it.
	Action TurnAction

	// BlockKind is the kind the task is parked with, empty for an action
	// that does not park.
	BlockKind orchestration.TaskBlockKind

	// Condition is the typed unblock condition the park carries: what would
	// return the task to the queue, in the form UnblockSweeper evaluates.
	// Empty for an action that does not park.
	//
	// NEITHER OF THE TWO KINDS USED HERE IS MACHINE-CHECKABLE, and that is
	// the honest encoding rather than a gap. A worker that asked a question
	// is waiting on a person by construction; a factory that could clear the
	// condition itself would be answering exactly the class of question the
	// SDLC convention forbids it to answer.
	Condition UnblockKind

	// FaultClass attributes a parking outcome to the FACTORY rather than to
	// the work, as one of orchestration's operator fault classes. Empty for
	// every outcome that is a judgement about the work's own content.
	//
	// IT IS SET EXACTLY WHERE Condition IS UnblockOperatorRepair, and the two
	// say the same thing to different readers: the condition says who must act
	// before the task returns, the class says who is not charged for it. A row
	// that waits on an operator and still spends the task's attempt budget is
	// the accounting defect go-quicbaas#908 measured — two of the task's three
	// attempts spent on a workspace nobody had mounted.
	FaultClass string

	// Headline is the sentence that introduces the worker's text wherever
	// the outcome is reported.
	Headline string
}

// turnVerdicts is the classification table: one row per outcome, keyed for
// constant-time lookup. Every outcome has a row, and a test walks the set to
// prove it — an outcome with no row would be an outcome the runner could not
// act on.
var turnVerdicts = map[TurnOutcome]TurnVerdict{
	TurnOutcomeSilent: {
		Outcome:  TurnOutcomeSilent,
		Action:   TurnActionRetry,
		Headline: "The worker's turn ended with no reply and no changes.",
	},
	TurnOutcomeQuestion: {
		Outcome:   TurnOutcomeQuestion,
		Action:    TurnActionPark,
		BlockKind: orchestration.TaskBlockKindNeedsInfo,
		Condition: UnblockOwnerRuling,
		Headline:  "The worker stopped on a question it cannot answer from the repository.",
	},
	TurnOutcomeOwnerRuling: {
		Outcome:   TurnOutcomeOwnerRuling,
		Action:    TurnActionPark,
		BlockKind: orchestration.TaskBlockKindNeedsInfo,
		Condition: UnblockOwnerRuling,
		Headline:  "The worker stopped on a decision the SDLC convention reserves for the owner.",
	},
	TurnOutcomeEnvironment: {
		Outcome:    TurnOutcomeEnvironment,
		Action:     TurnActionPark,
		BlockKind:  orchestration.TaskBlockKindBlocked,
		Condition:  UnblockOperatorRepair,
		FaultClass: orchestration.FaultWorkerEnvironment,
		Headline:   "The worker stopped because its workspace or tooling made the work impossible.",
	},
	TurnOutcomeUnreadable: {
		Outcome:    TurnOutcomeUnreadable,
		Action:     TurnActionPark,
		BlockKind:  orchestration.TaskBlockKindBlocked,
		Condition:  UnblockOperatorRepair,
		FaultClass: orchestration.FaultWorkerEnvironment,
		Headline: "The worker declared a stop with a code this binary does not know, so " +
			"nothing could act on it automatically.",
	},
	TurnOutcomeClaimedWork: {
		Outcome:  TurnOutcomeClaimedWork,
		Action:   TurnActionCharge,
		Headline: "The worker reported work it did not do: the checkout is unchanged.",
	},
	TurnOutcomeNonAnswer: {
		Outcome: TurnOutcomeNonAnswer,
		Action:  TurnActionReprompt,
		// No BlockKind, no Condition and no FaultClass: nothing is parked,
		// nobody is waiting and nothing is judged. The same prompt is simply
		// asked again.
		Headline: "The worker's reply did not address the prompt it was given.",
	},
	TurnOutcomeSatisfied: {
		Outcome: TurnOutcomeSatisfied,
		Action:  TurnActionConclude,
		// No BlockKind, no Condition and no FaultClass: nothing is parked and
		// nothing is at fault, which is what makes this outcome different from
		// every other row in this table.
		Headline: "The worker found the change this task asks for already present, so it made none.",
	},
}

// MaxSilentTurns bounds how many times one attempt re-prompts a session that
// answered with nothing.
//
// ONE RETRY, because the two things a silent turn can be need exactly one
// more sample to tell apart: a transport that dropped a single answer is
// gone by the next prompt, and a worker whose process died answers with
// nothing again. A second retry would only spend more of the same attempt on
// a session that has already proven it is not there.
const MaxSilentTurns = 1

// NonAnswerTurnBound is how fast a reply must come back to be evidence that
// the worker never read the prompt the turn carried.
//
// MEASURED AS THE TURN'S OWN ELAPSED TIME: the wall time between the prompt
// leaving runTurn and the backend's answer arriving, which is the same
// `elapsed` the sdlc:agent_turn event reports. Nothing here is recomputed and
// nothing else is timed.
//
// FIVE SECONDS IS A BOUND NO WORKING TURN IS UNDER. A worker answering a
// round prompt reads the findings, opens the files they name and runs
// something; even the fastest honest no-change turn — one that reads the tree
// and declares `satisfied` — spends tens of seconds doing it, and a turn that
// declares a stop is classified on its declaration before this bound is
// consulted at all. What it catches was measured at 451ms on the burn-in
// fleet (go-quicbaas#62, 2026-09-22): a persistent CLI session holding a
// queued background-task notification answered THAT when the review round
// arrived — one text block, no tool use, nothing changed — and the round, the
// job and one of the task's attempts were spent on it.
const NonAnswerTurnBound = 5 * time.Second

// MaxNonAnswerTurns bounds how many times one attempt re-sends a prompt the
// worker's reply did not address.
//
// ONE, for the same reason MaxSilentTurns is one: a session that answered a
// queued notification has flushed it by the next prompt, and a session that
// answers past the prompt twice running is not reading prompts at all. The
// second consecutive non-answer therefore takes the ordinary no-change path
// — a reply claiming work on an untouched tree — and is charged.
const MaxNonAnswerTurns = 1

// TurnReplyLimit caps how much of a reply is recorded on a job event, in
// bytes.
//
// THE REPLY IS TRUNCATED, NEVER DROPPED. A worker's closing summary is
// ordinarily a paragraph; a worker that pastes a build log is the case this
// bounds, and the head of that log is what says why it stopped. The cap is
// larger than the forge comment's (publicOutputLimit) because a job event is
// read by an operator with the whole record in front of them, not by somebody
// scrolling an issue.
const TurnReplyLimit = 4096

// turnReplyEllipsis marks a reply the cap cut, so a reader can tell a short
// answer from a long one they are seeing the head of.
var turnReplyEllipsis = "\n… (truncated at " + strconv.Itoa(TurnReplyLimit) + " bytes)"

// AgentTurn is the record of ONE prompt handed to the worker and the answer
// it gave. It is written to the job's audit trail on every turn, and to the
// Delivery when the turn changed nothing, so the next attempt is told what
// the last one said instead of repeating its prompt verbatim.
type AgentTurn struct {
	// Attempt is the task attempt the turn belongs to, Round the review
	// round within it.
	Attempt int `json:"attempt"`
	Round   int `json:"round"`

	// Elapsed is how long the turn took.
	Elapsed time.Duration `json:"elapsed"`

	// Reply is the worker's final assistant text, WHOLE.
	//
	// IT IS CAPPED WHERE IT IS WRITTEN, NEVER BEFORE — Describe caps what
	// the event carries, Capped caps what is stored, and publicOutput caps
	// and scrubs what reaches the forge. Capping it here instead would cut
	// the stop declaration off the end of any reply longer than the cap,
	// and the classification reads that declaration: a worker that explains
	// itself at length and then correctly ends with `SDLC-STOP: question`
	// would be read as claiming work it did not do, charged an attempt, and
	// re-prompted — the exact loop this file exists to close, sprung on
	// precisely the workers with most to say.
	Reply string `json:"reply,omitempty"`

	// ReplyBytes is how long the reply is. On a record that was capped for
	// storage it is the length BEFORE the cap, so a reader can tell how
	// much was cut.
	ReplyBytes int `json:"reply_bytes"`

	// Blocks counts the content blocks the reply carried, by type. Tool use
	// appears here as the backend reported it; a backend that publishes only
	// assistant text reports only text, and an absent measurement is
	// recorded as absent rather than inferred from the prose.
	Blocks map[string]int `json:"blocks,omitempty"`

	// StopReason is the backend's own terminal reason for the turn.
	StopReason string `json:"stop_reason,omitempty"`

	// Usage is the token accounting the backend published, nil when it
	// published none.
	Usage *orchestration.TokenUsage `json:"usage,omitempty"`

	// StopCode is the code the worker declared, read from the WHOLE reply
	// at the moment it was assembled, and StopDeclared says whether it
	// declared one at all.
	//
	// They are fields rather than a re-parse of Reply because Reply is
	// capped on every copy that is written down, and a declaration past the
	// cap must still classify the turn it came from.
	StopCode     string `json:"stop_code,omitempty"`
	StopDeclared bool   `json:"stop_declared,omitempty"`

	// Outcome is the classification, set only for a turn that changed no
	// file. Empty on a turn whose work reached the checkout.
	Outcome TurnOutcome `json:"outcome,omitempty"`

	// PromptDigest identifies the prompt this turn answered, so a later
	// attempt can refuse to send the identical prompt again.
	PromptDigest string `json:"prompt_digest,omitempty"`
}

// ToolUses is how many non-text content blocks the reply carried.
func (t AgentTurn) ToolUses() int {
	uses := 0
	for kind, count := range t.Blocks {
		if kind != contentBlockText {
			uses += count
		}
	}
	return uses
}

// NonAnswer reports whether this turn answered something other than the
// prompt it was given.
//
// STRUCTURE, NEVER PROSE, like every other judgement in this file. The reply
// carries text (an empty one is TurnOutcomeSilent and has its own path); it
// declares no stop (a declaration IS an answer, however fast it was written);
// the backend reported the turn ENDED rather than being cut off by a limit or
// a tool budget; it used no tool; and it came back inside NonAnswerTurnBound.
// The checkout is not consulted here because this question is only asked
// about a turn that already changed nothing.
//
// AN UNMEASURED TURN IS NOT A NON-ANSWER. A record whose Elapsed is zero was
// assembled rather than timed — a stored turn read back, a turn built field
// by field — and the conservative direction for an unmeasured turn is the
// path it has today. A prompt is re-sent on evidence or not at all.
func (t AgentTurn) NonAnswer() bool {
	return t.ReplyBytes > 0 &&
		!t.StopDeclared &&
		t.ToolUses() == 0 &&
		t.StopReason == orchestration.StopReasonEndTurn &&
		t.Elapsed > 0 && t.Elapsed < NonAnswerTurnBound
}

// Truncated reports whether this record's reply has already been cut — true
// for a stored copy, false for the whole turn the run holds.
func (t AgentTurn) Truncated() bool { return t.ReplyBytes > len(t.Reply) }

// Capped answers the turn as it is written down: the same record with the
// reply bounded at TurnReplyLimit and the cut marked. ReplyBytes keeps the
// length before the cap, and the declaration keeps its own fields, so a
// stored turn still says what it was even when its text is a head.
func (t AgentTurn) Capped() AgentTurn {
	capped := t
	capped.Reply = capReply(t.Reply)
	return capped
}

// contentBlockText is the block type an assistant's own words arrive as.
const contentBlockText = "text"

// Describe renders the turn as a job event: a header of counts a reader
// scans, then the reply itself.
func (t AgentTurn) Describe() string {
	var b strings.Builder
	b.WriteString("attempt=" + strconv.Itoa(t.Attempt))
	b.WriteString(" round=" + strconv.Itoa(t.Round))
	b.WriteString(" elapsed=" + t.Elapsed.Round(time.Millisecond).String())
	b.WriteString(" reply_bytes=" + strconv.Itoa(t.ReplyBytes))
	b.WriteString(" tool_uses=" + strconv.Itoa(t.ToolUses()))
	if blocks := describeBlocks(t.Blocks); blocks != "" {
		b.WriteString(" blocks=" + blocks)
	}
	if t.StopReason != "" {
		b.WriteString(" stop_reason=" + t.StopReason)
	}
	if t.Usage != nil {
		b.WriteString(" tokens_in=" + strconv.FormatInt(t.Usage.InputTokens, 10))
		b.WriteString(" tokens_out=" + strconv.FormatInt(t.Usage.OutputTokens, 10))
	}
	if t.Outcome != "" {
		b.WriteString(" outcome=" + string(t.Outcome))
	}
	reply := capReply(t.Reply)
	if len(reply) != len(t.Reply) || t.Truncated() {
		b.WriteString(" truncated=true")
	}
	if reply == "" {
		return b.String() + "\n\n(the worker answered with no text)"
	}
	return b.String() + "\n\n" + reply
}

// describeBlocks renders the block census in a stable order, so two events
// about the same turn read identically.
func describeBlocks(blocks map[string]int) string {
	if len(blocks) == 0 {
		return ""
	}
	kinds := make([]string, 0, len(blocks))
	for kind := range blocks {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	parts := make([]string, 0, len(kinds))
	for _, kind := range kinds {
		parts = append(parts, kind+":"+strconv.Itoa(blocks[kind]))
	}
	return strings.Join(parts, ",")
}

// NewAgentTurn builds the record of a turn from the worker's whole reply.
//
// IT IS THE ONLY WAY A TURN'S REPLY IS SET, and that is the point. The
// classification reads StopCode/StopDeclared rather than the text, because
// every written-down copy of the text is capped; a turn assembled field by
// field could carry a reply whose declaration nothing had read, and would
// then classify as a worker claiming work it never did. One constructor,
// one place the declaration is parsed, no way to skip it.
func NewAgentTurn(reply string) AgentTurn {
	turn := AgentTurn{Reply: reply, ReplyBytes: len(reply), Blocks: map[string]int{}}
	turn.StopCode, turn.StopDeclared = ParseTurnStop(reply)
	return turn
}

// ReadAgentTurn takes the record of one turn out of the backend's answer.
//
// A NIL RESPONSE IS A SILENT TURN, not an error: the backend answered, and
// what it answered with is the evidence. Deciding what a silent turn means is
// ClassifyTurn's job, and it is the same decision whether the emptiness
// arrived as a nil response, an empty content list or blank text.
func ReadAgentTurn(resp *orchestration.PromptSessionResponse) AgentTurn {
	turn := AgentTurn{Blocks: map[string]int{}}
	if resp == nil {
		return turn
	}
	turn.StopReason = resp.StopReason
	turn.Usage = resp.Usage
	var text strings.Builder
	for _, block := range resp.Content {
		kind := strings.TrimSpace(block.Type)
		if kind == "" {
			kind = "unspecified"
		}
		turn.Blocks[kind]++
		if kind != contentBlockText {
			continue
		}
		if chunk := strings.TrimSpace(block.Text); chunk != "" {
			if text.Len() > 0 {
				text.WriteString("\n\n")
			}
			text.WriteString(chunk)
		}
	}
	// THE DECLARATION IS READ FROM THE WHOLE TEXT, once, before any copy of
	// it is bounded.
	assembled := NewAgentTurn(text.String())
	assembled.Blocks = turn.Blocks
	assembled.StopReason = turn.StopReason
	assembled.Usage = turn.Usage
	return assembled
}

// capReply bounds a reply at TurnReplyLimit, on a rune boundary, with the
// cut marked.
func capReply(reply string) string {
	if len(reply) <= TurnReplyLimit {
		return reply
	}
	cut := TurnReplyLimit
	for cut > 0 && !isUTF8Boundary(reply[cut]) {
		cut--
	}
	return reply[:cut] + turnReplyEllipsis
}

// isUTF8Boundary reports whether b starts a rune rather than continuing one.
func isUTF8Boundary(b byte) bool { return b&0xC0 != 0x80 }

// ParseTurnStop reads the worker's stop declaration out of a reply: the code
// it declared, and whether it declared one at all.
//
// THE LAST DECLARATION WINS, and it is found rather than assumed to be the
// final line. The charter asks for it on the closing line, and a model that
// adds a trailing blank line, a closing code fence or a signature after it
// has still stopped for the reason it stated. A reply that quotes the marker
// mid-text and then stops for real is read on its real declaration for the
// same reason.
func ParseTurnStop(reply string) (string, bool) {
	lines := strings.Split(reply, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		line = strings.TrimPrefix(line, "> ")
		line = strings.TrimSpace(strings.Trim(line, "*`"))
		if !strings.HasPrefix(line, TurnStopMarker) {
			continue
		}
		code := strings.TrimSpace(strings.TrimPrefix(line, TurnStopMarker))
		code = strings.Trim(code, "`'\"*. ")
		return strings.ToLower(code), true
	}
	return "", false
}

// ClassifyTurn decides what a turn that changed no file was, and what the
// runner does about it.
//
// ONE TABLE LOOKUP, TWICE: the declared code names the outcome, and the
// outcome names the verdict. There is no path that reads the prose.
func ClassifyTurn(turn AgentTurn) TurnVerdict {
	return turnVerdicts[classifyTurnOutcome(turn)]
}

// classifyTurnOutcome is the outcome half of ClassifyTurn.
//
// It reads the DECLARATION FIELDS, not the reply text, because the text on
// any written-down copy of a turn is capped and the declaration can sit past
// the cap. ReadAgentTurn fills them from the whole reply.
func classifyTurnOutcome(turn AgentTurn) TurnOutcome {
	if turn.ReplyBytes == 0 {
		return TurnOutcomeSilent
	}
	// THE NON-ANSWER IS READ BEFORE THE CLAIM. A reply with no declaration on
	// an untouched tree is ordinarily a worker claiming work it did not do;
	// one that also used no tool and came back inside NonAnswerTurnBound is
	// a worker that answered a different question altogether, and charging
	// the work for it spends an attempt nothing judged.
	if turn.NonAnswer() {
		return TurnOutcomeNonAnswer
	}
	if !turn.StopDeclared {
		return TurnOutcomeClaimedWork
	}
	if outcome, known := stopCodeOutcomes[turn.StopCode]; known {
		return outcome
	}
	return TurnOutcomeUnreadable
}

// TurnParkReason composes what a parked task's reason says: the typed
// unblock condition, the verdict's own sentence, then the worker's words.
//
// THE WORKER'S TEXT IS THE PAYLOAD. Everything above it is context a reader
// needs to know why they are being shown it; the redaction and the cap that
// make it publishable are applied where the comment is rendered, by
// publicOutput, so nothing here has to remember to.
//
// AND IT IS THE DETAIL, NEVER APPENDED AFTER THE MARKER. The marker is read
// with LastIndex, so text placed after it would let a worker's own reply
// declare the condition its task is parked under — the one thing a worker
// must not be able to say about itself.
func TurnParkReason(set prompts.Renderer, verdict TurnVerdict, turn AgentTurn) (string, error) {
	workerReply := ""
	if reply := strings.TrimSpace(turn.Capped().Reply); reply != "" {
		workerReply = "\n\n" + set.MustRender("blocked_comment_worker_said", prompts.Vars{
			"reply": defuseUnblockMarker(reply),
		})
	}
	return ComposeBlockedReason(UnblockCondition{Kind: verdict.Condition},
		set.MustRender("blocked_comment", prompts.Vars{
			"headline":     verdict.Headline,
			"worker_reply": workerReply,
		}))
}

// defuseUnblockMarker breaks an unblock marker a worker wrote in its own
// reply, so the text can be carried as the park's detail.
//
// A SPACE, VISIBLY, rather than a silent deletion. The reply is quoted to a
// person and the sequence is part of what the worker said; what must not
// survive is its function, and a worker discussing the encoding — this
// repository's own agents do — would otherwise have its park refused outright
// by the encoder.
func defuseUnblockMarker(reply string) string {
	return strings.ReplaceAll(reply, unblockMarkerOpen, "unblock [")
}
