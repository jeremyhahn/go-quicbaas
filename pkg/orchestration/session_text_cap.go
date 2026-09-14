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

package orchestration

import (
	"strconv"
	"unicode/utf8"
)

// MaxSessionEventTextBytes is the documented per-field cap on text persisted
// to the session transcript.
//
// A turn's text is operator-facing narrative, not a data channel: 64 KiB is
// several times the longest assistant answer observed in practice and still
// small enough that a thousand-event session stays inside a single-digit-
// megabyte keyspace. A field longer than this is clipped and MARKED — the
// payload carries the number of bytes removed and the stored text ends in a
// human-visible marker — because a transcript that silently shortens what an
// agent said is worse than one that admits it did.
const MaxSessionEventTextBytes = 64 * 1024

// MaxSessionEventArgsBytes is the cap on a tool invocation's JSON arguments.
//
// Arguments are structured, so they cannot be clipped mid-value and stay
// parseable; an oversized payload is REPLACED with a marker object rather than
// truncated. The cap is smaller than the text cap because arguments are an
// index into the work (a path, a pattern, a command), not the work itself.
const MaxSessionEventArgsBytes = 16 * 1024

// truncationMarkerPrefix opens the suffix appended to clipped text. Kept as a
// constant so a renderer can detect the marker rather than pattern-match prose.
const truncationMarkerPrefix = "\n\n[truncated "

// truncationMarkerSuffix closes the marker appended to clipped text.
const truncationMarkerSuffix = " bytes]"

// truncateSessionText clips s to at most MaxSessionEventTextBytes bytes and
// appends a visible marker naming how much was removed. It returns the stored
// text and the number of bytes dropped; a value already within the cap is
// returned unchanged with a zero count.
//
// The clip lands on a rune boundary. Cutting a multi-byte rune in half would
// store bytes that are not valid UTF-8, which json.Marshal silently rewrites to
// U+FFFD — so the transcript would show a corrupted final character with no
// indication of why.
func truncateSessionText(s string) (string, int64) {
	if len(s) <= MaxSessionEventTextBytes {
		return s, 0
	}
	cut := MaxSessionEventTextBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	dropped := int64(len(s) - cut)
	return s[:cut] + truncationMarkerPrefix + strconv.FormatInt(dropped, 10) + truncationMarkerSuffix, dropped
}

// capSessionEventText clips every operator-facing text field on an event to
// MaxSessionEventTextBytes, recording the dropped byte count on the payload
// that carried it.
//
// It runs on AppendNextSessionEvent — the single write path — so the bound is a
// property of the log rather than a rule each producer has to remember. A
// producer that already clipped its own field (the ACP backend does this for
// tool output it reads from a pipe) reports its own count; this adds to it
// rather than overwriting, so the total says how much of the original is gone.
func capSessionEventText(event *SessionEvent) {
	if event == nil {
		return
	}
	if p := event.Prompt; p != nil {
		var dropped, sysDropped int64
		p.Prompt, dropped = truncateSessionText(p.Prompt)
		p.SystemPrompt, sysDropped = truncateSessionText(p.SystemPrompt)
		p.TruncatedBytes += dropped + sysDropped
	}
	if c := event.AssistantChunk; c != nil {
		var dropped int64
		c.Text, dropped = truncateSessionText(c.Text)
		c.TruncatedBytes += dropped
	}
	if th := event.Thought; th != nil {
		var dropped int64
		th.Text, dropped = truncateSessionText(th.Text)
		th.TruncatedBytes += dropped
	}
	if inv := event.ToolInvocation; inv != nil && len(inv.Args) > MaxSessionEventArgsBytes {
		dropped := int64(len(inv.Args))
		inv.Args = []byte(`{"truncated_bytes":` + strconv.FormatInt(dropped, 10) + `}`)
		inv.TruncatedBytes += dropped
	}
	if res := event.ToolResult; res != nil {
		var dropped int64
		res.Content, dropped = truncateSessionText(res.Content)
		res.TruncatedBytes += dropped
	}
	if sys := event.System; sys != nil {
		sys.Message, _ = truncateSessionText(sys.Message)
	}
	if tf := event.TurnFailed; tf != nil {
		tf.Error, _ = truncateSessionText(tf.Error)
	}
}
