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
	"strings"
	"testing"
)

func fullLaneBriefSpec() LaneBriefSpec {
	return LaneBriefSpec{
		Objective:    "port the brief rules",
		Context:      "The factory hands a worker only the issue contract.",
		Scope:        []string{"render a brief", "validate a brief"},
		DoneWhen:     []string{"the tests pass"},
		Route:        "row/2359-lane-brief",
		SpecRow:      "factory briefs",
		TurnBudget:   120,
		FilesEdited:  []string{"pkg/sdlc/lane_brief.go", "pkg/sdlc/errors.go"},
		FilesToRead:  []string{"pkg/sdlc/contract.go"},
		ExpectedEdit: "a renderer and a validator",
		Acceptance:   "go test ./pkg/sdlc/ passes",
	}
}

// validBrief is a hand-written brief that passes ValidateLaneBrief(_, true).
const validBrief = `# Lane brief: x

Turn budget: 100.

## File map

Files this lane edits: a.go
Files to read first: b.go

Expected edit: one
Acceptance: two
Resume reason: three
`

func TestRenderLaneBrief_FullSpecRendersEverySectionInOrder(t *testing.T) {
	text, err := RenderLaneBrief(fullLaneBriefSpec())
	if err != nil {
		t.Fatalf("RenderLaneBrief: %v", err)
	}
	want := []string{
		"# Lane brief: port the brief rules\n",
		"\nTurn budget: 120. Wrap-up at turn 110.\n",
		"\n## Objective\n\nport the brief rules\n",
		"\n## Context\n\nThe factory hands a worker only the issue contract.\n",
		"\n## Scope\n\n- render a brief\n- validate a brief\n",
		"\n## Done when\n\n- the tests pass\n",
		"\nRoute: row/2359-lane-brief\n",
		"SPEC row: factory briefs\n",
		"\n## File map\n\n",
		"Files this lane edits: pkg/sdlc/lane_brief.go; pkg/sdlc/errors.go\n",
		"Files to read first: pkg/sdlc/contract.go\n",
		"\nExpected edit: a renderer and a validator\n",
		"Acceptance: go test ./pkg/sdlc/ passes\n",
	}
	if !strings.HasPrefix(text, want[0]) {
		t.Fatalf("brief does not start with the title:\n%s", text)
	}
	at := 0
	for _, w := range want {
		i := strings.Index(text[at:], w)
		if i < 0 {
			t.Fatalf("brief lacks %q after offset %d:\n%s", w, at, text)
		}
		at += i + len(w)
	}
	for _, absent := range []string{"Owner words:", "Resume reason:"} {
		if strings.Contains(text, absent) {
			t.Fatalf("brief carries %q:\n%s", absent, text)
		}
	}
	if err := ValidateLaneBrief(text, false); err != nil {
		t.Fatalf("rendered brief does not validate: %v", err)
	}
}

func TestRenderLaneBrief_Traces(t *testing.T) {
	cases := []struct {
		name, specRow, ownerWords string
		want, absent              []string
	}{
		{"spec row only", "row 7", "", []string{"SPEC row: row 7\n"}, []string{"Owner words:"}},
		{"owner words only", "", "do it\nnow", []string{"Owner words: do it now\n"}, []string{"SPEC row:"}},
		{"both", "row 7", "do it", []string{"SPEC row: row 7\nOwner words: do it\n"}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := fullLaneBriefSpec()
			spec.SpecRow, spec.OwnerWords = tc.specRow, tc.ownerWords
			text, err := RenderLaneBrief(spec)
			if err != nil {
				t.Fatalf("RenderLaneBrief: %v", err)
			}
			for _, w := range tc.want {
				if !strings.Contains(text, w) {
					t.Fatalf("brief lacks %q:\n%s", w, text)
				}
			}
			for _, a := range tc.absent {
				if strings.Contains(text, a) {
					t.Fatalf("brief carries %q:\n%s", a, text)
				}
			}
		})
	}
}

func TestRenderLaneBrief_UntracedIsRefused(t *testing.T) {
	spec := fullLaneBriefSpec()
	spec.SpecRow, spec.OwnerWords = "  ", "\n"
	text, err := RenderLaneBrief(spec)
	if !errors.Is(err, ErrUntracedBrief) {
		t.Fatalf("err = %v, want ErrUntracedBrief", err)
	}
	var untraced *UntracedBriefError
	if !errors.As(err, &untraced) || untraced.Objective != "port the brief rules" {
		t.Fatalf("err = %#v, want *UntracedBriefError naming the objective", err)
	}
	if text != "" {
		t.Fatalf("refused brief rendered text: %q", text)
	}
}

func TestRenderLaneBrief_MultiLineAcceptanceRendersOnOneLine(t *testing.T) {
	spec := fullLaneBriefSpec()
	spec.Acceptance = "go test passes;\n  go build exits 0\n"
	spec.ExpectedEdit = "one\r\ntwo"
	text, err := RenderLaneBrief(spec)
	if err != nil {
		t.Fatalf("RenderLaneBrief: %v", err)
	}
	for _, w := range []string{"\nAcceptance: go test passes; go build exits 0\n", "\nExpected edit: one two\n"} {
		if !strings.Contains(text, w) {
			t.Fatalf("brief lacks %q:\n%s", w, text)
		}
	}
}

func TestRenderLaneBrief_ResumeCarriesResumeReason(t *testing.T) {
	spec := fullLaneBriefSpec()
	spec.ResumeReason = "the budget ran out"
	text, err := RenderLaneBrief(spec)
	if err != nil {
		t.Fatalf("RenderLaneBrief: %v", err)
	}
	if !strings.HasSuffix(text, "\nAcceptance: go test ./pkg/sdlc/ passes\nResume reason: the budget ran out\n") {
		t.Fatalf("brief does not end with the resume reason:\n%s", text)
	}
	if err := ValidateLaneBrief(text, true); err != nil {
		t.Fatalf("resume brief does not validate: %v", err)
	}
}

func TestRenderLaneBrief_SmallBudgetHasNoWrapUp(t *testing.T) {
	spec := fullLaneBriefSpec()
	spec.TurnBudget = 10
	text, err := RenderLaneBrief(spec)
	if err != nil {
		t.Fatalf("RenderLaneBrief: %v", err)
	}
	if !strings.Contains(text, "\nTurn budget: 10.\n") {
		t.Fatalf("brief lacks a bare turn budget:\n%s", text)
	}
}

func TestRenderLaneBrief_MissingFieldIsRefused(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*LaneBriefSpec)
		want   []string
	}{
		{"expected edit", func(s *LaneBriefSpec) { s.ExpectedEdit = " " }, []string{"Expected edit"}},
		{"turn budget", func(s *LaneBriefSpec) { s.TurnBudget = 0 }, []string{"Turn budget"}},
		{"file lists", func(s *LaneBriefSpec) { s.FilesEdited, s.FilesToRead = nil, []string{""} },
			[]string{"Files this lane edits", "Files to read first"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := fullLaneBriefSpec()
			tc.mutate(&spec)
			text, err := RenderLaneBrief(spec)
			if !errors.Is(err, ErrBriefIncomplete) {
				t.Fatalf("err = %v, want ErrBriefIncomplete", err)
			}
			var missing *MissingBriefLineError
			if !errors.As(err, &missing) || !reflect.DeepEqual(missing.Missing, tc.want) {
				t.Fatalf("err = %v, want missing %v", err, tc.want)
			}
			if text != "" {
				t.Fatalf("refused brief rendered text: %q", text)
			}
		})
	}
}

func TestValidateLaneBrief_Gaps(t *testing.T) {
	replace := func(old, repl string) string {
		if !strings.Contains(validBrief, old) {
			t.Fatalf("validBrief lacks %q", old)
		}
		return strings.Replace(validBrief, old, repl, 1)
	}
	cases := []struct {
		name   string
		text   string
		resume bool
		want   []string
	}{
		{"valid", validBrief, true, nil},
		{"valid without resume", replace("Resume reason: three\n", ""), false, nil},
		{"turn budget missing", replace("Turn budget: 100.\n", ""), true, []string{"Turn budget"}},
		{"turn budget not a number", replace("Turn budget: 100.", "Turn budget: about 100"), true, []string{"Turn budget"}},
		{"turn budget parenthetical", replace("Turn budget: 100.", "Turn budget: 100 (tool calls)"), true, []string{"Turn budget"}},
		{"turn budget placeholder", replace("Turn budget: 100.", "Turn budget: <n>"), true, []string{"Turn budget"}},
		{"turn budget bare number", replace("Turn budget: 100.", "Turn budget: 100"), true, nil},
		{"file map missing", replace("## File map\n", ""), true, []string{"## File map"}},
		{"files edited missing", replace("Files this lane edits: a.go\n", ""), true, []string{"Files this lane edits"}},
		{"files to read empty", replace("Files to read first: b.go", "Files to read first:   "), true, []string{"Files to read first"}},
		{"expected edit placeholder", replace("Expected edit: one", "Expected edit: <one-line edit>"), true, []string{"Expected edit"}},
		{"acceptance empty", replace("Acceptance: two", "Acceptance:"), true, []string{"Acceptance"}},
		{"acceptance indented is not at line start", replace("Acceptance: two", "  Acceptance: two"), true, []string{"Acceptance"}},
		{"resume reason missing on resume", replace("Resume reason: three\n", ""), true, []string{"Resume reason"}},
		{"resume reason placeholder on resume", replace("Resume reason: three", "Resume reason: <why>"), true, []string{"Resume reason"}},
		{"resume reason not needed without resume", replace("Resume reason: three", "Resume reason: <why>"), false, nil},
		{"first occurrence is read", replace("Acceptance: two\n", "Acceptance: <x>\nAcceptance: two\n"), true, []string{"Acceptance"}},
		{"crlf", strings.ReplaceAll(validBrief, "\n", "\r\n"), true, nil},
		{"all gaps in order", "# Lane brief: x\n", true, []string{
			"Turn budget", "## File map", "Files this lane edits", "Files to read first",
			"Expected edit", "Acceptance", "Resume reason",
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateLaneBrief(tc.text, tc.resume)
			if tc.want == nil {
				if err != nil {
					t.Fatalf("ValidateLaneBrief: %v", err)
				}
				return
			}
			if !errors.Is(err, ErrBriefIncomplete) {
				t.Fatalf("err = %v, want ErrBriefIncomplete", err)
			}
			var missing *MissingBriefLineError
			if !errors.As(err, &missing) || !reflect.DeepEqual(missing.Missing, tc.want) {
				t.Fatalf("err = %v, want missing %v", err, tc.want)
			}
			if !strings.Contains(err.Error(), strings.Join(tc.want, ", ")) {
				t.Fatalf("message %q does not list %v", err.Error(), tc.want)
			}
		})
	}
}
