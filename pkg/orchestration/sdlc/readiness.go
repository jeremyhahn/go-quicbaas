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
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// Readiness — more than one predicate
// =============================================================================
//
// ELIGIBILITY USED TO BE ONE TEST: the issue is open and carries the binding's
// trigger label. Three consequences, all of which cost real money on
// 2026-09-12:
//
//   - THERE WAS NO BLOCKED-ON CONCEPT, so the owner's cross-project ruling
//     ("work in another repo only when blocked, and own that issue until it
//     merges") had nothing to attach to. An issue that cannot be started until
//     another one lands was handed to an agent that discovered that for itself,
//     with tokens.
//   - AN ISSUE COULD BE ADOPTED WHILE A LIVE SESSION ALREADY OWNED IT. Two
//     projects bound to one repository, or a task removed while its job still
//     ran, and two agents author two branches for one issue.
//   - AN ISSUE WITH NO ACCEPTANCE CRITERIA was handed to an agent that then had
//     to guess what "done" meant, and the guess is the expensive part: it is
//     discovered at review, after the work.
//
// So readiness is five predicates, evaluated in a fixed order and reported as
// ONE verdict per issue. The order is by cost ascending — the free local tests
// first, the forge round trips last — so an issue refused for a reason readable
// from its own text costs no network at all.
//
// PARKING, AND WHY IT IS THE TASK'S PARK AND NOT A NEW ONE. An upstream wait is
// unbounded: nobody can say when another repository's issue will close. A
// readiness gate that held a session while waiting would hold it for days. The
// engine already has exactly the right primitive — a task parked with a typed
// UnblockCondition, re-examined by UnblockSweeper on every pass and readmitted
// through the engine's own announcing verb when the condition clears — so a
// dependency-blocked issue is adopted AS A PARKED TASK and resumes from there.
// It holds no session, no container and no lease while it waits, and it is
// visible in the queue with a stated reason instead of being invisible at the
// forge.
//
// WHY ADOPT AT ALL RATHER THAN SKIP. Skipping is cheaper and worse: nothing
// records that the issue was seen, nothing tells the person watching it why it
// is not moving, and every sweep re-derives the same answer from the same forge
// reads forever. A parked task is one record, one comment on the issue, and one
// existing sweep that already knows how to clear it.

// ReadinessPolicy is what an issue must satisfy before it becomes a Task.
//
// THE ZERO VALUE IS THE SHIPPED DEFAULT, and it enables every predicate that
// cannot refuse work wrongly. Dependency satisfaction and live-session ownership
// are facts, so they are on: an issue that declares no dependency is unaffected
// by the first, and one nothing is working on is unaffected by the second.
// Acceptance criteria are a JUDGEMENT about an issue's text, so that predicate
// is off unless an operator turns it on — a project whose issues do not use the
// heading convention would otherwise have every issue refused, which is a worse
// failure than an under-specified issue.
type ReadinessPolicy struct {
	// RequireAcceptanceCriteria refuses an issue whose text carries none of
	// AcceptanceHeadings.
	RequireAcceptanceCriteria bool `json:"require_acceptance_criteria,omitempty"`

	// AcceptanceHeadings are the section headings that count as acceptance
	// criteria, matched case-insensitively against a markdown heading or a
	// bold line. Empty takes DefaultAcceptanceHeadings.
	AcceptanceHeadings []string `json:"acceptance_headings,omitempty"`

	// SkipDependencies stops declared dependencies being evaluated at all.
	//
	// PHRASED AS AN OPT-OUT so the zero value evaluates them. An operator who
	// sets this is saying "this project's issues do not use the directives",
	// and the honest cost of that is stated rather than defaulted into: work
	// starts before what it consumes exists.
	SkipDependencies bool `json:"skip_dependencies,omitempty"`

	// MaxDependencyDepth bounds how far the dependency walk follows one chain
	// of issues. Zero takes DefaultMaxDependencyDepth; see the walk for why a
	// bound is required rather than advisory.
	MaxDependencyDepth int `json:"max_dependency_depth,omitempty"`
}

// DefaultAcceptanceHeadings are the section headings this tree's own issues use
// to state what "done" means.
//
// SEVERAL SPELLINGS, ONE MEANING. The convention grew rather than being
// declared, so the headings in use differ between repositories; refusing all but
// one of them would refuse issues that do state their criteria.
var DefaultAcceptanceHeadings = []string{
	"acceptance",
	"acceptance criteria",
	"definition of done",
	"verification",
}

// DefaultMaxDependencyDepth bounds the dependency walk.
//
// EIGHT, and the number is a judgement rather than a measurement: a chain longer
// than this is a decomposition problem rather than a dependency, and every extra
// level is a forge round trip per sweep. What matters is that a bound EXISTS —
// see walkDependencies.
const DefaultMaxDependencyDepth = 8

// Headings returns the acceptance headings in force, lower-cased.
func (p ReadinessPolicy) Headings() []string {
	source := p.AcceptanceHeadings
	if len(source) == 0 {
		source = DefaultAcceptanceHeadings
	}
	out := make([]string, 0, len(source))
	for _, heading := range source {
		trimmed := strings.ToLower(strings.TrimSpace(heading))
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	return out
}

// Depth returns the dependency depth bound in force.
func (p ReadinessPolicy) Depth() int {
	if p.MaxDependencyDepth <= 0 {
		return DefaultMaxDependencyDepth
	}
	return p.MaxDependencyDepth
}

// Validate refuses a policy that cannot mean what it appears to.
func (p ReadinessPolicy) Validate() error {
	if p.MaxDependencyDepth < 0 {
		return &InvalidInputError{
			Field:   "readiness.max_dependency_depth",
			Message: "must not be negative; zero takes the default",
		}
	}
	for i, heading := range p.AcceptanceHeadings {
		if strings.TrimSpace(heading) == "" {
			return &InvalidInputError{
				Field:   "readiness.acceptance_headings[" + strconv.Itoa(i) + "]",
				Message: "must not be blank; a blank heading matches every line and would pass every issue",
			}
		}
	}
	if p.RequireAcceptanceCriteria && len(p.AcceptanceHeadings) == 0 {
		// Not an error: the default headings apply. Stated here so the reader
		// of this function knows the combination was considered.
		return nil
	}
	return nil
}

// =============================================================================
// Verdicts
// =============================================================================

// ReadinessVerdict is what readiness decided about one issue.
type ReadinessVerdict string

const (
	// ReadinessAdopt means every predicate passed: the issue becomes a
	// runnable Task.
	ReadinessAdopt ReadinessVerdict = "adopt"

	// ReadinessNotTriggered means the issue is closed, or does not carry the
	// binding's trigger label. Not offered, and not reported as a problem:
	// this is the ordinary state of most of a repository's issues.
	ReadinessNotTriggered ReadinessVerdict = "not_triggered"

	// ReadinessHeld means the issue carries the portfolio hold (HoldLabel).
	// Not offered, whatever its other labels say: a trigger label beside a
	// hold is a contradiction only the party named on the issue resolves, and
	// the hold is the side that must win it.
	ReadinessHeld ReadinessVerdict = "held"

	// ReadinessOwned means a live session already holds this issue, so
	// adopting it would put two agents on one piece of work.
	ReadinessOwned ReadinessVerdict = "owned"

	// ReadinessClosedByPullRequest means an open pull request already closes
	// the issue. Adopting it would put a second change under review for one
	// piece of work (go-quicbaas#1054). Not adopted, and offered again once
	// the pull request is no longer open, merged or not.
	ReadinessClosedByPullRequest ReadinessVerdict = "closed_by_pull_request"

	// ReadinessNoAcceptanceCriteria means the issue states no criteria and the
	// policy requires them. A person must say what "done" is.
	ReadinessNoAcceptanceCriteria ReadinessVerdict = "no_acceptance_criteria"

	// ReadinessPark means a declared dependency is unsatisfied: the issue
	// becomes a PARKED task carrying the condition, which UnblockSweeper
	// clears. See the note at the top of this file.
	ReadinessPark ReadinessVerdict = "park"

	// ReadinessRefuse means the issue cannot be worked as declared and no
	// sweep can change that — a dependency cycle, a depth bound exceeded, a
	// malformed declaration. It becomes a parked task awaiting a person,
	// never a running one, and never a retry.
	ReadinessRefuse ReadinessVerdict = "refuse"

	// ReadinessUndecidable means a predicate could not be evaluated — the
	// forge failed, a store could not be read. Nothing is adopted and nothing
	// is parked: the next sweep asks again.
	ReadinessUndecidable ReadinessVerdict = "undecidable"
)

// String returns the verdict's name.
func (v ReadinessVerdict) String() string { return string(v) }

// Adoptable reports whether the verdict produces a task at all, running or
// parked.
func (v ReadinessVerdict) Adoptable() bool {
	return v == ReadinessAdopt || v == ReadinessPark || v == ReadinessRefuse
}

// ReadinessFinding is one issue's whole readiness answer.
type ReadinessFinding struct {
	// IssueNumber is the issue judged.
	IssueNumber int

	// Verdict is what readiness decided.
	Verdict ReadinessVerdict

	// Condition is the typed park condition, set for ReadinessPark. It is what
	// the parked task carries and what UnblockSweeper evaluates.
	Condition UnblockCondition

	// Dependencies are every dependency the issue declared, satisfied or not,
	// in declaration order. Carried on every verdict so an operator asking
	// "what is this waiting for" gets the whole list rather than the first
	// unsatisfied one.
	Dependencies []Dependency

	// Unsatisfied are the dependencies that are not yet met, in declaration
	// order. Condition is the first of these.
	Unsatisfied []Dependency

	// Cycle names the dependency cycle, for a ReadinessRefuse that found one,
	// as the chain of issue references that closes on itself.
	Cycle []string

	// Claim is the pull request that already closes the issue, for a
	// ReadinessClosedByPullRequest.
	Claim ClosingClaim

	// Evidence is the operator-facing reason for the verdict.
	Evidence string
}

// ParkKind is the block kind a parked adoption carries.
//
// A DEPENDENCY WAIT IS TaskBlockKindBlocked — "the task waits on an external
// condition", which is exactly what it does. A refusal is TaskBlockKindNeedsInfo:
// a cycle or a malformed declaration needs a person to change the issue, and
// nothing external will ever clear it, so classifying it as a wait would leave
// the sweep re-checking a condition that cannot become true.
func (f ReadinessFinding) ParkKind() orchestration.TaskBlockKind {
	if f.Verdict == ReadinessRefuse {
		return orchestration.TaskBlockKindNeedsInfo
	}
	return orchestration.TaskBlockKindBlocked
}

// ParkReason renders the reason the parked task carries: the condition's
// sentence, the evidence, and the machine-readable marker when there is a
// condition to encode.
//
// A REFUSAL CARRIES NO CONDITION, DELIBERATELY. A cycle has nothing a check
// could decide, and manufacturing a condition for it would have UnblockSweeper
// evaluate something invented to satisfy a format — the fail-open failure in a
// triage costume that readmission.go refuses by name. So a refusal parks with
// prose, which the sweep reports as awaiting a human diagnosis on every pass.
func (f ReadinessFinding) ParkReason() (string, error) {
	if f.Verdict != ReadinessPark {
		return f.Evidence, nil
	}
	return ComposeBlockedReason(f.Condition, f.Evidence)
}

// =============================================================================
// Live ownership
// =============================================================================

// LiveWorkReader reports which work references the fleet is currently running.
//
// KEYED ON THE REFERENCE RATHER THAN ON THE TASK, because the question is about
// the ISSUE and two projects bound to one repository produce two different tasks
// for it. A job's lease already names the task's source reference — the issue
// reference this source emits — so the answer is a read of records that already
// exist, with no second bookkeeping surface to drift from them.
type LiveWorkReader interface {
	// LiveRefs returns the work references held by a live lease, fleet-wide.
	// An empty tenant means every tenant.
	LiveRefs(ctx context.Context, tenantID string, now time.Time) (map[string]struct{}, error)
}

// leaseWorkReader answers LiveWorkReader from the engine's job leases.
type leaseWorkReader struct {
	leases orchestration.JobLeaseStore
	ttl    time.Duration
}

// Compile-time interface satisfaction check.
var _ LiveWorkReader = (*leaseWorkReader)(nil)

// NewLeaseWorkReader returns the LiveWorkReader backed by job leases. The TTL
// must be the one the lease store itself was built with, or the two disagree
// about which holders are alive.
func NewLeaseWorkReader(leases orchestration.JobLeaseStore, ttl time.Duration) (LiveWorkReader, error) {
	switch {
	case leases == nil:
		return nil, &InvalidInputError{Field: "leases", Message: "ownership is read from the job leases the fleet holds"}
	case ttl <= 0:
		return nil, &InvalidInputError{Field: "lease_ttl", Message: "must be positive; a zero TTL makes every holder stale on the next read"}
	}
	return &leaseWorkReader{leases: leases, ttl: ttl}, nil
}

// LiveRefs implements LiveWorkReader.
//
// FLEET-WIDE BY DEFAULT, not project-wide. The failure this predicate exists to
// stop is two projects bound to one repository each adopting the same issue, and
// a project-scoped read cannot see the other project's lease.
func (r *leaseWorkReader) LiveRefs(ctx context.Context, tenantID string, now time.Time) (map[string]struct{}, error) {
	held, err := r.leases.List(ctx, tenantID, "")
	if err != nil {
		return nil, err
	}
	refs := make(map[string]struct{}, len(held))
	for _, lease := range held {
		if lease == nil || lease.Ref == "" || !lease.Live(now, r.ttl) {
			continue
		}
		refs[lease.Ref] = struct{}{}
	}
	return refs, nil
}

// =============================================================================
// The evaluator
// =============================================================================

// ReadinessScope is everything one issue's readiness is judged against.
type ReadinessScope struct {
	// Binding carries the trigger label and the readiness policy.
	Binding *ProjectBinding

	// Forge reads the issues and tags the predicates consult.
	Forge ForgeBackend

	// Repo is the repository the issue lives in — the binding's own.
	Repo RepoRef

	// LiveRefs are the work references the fleet holds, read once per sweep
	// rather than once per issue.
	LiveRefs map[string]struct{}

	// Closing maps an issue number to the open pull request that already
	// closes it. It is read once per sweep from the repository's open pull
	// requests (see closingClaims).
	Closing map[int]ClosingClaim
}

// ClosingClaim is an open pull request's claim on an issue: it says it closes
// the issue. Only an open pull request claims; see issue_source_claims.go.
type ClosingClaim struct {
	// PullRequest is the claiming pull request's number.
	PullRequest int
}

// Validate checks the scope names what every predicate needs.
func (s ReadinessScope) Validate() error {
	switch {
	case s.Binding == nil:
		return &InvalidInputError{Field: "binding", Message: "the trigger label and the readiness policy live on the binding"}
	case s.Forge == nil:
		return &InvalidInputError{Field: "forge", Message: "a dependency is decided through the forge adapter"}
	case s.Repo.Owner == "" || s.Repo.Repo == "":
		return &InvalidInputError{Field: "repo", Message: "a bare issue reference resolves against the declaring issue's repository"}
	}
	return nil
}

// ReadinessEvaluator decides one issue's readiness.
//
// Stateless between calls and safe to share: everything it reads for one
// decision arrives in the scope.
type ReadinessEvaluator struct {
	now func() time.Time
}

// NewReadinessEvaluator builds the evaluator; a nil clock takes time.Now.
func NewReadinessEvaluator(now func() time.Time) *ReadinessEvaluator {
	if now == nil {
		now = time.Now
	}
	return &ReadinessEvaluator{now: now}
}

// readinessPredicate is one test. It answers a finding when it DECIDES, and
// (nil, nil) when it has nothing to say and the next predicate should run.
//
// A predicate that returns an error has failed to evaluate, which is different
// from deciding against the issue: the first is an outage the next sweep retries
// and the second is a refusal that stands.
type readinessPredicate func(ctx context.Context, e *ReadinessEvaluator, scope ReadinessScope, issue *ForgeIssue) (*ReadinessFinding, error)

// readinessPredicates is the ordered gate.
//
// A SLICE RATHER THAN A MAP, and this is the one place in the module where order
// is the semantics rather than an implementation detail: the tests run cheapest
// first, so an issue refused on its own text costs no forge call, and the
// dependency walk — the only predicate that makes unbounded network calls — runs
// last and only for an issue that would otherwise be adopted.
var readinessPredicates = []readinessPredicate{
	predicateTriggered,
	predicateNotHeld,
	predicateAcceptanceCriteria,
	predicateNotOwned,
	predicateNotClosedByPullRequest,
	predicateDependenciesSatisfied,
}

// Evaluate judges one issue.
func (e *ReadinessEvaluator) Evaluate(ctx context.Context, scope ReadinessScope, issue *ForgeIssue) (ReadinessFinding, error) {
	if err := scope.Validate(); err != nil {
		return ReadinessFinding{}, err
	}
	if issue == nil {
		return ReadinessFinding{}, &InvalidInputError{Field: "issue", Message: "must not be nil"}
	}
	for _, predicate := range readinessPredicates {
		finding, err := predicate(ctx, e, scope, issue)
		if err != nil {
			// A BACKEND OUTAGE IS NOT ONE ISSUE'S PROBLEM. The forge being down
			// makes every issue in the sweep undecidable, and reporting that as
			// N per-issue verdicts leaves the sweep looking healthy while it
			// adopts nothing — which is the shape of a poller that has silently
			// stopped. It is returned so the caller classifies it once, as the
			// outage it is.
			//
			// EVERYTHING ELSE IS THIS ISSUE'S PROBLEM and must not stop the
			// others: a dangling dependency reference is undecidable for the
			// issue that declared it and says nothing about its neighbours.
			if errors.Is(err, ErrBackend) {
				return ReadinessFinding{}, err
			}
			return ReadinessFinding{
				IssueNumber: issue.Number,
				Verdict:     ReadinessUndecidable,
				Evidence:    "readiness could not be decided, so the issue is left where it is: " + err.Error(),
			}, nil
		}
		if finding != nil {
			finding.IssueNumber = issue.Number
			return *finding, nil
		}
	}
	return ReadinessFinding{
		IssueNumber: issue.Number,
		Verdict:     ReadinessAdopt,
		Evidence:    "open, triggered, unowned, closed by no pull request, and every declared dependency is satisfied",
	}, nil
}

// predicateTriggered is the pre-existing test, unchanged: open and carrying the
// binding's trigger label.
func predicateTriggered(_ context.Context, _ *ReadinessEvaluator, scope ReadinessScope, issue *ForgeIssue) (*ReadinessFinding, error) {
	if issueTriggered(scope.Binding, issue) && !issueClosed(issue) {
		return nil, nil
	}
	return &ReadinessFinding{
		Verdict:  ReadinessNotTriggered,
		Evidence: "the issue is closed, or does not carry the trigger label " + strconv.Quote(scope.Binding.Forge.TriggerLabel),
	}, nil
}

// predicateNotHeld refuses an issue carrying the portfolio hold, even when it
// also carries the trigger. It runs straight after the trigger test and before
// anything that costs a forge call: a held issue is not this source's to look
// at any further.
func predicateNotHeld(_ context.Context, _ *ReadinessEvaluator, _ ReadinessScope, issue *ForgeIssue) (*ReadinessFinding, error) {
	if !issueHeld(issue) {
		return nil, nil
	}
	return &ReadinessFinding{
		Verdict:  ReadinessHeld,
		Evidence: "the issue carries the hold " + strconv.Quote(HoldLabel) + "; only the party named on the issue clears it",
	}, nil
}

// predicateAcceptanceCriteria refuses an issue that does not say what done is.
//
// IT READS THE BODY ONLY, not the discussion. Criteria stated in a comment
// months into a thread are criteria the worker has to find, and the worker reads
// the whole contract — so the predicate is not about whether the text EXISTS
// anywhere, it is about whether the issue's own statement of the work includes
// it. A comment-tolerant version would pass exactly the issues this exists to
// catch.
func predicateAcceptanceCriteria(_ context.Context, _ *ReadinessEvaluator, scope ReadinessScope, issue *ForgeIssue) (*ReadinessFinding, error) {
	policy := scope.Binding.Readiness
	if !policy.RequireAcceptanceCriteria {
		return nil, nil
	}
	if HasAcceptanceCriteria(issue.Body, policy.Headings()) {
		return nil, nil
	}
	return &ReadinessFinding{
		Verdict: ReadinessNoAcceptanceCriteria,
		Evidence: "the issue states no acceptance criteria: none of the section heading(s) " +
			strings.Join(policy.Headings(), ", ") + " appears in its body, so an agent would have to guess what done means",
	}, nil
}

// HasAcceptanceCriteria reports whether body carries one of headings as a
// markdown heading or a bold line, with something under it.
//
// THE HEADING ALONE IS NOT CRITERIA. An issue template leaves empty sections
// behind, and a predicate satisfied by the heading would pass precisely the
// template-filled issue that states nothing — so a heading must be followed by
// at least one non-blank line that is not itself another heading.
func HasAcceptanceCriteria(body string, headings []string) bool {
	if len(headings) == 0 {
		return false
	}
	wanted := make(map[string]struct{}, len(headings))
	for _, heading := range headings {
		wanted[heading] = struct{}{}
	}
	lines := strings.Split(body, "\n")
	for i, raw := range lines {
		title, isHeading := headingTitle(raw)
		if !isHeading {
			continue
		}
		if _, match := wanted[title]; !match {
			continue
		}
		for _, following := range lines[i+1:] {
			trimmed := strings.TrimSpace(following)
			if trimmed == "" {
				continue
			}
			if _, nextHeading := headingTitle(following); nextHeading {
				break
			}
			return true
		}
	}
	return false
}

// headingTitle reads a line as a section heading, returning its lower-cased
// title.
//
// Three spellings, because all three are in use in this tree's own issues: an
// ATX heading ("## Verification"), a bold line ("**Verification**") and a bold
// line with a colon ("**Verification:**"). A trailing colon on a plain heading
// is also accepted for the same reason.
func headingTitle(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	switch {
	case strings.HasPrefix(trimmed, "#"):
		trimmed = strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
	case strings.HasPrefix(trimmed, "**") && strings.HasSuffix(trimmed, "**") && len(trimmed) > 4:
		trimmed = strings.TrimSpace(trimmed[2 : len(trimmed)-2])
	default:
		return "", false
	}
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(trimmed), ":")), trimmed != ""
}

// predicateNotOwned refuses an issue a live session already holds.
func predicateNotOwned(_ context.Context, _ *ReadinessEvaluator, scope ReadinessScope, issue *ForgeIssue) (*ReadinessFinding, error) {
	ref := IssueRefString(scope.Repo, issue.Number)
	if _, owned := scope.LiveRefs[ref]; !owned {
		return nil, nil
	}
	return &ReadinessFinding{
		Verdict:  ReadinessOwned,
		Evidence: "a live job lease already holds " + ref + ", so adopting it would put a second agent on one piece of work",
	}, nil
}

// predicateNotClosedByPullRequest refuses an issue a pull request already
// closes: one whose text says so (scope.Closing, read once per sweep), or one
// the forge itself links to the issue (IssueLinkReader, read per issue and
// only when the text claimed nothing). See closing_refs.go and forge_links.go.
func predicateNotClosedByPullRequest(ctx context.Context, _ *ReadinessEvaluator, scope ReadinessScope, issue *ForgeIssue) (*ReadinessFinding, error) {
	claim, claimed := scope.Closing[issue.Number]
	if !claimed {
		reader, reads := scope.Forge.(IssueLinkReader)
		if !reads {
			return nil, nil
		}
		links, err := reader.ListClosingPullRequests(ctx, scope.Repo, issue.Number)
		if err != nil {
			return nil, err
		}
		if claim, claimed = claimFromLinks(links); !claimed {
			return nil, nil
		}
	}
	return &ReadinessFinding{
		Verdict: ReadinessClosedByPullRequest,
		Claim:   claim,
		Evidence: "the open pull request #" + strconv.Itoa(claim.PullRequest) +
			" already closes it, so adopting it would put a second change under review for one piece of work",
	}, nil
}

// predicateDependenciesSatisfied walks the issue's declared dependencies.
func predicateDependenciesSatisfied(ctx context.Context, e *ReadinessEvaluator, scope ReadinessScope, issue *ForgeIssue) (*ReadinessFinding, error) {
	if scope.Binding.Readiness.SkipDependencies {
		return nil, nil
	}
	graph, err := e.walkDependencies(ctx, scope, issue)
	if err != nil {
		return readinessRefusal(err)
	}
	if len(graph.unsatisfied) == 0 {
		return nil, nil
	}
	first := graph.unsatisfied[0]
	return &ReadinessFinding{
		Verdict:      ReadinessPark,
		Condition:    first.Condition(),
		Dependencies: graph.declared,
		Unsatisfied:  graph.unsatisfied,
		Evidence: "the issue declares " + strconv.Itoa(len(graph.declared)) +
			" dependency/dependencies, of which " + strconv.Itoa(len(graph.unsatisfied)) +
			" are unsatisfied: " + describeDependencies(graph.unsatisfied),
	}, nil
}

// readinessRefusal turns a dependency-walk failure into either a standing
// refusal or an undecidable answer.
//
// THE SPLIT IS THE WHOLE VALUE. A cycle, a depth bound and a malformed
// declaration are properties of what somebody wrote: no sweep will ever change
// them, so they park for a person. A forge that would not answer is an outage:
// parking for a person would summon somebody to look at a healthy issue. Both
// leave the issue unstarted, which is why conflating them is easy and why doing
// so wastes a person's time on every outage.
func readinessRefusal(err error) (*ReadinessFinding, error) {
	var cycle *DependencyCycleError
	if errors.As(err, &cycle) {
		return &ReadinessFinding{
			Verdict:  ReadinessRefuse,
			Cycle:    cycle.Chain,
			Evidence: "the dependency graph cannot be satisfied: " + err.Error(),
		}, nil
	}
	if errors.Is(err, ErrDependencyDepthExceeded) || errors.Is(err, ErrInvalidInput) {
		return &ReadinessFinding{
			Verdict:  ReadinessRefuse,
			Evidence: "the dependency graph cannot be satisfied: " + err.Error(),
		}, nil
	}
	return nil, err
}

// describeDependencies renders a dependency list for a person.
func describeDependencies(dependencies []Dependency) string {
	parts := make([]string, 0, len(dependencies))
	for _, dependency := range dependencies {
		parts = append(parts, dependency.Describe())
	}
	return strings.Join(parts, "; ")
}

// =============================================================================
// The dependency walk
// =============================================================================

// dependencyGraph is what one walk found.
type dependencyGraph struct {
	// declared are the root issue's own declarations, in declaration order.
	declared []Dependency

	// unsatisfied are the declarations that are not met, in declaration order.
	unsatisfied []Dependency
}

// walkDependencies reads the issue's declarations, checks each, and follows the
// unsatisfied issue dependencies far enough to prove the graph is acyclic.
//
// THREE BOUNDS, AND EACH ONE IS LOAD-BEARING.
//
//  1. CYCLE DETECTION. A blocks B blocks A is an issue that can never start,
//     and without detection the pair sits in the queue being re-examined
//     forever — or, if a future caller treats an unsatisfied dependency as work
//     to spawn, spawns agents in a loop. That is the most expensive failure mode
//     available here, so the walk keeps the DFS path and refuses the moment it
//     meets a reference already on it.
//
//  2. A DEPTH BOUND. A cycle-free chain can still be arbitrarily long, and each
//     level is a forge round trip on every sweep. The bound is refused rather
//     than truncated: truncating would report a graph as satisfiable on the
//     strength of the part we looked at.
//
//  3. A VISITED SET. A diamond — two dependencies that share an upstream — is
//     not a cycle and must not be refused as one, and re-reading the shared node
//     once per path is exponential in the diamond count.
//
// THE WALK FOLLOWS UNSATISFIED DEPENDENCIES ONLY. A closed issue's own
// declarations are irrelevant: it is done, so nothing it once waited for can
// block anything now, and following it would turn every finished chain in a
// repository's history into forge traffic. A cycle through a closed issue is
// therefore not reported, which is correct — it is not a cycle that blocks
// anybody.
func (e *ReadinessEvaluator) walkDependencies(ctx context.Context, scope ReadinessScope, issue *ForgeIssue) (dependencyGraph, error) {
	root := IssueRefString(scope.Repo, issue.Number)
	comments, err := scope.Forge.ListIssueComments(ctx, scope.Repo, issue.Number)
	if err != nil {
		return dependencyGraph{}, err
	}
	declared, err := ParseDependencies(scope.Repo, issue.Body, comments)
	if err != nil {
		return dependencyGraph{}, err
	}

	walk := &dependencyWalk{
		evaluator: e,
		scope:     scope,
		depth:     scope.Binding.Readiness.Depth(),
		visited:   map[string]struct{}{root: {}},
		onPath:    map[string]struct{}{root: {}},
		path:      []string{root},
	}
	graph := dependencyGraph{declared: declared}
	for _, dependency := range declared {
		satisfied, checkErr := walk.satisfied(ctx, dependency)
		if checkErr != nil {
			return dependencyGraph{}, checkErr
		}
		if satisfied {
			continue
		}
		graph.unsatisfied = append(graph.unsatisfied, dependency)
		if err := walk.follow(ctx, dependency, 1); err != nil {
			return dependencyGraph{}, err
		}
	}
	return graph, nil
}

// dependencyWalk carries one walk's state.
//
// Not concurrency-safe, and does not need to be: a walk is one goroutine
// following one issue's chain.
type dependencyWalk struct {
	evaluator *ReadinessEvaluator
	scope     ReadinessScope
	depth     int

	// visited is every reference already resolved, so a shared upstream is
	// read once per walk rather than once per path.
	visited map[string]struct{}

	// onPath is the references on the CURRENT DFS path — the cycle test. It is
	// distinct from visited for the reason every DFS keeps both: a reference
	// reachable by two paths is a diamond, and only a reference reachable from
	// itself is a cycle.
	onPath map[string]struct{}

	// path is onPath in order, so a refusal can name the chain rather than
	// only the reference it closed on.
	path []string
}

// satisfied decides one dependency through the same checks UnblockSweeper uses.
func (w *dependencyWalk) satisfied(ctx context.Context, dependency Dependency) (bool, error) {
	condition := dependency.Condition()
	if err := condition.Validate(); err != nil {
		return false, err
	}
	spec, known := unblockKinds[condition.Kind]
	if !known || spec.check == nil {
		return false, &UnblockKindError{Kind: string(condition.Kind), Sentinel: ErrUnblockKindUnhandled}
	}
	return spec.check(ctx, unblockScope{
		forge: w.scope.Forge,
		repo:  dependency.Repo,
		now:   w.evaluator.now,
	}, condition)
}

// follow descends into one unsatisfied dependency's own declarations.
func (w *dependencyWalk) follow(ctx context.Context, dependency Dependency, depth int) error {
	ref := dependency.IssueRef()
	if ref == "" {
		// A release dependency is a leaf: a tag declares nothing.
		return nil
	}
	if _, cycling := w.onPath[ref]; cycling {
		return &DependencyCycleError{Chain: append(append([]string{}, w.path...), ref)}
	}
	if depth > w.depth {
		return &DependencyDepthExceededError{Chain: append(append([]string{}, w.path...), ref), Limit: w.depth}
	}
	if _, seen := w.visited[ref]; seen {
		return nil
	}
	w.visited[ref] = struct{}{}
	w.onPath[ref] = struct{}{}
	w.path = append(w.path, ref)
	defer func() {
		delete(w.onPath, ref)
		w.path = w.path[:len(w.path)-1]
	}()

	issue, err := w.scope.Forge.GetIssue(ctx, dependency.Repo, dependency.Issue)
	if err != nil {
		return err
	}
	comments, err := w.scope.Forge.ListIssueComments(ctx, dependency.Repo, dependency.Issue)
	if err != nil {
		return err
	}
	declared, err := ParseDependencies(dependency.Repo, issue.Body, comments)
	if err != nil {
		return err
	}
	for _, next := range declared {
		satisfied, checkErr := w.satisfied(ctx, next)
		if checkErr != nil {
			return checkErr
		}
		if satisfied {
			continue
		}
		if err := w.follow(ctx, next, depth+1); err != nil {
			return err
		}
	}
	return nil
}

// =============================================================================
// Reports
// =============================================================================

// ReadinessReport is one project sweep's readiness answers.
type ReadinessReport struct {
	// Findings is one entry per issue seen, ordered by issue number so two
	// sweeps over the same state produce comparable reports.
	Findings []ReadinessFinding
}

// Count returns how many findings carry a verdict.
func (r ReadinessReport) Count(v ReadinessVerdict) int {
	n := 0
	for _, finding := range r.Findings {
		if finding.Verdict == v {
			n++
		}
	}
	return n
}

// Sorted returns the findings ordered by issue number.
func (r ReadinessReport) Sorted() []ReadinessFinding {
	out := append([]ReadinessFinding{}, r.Findings...)
	sort.Slice(out, func(i, j int) bool { return out[i].IssueNumber < out[j].IssueNumber })
	return out
}
