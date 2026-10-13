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

// Command buildtagcover reports every //go:build constraint in the tree that
// NO configured test pass satisfies.
//
// WHY THIS IS NOT THE SHELL CHECK IT REPLACES. `make check-build-tags` asked
// whether every tag NAME appears in some pass's tag list. That question has a
// blind spot the size of the defect class it was written to prevent: a
// constraint is a BOOLEAN EXPRESSION, and the satisfiability of an expression
// is not a property of the names in it.
//
//	rag_stub.go	 //go:build rest && !rag
//
// Both `rest` and `rag` appear in the maximal pass, so the name check passed.
// But the maximal pass sets BOTH, and the minimal pass sets NEITHER, so
// `rest && !rag` was satisfied by no pass and the file was compiled by nothing
// — for long enough that the REST route goldens froze a surface the shipped
// build does not serve (go-quicbaas#299). The same hole, one tag over, still
// hid `rest && !timeseries` (#305), and the inverse of it — a tag turned off
// in a pass with no !tag file behind it — cost pkg/sdlc every test in the
// minimal pass (#304).
//
// So this asks the question that actually matters: FOR EACH CONSTRAINT, IS
// THERE A PASS UNDER WHICH IT EVALUATES TRUE? It parses with
// go/build/constraint — the same package the toolchain itself uses, so `||`,
// `&&`, `!` and parentheses mean here exactly what they mean to the compiler
// rather than what a `tr`-and-grep pipeline approximates.
//
// FREE TAGS are the ones a pass does not decide: GOOS/GOARCH and friends
// (linux, unix, cgo), which the platform supplies, and the exempt tags that
// gate hardware or credentials no runner has (pkcs11, swtpm, gpu...). A
// constraint mentioning one is covered when SOME assignment of those tags
// satisfies it under some pass — which is the honest reading, because such a
// tag is genuinely free to be either value depending on where the build runs.
// Enumerating them is cheap: no constraint in this tree carries more than a
// few.
package main

import (
	"flag"
	"fmt"
	"go/build/constraint"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// pass is one configured test invocation: a name, the tags it sets, and
// whether CI actually runs it.
//
// THE DISTINCTION IS LOAD-BEARING. "A make target exists" and "something
// compiles this in CI" are different claims, and a gate that conflates them
// re-creates the problem it was written for one level up: `make test-desktop`
// covers the whole Wails binding surface, needs webkit2gtk, and no CI job
// runs it — so reporting those files as covered would be true of the Makefile
// and false of the pipeline. Constraints reachable only from a local-only
// pass are reported separately rather than counted silently.
type pass struct {
	name string
	tags map[string]bool
	inCI bool
}

// stringList collects a repeatable flag.
type stringList []string

func (s *stringList) String() string     { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error { *s = append(*s, v); return nil }

func main() {
	var passSpecs, localSpecs, dirs stringList
	listNegated := flag.Bool("list-negated", false, "print every tag that appears negated in the tree, and exit")
	free := flag.String("free", "", "space-separated tags a pass does not decide (platform and exempt tags)")
	flag.Var(&passSpecs, "pass", "a test pass CI runs, as name:tag tag tag (repeatable)")
	flag.Var(&localSpecs, "localpass", "a test pass that exists but CI does not run (repeatable)")
	flag.Var(&dirs, "dir", "directory to scan (repeatable)")
	flag.Parse()

	if len(dirs) == 0 || (len(passSpecs) == 0 && !*listNegated) {
		fmt.Fprintln(os.Stderr, "usage: buildtagcover -pass name:'tags' [-localpass name:'tags'] -dir pkg [-dir cmd] [-free 'tags']")
		fmt.Fprintln(os.Stderr, "       buildtagcover -list-negated -dir pkg [-dir cmd]")
		os.Exit(2)
	}

	// -list-negated answers a different question from the gate, for the
	// Makefile rather than for a human: WHICH TAGS HAVE A DISABLED SIDE?
	//
	// That is the set a pass must turn off to compile the stub half of the
	// tree, and deriving it is what keeps the answer true. Hand-listing it is
	// one new stub file away from being wrong, and wrong SILENTLY: the pass
	// still runs, still passes, and simply stops compiling the file nobody
	// noticed was added — which is #299 with the numbers changed. A tag with
	// no `!tag` file must NOT be in the set: turning it off buys no stub
	// coverage and merely deletes working code, which is what broke pkg/sdlc
	// in the minimal pass (#304).
	if *listNegated {
		found, err := scan(dirs)
		if err != nil {
			fmt.Fprintln(os.Stderr, "buildtagcover:", err)
			os.Exit(2)
		}
		negated := make(map[string]bool)
		for _, e := range found {
			for _, tag := range negatedTagsIn(e.parsed) {
				negated[tag] = true
			}
		}
		// The Makefile consumes this line.
		fmt.Fprintln(os.Stdout, strings.Join(sortedSet(negated), " "))
		return
	}

	passes, err := parsePasses(passSpecs, true)
	if err != nil {
		fmt.Fprintln(os.Stderr, "buildtagcover:", err)
		os.Exit(2)
	}
	localPasses, err := parsePasses(localSpecs, false)
	if err != nil {
		fmt.Fprintln(os.Stderr, "buildtagcover:", err)
		os.Exit(2)
	}
	passes = append(passes, localPasses...)
	freeTags := toSet(strings.Fields(*free))
	for tag := range builtinFreeTags {
		freeTags[tag] = true
	}

	found, err := scan(dirs)
	if err != nil {
		fmt.Fprintln(os.Stderr, "buildtagcover:", err)
		os.Exit(2)
	}

	var uncovered, localOnly []string
	coveredBy := make(map[string]string)
	localFiles := 0
	for _, expr := range sortedKeys(found) {
		got, by := coverageOf(found[expr].parsed, passes, freeTags)
		switch got {
		case coveredInCI:
			continue
		case coveredLocallyOnly:
			localOnly = append(localOnly, expr)
			coveredBy[expr] = by
			localFiles += len(found[expr].files)
		default:
			uncovered = append(uncovered, expr)
		}
	}

	// Reported before the fatal section so it is not lost in a failing run,
	// and non-fatal because the fix is a CI capability (a GUI toolchain on the
	// runner), not something a lane can correct in its own diff.
	if len(localOnly) > 0 {
		fmt.Fprintf(os.Stderr, "NOTE — COMPILED BY A make TARGET, BUT BY NO CI JOB.\n")
		fmt.Fprintf(os.Stderr, "These %d constraint(s), covering %d file(s), ARE satisfied by a configured\n",
			len(localOnly), localFiles)
		fmt.Fprintf(os.Stderr, "pass — so they are NOT the same as the failures below, which no pass\n")
		fmt.Fprintf(os.Stderr, "reaches at all. The pass that covers them is one CI does not run, so\n")
		fmt.Fprintf(os.Stderr, "nothing in the pipeline compiles these files:\n\n")
		for _, expr := range localOnly {
			fmt.Fprintf(os.Stderr, "  //go:build %s\n      %d file(s), covered only by the %q pass\n",
				expr, len(found[expr].files), coveredBy[expr])
		}
		fmt.Fprintf(os.Stderr, "\nThis is a recorded gap, not a failure: the fix is a CI capability (a\n")
		fmt.Fprintf(os.Stderr, "runner that can run that pass), not something a lane can correct in its\n")
		fmt.Fprintf(os.Stderr, "own diff. Wiring the pass into CI is what turns this note into coverage.\n\n")
	}

	if len(uncovered) == 0 {
		fmt.Fprintf(os.Stdout, "Build-constraint coverage OK (%d distinct constraints, %d passes, %d local-only)\n",
			len(found), len(passes), len(localOnly))
		return
	}

	fmt.Fprintf(os.Stderr, "FAIL — SATISFIED BY NO CONFIGURED PASS AT ALL (not merely absent from CI):\n\n")
	for _, expr := range uncovered {
		fmt.Fprintf(os.Stderr, "  //go:build %s\n", expr)
		for _, f := range found[expr].files {
			fmt.Fprintf(os.Stderr, "      %s\n", f)
		}
		fmt.Fprintln(os.Stderr)
	}
	fmt.Fprintf(os.Stderr, "Every file above is compiled by nothing: `go test` reports\n")
	fmt.Fprintf(os.Stderr, "\"no test files\" or silently omits it, and the gate still passes.\n")
	fmt.Fprintf(os.Stderr, "Fix by adding a pass that satisfies the constraint, by changing the\n")
	fmt.Fprintf(os.Stderr, "constraint to one a pass reaches, or by listing a tag in TAG_GATE_EXEMPT\n")
	fmt.Fprintf(os.Stderr, "with the reason it cannot run.\n")
	os.Exit(1)
}

// builtinFreeTags are decided by the platform or the toolchain rather than by
// a -tags list, so a pass neither sets nor withholds them.
//
// A KNOWN IMPRECISION, recorded rather than hidden: these are enumerated as
// independent booleans, so a constraint no Go target can satisfy — "linux &&
// windows", "darwin && !unix" — is reported covered. Modelling GOOS and GOARCH
// as mutually exclusive would catch those, but such a file is dead in EVERY
// build, which is a different defect from the one this gate is for, and no
// constraint in this tree has that shape.
var builtinFreeTags = toSet(strings.Fields(
	"linux darwin windows freebsd openbsd netbsd dragonfly solaris aix js wasip1 ios android plan9 " +
		"unix cgo race msan asan boringcrypto gc gccgo " +
		"amd64 arm64 386 arm ppc64 ppc64le mips mips64 mipsle mips64le riscv64 s390x wasm loong64",
))

// isFreeTag reports whether a pass decides the tag.
//
// The go1.N release tags are free by shape rather than by listing: the
// toolchain sets whichever ones its version implies, and enumerating them
// would go stale on every Go release.
func isFreeTag(tag string, freeTags map[string]bool) bool {
	return freeTags[tag] || builtinFreeTags[tag] || strings.HasPrefix(tag, "go1.")
}

// entry is one distinct constraint and the files carrying it.
type entry struct {
	parsed constraint.Expr
	files  []string
}

// parsePasses turns "name:tag tag" specs into passes.
func parsePasses(specs []string, inCI bool) ([]pass, error) {
	passes := make([]pass, 0, len(specs))
	for _, spec := range specs {
		name, tags, ok := strings.Cut(spec, ":")
		if !ok {
			return nil, fmt.Errorf("pass %q is not name:tags", spec)
		}
		set := make(map[string]bool)
		for _, tag := range strings.Fields(tags) {
			// A leading "!" in a pass spec is documentation that the tag is
			// absent (TEST_TAGS spells it that way); absence is what the
			// evaluator reads, so drop the marker rather than record a tag
			// literally named "!integration".
			set[strings.TrimPrefix(tag, "!")] = !strings.HasPrefix(tag, "!")
		}
		for tag, on := range set {
			if !on {
				delete(set, tag)
			}
		}
		passes = append(passes, pass{name: name, tags: set, inCI: inCI})
	}
	return passes, nil
}

// scan walks the directories and groups files by their constraint.
func scan(dirs []string) (map[string]*entry, error) {
	found := make(map[string]*entry)
	for _, dir := range dirs {
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if name := d.Name(); name == "testdata" || name == "vendor" || strings.HasPrefix(name, ".") {
					return fs.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") {
				return nil
			}
			line, err := buildLine(path)
			if err != nil {
				return err
			}
			if line == "" {
				return nil // unconstrained: always compiled
			}
			expr, err := constraint.Parse(line)
			if err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
			key := expr.String()
			e := found[key]
			if e == nil {
				e = &entry{parsed: expr}
				found[key] = e
			}
			e.files = append(e.files, path)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return found, nil
}

// buildLine returns the file's //go:build line, or "" when it has none.
//
// It reads only the header: the constraint must precede the package clause,
// so everything after it is irrelevant and parsing the whole file would make
// this walk needlessly expensive on a tree this size.
func buildLine(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "package ") {
			return "", nil
		}
		if constraint.IsGoBuild(line) {
			return line, nil
		}
	}
	return "", nil
}

// coverage is how well a constraint is reached.
type coverage int

const (
	notCovered coverage = iota
	coveredLocallyOnly
	coveredInCI
)

// coverageOf reports the best coverage any pass gives the constraint, for some
// assignment of the free tags it mentions, and names a pass that provides it.
func coverageOf(expr constraint.Expr, passes []pass, freeTags map[string]bool) (coverage, string) {
	best, by := notCovered, ""
	freeInExpr := freeTagsIn(expr, freeTags)
	for _, p := range passes {
		for _, assignment := range assignments(freeInExpr) {
			ok := expr.Eval(func(tag string) bool {
				if v, isFree := assignment[tag]; isFree {
					return v
				}
				return p.tags[tag]
			})
			if !ok {
				continue
			}
			if p.inCI {
				return coveredInCI, p.name
			}
			if best == notCovered {
				best, by = coveredLocallyOnly, p.name
			}
			break
		}
	}
	return best, by
}

// coveredBySomePass reports whether any pass reaches the constraint at all.
func coveredBySomePass(expr constraint.Expr, passes []pass, freeTags map[string]bool) bool {
	got, _ := coverageOf(expr, passes, freeTags)
	return got != notCovered
}

// negatedTagsIn returns every tag the constraint mentions under a negation.
//
// Under a NotExpr, at any depth: `!rag`, `rest && !rag` and
// `!vector || !qrdb_vector` all mean the file exists to be compiled when that
// tag is OFF. The walk carries the polarity rather than pattern-matching on
// the top level, so a negation nested inside a conjunction or disjunction is
// found wherever it sits. It cannot nest inside another negation: go/build
// /constraint refuses `!!rag` outright ("double negation not allowed"), so
// the flag only ever toggles once.
func negatedTagsIn(expr constraint.Expr) []string {
	seen := make(map[string]bool)
	var walk func(e constraint.Expr, negated bool)
	walk = func(e constraint.Expr, negated bool) {
		switch x := e.(type) {
		case *constraint.TagExpr:
			if negated {
				seen[x.Tag] = true
			}
		case *constraint.NotExpr:
			walk(x.X, !negated)
		case *constraint.AndExpr:
			walk(x.X, negated)
			walk(x.Y, negated)
		case *constraint.OrExpr:
			walk(x.X, negated)
			walk(x.Y, negated)
		}
	}
	walk(expr, false)
	return sortedSet(seen)
}

// freeTagsIn returns the free tags the constraint actually mentions, sorted so
// the enumeration below is deterministic.
func freeTagsIn(expr constraint.Expr, freeTags map[string]bool) []string {
	seen := make(map[string]bool)
	var walk func(constraint.Expr)
	walk = func(e constraint.Expr) {
		switch x := e.(type) {
		case *constraint.TagExpr:
			if isFreeTag(x.Tag, freeTags) {
				seen[x.Tag] = true
			}
		case *constraint.NotExpr:
			walk(x.X)
		case *constraint.AndExpr:
			walk(x.X)
			walk(x.Y)
		case *constraint.OrExpr:
			walk(x.X)
			walk(x.Y)
		}
	}
	walk(expr)
	return sortedSet(seen)
}

// assignments enumerates every true/false combination of the given tags.
//
// Exhaustive rather than clever: a constraint in this tree mentions at most a
// handful of free tags, so 2^n is a few iterations, and an exact answer beats
// a heuristic in a gate whose whole purpose is that the previous heuristic
// missed things.
func assignments(tags []string) []map[string]bool {
	out := make([]map[string]bool, 0, 1<<len(tags))
	for mask := 0; mask < 1<<len(tags); mask++ {
		a := make(map[string]bool, len(tags))
		for i, tag := range tags {
			a[tag] = mask&(1<<i) != 0
		}
		out = append(out, a)
	}
	return out
}

func toSet(items []string) map[string]bool {
	set := make(map[string]bool, len(items))
	for _, item := range items {
		set[item] = true
	}
	return set
}

func sortedSet(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedKeys(m map[string]*entry) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
