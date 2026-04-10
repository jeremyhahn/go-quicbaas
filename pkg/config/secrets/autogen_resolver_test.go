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
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestAutoGenResolver_Scheme(t *testing.T) {
	resolver := NewAutoGenResolver()
	if got := resolver.Scheme(); got != "auto" {
		t.Errorf("Scheme() = %q, want %q", got, "auto")
	}
}

func TestAutoGenResolver_CanResolve(t *testing.T) {
	resolver := NewAutoGenResolver()

	tests := []struct {
		name string
		raw  string
		want bool
	}{
		// Positive cases - should resolve
		{name: "auto lowercase", raw: "auto", want: true},
		{name: "auto uppercase", raw: "AUTO", want: true},
		{name: "auto mixed case", raw: "Auto", want: true},
		{name: "123456", raw: "123456", want: true},
		{name: "changeme lowercase", raw: "changeme", want: true},
		{name: "CHANGEME uppercase", raw: "CHANGEME", want: true},
		{name: "changeme mixed", raw: "ChangeMe", want: true},
		{name: "placeholder", raw: "placeholder", want: true},
		{name: "placeholder uppercase", raw: "PLACEHOLDER", want: true},
		{name: "auto with leading space", raw: " auto", want: true},
		{name: "auto with trailing space", raw: "auto ", want: true},
		{name: "auto with surrounding spaces", raw: "  auto  ", want: true},

		// Negative cases - should not resolve
		{name: "empty string", raw: "", want: false},
		{name: "random string", raw: "my-secret-value", want: false},
		{name: "auto prefix", raw: "auto-generate", want: false},
		{name: "auto suffix", raw: "myauto", want: false},
		{name: "similar to placeholder", raw: "placeholders", want: false},
		{name: "uuid", raw: "550e8400-e29b-41d4-a716-446655440000", want: false},
		{name: "actual secret", raw: "dGhpcyBpcyBhIHNlY3JldA==", want: false},
		{name: "12345 not 123456", raw: "12345", want: false},
		{name: "1234567", raw: "1234567", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolver.CanResolve(tt.raw)
			if got != tt.want {
				t.Errorf("CanResolve(%q) = %v, want %v", tt.raw, got, tt.want)
			}
		})
	}
}

func TestAutoGenResolver_Resolve_Success(t *testing.T) {
	ctx := context.Background()
	resolver := NewAutoGenResolver()

	result, err := resolver.Resolve(ctx, "auto")
	if err != nil {
		t.Fatalf("Resolve() error = %v, want nil", err)
	}

	// Verify result is not nil
	if result == nil {
		t.Fatal("Resolve() returned nil result")
	}

	// Verify strategy
	if result.Strategy != StrategyAutoGen {
		t.Errorf("Strategy = %q, want %q", result.Strategy, StrategyAutoGen)
	}

	// Verify raw value is preserved
	if result.Raw != "auto" {
		t.Errorf("Raw = %q, want %q", result.Raw, "auto")
	}

	// Verify value is not empty
	if result.Value == "" {
		t.Error("Value is empty")
	}

	// Verify value is valid URL-safe base64
	decoded, err := base64.RawURLEncoding.DecodeString(result.Value)
	if err != nil {
		t.Errorf("Value is not valid URL-safe base64: %v", err)
	}

	// Verify decoded length matches secret size
	if len(decoded) != DefaultSecretSize {
		t.Errorf("Decoded length = %d, want %d", len(decoded), DefaultSecretSize)
	}
}

func TestAutoGenResolver_Resolve_UniquenessPerCall(t *testing.T) {
	ctx := context.Background()
	resolver := NewAutoGenResolver()

	const iterations = 100
	seen := make(map[string]bool, iterations)

	for i := 0; i < iterations; i++ {
		result, err := resolver.Resolve(ctx, "auto")
		if err != nil {
			t.Fatalf("Resolve() iteration %d error = %v", i, err)
		}

		if seen[result.Value] {
			t.Errorf("Duplicate value generated at iteration %d: %s", i, result.Value)
		}
		seen[result.Value] = true
	}
}

func TestAutoGenResolver_Resolve_AllPlaceholders(t *testing.T) {
	ctx := context.Background()
	resolver := NewAutoGenResolver()

	for _, placeholder := range AutoGenPlaceholders {
		t.Run(placeholder, func(t *testing.T) {
			result, err := resolver.Resolve(ctx, placeholder)
			if err != nil {
				t.Fatalf("Resolve(%q) error = %v", placeholder, err)
			}
			if result == nil {
				t.Fatalf("Resolve(%q) returned nil", placeholder)
			}
			if result.Value == "" {
				t.Errorf("Resolve(%q) returned empty value", placeholder)
			}
			// Verify raw value is preserved
			if result.Raw != placeholder {
				t.Errorf("Raw = %q, want %q", result.Raw, placeholder)
			}
		})
	}
}

func TestAutoGenResolver_WithSecretSize(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name         string
		size         int
		expectedSize int
	}{
		{name: "size 16", size: 16, expectedSize: 16},
		{name: "size 32", size: 32, expectedSize: 32},
		{name: "size 64", size: 64, expectedSize: 64},
		{name: "size 1", size: 1, expectedSize: 1},
		{name: "size 0 uses default", size: 0, expectedSize: DefaultSecretSize},
		{name: "negative uses default", size: -1, expectedSize: DefaultSecretSize},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver := NewAutoGenResolver().WithSecretSize(tt.size)

			result, err := resolver.Resolve(ctx, "auto")
			if err != nil {
				t.Fatalf("Resolve() error = %v", err)
			}

			decoded, err := base64.RawURLEncoding.DecodeString(result.Value)
			if err != nil {
				t.Fatalf("Failed to decode value: %v", err)
			}

			if len(decoded) != tt.expectedSize {
				t.Errorf("Decoded length = %d, want %d", len(decoded), tt.expectedSize)
			}
		})
	}
}

// deterministicReader provides predictable bytes for testing.
type deterministicReader struct {
	data   []byte
	offset int
}

func newDeterministicReader(data []byte) *deterministicReader {
	return &deterministicReader{data: data}
}

func (r *deterministicReader) Read(p []byte) (int, error) {
	if r.offset >= len(r.data) {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.offset:])
	r.offset += n
	return n, nil
}

func TestAutoGenResolver_WithEntropySource(t *testing.T) {
	ctx := context.Background()

	// Create deterministic data
	testData := make([]byte, DefaultSecretSize)
	for i := range testData {
		testData[i] = byte(i)
	}

	// Create resolver with deterministic entropy
	entropy := newDeterministicReader(testData)
	resolver := NewAutoGenResolver().WithEntropySource(entropy)

	result, err := resolver.Resolve(ctx, "auto")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	// Verify the output matches expected encoding
	expected := base64.RawURLEncoding.EncodeToString(testData)
	if result.Value != expected {
		t.Errorf("Value = %q, want %q", result.Value, expected)
	}
}

func TestAutoGenResolver_WithEntropySource_Chaining(t *testing.T) {
	// Verify that chaining preserves settings
	testData := make([]byte, 16)
	for i := range testData {
		testData[i] = byte(i + 100)
	}

	entropy := newDeterministicReader(testData)
	resolver := NewAutoGenResolver().
		WithSecretSize(16).
		WithEntropySource(entropy)

	ctx := context.Background()
	result, err := resolver.Resolve(ctx, "auto")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	decoded, err := base64.RawURLEncoding.DecodeString(result.Value)
	if err != nil {
		t.Fatalf("Failed to decode: %v", err)
	}

	if len(decoded) != 16 {
		t.Errorf("Decoded length = %d, want 16", len(decoded))
	}

	if !bytes.Equal(decoded, testData) {
		t.Errorf("Decoded data mismatch")
	}
}

// errorReader always returns an error.
type errorReader struct {
	err error
}

func (r *errorReader) Read(p []byte) (int, error) {
	return 0, r.err
}

func TestAutoGenResolver_Resolve_EntropyError(t *testing.T) {
	ctx := context.Background()
	testErr := errors.New("entropy source failure")

	resolver := NewAutoGenResolver().WithEntropySource(&errorReader{err: testErr})

	result, err := resolver.Resolve(ctx, "auto")

	// Verify error returned
	if err == nil {
		t.Fatal("Resolve() error = nil, want error")
	}

	// Verify result is nil
	if result != nil {
		t.Errorf("Resolve() result = %+v, want nil", result)
	}

	// Verify error type is EntropyGenerationFailedError
	var entropyErr *EntropyGenerationFailedError
	if !errors.As(err, &entropyErr) {
		t.Fatalf("Error is not EntropyGenerationFailedError: %T", err)
	}

	// Verify error contains cause
	if entropyErr.Cause == nil {
		t.Error("Error cause is nil")
	}

	// Verify error message contains entropy info
	if !strings.Contains(err.Error(), "entropy") {
		t.Errorf("Error message does not mention entropy: %s", err.Error())
	}
}

// partialReader returns fewer bytes than requested.
type partialReader struct {
	bytesToReturn int
}

func (r *partialReader) Read(p []byte) (int, error) {
	if r.bytesToReturn <= 0 {
		return 0, io.EOF
	}
	n := r.bytesToReturn
	if n > len(p) {
		n = len(p)
	}
	for i := 0; i < n; i++ {
		p[i] = byte(i)
	}
	r.bytesToReturn = 0
	return n, nil
}

func TestAutoGenResolver_Resolve_PartialRead(t *testing.T) {
	ctx := context.Background()

	// Reader that returns fewer bytes than needed
	resolver := NewAutoGenResolver().
		WithSecretSize(32).
		WithEntropySource(&partialReader{bytesToReturn: 16})

	result, err := resolver.Resolve(ctx, "auto")

	// Should return an error
	if err == nil {
		t.Fatal("Resolve() error = nil, want error for partial read")
	}

	if result != nil {
		t.Errorf("Resolve() result = %+v, want nil", result)
	}

	// Verify error type is EntropyGenerationFailedError
	var entropyErr *EntropyGenerationFailedError
	if !errors.As(err, &entropyErr) {
		t.Fatalf("Error is not EntropyGenerationFailedError: %T", err)
	}

	// Verify RequestedBytes is set
	if entropyErr.RequestedBytes != 32 {
		t.Errorf("RequestedBytes = %d, want 32", entropyErr.RequestedBytes)
	}
}

func TestAutoGenResolver_Resolve_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	resolver := NewAutoGenResolver()

	result, err := resolver.Resolve(ctx, "auto")

	// Should return an error
	if err == nil {
		t.Fatal("Resolve() error = nil, want context cancelled error")
	}

	if result != nil {
		t.Errorf("Resolve() result = %+v, want nil", result)
	}

	// Verify error mentions context
	var resErr *SecretResolutionError
	if !errors.As(err, &resErr) {
		t.Fatalf("Error is not SecretResolutionError: %T", err)
	}

	if !strings.Contains(err.Error(), "context") {
		t.Errorf("Error message does not mention context: %s", err.Error())
	}
}

func TestAutoGenResolver_URLSafeBase64(t *testing.T) {
	ctx := context.Background()
	resolver := NewAutoGenResolver()

	// Generate many secrets and verify they're URL-safe
	for i := 0; i < 50; i++ {
		result, err := resolver.Resolve(ctx, "auto")
		if err != nil {
			t.Fatalf("Resolve() iteration %d error = %v", i, err)
		}

		// URL-safe base64 should not contain + or /
		if strings.ContainsAny(result.Value, "+/=") {
			t.Errorf("Value contains non-URL-safe characters: %s", result.Value)
		}

		// Should be decodable with RawURLEncoding
		_, err = base64.RawURLEncoding.DecodeString(result.Value)
		if err != nil {
			t.Errorf("Value is not valid RawURLEncoding: %v", err)
		}
	}
}

func TestAutoGenResolver_ImplementsInterface(t *testing.T) {
	// Compile-time check that AutoGenResolver implements SecretResolver
	var _ SecretResolver = (*AutoGenResolver)(nil)

	// Runtime verification
	resolver := NewAutoGenResolver()
	var sr SecretResolver = resolver

	if sr.Scheme() != "auto" {
		t.Errorf("Interface Scheme() = %q, want %q", sr.Scheme(), "auto")
	}
}

func TestAutoGenPlaceholders_Contents(t *testing.T) {
	// Verify expected placeholders are present
	expected := map[string]bool{
		"auto":        true,
		"123456":      true,
		"changeme":    true,
		"CHANGEME":    true,
		"placeholder": true,
	}

	for _, p := range AutoGenPlaceholders {
		if !expected[p] {
			t.Errorf("Unexpected placeholder: %q", p)
		}
		delete(expected, p)
	}

	for p := range expected {
		t.Errorf("Missing expected placeholder: %q", p)
	}
}

func TestNewAutoGenResolver_DefaultValues(t *testing.T) {
	resolver := NewAutoGenResolver()

	// Verify default entropy source works
	ctx := context.Background()
	result, err := resolver.Resolve(ctx, "auto")
	if err != nil {
		t.Fatalf("Default entropy source failed: %v", err)
	}

	// Verify default size
	decoded, err := base64.RawURLEncoding.DecodeString(result.Value)
	if err != nil {
		t.Fatalf("Failed to decode: %v", err)
	}

	if len(decoded) != DefaultSecretSize {
		t.Errorf("Default secret size = %d, want %d", len(decoded), DefaultSecretSize)
	}
}

func TestAutoGenResolver_Resolve_PreservesRawValue(t *testing.T) {
	ctx := context.Background()
	resolver := NewAutoGenResolver()

	testCases := []string{"auto", "changeme", "123456", "CHANGEME", "placeholder"}

	for _, raw := range testCases {
		t.Run(raw, func(t *testing.T) {
			result, err := resolver.Resolve(ctx, raw)
			if err != nil {
				t.Fatalf("Resolve(%q) error = %v", raw, err)
			}

			if result.Raw != raw {
				t.Errorf("Raw = %q, want %q", result.Raw, raw)
			}
		})
	}
}

func TestEntropyGenerationFailedError_Error(t *testing.T) {
	tests := []struct {
		name    string
		err     *EntropyGenerationFailedError
		wantMsg string
	}{
		{
			name: "with cause",
			err: &EntropyGenerationFailedError{
				RequestedBytes: 32,
				Cause:          errors.New("random source unavailable"),
			},
			wantMsg: "failed to generate 32 bytes of entropy: random source unavailable",
		},
		{
			name: "without cause",
			err: &EntropyGenerationFailedError{
				RequestedBytes: 16,
			},
			wantMsg: "failed to generate 16 bytes of entropy",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.wantMsg {
				t.Errorf("Error() = %q, want %q", got, tt.wantMsg)
			}
		})
	}
}

func TestEntropyGenerationFailedError_Unwrap(t *testing.T) {
	cause := errors.New("underlying error")
	err := &EntropyGenerationFailedError{
		RequestedBytes: 32,
		Cause:          cause,
	}

	if unwrapped := err.Unwrap(); unwrapped != cause {
		t.Errorf("Unwrap() = %v, want %v", unwrapped, cause)
	}
}
