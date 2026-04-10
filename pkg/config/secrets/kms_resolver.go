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
	"strings"
)

const (
	// kmsScheme is the URI scheme for local KMS secrets.
	kmsScheme = "kms"

	// kmsURIPrefix is the full prefix for KMS URIs.
	kmsURIPrefix = "kms://"

	// kmsURIMinLength is the minimum valid URI length (scheme + at least one char path).
	kmsURIMinLength = 7 // "kms://x"
)

// SecretsClient defines the interface for retrieving secrets from the local KMS.
// This interface allows for dependency injection and testing.
type SecretsClient interface {
	// GetSecret retrieves a secret by its ID/path from the local KMS.
	// Returns the secret data as bytes, or an error if the secret cannot be retrieved.
	GetSecret(ctx context.Context, secretID string) ([]byte, error)
}

// LocalKMSResolver resolves secrets from the local QuicBaaS KMS using the kms:// URI scheme.
// It implements the SecretResolver interface and delegates secret retrieval to a SecretsClient.
type LocalKMSResolver struct {
	client SecretsClient
}

// NewLocalKMSResolver creates a new LocalKMSResolver with the provided SecretsClient.
// The client is used to retrieve secrets from the local KMS.
// Returns nil if client is nil.
func NewLocalKMSResolver(client SecretsClient) *LocalKMSResolver {
	if client == nil {
		return nil
	}
	return &LocalKMSResolver{
		client: client,
	}
}

// Scheme returns "kms" as the scheme identifier for this resolver.
func (r *LocalKMSResolver) Scheme() string {
	return kmsScheme
}

// CanResolve returns true if the raw value is a valid kms:// URI.
// The URI must start with "kms://" and have a non-empty path component.
func (r *LocalKMSResolver) CanResolve(raw string) bool {
	return strings.HasPrefix(raw, kmsURIPrefix) && len(raw) >= kmsURIMinLength
}

// Resolve retrieves a secret from the local KMS using the provided URI.
// The URI format is kms://path/to/secret where the path identifies the secret in KMS.
//
// Returns:
//   - SecretValue with Strategy StrategyLocalKMS on success
//   - InvalidSecretURIError if the URI format is invalid
//   - SecretNotFoundError if the secret does not exist
//   - ResolutionFailedError for other retrieval failures
func (r *LocalKMSResolver) Resolve(ctx context.Context, raw string) (*SecretValue, error) {
	const op = "LocalKMSResolver.Resolve"

	// Check for context cancellation first
	select {
	case <-ctx.Done():
		return nil, NewResolutionFailedError(op, raw, "context cancelled", ctx.Err())
	default:
	}

	// Validate URI format
	if !r.CanResolve(raw) {
		if !strings.HasPrefix(raw, kmsURIPrefix) {
			return nil, NewInvalidSecretURIError(op, raw, "must start with kms://")
		}
		return nil, NewInvalidSecretURIError(op, raw, "path cannot be empty")
	}

	// Extract the path from the URI
	path := strings.TrimPrefix(raw, kmsURIPrefix)
	if path == "" {
		return nil, NewInvalidSecretURIError(op, raw, "path cannot be empty")
	}

	// Retrieve the secret from the client
	secretData, err := r.client.GetSecret(ctx, path)
	if err != nil {
		// Check if it's a not found error
		if IsSecretNotFound(err) {
			return nil, NewSecretNotFoundError(op, path, "local_kms")
		}
		// For other errors, wrap as resolution failed
		return nil, NewResolutionFailedError(op, raw, "failed to retrieve secret from KMS", err)
	}

	return &SecretValue{
		Raw:      raw,
		Value:    string(secretData),
		Strategy: StrategyLocalKMS,
	}, nil
}

// Ensure LocalKMSResolver implements SecretResolver.
var _ SecretResolver = (*LocalKMSResolver)(nil)
