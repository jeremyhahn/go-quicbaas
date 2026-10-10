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
	"regexp"
	"strings"
)

// =============================================================================
// Ask extraction — the plain-language question inside a park reason
// =============================================================================
//
// A blocked task's BlockedReason is a sentence this module composed
// (TurnParkReason / repeatedPromptReason / nextRound's escalation) followed
// by the worker's own words, quoted verbatim under "The worker said:" — see
// blocked.go and turn.go. A person reading the attention inbox wants two
// things out of that quote: what it is asking, and what it recommends. Most
// workers say both, but they say them in prose, at whatever length and
// structure the model chose, not in a form a machine already understands.
//
// ExtractAsk reads the quoted reply and recovers both, preferring the
// sections a worker names explicitly ("## What I need from you",
// "Recommendation", "Pick one") and falling back to the reply's own last
// paragraph — empirically the sentence a stopped worker ends on — when it
// named none. It is deliberately forgiving about heading punctuation and
// case: the heading was written by a model, not a machine, on THIS run's
// only chance to be read by the person it is asking.

// Ask is what ExtractAsk recovers from a worker's reply.
type Ask struct {
	// Question is the plain-language question the worker is asking, when it
	// named one under an explicit heading. Empty when none was found.
	Question string

	// Recommendation is the worker's own suggested resolution, when it named
	// one under an explicit heading, or — failing that — the reply's last
	// paragraph, which is where a stopped worker's concrete ask usually
	// lands even when it drew no heading around it.
	Recommendation string

	// Truncated is true when the reply was capped before it was stored (see
	// AgentTurn.Capped in turn.go), so Recommendation may end mid-sentence.
	Truncated bool
}

// questionHeadingPattern matches a markdown or bold heading line naming the
// question section. Anchored to the WHOLE line: "## The ask is real and I am
// not disputing it" must not match "the ask" as a bare heading.
var questionHeadingPattern = regexp.MustCompile(`(?im)^(?:#{1,6}[ \t]*|\*\*)[ \t]*(?:what i need from you|what i need|question for (?:the )?owner)[ \t]*:?\**[ \t]*$`)

// recommendationHeadingPattern matches a markdown or bold heading line
// naming the recommendation section.
var recommendationHeadingPattern = regexp.MustCompile(`(?im)^(?:#{1,6}[ \t]*|\*\*)[ \t]*(?:my recommendation|recommendation|recommended (?:path|option)|pick one)[ \t]*:?\**[ \t]*$`)

// anyMarkdownHeadingPattern bounds a section: whatever follows a matched
// heading runs until the next markdown heading of any kind, or to the end of
// the text.
var anyMarkdownHeadingPattern = regexp.MustCompile(`(?m)^#{1,6}[ \t]+\S`)

// truncationNotePattern matches either note this codebase appends when a
// reply was cut short, each on its own line: "… (truncated at 4096 bytes)"
// from AgentTurn.Capped() (turn.go), and the publicOutput note that recaps an
// already-capped reply again when it is rendered for publication. The pattern
// still recognizes the old daemon-log wording so existing records parse.
var truncationNotePattern = regexp.MustCompile(`(?m)^…?[ \t]*\((?:truncated at \d+ bytes|\d+ more bytes withheld; (?:see the daemon log|inspect the worker event record|diagnostic truncated for publication))\)[ \t]*$`)

// ExtractAsk parses a worker's quoted reply into the question it is asking
// and the resolution it recommends. reply is the "worker said" text alone —
// callers that hold a full BlockedReason should split the condition marker
// and the module's own preamble off first (see ParseBlockedReason and
// blockedCondition).
func ExtractAsk(reply string) Ask {
	text := strings.TrimSpace(reply)

	ask := Ask{Truncated: truncationNotePattern.MatchString(text)}
	text = strings.TrimSpace(truncationNotePattern.ReplaceAllString(text, ""))

	if section, ok := extractHeadedSection(text, questionHeadingPattern); ok {
		ask.Question = section
	}
	if section, ok := extractHeadedSection(text, recommendationHeadingPattern); ok {
		ask.Recommendation = section
	}

	// NEITHER SECTION WAS NAMED: the whole reply is unstructured prose, and
	// the last paragraph is where a stopped worker's concrete ask lands even
	// without a heading around it — every real park reason on the burn-in
	// node at the time this was written ended that way (go-quicbaas
	// attention-inbox fixtures). The Question stays empty rather than
	// duplicating the same text under both labels: a UI rendering both
	// fields would otherwise show the same sentence twice.
	if ask.Question == "" && ask.Recommendation == "" {
		ask.Recommendation = lastParagraph(text)
	}

	return ask
}

// extractHeadedSection returns the text between a heading matching pattern
// and the next markdown heading (or the end of text), or ("", false) when
// pattern matches nothing.
func extractHeadedSection(text string, pattern *regexp.Regexp) (string, bool) {
	loc := pattern.FindStringIndex(text)
	if loc == nil {
		return "", false
	}
	rest := strings.TrimLeft(text[loc[1]:], "\n")
	if next := anyMarkdownHeadingPattern.FindStringIndex(rest); next != nil {
		rest = rest[:next[0]]
	}
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return "", false
	}
	return rest, true
}

// lastParagraph returns the last blank-line-delimited block of text, trimmed.
// "" in, "" out.
func lastParagraph(text string) string {
	blocks := regexp.MustCompile(`\n[ \t]*\n`).Split(strings.TrimSpace(text), -1)
	for i := len(blocks) - 1; i >= 0; i-- {
		if trimmed := strings.TrimSpace(blocks[i]); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
