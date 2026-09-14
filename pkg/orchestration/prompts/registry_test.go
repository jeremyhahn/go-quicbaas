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
	"sync"
	"testing"
)

// TestRegistry_AnEmptyOneAnswersNothingRatherThanPanicking covers the state
// the process registry is in before any owning package has run its init.
func TestRegistry_AnEmptyOneAnswersNothingRatherThanPanicking(t *testing.T) {
	empty := NewRegistry()
	if set, known := empty.Lookup("sdlc"); known {
		t.Fatalf("an empty registry resolves nothing, got %v", set)
	}
	if sets := empty.All(); sets != nil {
		t.Fatalf("an empty registry lists nothing, got %v", sets)
	}
}

func TestRegistry_RegistersLooksUpAndLists(t *testing.T) {
	r := NewRegistry()
	if _, err := r.Register(simpleSpec("beta")); err != nil {
		t.Fatalf("register beta: %v", err)
	}
	if _, err := r.Register(simpleSpec("alpha")); err != nil {
		t.Fatalf("register alpha: %v", err)
	}
	if _, err := r.Register(simpleSpec("beta")); !errors.Is(err, ErrSetAlreadyRegistered) {
		t.Fatalf("a kind may be claimed once, got %v", err)
	}
	sets := r.All()
	if len(sets) != 2 || sets[0].Kind() != "alpha" || sets[1].Kind() != "beta" {
		t.Fatalf("All must list by kind, got %v", sets)
	}
	if _, known := r.Lookup("alpha"); !known {
		t.Fatal("a registered kind must resolve")
	}
	if _, known := r.Lookup("gamma"); known {
		t.Fatal("an unregistered kind must not resolve")
	}
}

// TestRegistry_ConcurrentRegistrationsAllLand proves the compare-and-swap
// loop does not lose a set to a racing one, which on a real registry would
// be a package believing it owns prompts nothing can reach.
func TestRegistry_ConcurrentRegistrationsAllLand(t *testing.T) {
	r := NewRegistry()
	kinds := []SetKind{"a", "b", "c", "d", "e", "f", "g", "h"}
	var wg sync.WaitGroup
	for _, kind := range kinds {
		wg.Add(1)
		go func(kind SetKind) {
			defer wg.Done()
			if _, err := r.Register(simpleSpec(kind)); err != nil {
				t.Errorf("register %s: %v", kind, err)
			}
		}(kind)
	}
	wg.Wait()
	if got := r.All(); len(got) != len(kinds) {
		t.Fatalf("registered %d sets, %d survived", len(kinds), len(got))
	}
}

func TestSetKind_String(t *testing.T) {
	if SetKind("sdlc").String() != "sdlc" {
		t.Fatal("a kind prints as its name")
	}
	if SetKind("").String() != "" {
		t.Fatal("an unnamed kind prints as nothing")
	}
}

func TestValidVariableName_AcceptsOnlyWhatThePlaceholderSyntaxAddresses(t *testing.T) {
	cases := map[string]bool{
		"name": true, "head_sha": true, "a1": true, "_": true,
		"": false, "Name": false, "head-sha": false, "head.sha": false, "héad": false,
	}
	for name, want := range cases {
		if got := validVariableName(name); got != want {
			t.Fatalf("validVariableName(%q) = %v, want %v", name, got, want)
		}
	}
}
