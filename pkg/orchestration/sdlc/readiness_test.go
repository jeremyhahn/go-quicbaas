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
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// Fixtures
// =============================================================================

// readinessClock is the instant every readiness test judges at.
var readinessClock = time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)

// readinessRig is one binding, one forge and the evaluator over them.
type readinessRig struct {
	binding   *ProjectBinding
	forge     *fakeForge
	evaluator *ReadinessEvaluator
	liveRefs  map[string]struct{}
}

func newReadinessRig(t *testing.T) *readinessRig {
	t.Helper()
	return &readinessRig{
		binding:   testBinding(),
		forge:     newFakeForge(),
		evaluator: NewReadinessEvaluator(func() time.Time { return readinessClock }),
		liveRefs:  map[string]struct{}{},
	}
}

func (r *readinessRig) scope() ReadinessScope {
	return ReadinessScope{
		Binding: r.binding, Forge: r.forge, Repo: r.binding.Repo(), LiveRefs: r.liveRefs,
	}
}

// issue publishes an open, triggered issue with the given body.
func (r *readinessRig) issue(number int, body string) *ForgeIssue {
	issue := r.forge.putIssue(number, "issue "+strconv.Itoa(number), r.binding.Forge.TriggerLabel)
	issue.Body = body
	return issue
}

func (r *readinessRig) evaluate(t *testing.T, issue *ForgeIssue) ReadinessFinding {
	t.Helper()
	finding, err := r.evaluator.Evaluate(context.Background(), r.scope(), issue)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	return finding
}

// =============================================================================
// Construction and validation
// =============================================================================

func TestNewReadinessEvaluator_DefaultsTheClock(t *testing.T) {
	t.Parallel()
	if NewReadinessEvaluator(nil).now == nil {
		t.Fatal("an evaluator built with no clock must default to one")
	}
	fixed := func() time.Time { return readinessClock }
	if !NewReadinessEvaluator(fixed).now().Equal(readinessClock) {
		t.Fatal("an injected clock must be the one read")
	}
}

func TestReadinessEvaluator_RefusesAnIncompleteScope(t *testing.T) {
	t.Parallel()
	rig := newReadinessRig(t)
	issue := rig.issue(1, "")

	cases := map[string]ReadinessScope{
		"no binding": {Forge: rig.forge, Repo: rig.binding.Repo()},
		"no forge":   {Binding: rig.binding, Repo: rig.binding.Repo()},
		"no repo":    {Binding: rig.binding, Forge: rig.forge},
		"half a repo": {
			Binding: rig.binding, Forge: rig.forge, Repo: RepoRef{Owner: "acme"},
		},
	}
	for name, scope := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := rig.evaluator.Evaluate(context.Background(), scope, issue); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("want ErrInvalidInput, got %v", err)
			}
		})
	}
	if _, err := rig.evaluator.Evaluate(context.Background(), rig.scope(), nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("a nil issue: want ErrInvalidInput, got %v", err)
	}
}

func TestReadinessPolicy_Defaults(t *testing.T) {
	t.Parallel()
	var zero ReadinessPolicy
	if got := zero.Depth(); got != DefaultMaxDependencyDepth {
		t.Fatalf("Depth = %d, want %d", got, DefaultMaxDependencyDepth)
	}
	if got := len(zero.Headings()); got != len(DefaultAcceptanceHeadings) {
		t.Fatalf("Headings = %v, want the defaults", zero.Headings())
	}
	// THE ZERO VALUE ENABLES THE FACT-BASED PREDICATES AND NOT THE JUDGEMENT ONE.
	if zero.RequireAcceptanceCriteria {
		t.Fatal("acceptance criteria are a judgement and must be opt-in")
	}
	if zero.SkipDependencies {
		t.Fatal("dependency satisfaction is a fact and must be on by default")
	}

	explicit := ReadinessPolicy{MaxDependencyDepth: 3, AcceptanceHeadings: []string{" Done ", "  "}}
	if got := explicit.Depth(); got != 3 {
		t.Fatalf("Depth = %d, want 3", got)
	}
	headings := explicit.Headings()
	if len(headings) != 1 || headings[0] != "done" {
		t.Fatalf("Headings = %v, want the trimmed lower-cased non-blank ones", headings)
	}
	negative := ReadinessPolicy{MaxDependencyDepth: -1}
	if got := negative.Depth(); got != DefaultMaxDependencyDepth {
		t.Fatalf("a negative depth must take the default at read time: %d", got)
	}
}

func TestReadinessPolicy_Validate(t *testing.T) {
	t.Parallel()
	for _, policy := range []ReadinessPolicy{
		{},
		{RequireAcceptanceCriteria: true},
		{MaxDependencyDepth: 4, AcceptanceHeadings: []string{"Done"}},
		{SkipDependencies: true},
	} {
		if err := policy.Validate(); err != nil {
			t.Fatalf("a usable policy was refused: %+v: %v", policy, err)
		}
	}
	for name, policy := range map[string]ReadinessPolicy{
		"a negative depth":  {MaxDependencyDepth: -1},
		"a blank heading":   {AcceptanceHeadings: []string{"Done", "  "}},
		"only a blank one":  {AcceptanceHeadings: []string{""}},
		"a tabbed heading":  {AcceptanceHeadings: []string{"\t"}},
		"required, blank h": {RequireAcceptanceCriteria: true, AcceptanceHeadings: []string{" "}},
	} {
		if err := policy.Validate(); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("%s: want ErrInvalidInput, got %v", name, err)
		}
	}
}

// TestProjectBinding_ValidatesItsReadinessPolicy: the policy must be refused at
// the write, not at the sweep that meets it.
func TestProjectBinding_ValidatesItsReadinessPolicy(t *testing.T) {
	t.Parallel()
	binding := testBinding()
	binding.Readiness = ReadinessPolicy{MaxDependencyDepth: -1}
	if err := binding.Validate(); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("want ErrInvalidInput, got %v", err)
	}
	binding.Readiness = ReadinessPolicy{MaxDependencyDepth: 4, RequireAcceptanceCriteria: true}
	if err := binding.Validate(); err != nil {
		t.Fatalf("a usable readiness policy was refused: %v", err)
	}
}

// =============================================================================
// The predicate the gate already had
// =============================================================================

func TestReadiness_TriggerAndStateAreStillTheFirstTest(t *testing.T) {
	t.Parallel()
	rig := newReadinessRig(t)

	ready := rig.issue(1, "")
	if got := rig.evaluate(t, ready).Verdict; got != ReadinessAdopt {
		t.Fatalf("an open triggered issue: verdict = %s, want adopt", got)
	}

	closed := rig.issue(2, "")
	closed.State = "closed"
	if got := rig.evaluate(t, closed).Verdict; got != ReadinessNotTriggered {
		t.Fatalf("a closed issue: verdict = %s, want not_triggered", got)
	}

	untriggered := rig.forge.putIssue(3, "no label")
	finding := rig.evaluate(t, untriggered)
	if finding.Verdict != ReadinessNotTriggered {
		t.Fatalf("an unlabelled issue: verdict = %s, want not_triggered", finding.Verdict)
	}
	if !strings.Contains(finding.Evidence, rig.binding.Forge.TriggerLabel) {
		t.Fatalf("the evidence must name the trigger label it looked for: %q", finding.Evidence)
	}
	if finding.Verdict.Adoptable() {
		t.Fatal("an untriggered issue must not produce a task of any kind")
	}
}

// =============================================================================
// Acceptance criteria
// =============================================================================

// TestReadiness_AnIssueWithNoAcceptanceCriteriaIsRefused: an agent handed an
// issue with no statement of done has to guess, and the guess is discovered at
// review, after the work.
func TestReadiness_AnIssueWithNoAcceptanceCriteriaIsRefused(t *testing.T) {
	t.Parallel()
	rig := newReadinessRig(t)
	rig.binding.Readiness.RequireAcceptanceCriteria = true

	bare := rig.issue(1, "Please make the thing faster.")
	finding := rig.evaluate(t, bare)
	if finding.Verdict != ReadinessNoAcceptanceCriteria {
		t.Fatalf("verdict = %s, want no_acceptance_criteria", finding.Verdict)
	}
	if !strings.Contains(finding.Evidence, "verification") {
		t.Fatalf("the evidence must list the headings it looked for: %q", finding.Evidence)
	}
	if finding.Verdict.Adoptable() {
		t.Fatal("an issue with nothing to verify must not become a task")
	}

	stated := rig.issue(2, "Make it faster.\n\n## Verification\n- `make bench-x` shows p99 under 1ms\n")
	if got := rig.evaluate(t, stated).Verdict; got != ReadinessAdopt {
		t.Fatalf("an issue stating criteria: verdict = %s, want adopt", got)
	}
}

// TestReadiness_TheAcceptancePredicateIsOptIn: a project whose issues do not use
// the heading convention must not have every issue refused.
func TestReadiness_TheAcceptancePredicateIsOptIn(t *testing.T) {
	t.Parallel()
	rig := newReadinessRig(t)
	bare := rig.issue(1, "Please make the thing faster.")
	if got := rig.evaluate(t, bare).Verdict; got != ReadinessAdopt {
		t.Fatalf("with the predicate off: verdict = %s, want adopt", got)
	}
}

// TestHasAcceptanceCriteria is the predicate's own table. The half that earns its
// keep is the EMPTY SECTION: an issue template leaves headings behind, and a
// predicate satisfied by the heading alone would pass exactly the template-filled
// issue that states nothing.
func TestHasAcceptanceCriteria(t *testing.T) {
	t.Parallel()
	headings := DefaultAcceptanceHeadings
	cases := []struct {
		name string
		body string
		want bool
	}{
		{"an atx heading with content", "## Verification\n- make test\n", true},
		{"a deeper atx heading", "#### Acceptance criteria\nit works\n", true},
		{"a bold line", "**Verification**\n- make test\n", true},
		{"a bold line with a colon", "**Verification:**\nmake test\n", true},
		{"a plain heading with a trailing colon", "## Verification:\nmake test\n", true},
		{"case is irrelevant", "## VERIFICATION\nmake test\n", true},
		{"definition of done", "## Definition of done\nit ships\n", true},
		{"content after a blank line still counts", "## Verification\n\n\n- make test\n", true},
		{"a later section also counts", "## Design\nx\n\n## Acceptance\ny\n", true},

		{"no heading at all", "just prose\n", false},
		{"a heading nobody listed", "## Notes\nmake test\n", false},
		{"an empty section", "## Verification\n", false},
		{"an empty section followed by another heading", "## Verification\n\n## Notes\nx\n", false},
		{"a heading with only blank lines under it", "## Verification\n   \n\t\n", false},
		{"the word in prose", "we should add verification later\n", false},
		{"an empty body", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := HasAcceptanceCriteria(tc.body, headings); got != tc.want {
				t.Fatalf("HasAcceptanceCriteria(%q) = %v, want %v", tc.body, got, tc.want)
			}
		})
	}
	if HasAcceptanceCriteria("## Verification\nx\n", nil) {
		t.Fatal("with no headings configured nothing can match")
	}
}

func TestHeadingTitle(t *testing.T) {
	t.Parallel()
	cases := []struct {
		line  string
		title string
		ok    bool
	}{
		{"## Verification", "verification", true},
		{"# Done", "done", true},
		{"###### Deep", "deep", true},
		{"  ## Indented  ", "indented", true},
		{"**Bold**", "bold", true},
		{"**Bold:**", "bold", true},
		{"## Trailing:", "trailing", true},
		{"plain text", "", false},
		{"", "", false},
		{"**", "", false},
		{"****", "", false},
	}
	for _, tc := range cases {
		t.Run(strconv.Quote(tc.line), func(t *testing.T) {
			title, ok := headingTitle(tc.line)
			if ok != tc.ok || title != tc.title {
				t.Fatalf("headingTitle(%q) = %q, %v; want %q, %v", tc.line, title, ok, tc.title, tc.ok)
			}
		})
	}
}

// =============================================================================
// Live ownership
// =============================================================================

// TestReadiness_AnIssueOwnedByALiveSessionIsNotAdoptedTwice is one of the
// issue's acceptance checks: the failure it stops is two agents authoring two
// branches for one issue, which the per-project source index cannot catch when
// two projects are bound to one repository.
func TestReadiness_AnIssueOwnedByALiveSessionIsNotAdoptedTwice(t *testing.T) {
	t.Parallel()
	rig := newReadinessRig(t)
	issue := rig.issue(7, "")
	ref := IssueRefString(rig.binding.Repo(), 7)
	rig.liveRefs[ref] = struct{}{}

	finding := rig.evaluate(t, issue)
	if finding.Verdict != ReadinessOwned {
		t.Fatalf("verdict = %s, want owned", finding.Verdict)
	}
	if !strings.Contains(finding.Evidence, ref) {
		t.Fatalf("the evidence must name the reference that is held: %q", finding.Evidence)
	}
	if finding.Verdict.Adoptable() {
		t.Fatal("an owned issue must not produce a second task")
	}

	// A lease on a DIFFERENT issue is not this issue's owner.
	delete(rig.liveRefs, ref)
	rig.liveRefs[IssueRefString(rig.binding.Repo(), 8)] = struct{}{}
	if got := rig.evaluate(t, issue).Verdict; got != ReadinessAdopt {
		t.Fatalf("another issue's lease must not block this one: verdict = %s", got)
	}
}

// TestLeaseWorkReader_ReportsOnlyLiveRefs: a released or stale lease owns
// nothing, or an issue would be un-adoptable forever after any crash.
func TestLeaseWorkReader_ReportsOnlyLiveRefs(t *testing.T) {
	t.Parallel()
	const ttl = time.Minute
	store := &readinessLeaseStore{records: []*orchestration.JobLease{
		{
			TenantID: testTenant, ProjectID: "p", Ref: "acme/api#1", JobID: "j1", Generation: 1,
			AcquiredAt: readinessClock.UnixNano(), HeartbeatAt: readinessClock.UnixNano(),
		},
		{
			TenantID: testTenant, ProjectID: "other", Ref: "acme/api#2", JobID: "j2", Generation: 1,
			AcquiredAt: readinessClock.UnixNano(), HeartbeatAt: readinessClock.UnixNano(),
		},
		{
			TenantID: testTenant, ProjectID: "p", Ref: "acme/api#3", JobID: "j3", Generation: 1,
			AcquiredAt: readinessClock.UnixNano(), HeartbeatAt: readinessClock.UnixNano(),
			Released: true, ReleasedAt: readinessClock.UnixNano(),
		},
		{
			TenantID: testTenant, ProjectID: "p", Ref: "acme/api#4", JobID: "j4", Generation: 1,
			AcquiredAt:  readinessClock.Add(-time.Hour).UnixNano(),
			HeartbeatAt: readinessClock.Add(-time.Hour).UnixNano(),
		},
		{TenantID: testTenant, ProjectID: "p", Ref: "", JobID: "j5", Generation: 1,
			HeartbeatAt: readinessClock.UnixNano()},
		nil,
	}}
	reader, err := NewLeaseWorkReader(store, ttl)
	if err != nil {
		t.Fatalf("NewLeaseWorkReader: %v", err)
	}
	refs, err := reader.LiveRefs(context.Background(), testTenant, readinessClock)
	if err != nil {
		t.Fatalf("LiveRefs: %v", err)
	}
	// #1 is live in this project, #2 is live in ANOTHER project of the same
	// tenant and is exactly the cross-project case this predicate exists for.
	for _, want := range []string{"acme/api#1", "acme/api#2"} {
		if _, ok := refs[want]; !ok {
			t.Fatalf("LiveRefs = %v, must include the live %s", refs, want)
		}
	}
	for _, absent := range []string{"acme/api#3", "acme/api#4", ""} {
		if _, ok := refs[absent]; ok {
			t.Fatalf("LiveRefs = %v, must not include %q", refs, absent)
		}
	}
}

func TestNewLeaseWorkReader_RefusesAnUnusableConfiguration(t *testing.T) {
	t.Parallel()
	if _, err := NewLeaseWorkReader(nil, time.Minute); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("a nil lease store: want ErrInvalidInput, got %v", err)
	}
	store := &readinessLeaseStore{}
	for _, ttl := range []time.Duration{0, -time.Second} {
		if _, err := NewLeaseWorkReader(store, ttl); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("ttl %s: want ErrInvalidInput, got %v", ttl, err)
		}
	}
}

func TestLeaseWorkReader_PropagatesAStoreFailure(t *testing.T) {
	t.Parallel()
	down := errors.New("kv down")
	reader, err := NewLeaseWorkReader(&readinessLeaseStore{err: down}, time.Minute)
	if err != nil {
		t.Fatalf("NewLeaseWorkReader: %v", err)
	}
	if _, err := reader.LiveRefs(context.Background(), testTenant, readinessClock); !errors.Is(err, down) {
		t.Fatalf("LiveRefs must not swallow a store failure: %v", err)
	}
}

// readinessLeaseStore is a JobLeaseStore whose records a test writes directly.
// Only List is overridden; nothing else is reached.
type readinessLeaseStore struct {
	orchestration.JobLeaseStore
	records []*orchestration.JobLease
	err     error
}

func (s *readinessLeaseStore) List(_ context.Context, tenantID, projectID string) ([]*orchestration.JobLease, error) {
	if s.err != nil {
		return nil, s.err
	}
	out := make([]*orchestration.JobLease, 0, len(s.records))
	for _, lease := range s.records {
		if lease == nil {
			out = append(out, lease)
			continue
		}
		if tenantID != "" && lease.TenantID != tenantID {
			continue
		}
		if projectID != "" && lease.ProjectID != projectID {
			continue
		}
		out = append(out, lease)
	}
	return out, nil
}

// =============================================================================
// Dependencies — park and resume
// =============================================================================

// TestReadiness_AnUnsatisfiedDependencyParksWithACheckableCondition is the
// issue's park-and-resume acceptance check, first half: the task carries a typed
// condition that UnblockSweeper already knows how to evaluate.
func TestReadiness_AnUnsatisfiedDependencyParksWithACheckableCondition(t *testing.T) {
	t.Parallel()
	rig := newReadinessRig(t)
	upstream := rig.forge.putIssue(41, "the upstream work")
	upstream.State = "open"
	issue := rig.issue(42, "Do the thing.\n\nBlocked by: #41\n")

	finding := rig.evaluate(t, issue)
	if finding.Verdict != ReadinessPark {
		t.Fatalf("verdict = %s, want park", finding.Verdict)
	}
	if !finding.Verdict.Adoptable() {
		t.Fatal("a parked issue still becomes a task; that is what makes it visible and resumable")
	}
	if finding.Condition.Kind != UnblockForgeIssueState || finding.Condition.Subject != "acme/api#41" {
		t.Fatalf("Condition = %+v", finding.Condition)
	}
	if len(finding.Dependencies) != 1 || len(finding.Unsatisfied) != 1 {
		t.Fatalf("Dependencies = %+v, Unsatisfied = %+v", finding.Dependencies, finding.Unsatisfied)
	}
	if finding.ParkKind() != orchestration.TaskBlockKindBlocked {
		t.Fatalf("ParkKind = %s, want blocked: it waits on an external condition", finding.ParkKind())
	}
	reason, err := finding.ParkReason()
	if err != nil {
		t.Fatalf("ParkReason: %v", err)
	}
	// The reason must round-trip through the SAME parser the sweep uses, or the
	// park would be invisible to the thing that clears it.
	condition, err := ParseUnblockCondition(reason)
	if err != nil {
		t.Fatalf("the park reason must carry a parseable condition: %v (%q)", err, reason)
	}
	if condition != finding.Condition {
		t.Fatalf("round trip gave %+v, want %+v", condition, finding.Condition)
	}
}

// TestReadiness_ASatisfiedDependencyAdopts: the same issue, once its upstream
// closes, is ready. That is the resume half.
func TestReadiness_ASatisfiedDependencyAdopts(t *testing.T) {
	t.Parallel()
	rig := newReadinessRig(t)
	upstream := rig.forge.putIssue(41, "the upstream work")
	issue := rig.issue(42, "Do the thing.\n\nBlocked by: #41\n")

	if got := rig.evaluate(t, issue).Verdict; got != ReadinessPark {
		t.Fatalf("with the upstream open: verdict = %s, want park", got)
	}
	upstream.State = "closed"
	finding := rig.evaluate(t, issue)
	if finding.Verdict != ReadinessAdopt {
		t.Fatalf("with the upstream closed: verdict = %s, want adopt", finding.Verdict)
	}
	reason, err := finding.ParkReason()
	if err != nil {
		t.Fatalf("ParkReason on an adopting finding: %v", err)
	}
	if strings.Contains(reason, "unblock[") {
		t.Fatalf("an adopting finding has no condition to encode: %q", reason)
	}
}

// TestReadiness_MergedIsNotReleased is the issue's merged-but-unreleased
// acceptance check, and it is the one that is easy to get wrong: the upstream
// issue is CLOSED — the fix is merged — and the consumer pins a version, so the
// tag is what decides. Until the tag exists the consumer's build still selects
// the old code.
func TestReadiness_MergedIsNotReleased(t *testing.T) {
	t.Parallel()
	rig := newReadinessRig(t)
	upstreamRepo := RepoRef{Owner: "other", Repo: "lib"}

	// The upstream issue is closed: merged.
	merged := rig.forge.putIssue(7, "the upstream fix")
	merged.State = "closed"

	issue := rig.issue(42,
		"Consume the fix.\n\nBlocked by: other/lib#7\nRequires release: other/lib@v0.9.5-alpha\n")

	finding := rig.evaluate(t, issue)
	if finding.Verdict != ReadinessPark {
		t.Fatalf("merged but not tagged: verdict = %s, want park", finding.Verdict)
	}
	if len(finding.Unsatisfied) != 1 {
		t.Fatalf("exactly the release dependency must be unsatisfied: %+v", finding.Unsatisfied)
	}
	if finding.Unsatisfied[0].Kind != DependencyReleaseTag {
		t.Fatalf("the unsatisfied dependency must be the RELEASE, not the closed issue: %+v", finding.Unsatisfied[0])
	}
	if finding.Condition.Kind != UnblockReleaseTag || finding.Condition.Target != "v0.9.5-alpha" {
		t.Fatalf("Condition = %+v", finding.Condition)
	}

	// The tag is published: now it is released, and the consumer may proceed.
	rig.forge.putTag(upstreamRepo, "v0.9.5-alpha", "deadbeef")
	if got := rig.evaluate(t, issue).Verdict; got != ReadinessAdopt {
		t.Fatalf("once the tag exists: verdict = %s, want adopt", got)
	}
}

// TestReadiness_ADifferentTagIsNotTheOnePinned: a repository that has released
// v1.0.0 has not released v0.9.5, and a check that matched any tag would unblock
// a consumer against a version that does not exist.
func TestReadiness_ADifferentTagIsNotTheOnePinned(t *testing.T) {
	t.Parallel()
	rig := newReadinessRig(t)
	rig.forge.putTag(RepoRef{Owner: "other", Repo: "lib"}, "v1.0.0", "cafe")
	issue := rig.issue(42, "Consume it.\n\nRequires release: other/lib@v0.9.5-alpha\n")

	if got := rig.evaluate(t, issue).Verdict; got != ReadinessPark {
		t.Fatalf("another tag must not satisfy the pinned one: verdict = %s, want park", got)
	}
}

// TestReadiness_ADependencyCycleIsRefusedNotLooped is the issue's cycle
// acceptance check, and the most expensive failure mode it guards: an undetected
// A-blocks-B-blocks-A is an unbounded re-examination, or an unbounded spawn.
func TestReadiness_ADependencyCycleIsRefusedNotLooped(t *testing.T) {
	t.Parallel()
	rig := newReadinessRig(t)
	// #1 blocked by #2, #2 blocked by #1. Both open.
	first := rig.issue(1, "Blocked by: #2\n")
	second := rig.forge.putIssue(2, "the other half")
	second.Body = "Blocked by: #1\n"

	finding := rig.evaluate(t, first)
	if finding.Verdict != ReadinessRefuse {
		t.Fatalf("verdict = %s, want refuse", finding.Verdict)
	}
	if len(finding.Cycle) < 2 {
		t.Fatalf("the finding must name the cycle it found: %v", finding.Cycle)
	}
	if finding.Cycle[0] != finding.Cycle[len(finding.Cycle)-1] {
		t.Fatalf("the chain must close on itself: %v", finding.Cycle)
	}
	if !strings.Contains(finding.Evidence, "cycle") {
		t.Fatalf("the evidence must say what is wrong: %q", finding.Evidence)
	}
	// A CYCLE NEEDS A PERSON, NOT A SWEEP. Nothing external will ever clear it,
	// so parking it as a wait would leave the sweep re-checking a condition that
	// cannot become true.
	if finding.ParkKind() != orchestration.TaskBlockKindNeedsInfo {
		t.Fatalf("ParkKind = %s, want needs_info", finding.ParkKind())
	}
	reason, err := finding.ParkReason()
	if err != nil {
		t.Fatalf("ParkReason: %v", err)
	}
	if strings.Contains(reason, "unblock[") {
		t.Fatalf("a refusal must carry NO checkable condition; inventing one would have the sweep "+
			"evaluate a diagnosis nobody made: %q", reason)
	}
}

// TestReadiness_ALongerCycleIsAlsoRefused: three links, to prove the detection
// is a path check and not a same-issue special case.
func TestReadiness_ALongerCycleIsAlsoRefused(t *testing.T) {
	t.Parallel()
	rig := newReadinessRig(t)
	first := rig.issue(1, "Blocked by: #2\n")
	second := rig.forge.putIssue(2, "two")
	second.Body = "Blocked by: #3\n"
	third := rig.forge.putIssue(3, "three")
	third.Body = "Blocked by: #1\n"

	finding := rig.evaluate(t, first)
	if finding.Verdict != ReadinessRefuse {
		t.Fatalf("verdict = %s, want refuse", finding.Verdict)
	}
	if len(finding.Cycle) != 4 {
		t.Fatalf("Cycle = %v, want the four-link path 1 -> 2 -> 3 -> 1", finding.Cycle)
	}
}

// TestReadiness_ADiamondIsNotACycle: two dependencies sharing one upstream is an
// ordinary graph, and refusing it as a cycle would refuse correct work. This is
// what the visited set is for, kept separate from the on-path set.
func TestReadiness_ADiamondIsNotACycle(t *testing.T) {
	t.Parallel()
	rig := newReadinessRig(t)
	root := rig.issue(1, "Blocked by: #2, #3\n")
	left := rig.forge.putIssue(2, "left")
	left.Body = "Blocked by: #4\n"
	right := rig.forge.putIssue(3, "right")
	right.Body = "Blocked by: #4\n"
	rig.forge.putIssue(4, "shared upstream")

	finding := rig.evaluate(t, root)
	if finding.Verdict != ReadinessPark {
		t.Fatalf("a diamond is ordinary work waiting on an upstream: verdict = %s, want park (%q)",
			finding.Verdict, finding.Evidence)
	}
	if len(finding.Unsatisfied) != 2 {
		t.Fatalf("both of the root's own dependencies are open: %+v", finding.Unsatisfied)
	}
}

// TestReadiness_ADepthBoundIsRefusedNotTruncated: a cycle-free chain can still be
// arbitrarily long, and each level is a forge round trip on every sweep.
// Truncating would report the graph satisfiable on the strength of the part we
// looked at.
func TestReadiness_ADepthBoundIsRefusedNotTruncated(t *testing.T) {
	t.Parallel()
	rig := newReadinessRig(t)
	rig.binding.Readiness.MaxDependencyDepth = 2

	// A chain of five open issues: 1 -> 2 -> 3 -> 4 -> 5.
	root := rig.issue(1, "Blocked by: #2\n")
	for i := 2; i <= 4; i++ {
		link := rig.forge.putIssue(i, "link "+strconv.Itoa(i))
		link.Body = "Blocked by: #" + strconv.Itoa(i+1) + "\n"
	}
	rig.forge.putIssue(5, "the end")

	finding := rig.evaluate(t, root)
	if finding.Verdict != ReadinessRefuse {
		t.Fatalf("verdict = %s, want refuse", finding.Verdict)
	}
	if !strings.Contains(finding.Evidence, "depth bound") {
		t.Fatalf("the evidence must name the bound that was exceeded: %q", finding.Evidence)
	}

	// Within the bound the same chain parks rather than being refused.
	rig.binding.Readiness.MaxDependencyDepth = 8
	if got := rig.evaluate(t, root).Verdict; got != ReadinessPark {
		t.Fatalf("inside the bound: verdict = %s, want park", got)
	}
}

// TestReadiness_AReleaseDependencyIsALeaf: a tag declares nothing, so the walk
// must not try to descend into one — which would be a GetIssue for an issue
// number that does not exist.
func TestReadiness_AReleaseDependencyIsALeaf(t *testing.T) {
	t.Parallel()
	rig := newReadinessRig(t)
	rig.binding.Readiness.MaxDependencyDepth = 1
	issue := rig.issue(1, "Requires release: other/lib@v1.0.0\n")

	finding := rig.evaluate(t, issue)
	if finding.Verdict != ReadinessPark {
		t.Fatalf("verdict = %s, want park (%q)", finding.Verdict, finding.Evidence)
	}
}

// TestReadiness_ASatisfiedDependencyIsNotDescendedInto: a closed issue is done,
// so whatever it once waited for cannot block anybody now. Following it would
// turn every finished chain in a repository's history into forge traffic.
func TestReadiness_ASatisfiedDependencyIsNotDescendedInto(t *testing.T) {
	t.Parallel()
	rig := newReadinessRig(t)
	root := rig.issue(1, "Blocked by: #2\n")
	closed := rig.forge.putIssue(2, "already done")
	closed.State = "closed"
	// #2 once waited on #1 — a cycle, through a CLOSED node, which blocks
	// nobody and must not be reported.
	closed.Body = "Blocked by: #1\n"

	finding := rig.evaluate(t, root)
	if finding.Verdict != ReadinessAdopt {
		t.Fatalf("verdict = %s, want adopt (%q)", finding.Verdict, finding.Evidence)
	}
}

// TestReadiness_SkipDependenciesLeavesTheAxisOut: the opt-out works, and its cost
// is exactly that work starts before what it consumes exists.
func TestReadiness_SkipDependenciesLeavesTheAxisOut(t *testing.T) {
	t.Parallel()
	rig := newReadinessRig(t)
	rig.binding.Readiness.SkipDependencies = true
	rig.forge.putIssue(41, "open upstream")
	issue := rig.issue(42, "Blocked by: #41\n")

	if got := rig.evaluate(t, issue).Verdict; got != ReadinessAdopt {
		t.Fatalf("with the axis off: verdict = %s, want adopt", got)
	}
}

// TestReadiness_AMalformedDeclarationIsRefusedForAPerson: a directive nothing can
// parse is a statement of intent nobody can act on, and proceeding as though the
// issue declared nothing is the failure the refusal exists to stop.
func TestReadiness_AMalformedDeclarationIsRefusedForAPerson(t *testing.T) {
	t.Parallel()
	rig := newReadinessRig(t)
	issue := rig.issue(1, "Blocked by: not-a-reference\n")

	finding := rig.evaluate(t, issue)
	if finding.Verdict != ReadinessRefuse {
		t.Fatalf("verdict = %s, want refuse", finding.Verdict)
	}
	if finding.ParkKind() != orchestration.TaskBlockKindNeedsInfo {
		t.Fatalf("ParkKind = %s, want needs_info", finding.ParkKind())
	}
}

// =============================================================================
// Undecidable
// =============================================================================

// TestReadiness_ADanglingReferenceIsUndecidableForThatIssueOnly: an issue that
// names a record nobody can find is undecidable, and must not stop the issues
// around it being judged.
func TestReadiness_ADanglingReferenceIsUndecidableForThatIssueOnly(t *testing.T) {
	t.Parallel()
	rig := newReadinessRig(t)
	issue := rig.issue(1, "Blocked by: #9999\n")

	finding := rig.evaluate(t, issue)
	if finding.Verdict != ReadinessUndecidable {
		t.Fatalf("verdict = %s, want undecidable", finding.Verdict)
	}
	if finding.Verdict.Adoptable() {
		t.Fatal("an undecidable issue produces no task; the next sweep asks again")
	}
	if finding.Evidence == "" {
		t.Fatal("an undecidable finding must say what could not be decided")
	}
}

// TestReadiness_AForgeOutageIsReturnedRatherThanPerIssue: the forge being down
// makes every issue undecidable, and reporting that as N per-issue verdicts
// leaves the sweep looking healthy while it adopts nothing.
func TestReadiness_AForgeOutageIsReturnedRatherThanPerIssue(t *testing.T) {
	t.Parallel()
	rig := newReadinessRig(t)
	issue := rig.issue(1, "Blocked by: #2\n")
	rig.forge.putIssue(2, "upstream")
	rig.forge.listCommentsErr = backendError("ListIssueComments", errors.New("502"))

	_, err := rig.evaluator.Evaluate(context.Background(), rig.scope(), issue)
	if !errors.Is(err, ErrBackend) {
		t.Fatalf("a backend outage must be returned, not folded into a verdict: %v", err)
	}
}

// TestReadiness_AnUnreadableTagIsUndecidable: a forge that would not answer
// whether a tag exists is different from one that answered "no".
func TestReadiness_AnUnreadableTagIsUndecidable(t *testing.T) {
	t.Parallel()
	rig := newReadinessRig(t)
	issue := rig.issue(1, "Requires release: other/lib@v1.0.0\n")
	rig.forge.getTagErr = errors.New("gateway timeout")

	finding := rig.evaluate(t, issue)
	if finding.Verdict != ReadinessUndecidable {
		t.Fatalf("verdict = %s, want undecidable", finding.Verdict)
	}
}

// =============================================================================
// Reports
// =============================================================================

func TestReadinessReport_CountAndSorted(t *testing.T) {
	t.Parallel()
	report := ReadinessReport{Findings: []ReadinessFinding{
		{IssueNumber: 9, Verdict: ReadinessAdopt},
		{IssueNumber: 3, Verdict: ReadinessPark},
		{IssueNumber: 5, Verdict: ReadinessAdopt},
	}}
	if got := report.Count(ReadinessAdopt); got != 2 {
		t.Fatalf("Count(adopt) = %d, want 2", got)
	}
	if got := report.Count(ReadinessOwned); got != 0 {
		t.Fatalf("Count(owned) = %d, want 0", got)
	}
	sorted := report.Sorted()
	if len(sorted) != 3 || sorted[0].IssueNumber != 3 || sorted[2].IssueNumber != 9 {
		t.Fatalf("Sorted = %+v, want ascending by issue number", sorted)
	}
	// Sorted must not disturb the original, or two calls would disagree.
	if report.Findings[0].IssueNumber != 9 {
		t.Fatal("Sorted must not reorder the report in place")
	}
	empty := ReadinessReport{}
	if got := empty.Count(ReadinessAdopt); got != 0 {
		t.Fatalf("an empty report counts nothing: %d", got)
	}
	if got := empty.Sorted(); len(got) != 0 {
		t.Fatalf("an empty report sorts to nothing: %+v", got)
	}
}

func TestReadinessVerdict_StringAndAdoptable(t *testing.T) {
	t.Parallel()
	adoptable := map[ReadinessVerdict]bool{
		ReadinessAdopt:                true,
		ReadinessPark:                 true,
		ReadinessRefuse:               true,
		ReadinessNotTriggered:         false,
		ReadinessOwned:                false,
		ReadinessNoAcceptanceCriteria: false,
		ReadinessUndecidable:          false,
	}
	for verdict, want := range adoptable {
		if verdict.String() != string(verdict) {
			t.Fatalf("String() of %q = %q", verdict, verdict.String())
		}
		if got := verdict.Adoptable(); got != want {
			t.Fatalf("%s.Adoptable() = %v, want %v", verdict, got, want)
		}
	}
	if ReadinessVerdict("invented").Adoptable() {
		t.Fatal("an unknown verdict must not produce a task")
	}
}

// =============================================================================
// Failures found at depth, not at the root
// =============================================================================

// TestReadiness_AMalformedDeclarationUpstreamIsRefusedToo: the walk reads each
// unsatisfied dependency's OWN declarations, so a directive nobody can parse two
// links up is as unactionable as one in the root issue.
func TestReadiness_AMalformedDeclarationUpstreamIsRefusedToo(t *testing.T) {
	t.Parallel()
	rig := newReadinessRig(t)
	root := rig.issue(1, "Blocked by: #2\n")
	upstream := rig.forge.putIssue(2, "upstream")
	upstream.Body = "Blocked by: not-a-reference\n"

	finding := rig.evaluate(t, root)
	if finding.Verdict != ReadinessRefuse {
		t.Fatalf("verdict = %s, want refuse (%q)", finding.Verdict, finding.Evidence)
	}
	if finding.ParkKind() != orchestration.TaskBlockKindNeedsInfo {
		t.Fatalf("ParkKind = %s, want needs_info", finding.ParkKind())
	}
}

// TestReadiness_AnUpstreamThatCannotBeReadIsUndecidable: a read failure on a
// dependency is undecidable for THIS issue rather than a refusal a person has to
// look at, and rather than a panic.
//
// The specific shape is an adapter answering neither a record nor an error. The
// walk reaches issues in OTHER repositories through whatever adapter that forge
// has, so an answer of nothing is reachable in a way it was not when only the
// sweep read the project's own issues.
func TestReadiness_AnUpstreamThatCannotBeReadIsUndecidable(t *testing.T) {
	t.Parallel()
	rig := newReadinessRig(t)
	root := rig.issue(1, "Blocked by: #2\n")
	rig.forge.putIssue(2, "upstream")
	rig.forge.getIssueNil = true

	finding := rig.evaluate(t, root)
	if finding.Verdict != ReadinessUndecidable {
		t.Fatalf("verdict = %s, want undecidable (%q)", finding.Verdict, finding.Evidence)
	}

	// And an ordinary read failure is the same class.
	rig.forge.getIssueNil = false
	rig.forge.getIssueErr = errors.New("gateway timeout")
	if got := rig.evaluate(t, root).Verdict; got != ReadinessUndecidable {
		t.Fatalf("verdict = %s, want undecidable", got)
	}
}

// TestReadiness_AnUpstreamDiscussionThatCannotBeReadIsAnOutage: the descent reads
// the upstream's comments too, and a backend failure there is the forge being
// down rather than one issue's problem.
func TestReadiness_AnUpstreamDiscussionThatCannotBeReadIsAnOutage(t *testing.T) {
	t.Parallel()
	rig := newReadinessRig(t)
	root := rig.issue(1, "Blocked by: #2\n")
	rig.forge.putIssue(2, "upstream")
	rig.forge.listCommentsErr = backendError("ListIssueComments", errors.New("503"))

	if _, err := rig.evaluator.Evaluate(context.Background(), rig.scope(), root); !errors.Is(err, ErrBackend) {
		t.Fatalf("a backend outage at depth must still be returned as one: %v", err)
	}
}

// TestDependencyWalk_RefusesAKindNothingDecides is a guard test on the walk's own
// dispatch. Every kind a parsed Dependency can produce has a check, so this is
// unreachable through ParseDependencies today — which is exactly why it is worth
// pinning: a dependency kind added without a checker must stop the walk rather
// than be read as satisfied, and the permissive default is the one that would
// release work on a condition nobody evaluated.
func TestDependencyWalk_RefusesAKindNothingDecides(t *testing.T) {
	t.Parallel()
	rig := newReadinessRig(t)
	walk := &dependencyWalk{evaluator: rig.evaluator, scope: rig.scope(), depth: 4}

	_, err := walk.satisfied(context.Background(), Dependency{
		Kind: DependencyKind("invented"), Repo: depRepo, Issue: 1,
	})
	// An invented kind renders as an issue condition whose subject is well
	// formed, so what refuses it is the KIND registry rather than the fields.
	if err == nil {
		t.Fatal("a dependency kind nothing decides must stop the walk, not read as satisfied")
	}

	// A condition whose fields the vocabulary refuses is stopped by validation.
	_, err = walk.satisfied(context.Background(), Dependency{
		Kind: DependencyReleaseTag, Repo: depRepo,
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("a release dependency with no tag: want ErrInvalidInput, got %v", err)
	}
}
