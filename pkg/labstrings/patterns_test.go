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

package labstrings

import (
	"errors"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// mustParse parses pattern text that the test knows is good.
func mustParse(t testing.TB, text string) []Pattern {
	t.Helper()
	patterns, err := ParsePatterns(text)
	if err != nil {
		t.Fatalf("ParsePatterns: %v", err)
	}
	return patterns
}

func TestParsePatterns_ReadsEveryClass(t *testing.T) {
	patterns := mustParse(t, "lab-address\t"+`\b203\.0\.113\.\d{1,3}\b`+"\n"+
		"lab-host\t"+`\bfleet-node-\d{2}\b`+"\n"+
		"owner-path\t"+`/home/example-owner\b`+"\n"+
		"owner-account\t"+`\bexample-owner\b`+"\n")

	want := []struct {
		class  Class
		sample string
	}{
		{ClassLabAddress, "dial 203.0.113.7:8443"},
		{ClassLabHost, "on fleet-node-07"},
		{ClassOwnerPath, "/home/example-owner/sources"},
		{ClassOwnerAccount, "ask example-owner"},
	}
	if len(patterns) != len(want) {
		t.Fatalf("got %d patterns, want %d", len(patterns), len(want))
	}
	for i, w := range want {
		p := patterns[i]
		if p.Class != w.class {
			t.Errorf("pattern %d class = %s, want %s", i, p.Class, w.class)
		}
		if !p.Expr.MatchString(w.sample) {
			t.Errorf("pattern %d does not match %q", i, w.sample)
		}
		if p.Guidance != classGuidance[w.class] {
			t.Errorf("pattern %d guidance = %q, want the class advice", i, p.Guidance)
		}
	}
}

func TestParsePatterns_SkipsCommentsAndBlankLinesAndStripsCR(t *testing.T) {
	text := "# a header\r\n\r\n   \t\r\n   # an indented comment\r\nlab-host\tfleet-node-[0-9]{2}\r\n\r\n"
	patterns := mustParse(t, text)
	if len(patterns) != 1 {
		t.Fatalf("got %d patterns, want 1", len(patterns))
	}
	if got := patterns[0].Expr.String(); got != "fleet-node-[0-9]{2}" {
		t.Errorf("regex = %q: the carriage return was not stripped", got)
	}
	if !patterns[0].Expr.MatchString("fleet-node-07") {
		t.Error("the parsed rule does not match its own value")
	}
}

func TestParsePatterns_SplitsAtTheFirstTab(t *testing.T) {
	patterns := mustParse(t, "lab-host\ta\tb")
	if got := patterns[0].Expr.String(); got != "a\tb" {
		t.Errorf("regex = %q, want everything after the first tab", got)
	}
}

func TestParsePatterns_TheFoldSuffixIgnoresCase(t *testing.T) {
	exact := mustParse(t, "lab-host\tfleet-node")[0]
	folded := mustParse(t, "lab-host/i\tfleet-node")[0]

	if exact.Class != ClassLabHost || folded.Class != ClassLabHost {
		t.Fatalf("classes = %s, %s; the suffix is not part of the class", exact.Class, folded.Class)
	}
	if exact.Expr.MatchString("FLEET-NODE") {
		t.Error("a rule without /i ignored case")
	}
	if !folded.Expr.MatchString("FLEET-NODE") {
		t.Error("a rule with /i did not ignore case")
	}
}

func TestParsePatterns_MovesOwnerAccountRulesLast(t *testing.T) {
	patterns := mustParse(t, strings.Join([]string{
		"owner-account\tfirst-account",
		"lab-host\tsecond-host",
		"owner-account\tthird-account",
		"owner-path\tfourth-path",
	}, "\n"))

	var got []string
	for _, p := range patterns {
		got = append(got, p.Expr.String())
	}
	want := []string{"second-host", "fourth-path", "first-account", "third-account"}
	if !slices.Equal(got, want) {
		t.Fatalf("order = %v, want %v (file order, accounts last)", got, want)
	}
}

func TestParsePatterns_TextWithNoRulesIsAnEmptySet(t *testing.T) {
	for _, text := range []string{"", "\n\n", "# only a comment\n"} {
		patterns, err := ParsePatterns(text)
		if err != nil {
			t.Errorf("ParsePatterns(%q): %v", text, err)
		}
		if len(patterns) != 0 {
			t.Errorf("ParsePatterns(%q) = %d patterns, want none", text, len(patterns))
		}
	}
}

// TestParsePatterns_RefusesAnUnusableLineByNumberOnly pins both halves of the
// error contract: the line number is reported, and nothing the line says is.
// Every fixture line carries the marker "private", standing in for a value that
// must never reach a log.
func TestParsePatterns_RefusesAnUnusableLineByNumberOnly(t *testing.T) {
	cases := []struct {
		name     string
		text     string
		line     int
		problem  PatternProblem
		wantCode bool
	}{
		{
			name:    "no tab",
			text:    "# header\nlab-host\tfleet-node\nlab-host private-value",
			line:    3,
			problem: PatternNotClassTabRegex,
		},
		{
			name:    "unknown class",
			text:    "\nprivate-class\tprivate-value",
			line:    2,
			problem: PatternUnknownClass,
		},
		{
			name:    "a class is case-sensitive",
			text:    "Lab-Host\tprivate-value",
			line:    1,
			problem: PatternUnknownClass,
		},
		{
			name:    "only /i is a suffix",
			text:    "lab-host/x\tprivate-value",
			line:    1,
			problem: PatternUnknownClass,
		},
		{
			name:    "leading space is part of the class",
			text:    " lab-host\tprivate-value",
			line:    1,
			problem: PatternUnknownClass,
		},
		{
			name:    "empty regex",
			text:    "lab-host\tfleet-node\nowner-path\t",
			line:    2,
			problem: PatternEmptyRegex,
		},
		{
			name:    "empty regex once the carriage return is stripped",
			text:    "lab-host/i\t\r\n",
			line:    1,
			problem: PatternEmptyRegex,
		},
		{
			name:     "unbalanced group",
			text:     "# one\n# two\n\nlab-address\tprivate-value(",
			line:     4,
			problem:  PatternBadRegex,
			wantCode: true,
		},
		{
			name:     "trailing backslash",
			text:     "owner-account/i\tprivate-value\\",
			line:     1,
			problem:  PatternBadRegex,
			wantCode: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			patterns, err := ParsePatterns(tc.text)
			if patterns != nil {
				t.Errorf("a failed parse returned %d patterns", len(patterns))
			}
			var patternErr *PatternError
			if !errors.As(err, &patternErr) {
				t.Fatalf("err = %v, want *PatternError", err)
			}
			if patternErr.Line != tc.line {
				t.Errorf("line = %d, want %d", patternErr.Line, tc.line)
			}
			if patternErr.Problem != tc.problem {
				t.Errorf("problem = %q, want %q", patternErr.Problem, tc.problem)
			}
			if tc.wantCode && patternErr.Code == "" {
				t.Error("a compile failure carries no error code")
			}
			if strings.Contains(err.Error(), "private") {
				t.Errorf("the error quotes the line: %q", err.Error())
			}
		})
	}
}

func TestPatternsFromEnv(t *testing.T) {
	t.Run("unset or empty is no patterns", func(t *testing.T) {
		t.Setenv(PatternsEnv, "")
		_, err := PatternsFromEnv()
		var noPatterns *NoPatternsError
		if !errors.As(err, &noPatterns) {
			t.Fatalf("err = %v, want *NoPatternsError", err)
		}
		if noPatterns.Variable != PatternsEnv {
			t.Errorf("variable = %q, want %q", noPatterns.Variable, PatternsEnv)
		}
	})
	t.Run("comments only is no patterns", func(t *testing.T) {
		t.Setenv(PatternsEnv, "# nothing yet\n")
		var noPatterns *NoPatternsError
		if _, err := PatternsFromEnv(); !errors.As(err, &noPatterns) {
			t.Fatalf("err = %v, want *NoPatternsError", err)
		}
	})
	t.Run("a malformed line is a pattern error", func(t *testing.T) {
		t.Setenv(PatternsEnv, "lab-host\tok\nbroken")
		var patternErr *PatternError
		if _, err := PatternsFromEnv(); !errors.As(err, &patternErr) || patternErr.Line != 2 {
			t.Fatalf("err = %v, want *PatternError on line 2", err)
		}
	})
	t.Run("the fixture set", func(t *testing.T) {
		t.Setenv(PatternsEnv, fixturePatternText)
		patterns, err := PatternsFromEnv()
		if err != nil {
			t.Fatalf("PatternsFromEnv: %v", err)
		}
		if len(patterns) != len(fixturePatterns(t)) {
			t.Errorf("got %d patterns, want the fixture's %d", len(patterns), len(fixturePatterns(t)))
		}
	})
}

func TestScan_ReadsItsRulesFromTheEnvironment(t *testing.T) {
	root, files := writeTree(t, map[string]string{"docs/a.md": "Run it on fleet-node-07.\n"})

	t.Setenv(PatternsEnv, fixturePatternText)
	findings, err := Scan(root, files, nil)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(findings) != 1 || findings[0].Class != ClassLabHost {
		t.Fatalf("findings = %v, want the one lab-host value", findings)
	}

	t.Setenv(PatternsEnv, "")
	var noPatterns *NoPatternsError
	if _, err := Scan(root, files, nil); !errors.As(err, &noPatterns) {
		t.Fatalf("Scan with no rules = %v, want *NoPatternsError", err)
	}
}

// TestScanRepo_ReadsTheRulesBeforeListingAnything runs ScanRepo outside a work
// tree: with no rules the failure is the missing rules, not the missing index,
// so a misconfigured gate says what is actually wrong with it.
func TestScanRepo_ReadsTheRulesBeforeListingAnything(t *testing.T) {
	t.Setenv(PatternsEnv, "")
	var noPatterns *NoPatternsError
	if _, err := ScanRepo(t.TempDir()); !errors.As(err, &noPatterns) {
		t.Fatalf("ScanRepo with no rules = %v, want *NoPatternsError", err)
	}

	t.Setenv(PatternsEnv, fixturePatternText)
	var listErr *ListError
	if _, err := ScanRepo(t.TempDir()); !errors.As(err, &listErr) {
		t.Fatalf("ScanRepo outside a work tree = %v, want *ListError", err)
	}
}

func TestScanWith_RefusesAnEmptyPatternSet(t *testing.T) {
	root, files := writeTree(t, map[string]string{"a.txt": "fleet-node-07\n"})
	_, err := ScanWith(root, files, nil, nil)
	var noPatterns *NoPatternsError
	if !errors.As(err, &noPatterns) {
		t.Fatalf("err = %v, want *NoPatternsError", err)
	}
	if noPatterns.Variable != "" {
		t.Errorf("variable = %q for a set the caller passed", noPatterns.Variable)
	}
}

func TestScan_AZeroWidthMatchIsNotAFinding(t *testing.T) {
	patterns := mustParse(t, "lab-host\t"+`\b`+"\nlab-host\t"+`x*`)
	root, files := writeTree(t, map[string]string{"a.txt": "a b xx\n"})
	findings, err := ScanWith(root, files, nil, patterns)
	if err != nil {
		t.Fatalf("ScanWith: %v", err)
	}
	if len(findings) != 1 || findings[0].Match != "xx" {
		t.Fatalf("findings = %v, want only the non-empty match", findings)
	}
}

// sieveCases are expressions with strings each is known to match. They cover
// the shapes the real rules take and the corners of literal derivation: case
// folding, alternation, optional and repeated parts, and the letters Go's case
// folding equates with non-ASCII runes.
var sieveCases = []struct {
	expr    string
	fold    bool
	samples []string
}{
	{expr: `\b203\.0\.113\.\d{1,3}\b`, samples: []string{"at 203.0.113.51:8443", "203.0.113.7"}},
	{expr: `\bfleet-node-\d{2}\b`, fold: true, samples: []string{"FLEET-NODE-07 rebooted", "fleet-node-07"}},
	{expr: `\bfleethost(\.test)?\b`, fold: true, samples: []string{"on FleetHost", "http://fleethost.test:8077"}},
	{expr: `/home/example-owner\b`, samples: []string{ownerPathFixture + "/sources"}},
	{expr: `\bexample-owner\b`, fold: true, samples: []string{"example-owner@example.com", "signed off by EXAMPLE-OWNER"}},
	{expr: `alpha-\d|beta-\d`, samples: []string{"beta-1", "x alpha-2"}},
	{expr: `(?:alpha|beta)-node`, fold: true, samples: []string{"BETA-NODE", "alpha-node"}},
	{expr: `(?:node-){2}x`, samples: []string{"node-node-x"}},
	{expr: `a?bc`, samples: []string{"bc", "abc"}},
	{expr: `[0-9]+-only`, samples: []string{"12-only"}},
	{expr: `[a-z]{3}\d`, samples: []string{"abc1"}},
	{expr: `x(?i:ABC)`, samples: []string{"xabc", "xAbC"}},
	{expr: `kelvin`, fold: true, samples: []string{"Kelvin", "KELVIN"}},
	{expr: `ss`, fold: true, samples: []string{"ſſ", "SS"}},
	{expr: `grüße`, samples: []string{"viele grüße"}},
	{expr: `(?:fleet)+-box`, samples: []string{"fleetfleet-box"}},
	{expr: `\Qa.b\E`, samples: []string{"a.b"}},
}

// TestSieve_NeverHidesAMatch pins the sieve's one invariant: a string a rule
// matches always passes that rule's sieve. A sieve that broke it would skip the
// file, and the gate would pass while seeing nothing.
func TestSieve_NeverHidesAMatch(t *testing.T) {
	for _, tc := range sieveCases {
		t.Run(tc.expr, func(t *testing.T) {
			p := sievePattern(t, tc.expr, tc.fold)
			for _, sample := range tc.samples {
				if !p.Expr.MatchString(sample) {
					t.Fatalf("%q does not match its own sample %q", p.Expr, sample)
				}
				if len(candidates([]byte(sample), []Pattern{p}, nil)) != 1 {
					t.Errorf("%q matches %q but the sieve (%v) rules it out", p.Expr, sample, p.sieve)
				}
			}
		})
	}
}

// FuzzSieve_NeverHidesAMatch is the same invariant over expressions nobody
// thought of. The seeds run with every `go test`.
func FuzzSieve_NeverHidesAMatch(f *testing.F) {
	for _, tc := range sieveCases {
		for _, sample := range tc.samples {
			f.Add(tc.expr, sample, tc.fold)
		}
	}
	f.Fuzz(func(t *testing.T, expr, sample string, fold bool) {
		if fold {
			expr = foldFlag + expr
		}
		re, err := regexp.Compile(expr)
		if err != nil || !re.MatchString(sample) {
			return
		}
		p := Pattern{Class: ClassLabHost, Expr: re, sieve: deriveSieve(expr)}
		if len(candidates([]byte(sample), []Pattern{p}, nil)) != 1 {
			t.Fatalf("%q matches %q but the sieve (%v) rules it out", expr, sample, p.sieve)
		}
	})
}

// TestSieve_RulesOutCleanContent pins the other half: the sieve is worth having.
// Every fixture rule has a literal to search for, so a file with no lab value
// is settled without running a single expression; a rule with no literal is
// applied everywhere rather than nowhere.
func TestSieve_RulesOutCleanContent(t *testing.T) {
	patterns := fixturePatterns(t)
	for _, p := range patterns {
		if p.sieve == nil {
			t.Errorf("%q: no literal derived; every file would be scanned with it", p.Expr)
		}
	}
	clean := []byte("dial 198.51.100.7, run on agent-01, check /home/agent and /mnt/data\n")
	if live := candidates(clean, patterns, nil); len(live) != 0 {
		t.Errorf("clean content passed the sieve of %d rules", len(live))
	}

	noLiteral := sievePattern(t, `[a-z]{3}\d`, false)
	if noLiteral.sieve != nil {
		t.Fatalf("a literal was derived from a rule that has none: %v", noLiteral.sieve)
	}
	if len(candidates(clean, []Pattern{noLiteral}, nil)) != 1 {
		t.Error("a rule with no literal was ruled out instead of applied")
	}
}

func TestSieve_SkipsAcceptedClasses(t *testing.T) {
	patterns := fixturePatterns(t)
	live := candidates([]byte("fleet-node-07 and 203.0.113.7\n"), patterns, map[Class]bool{ClassLabHost: true})
	if len(live) != 1 || live[0].Class != ClassLabAddress {
		t.Fatalf("live = %v, want only the lab-address rule", live)
	}
}

// sievePattern builds one rule the way ParsePatterns would.
func sievePattern(t *testing.T, expr string, fold bool) Pattern {
	t.Helper()
	class := string(ClassLabHost)
	if fold {
		class += foldSuffix
	}
	return mustParse(t, class+"\t"+expr)[0]
}
