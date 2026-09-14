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
	"errors"
	"sort"
	"sync"
	"time"
)

// =============================================================================
// The dependency-analysis seam
// =============================================================================
//
// Priority's computed baseline is BLAST RADIUS: how many projects a change to
// this one unblocks. Dispatch was arrival-ordered in the shell scripts, so a
// leaf repo's task could take a slot ahead of one holding six others — measured
// on 2026-08-17, when go-objstore (blast 6) sat red while go-luks2 (blast 1)
// drew the attention (doc 20 §8).
//
// The computation is ECOSYSTEM-SPECIFIC, so it is a seam rather than a
// function. A gomod analyzer cannot see an npm dependency, and a portfolio with
// mixed stacks needs more than one answer. Two rules make the seam real rather
// than aspirational, and they are the same two the strategy registry follows:
//
//   - ONLY COMPLETE ANALYZERS ARE REGISTERED. A half-built analyzer is not
//     registered under a name and left to return "not implemented"; a project
//     that can select it can be broken by it.
//   - SELECTION IS FAIL-CLOSED. A project pinning an analyzer no registry knows
//     fails rather than being ranked by a different analyzer's graph — which
//     would look entirely normal and be wrong.
//
// ANALYZERS ARE PURE, and that is a deliberate departure from what the plan
// sketched. The manifests are supplied by the caller rather than fetched by the
// analyzer, which makes an analyzer a stateless value registrable at init like
// a strategy, makes it testable without a store or a forge, and lets one sweep
// fetch the portfolio's manifests ONCE for every project it ranks instead of
// once per project.
//
// NO MATCH IS NOT AN ERROR, AND IT IS NOT ZERO. An analyzer that cannot speak
// for a project says so (ErrAnalyzerNoMatch) and BlastRadius falls to a neutral
// baseline. Reporting "no dependents" instead would be an assertion nobody has
// evidence for: a mixed-stack project would be ranked a leaf forever. Reporting
// an error would starve it out of the queue entirely, which the plan forbids in
// as many words — "never an error, never starved".

// DependencyManifest is what an analyzer reads about one project.
//
// One type serves every analyzer, so a sweep gathers the portfolio once and
// each analyzer takes the fields it understands. The alternative — a manifest
// type per analyzer — would make the source fetch the portfolio once per
// ecosystem to describe the same repositories.
type DependencyManifest struct {
	// ProjectID is the project this describes. It is the identifier the
	// dependency graph is expressed in, and the one a snapshot names.
	ProjectID string

	// Module is the module path this project publishes, when it publishes one.
	// Read by the gomod analyzer; empty means that analyzer has no opinion.
	Module string

	// Requires are the module paths this project depends on. Paths outside the
	// portfolio are ignored rather than treated as projects — a manifest
	// requires dozens of third-party modules, and each one admitted as a node
	// would be a project the dispatcher believes exists.
	Requires []string

	// Dependents is an explicit list of the projects that depend on this one,
	// for projects with no manifest the module can parse or with a stack it
	// does not analyse. Read by the declared analyzer.
	Dependents []string
}

// ManifestSource supplies the manifests a dispatch sweep ranks by.
//
// It is an interface because where manifests come from is a separate question
// from how they are analysed: a static configuration today, go.mod read at HEAD
// through the forge later. Neither answer belongs inside an analyzer.
type ManifestSource interface {
	// Manifests returns one entry per project the source knows about.
	Manifests(ctx context.Context, tenantID string) ([]DependencyManifest, error)
}

// DependencyAnalyzer derives one project's blast radius from a set of
// manifests.
type DependencyAnalyzer interface {
	// Name is the registry key a project selects this analyzer by.
	Name() string

	// Derived reports whether this analyzer computes the graph from a
	// machine-readable manifest rather than reading a hand-maintained list.
	//
	// It is on the interface because it decides PRECEDENCE, and precedence is
	// doc 20 section 8's rule rather than this package's preference: derived
	// beats declared, for the same reason the module tagger discovers modules
	// instead of listing them — a hand-maintained ordering drifts from the
	// graph and the drift is invisible until it strands something. A project
	// carrying both a module path and a declared list must be ranked from the
	// module path.
	Derived() bool

	// Analyze returns the projects that transitively depend on projectID.
	//
	// A project this analyzer cannot speak for yields ErrAnalyzerNoMatch, which
	// is distinct from a project with no dependents: the first is silence, the
	// second is an answer.
	Analyze(projectID string, manifests []DependencyManifest) (*DependencyGraphSnapshot, error)
}

// =============================================================================
// Errors
// =============================================================================

var (
	// ErrAnalyzerNotRegistered indicates a project pinning an analyzer no
	// registry knows. Fail-closed: ranking that project by a different
	// analyzer's graph would produce a plausible ordering derived from the
	// wrong evidence.
	ErrAnalyzerNotRegistered = errors.New("sdlc: dependency analyzer not registered")

	// ErrAnalyzerNoMatch indicates an analyzer that has nothing to say about a
	// project — no manifest of its kind, or one with no usable field.
	//
	// NOT A FAILURE. It is the signal that the next analyzer should be asked
	// and, if none matches, that the project takes the neutral baseline. A
	// caller that treated it as a failure would starve exactly the projects the
	// neutral baseline exists to protect.
	ErrAnalyzerNoMatch = errors.New("sdlc: analyzer has no dependency data for this project")
)

// AnalyzerNotRegisteredError reports a project pinning an unknown analyzer.
type AnalyzerNotRegisteredError struct {
	Name string
}

// Error implements the error interface.
func (e *AnalyzerNotRegisteredError) Error() string {
	return "sdlc: no dependency analyzer registered under " + e.Name
}

// Unwrap returns the sentinel for errors.Is support.
func (e *AnalyzerNotRegisteredError) Unwrap() error { return ErrAnalyzerNotRegistered }

// AnalyzerNoMatchError reports an analyzer with no data for a project.
type AnalyzerNoMatchError struct {
	Analyzer  string
	ProjectID string
}

// Error implements the error interface.
func (e *AnalyzerNoMatchError) Error() string {
	return "sdlc: analyzer " + e.Analyzer + " has no dependency data for project " + e.ProjectID
}

// Unwrap returns the sentinel for errors.Is support.
func (e *AnalyzerNoMatchError) Unwrap() error { return ErrAnalyzerNoMatch }

// =============================================================================
// Registry
// =============================================================================

// analyzerRegistry holds the registered analyzers.
//
// Map-based dispatch behind a mutex, matching the strategy registries: which
// analyzers exist is decided by which build registered them, and a switch would
// have to name every one at compile time.
var (
	analyzerMu     sync.RWMutex
	analyzerByName = map[string]DependencyAnalyzer{}
)

// RegisterDependencyAnalyzer adds a complete analyzer under its name.
//
// It panics on a duplicate or a nil analyzer for the reason the strategy
// registries do: both are programming errors discovered at init, and two
// analyzers answering to one name means selection is decided by registration
// order.
func RegisterDependencyAnalyzer(a DependencyAnalyzer) {
	if a == nil {
		panic("sdlc: nil dependency analyzer registered")
	}
	analyzerMu.Lock()
	defer analyzerMu.Unlock()
	if _, dup := analyzerByName[a.Name()]; dup {
		panic("sdlc: duplicate dependency analyzer registered: " + a.Name())
	}
	analyzerByName[a.Name()] = a
}

// LookupDependencyAnalyzer resolves an analyzer by name, failing closed on an
// unknown one.
func LookupDependencyAnalyzer(name string) (DependencyAnalyzer, error) {
	analyzerMu.RLock()
	defer analyzerMu.RUnlock()
	a, ok := analyzerByName[name]
	if !ok {
		return nil, &AnalyzerNotRegisteredError{Name: name}
	}
	return a, nil
}

// RegisteredDependencyAnalyzers lists the registered names, sorted.
func RegisteredDependencyAnalyzers() []string {
	analyzerMu.RLock()
	defer analyzerMu.RUnlock()
	names := make([]string, 0, len(analyzerByName))
	for name := range analyzerByName {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// =============================================================================
// Blast radius across the registry
// =============================================================================

// NeutralAnalyzerName is the analyzer named by a snapshot no registered
// analyzer produced.
//
// It is a name rather than an empty string so an operator reading a snapshot
// can tell "nothing could analyse this" from "this has not been analysed". The
// two look identical in a ranking and mean opposite things about whether the
// module needs a new analyzer.
const NeutralAnalyzerName = "neutral"

// BlastRadius computes a project's dependents using whichever registered
// analyzer can speak for it.
//
// Analyzers are tried in registered order and the first match wins. A project
// none of them match gets the neutral baseline — a real snapshot with no
// dependents, named so the fallback is visible — because the alternative is a
// repository silently dropping out of the queue for want of a manifest parser.
func BlastRadius(projectID string, manifests []DependencyManifest) (*DependencyGraphSnapshot, error) {
	for _, analyzer := range analyzersByPrecedence() {
		snapshot, err := analyzer.Analyze(projectID, manifests)
		if errors.Is(err, ErrAnalyzerNoMatch) {
			continue
		}
		if err != nil {
			return nil, err
		}
		return snapshot, nil
	}
	return neutralSnapshot(projectID), nil
}

// analyzersByPrecedence returns the registered analyzers in the order they
// should be asked: every derived analyzer first, then the declared ones.
//
// NOT registration order and NOT name order, both of which were tried and are
// wrong. Name order is actively harmful here — "declared" sorts before "gomod",
// so a project carrying both a module path and a hand-maintained list was
// ranked from the list and its go.mod was never consulted, which inverts doc 20
// section 8's rule exactly. Registration order would work by accident and break
// the first time somebody reordered two init calls.
//
// Within each group the order is by name, so a portfolio with several derived
// analyzers resolves the same way on every node.
func analyzersByPrecedence() []DependencyAnalyzer {
	analyzerMu.RLock()
	defer analyzerMu.RUnlock()

	names := make([]string, 0, len(analyzerByName))
	for name := range analyzerByName {
		names = append(names, name)
	}
	sort.Strings(names)

	ordered := make([]DependencyAnalyzer, 0, len(names))
	for _, derived := range []bool{true, false} {
		for _, name := range names {
			if analyzerByName[name].Derived() == derived {
				ordered = append(ordered, analyzerByName[name])
			}
		}
	}
	return ordered
}

// BlastRadiusWith computes a project's dependents using the analyzer it pinned.
//
// The pin is honoured exactly: an analyzer that does not match yields the
// neutral baseline rather than another analyzer's answer. A project pins an
// analyzer precisely because the others would answer from evidence it does not
// consider authoritative — a repository whose go.mod is deliberately not the
// source of truth for what depends on it — so substituting one would produce a
// confident ranking from the data the pin exists to exclude.
//
// An unregistered pin is a hard failure, not a fallback: the project asked for
// something this build does not have.
func BlastRadiusWith(analyzerName, projectID string, manifests []DependencyManifest) (*DependencyGraphSnapshot, error) {
	if analyzerName == "" {
		return BlastRadius(projectID, manifests)
	}
	analyzer, err := LookupDependencyAnalyzer(analyzerName)
	if err != nil {
		return nil, err
	}
	snapshot, err := analyzer.Analyze(projectID, manifests)
	if errors.Is(err, ErrAnalyzerNoMatch) {
		return neutralSnapshot(projectID), nil
	}
	if err != nil {
		return nil, err
	}
	return snapshot, nil
}

// neutralSnapshot builds the baseline for a project no analyzer speaks for.
func neutralSnapshot(projectID string) *DependencyGraphSnapshot {
	return &DependencyGraphSnapshot{
		Analyzer:   NeutralAnalyzerName,
		ProjectID:  projectID,
		ComputedAt: time.Now().UTC(),
	}
}

// =============================================================================
// gomod
// =============================================================================

// GoModAnalyzerName is the registry key for the go-module analyzer.
const GoModAnalyzerName = "gomod"

// goModAnalyzer derives blast radius from module requirements.
type goModAnalyzer struct{}

// Compile-time interface satisfaction check.
var _ DependencyAnalyzer = goModAnalyzer{}

// Name implements DependencyAnalyzer.
func (goModAnalyzer) Name() string { return GoModAnalyzerName }

// Derived implements DependencyAnalyzer: the graph comes from go.mod.
func (goModAnalyzer) Derived() bool { return true }

// Analyze implements DependencyAnalyzer by walking the requirement graph
// backwards.
//
// TRANSITIVE, not direct, because that is what blast radius means: go-objstore
// has one direct dependent and three transitive ones, and ranking it by the
// direct count would put it level with a leaf's parent. The walk carries a
// visited set, so a cycle in the manifests — impossible in real go modules,
// entirely possible in a partially-updated manifest set — terminates instead of
// hanging the sweep, which would fail the module closed with no error and no
// symptom.
func (goModAnalyzer) Analyze(projectID string, manifests []DependencyManifest) (*DependencyGraphSnapshot, error) {
	// projectOf maps a module path to the project publishing it. A require
	// naming a path absent here is outside the portfolio and is ignored.
	projectOf := make(map[string]string, len(manifests))
	analysable := false
	for _, m := range manifests {
		if m.Module == "" {
			continue
		}
		projectOf[m.Module] = m.ProjectID
		if m.ProjectID == projectID {
			analysable = true
		}
	}
	if !analysable {
		return nil, &AnalyzerNoMatchError{Analyzer: GoModAnalyzerName, ProjectID: projectID}
	}

	// dependents reverses the requirement edges: if A requires B's module, then
	// A is a dependent of B.
	dependents := make(map[string][]string, len(manifests))
	for _, m := range manifests {
		for _, required := range m.Requires {
			owner, known := projectOf[required]
			if !known || owner == m.ProjectID {
				continue
			}
			dependents[owner] = append(dependents[owner], m.ProjectID)
		}
	}

	return &DependencyGraphSnapshot{
		Analyzer:   GoModAnalyzerName,
		ProjectID:  projectID,
		Dependents: reachable(projectID, dependents),
		ComputedAt: time.Now().UTC(),
	}, nil
}

// reachable collects every project reachable from root through the dependents
// graph, excluding root itself, sorted.
//
// Breadth-first with a visited set. The set is what makes a cycle terminate,
// and excluding root is what stops a self-requiring manifest inflating every
// blast radius by one — a shift that is invisible in a ranking because it moves
// every item equally, right up until a project with no dependents ties one that
// genuinely has none.
func reachable(root string, dependents map[string][]string) []string {
	// The empty name is seeded as visited so a blank entry — which a
	// hand-maintained declared list produces from a stray comma — is dropped
	// rather than counted. An empty dependent still inflates a blast radius,
	// and a ranking is a comparison, so an inflated one outranks an honest one.
	visited := map[string]bool{root: true, "": true}
	found := make([]string, 0, len(dependents))

	queue := []string{root}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, next := range dependents[current] {
			if visited[next] {
				continue
			}
			visited[next] = true
			found = append(found, next)
			queue = append(queue, next)
		}
	}

	sort.Strings(found)
	return found
}

// =============================================================================
// declared
// =============================================================================

// DeclaredAnalyzerName is the registry key for the explicit-list analyzer.
const DeclaredAnalyzerName = "declared"

// declaredAnalyzer reads a stated dependents list.
//
// It serves the projects the manifest-parsing analyzers cannot: mixed stacks,
// repositories with no machine-readable manifest, and couplings that exist but
// are not imports — xkey-android depends on go-xkms by a wire contract, which
// no dependency file records.
type declaredAnalyzer struct{}

// Compile-time interface satisfaction check.
var _ DependencyAnalyzer = declaredAnalyzer{}

// Name implements DependencyAnalyzer.
func (declaredAnalyzer) Name() string { return DeclaredAnalyzerName }

// Derived implements DependencyAnalyzer: the list is stated, not computed, so
// this analyzer answers only where no derived one can.
func (declaredAnalyzer) Derived() bool { return false }

// Analyze implements DependencyAnalyzer by normalising the declared list.
//
// Normalisation is not politeness. A hand-maintained list repeats entries and
// names its own project, and both inflate the blast radius — which is a
// ranking, so an inflated one outranks an honest one.
func (declaredAnalyzer) Analyze(projectID string, manifests []DependencyManifest) (*DependencyGraphSnapshot, error) {
	// The whole declared graph, not just this project's row.
	//
	// TRANSITIVE, for the same reason gomod is and because Analyze promises it:
	// blast radius means everything a change reaches. In a declared graph where
	// A lists B as its dependent and B lists C as its own, a change to A
	// reaches both — reading only A's row would rank it 1 instead of 2 and
	// systematically under-prioritise exactly the mixed-stack projects this
	// analyzer exists to serve.
	dependents := make(map[string][]string, len(manifests))
	declared := false
	for _, m := range manifests {
		if len(m.Dependents) == 0 {
			continue
		}
		dependents[m.ProjectID] = m.Dependents
		if m.ProjectID == projectID {
			declared = true
		}
	}
	if !declared {
		return nil, &AnalyzerNoMatchError{Analyzer: DeclaredAnalyzerName, ProjectID: projectID}
	}

	return &DependencyGraphSnapshot{
		Analyzer:  DeclaredAnalyzerName,
		ProjectID: projectID,
		// reachable is the same walk gomod uses, so both analyzers deduplicate,
		// drop the project itself, sort, and terminate on a cycle by exactly
		// one rule. A hand-maintained list repeats entries and names its own
		// project, and both inflate a blast radius — which is a ranking, so an
		// inflated one outranks an honest one.
		Dependents: reachable(projectID, dependents),
		ComputedAt: time.Now().UTC(),
	}, nil
}

// =============================================================================
// Static manifests
// =============================================================================

// StaticManifestSource serves a fixed set of manifests.
//
// A complete implementation, not a placeholder: a portfolio whose dependency
// declarations are configuration is exactly what the `declared` analyzer is
// for, and this is how those declarations reach a sweep. A forge-reading source
// that parses go.mod at HEAD is additional, not a replacement.
type StaticManifestSource struct {
	manifests []DependencyManifest
}

// Compile-time interface satisfaction check.
var _ ManifestSource = (*StaticManifestSource)(nil)

// NewStaticManifestSource returns a source serving the supplied manifests.
func NewStaticManifestSource(manifests []DependencyManifest) *StaticManifestSource {
	held := make([]DependencyManifest, len(manifests))
	copy(held, manifests)
	return &StaticManifestSource{manifests: held}
}

// Manifests implements ManifestSource.
//
// The same set for every tenant: these describe the shape of a code portfolio,
// which is not a per-tenant fact. A source that did vary by tenant is free to;
// this one does not pretend to.
func (s *StaticManifestSource) Manifests(_ context.Context, _ string) ([]DependencyManifest, error) {
	out := make([]DependencyManifest, len(s.manifests))
	copy(out, s.manifests)
	return out, nil
}

// init registers the analyzers this build ships. Only complete ones appear.
func init() {
	RegisterDependencyAnalyzer(goModAnalyzer{})
	RegisterDependencyAnalyzer(declaredAnalyzer{})
}
