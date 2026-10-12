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
	"context"
	"errors"
	"testing"
)

// refFor returns the version ref a resolved set recorded for one prompt.
func refFor(t *testing.T, set *ResolvedSet, name string) VersionRef {
	t.Helper()
	for _, ref := range set.Refs() {
		if ref.Name == name {
			return ref
		}
	}
	t.Fatalf("no ref for %s in %+v", name, set.Refs())
	return VersionRef{}
}

func TestResolver_NearestEditedLayerWins(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()
	resolver := NewResolver(f.store)
	resolve := func() *ResolvedSet {
		t.Helper()
		set, err := resolver.Resolve(ctx, testTenant, testProject, testSet)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		return set
	}

	// Nothing stored: the embedded default.
	set := resolve()
	if body, getErr := set.Get(testCharter); getErr != nil || body != storeTestSpec().Prompts[0].Default {
		t.Fatalf("embedded body = %q", body)
	}
	if ref := refFor(t, set, testCharter); ref.Scope != ScopeEmbedded || ref.Version != 0 || ref.Hash != Hash(storeTestSpec().Prompts[0].Default) {
		t.Fatalf("embedded ref = %+v", ref)
	}

	layers := []struct {
		ref   Ref
		body  string
		scope Scope
		id    string
	}{
		{charterRef(systemTenant(), ""), "system STOP:", ScopeSystem, systemTenant()},
		{charterRef(testTenant, ""), "tenant STOP:", ScopeTenant, testTenant},
		{charterRef(testTenant, testProject), "project STOP:", ScopeProject, testProject},
	}
	for _, layer := range layers {
		if _, err := f.store.Save(ctx, layer.ref, edit(layer.body, 0)); err != nil {
			t.Fatal(err)
		}
		set = resolve()
		if body, err := set.Get(testCharter); err != nil || body != layer.body {
			t.Fatalf("after writing %s: body=%q err=%v", layer.scope, body, err)
		}
		ref := refFor(t, set, testCharter)
		if ref.Scope != layer.scope || ref.ScopeID != layer.id || ref.Version != 1 || ref.Hash != Hash(layer.body) {
			t.Fatalf("after writing %s: ref=%+v", layer.scope, ref)
		}
	}
	// The untouched prompt still resolves to its default.
	if ref := refFor(t, set, testNote); ref.Scope != ScopeEmbedded {
		t.Fatalf("untouched prompt ref = %+v", ref)
	}
}

func TestResolver_AFollowingRecordIsSkipped(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()
	if _, err := f.store.Save(ctx, charterRef(systemTenant(), ""), edit("system STOP:", 0)); err != nil {
		t.Fatal(err)
	}
	seeded := Change{Stamp: Stamp{Author: "seed"}, Content: "stale copy STOP:", FollowParent: true}
	if _, err := f.store.Save(ctx, charterRef(testTenant, ""), seeded); err != nil {
		t.Fatal(err)
	}
	set, err := NewResolver(f.store).Resolve(ctx, testTenant, "", testSet)
	if err != nil {
		t.Fatal(err)
	}
	if body, getErr := set.Get(testCharter); getErr != nil || body != "system STOP:" {
		t.Fatalf("a following tenant copy answered: %q", body)
	}
	if ref := refFor(t, set, testCharter); ref.Scope != ScopeSystem {
		t.Fatalf("ref = %+v", ref)
	}
}

func TestResolver_TheSystemTenantResolvesItsOwnChain(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()
	resolver := NewResolver(f.store)
	if _, err := f.store.Save(ctx, charterRef(systemTenant(), ""), edit("system STOP:", 0)); err != nil {
		t.Fatal(err)
	}
	set, err := resolver.Resolve(ctx, systemTenant(), testProject, testSet)
	if err != nil {
		t.Fatal(err)
	}
	if ref := refFor(t, set, testCharter); ref.Scope != ScopeSystem || ref.ScopeID != systemTenant() {
		t.Fatalf("system tenant ref = %+v", ref)
	}
	if _, err := f.store.Save(ctx, charterRef(systemTenant(), testProject), edit("system project STOP:", 0)); err != nil {
		t.Fatal(err)
	}
	set, err = resolver.Resolve(ctx, systemTenant(), testProject, testSet)
	if err != nil {
		t.Fatal(err)
	}
	if ref := refFor(t, set, testCharter); ref.Scope != ScopeProject || ref.ScopeID != testProject {
		t.Fatalf("system tenant project ref = %+v", ref)
	}
	// No tenant at all: the system set.
	set, err = resolver.Resolve(ctx, "", "", testSet)
	if err != nil {
		t.Fatal(err)
	}
	if ref := refFor(t, set, testCharter); ref.Scope != ScopeSystem {
		t.Fatalf("tenantless ref = %+v", ref)
	}
}

func TestResolver_NilStoreResolvesEmbedded(t *testing.T) {
	own := NewRegistry()
	if _, err := own.Register(storeTestSpec()); err != nil {
		t.Fatal(err)
	}
	resolver := NewResolver(nil)
	resolver.registry = own
	set, err := resolver.Resolve(context.Background(), testTenant, testProject, testSet)
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range set.Refs() {
		if ref.Scope != ScopeEmbedded {
			t.Fatalf("ref = %+v", ref)
		}
	}
	if len(set.Refs()) != 2 || set.Kind() != testSet {
		t.Fatalf("resolved %s with %d refs", set.Kind(), len(set.Refs()))
	}
	if NewResolver(nil).registry != registry {
		t.Fatal("a nil store must resolve from the process registry")
	}
}

func TestResolver_FailuresAreResolveErrors(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()
	resolver := NewResolver(f.store)

	_, err := resolver.Resolve(ctx, testTenant, "", "nope")
	if !errors.Is(err, ErrResolve) || !errors.Is(err, ErrUnknownSet) {
		t.Fatalf("unknown set: %v", err)
	}

	f.resolver.fail(errors.New("barrier sealed"))
	_, err = resolver.Resolve(ctx, testTenant, testProject, testSet)
	var resolveErr *ResolveError
	if !errors.As(err, &resolveErr) || !errors.Is(err, ErrBackend) || resolveErr.ProjectID != testProject {
		t.Fatalf("sealed store: %v", err)
	}
	f.resolver.fail(nil)

	if _, err := resolver.Resolve(ctx, testTenant, "a/b", testSet); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("bad project id: %v", err)
	}

	// A stored body the current spec refuses — saved before the spec grew a
	// marker — refuses the launch by name instead of reaching an agent.
	f.plant(t, charterRef(testTenant, ""), &Record{TenantID: testTenant, Set: testSet, Name: testCharter, Content: "no marker", Version: 1})
	_, err = resolver.Resolve(ctx, testTenant, "", testSet)
	var missing *MissingMarkerError
	if !errors.As(err, &resolveErr) || !errors.As(err, &missing) || resolveErr.Prompt != testCharter {
		t.Fatalf("invalid stored body: %v", err)
	}
}

func TestResolvedSet_RendersOnlyWhatItDeclares(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()
	note := Ref{TenantID: testTenant, Set: testSet, Name: testNote}
	if _, err := f.store.Save(ctx, note, edit("Watch ${topic} closely.", 0)); err != nil {
		t.Fatal(err)
	}
	set, err := NewResolver(f.store).Resolve(ctx, testTenant, "", testSet)
	if err != nil {
		t.Fatal(err)
	}
	var renderer Renderer = set
	if got := renderer.MustRender(testNote, Vars{"topic": "the tests"}); got != "Watch the tests closely." {
		t.Fatalf("rendered %q", got)
	}
	if _, err := set.Render("nope", nil); !errors.Is(err, ErrUnknownPrompt) {
		t.Fatalf("render of an unknown prompt: %v", err)
	}
	if _, err := set.Get("nope"); !errors.Is(err, ErrUnknownPrompt) {
		t.Fatalf("get of an unknown prompt: %v", err)
	}
	refs := set.Refs()
	refs[0].Version = 99
	if set.Refs()[0].Version == 99 {
		t.Fatal("Refs exposes the set's own slice")
	}
	defer func() {
		if recover() == nil {
			t.Fatal("MustRender with an undeclared variable did not panic")
		}
	}()
	set.MustRender(testNote, Vars{"typo": "x"})
}

func TestRendererOr_PrefersAResolutionOfTheSameSet(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()
	embedded, known := f.store.registry.Lookup(testSet)
	if !known {
		t.Fatal("the fixture set is not registered")
	}
	resolved, err := NewResolver(f.store).Resolve(ctx, testTenant, "", testSet)
	if err != nil {
		t.Fatal(err)
	}

	if got := RendererOr(resolved, embedded); got != Renderer(resolved) {
		t.Fatalf("a resolution of the set was not preferred: %T", got)
	}
	if got := RendererOr(nil, embedded); got != Renderer(embedded) {
		t.Fatalf("no resolution did not fall back to the build: %T", got)
	}

	other := NewRegistry()
	spec := storeTestSpec()
	spec.Kind = "other"
	otherSet, err := other.Register(spec)
	if err != nil {
		t.Fatal(err)
	}
	if got := RendererOr(resolved, otherSet); got != Renderer(otherSet) {
		t.Fatal("a resolution of a different set was rendered in place of the build's")
	}
}
