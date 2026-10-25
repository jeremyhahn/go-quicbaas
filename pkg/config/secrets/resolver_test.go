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

// plaintextOf returns v's plaintext so a test can assert on it. Copying the
// secret into a string is acceptable only in a test.
func plaintextOf(t *testing.T, v *SecretValue) string {
	t.Helper()
	var plaintext string
	err := v.WithBytes(t.Context(), func(b []byte) error {
		plaintext = string(b) //secure:unwiped test assertion only
		return nil
	})
	if err != nil {
		t.Fatalf("WithBytes: %v", err)
	}
	return plaintext
}

// testResolver is a simple mock resolver for testing the registry
type testResolver struct {
	schemeName     string
	canResolveFunc func(string) bool
	resolveFunc    func(context.Context, string) (*SecretValue, error)
}

func (m *testResolver) Scheme() string {
	return m.schemeName
}

func (m *testResolver) CanResolve(raw string) bool {
	if m.canResolveFunc != nil {
		return m.canResolveFunc(raw)
	}
	return false
}

func (m *testResolver) Resolve(ctx context.Context, raw string) (*SecretValue, error) {
	if m.resolveFunc != nil {
		return m.resolveFunc(ctx, raw)
	}
	return nil, stderrors.New("not implemented")
}

func TestNewResolverRegistry(t *testing.T) {
	registry := NewResolverRegistry()
	if registry == nil {
		t.Fatal("Expected non-nil registry")
	}
	if len(registry.Resolvers()) != 0 {
		t.Errorf("Expected empty resolvers, got %d", len(registry.Resolvers()))
	}
}

func TestResolverRegistry_Register(t *testing.T) {
	registry := NewResolverRegistry()

	// Register a resolver
	resolver := &testResolver{schemeName: "test"}
	registry.Register(resolver)

	if len(registry.Resolvers()) != 1 {
		t.Errorf("Expected 1 resolver, got %d", len(registry.Resolvers()))
	}

	// Register nil should be ignored
	registry.Register(nil)
	if len(registry.Resolvers()) != 1 {
		t.Errorf("Expected 1 resolver after nil register, got %d", len(registry.Resolvers()))
	}

	// Register another resolver
	resolver2 := &testResolver{schemeName: "test2"}
	registry.Register(resolver2)
	if len(registry.Resolvers()) != 2 {
		t.Errorf("Expected 2 resolvers, got %d", len(registry.Resolvers()))
	}
}

func TestResolverRegistry_Resolvers(t *testing.T) {
	registry := NewResolverRegistry()

	// Empty registry
	resolvers := registry.Resolvers()
	if len(resolvers) != 0 {
		t.Errorf("Expected empty resolvers, got %d", len(resolvers))
	}

	// Add resolvers
	resolver1 := &testResolver{schemeName: "test1"}
	resolver2 := &testResolver{schemeName: "test2"}
	registry.Register(resolver1)
	registry.Register(resolver2)

	resolvers = registry.Resolvers()
	if len(resolvers) != 2 {
		t.Errorf("Expected 2 resolvers, got %d", len(resolvers))
	}

	// Verify it's a copy (modifying returned slice doesn't affect registry)
	resolvers[0] = nil
	registryResolvers := registry.Resolvers()
	if registryResolvers[0] == nil {
		t.Error("Modifying returned slice should not affect registry")
	}
}

func TestResolverRegistry_Resolve_NilContext(t *testing.T) {
	registry := NewResolverRegistry()
	var nilCtx context.Context
	_, err := registry.Resolve(nilCtx, "test")
	if err == nil {
		t.Fatal("Expected error for nil context")
	}
	if !stderrors.Is(err, qrdbsecret.ErrNilContext) {
		t.Errorf("Expected ErrNilContext, got %T: %v", err, err)
	}
}

func TestResolverRegistry_Resolve_LiteralValue(t *testing.T) {
	registry := NewResolverRegistry()
	ctx := context.Background()

	// No resolvers registered, should return literal
	value, err := registry.Resolve(ctx, "plaintext-value")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if value.Strategy != StrategyLiteral {
		t.Errorf("Expected StrategyLiteral, got %v", value.Strategy)
	}
	if plaintextOf(t, value) != "plaintext-value" {
		t.Errorf("Expected 'plaintext-value', got %s", plaintextOf(t, value))
	}
	if value.Raw != "plaintext-value" {
		t.Errorf("Expected raw 'plaintext-value', got %s", value.Raw)
	}
}

func TestResolverRegistry_Resolve_MatchingResolver(t *testing.T) {
	registry := NewResolverRegistry()
	ctx := context.Background()

	resolver := &testResolver{
		schemeName: "test",
		canResolveFunc: func(raw string) bool {
			return len(raw) > 7 && raw[:7] == "test://"
		},
		resolveFunc: func(ctx context.Context, raw string) (*SecretValue, error) {
			return NewSecretValue(ctx, raw, []byte("resolved-value"), StrategyLocalKMS)
		},
	}
	registry.Register(resolver)

	value, err := registry.Resolve(ctx, "test://secret")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if plaintextOf(t, value) != "resolved-value" {
		t.Errorf("Expected 'resolved-value', got %s", plaintextOf(t, value))
	}
	if value.Strategy != StrategyLocalKMS {
		t.Errorf("Expected StrategyLocalKMS, got %v", value.Strategy)
	}
}

func TestResolverRegistry_Resolve_ResolverError(t *testing.T) {
	registry := NewResolverRegistry()
	ctx := context.Background()

	resolver := &testResolver{
		schemeName: "test",
		canResolveFunc: func(raw string) bool {
			return len(raw) > 7 && raw[:7] == "test://"
		},
		resolveFunc: func(ctx context.Context, raw string) (*SecretValue, error) {
			return nil, stderrors.New("resolution failed")
		},
	}
	registry.Register(resolver)

	_, err := registry.Resolve(ctx, "test://secret")
	if err == nil {
		t.Fatal("Expected error")
	}

	var rfe *qrdbsecret.ResolutionFailedError
	if !stderrors.As(err, &rfe) {
		t.Fatalf("Expected ResolutionFailedError, got %T", err)
	}
	if rfe.Scheme != "test" {
		t.Errorf("Expected scheme 'test', got %v", rfe.Scheme)
	}
}

func TestResolverRegistry_Resolve_FirstMatchWins(t *testing.T) {
	registry := NewResolverRegistry()
	ctx := context.Background()

	// Register two resolvers that both can handle the same input
	resolver1 := &testResolver{
		schemeName:     "first",
		canResolveFunc: func(raw string) bool { return true },
		resolveFunc: func(ctx context.Context, raw string) (*SecretValue, error) {
			return NewSecretValue(ctx, raw, []byte("from-first"), StrategyLiteral)
		},
	}
	resolver2 := &testResolver{
		schemeName:     "second",
		canResolveFunc: func(raw string) bool { return true },
		resolveFunc: func(ctx context.Context, raw string) (*SecretValue, error) {
			return NewSecretValue(ctx, raw, []byte("from-second"), StrategyLiteral)
		},
	}
	registry.Register(resolver1)
	registry.Register(resolver2)

	value, err := registry.Resolve(ctx, "any-value")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if plaintextOf(t, value) != "from-first" {
		t.Errorf("Expected 'from-first', got %s", plaintextOf(t, value))
	}
}

func TestResolverRegistry_IsSecretURI(t *testing.T) {
	registry := NewResolverRegistry()

	// Empty registry
	if registry.IsSecretURI("kms://secret") {
		t.Error("Expected false for empty registry")
	}

	// Add resolver
	resolver := &testResolver{
		schemeName: "kms",
		canResolveFunc: func(raw string) bool {
			return len(raw) > 6 && raw[:6] == "kms://"
		},
	}
	registry.Register(resolver)

	if !registry.IsSecretURI("kms://secret") {
		t.Error("Expected true for matching URI")
	}
	if registry.IsSecretURI("other://secret") {
		t.Error("Expected false for non-matching URI")
	}
	if registry.IsSecretURI("plaintext") {
		t.Error("Expected false for plaintext value")
	}
}

func TestResolverRegistry_ConcurrentAccess(t *testing.T) {
	registry := NewResolverRegistry()
	ctx := context.Background()

	// Register a resolver
	resolver := &testResolver{
		schemeName:     "test",
		canResolveFunc: func(raw string) bool { return false },
		resolveFunc:    func(ctx context.Context, raw string) (*SecretValue, error) { return nil, nil },
	}
	registry.Register(resolver)

	// Concurrent reads and writes
	done := make(chan bool)

	// Reader goroutines
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				_ = registry.IsSecretURI("test")
				_, _ = registry.Resolve(ctx, "test")
				_ = registry.Resolvers()
			}
			done <- true
		}()
	}

	// Writer goroutines
	for i := 0; i < 5; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				registry.Register(&testResolver{schemeName: "dynamic"})
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 15; i++ {
		<-done
	}
}

func TestResolvedStrategy_Constants(t *testing.T) {
	// Verify strategy constants are correctly defined
	if StrategyLiteral != "literal" {
		t.Errorf("Expected 'literal', got %s", StrategyLiteral)
	}
	if StrategyLocalKMS != "local_kms" {
		t.Errorf("Expected 'local_kms', got %s", StrategyLocalKMS)
	}
	if StrategyRemoteKMS != "remote_kms" {
		t.Errorf("Expected 'remote_kms', got %s", StrategyRemoteKMS)
	}
	if StrategyAutoGen != "autogen" {
		t.Errorf("Expected 'autogen', got %s", StrategyAutoGen)
	}
	if StrategyEncrypted != "encrypted" {
		t.Errorf("Expected 'encrypted', got %s", StrategyEncrypted)
	}
}
