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
	"strconv"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/prompts"
)

// =============================================================================
// The reviewer's charter, and reading a verdict out of what it answered
// =============================================================================
//
// THE VERDICT IS READ FROM STRUCTURE, NEVER FROM PROSE — the same rule
// turn.go applies to a worker's stop declaration, for the same reason. A
// verdict decides whether a change lands, so grepping a paragraph for
// "looks good" would let a sentence in a quoted code block, a summary of
// somebody else's opinion, or a polite opening line merge a change nobody
// approved.
//
// So the reviewer declares, on the last line, a marker and a code from a
// closed set, and everything above it is the findings. Three properties fall
// out of that and each is a defect the shell automation shipped:
//
//   - A REVIEW WITH NO DECLARATION IS NOT A VERDICT. It is an error, and it
//     blocks: the gate's VerdictError path already fails closed on exactly
//     this, because a reviewer that could not state an answer has not given
//     one.
//   - A VERDICT WITH NO FINDINGS IS NOT A VERDICT EITHER. An approval whose
//     body is empty is what a crashed, truncated or timed-out reviewer
//     produces, and it is indistinguishable from an examined approval once
//     posted. Refused here, before it reaches the forge.
//   - AN UNKNOWN CODE IS NOT AN APPROVAL. It is an error, because every
//     cheap guess resolves towards "carry on".

// ReviewVerdictMarker introduces the reviewer's verdict declaration.
const ReviewVerdictMarker = "SDLC-REVIEW:"

// The verdict codes a reviewer may declare. A CLOSED SET.
const (
	// ReviewCodeApprove accepts the head under review.
	ReviewCodeApprove = "approve"

	// ReviewCodeRequestChanges refuses the head, with findings.
	ReviewCodeRequestChanges = "request-changes"

	// ReviewCodeComment records findings without deciding. It is what a
	// reviewer says about a draft, and about a change it has read but is not
	// the right judge of.
	ReviewCodeComment = "comment"
)

// reviewCodeVerdicts is the vocabulary: the code a reviewer declares, and the
// verdict it means. Map-based so a code answers in constant time and the set
// is data a completeness test can walk.
var reviewCodeVerdicts = map[string]ReviewVerdict{
	ReviewCodeApprove:        VerdictApprove,
	ReviewCodeRequestChanges: VerdictRequestChanges,
	ReviewCodeComment:        VerdictComment,
}

// ReviewerCIGreenMarker is the reviewer charter's statement that the head's
// pipeline has already passed.
//
// IT IS A MARKER BECAUSE IT IS NOW TRUE BY CONSTRUCTION. ReviewDispatcher
// refuses to start a review until the head's required checks are green
// (review_dispatch.go), so the charter is not making a hopeful claim — it is
// telling the reviewer the one thing about its situation it cannot observe.
// A charter edit that dropped the sentence would leave an agent re-running a
// suite to answer a question that was settled before it was spawned, which is
// exactly the spend go-quicbaas#927 measured.
const ReviewerCIGreenMarker = "CI has already run and passed on this head"

// ReviewerNoRerunMarker is the charter's instruction not to redo the
// pipeline's work.
//
// SAYING CI PASSED IS NOT THE SAME AS SAYING "SO DO NOT RUN IT". An agent
// told only the first will still reach for `make test`, because running the
// tests is what a careful reviewer does when nothing forbids it. Both halves
// are therefore required to stay in the body.
const ReviewerNoRerunMarker = "Do not run the build, the tests, the linters or the scanners"

// ReviewerCharter is the reviewer role's system prompt.
//
// The literals ParseReviewVerdict reads are declared as markers on the prompt
// specification (prompt_set.go, reviewerCharterMarkers), so a charter cannot
// be saved that stops naming a code the parser knows — the same coupling the
// old concatenated constant achieved, now checked rather than assumed.
func ReviewerCharter(set prompts.Renderer) string { return set.MustRender("reviewer_charter", nil) }

// ReviewOutcome is one reviewer turn, read.
type ReviewOutcome struct {
	// Verdict is what the reviewer decided.
	Verdict ReviewVerdict

	// Findings is everything the reviewer wrote above its declaration.
	Findings string

	// Declared says a verdict declaration was found at all.
	Declared bool

	// Code is the declared code, whether or not it is in the vocabulary.
	Code string
}

// Submission renders the outcome as the verdict to post.
func (o ReviewOutcome) Submission(headSHA string) ReviewSubmission {
	return ReviewSubmission{Verdict: o.Verdict, Body: o.Findings, HeadSHA: headSHA}
}

// ParseReviewVerdict reads a reviewer's reply into an outcome.
//
// THE DECLARATION IS TAKEN FROM THE LAST LINE THAT CARRIES ONE, scanning
// upwards, and the findings are everything before it. Scanning upwards rather
// than downwards matters: a reviewer quoting the charter, or quoting a
// previous round's declaration inside its findings, must not have that quote
// read as this round's answer.
//
// A verdict is returned only when the code is known AND the findings are not
// empty. Everything else is reported as not-a-verdict for the caller to
// record as an error, which is the fail-closed direction.
func ParseReviewVerdict(reply string) (ReviewOutcome, error) {
	lines := strings.Split(reply, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		code, found := reviewDeclarationOn(lines[i])
		if !found {
			continue
		}
		outcome := ReviewOutcome{
			Declared: true,
			Code:     code,
			Findings: reviewFindings(lines[:i]),
		}
		// THE VERDICT IS NOT SET ON A REFUSAL. A caller that ignored the
		// error would otherwise read an approval off an outcome the parser
		// had just rejected, which is the one mistake this whole file exists
		// to make impossible.
		if _, known := reviewCodeVerdicts[code]; !known {
			return outcome, &ReviewVerdictUnreadableError{
				Reason: "the reviewer declared " + strconv.Quote(code) +
					", which is not one of approve, request-changes or comment",
			}
		}
		if outcome.Findings == "" {
			return outcome, &ReviewVerdictUnreadableError{
				Reason: "the reviewer declared " + code + " and wrote nothing above it; " +
					"a verdict with no findings is indistinguishable from a review that never read the change",
			}
		}
		outcome.Verdict = reviewCodeVerdicts[code]
		return outcome, nil
	}
	return ReviewOutcome{Findings: strings.TrimSpace(reply)}, &ReviewVerdictUnreadableError{
		Reason: "the reviewer's reply carries no " + ReviewVerdictMarker + " declaration, so it states no verdict",
	}
}

// reviewFindings is everything the reviewer wrote above its declaration, or
// empty when it wrote nothing that says anything.
//
// A LINE OF BACKTICKS IS NOT A FINDING. An agent shown the declaration inside
// a code fence reproduces the fence, so the literal reply is an opening
// fence, the declaration, and a closing fence — three lines, one of which is
// above the declaration. Counting that as findings would make the emptiest
// possible approval the one that passed, which is failure 6 surviving its own
// fix.
func reviewFindings(lines []string) string {
	joined := strings.TrimSpace(strings.Join(lines, "\n"))
	if joined == "" {
		return ""
	}
	for _, line := range strings.Split(joined, "\n") {
		if strings.TrimSpace(strings.Trim(line, "`~-*_> \t")) != "" {
			return joined
		}
	}
	return ""
}

// reviewDeclarationOn reads a declaration off one line, tolerating the
// decoration an agent puts around a literal it was shown in a code fence.
func reviewDeclarationOn(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	trimmed = strings.TrimPrefix(trimmed, "> ")
	trimmed = strings.TrimSpace(strings.Trim(trimmed, "*`"))
	if !strings.HasPrefix(trimmed, ReviewVerdictMarker) {
		return "", false
	}
	code := strings.TrimSpace(strings.TrimPrefix(trimmed, ReviewVerdictMarker))
	return strings.ToLower(strings.Trim(code, "`'\"*. ")), true
}
