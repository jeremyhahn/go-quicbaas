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
	"crypto/rand"
	"encoding/base64"
	"io"
	"strings"
)

// AutoGenPlaceholders defines the placeholder values that trigger auto-generation.
// When a config value matches any of these (case-insensitive), the AutoGenResolver
// will generate a new high-entropy secret.
var AutoGenPlaceholders = []string{
	"auto",
	"123456",
	"changeme",
	"CHANGEME",
	"placeholder",
}

// DefaultSecretSize is the default number of bytes for generated secrets.
const DefaultSecretSize = 32

// AutoGenResolver generates high-entropy secrets when config values match placeholders.
// It uses crypto/rand for cryptographically secure random number generation.
type AutoGenResolver struct {
	entropy    io.Reader
	secretSize int
}

// NewAutoGenResolver creates a new AutoGenResolver with default settings.
// Uses crypto/rand.Reader for entropy and 32-byte secret size.
func NewAutoGenResolver() *AutoGenResolver {
	return &AutoGenResolver{
		entropy:    rand.Reader,
		secretSize: DefaultSecretSize,
	}
}

// WithEntropySource returns a new AutoGenResolver with the specified entropy source.
// This is primarily useful for testing with deterministic entropy.
// WARNING: Only use deterministic entropy sources for testing purposes.
func (r *AutoGenResolver) WithEntropySource(entropy io.Reader) *AutoGenResolver {
	return &AutoGenResolver{
		entropy:    entropy,
		secretSize: r.secretSize,
	}
}

// WithSecretSize returns a new AutoGenResolver with the specified secret size.
// The size is the number of random bytes to generate before base64 encoding.
// Minimum size is 1 byte; values less than 1 will use DefaultSecretSize.
func (r *AutoGenResolver) WithSecretSize(size int) *AutoGenResolver {
	if size < 1 {
		size = DefaultSecretSize
	}
	return &AutoGenResolver{
		entropy:    r.entropy,
		secretSize: size,
	}
}

// Scheme returns "auto" as the scheme identifier for this resolver.
func (r *AutoGenResolver) Scheme() string {
	return "auto"
}

// CanResolve returns true if the raw value matches any auto-generation placeholder.
// Comparison is case-insensitive for flexibility in configuration files.
func (r *AutoGenResolver) CanResolve(raw string) bool {
	normalizedRaw := strings.TrimSpace(strings.ToLower(raw))
	for _, placeholder := range AutoGenPlaceholders {
		if normalizedRaw == strings.ToLower(placeholder) {
			return true
		}
	}
	return false
}

// Resolve generates a new high-entropy secret value.
// Each call generates a unique value - no caching is performed.
// The generated secret is URL-safe base64 encoded.
func (r *AutoGenResolver) Resolve(ctx context.Context, raw string) (*SecretValue, error) {
	// Check context cancellation
	select {
	case <-ctx.Done():
		return nil, NewSecretResolutionError("auto", "context cancelled", ctx.Err())
	default:
	}

	// Generate random bytes
	randomBytes := make([]byte, r.secretSize)
	n, err := r.entropy.Read(randomBytes)
	if err != nil {
		return nil, &EntropyGenerationFailedError{
			RequestedBytes: r.secretSize,
			Cause:          err,
		}
	}
	if n != r.secretSize {
		return nil, &EntropyGenerationFailedError{
			RequestedBytes: r.secretSize,
			Cause:          io.ErrUnexpectedEOF,
		}
	}

	// Encode as URL-safe base64 (no padding for cleaner secrets)
	encoded := base64.RawURLEncoding.EncodeToString(randomBytes)

	return NewSecretValue(raw, []byte(encoded), StrategyAutoGen), nil
}

// Ensure AutoGenResolver implements SecretResolver.
var _ SecretResolver = (*AutoGenResolver)(nil)
