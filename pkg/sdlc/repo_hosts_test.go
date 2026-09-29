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
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
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

func TestRepoHostGuard_HTTPNamesTheCleartextReason(t *testing.T) {
	g, err := NewRepoHostGuard([]string{"forge.example.test"}, false)
	if err != nil {
		t.Fatal(err)
	}
	err = g.CheckRepoURL("http://forge.example.test/acme/handbook")
	var refused *RepoURLRefusedError
	if !errors.As(err, &refused) {
		t.Fatalf("http accepted or wrongly typed: %v", err)
	}
	if !strings.Contains(refused.Reason, "cleartext") || !strings.Contains(refused.Reason, "ssh://") {
		t.Fatalf("reason does not say why http fails or what to use instead: %q", refused.Reason)
	}
}

func TestRepoHostGuard_CheckContextRepos(t *testing.T) {
	g, err := NewRepoHostGuard([]string{"forge.example.test", "other.example.test"}, false)
	if err != nil {
		t.Fatal(err)
	}
	const sshProject = "ssh://git@forge.example.test:2222/acme/api.git"
	const httpsProject = "https://forge.example.test/acme/api.git"
	repo := func(url string) []orchestration.ContextRepo {
		return []orchestration.ContextRepo{{Name: "handbook", RepoURL: url, Ref: "main"}}
	}

	t.Run("accepted", func(t *testing.T) {
		for name, tc := range map[string]struct{ project, context string }{
			"https beside an ssh project":   {sshProject, "https://forge.example.test/acme/handbook.git"},
			"ssh on the project's ssh host": {sshProject, "ssh://git@forge.example.test:2222/acme/handbook.git"},
			"https beside an https project": {httpsProject, "https://other.example.test/acme/handbook.git"},
		} {
			if err := g.CheckContextRepos(tc.project, repo(tc.context)); err != nil {
				t.Errorf("%s: refused: %v", name, err)
			}
		}
		if err := g.CheckContextRepos(sshProject, nil); err != nil {
			t.Errorf("no context repos: %v", err)
		}
	})

	t.Run("refused", func(t *testing.T) {
		for name, tc := range map[string]struct{ project, context, text string }{
			"http":                     {sshProject, "http://forge.example.test/acme/handbook", "cleartext"},
			"host off the allowlist":   {sshProject, "https://github.com/acme/handbook.git", "not an allowed forge host"},
			"ssh on another host":      {sshProject, "ssh://git@other.example.test/acme/handbook.git", "pins only the project's ssh host"},
			"ssh on another port":      {sshProject, "ssh://git@forge.example.test/acme/handbook.git", "cannot be verified"},
			"ssh beside https project": {httpsProject, "git@forge.example.test:acme/handbook.git", "no host key is pinned"},
		} {
			err := g.CheckContextRepos(tc.project, repo(tc.context))
			var refused *ContextRepoRefusedError
			if !errors.As(err, &refused) || !errors.Is(err, ErrInvalidInput) {
				t.Errorf("%s: accepted or wrongly typed: %v", name, err)
				continue
			}
			if refused.Name != "handbook" || !strings.Contains(err.Error(), tc.text) {
				t.Errorf("%s: error %q does not name the repository and %q", name, err.Error(), tc.text)
			}
		}
	})
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

// TestRepoHostGuard_CheckProject covers the one check bind and every
// workspace open share: the project's own remote first, then each context
// repository against it.
func TestRepoHostGuard_CheckProject(t *testing.T) {
	g, err := NewRepoHostGuard([]string{"forge.example.test"}, false)
	if err != nil {
		t.Fatal(err)
	}
	project := func(remote, contextURL string) *orchestration.Project {
		p := &orchestration.Project{Git: orchestration.ProjectGit{RepoURL: remote}}
		if contextURL != "" {
			p.ContextRepos = []orchestration.ContextRepo{{Name: "handbook", RepoURL: contextURL, Ref: "main"}}
		}
		return p
	}

	if err := g.CheckProject(project("ssh://git@forge.example.test:2222/acme/api.git", "https://forge.example.test/acme/handbook.git")); err != nil {
		t.Fatalf("an admissible project was refused: %v", err)
	}

	err = g.CheckProject(project("https://github.com/acme/api.git", ""))
	var urlErr *RepoURLRefusedError
	if !errors.As(err, &urlErr) || !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("a project remote off the allowlist: err = %v, want *RepoURLRefusedError", err)
	}

	err = g.CheckProject(project("https://forge.example.test/acme/api.git", "http://forge.example.test/acme/handbook.git"))
	var contextErr *ContextRepoRefusedError
	if !errors.As(err, &contextErr) || contextErr.Name != "handbook" {
		t.Fatalf("an http context repository: err = %v, want *ContextRepoRefusedError naming it", err)
	}
}
