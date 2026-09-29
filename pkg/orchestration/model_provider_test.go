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

package orchestration

import (
	"context"
	"errors"
	"slices"
	"testing"
)

// fakeModelLister answers ListModels with fixed models or a fixed error.
type fakeModelLister struct {
	models []string
	err    error
}

func (f fakeModelLister) ListModels(context.Context) ([]string, error) { return f.models, f.err }

func TestModelProviderOf(t *testing.T) {
	t.Parallel()
	cases := map[string]ModelProvider{
		"ollama/review-q8:latest":   ModelProviderOllama,
		" Ollama/review-q8 ":        ModelProviderOllama,
		"anthropic/claude-opus-5-5": "anthropic",
		"ollama/hf.co/org/model:q8": ModelProviderOllama,
		"review-q8:latest":          "",
		"":                          "",
	}
	for model, want := range cases {
		if got := ModelProviderOf(model); got != want {
			t.Errorf("ModelProviderOf(%q) = %q, want %q", model, got, want)
		}
	}
	if !ModelProviderOllama.Local() || ModelProvider("anthropic").Local() || ModelProvider("").Local() {
		t.Fatal("only ollama is a local provider")
	}
}

func TestModelProvider_Ref(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"review-q8:latest":        "ollama/review-q8:latest",
		"ollama/review-q8:latest": "ollama/review-q8:latest",
		"OLLAMA/review-q8:latest": "ollama/review-q8:latest",
		"hf.co/org/model:q8":      "ollama/hf.co/org/model:q8",
		" review-q8 ":             "ollama/review-q8",
	}
	for model, want := range cases {
		if got := ModelProviderOllama.Ref(model); got != want {
			t.Errorf("Ref(%q) = %q, want %q", model, got, want)
		}
	}
}

// TestACPBackend_ListModels_ExtendsTheCatalogFromItsProviders: an opencode node
// with an ollama reports that ollama's models as ollama/<name>, after the
// curated hosted ones, so ollama/review-q8:latest is a model the node runs.
func TestACPBackend_ListModels_ExtendsTheCatalogFromItsProviders(t *testing.T) {
	t.Parallel()
	backend := NewACPBackend(ACPBackendConfig{
		Command: "opencode",
		ModelProviders: []ACPModelProvider{{
			ID:     ModelProviderOllama,
			Models: fakeModelLister{models: []string{"review-q8:latest", " ", "qwen3:8b"}},
		}},
	})
	models, err := backend.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels() error = %v", err)
	}
	for _, want := range []string{"ollama/review-q8:latest", "ollama/qwen3:8b", "openai/gpt-5.5"} {
		if !slices.Contains(models, want) {
			t.Errorf("ListModels() = %v, missing %q", models, want)
		}
	}
	if slices.Contains(models, "ollama/") {
		t.Errorf("ListModels() = %v lists a blank model", models)
	}
	// The curated catalog is not mutated by the extension.
	if slices.Contains(acpModelCatalog["opencode"], "ollama/review-q8:latest") {
		t.Fatal("the extension leaked into the shared curated catalog")
	}
}

// TestACPBackend_ListModels_RefusesWhenAProviderCannotBeListed: a node that
// cannot read its own ollama says so rather than reporting a catalog that
// silently lacks the models it would otherwise run.
func TestACPBackend_ListModels_RefusesWhenAProviderCannotBeListed(t *testing.T) {
	t.Parallel()
	cause := errors.New("connection refused")
	backend := NewACPBackend(ACPBackendConfig{
		Command:        "opencode",
		ModelProviders: []ACPModelProvider{{ID: ModelProviderOllama, Models: fakeModelLister{err: cause}}},
	})
	_, err := backend.ListModels(context.Background())
	var unavailable *ModelProviderUnavailableError
	if !errors.As(err, &unavailable) || unavailable.Provider != ModelProviderOllama || !errors.Is(err, cause) {
		t.Fatalf("ListModels() error = %v, want *ModelProviderUnavailableError wrapping the cause", err)
	}
}

// TestACPBackend_ListModels_NoProvidersIsTheCuratedCatalog keeps every
// backend without providers exactly as it was.
func TestACPBackend_ListModels_NoProvidersIsTheCuratedCatalog(t *testing.T) {
	t.Parallel()
	models, err := NewACPBackend(ACPBackendConfig{Command: "codex"}).ListModels(context.Background())
	if err != nil || !slices.Equal(models, acpModelCatalog["codex"]) {
		t.Fatalf("ListModels() = %v, %v; want the curated codex catalog", models, err)
	}
	models, err = NewACPBackend(ACPBackendConfig{Command: "unknown-cli"}).ListModels(context.Background())
	if err != nil || models == nil || len(models) != 0 {
		t.Fatalf("ListModels() = %#v, %v; want an empty, non-nil slice", models, err)
	}
}
