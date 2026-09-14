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

import "strings"

// Validate judges a body against one prompt's contract.
//
// ONE VALIDATOR, TWO CALL SITES: registration, where a shipped default that
// fails is a build that must not boot, and save, where a tenant's edit that
// fails is refused by name. Holding both to the same four rules is what makes
// "the prompts are editable" a claim the engine can keep.
func Validate(kind SetKind, spec PromptSpec, content string) error {
	if strings.TrimSpace(content) == "" {
		return &BlankPromptError{Set: kind, Prompt: spec.Name}
	}
	present := placeholders(content)
	for _, name := range sortedKeys(present) {
		if _, declared := spec.Variable(name); !declared {
			return &UnknownPlaceholderError{Set: kind, Prompt: spec.Name, Placeholder: name}
		}
	}
	for _, variable := range spec.Variables {
		if !variable.Required {
			continue
		}
		if _, found := present[variable.Name]; !found {
			return &MissingVariableError{Set: kind, Prompt: spec.Name, Variable: variable.Name}
		}
	}
	for _, marker := range spec.Markers {
		if !strings.Contains(content, marker.Literal) {
			return &MissingMarkerError{
				Set: kind, Prompt: spec.Name,
				Marker: marker.Literal, Reason: marker.Reason,
			}
		}
	}
	return nil
}

// placeholderOpen and placeholderClose bracket a hole.
const (
	placeholderOpen  = "${"
	placeholderClose = "}"
)

// placeholders is every ${name} a body carries.
//
// A SEQUENCE THAT IS NOT A WELL-FORMED HOLE IS TEXT. A prompt discussing
// ${HOME} or writing "$5 {each}" has said something, not opened a hole
// nothing declared, and refusing it would make the validator the reason a
// prompt cannot say what it means.
func placeholders(content string) map[string]struct{} {
	found := make(map[string]struct{})
	rest := content
	for {
		start := strings.Index(rest, placeholderOpen)
		if start < 0 {
			return found
		}
		rest = rest[start+len(placeholderOpen):]
		end := strings.Index(rest, placeholderClose)
		if end < 0 {
			return found
		}
		if name := rest[:end]; validVariableName(name) {
			found[name] = struct{}{}
		}
		rest = rest[end+len(placeholderClose):]
	}
}
