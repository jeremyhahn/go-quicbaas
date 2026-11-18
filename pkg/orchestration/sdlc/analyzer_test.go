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
	"errors"
	"reflect"
	"testing"
)

// =============================================================================
// The portfolio fixture
// =============================================================================
//
// The shape is the real one, because the requirement was measured on it:
// go-objstore had blast radius 6 and was holding six repos while go-luks2
// (blast 1) drew the attention (doc 20 §8). A synthetic two-node graph would
// not distinguish transitive dependents from direct ones, which is the whole
// point of the computation.

// portfolio returns manifests modelling the layered dependency graph: a
// foundational library, the database above it, the composer above that, and the
// platform on top, plus a leaf nobody depends on.
func portfolio() []DependencyManifest {
	return []DependencyManifest{
		{ProjectID: "go-objstore", Module: "example.com/go-objstore"},
		{
			ProjectID: "go-qrdb",
			Module:    "example.com/go-qrdb",
			Requires:  []string{"example.com/go-objstore"},
		},
		{
			ProjectID: "go-quicbaas",
			Module:    "example.com/go-quicbaas",
			Requires:  []string{"example.com/go-qrdb"},
		},
		{
			ProjectID: "go-trusted-platform",
			Module:    "example.com/go-trusted-platform",
			Requires:  []string{"example.com/go-quicbaas"},
		},
		{ProjectID: "go-luks2", Module: "example.com/go-luks2"},
	}
}

// =============================================================================
// gomod
// =============================================================================

// TestGoModAnalyzer_BlastRadiusIsTransitive is the measured requirement: a
// change to a foundational library matters as much as everything that reaches
// it, not as much as what imports it directly.
//
// go-objstore has ONE direct dependent (go-qrdb) and THREE transitive ones. An
// analyzer that counted direct edges would rank it equal to a leaf's parent,
// which is the arrival-ordered dispatch this replaces.
func TestGoModAnalyzer_BlastRadiusIsTransitive(t *testing.T) {
	t.Parallel()

	analyzer, err := LookupDependencyAnalyzer(GoModAnalyzerName)
	if err != nil {
		t.Fatalf("LookupDependencyAnalyzer: %v", err)
	}

	snapshot, err := analyzer.Analyze("go-objstore", portfolio())
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	want := []string{"go-qrdb", "go-quicbaas", "go-trusted-platform"}
	if !reflect.DeepEqual(snapshot.Dependents, want) {
		t.Errorf("dependents = %v, want %v", snapshot.Dependents, want)
	}
	if snapshot.BlastRadius() != len(want) {
		t.Errorf("blast radius = %d, want %d", snapshot.BlastRadius(), len(want))
	}
	if snapshot.Analyzer != GoModAnalyzerName {
		t.Errorf("snapshot names analyzer %q, want %q", snapshot.Analyzer, GoModAnalyzerName)
	}
	if snapshot.ProjectID != "go-objstore" {
		t.Errorf("snapshot names project %q, want go-objstore", snapshot.ProjectID)
	}
}

// TestGoModAnalyzer_LeafHasNoDependents proves the low end of the scale is a
// real answer rather than a missing one: go-luks2 is analysable and its blast
// radius is zero, which is different from "no analyzer could speak for it".
func TestGoModAnalyzer_LeafHasNoDependents(t *testing.T) {
	t.Parallel()

	analyzer := mustAnalyzer(t, GoModAnalyzerName)
	snapshot, err := analyzer.Analyze("go-luks2", portfolio())
	if err != nil {
		t.Fatalf("a manifest-bearing leaf must analyse, not refuse: %v", err)
	}
	if snapshot.BlastRadius() != 0 {
		t.Errorf("blast radius = %d, want 0", snapshot.BlastRadius())
	}
}

// TestGoModAnalyzer_SelfIsNeverItsOwnDependent guards the direction of the
// reversed edge. A project that counted itself would inflate every blast radius
// by one, which is invisible in a ranking because it shifts every item equally
// — until a project with no dependents outranks one with none, both reading 1.
func TestGoModAnalyzer_SelfIsNeverItsOwnDependent(t *testing.T) {
	t.Parallel()

	manifests := []DependencyManifest{
		{
			ProjectID: "self-requiring",
			Module:    "example.com/self",
			Requires:  []string{"example.com/self"},
		},
	}
	snapshot, err := mustAnalyzer(t, GoModAnalyzerName).Analyze("self-requiring", manifests)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if snapshot.BlastRadius() != 0 {
		t.Errorf("a project counted itself as its own dependent: %v", snapshot.Dependents)
	}
}

// TestGoModAnalyzer_CycleTerminates proves a dependency cycle is walked once,
// not forever.
//
// Go modules cannot contain an import cycle, but MANIFESTS can: they are read
// from whatever the source supplies, and a mis-declared or partially-updated
// pair produces one. A traversal that trusted the graph to be acyclic would
// hang the dispatch sweep, which fails the module closed in the worst possible
// way — no error, no dispatch, no symptom.
func TestGoModAnalyzer_CycleTerminates(t *testing.T) {
	t.Parallel()

	manifests := []DependencyManifest{
		{ProjectID: "a", Module: "example.com/a", Requires: []string{"example.com/b"}},
		{ProjectID: "b", Module: "example.com/b", Requires: []string{"example.com/a"}},
	}
	snapshot, err := mustAnalyzer(t, GoModAnalyzerName).Analyze("a", manifests)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if !reflect.DeepEqual(snapshot.Dependents, []string{"b"}) {
		t.Errorf("dependents = %v, want [b]", snapshot.Dependents)
	}
}

// TestGoModAnalyzer_ProjectWithoutAManifestDoesNotMatch is the seam between
// "this analyzer has no opinion" and "this project has no dependents".
//
// The two are not interchangeable. A project with no go.mod is one the gomod
// analyzer cannot speak for, and reporting it as blast radius zero would be an
// assertion the analyzer has no evidence for — it would rank a mixed-stack
// project as a leaf forever. ErrAnalyzerNoMatch is what lets the priority layer
// fall to a neutral baseline instead.
func TestGoModAnalyzer_ProjectWithoutAManifestDoesNotMatch(t *testing.T) {
	t.Parallel()

	snapshot, err := mustAnalyzer(t, GoModAnalyzerName).Analyze("xkey-android", portfolio())
	if !errors.Is(err, ErrAnalyzerNoMatch) {
		t.Fatalf("expected ErrAnalyzerNoMatch, got %T: %v", err, err)
	}
	if snapshot != nil {
		t.Errorf("a no-match must return no snapshot, got %+v", snapshot)
	}
}

// TestGoModAnalyzer_ManifestWithoutAModulePathDoesNotMatch covers the project
// that HAS a manifest but no module path — a declared-only entry sharing the
// source with go modules. It is the same distinction as the missing manifest
// and must not be answered as a leaf.
func TestGoModAnalyzer_ManifestWithoutAModulePathDoesNotMatch(t *testing.T) {
	t.Parallel()

	manifests := []DependencyManifest{{ProjectID: "website", Dependents: []string{"x"}}}
	_, err := mustAnalyzer(t, GoModAnalyzerName).Analyze("website", manifests)
	if !errors.Is(err, ErrAnalyzerNoMatch) {
		t.Fatalf("expected ErrAnalyzerNoMatch, got %T: %v", err, err)
	}
}

// TestGoModAnalyzer_RequiresOutsideThePortfolioAreIgnored proves a third-party
// require does not invent a project. A manifest requires dozens of modules
// nobody in the portfolio publishes, and each one that became a graph node
// would be a project the dispatcher believes exists.
func TestGoModAnalyzer_RequiresOutsideThePortfolioAreIgnored(t *testing.T) {
	t.Parallel()

	manifests := []DependencyManifest{
		{ProjectID: "lib", Module: "example.com/lib"},
		{
			ProjectID: "app",
			Module:    "example.com/app",
			Requires:  []string{"example.com/lib", "github.com/third/party"},
		},
	}
	snapshot, err := mustAnalyzer(t, GoModAnalyzerName).Analyze("lib", manifests)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if !reflect.DeepEqual(snapshot.Dependents, []string{"app"}) {
		t.Errorf("dependents = %v, want [app]", snapshot.Dependents)
	}
}

// =============================================================================
// declared
// =============================================================================

// TestDeclaredAnalyzer_ReadsTheExplicitList covers the ecosystem-agnostic path:
// a project with no manifest the module can parse states its dependents, and
// they are taken as stated.
func TestDeclaredAnalyzer_ReadsTheExplicitList(t *testing.T) {
	t.Parallel()

	manifests := []DependencyManifest{
		{ProjectID: "xkey-android", Dependents: []string{"go-xkms", "xkey"}},
	}
	snapshot, err := mustAnalyzer(t, DeclaredAnalyzerName).Analyze("xkey-android", manifests)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	// Sorted, because a ranking derived from this must not depend on the order
	// somebody typed the list in.
	if !reflect.DeepEqual(snapshot.Dependents, []string{"go-xkms", "xkey"}) {
		t.Errorf("dependents = %v, want [go-xkms xkey]", snapshot.Dependents)
	}
	if snapshot.Analyzer != DeclaredAnalyzerName {
		t.Errorf("snapshot names analyzer %q, want %q", snapshot.Analyzer, DeclaredAnalyzerName)
	}
}

// TestDeclaredAnalyzer_DeduplicatesAndDropsSelf proves the declared list is
// normalised rather than trusted verbatim. A hand-maintained list repeats
// entries and names its own project; both would inflate the blast radius, and
// an inflated radius outranks a real one.
func TestDeclaredAnalyzer_DeduplicatesAndDropsSelf(t *testing.T) {
	t.Parallel()

	manifests := []DependencyManifest{
		{ProjectID: "p", Dependents: []string{"b", "a", "b", "p", ""}},
	}
	snapshot, err := mustAnalyzer(t, DeclaredAnalyzerName).Analyze("p", manifests)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if !reflect.DeepEqual(snapshot.Dependents, []string{"a", "b"}) {
		t.Errorf("dependents = %v, want [a b]", snapshot.Dependents)
	}
}

// TestDeclaredAnalyzer_EmptyListDoesNotMatch keeps the same distinction the
// gomod analyzer draws: a project that declared nothing has not declared zero.
func TestDeclaredAnalyzer_EmptyListDoesNotMatch(t *testing.T) {
	t.Parallel()

	manifests := []DependencyManifest{{ProjectID: "p", Module: "example.com/p"}}
	_, err := mustAnalyzer(t, DeclaredAnalyzerName).Analyze("p", manifests)
	if !errors.Is(err, ErrAnalyzerNoMatch) {
		t.Fatalf("expected ErrAnalyzerNoMatch, got %T: %v", err, err)
	}
}

// =============================================================================
// The registry
// =============================================================================

// TestDependencyAnalyzerRegistry_ShipsBothAnalyzers proves the build registers
// the two complete analyzers and nothing else. A name appearing here that has
// no working implementation is the framework-with-no-adapters shape the
// completeness rule exists to prevent.
func TestDependencyAnalyzerRegistry_ShipsBothAnalyzers(t *testing.T) {
	t.Parallel()

	got := RegisteredDependencyAnalyzers()
	want := []string{DeclaredAnalyzerName, GoModAnalyzerName}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("registered analyzers = %v, want %v", got, want)
	}
}

// TestLookupDependencyAnalyzer_UnknownNameFailsClosed proves selection does not
// substitute a default.
//
// A project pinned to an analyzer that is not present must fail loudly. Falling
// back to another analyzer would rank that project's queue by a graph nobody
// chose, and the ranking would look entirely normal.
func TestLookupDependencyAnalyzer_UnknownNameFailsClosed(t *testing.T) {
	t.Parallel()

	analyzer, err := LookupDependencyAnalyzer("cargo")
	if !errors.Is(err, ErrAnalyzerNotRegistered) {
		t.Fatalf("expected ErrAnalyzerNotRegistered, got %T: %v", err, err)
	}
	if analyzer != nil {
		t.Errorf("a failed lookup returned an analyzer: %T", analyzer)
	}

	var notRegistered *AnalyzerNotRegisteredError
	if !errors.As(err, &notRegistered) {
		t.Fatalf("error does not carry the analyzer name: %T", err)
	}
	if notRegistered.Name != "cargo" {
		t.Errorf("error names %q, want cargo", notRegistered.Name)
	}
}

// =============================================================================
// Blast radius across the registry
// =============================================================================

// TestBlastRadius_PrefersTheAnalyzerThatMatches proves the layered lookup: the
// gomod analyzer answers for a Go project and the declared analyzer answers for
// one with no module path, from a single mixed manifest set.
func TestBlastRadius_PrefersTheAnalyzerThatMatches(t *testing.T) {
	t.Parallel()

	manifests := []DependencyManifest{
		{ProjectID: "go-objstore", Module: "example.com/go-objstore"},
		{ProjectID: "go-qrdb", Module: "example.com/go-qrdb", Requires: []string{"example.com/go-objstore"}},
		{ProjectID: "website", Dependents: []string{"a", "b"}},
	}

	goSnapshot, err := BlastRadius("go-objstore", manifests)
	if err != nil {
		t.Fatalf("BlastRadius(go-objstore): %v", err)
	}
	if goSnapshot.Analyzer != GoModAnalyzerName {
		t.Errorf("go project answered by %q, want %q", goSnapshot.Analyzer, GoModAnalyzerName)
	}
	if goSnapshot.BlastRadius() != 1 {
		t.Errorf("go-objstore blast radius = %d, want 1", goSnapshot.BlastRadius())
	}

	declaredSnapshot, err := BlastRadius("website", manifests)
	if err != nil {
		t.Fatalf("BlastRadius(website): %v", err)
	}
	if declaredSnapshot.Analyzer != DeclaredAnalyzerName {
		t.Errorf("manifest-less project answered by %q, want %q",
			declaredSnapshot.Analyzer, DeclaredAnalyzerName)
	}
	if declaredSnapshot.BlastRadius() != 2 {
		t.Errorf("website blast radius = %d, want 2", declaredSnapshot.BlastRadius())
	}
}

// TestBlastRadius_NoAnalyzerMatchIsNeutralNotAnError is the starvation guard,
// and it is the reason this returns a snapshot rather than an error.
//
// A project no analyzer speaks for must still be dispatchable. The plan states
// it directly — "never an error, never starved" — and the failure it prevents
// is a whole repository silently dropping out of the queue because nobody
// taught the module to read its manifest format.
func TestBlastRadius_NoAnalyzerMatchIsNeutralNotAnError(t *testing.T) {
	t.Parallel()

	snapshot, err := BlastRadius("unknown-project", portfolio())
	if err != nil {
		t.Fatalf("an unmatched project must not error: %v", err)
	}
	if snapshot == nil {
		t.Fatal("an unmatched project must still get a snapshot")
	}
	if snapshot.BlastRadius() != 0 {
		t.Errorf("neutral blast radius = %d, want 0", snapshot.BlastRadius())
	}
	if snapshot.Analyzer != NeutralAnalyzerName {
		t.Errorf("neutral snapshot names %q, want %q", snapshot.Analyzer, NeutralAnalyzerName)
	}
}

// TestBlastRadiusWith_PinnedAnalyzerIsNotSubstituted proves a project that
// pinned an analyzer gets that analyzer or a neutral baseline — never a
// different one's answer.
//
// Substituting would be the silent-fallback shape: a project pinned to
// "declared" because its go.mod is deliberately not the source of truth would
// be ranked from that go.mod anyway, and the ranking would look correct.
func TestBlastRadiusWith_PinnedAnalyzerIsNotSubstituted(t *testing.T) {
	t.Parallel()

	// The project has a module path, so gomod WOULD match it. It pinned
	// declared, and declared has nothing to say.
	manifests := []DependencyManifest{
		{ProjectID: "go-qrdb", Module: "example.com/go-qrdb"},
		{ProjectID: "go-quicbaas", Module: "example.com/go-quicbaas", Requires: []string{"example.com/go-qrdb"}},
	}

	snapshot, err := BlastRadiusWith(DeclaredAnalyzerName, "go-qrdb", manifests)
	if err != nil {
		t.Fatalf("a pinned analyzer that does not match must be neutral, not an error: %v", err)
	}
	if snapshot.Analyzer != NeutralAnalyzerName {
		t.Errorf("pinned lookup answered by %q, want the neutral baseline", snapshot.Analyzer)
	}
	if snapshot.BlastRadius() != 0 {
		t.Errorf("blast radius = %d, want 0 — gomod's answer leaked through the pin",
			snapshot.BlastRadius())
	}
}

// TestBlastRadiusWith_UnregisteredPinFailsClosed proves a pin naming an
// analyzer this build does not have is refused rather than ignored.
func TestBlastRadiusWith_UnregisteredPinFailsClosed(t *testing.T) {
	t.Parallel()

	_, err := BlastRadiusWith("npm", "go-qrdb", portfolio())
	if !errors.Is(err, ErrAnalyzerNotRegistered) {
		t.Fatalf("expected ErrAnalyzerNotRegistered, got %T: %v", err, err)
	}
}

// mustAnalyzer resolves a registered analyzer or fails the test.
func mustAnalyzer(t *testing.T, name string) DependencyAnalyzer {
	t.Helper()
	analyzer, err := LookupDependencyAnalyzer(name)
	if err != nil {
		t.Fatalf("LookupDependencyAnalyzer(%q): %v", name, err)
	}
	return analyzer
}

// TestBlastRadius_DerivedBeatsDeclared is doc 20 §8's rule, asserted on the one
// input that can tell the two apart: a project carrying BOTH a module path and
// a hand-maintained dependents list.
//
// This failed when resolution walked the registry in name order, because
// "declared" sorts before "gomod": the stated list won, the go.mod graph was
// never consulted, and the ranking looked entirely normal while being derived
// from whatever somebody last remembered to type. That is the inversion the
// rule exists to prevent — a hand-maintained ordering drifts from the graph and
// the drift is invisible until it strands something.
func TestBlastRadius_DerivedBeatsDeclared(t *testing.T) {
	t.Parallel()

	manifests := []DependencyManifest{
		{
			ProjectID: "go-objstore",
			Module:    "example.com/go-objstore",
			// Stale by three: the real graph below has one dependent.
			Dependents: []string{"stale-a", "stale-b", "stale-c", "stale-d"},
		},
		{
			ProjectID: "go-qrdb",
			Module:    "example.com/go-qrdb",
			Requires:  []string{"example.com/go-objstore"},
		},
	}

	snapshot, err := BlastRadius("go-objstore", manifests)
	if err != nil {
		t.Fatalf("BlastRadius: %v", err)
	}
	if snapshot.Analyzer != GoModAnalyzerName {
		t.Errorf("answered by %q; the module graph must win over a declared list",
			snapshot.Analyzer)
	}
	if !reflect.DeepEqual(snapshot.Dependents, []string{"go-qrdb"}) {
		t.Errorf("dependents = %v, want [go-qrdb] from the module graph",
			snapshot.Dependents)
	}
}

// TestAnalyzersByPrecedence_PutsEveryDerivedAnalyzerFirst pins the ordering
// itself, so an analyzer added later cannot land in the wrong group unnoticed.
func TestAnalyzersByPrecedence_PutsEveryDerivedAnalyzerFirst(t *testing.T) {
	t.Parallel()

	ordered := analyzersByPrecedence()
	if len(ordered) != len(RegisteredDependencyAnalyzers()) {
		t.Fatalf("precedence covers %d analyzers, %d are registered",
			len(ordered), len(RegisteredDependencyAnalyzers()))
	}

	seenDeclared := false
	for _, analyzer := range ordered {
		if !analyzer.Derived() {
			seenDeclared = true
			continue
		}
		if seenDeclared {
			t.Errorf("derived analyzer %q is asked after a declared one", analyzer.Name())
		}
	}
}

// TestRegisteredDependencyAnalyzers_StaysSortedForDisplay keeps the listing
// stable and separate from the precedence order.
//
// The two answer different questions — "what does this build have" and "which
// should be asked first" — and collapsing them is what caused the inversion
// above.
func TestRegisteredDependencyAnalyzers_StaysSortedForDisplay(t *testing.T) {
	t.Parallel()

	got := RegisteredDependencyAnalyzers()
	if !reflect.DeepEqual(got, []string{DeclaredAnalyzerName, GoModAnalyzerName}) {
		t.Errorf("listing = %v, want it sorted by name", got)
	}
	if first := analyzersByPrecedence()[0].Name(); first == got[0] {
		t.Errorf("precedence order starts with %q, the same as the sorted listing; "+
			"the two orders are being conflated", first)
	}
}

// TestDeclaredAnalyzer_IsTransitive holds the declared analyzer to the same
// promise Analyze makes for every implementation.
//
// Reading only the project's own row would rank a change by its first hop, so a
// project whose dependents have dependents of their own is ranked lower than it
// reaches — and those are precisely the mixed-stack projects this analyzer
// exists to serve, so the under-prioritisation would be systematic rather than
// occasional.
func TestDeclaredAnalyzer_IsTransitive(t *testing.T) {
	t.Parallel()

	manifests := []DependencyManifest{
		{ProjectID: "a", Dependents: []string{"b"}},
		{ProjectID: "b", Dependents: []string{"c"}},
		{ProjectID: "c", Dependents: []string{"d"}},
	}
	snapshot, err := mustAnalyzer(t, DeclaredAnalyzerName).Analyze("a", manifests)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if !reflect.DeepEqual(snapshot.Dependents, []string{"b", "c", "d"}) {
		t.Errorf("dependents = %v, want [b c d]; the walk stopped at the first hop",
			snapshot.Dependents)
	}
}

// TestDeclaredAnalyzer_CycleTerminates proves the declared walk is cycle-safe,
// which matters more here than for go modules: a declared list is typed by hand
// and nothing validates it.
func TestDeclaredAnalyzer_CycleTerminates(t *testing.T) {
	t.Parallel()

	manifests := []DependencyManifest{
		{ProjectID: "a", Dependents: []string{"b"}},
		{ProjectID: "b", Dependents: []string{"a"}},
	}
	snapshot, err := mustAnalyzer(t, DeclaredAnalyzerName).Analyze("a", manifests)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if !reflect.DeepEqual(snapshot.Dependents, []string{"b"}) {
		t.Errorf("dependents = %v, want [b]", snapshot.Dependents)
	}
}
