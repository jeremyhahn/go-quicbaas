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
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// =============================================================================
// Publishable text
// =============================================================================
//
// A REASON IS PUBLISHED, SO IT IS BUILT PUBLISHABLE.
//
// Since #198 a parked item's reason is posted as a comment on a forge issue.
// Two of the things that become a reason are not the worker's own words but the
// OUTPUT OF A SUBPROCESS — a failed verification command's stderr, and the
// local reviewer's findings — and a command that echoes a token before failing
// puts that token in the reason (hosted review 1350).
//
// THE SANITISING HAPPENS WHERE THE REASON IS BUILT, NOT WHERE IT IS RENDERED.
// Doing it in surface.go would protect the forge comment and leave every other
// consumer of the reason exposed, which is the per-call-site shape this
// codebase has already measured failing twice (#187, and again at review 1330).
// Building it here means every producer of a reason yields a publishable one by
// construction, and nothing downstream has to remember.
//
// AND IT HAPPENS BEFORE ANYTHING DURABLE. The bounded text is what becomes
// WorkItem.BlockedReason, so the unsanitised form is never written to the
// store, never reserved onto a Surfacing record, and never rendered.
//
// HONEST ABOUT ITS LIMITS. Pattern redaction cannot be complete — this
// codebase's own doctrine is that free text cannot be reliably sanitised, which
// is why the ERROR types withhold their payload outright rather than scrubbing
// it. The difference here is what the text is FOR: an error's captured output
// is a diagnostic an operator can get from the daemon, while a failed check's
// output is the payload a human needs in order to act on the park. Withholding
// it entirely would leave "verification failed: build" on the issue and send
// nobody anywhere. So this bounds the exposure rather than eliminating it:
// known credential shapes go, the window is capped, and what is left is a
// summary rather than a dump.

// publicOutputLimit caps how much subprocess output a reason may carry.
//
// A cap matters INDEPENDENTLY of secrets: an unbounded stderr dump in an issue
// comment is unreadable, and a human who cannot read the park cannot act on it
// — which is the failure #198 exists to remove, arriving as noise instead of
// silence. It also bounds how much of anything a miss in the patterns below can
// expose.
const publicOutputLimit = 1200

// secretShapes are the credential forms a build or review step is most likely
// to print. Best-effort by construction; see the file comment.
//
// EACH PATTERN CONSUMES THE WHOLE VALUE. A pattern that stops early leaves the
// tail visible and looks like it worked, which is worse than not matching at
// all — measured twice while writing this file: an `\S+` after
// "Authorization:" redacted only the word "Bearer", and an assignment pattern
// that took a single `[=:]` matched `clientSecret :` and left `= "value"`.
//
// OVER-MATCHING IS THE SAFE DIRECTION and is chosen deliberately. A diff line
// reading `token := scanner.Scan()` will be redacted even though it holds no
// secret. Losing a line of a stack trace costs a reader some context; leaking a
// credential onto a forge issue costs a rotation.
var secretShapes = []*regexp.Regexp{
	// PEM private key blocks first: they span lines and would otherwise be
	// chewed up piecemeal by the narrower patterns below.
	regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`),

	// Headers that carry credentials. To END OF LINE, never one token.
	// `[ \t]*` rather than `\s*` around the colon: `\s` includes newlines, so a
	// header with an empty value would swallow the NEXT diagnostic line.
	regexp.MustCompile(`(?i)\b(authorization|proxy-authorization|x-api-key|api-key|x-auth-token|cookie|set-cookie)[ \t]*:[ \t]*[^\r\n]*`),
	regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/=-]{8,}`),

	// Credentials embedded in a URL: scheme://user:secret@host.
	regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://)[^/\s:@]+:[^/\s@]+@`),

	// JWT-shaped triples.
	regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}`),

	// Vendor-prefixed tokens. Kept as one alternation so the list is auditable
	// against a provider's documented prefix.
	// DISTINCTIVE PREFIXES, DELIBERATELY UNANCHORED. A token glued to
	// surrounding word characters — "PREFIXghp_...", or one inside a longer
	// blob — has no word boundary before it, so an anchored pattern silently
	// misses it. These prefixes are long and unusual enough to stand alone.
	regexp.MustCompile(`(` + strings.Join([]string{
		`gh[pousr]_[A-Za-z0-9]{16,}`,              // GitHub classic PAT / OAuth / server / user / refresh
		`github_pat_[A-Za-z0-9_]{20,}`,            // GitHub fine-grained PAT
		`glpat-[A-Za-z0-9_-]{16,}`,                // GitLab PAT
		`sk-(proj|ant)-[A-Za-z0-9_-]{16,}`,        // OpenAI / Anthropic project keys
		`(sk|rk|pk)_(live|test)_[A-Za-z0-9]{12,}`, // Stripe
		`AIza[A-Za-z0-9_-]{20,}`,                  // Google API key
		`xox[baprs]-[A-Za-z0-9-]{10,}`,            // Slack
		`hvs\.[A-Za-z0-9_-]{16,}`,                 // HashiCorp Vault
		`(AKIA|ASIA)[0-9A-Z]{12,}`,                // AWS long-term and temporary access key ids
	}, "|") + `)`),

	// AMBIGUOUS SHORT PREFIXES, ANCHORED. These are two or three characters and
	// occur inside ordinary words: unanchored, `sk-[A-Za-z0-9]{16,}` matches
	// inside "task-1234567890abcdef" and `tok_` inside any word ending "tok".
	// Redacting a branch name out of a build log is the over-matching that
	// costs a reader real diagnostics, so these keep their left boundary and
	// accept the narrower coverage.
	regexp.MustCompile(`\b(sk-[A-Za-z0-9]{16,}|tok_[A-Za-z0-9_-]{12,})`),

	// key = value, key: value, key := "value", "client_secret": "value".
	//
	// The separator alternation puts `:=` FIRST so it is preferred over a bare
	// `:`, and the value side tolerates a leading quote and stops at the
	// closing one — the two mistakes that left a value visible before.
	// TWO VALUE FORMS, AND THE QUOTED ONE IS FIRST. A quoted value may contain
	// spaces — `client_secret: "alpha beta"` — and a pattern that stopped at
	// whitespace redacted through "alpha" and left ` beta"` on the page. That
	// is the partial redaction that looks like it worked, which this file has
	// now produced three times in three different patterns.
	regexp.MustCompile(`(?i)"?\b[A-Za-z0-9_.-]*(token|secret|password|passwd|api[_-]?key|apikey|credential|private[_-]?key)[A-Za-z0-9_.-]*"?[ \t]*(:=|=|:)[ \t]*("[^"\r\n]*"|'[^'\r\n]*'|[^\s"',;)]+)`),
}

// redactedMarker replaces a matched credential shape.
const redactedMarker = "[redacted]"

// redactSecrets replaces every known credential shape in text.
//
// SEPARATE FROM publicOutput BECAUSE THE BOUND IS NOT UNIVERSAL AND THE
// REDACTION IS. A parked reason is a forge comment and wants 1200 bytes of the
// HEAD; a failed check's log excerpt is a worker prompt and wants the TAIL,
// because a build states what broke at its end. Those are two budgets for two
// audiences — but "what a secret looks like" must have exactly ONE definition,
// or the next consumer of untrusted output copies the list and the copy is
// where a shape goes missing.
//
// Best-effort, with the whole of this file's honesty about that: it bounds the
// exposure of output the module did not write, it does not eliminate it.
func redactSecrets(text string) string {
	for _, shape := range secretShapes {
		text = shape.ReplaceAllString(text, redactedMarker)
	}
	return text
}

// publicOutput turns subprocess output into text that may be posted publicly.
//
// Redacted, then bounded, and in that order: bounding first could cut a
// credential in half and leave the visible half in the comment.
func publicOutput(raw string) string {
	text := strings.TrimSpace(raw)
	if text == "" {
		return ""
	}
	text = redactSecrets(text)
	if len(text) <= publicOutputLimit {
		return text
	}
	// The head is kept rather than the tail: a build failure states what broke
	// before it states how far it got.
	//
	// CUT ON A RUNE BOUNDARY, NOT A BYTE ONE (review 1376). A byte slice at the
	// limit can split a multibyte character; the JSON encoding then replaces the
	// broken tail with U+FFFD and the diagnostic posted to the issue is
	// corrupted for no reason. This is the same family as the redaction
	// mistakes above — an operation that stops in the wrong place and produces
	// something that looks fine.
	//
	// The byte cap still bounds the KEPT HEAD: backing up to a boundary only
	// ever removes bytes, so the diagnostic carried here never exceeds the
	// limit. The returned string is longer, because the truncation notice below
	// is appended after it — the cap governs how much subprocess output is
	// published, not the length of the whole reply.
	//
	// BOUNDED AT ONE RUNE'S WIDTH. A valid character needs at most three bytes
	// of back-up; text needing more was never valid UTF-8, and subprocess
	// output is arbitrary bytes. Unbounded, a long run of continuation bytes
	// walks the cut to zero and withholds the ENTIRE diagnostic — the silent
	// total loss this file exists to prevent, arrived at while fixing a
	// cosmetic one. Past the bound there is no boundary to find, and cutting at
	// the limit is as good as anywhere.
	cut := publicOutputLimit
	floor := max(0, cut-utf8.UTFMax+1)
	for cut > floor && !utf8.RuneStart(text[cut]) {
		cut--
	}
	withheld := len(text) - cut
	return strings.TrimSpace(text[:cut]) +
		"\n… (" + strconv.Itoa(withheld) + " more bytes withheld; see the daemon log)"
}

// =============================================================================
// Diagnostic text
// =============================================================================
//
// A DIAGNOSTIC IS READ BY AN OPERATOR, SO IT SAYS WHAT HAPPENED.
//
// The error types in this package withhold the output of the commands they
// report, and say so by counting the bytes they are not showing. That rule was
// written for a push whose remote may echo a credential, and it is the right
// rule — but applied to the whole of a worker's output it withheld the ONE line
// that diagnosed a failure: `checkout: git clone … exited 128 (102 bytes of
// output withheld)`, where the 102 bytes were `sh: ssh: not found` (#751).
// Reading them took exec'ing into a leaked container by hand.
//
// So the rule is kept and the blindness is not: the output goes through the
// same redactSecrets every publishable reason goes through, is bounded, and the
// result is what the error says. The exposure is the same shape publicOutput
// already accepts for a forge comment — and an error string is the narrower
// audience of the two, because it reaches the daemon log and the task record
// rather than a public issue.

// diagnosticTailLimit caps how much of a command's output an error carries.
//
// The TAIL, not the head, and 4 KiB of it: a subprocess states what stopped it
// last, and a clone that fails after pages of progress output has its cause on
// the final line. The cap bounds both the unreadability of an error string and
// how much a miss in the redaction patterns can expose.
const diagnosticTailLimit = 4096

// diagnosticTail is the scrubbed, bounded tail of a command's output, for an
// error that must name the cause rather than count it.
//
// Empty output yields an empty string, so a caller renders nothing rather than
// an empty excerpt.
func diagnosticTail(raw string) string {
	text := strings.TrimSpace(redactSecrets(raw))
	if text == "" {
		return ""
	}
	tail, truncated := boundedTail(text, diagnosticTailLimit)
	if truncated {
		return "… " + tail
	}
	return tail
}

// boundedTail returns the last limit bytes of text, cut on a line boundary and
// never inside a rune, and reports whether anything was dropped.
//
// ONE DEFINITION OF "THE TAIL", for the same reason redactSecrets is one
// definition of "a secret": the cut that splits a multibyte character produces
// a corrupted excerpt, and a second copy of this arithmetic is where that
// mistake comes back.
func boundedTail(text string, limit int) (string, bool) {
	if limit <= 0 || len(text) <= limit {
		return strings.TrimSpace(text), false
	}
	cut := text[len(text)-limit:]
	// A whole line reads; half a line misleads. When the window opens mid-line
	// the partial head of it is dropped in favour of the first complete line.
	if nl := strings.IndexByte(cut, '\n'); nl >= 0 {
		return strings.TrimSpace(cut[nl+1:]), true
	}
	// No newline in the window: keep it all, starting at the first rune
	// boundary so the excerpt is valid UTF-8.
	for i := 0; i < utf8.UTFMax && i < len(cut); i++ {
		if utf8.RuneStart(cut[i]) {
			return strings.TrimSpace(cut[i:]), true
		}
	}
	return strings.TrimSpace(cut), true
}
