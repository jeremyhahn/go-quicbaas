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

package prompts

import (
	"errors"
	"strings"
	"testing"
)

// validatedPrompt is the specification every validator case is judged
// against: one required hole, one optional, one marker.
func validatedPrompt() PromptSpec {
	return PromptSpec{
		Name: "charter",
		Variables: []Variable{
			{Name: "name", Required: true},
			{Name: "note"},
		},
		Markers: []Marker{{Literal: "STOP:", Reason: "the runner parses the stop declaration"}},
	}
}

func TestValidate_AcceptsABodyThatMeetsItsContract(t *testing.T) {
	if err := Validate("s", validatedPrompt(), "Hello ${name}.\nEnd with STOP: <code>."); err != nil {
		t.Fatalf("a conforming body must be accepted: %v", err)
	}
	if err := Validate("s", validatedPrompt(), "Hello ${name}${note}. STOP:"); err != nil {
		t.Fatalf("an optional hole may be used: %v", err)
	}
}

func TestValidate_RefusesEachWayABodyCanBreakItsContract(t *testing.T) {
	cases := []struct {
		name     string
		content  string
		sentinel error
		says     string
	}{
		{"blank", "   \n\t ", ErrBlankPrompt, "charter"},
		{"undeclared hole", "Hello ${undeclared_hole}. STOP:", ErrUnknownPlaceholder, "${undeclared_hole}"},
		{"required hole dropped", "Hello. STOP:", ErrMissingVariable, "${name}"},
		{"marker dropped", "Hello ${name}.", ErrMissingMarker, `"STOP:"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := Validate("s", validatedPrompt(), tc.content)
			if !errors.Is(err, tc.sentinel) {
				t.Fatalf("want %v, got %v", tc.sentinel, err)
			}
			if !strings.Contains(err.Error(), tc.says) {
				t.Fatalf("the refusal must say %q, got %q", tc.says, err.Error())
			}
		})
	}
}

func TestValidate_AMissingMarkerSaysWhatReadsIt(t *testing.T) {
	err := Validate("sdlc", validatedPrompt(), "Hello ${name}.")
	var typed *MissingMarkerError
	if !errors.As(err, &typed) {
		t.Fatalf("want a missing-marker refusal, got %v", err)
	}
	if typed.Set != "sdlc" || typed.Prompt != "charter" || typed.Marker != "STOP:" {
		t.Fatalf("the refusal must identify the prompt and the marker: %+v", typed)
	}
	if !strings.Contains(typed.Error(), "the runner parses the stop declaration") {
		t.Fatalf("the refusal must say what would break: %q", typed.Error())
	}
}

func TestPlaceholders_ReadsOnlyWellFormedHoles(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    []string
	}{
		{"one hole", "a ${name} b", []string{"name"}},
		{"two holes", "${a}${b}", []string{"a", "b"}},
		{"repeated hole", "${a} and ${a}", []string{"a"}},
		{"shell variable is text", "run with ${HOME}", nil},
		{"unterminated is text", "a ${name", nil},
		{"no dollar brace is text", "a $5 {each}", nil},
		{"underscores and digits", "${head_sha1}", []string{"head_sha1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			found := placeholders(tc.content)
			got := sortedKeys(found)
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("got %v, want %v", got, tc.want)
				}
			}
		})
	}
}

func TestValidate_AShellStyleHoleIsTextRatherThanARefusal(t *testing.T) {
	spec := PromptSpec{Name: "p"}
	if err := Validate("s", spec, "export ${HOME} and keep going"); err != nil {
		t.Fatalf("a prompt may talk about ${HOME}: %v", err)
	}
}
