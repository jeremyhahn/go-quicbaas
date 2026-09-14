// Scoped to `vector`, matching embedding_builder_test.go where the shared
// manager double lives. Without the tag pkg/embedding supplies stubs and there
// is no provider registry to route against.
//go:build vector

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

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/embedding"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// GenerateEmbeddingRequest.Provider and SetDefaultProvider were both documented
// but inert: the first was recorded for metrics and then ignored, and the second
// returned success without changing anything. Any provider that is not the
// manager's configured default was therefore unreachable through this service.
// These tests pin down that both now take effect.

// routedProvider is a real interfaces.EmbeddingProvider that records the calls
// it receives, so a test can prove which provider actually served a request.
type routedProvider struct {
	name       string
	dimensions int
	vector     []float32
	err        error

	embedCalls      []string
	embedBatchCalls [][]string
	closed          bool
}

func (p *routedProvider) Embed(_ context.Context, text string) ([]float32, error) {
	p.embedCalls = append(p.embedCalls, text)
	if p.err != nil {
		return nil, p.err
	}
	return p.vector, nil
}

func (p *routedProvider) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	p.embedBatchCalls = append(p.embedBatchCalls, texts)
	if p.err != nil {
		return nil, p.err
	}
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = p.vector
	}
	return out, nil
}

func (p *routedProvider) Dimensions() int { return p.dimensions }
func (p *routedProvider) MaxTokens() int  { return 512 }
func (p *routedProvider) Name() string    { return p.name }
func (p *routedProvider) Close() error    { p.closed = true; return nil }

// newRoutingService wires a service over a manager holding the given providers.
// The manager's own Embed records that it was used, so a test can tell the
// difference between "routed to a provider" and "fell through to the default".
func newRoutingService(
	t *testing.T,
	providers map[string]*routedProvider,
) (*embeddingService, *mockEmbeddingManager, *int) {
	t.Helper()

	managerCalls := 0
	mgr := newMockEmbeddingManager()
	for name, p := range providers {
		mgr.registeredProviders[name] = p
	}
	mgr.embedFunc = func(_ context.Context, _ string) ([]float32, error) {
		managerCalls++
		return []float32{0.1, 0.2, 0.3}, nil
	}
	mgr.embedBatchFunc = func(_ context.Context, texts []string) ([][]float32, error) {
		managerCalls++
		out := make([][]float32, len(texts))
		for i := range texts {
			out[i] = []float32{0.1, 0.2, 0.3}
		}
		return out, nil
	}

	svc, ok := NewEmbeddingService(mgr, nil).(*embeddingService)
	if !ok {
		t.Fatal("NewEmbeddingService did not return an *embeddingService")
	}
	return svc, mgr, &managerCalls
}

// -----------------------------------------------------------------------------
// Per-request routing
// -----------------------------------------------------------------------------

func TestGenerateEmbeddingRoutesToRequestedProvider(t *testing.T) {
	npu := &routedProvider{name: "npu", dimensions: 384, vector: make([]float32, 384)}
	svc, _, managerCalls := newRoutingService(t, map[string]*routedProvider{"npu": npu})

	resp, err := svc.GenerateEmbedding(context.Background(), &GenerateEmbeddingRequest{
		Text:     "route me to the npu",
		Provider: "npu",
	})
	if err != nil {
		t.Fatalf("GenerateEmbedding: unexpected error: %v", err)
	}

	if len(npu.embedCalls) != 1 || npu.embedCalls[0] != "route me to the npu" {
		t.Errorf("the named provider must serve the request; calls = %v", npu.embedCalls)
	}
	if *managerCalls != 0 {
		t.Errorf("the manager default must not be used when a provider is named; calls = %d", *managerCalls)
	}
	if len(resp.Embedding) != 384 {
		t.Errorf("got a %d-wide vector, want the routed provider's 384", len(resp.Embedding))
	}
	// Metadata must describe the provider that actually served the request.
	if resp.Metadata.Dimensions != 384 {
		t.Errorf("metadata reports %d dimensions, want the routed provider's 384", resp.Metadata.Dimensions)
	}
}

func TestGenerateEmbeddingFallsThroughToManagerDefault(t *testing.T) {
	npu := &routedProvider{name: "npu", dimensions: 384, vector: make([]float32, 384)}
	svc, _, managerCalls := newRoutingService(t, map[string]*routedProvider{"npu": npu})

	if _, err := svc.GenerateEmbedding(context.Background(), &GenerateEmbeddingRequest{
		Text: "no provider named",
	}); err != nil {
		t.Fatalf("GenerateEmbedding: unexpected error: %v", err)
	}

	if *managerCalls != 1 {
		t.Errorf("an unnamed provider must use the manager default; calls = %d", *managerCalls)
	}
	if len(npu.embedCalls) != 0 {
		t.Errorf("a registered non-default provider must not serve an unnamed request; calls = %v", npu.embedCalls)
	}
}

func TestBatchGenerateEmbeddingsRoutesToRequestedProvider(t *testing.T) {
	npu := &routedProvider{name: "npu", dimensions: 384, vector: make([]float32, 384)}
	svc, _, managerCalls := newRoutingService(t, map[string]*routedProvider{"npu": npu})

	resp, err := svc.BatchGenerateEmbeddings(context.Background(), &BatchGenerateEmbeddingsRequest{
		Provider: "npu",
		Items: []*BatchEmbeddingItem{
			{ID: "a", Text: "first"},
			{ID: "b", Text: "second"},
		},
	})
	if err != nil {
		t.Fatalf("BatchGenerateEmbeddings: unexpected error: %v", err)
	}

	if len(npu.embedBatchCalls) != 1 {
		t.Fatalf("the named provider must serve the batch; calls = %v", npu.embedBatchCalls)
	}
	if got := npu.embedBatchCalls[0]; len(got) != 2 || got[0] != "first" || got[1] != "second" {
		t.Errorf("the provider saw texts %v, want [first second] in order", got)
	}
	if *managerCalls != 0 {
		t.Errorf("the manager default must not be used; calls = %d", *managerCalls)
	}
	if resp.SuccessCount != 2 {
		t.Errorf("SuccessCount = %d, want 2", resp.SuccessCount)
	}
	for i, result := range resp.Results {
		if result.Metadata.Dimensions != 384 {
			t.Errorf("result %d reports %d dimensions, want 384", i, result.Metadata.Dimensions)
		}
	}
}

func TestBatchGenerateEmbeddingsFallsThroughToManagerDefault(t *testing.T) {
	npu := &routedProvider{name: "npu", dimensions: 384, vector: make([]float32, 384)}
	svc, _, managerCalls := newRoutingService(t, map[string]*routedProvider{"npu": npu})

	if _, err := svc.BatchGenerateEmbeddings(context.Background(), &BatchGenerateEmbeddingsRequest{
		Items: []*BatchEmbeddingItem{{ID: "a", Text: "first"}},
	}); err != nil {
		t.Fatalf("BatchGenerateEmbeddings: unexpected error: %v", err)
	}

	if *managerCalls != 1 {
		t.Errorf("an unnamed provider must use the manager default; calls = %d", *managerCalls)
	}
	if len(npu.embedBatchCalls) != 0 {
		t.Errorf("the non-default provider must not be called; calls = %v", npu.embedBatchCalls)
	}
}

// -----------------------------------------------------------------------------
// Error handling on the routed path
// -----------------------------------------------------------------------------

func TestGenerateEmbeddingUnknownProviderIsAnError(t *testing.T) {
	svc, _, managerCalls := newRoutingService(t, nil)

	_, err := svc.GenerateEmbedding(context.Background(), &GenerateEmbeddingRequest{
		Text:     "anything",
		Provider: "does-not-exist",
	})
	if err == nil {
		t.Fatal("naming an unregistered provider must be an error, not a silent fallback")
	}
	if *managerCalls != 0 {
		t.Errorf("an unknown provider must not quietly fall back to the default; calls = %d", *managerCalls)
	}
}

func TestGenerateEmbeddingSurfacesProviderError(t *testing.T) {
	failing := &routedProvider{name: "npu", dimensions: 384, err: embedding.ErrProviderUnavailable}
	svc, _, _ := newRoutingService(t, map[string]*routedProvider{"npu": failing})

	_, err := svc.GenerateEmbedding(context.Background(), &GenerateEmbeddingRequest{
		Text:     "anything",
		Provider: "npu",
	})
	if err == nil {
		t.Fatal("a routed provider's failure must reach the caller")
	}
}

func TestBatchGenerateEmbeddingsSurfacesProviderError(t *testing.T) {
	failing := &routedProvider{name: "npu", dimensions: 384, err: embedding.ErrProviderUnavailable}
	svc, _, _ := newRoutingService(t, map[string]*routedProvider{"npu": failing})

	_, err := svc.BatchGenerateEmbeddings(context.Background(), &BatchGenerateEmbeddingsRequest{
		Provider: "npu",
		Items:    []*BatchEmbeddingItem{{ID: "a", Text: "first"}},
	})
	if err == nil {
		t.Fatal("a routed provider's failure must reach the caller")
	}
}

// TestRoutingHandlesNilProviderFromManager covers a manager that reports success
// while handing back no provider, which would otherwise nil-panic.
func TestRoutingHandlesNilProviderFromManager(t *testing.T) {
	mgr := newMockEmbeddingManager()
	mgr.getProviderFunc = func(_ string) (interfaces.EmbeddingProvider, error) {
		return nil, nil
	}
	svc, ok := NewEmbeddingService(mgr, nil).(*embeddingService)
	if !ok {
		t.Fatal("unexpected service type")
	}

	_, err := svc.GenerateEmbedding(context.Background(), &GenerateEmbeddingRequest{
		Text:     "anything",
		Provider: "npu",
	})
	if err == nil {
		t.Fatal("a nil provider must be an error rather than a panic")
	}
}

// -----------------------------------------------------------------------------
// SetDefaultProvider actually takes effect
// -----------------------------------------------------------------------------

func TestSetDefaultProviderChangesWhichProviderServes(t *testing.T) {
	npu := &routedProvider{name: "npu", dimensions: 384, vector: make([]float32, 384)}
	svc, _, managerCalls := newRoutingService(t, map[string]*routedProvider{"npu": npu})
	ctx := context.Background()

	// Before: an unnamed request uses the manager's default.
	if _, err := svc.GenerateEmbedding(ctx, &GenerateEmbeddingRequest{Text: "before"}); err != nil {
		t.Fatalf("GenerateEmbedding: %v", err)
	}
	if *managerCalls != 1 {
		t.Fatalf("setup: manager calls = %d, want 1", *managerCalls)
	}

	if _, err := svc.SetDefaultProvider(ctx, &SetDefaultProviderRequest{Provider: "npu"}); err != nil {
		t.Fatalf("SetDefaultProvider: unexpected error: %v", err)
	}

	// After: the same unnamed request must reach the new default.
	if _, err := svc.GenerateEmbedding(ctx, &GenerateEmbeddingRequest{Text: "after"}); err != nil {
		t.Fatalf("GenerateEmbedding: %v", err)
	}

	if *managerCalls != 1 {
		t.Errorf("the manager default must not serve after the switch; calls = %d", *managerCalls)
	}
	if len(npu.embedCalls) != 1 || npu.embedCalls[0] != "after" {
		t.Errorf("the new default must serve the request; calls = %v", npu.embedCalls)
	}
}

func TestSetDefaultProviderAppliesToBatchToo(t *testing.T) {
	npu := &routedProvider{name: "npu", dimensions: 384, vector: make([]float32, 384)}
	svc, _, managerCalls := newRoutingService(t, map[string]*routedProvider{"npu": npu})
	ctx := context.Background()

	if _, err := svc.SetDefaultProvider(ctx, &SetDefaultProviderRequest{Provider: "npu"}); err != nil {
		t.Fatalf("SetDefaultProvider: %v", err)
	}

	if _, err := svc.BatchGenerateEmbeddings(ctx, &BatchGenerateEmbeddingsRequest{
		Items: []*BatchEmbeddingItem{{ID: "a", Text: "first"}},
	}); err != nil {
		t.Fatalf("BatchGenerateEmbeddings: %v", err)
	}

	if *managerCalls != 0 {
		t.Errorf("the manager default must not serve batches after the switch; calls = %d", *managerCalls)
	}
	if len(npu.embedBatchCalls) != 1 {
		t.Errorf("the new default must serve the batch; calls = %v", npu.embedBatchCalls)
	}
}

func TestSetDefaultProviderReportsThePreviousDefault(t *testing.T) {
	npu := &routedProvider{name: "npu", dimensions: 384, vector: make([]float32, 384)}
	ollama := &routedProvider{name: "ollama", dimensions: 768, vector: make([]float32, 768)}

	mgr := newMockEmbeddingManager()
	mgr.registeredProviders["npu"] = npu
	mgr.registeredProviders["ollama"] = ollama
	mgr.getStatsFunc = func() interfaces.EmbeddingStats {
		return interfaces.EmbeddingStats{DefaultProvider: "ollama", ProviderNames: []string{"ollama", "npu"}}
	}

	svc := NewEmbeddingService(mgr, nil)
	ctx := context.Background()

	// With no override yet in force, the manager's configured default was
	// serving; "default" is the established sentinel for that.
	first, err := svc.SetDefaultProvider(ctx, &SetDefaultProviderRequest{Provider: "npu"})
	if err != nil {
		t.Fatalf("SetDefaultProvider: %v", err)
	}
	if first.PreviousProvider != "default" {
		t.Errorf("PreviousProvider = %q, want %q", first.PreviousProvider, "default")
	}
	if first.Provider != "npu" {
		t.Errorf("Provider = %q, want %q", first.Provider, "npu")
	}

	// A second switch reports the override it replaced.
	second, err := svc.SetDefaultProvider(ctx, &SetDefaultProviderRequest{Provider: "ollama"})
	if err != nil {
		t.Fatalf("SetDefaultProvider: %v", err)
	}
	if second.PreviousProvider != "npu" {
		t.Errorf("PreviousProvider = %q, want %q", second.PreviousProvider, "npu")
	}
}

func TestSetDefaultProviderRejectsUnknownProvider(t *testing.T) {
	svc, _, _ := newRoutingService(t, nil)

	_, err := svc.SetDefaultProvider(context.Background(), &SetDefaultProviderRequest{
		Provider: "does-not-exist",
	})
	if err == nil {
		t.Fatal("switching to an unregistered provider must fail")
	}
}

func TestListProvidersReportsTheOverriddenDefault(t *testing.T) {
	npu := &routedProvider{name: "npu", dimensions: 384, vector: make([]float32, 384)}
	mgr := newMockEmbeddingManager()
	mgr.registeredProviders["npu"] = npu
	mgr.getStatsFunc = func() interfaces.EmbeddingStats {
		return interfaces.EmbeddingStats{DefaultProvider: "ollama", ProviderNames: []string{"npu"}}
	}

	svc := NewEmbeddingService(mgr, nil)
	ctx := context.Background()

	before, err := svc.ListProviders(ctx, &ListProvidersRequest{})
	if err != nil {
		t.Fatalf("ListProviders: %v", err)
	}
	if before.DefaultProvider != "ollama" {
		t.Errorf("DefaultProvider = %q, want %q before any switch", before.DefaultProvider, "ollama")
	}

	if _, err := svc.SetDefaultProvider(ctx, &SetDefaultProviderRequest{Provider: "npu"}); err != nil {
		t.Fatalf("SetDefaultProvider: %v", err)
	}

	after, err := svc.ListProviders(ctx, &ListProvidersRequest{})
	if err != nil {
		t.Fatalf("ListProviders: %v", err)
	}
	if after.DefaultProvider != "npu" {
		t.Errorf("DefaultProvider = %q, want the override %q", after.DefaultProvider, "npu")
	}
}

// TestRoutingIsConcurrencySafe runs the routed path against the race detector,
// since the default override is read on every embed call.
func TestRoutingIsConcurrencySafe(t *testing.T) {
	npu := &routedProvider{name: "npu", dimensions: 384, vector: make([]float32, 384)}
	mgr := newMockEmbeddingManager()
	// A provider that is safe to call concurrently: the recording provider above
	// appends to a slice, so use a stateless one here.
	mgr.getProviderFunc = func(name string) (interfaces.EmbeddingProvider, error) {
		if name == "npu" {
			return &statelessProvider{dimensions: 384}, nil
		}
		return nil, embedding.ErrProviderNotFound
	}
	_ = npu

	svc := NewEmbeddingService(mgr, nil)
	ctx := context.Background()

	const workers = 16
	errs := make(chan error, workers*2)
	done := make(chan struct{})

	for i := range workers {
		go func(n int) {
			defer func() { done <- struct{}{} }()
			if n%2 == 0 {
				if _, err := svc.SetDefaultProvider(ctx, &SetDefaultProviderRequest{Provider: "npu"}); err != nil {
					errs <- err
				}
				return
			}
			if _, err := svc.GenerateEmbedding(ctx, &GenerateEmbeddingRequest{
				Text:     "concurrent",
				Provider: "npu",
			}); err != nil {
				errs <- err
			}
		}(i)
	}
	for range workers {
		<-done
	}
	close(errs)

	for err := range errs {
		if !errors.Is(err, embedding.ErrProviderNotFound) {
			t.Errorf("concurrent routing: %v", err)
		}
	}
}

// statelessProvider is safe for concurrent use.
type statelessProvider struct{ dimensions int }

func (p *statelessProvider) Embed(_ context.Context, _ string) ([]float32, error) {
	return make([]float32, p.dimensions), nil
}

func (p *statelessProvider) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = make([]float32, p.dimensions)
	}
	return out, nil
}

func (p *statelessProvider) Dimensions() int { return p.dimensions }
func (p *statelessProvider) MaxTokens() int  { return 512 }
func (p *statelessProvider) Name() string    { return "npu" }
func (p *statelessProvider) Close() error    { return nil }

// -----------------------------------------------------------------------------
// Disabled providers are excluded from routing
// -----------------------------------------------------------------------------

// SetProviderStatus recorded the disabled set but nothing ever read it, so
// disabling a provider did nothing. The service contract says disabled providers
// are "excluded from request routing and fallback chains" -- these tests hold it
// to that, because disabling a provider whose backend is down is the first thing
// an operator reaches for during an outage.

func TestDisabledProviderIsRefusedForRoutedRequest(t *testing.T) {
	npu := &routedProvider{name: "npu", dimensions: 384, vector: make([]float32, 384)}
	svc, _, managerCalls := newRoutingService(t, map[string]*routedProvider{"npu": npu})
	ctx := context.Background()

	if _, err := svc.SetProviderStatus(ctx, &SetProviderStatusRequest{
		Name:    "npu",
		Enabled: false,
	}); err != nil {
		t.Fatalf("SetProviderStatus: %v", err)
	}

	_, err := svc.GenerateEmbedding(ctx, &GenerateEmbeddingRequest{
		Text:     "should be refused",
		Provider: "npu",
	})
	if err == nil {
		t.Fatal("a disabled provider must not serve a request")
	}

	var disabled *ProviderDisabledError
	if !errors.As(err, &disabled) {
		t.Fatalf("error %v is not a *ProviderDisabledError", err)
	}
	if disabled.Provider != "npu" {
		t.Errorf("disabled.Provider = %q, want %q", disabled.Provider, "npu")
	}
	// It must map onto the general embedding error contract.
	if !errors.Is(err, embedding.ErrProviderUnavailable) {
		t.Errorf("error %v must match embedding.ErrProviderUnavailable", err)
	}

	if len(npu.embedCalls) != 0 {
		t.Errorf("the disabled provider must not be called; calls = %v", npu.embedCalls)
	}
	if *managerCalls != 0 {
		t.Errorf("a disabled provider must not silently fall back to the default; calls = %d", *managerCalls)
	}
}

func TestDisabledProviderIsRefusedForBatch(t *testing.T) {
	npu := &routedProvider{name: "npu", dimensions: 384, vector: make([]float32, 384)}
	svc, _, _ := newRoutingService(t, map[string]*routedProvider{"npu": npu})
	ctx := context.Background()

	if _, err := svc.SetProviderStatus(ctx, &SetProviderStatusRequest{Name: "npu", Enabled: false}); err != nil {
		t.Fatalf("SetProviderStatus: %v", err)
	}

	_, err := svc.BatchGenerateEmbeddings(ctx, &BatchGenerateEmbeddingsRequest{
		Provider: "npu",
		Items:    []*BatchEmbeddingItem{{ID: "a", Text: "first"}},
	})
	if !errors.Is(err, embedding.ErrProviderUnavailable) {
		t.Fatalf("a disabled provider must be refused for batches; got %v", err)
	}
	if len(npu.embedBatchCalls) != 0 {
		t.Errorf("the disabled provider must not be called; calls = %v", npu.embedBatchCalls)
	}
}

func TestReEnablingAProviderRestoresRouting(t *testing.T) {
	npu := &routedProvider{name: "npu", dimensions: 384, vector: make([]float32, 384)}
	svc, _, _ := newRoutingService(t, map[string]*routedProvider{"npu": npu})
	ctx := context.Background()

	if _, err := svc.SetProviderStatus(ctx, &SetProviderStatusRequest{Name: "npu", Enabled: false}); err != nil {
		t.Fatalf("SetProviderStatus(false): %v", err)
	}
	if _, err := svc.SetProviderStatus(ctx, &SetProviderStatusRequest{Name: "npu", Enabled: true}); err != nil {
		t.Fatalf("SetProviderStatus(true): %v", err)
	}

	if _, err := svc.GenerateEmbedding(ctx, &GenerateEmbeddingRequest{
		Text:     "back in service",
		Provider: "npu",
	}); err != nil {
		t.Fatalf("a re-enabled provider must serve again: %v", err)
	}
	if len(npu.embedCalls) != 1 {
		t.Errorf("the re-enabled provider must be called; calls = %v", npu.embedCalls)
	}
}

func TestDisablingOneProviderLeavesTheDefaultPathAlone(t *testing.T) {
	npu := &routedProvider{name: "npu", dimensions: 384, vector: make([]float32, 384)}
	svc, _, managerCalls := newRoutingService(t, map[string]*routedProvider{"npu": npu})
	ctx := context.Background()

	if _, err := svc.SetProviderStatus(ctx, &SetProviderStatusRequest{Name: "npu", Enabled: false}); err != nil {
		t.Fatalf("SetProviderStatus: %v", err)
	}

	if _, err := svc.GenerateEmbedding(ctx, &GenerateEmbeddingRequest{Text: "unnamed"}); err != nil {
		t.Fatalf("disabling one provider must not break the manager default: %v", err)
	}
	if *managerCalls != 1 {
		t.Errorf("the manager default must still serve; calls = %d", *managerCalls)
	}
}

func TestSetDefaultProviderRefusesADisabledProvider(t *testing.T) {
	npu := &routedProvider{name: "npu", dimensions: 384, vector: make([]float32, 384)}
	svc, _, _ := newRoutingService(t, map[string]*routedProvider{"npu": npu})
	ctx := context.Background()

	if _, err := svc.SetProviderStatus(ctx, &SetProviderStatusRequest{Name: "npu", Enabled: false}); err != nil {
		t.Fatalf("SetProviderStatus: %v", err)
	}

	_, err := svc.SetDefaultProvider(ctx, &SetDefaultProviderRequest{Provider: "npu"})
	if err == nil {
		t.Fatal("making a disabled provider the default must be refused")
	}
	var disabled *ProviderDisabledError
	if !errors.As(err, &disabled) {
		t.Fatalf("error %v is not a *ProviderDisabledError", err)
	}

	// The override must not have been taken.
	if got := svc.effectiveDefaultProvider(); got != "" {
		t.Errorf("effectiveDefaultProvider = %q, want unset after a refused switch", got)
	}
}

func TestProviderDisabledErrorMessage(t *testing.T) {
	err := &ProviderDisabledError{Provider: "npu"}
	if got := err.Error(); got != "embeddings: provider is disabled: npu" {
		t.Errorf("Error() = %q", got)
	}
	if !errors.Is(err, embedding.ErrProviderUnavailable) {
		t.Error("must match embedding.ErrProviderUnavailable")
	}
}

// TestDisablingTheManagerDefaultStopsUnnamedTraffic covers the gap that an
// unnamed request routes through the manager's cached default path, which names
// no provider -- so the disabled check was never reached for the one provider an
// operator is most likely to disable: the one currently serving.
func TestDisablingTheManagerDefaultStopsUnnamedTraffic(t *testing.T) {
	mgr := newMockEmbeddingManager()
	managerCalls := 0
	mgr.embedFunc = func(_ context.Context, _ string) ([]float32, error) {
		managerCalls++
		return []float32{0.1, 0.2, 0.3}, nil
	}
	mgr.getStatsFunc = func() interfaces.EmbeddingStats {
		return interfaces.EmbeddingStats{DefaultProvider: "ollama"}
	}
	mgr.registeredProviders["ollama"] = &routedProvider{name: "ollama", dimensions: 3}

	svc := NewEmbeddingService(mgr, nil)
	ctx := context.Background()

	// Before: unnamed traffic flows.
	if _, err := svc.GenerateEmbedding(ctx, &GenerateEmbeddingRequest{Text: "before"}); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if managerCalls != 1 {
		t.Fatalf("setup: manager calls = %d, want 1", managerCalls)
	}

	// The operator takes the serving backend out of service during an outage.
	if _, err := svc.SetProviderStatus(ctx, &SetProviderStatusRequest{
		Name:    "ollama",
		Enabled: false,
	}); err != nil {
		t.Fatalf("SetProviderStatus: %v", err)
	}

	_, err := svc.GenerateEmbedding(ctx, &GenerateEmbeddingRequest{Text: "after"})
	if err == nil {
		t.Fatal("unnamed traffic must stop once the manager default is disabled")
	}
	var disabled *ProviderDisabledError
	if !errors.As(err, &disabled) {
		t.Fatalf("error %v is not a *ProviderDisabledError", err)
	}
	if disabled.Provider != "ollama" {
		t.Errorf("disabled.Provider = %q, want %q", disabled.Provider, "ollama")
	}
	if managerCalls != 1 {
		t.Errorf("the disabled default must not be called again; calls = %d", managerCalls)
	}
}

func TestDisablingTheManagerDefaultStopsUnnamedBatches(t *testing.T) {
	mgr := newMockEmbeddingManager()
	managerCalls := 0
	mgr.embedBatchFunc = func(_ context.Context, texts []string) ([][]float32, error) {
		managerCalls++
		out := make([][]float32, len(texts))
		for i := range texts {
			out[i] = []float32{0.1, 0.2, 0.3}
		}
		return out, nil
	}
	mgr.getStatsFunc = func() interfaces.EmbeddingStats {
		return interfaces.EmbeddingStats{DefaultProvider: "ollama"}
	}
	mgr.registeredProviders["ollama"] = &routedProvider{name: "ollama", dimensions: 3}

	svc := NewEmbeddingService(mgr, nil)
	ctx := context.Background()

	if _, err := svc.SetProviderStatus(ctx, &SetProviderStatusRequest{Name: "ollama", Enabled: false}); err != nil {
		t.Fatalf("SetProviderStatus: %v", err)
	}

	_, err := svc.BatchGenerateEmbeddings(ctx, &BatchGenerateEmbeddingsRequest{
		Items: []*BatchEmbeddingItem{{ID: "a", Text: "first"}},
	})
	if !errors.Is(err, embedding.ErrProviderUnavailable) {
		t.Fatalf("unnamed batches must stop too; got %v", err)
	}
	if managerCalls != 0 {
		t.Errorf("the disabled default must not be called; calls = %d", managerCalls)
	}
}

func TestReEnablingTheManagerDefaultRestoresUnnamedTraffic(t *testing.T) {
	mgr := newMockEmbeddingManager()
	mgr.getStatsFunc = func() interfaces.EmbeddingStats {
		return interfaces.EmbeddingStats{DefaultProvider: "ollama"}
	}
	mgr.registeredProviders["ollama"] = &routedProvider{name: "ollama", dimensions: 3}

	svc := NewEmbeddingService(mgr, nil)
	ctx := context.Background()

	if _, err := svc.SetProviderStatus(ctx, &SetProviderStatusRequest{Name: "ollama", Enabled: false}); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if _, err := svc.SetProviderStatus(ctx, &SetProviderStatusRequest{Name: "ollama", Enabled: true}); err != nil {
		t.Fatalf("enable: %v", err)
	}

	if _, err := svc.GenerateEmbedding(ctx, &GenerateEmbeddingRequest{Text: "back"}); err != nil {
		t.Errorf("re-enabling the default must restore unnamed traffic: %v", err)
	}
}

// TestUnnamedTrafficIsUnaffectedByOtherDisabledProviders keeps the guard narrow:
// disabling some other provider must not stop the default from serving.
func TestUnnamedTrafficIsUnaffectedByOtherDisabledProviders(t *testing.T) {
	mgr := newMockEmbeddingManager()
	managerCalls := 0
	mgr.embedFunc = func(_ context.Context, _ string) ([]float32, error) {
		managerCalls++
		return []float32{0.1}, nil
	}
	mgr.getStatsFunc = func() interfaces.EmbeddingStats {
		return interfaces.EmbeddingStats{DefaultProvider: "ollama"}
	}
	mgr.registeredProviders["npu"] = &routedProvider{name: "npu", dimensions: 384}

	svc := NewEmbeddingService(mgr, nil)
	ctx := context.Background()

	if _, err := svc.SetProviderStatus(ctx, &SetProviderStatusRequest{Name: "npu", Enabled: false}); err != nil {
		t.Fatalf("SetProviderStatus: %v", err)
	}
	if _, err := svc.GenerateEmbedding(ctx, &GenerateEmbeddingRequest{Text: "unnamed"}); err != nil {
		t.Fatalf("disabling npu must not stop the ollama default: %v", err)
	}
	if managerCalls != 1 {
		t.Errorf("manager calls = %d, want 1", managerCalls)
	}
}

// TestEmptyDisabledSetSkipsTheManagerLookup pins the short-circuit: with nothing
// disabled, the embed path must not pay a GetStats call per request.
func TestEmptyDisabledSetSkipsTheManagerLookup(t *testing.T) {
	mgr := newMockEmbeddingManager()
	statsCalls := 0
	mgr.getStatsFunc = func() interfaces.EmbeddingStats {
		statsCalls++
		return interfaces.EmbeddingStats{DefaultProvider: "ollama"}
	}

	svc, ok := NewEmbeddingService(mgr, nil).(*embeddingService)
	if !ok {
		t.Fatal("unexpected service type")
	}

	// GenerateEmbedding already reads stats twice for cache accounting; the
	// disabled check must add nothing on top of that.
	before := statsCalls
	if _, _, err := svc.embedOne(context.Background(), "", "text"); err != nil {
		t.Fatalf("embedOne: %v", err)
	}
	if statsCalls != before {
		t.Errorf("the disabled check made %d manager calls with nothing disabled, want 0",
			statsCalls-before)
	}
}
