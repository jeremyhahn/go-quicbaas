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

package secrets

import (
	"context"
	stderrors "errors"
	"testing"

	qrdbsecret "github.com/jeremyhahn/go-qrdb/sdk/go/secret"
)

// mockResolver is a test resolver that resolves values with a "mock://" prefix.
type mockResolver struct {
	resolveFunc func(ctx context.Context, raw string) (*SecretValue, error)
}

func (m *mockResolver) Scheme() string {
	return "mock"
}

func (m *mockResolver) CanResolve(raw string) bool {
	return len(raw) > 7 && raw[:7] == "mock://"
}

func (m *mockResolver) Resolve(ctx context.Context, raw string) (*SecretValue, error) {
	if m.resolveFunc != nil {
		return m.resolveFunc(ctx, raw)
	}
	// Default: return the value after the prefix as the resolved value.
	return NewSecretValue(raw, []byte(raw[7:]), StrategyLocalKMS)
}

// mockOtherResolver is a second test resolver bound to a different scheme,
// used to verify that the registry dispatches strictly by URI scheme.
type mockOtherResolver struct{}

func (m *mockOtherResolver) Scheme() string {
	return "other"
}

func (m *mockOtherResolver) CanResolve(raw string) bool {
	return len(raw) > 8 && raw[:8] == "other://"
}

func (m *mockOtherResolver) Resolve(ctx context.Context, raw string) (*SecretValue, error) {
	return NewSecretValue(raw, []byte("other-"+raw[8:]), StrategyRemoteKMS)
}

// failingResolver always fails to resolve.
type failingResolver struct{}

func (f *failingResolver) Scheme() string {
	return "fail"
}

func (f *failingResolver) CanResolve(raw string) bool {
	return len(raw) > 7 && raw[:7] == "fail://"
}

func (f *failingResolver) Resolve(ctx context.Context, raw string) (*SecretValue, error) {
	return nil, &ResolutionFailedError{URI: raw, Reason: "intentional failure"}
}

func TestConfigProcessor_Process_Pointers(t *testing.T) {
	type Credentials struct {
		Username string
		Password string
	}

	type Config struct {
		Name  string
		Creds *Credentials
	}

	registry := NewResolverRegistry()
	registry.Register(&mockResolver{})

	processor := NewConfigProcessor(registry)

	cfg := &Config{
		Name: "test",
		Creds: &Credentials{
			Username: "admin",
			Password: "mock://ptr-secret",
		},
	}

	ctx := context.Background()
	result, err := processor.Process(ctx, cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Resolved != 1 {
		t.Fatalf("expected 1 resolved secret, got %d", result.Resolved)
	}

	if cfg.Creds.Password != "ptr-secret" {
		t.Errorf("expected password 'ptr-secret', got %q", cfg.Creds.Password)
	}
}

func TestConfigProcessor_Process_Slices(t *testing.T) {
	type Backend struct {
		Name   string
		Secret string
	}

	type Config struct {
		Strings  []string
		Backends []Backend
		Items    []*Backend
	}

	registry := NewResolverRegistry()
	registry.Register(&mockResolver{})

	processor := NewConfigProcessor(registry)

	cfg := &Config{
		Strings: []string{"mock://s1", "literal", "mock://s3"},
		Backends: []Backend{
			{Name: "tpm", Secret: "mock://tpm-secret"},
			{Name: "plain", Secret: "not-a-secret"},
		},
		Items: []*Backend{
			{Secret: "mock://item1"},
			nil, // nil pointer should be skipped
			{Secret: "mock://item3"},
		},
	}

	result, err := processor.Process(context.Background(), cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Resolved != 5 {
		t.Fatalf("expected 5 resolved secrets, got %d", result.Resolved)
	}

	checks := []struct {
		name, got, want string
	}{
		{"Strings[0]", cfg.Strings[0], "s1"},
		{"Strings[1]", cfg.Strings[1], "literal"},
		{"Strings[2]", cfg.Strings[2], "s3"},
		{"Backends[0]", cfg.Backends[0].Secret, "tpm-secret"},
		{"Backends[1]", cfg.Backends[1].Secret, "not-a-secret"},
		{"Items[0]", cfg.Items[0].Secret, "item1"},
		{"Items[2]", cfg.Items[2].Secret, "item3"},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s: expected %q, got %q", c.name, c.want, c.got)
		}
	}
}

func TestConfigProcessor_Process_Maps(t *testing.T) {
	type Config struct {
		Secrets map[string]string
	}

	registry := NewResolverRegistry()
	registry.Register(&mockResolver{})

	processor := NewConfigProcessor(registry)

	cfg := &Config{
		Secrets: map[string]string{
			"api_key":  "mock://api-key-value",
			"password": "mock://password-value",
			"literal":  "plain-text",
		},
	}

	ctx := context.Background()
	result, err := processor.Process(ctx, cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Resolved != 2 {
		t.Fatalf("expected 2 resolved secrets, got %d", result.Resolved)
	}

	// Verify map values are updated
	if cfg.Secrets["api_key"] != "api-key-value" {
		t.Errorf("expected 'api-key-value', got %q", cfg.Secrets["api_key"])
	}
	if cfg.Secrets["password"] != "password-value" {
		t.Errorf("expected 'password-value', got %q", cfg.Secrets["password"])
	}
	if cfg.Secrets["literal"] != "plain-text" {
		t.Errorf("expected 'plain-text' unchanged, got %q", cfg.Secrets["literal"])
	}
}

func TestConfigProcessor_Process_MixedTypes(t *testing.T) {
	type Nested struct {
		Secret string
	}

	type Config struct {
		Name     string
		Port     int
		Enabled  bool
		Password string
		Nested   Nested
		Secrets  []string
		Tags     map[string]string
	}

	registry := NewResolverRegistry()
	registry.Register(&mockResolver{})

	processor := NewConfigProcessor(registry)

	cfg := &Config{
		Name:     "app",
		Port:     8080,
		Enabled:  true,
		Password: "mock://root-password",
		Nested: Nested{
			Secret: "mock://nested-secret",
		},
		Secrets: []string{"mock://slice-secret-1", "mock://slice-secret-2"},
		Tags: map[string]string{
			"api": "mock://tag-secret",
		},
	}

	ctx := context.Background()
	result, err := processor.Process(ctx, cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should resolve: Password, Nested.Secret, Secrets[0], Secrets[1], Tags[api]
	if result.Resolved != 5 {
		t.Fatalf("expected 5 resolved secrets, got %d", result.Resolved)
	}

	// Verify original non-secret values unchanged
	if cfg.Name != "app" {
		t.Errorf("name should be unchanged")
	}
	if cfg.Port != 8080 {
		t.Errorf("port should be unchanged")
	}
	if !cfg.Enabled {
		t.Errorf("enabled should be unchanged")
	}

	// Verify secrets resolved
	if cfg.Password != "root-password" {
		t.Errorf("expected 'root-password', got %q", cfg.Password)
	}
	if cfg.Nested.Secret != "nested-secret" {
		t.Errorf("expected 'nested-secret', got %q", cfg.Nested.Secret)
	}
}

func TestConfigProcessor_Process_TracksResolvedCount(t *testing.T) {
	type Config struct {
		Secret1 string
		Secret2 string
		Secret3 string
	}

	registry := NewResolverRegistry()
	registry.Register(&mockResolver{})

	processor := NewConfigProcessor(registry)

	cfg := &Config{
		Secret1: "mock://value1",
		Secret2: "mock://value2",
		Secret3: "mock://value3",
	}

	ctx := context.Background()
	result, err := processor.Process(ctx, cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Resolved != 3 {
		t.Fatalf("expected 3 resolved secrets, got %d", result.Resolved)
	}

	// Verify each secret was resolved correctly
	if cfg.Secret1 != "value1" {
		t.Errorf("Secret1: expected 'value1', got %q", cfg.Secret1)
	}
	if cfg.Secret2 != "value2" {
		t.Errorf("Secret2: expected 'value2', got %q", cfg.Secret2)
	}
	if cfg.Secret3 != "value3" {
		t.Errorf("Secret3: expected 'value3', got %q", cfg.Secret3)
	}
}

func TestConfigProcessor_Process_HandlesErrorsGracefully(t *testing.T) {
	type Config struct {
		Good1 string
		Bad   string
		Good2 string
	}

	registry := NewResolverRegistry()
	registry.Register(&mockResolver{})
	registry.Register(&failingResolver{})

	processor := NewConfigProcessor(registry)

	cfg := &Config{
		Good1: "mock://good-value1",
		Bad:   "fail://will-fail",
		Good2: "mock://good-value2",
	}

	ctx := context.Background()
	result, err := processor.Process(ctx, cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should resolve 2 secrets successfully
	if result.Resolved != 2 {
		t.Fatalf("expected 2 resolved secrets, got %d", result.Resolved)
	}

	// Should have 1 error
	if len(result.Errors) != 1 {
		t.Fatalf("expected 1 error, got %d", len(result.Errors))
	}

	// Should have 1 unresolved
	if len(result.Unresolved) != 1 {
		t.Fatalf("expected 1 unresolved, got %d", len(result.Unresolved))
	}

	if result.Unresolved[0] != "Bad" {
		t.Errorf("expected unresolved field 'Bad', got %q", result.Unresolved[0])
	}

	// Good values should be resolved
	if cfg.Good1 != "good-value1" {
		t.Errorf("expected 'good-value1', got %q", cfg.Good1)
	}
	if cfg.Good2 != "good-value2" {
		t.Errorf("expected 'good-value2', got %q", cfg.Good2)
	}

	// Bad value should be unchanged
	if cfg.Bad != "fail://will-fail" {
		t.Errorf("expected 'fail://will-fail', got %q", cfg.Bad)
	}
}

func TestConfigProcessor_Process_InvalidInput(t *testing.T) {
	type Config struct {
		Name string
	}

	var nilCfg *Config
	value := "just a string"

	tests := []struct {
		name      string
		ctx       context.Context
		cfg       interface{}
		wantInput bool // ProcessorInputError expected; otherwise ErrNilContext
	}{
		{name: "nil config", ctx: context.Background(), cfg: nil, wantInput: true},
		{name: "non-pointer", ctx: context.Background(), cfg: Config{Name: "test"}, wantInput: true},
		{name: "pointer to non-struct", ctx: context.Background(), cfg: &value, wantInput: true},
		{name: "nil pointer", ctx: context.Background(), cfg: nilCfg, wantInput: true},
		{name: "nil context", ctx: nil, cfg: &Config{Name: "test"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			processor := NewConfigProcessor(NewResolverRegistry())

			result, err := processor.Process(tt.ctx, tt.cfg)

			if err == nil {
				t.Fatal("expected error")
			}
			if tt.wantInput {
				var pie *ProcessorInputError
				if !stderrors.As(err, &pie) {
					t.Fatalf("expected ProcessorInputError, got %T", err)
				}
			} else if !stderrors.Is(err, qrdbsecret.ErrNilContext) {
				t.Fatalf("expected ErrNilContext, got %T: %v", err, err)
			}
			if result != nil {
				t.Errorf("expected nil result, got %+v", result)
			}
		})
	}
}

func TestConfigProcessor_Process_ContextCancellation(t *testing.T) {
	type Config struct {
		Secret1 string
		Secret2 string
	}

	tests := []struct {
		name string
		// cancelInResolver cancels the context while the resolver is
		// running; otherwise the context is cancelled before Process.
		cancelInResolver bool
	}{
		{name: "cancelled before process"},
		{name: "cancelled during resolve", cancelInResolver: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if !tt.cancelInResolver {
				cancel()
			}

			blockingResolver := &mockResolver{
				resolveFunc: func(ctx context.Context, raw string) (*SecretValue, error) {
					cancel()
					<-ctx.Done()
					return nil, ctx.Err()
				},
			}
			registry := NewResolverRegistry()
			registry.Register(blockingResolver)
			processor := NewConfigProcessor(registry)

			cfg := &Config{Secret1: "mock://s1", Secret2: "mock://s2"}

			result, err := processor.Process(ctx, cfg)

			if err != nil {
				t.Fatalf("unexpected top-level error: %v", err)
			}
			if len(result.Errors) == 0 {
				t.Fatal("expected errors from cancellation")
			}
			if len(result.Unresolved) == 0 {
				t.Fatal("expected unresolved field from cancellation")
			}
			if cfg.Secret1 != "mock://s1" {
				t.Errorf("cancelled secret must keep its reference, got %q", cfg.Secret1)
			}
		})
	}
}

func TestConfigProcessor_Process_UnexportedFields(t *testing.T) {
	type Config struct {
		Public  string
		private string
	}

	registry := NewResolverRegistry()
	registry.Register(&mockResolver{})

	processor := NewConfigProcessor(registry)

	cfg := &Config{
		Public:  "mock://public-secret",
		private: "mock://private-secret",
	}

	ctx := context.Background()
	result, err := processor.Process(ctx, cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only public field should be resolved.
	if result.Resolved != 1 {
		t.Fatalf("expected 1 resolved secret, got %d", result.Resolved)
	}

	if cfg.Public != "public-secret" {
		t.Errorf("expected 'public-secret', got %q", cfg.Public)
	}

	// Private field should be unchanged.
	if cfg.private != "mock://private-secret" {
		t.Errorf("private field should be unchanged, got %q", cfg.private)
	}
}

func TestConfigProcessor_Process_DeeplyNestedStructs(t *testing.T) {
	type Level4 struct {
		Secret string
	}

	type Level3 struct {
		L4 Level4
	}

	type Level2 struct {
		L3 Level3
	}

	type Level1 struct {
		L2 Level2
	}

	type Config struct {
		L1 Level1
	}

	registry := NewResolverRegistry()
	registry.Register(&mockResolver{})

	processor := NewConfigProcessor(registry)

	cfg := &Config{
		L1: Level1{
			L2: Level2{
				L3: Level3{
					L4: Level4{
						Secret: "mock://deep-secret",
					},
				},
			},
		},
	}

	ctx := context.Background()
	result, err := processor.Process(ctx, cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Resolved != 1 {
		t.Fatalf("expected 1 resolved secret, got %d", result.Resolved)
	}

	if cfg.L1.L2.L3.L4.Secret != "deep-secret" {
		t.Errorf("expected 'deep-secret', got %q", cfg.L1.L2.L3.L4.Secret)
	}
}

func TestConfigProcessor_Process_MultipleResolvers(t *testing.T) {
	type Config struct {
		MockSecret  string
		OtherSecret string
	}

	registry := NewResolverRegistry()
	registry.Register(&mockResolver{})
	registry.Register(&mockOtherResolver{})

	processor := NewConfigProcessor(registry)

	cfg := &Config{
		MockSecret:  "mock://value",
		OtherSecret: "other://value",
	}

	ctx := context.Background()
	result, err := processor.Process(ctx, cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Resolved != 2 {
		t.Fatalf("expected 2 resolved secrets, got %d", result.Resolved)
	}

	// Each resolver must have claimed only its own scheme.
	if cfg.MockSecret != "value" {
		t.Errorf("expected 'value', got %q", cfg.MockSecret)
	}

	if cfg.OtherSecret != "other-value" {
		t.Errorf("expected 'other-value', got %q", cfg.OtherSecret)
	}
}

// TestConfigProcessor_Process_SchemelessValueIsVerbatim guards the resolver SPI
// contract: dispatch is by URI scheme only. A scheme-less config value — even
// one that looks like a placeholder ("123456", "changeme", "auto") — is not a
// secret reference and must survive processing byte-for-byte. Silently swapping
// such a value for generated entropy corrupts length-constrained fields such as
// a TPM TPM2B_AUTH.
func TestConfigProcessor_Process_SchemelessValueIsVerbatim(t *testing.T) {
	type Config struct {
		UserPIN  string
		Password string
		Mode     string
	}

	registry := NewResolverRegistry()
	registry.Register(&mockResolver{})

	processor := NewConfigProcessor(registry)

	cfg := &Config{
		UserPIN:  "123456",
		Password: "changeme",
		Mode:     "auto",
	}

	ctx := context.Background()
	result, err := processor.Process(ctx, cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Resolved != 0 {
		t.Fatalf("expected 0 resolved secrets, got %d", result.Resolved)
	}

	if len(result.Errors) != 0 {
		t.Fatalf("expected no errors, got %v", result.Errors)
	}

	if cfg.UserPIN != "123456" {
		t.Errorf("expected UserPIN '123456', got %q", cfg.UserPIN)
	}

	if cfg.Password != "changeme" {
		t.Errorf("expected Password 'changeme', got %q", cfg.Password)
	}

	if cfg.Mode != "auto" {
		t.Errorf("expected Mode 'auto', got %q", cfg.Mode)
	}
}

func TestConfigProcessor_Process_MapWithEmptyStringValue(t *testing.T) {
	type Config struct {
		Secrets map[string]string
	}

	registry := NewResolverRegistry()
	registry.Register(&mockResolver{})

	processor := NewConfigProcessor(registry)

	cfg := &Config{
		Secrets: map[string]string{
			"empty":  "",
			"secret": "mock://value",
		},
	}

	ctx := context.Background()
	result, err := processor.Process(ctx, cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only non-empty secret should be resolved.
	if result.Resolved != 1 {
		t.Fatalf("expected 1 resolved secret, got %d", result.Resolved)
	}

	if cfg.Secrets["empty"] != "" {
		t.Errorf("empty string should remain empty, got %q", cfg.Secrets["empty"])
	}
	if cfg.Secrets["secret"] != "value" {
		t.Errorf("expected 'value', got %q", cfg.Secrets["secret"])
	}
}

func TestConfigProcessor_Process_MapWithFailingSecret(t *testing.T) {
	type Config struct {
		Secrets map[string]string
	}

	registry := NewResolverRegistry()
	registry.Register(&mockResolver{})
	registry.Register(&failingResolver{})

	processor := NewConfigProcessor(registry)

	cfg := &Config{
		Secrets: map[string]string{
			"good": "mock://good-value",
			"bad":  "fail://bad-value",
		},
	}

	ctx := context.Background()
	result, err := processor.Process(ctx, cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have 1 resolved and 1 error.
	if result.Resolved != 1 {
		t.Fatalf("expected 1 resolved secret, got %d", result.Resolved)
	}
	if len(result.Errors) != 1 {
		t.Fatalf("expected 1 error, got %d", len(result.Errors))
	}
	if len(result.Unresolved) != 1 {
		t.Fatalf("expected 1 unresolved, got %d", len(result.Unresolved))
	}

	if cfg.Secrets["good"] != "good-value" {
		t.Errorf("expected 'good-value', got %q", cfg.Secrets["good"])
	}
	// Bad value should be unchanged.
	if cfg.Secrets["bad"] != "fail://bad-value" {
		t.Errorf("expected 'fail://bad-value', got %q", cfg.Secrets["bad"])
	}
}

func TestConfigProcessor_Process_MapWithSliceValues(t *testing.T) {
	type Config struct {
		Data map[string][]string
	}

	registry := NewResolverRegistry()
	registry.Register(&mockResolver{})

	processor := NewConfigProcessor(registry)

	cfg := &Config{
		Data: map[string][]string{
			"secrets": {"mock://s1", "literal", "mock://s3"},
		},
	}

	ctx := context.Background()
	result, err := processor.Process(ctx, cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Slices in maps should be processed.
	if result.Resolved != 2 {
		t.Fatalf("expected 2 resolved secrets, got %d", result.Resolved)
	}

	if cfg.Data["secrets"][0] != "s1" {
		t.Errorf("expected 's1', got %q", cfg.Data["secrets"][0])
	}
	if cfg.Data["secrets"][1] != "literal" {
		t.Errorf("expected 'literal', got %q", cfg.Data["secrets"][1])
	}
	if cfg.Data["secrets"][2] != "s3" {
		t.Errorf("expected 's3', got %q", cfg.Data["secrets"][2])
	}
}

func TestConfigProcessor_Process_MapWithPointerValues(t *testing.T) {
	type Item struct {
		Secret string
	}

	type Config struct {
		Items map[string]*Item
	}

	registry := NewResolverRegistry()
	registry.Register(&mockResolver{})

	processor := NewConfigProcessor(registry)

	cfg := &Config{
		Items: map[string]*Item{
			"item1": {Secret: "mock://ptr-map-1"},
			"item2": nil, // nil pointer
			"item3": {Secret: "mock://ptr-map-3"},
		},
	}

	ctx := context.Background()
	result, err := processor.Process(ctx, cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Resolved != 2 {
		t.Fatalf("expected 2 resolved secrets, got %d", result.Resolved)
	}

	if cfg.Items["item1"].Secret != "ptr-map-1" {
		t.Errorf("expected 'ptr-map-1', got %q", cfg.Items["item1"].Secret)
	}
	if cfg.Items["item3"].Secret != "ptr-map-3" {
		t.Errorf("expected 'ptr-map-3', got %q", cfg.Items["item3"].Secret)
	}
}

func TestConfigProcessor_Process_MapWithMapValues(t *testing.T) {
	type Config struct {
		NestedMaps map[string]map[string]string
	}

	registry := NewResolverRegistry()
	registry.Register(&mockResolver{})

	processor := NewConfigProcessor(registry)

	cfg := &Config{
		NestedMaps: map[string]map[string]string{
			"group1": {
				"secret1": "mock://nested-1",
				"secret2": "mock://nested-2",
			},
			"group2": {
				"secret3": "mock://nested-3",
			},
		},
	}

	ctx := context.Background()
	result, err := processor.Process(ctx, cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Resolved != 3 {
		t.Fatalf("expected 3 resolved secrets, got %d", result.Resolved)
	}

	if cfg.NestedMaps["group1"]["secret1"] != "nested-1" {
		t.Errorf("expected 'nested-1', got %q", cfg.NestedMaps["group1"]["secret1"])
	}
	if cfg.NestedMaps["group1"]["secret2"] != "nested-2" {
		t.Errorf("expected 'nested-2', got %q", cfg.NestedMaps["group1"]["secret2"])
	}
	if cfg.NestedMaps["group2"]["secret3"] != "nested-3" {
		t.Errorf("expected 'nested-3', got %q", cfg.NestedMaps["group2"]["secret3"])
	}
}

func TestConfigProcessor_Process_EmptyAndNilContainers(t *testing.T) {
	type SliceConfig struct {
		Secrets []string
	}
	type MapConfig struct {
		Secrets map[string]string
	}
	type StringConfig struct {
		Empty  string
		Secret string
	}

	tests := []struct {
		name         string
		cfg          interface{}
		wantResolved int
	}{
		{name: "empty slice", cfg: &SliceConfig{Secrets: []string{}}},
		{name: "nil slice", cfg: &SliceConfig{Secrets: nil}},
		{name: "empty map", cfg: &MapConfig{Secrets: map[string]string{}}},
		{name: "nil map", cfg: &MapConfig{Secrets: nil}},
		{name: "empty string beside a secret", cfg: &StringConfig{Empty: "", Secret: "mock://value"}, wantResolved: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := NewResolverRegistry()
			registry.Register(&mockResolver{})
			processor := NewConfigProcessor(registry)

			result, err := processor.Process(context.Background(), tt.cfg)

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.Resolved != tt.wantResolved {
				t.Fatalf("expected %d resolved secrets, got %d", tt.wantResolved, result.Resolved)
			}
			if len(result.Errors) != 0 {
				t.Fatalf("expected 0 errors, got %d", len(result.Errors))
			}
		})
	}
}

// ============================================================================
// Additional tests for coverage gaps
// ============================================================================

// TestConfigProcessor_Process_ChannelField tests that channel types are skipped.
func TestConfigProcessor_Process_NonStringFieldsUntouched(t *testing.T) {
	type Config struct {
		Channel   chan string
		Callback  func() string
		Complex   complex128
		Complex64 complex64
		Int8      int8
		Int64     int64
		Uint8     uint8
		Uint64    uint64
		Float32   float32
		Float64   float64
		Enabled   bool
		Data      interface{}
		Secret    string
	}

	registry := NewResolverRegistry()
	registry.Register(&mockResolver{})
	processor := NewConfigProcessor(registry)

	ch := make(chan string, 1)
	cfg := &Config{
		Channel:   ch,
		Callback:  func() string { return "callback" },
		Complex:   1 + 2i,
		Complex64: 3 + 4i,
		Int8:      1,
		Int64:     4,
		Uint8:     5,
		Uint64:    8,
		Float32:   9.0,
		Float64:   10.5,
		Enabled:   true,
		Data:      nil,
		Secret:    "mock://kinds-test",
	}

	result, err := processor.Process(context.Background(), cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Resolved != 1 {
		t.Fatalf("expected 1 resolved secret, got %d", result.Resolved)
	}
	if len(result.Errors) != 0 {
		t.Fatalf("expected 0 errors, got %d", len(result.Errors))
	}
	if cfg.Secret != "kinds-test" {
		t.Errorf("expected 'kinds-test', got %q", cfg.Secret)
	}

	untouched := []struct {
		name string
		ok   bool
	}{
		{"Channel", cfg.Channel == ch},
		{"Callback", cfg.Callback != nil && cfg.Callback() == "callback"},
		{"Complex", cfg.Complex == 1+2i},
		{"Complex64", cfg.Complex64 == 3+4i},
		{"Int8", cfg.Int8 == 1},
		{"Int64", cfg.Int64 == 4},
		{"Uint8", cfg.Uint8 == 5},
		{"Uint64", cfg.Uint64 == 8},
		{"Float32", cfg.Float32 == 9.0},
		{"Float64", cfg.Float64 == 10.5},
		{"Enabled", cfg.Enabled},
		{"Data", cfg.Data == nil},
	}
	for _, f := range untouched {
		if !f.ok {
			t.Errorf("field %s changed unexpectedly", f.name)
		}
	}
}

// TestConfigProcessor_Process_FuncField tests that func types are skipped.
// TestConfigProcessor_Process_MapInterfaceWithString tests maps with interface{} values.
func TestConfigProcessor_Process_MapInterfaceWithString(t *testing.T) {
	type Config struct {
		Data map[string]interface{}
	}

	registry := NewResolverRegistry()
	registry.Register(&mockResolver{})

	processor := NewConfigProcessor(registry)

	cfg := &Config{
		Data: map[string]interface{}{
			"secret":  "mock://interface-map-value",
			"literal": "plain-text",
			"number":  42,
			"nested": map[string]interface{}{
				"inner": "mock://nested-interface",
			},
		},
	}

	ctx := context.Background()
	result, err := processor.Process(ctx, cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Interface string values in maps are not directly settable.
	if len(result.Errors) > 0 {
		t.Errorf("unexpected errors: %v", result.Errors)
	}
}

// TestConfigProcessor_Process_DeeplyNestedMaps tests deeply nested map structures.
func TestConfigProcessor_Process_DeeplyNestedMaps(t *testing.T) {
	type Config struct {
		Level1 map[string]map[string]map[string]string
	}

	registry := NewResolverRegistry()
	registry.Register(&mockResolver{})

	processor := NewConfigProcessor(registry)

	cfg := &Config{
		Level1: map[string]map[string]map[string]string{
			"a": {
				"b": {
					"secret": "mock://deep-nested-map",
				},
			},
		},
	}

	ctx := context.Background()
	result, err := processor.Process(ctx, cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Resolved != 1 {
		t.Fatalf("expected 1 resolved secret, got %d", result.Resolved)
	}

	if cfg.Level1["a"]["b"]["secret"] != "deep-nested-map" {
		t.Errorf("expected 'deep-nested-map', got %q", cfg.Level1["a"]["b"]["secret"])
	}
}

// TestConfigProcessor_Process_InterfaceWithStruct tests interface fields containing structs.
func TestConfigProcessor_Process_StructInInterface(t *testing.T) {
	type Inner struct {
		Secret string
	}

	type Config struct {
		Data interface{}
	}

	tests := []struct {
		name         string
		data         interface{}
		wantResolved int
		wantSecret   func(data interface{}) string
	}{
		{
			// A struct value inside an interface is not settable.
			name:       "struct value",
			data:       Inner{Secret: "mock://interface-struct"},
			wantSecret: func(data interface{}) string { return data.(Inner).Secret },
		},
		{
			// A pointer inside an interface makes the fields settable.
			name:         "pointer to struct",
			data:         &Inner{Secret: "mock://interface-struct"},
			wantResolved: 1,
			wantSecret:   func(data interface{}) string { return data.(*Inner).Secret },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := NewResolverRegistry()
			registry.Register(&mockResolver{})
			processor := NewConfigProcessor(registry)

			cfg := &Config{Data: tt.data}

			result, err := processor.Process(context.Background(), cfg)

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(result.Errors) != 0 {
				t.Fatalf("unexpected errors: %v", result.Errors)
			}
			if result.Resolved != tt.wantResolved {
				t.Fatalf("expected %d resolved secrets, got %d", tt.wantResolved, result.Resolved)
			}
			want := "mock://interface-struct"
			if tt.wantResolved == 1 {
				want = "interface-struct"
			}
			if got := tt.wantSecret(cfg.Data); got != want {
				t.Errorf("expected %q, got %q", want, got)
			}
		})
	}
}

// TestConfigProcessor_Process_InterfaceWithPointerStruct tests interface fields
// containing pointer to struct.
// TestConfigProcessor_Process_ContextCancellationDuringStruct tests context
// cancellation while processing struct fields.
// TestConfigProcessor_Process_MapWithInterfaceContainingSlice tests map with
// interface values containing slices.
func TestConfigProcessor_Process_MapWithInterfaceContainingSlice(t *testing.T) {
	type Config struct {
		Data map[string]interface{}
	}

	registry := NewResolverRegistry()
	registry.Register(&mockResolver{})

	processor := NewConfigProcessor(registry)

	cfg := &Config{
		Data: map[string]interface{}{
			"secrets": []string{"mock://slice-in-interface-1", "mock://slice-in-interface-2"},
		},
	}

	ctx := context.Background()
	result, err := processor.Process(ctx, cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Slices inside interface values in maps should be processed.
	if result.Resolved != 2 {
		t.Fatalf("expected 2 resolved secrets (from slice in interface), got %d", result.Resolved)
	}
}

// TestConfigProcessor_Process_NumericTypes tests that various numeric types are skipped.
// TestConfigProcessor_Process_BoolField tests that bool types are skipped.
// TestConfigProcessor_Process_MapWithNonStringKey tests maps with non-string keys.
func TestConfigProcessor_Process_MapWithNonStringKey(t *testing.T) {
	type Config struct {
		IntKeyMap map[int]string
	}

	registry := NewResolverRegistry()
	registry.Register(&mockResolver{})

	processor := NewConfigProcessor(registry)

	cfg := &Config{
		IntKeyMap: map[int]string{
			1: "mock://int-key-1",
			2: "mock://int-key-2",
		},
	}

	ctx := context.Background()
	result, err := processor.Process(ctx, cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Maps with non-string keys should still be processed.
	if result.Resolved != 2 {
		t.Fatalf("expected 2 resolved secrets, got %d", result.Resolved)
	}

	if cfg.IntKeyMap[1] != "int-key-1" {
		t.Errorf("expected 'int-key-1', got %q", cfg.IntKeyMap[1])
	}
	if cfg.IntKeyMap[2] != "int-key-2" {
		t.Errorf("expected 'int-key-2', got %q", cfg.IntKeyMap[2])
	}
}

// TestConfigProcessor_Process_NonSecretURINotResolved tests that strings
// without secret URI prefix are not resolved.
func TestConfigProcessor_Process_NonSecretURINotResolved(t *testing.T) {
	type Config struct {
		Host     string
		Path     string
		URL      string
		Password string
	}

	registry := NewResolverRegistry()
	registry.Register(&mockResolver{})

	processor := NewConfigProcessor(registry)

	cfg := &Config{
		Host:     "localhost:8080",
		Path:     "/var/lib/data",
		URL:      "https://example.com/api",
		Password: "mock://real-password",
	}

	ctx := context.Background()
	result, err := processor.Process(ctx, cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only Password should be resolved (has mock:// prefix).
	if result.Resolved != 1 {
		t.Fatalf("expected 1 resolved secret, got %d", result.Resolved)
	}

	// Non-secret fields should be unchanged.
	if cfg.Host != "localhost:8080" {
		t.Errorf("Host should be unchanged, got %q", cfg.Host)
	}
	if cfg.Path != "/var/lib/data" {
		t.Errorf("Path should be unchanged, got %q", cfg.Path)
	}
	if cfg.URL != "https://example.com/api" {
		t.Errorf("URL should be unchanged, got %q", cfg.URL)
	}
	if cfg.Password != "real-password" {
		t.Errorf("expected 'real-password', got %q", cfg.Password)
	}
}

// TestConfigProcessor_Process_MapNonSecretNotResolved tests that map string values
// without secret URI are not resolved.
func TestConfigProcessor_Process_MapNonSecretNotResolved(t *testing.T) {
	type Config struct {
		Settings map[string]string
	}

	registry := NewResolverRegistry()
	registry.Register(&mockResolver{})

	processor := NewConfigProcessor(registry)

	cfg := &Config{
		Settings: map[string]string{
			"host":     "localhost",
			"port":     "8080",
			"password": "mock://the-password",
		},
	}

	ctx := context.Background()
	result, err := processor.Process(ctx, cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only password should be resolved.
	if result.Resolved != 1 {
		t.Fatalf("expected 1 resolved secret, got %d", result.Resolved)
	}

	if cfg.Settings["host"] != "localhost" {
		t.Errorf("host should be unchanged, got %q", cfg.Settings["host"])
	}
	if cfg.Settings["port"] != "8080" {
		t.Errorf("port should be unchanged, got %q", cfg.Settings["port"])
	}
	if cfg.Settings["password"] != "the-password" {
		t.Errorf("expected 'the-password', got %q", cfg.Settings["password"])
	}
}

// TestConfigProcessor_Process_EmbeddedStruct tests embedded struct fields.
func TestConfigProcessor_Process_EmbeddedStruct(t *testing.T) {
	type Credentials struct {
		Username string
		Password string
	}

	type Config struct {
		Credentials
		Name string
	}

	registry := NewResolverRegistry()
	registry.Register(&mockResolver{})

	processor := NewConfigProcessor(registry)

	cfg := &Config{
		Credentials: Credentials{
			Username: "admin",
			Password: "mock://embedded-password",
		},
		Name: "test",
	}

	ctx := context.Background()
	result, err := processor.Process(ctx, cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Resolved != 1 {
		t.Fatalf("expected 1 resolved secret, got %d", result.Resolved)
	}

	if cfg.Password != "embedded-password" {
		t.Errorf("expected 'embedded-password', got %q", cfg.Password)
	}
}

// TestConfigProcessor_Process_SliceOfInterfaces tests slices of interface{}.
func TestConfigProcessor_Process_SliceOfInterfaces(t *testing.T) {
	type Config struct {
		Items []interface{}
	}

	registry := NewResolverRegistry()
	registry.Register(&mockResolver{})

	processor := NewConfigProcessor(registry)

	cfg := &Config{
		Items: []interface{}{
			"mock://slice-interface-1",
			42,
			"mock://slice-interface-2",
			nil,
			map[string]string{"key": "mock://map-in-slice"},
		},
	}

	ctx := context.Background()
	result, err := processor.Process(ctx, cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// String values in interface slice are not settable, but map should be processed.
	if result.Resolved != 1 {
		t.Fatalf("expected 1 resolved secret (from map), got %d", result.Resolved)
	}
}

// TestConfigProcessor_Process_WithFailClosed verifies that WithFailClosed causes
// Process to return ErrUnresolvedSecretURI when resolution fails.
func TestConfigProcessor_Process_WithFailClosed(t *testing.T) {
	type Config struct {
		Secret string
	}

	registry := NewResolverRegistry()
	registry.Register(&failingResolver{})

	processor := NewConfigProcessor(registry, WithFailClosed())

	cfg := &Config{Secret: "fail://bad-secret"}

	ctx := context.Background()
	result, err := processor.Process(ctx, cfg)

	if result == nil {
		t.Fatal("expected non-nil result even with error")
	}
	if !stderrors.Is(err, ErrUnresolvedSecretURI) {
		t.Errorf("expected ErrUnresolvedSecretURI, got %v", err)
	}
	if len(result.Unresolved) == 0 {
		t.Error("expected non-empty Unresolved")
	}
}
