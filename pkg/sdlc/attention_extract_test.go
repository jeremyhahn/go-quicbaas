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
	"testing"
)

// =============================================================================
// Fixtures — four real parked tasks, captured verbatim from the burn-in
// tenant (automatethethings) on 2026-09-22 via
// GET /api/v1/orchestrator/tasks?tenant_id=automatethethings&status=blocked.
// Each is Task.BlockedReason exactly as the store carries it: the module's
// own sentence, the marker, and — where the park quoted a worker — the
// worker's own words, already capped by AgentTurn.Capped() at write time.
// =============================================================================

// fixtureOwnerRulingIssueBody is a prose park with no worker quote at all —
// the operator ruled directly in the issue body, no turn ever ran.
const fixtureOwnerRulingIssueBody = `owner ruling requested in the issue body (remote fleet node workspaces, go-quicbaas#981); not work until the owner decides`

// fixtureReviewRoundsExhausted is nextRound's own escalation sentence: no
// worker quote, because nothing asked the worker anything — the rounds ran
// out.
const fixtureReviewRoundsExhausted = `the owner must rule on the question recorded with this task: verification failed after 3 review round(s); this change needs a person unblock[kind=owner_ruling]`

// fixtureForgeIssueState is a blocked (not needs_info) park whose condition
// is machine-checkable: it clears itself once go-quicbaas#967 closes.
const fixtureForgeIssueState = `blocked on the corrected release-alpha template: go-quicbaas#967 tracks the upstream fix (push.followTags publishes reachable annotated tags; a fenced changelog heading passes the guard). Vendoring v2.3.5 verbatim cannot pass review and diverging from it defeats the issue; the next attempt vendors the corrected revision unblock[kind=forge_issue_state;subject=jeremyhahn/go-quicbaas#967;target=closed]`

// fixtureP256ProtoOwnerRuling is a long, structured worker reply (headed
// sections, a numbered list) capped mid-sentence at the turn's byte limit.
const fixtureP256ProtoOwnerRuling = `the owner must rule on the question recorded with this task: The worker stopped on a decision the SDLC convention reserves for the owner. It made no change to the checkout, so the verification commands were not run and there is nothing to review.

The worker said:

I read the issue, ` + "`pkg/p256proto`" + ` (` + "`keys.go`, `persist.go`, `doc.go`" + `), the sibling ` + "`pkg/noiseproto`" + `, ` + "`docs/approved-handshake.md`" + `, the CHANGELOG entry that shipped this API, and the handbook. I have **not** changed any file, because completing this task requires an architecture ruling I do not hold. Details and a recommendation below so the ruling is cheap to give.

## The ask is real and I am not disputing it

Confirmed against the tree: the only three exported entry points for a P-256 static scalar are affected.

## Why I stopped

**1. This reverses a documented, owner-ruled design property.** NewStaticKey taking a LockedBuffer is not an oversight to be tidied.

**2. Option 3 is forbidden outright — do not choose it.**

**3. The mandated target type is neither of the remaining two options.**

**4. One factual gap I cannot close from here.** The issue states go-qrdb's secure.Buffer "exposes no accessor" to its inner buffer, and then proposes option 2 as satisfied by anything with Bytes() []byte. Those cannot both be true, and option 1 depends on the same unknown.

Only ` + "`/workspace/go-truststrap`" + ` and ` + "`/wor" + `
… (truncated at 4096 bytes) unblock[kind=owner_ruling]`

// blockedReasonDetail runs the real pipeline a caller uses before ExtractAsk:
// ParseBlockedReason plus the same publishable-text bounding the forge
// comment and the attention item both go through (blockedCondition).
// blockedCondition always returns a usable detail — ok is false only when
// the reason carries no typed marker at all (a prose park), in which case
// the zero UnblockCondition is the honest answer and detail is still the
// reason's own words.
func blockedReasonDetail(t *testing.T, reason string) (UnblockCondition, string) {
	t.Helper()
	condition, detail, _ := blockedCondition(reason)
	return condition, detail
}

func TestExtractAsk_FallsBackToLastParagraphOnUnstructuredParks(t *testing.T) {
	cases := []struct {
		name   string
		reason string
	}{
		{"owner-ruling-issue-body", fixtureOwnerRulingIssueBody},
		{"review-rounds-exhausted", fixtureReviewRoundsExhausted},
		{"forge-issue-state", fixtureForgeIssueState},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, detail := blockedReasonDetail(t, c.reason)
			ask := ExtractAsk(detail)
			if ask.Question != "" {
				t.Errorf("Question = %q, want empty: none of these parks name an explicit question section", ask.Question)
			}
			if strings.TrimSpace(ask.Recommendation) == "" {
				t.Fatalf("Recommendation is empty for an unstructured park; fallback should have used the last paragraph")
			}
			if ask.Truncated {
				t.Errorf("Truncated = true, want false: this fixture was never capped")
			}
		})
	}
}

func TestExtractAsk_StructuredReplyMarksTruncation(t *testing.T) {
	condition, detail := blockedReasonDetail(t, fixtureP256ProtoOwnerRuling)
	if condition.Kind != UnblockOwnerRuling {
		t.Fatalf("condition kind = %q, want %q", condition.Kind, UnblockOwnerRuling)
	}

	ask := ExtractAsk(detail)

	// No explicit "## What I need from you" / "Recommendation" heading in
	// this reply, so it falls back to the last paragraph, same as the
	// unstructured fixtures — the worker's numbered "Why I stopped" list
	// carries no such heading either.
	if strings.TrimSpace(ask.Recommendation) == "" {
		t.Fatalf("Recommendation is empty; fallback should have used the last paragraph")
	}
	if ask.Question != "" {
		t.Errorf("Question = %q, want empty", ask.Question)
	}
	// The stored reason was capped mid-word ("/wor" then the truncation
	// note); the public-output bound may cap it again, but the fixture's
	// own cap survives either way.
	if !ask.Truncated {
		t.Errorf("Truncated = false, want true: the fixture carries a turn.Capped() truncation note; detail=%q", detail)
	}
}

// TestExtractAsk_PrefersExplicitSections is a SYNTHETIC fixture — none of
// the four real burn-in parks captured for this change carried an explicit
// "## What I need from you" / "Recommendation" heading, but the model that
// writes these replies does use them, and the inbox has to read them when it
// does.
func TestExtractAsk_PrefersExplicitSections(t *testing.T) {
	reply := `I looked at the two ways to satisfy this and both are defensible.

## What I need from you

Should the retry budget be per-task or per-project? Both are used elsewhere in this codebase for similar knobs.

## Recommendation

Per-task: it composes with the existing attempt budget without touching the project binding, and it is the smaller, more reversible change.

Either way I can implement it once you say which.`

	ask := ExtractAsk(reply)
	if !strings.Contains(ask.Question, "per-task or per-project") {
		t.Errorf("Question = %q, want it to contain the actual question", ask.Question)
	}
	if !strings.Contains(ask.Recommendation, "Per-task") {
		t.Errorf("Recommendation = %q, want it to contain the worker's own recommendation", ask.Recommendation)
	}
	// The explicit heading was found, so this is the SECTION, not the
	// fallback: it runs to the end of the reply since no further heading
	// bounds it, which is why it also carries the closing sentence.
	if !strings.Contains(ask.Recommendation, "Either way") {
		t.Errorf("Recommendation = %q, want it to run to the end of the reply (no further heading bounds it)", ask.Recommendation)
	}
}

func TestExtractAsk_EmptyReplyYieldsEmptyAsk(t *testing.T) {
	ask := ExtractAsk("   \n\n  ")
	if ask.Question != "" || ask.Recommendation != "" || ask.Truncated {
		t.Errorf("ExtractAsk(whitespace) = %+v, want the zero Ask", ask)
	}
}

func TestExtractAsk_HeadingLikeTextInsideAParagraphIsNotMistakenForASection(t *testing.T) {
	// "the ask is real" is NOT "the ask" alone on its own line — the
	// question-heading pattern must not match arbitrary prose that merely
	// mentions the word.
	ask := ExtractAsk("The ask is real and I am not disputing it, but I need a ruling before I can act on it.")
	if ask.Question != "" {
		t.Errorf("Question = %q, want empty: this is prose, not a heading", ask.Question)
	}
	if ask.Recommendation == "" {
		t.Errorf("Recommendation is empty, want the fallback paragraph")
	}
}
