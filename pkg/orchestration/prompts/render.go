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
	"sort"
	"strings"
)

// Vars are the values one rendering substitutes, keyed by variable name.
type Vars map[string]string

// Render substitutes vars into a body.
//
// SUBSTITUTION, NOT EVALUATION. Every ${x} the body carries must be declared,
// every required variable must have a value, and every value the caller
// supplies must name a variable the prompt declares — a typo in an assembler
// is an error here rather than a hole that renders as silence in what an
// agent is told. An optional variable with no value renders as nothing, which
// is how a clause the engine has nothing to say for disappears.
//
// A SUBSTITUTED VALUE IS NEVER RESCANNED. Text an agent wrote can therefore
// not open a hole of its own.
func Render(kind SetKind, spec PromptSpec, content string, vars Vars) (string, error) {
	present := placeholders(content)
	for _, name := range sortedKeys(present) {
		if _, declared := spec.Variable(name); !declared {
			return "", &UnknownPlaceholderError{Set: kind, Prompt: spec.Name, Placeholder: name}
		}
	}
	for _, name := range sortedVarKeys(vars) {
		if _, declared := spec.Variable(name); !declared {
			return "", &UnknownVariableError{Set: kind, Prompt: spec.Name, Variable: name}
		}
	}
	for _, variable := range spec.Variables {
		if !variable.Required {
			continue
		}
		if _, supplied := vars[variable.Name]; !supplied {
			return "", &MissingVariableError{Set: kind, Prompt: spec.Name, Variable: variable.Name}
		}
	}
	return substitute(content, vars), nil
}

// substitute replaces every well-formed hole with its value, left to right,
// scanning the body once so no value can be rescanned as a hole.
func substitute(content string, vars Vars) string {
	var b strings.Builder
	b.Grow(len(content))
	rest := content
	for {
		start := strings.Index(rest, placeholderOpen)
		if start < 0 {
			b.WriteString(rest)
			return b.String()
		}
		b.WriteString(rest[:start])
		after := rest[start+len(placeholderOpen):]
		end := strings.Index(after, placeholderClose)
		if end < 0 {
			b.WriteString(rest[start:])
			return b.String()
		}
		name := after[:end]
		if !validVariableName(name) {
			b.WriteString(placeholderOpen)
			rest = after
			continue
		}
		b.WriteString(vars[name])
		rest = after[end+len(placeholderClose):]
	}
}

// sortedKeys orders a placeholder set, so a body with two undeclared holes is
// refused on the same one every time.
func sortedKeys(set map[string]struct{}) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// sortedVarKeys orders supplied values for the same reason.
func sortedVarKeys(vars Vars) []string {
	keys := make([]string, 0, len(vars))
	for key := range vars {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
