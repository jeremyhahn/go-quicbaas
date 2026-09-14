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

import "sort"

// SetKind names one set of prompts. A set is owned by the package that sends
// its prompts, which is the package that knows what each one has to say.
type SetKind string

// String returns the kind's name.
func (k SetKind) String() string { return string(k) }

// Variable is one ${name} hole a prompt may carry.
type Variable struct {
	// Name is the identifier between ${ and }. Lowercase letters, digits and
	// underscores only, so a shell-style ${HOME} in a prompt's own prose is
	// text rather than a hole nothing declared.
	Name string

	// Description says what the engine puts here, for the person editing the
	// prompt.
	Description string

	// Required says the body must keep this hole. An optional variable may be
	// dropped by an edit — a prompt that stops naming the project is still a
	// prompt — while a required one carries the thing the prompt exists to
	// say.
	Required bool
}

// Marker is a literal the engine's own parser reads out of what an agent was
// told, or out of what the prompt produced.
//
// IT IS DECLARED BY THE OWNING PACKAGE, from that parser's own constants,
// never retyped here. A marker written out a second time is a marker that can
// drift from the thing that reads it, which is the failure the validator
// exists to catch.
type Marker struct {
	// Literal is the exact text that must appear in the body.
	Literal string

	// Reason names what reads it, so a refusal tells the editor what they
	// would have broken.
	Reason string
}

// PromptSpec declares one prompt: its shipped body and the contract an edited
// body must still meet.
type PromptSpec struct {
	// Name identifies the prompt within its set and is the base name of its
	// markdown file.
	Name string

	// Description says what the prompt is for, for the person editing it.
	Description string

	// Default is the shipped body: the markdown embedded in the binary, which
	// is the system tenant's content.
	Default string

	// Variables is every ${name} the body may carry.
	Variables []Variable

	// Markers is every literal the body must keep.
	Markers []Marker
}

// Variable returns the declaration of one variable.
func (p PromptSpec) Variable(name string) (Variable, bool) {
	for _, variable := range p.Variables {
		if variable.Name == name {
			return variable, true
		}
	}
	return Variable{}, false
}

// SetSpec declares a whole set.
type SetSpec struct {
	// Kind names the set.
	Kind SetKind

	// Description says what the set is for.
	Description string

	// Prompts is every prompt in it.
	Prompts []PromptSpec
}

// Prompt returns one prompt's specification.
func (s SetSpec) Prompt(name string) (PromptSpec, bool) {
	for _, prompt := range s.Prompts {
		if prompt.Name == name {
			return prompt, true
		}
	}
	return PromptSpec{}, false
}

// Names returns every prompt name in the set, sorted, so two reads of one set
// list identically.
func (s SetSpec) Names() []string {
	names := make([]string, 0, len(s.Prompts))
	for _, prompt := range s.Prompts {
		names = append(names, prompt.Name)
	}
	sort.Strings(names)
	return names
}

// checkSpec refuses a specification that describes nothing registrable, and
// validates every shipped default against its own contract.
//
// THE DEFAULT IS HELD TO THE RULE IT IMPOSES. A set whose own markdown would
// be refused on save is a set that ships broken and refuses the fix.
func checkSpec(spec SetSpec) error {
	if spec.Kind == "" {
		return &InvalidSpecError{Message: "a set must name its kind"}
	}
	if len(spec.Prompts) == 0 {
		return &InvalidSpecError{Set: spec.Kind, Message: "a set must declare at least one prompt"}
	}
	seen := make(map[string]struct{}, len(spec.Prompts))
	for _, prompt := range spec.Prompts {
		if prompt.Name == "" {
			return &InvalidSpecError{Set: spec.Kind, Message: "a prompt must be named"}
		}
		if _, duplicate := seen[prompt.Name]; duplicate {
			return &InvalidSpecError{Set: spec.Kind, Prompt: prompt.Name, Message: "declared twice"}
		}
		seen[prompt.Name] = struct{}{}
		if err := checkVariables(spec.Kind, prompt); err != nil {
			return err
		}
		if err := checkMarkers(spec.Kind, prompt); err != nil {
			return err
		}
		if err := Validate(spec.Kind, prompt, prompt.Default); err != nil {
			return err
		}
	}
	return nil
}

// checkVariables refuses a variable list that could not be matched against a
// body.
func checkVariables(kind SetKind, prompt PromptSpec) error {
	seen := make(map[string]struct{}, len(prompt.Variables))
	for _, variable := range prompt.Variables {
		if !validVariableName(variable.Name) {
			return &InvalidSpecError{
				Set: kind, Prompt: prompt.Name,
				Message: "variable name " + variable.Name +
					" must be lowercase letters, digits and underscores",
			}
		}
		if _, duplicate := seen[variable.Name]; duplicate {
			return &InvalidSpecError{
				Set: kind, Prompt: prompt.Name,
				Message: "variable " + variable.Name + " declared twice",
			}
		}
		seen[variable.Name] = struct{}{}
	}
	return nil
}

// checkMarkers refuses a marker that says nothing.
func checkMarkers(kind SetKind, prompt PromptSpec) error {
	for _, marker := range prompt.Markers {
		if marker.Literal == "" {
			return &InvalidSpecError{
				Set: kind, Prompt: prompt.Name,
				Message: "a marker must carry the literal it requires",
			}
		}
		if marker.Reason == "" {
			return &InvalidSpecError{
				Set: kind, Prompt: prompt.Name,
				Message: "marker " + marker.Literal + " must say what reads it",
			}
		}
	}
	return nil
}

// validVariableName reports whether a name is one the placeholder syntax can
// address.
func validVariableName(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '_':
		default:
			return false
		}
	}
	return true
}
