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
	"time"
)

const (
	// RemoteKMSScheme is the URI scheme for remote KMS secrets.
	RemoteKMSScheme = "remote-kms"

	// RemoteKMSSchemePrefix is the URI prefix for remote KMS secrets.
	RemoteKMSSchemePrefix = RemoteKMSScheme + "://"

	// DefaultRemoteKMSTimeout is the default timeout for remote KMS operations.
	DefaultRemoteKMSTimeout = 30 * time.Second
)

// RemoteSecretsClient defines the interface for fetching secrets from a remote QuicBaaS instance.
type RemoteSecretsClient interface {
	// GetSecret retrieves a secret from the remote QuicBaaS instance.
	// The host parameter is the host:port of the remote instance.
	// The secretID parameter is the path to the secret.
	GetSecret(ctx context.Context, host string, secretID string) ([]byte, error)
}

// RemoteKMSResolverOption is a functional option for configuring RemoteKMSResolver.
type RemoteKMSResolverOption func(*RemoteKMSResolver)

// RemoteKMSResolver resolves secrets from a remote QuicBaaS instance using the remote-kms:// URI scheme.
type RemoteKMSResolver struct {
	client  RemoteSecretsClient
	timeout time.Duration
}

// NewRemoteKMSResolver creates a new RemoteKMSResolver with the provided client.
func NewRemoteKMSResolver(client RemoteSecretsClient, opts ...RemoteKMSResolverOption) *RemoteKMSResolver {
	r := &RemoteKMSResolver{
		client:  client,
		timeout: DefaultRemoteKMSTimeout,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// WithTimeout returns a functional option that sets the timeout for remote KMS operations.
func WithTimeout(d time.Duration) RemoteKMSResolverOption {
	return func(r *RemoteKMSResolver) {
		if d > 0 {
			r.timeout = d
		}
	}
}

// Scheme returns the URI scheme this resolver handles.
func (r *RemoteKMSResolver) Scheme() string {
	return RemoteKMSScheme
}

// CanResolve returns true if the raw value is a remote-kms:// URI.
func (r *RemoteKMSResolver) CanResolve(raw string) bool {
	return strings.HasPrefix(raw, RemoteKMSSchemePrefix)
}

// Resolve fetches the secret from the remote QuicBaaS instance.
// URI format: remote-kms://host:port/path/to/secret
func (r *RemoteKMSResolver) Resolve(ctx context.Context, raw string) (*SecretValue, error) {
	const op = "RemoteKMSResolver.Resolve"

	// Validate context
	if ctx == nil {
		return nil, NewSecretResolutionError(RemoteKMSScheme, "context cannot be nil", ErrNilContext)
	}

	// Check context cancellation before proceeding
	select {
	case <-ctx.Done():
		return nil, NewSecretResolutionError(RemoteKMSScheme, "context cancelled", ctx.Err())
	default:
	}

	// Validate client is set
	if r.client == nil {
		return nil, NewResolutionFailedError(op, raw, "remote secrets client is nil", nil)
	}

	// Parse the URI
	host, path, err := r.parseURI(raw)
	if err != nil {
		return nil, err
	}

	// Apply timeout to context
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	// Fetch the secret from the remote instance
	secretData, err := r.client.GetSecret(ctx, host, path)
	if err != nil {
		// Check if it's a context cancellation/timeout
		if ctx.Err() != nil {
			if ctx.Err() == context.DeadlineExceeded {
				return nil, NewRemoteConnectionFailedError(op, host, RemoteKMSScheme, context.DeadlineExceeded)
			}
			return nil, NewSecretResolutionError(RemoteKMSScheme, "context cancelled", ctx.Err())
		}

		// Check if the error indicates the secret was not found
		if IsSecretNotFound(err) {
			return nil, NewSecretNotFoundError(op, path, host)
		}

		// Wrap as connection failed error
		return nil, NewRemoteConnectionFailedError(op, host, RemoteKMSScheme, err)
	}

	return NewSecretValue(raw, secretData, StrategyRemoteKMS)
}

// parseURI extracts the host and path from a remote-kms:// URI.
// Format: remote-kms://host:port/path/to/secret
func (r *RemoteKMSResolver) parseURI(raw string) (host string, path string, err error) {
	const op = "RemoteKMSResolver.parseURI"

	// Verify prefix
	if !strings.HasPrefix(raw, RemoteKMSSchemePrefix) {
		return "", "", NewInvalidSecretURIError(op, raw, "URI must start with "+RemoteKMSSchemePrefix)
	}

	// Remove the scheme prefix
	remainder := strings.TrimPrefix(raw, RemoteKMSSchemePrefix)
	if remainder == "" {
		return "", "", NewInvalidSecretURIError(op, raw, "URI is missing host and path")
	}

	// Find the first slash after the host portion
	host, path, found := strings.Cut(remainder, "/")
	if !found {
		return "", "", NewInvalidSecretURIError(op, raw, "URI is missing path separator")
	}
	if host == "" {
		return "", "", NewInvalidSecretURIError(op, raw, "URI is missing host")
	}

	// Validate path is not empty
	if path == "" {
		return "", "", NewInvalidSecretURIError(op, raw, "URI path cannot be empty")
	}

	return host, path, nil
}

// Ensure RemoteKMSResolver implements SecretResolver.
var _ SecretResolver = (*RemoteKMSResolver)(nil)
