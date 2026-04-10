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

package cache

import (
	"context"
	stderrors "errors"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// Secrets adapter errors.
var (
	// ErrNilInternalSecretStore indicates a nil internal secret store was provided.
	ErrNilInternalSecretStore = stderrors.New("cache: internal secret store cannot be nil")

	// ErrEmptySecretPath indicates an empty secret path was provided.
	ErrEmptySecretPath = stderrors.New("cache: secret path cannot be empty")

	// ErrEmptySecretValue indicates an empty secret value was provided.
	ErrEmptySecretValue = stderrors.New("cache: secret value cannot be empty")

	// ErrEmptySecret indicates the secret value retrieved is empty.
	ErrEmptySecret = stderrors.New("cache: internal secret is empty")
)

// InternalSecretStore defines the interface for QuicBaaS's internal secret storage.
// This interface mirrors the key methods from pkg/secrets/internal_store.go.
type InternalSecretStore interface {
	// Get decrypts and retrieves an internal secret.
	// Returns ErrInternalSecretNotFound if the secret doesn't exist.
	Get(ctx context.Context, key string) ([]byte, error)

	// Store encrypts and stores an internal secret.
	Store(ctx context.Context, key string, value []byte) error

	// Delete removes an internal secret.
	Delete(ctx context.Context, key string) error

	// Exists checks if a secret exists.
	Exists(ctx context.Context, key string) (bool, error)
}

// InternalSecretsFetcher wraps an InternalSecretStore to implement SecretsFetcher.
// This adapter allows the Redis L2 cache to securely retrieve credentials from
// QuicBaaS's internal secrets management system.
type InternalSecretsFetcher struct {
	store InternalSecretStore
}

// NewInternalSecretsFetcher creates a new InternalSecretsFetcher instance.
// The store parameter is required and must be non-nil.
func NewInternalSecretsFetcher(store InternalSecretStore) (*InternalSecretsFetcher, error) {
	if store == nil {
		return nil, ErrNilInternalSecretStore
	}

	return &InternalSecretsFetcher{
		store: store,
	}, nil
}

// GetSecret retrieves a secret value from the internal secrets store by path.
// The path follows QuicBaaS's internal secret key naming convention, such as
// "cache/redis/password" for the Redis password.
//
// Returns an error if the secret does not exist or cannot be retrieved.
func (f *InternalSecretsFetcher) GetSecret(ctx context.Context, path string) ([]byte, error) {
	if path == "" {
		return nil, ErrEmptySecretPath
	}

	if ctx == nil {
		ctx = context.Background()
	}

	// Retrieve the secret from the internal store
	secret, err := f.store.Get(ctx, path)
	if err != nil {
		return nil, &errors.DragonError{
			Code: errors.ErrInternal,
			Op:   "InternalSecretsFetcher.GetSecret",
			Err:  err,
		}
	}

	if len(secret) == 0 {
		return nil, ErrEmptySecret
	}

	return secret, nil
}

// Store stores a secret in the internal secrets store.
// This is a convenience method for setting up secrets programmatically.
func (f *InternalSecretsFetcher) Store(ctx context.Context, path string, value []byte) error {
	if path == "" {
		return ErrEmptySecretPath
	}

	if len(value) == 0 {
		return ErrEmptySecretValue
	}

	if ctx == nil {
		ctx = context.Background()
	}

	if err := f.store.Store(ctx, path, value); err != nil {
		return &errors.DragonError{
			Code: errors.ErrInternal,
			Op:   "InternalSecretsFetcher.Store",
			Err:  err,
		}
	}

	return nil
}

// Exists checks if a secret exists in the internal secrets store.
func (f *InternalSecretsFetcher) Exists(ctx context.Context, path string) (bool, error) {
	if path == "" {
		return false, ErrEmptySecretPath
	}

	if ctx == nil {
		ctx = context.Background()
	}

	exists, err := f.store.Exists(ctx, path)
	if err != nil {
		return false, &errors.DragonError{
			Code: errors.ErrInternal,
			Op:   "InternalSecretsFetcher.Exists",
			Err:  err,
		}
	}

	return exists, nil
}
