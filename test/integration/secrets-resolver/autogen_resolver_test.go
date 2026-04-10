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

//go:build integration

package secretsresolver

import (
	"context"
	"encoding/base64"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config/secrets"
)

// =============================================================================
// AutoGenResolver Integration Tests
// =============================================================================

// TestAutoGenResolver_ResolvePlaceholders tests that all placeholder values
// trigger auto-generation of secrets.
func TestAutoGenResolver_ResolvePlaceholders(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resolver := secrets.NewAutoGenResolver()

	placeholders := []string{
		"auto",
		"AUTO",
		"Auto",
		"  auto  ",
		"123456",
		"changeme",
		"CHANGEME",
		"ChangeME",
		"placeholder",
		"PLACEHOLDER",
		"Placeholder",
	}

	for _, placeholder := range placeholders {
		t.Run("Placeholder_"+placeholder, func(t *testing.T) {
			// Verify CanResolve returns true
			canResolve := resolver.CanResolve(placeholder)
			assert.True(t, canResolve, "CanResolve should return true for placeholder: %q", placeholder)

			// Verify Resolve generates a secret
			result, err := resolver.Resolve(ctx, placeholder)
			require.NoError(t, err, "Resolve should not return an error for placeholder: %q", placeholder)
			require.NotNil(t, result, "Resolve should return a non-nil result")

			// Verify result metadata
			assert.Equal(t, placeholder, result.Raw, "Raw value should match input")
			assert.Equal(t, secrets.StrategyAutoGen, result.Strategy, "Strategy should be autogen")
			assert.NotEmpty(t, result.Value, "Generated value should not be empty")

			t.Logf("Placeholder %q generated secret of length %d", placeholder, len(result.Value))
		})
	}
}

// TestAutoGenResolver_EntropyQuality verifies that generated secrets have
// sufficient entropy (32 bytes of random data).
func TestAutoGenResolver_EntropyQuality(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resolver := secrets.NewAutoGenResolver()

	t.Run("DefaultSecretSize", func(t *testing.T) {
		result, err := resolver.Resolve(ctx, "auto")
		require.NoError(t, err)
		require.NotNil(t, result)

		// Decode the base64 value to verify byte count
		decoded, err := base64.RawURLEncoding.DecodeString(result.Value)
		require.NoError(t, err, "Generated value should be valid base64")

		// Default secret size is 32 bytes
		assert.Equal(t, secrets.DefaultSecretSize, len(decoded),
			"Decoded secret should be %d bytes", secrets.DefaultSecretSize)

		t.Logf("Generated %d bytes of entropy (base64 length: %d)",
			len(decoded), len(result.Value))
	})

	t.Run("CustomSecretSize", func(t *testing.T) {
		customSizes := []int{16, 32, 64, 128}

		for _, size := range customSizes {
			customResolver := resolver.WithSecretSize(size)
			result, err := customResolver.Resolve(ctx, "auto")
			require.NoError(t, err)
			require.NotNil(t, result)

			decoded, err := base64.RawURLEncoding.DecodeString(result.Value)
			require.NoError(t, err)

			assert.Equal(t, size, len(decoded),
				"Custom size %d: decoded secret should match requested size", size)

			t.Logf("Custom size %d: generated %d bytes (base64 length: %d)",
				size, len(decoded), len(result.Value))
		}
	})

	t.Run("InvalidSecretSizeDefaultsTo32", func(t *testing.T) {
		invalidSizes := []int{0, -1, -100}

		for _, size := range invalidSizes {
			customResolver := resolver.WithSecretSize(size)
			result, err := customResolver.Resolve(ctx, "auto")
			require.NoError(t, err)
			require.NotNil(t, result)

			decoded, err := base64.RawURLEncoding.DecodeString(result.Value)
			require.NoError(t, err)

			assert.Equal(t, secrets.DefaultSecretSize, len(decoded),
				"Invalid size %d should default to %d bytes", size, secrets.DefaultSecretSize)
		}
	})
}

// TestAutoGenResolver_Base64Encoding verifies that output is URL-safe base64 encoded.
func TestAutoGenResolver_Base64Encoding(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resolver := secrets.NewAutoGenResolver()

	t.Run("URLSafeBase64", func(t *testing.T) {
		// Generate multiple secrets to increase chance of hitting special chars
		for i := 0; i < 100; i++ {
			result, err := resolver.Resolve(ctx, "auto")
			require.NoError(t, err)
			require.NotNil(t, result)

			// URL-safe base64 should not contain + or /
			assert.NotContains(t, result.Value, "+",
				"URL-safe base64 should not contain '+'")
			assert.NotContains(t, result.Value, "/",
				"URL-safe base64 should not contain '/'")

			// Should not contain padding (RawURLEncoding)
			assert.NotContains(t, result.Value, "=",
				"RawURLEncoding should not contain '=' padding")

			// Should be decodable with RawURLEncoding
			_, err = base64.RawURLEncoding.DecodeString(result.Value)
			assert.NoError(t, err, "Value should be valid RawURLEncoding base64")
		}

		t.Logf("Generated 100 secrets, all URL-safe base64 compliant")
	})

	t.Run("ValidBase64Characters", func(t *testing.T) {
		validChars := "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"

		result, err := resolver.Resolve(ctx, "auto")
		require.NoError(t, err)

		for _, char := range result.Value {
			found := false
			for _, validChar := range validChars {
				if char == validChar {
					found = true
					break
				}
			}
			assert.True(t, found,
				"Character %q should be in URL-safe base64 alphabet", string(char))
		}
	})
}

// TestAutoGenResolver_Uniqueness verifies that multiple generated secrets are unique.
func TestAutoGenResolver_Uniqueness(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	resolver := secrets.NewAutoGenResolver()

	t.Run("SequentialUniqueness", func(t *testing.T) {
		const numSecrets = 1000
		secrets := make(map[string]struct{}, numSecrets)

		for i := 0; i < numSecrets; i++ {
			result, err := resolver.Resolve(ctx, "auto")
			require.NoError(t, err)
			require.NotNil(t, result)

			_, exists := secrets[result.Value]
			assert.False(t, exists,
				"Secret collision detected at iteration %d: %s", i, result.Value)

			secrets[result.Value] = struct{}{}
		}

		assert.Equal(t, numSecrets, len(secrets),
			"All %d secrets should be unique", numSecrets)

		t.Logf("Generated %d unique secrets sequentially", numSecrets)
	})

	t.Run("UniquenessAcrossPlaceholders", func(t *testing.T) {
		placeholders := []string{"auto", "123456", "changeme", "placeholder"}
		secretsMap := make(map[string]string)

		for _, placeholder := range placeholders {
			for i := 0; i < 100; i++ {
				result, err := resolver.Resolve(ctx, placeholder)
				require.NoError(t, err)

				key := result.Value
				if existingPlaceholder, exists := secretsMap[key]; exists {
					t.Errorf("Secret collision: %q from placeholder %q already generated from %q",
						key, placeholder, existingPlaceholder)
				}
				secretsMap[key] = placeholder
			}
		}

		t.Logf("Generated %d unique secrets across %d placeholders",
			len(secretsMap), len(placeholders))
	})
}

// TestAutoGenResolver_NotPlaceholder verifies that non-placeholder values
// return false from CanResolve.
func TestAutoGenResolver_NotPlaceholder(t *testing.T) {
	resolver := secrets.NewAutoGenResolver()

	nonPlaceholders := []string{
		"",
		"actual-secret-value",
		"password123",
		"my-secure-token",
		"kms://secret/path",
		"env://MY_SECRET",
		"file:///etc/secrets/token",
		"random_string_123",
		"autogenerate",
		"automate",
		"change",
		"place",
		"holder",
		"12345",
		"1234567",
		"CHANGE_ME",
		"change-me",
		"change_me",
	}

	for _, value := range nonPlaceholders {
		t.Run("NonPlaceholder_"+sanitizeTestName(value), func(t *testing.T) {
			canResolve := resolver.CanResolve(value)
			assert.False(t, canResolve,
				"CanResolve should return false for non-placeholder: %q", value)
		})
	}
}

// TestAutoGenResolver_ConcurrentGeneration verifies thread safety when generating
// secrets concurrently from multiple goroutines.
func TestAutoGenResolver_ConcurrentGeneration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	resolver := secrets.NewAutoGenResolver()

	t.Run("ConcurrentResolve", func(t *testing.T) {
		const numGoroutines = 100
		const secretsPerGoroutine = 50

		var wg sync.WaitGroup
		var mu sync.Mutex
		allSecrets := make(map[string]int)
		errorCount := 0

		wg.Add(numGoroutines)

		for g := 0; g < numGoroutines; g++ {
			go func(goroutineID int) {
				defer wg.Done()

				for i := 0; i < secretsPerGoroutine; i++ {
					result, err := resolver.Resolve(ctx, "auto")
					if err != nil {
						mu.Lock()
						errorCount++
						mu.Unlock()
						continue
					}

					mu.Lock()
					allSecrets[result.Value]++
					mu.Unlock()
				}
			}(g)
		}

		wg.Wait()

		assert.Equal(t, 0, errorCount, "No errors should occur during concurrent generation")

		// Check for collisions
		totalSecrets := numGoroutines * secretsPerGoroutine
		assert.Equal(t, totalSecrets, len(allSecrets),
			"All %d secrets should be unique (found %d unique)", totalSecrets, len(allSecrets))

		// Verify no duplicates
		for secret, count := range allSecrets {
			assert.Equal(t, 1, count,
				"Secret %q was generated %d times (should be 1)", secret[:20]+"...", count)
		}

		t.Logf("Concurrent generation: %d goroutines x %d secrets = %d unique secrets",
			numGoroutines, secretsPerGoroutine, len(allSecrets))
	})

	t.Run("ConcurrentCanResolve", func(t *testing.T) {
		const numGoroutines = 100
		const checksPerGoroutine = 100

		var wg sync.WaitGroup
		wg.Add(numGoroutines)

		for g := 0; g < numGoroutines; g++ {
			go func() {
				defer wg.Done()

				for i := 0; i < checksPerGoroutine; i++ {
					// Mix of valid and invalid placeholders
					if i%2 == 0 {
						assert.True(t, resolver.CanResolve("auto"))
						assert.True(t, resolver.CanResolve("changeme"))
					} else {
						assert.False(t, resolver.CanResolve("not-a-placeholder"))
						assert.False(t, resolver.CanResolve("secret-value"))
					}
				}
			}()
		}

		wg.Wait()

		t.Logf("Concurrent CanResolve: %d goroutines x %d checks completed successfully",
			numGoroutines, checksPerGoroutine)
	})
}

// TestAutoGenResolver_ContextCancellation verifies that context cancellation is respected.
func TestAutoGenResolver_ContextCancellation(t *testing.T) {
	resolver := secrets.NewAutoGenResolver()

	t.Run("CancelledContext", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		result, err := resolver.Resolve(ctx, "auto")
		assert.Error(t, err, "Should return error for cancelled context")
		assert.Nil(t, result, "Result should be nil for cancelled context")

		t.Logf("Cancelled context correctly rejected")
	})

	t.Run("ExpiredContext", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
		defer cancel()

		// Small delay to ensure context expires
		time.Sleep(1 * time.Millisecond)

		result, err := resolver.Resolve(ctx, "auto")
		assert.Error(t, err, "Should return error for expired context")
		assert.Nil(t, result, "Result should be nil for expired context")

		t.Logf("Expired context correctly rejected")
	})
}

// TestAutoGenResolver_Scheme verifies the resolver reports the correct scheme.
func TestAutoGenResolver_Scheme(t *testing.T) {
	resolver := secrets.NewAutoGenResolver()

	scheme := resolver.Scheme()
	assert.Equal(t, "auto", scheme, "Scheme should be 'auto'")
}

// TestAutoGenResolver_ResolverInterface verifies AutoGenResolver implements SecretResolver.
func TestAutoGenResolver_ResolverInterface(t *testing.T) {
	resolver := secrets.NewAutoGenResolver()

	// Verify it implements SecretResolver interface
	var _ secrets.SecretResolver = resolver

	t.Logf("AutoGenResolver correctly implements SecretResolver interface")
}

// TestAutoGenResolver_RegistryIntegration tests AutoGenResolver with ResolverRegistry.
func TestAutoGenResolver_RegistryIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	registry := secrets.NewResolverRegistry()
	autoGenResolver := secrets.NewAutoGenResolver()
	registry.Register(autoGenResolver)

	t.Run("RegistryResolvesPlaceholder", func(t *testing.T) {
		result, err := registry.Resolve(ctx, "auto")
		require.NoError(t, err)
		require.NotNil(t, result)

		assert.Equal(t, secrets.StrategyAutoGen, result.Strategy,
			"Strategy should be autogen when resolved through registry")
		assert.NotEmpty(t, result.Value, "Generated value should not be empty")

		t.Logf("Registry integration: resolved placeholder to %d char secret",
			len(result.Value))
	})

	t.Run("RegistryPassesThroughNonPlaceholder", func(t *testing.T) {
		literal := "my-actual-secret-value"
		result, err := registry.Resolve(ctx, literal)
		require.NoError(t, err)
		require.NotNil(t, result)

		assert.Equal(t, secrets.StrategyLiteral, result.Strategy,
			"Non-placeholder should be treated as literal")
		assert.Equal(t, literal, result.Value,
			"Literal value should pass through unchanged")

		t.Logf("Registry integration: literal value passed through correctly")
	})

	t.Run("IsSecretURI", func(t *testing.T) {
		assert.True(t, registry.IsSecretURI("auto"),
			"Registry should recognize 'auto' as secret URI")
		assert.True(t, registry.IsSecretURI("changeme"),
			"Registry should recognize 'changeme' as secret URI")
		assert.False(t, registry.IsSecretURI("my-secret"),
			"Registry should not recognize arbitrary value as secret URI")
	})
}

// sanitizeTestName converts a test input to a safe test name.
func sanitizeTestName(s string) string {
	if s == "" {
		return "empty"
	}
	if len(s) > 20 {
		return s[:20]
	}
	return s
}
