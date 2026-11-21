package sdlc

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// ReleaseKind is the channel a release tag is cut for.
type ReleaseKind string

const (
	// ReleaseAlpha is a pre-release tag vX.Y.Z-alpha, cut from a green main
	// with no owner's authorization.
	ReleaseAlpha ReleaseKind = "alpha"
	// ReleaseStaging is a release candidate vX.Y.Z-rcN or vX.Y.Z-staging,
	// cut from a green main with no owner's authorization.
	ReleaseStaging ReleaseKind = "staging"
	// ReleaseProduction is a plain vX.Y.Z, cut from a green main and only
	// with an owner's authorization naming the tag and the commit.
	ReleaseProduction ReleaseKind = "production"
)

// ReleaseRequest asks for one tag at one commit.
type ReleaseRequest struct {
	Kind ReleaseKind
	// Tag is the tag to cut, vMAJOR.MINOR.PATCH[-pre].
	Tag string
	// SHA is main's tip as the caller read it, a full 40-hex commit SHA. A
	// short SHA is refused: the authorization and the checks must name
	// exactly one commit.
	SHA string
	// ReleaseIssue is the issue the release is discussed on, where a
	// production authorization is written and the consumer check reported.
	ReleaseIssue int
}

// ReleaseRefusal names why a release was refused.
type ReleaseRefusal string

const (
	// ReleaseTagInvalid: the tag is not vMAJOR.MINOR.PATCH[-pre], or the
	// commit is not a full SHA.
	ReleaseTagInvalid ReleaseRefusal = "release_tag_invalid"
	// ReleaseKindMismatch: the tag's pre-release does not match the kind.
	ReleaseKindMismatch ReleaseRefusal = "release_kind_mismatch"
	// ReleaseTagExists: the repository already carries the tag.
	ReleaseTagExists ReleaseRefusal = "release_tag_exists"
	// ReleaseMainNotGreen: main's tip is red, still running, or carries no
	// check at all.
	ReleaseMainNotGreen ReleaseRefusal = "release_main_not_green"
	// ReleaseNotAuthorized: a production release has no owner's
	// authorization naming this tag and this commit.
	ReleaseNotAuthorized ReleaseRefusal = "release_not_authorized"
)

// ReleaseDecision is the release gate's answer.
type ReleaseDecision struct {
	Allowed bool
	Refusal ReleaseRefusal
	Detail  string
}

// releaseAuthorizationPrefix opens the line an owner writes on the release
// issue to authorize a production release: "release-authorized: <tag> <sha>".
const releaseAuthorizationPrefix = "release-authorized:"

// consumerCheckMarker opens the comment ReportConsumerCheck posts.
const consumerCheckMarker = "quicbaas-sdlc:consumer-check"

// consumerCheckDetailLimit bounds the detail quoted in a consumer check
// report, in bytes.
const consumerCheckDetailLimit = 4000

var (
	releaseTagPattern = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-([0-9A-Za-z]+(?:[.-][0-9A-Za-z]+)*))?$`)
	fullSHAPattern    = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)
)

func refuseRelease(refusal ReleaseRefusal, detail string) ReleaseDecision {
	return ReleaseDecision{Refusal: refusal, Detail: detail}
}

// EvaluateReleaseGate decides whether req may be tagged. It is pure: existing
// is the tag as the forge reports it (nil when absent), checks are the check
// records at req.SHA, comments are the release issue's discussion and rulers
// the logins whose authorization counts.
//
// The order is fixed: tag shape, kind, existing tag, green checks, and for
// production the owner's authorization. Green is always required, so a head
// with no check at all is refused like a red one.
func EvaluateReleaseGate(req ReleaseRequest, existing *ForgeTag, checks []CheckRecord, comments []ForgeComment, rulers map[string]struct{}) ReleaseDecision {
	match := releaseTagPattern.FindStringSubmatch(req.Tag)
	if match == nil {
		return refuseRelease(ReleaseTagInvalid, fmt.Sprintf("%q is not vMAJOR.MINOR.PATCH[-pre]", req.Tag))
	}
	if !fullSHAPattern.MatchString(req.SHA) {
		return refuseRelease(ReleaseTagInvalid, fmt.Sprintf("%q is not a full 40-hex commit SHA", req.SHA))
	}
	if detail, ok := releaseKindMatches(req.Kind, match[4]); !ok {
		return refuseRelease(ReleaseKindMismatch, detail)
	}
	if existing != nil {
		return refuseRelease(ReleaseTagExists, fmt.Sprintf("tag %s already exists at %s", req.Tag, shortSHA(existing.SHA)))
	}
	if reason, detail := evaluateChecks(checks, req.SHA); reason != GateClear {
		return refuseRelease(ReleaseMainNotGreen, fmt.Sprintf("main at %s is not green (%s): %s", shortSHA(req.SHA), reason, detail))
	}
	if req.Kind == ReleaseProduction && !releaseAuthorized(req, comments, rulers) {
		return refuseRelease(ReleaseNotAuthorized, fmt.Sprintf("no owner's authorization %q on issue #%d",
			releaseAuthorizationPrefix+" "+req.Tag+" "+req.SHA, req.ReleaseIssue))
	}
	return ReleaseDecision{Allowed: true}
}

// releaseKindMatches reports whether a tag's pre-release suits the kind.
func releaseKindMatches(kind ReleaseKind, pre string) (string, bool) {
	channel := strings.ToLower(strings.TrimRight(strings.SplitN(pre, ".", 2)[0], "0123456789"))
	switch kind {
	case ReleaseAlpha:
		if channel == "alpha" {
			return "", true
		}
		return "an alpha release needs an -alpha tag", false
	case ReleaseStaging:
		if channel == "rc" || channel == "staging" {
			return "", true
		}
		return "a staging release needs an -rc or -staging tag", false
	case ReleaseProduction:
		if pre == "" {
			return "", true
		}
		return "a production release takes no pre-release suffix", false
	default:
		return fmt.Sprintf("unknown release kind %q", kind), false
	}
}

// releaseAuthorized reports whether a ruler wrote, on its own line,
// "release-authorized: <tag> <sha>" for exactly this tag and full SHA.
func releaseAuthorized(req ReleaseRequest, comments []ForgeComment, rulers map[string]struct{}) bool {
	want := releaseAuthorizationPrefix + " " + req.Tag + " " + strings.ToLower(req.SHA)
	for _, comment := range comments {
		if !isRuling(comment, rulers) {
			continue
		}
		for _, line := range strings.Split(comment.Body, "\n") {
			fields := strings.Fields(line)
			if len(fields) == 3 && strings.Join([]string{fields[0], fields[1], strings.ToLower(fields[2])}, " ") == want {
				return true
			}
		}
	}
	return false
}

// NextAlphaTag returns the alpha tag after latest, as the Makefile's
// release-alpha target computes it: the patch number is bumped and the
// pre-release becomes -alpha. An empty latest starts at v0.0.1-alpha.
func NextAlphaTag(latest string) (string, error) {
	if latest == "" {
		latest = "v0.0.0-alpha"
	}
	match := releaseTagPattern.FindStringSubmatch(latest)
	if match == nil {
		return "", fmt.Errorf("sdlc: %q is not vMAJOR.MINOR.PATCH[-pre]", latest)
	}
	patch, err := strconv.Atoi(match[3])
	if err != nil {
		return "", fmt.Errorf("sdlc: patch of %q: %w", latest, err)
	}
	return fmt.Sprintf("v%s.%s.%d-alpha", match[1], match[2], patch+1), nil
}

// ConsumerCheckResult is the outcome of building one consumer against a
// release tag.
type ConsumerCheckResult struct {
	Tag      string
	SHA      string
	Consumer string
	Passed   bool
	Detail   string
}

func consumerCheckMarkerFor(tag, consumer string) string {
	return "<!-- " + consumerCheckMarker + " tag=" + tag + " consumer=" + consumer + " -->"
}

// ReportConsumerCheck posts the consumer check's result on the release issue,
// once: when a comment there already carries this tag's and consumer's
// marker, nothing is posted. Forge errors are returned.
func ReportConsumerCheck(ctx context.Context, forge ForgeBackend, repo RepoRef, issue int, r ConsumerCheckResult) error {
	marker := consumerCheckMarkerFor(r.Tag, r.Consumer)
	comments, err := forge.ListIssueComments(ctx, repo, issue)
	if err != nil {
		return fmt.Errorf("sdlc: read release issue #%d: %w", issue, err)
	}
	for _, comment := range comments {
		if strings.Contains(comment.Body, marker) {
			return nil
		}
	}
	verdict := "passed"
	if !r.Passed {
		verdict = "FAILED"
	}
	var body strings.Builder
	body.WriteString(marker + "\n")
	fmt.Fprintf(&body, "Consumer check %s: %s at %s against %s.\n", verdict, r.Tag, shortSHA(r.SHA), r.Consumer)
	if detail := strings.TrimSpace(r.Detail); detail != "" {
		detail = limitBytes(detail, consumerCheckDetailLimit)
		fence := codeFenceFor(detail)
		body.WriteString("\n" + fence + "\n" + detail + "\n" + fence + "\n")
	}
	if _, err := forge.CommentOnIssue(ctx, repo, issue, body.String()); err != nil {
		return fmt.Errorf("sdlc: report consumer check on #%d: %w", issue, err)
	}
	return nil
}

// limitBytes cuts s to at most n bytes without splitting a UTF-8 sequence.
func limitBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := s[:n]
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return cut
}

// codeFenceFor returns a backtick fence longer than any backtick run in text,
// so the text cannot close its own block.
func codeFenceFor(text string) string {
	longest, run := 0, 0
	for _, r := range text {
		if r == '`' {
			run++
			if run > longest {
				longest = run
			}
			continue
		}
		run = 0
	}
	if longest < 3 {
		return "```"
	}
	return strings.Repeat("`", longest+1)
}

// ReleaseGate reads what EvaluateReleaseGate decides on from the forge.
type ReleaseGate struct {
	// Rulers are logins whose authorization counts beyond the binding's
	// owner and rulers.
	Rulers []string
	// Now stamps check records; nil takes time.Now.
	Now func() time.Time
}

// Evaluate reads the existing tag, the checks at req.SHA and, for a
// production release, the release issue's discussion, then decides. A forge
// read that fails is returned as an error, never as a decision.
func (g *ReleaseGate) Evaluate(ctx context.Context, forge ForgeBackend, binding *ProjectBinding, repo RepoRef, req ReleaseRequest) (ReleaseDecision, error) {
	now := time.Now
	var configured []string
	if g != nil {
		configured = g.Rulers
		if g.Now != nil {
			now = g.Now
		}
	}
	var existing *ForgeTag
	if match := releaseTagPattern.MatchString(req.Tag); match {
		tag, err := forge.GetTag(ctx, repo, req.Tag)
		switch {
		case err == nil:
			existing = tag
		case errors.Is(err, ErrNotFound):
		default:
			return ReleaseDecision{}, fmt.Errorf("sdlc: read tag %s: %w", req.Tag, err)
		}
	}
	var checks []CheckRecord
	if fullSHAPattern.MatchString(req.SHA) {
		runs, err := forge.ListCheckRuns(ctx, repo, req.SHA)
		if err != nil {
			return ReleaseDecision{}, fmt.Errorf("sdlc: read checks at %s: %w", shortSHA(req.SHA), err)
		}
		checks = checkRecordsFor(runs, req.SHA, now())
	}
	var comments []ForgeComment
	if req.Kind == ReleaseProduction && req.ReleaseIssue > 0 {
		read, err := forge.ListIssueComments(ctx, repo, req.ReleaseIssue)
		if err != nil {
			return ReleaseDecision{}, fmt.Errorf("sdlc: read release issue #%d: %w", req.ReleaseIssue, err)
		}
		comments = read
	}
	return EvaluateReleaseGate(req, existing, checks, comments, rulerSet(binding, configured)), nil
}
