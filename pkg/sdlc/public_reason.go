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

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
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
// SUBPROCESS OUTPUT IS NO LONGER PUBLISHED AT ALL (#213, ruling 189). Pattern
// redaction cannot be complete: a secret printed in a shape no pattern knows
// passes through, so redacting output and then posting it fails open. A failed
// check now reaches a forge issue only as the structured summary below
// (publicCheckSummary), and its output stays in the job's audit record. What
// publicOutput still bounds is AUTHORED text: the worker's own words, a
// readiness assessment, a forge adapter's error. For that text the patterns
// are a second line of defence, not the thing that makes it publishable.

// publicOutputLimit caps how much text a published reason may carry.
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

// publicOutput bounds and scrubs authored text that may be posted publicly.
//
// NOT FOR SUBPROCESS OUTPUT. Redaction fails open on a secret shape it does
// not know, so a command's output is published only as publicCheckSummary.
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
		"\n… (" + strconv.Itoa(withheld) + " more bytes withheld; diagnostic truncated for publication)"
}

// =============================================================================
// Public check summary
// =============================================================================
//
// A FAILED CHECK IS PUBLISHED AS FIELDS, NEVER AS ITS OUTPUT (#213, ruling 189).
//
// A check's output is written by a process the module does not control, and
// redaction only removes the secret shapes it knows about. A bare database
// password, a recovery phrase or an internal token with no known prefix gets
// past every pattern above. Publishing redacted output is therefore fail-open,
// and security-review.md §6 blocks fail-open in review. So the park reason a
// forge issue carries for a failed check holds only fields the output never
// supplies:
//
//   - the command, which is the operator's configuration (the binding's
//     verification list), not the subprocess's output;
//   - the exit status, an integer;
//   - how many bytes of output were withheld, an integer;
//   - the job whose audit record holds the failing lines.
//
// THE OUTPUT IS NOT LOST; IT IS KEPT WHERE IT IS NOT PUBLISHED. The job's
// `sdlc:verification` audit record carries each failed command's failing
// lines (auditVerificationResults), and the worker's feedback prompt carries
// the full failure. The summary names the job and the record, so the person
// who takes the escalation is pointed at the cause instead of left to search
// the daemon log for it. That pointer is what keeps the park actionable (#198).
//
// IT DOES NOT DEPEND ON THE FORGE BEING PRIVATE. Every field is safe to post
// on a hosted or public forge, so a change of forge does not reopen this.

// publicCheckSummary is the publishable account of one failed check.
type publicCheckSummary struct {
	// Command is the verification command as the binding configures it.
	Command string
	// ExitCode is the status the command exited with; zero for a command
	// that failed without running to an exit status.
	ExitCode int
	// WithheldBytes is the size of the output kept out of publication.
	WithheldBytes int
}

// newPublicCheckSummary takes only the fields of result that its output does
// not supply. Message and Details are read for their length only.
func newPublicCheckSummary(result orchestration.ValidationResult) publicCheckSummary {
	return publicCheckSummary{
		Command:       result.Name,
		ExitCode:      result.ExitCode,
		WithheldBytes: len(result.Message) + len(result.Details),
	}
}

// String renders the summary as one line of an issue comment.
//
// A backtick in the command would close the code span early and let the rest
// render as markdown, so it is replaced.
func (s publicCheckSummary) String() string {
	verdict := "failed"
	if s.ExitCode != 0 {
		verdict = "exited " + strconv.Itoa(s.ExitCode)
	}
	return "- `" + strings.ReplaceAll(s.Command, "`", "'") + "` " + verdict +
		"; " + strconv.Itoa(s.WithheldBytes) + " bytes of output withheld from publication"
}

// publicVerificationSummary renders the failed checks of a verification as
// the structured public summary, in at most budget bytes.
//
// THE POINTER TO THE OUTPUT IS RESERVED FIRST. A summary that lists checks and
// drops the line saying where their output is would send the reader nowhere,
// so the pointer is always kept and the checks that do not fit after it are
// counted instead of listed.
func publicVerificationSummary(jobID string, failed []orchestration.ValidationResult, budget int) string {
	if len(failed) == 0 || budget <= 0 {
		return ""
	}
	pointer := "The output is not published. The failing lines are in the `" + auditVerification +
		"` audit record of job " + jobID + "."
	if len(pointer) > budget {
		return ""
	}
	used := len(pointer)
	var lines []string
	for i, result := range failed {
		line := newPublicCheckSummary(result).String()
		remaining := len(failed) - i - 1
		// Room for this line, and for the count of any that would follow it.
		reserve := 0
		if remaining > 0 {
			reserve = len(moreFailedChecks(remaining)) + 1
		}
		if used+len(line)+1+reserve > budget {
			// The previous line reserved room for this count; the first line
			// had no previous one, so the count is checked like any other.
			if more := moreFailedChecks(len(failed) - i); used+len(more)+1 <= budget {
				lines = append(lines, more)
			}
			break
		}
		lines = append(lines, line)
		used += len(line) + 1
	}
	return strings.Join(append(lines, pointer), "\n")
}

// moreFailedChecks counts the failed checks a summary had no room to list.
func moreFailedChecks(n int) string {
	return "- and " + strconv.Itoa(n) + " more failed check(s)"
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
