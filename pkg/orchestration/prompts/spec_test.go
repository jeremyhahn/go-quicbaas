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

// simpleSpec is a one-prompt set a test can register under its own kind.
func simpleSpec(kind SetKind) SetSpec {
	return SetSpec{
		Kind:        kind,
		Description: "a set for a test",
		Prompts: []PromptSpec{{
			Name:        "greeting",
			Description: "what the agent is told first",
			Default:     "Hello ${name}. Answer with READY.",
			Variables:   []Variable{{Name: "name", Description: "who", Required: true}},
			Markers:     []Marker{{Literal: "READY", Reason: "the handshake waits for READY"}},
		}},
	}
}

func TestRegisterSet_PublishesASetWithItsDefaults(t *testing.T) {
	set, err := RegisterSet(simpleSpec("register-ok"))
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if set.Kind() != "register-ok" {
		t.Fatalf("kind = %q", set.Kind())
	}
	if got := set.Names(); len(got) != 1 || got[0] != "greeting" {
		t.Fatalf("names = %v", got)
	}
	body, err := set.Body("greeting")
	if err != nil {
		t.Fatalf("body: %v", err)
	}
	if body != "Hello ${name}. Answer with READY." {
		t.Fatalf("body = %q", body)
	}
	if set.Spec().Description != "a set for a test" {
		t.Fatalf("spec lost its description: %+v", set.Spec())
	}
	looked, known := LookupSet("register-ok")
	if !known || looked != set {
		t.Fatalf("lookup returned %v known=%v", looked, known)
	}
	embedded, err := Embedded("register-ok")
	if err != nil || embedded != set {
		t.Fatalf("Embedded returned %v, %v", embedded, err)
	}
}

func TestRegisterSet_RefusesASecondSetClaimingTheKind(t *testing.T) {
	if _, err := RegisterSet(simpleSpec("register-twice")); err != nil {
		t.Fatalf("first registration: %v", err)
	}
	_, err := RegisterSet(simpleSpec("register-twice"))
	if !errors.Is(err, ErrSetAlreadyRegistered) {
		t.Fatalf("second registration must be refused, got %v", err)
	}
	var typed *SetAlreadyRegisteredError
	if !errors.As(err, &typed) || typed.Set != "register-twice" {
		t.Fatalf("the refusal must name the kind, got %v", err)
	}
}

func TestLookupSet_AnswersFalseForAKindNothingRegistered(t *testing.T) {
	if set, known := LookupSet("never-registered"); known {
		t.Fatalf("an unregistered kind must not resolve, got %v", set)
	}
	_, err := Embedded("never-registered")
	if !errors.Is(err, ErrUnknownSet) {
		t.Fatalf("Embedded must refuse an unknown kind, got %v", err)
	}
	var typed *UnknownSetError
	if !errors.As(err, &typed) || typed.Set != "never-registered" {
		t.Fatalf("the refusal must name the kind, got %v", err)
	}
}

func TestSets_ListsEveryRegisteredSetInKindOrder(t *testing.T) {
	for _, kind := range []SetKind{"zzz-list", "aaa-list"} {
		if _, err := RegisterSet(simpleSpec(kind)); err != nil {
			t.Fatalf("register %s: %v", kind, err)
		}
	}
	var seen []string
	for _, set := range Sets() {
		seen = append(seen, string(set.Kind()))
	}
	if len(seen) < 2 {
		t.Fatalf("Sets returned %v", seen)
	}
	for i := 1; i < len(seen); i++ {
		if seen[i-1] > seen[i] {
			t.Fatalf("Sets must be ordered by kind, got %v", seen)
		}
	}
}

func TestRegisterSet_RefusesEverySpecificationThatDescribesNothing(t *testing.T) {
	valid := PromptSpec{Name: "p", Default: "text"}
	cases := []struct {
		name string
		spec SetSpec
		want string
	}{
		{"no kind", SetSpec{Prompts: []PromptSpec{valid}}, "must name its kind"},
		{"no prompts", SetSpec{Kind: "empty-set"}, "at least one prompt"},
		{"unnamed prompt", SetSpec{Kind: "k1", Prompts: []PromptSpec{{Default: "text"}}}, "must be named"},
		{"prompt twice", SetSpec{Kind: "k2", Prompts: []PromptSpec{valid, valid}}, "declared twice"},
		{"bad variable name", SetSpec{Kind: "k3", Prompts: []PromptSpec{{
			Name: "p", Default: "text", Variables: []Variable{{Name: "Name"}},
		}}}, "lowercase letters"},
		{"variable twice", SetSpec{Kind: "k4", Prompts: []PromptSpec{{
			Name: "p", Default: "text", Variables: []Variable{{Name: "a"}, {Name: "a"}},
		}}}, "declared twice"},
		{"marker with no literal", SetSpec{Kind: "k5", Prompts: []PromptSpec{{
			Name: "p", Default: "text", Markers: []Marker{{Reason: "something reads it"}},
		}}}, "carry the literal"},
		{"marker with no reason", SetSpec{Kind: "k6", Prompts: []PromptSpec{{
			Name: "p", Default: "text", Markers: []Marker{{Literal: "text"}},
		}}}, "what reads it"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := RegisterSet(tc.spec)
			if !errors.Is(err, ErrInvalidSpec) {
				t.Fatalf("want an invalid-spec refusal, got %v", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("the refusal must say %q, got %q", tc.want, err.Error())
			}
		})
	}
}

func TestRegisterSet_HoldsAShippedDefaultToItsOwnContract(t *testing.T) {
	spec := SetSpec{Kind: "bad-default", Prompts: []PromptSpec{{
		Name:    "greeting",
		Default: "Hello.",
		Markers: []Marker{{Literal: "READY", Reason: "the handshake waits for READY"}},
	}}}
	_, err := RegisterSet(spec)
	var typed *MissingMarkerError
	if !errors.As(err, &typed) {
		t.Fatalf("a default missing its own marker must be refused, got %v", err)
	}
	if typed.Marker != "READY" || typed.Prompt != "greeting" {
		t.Fatalf("the refusal must name the marker and the prompt, got %+v", typed)
	}
}

func TestMustRegisterSet_ReturnsTheSetAndPanicsOnAnInvalidOne(t *testing.T) {
	if set := MustRegisterSet(simpleSpec("must-register")); set == nil {
		t.Fatal("MustRegisterSet returned nothing")
	}
	defer func() {
		recovered := recover()
		if recovered == nil {
			t.Fatal("an invalid set must panic in init rather than shipping")
		}
		if _, ok := recovered.(error); !ok {
			t.Fatalf("the panic must carry the typed error, got %T", recovered)
		}
	}()
	MustRegisterSet(SetSpec{Kind: "must-register-bad"})
}

func TestSetBody_RefusesAPromptTheSetDoesNotDeclare(t *testing.T) {
	set, err := RegisterSet(simpleSpec("unknown-prompt"))
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if _, err := set.Body("nothing"); !errors.Is(err, ErrUnknownPrompt) {
		t.Fatalf("want an unknown-prompt refusal, got %v", err)
	}
	_, err = set.Render("nothing", nil)
	var typed *UnknownPromptError
	if !errors.As(err, &typed) || typed.Prompt != "nothing" || typed.Set != "unknown-prompt" {
		t.Fatalf("the refusal must name the set and the prompt, got %v", err)
	}
}

func TestSetRender_RendersAndMustRenderPanicsOnABadCall(t *testing.T) {
	set, err := RegisterSet(simpleSpec("render-set"))
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	got, err := set.Render("greeting", Vars{"name": "worker"})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if got != "Hello worker. Answer with READY." {
		t.Fatalf("rendered %q", got)
	}
	if got := set.MustRender("greeting", Vars{"name": "worker"}); got != "Hello worker. Answer with READY." {
		t.Fatalf("MustRender produced %q", got)
	}
	defer func() {
		if recover() == nil {
			t.Fatal("MustRender must panic when the assembler withholds a required variable")
		}
	}()
	set.MustRender("greeting", nil)
}

func TestSpecLookups_AnswerWhatTheyDeclareAndNothingElse(t *testing.T) {
	spec := simpleSpec("lookups")
	prompt, known := spec.Prompt("greeting")
	if !known || prompt.Name != "greeting" {
		t.Fatalf("Prompt(greeting) = %+v, %v", prompt, known)
	}
	if _, known := spec.Prompt("absent"); known {
		t.Fatal("Prompt must not invent a declaration")
	}
	variable, declared := prompt.Variable("name")
	if !declared || !variable.Required {
		t.Fatalf("Variable(name) = %+v, %v", variable, declared)
	}
	if _, declared := prompt.Variable("absent"); declared {
		t.Fatal("Variable must not invent a declaration")
	}
}
