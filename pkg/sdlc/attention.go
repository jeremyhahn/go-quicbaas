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
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// The attention inbox — every blocker the operator, not the factory, clears
// =============================================================================
//
// A task the runner parks is stopped for good reason (blocked.go,
// readmission.go), but a parked task is invisible to anyone who is not
// reading task lists: it sits at status blocked with a reason only readable
// by fetching the record and parsing it. The attention inbox is the ONE
// place the factory surfaces every reason it has stopped and cannot start
// itself again — a parked task's question, and a fault in the factory's own
// operation — so the person who can unblock it does not have to go looking.
//
// TWO KINDS OF ITEM, ONE LIST. A parked task is a fact about the WORK; an
// environmental alert (an agent gone missing, a pooled credential erroring,
// the barrier sealed) is a fact about the FACTORY. Both stop progress and
// both need a person, so both are AttentionItems, distinguished by Kind —
// exactly the reasoning FacetSpec's "a record can belong under several
// facets" already applies elsewhere in this codebase to one list carrying
// several shapes of row.

// AttentionKind classifies one attention item.
type AttentionKind string

const (
	// AttentionParkedTask is a task the SDLC runner parked — BlockKind and
	// Condition say why. The great majority of the inbox.
	AttentionParkedTask AttentionKind = "parked_task"

	// AttentionFleetNodeOffline is a fleet node the engine has not heard from:
	// FleetNodeStatusOffline or FleetNodeStatusError. Work routed to it will not run.
	AttentionFleetNodeOffline AttentionKind = "agent_offline"

	// AttentionAccountError is a pooled provider account whose last
	// credential refresh failed (AccountStatusError) — the "pool credential
	// stale" alert. A worker resolving this account cannot get a token.
	AttentionAccountError AttentionKind = "account_error"

	// AttentionBarrierSealed is the node's storage barrier sealed: no data
	// operation can run anywhere on this node until it is unsealed.
	AttentionBarrierSealed AttentionKind = "barrier_sealed"

	// AttentionProjectIdle is an autonomous, unparked project with nothing
	// pending or in progress while it holds parked work: the factory has run
	// out of things it may do there, and only a ruling on the parked items
	// gives it more. It names the throughput cost the parked rows alone do
	// not — a project quietly idle for hours reads, row by row, as a handful
	// of old questions.
	AttentionProjectIdle AttentionKind = "project_idle"
)

// attentionKinds is every kind, for validation and for the refusal message a
// bad kind filter carries back to the caller.
var attentionKinds = []AttentionKind{
	AttentionParkedTask,
	AttentionFleetNodeOffline,
	AttentionAccountError,
	AttentionBarrierSealed,
	AttentionProjectIdle,
}

// Valid reports whether k is a member of the closed set.
func (k AttentionKind) Valid() bool {
	for _, known := range attentionKinds {
		if k == known {
			return true
		}
	}
	return false
}

// AttentionAction is one thing an operator may do to resolve an item.
type AttentionAction string

const (
	// AttentionApproveRecommendation accepts the worker's own recommendation
	// verbatim: it is posted as the ruling and the task is readmitted.
	AttentionApproveRecommendation AttentionAction = "approve_recommendation"

	// AttentionAnswer posts the operator's free-text ruling and readmits the
	// task.
	AttentionAnswer AttentionAction = "answer"

	// AttentionAllowAnotherRound raises the task's review-round budget by
	// one (bounded, orchestration.MaxReviewRoundBonus) and readmits it — for
	// a task escalated because review did not converge in the rounds the
	// project allows.
	AttentionAllowAnotherRound AttentionAction = "allow_another_round"

	// AttentionCloseAsDone takes the park labels off the forge issue, closes
	// it and completes the task without readmitting it: the operator has
	// decided no further work is needed.
	AttentionCloseAsDone AttentionAction = "close_as_done"

	// AttentionReadmit returns the task to the queue with no ruling beyond
	// "try again" — for a park whose condition has already been satisfied by
	// something outside the forge (an operator repair, for instance) and
	// needs no comment to explain.
	AttentionReadmit AttentionAction = "readmit"

	// AttentionNote posts the operator's free text to the task's forge issue
	// and leaves the task parked: a ruling on part of the question, context
	// for whoever picks it up, a decision recorded on a parked project whose
	// work must not restart yet. It is the one action that moves nothing.
	AttentionNote AttentionAction = "note"
)

// attentionActions is every action, for validation.
var attentionActions = []AttentionAction{
	AttentionApproveRecommendation,
	AttentionAnswer,
	AttentionAllowAnotherRound,
	AttentionCloseAsDone,
	AttentionReadmit,
	AttentionNote,
}

// Valid reports whether a is a member of the closed set.
func (a AttentionAction) Valid() bool {
	for _, known := range attentionActions {
		if a == known {
			return true
		}
	}
	return false
}

// AttentionItem is one thing the operator, and only the operator, can clear.
type AttentionItem struct {
	// ID identifies the item. For a parked task it is the task ID; for an
	// environmental alert it is a synthetic id ("agent_offline:<agentID>",
	// "account_error:<accountID>", "barrier_sealed:system") so the inbox can
	// address it even though nothing readmits it.
	ID string `json:"id"`

	// Kind classifies the item; see AttentionKind.
	Kind AttentionKind `json:"kind"`

	// ProjectID and ProjectName locate a parked task within the factory.
	// Both empty for an environmental alert that names no project.
	ProjectID   string `json:"project_id,omitempty"`
	ProjectName string `json:"project_name,omitempty"`

	// Subject is the one-line summary: the task's subject, or the alert's
	// own headline.
	Subject string `json:"subject"`

	// IssueRef is "owner/repo#N" and IssueURL its browsable link, both empty
	// when the item names no forge issue (every environmental alert; a
	// parked task whose task predates forge-issue provenance).
	IssueRef string `json:"issue_ref,omitempty"`
	IssueURL string `json:"issue_url,omitempty"`

	// BlockKind and Condition are the task's own park classification —
	// orchestration.TaskBlockKindNeedsInfo / Blocked / Escalated, and the
	// typed UnblockKind its reason names. Empty for an environmental alert.
	BlockKind orchestration.TaskBlockKind `json:"block_kind,omitempty"`
	Condition UnblockKind                 `json:"condition,omitempty"`

	// Question and Recommendation are ExtractAsk's reading of the worker's
	// own words. Both empty for an environmental alert, or for a park whose
	// reason carried neither.
	Question       string `json:"question,omitempty"`
	Recommendation string `json:"recommendation,omitempty"`

	// Truncated is true when the underlying reply was capped before it was
	// stored, so Recommendation may end mid-sentence.
	Truncated bool `json:"truncated,omitempty"`

	// RulingPosted is true when the parked task's issue carries a comment by
	// an authorized ruler (see ruling.go) newer than the park: the owner has
	// already answered, and the task is readmitted on the next unblock sweep
	// if nothing readmits it sooner. Always false for an environmental alert.
	RulingPosted bool `json:"ruling_posted,omitempty"`

	// Message is an environmental alert's own explanation. Empty for a
	// parked task, whose Question/Recommendation carry the equivalent.
	Message string `json:"message,omitempty"`

	// Severity is "warning" or "critical", set only on environmental
	// alerts — a parked task's urgency is read from its age, not a label.
	Severity string `json:"severity,omitempty"`

	// CreatedAt and StatusChangedAt are Unix-nanosecond instants; the age a
	// listing renders is StatusChangedAt (when the item started waiting),
	// falling back to CreatedAt when the item carries no status change (an
	// environmental alert observed just now).
	CreatedAt       int64 `json:"created_at,omitempty"`
	StatusChangedAt int64 `json:"status_changed_at,omitempty"`

	// Attempts is the task's charged attempt count; zero for an
	// environmental alert.
	Attempts int `json:"attempts,omitempty"`

	// Actions are the resolutions available for this item. Always empty for
	// an environmental alert: alerts are observed, not resolved through the
	// task lifecycle — the operator's action is outside the factory (repair
	// the agent, rotate the credential, unseal the barrier).
	Actions []AttentionAction `json:"actions,omitempty"`
}

// AttentionFilter narrows the attention listing.
type AttentionFilter struct {
	// ProjectID restricts the listing to one project. Ignored for
	// environmental alerts, which name no project.
	ProjectID string `json:"project_id,omitempty"`

	// Kind restricts the listing to one AttentionKind.
	Kind AttentionKind `json:"kind,omitempty"`
}

// attentionPageSpec orders and searches the attention listing, oldest first:
// the item that has waited longest is the one an operator most wants to see
// first, which is the opposite default of every other timestamped listing in
// this codebase (those show the newest activity; this shows the oldest
// unresolved one).
var attentionPageSpec = orchestration.PageSpec[*AttentionItem]{
	Sorts: map[string]func(a, b *AttentionItem) int{
		"id":                func(a, b *AttentionItem) int { return compareAttentionStrings(a.ID, b.ID) },
		"kind":              func(a, b *AttentionItem) int { return compareAttentionStrings(string(a.Kind), string(b.Kind)) },
		"project_id":        func(a, b *AttentionItem) int { return compareAttentionStrings(a.ProjectID, b.ProjectID) },
		"subject":           func(a, b *AttentionItem) int { return compareAttentionStrings(a.Subject, b.Subject) },
		"status_changed_at": func(a, b *AttentionItem) int { return compareAttentionInt64(attentionAge(a), attentionAge(b)) },
	},
	DefaultSort: "status_changed_at",
	Text: func(item *AttentionItem) []string {
		return []string{item.ID, item.Subject, item.ProjectName, item.IssueRef, item.Question, item.Recommendation, item.Message}
	},
}

// attentionFacetSpec counts the attention listing by project and kind, the
// two narrowings the inbox's filter bar offers.
var attentionFacetSpec = orchestration.FacetSpec[*AttentionItem]{
	Values: map[string]func(*AttentionItem) []string{
		"project_id": func(item *AttentionItem) []string {
			if item.ProjectID == "" {
				return nil
			}
			return []string{item.ProjectID}
		},
		"kind": func(item *AttentionItem) []string { return []string{string(item.Kind)} },
	},
}

// attentionAge is the instant a listing sorts and ages by: StatusChangedAt
// when the item has one, otherwise CreatedAt.
func attentionAge(item *AttentionItem) int64 {
	if item.StatusChangedAt > 0 {
		return item.StatusChangedAt
	}
	return item.CreatedAt
}

func compareAttentionStrings(a, b string) int {
	return strings.Compare(strings.ToLower(a), strings.ToLower(b))
}

func compareAttentionInt64(a, b int64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}
