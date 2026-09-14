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

func renderedPrompt() PromptSpec {
	return PromptSpec{
		Name: "charter",
		Variables: []Variable{
			{Name: "name", Required: true},
			{Name: "note"},
		},
	}
}

func TestRender_SubstitutesDeclaredHolesAndDropsAnAbsentOptionalOne(t *testing.T) {
	got, err := Render("s", renderedPrompt(), "Hello ${name}.${note}", Vars{"name": "worker"})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if got != "Hello worker." {
		t.Fatalf("got %q", got)
	}
	got, err = Render("s", renderedPrompt(), "Hello ${name}.${note}", Vars{"name": "worker", "note": " Note."})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if got != "Hello worker. Note." {
		t.Fatalf("got %q", got)
	}
}

func TestRender_RefusesACallItCannotHonour(t *testing.T) {
	cases := []struct {
		name     string
		content  string
		vars     Vars
		sentinel error
		says     string
	}{
		{"undeclared hole", "Hello ${undeclared_hole}", Vars{"name": "w"}, ErrUnknownPlaceholder, "${undeclared_hole}"},
		{"required value withheld", "Hello ${name}", nil, ErrMissingVariable, "${name}"},
		{"value nothing declares", "Hello ${name}", Vars{"name": "w", "undeclared_hole": "w"}, ErrUnknownVariable, "undeclared_hole"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Render("s", renderedPrompt(), tc.content, tc.vars)
			if !errors.Is(err, tc.sentinel) {
				t.Fatalf("want %v, got %v", tc.sentinel, err)
			}
			if !strings.Contains(err.Error(), tc.says) {
				t.Fatalf("the refusal must say %q, got %q", tc.says, err.Error())
			}
		})
	}
}

// TestRender_NeverRescansWhatItSubstituted is the guard that keeps an agent's
// own words out of the template language: a reply quoting ${name} is text in
// the rendered prompt, not a hole that resolves.
func TestRender_NeverRescansWhatItSubstituted(t *testing.T) {
	got, err := Render("s", renderedPrompt(), "Hello ${name}.", Vars{"name": "${note} and ${name}"})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if got != "Hello ${note} and ${name}." {
		t.Fatalf("a substituted value must be carried verbatim, got %q", got)
	}
}

func TestSubstitute_LeavesTextThatIsNotAWellFormedHoleAlone(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    string
	}{
		{"no holes", "plain text", "plain text"},
		{"shell variable", "export ${HOME}", "export ${HOME}"},
		{"unterminated", "a ${name", "a ${name"},
		{"trailing dollar brace", "end ${", "end ${"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := substitute(tc.content, Vars{"name": "x"}); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestHash_IdentifiesABodyByContent(t *testing.T) {
	first := Hash("Hello worker.")
	if !strings.HasPrefix(first, HashPrefix) {
		t.Fatalf("a hash must name its digest, got %q", first)
	}
	if first != Hash("Hello worker.") {
		t.Fatal("the same body must hash the same twice")
	}
	if first == Hash("Hello worker. ") {
		t.Fatal("a body that changed by one byte must hash differently")
	}
	if Hash("") == Hash("x") {
		t.Fatal("the empty body must not collide")
	}
}
