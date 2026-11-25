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
	"net/url"
	"strconv"
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

// SecretsEnsurer is the optional capability a SecretsClient may implement to
// support the `?ensure=true` form of a kms:// URI: generate-on-first-use with
// the given entropy length and encoding, then return the SAME value on every
// later call (idempotent).
//
// This is what makes the platform work securely out of the box: config carries
// references like
//
//	kms://ca/root/keys/0/password?ensure=true&len=32&enc=base64
//
// and the very first boot mints a high-entropy secret rather than failing with
// "secret not found" because nothing has been provisioned yet.
//
// A SecretsClient that does not implement this interface simply does not
// support ensure=true; the resolver then reports a typed error instead of
// silently degrading to a plain read that is guaranteed to fail on first boot.
type SecretsEnsurer interface {
	// EnsureSecret returns the secret at secretID, generating and persisting it
	// with the requested length/encoding when it does not yet exist.
	EnsureSecret(ctx context.Context, secretID string, length int, encoding string) ([]byte, error)
}

// kmsURIOptions holds the parsed query parameters of a kms:// URI.
type kmsURIOptions struct {
	// ensure requests generate-on-first-use semantics.
	ensure bool
	// length is the entropy length (bytes for base64/hex/raw, digits for numeric).
	length int
	// encoding is the output encoding: base64 | hex | numeric | raw.
	encoding string
}

// Default entropy for `ensure=true` when the URI omits len/enc.
const (
	defaultEnsureLength   = 32
	defaultEnsureEncoding = "base64"
)

// parseKMSURI splits a kms:// URI into the secret path (the stable secret
// identity) and its options. The query string MUST NOT be part of the secret
// ID — treating the whole remainder as the ID (the historical behaviour) made
// `?ensure=true&len=32&enc=base64` part of the name and silently discarded the
// auto-generation contract.
func parseKMSURI(raw string) (string, *kmsURIOptions, error) {
	rest := strings.TrimPrefix(raw, kmsURIPrefix)
	opts := &kmsURIOptions{
		length:   defaultEnsureLength,
		encoding: defaultEnsureEncoding,
	}

	path := rest
	if i := strings.IndexByte(rest, '?'); i >= 0 {
		path = rest[:i]
		q, err := url.ParseQuery(rest[i+1:])
		if err != nil {
			return "", nil, NewInvalidSecretURIError("parseKMSURI", raw, "malformed query string")
		}
		if v := q.Get("ensure"); v != "" {
			b, perr := strconv.ParseBool(v)
			if perr != nil {
				return "", nil, NewInvalidSecretURIError("parseKMSURI", raw, "ensure must be a boolean")
			}
			opts.ensure = b
		}
		if v := q.Get("len"); v != "" {
			n, perr := strconv.Atoi(v)
			if perr != nil || n <= 0 {
				return "", nil, NewInvalidSecretURIError("parseKMSURI", raw, "len must be a positive integer")
			}
			opts.length = n
		}
		if v := q.Get("enc"); v != "" {
			switch v {
			case "base64", "hex", "numeric", "raw":
				opts.encoding = v
			default:
				return "", nil, NewInvalidSecretURIError("parseKMSURI", raw, "enc must be base64, hex, numeric, or raw")
			}
		}
	}

	if path == "" {
		return "", nil, NewInvalidSecretURIError("parseKMSURI", raw, "path cannot be empty")
	}
	return path, opts, nil
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

	// Split the secret identity from its options. The query string is NOT part
	// of the secret ID.
	path, opts, err := parseKMSURI(raw)
	if err != nil {
		return nil, err
	}

	// ensure=true: generate-on-first-use. This is the contract platform configs
	// rely on (kms://...?ensure=true&len=32&enc=base64) so a first boot mints
	// the secret instead of failing "not found" against an empty store.
	if opts.ensure {
		ensurer, ok := r.client.(SecretsEnsurer)
		if !ok {
			return nil, NewResolutionFailedError(op, raw,
				"ensure=true requested but the KMS client does not support secret generation", nil)
		}
		secretData, ensureErr := ensurer.EnsureSecret(ctx, path, opts.length, opts.encoding)
		if ensureErr != nil {
			return nil, NewResolutionFailedError(op, raw, "failed to ensure secret in KMS", ensureErr)
		}
		return NewSecretValue(ctx, raw, secretData, StrategyLocalKMS)
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

	return NewSecretValue(ctx, raw, secretData, StrategyLocalKMS)
}

// Ensure LocalKMSResolver implements SecretResolver.
var _ SecretResolver = (*LocalKMSResolver)(nil)
