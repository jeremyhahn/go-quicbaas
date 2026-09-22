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
	"embed"
	"sort"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/prompts"
)

// =============================================================================
// The sdlc prompt set
// =============================================================================
//
// EVERYTHING THIS MODULE SAYS IS A FILE UNDER promptdefaults/sdlc: the
// charter a worker runs under, the contract it is handed, the findings that
// come back from a round, and every comment a person reads on the issue. The
// markdown is the system tenant's content — embedded in the binary, seeded
// into each tenant, inherited until a tenant overrides it.
//
// THE CHARTER'S OLD COMMENT SAID CONSTANTS WERE THE POINT: instructions
// loaded from a path are instructions an operator can change without a
// review. That control is not abandoned here, it is moved into the product.
// A prompt is refused unless it still carries every literal the engine
// parses, and those literals are declared from turn.go's own constants
// rather than written out a second time — so a charter that stopped naming a
// stop code the runner accepts cannot be saved, and cannot even be built.

// SetSDLC is the delivery module's own set.
const SetSDLC prompts.SetKind = "sdlc"

//go:embed promptdefaults/sdlc
var sdlcDefaultsFS embed.FS

// sdlcDefaults is the embedded markdown, keyed by prompt name.
var sdlcDefaults = prompts.MustDefaults(sdlcDefaultsFS, "promptdefaults/sdlc")

// sdlcPrompts is the registered set.
var sdlcPrompts = prompts.MustRegisterSet(sdlcSetSpec())

// SDLCPrompts returns the module's registered prompt set.
func SDLCPrompts() *prompts.Set { return sdlcPrompts }

// workerCharterMarkers is what the charter may not stop saying: the stop
// marker ParseTurnStop looks for, and EVERY code ClassifyTurn accepts.
//
// IT IS BUILT FROM THE PARSER'S OWN TABLE, never from a second copy of the
// vocabulary. A code added to stopCodeOutcomes is a code the charter must
// teach, and a charter that does not name it fails registration — the same
// coupling the old constant achieved by concatenation, now checked rather
// than assumed. The order is sorted so a refusal names the same marker every
// time.
func workerCharterMarkers() []prompts.Marker {
	codes := make([]string, 0, len(stopCodeOutcomes))
	for code := range stopCodeOutcomes {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	markers := make([]prompts.Marker, 0, len(codes)+1)
	markers = append(markers, prompts.Marker{
		Literal: TurnStopMarker,
		Reason:  "ParseTurnStop reads a worker's stop declaration by this marker and nothing else",
	})
	for _, code := range codes {
		markers = append(markers, prompts.Marker{
			Literal: code,
			Reason:  "ClassifyTurn acts on this stop code; a charter that never names it is a stop nobody declares",
		})
	}
	return markers
}

// reviewerCharterMarkers is what the reviewer charter may not stop saying: the
// declaration marker ParseReviewVerdict reads, EVERY code it accepts, and the
// two sentences that keep a reviewer from re-running a pipeline that has
// already passed.
//
// THE VOCABULARY IS BUILT FROM THE PARSER'S OWN TABLE, never from a second
// copy. reviewCodeVerdicts is the closed set a verdict may be declared from, so
// a code added there is a code the charter must teach, and a charter that does
// not name it fails registration rather than silently becoming unreachable.
//
// THE TWO CI SENTENCES ARE MARKERS FOR A DIFFERENT REASON. Nothing parses
// them: they are what makes the review cheap. A reviewer that is not told the
// pipeline is green re-runs it — measured at 13m46s of wall clock and a whole
// attempt in go-quicbaas#927 — so the instruction is part of the prompt's
// contract exactly as the declaration marker is, and an edit that drops it is
// refused with the reason.
func reviewerCharterMarkers() []prompts.Marker {
	codes := make([]string, 0, len(reviewCodeVerdicts))
	for code := range reviewCodeVerdicts {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	markers := make([]prompts.Marker, 0, len(codes)+3)
	markers = append(markers, prompts.Marker{
		Literal: ReviewVerdictMarker,
		Reason:  "ParseReviewVerdict reads a reviewer's verdict by this marker and nothing else",
	})
	for _, code := range codes {
		markers = append(markers, prompts.Marker{
			Literal: code,
			Reason:  "ParseReviewVerdict accepts this verdict code; a charter that never names it is a verdict nobody declares",
		})
	}
	return append(markers,
		prompts.Marker{
			Literal: ReviewerCIGreenMarker,
			Reason:  "the dispatcher starts no review until the head is green; a reviewer not told so re-runs the pipeline",
		},
		prompts.Marker{
			Literal: ReviewerNoRerunMarker,
			Reason:  "stating that CI passed is not the same as forbidding a re-run, and only the second stops the spend",
		},
	)
}

// sdlcSetSpec declares the set.
func sdlcSetSpec() prompts.SetSpec {
	return prompts.SetSpec{
		Kind:        SetSDLC,
		Description: "driving one forge issue to a merge: the charter, the contract, the findings and the endings",
		Prompts: []prompts.PromptSpec{
			{
				Name:        "worker_charter",
				Description: "the standing rules every worker attempt runs under",
				Default:     sdlcDefaults["worker_charter"],
				Markers:     workerCharterMarkers(),
			},
			{
				Name:        "reviewer_charter",
				Description: "the standing rules every review runs under, including that CI has already passed",
				Default:     sdlcDefaults["reviewer_charter"],
				Markers:     reviewerCharterMarkers(),
			},
			{
				Name:        "issue_contract",
				Description: "the task description a worker is handed: the issue, verbatim, and its discussion",
				Default:     sdlcDefaults["issue_contract"],
				Variables: []prompts.Variable{
					{Name: "repo", Description: "the repository, as owner/repo", Required: true},
					{Name: "issue_number", Description: "the issue's number", Required: true},
					{Name: "title", Description: "the issue's title", Required: true},
					{Name: "body", Description: "the issue's body, empty when it has none"},
					{Name: "discussion", Description: "the issue_contract_comment prompts, in order", Required: true},
				},
			},
			{
				Name:        "issue_contract_comment",
				Description: "one comment in the issue's discussion",
				Default:     sdlcDefaults["issue_contract_comment"],
				Variables: []prompts.Variable{
					{Name: "index", Description: "the comment's position, counting only the ones with text", Required: true},
					{Name: "attribution", Description: "who wrote it and when, empty when neither is known"},
					{Name: "body", Description: "what the comment says", Required: true},
				},
			},
			{
				Name:        "round_findings",
				Description: "what the last review round said, handed to the worker with the next prompt",
				Default:     sdlcDefaults["round_findings"],
				Variables: []prompts.Variable{
					{Name: "round", Description: "the review round", Required: true},
					{Name: "findings", Description: "the job's memory, or this run's own feedback", Required: true},
				},
			},
			{
				Name:        "verification_failed",
				Description: "how a failed verification command is reported back to the worker",
				Default:     sdlcDefaults["verification_failed"],
				Variables: []prompts.Variable{
					{Name: "failures", Description: "each failed criterion and what it said", Required: true},
				},
			},
			{
				Name:        "review_findings",
				Description: "how a refused review is reported back to the worker",
				Default:     sdlcDefaults["review_findings"],
				Variables: []prompts.Variable{
					{Name: "failures", Description: "each failed criterion and what it said", Required: true},
				},
			},
			{
				Name:        "changes_requested",
				Description: "one reviewer's refusal, for the worker's next round",
				Default:     sdlcDefaults["changes_requested"],
				Variables: []prompts.Variable{
					{Name: "reviewer", Description: "the reviewer's forge login", Required: true},
					{Name: "head_sha", Description: "the head the verdict binds to", Required: true},
					{Name: "findings", Description: "what the reviewer wrote", Required: true},
				},
			},
			{
				Name: "workspace_unfit",
				Description: "the pre-flight refused the checkout before anything was prompted, and what " +
					"an operator does about it",
				Default: sdlcDefaults["workspace_unfit"],
				Variables: []prompts.Variable{
					{Name: "fault", Description: "which repository was unfit and how", Required: true},
				},
			},
			{
				Name:        "changes_requested_no_findings",
				Description: "what stands in for a refusal that wrote nothing",
				Default:     sdlcDefaults["changes_requested_no_findings"],
			},
			{
				Name:        "ci_red",
				Description: "the pull request's checks went red and stayed red",
				Default:     sdlcDefaults["ci_red"],
				Variables: []prompts.Variable{
					{Name: "head_sha", Description: "the head the checks ran on", Required: true},
					{Name: "failed_count", Description: "how many required checks failed", Required: true},
					{Name: "check_noun", Description: "\"required check\" or \"required checks\", agreeing with the count", Required: true},
					{Name: "infrastructure", Description: "the ci_red_infrastructure prompt, empty when every red is the change's own"},
					{Name: "checks", Description: "the ci_red_check prompts, one per failed check that is the change's own", Required: true},
				},
			},
			{
				Name:        "ci_red_infrastructure",
				Description: "the failed checks that are NOT the change's, which the engine is handling without the worker",
				Default:     sdlcDefaults["ci_red_infrastructure"],
				Variables: []prompts.Variable{
					{Name: "checks", Description: "the infrastructure-fault checks, each as \"name (signature)\"", Required: true},
				},
			},
			{
				Name:        "ci_red_check",
				Description: "one failed check",
				Default:     sdlcDefaults["ci_red_check"],
				Variables: []prompts.Variable{
					{Name: "check_name", Description: "the check's name", Required: true},
					{Name: "finished", Description: "when it finished, empty when the forge did not say"},
				},
			},
			{
				Name:        "ci_red_check_log",
				Description: "the tail of a failed check's log, when the forge serves one",
				Default:     sdlcDefaults["ci_red_check_log"],
				Variables: []prompts.Variable{
					{Name: "excerpt", Description: "the last lines of the log, with secrets redacted", Required: true},
				},
			},
			{
				Name:        "prior_turn",
				Description: "what the last attempt at this task said, and that it changed nothing",
				Default:     sdlcDefaults["prior_turn"],
				Variables: []prompts.Variable{
					{Name: "attempt", Description: "which attempt it was", Required: true},
					{Name: "elapsed", Description: "how long the turn took", Required: true},
					{Name: "classification", Description: "the prior_turn_classification prompt, empty when the turn was not classified"},
					{Name: "account", Description: "the prior_turn_said or prior_turn_silent prompt", Required: true},
				},
			},
			{
				Name:        "prior_turn_classification",
				Description: "how a classified prior turn is named",
				Default:     sdlcDefaults["prior_turn_classification"],
				Variables: []prompts.Variable{
					{Name: "outcome", Description: "what the turn was classified as", Required: true},
				},
			},
			{
				Name:        "prior_turn_silent",
				Description: "what to make of a prior turn that answered with nothing",
				Default:     sdlcDefaults["prior_turn_silent"],
			},
			{
				Name:        "prior_turn_said",
				Description: "the prior turn's own words, and what this worker must do differently",
				Default:     sdlcDefaults["prior_turn_said"],
				Variables: []prompts.Variable{
					{Name: "reply", Description: "what the previous worker said", Required: true},
				},
			},
			{
				Name:        "silent_turn_note",
				Description: "what is added to the prompt when this run's own turn answered with nothing",
				Default:     sdlcDefaults["silent_turn_note"],
				Variables: []prompts.Variable{
					{Name: "elapsed", Description: "how long the silent turn took", Required: true},
				},
			},
			{
				Name:        "non_answer_turn_note",
				Description: "what is added to the prompt that is sent again because the last reply did not address it",
				Default:     sdlcDefaults["non_answer_turn_note"],
				Variables: []prompts.Variable{
					{Name: "elapsed", Description: "how fast the reply that missed the prompt came back", Required: true},
				},
			},
			{
				Name:        "session_rolled_note",
				Description: "what is added to the first prompt of a fresh session that replaced one whose context was full",
				Default:     sdlcDefaults["session_rolled_note"],
				Variables: []prompts.Variable{
					{Name: "elapsed", Description: "how long the turn that filled the old session's context took", Required: true},
					{Name: "round", Description: "the round the old session ran out of context in", Required: true},
				},
			},
			{
				Name:        "repeated_prompt",
				Description: "why a task is parked rather than handed a prompt it already answered",
				Default:     sdlcDefaults["repeated_prompt"],
			},
			{
				Name:        "blocked_comment",
				Description: "why a task parked on a turn that changed no file",
				Default:     sdlcDefaults["blocked_comment"],
				Variables: []prompts.Variable{
					{Name: "headline", Description: "the verdict's own sentence", Required: true},
					{Name: "worker_reply", Description: "the blocked_comment_worker_said prompt, empty when the worker said nothing"},
				},
			},
			{
				Name:        "blocked_comment_worker_said",
				Description: "how the worker's own words are introduced in a park reason",
				Default:     sdlcDefaults["blocked_comment_worker_said"],
				Variables: []prompts.Variable{
					{Name: "reply", Description: "what the worker said, bounded and with its unblock markers defused", Required: true},
				},
			},
			{
				Name:        "outcome_comment",
				Description: "the comment every ending posts to the issue",
				Default:     sdlcDefaults["outcome_comment"],
				Variables: []prompts.Variable{
					{Name: "headline", Description: "what the ending announces", Required: true},
					{Name: "kind", Description: "the outcome's name", Required: true},
					{Name: "meaning", Description: "what the outcome means for the reader", Required: true},
					{Name: "evidence", Description: "the quoted park reason, empty for an ending with nothing to quote"},
					{Name: "facts", Description: "the cause, condition, attempt, task, delivery and label lines"},
					{Name: "remedy", Description: "what the reader can do about it", Required: true},
					{Name: "marker", Description: "the HTML comment identifying this park", Required: true},
				},
			},
			{
				Name:        "outcome_no_reason_recorded",
				Description: "what a terminal outcome quotes when nothing recorded a reason",
				Default:     sdlcDefaults["outcome_no_reason_recorded"],
			},
			{
				Name:        "outcome_cleared_by_sweep",
				Description: "who clears a condition nobody in particular is waiting on",
				Default:     sdlcDefaults["outcome_cleared_by_sweep"],
			},
			{
				Name:        "outcome_readmit_action",
				Description: "how the reader is told to put a parked task back in the queue",
				Default:     sdlcDefaults["outcome_readmit_action"],
				Variables: []prompts.Variable{
					{Name: "task_argument", Description: "the task identifier the command takes, empty when there is none"},
				},
			},
			{
				Name:        "outcome_remedy_readmitted",
				Description: "the remedy for a readmission, which asks for nothing",
				Default:     sdlcDefaults["outcome_remedy_readmitted"],
			},
			{
				Name:        "outcome_remedy_failed",
				Description: "the remedy for a task the store will not take back",
				Default:     sdlcDefaults["outcome_remedy_failed"],
			},
			{
				Name:        "outcome_remedy_budget_spent",
				Description: "the remedy for a parked task whose attempt budget is spent",
				Default:     sdlcDefaults["outcome_remedy_budget_spent"],
				Variables: []prompts.Variable{
					{Name: "readmit", Description: "the outcome_readmit_action prompt", Required: true},
				},
			},
			{
				Name:        "outcome_remedy_self_clearing",
				Description: "the remedy for a condition the sweep checks by itself",
				Default:     sdlcDefaults["outcome_remedy_self_clearing"],
			},
			{
				Name:        "outcome_remedy_resume",
				Description: "the remedy for a park a person must answer",
				Default:     sdlcDefaults["outcome_remedy_resume"],
				Variables: []prompts.Variable{
					{Name: "readmit", Description: "the outcome_readmit_action prompt", Required: true},
				},
			},
		},
	}
}
