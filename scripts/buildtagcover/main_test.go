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

package main

import (
	"go/build/constraint"
	"strings"
	"testing"
)

// mustParse parses a constraint the way the scanner does.
func mustParse(t *testing.T, expr string) constraint.Expr {
	t.Helper()
	parsed, err := constraint.Parse("//go:build " + expr)
	if err != nil {
		t.Fatalf("parse %q: %v", expr, err)
	}
	return parsed
}

// mustPasses builds passes from "name:tags" specs.
func mustPasses(t *testing.T, specs ...string) []pass {
	t.Helper()
	passes, err := parsePasses(specs, true)
	if err != nil {
		t.Fatalf("parsePasses: %v", err)
	}
	return passes
}

// TestCoverage_TheDefectsThisGateExistsFor pins the three real cases, so the
// gate cannot regress into the name-matching check it replaced.
//
// Each is a constraint whose TAG NAMES all appear in some pass while the
// EXPRESSION is satisfied by none — the exact blind spot that let #299 ship
// and that hid #305.
func TestCoverage_TheDefectsThisGateExistsFor(t *testing.T) {
	maximal := "maximal:rest rag mcp timeseries otel vector"
	minimal := "minimal:hashicorpmetrics local"

	cases := map[string]struct {
		expr    string
		passes  []string
		covered bool
	}{
		// go#299: rag_stub.go. Both names are in the maximal pass, which is
		// why the old name check passed; the maximal pass sets both and the
		// minimal pass sets neither, so nothing compiled it.
		"rest && !rag with no lean pass": {
			expr:    "rest && !rag",
			passes:  []string{maximal, minimal},
			covered: false,
		},
		"rest && !rag once a pass sets rest without rag": {
			expr:    "rest && !rag",
			passes:  []string{maximal, minimal, "standard:rest timeseries otel"},
			covered: true,
		},
		// go#305, the same hole one tag over.
		"rest && !timeseries": {
			expr:    "rest && !timeseries",
			passes:  []string{maximal, minimal, "standard:rest timeseries otel"},
			covered: false,
		},
		// The sharpest one: otel is TEST-ONLY, so !otel is what every shipped
		// binary runs, and every pass that sets rest also set otel.
		"rest && !otel": {
			expr:    "rest && !otel",
			passes:  []string{maximal, minimal, "standard:rest timeseries otel"},
			covered: false,
		},
		"rest && !otel with the lean pass": {
			expr:    "rest && !otel",
			passes:  []string{maximal, minimal, "lean:rest mcp"},
			covered: true,
		},
		// A plain positive tag no pass supplies — the one case the old check
		// did catch, kept so the replacement is a superset.
		"an unknown feature": {
			expr:    "somethingnew",
			passes:  []string{maximal, minimal},
			covered: false,
		},
		// A negation alone is satisfied by any pass lacking the tag, so the
		// file IS compiled and must not be reported.
		"a bare negation": {
			expr:    "!somethingnew",
			passes:  []string{maximal, minimal},
			covered: true,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := coveredBySomePass(mustParse(t, tc.expr), mustPasses(t, tc.passes...), map[string]bool{})
			if got != tc.covered {
				t.Errorf("coveredBySomePass(%q) = %v, want %v", tc.expr, got, tc.covered)
			}
		})
	}
}

// TestCoverage_OperatorsMeanWhatTheCompilerMeans covers the structure a
// tr-and-grep pipeline cannot see at all: disjunction, parentheses, and
// nested negation.
func TestCoverage_OperatorsMeanWhatTheCompilerMeans(t *testing.T) {
	passes := mustPasses(t, "maximal:vector qrdb_vector", "minimal:local")

	cases := map[string]struct {
		expr    string
		covered bool
	}{
		// Satisfied by the minimal pass, which has neither tag.
		"!vector || !qrdb_vector": {expr: "!vector || !qrdb_vector", covered: true},
		// Satisfied by the maximal pass, which has both.
		"vector && qrdb_vector": {expr: "vector && qrdb_vector", covered: true},
		// Neither pass sets vector without qrdb_vector.
		"vector && !qrdb_vector": {expr: "vector && !qrdb_vector", covered: false},
		// Parenthesised disjunction: the minimal pass has local.
		"(vector && qrdb_vector) || local": {expr: "(vector && qrdb_vector) || local", covered: true},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := coveredBySomePass(mustParse(t, tc.expr), passes, map[string]bool{}); got != tc.covered {
				t.Errorf("coveredBySomePass(%q) = %v, want %v", tc.expr, got, tc.covered)
			}
		})
	}
}

// TestCoverage_FreeTagsAreSatisfiableEitherWay proves the treatment of tags a
// pass does not decide.
//
// A platform or exempt tag is genuinely free — the build may run somewhere it
// is set and somewhere it is not — so a constraint mentioning one is covered
// when SOME assignment satisfies it. Without this, every `unix` file in the
// tree would be reported as uncovered, and the gate would be ignored within a
// day.
func TestCoverage_FreeTagsAreSatisfiableEitherWay(t *testing.T) {
	passes := mustPasses(t, "maximal:rest")
	free := map[string]bool{"unix": true, "gpu": true}

	for _, expr := range []string{"unix", "!unix", "rest && !gpu", "rest && gpu"} {
		t.Run(expr, func(t *testing.T) {
			if !coveredBySomePass(mustParse(t, expr), passes, free) {
				t.Errorf("%q should be covered: its free tags can take either value", expr)
			}
		})
	}

	// A free tag does not rescue a constraint whose FEATURE half no pass sets.
	if coveredBySomePass(mustParse(t, "unix && somethingnew"), passes, free) {
		t.Error("a free tag must not make an unsupplied feature tag satisfiable")
	}
}

// TestParsePasses_NegationMarkersAreAbsence pins how "!integration" in a pass
// spec is read: TEST_TAGS spells absence that way, and a pass must not end up
// holding a tag literally named "!integration".
func TestParsePasses_NegationMarkersAreAbsence(t *testing.T) {
	passes := mustPasses(t, "maximal:!integration rest")
	tags := passes[0].tags

	if tags["integration"] {
		t.Error("!integration must mean the tag is ABSENT, not present")
	}
	for tag := range tags {
		if strings.HasPrefix(tag, "!") {
			t.Errorf("pass holds a tag literally named %q", tag)
		}
	}
	if !tags["rest"] {
		t.Error("rest should be set")
	}
	// And the constraint every test file carries must be satisfied by it.
	if !coveredBySomePass(mustParse(t, "!integration && rest"), passes, map[string]bool{}) {
		t.Error("!integration && rest must be covered by a pass that omits integration and sets rest")
	}
}

// TestCoverage_ALocalOnlyPassIsNotCIcoverage is the distinction that keeps
// this gate from repeating, one level up, the dishonesty it was written to
// remove.
//
// `make test-desktop` covers the entire Wails binding surface and needs
// webkit2gtk, so no CI job runs it. Counting it as coverage would make the
// gate's claim true of the Makefile and false of the pipeline — which is
// exactly the shape of "the tag appears in a pass, therefore it is tested".
func TestCoverage_ALocalOnlyPassIsNotCIcoverage(t *testing.T) {
	ci, err := parsePasses([]string{"maximal:rest rag"}, true)
	if err != nil {
		t.Fatalf("parsePasses: %v", err)
	}
	local, err := parsePasses([]string{"desktop:rest rag desktop"}, false)
	if err != nil {
		t.Fatalf("parsePasses: %v", err)
	}
	passes := append(ci, local...)

	if got, by := coverageOf(mustParse(t, "desktop"), passes, map[string]bool{}); got != coveredLocallyOnly || by != "desktop" {
		t.Errorf("`desktop` coverage = %v by %q, want coveredLocallyOnly by \"desktop\": only a pass CI skips satisfies it", got, by)
	}
	if got, by := coverageOf(mustParse(t, "rest"), passes, map[string]bool{}); got != coveredInCI || by != "maximal" {
		t.Errorf("`rest` coverage = %v by %q, want coveredInCI by \"maximal\"", got, by)
	}
	if got, _ := coverageOf(mustParse(t, "nosuchtag"), passes, map[string]bool{}); got != notCovered {
		t.Errorf("`nosuchtag` coverage = %v, want notCovered", got)
	}
	// A local-only pass still counts as "reached by something", so the fatal
	// section must not claim otherwise.
	if !coveredBySomePass(mustParse(t, "desktop"), passes, map[string]bool{}) {
		t.Error("a local-only pass still reaches the constraint; it is a note, not an error")
	}
}

// TestNegatedTagsIn is the derivation the minimal pass's off-set is built
// from, so it is pinned rather than trusted.
//
// The set answers "which tags have a DISABLED side" — the tags a pass must
// turn off for the stub half of the tree to compile. Getting it wrong is
// silent in both directions: a missing tag leaves stubs compiled by nothing
// (#299), and a spurious one turns off working code for no coverage, which is
// what broke pkg/sdlc in the minimal pass (#304).
func TestNegatedTagsIn(t *testing.T) {
	cases := map[string][]string{
		// The plain shapes.
		"!rag":                    {"rag"},
		"rest":                    nil,
		"rest && !rag":            {"rag"},
		"!vector || !qrdb_vector": {"qrdb_vector", "vector"},
		// Both sides of a conjunction, and a tag that appears both ways.
		"!quicbaas_amqp && !quicbaas_mqtt && !quicbaas_stomp": {"quicbaas_amqp", "quicbaas_mqtt", "quicbaas_stomp"},
		"vector && !gpu": {"gpu"},
		// A tag used positively somewhere and negatively here still counts:
		// the file exists BECAUSE it is off.
		"!integration && raft && local": {"integration"},
	}

	for expr, want := range cases {
		t.Run(expr, func(t *testing.T) {
			got := negatedTagsIn(mustParse(t, expr))
			if len(got) != len(want) {
				t.Fatalf("negatedTagsIn(%q) = %v, want %v", expr, got, want)
			}
			for i := range want {
				if got[i] != want[i] {
					t.Fatalf("negatedTagsIn(%q) = %v, want %v", expr, got, want)
				}
			}
		})
	}
}

// TestNegatedTagsIn_ExcludesTagsUsedOnlyPositively is the #304 direction,
// stated as its own property: a tag with no disabled side must never reach the
// off-set, or the pass turns off working code and buys nothing.
func TestNegatedTagsIn_ExcludesTagsUsedOnlyPositively(t *testing.T) {
	for _, expr := range []string{"codec_json", "rest && quic", "hashicorpmetrics && local"} {
		if got := negatedTagsIn(mustParse(t, expr)); len(got) != 0 {
			t.Errorf("negatedTagsIn(%q) = %v, want none: nothing here has a disabled side", expr, got)
		}
	}
}
