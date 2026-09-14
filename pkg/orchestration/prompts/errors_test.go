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

// TestErrors_EachOneSaysWhatItIsAndUnwrapsToItsSentinel walks the typed
// errors this package refuses with. Every one is read by a person editing a
// prompt or by a transport mapping a status, so neither the words nor the
// sentinel is incidental.
func TestErrors_EachOneSaysWhatItIsAndUnwrapsToItsSentinel(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		sentinel error
		says     []string
	}{
		{
			"invalid spec", &InvalidSpecError{Set: "sdlc", Prompt: "charter", Message: "declared twice"},
			ErrInvalidSpec, []string{"sdlc/charter", "declared twice"},
		},
		{
			"set already registered", &SetAlreadyRegisteredError{Set: "sdlc"},
			ErrSetAlreadyRegistered, []string{"sdlc"},
		},
		{"unknown set", &UnknownSetError{Set: "nope"}, ErrUnknownSet, []string{"nope"}},
		{
			"unknown prompt", &UnknownPromptError{Set: "sdlc", Prompt: "nope"},
			ErrUnknownPrompt, []string{"sdlc/nope"},
		},
		{
			"blank prompt", &BlankPromptError{Set: "sdlc", Prompt: "charter"},
			ErrBlankPrompt, []string{"sdlc/charter"},
		},
		{
			"unknown placeholder", &UnknownPlaceholderError{Set: "sdlc", Prompt: "charter", Placeholder: "undeclared_hole"},
			ErrUnknownPlaceholder, []string{"sdlc/charter", "${undeclared_hole}"},
		},
		{
			"missing variable", &MissingVariableError{Set: "sdlc", Prompt: "charter", Variable: "name"},
			ErrMissingVariable, []string{"${name}"},
		},
		{
			"unknown variable", &UnknownVariableError{Set: "sdlc", Prompt: "charter", Variable: "undeclared_hole"},
			ErrUnknownVariable, []string{"undeclared_hole"},
		},
		{
			"missing marker",
			&MissingMarkerError{Set: "sdlc", Prompt: "charter", Marker: "SDLC-STOP:", Reason: "the runner parses it"},
			ErrMissingMarker, []string{`"SDLC-STOP:"`, "the runner parses it"},
		},
		{
			"defaults unreadable", &DefaultsUnreadableError{Dir: "promptdefaults/sdlc", Cause: errors.New("no such file")},
			ErrDefaultsUnreadable, []string{"promptdefaults/sdlc", "no such file"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !errors.Is(tc.err, tc.sentinel) {
				t.Fatalf("%v must unwrap to %v", tc.err, tc.sentinel)
			}
			for _, want := range tc.says {
				if !strings.Contains(tc.err.Error(), want) {
					t.Fatalf("%q must say %q", tc.err.Error(), want)
				}
			}
		})
	}
}

func TestErrors_StayReadableWhenTheyKnowLess(t *testing.T) {
	if got := (&MissingMarkerError{Marker: "X"}).Error(); !strings.Contains(got, "(unnamed)") {
		t.Fatalf("an error about nothing in particular must still read: %q", got)
	}
	if got := (&InvalidSpecError{Set: "sdlc", Message: "no prompts"}).Error(); !strings.Contains(got, ": sdlc: no prompts") {
		t.Fatalf("a set-level refusal names the set alone: %q", got)
	}
	if got := (&UnknownPromptError{Prompt: "charter"}).Error(); !strings.Contains(got, "charter") {
		t.Fatalf("a prompt-level refusal names the prompt alone: %q", got)
	}
	if got := (&DefaultsUnreadableError{Dir: "d"}).Error(); strings.HasSuffix(got, ": ") {
		t.Fatalf("an unreadable-defaults error with no cause must not trail a colon: %q", got)
	}
}
