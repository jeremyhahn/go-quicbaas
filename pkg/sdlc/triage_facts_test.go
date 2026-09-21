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
	"strconv"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// TestExtractTriageProbes_ReadsWhatAnIssueNames proves the probes are the
// paths and identifiers a person would check first: paths with and without
// line suffixes, build files, backticked identifiers -- deduplicated, in
// order, with short common words and paths-in-backticks left out.
func TestExtractTriageProbes_ReadsWhatAnIssueNames(t *testing.T) {
	body := "The bug is in `pkg/luks2/kdf.go:62` (exported `DeriveKey`) and unlock.go:53.\n" +
		"See Makefile and Dockerfile.fips, plus docs/TESTING.md. `err` is ignored; `ctx` too.\n" +
		"`clearBytes` is called from pkg/luks2/kdf.go again, and `MFAPolicy.Required` matters.\n" +
		"`pkg/luks2/kdf.go` in backticks is a path, not an identifier."
	probes := ExtractTriageProbes("keyslot.go leaks", body)
	wantPaths := []string{"keyslot.go", "pkg/luks2/kdf.go", "unlock.go", "Makefile", "Dockerfile.fips", "docs/TESTING.md"}
	if strings.Join(probes.Paths, ",") != strings.Join(wantPaths, ",") {
		t.Fatalf("paths: want %v, got %v", wantPaths, probes.Paths)
	}
	wantIDs := []string{"DeriveKey", "clearBytes", "MFAPolicy.Required"}
	if strings.Join(probes.Identifiers, ",") != strings.Join(wantIDs, ",") {
		t.Fatalf("identifiers: want %v, got %v", wantIDs, probes.Identifiers)
	}
}

// TestExtractTriageProbes_IsBounded proves an issue that names forty files
// probes ten of them, and one that names nothing probes nothing.
func TestExtractTriageProbes_IsBounded(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 40; i++ {
		b.WriteString("pkg/f" + strings.Repeat("x", i%7) + strings.Repeat("y", i/7) + ".go `Ident" + strings.Repeat("z", i) + "` ")
	}
	probes := ExtractTriageProbes("", b.String())
	if len(probes.Paths) != triageFactMaxPaths || len(probes.Identifiers) != triageFactMaxIdentifiers {
		t.Fatalf("bounds: %d paths, %d identifiers", len(probes.Paths), len(probes.Identifiers))
	}
	empty := ExtractTriageProbes("Adopt Go 1.27", "Bump the toolchain portfolio-wide.")
	if len(empty.Paths) != 0 || len(empty.Identifiers) != 0 {
		t.Fatalf("an issue naming nothing probes nothing, got %+v", empty)
	}
}

// scriptedExec answers git commands from a map of path -> content, the way
// the fake container does, so the measurement can be tested on its own.
func scriptedExec(tree map[string]string, fail error) func(*orchestration.ExecSpec) (*orchestration.ExecResult, error) {
	return func(spec *orchestration.ExecSpec) (*orchestration.ExecResult, error) {
		if fail != nil {
			return nil, fail
		}
		sub := spec.Cmd[1]
		last := spec.Cmd[len(spec.Cmd)-1]
		switch sub {
		case "ls-files":
			if _, ok := tree[last]; ok {
				return &orchestration.ExecResult{Stdout: last + "\n"}, nil
			}
			return &orchestration.ExecResult{}, nil
		case "grep":
			var out strings.Builder
			for file, content := range tree {
				for n, line := range strings.Split(content, "\n") {
					if strings.Contains(line, last) {
						out.WriteString(file + ":" + strconv.Itoa(n+1) + ":" + line + "\n")
					}
				}
			}
			if out.Len() == 0 {
				return &orchestration.ExecResult{ExitCode: 1}, nil
			}
			return &orchestration.ExecResult{Stdout: out.String()}, nil
		}
		return &orchestration.ExecResult{ExitCode: 2, Stderr: "unmodelled"}, nil
	}
}

// TestMeasureTriageFacts_AsksGitAndKeepsTheAnswersApart proves a present
// path, an absent path, a found identifier and an unfound one each come back
// as what they are, bounded to three matches.
func TestMeasureTriageFacts_AsksGitAndKeepsTheAnswersApart(t *testing.T) {
	tree := map[string]string{
		"pkg/kdf.go": "package kdf\nfunc DeriveKey() {}\nvar _ = DeriveKey\nfunc use() { DeriveKey() }\nfunc more() { DeriveKey() }",
	}
	facts, err := MeasureTriageFacts(scriptedExec(tree, nil), "/workspace/p", "abc123", TriageProbes{
		Paths: []string{"pkg/kdf.go", "pkg/missing.go"}, Identifiers: []string{"DeriveKey", "NoSuchThing"},
	})
	if err != nil {
		t.Fatalf("MeasureTriageFacts: %v", err)
	}
	if !facts.Paths[0].Present || facts.Paths[1].Present {
		t.Fatalf("path presence: %+v", facts.Paths)
	}
	if !facts.Identifiers[0].Present || len(facts.Identifiers[0].Matches) != triageFactMaxMatches {
		t.Fatalf("a found identifier carries at most %d matches: %+v", triageFactMaxMatches, facts.Identifiers[0])
	}
	if facts.Identifiers[1].Present {
		t.Fatalf("an unfound identifier is not present: %+v", facts.Identifiers[1])
	}
	rendered := facts.Render()
	for _, want := range []string{"measured by the factory at abc123", "`pkg/kdf.go` — present", "`pkg/missing.go` — ABSENT", "`DeriveKey` — found", "`NoSuchThing` — no match"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered facts must carry %q:\n%s", want, rendered)
		}
	}
}

// TestMeasureTriageFacts_RefusesAHalfMeasurement proves an exec failure and a
// git error both fail the whole measurement rather than reporting partial
// facts, and that nothing probed renders nothing.
func TestMeasureTriageFacts_RefusesAHalfMeasurement(t *testing.T) {
	if _, err := MeasureTriageFacts(scriptedExec(nil, errors.New("container gone")), "/w", "h", TriageProbes{Paths: []string{"a.go"}}); err == nil {
		t.Fatal("an exec failure must fail the measurement")
	}
	broken := func(*orchestration.ExecSpec) (*orchestration.ExecResult, error) {
		return &orchestration.ExecResult{ExitCode: 128, Stderr: "fatal: not a git repository"}, nil
	}
	var cmdErr *CheckoutCommandError
	if _, err := MeasureTriageFacts(broken, "/w", "h", TriageProbes{Identifiers: []string{"Thing"}}); !errors.As(err, &cmdErr) {
		t.Fatalf("a git error is typed, got %v", err)
	}
	if got := (TriageFacts{Head: "h"}).Render(); got != "" {
		t.Fatalf("nothing probed renders nothing, got %q", got)
	}
}
