// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

package sdlc

import (
	"context"
	"errors"
	"testing"
)

func TestRepoHostGuard_AllowsListedHostsOnly(t *testing.T) {
	g, err := NewRepoHostGuard([]string{"Forge.Example.Test", "gitea.internal:3001"}, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, ok := range []string{
		"https://forge.example.test/acme/api.git",
		"ssh://git@forge.example.test/acme/api.git",
		"git@forge.example.test:acme/api.git",
		"https://gitea.internal:3001/acme/api.git",
	} {
		if err := g.CheckRepoURL(ok); err != nil {
			t.Fatalf("%s refused: %v", ok, err)
		}
	}
	for _, bad := range []string{
		"",
		"https://github.com/acme/api.git",
		"ssh://git@other.test/acme/api.git",
		"http://forge.example.test/acme/api.git",
		"file:///srv/repo.git",
		"/srv/repo.git",
		"://nope",
		"https:///no-host",
	} {
		err := g.CheckRepoURL(bad)
		var refused *RepoURLRefusedError
		if !errors.As(err, &refused) || !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("%q accepted or wrongly typed: %v", bad, err)
		}
	}
}

func TestRepoHostGuard_LocalReposAreOptIn(t *testing.T) {
	local, err := NewRepoHostGuard(nil, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, ok := range []string{"file:///srv/repo.git", "/srv/repo.git"} {
		if err := local.CheckRepoURL(ok); err != nil {
			t.Fatalf("%s refused with local repos allowed: %v", ok, err)
		}
	}
	if err := local.CheckRepoURL("https://forge.example.test/x.git"); err == nil {
		t.Fatal("allowing local repositories must not allow every host")
	}
	if _, err := NewRepoHostGuard([]string{" "}, false); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("empty host entry accepted: %v", err)
	}
}

func TestModelFamily_ByBackendThenModel(t *testing.T) {
	cases := map[[2]string]string{
		{"claude", "whatever"}:           ModelFamilyAnthropic,
		{"claude-code", ""}:              ModelFamilyAnthropic,
		{"codex", "gpt-5"}:               ModelFamilyOpenAI,
		{"ollama", "anthropic/claude-x"}: ModelFamilyAnthropic,
		{"ollama", "openai/gpt-5.5"}:     ModelFamilyOpenAI,
		{"ollama", "claude-opus-5"}:      ModelFamilyAnthropic,
		{"ollama", "gpt-5.5"}:            ModelFamilyOpenAI,
		{"ollama", "vendor/gpt-5.5"}:     ModelFamilyOpenAI,
		{"ollama", "llama3"}:             "",
	}
	for in, want := range cases {
		if got := ModelFamily(in[0], in[1]); got != want {
			t.Fatalf("ModelFamily(%q, %q) = %q, want %q", in[0], in[1], got, want)
		}
	}
}

func TestPriorityInputs_ComputedSaturates(t *testing.T) {
	if got := (PriorityInputs{}).Computed(); got != 0 {
		t.Fatalf("zero inputs = %d", got)
	}
	if got := (PriorityInputs{BlastRadius: 2, ProjectDefault: 5}).Computed(); got != 25 {
		t.Fatalf("2 dependents + 5 = %d", got)
	}
	if got := (PriorityInputs{BlastRadius: 1_000_000}).Computed(); got != ComputedCeiling {
		t.Fatalf("huge radius = %d, want the ceiling", got)
	}
	if got := (PriorityInputs{BlastRadius: -5, ProjectDefault: -1_000_000}).Computed(); got != ComputedFloor {
		t.Fatalf("negative inputs = %d, want the floor", got)
	}
}

type staticBindings struct {
	bindings []*ProjectBinding
	err      error
}

func (s *staticBindings) GetBinding(_ context.Context, _, projectID string) (*ProjectBinding, error) {
	if s.err != nil {
		return nil, s.err
	}
	for _, b := range s.bindings {
		if b.ProjectID == projectID {
			return b, nil
		}
	}
	return nil, &NotFoundError{Entity: bindingEntityName, ID: projectID}
}

func (s *staticBindings) ListBindings(context.Context, string) ([]*ProjectBinding, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.bindings, nil
}

func TestRanker_BlastRadiusFromDeclaredDependents(t *testing.T) {
	api := testBinding()
	api.Ranking = RankingPolicy{DefaultPriority: 3, AnalyzerName: DeclaredAnalyzerName, DeclaredDependents: []string{"web", "cli"}}
	web := testBinding()
	web.ProjectID = "web"
	ranker, err := NewRanker(&staticBindings{bindings: []*ProjectBinding{api, web}})
	if err != nil {
		t.Fatal(err)
	}
	inputs, err := ranker.Inputs(context.Background(), testTenant, api)
	if err != nil {
		t.Fatal(err)
	}
	if inputs.BlastRadius != 2 || inputs.ProjectDefault != 3 || inputs.Computed() != 23 {
		t.Fatalf("inputs = %+v (computed %d)", inputs, inputs.Computed())
	}
	// A project nobody depends on ranks by its default alone.
	inputs, err = ranker.Inputs(context.Background(), testTenant, web)
	if err != nil {
		t.Fatal(err)
	}
	if inputs.BlastRadius != 0 {
		t.Fatalf("web blast radius = %d", inputs.BlastRadius)
	}
	if _, err := ranker.Inputs(context.Background(), testTenant, nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("nil binding: %v", err)
	}
	if _, err := NewRanker(nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("nil bindings: %v", err)
	}
	failing, _ := NewRanker(&staticBindings{err: errors.New("down")})
	if _, err := failing.Inputs(context.Background(), testTenant, api); err == nil {
		t.Fatal("listing failure swallowed")
	}
}
