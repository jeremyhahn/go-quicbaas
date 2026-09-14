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
	"sync/atomic"
)

// Set is one registered set, resolved: every prompt's specification and the
// body that will be rendered.
//
// IT IS IMMUTABLE. Registration publishes it once and every reader shares the
// same value, so a prompt cannot change under a job that is part way through
// rendering it.
type Set struct {
	spec   SetSpec
	bodies map[string]string
}

// Kind returns the set's kind.
func (s *Set) Kind() SetKind { return s.spec.Kind }

// Spec returns the set's declaration.
func (s *Set) Spec() SetSpec { return s.spec }

// Names returns every prompt in the set, sorted.
func (s *Set) Names() []string { return s.spec.Names() }

// Body returns one prompt's body.
func (s *Set) Body(name string) (string, error) {
	body, known := s.bodies[name]
	if !known {
		return "", &UnknownPromptError{Set: s.spec.Kind, Prompt: name}
	}
	return body, nil
}

// Render renders one prompt.
func (s *Set) Render(name string, vars Vars) (string, error) {
	spec, known := s.spec.Prompt(name)
	if !known {
		return "", &UnknownPromptError{Set: s.spec.Kind, Prompt: name}
	}
	return Render(s.spec.Kind, spec, s.bodies[name], vars)
}

// MustRender renders one prompt or panics.
//
// FOR ASSEMBLERS WITH NO ERROR TO RETURN. The body is embedded and was
// validated against this very spec at registration, so the only way this
// fails is an assembler naming a variable the prompt does not declare or
// withholding one it requires — a defect in this binary, caught by the golden
// tests before it ships, not a condition a running node can be in. A node
// that could not render an agent's instructions has nothing to send it
// anyway.
func (s *Set) MustRender(name string, vars Vars) string {
	rendered, err := s.Render(name, vars)
	if err != nil {
		panic(err)
	}
	return rendered
}

// =============================================================================
// Registry
// =============================================================================

// Registry holds prompt sets by kind. Registration is append-only.
//
// THE MAP IS SWAPPED AS A WHOLE rather than locked: every job launch reads
// it and nothing writes it after init, so the read path is one atomic load
// and the writers pay the copy.
type Registry struct {
	sets atomic.Pointer[map[SetKind]*Set]
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry { return &Registry{} }

// Register validates a set's shipped defaults and publishes it.
//
// A second set claiming a kind is refused rather than replacing the first,
// because the loser of that race would be a set some package believes it
// owns and nothing can reach.
func (r *Registry) Register(spec SetSpec) (*Set, error) {
	if err := checkSpec(spec); err != nil {
		return nil, err
	}
	set := &Set{spec: spec, bodies: make(map[string]string, len(spec.Prompts))}
	for _, prompt := range spec.Prompts {
		set.bodies[prompt.Name] = prompt.Default
	}
	for {
		current := r.sets.Load()
		next := make(map[SetKind]*Set)
		if current != nil {
			if _, taken := (*current)[spec.Kind]; taken {
				return nil, &SetAlreadyRegisteredError{Set: spec.Kind}
			}
			for kind, registered := range *current {
				next[kind] = registered
			}
		}
		next[spec.Kind] = set
		if r.sets.CompareAndSwap(current, &next) {
			return set, nil
		}
	}
}

// Lookup returns the set registered under kind.
func (r *Registry) Lookup(kind SetKind) (*Set, bool) {
	current := r.sets.Load()
	if current == nil {
		return nil, false
	}
	set, known := (*current)[kind]
	return set, known
}

// All returns every registered set, ordered by kind so two reads of the same
// registry list identically.
func (r *Registry) All() []*Set {
	current := r.sets.Load()
	if current == nil {
		return nil
	}
	kinds := make([]string, 0, len(*current))
	for kind := range *current {
		kinds = append(kinds, string(kind))
	}
	sort.Strings(kinds)
	sets := make([]*Set, 0, len(kinds))
	for _, kind := range kinds {
		sets = append(sets, (*current)[SetKind(kind)])
	}
	return sets
}

// registry is the process registry: the one an owning package registers into
// from init() and the one a resolver reads.
var registry = NewRegistry()

// RegisterSet validates a set's shipped defaults and publishes it in the
// process registry.
func RegisterSet(spec SetSpec) (*Set, error) { return registry.Register(spec) }

// MustRegisterSet registers a set or panics.
//
// IT IS HOW A SET IS REGISTERED FROM init(). An embedded default that fails
// its own specification — a charter that no longer names the stop marker the
// runner parses, a body with a hole nothing declares — is a binary that
// would send agents instructions the engine cannot act on. It refuses to
// start, the way an unregisterable job runner does, rather than discovering
// it on somebody's issue.
func MustRegisterSet(spec SetSpec) *Set {
	set, err := RegisterSet(spec)
	if err != nil {
		panic(err)
	}
	return set
}

// LookupSet returns the set registered under kind.
func LookupSet(kind SetKind) (*Set, bool) { return registry.Lookup(kind) }

// Embedded returns the set registered under kind, or a typed error naming
// the kind nothing registered.
func Embedded(kind SetKind) (*Set, error) {
	set, known := LookupSet(kind)
	if !known {
		return nil, &UnknownSetError{Set: kind}
	}
	return set, nil
}

// Sets returns every registered set, ordered by kind.
func Sets() []*Set { return registry.All() }
