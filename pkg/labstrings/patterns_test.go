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
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp/syntax"
	"strings"
	"testing"
)

func TestNewPattern_DerivesTheNeedleAndGuidance(t *testing.T) {
	cases := []struct {
		expr       string
		wantNeedle string
		sample     string
	}{
		{`\b203\.0\.113\.\d{1,3}\b`, "203.0.113.", "at 203.0.113.9:80"},
		{`(?i)\bcore-\d{2}(\.example)?\b`, "core-", "CORE-01.example"},
		{`/mnt/lab-share\b`, "/mnt/lab-share", "/mnt/lab-share/x"},
		{`(?i)\bjdoe\b`, "jdoe", "signed off by JDOE"},
		{`(capture-wrapped)`, "capture-wrapped", "a capture-wrapped value"},
		{`x\d+longer-literal`, "longer-literal", "x12longer-literal"},
	}
	for _, tc := range cases {
		t.Run(tc.expr, func(t *testing.T) {
			p, err := NewPattern(ClassLabHost, tc.expr)
			if err != nil {
				t.Fatalf("NewPattern: %v", err)
			}
			if p.Needle != tc.wantNeedle {
				t.Errorf("needle = %q, want %q", p.Needle, tc.wantNeedle)
			}
			if !p.Expr.MatchString(tc.sample) {
				t.Errorf("%q does not match %q", tc.expr, tc.sample)
			}
			if !strings.Contains(strings.ToLower(tc.sample), p.Needle) {
				t.Errorf("sample %q does not contain needle %q: the sieve would skip it", tc.sample, p.Needle)
			}
			if p.Guidance != classGuidance[ClassLabHost] {
				t.Errorf("guidance = %q, want the class guidance", p.Guidance)
			}
		})
	}
}

func TestNewPattern_RefusesAnUnusablePattern(t *testing.T) {
	cases := []struct {
		name    string
		class   Class
		expr    string
		problem string
		syntax  bool
	}{
		{"unknown class", Class("fleet-host"), `core-\d{2}`, "unknown class", false},
		{"empty class", Class(""), `core-\d{2}`, "unknown class", false},
		{"bad regexp", ClassLabHost, `core-(`, "invalid regexp", true},
		{"alternation with no shared literal", ClassLabHost, `core-01|edge-01`, "no literal", false},
		{"pure character class", ClassLabAddress, `\d+`, "no literal", false},
		{"empty expression", ClassOwnerAccount, ``, "no literal", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewPattern(tc.class, tc.expr)
			var patternErr *PatternError
			if !errors.As(err, &patternErr) {
				t.Fatalf("err = %v, want *PatternError", err)
			}
			if !strings.Contains(patternErr.Error(), tc.problem) {
				t.Errorf("error %q does not report %q", patternErr.Error(), tc.problem)
			}
			var syntaxErr *syntax.Error
			if got := errors.As(err, &syntaxErr); got != tc.syntax {
				t.Errorf("unwraps to the compiler error = %v, want %v", got, tc.syntax)
			}
		})
	}
}

func TestSieveNeedle(t *testing.T) {
	needle, err := sieveNeedle(`(?i)\bCore-\d{2}\b`)
	if err != nil {
		t.Fatalf("sieveNeedle: %v", err)
	}
	if needle != "core-" {
		t.Errorf("needle = %q, want the lowercased literal core-", needle)
	}

	if _, err := sieveNeedle(`core-(`); err == nil {
		t.Error("sieveNeedle accepted an expression that does not parse")
	}
}

func TestMustPattern_PanicsOnABrokenBuiltIn(t *testing.T) {
	p := mustPattern(ClassOwnerPath, `/srv/x/[a-z]+`, "/srv/x/ok")
	if p.Needle != "/srv/x/" || len(p.Except) != 1 {
		t.Errorf("mustPattern = %+v, want needle /srv/x/ and one exception", p)
	}

	defer func() {
		if recover() == nil {
			t.Error("mustPattern accepted a pattern NewPattern refuses")
		}
	}()
	mustPattern(Class("nope"), `x`)
}

func TestDefaultPatterns_HoldNoClassThePublicCannotUse(t *testing.T) {
	for _, p := range DefaultPatterns {
		if _, known := classGuidance[p.Class]; !known {
			t.Errorf("%s: unknown class %q", p.Expr, p.Class)
		}
		for _, value := range p.Except {
			if !p.Expr.MatchString(value) {
				t.Errorf("%s: exception %q is not a match, so it excepts nothing", p.Expr, value)
			}
		}
	}
}

func TestLoadPatterns_CompilesEveryEntry(t *testing.T) {
	patterns, err := LoadPatterns(writePatternFile(t, privateFixture))
	if err != nil {
		t.Fatalf("LoadPatterns: %v", err)
	}
	want := []Class{ClassLabAddress, ClassLabHost, ClassOwnerPath, ClassOwnerAccount}
	if len(patterns) != len(want) {
		t.Fatalf("loaded %d patterns, want %d", len(patterns), len(want))
	}
	for i, p := range patterns {
		if p.Class != want[i] {
			t.Errorf("pattern %d class = %s, want %s", i, p.Class, want[i])
		}
		if p.Needle == "" || p.Guidance == "" {
			t.Errorf("pattern %d is missing its needle or guidance: %+v", i, p)
		}
	}

	empty, err := LoadPatterns(writePatternFile(t, "[]"))
	if err != nil {
		t.Fatalf("LoadPatterns on an empty list: %v", err)
	}
	if len(empty) != 0 {
		t.Errorf("an empty list loaded %d patterns", len(empty))
	}
}

func TestLoadPatterns_RefusesAnUnusableFile(t *testing.T) {
	cases := []struct {
		name    string
		content string
		check   func(t *testing.T, err error)
	}{
		{
			name:    "not JSON",
			content: "class: lab-host\n",
			check: func(t *testing.T, err error) {
				var syntaxErr *json.SyntaxError
				if !errors.As(err, &syntaxErr) {
					t.Errorf("err = %v, want a JSON syntax error inside", err)
				}
			},
		},
		{
			name:    "an object, not a list",
			content: `{"class": "lab-host", "expr": "core-"}`,
			check: func(t *testing.T, err error) {
				var typeErr *json.UnmarshalTypeError
				if !errors.As(err, &typeErr) {
					t.Errorf("err = %v, want a JSON type error inside", err)
				}
			},
		},
		{
			name:    "a misspelt key",
			content: `[{"class": "lab-host", "exp": "core-"}]`,
			check: func(t *testing.T, err error) {
				if !strings.Contains(err.Error(), "exp") {
					t.Errorf("err = %v, want it to name the unknown field", err)
				}
			},
		},
		{
			name:    "an unknown class",
			content: `[{"class": "fleet-host", "expr": "core-"}]`,
			check:   wantPatternProblem("unknown class"),
		},
		{
			name:    "a regexp that does not compile",
			content: `[{"class": "lab-host", "expr": "core-("}]`,
			check:   wantPatternProblem("invalid regexp"),
		},
		{
			name:    "a regexp with no literal",
			content: `[{"class": "lab-host", "expr": "a|b"}]`,
			check:   wantPatternProblem("no literal"),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writePatternFile(t, tc.content)
			_, err := LoadPatterns(path)
			var fileErr *PatternFileError
			if !errors.As(err, &fileErr) {
				t.Fatalf("err = %v, want *PatternFileError", err)
			}
			if fileErr.Path != path {
				t.Errorf("error names %q, want %q", fileErr.Path, path)
			}
			tc.check(t, err)
		})
	}

	t.Run("a missing file", func(t *testing.T) {
		_, err := LoadPatterns(filepath.Join(t.TempDir(), "absent.json"))
		var fileErr *PatternFileError
		if !errors.As(err, &fileErr) || !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("err = %v, want *PatternFileError wrapping fs.ErrNotExist", err)
		}
	})

	t.Run("a directory", func(t *testing.T) {
		_, err := LoadPatterns(t.TempDir())
		var fileErr *PatternFileError
		if !errors.As(err, &fileErr) {
			t.Fatalf("err = %v, want *PatternFileError", err)
		}
	})
}

// wantPatternProblem checks that err carries a *PatternError reporting problem.
func wantPatternProblem(problem string) func(t *testing.T, err error) {
	return func(t *testing.T, err error) {
		t.Helper()
		var patternErr *PatternError
		if !errors.As(err, &patternErr) {
			t.Fatalf("err = %v, want a *PatternError inside", err)
		}
		if patternErr.Problem != problem && !strings.HasPrefix(patternErr.Problem, problem) {
			t.Errorf("problem = %q, want %q", patternErr.Problem, problem)
		}
	}
}

func TestConfiguredPatterns_UnsetIsThePublicDefaultAlone(t *testing.T) {
	t.Setenv(PatternsEnv, "")
	set, err := ConfiguredPatterns(t.TempDir())
	if err != nil {
		t.Fatalf("ConfiguredPatterns: %v", err)
	}
	if len(set) != len(DefaultPatterns) {
		t.Fatalf("got %d patterns, want the %d defaults", len(set), len(DefaultPatterns))
	}
}

func TestConfiguredPatterns_AddsThePrivateFileToTheDefault(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "private.json"), []byte(privateFixture), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	for name, value := range map[string]string{
		"absolute path":                     filepath.Join(root, "private.json"),
		"path relative to the scanned root": "private.json",
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv(PatternsEnv, value)
			set, err := ConfiguredPatterns(root)
			if err != nil {
				t.Fatalf("ConfiguredPatterns: %v", err)
			}
			if len(set) != len(DefaultPatterns)+4 {
				t.Fatalf("got %d patterns, want the %d defaults plus 4", len(set), len(DefaultPatterns))
			}
			for i := range DefaultPatterns {
				if set[i].Expr != DefaultPatterns[i].Expr {
					t.Errorf("pattern %d is not the default it should be", i)
				}
			}
			if set[len(set)-1].Class != ClassOwnerAccount {
				t.Errorf("last pattern = %s, want the file's last entry", set[len(set)-1].Class)
			}
		})
	}
}

func TestConfiguredPatterns_AnUnusableFileIsAnError(t *testing.T) {
	root := t.TempDir()
	t.Setenv(PatternsEnv, "absent.json")
	_, err := ConfiguredPatterns(root)
	var fileErr *PatternFileError
	if !errors.As(err, &fileErr) {
		t.Fatalf("err = %v, want *PatternFileError", err)
	}
	if want := filepath.Join(root, "absent.json"); fileErr.Path != want {
		t.Errorf("error names %q, want the path resolved against the root, %q", fileErr.Path, want)
	}
}
