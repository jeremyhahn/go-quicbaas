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
	"sort"
	"strconv"
	"strings"
	"sync"
)

// =============================================================================
// Strategy — one SDLC, one seam
// =============================================================================
//
// An SDLC is ONE decision. GitHub Flow branches off the default branch and
// squashes; GitFlow branches a feature off develop and lands a merge commit,
// and a hotfix off the default branch with a back-merge behind it. Splitting
// branching from merging would let a project select GitFlow branching with a
// squash merge — a combination no SDLC describes, and one that silently
// discards the merge commit GitFlow's history depends on. So a project names
// a strategy, and that strategy owns the branch, its base, its target, whether
// a pull request is required, how the pull request lands and what happens
// after it lands.
//
// Two rules make the seam real rather than aspirational:
//
//   - ONLY COMPLETE STRATEGIES ARE REGISTERED. A half-built strategy is not
//     registered under a name and left to return "not implemented" — a caller
//     that can select it can be broken by it. This is the same completeness
//     rule the forge adapters follow, and it exists to avoid the
//     framework-with-no-working-implementations shape.
//   - SELECTION IS FAIL-CLOSED. A project naming a strategy no registry knows
//     fails registration with a typed error naming what IS available, rather
//     than falling back to a default. A project silently run under the wrong
//     strategy pushes to the wrong ref and lands history the wrong way.
//     Neither is recoverable by retrying.

// IssueRef is the subset of an issue a strategy needs to plan its work.
//
// It is a narrow value rather than the Task entity so a strategy cannot reach
// for state it has no business reading — a strategy that could see the attempt
// budget would eventually branch differently on it.
type IssueRef struct {
	// Number is the forge issue number the work mirrors.
	Number int

	// Title is the issue title, used to derive a human-readable slug.
	Title string

	// Labels are the forge labels on the issue, as the forge spells them.
	// Strategies route on them — a change class that skips review, a hotfix
	// that cuts from a different base — and compare them case-insensitively.
	Labels []string

	// Milestone is the issue's milestone title, empty when it has none.
	// A strategy with release branches derives the release from it.
	Milestone string
}

// HasLabel reports whether the issue carries label, compared without regard
// to case or surrounding space.
//
// Case-insensitive because a change class is a human-maintained label on a
// forge that lets people type it: refusing to recognise "Docs" because the
// strategy was configured with "docs" would send a docs change through a
// review nobody asked for, silently.
func (r IssueRef) HasLabel(label string) bool {
	want := strings.ToLower(strings.TrimSpace(label))
	if want == "" {
		return false
	}
	for _, have := range r.Labels {
		if strings.ToLower(strings.TrimSpace(have)) == want {
			return true
		}
	}
	return false
}

// HasAnyLabel reports whether the issue carries any of labels.
func (r IssueRef) HasAnyLabel(labels []string) bool {
	for _, label := range labels {
		if r.HasLabel(label) {
			return true
		}
	}
	return false
}

// BranchPlan is what a Strategy decides for one issue.
//
// Base and Target are independent: GitFlow cuts a hotfix from the default
// branch and targets it there, then back-merges into develop, while a feature
// cuts from develop and targets develop. An empty Base or Target means THE
// PROJECT'S DEFAULT BRANCH — the project supplies the default, the strategy
// reads it, and nothing downstream overwrites what a strategy did decide.
// Resolve turns a plan into the refs the git and forge calls use.
type BranchPlan struct {
	// Branch is the ref name to create and push.
	Branch string

	// Base is the ref the branch is cut from. Empty means the project's
	// default branch.
	Base string

	// Target is the ref a pull request targets. Empty means the project's
	// default branch.
	Target string

	// RequiresPullRequest reports whether the change must land through a
	// reviewed pull request. False is the carve-out path (docs, CI, release
	// commits in some strategies) and is never the default.
	RequiresPullRequest bool

	// DeleteAfterMerge reports whether the branch is removed once landed.
	DeleteAfterMerge bool
}

// FallbackDefaultBranch is the default branch assumed for a project that
// names none. It is a last resort, not a policy: a project that cares names
// its default branch.
const FallbackDefaultBranch = "main"

// Resolve fills the plan's Base and Target from the project's default branch,
// leaving whatever the strategy decided untouched.
func (p BranchPlan) Resolve(projectDefaultBranch string) BranchPlan {
	fallback := strings.TrimSpace(projectDefaultBranch)
	if fallback == "" {
		fallback = FallbackDefaultBranch
	}
	if strings.TrimSpace(p.Base) == "" {
		p.Base = fallback
	}
	if strings.TrimSpace(p.Target) == "" {
		p.Target = fallback
	}
	return p
}

// Merge methods are the forge-neutral names a strategy asks to land by; each
// forge adapter maps them onto its own and refuses what it cannot do.
const (
	// MergeMethodSquash lands the pull request as a single commit.
	MergeMethodSquash = "squash"

	// MergeMethodMerge lands the pull request as a merge commit, keeping
	// the branch's history and the merge point.
	MergeMethodMerge = "merge"
)

// MergeSpec is what a Strategy decides for one approved pull request.
type MergeSpec struct {
	// Method is the forge merge method to request.
	Method string

	// CommitTitle is the title for the resulting commit.
	CommitTitle string

	// CommitMessage is the body, carrying the issue linkage.
	CommitMessage string

	// ExpectSourceDeleted asks the forge to delete the source branch and is
	// what the reconciler verifies afterwards.
	//
	// A strategy does NOT set it: branch lifecycle is BranchPlan's
	// DeleteAfterMerge, decided once when the branch was cut, and the caller
	// copies it here. Deciding it twice is how the two halves drift.
	ExpectSourceDeleted bool
}

// PullRequestRef is the subset of a pull request a strategy needs to land it.
type PullRequestRef struct {
	// Number is the forge pull-request number.
	Number int

	// Title is the pull request title.
	Title string

	// IssueNumber is the forge issue this closes. Zero means the pull
	// request closes no issue, which is legal for carve-out changes.
	IssueNumber int

	// SourceBranch is the branch being merged.
	SourceBranch string
}

// MergedRef is the subset of a landed pull request a strategy needs to decide
// what follows it.
type MergedRef struct {
	// Number is the forge pull-request number that landed.
	Number int

	// Title is the pull request title.
	Title string

	// IssueNumber is the forge issue it closed, zero when it closed none.
	IssueNumber int

	// SourceBranch is the branch that was merged.
	SourceBranch string

	// TargetBranch is the branch it landed on — the resolved target, not
	// the plan's possibly-empty one.
	TargetBranch string

	// Labels are the forge labels on the issue, so a strategy routes the
	// same way after the merge as it did before it.
	Labels []string
}

// HasLabel reports whether the merged work carries label, compared without
// regard to case or surrounding space.
func (r MergedRef) HasLabel(label string) bool {
	return IssueRef{Labels: r.Labels}.HasLabel(label)
}

// StepKind names what a post-merge step does.
type StepKind string

// StepBackMerge merges one branch into another so a fix landed on a release
// line is not lost from the development line.
const StepBackMerge StepKind = "back_merge"

// Step is one action an SDLC performs after a pull request has landed.
//
// A step is DECLARED here and EXECUTED by the runner through the forge
// adapter, so a strategy never holds a forge client and a step is a value a
// test can assert on.
type Step struct {
	// Kind selects what the step does.
	Kind StepKind

	// Source is the ref the step reads from.
	Source string

	// Target is the ref the step writes to.
	Target string

	// Title is the subject of the pull request or commit the step opens.
	Title string

	// Body is its description.
	Body string
}

// Strategy owns one SDLC end to end: how a branch is named and where it is
// cut from, whether the change is reviewed, how it lands, and what follows.
type Strategy interface {
	// Name is the registry key a project selects this strategy by.
	Name() string

	// PlanBranch decides the branch, its base and target, and whether the
	// change must go through a pull request.
	PlanBranch(issue IssueRef) (BranchPlan, error)

	// PlanMerge composes the merge for a pull request that has already
	// satisfied its review policy. It decides HOW to land, never WHETHER —
	// the gate is the reviewer's and the reconciler's.
	PlanMerge(pr PullRequestRef) (MergeSpec, error)

	// PostMerge lists what must happen after the pull request lands. An
	// empty slice is the common answer and is not an error.
	PostMerge(merged MergedRef) ([]Step, error)
}

// =============================================================================
// Registry
// =============================================================================

// strategyByName holds the registered strategies.
//
// Map-based dispatch with a mutex rather than a switch: strategies are
// registered at startup by whichever build includes them, and a switch would
// have to name every one at compile time — which is the coupling the seam
// exists to remove.
var (
	registryMu     sync.RWMutex
	strategyByName = map[string]Strategy{}
)

// RegisterStrategy adds a complete SDLC strategy under its name.
//
// It panics on a duplicate name or a nil strategy because both are programming
// errors discovered at init: two strategies answering to one name means a
// project's selection is decided by registration order, which is not a
// behaviour to recover from at runtime.
func RegisterStrategy(s Strategy) {
	if s == nil {
		panic("sdlc: nil strategy registered")
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, dup := strategyByName[s.Name()]; dup {
		panic("sdlc: duplicate strategy registered: " + s.Name())
	}
	strategyByName[s.Name()] = s
}

// LookupStrategy resolves a strategy by name.
//
// An unknown name is a typed error naming what is available, never a default:
// a project silently run under a strategy it did not choose pushes to refs its
// operators do not expect and lands history the wrong way.
func LookupStrategy(name string) (Strategy, error) {
	registryMu.RLock()
	s, ok := strategyByName[name]
	registryMu.RUnlock()
	if !ok {
		return nil, &StrategyNotRegisteredError{Name: name, Available: RegisteredStrategies()}
	}
	return s, nil
}

// RegisteredStrategies lists the registered names, sorted.
func RegisteredStrategies() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	names := make([]string, 0, len(strategyByName))
	for name := range strategyByName {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// =============================================================================
// github-flow
// =============================================================================

// GitHubFlowName is the registry key for the github-flow strategy.
const GitHubFlowName = "github-flow"

// gitHubFlow implements GitHub Flow: short-lived issue branches cut from the
// project's default branch, landed as one squashed commit through a reviewed
// pull request, deleted after merge.
//
// This is the company's own strategy (handbook conventions/sdlc.md §2).
type gitHubFlow struct{}

// Name implements Strategy.
func (gitHubFlow) Name() string { return GitHubFlowName }

// PlanBranch implements Strategy.
//
// Base and Target are left empty, which means the project's default branch:
// GitHub Flow has exactly one long-lived branch and does not name it, the
// project does.
//
// Every change goes through a pull request. The docs/CI carve-out that
// conventions/sdlc.md permits belongs to trunk-based, which is the strategy
// that has one — encoding it here would make github-flow two strategies
// wearing one name.
func (gitHubFlow) PlanBranch(issue IssueRef) (BranchPlan, error) {
	branch, err := issueBranch("issue/", issue)
	if err != nil {
		return BranchPlan{}, err
	}
	return BranchPlan{
		Branch:              branch,
		RequiresPullRequest: true,
		DeleteAfterMerge:    true,
	}, nil
}

// PlanMerge implements Strategy: one squashed commit per issue.
func (gitHubFlow) PlanMerge(pr PullRequestRef) (MergeSpec, error) {
	return squashSpec(pr)
}

// PostMerge implements Strategy: GitHub Flow has one long-lived branch, so a
// landed pull request leaves nothing to reconcile.
func (gitHubFlow) PostMerge(MergedRef) ([]Step, error) { return nil, nil }

// =============================================================================
// trunk-based
// =============================================================================

// TrunkBasedName is the registry key for the trunk-based strategy.
const TrunkBasedName = "trunk-based"

// DefaultDirectPushLabels are the change classes trunk-based lands without a
// pull request, per handbook conventions/sdlc.md §2: documentation and CI
// configuration, which carry no runtime risk and whose review round costs more
// than it catches.
var DefaultDirectPushLabels = []string{"docs", "ci"}

// trunkBased implements trunk-based development: everything lands on the
// project's default branch, through a reviewed pull request, EXCEPT the change
// classes the project carves out — those are pushed straight to the trunk.
//
// The carve-out is a set of forge labels rather than a guess at the content of
// the change: a strategy that decided for itself which diffs were "just docs"
// would eventually skip review on something that was not.
type trunkBased struct {
	// directPush are the labels whose changes bypass the pull request.
	directPush []string
}

// NewTrunkBased builds a trunk-based strategy whose carve-out is the given
// change-class labels. With none, DefaultDirectPushLabels apply; pass an
// explicitly empty, non-nil slice for a trunk with no carve-out at all.
//
// It is a constructor rather than a package-level knob so a build that wants a
// different carve-out registers its own named strategy instead of mutating the
// one other projects already selected.
func NewTrunkBased(directPushLabels ...string) Strategy {
	if directPushLabels == nil {
		directPushLabels = DefaultDirectPushLabels
	}
	labels := make([]string, 0, len(directPushLabels))
	for _, label := range directPushLabels {
		if trimmed := strings.TrimSpace(label); trimmed != "" {
			labels = append(labels, trimmed)
		}
	}
	return trunkBased{directPush: labels}
}

// Name implements Strategy.
func (trunkBased) Name() string { return TrunkBasedName }

// PlanBranch implements Strategy.
//
// A carved-out change still gets a branch: the worker commits to it and the
// runner pushes it to the target, so the work is reviewable in history and
// recoverable if the push is refused. What the carve-out removes is the
// pull request and its review round, not the branch.
func (s trunkBased) PlanBranch(issue IssueRef) (BranchPlan, error) {
	branch, err := issueBranch("issue/", issue)
	if err != nil {
		return BranchPlan{}, err
	}
	return BranchPlan{
		Branch:              branch,
		RequiresPullRequest: !issue.HasAnyLabel(s.directPush),
		DeleteAfterMerge:    true,
	}, nil
}

// PlanMerge implements Strategy: one squashed commit per issue, as
// github-flow. A carved-out change never reaches this — it was pushed to the
// trunk and has no pull request to land.
func (trunkBased) PlanMerge(pr PullRequestRef) (MergeSpec, error) {
	return squashSpec(pr)
}

// PostMerge implements Strategy: the trunk is the only long-lived branch.
func (trunkBased) PostMerge(MergedRef) ([]Step, error) { return nil, nil }

// =============================================================================
// gitflow
// =============================================================================

// GitFlowName is the registry key for the gitflow strategy.
const GitFlowName = "gitflow"

// GitFlowDevelopBranch is the integration branch GitFlow cuts features from
// and lands them on. It is GitFlow's own convention, not the project's default
// branch, which is the release line.
const GitFlowDevelopBranch = "develop"

// GitFlowHotfixLabel marks an issue that must be fixed on the release line
// rather than waiting for the next release off develop.
const GitFlowHotfixLabel = "hotfix"

// gitFlow implements the two GitFlow flows this build ships:
//
//   - feature — feature/<n>-<slug> cut from develop, landed on develop as a
//     MERGE COMMIT. GitFlow's history is its record of which features landed
//     together; a squash would erase it.
//   - hotfix — hotfix/<n>-<slug> cut from the project's default branch (the
//     release line), landed there, then BACK-MERGED into develop so the next
//     release does not regress the fix.
//
// Release branches — cutting release/<version> off develop, tagging the
// release merge and the double back-merge behind it — are not implemented and
// are not registered as half of one: see issue #597.
type gitFlow struct{}

// Name implements Strategy.
func (gitFlow) Name() string { return GitFlowName }

// PlanBranch implements Strategy.
func (gitFlow) PlanBranch(issue IssueRef) (BranchPlan, error) {
	if issue.HasLabel(GitFlowHotfixLabel) {
		branch, err := issueBranch("hotfix/", issue)
		if err != nil {
			return BranchPlan{}, err
		}
		// Base and Target empty: the release line is the project's default
		// branch, and only the project knows what it is called.
		return BranchPlan{
			Branch:              branch,
			RequiresPullRequest: true,
			DeleteAfterMerge:    true,
		}, nil
	}
	branch, err := issueBranch("feature/", issue)
	if err != nil {
		return BranchPlan{}, err
	}
	return BranchPlan{
		Branch:              branch,
		Base:                GitFlowDevelopBranch,
		Target:              GitFlowDevelopBranch,
		RequiresPullRequest: true,
		DeleteAfterMerge:    true,
	}, nil
}

// PlanMerge implements Strategy: a merge commit, so the branch's history and
// the point it joined survive.
func (gitFlow) PlanMerge(pr PullRequestRef) (MergeSpec, error) {
	if err := checkPullRequest(pr); err != nil {
		return MergeSpec{}, err
	}
	spec := MergeSpec{
		Method:      MergeMethodMerge,
		CommitTitle: "Merge " + pr.SourceBranch + " (#" + strconv.Itoa(pr.Number) + ")",
	}
	if pr.IssueNumber > 0 {
		spec.CommitMessage = closesLine(pr.IssueNumber)
	}
	return spec, nil
}

// PostMerge implements Strategy: a hotfix that landed on the release line is
// back-merged into develop, or the next release rebuilds the bug.
func (gitFlow) PostMerge(merged MergedRef) ([]Step, error) {
	if !merged.HasLabel(GitFlowHotfixLabel) {
		return nil, nil
	}
	target := strings.TrimSpace(merged.TargetBranch)
	if target == "" {
		return nil, &InvalidInputError{
			Field:   "merged.target_branch",
			Message: "a gitflow hotfix back-merge needs the release branch it landed on",
		}
	}
	if target == GitFlowDevelopBranch {
		// The hotfix landed on develop already; there is nothing to carry.
		return nil, nil
	}
	return []Step{{
		Kind:   StepBackMerge,
		Source: target,
		Target: GitFlowDevelopBranch,
		Title:  "Back-merge " + target + " into " + GitFlowDevelopBranch + " after hotfix #" + strconv.Itoa(merged.Number),
		Body: "Carries the hotfix landed by #" + strconv.Itoa(merged.Number) + " from `" + target +
			"` into `" + GitFlowDevelopBranch + "` so the next release keeps it.\n",
	}}, nil
}

// =============================================================================
// Shared planning
// =============================================================================

// issueBranch names a branch after the issue it closes, under prefix.
func issueBranch(prefix string, issue IssueRef) (string, error) {
	if issue.Number <= 0 {
		return "", &InvalidInputError{
			Field:   "issue_number",
			Message: "an sdlc branch is named after the issue it closes",
		}
	}
	return prefix + strconv.Itoa(issue.Number) + "-" + Slugify(issue.Title), nil
}

// issueNumberFromBranch reads back the issue number issueBranch wrote into a
// branch name, and false for a branch this module did not name.
//
// IT IS PARSING THIS MODULE'S OWN OUTPUT, not guessing at a convention. The
// delivery path names every branch "issue/<number>-<slug>" a few lines above,
// so the number is recoverable from the one field a pull request always
// carries. That matters because the REVIEWER needs the issue a change claims
// to close, and nothing else on a ForgePullRequest names it: a body may say
// "closes #12" or may say nothing, and neither is this module's word.
func issueNumberFromBranch(prefix, ref string) (int, bool) {
	rest, found := strings.CutPrefix(strings.TrimSpace(ref), prefix)
	if !found {
		return 0, false
	}
	digits, _, _ := strings.Cut(rest, "-")
	number, err := strconv.Atoi(digits)
	if err != nil || number <= 0 {
		return 0, false
	}
	return number, true
}

// checkPullRequest refuses a pull request a merge could not be composed for.
func checkPullRequest(pr PullRequestRef) error {
	if pr.Number <= 0 {
		return &InvalidInputError{
			Field:   "pull_request_number",
			Message: "a merge needs the pull request it lands",
		}
	}
	if strings.TrimSpace(pr.Title) == "" {
		return &InvalidInputError{
			Field:   "pull_request_title",
			Message: "a merge commit needs a subject",
		}
	}
	return nil
}

// squashSpec is the one-commit-per-issue merge github-flow and trunk-based
// share.
//
// The commit message carries "Closes #n" so the forge closes the issue on
// merge, which is what keeps the issue's forge mirror and the module's own
// record from drifting apart. A pull request that closes no issue is legal and
// simply carries no linkage line.
func squashSpec(pr PullRequestRef) (MergeSpec, error) {
	if err := checkPullRequest(pr); err != nil {
		return MergeSpec{}, err
	}
	spec := MergeSpec{
		Method:      MergeMethodSquash,
		CommitTitle: pr.Title + " (#" + strconv.Itoa(pr.Number) + ")",
	}
	if pr.IssueNumber > 0 {
		spec.CommitMessage = closesLine(pr.IssueNumber)
	}
	return spec, nil
}

// closesLine is the forge's issue-closing keyword for an issue number.
func closesLine(issue int) string { return "Closes #" + strconv.Itoa(issue) }

// =============================================================================
// Slug derivation
// =============================================================================

// slugUnsafe matches every run of characters that may not appear in a slug.
var slugUnsafe = regexp.MustCompile(`[^a-z0-9]+`)

// maxSlugLen bounds the slug so a long issue title cannot produce a branch name
// the forge or the filesystem refuses.
const maxSlugLen = 48

// Slugify derives a branch-safe slug from an issue title.
//
// The result contains only lowercase alphanumerics and single hyphens, so it is
// safe in a ref name, in a path, and in a URL without escaping. A title that
// slugifies to nothing — punctuation only, or a non-Latin script — yields
// "untitled" rather than an empty segment, because an empty slug would produce
// a branch ending in a bare hyphen.
func Slugify(title string) string {
	slug := slugUnsafe.ReplaceAllString(strings.ToLower(title), "-")
	slug = strings.Trim(slug, "-")
	if slug == "" {
		return "untitled"
	}
	if len(slug) > maxSlugLen {
		slug = strings.Trim(slug[:maxSlugLen], "-")
	}
	if slug == "" {
		return "untitled"
	}
	return slug
}

// init registers the strategies this build ships. Only complete ones appear.
func init() {
	RegisterStrategy(gitHubFlow{})
	RegisterStrategy(NewTrunkBased())
	RegisterStrategy(gitFlow{})
}
