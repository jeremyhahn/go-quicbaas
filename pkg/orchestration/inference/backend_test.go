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
	"sync/atomic"
	"testing"
)

// fakeBackend is a complete in-memory implementation of APIInferenceBackend
// used to exercise the contract from tests. It does NOT ship as a real
// adapter; production providers live in their own files.
type fakeBackend struct {
	provider string
	baseURL  string
	models   []Model
	closed   atomic.Bool

	// generateFn lets individual tests inject behavior without growing the
	// struct. When nil, Generate returns a canned response.
	generateFn func(ctx context.Context, req GenerateRequest) (*GenerateResponse, error)

	// streamFn lets individual tests inject streaming behavior.
	streamFn func(ctx context.Context, req GenerateRequest) (<-chan GenerateChunk, <-chan error)
}

// Compile-time interface check.
var _ APIInferenceBackend = (*fakeBackend)(nil)

func (b *fakeBackend) Provider() string { return b.provider }

func (b *fakeBackend) BaseURL() string { return b.baseURL }

func (b *fakeBackend) ListModels(ctx context.Context) ([]Model, error) {
	if b.closed.Load() {
		return nil, errors.New("closed")
	}
	out := make([]Model, len(b.models))
	copy(out, b.models)
	return out, nil
}

func (b *fakeBackend) Generate(ctx context.Context, req GenerateRequest) (*GenerateResponse, error) {
	if b.closed.Load() {
		return nil, errors.New("closed")
	}
	if b.generateFn != nil {
		return b.generateFn(ctx, req)
	}
	return &GenerateResponse{
		Content:      "ok",
		Model:        req.Model,
		Usage:        Usage{PromptTokens: 1, CompletionTokens: 1, TotalTokens: 2},
		FinishReason: FinishReasonStop,
	}, nil
}

func (b *fakeBackend) Stream(ctx context.Context, req GenerateRequest) (<-chan GenerateChunk, <-chan error) {
	if b.streamFn != nil {
		return b.streamFn(ctx, req)
	}
	chunks := make(chan GenerateChunk, 2)
	errs := make(chan error, 1)
	go func() {
		defer close(chunks)
		defer close(errs)
		select {
		case <-ctx.Done():
			errs <- ctx.Err()
			return
		case chunks <- GenerateChunk{Delta: "hello "}:
		}
		select {
		case <-ctx.Done():
			errs <- ctx.Err()
			return
		case chunks <- GenerateChunk{Delta: "world", FinishReason: FinishReasonStop}:
		}
	}()
	return chunks, errs
}

func (b *fakeBackend) Close() error {
	if !b.closed.CompareAndSwap(false, true) {
		return nil
	}
	return nil
}

func TestAPIInferenceBackend_GenerateContract(t *testing.T) {
	t.Parallel()

	be := &fakeBackend{provider: "anthropic", baseURL: "https://api.anthropic.com"}

	resp, err := be.Generate(context.Background(), GenerateRequest{
		Model:    "claude-3-5-sonnet",
		Messages: []Message{{Role: RoleUser, Content: "ping"}},
	})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if resp.Content != "ok" {
		t.Errorf("Content = %q, want %q", resp.Content, "ok")
	}
	if resp.Model != "claude-3-5-sonnet" {
		t.Errorf("Model = %q, want %q", resp.Model, "claude-3-5-sonnet")
	}
	if resp.FinishReason != FinishReasonStop {
		t.Errorf("FinishReason = %q, want %q", resp.FinishReason, FinishReasonStop)
	}
	if resp.Usage.TotalTokens != 2 {
		t.Errorf("TotalTokens = %d, want 2", resp.Usage.TotalTokens)
	}
}

func TestAPIInferenceBackend_GenerateAfterCloseFails(t *testing.T) {
	t.Parallel()

	be := &fakeBackend{provider: "anthropic"}
	if err := be.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	if _, err := be.Generate(context.Background(), GenerateRequest{Model: "m"}); err == nil {
		t.Error("expected error after Close, got nil")
	}
	if _, err := be.ListModels(context.Background()); err == nil {
		t.Error("expected ListModels error after Close, got nil")
	}
}

func TestAPIInferenceBackend_CloseIsIdempotent(t *testing.T) {
	t.Parallel()

	be := &fakeBackend{provider: "anthropic"}
	if err := be.Close(); err != nil {
		t.Fatalf("first Close failed: %v", err)
	}
	if err := be.Close(); err != nil {
		t.Errorf("second Close should be a no-op, got %v", err)
	}
}

func TestAPIInferenceBackend_StreamDeliversChunksAndCloses(t *testing.T) {
	t.Parallel()

	be := &fakeBackend{provider: "anthropic"}
	chunks, errs := be.Stream(context.Background(), GenerateRequest{Model: "m"})

	var got string
	var finish FinishReason
	for c := range chunks {
		got += c.Delta
		if c.FinishReason != "" {
			finish = c.FinishReason
		}
	}
	if got != "hello world" {
		t.Errorf("aggregate stream = %q, want %q", got, "hello world")
	}
	if finish != FinishReasonStop {
		t.Errorf("FinishReason = %q, want %q", finish, FinishReasonStop)
	}

	// Error channel must close cleanly with no error.
	if err, ok := <-errs; ok {
		t.Errorf("expected clean close on err channel, got %v", err)
	}
}

func TestAPIInferenceBackend_StreamRespectsContextCancel(t *testing.T) {
	t.Parallel()

	be := &fakeBackend{provider: "anthropic"}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before Stream even runs

	chunks, errs := be.Stream(ctx, GenerateRequest{Model: "m"})

	// Drain chunks; we don't require any to arrive.
	for range chunks {
	}
	// The error channel must yield ctx.Err and then close.
	select {
	case err, ok := <-errs:
		if !ok {
			// Nothing was sent before close — also acceptable for a fast-cancel
			// path that simply closes both channels.
			return
		}
		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected context.Canceled, got %v", err)
		}
	default:
	}
}

func TestModel_CapabilitiesAreCanonical(t *testing.T) {
	t.Parallel()

	m := Model{
		ID:            "claude-3-5-sonnet",
		Name:          "Claude 3.5 Sonnet",
		ContextWindow: 200_000,
		Capabilities:  []Capability{CapabilityTools, CapabilityVision, CapabilityStreaming},
	}

	want := map[Capability]bool{
		CapabilityTools:     true,
		CapabilityVision:    true,
		CapabilityStreaming: true,
	}
	for _, c := range m.Capabilities {
		if !want[c] {
			t.Errorf("unexpected capability %q", c)
		}
		delete(want, c)
	}
	if len(want) != 0 {
		t.Errorf("missing capabilities: %v", want)
	}
}

func TestSecretRef_IsZero(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		ref  SecretRef
		want bool
	}{
		{"empty", SecretRef{}, true},
		{"engine only", SecretRef{Engine: "secrets"}, false},
		{"path only", SecretRef{Path: "tenants/x"}, false},
		{"key only", SecretRef{Key: "api_key"}, false},
		{"populated", SecretRef{Engine: "secrets", Path: "x", Key: "k"}, false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.ref.IsZero(); got != tc.want {
				t.Errorf("IsZero() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestErrors_AsAndIs(t *testing.T) {
	t.Parallel()

	t.Run("ProviderNotRegistered", func(t *testing.T) {
		t.Parallel()
		err := &ProviderNotRegisteredError{Provider: "xai"}
		if !errors.Is(err, ErrProviderNotRegistered) {
			t.Error("errors.Is should match ErrProviderNotRegistered")
		}
		var typed *ProviderNotRegisteredError
		if !errors.As(err, &typed) {
			t.Error("errors.As should extract *ProviderNotRegisteredError")
		}
		if typed.Provider != "xai" {
			t.Errorf("Provider = %q, want %q", typed.Provider, "xai")
		}
		if err.Error() == "" {
			t.Error("Error() must not be empty")
		}
	})

	t.Run("ProviderAlreadyRegistered", func(t *testing.T) {
		t.Parallel()
		err := &ProviderAlreadyRegisteredError{Provider: "openai"}
		if !errors.Is(err, ErrProviderAlreadyRegistered) {
			t.Error("errors.Is should match ErrProviderAlreadyRegistered")
		}
		if err.Error() == "" {
			t.Error("Error() must not be empty")
		}
	})

	t.Run("InvalidProviderConfig", func(t *testing.T) {
		t.Parallel()
		err := &InvalidProviderConfigError{
			Provider: "anthropic",
			Field:    "base_url",
			Reason:   "must be https",
		}
		if !errors.Is(err, ErrInvalidProviderConfig) {
			t.Error("errors.Is should match ErrInvalidProviderConfig")
		}
		msg := err.Error()
		if msg == "" {
			t.Error("Error() must not be empty")
		}
		// Sanity: each populated field shows up in the message.
		for _, sub := range []string{"anthropic", "base_url", "must be https"} {
			if !contains(msg, sub) {
				t.Errorf("Error() = %q, missing %q", msg, sub)
			}
		}
	})

	t.Run("InvalidProviderConfig_BareMessage", func(t *testing.T) {
		t.Parallel()
		// All optional fields empty: must not panic and must still mention
		// the invalid-config sentinel.
		err := &InvalidProviderConfigError{}
		if err.Error() == "" {
			t.Error("Error() must not be empty")
		}
		if !errors.Is(err, ErrInvalidProviderConfig) {
			t.Error("errors.Is should match ErrInvalidProviderConfig")
		}
	})
}

// contains reports whether substr is a substring of s without pulling in
// strings just for tests.
func contains(s, substr string) bool {
	if substr == "" {
		return true
	}
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
