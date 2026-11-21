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
	"errors"
	"strings"
	"testing"
)

// depRepo is the declaring issue's own repository, against which a bare "#N"
// resolves.
var depRepo = RepoRef{Owner: "acme", Repo: "api"}

// =============================================================================
// Parsing — what an issue says it is waiting for
// =============================================================================

func TestParseDependencies_ReadsEveryDirectiveSpelling(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		body string
		want []Dependency
	}{
		{
			name: "a bare number resolves against the declaring repository",
			body: "Blocked by: #41",
			want: []Dependency{{Kind: DependencyIssueClosed, Repo: depRepo, Issue: 41}},
		},
		{
			name: "a full reference names another repository",
			body: "Blocked by: other/lib#7",
			want: []Dependency{{Kind: DependencyIssueClosed, Repo: RepoRef{Owner: "other", Repo: "lib"}, Issue: 7}},
		},
		{
			name: "the hyphenated spelling",
			body: "Blocked-by: #41",
			want: []Dependency{{Kind: DependencyIssueClosed, Repo: depRepo, Issue: 41}},
		},
		{
			name: "depends on",
			body: "Depends on: #41",
			want: []Dependency{{Kind: DependencyIssueClosed, Repo: depRepo, Issue: 41}},
		},
		{
			name: "depends-on",
			body: "Depends-on: #41",
			want: []Dependency{{Kind: DependencyIssueClosed, Repo: depRepo, Issue: 41}},
		},
		{
			name: "case is irrelevant to the directive",
			body: "BLOCKED BY: #41",
			want: []Dependency{{Kind: DependencyIssueClosed, Repo: depRepo, Issue: 41}},
		},
		{
			name: "a comma-separated list",
			body: "Blocked by: #41, other/lib#7",
			want: []Dependency{
				{Kind: DependencyIssueClosed, Repo: depRepo, Issue: 41},
				{Kind: DependencyIssueClosed, Repo: RepoRef{Owner: "other", Repo: "lib"}, Issue: 7},
			},
		},
		{
			name: "a space-separated list",
			body: "Blocked by: #41 #42",
			want: []Dependency{
				{Kind: DependencyIssueClosed, Repo: depRepo, Issue: 41},
				{Kind: DependencyIssueClosed, Repo: depRepo, Issue: 42},
			},
		},
		{
			name: "inside a markdown list item",
			body: "Ordering:\n- Blocked by: #41\n- Blocked by: #42\n",
			want: []Dependency{
				{Kind: DependencyIssueClosed, Repo: depRepo, Issue: 41},
				{Kind: DependencyIssueClosed, Repo: depRepo, Issue: 42},
			},
		},
		{
			name: "emphasised",
			body: "**Blocked by:** #41",
			want: []Dependency{{Kind: DependencyIssueClosed, Repo: depRepo, Issue: 41}},
		},
		{
			name: "a release dependency",
			body: "Requires release: other/lib@v0.9.5-alpha",
			want: []Dependency{{Kind: DependencyReleaseTag, Repo: RepoRef{Owner: "other", Repo: "lib"}, Tag: "v0.9.5-alpha"}},
		},
		{
			name: "requires-release",
			body: "Requires-release: other/lib@v1.0.0",
			want: []Dependency{{Kind: DependencyReleaseTag, Repo: RepoRef{Owner: "other", Repo: "lib"}, Tag: "v1.0.0"}},
		},
		{
			name: "both kinds together, because an issue may need both",
			body: "Blocked by: other/lib#7\nRequires release: other/lib@v1.0.0\n",
			want: []Dependency{
				{Kind: DependencyIssueClosed, Repo: RepoRef{Owner: "other", Repo: "lib"}, Issue: 7},
				{Kind: DependencyReleaseTag, Repo: RepoRef{Owner: "other", Repo: "lib"}, Tag: "v1.0.0"},
			},
		},
		{
			name: "an issue declaring nothing",
			body: "Just some prose about the work.\n\n## Verification\n- make test\n",
			want: nil,
		},
		{
			name: "prose that merely begins with the words is not a directive",
			body: "Blocked by the missing config on that host, which we fixed.",
			want: nil,
		},
		{
			name: "an empty body",
			body: "",
			want: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseDependencies(depRepo, tc.body, nil)
			if err != nil {
				t.Fatalf("ParseDependencies: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("ParseDependencies = %+v, want %+v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("ParseDependencies[%d] = %+v, want %+v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// TestParseDependencies_ReadsTheDiscussionToo: a dependency found after filing
// is written as a comment, which is the only place somebody without edit rights
// can write it.
func TestParseDependencies_ReadsTheDiscussionToo(t *testing.T) {
	t.Parallel()
	got, err := ParseDependencies(depRepo, "Blocked by: #1", []ForgeComment{
		{Body: "Reviewing this, it also needs the upstream change.\n\nBlocked by: #2\n", Author: "alice"},
		{Body: "no directive here"},
	})
	if err != nil {
		t.Fatalf("ParseDependencies: %v", err)
	}
	if len(got) != 2 || got[0].Issue != 1 || got[1].Issue != 2 {
		t.Fatalf("ParseDependencies = %+v, want the body's and the comment's", got)
	}
}

// TestParseDependencies_ADirectiveMustLeadItsLine, and the reason is negation: a
// mid-sentence match would read "this is NOT blocked by: #9" as a dependency on
// #9, and nothing downstream could recover the sense. A directive is a
// statement, so it gets its own line.
func TestParseDependencies_ADirectiveMustLeadItsLine(t *testing.T) {
	t.Parallel()
	for _, body := range []string{
		"This is not blocked by: #9",
		"We thought it was blocked by: #9 but it was the runner",
	} {
		got, err := ParseDependencies(depRepo, body, nil)
		if err != nil {
			t.Fatalf("%q: %v", body, err)
		}
		if len(got) != 0 {
			t.Fatalf("%q was read as declaring %+v", body, got)
		}
	}
}

// TestParseDependencies_DuplicatesCollapse: the same dependency declared twice
// is one dependency, or it would be checked and reported twice per sweep.
func TestParseDependencies_DuplicatesCollapse(t *testing.T) {
	t.Parallel()
	got, err := ParseDependencies(depRepo, "Blocked by: #1, #1\nBlocked by: acme/api#1\n",
		[]ForgeComment{{Body: "Blocked by: #1"}})
	if err != nil {
		t.Fatalf("ParseDependencies: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("ParseDependencies = %+v, want one dependency", got)
	}
}

// TestParseDependencies_TheSameIssueAndTagAreTwoDependencies: the whole point of
// the two kinds is that a closed issue and a published tag are different facts.
func TestParseDependencies_TheSameIssueAndTagAreTwoDependencies(t *testing.T) {
	t.Parallel()
	got, err := ParseDependencies(depRepo, "Blocked by: other/lib#7\nRequires release: other/lib@v1.0.0\n", nil)
	if err != nil {
		t.Fatalf("ParseDependencies: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ParseDependencies = %+v, want both kinds kept", got)
	}
}

// TestParseDependencies_RefusesAMalformedDeclaration is the half that earns its
// keep: a directive the parser cannot read must not be dropped, because work
// proceeding as though it had no dependencies is indistinguishable from work
// that genuinely has none.
func TestParseDependencies_RefusesAMalformedDeclaration(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"no operand at all":                  "Blocked by:",
		"an issue number that is not one":    "Blocked by: #abc",
		"a zero issue number":                "Blocked by: #0",
		"a negative issue number":            "Blocked by: #-4",
		"a reference with no repository":     "Blocked by: api#4",
		"a reference with no owner":          "Blocked by: /api#4",
		"a reference with no name":           "Blocked by: acme/#4",
		"a release with no operand":          "Requires release:",
		"a release with no tag":              "Requires release: other/lib@",
		"a release with no at sign":          "Requires release: other/lib",
		"a release with no repository":       "Requires release: @v1.0.0",
		"a release with an unowned repo":     "Requires release: lib@v1.0.0",
		"a release tag with a reserved char": "Requires release: other/lib@v1.0.0[x]",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := ParseDependencies(depRepo, body, nil)
			if err == nil {
				t.Fatalf("%q was accepted as %+v; a declaration nothing can check must be refused", body, got)
			}
			if !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("want ErrInvalidInput, got %v", err)
			}
		})
	}
}

// TestParseDependencies_RefusesAParseWithNoDeclaringRepository: a bare "#N"
// resolves against the declaring issue's repository, so the parse has to know it.
func TestParseDependencies_RefusesAParseWithNoDeclaringRepository(t *testing.T) {
	t.Parallel()
	for _, repo := range []RepoRef{{}, {Owner: "acme"}, {Repo: "api"}} {
		if _, err := ParseDependencies(repo, "Blocked by: #1", nil); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("repo %+v: want ErrInvalidInput, got %v", repo, err)
		}
	}
}

// TestParseDependencies_PreservesTagCase: a tag's case is part of its identity,
// and folding it would ask the forge for a tag that does not exist.
func TestParseDependencies_PreservesTagCase(t *testing.T) {
	t.Parallel()
	got, err := ParseDependencies(depRepo, "Requires release: other/Lib@V1.2.3-RC1", nil)
	if err != nil {
		t.Fatalf("ParseDependencies: %v", err)
	}
	if len(got) != 1 || got[0].Tag != "V1.2.3-RC1" || got[0].Repo.Repo != "Lib" {
		t.Fatalf("ParseDependencies = %+v, want the case preserved", got)
	}
}

// =============================================================================
// Rendering
// =============================================================================

func TestDependency_RefAndIssueRefAndCondition(t *testing.T) {
	t.Parallel()
	issue := Dependency{Kind: DependencyIssueClosed, Repo: depRepo, Issue: 41}
	release := Dependency{Kind: DependencyReleaseTag, Repo: depRepo, Tag: "v1.2.3"}

	if got := issue.Ref(); got != "acme/api#41" {
		t.Fatalf("issue Ref = %q", got)
	}
	if got := release.Ref(); got != "acme/api@v1.2.3" {
		t.Fatalf("release Ref = %q", got)
	}
	if got := issue.IssueRef(); got != "acme/api#41" {
		t.Fatalf("issue IssueRef = %q", got)
	}
	// A RELEASE IS A LEAF: a tag declares nothing, so the graph walk must not
	// try to descend into it.
	if got := release.IssueRef(); got != "" {
		t.Fatalf("release IssueRef = %q, want empty so the walk treats it as a leaf", got)
	}

	issueCondition := issue.Condition()
	if issueCondition.Kind != UnblockForgeIssueState || issueCondition.Subject != "acme/api#41" || issueCondition.Target != "closed" {
		t.Fatalf("issue Condition = %+v", issueCondition)
	}
	releaseCondition := release.Condition()
	if releaseCondition.Kind != UnblockReleaseTag || releaseCondition.Subject != "acme/api" || releaseCondition.Target != "v1.2.3" {
		t.Fatalf("release Condition = %+v", releaseCondition)
	}
	// Both must be storable, or a park could not record them.
	for _, condition := range []UnblockCondition{issueCondition, releaseCondition} {
		if err := condition.Validate(); err != nil {
			t.Fatalf("condition %+v is not storable: %v", condition, err)
		}
		if _, err := condition.Encode(); err != nil {
			t.Fatalf("condition %+v does not encode: %v", condition, err)
		}
	}
	if !strings.Contains(issue.Describe(), "acme/api#41") {
		t.Fatalf("issue Describe = %q", issue.Describe())
	}
	if !strings.Contains(release.Describe(), "v1.2.3") {
		t.Fatalf("release Describe = %q", release.Describe())
	}
}

func TestDependencyKind_String(t *testing.T) {
	t.Parallel()
	for _, kind := range []DependencyKind{DependencyIssueClosed, DependencyReleaseTag} {
		if kind.String() != string(kind) {
			t.Fatalf("String() of %q = %q", kind, kind.String())
		}
	}
	if DependencyKind("invented").String() != "invented" {
		t.Fatal("an unknown kind must render as itself")
	}
}

// TestDependencyDirectives lists what the parser accepts, and asserts every
// listed directive actually parses — a list that drifted from the map would
// tell an operator to write something the parser ignores.
func TestDependencyDirectives(t *testing.T) {
	t.Parallel()
	directives := DependencyDirectives()
	if len(directives) != len(dependencyDirectives) {
		t.Fatalf("DependencyDirectives returned %d of %d", len(directives), len(dependencyDirectives))
	}
	for i := 1; i < len(directives); i++ {
		if directives[i-1] >= directives[i] {
			t.Fatalf("DependencyDirectives must be sorted: %v", directives)
		}
	}
	for _, directive := range directives {
		operand := "#1"
		if strings.Contains(directive, "release") {
			operand = "other/lib@v1.0.0"
		}
		got, err := ParseDependencies(depRepo, directive+" "+operand, nil)
		if err != nil {
			t.Fatalf("the listed directive %q does not parse: %v", directive, err)
		}
		if len(got) != 1 {
			t.Fatalf("the listed directive %q parsed to %+v", directive, got)
		}
	}
}

// =============================================================================
// Errors
// =============================================================================

func TestDependencyCycleError(t *testing.T) {
	t.Parallel()
	err := &DependencyCycleError{Chain: []string{"acme/api#1", "acme/api#2", "acme/api#1"}}
	if !errors.Is(err, ErrDependencyCycle) {
		t.Fatal("must unwrap to ErrDependencyCycle")
	}
	// THE WHOLE CHAIN, not just the reference it closed on: a person handed one
	// reference has to reconstruct the path across however many repositories it
	// crosses.
	for _, ref := range err.Chain {
		if !strings.Contains(err.Error(), ref) {
			t.Fatalf("the error must name every link: %q", err.Error())
		}
	}
	if !strings.Contains(err.Error(), "->") {
		t.Fatalf("the error must render the chain as a path: %q", err.Error())
	}
	empty := &DependencyCycleError{}
	if !errors.Is(empty, ErrDependencyCycle) || empty.Error() == "" {
		t.Fatalf("an empty chain must still be a cycle error: %q", empty.Error())
	}
}

func TestDependencyDepthExceededError(t *testing.T) {
	t.Parallel()
	err := &DependencyDepthExceededError{Chain: []string{"acme/api#1", "acme/api#2"}, Limit: 8}
	if !errors.Is(err, ErrDependencyDepthExceeded) {
		t.Fatal("must unwrap to ErrDependencyDepthExceeded")
	}
	if !strings.Contains(err.Error(), "8") {
		t.Fatalf("the error must name the bound that was exceeded: %q", err.Error())
	}
	if !strings.Contains(err.Error(), "acme/api#2") {
		t.Fatalf("the error must name the path: %q", err.Error())
	}
	empty := &DependencyDepthExceededError{}
	if !errors.Is(empty, ErrDependencyDepthExceeded) || empty.Error() == "" {
		t.Fatalf("an empty chain must still be a depth error: %q", empty.Error())
	}
}

// =============================================================================
// ParseRepoRef
// =============================================================================

func TestParseRepoRef(t *testing.T) {
	t.Parallel()
	got, err := ParseRepoRef("acme/api")
	if err != nil || got != depRepo {
		t.Fatalf("ParseRepoRef = %+v, %v", got, err)
	}
	if got, err := ParseRepoRef("  acme/api  "); err != nil || got != depRepo {
		t.Fatalf("surrounding space must be tolerated: %+v, %v", got, err)
	}
	if got := depRepo.String(); got != "acme/api" {
		t.Fatalf("String/ParseRepoRef must round trip: %q", got)
	}
	for _, bad := range []string{"", "acme", "/api", "acme/", "acme/api/extra", "   "} {
		if _, err := ParseRepoRef(bad); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("ParseRepoRef(%q): want ErrInvalidInput, got %v", bad, err)
		}
	}
}

// TestSplitOperands_DropsWhatIsOnlyPunctuation: markdown emphasis around an
// operand is not part of the reference, and a field that is nothing but emphasis
// is not an operand at all.
func TestSplitOperands_DropsWhatIsOnlyPunctuation(t *testing.T) {
	t.Parallel()
	got, err := ParseDependencies(depRepo, "Blocked by: `#41`, ``, *#42*\n", nil)
	if err != nil {
		t.Fatalf("ParseDependencies: %v", err)
	}
	if len(got) != 2 || got[0].Issue != 41 || got[1].Issue != 42 {
		t.Fatalf("ParseDependencies = %+v, want #41 and #42 with the emphasis stripped", got)
	}
	// A directive whose whole operand list is emphasis states nothing, which is
	// the same as stating nothing at all and is refused as such.
	if _, err := ParseDependencies(depRepo, "Blocked by: ``\n", nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("want ErrInvalidInput, got %v", err)
	}
}
