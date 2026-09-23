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
	"context"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// =============================================================================
// The forge seam
// =============================================================================
//
// A ForgeBackend is the module's whole view of a code-hosting platform. Gitea,
// GitHub and GitLab differ in their APIs and in what they can do at all, and
// both kinds of difference are handled here so nothing above this line has to
// know which forge it is talking to.
//
// THREE RULES SHAPE EVERY ADAPTER, all three learned from the shell scripts:
//
//   - CAPABILITIES ARE DATA, NOT MISSING CODE. A platform that cannot cancel a
//     CI run says so through ForgeCapabilities. It never ships a Cancel method
//     that quietly does nothing, because the engine's behaviour has to differ —
//     an uncancellable run becomes an ORPHAN to record and surface, not a
//     cancellation to assume. A silent no-op turns a capacity leak into an
//     invisible one.
//
//   - EVERY WRITE IS VERIFIED BY READ-BACK. Gitea has documented cases of
//     returning 200 for a write that did not take effect — the label call this
//     module once depended on among them. An adapter that trusts a status code
//     reports success for work that never happened, so each write reads its
//     subject back and compares.
//
//   - WEBHOOKS ARE HINTS, NEVER FACTS. An event prompts a reconcile; it never
//     decides anything by itself. Deliveries are lost, duplicated, delayed and
//     forged, so the polling reconciler is the floor and dispatch decisions
//     read quicbaas records only. Labels in particular are display output: the
//     shell scripts used label swaps as its claim mechanism and lost work to
//     every race that implies.

// ForgeType and its constants are declared in types.go, where SDLCProject's
// ForgeConfig already selects one. A second declaration here would be the same
// enumeration in two places.

// RepoRef identifies one repository on one forge.
type RepoRef struct {
	Owner string `json:"owner"`
	Repo  string `json:"repo"`
}

// String renders the reference as "owner/repo".
func (r RepoRef) String() string { return r.Owner + "/" + r.Repo }

// ParseRepoRef recovers a reference from its "owner/repo" rendering.
//
// The inverse of String, and exact rather than lenient: a reference with a
// missing half, or with a path segment too many, names no repository and is
// refused here rather than turned into a forge call for something that cannot
// exist.
func ParseRepoRef(ref string) (RepoRef, error) {
	owner, repo, ok := strings.Cut(strings.TrimSpace(ref), "/")
	if !ok || owner == "" || repo == "" || strings.Contains(repo, "/") {
		return RepoRef{}, &InvalidInputError{
			Field:   "repo",
			Message: "expected owner/repo, got " + strconv.Quote(ref),
		}
	}
	return RepoRef{Owner: owner, Repo: repo}, nil
}

// =============================================================================
// Capabilities
// =============================================================================

// ForgeCapabilities records what a PLATFORM can do, as data the engine reads.
//
// Every field describes the forge, never the adapter's completeness. An adapter
// is registered only when it implements everything its platform supports, so a
// false here always means "this platform cannot", and the engine can plan
// around it. "This adapter has not got round to it yet" is not expressible on
// purpose — that is the state the completeness rule forbids.
type ForgeCapabilities struct {
	// CancelCheckRuns reports whether in-flight CI runs can be cancelled.
	//
	// Gitea cannot: it exposes no cancel route. Supersession therefore cannot
	// stop the displaced run there, which is exactly why SupersessionRecord
	// records an uncancelled displacement as an orphan rather than pretending
	// the run stopped.
	CancelCheckRuns bool `json:"cancel_check_runs"`

	// RerunCheckRuns reports whether a FINISHED run can be started again.
	//
	// It is what makes a void verdict actionable. pkg/cifault can say "this
	// red is not about the change — a runner died holding the job", but the
	// only useful response to that is to run the job again, and on a platform
	// that cannot, the engine has to escalate to an operator instead of
	// quietly waiting for a rerun nobody will ever trigger. Declaring it as
	// data is what lets the engine tell those two apart.
	//
	// Separate from CancelCheckRuns because the two are separate routes and
	// Gitea has exactly one of them: it publishes a rerun for a finished run
	// and no cancel for a running one.
	RerunCheckRuns bool `json:"rerun_check_runs"`

	// CheckRunLogs reports whether a finished run's output can be read back.
	//
	// It is what decides how much a synthesized checks-failed round can tell
	// the worker: with logs, the findings quote what actually failed; without
	// them, they can only name the red contexts and send the worker to
	// reproduce the pipeline locally. Both are actionable — this flag is the
	// difference between the two, not between working and not.
	CheckRunLogs bool `json:"check_run_logs"`

	// ReliableLabelEvents reports whether label webhook deliveries can be
	// trusted to arrive.
	//
	// Gitea's are unreliable. It changes nothing about correctness — labels are
	// display output and no decision reads them — but a reconciler that assumed
	// delivery would let displayed state drift from real state indefinitely.
	ReliableLabelEvents bool `json:"reliable_label_events"`

	// AdditiveLabels reports whether labels can be added and removed one at a
	// time, rather than only by replacing the whole set.
	//
	// It matters to display sync specifically, and the difference is data loss
	// rather than convenience. Whole-set replacement computes the successor
	// from labels read a moment earlier, so a label a human adds between that
	// read and the write is destroyed — and nothing notices, because the write
	// did exactly what it was told. Additive operations name only the module's
	// own labels and leave everything else untouched, which closes the window
	// instead of narrowing it.
	AdditiveLabels bool `json:"additive_labels"`

	// DraftPullRequests reports whether the platform has a draft PR state.
	DraftPullRequests bool `json:"draft_pull_requests"`

	// RequiredReviewers reports whether review requirements can be enforced by
	// the platform itself rather than only by the module's own review gate.
	RequiredReviewers bool `json:"required_reviewers"`

	// SquashMerge reports whether the platform can squash-merge through its
	// API. A merge strategy naming a method its forge lacks fails closed at
	// registration rather than merging the wrong way.
	SquashMerge bool `json:"squash_merge"`

	// DeleteBranchOnMerge reports whether the platform can delete the source
	// branch as part of the merge call.
	DeleteBranchOnMerge bool `json:"delete_branch_on_merge"`

	// BranchProtectionContexts reports whether the platform can require named
	// status-check contexts on a protected branch.
	//
	// It decides what a conformance pass MEANS by a branch that does not
	// require quicbaas's own contexts. Where the platform can express the
	// requirement, a branch that does not carry it is a finding an operator
	// fixes. Where it cannot, the same observation is not a finding at all,
	// and reporting one would send somebody looking for a setting that does
	// not exist.
	BranchProtectionContexts bool `json:"branch_protection_contexts"`
	// SubmitReviews reports whether a REVIEW VERDICT can be written through
	// the API, bound to the commit it judges.
	//
	// It is the write half of ListReviews, and the two are separate
	// capabilities because a platform can perfectly well let the module read
	// a human's verdict without letting it post one of its own. What turns on
	// it is whether this deployment can review its OWN pull requests: false
	// means every verdict must come from outside, so the review runner is not
	// dispatchable for that project and says so at registration rather than
	// failing per pull request.
	SubmitReviews bool `json:"submit_reviews"`

	// CommitStatuses reports whether a NAMED status can be written against a
	// commit.
	//
	// It is what makes a verdict enforceable rather than advisory. A review
	// the module posts is a record; a status the module writes is something
	// the platform's own branch protection can require, so a rejection stops
	// the merge even when every other path to it is bypassed. A platform
	// without it can still be reviewed — the module's own gate is unaffected
	// — but nothing outside the module will honour the verdict.
	CommitStatuses bool `json:"commit_statuses"`
}

// =============================================================================
// Wire records
// =============================================================================
//
// These are the forge's view of the world, deliberately separate from this
// package's own WorkItem, PullRequest and Review records. The module's records
// are what it decides with; these are what a forge reported at one moment. A
// single type serving both would make a remote observation indistinguishable
// from a decision the module made.

// ForgeIssue is an issue as the forge reports it.
type ForgeIssue struct {
	Number int      `json:"number"`
	Title  string   `json:"title"`
	Body   string   `json:"body"`
	State  string   `json:"state"`
	Labels []string `json:"labels,omitempty"`
	// Milestone is the milestone's title, empty when the issue has none.
	Milestone string    `json:"milestone,omitempty"`
	Author    string    `json:"author,omitempty"`
	URL       string    `json:"url,omitempty"`
	Created   time.Time `json:"created"`
	Updated   time.Time `json:"updated"`
	Assignee  string    `json:"assignee,omitempty"`
}

// ForgeComment is a comment on an issue or pull request.
type ForgeComment struct {
	ID      int64     `json:"id"`
	Body    string    `json:"body"`
	Author  string    `json:"author,omitempty"`
	Created time.Time `json:"created"`
}

// ForgeBranch is a branch and the commit it points at.
type ForgeBranch struct {
	Name string `json:"name"`
	SHA  string `json:"sha"`
}

// ForgeTag is a tag and the commit it names.
//
// The commit is carried even though no readiness check reads it today, because
// a tag's identity is the pair: two reads of one name can report different
// commits after a force-move, and a record keeping only the name could not tell
// that apart from the tag nobody touched.
type ForgeTag struct {
	Name string `json:"name"`
	SHA  string `json:"sha"`
}

// ForgePullRequest is a pull request as the forge reports it.
type ForgePullRequest struct {
	Number  int    `json:"number"`
	Title   string `json:"title"`
	Body    string `json:"body"`
	State   string `json:"state"`
	HeadRef string `json:"head_ref"`
	HeadSHA string `json:"head_sha"`
	BaseRef string `json:"base_ref"`

	// HeadRepo is the repository HeadRef lives in: the pull request's own
	// repository, or a fork. A branch NAME does not identify a source — a
	// fork can carry one with the same name — so anything that matches a
	// pull request to a branch must match this too. Zero when the forge
	// reports no head repository (a deleted fork).
	HeadRepo RepoRef `json:"head_repo"`

	Draft     bool      `json:"draft"`
	Merged    bool      `json:"merged"`
	MergeSHA  string    `json:"merge_sha,omitempty"`
	Mergeable bool      `json:"mergeable"`
	URL       string    `json:"url,omitempty"`
	Created   time.Time `json:"created"`
	Updated   time.Time `json:"updated"`
}

// ForgeReview is one review on a pull request.
//
// HeadSHA is load-bearing: a verdict applies to the tree it was given, and a
// review carried onto a later commit approves code nobody read. The module's
// own Review record binds the same way.
type ForgeReview struct {
	ID        int64     `json:"id"`
	Reviewer  string    `json:"reviewer"`
	State     string    `json:"state"`
	Body      string    `json:"body,omitempty"`
	HeadSHA   string    `json:"head_sha"`
	Submitted time.Time `json:"submitted"`
}

// ReviewSubmission is a verdict the module POSTS on one pull request.
//
// SCM-PORTABLE BY CONSTRUCTION, and each field is the portable half of a
// choice the adapters disagree about:
//
//   - Verdict is the module's own ReviewVerdict, never a platform's wire
//     string. Gitea and GitHub both spell approval "APPROVED" and refusal
//     "REQUEST_CHANGES"; GitLab has no review object at all and expresses the
//     same three outcomes as an approval, an unapproval plus a note, and a
//     note. A submission carrying "APPROVED" would have encoded one
//     platform's vocabulary into every caller.
//   - HeadSHA is REQUIRED, because a verdict is about a tree and not about a
//     pull request. Gitea and GitHub bind it natively (commit_id); an adapter
//     for a platform that does not must record the SHA in the body it writes,
//     so ListReviews can bind the verdict back. Making it optional would let
//     a verdict be posted that the gate could never attribute to a head —
//     which is failure 9 with the module itself as the cause.
//   - Body is REQUIRED for every verdict. See Validate.
type ReviewSubmission struct {
	// Verdict is what the reviewer decided.
	Verdict ReviewVerdict `json:"verdict"`

	// Body is the reviewer's findings. Required; see Validate.
	Body string `json:"body"`

	// HeadSHA is the commit the verdict judges. Required.
	HeadSHA string `json:"head_sha"`
}

// submittableVerdicts is the closed set a reviewer may post. VerdictError is
// absent deliberately: a review that could not be PRODUCED is the module's
// own record of its own failure, and posting it to the forge would publish an
// internal fault as a reviewer's opinion.
var submittableVerdicts = map[ReviewVerdict]bool{
	VerdictApprove:        true,
	VerdictRequestChanges: true,
	VerdictComment:        true,
}

// Validate refuses a submission no forge should be asked to store.
//
// AN EMPTY BODY IS REFUSED FOR EVERY VERDICT, and that is the fix for a real
// incident rather than tidiness. An approval with no body is
// indistinguishable from a review that never read the change: it is what a
// reviewer that crashed, timed out or answered with nothing produces, and it
// clears the gate exactly as an examined approval does. A refusal with no
// body is worse still — it blocks a change and tells the worker nothing to
// fix, which costs a whole review round to discover.
func (s ReviewSubmission) Validate() error {
	if !submittableVerdicts[s.Verdict] {
		return &InvalidInputError{
			Field:   "verdict",
			Message: "must be approve, request_changes or comment; got " + string(s.Verdict),
		}
	}
	if strings.TrimSpace(s.Body) == "" {
		return &InvalidInputError{
			Field:   "body",
			Message: "a verdict with no body is indistinguishable from a review that never read the change",
		}
	}
	if strings.TrimSpace(s.HeadSHA) == "" {
		return &InvalidInputError{
			Field:   "head_sha",
			Message: "a verdict is about a tree; one bound to no commit could never be attributed to a head",
		}
	}
	return nil
}

// CommitStatus is a named check's state against one commit, as the module
// WRITES it.
//
// SCM-PORTABLE BY CONSTRUCTION. Status is the module's own CheckStatus — the
// same vocabulary ForgeCheckRun is read back into — rather than any
// platform's spelling of it: Gitea writes pending/success/failure/error,
// GitHub adds a queued/in_progress/completed split with a separate
// conclusion, and GitLab writes pending/running/success/failed/canceled. Each
// adapter maps one way and reads back the other, so a caller never learns
// which platform it is on.
//
// Context is the status's NAME, which is the identity branch protection
// requires by. It is the caller's to choose and the adapter's to pass
// through unchanged: a platform-specific prefix invented in an adapter would
// mean the same logical check had a different required name per forge.
type CommitStatus struct {
	// Context names the check. Required.
	Context string `json:"context"`

	// Status is the state to publish. Required, and a status the platform
	// cannot express is an adapter error rather than a nearest match.
	Status CheckStatus `json:"status"`

	// Description is the one-line summary a person reads beside the check.
	Description string `json:"description,omitempty"`

	// TargetURL is where that person goes for the detail.
	TargetURL string `json:"target_url,omitempty"`
}

// Validate refuses a status that would publish nothing usable.
func (s CommitStatus) Validate() error {
	if strings.TrimSpace(s.Context) == "" {
		return &InvalidInputError{
			Field:   "context",
			Message: "a status with no name cannot be required by branch protection",
		}
	}
	if s.Status == "" {
		return &InvalidInputError{Field: "status", Message: "must name a state"}
	}
	return nil
}

// ForgeChangedFile is one file a pull request changes.
//
// The counts are carried because they are what lets a reviewer's prompt be
// scoped without fetching every patch: a listing is the map of the change,
// and the diff of a file is read only where the map says to look.
type ForgeChangedFile struct {
	// Path is the file's path after the change.
	Path string `json:"path"`

	// PreviousPath is where it was before a rename, empty otherwise.
	PreviousPath string `json:"previous_path,omitempty"`

	// Status is the platform's word for what happened to it — added,
	// modified, deleted, renamed, copied — lowercased.
	Status string `json:"status,omitempty"`

	// Additions and Deletions are the changed line counts.
	Additions int `json:"additions"`
	Deletions int `json:"deletions"`
}

// ForgeCheckRun is one CI run against one commit.
type ForgeCheckRun struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	HeadSHA    string    `json:"head_sha"`
	Status     string    `json:"status"`
	Conclusion string    `json:"conclusion,omitempty"`
	URL        string    `json:"url,omitempty"`
	Started    time.Time `json:"started"`
	Completed  time.Time `json:"completed,omitempty"`
}

// PullRequestSpec is what a branching strategy asks the forge to open.
type PullRequestSpec struct {
	Title   string   `json:"title"`
	Body    string   `json:"body"`
	HeadRef string   `json:"head_ref"`
	BaseRef string   `json:"base_ref"`
	Draft   bool     `json:"draft"`
	Labels  []string `json:"labels,omitempty"`
}

// MergeSpec is DECLARED IN strategy.go — the merge strategy produces it and
// the forge adapter consumes it, which is the whole point of the seam: the
// strategy decides how a change lands and the adapter carries that out. A
// second forge-side copy would let the two drift into disagreeing about what
// "squash" means.
//
// Its HeadSHA-equivalent guard lives on the adapter call rather than in the
// spec: MergePullRequest takes the head the caller approved so a commit pushed
// after the review cannot ride in on that review's approval.

// WebhookSpec describes the delivery the module wants configured.
type WebhookSpec struct {
	URL    string   `json:"url"`
	Events []string `json:"events"`

	// SecretRef names the secret in the SecretsService whose value signs
	// deliveries. The VALUE never travels in this struct — a spec is logged,
	// stored and compared, and a secret in it would leak through all three.
	SecretRef string `json:"secret_ref"`
}

// ForgeEventKind classifies a parsed webhook delivery.
type ForgeEventKind string

const (
	// EventIssueOpened, EventIssueEdited, EventIssueLabeled and
	// EventIssueClosed cover issue lifecycle deliveries.
	EventIssueOpened ForgeEventKind = "issue_opened"
	EventIssueEdited ForgeEventKind = "issue_edited"
	EventIssueClosed ForgeEventKind = "issue_closed"

	// EventIssueLabeled is a label added to or removed from an issue.
	//
	// It is its own kind rather than an edit because the two mean different
	// things to the two halves of the loop: an edit changes what the issue
	// ASKS FOR, a label change changes where the issue STANDS. Triage wakes
	// for both — a person clearing needs-info is how an answer gets noticed
	// — and before this kind existed the adapter mapped label actions to
	// EventUnsupported, so nothing woke for them at all.
	EventIssueLabeled ForgeEventKind = "issue_labeled"

	// EventPullRequestOpened, EventPullRequestUpdated, EventPullRequestMerged
	// and EventPullRequestClosed cover pull-request lifecycle deliveries.
	EventPullRequestOpened  ForgeEventKind = "pull_request_opened"
	EventPullRequestUpdated ForgeEventKind = "pull_request_updated"
	EventPullRequestMerged  ForgeEventKind = "pull_request_merged"
	EventPullRequestClosed  ForgeEventKind = "pull_request_closed"

	// EventReviewSubmitted covers a review verdict landing.
	EventReviewSubmitted ForgeEventKind = "review_submitted"

	// EventCheckRunUpdated covers CI status changes.
	EventCheckRunUpdated ForgeEventKind = "check_run_updated"

	// EventUnsupported is a delivery this adapter recognises as valid but does
	// not map. It is a KIND rather than an error because an unmapped event is
	// a normal thing for a forge to send, and treating it as a failure would
	// make ordinary traffic look like an outage.
	EventUnsupported ForgeEventKind = "unsupported"
)

// ForgeEvent is a parsed webhook delivery.
//
// It is a HINT that something may have changed, never a fact to act on: the
// fields say what the forge claimed, and the reconciler re-reads the subject
// before deciding anything.
type ForgeEvent struct {
	Kind ForgeEventKind `json:"kind"`
	Repo RepoRef        `json:"repo"`

	// IssueNumber and PullRequestNumber are set when the event names one.
	IssueNumber       int `json:"issue_number,omitempty"`
	PullRequestNumber int `json:"pull_request_number,omitempty"`

	// HeadSHA is set for events that name a commit.
	HeadSHA string `json:"head_sha,omitempty"`

	// Delivery is the forge's delivery identifier, for de-duplication and for
	// correlating a reconcile back to what prompted it.
	Delivery string `json:"delivery,omitempty"`
}

// IssueFilter narrows aListIssues call.
type IssueFilter struct {
	// State is "open", "closed" or "all". Empty means open.
	State string `json:"state,omitempty"`

	// Labels, when set, restricts to issues carrying ALL of them.
	Labels []string `json:"labels,omitempty"`

	// Since, when set, restricts to issues updated at or after it.
	Since time.Time `json:"since,omitempty"`

	// Limit bounds the TOTAL number of issues returned, across however many
	// pages the adapter has to fetch. Zero means every issue matching the
	// filter.
	//
	// THIS FIELD'S MEANING CHANGED, and a reader of an older call site needs to
	// know the old reading existed. It used to be handed straight to the forge
	// as a PAGE SIZE, so a caller that set nothing received one page and
	// believed it had everything — which left the polling reconciler silently
	// orphaning every issue past the first page, beneath the floor every other
	// correctness claim rests on. Any call site written against the old meaning
	// is asking a different question now: `Limit: 50` used to mean "fifty per
	// page, all pages" and now means "fifty issues, then stop".
	//
	// A caller that asks for no bound gets no bound. A caller that sets one
	// knows it may be seeing a prefix, because it chose the prefix.
	//
	// WHAT AN UNBOUNDED LISTING GETS, in the three cases that differ — an
	// adapter paginates under a ceiling of its own, and how it behaves at that
	// ceiling is the part worth stating rather than discovering:
	//
	//   - Fewer issues than the ceiling allows: all of them, no error. The
	//     forge ends the listing with a short page.
	//   - Exactly as many as the ceiling allows: all of them, no error. The
	//     adapter asks once more to establish that nothing was missed, rather
	//     than reporting a repository that ended on a page boundary as broken.
	//   - More than the ceiling allows: an error, and NO partial list. A short
	//     list with a nil error is indistinguishable from "that is all there
	//     is", and a polling reconciler would treat it as an authoritative view
	//     of the project — the original hole with a new cause.
	Limit int `json:"limit,omitempty"`
}

// =============================================================================
// ForgeBackend
// =============================================================================

// ForgeBackend is one code-hosting platform's adapter.
//
// Registered only when COMPLETE for its platform: every method its forge can
// support is implemented. A partially implemented adapter is the anti-pattern
// this rule exists to prevent — it makes the engine's behaviour depend on which
// forge it happens to be talking to, in ways nothing declares.
type ForgeBackend interface {
	// Type names the platform this adapter serves.
	Type() ForgeType

	// Capabilities reports what the PLATFORM can do. See ForgeCapabilities.
	Capabilities() ForgeCapabilities

	// ListIssues returns issues matching filter.
	ListIssues(ctx context.Context, repo RepoRef, filter IssueFilter) ([]ForgeIssue, error)

	// GetIssue returns one issue, or a NotFoundError.
	GetIssue(ctx context.Context, repo RepoRef, number int) (*ForgeIssue, error)

	// CommentOnIssue posts a comment and returns it as stored.
	CommentOnIssue(ctx context.Context, repo RepoRef, number int, body string) (*ForgeComment, error)

	// CloseIssue closes an issue, verified by read-back.
	//
	// IT IS SEPARATE FROM EVERY OTHER ISSUE VERB AND IT IS GATED. Labels are
	// reversible and carry no social weight; closing somebody's issue does,
	// and the triage charter forbade proposing it for exactly that reason. The
	// verb exists so a project that WANTS its stale reports closed can have
	// them closed — see ProjectBinding.TriageMayClose — and a project that
	// does not keeps today's behaviour, which is a comment and nothing else.
	//
	// Closing an already-closed issue is not an error: a pass that crashed
	// between the comment and the close must be able to finish.
	CloseIssue(ctx context.Context, repo RepoRef, number int) error

	// ListIssueComments returns an issue's whole discussion, oldest first.
	//
	// THE DISCUSSION IS PART OF THE CONTRACT, not decoration on it. An answer
	// to a park's question is written here and nowhere else — the issue body
	// is what the filer wrote, and a human answering a question adds a comment
	// rather than editing it — so a dispatch that cannot read this cannot know
	// what it has been asked to do (#263).
	//
	// WHOLE, and an adapter that cannot deliver the whole discussion reports a
	// fault rather than a prefix. A truncated thread loses the LAST comments
	// first, which is exactly where an answer is.
	ListIssueComments(ctx context.Context, repo RepoRef, number int) ([]ForgeComment, error)

	// SetIssueLabels replaces an issue's labels, verified by read-back.
	//
	// Labels are display output. This method exists so an operator watching the
	// forge sees what the module is doing; no dispatch decision reads them.
	//
	// REPLACES. Prefer AddIssueLabels and RemoveIssueLabel wherever the caller
	// owns only part of an issue's label set: a replacement computed from a
	// read a moment earlier destroys whatever arrived in between.
	SetIssueLabels(ctx context.Context, repo RepoRef, number int, labels []string) error

	// AddIssueLabels adds labels to an issue, leaving every other label alone,
	// verified by read-back.
	//
	// Adding an issue's existing label is not an error. Forges treat it as a
	// no-op and so does this: a reconciler that had to know the current set
	// before it could safely add would be back to read-then-write, which is
	// the race these methods exist to remove.
	AddIssueLabels(ctx context.Context, repo RepoRef, number int, labels []string) error

	// RemoveIssueLabel removes ONE label from an issue, leaving every other
	// label alone, verified by read-back.
	//
	// One at a time rather than a set, because that is what the forges expose
	// and because a partial failure across a set has no good reporting: the
	// caller would learn that something failed without learning what still
	// holds. Removing a label the issue does not carry is not an error, for the
	// reason adding an existing one is not.
	RemoveIssueLabel(ctx context.Context, repo RepoRef, number int, label string) error

	// GetTag returns one tag, or a NotFoundError when the repository has no
	// such tag.
	//
	// IT IS WHAT SEPARATES "MERGED" FROM "RELEASED". A dependency whose fix is
	// merged is not yet available to a consumer that pins a version: the
	// dependency-resolution convention resolves module versions from forge
	// TAGS, so until a tag names the merge commit the consumer's build still
	// selects the old code. Readiness therefore needs a published-artifact
	// signal that a merged pull request cannot supply, and the tag IS that
	// signal — it is the thing a version pin resolves against, so asking
	// whether it exists asks exactly the consumer's own question.
	//
	// An absent tag is a NotFoundError rather than a nil tag: "there is no
	// v1.2.3" and "the forge could not be reached" must not arrive as the same
	// answer, because one is a dependency still to be released and the other is
	// an outage that leaves every waiting task where it is.
	GetTag(ctx context.Context, repo RepoRef, tag string) (*ForgeTag, error)

	// CreateBranch creates a branch at fromRef and returns it, verified by
	// read-back.
	CreateBranch(ctx context.Context, repo RepoRef, name, fromRef string) (*ForgeBranch, error)

	// GetBranch returns one branch, or a NotFoundError.
	GetBranch(ctx context.Context, repo RepoRef, name string) (*ForgeBranch, error)

	// DeleteBranch removes a branch, verified by read-back.
	DeleteBranch(ctx context.Context, repo RepoRef, name string) error

	// CreatePullRequest opens a pull request and returns it, verified by
	// read-back.
	CreatePullRequest(ctx context.Context, repo RepoRef, spec PullRequestSpec) (*ForgePullRequest, error)

	// GetPullRequest returns one pull request, or a NotFoundError.
	GetPullRequest(ctx context.Context, repo RepoRef, number int) (*ForgePullRequest, error)

	// ListOpenPullRequests returns EVERY open pull request on the repository.
	//
	// IT EXISTS TO ANSWER "WHICH PULL REQUEST IS THIS COMMIT THE HEAD OF".
	// A pipeline delivery — a commit status, a check run, a workflow job —
	// names a COMMIT and not a pull request, on every forge the module
	// speaks to. So a build going green cannot start a review without this
	// lookup, and the review would instead have to be started before the
	// tests ran, which is the spend go-quicbaas#927 measured.
	//
	// WHOLE, AND A PREFIX IS A FAULT, for the reason every other listing here
	// says so: a capped page silently answers "no pull request has this head"
	// for a repository with more open pull requests than fit on it, and the
	// change whose CI just passed is then never reviewed. An adapter that
	// cannot deliver the whole listing reports a fault.
	//
	// OPEN ONLY, because a merged or closed pull request needs no verdict and
	// a listing that carried the repository's whole history would grow without
	// bound against a question that only concerns what is still in flight.
	ListOpenPullRequests(ctx context.Context, repo RepoRef) ([]ForgePullRequest, error)

	// MergePullRequest merges according to spec, verified by read-back.
	//
	// expectedHead is the tree the caller approved: the merge is refused when
	// the pull request's head has moved past it, so a commit pushed after a
	// review cannot land on that review's approval. Refused too when the
	// platform lacks spec.Method — landing a merge the wrong way is not a
	// degraded success.
	MergePullRequest(ctx context.Context, repo RepoRef, number int, spec MergeSpec, expectedHead string) error

	// ListReviews returns EVERY review on one pull request, oldest first.
	//
	// WHOLE, AND A PREFIX IS A FAULT. The gate reduces these to each
	// reviewer's operative verdict on the CURRENT head, and the current
	// head's verdicts are the NEWEST ones — the far end of the listing. A
	// capped first page therefore hides exactly the reviews that decide the
	// merge: the approval that would clear the gate goes missing and the
	// pull request waits for a verdict that was already given, or a
	// superseded rejection is the last thing anyone can see. An adapter that
	// cannot deliver the whole listing reports a fault rather than a prefix,
	// for the same reason ListIssueComments does.
	ListReviews(ctx context.Context, repo RepoRef, number int) ([]ForgeReview, error)

	// SubmitReview POSTS a verdict on one pull request and returns it as
	// stored, verified by read-back.
	//
	// THE WRITE HALF OF ListReviews, and the reason this module can review
	// its own work rather than only judge someone else's. The submission
	// binds to a head (ReviewSubmission.HeadSHA) so the gate can attribute
	// it, and it is refused with no body — see ReviewSubmission.Validate.
	//
	// Adapters whose platform reports SubmitReviews false return
	// ErrForgeUnsupported, never a nil error: a caller that believed its
	// verdict was posted would write a commit status the forge's own review
	// history contradicts, and nobody reading the pull request could tell
	// what had actually been decided.
	SubmitReview(ctx context.Context, repo RepoRef, number int, submission ReviewSubmission) (*ForgeReview, error)

	// SetCommitStatus writes a named check's state against one commit,
	// verified by read-back.
	//
	// IT IS WHAT MAKES A VERDICT ENFORCEABLE. The module's own gate decides
	// whether the module will merge; this is how the PLATFORM is told, so
	// branch protection can require the check and a rejected change cannot
	// land by any route — including the routes the module does not own.
	//
	// A status is written against a SHA rather than a pull request because
	// that is what the verdict is about, and because the two disagree the
	// moment a commit is pushed: a status addressed to the pull request
	// would silently re-colour the new head with the old head's answer,
	// which is failure 9 wearing a different hat.
	//
	// Adapters whose platform reports CommitStatuses false return
	// ErrForgeUnsupported, never nil.
	SetCommitStatus(ctx context.Context, repo RepoRef, sha string, status CommitStatus) error

	// ListPullRequestFiles returns EVERY file one pull request changes.
	//
	// WHOLE, AND A PREFIX IS A FAULT, for a sharper reason than the other
	// listings have: this one is read to tell a reviewer what to review. A
	// capped page is not a shorter answer to the question — it is a
	// confident answer to a different one, because a reviewer handed
	// thirty of a change's eighty files reviews thirty files and approves
	// the change. The forge's page size then silently decides how much of a
	// change gets read, and the larger the change the less of it is
	// examined.
	ListPullRequestFiles(ctx context.Context, repo RepoRef, number int) ([]ForgeChangedFile, error)

	// ListCheckRuns returns the CI runs against one commit.
	ListCheckRuns(ctx context.Context, repo RepoRef, sha string) ([]ForgeCheckRun, error)

	// CancelCheckRun stops one run.
	//
	// Adapters whose platform reports CancelCheckRuns false return
	// ErrForgeUnsupported. They do NOT return nil: a caller that believed a run
	// was cancelled would stop tracking capacity the run is still consuming.
	CancelCheckRun(ctx context.Context, repo RepoRef, id string) error

	// RerunCheckRun starts one FINISHED run again, verified by read-back.
	//
	// THE ANSWER TO A VOID RED. pkg/cifault classifies a failure that is not
	// about the change at all — a runner killed mid-job, a status write that
	// raced the runner's shutdown — and the engine's response is to ask the
	// machine again rather than wake a coding agent. This is that ask. The
	// CALLER bounds how many times it is made, because a permanently broken
	// host answers void every time and an unbounded loop would spend CI
	// forever without telling anybody.
	//
	// THE WHOLE ForgeCheckRun IS PASSED, for the reason CheckRunLogs takes
	// one: what identifies a rerunnable run differs by platform, and the
	// caller must not have to know which field this one reads.
	//
	// Adapters whose platform reports RerunCheckRuns false return
	// ErrForgeUnsupported, never nil: a caller told a run had been restarted
	// would wait out its whole review poll for a result that is never coming,
	// and the failure would surface as a job that hung rather than as a fleet
	// an operator needs to look at.
	RerunCheckRun(ctx context.Context, repo RepoRef, run ForgeCheckRun) error

	// CheckRunLogs returns the TAIL of one run's output, at most maxBytes of
	// it, for quoting to whoever has to fix the failure.
	//
	// THE WHOLE ForgeCheckRun IS PASSED, not its id, because what identifies a
	// log differs by platform and the caller must not have to know which field
	// this one reads. Gitea's check runs are commit statuses whose id addresses
	// no log at all — the Actions job is named only inside the status target
	// URL — while a platform with first-class check runs reads the id.
	//
	// THE TAIL RATHER THAN THE HEAD: a CI log opens with runner banners and
	// closes with the thing that failed. maxBytes is a bound on what the caller
	// receives AND on what the adapter retains, so a chatty job cannot cost the
	// sweep its memory.
	//
	// Adapters whose platform reports CheckRunLogs false return
	// ErrForgeUnsupported, never an empty string: a caller told a failing run
	// logged nothing would report that as the finding.
	CheckRunLogs(ctx context.Context, repo RepoRef, run ForgeCheckRun, maxBytes int) (string, error)

	// VerifyWebhook authenticates a delivery against the shared secret.
	//
	// Fail-closed and constant-time. An unverifiable delivery is discarded, not
	// processed on the strength of its contents.
	VerifyWebhook(payload []byte, signature string, secret []byte) error

	// ParseWebhook maps a verified delivery onto a ForgeEvent. Callers verify
	// FIRST; parsing an unverified payload is parsing whatever an attacker
	// sent.
	ParseWebhook(eventType string, payload []byte) (*ForgeEvent, error)

	// EnsureWebhook makes the repository's delivery match spec, creating or
	// updating as needed, verified by read-back.
	EnsureWebhook(ctx context.Context, repo RepoRef, spec WebhookSpec, secret []byte) error

	// ForgeOnboarding is the repository-level surface that takes a repository
	// from unmanaged to driven — labels, webhooks, tree listing and branch
	// protection. Embedded rather than kept apart so an adapter is still
	// complete or not registered at all; declared in forge_onboarding.go.
	ForgeOnboarding
}

// =============================================================================
// Registry
// =============================================================================

// ForgeFactory builds an adapter for ONE project's forge configuration.
//
// The registry holds factories rather than instances because a forge adapter is
// not process-wide: every project carries its own BaseURL and its own
// credential, so two projects may sit on different Gitea instances, or on the
// same one under different tokens. A single registered instance could serve
// only whichever project happened to configure it first, and the rest would
// silently talk to the wrong server.
//
// The token is a VALUE and never travels on ForgeConfig, which is a stored,
// logged, compared record. Callers resolve it from the SecretsService for the
// call and let it go.
//
// The transport policy is the NODE's (see ForgeTransport), passed alongside
// rather than stored on the binding: how long a forge may take is a property
// of the host it runs on, shared by every project on it.
type ForgeFactory func(cfg ForgeConfig, creds ForgeCredentials, transport ForgeTransport) (ForgeBackend, error)

// ForgeCredentials carries every credential an adapter needs, as VALUES.
//
// Grouped rather than passed positionally because credentials accrete: the
// workflow token came first, token custody added a password-equivalent, and a
// positional signature would have to change again for the next one. None of
// these travel on ForgeConfig, which is a stored, logged, compared record —
// callers resolve them from the SecretsService for the call and let them go.
//
// This mirrors the GitCredentials factoring on the git side: same problem,
// forge half.
type ForgeCredentials struct {
	// Token authenticates workflow calls. Required.
	Token string

	// Custody is the password-equivalent used to mint and revoke tokens.
	//
	// OPTIONAL. A zero value builds a workflow-only adapter whose custody
	// methods refuse loudly, which is the right shape for the many projects
	// that never rotate: they change one signature and nothing else.
	Custody CustodyCredential
}

var (
	forgeMu     sync.RWMutex
	forgeByType = map[ForgeType]ForgeFactory{}
)

// RegisterForgeFactory adds a COMPLETE adapter's constructor under its platform
// type.
//
// Panics on a nil or duplicate registration, both of which are wiring errors
// that must fail at init rather than at the first dispatch: an adapter silently
// replaced by another would route a project's work to the wrong platform.
func RegisterForgeFactory(t ForgeType, f ForgeFactory) {
	if f == nil {
		panic("sdlc: nil forge factory registered")
	}
	if t == "" {
		panic("sdlc: forge factory registered under an empty type")
	}
	forgeMu.Lock()
	defer forgeMu.Unlock()
	if _, dup := forgeByType[t]; dup {
		panic("sdlc: duplicate forge backend registered: " + string(t))
	}
	forgeByType[t] = f
}

// NewForgeBackend builds the adapter for one project's forge.
//
// Fail-closed: a type nothing implements is an error, never a default. A
// project pointed at a forge no adapter serves must fail registration rather
// than run against whichever adapter happened to be registered.
//
// A zero transport takes DefaultForgeTransport.
func NewForgeBackend(cfg ForgeConfig, creds ForgeCredentials, transport ForgeTransport) (ForgeBackend, error) {
	forgeMu.RLock()
	factory, ok := forgeByType[cfg.Type]
	forgeMu.RUnlock()
	if !ok {
		return nil, NewForgeNotRegisteredError(cfg.Type)
	}
	return factory(cfg, creds, transport)
}

// RegisteredForgeTypes returns the registered platform types, for diagnostics
// and for an operator asking what this build can talk to.
func RegisteredForgeTypes() []ForgeType {
	forgeMu.RLock()
	defer forgeMu.RUnlock()
	types := make([]ForgeType, 0, len(forgeByType))
	for t := range forgeByType {
		types = append(types, t)
	}
	sort.Slice(types, func(i, j int) bool { return types[i] < types[j] })
	return types
}

// NewForgeNotRegisteredError refuses a forge type with no adapter, naming the
// types this build DOES carry.
//
// Every ForgeType is selectable on a binding — the choice is real, and an
// operator who picks one this build cannot talk to has to be told which ones
// it can, not merely that theirs failed.
func NewForgeNotRegisteredError(forge ForgeType) *ForgeNotRegisteredError {
	return &ForgeNotRegisteredError{Type: forge, Available: RegisteredForgeTypes()}
}
