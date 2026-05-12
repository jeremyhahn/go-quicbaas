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

package inference

import (
	"context"
	"errors"
	"sync"
	"testing"
)

// stubFactory returns a factory that produces fakeBackend values. This is
// the only place fakeBackend is referenced from registry tests.
func stubFactory(provider string) Factory {
	return func(ctx context.Context, cfg Config) (APIInferenceBackend, error) {
		return &fakeBackend{provider: provider, baseURL: cfg.BaseURL}, nil
	}
}

func TestRegistry_RegisterAndCreate(t *testing.T) {
	t.Parallel()

	r := NewRegistry()

	if err := r.Register("anthropic", stubFactory("anthropic")); err != nil {
		t.Fatalf("Register returned unexpected error: %v", err)
	}

	be, err := r.Create(context.Background(), Config{Provider: "anthropic", BaseURL: "https://api.anthropic.com"})
	if err != nil {
		t.Fatalf("Create returned unexpected error: %v", err)
	}
	if be.Provider() != "anthropic" {
		t.Errorf("backend.Provider() = %q, want %q", be.Provider(), "anthropic")
	}
	if be.BaseURL() != "https://api.anthropic.com" {
		t.Errorf("backend.BaseURL() = %q, want %q", be.BaseURL(), "https://api.anthropic.com")
	}
}

func TestRegistry_Register_DuplicateReturnsTypedError(t *testing.T) {
	t.Parallel()

	r := NewRegistry()
	if err := r.Register("openai", stubFactory("openai")); err != nil {
		t.Fatalf("first Register failed: %v", err)
	}

	err := r.Register("openai", stubFactory("openai"))
	if err == nil {
		t.Fatal("expected error on duplicate register, got nil")
	}

	var dup *ProviderAlreadyRegisteredError
	if !errors.As(err, &dup) {
		t.Fatalf("expected *ProviderAlreadyRegisteredError, got %T: %v", err, err)
	}
	if dup.Provider != "openai" {
		t.Errorf("dup.Provider = %q, want %q", dup.Provider, "openai")
	}
	if !errors.Is(err, ErrProviderAlreadyRegistered) {
		t.Errorf("errors.Is should match ErrProviderAlreadyRegistered")
	}
}

func TestRegistry_Register_EmptyProvider(t *testing.T) {
	t.Parallel()

	r := NewRegistry()
	err := r.Register("", stubFactory("x"))
	if !errors.Is(err, ErrEmptyProvider) {
		t.Errorf("expected ErrEmptyProvider, got %v", err)
	}
}

func TestRegistry_Register_NilFactory(t *testing.T) {
	t.Parallel()

	r := NewRegistry()
	err := r.Register("anthropic", nil)
	if !errors.Is(err, ErrNilFactory) {
		t.Errorf("expected ErrNilFactory, got %v", err)
	}
}

func TestRegistry_Create_UnknownProviderReturnsTypedError(t *testing.T) {
	t.Parallel()

	r := NewRegistry()
	_, err := r.Create(context.Background(), Config{Provider: "does-not-exist"})
	if err == nil {
		t.Fatal("expected error for unknown provider, got nil")
	}

	var nf *ProviderNotRegisteredError
	if !errors.As(err, &nf) {
		t.Fatalf("expected *ProviderNotRegisteredError, got %T: %v", err, err)
	}
	if nf.Provider != "does-not-exist" {
		t.Errorf("nf.Provider = %q, want %q", nf.Provider, "does-not-exist")
	}
	if !errors.Is(err, ErrProviderNotRegistered) {
		t.Errorf("errors.Is should match ErrProviderNotRegistered")
	}
}

func TestRegistry_Create_EmptyProviderReturnsInvalidConfig(t *testing.T) {
	t.Parallel()

	r := NewRegistry()
	_, err := r.Create(context.Background(), Config{Provider: ""})
	if err == nil {
		t.Fatal("expected error for empty provider, got nil")
	}

	var ic *InvalidProviderConfigError
	if !errors.As(err, &ic) {
		t.Fatalf("expected *InvalidProviderConfigError, got %T: %v", err, err)
	}
	if ic.Field != "provider" {
		t.Errorf("ic.Field = %q, want %q", ic.Field, "provider")
	}
	if !errors.Is(err, ErrInvalidProviderConfig) {
		t.Errorf("errors.Is should match ErrInvalidProviderConfig")
	}
}

func TestRegistry_Create_FactoryError(t *testing.T) {
	t.Parallel()

	want := errors.New("factory exploded")
	r := NewRegistry()
	if err := r.Register("boom", func(ctx context.Context, cfg Config) (APIInferenceBackend, error) {
		return nil, want
	}); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	_, err := r.Create(context.Background(), Config{Provider: "boom"})
	if !errors.Is(err, want) {
		t.Errorf("expected factory error to propagate, got %v", err)
	}
}

func TestRegistry_Unregister(t *testing.T) {
	t.Parallel()

	r := NewRegistry()
	if err := r.Register("xai", stubFactory("xai")); err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	if !r.HasProvider("xai") {
		t.Fatal("expected HasProvider(xai) to be true after register")
	}
	if err := r.Unregister("xai"); err != nil {
		t.Fatalf("Unregister failed: %v", err)
	}
	if r.HasProvider("xai") {
		t.Fatal("expected HasProvider(xai) to be false after unregister")
	}

	err := r.Unregister("xai")
	var nf *ProviderNotRegisteredError
	if !errors.As(err, &nf) {
		t.Errorf("expected *ProviderNotRegisteredError on second Unregister, got %T", err)
	}
}

func TestRegistry_ProvidersIsSorted(t *testing.T) {
	t.Parallel()

	r := NewRegistry()
	for _, name := range []string{"openai", "anthropic", "xai", "mistral"} {
		if err := r.Register(name, stubFactory(name)); err != nil {
			t.Fatalf("Register(%q) failed: %v", name, err)
		}
	}

	got := r.Providers()
	want := []string{"anthropic", "mistral", "openai", "xai"}
	if len(got) != len(want) {
		t.Fatalf("Providers length = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Providers[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestRegistry_ConcurrentRegisterAndCreate(t *testing.T) {
	t.Parallel()

	r := NewRegistry()
	const workers = 16

	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		i := i
		go func() {
			defer wg.Done()
			name := provName(i)
			if err := r.Register(name, stubFactory(name)); err != nil {
				t.Errorf("worker %d Register failed: %v", i, err)
				return
			}
			if _, err := r.Create(context.Background(), Config{Provider: name}); err != nil {
				t.Errorf("worker %d Create failed: %v", i, err)
			}
		}()
	}
	wg.Wait()

	if got := len(r.Providers()); got != workers {
		t.Errorf("Providers count = %d, want %d", got, workers)
	}
}

func TestRegistry_MustRegister_PanicsOnDuplicate(t *testing.T) {
	t.Parallel()

	r := NewRegistry()
	r.MustRegister("dup", stubFactory("dup"))

	defer func() {
		if recover() == nil {
			t.Error("expected MustRegister to panic on duplicate")
		}
	}()
	r.MustRegister("dup", stubFactory("dup"))
}

func TestDefaultRegistry_PackageHelpers(t *testing.T) {
	// Use a unique name so we do not collide with adapters that may have
	// registered against Default in their init().
	const name = "test-default-registry-helper"

	if err := Register(name, stubFactory(name)); err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	t.Cleanup(func() {
		if err := Default.Unregister(name); err != nil {
			t.Errorf("cleanup Unregister failed: %v", err)
		}
	})

	be, err := Create(context.Background(), Config{Provider: name})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if be.Provider() != name {
		t.Errorf("Provider() = %q, want %q", be.Provider(), name)
	}

	found := false
	for _, p := range Providers() {
		if p == name {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Providers() did not contain %q", name)
	}
}

// provName returns a deterministic provider name for the i-th worker.
func provName(i int) string {
	return "worker-" + itoa(i)
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	negative := false
	if i < 0 {
		negative = true
		i = -i
	}
	var digits [20]byte
	pos := len(digits)
	for i > 0 {
		pos--
		digits[pos] = byte('0' + i%10)
		i /= 10
	}
	if negative {
		pos--
		digits[pos] = '-'
	}
	return string(digits[pos:])
}
